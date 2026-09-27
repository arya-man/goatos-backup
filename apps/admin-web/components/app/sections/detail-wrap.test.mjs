// guard: detail-row-wrap (REVIEW-27 O33). Template detail rows print the value as a bare text node
// beside a 120px label; a long unbroken value (a request id) overflowed the card at 390 and 1440.
// Every caller passes the declared override (detailWrapSx) and it keeps such a value inside the card.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { chromium } from "playwright";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("delivery rows and customer lines take the declared wrap override at every call site", () => {
  for (const f of ["ceo-ai-admin/trace-viewer.tsx", "procurement/load-detail.tsx", "vaccination-execution/shed-drilldown.tsx", "process-integrity/workflow-drilldown.tsx"]) {
    const src = read(`../../../features/${f}`);
    for (const m of src.matchAll(/<OrderDetails(Delivery|Customer)\b[^>]*/g)) {
      assert.match(m[0], m[1] === "Delivery" ? /slotProps=\{\{ row: detailWrapSx \}\}/ : /slotProps=\{\{ line: detailWrapSx \}\}/, `${f}: ${m[0].slice(0, 60)}`);
    }
  }
  assert.match(read("../detail-wrap.ts"), /minWidth: 0, overflowWrap: 'anywhere'/);
});

test("with the override a 64-char unbroken value stays inside a 358px card; without it, it overflows", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 600 } });
    const row = (extra) => `<div class="card" style="width:358px;overflow:hidden;font:14px sans-serif"><div style="display:flex;flex-direction:column;padding:24px">
      <div class="row" style="display:flex;align-items:center;${extra}"><span style="width:120px;flex-shrink:0">Request</span>req_${"0123456789abcdef".repeat(4)}</div></div></div>`;
    const inside = () => page.evaluate(() => {
      const r = document.querySelector(".row"); const card = document.querySelector(".card").getBoundingClientRect();
      const range = document.createRange(); range.selectNodeContents(r.lastChild); const t = range.getBoundingClientRect();
      return t.right <= card.right - 24 + 1;
    });
    await page.setContent(row(""));
    assert.equal(await inside(), false, "fixture reproduces the overflow");
    await page.setContent(row("min-width:0;overflow-wrap:anywhere"));
    assert.equal(await inside(), true);
  } finally {
    await browser.close();
  }
});
