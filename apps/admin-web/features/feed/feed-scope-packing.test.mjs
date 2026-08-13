import assert from "node:assert/strict";
import test from "node:test";

// Feed PACKING browses by the PACKING day (maintainer decision 2026-07-27), not the feed day. The
// picker defaults to today and is bound to [today - 30, today]: it CANNOT select a future packing day
// (tomorrow's run does not exist until tomorrow morning), and it reaches 30 days back so previously
// packed sheets stay reviewable. The backend is asked for feed day = packing day + 1.
//
// `clampPackingDay` and the packing-day+1 map are re-implemented here (as the sibling suites do for
// `istDayPlus`/`clampFeedDay`) because feed-scope.ts is server-only TypeScript and this suite runs
// under plain `node --test`. The window bounds are passed in explicitly so the assertions do not
// depend on the wall clock. The load-bearing behavior is the string clamp on YYYY-MM-DD business
// dates and the +1 map from packing day to feed day.
function istDayPlus(day, days) {
  const [year, month, date] = day.split("-").map(Number);
  if (!Number.isFinite(year) || !Number.isFinite(month) || !Number.isFinite(date)) return day;
  const shifted = new Date(Date.UTC(year, month - 1, date + days));
  const pad = (value) => String(value).padStart(2, "0");
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

function clampPackingDay(requested, min, max, fallback) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(requested)) return fallback;
  if (requested < min) return min;
  if (requested > max) return max;
  return requested;
}

const MAX = "2026-07-27"; // today (the packing day cap)
const MIN = "2026-06-27"; // today - 30
const FALLBACK = MAX; // default packing day is today

test("today passes through and is the maximum packing day", () => {
  assert.equal(clampPackingDay("2026-07-27", MIN, MAX, FALLBACK), "2026-07-27");
});

test("a past packing day inside the 30-day window passes through unchanged", () => {
  assert.equal(clampPackingDay("2026-07-17", MIN, MAX, FALLBACK), "2026-07-17");
  assert.equal(clampPackingDay("2026-07-07", MIN, MAX, FALLBACK), "2026-07-07");
});

test("a future packing day clamps down to today — tomorrow's run does not exist yet", () => {
  assert.equal(clampPackingDay("2026-07-28", MIN, MAX, FALLBACK), "2026-07-27");
  assert.equal(clampPackingDay("2026-12-31", MIN, MAX, FALLBACK), "2026-07-27");
});

test("a day older than the history window clamps up to the window floor", () => {
  assert.equal(clampPackingDay("2026-05-01", MIN, MAX, FALLBACK), "2026-06-27");
  assert.equal(clampPackingDay("2025-01-01", MIN, MAX, FALLBACK), "2026-06-27");
});

test("an unparseable value falls back to the default packing day (today)", () => {
  assert.equal(clampPackingDay("", MIN, MAX, FALLBACK), FALLBACK);
  assert.equal(clampPackingDay("not-a-date", MIN, MAX, FALLBACK), FALLBACK);
  assert.equal(clampPackingDay("2026-7-1", MIN, MAX, FALLBACK), FALLBACK);
});

test("the feed day sent to the backend is the packing day + 1", () => {
  assert.equal(istDayPlus("2026-07-27", 1), "2026-07-28"); // today's pack -> tomorrow's feed
  assert.equal(istDayPlus("2026-07-17", 1), "2026-07-18"); // a past pack -> its own next day
  assert.equal(istDayPlus("2026-07-31", 1), "2026-08-01"); // month rollover
});
