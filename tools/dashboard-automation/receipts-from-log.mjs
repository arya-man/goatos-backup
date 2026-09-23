#!/usr/bin/env node
// receipts-from-log.mjs — rebuild a worker's receipts from its console log.
//
//   node tools/dashboard-automation/receipts-from-log.mjs run.log tools/dashboard-automation/receipts.wN.json
//
// WHY THIS EXISTS. prove-commit-ledger-reverts writes its receipts file ONCE, when the whole slice
// is finished, so a worker stopped or killed part-way through loses every verdict it had already
// reached — and a slice of 131 Postgres packages is hours of work to lose. Its console output,
// though, is one line per verdict as it happens: `<sha> <verdict> <check-or-why>`.
//
// The rebuilt receipt carries what the LINE can support and nothing more: the sha, the verdict,
// and the named check. `allFailing` and `packages` are not in the log, so they are absent rather
// than invented, and `method` says the receipt was recovered — a reader must be able to tell a
// rebuilt row from one the prover wrote in full.
import { readFileSync, writeFileSync } from "node:fs";

const [, , logPath, outPath] = process.argv;
if (!logPath || !outPath) {
  console.error("usage: receipts-from-log.mjs <worker.log> <receipts.json>");
  process.exit(1);
}

const receipts = [];
const rejected = [];
for (const line of readFileSync(logPath, "utf8").split("\n")) {
  const m = /^([0-9a-f]{7,40})\s+(proved|disproved|inconclusive|skipped)\s*(.*)$/.exec(line.trim());
  if (!m) continue;
  const [, sha, verdict, rest] = m;
  if (verdict === "proved") {
    receipts.push({
      sha,
      check: rest.trim(),
      method: "recovered from the worker's console log after an interrupted run; the prover's own file was never written",
      recordedAt: new Date().toISOString().slice(0, 10),
    });
  } else {
    rejected.push({ sha, verdict, why: rest.trim(), packages: [] });
  }
}

writeFileSync(outPath, `${JSON.stringify({ receipts, rejected }, null, 1)}\n`);
console.log(`receipts-from-log: ${receipts.length} proved, ${rejected.length} rejected -> ${outPath}`);
