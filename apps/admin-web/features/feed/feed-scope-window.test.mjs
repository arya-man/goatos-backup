import assert from "node:assert/strict";
import test from "node:test";

// The feed-day window is [today, tomorrow] in Asia/Kolkata. A sheet for any other day is fabricated
// (it silently freezes today's herd), so the picker is bound to the window AND a stale URL is clamped
// into it rather than requesting a beyond-horizon/past day.
//
// `clampFeedDay` is re-implemented here (as the suite already does for `istDayPlus`) because
// feed-scope.ts is server-only TypeScript and this suite runs under plain `node --test`. The window
// bounds are passed in explicitly so the assertions are deterministic and do not depend on the wall
// clock. The load-bearing behavior is the string clamp on YYYY-MM-DD business dates.
function clampFeedDay(requested, min, max, fallback) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(requested)) return fallback;
  if (requested < min) return min;
  if (requested > max) return max;
  return requested;
}

const MIN = "2026-07-20"; // today
const MAX = "2026-07-21"; // tomorrow
const FALLBACK = MAX;

test("today and tomorrow pass through unchanged", () => {
  assert.equal(clampFeedDay("2026-07-20", MIN, MAX, FALLBACK), "2026-07-20");
  assert.equal(clampFeedDay("2026-07-21", MIN, MAX, FALLBACK), "2026-07-21");
});

test("a far-future stale bookmark clamps down to tomorrow, never requesting a fabricated day", () => {
  assert.equal(clampFeedDay("2026-08-15", MIN, MAX, FALLBACK), "2026-07-21");
  assert.equal(clampFeedDay("2027-01-01", MIN, MAX, FALLBACK), "2026-07-21");
});

test("a past day clamps up to today", () => {
  assert.equal(clampFeedDay("2026-07-19", MIN, MAX, FALLBACK), "2026-07-20");
  assert.equal(clampFeedDay("2025-01-01", MIN, MAX, FALLBACK), "2026-07-20");
});

test("an unparseable value falls back to the default feed day (tomorrow)", () => {
  assert.equal(clampFeedDay("", MIN, MAX, FALLBACK), FALLBACK);
  assert.equal(clampFeedDay("not-a-date", MIN, MAX, FALLBACK), FALLBACK);
  assert.equal(clampFeedDay("2026-7-1", MIN, MAX, FALLBACK), FALLBACK);
});
