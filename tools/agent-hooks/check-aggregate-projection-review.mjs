#!/usr/bin/env node

// Blocks new/changed aggregate projections that lack explicit grain/key proof
// and adversarial regression tests. Includes committed, staged, unstaged, and
// untracked work so a dirty tree cannot produce a false "no relevant changes".

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const MARKER = /projection-review:\s*membership=([^;\n]+);\s*group_key=([^;\n]+);\s*join_cardinality=([^;\n]+);\s*pagination=([^;\n]+);\s*scope=([^;\n]+)/i;
const TESTS = {
  cardinality: /(?:OneToMany|MultipleDimensions)/,
  pagination: /(?:Pagination|PageBoundary|MultiPage)/,
  date: /(?:DateShift|ScheduledDate|ExecutionDate)/,
  scope: /(?:ScopeHierarchy|ParkScope|CohortScope)/,
  status: /(?:StatusMatrix|EveryStatus|StatusBuckets)/,
};

function git(args, allowFailure = false) {
  try {
    return execFileSync("git", args, { cwd: repo, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  } catch (error) {
    if (allowFailure) return "";
    throw error;
  }
}

function baseRef() {
  const configured = process.env.AGGREGATE_PROJECTION_BASE;
  if (configured) return configured;
  if (git(["rev-parse", "--verify", "origin/main"], true).trim()) return "origin/main";
  return git(["rev-parse", "HEAD^"], true).trim() || "HEAD";
}

function zlist(output) {
  return output.split("\0").filter(Boolean);
}

function changedFiles(base) {
  const files = new Set([
    ...zlist(git(["diff", "--name-only", "-z", `${base}...HEAD`], true)),
    ...zlist(git(["diff", "--name-only", "-z"], true)),
    ...zlist(git(["diff", "--cached", "--name-only", "-z"], true)),
    ...zlist(git(["ls-files", "--others", "--exclude-standard", "-z"], true)),
  ]);
  return [...files];
}

function diffFor(file, base, untracked) {
  if (untracked.has(file)) {
    const source = readFileSync(resolve(repo, file), "utf8");
    return source.split("\n").map((line) => `+${line}`).join("\n");
  }
  return [
    git(["diff", "--unified=80", `${base}...HEAD`, "--", file], true),
    git(["diff", "--unified=80", "HEAD", "--", file], true),
  ].join("\n");
}

function hunks(diff) {
  const split = diff.split(/^@@.*$/m);
  const parts = split.length > 1 ? split.slice(1) : [diff];
  return parts.map((part) => {
    const lines = part.split("\n").filter((line) => !line.startsWith("---") && !line.startsWith("+++"));
    return {
      visible: lines.filter((line) => !line.startsWith("-")).map((line) => line.replace(/^\+/, "")).join("\n"),
      added: lines.filter((line) => line.startsWith("+")).map((line) => line.slice(1)).join("\n"),
    };
  });
}

function isCandidate(hunk) {
  const shape = /\b(?:COUNT|SUM|AVG|JSONB?_AGG|ARRAY_AGG)\s*\(/i.test(hunk.visible)
    && /\bJOIN\b/i.test(hunk.visible)
    && /\bGROUP\s+BY\b/i.test(hunk.visible);
  const touched = /\b(?:SELECT|WITH|JOIN|GROUP\s+BY|COUNT|SUM|AVG|JSONB?_AGG|ARRAY_AGG)\b/i.test(hunk.added);
  return shape && touched;
}

export function inspectFixture(sourceHunks, changedTests) {
  const candidates = sourceHunks.filter(isCandidate);
  if (candidates.length === 0) return [];
  const failures = [];
  for (const [index, candidate] of candidates.entries()) {
    if (!MARKER.test(candidate.visible)) failures.push(`aggregate hunk ${index + 1}: missing complete projection-review marker`);
  }
  const requirements = new Set(["cardinality", "pagination"]);
  const joined = candidates.map((h) => h.visible).join("\n");
  if (/\b(?:due_at|due_date|planned_date|window_start|window_end|execution_date)\b/i.test(joined)) requirements.add("date");
  if (/\b(?:scope_type|scope_id|parent_location_id|park_id|shed_id|cohort_id)\b/i.test(joined)) requirements.add("scope");
  if (/\bstatus\b/i.test(joined)) requirements.add("status");
  for (const requirement of requirements) {
    if (!TESTS[requirement].test(changedTests)) failures.push(`missing changed ${requirement} adversarial test (${TESTS[requirement]})`);
  }
  return failures;
}

function selfTest() {
  const sql = `-- projection-review: membership=batch_members; group_key=batch_id; join_cardinality=dimensions pre-aggregated; pagination=one tenant aggregate before paging; scope=explicit park/shed/cohort CASE\nSELECT park_id, status, due_date, COUNT(*) FROM obligations JOIN dimensions USING (rule_id) GROUP BY park_id, status, due_date`;
  const hunk = { visible: sql, added: sql };
  const goodTests = "TestDriveOneToMany TestDrivePageBoundary TestDriveDateShift TestDriveScopeHierarchy TestDriveStatusMatrix";
  const bad = inspectFixture([{ visible: sql.replace(/-- projection-review.*\n/, ""), added: sql }], "");
  const good = inspectFixture([hunk], goodTests);
  if (bad.length !== 6 || good.length !== 0) {
    console.error("aggregate-projection-guard self-test failed", { bad, good });
    process.exit(1);
  }
  console.log("aggregate-projection-guard self-test: PASS");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  const base = baseRef();
  const files = changedFiles(base);
  const untracked = new Set(zlist(git(["ls-files", "--others", "--exclude-standard", "-z"], true)));
  const sourceFiles = files.filter((file) => /^(?:backend|tools)\/.*\.(?:go|sql)$/.test(file) && !file.endsWith("_test.go"));
  const testFiles = files.filter((file) => /(?:_test\.go|\.test\.[cm]?[jt]s|\.spec\.[cm]?[jt]s)$/.test(file));
  const sourceHunks = sourceFiles.flatMap((file) => hunks(diffFor(file, base, untracked)).filter(isCandidate).map((h) => ({ ...h, file })));
  if (sourceHunks.length === 0) {
    console.log("aggregate-projection-guard: PASS (no changed aggregate projection hunks)");
    return;
  }
  // Only newly-added test lines count. Merely touching a test file that already
  // contained a conveniently named test must not satisfy this change's proof.
  const changedTests = testFiles.flatMap((file) =>
    hunks(diffFor(file, base, untracked)).map((hunk) => hunk.added),
  ).join("\n");
  const failures = inspectFixture(sourceHunks, changedTests);
  if (failures.length) {
    console.error("aggregate-projection-guard: FAIL");
    for (const hunk of sourceHunks) console.error(`  candidate: ${hunk.file}`);
    for (const failure of failures) console.error(`  - ${failure}`);
    console.error("\nSee .agents/skills/goatos-code-review/references/aggregates-and-projections.md");
    process.exit(1);
  }
  console.log(`aggregate-projection-guard: PASS (${sourceHunks.length} reviewed aggregate hunk(s))`);
}

main();
