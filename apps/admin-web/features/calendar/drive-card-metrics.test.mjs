import { test } from "node:test";
import assert from "node:assert";
import { driveCoveragePct, driveCoverage, driveStatusChips } from "./drive-card-metrics.ts";

test("driveCoveragePct: rounds half-up", () => {
  // 46/77 = 59.74%, rounds to 60%
  assert.strictEqual(driveCoveragePct(46, 77), 60);
  // 1/2 = 50%, rounds to 50%
  assert.strictEqual(driveCoveragePct(1, 2), 50);
  // 2/3 = 66.67%, rounds to 67%
  assert.strictEqual(driveCoveragePct(2, 3), 67);
  // 0/10 = 0%
  assert.strictEqual(driveCoveragePct(0, 10), 0);
  // 10/10 = 100%
  assert.strictEqual(driveCoveragePct(10, 10), 100);
});

test("driveCoveragePct: handles zero total", () => {
  assert.strictEqual(driveCoveragePct(0, 0), 0);
  assert.strictEqual(driveCoveragePct(5, 0), 0);
});

test("driveCoverage: prefers animal counts", () => {
  const result = driveCoverage(10, 20, 30, 40);
  assert.strictEqual(result.completed, 10);
  assert.strictEqual(result.total, 20);
  assert.strictEqual(result.usesAnimals, true);
});

test("driveCoverage: falls back to dose counts when animals missing", () => {
  const result = driveCoverage(null, undefined, 30, 40);
  assert.strictEqual(result.completed, 30);
  assert.strictEqual(result.total, 40);
  assert.strictEqual(result.usesAnimals, false);
});

test("driveCoverage: falls back when only one animal count missing", () => {
  const result = driveCoverage(10, null, 30, 40);
  assert.strictEqual(result.completed, 30);
  assert.strictEqual(result.total, 40);
  assert.strictEqual(result.usesAnimals, false);
});

test("driveStatusChips: returns nonzero chips in order", () => {
  const result = driveStatusChips({
    completed_count: 5,
    due_count: 3,
    overdue_count: 0,
    deferred_count: 1,
  });
  assert.deepStrictEqual(result, [
    { key: "done", count: 5 },
    { key: "due", count: 3 },
    { key: "deferred", count: 1 },
  ]);
});

test("driveStatusChips: skips zero counts", () => {
  const result = driveStatusChips({
    completed_count: 0,
    due_count: 0,
    overdue_count: 2,
    deferred_count: 0,
  });
  assert.deepStrictEqual(result, [{ key: "overdue", count: 2 }]);
});

test("driveStatusChips: returns empty array when all zero", () => {
  const result = driveStatusChips({
    completed_count: 0,
    due_count: 0,
    overdue_count: 0,
    deferred_count: 0,
  });
  assert.deepStrictEqual(result, []);
});
