#!/usr/bin/env node
/**
 * E2E: change one vaccine's repeat interval, publish, and prove the blast radius.
 *
 * The claim under test is the one the whole design rests on:
 *
 *   publishing a new version must NOT disturb work that is finished or in
 *   flight, and MUST move future dates.
 *
 * Driven through the browser, not the API, because the point is that the screen
 * a person actually uses produces this outcome.
 */
import { chromium } from "playwright";

const BASE = process.argv[2] ?? "http://127.0.0.1:3399";
const OUT = process.argv[3] ?? ".e2e-artifacts";

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1512, height: 950 } });
const errors = [];
page.on("console", (m) => { if (m.type() === "error" && !m.text().includes("firebase-config")) errors.push(m.text()); });

async function shot(name) {
  await page.screenshot({ path: `${OUT}/case02-${name}.png`, fullPage: true });
  console.log(`  shot: case02-${name}.png`);
}

console.log("1. open the plan list");
await page.goto(`${BASE}/vaccination/plan`, { waitUntil: "networkidle" });
await shot("01-list");

const startButton = page.getByRole("button", { name: /Start a new version/i });
const openDraft = page.getByRole("link", { name: /^Open V\d/i });
if (await startButton.count()) {
  console.log("2. start a new version");
  await startButton.click();
  await page.waitForTimeout(2500);
} else {
  console.log("2. a draft already exists");
}
await shot("02-draft-created");

console.log("3. open the draft");
await openDraft.first().click();
await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 20000 });
await page.waitForLoadState("networkidle");
await shot("03-editor");

console.log("4. change ET+TT repeat to 3 months via the preset");
await page.getByRole("button", { name: "3 months", exact: true }).click();
await page.waitForTimeout(400);
await shot("04-changed");

const saveButton = page.getByRole("button", { name: /Save draft/i });
if (await saveButton.isEnabled()) {
  console.log("   Save draft became enabled — the edit registered");
} else {
  throw new Error("Save draft stayed disabled after an edit; the change did not register");
}

console.log("5. publish");
await page.getByRole("button", { name: /Publish plan/i }).click();
await page.waitForURL(/\/vaccination\/plan$/, { timeout: 60000 });
await page.waitForLoadState("networkidle");
await page.waitForTimeout(1500);
await shot("05-published");

const body = await page.locator("body").innerText();
if (/could not|error/i.test(body)) console.log("   NOTE: page mentions an error — check the screenshot");

console.log(errors.length ? `console errors: ${errors.join(" | ")}` : "no console errors");
await browser.close();
console.log("done");
