import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";

import plugin, { probeControls } from "./controls.mjs";

test("controls plugin is a P0 r2 audit check (guard: toolbar-controls)", () => {
  assert.equal(plugin.name, "controls");
  assert.equal(plugin.p0, true);
});

const field = (label, extra = "") => `<div class="MuiFormControl-root" style="display:inline-block;width:160px;height:48px;${extra}">
  <label class="MuiInputLabel-root">${label}</label><input style="width:150px"></div>`;

test("probe: doubled label, overlapping controls and a clipped pager are flagged; a clean toolbar passes", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
    await page.setContent(`<div style="display:flex;gap:16px">${field("Assignee")}${field("Sort")}</div>
      <div class="MuiPaper-root" style="width:300px"><div class="MuiTablePagination-root" style="overflow:visible;display:flex;justify-content:flex-end">
      <span>1-7 tasks</span><button aria-label="Next" style="width:36px">&gt;</button></div></div>`);
    assert.deepEqual(await page.evaluate(probeControls), []);
    // A drawer field floating over a page field is a different layer, not a collision.
    await page.setContent(`<div>${field("Search")}</div><div class="MuiModal-root" style="position:fixed;top:0;left:40px">${field("Price")}</div>`);
    assert.deepEqual(await page.evaluate(probeControls), []);

    await page.setContent(`<div style="display:flex">${field("Sort", "")}<span></span></div>
      <div style="display:flex"><span>Sort</span>${field("Sort")}</div>
      <div style="display:flex">${field("Assignee")}${field("Search", "margin-left:-60px")}</div>
      <div class="MuiPaper-root" style="width:200px"><div class="MuiTablePagination-root" style="overflow:visible;display:flex;width:200px">
      <span style="flex:0 0 190px">1-7 tasks · Page 1</span><button aria-label="Next" style="flex:0 0 36px">&gt;</button></div></div>`);
    const kinds = (await page.evaluate(probeControls)).map((f) => f.kind).sort();
    assert.deepEqual(kinds, ["control-overlap", "label-doubled", "pager-clipped"], JSON.stringify(kinds));
  } finally {
    await browser.close();
  }
});
