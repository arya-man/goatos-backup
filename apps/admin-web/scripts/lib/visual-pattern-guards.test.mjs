import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { VISUAL_PATTERNS, assertChartHoverStability, collectVisualPatternFindings } from "./visual-pattern-guards.mjs";

const source = readFileSync(new URL("./visual-pattern-guards.mjs", import.meta.url), "utf8");

test("exports the in-page collector and the async chart-hover check", () => {
  assert.equal(typeof collectVisualPatternFindings, "function");
  assert.equal(typeof assertChartHoverStability, "function");
});

test("every declared pattern is emitted somewhere in the collector", () => {
  const names = ["P-text-icon-overlap", "P-wide-table-no-wrapper", "P-chart-axis-tiny", "P-pinned-bar-blur-flicker", "P-drawer-filter-mismatch", "P-chart-hover-remount"];
  for (const name of names) {
    assert.ok(VISUAL_PATTERNS[name], `declared: ${name}`);
    assert.ok(source.includes(`"${name}"`), `referenced: ${name}`);
  }
  for (const name of Object.keys(VISUAL_PATTERNS)) assert.ok(names.includes(name), `unknown pattern: ${name}`);
});

test("text-icon-overlap targets svg / i / img siblings inside flex or grid rows", () => {
  assert.match(source, /flex\|grid/);
  assert.match(source, /svg,i,img,use/);
  // A text leaf that IS the icon (svg with text inside) must not overlap itself.
  assert.match(source, /t === icon \|\| t\.contains\(icon\) \|\| icon\.contains\(t\)/);
});

test("wide-table-no-wrapper matches at every viewport, not only phone", () => {
  assert.match(source, /table\.scrollWidth <= vw \+ 1/);
  assert.match(source, /scrollXAncestor/);
  // The webview lane already has its own version; this one is intentionally viewport-agnostic.
  assert.doesNotMatch(source, /isMobile.*table\.scrollWidth/);
});

test("chart-axis-tiny defaults to 11px and covers both DOM and SVG labels", () => {
  assert.match(source, /minChartAxisPx = 11/);
  assert.match(source, /apexcharts-xaxis-label/);
  assert.match(source, /svg\[role=img\]/);
  assert.match(source, /bbox\.height \/ 1\.2/);
});

test("pinned-bar-blur-flicker only fires when content actually scrolls under a sticky/fixed blur", () => {
  assert.match(source, /position !== "sticky" && cs\.position !== "fixed"/);
  assert.match(source, /backdropFilter \|\| cs\.webkitBackdropFilter/);
  assert.match(source, /scrollingAncestorBehind/);
  // Documented cause of the class, so a reviewer knows why the guard trips.
  assert.match(source, /Android WebView repaint/);
});

test("drawer-filter-mismatch compares overlay data-filters to page data-filters", () => {
  assert.match(source, /data-page-filters/);
  assert.match(source, /data-drawer-filters|data-export-filters/);
  assert.match(source, /own !== pageFilters/);
});

test("chart hover check waits for the tooltip and asserts the same node survives three rAF ticks", () => {
  assert.match(source, /requestAnimationFrame/);
  assert.match(source, /now !== tip/);
  assert.match(source, /tip\.isConnected/);
  // Default selector covers both Apex tooltips and the kit's own tooltip card.
  assert.match(source, /apexcharts-tooltip.*kit-chart-tooltip/);
});
