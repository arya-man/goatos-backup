import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// Sales Config's Load wise table makes every cell a .celllink, which defaults to
// overflow-wrap:anywhere. At 390px that shredded "136 · 16/09/2026" and "CBE" one character per
// line (responsive guard C-cell-mid-word-wrap, 2026-09-25). The table owns horizontal scroll, so
// its cells must not wrap (AGENTS.md admin-web failure mode 4b).
const tsx = readFileSync(new URL("./sales-config.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("the Load wise entry table carries its no-wrap class", () => {
  assert.match(tsx, /<table className="sales-load-entry-table" aria-label=\{copy\(pageContract, "section\.load_entry\.title"\)\}>/);
});

test("the no-wrap rule covers the table's th, td and linked cells", () => {
  for (const sel of ["table.sales-load-entry-table th", "table.sales-load-entry-table td", "table.sales-load-entry-table td .celllink"]) {
    assert.ok(css.includes(sel), `missing ${sel}`);
  }
  const rule = css.slice(css.indexOf(".main table.sales-load-entry-table th"));
  assert.match(rule.slice(0, rule.indexOf("}") + 1), /white-space:nowrap;overflow-wrap:normal;word-break:normal/);
});

// The market price chart's axis text rendered at ~5px on a 390px phone (responsive guard
// A-svg-text-tiny, 2026-09-25): its 7-unit date labels need >= 640px of rendered width to reach
// the 8px floor, so the chart keeps that floor inside its own pan wrapper.
test("the market trend chart keeps a 640px floor inside its own scroll wrapper", () => {
  const trend = readFileSync(new URL("./market-trend-section.tsx", import.meta.url), "utf8");
  assert.match(trend, /className="sales-market-trend-scroll"[\s\S]*className="sales-market-trend-inner"[\s\S]*<SeriesLines/);
  assert.match(css, /\.sales-market-trend-scroll\{max-width:100%;overflow-x:auto/);
  assert.match(css, /\.sales-market-trend-inner\{min-width:640px\}/);
});
