import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./full-vaccine-schedule.tsx", import.meta.url), "utf8");
const moveDrawerSource = readFileSync(new URL("./full-vaccine-schedule-move-drawer.tsx", import.meta.url), "utf8");
const nativeDateInputSource = readFileSync(new URL("./native-date-input.tsx", import.meta.url), "utf8");
const operationsSource = readFileSync(new URL("./operations.tsx", import.meta.url), "utf8");
const shedBoardSource = readFileSync(new URL("../vaccination-sheds/shed-board.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
const adminUiContractSource = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("vaccination schedule is driven by persisted operator assignments", () => {
  assert.match(source, /getVaccinationDriveAssignments/);
  assert.match(source, /section\.full_schedule\.operator_title/);
  assert.match(source, /physicalShed/);
  assert.match(source, /partitionLabel/);
  assert.match(source, /operatorName/);
  assert.match(source, /groupOperatorDayRows/);
  assert.match(source, /shed\.partitions\.push/);
  assert.equal(
    source.includes("getVaccinationSchedule"),
    false,
    "Full schedule must not use the old obligation/vaccine-task aggregate endpoint.",
  );
  assert.equal(
    source.includes("vaccine tasks"),
    false,
    "Operator-cap schedule must display animal assignment counts, not vaccine task totals.",
  );
});

test("vaccination schedule and operator labels are backend-contract owned", () => {
  const forbiddenFrontendLiterals = [
    "Operator drive schedule",
    "Loading planned operator assignments.",
    "Planned vaccination drives split by operator capacity, physical shed, and partition.",
    "Animals assigned",
    "Drive rows",
    "Drive schedule unavailable",
    "No operator drive rows",
    "No persisted operator assignments exist",
    "Whole shed",
    "Operators unassigned",
    "No drive",
  ];
  for (const literal of forbiddenFrontendLiterals) {
    assert.equal(source.includes(literal), false, `${literal} must come from backend pageContract copy, not FE literals`);
    assert.equal(shedBoardSource.includes(literal), false, `${literal} must come from backend pageContract copy, not FE literals`);
  }
  for (const key of [
    "section.full_schedule.operator_title",
    "section.full_schedule.operator_note",
    "section.full_schedule.loading_operator_note",
    "section.full_schedule.assignment_unavailable_title",
    "section.full_schedule.no_assignments_title",
    "section.full_schedule.no_assignments_body",
    "schedule.kpi.animals_assigned",
    "schedule.kpi.drive_rows",
    "schedule.column.operator",
    "schedule.column.partition",
    "schedule.column.workload",
    "schedule.partition.whole_shed",
    "label.operators_unassigned",
    "label.no_drive",
    "schedule.move.open",
    "schedule.move.title",
    "schedule.move.recorded_title",
    "schedule.move.recorded_body",
    "schedule.move.error_title",
    "schedule.move.missing_title",
  ]) {
    assert.match(adminUiContractSource, new RegExp(`"${key.replaceAll(".", "\\.")}"\\s*:`), `${key} missing from backend UI contract`);
  }
});

test("vaccination schedule opens the local drawer from operator-day rows", () => {
  assert.match(source, /LocalOverlayLink/);
  assert.match(source, /ScheduleLocalDrawer/);
  assert.match(source, /drawerRows\(operatorDayRows, pageContract, scope\)/);
  assert.match(source, /#schedule_event=/);
  assert.equal(source.includes("VaccineChipOverflow"), false);
});

test("vaccination schedule drawer shed rows deep-link to the execution goat list", () => {
  assert.match(source, /id:\s*row\.shedId/);
  assert.match(source, /const href = shed\.id/);
  assert.match(source, /scopeHref\(`\/vaccination\/execution\/sheds\/\$\{encodeURIComponent\(shed\.id\)\}`/);
  assert.match(source, /park:\s*row\.parkId/);
});

test("vaccination schedule renders one visible row per operator day", () => {
  assert.match(source, /const operatorDayRows = groupOperatorDayRows\(rows\)/);
  assert.match(source, /operatorDayRows\.map/);
  assert.doesNotMatch(source, /rows\.map\(\(row\) => \(\s*<tr/s);
});

test("vaccination schedule keeps shed totals visible and partition detail out of the overview columns", () => {
  assert.match(source, /shedPartitionTitle\(pageContract, shed\)/);
  assert.match(source, /operator-day-shed/);
  assert.doesNotMatch(source, /<th>\{copy\(pageContract, "schedule\.column\.partition"\)\}<\/th>/);
  assert.doesNotMatch(source, /className="operator-day-partitions"/);
});

test("vaccination schedule keeps workload bars animal-based on backend assignment rows", () => {
  assert.match(source, /schedule-load-card operator-workload-card/);
  assert.match(source, /scheduleLoadBuckets/);
  assert.match(source, /schedule-load-bar/);
  assert.match(source, /row\.dueAnimals/);
  assert.match(source, /row\.deferredAnimals/);
  assert.match(source, /row\.overdueAnimals/);
  assert.match(source, /schedule-load-total">\{row\.animals\}/);
  assert.match(source, /schedule-load-goats">\{row\.totalDoses\}/);
  assert.match(source, /row\.totalDoses/);
  assert.match(source, /row\.animals/);
  assert.match(css, /\.operator-workload-card/);
  assert.match(css, /\.schedule-load-seg\.tone-danger/);
  assert.match(css, /\.schedule-load-seg\.tone-warn/);
  assert.match(css, /\.schedule-load-seg\.tone-done/);
});

test("vaccination schedule move date uses an overlay and native browser date picker", () => {
  assert.match(source, /ScheduleMoveDrawer/);
  assert.match(source, /scheduleMoveHref/);
  assert.match(source, /#schedule_move=/);
  assert.match(source, /schedule_move_result/);
  assert.match(source, /schedule-move-banner/);
  assert.match(source, /redirect\(/);
  assert.doesNotMatch(source, /<input name="override_date"[^>]*type="date"/);
  assert.doesNotMatch(source, /<select name="vaccine_code"/);
  assert.match(moveDrawerSource, /<NativeDateInput name="override_date"/);
  assert.match(moveDrawerSource, /<select name="vaccine_code"/);
  assert.match(moveDrawerSource, /schedule-move-form/);
  assert.match(nativeDateInputSource, /showPicker\?\.\(\)/);
  assert.match(nativeDateInputSource, /onClick=\{handleClick\}/);
  assert.match(nativeDateInputSource, /type="date"/);
});

test("vaccination schedule year navigation is bounded at 2025", () => {
  assert.match(source, /const MIN_SCHEDULE_YEAR = 2025/);
  assert.match(source, /boundedInt\(one\(searchParams \?\? \{\}, "schedule_year"\), CURRENT_YEAR, MIN_SCHEDULE_YEAR, CURRENT_YEAR \+ 5\)/);
  assert.match(source, /year > MIN_SCHEDULE_YEAR/);
});

test("full schedule action is hidden while already inside the schedule view", () => {
  assert.match(operationsSource, /isFullSchedule \? null : <VaccinationFullScheduleButton/);
});

test("vaccination schedule table is visually bounded on desktop", () => {
  assert.equal(
    /\.full-vaccine-schedule-table\{[^}]*min-width\s*:\s*1280px/.test(css),
    false,
    "Full schedule table must not force right-edge columns off-screen at desktop widths.",
  );
  assert.match(css, /\.full-vaccine-schedule-table\{[^}]*width\s*:\s*100%/);
  assert.match(css, /\.full-vaccine-schedule-table th:nth-child\(6\)/);
});
