import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";

import plugin, { probeTextFit } from "./text-fit.mjs";

// guards: button-label-wrap (J3 P1-3), axis-label-overlap (J3 P1-4), raw-id-text (J2 P1-3/P1-4),
// dead-primary (J2 P1-1). Each probe flags the defect and passes the fixed shape.
test("text-fit plugin is a P0 r2 audit check", () => {
  assert.equal(plugin.name, "text-fit");
  assert.equal(plugin.p0, true);
  assert.equal(typeof plugin.run, "function");
});

test("probe flags a wrapped button label, overlapping axis labels, raw ids and a dead header primary", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 800 } });
    await page.setContent(`<main style="font:14px sans-serif">
      <header data-page-header><button class="MuiButton-root MuiButton-contained Mui-disabled" disabled>New task</button></header>
      <div style="display:flex;width:120px"><button class="MuiButton-root" style="width:40px">Open the draft</button>
      <button class="MuiButton-root" style="white-space:nowrap">Keep it</button></div>
      <div class="apexcharts-canvas"><svg width="300" height="60">
        <text class="apexcharts-xaxis-label" x="10" y="20">Sirohi</text>
        <text class="apexcharts-xaxis-label" x="40" y="20">Anantapur Sheep</text>
        <text class="apexcharts-xaxis-label" x="200" y="50" transform="rotate(-45 200 50)">Beetal</text>
        <text class="apexcharts-xaxis-label" x="210" y="50" transform="rotate(-45 210 50)">Sojat</text>
      </svg></div>
      <table><tr><td>cee6e124</td><td>Beetal</td><td>2026</td></tr></table>
      <div class="MuiCardHeader-subheader">goat_identity_events</div>
      <div class="MuiCardHeader-subheader">Identity changes</div>
    </main>`);
    const found = await page.evaluate(probeTextFit);
    const kinds = found.map((f) => f.kind).sort();
    assert.deepEqual(kinds, ["axis-label-overlap", "button-label-wrap", "dead-primary", "raw-id-text", "raw-id-text"], JSON.stringify(found));
    assert.ok(found.some((f) => /Open the draft/.test(f.detail)));
    assert.ok(!found.some((f) => /Keep it/.test(f.detail)));
    assert.ok(found.some((f) => /cee6e124/.test(f.detail)) && found.some((f) => /goat_identity_events/.test(f.detail)));
    assert.ok(!found.some((f) => /Beetal|2026/.test(f.detail)), "words, years and rotated labels pass");
  } finally {
    await browser.close();
  }
});
