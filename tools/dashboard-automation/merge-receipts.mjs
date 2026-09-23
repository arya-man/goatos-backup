#!/usr/bin/env node
// merge-receipts.mjs — fold each parallel prover worker's receipts file into the canonical one.
//
// The workers take disjoint slices of the ledger and write their own files so they never race on
// one path; every receipt and every rejection is keyed by sha, so the fold is an ordinary union.
//
// TWO RULES, both about not letting a stale verdict outlive its evidence:
//   - a PROOF wins over a rejection for the same sha, and retires it. `disproved` is published as
//     an accusation against a named commit's test, so leaving one beside a fresh proof keeps
//     accusing work that has since been shown to bite.
//   - a later verdict wins over an earlier one for the same sha, because a re-run only happens
//     when something that decides the answer has changed (a database it can now reach, a harness
//     it can now build against).
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const canonical = join(here, "commit-ledger-receipts.json");
const prior = JSON.parse(readFileSync(canonical, "utf8"));
const proved = new Map((prior.receipts ?? []).map((r) => [r.sha, r]));
const rejected = new Map((prior.rejected ?? []).map((r) => [r.sha, r]));

let files = 0;
for (const name of readdirSync(here).filter((f) => /^receipts\.w\d+\.json$/.test(f))) {
  const part = JSON.parse(readFileSync(join(here, name), "utf8"));
  for (const r of part.receipts ?? []) proved.set(r.sha, r);
  for (const r of part.rejected ?? []) if (!proved.has(r.sha)) rejected.set(r.sha, r);
  files += 1;
}
for (const sha of proved.keys()) rejected.delete(sha);

writeFileSync(canonical, `${JSON.stringify({
  ...prior,
  proved: proved.size,
  rejectedCount: rejected.size,
  receipts: [...proved.values()].sort((a, b) => a.sha.localeCompare(b.sha)),
  rejected: [...rejected.values()].sort((a, b) => a.sha.localeCompare(b.sha)),
}, null, 1)}\n`);

const byVerdict = {};
for (const r of rejected.values()) byVerdict[r.verdict] = (byVerdict[r.verdict] ?? 0) + 1;
console.log(`merged ${files} worker file(s): proved ${proved.size}, rejected ${rejected.size}`, byVerdict);
