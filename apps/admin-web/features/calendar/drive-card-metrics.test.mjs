import assert from "node:assert/strict";
import test from "node:test";

import { driveCoverage, driveCoveragePct } from "./drive-card-metrics.ts";

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

// CDR-R1 regression: a mixed-version response (older backend instance mid-rollout) omits the
// animal fields, so they arrive as undefined -- NOT 0. driveCoverage must then fall back to the
// dose counts that ARE present, not render a false "0 / 0 animals" over a valid drive.
test("driveCoverage uses the animal grain when both animal counts are present", () => {
  const c = driveCoverage(42, 77, 46, 88);
  assert.deepEqual(c, { completed: 42, total: 77, usesAnimals: true });
  assert.equal(driveCoveragePct(c.completed, c.total), 55);
});

test("driveCoverage falls back to dose counts when animal fields are absent (undefined)", () => {
  const c = driveCoverage(undefined, undefined, 46, 77);
  assert.deepEqual(c, { completed: 46, total: 77, usesAnimals: false });
  assert.equal(driveCoveragePct(c.completed, c.total), 60);
});

test("driveCoverage falls back to doses if only one animal field is present (partial/legacy)", () => {
  assert.equal(driveCoverage(10, undefined, 20, 30).usesAnimals, false);
  assert.equal(driveCoverage(undefined, 30, 20, 30).usesAnimals, false);
  assert.equal(driveCoverage(null, null, 5, 9).usesAnimals, false);
});

test("driveCoverage keeps a genuine 0 animal count as animal grain (0/0 != absent)", () => {
  const c = driveCoverage(0, 0, 4, 8);
  assert.equal(c.usesAnimals, true); // a real 0 distinct animals is NOT the legacy-absent case
});
