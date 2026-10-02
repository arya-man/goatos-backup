import assert from "node:assert/strict";
import test from "node:test";

import { completeDaySeries } from "./feed-spark-series.ts";

const full = [2232, 2224, 2241, 2232, 2232, 2232, 2233];

test("a partial last day (below 60% of the trailing 7-day median) is dropped", () => {
  assert.deepEqual(completeDaySeries([...full, 1234]), full);
});

test("a missing or zero last day is dropped", () => {
  assert.deepEqual(completeDaySeries([...full, null]), full);
  assert.deepEqual(completeDaySeries([...full, 0]), full);
});

test("a real but lower last day above the 60% line stays", () => {
  assert.deepEqual(completeDaySeries([...full, 1500]), [...full, 1500]);
});

test("a low day in the middle is never trimmed", () => {
  const series = [2232, 2224, 900, 2232, 2232, 2232, 2233, 2230];
  assert.deepEqual(completeDaySeries(series), series);
});

test("fewer than two days gives no sparkline", () => {
  assert.equal(completeDaySeries([2232]), undefined);
  assert.equal(completeDaySeries([null, 1]), undefined);
});

import { latestSheetDay } from "./feed-spark-series.ts";

const day = (feed_day, kg) => ({ feed_day, directed_kg: kg });
const kg = (d) => d.directed_kg;

test("the tiles describe yesterday when yesterday has a complete sheet", () => {
  const days = [day("2026-09-28", 2232), day("2026-09-29", 2230), day("2026-09-30", 2231), day("2026-10-01", 2233), day("2026-10-02", 400)];
  assert.equal(latestSheetDay(days, "2026-10-01", kg)?.feed_day, "2026-10-01");
});

test("no sheet yesterday: the latest complete earlier day, never a bare dash (PR #294 O6)", () => {
  // The live clone stops on 26/09 with a half-issued day; yesterday is 01/10.
  const days = [day("2026-09-20", 2232), day("2026-09-21", 2232), day("2026-09-22", 2233), day("2026-09-23", 1234), day("2026-09-25", 2227), day("2026-09-26", 492)];
  assert.equal(latestSheetDay(days, "2026-10-01", kg)?.feed_day, "2026-09-25");
  assert.equal(latestSheetDay([], "2026-10-01", kg), undefined);
  assert.equal(latestSheetDay([day("2026-09-26", 492)], "2026-10-01", kg)?.feed_day, "2026-09-26");
});
