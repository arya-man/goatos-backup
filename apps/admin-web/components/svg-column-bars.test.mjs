import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./svg-column-bars.tsx", import.meta.url), "utf8");
const chart = readFileSync(new URL("./minimal/bar-charts/bar-charts.tsx", import.meta.url), "utf8");

test("column bars use the empty state for all-zero data instead of blank mobile chart scaffolding", () => {
  assert.match(source, /const hasAnyValue = data\.some\(\(d\) => d\.value > 0 \|\| \(d\.compareValue \?\? 0\) > 0\);/);
  assert.match(source, /data\.length === 0 \|\| !hasAnyValue/);
});

test("zero values do not receive the minimum visible bar height", () => {
  // The raw value reaches the chart: a zero draws no column, and nothing pads it to a sliver.
  assert.match(source, /values: data\.map\(\(d\) => d\.value\)/);
  assert.doesNotMatch(source, /Math\.max\(/);
  assert.doesNotMatch(chart, /minBarLength|Math\.max\(\(?(?:d|datum)\.value/);
});

test("every column, a zero day included, prints its figure", () => {
  assert.match(source, /labels: data\.map\(\(d\) => figure\(d\.value\)\)/);
  assert.match(chart, /orientation: "vertical", hideOverflowingLabels: false/);
});

test("every day keeps its axis label and the strip scrolls inside the card on a phone", () => {
  assert.match(chart, /hideOverlappingLabels: false, trim: false/);
  assert.match(chart, /overflowX: "auto"/);
  assert.match(chart, /overscrollBehaviorX: "contain"/);
});
