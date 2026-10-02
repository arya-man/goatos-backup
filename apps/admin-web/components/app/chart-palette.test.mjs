// The categorical chart palette (theme/chart-palette.ts) must stay a set of distinguishable hues
// in BOTH schemes: PR #294 D2/E4 found eleven feeds/breeds painted in three greens, two blues and
// two purples. This re-checks, from the hexes themselves, what the dataviz validator enforced when
// the order was chosen: every slot inside the mode's OKLCH lightness band and above the chroma
// floor, and every ADJACENT pair at least deltaE 15 apart (OKLab x100) under normal vision and at
// least 8 apart under simulated deuteranopia/protanopia.
import assert from "node:assert/strict";
import { test } from "node:test";
import { CHART_CATEGORICAL } from "../../theme/chart-palette.ts";

const lin = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const rgb = (hex) => [1, 3, 5].map((i) => lin(parseInt(hex.slice(i, i + 2), 16) / 255));
// Machado et al. 2009, severity 1.0, on linear RGB.
const CVD = {
  protan: [[0.152286, 1.052583, -0.204868], [0.114503, 0.786281, 0.099216], [-0.003882, -0.048116, 1.051998]],
  deutan: [[0.367322, 0.860646, -0.227968], [0.280085, 0.672501, 0.047413], [-0.01182, 0.04294, 0.968881]],
};
const clamp = (v) => Math.min(1, Math.max(0, v));
const sim = (c, m) => (m ? m.map((row) => clamp(row[0] * c[0] + row[1] * c[1] + row[2] * c[2])) : c);
function oklab([r, g, b]) {
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s, 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s];
}
const dE = (a, b, m) => {
  const [x, y] = [oklab(sim(rgb(a), m)), oklab(sim(rgb(b), m))];
  return 100 * Math.hypot(x[0] - y[0], x[1] - y[1], x[2] - y[2]);
};
const BAND = { light: [0.43, 0.77], dark: [0.48, 0.67] };

for (const mode of ["light", "dark"]) {
  test(`chart palette (${mode}): twelve slots, in band, saturated, adjacent pairs distinguishable`, () => {
    const hexes = CHART_CATEGORICAL.map((slot) => slot[mode]);
    assert.equal(hexes.length, 12);
    assert.equal(new Set(CHART_CATEGORICAL.map((slot) => slot.key)).size, 12);
    for (const hex of hexes) {
      const [L, a, b] = oklab(rgb(hex));
      assert.ok(L >= BAND[mode][0] && L <= BAND[mode][1], `${hex} lightness ${L.toFixed(3)} outside the ${mode} band`);
      assert.ok(Math.hypot(a, b) >= 0.1, `${hex} reads as grey`);
    }
    for (let i = 1; i < hexes.length; i += 1) {
      const [a, b] = [hexes[i - 1], hexes[i]];
      assert.ok(dE(a, b) >= 15, `${a} / ${b} normal-vision deltaE ${dE(a, b).toFixed(1)} < 15`);
      for (const kind of ["protan", "deutan"]) assert.ok(dE(a, b, CVD[kind]) >= 8, `${a} / ${b} ${kind} deltaE ${dE(a, b, CVD[kind]).toFixed(1)} < 8`);
    }
  });
}

test("the chartRamp order is the palette's validated order", async () => {
  const { readFileSync } = await import("node:fs");
  const source = readFileSync(new URL("./chart-colors.ts", import.meta.url), "utf8");
  const ramp = [...source.match(/const RAMP: ChartColorKey\[\] = \[(.*)\];/)[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(ramp, CHART_CATEGORICAL.map((slot) => slot.key));
});
