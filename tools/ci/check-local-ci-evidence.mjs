#!/usr/bin/env node
// check-local-ci-evidence.mjs — exact-SHA local-CI push gate.
//
// Governance invariant (docs/runbooks/local-release-evidence.md): a push to `main` is only
// authorized by a green `make ci-local` on the EXACT commit being pushed. The receipt is
// either FULL, or SCOPED to the exact remote-main base with every classifier-selected job.
// Explicit partial `JOB=...` runs never authorize a main push.
//
// Machinery:
//   tools/ci/run-local-ci.sh, after an auto-scoped or forced-full run ends GREEN, calls
//     node tools/ci/check-local-ci-evidence.mjs --record <sha> --mode <all|scoped> ...
//   which writes a SHA-bound receipt to the worktree git dir (never committed).
//   The installed pre-push hook calls
//     node tools/ci/check-local-ci-evidence.mjs --pre-push   (git push payload on stdin)
//   which blocks any update to refs/heads/main whose local SHA is not based on the
//   current remote main or has no matching green receipt. Claude/Codex agent hooks
//   also block direct main-push commands and route agents through `make land-main`.
//
// Bypass: set GOATOS_BYPASS_LOCAL_CI=1 for an explicit one-command bypass of the
// local-CI evidence gate. This does not bypass the separate staging promotion guard.
//
// Modes: --record <sha> | --verify | --pre-push | --agent-hook | --self-test
// Deterministic, offline. No network.

import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, unlinkSync, writeFileSync } from "node:fs";

const MAIN_REF = "refs/heads/main";
const ZERO_SHA = "0000000000000000000000000000000000000000";
const RECEIPT_NAME = "goatos-ci-local-receipt.json";

function receiptPath() {
  // --git-path resolves per-worktree (worktree-safe), keeping the receipt local & uncommitted.
  return execFileSync("git", ["rev-parse", "--git-path", RECEIPT_NAME]).toString("utf8").trim();
}

function readReceipt() {
  try {
    return JSON.parse(readFileSync(receiptPath(), "utf8"));
  } catch {
    return null;
  }
}

function argValue(args, name, fallback = undefined) {
  const index = args.indexOf(name);
  return index >= 0 ? args[index + 1] : fallback;
}

function normalizedJobs(value) {
  const jobs = Array.isArray(value) ? value : String(value || "").split(",");
  return [...new Set(jobs.map((job) => job.trim()).filter(Boolean))].sort();
}

function currentRulesHash() {
  return createHash("sha256").update(readFileSync("tools/ci/component-paths.json")).digest("hex");
}

function computeMainFreshness({ localSha, remoteSha }) {
  if (!remoteSha || remoteSha === ZERO_SHA) return { ok: true };
  try {
    execFileSync("git", ["merge-base", "--is-ancestor", remoteSha, localSha], { stdio: "ignore" });
    return { ok: true };
  } catch {
    return {
      ok: false,
      reason: `candidate ${localSha.slice(0, 12)} is not based on current remote main ${remoteSha.slice(0, 12)}; run \`make land-main\``,
    };
  }
}

function unquote(token) {
  return token.replace(/^["']+|["',)]+$/g, "");
}

function isMainDestination(token) {
  const value = unquote(token);
  return (
    value === "main" ||
    value === MAIN_REF ||
    value.endsWith(":main") ||
    value.endsWith(`:${MAIN_REF}`)
  );
}

function localCiBypassEnabled() {
  return process.env.GOATOS_BYPASS_LOCAL_CI === "1";
}

function commandHasLocalCiBypass(command) {
  return /\bGOATOS_BYPASS_LOCAL_CI=1\b/.test(command);
}

export function commandAttemptsDirectMainPush(command) {
  if (typeof command !== "string" || !command.trim()) return false;
  const matcher =
    /\bgit(?:\s+(?:-c|-C)\s+\S+|\s+--(?:git-dir|work-tree|namespace)(?:=\S+|\s+\S+)|\s+--(?:bare|no-pager|literal-pathspecs|glob-pathspecs|noglob-pathspecs|icase-pathspecs))*\s+(mesha-push|push)\b([^;&|\n]*)/g;
  for (const match of command.matchAll(matcher)) {
    const verb = match[1];
    const args = match[2].trim().split(/\s+/).filter(Boolean);
    if (args.some(isMainDestination)) return true;
    if (verb === "mesha-push" && args.length === 0) return true; // alias defaults to main
    if (verb === "push") {
      const positional = args.filter((arg) => !arg.startsWith("-"));
      if (positional.length < 2) return true; // implicit current/upstream ref may be main
    }
  }
  return false;
}

function commandFromHookPayload(raw) {
  let payload;
  try {
    payload = JSON.parse(raw || "{}");
  } catch {
    return raw || "";
  }
  const input = payload?.tool_input ?? payload?.input ?? payload?.arguments ?? {};
  if (typeof input === "string") return input;
  if (input && typeof input === "object") {
    const command = input.command ?? input.cmd ?? input.argv ?? input.source;
    if (Array.isArray(command)) return command.join(" ");
    if (typeof command === "string") return command;
  }
  const command = payload?.command ?? payload?.cmd ?? payload?.argv;
  if (Array.isArray(command)) return command.join(" ");
  if (typeof command === "string") return command;
  return raw || "";
}

// EVERY receipt — `all` as well as `scoped` — records the diff base it was
// computed against, and that base must be a genuine ancestor of (or equal to)
// the remote main being pushed to.
//
// WHY `all` NEEDS THIS TOO. `mode:"all"` says "every job ran"; it does NOT say
// "every job was given the real diff". run-local-ci.sh derives BOTH the job
// scope and the Android UI-diff detector from the base, so a base that is not
// behind remote main (e.g. GOATOS_CI_BASE=HEAD, or the HEAD~1 fallback on a bad
// network) yields an EMPTY diff: the UI-diff detector sees nothing, the
// "skipped-with-ui-diff" banner never fires, and a genuinely green mode:"all"
// receipt records screenshots:"skipped" while hiding a real UI change.
// A base at-or-behind remote main can only ever OVER-detect, which is safe.
function computeBaseAncestry({ receipt, remoteSha }) {
  const base = receipt?.base;
  if (!base || !/^[0-9a-f]{40}$/.test(String(base))) {
    return {
      ok: false,
      reason: `local-CI receipt records no diff base (base=${String(base ?? "missing")}); re-run \`make ci-local\` so the receipt can be validated against remote main`,
    };
  }
  if (!remoteSha || remoteSha === ZERO_SHA) return { ok: true }; // creating main
  if (base === remoteSha) return { ok: true };
  try {
    execFileSync("git", ["merge-base", "--is-ancestor", base, remoteSha], { stdio: "ignore" });
    return { ok: true };
  } catch {
    return {
      ok: false,
      reason: `local-CI receipt was computed against base ${String(base).slice(0, 12)}, which is not an ancestor of remote main ${remoteSha.slice(0, 12)}; that run diffed against the wrong base (GOATOS_CI_BASE spoof or a HEAD~1 fallback) and may have skipped proofs — re-run \`make ci-local\` with a real base`,
    };
  }
}

function computeScopedCoverage({ localSha, remoteSha, receipt }) {
  try {
    if (!receipt.base || receipt.base !== remoteSha) {
      return { ok: false, reason: `scoped receipt base ${String(receipt.base).slice(0, 12)} does not match remote main ${String(remoteSha).slice(0, 12)}` };
    }
    if (receipt.rulesHash !== currentRulesHash()) {
      return { ok: false, reason: "component path rules changed after the scoped receipt was recorded" };
    }
    const result = JSON.parse(execFileSync(
      process.execPath,
      ["tools/ci/ci-scope.mjs", "--base", receipt.base, "--head", localSha, "--format", "json"],
      { encoding: "utf8" },
    ));
    if (result.full) return { ok: false, reason: "the current diff requires the full CI suite" };
    const expected = normalizedJobs(result.selectedJobs);
    const recorded = normalizedJobs(receipt.jobs);
    if (JSON.stringify(expected) !== JSON.stringify(recorded)) {
      return { ok: false, reason: `scoped receipt jobs ${recorded.join(",")} do not cover required jobs ${expected.join(",")}` };
    }
    return { ok: true };
  } catch (error) {
    return { ok: false, reason: `could not revalidate scoped CI coverage: ${error.message}` };
  }
}

// Screenshot coverage is EVIDENCE, not decoration. `skipped` is the authorized
// default (Paparazzi is opt-in). `skipped-with-ui-diff` means the run itself
// detected an Android UI/snapshot diff and still did not prove it — that receipt
// does not authorize main. Clear it with `make ci-local-screenshots`.
const SCREENSHOT_BLOCKING = "skipped-with-ui-diff";
const SCREENSHOT_ALLOWED = new Set(["yes", "skipped", "not-applicable"]);

function receiptCoversAndroid(receipt) {
  return receipt.mode === "all" || normalizedJobs(receipt.jobs).includes("android");
}

// Pure: given the git push payload lines and the current receipt, decide whether the push is
// blocked. Only pushes that UPDATE refs/heads/main are gated; deletes and other refs pass.
export function evaluatePush({
  pushLines,
  receipt,
  scopedCoverage = () => ({ ok: false, reason: "scoped coverage was not revalidated" }),
  baseAncestry = () => ({ ok: false, reason: "the receipt's diff base was not validated against remote main" }),
  mainFreshness = () => ({ ok: true }),
}) {
  const reasons = [];
  for (const raw of pushLines) {
    const line = raw.trim();
    if (!line) continue;
    const [localRef, localSha, remoteRef, remoteSha] = line.split(/\s+/);
    if (remoteRef !== MAIN_REF) continue; // only gate main
    if (localSha === ZERO_SHA) continue; // branch delete — not a content push
    const freshness = mainFreshness({ localSha, remoteSha });
    if (!freshness.ok) reasons.push(freshness.reason);
    if (!receipt) {
      reasons.push(`no local-CI receipt found for ${localSha.slice(0, 12)}; run \`make land-main\``);
      continue;
    }
    if (receipt.result !== "green") {
      reasons.push(`local-CI receipt is "${receipt.result}", not green (sha ${String(receipt.sha).slice(0, 12)})`);
      continue;
    }
    if (receipt.sha !== localSha) {
      reasons.push(`local-CI receipt is for ${String(receipt.sha).slice(0, 12)} but you are pushing ${localSha.slice(0, 12)}; re-run \`make ci-local\` on the exact commit`);
      continue;
    }
    // Base-provenance gate — like the screenshot gate, checked BEFORE the mode
    // branches so it applies to `all` and `scoped` receipts alike. A receipt
    // whose base is not behind remote main cannot authorize main, whatever its
    // mode: it proves nothing about the diff that is actually being pushed.
    const ancestry = baseAncestry({ localSha, remoteSha, receipt });
    if (!ancestry.ok) reasons.push(ancestry.reason);
    // Screenshot evidence gate — checked BEFORE the mode branches so it applies
    // to `all` and `scoped` receipts alike, and to receipts carried through
    // --reuse-after-rebase (which forwards the value verbatim by design).
    if (receipt.screenshots === SCREENSHOT_BLOCKING) {
      reasons.push(
        `local-CI receipt records screenshots="${SCREENSHOT_BLOCKING}": this diff touches Android UI/snapshots and the Paparazzi proof did not run; run \`make ci-local-screenshots\``,
      );
    } else if (!SCREENSHOT_ALLOWED.has(receipt.screenshots) && receiptCoversAndroid(receipt)) {
      reasons.push(
        `local-CI receipt covers the android job but records screenshots="${receipt.screenshots ?? "unknown"}"; re-run \`make ci-local\` to record screenshot coverage`,
      );
    }
    if (receipt.mode === "scoped") {
      const coverage = scopedCoverage({ localSha, remoteSha, receipt });
      if (!coverage.ok) reasons.push(coverage.reason);
      continue;
    }
    if (receipt.mode !== "all") reasons.push(`local-CI receipt mode="${receipt.mode}" is not an auto-scoped or full run`);
  }
  return { blocked: reasons.length > 0, reasons };
}

const SCREENSHOT_STATES = ["yes", "skipped", "skipped-with-ui-diff", "not-applicable"];

function record(sha, { mode, base, jobs, screenshots }) {
  // run-local-ci.sh's reachability trace mode executes nothing. It already exits
  // before the receipt block; this is the second lock on that door.
  if (["1", "true", "TRUE", "True"].includes(process.env.GOATOS_CI_TRACE_ONLY ?? "")) {
    console.error("refusing to record: GOATOS_CI_TRACE_ONLY is set; a trace run executes nothing and can never produce evidence");
    process.exit(2);
  }
  if (!sha || !/^[0-9a-f]{40}$/.test(sha)) {
    console.error(`--record needs a full 40-hex sha, got: ${sha}`);
    process.exit(2);
  }
  if (!['all', 'scoped'].includes(mode)) {
    console.error(`--mode must be all or scoped, got: ${mode}`);
    process.exit(2);
  }
  if (!SCREENSHOT_STATES.includes(screenshots)) {
    console.error(`--screenshots must be one of ${SCREENSHOT_STATES.join("|")}, got: ${screenshots}`);
    process.exit(2);
  }
  // A receipt covering the android job must state screenshot coverage explicitly.
  // It must never assert "irrelevant" by default. This RECORDS a fact; it does
  // not relax any exact-SHA / base / job-coverage check.
  const coversAndroid = mode === "all" || String(jobs || "").includes("android");
  if (coversAndroid && (screenshots === "unknown" || screenshots === "not-applicable")) {
    console.error("refusing to record: this run covers the android job but did not report screenshot coverage");
    process.exit(2);
  }
  // BOTH modes must state the base they diffed against — see computeBaseAncestry.
  // A mode:"all" receipt without a base is unvalidatable and would re-open the
  // base-spoof hole (`GOATOS_CI_BASE=HEAD make ci-local` -> green, UI proof
  // silently skipped).
  if (!base || !/^[0-9a-f]{40}$/.test(base)) {
    console.error(`--record needs --base with a full 40-hex sha (the resolved CI diff base), got: ${base}`);
    process.exit(2);
  }
  const receipt = {
    sha,
    mode,
    base,
    screenshots,
    result: "green",
    timestamp: new Date().toISOString(),
    generatedBy: "tools/ci/run-local-ci.sh",
  };
  if (mode === "scoped") {
    receipt.jobs = normalizedJobs(jobs);
    receipt.rulesHash = currentRulesHash();
    const coverage = computeScopedCoverage({ localSha: sha, remoteSha: base, receipt });
    if (!coverage.ok) {
      console.error(`refusing to record incomplete scoped receipt: ${coverage.reason}`);
      process.exit(1);
    }
  }
  writeFileSync(receiptPath(), JSON.stringify(receipt, null, 2) + "\n");
  console.log(`local-ci-evidence: recorded GREEN ${mode} receipt for ${sha.slice(0, 12)} at ${receiptPath()}`);
}

// `recordFn` is a test seam ONLY: production always passes the real `record`.
// It exists so the self-test can drive BOTH re-record branches (including the
// scoped one, whose real `record` would need live git objects) and assert the
// CARRIED VALUE rather than the shape of this function's source text.
function reuseAfterRebase({ oldSha, newSha, newBase, jobs, recordFn = record }) {
  const receipt = readReceipt();
  if (!receipt || receipt.result !== "green") {
    console.error("cannot reuse CI receipt: no green receipt is present");
    process.exit(1);
  }
  if (receipt.sha !== oldSha) {
    console.error(`cannot reuse CI receipt: receipt is for ${String(receipt.sha).slice(0, 12)}, not ${oldSha.slice(0, 12)}`);
    process.exit(1);
  }
  if (!/^[0-9a-f]{40}$/.test(newSha) || !/^[0-9a-f]{40}$/.test(newBase)) {
    console.error("cannot reuse CI receipt: --new-sha and --new-base must be full 40-hex SHAs");
    process.exit(2);
  }
  if (receipt.mode === "all") {
    // Carry screenshot coverage forward VERBATIM; never upgrade skipped -> yes.
    // Rebind to the NEW base as well as the new sha: a carried receipt still has
    // to name a base that push-time ancestry validation can check.
    recordFn(newSha, { mode: "all", base: newBase, jobs: "common,backend,query-plans,admin-web,android", screenshots: receipt.screenshots });
    return;
  }
  if (receipt.mode !== "scoped") {
    console.error(`cannot reuse CI receipt: unsupported receipt mode ${receipt.mode}`);
    process.exit(1);
  }
  if (receipt.rulesHash !== currentRulesHash()) {
    console.error("cannot reuse CI receipt: component path rules changed after the receipt was recorded");
    process.exit(1);
  }
  const recorded = normalizedJobs(receipt.jobs);
  const required = normalizedJobs(jobs);
  if (JSON.stringify(recorded) !== JSON.stringify(required)) {
    console.error(`cannot reuse CI receipt: required jobs changed from ${recorded.join(",")} to ${required.join(",")}`);
    process.exit(1);
  }
  recordFn(newSha, { mode: "scoped", base: newBase, jobs: required.join(","), screenshots: receipt.screenshots });
}

function verify() {
  const head = execFileSync("git", ["rev-parse", "HEAD"]).toString("utf8").trim();
  const receipt = readReceipt();
  // --verify is the OFFLINE local sanity check (guardrail-manifest realCheck),
  // not the push gate. It has no push payload, so it uses the locally known
  // origin/main when one exists — which is what makes a base spoof visible here
  // too — and otherwise falls back to the receipt's own base, exactly as before.
  // The authoritative remote-main comparison is prePush, which gets the true
  // remote sha from git.
  let localOriginMain = "";
  try {
    localOriginMain = execFileSync("git", ["rev-parse", "--verify", "refs/remotes/origin/main"], { stdio: ["ignore", "pipe", "ignore"] }).toString("utf8").trim();
  } catch { /* no remote-tracking main locally (offline clone / fresh worktree) */ }
  if (!localOriginMain) {
    console.error("local-ci-evidence: NOTE — refs/remotes/origin/main is unavailable, so the receipt's diff base could NOT be checked against real remote main here. The pre-push hook still enforces it.");
  }
  const remoteSha = receipt?.base || ZERO_SHA;
  const { blocked, reasons } = evaluatePush({
    pushLines: [`HEAD ${head} ${MAIN_REF} ${remoteSha}`],
    receipt,
    scopedCoverage: computeScopedCoverage,
    // Ancestry is checked against the locally known origin/main, NOT against the
    // synthetic remoteSha above (which is the receipt's own base and would make
    // the check vacuous). With no local origin/main this degrades to "the
    // receipt must at least NAME a base", and says so loudly above.
    baseAncestry: ({ receipt: r }) => computeBaseAncestry({ receipt: r, remoteSha: localOriginMain || ZERO_SHA }),
  });
  // The synthetic line above pushes HEAD to main; reuse the same gate.
  if (blocked) {
    console.error("local-ci-evidence: HEAD is NOT authorized to push main:");
    for (const r of reasons) console.error(`  - ${r}`);
    process.exit(1);
  }
  console.log(`local-ci-evidence: HEAD ${head.slice(0, 12)} has a matching GREEN ${receipt.mode} CI receipt (screenshots=${receipt.screenshots ?? "unknown"})`);
}

function prePush() {
  if (localCiBypassEnabled()) {
    console.error("local-ci-evidence: GOATOS_BYPASS_LOCAL_CI=1; bypassing exact-SHA local-CI receipt gate for this push");
    return;
  }
  const payload = readFileSync(0, "utf8"); // stdin
  const { blocked, reasons } = evaluatePush({
    pushLines: payload.split("\n"),
    receipt: readReceipt(),
    scopedCoverage: computeScopedCoverage,
    baseAncestry: computeBaseAncestry,
    mainFreshness: computeMainFreshness,
  });
  if (blocked) {
    console.error("╔══ push to main BLOCKED — use the fresh-main landing gate ══╗");
    for (const r of reasons) console.error(`  ✗ ${r}`);
    console.error("  Run:  make land-main   (fetch + rebase + exact-SHA CI + guarded push).");
    console.error("╚════════════════════════════════════════════════════════════════╝");
    process.exit(1);
  }
}

function agentHook() {
  const command = commandFromHookPayload(readFileSync(0, "utf8"));
  if (!commandAttemptsDirectMainPush(command)) return;
  if (localCiBypassEnabled() || commandHasLocalCiBypass(command)) {
    console.error("GOATOS MAIN LANDING LOCAL-CI BYPASS ENABLED");
    console.error("Proceeding because GOATOS_BYPASS_LOCAL_CI=1 was explicit on this command.");
    return;
  }
  console.error("GOATOS MAIN LANDING BLOCKED FOR AGENT");
  console.error("Codex and Claude must run `make land-main`; it refreshes and rebases origin/main before CI, reruns CI if main moves, then pushes the exact green SHA.");
  process.exit(2);
}

function selfTest() {
  const sha = "a".repeat(40);
  const other = "b".repeat(40);
  // Every fixture receipt now carries `base`, because a receipt without one no
  // longer authorizes main (see computeBaseAncestry). `okBase` stands in for a
  // base that IS an ancestor of remote main; the real ancestry function is
  // exercised against live git objects further down.
  const okBase = () => ({ ok: true });
  const green = { sha, base: other, mode: "all", result: "green", screenshots: "skipped" };
  const mainPush = [`refs/heads/main ${sha} ${MAIN_REF} ${other}`];

  // matching green full receipt -> allowed
  if (evaluatePush({ pushLines: mainPush, receipt: green, baseAncestry: okBase }).blocked) throw new Error("self-test: matching receipt should allow");
  // a full receipt never authorizes a stale/non-rebased candidate
  if (!evaluatePush({ pushLines: mainPush, receipt: green, baseAncestry: okBase, mainFreshness: () => ({ ok: false, reason: "stale main" }) }).blocked) {
    throw new Error("self-test: stale main should block even with a full receipt");
  }
  // no receipt -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: null }).blocked) throw new Error("self-test: missing receipt should block");
  // explicit partial run -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha, mode: "guardrails", result: "green" } }).blocked) throw new Error("self-test: partial run should block");
  // matching scoped receipt -> allowed only when its coverage was revalidated
  const scoped = { sha, base: other, jobs: ["common", "backend"], mode: "scoped", result: "green" };
  if (evaluatePush({ pushLines: mainPush, receipt: scoped, scopedCoverage: () => ({ ok: true }), baseAncestry: okBase }).blocked) throw new Error("self-test: valid scoped receipt should allow");
  if (!evaluatePush({ pushLines: mainPush, receipt: scoped }).blocked) throw new Error("self-test: unvalidated scoped receipt should block");
  // red result -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha, mode: "all", result: "red" } }).blocked) throw new Error("self-test: red receipt should block");
  // receipt for a different sha -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha: other, mode: "all", result: "green", screenshots: "skipped" } }).blocked) throw new Error("self-test: wrong-sha receipt should block");

  // screenshot coverage is EVIDENCE: skipped-with-ui-diff never authorizes main
  // F-C: `base` + okBase are LOAD-BEARING. Without them computeBaseAncestry
  // rejects the fixture first, the screenshot branch is never reached, and this
  // assertion passes even when the whole gate is deleted.
  const uiDiff = { sha, base: other, mode: "all", result: "green", screenshots: "skipped-with-ui-diff" };
  if (!evaluatePush({ pushLines: mainPush, receipt: uiDiff, baseAncestry: okBase }).blocked) {
    throw new Error("self-test: skipped-with-ui-diff must block a main push");
  }
  for (const state of ["yes", "skipped"]) {
    if (evaluatePush({ pushLines: mainPush, receipt: { sha, base: other, mode: "all", result: "green", screenshots: state }, baseAncestry: okBase }).blocked) {
      throw new Error(`self-test: screenshots=${state} must still authorize main`);
    }
  }
  // an android-covering receipt with no recorded coverage is not evidence
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha, base: other, mode: "all", result: "green" }, baseAncestry: okBase }).blocked) {
    throw new Error("self-test: android-covering receipt with no screenshots field must block");
  }
  // a scoped receipt that does NOT cover android is unaffected by the screenshot gate
  if (evaluatePush({
    pushLines: mainPush,
    receipt: { sha, base: other, jobs: ["common", "backend"], mode: "scoped", result: "green", screenshots: "not-applicable" },
    scopedCoverage: () => ({ ok: true }),
    baseAncestry: okBase,
  }).blocked) {
    throw new Error("self-test: a non-android scoped receipt must not be screenshot-gated");
  }
  // ...but a scoped receipt that DOES cover android with a ui-diff still blocks
  if (!evaluatePush({
    pushLines: mainPush,
    receipt: { sha, base: other, jobs: ["common", "android"], mode: "scoped", result: "green", screenshots: "skipped-with-ui-diff" },
    scopedCoverage: () => ({ ok: true }),
    baseAncestry: okBase,
  }).blocked) {
    throw new Error("self-test: a scoped android receipt with skipped-with-ui-diff must block");
  }
  // ── BASE PROVENANCE (HOLE B) ────────────────────────────────────────────
  // The exact receipt `GOATOS_CI_BASE=HEAD make ci-local` used to produce:
  // genuinely green, mode:"all", screenshots:"skipped" — and no base at all.
  // It must not authorize main.
  if (!evaluatePush({
    pushLines: mainPush,
    receipt: { sha, mode: "all", result: "green", screenshots: "skipped" },
    baseAncestry: computeBaseAncestry,
  }).blocked) {
    throw new Error("self-test: a mode=all receipt with NO base must block a main push");
  }
  // ...and the ancestry rule itself, exercised against REAL git objects rather
  // than a stub, so a broken merge-base call cannot pass this file.
  const gitSha = (rev) => {
    try {
      return execFileSync("git", ["rev-parse", "--verify", `${rev}^{commit}`], { stdio: ["ignore", "pipe", "ignore"] }).toString("utf8").trim();
    } catch { return ""; }
  };
  const headSha = gitSha("HEAD");
  const parentSha = gitSha("HEAD~1");
  if (headSha && parentSha) {
    // legitimate: base is behind remote main
    if (!computeBaseAncestry({ receipt: { base: parentSha }, remoteSha: headSha }).ok) {
      throw new Error("self-test: an ancestor base must be accepted");
    }
    // legitimate: base IS remote main
    if (!computeBaseAncestry({ receipt: { base: headSha }, remoteSha: headSha }).ok) {
      throw new Error("self-test: base == remote main must be accepted");
    }
    // THE SPOOF: base ahead of remote main (GOATOS_CI_BASE=HEAD) diffs nothing
    if (computeBaseAncestry({ receipt: { base: headSha }, remoteSha: parentSha }).ok) {
      throw new Error("self-test: a base that is NOT an ancestor of remote main must be rejected");
    }
    // and the same spoof must block the whole push, not just fail a helper
    if (!evaluatePush({
      pushLines: [`refs/heads/main ${headSha} ${MAIN_REF} ${parentSha}`],
      receipt: { sha: headSha, base: headSha, mode: "all", result: "green", screenshots: "skipped" },
      baseAncestry: computeBaseAncestry,
    }).blocked) {
      throw new Error("self-test: a base-spoofed mode=all receipt must block a main push");
    }
  } else {
    console.error("local-ci-evidence self-test: NOTE — no HEAD~1 available (shallow clone); live-git ancestry cases skipped");
  }

  // pushing a non-main branch -> allowed even with no receipt
  if (evaluatePush({ pushLines: [`refs/heads/feature ${sha} refs/heads/feature ${other}`], receipt: null }).blocked) throw new Error("self-test: non-main push should not be gated");
  // deleting main (zero local sha) -> allowed
  if (evaluatePush({ pushLines: [`(delete) ${ZERO_SHA} ${MAIN_REF} ${other}`], receipt: null }).blocked) throw new Error("self-test: main delete should not be gated");
  if (!commandHasLocalCiBypass("GOATOS_BYPASS_LOCAL_CI=1 git mesha-push HEAD:main")) {
    throw new Error("self-test: direct main push bypass marker was not detected");
  }

  for (const command of [
    "git mesha-push main",
    "git mesha-push HEAD:main",
    "zsh -ic 'git mesha-push HEAD:refs/heads/main'",
    "git push origin main",
    "git push origin HEAD:main",
    "git push --force origin HEAD:refs/heads/main",
    "git push --delete origin main",
    "git push",
    "git push origin",
  ]) {
    if (!commandAttemptsDirectMainPush(command)) throw new Error(`self-test: direct main push escaped agent gate: ${command}`);
  }
  for (const command of [
    "make land-main",
    "bash tools/ci/land-main.sh",
    "git fetch origin main",
    "git push origin feature/example",
    "git mesha-push feature/example",
  ]) {
    if (commandAttemptsDirectMainPush(command)) throw new Error(`self-test: safe command was blocked: ${command}`);
  }

  // screenshot-coverage field: recorded + carried forward, never upgraded.
  const tmpReceipt = receiptPath();
  const restore = (() => { try { return readFileSync(tmpReceipt, "utf8"); } catch { return null; } })();
  const runRecord = (argv) => {
    try {
      execFileSync(process.execPath, ["tools/ci/check-local-ci-evidence.mjs", ...argv], { stdio: "pipe" });
      return 0;
    } catch (error) {
      return error.status ?? 1;
    }
  };
  try {
    // (i) a skipped receipt round-trips
    if (runRecord(["--record", sha, "--base", other, "--mode", "all", "--jobs", "common,android", "--screenshots", "skipped"]) !== 0) {
      throw new Error("self-test: recording a skipped-screenshot full receipt should succeed");
    }
    if (readReceipt()?.screenshots !== "skipped") throw new Error("self-test: screenshots field did not round-trip");
    // (ii) reuseAfterRebase preserves `skipped` in the ALL branch
    record(other, { mode: "all", base: sha, jobs: "common,android", screenshots: readReceipt().screenshots });
    if (readReceipt()?.screenshots !== "skipped") throw new Error("self-test: all-branch reuse must preserve skipped");
    // (iii) BEHAVIOURAL: drive the REAL reuseAfterRebase for BOTH re-record
    // branches and assert the value it actually carries. This is deliberately
    // NOT a source-shape assertion: a source regex is satisfied by
    // `receipt.screenshots ?? "skipped"`, which would silently green a
    // field-less receipt across a rebase. Here that patch changes the CARRIED
    // VALUE, so the undefined case below fails.
    const carried = (receiptOverrides, extra = {}) => {
      writeFileSync(tmpReceipt, JSON.stringify({ sha: other, result: "green", ...receiptOverrides }) + "\n");
      let seen; let calls = 0;
      reuseAfterRebase({
        oldSha: other,
        newSha: sha,
        newBase: "c".repeat(40),
        ...extra,
        recordFn: (recordedSha, options) => { calls += 1; seen = { recordedSha, options }; },
      });
      if (calls !== 1) throw new Error(`self-test: reuseAfterRebase must re-record exactly once (got ${calls})`);
      if (seen.recordedSha !== sha) throw new Error("self-test: reuseAfterRebase must re-record against the NEW sha");
      return seen.options;
    };
    for (const state of [...SCREENSHOT_STATES, undefined]) {
      // ALL branch
      const all = carried({ mode: "all", screenshots: state });
      if (all.mode !== "all") throw new Error("self-test: all-branch reuse must stay mode=all");
      if (all.screenshots !== state) {
        throw new Error(`self-test: all-branch reuse carried screenshots=${JSON.stringify(all.screenshots)}, expected ${JSON.stringify(state)}`);
      }
      // SCOPED branch (jobs + rulesHash must match for it to reach the re-record)
      const scopedOut = carried(
        { mode: "scoped", screenshots: state, jobs: ["common", "backend"], rulesHash: currentRulesHash() },
        { jobs: "backend,common" },
      );
      if (scopedOut.mode !== "scoped") throw new Error("self-test: scoped-branch reuse must stay mode=scoped");
      if (scopedOut.screenshots !== state) {
        throw new Error(`self-test: scoped-branch reuse carried screenshots=${JSON.stringify(scopedOut.screenshots)}, expected ${JSON.stringify(state)}`);
      }
    }
    // (iv) an invalid value exits 2
    if (runRecord(["--record", sha, "--base", other, "--mode", "all", "--jobs", "common", "--screenshots", "maybe"]) !== 2) {
      throw new Error("self-test: an invalid --screenshots value must exit 2");
    }
    // (v) android in scope with no --screenshots exits 2
    if (runRecord(["--record", sha, "--base", other, "--mode", "all", "--jobs", "common,android"]) !== 2) {
      throw new Error("self-test: android coverage without --screenshots must exit 2");
    }
    // (vi) the ui-diff state is recordable and round-trips, so the blocking
    // check above is reachable from a real run and survives a rebase carry.
    if (runRecord(["--record", sha, "--base", other, "--mode", "all", "--jobs", "common,android", "--screenshots", "skipped-with-ui-diff"]) !== 0) {
      throw new Error("self-test: recording skipped-with-ui-diff should succeed (it is blocked at push time, not record time)");
    }
    if (readReceipt()?.screenshots !== "skipped-with-ui-diff") throw new Error("self-test: ui-diff state must round-trip");
    // (vii) a receipt with NO base is unrecordable in BOTH modes — the record
    // side of the base-provenance rule, so a run cannot mint the unvalidatable
    // receipt in the first place.
    if (runRecord(["--record", sha, "--mode", "all", "--jobs", "common", "--screenshots", "skipped"]) !== 2) {
      throw new Error("self-test: recording a mode=all receipt without --base must exit 2");
    }
    if (runRecord(["--record", sha, "--mode", "scoped", "--jobs", "common", "--screenshots", "skipped"]) !== 2) {
      throw new Error("self-test: recording a mode=scoped receipt without --base must exit 2");
    }
  } finally {
    if (restore !== null) writeFileSync(tmpReceipt, restore);
    else { try { unlinkSync(tmpReceipt); } catch { /* nothing to clean up */ } }
  }

  console.log("local-ci-evidence guard: self-test passed");
}

const args = process.argv.slice(2);
if (args.includes("--self-test")) selfTest();
else if (args.includes("--record")) record(argValue(args, "--record"), {
  mode: argValue(args, "--mode", "all"),
  base: argValue(args, "--base"),
  jobs: argValue(args, "--jobs", ""),
  screenshots: argValue(args, "--screenshots", "unknown"),
});
else if (args.includes("--reuse-after-rebase")) reuseAfterRebase({
  oldSha: argValue(args, "--old-sha"),
  newSha: argValue(args, "--new-sha"),
  newBase: argValue(args, "--new-base"),
  jobs: argValue(args, "--jobs", ""),
});
else if (args.includes("--verify")) verify();
else if (args.includes("--pre-push")) prePush();
else if (args.includes("--agent-hook")) agentHook();
else {
  console.error("usage: check-local-ci-evidence.mjs --record <sha> [--mode all|scoped --base <sha> --jobs <csv> --screenshots yes|skipped|skipped-with-ui-diff|not-applicable] | --verify | --pre-push | --agent-hook | --self-test");
  process.exit(2);
}
