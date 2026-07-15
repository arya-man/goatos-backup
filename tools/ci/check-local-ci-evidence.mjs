#!/usr/bin/env node
// check-local-ci-evidence.mjs — exact-SHA local-CI push gate.
//
// Governance invariant (docs/runbooks/local-release-evidence.md): a push to `main` is only
// authorized by a FULL green `make ci-local` on the EXACT commit being pushed. Partial
// `JOB=...` runs are fine while developing but never authorize a main push.
//
// Machinery:
//   tools/ci/run-local-ci.sh, after a FULL run (job "all") that ends GREEN, calls
//     node tools/ci/check-local-ci-evidence.mjs --record <sha>
//   which writes a SHA-bound receipt to the worktree git dir (never committed).
//   The installed pre-push hook calls
//     node tools/ci/check-local-ci-evidence.mjs --pre-push   (git push payload on stdin)
//   which blocks any update to refs/heads/main whose local SHA has no matching green receipt.
//
// Modes: --record <sha> | --verify | --pre-push | --self-test
// Deterministic, offline. No network.

import { readFileSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";

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

// Pure: given the git push payload lines and the current receipt, decide whether the push is
// blocked. Only pushes that UPDATE refs/heads/main are gated; deletes and other refs pass.
export function evaluatePush({ pushLines, receipt }) {
  const reasons = [];
  for (const raw of pushLines) {
    const line = raw.trim();
    if (!line) continue;
    const [localRef, localSha, remoteRef] = line.split(/\s+/);
    if (remoteRef !== MAIN_REF) continue; // only gate main
    if (localSha === ZERO_SHA) continue; // branch delete — not a content push
    if (!receipt) {
      reasons.push(`no local-CI receipt found; run a full \`make ci-local\` on ${localSha.slice(0, 12)} before pushing main`);
      continue;
    }
    if (receipt.result !== "green") {
      reasons.push(`local-CI receipt is "${receipt.result}", not green (sha ${String(receipt.sha).slice(0, 12)})`);
      continue;
    }
    if (receipt.mode !== "all") {
      reasons.push(`local-CI receipt is a partial run (mode="${receipt.mode}"); a FULL \`make ci-local\` (all jobs) is required to push main`);
      continue;
    }
    if (receipt.sha !== localSha) {
      reasons.push(`local-CI receipt is for ${String(receipt.sha).slice(0, 12)} but you are pushing ${localSha.slice(0, 12)}; re-run \`make ci-local\` on the exact commit`);
      continue;
    }
  }
  return { blocked: reasons.length > 0, reasons };
}

function record(sha) {
  if (!sha || !/^[0-9a-f]{40}$/.test(sha)) {
    console.error(`--record needs a full 40-hex sha, got: ${sha}`);
    process.exit(2);
  }
  const receipt = {
    sha,
    mode: "all",
    result: "green",
    timestamp: new Date().toISOString(),
    generatedBy: "tools/ci/run-local-ci.sh",
  };
  writeFileSync(receiptPath(), JSON.stringify(receipt, null, 2) + "\n");
  console.log(`local-ci-evidence: recorded GREEN receipt for ${sha.slice(0, 12)} at ${receiptPath()}`);
}

function verify() {
  const head = execFileSync("git", ["rev-parse", "HEAD"]).toString("utf8").trim();
  const { blocked, reasons } = evaluatePush({
    pushLines: [`HEAD ${head} ${MAIN_REF} ${ZERO_SHA}`.replace(ZERO_SHA, "0".repeat(40))],
    receipt: readReceipt(),
  });
  // The synthetic line above pushes HEAD to main; reuse the same gate.
  if (blocked) {
    console.error("local-ci-evidence: HEAD is NOT authorized to push main:");
    for (const r of reasons) console.error(`  - ${r}`);
    process.exit(1);
  }
  console.log(`local-ci-evidence: HEAD ${head.slice(0, 12)} has a matching GREEN full-CI receipt`);
}

function prePush() {
  const payload = readFileSync(0, "utf8"); // stdin
  const { blocked, reasons } = evaluatePush({ pushLines: payload.split("\n"), receipt: readReceipt() });
  if (blocked) {
    console.error("╔══ push to main BLOCKED — missing exact-SHA local-CI evidence ══╗");
    for (const r of reasons) console.error(`  ✗ ${r}`);
    console.error("  Run:  make ci-local   (full green on the exact commit) then push again.");
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
  // partial run -> blocked
  if (!evaluatePush({ pushLines: mainPush, receipt: { sha, mode: "guardrails", result: "green" } }).blocked) throw new Error("self-test: partial run should block");
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
else if (args.includes("--record")) record(args[args.indexOf("--record") + 1]);
else if (args.includes("--verify")) verify();
else if (args.includes("--pre-push")) prePush();
else {
  console.error("usage: check-local-ci-evidence.mjs --record <sha> | --verify | --pre-push | --self-test");
  process.exit(2);
}
