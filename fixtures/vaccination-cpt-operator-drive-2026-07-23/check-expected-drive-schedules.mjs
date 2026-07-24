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
//   per-date/operator/animal-count rows of one named variant. This CPT seed packet currently
//   schedules ET+TT only; expected-drive-schedules.json also marks PPR and adult-entry-date
//   spillover dose-code prefixes as forbidden from seeded drive output.
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
  const prohibitedDosePrefixes = Array.isArray(expected?.prohibited_drive_dose_code_prefixes)
    ? expected.prohibited_drive_dose_code_prefixes.map((value) => String(value).trim().toLowerCase()).filter(Boolean)
    : [];
  const seedCatchupOverrides = Array.isArray(expected?.operator_rules?.seed_catchup_overrides)
    ? expected.operator_rules.seed_catchup_overrides.map((row) => ({
      date: String(row?.date ?? "").trim(),
      operator: String(row?.operator ?? "").trim(),
      maxAnimals: Number(row?.max_animals),
    })).filter((row) => /^\d{4}-\d{2}-\d{2}$/.test(row.date) && row.operator && Number.isInteger(row.maxAnimals) && row.maxAnimals >= cap)
    : [];
  return { expected, cap, activeOperatorsPerDay, park, businessDate, operatorNames, prohibitedDosePrefixes, seedCatchupOverrides };
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
SELECT vda.planned_date::text,
       COALESCE(wm.display_name, '(unassigned)'),
       pr.dose_code,
       count(DISTINCT m.goat_id)
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob ON ob.tenant_id = vda.tenant_id AND ob.batch_id = vda.batch_id
JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = vda.tenant_id AND m.assignment_id = vda.assignment_id
JOIN obligation_instances oi ON oi.tenant_id = m.tenant_id AND oi.obligation_id = m.obligation_id
JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
LEFT JOIN workforce_members wm ON wm.workforce_member_id = vda.operator_id
WHERE vda.tenant_id = '${tenant}'::uuid
  AND ob.status <> 'superseded'
  AND oi.status IN ('scheduled', 'due', 'missed')
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3`;

const DUPLICATE_OPEN_ASSIGNMENT_SQL = (tenant) => `
SELECT pr.dose_code,
       m.goat_id::text,
       count(DISTINCT m.assignment_id)::text
FROM vaccination_drive_assignment_members m
JOIN obligation_instances oi ON oi.tenant_id = m.tenant_id AND oi.obligation_id = m.obligation_id
JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
JOIN obligation_batches ob ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
WHERE m.tenant_id = '${tenant}'::uuid
  AND oi.status IN ('scheduled', 'due', 'missed')
  AND ob.status <> 'superseded'
GROUP BY 1, 2
HAVING count(DISTINCT m.assignment_id) > 1
ORDER BY 1, 2
LIMIT 20`;

const OPERATOR_CONFIG_SQL = (tenant) => `
SELECT wm.display_name, wp.vaccination_daily_animal_cap::text, sc.week_off_weekday
FROM vaccination_operator_shift_config sc
JOIN workforce_members wm ON wm.workforce_member_id = sc.operator_id
LEFT JOIN workforce_positions wp
  ON wp.tenant_id = sc.tenant_id AND wp.workforce_member_id = sc.operator_id AND wp.status = 'active'
WHERE sc.tenant_id = '${tenant}'::uuid
ORDER BY 1`;

const ADULT_POST_ARRIVAL_RULE_SQL = (tenant) => `
SELECT pr.dose_code, pr.trigger_type
FROM protocol_rules pr
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id
 AND pv.protocol_version_id = pr.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id
 AND pd.protocol_id = pv.protocol_id
WHERE pr.tenant_id = '${tenant}'::uuid
  AND pv.status = 'published'
  AND pd.category = 'vaccination'
  AND pr.dose_code LIKE '%\\_adult\\_%' ESCAPE '\\'
  AND pr.dose_code NOT LIKE '%\\_revac' ESCAPE '\\'
  AND pr.trigger_type = 'post_arrival'
ORDER BY 1`;

export function evaluate({ contract, capRows, parkRows, shellRows, operatorRows, variant, vaccineRows, duplicateOpenAssignmentRows = [], adultPostArrivalRuleRows = [] }) {
  const failures = [];
  const { cap, activeOperatorsPerDay, park, businessDate, operatorNames, prohibitedDosePrefixes = [], seedCatchupOverrides = [] } = contract;
  const capFor = (date, operator) => {
    const override = seedCatchupOverrides.find((row) => row.date === date && row.operator === operator);
    return override?.maxAnimals ?? cap;
  };

  const operatorsByDate = new Map();
  for (const [date, operator, animalsText] of capRows) {
    const animals = Number(animalsText);
    const allowedCap = capFor(date, operator);
    if (animals > allowedCap) {
      failures.push(`cap breach: ${date} operator ${operator} has ${animals} distinct animals (cap ${allowedCap})`);
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

  for (const [doseCode, goatID, count] of duplicateOpenAssignmentRows) {
    failures.push(`duplicate open assignment: goat ${goatID} has ${count} open assignments for ${doseCode}`);
  }

  for (const [doseCode, triggerType] of adultPostArrivalRuleRows) {
    failures.push(`adult entry-date anchor rule: ${doseCode} has trigger_type=${triggerType}; adult initial vaccination rules must use campaign/catch-up, never post_arrival`);
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

  for (const row of vaccineRows) {
    const date = row[0];
    const doseCode = row.length === 3 ? row[1] : row[2];
    const animalsText = row.length === 3 ? row[2] : row[3];
    const normalizedDose = String(doseCode ?? "").trim().toLowerCase();
    const prefix = prohibitedDosePrefixes.find((candidate) => normalizedDose.startsWith(candidate));
    if (prefix) {
      failures.push(`prohibited seed vaccine: ${date} has ${animalsText} animal(s) for ${doseCode}; ${prefix.toUpperCase()} is excluded from this seed packet`);
    }
  }

  if (variant) {
    const byDateOperator = new Map();
    const expectedByFamily = new Map();
    for (const row of vaccineRows) {
      const date = row[0];
      const operator = row.length === 3 ? "" : row[1];
      const doseCode = row.length === 3 ? row[1] : row[2];
      const animals = Number(row.length === 3 ? row[2] : row[3]);
      const key = `${date}\u0000${operator}`;
      if (!byDateOperator.has(key)) byDateOperator.set(key, new Map());
      byDateOperator.get(key).set(doseCode, animals);
    }
    const token = (value) => String(value).toUpperCase().replace(/[^A-Z]/g, "");
    for (const row of variant.drive_rows ?? []) {
      const families = row.drive ? [row.drive] : (row.vaccines ?? []);
      const expectedOperator = String(row.operator ?? "").trim();
      const seeded = byDateOperator.get(`${row.date}\u0000${expectedOperator}`);
      if (!seeded) {
        failures.push(`variant ${variant.id}: expected drive row on ${row.date} for ${expectedOperator}, database has none`);
        continue;
      }
      for (const family of families) {
        const expectedFamily = token(family);
        const matches = [...seeded.entries()].filter(([doseCode]) => token(doseCode).startsWith(expectedFamily));
        if (matches.length === 0) {
          failures.push(`variant ${variant.id}: ${row.date} ${expectedOperator} missing expected ${expectedFamily} drive`);
          continue;
        }
        if (Number.isInteger(row.animals_scheduled)) {
          expectedByFamily.set(expectedFamily, (expectedByFamily.get(expectedFamily) ?? 0) + row.animals_scheduled);
          const actual = matches.reduce((sum, [, animals]) => sum + animals, 0);
          if (actual !== row.animals_scheduled) {
            failures.push(`variant ${variant.id}: ${row.date} ${expectedOperator} ${expectedFamily} has ${actual} animals, expected ${row.animals_scheduled}`);
          }
        }
      }
    }
    for (const [expectedFamily, expectedAnimals] of expectedByFamily) {
      let actualAnimals = 0;
      const actualRows = [];
      for (const row of vaccineRows) {
        const date = row[0];
        const operator = row.length === 3 ? "" : row[1];
        const doseCode = row.length === 3 ? row[1] : row[2];
        const animals = Number(row.length === 3 ? row[2] : row[3]);
        if (!token(doseCode).startsWith(expectedFamily)) continue;
        actualAnimals += animals;
        actualRows.push(`${date} ${operator || "(no operator)"} ${doseCode} ${animals}`);
      }
      if (actualAnimals !== expectedAnimals) {
        failures.push(`variant ${variant.id}: ${expectedFamily} total has ${actualAnimals} animals across open assignments, expected ${expectedAnimals}; rows: ${actualRows.join("; ") || "(none)"}`);
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
    prohibitedDosePrefixes: ["ppr"],
  };
  const clean = {
    contract,
    capRows: [["2026-07-24", "Darshan Talwar", "200"], ["2026-07-25", "Darshan Talwar", "124"]],
    parkRows: [["CPT"]],
    shellRows: [],
    operatorRows: [["Amit Kumar", "200", "friday"], ["Darshan Talwar", "200", "sunday"], ["Sagar Mahoor", "200", "saturday"]],
    variant: null,
    vaccineRows: [],
    duplicateOpenAssignmentRows: [],
    adultPostArrivalRuleRows: [],
  };
  const exactVariant = {
    id: "exact-210",
    drive_rows: [{
      date: "2026-07-25",
      operator: "Darshan Talwar",
      drive: "ET+TT",
      animals_scheduled: 210,
    }],
  };
  const cases = [
    ["clean seed passes", clean, 0],
    ["cap breach fails", { ...clean, capRows: [["2026-07-24", "Darshan Talwar", "231"]] }, 1],
    ["multi-batch aggregate cap breach fails", { ...clean, capRows: [["2026-08-23", "Sagar Mahoor", String(120 + 84 + 17)]] }, 1],
    ["operator fan-out fails", { ...clean, capRows: [["2026-07-24", "Darshan Talwar", "200"], ["2026-07-24", "Sagar Mahoor", "124"]] }, 1],
    ["pre-business-date drive fails", { ...clean, capRows: [["2026-07-22", "Darshan Talwar", "10"]] }, 1],
    ["unknown operator fails", { ...clean, capRows: [["2026-07-24", "Someone Else", "10"]] }, 1],
    ["forbidden park fails", { ...clean, parkRows: [["CPT"], ["CBE"]] }, 1],
    ["empty shell batch fails", { ...clean, shellRows: [["superseded", "2"]] }, 1],
    ["missing shift config fails", { ...clean, operatorRows: [["Darshan Talwar", "200", "sunday"]] }, 2],
    ["wrong cap fails", { ...clean, operatorRows: [["Amit Kumar", "200", "friday"], ["Darshan Talwar", "50", "sunday"], ["Sagar Mahoor", "200", "saturday"]] }, 1],
    ["prohibited PPR drive fails", { ...clean, vaccineRows: [["2026-08-07", "ppr_adult_w1", "124"]] }, 1],
    ["adult post-arrival rule fails", { ...clean, adultPostArrivalRuleRows: [["fmd_adult_w1", "post_arrival"]] }, 1],
    ["variant exact date/operator/count passes", { ...clean, variant: exactVariant, vaccineRows: [["2026-07-25", "Darshan Talwar", "et_tt_adult_w2", "210"]] }, 0],
    ["variant exact date/operator/count fails", { ...clean, variant: exactVariant, vaccineRows: [["2026-07-25", "Darshan Talwar", "et_tt_adult_w2", "200"]] }, 2],
    ["variant extra ET+TT rows fail", { ...clean, variant: exactVariant, vaccineRows: [["2026-07-25", "Darshan Talwar", "et_tt_adult_w2", "210"], ["2026-07-26", "Sagar Mahoor", "et_tt_adult_w2", "199"], ["2026-07-27", "Darshan Talwar", "et_tt_adult_w2", "11"]] }, 1],
    ["duplicate goat dose assignments fail", { ...clean, duplicateOpenAssignmentRows: [["et_tt_adult_w2", "goat-1", "2"]] }, 1],
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
    vaccineRows: (variant || contract.prohibitedDosePrefixes.length) ? psql(VACCINE_BY_DATE_SQL(tenant)) : [],
    duplicateOpenAssignmentRows: psql(DUPLICATE_OPEN_ASSIGNMENT_SQL(tenant)),
    adultPostArrivalRuleRows: psql(ADULT_POST_ARRIVAL_RULE_SQL(tenant)),
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
