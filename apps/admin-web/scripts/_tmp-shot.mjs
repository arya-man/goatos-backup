import { chromium } from "@playwright/test";
const OUT = "/private/tmp/claude-501/-Users-raviteja-mesha/043cba5f-5caf-4d15-9eaf-dc6fcb54d4df/scratchpad/redesign/sales-procurement";
const routes = process.argv.slice(2);
const b = await chromium.launch();
for (const [w, h] of [[1440, 1100], [390, 844]]) {
  for (const theme of ["dark", "light"]) {
    const ctx = await b.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: 1 });
    const p = await ctx.newPage();
    for (const r of routes) {
      const name = r.replace(/^\//, "").replace(/\//g, "-");
      try {
        await p.goto("http://127.0.0.1:3310" + r, { waitUntil: "domcontentloaded", timeout: 300000 });
        await p.evaluate((t) => { document.documentElement.classList.toggle("dark", t === "dark"); document.documentElement.setAttribute("data-theme", t); }, theme);
        await p.waitForTimeout(2500);
        await p.screenshot({ path: `${OUT}/${name}-${theme}-${w}.png`, fullPage: w === 1440 });
        console.log("ok", name, theme, w);
      } catch (e) { console.log("FAIL", name, theme, w, String(e).slice(0, 120)); }
    }
    await ctx.close();
  }
}
await b.close();
