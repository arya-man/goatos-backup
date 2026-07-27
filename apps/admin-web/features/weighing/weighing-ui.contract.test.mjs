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
  assert.match(page, /RFID \+ animal identity \+ weight \+ mandatory per-animal video/);
  assert.match(page, /Selected-scope result \+ scope proof; no individual weight update/);
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

test("weighing UI exposes wrong-shed expected-original and actual-current context", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /expectedShed/);
  assert.match(data, /originalPartition/);
  assert.match(data, /actualShed/);
  assert.match(data, /currentPartition/);
  assert.match(page, /Expected \/ original/);
  assert.match(page, /Actual \/ current/);
});

test("weighing leadership planner covers park-week task creation and duplicate edit state", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.match(data, /duplicateBlocked/);
  assert.match(data, /existingCampaignId/);
  assert.match(data, /CPT · Channapatna/);
  assert.match(data, /Castro 1/);
  assert.match(data, /Gandhi 1/);
  assert.match(data, /Amit Kumar/);
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

  assert.match(data, /weeksFromCampaigns\(result\.data\.items, campaign\)/);
  assert.match(data, /sort\(\(a, b\) => a\.period_start_date\.localeCompare\(b\.period_start_date\)\)/);
  assert.match(data, /weekRangeLabel\(item\.period_start_date, item\.period_end_date\)/);
  assert.doesNotMatch(data, /key: "2026-07-19"/);
  assert.doesNotMatch(data, /key: "2026-08-02"/);
});
