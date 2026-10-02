import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const source = readFileSync(new URL("./full-vaccine-schedule.tsx", import.meta.url), "utf8");
const moveDrawerSource = readFileSync(new URL("./full-vaccine-schedule-move-drawer.tsx", import.meta.url), "utf8");
// The themed picker is a SHARED component now (moved 2026-08-27): Sales uses the same control,
// bounded by `max` instead of `min`. Same file, same guarantees, one directory up.
const themedDatePickerSource = readFileSync(new URL("../../components/themed-date-picker.tsx", import.meta.url), "utf8");
const operationsSource = readFileSync(new URL("./operations.tsx", import.meta.url), "utf8");
const shedBoardSource = readFileSync(new URL("../vaccination-sheds/shed-board.tsx", import.meta.url), "utf8");
const css = legacyCss("mesha-theme");
const adminUiContractFallbackSource = readFileSync(new URL("../../lib/admin-ui-contract.ts", import.meta.url), "utf8");
const adminUiContractSource = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("vaccination schedule is driven by persisted operator assignments", () => {
  assert.match(source, /getVaccinationDriveAssignments/);
  assert.match(source, /section\.full_schedule\.operator_title/);
  assert.match(source, /schedulePenKey\(row\)/);
  assert.match(source, /partitionLabel/);
  assert.match(source, /originalPlannedDate/);
  assert.match(source, /vaccineOriginalDates/);
  assert.match(source, /operatorName/);
  assert.match(source, /groupOperatorDayRows/);
  assert.match(source, /addSchedulePen\(group\.pens, row, row\.animals\)/);
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
  assert.match(source, /drawerRows\(operatorDayRows, scope, closeHref\)/);
  assert.match(source, /#schedule_event=/);
  assert.equal(source.includes("VaccineChipOverflow"), false);
});

test("vaccination schedule drawer shed rows deep-link to the execution goat list", () => {
  assert.match(source, /const href = pen\.shedId/);
  assert.match(source, /scopeHref\(\s*`\/vaccination\/execution\/sheds\/\$\{encodeURIComponent\(pen\.shedId\)\}`/);
  assert.match(source, /park:\s*row\.parkId/);
  assert.match(source, /partition_label:\s*pen\.partitionLabel/);
  assert.match(source, /ret/);
});

test("vaccination schedule renders one visible row per operator day", () => {
  assert.match(source, /const operatorDayRows = groupOperatorDayRows\(rows\)/);
  assert.match(source, /operatorDayRows\.map/);
  assert.doesNotMatch(source, /rows\.map\(\(row\) => \(\s*<tr/s);
});

test("vaccination schedule keeps shed totals visible and partition detail out of the overview columns", () => {
  assert.match(source, /title=\{penTitle\(pen\)\}/);
  assert.match(source, /<Label key=\{pen\.key\} title=\{penTitle\(pen\)\}>/);
  assert.doesNotMatch(source, /<th>\{copy\(pageContract, "schedule\.column\.partition"\)\}<\/th>/);
  assert.doesNotMatch(source, /className="operator-day-partitions"/);
});

test("vaccination schedule keeps workload bars animal-based on backend assignment rows", () => {
  assert.match(source, /scheduleLoadBuckets/);
  assert.match(source, /flexBasis: workloadSegmentWidth\(bucket, segmentTotal\)/);
  assert.match(source, /row\.dueAnimals/);
  assert.match(source, /row\.deferredAnimals/);
  assert.match(source, /row\.overdueAnimals/);
  assert.match(source, /component="span">\{row\.animals\}<\/Typography>/);
  assert.match(source, /\{row\.totalDoses\} \{copy\(pageContract, "schedule\.unit\.doses"\)\}/);
  assert.match(source, /row\.totalDoses/);
  assert.match(source, /row\.animals/);
  // Bucket tones are theme palette colours (template sx), not legacy .schedule-load-* CSS.
  assert.match(source, /danger: "error\.main"/);
  assert.match(source, /warn: "warning\.main"/);
  assert.match(source, /done: "grey\.500"/);
  assert.doesNotMatch(css, /\.operator-workload-card|\.schedule-load-seg/);
});

test("vaccination shed summary row keys include the full rendered summary grain", () => {
  assert.match(shedBoardSource, /const partitionAwareKey = \[/);
  for (const token of [
    "row.parkId",
    "row.shedId",
    "row.partitionLabel",
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
  assert.match(source, /<Alert severity=\{scheduleMoveStatus === "recorded" \? "success" : "error"\} role="status"/);
  assert.match(source, /redirect\(/);
  assert.doesNotMatch(source, /<input name="override_date"[^>]*type="date"/);
  assert.doesNotMatch(source, /<select name="vaccine_code"/);
  assert.match(moveDrawerSource, /<ThemedDatePicker[\s\S]*name="override_date"/);
  // The vaccine choice is an MUI TextField select; the form still posts `vaccine_code` through a hidden
  // input that mirrors the selected value, so the server action reads the same field as before.
  assert.doesNotMatch(moveDrawerSource, /<select/);
  assert.match(moveDrawerSource, /<input type="hidden" name="vaccine_code" value=\{selectedVaccineCode\} \/>/);
  assert.match(moveDrawerSource, /<TextField\s+select[\s\S]*value=\{selectedVaccineCode\}[\s\S]*onChange=\{\(event\) => setSelectedVaccineCode\(event\.target\.value\)\}[\s\S]*displayedRow\.vaccineCodes\.map\(\(code, index\) => \([\s\S]*<MenuItem key=\{code\} value=\{code\}>[\s\S]*displayedRow\.vaccineNames\[index\] \?\? code/);
  assert.match(source, /formData\.get\("vaccine_code"\)/);
  assert.match(moveDrawerSource, /schedule-move-form/);
  assert.doesNotMatch(themedDatePickerSource, /showPicker/);
  assert.doesNotMatch(themedDatePickerSource, /type="date"/);
  assert.doesNotMatch(themedDatePickerSource, /className=.*out/);
  assert.doesNotMatch(themedDatePickerSource, /addDays\(parseDateKey\(min\), 1\)/);
  assert.match(themedDatePickerSource, /const minDate = useMemo\(\(\) => parseDateKey\(min\), \[min\]\)/);
  // FIXJ4: the picker is the template MUI X DatePicker (DD/MM/YYYY, floating label), posting the ISO
  // key through a hidden input; the legacy `.move-date-*` summary button + popover are gone.
  assert.match(moveDrawerSource, /displayedRow\.vaccineOriginalDates\[selectedVaccineCode\]/);
  assert.match(moveDrawerSource, /value=\{selectedVaccineCode\}/);
  assert.match(moveDrawerSource, /min=\{todayIso\(\)\}/);
  assert.doesNotMatch(themedDatePickerSource, /move-date-|\.module\.css/);
  assert.match(themedDatePickerSource, /format="DD\/MM\/YYYY"/);
  assert.match(themedDatePickerSource, /<input ref=\{anchorRef\} type="hidden" name=\{name\}/);
  // Calendar body now uses MUI X DateCalendar (template's CustomDateRangePicker calendar) so the
  // hand-rolled `.move-date-spacer` empty-cell placeholders no longer exist; the check the test
  // guards (not a native date input, month arrows aria-labelled, close-on-outside-click) stays.
  assert.match(themedDatePickerSource, /<DatePicker/);
  assert.match(moveDrawerSource, /schedule\.move\.previous_month/);
  assert.match(moveDrawerSource, /schedule\.move\.next_month/);
  assert.match(moveDrawerSource, /schedule\.move\.invalid_future_date/);
  assert.doesNotMatch(css, /\.move-date-/);
  // FIXJ6: mesha-theme.css (and its --panel paint) is deleted; the picker is the themed MUI X DatePicker.
  assert.match(themedDatePickerSource, /DatePicker/);
});

test("the schedule move drawer is the template temporary drawer (closed = unmounted modal, never a click trap)", () => {
  // MUI Drawer (temporary) unmounts its modal + backdrop when closed, so a closed drawer cannot
  // intercept page clicks; the old hand-made backdrop needed aria-hidden pointer-events rules.
  assert.match(moveDrawerSource, /<DetailDrawer\b/);
  assert.match(moveDrawerSource, /open=\{drawerOpen\}/);
  assert.match(moveDrawerSource, /onClose=\{closeDrawer\}/);
  assert.doesNotMatch(moveDrawerSource, /schedule-drawer-backdrop|schedule-side-drawer/);
  assert.doesNotMatch(css, /\.schedule-drawer-backdrop|\.schedule-side-drawer/);
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

test("vaccination schedule table scrolls inside its card on the template Scrollbar", () => {
  // Template table anatomy: the table scrolls sideways in the card's Scrollbar at its own min width;
  // no legacy .full-vaccine-schedule-table / .vaccination-schedule-* CSS (FIXJ2 J1 P0-1/P0-2).
  assert.match(source, /<Scrollbar>\s*<Table aria-label=\{copy\(pageContract, "section\.full_schedule\.operator_title"\)\} sx=\{\{ minWidth: FULL_SCHEDULE\.tableMinWidth \}\}>/);
  assert.match(source, /<TableHeadCustom headCells=\{headCells\} \/>/);
  assert.doesNotMatch(source, /className=/);
  assert.doesNotMatch(source, /style=\{\{/);
  assert.doesNotMatch(css, /full-vaccine-schedule-table|vaccination-schedule-(?:card|hd|summary|legend|tablewrap)/);
});

// pr294 L-N6: the operator schedule read "No operator drive rows in Oct 2026" while Scheduled Ahead
// listed ten October drives -- the table reads persisted operator assignments, not planned drives.
test("empty operator schedule points at Scheduled Ahead instead of denying the month's drives", async () => {
  const { readFileSync: read } = await import("node:fs");
  const service = read(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
  assert.match(service, /"section\.full_schedule\.no_assignments_title":\s+"No drives given to an operator yet"/);
  assert.match(service, /"section\.full_schedule\.no_assignments_body":\s+"Planned drives are listed under Scheduled Ahead/);
});
