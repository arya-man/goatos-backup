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
// The terms that make a migration part of the VACCINATION seed contract.
//
// The table names carry a leading \b deliberately. Without it, `protocol_versions` matches as a
// SUBSTRING of another module's table -- `health_protocol_versions` (the Health module's treatment
// protocols, migration 000098/000121) is a completely different table with its own seed path, and
// it was tripping this guard and demanding seven vaccination companions that have nothing to do
// with it. `\b` does not match between `_` and a letter (both are word characters), so
// `health_protocol_versions` no longer matches while a bare `protocol_versions` still does.
const RELEVANT_MIGRATION_TERMS = /vaccination|\bprotocol_versions|\bprotocol_rules|\bsop_versions|\bworkforce_members|\bworkforce_positions|\bposition_module_duties|\bgoats\b|management_stage|\bdob\b|species/i;

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
    // Capture diffs for migrations, the Makefile, and the contract sources themselves.
    //
    // Migrations and the Makefile so couplingProblems can decide whether the change
    // actually touches the seed pipeline (vs an unrelated edit like registering a new CI
    // guard target). Contract sources because their diff is where the
    // `seed-fixture-guard:ignore:` marker lives -- without their diff the marker is
    // invisible to the check and can never be honoured.
    const isMigration = /^backend\/migrations\/postgres\/.*\.sql$/.test(file);
    if (!isMigration && file !== "Makefile" && !CONTRACT_SOURCES.includes(file)) continue;
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

// RELEVANCE IS A PROPERTY OF SQL, NOT OF PROSE.
//
// RELEVANT_MIGRATION_TERMS used to be tested against the RAW diff, so an
// explanatory SQL comment was enough to couple a migration to the vaccination
// seed contract. That produced a false positive class with no schema meaning:
// weighing migrations 000058/000059 do DDL only on weighing_* tables, but their
// header comments say "nothing here references goats ... or vaccination", and
// the `seed-fixture-guard:ignore:` marker's own reason text contains the word
// "Vaccination". A migration was therefore penalised for DOCUMENTING that it is
// out of scope, and could be exonerated by deleting a comment — the guard was
// reading English, not schema.
//
// Relevance is now tested against the SQL with comments removed. This deletes
// only prose; every executable statement is still in scope, so a data migration
// such as `UPDATE public.goats SET species = ...` sitting next to a weighing
// CREATE TABLE still couples (self-test `dmlOnCanonicalTable`).
//
// KNOWN BLIND SPOTS (stated per the AGENTS.md guard-honesty rule):
//   - `--` inside a string literal (e.g. `DEFAULT 'a--b'`) is stripped as a
//     comment. No migration in this repo does that, and the failure direction is
//     a false NEGATIVE only when the seed-relevant token sits after such a `--`
//     in the same line, which the addedDdl check below would still catch for any
//     real CREATE/ALTER/DROP of a canonical table.
//   - Dollar-quoted bodies ($$ ... $$) are scanned as ordinary SQL; a `--`
//     comment inside a function body is stripped, which is the intended reading.
function stripSqlComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .split("\n")
    .map((line) => line.replace(/--.*$/, ""))
    .join("\n");
}

// A goose Down block only RESTORES the shape that existed before this migration.
// It cannot introduce a seed/config/SOP contract: whatever it recreates was
// already covered by whichever migration originally created it. Dropping a
// weighing column that carried an FK to `goats` therefore has to write
// `REFERENCES public.goats(goat_id)` in its rollback, and that mention alone
// used to make the guard demand vaccination/HRMS fixture companions for a
// weighing-only schema change.
//
// Relevance is decided on the Up block ONLY. Everything the guard actually
// protects -- a real forward change to a seeded vaccination/HRMS/goats table --
// lives in Up and is still tested exactly as before.
function upBlock(diff) {
  const down = diff.search(/^[+ -]?\s*--\s*\+goose\s+Down\b/mi);
  return down < 0 ? diff : diff.slice(0, down);
}

function migrationCouplesToSeedContract(diff) {
  if (/Collapsed clean-slate baseline generated from migrations 000001\.\.000046/.test(diff)) {
    return false;
  }
  if (!RELEVANT_MIGRATION_TERMS.test(stripSqlComments(upBlock(diff)))) return false;
  const addedDdl = diff
    .split("\n")
    .filter((line) => /^\+/.test(line) && !/^\+\+\+/.test(line))
    .filter((line) =>
      /\b(CREATE\s+(OR\s+REPLACE\s+)?(TABLE|VIEW|MATERIALIZED\s+VIEW)|ALTER\s+TABLE|DROP\s+(TABLE|VIEW|MATERIALIZED\s+VIEW))\b/i.test(line),
    )
    .filter((line) => !/\bALTER\s+TABLE\b.+\b(?:DISABLE|ENABLE)\s+TRIGGER\b/i.test(line));
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

// A CONTRACT_SOURCE change that does NOT touch the seed SOURCE contract.
//
// CONTRACT_SOURCES names whole files -- generation.go among them -- so every edit to the
// generator, including a bug fix that changes no seed input at all, demanded the entire
// fixture pipeline be touched: a manifest, two source-CSV validators, and four documents.
// Touching a source-CSV validator because a due-date calculation was corrected is not
// evidence of anything; it is ritual, and ritual companions are what make a guard start
// getting waved through.
//
// So the marker excuses a CONTRACT_SOURCE edit, and ONLY under conditions that keep it
// honest: the change must not also carry canonical-table DDL, and must not touch the
// fixture pipeline itself -- if it does, the coupling is real and the companions are the
// point. The reason is mandatory and reviewable in the diff, exactly like the migration
// marker above.
function contractSourceChangeIsExcused(files, diffs) {
  const sourceEdits = files.filter((file) => file !== "Makefile" && CONTRACT_SOURCES.includes(file));
  if (sourceEdits.length === 0) return false;
  const everyEditMarked = sourceEdits.every((file) => SEED_CONTRACT_IGNORE.test(diffs.get(file) ?? ""));
  if (!everyEditMarked) return false;
  // A migration in the same change means real schema movement: judge it on its own terms.
  const carriesCoupledMigration = files.some((file) => {
    if (!/^backend\/migrations\/postgres\/.*\.sql$/.test(file)) return false;
    return migrationCouplesToSeedContract(diffs.get(file) ?? "");
  });
  if (carriesCoupledMigration) return false;
  // Touching the fixture pipeline is itself the admission that this IS a seed contract
  // change, so the marker cannot then wave the rest of it through.
  return !files.some((file) => file.startsWith("fixtures/") || /^tools\/dev\/.*vaccination-hrms/.test(file));
}

export function couplingProblems(files, diffs = new Map()) {
  if (contractSourceChangeIsExcused(files, diffs)) return [];
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
  // A weighing-only DROP whose goose Down block has to restore an FK to `goats`
  // must NOT couple: the rollback recreates a shape that already existed, so it
  // introduces no seed/config/SOP contract.
  const downOnlyGoatsReference = new Map([[
    "backend/migrations/postgres/000993_weighing_drop.sql",
    "+-- +goose Up\n+ALTER TABLE public.weighing_observations\n+  DROP COLUMN IF EXISTS animal_id;\n+\n+-- +goose Down\n+ALTER TABLE public.weighing_observations\n+  ADD COLUMN IF NOT EXISTS animal_id uuid REFERENCES public.goats(goat_id);\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000993_weighing_drop.sql"], downOnlyGoatsReference).length !== 0) {
    throw new Error("contract coupling self-test wrongly flagged a rollback-only canonical-table reference");
  }
  // ...but the SAME term in the Up block still couples. The Down-block carve-out
  // must never become a way to launder a forward seed-schema change.
  const upBlockGoatsChange = new Map([[
    "backend/migrations/postgres/000990_goats_alter.sql",
    "+-- +goose Up\n+ALTER TABLE public.goats\n+  ADD COLUMN IF NOT EXISTS species text;\n+\n+-- +goose Down\n+ALTER TABLE public.goats DROP COLUMN IF EXISTS species;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000990_goats_alter.sql"], upBlockGoatsChange).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test let an Up-block canonical-table change skip its companions");
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
  // A migration for ANOTHER module whose table name merely CONTAINS a vaccination table name must
  // NOT couple. `health_protocol_versions` is the Health module's treatment-protocol table and has
  // no relationship to the vaccination seed contract.
  const otherModuleMigration = new Map([[
    "backend/migrations/postgres/000997_health_protocol_authoring.sql",
    "+ALTER TABLE public.health_protocol_versions ADD COLUMN created_by uuid;\n" +
      "+CREATE UNIQUE INDEX health_protocol_versions_one_draft_uq ON public.health_protocol_versions (tenant_id, disease_key, age_band) WHERE status = 'draft';\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000997_health_protocol_authoring.sql"], otherModuleMigration).length !== 0) {
    throw new Error("contract coupling self-test wrongly coupled another module's protocol table");
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
  // A marked generation.go change that carries no schema movement and touches no fixture
  // must NOT demand the whole fixture pipeline.
  const markedGeneratorFix = new Map([[
    "backend/internal/vaccination/app/generation.go",
    "+// seed-fixture-guard:ignore: corrects reconciliation of an existing obligation; reads no seed source and changes no fixture input\n+\tif err != nil { return err }",
  ]]);
  if (couplingProblems(["backend/internal/vaccination/app/generation.go"], markedGeneratorFix).length !== 0) {
    throw new Error("contract coupling self-test wrongly demanded fixture companions for a marked generator fix");
  }
  // UNMARKED, it still couples.
  const unmarkedGeneratorFix = new Map([[
    "backend/internal/vaccination/app/generation.go",
    "+\tif err != nil { return err }",
  ]]);
  if (couplingProblems(["backend/internal/vaccination/app/generation.go"], unmarkedGeneratorFix).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test let an unmarked generator change skip its companions");
  }
  // The marker must not wave through a change that ALSO moves canonical schema.
  const markedWithMigration = new Map([
    ["backend/internal/vaccination/app/generation.go", "+// seed-fixture-guard:ignore: pretending\n+\tx := 1"],
    ["backend/migrations/postgres/000993_alter.sql", "+ALTER TABLE goats ADD COLUMN species text;"],
  ]);
  if (couplingProblems(
    ["backend/internal/vaccination/app/generation.go", "backend/migrations/postgres/000993_alter.sql"],
    markedWithMigration,
  ).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test: the marker wrongly excused a change carrying canonical DDL");
  }
  // Nor one that edits the fixture pipeline, which is itself the admission.
  const markedWithFixture = new Map([
    ["backend/internal/vaccination/app/generation.go", "+// seed-fixture-guard:ignore: pretending\n+\tx := 1"],
    ["fixtures/vaccination-hrms-source-full/manifest.json", "+  \"schema_version\": 2,"],
  ]);
  if (couplingProblems(
    ["backend/internal/vaccination/app/generation.go", "fixtures/vaccination-hrms-source-full/manifest.json"],
    markedWithFixture,
  ).length === 0) {
    throw new Error("contract coupling self-test: the marker wrongly excused a change editing the fixture pipeline");
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
  // A migration whose DDL touches ONLY module-owned tables must NOT couple just
  // because its explanatory comments NAME canonical seed tables — including a
  // comment that exists precisely to say the migration is out of scope. This is
  // the weighing 000058/000059 case.
  const proseOnlyMention = new Map([[
    "backend/migrations/postgres/000993_weighing_close.sql",
    "+-- Free-flow is preserved: nothing here references goats, herd rosters, or\n+-- vaccination, and no species or dob column is added.\n+ALTER TABLE public.weighing_observations ADD COLUMN verification_status text NOT NULL DEFAULT 'pending';\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000993_weighing_close.sql"], proseOnlyMention).length !== 0) {
    throw new Error("contract coupling self-test wrongly flagged a module-only migration that merely NAMES canonical tables in comments");
  }
  // ADVERSARIAL: comment-stripping must not become a laundering channel. A
  // comment claiming the migration is weighing-only cannot excuse real DDL on a
  // canonical seed table.
  const commentDisguisedSeedAlter = new Map([[
    "backend/migrations/postgres/000992_disguised.sql",
    "+-- Weighing-only change; does not touch the herd register.\n+ALTER TABLE public.goats ADD COLUMN species text;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000992_disguised.sql"], commentDisguisedSeedAlter).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test let a reassuring comment launder an ALTER of a canonical seed table");
  }
  // ADVERSARIAL: stripping removes COMMENTS ONLY. A canonical-table DML data
  // migration riding alongside module-owned DDL must still couple.
  const dmlOnCanonicalTable = new Map([[
    "backend/migrations/postgres/000991_backfill.sql",
    "+-- weighing work items\n+CREATE TABLE public.weighing_work_items (tenant_id uuid NOT NULL);\n+UPDATE public.goats SET species = 'goat' WHERE species IS NULL;\n",
  ]]);
  if (couplingProblems(["backend/migrations/postgres/000991_backfill.sql"], dmlOnCanonicalTable).length !== REQUIRED_COMPANIONS.length) {
    throw new Error("contract coupling self-test let a canonical-table DML backfill skip its companions");
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
