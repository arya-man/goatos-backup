import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

import plugin, { probeColumnFit } from "./column-fit.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

test("column-fit plugin is a P0 r2 audit check (guard: url-panel-min-width)", () => {
  assert.equal(plugin.name, "layout");
  assert.equal(plugin.p0, true);
  assert.equal(typeof plugin.run, "function");
});

// The /sales/loads shape: the page grid, a header, then a display:contents panel holding a wide table.
const page = (panelSx) => `<style>
  .wrap{width:600px}
  .wrap>.screen{display:grid;gap:24px;min-width:0}
  .wrap>.screen>*{min-width:0}
  .panel{display:contents}
  ${panelSx}
</style>
<div class="wrap"><div class="screen">
  <header style="display:flex"><h1>Load wise</h1></header>
  <div class="panel"><section style="display:flex;flex-direction:column">
    <div style="overflow-x:auto"><table style="min-width:960px"><tr><td>x</td></tr></table></div>
  </section></div>
</div></div>`;

test("probe flags blocks widened by a display:contents panel and passes the fixed panel", async () => {
  const browser = await chromium.launch();
  try {
    const p = await browser.newPage({ viewport: { width: 800, height: 400 } });
    await p.setContent(page(""));
    const broken = await p.evaluate(probeColumnFit);
    assert.ok(broken.length >= 1, JSON.stringify(broken));
    assert.match(broken[0].detail, /in a 600px column/);
    await p.setContent(page(".panel>*{min-width:0}"));
    assert.deepEqual(await p.evaluate(probeColumnFit), []);
  } finally {
    await browser.close();
  }
});

test("UrlPanel gives its display:contents children min-width 0", () => {
  const src = readFileSync(join(root, "components/app/url-panel.tsx"), "utf8");
  assert.match(src, /display: "contents", "& > \*": \{ minWidth: 0 \}/);
});
