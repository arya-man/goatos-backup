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
import { readFileSync, writeFileSync } from "node:fs";

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

// Pure: given the git push payload lines and the current receipt, decide whether the push is
// blocked. Only pushes that UPDATE refs/heads/main are gated; deletes and other refs pass.
export function evaluatePush({
  pushLines,
  receipt,
  scopedCoverage = () => ({ ok: false, reason: "scoped coverage was not revalidated" }),
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
    if (receipt.mode === "scoped") {
      const coverage = scopedCoverage({ localSha, remoteSha, receipt });
      if (!coverage.ok) reasons.push(coverage.reason);
      continue;
    }
    if (receipt.mode !== "all") reasons.push(`local-CI receipt mode="${receipt.mode}" is not an auto-scoped or full run`);
  }
  return { blocked: reasons.length > 0, reasons };
}

function record(sha, { mode, base, jobs }) {
  if (!sha || !/^[0-9a-f]{40}$/.test(sha)) {
    console.error(`--record needs a full 40-hex sha, got: ${sha}`);
    process.exit(2);
  }
  if (!['all', 'scoped'].includes(mode)) {
    console.error(`--mode must be all or scoped, got: ${mode}`);
    process.exit(2);
  }
  const receipt = {
    sha,
    mode,
    result: "green",
    timestamp: new Date().toISOString(),
    generatedBy: "tools/ci/run-local-ci.sh",
  };
  if (mode === "scoped") {
    if (!base || !/^[0-9a-f]{40}$/.test(base)) {
      console.error(`scoped --record needs --base with a full 40-hex sha, got: ${base}`);
      process.exit(2);
    }
    receipt.base = base;
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

function reuseAfterRebase({ oldSha, newSha, newBase, jobs }) {
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
    record(newSha, { mode: "all", jobs: "common,backend,query-plans,admin-web,android" });
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
  record(newSha, { mode: "scoped", base: newBase, jobs: required.join(",") });
}

function verify() {
  const head = execFileSync("git", ["rev-parse", "HEAD"]).toString("utf8").trim();
  const receipt = readReceipt();
  const remoteSha = receipt?.base || ZERO_SHA;
  const { blocked, reasons } = evaluatePush({
    pushLines: [`HEAD ${head} ${MAIN_REF} ${remoteSha}`],
    receipt,
    scopedCoverage: computeScopedCoverage,
  });
  // The synthetic line above pushes HEAD to main; reuse the same gate.
  if (blocked) {
    console.error("local-ci-evidence: HEAD is NOT authorized to push main:");
    for (const r of reasons) console.error(`  - ${r}`);
    process.exit(1);
  }
  console.log(`local-ci-evidence: HEAD ${head.slice(0, 12)} has a matching GREEN ${receipt.mode} CI receipt`);
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
  const green = { sha, mode: "all", result: "green" };
  const mainPush = [`refs/heads/main ${sha} ${MAIN_REF} ${other}`];

  // matching green full receipt -> allowed
  if (evaluatePush({ pushLines: mainPush, receipt: green }).blocked) throw new Error("self-test: matching receipt should allow");
  // a full receipt never authorizes a stale/non-rebased candidate
  if (!evaluatePush({ pushLines: mainPush, receipt: green, mainFreshness: () => ({ ok: false, reason: "stale main" }) }).blocked) {
    throw new Error("self-test: stale main should block even with a full receipt");
  }
  // no receipt -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: null }).blocked) throw new Error("self-test: missing receipt should block");
  // explicit partial run -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha, mode: "guardrails", result: "green" } }).blocked) throw new Error("self-test: partial run should block");
  // matching scoped receipt -> allowed only when its coverage was revalidated
  const scoped = { sha, base: other, jobs: ["common", "backend"], mode: "scoped", result: "green" };
  if (evaluatePush({ pushLines: mainPush, receipt: scoped, scopedCoverage: () => ({ ok: true }) }).blocked) throw new Error("self-test: valid scoped receipt should allow");
  if (!evaluatePush({ pushLines: mainPush, receipt: scoped }).blocked) throw new Error("self-test: unvalidated scoped receipt should block");
  // red result -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha, mode: "all", result: "red" } }).blocked) throw new Error("self-test: red receipt should block");
  // receipt for a different sha -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha: other, mode: "all", result: "green" } }).blocked) throw new Error("self-test: wrong-sha receipt should block");
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

  console.log("local-ci-evidence guard: self-test passed");
}

const args = process.argv.slice(2);
if (args.includes("--self-test")) selfTest();
else if (args.includes("--record")) record(argValue(args, "--record"), {
  mode: argValue(args, "--mode", "all"),
  base: argValue(args, "--base"),
  jobs: argValue(args, "--jobs", ""),
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
  console.error("usage: check-local-ci-evidence.mjs --record <sha> [--mode all|scoped --base <sha> --jobs <csv>] | --verify | --pre-push | --agent-hook | --self-test");
  process.exit(2);
}
