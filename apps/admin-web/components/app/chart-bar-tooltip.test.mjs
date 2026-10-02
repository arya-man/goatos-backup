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

test("chart-bar-shared-tooltip: horizontal conversion bars pin + wrap the tooltip on phones", () => {
  const conv = readFileSync(new URL("./conversion-rates-card.tsx", import.meta.url), "utf8");
  assert.match(conv, /useMediaQuery\(\(theme: Theme\) => theme\.breakpoints\.down\("sm"\), \{ noSsr: true \}\)/, "decided at construction");
  assert.match(conv, /fixed: \{ enabled: true, position: "topLeft"/);
  assert.match(conv, /x: \{ formatter: \(label: string \| number\) => wrapTooltipTitle\(String\(label\)\) \}/);
  assert.match(conv, /replace\(\/&\/g, "&amp;"\)\.replace\(\/<\/g, "&lt;"\)/, "title HTML is escaped");
  assert.doesNotMatch(conv, /apexcharts-tooltip/, "no CSS on the tooltip");
});

// PR #294 K3/K4: no white value label straddling the bar end (labels off, figure in the tooltip),
// and a negative bar wears the error tone.
import { test as kTest } from "node:test";
kTest("conversion-rates bars: no straddling value labels, red negatives", async () => {
  const { readFileSync: read } = await import("node:fs");
  const src = read(new URL("./conversion-rates-card.tsx", import.meta.url), "utf8");
  const { match } = (await import("node:assert/strict")).default;
  match(src, /dataLabels: \{ enabled: false \}/);
  match(src, /ranges: \[\{ from: -Number\.MAX_SAFE_INTEGER, to: -Number\.EPSILON, color: theme\.vars\.palette\.error\.main \}\]/);
});
