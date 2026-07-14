import assert from "node:assert/strict";
import test from "node:test";

import { enumerateWeekDays, historyWindow, monthWindow, weekWindow } from "./calendar-window.ts";

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
