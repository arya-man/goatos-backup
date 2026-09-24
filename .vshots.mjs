import { chromium } from "playwright";
const BASE = "http://127.0.0.1:3423", OUT = process.argv[2];
const browser = await chromium.launch();
async function shootEl(w, h, name, url, sel) {
  const ctx = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: 2 });
  const p = await ctx.newPage();
  await p.goto(`${BASE}${url}`, { waitUntil: "domcontentloaded", timeout: 120000 });
  const el = p.locator(sel).first();
  await el.waitFor({ timeout: 60000 });
  await p.waitForTimeout(2500);
  await el.scrollIntoViewIfNeeded();
  await p.waitForTimeout(600);
  await el.screenshot({ path: `${OUT}/${name}.png` });
  // and whether the page itself scrolls sideways at this width
  const over = await p.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth + 1);
  console.log(`${name}: sideways page scroll = ${over}`);
  await ctx.close();
}
await shootEl(1440, 1200, "fv-laptop", "/sales/farm-value", "main");
await shootEl(390, 900, "fv-phone", "/sales/farm-value", "main");
await browser.close();
