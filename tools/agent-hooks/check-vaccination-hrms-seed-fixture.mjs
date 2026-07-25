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
    // Capture diffs for migrations AND the Makefile so couplingProblems can
    // decide whether the Makefile change actually touches the seed pipeline
    // (vs an unrelated edit like registering a new CI guard target).
    if (!/^backend\/migrations\/postgres\/.*\.sql$/.test(file) && file !== "Makefile") continue;
    diffs.set(file, execFileSync("git", ["diff", "--unified=0", base, "--", file], { cwd: repo, encoding: "utf8" }));
  }
  return { files, diffs };
}

// A Makefile edit only couples to the vaccination-seed contract when it actually
// touches the seed PIPELINE (a seed target's recipe/prereqs) — NOT when it merely
// adds an unrelated name to the giant `.PHONY:` manifest line (which lists every
// target, seed ones included) or registers a new CI guard target.
const SEED_MAKEFILE_TERMS = /seed-vaccination|seed-roster|seed-shed-positions|seed-position-duties|vaccination-hrms|seed-closeout/;

function makefileTouchesSeedPipeline(diff) {
  return diff
    .split("\n")
    .filter((line) => /^\+/.test(line) && !/^\+\+\+/.test(line)) // added lines only
    .filter((line) => !/^\+\s*\.PHONY\b/.test(line)) // ignore the .PHONY manifest line
    .some((line) => SEED_MAKEFILE_TERMS.test(line));
}

// A migration couples to the vaccination-seed/config/SOP contract only when it
// actually does DDL on a CANONICAL (public) seed table. A migration that ONLY
// creates/alters/drops ceo_ai.* reporting views does not change that contract,
// even though its view SELECTs necessarily reference canonical table names
// (goats/species/vaccination/...) that match RELEVANT_MIGRATION_TERMS. Without
// this, adding a read-only ceo_ai reporting view falsely demands seed fixture +
// runbook companions (e.g. migration 000030 cube source views).
// Declared opt-out marker for operational (runtime-producer-written) tables that
// carry no seed-data contract. Must state a reason.
const SEED_CONTRACT_IGNORE = /seed-fixture-guard:ignore:\s*\S+/i;

function migrationCouplesToSeedContract(diff) {
  if (/Collapsed clean-slate baseline generated from migrations 000001\.\.000046/.test(diff)) {
    return false;
  }
  if (!RELEVANT_MIGRATION_TERMS.test(diff)) return false;
  const addedDdl = diff
    .split("\n")
    .filter((line) => /^\+/.test(line) && !/^\+\+\+/.test(line))
    .filter((line) =>
      /\b(CREATE\s+(OR\s+REPLACE\s+)?(TABLE|VIEW|MATERIALIZED\s+VIEW)|ALTER\s+TABLE|DROP\s+(TABLE|VIEW|MATERIALIZED\s+VIEW))\b/i.test(line),
    );
  // No canonical-table DDL at all → not a seed/config/SOP contract change, even
  // though a term matched. This covers operational-infra migrations that only
  // CREATE INDEX / CREATE OR REPLACE FUNCTION / add a trigger on a non-seed table
  // (e.g. outbox_messages) and merely NAME a canonical table in a comment, index
  // predicate, or validation-function body (event-type strings like
  // 'vaccination.leave.changed'). An index or trigger-function change carries no
  // seed data contract, so it needs no fixture/runbook companions.
  if (addedDdl.length === 0) return false;
  // Only-ceo_ai reporting-view DDL → not a seed contract change.
  if (addedDdl.length > 0 && addedDdl.every((line) => /\bceo_ai\./i.test(line))) return false;
  // EXPLICIT, AUDITABLE opt-out for a canonical-schema table that is purely
  // OPERATIONAL: written only by a runtime producer (scheduler/worker), never
  // authored as seed data and never rebuilt by seed closeout. Such a table
  // carries no seed-data contract, so fixtures/manifest/runbook companions would
  // be noise. This is deliberately a declared marker rather than a widened
  // heuristic: the reason is reviewable in the migration itself and cannot be
  // acquired accidentally. Mirrors the `scale-guard:ignore:` convention.
  // A migration that ALSO does DDL on a real seed table still couples, because
  // the marker only excuses the lines it annotates -- every added DDL line must
  // be covered by the marker for the migration to opt out.
  // The marker is only honoured when EVERY added DDL line is a CREATE TABLE (a
  // brand-new operational table). An ALTER/DROP of an already-seeded table can
  // never be excused this way, so the marker cannot launder a real seed-schema
  // change sitting in the same file.
  if (SEED_CONTRACT_IGNORE.test(diff)) {
    const created = new Set(
      addedDdl
        .map((line) => /\bCREATE\s+TABLE\b(?:\s+IF\s+NOT\s+EXISTS)?\s+([\w.]+)/i.exec(line)?.[1])
        .filter(Boolean)
        .map((name) => name.replace(/^public\./i, "").toLowerCase()),
    );
    // Every added DDL line must be either the CREATE of a brand-new table, or the
    // matching DROP of a table this same migration creates (the goose Down block).
    // An ALTER of any table, or a DROP of a table this migration did not create,
    // is a real schema change on existing (possibly seeded) data and can never be
    // excused by the marker.
    const onlyNewTableDdl = addedDdl.every((line) => {
      if (/\bALTER\s+TABLE\b/i.test(line)) return false;
      if (/\bCREATE\s+TABLE\b/i.test(line)) return true;
      const dropped = /\bDROP\s+TABLE\b(?:\s+IF\s+EXISTS)?\s+([\w.]+)/i.exec(line)?.[1];
      if (!dropped) return false;
      return created.has(dropped.replace(/^public\./i, "").toLowerCase());
    });
    if (onlyNewTableDdl) return false;
  }
  return true;
}

export function couplingProblems(files, diffs = new Map()) {
  const relevant =
    files.some((file) => file !== "Makefile" && CONTRACT_SOURCES.includes(file)) ||
    (files.includes("Makefile") && makefileTouchesSeedPipeline(diffs.get("Makefile") ?? "")) ||
    files.some((file) => {
      if (!/^backend\/migrations\/postgres\/.*\.sql$/.test(file)) return false;
      return migrationCouplesToSeedContract(diffs.get(file) ?? "");
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
  mutate("closed health case mapping", (b) => {
    b.manifest.contracts.health_case_log_normalization.Closed = "recovering";
  }, "Closed->healthy");
  mutate("fine health case mapping", (b) => {
    b.manifest.contracts.health_case_log_normalization.Fine = "recovering";
  }, "Fine->healthy");
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
  // ceo_ai-only reporting-view migration must NOT couple, even when its view
  // SELECT references canonical seed table names.
  const ceoAiOnlyMigration = new Map([[
    "backend/migrations/postgres/000999_ceo_ai_view.sql",
    "+CREATE OR REPLACE VIEW ceo_ai.vaccination_obligations_base AS\n+SELECT g.species FROM obligation_instances o LEFT JOIN goats g ON g.goat_id = o.target_id;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000999_ceo_ai_view.sql"], ceoAiOnlyMigration).length !== 0) {
    throw new Error("contract coupling self-test wrongly flagged a ceo_ai-only reporting-view migration");
  }
  // An OPERATIONAL table declaring the explicit opt-out marker must NOT couple.
  const operationalMigration = new Map([[
    "backend/migrations/postgres/000996_members.sql",
    "+-- seed-fixture-guard:ignore: operational scheduler-written membership; no seed data contract\n+CREATE TABLE public.vaccination_drive_assignment_members (tenant_id uuid NOT NULL);\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000996_members.sql"], operationalMigration).length !== 0) {
    throw new Error("contract coupling self-test wrongly flagged a marker-declared operational table migration");
  }
  // ADVERSARIAL: the same operational DDL WITHOUT the marker must still couple,
  // so the opt-out can never be acquired by accident.
  const operationalNoMarker = new Map([[
    "backend/migrations/postgres/000995_members_nomarker.sql",
    "+CREATE TABLE public.vaccination_drive_assignment_members (tenant_id uuid NOT NULL);\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000995_members_nomarker.sql"], operationalNoMarker).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test let an UNMARKED operational-table migration skip its companions");
  }
  // ADVERSARIAL: a marker must not launder a REAL seed-table alter in the same file.
  const markerLaunderingSeedAlter = new Map([[
    "backend/migrations/postgres/000994_launder.sql",
    "+-- seed-fixture-guard:ignore: pretending this is operational\n+ALTER TABLE goats ADD COLUMN species text;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000994_launder.sql"], markerLaunderingSeedAlter).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test let a marker launder an ALTER of a real seed table");
  }
  // A migration that ALTERs a canonical (public) seed table must still couple.
  const canonicalMigration = new Map([[
    "backend/migrations/postgres/000998_alter_goats.sql",
    "+ALTER TABLE goats ADD COLUMN species text;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000998_alter_goats.sql"], canonicalMigration).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test missed a canonical seed-table migration");
  }
  // An index-only migration that merely NAMES a canonical table in its predicate
  // (e.g. a partial unique index keyed on a vaccination event_type) must NOT couple.
  const indexOnlyMigration = new Map([[
    "backend/migrations/postgres/000997_cascade_index.sql",
    "+CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_leave_idx\n+  ON public.outbox_messages (tenant_id, idempotency_key)\n+  WHERE (event_type = 'vaccination.leave.changed');\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000997_cascade_index.sql"], indexOnlyMigration).length !== 0) {
    throw new Error("contract coupling self-test wrongly flagged an index-only migration naming a vaccination event_type");
  }
  // A trigger-function-replace migration that references canonical tables in its
  // body (referential-integrity checks) but does no seed-table DDL must NOT couple.
  const functionOnlyMigration = new Map([[
    "backend/migrations/postgres/000996_validate_fn.sql",
    "+CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant() RETURNS trigger AS $$\n+BEGIN\n+  IF NEW.aggregate_type = 'absence' THEN\n+    IF NOT EXISTS (SELECT 1 FROM workforce_absences WHERE absence_id = NEW.aggregate_id) THEN\n+      RAISE EXCEPTION 'vaccination cascade absence missing';\n+    END IF;\n+  END IF;\n+  RETURN NEW;\n+END; $$ LANGUAGE plpgsql;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000996_validate_fn.sql"], functionOnlyMigration).length !== 0) {
    throw new Error("contract coupling self-test wrongly flagged a trigger-function-only migration referencing canonical tables");
  }
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
