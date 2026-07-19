import { test } from "node:test";
import assert from "node:assert";
import {
  classifyFeedQuantity,
  isConfiguredZeroQuantity,
  isHiddenOnOperationalSheet,
  isNothingToFeed,
  visibleOperationalFeedItems,
} from "./feed-quantity-state.ts";

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

// -------------------------------------------------------------------------------------------------
// Operational-sheet visibility. The filter that hides "pack 0.000 kg" lines from Feed Direction and
// Feed Packing sits one wrong character away from also hiding BLOCKED lines (both are "no quantity to
// show"), which would turn a loud unfed-shed warning into an invisible one. These tests exist to make
// that regression impossible to land.
// -------------------------------------------------------------------------------------------------

test("BLOCKED SURVIVES THE FILTER — a shed with no authored ration is never hidden", () => {
  // The exact API shape for an unauthored combination: status blocked, NO quantity at all. A naive
  // `if (!quantity_kg) hide` swallows precisely this row.
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "blocked", quantity_kg: null }), false);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "blocked" }), false);
  // Even when a zero-shaped quantity rides along with blocked status, blocked wins and stays visible.
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "blocked", quantity_kg: "0.000" }), false);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "blocked", quantity_kg: "0" }), false);
  // And the contract-impossible resolved-with-no-quantity case classifies as blocked, so it also stays.
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: null }), false);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "  " }), false);

  // Through the list filter, which is what the pages actually call.
  const items = [
    { feed_item: "COFS", status: "resolved", quantity_kg: "18.900" },
    { feed_item: "RGS Concentrate", status: "resolved", quantity_kg: "0.000" },
    { feed_item: "Mineral Mix", status: "blocked", quantity_kg: null },
  ];
  const visible = visibleOperationalFeedItems(items);
  assert.deepStrictEqual(
    visible.map((item) => item.feed_item),
    ["COFS", "Mineral Mix"],
    "the configured zero is dropped and the blocked line is kept",
  );
});

test("only a resolved authored zero is hidden", () => {
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "0.000" }), true);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "0" }), true);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "0.00" }), true);
  // A real quantity, however small, is always shown — never rounded into the hidden bucket.
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "0.001" }), false);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "0.0001" }), false);
  assert.strictEqual(isHiddenOnOperationalSheet({ status: "resolved", quantity_kg: "18.900" }), false);
});

test("visibleOperationalFeedItems preserves order and leaves an all-visible list intact", () => {
  const items = [
    { feed_item: "A", status: "resolved", quantity_kg: "1.000" },
    { feed_item: "B", status: "blocked", quantity_kg: null },
    { feed_item: "C", status: "resolved", quantity_kg: "2.500" },
  ];
  assert.deepStrictEqual(
    visibleOperationalFeedItems(items).map((item) => item.feed_item),
    ["A", "B", "C"],
  );
  assert.deepStrictEqual(visibleOperationalFeedItems([]), []);
});

test("nothing-to-feed is every-item-zero, and a blocked row never counts as one", () => {
  // Castro 1's shape once the zeros go: still has real work, so not a nothing-to-feed session.
  assert.strictEqual(
    isNothingToFeed([
      { status: "resolved", quantity_kg: "18.900" },
      { status: "resolved", quantity_kg: "0.000" },
    ]),
    false,
  );
  // Every line authored as zero — genuinely nothing to feed this session.
  assert.strictEqual(
    isNothingToFeed([
      { status: "resolved", quantity_kg: "0.000" },
      { status: "resolved", quantity_kg: "0" },
    ]),
    true,
  );
  // A blocked line means the session is NOT "nothing to feed" — it is "we do not know what to feed".
  assert.strictEqual(
    isNothingToFeed([
      { status: "resolved", quantity_kg: "0.000" },
      { status: "blocked", quantity_kg: null },
    ]),
    false,
  );
  // No items at all is a different (backend) condition, not this one.
  assert.strictEqual(isNothingToFeed([]), false);
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
