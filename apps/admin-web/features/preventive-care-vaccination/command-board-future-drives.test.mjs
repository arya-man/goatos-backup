import assert from "node:assert/strict";
import test from "node:test";

import {
  formatDateSpan,
  formatScheduledDriveDates,
  scheduledDriveCampaigns,
  scheduledDriveRows,
} from "./command-board-future-drives.ts";

function option(overrides) {
  return {
    driveBatchId: "batch",
    driveName: "FMD",
    label: "FMD",
    status: "planned",
    plannedDate: "2027-01-06T00:00:00+05:30",
    windowStart: "2027-01-06T00:00:00+05:30",
    windowEnd: "2027-01-13T00:00:00+05:30",
    targetCount: 193,
    doseCount: 193,
    shedNames: ["Gandhi", "Godel 2", "Mandela 2"],
    ...overrides,
  };
}

test("split operator days fold into one logical drive with the full animal total", () => {
  const rows = scheduledDriveRows([
    option({ driveBatchId: "fmd-day-1" }),
    option({
      driveBatchId: "fmd-day-2",
      plannedDate: "2027-01-07T00:00:00+05:30",
      targetCount: 131,
      doseCount: 131,
      shedNames: ["Godel 1", "Old Yashoda"],
    }),
    option({ driveBatchId: "old-ettt", driveName: "ET+TT", status: "completed" }),
  ]);

  assert.equal(rows.length, 1);
  assert.equal(rows[0].driveName, "FMD");
  assert.equal(rows[0].targetCount, 324);
  assert.equal(rows[0].doseCount, 324);
  assert.deepEqual(rows[0].dateKeys, ["2027-01-06", "2027-01-07"]);
  assert.deepEqual(rows[0].batchIds, ["fmd-day-1", "fmd-day-2"]);
  assert.deepEqual(rows[0].shedNames, ["Gandhi", "Godel 1", "Godel 2", "Mandela 2", "Old Yashoda"]);
});

test("combined vaccines keep head count separate from vaccination count", () => {
  const rows = scheduledDriveRows([
    option({
      driveBatchId: "pox-day-1",
      driveName: "Blue Tongue + Sheep Pox",
      plannedDate: "2027-07-24T00:00:00+05:30",
      windowStart: "2027-07-24T00:00:00+05:30",
      windowEnd: "2027-07-31T00:00:00+05:30",
      targetCount: 109,
      doseCount: 218,
    }),
    option({
      driveBatchId: "pox-day-2",
      driveName: "Blue Tongue + Sheep Pox",
      plannedDate: "2027-07-25T00:00:00+05:30",
      windowStart: "2027-07-24T00:00:00+05:30",
      windowEnd: "2027-07-31T00:00:00+05:30",
      targetCount: 120,
      doseCount: 240,
    }),
  ]);

  assert.equal(rows[0].targetCount, 229);
  assert.equal(rows[0].doseCount, 458);
});

test("sheep and goat treatments roll up to one common adult annual campaign", () => {
  const rows = scheduledDriveRows([
    option({
      driveBatchId: "pox-sheep",
      driveName: "Blue Tongue + Sheep Pox",
      plannedDate: "2027-07-24T00:00:00+05:30",
      windowStart: "2027-07-24T00:00:00+05:30",
      windowEnd: "2027-07-31T00:00:00+05:30",
      targetCount: 229,
      doseCount: 458,
    }),
    option({
      driveBatchId: "pox-goat",
      driveName: "Goat Pox",
      plannedDate: "2027-07-26T00:00:00+05:30",
      windowStart: "2027-07-24T00:00:00+05:30",
      windowEnd: "2027-07-31T00:00:00+05:30",
      targetCount: 95,
      doseCount: 95,
    }),
  ]);
  const campaigns = scheduledDriveCampaigns(rows);

  assert.equal(campaigns.length, 1);
  assert.equal(campaigns[0].name, "CPT Adult Annual Pox + Blue Tongue");
  assert.equal(campaigns[0].targetCount, 324);
  assert.equal(campaigns[0].doseCount, 553);
  assert.equal(campaigns[0].treatments.length, 2);
});

test("future drive date labels compress consecutive operator days", () => {
  assert.equal(formatScheduledDriveDates(["2027-01-06", "2027-01-07"]), "6–7 Jan 2027");
  assert.equal(formatScheduledDriveDates(["2027-07-26"]), "26 Jul 2027");
});

test("actual vaccination date spans use leadership-readable dates", () => {
  assert.equal(formatDateSpan("2026-07-24T00:00:00+05:30", "2026-07-26T00:00:00+05:30"), "24–26 Jul 2026");
  assert.equal(formatDateSpan("2026-03-19T00:00:00+05:30", "2026-04-07T00:00:00+05:30"), "19 Mar–7 Apr 2026");
});
