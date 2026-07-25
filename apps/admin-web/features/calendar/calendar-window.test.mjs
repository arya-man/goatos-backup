import assert from "node:assert/strict";
import test from "node:test";

import { calendarDateKeyParts, calendarMonthAnchor, enumerateWeekDays, historyWindow, monthWindow, shiftedDateKey, shiftedMonthStartKey, weekWindow } from "./calendar-window.ts";

test("weekWindow returns the Monday-Sunday week across month boundaries", () => {
  assert.deepEqual(weekWindow("2026-07-03"), {
    dateFrom: "2026-06-29",
    dateTo: "2026-07-05",
  });
});

test("weekWindow returns the Monday-Sunday week across year boundaries", () => {
  assert.deepEqual(weekWindow("2026-01-01"), {
    dateFrom: "2025-12-29",
    dateTo: "2026-01-04",
  });
});

test("monthWindow remains date-only and leap-year safe", () => {
  assert.deepEqual(monthWindow("2028-02-15"), {
    dateFrom: "2028-02-01",
    dateTo: "2028-02-29",
  });
});

test("calendarMonthAnchor does not shift first-of-month anchors across timezones", () => {
  const oldTZ = process.env.TZ;
  try {
    process.env.TZ = "UTC";
    assert.deepEqual(calendarMonthAnchor("2026-10-01", "2026-07-17"), { year: 2026, month: 9 });
    process.env.TZ = "Asia/Kolkata";
    assert.deepEqual(calendarMonthAnchor("2026-10-01", "2026-07-17"), { year: 2026, month: 9 });
  } finally {
    if (oldTZ !== undefined) process.env.TZ = oldTZ;
    else delete process.env.TZ;
  }
});

test("shiftedMonthStartKey keeps picker navigation on calendar months", () => {
  assert.equal(shiftedMonthStartKey(2026, 9, -1), "2026-09-01");
  assert.equal(shiftedMonthStartKey(2026, 9, 1), "2026-11-01");
  assert.equal(shiftedMonthStartKey(2026, 0, -1), "2025-12-01");
  assert.equal(shiftedMonthStartKey(2026, 11, 1), "2027-01-01");
});

test("calendarDateKeyParts keeps date-only labels stable on UTC hosts", () => {
  const oldTZ = process.env.TZ;
  try {
    process.env.TZ = "UTC";
    assert.deepEqual(calendarDateKeyParts("2026-07-25"), {
      year: 2026,
      month: 6,
      day: 25,
      weekday: 6,
    });
    assert.deepEqual(calendarDateKeyParts("2026-07-26"), {
      year: 2026,
      month: 6,
      day: 26,
      weekday: 0,
    });
  } finally {
    if (oldTZ !== undefined) process.env.TZ = oldTZ;
    else delete process.env.TZ;
  }
});

test("shiftedDateKey keeps week navigation on date keys instead of host-local instants", () => {
  const oldTZ = process.env.TZ;
  try {
    process.env.TZ = "UTC";
    assert.equal(shiftedDateKey("2026-07-25", -7), "2026-07-18");
    assert.equal(shiftedDateKey("2026-07-25", 7), "2026-08-01");
  } finally {
    if (oldTZ !== undefined) process.env.TZ = oldTZ;
    else delete process.env.TZ;
  }
});

test("historyWindow keeps a bounded forty five day trail ending on the anchor", () => {
  assert.deepEqual(historyWindow("2026-07-12"), {
    dateFrom: "2026-05-29",
    dateTo: "2026-07-12",
  });
});

test("enumerateWeekDays returns exactly 7 consecutive calendar days regardless of timezone (UTC)", () => {
  const oldTZ = process.env.TZ;
  process.env.TZ = "UTC";
  try {
    const days = enumerateWeekDays("2026-06-29");
    assert.equal(days.length, 7, "should return exactly 7 days");
    assert.deepEqual(days, [
      "2026-06-29",
      "2026-06-30",
      "2026-07-01",
      "2026-07-02",
      "2026-07-03",
      "2026-07-04",
      "2026-07-05",
    ]);
  } finally {
    if (oldTZ !== undefined) process.env.TZ = oldTZ;
    else delete process.env.TZ;
  }
});

test("enumerateWeekDays returns exactly 7 consecutive calendar days regardless of timezone (Asia/Kolkata)", () => {
  const oldTZ = process.env.TZ;
  process.env.TZ = "Asia/Kolkata";
  try {
    const days = enumerateWeekDays("2026-06-29");
    assert.equal(days.length, 7, "should return exactly 7 days");
    assert.deepEqual(days, [
      "2026-06-29",
      "2026-06-30",
      "2026-07-01",
      "2026-07-02",
      "2026-07-03",
      "2026-07-04",
      "2026-07-05",
    ]);
  } finally {
    if (oldTZ !== undefined) process.env.TZ = oldTZ;
    else delete process.env.TZ;
  }
});

test("enumerateWeekDays handles month boundaries correctly", () => {
  const days = enumerateWeekDays("2026-01-02");
  assert.equal(days.length, 7);
  assert.deepEqual(days, [
    "2026-01-02",
    "2026-01-03",
    "2026-01-04",
    "2026-01-05",
    "2026-01-06",
    "2026-01-07",
    "2026-01-08",
  ]);
});

test("enumerateWeekDays handles year boundaries correctly", () => {
  const days = enumerateWeekDays("2025-12-29");
  assert.equal(days.length, 7);
  assert.deepEqual(days, [
    "2025-12-29",
    "2025-12-30",
    "2025-12-31",
    "2026-01-01",
    "2026-01-02",
    "2026-01-03",
    "2026-01-04",
  ]);
});

test("CAL-MAIN-01 guard: selecting a different week yields column dates matching the fetched window", () => {
  // Today is 2026-07-15 (Wednesday)
  const today = "2026-07-15";
  const todayWindow = weekWindow(today);
  const todayDays = enumerateWeekDays(todayWindow.dateFrom);

  // Select previous week (2026-07-08, Wednesday)
  const prevWeek = "2026-07-08";
  const prevWindow = weekWindow(prevWeek);
  const prevDays = enumerateWeekDays(prevWindow.dateFrom);

  // Select next week (2026-07-22, Wednesday)
  const nextWeek = "2026-07-22";
  const nextWindow = weekWindow(nextWeek);
  const nextDays = enumerateWeekDays(nextWindow.dateFrom);

  // Verify each week's displayed days match its window's dateFrom
  assert.deepEqual(todayDays[0], todayWindow.dateFrom, "today's first column should match its window dateFrom");
  assert.deepEqual(prevDays[0], prevWindow.dateFrom, "prev week's first column should match its window dateFrom");
  assert.deepEqual(nextDays[0], nextWindow.dateFrom, "next week's first column should match its window dateFrom");

  // Verify the weeks are different
  assert.notEqual(todayWindow.dateFrom, prevWindow.dateFrom, "today and prev week should have different dateFrom");
  assert.notEqual(todayWindow.dateFrom, nextWindow.dateFrom, "today and next week should have different dateFrom");
  assert.notEqual(prevWindow.dateFrom, nextWindow.dateFrom, "prev and next week should have different dateFrom");
});
