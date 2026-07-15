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

// Parses each supervisor.RegisterCadence[WithTimeout]("name", interval, ...) call
// into { name, intervalSeconds, body } where body is the source from that call up
// to the next RegisterCadence (so it contains that cadence's stage constructors).
function parseCadences(mainGo) {
  const re = /supervisor\.RegisterCadence(?:WithTimeout)?\(\s*"([^"]+)"\s*,\s*([0-9]+\s*\*\s*time\.(?:Second|Minute|Hour))/g;
  const matches = [...mainGo.matchAll(re)];
  return matches.map((m, i) => ({
    name: m[1],
    intervalSeconds: durationToSeconds(m[2]),
    body: mainGo.slice(m.index, i + 1 < matches.length ? matches[i + 1].index : mainGo.length),
  }));
}

function cadenceFindings(mainGo) {
  const findings = [];
  const cadences = parseCadences(mainGo);
  const outbox = cadences.find((c) => c.body.includes("NewOutboxRelayStage"));
  const notify = cadences.find((c) => c.body.includes("NewNotificationDispatcherStage"));

  if (!outbox) {
    findings.push("backend/cmd/kernel-worker/main.go: NewOutboxRelayStage is not registered on any cadence");
  } else {
    const effective = Math.floor(outbox.intervalSeconds * 0.9); // supervisor caps at 90% of interval
    if (effective < ACCEPTED_OUTBOX_BUDGET_SECONDS) {
      findings.push(
        `backend/cmd/kernel-worker/main.go: outbox cadence "${outbox.name}" interval ${outbox.intervalSeconds}s gives an effective per-run budget of ${effective}s, below the accepted ${ACCEPTED_OUTBOX_BUDGET_SECONDS}s outbox budget`,
      );
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
  const goodTf = `env { name = "GOATOS_OUTBOX_LIMIT" value = "500" } env { name = "GOATOS_NOTIFICATION_LIMIT" value = "100" }`;
  const goodMain = `
  supervisor.RegisterCadence("outbox", 1*time.Minute,
    kernelstages.NewOutboxRelayStage(deps),
  )
  supervisor.RegisterCadence("notify", 1*time.Minute,
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;

  if (limitFindings("x", goodTf).length !== 0 || cadenceFindings(goodMain).length !== 0) {
    throw new Error("self-test: a compliant config produced findings");
  }
  // 1) outbox limit below 500.
  if (!limitFindings("x", goodTf.replace('"500"', '"400"')).some((f) => f.includes("GOATOS_OUTBOX_LIMIT"))) {
    throw new Error("self-test: missed outbox limit below 500");
  }
  // 2) notification limit below 100.
  if (!limitFindings("x", goodTf.replace('"100"', '"50"')).some((f) => f.includes("GOATOS_NOTIFICATION_LIMIT"))) {
    throw new Error("self-test: missed notification limit below 100");
  }
  // 3) outbox timeout below the accepted budget (30s cadence -> 27s effective).
  const shortCadence = goodMain.replace('"outbox", 1*time.Minute', '"outbox", 30*time.Second');
  if (!cadenceFindings(shortCadence).some((f) => f.includes("below the accepted"))) {
    throw new Error("self-test: missed outbox cadence with sub-budget effective timeout");
  }
  // 4) incompatible combination: outbox + notification on the SAME cadence.
  const shared = `
  supervisor.RegisterCadence("fast", 1*time.Minute,
    kernelstages.NewOutboxRelayStage(deps),
    kernelstages.NewNotificationDispatcherStage(deps, tenantID),
  )`;
  if (!cadenceFindings(shared).some((f) => f.includes("share cadence"))) {
    throw new Error("self-test: missed outbox+notification sharing one cadence");
  }
  console.error("worker-stage-budgets guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const errors = [];
for (const env of ENVS) {
  const path = resolve(repo, "infra/envs", env, "cloud_run_worker.tf");
  if (!existsSync(path)) {
    errors.push(`infra/envs/${env}/cloud_run_worker.tf: missing (kernel-worker service not defined)`);
    continue;
  }
  errors.push(...limitFindings(env, readFileSync(path, "utf8")));
}
const mainGoPath = resolve(repo, "backend/cmd/kernel-worker/main.go");
if (!existsSync(mainGoPath)) {
  errors.push("backend/cmd/kernel-worker/main.go: missing");
} else {
  errors.push(...cadenceFindings(readFileSync(mainGoPath, "utf8")));
}

if (errors.length > 0) {
  console.error("worker-stage-budgets guard failed:");
  for (const e of errors) console.error(`- ${e}`);
  process.exit(1);
}
console.error(
  `worker-stage-budgets guard: batch limits meet the retired job budgets and outbox execution budget/cadence is compatible (${ENVS.length} environments checked)`,
);
