import assert from "node:assert/strict";
import test from "node:test";

import { historyWindow, monthWindow, weekWindow } from "./calendar-window.ts";

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
