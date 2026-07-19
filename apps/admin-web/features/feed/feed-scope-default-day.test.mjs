import assert from "node:assert/strict";
import test from "node:test";

// The feed day default is business-critical, not cosmetic: a direction is issued today FOR
// TOMORROW'S feed. Defaulting to today opens the sheet on a day that was packed yesterday and is
// already at the shed, so every correction made there applies to nothing.
//
// `istDayPlus` is re-implemented here rather than imported because lib/format.ts is TypeScript and
// this suite runs under plain `node --test`. The assertions below are the ones that actually catch
// bugs: month, year and leap-day rollover, which naive string or +86400000 arithmetic gets wrong.
function istDayPlus(day, days) {
  const [year, month, date] = day.split("-").map(Number);
  if (!Number.isFinite(year) || !Number.isFinite(month) || !Number.isFinite(date)) return day;
  const shifted = new Date(Date.UTC(year, month - 1, date + days));
  const pad = (value) => String(value).padStart(2, "0");
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

test("advances an ordinary day", () => {
  assert.equal(istDayPlus("2026-07-19", 1), "2026-07-20");
});

test("rolls over month end", () => {
  assert.equal(istDayPlus("2026-07-31", 1), "2026-08-01");
  assert.equal(istDayPlus("2026-04-30", 1), "2026-05-01");
});

test("rolls over year end", () => {
  assert.equal(istDayPlus("2026-12-31", 1), "2027-01-01");
});

test("handles February in a leap and a non-leap year", () => {
  assert.equal(istDayPlus("2028-02-28", 1), "2028-02-29");
  assert.equal(istDayPlus("2026-02-28", 1), "2026-03-01");
});

test("keeps zero-padding so the value stays a valid date input", () => {
  const result = istDayPlus("2026-01-08", 1);
  assert.equal(result, "2026-01-09");
  assert.match(result, /^\d{4}-\d{2}-\d{2}$/);
});

test("returns the input unchanged when it is not a date", () => {
  assert.equal(istDayPlus("", 1), "");
  assert.equal(istDayPlus("not-a-date", 1), "not-a-date");
});
