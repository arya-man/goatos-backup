import assert from "node:assert/strict";
import test from "node:test";

import { driveCoveragePct } from "./drive-card-metrics.ts";

// CDR-005 regression: the coverage ring rounds half-up (Math.round), not truncates.
// This is the exact case the Android card got wrong before a6765ac5 (integer division
// gave 59; web + Android now both give 60).
test("driveCoveragePct rounds 46/77 up to 60 (was 59 via truncation)", () => {
  assert.equal(driveCoveragePct(46, 77), 60);
});

test("driveCoveragePct rounds to nearest, both directions", () => {
  assert.equal(driveCoveragePct(1, 3), 33); // 33.33 -> 33
  assert.equal(driveCoveragePct(2, 3), 67); // 66.66 -> 67
});

test("driveCoveragePct handles the 0% and 100% edges", () => {
  assert.equal(driveCoveragePct(0, 55), 0);
  assert.equal(driveCoveragePct(55, 55), 100);
});

test("driveCoveragePct is 0 when there are no animals (no divide-by-zero)", () => {
  assert.equal(driveCoveragePct(0, 0), 0);
  assert.equal(driveCoveragePct(5, 0), 0);
});
