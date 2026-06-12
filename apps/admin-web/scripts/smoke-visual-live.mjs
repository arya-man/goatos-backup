import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "@playwright/test";

const appBaseUrl = "http://127.0.0.1:3300";
const requiredEnv = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID", "GOATOS_IMPORT_RUN_ID"];
const missing = requiredEnv.filter((key) => !process.env[key]);

if (missing.length > 0) {
  console.error(`Missing required live-smoke env: ${missing.join(", ")}`);
  process.exit(2);
}

const apiBaseUrl = trimTrailingSlash(process.env.GOATOS_API_BASE_URL);
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
const importRunId = process.env.GOATOS_IMPORT_RUN_ID;
const screenshotDir = join(
  process.cwd(),
  ".codex-goatos-render",
  "admin-web-screenshots",
  new Date().toISOString().replaceAll(/[:.]/g, "-"),
);

await waitForApp(appBaseUrl);
const goatId = await fetchFirstGoatID(apiBaseUrl, bearerToken);
mkdirSync(screenshotDir, { recursive: true });

const routes = [
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
    const page = await browser.newPage({ viewport: { width: viewport.width, height: viewport.height } });
    for (const route of routes) {
      const url = `${appBaseUrl}${route.path}`;
      await page.goto(url, { waitUntil: "networkidle", timeout: 30_000 });
      const html = await page.content();
      assertHealthyHTML(route.name, html, bearerToken);
      await page.screenshot({
        path: join(screenshotDir, `${viewport.label}-${route.name}.png`),
        fullPage: true,
      });
    }
    await page.close();
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
    },
    null,
    2,
  ),
);

console.log(`screenshots_dir=${relativeToRepo(screenshotDir)}`);
console.log(`goat_id=${goatId}`);
console.log(`routes_captured=${routes.map((route) => route.path).join(",")}`);

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

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function relativeToRepo(path) {
  return path.startsWith(process.cwd()) ? path.slice(process.cwd().length + 1) : path;
}
