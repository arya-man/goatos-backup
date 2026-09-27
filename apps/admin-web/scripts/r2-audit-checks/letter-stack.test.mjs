import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

import plugin, { probeLetterStack } from "./letter-stack.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

test("letter-stack plugin is a P0 r2 audit check (guard: letter-stack)", () => {
  assert.equal(plugin.name, "text");
  assert.equal(plugin.p0, true);
  assert.equal(typeof plugin.run, "function");
});

test("probe flags a squeezed cell and passes a normal one", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 600, height: 400 } });
    await page.setContent(`<table style="font:14px sans-serif"><tr>
      <td style="width:8px;max-width:8px;word-break:break-all">Godel</td>
      <td>Yashoda</td><td style="width:8px;max-width:8px">—</td></tr></table>`);
    const found = await page.evaluate(probeLetterStack);
    assert.equal(found.length, 1, JSON.stringify(found));
    assert.match(found[0].detail, /"Godel"/);
  } finally {
    await browser.close();
  }
});

// Source pin for the case that raised it: the drive roster keeps the template table kit on laptops
// (Scrollbar + minWidth + nowrap identity cells) and a stacked row per animal on phones.
test("/calendar/drive roster: table minWidth + nowrap cells, stacked phone rows", () => {
  const src = readFileSync(join(root, "features/calendar/calendar-drive-detail.tsx"), "utf8");
  assert.match(src, /<Scrollbar>\s*<Table sx=\{\{ minWidth: 960,[\s\S]{0,160}wordBreak: "normal"[\s\S]{0,60}"& td \.celllink": \{ whiteSpace: "nowrap" \}/);
  assert.match(src, /display: \{ xs: "block", md: "none" \}[\s\S]{0,400}<Stack spacing=\{0\.5\}/);
});
