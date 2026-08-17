import assert from "node:assert/strict";
import test from "node:test";

import {
  commonDriveName,
  driveSelectionValue,
  executedDriveCampaigns,
  formatDateSpan,
  formatScheduledDriveDates,
  parseDriveSelectionValue,
  resolveSelectedDrive,
  scheduledDriveCampaigns,
  scheduledDriveRows,
  sortDriveCampaignsNewestFirst,
} from "./command-board-future-drives.ts";

function option(overrides) {
  return {
    driveBatchId: "batch",
    driveName: "FMD",
    label: "FMD",
    parkId: "park-cpt",
    parkName: "CPT",
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
  assert.equal(formatScheduledDriveDates(["2026-07-24", "2026-07-25", "2026-07-26"]), "24–26 Jul 2026");
  assert.equal(formatScheduledDriveDates(["2027-07-26"]), "26 Jul 2027");
});

test("executed drives render as grouped selector campaigns with operator-day completion splits", () => {
  const campaigns = executedDriveCampaigns([
    option({ driveBatchId: "future", status: "planned", plannedDate: "2027-04-09T00:00:00+05:30" }),
    option({
      driveBatchId: "cpt-324",
      status: "in_progress",
      plannedDate: "2026-07-24T00:00:00+05:30",
      targetCount: 324,
      operatorDays: [
        { date: "2026-07-24", targetCount: 114, doseCount: 114 },
        { date: "2026-07-25", targetCount: 163, doseCount: 163 },
        { date: "2026-07-26", targetCount: 47, doseCount: 47 },
      ],
    }),
    option({
      driveBatchId: "cbe-aug-5",
      parkId: "park-cbe",
      parkName: "CBE",
      status: "in_progress",
      plannedDate: "2026-08-05T00:00:00+05:30",
      targetCount: 137,
      operatorDays: [{ date: "2026-08-05", targetCount: 137, doseCount: 137 }],
    }),
  ]);

  assert.deepEqual(campaigns.map((campaign) => campaign.name), ["CBE Adult FMD", "CPT Adult FMD"]);
  assert.deepEqual(campaigns[1].dateKeys, ["2026-07-24", "2026-07-25", "2026-07-26"]);
  assert.deepEqual(campaigns[1].treatments.map((row) => row.targetCount), [114, 163, 47]);
});

test("visible drive selector campaigns are sorted by first date newest first after merging statuses", () => {
  const futureCampaigns = scheduledDriveCampaigns(scheduledDriveRows([
    option({
      driveBatchId: "future-jan",
      plannedDate: "2027-01-09T00:00:00+05:30",
      windowStart: "2027-01-09T00:00:00+05:30",
      windowEnd: "2027-01-09T00:00:00+05:30",
    }),
  ]));
  const executedCampaigns = executedDriveCampaigns([
    option({
      driveBatchId: "aug-5",
      status: "completed",
      plannedDate: "2026-08-05T00:00:00+05:30",
      operatorDays: [{ date: "2026-08-05", targetCount: 137, doseCount: 137 }],
    }),
    option({
      driveBatchId: "jul-24-26",
      status: "in_progress",
      plannedDate: "2026-07-24T00:00:00+05:30",
      targetCount: 324,
      operatorDays: [
        { date: "2026-07-24", targetCount: 114, doseCount: 114 },
        { date: "2026-07-25", targetCount: 163, doseCount: 163 },
        { date: "2026-07-26", targetCount: 47, doseCount: 47 },
      ],
    }),
    option({
      driveBatchId: "aug-12",
      status: "completed",
      plannedDate: "2026-08-12T00:00:00+05:30",
      operatorDays: [{ date: "2026-08-12", targetCount: 145, doseCount: 145 }],
    }),
  ]);

  const campaigns = sortDriveCampaignsNewestFirst([...executedCampaigns, ...futureCampaigns]);

  assert.deepEqual(campaigns.map((campaign) => campaign.dateKeys[0]), [
    "2027-01-09",
    "2026-08-12",
    "2026-08-05",
    "2026-07-24",
  ]);
});

test("actual vaccination date spans use leadership-readable dates", () => {
  assert.equal(formatDateSpan("2026-07-24T00:00:00+05:30", "2026-07-26T00:00:00+05:30"), "24–26 Jul 2026");
  assert.equal(formatDateSpan("2026-03-19T00:00:00+05:30", "2026-04-07T00:00:00+05:30"), "19 Mar–7 Apr 2026");
});

test("same-name same-window drives from two parks stay separate rows with their own park labels", () => {
  const rows = scheduledDriveRows([
    option({ driveBatchId: "fmd-cpt" }),
    option({
      driveBatchId: "fmd-cbe",
      parkId: "park-cbe",
      parkName: "CBE",
      targetCount: 131,
      doseCount: 131,
      shedNames: ["Castro 1"],
    }),
  ]);

  assert.equal(rows.length, 2);
  const byPark = Object.fromEntries(rows.map((row) => [row.parkName, row]));
  assert.equal(byPark.CPT.targetCount, 193);
  assert.equal(byPark.CBE.targetCount, 131);
  assert.deepEqual(byPark.CBE.batchIds, ["fmd-cbe"]);
  assert.deepEqual(byPark.CBE.shedNames, ["Castro 1"]);

  const campaigns = scheduledDriveCampaigns(rows);
  assert.equal(campaigns.length, 2);
  assert.deepEqual(campaigns.map((campaign) => campaign.name).sort(), ["CBE Adult FMD", "CPT Adult FMD"]);
});

test("annual pox campaigns from two parks do not collapse into one park's campaign", () => {
  const poxOption = (overrides) => option({
    driveName: "Blue Tongue + Sheep Pox",
    plannedDate: "2027-07-24T00:00:00+05:30",
    windowStart: "2027-07-24T00:00:00+05:30",
    windowEnd: "2027-07-31T00:00:00+05:30",
    ...overrides,
  });
  const rows = scheduledDriveRows([
    poxOption({ driveBatchId: "pox-cpt", targetCount: 229, doseCount: 458 }),
    poxOption({ driveBatchId: "pox-cbe", parkId: "park-cbe", parkName: "CBE", targetCount: 95, doseCount: 95 }),
  ]);
  const campaigns = scheduledDriveCampaigns(rows);

  assert.equal(campaigns.length, 2);
  const byName = Object.fromEntries(campaigns.map((campaign) => [campaign.name, campaign]));
  assert.equal(byName["CPT Adult Annual Pox + Blue Tongue"].targetCount, 229);
  assert.equal(byName["CBE Adult Annual Pox + Blue Tongue"].targetCount, 95);
});

test("a drive with no park from the API is labelled without inventing one", () => {
  assert.equal(commonDriveName("FMD"), "Adult FMD");
  assert.equal(commonDriveName("Goat Pox", "   "), "Adult Annual Pox + Blue Tongue");
  assert.equal(commonDriveName("FMD", "CBE"), "CBE Adult FMD");
});

test("drive selection identity is (batch, park), so one batch in two parks stays two choices", () => {
  const cbe = { driveBatchId: "batch-1", parkId: "park-cbe", parkName: "Park One", driveName: "PPR", label: "", status: "planned", targetCount: 10, doseCount: 10, shedNames: [] };
  const cpt = { ...cbe, parkId: "park-cpt", parkName: "Park Two", targetCount: 20 };

  const cbeValue = driveSelectionValue(cbe.driveBatchId, cbe.parkId);
  const cptValue = driveSelectionValue(cpt.driveBatchId, cpt.parkId);

  assert.notEqual(cbeValue, cptValue);
  assert.deepEqual(parseDriveSelectionValue(cbeValue), { driveBatchId: "batch-1", parkId: "park-cbe" });
  assert.equal(resolveSelectedDrive([cbe, cpt], "batch-1", "park-cpt"), cpt);
  assert.equal(resolveSelectedDrive([cbe, cpt], "batch-1", "park-cbe"), cbe);
});

test("a park-blind or unknown drive selection resolves to nothing rather than the wrong park", () => {
  const cbe = { driveBatchId: "batch-1", parkId: "park-cbe", parkName: "Park One", driveName: "PPR", label: "", status: "planned", targetCount: 10, doseCount: 10, shedNames: [] };
  const cpt = { ...cbe, parkId: "park-cpt", parkName: "Park Two", targetCount: 20 };

  // Older link carrying only the batch while the batch spans two parks: ambiguous, so unresolved.
  assert.equal(resolveSelectedDrive([cbe, cpt], "batch-1", undefined), undefined);
  // Unambiguous single-park batch still resolves without a park on the URL.
  assert.equal(resolveSelectedDrive([cbe], "batch-1", undefined), cbe);
  assert.equal(resolveSelectedDrive([cbe, cpt], "batch-9", "park-cbe"), undefined);
  assert.equal(resolveSelectedDrive([cbe, cpt], undefined, "park-cbe"), undefined);
  // A park that does not hold the batch must not fall back to the park that does.
  assert.equal(resolveSelectedDrive([cbe], "batch-1", "park-cpt"), undefined);
});

test("a skinny API drive catalogue can resolve an unambiguous URL park selection", () => {
  const skinny = { driveBatchId: "batch-1", driveName: "PPR", label: "", status: "planned", targetCount: 10, doseCount: 10, shedNames: [] };

  assert.equal(resolveSelectedDrive([skinny], "batch-1", "park-cpt"), skinny);
});

test("a drive with no park still round-trips as its own selection value", () => {
  const unparked = { driveBatchId: "batch-2", parkId: "", parkName: "", driveName: "PPR", label: "", status: "completed", targetCount: 5, doseCount: 5, shedNames: [] };

  assert.deepEqual(parseDriveSelectionValue(driveSelectionValue("batch-2", "")), { driveBatchId: "batch-2", parkId: "" });
  assert.equal(resolveSelectedDrive([unparked], "batch-2", undefined), unparked);
});
