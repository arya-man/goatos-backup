import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./full-vaccine-schedule.tsx", import.meta.url), "utf8");
const moveDrawerSource = readFileSync(new URL("./full-vaccine-schedule-move-drawer.tsx", import.meta.url), "utf8");
const themedDatePickerSource = readFileSync(new URL("./themed-date-picker.tsx", import.meta.url), "utf8");
const operationsSource = readFileSync(new URL("./operations.tsx", import.meta.url), "utf8");
const shedBoardSource = readFileSync(new URL("../vaccination-sheds/shed-board.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
const adminUiContractFallbackSource = readFileSync(new URL("../../lib/admin-ui-contract.ts", import.meta.url), "utf8");
const adminUiContractSource = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("vaccination schedule is driven by persisted operator assignments", () => {
  assert.match(source, /getVaccinationDriveAssignments/);
  assert.match(source, /section\.full_schedule\.operator_title/);
  assert.match(source, /physicalShed/);
  assert.match(source, /operational_location_display/);
  assert.doesNotMatch(source, /partitionLabel/);
  assert.match(source, /originalPlannedDate/);
  assert.match(source, /vaccineOriginalDates/);
  assert.match(source, /operatorName/);
  assert.match(source, /groupOperatorDayRows/);
  assert.doesNotMatch(source, /partitions:/);
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
    "schedule.move.previous_month",
    "schedule.move.next_month",
    "schedule.move.invalid_future_date",
  ]) {
    assert.match(adminUiContractSource, new RegExp(`"${key.replaceAll(".", "\\.")}"\\s*:`), `${key} missing from backend UI contract`);
    assert.match(adminUiContractFallbackSource, new RegExp(`"${key.replaceAll(".", "\\.")}"\\s*:`), `${key} missing from admin-web fallback contract`);
  }
});

test("vaccination schedule opens the local drawer from operator-day rows", () => {
  assert.match(source, /LocalOverlayLink/);
  assert.match(source, /ScheduleLocalDrawer/);
  assert.match(source, /drawerRows\(operatorDayRows, pageContract, scope, closeHref\)/);
  assert.match(source, /#schedule_event=/);
  assert.equal(source.includes("VaccineChipOverflow"), false);
});

test("vaccination schedule drawer shed rows deep-link to the execution goat list", () => {
  assert.match(source, /id:\s*row\.shedId/);
  assert.match(source, /const href = shed\.id/);
  assert.match(source, /scopeHref\(\s*`\/vaccination\/execution\/sheds\/\$\{encodeURIComponent\(shed\.id\)\}`/);
  assert.match(source, /park:\s*row\.parkId/);
  assert.doesNotMatch(source, /partition_label:\s*partition/);
  assert.match(source, /ret/);
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

test("vaccination shed summary row keys include the full rendered summary grain", () => {
  assert.match(shedBoardSource, /const rowKey = \[/);
  for (const token of [
    "row.parkId",
    "row.shedId",
    "row.nextDue",
    "row.status",
    "row.capacity",
    "row.animals",
    "row.due",
    "row.done",
    "row.sessions",
    "rowIndex",
  ]) {
    assert.match(shedBoardSource, new RegExp(token.replaceAll(".", "\\.")));
  }
  assert.doesNotMatch(shedBoardSource, /row\.partitionLabel/);
  assert.doesNotMatch(
    shedBoardSource,
    /const partitionAwareKey = `\$\{row\.shedId\}\|\$\{row\.partitionLabel/,
    "shed + partition is not unique when the backend returns multiple summary rows for one shed",
  );
});

test("vaccination schedule move date uses an overlay and themed dark date picker", () => {
  assert.match(source, /ScheduleMoveDrawer/);
  assert.match(source, /scheduleMoveHref/);
  assert.match(source, /#schedule_move=/);
  assert.match(source, /schedule_move_result/);
  assert.match(source, /schedule_move_vaccine/);
  assert.match(source, /schedule_move_date/);
  assert.match(source, /schedule-move-banner/);
  assert.match(source, /redirect\(/);
  assert.doesNotMatch(source, /<input name="override_date"[^>]*type="date"/);
  assert.doesNotMatch(source, /<select name="vaccine_code"/);
  assert.match(moveDrawerSource, /<ThemedDatePicker[\s\S]*name="override_date"/);
  assert.match(moveDrawerSource, /<select name="vaccine_code"/);
  assert.match(moveDrawerSource, /schedule-move-form/);
  assert.doesNotMatch(themedDatePickerSource, /showPicker/);
  assert.doesNotMatch(themedDatePickerSource, /type="date"/);
  assert.doesNotMatch(themedDatePickerSource, /className=.*out/);
  assert.doesNotMatch(themedDatePickerSource, /addDays\(parseDateKey\(min\), 1\)/);
  assert.match(themedDatePickerSource, /const minDate = useMemo\(\(\) => parseDateKey\(min\), \[min\]\)/);
  assert.match(moveDrawerSource, /displayedRow\.vaccineOriginalDates\[selectedVaccineCode\]/);
  assert.match(moveDrawerSource, /value=\{selectedVaccineCode\}/);
  assert.match(moveDrawerSource, /onChange=\{\(event\) => setSelectedVaccineCode\(event\.currentTarget\.value\)\}/);
  assert.match(moveDrawerSource, /min=\{todayIso\(\)\}/);
  assert.match(themedDatePickerSource, /move-date-popover/);
  assert.match(themedDatePickerSource, /move-date-spacer/);
  assert.match(themedDatePickerSource, /document\.addEventListener\("pointerdown", onPointerDown\)/);
  assert.match(themedDatePickerSource, /detailsRef\.current\.open = false/);
  assert.match(moveDrawerSource, /schedule\.move\.previous_month/);
  assert.match(moveDrawerSource, /schedule\.move\.next_month/);
  assert.match(moveDrawerSource, /schedule\.move\.invalid_future_date/);
  assert.match(css, /\.move-date-popover/);
  assert.match(css, /\.move-date-spacer/);
  assert.match(css, /background:var\(--panel\)/);
});

test("closed vaccination schedule drawers do not intercept page clicks", () => {
  assert.match(css, /\.schedule-drawer-backdrop\[aria-hidden="true"\]\{[^}]*pointer-events\s*:\s*none/);
  assert.match(css, /\.schedule-drawer-backdrop\[aria-hidden="true"\]\{[^}]*visibility\s*:\s*hidden/);
  assert.match(moveDrawerSource, /disabled=\{!drawerOpen\}/);
  assert.match(moveDrawerSource, /tabIndex=\{drawerOpen \? 0 : -1\}/);
});

test("vaccination schedule month navigation stays in the operating window", () => {
  assert.match(source, /const MIN_SCHEDULE_YEAR = 2025/);
  assert.match(source, /boundedInt\(one\(searchParams \?\? \{\}, "schedule_year"\), CURRENT_YEAR, MIN_SCHEDULE_YEAR, CURRENT_YEAR \+ 5\)/);
  assert.match(source, /function scheduleWindowMonths/);
  assert.match(source, /\[-1, 0, 1\]\.map/);
  assert.match(source, /const monthWindow = scheduleWindowMonths\(\)/);
  assert.match(source, /monthWindow\.map/);
  assert.doesNotMatch(source, /Array\.from\(\{ length: 12 \}, \(_, index\) => index \+ 1\)/);
  assert.doesNotMatch(source, /copy\(pageContract, "action\.next_year"\)/);
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
