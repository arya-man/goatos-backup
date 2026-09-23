// Lane 4 screen driver: drives ONE write journey through the real admin-web UI.
//
// The runner (tools/dashboard-automation/run-write-journeys.mjs) spawns this once per journey via
// `npm --prefix apps/admin-web run smoke:write-journey:live`, with the already-token-substituted
// journey in GOATOS_WRITE_JOURNEY. This script only drives the screen and asserts what a person
// sees; the row the write created, the undeclared-write check, the restore and the restore proof
// all stay in the runner, which owns the database half.
//
// Contract with the runner, in both directions:
//   in  — GOATOS_WRITE_JOURNEY (JSON), GOATOS_WRITE_JOURNEY_TOKEN, GOATOS_WRITE_JOURNEY_SHOT_DIR
//   out — `screenshot_path=<path>` on stdout, always, pass or fail, so a failure always has a
//         picture; `write_journey_failed_step=` and `write_journey_reason=` when the screen failed;
//         exit 0 only when every step ran and every `screenAssertion.visible` entry was on screen.
//
// A screen the harness could not reach is NEVER reported as a broken product: the reason line says
// so in as many words, so a run that did not happen can never be rendered as "publishing did not
// save". That distinction is the whole point of this lane.
import { chromium } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

const journey = JSON.parse(requiredEnv("GOATOS_WRITE_JOURNEY"));
const runToken = process.env.GOATOS_WRITE_JOURNEY_TOKEN ?? "";
const shotDir = process.env.GOATOS_WRITE_JOURNEY_SHOT_DIR ?? ".";
const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const bearerToken = process.env.GOATOS_BEARER_TOKEN ?? "";
const stepTimeoutMs = Number(process.env.GOATOS_WRITE_JOURNEY_STEP_TIMEOUT_MS ?? "15000");

fs.mkdirSync(shotDir, { recursive: true });
const shotPath = path.join(shotDir, `${journey.name}.png`);

let browser;
let failedStep = null;
let reason = null;
let harnessFault = false;
// The row or form a person is currently editing. An inline row editor repeats the same words as
// the page chrome -- a ration row's "Apply" is the same word as the grid filter's "Apply" -- so a
// press that follows a typed field is scoped to that field's own row first. Without this the
// driver presses the filter and reports that saving did not work, which would be a lie.
let lastFilled = null;
let page = null;

try {
  browser = await chromium.launch({ args: ["--no-sandbox"] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const cookieUrl = new URL(appBaseUrl);
  if (bearerToken) {
    await context.addCookies([
      {
        name: "goatos_firebase_id_token",
        value: bearerToken,
        domain: cookieUrl.hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      },
    ]);
  }
  page = await context.newPage();
  const consoleErrors = [];
  page.on("console", (message) => {
    if (message.type() === "error") consoleErrors.push(message.text().slice(0, 200));
  });

  // Step 0 is always "open the screen". A site that will not load is a harness fault until proven
  // otherwise -- the journey never got as far as pressing anything.
  const url = `${appBaseUrl}${journey.url}`;
  let response;
  try {
    response = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 30_000 });
  } catch (error) {
    harnessFault = true;
    throw new Error(`the site could not be reached at ${journey.url}: ${String(error?.message ?? error).slice(0, 200)}`);
  }
  if (response && response.status() >= 500) {
    throw new Error(`the screen returned HTTP ${response.status()}`);
  }
  await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});

  for (const [index, step] of (journey.steps ?? []).entries()) {
    failedStep = step.do ?? `step ${index + 1}`;
    if (step.click) await clickByText(page, step.click.text);
    if (step.fill) await fillByLabel(page, step.fill.label, String(step.fill.value));
    if (step.confirm) await confirmDialog(page);
    await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});
  }
  failedStep = null;

  // What a person must see afterwards. This is the half that has never run before today.
  const missing = [];
  for (const want of journey.screenAssertion?.visible ?? []) {
    const found = await page
      .getByText(String(want.text), { exact: false })
      .first()
      .isVisible({ timeout: stepTimeoutMs })
      .catch(() => false);
    if (!found) missing.push(want.text);
  }
  // What must be GONE afterwards. A publish is only proved by the draft no longer being offered:
  // "Live right now" is on the plan page whenever any version is live, so it passes before the
  // publish too. Without this, the journey reports a green screen for a publish that never landed.
  const stillThere = [];
  for (const want of journey.screenAssertion?.notVisible ?? []) {
    const gone = await page
      .getByText(String(want.text), { exact: false })
      .first()
      .isHidden({ timeout: stepTimeoutMs })
      .catch(() => true);
    if (!gone) stillThere.push(want.text);
  }
  for (const text of stillThere) missing.push(`(still on screen) ${text}`);
  await page.screenshot({ path: shotPath, fullPage: true }).catch(() => {});
  if (missing.length) {
    const shown = await onScreenError(page);
    reason = `${journey.screenAssertion?.description ?? "the screen assertion"} — not on screen: ${missing.join(", ")}${shown ? `; the screen said: "${shown}"` : ""}`;
    throw new Error(reason);
  }
  if (consoleErrors.length) console.log(`write_journey_console_errors=${consoleErrors.length}`);
  console.log(`screenshot_path=${shotPath}`);
  console.log(`write_journey_reason=${journey.screenAssertion?.description ?? "the screen showed what it should"}`);
  await browser.close();
  process.exit(0);
} catch (error) {
  const shown = page ? await onScreenError(page).catch(() => null) : null;
  reason = reason ?? `${String(error?.message ?? error)}${shown ? `; the screen said: "${shown}"` : ""}`;
  try {
    const pages = browser ? browser.contexts().flatMap((c) => c.pages()) : [];
    if (pages[0]) await pages[0].screenshot({ path: shotPath, fullPage: true });
  } catch {}
  if (fs.existsSync(shotPath)) console.log(`screenshot_path=${shotPath}`);
  if (failedStep) console.log(`write_journey_failed_step=${failedStep}`);
  console.log(`write_journey_reason=${harnessFault ? `THE CHECK COULD NOT RUN: ${reason}` : reason}`);
  await browser?.close().catch(() => {});
  process.exit(1);
}

function requiredEnv(name) {
  const value = process.env[name];
  if (!value) {
    console.log(`write_journey_reason=THE CHECK COULD NOT RUN: ${name} was not set by the runner`);
    process.exit(1);
  }
  return value;
}

function trimTrailingSlash(value) {
  return String(value).replace(/\/+$/, "");
}

/** Buttons, links and tabs a person would press, by the words on them. */
async function clickByText(page, text) {
  const target = String(text);
  for (const scope of scopesFor(page)) {
    for (const locator of [
      scope.getByRole("button", { name: target, exact: false }),
      scope.getByRole("link", { name: target, exact: false }),
      scope.getByRole("tab", { name: target, exact: false }),
      scope.getByText(target, { exact: false }),
    ]) {
      const first = locator.first();
      if (await first.isVisible({ timeout: 2_000 }).catch(() => false)) {
        await first.click({ timeout: stepTimeoutMs });
        return;
      }
    }
  }
  throw new Error(`nothing on the screen says "${target}" to press`);
}

/** The edited row, then its form, then the whole page. */
function scopesFor(page) {
  if (!lastFilled) return [page];
  return [
    lastFilled.locator("xpath=ancestor::tr[1]"),
    lastFilled.locator("xpath=ancestor::form[1]"),
    page,
  ];
}

async function fillByLabel(page, label, value) {
  const target = String(label);
  for (const locator of [
    page.getByLabel(target, { exact: false }),
    page.getByPlaceholder(target, { exact: false }),
    page.getByRole("textbox", { name: target, exact: false }),
    page.getByRole("spinbutton", { name: target, exact: false }),
  ]) {
    const first = locator.first();
    if (await first.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await first.fill(value, { timeout: stepTimeoutMs });
      lastFilled = first;
      return;
    }
  }
  throw new Error(`the screen has no field called "${target}" to type into`);
}

/**
 * The second press a destructive or publishing action asks for -- only ever inside a real dialog.
 * Scoped there deliberately: the page's own "Publish plan" button matches the word "Publish" too,
 * and pressing it again while the first press is still in flight hangs the step and reports a
 * timeout, hiding whatever the screen actually said about why the publish failed.
 */
async function confirmDialog(page) {
  const dialog = page.getByRole("dialog").first();
  if (!(await dialog.isVisible({ timeout: 2_000 }).catch(() => false))) return;
  for (const name of ["Confirm", "Publish", "Yes", "OK", "Continue", "Save"]) {
    const button = dialog.getByRole("button", { name, exact: false }).last();
    if (!(await button.isVisible({ timeout: 1_000 }).catch(() => false))) continue;
    if (!(await button.isEnabled().catch(() => false))) continue;
    await button.click({ timeout: stepTimeoutMs });
    return;
  }
  // Not every publish asks twice; a missing confirm step is not a failure on its own.
}

/**
 * What the screen told the person went wrong. A journey that failed because the site refused the
 * write must report the site's own words, not the automation's -- "the publish button timed out"
 * sends someone looking at the test; "could not publish the plan" sends them at the product.
 */
async function onScreenError(page) {
  for (const selector of ['[role="alert"]', "[data-error]", ".text-destructive", '[class*="error"]']) {
    const node = page.locator(selector).first();
    if (!(await node.isVisible({ timeout: 500 }).catch(() => false))) continue;
    const text = (await node.innerText().catch(() => "")).trim().replace(/\s+/g, " ");
    if (text) return text.slice(0, 300);
  }
  return null;
}
