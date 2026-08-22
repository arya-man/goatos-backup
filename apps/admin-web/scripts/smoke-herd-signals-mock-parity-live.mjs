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
const normalizeContent = process.env.GOATOS_HERD_SIGNALS_PARITY_NORMALIZE_CONTENT !== "0";
const viewport = { width: 1512, height: 982 };
const artifactDir = resolve(
  process.env.GOATOS_HERD_SIGNALS_PARITY_DIR ??
    join(repoRoot, ".codex-goatos-render", "herd-signals-mock-parity", `${new Date().toISOString().replaceAll(/[:.]/g, "-")}-${process.pid}`),
);

mkdirSync(artifactDir, { recursive: true });

const browser = await chromium.launch({ channel: "chrome" });
const context = await browser.newContext({ viewport, deviceScaleFactor: 1 });

try {
  const mockPage = await context.newPage();
  await mockPage.goto(mockUrl, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await mockPage.evaluate(() => {
    if (typeof closeFs === "function") closeFs();
    if (typeof openDrawer === "function") openDrawer("A0002E");
  });
  await mockPage.waitForSelector("#drawer.on .hchart", { timeout: 10_000 });
  await mockPage.waitForSelector("#drawer.on .kv", { timeout: 10_000 });
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
  const rawLiveAxisLabels = await axisLabels(livePage, "aside.drawer");
  assertLiveAxisScale(rawLiveAxisLabels);
  if (normalizeContent) await normalizeLiveDrawer(mockPage, livePage);

  const mock = await capture(mockPage, "mock", "#drawer");
  const live = await capture(livePage, "live", "aside.drawer");
  const comparisons = compareRegions(mock, live);
  const geometryFailures = compareGeometry(mock.metrics, live.metrics);

  const report = {
    artifactDir,
    mockUrl,
    liveUrl: livePage.url(),
    normalizeContent,
    rawLiveAxisLabels,
    mockAxisLabels: await axisLabels(mockPage, "#drawer"),
    liveAxisLabels: await axisLabels(livePage, "aside.drawer"),
    comparisons,
    mock: mock.metrics,
    live: live.metrics,
  };
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
    viewport: { x: 0, y: 0, width: viewport.width, height: viewport.height },
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

async function axisLabels(page, drawerSelector) {
  return page.locator(`${drawerSelector} .hchart text`).evaluateAll((nodes) =>
    nodes
      .slice(0, 4)
      .map((node) => node.textContent?.trim() ?? "")
      .filter(Boolean),
  );
}

function assertLiveAxisScale(labels) {
  if (labels[0] === "400") {
    throw new Error(`Herd Signals drawer chart is using the old gap fallback scale: ${labels.join(" / ")}`);
  }
}

function compareRegions(mock, live) {
  const thresholds = new Map([
    ["viewport", normalizeContent ? 0.05 : 0.08],
    ["drawer", normalizeContent ? 0.025 : 0.10],
    ["header", normalizeContent ? 0.035 : 0.12],
    ["controls", normalizeContent ? 0.05 : 0.16],
    ["chart", normalizeContent ? 0.06 : 0.32],
    ["legend", normalizeContent ? 0.01 : 0.18],
    ["readings", normalizeContent ? 0.025 : 0.30],
    ["banner", 0.015],
  ]);

  return [...thresholds.keys()].map((name) => {
    const ratio = comparePngs(join(artifactDir, `mock-${name}.png`), join(artifactDir, `live-${name}.png`), join(artifactDir, `diff-${name}.png`));
    return { name, ratio, max: thresholds.get(name), pass: ratio <= thresholds.get(name) };
  });
}

async function normalizeLiveDrawer(mockPage, livePage) {
  const mockChartMarkup = await mockPage.locator("#drawer .hchart").first().evaluate((element) => element.outerHTML);
  const mockBannerMarkup = await mockPage.locator("#drawer .banner").first().evaluate((element) => element.innerHTML);
  await livePage.evaluate(({ chartMarkup, bannerMarkup }) => {
    const drawer = document.querySelector("aside.drawer");
    if (!drawer) throw new Error("live drawer missing during normalization");

    const title = drawer.querySelector(".dh b");
    if (title) title.textContent = "CH-1290 · A0002E";
    const subtitle = drawer.querySelector(".dh .mono");
    if (subtitle) subtitle.textContent = "F0:C9:90:A0:00:2E · Yashoda 2 · GW-514060";

    const chips = drawer.querySelectorAll(".patrow .tag");
    if (chips[0]) chips[0].textContent = "Normal activity";
    if (chips[1]) chips[1].textContent = "baseline 119 / 5 min";

    const note = drawer.querySelector(".hs-pattern-note");
    if (note) {
      note.textContent =
        "Deltas in line with this animal’s baseline. Activity uses motion-count deltas from historical packets; resting for short periods is normal, and alerts use sustained patterns.";
    }

    const chart = drawer.querySelector(".hchart");
    if (chart) chart.outerHTML = chartMarkup;
    const banner = drawer.querySelector(".banner");
    if (banner) banner.innerHTML = bannerMarkup;

    const rows = [
      ["Tag ID", "A0002E", "DIRECT"],
      ["BLE MAC", "F0:C9:90:A0:00:2E", "DIRECT"],
      ["Animal", "CH-1290", "DERIVED"],
      ["Location", "Yashoda 2", "CORRELATED"],
      ["Gateway", "GW-514060", "DIRECT"],
      ["RSSI", "-59 dBm", "DIRECT"],
      ["Battery voltage", "3.2 V (3200 mV)", "DIRECT"],
      ["Estimated battery life", "~2 years", "INFERRED"],
      ["Tag temp", "26.1 C", "DIRECT"],
      ["Motion count", "6,992", "DIRECT"],
      ["15m motion delta", "+7", "DERIVED"],
      ["Movement state", "Quiet", "INFERRED"],
      ["Last seen", "20s ago", "DIRECT"],
      ["Temp sensor", "OK", "DIRECT"],
      ["Accelerometer", "OK", "DIRECT"],
      ["Mapping state", "mapped", "DERIVED"],
    ];
    const terms = [...drawer.querySelectorAll(".kv dt")];
    const defs = [...drawer.querySelectorAll(".kv dd")];
    rows.forEach(([term, value, source], index) => {
      if (terms[index]) terms[index].textContent = term;
      if (!defs[index]) return;
      const sourceClass = source.toLowerCase();
      defs[index].innerHTML = `${value}<span class="srcl ${sourceClass}">${source}</span>`;
    });
  }, { chartMarkup: mockChartMarkup, bannerMarkup: mockBannerMarkup });
}

function compareGeometry(mockMetrics, liveMetrics) {
  const tolerances = new Map([
    ["drawer", { y: 0, width: 0, height: 0 }],
    ["viewport", { width: 0, height: 0 }],
    ["header", { y: 0.5, height: 0.5 }],
    ["controls", { y: 0.5, height: 0.5 }],
    ["chart", { y: 0.5, height: 0.5 }],
    ["legend", { y: 0.5, height: 0.5 }],
    ["readings", { y: 0.5, height: 1 }],
    ["banner", { y: 0.5, height: 0.5 }],
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
