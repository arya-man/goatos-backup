import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./calendar.tsx", import.meta.url), "utf8");

// After the FullCalendar rewrite the /calendar page no longer renders a hand-rolled week/history
// grid, so the old set of match points (WeekView, HistoryView, page-local pagination) is retired.
// The invariant that survives — aggregated vaccination-drive placeholders without a drive_summary
// must not become empty rows on the calendar — still applies: the raw list has to be filtered
// through isRenderableCalendarEvent BEFORE it reaches the FullCalendar adapter, or the calendar
// grid renders empty "drive" pills that click nowhere useful.
test("calendar hides aggregated vaccination drive placeholders without drive summary", () => {
  assert.match(pageSource, /function isRenderableCalendarEvent\(event: CalendarEvent\): boolean/);
  assert.match(pageSource, /event\.aggregated && event\.event_type === "vaccination_drive" && !event\.drive_summary/);
  assert.match(pageSource, /events\.filter\(isRenderableCalendarEvent\)/);
  assert.match(pageSource, /toFullCalendarEvents\(renderableEvents, ownerMeta\)/);
});
