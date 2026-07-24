#!/usr/bin/env node
// check-expected-drive-schedules.mjs — DB-proving gate for the CPT operator-drive rehearsal
// (BUG-010). expected-drive-schedules.json documented the post-seed contract in prose and
// JSON, but nothing ever compared it against the seeded database, so a reseed could report
// "complete" while breaching the 200 unique-animals/operator/day cap, assigning drives to
// the wrong operator, materializing pre-business-date work, or proving a schedule from
// superseded / empty shell batches.
//
// Wired into tools/dev/seed-closeout.sh; it FAILS the closeout (non-zero exit).
//
// Two tiers, deliberately separated:
//
//   INVARIANTS (always enforced) — properties that must hold for ANY seed of this packet,
//   independent of which discussed drive-policy override the maintainer applied:
//     * no (operator, business date) exceeds the contract animal cap;
//     * no planned_date carries more distinct operators than active_operators_per_day;
//     * every drive operator is one of the contract's declared operators;
//     * no open drive work before the contract business date;
//     * no drive rows outside the contract park (never CBE/Coimbatore);
//     * no superseded batch and no zero-obligation shell batch is presented as schedule.
//
//   VARIANT COMPARISON (opt-in via GOATOS_EXPECTED_DRIVE_VARIANT=<variant id>) — the exact
//   per-date/operator/animal-count rows of one named variant. These are NOT unconditional:
//   the "ET+TT only on 2026-07-24, PPR moved to 2026-08-07" plan exists only after the drive
//   date override is applied, and expected-drive-schedules.json itself records the
//   natural-interleaving output as a mismatch report rather than a seed defect.
//
// Usage:
//   node check-expected-drive-schedules.mjs --self-test
//   DATABASE_URL=... GOATOS_TENANT_ID=... node check-expected-drive-schedules.mjs \
//       [--expected <expected-drive-schedules.json>]

import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const packetDir = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_EXPECTED = path.join(packetDir, "expected-drive-schedules.json");
const FORBIDDEN_PARKS = ["CBE", "Coimbatore"];

function loadExpected(file) {
  const expected = JSON.parse(fs.readFileSync(file, "utf8"));
  const cap = expected?.operator_rules?.default_operator?.animal_cap_per_day;
  const activeOperatorsPerDay = expected?.operator_rules?.default_active_operators_per_day;
  const park = String(expected?.park?.code ?? "").trim();
  const businessDate = String(expected?.source_business_date ?? "").trim();
  const problems = [];
  if (!Number.isInteger(cap) || cap < 1) problems.push("operator_rules.default_operator.animal_cap_per_day must be a positive integer");
  if (!Number.isInteger(activeOperatorsPerDay) || activeOperatorsPerDay < 1) problems.push("operator_rules.default_active_operators_per_day must be a positive integer");
  if (!park) problems.push("park.code is required");
  if (!/^\d{4}-\d{2}-\d{2}$/.test(businessDate)) problems.push("source_business_date must be YYYY-MM-DD");
  if (problems.length) throw new Error(`expected-drive-schedules.json is unusable: ${problems.join("; ")}`);
  const operatorNames = new Set();
  for (const key of ["default_operator", "fallback_operator_when_default_off_or_on_leave", "other_operator"]) {
    const name = expected?.operator_rules?.[key]?.display_name;
    if (name) operatorNames.add(name);
  }
  return { expected, cap, activeOperatorsPerDay, park, businessDate, operatorNames };
}

function psql(sql) {
  const url = process.env.DATABASE_URL;
  if (!url) throw new Error("DATABASE_URL is required");
  let out;
  try {
    out = execFileSync("psql", [url, "-qAt", "-F", "", "-v", "ON_ERROR_STOP=1", "-c", sql], {
      encoding: "utf8",
      maxBuffer: 32 * 1024 * 1024,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (err) {
    // A missing relation means the target database is behind this build's migrations. Fail
    // closed with a named cause instead of a node stack trace: an unprovable database is
    // never a passing closeout.
    const detail = String(err.stderr || err.message).trim().split("\n")[0];
    throw new Error(
      `database query failed (${detail}); apply migrations before closeout -- this gate cannot ` +
      "prove a drive schedule against a database behind the build's migrations",
    );
  }
  return out.split("\n").filter((line) => line.length > 0).map((line) => line.split(""));
}

// Distinct animals per (operator, planned_date) for non-superseded vaccination drive batches.
// Grain note (aggregates-and-projections lens): capacity is DISTINCT goats per operator per
// business date. Count them from the EXACT membership table vaccination_drive_assignment_members
// (one row per goat-obligation, keyed to the assignment_id the goat actually landed on), NOT by
// joining obligation_instances to the whole batch: a batch can hold several operator arms, so the
// coarse batch join attributes every batch obligation to every operator on it and can both
// over-count one operator and hide a real breach. The exact member join makes the proof tie to the
// operator each goat is truly assigned to, so three same-day batches on one operator
// (120 + 84 + 17 = 221) correctly aggregate to a single 221 > 200 breach.
const CAP_SQL = (tenant) => `
SELECT vda.planned_date::text,
       COALESCE(wm.display_name, '(unassigned)'),
       count(DISTINCT m.goat_id)
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob ON ob.tenant_id = vda.tenant_id AND ob.batch_id = vda.batch_id
JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = vda.tenant_id AND m.assignment_id = vda.assignment_id
LEFT JOIN workforce_members wm ON wm.workforce_member_id = vda.operator_id
WHERE vda.tenant_id = '${tenant}'::uuid
  AND ob.status <> 'superseded'
GROUP BY vda.planned_date, COALESCE(wm.display_name, '(unassigned)'), vda.operator_id
ORDER BY 1, 2`;

const PARK_SQL = (tenant) => `
SELECT DISTINCT COALESCE(p.location_code, p.name)
FROM vaccination_drive_assignments vda
JOIN locations p ON p.tenant_id = vda.tenant_id AND p.location_id = vda.park_id
WHERE vda.tenant_id = '${tenant}'::uuid`;

const SHELL_BATCH_SQL = (tenant) => `
SELECT ob.status, count(*)
FROM obligation_batches ob
JOIN vaccination_drive_assignments vda ON vda.tenant_id = ob.tenant_id AND vda.batch_id = ob.batch_id
WHERE ob.tenant_id = '${tenant}'::uuid
  AND NOT EXISTS (
    SELECT 1 FROM obligation_instances oi
    WHERE oi.tenant_id = ob.tenant_id AND oi.batch_id = ob.batch_id
  )
GROUP BY 1`;

const VACCINE_BY_DATE_SQL = (tenant) => `
SELECT vda.planned_date::text, pr.dose_code, count(DISTINCT oi.target_id)
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob ON ob.tenant_id = vda.tenant_id AND ob.batch_id = vda.batch_id
JOIN obligation_instances oi ON oi.tenant_id = vda.tenant_id AND oi.batch_id = vda.batch_id
JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
WHERE vda.tenant_id = '${tenant}'::uuid
  AND ob.status <> 'superseded'
GROUP BY 1, 2
ORDER BY 1, 2`;

const OPERATOR_CONFIG_SQL = (tenant) => `
SELECT wm.display_name, wp.vaccination_daily_animal_cap::text, sc.week_off_weekday
FROM vaccination_operator_shift_config sc
JOIN workforce_members wm ON wm.workforce_member_id = sc.operator_id
LEFT JOIN workforce_positions wp
  ON wp.tenant_id = sc.tenant_id AND wp.workforce_member_id = sc.operator_id AND wp.status = 'active'
WHERE sc.tenant_id = '${tenant}'::uuid
ORDER BY 1`;

export function evaluate({ contract, capRows, parkRows, shellRows, operatorRows, variant, vaccineRows }) {
  const failures = [];
  const { cap, activeOperatorsPerDay, park, businessDate, operatorNames } = contract;

  const operatorsByDate = new Map();
  for (const [date, operator, animalsText] of capRows) {
    const animals = Number(animalsText);
    if (animals > cap) {
      failures.push(`cap breach: ${date} operator ${operator} has ${animals} distinct animals (cap ${cap})`);
    }
    if (date < businessDate) {
      failures.push(`pre-business-date drive: ${date} is before the contract business date ${businessDate}`);
    }
    if (operator === "(unassigned)") {
      failures.push(`unassigned drive: ${date} has a drive assignment with no operator`);
    } else if (operatorNames.size && !operatorNames.has(operator)) {
      failures.push(`unknown operator: ${date} assigned to ${operator}, not a contract operator`);
    }
    if (!operatorsByDate.has(date)) operatorsByDate.set(date, new Set());
    operatorsByDate.get(date).add(operator);
  }
  for (const [date, operators] of operatorsByDate) {
    if (operators.size > activeOperatorsPerDay) {
      failures.push(`operator fan-out: ${date} assigned ${operators.size} operators (active_operators_per_day=${activeOperatorsPerDay})`);
    }
  }

  for (const [parkCode] of parkRows) {
    if (FORBIDDEN_PARKS.some((forbidden) => forbidden.toLowerCase() === String(parkCode).toLowerCase())) {
      failures.push(`forbidden park in drive rows: ${parkCode} (this rehearsal is ${park} only)`);
    }
  }

  for (const [status, count] of shellRows) {
    failures.push(`empty shell batch: ${count} ${status} batch(es) carry drive assignments with zero attached obligations`);
  }

  const configuredOperators = new Set(operatorRows.map(([name]) => name));
  for (const name of operatorNames) {
    if (!configuredOperators.has(name)) {
      failures.push(`missing operator shift config: ${name} has no vaccination_operator_shift_config row`);
    }
  }
  for (const [name, capText] of operatorRows) {
    if (operatorNames.has(name) && Number(capText) !== cap) {
      failures.push(`operator cap mismatch: ${name} has vaccination_daily_animal_cap=${capText || "NULL"}, expected ${cap}`);
    }
  }

  if (variant) {
    const byDate = new Map();
    for (const [date, doseCode, animalsText] of vaccineRows) {
      if (!byDate.has(date)) byDate.set(date, new Map());
      byDate.get(date).set(doseCode, Number(animalsText));
    }
    const expectedDates = new Map();
    const token = (value) => String(value).toUpperCase().replace(/[^A-Z]/g, "");
    for (const row of variant.drive_rows ?? []) {
      const families = row.drive ? [row.drive] : (row.vaccines ?? []);
      if (!expectedDates.has(row.date)) expectedDates.set(row.date, new Set());
      for (const family of families) expectedDates.get(row.date).add(token(family));
    }
    for (const [date, families] of expectedDates) {
      const seeded = byDate.get(date);
      if (!seeded) {
        failures.push(`variant ${variant.id}: expected drive rows on ${date}, database has none`);
        continue;
      }
      for (const doseCode of seeded.keys()) {
        if (![...families].some((family) => token(doseCode).startsWith(family))) {
          failures.push(`variant ${variant.id}: ${date} carries dose_code ${doseCode}, expected only ${[...families].join(", ")}`);
        }
      }
    }
  }
  return failures;
}

function selfTest() {
  const contract = {
    cap: 200,
    activeOperatorsPerDay: 1,
    park: "CPT",
    businessDate: "2026-07-23",
    operatorNames: new Set(["Darshan Talwar", "Sagar Mahoor", "Amit Kumar"]),
  };
  const clean = {
    contract,
    capRows: [["2026-07-24", "Darshan Talwar", "200"], ["2026-07-25", "Darshan Talwar", "124"]],
    parkRows: [["CPT"]],
    shellRows: [],
    operatorRows: [["Amit Kumar", "200", "friday"], ["Darshan Talwar", "200", "sunday"], ["Sagar Mahoor", "200", "saturday"]],
    variant: null,
    vaccineRows: [],
  };
  const cases = [
    ["clean seed passes", clean, 0],
    ["cap breach fails", { ...clean, capRows: [["2026-07-24", "Darshan Talwar", "231"]] }, 1],
    ["operator fan-out fails", { ...clean, capRows: [["2026-07-24", "Darshan Talwar", "200"], ["2026-07-24", "Sagar Mahoor", "124"]] }, 1],
    ["pre-business-date drive fails", { ...clean, capRows: [["2026-07-22", "Darshan Talwar", "10"]] }, 1],
    ["unknown operator fails", { ...clean, capRows: [["2026-07-24", "Someone Else", "10"]] }, 1],
    ["forbidden park fails", { ...clean, parkRows: [["CPT"], ["CBE"]] }, 1],
    ["empty shell batch fails", { ...clean, shellRows: [["superseded", "2"]] }, 1],
    ["missing shift config fails", { ...clean, operatorRows: [["Darshan Talwar", "200", "sunday"]] }, 2],
    ["wrong cap fails", { ...clean, operatorRows: [["Amit Kumar", "200", "friday"], ["Darshan Talwar", "50", "sunday"], ["Sagar Mahoor", "200", "saturday"]] }, 1],
  ];
  let bad = 0;
  for (const [label, input, expectedCount] of cases) {
    const failures = evaluate(input);
    if (failures.length !== expectedCount) {
      console.error(`self-test FAIL: ${label} -> ${failures.length} failures, expected ${expectedCount}: ${failures.join(" | ")}`);
      bad += 1;
    } else {
      console.log(`self-test ok: ${label} (${failures.length} failure(s))`);
    }
  }
  if (bad) process.exit(1);
  console.log("check-expected-drive-schedules: self-test passed");
}

function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) {
    selfTest();
    return;
  }
  const expectedIndex = args.indexOf("--expected");
  const expectedFile = expectedIndex >= 0 ? args[expectedIndex + 1] : DEFAULT_EXPECTED;
  const contract = loadExpected(expectedFile);
  const tenant = process.env.GOATOS_TENANT_ID || "00000000-0000-4000-8000-000000000001";
  if (!/^[0-9a-fA-F-]{36}$/.test(tenant)) {
    console.error(`check-expected-drive-schedules: GOATOS_TENANT_ID ${tenant} is not a uuid`);
    process.exit(2);
  }

  const variantID = String(process.env.GOATOS_EXPECTED_DRIVE_VARIANT ?? "").trim();
  let variant = null;
  if (variantID) {
    variant = (contract.expected.variants ?? []).find((v) => v.id === variantID) ?? null;
    if (!variant) {
      console.error(`check-expected-drive-schedules: GOATOS_EXPECTED_DRIVE_VARIANT=${variantID} is not declared in ${expectedFile}`);
      process.exit(2);
    }
  }

  const failures = evaluate({
    contract,
    capRows: psql(CAP_SQL(tenant)),
    parkRows: psql(PARK_SQL(tenant)),
    shellRows: psql(SHELL_BATCH_SQL(tenant)),
    operatorRows: psql(OPERATOR_CONFIG_SQL(tenant)),
    variant,
    vaccineRows: variant ? psql(VACCINE_BY_DATE_SQL(tenant)) : [],
  });

  if (failures.length) {
    console.error(`check-expected-drive-schedules: ${failures.length} expectation(s) violated against ${expectedFile}`);
    for (const failure of failures) console.error(`  - ${failure}`);
    process.exit(1);
  }
  console.log(
    `check-expected-drive-schedules: DB matches ${path.basename(expectedFile)} ` +
    `(park=${contract.park} cap=${contract.cap} N=${contract.activeOperatorsPerDay}${variant ? ` variant=${variant.id}` : ""})`,
  );
}

try {
  main();
} catch (err) {
  console.error(`check-expected-drive-schedules: ${err.message}`);
  process.exit(1);
}
