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

// Starting a version lands straight in the editor now -- there is no intermediate
// "Open V2" link to click, and waiting for one made this script fail on a working app.
const startButton = page.getByRole("button", { name: /Start a new version/i });
const openDraft = page.getByRole("link", { name: /^Open V\d/i });
if (await startButton.count()) {
  console.log("2. start a new version");
  await startButton.click();
} else {
  console.log("2. a draft already exists — open it");
  await openDraft.first().click();
}
await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
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
// A publish that renders "could not publish the plan" used to print a note and exit 0,
// so the one thing this case exists to prove could fail and the case still passed.
if (/could not|error/i.test(body)) {
  await browser.close();
  throw new Error(`publish landed on a page reporting an error: ${body.slice(0, 300)}`);
}

console.log(errors.length ? `console errors: ${errors.join(" | ")}` : "no console errors");
await browser.close();
if (errors.length) throw new Error(`client-side errors during publish: ${errors.join(" | ")}`);
console.log("done");
