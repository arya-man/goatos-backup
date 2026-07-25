#!/usr/bin/env node
// materialize-source.mjs — turn this committed CPT operator-drive packet into the
// normalized seed-source bundle that `tools/dev/validate-vaccination-hrms-source.mjs`,
// `backend/cmd/seed-roster-real`, and `backend/cmd/seed-vaccination-real` actually
// consume (BUG-009: the packet documented raw/ filenames while every executable path
// requires root-level normalized filenames).
//
// Root cause being fixed: the packet was documentation-shaped, not command-shaped.
// Rather than committing a second 250KB copy of the raw animal/vaccination rows, the
// documented seed command now performs the transformation itself, deterministically,
// from exactly two committed inputs:
//
//   raw/CPT-Adult-goats.json        -> <out>/goats.json          (byte-identical copy)
//   raw/CPT-Adult-vaccination.json  -> <out>/vaccination.json    (byte-identical copy)
//   cpt-operator-roster.json        -> <out>/cpt-operator-roster.json (copy)
//                                   -> <out>/attendance-jun-26.json
//                                   -> <out>/timetable-goats-team-v1.json
//                                   -> <out>/roster-name-mapping.jun26-review.csv
//                                   -> <out>/shed-manager-mapping.jul11-vaccination.csv
//
// The HRMS files are DERIVED from the roster contract, never hand-authored, so the
// contract stays the single source of truth for who the CPT operators are, their
// week-offs, and their caps.
//
// The output directory must live OUTSIDE fixtures/: the source validator treats any
// path containing /fixtures/ as a committed fixture and then (correctly) rejects real
// staff names. The CPT roster carries reviewed runtime names, so the materialized
// bundle is a local, gitignored build artifact — never committed.
//
// CPT invariant (fixtures/.../README.md + LOCAL_DB_RESEED_VALIDATION.md), enforced here:
//   - Channapatna (CPT) only; CBE/Coimbatore rows are never synthesized;
//   - Amit Kumar / Darshan Talwar / Sagar Mahoor are EQUAL vaccination operators;
//   - every CPT physical shed gets Darshan as manager and Sagar as backup;
//   - Chandrakant is director-only monitoring (no operator seat, no field capacity);
//   - business/as-of date 2026-07-24.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const packetDir = path.dirname(fileURLToPath(import.meta.url));

// The three reviewed CPT timetable seats the generic jun-26 model requires per center.
// seed-roster-real's operator-roster overlay immediately recasts each of them into a
// per-person `vaccination_operator_<name>` manager-tier position, so the seat label is
// a source-shape requirement only, never the seeded role.
const REQUIRED_CENTER_SEATS = ["Preventive Care Manager", "Backup Manager", "Park Head"];

const ATTENDANCE_HEADER = [
  "Name", "Type", "Designation Type", "Designation", "Location",
  ...Array.from({ length: 30 }, (_, index) => String(index + 1)),
];
const TIMETABLE_HEADER = ["", "Shift", "CBE", "CPT", "Week OFFs", "Backup"];
const ROSTER_HEADER = [
  "center", "timetable_position", "timetable_name", "jun26_candidate",
  "designation_type", "designation", "location", "confidence", "notes",
];
const MANAGER_HEADER = [
  "shed_code", "shed_name", "park_code", "manager_code", "manager_name",
  "assignment_source", "source_ref", "confidence", "needs_review", "manager_role",
  "backup_manager_code", "backup_manager_name", "backup_role", "backup_source_ref",
  "goat_count", "notes",
];

const FORBIDDEN_CENTERS = new Set(["CBE", "COIMBATORE"]);

function fail(message) {
  console.error(`materialize-source: ${message}`);
  process.exit(1);
}

function titleCase(value) {
  const text = String(value ?? "").trim();
  return text ? text[0].toUpperCase() + text.slice(1).toLowerCase() : "";
}

function seedLocationCode(name) {
  let code = String(name ?? "").trim().toUpperCase().replace(/[^A-Z0-9]/g, "_");
  while (code.includes("__")) code = code.replaceAll("__", "_");
  code = code.replace(/^_+|_+$/g, "");
  return code || "UNKNOWN";
}

function shedCode(farm, shed) {
  const slug = seedLocationCode(shed);
  return `${seedLocationCode(farm)}_SHED_${slug === "UNKNOWN" ? "SHED" : slug}`;
}

function normalizeShedPartitionName(raw) {
  const name = String(raw ?? "").trim().replace(/\s+/g, " ");
  if (!name) return { physical: "", partition: "whole" };
  const partMatch = /^(.*?)\s*-\s*Part\s+(\d+)$/i.exec(name);
  if (partMatch) return { physical: partMatch[1].trim(), partition: `Part ${partMatch[2]}` };
  const numberMatch = /^(.*?)\s+(\d+)$/.exec(name);
  if (numberMatch) return { physical: numberMatch[1].trim(), partition: numberMatch[2] };
  return { physical: name, partition: "whole" };
}

function csv(rows) {
  return rows
    .map((row) => row.map((value) => {
      const text = String(value ?? "");
      return /[",\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
    }).join(","))
    .join("\n") + "\n";
}

function readContract() {
  const file = path.join(packetDir, "cpt-operator-roster.json");
  const contract = JSON.parse(fs.readFileSync(file, "utf8"));
  const park = String(contract?.source_scope?.park_code ?? "").trim();
  if (!park) fail("cpt-operator-roster.json is missing source_scope.park_code");
  if (FORBIDDEN_CENTERS.has(park.toUpperCase())) {
    fail(`cpt-operator-roster.json park_code ${park} is a forbidden center for this rehearsal packet`);
  }
  const operators = Array.isArray(contract.operators) ? contract.operators : [];
  if (operators.length !== REQUIRED_CENTER_SEATS.length) {
    fail(
      `this packet materializes exactly ${REQUIRED_CENTER_SEATS.length} equal vaccination operators ` +
      `(one per reviewed ${park} timetable seat); cpt-operator-roster.json declares ${operators.length}`,
    );
  }
  const emails = new Map();
  for (const op of operators) {
    const label = op?.code || op?.display_name || "operator";
    const email = String(op?.email_hint ?? "").trim().toLowerCase();
    if (!email || !email.includes("@")) fail(`${label}: email_hint is required for Android login provisioning`);
    if (emails.has(email)) fail(`${label}: email_hint duplicates ${emails.get(email)}; operator Android logins must be unique`);
    emails.set(email, label);
  }
  const login = contract?.operator_android_login;
  const requiredLogin = {
    required_after_database_seed: true,
    identity_provider: "firebase_email_password",
    source_email_field: "operators[].email_hint",
    unique_email_per_operator: true,
    unique_temporary_password_per_operator: true,
    shared_password_forbidden: true,
    plaintext_passwords_in_git_forbidden: true,
    must_send_or_record_individual_reset_flow: true,
    android_login_smoke_required: true,
  };
  if (!login || typeof login !== "object") fail("cpt-operator-roster.json is missing operator_android_login contract");
  for (const [key, expected] of Object.entries(requiredLogin)) {
    if (login[key] !== expected) fail(`operator_android_login.${key} must be ${JSON.stringify(expected)}`);
  }
  for (const center of contract?.source_scope?.centers_allowed ?? [park]) {
    if (FORBIDDEN_CENTERS.has(String(center).toUpperCase())) {
      fail(`centers_allowed contains forbidden center ${center}`);
    }
  }
  return { contract, park, operators };
}

function verifyRawHeader(values, expectedFirstCell, file) {
  if (!Array.isArray(values?.values) || values.values.length < 2) {
    fail(`${file}: expected a non-empty top-level values array`);
  }
  const first = String(values.values[0]?.[0] ?? "").trim();
  if (first !== expectedFirstCell) {
    fail(`${file}: unexpected first header cell ${JSON.stringify(first)} (expected ${JSON.stringify(expectedFirstCell)})`);
  }
}

function copyRaw(rawName, outName, expectedFirstCell, outDir) {
  const from = path.join(packetDir, "raw", rawName);
  if (!fs.existsSync(from)) fail(`missing committed raw input ${path.relative(packetDir, from)}`);
  const text = fs.readFileSync(from, "utf8");
  verifyRawHeader(JSON.parse(text), expectedFirstCell, rawName);
  fs.writeFileSync(path.join(outDir, outName), text);
}

function buildAttendance(operators, directors, park) {
  const rows = [ATTENDANCE_HEADER];
  const blankDays = Array.from({ length: 30 }, () => "");
  for (const op of operators) {
    // Day cells stay blank: "" is explicit no-data. Week-offs are recurring roster facts
    // carried by the timetable + the contract, never fabricated attendance absences.
    rows.push([op.display_name, "Staff", "Manager", "Vaccination Operator", park, ...blankDays]);
  }
  for (const director of directors) {
    rows.push([director.display_name, "Staff", "Director", "Preventive Care Director", park, ...blankDays]);
  }
  return { values: rows };
}

function buildTimetable(seats, park) {
  const rows = [TIMETABLE_HEADER];
  for (const { seat, operator } of seats) {
    rows.push([seat, String(operator.shift_label ?? "").toUpperCase(), "--", operator.display_name, titleCase(operator.week_off), "--"]);
  }
  return { values: rows };
}

function buildRosterCSV(seats, park) {
  const rows = [ROSTER_HEADER];
  for (const { seat, operator } of seats) {
    rows.push([
      park, seat, operator.display_name, operator.display_name,
      "Manager", "Vaccination Operator", park, "REVIEWED",
      `operator-drive contract seat; recast to ${operator.code}`,
    ]);
  }
  return csv(rows);
}

function buildShedManagerCSV(park, manager, backup) {
  const rawPath = path.join(packetDir, "raw", "CPT-Adult-goats.json");
  const goats = JSON.parse(fs.readFileSync(rawPath, "utf8")).values;
  const header = goats[0].map((value) => String(value ?? "").trim().toLowerCase());
  const farmIndex = header.indexOf("farm");
  const shedIndex = header.indexOf("shed");
  if (farmIndex < 0 || shedIndex < 0) fail("CPT-Adult-goats.json must include farm and shed columns");

  const byShed = new Map();
  for (const row of goats.slice(1)) {
    const farm = String(row[farmIndex] ?? "").trim();
    const rawShed = String(row[shedIndex] ?? "").trim();
    if (!farm || !rawShed) continue;
    const { physical } = normalizeShedPartitionName(rawShed);
    if (!physical) continue;
    const key = `${farm}\u0000${physical}`;
    const current = byShed.get(key) ?? { farm, physical, goats: 0 };
    current.goats += 1;
    byShed.set(key, current);
  }

  const rows = [MANAGER_HEADER];
  for (const shed of [...byShed.values()].sort((a, b) => a.physical.localeCompare(b.physical))) {
    rows.push([
      shedCode(shed.farm, shed.physical),
      shed.physical,
      park,
      manager.code,
      manager.display_name,
      "reviewed",
      "cpt-operator-roster.default_operator_assignment",
      "REVIEWED",
      "false",
      "shed_manager",
      backup.code,
      backup.display_name,
      "shed_backup_manager",
      "cpt-operator-roster.default_operator_assignment.fallback_operator_code",
      String(shed.goats),
      "CPT vaccination shed ownership: Darshan manager, Sagar backup fallback",
    ]);
  }
  return csv(rows);
}

function main() {
  const args = process.argv.slice(2);
  const outIndex = args.indexOf("--out");
  if (outIndex < 0 || !args[outIndex + 1]) {
    console.error("usage: materialize-source.mjs --out <dir>");
    process.exit(2);
  }
  const outDir = path.resolve(args[outIndex + 1]);
  if (outDir.split(path.sep).includes("fixtures")) {
    fail(
      `--out ${outDir} is inside a fixtures/ path; the materialized bundle carries reviewed runtime ` +
      "staff names and must never be committed. Point --out at a gitignored build directory.",
    );
  }

  const { contract, park, operators } = readContract();
  const directors = Array.isArray(contract.directors) ? contract.directors : [];
  const seats = REQUIRED_CENTER_SEATS.map((seat, index) => ({ seat, operator: operators[index] }));
  const manager = operators.find((operator) => operator.code === "vaccination_operator_darshan");
  const backup = operators.find((operator) => operator.code === "vaccination_operator_sagar");
  if (!manager) fail("cpt-operator-roster.json must declare vaccination_operator_darshan for shed manager ownership");
  if (!backup) fail("cpt-operator-roster.json must declare vaccination_operator_sagar for shed backup ownership");

  fs.mkdirSync(outDir, { recursive: true });
  copyRaw("CPT-Adult-goats.json", "goats.json", "rfid", outDir);
  copyRaw("CPT-Adult-vaccination.json", "vaccination.json", "Farm", outDir);
  fs.copyFileSync(
    path.join(packetDir, "cpt-operator-roster.json"),
    path.join(outDir, "cpt-operator-roster.json"),
  );
  fs.writeFileSync(
    path.join(outDir, "attendance-jun-26.json"),
    JSON.stringify(buildAttendance(operators, directors, park), null, 2) + "\n",
  );
  fs.writeFileSync(
    path.join(outDir, "timetable-goats-team-v1.json"),
    JSON.stringify(buildTimetable(seats, park), null, 2) + "\n",
  );
  fs.writeFileSync(path.join(outDir, "roster-name-mapping.jun26-review.csv"), buildRosterCSV(seats, park));
  fs.writeFileSync(path.join(outDir, "shed-manager-mapping.jul11-vaccination.csv"), buildShedManagerCSV(park, manager, backup));

  console.log(
    `materialize-source: park=${park} operators=${operators.length} directors=${directors.length} out=${outDir}`,
  );
}

main();
