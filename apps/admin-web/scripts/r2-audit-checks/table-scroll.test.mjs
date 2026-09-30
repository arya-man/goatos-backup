import test from "node:test";
import assert from "node:assert/strict";
import { chromium } from "playwright";

import plugin, { probeTableScroll } from "./table-scroll.mjs";

// guard: sticky-identity + table-scroll-trap (FIXJ11, J3B N-P1-1 / P2-1)
test("table-scroll is a P0 390 check", () => {
  assert.equal(plugin.p0, true);
  assert.deepEqual(plugin.profiles, ["390-dark"]);
});

const row = (n) => `<tr><td>Pen ${n}</td>${Array.from({ length: 8 }, (_, i) => `<td>cell ${i} with text</td>`).join("")}<td><button data-row-menu>⋮</button></td></tr>`;
const html = (sticky, trap) => `<style>
  body{margin:0;font:14px sans-serif;background:#141a21;color:#fff}
  td{white-space:nowrap;padding:8px 16px}
  ${sticky ? "td:first-child{position:sticky;left:0;z-index:2;background:#1c252e} td:last-child{position:sticky;right:0;z-index:2;background:#1c252e}" : ""}
</style>
<div id="scroller" aria-label="Pen x Vaccine" style="width:358px;overflow-x:auto;${trap ? "overflow-y:auto;height:120px" : ""}">
  <table><tbody>${[1, 2, 3, 4].map(row).join("")}</tbody></table>
</div>`;

test("probe flags an unpinned wide table and a vertical trap; passes the pinned one", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 800 } });
    await page.setContent(html(false, true));
    const bad = await page.evaluate(probeTableScroll);
    assert.deepEqual([...new Set(bad.map((f) => f.pattern))].sort(), ["sticky-identity", "table-scroll-trap"]);
    assert.ok(bad.some((f) => /first column scrolled away/.test(f.detail)), JSON.stringify(bad));
    assert.ok(bad.some((f) => /last column scrolled away/.test(f.detail)), JSON.stringify(bad));
    await page.setContent(html(true, false));
    assert.deepEqual(await page.evaluate(probeTableScroll), []);
    // restores the reader's scroll position
    assert.equal(await page.evaluate(() => document.querySelector("#scroller").scrollLeft), 0);
    // a see-through pinned cell is still a failure (the scrolling cells show through it)
    await page.addStyleTag({ content: "td:first-child{background:transparent !important}" });
    const seeThrough = await page.evaluate(probeTableScroll);
    assert.ok(seeThrough.some((f) => /see-through/.test(f.detail)), JSON.stringify(seeThrough));
  } finally {
    await browser.close();
  }
});
