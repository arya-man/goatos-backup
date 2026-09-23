// Both halves, again: each rule must catch its bug, and must stay quiet on the answers
// production actually gives. The quiet half is not decoration — the first draft of this
// file fired on 20 of 58 real endpoints and every one of those was a correct page.
import assert from "node:assert/strict";
import test from "node:test";
import { selfConsistencyFindings } from "./self-consistency.mjs";

const rules = (payload) => selfConsistencyFindings(payload, { pageName: "The Weights page" }).map((f) => f.rule);

test("a share that is not a possible share is caught on any screen", () => {
  assert.deepEqual(rules({ summary: { adherence_percent: 999 } }), ["impossible-percentage"]);
  assert.deepEqual(rules({ summary: { adherence_percent: -5 } }), ["impossible-percentage"]);
  assert.deepEqual(rules({ summary: { adherence_percent: 80 } }), []);
});

test("a change is not a share, and is not held to nought-to-a-hundred", () => {
  // Herd signals really does report one pen's motion up 272% and another's down 61%.
  assert.deepEqual(rules({ items: [{ group_motion_delta_pct: 272.3 }, { group_motion_delta_pct: -61.4 }] }), []);
  assert.deepEqual(rules({ summary: { growth_percent: 140 } }), []);
});

test("nothing is counted a negative number of times", () => {
  assert.deepEqual(rules({ summary: { animals_count: -4 } }), ["negative-count"]);
  assert.deepEqual(rules({ summary: { animals_count: 0 } }), []);
  // A gain or a delta may legitimately be negative: a pen can lose weight.
  assert.deepEqual(rules({ summary: { delta_count: -4 } }), []);
});

test("an average is its own total over its own number of things", () => {
  assert.deepEqual(rules({ summary: { total_weight_kg: 5742, animals_weighed: 220, average_weight_kg: 261 } }), ["average-not-its-own-total"]);
  assert.deepEqual(rules({ summary: { total_weight_kg: 5742, animals_weighed: 220, average_weight_kg: 26.1 } }), []);
  assert.deepEqual(rules({ summary: { total_weight_kg: 0, animals_weighed: 0, average_weight_kg: 0 } }), []);
});

test("a stated total agrees with the list printed under it", () => {
  assert.deepEqual(rules({ operators: [{ a: 1 }, { a: 2 }], operators_total: 7 }), ["list-disagrees-with-its-total"]);
  assert.deepEqual(rules({ operators: [{ a: 1 }, { a: 2 }], operators_total: 2 }), []);
  // A list the answer says it cut short at a cap is not disagreeing with itself.
  assert.deepEqual(rules({ operators: [{ a: 1 }], operators_total: 7, operators_truncated: true }), []);
});

test("an empty answer is not a disagreement", () => {
  assert.deepEqual(rules({}), []);
  assert.deepEqual(rules({ rows: [], summary: {} }), []);
});
