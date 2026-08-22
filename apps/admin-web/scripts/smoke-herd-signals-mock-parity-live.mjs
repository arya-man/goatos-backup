import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const baseUrl = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3318").replace(/\/$/, "");
const mockUrl = process.env.GOATOS_HERD_SIGNALS_MOCK_URL ?? "file:///Users/ravi/mesha/goatos/mock/herd-signals-mock.html";
const liveTag = process.env.GOATOS_HERD_SIGNALS_PARITY_TAG ?? "A0002E";
const viewport = { width: 1512, height: 982 };
const artifactDir = resolve(
  process.env.GOATOS_HERD_SIGNALS_PARITY_DIR ??
    join(repoRoot, ".codex-goatos-render", "herd-signals-mock-parity", new Date().toISOString().replaceAll(/[:.]/g, "-")),
);

mkdirSync(artifactDir, { recursive: true });

const browser = await chromium.launch({ channel: "chrome" });
const context = await browser.newContext({ viewport, deviceScaleFactor: 1 });

try {
  const mockPage = await context.newPage();
  await mockPage.goto(mockUrl, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await mockPage.evaluate(() => {
    if (typeof closeFs === "function") closeFs();
  });
  const mockRow = mockPage.locator("tr", { hasText: "A0002E" }).first();
  if ((await mockRow.count()) > 0) await mockRow.click();
  else await mockPage.evaluate(() => openDrawer("A0002E"));
  await mockPage.waitForSelector("#drawer.on .hchart", { timeout: 10_000 });
  await mockPage.waitForTimeout(300);

  const livePage = await context.newPage();
  await livePage.goto(`${baseUrl}/herd-signals?scope_mode=company&hs_tag=${liveTag}#hs-tag-${liveTag}`, {
    waitUntil: "domcontentloaded",
    timeout: 30_000,
  });
  await livePage.waitForSelector("aside.drawer.on .hchart", { timeout: 15_000 });
  await livePage
    .waitForFunction(() => !document.querySelector("aside.drawer .skelrow"), null, { timeout: 15_000 })
    .catch(() => undefined);
  await livePage.waitForTimeout(800);

  const mock = await capture(mockPage, "mock", "#drawer");
  const live = await capture(livePage, "live", "aside.drawer");
  const comparisons = compareRegions(mock, live);
  const geometryFailures = compareGeometry(mock.metrics, live.metrics);

  const report = { artifactDir, mockUrl, liveUrl: livePage.url(), comparisons, mock: mock.metrics, live: live.metrics };
  writeFileSync(join(artifactDir, "report.json"), JSON.stringify(report, null, 2));

  const failed = comparisons.filter((comparison) => !comparison.pass);
  if (failed.length > 0 || geometryFailures.length > 0) {
    throw new Error(
      `Herd Signals mock parity failed: ${[
        ...failed.map((comparison) => `${comparison.name}=${comparison.ratio.toFixed(4)}`),
        ...geometryFailures,
      ].join(", ")}; artifacts=${relativeToRepo(artifactDir)}`,
    );
  }

  console.log(
    `PASS herd-signals mock parity: ${comparisons.map((comparison) => `${comparison.name}=${comparison.ratio.toFixed(4)}`).join(" ")} artifacts=${relativeToRepo(artifactDir)}`,
  );
} finally {
  await browser.close();
}

async function capture(page, label, drawerSelector) {
  const fullPath = join(artifactDir, `${label}-full.png`);
  await page.screenshot({ path: fullPath, fullPage: false });
  const drawer = await page.locator(drawerSelector).first().boundingBox();
  if (!drawer) throw new Error(`${label} drawer was not visible`);

  const regions = {
    drawer: drawer,
    header: await box(page, `${drawerSelector} .dh, ${drawerSelector} .dhd`),
    controls: await box(page, `${drawerSelector} .patrow`),
    chart: await box(page, `${drawerSelector} .hchart`),
    legend: await box(page, `${drawerSelector} .legend`),
    readings: await box(page, `${drawerSelector} .kv`),
    banner: await box(page, `${drawerSelector} .banner`),
  };

  const metrics = {};
  for (const [name, region] of Object.entries(regions)) {
    metrics[name] = { x: round(region.x), y: round(region.y), width: round(region.width), height: round(region.height) };
    await page.screenshot({ path: join(artifactDir, `${label}-${name}.png`), clip: clip(region) });
  }

  return { fullPath, regions, metrics };
}

async function box(page, selector) {
  const locator = page.locator(selector).first();
  const result = await locator.boundingBox();
  if (!result) throw new Error(`missing region: ${selector}`);
  return result;
}

function compareRegions(mock, live) {
  const thresholds = new Map([
    // The aggregate drawer includes live data/text and real packet history, which intentionally
    // differs from the static mock. Keep it in the report, but gate the actionable subregions below.
    ["drawer", 0.10],
    ["header", 0.12],
    ["controls", 0.16],
    ["chart", 0.32],
    ["legend", 0.18],
    ["readings", 0.30],
    ["banner", 0.08],
  ]);

  return [...thresholds.keys()].map((name) => {
    const ratio = comparePngs(join(artifactDir, `mock-${name}.png`), join(artifactDir, `live-${name}.png`), join(artifactDir, `diff-${name}.png`));
    return { name, ratio, max: thresholds.get(name), pass: ratio <= thresholds.get(name) };
  });
}

function compareGeometry(mockMetrics, liveMetrics) {
  const tolerances = new Map([
    ["drawer", { y: 0, width: 0, height: 0 }],
    ["header", { y: 0.5, height: 0.5 }],
    ["controls", { y: 0.5, height: 0.5 }],
    ["chart", { y: 12, height: 1 }],
    ["legend", { y: 8, height: 8 }],
    ["readings", { y: 20, height: 8 }],
    ["banner", { y: 20, height: 1 }],
  ]);
  const failures = [];
  for (const [name, tolerance] of tolerances) {
    for (const [key, maxDelta] of Object.entries(tolerance)) {
      const delta = Math.abs(liveMetrics[name][key] - mockMetrics[name][key]);
      if (delta > maxDelta) failures.push(`${name}.${key}Δ=${delta.toFixed(2)}>${maxDelta}`);
    }
  }
  return failures;
}

function comparePngs(mockPath, livePath, diffPath) {
  const expected = PNG.sync.read(readFileSync(mockPath));
  const actual = PNG.sync.read(readFileSync(livePath));
  const width = Math.min(expected.width, actual.width);
  const height = Math.min(expected.height, actual.height);
  const expectedCrop = cropPng(expected, width, height);
  const actualCrop = cropPng(actual, width, height);
  const diff = new PNG({ width, height });
  const diffPixels = pixelmatch(expectedCrop.data, actualCrop.data, diff.data, width, height, { threshold: 0.1 });
  writeFileSync(diffPath, PNG.sync.write(diff));
  return diffPixels / (width * height);
}

function cropPng(source, width, height) {
  if (source.width === width && source.height === height) return source;
  const target = new PNG({ width, height });
  for (let y = 0; y < height; y += 1) {
    const sourceStart = (y * source.width) << 2;
    const targetStart = (y * width) << 2;
    source.data.copy(target.data, targetStart, sourceStart, sourceStart + (width << 2));
  }
  return target;
}

function clip(region) {
  return {
    x: Math.max(0, region.x),
    y: Math.max(0, region.y),
    width: Math.max(1, Math.min(viewport.width - Math.max(0, region.x), region.width)),
    height: Math.max(1, Math.min(viewport.height - Math.max(0, region.y), region.height)),
  };
}

function round(value) {
  return Math.round(value * 100) / 100;
}

function relativeToRepo(path) {
  return path.startsWith(repoRoot) ? path.slice(repoRoot.length + 1) : path;
}
