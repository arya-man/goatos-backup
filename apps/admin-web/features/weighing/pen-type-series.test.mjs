import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// PEN TYPES ARE THE FARM'S OWN LIST (Configuration -> Items and settings -> Pen types, migration
// 000428, maintainer instruction 2026-09-25). The Pen-wise chart's series, their names and their
// order come from the page contract's `pen_types` option group; the page must not name a pen type,
// or a type the farm adds would be missing from the chart.
const source = readFileSync(new URL("./weights-analytics.tsx", import.meta.url), "utf8");
const shedTab = source.slice(source.indexOf("function ShedTab("), source.indexOf("function WeightTab("));

test("the Pen-wise chart builds its series from the pen_types option group", () => {
  assert.ok(shedTab.length > 0, "ShedTab not found");
  assert.match(shedTab, /group\.id === "pen_types"/);
});

test("the Pen-wise chart names no pen type of its own", () => {
  for (const literal of ['"elevated"', '"non_elevated"', "view.shed_type."]) {
    assert.ok(!shedTab.includes(literal), `ShedTab names ${literal}; pen types come from the register`);
  }
});
