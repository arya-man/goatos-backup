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
  assert.equal((source.match(/whiteSpace: "nowrap" \}\}>\{bucketLabel\(bucket\.label\)/g) ?? []).length, 2);
});

test("months chart keeps the template colour pair: no palette KEY handed to chart.colors (audit chart-black)", () => {
  assert.doesNotMatch(source, /seriesColorVar/);
});

// guard: mortality-kpi-trend-twin (SYNC). The Deaths / Kids / Adults cards carry the complete-month
// trend, so KpiWidget draws them as the booking card with a trend row; the loading twin must draw the
// same rows or the 390 skeleton card is 46px short (IoU 0.7). The page's trend-bearing cards and
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
  assert.match(skeletons, /MORTALITY_KPI_TRENDS\[i\] \? \{ \.\.\.shape, booking: true, trend: true \}/);
});
