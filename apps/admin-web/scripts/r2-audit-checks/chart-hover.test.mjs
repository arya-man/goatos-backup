import assert from "node:assert/strict";
import test from "node:test";

import plugin, { HOVER_FRACTIONS, tooltipClip } from "./chart-hover.mjs";

// guard: r2-visual-audit chart-hover (P0) -- a hovered chart's tooltip is never clipped and the raw
// ApexCharts a11y string never shows (Ravi 2026-09-27, /sales/sold Month by month).
const viewport = { width: 390, height: 844 };

test("a tooltip inside the viewport and its clipping card passes", () => {
  const card = { left: 16, right: 374, top: 100, bottom: 600, sig: "div.MuiCard-root" };
  assert.deepEqual(tooltipClip({ left: 40, right: 200, top: 150, bottom: 230 }, [card], viewport), []);
});

test("a tooltip cut by a sideways scroller or the screen edge fails", () => {
  const scroller = { left: 16, right: 374, top: -1e9, bottom: 1e9, sig: "div.chart-slots-scroll" };
  assert.deepEqual(tooltipClip({ left: 300, right: 460, top: 150, bottom: 230 }, [scroller], viewport), ["viewport", "div.chart-slots-scroll"]);
});

test("the plugin is a P0 r2-visual-audit check", () => {
  assert.equal(plugin.name, "chart-hover");
  assert.equal(plugin.p0, true);
  assert.equal(typeof plugin.run, "function");
});

test("hover samples cover the whole plot: no gap wider than 15% (a flipped tooltip over a bar a third in)", () => {
  const points = [0, ...HOVER_FRACTIONS, 1];
  for (let i = 1; i < points.length; i++) assert.ok(points[i] - points[i - 1] <= 0.15 + 1e-9, `gap ${points[i - 1]}..${points[i]}`);
  // /weighing/analytics Weekly growth at 390 dark: the tooltip flipped left of the third bar, 58px past the card.
  const card = { left: 16, right: 374, top: 100, bottom: 700, sig: "div.MuiCard-root" };
  assert.deepEqual(tooltipClip({ left: -42, right: 175, top: 350, bottom: 420 }, [card], viewport), ["viewport", "div.MuiCard-root"]);
});
