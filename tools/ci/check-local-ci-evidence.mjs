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
//   which blocks any update to refs/heads/main whose local SHA has no matching green receipt.
//
// Modes: --record <sha> | --verify | --pre-push | --self-test
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
export function evaluatePush({ pushLines, receipt, scopedCoverage = () => ({ ok: false, reason: "scoped coverage was not revalidated" }) }) {
  const reasons = [];
  for (const raw of pushLines) {
    const line = raw.trim();
    if (!line) continue;
    const [localRef, localSha, remoteRef, remoteSha] = line.split(/\s+/);
    if (remoteRef !== MAIN_REF) continue; // only gate main
    if (localSha === ZERO_SHA) continue; // branch delete — not a content push
    if (!receipt) {
      reasons.push(`no local-CI receipt found; run \`make ci-local\` on ${localSha.slice(0, 12)} before pushing main`);
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
  const payload = readFileSync(0, "utf8"); // stdin
  const { blocked, reasons } = evaluatePush({
    pushLines: payload.split("\n"),
    receipt: readReceipt(),
    scopedCoverage: computeScopedCoverage,
  });
  if (blocked) {
    console.error("╔══ push to main BLOCKED — missing exact-SHA local-CI evidence ══╗");
    for (const r of reasons) console.error(`  ✗ ${r}`);
    console.error("  Run:  make ci-local   (complete affected-component green on the exact commit) then push again.");
    console.error("╚════════════════════════════════════════════════════════════════╝");
    process.exit(1);
  }
}

function selfTest() {
  const sha = "a".repeat(40);
  const other = "b".repeat(40);
  const green = { sha, mode: "all", result: "green" };
  const mainPush = [`refs/heads/main ${sha} ${MAIN_REF} ${other}`];

  // matching green full receipt -> allowed
  if (evaluatePush({ pushLines: mainPush, receipt: green }).blocked) throw new Error("self-test: matching receipt should allow");
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

  console.log("local-ci-evidence guard: self-test passed");
}

const args = process.argv.slice(2);
if (args.includes("--self-test")) selfTest();
else if (args.includes("--record")) record(argValue(args, "--record"), {
  mode: argValue(args, "--mode", "all"),
  base: argValue(args, "--base"),
  jobs: argValue(args, "--jobs", ""),
});
else if (args.includes("--verify")) verify();
else if (args.includes("--pre-push")) prePush();
else {
  console.error("usage: check-local-ci-evidence.mjs --record <sha> [--mode all|scoped --base <sha> --jobs <csv>] | --verify | --pre-push | --self-test");
  process.exit(2);
}
