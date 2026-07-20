import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const baseUrl = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300").replace(/\/$/, "");
const actionCenterPath = "/action-center?scope_mode=company&ac_page=1&ac_limit=10&verify_page=1&verify_limit=10";
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const screenshotDir = resolve(process.env.GOATOS_ACTION_CENTER_DRAWER_SCREENSHOT_DIR ?? `${repoRoot}/.codex-goatos-render/action-center-local-drawer`);
mkdirSync(screenshotDir, { recursive: true });

const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage();

try {
  await page.goto(`${baseUrl}${actionCenterPath}`, { waitUntil: "networkidle", timeout: 30_000 });
  const firstCard = page.locator(".taskboard .task-ac").first();
  if ((await firstCard.count()) !== 1) throw new Error("Action Center requires at least one work card for the drawer regression");

  const samePageRequests = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin !== new URL(baseUrl).origin || url.pathname !== "/action-center") return;
    if (!["document", "fetch"].includes(request.resourceType())) return;
    samePageRequests.push({ method: request.method(), resourceType: request.resourceType(), url: url.toString() });
  });

  const startedAt = performance.now();
  await firstCard.click();
  await page.locator(".drawer.on").waitFor({ state: "visible", timeout: 5_000 });
  await waitForDrawerSettled(page);
  const openMs = Math.round(performance.now() - startedAt);
  assertNoSamePageRequests(samePageRequests, "opening the drawer");
  await page.screenshot({ path: `${screenshotDir}/desktop-open.png`, fullPage: false });
  if (!new URLSearchParams(new URL(page.url()).hash.replace(/^#/, "")).has("ac_row")) {
    throw new Error("opening the drawer did not add ac_row to browser history");
  }

  await page.goBack().catch(() => undefined);
  await page.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  assertNoSamePageRequests(samePageRequests, "closing the drawer with Back");

  await firstCard.click();
  await page.locator(".drawer.on").waitFor({ state: "visible", timeout: 2_000 });
  await page.locator('[data-testid="action-center-drawer-scrim"]').click({ position: { x: 8, y: 8 } });
  await page.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  assertNoSamePageRequests(samePageRequests, "closing the drawer from the scrim");

  await firstCard.click();
  await page.locator(".drawer.on").waitFor({ state: "visible", timeout: 2_000 });
  await page.locator(".drawer.on button.iconbtn").click();
  await page.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  assertNoSamePageRequests(samePageRequests, "closing the drawer from the close button");

  await firstCard.dblclick();
  await page.locator(".drawer.on").waitFor({ state: "visible", timeout: 2_000 });
  await page.locator(".drawer.on button.iconbtn").click();
  await page.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  if (new URL(page.url()).hash) throw new Error("double-click created duplicate drawer history entries");
  assertNoSamePageRequests(samePageRequests, "double-clicking and closing the drawer");

  await firstCard.click();
  await page.locator(".drawer.on").waitFor({ state: "visible", timeout: 2_000 });
  await page.keyboard.press("Escape");
  await page.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  assertNoSamePageRequests(samePageRequests, "closing the drawer with Escape");

  await page.setViewportSize({ width: 390, height: 900 });
  await firstCard.click();
  const narrowDrawer = page.locator(".drawer.on");
  await narrowDrawer.waitFor({ state: "visible", timeout: 2_000 });
  await waitForDrawerSettled(page);
  const narrowLayout = await narrowDrawer.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const style = getComputedStyle(element);
    return { left: rect.left, right: rect.right, width: rect.width, viewportWidth: window.innerWidth, documentWidth: document.documentElement.scrollWidth, cssWidth: style.width, cssMaxWidth: style.maxWidth, transform: style.transform };
  });
  if (narrowLayout.left < -1 || narrowLayout.right > narrowLayout.viewportWidth + 1 || narrowLayout.documentWidth > narrowLayout.viewportWidth + 1) {
    throw new Error(`narrow drawer overflow: ${JSON.stringify(narrowLayout)}`);
  }
  await page.screenshot({ path: `${screenshotDir}/narrow-open.png`, fullPage: false });
  await page.keyboard.press("Escape");
  await page.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  assertNoSamePageRequests(samePageRequests, "narrow drawer interaction");

  const deepLinkHref = await firstCard.getAttribute("href");
  if (!deepLinkHref) throw new Error("Action Center card is missing its refreshable drawer href");
  const deepLinkPage = await context.newPage();
  await deepLinkPage.goto(new URL(deepLinkHref, baseUrl).toString(), { waitUntil: "networkidle", timeout: 30_000 });
  await deepLinkPage.locator(".drawer.on").waitFor({ state: "visible", timeout: 2_000 });
  await waitForDrawerSettled(deepLinkPage);
  await deepLinkPage.locator(".drawer.on button.iconbtn").click();
  await deepLinkPage.locator(".drawer.on").waitFor({ state: "hidden", timeout: 2_000 });
  await deepLinkPage.close();

  if (await page.locator("html.route-busy").count()) throw new Error("local drawer navigation incorrectly triggered the global route-pending UI");
  console.log(`PASS Action Center local drawer: one-click open in ${openMs}ms; double-click idempotent; desktop+narrow; Back/scrim/button/Escape close; refreshable deep link; 0 same-page requests; screenshots=${screenshotDir}`);
} finally {
  await browser.close();
}

function assertNoSamePageRequests(requests, action) {
  if (requests.length === 0) return;
  throw new Error(`${action} triggered ${requests.length} Action Center route request(s): ${requests.map((request) => request.url).join(", ")}`);
}

async function waitForDrawerSettled(page) {
  await page.waitForFunction(() => {
    const drawer = document.querySelector(".drawer.on");
    return drawer instanceof HTMLElement && getComputedStyle(drawer).transform === "none";
  });
}
