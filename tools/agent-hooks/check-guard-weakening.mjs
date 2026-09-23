#!/usr/bin/env node

// check-guard-weakening.mjs — meta-guard: a change that WEAKENS a guard must be
// loud. Tightening stays free.
//
// Why this exists (measured 2026-09-23, mutation audit of all 137 guard scripts):
//   - Deleting a guard outright — script + manifest entry + Makefile target +
//     run-local-ci.sh step — left check-guardrail-registration.mjs GREEN and
//     printing "140 guards registered ... all registered". All 136 surviving
//     guards were then run against that tree: NOT ONE noticed.
//   - Flipping `requiredInCI: true -> false` in tools/ci/guardrail-manifest.json
//     and deleting the matching `step` line from run-local-ci.sh: also GREEN.
//   - Replacing a real `selfTest` with `selfTestExemptReason: "n/a"`: also GREEN.
//   - `ratchet-guard.py --regenerate` after adding new debt admits the new
//     violation into the baseline; the WARNING is printed at regeneration time,
//     in the author's terminal, and the NEXT CI run is green with nothing
//     anywhere saying the baseline grew.
//   - A one-line `<guard>:ignore: owner=attacker issue=none scope=whatever
//     expiry=2020-01-01` comment silences a real, reproduced violation. 10 of
//     the 11 guards that accept such a marker validate only its FORMAT, never
//     the date, the owner, or the issue.
//
// Each of those is a way for an agent to make a guard stop protecting something
// while CI reports success. This guard makes every one of them fail closed.
//
// THE ESCAPE HATCH IS DELIBERATE AND REVIEWABLE. Weakening is sometimes right.
// It is never right SILENTLY. To land a weakening, add one line per finding to
// docs/observability/GUARD_WEAKENING_LEDGER.md in the SAME diff:
//
//   GUARD-WEAKENING-ACK: <finding id> owner=<name> reason=<one line>
//
// The finding id is printed by this guard. It is specific ("deleted-guard:
// tools/agent-hooks/check-idempotency-writes.mjs"), so an ACK cannot be a
// blanket waiver and it shows up as an added line in review.
//
// COST CONTRACT (docs/runbooks/local-ci.md — "relevant checks only"):
//   Diff-scoped, two stages, fail-OPEN on an undeterminable diff (same posture
//   as ci_tooling_changed in run-local-ci.sh — an unknown diff RUNS the check).
//     stage 1: one `git diff --name-only`. No guard-surface path and no source
//              path in the diff -> exit 0. A docs-only change pays one git call.
//     stage 2: only for the paths stage 1 matched: `git show base:<file>` for
//              the handful of manifest/baseline files that changed, and one
//              `git diff -U0` limited to those paths.
//   It NEVER scans the tree. It never reads a file the diff did not touch.
//
// Modes:
//   (default)     analyse the diff against the CI base; exit 1 on un-ACKed weakening.
//   --self-test   run the built-in adversarial fixtures against the pure
//                 analysis functions and exit. Milliseconds, no git, no tree.

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const LEDGER = "docs/observability/GUARD_WEAKENING_LEDGER.md";
const ACK_RE = /GUARD-WEAKENING-ACK:\s*(\S+)\s+owner=(\S+)\s+reason=(.+)$/;

// ── what counts as the guard surface ────────────────────────────────────────
const GUARD_SCRIPT_RE = /^tools\/(?:agent-hooks|ci)\/check-[\w.-]+\.(?:mjs|sh)$/;
const GUARD_SELFTEST_RE = /^tools\/(?:agent-hooks|ci)\/check-[\w.-]+\.test\.sh$/;
const MANIFEST = "tools/ci/guardrail-manifest.json";
const GUARD_INPUTS_MANIFEST = "tools/ci/guard-inputs.json";
const RUN_LOCAL_CI = "tools/ci/run-local-ci.sh";
const BASELINE_RE = /(?:baseline|allowlist|exception|ratchet)[\w.-]*\.(?:json|txt)$/i;
const SOURCE_RE = /\.(?:go|kt|kts|ts|tsx|js|mjs|sql|py|sh|yaml|yml)$/;

// Markers that silence a GOATOS guard wherever they appear in product code.
// Deliberately only the repo's own conventions: `<guard>:ignore:` (11 guards
// accept one; 10 of them validate the expiry's FORMAT and nothing else, so an
// expiry of 2020-01-01 silences a real violation) and the exception/telemetry
// exempt markers. Generic lint silencers (eslint-disable, @ts-expect-error,
// nolint, # noqa) are NOT listed: they do not switch off a guard in this repo,
// and flagging them would make this guard fire on correct code — which is worse
// than not checking at all (CONTRACT.md §3).
const SILENCER_RE = /(?:[\w.-]+:ignore:|exception:exempt|telemetry:exempt)/;

export function isGuardSurface(path) {
  return (
    GUARD_SCRIPT_RE.test(path) ||
    GUARD_SELFTEST_RE.test(path) ||
    path === MANIFEST ||
    path === GUARD_INPUTS_MANIFEST ||
    path === RUN_LOCAL_CI ||
    path === "Makefile" ||
    path === "tools/ci/ratchet-guard.py" ||
    BASELINE_RE.test(path)
  );
}

// ── pure analysis (all inputs injected so the self-test can attack it) ───────
//
// `input` shape:
//   {
//     changed:   [{ status: "A"|"M"|"D"|"R", path, oldPath? }],
//     before:    { [path]: string | null },   // content at the CI base
//     after:     { [path]: string | null },   // content in the working tree
//     addedLines:[{ path, line }],            // added lines, from git diff -U0
//     removedLines:[{ path, line }],
//   }
// Returns { findings: [{ id, message }], acks: [...] }

export function analyse(input) {
  const findings = [];
  const add = (id, message) => findings.push({ id, message });

  // W1 — a guard script disappeared (deleted, or renamed out of the guard namespace).
  for (const c of input.changed) {
    if (c.status === "D" && GUARD_SCRIPT_RE.test(c.path)) {
      add(`deleted-guard:${c.path}`, `guard script deleted: ${c.path}`);
    }
    if (c.status === "R" && GUARD_SCRIPT_RE.test(c.oldPath || "") && !GUARD_SCRIPT_RE.test(c.path)) {
      add(
        `renamed-guard-out:${c.oldPath}`,
        `guard script renamed out of the guard namespace: ${c.oldPath} -> ${c.path}`,
      );
    }
    if (c.status === "D" && GUARD_SELFTEST_RE.test(c.path)) {
      add(`deleted-guard-selftest:${c.path}`, `guard self-test harness deleted: ${c.path}`);
    }
  }

  // W2 — the guardrail manifest lost enforcement.
  const mBefore = parseJson(input.before?.[MANIFEST]);
  const mAfter = parseJson(input.after?.[MANIFEST]);
  if (mBefore && mAfter) {
    const byScript = (m) => new Map((m.guards || []).map((g) => [g.script, g]));
    const b = byScript(mBefore);
    const a = byScript(mAfter);
    if ((mAfter.guards || []).length < (mBefore.guards || []).length) {
      add(
        "manifest-shrank",
        `guardrail manifest shrank: ${(mBefore.guards || []).length} -> ${(mAfter.guards || []).length} guards`,
      );
    }
    for (const [script, before] of b) {
      const after = a.get(script);
      if (!after) {
        add(`manifest-entry-removed:${script}`, `guard removed from the manifest: ${script}`);
        continue;
      }
      if (before.requiredInCI === true && after.requiredInCI !== true) {
        add(
          `required-in-ci-downgraded:${script}`,
          `requiredInCI true -> ${JSON.stringify(after.requiredInCI)} for ${script}`,
        );
      }
      if (before.selfTest && !after.selfTest) {
        add(
          `selftest-removed:${script}`,
          `selfTest removed for ${script}` +
            (after.selfTestExemptReason ? " (replaced by selfTestExemptReason)" : ""),
        );
      }
      if (before.ciStep && !after.ciStep) {
        add(`cistep-removed:${script}`, `ciStep removed for ${script}`);
      }
    }
  }

  // W3 — a ratchet/baseline grew. Shrinking is free, by design: the whole point
  // of a ratchet is that paying debt down must never need paperwork.
  for (const c of input.changed) {
    if (!BASELINE_RE.test(c.path)) continue;
    if (c.status === "D") {
      add(`baseline-deleted:${c.path}`, `baseline deleted: ${c.path}`);
      continue;
    }
    const before = input.before?.[c.path];
    const after = input.after?.[c.path];
    if (before == null || after == null) continue;
    const grew = baselineGrowth(c.path, before, after);
    if (grew) add(`baseline-grew:${c.path}`, `${c.path}: ${grew}`);
  }

  // W4 — a new silencer marker landed in product source.
  for (const { path, line } of input.addedLines || []) {
    if (isGuardSurface(path)) continue; // guard-owned fixtures are not product silencers
    if (!SOURCE_RE.test(path)) continue;
    if (!SILENCER_RE.test(line)) continue;
    add(`new-silencer:${path}`, `new guard-silencing marker in ${path}: ${line.trim().slice(0, 140)}`);
  }

  // W5 — a numeric threshold in a guard script moved in the LOOSER direction.
  // For a ceiling (MAX_*, *_LIMIT, *_BUDGET) that is up. For a FLOOR (MIN_*,
  // *_FLOOR, *_MIN) it is DOWN: a floor is what stops a guard that has gone
  // blind from reporting a pass, so lowering one is exactly the move this guard
  // exists to catch. Getting that backwards would have left every floor added by
  // the 2026-09-23 audit trivially removable.
  for (const pair of pairThresholdChanges(input.removedLines || [], input.addedLines || [])) {
    const isFloor = /(?:^MIN_|_MIN$|_FLOOR$|^FLOOR_)/.test(pair.name);
    const looser = isFloor ? pair.after < pair.before : pair.after > pair.before;
    if (looser) {
      add(
        `threshold-loosened:${pair.path}:${pair.name}`,
        `${pair.path}: ${pair.name} ${isFloor ? "floor lowered" : "raised"} ${pair.before} -> ${pair.after}`,
      );
    }
  }

  // W6 — a CI step or a guardrails Makefile target stopped being invoked.
  for (const [file, re, kind] of [
    [RUN_LOCAL_CI, /^\s*(?:optional_)?step\s+"([^"]+)"/, "ci-step"],
    ["Makefile", /\$\(MAKE\)\s+([\w.-]*-guard[\w.-]*)|(?:^|\s)make\s+([\w.-]*-guard[\w.-]*)/, "make-target"],
  ]) {
    const before = input.before?.[file];
    const after = input.after?.[file];
    if (before == null || after == null) continue;
    const names = (text) => {
      const out = new Set();
      for (const line of String(text).split("\n")) {
        if (/^\s*#/.test(line)) continue; // a comment is not an invocation
        const m = line.match(re);
        if (m) out.add(m[1] || m[2]);
      }
      return out;
    };
    const nb = names(before);
    const na = names(after);
    for (const n of nb) {
      if (!na.has(n)) add(`invocation-removed:${file}:${n}`, `${kind} no longer invoked in ${file}: ${n}`);
    }
  }

  // W7 — the guard-input manifest lost entries (see check-guard-input-presence.mjs).
  const gBefore = parseJson(input.before?.[GUARD_INPUTS_MANIFEST]);
  const gAfter = parseJson(input.after?.[GUARD_INPUTS_MANIFEST]);
  if (gBefore && gAfter) {
    const count = (m) => (m.guards || []).reduce((n, g) => n + (g.inputs || []).length, 0);
    if (count(gAfter) < count(gBefore)) {
      add(
        "guard-inputs-shrank",
        `guard-input manifest shrank: ${count(gBefore)} -> ${count(gAfter)} declared inputs`,
      );
    }
  }

  const acks = collectAcks(input.addedLines || []);
  return { findings, acks };
}

function parseJson(text) {
  if (typeof text !== "string" || !text.trim()) return null;
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

// baselineGrowth: returns a human string when `after` allows MORE than `before`.
// Handles the two shapes in this repo: a count-keyed JSON ratchet
// ({entries: {"file|kind": n}, total: n}) and a line-per-violation .txt baseline.
export function baselineGrowth(path, before, after) {
  const jb = parseJson(before);
  const ja = parseJson(after);
  if (jb && ja && (jb.entries || ja.entries)) {
    const eb = jb.entries || {};
    const ea = ja.entries || {};
    const tb = Object.values(eb).reduce((a, b) => a + (Number(b) || 0), 0);
    const ta = Object.values(ea).reduce((a, b) => a + (Number(b) || 0), 0);
    const newKeys = Object.keys(ea).filter((k) => !(k in eb));
    const grownKeys = Object.keys(ea).filter((k) => k in eb && Number(ea[k]) > Number(eb[k]));
    if (ta > tb || newKeys.length || grownKeys.length) {
      const bits = [];
      if (ta > tb) bits.push(`total ${tb} -> ${ta}`);
      if (newKeys.length) bits.push(`new keys: ${newKeys.slice(0, 5).join(", ")}`);
      if (grownKeys.length) bits.push(`grown keys: ${grownKeys.slice(0, 5).join(", ")}`);
      return `ratchet baseline ADMITTED NEW DEBT (${bits.join("; ")})`;
    }
    return null;
  }
  // Line-shaped baseline/allowlist: any added non-comment line is a new exemption.
  const lines = (t) =>
    String(t)
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith("#"));
  const lb = new Set(lines(before));
  const added = lines(after).filter((l) => !lb.has(l));
  if (added.length) {
    return `${added.length} entry(ies) appended to the allowlist: ${added.slice(0, 3).join(" | ")}`;
  }
  return null;
}

// pairThresholdChanges: match `const NAME = <number>` removed/added in the same
// guard file and report the pairs. Deliberately narrow — a named numeric
// constant in a guard script is what a "just bump the limit" change looks like.
const THRESHOLD_RE = /(?:const|let|var)?\s*([A-Z][A-Z0-9_]{2,})\s*[=:]\s*(\d+(?:\.\d+)?)\s*[;,]?\s*$/;
export function pairThresholdChanges(removed, added) {
  const pick = (rows) => {
    const out = new Map();
    for (const { path, line } of rows) {
      if (!GUARD_SCRIPT_RE.test(path) && path !== "tools/ci/ratchet-guard.py") continue;
      const m = line.match(THRESHOLD_RE);
      if (m) out.set(`${path}|${m[1]}`, Number(m[2]));
    }
    return out;
  };
  const rb = pick(removed);
  const ra = pick(added);
  const pairs = [];
  for (const [key, before] of rb) {
    if (!ra.has(key)) continue;
    const [path, name] = key.split("|");
    pairs.push({ path, name, before, after: ra.get(key) });
  }
  return pairs;
}

export function collectAcks(addedLines) {
  const acks = [];
  for (const { line } of addedLines) {
    const m = line.match(ACK_RE);
    if (m) acks.push({ id: m[1], owner: m[2], reason: m[3].trim() });
  }
  return acks;
}

// judge: a finding is cleared only by an ACK naming its exact id.
export function judge({ findings, acks }) {
  const ackIds = new Set(acks.map((a) => a.id));
  const unacked = findings.filter((f) => !ackIds.has(f.id));
  const acked = findings.filter((f) => ackIds.has(f.id));
  return { unacked, acked };
}

// ── git plumbing ────────────────────────────────────────────────────────────
function git(args, opts = {}) {
  return execFileSync("git", args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024, ...opts });
}

function resolveBase() {
  const requested = process.env.GOATOS_CI_BASE || "origin/main";
  try {
    git(["rev-parse", "--verify", `${requested}^{commit}`], { stdio: ["ignore", "pipe", "ignore"] });
    return { ref: requested, fallback: false };
  } catch {
    try {
      git(["rev-parse", "--verify", "HEAD~1^{commit}"], { stdio: ["ignore", "pipe", "ignore"] });
      return { ref: "HEAD~1", fallback: true };
    } catch {
      return { ref: null, fallback: true };
    }
  }
}

function changedFiles(base) {
  // Committed diff vs base, plus staged, plus unstaged — same three sources as
  // changed_since_base() in run-local-ci.sh, so this guard and the job scoping
  // can never disagree about what "changed" means. `stages` records which of the
  // three actually produced anything, so the line-level diff below pays only for
  // those (a clean working tree is the common case and costs nothing extra).
  const rows = [];
  const stages = { committed: false, staged: false, unstaged: false };
  const parseRows = (out) => {
    let any = false;
    for (const rec of out.split("\n")) {
      if (!rec.trim()) continue;
      any = true;
      const parts = rec.split("\t");
      const status = parts[0][0];
      if (status === "R") rows.push({ status: "R", oldPath: parts[1], path: parts[2] });
      else rows.push({ status, path: parts[1] });
    }
    return any;
  };
  for (const [key, args] of [
    ["committed", base ? ["diff", "--name-status", "--diff-filter=ACMRD", `${base}...HEAD`] : null],
    ["staged", ["diff", "--name-status", "--diff-filter=ACMRD", "--cached"]],
    ["unstaged", ["diff", "--name-status", "--diff-filter=ACMRD"]],
  ]) {
    if (!args) continue;
    try {
      stages[key] = parseRows(git(args));
    } catch {
      /* a source that cannot be read contributes nothing; the others still count */
    }
  }
  const seen = new Map();
  for (const r of rows) seen.set(r.path, r);
  const out = [...seen.values()];
  out.stages = stages;
  return out;
}

// A path that does not exist at the base is simply new; git writes a `fatal:`
// line to stderr for it, which is noise in a green CI log.
function showAt(rev, path) {
  try {
    return git(["show", `${rev}:${path}`], { stdio: ["ignore", "pipe", "ignore"] });
  } catch {
    return null;
  }
}

// The "after" side is what CI will actually run against: the file on disk.
// Reading `git show :<path>` (the index) instead was a real defeat found while
// testing this guard — an UNSTAGED manifest edit made `after` identical to HEAD,
// so requiredInCI:true -> false compared clean and the guard passed.
function readWorking(path) {
  try {
    return readFileSync(path, "utf8");
  } catch {
    try {
      return git(["show", `:${path}`]);
    } catch {
      return null;
    }
  }
}

function diffLines(base, paths, stages = { committed: true, staged: true, unstaged: true }) {
  const added = [];
  const removed = [];
  if (!paths.length) return { added, removed };
  const collect = (out) => {
    let file = null;
    for (const line of out.split("\n")) {
      if (line.startsWith("+++ b/")) {
        file = line.slice(6);
        continue;
      }
      if (line.startsWith("--- ") || line.startsWith("diff --git") || line.startsWith("@@")) continue;
      if (!file) continue;
      if (line.startsWith("+")) added.push({ path: file, line: line.slice(1) });
      else if (line.startsWith("-")) removed.push({ path: file, line: line.slice(1) });
    }
  };
  for (const [enabled, args] of [
    [stages.committed && !!base, base ? ["diff", "-U0", `${base}...HEAD`, "--"] : null],
    [stages.staged, ["diff", "-U0", "--cached", "--"]],
    [stages.unstaged, ["diff", "-U0", "--"]],
  ]) {
    if (!enabled || !args) continue;
    try {
      collect(git([...args, ...paths]));
    } catch {
      /* ignore */
    }
  }
  return { added, removed };
}

// ── self-test ───────────────────────────────────────────────────────────────
// Every case plants a REAL weakening shape and asserts this guard reports it.
// Pure functions, no git, no tree: the whole suite is milliseconds.
function selfTest() {
  let failed = 0;
  const check = (name, cond, detail = "") => {
    if (cond) {
      console.log(`  ok   ${name}`);
    } else {
      failed += 1;
      console.log(`  FAIL ${name} ${detail}`);
    }
  };
  const fires = (input, id) => {
    const { findings, acks } = analyse(input);
    const { unacked } = judge({ findings, acks });
    return unacked.some((f) => f.id === id);
  };

  const manifest = (over = {}) =>
    JSON.stringify({
      guards: [
        {
          script: "tools/agent-hooks/check-money.mjs",
          requiredInCI: true,
          selfTest: "node tools/agent-hooks/check-money.mjs --self-test",
          ciStep: "money-guard",
          ...over,
        },
        { script: "tools/agent-hooks/check-other.mjs", requiredInCI: true },
      ],
    });

  // W1 deleted guard
  check(
    "deleted guard script fires",
    fires(
      { changed: [{ status: "D", path: "tools/agent-hooks/check-money.mjs" }], before: {}, after: {}, addedLines: [] },
      "deleted-guard:tools/agent-hooks/check-money.mjs",
    ),
  );
  // W1 ACKed deletion passes
  check(
    "ACKed deletion clears",
    !fires(
      {
        changed: [{ status: "D", path: "tools/agent-hooks/check-money.mjs" }],
        before: {},
        after: {},
        addedLines: [
          {
            path: LEDGER,
            line: "GUARD-WEAKENING-ACK: deleted-guard:tools/agent-hooks/check-money.mjs owner=ravi reason=superseded by check-money-v2",
          },
        ],
      },
      "deleted-guard:tools/agent-hooks/check-money.mjs",
    ),
  );
  // W1 an ACK for a DIFFERENT id does not clear this one
  check(
    "mismatched ACK does not clear",
    fires(
      {
        changed: [{ status: "D", path: "tools/agent-hooks/check-money.mjs" }],
        before: {},
        after: {},
        addedLines: [{ path: LEDGER, line: "GUARD-WEAKENING-ACK: deleted-guard:tools/agent-hooks/check-other.mjs owner=x reason=y" }],
      },
      "deleted-guard:tools/agent-hooks/check-money.mjs",
    ),
  );
  // W2 manifest entry removed
  check(
    "manifest entry removal fires",
    fires(
      {
        changed: [{ status: "M", path: MANIFEST }],
        before: { [MANIFEST]: manifest() },
        after: { [MANIFEST]: JSON.stringify({ guards: [{ script: "tools/agent-hooks/check-other.mjs", requiredInCI: true }] }) },
        addedLines: [],
      },
      "manifest-entry-removed:tools/agent-hooks/check-money.mjs",
    ),
  );
  // W2 requiredInCI downgrade
  check(
    "requiredInCI downgrade fires",
    fires(
      {
        changed: [{ status: "M", path: MANIFEST }],
        before: { [MANIFEST]: manifest() },
        after: { [MANIFEST]: manifest({ requiredInCI: false }) },
        addedLines: [],
      },
      "required-in-ci-downgraded:tools/agent-hooks/check-money.mjs",
    ),
  );
  // W2 selfTest swapped for an exemption
  check(
    "selfTest -> selfTestExemptReason fires",
    fires(
      {
        changed: [{ status: "M", path: MANIFEST }],
        before: { [MANIFEST]: manifest() },
        after: {
          [MANIFEST]: JSON.stringify({
            guards: [
              { script: "tools/agent-hooks/check-money.mjs", requiredInCI: true, ciStep: "money-guard", selfTestExemptReason: "n/a" },
              { script: "tools/agent-hooks/check-other.mjs", requiredInCI: true },
            ],
          }),
        },
        addedLines: [],
      },
      "selftest-removed:tools/agent-hooks/check-money.mjs",
    ),
  );
  // W3 ratchet baseline admitting new debt (the --regenerate laundering path)
  const base63 = JSON.stringify({ total: 63, entries: { "a.go|discarded_err": 1, "b.go|swallowed_err": 62 } });
  const base64new = JSON.stringify({ total: 64, entries: { "a.go|discarded_err": 1, "b.go|swallowed_err": 62, "new.go|discarded_err": 1 } });
  const base64grown = JSON.stringify({ total: 64, entries: { "a.go|discarded_err": 2, "b.go|swallowed_err": 62 } });
  const base62 = JSON.stringify({ total: 62, entries: { "b.go|swallowed_err": 62 } });
  check(
    "ratchet baseline new key fires",
    fires(
      {
        changed: [{ status: "M", path: "tools/exception-guard/baseline.json" }],
        before: { "tools/exception-guard/baseline.json": base63 },
        after: { "tools/exception-guard/baseline.json": base64new },
        addedLines: [],
      },
      "baseline-grew:tools/exception-guard/baseline.json",
    ),
  );
  check(
    "ratchet baseline grown count fires",
    fires(
      {
        changed: [{ status: "M", path: "tools/exception-guard/baseline.json" }],
        before: { "tools/exception-guard/baseline.json": base63 },
        after: { "tools/exception-guard/baseline.json": base64grown },
        addedLines: [],
      },
      "baseline-grew:tools/exception-guard/baseline.json",
    ),
  );
  // TIGHTENING MUST STAY FREE — this is the case that keeps the guard usable.
  check(
    "ratchet baseline SHRINK is silent",
    !fires(
      {
        changed: [{ status: "M", path: "tools/exception-guard/baseline.json" }],
        before: { "tools/exception-guard/baseline.json": base63 },
        after: { "tools/exception-guard/baseline.json": base62 },
        addedLines: [],
      },
      "baseline-grew:tools/exception-guard/baseline.json",
    ),
  );
  // line-shaped allowlist
  check(
    "appended allowlist line fires",
    fires(
      {
        changed: [{ status: "M", path: "tools/agent-hooks/idempotency-writes-baseline.txt" }],
        before: { "tools/agent-hooks/idempotency-writes-baseline.txt": "# header\nbackend/a.go\n" },
        after: { "tools/agent-hooks/idempotency-writes-baseline.txt": "# header\nbackend/a.go\nbackend/my-new-violation.go\n" },
        addedLines: [],
      },
      "baseline-grew:tools/agent-hooks/idempotency-writes-baseline.txt",
    ),
  );
  check(
    "removed allowlist line is silent",
    !fires(
      {
        changed: [{ status: "M", path: "tools/agent-hooks/idempotency-writes-baseline.txt" }],
        before: { "tools/agent-hooks/idempotency-writes-baseline.txt": "# header\nbackend/a.go\nbackend/b.go\n" },
        after: { "tools/agent-hooks/idempotency-writes-baseline.txt": "# header\nbackend/a.go\n" },
        addedLines: [],
      },
      "baseline-grew:tools/agent-hooks/idempotency-writes-baseline.txt",
    ),
  );
  // W4 new silencer marker in product source (the expired-expiry attack)
  check(
    "new :ignore: marker in product source fires",
    fires(
      {
        changed: [{ status: "M", path: "apps/admin-web/components/mesha-shell.tsx" }],
        before: {},
        after: {},
        addedLines: [
          {
            path: "apps/admin-web/components/mesha-shell.tsx",
            line: 'const label = shed + "whole"; // operational-location:ignore: owner=attacker issue=none scope=whatever expiry=2020-01-01',
          },
        ],
      },
      "new-silencer:apps/admin-web/components/mesha-shell.tsx",
    ),
  );
  check(
    "ordinary source line does not fire",
    !fires(
      {
        changed: [{ status: "M", path: "apps/admin-web/components/mesha-shell.tsx" }],
        before: {},
        after: {},
        addedLines: [{ path: "apps/admin-web/components/mesha-shell.tsx", line: "const label = shed.displayName;" }],
      },
      "new-silencer:apps/admin-web/components/mesha-shell.tsx",
    ),
  );
  // W5 threshold raised / lowered
  check(
    "raised threshold in a guard fires",
    fires(
      {
        changed: [{ status: "M", path: "tools/agent-hooks/check-money.mjs" }],
        before: {},
        after: {},
        removedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MAX_DRIFT = 5;" }],
        addedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MAX_DRIFT = 500;" }],
      },
      "threshold-loosened:tools/agent-hooks/check-money.mjs:MAX_DRIFT",
    ),
  );
  check(
    "lowered threshold is silent",
    !fires(
      {
        changed: [{ status: "M", path: "tools/agent-hooks/check-money.mjs" }],
        before: {},
        after: {},
        removedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MAX_DRIFT = 500;" }],
        addedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MAX_DRIFT = 5;" }],
      },
      "threshold-loosened:tools/agent-hooks/check-money.mjs:MAX_DRIFT",
    ),
  );
  check(
    "lowered FLOOR fires (a floor is loosened by going down)",
    fires(
      {
        changed: [{ status: "M", path: "tools/agent-hooks/check-money.mjs" }],
        before: {},
        after: {},
        removedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MIN_CALLSITES = 8;" }],
        addedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MIN_CALLSITES = 0;" }],
      },
      "threshold-loosened:tools/agent-hooks/check-money.mjs:MIN_CALLSITES",
    ),
  );
  check(
    "raised FLOOR is silent (tightening)",
    !fires(
      {
        changed: [{ status: "M", path: "tools/agent-hooks/check-money.mjs" }],
        before: {},
        after: {},
        removedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MIN_CALLSITES = 8;" }],
        addedLines: [{ path: "tools/agent-hooks/check-money.mjs", line: "const MIN_CALLSITES = 12;" }],
      },
      "threshold-loosened:tools/agent-hooks/check-money.mjs:MIN_CALLSITES",
    ),
  );
  // W6 CI step removed
  check(
    "removed ci step fires",
    fires(
      {
        changed: [{ status: "M", path: RUN_LOCAL_CI }],
        before: { [RUN_LOCAL_CI]: 'run_common() {\n  step "money-guard" make money-guard\n  step "other" make other\n}\n' },
        after: { [RUN_LOCAL_CI]: 'run_common() {\n  step "other" make other\n}\n' },
        addedLines: [],
      },
      "invocation-removed:tools/ci/run-local-ci.sh:money-guard",
    ),
  );
  check(
    "commenting a step out still fires",
    fires(
      {
        changed: [{ status: "M", path: RUN_LOCAL_CI }],
        before: { [RUN_LOCAL_CI]: 'run_common() {\n  step "money-guard" make money-guard\n}\n' },
        after: { [RUN_LOCAL_CI]: 'run_common() {\n  # step "money-guard" make money-guard\n}\n' },
        addedLines: [],
      },
      "invocation-removed:tools/ci/run-local-ci.sh:money-guard",
    ),
  );
  // W7 guard-input manifest shrank
  check(
    "guard-input manifest shrink fires",
    fires(
      {
        changed: [{ status: "M", path: GUARD_INPUTS_MANIFEST }],
        before: { [GUARD_INPUTS_MANIFEST]: JSON.stringify({ guards: [{ guard: "g", inputs: ["a", "b"] }] }) },
        after: { [GUARD_INPUTS_MANIFEST]: JSON.stringify({ guards: [{ guard: "g", inputs: ["a"] }] }) },
        addedLines: [],
      },
      "guard-inputs-shrank",
    ),
  );
  // scope: a docs-only path is not guard surface
  check("docs path is not guard surface", !isGuardSurface("docs/progress/whatever.md"));
  check("guard script is guard surface", isGuardSurface("tools/agent-hooks/check-money.mjs"));
  check("manifest is guard surface", isGuardSurface(MANIFEST));

  console.log(failed === 0 ? "guard-weakening self-test: ok" : `guard-weakening self-test: ${failed} FAILED`);
  return failed === 0 ? 0 : 1;
}

// ── main ────────────────────────────────────────────────────────────────────
function main() {
  if (process.argv.includes("--self-test")) return selfTest();

  const { ref, fallback } = resolveBase();
  if (fallback) {
    console.log(
      "guard-weakening: base ref unresolvable, falling back" + (ref ? ` to ${ref}` : " to a working-tree-only diff"),
    );
  }

  // stage 1 — one git call. Nothing relevant in the diff, nothing to do.
  const changed = changedFiles(ref);
  const surface = changed.filter((c) => isGuardSurface(c.path) || isGuardSurface(c.oldPath || ""));
  const sources = changed.filter((c) => SOURCE_RE.test(c.path) && !isGuardSurface(c.path));
  if (!surface.length && !sources.length) {
    console.log("guard-weakening: ok — no guard script, manifest, baseline or source file in the diff");
    return 0;
  }

  // stage 2 — read ONLY what the diff touched.
  // Only the files whose CONTENT the rules compare. A guard script's deletion is
  // read from its status and its threshold changes from the line diff, so there
  // is no reason to spawn a `git show` per guard script: on a branch touching a
  // dozen of them that was ~850 ms of process spawns for content nothing read.
  const needsContent = (path) =>
    path === MANIFEST ||
    path === GUARD_INPUTS_MANIFEST ||
    path === RUN_LOCAL_CI ||
    path === "Makefile" ||
    BASELINE_RE.test(path);
  const before = {};
  const after = {};
  for (const c of surface) {
    if (c.status === "D" || !needsContent(c.path)) continue;
    before[c.path] = ref ? showAt(ref, c.path) : null;
    after[c.path] = readWorking(c.path);
  }
  const { added, removed } = diffLines(
    ref,
    [...new Set([...surface.map((c) => c.path), ...sources.map((c) => c.path), LEDGER])],
    changed.stages,
  );

  const { findings, acks } = analyse({ changed, before, after, addedLines: added, removedLines: removed });
  const { unacked, acked } = judge({ findings, acks });

  for (const f of acked) console.log(`guard-weakening: ACKed — ${f.message}`);
  if (!unacked.length) {
    console.log(`guard-weakening: ok — ${changed.length} changed file(s), no un-ACKed weakening`);
    return 0;
  }
  console.error("guard-weakening: BLOCKED — this diff makes a guard protect less:");
  for (const f of unacked) {
    console.error(`  - ${f.message}`);
    console.error(`    ack with: GUARD-WEAKENING-ACK: ${f.id} owner=<you> reason=<why this is right>`);
  }
  console.error(`  Add the ACK line(s) to ${LEDGER} in this same diff, or undo the weakening.`);
  console.error("  Tightening a guard, shrinking a ratchet, or deleting an exemption never needs an ACK.");
  return 1;
}

if (import.meta.url === `file://${process.argv[1]}`) process.exit(main());
