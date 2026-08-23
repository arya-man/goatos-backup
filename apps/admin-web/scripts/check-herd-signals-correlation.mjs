#!/usr/bin/env node
/** @typedef {import("playwright").chromium} */
//
// This proof was previously false-green: a missing grid, a missing dialog, a missing correlation
// table, and all-dash correlation values were all logged as "⚠ warning" and the script still
// printed "Test complete" and exited 0. A check that cannot fail proves nothing — this repo has
// been burned by exactly that shape of bug once already (a no-reload proof that read
// performance.getEntriesByType('navigation').length, which a reload resets to 1, so it could
// never fail). Every one of those four conditions is now a hard failure: it throws with a message
// naming what was EXPECTED and what was FOUND, and the script exits non-zero.
//
// The tag driven here (A00031) is a documented fixture with one real recorded vaccination event
// in-window (see docs/modules/herd-signals.md and the herd-signals worktree notes) — most mapped
// tags in this seed legitimately have zero activity events because the activity window is clamped
// to each tag's monitoring boundary, so a script that clicked "the first row" (as this one used
// to) would show an empty correlation table on almost every run and could never meaningfully
// exercise this screen.

import { chromium } from "@playwright/test";

const ADMIN_WEB_URL = process.env.ADMIN_WEB_URL || "http://127.0.0.1:3318";
const API_URL = process.env.API_URL || "http://127.0.0.1:8098";
const TENANT_ID = process.env.TENANT_ID || "00000000-0000-4000-8000-000000000001";
// A00031 has one known vaccination event inside its monitoring window (2026-08-01) — override via
// env if that fixture ever moves.
const TAG_ID = process.env.HERD_SIGNALS_TAG_ID || "A00031";

class ProofFailure extends Error {
  constructor(what, expected, found) {
    super(`${what}\n    expected: ${expected}\n    found:    ${found}`);
    this.name = "ProofFailure";
  }
}

function fail(what, expected, found) {
  throw new ProofFailure(what, expected, found);
}

async function main() {
  console.log("Testing herd-signals activity correlation rendering...");
  console.log("Admin-web URL:", ADMIN_WEB_URL);
  console.log("API URL:", API_URL);
  console.log("Tenant ID:", TENANT_ID);
  console.log("Tag ID:", TAG_ID);

  const browser = await chromium.launch({ channel: "chrome", headless: process.env.HEADLESS !== "false" });
  try {
    const context = await browser.newContext({
      extraHTTPHeaders: {
        "X-Tenant-Id": TENANT_ID,
      },
    });
    const page = await context.newPage();

    console.log("\nNavigating to herd-signals live page...");
    await page.goto(`${ADMIN_WEB_URL}/herd-signals`, { waitUntil: "networkidle" });

    // 1) THE GRID. Previously: waitForSelector("[role='grid']").catch(() => console.warn(...)) —
    // a missing grid never stopped the script, and it turns out `[role="grid"]` never existed on
    // this screen at all (the live table uses `tr[role="button"]` rows, not an ARIA grid widget),
    // so this check was not merely soft, it was UNCONDITIONALLY dead: it could never once have
    // passed and nobody noticed because the failure was swallowed. Fixed to match real markup.
    const grid = page.locator("tr[role='button']");
    const gridVisible = await grid
      .first()
      .waitFor({ timeout: 10000, state: "visible" })
      .then(() => true)
      .catch(() => false);
    if (!gridVisible) {
      fail("Live tag signals table did not render", "at least one tr[role='button'] data row visible within 10s", "no tr[role='button'] element became visible");
    }
    console.log("✓ Live tag signals table rendered");

    // Find and click the fixture tag's full-row hit-target, not "the first row" — most rows have
    // zero activity events (see module note above), so "first row" proved nothing on most runs.
    const row = page.locator("tr[role='button']", { hasText: TAG_ID }).first();
    const rowFound = await row
      .waitFor({ timeout: 10000, state: "visible" })
      .then(() => true)
      .catch(() => false);
    if (!rowFound) {
      fail(`Row for tag ${TAG_ID} did not render in the grid`, `a tr[role='button'] containing "${TAG_ID}" visible within 10s`, "no matching row became visible — check the tag is still mapped and present in this tenant's seed");
    }
    await row.locator(".hs-row-hit").click();
    console.log(`Clicked row for ${TAG_ID}, waiting for drawer...`);

    // 2) THE DIALOG. Previously: waitForSelector("[role='dialog']").catch(() => console.warn(...))
    // — but the SIDE drawer (`aside.drawer`, opened by a row click) carries no `role="dialog"` at
    // all; only the FULL-SCREEN view opened via its "Expand" button does (see
    // herd-signals-history-fullscreen.tsx: `role="dialog"` on the `.fs` element). So this
    // assertion, run right after the row click and before Expand is ever clicked, could also
    // never have passed — same unconditionally-dead shape as the grid check above. Check the
    // actual side drawer here (`aside.drawer.on`), and check `role="dialog"` after Expand below.
    const drawer = page.locator("aside.drawer.on");
    const drawerVisible = await drawer
      .waitFor({ timeout: 5000, state: "visible" })
      .then(() => true)
      .catch(() => false);
    if (!drawerVisible) {
      fail("Tag detail drawer did not open", "an aside.drawer.on element visible within 5s of clicking the row", "no aside.drawer.on element became visible");
    }
    console.log("Drawer opened.");

    // The known event (2026-08-01) is outside the default 24h window — switch to 30d so it is
    // actually in-window. Reach the full-screen correlation table via Expand (real user path).
    const expandBtn = page.locator("a:has-text('Expand'), button:has-text('Expand')");
    const expandFound = await expandBtn
      .first()
      .waitFor({ timeout: 5000, state: "visible" })
      .then(() => true)
      .catch(() => false);
    if (!expandFound) {
      fail("Expand control did not render in the drawer", "an Expand link/button visible within 5s", "no Expand control became visible");
    }
    await expandBtn.first().click();

    const fsDialog = page.locator("[role='dialog']");
    const fsVisible = await fsDialog
      .first()
      .waitFor({ timeout: 5000, state: "visible" })
      .then(() => true)
      .catch(() => false);
    if (!fsVisible) {
      fail("Full-screen history view did not open after clicking Expand", "a [role='dialog'] element visible within 5s of clicking Expand", "no [role='dialog'] element became visible");
    }

    const rangeBtn = page.locator(".fs .rangepick button:has-text('30d')");
    await rangeBtn.click();
    await page.waitForTimeout(1500);

    const timestamp = new Date().toISOString().replace(/[:.]/g, "-");
    await page.screenshot({ path: `./.codex-proof/herd-signals-correlation-${timestamp}.png`, fullPage: true }).catch(() => {});

    // 3) THE TABLE. Previously: correlationTable.waitFor(...).catch(() => console.warn(...)), then
    // an `if (tableVisible)` branch that just skipped everything and still printed "Test
    // complete" when the table never showed up. Now hard.
    //
    // Also fixed: `table.resp:has-text('Activity around recorded farm activity')` could never
    // match, ever — that heading is an `<h3>` sibling of the `<table>`, not text inside it (see
    // herd-signals-history-fullscreen.tsx ~395). Scope to the containing card instead.
    const correlationCard = page
      .locator("h3", { hasText: "Activity around recorded farm activity" })
      .locator("xpath=ancestor::div[contains(concat(' ', normalize-space(@class), ' '), ' card ')][1]");
    const correlationTable = correlationCard.locator("table.resp").first();
    const tableVisible = await correlationTable
      .waitFor({ timeout: 8000, state: "visible" })
      .then(() => true)
      .catch(() => false);
    if (!tableVisible) {
      fail(
        "Activity correlation table did not render",
        "a table.resp containing 'Activity around recorded farm activity' visible within 8s of switching to the 30d range",
        "no matching table became visible",
      );
    }
    console.log("\n✓ Activity correlation table is visible");

    // The table renders exactly ONE placeholder <tr><td colspan=6>...</td></tr> for its
    // loading/error/empty states (see herd-signals-history-fullscreen.tsx ~374-408) — that is
    // real, correct markup for "this tag legitimately has zero activity events in this window"
    // (most tags do — see the module note at the top of this file), NOT a broken correlation
    // table. Naively counting every <tr> conflated that placeholder with a real 6-cell event row
    // and would have misreported "0 rows" as "1 row, all dashes". Only rows with 6 real <td>
    // cells (no colspan placeholder) count as event rows.
    const allRows = correlationTable.locator("tbody tr");
    const allRowCount = await allRows.count();
    const eventRows = [];
    for (let i = 0; i < allRowCount; i++) {
      const tr = allRows.nth(i);
      const placeholderCount = await tr.locator("td[colspan]").count();
      if (placeholderCount > 0) continue;
      eventRows.push(await tr.locator("td").allTextContents());
    }
    console.log(`  ${allRowCount} <tr> total, ${eventRows.length} real event row(s)`);
    if (eventRows.length === 0) {
      fail(
        `Correlation table for ${TAG_ID} has no event rows`,
        `at least 1 event row for the known in-window vaccination event on tag ${TAG_ID}`,
        `0 event rows (table showed its empty/placeholder state) — either the fixture event is gone, the tag's monitoring boundary moved, or event.kind/overlay filtering is broken`,
      );
    }

    // 4) ALL-DASH CORRELATION VALUES. Previously: computed `hasDashes` for the FIRST row only,
    // then just logged "⚠ Correlation values are showing dashes" and carried on to "Test
    // complete" regardless. Now: check EVERY real event row, and fail hard if every row's
    // before/after/change cell is unpopulated — that is the shape of a correlation computation
    // that never ran, not a few individually-incomplete windows.
    eventRows.forEach((cells, i) =>
      console.log(`\n  Row ${i}: ${cells[0]?.trim()} | ${cells[1]?.trim()} | before=${cells[2]?.trim()} after=${cells[3]?.trim()} change=${cells[4]?.trim()}`),
    );
    const everyRowAllDash = eventRows.every((cells) => cells.slice(2, 5).every((text) => text.trim() === "—"));
    if (everyRowAllDash) {
      fail(
        "Every correlation row has dash-only before/after/change values",
        "at least one row with a populated 2h-before, 2h-after, or change value",
        `all ${eventRows.length} event row(s) showed "—" for before, after, and change — the correlation computation for tag ${TAG_ID} produced nothing to correlate against`,
      );
    }
    console.log("\n✓ At least one row has populated correlation values");

    await correlationTable.screenshot({ path: `./.codex-proof/herd-signals-correlation-table-${timestamp}.png` }).catch(() => {});

    await context.close();
  } finally {
    await browser.close();
  }

  console.log("\nAll herd-signals correlation checks passed.");
}

main().catch((err) => {
  console.error("\n✗ herd-signals correlation proof FAILED:\n");
  console.error(err instanceof ProofFailure ? err.message : err);
  process.exit(1);
});
