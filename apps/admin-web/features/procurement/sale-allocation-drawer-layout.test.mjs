import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

// Tag animals to sale on a 390px phone (2026-09-25): the tagging tables were clipped inside the
// drawer (the weight step showed ")6" beside a clipped box) and Park/Pen ran past its edge.
const css = legacyCss("mesha-theme");
const drawer = readFileSync(new URL("./sale-allocation-drawer.tsx", import.meta.url), "utf8");

// R2-4 (Ravi): the drawer is the template temporary Drawer (DetailDrawer: portal, backdrop,
// 480px paper, header title + close at the edge, Scrollbar body, footer actions). Its tables keep
// every column whole and pan inside their own Scrollbar instead of clipping at the drawer edge.
test("Tag animals to sale renders the template drawer, not the legacy aside", () => {
  assert.match(drawer, /<DetailDrawer\b[^]*?onClose=\{close\}/);
  assert.doesNotMatch(drawer, /className="(?:scrim|drawer)\b|sales-tag/);
  assert.doesNotMatch(css, /\.sales-tag/);
});

test("both tagging tables scroll inside their own Scrollbar", () => {
  const scrolled = drawer.match(/<DrawerTableScroll>\s*<Table size="small" sx=\{\{ minWidth: \d+ \}\}/g) ?? [];
  assert.equal(scrolled.length, 2);
});

test("the pick step is one column: the running count sits above the list", () => {
  assert.ok(drawer.indexOf('data-testid="sale-tag-count"') < drawer.indexOf('id="tag-park"'));
  assert.doesNotMatch(drawer, /gridTemplateColumns/);
});

test("the actions live in the drawer footer", () => {
  assert.match(drawer, /footer=\{[^]*action\.done[^]*action\.confirm_sold[^]*\}\s*>/);
});

// FIXJ6: the `:where(.card)>:where(.empty)` legacy rule died with mesha-theme.css; the drawer renders
// no legacy `card` / `empty` class that would have needed it.
test("the drawer renders no legacy card / empty class", () => {
  assert.doesNotMatch(drawer, /className=["'{][^"'}]*\b(?:card|empty)\b/);
});

test("the Load wise hover card is the template tooltip, never clipped by a chart scroller", () => {
  // Ravi 2026-09-27: the plot no longer scrolls inside the card (the scroller was what cut the
  // card), so no page CSS repositions the template's .apexcharts-tooltip.
  assert.doesNotMatch(css, /apexcharts-tooltip/);
  assert.doesNotMatch(css, /\.gcols-scroll/);
});
