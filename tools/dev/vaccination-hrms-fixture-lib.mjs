// Vaccination HRMS fixture utilities — used by seed scripts and ceo_ai reporting views
// (migrations 000024-000027) to load and validate vaccination source data.
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const REQUIRED_DATA_FILES = [
  "goats.json",
  "vaccination.json",
  "attendance-jun-26.json",
  "timetable-goats-team-v1.json",
  "roster-name-mapping.jun26-review.csv",
  "shed-manager-mapping.jul11-vaccination.csv",
  "corrections.json",
];

export const SEED_SOURCE_POLICY_PATH = fileURLToPath(new URL("../../contracts/vaccination-seed-source-policy.json", import.meta.url));
const seedSourcePolicyBytes = fs.readFileSync(SEED_SOURCE_POLICY_PATH);
export const SEED_SOURCE_POLICY = JSON.parse(seedSourcePolicyBytes.toString("utf8"));
export const SEED_SOURCE_POLICY_SHA256 = crypto.createHash("sha256").update(seedSourcePolicyBytes).digest("hex");

// Vaccination drive assignments are generated planner output, not another source file:
// they must be derived from validated animals/vaccination rows plus timetable-backed
// operator availability so raw HRMS sheets cannot smuggle manual assignment truth.
export const DRIVE_ASSIGNMENTS_ARE_DERIVED_FROM_VALIDATED_SOURCE = true;
export const DRIVE_ASSIGNMENT_CAPACITY_GRAIN = "operator_business_date_unique_animals";
// An operator-drive rehearsal source may ship an authoritative operator-roster
// contract (cpt-operator-roster.json). When present it is the source of truth
// for that park's field capacity: seed-roster-real recasts the resolved seats
// into equal per-person vaccination_operator_<name> positions (manager tier,
// not a backup slot) with contract-owned week-offs, instead of the generic
// jun-26 PC-manager/backup/park-head trio. The generic timetable model still
// governs every other center/source that ships no such contract.
export const OPERATOR_ROSTER_CONTRACT_FILE = "cpt-operator-roster.json";
export const OPERATOR_ROSTER_OVERLAY_IS_AUTHORITATIVE_FIELD_CAPACITY = true;
export const OPERATOR_ROSTER_ANIMAL_CAP_FIELD = "animal_cap_per_day";
export const HRMS_VACCINATION_DAILY_ANIMAL_CAP_FIELD = "workforce_positions.vaccination_daily_animal_cap";
export const OPERATOR_SHIFT_LABEL_FIELD = "shift_label";
export const OPERATOR_SHIFT_START_MINUTE_FIELD = "shift_start_minute";
export const OPERATOR_SHIFT_END_MINUTE_FIELD = "shift_end_minute";
export const OPERATOR_ASSIGNMENT_CONFIG_ACTIVE_OPERATORS_FIELD = "active_operators_per_day";
export const OPERATOR_ASSIGNMENT_CONFIG_DEFAULT_OPERATOR_FIELD = "default_operator_code";
export const OPERATOR_ASSIGNMENT_CONFIG_IS_SCHEDULER_CONSUMED = true;
export const OPERATOR_SHIFT_LABEL_IS_FALLBACK_IDENTITY_NOT_TIME_OF_DAY = true;
// Minutes-of-day are 0..1439 on BOTH bounds, for start AND end. The DB CHECK in
// 000035_vaccination_operator_assignment_config.sql and the domain validator in
// vaccinationexecution/domain/operator_assignment.go both use that range; the
// source validator previously accepted 1..1440 for the end minute, so a
// source-valid 1440 passed preflight and then failed at the seed boundary while
// an invalid 0 was rejected at preflight and accepted downstream (BUG-011).
export const OPERATOR_SHIFT_MINUTE_MIN = 0;
export const OPERATOR_SHIFT_MINUTE_MAX_EXCLUSIVE = 1440;
export const OPERATOR_SHIFT_MINUTES_ARE_SAME_RANGE_AT_SOURCE_DB_AND_DOMAIN = true;
// The operator-roster contract is a loader contract, not just a data file:
// backend/cmd/seed-roster-real decodes cpt-operator-roster.json with
// DisallowUnknownFields, so a declared block with no consuming struct field is a
// hard, named failure instead of an encoding/json silent drop (BUG-024). Any new
// block added to the contract must be consumed by the seeder in the same change.
export const OPERATOR_ROSTER_LOADER_REJECTS_UNKNOWN_BLOCKS = true;
// Blocks the seeder now genuinely consumes (previously parsed and discarded):
// `directors[]` seeds monitoring-only workforce_members with NO workforce_positions
// row — zero vaccination_daily_animal_cap, zero shift config, zero field capacity,
// asserted rather than assumed; `leadership_full_access` seeds tenant-scoped
// auth_pending_email_grants through the same path as seed-dev-email-grants, so a
// park-only rehearsal reseed produces its own CEO/CXO grants.
export const OPERATOR_ROSTER_DIRECTORS_FIELD = "directors";
export const OPERATOR_ROSTER_DIRECTOR_HAS_ZERO_EXECUTION_CAPACITY = true;
export const OPERATOR_ROSTER_LEADERSHIP_FIELD = "leadership_full_access";
export const OPERATOR_ROSTER_LEADERSHIP_GRANT_SCOPE = "tenant";
// A reseed proof is only valid from an origin/main-identical, clean checkout:
// `make seed-checkout-staleness-gate` runs read-only BEFORE any DB mutation in
// both seed targets and fails closed (BUG-023). GOATOS_ALLOW_STALE_SEED_CHECKOUT=1
// is a loud throwaway-experiment escape hatch and voids the proof.
export const SEED_CHECKOUT_STALENESS_GATE_TARGET = "seed-checkout-staleness-gate";
// Documented expected-drive-schedule comparison is now executable: seed-closeout
// runs the packet's check-expected-drive-schedules.mjs against real DB rows when
// GOATOS_EXPECTED_DRIVE_SCHEDULES points at the packet expectation file (BUG-010).
export const EXPECTED_DRIVE_SCHEDULE_PROOF_ENV = "GOATOS_EXPECTED_DRIVE_SCHEDULES";
export const EXPECTED_DRIVE_SCHEDULE_PROOF_IS_EXECUTED_IN_SEED_CLOSEOUT = true;
export const HEALTH_CASE_LOG_NORMALIZATION = Object.freeze({
  Open: "sick",
  Extended: "under_treatment",
  Closed: "healthy",
  Fine: "healthy",
});
export const CLOSED_HEALTH_CASE_IS_RESOLVED_NOT_RECOVERING = true;
export const SHED_PARTITION_NAME_PATTERN_CONTRACT =
  "raw shed labels like Gandhi 1 and Godel 1 - Part 3 are source partition labels; canonical DB locations store the physical shed (Gandhi, Godel 1) and drive/read models carry the partition label separately";
export const ADULT_ETTT_DOSE2_POST_SEED_CONTRACT =
  "accepted et_tt_adult_w1 requires same-goat et_tt_adult_w2 obligation or completion before seed handoff";

function normalizeShedName(raw) {
  const name = String(raw ?? "").trim().replace(/\s+/g, " ");
  if (!name) return { physical: "", partition: "whole" };
  const partMatch = /^(.*?)\s*-\s*Part\s+(\d+)$/i.exec(name);
  if (partMatch) return { physical: partMatch[1].trim(), partition: `Part ${partMatch[2]}` };
  const numberMatch = /^(.*?)\s+(\d+)$/.exec(name);
  if (numberMatch) return { physical: numberMatch[1].trim(), partition: numberMatch[2] };
  return { physical: name, partition: "whole" };
}

export const VACCINE_COLUMNS = SEED_SOURCE_POLICY.source_columns.map((column) => ({
  index: column.index,
  vaccine: column.vaccine,
  dose: column.dose,
  minimumAgeDays: column.minimum_age_days,
  minimumGapFromPreviousDays: column.minimum_gap_from_previous_days ?? 0,
  species: column.species,
}));

const SENSITIVE_ATTENDANCE_HEADERS = [
  "basic salary", "incentive", "doj", "adv", "ptax", "tds", "salary to pay",
  "ifsc code", "bank account number", "status",
];

export function parseCSV(text) {
  const rows = [];
  let row = [];
  let cell = "";
  let quoted = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"' && text[i + 1] === '"') {
        cell += '"';
        i += 1;
      } else if (ch === '"') {
        quoted = false;
      } else {
        cell += ch;
      }
    } else if (ch === '"') {
      quoted = true;
    } else if (ch === ",") {
      row.push(cell);
      cell = "";
    } else if (ch === "\n") {
      row.push(cell.replace(/\r$/, ""));
      rows.push(row);
      row = [];
      cell = "";
    } else {
      cell += ch;
    }
  }
  if (quoted) throw new Error("unterminated quoted CSV cell");
  if (cell !== "" || row.length > 0) {
    row.push(cell.replace(/\r$/, ""));
    rows.push(row);
  }
  return rows.filter((r) => r.some((v) => String(v).trim() !== ""));
}

export function encodeCSV(rows) {
  const quote = (value) => {
    const text = String(value ?? "");
    return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
  };
  return `${rows.map((row) => row.map(quote).join(",")).join("\n")}\n`;
}

export function headerMap(header) {
  return new Map(header.map((name, index) => [String(name).trim(), index]));
}

export function sourceAnimalKey(row, columns) {
  const rfid = cell(row, columns, "rfid", "RFID");
  if (rfid) return rfid;
  const oldID = cell(row, columns, "old_id", "Old ID");
  const suffix = cell(row, columns, "old_id_suffix", "Old ID Suffix");
  if (!oldID || /^(none|na|n\/a)$/i.test(oldID)) return "";
  return !suffix || /^(none|na|n\/a)$/i.test(suffix) ? oldID : `${suffix}-${oldID}`;
}

export function cell(row, columns, ...names) {
  for (const name of names) {
    if (columns.has(name)) return String(row[columns.get(name)] ?? "").trim();
  }
  return "";
}

export function setCell(row, columns, name, value) {
  const index = columns.get(name);
  if (index === undefined) throw new Error(`missing column ${name}`);
  while (row.length <= index) row.push("");
  row[index] = value;
}

export function parseDate(value) {
  const text = String(value ?? "").trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(text)) return null;
  const date = new Date(`${text}T00:00:00.000Z`);
  if (Number.isNaN(date.valueOf())) return null;
  return date.toISOString().slice(0, 10) === text ? date : null;
}

export function dayDiff(left, right) {
  return Math.floor((right.valueOf() - left.valueOf()) / 86_400_000);
}

export function earliestDate(...values) {
  const dates = values.map(parseDate).filter(Boolean).sort((a, b) => a - b);
  return dates[0] ?? null;
}

export function deriveSpecies(breed) {
  const reviewed = SEED_SOURCE_POLICY.reviewed_breed_species ?? {};
  const wanted = String(breed ?? "").trim().toLowerCase();
  const match = Object.entries(reviewed).find(([name]) => name.toLowerCase() === wanted);
  return match?.[1] ?? "";
}

export function isDatedVaccination(value) {
  return parseDate(value) !== null;
}

export function sha256File(file) {
  return crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
}

export function loadFixture(directory) {
  const required = ["manifest.json", ...REQUIRED_DATA_FILES];
  for (const file of required) {
    if (!fs.existsSync(path.join(directory, file))) throw new Error(`missing required fixture file: ${file}`);
  }
  return {
    directory,
    manifest: JSON.parse(fs.readFileSync(path.join(directory, "manifest.json"), "utf8")),
    goats: JSON.parse(fs.readFileSync(path.join(directory, "goats.json"), "utf8")),
    vaccination: JSON.parse(fs.readFileSync(path.join(directory, "vaccination.json"), "utf8")),
    attendance: JSON.parse(fs.readFileSync(path.join(directory, "attendance-jun-26.json"), "utf8")),
    timetable: JSON.parse(fs.readFileSync(path.join(directory, "timetable-goats-team-v1.json"), "utf8")),
    roster: parseCSV(fs.readFileSync(path.join(directory, "roster-name-mapping.jun26-review.csv"), "utf8")),
    shedManagers: parseCSV(fs.readFileSync(path.join(directory, "shed-manager-mapping.jul11-vaccination.csv"), "utf8")),
    corrections: JSON.parse(fs.readFileSync(path.join(directory, "corrections.json"), "utf8")),
  };
}

function expect(condition, message, problems) {
  if (!condition) problems.push(message);
}

function countVaccinationCells(rows) {
  const counts = { dated: 0, pending: 0, na: 0, malformed: 0 };
  for (const row of rows.slice(2)) {
    for (const def of VACCINE_COLUMNS) {
      const value = String(row[def.index] ?? "").trim();
      if (isDatedVaccination(value)) counts.dated += 1;
      else if (/^pending$/i.test(value)) counts.pending += 1;
      else if (/^(na|n\/a|-)?$/i.test(value)) counts.na += 1;
      else counts.malformed += 1;
    }
  }
  return counts;
}

export function validateLoadedFixture(bundle, { checkHashes = true } = {}) {
  const problems = [];
  const { manifest } = bundle;
  expect(manifest.fixture_kind === "synthetic_sanitized", "manifest.fixture_kind must be synthetic_sanitized", problems);
  expect(manifest.data_as_of === "2026-07-20", "manifest.data_as_of must pin the reviewed business date 2026-07-20", problems);
  expect(manifest.minimum_migration === "000008", "manifest.minimum_migration must be 000008", problems);
  expect(manifest.contracts?.source_policy_sha256 === SEED_SOURCE_POLICY_SHA256, "manifest source policy digest differs from contracts/vaccination-seed-source-policy.json", problems);
  expect(manifest.contracts?.vaccination_sop_code === "vaccination.drive", "manifest must bind vaccination.drive SOP", problems);
  expect(manifest.contracts?.proof_mode === "shed_level_video", "manifest proof_mode must be shed_level_video so seed, SOP, Android, and verifier all use one-to-five shed videos instead of per-goat videos", problems);
  expect(manifest.contracts?.video_proof_subject_scope === "shed", "manifest video proof subject must be shed", problems);
  expect(Array.isArray(manifest.contracts?.video_capture_sources) && manifest.contracts.video_capture_sources.includes("in_app_camera") && manifest.contracts.video_capture_sources.includes("gallery_picker"), "manifest shed video proof must allow camera and gallery picker", problems);
  expect(manifest.contracts?.minimum_video_count_per_shed === 1, "manifest must require at least one video per shed", problems);
  expect(manifest.contracts?.maximum_video_count_per_shed === 5, "manifest must allow at most five videos per shed", problems);
  expect(manifest.contracts?.verifier_approval_required === true, "manifest must require verifier approval", problems);
  expect(manifest.contracts?.shed_completion === "acknowledgement_only", "shed completion must be acknowledgement_only", problems);
  expect(manifest.contracts?.local_trigger_primary_rfid_fixture === "CBE-RFID-0001", "manifest must bind the local trigger primary RFID fixture used by emulator scan E2E", problems);
  expect(manifest.contracts?.protocol_route_site === "subcutaneous", "manifest must bind vaccination matrix route_site=subcutaneous", problems);
  expect(manifest.contracts?.protocol_route_site_is_not_operator_form_field === true, "manifest route_site must remain protocol metadata, not an operator form field", problems);
  expect(JSON.stringify(manifest.contracts?.health_case_log_normalization ?? {}) === JSON.stringify(HEALTH_CASE_LOG_NORMALIZATION), "manifest must bind health case-log normalization: Open->sick, Extended->under_treatment, Closed->healthy, Fine->healthy", problems);
  expect(manifest.contracts?.closed_health_case_is_resolved_not_recovering === CLOSED_HEALTH_CASE_IS_RESOLVED_NOT_RECOVERING, "manifest must state Closed health cases are resolved/healthy, never recovering", problems);
  expect(manifest.contracts?.full_access_grant_role === "ceo_internal", "manifest must bind CEO/CXO full-access grants to ceo_internal", problems);
  expect(manifest.contracts?.full_access_workforce_hint === "cxo", "manifest must bind CEO/CXO workforce hint to cxo", problems);

  if (checkHashes) {
    for (const file of REQUIRED_DATA_FILES) {
      expect(manifest.files?.[file]?.sha256 === sha256File(path.join(bundle.directory, file)), `checksum mismatch for ${file}`, problems);
    }
  }

  const goatRows = bundle.goats.values ?? [];
  const vaccinationRows = bundle.vaccination.values ?? [];
  expect(goatRows.length > 1, "goats.json must include rows", problems);
  expect(vaccinationRows.length > 2, "vaccination.json must include two headers and rows", problems);
  if (goatRows.length < 2 || vaccinationRows.length < 3) return problems;

  const goatColumns = headerMap(goatRows[0]);
  for (const required of ["rfid", "farm", "shed", "stage", "age", "breed", "dob", "origin_type", "status", "death_date", "sale_date", "species"]) {
    expect(goatColumns.has(required), `goats.json missing ${required}`, problems);
  }

  const goatByRFID = new Map();
  const shedCounts = new Map();
  const asOf = parseDate(manifest.data_as_of);
  let goats = 0;
  let sheep = 0;
  let correctedStageContradictions = 0;
  for (const [offset, row] of goatRows.slice(1).entries()) {
    const sourceRow = offset + 2;
    const rfid = cell(row, goatColumns, "rfid");
    expect(/^990\d{12}$/.test(rfid), `goats row ${sourceRow}: RFID must be synthetic 15-digit 990…`, problems);
    expect(rfid && !goatByRFID.has(rfid), `goats row ${sourceRow}: duplicate or blank RFID ${rfid}`, problems);
    goatByRFID.set(rfid, row);
    const farm = cell(row, goatColumns, "farm");
    const shed = normalizeShedName(cell(row, goatColumns, "shed")).physical;
    shedCounts.set(`${farm}\0${shed}`, (shedCounts.get(`${farm}\0${shed}`) ?? 0) + 1);

    const species = cell(row, goatColumns, "species").toLowerCase();
    expect(species === "goat" || species === "sheep", `goats row ${sourceRow}: invalid explicit species ${species}`, problems);
    if (species === "goat") goats += 1;
    if (species === "sheep") sheep += 1;
    expect(species === deriveSpecies(cell(row, goatColumns, "breed")), `goats row ${sourceRow}: breed/species contradiction`, problems);

    const dob = parseDate(cell(row, goatColumns, "dob"));
    const origin = cell(row, goatColumns, "origin_type").toLowerCase();
    const entry = parseDate(origin === "birth" ? cell(row, goatColumns, "stage_entry_date") : (cell(row, goatColumns, "purchase_date") || cell(row, goatColumns, "stage_entry_date")));
    const terminal = earliestDate(cell(row, goatColumns, "death_date"), cell(row, goatColumns, "sale_date"));
    if (dob && entry) expect(dob <= entry, `goats row ${sourceRow}: DOB after entry`, problems);
    if (dob && terminal) expect(dob <= terminal, `goats row ${sourceRow}: DOB after terminal event`, problems);
    const delivery = parseDate(cell(row, goatColumns, "delivery_date"));
    if (dob && delivery) expect(dob <= delivery, `goats row ${sourceRow}: delivery before DOB`, problems);

    const mother = cell(row, goatColumns, "mother_id");
    if (mother) expect(/^990\d{12}$/.test(mother), `goats row ${sourceRow}: mother_id must resolve to a synthetic RFID`, problems);

    if (dob && asOf) {
      const kid = Math.floor(dayDiff(dob, asOf) / 7) <= SEED_SOURCE_POLICY.kid_finish_cutoff_completed_weeks;
      const stage = cell(row, goatColumns, "stage").toUpperCase();
      const age = cell(row, goatColumns, "age").toLowerCase();
      const stageKid = stage.startsWith("K");
      if (stageKid !== kid || ((age === "kid") !== kid)) correctedStageContradictions += 1;
    }
    const stageEntry = parseDate(cell(row, goatColumns, "stage_entry_date"));
    const daysInStage = cell(row, goatColumns, "days_in_stage");
    if (stageEntry && daysInStage) {
      expect(Number(daysInStage) === Math.max(0, dayDiff(stageEntry, asOf)), `goats row ${sourceRow}: days_in_stage is stale`, problems);
    }
  }
  for (const row of goatRows.slice(1)) {
    const mother = cell(row, goatColumns, "mother_id");
    if (mother) {
      const motherRow = goatByRFID.get(mother);
      expect(Boolean(motherRow), `unresolved mother RFID ${mother}`, problems);
      expect(mother !== cell(row, goatColumns, "rfid"), `self-referential mother RFID ${mother}`, problems);
      if (motherRow) {
        expect(/female|doe|ewe/i.test(cell(motherRow, goatColumns, "gender")), `mother RFID ${mother} is not female`, problems);
        expect(cell(motherRow, goatColumns, "species") === cell(row, goatColumns, "species"), `mother RFID ${mother} has different species`, problems);
        const childDOB = parseDate(cell(row, goatColumns, "dob"));
        const motherDOB = parseDate(cell(motherRow, goatColumns, "dob"));
        if (childDOB && motherDOB) expect(motherDOB < childDOB, `mother RFID ${mother} is not older than child`, problems);
        if (goatColumns.has("mother_breed")) expect(cell(row, goatColumns, "mother_breed") === cell(motherRow, goatColumns, "breed"), `mother RFID ${mother} breed metadata is stale`, problems);
      }
    }
  }
  expect(correctedStageContradictions === 0, `fixture has ${correctedStageContradictions} age/stage contradictions`, problems);

  const vaccColumns = headerMap(vaccinationRows[0]);
  const seenVaccRFID = new Set();
  for (const [offset, row] of vaccinationRows.slice(2).entries()) {
    const sourceRow = offset + 3;
    const rfid = cell(row, vaccColumns, "RFID");
    expect(goatByRFID.has(rfid), `vaccination row ${sourceRow}: unknown RFID ${rfid}`, problems);
    expect(!seenVaccRFID.has(rfid), `vaccination row ${sourceRow}: duplicate RFID ${rfid}`, problems);
    seenVaccRFID.add(rfid);
    const goat = goatByRFID.get(rfid);
    if (!goat) continue;
    const species = cell(goat, goatColumns, "species").toLowerCase();
    const dob = parseDate(cell(goat, goatColumns, "dob"));
    const terminal = earliestDate(cell(goat, goatColumns, "death_date"), cell(goat, goatColumns, "sale_date"));
    for (const def of VACCINE_COLUMNS) {
      const value = String(row[def.index] ?? "").trim();
      if (!isDatedVaccination(value)) continue;
      const date = parseDate(value);
      expect(def.species.includes(species), `vaccination row ${sourceRow}: ${def.vaccine} incompatible with ${species}`, problems);
      if (dob) {
        expect(date >= dob, `vaccination row ${sourceRow}: ${def.vaccine} before DOB`, problems);
        expect(dayDiff(dob, date) >= def.minimumAgeDays, `vaccination row ${sourceRow}: ${def.vaccine} before minimum age`, problems);
      }
      if (terminal) expect(date <= terminal, `vaccination row ${sourceRow}: ${def.vaccine} after terminal event`, problems);
      if (asOf) expect(date <= asOf, `vaccination row ${sourceRow}: future vaccination`, problems);
    }
  }
  expect(seenVaccRFID.size === goatByRFID.size, `goat/vaccination identity coverage differs (${goatByRFID.size} goats vs ${seenVaccRFID.size} vaccination rows)`, problems);

  const vaccCounts = countVaccinationCells(vaccinationRows);
  for (const [key, value] of Object.entries(vaccCounts)) {
    expect(value === manifest.counts?.vaccination_cells?.[key], `vaccination ${key}=${value}, manifest=${manifest.counts?.vaccination_cells?.[key]}`, problems);
  }
  expect(goatByRFID.size === manifest.counts?.animals, `animal count ${goatByRFID.size} differs from manifest`, problems);
  expect(goats === manifest.counts?.species?.goat && sheep === manifest.counts?.species?.sheep, "species counts differ from manifest", problems);
  expect(shedCounts.size === manifest.counts?.sheds, `shed count ${shedCounts.size} differs from manifest`, problems);

  const attendanceRows = bundle.attendance.values ?? [];
  const attendanceHeaders = (attendanceRows[0] ?? []).map((v) => String(v).trim().toLowerCase());
  for (const header of SENSITIVE_ATTENDANCE_HEADERS) expect(!attendanceHeaders.includes(header), `attendance fixture contains forbidden PII/payroll header ${header}`, problems);
  const paymentValues = [];
  for (let rowIndex = 1; rowIndex < attendanceRows.length; rowIndex += 1) {
    for (let column = 0; column < (attendanceRows[rowIndex] ?? []).length; column += 1) {
      if (/^(paid|unpaid|salary|payroll)$/i.test(String(attendanceRows[rowIndex][column] ?? "").trim())) {
        paymentValues.push(`attendance row ${rowIndex + 1}:column ${column + 1}`);
      }
    }
  }
  expect(paymentValues.length === 0, `attendance fixture contains payment/status values: ${paymentValues.slice(0, 5).join(", ")}`, problems);
  const attendanceColumns = headerMap(attendanceRows[0] ?? []);
  const staffCodes = new Set();
  const staffNames = new Set();
  const staffCodeByName = new Map();
  const rosterSeatByCode = new Map();
  for (let index = 1; index < attendanceRows.length; index += 1) {
    const name = cell(attendanceRows[index], attendanceColumns, "Name");
    if (!name) continue;
    expect(/^Fixture Staff \d{3}$/.test(name), `attendance row ${index + 1}: non-synthetic staff name`, problems);
    staffNames.add(name);
    staffCodeByName.set(name, `HRMS-JUN26-${String(index + 1).padStart(3, "0")}`);
  }

  const rosterHeader = headerMap(bundle.roster[0] ?? []);
  const rosterByCenterPosition = new Map();
  for (let index = 1; index < bundle.roster.length; index += 1) {
    const row = bundle.roster[index];
    const center = cell(row, rosterHeader, "center");
    const position = cell(row, rosterHeader, "timetable_position").replace(/\s+/g, " ");
    const candidate = cell(row, rosterHeader, "jun26_candidate");
    const confidence = cell(row, rosterHeader, "confidence");
    expect(center === "CBE" || center === "CPT", `roster row ${index + 1}: invalid center`, problems);
    expect(candidate.startsWith("Fixture Staff "), `roster row ${index + 1}: candidate must be synthetic`, problems);
    expect(confidence !== "UNRESOLVED", `roster row ${index + 1}: unresolved position`, problems);
    if (confidence === "MANUAL_SEED") {
      const code = `HRMS-MANUAL-${9000 + index + 1}`;
      staffCodes.add(code);
      rosterSeatByCode.set(code, { center, position, candidate });
    } else {
      expect(staffNames.has(candidate), `roster row ${index + 1}: candidate absent from attendance fixture`, problems);
      const code = staffCodeByName.get(candidate);
      if (code) {
        staffCodes.add(code);
        rosterSeatByCode.set(code, { center, position, candidate });
      }
    }
    const key = `${center}\0${position}`;
    expect(!rosterByCenterPosition.has(key), `duplicate roster seat ${center}/${position}`, problems);
    rosterByCenterPosition.set(key, candidate);
  }
  for (const center of ["CBE", "CPT"]) {
    for (const position of ["Preventive Care Manager", "Backup Manager", "Park Head"]) {
      expect(rosterByCenterPosition.has(`${center}\0${position}`), `missing ${center} ${position}`, problems);
    }
  }
  // The full source fixture keeps the original timetable seat names. CPT-only
  // vaccination rehearsal seeds must map those reviewed CPT seats to equal
  // manager-tier vaccination operators: Amit Friday off, Darshan Sunday off,
  // Sagar Saturday off.

  const managerHeader = headerMap(bundle.shedManagers[0] ?? []);
  const managerSheds = new Map();
  for (let index = 1; index < bundle.shedManagers.length; index += 1) {
    const row = bundle.shedManagers[index];
    const park = cell(row, managerHeader, "park_code");
    const shedName = cell(row, managerHeader, "shed_name");
    const shedCode = cell(row, managerHeader, "shed_code");
    const managerCode = cell(row, managerHeader, "manager_code");
    const backupCode = cell(row, managerHeader, "backup_manager_code");
    const needsReview = cell(row, managerHeader, "needs_review").toLowerCase();
    const count = Number(cell(row, managerHeader, "goat_count"));
    const normalizedShed = normalizeShedName(shedName);
    const key = `${park}\0${normalizedShed.physical}`;
    const aggregate = managerSheds.get(key) ?? { count: 0, managerCode, backupCode };
    expect(aggregate.managerCode === managerCode && aggregate.backupCode === backupCode, `shed manager row ${index + 1}: partition manager mismatch for ${park}/${normalizedShed.physical}`, problems);
    expect(Boolean(shedCode), `shed manager row ${index + 1}: blank shed_code`, problems);
    expect(staffCodes.has(managerCode), `shed manager row ${index + 1}: unknown manager ${managerCode}`, problems);
    expect(staffCodes.has(backupCode), `shed manager row ${index + 1}: unknown backup ${backupCode}`, problems);
    expect(managerCode !== backupCode, `shed manager row ${index + 1}: manager and backup must differ`, problems);
    const managerSeat = rosterSeatByCode.get(managerCode);
    const backupSeat = rosterSeatByCode.get(backupCode);
    expect(managerSeat?.center === park && managerSeat?.position === "Preventive Care Manager", `shed manager row ${index + 1}: manager must hold ${park} Preventive Care Manager`, problems);
    expect(backupSeat?.center === park && backupSeat?.position === "Backup Manager", `shed manager row ${index + 1}: backup must hold ${park} Backup Manager`, problems);
    expect(cell(row, managerHeader, "manager_name") === managerSeat?.candidate, `shed manager row ${index + 1}: manager name/code mismatch`, problems);
    expect(cell(row, managerHeader, "backup_manager_name") === backupSeat?.candidate, `shed manager row ${index + 1}: backup name/code mismatch`, problems);
    expect(needsReview === "false", `shed manager row ${index + 1}: needs_review must be false`, problems);
    aggregate.count += Number.isFinite(count) ? count : 0;
    managerSheds.set(key, aggregate);
  }
  for (const [key, aggregate] of managerSheds.entries()) {
    expect(shedCounts.get(key) === aggregate.count, `shed manager aggregate goat_count mismatch for ${key.replace("\0", "/")}`, problems);
  }
  const mappedGoats = [...managerSheds.values()].reduce((sum, aggregate) => sum + aggregate.count, 0);
  expect(managerSheds.size === shedCounts.size, `shed manager coverage ${managerSheds.size}/${shedCounts.size}`, problems);
  expect(mappedGoats === goatByRFID.size, `shed manager goat total ${mappedGoats}/${goatByRFID.size}`, problems);

  const expectedCorrections = manifest.counts?.corrections ?? {};
  expect(expectedCorrections.source_vaccination_cells_changed === 0, "source vaccination cells changed; committed fixture must preserve vaccination truth byte-for-byte", problems);
  for (const [key, value] of Object.entries(expectedCorrections)) {
    expect(bundle.corrections.counts?.[key] === value, `corrections.${key} differs from manifest`, problems);
  }
  return problems;
}

export function validateFixture(directory, options) {
  const bundle = loadFixture(directory);
  const problems = validateLoadedFixture(bundle, options);
  return { bundle, problems };
}

export function updateManifestHashes(directory, manifest) {
  manifest.files = {};
  for (const file of REQUIRED_DATA_FILES) {
    manifest.files[file] = {
      bytes: fs.statSync(path.join(directory, file)).size,
      sha256: sha256File(path.join(directory, file)),
    };
  }
  return manifest;
}

// Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations
// 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination
// HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no
// fixture/source-data change is required. See fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews.

// 2026-07-23 operator-config auto-cascade: migration 000036 adds obligation_operator_config_replan_watermarks, an operational idempotency-watermark table (no seed data / no HRMS-source rows; consumer-only). No fixture bytes change.

// Coupling review 2026-07-24 (BUG-009/010/011/023/024): the seed pipeline gained
// `make seed-vaccination-cpt-operator-drive` (executable CPT rehearsal chain), the
// read-only `seed-checkout-staleness-gate` ahead of every DB write, an executed
// expected-drive-schedule proof inside seed-closeout, DisallowUnknownFields on the
// operator-roster loader, newly consumed `directors` / `leadership_full_access`
// blocks, and a shift end-minute range corrected to 0..1439. None of it changes the
// committed jun-26 fixture bytes: that bundle ships no cpt-operator-roster.json, so
// the loader/contract changes are a no-op for it and every manifest hash, row count,
// and correction-ledger entry is unchanged. Recorded in
// fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews.
