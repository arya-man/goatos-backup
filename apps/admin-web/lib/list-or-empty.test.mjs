import assert from "node:assert/strict";
import test from "node:test";
import { listOrEmpty } from "./list-or-empty.ts";

test("null and undefined lists become []", () => {
  assert.deepEqual(listOrEmpty(null), []);
  assert.deepEqual(listOrEmpty(undefined), []);
});

test("arrays pass through unchanged", () => {
  const items = [{ work_state: "due", count: 2 }];
  assert.equal(listOrEmpty(items), items);
});

test("action-center payload with null items/counts renders as empty", () => {
  const data = { items: null, counts_by_work_state: null, total_count: 0 };
  const counts = new Map();
  for (const c of listOrEmpty(data.counts_by_work_state)) counts.set(c.work_state, c.count);
  assert.equal(counts.size, 0);
  assert.equal(listOrEmpty(data.items).length, 0);
});
