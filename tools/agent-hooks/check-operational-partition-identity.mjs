#!/usr/bin/env node

// Guards the "partition is the operational shed" rule documented in
// docs/decisions/partition-is-operational-shed.md. It is intentionally narrow:
// it blocks the common regressions that caused operator cards/dropdowns to
// club actual sheds like Godel 1 - Part 1 under the plain base/common name.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const relevant = (rel) =>
  (
    rel.startsWith("apps/goatos-android/") ||
    rel.startsWith("apps/admin-web/") ||
    rel.startsWith("backend/internal/vaccinationexecution/") ||
    rel.startsWith("backend/internal/weighing/") ||
    rel.startsWith("contracts/openapi/")
  ) &&
  /\.(kt|go|tsx|ts|yaml|yml)$/.test(rel) &&
  !rel.includes("/build/") &&
  !rel.includes("/generated/");

function walk(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules", ".next"].includes(entry.name)) continue;
      out.push(...walk(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (relevant(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const files = new Set();
  for (const args of [
    "diff --name-only --diff-filter=d",
    "diff --cached --name-only --diff-filter=d",
  ]) {
    try {
      execSync(`git ${args}`, { cwd: repo, encoding: "utf8" })
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean)
        .filter(relevant)
        .forEach((file) => files.add(file));
    } catch {
      // fall through to committed-range checks
    }
  }
  if (files.size > 0) return [...files];

  const base = process.env.PARTITION_GUARD_BASE || "origin/main";
  for (const range of [`${base}...HEAD`, "HEAD~1...HEAD"]) {
    try {
      const ref = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${ref}^{commit}`, { cwd: repo, stdio: "ignore" });
      return execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" })
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean)
        .filter(relevant);
    } catch {
      // try fallback
    }
  }
  return [];
}

function lineOf(source, index) {
  return source.slice(0, index).split("\n").length;
}

function sourceFindings(source, rel) {
  const findings = [];
  const add = (index, rule, message) => findings.push({ rel, line: lineOf(source, index), rule, message });
  if (/partition-identity-guard:ignore/.test(source)) return findings;

  const operationalSurface =
    /Vaccination|vaccination|Weighing|weighing|ShedRow|ShedsViewModel|campaign_shed|dropdown|ShedOption|ExecutionIdentity/.test(source);
  if (!operationalSurface) return findings;

  const identityNoPartition = /data\s+class\s+\w*Identity\s*\([^)]*\bshedId\s*:\s*String(?![^)]*\bpartition\b)[^)]*\)/gs;
  for (const match of source.matchAll(identityNoPartition)) {
    add(match.index, "identity-missing-partition", "Operator work identity must resolve to the actual shed; legacy shedId alone is not enough.");
  }

  const groupNoPartition = /\.groupBy\s*\{[^}]*\b(?:shedId|taskId|batchId|driveId|campaignShedId)\b(?![^}]*\bpartition\b)[^}]*\}/gs;
  for (const match of source.matchAll(groupNoPartition)) {
    add(match.index, "grouping-missing-partition", "Operator card/dropdown grouping without the actual shed can club separate sheds.");
  }

  const labelPlainShed = /\b(?:name|label|displayName|shedLabel)\s*=\s*(?:first\.|row\.|it\.)?(?:shedName|physicalShed)\b/g;
  for (const match of source.matchAll(labelPlainShed)) {
    add(match.index, "label-missing-partition", "Visible operator shed label must render the actual shed label.");
  }

  const keyPlainShed = /\bkey\s*=\s*\{[^}]*\b(?:shedId|taskId|batchId|campaignShedId)\b(?![^}]*\bpartition\b)[^}]*\}/gs;
  for (const match of source.matchAll(keyPlainShed)) {
    add(match.index, "key-missing-partition", "Repeated operator UI keys must include partition when shed partitions exist.");
  }

  return findings;
}

function checkFiles(files) {
  const findings = [];
  for (const rel of files) {
    const abs = join(repo, rel);
    if (!statSync(abs).isFile()) continue;
    findings.push(...sourceFindings(readFileSync(abs, "utf8"), rel));
  }
  return findings;
}

function selfTest() {
  const bad = `
data class ExecutionIdentity(val shedId: String, val taskId: String?)
val cards = rows.groupBy { it.shedId }
ShedRow(name = first.shedName)
items(rows, key = { it.campaignShedId }) { row -> Text(row.shedName) }
`;
  const good = `
data class ExecutionIdentity(val shedId: String, val partition: String?, val taskId: String?)
val cards = rows.groupBy { it.shedId to it.partition }
ShedRow(name = operationalPartitionLabel(first.physicalShed, first.partition))
items(rows, key = { it.uiKey }) { row -> Text(row.label) }
`;
  const badFindings = sourceFindings(bad, "fixture.kt");
  const goodFindings = sourceFindings(good, "fixture.kt");
  if (badFindings.length < 4) {
    console.error("self-test failed: bad fixture was not rejected enough");
    process.exit(1);
  }
  if (goodFindings.length !== 0) {
    console.error("self-test failed: good fixture was rejected", goodFindings);
    process.exit(1);
  }
  console.log("operational-partition-identity self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const files = process.argv.includes("--all") ? walk(repo) : changedFiles();
const findings = checkFiles(files);
if (findings.length) {
  console.error("operational partition identity guard failed:");
  for (const f of findings) {
    console.error(`${f.rel}:${f.line} [${f.rule}] ${f.message}`);
  }
  console.error("\nSee docs/decisions/partition-is-operational-shed.md");
  process.exit(1);
}

console.log(`operational partition identity guard passed (${files.length} file(s))`);
