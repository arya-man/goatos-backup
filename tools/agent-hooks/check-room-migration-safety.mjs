#!/usr/bin/env node

// check-room-migration-safety.mjs — blocks the Room-migration anti-patterns that crash an
// already-installed Android app on update. An @Entity added to a @Database with no migration to
// create its table (the roster_timetable_cache / roster_coverage_cache defect) compiles fine and
// works on a FRESH install — Room's createAllTables makes every table — while every in-place
// upgrade crashes on open with "Migration didn't properly handle <table>". Fresh-install tests
// (a plain in-memory Room build) never exercise the upgrade path, so this is invisible without a
// migration test. See docs/decisions/room-migration-safety.md.
//
// Enforced invariants (Android @Database in apps/goatos-android):
//   R1  exportSchema = true          — without the exported golden schema JSON you cannot
//                                       MigrationTestHelper-validate a migration. exportSchema=false
//                                       (or omitted) is banned.
//   R2  golden schema committed      — schemas/<pkg.Class>/<version>.json must exist for the
//                                       declared @Database version (every version is reviewable +
//                                       is the schema MigrationTestHelper validates against).
//   R3  version bump ⇒ migration     — (diff-scoped) if a @Database `version = N` increased vs the
//                                       base, a Migration(N-1, N) must exist in the module.
//   R4  new table ⇒ created by its    — for consecutive schema JSONs vK, vK+1 that both exist, every
//       migration                      table added in vK+1 must be CREATE'd by the module's
//                                       Migration(K, K+1). This is the direct roster-bug catch.
//
// Modes:
//   (default)     diff-scoped: run only when a @Database, a *Migration*.kt, or a schemas/ file
//                 changed vs $ROOM_GUARD_BASE (or origin/main). Nothing relevant changed -> PASS.
//   --all         audit every Android @Database in the tree.
//   --self-test   run the built-in fixtures and exit.
//
// Escape hatch: append `room-migration-guard:ignore: <reason>` on the offending @Database `version`
// or `exportSchema` line for a genuinely-justified exception.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const DOC = "docs/decisions/room-migration-safety.md";

const isDatabaseKt = (rel) =>
  rel.startsWith("apps/goatos-android/") &&
  rel.endsWith(".kt") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/");

// ---- pure parsing helpers (unit-tested by --self-test) -------------------------------------

/**
 * Parse @Database declarations out of a Kotlin source file. Returns
 * [{ pkg, cls, version, exportSchema, exportLine, versionLine }] (usually one per file).
 */
export function parseDatabases(source) {
  const out = [];
  const pkg = (source.match(/^\s*package\s+([\w.]+)/m) || [])[1] || "";
  const re = /@Database\s*\(([\s\S]*?)\)\s*(?:@\w+(?:\([\s\S]*?\))?\s*)*abstract\s+class\s+(\w+)/g;
  let m;
  while ((m = re.exec(source))) {
    const args = m[1];
    const cls = m[2];
    const version = Number((args.match(/version\s*=\s*(\d+)/) || [])[1]);
    const exportMatch = args.match(/exportSchema\s*=\s*(true|false)/);
    const exportSchema = exportMatch ? exportMatch[1] === "true" : null; // null = omitted (defaults true, but we require explicit true)
    out.push({ pkg, cls, version, exportSchema, args });
  }
  return out;
}

/** Table names declared in a Room-exported schema JSON. */
export function tablesInSchema(json) {
  const entities = (json.database && json.database.entities) || [];
  return new Set(entities.map((e) => e.tableName).filter(Boolean));
}

/**
 * Does `migrationSource` contain a Migration(from, to) whose body CREATEs `table`?
 * Heuristic but robust for this codebase's `object : Migration(a, b) { ... CREATE TABLE `t` ... }`
 * shape. `table` is matched with optional backticks.
 */
const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

export function migrationCreatesTable(migrationSource, from, to, table) {
  const anchor = new RegExp(`Migration\\s*\\(\\s*${from}\\s*,\\s*${to}\\s*\\)`);
  const start = migrationSource.search(anchor);
  if (start < 0) return false;
  // Body = from the anchor to the next `Migration(` declaration or +4000 chars.
  const rest = migrationSource.slice(start + 1);
  const nextIdx = rest.search(/Migration\s*\(\s*\d+\s*,\s*\d+\s*\)/);
  const body = rest.slice(0, nextIdx < 0 ? 4000 : nextIdx);
  // Escape the table name — a table id could in principle carry regex metacharacters; match it
  // as a literal, optionally backtick-quoted.
  const createRe = new RegExp(`CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?\`?${escapeRe(table)}\`?`, "i");
  return createRe.test(body);
}

/**
 * Which required test kinds are missing from a module's test file names. The Room migration
 * discipline requires BOTH a schema-equivalence `*MigrationTest` and an upgrade-crash
 * `*UpgradeCrashTest` per @Database — a machine check of AGENTS.md's rule, not just docs.
 */
export function missingRequiredTests(testFileNames) {
  const has = (needle) => testFileNames.some((n) => n.includes(needle));
  const missing = [];
  if (!has("MigrationTest")) missing.push("*MigrationTest (schema-equivalence)");
  if (!has("UpgradeCrashTest")) missing.push("*UpgradeCrashTest (installed-APK upgrade)");
  return missing;
}

export function hasMigration(migrationSource, from, to) {
  return new RegExp(`Migration\\s*\\(\\s*${from}\\s*,\\s*${to}\\s*\\)`).test(migrationSource);
}

// ---- fs / git glue --------------------------------------------------------------------------

function moduleDirFor(absFile) {
  // .../<module>/src/main/kotlin/... -> <module>
  const idx = absFile.indexOf("/src/main/");
  return idx < 0 ? dirname(absFile) : absFile.slice(0, idx);
}

function readMigrationSource(moduleDir) {
  const srcRoot = join(moduleDir, "src/main");
  if (!existsSync(srcRoot)) return "";
  const chunks = [];
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) {
        if (e.name === "build") continue;
        walk(p);
      } else if (e.name.endsWith(".kt")) {
        const s = readFileSync(p, "utf8");
        if (s.includes("Migration(")) chunks.push(s);
      }
    }
  };
  walk(srcRoot);
  return chunks.join("\n");
}

function schemaJsonPath(moduleDir, pkg, cls, version) {
  return join(moduleDir, "schemas", `${pkg}.${cls}`, `${version}.json`);
}

/** Is `absPath` tracked by git (committed or staged)? A generated-but-uncommitted schema JSON
 *  passes `existsSync` locally yet is missing in CI / a fresh clone — R2 must catch that. */
function isGitTracked(absPath) {
  try {
    execSync(`git ls-files --error-unmatch ${JSON.stringify(relative(repo, absPath))}`, {
      cwd: repo,
      stdio: "ignore",
    });
    return true;
  } catch {
    return false;
  }
}

/** Basenames of every test .kt under the module's src/test + src/androidTest. */
function moduleTestFileNames(moduleDir) {
  const out = [];
  for (const sub of ["src/test", "src/androidTest"]) {
    const root = join(moduleDir, sub);
    if (!existsSync(root)) continue;
    const walk = (dir) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const p = join(dir, e.name);
        if (e.isDirectory()) walk(p);
        else if (e.name.endsWith(".kt")) out.push(e.name);
      }
    };
    walk(root);
  }
  return out;
}

function baseVersionOf(relFile, pkg, cls) {
  const base = process.env.ROOM_GUARD_BASE || "origin/main";
  for (const ref of [base, "HEAD~1"]) {
    try {
      execSync(`git rev-parse --verify --quiet ${ref}^{commit}`, { cwd: repo, stdio: "ignore" });
      const old = execSync(`git show ${ref}:${relFile}`, { cwd: repo, encoding: "utf8" });
      const db = parseDatabases(old).find((d) => d.cls === cls && d.pkg === pkg);
      if (db && Number.isFinite(db.version)) return db.version;
    } catch {
      /* try next ref / not present in base */
    }
  }
  return null; // new file or no base
}

function changedRelevantFiles() {
  const base = process.env.ROOM_GUARD_BASE || "origin/main";
  for (const range of [`${base}...HEAD`, "HEAD~1...HEAD"]) {
    try {
      execSync(`git rev-parse --verify --quiet ${range.split("...")[0]}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean);
    } catch {
      /* next */
    }
  }
  return null;
}

function findAllDatabaseFiles() {
  const out = [];
  const root = join(repo, "apps/goatos-android");
  if (!existsSync(root)) return out;
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) {
        if (["build", "node_modules"].includes(e.name)) continue;
        walk(p);
      } else if (isDatabaseKt(relative(repo, p)) && readFileSync(p, "utf8").includes("@Database")) {
        out.push(relative(repo, p));
      }
    }
  };
  walk(root);
  return out;
}

// ---- the check ------------------------------------------------------------------------------

function checkDatabaseFile(relFile, { diffScoped }) {
  const findings = [];
  const abs = join(repo, relFile);
  const source = readFileSync(abs, "utf8");
  const dbs = parseDatabases(source);
  const moduleDir = moduleDirFor(abs);
  const migrationSource = readMigrationSource(moduleDir);

  for (const db of dbs) {
    const ignore = new RegExp(`(exportSchema|version)[^\\n]*room-migration-guard:ignore`).test(source);
    if (ignore) continue;

    // R1: exportSchema must be explicitly true.
    if (db.exportSchema !== true) {
      findings.push({
        rel: relFile,
        rule: "export-schema-off",
        message: `@Database ${db.cls} has exportSchema ${db.exportSchema === false ? "= false" : "omitted"}; must be = true so migrations can be schema-validated (${DOC})`,
      });
    }

    if (Number.isFinite(db.version)) {
      // R2: golden schema JSON for the current version must exist AND be committed to git —
      // a generated-but-uncommitted JSON passes locally but is absent in CI / a fresh clone.
      const jsonPath = schemaJsonPath(moduleDir, db.pkg, db.cls, db.version);
      if (!existsSync(jsonPath)) {
        findings.push({
          rel: relFile,
          rule: "missing-golden-schema",
          message: `no schema JSON for ${db.cls} v${db.version} (expected ${relative(repo, jsonPath)}); enable exportSchema + commit it`,
        });
      } else if (!isGitTracked(jsonPath)) {
        findings.push({
          rel: relFile,
          rule: "schema-not-committed",
          message: `schema JSON ${relative(repo, jsonPath)} exists but is not tracked by git — commit it so CI/fresh clones can validate migrations`,
        });
      }

      // R5: the migration discipline requires BOTH test kinds per @Database (AGENTS.md) — encode
      // it as a machine check, not just docs, so it is "followed thoroughly".
      const missingTests = missingRequiredTests(moduleTestFileNames(moduleDir));
      for (const kind of missingTests) {
        findings.push({
          rel: relFile,
          rule: "missing-migration-test",
          message: `${db.cls}'s module has no ${kind} test — every @Database needs a schema-equivalence AND an upgrade-crash test`,
        });
      }

      // R3 (diff-scoped): a version bump must ship its migration.
      if (diffScoped) {
        const baseV = baseVersionOf(relFile, db.pkg, db.cls);
        if (baseV != null && db.version > baseV) {
          for (let v = baseV; v < db.version; v++) {
            if (!hasMigration(migrationSource, v, v + 1)) {
              findings.push({
                rel: relFile,
                rule: "version-bump-without-migration",
                message: `${db.cls} version rose ${baseV}->${db.version} but no Migration(${v}, ${v + 1}) exists in the module — an in-place upgrade will crash`,
              });
            }
          }
        }
      }

      // R4: every table added between consecutive committed schemas must be created by its migration.
      for (let v = 1; v < db.version; v++) {
        const prevPath = schemaJsonPath(moduleDir, db.pkg, db.cls, v);
        const nextPath = schemaJsonPath(moduleDir, db.pkg, db.cls, v + 1);
        if (!existsSync(prevPath) || !existsSync(nextPath)) continue; // historical JSON may not exist
        const prev = tablesInSchema(JSON.parse(readFileSync(prevPath, "utf8")));
        const next = tablesInSchema(JSON.parse(readFileSync(nextPath, "utf8")));
        for (const t of next) {
          if (prev.has(t)) continue;
          if (!migrationCreatesTable(migrationSource, v, v + 1, t)) {
            findings.push({
              rel: relFile,
              rule: "entity-without-migration",
              message: `table \`${t}\` is new in ${db.cls} v${v + 1} but Migration(${v}, ${v + 1}) does not CREATE it — fresh installs get it, in-place upgrades crash`,
            });
          }
        }
      }
    }
  }
  return findings;
}

// ---- self-test ------------------------------------------------------------------------------

function selfTest() {
  // parseDatabases
  const dbSrc = `package a.b\n@Database(entities = [X::class], version = 4, exportSchema = true)\nabstract class GoatDatabase : RoomDatabase() { }`;
  const parsed = parseDatabases(dbSrc);
  if (parsed.length !== 1 || parsed[0].version !== 4 || parsed[0].exportSchema !== true || parsed[0].cls !== "GoatDatabase") {
    throw new Error("self-test: parseDatabases failed: " + JSON.stringify(parsed));
  }
  if (parseDatabases(`@Database(entities=[X::class], version=2, exportSchema = false)\nabstract class D : RoomDatabase(){}`)[0].exportSchema !== false) {
    throw new Error("self-test: exportSchema=false not parsed");
  }
  if (parseDatabases(`@Database(entities=[X::class], version=1)\nabstract class D : RoomDatabase(){}`)[0].exportSchema !== null) {
    throw new Error("self-test: omitted exportSchema should be null");
  }

  // migrationCreatesTable — the roster bug shape.
  const mig = `val M34 = object : Migration(3, 4) {\n  override fun migrate(db: X) {\n    db.execSQL("CREATE TABLE IF NOT EXISTS \`scanned_goat_capture\` (...)")\n  }\n}\nval M23 = object : Migration(2, 3) { override fun migrate(db: X){ db.execSQL("CREATE TABLE \`task_detail_cache\`(...)") } }`;
  if (!migrationCreatesTable(mig, 3, 4, "scanned_goat_capture")) throw new Error("self-test: should detect CREATE in M34");
  if (migrationCreatesTable(mig, 3, 4, "roster_timetable_cache")) throw new Error("self-test: roster table must NOT be found in M34 (the bug)");
  if (!migrationCreatesTable(mig, 2, 3, "task_detail_cache")) throw new Error("self-test: should detect CREATE in M23");
  if (!hasMigration(mig, 3, 4) || hasMigration(mig, 4, 5)) throw new Error("self-test: hasMigration failed");

  // migrationCreatesTable — table name carrying a regex metacharacter must still match literally
  // (escaping), and must NOT be interpreted as a pattern.
  const metaMig = `object : Migration(1, 2) { db.execSQL("CREATE TABLE IF NOT EXISTS \`a.b(cache)\` (...)") }`;
  if (!migrationCreatesTable(metaMig, 1, 2, "a.b(cache)")) throw new Error("self-test: metachar table not matched literally");
  if (migrationCreatesTable(metaMig, 1, 2, "axb_cache_")) throw new Error("self-test: '.' was treated as a wildcard (escaping broken)");

  // tablesInSchema
  const json = { database: { entities: [{ tableName: "a" }, { tableName: "b" }] } };
  const tabs = tablesInSchema(json);
  if (!(tabs.has("a") && tabs.has("b") && tabs.size === 2)) throw new Error("self-test: tablesInSchema failed");

  // missingRequiredTests (R5)
  if (missingRequiredTests([]).length !== 2) throw new Error("self-test: empty module should miss both test kinds");
  if (missingRequiredTests(["GoatDatabaseMigrationTest.kt"]).length !== 1) throw new Error("self-test: should still miss UpgradeCrashTest");
  if (missingRequiredTests(["FooMigrationTest.kt", "BarUpgradeCrashTest.kt"]).length !== 0) throw new Error("self-test: both present should be clean");

  console.log("room-migration-safety self-test: ok");
}

// ---- main -----------------------------------------------------------------------------------

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
let diffScoped;
if (all) {
  files = findAllDatabaseFiles();
  diffScoped = false;
} else {
  const changed = changedRelevantFiles();
  if (changed === null) {
    console.log("room-migration-safety: skipped (no git diff base; run with --all to audit)");
    process.exit(0);
  }
  const touchesRoom = changed.some(
    (f) => isDatabaseKt(f) || (f.startsWith("apps/goatos-android/") && (f.includes("Migration") || f.includes("/schemas/"))),
  );
  if (!touchesRoom) {
    console.log("room-migration-safety: ok (no Room DB/migration/schema changed)");
    process.exit(0);
  }
  // Audit every @Database in the tree (a Migration/schema change can affect any DB in its module),
  // but run the diff-scoped R3 check too.
  files = findAllDatabaseFiles();
  diffScoped = true;
}

const findings = [];
for (const rel of files) {
  try {
    findings.push(...checkDatabaseFile(rel, { diffScoped }));
  } catch (e) {
    console.error(`room-migration-safety: could not analyze ${rel}: ${e.message}`);
    process.exitCode = 2;
  }
}

if (findings.length) {
  console.error(`room-migration-safety: ${findings.length} issue(s) (see ${DOC})`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}: ${f.message}`);
  process.exit(1);
}
console.log(`room-migration-safety: ok (${files.length} @Database checked)`);
