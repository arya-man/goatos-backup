import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { BAR_ROW_PX, GROW_BARS, VISIBLE_BARS, barsScroll, barsViewHeight, barsWindowHeight } from "./minimal/bar-charts/geometry.ts";

const wrapper = readFileSync(new URL("./svg-bars.tsx", import.meta.url), "utf8");
const chart = readFileSync(new URL("./minimal/bar-charts/bar-charts.tsx", import.meta.url), "utf8");

test("the scroll window is a whole number of fixed-height rows, never a width-dependent box", () => {
  // Apex draws each row at chartHeight / rows, so the chart height is rows x BAR_ROW_PX and the
  // window is VISIBLE_BARS of those rows at every card width (the old viewBox needed an
  // aspect-ratio because its row height followed the width; this one must not).
  assert.equal(barsViewHeight(12) - barsViewHeight(11), BAR_ROW_PX);
  assert.equal(barsWindowHeight(GROW_BARS + 1), barsViewHeight(VISIBLE_BARS));
  assert.match(chart, /sx=\{\{ height: barsViewHeight\(rows\.length\) \}\}/);
  assert.match(chart, /maxHeight: barsWindowHeight\(rows\)/);
  assert.doesNotMatch(chart, /aspectRatio|aspect-ratio/);
});

test("the window appears only when there is something to scroll to", () => {
  // Applied unconditionally it would stretch a three-bar chart to ten rows of empty card; and up to
  // GROW_BARS rows the chart simply grows, so a 12-row chart gains no needless inner scroll.
  assert.ok(GROW_BARS >= VISIBLE_BARS);
  assert.equal(barsScroll(GROW_BARS), false);
  assert.equal(barsScroll(GROW_BARS + 1), true);
  assert.equal(barsWindowHeight(3), barsViewHeight(3));
  assert.match(chart, /const scrolls = barsScroll\(rows\);/);
  assert.match(chart, /sx=\{\s*scrolls\s*\?/);
});

test("the window scrolls inside the card without trapping the page", () => {
  assert.match(chart, /overflowY: "auto"/);
  assert.match(chart, /overscrollBehavior: "contain"/);
});

test("a scrolling chart is keyboard-reachable, a short one adds no tab stop", () => {
  assert.match(chart, /tabIndex=\{scrolls \? 0 : undefined\}/);
});

test("ten bars stand in the card", () => {
  assert.equal(VISIBLE_BARS, 10);
});

test("every drawn bar carries its value label, and a loss draws red left of a zero rule", () => {
  assert.match(chart, /dataLabels: \{\s*enabled: true,/);
  assert.match(chart, /hideOverflowingLabels: false/);
  assert.match(chart, /r\.value < 0 \? "var\(--error\)"/);
  assert.match(chart, /annotations: hasNegative/);
  // Losses are data, not noise: only zeros and non-finite values are left off the chart.
  assert.match(wrapper, /data\.filter\(\(d\) => Number\.isFinite\(d\.value\) && d\.value !== 0\)/);
});

test("category labels truncate at a card-relative width instead of being dropped", () => {
  assert.match(chart, /maxWidth: labelPx/);
  assert.match(chart, /fontSize: "13px"/);
});

test("the wrapper hands the client chart only serializable props", () => {
  assert.doesNotMatch(wrapper, /^"use client"/);
  assert.match(chart, /^"use client";/);
  assert.doesNotMatch(wrapper, /formatter\s*[:=]/);
});
