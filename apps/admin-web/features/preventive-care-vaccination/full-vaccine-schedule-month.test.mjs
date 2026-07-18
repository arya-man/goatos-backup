import test from "node:test";
import assert from "node:assert/strict";

import { isInScheduleMonth } from "./full-vaccine-schedule-month.ts";

test("full schedule month checks use IST calendar month", () => {
  assert.equal(isInScheduleMonth("2026-08-01T00:00:00+05:30", 2026, 8), true);
  assert.equal(isInScheduleMonth("2026-08-01T00:00:00+05:30", 2026, 7), false);
});
