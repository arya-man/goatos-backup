import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

import {
  humanDate,
  inr,
  inrCompact,
  marketLossPerKg,
  monthLabel,
  monthlyAnimalRevenueTotal,
  monthlyAnimalsTotal,
  monthlyRevenueTotal,
  num,
  numCompact,
  numCompactWhole,
  resolveFarm,
  trimEmptyMonthlyStart,
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

test("numCompactWhole rounds chart labels without decimal units", () => {
  assert.equal(numCompactWhole(94_800), "95k");
  assert.equal(numCompactWhole(10_200), "10k");
  assert.equal(numCompactWhole(412.49), "412");
  assert.equal(numCompactWhole(412.5), "413");
  assert.equal(numCompactWhole(-1_499), "−1k");
});

test("monthLabel and humanDate turn ISO values into farm-readable dates", () => {
  // A MONTH heading has no day component, so it is not a date and keeps its own form — but with
  // the year in full, because "Apr 25" reads as April 25th beside numeric dates.
  assert.equal(monthLabel("2025-04"), "Apr 2025");
  assert.equal(monthLabel("2026-12"), "Dec 2026");
  assert.equal(monthLabel("garbage"), "garbage");
  // A DATE is DD/MM/YYYY everywhere (maintainer decision 2026-09-10).
  assert.equal(humanDate("2025-04-15"), "15/04/2025");
  assert.equal(humanDate("2026-08-11"), "11/08/2026");
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

test("monthly charts trim zero-only starts per metric and keep the current tail", () => {
  const months = [
    { month: "2025-04", manure_kg: 0 },
    { month: "2026-02", manure_kg: 0 },
    { month: "2026-03", manure_kg: 46800 },
    { month: "2026-04", manure_kg: 94750 },
    { month: "2026-09", manure_kg: 0 },
    { month: "2026-10", manure_kg: 1200 },
    { month: "2026-11", manure_kg: 0 },
  ];

  assert.deepEqual(
    trimEmptyMonthlyStart(months, (month) => month.manure_kg).map((month) => month.month),
    ["2026-03", "2026-04", "2026-09", "2026-10", "2026-11"],
  );
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

test("sales chart bar labels stay whole and suffix-free", () => {
  // The monthly and price-band charts live on Sold since the 2026-09-11 split.
  const salesSource = readFileSync(new URL("./sales-sold.tsx", import.meta.url), "utf8");
  const loadwiseSource = readFileSync(new URL("./loadwise-section.tsx", import.meta.url), "utf8");
  assert.match(salesSource, /display: numCompactWhole\(month\.manure_kg\)/);
  assert.match(salesSource, /display: inr\(Math\.round\(band\.avg_price_per_kg\)\)/);
  assert.doesNotMatch(salesSource, /display: `\$\{inr\(Math\.round\(band\.avg_price_per_kg\)\)\} \$\{perKgSuffix\}`/);
  assert.match(loadwiseSource, /barLabels:\s*\[\s*load\.avg_purchase_weight_kg == null \? null : numCompactWhole\(load\.avg_purchase_weight_kg\)/s);
  assert.match(loadwiseSource, /barLabels:\s*\[\s*load\.landed_price_per_kg == null \? null : inr\(Math\.round\(load\.landed_price_per_kg\)\)/s);
  assert.match(loadwiseSource, /barLabels:\s*\[\s*load\.fattening_days == null \? null : numCompactWhole\(load\.fattening_days\)/s);
});

test("loadwise Weighs now stays visible for part-sold loads", () => {
  const source = readFileSync(new URL("./loadwise-section.tsx", import.meta.url), "utf8");
  assert.match(source, /if \(load\.remaining <= 0 \|\| !load\.load_ref\) return null;/);
  assert.doesNotMatch(source, /if \(load\.avg_sale_weight_kg != null \|\| !load\.load_ref\) return null;/);
  assert.doesNotMatch(source, /if \(load\.avg_sale_weight_kg != null\) return null;/);
});

test("the Over 35 kg card asks for six weeks, and the backend floors it", () => {
  // SIX WEEKS is the reader's window (maintainer, 2026-09-04). The BACKEND holds the floor -- the
  // first dense weighing day -- so the two never disagree about where the count starts, and the
  // page does not carry a second copy of a date the server already owns. The card lives on
  // Farm value since the 2026-09-11 split.
  const source = readFileSync(new URL("./sales-farm-value.tsx", import.meta.url), "utf8");
  // One window for the page's first count and the card's in-place re-count (over35-actions.ts).
  const windowSource = readFileSync(new URL("./over35-window.ts", import.meta.url), "utf8");
  assert.match(windowSource, /export const OVER35_WINDOW_DAYS = 42;/);
  const actionSource = readFileSync(new URL("./over35-actions.ts", import.meta.url), "utf8");
  assert.match(actionSource, /from: istDayPlus\(to, -OVER35_WINDOW_DAYS\)/);
  assert.match(source, /const over35From = istDayPlus\(over35To, -OVER35_WINDOW_DAYS\);/);
  // No anchor date lives on this page: a client-side floor would drift from the server's.
  assert.doesNotMatch(source, /2026-08-0\d/);
});

test("farm value cards render the backend valuation contract", () => {
  const source = readFileSync(new URL("./sales-farm-value.tsx", import.meta.url), "utf8");
  assert.match(source, /overview\.farm_valuation\.total_value_rupees/);
  assert.match(source, /overview\.farm_valuation\.total_meat_kg/);
  assert.match(source, /overview\.farm_valuation\.total_animals/);
  assert.match(source, /overview\.farm_valuation\.valued_animals/);
  assert.match(source, /overview\.farm_valuation\.excluded_animals/);
  assert.match(source, /overview\.farm_valuation\.not_valued/);
  assert.match(source, /overview\.farm_valuation\.buckets\.map/);
  assert.doesNotMatch(source, /farm_valuation\.buckets\.reduce/);
  // The sex split (maintainer request 2026-09-11) renders the three backend counts verbatim on
  // the fattening and kid cards only, and derives none of them.
  assert.match(source, /SEX_SPLIT_BUCKETS = new Set\(\["fattening", "K0", "K1", "K2", "K3"\]\)/);
  assert.match(source, /bucket\.male_count/);
  assert.match(source, /bucket\.female_count/);
  // "don't show missing" (maintainer, 2026-09-11): the unrecorded remainder is not printed.
  assert.doesNotMatch(source, /sex_missing_count|value\.sex\.missing/);
  // "remove how many we weighed count" (maintainer, 2026-09-11): the fattening card no longer
  // prints the weighed-animal count behind its average.
  assert.doesNotMatch(source, /weighed_animals|value\.weighed"/);
  assert.doesNotMatch(source, /animal_count\s*-\s*bucket\.(male|female)_count/);
});

// The retired board's blocks are divided, never duplicated: Sold carries no valuation figure,
// Farm value no chart or ledger, and the ledger closes the Sold page ("keep it at last").
test("Sold ends with the deals ledger and Farm value carries no sold block", () => {
  const sold = readFileSync(new URL("./sales-sold.tsx", import.meta.url), "utf8");
  const farmValue = readFileSync(new URL("./sales-farm-value.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(sold, /farm_valuation|getShedWeights/);
  assert.match(sold, /listSalesDeals\(/);
  assert.ok(sold.lastIndexOf("sales-deals-table") > sold.lastIndexOf("section.buyers.title"), "the ledger renders after the last sold block");
  assert.doesNotMatch(farmValue, /listSalesDeals|MonthColumns|HBarList|sales-deals-table/);
  assert.ok(!existsSync(new URL("./sales.tsx", import.meta.url)), "the retired board component must not come back");
});

// Every Sales read page renders the farm chips (which write the shell's `park`), so the shell must
// hide its top-bar park selector on each (PR 238 review; one park filter across Sales, 2026-09-25):
// the retired /sales path alone no longer matches any page that renders.
test("the shell hides its park selector on every Sales page that carries the farm chips", () => {
  const shell = readFileSync(new URL("../../components/mesha-shell.tsx", import.meta.url), "utf8");
  const owning = shell.match(/const PAGES_OWNING_PARK_SCOPE = \[([\s\S]*?)\];/);
  assert.ok(owning, "PAGES_OWNING_PARK_SCOPE must still exist");
  for (const path of ["/sales/sold", "/sales/farm-value", "/sales/buyer-analytics", "/sales/loads", "/sales/farm-born"]) {
    assert.match(owning[1], new RegExp(`"${path.replaceAll("/", "\\/")}"`), `${path} must hide the top-bar park`);
  }
  assert.doesNotMatch(owning[1], /"\/sales"[,\s]/, "the retired board path is not a page that renders");
  // The list the shell actually hides the top bar on SPREADS that one, so the two cannot drift.
  const lock = shell.match(/const PAGES_WITH_LOCAL_OR_NO_PARK_SCOPE = \[([\s\S]*?)\];/);
  assert.ok(lock, "PAGES_WITH_LOCAL_OR_NO_PARK_SCOPE must still exist");
  assert.match(lock[1], /\.\.\.PAGES_OWNING_PARK_SCOPE/);
});

test("farm value copy keys have rollout fallbacks", () => {
  const source = readFileSync(new URL("../../lib/admin-ui-contract.ts", import.meta.url), "utf8");
  assert.match(source, /"sales-farm-value":\s*{/);
  for (const key of [
    "kpi.farm_value",
    "kpi.farm_value.detail",
    "kpi.total_meat",
    "kpi.total_meat.detail",
    "value.excluded_animals",
    "value.live_animals",
    "value.not_valued",
    "value.valued_animals",
    "value.weighed",
  ]) {
    assert.match(source, new RegExp(`"${key.replaceAll(".", "\\.")}"`));
  }
});

test("monthly charts include feed, counted goods and custom animals through backend totals", () => {
 const month={month:"2026-09",sheep_revenue:0,goat_revenue:0,manure_revenue:0,sheep_count:0,goat_count:0,revenue:3000,live_revenue:1000,animals:2,feed_revenue:1000,other_revenue:1000};
 assert.equal(monthlyRevenueTotal(month),3000);
 assert.equal(monthlyAnimalsTotal(month),2);
 assert.equal(monthlyAnimalRevenueTotal(month),1000);
 assert.equal(trimEmptyMonthlyStart([month],monthlyRevenueTotal).length,1);
 const zero={...month,revenue:0,live_revenue:0,animals:0,sheep_revenue:999,sheep_count:99};
 assert.equal(monthlyRevenueTotal(zero),0);
 assert.equal(monthlyAnimalsTotal(zero),0);
 assert.equal(monthlyAnimalRevenueTotal(zero),0);
 const {revenue,live_revenue,animals,...legacy}=month;
 assert.equal(monthlyRevenueTotal(legacy),2000);
});

test("the planned sale date shows only when it differs from the sale date", async () => {
  const { plannedSaleDateIfDifferent } = await import("./sales-format.ts");
  assert.equal(plannedSaleDateIfDifferent({ sale_date: "2026-09-25", planned_sale_date: "2026-09-20" }), "2026-09-20");
  assert.equal(plannedSaleDateIfDifferent({ sale_date: "2026-09-25", planned_sale_date: "2026-09-25" }), null);
  assert.equal(plannedSaleDateIfDifferent({ sale_date: "2026-09-25", planned_sale_date: null }), null);
  assert.equal(plannedSaleDateIfDifferent({ sale_date: "2026-09-25" }), null);
  assert.equal(plannedSaleDateIfDifferent({ sale_date: "2026-09-25", planned_sale_date: "  " }), null);
});
