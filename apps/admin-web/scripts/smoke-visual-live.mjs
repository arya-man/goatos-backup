import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import AxeBuilder from "@axe-core/playwright";
import { TENANT_CONTEXT_HEADER } from "@goatos/api-client/constants";
import { chromium } from "@playwright/test";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";

const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const requiredEnv = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
const missing = requiredEnv.filter((key) => !process.env[key]);
const args = parseArgs(process.argv.slice(2));

if (missing.length > 0) {
  console.error(`Missing required live-smoke env: ${missing.join(", ")}`);
  process.exit(2);
}

const apiBaseUrl = trimTrailingSlash(process.env.GOATOS_API_BASE_URL);
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
const tenantId = process.env.GOATOS_TENANT_ID;
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const baselineDir = normalizeRepoPath(args.baselineDir ?? process.env.GOATOS_VISUAL_BASELINE_DIR);
const updateBaseline = args.updateBaseline || process.env.GOATOS_VISUAL_UPDATE_BASELINE === "1";
const requireBaseline = args.requireBaseline || process.env.GOATOS_VISUAL_REQUIRE_BASELINE === "1";
const maxDiffRatio = args.maxDiffRatio ?? Number(process.env.GOATOS_VISUAL_MAX_DIFF_RATIO ?? "0.01");
const screenshotDir = join(
  repoRoot,
  ".codex-goatos-render",
  "admin-web-screenshots",
  new Date().toISOString().replaceAll(/[:.]/g, "-"),
);
const diffDir = join(screenshotDir, "diffs");
let baselineCompared = 0;
let baselineUpdated = 0;

await waitForApp(appBaseUrl);
const goatId = await resolveSmokeGoatID(apiBaseUrl, bearerToken, tenantId);
const procurementLoadId = await resolveSmokeProcurementLoadID(apiBaseUrl, bearerToken, tenantId);
mkdirSync(screenshotDir, { recursive: true });
if (baselineDir) mkdirSync(diffDir, { recursive: true });

const routes = [
  { name: "login", path: "/login" },
  { name: "control-tower", path: "/" },
  { name: "action-center", path: "/action-center" },
  { name: "calendar", path: "/calendar" },
  { name: "protocol-adherence", path: "/protocol-adherence" },
  { name: "workflows", path: "/workflows" },
  { name: "vaccination", path: "/vaccination" },
  { name: "vaccination-execution", path: "/vaccination#execution" },
  { name: "procurement-source-entry", path: "/procurement/source-entry" },
  { name: "config", path: "/config?category=vaccination" },
  { name: "sops", path: "/sops" },
  { name: "counts-herd", path: "/counts/herd" },
  { name: "operations-audit", path: "/operations/audit" },
  { name: "goat-passport", path: `/goats/${encodeURIComponent(goatId)}` },
];
if (procurementLoadId) {
  routes.push({
    name: "procurement-load-detail",
    path: `/procurement/source-entry/loads/${encodeURIComponent(procurementLoadId)}`,
  });
}

const browser = await chromium.launch();
try {
  for (const viewport of [
    { label: "desktop", width: 1440, height: 1000 },
    { label: "narrow", width: 390, height: 900 },
  ]) {
    const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } });
    const page = await context.newPage();
    for (const route of routes) {
      const url = `${appBaseUrl}${appPath(route.path)}`;
      await page.goto(url, { waitUntil: "networkidle", timeout: 30_000 });
      const html = await page.content();
      assertHealthyHTML(route.name, html, bearerToken);
      await assertLayoutHealthy(page, route.name, viewport.label);
      await assertA11y(page, route.name, viewport.label);
      await assertCoreInteractions(page, route.name, viewport.label);
      await settleAtTop(page);
      const screenshotName = `${viewport.label}-${route.name}.png`;
      const screenshotPath = join(screenshotDir, screenshotName);
      await page.screenshot({ path: screenshotPath, fullPage: true });
      if (baselineDir) {
        compareOrUpdateBaseline(screenshotName, screenshotPath);
      }
    }
    await context.close();
  }
} finally {
  await browser.close();
}

writeFileSync(
  join(screenshotDir, "manifest.json"),
  JSON.stringify(
    {
      app_base_url: appBaseUrl,
      goat_id: goatId,
      routes: routes.map((route) => appPath(route.path)),
      baseline_dir: baselineDir ? relativeToRepo(baselineDir) : null,
      baseline_compared: baselineCompared,
      baseline_updated: baselineUpdated,
      max_diff_ratio: baselineDir ? maxDiffRatio : null,
    },
    null,
    2,
  ),
);

console.log(`screenshots_dir=${relativeToRepo(screenshotDir)}`);
console.log(`goat_id=${goatId}`);
console.log(`routes_captured=${routes.map((route) => appPath(route.path)).join(",")}`);
if (baselineDir) {
  console.log(`baseline_dir=${relativeToRepo(baselineDir)}`);
  console.log(`baseline_compared=${baselineCompared}`);
  console.log(`baseline_updated=${baselineUpdated}`);
}

async function waitForApp(url) {
  const deadline = Date.now() + 30_000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${url}/`, { cache: "no-store" });
      if (response.ok) return;
      lastError = new Error(`status ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`admin-web did not respond at ${url}: ${lastError instanceof Error ? lastError.message : String(lastError)}`);
}

function appPath(path) {
  return path;
}

async function resolveSmokeGoatID(baseUrl, token, tenant) {
  if (process.env.GOATOS_SMOKE_GOAT_ID) {
    return process.env.GOATOS_SMOKE_GOAT_ID;
  }
  const response = await fetch(`${baseUrl}/goats/search?limit=1`, {
    headers: { Authorization: `Bearer ${token}`, [TENANT_CONTEXT_HEADER]: tenant },
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`backend smoke goat lookup failed: status ${response.status}`);
  }
  const body = await response.json();
  const goatID = body?.items?.[0]?.goat_id;
  if (typeof goatID !== "string" || goatID.length === 0) {
    throw new Error("backend smoke goat lookup returned no goat_id for passport smoke");
  }
  return goatID;
}

async function resolveSmokeProcurementLoadID(baseUrl, token, tenant) {
  const response = await fetch(`${baseUrl}/procurement/source-entry/loads?limit=1`, {
    headers: { Authorization: `Bearer ${token}`, [TENANT_CONTEXT_HEADER]: tenant },
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`backend smoke procurement load lookup failed: status ${response.status}`);
  }
  const body = await response.json();
  const loadID = body?.items?.[0]?.load_id;
  return typeof loadID === "string" && loadID.length > 0 ? loadID : null;
}

function assertHealthyHTML(routeName, html, token) {
  const forbidden = [
    "Server configuration missing",
    "Bearer authentication failed",
    "Token is valid, but",
    "Backend service is not reachable",
    "Application error",
    "Runtime Error",
    "Trace ",
    "Rendered ",
  ];
  for (const marker of forbidden) {
    if (html.includes(marker)) {
      throw new Error(`${routeName} rendered failure marker: ${marker}`);
    }
  }
  if (token && html.includes(token)) {
    throw new Error(`${routeName} rendered GOATOS_BEARER_TOKEN into HTML`);
  }
}

async function assertLayoutHealthy(page, routeName, viewportLabel) {
  // Settle the page at the top BEFORE measuring. A hash route (e.g. /vaccination#execution) scrolls to its
  // anchor, and an in-flight smooth-scroll animation yields a transient negative main.top — a vertical
  // scroll artifact, not a layout defect. Force scroll-behavior to instant, scroll to the top, and let it
  // settle so the measurement reflects the resting layout. This does NOT touch the horizontal overflow or
  // card-clipping checks (they measure the same resting layout) — it only removes the main.top false positive.
  await settleAtTop(page);
  const layout = await page.evaluate(() => {
    window.scrollTo(0, 0);
    const root = document.documentElement;
    const overflow = root.scrollWidth - root.clientWidth;
    const main = document.querySelector("main")?.getBoundingClientRect();
    const panels = Array.from(document.querySelectorAll("section"))
      .filter(isVisible)
      .filter((element) => element.getBoundingClientRect().width > 120)
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.left < -1 || rect.right > root.clientWidth + 1;
      })
      .slice(0, 5)
      .map(describeElement);
    const clippedControls = Array.from(document.querySelectorAll("a, button"))
      .filter(isVisible)
      .filter((element) => (element.textContent ?? "").trim().length > 0)
      .filter((element) => element.scrollWidth > element.clientWidth + 2 || element.scrollHeight > element.clientHeight + 8)
      .slice(0, 5)
      .map(describeElement);
    const clippedNavLabels = Array.from(document.querySelectorAll('nav[aria-label^="Mesha"] span'))
      .filter(isVisible)
      .filter((element) => (element.textContent ?? "").trim().length > 0)
      .filter((element) => element.scrollWidth > element.clientWidth + 1)
      .slice(0, 5)
      .map(describeElement);
    const navLabelRects = Array.from(document.querySelectorAll('nav[aria-label^="Mesha"] a span:last-child, nav[aria-label^="Mesha"] button span:last-child'))
      .filter(isVisible)
      .filter((element) => (element.textContent ?? "").trim().length > 0)
      .map((element) => element.getBoundingClientRect());
    const navLabelLefts = navLabelRects.map((rect) => Math.round(rect.left));
    const navLabelSpread = navLabelLefts.length > 1 ? Math.max(...navLabelLefts) - Math.min(...navLabelLefts) : 0;
    // WCAG 2.5.8 (Target Size Minimum) exempts targets rendered INLINE within a sentence. True
    // display:inline prose anchors (e.g. ".lk" cross-references — "…ripples into Protocol Adherence and the
    // Control Tower.") are not standalone tap targets: they report a 0 content box and their wrapped-line
    // rects overlap. Exempt ONLY display:inline anchors from the small-target + overlap checks. Every real
    // control (buttons and .btn/.nav/.tab/.leaf/.lk.small links) renders inline-flex/block and stays checked.
    const interactives = Array.from(document.querySelectorAll('a[href], button:not([disabled]), input:not([type="hidden"]), select, textarea, [role="button"], [tabindex]:not([tabindex="-1"])'))
      .filter(isVisible)
      .filter((element) => !(element.tagName === "A" && window.getComputedStyle(element).display === "inline"));
    const smallTargets = interactives
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        const min = root.clientWidth < 600 ? 40 : 28;
        if (element instanceof HTMLInputElement && (element.type === "checkbox" || element.type === "radio")) {
          const label = element.closest("label");
          if (label) {
            const labelRect = label.getBoundingClientRect();
            return labelRect.width < min || labelRect.height < min;
          }
        }
        return rect.width < min || rect.height < min;
      })
      .slice(0, 5)
      .map(describeElement);
    const overlaps = [];
    for (let i = 0; i < interactives.length; i += 1) {
      for (let j = i + 1; j < interactives.length; j += 1) {
        const first = interactives[i];
        const second = interactives[j];
        if (first.contains(second) || second.contains(first)) continue;
        const a = first.getBoundingClientRect();
        const b = second.getBoundingClientRect();
        const xOverlap = Math.max(0, Math.min(a.right, b.right) - Math.max(a.left, b.left));
        const yOverlap = Math.max(0, Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));
        if (xOverlap > 4 && yOverlap > 4) {
          overlaps.push({ first: describeElement(first), second: describeElement(second), xOverlap: Math.round(xOverlap), yOverlap: Math.round(yOverlap) });
        }
        if (overlaps.length >= 5) break;
      }
      if (overlaps.length >= 5) break;
    }

    return {
      overflow,
      mainInViewport: !main || (main.left >= -1 && main.right <= root.clientWidth + 1 && main.top >= -1),
      panels,
      clippedControls,
      clippedNavLabels,
      navLabelSpread,
      smallTargets,
      overlaps,
    };

    function isVisible(element) {
      if (element.closest("details:not([open])")) return false;
      const rect = element.getBoundingClientRect();
      const style = window.getComputedStyle(element);
      return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
    }

    function describeElement(element) {
      return {
        tag: element.tagName.toLowerCase(),
        text: (element.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 80),
        width: Math.round(element.getBoundingClientRect().width),
        height: Math.round(element.getBoundingClientRect().height),
        clientWidth: element.clientWidth,
        scrollWidth: element.scrollWidth,
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
      };
    }
  });

  if (layout.overflow > 2) {
    throw new Error(`${routeName} ${viewportLabel} has horizontal overflow of ${layout.overflow}px`);
  }
  if (!layout.mainInViewport) {
    throw new Error(`${routeName} ${viewportLabel} main content extends outside the viewport`);
  }
  if (layout.panels.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has cards/panels cut at the viewport edge: ${JSON.stringify(layout.panels)}`);
  }
  if (layout.clippedControls.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has clipped button/link text: ${JSON.stringify(layout.clippedControls)}`);
  }
  if (viewportLabel === "desktop" && layout.clippedNavLabels.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has clipped navigation labels: ${JSON.stringify(layout.clippedNavLabels)}`);
  }
  if (viewportLabel === "desktop" && layout.navLabelSpread > 1) {
    throw new Error(`${routeName} ${viewportLabel} has misaligned navigation labels; x spread ${layout.navLabelSpread}px`);
  }
  if (viewportLabel === "narrow" && layout.smallTargets.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has interactive targets below 40px: ${JSON.stringify(layout.smallTargets)}`);
  }
  if (layout.overlaps.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has overlapping interactive elements: ${JSON.stringify(layout.overlaps)}`);
  }
}

async function settleAtTop(page) {
  await page.evaluate(() => {
    document.documentElement.style.scrollBehavior = "auto";
    if (document.body) document.body.style.scrollBehavior = "auto";
    window.scrollTo(0, 0);
  });
  await page.waitForTimeout(80);
}

async function assertA11y(page, routeName, viewportLabel) {
  const results = await new AxeBuilder({ page }).analyze();
  const violations = results.violations.filter((violation) => violation.impact === "critical" || violation.impact === "serious");
  if (violations.length === 0) return;
  const summary = violations.slice(0, 5).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    description: violation.description,
    nodes: violation.nodes.slice(0, 3).map((node) => node.target),
  }));
  throw new Error(`${routeName} ${viewportLabel} has serious/critical accessibility violations: ${JSON.stringify(summary)}`);
}

async function assertCoreInteractions(page, routeName, viewportLabel) {
  // The visual smoke gate is not a full mock-fidelity claim, but it must still prove that the core mock
  // controls are not dead. These checks deliberately avoid business writes: they only open/close overlays.
  if (viewportLabel !== "desktop") return;

  if (routeName === "counts-herd") {
    await openAndCloseDialog(page, page.getByRole("button", { name: "Filters", exact: true }), "Filter — Counts / Herd", "Close filters", routeName);
    await openAndCloseDialog(page, page.getByRole("button", { name: "Register goat", exact: true }), "Register goat", "Close", routeName);
    await openAndCloseDialog(page, page.getByRole("button", { name: "Import sheet", exact: true }), "Import sheet", "Close", routeName);
    await openAndCloseDrawer(
      page,
      page.locator('section:has-text("Herd") tbody tr .celllink').first(),
      "Goat Passport",
      routeName,
    );
  }

  if (routeName === "action-center") {
    await openAndCloseDrawer(page, page.locator(".taskboard .task").first(), "ACTION", routeName);
  }

  if (routeName === "calendar") {
    await openAndCloseDrawer(page, page.locator(".agenda .ev.celllink").first(), "CALENDAR EVENT", routeName);
    // Month view + a month-cell (.mev) event open — exercised in-app so the top-bar scope is carried.
    const monthTab = page.getByRole("link", { name: "Month", exact: true });
    if ((await monthTab.count()) === 1) {
      await monthTab.click();
      await page.locator(".mcal").first().waitFor({ state: "visible", timeout: 5_000 });
      const mev = page.locator(".mcal .mev").first();
      if ((await mev.count()) === 1) {
        await openAndCloseDrawer(page, mev, "CALENDAR EVENT", routeName);
      }
    }
  }

  if (routeName === "procurement-source-entry") {
    await openAndCloseDrawer(
      page,
      page.locator('section:has-text("Supplier warmup") tbody tr .celllink').first(),
      "SOURCE LOAD",
      routeName,
    );
  }

  if (routeName === "workflows") {
    const row = page.locator(".wfcat .wfrow").first();
    const rowCount = await row.count();
    if (rowCount === 1) {
      await row.scrollIntoViewIfNeeded();
      await row.click();
      await page.waitForURL(/workflow=/, { timeout: 5_000 });
      await page.getByRole("link", { name: /Back to list/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("link", { name: /Open workflow detail/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("link", { name: /Back to list/i }).click();
      await page.waitForURL((url) => !url.searchParams.has("workflow"), { timeout: 5_000 });
    }
  }

  if (routeName === "vaccination") {
    await openAndCloseDialog(page, page.getByRole("button", { name: "SOP", exact: true }), "Vaccination Drive SOP", "Close", routeName);
    await openAndCloseDialog(page, page.getByRole("button", { name: "Import sheet", exact: true }), "Import vaccination sheet", "Close", routeName);
    await openAndCloseDialog(page, page.getByRole("button", { name: "New drive", exact: true }), "New vaccination drive", "Close", routeName);

    const filters = page.getByRole("button", { name: "Filters", exact: true });
    const filterCount = await filters.count();
    if (filterCount !== 4) {
      throw new Error(`${routeName} expected 4 Filters buttons, found ${filterCount}`);
    }
    for (const [i, label] of [
      "Filter — Supplier warmup",
      "Filter — Vaccination status matrix",
      "Filter — Per-cohort vaccination detail",
      "Filter — Vaccination shed events",
    ].entries()) {
      await openAndCloseDialog(page, filters.nth(i), label, "Close filters", routeName);
    }

    await openAndCloseDrawer(
      page,
      page.locator('section:has-text("Supplier warmup") tbody tr .celllink').first(),
      "WARMUP",
      routeName,
    );
    await openAndCloseDrawer(
      page,
      page.locator('section:has-text("Vaccination status matrix") tbody tr .celllink').first(),
      "Record / verify vaccination",
      routeName,
    );
    await openAndCloseDrawer(
      page,
      page.locator('section:has-text("Per-cohort vaccination detail") tbody tr .celllink').first(),
      "RECORD",
      routeName,
    );
    await openAndCloseDrawer(page, page.locator(".pexec .pexr").first(), "RECORD", routeName);
  }
}

async function openAndCloseDialog(page, trigger, dialogLabel, closeName, routeName) {
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} trigger for "${dialogLabel}" resolved to ${triggerCount} elements`);
  }
  await trigger.click();
  const dialog = page.locator(`[role="dialog"][aria-label="${cssString(dialogLabel)}"]`);
  await dialog.waitFor({ state: "visible", timeout: 5_000 });
  const close = dialog.locator(`button[aria-label="${cssString(closeName)}"]`);
  const closeCount = await close.count();
  if (closeCount !== 1) {
    throw new Error(`${routeName} close button for "${dialogLabel}" resolved to ${closeCount} elements`);
  }
  const topmost = await close.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const top = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return top === element || element.contains(top);
  });
  if (!topmost) {
    throw new Error(`${routeName} close button for "${dialogLabel}" is covered by another layer`);
  }
  await close.click();
  await dialog.waitFor({ state: "hidden", timeout: 5_000 });
}

async function openAndCloseDrawer(page, trigger, expectedText, routeName) {
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} drawer trigger for "${expectedText}" resolved to ${triggerCount} elements`);
  }
  await trigger.scrollIntoViewIfNeeded();
  await trigger.click();
  const drawer = page.locator(".drawer.on").first();
  await drawer.waitFor({ state: "visible", timeout: 5_000 });
  await drawer.getByText(expectedText, { exact: false }).first().waitFor({ state: "visible", timeout: 5_000 });
  const close = drawer.locator('a[aria-label^="Close"]').first();
  const closeCount = await close.count();
  if (closeCount !== 1) {
    throw new Error(`${routeName} close link for drawer "${expectedText}" resolved to ${closeCount} elements`);
  }
  const topmost = await close.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const top = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return top === element || element.contains(top);
  });
  if (!topmost) {
    throw new Error(`${routeName} close link for drawer "${expectedText}" is covered by another layer`);
  }
  await close.click();
  await drawer.waitFor({ state: "hidden", timeout: 5_000 });
}

function cssString(value) {
  return value.replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function compareOrUpdateBaseline(screenshotName, screenshotPath) {
  const baselinePath = join(baselineDir, screenshotName);
  if (updateBaseline) {
    mkdirSync(baselineDir, { recursive: true });
    copyFileSync(screenshotPath, baselinePath);
    baselineUpdated += 1;
    return;
  }
  if (!existsSync(baselinePath)) {
    if (requireBaseline) {
      throw new Error(`Missing visual baseline for ${screenshotName} at ${relativeToRepo(baselinePath)}`);
    }
    return;
  }
  const actual = PNG.sync.read(readFileSync(screenshotPath));
  const expected = PNG.sync.read(readFileSync(baselinePath));
  if (actual.width !== expected.width || actual.height !== expected.height) {
    throw new Error(`${screenshotName} dimensions changed: actual ${actual.width}x${actual.height}, baseline ${expected.width}x${expected.height}`);
  }
  const diff = new PNG({ width: actual.width, height: actual.height });
  const diffPixels = pixelmatch(expected.data, actual.data, diff.data, actual.width, actual.height, { threshold: 0.1 });
  const ratio = diffPixels / (actual.width * actual.height);
  baselineCompared += 1;
  if (ratio > maxDiffRatio) {
    const diffPath = join(diffDir, screenshotName);
    writeFileSync(diffPath, PNG.sync.write(diff));
    throw new Error(`${screenshotName} visual diff ${ratio.toFixed(4)} exceeds max ${maxDiffRatio}; diff=${relativeToRepo(diffPath)}`);
  }
}

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function relativeToRepo(path) {
  return path.startsWith(repoRoot) ? path.slice(repoRoot.length + 1) : path;
}

function normalizeRepoPath(path) {
  if (!path) return undefined;
  return isAbsolute(path) ? path : join(repoRoot, path);
}

function parseArgs(argv) {
  const parsed = {
    baselineDir: undefined,
    updateBaseline: false,
    requireBaseline: false,
    maxDiffRatio: undefined,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--baseline-dir") {
      parsed.baselineDir = argv[++index];
    } else if (arg === "--update-baseline") {
      parsed.updateBaseline = true;
    } else if (arg === "--require-baseline") {
      parsed.requireBaseline = true;
    } else if (arg === "--max-diff-ratio") {
      parsed.maxDiffRatio = Number(argv[++index]);
      if (!Number.isFinite(parsed.maxDiffRatio) || parsed.maxDiffRatio < 0 || parsed.maxDiffRatio > 1) {
        throw new Error("--max-diff-ratio must be a number between 0 and 1");
      }
    } else {
      throw new Error(`Unknown argument: ${arg}`);
    }
  }
  return parsed;
}
