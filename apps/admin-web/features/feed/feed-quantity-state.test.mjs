import { test } from "node:test";
import assert from "node:assert";
import { classifyFeedQuantity, isConfiguredZeroQuantity } from "./feed-quantity-state.ts";

// Regression tests for the one rule that must never break: a BLOCKED feed cell (no authored ration,
// shed goes unfed) and an AUTHORED ZERO (fed none of this item on purpose) are opposite states.
//
// These matter more than usual because the live seed contains no blocked cells — both CBE and CPT are
// fully configured — so no amount of clicking around the running app exercises the blocked path.

test("blocked is never read as zero", () => {
  // The exact shape the API sends for an unauthored combination.
  assert.strictEqual(classifyFeedQuantity({ status: "blocked", quantity_kg: null }), "blocked");
  assert.strictEqual(classifyFeedQuantity({ status: "blocked" }), "blocked");
  // Status wins even if a quantity somehow accompanies it — blocked is not a number.
  assert.strictEqual(classifyFeedQuantity({ status: "blocked", quantity_kg: "0.000" }), "blocked");
  assert.strictEqual(classifyFeedQuantity({ status: "blocked", quantity_kg: "12.500" }), "blocked");
});

test("an authored zero is a real configured value, not a gap", () => {
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "0.000" }), "configured_zero");
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "0" }), "configured_zero");
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "0.00" }), "configured_zero");
});

test("a real quantity is planned", () => {
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "18.900" }), "planned");
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "0.001" }), "planned");
  // A small non-zero must not be rounded into the zero bucket.
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "0.0001" }), "planned");
});

test("a resolved cell with no usable quantity fails toward blocked, never toward zero", () => {
  // Contract-impossible, but if the invariant ever breaks, "we do not know" is the safe direction.
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: null }), "blocked");
  assert.strictEqual(classifyFeedQuantity({ status: "resolved" }), "blocked");
  assert.strictEqual(classifyFeedQuantity({ status: "resolved", quantity_kg: "  " }), "blocked");
});

test("isConfiguredZeroQuantity decides on string shape, without parsing", () => {
  assert.strictEqual(isConfiguredZeroQuantity("0.000"), true);
  assert.strictEqual(isConfiguredZeroQuantity("0"), true);
  assert.strictEqual(isConfiguredZeroQuantity(" 0.00 "), true);
  assert.strictEqual(isConfiguredZeroQuantity("0.001"), false);
  assert.strictEqual(isConfiguredZeroQuantity("10.000"), false);
  // Null/undefined are the ABSENCE of a quantity — never zero.
  assert.strictEqual(isConfiguredZeroQuantity(null), false);
  assert.strictEqual(isConfiguredZeroQuantity(undefined), false);
  assert.strictEqual(isConfiguredZeroQuantity(""), false);
});
