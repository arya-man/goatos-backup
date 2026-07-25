#!/usr/bin/env node
import { execFileSync } from "node:child_process";

const databaseUrl = process.env.DATABASE_URL;
const tenantId = process.env.GOATOS_TENANT_ID || "00000000-0000-4000-8000-000000000001";
const userId = process.env.GOATOS_LOCAL_USER_ID || "90000000-0000-4000-8000-000000000101";
const apiBaseUrl = (process.env.GOATOS_API_BASE_URL || "").replace(/\/+$/, "");

if (!databaseUrl) {
  console.error("DATABASE_URL is required");
  process.exit(2);
}
if (!apiBaseUrl) {
  console.error("GOATOS_API_BASE_URL is required so this checks the real passport API JSON");
  process.exit(2);
}

const cases = [
  {
    rfid: "901007000504553",
    historyDates: ["2026-06-30", "2026-07-24"],
    openDates: [],
  },
  {
    rfid: "901007000503935",
    historyDates: ["2026-07-01"],
    openDates: ["2026-07-25"],
  },
  {
    rfid: "901007000504370",
    historyDates: ["2026-07-01"],
    openDates: ["2026-07-25"],
  },
];

function sqlLiteral(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function runSql(sql) {
  const output = execFileSync("psql", [
    databaseUrl,
    "-v",
    "ON_ERROR_STOP=1",
    "-P",
    "pager=off",
    "-A",
    "-F",
    "\t",
    "-c",
    sql,
  ], { encoding: "utf8" });
  const lines = output.trim().split("\n").filter(Boolean);
  if (lines.length <= 2) return [];
  return lines.slice(1, -1).map((line) => line.split("\t"));
}

function mintToken() {
  const env = {
    ...process.env,
    GOATOS_AUTH_ISSUER: process.env.GOATOS_AUTH_ISSUER || "goatos-local",
    GOATOS_AUTH_AUDIENCE: process.env.GOATOS_AUTH_AUDIENCE || "goatos-api",
    GOATOS_AUTH_HS256_SECRET: process.env.GOATOS_AUTH_HS256_SECRET || "goatos-local-dev-secret-32-bytes-min",
    GOATOS_AUTH_MAX_TOKEN_TTL: process.env.GOATOS_AUTH_MAX_TOKEN_TTL || "24h",
  };
  return execFileSync("go", [
    "run",
    "./cmd/mint-dev-token",
    "-tenant-id",
    tenantId,
    "-user-id",
    userId,
    "-ttl",
    "1h",
  ], { cwd: new URL("../../backend/", import.meta.url), env, encoding: "utf8" }).trim();
}

function apiDate(value) {
  return String(value || "").slice(0, 10);
}

function acceptedEtTtHistory(row, date) {
  return apiDate(row.administered_at) === date &&
    row.status === "accepted" &&
    String(row.dose_code || "").startsWith("et_tt_adult_w") &&
    String(row.display_label || "").startsWith("ET+TT");
}

function scheduledEtTtW2(row, date) {
  return apiDate(row.scheduled_for || row.due_at) === date &&
    row.status === "scheduled" &&
    row.dose_code === "et_tt_adult_w2" &&
    row.display_label === "ET+TT W2";
}

let failures = 0;
const token = mintToken();

for (const item of cases) {
  const goatRows = runSql(`
    SELECT goat_id::text
    FROM goat_identifiers
    WHERE tenant_id = ${sqlLiteral(tenantId)}
      AND identifier_value = ${sqlLiteral(item.rfid)}
      AND status = 'active'
    ORDER BY is_primary_for_goat DESC, valid_from DESC
    LIMIT 1;
  `);
  const goatId = goatRows[0]?.[0];
  if (!goatId) {
    console.error(`FAIL ${item.rfid}: goat not found`);
    failures += 1;
    continue;
  }

  const response = await fetch(`${apiBaseUrl}/goats/${encodeURIComponent(goatId)}/passport`, {
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${token}`,
      "X-GoatOS-Tenant-ID": tenantId,
    },
  });
  if (!response.ok) {
    console.error(`FAIL ${item.rfid}: passport API HTTP ${response.status}: ${await response.text()}`);
    failures += 1;
    continue;
  }
  const passport = await response.json();
  const history = passport.vaccination_history || [];
  const open = passport.open_obligations || [];

  for (const expectedDate of item.historyDates) {
    if (!history.some((row) => acceptedEtTtHistory(row, expectedDate))) {
      console.error(`FAIL ${item.rfid}: API missing accepted ET+TT history on ${expectedDate}`);
      failures += 1;
    }
  }

  for (const expectedDate of item.openDates) {
    if (!open.some((row) => scheduledEtTtW2(row, expectedDate))) {
      console.error(`FAIL ${item.rfid}: API missing scheduled ET+TT W2 open row on ${expectedDate}`);
      failures += 1;
    }
  }

  if (item.openDates.length === 0 && open.some((row) => row.dose_code === "et_tt_adult_w2")) {
    console.error(`FAIL ${item.rfid}: completed animal still has open ET+TT W2 rows`);
    failures += 1;
  }

  console.log(`OK ${item.rfid}: goat_id=${goatId} history=${JSON.stringify(history.map((row) => [apiDate(row.administered_at), row.status, row.dose_code, row.display_label]))} open=${JSON.stringify(open.map((row) => [apiDate(row.scheduled_for || row.due_at), apiDate(row.clinical_due_at), row.status, row.dose_code, row.display_label]))}`);
}

if (failures > 0) process.exit(1);
console.log("check-cpt-vaccination-passport-display: passed");
