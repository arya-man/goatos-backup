#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadFixture, validateFixture, validateLoadedFixture } from "../dev/vaccination-hrms-fixture-lib.mjs";
import { auditSourceDirectory } from "../dev/validate-vaccination-hrms-source.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const fixture = path.join(repo, "fixtures/vaccination-hrms-source-full");

const CONTRACT_SOURCES = [
  "backend/cmd/seed-vaccination-real/main.go",
  "backend/cmd/seed-vaccination-trigger/main.go",
  "backend/cmd/seed-roster-real/main.go",
  "backend/cmd/seed-shed-positions/main.go",
  "backend/cmd/seed-position-duties/main.go",
  "backend/internal/vaccination/app/schedule_policy.go",
  "backend/internal/vaccination/app/generation.go",
  "Makefile",
];
const REQUIRED_COMPANIONS = [
  "fixtures/vaccination-hrms-source-full/manifest.json",
  "tools/dev/vaccination-hrms-fixture-lib.mjs",
  "tools/dev/validate-vaccination-hrms-source.mjs",
  "docs/runbooks/source-seed-data-validation.md",
  "docs/runbooks/vaccination-seed-source-date-contract.md",
  "docs/decisions/scale-anti-patterns.md",
  ".agents/skills/goatos-build/SKILL.md",
];
const RELEVANT_MIGRATION_TERMS = /vaccination|protocol_versions|protocol_rules|sop_versions|workforce_members|workforce_positions|position_module_duties|\bgoats\b|management_stage|\bdob\b|species/i;

function changedFilesAndDiffs() {
  let base;
  try {
    base = execFileSync("git", ["merge-base", "HEAD", "origin/main"], { cwd: repo, encoding: "utf8" }).trim();
  } catch {
    return { files: [], diffs: new Map() };
  }
  const trackedChanges = execFileSync("git", ["diff", "--name-only", base, "--"], { cwd: repo, encoding: "utf8" })
    .trim().split("\n").filter(Boolean);
  const untracked = execFileSync("git", ["ls-files", "--others", "--exclude-standard"], { cwd: repo, encoding: "utf8" })
    .trim().split("\n").filter(Boolean);
  const files = [...new Set([...trackedChanges, ...untracked])];
  const diffs = new Map();
  for (const file of trackedChanges) {
    if (!/^backend\/migrations\/postgres\/.*\.sql$/.test(file)) continue;
    diffs.set(file, execFileSync("git", ["diff", "--unified=0", base, "--", file], { cwd: repo, encoding: "utf8" }));
  }
  return { files, diffs };
}

export function couplingProblems(files, diffs = new Map()) {
  const relevant = files.some((file) => CONTRACT_SOURCES.includes(file)) || files.some((file) => {
    if (!/^backend\/migrations\/postgres\/.*\.sql$/.test(file)) return false;
    return RELEVANT_MIGRATION_TERMS.test(diffs.get(file) ?? "");
  });
  if (!relevant) return [];
  return REQUIRED_COMPANIONS
    .filter((file) => !files.includes(file))
    .map((file) => `seed/config/SOP contract changed without required companion ${file}`);
}

export function seedOrderingProblems(makefileText) {
  const lines = makefileText.split("\n");
  const start = lines.findIndex((line) => line.startsWith("seed-vaccination-source-full:"));
  if (start < 0) return ["Makefile is missing seed-vaccination-source-full"];
  let end = start + 1;
  while (end < lines.length && !/^[A-Za-z0-9_.-]+:/.test(lines[end])) end += 1;
  const prerequisites = lines[start].slice(lines[start].indexOf(":") + 1);
  const body = lines.slice(start + 1, end).join("\n");
  const auditAt = body.indexOf("validate-vaccination-hrms-source.mjs");
  const firstWriteAt = ["seed-dev-email-grants", "seed-roster-real", "seed-vaccination-real"]
    .map((token) => body.indexOf(token)).filter((index) => index >= 0).sort((a, b) => a - b)[0] ?? -1;
  const problems = [];
  if (/seed-dev-email-grants|seed-roster-real|seed-vaccination-real/.test(prerequisites)) {
    problems.push("DB-mutating seed command must not be a seed-vaccination-source-full prerequisite");
  }
  if (auditAt < 0 || firstWriteAt < 0 || auditAt > firstWriteAt) {
    problems.push("exact selected source audit must be the first seed-vaccination-source-full recipe step before every DB write");
  }
  if (!body.includes('"$(GOATOS_VACCINATION_SOURCE_DIR)"')) {
    problems.push("seed-vaccination-source-full audit must validate GOATOS_VACCINATION_SOURCE_DIR exactly");
  }
  return problems;
}

function runSelfTest() {
  const original = loadFixture(fixture);
  const baseline = validateLoadedFixture(original, { checkHashes: false });
  if (baseline.length) throw new Error(`baseline fixture invalid during self-test: ${baseline.join("; ")}`);

  const mutate = (label, fn, expected) => {
    const clone = structuredClone(original);
    fn(clone);
    const problems = validateLoadedFixture(clone, { checkHashes: false });
    if (!problems.some((problem) => problem.includes(expected))) {
      throw new Error(`${label}: detector missed ${expected}; got ${problems.join("; ")}`);
    }
  };
  mutate("species", (b) => { b.goats.values[1][b.goats.values[0].indexOf("species")] = ""; }, "invalid explicit species");
  mutate("pre-birth", (b) => {
    const headers = b.goats.values[0];
    b.goats.values[1][headers.indexOf("dob")] = "2026-07-01";
    b.vaccination.values[2][11] = "2026-06-01";
  }, "before DOB");
  mutate("unresolved roster", (b) => { b.roster[1][b.roster[0].indexOf("confidence")] = "UNRESOLVED"; }, "unresolved position");
  mutate("missing Park Head", (b) => {
    const h = b.roster[0];
    b.roster = b.roster.filter((row, index) => index === 0 || !(row[h.indexOf("center")] === "CBE" && row[h.indexOf("timetable_position")] === "Park Head"));
  }, "missing CBE Park Head");
  mutate("missing backup", (b) => { b.shedManagers[1][b.shedManagers[0].indexOf("backup_manager_code")] = ""; }, "unknown backup");
  mutate("provisional owner", (b) => { b.shedManagers[1][b.shedManagers[0].indexOf("needs_review")] = "true"; }, "needs_review must be false");
  mutate("PII header", (b) => { b.attendance.values[0][1] = "Bank Account Number"; }, "forbidden PII/payroll header");
  mutate("proof mode", (b) => { b.manifest.contracts.proof_mode = "per_goat_video"; }, "proof_mode must be shed_level_video");
  mutate("proof grain", (b) => { b.manifest.contracts.video_proof_subject_scope = "goat"; }, "video proof subject must be shed");
  mutate("proof capture source", (b) => { b.manifest.contracts.video_capture_sources = ["in_app_camera"]; }, "camera and gallery picker");
  mutate("stale days in stage", (b) => {
    const headers = b.goats.values[0];
    const index = b.goats.values.findIndex((row, i) => i > 0 && row[headers.indexOf("stage_entry_date")] && row[headers.indexOf("days_in_stage")]);
    if (index < 0) throw new Error("self-test fixture has no populated days_in_stage row");
    b.goats.values[index][headers.indexOf("days_in_stage")] = String(Number(b.goats.values[index][headers.indexOf("days_in_stage")]) + 1);
  }, "days_in_stage is stale");
  mutate("vaccination cell changed counter", (b) => {
    b.corrections.counts.source_vaccination_cells_changed = 1;
    b.manifest.counts.corrections.source_vaccination_cells_changed = 1;
  }, "source vaccination cells changed");

  const audit = auditSourceDirectory(fixture);
  if (!audit.valid_for_direct_seed) throw new Error(`source preflight rejected baseline fixture: ${JSON.stringify(audit.checks.filter((check) => check.status === "fail"))}`);
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), "goatos-seed-source-self-test-"));
  try {
    fs.cpSync(fixture, temp, { recursive: true });
    const goatsPath = path.join(temp, "goats.json");
    const goats = JSON.parse(fs.readFileSync(goatsPath, "utf8"));
    const headers = goats.values[0];
    goats.values[1][headers.indexOf("death_date")] = "2020-01-01";
    fs.writeFileSync(goatsPath, `${JSON.stringify(goats)}\n`);
    const badAudit = auditSourceDirectory(temp);
    if (!badAudit.checks.some((check) => check.id === "vaccination_after_terminal" && check.status === "fail")) {
      throw new Error("generic source preflight self-test missed vaccination-after-terminal corruption");
    }
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }

  const coupled = couplingProblems(
    ["backend/cmd/seed-vaccination-real/main.go"],
    new Map(),
  );
  if (coupled.length !== REQUIRED_COMPANIONS.length) throw new Error("contract coupling self-test failed to require all companions");
  if (couplingProblems(["README.md"]).length !== 0) throw new Error("contract coupling self-test flagged unrelated docs");
  const makefile = fs.readFileSync(path.join(repo, "Makefile"), "utf8");
  if (seedOrderingProblems(makefile).length) throw new Error(`baseline seed ordering invalid: ${seedOrderingProblems(makefile).join("; ")}`);
  const bypass = makefile.replace(
    "seed-vaccination-source-full: vaccination-hrms-seed-fixture-guard",
    "seed-vaccination-source-full: vaccination-hrms-seed-fixture-guard seed-dev-email-grants",
  );
  if (!seedOrderingProblems(bypass).some((problem) => problem.includes("prerequisite"))) {
    throw new Error("seed ordering self-test missed DB-mutating prerequisite bypass");
  }
  console.log("vaccination/HRMS seed fixture guard: adversarial self-test passed");
}

function main() {
  const { problems } = validateFixture(fixture);
  const sourceAudit = auditSourceDirectory(fixture);
  for (const check of sourceAudit.checks.filter((item) => item.status === "fail")) {
    problems.push(`source preflight ${check.id} failed (${check.count}): ${check.action}`);
  }
  problems.push(...seedOrderingProblems(fs.readFileSync(path.join(repo, "Makefile"), "utf8")));
  const changed = changedFilesAndDiffs();
  problems.push(...couplingProblems(changed.files, changed.diffs));
  if (problems.length) {
    console.error("vaccination/HRMS seed fixture guard failed:");
    for (const problem of problems) console.error(`- ${problem}`);
    process.exit(1);
  }
  console.log("vaccination/HRMS seed fixture guard: fixture, relations, privacy, ownership, and change coupling passed");
}

if (process.argv.includes("--self-test")) runSelfTest(); else main();
