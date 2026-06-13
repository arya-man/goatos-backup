import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import AxeBuilder from "@axe-core/playwright";
import { chromium } from "@playwright/test";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";

const appBaseUrl = "http://127.0.0.1:3300";
const requiredEnv = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID", "GOATOS_IMPORT_RUN_ID"];
const missing = requiredEnv.filter((key) => !process.env[key]);
const args = parseArgs(process.argv.slice(2));

if (missing.length > 0) {
  console.error(`Missing required live-smoke env: ${missing.join(", ")}`);
  process.exit(2);
}

const apiBaseUrl = trimTrailingSlash(process.env.GOATOS_API_BASE_URL);
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
const importRunId = process.env.GOATOS_IMPORT_RUN_ID;
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
const goatId = await fetchFirstGoatID(apiBaseUrl, bearerToken);
mkdirSync(screenshotDir, { recursive: true });
if (baselineDir) mkdirSync(diffDir, { recursive: true });

const routes = [
  { name: "login", path: "/login" },
  { name: "overview", path: "/" },
  { name: "counts", path: "/counts" },
  { name: "herd", path: "/herd" },
  { name: "import-review", path: `/import-review?import_run_id=${encodeURIComponent(importRunId)}` },
  { name: "data-quality", path: "/data-quality" },
  { name: "goat-passport", path: `/goats/${encodeURIComponent(goatId)}` },
];

const browser = await chromium.launch();
try {
  for (const viewport of [
    { label: "desktop", width: 1440, height: 1000 },
    { label: "narrow", width: 390, height: 900 },
  ]) {
    const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } });
    const page = await context.newPage();
    for (const route of routes) {
      const url = `${appBaseUrl}${route.path}`;
      await page.goto(url, { waitUntil: "networkidle", timeout: 30_000 });
      const html = await page.content();
      assertHealthyHTML(route.name, html, bearerToken);
      await assertLayoutHealthy(page, route.name, viewport.label);
      await assertA11y(page, route.name, viewport.label);
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
      import_run_id: importRunId,
      routes: routes.map((route) => route.path),
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
console.log(`routes_captured=${routes.map((route) => route.path).join(",")}`);
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
      const response = await fetch(url, { cache: "no-store" });
      if (response.ok) return;
      lastError = new Error(`status ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`admin-web did not respond at ${url}: ${lastError instanceof Error ? lastError.message : String(lastError)}`);
}

async function fetchFirstGoatID(baseUrl, token) {
  const response = await fetch(`${baseUrl}/goats/search?limit=1`, {
    headers: { Authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`backend goat search failed: status ${response.status}`);
  }
  const body = await response.json();
  const goatID = body?.items?.[0]?.goat_id;
  if (typeof goatID !== "string" || goatID.length === 0) {
    throw new Error("backend goat search returned no goat_id for passport smoke");
  }
  return goatID;
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
  const layout = await page.evaluate(() => {
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
    const interactives = Array.from(document.querySelectorAll('a[href], button:not([disabled]), input:not([type="hidden"]), select, textarea, [role="button"], [tabindex]:not([tabindex="-1"])'))
      .filter(isVisible);
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
