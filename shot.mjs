import { chromium } from "playwright";
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1440, height: 1100 } });
const errs = []; p.on("pageerror", e => errs.push(String(e)));
await p.goto("http://localhost:3421/feed/analytics?tab=followup", { waitUntil: "domcontentloaded" });
await p.waitForSelector("table.ffu-table tbody tr", { timeout: 30000 });
await p.waitForTimeout(700);
await p.screenshot({ path: "/tmp/ffu-shots/50-after-fix.png", fullPage: true });
const rows = p.locator("table.ffu-table tbody tr");
const n = await rows.count();
console.log("lines:", n);
for (let i = 0; i < Math.min(8, n); i++) {
  console.log("  " + (await rows.nth(i).innerText()).replace(/\n/g, " | ").replace(/\t/g, "  "));
}
console.log("errors:", errs.length ? errs.slice(0,2) : "none");
await b.close();
