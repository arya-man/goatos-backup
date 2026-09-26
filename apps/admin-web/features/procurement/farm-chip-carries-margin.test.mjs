import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { liveQueryHref, salesPageHref, salesParkPatch } from "./sales-park-scope.ts";

// Farm value, 2026-09-25: Apply on the Over 35 kg card moves the URL in place
// (replaceLocalOverlayUrl, no navigation), so the farm chips the SERVER rendered still carried the
// old query -- switching farm dropped the applied margin back to the default.
const CPT = "22222222-2222-4222-8222-222222222222";
const serverSp = { scope_mode: "company" };
const liveSearch = "?scope_mode=company&sale_ready_tolerance_g=1500";

test("a chip built from the server's params loses a margin applied in place (the defect)", () => {
  const stale = new URL(salesPageHref("/sales/farm-value", serverSp, salesParkPatch(CPT)), "http://x");
  assert.equal(stale.searchParams.get("sale_ready_tolerance_g"), null);
});

test("a chip built from the live URL carries the applied margin and switches the farm", () => {
  const href = new URL(liveQueryHref("/sales/farm-value", liveSearch, salesParkPatch(CPT)), "http://x");
  assert.equal(href.searchParams.get("sale_ready_tolerance_g"), "1500");
  assert.equal(href.searchParams.get("park"), CPT);
  assert.equal(href.searchParams.get("scope_mode"), "park");
  const all = new URL(liveQueryHref("/sales/farm-value", `?scope_mode=park&park=${CPT}&sale_ready_tolerance_g=1500`, salesParkPatch("")), "http://x");
  assert.equal(all.searchParams.get("park"), null);
  assert.equal(all.searchParams.get("sale_ready_tolerance_g"), "1500");
});

test("the farm chips render through the live-URL link, which follows in-place URL moves", () => {
  const chrome = readFileSync(new URL("./sales-chrome.tsx", import.meta.url), "utf8");
  const link = readFileSync(new URL("./live-query-link.tsx", import.meta.url), "utf8");
  // MUI redesign: the chips are the template pill tabs (LiveQueryTabs), same live-URL rule.
  assert.match(chrome, /<LiveQueryTabs[\s\S]*?patch: \{ \.\.\.cleared, \.\.\.salesParkPatch\(id\) \}/);
  assert.match(link, /LOCAL_OVERLAY_URL_CHANGE_EVENT/);
  assert.match(link, /liveQueryHref\(pagePath, search, patch\)/);
});

// MUI redesign: the grouped columns are the template ApexCharts chart; its legend is ChartLegends.
test("the striped series' key is striped too, against the panel colour", () => {
  const legend = readFileSync(new URL("../../components/series-charts.tsx", import.meta.url), "utf8");
  assert.match(legend, /repeating-linear-gradient\(135deg, \$\{e\.colorVar\} 0 2px, var\(--palette-background-paper\) 2px 4px\)/);
});

test("every swatch (legend and bar) carries the okHatch stripe, not a fifth solid colour", () => {
  const chart = readFileSync(new URL("../../components/grouped-columns.tsx", import.meta.url), "utf8");
  assert.match(chart, /hatched: s\.tone === "okHatch"/);
  assert.match(chart, /s\.tone === "okHatch" \? "pattern" : "solid"/);
});
