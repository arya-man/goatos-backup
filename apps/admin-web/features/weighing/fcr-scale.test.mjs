import assert from "node:assert/strict";
import test from "node:test";
import { fcrAxisCeiling, isOffScale } from "./fcr-scale.ts";

test("one runaway pen does not set the FCR axis for every other pen", () => {
  const pens = [6.2, 7.1, 8.13, 7.4, 9.0, 6.8, 7.7, 8.9, 468.45];
  const ceiling = fcrAxisCeiling(pens, 7.5);
  assert.ok(ceiling !== undefined, "the outlier clamps the axis");
  assert.ok(ceiling < 30, `the axis stays near the bulk of the pens, got ${ceiling}`);
  assert.ok(ceiling >= 9 * 1.5 - 1e-9 || ceiling >= 7.5 * 1.5, "every ordinary pen and break-even still fit");
  assert.equal(isOffScale(468.45, ceiling), true);
  assert.equal(isOffScale(9.0, ceiling), false);
});

test("pens without an outlier keep the chart's own scale", () => {
  assert.equal(fcrAxisCeiling([6.2, 7.1, 8.1, 9.4], 7.5), undefined);
  assert.equal(fcrAxisCeiling([5, 500], 7), undefined, "two pens are too few to call one an outlier");
  assert.equal(isOffScale(500, undefined), false);
});

test("break-even always fits under the ceiling", () => {
  const ceiling = fcrAxisCeiling([2, 2.1, 2.2, 2.3, 300], 20);
  assert.ok(ceiling !== undefined && ceiling >= 20);
});
