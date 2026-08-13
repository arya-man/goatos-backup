#!/usr/bin/env node

// check-migration-duplicate-versions.mjs — detect duplicate migration version
// numbers in backend/migrations/postgres/.
//
// Rationale: goose applies migrations by version number. Two migration files
// sharing the same numeric prefix (e.g., 000031_a.sql and 000031_b.sql) create
// a silent checksum hazard: only one will apply, and it is undefined which one
// goose selects. AGENTS.md forbids ever amending an applied migration, so a
// duplicate must be caught before it is pushed.
//
// Failure modes (each has a pure finding function, an in-process self-test, and
// a real-process exit-code self-test):
//
//   1. duplicate-migration-version — two or more .sql files under
//      backend/migrations/postgres/ share the same numeric version prefix
//      (e.g., 000031_); the guard extracts the numeric prefix from each file
//      and detects duplicates.
//
// Blind spots (native Grep/Read must still catch these):
//   - migration files outside backend/migrations/postgres/ (the guard scans
//     exactly this directory)
//   - renamed/moved migration files tracked under git but not on disk (the guard
//     scans the live directory, not git history)
//   - committed but non-.sql migration files (the guard only counts .sql files)

import { readdirSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, resolve, basename } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repo = process.env.MIGRATION_VERSION_GUARD_TEST_REPO
  ? resolve(process.env.MIGRATION_VERSION_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);

const MIGRATIONS_DIR = "backend/migrations/postgres";

// ---------------------------------------------------------------------------
// mode 1: detect duplicate version prefixes
// ---------------------------------------------------------------------------
export function findingsDuplicateMigrationVersions(migrationsDir) {
  const findings = [];
  const dir = resolve(repo, migrationsDir);

  if (!existsSync(dir)) {
    findings.push({
      rule: "duplicate-migration-version",
      message: `${migrationsDir}: directory does not exist`,
    });
    return findings;
  }

  let files;
  try {
    files = readdirSync(dir);
  } catch (err) {
    findings.push({
      rule: "duplicate-migration-version",
      message: `${migrationsDir}: failed to read directory: ${err.message}`,
    });
    return findings;
  }

  // Filter to .sql files and extract version prefixes
  const sqlFiles = files.filter((f) => f.endsWith(".sql"));
  const versionMap = new Map(); // version -> [filenames]

  for (const file of sqlFiles) {
    // Extract numeric prefix, e.g., "000031_" from "000031_shifting_verification_gate.sql"
    const match = file.match(/^(\d+)_/);
    if (!match) {
      // File doesn't start with digits, skip it (not a numbered migration)
      continue;
    }

    const version = match[1];
    if (!versionMap.has(version)) {
      versionMap.set(version, []);
    }
    versionMap.get(version).push(file);
  }

  // Report duplicates
  for (const [version, files] of versionMap.entries()) {
    if (files.length > 1) {
      findings.push({
        rule: "duplicate-migration-version",
        message: `${migrationsDir}: version ${version} has multiple files: ${files.join(", ")} — goose applies by version; duplicate versions cause silent checksum hazards`,
      });
    }
  }

  return findings;
}

function run() {
  const problems = [];
  const findings = findingsDuplicateMigrationVersions(MIGRATIONS_DIR);

  for (const f of findings) {
    problems.push(`[${f.rule}] ${f.message}`);
  }

  if (problems.length > 0) {
    console.error("migration-duplicate-versions guard failed:");
    for (const p of problems) console.error(`- ${p}`);
    process.exit(1);
  }

  console.log("migration-duplicate-versions guard: ok (no duplicate version prefixes)");
}

// ---------------------------------------------------------------------------
// self-tests
// ---------------------------------------------------------------------------

function selfTest() {
  const expectClean = (label, files) => {
    const dir = mkdtempSync(join(tmpdir(), "migration-check-"));
    try {
      for (const file of files) {
        writeFileSync(join(dir, file), "");
      }
      const got = findingsDuplicateMigrationVersions(dir);
      if (got.length !== 0) {
        throw new Error(`self-test failed: false positive [${label}]: ${JSON.stringify(got)}`);
      }
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  };

  const expectDuplicate = (label, files) => {
    const dir = mkdtempSync(join(tmpdir(), "migration-check-"));
    try {
      for (const file of files) {
        writeFileSync(join(dir, file), "");
      }
      const got = findingsDuplicateMigrationVersions(dir);
      if (!got.some((f) => f.rule === "duplicate-migration-version")) {
        throw new Error(`self-test failed: [${label}] did not flag duplicate. got: ${JSON.stringify(got)}`);
      }
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  };

  // Clean: no duplicates
  expectClean("no migrations", []);
  expectClean("single migration", ["000001_baseline.sql"]);
  expectClean("sequential migrations", [
    "000001_baseline.sql",
    "000002_second.sql",
    "000031_shifting.sql",
    "000032_next.sql",
  ]);
  // ADVERSARIAL: non-.sql files should be ignored
  expectClean("non-sql files alongside migrations", [
    "000001_baseline.sql",
    "000001_notes.txt",
    "000001_README.md",
  ]);
  // ADVERSARIAL: migrations without numeric prefix should be ignored
  expectClean("non-numbered migrations", [
    "000001_baseline.sql",
    "test_helper.sql",
    "migration_test.go",
  ]);

  // Duplicates: should flag
  expectDuplicate("two files same version", [
    "000031_a.sql",
    "000031_b.sql",
  ]);
  expectDuplicate("three files same version", [
    "000031_shifting.sql",
    "000031_migration_test.go",
    "000031_another.sql",
  ]);
  // ADVERSARIAL NEAR-MISS: 000031 vs 000310 are different versions
  expectClean("near-miss: 000031 vs 000310", [
    "000031_shifting.sql",
    "000310_much_later.sql",
  ]);

  console.log("migration-duplicate-versions guard: self-test passed");
}

function runOneExitCodeCase(label, files, expectFailure) {
  const dir = mkdtempSync(join(tmpdir(), "migration-check-"));
  try {
    mkdirSync(resolve(dir, MIGRATIONS_DIR), { recursive: true });
    for (const file of files) {
      writeFileSync(resolve(dir, MIGRATIONS_DIR, file), "");
    }
    const result = spawnSync(process.execPath, [SELF_PATH], {
      env: { ...process.env, MIGRATION_VERSION_GUARD_TEST_REPO: dir },
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
  const clean = [
    "000001_baseline.sql",
    "000002_second.sql",
    "000057_growth_director_role.sql",
    "000058_weighing_state.sql",
  ];

  runOneExitCodeCase("clean tree", clean, false);
  runOneExitCodeCase("duplicate 000057", [
    "000001_baseline.sql",
    "000057_growth_director_role_v1.sql",
    "000057_growth_director_role_v2.sql",
  ], true);
  runOneExitCodeCase("duplicate 000001", [
    "000001_baseline_a.sql",
    "000001_baseline_b.sql",
  ], true);
  runOneExitCodeCase("multiple duplicates", [
    "000001_a.sql",
    "000001_b.sql",
    "000002_x.sql",
    "000002_y.sql",
  ], true);
  runOneExitCodeCase("clean with non-sql files", [
    "000001_baseline.sql",
    "000001_notes.txt",
    "migration_test.go",
  ], false);

  console.log("migration-duplicate-versions guard: exit-code self-test passed (clean=0, 4/4 violations=non-zero)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  selfTestExitCodes();
} else {
  run();
}
