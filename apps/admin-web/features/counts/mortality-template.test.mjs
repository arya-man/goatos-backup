import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// /counts/mortality side-by-side vs the Minimal analytics overview (R3CNT 2026-09-27).
const source = readFileSync(new URL("./mortality.tsx", import.meta.url), "utf8");

test("rate-table rows show words, never raw codes; heads are capitalised; labels hold one line", () => {
  // "male" / "goat" rows sat under "Deaths" / "animals" heads (rhythm|raw-code-label).
  assert.match(source, /function bucketLabel\([\s\S]{0,200}humanizeEnum\(label\)/);
  assert.doesNotMatch(source, /\{bucket\.label \|\| unassignedLabel/);
  assert.match(source, /animalsLabel\.charAt\(0\)\.toUpperCase\(\) \+ animalsLabel\.slice\(1\)/);
  // One line, ending in an ellipsis with the full text as the title (PR #294 O3), never wrapped.
  assert.match(source, /const LABEL_CELL_SX = \{ typography: "subtitle2", whiteSpace: "nowrap"/);
  assert.equal((source.match(/sx=\{LABEL_CELL_SX\} title=/g) ?? []).length, 3);
});

test("months chart keeps the template colour pair: no palette KEY handed to chart.colors (audit chart-black)", () => {
  assert.doesNotMatch(source, /seriesColorVar/);
});

// guard: mortality-kpi-trend-twin (SYNC, J2 P1-8). The Deaths / Kids / Adults cards carry the
// complete-month trend, which KpiWidget prints as the lead of the Course card's sub-line (one widget
// kind per row); the loading twin draws that caption line. The page's trend-bearing cards and
// MORTALITY_KPI_TRENDS stay in the same order.
test("mortality KPI twin draws a trend row exactly where the page has one", async () => {
  const { readFileSync: read } = await import("node:fs");
  const page = read(new URL("./mortality.tsx", import.meta.url), "utf8");
  const layout = read(new URL("./counts-layout.ts", import.meta.url), "utf8");
  const skeletons = read(new URL("./counts-skeletons.tsx", import.meta.url), "utf8");
  const body = page.slice(page.indexOf("const kpis:"), page.indexOf("];", page.indexOf("const kpis:")));
  const cards = body.split(/\n\s{4}\{/).slice(1).map((card) => /trend: monthTrend\(/.test(card));
  const declared = JSON.parse(layout.match(/MORTALITY_KPI_TRENDS = (\[[^\]]*\])/)[1]);
  assert.deepEqual(cards, declared);
  assert.match(skeletons, /withTrendLine\(kpiShapes\(MORTALITY_KPI_CAPTIONS\), MORTALITY_KPI_TRENDS\)/);
  assert.doesNotMatch(skeletons, /booking: true/, "no Booking-card twin: the page renders none");
});
