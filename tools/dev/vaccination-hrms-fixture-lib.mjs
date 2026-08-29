// Vaccination HRMS fixture utilities — used by seed scripts and ceo_ai reporting views
// Coupling review 2026-08-05: migration 000109 adds animal_stage_lookup.age_band
// ('kid'/'adult'/NULL) so a shifting stamps the destination cohort's kid/adult band onto the
// animals it moves. NO CHANGE to this file's contract: age_band lives on the stage VOCABULARY,
// not on the HRMS roster or vaccination source rows validated here, and the fixture ships no
// stage-catalog rows. Deliberately NOT derived from source DOB or from min_age_days/max_age_days
// -- F2 fattening cohorts stay kid to 67 weeks -- so do not add a source-date-derived age band.
// 2026-08-05: no fixture shape change from the SOP rework-reopen work. Reopening an
// accepted task is a runtime state transition; the fixture's source columns are untouched.
// (migrations 000024-000027) to load and validate vaccination source data.
// Coupling review 2026-07-25: migration 000045 adds a nullable
// vaccination_capacity_config.max_shots_per_animal_per_drive admin override. This fixture
// seeds no override (sweeper falls back to rule_dsl/default), so its data and hashes are
// unchanged; the loader/validator needs no new field handling.
// Coupling review 2026-08-04: seed-roster-real's defaultDepartmentModules now grants
// health -> aas_health + counts + milk + feed_direction + vaccination, and pairs milk with
// counts everywhere counts is granted. Those are department -> module GRANT rows derived at
// seed time from department codes, not source-spreadsheet fields, so no header, raw byte,
// file hash, row count, vaccination date anchor or schedule-path selection changes here.
// Coupling review 2026-08-05: CBE/CPT seed-port sources may carry an optional
// rfid2 column for real double-tag aliases, and the seed publication may exclude
// named vaccines for stock/defer decisions. These are importer/publication
// controls, not committed full-fixture source bytes.
// Coupling review 2026-08-06: migrations 000120/000121/000123 add partition_label
// columns to verification_items, weighing_campaign_sheds, and health_cases with backfill from
// goat_shed_partitions (canonical per-animal partition assignment seeded at animal placement).
// NO CHANGE to fixture bytes/hashes/counts: partition resolution is a seed DATA contract
// (every animal in a partitioned shed must have a goat_shed_partitions row at seed time), not
// a source-file schema change. Source shed labels like "Godel 1 - Part 3" are parsed by the
// seeder into physical shed + partition at animal write time; goat_shed_partitions rows are
// seeded from that parsed label, and later migrations backfill partition_label on location-bearing
// output tables from that seeded data. The fixture's source validation adds a partition-resolution
// contract check (pass/warning); it does not change validation logic or impact fixture loading.
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
// Accepted source history is canonical for one-time vaccine rules. If kernel
// generation emits an active obligation for the same goat/rule after the seed
// imports accepted completion history, the seed must supersede that active row
// and keep the accepted completion visible as goat history.
export const ACCEPTED_ONE_TIME_HISTORY_SUPERSEDES_ACTIVE_SEED_OBLIGATIONS = true;
// An operator-drive rehearsal source may ship an authoritative operator-roster
// contract (cpt-operator-roster.json). When present it is the source of truth
// for that park's field capacity: seed-roster-real recasts the resolved seats
// into equal per-person vaccination_operator_<name> positions (manager tier,
// not a backup slot) with contract-owned week-offs, instead of the generic
// jun-26 PC-manager/backup/park-head trio. The generic timetable model still
// governs every other center/source that ships no such contract.
export const OPERATOR_ROSTER_CONTRACT_FILE = "cpt-operator-roster.json";
export const OPERATOR_ROSTER_OVERLAY_IS_AUTHORITATIVE_FIELD_CAPACITY = true;
// CPT-only operator-roster bundles must be able to start from a clean migrated
// local/dev DB: seed-roster-real resolves only centers present in the source
// bundle and may create that required park row before member/position import.
export const OPERATOR_ROSTER_CLEAN_DB_BOOTSTRAPS_PRESENT_CENTERS_ONLY = true;
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
// Coupling review 2026-08-22: migration 000187 adds NULLABLE
// workforce_members.first_name/last_name/email for the People/HRMS directory
// and the in-app Add Person onboarding. NO CHANGE to this fixture contract:
// seed commands never populate the three columns (identity stays
// display_name/display_code), the unique email index ignores NULLs, and emails
// enter only through POST /admin/workforce/people at runtime. Do not add
// email/name-split fields to the roster fixture.
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
export const OPERATOR_ROSTER_VERIFIERS_FIELD = "verifiers";
export const OPERATOR_ROSTER_VERIFIER_ROLE = "verifier";
export const OPERATOR_ROSTER_VERIFIER_IDENTITY_PROVIDER = "firebase_email_password";
export const OPERATOR_ROSTER_VERIFIER_HAS_ZERO_EXECUTION_CAPACITY = true;
export const GROWTH_DIRECTOR_ROLE_HINT = "growth_director";
export const GROWTH_DIRECTOR_IS_WEIGHING_ONLY = true;
export const GROWTH_DIRECTOR_HAS_ZERO_VACCINATION_CAPACITY = true;
export const ADULT_CAMPAIGN_HISTORY_CUTOFF_IS_AS_OF_BUSINESS_DAY_END = true;
// A vaccination operator's app designation is its EXECUTION capability, not its HR
// capacity-tier. seed-roster-real gives each rehearsal operator a manager-tier
// vaccination_operator_<name> position for capacity/roster, and a person may also
// hold a higher-ranked June seat (e.g. park_head). deriveRoleHint must still emit
// primary_role_hint="operator" for anyone holding an active vaccination_operator_*
// position, overriding tier/bestCode — otherwise the mobile scan gate
// (operatorAllowed = primary_role_hint == "operator") silently drops every scan for
// a field executor mislabelled supervisor/park_head (the Amit/Darshan/Sagar STG
// incident). Capacity tier "manager" is NOT a role and never a non-operator hint.
export const OPERATOR_ROSTER_OPERATOR_RESOLVES_TO_OPERATOR_ROLE_HINT = true;
// The same CPT operator-roster contract also owns Android field-login setup
// after DB seed: every executable vaccination operator must have a distinct
// email/password identity derived from operators[].email_hint. Shared operator
// logins, shared passwords, founder/CXO logins for field execution, and
// plaintext passwords in git are invalid seed evidence.
export const OPERATOR_ANDROID_LOGIN_CONTRACT_FIELD = "operator_android_login";
export const OPERATOR_ANDROID_LOGIN_IDENTITY_PROVIDER = "firebase_email_password";
export const OPERATOR_ANDROID_LOGIN_EMAIL_FIELD = "operators[].email_hint";
export const OPERATOR_ANDROID_SHARED_PASSWORD_FORBIDDEN = true;
// CPT operator-drive materialization must emit reviewed shed-manager mapping
// rows from cpt-operator-roster.json, not a header-only placeholder. Darshan is
// the default reviewed shed/drive owner and Sagar is the reviewed backup for
// every active CPT physical shed in that packet.
export const OPERATOR_ROSTER_MATERIALIZES_REVIEWED_SHED_OWNERS = true;
export const CPT_OPERATOR_ROSTER_DEFAULT_SHED_OWNER = "vaccination_operator_darshan";
export const CPT_OPERATOR_ROSTER_BACKUP_SHED_OWNER = "vaccination_operator_sagar";
// Completed accepted vaccination history is still drive work evidence: shed
// summary/operator reads must resolve it through the scheduler default operator
// when no open vaccination_drive_assignments row remains.
export const ACCEPTED_COMPLETED_HISTORY_RESOLVES_DEFAULT_OPERATOR = true;
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
export const ADULT_BLANK_HISTORY_JOINS_NORMAL_DRIVE = true;
export const VACCINATION_MEDICAL_DATE_FIELD = "vaccination_completions.administered_at";
export const SCHEDULE_PATH_POLICY = "shared_schedule_path_for_goat";
export const OPTIONAL_SECONDARY_RFID_FIELD = "rfid2";
export const SEED_PUBLICATION_VACCINE_EXCLUSION_ENV = "GOATOS_SEED_EXCLUDE_VACCINES";

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
  expect(manifest.contracts?.adult_blank_history_joins_normal_drive === ADULT_BLANK_HISTORY_JOINS_NORMAL_DRIVE, "manifest must auto-enrol adult blank-history animals into the normal generated drive", problems);
  expect(manifest.contracts?.schedule_path_policy === SCHEDULE_PATH_POLICY, "manifest must bind kid/adult path selection to shared SchedulePathForGoat", problems);
  expect(manifest.contracts?.vaccination_medical_date_field === VACCINATION_MEDICAL_DATE_FIELD, "manifest must bind repeat timing to operator-administered vaccination_completions.administered_at", problems);

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
    // This assertion is what guarantees the seeded stack can BOOT, not just a naming rule.
    // seed-position-duties derives the pc.vaccination `manage` duty only from manager-tier seats
    // that are neither operators nor backups -- vaccination_operator_* and backup_manager stay on
    // `execute` -- and seed-closeout fails the whole stack when no active seat holds `manage`,
    // because the reminder ladder resolves both duty types. "Preventive Care Manager" is that seat.
    // Relaxing this to allow an operator or backup here would produce a fixture that validates,
    // seeds, and then loops "Local database preparation failed" at closeout.
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

// Coupling review 2026-07-29: seed-roster-real adds feed_direction to the preventive_care
// department module grant. This changes runtime module/navigation authorization only; it does not
// change HRMS roster rows, vaccination history, source dates, fixture bytes, hashes, or counts.
// Coupling review 2026-07-20: the counts (approval, department_module_grants) and feed_direction migrations
// 000009-000015 plus the seed-roster-real department-module-grants write were reviewed against the vaccination
// HRMS seed source. They are orthogonal to it (counts/feed tables, not the vaccination roster source), so no
// fixture/source-data change is required. See fixtures/vaccination-hrms-source-full/manifest.json -> seed_contract_coupling_reviews.
// Coupling review 2026-08-21: seed-roster-real adds pc_care (deworming / ticks removal /
// hoof trimming / hair trimming) to the preventive_care department module grant; migration
// 000181_pc_care_module_grants.sql applies the same grant to already-seeded databases. This
// changes runtime module/navigation authorization only; it does not change HRMS roster rows,
// vaccination history, source dates, fixture bytes, hashes, or counts.
// Coupling review 2026-08-26: protocol_rule_dimensions.procurement_purpose is publish-time
// compiled selector metadata with DEFAULT 'all'. It is not read from the fixture, not validated
// here, and changes no HRMS/vaccination source bytes, hashes, row counts, SOP proof grain, or
// operator-capacity contract.

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

// Coupling review 2026-07-24 (CPT no-PPR 2026 seed): the CPT operator-drive
// Makefile target may publish a packet-scoped rule subset without PPR. The
// canonical source fixture and validator still preserve PPR history mapping.

// Coupling review 2026-07-24/25: CPT adult campaign grouping and
// seed_catchup_overrides affect only operator-drive rehearsal bundles that ship
// cpt-operator-roster.json. Adult entry_date is never a vaccination due-date
// anchor; adult blank-history rows are ordinary generated drive work by physical
// shed/partition and require no manual approval. The committed full fixture has no such contract file, so raw
// fixture bytes and manifest hashes remain unchanged.
// Coupling review 2026-08-02: blank-history adults are automatically generated into the
// next compatible normal adult drive; `manual_campaign` remains rule metadata, not a manual
// approval gate. Accepted operator submission records `vaccination_completions.administered_at`
// as the medical anchor; later verifier/director timestamps never replace it. Raw fixture
// vaccination/HRMS bytes and hashes remain unchanged.

// Coupling review 2026-07-25: adult non-repeating physical-partition campaign
// obligations are generation-idempotent at campaign grain. Replaying with a
// corrected as-of date may realign unbatched derived obligations, but this
// fixture library's raw source hashes/counts remain unchanged.

// Coupling review 2026-07-25: CPT operator-roster verifiers are auth/workflow
// reviewers only. They seed pending Firebase email-password verifier grants and
// must never become vaccination operators, shift seats, or animal-capacity rows.
// Coupling review 2026-07-25: selected_operator_ids is admin-authored runtime
// assignment config over seeded operators. The committed full fixture has no
// cpt-operator-roster.json, so fixture bytes/hashes and loader semantics remain
// unchanged.
// Coupling review 2026-07-25: migration 000002 only restores that runtime
// selected_operator_ids column on already-migrated DBs. It backfills from the
// default operator and does not introduce a source fixture field.
// 2026-08-01 verify-duty seeding: seed-position-duties now derives a verify duty per notification
// module from notificationbridge.PendingNotificationDutyModules. Duty rows are generated from
// position codes at seed time and are not an HRMS-source field, so no fixture bytes, hashes or
// counts change here.
// Coupling review 2026-08-04: vaccination_drive_date_overrides requested/applied
// safe-date metadata is runtime override state, not source data. Approved combo
// helpers share clinical scheduling config only; fixture bytes, hashes, counts,
// HRMS rows, SOP contracts, and validation semantics remain unchanged.
// Coupling review 2026-08-05: the CBE/CPT controlled port changes runtime import
// alias handling, campaign-filtered sweeps, verifier grant backfill, weighing
// duties, and active position conflict keys only. The HRMS fixture source remains
// byte-for-byte unchanged; current open-port policy explicitly excludes Blue
// Tongue and PPR generation until stock/source scheduling is confirmed.

// Coupling review 2026-08-05 (preventive_care module grants): seed-roster-real drops "milk" from
// preventive_care defaultDepartmentModules; migration 000110 deactivates the existing preventive_care
// milk + aas_health department_module_grants rows. Nothing in this fixture contract changes: module
// grants gate a bottom bar, not a vaccination source input. No fixture byte, hash, row count, goat
// field, protocol rule or operator capacity is touched, so no validator here needed updating.
// Coupling review 2026-08-14: migrations 000160/000161 add capacity and cohort columns to
// shed_partitions for the Counts/Sheds directory. Those are pen-catalog configuration fields, not
// vaccination HRMS source fields; the committed fixture bytes, hashes, row counts, SOP proof grain,
// protocol rows, goat_shed_partitions placement contract, and operator capacity rules stay unchanged.
// Coupling review 2026-08-15: seed import and runtime generation share SchedulePathForGoat.
// No raw fixture bytes or HRMS rows change; the fixture contract records that kid/adult path
// selection is derived once from reviewed DOB/stage/history evidence through that shared policy.
// Coupling review 2026-08-16: Flushing is now seeded as an active animal_stage_lookup row with NULL
// age_band (migration 000171 parity for fresh tenants). This fixture library still has no
// stage-catalog input: committed HRMS/vaccination source bytes, hashes, rows, SOP proof grain,
// operator capacity, and validation semantics are unchanged.
// Coupling review 2026-08-29: manual vaccination anchors now suppress same-family manual_campaign
// seed rows before the anchor. This library remains unchanged because the fixture source still
// describes imported rows/dates, not runtime manual-anchor replay behavior.
