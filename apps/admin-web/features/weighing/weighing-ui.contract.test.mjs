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
  // NEW-6: the operator name is BACKEND-RESOLVED (operator_display_name). The UI must read it
  // rather than re-deriving from a client catalog -- the old hardcoded "Operator not reported by
  // API" printed even when the API HAD reported it. The honest-fallback intent of this assertion
  // is preserved below: an empty name is still distinguished, never fabricated.
  assert.match(data, /operator_display_name/);
  assert.match(data, /Roster gap \(operator not found\)/);
  assert.match(data, /Unassigned/);
  assert.doesNotMatch(data, /operatorName: "Operator not reported by API"/);
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
  // NEW-8: the park step moved into the ParkSelector client component, because the radios had
  // no onChange and selecting a non-default park silently produced an empty shed list and a
  // failing submit. The step still exists -- it just lives in the component that can react to it.
  assert.match(page, /<ParkSelector/);
  assert.match(source("park-selector.tsx"), /Step 2 · Park/);
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

test("DEFECT B14 FIX: weighing UI reads real pending_verification_count and ready_to_close from backend", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  // Verify that pending_verification_count is read from the API instead of hardcoded
  assert.match(data, /proofPendingCount: shed\.pending_verification_count/);
  // Verify that ready_to_close is read from the API
  assert.match(data, /readyToClose: shed\.ready_to_close/);
  // Verify that campaign-level proofPending aggregates from scopes, not hardcoded
  assert.match(data, /const proofPending = scopes\.reduce\(\(sum, scope\) => sum \+ scope\.proofPendingCount/);
  // Finding 9: "linked" is now gated on readyToClose ALONE. The old expression treated
  // "nothing submitted yet" (proofPendingCount === 0) as verified, rendering it identically to
  // genuinely-verified proof. Three distinct states are now required.
  assert.match(page, /row\.readyToClose \? \(/);
  assert.match(page, /not submitted/);
  assert.doesNotMatch(page, /!row\.readyToClose && row\.proofPendingCount > 0 \? "warn" : "ok"/);
  assert.doesNotMatch(data, /proofPendingCount: 0[,\n]/);
});

test("DEFECT B18 FIX: weighing UI supports closed status in both campaign and shed states", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  // Verify that closed status is in the WeighingCampaignState union
  assert.match(data, /"draft" \| "published" \| "in_progress" \| "delayed" \| "completed" \| "closed"/);
  // Verify that closed status is in the WeighingScopeStatus union
  assert.match(data, /"pending" \| "in_progress" \| "needs_review" \| "completed" \| "closed" \| "delayed"/);
  // Verify that closed status has a statusTone mapping
  assert.match(page, /closed: "ok"/);
  // Verify that closed status has a statusLabel mapping
  assert.match(page, /closed: "Closed"/);
});
