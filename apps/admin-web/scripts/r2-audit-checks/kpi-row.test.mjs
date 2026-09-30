import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";

import plugin, { probeKpiRows } from "./kpi-row.mjs";

// guard: kpi-row-one-kind (J2 P1-8). One widget kind per KPI row, and no row of a deck stops short
// while another row is full.
test("kpi-row plugin is a P0 r2 audit check", () => {
  assert.equal(plugin.name, "kpi-row");
  assert.equal(plugin.p0, true);
});

test("probe flags a mixed row and an empty slot, passes uniform full rows", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
    const tile = (kind, w) => `<div style="width:${w}px;height:100px"><div data-kpi-kind="${kind}" style="height:100px">x</div></div>`;
    await page.setContent(`
      <div class="MuiGrid-container" id="a" style="display:flex;flex-wrap:wrap;width:900px">${tile("course", 300)}${tile("booking", 300)}${tile("course", 300)}</div>
      <div class="MuiGrid-container" id="b" style="display:flex;flex-wrap:wrap;width:900px">${tile("app", 300)}${tile("app", 300)}${tile("app", 300)}${tile("course", 300)}${tile("course", 300)}</div>
      <div class="MuiGrid-container" id="c" style="display:flex;flex-wrap:wrap;width:900px">${tile("app", 300)}${tile("app", 300)}${tile("app", 300)}${tile("course", 450)}${tile("course", 450)}</div>`);
    const found = await page.evaluate(probeKpiRows);
    assert.deepEqual(found.map((f) => f.kind).sort(), ["empty-slot", "mixed-kind"], JSON.stringify(found));
  } finally {
    await browser.close();
  }
});
