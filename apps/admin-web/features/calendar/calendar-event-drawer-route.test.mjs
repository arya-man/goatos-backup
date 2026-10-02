import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./calendar.tsx", import.meta.url), "utf8");
const viewSource = readFileSync(new URL("./calendar-full-view.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./calendar-event-drawer.tsx", import.meta.url), "utf8");
const driveDetailSource = readFileSync(new URL("./calendar-drive-detail.tsx", import.meta.url), "utf8");
const contractSource = readFileSync(new URL("./calendar-contract.ts", import.meta.url), "utf8");

// After the FullCalendar rewrite the row-anchor `LocalOverlayLink` is gone — FullCalendar owns
// event rendering and its eventClick handler pushes the same `#calendar_event=<id>` hash the
// drawer listens for via `pushLocalOverlayUrl`. The invariant is that a non-drive event still
// opens a hash-backed local drawer without a global route refresh.
test("calendar summary events open a hash-backed local drawer", () => {
  assert.match(pageSource, /#calendar_event=/);
  assert.match(viewSource, /pushLocalOverlayUrl/);
  assert.match(drawerSource, /LOCAL_OVERLAY_URL_CHANGE_EVENT/);
  assert.match(drawerSource, /popstate/);
});

test("calendar drawer close controls do not navigate the calendar route", () => {
  assert.doesNotMatch(drawerSource, /<Link[\s\S]{0,180}href=\{closeHref\}/);
  assert.match(drawerSource, /currentHistoryEntryIsLocalOverlay/);
  assert.match(drawerSource, /replaceLocalOverlayUrl/);
});

test("calendar drive detail roster opens the goat passport drawer with vaccination history", () => {
  assert.match(driveDetailSource, /LocalOverlayLink/);
  assert.match(driveDetailSource, /goat_passport/);
  assert.match(driveDetailSource, /HerdPassportLocalDrawer/);
  assert.match(driveDetailSource, /HerdPassportDrawerItem/);
  assert.doesNotMatch(driveDetailSource, /<td>\{item\.display_id \|\| "—"\}<\/td>/);
});

test("calendar drive links preserve operational partition identity", () => {
  assert.match(contractSource, /driveExecutionPath\(event\.shed_id\)/);
  assert.match(contractSource, /partition_label:\s*partition/);
  assert.match(drawerSource, /driveExecutionPath\(shedId\)/);
  assert.match(drawerSource, /partition \? \{ partition_label: partition \} : \{\}/);
  assert.match(drawerSource, /scopeHref\(l\.appPath,\s*scope,\s*\{\},\s*l\.query \?\? \{\}\)/);
});

// pr294 L-N1: the drawer's Vaccine / dose cell rendered `${vaccine_name} · ${dose_code}`, i.e.
// "Preventive Care Vaccination Matrix · blue_tongue_first" -- the protocol family and a raw config
// token. The human label is the backend-mapped vaccine_labels; the raw fields are identifiers only.
test("calendar drawer never renders the raw vaccine_name or dose_code", () => {
  const code = drawerSource.replace(/\/\/[^\n]*/g, "");
  assert.doesNotMatch(code, /event\.vaccine_name/);
  assert.doesNotMatch(code, /event\.dose_code/);
  assert.match(code, /event\.vaccine_labels\.join/);
});
