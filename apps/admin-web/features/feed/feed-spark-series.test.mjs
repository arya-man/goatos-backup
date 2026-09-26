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
