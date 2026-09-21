#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const config = JSON.parse(readFileSync(path.join(repo, "tools/dashboard-automation/config.json"), "utf8"));
const args = parseArgs(process.argv.slice(2));

if (args.selfTest) {
  selfTest();
  process.exit(0);
}

const stgUrl = process.env.GOATOS_STG_READONLY_DATABASE_URL;
const ociUrl = process.env.GOATOS_OCI_READONLY_DATABASE_URL;
const psqlBin = process.env.GOATOS_PSQL_BIN || (existsSync("/usr/bin/psql") ? "/usr/bin/psql" : "psql");
if (!stgUrl || !ociUrl) {
  fail("missing read-only parity env: GOATOS_STG_READONLY_DATABASE_URL and GOATOS_OCI_READONLY_DATABASE_URL are required");
}

const outPath = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/business-data-parity.json"));
mkdirSync(path.dirname(outPath), { recursive: true });

const result = {
  generatedAt: new Date().toISOString(),
  status: "pass",
  readOnlyProof: {},
  criticalTables: {},
  bestEffortTables: {},
  sentinelQueries: {},
  blockers: []
};

assertReadOnlyConnection("stg", stgUrl, true);
assertReadOnlyConnection("oci", ociUrl, true);

for (const table of config.businessDataParity.criticalTables) {
  compareCount(table, "criticalTables", true);
}
for (const table of config.businessDataParity.bestEffortTables) {
  compareCount(table, "bestEffortTables", false);
}
runSentinel("cbe_herd_analytics_window", cbeHerdWindowSql());
runZeroRowSentinel("godel_2_timewise_adg", godelTimewiseAdgSql());
runWeighingPenAliasFormBRows();
runSalesSoldWeightCoverage();
for (const reconciliation of config.businessDataParity.fieldReconciliations ?? []) {
  runFieldReconciliation(reconciliation);
}
runCastroReconciliationSummary();

if (result.blockers.length > 0) result.status = "fail";
writeJson(outPath, result);
console.log(`dashboard business data parity ${result.status}; wrote ${path.relative(repo, outPath)}`);
if (result.status !== "pass") process.exit(1);

function compareCount(table, section, critical) {
  const sql = readOnlySql(`select count(*)::text from ${quoteIdent(table)}`);
  const stg = psql(stgUrl, sql);
  const oci = psql(ociUrl, sql);
  const row = { stg: stg.value, oci: oci.value, status: "pass" };
  if (stg.error || oci.error) {
    row.status = critical ? "fail" : "skip";
    row.error = redactText(stg.error ?? oci.error);
  } else if (stg.value !== oci.value) {
    row.status = critical ? "fail" : "warn";
    row.delta = Number(oci.value) - Number(stg.value);
  }
  result[section][table] = row;
  if (row.status === "fail") result.blockers.push({ kind: "table_count_mismatch", table, ...row });
}

function runSentinel(name, sql) {
  const stg = psqlRows(stgUrl, readOnlySql(sql));
  const oci = psqlRows(ociUrl, readOnlySql(sql));
  const row = { stg: stg.rows, oci: oci.rows, status: "pass" };
  if (stg.error || oci.error) {
    row.status = "fail";
    row.error = redactText(stg.error ?? oci.error);
  } else if (JSON.stringify(stg.rows) !== JSON.stringify(oci.rows)) {
    row.status = "fail";
  }
  result.sentinelQueries[name] = row;
  if (row.status === "fail") result.blockers.push({ kind: "sentinel_mismatch", name });
}

function runZeroRowSentinel(name, sql) {
  const stg = psqlRows(stgUrl, readOnlySql(sql));
  const oci = psqlRows(ociUrl, readOnlySql(sql));
  const row = { stg: zeroRowObject(stg.rows?.[0]), oci: zeroRowObject(oci.rows?.[0]), status: "pass" };
  if (stg.error || oci.error) {
    row.status = "fail";
    row.error = redactText(stg.error ?? oci.error);
  } else if (JSON.stringify(row.stg) !== JSON.stringify(row.oci)) {
    row.status = "fail";
    row.reason = "stg_oci_mismatch";
  } else if ((row.stg?.violations ?? 0) > 0) {
    row.status = "fail";
    row.reason = "semantic_violation";
  }
  result.sentinelQueries[name] = row;
  if (row.status === "fail") result.blockers.push({ kind: "sentinel_mismatch", name, reason: row.reason ?? "query_failed" });
}

function runFieldReconciliation(reconciliation) {
  const name = reconciliation.name;
  const sql = fieldReconciliationSql(reconciliation);
  const stg = psqlRows(stgUrl, readOnlySql(sql));
  const oci = psqlRows(ociUrl, readOnlySql(sql));
  const expected = reconciliation.expected;
  const row = {
    source: reconciliation.source,
    expected,
    stg: rowObject(stg.rows?.[0]),
    oci: rowObject(oci.rows?.[0]),
    status: "pass"
  };
  if (stg.error || oci.error) {
    row.status = "fail";
    row.error = redactText(stg.error ?? oci.error);
  } else if (JSON.stringify(row.stg) !== JSON.stringify(row.oci)) {
    row.status = "fail";
    row.reason = "stg_oci_mismatch";
  } else if (!matchesExpected(row.stg, expected)) {
    row.status = "fail";
    row.reason = "field_reconciliation_mismatch";
  }
  result.sentinelQueries[name] = row;
  if (row.status === "fail") result.blockers.push({ kind: "field_reconciliation_mismatch", name, reason: row.reason ?? "query_failed" });
}

function runCastroReconciliationSummary() {
  const name = "castro_reconciliation";
  const castroChecks = (config.businessDataParity.fieldReconciliations ?? [])
    .filter((item) => /^castro_/i.test(item.name) || /Castro/i.test(item.source_shed_name ?? ""));
  const implemented = config.businessDataParity.sentinelQueries
    .some((item) => item.name === name && item.implementationStatus === "implemented");
  const statuses = castroChecks.map((item) => result.sentinelQueries[item.name]?.status ?? "missing");
  const row = {
    implemented,
    coveredChecks: castroChecks.map((item) => item.name),
    status: implemented && castroChecks.length > 0 && statuses.every((status) => status === "pass") ? "pass" : "fail"
  };
  if (!implemented) row.reason = "sentinel_not_marked_implemented";
  else if (castroChecks.length === 0) row.reason = "no_castro_field_reconciliations";
  else if (!statuses.every((status) => status === "pass")) row.reason = "castro_field_reconciliation_failed";
  result.sentinelQueries[name] = row;
  if (row.status === "fail") result.blockers.push({ kind: "sentinel_mismatch", name, reason: row.reason });
}

function runSalesSoldWeightCoverage() {
  const name = "sales_sold_weight_coverage";
  const stg = psqlRows(stgUrl, readOnlySql(salesSoldWeightCoverageSql()));
  const oci = psqlRows(ociUrl, readOnlySql(salesSoldWeightCoverageSql()));
  const row = { stg: salesWeightObject(stg.rows?.[0]), oci: salesWeightObject(oci.rows?.[0]), status: "pass" };
  if (stg.error || oci.error) {
    row.status = "fail";
    row.error = redactText(stg.error ?? oci.error);
  } else if (JSON.stringify(row.stg) !== JSON.stringify(row.oci)) {
    row.status = "fail";
    row.reason = "stg_oci_mismatch";
  } else if (row.stg?.status !== "ok") {
    row.status = "fail";
    row.reason = row.stg?.status ?? "unknown_sales_weight_gap";
  }
  result.sentinelQueries[name] = row;
  if (row.status === "fail") result.blockers.push({ kind: "sentinel_mismatch", name, reason: row.reason ?? "query_failed" });
}

function runWeighingPenAliasFormBRows() {
  const name = "weighing_pen_alias_form_b_rows";
  const stg = psqlRows(stgUrl, readOnlySql(weighingPenAliasFormBRowsSql()));
  const oci = psqlRows(ociUrl, readOnlySql(weighingPenAliasFormBRowsSql()));
  const row = {
    stg: weighingAliasObject(stg.rows?.[0]),
    oci: weighingAliasObject(oci.rows?.[0]),
    status: "pass"
  };
  if (stg.error || oci.error) {
    row.status = "fail";
    row.error = redactText(stg.error ?? oci.error);
  } else if (JSON.stringify(row.stg) !== JSON.stringify(row.oci)) {
    row.status = "fail";
    row.reason = "stg_oci_mismatch";
  } else if ((row.stg?.formBRows ?? 0) > 0) {
    row.status = "fail";
    row.reason = "planner_alias_form_b_rows";
  }
  result.sentinelQueries[name] = row;
  if (row.status === "fail") result.blockers.push({ kind: "sentinel_mismatch", name, reason: row.reason ?? "query_failed" });
}

function weighingAliasObject(row) {
  if (!row) return null;
  return {
    formBRows: Number(row[0]),
    affectedPens: Number(row[1]),
    firstDate: row[2] || null,
    lastDate: row[3] || null,
    examples: row[4] || ""
  };
}

function zeroRowObject(row) {
  if (!row) return null;
  return {
    violations: Number(row[0]),
    affectedPens: Number(row[1]),
    firstDate: row[2] || null,
    lastDate: row[3] || null,
    examples: row[4] || ""
  };
}

function salesWeightObject(row) {
  if (!row) return null;
  return {
    soldAnimals: Number(row[0]),
    animalsWithDealAvgWeight: Number(row[1]),
    taggedAllocations: Number(row[2]),
    taggedAllocationsWithWeight: Number(row[3]),
    status: row[4]
  };
}


function fieldReconciliationSql(reconciliation) {
  const sourceShedName = sqlLiteral(reconciliation.source_shed_name);
  const partitionLabel = sqlLiteral(reconciliation.partition_label);
  return `
select
  count(*) filter (where g.lifecycle_status = 'alive' and g.merged_into_goat_id is null)::int as now,
  count(*) filter (where (g.lifecycle_status = 'dead' or g.exit_reason in ('death', 'died')) and g.merged_into_goat_id is null)::int as deaths,
  count(*) filter (where g.health_status ilike '%icu%' and g.merged_into_goat_id is null)::int as icu,
  count(*) filter (where (g.lifecycle_status = 'sold' or g.exit_reason = 'sale') and g.merged_into_goat_id is null)::int as sold,
  count(*) filter (where g.management_stage ilike '%y1%' and g.merged_into_goat_id is null)::int as y1,
  count(*) filter (where g.merged_into_goat_id is null)::int as total
from goat_shed_partitions gsp
join goats g on g.goat_id = gsp.goat_id
where gsp.source_shed_name = ${sourceShedName}
  and gsp.partition_label = ${partitionLabel}`;
}

function rowObject(row) {
  if (!row) return null;
  return {
    now: Number(row[0]),
    deaths: Number(row[1]),
    icu: Number(row[2]),
    sold: Number(row[3]),
    y1: Number(row[4]),
    total: Number(row[5])
  };
}

function matchesExpected(actual, expected) {
  if (!actual) return false;
  return ["now", "deaths", "icu", "sold", "y1", "total"].every((key) => Number(actual[key]) === Number(expected[key]));
}

function cbeHerdWindowSql() {
  return `
with cbe as (
  select location_id from locations where location_code = 'CBE' and location_type = 'park' limit 1
), live as (
  select
    count(*)::text total,
    count(*) filter (where coalesce(herd_register_is_kid(age_band, management_stage), false))::text kids,
    count(*) filter (where not coalesce(herd_register_is_kid(age_band, management_stage), false))::text adults
  from goats g, cbe
  where g.tenant_id = '00000000-0000-4000-8000-000000000001'
    and g.park_id = cbe.location_id
    and g.merged_into_goat_id is null
    and g.lifecycle_status = 'alive'
), flow as (
  select
    count(*) filter (where lifecycle_status = 'dead' or exit_reason = 'death')::text deaths,
    count(*) filter (where lifecycle_status = 'sold' or exit_reason = 'sale')::text sold
  from goats g, cbe
  where g.tenant_id = '00000000-0000-4000-8000-000000000001'
    and g.park_id = cbe.location_id
    and g.merged_into_goat_id is null
    and coalesce((g.exited_at at time zone 'Asia/Kolkata')::date, (g.updated_at at time zone 'Asia/Kolkata')::date)
      between date '2026-09-14' and date '2026-09-21'
)
select total, kids, adults, deaths, sold from live, flow`;
}

function godelTimewiseAdgSql() {
  return `
with alias_shape_split as (
  select
    wcs.campaign_shed_id,
    wcs.start_business_date,
    parent.name as shed_name,
    wcs.partition_label,
    alias.name as alias_name
  from weighing_campaign_sheds wcs
  join locations parent
    on parent.tenant_id = wcs.tenant_id
   and parent.location_id = wcs.location_id
   and parent.location_type = 'shed'
  join shed_partitions sp
    on sp.tenant_id = wcs.tenant_id
   and sp.shed_id = parent.location_id
   and sp.normalized_label = regexp_replace(lower(btrim(wcs.partition_label)), '^(part|p)[[:space:]]+', '')
   and sp.alias_location_id is not null
  join locations alias
    on alias.tenant_id = sp.tenant_id
   and alias.location_id = sp.alias_location_id
  where nullif(btrim(wcs.partition_label), '') is not null
    and wcs.weighing_category = 'per_shed_partition'
    and wcs.status <> 'canceled'
    and lower(parent.name) ~ '(godel|mandela|castro)'
)
select
  count(*)::text as violations,
  count(distinct shed_name || '|' || partition_label)::text as affected_pens, -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=internal-distinct-key-not-display-label expiry=2027-09-21
  coalesce(min(start_business_date)::text, '') as first_date,
  coalesce(max(start_business_date)::text, '') as last_date,
  coalesce(string_agg(distinct shed_name || ' / ' || partition_label || ' should be ' || alias_name, ', ' order by shed_name || ' / ' || partition_label || ' should be ' || alias_name), '') as examples -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=diagnostic-legacy-alias-example-not-product-display expiry=2027-09-21
from alias_shape_split`;
}

function salesSoldWeightCoverageSql() {
  return `
with deals as (
  select
    coalesce(sum(animal_count), 0)::int as sold_animals,
    coalesce(sum(animal_count) filter (where animal_count > 0 and total_weight_kg > 0), 0)::int as animals_with_deal_avg_weight
  from sales_deals
), allocations as (
  select
    count(*) filter (where status = 'tagged')::int as tagged_allocations,
    count(*) filter (where status = 'tagged' and weight_kg is not null)::int as tagged_allocations_with_weight
  from goat_sale_allocations
)
select sold_animals::text, animals_with_deal_avg_weight::text, tagged_allocations::text, tagged_allocations_with_weight::text,
  case when animals_with_deal_avg_weight > tagged_allocations_with_weight then 'deal_average_weight_gap' else 'ok' end
from deals, allocations`;
}

function weighingPenAliasFormBRowsSql() {
  return `
with form_b as (
  select
    wcs.campaign_shed_id,
    wcs.start_business_date,
    parent.name as shed_name,
    wcs.partition_label,
    alias.location_id as canonical_alias_location_id
  from weighing_campaign_sheds wcs
  cross join lateral (
    select lower(regexp_replace(btrim(wcs.partition_label), '^(part|p)\\s*', '', 'i')) as partition_number
  ) normalized
  join locations parent
    on parent.tenant_id = wcs.tenant_id
   and parent.location_id = wcs.location_id
   and parent.location_type = 'shed'
   and parent.status = 'active'
   and parent.retired_at is null
  join locations alias
    on alias.tenant_id = parent.tenant_id
   and alias.parent_location_id = parent.parent_location_id
   and alias.location_type = 'shed'
   and alias.status = 'inactive'
   and lower(regexp_replace(btrim(alias.name), '\\s+', ' ', 'g')) in (
      lower(regexp_replace(btrim(parent.name || ' - ' || wcs.partition_label), '\\s+', ' ', 'g')), -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=legacy-alias-matching-not-display-label expiry=2027-09-21
      lower(regexp_replace(btrim(parent.name || ' ' || wcs.partition_label), '\\s+', ' ', 'g')), -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=legacy-alias-matching-not-display-label expiry=2027-09-21
      lower(regexp_replace(btrim(parent.name || ' - Part ' || normalized.partition_number), '\\s+', ' ', 'g')), -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=legacy-alias-matching-not-display-label expiry=2027-09-21
      lower(regexp_replace(btrim(parent.name || ' Part ' || normalized.partition_number), '\\s+', ' ', 'g')), -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=legacy-alias-matching-not-display-label expiry=2027-09-21
      lower(regexp_replace(btrim(parent.name || ' - P' || normalized.partition_number), '\\s+', ' ', 'g')), -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=legacy-alias-matching-not-display-label expiry=2027-09-21
      lower(regexp_replace(btrim(parent.name || ' P' || normalized.partition_number), '\\s+', ' ', 'g')) -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=legacy-alias-matching-not-display-label expiry=2027-09-21
   )
  where nullif(btrim(wcs.partition_label), '') is not null
    and wcs.weighing_category = 'per_shed_partition'
    and wcs.status <> 'canceled'
)
select
  count(*)::text as form_b_rows,
  count(distinct shed_name || '|' || partition_label)::text as affected_pens, -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=internal-distinct-key-not-display-label expiry=2027-09-21
  coalesce(min(start_business_date)::text, '') as first_date,
  coalesce(max(start_business_date)::text, '') as last_date,
  coalesce(string_agg(distinct shed_name || ' / ' || partition_label, ', ' order by shed_name || ' / ' || partition_label), '') as examples -- operational-location:ignore: owner=ravi issue=dashboard-parity-form-b-alias scope=diagnostic-legacy-alias-example-not-product-display expiry=2027-09-21
from form_b`;
}

function psql(databaseUrl, sql) {
  const rows = psqlRows(databaseUrl, sql);
  if (rows.error) return { error: rows.error };
  return { value: rows.rows[0]?.[0] ?? null };
}

function psqlRows(databaseUrl, sql) {
  const child = spawnSync(psqlBin, [databaseUrl, "-v", "ON_ERROR_STOP=1", "-X", "-At", "-F", "\t", "-c", sql], {
    cwd: repo,
    encoding: "utf8"
  });
  if (child.status !== 0) return { error: redactText(child.stderr || child.stdout || `psql exited ${child.status}`) };
  return {
    rows: child.stdout
      .split("\n")
      .filter(Boolean)
      .filter((line) => !["SET", "BEGIN", "COMMIT", "ROLLBACK"].includes(line.trim()))
      .map((line) => line.split("\t"))
  };
}

function readOnlySql(sql) {
  return `begin read only; set local default_transaction_read_only = on; ${sql}; rollback`;
}

function assertReadOnlyConnection(name, databaseUrl, required) {
  const proof = psqlRows(databaseUrl, readOnlySql(`
select
  current_user,
  current_setting('transaction_read_only'),
  current_setting('default_transaction_read_only')`));
  const row = proof.rows?.[0];
  result.readOnlyProof[name] = {
    user: row?.[0] ?? null,
    transactionReadOnly: row?.[1] ?? null,
    defaultTransactionReadOnly: row?.[2] ?? null,
    status: "pass"
  };
  if (proof.error || !row || row[1] !== "on") {
    result.readOnlyProof[name].status = required ? "fail" : "skip";
    result.readOnlyProof[name].error = proof.error;
    if (required) {
      result.blockers.push({
        kind: "read_only_proof_failed",
        database: name,
        user: result.readOnlyProof[name].user,
        transactionReadOnly: result.readOnlyProof[name].transactionReadOnly,
        defaultTransactionReadOnly: result.readOnlyProof[name].defaultTransactionReadOnly
      });
    }
  }
}

function quoteIdent(value) {
  if (!/^[a-z_][a-z0-9_]*$/i.test(value)) throw new Error(`unsafe SQL identifier: ${value}`);
  return `"${value.replaceAll('"', '""')}"`;
}

function sqlLiteral(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function writeJson(file, value) {
  const text = `${JSON.stringify(value, null, 2)}\n`;
  if (containsUnredactedSecret(text)) throw new Error("refusing to write parity receipt that appears to contain a secret");
  writeFileSync(file, text);
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--out") parsed.out = raw[++i];
    else fail(`unknown argument: ${arg}`);
  }
  return parsed;
}

function selfTest() {
  if (!config.businessDataParity.criticalTables.includes("goats")) {
    throw new Error("self-test: goats must be a critical business parity table");
  }
  if (!config.businessDataParity.criticalTables.includes("goat_sale_allocations")) {
    throw new Error("self-test: goat_sale_allocations must be a critical sales parity table");
  }
  if (!config.businessDataParity.bestEffortTables.some((table) => table.includes("herd_signal"))) {
    throw new Error("self-test: herd signal telemetry must be best-effort");
  }
  if (!config.businessDataParity.fieldReconciliations.some((item) => item.name === "castro_3_partition_3_2026_09_21")) {
    throw new Error("self-test: Castro 3 field reconciliation must be configured");
  }
  if (!config.businessDataParity.sentinelQueries.some((item) => item.name === "weighing_pen_alias_form_b_rows" && item.implementationStatus === "implemented")) {
    throw new Error("self-test: weighing pen alias Form B sentinel must be implemented");
  }
  if (!config.businessDataParity.sentinelQueries.some((item) => item.name === "godel_2_timewise_adg" && item.implementationStatus === "implemented")) {
    throw new Error("self-test: Godel ADG sentinel must be implemented");
  }
  if (!config.businessDataParity.sentinelQueries.some((item) => item.name === "castro_reconciliation" && item.implementationStatus === "implemented")) {
    throw new Error("self-test: Castro reconciliation sentinel must be implemented");
  }
  if (!readOnlySql("select 1").includes("begin read only") || !readOnlySql("select 1").includes("rollback")) {
    throw new Error("self-test: parity SQL must run inside an explicit read-only transaction");
  }
  const aliasSql = weighingPenAliasFormBRowsSql();
  for (const fragment of ["weighing_campaign_sheds", "partition_label", "locations alias", "form_b_rows"]) {
    if (!aliasSql.includes(fragment)) throw new Error(`self-test: alias sentinel SQL missing ${fragment}`);
  }
  const godelSql = godelTimewiseAdgSql();
  for (const fragment of ["shed_partitions", "alias_location_id", "godel|mandela|castro", "should be"]) {
    if (!godelSql.includes(fragment)) throw new Error(`self-test: Godel sentinel SQL missing ${fragment}`);
  }
  if (config.selfHealing.mode !== "pull_request_only") {
    throw new Error("self-test: self-healing must be PR-only");
  }
  if (!config.selfHealing.forbiddenActions.includes("writeOciData")) {
    throw new Error("self-test: OCI writes must remain forbidden");
  }
  console.log("dashboard business data parity: self-test passed");
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
