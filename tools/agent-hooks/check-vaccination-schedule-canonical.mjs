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

function invariantFindings(readText) {
  const checks = [
    {
      file: "backend/internal/calendar/adapters/postgres/targets.go",
      fragments: [
        "JOIN vaccination_drive_assignments vda",
        "vda.planned_date = $3::date",
        "vda.park_id = $4::uuid",
      ],
      message: "Calendar L3 drive roster must resolve parkdrive members from vaccination_drive_assignments.planned_date, not stale obligation_batches.planned_date",
    },
    {
      file: "backend/internal/processintegrity/adapters/postgres/repository.go",
      fragments: [
        "vda.assignment_planned_at",
        "(assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at",
        "COALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at)",
        "COALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AS execution_due_at",
      ],
      message: "Action Center / Protocol Adherence / Control Tower / Workflows must prefer operator assignment date before batch or obligation dates",
    },
    {
      file: "backend/internal/vaccinationexecution/adapters/postgres/repository.go",
      fragments: [
        "COALESCE(override.override_date, vda.planned_date) AS effective_planned_date",
        "effective_planned_date AS planned_date",
        "WHERE effective.planned_date >= $2::date",
      ],
      message: "Vaccination schedule must expose override-aware effective assignment dates",
    },
  ];
  const findings = [];
  for (const check of checks) {
    const text = readText(check.file);
    for (const fragment of check.fragments) {
      if (!text.includes(fragment)) {
        findings.push(`${check.file}: ${check.message}; missing ${JSON.stringify(fragment)}`);
      }
    }
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
  const invariantBad = invariantFindings((file) =>
    file.endsWith("targets.go")
      ? "FROM obligation_batches ob\n"
      : file.endsWith("processintegrity/adapters/postgres/repository.go")
        ? "COALESCE(raw.batch_planned_at, raw.due_at) AS execution_due_at\n"
        : "effective_planned_date AS planned_date\n",
  );
  if (invariantBad.length === 0) throw new Error("self-test: expected invariant findings for stale date sources");
  const invariantGood = invariantFindings((file) => {
    if (file.endsWith("calendar/adapters/postgres/targets.go")) {
      return "JOIN vaccination_drive_assignments vda\nAND vda.planned_date = $3::date\nvda.park_id = $4::uuid\n";
    }
    if (file.endsWith("processintegrity/adapters/postgres/repository.go")) {
      return "vda.assignment_planned_at\n(assignment.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') AS assignment_planned_at\nCOALESCE(vda.assignment_planned_at, ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata', oi.due_at)\nCOALESCE(raw.assignment_planned_at, raw.batch_planned_at, raw.due_at) AS execution_due_at\n";
    }
    return "COALESCE(override.override_date, vda.planned_date) AS effective_planned_date\neffective_planned_date AS planned_date\nWHERE effective.planned_date >= $2::date\n";
  });
  if (invariantGood.length !== 0) throw new Error(`self-test: expected clean invariant fixture, got ${JSON.stringify(invariantGood)}`);
  console.log("vaccination-schedule-canonical guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const files = trackedFiles();
const existingFiles = files.filter((file) => existsSync(resolve(repo, file)));
const findings = [
  ...findingsForFiles(existingFiles, (file) => readFileSync(resolve(repo, file), "utf8")),
  ...invariantFindings((file) => readFileSync(resolve(repo, file), "utf8")),
];
if (findings.length > 0) {
  console.error("vaccination-schedule-canonical guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}

console.log(`vaccination-schedule-canonical guard: ok (${existingFiles.length} file(s) scanned)`);
