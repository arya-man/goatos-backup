#!/usr/bin/env node

// check-weighing-free-flow-guard.mjs — Weighing is FREE-FLOW: operators scan real ear-tag
// RFIDs, `weighing_observations.animal_id` is nullable per migration
// 000007_weighing_free_flow_scanned_identifier.sql, and there is deliberately NO validation
// against herd roster / vaccination tables / shed ownership before accepting a scan. The same
// scanned_identifier may legitimately appear in different weighing buckets
// (campaign_shed_id). Vaccination stays strict; weighing must never borrow its gating.
// See context/repo-audits/weighing-implementation-do-not-reopen-ledger.md (A-6, B-4, C-3)
// and docs/features/weighing/TRD.md.
//
// Fails on four failure modes inside weighing backend code
// (backend/internal/weighing/**, backend/migrations/postgres/*weighing*.sql):
//   1. vaccination-or-herd-roster-read-in-write-path — an observation write/submit function
//      joins/reads a vaccination_* table, sop_submissions*, protocol_rules, or a herd-roster
//      membership/ownership assertion before accepting a scan.
//   2. animal-id-not-null-reintroduced — a migration's Up section re-adds
//      `SET NOT NULL` on weighing_observations.animal_id.
//   3. scanned-identifier-unique-across-buckets — a migration's Up section creates a
//      UNIQUE index/constraint on scanned_identifier that does not also key on
//      campaign_shed_id, which would collapse the same scanned RFID across buckets.
//   4. reject-null-animal-id — Go write-path code returns an error because animal_id/AnimalID
//      is nil/empty without routing through the unknown-animal free-flow path.
//
// Modes:
//   (default)     scan the real weighing backend tree + weighing migrations.
//   --self-test   run adversarial good/bad fixtures for all four failure modes and exit.
//
// Blind spots (native Grep/Read must still catch these): dynamically built SQL strings
// (string concatenation/fmt.Sprintf assembling table names), reflection-based query builders,
// and any new write-path function not listed in WRITE_FN_NAMES below (extend the list when a
// new observation-accepting function is added).

import { readFileSync, readdirSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

// WEIGHING_GUARD_TEST_REPO lets the exit-code self-test point this script at a throwaway
// fixture tree instead of the real repo, so it can prove process.exit(1)/exit(0) behavior by
// spawning this same script as a child process against planted good/bad fixtures.
const repo = process.env.WEIGHING_GUARD_TEST_REPO
  ? resolve(process.env.WEIGHING_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);

const WEIGHING_DIR = "backend/internal/weighing";
const MIGRATIONS_DIR = "backend/migrations/postgres";

// Functions that accept/write a weighing observation (submit path). Extend this list when a
// new observation-writing function is added to the weighing module.
const WRITE_FN_NAMES = [
  "RecordAnimalObservation",
  "recordUnknownAnimalObservationTx",
  "RecordShedObservation",
  "classifyAnimalObservationRejection",
  "classifyShedObservationRejection",
  "SubmitIndividualScope",
];

const FORBIDDEN_TABLE_RE = /\b(?:FROM|JOIN)\s+(vaccination_\w+|sop_submissions\w*|sop_submission_items\w*|protocol_rules\w*|vaccination_completions\w*)\b/i;
const FORBIDDEN_ROSTER_FN_RE = /\b(AssertHerdRosterMembership|ValidateHerdRoster|ValidateAgainstHerdRegister|RequireShedOwnership|AssertShedOwnership)\s*\(/;

function extractFunctionBody(source, fnName) {
  // Matches `func (recv) Name(` or `func Name(` and returns the body up to the next
  // column-zero `func ` (heuristic used elsewhere in this repo's guards).
  const re = new RegExp(`\\nfunc\\s+(?:\\([^)]*\\)\\s+)?${fnName}\\s*\\(`);
  const m = re.exec("\n" + source);
  if (!m) return null;
  const start = m.index + m[0].length;
  const rest = source.slice(start);
  const nextFn = rest.search(/\nfunc\s/);
  const body = rest.slice(0, nextFn < 0 ? rest.length : nextFn);
  return body;
}

export function findingsForGoSource(rel, source) {
  const findings = [];
  for (const fn of WRITE_FN_NAMES) {
    const body = extractFunctionBody(source, fn);
    if (body == null) continue;
    if (FORBIDDEN_TABLE_RE.test(body)) {
      findings.push({
        rule: "vaccination-or-herd-roster-read-in-write-path",
        message: `${rel}: ${fn}() joins/reads a vaccination/SOP/protocol table — weighing free-flow must never gate a scan on vaccination or herd-roster state`,
      });
    }
    if (FORBIDDEN_ROSTER_FN_RE.test(body)) {
      findings.push({
        rule: "vaccination-or-herd-roster-read-in-write-path",
        message: `${rel}: ${fn}() calls a herd-roster/shed-ownership assertion — weighing free-flow must accept any scanned RFID without roster/ownership validation`,
      });
    }
    // Failure mode 4: rejecting because animal_id is null/empty without routing through the
    // unknown-animal free-flow path.
    const rejectRe = /(?:cmd\.)?AnimalID\s*==\s*(?:""|nil)[\s\S]{0,220}?return[\s\S]{0,120}?(?:Err\w*|error)/;
    const rm = rejectRe.exec(body);
    if (rm) {
      const windowStart = Math.max(0, rm.index - 200);
      const window = body.slice(windowStart, rm.index + rm[0].length + 200);
      if (!/recordUnknownAnimalObservationTx|ScannedIdentifier|unknown[A-Za-z]*Animal/i.test(window)) {
        findings.push({
          rule: "reject-null-animal-id",
          message: `${rel}: ${fn}() appears to reject an observation solely because animal_id is null/empty — free-flow must route null-animal_id scans through the unknown-animal path (scanned_identifier), never reject them`,
        });
      }
    }
  }
  return findings;
}

function upSection(sql) {
  // goose migrations mark Up/Down with `-- +goose Up` / `-- +goose Down`. Only the Up section
  // represents the forward (currently-applied) schema; the Down section legitimately restores
  // the old NOT NULL constraint as a rollback and must not be flagged.
  const downIdx = sql.search(/--\s*\+goose\s+Down/i);
  return downIdx < 0 ? sql : sql.slice(0, downIdx);
}

export function findingsForMigrationSource(rel, sql) {
  const findings = [];
  const up = upSection(sql);

  if (/weighing_observations[\s\S]{0,400}?ALTER COLUMN\s+animal_id\s+SET\s+NOT\s+NULL/i.test(up)) {
    findings.push({
      rule: "animal-id-not-null-reintroduced",
      message: `${rel}: Up section re-adds NOT NULL on weighing_observations.animal_id — this column must stay nullable per migration 000007 (free-flow scanned_identifier contract)`,
    });
  }

  const uniqueRe = /CREATE\s+UNIQUE\s+INDEX[^;]*?ON\s+(?:public\.)?weighing_observations\s*\(([^)]*)\)|ADD\s+CONSTRAINT\s+\w+\s+UNIQUE\s*\(([^)]*)\)/gi;
  let um;
  while ((um = uniqueRe.exec(up)) !== null) {
    const cols = (um[1] || um[2] || "").toLowerCase();
    if (cols.includes("scanned_identifier") && !cols.includes("campaign_shed_id")) {
      findings.push({
        rule: "scanned-identifier-unique-across-buckets",
        message: `${rel}: UNIQUE index/constraint on scanned_identifier without campaign_shed_id would collapse the same scanned RFID across different weighing buckets — include campaign_shed_id in the key or drop the uniqueness`,
      });
    }
  }
  return findings;
}

function isWeighingGo(rel) {
  return rel.startsWith(`${WEIGHING_DIR}/`) && rel.endsWith(".go");
}

function isWeighingMigration(rel) {
  return rel.startsWith(`${MIGRATIONS_DIR}/`) && /weighing/i.test(rel) && rel.endsWith(".sql");
}

function walk(dir, matcher, out) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(path, matcher, out);
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (matcher(rel)) out.push(rel);
    }
  }
  return out;
}

function run() {
  const goFiles = walk(resolve(repo, WEIGHING_DIR), isWeighingGo, []);
  const migrationFiles = walk(resolve(repo, MIGRATIONS_DIR), isWeighingMigration, []);
  const problems = [];
  for (const rel of goFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForGoSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  for (const rel of migrationFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForMigrationSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  if (problems.length > 0) {
    console.error("weighing-free-flow guard failed:");
    for (const p of problems) console.error(`- ${p}`);
    process.exit(1);
  }
  console.log(`weighing-free-flow guard: ok (${goFiles.length} weighing Go files, ${migrationFiles.length} weighing migrations)`);
}

function selfTest() {
  // Mode 1: vaccination/herd-roster read in write path.
  const badFn1 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM vaccination_completions WHERE goat_id=$1", cmd.AnimalID)
  return domain.Observation{}, nil
}
`;
  const goodFn1 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM goats g WHERE g.goat_id=$1", cmd.AnimalID)
  return domain.Observation{}, nil
}
`;
  const badFn1b = `
func (r *Repository) classifyAnimalObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) error {
  if err := RequireShedOwnership(ctx, tx, cmd.AnimalID); err != nil { return err }
  return nil
}
`;

  // Mode 4: reject on null animal_id without routing through the unknown-animal path.
  const badFn4 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if cmd.AnimalID == "" {
    return domain.Observation{}, ports.ErrInvalidArgument
  }
  return domain.Observation{}, nil
}
`;
  const goodFn4 = `
func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if !uuidutil.IsUUIDString(cmd.AnimalID) {
    return r.recordUnknownAnimalObservationTx(ctx, tx, cmd)
  }
  return domain.Observation{}, nil
}
`;

  const findings1 = findingsForGoSource("fake.go", badFn1);
  if (!findings1.some((f) => f.rule === "vaccination-or-herd-roster-read-in-write-path")) {
    throw new Error(`self-test failed: mode 1 (table) not flagged. got: ${JSON.stringify(findings1)}`);
  }
  const findings1b = findingsForGoSource("fake.go", badFn1b);
  if (!findings1b.some((f) => f.rule === "vaccination-or-herd-roster-read-in-write-path")) {
    throw new Error(`self-test failed: mode 1 (roster fn) not flagged. got: ${JSON.stringify(findings1b)}`);
  }
  if (findingsForGoSource("fake.go", goodFn1).length !== 0) {
    throw new Error("self-test failed: mode 1 false positive on clean goats-only join");
  }

  const findings4 = findingsForGoSource("fake.go", badFn4);
  if (!findings4.some((f) => f.rule === "reject-null-animal-id")) {
    throw new Error(`self-test failed: mode 4 not flagged. got: ${JSON.stringify(findings4)}`);
  }
  if (findingsForGoSource("fake.go", goodFn4).length !== 0) {
    throw new Error("self-test failed: mode 4 false positive on free-flow unknown-animal routing");
  }

  // Mode 2: NOT NULL re-add.
  const badMig2 = `
-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
`;
  const goodMig2 = `
-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
`;
  const findings2 = findingsForMigrationSource("fake.sql", badMig2);
  if (!findings2.some((f) => f.rule === "animal-id-not-null-reintroduced")) {
    throw new Error(`self-test failed: mode 2 not flagged. got: ${JSON.stringify(findings2)}`);
  }
  if (findingsForMigrationSource("fake.sql", goodMig2).length !== 0) {
    throw new Error("self-test failed: mode 2 false positive on legitimate Down-section rollback");
  }

  // Mode 3: unique constraint collapsing buckets.
  const badMig3 = `
-- +goose Up
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, scanned_identifier);
`;
  const goodMig3 = `
-- +goose Up
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, campaign_shed_id, scanned_identifier);
`;
  const findings3 = findingsForMigrationSource("fake.sql", badMig3);
  if (!findings3.some((f) => f.rule === "scanned-identifier-unique-across-buckets")) {
    throw new Error(`self-test failed: mode 3 not flagged. got: ${JSON.stringify(findings3)}`);
  }
  if (findingsForMigrationSource("fake.sql", goodMig3).length !== 0) {
    throw new Error("self-test failed: mode 3 false positive on bucket-scoped unique index");
  }

  console.log("weighing-free-flow guard: self-test passed (4/4 failure modes)");
}

// Builds a throwaway fixture repo under os.tmpdir(), writes ONE Go file and ONE migration file
// (either clean or containing exactly one planted violation), spawns THIS script as a child
// process with WEIGHING_GUARD_TEST_REPO pointed at it, and asserts the real process exit code —
// not just the in-process finding text. This is the required proof that `process.exit(1)` (not
// just console.error) actually fires for each failure mode, and that a clean tree exits 0.
function runOneExitCodeCase(label, { goSource, migrationSource }, expectFailure) {
  const dir = mkdtempSync(join(tmpdir(), "weighing-free-flow-guard-exitcode-"));
  try {
    const goDir = join(dir, "backend/internal/weighing/adapters/postgres");
    const migDir = join(dir, "backend/migrations/postgres");
    mkdirSync(goDir, { recursive: true });
    mkdirSync(migDir, { recursive: true });
    writeFileSync(join(goDir, "fixture_repo.go"), goSource ?? "package postgres\n");
    writeFileSync(join(migDir, "000900_fixture_weighing.sql"), migrationSource ?? "-- +goose Up\n-- +goose Down\n");
    const result = spawnSync(process.execPath, [SELF_PATH], {
      env: { ...process.env, WEIGHING_GUARD_TEST_REPO: dir },
      encoding: "utf8",
    });
    const status = result.status;
    const failed = status !== 0;
    if (failed !== expectFailure) {
      throw new Error(
        `exit-code self-test failed [${label}]: expected exit ${expectFailure ? "non-zero" : "0"}, got ${status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
      );
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function selfTestExitCodes() {
  const cleanGo = `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM goats g WHERE g.goat_id=$1", cmd.AnimalID)
  if !uuidutil.IsUUIDString(cmd.AnimalID) {
    return r.recordUnknownAnimalObservationTx(ctx, tx, cmd)
  }
  return domain.Observation{}, nil
}
`;
  const cleanMig = `-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, campaign_shed_id, scanned_identifier);
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
`;

  runOneExitCodeCase("clean tree", { goSource: cleanGo, migrationSource: cleanMig }, false);

  runOneExitCodeCase(
    "mode 1: vaccination table read in write path",
    {
      goSource: `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  rows, err := tx.Query(ctx, "SELECT 1 FROM vaccination_completions WHERE goat_id=$1", cmd.AnimalID)
  return domain.Observation{}, nil
}
`,
      migrationSource: cleanMig,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 4: reject-null-animal-id",
    {
      goSource: `package postgres

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
  if cmd.AnimalID == "" {
    return domain.Observation{}, ports.ErrInvalidArgument
  }
  return domain.Observation{}, nil
}
`,
      migrationSource: cleanMig,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 2: animal-id NOT NULL reintroduced",
    {
      goSource: cleanGo,
      migrationSource: `-- +goose Up
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
-- +goose Down
ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
`,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 3: scanned_identifier unique across buckets",
    {
      goSource: cleanGo,
      migrationSource: `-- +goose Up
CREATE UNIQUE INDEX weighing_observations_scanned_identifier_idx ON public.weighing_observations (tenant_id, scanned_identifier);
`,
    },
    true,
  );

  console.log("weighing-free-flow guard: exit-code self-test passed (clean=0, 4/4 violations=non-zero)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  selfTestExitCodes();
} else {
  run();
}
