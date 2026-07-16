import assert from "node:assert/strict";
import test from "node:test";

import {
  calendarEventDateKey,
  calendarEventWeekday,
  filterEventsForSelectedWeek,
  selectedWeekDateKeys,
} from "./calendar-window-events.ts";

const event = (event_id, due_at) => ({ event_id, due_at });

test("calendar selected-week filter rejects catch-up rows outside the selected week", () => {
  const rows = [
    event("selected-thu", "2026-07-02T09:00:00+05:30"),
    event("future-fri", "2026-07-17T09:00:00+05:30"),
    event("future-thu-a", "2026-07-23T09:00:00+05:30"),
    event("future-thu-b", "2026-07-30T09:00:00+05:30"),
  ];

  assert.deepEqual(
    filterEventsForSelectedWeek(rows, "2026-07-01", "Thu").map((row) => row.event_id),
    ["selected-thu"],
  );
});

test("calendar all-week rendering is bounded to the anchor week", () => {
  const rows = [
    event("selected-wed", "2026-07-01T09:00:00+05:30"),
    event("selected-sun", "2026-07-05T09:00:00+05:30"),
    event("future-thu", "2026-07-23T09:00:00+05:30"),
  ];

  assert.deepEqual(
    filterEventsForSelectedWeek(rows, "2026-07-01").map((row) => row.event_id),
    ["selected-wed", "selected-sun"],
  );
});

test("calendar selected-week dates are the Monday-Sunday dates around the anchor", () => {
  assert.deepEqual(Array.from(selectedWeekDateKeys("2026-07-01")), [
    "2026-06-29",
    "2026-06-30",
    "2026-07-01",
    "2026-07-02",
    "2026-07-03",
    "2026-07-04",
    "2026-07-05",
  ]);
});

test("calendar event date keys and weekdays use the India business day", () => {
  assert.equal(calendarEventDateKey("2026-07-01T23:30:00Z"), "2026-07-02");
  assert.equal(calendarEventWeekday("2026-07-01T23:30:00Z"), "Thu");
});
