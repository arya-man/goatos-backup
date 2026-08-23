import { chromium } from "@playwright/test";
import fs from "node:fs";

const TOK = fs.readFileSync("/tmp/hs.tok", "utf8").trim();
const TENANT = "00000000-0000-4000-8000-000000000001";

async function main() {
  const browser = await chromium.launch({ channel: "chrome" });
  const context = await browser.newContext();
  await context.addCookies([]);
  const page = await context.newPage();
  // Set localStorage / auth similar to app expectations - use bearer via API is server-side;
  // admin-web itself mints its own token server-side, so we just navigate and let it proxy.
  await page.goto("http://127.0.0.1:3318/herd-signals", { waitUntil: "networkidle" });
  console.log("URL after nav:", page.url());
  await page.waitForTimeout(4000);
  fs.writeFileSync("/tmp/hs-page.html", await page.content());
  await page.screenshot({ path: "/tmp/hs-live-page.png", fullPage: true });

  // Find A00031 row and click it, then Expand.
  const rowText = page.locator("text=A00031").first();
  await rowText.waitFor({ timeout: 15000 });
  await rowText.click();
  await page.waitForTimeout(1500);
  await page.screenshot({ path: "/tmp/hs-after-row-click.png", fullPage: true });
  const expandBtn = page.locator("a:has-text('Expand'), button:has-text('Expand')");
  await expandBtn.waitFor({ timeout: 10000 });
  await expandBtn.click();

  const dialog = page.locator("[role='dialog']");
  await dialog.waitFor({ timeout: 10000 });

  // Switch to 30d range so the known vaccination event (2026-08-01) is in-window.
  const rangeBtn = page.locator(".rangepick button:has-text('30d')");
  await rangeBtn.click();

  // wait for activity table to populate
  await page.waitForTimeout(2500);

  const evlineCountBefore = await page.locator(".hchart .evline").count();
  const rowCountBefore = await page.locator("table.resp tbody tr").count();
  console.log("Before toggle off: evline markers =", evlineCountBefore, " table rows =", rowCountBefore);

  // Toggle Vaccination chip off
  const chip = page.locator(".evchip:has-text('Vaccination')");
  await chip.click();
  await page.waitForTimeout(500);

  const evlineCountAfter = await page.locator(".hchart .evline").count();
  const rowCountAfter = await page.locator("table.resp tbody tr").count();
  const emptyMsg = await page.locator("text=All recorded activity is hidden").count();
  console.log("After toggle off: evline markers =", evlineCountAfter, " table rows =", rowCountAfter, " empty-msg =", emptyMsg);

  // Toggle back on
  await chip.click();
  await page.waitForTimeout(500);
  const evlineCountRestored = await page.locator(".hchart .evline").count();
  const rowCountRestored = await page.locator("table.resp tbody tr").count();
  console.log("After toggle back on: evline markers =", evlineCountRestored, " table rows =", rowCountRestored);

  await page.screenshot({ path: "/tmp/hs-overlay-verify.png", fullPage: true });

  await browser.close();

  const pass =
    evlineCountBefore >= 1 &&
    rowCountBefore >= 1 &&
    evlineCountAfter === 0 &&
    emptyMsg >= 1 &&
    evlineCountRestored >= 1 &&
    rowCountRestored >= 1;
  console.log(pass ? "PASS" : "FAIL");
  process.exit(pass ? 0 : 1);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
