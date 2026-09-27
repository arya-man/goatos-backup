// guard: kpi-month-trend (REVIEW-11). A month tile compares the last two COMPLETE months.
import assert from "node:assert/strict";
import test from "node:test";

import { completeMonthPercent, lastStepPercent, monthIsPartial, sevenDayPercent } from "./kpi-trend.ts";

test("a running month is partial; the last day of a month is not", () => {
  assert.equal(monthIsPartial("2026-09-27"), true);
  assert.equal(monthIsPartial("2026-09-30"), false);
  assert.equal(monthIsPartial("2026-02-28"), false);
  assert.equal(monthIsPartial("2028-02-28"), true); // leap year
  assert.equal(monthIsPartial(undefined), true);
});

test("the partial last month is left out of the month-on-month change", () => {
  // Jul 10, Aug 20, Sep (27 days in) 3: the honest change is Aug vs Jul (+100%), not Sep vs Aug.
  assert.equal(completeMonthPercent([10, 20, 3], "2026-09-27"), 100);
  assert.equal(completeMonthPercent([10, 20, 30], "2026-09-30"), 50);
  assert.equal(completeMonthPercent([20, 3], "2026-09-27"), null, "one complete month is no comparison");
});

test("step and seven-day helpers", () => {
  assert.equal(lastStepPercent([1, 2]), 100);
  assert.equal(lastStepPercent([0, 2]), null);
  assert.equal(sevenDayPercent(Array(13).fill(1)), null);
  assert.equal(sevenDayPercent([...Array(7).fill(1), ...Array(7).fill(2)]), 100);
});

test("the month tiles use completeMonthPercent, never a raw last-two-points step", async () => {
  const { readFileSync } = await import("node:fs");
  for (const rel of ["../features/counts/herd-analytics.tsx", "../features/counts/mortality.tsx", "../features/health/health-analytics.tsx"]) {
    const src = readFileSync(new URL(rel, import.meta.url), "utf8");
    assert.match(src, /completeMonthPercent\(/, rel);
    assert.doesNotMatch(src, /lastStepPercent\(/, `${rel}: raw last-step percent on a monthly series`);
  }
});
