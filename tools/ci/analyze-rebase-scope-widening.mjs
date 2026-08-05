#!/usr/bin/env node
// analyze-rebase-scope-widening.mjs — ANALYSIS ONLY. Not a gate, not wired into CI.
//
// Question it answers (Item 7b of the landing-path wall-clock work): when
// land-main rebases onto a main that moved, how often would the base advance
// WIDEN the classifier selection — which is what forces a second full ~20-minute
// gate run? ci-scope.mjs forces the full suite for tools/ci/** and unmapped
// paths, so a single infrastructure commit landing on main can widen everyone.
//
// Read-only: it only reads git history and runs the classifier. Run:
//   node tools/ci/analyze-rebase-scope-widening.mjs [count]
import { execFileSync } from "node:child_process";

const count = Number(process.argv[2] || 30);
const sh = (args) => execFileSync("git", args, { encoding: "utf8" }).trim();

const commits = sh(["rev-list", `-n${count + 1}`, "origin/main"]).split("\n").reverse();

const scopeFor = (base, head) => {
  try {
    return JSON.parse(
      execFileSync(process.execPath, ["tools/ci/ci-scope.mjs", "--base", base, "--head", head, "--format", "json"], {
        encoding: "utf8",
      }),
    );
  } catch (error) {
    return { error: error.message };
  }
};

let widened = 0;
let sameScope = 0;
const rows = [];

// For each adjacent pair (older base -> newer base), treat the newer commit as
// "a candidate" and compare its selection against the older base vs the newer
// base. A widening means: the rebase would have invalidated a scoped receipt.
for (let i = 1; i < commits.length - 1; i += 1) {
  const olderBase = commits[i - 1];
  const newerBase = commits[i];
  const candidate = commits[i + 1];
  const before = scopeFor(olderBase, candidate);
  const after = scopeFor(newerBase, candidate);
  if (before.error || after.error) continue;
  const b = before.full ? "FULL" : [...before.selectedJobs].sort().join(",");
  const a = after.full ? "FULL" : [...after.selectedJobs].sort().join(",");
  if (a === b) sameScope += 1;
  else widened += 1;
  rows.push(`${candidate.slice(0, 12)}  base ${olderBase.slice(0, 8)}->${newerBase.slice(0, 8)}  ${b}  =>  ${a}`);
}

for (const row of rows) console.log(row);
console.log("");
console.log(`pairs analysed: ${rows.length}`);
console.log(`selection unchanged across the base advance: ${sameScope}`);
console.log(`selection CHANGED (receipt reuse would be refused): ${widened}`);
