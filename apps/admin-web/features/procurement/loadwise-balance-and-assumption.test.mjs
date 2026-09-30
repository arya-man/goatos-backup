import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../scripts/lib/legacy-css.mjs";

const section = readFileSync(new URL("./loadwise-section.tsx", import.meta.url), "utf8");
const chart = readFileSync(new URL("../../components/grouped-columns.tsx", import.meta.url), "utf8");
const css = legacyCss("mesha-theme");

// Tagging takes an animal out of the herd at once, but only a closed deal is a sale: the load
// balances through its own "Tagged, sale not closed" bucket (maintainer decision 2026-09-25).
test("tagged-not-closed is its own series, table cell and tile figure", () => {
  assert.match(section, /key: "tagged_not_closed", label: copy\(pageContract, "chart\.series\.tagged_not_closed"\), tone: "okHatch"/);
  assert.match(section, /values: \[load\.purchased, load\.sold, load\.mortality, load\.remaining, load\.tagged_not_closed\]/);
  assert.match(section, /summary\.tagged_not_closed > 0/);
  assert.match(section, /load\.tagged_not_closed === 0/);
  // A fifth solid colour would read as one of the four; the striped sold green cannot (the Apex
  // chart fills okHatch series with a pattern on the MUI redesign).
  assert.match(chart, /s\.tone === "okHatch" \? "pattern" : "solid"/);
});

// Profit carries animals still on farm at a price nobody has paid. The money chart stacks that
// ASSUMED value on the sold (realised) value, and the tooltip says how it was assumed in the
// backend's own sentence -- the client composes no basis of its own.
test("the money chart stacks the assumed value on the sold value and shows the backend's basis", () => {
  assert.match(section, /key: "assumed_value",[\s\S]*?tone: "okHatch",[\s\S]*?stackOn: "sold_value"/);
  assert.match(section, /tipLines: load\.assumed_value_basis \? \[load\.assumed_value_basis\] : undefined/);
  assert.match(section, /summary\.assumed_value_basis/);
  assert.doesNotMatch(section, /value\.profit_incl_stock/);
});

test("a stacked series draws inside its base slot, and the scale covers the stacked total", () => {
  assert.match(chart, /stackOn\?: string/);
  // Apex grouped stacking: a stacked series shares its base's group.
  assert.match(chart, /group: groupOf\(i\)/);
  // The y scale is computed over the column sums.
  assert.match(chart, /series\.map\(\(_, i\) => columnValue\(d, i\)\)/);
});

// "Fattening days on farm: sold, alive in any case at the bottom; show me when they reached the
// farm on hover" (maintainer, 2026-09-25).
test("the fattening chart states sold and alive under every bar and the reached-farm date on hover", () => {
  assert.match(section, /load\.sold > 0 \? `\$\{num\(load\.sold\)\} \$\{copy\(pageContract, "value\.sold_count"\)\}` : null/);
  assert.match(section, /load\.remaining > 0 \? `\$\{num\(load\.remaining\)\} \$\{copy\(pageContract, "value\.still_on_farm"\)\}` : null/);
  assert.match(section, /copy\(pageContract, "value\.arrived_on"\)\} \$\{humanDate\(load\.arrived_on\)\}/);
  assert.match(section, /copy\(pageContract, "value\.bought_on"\)\} \$\{humanDate\(load\.purchase_date\)\}/);
  // Hover-only lines render in the card, not under the axis.
  assert.match(chart, /datum\.tipLines\?\.map/);
});
