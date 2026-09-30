import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

// Sales Config's Load wise table makes every cell a drawer link (template Box link, the legacy
// .celllink defaulted to overflow-wrap:anywhere). At 390px that shredded "136 · 16/09/2026" and "CBE" one character per
// line (responsive guard C-cell-mid-word-wrap, 2026-09-25). The table owns horizontal scroll, so
// its cells must not wrap (AGENTS.md admin-web failure mode 4b).
const tsx = readFileSync(new URL("./sales-config.tsx", import.meta.url), "utf8");
const css = legacyCss("mesha-theme");

// MUI redesign: the table is the template MUI Table and the no-wrap rule rides its own sx.
test("the Load wise entry table carries its no-wrap rule", () => {
  assert.match(tsx, /<Table\s+aria-label=\{copy\(pageContract, "section\.load_entry\.title"\)\}[\s\S]*?sx=\{\{/);
});

test("the no-wrap rule covers the table's th, td and linked cells", () => {
  assert.match(tsx, /"& th, & td, & td a": \{ whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" \}/);
  assert.match(tsx, /"& td a": \{ maxWidth: "none", minWidth: "max-content" \}/);
  // Legacy-free: the linked cells are template Box links on theme sx, never the .celllink class.
  assert.doesNotMatch(tsx, /className="celllink"/);
});

// The market price chart's axis text rendered at ~5px on a 390px phone (responsive guard
// A-svg-text-tiny, 2026-09-25): its 7-unit date labels need >= 640px of rendered width to reach
// the 8px floor, so the chart keeps that floor inside its own pan wrapper.
// MUI redesign: the trend is the template ApexCharts line chart (SeriesLines), whose axis text is
// drawn in CSS pixels at every card width, so it needs no scaled-viewBox floor or pan wrapper.
test("the market trend chart is the template chart, not a scaled SVG", () => {
  const trend = readFileSync(new URL("./market-trend-section.tsx", import.meta.url), "utf8");
  assert.match(trend, /<SeriesLines/);
  assert.doesNotMatch(trend, /ChartHover/);
});
