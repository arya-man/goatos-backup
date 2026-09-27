import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";

import plugin, { probeCardFit } from "./card-fit.mjs";

test("card-fit is a P0 390 check (guard: card-fit)", () => {
  assert.equal(plugin.p0, true);
  assert.deepEqual(plugin.profiles, ["390-dark"]);
});

test("probe flags a nested scroller and an overlapping row; passes the fixed versions", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 800 } });
    await page.setContent(`<div class="MuiCard-root" style="width:358px;font:14px sans-serif">
      <div id="scroll" style="overflow:auto;min-height:40px"><div role="list" style="min-width:360px;display:flex;flex-direction:column">Price list</div></div><div id="strip" style="overflow:auto"><div style="display:flex;min-width:800px">strip</div></div>
      <div id="row" style="display:flex;justify-content:space-between;width:200px"><span>A very long pen name here that wraps</span><span>197 g</span></div>
    </div>`);
    const bad = await page.evaluate(probeCardFit);
    assert.deepEqual(bad.map((f) => f.pattern).sort(), ["card-inner-scroll", "row-overlap"]);
    await page.evaluate(() => {
      document.querySelector("#scroll > div").style.minWidth = "0";
      const row = document.querySelector("#row");
      row.style.gap = "16px";
      row.children[0].style.minWidth = "0"; row.children[0].style.overflowWrap = "anywhere";
      row.children[1].style.flexShrink = "0";
    });
    assert.deepEqual(await page.evaluate(probeCardFit), []);
  } finally {
    await browser.close();
  }
});

test("the two REVIEW-24 callers pass their declared overrides", () => {
  const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
  assert.match(read("../../features/procurement/sales-sold.tsx"), /slotProps=\{\{ scrollbar: \{ minHeight: \{ xs: "auto", sm: 384 \} \}, list: \{ minWidth: \{ xs: 0, sm: 360 \} \} \}\}\s*wrapNames/);
  assert.match(read("../../features/weighing/weights-analytics.tsx"), /slotProps=\{\{ row: \{ gap: 2, "& > span:first-of-type": \{ minWidth: 0, overflowWrap: "anywhere" \}/);
});
