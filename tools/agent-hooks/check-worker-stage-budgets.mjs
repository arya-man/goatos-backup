#!/usr/bin/env node
// check-worker-stage-budgets.mjs — KERN-REV-05 / 05B parity guard.
//
// Consolidating the retired per-stage Cloud Run Jobs into the kernel-worker
// service must NOT silently shrink their processing budgets. This guard checks
// BOTH dimensions the consolidation can regress:
//   1. Batch limits — each env's cloud_run_worker.tf must set GOATOS_OUTBOX_LIMIT
//      >= 500 and GOATOS_NOTIFICATION_LIMIT >= 100 (else the stages fall back to
//      the one-shot default of 50).
//   2. Execution budget / cadence compatibility (backend/cmd/kernel-worker/main.go)
//      — the outbox relay must sit on a cadence whose EFFECTIVE per-run timeout
//      (supervisor caps at 90% of the interval) is at least the accepted 54s
//      budget, and must NOT share a cadence with the notification dispatcher (a
//      slow outbox drain would otherwise starve notification delivery).
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const ENVS = ["dev", "stg"];

const REQUIRED_MIN = {
  GOATOS_OUTBOX_LIMIT: 500,
  GOATOS_NOTIFICATION_LIMIT: 100,
};

// Accepted outbox execution budget (see KERN-REV-05A: proven sufficient for a
// 500-message batch). A 1-minute fast lane yields exactly this after the 90% cap.
const ACCEPTED_OUTBOX_BUDGET_SECONDS = 54;

function envValue(tf, name) {
  const re = new RegExp(`name\\s*=\\s*"${name}"[\\s\\S]{0,80}?value\\s*=\\s*"([^"]*)"`);
  const m = tf.match(re);
  return m ? m[1] : null;
}

// parseAllEnv returns a Map of every literal env { name = "X" value = "Y" } pair in the tf. Used to
// resolve durationEnv("X", <default>) explicit timeouts against the deployed value (KERN-06).
function parseAllEnv(tf) {
  const map = new Map();
  const re = /name\s*=\s*"([^"]+)"[\s\S]{0,120}?value\s*=\s*"([^"]*)"/g;
  let m;
  while ((m = re.exec(tf)) !== null) {
    if (!map.has(m[1])) map.set(m[1], m[2]);
  }
  return map;
}

// --- limit checks (pure) ---
function limitFindings(env, tf) {
  const findings = [];
  for (const [name, min] of Object.entries(REQUIRED_MIN)) {
    const raw = envValue(tf, name);
    if (raw === null) {
      findings.push(
        `infra/envs/${env}/cloud_run_worker.tf: ${name} is not set — the consolidated stage would fall back to the default 50, below the retired job budget of ${min}`,
      );
      continue;
    }
    const val = Number.parseInt(raw, 10);
    if (!Number.isFinite(val) || val < min) {
      findings.push(`infra/envs/${env}/cloud_run_worker.tf: ${name}=${raw} is below the retired job budget of ${min}`);
    }
  }
  return findings;
}

// --- cadence / execution-budget checks (pure) ---
function durationToSeconds(expr) {
  const m = expr.match(/([0-9]+)\s*\*\s*time\.(Second|Minute|Hour)/);
  if (!m) return null;
  const n = Number(m[1]);
  return m[2] === "Second" ? n : m[2] === "Minute" ? n * 60 : n * 3600;
}

// Parses Go duration strings like "30s", "1m30s", "2h" into seconds.
// Returns null if unparseable.
function parseGoDuration(dur) {
  if (!dur) return null;
  dur = dur.trim();
  let seconds = 0;
  let matched = false;

  // Match hours (e.g., "2h")
  let m = dur.match(/(\d+(\.\d+)?)\s*h/);
  if (m) {
    seconds += Number(m[1]) * 3600;
    matched = true;
  }
  // Match minutes (e.g., "30m")
  m = dur.match(/(\d+(\.\d+)?)\s*m(?!s)/);
  if (m) {
    seconds += Number(m[1]) * 60;
    matched = true;
  }
  // Match seconds (e.g., "30s")
  m = dur.match(/(\d+(\.\d+)?)\s*s/);
  if (m) {
    seconds += Number(m[1]);
    matched = true;
  }

  return matched ? Math.floor(seconds) : null;
}

// splitTopLevelArgs returns the trimmed top-level argument expressions of the balanced (...) group
// whose opening '(' is at openParenIdx. Nested parens (e.g. durationEnv("X", 10*time.Second) or a
// stage constructor) and string literals are respected, so a comma inside them does NOT split.
function splitTopLevelArgs(src, openParenIdx) {
  const args = [];
  let depth = 0;
  let start = openParenIdx + 1;
  let inStr = false;
  let strCh = "";
  for (let i = openParenIdx; i < src.length; i++) {
    const ch = src[i];
    if (inStr) {
      if (ch === strCh && src[i - 1] !== "\\") inStr = false;
      continue;
    }
    if (ch === '"' || ch === "`") {
      inStr = true;
      strCh = ch;
      continue;
    }
    if (ch === "(") {
      depth++;
      if (depth === 1) start = i + 1;
      continue;
    }
    if (ch === ")") {
      depth--;
      if (depth === 0) {
        const last = src.slice(start, i).trim();
        if (last !== "") args.push(last);
        break;
      }
      continue;
    }
    if (ch === "," && depth === 1) {
      args.push(src.slice(start, i).trim());
      start = i + 1;
    }
  }
  return args;
}

// resolveExplicitTimeout evaluates a RegisterCadenceWithTimeout 3rd argument (the explicit timeout)
// to seconds. It returns:
//   - null                       -> no explicit timeout (plain RegisterCadence); caller uses the default formula
//   - { seconds }                -> a literal duration OR a durationEnv("ENV", <default>) resolved against the
//                                   deployed env (env value if set, else the Go default literal)
//   - { unresolvable: reason }   -> the expression is PRESENT but cannot be evaluated -> FAIL CLOSED
// KERN-06: a durationEnv(...) (or any non-literal) 3rd arg previously parsed as null and was silently
// treated as "no explicit timeout", so the guard assumed the 90%-of-interval default and could pass a
// 10s configured budget as 54s. This resolves it or fails closed instead.
function resolveExplicitTimeout(expr, envMap) {
  if (expr === null || expr === undefined) return null;
  const trimmed = expr.trim();

  // Literal N*time.Unit — anchored so it does NOT greedily match the inner default literal of a
  // durationEnv("X", 60*time.Second) expression (which must go through env resolution below).
  if (/^[0-9]+\s*\*\s*time\.(?:Second|Minute|Hour)$/.test(trimmed)) {
    return { seconds: durationToSeconds(trimmed) };
  }

  // durationEnv("ENV_NAME", <defaultLiteral>) — resolve to the deployed env value or the Go default.
  const de = trimmed.match(/^durationEnv\(\s*"([^"]+)"\s*,\s*([0-9]+\s*\*\s*time\.(?:Second|Minute|Hour))\s*\)$/);
  if (de) {
    const envName = de[1];
    const defaultSeconds = durationToSeconds(de[2]);
    const envVal = envMap.get(envName);
    if (envVal !== undefined && envVal !== null) {
      const parsed = parseGoDuration(String(envVal));
      if (parsed !== null) return { seconds: parsed };
      return {
        unresolvable: `explicit timeout durationEnv("${envName}", …) is overridden by ${envName}="${envVal}", which is not a valid Go duration`,
      };
    }
    // Env not set (or nonliteral) in the deployed tf -> the Go default literal applies at runtime.
    if (defaultSeconds !== null) return { seconds: defaultSeconds };
    return { unresolvable: `explicit timeout durationEnv("${envName}", …) has a non-literal default the guard cannot evaluate` };
  }

  // Present but neither a literal nor a recognized durationEnv(...) form -> fail closed.
  return {
    unresolvable: `explicit timeout expression "${trimmed}" is not a literal duration or durationEnv("ENV", <default>) — the guard cannot evaluate the deployed budget`,
  };
}

// Parses each supervisor.RegisterCadence[WithTimeout]("name", interval, ...) call into
// { name, intervalSeconds, isWithTimeout, explicitTimeoutExpr, body } where body is the source from
// that call up to the next RegisterCadence (so it contains that cadence's stage constructors).
// For RegisterCadenceWithTimeout the 3rd top-level arg is the RAW explicit-timeout expression (a
// literal, a durationEnv(...), or something else — resolved later by resolveExplicitTimeout, which
// fails closed rather than silently assuming the default). For plain RegisterCadence it is null.
function parseCadences(mainGo) {
  const re = /supervisor\.RegisterCadence(WithTimeout)?\s*\(/g;
  const calls = [];
  let m;
  while ((m = re.exec(mainGo)) !== null) {
    calls.push({ isWithTimeout: !!m[1], parenIdx: re.lastIndex - 1, index: m.index });
  }
  return calls.map((c, i) => {
    const args = splitTopLevelArgs(mainGo, c.parenIdx);
    const name = (args[0] || "").replace(/^"|"$/g, "");
    const intervalExpr = args[1] || "";
    const explicitTimeoutExpr = c.isWithTimeout ? (args[2] ?? null) : null;
    return {
      name,
      intervalSeconds: durationToSeconds(intervalExpr),
      isWithTimeout: c.isWithTimeout,
      explicitTimeoutExpr,
      body: mainGo.slice(c.index, i + 1 < calls.length ? calls[i + 1].index : mainGo.length),
    };
  });
}

// Reproduces the supervisor's defaultStageTimeout formula exactly (supervisor.go lines 161-195).
// Given an interval and queryTimeout, computes the effective per-run budget for a plain cadence.
function defaultStageTimeout(intervalSeconds, queryTimeoutSeconds) {
  const minStageTimeout = 1;

  if (intervalSeconds <= 0) {
    if (queryTimeoutSeconds > 0) {
      return queryTimeoutSeconds * 2;
    }
    return minStageTimeout;
  }

  let timeoutCap = intervalSeconds - intervalSeconds / 10; // 90% of interval
  if (timeoutCap <= 0) {
    timeoutCap = intervalSeconds;
  }

  let target = queryTimeoutSeconds * 2;
  const floor = intervalSeconds / 4;
  if (floor > target) {
    target = floor;
  }
  if (target > timeoutCap) {
    target = timeoutCap;
  }
  if (target < minStageTimeout) {
    target = minStageTimeout;
  }
  if (target >= intervalSeconds) {
    target = intervalSeconds - 1;
    if (target <= 0) {
      target = 1;
    }
  }
  return Math.floor(target);
}

// Calculates the effective per-run budget for a cadence, reproducing the
// supervisor's formula exactly (see RegisterCadenceWithTimeout, supervisor.go lines 121-130):
//   - If explicit timeout is given (> 0), cap it at 90% of interval
//   - If explicit timeout is absent/zero, use defaultStageTimeout
// explicitSeconds is the RESOLVED explicit timeout (from resolveExplicitTimeout) or null for none.
function effectiveBudgetSeconds(intervalSeconds, explicitSeconds, queryTimeoutSeconds) {
  if (explicitSeconds && explicitSeconds > 0) {
    // Cap explicit timeout at 90% of interval (the supervisor's rule).
    const cap = intervalSeconds - intervalSeconds / 10;
    return Math.min(explicitSeconds, Math.floor(cap));
  }
  // No explicit timeout: use defaultStageTimeout with the actual queryTimeout.
  return defaultStageTimeout(intervalSeconds, queryTimeoutSeconds);
}

function cadenceFindings(mainGo, envMap) {
  const findings = [];

  // Read and validate GOATOS_PG_QUERY_TIMEOUT from deployed environment.
  let queryTimeoutSeconds = null;
  const queryTimeoutEnv = envMap.get("GOATOS_PG_QUERY_TIMEOUT");
  if (queryTimeoutEnv === undefined) {
    findings.push(
      "cloud_run_worker.tf: GOATOS_PG_QUERY_TIMEOUT is not set — cadence budgets cannot be computed",
    );
    return findings; // Stop further checks if queryTimeout is missing.
  }
  if (queryTimeoutEnv === null) {
    // Explicitly null means the expression is nonliteral (e.g., var.something, no quoted value).
    findings.push(
      "cloud_run_worker.tf: GOATOS_PG_QUERY_TIMEOUT uses a nonliteral expression — the guard cannot validate the deployed budget. Set an explicit string value (e.g., value = \"30s\").",
    );
    return findings;
  }
  // Check for interpolated expressions like "${var.timeout}"
  if (queryTimeoutEnv.includes("${") || queryTimeoutEnv.includes("var.")) {
    findings.push(
      `cloud_run_worker.tf: GOATOS_PG_QUERY_TIMEOUT value "${queryTimeoutEnv}" uses variable interpolation — the guard cannot validate the deployed budget. Set an explicit string value (e.g., value = "30s").`,
    );
    return findings;
  }
  queryTimeoutSeconds = parseGoDuration(queryTimeoutEnv);
  if (queryTimeoutSeconds === null) {
    findings.push(
      `cloud_run_worker.tf: GOATOS_PG_QUERY_TIMEOUT value "${queryTimeoutEnv}" is not a valid Go duration (expected format like "30s", "1m", etc.)`,
    );
    return findings;
  }

  const cadences = parseCadences(mainGo);
  const outbox = cadences.find((c) => c.body.includes("NewOutboxRelayStage"));
  const notify = cadences.find((c) => c.body.includes("NewNotificationDispatcherStage"));

  if (!outbox) {
    findings.push("backend/cmd/kernel-worker/main.go: NewOutboxRelayStage is not registered on any cadence");
  } else {
    const resolved = resolveExplicitTimeout(outbox.explicitTimeoutExpr, envMap);
    if (resolved && resolved.unresolvable) {
      // KERN-06: an explicit timeout the guard cannot evaluate must FAIL CLOSED, never fall back to
      // the 90%-of-interval default (which could pass a 10s configured budget as 54s).
      findings.push(
        `backend/cmd/kernel-worker/main.go: outbox cadence "${outbox.name}" ${resolved.unresolvable}; cannot prove the per-run budget meets the accepted ${ACCEPTED_OUTBOX_BUDGET_SECONDS}s`,
      );
    } else {
      const explicitSeconds = resolved ? resolved.seconds : null;
      const effective = effectiveBudgetSeconds(outbox.intervalSeconds, explicitSeconds, queryTimeoutSeconds);
      if (effective < ACCEPTED_OUTBOX_BUDGET_SECONDS) {
        const explicitNote = explicitSeconds
          ? ` (explicit timeout ${explicitSeconds}s capped to 90% of interval)`
          : ` (computed from GOATOS_PG_QUERY_TIMEOUT=${queryTimeoutEnv})`;
        findings.push(
          `backend/cmd/kernel-worker/main.go: outbox cadence "${outbox.name}" interval ${outbox.intervalSeconds}s gives an effective per-run budget of ${effective}s${explicitNote}, below the accepted ${ACCEPTED_OUTBOX_BUDGET_SECONDS}s outbox budget`,
        );
      }
    }
  }
  if (outbox && notify && outbox.name === notify.name) {
    findings.push(
      `backend/cmd/kernel-worker/main.go: outbox relay and notification dispatcher share cadence "${outbox.name}" — a slow outbox drain can starve notification dispatch (put them on separate cadences)`,
    );
  }
  return findings;
}

function selfTest() {
  const goodTf = `env { name = "GOATOS_OUTBOX_LIMIT" value = "500" } env { name = "GOATOS_NOTIFICATION_LIMIT" value = "100" } env { name = "GOATOS_PG_QUERY_TIMEOUT" value = "30s" }`;
  const goodMain = `
  supervisor.RegisterCadence("outbox", 1*time.Minute,
    kernelstages.NewOutboxRelayStage(deps),
  )
  supervisor.RegisterCadence("notify", 1*time.Minute,
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;
  const goodEnvMap = new Map([["GOATOS_PG_QUERY_TIMEOUT", "30s"]]);

  if (limitFindings("x", goodTf).length !== 0 || cadenceFindings(goodMain, goodEnvMap).length !== 0) {
    throw new Error("self-test: a compliant config produced findings");
  }

  // 1) outbox limit below 500.
  if (!limitFindings("x", goodTf.replace('"500"', '"400"')).some((f) => f.includes("GOATOS_OUTBOX_LIMIT"))) {
    throw new Error("self-test (1): missed outbox limit below 500");
  }

  // 2) notification limit below 100.
  if (!limitFindings("x", goodTf.replace('"100"', '"50"')).some((f) => f.includes("GOATOS_NOTIFICATION_LIMIT"))) {
    throw new Error("self-test (2): missed notification limit below 100");
  }

  // 3a) explicit timeout BELOW accepted budget (30s cadence with 20s explicit: capped to 27s).
  const explicitSubBudget = `
  supervisor.RegisterCadenceWithTimeout("outbox", 30*time.Second, 20*time.Second,
    kernelstages.NewOutboxRelayStage(deps),
  )
  supervisor.RegisterCadence("notify", 1*time.Minute,
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;
  if (!cadenceFindings(explicitSubBudget, goodEnvMap).some((f) => f.includes("below the accepted") && f.includes("explicit timeout"))) {
    throw new Error("self-test (3a): missed explicit timeout sub-budget case");
  }

  // 3b) explicit timeout that is fine: obligation-sweep 180s on 5m, capped to 90% of 5m = 270s, so 180s OK.
  const explicitOk = `
  supervisor.RegisterCadence("outbox", 1*time.Minute,
    kernelstages.NewOutboxRelayStage(deps),
  )
  supervisor.RegisterCadenceWithTimeout("obligation-sweep", 5*time.Minute, 180*time.Second,
    kernelstages.NewObligationSweeperStage(deps, sweeperCfg),
  )`;
  if (cadenceFindings(explicitOk, goodEnvMap).length !== 0) {
    throw new Error("self-test (3b): compliant explicit timeout (180s on 5m) flagged as error");
  }

  // 3c) interval-derived (plain RegisterCadence, no explicit) → existing behavior preserved.
  const intervalDerived = goodMain;
  if (cadenceFindings(intervalDerived, goodEnvMap).length !== 0) {
    throw new Error("self-test (3c): plain RegisterCadence (interval-derived) flagged as error");
  }

  // 3d) outbox timeout below the accepted budget (30s cadence -> 27s effective with 30s queryTimeout).
  const shortCadence = goodMain.replace('"outbox", 1*time.Minute', '"outbox", 30*time.Second');
  if (!cadenceFindings(shortCadence, goodEnvMap).some((f) => f.includes("below the accepted"))) {
    throw new Error("self-test (3d): missed outbox cadence with sub-budget effective timeout");
  }

  // 4) incompatible combination: outbox + notification on the SAME cadence.
  const shared = `
  supervisor.RegisterCadence("fast", 1*time.Minute,
    kernelstages.NewOutboxRelayStage(deps),
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;
  if (!cadenceFindings(shared, goodEnvMap).some((f) => f.includes("share cadence"))) {
    throw new Error("self-test (4): missed outbox+notification sharing one cadence");
  }

  // 5) MISSING GOATOS_PG_QUERY_TIMEOUT — guard must FAIL.
  const missingQueryTimeoutMap = new Map(); // Empty map, no queryTimeout entry
  if (!cadenceFindings(goodMain, missingQueryTimeoutMap).some((f) => f.includes("GOATOS_PG_QUERY_TIMEOUT is not set"))) {
    throw new Error("self-test (5): missed missing GOATOS_PG_QUERY_TIMEOUT");
  }

  // 6) LOWERED GOATOS_PG_QUERY_TIMEOUT below what stages need — with 10s queryTimeout on 1m cadence,
  //    effective = max(2*10=20, 60/4=15) = 20s < 54s — must FAIL.
  const lowQueryTimeoutMap = new Map([["GOATOS_PG_QUERY_TIMEOUT", "10s"]]);
  if (!cadenceFindings(goodMain, lowQueryTimeoutMap).some((f) => f.includes("below the accepted"))) {
    throw new Error("self-test (6): missed lowered GOATOS_PG_QUERY_TIMEOUT producing sub-budget");
  }

  // 7) NONLITERAL durationEnv(...) expression — guard must FAIL.
  const nonliteralMap = new Map([["GOATOS_PG_QUERY_TIMEOUT", null]]); // null = unparseable nonliteral
  if (!cadenceFindings(goodMain, nonliteralMap).some((f) => f.includes("nonliteral expression"))) {
    throw new Error("self-test (7): missed nonliteral durationEnv(...) expression");
  }

  // 8) INVALID Go duration format — guard must FAIL.
  const invalidDurationMap = new Map([["GOATOS_PG_QUERY_TIMEOUT", "invalid"]]);
  if (!cadenceFindings(goodMain, invalidDurationMap).some((f) => f.includes("not a valid Go duration"))) {
    throw new Error("self-test (8): missed invalid Go duration format");
  }

  // 9) KERN-06: outbox on RegisterCadenceWithTimeout with a durationEnv(...) explicit timeout whose
  //    DEFAULT meets budget and whose env is unset — must PASS (resolved to the default, not silently
  //    assumed). 1m interval, durationEnv default 60s -> cap 90% of 60 = 54s == accepted budget.
  const durEnvDefaultOk = `
  supervisor.RegisterCadenceWithTimeout("outbox", 1*time.Minute, durationEnv("GOATOS_OUTBOX_TIMEOUT", 60*time.Second),
    kernelstages.NewOutboxRelayStage(deps),
  )
  supervisor.RegisterCadence("notify", 1*time.Minute,
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;
  if (cadenceFindings(durEnvDefaultOk, goodEnvMap).length !== 0) {
    throw new Error("self-test (9): durationEnv explicit timeout (default 60s -> 54s) wrongly flagged");
  }

  // 10) KERN-06: the SAME durationEnv cadence, but the deployed env OVERRIDES it to a sub-budget value
  //     — must FAIL (previously the null-parse silently assumed 54s and passed). Env 10s -> 10s < 54s.
  const durEnvOverrideLow = new Map([
    ["GOATOS_PG_QUERY_TIMEOUT", "30s"],
    ["GOATOS_OUTBOX_TIMEOUT", "10s"],
  ]);
  if (
    !cadenceFindings(durEnvDefaultOk, durEnvOverrideLow).some(
      (f) => f.includes("below the accepted") && f.includes("explicit timeout 10s"),
    )
  ) {
    throw new Error("self-test (10): missed durationEnv env-override below the accepted budget");
  }

  // 11) KERN-06: an explicit timeout expression the guard cannot evaluate (not a literal, not a
  //     recognized durationEnv form) must FAIL CLOSED, never fall back to the 90%-of-interval default.
  const unresolvableExplicit = `
  supervisor.RegisterCadenceWithTimeout("outbox", 1*time.Minute, someComputedTimeout(cfg),
    kernelstages.NewOutboxRelayStage(deps),
  )
  supervisor.RegisterCadence("notify", 1*time.Minute,
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;
  if (!cadenceFindings(unresolvableExplicit, goodEnvMap).some((f) => f.includes("cannot prove the per-run budget"))) {
    throw new Error("self-test (11): missed unresolvable explicit timeout (must fail closed)");
  }

  console.error(
    "worker-stage-budgets guard: self-test passed (11 cases: limits + explicit-sub-budget + explicit-ok + interval-derived + short-cadence + shared-cadence + missing/lowered/nonliteral/invalid queryTimeout + durationEnv default-ok/env-override-low/unresolvable-fail-closed)",
  );
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const errors = [];
let allEnvMap = new Map(); // Shared environment map; use the first valid environment's queryTimeout.
let foundEnv = false;

for (const env of ENVS) {
  const path = resolve(repo, "infra/envs", env, "cloud_run_worker.tf");
  if (!existsSync(path)) {
    errors.push(`infra/envs/${env}/cloud_run_worker.tf: missing (kernel-worker service not defined)`);
    continue;
  }
  const tfContent = readFileSync(path, "utf8");
  errors.push(...limitFindings(env, tfContent));

  // Build the deployed env map from the first available environment's TF. All literal name/value env
  // pairs are captured so a durationEnv("SOME_ENV", <default>) explicit timeout can be resolved against
  // the actually-deployed value (KERN-06). GOATOS_PG_QUERY_TIMEOUT keeps its explicit missing/nonliteral
  // handling (undefined = absent, null = nonliteral) that cadenceFindings depends on.
  if (!foundEnv) {
    allEnvMap = parseAllEnv(tfContent);
    const queryTimeoutValue = envValue(tfContent, "GOATOS_PG_QUERY_TIMEOUT");
    if (queryTimeoutValue === null) {
      allEnvMap.set("GOATOS_PG_QUERY_TIMEOUT", undefined);
    } else if (queryTimeoutValue.includes("env.") || queryTimeoutValue.includes("var.")) {
      allEnvMap.set("GOATOS_PG_QUERY_TIMEOUT", null);
    } else {
      allEnvMap.set("GOATOS_PG_QUERY_TIMEOUT", queryTimeoutValue);
    }
    foundEnv = true;
  }
}

const mainGoPath = resolve(repo, "backend/cmd/kernel-worker/main.go");
if (!existsSync(mainGoPath)) {
  errors.push("backend/cmd/kernel-worker/main.go: missing");
} else {
  errors.push(...cadenceFindings(readFileSync(mainGoPath, "utf8"), allEnvMap));
}

if (errors.length > 0) {
  console.error("worker-stage-budgets guard failed:");
  for (const e of errors) console.error(`- ${e}`);
  process.exit(1);
}
console.error(
  `worker-stage-budgets guard: batch limits meet the retired job budgets and outbox execution budget/cadence is compatible (${ENVS.length} environments checked)`,
);
