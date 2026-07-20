#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import {
  VACCINE_COLUMNS,
  SEED_SOURCE_POLICY,
  SEED_SOURCE_POLICY_SHA256,
  cell,
  dayDiff,
  deriveSpecies,
  earliestDate,
  encodeCSV,
  headerMap,
  isDatedVaccination,
  parseCSV,
  parseDate,
  setCell,
  sourceAnimalKey,
  updateManifestHashes,
  validateFixture,
} from "./vaccination-hrms-fixture-lib.mjs";

function usage() {
  console.error("usage: build-vaccination-hrms-fixture.mjs --source <private-source-dir> --out <fixture-dir>");
  process.exit(2);
}

const args = process.argv.slice(2);
const sourceIndex = args.indexOf("--source");
const outIndex = args.indexOf("--out");
if (sourceIndex < 0 || outIndex < 0 || !args[sourceIndex + 1] || !args[outIndex + 1]) usage();
const source = path.resolve(args[sourceIndex + 1]);
const out = path.resolve(args[outIndex + 1]);
const asOfText = "2026-07-20";
const asOf = parseDate(asOfText);
const kidFinishCutoffCompletedWeeks = SEED_SOURCE_POLICY.kid_finish_cutoff_completed_weeks;
const adultMinimumAgeDays = (kidFinishCutoffCompletedWeeks + 1) * 7;

function readJSON(name) {
  return JSON.parse(fs.readFileSync(path.join(source, name), "utf8"));
}

function writeJSON(name, value) {
  fs.writeFileSync(path.join(out, name), `${JSON.stringify(value, null, 2)}\n`);
}

function pad(value, width = 4) {
  return String(value).padStart(width, "0");
}

function setOptional(row, columns, name, value) {
  if (columns.has(name)) setCell(row, columns, name, value);
}

function normalizedAlias(value) {
  return String(value ?? "").trim().toLowerCase();
}

function oldTag(row, columns) {
  const oldID = cell(row, columns, "old_id");
  const suffix = cell(row, columns, "old_id_suffix");
  if (!oldID || /^(none|na|n\/a)$/i.test(oldID)) return "";
  return !suffix || /^(none|na|n\/a)$/i.test(suffix) ? oldID : `${suffix}-${oldID}`;
}

function minusDays(date, days) {
  return new Date(date.valueOf() - days * 86_400_000);
}

function minimumDate(dates) {
  return dates.filter(Boolean).sort((left, right) => left - right)[0] ?? null;
}

fs.mkdirSync(out, { recursive: true });

const sourceGoats = readJSON("goats.json");
const goatHeader = [...sourceGoats.values[0]];
if (!goatHeader.includes("species")) goatHeader.push("species");
const goatColumns = headerMap(goatHeader);
const rawGoatColumns = headerMap(sourceGoats.values[0]);

const goatInfos = [];
const aliasToRFID = new Map();
const sourceKeyToInfo = new Map();
for (const [index, sourceRow] of sourceGoats.values.slice(1).entries()) {
  const key = sourceAnimalKey(sourceRow, rawGoatColumns);
  if (!key) throw new Error(`goats row ${index + 2}: no source identity`);
  if (sourceKeyToInfo.has(key)) throw new Error(`duplicate source identity ${key}`);
  const rfid = String(990000000000001n + BigInt(index));
  const info = { index, sourceRow, key, rfid };
  goatInfos.push(info);
  sourceKeyToInfo.set(key, info);
  for (const alias of [
    key,
    cell(sourceRow, rawGoatColumns, "rfid"),
    cell(sourceRow, rawGoatColumns, "goat_id"),
    cell(sourceRow, rawGoatColumns, "farm_goat_id"),
    cell(sourceRow, rawGoatColumns, "mapped_farm_goat_id"),
    cell(sourceRow, rawGoatColumns, "old_id"),
    oldTag(sourceRow, rawGoatColumns),
  ]) {
    if (alias) aliasToRFID.set(normalizedAlias(alias), rfid);
  }
}

const sourceVaccination = readJSON("vaccination.json");
const vaccinationColumns = headerMap(sourceVaccination.values[0]);
const vaccinationFactsByKey = new Map();
const vaccineSpeciesByKey = new Map();
for (const [index, row] of sourceVaccination.values.slice(2).entries()) {
  const key = sourceAnimalKey(row, vaccinationColumns);
  if (!sourceKeyToInfo.has(key)) throw new Error(`vaccination row ${index + 3}: source identity ${key} is absent from goats`);
  const facts = [];
  const evidence = new Set();
  for (const def of VACCINE_COLUMNS) {
    const value = String(row[def.index] ?? "").trim();
    if (!isDatedVaccination(value)) continue;
    facts.push({ ...def, value, date: parseDate(value) });
    if (def.species.length === 1) evidence.add(def.species[0]);
  }
  if (evidence.size > 1) {
    throw new Error(`source animal ${key} has mutually exclusive goat-only and sheep-only vaccination facts`);
  }
  vaccinationFactsByKey.set(key, facts);
  if (evidence.size === 1) vaccineSpeciesByKey.set(key, [...evidence][0]);
}

const corrections = {
  counts: {
    stage_age_corrected: 0,
    dob_shifted_earlier: 0,
    dob_shifted_for_adult_truth: 0,
    dob_shifted_for_vaccination_schedule: 0,
    dob_shifted_for_other_event: 0,
    death_dates_removed: 0,
    sale_dates_removed: 0,
    terminal_animals_restored_to_alive: 0,
    species_reclassified_from_vaccination: 0,
    anantapur_renamed_to_anantapur_sheep: 0,
    unresolved_mother_references_blanked: 0,
    retained_mother_metadata_synchronized: 0,
    vaccination_metadata_cells_synchronized: 0,
    source_vaccination_cells_changed: 0,
    unresolved_roster_seats_filled: 0,
    synthetic_park_heads_added: 0,
  },
  policy: {
    vaccination_cells_are_authoritative_and_immutable: true,
    metadata_is_repaired_around_vaccination_truth: true,
    no_new_na_or_pending_corrections: true,
    terminal_dates_conflicting_with_vaccination_or_dob: "remove_terminal_fact_and_restore_alive",
    anantapur_species_rule: "plain_anantapur_goat; anantapur_sheep_sheep; species_specific_vaccination_overrides_and_renames_breed",
    stage_is_derived_from_dob_as_of: asOfText,
  },
};

const transformedGoats = [goatHeader];
const transformedInfo = new Map();
const loadAliases = new Map();
function fixtureLoad(value) {
  const text = String(value ?? "").trim();
  if (!text) return "";
  if (!loadAliases.has(text)) loadAliases.set(text, `FIXTURE-LOAD-${pad(loadAliases.size + 1)}`);
  return loadAliases.get(text);
}

for (const info of goatInfos) {
  const row = [...info.sourceRow];
  while (row.length < goatHeader.length) row.push("");
  const originalDOB = parseDate(cell(row, goatColumns, "dob"));
  const origin = cell(row, goatColumns, "origin_type").toLowerCase();
  const entry = parseDate(origin === "birth" ? cell(row, goatColumns, "stage_entry_date") : (cell(row, goatColumns, "purchase_date") || cell(row, goatColumns, "stage_entry_date")));
  const facts = vaccinationFactsByKey.get(info.key) ?? [];
  const sourceStageKid = /^(k|kid)/i.test(cell(row, goatColumns, "stage") || cell(row, goatColumns, "age"));

  const originalDeath = parseDate(cell(row, goatColumns, "death_date"));
  const originalSale = parseDate(cell(row, goatColumns, "sale_date"));
  const terminalConflicts = (terminal) => terminal && (
    (originalDOB && terminal < originalDOB) || facts.some((fact) => fact.date > terminal)
  );
  if (terminalConflicts(originalDeath)) {
    setOptional(row, goatColumns, "death_date", "");
    setOptional(row, goatColumns, "death_reason", "");
    corrections.counts.death_dates_removed += 1;
  }
  if (terminalConflicts(originalSale)) {
    setOptional(row, goatColumns, "sale_date", "");
    setOptional(row, goatColumns, "sale_reason", "");
    corrections.counts.sale_dates_removed += 1;
  }
  const remainingTerminal = earliestDate(cell(row, goatColumns, "death_date"), cell(row, goatColumns, "sale_date"));
  if (!remainingTerminal && (terminalConflicts(originalDeath) || terminalConflicts(originalSale))) {
    setOptional(row, goatColumns, "status", "Alive");
    setOptional(row, goatColumns, "animal_status", "Alive");
    const lastEvent = cell(row, goatColumns, "last_event").toLowerCase();
    if (lastEvent.includes("death") || lastEvent.includes("sale") || lastEvent.includes("sold")) {
      setOptional(row, goatColumns, "last_event", "");
      setOptional(row, goatColumns, "last_event_date", "");
    }
    corrections.counts.terminal_animals_restored_to_alive += 1;
  }

  const adultOffset = sourceStageKid ? 1 : adultMinimumAgeDays;
  const adultTruthBounds = [];
  if (!sourceStageKid) adultTruthBounds.push(minusDays(asOf, adultMinimumAgeDays));
  if (entry) adultTruthBounds.push(minusDays(entry, adultOffset));
  const vaccinationBounds = facts.map((fact) => minusDays(fact.date, fact.minimumAgeDays));
  const otherEventBounds = [];
  for (const name of ["delivery_date", "abortion_date"]) {
    const date = parseDate(cell(row, goatColumns, name));
    if (date) otherEventBounds.push(minusDays(date, adultMinimumAgeDays));
  }
  for (const name of ["disease_1_date", "disease_2_date", "disease_3_date", "latest_weight_date", "last_event_date"]) {
    const date = parseDate(cell(row, goatColumns, name));
    if (date) otherEventBounds.push(minusDays(date, adultOffset));
  }
  let dob = originalDOB;
  const adultTruthBound = minimumDate(adultTruthBounds);
  const vaccinationBound = minimumDate(vaccinationBounds);
  const otherEventBound = minimumDate(otherEventBounds);
  const requiredDOB = minimumDate([adultTruthBound, vaccinationBound, otherEventBound]);
  if (dob && requiredDOB && dob > requiredDOB) {
    if (adultTruthBound && originalDOB > adultTruthBound) corrections.counts.dob_shifted_for_adult_truth += 1;
    if (vaccinationBound && originalDOB > vaccinationBound) corrections.counts.dob_shifted_for_vaccination_schedule += 1;
    if (otherEventBound && originalDOB > otherEventBound) corrections.counts.dob_shifted_for_other_event += 1;
    dob = requiredDOB;
    corrections.counts.dob_shifted_earlier += 1;
  }
  let kid;
  if (dob) kid = Math.floor(dayDiff(dob, asOf) / 7) <= kidFinishCutoffCompletedWeeks;
  else kid = sourceStageKid;
  if (kid !== sourceStageKid) corrections.counts.stage_age_corrected += 1;

  setCell(row, goatColumns, "rfid", info.rfid);
  setOptional(row, goatColumns, "goat_id", `FIXTURE-GOAT-${pad(info.index + 1)}`);
  setOptional(row, goatColumns, "farm_goat_id", `FIXTURE-FARM-GOAT-${pad(info.index + 1)}`);
  setOptional(row, goatColumns, "old_id", "");
  setOptional(row, goatColumns, "old_id_suffix", "");
  setOptional(row, goatColumns, "mapped_farm_goat_id", `FIXTURE-FARM-GOAT-${pad(info.index + 1)}`);
  setOptional(row, goatColumns, "has_mapping", true);
  // A missing DOB is not permission to invent one. Keep the source kid/adult
  // classification as the explicit fallback that the runtime classifier
  // already understands; only a trusted DOB may age a K* animal into Adult.
  const sourceStage = cell(info.sourceRow, rawGoatColumns, "stage").trim();
  const reviewedKidStage = /^K[12]$/i.test(sourceStage) ? sourceStage.toUpperCase() : "K1";
  setCell(row, goatColumns, "stage", kid ? reviewedKidStage : "Adult");
  setCell(row, goatColumns, "age", kid ? "Kid" : "Adult");
  setCell(row, goatColumns, "dob", dob ? dob.toISOString().slice(0, 10) : "");
  if (goatColumns.has("days_in_stage")) {
    const stageEntry = parseDate(cell(row, goatColumns, "stage_entry_date"));
    setCell(row, goatColumns, "days_in_stage", stageEntry ? String(Math.max(0, dayDiff(stageEntry, asOf))) : "");
  }
  const breedSpecies = deriveSpecies(cell(row, goatColumns, "breed"));
  if (!breedSpecies) throw new Error(`source animal ${info.key}: unreviewed breed ${cell(row, goatColumns, "breed")}`);
  const vaccineSpecies = vaccineSpeciesByKey.get(info.key);
  let species = vaccineSpecies ?? breedSpecies;
  if (vaccineSpecies && vaccineSpecies !== breedSpecies) {
    corrections.counts.species_reclassified_from_vaccination += 1;
    if (cell(row, goatColumns, "breed") === "Anantapur" && vaccineSpecies === "sheep") {
      setCell(row, goatColumns, "breed", "Anantapur Sheep");
      corrections.counts.anantapur_renamed_to_anantapur_sheep += 1;
    } else if (cell(row, goatColumns, "breed") === "Anantapur Sheep" && vaccineSpecies === "goat") {
      setCell(row, goatColumns, "breed", "Anantapur");
    } else {
      throw new Error(`source animal ${info.key}: breed ${cell(row, goatColumns, "breed")} conflicts with authoritative ${vaccineSpecies}-only vaccination`);
    }
    species = deriveSpecies(cell(row, goatColumns, "breed"));
  }
  setCell(row, goatColumns, "species", species);

  if (origin === "birth") {
    for (const name of ["purchase_vendor", "purchase_load_id", "purchase_date", "purchase_weight", "load_id"]) setOptional(row, goatColumns, name, "");
  }
  for (const name of ["purchase_vendor", "mother_vendor"]) setOptional(row, goatColumns, name, cell(row, goatColumns, name) ? "Fixture Vendor" : "");
  for (const name of ["purchase_load_id", "mother_load_id", "load_id"]) setOptional(row, goatColumns, name, fixtureLoad(cell(row, goatColumns, name)));

  transformedGoats.push(row);
  transformedInfo.set(info.key, { ...info, row, species, correctedDOB: dob, terminal: remainingTerminal });
}

const transformedByRFID = new Map([...transformedInfo.values()].map((info) => [info.rfid, info]));
for (const info of transformedInfo.values()) {
  const sourceMother = cell(info.sourceRow, rawGoatColumns, "mother_id");
  if (!sourceMother) continue;
  const fullRFID = /^\d{15}$/.test(sourceMother) ? aliasToRFID.get(normalizedAlias(sourceMother)) : "";
  const mother = fullRFID ? transformedByRFID.get(fullRFID) : null;
  const motherGender = mother ? cell(mother.row, goatColumns, "gender").toLowerCase() : "";
  const older = mother?.correctedDOB && info.correctedDOB && mother.correctedDOB < info.correctedDOB;
  if (mother && mother.rfid !== info.rfid && mother.species === info.species && /female|doe|ewe/.test(motherGender) && older) {
    setCell(info.row, goatColumns, "mother_id", mother.rfid);
    if (goatColumns.has("mother_breed") && cell(info.row, goatColumns, "mother_breed") !== cell(mother.row, goatColumns, "breed")) {
      setCell(info.row, goatColumns, "mother_breed", cell(mother.row, goatColumns, "breed"));
      corrections.counts.retained_mother_metadata_synchronized += 1;
    }
  } else {
    setCell(info.row, goatColumns, "mother_id", "");
    corrections.counts.unresolved_mother_references_blanked += 1;
  }
}

const transformedVaccination = [sourceVaccination.values[0], sourceVaccination.values[1]];
for (const [index, sourceRow] of sourceVaccination.values.slice(2).entries()) {
  const key = sourceAnimalKey(sourceRow, vaccinationColumns);
  const info = sourceKeyToInfo.get(key);
  if (!info) throw new Error(`vaccination row ${index + 3}: source identity ${key} is absent from goats`);
  const transformed = transformedInfo.get(key);
  const row = [...sourceRow];
  setCell(row, vaccinationColumns, "RFID", info.rfid);
  setCell(row, vaccinationColumns, "Old ID", "");
  setCell(row, vaccinationColumns, "Old ID Suffix", "");
  for (const [vaccinationField, value] of [
    ["Farm", cell(transformed.row, goatColumns, "farm")],
    ["Age", cell(transformed.row, goatColumns, "age")],
    ["Gender", cell(transformed.row, goatColumns, "gender")],
    ["Breed", cell(transformed.row, goatColumns, "breed")],
    ["Tag", cell(transformed.row, goatColumns, "shed_tag") || cell(transformed.row, goatColumns, "stage")],
    ["Shed", cell(transformed.row, goatColumns, "shed")],
    ["Partition", cell(transformed.row, goatColumns, "partition")],
  ]) {
    if (vaccinationColumns.has(vaccinationField) && String(row[vaccinationColumns.get(vaccinationField)] ?? "").trim() !== String(value ?? "").trim()) {
      corrections.counts.vaccination_metadata_cells_synchronized += 1;
    }
    setOptional(row, vaccinationColumns, vaccinationField, value);
  }
  for (const def of VACCINE_COLUMNS) {
    if (String(row[def.index] ?? "") !== String(sourceRow[def.index] ?? "")) corrections.counts.source_vaccination_cells_changed += 1;
  }
  transformedVaccination.push(row);
}

const attendance = readJSON("attendance-jun-26.json");
const rawAttendanceColumns = headerMap(attendance.values[0]);
const safeAttendanceHeader = ["Name", "Type", "Designation Type", "Designation", "Location", ...Array.from({ length: 30 }, (_, i) => String(i + 1))];
const safeAttendance = [safeAttendanceHeader];
const staffNameMap = new Map();
for (let index = 1; index < attendance.values.length; index += 1) {
  const sourceRow = attendance.values[index];
  const name = cell(sourceRow, rawAttendanceColumns, "Name");
  const fixtureName = name ? `Fixture Staff ${pad(index + 1, 3)}` : "";
  if (name) staffNameMap.set(name, fixtureName);
  safeAttendance.push([
    fixtureName,
    cell(sourceRow, rawAttendanceColumns, "Type"),
    cell(sourceRow, rawAttendanceColumns, "Designation Type"),
    cell(sourceRow, rawAttendanceColumns, "Designation"),
    cell(sourceRow, rawAttendanceColumns, "Location"),
    ...Array.from({ length: 30 }, (_, day) => cell(sourceRow, rawAttendanceColumns, String(day + 1))),
  ]);
}

const roster = parseCSV(fs.readFileSync(path.join(source, "roster-name-mapping.jun26-review.csv"), "utf8"));
const rosterColumns = headerMap(roster[0]);
const timetableNames = new Map();
for (let index = 1; index < roster.length; index += 1) {
  const row = roster[index];
  const originalCandidate = cell(row, rosterColumns, "jun26_candidate");
  const confidence = cell(row, rosterColumns, "confidence");
  let candidate = staffNameMap.get(originalCandidate) ?? "";
  if (confidence === "UNRESOLVED" || !candidate) {
    candidate = `Fixture Staff Manual ${pad(index + 1, 3)}`;
    setCell(row, rosterColumns, "confidence", "MANUAL_SEED");
    corrections.counts.unresolved_roster_seats_filled += confidence === "UNRESOLVED" ? 1 : 0;
  }
  setCell(row, rosterColumns, "jun26_candidate", candidate);
  const slotName = `Fixture Slot ${pad(index, 3)}`;
  timetableNames.set(`${cell(row, rosterColumns, "center")}\0${cell(row, rosterColumns, "timetable_position").replace(/\s+/g, " ")}`, slotName);
  setCell(row, rosterColumns, "timetable_name", slotName);
  if (rosterColumns.has("notes")) setCell(row, rosterColumns, "notes", "Synthetic sanitized fixture assignment.");
}
for (const center of ["CBE", "CPT"]) {
  const rowNumber = roster.length + 1;
  roster.push([
    center,
    "Park Head",
    `Fixture Park Head ${center}`,
    `Fixture Staff Manual ${pad(rowNumber, 3)}`,
    "Manager",
    "Park Head",
    center,
    "MANUAL_SEED",
    "Synthetic sanitized fixture owner required for escalation coverage.",
  ]);
  corrections.counts.synthetic_park_heads_added += 1;
}

const timetable = readJSON("timetable-goats-team-v1.json");
const safeTimetable = timetable.values.map((sourceRow, index) => {
  const row = [...sourceRow].slice(0, timetable.values[0].length);
  if (index === 0) return row;
  const position = String(row[0] ?? "").trim().replace(/\s+/g, " ");
  if (position) {
    if (row.length > 2 && String(row[2] ?? "").trim() && String(row[2]).trim() !== "--") row[2] = timetableNames.get(`CBE\0${position}`) ?? "Fixture Staff";
    if (row.length > 3 && String(row[3] ?? "").trim() && String(row[3]).trim() !== "--") row[3] = timetableNames.get(`CPT\0${position}`) ?? "Fixture Staff";
  }
  if (row.length > 5 && String(row[5] ?? "").trim() && String(row[5]).trim() !== "--") {
    row[5] = `Fixture Backup ${pad(index, 3)}`;
  }
  return row;
});

const shedManagers = parseCSV(fs.readFileSync(path.join(source, "shed-manager-mapping.jul11-vaccination.csv"), "utf8"));
const shedManagerColumns = headerMap(shedManagers[0]);
const codeToName = new Map();
for (let index = 1; index < safeAttendance.length; index += 1) {
  if (safeAttendance[index][0]) codeToName.set(`HRMS-JUN26-${pad(index + 1, 3)}`, safeAttendance[index][0]);
}
for (let index = 1; index < roster.length; index += 1) {
  const row = roster[index];
  if (cell(row, rosterColumns, "confidence") === "MANUAL_SEED") {
    codeToName.set(`HRMS-MANUAL-${9000 + index + 1}`, cell(row, rosterColumns, "jun26_candidate"));
  }
}
for (const row of shedManagers.slice(1)) {
  setOptional(row, shedManagerColumns, "manager_name", codeToName.get(cell(row, shedManagerColumns, "manager_code")) ?? "Fixture Staff");
  setOptional(row, shedManagerColumns, "backup_manager_name", codeToName.get(cell(row, shedManagerColumns, "backup_manager_code")) ?? "Fixture Staff");
  setOptional(row, shedManagerColumns, "source_ref", "synthetic sanitized fixture roster");
  setOptional(row, shedManagerColumns, "backup_source_ref", "synthetic sanitized fixture roster");
  setOptional(row, shedManagerColumns, "notes", "Synthetic shed ownership fixture; manager and backup resolve through committed roster.");
}

writeJSON("goats.json", { values: transformedGoats });
writeJSON("vaccination.json", { values: transformedVaccination });
writeJSON("attendance-jun-26.json", { values: safeAttendance });
writeJSON("timetable-goats-team-v1.json", { values: safeTimetable });
fs.writeFileSync(path.join(out, "roster-name-mapping.jun26-review.csv"), encodeCSV(roster));
fs.writeFileSync(path.join(out, "shed-manager-mapping.jul11-vaccination.csv"), encodeCSV(shedManagers));
writeJSON("corrections.json", corrections);

const vaccinationCellCounts = { dated: 0, pending: 0, na: 0, malformed: 0 };
for (const row of transformedVaccination.slice(2)) {
  for (const def of VACCINE_COLUMNS) {
    const value = String(row[def.index] ?? "").trim();
    if (isDatedVaccination(value)) vaccinationCellCounts.dated += 1;
    else if (/^pending$/i.test(value)) vaccinationCellCounts.pending += 1;
    else if (/^(na|n\/a|-)?$/i.test(value)) vaccinationCellCounts.na += 1;
    else vaccinationCellCounts.malformed += 1;
  }
}
const speciesCounts = transformedGoats.slice(1).reduce((counts, row) => {
  counts[cell(row, goatColumns, "species")] += 1;
  return counts;
}, { goat: 0, sheep: 0 });
const shedCount = new Set(transformedGoats.slice(1).map((row) => `${cell(row, goatColumns, "farm")}\0${cell(row, goatColumns, "shed")}`)).size;

let manifest = {
  schema_version: 1,
  fixture_kind: "synthetic_sanitized",
  data_as_of: asOfText,
  minimum_migration: "000008",
  generated_by: "tools/dev/build-vaccination-hrms-fixture.mjs",
  contracts: {
    vaccination_protocol_code: "vaccination.matrix",
    vaccination_sop_code: "vaccination.drive",
    video_proof_subject_scope: "goat",
    video_capture_source: "in_app_camera",
    minimum_video_count_per_goat: 1,
    maximum_video_count_per_goat: 5,
    verifier_approval_required: true,
    shed_completion: "acknowledgement_only",
    protocol_route_site: SEED_SOURCE_POLICY.protocol_schedule_policy.route_site,
    protocol_route_site_is_not_operator_form_field: SEED_SOURCE_POLICY.protocol_schedule_policy.route_site_is_not_operator_form_field,
    source_dates_are_never_invented: true,
    source_policy_sha256: SEED_SOURCE_POLICY_SHA256,
  },
  counts: {
    animals: transformedGoats.length - 1,
    species: speciesCounts,
    sheds: shedCount,
    vaccination_cells: vaccinationCellCounts,
    roster_positions: roster.length - 1,
    corrections: corrections.counts,
  },
};
manifest = updateManifestHashes(out, manifest);
writeJSON("manifest.json", manifest);

const { problems } = validateFixture(out);
if (problems.length) throw new Error(`generated fixture failed validation:\n- ${problems.join("\n- ")}`);
console.log(`generated validated fixture at ${out}`);
console.log(JSON.stringify(manifest.counts, null, 2));
