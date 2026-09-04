import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./calendar.tsx", import.meta.url), "utf8");

test("calendar hides aggregated vaccination drive placeholders without drive summary", () => {
  assert.match(pageSource, /function isRenderableCalendarEvent\(event: CalendarEvent\): boolean/);
  assert.match(pageSource, /event\.aggregated && event\.event_type === "vaccination_drive" && !event\.drive_summary/);
  assert.match(pageSource, /filterEventsForSelectedWeek\(events, anchorDay, dayFilter\)\.filter\(isRenderableCalendarEvent\)/);
  assert.match(pageSource, /events\.filter\(isRenderableCalendarEvent\)\.sort/);
});
