#!/usr/bin/env node
// smoke-routes-visual.mjs — ROUTE-level visual regression + render-integrity lane.
//
// The Paparazzi-style twin of smoke-stories-visual.mjs for real pages. It takes the route list
// from smoke-visual-live.mjs (parsed, never re-typed — the same source check-mobile-webview.mjs
// uses), renders every route at three device profiles x two themes against a live admin-web,
// runs the shared render-integrity probe (scripts/lib/render-integrity.mjs) and diffs each
// capture against the committed manifest / local PNG baseline (scripts/lib/visual-baseline.mjs).
//
// Profiles: desktop 1440x900 · phone 390x844 (iPhone UA, touch, dpr 3) · webview 390x844 and
// webview-small 360x780 (Android Chrome WebView UA with the `; wv)` token, touch, dpr 3).
// Mobile profiles add the phone-only integrity checks (chart axis text >= 11px, tap targets
// >= 44px, sticky headers not detached) on top of the shared probe.
//
// Unlike smoke-visual-live.mjs this lane does NOT need the API env / local-stack receipt: it
// only needs the app URL and (optionally) a bearer token for the auth cookie. It is not a
// replacement for that lane's route-health assertions; it is the picture + integrity gate.
//
//   GOATOS_ADMIN_WEB_BASE_URL   default http://127.0.0.1:3300
//   GOATOS_BEARER_TOKEN         optional; set as the auth cookie when present
//   --only <substr>[,<substr>]  restrict to route names containing any of these
//   --webview-critical          only the phone-critical routes in scripts/webview-critical-routes.json
//   --profiles desktop,phone,webview,webview-small   --themes dark,light
//   --open-drawers              after the settled capture, open the first drawer/dialog trigger
//                               on the page and capture it as <route>__<profile>__<theme>__drawer
//   --with-webview-static       also run check-mobile-webview.mjs --static-only and merge its
//                               report (100vh/dvh, backdrop-filter …) into this lane's summary
//   --baseline-dir <dir>        PNG baselines (default .codex-goatos-render/admin-web-route-baselines)
//   --update-baseline           rewrite manifest + PNGs; prunes waivers that no longer reproduce, NEVER adds one
//   --waive "<reason>"          record this run's unwaived integrity findings as accepted debt (deliberate only)
//   --require-baseline          a missing baseline is a FAILURE, not a skip
//   --max-diff-ratio <0..1>     per-capture pixel tolerance (default 0.01)
//   --no-integrity              pixel diff only
//   --browser-channel <name>    e.g. chrome (default: bundled chromium)
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { chromium } from "@playwright/test";
import { LOAD_PHASES_CLEAR, LOAD_PHASES_INSTALL, LOAD_PHASES_READ, RENDER_INTEGRITY_PROBE, collectConsoleErrors, judgeLoadPhases, waitForFonts } from "./lib/render-integrity.mjs";
import { assertChartHoverStability, collectVisualPatternFindings } from "./lib/visual-pattern-guards.mjs";
import { VisualBaseline } from "./lib/visual-baseline.mjs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(scriptDir, "..");
const repoRoot = resolve(appDir, "../..");
const args = parseArgs(process.argv.slice(2));

const appBaseUrl = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300").replace(/\/$/, "");
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
const navigationTimeoutMs = Number(process.env.GOATOS_SMOKE_NAVIGATION_TIMEOUT_MS ?? 60_000);
// Current key plus the pre-rename key, so the lane can drive a build from either side of the
// brand storage-key change (a gate server may run an older SHA than this script).
const THEME_STORAGE_KEYS = ["mesha.shell.theme", "goatos-theme"];
// The fonts the APP renders with. theme-config names Barlow as `fontFamily.secondary`, but no app
// surface reads fontSecondaryFamily and the app never loads Barlow (only Storybook does, for the
// template stories), so requiring it flagged `font-not-loaded Barlow` on every route (PR #294 F9).
// The story lane (smoke-stories-visual.mjs) still requires both. Pinned by route-fonts.test.mjs.
const ROUTE_FONTS = ["Public Sans"];

const PROFILES = {
  desktop: { label: "desktop", viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 },
  phone: {
    label: "phone",
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    userAgent:
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
  },
  webview: {
    label: "webview",
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    // Android WebView UA: the `; wv` token is what distinguishes an embedded WebView from Chrome.
    userAgent:
      "Mozilla/5.0 (Linux; Android 14; Pixel 5 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/127.0.6533.103 Mobile Safari/537.36",
  },
  "webview-small": {
    label: "webview-small",
    viewport: { width: 360, height: 780 },
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: 3,
    userAgent:
      "Mozilla/5.0 (Linux; Android 13; SM-A135F Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/127.0.6533.103 Mobile Safari/537.36",
  },
};
const profiles = (args.profiles ?? ["desktop", "phone", "webview", "webview-small"]).map((name) => {
  if (!PROFILES[name]) throw new Error(`Unknown profile "${name}". Known: ${Object.keys(PROFILES).join(", ")}`);
  return PROFILES[name];
});
const themes = args.themes ?? ["dark", "light"];

// ── Route source: parsed from smoke-visual-live.mjs ──────────────────────────
function loadSmokeRoutes() {
  const source = readFileSync(join(scriptDir, "smoke-visual-live.mjs"), "utf8");
  const block = source.match(/function buildRoutes\([^)]*\) \{[\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";
  if (!block) throw new Error("could not locate buildRoutes(...) in smoke-visual-live.mjs — route source moved");
  const year = String(new Date().getFullYear());
  const entries = [];
  for (const [, name, quote, path] of block.matchAll(/name:\s*"([^"]+)"[\s\S]{0,500}?path:\s*([`"])([^`"]+)/g)) {
    let resolved = path;
    if (quote === "`") {
      resolved = resolved
        .replace(/\$\{new Date\(\)\.getFullYear\(\)\}/g, year)
        .replace(/\$\{smokeWideWindowFrom\}/g, isoDaysAgo(43))
        .replace(/\$\{smokeWideWindowTo\}/g, isoDaysAgo(0));
    }
    resolved = resolved.replace(/\$\{sopIds\.(\w+)\}/g, (_, key) => sopIds?.[key] ?? "${missing}");
    if (resolved.includes("${")) {
      entries.push({ name, path: resolved, skip: "dynamic-fixture-id" });
      continue;
    }
    entries.push({ name, path: resolved });
  }
  if (entries.length < 20) throw new Error(`route parse produced only ${entries.length} routes — regex drifted`);
  return entries;
}
function isoDaysAgo(days) {
  return new Date(Date.now() - days * 86_400_000).toISOString().slice(0, 10);
}

// SOP studio editors need a real SOP id. Resolved by CODE from the API when the API env is present
// (GOATOS_API_BASE_URL + GOATOS_BEARER_TOKEN + GOATOS_TENANT_ID); otherwise those routes are skipped
// as dynamic fixtures, exactly like the other id-bearing ones.
const sopIds = await resolveSopIds();
async function resolveSopIds() {
  const api = process.env.GOATOS_API_BASE_URL?.replace(/\/$/, "");
  const tenant = process.env.GOATOS_TENANT_ID;
  if (!api || !bearerToken || !tenant) return null;
  try {
    const response = await fetch(`${api}/admin/sops?limit=200`, { headers: { authorization: `Bearer ${bearerToken}`, "X-GoatOS-Tenant-ID": tenant } });
    if (!response.ok) return null;
    const body = await response.json();
    const items = Array.isArray(body?.items) ? body.items : Array.isArray(body?.sops) ? body.sops : [];
    const find = (test) => items.find((item) => typeof item?.code === "string" && test(item.code));
    const id = (item) => (item ? (item.id ?? item.sop_id ?? null) : null);
    return {
      general: id(find((code) => code.startsWith("general."))),
      weighing: id(find((code) => code === "weighing.session")),
      feed: id(find((code) => code === "feed.direction")),
    };
  } catch {
    return null;
  }
}

// The bare entry URL a person actually types: server redirect chain / -> /weighing/analytics?wt_from…
// (smoke-visual-live's list only has the lens-qualified control tower).
const allRoutes = [{ name: "home", path: "/" }, ...loadSmokeRoutes()];
const criticalList = JSON.parse(readFileSync(join(scriptDir, "webview-critical-routes.json"), "utf8")).routes;
const unknownCritical = criticalList.filter((name) => !allRoutes.some((route) => route.name === name));
if (unknownCritical.length) throw new Error(`webview-critical-routes.json names routes missing from smoke-visual-live.mjs: ${unknownCritical.join(", ")}`);
const criticalSet = new Set(criticalList);
const routes = allRoutes
  .filter((route) => !args.webviewCritical || criticalSet.has(route.name))
  .filter((route) => !args.only || args.only.some((needle) => route.name.includes(needle)));
if (routes.length === 0) throw new Error(`No routes matched --only ${args.only?.join(",")}. Known: ${allRoutes.map((r) => r.name).join(", ")}`);

const outDir = join(repoRoot, ".codex-goatos-render", "admin-web-route-screenshots", new Date().toISOString().replaceAll(/[:.]/g, "-"));
mkdirSync(outDir, { recursive: true });
const baseline = new VisualBaseline({
  lane: "admin-web-routes-visual",
  manifestDir: join(appDir, "visual-baselines", "routes"),
  pngDir: normalizeRepoPath(args.baselineDir ?? process.env.GOATOS_ROUTE_VISUAL_BASELINE_DIR ?? ".codex-goatos-render/admin-web-route-baselines"),
  diffDir: join(outDir, "diffs"),
  updateBaseline: args.updateBaseline,
  requireBaseline: args.requireBaseline,
  waive: args.waive ? { reason: args.waive } : null,
  maxDiffRatio: args.maxDiffRatio,
  relativeToRepo,
});

const failures = [];
const integrityFindings = [];
const skipped = [];
const perRoute = [];
const unstable = [];
const ERROR_STATE_RE = /Something went wrong|Application error|ERR_CONNECTION_REFUSED|This page could not be loaded|Internal Server Error|internal server error|contract unavailable/i;
// API health probe: GOATOS_API_BASE_URL/version (the same identity endpoint smoke-visual-live uses),
// falling back to the app's own /api/health if no API URL is set. Failure during a run marks
// captures unstable so a DB/tunnel stall can never be frozen into a baseline.
const apiHealthUrl = process.env.GOATOS_API_BASE_URL ? `${process.env.GOATOS_API_BASE_URL.replace(/\/$/, "")}/version` : `${appBaseUrl}/api/health`;
async function probeApiHealth() {
  try {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 5_000);
    const response = await fetch(apiHealthUrl, { signal: controller.signal, headers: bearerToken ? { authorization: `Bearer ${bearerToken}` } : {} });
    clearTimeout(timer);
    return response.status < 500;
  } catch {
    return false;
  }
}
async function waitForSettledDom(page, selector, budgetMs = 2_000) {
  await page
    .evaluate(
      ({ selector, budgetMs }) =>
        new Promise((resolve) => {
          const root = document.querySelector(selector) ?? document.body;
          let quietFrames = 0;
          let lastText = root.innerText;
          let dirty = false;
          const observer = new MutationObserver(() => { dirty = true; });
          observer.observe(root, { subtree: true, childList: true, characterData: true, attributes: true });
          const deadline = performance.now() + budgetMs;
          const tick = () => {
            const text = root.innerText;
            if (!dirty && text === lastText) quietFrames += 1;
            else quietFrames = 0;
            dirty = false;
            lastText = text;
            if (quietFrames >= 2 || performance.now() > deadline) {
              observer.disconnect();
              resolve();
              return;
            }
            requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
      { selector, budgetMs },
    )
    .catch(() => {});
}
const CHART_SELECTOR = ".cx-fig, .cx-reveal, .minimal__chart__root, [data-chart], .kit-chart";
async function revealCharts(page) {
  const count = await page.evaluate((selector) => {
    const els = [...document.querySelectorAll(selector)];
    for (const el of els) {
      try { el.scrollIntoView({ block: "center", inline: "nearest" }); } catch {}
    }
    window.scrollTo(0, 0);
    return els.length;
  }, CHART_SELECTOR);
  if (count === 0) return;
  await page
    .waitForFunction((selector) => {
      const els = [...document.querySelectorAll(selector)];
      return els.every((el) => {
        if (el.classList.contains("cx-reveal") && !el.classList.contains("cx-in")) return false;
        const svg = el.matches("svg") ? el : el.querySelector("svg");
        if (!svg) return !el.matches(".minimal__chart__root, .kit-chart");
        return Boolean(svg.querySelector("path, rect, circle, line, polyline"));
      });
    }, CHART_SELECTOR, { timeout: 8_000 })
    .catch(() => {});
  // A path EXISTS from the first frame of Apex's draw-in animation (series drawn flat on the zero
  // line, growing up), so "has a path" is not "has painted". Wait until every series path is the same
  // across two samples (PR #294 K12: a /feed/analytics expenditure line captured flat at 0 under an
  // 80,000 axis while the live page drew it at ~67,000).
  await page
    .waitForFunction(() => {
      const snap = [...document.querySelectorAll(".apexcharts-series path")].map((p) => p.getAttribute("d") ?? "").join("|");
      const w = window;
      const prev = w.__chartPathsSnap;
      w.__chartPathsSnap = snap;
      return prev !== undefined && prev === snap;
    }, undefined, { timeout: 6_000, polling: 250 })
    .catch(() => {});
}
async function resetScrollPosition(page) {
  await page
    .evaluate(() => {
      window.scrollTo(0, 0);
      document.scrollingElement?.scrollTo?.(0, 0);
      for (const el of document.querySelectorAll("*")) {
        // The sidebar scrolls its ACTIVE leaf into view (mesha-shell); that scroll position is app
        // state, not leftover page scroll. Zeroing it hid the current leaf below the fold on a
        // 900px laptop and read as "no leaf highlighted" (PR #294 round 2, P1/K2).
        // The template nav column (layouts/core classes `*__layout__nav__*`) is matched by class: its
        // markup is template-derived and carries no attribute of ours.
        if (el instanceof HTMLElement && el.closest('[class*="layout__nav__vertical"], [class*="layout__nav__mobile"]')) continue;
        if (el instanceof HTMLElement && (el.scrollTop > 0 || el.scrollLeft > 0)) {
          el.scrollTop = 0;
          el.scrollLeft = 0;
        }
      }
    })
    .catch(() => {});
}
const infoFindings = [];
let captured = 0;

// Phone-only integrity: the checks the mobile-webview taxonomy names that the shared probe
// does not (axis text size, tap targets, sticky headers). check-mobile-webview.mjs remains the
// authoritative seven-class lane; this keeps the visual capture honest at phone width.
const MOBILE_INTEGRITY_PROBE = ({ viewportWidth }) => {
  const findings = [];
  const seen = new Set();
  const push = (check, target, detail) => {
    if (seen.has(`${check}|${target}`)) return;
    seen.add(`${check}|${target}`);
    findings.push({ check, target: String(target).slice(0, 160), detail: String(detail ?? "").slice(0, 200) });
  };
  const describe = (el) => `${el.tagName.toLowerCase()}${el.id ? `#${el.id}` : ""}${typeof el.className === "string" && el.className ? `.${el.className.trim().split(/\s+/).slice(0, 2).join(".")}` : ""}`;
  const sample = (el) => (el.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 30);
  const isVisible = (el) => (typeof el.checkVisibility === "function" ? el.checkVisibility() : true) && el.getBoundingClientRect().width > 0;
  // chart axis / category text >= 11px
  let small = 0;
  for (const el of document.querySelectorAll("svg text, .gclab, .kit-barlist-label, .kit-chart-axis, [class*=axis] text")) {
    if (small >= 6 || !isVisible(el)) continue;
    const size = parseFloat(getComputedStyle(el).fontSize);
    if (size && size < 11) {
      small += 1;
      push("mobile-axis-text-too-small", describe(el), `${size}px < 11px "${sample(el)}"`);
    }
  }
  // tap targets >= 44px — INFORMATIONAL here (recorded, never failing): class 4 of the
  // mobile-webview taxonomy is gated, with its own waivers, by check-mobile-webview.mjs.
  let tiny = 0;
  for (const el of document.querySelectorAll('a[href], button, [role="button"], summary, select, input:not([type="hidden"]), label[for], .chip, .kit-chip, .kit-tab')) {
    if (tiny >= 3 || !isVisible(el)) continue;
    if (el.closest("nav, header, .sidebar, .kit-sidebar, .topbar")) continue; // chrome owned by the shell
    const r = el.getBoundingClientRect();
    if (r.width < 44 || r.height < 44) {
      const cs = getComputedStyle(el);
      const padded = r.width + parseFloat(cs.paddingLeft || 0) + parseFloat(cs.paddingRight || 0);
      if (padded >= 44 && r.height >= 44) continue;
      if (el.tagName === "A" && el.closest("p, td, li, .kit-crumbs")) continue; // inline text links
      if (tiny >= 3) break;
      tiny += 1;
      findings.push({ check: "mobile-tap-target-too-small", info: true, target: describe(el).slice(0, 160), detail: `${Math.round(r.width)}x${Math.round(r.height)} < 44x44 (gated by smoke:webview)` });
    }
  }
  // sticky headers whose NEAREST overflow ancestor is hidden/clip (an auto/scroll owner in
  // between is the intended scrollport and is fine). One finding per table.
  const stickySeen = new Set();
  for (const el of document.querySelectorAll("thead, th, [class*=sticky], .kit-table-head")) {
    if (getComputedStyle(el).position !== "sticky") continue;
    const table = el.closest("table") ?? el;
    if (stickySeen.has(table)) continue;
    for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
      const cs = getComputedStyle(n);
      const o = [cs.overflowX, cs.overflowY];
      if (o.includes("auto") || o.includes("scroll")) break;
      if (o.includes("hidden") || o.includes("clip")) {
        stickySeen.add(table);
        push("mobile-sticky-detached", describe(el), `sticky inside overflow:${o.join("/")} ancestor ${n.tagName.toLowerCase()} with no scroll owner in between`);
        break;
      }
    }
  }
  // clipped table / chart inside the viewport: a table or chart wrapper wider than its scroll owner with no scroll
  for (const el of document.querySelectorAll("table, .minimal__chart__root, svg.chart, .kit-chart")) {
    if (!isVisible(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.right <= viewportWidth + 1) continue;
    let owner = null;
    for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
      const ox = getComputedStyle(n).overflowX;
      if (ox === "auto" || ox === "scroll") { owner = n; break; }
    }
    if (!owner) push("mobile-table-or-chart-clipped", describe(el), `right ${Math.round(r.right)} > ${viewportWidth} with no horizontal scroll owner`);
  }
  return findings;
};

const browser = await chromium.launch(args.browserChannel ? { channel: args.browserChannel } : {});
try {
  for (const profile of profiles) {
    for (const theme of themes) {
      const context = await browser.newContext({
        viewport: profile.viewport,
        isMobile: Boolean(profile.isMobile),
        hasTouch: Boolean(profile.hasTouch),
        deviceScaleFactor: profile.deviceScaleFactor,
        userAgent: profile.userAgent,
        colorScheme: theme,
        locale: "en-SG",
        timezoneId: "Asia/Singapore",
      });
      // Theme exactly as the app's boot script reads it.
      await context.addInitScript(([keys, value]) => {
        try {
          for (const key of keys) window.localStorage.setItem(key, value);
        } catch {}
      }, [THEME_STORAGE_KEYS, theme]);
      // Load-phase sampler (double-skeleton / blank-frame) starts at document start on every hard load.
      await context.addInitScript(LOAD_PHASES_INSTALL);
      if (bearerToken) {
        const cookieUrl = new URL(appBaseUrl);
        await context.addCookies([
          { name: "goatos_firebase_id_token", value: bearerToken, domain: cookieUrl.hostname, path: "/", httpOnly: true, sameSite: "Lax", expires: Math.floor(Date.now() / 1000) + 3600 },
        ]);
      }

      for (const route of routes) {
        if (route.skip) {
          if (profile.label === "desktop" && theme === "dark") skipped.push({ name: route.name, reason: route.skip });
          continue;
        }
        const name = `${route.name}__${profile.label}__${theme}.png`;
        const page = await context.newPage();
        const drainConsole = collectConsoleErrors(page);
        try {
          // Fresh load-phase timeline per route: same-origin sessionStorage survives the redirect chain.
          await page.goto(`${appBaseUrl}/robots.txt`, { waitUntil: "commit", timeout: navigationTimeoutMs }).catch(() => {}); // any same-origin doc (404 is fine)
          await page.evaluate(LOAD_PHASES_CLEAR).catch(() => {});
          const response = await page.goto(`${appBaseUrl}${route.path}`, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
          if (response && response.status() >= 400) throw new Error(`HTTP ${response.status()} for ${route.path}`);
          await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => {});
          // Let skeletons resolve: wait for the app's loading markers to leave (best effort).
          await page.waitForFunction(() => !document.querySelector('[data-loading="true"], .MuiSkeleton-root, [data-skel-root], .skeleton'), { timeout: 8_000 }).catch(() => {});
          await waitForSettledDom(page, "main, body");
          // The theme attribute is applied by the boot script and re-asserted after hydration:
          // wait for the state, do not sample it once.
          const themeApplied = await page
            .waitForFunction((expected) => document.documentElement.getAttribute("data-theme") === expected, theme, { timeout: 5_000 })
            .then(() => true)
            .catch(() => false);
          if (!themeApplied) {
            const appliedTheme = await page.evaluate(() => document.documentElement.getAttribute("data-theme"));
            throw new Error(`theme not applied: expected data-theme=${theme}, got ${appliedTheme}`);
          }
          const bodyText = await page.evaluate(() => document.body.innerText.slice(0, 4000));
          const errorState = bodyText.match(ERROR_STATE_RE)?.[0] ?? (await page.evaluate(() => Boolean(document.querySelector("[data-error-boundary], #error-boundary"))) ? "error boundary" : null);
          if (errorState) {
            // Stall detector: an error page is NEVER a baseline. If the API is also unhealthy
            // right now the capture is "unstable" (infra), otherwise it is a real route failure.
            const apiHealthy = await probeApiHealth();
            unstable.push({ capture: name, reason: errorState, api_healthy: apiHealthy });
            throw new Error(`${apiHealthy ? "route rendered an error state" : "UNSTABLE (API unhealthy during run) — error state"}: ${errorState}`);
          }
          await waitForFonts(page, ROUTE_FONTS);
          // Freeze motion so the pixels are stable, then capture.
          await page.addStyleTag({
            content: `*,*::before,*::after{animation-duration:0s!important;animation-delay:0s!important;transition-duration:0s!important;transition-delay:0s!important;caret-color:transparent!important}`,
          });
          await revealCharts(page);
          await resetScrollPosition(page);
          await waitForSettledDom(page, "main, body");
          const screenshotPath = join(outDir, name);
          await page.screenshot({ path: screenshotPath, fullPage: true, animations: "disabled" });
          captured += 1;

          if (args.integrity) {
            const probed = await page.evaluate(RENDER_INTEGRITY_PROBE, { viewportWidth: profile.viewport.width, fonts: ROUTE_FONTS, textRoot: "main, body" });
            const mobileProbed = profile.isMobile ? await page.evaluate(MOBILE_INTEGRITY_PROBE, { viewportWidth: profile.viewport.width }) : [];
            // Production-bug CLASSES from the 2026-09-25 dashboard.mesha.sg smoke and PR #294
            // invariants audit. `pattern` normalises to the same shape as a render-integrity
            // finding so the same baseline/waiver flow catches them.
            const patternProbed = (await page.evaluate(collectVisualPatternFindings, { viewport: profile.label }))
              .map((f) => ({ check: f.pattern, target: f.target, detail: f.detail }));
            const hoverProbed = (await assertChartHoverStability(page).catch(() => []))
              .map((f) => ({ check: f.pattern, target: f.target, detail: f.detail }));
            const loadFrames = await page.evaluate(LOAD_PHASES_READ).catch(() => []);
            const phaseFindings = judgeLoadPhases(loadFrames, { ignorePaths: ["/robots.txt"] });
            const found = [...probed, ...mobileProbed.filter((f) => !f.info), ...patternProbed, ...hoverProbed, ...phaseFindings, ...drainConsole()];
            for (const info of mobileProbed.filter((f) => f.info)) infoFindings.push({ capture: name, ...info });
            const live = baseline.unwaived(`${route.name}|${profile.label}|${theme}`, found);
            for (const finding of live) integrityFindings.push({ capture: name, ...finding });
            // A baseline refresh never absorbs integrity findings; only an explicit --waive does.
            if (live.length > 0 && !args.waive) {
              throw new Error(`render integrity: ${live.map((f) => `${f.check} ${f.target} (${f.detail})`).slice(0, 4).join(" | ")} — png=${relativeToRepo(screenshotPath)}`);
            }
          }
          baseline.compare(name, screenshotPath);
          perRoute.push({ route: route.name, profile: profile.label, theme, status: "ok", critical: criticalSet.has(route.name), png: relativeToRepo(screenshotPath) });
          console.log(`route_capture_ok=${name}`);

          // Optional drawer/dialog capture: the first overlay trigger the page exposes.
          if (args.openDrawers) {
            const trigger = page.locator('[data-smoke-drawer], button[aria-haspopup="dialog"], [data-testid$="-drawer-trigger"], .kit-rowmenu-trigger, tbody tr[role="button"], tbody tr[data-href], tbody tr a[href]').first();
            if ((await trigger.count()) > 0 && (await trigger.isVisible().catch(() => false))) {
              await trigger.click({ timeout: 5_000 }).catch(() => {});
              const opened = await page
                .waitForSelector('[role="dialog"], .kit-sheet, .kit-dialog-root, [data-drawer-open="true"]', { state: "visible", timeout: 4_000 })
                .then(() => true)
                .catch(() => false);
              if (opened) await waitForSettledDom(page, "body");
              if (opened) {
                const drawerName = `${route.name}__${profile.label}__${theme}__drawer.png`;
                const drawerPath = join(outDir, drawerName);
                await page.screenshot({ path: drawerPath, fullPage: false, animations: "disabled" });
                captured += 1;
                if (args.integrity) {
                  const probed = await page.evaluate(RENDER_INTEGRITY_PROBE, { viewportWidth: profile.viewport.width, fonts: ROUTE_FONTS, requireFonts: false, textRoot: "body" });
                  const mobileProbed = profile.isMobile ? await page.evaluate(MOBILE_INTEGRITY_PROBE, { viewportWidth: profile.viewport.width }) : [];
                  const patternProbed = (await page.evaluate(collectVisualPatternFindings, { viewport: profile.label }))
                    .map((f) => ({ check: f.pattern, target: f.target, detail: f.detail }));
                  const live = baseline.unwaived(`${route.name}|${profile.label}|${theme}|drawer`, [...probed, ...mobileProbed.filter((f) => !f.info), ...patternProbed, ...drainConsole()]);
                  for (const finding of live) integrityFindings.push({ capture: drawerName, ...finding });
                  if (live.length > 0 && !args.waive) throw new Error(`drawer render integrity: ${live.map((f) => `${f.check} ${f.target}`).slice(0, 4).join(" | ")} — png=${relativeToRepo(drawerPath)}`);
                }
                baseline.compare(drawerName, drawerPath);
                console.log(`route_capture_ok=${drawerName}`);
              }
            }
          }
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error);
          failures.push(`${name}: ${message}`);
          perRoute.push({ route: route.name, profile: profile.label, theme, status: "fail", critical: criticalSet.has(route.name), error: message.split("\n")[0].slice(0, 200) });
          console.log(`route_capture_fail=${name} ${message.split("\n")[0].slice(0, 160)}`);
        } finally {
          await page.close().catch(() => {});
        }
      }
      await context.close();
    }
  }
} finally {
  await browser.close();
}

baseline.writeUpdated({
  waiverNote: "Accepted render-integrity debt for admin-web routes (key = check|route|profile|theme|target). Every entry is a KNOWN broken render; shrink it, never grow it to land a change. Never update while the backend is down.",
});

let webviewStatic = null;
if (args.withWebviewStatic) {
  const run = spawnSync(process.execPath, [join(scriptDir, "check-mobile-webview.mjs"), "--static-only", "--baseline-dir", "apps/admin-web/scripts/check-mobile-webview-waivers", "--require-baseline"], { cwd: appDir, encoding: "utf8" });
  const reportPath = (run.stdout ?? "").match(/mobile_webview_report=(\S+)/)?.[1];
  webviewStatic = {
    exit_code: run.status,
    report: reportPath ? relativeToRepo(reportPath) : null,
    failures: reportPath ? JSON.parse(readFileSync(reportPath, "utf8")).findings ?? [] : [],
  };
  if (run.status !== 0) failures.push(`check-mobile-webview --static-only: ${webviewStatic.failures.length} unwaived finding(s), report=${webviewStatic.report}`);
}

const summary = {
  lane: "admin-web-routes-visual",
  app: appBaseUrl,
  webview_critical_only: args.webviewCritical,
  webview_critical_routes: criticalList.filter((name) => routes.some((route) => route.name === name)),
  per_route: perRoute,
  webview_static: webviewStatic,
  routes: routes.length,
  skipped,
  profiles: profiles.map((p) => `${p.label}:${p.viewport.width}x${p.viewport.height}`),
  themes,
  captures: captured,
  expected_captures: routes.filter((r) => !r.skip).length * profiles.length * themes.length,
  baseline: baseline.summary(),
  integrity_findings: integrityFindings.length,
  integrity_by_check: integrityFindings.reduce((acc, f) => ({ ...acc, [f.check]: (acc[f.check] ?? 0) + 1 }), {}),
  informational_findings: infoFindings.length,
  unstable_captures: unstable,
  screenshot_dir: relativeToRepo(outDir),
  failures,
};
writeFileSync(join(outDir, "summary.json"), `${JSON.stringify(summary, null, 2)}\n`);
writeFileSync(join(outDir, "integrity.json"), `${JSON.stringify([...integrityFindings, ...infoFindings], null, 2)}\n`);
console.log(JSON.stringify({ ...summary, per_route: undefined }, null, 2));
console.log("\nPer route:");
for (const route of routes.filter((r) => !r.skip)) {
  const rows = perRoute.filter((r) => r.route === route.name);
  const bad = rows.filter((r) => r.status === "fail");
  console.log(`  ${bad.length ? "FAIL" : " ok "} ${criticalSet.has(route.name) ? "[webview-critical] " : ""}${route.name}  ${rows.length - bad.length}/${rows.length}${bad.length ? `  ${bad.map((r) => `${r.profile}/${r.theme}: ${r.error}`).slice(0, 2).join(" | ")}` : ""}`);
}
if (failures.length > 0) {
  console.error(`\nFAIL: ${failures.length} route capture(s) failed:`);
  for (const failure of failures.slice(0, 40)) console.error(`  - ${failure}`);
  process.exit(1);
}
console.log(`\nOK: ${captured} route captures across ${profiles.length} profile(s) x ${themes.length} theme(s).`);

function relativeToRepo(path) {
  return path.startsWith(repoRoot) ? path.slice(repoRoot.length + 1) : path;
}
function normalizeRepoPath(path) {
  if (!path) return undefined;
  return isAbsolute(path) ? path : join(repoRoot, path);
}
function parseArgs(argv) {
  const parsed = {
    only: undefined,
    profiles: undefined,
    themes: undefined,
    baselineDir: undefined,
    updateBaseline: process.env.GOATOS_VISUAL_UPDATE_BASELINE === "1",
    waive: null,
    requireBaseline: process.env.GOATOS_VISUAL_REQUIRE_BASELINE === "1",
    maxDiffRatio: Number(process.env.GOATOS_VISUAL_MAX_DIFF_RATIO ?? "0.01"),
    integrity: true,
    browserChannel: process.env.GOATOS_SMOKE_BROWSER_CHANNEL,
    webviewCritical: false,
    openDrawers: false,
    withWebviewStatic: false,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--only") parsed.only = String(argv[++i]).split(",").map((s) => s.trim()).filter(Boolean);
    else if (arg === "--profiles") parsed.profiles = String(argv[++i]).split(",").map((s) => s.trim()).filter(Boolean);
    else if (arg === "--themes") parsed.themes = String(argv[++i]).split(",").map((s) => s.trim()).filter(Boolean);
    else if (arg === "--baseline-dir") parsed.baselineDir = argv[++i];
    else if (arg === "--update-baseline") parsed.updateBaseline = true;
    else if (arg === "--waive") { parsed.waive = String(argv[++index] ?? "").trim(); if (!parsed.waive) throw new Error("--waive needs a reason"); }
    else if (arg === "--require-baseline") parsed.requireBaseline = true;
    else if (arg === "--no-integrity") parsed.integrity = false;
    else if (arg === "--webview-critical") parsed.webviewCritical = true;
    else if (arg === "--open-drawers") parsed.openDrawers = true;
    else if (arg === "--with-webview-static") parsed.withWebviewStatic = true;
    else if (arg === "--browser-channel") parsed.browserChannel = argv[++i];
    else if (arg === "--max-diff-ratio") {
      parsed.maxDiffRatio = Number(argv[++i]);
      if (!Number.isFinite(parsed.maxDiffRatio) || parsed.maxDiffRatio < 0 || parsed.maxDiffRatio > 1) throw new Error("--max-diff-ratio must be between 0 and 1");
    } else throw new Error(`Unknown argument: ${arg}`);
  }
  return parsed;
}
