#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const tenantId = readArg("tenant-id", process.env.GOATOS_TENANT_ID || "00000000-0000-4000-8000-000000000001");
if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(tenantId)) {
  fail(`invalid --tenant-id=${tenantId}; expected UUID`);
}
const apply = hasFlag("apply");
const allowOciOnly = hasFlag("allow-oci-only");
const scope = readArg("scope", "all");
const outDir = resolve(readArg("out", `.codex-goatos-render/parity/${new Date().toISOString().replace(/[:.]/g, "-")}`));
const ociEnv = readArg("oci-env", `${process.env.HOME}/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`);
const stgEnv = readArg("stg-env", `${process.env.HOME}/mesha/local-data/goatos-stg-to-oci/stg-sync-sa.env`);

const commonTables = [
  "locations",
  "workforce_members",
];

const feedTables = [
  ...commonTables,
  "feed_purchases",
  "feed_external_consumption",
  "milk_preparation_completions",
  "milk_preparation_proof_attempts",
  "feed_direction_issues",
  "feed_direction_issue_rows",
  "feed_packing_completions",
  "feed_packing_verified_quantities",
  "feed_distribution_completions",
  "feed_wastage_completions",
  "feed_transport_tasks",
  "feed_transport_attempts",
];

const weighingTables = [
  ...commonTables,
  "goats",
  "goat_identifiers",
  "goat_shed_partitions",
  "procurement_loads",
  "procurement_load_goats",
  "weighing_campaigns",
  "weighing_campaign_sheds",
  "weighing_work_items",
  "weighing_observations",
  "weighing_shed_observations",
  "weighing_shed_observation_proofs",
  "weighing_shed_load_tags",
  "verification_items",
];

const tables = scope === "feed" ? feedTables
  : scope === "weighing" ? weighingTables
  : scope === "all" ? [...feedTables, ...weighingTables.filter((table) => !feedTables.includes(table))]
  : fail(`unknown --scope=${scope}; expected all, feed, or weighing`);

const verificationFilter = `(tenant_id = '${tenantId}' AND ((module = 'weighing' AND category = 'weighing_proof') OR (module = 'feed' AND category IN ('feed_distribution', 'feed_transport', 'feed_wastage', 'feed_packing'))))`;

mkdirSync(outDir, { recursive: true });
const ociDsn = ociDatabaseUrl();
assertOciTarget(ociDsn);
const stgDsn = stgDatabaseUrl();
const schema = loadSchema(stgDsn, ociDsn);
const plan = tables.map((table) => tablePlan(table, schema));
writeFileSync(join(outDir, "plan.json"), JSON.stringify({ tenant_id: tenantId, apply, allow_oci_only: allowOciOnly, tables: plan }, null, 2));

for (const table of plan) {
  exportCsv(stgDsn, table);
}

const preflight = plan.map((table) => preflightTable(ociDsn, table));
writeFileSync(join(outDir, "preflight.json"), JSON.stringify(preflight, null, 2));
const blockers = preflight.filter((row) => row.oci_only_count > 0);
if (blockers.length && !allowOciOnly) {
  console.error(`Refusing sync: OCI has rows not present in STG for ${blockers.map((row) => `${row.table}=${row.oci_only_count}`).join(", ")}`);
  console.error(`Review ${join(outDir, "preflight.json")} and rerun with --allow-oci-only only if those extra rows are expected.`);
  process.exit(3);
}

if (!apply) {
  console.log(`dry_run=true`);
  console.log(`out=${outDir}`);
  console.table(preflight.map(({ table, stg_count, oci_count, oci_only_count }) => ({ table, stg_count, oci_count, oci_only_count })));
  process.exit(0);
}

const before = snapshot(ociDsn, plan);
writeFileSync(join(outDir, "oci-before.json"), JSON.stringify(before, null, 2));
const sqlPath = join(outDir, "import.sql");
writeFileSync(sqlPath, importSql(plan));
psql(ociDsn, ["-X", "-v", "ON_ERROR_STOP=1", "-f", sqlPath], { PGOPTIONS: "-c statement_timeout=900000 -c lock_timeout=5000" });
const after = snapshot(ociDsn, plan);
writeFileSync(join(outDir, "oci-after.json"), JSON.stringify(after, null, 2));
console.log(`dry_run=false`);
console.log(`out=${outDir}`);
console.table(after.map(({ table, rows, hash }) => ({ table, rows, hash })));

function tablePlan(table, schema) {
  const stgCols = schema.stg[table] || [];
  const ociCols = schema.oci[table] || [];
  const cols = stgCols
    .filter((col) => !col.is_generated)
    .filter((col) => ociCols.some((other) => other.name === col.name && !other.is_generated))
    .map((col) => col.name);
  const pk = schema.pk[table] || [];
  if (!cols.length || !pk.length) throw new Error(`missing schema metadata for ${table}`);
  return { table, cols, pk, filter: table === "verification_items" ? verificationFilter : `tenant_id = '${tenantId}'` };
}

function loadSchema(stg, oci) {
  const query = `
WITH cols AS (
  SELECT table_name, column_name, is_generated <> 'NEVER' AS is_generated, ordinal_position
  FROM information_schema.columns
  WHERE table_schema = 'public' AND table_name = ANY('{${tables.join(",")}}'::text[])
)
SELECT json_object_agg(table_name, columns ORDER BY table_name)
FROM (
  SELECT table_name, json_agg(json_build_object('name', column_name, 'is_generated', is_generated) ORDER BY ordinal_position) AS columns
  FROM cols GROUP BY table_name
) s`;
  const pkQuery = `
SELECT json_object_agg(table_name, columns ORDER BY table_name)
FROM (
  SELECT tc.table_name, json_agg(kcu.column_name ORDER BY kcu.ordinal_position) AS columns
  FROM information_schema.table_constraints tc
  JOIN information_schema.key_column_usage kcu ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
  WHERE tc.table_schema = 'public' AND tc.constraint_type = 'PRIMARY KEY' AND tc.table_name = ANY('{${tables.join(",")}}'::text[])
  GROUP BY tc.table_name
) s`;
  return {
    stg: JSON.parse(psqlOut(stg, query)),
    oci: JSON.parse(psqlOut(oci, query)),
    pk: JSON.parse(psqlOut(oci, pkQuery)),
  };
}

function exportCsv(dsn, table) {
  const csv = join(outDir, `${table.table}.csv`);
  const query = `SELECT ${table.cols.map(q).join(", ")} FROM public.${q(table.table)} WHERE ${table.filter} ORDER BY ${table.pk.map(q).join(", ")}`;
  psql(dsn, ["-X", "-v", "ON_ERROR_STOP=1", "-c", `\\copy (${query}) TO '${csv.replaceAll("'", "''")}' WITH CSV HEADER`], {
    PGOPTIONS: "-c default_transaction_read_only=on -c statement_timeout=600000",
  });
}

function preflightTable(dsn, table) {
  const tempName = `stg_${table.table}`;
  const csv = join(outDir, `${table.table}.csv`);
  const sql = `
CREATE TEMP TABLE ${q(tempName)} AS SELECT ${table.cols.map(q).join(", ")} FROM public.${q(table.table)} WHERE false;
\\copy ${q(tempName)} (${table.cols.map(q).join(", ")}) FROM '${csv.replaceAll("'", "''")}' WITH CSV HEADER
SELECT json_build_object(
  'table', '${table.table}',
  'stg_count', (SELECT count(*) FROM ${q(tempName)}),
  'oci_count', (SELECT count(*) FROM public.${q(table.table)} WHERE ${table.filter}),
  'oci_only_count', (
    SELECT count(*) FROM public.${q(table.table)} o
    WHERE ${table.filter.replaceAll("tenant_id", "o.tenant_id")}
      AND NOT EXISTS (${matchSubquery(table, "s", "o", tempName)})
  )
)::text;`;
  return JSON.parse(psqlScriptOut(dsn, `${table.table}-preflight.sql`, sql));
}

function snapshot(dsn, plan) {
  return plan.map((table) => JSON.parse(psqlOut(dsn, `
SELECT json_build_object(
  'table', '${table.table}',
  'rows', count(*),
  'hash', md5(coalesce(string_agg(md5(row_to_json(t)::text), '' ORDER BY ${table.pk.map(q).join(", ")}), ''))
)::text
FROM (SELECT ${table.cols.map(q).join(", ")} FROM public.${q(table.table)} WHERE ${table.filter}) t`)));
}

function importSql(plan) {
  const statements = ["BEGIN;", "SET LOCAL statement_timeout = '15min';", "SET LOCAL lock_timeout = '5s';"];
  for (const table of plan) {
    const tempName = `stg_${table.table}`;
    const csv = join(outDir, `${table.table}.csv`).replaceAll("'", "''");
    statements.push(`CREATE TEMP TABLE ${q(tempName)} AS SELECT ${table.cols.map(q).join(", ")} FROM public.${q(table.table)} WHERE false;`);
    statements.push(`\\copy ${q(tempName)} (${table.cols.map(q).join(", ")}) FROM '${csv}' WITH CSV HEADER`);
    const insertCols = [...table.cols];
    const selectCols = table.cols.map((col) => q(col));
    const mutableCols = table.cols.filter((col) => !table.pk.includes(col));
    const updates = mutableCols.map((col) => `${q(col)} = EXCLUDED.${q(col)}`);
    if (table.table === "feed_transport_tasks") {
      const currentAttemptIndex = insertCols.indexOf("current_attempt_id");
      if (currentAttemptIndex >= 0) {
        selectCols[currentAttemptIndex] = "NULL";
      }
    }
    if (table.table === "feed_purchases") {
      for (const [col, expr] of [["delivery_status", "'reached'"], ["reached_on", "purchase_date"], ["reached_weight_kg", "NULL"], ["reached_by", "NULL"]]) {
        if (!insertCols.includes(col)) {
          insertCols.push(col);
          selectCols.push(expr);
          mutableCols.push(col);
          updates.push(`${q(col)} = EXCLUDED.${q(col)}`);
        }
      }
    }
    statements.push(`
INSERT INTO public.${q(table.table)} (${insertCols.map(q).join(", ")})
SELECT ${selectCols.join(", ")} FROM ${q(tempName)}
ON CONFLICT (${table.pk.map(q).join(", ")}) DO UPDATE SET
  ${updates.join(",\n  ")}
WHERE ${mutableCols.map((col) => `${q(table.table)}.${q(col)} IS DISTINCT FROM EXCLUDED.${q(col)}`).join("\n   OR ")};`);
    if (table.table === "feed_transport_attempts") {
      statements.push(`
UPDATE public.feed_transport_tasks t
SET current_attempt_id = s.current_attempt_id
FROM stg_feed_transport_tasks s
WHERE t.tenant_id = '${tenantId}'
  AND t.task_id = s.task_id;`);
    }
  }
  statements.push("COMMIT;");
  return `${statements.join("\n")}\n`;
}

function matchSubquery(table, s, o, tempName) {
  return `SELECT 1 FROM ${q(tempName)} ${s} WHERE ${table.pk.map((col) => `${s}.${q(col)} IS NOT DISTINCT FROM ${o}.${q(col)}`).join(" AND ")}`;
}

function stgDatabaseUrl() {
  if (process.env.STG_DATABASE_URL) return process.env.STG_DATABASE_URL;
  if (!existsSync(stgEnv)) throw new Error(`missing STG env: ${stgEnv}`);
  const env = shellEnv(`source ${shellQuote(stgEnv)} >/dev/null 2>&1 && env`);
  const url = execFileSync("gcloud", ["secrets", "versions", "access", "latest", "--secret=goatos-stg-database-url", `--project=${env.GOATOS_STG_PROJECT || "goatos-stg"}`], { encoding: "utf8", env: { ...process.env, ...env } }).trim();
  return url.replace(/@\/goatos\?.*$/, "@127.0.0.1:15433/goatos?sslmode=disable");
}

function ociDatabaseUrl() {
  if (process.env.OCI_DATABASE_URL) return process.env.OCI_DATABASE_URL;
  if (!existsSync(ociEnv)) throw new Error(`missing OCI env: ${ociEnv}`);
  const env = shellEnv(`source ${shellQuote(ociEnv)} >/dev/null 2>&1 && env`);
  return env.DATABASE_URL;
}

function assertOciTarget(dsn) {
  const url = new URL(dsn);
  if (url.hostname !== "127.0.0.1" || url.port !== "15432" || url.pathname !== "/goatos") {
    fail(`refusing OCI sync target ${redactDsn(dsn)}; expected local OCI tunnel 127.0.0.1:15432/goatos`);
  }
}

function psqlOut(dsn, sql) {
  return psql(dsn, ["-X", "-tA", "-v", "ON_ERROR_STOP=1", "-c", sql]).trim();
}

function psqlScriptOut(dsn, filename, sql) {
  const file = join(outDir, filename);
  writeFileSync(file, sql);
  return psql(dsn, ["-X", "-q", "-tA", "-v", "ON_ERROR_STOP=1", "-f", file]).trim();
}

function psql(dsn, args, extraEnv = {}) {
  try {
    return execFileSync("psql", [dsn, ...args], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], env: { ...process.env, ...extraEnv } });
  } catch (error) {
    redactExecError(error, dsn);
    throw error;
  }
}

function redactExecError(error, dsn) {
  const redacted = redactDsn(dsn);
  if (typeof error.message === "string") error.message = error.message.replaceAll(dsn, redacted);
  for (const key of ["stdout", "stderr"]) {
    if (typeof error[key] === "string") error[key] = error[key].replaceAll(dsn, redacted);
  }
  if (Array.isArray(error.output)) {
    error.output = error.output.map((item) => typeof item === "string" ? item.replaceAll(dsn, redacted) : item);
  }
}

function redactDsn(dsn) {
  try {
    const url = new URL(dsn);
    if (url.password) url.password = "REDACTED";
    return url.toString();
  } catch {
    return String(dsn).replace(/:\/\/([^:@/]+):([^@/]+)@/, "://$1:REDACTED@");
  }
}

function shellEnv(command) {
  return Object.fromEntries(execFileSync("bash", ["-lc", command], { encoding: "utf8" }).split("\n").filter(Boolean).map((line) => {
    const index = line.indexOf("=");
    return [line.slice(0, index), line.slice(index + 1)];
  }));
}

function q(identifier) {
  return `"${String(identifier).replaceAll('"', '""')}"`;
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", "'\\''")}'`;
}

function readArg(name, fallback = "") {
  const flag = `--${name}`;
  const inline = process.argv.find((arg) => arg.startsWith(`${flag}=`));
  if (inline) return inline.slice(flag.length + 1);
  const index = process.argv.indexOf(flag);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

function hasFlag(name) {
  return process.argv.includes(`--${name}`);
}

function fail(message) {
  console.error(message);
  process.exit(2);
}
