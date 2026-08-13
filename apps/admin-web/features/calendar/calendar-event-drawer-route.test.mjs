import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./calendar.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./calendar-event-drawer.tsx", import.meta.url), "utf8");
const driveDetailSource = readFileSync(new URL("./calendar-drive-detail.tsx", import.meta.url), "utf8");
const contractSource = readFileSync(new URL("./calendar-contract.ts", import.meta.url), "utf8");

test("calendar summary events open a hash-backed local drawer", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /#calendar_event=/);
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

test("calendar drive links use exact shed identity without partition query", () => {
  assert.match(contractSource, /driveExecutionPath\(event\.shed_id\)/);
  assert.doesNotMatch(contractSource, /partition_label:\s*partition/);
  assert.doesNotMatch(contractSource, /function eventPartitionLabel/);
  assert.match(drawerSource, /driveExecutionPath\(shedId\)/);
  assert.doesNotMatch(drawerSource, /partition \? \{ partition_label: partition \} : \{\}/);
  assert.match(drawerSource, /scopeHref\(l\.appPath,\s*scope,\s*\{\},\s*l\.query \?\? \{\}\)/);
});

test("calendar aggregate shed coverage treats exact shed labels as atomic", () => {
  const start = drawerSource.indexOf("event.shed_labels.map");
  const coverageBlock = drawerSource.slice(start, drawerSource.indexOf("</div>", start));
  assert.ok(start > 0, "expected aggregate shed coverage renderer");
  assert.match(coverageBlock, /event\.shed_labels\.map\(\(label, i\)/);
  assert.match(coverageBlock, /<Tag key=\{`\$\{label\}-\$\{i\}`\} tone="mut">\{label\}<\/Tag>/);
  assert.doesNotMatch(coverageBlock, /shed_partition_labels/);
  assert.doesNotMatch(coverageBlock, /partitionLabel/);
  assert.doesNotMatch(coverageBlock, /operationalLocationLabel/);
  assert.doesNotMatch(coverageBlock, /shed_ids/);
});
