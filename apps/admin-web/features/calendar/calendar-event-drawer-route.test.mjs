import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./calendar.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./calendar-event-drawer.tsx", import.meta.url), "utf8");

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
