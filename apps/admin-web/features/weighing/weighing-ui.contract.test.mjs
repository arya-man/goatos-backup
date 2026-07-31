import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));

function source(name) {
  return readFileSync(join(here, name), "utf8");
}

test("weighing UI keeps persona capabilities explicit", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /canCreate: leadership/);
  assert.match(data, /canEdit: leadership/);
  assert.match(data, /canPublish: leadership/);
  assert.match(data, /canExecute: operator/);
  assert.match(data, /reviewOnly: role === "director"/);
  assert.match(page, /Monitor \/ review only/);
  assert.match(page, /cannot create, publish, edit, or execute/);
});

test("weighing UI renders category-aware progress and blocks lumpsum individual truth", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /individual_animal/);
  assert.match(data, /per_shed_partition/);
  assert.match(page, /Free-flow RFID\/tag bucket \+ weight \+ mandatory per-row video/);
  assert.match(page, /Free-flow shed bucket result \+ total count \+ at least one synced video/);
  assert.match(page, /categoryLabel\[row\.category\]/);
});

test("weighing UI uses product labels without fabricating live API labels", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(page, /In progress/);
  assert.match(page, /Needs review/);
  assert.match(page, /Shed total/);
  assert.doesNotMatch(page, /replaceAll\("_", " "\)/);
  assert.match(data, /Operator not reported by API/);
  assert.match(data, /Park not reported by API/);
  assert.doesNotMatch(data, /operatorName: "Assigned operator"/);
  assert.doesNotMatch(data, /parkName: "Selected park"/);
});

test("weighing UI keeps bucket language free-flow and avoids vaccination wrong-shed copy", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  // Renamed from `expectedShed`: weighing is free-flow and has no expected roster, so
  // the field names the bucket the scan was ASSIGNED to, not one it was expected in.
  assert.match(data, /originalShed/);
  assert.doesNotMatch(data, /expectedShed/);
  assert.match(data, /originalPartition/);
  assert.match(data, /actualShed/);
  assert.match(data, /currentPartition/);
  assert.match(page, /Bucket \/ original/);
  assert.match(page, /Captured \/ current/);
  assert.doesNotMatch(page, /wrong shed/);
});

test("weighing leadership planner covers park-week task creation and duplicate edit state", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /duplicateBlocked/);
  assert.match(data, /existingCampaignId/);
  assert.match(data, /getWeighingPlannerCatalog/);
  assert.match(data, /plannerFromCatalog/);
  assert.match(data, /selectedPark\?\.park_id/);
  assert.match(data, /operator\.display_name/);
  assert.doesNotMatch(data, /CPT · Channapatna/);
  assert.doesNotMatch(data, /Castro 1/);
  assert.doesNotMatch(data, /Gandhi 1/);
  assert.doesNotMatch(data, /Amit Kumar/);
  assert.match(page, /Create weekly kids weighing task/);
  assert.match(page, /Edit weekly kids weighing task/);
  assert.match(page, /Step 2 · Park/);
  assert.match(page, /Step 3 · Sheds/);
  assert.match(page, /Individual/);
  assert.match(page, /Lumpsum/);
  assert.match(page, /Assign operator/);
  assert.match(page, /Already scheduled/);
  assert.match(page, /task already exists/);
  assert.match(page, /Existing task/);
  assert.match(page, /Edit existing task/);
  assert.match(page, /create is blocked/);
  assert.match(page, /It never creates a second task/);
  assert.match(page, /duplicate_blocked/);
});

test("weighing week strip is derived from campaign response", () => {
  const data = source("data.ts");

  assert.match(data, /selectCampaign\(result\.data\.items, selectedWeek, selectedCampaignId\)/);
  assert.match(data, /weeksFromCampaigns\(result\.data\.items, campaign\)/);
  assert.match(data, /sort\(\(a, b\) => a\.period_start_date\.localeCompare\(b\.period_start_date\)\)/);
  assert.match(data, /weekRangeLabel\(item\.period_start_date, item\.period_end_date\)/);
  assert.doesNotMatch(data, /key: "2026-07-19"/);
  assert.doesNotMatch(data, /key: "2026-08-02"/);
});
