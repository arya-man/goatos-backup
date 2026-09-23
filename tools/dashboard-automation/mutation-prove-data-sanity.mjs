#!/usr/bin/env node
// Lane 2 mutation prover.
//
// "Covered" means the check FAILS when the fix is reverted. For a SQL check that means: plant
// the defect it names, show the check fires; remove it, show the check goes quiet. This runner
// does exactly that, for every check in the catalogue, against a LOCAL THROWAWAY database.
//
// It never touches stg. It refuses any DSN that is not visibly a disposable local database, and
// it refuses to run at all if its fixture file is missing or does not cover every check — a
// prover that silently proves nothing is worse than no prover.
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadCatalogue, assertSelectOnly, cappedSql } from "./check-data-sanity.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
export const fixturesPath = path.join(here, "data-sanity-mutations.json");

// A DSN this runner will write to. Local loopback only, and the database name must say it is
// disposable. Anything else — any host that is not loopback, any name without a throwaway
// marker — is refused before a connection is opened.
export function assertThrowawayDsn(dsn) {
  if (!dsn) throw new Error("no database url: set GOATOS_LANE2_THROWAWAY_DATABASE_URL to a local throwaway database");
  let url;
  try {
    url = new URL(dsn);
  } catch {
    throw new Error("the throwaway database url is not a url");
  }
  if (!["postgres:", "postgresql:"].includes(url.protocol)) throw new Error("the throwaway database url must be a postgres url");
  if (!["127.0.0.1", "localhost", "::1", "[::1]"].includes(url.hostname)) {
    throw new Error("refusing a non-loopback database: this runner writes, and may only write to a local throwaway");
  }
  const name = url.pathname.replace(/^\//, "");
  if (!/throwaway|disposable|_tmp|scratch/i.test(name)) {
    throw new Error("refusing a database whose name does not mark it disposable; name it *_throwaway");
  }
  if (/stg|staging|prod|goatos-stg/i.test(name)) throw new Error("refusing a database whose name looks like stg or production");
  return true;
}

function psql(dsn, sql, { tuplesOnly = true } = {}) {
  const bin = process.env.GOATOS_PSQL_BIN || "psql";
  const args = [dsn, "-v", "ON_ERROR_STOP=1", "-X", "-A", "-F", "\t"];
  if (tuplesOnly) args.push("-t");
  const child = spawnSync(bin, args, { encoding: "utf8", input: `${sql}\n`, timeout: 120000 });
  if (child.status !== 0) return { error: (child.stderr || child.stdout || `psql exited ${child.status}`).trim() };
  const rows = String(child.stdout ?? "")
    .split("\n")
    .filter((line) => line !== "" && !["SET", "BEGIN", "COMMIT", "ROLLBACK", "INSERT 0 1"].includes(line.trim()) && !/^INSERT 0 \d+$/.test(line.trim()) && !/^\(\d+ rows?\)$/.test(line.trim()));
  return { rows };
}

// Columns a row must supply. Anything NOT NULL without a default is filled with a
// type-appropriate placeholder so a fixture only has to state the columns that carry the defect.
let placeholderSeq = 0;

export function buildInsert(sql, table, row, required) {
  const values = { ...row };
  for (const column of required) {
    if (column.name in values) continue;
    // Unique per filled column, so a table with a UNIQUE key on a column the
    // fixture does not name cannot reject the second row it plants.
    placeholderSeq += 1;
    values[column.name] = placeholderFor(column.type, placeholderSeq);
  }
  const columns = Object.keys(values);
  const literals = columns.map((c) => values[c]);
  return `insert into public.${table} (${columns.map((c) => `"${c}"`).join(", ")}) values (${literals.join(", ")})`;
}

export function placeholderFor(type, seq = 0) {
  const t = String(type).toLowerCase();
  if (t === "uuid") return "'00000000-0000-4000-8000-000000000001'::uuid";
  if (t.startsWith("timestamp")) return "now()";
  if (t === "date") return "current_date";
  if (t === "boolean") return "false";
  if (["integer", "bigint", "smallint", "numeric", "real", "double precision"].includes(t)) return "1";
  if (t === "jsonb" || t === "json") return `'{}'::${t}`;
  if (t === "array" || t.endsWith("[]")) return "'{}'";
  return `'mutation-fixture${seq ? `-${seq}` : ""}'`;
}

function requiredColumns(dsn, table) {
  const out = psql(dsn, `select column_name, data_type from information_schema.columns where table_schema='public' and table_name='${table}' and is_nullable='NO' and column_default is null order by ordinal_position`);
  if (out.error) throw new Error(`could not read the columns of ${table}: ${out.error}`);
  return out.rows.map((line) => {
    const [name, type] = line.split("\t");
    return { name, type };
  });
}

export function loadFixtures(file = fixturesPath) {
  if (!existsSync(file)) {
    // Fail closed. §8: 87 guard/input pairs exited 0 when their input file was deleted.
    throw new Error("the lane 2 mutation fixture file is missing, so nothing can be proved; refusing to report a pass");
  }
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  if (!parsed || typeof parsed.mutations !== "object" || Array.isArray(parsed.mutations)) {
    throw new Error("the lane 2 mutation fixture file has no mutations, so nothing can be proved");
  }
  return parsed;
}

// Which checks have a usable fixture, and the named reason for every one that does not.
export function fixtureCoverage(checks, fixtures) {
  const covered = [];
  const gaps = [];
  for (const check of checks) {
    const entry = fixtures.mutations[check.name];
    if (!entry) {
      gaps.push({ name: check.name, reason: "no defect has been written for this check yet, so it has never been shown to fire" });
    } else if (entry.unprovable) {
      gaps.push({ name: check.name, reason: String(entry.unprovable) });
    } else if (!Array.isArray(entry.plant) || entry.plant.length === 0) {
      gaps.push({ name: check.name, reason: "this check's defect names no rows to plant, so it has never been shown to fire" });
    } else {
      covered.push(check.name);
    }
  }
  return { covered, gaps };
}

const args = parse(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : main());

function main() {
  const catalogue = loadCatalogue();
  const checks = catalogue.checks.filter((check) => !args.only || check.name === args.only);
  const fixtures = loadFixtures();
  const dsn = args.dsn ?? process.env.GOATOS_LANE2_THROWAWAY_DATABASE_URL;
  assertThrowawayDsn(dsn);

  const report = {
    generatedAt: new Date().toISOString(),
    database: "a local throwaway clone; stg was never contacted",
    totalChecks: catalogue.checks.length,
    proved: [],
    notProved: []
  };

  const tableColumns = new Map();
  for (const check of checks) {
    const entry = fixtures.mutations[check.name];
    if (!entry || entry.unprovable || !Array.isArray(entry.plant) || !entry.plant.length) {
      report.notProved.push({
        name: check.name,
        reason: entry?.unprovable ?? "no defect has been written for this check yet, so it has never been shown to fire"
      });
      continue;
    }
    assertSelectOnly(check.sql, check.name);
    const capped = cappedSql(check.sql, 500);

    const inserts = [];
    let buildError = null;
    for (const planted of entry.plant) {
      try {
        if (!tableColumns.has(planted.table)) tableColumns.set(planted.table, requiredColumns(dsn, planted.table));
        inserts.push(buildInsert(capped, planted.table, planted.row ?? {}, tableColumns.get(planted.table)));
      } catch (error) {
        buildError = error.message;
        break;
      }
    }
    if (buildError) {
      report.notProved.push({ name: check.name, reason: `the defect could not be written into the throwaway clone: ${buildError}` });
      continue;
    }

    // Before: the check must be quiet on the clean clone, otherwise "it fired" proves nothing.
    const before = psql(dsn, `${capped}`);
    if (before.error) {
      report.notProved.push({ name: check.name, reason: `the check could not be run on the clean clone: ${before.error.split("\n")[0]}` });
      continue;
    }
    if (before.rows.length !== 0) {
      report.notProved.push({ name: check.name, reason: "the clone was not clean for this check before the defect was planted, so firing would prove nothing" });
      continue;
    }

    // Plant, run, roll back — all in one transaction, so the clone is never left dirty.
    // Foreign keys are skipped for the planted rows only; the defect itself is real data.
    const planted = psql(dsn, [
      "begin",
      "set local session_replication_role = replica",
      ...inserts,
      capped,
      "rollback"
    ].join(";\n"), { tuplesOnly: true });
    if (planted.error) {
      report.notProved.push({ name: check.name, reason: `the defect could not be planted: ${planted.error.split("\n").slice(0, 2).join(" ").slice(0, 300)}` });
      continue;
    }
    const firedRows = planted.rows.length;

    // After: the rollback removed the defect, so the check must go quiet again.
    const after = psql(dsn, capped);
    if (after.error) {
      report.notProved.push({ name: check.name, reason: `the check could not be re-run after the defect was removed: ${after.error.split("\n")[0]}` });
      continue;
    }

    if (firedRows > 0 && after.rows.length === 0) {
      report.proved.push({ name: check.name, firedRows, quietAfterRemoval: true });
    } else if (firedRows === 0) {
      report.notProved.push({ name: check.name, reason: "the defect was planted and the check stayed quiet, so this check cannot fail and is not coverage" });
    } else {
      report.notProved.push({ name: check.name, reason: "the check kept firing after the defect was removed, so what it reports is not the defect" });
    }
  }

  report.coverage = `${report.proved.length}/${report.totalChecks}`;
  const out = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/data-sanity-mutations.json"));
  mkdirSync(path.dirname(out), { recursive: true });
  writeFileSync(out, `${JSON.stringify(report, null, 2)}\n`);
  console.log(`lane 2 mutation proof: ${report.proved.length} of ${report.totalChecks} checks fired on their own defect and went quiet when it was removed`);
  for (const gap of report.notProved) console.log(`  not proved — ${gap.name}: ${gap.reason}`);
  return report.notProved.length === 0 ? 0 : 1;
}

function parse(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    if (raw[i] === "--self-test") parsed.selfTest = true;
    else if (raw[i] === "--out") parsed.out = raw[++i];
    else if (raw[i] === "--only") parsed.only = raw[++i];
    else if (raw[i] === "--dsn") parsed.dsn = raw[++i];
    else throw new Error(`unknown argument: ${raw[i]}`);
  }
  return parsed;
}

function selfTest() {
  // The DSN guard must refuse everything that is not a local throwaway, including the exact
  // names this lane reads in anger.
  for (const bad of [
    undefined,
    "",
    "not a url",
    "postgresql://user@stg-db.internal/goatos_throwaway",
    "postgresql://user@127.0.0.1/goatos",
    "postgresql://user@127.0.0.1/goatos_stg_throwaway",
    "http://127.0.0.1/goatos_throwaway"
  ]) {
    let refused = false;
    try { assertThrowawayDsn(bad); } catch { refused = true; }
    if (!refused) throw new Error(`self-test: the throwaway guard accepted ${JSON.stringify(bad)}`);
  }
  assertThrowawayDsn("postgresql://postgres@127.0.0.1:55432/goatos_lane2_throwaway");

  // Fail closed: a missing fixture file is an error, never a quiet pass.
  let refused = false;
  try { loadFixtures(path.join(here, "does-not-exist.json")); } catch { refused = true; }
  if (!refused) throw new Error("self-test: a missing fixture file must refuse, not pass");

  // Every catalogue check must be accounted for: proved, or named as a gap with a reason.
  const catalogue = loadCatalogue();
  const { covered, gaps } = fixtureCoverage(catalogue.checks, loadFixtures());
  if (covered.length + gaps.length !== catalogue.checks.length) throw new Error("self-test: the fixture coverage does not account for every check");
  for (const gap of gaps) {
    if (!gap.reason || gap.reason.length < 20) throw new Error(`self-test: the gap for ${gap.name} has no named reason`);
  }

  const insert = buildInsert("", "goats", { sex: "'f'" }, [{ name: "tenant_id", type: "uuid" }, { name: "sex", type: "text" }]);
  if (!insert.includes("\"sex\"") || !insert.includes("'f'")) throw new Error("self-test: a fixture's own value must survive");
  if (placeholderFor("text", 1) === placeholderFor("text", 2)) throw new Error("self-test: filled text must be unique per column");
  if (!insert.includes("00000000-0000-4000-8000-000000000001")) throw new Error("self-test: a required column must be filled");
  console.log(`lane 2 mutation prover: self-test passed (${covered.length} of ${catalogue.checks.length} checks have a defect to plant, ${gaps.length} named gaps)`);
  return 0;
}
