import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const runId = process.env.GOATOS_AUTHORING_RUN_ID ?? `AUTHORING-${new Date().toISOString().replaceAll(/[:.]/g, "-")}`;
const reportDir = process.env.GOATOS_AUTHORING_REPORT_DIR
  ? resolve(process.env.GOATOS_AUTHORING_REPORT_DIR)
  : join(repoRoot, ".codex-goatos-render", "vaccination-authoring", runId);
const failureDir = join(reportDir, "failures");
const results = [];
const allPageIssues = [];
const stepPageIssues = [];

mkdirSync(failureDir, { recursive: true });

const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
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
  const authoredSop = await step("sop builder: create, validate, dry-run, publish, reopen, edit, republish", () => verifySopAuthoring(page));
  if (authoredSop) {
    await step("config: create source-backed vaccination rule, preview, save, publish, reopen drawer", () => verifyConfigAuthoring(page, authoredSop));
  }
} finally {
  await browser.close();
}

const failed = results.filter((r) => r.status === "fail");
writeFileSync(join(reportDir, "authoring.json"), JSON.stringify({ appBaseUrl, runId, results, pageIssues: allPageIssues }, null, 2));
writeFileSync(join(reportDir, "authoring.md"), renderMarkdown());
console.log(`vaccination authoring smoke ${failed.length ? "failed" : "passed"}; report_dir=${reportDir}`);
if (failed.length > 0) process.exit(1);

async function step(name, fn) {
  const started = Date.now();
  stepPageIssues.length = 0;
  try {
    const value = await fn();
    if (stepPageIssues.length > 0) {
      throw new Error(`page issues: ${stepPageIssues.join(" | ").slice(0, 1000)}`);
    }
    results.push({ name, status: "pass", ms: Date.now() - started });
    console.log(`PASS ${name}`);
    return value;
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
    return undefined;
  }
}

async function verifySopAuthoring(page) {
  const suffix = runId.toLowerCase().replaceAll(/[^a-z0-9]+/g, "_").slice(0, 42);
  const sopName = `Vaccination Authoring ${suffix}`;
  const labels = [
    `Intake note ${suffix}`,
    `Measured temperature ${suffix}`,
    `Cold chain intact ${suffix}`,
    `Route selected ${suffix}`,
    `Symptoms observed ${suffix}`,
    `Goat scanned ${suffix}`,
    `Shed selected ${suffix}`,
    `Vaccine batch selected ${suffix}`,
    `Medicine selected ${suffix}`,
    `Photo proof captured ${suffix}`,
    `Video proof captured ${suffix}`,
  ];
  const editedLabel = `${labels[0]} edited`;
  const types = [
    "text",
    "number",
    "yesno",
    "select",
    "multiselect",
    "goat_scan",
    "shed_picker",
    "vaccine_batch_picker",
    "medicine_picker",
    "photo_proof",
    "video_proof",
  ];

  await goto(page, "/sops?scope_mode=company&new=1");
  let dialog = await openSopBuilder(page);
  await fillSopBuilder(dialog, sopName, types, labels);

  const publishBeforeSave = dialog.getByRole("button", { name: /Publish/i }).first();
  await expectAtLeastOne("publish before save", publishBeforeSave);
  if (!(await publishBeforeSave.isDisabled())) {
    throw new Error("SOP builder Publish is enabled before Save draft");
  }
  const disabledReason = await publishBeforeSave.getAttribute("title");
  if (!disabledReason || !/save/i.test(disabledReason)) {
    throw new Error(`SOP builder Publish disabled reason missing/incorrect: ${disabledReason ?? ""}`);
  }

  await saveDryRunPublish(dialog, /Draft saved/i, "new SOP");
  await openSopDetail(page, sopName);
  dialog = page.locator('[role="dialog"]').first();
  for (const label of labels) {
    await expectVisibleTextIn(dialog, new RegExp(escapeRegExp(label), "i"), `published SOP field ${label}`);
  }

  await dialog.getByRole("button", { name: /builder/i }).click();
  dialog = await page.getByRole("dialog", { name: /New SOP form builder/i }).waitFor({ state: "visible", timeout: 10_000 }).then(() => page.getByRole("dialog", { name: /New SOP form builder/i }).first());
  await expectInputValue(dialog.getByLabel("SOP name"), sopName, "edit builder SOP name");
  await expectInputValue(dialog.locator(".sopstep").first().locator("input").first(), labels[0], "edit builder first step");
  await dialog.locator(".sopstep").first().locator("input").first().fill(editedLabel);

  await saveDryRunPublish(dialog, /Edited draft saved/i, "edited SOP version");
  await openSopDetail(page, sopName);
  dialog = page.locator('[role="dialog"]').first();
  await expectVisibleTextIn(dialog, new RegExp(escapeRegExp(editedLabel), "i"), "edited SOP field label");
  return { sopName, suffix };
}

async function verifyConfigAuthoring(page, authoredSop) {
  const protocolCode = `vacc_e2e_${authoredSop.suffix}`;
  const protocolName = `Vaccination Config ${authoredSop.suffix}`;
  const approvedAt = "2026-07-01T08:00:00Z";
  const expectedSourceRows = ["ET+TT", "PPR", "Goat Pox", "FMD", "HS"];

  await goto(page, "/config?scope_mode=company&category=vaccination");
  await page.getByRole("link", { name: /New draft rule/i }).first().click();
  const dialog = page.getByTestId("rule-editor").first();
  await dialog.waitFor({ state: "visible", timeout: 10_000 });

  const publishBeforeSave = dialog.getByRole("button", { name: /Publish/i }).first();
  await expectAtLeastOne("config publish before save", publishBeforeSave);
  if (!(await publishBeforeSave.isDisabled())) {
    throw new Error("Config Publish is enabled before Save draft");
  }
  const disabledReason = await publishBeforeSave.getAttribute("title");
  if (!disabledReason || !/save/i.test(disabledReason)) {
    throw new Error(`Config Publish disabled reason missing/incorrect before save: ${disabledReason ?? ""}`);
  }

  const protocolInputs = dialog.locator('input[aria-label="Protocol code · name"]');
  await protocolInputs.nth(0).fill(protocolCode);
  await protocolInputs.nth(1).fill(protocolName);

  await dialog.locator('input[aria-label="Scope · effective from"]').first().fill("2026-07-01");
  await selectOptionByText(dialog.locator('select[aria-label="Executable SOP version (required to publish)"]').first(), authoredSop.sopName, "executable SOP version");

  await dialog.getByRole("button", { name: /Load Source Vaccine Matrix/i }).click();
  await expectVisibleTextIn(dialog, /5 rules/i, "loaded source vaccine matrix row count");
  for (const [index, code] of expectedSourceRows.entries()) {
    await expectInputValue(dialog.locator(`input[aria-label="Vaccine ${index + 1}"]`), code, `loaded source row ${code}`);
  }
  await expectDomTextIn(dialog, /Source schedule/i, "source schedule column");
  await expectDomTextIn(dialog, /Revaccination/i, "revaccination column");
  await expectDomTextIn(dialog, /Vial/i, "vial dose column");
  await expectDomTextIn(dialog, /kid critical schedule: mother vaccinated 4 and 7 weeks/i, "ET+TT source schedule");
  await expectDomTextIn(dialog, /adult revaccination: 6 months after accepted completion/i, "ET+TT adult revaccination schedule");
  await expectDomTextIn(dialog, /all \(every stage\)/i, "source matrix uses all-stage age-based schedule");
  await expectDomTextIn(dialog, /V1 live-live spacing effective due 140d/i, "Goat Pox live-live spacing");
  await expectDomTextIn(dialog, /182/i, "source revaccination interval");
  await expectVisibleTextIn(dialog, /Cross-vaccine spacing policy/i, "compatibility policy controls");
  await expectVisibleTextIn(dialog, /Procurement \/ source policy/i, "procurement policy controls");
  await expectVisibleTextIn(dialog, /Pregnancy \/ delivery policy/i, "pregnancy policy controls");

  const matrixTable = dialog.locator("table").first();
  await expectVisibleTextIn(matrixTable, /derived/i, "matrix source schedule derived badge");
  await matrixTable.locator("tbody tr").first().click();
  await expectInputValueMatches(dialog.locator('input[aria-label="Source schedule"]').first(), /kid critical schedule: mother vaccinated 4 and 7 weeks/i, "selected ET+TT source schedule detail");

  await dialog.getByRole("button", { name: /^Add matrix row$/i }).click();
  await expectVisibleTextIn(dialog, /6 rules/i, "blank matrix row count");
  await expectInputValue(dialog.locator('input[aria-label="Vaccine 6"]'), "", "blank add row vaccine code");
  await expectInputValue(dialog.locator('input[aria-label="Name 6"]'), "", "blank add row vaccine name");
  const blankRow = matrixTable.locator("tbody tr").nth(5);
  await expectDomTextIn(blankRow, /No source schedule/i, "blank row source schedule");
  await dialog.getByRole("button", { name: /Save draft/i }).click();
  await expectVisibleTextIn(dialog, /matrix row 6: vaccine code is required/i, "blank row validation");
  await blankRow.getByRole("button", { name: /Remove matrix row 6/i }).click();
  await expectVisibleTextIn(dialog, /5 rules/i, "blank row removed");

  await matrixTable.locator("tbody tr").first().click();
  await dialog.getByRole("button", { name: /^Copy selected row$/i }).click();
  await expectInputValue(dialog.locator('input[aria-label="Vaccine 6"]'), "ET+TT_COPY", "copy selected row vaccine code");
  await expectInputValueMatches(dialog.locator('input[aria-label="Source schedule"]').first(), /kid critical schedule: mother vaccinated 4 and 7 weeks/i, "copied row source schedule detail");
  await matrixTable.locator("tbody tr").nth(5).getByRole("button", { name: /Remove matrix row 6/i }).click();
  await expectVisibleTextIn(dialog, /5 rules/i, "copied row removed");

  const sourceSelects = dialog.locator('select[aria-label="Source & review (publish needs a real source + approval)"]');
  await sourceSelects.nth(0).selectOption("vaccinations_db");
  await sourceSelects.nth(1).selectOption("approved");
  const sourceInputs = dialog.locator('input[aria-label="Source & review (publish needs a real source + approval)"]');
  await sourceInputs.nth(0).fill(`source://${authoredSop.suffix}/vaccination-matrix`);
  await sourceInputs.nth(1).fill("codex-e2e-reviewer");
  await sourceInputs.nth(2).fill("codex-e2e-approver");
  await sourceInputs.nth(3).fill(approvedAt);
  await expectVisibleTextIn(dialog, /Approved - publishable/i, "source-backed publish badge");

  await dialog.getByRole("button", { name: /Preview Impact/i }).click();
  await expectVisibleTextIn(dialog, /Eligible goats/i, "config impact eligible goats");
  await expectVisibleTextIn(dialog, /Obligations \/ cycle/i, "config impact obligations");

  await dialog.getByRole("button", { name: /Save draft/i }).click();
  await expectVisibleTextIn(dialog, /draft saved/i, "config draft saved");
  await expectVisibleTextIn(dialog, /5 matrix drafts saved/i, "config source matrix rows persisted");

  const publish = dialog.getByRole("button", { name: /^Publish$/i }).last();
  if (await publish.isDisabled()) {
    throw new Error(`Config Publish stayed disabled after valid save: ${(await publish.getAttribute("title")) ?? ""}`);
  }
  await publish.click();
  await expectVisibleTextIn(dialog, /5 matrix rows published/i, "config source matrix rows published");

  await goto(page, "/config?scope_mode=company&category=vaccination");
  const search = page.locator(".tsearch input").first();
  await expectAtLeastOne("Config search", search);
  await search.fill(protocolName);
  const row = page.locator("tbody tr").filter({ hasText: protocolName }).first();
  await row.waitFor({ state: "visible", timeout: 15_000 });
  await expectVisibleTextIn(row, /published/i, "published config row");
  await row.click();
  const drawer = page.locator(".drawer.on").first();
  await drawer.waitFor({ state: "visible", timeout: 10_000 });
  await expectVisibleTextIn(drawer, new RegExp(escapeRegExp(protocolName), "i"), "config drawer protocol name");
  await expectVisibleTextIn(drawer, /ET\+TT/i, "config drawer first source vaccine code");
  await expectVisibleTextIn(drawer, /required_proofs/i, "config drawer proof policy");
  await expectVisibleTextIn(drawer, /source_schedule/i, "config drawer source schedule");
  await expectVisibleTextIn(drawer, /vial_doses/i, "config drawer vial dose metadata");
  await expectVisibleTextIn(drawer, /revaccination_interval_days/i, "config drawer revaccination metadata");

  await goto(page, "/config?scope_mode=company&category=vaccination");
  await search.fill("Goat Pox");
  const goatPoxRow = page.locator("tbody tr").filter({ hasText: "Goat Pox" }).first();
  await goatPoxRow.waitFor({ state: "visible", timeout: 15_000 });
  await expectVisibleTextIn(goatPoxRow, /published/i, "Goat Pox published config row");
}

async function openSopBuilder(page) {
  let dialog = page.getByRole("dialog", { name: /New SOP form builder/i }).first();
  if ((await dialog.count()) === 0 || !(await dialog.isVisible().catch(() => false))) {
    await page.getByRole("button", { name: /New SOP/i }).first().click();
    dialog = page.getByRole("dialog", { name: /New SOP form builder/i }).first();
  }
  await dialog.waitFor({ state: "visible", timeout: 10_000 });
  return dialog;
}

async function fillSopBuilder(dialog, sopName, types, labels) {
  await dialog.getByLabel("SOP name").fill(sopName);

  while ((await dialog.locator(".sopstep").count()) < types.length) {
    await dialog.getByRole("button", { name: /Add step/i }).click();
  }

  for (let i = 0; i < types.length; i += 1) {
    const step = dialog.locator(".sopstep").nth(i);
    await step.locator("select").first().selectOption(types[i]);
    await step.locator("input").first().fill(labels[i]);
  }

  await setConditional(dialog.locator(".sopstep").nth(1), "0", "require_if");
  await setConditional(dialog.locator(".sopstep").nth(2), "1", "require_proof");
  await setConditional(dialog.locator(".sopstep").nth(3), "2", "block_if_empty");
}

async function setConditional(step, showIf, onAnswer) {
  const selects = step.locator(".ss-cond select");
  if ((await selects.count()) < 2) throw new Error("conditional controls missing from SOP step");
  await selects.nth(0).selectOption(showIf);
  await selects.nth(1).selectOption(onAnswer);
}

async function saveDryRunPublish(dialog, savedPattern, label) {
  await dialog.getByRole("button", { name: /Save draft|Re-save draft/i }).click();
  await expectVisibleTextIn(dialog, savedPattern, `${label} save`);
  await expectVisibleTextIn(dialog, /form_dsl valid/i, `${label} backend validation`);

  await dialog.getByRole("button", { name: /Dry-run/i }).click();
  await expectVisibleTextIn(dialog, /dry-run/i, `${label} dry-run`);
  await expectVisibleTextIn(dialog, /workflow:/i, `${label} dry-run workflow`);

  const publish = dialog.getByRole("button", { name: /Publish/i }).first();
  await expectAtLeastOne(`${label} publish`, publish);
  if (await publish.isDisabled()) {
    throw new Error(`${label} Publish stayed disabled: ${(await publish.getAttribute("title")) ?? ""}`);
  }
  await publish.click();
  await dialog.waitFor({ state: "hidden", timeout: 20_000 });
}

async function openSopDetail(page, sopName) {
  await goto(page, "/sops?scope_mode=company");
  const search = page.locator(".tsearch input").first();
  await expectAtLeastOne("SOP search", search);
  await search.fill(sopName);
  const card = page.locator("#sopCards .card").filter({ hasText: sopName }).first();
  await card.waitFor({ state: "visible", timeout: 15_000 });
  await card.click();
  const dialog = page.locator('[role="dialog"]').first();
  await dialog.waitFor({ state: "visible", timeout: 10_000 });
  await expectVisibleTextIn(dialog, new RegExp(escapeRegExp(sopName), "i"), "SOP detail name");
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

async function expectVisibleTextIn(locator, pattern, label) {
  await locator.getByText(pattern).first().waitFor({ state: "visible", timeout: 15_000 }).catch(async (error) => {
    const text = await locator.innerText().catch(() => "");
    throw new Error(`${label} missing visible text ${pattern}; text=${text.replace(/\s+/g, " ").slice(0, 900)}`, { cause: error });
  });
}

async function expectDomTextIn(locator, pattern, label) {
  const deadline = Date.now() + 15_000;
  let text = "";
  while (Date.now() < deadline) {
    text = await locator.evaluate((node) => node.textContent ?? "").catch(() => "");
    if (pattern.test(text)) return;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`${label} missing DOM text ${pattern}; text=${text.replace(/\s+/g, " ").slice(0, 900)}`);
}

async function expectAtLeastOne(label, locator) {
  const count = await locator.count();
  if (count < 1) throw new Error(`${label} expected at least one element, got ${count}`);
}

async function expectInputValue(locator, expected, label) {
  await expectAtLeastOne(label, locator);
  const actual = await locator.first().inputValue();
  if (actual !== expected) throw new Error(`${label} expected value ${expected}, got ${actual}`);
}

async function expectInputValueMatches(locator, pattern, label) {
  await expectAtLeastOne(label, locator);
  const actual = await locator.first().inputValue();
  if (!pattern.test(actual)) throw new Error(`${label} expected value matching ${pattern}, got ${actual}`);
}

async function selectOptionByText(select, text, label) {
  await expectAtLeastOne(label, select);
  const value = await select.evaluate((node, needle) => {
    const options = Array.from(node.options);
    const match = options.find((option) => option.textContent?.includes(String(needle)));
    return match?.value ?? "";
  }, text);
  if (!value) throw new Error(`${label} option containing ${text} not found`);
  await select.selectOption(value);
}

function renderMarkdown() {
  const lines = [
    "# Vaccination Authoring Smoke",
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

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
