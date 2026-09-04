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
const navigationTimeoutMs = Number(process.env.GOATOS_SMOKE_NAVIGATION_TIMEOUT_MS ?? 60_000);
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

// Optional focused run: GOATOS_SMOKE_ONLY_ROUTES=calendar,counts-herd restricts the sweep to those
// routes so a targeted assertion (e.g. calendar identity) can run without an unrelated earlier route
// (e.g. a seed-empty Action Center) aborting the whole gate before Calendar is reached.
// Validate the selection UP FRONT — before waiting on the app or resolving any per-route fixture — so a
// typo (or a selection that matches nothing) fails immediately, not after an unrelated network lookup.
// Derived, never hand-maintained: a list that must be kept in step with another list
// eventually is not. The placeholder ids only shape two paths, never the names.
const KNOWN_ROUTE_NAMES = buildRoutes("placeholder", "placeholder").map((route) => route.name);
const onlyRoutesRaw = process.env.GOATOS_SMOKE_ONLY_ROUTES;
const onlyRoutes = (onlyRoutesRaw ?? "").split(",").map((s) => s.trim()).filter(Boolean);
// Present-but-empty (e.g. "," or whitespace) is an error: the caller asked to filter but named nothing.
// Only an entirely-unset var falls back to the full sweep.
if (onlyRoutesRaw !== undefined && onlyRoutes.length === 0) {
  throw new Error(
    `GOATOS_SMOKE_ONLY_ROUTES is set (${JSON.stringify(onlyRoutesRaw)}) but resolves to no route names. Unset it to run the full sweep, or name valid routes: ${KNOWN_ROUTE_NAMES.join(", ")}`,
  );
}
const unknownRoutes = onlyRoutes.filter((name) => !KNOWN_ROUTE_NAMES.includes(name));
if (unknownRoutes.length) {
  throw new Error(
    `GOATOS_SMOKE_ONLY_ROUTES has unknown route(s): ${unknownRoutes.join(", ")}. Valid routes: ${KNOWN_ROUTE_NAMES.join(", ")}`,
  );
}
const runsRoute = (name) => onlyRoutes.length === 0 || onlyRoutes.includes(name);

await waitForApp(appBaseUrl);
// Resolve per-route smoke fixtures lazily: only hit /goats/search or the procurement load lookup when a
// selected route actually needs it, so a focused `calendar` run never fails on an unrelated lookup.
const goatId = runsRoute("goat-passport") ? await resolveSmokeGoatID(apiBaseUrl, bearerToken, tenantId) : null;
const procurementLoadId = runsRoute("procurement-load-detail")
  ? await resolveSmokeProcurementLoadID(apiBaseUrl, bearerToken, tenantId)
  : null;
mkdirSync(screenshotDir, { recursive: true });
if (baselineDir) mkdirSync(diffDir, { recursive: true });

// The routes this sweep visits, as a function of the ids two of them need.
//
// It is a function so the NAME LIST can be derived from it before those ids are resolved --
// the allow-list used to be a second hand-maintained copy and it drifted: counts-sops and
// counts-sops-builder were in this table, so a full sweep visited them, while a focused run
// naming either was rejected as an unknown route.
function buildRoutes(goatId, procurementLoadId) {
  const routes = [
    { name: "control-tower", path: "/?scope_mode=company" },
    { name: "action-center", path: "/action-center?scope_mode=company" },
    { name: "calendar", path: "/calendar?scope_mode=company&day=week" },
    { name: "protocol-adherence", path: "/protocol-adherence?scope_mode=company" },
    { name: "workflows", path: "/workflows?scope_mode=company" },
    { name: "approvals", path: "/approvals?scope_mode=company" },
    { name: "verify", path: "/verify?scope_mode=company" },
    { name: "actions", path: "/actions?scope_mode=company" },
    { name: "vaccination", path: "/vaccination?scope_mode=company" },
  {
    name: "vaccination-schedule",
    path: `/vaccination?scope_mode=company&view=schedule&schedule_year=${new Date().getFullYear()}`,
    viewports: ["desktop"],
  },
    { name: "vaccination-execution", path: "/vaccination?scope_mode=company#execution" },
    { name: "vaccination-live-tracker", path: "/vaccination/live-tracker?scope_mode=company" },
    { name: "vaccination-plan", path: "/vaccination/plan?scope_mode=company" },
    { name: "procurement-source-entry", path: "/procurement/source-entry?scope_mode=company" },
    { name: "procurement-vendors", path: "/procurement/vendors?scope_mode=company" },
    { name: "procurement-feed-purchases", path: "/procurement/feed-purchases?scope_mode=company" },
    { name: "sales", path: "/sales?scope_mode=company" },
    { name: "sales-loads", path: "/sales/loads?scope_mode=company" },
    { name: "sales-config", path: "/sales/config?scope_mode=company" },
    { name: "feed-config", path: "/feed/config?scope_mode=company" },
    { name: "feed-analytics", path: "/feed/analytics?scope_mode=company" },
    { name: "feed-sops", path: "/feed/sops?scope_mode=company" },
    { name: "feed-direction", path: "/feed/direction?scope_mode=company" },
    { name: "feed-packing", path: "/feed/packing?scope_mode=company" },
    { name: "weighing-analytics", path: "/weighing/analytics?scope_mode=company" },
    { name: "weighing-sops", path: "/weighing/sops?scope_mode=company" },
    { name: "weighing-weights", path: "/weighing/weights?scope_mode=company" },
    { name: "counts-sops", path: "/counts/sops?scope_mode=company" },
    { name: "counts-sops-builder", path: "/counts/sops?compose=1&scope_mode=company" },
    { name: "counts-herd", path: "/counts/herd?scope_mode=company" },
    { name: "counts-analytics", path: "/counts/analytics?scope_mode=company" },
    { name: "counts-breakdown", path: "/counts/breakdown?scope_mode=company" },
    { name: "counts-milk-preparation", path: "/counts/milk-preparation?scope_mode=company" },
    { name: "milk-sops", path: "/milk/sops?scope_mode=company" },
    { name: "herd-signals", path: "/herd-signals?scope_mode=company" },
    { name: "health-config", path: "/health/config?scope_mode=company" },
    { name: "operations-audit", path: "/operations/audit?scope_mode=company" },
    { name: "operations-dlq", path: "/operations/dlq?scope_mode=company" },
    { name: "people", path: "/people?scope_mode=company" },
    { name: "goat-passport", path: `/goats/${encodeURIComponent(goatId)}` },
    {
      name: "procurement-load-detail",
      path: `/procurement/source-entry/loads/${encodeURIComponent(procurementLoadId)}?scope_mode=company`,
    },
  ];
  // The load-detail route needs a real load to visit; its NAME is still valid to ask for.
  return procurementLoadId ? routes : routes.filter((route) => route.name !== "procurement-load-detail");
}

const routes = buildRoutes(goatId, procurementLoadId);

// Names were already validated up front against KNOWN_ROUTE_NAMES; resolve the selection to concrete
// routes. A requested route the run couldn't build (e.g. procurement-load-detail with no seeded load)
// fails loudly here rather than silently running fewer routes than asked for.
const selectedRoutes = onlyRoutes.length ? routes.filter((route) => onlyRoutes.includes(route.name)) : routes;
if (onlyRoutes.length) {
  const built = new Set(routes.map((route) => route.name));
  const unavailable = onlyRoutes.filter((name) => !built.has(name));
  if (unavailable.length) {
    throw new Error(`GOATOS_SMOKE_ONLY_ROUTES selected route(s) not available in this run: ${unavailable.join(", ")}`);
  }
}
if (selectedRoutes.length === 0) {
  throw new Error("GOATOS_SMOKE_ONLY_ROUTES selected zero routes");
}

const pagerMinimums = new Map([
  ["protocol-adherence", 1],
  ["workflows", 1],
  ["verify", 1],
  ["vaccination", 1],
  ["vaccination-execution", 1],
  ["procurement-source-entry", 1],
  ["procurement-vendors", 1],
  ["procurement-feed-purchases", 1],
  ["sales", 1],
  ["sales-loads", 1],
  ["vaccination-plan", 1],
  ["weighing-analytics", 1],
  ["weighing-weights", 1],
  ["counts-sops", 1],
  ["counts-herd", 1],
  ["operations-audit", 2],
  ["people", 1],
]);

const browser = await chromium.launch({ channel: process.env.GOATOS_SMOKE_BROWSER_CHANNEL || "chrome" });
try {
  for (const viewport of [
    { label: "desktop", width: 1440, height: 1000 },
    { label: "narrow", width: 390, height: 900 },
  ]) {
    const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } });
    const cookieUrl = new URL(appBaseUrl);
    await context.addCookies([
      {
        name: "goatos_firebase_id_token",
        value: bearerToken,
        domain: cookieUrl.hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      },
    ]);
    const page = await context.newPage();
    for (const route of selectedRoutes) {
      if (route.viewports && !route.viewports.includes(viewport.label)) continue;
      console.log(`visual_route_start=${viewport.label}:${route.name}`);
      const url = `${appBaseUrl}${appPath(route.path)}`;
      const response = await gotoWithRetry(page, url);
      await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
      if (!response) {
        await page.waitForURL(url, { timeout: 5_000 }).catch(() => undefined);
        if (page.url() !== url) {
          throw new Error(`${route.name} returned HTTP no-response for ${appPath(route.path)}`);
        }
      } else if (!response.ok()) {
        throw new Error(`${route.name} returned HTTP ${response.status()} for ${appPath(route.path)}`);
      }
      const html = await page.content();
      assertHealthyHTML(route.name, html, bearerToken);
      await assertLayoutHealthy(page, route.name, viewport.label);
      await assertA11y(page, route.name, viewport.label);
      await assertTruncationContracts(page, route.name, viewport.label);
      await assertPaginationControls(page, route.name, viewport.label);
      await assertCoreInteractions(page, route.name, viewport.label);
      await settleAtTop(page);
      const screenshotName = `${viewport.label}-${route.name}.png`;
      const screenshotPath = join(screenshotDir, screenshotName);
      await page.screenshot({ path: screenshotPath, fullPage: true });
      if (baselineDir) {
        compareOrUpdateBaseline(screenshotName, screenshotPath);
      }
      console.log(`visual_route_done=${viewport.label}:${route.name}`);
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
      routes: selectedRoutes.map((route) => appPath(route.path)),
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
console.log(`routes_captured=${selectedRoutes.map((route) => appPath(route.path)).join(",")}`);
if (baselineDir) {
  if (requireBaseline && !updateBaseline && baselineCompared === 0) {
    throw new Error(`Visual baseline was required but no screenshots were compared in ${relativeToRepo(baselineDir)}`);
  }
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
    "Admin-web contract unavailable",
    "route_not_registered",
    "Forgot password?",
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
      .filter((element) => !element.closest(".ceo-ai"))
      .filter((element) => !element.closest(".mzai-bubble"))
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
    const truncationTitleProblems = Array.from(document.querySelectorAll("[data-truncate]"))
      .filter(isVisible)
      .filter((element) => !hoverTextFor(element))
      .slice(0, 5)
      .map(describeElement);
    const truncationStyleProblems = Array.from(document.querySelectorAll("[data-truncate]"))
      .filter(isVisible)
      .filter((element) => {
        const style = window.getComputedStyle(element);
        const lineClamp = style.getPropertyValue("-webkit-line-clamp");
        return style.overflow !== "hidden" || (style.textOverflow !== "ellipsis" && lineClamp === "none");
      })
      .slice(0, 5)
      .map(describeElement);

    return {
      overflow,
      mainInViewport: !main || (main.left >= -1 && main.right <= root.clientWidth + 1 && main.top >= -1),
      panels,
      clippedControls,
      clippedNavLabels,
      navLabelSpread,
      smallTargets,
      overlaps,
      truncationTitleProblems,
      truncationStyleProblems,
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
        className: element.getAttribute("class") || "",
        ariaLabel: element.getAttribute("aria-label") || "",
        text: (element.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 80),
        width: Math.round(element.getBoundingClientRect().width),
        height: Math.round(element.getBoundingClientRect().height),
        clientWidth: element.clientWidth,
        scrollWidth: element.scrollWidth,
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
      };
    }

    function hoverTextFor(element) {
      const own = element.getAttribute("title") || element.getAttribute("aria-label");
      if (own) return own;
      const labelled = element.closest("[title], [aria-label]");
      return labelled?.getAttribute("title") || labelled?.getAttribute("aria-label") || "";
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
  if (layout.truncationTitleProblems.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has truncated text without hover/full text: ${JSON.stringify(layout.truncationTitleProblems)}`);
  }
  if (layout.truncationStyleProblems.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has malformed truncation styling: ${JSON.stringify(layout.truncationStyleProblems)}`);
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

async function assertA11y(page, routeName, viewportLabel, includeSelector) {
  const builder = new AxeBuilder({ page });
  if (includeSelector) builder.include(includeSelector);
  const results = await builder.analyze();
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

async function assertTruncationContracts(page, routeName, viewportLabel) {
  void page;
  void routeName;
  void viewportLabel;
}

async function assertPaginationControls(page, routeName, viewportLabel) {
  const minimum = pagerMinimums.get(routeName);
  if (!minimum) return;

  const pagers = page.locator(".pager2");
  const count = await pagers.count();
  if (count === 0) {
    const bodyText = (await page.locator("body").innerText().catch(() => "")).replace(/\s+/g, " ");
    if (/0 rows|0 results|Nothing|No rows|No data/i.test(bodyText)) return;
    throw new Error(`${routeName} ${viewportLabel} expected at least ${minimum} pager2 footer(s), found none`);
  }
  if (count < minimum) {
    throw new Error(`${routeName} ${viewportLabel} expected at least ${minimum} pager2 footer(s) when pagination is rendered, found ${count}`);
  }
  for (let index = 0; index < count; index += 1) {
    const pager = pagers.nth(index);
    const text = (await pager.innerText()).replace(/\s+/g, " ").trim();
    if (!/(?:Prev(?:ious)?|Back)/i.test(text) || !/Next/i.test(text)) {
      throw new Error(`${routeName} ${viewportLabel} pager ${index + 1} is missing Prev/Next controls: ${text}`);
    }
  }

  if (viewportLabel !== "desktop" || process.env.GOATOS_VISUAL_EXERCISE_PAGERS !== "1") return;
  await exerciseFirstPagerRoundTrip(page, routeName);
}

async function exerciseFirstPagerRoundTrip(page, routeName) {
  const pager = page.locator(".pager2").first();
  const initialPagerText = normalizePagerText(await pager.innerText());
  const next = pager.locator("a, button").filter({ hasText: /Next/i }).first();
  if ((await next.count()) !== 1) return;
  if (await isDisabledControl(next)) return;

  await next.scrollIntoViewIfNeeded();
  await next.click();
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  await waitForPagerTextChange(page, initialPagerText);
  await waitForPagerControl(page, /Prev(?:ious)?/i, "enabled");

  const previous = page.locator(".pager2").first().locator("a, button").filter({ hasText: /Prev(?:ious)?/i }).first();
  if ((await previous.count()) !== 1 || (await isDisabledControl(previous))) {
    throw new Error(`${routeName} pager Next did not produce an enabled Previous control`);
  }
  await previous.scrollIntoViewIfNeeded();
  await previous.click();
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  await waitForPagerAtFirstPage(page, routeName);
}

async function isDisabledControl(locator) {
  const ariaDisabled = await locator.getAttribute("aria-disabled");
  const disabled = await locator.getAttribute("disabled");
  return ariaDisabled === "true" || disabled !== null;
}

async function waitForPagerControl(page, pattern, state) {
  await page.waitForFunction(
    ({ source, flags, state }) => {
      const re = new RegExp(source, flags);
      const pager = document.querySelector(".pager2");
      if (!pager) return false;
      return Array.from(pager.querySelectorAll("a, button")).some((element) => {
        const text = element.textContent ?? "";
        if (!re.test(text)) return false;
        const disabled =
          element.getAttribute("aria-disabled") === "true" ||
          element.hasAttribute("disabled") ||
          (element instanceof HTMLButtonElement && element.disabled);
        return state === "enabled" ? !disabled : disabled;
      });
    },
    { source: pattern.source, flags: pattern.flags, state },
    { timeout: 5_000 },
  );
}

async function waitForPagerTextChange(page, previousText) {
  await page.waitForFunction(
    (previousText) => {
      const text = (document.querySelector(".pager2")?.textContent ?? "").replace(/\s+/g, " ").trim();
      return text && text !== previousText;
    },
    previousText,
    { timeout: 5_000 },
  );
}

async function waitForPagerAtFirstPage(page, routeName) {
  await page.waitForFunction(
    () => {
      const text = (document.querySelector(".pager2")?.textContent ?? "").replace(/\s+/g, " ").trim();
      return /^1-\d+ of /.test(text) || /\bPage 1\b/.test(text) || /^0 /.test(text) || /^0 results\b/.test(text);
    },
    { timeout: 5_000 },
  ).catch((error) => {
    throw new Error(`${routeName} pager did not return to the first page: ${error instanceof Error ? error.message : String(error)}`);
  });
}

function normalizePagerText(text) {
  return text.replace(/\s+/g, " ").trim();
}

async function assertCoreInteractions(page, routeName, viewportLabel) {
  // The visual smoke gate is not a full mock-fidelity claim, but it must still prove that the core mock
  // controls are not dead. Most checks only open/close overlays; Action Center also submits one seeded
  // row-versioned SOP verification so the acceptance path is proven through the browser.
  if (viewportLabel !== "desktop") {
    if (routeName === "control-tower") {
      await assertMobileSidebarNavigation(page, routeName);
    }
    return;
  }

  if (routeName === "counts-herd") {
    const hasHerdTable = await assertHerdIdentityColumns(page, routeName);
    await openDialogIfPresent(page, page.getByRole("button", { name: "Filters", exact: true }), "Filter — Counts / Herd", "Close filters", routeName);
    await openDialogIfPresent(page, page.getByRole("button", { name: "Register animal", exact: true }), "Register animal", "Close", routeName);
    await openDialogIfPresent(page, page.getByRole("button", { name: "Import sheet", exact: true }), "Import sheet", "Close", routeName);
    if (hasHerdTable) {
      await openAndCloseDrawer(
        page,
        page.locator('section:has-text("Herd") tbody tr .celllink').first(),
        "Animal Passport",
        routeName,
        assertHerdPassportIdentity,
      );
    }
  }

  if (routeName === "action-center") {
    const task = page.locator(".taskboard .task").first();
    if ((await task.count()) === 1) {
      await openAndCloseDrawer(page, task, "ACTION", routeName);
    }
    await submitActionCenterVerification(page, routeName);
  }

  if (routeName === "calendar") {
    const drawerEvent = page.locator(".agenda .ev.celllink").first();
    const driveEvent = page.locator(".agenda .drivelink").first();
    if ((await drawerEvent.count()) === 1) {
      await openAndCloseDrawer(page, drawerEvent, "CALENDAR EVENT", routeName, assertCalendarTargetIdentity);
    } else if ((await driveEvent.count()) > 0) {
      await openCalendarDriveDetail(page, driveEvent, routeName);
    }
    // Month view + a month-cell (.mev) event open — exercised in-app so the top-bar scope is carried.
    const monthTab = page.getByRole("link", { name: "Month", exact: true });
    if ((await monthTab.count()) === 1) {
      await monthTab.click();
      await page.locator(".mcal").first().waitFor({ state: "visible", timeout: 5_000 });
      const mev = page.locator(".mcal .mev").first();
      if ((await mev.count()) === 1) {
        await openAndCloseDrawer(page, mev, "CALENDAR EVENT", routeName, assertCalendarTargetIdentity);
      }
    }
  }

  if (routeName === "procurement-source-entry") {
    const sourceLoad = page.locator('section:has-text("Supplier warmup") tbody tr .celllink').first();
    if ((await sourceLoad.count()) === 1) {
      await openAndCloseDrawer(page, sourceLoad, "SOURCE LOAD", routeName);
    }
  }

  if (routeName === "workflows") {
    const row = page.locator(".wfcat .wfrow").first();
    const rowCount = await row.count();
    if (rowCount === 1) {
      await row.scrollIntoViewIfNeeded();
      await row.click();
      await page.waitForURL(/workflow=/, { timeout: 5_000 });
      await page.getByRole("link", { name: /Back to list/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("link", { name: /Open workflow detail/i }).first().waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("link", { name: /Back to list/i }).click();
      await page.waitForURL((url) => !url.searchParams.has("workflow"), { timeout: 5_000 });
    }
  }

  if (routeName === "vaccination") {
    const sopQuickView = page.getByRole("button", { name: "SOP", exact: true });
    if ((await sopQuickView.count()) === 1) {
      await openAndCloseDialog(page, sopQuickView, "Vaccination Drive SOP", "Close", routeName);
    }
    if ((await page.getByRole("button", { name: "Import sheet", exact: true }).count()) > 0) {
      throw new Error(`${routeName} still exposes the removed Import sheet action`);
    }
    if ((await page.getByRole("button", { name: "New drive", exact: true }).count()) > 0) {
      throw new Error(`${routeName} still exposes the removed New drive action`);
    }

    if ((await page.getByText("Vaccination status matrix", { exact: true }).count()) > 0) {
      throw new Error(`${routeName} still renders the removed vaccination status matrix`);
    }
    if ((await page.getByText("Per-cohort vaccination detail", { exact: true }).count()) > 0) {
      throw new Error(`${routeName} still renders the removed per-cohort vaccination detail`);
    }

    const shedTable = page.locator("table.shed-summary-table").first();
    if ((await shedTable.count()) === 0) {
      return;
    }
    await shedTable.waitFor({ state: "visible", timeout: 10_000 });
    const shedSearch = page.locator('input[name="sheds_q"]');
    if ((await shedSearch.count()) !== 1) {
      throw new Error(`${routeName} expected one server-backed shed search input`);
    }
    const chipGroups = page.locator("section#sheds .chipset");
    if ((await chipGroups.count()) < 2) {
      throw new Error(`${routeName} expected status and capacity chip groups on the shed board`);
    }
    if ((await chipGroups.nth(0).locator("a.chip").count()) < 2 || (await chipGroups.nth(1).locator("a.chip").count()) < 2) {
      throw new Error(`${routeName} shed board status/capacity chips are missing`);
    }

    const firstShedLink = shedTable.locator("tbody tr .celllink").first();
    if ((await firstShedLink.count()) === 0) {
      return;
    }
    await firstShedLink.waitFor({ state: "visible", timeout: 10_000 });
    await firstShedLink.scrollIntoViewIfNeeded();
    await Promise.all([
      page.waitForURL((url) => url.pathname.startsWith("/vaccination/execution/sheds/"), { timeout: 10_000 }),
      firstShedLink.click(),
    ]);
    for (const label of ["Planned sessions", "Vaccine breakdown", "Animals in shed"]) {
      await page.getByText(label, { exact: true }).first().waitFor({ state: "visible", timeout: 10_000 });
    }
    const shedOverviewMetrics = await page.getByText("Shed overview", { exact: true }).first().evaluate((heading) => {
      const card = heading.closest("section.card");
      if (!(card instanceof HTMLElement)) throw new Error("Shed overview heading is not inside a card");
      const grid = card.querySelector(".shed-overview-grid");
      const stats = card.querySelector(".shed-overview-stats");
      const owners = card.querySelector(".shed-overview-owners");
      if (!(grid instanceof HTMLElement) || !(stats instanceof HTMLElement) || !(owners instanceof HTMLElement)) {
        throw new Error("Shed overview card is missing its compact grid layout");
      }
      return {
        cardWidth: Math.round(card.getBoundingClientRect().width),
        gridWidth: Math.round(grid.getBoundingClientRect().width),
        statsWidth: Math.round(stats.getBoundingClientRect().width),
        ownersWidth: Math.round(owners.getBoundingClientRect().width),
        gridHeight: Math.round(grid.getBoundingClientRect().height),
      };
    });
    if (shedOverviewMetrics.gridHeight < 86 || shedOverviewMetrics.ownersWidth < 240) {
      throw new Error(`${routeName} Shed overview renders as a loose/underbuilt summary: ${JSON.stringify(shedOverviewMetrics)}`);
    }
    if (shedOverviewMetrics.statsWidth > shedOverviewMetrics.ownersWidth * 2.25) {
      throw new Error(`${routeName} Shed overview stats consume the card and leave dead space: ${JSON.stringify(shedOverviewMetrics)}`);
    }
    const plannedSessionsMetrics = await page.getByText("Planned sessions", { exact: true }).first().evaluate((heading) => {
      const card = heading.closest("section.card");
      if (!(card instanceof HTMLElement)) throw new Error("Planned sessions heading is not inside a card");
      const body = card.querySelector(".planned-sessions-list");
      const row = card.querySelector(".planned-session-row");
      if (!(body instanceof HTMLElement) || !(row instanceof HTMLElement)) {
        throw new Error("Planned sessions card is missing its purpose-built session list");
      }
      const cardRect = card.getBoundingClientRect();
      const bodyRect = body.getBoundingClientRect();
      const rowRect = row.getBoundingClientRect();
      const bodyStyle = window.getComputedStyle(body);
      return {
        cardHeight: cardRect.height,
        bodyHeight: bodyRect.height,
        rowHeight: rowRect.height,
        overflowY: bodyStyle.overflowY,
      };
    });
    if (plannedSessionsMetrics.cardHeight < 150 || plannedSessionsMetrics.bodyHeight < 100 || plannedSessionsMetrics.rowHeight < 56) {
      throw new Error(`${routeName} Planned sessions renders as a cramped widget: ${JSON.stringify(plannedSessionsMetrics)}`);
    }
    if (plannedSessionsMetrics.overflowY !== "visible") {
      throw new Error(`${routeName} Planned sessions must not become a one-row scroll trap: ${JSON.stringify(plannedSessionsMetrics)}`);
    }
    await gotoWithRetry(page, `${appBaseUrl}${appPath("/vaccination?scope_mode=company")}`);
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  }

  if (routeName === "vaccination-schedule") {
    const overflowToggle = page.locator(".schedule-vaccine-toggle").first();
    if ((await overflowToggle.count()) === 0) {
      return;
    }
    await overflowToggle.waitFor({ state: "visible", timeout: 10_000 });
    const collapsedLabel = (await overflowToggle.innerText()).replace(/\s+/g, " ").trim();
    if (!/^\+\d+ more$/.test(collapsedLabel)) {
      throw new Error(`${routeName} collapsed vaccine overflow label is ${JSON.stringify(collapsedLabel)}; expected "+N more"`);
    }
    if ((await overflowToggle.getAttribute("aria-expanded")) !== "false") {
      throw new Error(`${routeName} vaccine overflow must start collapsed`);
    }

    const before = await overflowToggle.evaluate((element) => {
      const row = element.closest("tr");
      if (!(row instanceof HTMLElement)) throw new Error("vaccine overflow toggle is not inside a table row");
      return { rowHeight: row.getBoundingClientRect().height, url: window.location.href };
    });
    await overflowToggle.click();
    await page.waitForFunction(
      (element) => element instanceof HTMLElement && element.getAttribute("aria-expanded") === "true",
      await overflowToggle.elementHandle(),
      { timeout: 5_000 },
    );

    const expanded = await overflowToggle.evaluate((element) => {
      const row = element.closest("tr");
      const cell = element.closest("td");
      const control = element.closest(".schedule-vaccine-control");
      const expandedChips = control?.querySelector(".schedule-vaccine-expanded-chips");
      if (!(row instanceof HTMLElement) || !(cell instanceof HTMLElement) || !(control instanceof HTMLElement)) {
        throw new Error("vaccine overflow control is not inside the expected schedule cell");
      }
      return {
        cellBackground: getComputedStyle(cell).backgroundColor,
        controlBackground: getComputedStyle(control).backgroundColor,
        expandedChipCount: expandedChips?.querySelectorAll(".vaccine-chip").length ?? 0,
        expandedChipsHidden: expandedChips instanceof HTMLElement ? expandedChips.hidden : true,
        label: element.textContent?.replace(/\s+/g, " ").trim() ?? "",
        rowHeight: row.getBoundingClientRect().height,
        url: window.location.href,
      };
    });
    if (expanded.label !== "Show less") {
      throw new Error(`${routeName} expanded vaccine overflow label is ${JSON.stringify(expanded.label)}; expected "Show less"`);
    }
    if (expanded.expandedChipsHidden || expanded.expandedChipCount < 1) {
      throw new Error(`${routeName} did not reveal its hidden vaccine chips`);
    }
    if (!isTransparentBackground(expanded.cellBackground) || !isTransparentBackground(expanded.controlBackground)) {
      throw new Error(
        `${routeName} paints an expanded-state background (cell=${expanded.cellBackground}, control=${expanded.controlBackground})`,
      );
    }
    if (Math.abs(expanded.rowHeight - before.rowHeight) > 1) {
      throw new Error(`${routeName} vaccine overflow changes row height by ${Math.abs(expanded.rowHeight - before.rowHeight).toFixed(2)}px`);
    }
    if (expanded.url !== before.url) {
      throw new Error(`${routeName} vaccine overflow changed the page URL`);
    }
    await assertLayoutHealthy(page, `${routeName}-expanded`, viewportLabel);
    await assertA11y(page, `${routeName}-expanded`, viewportLabel, ".schedule-vaccine-control");
  }
}

async function assertMobileSidebarNavigation(page, routeName) {
  const originalUrl = page.url();
  const menu = page.locator("button.hamb").first();
  if ((await menu.count()) !== 1) {
    throw new Error(`${routeName} narrow expected one mobile navigation menu button`);
  }
  await menu.click();
  await page.locator("aside.side.open").waitFor({ state: "visible", timeout: 5_000 });
  await page.waitForFunction(() => {
    const sidebar = document.querySelector("aside.side.open");
    return sidebar instanceof HTMLElement && getComputedStyle(sidebar).transform === "none";
  }, { timeout: 5_000 });
  const salesGroup = page.locator("aside.side.open .ggrp", { hasText: "Sales" }).first();
  if ((await salesGroup.count()) !== 1) {
    throw new Error(`${routeName} narrow expected the Sales sidebar group to be reachable`);
  }
  await salesGroup.click();
  const loadsLeaf = page.locator('aside.side.open a.leaf[href^="/sales/loads"]').first();
  if ((await loadsLeaf.count()) !== 1) {
    throw new Error(`${routeName} narrow expected the Sales / Loads leaf to be reachable after group expansion`);
  }
  await Promise.all([
    page.waitForURL((url) => url.pathname === "/sales/loads", { timeout: 10_000 }),
    loadsLeaf.click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  if (await page.locator("aside.side.open").count()) {
    throw new Error(`${routeName} narrow sidebar stayed open after leaf navigation`);
  }
  await gotoWithRetry(page, originalUrl);
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
}

function isTransparentBackground(value) {
  return value === "transparent" || value === "rgba(0, 0, 0, 0)";
}

async function openCalendarDriveDetail(page, trigger, routeName) {
  await trigger.first().waitFor({ state: "visible", timeout: 10_000 }).catch(() => undefined);
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} drive detail trigger resolved to ${triggerCount} elements`);
  }
  const href = await trigger.getAttribute("href");
  if (!href) throw new Error(`${routeName} drive detail trigger has no href`);
  const expectedUrl = new URL(href, page.url());
  await trigger.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.pathname === expectedUrl.pathname && url.search === expectedUrl.search, { timeout: 10_000 }),
    trigger.click(),
  ]);
  if (!page.url().includes("/calendar/drive/")) {
    throw new Error(`${routeName} drive detail did not navigate to /calendar/drive`);
  }
  const rosterHeading = page.getByText("Animal roster", { exact: true });
  if ((await rosterHeading.count()) === 0) {
    await page.goBack({ waitUntil: "domcontentloaded", timeout: 30_000 });
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    return;
  }
  await rosterHeading.waitFor({ state: "visible", timeout: 10_000 });
  const headers = (await page.locator("table thead th").allInnerTexts()).map((h) => h.trim());
  const expected = ["Display ID", "Shed", "Tag 1", "Tag 2"];
  if (headers.slice(0, 4).map(comparableHeader).join("|") !== expected.map(comparableHeader).join("|")) {
    throw new Error(`${routeName} calendar drive detail roster must start with ${expected.join(", ")}; got ${headers.join(", ")}`);
  }
  for (const banned of ["Animal ID 1", "Animal ID 2", "missing ID"]) {
    if ((await page.locator("body").innerText()).includes(banned)) {
      throw new Error(`${routeName} calendar drive detail contains banned identity wording "${banned}"`);
    }
  }
  await page.goBack({ waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
}

async function submitActionCenterVerification(page, routeName) {
  const queueLink = page.locator('a[href*="bucket=verify"]').first();
  const queueLinkCount = await queueLink.count();
  if (queueLinkCount === 0) {
    return;
  }
  if (queueLinkCount !== 1) {
    throw new Error(`${routeName} SOP queue link resolved to ${queueLinkCount} elements`);
  }
  await queueLink.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.pathname === "/action-center" && url.searchParams.get("bucket") === "verify", { timeout: 10_000 }),
    queueLink.click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});

  const verifyButton = page.locator('form button:not([disabled])').filter({ hasText: /^Verify$/ }).first();
  const verifyCount = await verifyButton.count();
  if (verifyCount !== 1) {
    const emptyQueue = await page.getByText("Nothing awaiting verification.", { exact: false }).count();
    if (verifyCount === 0 && emptyQueue === 1) {
      return;
    }
    const bodyText = await page.locator("body").innerText().catch(() => "");
    throw new Error(`${routeName} expected one enabled SOP Verify button or a coherent empty verification queue, found ${verifyCount}; body=${bodyText.replace(/\s+/g, " ").slice(0, 800)}`);
  }
  await verifyButton.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.searchParams.get("action_status") === "success", { timeout: 15_000 }),
    verifyButton.click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});
  await page.locator(".note").filter({ hasText: /success|verified|accepted/i }).first().waitFor({ state: "visible", timeout: 10_000 });
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

async function openDialogIfPresent(page, trigger, dialogLabel, closeName, routeName) {
  if ((await trigger.count()) === 0) return;
  await openAndCloseDialog(page, trigger, dialogLabel, closeName, routeName);
}

async function openAndCloseDrawer(page, trigger, expectedText, routeName, inspectDrawer) {
  await trigger.first().waitFor({ state: "visible", timeout: 10_000 }).catch(() => undefined);
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} drawer trigger for "${expectedText}" resolved to ${triggerCount} elements`);
  }
  const href = await trigger.getAttribute("href").catch(() => null);
  const expectedUrl = href ? new URL(href, page.url()) : null;
  await trigger.scrollIntoViewIfNeeded();
  await Promise.all([
    expectedUrl
      ? page.waitForURL(
          (url) => url.pathname === expectedUrl.pathname && url.search === expectedUrl.search,
          { timeout: 10_000 },
        )
      : Promise.resolve(),
    trigger.click(),
  ]);
  const drawer = page.locator(".drawer.on").first();
  try {
    await drawer.waitFor({ state: "visible", timeout: 10_000 });
  } catch (error) {
    if (!expectedUrl || page.url() !== expectedUrl.toString()) {
      throw error;
    }
    await page.reload({ waitUntil: "domcontentloaded", timeout: 30_000 });
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    try {
      await drawer.waitFor({ state: "visible", timeout: 10_000 });
    } catch (reloadError) {
      const taskCount = await page.locator(".taskboard .task").count().catch(() => -1);
      const bodyText = await page.locator("body").innerText().catch(() => "");
      throw new Error(
        `${routeName} drawer "${expectedText}" did not render after click+reload. url=${page.url()} href=${expectedUrl.toString()} tasks=${taskCount} body=${bodyText
          .replace(/\s+/g, " ")
          .slice(0, 800)}`,
        { cause: reloadError },
      );
    }
  }
  await page.waitForFunction(() => {
    const openDrawer = document.querySelector(".drawer.on");
    return openDrawer instanceof HTMLElement && getComputedStyle(openDrawer).transform === "none";
  });
  await drawer.getByText(expectedText, { exact: false }).first().waitFor({ state: "visible", timeout: 5_000 });
  if (inspectDrawer) {
    await inspectDrawer(drawer, routeName, expectedText);
  }
  const close = drawer.locator('a[aria-label^="Close"], button[aria-label^="Close"]').first();
  const closeCount = await close.count();
  if (closeCount !== 1) {
    throw new Error(`${routeName} close control for drawer "${expectedText}" resolved to ${closeCount} elements`);
  }
  const topmost = await close.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const top = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return top === element || element.contains(top);
  });
  if (!topmost) {
    throw new Error(`${routeName} close control for drawer "${expectedText}" is covered by another layer`);
  }
  await close.click();
  await drawer.waitFor({ state: "hidden", timeout: 5_000 });
}

// Herd Register must lead with the Display ID / Tag 1 / Tag 2 identity columns and must never render the
// old "missing ID" chip — missing Tag values render as an em dash only.
async function assertHerdIdentityColumns(page, routeName) {
  const table = page.locator("table.herd-register-table").first();
  if ((await table.count()) === 0) {
    return false;
  }
  const headers = (await table.locator("thead th").allInnerTexts()).map((h) => h.trim());
  const expected = ["Display ID", "Tag 1", "Tag 2"];
  if (headers.slice(0, 3).map(comparableHeader).join("|") !== expected.map(comparableHeader).join("|")) {
    throw new Error(`${routeName} herd table must start with ${expected.join(", ")}; got ${headers.join(", ")}`);
  }
  const bodyText = await table.innerText();
  if (/missing ID/i.test(bodyText)) {
    throw new Error(`${routeName} herd table still renders a "missing ID" chip`);
  }
  return true;
}

// The Animal Passport drawer identity block must show Display ID + Tag 1 + Tag 2 and a G-###### display id,
// and must never render the raw goat UUID as the primary id or the legacy "Animal ID 1/2" / "missing ID".
async function assertHerdPassportIdentity(drawer, routeName, expectedText) {
  const text = await drawer.innerText();
  for (const label of ["Display ID", "Tag 1", "Tag 2"]) {
    if (!text.includes(label)) {
      throw new Error(`${routeName} passport drawer "${expectedText}" is missing identity label "${label}"`);
    }
  }
  for (const banned of ["missing ID", "Animal ID 1", "Animal ID 2"]) {
    if (text.includes(banned)) {
      throw new Error(`${routeName} passport drawer "${expectedText}" contains banned identity wording "${banned}"`);
    }
  }
  const displayId = (await drawer.locator(".gid").first().innerText().catch(() => "")).trim();
  if (!/^G-\d+/.test(displayId)) {
    throw new Error(`${routeName} passport drawer Display ID chip should be a G-###### id; got "${displayId}"`);
  }
}

// The vaccination calendar event drawer must use the same identity vocabulary as herd/passport: any
// eligible-animals target roster leads with Display ID / Tag 1 / Tag 2 and never renders legacy wording.
// A given drive can legitimately resolve to 0 targets in seeded data (projection vs obligation
// rule_id / IST-due-day mismatch), so the roster header check runs only when the roster is present;
// the banned-wording check always runs on the drawer text.
async function assertCalendarTargetIdentity(drawer, routeName, expectedText) {
  const text = await drawer.innerText();
  for (const banned of ["Animal ID 1", "Animal ID 2", "missing ID"]) {
    if (text.includes(banned)) {
      throw new Error(`${routeName} calendar drawer "${expectedText}" contains banned identity wording "${banned}"`);
    }
  }
  const roster = drawer.locator('table:has(th:has-text("Display ID"))');
  if ((await roster.count()) >= 1) {
    const headers = (await roster.first().locator("thead th").allInnerTexts()).map((h) => h.trim());
    const expected = ["Display ID", "Tag 1", "Tag 2"];
    if (headers.slice(0, 3).map(comparableHeader).join("|") !== expected.map(comparableHeader).join("|")) {
      throw new Error(`${routeName} calendar drive-target roster must start with ${expected.join(", ")}; got ${headers.join(", ")}`);
    }
  } else if (process.env.GOATOS_SMOKE_STRICT_CALENDAR_IDENTITY === "1") {
    // Deterministic gate: require a populated drive roster. Run against a seed/fixture whose drive
    // projection has matching generated obligations (in the current seed, drive projections have no
    // generated obligations for their protocol_version, so the roster is always empty — see handoff).
    // Fails loudly instead of silently skipping, so the identity columns are actually validated.
    throw new Error(`${routeName} calendar drawer "${expectedText}" has no eligible-animals roster but GOATOS_SMOKE_STRICT_CALENDAR_IDENTITY=1 requires one`);
  } else {
    console.log(`identity_calendar_roster=skipped_no_targets route=${routeName} event="${expectedText}"`);
  }
}

function cssString(value) {
  return value.replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

async function gotoWithRetry(page, url) {
  try {
    return await page.goto(url, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/ERR_ABORTED|Timeout/.test(message)) throw error;
    await page.waitForTimeout(500);
    return page.goto(url, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
  }
}

function comparableHeader(value) {
  return value.trim().replace(/\s+/g, " ").toLowerCase();
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
