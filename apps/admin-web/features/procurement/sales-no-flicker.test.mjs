import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

// No whole-page reload and no flicker on the Sales pages (maintainer request 2026-09-25).
const read = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("no one-shape skeleton replaces every Sales page on every hop", () => {
  // sales/loading.tsx was ONE fixed skeleton for nine different pages: every chip, pager and
  // sidebar hop flashed the wrong shape. Without it a soft navigation keeps the current page on
  // screen until the next one is ready.
  assert.ok(!existsSync(new URL("../../app/(admin)/sales/loading.tsx", import.meta.url)));
});

test("Market analytics question and city chips are view-only client state", () => {
  const trend = read("./market-trend-section.tsx");
  assert.match(trend, /^"use client";/);
  assert.doesNotMatch(trend, /<Link\b/, "a view-only pick must never navigate");
  assert.match(trend, /replaceLocalOverlayUrl\(/);
  // MUI redesign: the chips are template tabs driven by client state (onChange / onClick, no href).
  assert.match(trend, /onChange=\{\(_event, next: string\) => pick\(next, city\)\}/);
  assert.match(trend, /onClick: \(\) => pick\(question, tab\.value\)/);
  const page = read("./market-analytics.tsx");
  assert.doesNotMatch(page, /hrefWithQuery\(sp, \{ question:/);
  assert.doesNotMatch(page, /hrefWithQuery\(sp, \{ city:/);
});

test("Farm born stages its filters and never dims the whole page", () => {
  const page = read("./sales-farm-born.tsx");
  assert.match(page, /<WorklistFilters[\s\S]*?deferApply[\s\S]*?holdChildren=\{false\}/);
  const bar = read("../../components/worklist-filters.tsx");
  assert.match(bar, /className=\{busy && holdChildren \? "wfbusy" : undefined\}/);
});

test("the Over 35 kg margin re-counts its own card in place", () => {
  const control = read("./sales-ready-tolerance-control.tsx");
  assert.doesNotMatch(control, /router\.replace|useRouter/, "Apply must not re-render the whole page");
  assert.match(control, /disabled=\{draftG === valueG \|\| pending\}/, "the pending state is shown, not thrown away");
  const card = read("./over35-kpi.tsx");
  assert.match(card, /await countOver35Action\(parkId, nextG\)/);
  assert.match(card, /replaceLocalOverlayUrl\(/);
  const actions = read("./over35-actions.ts");
  assert.match(actions, /^"use server";/);
  assert.doesNotMatch(actions, /revalidatePath/);
});

test("Summary and Farm value stop reading what their filter does not change", () => {
  const sold = read("./sales-sold.tsx");
  assert.doesNotMatch(sold, /getSalesOptions\(/, "the read-only Summary has no record form to feed");
  const value = read("./sales-farm-value.tsx");
  // The valuation starts BEFORE the assumptions round trip, and there is one weighing read.
  assert.ok(value.indexOf("const overviewPromise = getSalesOverview") < value.indexOf("await getGrowthAssumptions()"));
  assert.equal((value.match(/getShedWeights\(/g) ?? []).length, 1);
});

test("a pressed chip or pager says it is busy in place", () => {
  // The farm chips are the template segmented tabs (SegmentTabs, Tabs indicatorColor="custom"):
  // the pressed strip is aria-busy while its page is on its way, which is the in-place busy mark.
  assert.match(read("./sales-chrome.tsx"), /<LiveQueryTabs/);
  assert.match(read("./live-query-link.tsx"), /<SegmentTabs/);
  assert.match(read("../../components/app/list/segment-tabs.tsx"), /aria-busy=\{busy \|\| pendingValue !== null \|\| undefined\}/);
  // Pagers are the template TablePaginationLinks (via ProcurementTableFooter), whose arrows turn
  // into a spinner while their link is pending; chip strips are SegmentTabs (aria-busy above).
  assert.match(read("../../components/app/table/table-pagination-links.tsx"), /const \{ pending \} = useLinkStatus\(\);/);
  for (const file of ["./sales-sold.tsx", "./sales-buyer-analytics.tsx", "./sales-farm-born.tsx"]) {
    assert.match(read(file), /<ProcurementTableFooter/, file);
  }
  for (const file of ["./sales-loads.tsx", "./market-analytics.tsx"]) {
    assert.match(read(file), /<SegmentTabs/, file);
  }
});

// guard: procurement-template-tabs (PR #294 R3SP2). Procurement and sales pages use the template
// tab strips only: UrlTabs (template list status Tabs + Label counts, useUrlTabNav) for URL
// status/filter tabs, SegmentTabs (template segmented Tabs) for chip strips, MUI Tabs for client
// state. The AnimatedTabs / StatStrip / KpiCard fakes are not imported here.
test("procurement and sales features use template tabs and widgets, not the removed fakes", async () => {
  const { readdirSync } = await import("node:fs");
  for (const name of readdirSync(new URL(".", import.meta.url))) {
    if (!/\.tsx?$/.test(name)) continue;
    const src = read(`./${name}`);
    assert.doesNotMatch(src, /\b(AnimatedTabs|StatStrip|KpiCard)\b(?!Skeleton)/, `${name} uses a removed fake (AnimatedTabs/StatStrip/KpiCard)`);
  }
});

// guard: market-loading-mirrors-panels (R3SP2 audit re-run). The route loading shape drew tall
// "hero" KPI cards and a short table, while the page's own in-place fallbacks (and the loaded page)
// are the KpiWidget row, a six-row table card with a header and a 320px chart card: skeleton IoU P0.
test("market analytics route loading uses the page's in-place fallback shapes", () => {
  const loading = read("../../app/(admin)/sales/market-analytics/loading.tsx");
  const page = read("./market-analytics.tsx");
  assert.match(page, /fallback=\{<KpiRowSkeleton count=\{4\} hero \/>\}/);
  assert.match(loading, /<KpiRowSkeleton count=\{4\} hero \/>/);
  assert.match(page, /<TableSkeleton columns=\{5\} rows=\{6\} pager=\{false\} subheader headerAction \/><ChartCardSkeleton height=\{320\} subheader \/>/);
  assert.match(loading, /<TableSkeleton columns=\{5\} rows=\{6\} pager=\{false\} subheader headerAction \/>\s*<ChartCardSkeleton height=\{320\} subheader \/>/);
});
