// Guard: chart-bar-shared-tooltip (TR1-#6). Apex flips a per-bar tooltip left of the bar without
// clamping, so on a 390 plot it ran off the viewport; one-measure balance-statistics cards use the
// shared tooltip (Apex's clamped placement) with the section's formatter restated. No CSS may move
// the tooltip (design:guard chart-tooltip-css).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const card = readFileSync(new URL("./balance-statistics-card.tsx", import.meta.url), "utf8");

test("chart-bar-shared-tooltip: one-measure balance cards share the tooltip", () => {
  assert.match(card, /function withSharedTooltip\(/);
  assert.match(card, /shared: true,\s*intersect: false/);
  assert.match(card, /formatSeriesValue\(value, only\.unit/, "formatter restated from the section");
  assert.match(card, /options: withSharedTooltip\(props\.chart\)/);
  assert.doesNotMatch(card, /apexcharts-tooltip/, "no CSS on the tooltip");
});
