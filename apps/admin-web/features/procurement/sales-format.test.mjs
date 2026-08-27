import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  humanDate,
  inr,
  inrCompact,
  marketLossPerKg,
  monthLabel,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  numCompact,
  resolveFarm,
  salesHref,
} from "./sales-format.ts";

test("inrCompact speaks lakh and crore for chart labels", () => {
  assert.equal(inrCompact(7160979), "₹71.6L");
  assert.equal(inrCompact(19_40_000), "₹19.4L");
  assert.equal(inrCompact(2_00_00_000), "₹2Cr");
  assert.equal(inrCompact(42500), "₹42.5k");
  assert.equal(inrCompact(900), "₹900");
  assert.equal(inrCompact(0), "₹0");
});

test("numCompact shortens counts and kg the same way", () => {
  assert.equal(numCompact(219305), "2.2L");
  assert.equal(numCompact(12410), "12.4k");
  assert.equal(numCompact(528), "528");
});

test("monthLabel and humanDate turn ISO values into farm-readable dates", () => {
  assert.equal(monthLabel("2025-04"), "Apr 25");
  assert.equal(monthLabel("2026-12"), "Dec 26");
  assert.equal(monthLabel("garbage"), "garbage");
  assert.equal(humanDate("2025-04-15"), "15 Apr 2025");
  assert.equal(humanDate("2026-08-11"), "11 Aug 2026");
  assert.equal(humanDate("not-a-date"), "not-a-date");
});

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
  assert.equal(salesHref({}, defaults), "/sales");
  assert.equal(salesHref({ farm: "all" }, defaults), "/sales");
  // Farm toggle: no offset carried, so a narrowed ledger starts at page one.
  assert.equal(salesHref({ farm: "CBE" }, defaults), "/sales?farm=CBE");
  // Pager: farm survives the click.
  assert.equal(
    salesHref({ farm: "CBE", offset: 50, limit: 25 }, defaults),
    "/sales?farm=CBE&offset=50",
  );
  // The default page size is omitted; a chosen one is kept.
  assert.equal(salesHref({ limit: 50 }, defaults), "/sales?limit=50");
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

test("record-sale vendor picker discloses a capped register instead of treating it as complete", () => {
  const source = readFileSync(new URL("./sales-record-drawer.tsx", import.meta.url), "utf8");
  assert.match(source, /vendorOptions\?\.truncated === true/);
  assert.match(source, /hint\.vendor_truncated/);
  assert.match(source, /vendorsTruncated[\s\S]*?canPickVendor/);
});
