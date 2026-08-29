#!/usr/bin/env node
/**
 * Visual parity: the implemented screen against the approved mock.
 *
 * Screenshots both at the same viewport and compares them pixel by pixel, then
 * writes a side-by-side and a diff mask so a failure can be looked at rather
 * than only counted.
 *
 * The mock is the contract. This exists because "it looks right" is not a
 * check -- the first build of this screen rendered with NO stylesheet at all
 * and still passed every typecheck and HTTP assertion.
 *
 *   node tools/e2e/visual-parity.mjs \
 *     --mock file:///abs/path/mock.html --impl http://127.0.0.1:3399/vaccination/plan \
 *     --out .e2e-artifacts/visual --name plan-list
 *
 * Exit code is non-zero when the difference exceeds --threshold (percent of
 * compared pixels), so it can gate CI.
 */
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import { chromium } from "playwright";
import { PNG } from "pngjs";
import pixelmatch from "pixelmatch";

function arg(name, fallback = null) {
  const i = process.argv.indexOf(`--${name}`);
  return i > -1 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}

const MOCK = arg("mock");
const IMPL = arg("impl");
const OUT = arg("out", ".e2e-artifacts/visual");
const NAME = arg("name", "screen");
const WIDTH = Number(arg("width", "1512"));
const HEIGHT = Number(arg("height", "950"));
const THRESHOLD = Number(arg("threshold", "1.5"));
// The mock renders its own chrome (top bar, sidebar); the app renders the real
// one. Comparing those compares two different components, so the region under
// test is the page content, which is what this change owns.
const CLIP = arg("selector", ".vp, #screenList");

if (!MOCK || !IMPL) {
  console.error("usage: visual-parity.mjs --mock <url> --impl <url> [--out dir] [--name id]");
  process.exit(2);
}

async function shoot(page, url, selector, file) {
  await page.goto(url, { waitUntil: "networkidle" });
  // Both surfaces are theme-aware; pin dark so a theme difference cannot be
  // mistaken for a layout difference.
  await page.emulateMedia({ colorScheme: "dark" });
  const target = await page.$(selector.split(",").map((s) => s.trim()).find(Boolean));
  const found = target ?? (await page.$(selector.split(",")[1]?.trim() ?? "body"));
  const element = found ?? (await page.$("body"));
  const buffer = await element.screenshot();
  await writeFile(file, buffer);
  return PNG.sync.read(buffer);
}

const outDir = path.resolve(OUT);
await mkdir(outDir, { recursive: true });

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: WIDTH, height: HEIGHT }, deviceScaleFactor: 1 });

const mockFile = path.join(outDir, `${NAME}.mock.png`);
const implFile = path.join(outDir, `${NAME}.impl.png`);
const diffFile = path.join(outDir, `${NAME}.diff.png`);

const mockPng = await shoot(page, MOCK, CLIP, mockFile);
const implPng = await shoot(page, IMPL, CLIP, implFile);
await browser.close();

const width = Math.min(mockPng.width, implPng.width);
const height = Math.min(mockPng.height, implPng.height);
const diff = new PNG({ width, height });

function crop(src) {
  const out = new PNG({ width, height });
  PNG.bitblt(src, out, 0, 0, width, height, 0, 0);
  return out;
}

const changed = pixelmatch(crop(mockPng).data, crop(implPng).data, diff.data, width, height, {
  threshold: 0.12,
  includeAA: false,
});
await writeFile(diffFile, PNG.sync.write(diff));

const total = width * height;
const percent = (changed / total) * 100;
const sizeMismatch = mockPng.width !== implPng.width || mockPng.height !== implPng.height;

console.log(`mock  ${mockPng.width}x${mockPng.height}  ${mockFile}`);
console.log(`impl  ${implPng.width}x${implPng.height}  ${implFile}`);
console.log(`diff  ${changed}/${total} pixels = ${percent.toFixed(2)}%  ${diffFile}`);
if (sizeMismatch) console.log(`SIZE MISMATCH: compared the overlapping ${width}x${height} region only`);

if (percent > THRESHOLD || sizeMismatch) {
  console.error(`FAIL: ${percent.toFixed(2)}% differs (threshold ${THRESHOLD}%)${sizeMismatch ? " + size mismatch" : ""}`);
  process.exit(1);
}
console.log(`PASS: within ${THRESHOLD}%`);
