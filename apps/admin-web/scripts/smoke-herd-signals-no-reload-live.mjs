// Regression guard for the herd-signals row-click drawer.
//
// A prior build regressed the tag-detail row link into a plain full-page navigation: the click
// still "worked" (a drawer appeared) but only because the browser reloaded the whole document and
// server-rendered the drawer open, discarding all client state. `performance.getEntriesByType
// ("navigation").length` cannot catch this -- a reload always resets it back to 1, so that check
// passes whether or not a reload happened. Instead this script plants a marker on `window` before
// the click; the marker can only survive if the click was handled client-side (LocalOverlayLink's
// history.pushState path) without the document being torn down and reloaded.
import { chromium } from "@playwright/test";

const baseUrl = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3318").replace(/\/$/, "");
const herdSignalsPath = "/herd-signals?scope_mode=company";

const browser = await chromium.launch({ channel: "chrome" });
const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
const page = await context.newPage();

try {
  await page.goto(`${baseUrl}${herdSignalsPath}`, { waitUntil: "networkidle", timeout: 30_000 });

  const row = page.locator('table tbody tr a[href*="hs_tag="]').first();
  if ((await row.count()) !== 1) throw new Error("herd-signals live table requires at least one row with a tag-detail link");

  // Plant a marker a full reload cannot survive. A client-side history.pushState navigation leaves
  // it in place; document.open()/unload from a real navigation wipes it.
  await page.evaluate(() => {
    window.__meshaReloadProbe = "alive";
  });

  await row.click();

  await page.locator(".drawer.on").waitFor({ state: "visible", timeout: 5_000 });

  const probeSurvived = await page.evaluate(() => typeof window.__meshaReloadProbe === "string");
  if (!probeSurvived) {
    throw new Error("row click reloaded the page (window.__meshaReloadProbe did not survive) instead of opening the drawer client-side");
  }

  const overlayState = await page.evaluate(() => {
    const state = window.history.state;
    return Boolean(state && typeof state === "object" && state.__meshaLocalOverlay);
  });
  if (!overlayState) {
    throw new Error("row click did not push a __meshaLocalOverlay history entry -- LocalOverlayLink's client-side path did not run");
  }

  const drawerLayout = await page.locator(".drawer.on").evaluate((element) => {
    const style = getComputedStyle(element);
    return { width: style.width, position: style.position, right: style.right };
  });
  if (drawerLayout.position !== "fixed") {
    throw new Error(`drawer is not a fixed side panel: position=${drawerLayout.position} width=${drawerLayout.width}`);
  }

  console.log(`PASS herd-signals row click: no reload (marker survived), drawer opened client-side as a fixed ${drawerLayout.width} side panel`);
} finally {
  await browser.close();
}
