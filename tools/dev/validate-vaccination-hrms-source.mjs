#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  VACCINE_COLUMNS,
  SEED_SOURCE_POLICY,
  cell,
  dayDiff,
  deriveSpecies,
  earliestDate,
  headerMap,
  isDatedVaccination,
  parseCSV,
  parseDate,
  sourceAnimalKey,
} from "./vaccination-hrms-fixture-lib.mjs";

const INPUT_FILES = [
  "goats.json",
  "vaccination.json",
  "attendance-jun-26.json",
  "timetable-goats-team-v1.json",
  "roster-name-mapping.jun26-review.csv",
  "shed-manager-mapping.jul11-vaccination.csv",
];

const SENSITIVE_HEADER = /salary|incentive|\bdoj\b|advance|\badv\b|ptax|tds|bank|ifsc|account|payroll|payment|\bpaid\b|tax|e-?mail|phone|mobile|aadhaar|aadhar|\bpan\b/i;
const SENSITIVE_VALUE = /(?:[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}|(?:\+?91[-\s]?)?[6-9]\d{9}|\b\d{9,18}\b)/i;
const PAYMENT_VALUE = /^(paid|unpaid|salary|payroll)$/i;
const EVENT_FIELDS = [
  "delivery_date", "abortion_date", "disease_1_date", "disease_2_date",
  "disease_3_date", "latest_weight_date", "last_event_date",
];

const GOAT_HEADERS = [
  "rfid", "goat_id", "farm_goat_id", "old_id", "old_id_suffix", "mapped_farm_goat_id", "has_mapping",
  "farm", "shed", "shed_tag", "partition", "stage", "stage_entry_date", "days_in_stage", "age", "breed",
  "gender", "dob", "birth_time", "birth_weight", "origin_type", "mother_id", "mother_breed", "mother_origin",
  "mother_vendor", "mother_load_id", "mother_purchase_date", "kids_born_count", "delivery_date", "has_deliveries",
  "colostrum_status", "is_milking_mother", "purchase_vendor", "purchase_load_id", "purchase_date", "purchase_weight",
  "status", "death_date", "death_reason", "sale_date", "sale_reason", "abortion_date", "last_event", "last_event_date",
  "health_status", "problem_name", "diagnosis", "medicine", "disease_1", "disease_1_date", "disease_2", "disease_2_date",
  "disease_3", "disease_3_date", "latest_weight", "latest_weight_date", "adg_grams_per_day", "total_gain_kg", "load_id",
  "animal_status",
];
const VACCINATION_HEADER = ["Farm", "Old ID", "Old ID Suffix", "RFID", "Age", "Gender", "Breed", "Tag", "Shed", "Partition", "", "ET+TT", "", "PPR", "Blue tongue", "", "FMD", "", "HS", "Goat Pox", "Sheep Pox"];
// The local trigger seed uses one synthetic, reviewed keyboard-wedge RFID so emulator E2E can
// exercise the same scan-capture path as a physical reader without reading private RFID material.
export const LOCAL_TRIGGER_PRIMARY_RFID_FIXTURE = "CBE-RFID-0001";
const VACCINATION_DOSE_HEADER = ["", "", "", "", "", "", "", "", "", "", "", "First Dose", "Booster", "First Dose", "First Dose", "Booster", "First Dose", "Booster", "First Dose", "First Dose", "First Dose"];
const ROSTER_HEADER = ["center", "timetable_position", "timetable_name", "jun26_candidate", "designation_type", "designation", "location", "confidence", "notes"];
const MANAGER_HEADER = ["shed_code", "shed_name", "park_code", "manager_code", "manager_name", "assignment_source", "source_ref", "confidence", "needs_review", "manager_role", "backup_manager_code", "backup_manager_name", "backup_role", "backup_source_ref", "goat_count", "notes"];
const TIMETABLE_HEADER = ["", "Shift", "CBE", "CPT", "Week OFFs", "Backup"];
const SAFE_ATTENDANCE_HEADERS = new Set(["Name", "Type", "Designation Type", "Designation", "Location", ...Array.from({ length: 30 }, (_, index) => String(index + 1))]);
const RAW_ATTENDANCE_EXTRA_HEADERS = new Set(["Basic Salary", "Incentive", "DOJ", "Total Days", "Adv", "PTAX", "TDS", "Salary to Pay", "IFSC Code", "Bank Account Number"]);
// Full-access humans are CEO/CXO only. Source validation and the committed fixture
// must not introduce a parallel admin business/person role.
const FULL_ACCESS_GRANT_ROLE = "ceo_internal";
const FULL_ACCESS_WORKFORCE_HINT = "cxo";

function arraysEqual(left, right) {
  return left.length === right.length && left.every((value, index) => String(value ?? "").trim() === String(right[index] ?? "").trim());
}

function normalized(value) {
  return String(value ?? "").trim().toLowerCase().replace(/\s+/g, " ");
}

function readJSON(directory, name) {
  const parsed = JSON.parse(fs.readFileSync(path.join(directory, name), "utf8"));
  if (!Array.isArray(parsed.values)) throw new Error(`${name}: expected top-level values array`);
  return parsed.values;
}

function pushSample(samples, value) {
  samples.push(value);
}

function makeCheck(id, count, message, action, samples = [], severity = "error") {
  return {
    id,
    status: count === 0 ? "pass" : severity === "warning" ? "warning" : "fail",
    severity,
    count,
    message,
    action,
    samples: samples.slice(0, 8),
  };
}

export function auditSourceDirectory(directory, { dataAsOf = "2026-07-20" } = {}) {
  const source = path.resolve(directory);
  const checks = [];
  const missingFiles = INPUT_FILES.filter((name) => !fs.existsSync(path.join(source, name)));
  checks.push(makeCheck(
    "required_files",
    missingFiles.length,
    "Every source bundle needs the complete animal, vaccination, HRMS roster, timetable, and shed-owner set.",
    "Add the missing files before transformation or any database write.",
    missingFiles,
  ));
  if (missingFiles.length) {
    return {
      schema_version: 1,
      data_as_of: dataAsOf,
      valid_for_direct_seed: false,
      summary: { files: INPUT_FILES.length - missingFiles.length, bytes: 0 },
      checks,
    };
  }

  const asOf = parseDate(dataAsOf);
  if (!asOf) throw new Error(`invalid --as-of date ${dataAsOf}; expected YYYY-MM-DD`);
  const goats = readJSON(source, "goats.json");
  const vaccination = readJSON(source, "vaccination.json");
  const attendance = readJSON(source, "attendance-jun-26.json");
  const timetable = readJSON(source, "timetable-goats-team-v1.json");
  const roster = parseCSV(fs.readFileSync(path.join(source, "roster-name-mapping.jun26-review.csv"), "utf8"));
  const managers = parseCSV(fs.readFileSync(path.join(source, "shed-manager-mapping.jul11-vaccination.csv"), "utf8"));
  const totalBytes = INPUT_FILES.reduce((sum, name) => sum + fs.statSync(path.join(source, name)).size, 0);

  const goatColumns = headerMap(goats[0] ?? []);
  const vaccColumns = headerMap(vaccination[0] ?? []);
  const attendanceColumns = headerMap(attendance[0] ?? []);
  const rosterColumns = headerMap(roster[0] ?? []);
  const managerColumns = headerMap(managers[0] ?? []);
  const schemaProblems = [];
  const goatHeader = (goats[0] ?? []).map((value) => String(value).trim());
  if (!arraysEqual(goatHeader, GOAT_HEADERS) && !arraysEqual(goatHeader, [...GOAT_HEADERS, "species"])) schemaProblems.push("goats.json header/order");
  if (!arraysEqual(vaccination[0] ?? [], VACCINATION_HEADER)) schemaProblems.push("vaccination.json vaccine header/order");
  if (!arraysEqual(vaccination[1] ?? [], VACCINATION_DOSE_HEADER)) schemaProblems.push("vaccination.json dose header/order");
  const attendanceHeader = (attendance[0] ?? []).map((value) => String(value).trim());
  const attendanceAllowed = new Set([...SAFE_ATTENDANCE_HEADERS, ...RAW_ATTENDANCE_EXTRA_HEADERS]);
  if (attendanceHeader.some((name) => !attendanceAllowed.has(name)) || [...SAFE_ATTENDANCE_HEADERS].some((name) => !attendanceHeader.includes(name)) || new Set(attendanceHeader).size !== attendanceHeader.length) schemaProblems.push("attendance-jun-26.json header/order/unknown column");
  if (!arraysEqual(roster[0] ?? [], ROSTER_HEADER)) schemaProblems.push("roster-name-mapping.jun26-review.csv header/order");
  if (!arraysEqual(managers[0] ?? [], MANAGER_HEADER)) schemaProblems.push("shed-manager-mapping.jul11-vaccination.csv header/order");
  if (!arraysEqual(timetable[0] ?? [], TIMETABLE_HEADER)) schemaProblems.push("timetable-goats-team-v1.json header/order");
  checks.push(makeCheck("source_schema", schemaProblems.length, "Every consumed header, vaccine/dose column, and column order must match the reviewed source schema.", "Stop and update the policy, validator, transform, failing tests, and docs together; never guess what a moved/new column means.", schemaProblems));

  const goatByKey = new Map();
  const goatAliases = new Map();
  const goatIdentityProblems = [];
  const shedCounts = new Map();
  for (const [offset, row] of goats.slice(1).entries()) {
    const rowNumber = offset + 2;
    const key = sourceAnimalKey(row, goatColumns);
    if (!key || goatByKey.has(key)) pushSample(goatIdentityProblems, `goats row ${rowNumber}`);
    else goatByKey.set(key, { row, rowNumber, key });
    for (const name of ["rfid", "goat_id", "farm_goat_id", "mapped_farm_goat_id", "old_id"]) {
      const alias = normalized(cell(row, goatColumns, name));
      if (alias) goatAliases.set(alias, key);
    }
    const oldID = cell(row, goatColumns, "old_id");
    const suffix = cell(row, goatColumns, "old_id_suffix");
    if (oldID && !/^(none|na|n\/a)$/i.test(oldID)) goatAliases.set(normalized(suffix && !/^(none|na|n\/a)$/i.test(suffix) ? `${suffix}-${oldID}` : oldID), key);
    const shedKey = `${cell(row, goatColumns, "farm")}\0${cell(row, goatColumns, "shed")}`;
    shedCounts.set(shedKey, (shedCounts.get(shedKey) ?? 0) + 1);
  }
  checks.push(makeCheck("animal_identity_unique", goatIdentityProblems.length, "Animal source identities must be present and unique.", "Fix blank/duplicate RFID or legacy identity keys before transformation.", goatIdentityProblems));

  const vaccByKey = new Map();
  const vaccinationIdentityProblems = [];
  const unknownVaccinationAnimals = [];
  const vaccinationCounts = { dated: 0, pending: 0, na: 0, malformed: 0, future: 0 };
  const speciesEvidence = new Map();
  const doseGapProblems = [];
  const liveGapProblems = [];
  for (const [offset, row] of vaccination.slice(2).entries()) {
    const rowNumber = offset + 3;
    const key = sourceAnimalKey(row, vaccColumns);
    if (!key || vaccByKey.has(key)) pushSample(vaccinationIdentityProblems, `vaccination row ${rowNumber}`);
    else vaccByKey.set(key, { row, rowNumber, key });
    if (!goatByKey.has(key)) pushSample(unknownVaccinationAnimals, `vaccination row ${rowNumber}`);
    const evidence = new Set();
    for (const def of VACCINE_COLUMNS) {
      const value = String(row[def.index] ?? "").trim();
      if (isDatedVaccination(value)) {
        vaccinationCounts.dated += 1;
        const date = parseDate(value);
        if (!date) vaccinationCounts.malformed += 1;
        else if (date > asOf) vaccinationCounts.future += 1;
        if (def.species.length === 1) evidence.add(def.species[0]);
      } else if (/^pending$/i.test(value)) vaccinationCounts.pending += 1;
      else if (/^(na|n\/a|-)?$/i.test(value)) vaccinationCounts.na += 1;
      else vaccinationCounts.malformed += 1;
    }
    for (const [firstIndex, boosterIndex, minimumGap, label] of [[11, 12, 21, "ET+TT"], [14, 15, 28, "Blue tongue"]]) {
      const first = parseDate(row[firstIndex]);
      const booster = parseDate(row[boosterIndex]);
      if (first && booster && dayDiff(first, booster) < minimumGap) pushSample(doseGapProblems, `vaccination row ${rowNumber}:${label}`);
    }
    const ppr = parseDate(row[13]);
    const pox = parseDate(row[19]) ?? parseDate(row[20]);
    if (ppr && pox && Math.abs(dayDiff(ppr, pox)) < 28) pushSample(liveGapProblems, `vaccination row ${rowNumber}:PPR/pox`);
    speciesEvidence.set(key, evidence);
  }
  const coverageDifference = Math.abs(goatByKey.size - vaccByKey.size) + unknownVaccinationAnimals.length;
  checks.push(makeCheck("vaccination_identity_unique", vaccinationIdentityProblems.length, "Vaccination rows must have unique animal identities.", "Deduplicate or restore the identity columns; never merge histories heuristically.", vaccinationIdentityProblems));
  checks.push(makeCheck("animal_vaccination_bijection", coverageDifference, "Animal and vaccination sources must have exact one-to-one coverage.", "Resolve missing/unknown animals before transformation.", unknownVaccinationAnimals));
  checks.push(makeCheck("vaccination_value_format", vaccinationCounts.malformed, "Vaccination cells may contain only YYYY-MM-DD, Pending, NA/N/A, '-', or blank.", "Correct malformed cells without replacing a real administered date.", []));
  checks.push(makeCheck("future_vaccination_dates", vaccinationCounts.future, "A source vaccination date after the reviewed business date needs explicit approval.", "Correct the source meaning or move it through a reviewed future-intent path; never mark it completed automatically.", []));
  checks.push(makeCheck("dose_order_and_minimum_gap", doseGapProblems.length, "ET+TT and Blue Tongue boosters must follow dose 1 by their published minimum gap.", "Stop for clinical/source clarification; never move an administered date automatically.", doseGapProblems));
  checks.push(makeCheck("live_vaccine_gap", liveGapProblems.length, "PPR and species-appropriate pox administrations must be at least 28 days apart.", "Stop for clinical/source clarification; never move an administered date automatically.", liveGapProblems));

  const speciesConflicts = [];
  const speciesOverrides = [];
  const unreviewedBreeds = [];
  for (const goat of goatByKey.values()) {
    if (!deriveSpecies(cell(goat.row, goatColumns, "breed"))) pushSample(unreviewedBreeds, `goats row ${goat.rowNumber}`);
  }
  for (const [key, evidence] of speciesEvidence) {
    const goat = goatByKey.get(key);
    if (!goat) continue;
    if (evidence.size > 1) pushSample(speciesConflicts, `vaccination row ${vaccByKey.get(key)?.rowNumber}`);
    if (evidence.size === 1 && !evidence.has(deriveSpecies(cell(goat.row, goatColumns, "breed")))) {
      pushSample(speciesOverrides, `goats row ${goat.rowNumber} / vaccination row ${vaccByKey.get(key)?.rowNumber}`);
    }
  }
  checks.push(makeCheck("mutually_exclusive_species_vaccines", speciesConflicts.length, "One animal cannot carry both goat-only and sheep-only vaccination facts.", "Stop for human clarification; metadata repair cannot choose between contradictory vaccine histories.", speciesConflicts));
  checks.push(makeCheck("reviewed_breed_vocabulary", unreviewedBreeds.length, "Every breed must have an explicit reviewed goat/sheep mapping.", "Add the reviewed breed to contracts/vaccination-seed-source-policy.json; never default an unknown breed to goat.", unreviewedBreeds));
  checks.push(makeCheck("species_metadata_repair", speciesOverrides.length, "Species-specific vaccination truth disagrees with breed/species metadata.", "Preserve the vaccination date and repair breed/species (plain Anantapur=goat; Anantapur Sheep=sheep).", speciesOverrides));

  const invalidDOB = [];
  const missingDOB = [];
  const dobEntry = [];
  const stageAge = [];
  const eventBeforeDOB = [];
  const vaccinationBeforeDOB = [];
  const vaccinationBeforeMinimumAge = [];
  const terminalBeforeDOB = [];
  const vaccinationAfterTerminal = [];
  const lifecycleStatusProblems = [];
  const motherProblems = [];
  const metadataMismatch = [];
  for (const goat of goatByKey.values()) {
    const { row, rowNumber, key } = goat;
    const dobText = cell(row, goatColumns, "dob");
    const dob = parseDate(dobText);
    if (dobText && !dob) pushSample(invalidDOB, `goats row ${rowNumber}`);
    if (!dobText) pushSample(missingDOB, `goats row ${rowNumber}`);
    const origin = normalized(cell(row, goatColumns, "origin_type"));
    const entry = parseDate(origin === "birth" ? cell(row, goatColumns, "stage_entry_date") : (cell(row, goatColumns, "purchase_date") || cell(row, goatColumns, "stage_entry_date")));
    if (dob && entry && (dob > entry || (origin !== "birth" && dob.valueOf() === entry.valueOf()))) pushSample(dobEntry, `goats row ${rowNumber}`);
    if (dob) {
      const kid = Math.floor(dayDiff(dob, asOf) / 7) <= SEED_SOURCE_POLICY.kid_finish_cutoff_completed_weeks;
      const stageKid = /^(k|kid)/i.test(cell(row, goatColumns, "stage") || cell(row, goatColumns, "age"));
      if (kid !== stageKid) pushSample(stageAge, `goats row ${rowNumber}`);
      for (const name of EVENT_FIELDS) {
        const event = parseDate(cell(row, goatColumns, name));
        if (event && event < dob) pushSample(eventBeforeDOB, `goats row ${rowNumber}:${name}`);
      }
    }
    const death = parseDate(cell(row, goatColumns, "death_date"));
    const sale = parseDate(cell(row, goatColumns, "sale_date"));
    const terminal = earliestDate(cell(row, goatColumns, "death_date"), cell(row, goatColumns, "sale_date"));
    const status = normalized(cell(row, goatColumns, "animal_status") || cell(row, goatColumns, "status"));
    if ((death && sale) || (terminal && status === "alive") || (!terminal && /dead|sold|sale/.test(status))) pushSample(lifecycleStatusProblems, `goats row ${rowNumber}`);
    if (dob && terminal && terminal < dob) pushSample(terminalBeforeDOB, `goats row ${rowNumber}`);
    const vacc = vaccByKey.get(key);
    if (vacc) {
      for (const def of VACCINE_COLUMNS) {
        const date = parseDate(vacc.row[def.index]);
        if (!date) continue;
        if (dob && date < dob) pushSample(vaccinationBeforeDOB, `vaccination row ${vacc.rowNumber}:${def.vaccine} ${def.dose}`);
        if (dob && dayDiff(dob, date) < def.minimumAgeDays) pushSample(vaccinationBeforeMinimumAge, `vaccination row ${vacc.rowNumber}:${def.vaccine} ${def.dose}`);
        if (terminal && date > terminal) pushSample(vaccinationAfterTerminal, `vaccination row ${vacc.rowNumber}:${def.vaccine} ${def.dose}`);
      }
      for (const [goatField, vaccField] of [["farm", "Farm"], ["age", "Age"], ["gender", "Gender"], ["breed", "Breed"], ["shed", "Shed"], ["partition", "Partition"]]) {
        if (normalized(cell(row, goatColumns, goatField)) !== normalized(cell(vacc.row, vaccColumns, vaccField))) {
          pushSample(metadataMismatch, `goats row ${rowNumber} / vaccination row ${vacc.rowNumber}:${vaccField}`);
        }
      }
    }
    const mother = cell(row, goatColumns, "mother_id");
    if (mother) {
      const motherKey = goatAliases.get(normalized(mother));
      const motherRow = motherKey ? goatByKey.get(motherKey) : null;
      const motherDOB = motherRow ? parseDate(cell(motherRow.row, goatColumns, "dob")) : null;
      if (!/^\d{15}$/.test(mother) || !motherRow || motherKey === key || !/female|doe|ewe/i.test(cell(motherRow?.row ?? [], goatColumns, "gender")) || (dob && motherDOB && motherDOB >= dob) || (motherRow && deriveSpecies(cell(motherRow.row, goatColumns, "breed")) !== deriveSpecies(cell(row, goatColumns, "breed"))) || (motherRow && goatColumns.has("mother_breed") && normalized(cell(row, goatColumns, "mother_breed")) !== normalized(cell(motherRow.row, goatColumns, "breed")))) {
        pushSample(motherProblems, `goats row ${rowNumber}`);
      }
    }
  }
  checks.push(makeCheck("invalid_dob", invalidDOB.length, "DOB values must be blank or valid YYYY-MM-DD dates.", "Correct or blank the invalid DOB; never guess from free text.", invalidDOB));
  checks.push(makeCheck("missing_dob", missingDOB.length, "Missing DOB is explicit unknown data, not permission to invent a birthday.", "Keep it null and retain a reviewed kid/adult fallback; runtime uses accepted history/entry/adult catch-up.", missingDOB, "warning"));
  checks.push(makeCheck("dob_entry_chronology", dobEntry.length, "DOB must be before purchase/arrival for procured animals and not after own entry for births.", "Move the mock DOB earlier according to reviewed kid/adult truth, or null it; never move vaccination dates.", dobEntry));
  checks.push(makeCheck("stage_age_consistency", stageAge.length, "K* is valid only through 20 weeks as of the reviewed business date.", "Derive K1/Adult and Kid/Adult from trusted DOB; age-out stale K tags.", stageAge));
  checks.push(makeCheck("event_before_dob", eventBeforeDOB.length, "Delivery, abortion, health, weight, or lifecycle history cannot predate DOB.", "Move the mock DOB earlier or null it; preserve trusted event dates.", eventBeforeDOB));
  checks.push(makeCheck("vaccination_before_dob", vaccinationBeforeDOB.length, "Vaccination history cannot predate DOB.", "Preserve vaccination dates and move the mock DOB earlier or null it.", vaccinationBeforeDOB));
  checks.push(makeCheck("vaccination_before_minimum_age", vaccinationBeforeMinimumAge.length, "Vaccination history must satisfy the published minimum age for that dose.", "Preserve vaccination dates and repair mock DOB/stage metadata; update config+validator together if the medical rule changes.", vaccinationBeforeMinimumAge));
  checks.push(makeCheck("terminal_before_dob", terminalBeforeDOB.length, "Death/sale metadata cannot predate DOB.", "Remove the contradictory mock terminal fact and restore Alive; never alter vaccination history.", terminalBeforeDOB));
  checks.push(makeCheck("vaccination_after_terminal", vaccinationAfterTerminal.length, "Vaccination cannot occur after death or sale.", "Preserve vaccination dates; remove contradictory mock death/sale metadata and restore Alive.", vaccinationAfterTerminal));
  checks.push(makeCheck("lifecycle_status_consistency", lifecycleStatusProblems.length, "Lifecycle status and death/sale facts must describe one consistent terminal state.", "Remove contradictory mock terminal facts and restore Alive, or stop for source clarification when both death and sale are asserted.", lifecycleStatusProblems));
  checks.push(makeCheck("maternal_relation_integrity", motherProblems.length, "Retained mother references must resolve to an older female animal.", "Keep only reviewed full-RFID mother links; blank ambiguous legacy references in the sanitized mock fixture.", motherProblems));
  checks.push(makeCheck("vaccination_metadata_matches_animal", metadataMismatch.length, "Duplicated vaccination row metadata must match canonical animal metadata.", "Synchronize non-vaccine metadata from the corrected animal row; vaccination cells remain byte-for-byte unchanged.", metadataMismatch));

  const sensitiveHeaders = [
    ["goats.json", goats[0]], ["vaccination.json", vaccination[0]], ["attendance-jun-26.json", attendance[0]],
    ["timetable-goats-team-v1.json", timetable[0]], ["roster-name-mapping.jun26-review.csv", roster[0]],
    ["shed-manager-mapping.jul11-vaccination.csv", managers[0]],
  ].flatMap(([file, headers]) => (headers ?? []).map((value) => String(value).trim()).filter((value) => SENSITIVE_HEADER.test(value)).map((value) => `${file}:${value}`));
  checks.push(makeCheck("hrms_sensitive_columns", sensitiveHeaders.length, "Source contains payroll, bank, contact, or identity fields outside Goat OS HRMS scope.", "Strip these columns before committing; the output schema is allowlisted.", sensitiveHeaders));

  const nonSyntheticNames = [];
  for (let index = 1; index < attendance.length; index += 1) {
    const name = cell(attendance[index], attendanceColumns, "Name");
    if (name && !/^Fixture Staff \d{3}$/.test(name)) pushSample(nonSyntheticNames, `attendance row ${index + 1}:Name`);
  }
  for (let index = 1; index < roster.length; index += 1) {
    for (const field of ["timetable_name", "jun26_candidate"]) {
      const value = cell(roster[index], rosterColumns, field);
      if (value && !value.startsWith("Fixture ")) pushSample(nonSyntheticNames, `roster row ${index + 1}:${field}`);
    }
  }
  for (let index = 1; index < managers.length; index += 1) {
    for (const field of ["manager_name", "backup_manager_name"]) {
      const value = cell(managers[index], managerColumns, field);
      if (value && !value.startsWith("Fixture ")) pushSample(nonSyntheticNames, `shed-manager row ${index + 1}:${field}`);
    }
  }
  for (let index = 1; index < timetable.length; index += 1) {
    for (const column of [2, 3, 5]) {
      const value = String(timetable[index]?.[column] ?? "").trim();
      if (value && value !== "--" && !value.startsWith("Fixture ")) pushSample(nonSyntheticNames, `timetable row ${index + 1}:column ${column + 1}`);
    }
  }
  checks.push(makeCheck("hrms_synthetic_identity", nonSyntheticNames.length, "Committed seed data must not contain real staff names.", "Replace every staff display name with a deterministic Fixture identity and keep only the reviewed role/center relationship.", nonSyntheticNames));

  const sensitiveValues = [];
  for (const [file, rows] of [["attendance", attendance], ["timetable", timetable], ["roster", roster], ["shed-manager", managers]]) {
    for (let rowIndex = 1; rowIndex < rows.length; rowIndex += 1) for (let column = 0; column < rows[rowIndex].length; column += 1) {
      const value = String(rows[rowIndex][column] ?? "").trim();
      if (value && ((SENSITIVE_VALUE.test(value) && !/^HRMS-(?:JUN26|MANUAL)-\d+$/.test(value)) || PAYMENT_VALUE.test(value))) pushSample(sensitiveValues, `${file} row ${rowIndex + 1}:column ${column + 1}`);
    }
  }
  checks.push(makeCheck("hrms_sensitive_values", sensitiveValues.length, "HRMS files contain contact/account-like values.", "Remove the values; do not merely rename their columns.", sensitiveValues));
  const staffNames = new Set();
  const staffCodes = new Set();
  const staffCodeByName = new Map();
  const rosterSeatByCode = new Map();
  for (let index = 1; index < attendance.length; index += 1) {
    const name = cell(attendance[index], attendanceColumns, "Name");
    if (name) staffNames.add(name);
    if (name) staffCodeByName.set(name, `HRMS-JUN26-${String(index + 1).padStart(3, "0")}`);
  }
  const rosterProblems = [];
  const rosterDuplicates = [];
  const rosterSeats = new Set();
  for (let index = 1; index < roster.length; index += 1) {
    const row = roster[index];
    const center = cell(row, rosterColumns, "center");
    const position = cell(row, rosterColumns, "timetable_position").replace(/\s+/g, " ");
    const confidence = cell(row, rosterColumns, "confidence");
    const candidate = cell(row, rosterColumns, "jun26_candidate");
    const key = `${center}\0${position}`;
    if (rosterSeats.has(key)) pushSample(rosterDuplicates, `roster row ${index + 1}`);
    rosterSeats.add(key);
    if (confidence === "UNRESOLVED" || (!staffNames.has(candidate) && confidence !== "MANUAL_SEED")) pushSample(rosterProblems, `roster row ${index + 1}`);
    const code = confidence === "MANUAL_SEED" ? `HRMS-MANUAL-${9000 + index + 1}` : staffCodeByName.get(candidate);
    if (code) {
      staffCodes.add(code);
      rosterSeatByCode.set(code, { center, position, candidate });
    }
  }
  const missingOwnerSeats = [];
  for (const center of ["CBE", "CPT"]) for (const position of ["Preventive Care Manager", "Backup Manager", "Park Head"]) {
    if (!rosterSeats.has(`${center}\0${position}`)) missingOwnerSeats.push(`${center}/${position}`);
  }
  checks.push(makeCheck("hrms_roster_resolved", rosterProblems.length, "Every timetable/owner seat must resolve to a reviewed synthetic workforce member.", "Resolve the mapping; no round-robin or silent default owner is allowed.", rosterProblems));
  checks.push(makeCheck("hrms_roster_unique", rosterDuplicates.length, "A center/position seat must occur exactly once.", "Remove duplicate roster assignments.", rosterDuplicates));
  checks.push(makeCheck("required_vaccination_owners", missingOwnerSeats.length, "CBE and CPT each require Preventive Care Manager, Backup Manager, and Park Head.", "Add explicit reviewed synthetic fixture seats before seeding.", missingOwnerSeats));
  const routeSiteProblems = [];
  if (SEED_SOURCE_POLICY.protocol_schedule_policy?.route_site !== "subcutaneous" ||
    SEED_SOURCE_POLICY.protocol_schedule_policy?.route_site_is_not_operator_form_field !== true) {
    routeSiteProblems.push("vaccination matrix route_site must be subcutaneous protocol metadata, not an SOP/operator form field");
  }
  checks.push(makeCheck("protocol_route_site_contract", routeSiteProblems.length, "Published vaccination matrix schedule rows require route_site metadata.", "Set route_site to subcutaneous in the protocol schedule contract and keep it out of vaccination SOP form fields.", routeSiteProblems));

  const managerProblems = [];
  const managerSheds = new Set();
  let mappedAnimals = 0;
  for (let index = 1; index < managers.length; index += 1) {
    const row = managers[index];
    const park = cell(row, managerColumns, "park_code");
    const shed = cell(row, managerColumns, "shed_name");
    const key = `${park}\0${shed}`;
    const count = Number(cell(row, managerColumns, "goat_count"));
    const manager = cell(row, managerColumns, "manager_code");
    const backup = cell(row, managerColumns, "backup_manager_code");
    const managerSeat = rosterSeatByCode.get(manager);
    const backupSeat = rosterSeatByCode.get(backup);
    if (managerSheds.has(key) || !shedCounts.has(key) || shedCounts.get(key) !== count || !staffCodes.has(manager) || !staffCodes.has(backup) || manager === backup || managerSeat?.center !== park || managerSeat?.position !== "Preventive Care Manager" || backupSeat?.center !== park || backupSeat?.position !== "Backup Manager" || cell(row, managerColumns, "manager_name") !== managerSeat?.candidate || cell(row, managerColumns, "backup_manager_name") !== backupSeat?.candidate || normalized(cell(row, managerColumns, "needs_review")) !== "false") {
      pushSample(managerProblems, `shed-manager row ${index + 1}`);
    }
    managerSheds.add(key);
    if (Number.isFinite(count)) mappedAnimals += count;
  }
  const ownerCoverageGap = managerProblems.length + Math.abs(managerSheds.size - shedCounts.size) + Math.abs(mappedAnimals - goatByKey.size);
  checks.push(makeCheck("shed_owner_coverage", ownerCoverageGap, "Every source shed and animal must have one reviewed manager plus one reviewed backup.", "Resolve codes/counts/review flags and cover every shed before the DB transaction starts.", managerProblems));

  const errorCount = checks.filter((check) => check.status === "fail").reduce((sum, check) => sum + check.count, 0);
  const warningCount = checks.filter((check) => check.status === "warning").reduce((sum, check) => sum + check.count, 0);
  return {
    schema_version: 1,
    data_as_of: dataAsOf,
    valid_for_direct_seed: errorCount === 0,
    summary: {
      files: INPUT_FILES.length,
      bytes: totalBytes,
      animals: goatByKey.size,
      vaccination_rows: vaccByKey.size,
      sheds: shedCounts.size,
      vaccination_cells: vaccinationCounts,
      errors: errorCount,
      warnings: warningCount,
    },
    checks,
  };
}

function printHuman(report) {
  console.log(`Vaccination + HRMS source preflight (business date ${report.data_as_of})`);
  console.log(`Files ${report.summary.files}/6; bytes ${report.summary.bytes}; animals ${report.summary.animals ?? 0}; vaccination rows ${report.summary.vaccination_rows ?? 0}; sheds ${report.summary.sheds ?? 0}`);
  if (report.summary.vaccination_cells) {
    const c = report.summary.vaccination_cells;
    console.log(`Vaccination cells: ${c.dated} dated, ${c.pending} Pending, ${c.na} NA/blank, ${c.malformed} malformed, ${c.future} future`);
  }
  for (const check of report.checks) {
    const marker = check.status === "pass" ? "PASS" : check.status === "warning" ? "WARN" : "FAIL";
    console.log(`\n[${marker}] ${check.id}: ${check.count}`);
    console.log(`  ${check.message}`);
    if (check.count) console.log(`  Required treatment: ${check.action}`);
    if (check.samples.length) console.log(`  Samples: ${check.samples.join(", ")}`);
  }
  console.log(`\nResult: ${report.valid_for_direct_seed ? "VALID for direct seed" : "BLOCKED — transform/correct and re-run before any DB write"}; errors=${report.summary.errors ?? 0}, warnings=${report.summary.warnings ?? 0}`);
}

function usage() {
  console.error("usage: validate-vaccination-hrms-source.mjs --source <dir> [--as-of YYYY-MM-DD] [--json] [--strict]");
  process.exit(2);
}

function main() {
  const args = process.argv.slice(2);
  const sourceIndex = args.indexOf("--source");
  if (sourceIndex < 0 || !args[sourceIndex + 1]) usage();
  const asOfIndex = args.indexOf("--as-of");
  const report = auditSourceDirectory(args[sourceIndex + 1], { dataAsOf: asOfIndex >= 0 ? args[asOfIndex + 1] : "2026-07-20" });
  if (args.includes("--json")) console.log(JSON.stringify(report, null, 2));
  else printHuman(report);
  if (args.includes("--strict") && !report.valid_for_direct_seed) process.exit(1);
}

if (path.resolve(process.argv[1] ?? "") === fileURLToPath(import.meta.url)) main();

// Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations
// 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination
// HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no
// fixture/source-data change is required. See fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews.
