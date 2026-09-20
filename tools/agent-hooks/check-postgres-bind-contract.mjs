#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { spawnSync } from "node:child_process";

const root = process.cwd();
const baselinePath = path.join(root, "tools/agent-hooks/postgres-bind-contract-baseline.json");

function parseFindings(output) {
  return output.split("\n").flatMap((line) => {
    const match = line.match(/^(.+):(\d+): ([^:]+): (.+)$/);
    return match ? [{ file: match[1], line: Number(match[2]), kind: match[3], message: match[4] }] : [];
  });
}

function countsByFileAndKind(findings) {
  const counts = {};
  for (const finding of findings.filter((item) => item.kind.startsWith("unverified-dynamic-"))) {
    const key = `${finding.file}|${finding.kind}`;
    counts[key] = (counts[key] ?? 0) + 1;
  }
  return counts;
}

function changedLines(base) {
  const changed = new Map();
  const diff = spawnSync("git", ["diff", "--unified=0", "--diff-filter=ACMR", base, "--", "backend"], {
    cwd: root,
    encoding: "utf8",
  });
  if (diff.status !== 0) throw new Error(diff.stderr?.trim() || `git diff against ${base} failed`);
  let file = "";
  for (const line of diff.stdout.split("\n")) {
    const fileMatch = line.match(/^\+\+\+ b\/(backend\/.+\.go)$/);
    if (fileMatch) {
      file = fileMatch[1].slice("backend/".length);
      changed.set(file, "all");
      continue;
    }
    const hunk = line.match(/^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/);
    if (!file || !hunk) continue;
    const start = Number(hunk[1]);
    const count = hunk[2] === undefined ? 1 : Number(hunk[2]);
    if (changed.get(file) === "all") continue;
    const lines = changed.get(file) ?? new Set();
    for (let n = start; n < start + count; n += 1) lines.add(n);
    changed.set(file, lines);
  }
  const untracked = spawnSync("git", ["ls-files", "--others", "--exclude-standard", "--", "backend"], {
    cwd: root,
    encoding: "utf8",
  });
  for (const name of untracked.stdout.split("\n").filter((item) => item.endsWith(".go"))) {
    changed.set(name.slice("backend/".length), "all");
  }
  return changed;
}

function assess(findings, baseline, changed) {
  const failures = findings.filter((item) => item.kind === "positional-bind-mismatch" || item.kind === "invalid-strict-named-args");
  const current = countsByFileAndKind(findings);
  for (const [key, count] of Object.entries(current)) {
    const allowed = baseline[key] ?? 0;
    if (count > allowed) failures.push({ kind: "dynamic-debt-growth", message: `${key} grew ${allowed} -> ${count}` });
  }
  for (const [key, allowed] of Object.entries(baseline)) {
    const count = current[key] ?? 0;
    if (count < allowed) failures.push({ kind: "stale-baseline", message: `${key} shrank ${allowed} -> ${count}; update the baseline` });
  }
  for (const finding of findings.filter((item) => item.kind.startsWith("unverified-dynamic-"))) {
    const lines = changed.get(finding.file);
    if (lines === "all" || lines?.has(finding.line)) {
      failures.push({ ...finding, kind: "changed-unverified-dynamic-bind" });
    }
  }
  return failures;
}

function selfTest() {
  const findings = parseFindings([
    "a.go:10: positional-bind-mismatch: missing $2",
    "b.go:20: unverified-dynamic-bind: dynamic SQL",
    "c.go:30: unverified-dynamic-args: args spread",
  ].join("\n"));
  const baseline = { "b.go|unverified-dynamic-bind": 1, "c.go|unverified-dynamic-args": 1 };
  if (assess(findings, baseline, new Map()).length !== 1) throw new Error("hard mismatch was not isolated");
  if (!assess(findings.slice(1), baseline, new Map([["b.go", "all"]])).some((f) => f.kind === "changed-unverified-dynamic-bind")) {
    throw new Error("changed legacy dynamic file was not rejected");
  }
  if (!assess([...findings.slice(1), { file: "d.go", line: 1, kind: "unverified-dynamic-bind", message: "new" }], baseline, new Map()).some((f) => f.kind === "dynamic-debt-growth")) {
    throw new Error("new dynamic debt was not rejected");
  }
  if (!assess(findings.slice(1, 2), baseline, new Map()).some((f) => f.kind === "stale-baseline")) {
    throw new Error("baseline shrink was not enforced");
  }
  console.log("postgres bind-contract guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const scan = spawnSync("go", ["run", "./cmd/sql-bind-contract-check", "-root", "."], {
  cwd: path.join(root, "backend"),
  encoding: "utf8",
});
if (scan.status !== 0 && scan.status !== 1) {
  console.error(scan.stderr || scan.stdout);
  process.exit(scan.status ?? 2);
}
const findings = parseFindings(`${scan.stdout ?? ""}\n${scan.stderr ?? ""}`);
const baseline = JSON.parse(fs.readFileSync(baselinePath, "utf8"));
let base = process.env.GOATOS_CI_BASE || "origin/main";
if (spawnSync("git", ["rev-parse", "--verify", `${base}^{commit}`], { cwd: root }).status !== 0) base = "HEAD~1";
const failures = assess(findings, baseline, changedLines(base));
if (failures.length > 0) {
  console.error("postgres bind-contract guard FAILED:");
  for (const failure of failures) {
    const where = failure.file ? `${failure.file}:${failure.line}: ` : "";
    console.error(`  ${where}${failure.kind}: ${failure.message}`);
  }
  process.exit(1);
}
console.log(`postgres bind-contract guard passed (${findings.length} legacy dynamic findings held by a shrink-only per-file baseline)`);
