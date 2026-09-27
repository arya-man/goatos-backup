import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const runId = process.env.GOATOS_CLICK_MATRIX_RUN_ID ?? `CLICK-${new Date().toISOString().replaceAll(/[:.]/g, "-")}`;
const reportDir = process.env.GOATOS_CLICK_MATRIX_REPORT_DIR
  ? resolve(process.env.GOATOS_CLICK_MATRIX_REPORT_DIR)
  : join(repoRoot, ".codex-goatos-render", "vaccination-click-matrix", runId);
const failureDir = join(reportDir, "failures");
const results = [];
const allPageIssues = [];
const stepPageIssues = [];

mkdirSync(failureDir, { recursive: true });

const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage();

page.on("console", (message) => {
  if (message.type() !== "error") return;
  const text = message.text();
  if (text.startsWith("Failed to load resource:")) return;
  recordPageIssue(`console: ${text}`);
});
page.on("pageerror", (error) => {
  recordPageIssue(`pageerror: ${error.message}`);
});
page.on("response", (response) => {
  if (response.status() < 500) return;
  if (isExpectedOptionalLocalAuthResponse(response.url())) return;
  recordPageIssue(`HTTP ${response.status()} ${response.url()}`);
});

try {
  await step("shell: top bar, sidebar groups, and sidebar route links", () => verifyShell(page));
  await step("vaccination: header actions, filters, row drawers, and drawer links", () => verifyVaccination(page));
  await step("action center: board filters, queue tab, drawer, and linked records", () => verifyActionCenter(page));
  await step("protocol adherence: filters, ledger drawer, and linked records", () => verifyProtocolAdherence(page));
  await step("workflows: catalog, chain links, and workflow detail", () => verifyWorkflows(page));
  await step("vaccination plan: live card and earlier-version settings", () => verifyVaccinationPlan(page));
} finally {
  await browser.close();
}

const failed = results.filter((r) => r.status === "fail");
writeFileSync(join(reportDir, "matrix.json"), JSON.stringify({ appBaseUrl, runId, results, pageIssues: allPageIssues }, null, 2));
writeFileSync(join(reportDir, "matrix.md"), renderMarkdown());
console.log(`vaccination click matrix ${failed.length ? "failed" : "passed"}; report_dir=${reportDir}`);
if (failed.length > 0) process.exit(1);

async function step(name, fn) {
  const started = Date.now();
  stepPageIssues.length = 0;
  try {
    await fn();
    if (stepPageIssues.length > 0) {
      throw new Error(`page issues: ${stepPageIssues.join(" | ").slice(0, 1000)}`);
    }
    results.push({ name, status: "pass", ms: Date.now() - started });
    console.log(`PASS ${name}`);
  } catch (error) {
    const safeName = name.replaceAll(/[^a-z0-9]+/gi, "-").replaceAll(/^-|-$/g, "").toLowerCase();
    const screenshot = join(failureDir, `${safeName}.png`);
    await page.screenshot({ path: screenshot, fullPage: true }).catch(() => undefined);
    results.push({
      name,
      status: "fail",
      ms: Date.now() - started,
      error: error instanceof Error ? error.message : String(error),
      screenshot,
    });
    console.error(`FAIL ${name}: ${error instanceof Error ? error.message : String(error)}`);
  }
}

async function verifyShell(page) {
  await goto(page, "/vaccination?scope_mode=company");

  // Template dashboard layout: the desktop sidebar is NavVertical (`.minimal__layout__nav__vertical`)
  // and its collapse control is the template NavToggleButton (an IconButton child of the nav) that
  // switches between the full and the mini width.
  const nav = page.locator(".minimal__layout__nav__vertical").first();
  const navToggle = nav.locator(":scope > button.MuiIconButton-root").first();
  await expectCount("nav collapse toggle", navToggle, 1);
  const navWidth = async () => (await nav.boundingBox())?.width ?? 0;
  const fullWidth = await navWidth();
  await navToggle.click();
  await page.waitForTimeout(400);
  if ((await navWidth()) >= fullWidth - 20) throw new Error("nav collapse toggle did not collapse the sidebar to the mini rail");
  await navToggle.click();
  await page.waitForTimeout(400);
  if ((await navWidth()) < fullWidth - 2) throw new Error("nav collapse toggle did not restore the full sidebar");

  const parkWise = page.locator(".parkpick a").filter({ hasText: "Park-wise" }).first();
  if ((await parkWise.count()) === 1) {
    const disabled = await parkWise.getAttribute("aria-disabled");
    if (disabled !== "true") {
      await clickAndExpectPath(page, parkWise, "/vaccination", "Park-wise scope toggle");
      await page.waitForURL((url) => url.searchParams.get("scope_mode") === "park", { timeout: 5_000 });
    }
  }
  const companyWide = page.locator(".parkpick a").filter({ hasText: "Company-wide" }).first();
  await clickAndExpectPath(page, companyWide, "/vaccination", "Company-wide scope toggle");

  await openMenuAndDismiss(page, page.locator("[data-park-scope-trigger]").first(), "park selector");
  await openMenuAndDismiss(page, page.locator(".pscope.date-scope").first(), "date selector");

  const themeButton = page.locator('button[aria-label*="light"], button[aria-label*="dark"]').first();
  if ((await themeButton.count()) === 1) {
    const wasLight = await page.locator("html.light").count();
    await themeButton.click();
    const isLight = await page.locator("html.light").count();
    if (wasLight === isLight) throw new Error("theme toggle did not change html.light state");
    await themeButton.click();
  }

  const roleButton = page.locator("button.msh-account").first();
  await expectCount("account menu button", roleButton, 1);
  await roleButton.click();
  const accountMenu = page.locator(".MuiPopover-paper").last();
  await accountMenu.waitFor({ state: "visible", timeout: 5_000 });
  const roleChoices = accountMenu.locator('[role="menuitem"]:has-text("Preview")');
  if ((await roleChoices.count()) !== 0) throw new Error("account menu should not expose role-preview choices");
  await expectCount("account menu sign out", accountMenu.locator('button[aria-label="Sign out"]'), 1);
  await page.keyboard.press("Escape").catch(() => undefined);

  const notification = page.locator('button[disabled][aria-label*="Notification"], button[disabled][aria-label*="notification"]').first();
  if ((await notification.count()) === 1) {
    const title = await notification.getAttribute("title");
    if (!title) throw new Error("disabled notification button has no reason/title");
  }

  // Groups are template nav items that carry the collapse arrow; toggling one shows/hides the
  // leaf links in its own <li>.
  const groups = page.locator(SIDEBAR_GROUP);
  for (let i = 0; i < await groups.count(); i += 1) {
    const group = groups.nth(i);
    const before = await visibleLeaves(group);
    await group.click();
    await page.waitForTimeout(350);
    const after = await visibleLeaves(group);
    if (before === after) throw new Error(`sidebar group ${i} did not toggle`);
    await group.click();
    await page.waitForTimeout(350);
  }
  await openAllSidebarGroups(page);

  for (const [label, path] of [
    ["Control Tower", "/?lens=control-tower"],
    ["Action Center", "/action-center"],
    ["Calendar", "/calendar"],
    ["Protocol Adherence", "/protocol-adherence"],
    ["Workflows", "/workflows"],
    ["DLQ Center", "/operations/dlq"],
  ]) {
    await goto(page, path);
  }

  for (const [label, path] of [
    ["Vaccination", "/vaccination"],
    ["Source Entry", "/procurement/source-entry"],
    ["SOP Library", "/sops"],
    ["Herd Analytics", "/counts/analytics"],
    ["Audit Log", "/operations/audit"],
  ]) {
    await goto(page, "/vaccination?scope_mode=company");
    await openAllSidebarGroups(page);
    const link = page.locator(".minimal__layout__nav__vertical a.minimal__nav__item__root").filter({ hasText: label }).first();
    if ((await link.count()) === 0) {
      throw new Error(`sidebar link missing: ${label}`);
    }
    await clickAndExpectPath(page, link, path, `sidebar ${label}`);
  }
}

const SIDEBAR_GROUP = ".minimal__layout__nav__vertical .minimal__nav__item__root:has(.minimal__nav__item__arrow)";

async function visibleLeaves(group) {
  return group.locator("xpath=..").locator("a.minimal__nav__item__root:visible").count();
}

async function openAllSidebarGroups(page) {
  const groups = page.locator(SIDEBAR_GROUP);
  for (let i = 0; i < await groups.count(); i += 1) {
    const group = groups.nth(i);
    if ((await visibleLeaves(group)) === 0) {
      await group.click();
      await page.waitForTimeout(350);
    }
  }
}

async function verifyVaccination(page) {
  await goto(page, "/vaccination?scope_mode=company");

  await openAndCloseDialog(page, page.getByRole("button", { name: "SOP", exact: true }), /Vaccination Drive SOP/i, /Close/i, "vaccination SOP quick view");
  await openDialogClickLink(page, page.getByRole("button", { name: "SOP", exact: true }), /Vaccination Drive SOP/i, /Open Vaccination SOP page/i, "/vaccination/plan");
  await goto(page, "/vaccination?scope_mode=company");


  await goto(page, "/vaccination?scope_mode=company");
  const filters = page.getByRole("button", { name: "Filters", exact: true });
  if ((await filters.count()) !== 3) throw new Error(`vaccination expected 3 Filters buttons, got ${await filters.count()}`);
  for (let i = 0; i < 3; i += 1) {
    await openFilterExerciseAndClose(page, filters.nth(i), `vaccination filter ${i + 1}`);
  }

  await openDrawerAndClose(
    page,
    page.locator('section:has-text("Vaccination status matrix") tbody tr td:first-child .celllink').first(),
    /Vaccination work context/i,
    "vaccination status matrix cohort drawer",
  );
  await openDrawerAndClose(
    page,
    page.locator('section:has-text("Vaccination status matrix") tbody tr td:not(:first-child) .celllink').first(),
    /Vaccination work context/i,
    "vaccination status matrix cell drawer",
  );
  await openDrawerClickLink(
    page,
    page.locator('section:has-text("Vaccination status matrix") tbody tr td:not(:first-child) .celllink').first(),
    /Vaccination work context/i,
    /Open Action Center/i,
    "/action-center",
  );
  await goto(page, "/vaccination?scope_mode=company");
  await openDrawerClickLink(
    page,
    page.locator('section:has-text("Vaccination status matrix") tbody tr td:not(:first-child) .celllink').first(),
    /Vaccination work context/i,
    /Protocol Adherence/i,
    "/protocol-adherence",
  );

  await goto(page, "/vaccination?scope_mode=company");
  await openDrawerAndClose(
    page,
    page.locator('section:has-text("Per-cohort vaccination detail") tbody tr .celllink').first(),
    /Vaccination work context/i,
    "vaccination cohort drawer",
  );

  await goto(page, "/vaccination?scope_mode=company#execution");
  await openDrawerAndClose(page, page.locator(".pexec .pexr").first(), /WORK CONTEXT/i, "vaccination shed event drawer");
  await goto(page, "/vaccination?scope_mode=company#execution");
  await openDrawerClickLink(page, page.locator(".pexec .pexr").first(), /WORK CONTEXT/i, /Shed detail/i, "/vaccination/execution/sheds");
  await goto(page, "/vaccination?scope_mode=company#execution");
  await openDrawerClickLink(page, page.locator(".pexec .pexr").first(), /WORK CONTEXT/i, /Open Action Center/i, "/action-center");
}

async function verifyActionCenter(page) {
  await goto(page, "/action-center?scope_mode=company");
  await clickAndExpectPath(page, page.getByRole("link", { name: /SOP queues/i }).first(), "/action-center", "Action Center SOP queues tab");
  await page.waitForURL((url) => url.searchParams.get("bucket") === "verify", { timeout: 5_000 });
  await openFilterExerciseAndClose(page, page.getByRole("button", { name: "Filters", exact: true }).first(), "verification queue filter");

  const queuePassport = page.getByRole("link", { name: /Passport/i }).first();
  if ((await queuePassport.count()) === 1) {
    await clickAndExpectPath(page, queuePassport, "/goats", "verification queue passport link");
  }

  await goto(page, "/action-center?scope_mode=company");
  await openActionCenterFilterPanel(page, page.getByRole("button", { name: "My tasks", exact: true }).first(), "Action Center My tasks");
  await openActionCenterFilterPanel(page, page.getByRole("button", { name: "Filters", exact: true }).first(), "Action Center Filters");
  await clickAndExpectPath(page, page.getByRole("link", { name: /^Overdue/i }).first(), "/action-center", "Action Center Overdue quick filter");
  await page.waitForURL((url) => url.searchParams.get("state") === "overdue", { timeout: 5_000 });

  await goto(page, "/action-center?scope_mode=company");
  const task = page.locator("[data-ac-board] [data-ac-card] a").first();
  if ((await task.count()) === 1) {
    await openDrawerAndClose(page, task, /ACTION/i, "Action Center work drawer");
    await openDrawerClickLink(page, task, /ACTION/i, /Workflow record/i, "/workflows");
    await goto(page, "/action-center?scope_mode=company");
    await openDrawerClickLink(page, page.locator("[data-ac-board] [data-ac-card] a").first(), /ACTION/i, /Goat Passport/i, "/goats", { optional: true });
  }
}

async function verifyProtocolAdherence(page) {
  await goto(page, "/protocol-adherence?scope_mode=company");
  await clickAndExpectPath(page, page.getByRole("link", { name: /At risk/i }).first(), "/protocol-adherence", "Protocol Adherence severity filter");
  await page.waitForURL((url) => url.searchParams.get("severity") === "at_risk", { timeout: 5_000 }).catch(() => undefined);

  await goto(page, "/protocol-adherence?scope_mode=company");
  await openFilterExerciseAndClose(page, page.getByRole("button", { name: "Filters", exact: true }).first(), "Protocol Adherence filter");
  const row = page.locator('section:has-text("Vaccination") tbody tr .celllink').first();
  if ((await row.count()) === 1) {
    await openDrawerAndClose(page, row, /ADHERENCE RECORD/i, "Protocol Adherence row drawer");
    await openDrawerClickLink(page, row, /ADHERENCE RECORD/i, /Open Action Center/i, "/action-center");
    await goto(page, "/protocol-adherence?scope_mode=company");
    await openDrawerClickLink(page, page.locator('section:has-text("Vaccination") tbody tr .celllink').first(), /ADHERENCE RECORD/i, /Workflow record/i, "/workflows");
  }
}

async function verifyWorkflows(page) {
  await goto(page, "/workflows?scope_mode=company");
  await openFilterExerciseAndClose(page, page.getByRole("button", { name: "Filters", exact: true }).first(), "Workflows filter");
  const row = page.locator(".wfcat .wfrow").first();
  if ((await row.count()) === 1) {
    await clickAndExpectPath(page, row, "/workflows", "workflow catalog row");
    await page.waitForURL((url) => url.searchParams.has("workflow"), { timeout: 5_000 });
    await expectVisibleText(page, /Open workflow detail/i, "workflow selected detail action");
    await clickAndExpectPath(page, page.getByRole("link", { name: /Open Action Center/i }).first(), "/action-center", "workflow chain Action Center link");
    await goto(page, "/workflows?scope_mode=company");
    const detail = page.getByRole("link", { name: /Open workflow detail/i }).first();
    if ((await detail.count()) === 1) await clickAndExpectPath(page, detail, "/workflows", "workflow detail route");
  }
}

async function verifyVaccinationPlan(page) {
  // Replaces verifyConfig and verifySops. /config and /vaccination/sops were both
  // removed: the plan console is the single surface that owns vaccination config,
  // and the proof method is one field on the plan rather than a separate SOP.
  await goto(page, "/vaccination/plan?scope_mode=company");
  await assertHealthy(page, "Vaccination plan");
  await expectVisibleText(page, /Vaccination plan/i, "Vaccination plan title");
  await expectVisibleText(page, /Live right now|Start a new version/i, "Vaccination plan live card or empty state");

  const viewSettings = page.getByRole("button", { name: /View settings/i }).first();
  if ((await viewSettings.count()) === 1) {
    await viewSettings.click();
    await page.locator('[role="dialog"]').first().waitFor({ state: "visible", timeout: 5_000 });
    await expectVisibleText(page, /Read-only/i, "earlier-version settings sheet");
    await page.keyboard.press("Escape");
    await page.locator('[role="dialog"]').first().waitFor({ state: "hidden", timeout: 5_000 }).catch(() => undefined);
  }
}
async function goto(page, path) {
  const response = await page.goto(`${appBaseUrl}${path}`, { waitUntil: "networkidle", timeout: 30_000 });
  if (response && !response.ok()) throw new Error(`${path} returned HTTP ${response.status()}`);
  await assertHealthy(page, path);
}

async function assertHealthy(page, label) {
  await page.locator("main").first().waitFor({ state: "visible", timeout: 10_000 });
  const text = await page.locator("body").innerText({ timeout: 5_000 });
  for (const bad of [
    "This page couldn't load",
    "Application error",
    "Unhandled Runtime Error",
    "invalid_bearer_token",
    "backend_down",
    "route_not_registered",
    "Server configuration missing",
  ]) {
    if (text.includes(bad)) throw new Error(`${label} contains unhealthy text: ${bad}`);
  }
}

async function clickAndExpectPath(page, locator, expectedPath, label) {
  await expectAtLeastOne(label, locator);
  await locator.first().scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.pathname === expectedPath || url.pathname.startsWith(`${expectedPath}/`), { timeout: 10_000 }),
    locator.first().click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => undefined);
  await assertHealthy(page, label);
}

async function openMenuAndDismiss(page, trigger, label) {
  await expectCount(label, trigger, 1);
  await trigger.click();
  await page.locator(".MuiPopover-paper").first().waitFor({ state: "visible", timeout: 5_000 });
  await page.keyboard.press("Escape");
  await page.locator(".MuiPopover-paper").first().waitFor({ state: "hidden", timeout: 5_000 }).catch(() => undefined);
}

async function openAndCloseDialog(page, trigger, expectedText, closeName, label) {
  await expectCount(label, trigger, 1);
  await trigger.click();
  await expectVisibleText(page, expectedText, label);
  const dialog = page.locator('[role="dialog"]').first();
  await dialog.waitFor({ state: "visible", timeout: 5_000 });
  const close = dialog.getByRole("button", { name: closeName }).last();
  await expectAtLeastOne(`${label} close`, close);
  await close.click();
  await dialog.waitFor({ state: "hidden", timeout: 5_000 });
}

async function openDialogClickLink(page, trigger, expectedText, linkName, expectedPath) {
  await expectCount(String(expectedText), trigger, 1);
  await trigger.click();
  await expectVisibleText(page, expectedText, String(expectedText));
  await clickAndExpectPath(page, page.getByRole("link", { name: linkName }).first(), expectedPath, `dialog link ${String(linkName)}`);
}

async function openFilterExerciseAndClose(page, trigger, label) {
  await expectAtLeastOne(label, trigger);
  await trigger.first().click();
  const dialog = page.locator('[role="dialog"]').first();
  await dialog.waitFor({ state: "visible", timeout: 5_000 });
  const input = dialog.locator("input").first();
  if ((await input.count()) === 1) {
    await input.fill("vaccination");
  }
  const chips = dialog.locator("button.chip");
  if ((await chips.count()) > 0) {
    await chips.first().click();
  }
  const clear = dialog.getByRole("button", { name: /Clear/i }).first();
  if ((await clear.count()) === 1) await clear.click();
  const apply = dialog.getByRole("button", { name: /Apply/i }).first();
  await expectAtLeastOne(`${label} apply`, apply);
  await apply.scrollIntoViewIfNeeded();
  await apply.click();
  await dialog.waitFor({ state: "hidden", timeout: 5_000 });
}

async function openActionCenterFilterPanel(page, trigger, label) {
  await expectAtLeastOne(label, trigger);
  await trigger.click();
  const dialog = page.locator('[role="dialog"]').first();
  await dialog.waitFor({ state: "visible", timeout: 5_000 });
  const links = dialog.locator("a");
  if ((await links.count()) === 0) throw new Error(`${label} has no filter links`);
  const close = dialog.getByRole("button", { name: /Close/i }).first();
  if ((await close.count()) === 1) await close.click();
  else await page.keyboard.press("Escape");
  await dialog.waitFor({ state: "hidden", timeout: 5_000 }).catch(() => undefined);
}

async function openDrawerAndClose(page, trigger, expectedText, label) {
  await openDrawer(page, trigger, expectedText, label);
  await closeDrawer(page, label);
}

async function openDrawerClickLink(page, trigger, expectedText, linkName, expectedPath, options = {}) {
  await openDrawer(page, trigger, expectedText, String(linkName));
  const link = page.locator(".drawer.on").first().getByRole("link", { name: linkName }).first();
  if ((await link.count()) === 0 && options.optional) {
    await closeDrawer(page, String(linkName));
    return;
  }
  await clickAndExpectPath(page, link, expectedPath, `drawer link ${String(linkName)}`);
}

async function openDrawer(page, trigger, expectedText, label) {
  await expectAtLeastOne(label, trigger);
  const first = trigger.first();
  await first.scrollIntoViewIfNeeded();
  const href = await first.getAttribute("href").catch(() => null);
  const expectedUrl = href ? new URL(href, page.url()) : null;
  await Promise.all([
    expectedUrl
      ? page.waitForURL((url) => url.pathname === expectedUrl.pathname && url.search === expectedUrl.search, { timeout: 10_000 })
      : Promise.resolve(),
    first.click(),
  ]);
  const drawer = page.locator(".drawer.on").first();
  await drawer.waitFor({ state: "visible", timeout: 10_000 });
  await expectVisibleText(page, expectedText, label);
}

async function closeDrawer(page, label) {
  const drawer = page.locator(".drawer.on").first();
  const close = drawer.locator('a[aria-label^="Close"]').first();
  await expectAtLeastOne(`${label} drawer close`, close);
  await close.click();
  await drawer.waitFor({ state: "hidden", timeout: 5_000 });
}

async function expectVisibleText(page, pattern, label) {
  await page.getByText(pattern).first().waitFor({ state: "visible", timeout: 7_500 }).catch(async (error) => {
    const body = await page.locator("body").innerText().catch(() => "");
    throw new Error(`${label} missing visible text ${pattern}; body=${body.replace(/\s+/g, " ").slice(0, 800)}`, { cause: error });
  });
}

async function expectCount(label, locator, n) {
  const count = await locator.count();
  if (count !== n) throw new Error(`${label} expected ${n} elements, got ${count}`);
}

async function expectAtLeastOne(label, locator) {
  const count = await locator.count();
  if (count < 1) throw new Error(`${label} expected at least one element, got ${count}`);
}

function renderMarkdown() {
  const lines = [
    "# Vaccination Click Matrix",
    "",
    `Run: \`${runId}\``,
    `Admin web: \`${appBaseUrl}\``,
    "",
    "| Status | Step | Time | Error |",
    "| --- | --- | ---: | --- |",
  ];
  for (const r of results) {
    lines.push(`| ${r.status} | ${r.name} | ${r.ms}ms | ${r.error ? r.error.replaceAll("|", "\\|") : ""} |`);
  }
  if (allPageIssues.length > 0) {
    lines.push("", "## Page Issues", "", ...allPageIssues.map((e) => `- ${e}`));
  }
  return `${lines.join("\n")}\n`;
}

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function recordPageIssue(issue) {
  stepPageIssues.push(issue);
  allPageIssues.push(issue);
}

function isExpectedOptionalLocalAuthResponse(url) {
  const pathname = new URL(url).pathname;
  return pathname === "/api/auth/firebase-config" || pathname === "/api/auth/session";
}
