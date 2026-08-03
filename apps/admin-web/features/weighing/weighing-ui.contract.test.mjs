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

// REVIEW-26 (weighing isolation): weighing is free-flow and carries NO herd-gate concept. A scan
// dumps whatever RFID the reader produces; the app never resolves it to a goat and never compares
// against a herd-sourced "expected" location. The ONLY rule is the duplicate-identifier check
// within a task's shed bucket. The former "Wrong-shed scans" / "Animal review notes" surfaces (and
// their backend Progress.wrong_shed_count reads) reintroduced exactly the herd-gate the maintainer
// rejected, so they are gone -- this test locks that removal in place.
test("weighing UI carries no herd/clinical review surface (free-flow, duplicate-check only)", () => {
  const data = source("data.ts");
  const page = source("page.tsx");

  assert.doesNotMatch(data, /WeighingWrongShedRow/);
  assert.doesNotMatch(data, /WeighingMissingRow/);
  assert.doesNotMatch(data, /wrongShedRows/);
  assert.doesNotMatch(data, /missingRows/);
  assert.doesNotMatch(data, /wrongShedScans/);
  assert.doesNotMatch(data, /wrongShedCount/);
  assert.doesNotMatch(data, /wrong_shed_count/);
  assert.doesNotMatch(page, /Wrong-shed scans/);
  assert.doesNotMatch(page, /Animal review notes/);
  assert.doesNotMatch(page, /In ICU/);
  assert.doesNotMatch(page, /Sold \/ transferred/);
  assert.doesNotMatch(page, /reviewLabel/);
  assert.doesNotMatch(page, /animalDisplayId/);
  assert.doesNotMatch(page, /row\.rfid/);
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

  assert.match(data, /selectCampaign\(result\.data\.items, thisWeekStart, selectedWeek, selectedCampaignId, selectedParkId\)/);
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

test("W14 FIX: campaign selection is park-scoped, never crosses parks on a plain park switch", () => {
  const data = source("data.ts");
  const page = source("page.tsx");
  const parkSelector = source("park-selector.tsx");
  // selectCampaign moved into campaign-selection.ts so the rule can be unit-tested;
  // data.ts is "server-only" and cannot be imported by node --test.
  const selection = source("campaign-selection.ts");

  // selectCampaign is park-scoped: an explicit park filters the candidate campaigns
  // before any week/campaign-id lookup, and never falls back to another park's row.
  assert.match(selection, /export function selectCampaign</);
  assert.match(selection, /selectedParkId\?: string,/);
  assert.match(selection, /const scoped = selectedParkId/);
  assert.match(selection, /item\.park_id === selectedParkId/);
  assert.match(data, /import \{ selectCampaign, weekStartOfDay \} from "\.\/campaign-selection";/);

  // ParkSelector must clear the stale campaign/week selection on every park switch so
  // Park A's campaign can never be treated as "the current campaign" for Park B.
  assert.match(parkSelector, /params\.delete\("campaign"\)/);
  assert.match(parkSelector, /params\.delete\("week"\)/);

  // editingCampaignId / existingCampaignId must derive from the (now park-scoped)
  // selectedItem and campaign, so a plain park switch to an empty park offers CREATE.
  assert.match(data, /editingCampaignId = selectedCampaignId && selectedItem\?\.campaign_id === selectedCampaignId/);
  assert.match(page, /Create weekly kids weighing task/);
});

test("W15 FIX: truncated shed list disables Save draft and Publish with a visible reason", () => {
  const page = source("page.tsx");

  assert.match(page, /Shed list is truncated at the 500-shed display cap/);
  assert.match(page, /disabled={planner\.shedListTruncated}/);
  assert.match(page, /Save and Publish are disabled: the shed list is truncated/);
  assert.match(page, /Disabled: shed list truncated at the 500-shed display cap/);
});

test("W20 FIX: unbacked captured count renders no fabricated digit", () => {
  const page = source("page.tsx");

  assert.doesNotMatch(page, /<b>\{row\.completedCount\}<\/b> \{row\.capturedCountIsBacked \? "captured" : "captured \(n\/a\)"\}/);
  assert.match(page, /row\.capturedCountIsBacked \? <><b>\{row\.completedCount\}<\/b> captured<\/> : "captured \(n\/a\)"/);
});

test("W21-TS: operator name distinguishes genuine roster gap from unassigned using backend-resolved field", () => {
  const data = source("data.ts");

  assert.match(data, /shed\.operator_display_name\?\.trim\(\) \|\| ""/);
  assert.match(data, /if \(shed\.operator_user_id\) \{/);
  assert.match(data, /operatorDisplay = "Roster gap \(operator not found\)";/);
  assert.match(data, /operatorDisplay = "Unassigned";/);
});

test("the planner week follows the SELECTED campaign, and 'this week' is the IST business day", () => {
  const data = source("data.ts");
  const selection = source("campaign-selection.ts");

  // selectCampaign may decline the URL's week and land on a different campaign. Preferring the URL
  // week for the planner read then showed that campaign's header above another week's catalog.
  assert.match(data, /const plannerWeek = campaign\.weekStart \|\| selectedWeek \|\| thisWeekStart;/);
  assert.doesNotMatch(data, /const plannerWeek = selectedWeek \|\|/);

  // "This week" must come from the Asia/Kolkata business day, not the UTC calendar date.
  assert.match(data, /import \{ todayIso \} from "@\/lib\/format";/);
  assert.match(data, /const thisWeekStart = weekStartOfDay\(todayIso\(\)\);/);
  assert.match(data, /selectCampaign\(result\.data\.items, thisWeekStart, selectedWeek, selectedCampaignId, selectedParkId\)/);

  // campaign-selection.ts must not reach for a clock of its own.
  assert.doesNotMatch(selection, /new Date\(\)/);
  assert.doesNotMatch(selection, /getUTCFullYear\(\)\, now/);
});
