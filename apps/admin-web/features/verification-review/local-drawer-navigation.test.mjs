import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");
const shellSource = readFileSync(new URL("../../components/mesha-shell.tsx", import.meta.url), "utf8");

test("Verification review records open and close locally without route navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(drawerSource, /LOCAL_OVERLAY_URL_CHANGE_EVENT/);
  assert.match(drawerSource, /popstate/);
  assert.match(drawerSource, /currentHistoryEntryIsLocalOverlay/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
  // The verdict form is the ONLY form left on this screen, so it is what pins the
  // server-action-not-route-navigation property now. This used to assert on the rework and
  // re-assign forms; both were removed on 2026-08-07 because source-task action is the
  // authority's, not the verifier's (SPEC section 1/3, and RoleVerifier does not hold
  // TaskAssign). Asserted as ABSENT so they cannot quietly return to this screen.
  assert.match(drawerSource, /action=\{recordVerificationVerdictAction\}/);
  assert.doesNotMatch(drawerSource, /action=\{reworkVerificationItemAction\}/);
  assert.doesNotMatch(drawerSource, /action=\{reassignVerificationItemAction\}/);
  assert.match(pageSource, /const PATHNAME = "\/actions"/);
  assert.match(drawerSource, /const PATHNAME = "\/actions"/);
});

test("Actions filters and video links are backend-contract driven", () => {
  assert.match(pageSource, /filter_options\.action_types/);
  assert.match(pageSource, /filter_options\.statuses/);
  assert.match(pageSource, /businessDate: scope\.asOf/);
  assert.match(pageSource, /parkId: scope\.parkId/);
  // The mock has no "Open video" link and no verdict explainer card: the proof is the video player the
  // verifier watches, sourced from the backend-signed media URL via ReviewVideoPlayer component
  // (which emits telemetry events and blocks forward seeking), plus the two-step reject that
  // cannot record a rejection without asking for a reason.
  assert.match(drawerSource, /src=\{activeMedia\.download_url\}/);
  assert.match(drawerSource, /<ReviewVideoPlayer/);
  assert.doesNotMatch(drawerSource, /drawer\.media\.open/);
  assert.match(drawerSource, /setRejecting\(true\)/);
  assert.match(drawerSource, /renderLabelOrFallback\(item\.verified_by_name\)/);
});

test("top bar hides the backend-owned as-of calendar filter", () => {
  assert.doesNotMatch(shellSource, /contract\.top_bar\.date_range_selector/);
  assert.doesNotMatch(shellSource, /<TopBarDatePicker/);
  assert.doesNotMatch(shellSource, /onSelectDate=\{\(date\) =>/);
  assert.doesNotMatch(shellSource, /currentScopeHref\(\{ asOf: date \}/);
  assert.match(shellSource, /const pageFilters = Object\.fromEntries\(searchParams\?\.entries\(\) \?\? \[\]\)/);
  assert.match(shellSource, /\.\.\.pageFilters, \.\.\.preserveVaccinationSchedule/);
  assert.doesNotMatch(shellSource, /type="date"/);
  assert.doesNotMatch(shellSource, /date\.menu_aria/);
});
