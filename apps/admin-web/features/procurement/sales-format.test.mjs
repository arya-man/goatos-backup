import assert from "node:assert/strict";
import test from "node:test";

import {
  inr,
  marketLossPerKg,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  resolveFarm,
  salesHref,
} from "./sales-format.ts";

test("inr uses Indian digit grouping with a rupee sign", () => {
  assert.equal(inr(1234567), "₹12,34,567");
  assert.equal(inr(0), "₹0");
  assert.equal(inr(412.5, 2), "₹412.50");
  // Whole rupees round rather than truncate.
  assert.equal(inr(999.6), "₹1,000");
});

test("num groups the Indian way too", () => {
  assert.equal(num(1234567), "12,34,567");
  assert.equal(num(72.25, 1), "72.3");
});

test("resolveFarm accepts only served option keys and falls back to the default", () => {
  const keys = ["all", "CBE", "CPT"];
  assert.equal(resolveFarm("CPT", keys, "all"), "CPT");
  assert.equal(resolveFarm("all", keys, "all"), "all");
  // A hand-edited or stale value never reaches the API.
  assert.equal(resolveFarm("cbe", keys, "all"), "all");
  assert.equal(resolveFarm("DROP TABLE", keys, "all"), "all");
  assert.equal(resolveFarm(undefined, keys, "all"), "all");
});

test("salesHref: a farm switch resets the ledger offset, a pager click keeps the farm", () => {
  const defaults = { farm: "all", limit: 25 };
  // The default view is the bare path — shared links stay canonical.
  assert.equal(salesHref({}, defaults), "/procurement/sales");
  assert.equal(salesHref({ farm: "all" }, defaults), "/procurement/sales");
  // Farm toggle: no offset carried, so a narrowed ledger starts at page one.
  assert.equal(salesHref({ farm: "CBE" }, defaults), "/procurement/sales?farm=CBE");
  // Pager: farm survives the click.
  assert.equal(
    salesHref({ farm: "CBE", offset: 50, limit: 25 }, defaults),
    "/procurement/sales?farm=CBE&offset=50",
  );
  // The default page size is omitted; a chosen one is kept.
  assert.equal(salesHref({ limit: 50 }, defaults), "/procurement/sales?limit=50");
});

test("monthly chart totals are plain sums of the backend components", () => {
  const month = {
    sheep_revenue: 100,
    goat_revenue: 250,
    manure_revenue: 50,
    sheep_count: 3,
    goat_count: 4,
  };
  assert.equal(monthlyRevenueTotal(month), 400);
  assert.equal(monthlyAnimalsTotal(month), 7);
});

test("marketLossPerKg is landed minus market, and null when either side is unrecorded", () => {
  assert.equal(marketLossPerKg(500, 420), 80);
  assert.equal(marketLossPerKg(400, 420), -20);
  assert.equal(marketLossPerKg(null, 420), null);
  assert.equal(marketLossPerKg(500, undefined), null);
});
