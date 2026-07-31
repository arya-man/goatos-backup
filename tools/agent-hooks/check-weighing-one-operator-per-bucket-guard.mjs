#!/usr/bin/env node

// check-weighing-one-operator-per-bucket-guard.mjs — one weighing campaign-shed bucket
// (`weighing_campaign_sheds`) has exactly ONE assigned operator. See
// context/repo-audits/weighing-implementation-do-not-reopen-ledger.md (B-1, C-1) and
// migration 000056_weighing_shed_operator_assignments.sql
// (`weighing_campaign_sheds.operator_user_id uuid NOT NULL`).
//
// Fails on three failure modes:
//   1. drop-not-null — a migration Up section drops NOT NULL on
//      weighing_campaign_sheds.operator_user_id.
//   2. second-operator-column — a migration Up section adds a second operator-identity
//      column (e.g. `operator_user_id_2`, `backup_operator_user_id`, `secondary_operator_*`)
//      to weighing_campaign_sheds.
//   3. operator-array-or-join-table — a migration Up section creates an array column
//      (`operator_user_ids uuid[]`) on weighing_campaign_sheds, or a new join table whose name
//      suggests a many-operators-per-bucket mapping (e.g. `weighing_campaign_shed_operators`).
//
// Modes:
//   (default)     scan weighing migrations + the weighing repository write path.
//   --self-test   run adversarial good/bad fixtures for all three failure modes and exit.
//
// Blind spots (native Grep/Read must still catch these): a second operator relationship
// introduced entirely outside backend/migrations/postgres/*weighing*.sql (e.g. via a generic
// shared assignment table not matched by the naming heuristics below).

import { readFileSync, readdirSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

// WEIGHING_GUARD_TEST_REPO lets the exit-code self-test point this script at a throwaway
// fixture tree instead of the real repo (see selfTestExitCodes below).
const repo = process.env.WEIGHING_GUARD_TEST_REPO
  ? resolve(process.env.WEIGHING_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);
const MIGRATIONS_DIR = "backend/migrations/postgres";

function upSection(sql) {
  const downIdx = sql.search(/--\s*\+goose\s+Down/i);
  return downIdx < 0 ? sql : sql.slice(0, downIdx);
}

export function findingsForMigrationSource(rel, sql) {
  const findings = [];
  const up = upSection(sql);

  if (/weighing_campaign_sheds[\s\S]{0,300}?ALTER COLUMN\s+operator_user_id\s+DROP\s+NOT\s+NULL/i.test(up)) {
    findings.push({
      rule: "drop-not-null",
      message: `${rel}: Up section drops NOT NULL on weighing_campaign_sheds.operator_user_id — every weighing bucket must have exactly one assigned operator (migration 000056)`,
    });
  }

  const secondColumnRe = /(?:ALTER TABLE\s+(?:public\.)?weighing_campaign_sheds\s+ADD COLUMN(?:\s+IF NOT EXISTS)?\s+|,\s*)([a-z0-9_]*operator[a-z0-9_]*)\s+uuid\b/gi;
  let scm;
  const seenCols = new Set();
  while ((scm = secondColumnRe.exec(up)) !== null) {
    const col = scm[1].toLowerCase();
    if (col !== "operator_user_id") seenCols.add(col);
  }
  for (const col of seenCols) {
    findings.push({
      rule: "second-operator-column",
      message: `${rel}: adds a second operator-identity column '${col}' to weighing_campaign_sheds — a bucket must have exactly one operator_user_id, not a second/backup operator column`,
    });
  }

  if (/weighing_campaign_sheds[\s\S]{0,300}?operator_user_ids\s+uuid\s*\[\s*\]/i.test(up)) {
    findings.push({
      rule: "operator-array-or-join-table",
      message: `${rel}: adds an operator_user_ids array column to weighing_campaign_sheds — a bucket must have exactly one operator, never an array of operators`,
    });
  }

  if (/CREATE TABLE\s+(?:IF NOT EXISTS\s+)?(?:public\.)?(\w*weighing_campaign_shed_operators?\w*|\w*weighing_shed_operators?\w*)\b/i.test(up)) {
    findings.push({
      rule: "operator-array-or-join-table",
      message: `${rel}: creates a join table mapping multiple operators to one weighing campaign shed — the one-operator-per-bucket invariant forbids a many-operators-per-bucket join table`,
    });
  }

  return findings;
}

// Repository-layer check: any INSERT into weighing_campaign_sheds selecting/looping more than
// one operator_user_id value per campaign_shed_id row is banned.
export function findingsForRepositorySource(rel, source) {
  const findings = [];
  if (/weighing_campaign_sheds/i.test(source) &&
      /for\s+_,\s*\w*[Oo]perator\w*\s*:?=\s*range\s+\w*\.?[Oo]perators?\b/.test(source)) {
    findings.push({
      rule: "operator-array-or-join-table",
      message: `${rel}: weighing_campaign_sheds write path iterates over multiple operators for one bucket — one campaign_shed_id row must have exactly one operator_user_id`,
    });
  }
  return findings;
}

function isWeighingMigration(rel) {
  return rel.startsWith(`${MIGRATIONS_DIR}/`) && /weighing/i.test(rel) && rel.endsWith(".sql");
}

function isWeighingRepoGo(rel) {
  return rel.startsWith("backend/internal/weighing/adapters/postgres/") && rel.endsWith(".go") && !rel.endsWith("_test.go");
}

function walk(dir, matcher, out) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) walk(path, matcher, out);
    else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (matcher(rel)) out.push(rel);
    }
  }
  return out;
}

function run() {
  const migrationFiles = walk(resolve(repo, MIGRATIONS_DIR), isWeighingMigration, []);
  const repoFiles = walk(resolve(repo, "backend/internal/weighing/adapters/postgres"), isWeighingRepoGo, []);
  const problems = [];
  for (const rel of migrationFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForMigrationSource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  for (const rel of repoFiles) {
    const source = readFileSync(resolve(repo, rel), "utf8");
    for (const f of findingsForRepositorySource(rel, source)) problems.push(`[${f.rule}] ${f.message}`);
  }
  if (problems.length > 0) {
    console.error("weighing-one-operator-per-bucket guard failed:");
    for (const p of problems) console.error(`- ${p}`);
    process.exit(1);
  }
  console.log(`weighing-one-operator-per-bucket guard: ok (${migrationFiles.length} weighing migrations, ${repoFiles.length} repository files)`);
}

function selfTest() {
  const badMig1 = `
-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ALTER COLUMN operator_user_id DROP NOT NULL;
`;
  const goodMig1 = `
-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN IF NOT EXISTS operator_user_id uuid;
ALTER TABLE public.weighing_campaign_sheds ALTER COLUMN operator_user_id SET NOT NULL;
`;
  const f1bad = findingsForMigrationSource("fake.sql", badMig1);
  if (!f1bad.some((f) => f.rule === "drop-not-null")) throw new Error(`self-test failed: mode 1 not flagged. got: ${JSON.stringify(f1bad)}`);
  if (findingsForMigrationSource("fake.sql", goodMig1).length !== 0) throw new Error("self-test failed: mode 1 false positive on NOT NULL migration");

  const badMig2 = `
-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN backup_operator_user_id uuid;
`;
  const goodMig2 = `
-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN IF NOT EXISTS operator_user_id uuid;
`;
  const f2bad = findingsForMigrationSource("fake.sql", badMig2);
  if (!f2bad.some((f) => f.rule === "second-operator-column")) throw new Error(`self-test failed: mode 2 not flagged. got: ${JSON.stringify(f2bad)}`);
  if (findingsForMigrationSource("fake.sql", goodMig2).length !== 0) throw new Error("self-test failed: mode 2 false positive on single operator column");

  const badMig3a = `
-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN operator_user_ids uuid[];
`;
  const badMig3b = `
-- +goose Up
CREATE TABLE public.weighing_campaign_shed_operators (
  campaign_shed_id uuid NOT NULL,
  operator_user_id uuid NOT NULL
);
`;
  const f3a = findingsForMigrationSource("fake.sql", badMig3a);
  if (!f3a.some((f) => f.rule === "operator-array-or-join-table")) throw new Error(`self-test failed: mode 3a not flagged. got: ${JSON.stringify(f3a)}`);
  const f3b = findingsForMigrationSource("fake.sql", badMig3b);
  if (!f3b.some((f) => f.rule === "operator-array-or-join-table")) throw new Error(`self-test failed: mode 3b (join table) not flagged. got: ${JSON.stringify(f3b)}`);

  const badRepo = `
func insertShed() {
  // INSERT INTO weighing_campaign_sheds (campaign_id, operator_user_id) ...
  for _, operator := range shed.Operators {
    tx.Exec(ctx, "INSERT INTO weighing_campaign_sheds (campaign_id, operator_user_id) VALUES ($1,$2)", campaignID, operator)
  }
}
`;
  const fRepoBad = findingsForRepositorySource("fake_repo.go", badRepo);
  if (!fRepoBad.some((f) => f.rule === "operator-array-or-join-table")) throw new Error(`self-test failed: repo-loop mode not flagged. got: ${JSON.stringify(fRepoBad)}`);

  console.log("weighing-one-operator-per-bucket guard: self-test passed (3/3 failure modes)");
}

// Spawns THIS script as a child process against a throwaway fixture repo and asserts the real
// process exit code (not just in-process finding text). One case per failure mode plus a clean
// baseline.
function runOneExitCodeCase(label, { migrationSource, repoGoSource }, expectFailure) {
  const dir = mkdtempSync(join(tmpdir(), "weighing-one-operator-guard-exitcode-"));
  try {
    const migDir = join(dir, "backend/migrations/postgres");
    const repoDir = join(dir, "backend/internal/weighing/adapters/postgres");
    mkdirSync(migDir, { recursive: true });
    mkdirSync(repoDir, { recursive: true });
    writeFileSync(join(migDir, "000900_fixture_weighing.sql"), migrationSource ?? "-- +goose Up\n-- +goose Down\n");
    writeFileSync(join(repoDir, "fixture_repo.go"), repoGoSource ?? "package postgres\n");
    const result = spawnSync(process.execPath, [SELF_PATH], {
      env: { ...process.env, WEIGHING_GUARD_TEST_REPO: dir },
      encoding: "utf8",
    });
    const failed = result.status !== 0;
    if (failed !== expectFailure) {
      throw new Error(
        `exit-code self-test failed [${label}]: expected exit ${expectFailure ? "non-zero" : "0"}, got ${result.status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
      );
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function selfTestExitCodes() {
  const cleanMig = `-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN IF NOT EXISTS operator_user_id uuid;
ALTER TABLE public.weighing_campaign_sheds ALTER COLUMN operator_user_id SET NOT NULL;
`;
  const cleanRepoGo = `package postgres

func insertShed() {
  tx.Exec(ctx, "INSERT INTO weighing_campaign_sheds (campaign_id, operator_user_id) VALUES ($1,$2)", campaignID, operatorID)
}
`;

  runOneExitCodeCase("clean tree", { migrationSource: cleanMig, repoGoSource: cleanRepoGo }, false);

  runOneExitCodeCase(
    "mode 1: drop-not-null",
    {
      migrationSource: `-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ALTER COLUMN operator_user_id DROP NOT NULL;
`,
      repoGoSource: cleanRepoGo,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 2: second-operator-column",
    {
      migrationSource: `-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN backup_operator_user_id uuid;
`,
      repoGoSource: cleanRepoGo,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 3: operator-array-or-join-table (array column)",
    {
      migrationSource: `-- +goose Up
ALTER TABLE public.weighing_campaign_sheds ADD COLUMN operator_user_ids uuid[];
`,
      repoGoSource: cleanRepoGo,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 3b: operator-array-or-join-table (join table)",
    {
      migrationSource: `-- +goose Up
CREATE TABLE public.weighing_campaign_shed_operators (
  campaign_shed_id uuid NOT NULL,
  operator_user_id uuid NOT NULL
);
`,
      repoGoSource: cleanRepoGo,
    },
    true,
  );

  runOneExitCodeCase(
    "mode 3c: operator-array-or-join-table (repo write loops multiple operators)",
    {
      migrationSource: cleanMig,
      repoGoSource: `package postgres

func insertShed() {
  for _, operator := range shed.Operators {
    tx.Exec(ctx, "INSERT INTO weighing_campaign_sheds (campaign_id, operator_user_id) VALUES ($1,$2)", campaignID, operator)
  }
}
`,
    },
    true,
  );

  console.log("weighing-one-operator-per-bucket guard: exit-code self-test passed (clean=0, 5/5 violations=non-zero)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  selfTestExitCodes();
} else {
  run();
}
