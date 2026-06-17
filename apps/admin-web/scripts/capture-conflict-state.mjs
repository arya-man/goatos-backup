import { mkdirSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const appBaseUrl = "http://127.0.0.1:3300";
const appBasePath = "/dashboard";
const conflictId = process.argv[2];
if (!conflictId) {
  console.error("usage: node capture-conflict-state.mjs <conflict_id>");
  process.exit(2);
}
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const outDir = join(repoRoot, ".codex-goatos-render", "admin-web-screenshots", "conflict-state");
mkdirSync(outDir, { recursive: true });

const forbidden = [
  "Local API configuration needed",
  "Bearer authentication failed",
  "Sign in required",
  "Server configuration missing",
  "Backend service is not reachable",
];

const browser = await chromium.launch();
try {
  for (const viewport of [
    { label: "desktop", width: 1440, height: 1000 },
    { label: "narrow", width: 390, height: 900 },
  ]) {
    const context = await browser.newContext({ viewport: { width: viewport.width, height: viewport.height } });
    const page = await context.newPage();
    const url = `${appBaseUrl}${appBasePath}/data-quality?conflict_id=${encodeURIComponent(conflictId)}`;
    await page.goto(url, { waitUntil: "networkidle", timeout: 30_000 });
    const html = await page.content();
    for (const marker of forbidden) {
      if (html.includes(marker)) throw new Error(`selected-conflict rendered failure marker: ${marker}`);
    }
    const path = join(outDir, `${viewport.label}-data-quality-conflict.png`);
    await page.screenshot({ path, fullPage: true });
    console.log(`captured ${path}`);
    await context.close();
  }
} finally {
  await browser.close();
}
