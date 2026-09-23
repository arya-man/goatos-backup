#!/usr/bin/env node
// Lane 6 (delta) — capture one business day's reading of the farm.
//
// WHY THIS EXISTS. Lane 2 asks "is the farm's data right at this instant". This lane asks the
// harder and more useful question: "did the farm's data change LEGALLY overnight". Transitions
// are where the bugs are, and a transition needs two readings to exist at all — so this script
// takes the reading, and check-delta.mjs judges the change between two of them.
//
// SAFETY. Every rule the 2026-09-23 outage produced is reused from lane 2 rather than rewritten:
// the same shared run lock, the same SELECT-only guard, the same read-only transaction, the same
// 15 second statement timeout, the same structural row cap, the same 150ms pause between reads.
// One connection, one read at a time. Nothing here writes to the product's database, and the
// snapshot is written to a store on the local disk — never back into the database it read.
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  INCIDENT_RULES,
  acquireRunLock,
  assertSelectOnly,
  cappedSql,
  enforcedStatementTimeoutMs,
  readOnlySql,
  readOnlyProofSql,
  reasonIsSubstantive
} from "./check-data-sanity.mjs";
import { redactText } from "./lib/redact.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
export const cataloguePath = path.join(here, "delta-snapshot.json");

/**
 * The store lives OUTSIDE the product's database, always. A snapshot written back into the
 * database it read would make this lane a writer, which it must never be.
 */
export function defaultStoreDir() {
  return process.env.GOATOS_DELTA_SNAPSHOT_DIR
    ?? path.join(process.env.HOME ?? "/tmp", ".cache/goatos-delta-snapshots");
}

/**
 * The farm's own day, never the machine's. A snapshot filed under a UTC date would put a 05:00
 * IST reading on the previous day and make two consecutive readings look like one day's worth
 * of movement when they are two, or none.
 */
export function businessDate(instant = new Date()) {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Kolkata", year: "numeric", month: "2-digit", day: "2-digit"
  }).format(instant);
}

export function isBusinessDate(text) {
  return /^\d{4}-\d{2}-\d{2}$/.test(String(text ?? "")) && !Number.isNaN(Date.parse(`${text}T00:00:00Z`));
}

export function addDays(date, days) {
  const base = new Date(`${date}T00:00:00Z`);
  base.setUTCDate(base.getUTCDate() + Number(days));
  return base.toISOString().slice(0, 10);
}

/**
 * The cutoff a movement section reads from. A movement section bounded by "yesterday" reads a
 * day of movement; unbounded it would read the whole history and stop being a daily reading.
 * A first-ever snapshot has no previous one, so it falls back to a short fixed window and SAYS
 * so in the snapshot, rather than quietly reading everything.
 */
export function movementSince(previousBusinessDate, today, fallbackDays = 3) {
  if (previousBusinessDate && isBusinessDate(previousBusinessDate)) {
    return { since: previousBusinessDate, bounded: "the previous snapshot's day" };
  }
  return { since: addDays(today, -Math.abs(fallbackDays)), bounded: `no previous snapshot, so the last ${Math.abs(fallbackDays)} days` };
}

/** A date is the only thing that may be substituted into a check's SQL, and only after proof. */
export function bindSince(sql, since) {
  if (!isBusinessDate(since)) throw new Error("the movement cutoff is not a date");
  return String(sql).replaceAll("$SINCE", `'${since}'`);
}

export function loadCatalogue(file = cataloguePath) {
  // Fail closed. A missing catalogue must never read as "no sections to take, all fine".
  if (!existsSync(file)) throw new Error("the delta snapshot catalogue is missing, so no reading can be taken");
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  if (!Array.isArray(parsed?.sections) || parsed.sections.length === 0) {
    throw new Error("the delta snapshot catalogue names no sections, so no reading can be taken");
  }
  return parsed;
}

/**
 * WHAT THE READING WAS TAKEN AGAINST. Two readings are only comparable when they are readings of
 * the same thing. A migration, a redeploy, or a different set of farms in scope between them
 * makes the difference between the two a difference in the SOFTWARE, not in the farm — and
 * reporting that as an illegal change is a false accusation.
 */
export function provenanceSql() {
  return "select coalesce(max(version)::text, 'none') as schema_version, current_database() as database_name, coalesce((select string_agg(distinct tenant_id::text, ',' order by tenant_id::text) from goats), '') as farms from goatos_schema_migrations limit 1";
}

export function parseProvenance(row) {
  return {
    schemaVersion: row?.[0] ?? null,
    databaseName: row?.[1] ?? null,
    farms: String(row?.[2] ?? "").split(",").filter(Boolean)
  };
}

/** Rows never leave un-redacted. Same rule as lane 2. */
export function redactRows(rows, columns) {
  return rows.map((row) => {
    const out = {};
    row.forEach((cell, index) => { out[columns?.[index] ?? `column_${index + 1}`] = redactText(cell === "" ? null : cell); });
    return out;
  });
}

/**
 * Whether every section this snapshot claims was actually READ. Counting what was LISTED rather
 * than what was READ is how a snapshot of nothing reads as a snapshot of a quiet farm.
 */
export function snapshotIntegrity(snapshot) {
  const names = Object.keys(snapshot?.sections ?? {});
  const read = names.filter((name) => snapshot.sections[name]?.read === true);
  const capped = read.filter((name) => snapshot.sections[name]?.capped === true);
  return {
    sectionsListed: names.length,
    sectionsRead: read.length,
    sectionsNotRead: names.length - read.length,
    sectionsCapped: capped.length,
    complete: names.length > 0 && read.length === names.length && capped.length === 0
  };
}

export function snapshotFileName(snapshot) {
  return `${snapshot.businessDate}-${snapshot.takenAt.replace(/[:.]/g, "-")}.json`;
}

/** The most recent snapshot strictly before `beforeBusinessDate`, or null. */
export function listSnapshots(storeDir) {
  if (!existsSync(storeDir)) return [];
  return readdirSync(storeDir)
    .filter((name) => name.endsWith(".json"))
    .map((name) => {
      try {
        const parsed = JSON.parse(readFileSync(path.join(storeDir, name), "utf8"));
        return { file: path.join(storeDir, name), snapshot: parsed };
      } catch { return null; }
    })
    .filter((entry) => entry && isBusinessDate(entry.snapshot?.businessDate))
    .sort((a, b) => String(a.snapshot.takenAt).localeCompare(String(b.snapshot.takenAt)));
}

export function latestSnapshotBefore(storeDir, businessDateExclusive) {
  const all = listSnapshots(storeDir).filter((entry) => entry.snapshot.businessDate < businessDateExclusive);
  return all.length ? all[all.length - 1] : null;
}

const args = parseArgs(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : main());

function main() {
  const catalogue = loadCatalogue();
  const storeDir = path.resolve(args.store ?? defaultStoreDir());
  const today = args.businessDate ?? businessDate();
  if (!isBusinessDate(today)) {
    console.error("the business date to file this reading under is not a date");
    return 2;
  }
  const previous = latestSnapshotBefore(storeDir, today);
  const { since, bounded } = movementSince(previous?.snapshot?.businessDate, today);

  const lockSkipAck = process.env.GOATOS_DASHBOARD_RUN_LOCK_SKIP_ACK ?? "";
  if (args.noRunLock && !reasonIsSubstantive(lockSkipAck)) {
    console.error("--no-run-lock needs GOATOS_DASHBOARD_RUN_LOCK_SKIP_ACK set to a real sentence saying why no lock is being taken");
    return 2;
  }
  const lock = args.noRunLock ? { acquired: true, skipped: true, shared: false, release() {} } : acquireRunLock();

  const snapshot = {
    takenAt: new Date().toISOString(),
    businessDate: today,
    lane: "delta",
    movement: { since, bounded },
    takenAgainst: { schemaVersion: null, databaseName: null, farms: [], appVersion: process.env.GOATOS_DELTA_APP_VERSION ?? null },
    incidentRules: {
      ...INCIDENT_RULES,
      statementTimeoutMs: enforcedStatementTimeoutMs(catalogue.statementTimeoutMs),
      runLockHeld: Boolean(lock.acquired) && !lock.skipped && lock.shared === true,
      runLockSkipped: Boolean(lock.skipped),
      runLockSkipReason: lock.skipped ? lockSkipAck.trim() : null
    },
    readOnly: { declared: true, proven: false },
    sections: {}
  };

  if (!lock.acquired) {
    console.error("delta snapshot refused: another sweep holds the run lock");
    return 2;
  }

  const databaseUrl = process.env.GOATOS_STG_READONLY_DATABASE_URL;
  if (!databaseUrl) {
    // Not "nothing changed". Not a snapshot at all. A snapshot with no readings in it would
    // agree with the next empty one and that agreement would read as a quiet night.
    lock.release();
    console.error("delta snapshot not taken: no read-only connection to the production data was available");
    return 1;
  }

  const bound = catalogue.sections.map((section) => ({ ...section, sql: bindSince(section.sql, since) }));
  for (const section of bound) assertSelectOnly(section.sql, section.name);

  const proof = psqlRows(databaseUrl, readOnlyProofSql());
  const proofRow = proof.rows?.[0];
  snapshot.readOnly = {
    declared: true,
    proven: Boolean(proofRow) && proofRow[1] === "on" && proofRow[2] === "on",
    user: proofRow?.[0] ?? null,
    howEnforced: "every statement runs inside BEGIN READ ONLY with default_transaction_read_only = on and is rolled back"
  };
  if (!snapshot.readOnly.proven) {
    lock.release();
    console.error("delta snapshot refused: the database session could not be proved read-only");
    return 1;
  }

  const provenance = psqlRows(databaseUrl, readOnlySql(provenanceSql(), catalogue.statementTimeoutMs));
  if (provenance.error || !provenance.rows?.[0]) {
    // Without provenance the reading cannot be compared with anything, because nothing would
    // say whether the next reading is of the same software and the same farms.
    lock.release();
    console.error("delta snapshot refused: what this reading was taken against could not be established, so it could never be compared with another");
    return 1;
  }
  snapshot.takenAgainst = { ...snapshot.takenAgainst, ...parseProvenance(provenance.rows[0]) };

  const cap = Number(catalogue.maxRowsPerSection ?? 2000);
  const timeoutMs = enforcedStatementTimeoutMs(catalogue.statementTimeoutMs);
  let index = 0;
  for (const section of bound) {
    if (index > 0) sleepMs(INCIDENT_RULES.requestDelayMs);
    index += 1;
    const started = Date.now();
    // Asks for one more than the cap, so a section that hit the cap is KNOWN to have hit it
    // rather than assumed complete. A capped reading is a partial reading, and a partial
    // reading compared with another partial reading is the "two readings agree" trap.
    const result = psqlRows(databaseUrl, readOnlySql(cappedSql(section.sql, cap + 1), timeoutMs), { columns: true });
    const tookMs = Date.now() - started;
    if (result.error) {
      snapshot.sections[section.name] = {
        read: false,
        describes: section.describes,
        reason: "this reading could not be taken from the production data",
        detail: redactText(result.error).split("\n").slice(0, 2).join(" ").slice(0, 300),
        tookMs
      };
      continue;
    }
    const capped = result.rows.length > cap;
    snapshot.sections[section.name] = {
      read: true,
      kind: section.kind,
      describes: section.describes,
      capped,
      rowCount: capped ? cap : result.rows.length,
      rows: redactRows(result.rows.slice(0, cap), result.columns),
      tookMs
    };
  }

  snapshot.integrity = snapshotIntegrity(snapshot);
  mkdirSync(storeDir, { recursive: true });
  const out = path.join(storeDir, snapshotFileName(snapshot));
  writeFileSync(out, `${JSON.stringify(snapshot, null, 2)}\n`);
  lock.release();
  const i = snapshot.integrity;
  console.log(`delta snapshot for ${today}: ${i.sectionsRead} of ${i.sectionsListed} readings taken${i.sectionsCapped ? `, ${i.sectionsCapped} of them incomplete` : ""}; stored outside the product database at ${path.relative(repo, out)}`);
  return i.complete ? 0 : 1;
}

function sleepMs(ms) {
  if (!(ms > 0)) return;
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

function psqlRows(databaseUrl, sql, { columns = false } = {}) {
  const psqlBin = process.env.GOATOS_PSQL_BIN || (existsSync("/usr/bin/psql") ? "/usr/bin/psql" : "psql");
  const psqlArgs = [databaseUrl, "-v", "ON_ERROR_STOP=1", "-X", "-F", "\t", "-A"];
  if (!columns) psqlArgs.push("-t");
  const child = spawnSync(psqlBin, psqlArgs, {
    encoding: "utf8",
    input: `${sql}\n`,
    timeout: 120000,
    env: { ...process.env, PGOPTIONS: `${process.env.PGOPTIONS ?? ""} -c default_transaction_read_only=on`.trim() }
  });
  if (child.status !== 0) return { error: redactText(child.stderr || child.stdout || `psql exited ${child.status}`) };
  const noise = new Set(["SET", "BEGIN", "COMMIT", "ROLLBACK"]);
  const lines = String(child.stdout ?? "").split("\n")
    .filter((line) => line !== "" && !noise.has(line.trim()) && !/^\(\d+ rows?\)$/.test(line.trim()));
  if (!columns) return { rows: lines.map((line) => line.split("\t")) };
  const header = lines.shift();
  return { columns: header ? header.split("\t") : [], rows: lines.map((line) => line.split("\t")) };
}

function parseArgs(argv) {
  const out = { selfTest: false, noRunLock: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--self-test") out.selfTest = true;
    else if (arg === "--no-run-lock") out.noRunLock = true;
    else if (arg === "--store") out.store = argv[++i];
    else if (arg === "--business-date") out.businessDate = argv[++i];
  }
  return out;
}

function selfTest() {
  const catalogue = loadCatalogue();
  for (const section of catalogue.sections) {
    assertSelectOnly(bindSince(section.sql, "2026-09-22"), section.name);
  }
  console.log(`delta snapshot self-test: ${catalogue.sections.length} readings are single capped read-only statements`);
  return 0;
}
