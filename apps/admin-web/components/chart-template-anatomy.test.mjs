import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Ravi 2026-09-27 (/sales/sold "Month by month" in dark, and the same on many pages): hovering a
// bar painted a big grey column, the tooltip was clipped and showed raw a11y text, every bar
// carried a value label, bars were fat and neon, axis labels were two-line "Apr 2025", and three
// charts sat in one card under overline sub-headings. The fix is a PATTERN: every chart is the
// template's Chart + useChart, verbatim, with palette colours and the template card anatomy.
// design:guard carries the repo-wide rules (chart-data-labels, chart-states-override,
// chart-bypasses-usechart, chart-raw-colour, chart-wrapper-verbatim); this file pins the pieces.

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("guard: chart-wrapper-verbatim -- Chart, useChart and the chart CSS are the template's bytes", () => {
  // No Mesha additions (useStableOptions, resolveCssVars, useTooltipInView, toMerged, a phone
  // legend rule): each one was an override the template does not have.
  const chart = read("./minimal/chart/chart.tsx");
  const useChart = read("./minimal/chart/use-chart.ts");
  for (const banned of [/useStableOptions|resolveCssVars|useTooltipInView|deps\?:/, /toMerged/, /Mesha/]) {
    assert.doesNotMatch(chart + useChart + read("./minimal/chart/styles.css"), banned);
  }
  assert.match(useChart, /updatedOptions \? merge\(baseOptions, updatedOptions\) : baseOptions/);
  assert.match(useChart, /dataLabels: \{\s*enabled: false,\s*\}/);
});

test("guard: chart-raw-colour -- every chart colour is a palette channel resolved from the theme", () => {
  const palette = read("./app/chart-colors.ts");
  assert.match(palette, /export function chartColor\(theme: Theme, token: string\): string \{/);
  assert.match(palette, /theme\.palette\[/);
  // Bars lead with primary.dark (AppAreaInstalled / BankingBalanceStatistics), never the neon main.
  assert.match(palette, /i === 0 && kind === "bar" \? "primary\.dark" : key/);
  for (const file of ["./series-charts.tsx", "./grouped-columns.tsx", "./app/bar-charts.tsx", "./app/trend-chart.tsx"]) {
    const src = read(file);
    assert.match(src, /chartColor\(theme,/, `${file} resolves its colours through chartColor`);
    assert.doesNotMatch(src, /colors: \[?["'](?:var\(|#)/, `${file} hands Apex a raw colour`);
  }
});

test("no chart prints a figure on its bars, and none scrolls its plot inside the card", () => {
  for (const file of ["./series-charts.tsx", "./grouped-columns.tsx", "./app/bar-charts.tsx", "./app/trend-chart.tsx", "../features/ceo-ai/ceo-ai-chart.tsx"]) {
    const src = read(file);
    assert.doesNotMatch(src, /dataLabels/, `${file} prints data labels`);
    assert.doesNotMatch(src, /overflowX: "auto"|overflow-x|gcols-scroll|chart-slots|BarsWindow/, `${file} scrolls its plot (clips the tooltip)`);
    assert.doesNotMatch(src, /hideOverlappingLabels: false|overwriteCategories|rotate: -?\d/, `${file} overrides the template axis labels`);
  }
  const css = read("../app/mesha-theme.css");
  assert.doesNotMatch(css, /apexcharts-tooltip/, "no custom tooltip CSS outside the template chart styles");
});

test("a missing grouped value draws no bar and a zero stays a zero", () => {
  const grouped = read("./grouped-columns.tsx");
  assert.match(grouped, /data: data\.map\(\(d\) => d\.values\[i\]\)/);
  assert.doesNotMatch(grouped, /d\.values\[i\] \?\? 0/);
  // A stacked series shares its base's column and the scale covers the column sums.
  assert.match(grouped, /\.\.\.\(hasStack \? \{ group: groupOf\(i\) \} : null\)/);
  const columns = read("./svg-column-bars.tsx");
  assert.match(columns, /values: data\.map\(\(d\) => d\.value\)/);
  assert.doesNotMatch(columns, /Math\.max\(/);
});

test("a loss draws red, and only zeros and non-finite values are left off a bar list", () => {
  const bars = read("./app/bar-charts.tsx");
  assert.match(bars, /r\.value < 0 \? chartColor\(theme, "error"\)/);
  assert.match(read("./svg-bars.tsx"), /data\.filter\(\(d\) => Number\.isFinite\(d\.value\) && d\.value !== 0\)/);
});

test("server wrappers hand the client charts only serializable props", () => {
  for (const file of ["./svg-bars.tsx", "./svg-column-bars.tsx", "./svg-series.tsx"]) {
    const src = read(file);
    assert.doesNotMatch(src, /^"use client"/, file);
  }
  for (const file of ["./app/bar-charts.tsx", "./series-charts.tsx", "./grouped-columns.tsx"]) {
    assert.match(read(file), /^"use client";/, file);
  }
});

test("guard: chart-a11y-text -- no chart shows ApexCharts' generated a11y string as a hover title", () => {
  // Apex 5 writes "bar chart with 1 data series: Revenue" into an SVG <title> (a native hover
  // tooltip). The global option turns that layer off for every chart; the runtime half is the
  // r2-visual-audit chart-hover plugin (a11y-text, P0).
  const globals = read("./app/apex-globals.ts");
  assert.match(globals, /accessibility: \{ enabled: false \}/);
  assert.match(read("../theme/app-theme-provider.tsx"), /import '@\/components\/app\/apex-globals';/);
  for (const file of ["./series-charts.tsx", "./grouped-columns.tsx", "./app/bar-charts.tsx"]) {
    assert.match(read(file), /role="img"\s+aria-label=\{chartLabel\}/, `${file} names its chart on the root`);
  }
});

test("guard: chart-tooltip-fits -- a stacked chart with many series names only the hovered segment", () => {
  // /feed/analytics "Daily directed feed" (14 feeds): the shared whole-column tooltip listed 14
  // rows, grew taller than the card and the card cut its top off. Past SHARED_TIP_MAX_SERIES the
  // tooltip is per segment. The runtime half is r2-visual-audit chart-hover (tooltip-clipped, P0).
  const src = read("./series-charts.tsx");
  assert.match(src, /export const SHARED_TIP_MAX_SERIES = [1-8];/);
  assert.match(src, /shared: series\.length <= SHARED_TIP_MAX_SERIES,\s*intersect: series\.length > SHARED_TIP_MAX_SERIES,/);
});
