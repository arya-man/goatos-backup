import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Two defects on the same figure, found together on the sales column charts (2026-09-20):
//   1. a column with a MEASURED zero carried a label and nothing above it, which reads as a
//      figure that failed to load rather than as "the farm sold none";
//   2. on a phone the TALLEST column's figure vanished -- the one column a reader most wants.
// Both are pinned here because both are one-line regressions to reintroduce. The grouped columns
// now draw with the template's ApexCharts Chart; the rules are the same.

const groupedColumns = readFileSync(new URL("./grouped-columns.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");

test("a measured zero still prints its figure; an absent value prints nothing", () => {
  // The data-label formatter is gated on ABSENT (null), never on zero: gating it on `value > 0`
  // is exactly the defect. "Cost not recorded" is not a quantity anyone measured, so null stays blank.
  assert.match(groupedColumns, /dataLabels: \{\s*enabled: true/);
  assert.match(groupedColumns, /datum\.values\[opts\.seriesIndex\] === null\) return ""/);
  assert.match(groupedColumns, /return barLabelFor\(datum, opts\.seriesIndex\) \?\? ""/);
  assert.doesNotMatch(groupedColumns, /values\[opts\.seriesIndex\] (?:=== 0|> 0|<= 0)/, "a zero column must still carry its figure");
});

test("the tallest column keeps its figure", () => {
  // Labels sit above the bar (position top) and are never hidden for overflowing the plot.
  assert.match(groupedColumns, /dataLabels: \{ position: "top", hideOverflowingLabels: false \}/);
});

test("a grouped column is wide enough for one figure per bar side by side at every width", () => {
  // The 64px floor printed figures over the NEXT column's bars ("₹63.7k" on "+₹88.7k", judge 4
  // P1-3). The slot is sized on the component from its own longest figure and axis line, so
  // Counts Breakdown and Load wise both get room, and the plot scrolls inside the card.
  const grouped = readFileSync(new URL("./grouped-columns.tsx", import.meta.url), "utf8");
  // One figure per COLUMN: a stacked series shares its base's column (main 7765efb29).
  assert.match(grouped, /\(columns \* \(longestFigure \* FIGURE_CHAR_PX \+ 10\)\) \/ GROUP_WIDTH/);
  assert.match(grouped, /columnWidth: `\$\{GROUP_WIDTH \* 100\}%`/);
  assert.match(css, /\.gcols-plot\{height:340px;min-width:calc\(var\(--gcols-n,1\) \* var\(--gcols-slot,64px\) \+ 64px\)\}/);
  assert.match(css, /\.gcols-scroll\{[^}]*overflow-x:auto/, "a long load list scrolls inside the chart, not the page");
  // The legend sits above the scroller, so every series is named on screen at 390 (P1-5).
  assert.match(grouped, /legend: \{ show: false \}/);
  assert.match(grouped, /<SeriesLegendView entries=\{series\.map[\s\S]*?<div className="gcols-scroll"/);
  // One unit per y axis, ₹ on money charts (b3d90d41b).
  assert.match(grouped, /const axisTick = money \? inrAxisTick : numAxisTick;/);
  assert.doesNotMatch(grouped, /compactTick/);
});
