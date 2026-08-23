#!/usr/bin/env node
/** @typedef {import("playwright").chromium} */

import { chromium } from "@playwright/test";

const ADMIN_WEB_URL = process.env.ADMIN_WEB_URL || "http://127.0.0.1:3318";
const API_URL = process.env.API_URL || "http://127.0.0.1:8098";
const TENANT_ID = process.env.TENANT_ID || "00000000-0000-4000-8000-000000000001";

async function main() {
  console.log("Testing herd-signals activity correlation rendering...");
  console.log("Admin-web URL:", ADMIN_WEB_URL);
  console.log("API URL:", API_URL);
  console.log("Tenant ID:", TENANT_ID);

  const browser = await chromium.launch({ headless: false });
  try {
    const context = await browser.newContext({
      extraHTTPHeaders: {
        "X-Tenant-Id": TENANT_ID,
      },
    });
    const page = await context.newPage();

    // Navigate to the herd-signals live page
    console.log("\nNavigating to herd-signals live page...");
    await page.goto(`${ADMIN_WEB_URL}/herd-signals/live`, { waitUntil: "networkidle" });

    // Wait for the table to load
    await page.waitForSelector("[role='grid']", { timeout: 10000 }).catch(() => {
      console.warn("Grid not found, may not be loaded yet");
    });

    console.log("Page loaded.");

    // Try to click on a tag row to open the history drawer
    const tagRows = page.locator('[role="row"]');
    const count = await tagRows.count();

    if (count > 1) {
      console.log(`Found ${count} rows, clicking first data row...`);
      // Click the first data row (after header)
      await tagRows.nth(1).click();
      console.log("Clicked row, waiting for drawer...");

      // Wait for the full-screen drawer to open
      await page.waitForSelector('[role="dialog"]', { timeout: 5000 }).catch(() => {
        console.warn("Dialog not found");
      });

      console.log("Drawer opened.");

      // Take a screenshot of the drawer
      const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
      const screenshotPath = `./.codex-proof/herd-signals-correlation-${timestamp}.png`;
      await page.screenshot({ path: screenshotPath, fullPage: true });
      console.log(`Screenshot saved: ${screenshotPath}`);

      // Wait a moment for the activity data to load
      await page.waitForTimeout(2000);

      // Check if the correlation table has data
      const correlationTable = page.locator('table.resp:has-text("Activity around recorded farm activity")').first();
      const tableVisible = await correlationTable.isVisible().catch(() => false);

      if (tableVisible) {
        console.log("\n✓ Activity correlation table is visible");

        // Count rows in the table
        const rows = correlationTable.locator("tbody tr");
        const rowCount = await rows.count();
        console.log(`  ${rowCount} activity events found`);

        if (rowCount > 0) {
          // Check first row for correlation values
          const firstRow = rows.nth(0);
          const cells = firstRow.locator("td");
          const cellTexts = await cells.allTextContents();

          console.log("\n  First event details:");
          console.log(`    Activity: ${cellTexts[0]}`);
          console.log(`    When: ${cellTexts[1]}`);
          console.log(`    2h Before: ${cellTexts[2]}`);
          console.log(`    2h After: ${cellTexts[3]}`);
          console.log(`    Change: ${cellTexts[4]}`);

          // Check if values are rendered (not just dashes)
          const hasDashes = cellTexts.slice(2, 5).every((text) => text.trim() === "—");
          if (!hasDashes) {
            console.log("\n✓ Correlation values are populated!");
          } else {
            console.log("\n⚠ Correlation values are showing dashes (no data yet)");
          }

          // Take another screenshot of the correlation table
          await correlationTable.screenshot({ path: `./.codex-proof/herd-signals-correlation-table-${timestamp}.png` });
          console.log(`  Table screenshot saved`);
        }
      } else {
        console.log("\n⚠ Activity correlation table not visible");
      }
    } else {
      console.log("No tags found in table");
    }

    await context.close();
  } finally {
    await browser.close();
  }

  console.log("\nTest complete.");
}

main().catch((err) => {
  console.error("Error:", err);
  process.exit(1);
});
