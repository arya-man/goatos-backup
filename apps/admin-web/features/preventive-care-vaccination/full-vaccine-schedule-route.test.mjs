import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./full-vaccine-schedule.tsx", import.meta.url), "utf8");
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
  ]) {
    assert.match(adminUiContractSource, new RegExp(`"${key.replaceAll(".", "\\.")}"\\s*:`), `${key} missing from backend UI contract`);
  }
});

test("vaccination schedule no longer opens the retired aggregate drawer", () => {
  assert.equal(source.includes("LocalOverlayLink"), false);
  assert.equal(source.includes("ScheduleLocalDrawer"), false);
  assert.equal(source.includes("VaccineChipOverflow"), false);
  assert.equal(source.includes("#schedule_event="), false);
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

test("vaccination schedule keeps workload bars on backend assignment rows", () => {
  assert.match(source, /schedule-load-card operator-workload-card/);
  assert.match(source, /schedule-load-bar/);
  assert.match(source, /row\.totalDoses/);
  assert.match(source, /row\.animals/);
  assert.match(css, /\.operator-workload-card/);
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
