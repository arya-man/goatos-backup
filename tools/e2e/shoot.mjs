#!/usr/bin/env node
// Full-page screenshot of a URL, for eyeballing and for the parity harness.
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright";

const url = process.argv[2];
const out = process.argv[3] ?? ".e2e-artifacts/shot.png";
const width = Number(process.argv[4] ?? 1512);
await mkdir(path.dirname(path.resolve(out)), { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width, height: 950 }, deviceScaleFactor: 1 });
page.on("console", (m) => { if (m.type() === "error") console.error("console:", m.text()); });
const res = await page.goto(url, { waitUntil: "networkidle" });
console.log("status", res?.status());
await page.screenshot({ path: path.resolve(out), fullPage: true });
console.log("wrote", path.resolve(out));
await browser.close();
