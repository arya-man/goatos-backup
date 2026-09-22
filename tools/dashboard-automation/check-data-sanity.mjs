#!/usr/bin/env node
// Lane 2 — read-only data sanity on the STG-backed production replica.
//
// Every check in data-sanity-checks.json is a single SELECT that returns only the rows that
// should not exist. Zero rows is a pass. Nothing here may ever write: the SELECT-only guard
// below refuses anything else before a connection is opened, and every statement is then run
// inside an explicit READ ONLY transaction that is rolled back.
//
// One bad check must never hide the other twenty-nine, so failures are collected and the sweep
// continues, exactly like lane 1's non-blocking route sweep.
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const cataloguePath = path.join(repo, "tools/dashboard-automation/data-sanity-checks.json");
// The history miner writes per-commit derived checks here. Consume it when it exists; never block on it.
const lanePath = path.join(repo, "tools/dashboard-automation/lane-checks.json");
const config = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/config.json"), "utf8"));

// Anything that is not a plain read. Checked against SQL with string literals and comments
// stripped, so a value like 'Deal Closed' can never trip the guard and a verb inside a literal
// can never sneak past it.
const FORBIDDEN_SQL_VERBS = [
  "insert", "update", "delete", "drop", "alter", "truncate", "grant", "revoke",
  "create", "copy", "call", "do", "merge", "vacuum", "refresh", "reindex",
  "cluster", "lock", "notify", "listen", "prepare", "execute", "set", "reset",
  "begin", "commit", "rollback", "savepoint", "into", "returning", "nextval",
  "setval", "pg_sleep", "pg_read_file", "pg_read_binary_file", "lo_import",
  "lo_export", "dblink", "pg_terminate_backend", "pg_cancel_backend"
];

export function stripSqlNoise(sql) {
  return String(sql ?? "")
    .replace(/--[^\n]*/g, " ")
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/'(?:[^']|'')*'/g, " 'literal' ")
    .replace(/\$\$[\s\S]*?\$\$/g, " ");
}

// Throws unless `sql` is exactly one read: a single SELECT (or WITH ... SELECT), no statement
// chaining, no psql meta-commands, no write verb, and a LIMIT so one check can never drag the
// replica down.
export function assertSelectOnly(sql, name = "check") {
  const raw = String(sql ?? "");
  if (!raw.trim()) throw new Error(`${name}: sql is empty`);
  if (raw.includes(";")) throw new Error(`${name}: sql must be a single statement with no ";" chaining`);
  if (/\\\s*\w/.test(raw)) throw new Error(`${name}: sql must not contain psql meta-commands`);
  // A comment can swallow whatever the runner appends, so the catalogue carries no comments.
  // Explanations belong in the entry's own fields, not inside the SQL.
  if (/--|\/\*|\*\//.test(raw)) throw new Error(`${name}: sql must not contain comments`);
  const bare = stripSqlNoise(raw);
  // Unbalanced parentheses let a check close the runner's wrapper early and set its own cap.
  // Counted on the comment- and literal-stripped text, so a ")" inside a value cannot trip it.
  let depth = 0;
  for (const character of bare) {
    if (character === "(") depth += 1;
    else if (character === ")") depth -= 1;
    if (depth < 0) throw new Error(`${name}: sql closes a parenthesis it never opened`);
  }
  if (depth !== 0) throw new Error(`${name}: sql leaves ${depth} parenthesis unclosed`);
  if (!/^\s*(select|with)\b/i.test(bare)) throw new Error(`${name}: sql must start with SELECT or WITH`);
  for (const verb of FORBIDDEN_SQL_VERBS) {
    if (new RegExp(`\\b${verb}\\b`, "i").test(bare)) {
      throw new Error(`${name}: sql must be read-only, found forbidden keyword "${verb}"`);
    }
  }
  if (/\bfor\s+(update|share|no\s+key\s+update)\b/i.test(bare)) {
    throw new Error(`${name}: sql must not take row locks`);
  }
  if (!/\blimit\s+\d+\b/i.test(bare)) throw new Error(`${name}: sql must cap its rows with LIMIT`);
  return true;
}

// The row cap must not depend on reading the SQL. A LIMIT inside a CTE satisfies any text-level
// check while the outer select still streams the whole table — `with c as (select 1 limit 1)
// select g.* from goats g, c` is a real bypass. So the runner caps structurally, by wrapping
// whatever the catalogue says in its own subquery. This holds for SQL with no LIMIT at all.
export function cappedSql(sql, maxRows) {
  const cap = Math.max(1, Number(maxRows) || 1);
  // The closing paren and the cap go on their own line so that nothing trailing in the check —
  // a comment, whitespace, anything — can reach them. Paired with the balanced-parenthesis and
  // no-comment rules in assertSelectOnly, a check cannot close the wrapper and set its own cap.
  return `select * from (\n${String(sql).trim()}\n) _capped limit ${cap}`;
}

// Wraps a verified SELECT in an explicit read-only transaction that is always rolled back.
export function readOnlySql(sql, statementTimeoutMs = 20000) {
  return [
    "begin read only",
    "set local default_transaction_read_only = on",
    `set local statement_timeout = ${Number(statementTimeoutMs) || 20000}`,
    sql,
    "rollback"
  ].join(";\n");
}

export function readOnlyProofSql() {
  return readOnlySql("select current_user, current_setting('transaction_read_only'), current_setting('default_transaction_read_only') limit 1");
}

// A read-only transaction is only half the guarantee: the role behind it must not be able to
// write at all. Anything less is recorded as a gap and parked, never waved through as a warning.
export function roleGrantProofSql(tables) {
  const values = tables.map((table) => `('${String(table).replaceAll("'", "''")}')`).join(", ");
  return readOnlySql(`with checked(table_name) as (values ${values}), scan as (select table_name, has_table_privilege(current_user, 'public.' || table_name, 'INSERT') or has_table_privilege(current_user, 'public.' || table_name, 'UPDATE') or has_table_privilege(current_user, 'public.' || table_name, 'DELETE') or has_table_privilege(current_user, 'public.' || table_name, 'TRUNCATE') as can_write from checked) select count(*)::text, has_schema_privilege(current_user, 'public', 'CREATE')::text, coalesce(string_agg(table_name, ',' order by table_name) filter (where can_write), '') from scan limit 1`);
}

// Tables named by a check's SQL, so the grant proof covers exactly what this lane reads.
export function tablesReadByCatalogue(checks) {
  const tables = new Set();
  for (const check of checks) {
    const text = stripSqlNoise(check.sql);
    const cteNames = new Set([...text.matchAll(/\b([a-z_][a-z0-9_]*)\s+as\s*\(/gi)].map((m) => m[1].toLowerCase()));
    // A trailing "." means the token was an alias ("is distinct from p.shed_id") and a trailing
    // "(" means it was a function ("is distinct from count(...)"). Neither is a table.
    for (const match of text.matchAll(/\b(?:from|join)\s+([a-z_][a-z0-9_]*)([.(]?)/gi)) {
      if (match[2]) continue;
      const name = match[1].toLowerCase();
      if (!cteNames.has(name)) tables.add(name);
    }
  }
  return [...tables].sort();
}

// Rows never leave this function un-capped or un-redacted: Slack and the receipt only ever see
// what comes back from here.
export function capAndRedactRows(rows, columns, maxRows, maxCellChars) {
  const capped = (rows ?? []).slice(0, Math.max(0, Number(maxRows) || 0));
  return capped.map((row) => {
    const out = {};
    row.forEach((cell, index) => {
      const key = columns?.[index] ?? `column_${index + 1}`;
      const text = redactText(cell === "" ? null : cell);
      out[key] = text.length > maxCellChars ? `${text.slice(0, maxCellChars - 1)}…` : text;
    });
    return out;
  });
}

// The history miner writes lane-checks.json from another process. It is untrusted input:
// every row is validated and normalised into this lane's own shape, and a row that cannot be
// normalised is parked with a reason rather than allowed anywhere near Slack. In particular a
// row never contributes Slack text unless it carries a human sentence, a screen and a unit.
export function normaliseLaneCheck(row, existingNames) {
  const name = typeof row?.name === "string" ? row.name : typeof row?.checkId === "string" ? row.checkId : null;
  if (!name) return { parked: { name: "an unnamed derived check", reason: "the derived check file offered a check with no name" } };
  if (existingNames.has(name)) return { parked: { name, reason: "a check of this name is already in the catalogue" } };
  const missing = ["sql", "humanFailure", "countUnit", "question"].filter((field) => typeof row?.[field] !== "string" || !row[field].trim());
  if (missing.length) {
    return { parked: { name, reason: "this derived check has no plain-English description yet, so it was not run" } };
  }
  if (!row?.page?.path || typeof row.page.path !== "string" || !row.page.path.startsWith("/") || !row.page.title) {
    return { parked: { name, reason: "this derived check does not name the screen it would show up on, so it was not run" } };
  }
  if (Number(row.expectRows ?? 0) !== 0) {
    return { parked: { name, reason: "this derived check does not expect zero offending rows, so it was not run" } };
  }
  try {
    assertSelectOnly(row.sql, name);
  } catch {
    return { parked: { name, reason: "this derived check is not a single capped read, so it was not run" } };
  }
  return {
    check: {
      name,
      question: row.question,
      severity: ["high", "medium", "low"].includes(row.severity) ? row.severity : "medium",
      expectRows: 0,
      page: { title: String(row.page.title), path: String(row.page.path) },
      sql: row.sql,
      humanFailure: row.humanFailure,
      countUnit: row.countUnit,
      sourceCommits: Array.isArray(row.sourceCommits) ? row.sourceCommits.map(String).slice(0, 6) : [],
      source: "lane-checks"
    }
  };
}

export function loadCatalogue({ cataloguePath: cat = cataloguePath, lanePath: lane = lanePath } = {}) {
  const base = JSON.parse(readFileSync(cat, "utf8"));
  const checks = [...(base.checks ?? [])];
  const added = [];
  const parked = [];
  if (existsSync(lane)) {
    let laneChecks = null;
    try {
      const extra = JSON.parse(readFileSync(lane, "utf8"));
      laneChecks = extra.dataSanityChecks ?? extra.lane2 ?? extra.checks ?? null;
    } catch {
      laneChecks = null;
    }
    if (!Array.isArray(laneChecks)) {
      // A malformed miner file must never stop the sweep; it is reported as parked, not fatal.
      parked.push({ name: "derived checks", reason: "the derived check file could not be read, so only this lane's own checks were run" });
    } else {
      const names = new Set(checks.map((check) => check.name));
      for (const row of laneChecks) {
        const result = normaliseLaneCheck(row, names);
        if (result.check) {
          names.add(result.check.name);
          checks.push(result.check);
          added.push(result.check.name);
        } else {
          parked.push(result.parked);
        }
      }
    }
  }
  return { ...base, checks, laneChecksAdded: added, laneChecksParked: parked };
}

// Plain English for Slack and for the runner's layer message. Deliberately free of SQL, table
// and column names, and check codes — the tests assert that.
export function layerSentence(findings, errors) {
  const parts = [];
  if (findings.length) {
    const rows = findings.reduce((sum, item) => sum + Number(item.rowCount ?? 0), 0);
    parts.push(`${findings.length} figure${findings.length === 1 ? "" : "s"} on the dashboard ${findings.length === 1 ? "does" : "do"} not add up (${rows} bad row${rows === 1 ? "" : "s"} in total)`);
  }
  if (errors.length) {
    parts.push(`${errors.length} check${errors.length === 1 ? "" : "s"} could not be run and ${errors.length === 1 ? "is" : "are"} parked`);
  }
  if (!parts.length) return "Every figure on the dashboard adds up.";
  return `${parts.join(", and ")}. The affected screens are listed in the thread.`;
}

const args = parseArgs(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (isMain) {
  if (args.selfTest) {
    selfTest();
    process.exit(0);
  }
  process.exit(main());
}

function main() {
  const catalogue = loadCatalogue();
  const maxRows = Number(catalogue.maxSampleRows ?? 10);
  const maxOffendingRows = Number(catalogue.maxOffendingRows ?? 500);
  const maxCellChars = Number(catalogue.maxCellChars ?? 80);
  const timeoutMs = Number(catalogue.statementTimeoutMs ?? 20000);
  const outPath = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/data-sanity.json"));
  mkdirSync(path.dirname(outPath), { recursive: true });

  const report = {
    generatedAt: new Date().toISOString(),
    status: "pass",
    productionUrl: config.productionUrl,
    readOnly: { declared: true, proven: false },
    checksRun: 0,
    findings: [],
    passed: [],
    parked: [],
    laneChecksAdded: catalogue.laneChecksAdded ?? []
  };
  report.parked.push(...(catalogue.laneChecksParked ?? []));

  const selected = catalogue.checks.filter((check) => !args.only || check.name === args.only);
  if (args.only && selected.length === 0) {
    fail(`no check named ${args.only} in the catalogue`);
  }

  // Refuse before opening a connection: a catalogue that is not read-only never reaches the replica.
  for (const check of selected) {
    assertSelectOnly(check.sql, check.name);
  }

  const databaseUrl = process.env.GOATOS_STG_READONLY_DATABASE_URL;
  if (!databaseUrl) {
    report.status = "parked";
    report.readOnly.reason = "GOATOS_STG_READONLY_DATABASE_URL is not set";
    report.parked.push({
      name: "connection",
      reason: "no read-only connection to the production replica was available, so no data check could be verified"
    });
    report.slackLayerMessage = "The figures on the dashboard could not be checked because the read-only connection to the production data was not available.";
    writeJson(outPath, report);
    console.log(`data-sanity parked; wrote ${path.relative(repo, outPath)}`);
    return 1;
  }

  const proof = psqlRows(databaseUrl, readOnlyProofSql());
  const proofRow = proof.rows?.[0];
  report.rowCap = { appliedBy: "the runner wraps every check in its own capped subquery", maxOffendingRows };
  report.readOnly = {
    declared: true,
    proven: Boolean(proofRow) && proofRow[1] === "on" && proofRow[2] === "on",
    user: proofRow?.[0] ?? null,
    transactionReadOnly: proofRow?.[1] ?? null,
    defaultTransactionReadOnly: proofRow?.[2] ?? null,
    howEnforced: "every statement runs inside BEGIN READ ONLY with default_transaction_read_only = on and is rolled back"
  };
  if (proof.error) report.readOnly.error = redactText(proof.error);
  // Defence in depth: prove the role itself cannot write the tables this lane reads.
  const tables = tablesReadByCatalogue(selected);
  const grant = tables.length ? psqlRows(databaseUrl, roleGrantProofSql(tables)) : { rows: [] };
  const grantRow = grant.rows?.[0];
  const writablyPrivileged = grantRow?.[2] ? grantRow[2].split(",").filter(Boolean) : [];
  report.readOnly.tablesChecked = Number(grantRow?.[0] ?? 0);
  report.readOnly.roleIsReadOnly = Boolean(grantRow) && writablyPrivileged.length === 0 && grantRow[1] !== "true";
  report.readOnly.writePrivilegedTableCount = writablyPrivileged.length;
  report.readOnly.roleCanCreateInPublicSchema = grantRow?.[1] === "true";
  if (grant.error) report.readOnly.grantProofError = redactText(grant.error);
  if (!report.readOnly.roleIsReadOnly) {
    // Not a warning. The lane can still read safely (session-level read-only, one capped SELECT
    // per call, no statement chaining), but the connection is not a genuine read-only identity,
    // so that gap is parked in the receipt instead of being claimed as a guarantee.
    report.parked.push({
      name: "read-only login",
      reason: "the login used to read production data is not a read-only login. Every check still ran inside a read-only session that cannot write, but a dedicated read-only login is still owed before this lane can claim the connection itself is safe."
    });
  }

  if (!report.readOnly.proven) {
    report.status = "fail";
    report.parked.push({
      name: "read-only session",
      reason: "the database session could not be proved read-only, so no check was run against production data"
    });
    report.slackLayerMessage = "The figures on the dashboard were not checked because the connection could not be proved read-only.";
    writeJson(outPath, report);
    console.error(`data-sanity refused to run: read-only session could not be proved; wrote ${path.relative(repo, outPath)}`);
    return 1;
  }

  for (const check of selected) {
    report.checksRun += 1;
    const started = Date.now();
    const result = psqlRows(databaseUrl, readOnlySql(cappedSql(check.sql, maxOffendingRows), timeoutMs), { columns: true });
    const tookMs = Date.now() - started;
    if (result.error) {
      // Collect and continue: a check that cannot run is parked with its reason, never faked green.
      report.parked.push({
        name: check.name,
        question: check.question,
        page: check.page ?? null,
        reason: "this check could not be run against the production replica",
        detail: redactText(result.error).split("\n").slice(0, 3).join(" ").slice(0, 400),
        tookMs
      });
      continue;
    }
    const rowCount = result.rows.length;
    const expected = Number(check.expectRows ?? 0);
    if (rowCount === expected) {
      report.passed.push({ name: check.name, question: check.question, tookMs });
      continue;
    }
    report.findings.push({
      name: check.name,
      question: check.question,
      humanFailure: check.humanFailure,
      severity: check.severity ?? "medium",
      page: check.page ?? null,
      rowCount,
      rowCountIsCapped: !check.summaryRow && rowCount >= Math.min(maxOffendingRows, sqlLimit(check.sql)),
      countUnit: check.countUnit ?? "rows that should not be there",
      sampleRows: capAndRedactRows(result.rows, result.columns, maxRows, maxCellChars),
      sourceCommits: check.sourceCommits ?? [],
      source: check.source ?? "data-sanity-checks.json",
      tookMs
    });
  }

  report.findings.sort((a, b) => severityRank(a.severity) - severityRank(b.severity) || b.rowCount - a.rowCount);
  report.status = report.findings.length ? "fail" : (report.parked.length ? "degraded" : "pass");
  report.slackLayerMessage = layerSentence(report.findings, report.parked);
  writeJson(outPath, report);

  // stdout is quoted verbatim into the receipt and can reach Slack, so it stays plain English.
  const line = `data-sanity ${report.status}: ${report.slackLayerMessage} (${report.checksRun} checks, report ${path.relative(repo, outPath)})`;
  if (report.status === "pass") {
    console.log(line);
    return 0;
  }
  console.error(line);
  return 1;
}

function sqlLimit(sql) {
  return Number(String(sql).match(/\blimit\s+(\d+)\b/i)?.[1] ?? Number.MAX_SAFE_INTEGER);
}

function severityRank(severity) {
  return { high: 0, medium: 1, low: 2 }[String(severity)] ?? 1;
}

function psqlRows(databaseUrl, sql, { columns = false } = {}) {
  const psqlBin = process.env.GOATOS_PSQL_BIN || (existsSync("/usr/bin/psql") ? "/usr/bin/psql" : "psql");
  const psqlArgs = [databaseUrl, "-v", "ON_ERROR_STOP=1", "-X", "-F", "\t", "-A"];
  if (!columns) psqlArgs.push("-t");
  const child = spawnSync(psqlBin, psqlArgs, {
    cwd: repo,
    encoding: "utf8",
    input: `${sql}\n`,
    timeout: 120000,
    // Session level, not transaction level. `set local` dies with a rogue COMMIT; this does not.
    env: { ...process.env, PGOPTIONS: `${process.env.PGOPTIONS ?? ""} -c default_transaction_read_only=on`.trim() }
  });
  if (child.status !== 0) {
    return { error: redactText(child.stderr || child.stdout || `psql exited ${child.status}`) };
  }
  const noise = new Set(["SET", "BEGIN", "COMMIT", "ROLLBACK"]);
  const lines = String(child.stdout ?? "")
    .split("\n")
    .filter((line) => line !== "")
    .filter((line) => !noise.has(line.trim()))
    .filter((line) => !/^\(\d+ rows?\)$/.test(line.trim()));
  if (!columns) return { rows: lines.map((line) => line.split("\t")) };
  const header = lines.shift();
  return { columns: header ? header.split("\t") : [], rows: lines.map((line) => line.split("\t")) };
}

function writeJson(file, value) {
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (containsUnredactedSecret(text)) throw new Error("refusing to write a data sanity report that appears to contain a secret");
  writeFileSync(file, text);
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--out") parsed.out = raw[++i];
    else if (arg === "--only") parsed.only = raw[++i];
    else fail(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  const catalogue = loadCatalogue();
  if (!Array.isArray(catalogue.checks) || catalogue.checks.length < 20) {
    throw new Error("self-test: the data sanity catalogue must cover the whole roadmap list");
  }
  const names = new Set();
  for (const check of catalogue.checks) {
    if (names.has(check.name)) throw new Error(`self-test: duplicate check name ${check.name}`);
    names.add(check.name);
    for (const field of ["name", "question", "sql", "humanFailure", "countUnit", "severity", "page"]) {
      if (!check[field]) throw new Error(`self-test: check ${check.name} is missing ${field}`);
    }
    if (Number(check.expectRows) !== 0) throw new Error(`self-test: check ${check.name} must expect zero offending rows`);
    if (!check.page?.path?.startsWith("/")) throw new Error(`self-test: check ${check.name} must name the screen it shows up on`);
    assertSelectOnly(check.sql, check.name);
  }
  // Every roadmap check must be present, by intent rather than by wording.
  for (const required of [
    "herd_total_vs_status_breakdown", "park_total_vs_pen_sum", "animal_in_two_pens",
    "non_positive_animal_weight", "implausible_weight_jump", "feed_issued_exceeds_purchased",
    "drive_doses_exceed_pen_animals", "future_dated_animal_record", "death_before_birth",
    "capture_after_submission", "orphan_weighing_record", "orphan_sale_record", "orphan_load_record",
    "weighing_run_stuck_over_2_days", "verification_pending_over_7_days", "sales_sold_count_vs_herd_sold"
  ]) {
    if (!names.has(required)) throw new Error(`self-test: the roadmap check ${required} is missing from the catalogue`);
  }
  // The guard must refuse every write verb, including one hidden after a legal SELECT.
  for (const bad of [
    "insert into goats values (1) limit 1",
    "select 1 limit 1; drop table goats",
    "update goats set sex = 'f' limit 1",
    "delete from goats limit 1",
    "drop table goats limit 1",
    "alter table goats add column x int limit 1",
    "truncate goats limit 1",
    "grant all on goats to public limit 1",
    "create table t as select 1 limit 1",
    "copy goats to stdout limit 1",
    "call do_something() limit 1",
    "do $$ begin end $$ limit 1",
    "select 1 into tmp from goats limit 1",
    "select pg_sleep(30) limit 1",
    "select * from goats for update limit 1",
    "\\copy goats to 'x' limit 1",
    "select 1"
  ]) {
    let refused = false;
    try {
      assertSelectOnly(bad, "guard");
    } catch {
      refused = true;
    }
    if (!refused) throw new Error(`self-test: SELECT-only guard accepted ${JSON.stringify(bad)}`);
  }
  assertSelectOnly("select 1 as ok limit 1", "guard");
  assertSelectOnly("with t as (select 1 as n limit 1) select n from t limit 1", "guard");
  // A value that merely looks like a write verb must not trip the guard.
  assertSelectOnly("select status from goats where status = 'do not delete' limit 1", "guard");

  // The cap is structural: it must hold for a check whose own LIMIT only caps a CTE, and for a
  // check with no LIMIT at all.
  const cteBypass = "with c as (select 1 limit 1) select g.goat_id from goats g, c";
  if (!cappedSql(cteBypass, 500).startsWith("select * from (")) throw new Error("self-test: the runner must cap structurally");
  if (!/\n\) _capped limit 500$/.test(cappedSql(cteBypass, 500))) throw new Error("self-test: the runner's cap must be the outermost limit, on its own line");
  if (!/limit\s+7$/.test(cappedSql("select 1", 7))) throw new Error("self-test: the runner must cap SQL that has no limit of its own");
  // The wrapper must survive a check that tries to close it early or comment it out.
  for (const hostile of [
    "select goat_id from goats) _capped limit 999999 --",
    "select goat_id from goats) _capped limit 999999 /*",
    "select goat_id from goats where (1=1 limit 5",
    "select goat_id from goats limit 5 -- trailing",
    "select goat_id from goats limit 5 /* trailing */"
  ]) {
    let refused = false;
    try {
      assertSelectOnly(hostile, "wrapper");
    } catch {
      refused = true;
    }
    if (!refused) throw new Error(`self-test: the guard accepted a check that can escape the cap: ${JSON.stringify(hostile)}`);
  }
  const wrapped = readOnlySql("select 1 limit 1");
  for (const fragment of ["begin read only", "default_transaction_read_only = on", "statement_timeout", "rollback"]) {
    if (!wrapped.includes(fragment)) throw new Error(`self-test: read-only wrapper missing ${fragment}`);
  }
  const rows = capAndRedactRows([["a", "postgres://u:p@h/db"], ["b", "x"], ["c", "y"]], ["k", "v"], 2, 80);
  if (rows.length !== 2) throw new Error("self-test: sample rows must be capped");
  if (JSON.stringify(rows).includes(":p@")) throw new Error("self-test: sample rows must be redacted");
  if (!layerSentence([], []).includes("adds up")) throw new Error("self-test: green layer sentence missing");
  if (!layerSentence([{ rowCount: 3 }], []).includes("does not add up")) throw new Error("self-test: failing layer sentence missing");
  if (!layerSentence([{ rowCount: 3 }, { rowCount: 1 }], []).includes("do not add up")) throw new Error("self-test: plural failing layer sentence missing");
  if (!layerSentence([], [{ name: "x" }]).includes("parked")) throw new Error("self-test: parked layer sentence missing");
  console.log("dashboard data sanity: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(2);
}
