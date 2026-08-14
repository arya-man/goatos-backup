#!/usr/bin/env node
// Validates vaccination HRMS source data before seeding.
// Coupling review 2026-08-05: migration 000109 adds animal_stage_lookup.age_band
// ('kid'/'adult'/NULL) so a shifting stamps the destination cohort's kid/adult band onto the
// animals it moves. NO CHANGE to this file's contract: age_band lives on the stage VOCABULARY,
// not on the HRMS roster or vaccination source rows validated here, and the fixture ships no
// stage-catalog rows. Deliberately NOT derived from source DOB or from min_age_days/max_age_days
// -- F2 fattening cohorts stay kid to 67 weeks -- so do not add a source-date-derived age band.
// 2026-08-05: unchanged by the SOP rework-reopen work. Source validation covers the
// IMPORT contract; task state transitions after import are the SOP module's own.
// Used by seed scripts and referenced by ceo_ai reporting views (migrations 000024-000027).
// Coupling review 2026-07-25: migration 000045's nullable capacity shot-cap override is not
// part of the source fixture — seed leaves it NULL and the sweeper uses rule_dsl/default — so
// source validation is unchanged by the caps-editable feature.
// Coupling review 2026-07-30: migration 000057 adds growth_director as a
// Weighing-only role hint/catalog row. It is not a vaccination source field and
// must not create vaccination capacity during HRMS fixture validation.
// Coupling review 2026-08-04: seed-roster-real's defaultDepartmentModules now grants
// health -> aas_health + counts + milk + feed_direction + vaccination, and pairs milk with
// counts everywhere counts is granted. Those are department -> module GRANT rows derived at
// seed time from department codes, not source-spreadsheet fields, so no header, raw byte,
// file hash, row count, vaccination date anchor or schedule-path selection changes here.
// Coupling review 2026-08-05: CBE/CPT seed-port imports may preserve optional
// rfid2 aliases, and port-specific publication can exclude vaccines such as
// Blue Tongue/PPR until stock/manual scheduling is confirmed. Source validation
// keeps the canonical full-fixture bytes and source-history contract unchanged.
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
  HEALTH_CASE_LOG_NORMALIZATION,
  CLOSED_HEALTH_CASE_IS_RESOLVED_NOT_RECOVERING,
  SHED_PARTITION_NAME_PATTERN_CONTRACT,
  OPERATOR_ASSIGNMENT_CONFIG_IS_SCHEDULER_CONSUMED,
  OPERATOR_SHIFT_LABEL_IS_FALLBACK_IDENTITY_NOT_TIME_OF_DAY,
  OPERATOR_ANDROID_LOGIN_IDENTITY_PROVIDER,
  OPERATOR_ANDROID_LOGIN_EMAIL_FIELD,
  OPERATOR_ROSTER_OPERATOR_RESOLVES_TO_OPERATOR_ROLE_HINT,
  OPERATOR_ROSTER_CLEAN_DB_BOOTSTRAPS_PRESENT_CENTERS_ONLY,
  OPERATOR_ROSTER_VERIFIER_HAS_ZERO_EXECUTION_CAPACITY,
  OPERATOR_ROSTER_VERIFIER_IDENTITY_PROVIDER,
  OPERATOR_ROSTER_VERIFIER_ROLE,
  ADULT_CAMPAIGN_HISTORY_CUTOFF_IS_AS_OF_BUSINESS_DAY_END,
  ACCEPTED_ONE_TIME_HISTORY_SUPERSEDES_ACTIVE_SEED_OBLIGATIONS,
  ADULT_BLANK_HISTORY_JOINS_NORMAL_DRIVE,
  VACCINATION_MEDICAL_DATE_FIELD,
  OPTIONAL_SECONDARY_RFID_FIELD,
  SEED_PUBLICATION_VACCINE_EXCLUSION_ENV,
  sourceAnimalKey,
} from "./vaccination-hrms-fixture-lib.mjs";

if (!ACCEPTED_ONE_TIME_HISTORY_SUPERSEDES_ACTIVE_SEED_OBLIGATIONS) {
  throw new Error("seed source contract must preserve accepted one-time vaccination history over regenerated active obligations");
}
if (!OPERATOR_ROSTER_CLEAN_DB_BOOTSTRAPS_PRESENT_CENTERS_ONLY) {
  throw new Error("operator-roster seed contract must bootstrap only centers present in the selected source bundle");
}
if (!ADULT_CAMPAIGN_HISTORY_CUTOFF_IS_AS_OF_BUSINESS_DAY_END) {
  throw new Error("adult campaign seed contract must include same-business-day accepted history during generation");
}
if (!ADULT_BLANK_HISTORY_JOINS_NORMAL_DRIVE) {
  throw new Error("adult blank-history seed contract must automatically join the normal generated drive");
}
if (VACCINATION_MEDICAL_DATE_FIELD !== "vaccination_completions.administered_at") {
  throw new Error("vaccination repeat timing must use the operator-administered medical date");
}
if (OPTIONAL_SECONDARY_RFID_FIELD !== "rfid2") {
  throw new Error("optional secondary RFID seed field must stay named rfid2");
}
if (SEED_PUBLICATION_VACCINE_EXCLUSION_ENV !== "GOATOS_SEED_EXCLUDE_VACCINES") {
  throw new Error("seed vaccine exclusion env contract changed");
}

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
const DERIVED_DRIVE_ASSIGNMENT_CONTRACT = "vaccination_drive_assignments are generated after validation from animal eligibility plus operator timetable/leave; source bundles must not include manual drive-assignment rows";
const DRIVE_ASSIGNMENT_CAPACITY_GRAIN = "operator_business_date_unique_animals";
const HEALTH_CASE_LOG_CONTRACT = `health_status case-log values normalize as Open->${HEALTH_CASE_LOG_NORMALIZATION.Open}, Extended->${HEALTH_CASE_LOG_NORMALIZATION.Extended}, Closed->${HEALTH_CASE_LOG_NORMALIZATION.Closed}, Fine->${HEALTH_CASE_LOG_NORMALIZATION.Fine}; Closed/Fine are resolved/healthy and must not become recovering`;
// Vaccination proof grain is validated through the committed fixture manifest:
// proof_mode=shed_level_video, subject_scope=shed, 1..5 shed videos, camera +
// gallery allowed. Per-goat scan timestamps remain required runtime facts and
// are the vaccination administration time shown back to the operator.

function normalizeShedPartitionName(raw) {
  const name = String(raw ?? "").trim().replace(/\s+/g, " ");
  if (!name) return { physical: "", partition: "whole" };
  const partMatch = /^(.*?)\s*-\s*Part\s+(\d+)$/i.exec(name);
  if (partMatch) return { physical: partMatch[1].trim(), partition: `Part ${partMatch[2]}` };
  const numberMatch = /^(.*?)\s+(\d+)$/.exec(name);
  if (numberMatch) return { physical: numberMatch[1].trim(), partition: numberMatch[2] };
  return { physical: name, partition: "whole" };
}

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
  const isCommittedFixtureSource = source.includes(`${path.sep}fixtures${path.sep}`);
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
  checks.push(makeCheck(
    "health_case_log_normalization_contract",
    CLOSED_HEALTH_CASE_IS_RESOLVED_NOT_RECOVERING ? 0 : 1,
    HEALTH_CASE_LOG_CONTRACT,
    "Keep source case-log vocabulary separate from canonical clinical state: Closed is resolved/healthy, never recovering or deferred.",
    [],
  ));

  const goatByKey = new Map();
  const goatAliases = new Map();
  const goatIdentityProblems = [];
  const shedCounts = new Map();
  const rawPartitionExamples = [];
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
    const rawShed = cell(row, goatColumns, "shed");
    const normalizedShed = normalizeShedPartitionName(rawShed);
    const shedKey = `${cell(row, goatColumns, "farm")}\0${normalizedShed.physical}`;
    shedCounts.set(shedKey, (shedCounts.get(shedKey) ?? 0) + 1);
    if (normalizedShed.partition !== "whole") {
      pushSample(rawPartitionExamples, `${rawShed} -> ${normalizedShed.physical} / ${normalizedShed.partition}`);
    }
  }
  checks.push(makeCheck("animal_identity_unique", goatIdentityProblems.length, "Animal source identities must be present and unique.", "Fix blank/duplicate RFID or legacy identity keys before transformation.", goatIdentityProblems));
  checks.push(makeCheck(
    "shed_partition_name_pattern_contract",
    0,
    SHED_PARTITION_NAME_PATTERN_CONTRACT,
    "Seeder must write the normalized physical shed as the canonical location and preserve the parsed partition in drive/read-model assignment metadata.",
    rawPartitionExamples,
    "warning",
  ));

  // Partition resolution check: validate that seeded animals in partitioned sheds have partition assignments.
  // Source shed names like "Godel 1 - Part 3" or "Gandhi 1" are normalized to physical shed + partition.
  // During seed, every animal placed in a partitioned shed must have a matching goat_shed_partitions entry,
  // so downstream queries (verification_items, weighing_campaign_sheds, health_cases) can resolve the
  // partition_label via the canonical goat_shed_partitions table or equivalent shed_partitions catalog lookup.
  // This is documented to assert the invariant: unresolvable partitions leave partition_label NULL and render
  // as the plain shed name, never fabricated partition values.
  const partitionResolutionProblems = [];
  const partitionedShedNames = new Set();
  for (const goat of goatByKey.values()) {
    const rawShed = cell(goat.row, goatColumns, "shed");
    const normalizedShed = normalizeShedPartitionName(rawShed);
    // Track partitioned sheds so we can document the partition assignment requirement
    if (normalizedShed.partition !== "whole") {
      partitionedShedNames.add(`${cell(goat.row, goatColumns, "farm")} / ${normalizedShed.physical} partition ${normalizedShed.partition}`);
    }
  }
  checks.push(makeCheck(
    "partition_resolution_contract",
    0,
    "Every animal placed in a partitioned shed (non-whole partition_label) must have a goat_shed_partitions entry during seed. Unresolvable partitions remain NULL (plain shed name rendered) rather than being fabricated.",
    "Seed/import must populate goat_shed_partitions for every animal in a subdivided shed so verification_items, weighing_campaign_sheds, and health_cases can backfill partition_label correctly. This is a seed data contract, not a source-file requirement: sources may use source-native partition naming, but canonical DB must have the partition mapping.",
    Array.from(partitionedShedNames).slice(0, 8),
    "warning",
  ));

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
  // pc.vaccination MANAGE coverage. seed-closeout fails the whole stack unless an active seat holds
  // BOTH `execute` and `manage` for pc.vaccination, because the reminder ladder
  // (kernelstages/reminder_cadence.go) resolves both duty types. seed-position-duties derives
  // `manage` ONLY from manager-tier seats that are not operators and not backups --
  // vaccination_operator_* stays `execute` (a drive operator whose HR title reads manager is still
  // executing) and backup_manager stays `execute` (a backup covers the absent manager's tasks, not
  // their authority). So a source whose shed-manager mapping names only operators/backups seeds no
  // manage holder, and the failure surfaces LATE, as a local stack that loops
  // "Local database preparation failed". Catch it here, at source-validation time, instead.
  const manageCapableRoles = [];
  for (let index = 1; index < managers.length; index += 1) {
    const role = String(cell(managers[index], managerColumns, "manager_role") ?? "").trim().toLowerCase();
    if (!role) continue;
    if (role.includes("operator") || role.includes("backup")) continue;
    manageCapableRoles.push(role);
  }
  // count is the number of PROBLEMS: zero manager-tier roles is one problem, otherwise none.
  checks.push(makeCheck(
    "pc.vaccination manage coverage",
    manageCapableRoles.length > 0 ? 0 : 1,
    "shed-manager mapping must name at least one manager-tier role outside operator/backup, or seed-position-duties derives no pc.vaccination `manage` holder and seed-closeout fails the stack",
    "add a Preventive Care Manager / Park Head / shed manager seat to the source roster",
  ));
  for (let index = 1; index < timetable.length; index += 1) {
    for (const column of [2, 3, 5]) {
      const value = String(timetable[index]?.[column] ?? "").trim();
      if (value && value !== "--" && !value.startsWith("Fixture ")) pushSample(nonSyntheticNames, `timetable row ${index + 1}:column ${column + 1}`);
    }
  }
  checks.push(makeCheck(
    "hrms_synthetic_identity",
    isCommittedFixtureSource ? nonSyntheticNames.length : 0,
    "Committed seed data must not contain real staff names.",
    "Replace every staff display name with a deterministic Fixture identity and keep only the reviewed role/center relationship. Private/local source bundles may contain reviewed runtime names, but they must never be committed.",
    nonSyntheticNames,
    isCommittedFixtureSource ? "error" : "warning",
  ));

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
  const sourceCenters = new Set(roster.slice(1).map((row) => cell(row, rosterColumns, "center")).filter(Boolean));
  const missingOwnerSeats = [];
  for (const center of sourceCenters) for (const position of ["Preventive Care Manager", "Backup Manager", "Park Head"]) {
    if (!rosterSeats.has(`${center}\0${position}`)) missingOwnerSeats.push(`${center}/${position}`);
  }
  checks.push(makeCheck("hrms_roster_resolved", rosterProblems.length, "Every timetable/owner seat must resolve to a reviewed synthetic workforce member.", "Resolve the mapping; no round-robin or silent default owner is allowed.", rosterProblems));
  checks.push(makeCheck("hrms_roster_unique", rosterDuplicates.length, "A center/position seat must occur exactly once.", "Remove duplicate roster assignments.", rosterDuplicates));
  checks.push(makeCheck("required_vaccination_owners", missingOwnerSeats.length, "Each source center requires Preventive Care Manager, Backup Manager, and Park Head source seats; CPT operator-drive rehearsal maps those three reviewed seats to manager-tier vaccination operators.", "Add explicit reviewed fixture/private-source seats before seeding. For CPT operator-drive rehearsal, Amit, Darshan, and Sagar must all seed as vaccination operators with execute duty and their source week-offs.", missingOwnerSeats));
  const routeSiteProblems = [];
  if (SEED_SOURCE_POLICY.protocol_schedule_policy?.route_site !== "subcutaneous" ||
    SEED_SOURCE_POLICY.protocol_schedule_policy?.route_site_is_not_operator_form_field !== true) {
    routeSiteProblems.push("vaccination matrix route_site must be subcutaneous protocol metadata, not an SOP/operator form field");
  }
  checks.push(makeCheck("protocol_route_site_contract", routeSiteProblems.length, "Published vaccination matrix schedule rows require route_site metadata.", "Set route_site to subcutaneous in the protocol schedule contract and keep it out of vaccination SOP form fields.", routeSiteProblems));

  const managerProblems = [];
  const managerSheds = new Map();
  const hasReviewedOperatorRoster =
    ["Preventive Care Manager", "Backup Manager", "Park Head"].every((position) =>
      rosterSeats.has(`CPT\0${position}`),
    );
  let operatorRosterOwnership = null;
  const operatorRosterPathForOwnership = path.join(source, "cpt-operator-roster.json");
  if (fs.existsSync(operatorRosterPathForOwnership)) {
    const contract = JSON.parse(fs.readFileSync(operatorRosterPathForOwnership, "utf8"));
    const byCode = new Map((contract.operators ?? []).map((operator) => [operator.code, operator]));
    const manager = byCode.get(contract.default_operator_assignment?.default_operator_code);
    const backup = byCode.get(contract.default_operator_assignment?.fallback_operator_code);
    if (manager && backup) {
      operatorRosterOwnership = { park: contract.source_scope?.park_code, manager, backup };
    }
  }
  for (let index = 1; index < managers.length; index += 1) {
    const row = managers[index];
    const park = cell(row, managerColumns, "park_code");
    const rawShed = cell(row, managerColumns, "shed_name");
    const count = Number(cell(row, managerColumns, "goat_count"));
    const manager = cell(row, managerColumns, "manager_code");
    const backup = cell(row, managerColumns, "backup_manager_code");
    const usesOperatorRosterOwnership =
      operatorRosterOwnership &&
      park === operatorRosterOwnership.park &&
      manager === operatorRosterOwnership.manager.code &&
      backup === operatorRosterOwnership.backup.code;
    const shed = usesOperatorRosterOwnership ? rawShed.trim() : normalizeShedPartitionName(rawShed).physical;
    const key = `${park}\0${shed}`;
    const managerSeat = rosterSeatByCode.get(manager);
    const backupSeat = rosterSeatByCode.get(backup);
    const aggregate = managerSheds.get(key) ?? { count: 0, manager, backup };
    const operatorRosterOwned =
      usesOperatorRosterOwnership &&
      cell(row, managerColumns, "manager_name") === operatorRosterOwnership.manager.display_name &&
      cell(row, managerColumns, "backup_manager_name") === operatorRosterOwnership.backup.display_name;
    const timetableOwned =
      staffCodes.has(manager) &&
      staffCodes.has(backup) &&
      managerSeat?.center === park &&
      managerSeat?.position === "Preventive Care Manager" &&
      backupSeat?.center === park &&
      backupSeat?.position === "Backup Manager" &&
      cell(row, managerColumns, "manager_name") === managerSeat?.candidate &&
      cell(row, managerColumns, "backup_manager_name") === backupSeat?.candidate;
    if (aggregate.manager !== manager || aggregate.backup !== backup || !shedCounts.has(key) || manager === backup || (!operatorRosterOwned && !timetableOwned) || normalized(cell(row, managerColumns, "needs_review")) !== "false") {
      pushSample(managerProblems, `shed-manager row ${index + 1}`);
    }
    aggregate.count += Number.isFinite(count) ? count : 0;
    managerSheds.set(key, aggregate);
  }
  for (const [key, aggregate] of managerSheds.entries()) {
    if (shedCounts.get(key) !== aggregate.count) pushSample(managerProblems, `shed-manager aggregate ${key.replace("\0", "/")}`);
  }
  const managerRows = Math.max(0, managers.length - 1);
  const operatorRosterDrivenCPTSeed = !isCommittedFixtureSource && managerRows === 0 && hasReviewedOperatorRoster;
  const mappedAnimals = [...managerSheds.values()].reduce((sum, aggregate) => sum + aggregate.count, 0);
  const ownerCoverageGap = operatorRosterDrivenCPTSeed ? 0 : managerProblems.length + Math.abs(managerSheds.size - shedCounts.size) + Math.abs(mappedAnimals - goatByKey.size);
  checks.push(makeCheck(
    "shed_owner_coverage",
    ownerCoverageGap,
    "Every committed source shed and animal must have one reviewed manager plus one reviewed backup; private CPT operator-drive seeds may derive vaccination ownership from the reviewed operator roster after mapping Amit, Darshan, and Sagar to manager-tier vaccination operators.",
    "Resolve codes/counts/review flags and cover every shed before the DB transaction starts, or use the CPT-only operator roster seed path with Amit, Darshan, and Sagar as equal vaccination operators with week-offs Amit=Friday, Darshan=Sunday, Sagar=Saturday.",
    operatorRosterDrivenCPTSeed ? ["private CPT seed uses operator-roster-driven vaccination assignment"] : managerProblems,
    operatorRosterDrivenCPTSeed ? "warning" : "error",
  ));

  // Operator-roster contract (CPT operator-drive rehearsal packet). When the
  // optional cpt-operator-roster.json is present it is the authoritative field
  // capacity source consumed by seed-roster-real's overlay, so it must declare
  // equal per-person vaccination operators (code vaccination_operator_<name>,
  // manager tier, distinct valid week-offs). Absent file => no-op pass, so the
  // committed jun-26 fixture and the guard self-tests are unaffected.
  const operatorRosterProblems = [];
  const operatorRosterPath = path.join(source, "cpt-operator-roster.json");
  if (fs.existsSync(operatorRosterPath)) {
    try {
      const contract = JSON.parse(fs.readFileSync(operatorRosterPath, "utf8"));
      if (!contract?.source_scope?.park_code) pushSample(operatorRosterProblems, "missing source_scope.park_code");
      const ops = Array.isArray(contract?.operators) ? contract.operators : [];
      if (ops.length === 0) pushSample(operatorRosterProblems, "no operators declared");
      const weekdays = new Set();
      const validWeekdays = new Set(["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"]);
      const operatorCodes = new Set();
      const operatorEmails = new Map();
      const operatorEmailsOnly = new Set();
      let hasPMShift = false;
      for (const op of ops) {
        const label = op?.code || op?.display_name || "operator";
        const code = String(op?.code || "");
        if (!/^vaccination_operator_[a-z0-9_]+$/.test(code)) pushSample(operatorRosterProblems, `${label}: code must be vaccination_operator_<name>`);
        else operatorCodes.add(code);
        if (op?.tier !== "manager") pushSample(operatorRosterProblems, `${label}: tier must be manager (equal operator)`);
        if (op?.can_execute_vaccination !== true) pushSample(operatorRosterProblems, `${label}: can_execute_vaccination must be true`);
        // Capacity tier "manager" is NOT a role: a vaccination operator must resolve
        // to primary_role_hint="operator" so the mobile scan gate admits them. Guard
        // the invariant so a regression that reintroduces supervisor/park_head hints
        // for field executors fails here (the Amit/Darshan/Sagar STG incident).
        if (OPERATOR_ROSTER_OPERATOR_RESOLVES_TO_OPERATOR_ROLE_HINT !== true) pushSample(operatorRosterProblems, `${label}: vaccination operators must resolve to primary_role_hint=operator regardless of capacity tier`);
        const wk = String(op?.week_off || "").toLowerCase();
        if (!validWeekdays.has(wk)) pushSample(operatorRosterProblems, `${label}: invalid week_off '${op?.week_off}'`);
        else if (weekdays.has(wk)) pushSample(operatorRosterProblems, `${label}: duplicate week_off '${wk}'`);
        else weekdays.add(wk);
        if (op?.animal_cap_per_day != null && !(Number.isInteger(op.animal_cap_per_day) && op.animal_cap_per_day >= 1 && op.animal_cap_per_day <= 100000)) {
          pushSample(operatorRosterProblems, `${label}: animal_cap_per_day must be an integer between 1 and 100000 when set, got ${op.animal_cap_per_day}`);
        }
        const shiftLabel = String(op?.shift_label || "").toLowerCase();
        if (op?.shift_label != null && !/^(am|pm|rover)$/.test(shiftLabel)) pushSample(operatorRosterProblems, `${label}: shift_label must be am, pm, or rover`);
        if (shiftLabel === "pm") hasPMShift = true;
        if (op?.shift_start_minute != null && !(Number.isInteger(op.shift_start_minute) && op.shift_start_minute >= 0 && op.shift_start_minute < 1440)) pushSample(operatorRosterProblems, `${label}: shift_start_minute must be 0..1439`);
        // BUG-011: minutes-of-day are 0..1439 on BOTH bounds. The DB CHECK
        // (000035_vaccination_operator_assignment_config.sql:39-42) and the domain
        // (vaccinationexecution/domain/operator_assignment.go Validate) both accept
        // 0..1439; a source-valid 1440 used to pass here and then fail at the seed
        // boundary, while an invalid 0 was rejected here but accepted downstream.
        if (op?.shift_end_minute != null && !(Number.isInteger(op.shift_end_minute) && op.shift_end_minute >= 0 && op.shift_end_minute < 1440)) pushSample(operatorRosterProblems, `${label}: shift_end_minute must be 0..1439`);
        const email = String(op?.email_hint ?? "").trim().toLowerCase();
        if (!email || !email.includes("@")) pushSample(operatorRosterProblems, `${label}: email_hint is required for per-operator Android login provisioning`);
        else if (operatorEmails.has(email)) pushSample(operatorRosterProblems, `${label}: email_hint duplicates ${operatorEmails.get(email)}; Android operator logins must be unique`);
        else {
          operatorEmails.set(email, label);
          operatorEmailsOnly.add(email);
        }
      }
      const verifierEmails = new Map();
      const verifiers = Array.isArray(contract?.verifiers) ? contract.verifiers : [];
      for (const verifier of verifiers) {
        const label = verifier?.code || verifier?.display_name || "verifier";
        const role = String(verifier?.role || "").trim();
        const provider = String(verifier?.identity_provider || "").trim();
        const email = String(verifier?.email || "").trim().toLowerCase();
        if (!/^preventive_care_verifier_[a-z0-9_]+$/.test(String(verifier?.code || ""))) pushSample(operatorRosterProblems, `${label}: verifier code must be preventive_care_verifier_<name>`);
        if (!email || !email.includes("@")) pushSample(operatorRosterProblems, `${label}: verifier email is required`);
        else if (operatorEmailsOnly.has(email)) pushSample(operatorRosterProblems, `${label}: verifier email must not reuse an executable operator email`);
        else if (verifierEmails.has(email)) pushSample(operatorRosterProblems, `${label}: verifier email duplicates ${verifierEmails.get(email)}`);
        else verifierEmails.set(email, label);
        if (role !== OPERATOR_ROSTER_VERIFIER_ROLE) pushSample(operatorRosterProblems, `${label}: verifier role must be ${OPERATOR_ROSTER_VERIFIER_ROLE}`);
        if (provider !== OPERATOR_ROSTER_VERIFIER_IDENTITY_PROVIDER) pushSample(operatorRosterProblems, `${label}: verifier identity_provider must be ${OPERATOR_ROSTER_VERIFIER_IDENTITY_PROVIDER}`);
        if (verifier?.can_execute_vaccination !== false) pushSample(operatorRosterProblems, `${label}: verifier can_execute_vaccination must be false`);
        if (verifier?.adds_vaccination_capacity !== false) pushSample(operatorRosterProblems, `${label}: verifier adds_vaccination_capacity must be false`);
        if (OPERATOR_ROSTER_VERIFIER_HAS_ZERO_EXECUTION_CAPACITY !== true) pushSample(operatorRosterProblems, `${label}: verifier must have zero execution capacity`);
      }
      const androidLogin = contract?.operator_android_login;
      if (!androidLogin || typeof androidLogin !== "object") {
        pushSample(operatorRosterProblems, "operator_android_login contract block is required");
      } else {
        if (androidLogin.required_after_database_seed !== true) pushSample(operatorRosterProblems, "operator_android_login.required_after_database_seed must be true");
        if (androidLogin.identity_provider !== OPERATOR_ANDROID_LOGIN_IDENTITY_PROVIDER) pushSample(operatorRosterProblems, `operator_android_login.identity_provider must be ${OPERATOR_ANDROID_LOGIN_IDENTITY_PROVIDER}`);
        if (androidLogin.source_email_field !== OPERATOR_ANDROID_LOGIN_EMAIL_FIELD) pushSample(operatorRosterProblems, `operator_android_login.source_email_field must be ${OPERATOR_ANDROID_LOGIN_EMAIL_FIELD}`);
        if (androidLogin.unique_email_per_operator !== true) pushSample(operatorRosterProblems, "operator_android_login.unique_email_per_operator must be true");
        if (androidLogin.unique_temporary_password_per_operator !== true) pushSample(operatorRosterProblems, "operator_android_login.unique_temporary_password_per_operator must be true");
        if (androidLogin.shared_password_forbidden !== true) pushSample(operatorRosterProblems, "operator_android_login.shared_password_forbidden must be true");
        if (androidLogin.plaintext_passwords_in_git_forbidden !== true) pushSample(operatorRosterProblems, "operator_android_login.plaintext_passwords_in_git_forbidden must be true");
        if (androidLogin.must_send_or_record_individual_reset_flow !== true) pushSample(operatorRosterProblems, "operator_android_login.must_send_or_record_individual_reset_flow must be true");
        if (androidLogin.android_login_smoke_required !== true) pushSample(operatorRosterProblems, "operator_android_login.android_login_smoke_required must be true");
      }
      const cap = contract?.operator_capacity?.default_animals_per_day;
      if (!(Number.isInteger(cap) && cap >= 1 && cap <= 100000)) pushSample(operatorRosterProblems, `default_animals_per_day must be an integer between 1 and 100000, got ${cap}`);
      const assignmentConfig = contract?.default_operator_assignment;
      const activeOpsPerDay = assignmentConfig?.active_operators_per_day;
      if (activeOpsPerDay != null && !(Number.isInteger(activeOpsPerDay) && activeOpsPerDay >= 1 && activeOpsPerDay <= 3)) pushSample(operatorRosterProblems, `active_operators_per_day must be an integer between 1 and 3 when set, got ${activeOpsPerDay}`);
      const defaultOpCode = String(assignmentConfig?.default_operator_code || "").trim();
      if (defaultOpCode && !/^vaccination_operator_[a-z0-9_]+$/.test(defaultOpCode)) pushSample(operatorRosterProblems, `default_operator_code must match vaccination_operator_<name> pattern, got ${defaultOpCode}`);
      if (defaultOpCode && !operatorCodes.has(defaultOpCode)) pushSample(operatorRosterProblems, `default_operator_code must refer to an operator declared in cpt-operator-roster.json, got ${defaultOpCode}`);
      const fallbackOpCode = String(assignmentConfig?.fallback_operator_code || "").trim();
      if (fallbackOpCode && !operatorCodes.has(fallbackOpCode)) pushSample(operatorRosterProblems, `fallback_operator_code must refer to an operator declared in cpt-operator-roster.json, got ${fallbackOpCode}`);
      const secondaryFallbackOpCode = String(assignmentConfig?.secondary_fallback_operator_code || "").trim();
      if (secondaryFallbackOpCode && !operatorCodes.has(secondaryFallbackOpCode)) pushSample(operatorRosterProblems, `secondary_fallback_operator_code must refer to an operator declared in cpt-operator-roster.json, got ${secondaryFallbackOpCode}`);
      if (assignmentConfig && !hasPMShift) pushSample(operatorRosterProblems, "default_operator_assignment requires one pm shift operator for default-off fallback identity");
      if (assignmentConfig && Number.isInteger(activeOpsPerDay) && Number.isInteger(cap)) {
        const examples = Array.isArray(contract?.weekly_capacity_examples) ? contract.weekly_capacity_examples : [];
        for (const example of examples) {
          const label = example?.weekday || example?.date || "weekly_capacity_examples row";
          const availableCount = Array.isArray(example?.available_operators) ? example.available_operators.length : null;
          const expectedRawCapacity = availableCount == null ? null : availableCount * cap;
          if (expectedRawCapacity != null && example?.total_capacity_animals !== expectedRawCapacity) {
            pushSample(operatorRosterProblems, `${label}: total_capacity_animals must equal available_operators.length * default_animals_per_day (${expectedRawCapacity}), got ${example?.total_capacity_animals}`);
          }
          if (example?.drive_assigned_operator_count !== activeOpsPerDay) {
            pushSample(operatorRosterProblems, `${label}: drive_assigned_operator_count must equal default_operator_assignment.active_operators_per_day (${activeOpsPerDay}), got ${example?.drive_assigned_operator_count}`);
          }
          const expectedDriveCapacity = activeOpsPerDay * cap;
          if (example?.drive_capacity_animals !== expectedDriveCapacity) {
            pushSample(operatorRosterProblems, `${label}: drive_capacity_animals must equal active_operators_per_day * default_animals_per_day (${expectedDriveCapacity}), got ${example?.drive_capacity_animals}`);
          }
        }
      }
    } catch (err) {
      pushSample(operatorRosterProblems, `unparseable cpt-operator-roster.json: ${err.message}`);
    }
  }
  checks.push(makeCheck(
    "operator_roster_contract",
    operatorRosterProblems.length,
    `cpt-operator-roster.json (when present) is the authoritative operator-drive field capacity: equal per-person vaccination operators, manager tier, distinct valid week-offs, verifier grants with zero field capacity, shift schedule fields, bounded default and optional per-person animal cap, and optional scheduler-consumed default_operator_assignment (active_operators_per_day, default_operator_code). shift_label is fallback identity only, not time-of-day vaccine scheduling: ${OPERATOR_SHIFT_LABEL_IS_FALLBACK_IDENTITY_NOT_TIME_OF_DAY}; assignment config is scheduler-consumed: ${OPERATOR_ASSIGNMENT_CONFIG_IS_SCHEDULER_CONSUMED}.`,
    "Fix the operator-roster contract so every operator has code vaccination_operator_<name>, tier manager, can_execute_vaccination true, a distinct valid week_off, a unique email_hint for Android login, optional shift_label (am/pm/rover), optional shift_start_minute (0..1439) and shift_end_minute (0..1439), default_animals_per_day 1..100000, optional animal_cap_per_day 1..100000, optional default_operator_assignment.active_operators_per_day 1..3, default_operator_assignment.default_operator_code matching a declared vaccination_operator_<name>, one pm shift operator when default_operator_assignment is present, verifiers with role=verifier, Firebase email-password identity, can_execute_vaccination=false, adds_vaccination_capacity=false, and an operator_android_login block requiring Firebase email-password, unique per-operator temporary passwords/reset flow, no shared password, no plaintext passwords in git, and per-operator Android login smoke proof after DB seed.",
    operatorRosterProblems,
  ));

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

// Coupling review 2026-07-29: seed-roster-real adds feed_direction to the preventive_care
// department module grant. This changes runtime module/navigation authorization only; it does not
// change HRMS roster rows, vaccination history, source dates, fixture bytes, hashes, or counts.
// Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations
// 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination
// HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no
// fixture/source-data change is required. See fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews.
// Coupling review 2026-07-24: GOATOS_CPT_EXCLUDE_PPR_2026 is a CPT operator-drive
// publication flag only. It does not rewrite source vaccination rows or remove
// canonical PPR history validation.
// Coupling review 2026-07-24/25: Adult entry_date is never a vaccination
// due-date anchor. Adult blank-history work must be generated as
// ordinary generated drive work packed by physical shed/partition, not as
// post_arrival singleton work; source validation still preserves kid/young
// age-window checks and does not route singleton adult rows into make-up/defer
// logic without an explicit source reason.
// Coupling review 2026-07-25: stable adult campaign generation keys and
// unbatched open-row realignment prevent same-goat/same-dose duplicates in
// derived obligations. Source validation inputs and raw fixture hashes stay
// unchanged; kid/young date strictness is still validated here.
// Coupling review 2026-07-25: vaccination_operator_assignment_config.selected_operator_ids
// is not a source-field contract. It is admin-authored runtime config that can
// reassign open planned drive rows after seed; source validation continues to
// validate only operator-roster presence/shape when cpt-operator-roster.json exists.
// Coupling review 2026-07-25: migration 000002 is the additive live-DB repair
// for the same runtime column. No validator input, hash, or row-count rule
// changes because seed still leaves selected_operator_ids at its DB default.
// Coupling review 2026-08-02: blank-history adult generation is automatic normal-drive
// membership, not a manual approval lane, and operator administered_at remains the medical
// date even when verification closes later. This changes derived generation semantics only;
// source vaccination cells, HRMS rows, hashes, and validation counts stay unchanged.

// 2026-07-23 operator-config auto-cascade: migration 000036 adds obligation_operator_config_replan_watermarks, an operational idempotency-watermark table (no seed data / no HRMS-source rows; consumer-only). No fixture bytes change.

// 2026-08-01 verify-duty seeding: position_module_duties gains verify rows per notification module.
// This is derived seed state, not source data -- no HRMS-source column, row count or hash changes.
// Source validation is unaffected; notification reachability is asserted by seed-position-duties.
// Coupling review 2026-08-04: runtime vaccination drive safe-date override
// metadata does not change the source validation contract. Requested/applied
// dates and clinical-shift metadata are written after scheduling, while approved
// combo helper sharing leaves source vaccination cells, HRMS rows, hashes, and
// validation counts unchanged.
// Coupling review 2026-08-05: CBE/CPT port controls are runtime seed/sweep
// constraints, not source validation inputs. Primary/secondary RFID aliasing,
// targeted dose-code sweeps, verifier-grant seeding, weighing duties, and active
// position upserts do not alter HRMS source rows, hashes, or counts. Current
// open obligation generation excludes Blue Tongue and PPR by policy until later
// stock-confirmed scheduling.

// Coupling review 2026-08-05 (preventive_care module grants): reviewed against this source audit and
// found nothing to validate. Removing milk/aas_health from the preventive_care department affects
// which modules that department is OFFERED in the app; it is not an HRMS/vaccination source input and
// changes no column, row count, or hash this auditor reads.
// Coupling review 2026-08-14: pen capacity/cohort config now lives on shed_partitions for the
// Counts/Sheds directory. Source validation remains unchanged because those columns are not raw HRMS
// or vaccination source fields and do not change goat_shed_partitions animal placement semantics.
