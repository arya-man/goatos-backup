#!/usr/bin/env node

// Full Schedule is a canonical monthly read in the 5k-50k envelope.
// This guard blocks reintroducing the deleted vaccination schedule projection
// tables, projector command/stage, or projection_unavailable branch for
// /vaccination/schedule runtime code.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const forbidden = [
  /vaccination_schedule_projection/i,
  /vaccination_schedule_dirty/i,
  /vaccination-schedule-projection/i,
  /VaccinationScheduleProjection/,
  /ScheduleProjectionState/,
  /ScheduleRebuildSummary/,
  /ErrScheduleProjectionUnavailable/,
  /projection_unavailable[^]*vaccination schedule read model/i,
];

const scanRoots = [
  "backend",
  "apps/admin-web",
  "contracts",
  "deploy",
  "infra",
  "tools",
  "Makefile",
];

function git(args) {
  return execFileSync("git", args, { cwd: repo, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
}

function trackedFiles() {
  const raw = git(["ls-files", ...scanRoots]);
  return raw
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((file) => !file.endsWith("check-vaccination-schedule-canonical.mjs"));
}

function findingsForFiles(files, readText) {
  const findings = [];
  for (const file of files) {
    const text = readText(file);
    forbidden.forEach((pattern) => {
      if (pattern.test(text)) findings.push(`${file}: forbidden Full Schedule projection reference (${pattern})`);
    });
  }
  return findings;
}

function selfTest() {
  const bad = findingsForFiles(["backend/a.go", "tools/b.sh"], (file) =>
    file === "backend/a.go"
      ? "var _ = ErrScheduleProjectionUnavailable\n"
      : "go run ./cmd/vaccination-schedule-projection-recompute\n",
  );
  if (bad.length !== 2) throw new Error(`self-test: expected 2 findings, got ${JSON.stringify(bad)}`);
  const good = findingsForFiles(["backend/a.go"], () => "func VaccinationSchedule() { /* canonical */ }\n");
  if (good.length !== 0) throw new Error(`self-test: expected clean fixture, got ${JSON.stringify(good)}`);
  console.log("vaccination-schedule-canonical guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const files = trackedFiles();
const existingFiles = files.filter((file) => existsSync(resolve(repo, file)));
const findings = findingsForFiles(existingFiles, (file) => readFileSync(resolve(repo, file), "utf8"));
if (findings.length > 0) {
  console.error("vaccination-schedule-canonical guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}

console.log(`vaccination-schedule-canonical guard: ok (${existingFiles.length} file(s) scanned)`);
