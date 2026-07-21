import test from "node:test";
import assert from "node:assert/strict";

import { scheduleLoadBuckets } from "./full-vaccine-schedule-load.ts";

function counts(overrides) {
  return {
    total: 0,
    scheduled: 0,
    due: 0,
    inProgress: 0,
    deferred: 0,
    overdue: 0,
    missed: 0,
    accepted: 0,
    proofPending: 0,
    rejected: 0,
    ...overrides,
  };
}

function sum(buckets) {
  return buckets.reduce((acc, b) => acc + b.value, 0);
}

test("segments always sum EXACTLY to counts.total (bar never lies)", () => {
  const cases = [
    counts({ total: 33, scheduled: 7, deferred: 19, overdue: 0 }), // real row: 7 obligations were invisible in the buggy bar
    counts({ total: 187, scheduled: 100, deferred: 87 }),
    counts({ total: 199, scheduled: 100, deferred: 87, overdue: 12 }),
    counts({ total: 100, scheduled: 60, due: 20, inProgress: 20 }),
    counts({ total: 50, accepted: 50 }), // all done: bar is fully "done"
    counts({ total: 40, missed: 10, overdue: 5, deferred: 5, scheduled: 20 }),
    counts({ total: 0 }), // empty
  ];
  for (const c of cases) {
    const { total, buckets } = scheduleLoadBuckets(c, 0);
    assert.equal(sum(buckets), total, `segments must sum to total for ${JSON.stringify(c)}`);
  }
});

test("done = total minus the disjoint open/held/overdue groups", () => {
  const { buckets } = scheduleLoadBuckets(counts({ total: 100, scheduled: 40, deferred: 10, overdue: 5 }), 0);
  const by = Object.fromEntries(buckets.map((b) => [b.key, b.value]));
  assert.equal(by.scheduled, 40);
  assert.equal(by.deferred, 10);
  assert.equal(by.overdue, 5);
  assert.equal(by.done, 45); // 100 - 40 - 10 - 5
});

test("due + in_progress fold into scheduled; missed folds into overdue", () => {
  const { buckets } = scheduleLoadBuckets(counts({ total: 60, scheduled: 10, due: 15, inProgress: 5, overdue: 4, missed: 6 }), 0);
  const by = Object.fromEntries(buckets.map((b) => [b.key, b.value]));
  assert.equal(by.scheduled, 30); // 10 + 15 + 5
  assert.equal(by.overdue, 10); // 4 + 6
  assert.equal(by.done, 20); // 60 - 30 - 0 - 10
});

test("done clamps at 0 and never goes negative if open buckets exceed total", () => {
  // Defensive: proof_pending/rejected overlaps could in theory push open buckets
  // past total; done must not render a negative segment.
  const { total, buckets } = scheduleLoadBuckets(counts({ total: 10, scheduled: 8, deferred: 5, overdue: 3 }), 0);
  const by = Object.fromEntries(buckets.map((b) => [b.key, b.value]));
  assert.equal(by.done, 0);
  assert.ok(sum(buckets) >= total);
});

test("goats (animals) is carried separately from the obligation total", () => {
  const { total, goats } = scheduleLoadBuckets(counts({ total: 187, scheduled: 100, deferred: 87 }), 126);
  assert.equal(total, 187); // vaccine tasks (obligations)
  assert.equal(goats, 126); // distinct goats — a different number, by design
});
