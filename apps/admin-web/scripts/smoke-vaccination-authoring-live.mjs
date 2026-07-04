import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const appBaseUrl = trimTrailingSlash(
  process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300",
);
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const runId =
  process.env.GOATOS_AUTHORING_RUN_ID ??
  `AUTHORING-${new Date().toISOString().replaceAll(/[:.]/g, "-")}`;
const reportDir = process.env.GOATOS_AUTHORING_REPORT_DIR
  ? resolve(process.env.GOATOS_AUTHORING_REPORT_DIR)
  : join(repoRoot, ".codex-goatos-render", "vaccination-authoring", runId);
const failureDir = join(reportDir, "failures");
const results = [];
const allPageIssues = [];
const stepPageIssues = [];

mkdirSync(failureDir, { recursive: true });

const browser = await chromium.launch();
const context = await browser.newContext({
  viewport: { width: 1440, height: 1100 },
});
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
  const authoredSop = await step(
    "sop builder: create, validate, dry-run, publish, reopen, edit, republish",
    () => verifySopAuthoring(page),
  );
  if (authoredSop) {
    await step(
      "config: create vaccination matrix rule, preview, save, publish, reopen drawer",
      () => verifyConfigAuthoring(page, authoredSop),
    );
  }
} finally {
  await browser.close();
}

const failed = results.filter((r) => r.status === "fail");
writeFileSync(
  join(reportDir, "authoring.json"),
  JSON.stringify(
    { appBaseUrl, runId, results, pageIssues: allPageIssues },
    null,
    2,
  ),
);
writeFileSync(join(reportDir, "authoring.md"), renderMarkdown());
console.log(
  `vaccination authoring smoke ${failed.length ? "failed" : "passed"}; report_dir=${reportDir}`,
);
if (failed.length > 0) process.exit(1);

async function step(name, fn) {
  const started = Date.now();
  stepPageIssues.length = 0;
  try {
    const value = await fn();
    if (stepPageIssues.length > 0) {
      throw new Error(
        `page issues: ${stepPageIssues.join(" | ").slice(0, 1000)}`,
      );
    }
    results.push({ name, status: "pass", ms: Date.now() - started });
    console.log(`PASS ${name}`);
    return value;
  } catch (error) {
    const safeName = name
      .replaceAll(/[^a-z0-9]+/gi, "-")
      .replaceAll(/^-|-$/g, "")
      .toLowerCase();
    const screenshot = join(failureDir, `${safeName}.png`);
    await page
      .screenshot({ path: screenshot, fullPage: true })
      .catch(() => undefined);
    results.push({
      name,
      status: "fail",
      ms: Date.now() - started,
      error: error instanceof Error ? error.message : String(error),
      screenshot,
    });
    console.error(
      `FAIL ${name}: ${error instanceof Error ? error.message : String(error)}`,
    );
    return undefined;
  }
}

async function verifySopAuthoring(page) {
  const suffix = runId
    .toLowerCase()
    .replaceAll(/[^a-z0-9]+/g, "_")
    .slice(0, 42);
  const sopName = `Vaccination Authoring ${suffix}`;

  await goto(page, "/sops?scope_mode=company&new=1");
  const builder = page.locator("main").first();
  await builder.getByLabel("SOP name").fill(sopName);
  const choiceInputs = builder.locator('input[placeholder="Choice text"]');
  if ((await choiceInputs.count()) >= 2) {
    await choiceInputs.nth(0).fill("subcutaneous");
    await choiceInputs.nth(1).fill("intramuscular");
  }

  const publishBeforeSave = builder
    .getByRole("button", { name: /Publish/i })
    .first();
  await expectAtLeastOne("publish before save", publishBeforeSave);
  if (!(await publishBeforeSave.isDisabled())) {
    throw new Error("SOP builder Publish is enabled before Save draft");
  }
  const disabledReason = await publishBeforeSave.getAttribute("title");
  if (!disabledReason || !/save/i.test(disabledReason)) {
    throw new Error(
      `SOP builder Publish disabled reason missing/incorrect: ${disabledReason ?? ""}`,
    );
  }

  await saveDryRunPublish(builder, /Draft saved/i, "new SOP");
  await openSopDetail(page, sopName);
  return { sopName, suffix };
}

async function verifyConfigAuthoring(page, authoredSop) {
  const protocolCode = `vacc_e2e_${authoredSop.suffix}`;
  const protocolName = `Vaccination Config ${authoredSop.suffix}`;
  const expectedMatrixRows = [
    "ET+TT",
    "PPR",
    "Goat Pox",
    "FMD",
    "HS",
    "Blue Tongue",
    "Sheep Pox",
  ];

  await goto(page, "/config?scope_mode=company&category=vaccination");
  await page
    .getByRole("link", { name: /New draft rule/i })
    .first()
    .click();
  const dialog = page.getByTestId("rule-editor").first();
  await dialog.waitFor({ state: "visible", timeout: 10_000 });

  const publishBeforeSave = dialog
    .getByRole("button", { name: /Publish/i })
    .first();
  await expectAtLeastOne("config publish before save", publishBeforeSave);
  if (!(await publishBeforeSave.isDisabled())) {
    throw new Error("Config Publish is enabled before Save draft");
  }
  const disabledReason = await publishBeforeSave.getAttribute("title");
  if (!disabledReason || !/save/i.test(disabledReason)) {
    throw new Error(
      `Config Publish disabled reason missing/incorrect before save: ${disabledReason ?? ""}`,
    );
  }

  const protocolInputs = dialog.locator(
    'input[aria-label="Protocol code · name"]',
  );
  await protocolInputs.nth(0).fill(protocolCode);
  await protocolInputs.nth(1).fill(protocolName);

  await dialog
    .locator('input[aria-label="Scope · effective from"]')
    .first()
    .fill("2026-07-01");
  await selectOptionByText(
    dialog
      .locator(
        'select[aria-label="Executable SOP version (required to publish)"]',
      )
      .first(),
    authoredSop.sopName,
    "executable SOP version",
  );

  await dialog.getByRole("button", { name: /Load approved vaccine plan|Load vaccine matrix/i }).click();
  await expectVisibleTextIn(
    dialog,
    /7\s+on/i,
    "loaded vaccine plan on count",
  );
  for (const code of expectedMatrixRows) {
    await expectVisibleTextIn(
      dialog,
      new RegExp(escapeRegExp(code), "i"),
      `loaded vaccine card ${code}`,
    );
  }
  await expectDomTextIn(
    dialog,
    /primary: 4w/i,
    "ET+TT schedule",
  );
  await expectDomTextIn(
    dialog,
    /repeat 6 months/i,
    "ET+TT adult revaccination schedule",
  );
  await expectVisibleTextIn(
    dialog,
    /Automatic safety rules/i,
    "read-only safety section",
  );
  await expectVisibleTextIn(
    dialog,
    /Live-to-live minimum gap is 28 days/i,
    "live-live safety rule",
  );
  await expectVisibleTextIn(
    dialog,
    /Pregnancy months 4 and 5 skip vaccination/i,
    "pregnancy safety rule",
  );
  await expectVisibleTextIn(
    dialog,
    /Mother vaccinated\/unknown category is ignored/i,
    "mother unknown hard ignore rule",
  );
  await expectVisibleTextIn(
    dialog,
    /Trusted history only means vaccines given by us/i,
    "trusted holding source rule",
  );

  const scopePicker = dialog
    .locator('select[aria-label="Scope · effective from"]')
    .first();
  const parkScopeCount = await scopePicker.locator('option[value^="park:"]').count();
  if (parkScopeCount > 0) {
    await dialog.getByRole("button", { name: /^One park$/i }).click();
    const parkScopeValue = await scopePicker.inputValue();
    if (!parkScopeValue.startsWith("park:")) {
      throw new Error(`One park segment did not select a park scope: ${parkScopeValue}`);
    }
    await dialog.getByRole("button", { name: /^Whole company$/i }).click();
    const companyScopeValue = await scopePicker.inputValue();
    if (companyScopeValue !== "tenant") {
      throw new Error(`Whole company segment did not restore company scope: ${companyScopeValue}`);
    }
  }

  const jsonPreview = dialog.locator(".cfgjson").first();
  await dialog.getByRole("button", { name: /^Goats$/i }).click();
  await expectVisibleTextIn(dialog, /5\s+on/i, "goat-scoped vaccine count");
  await expectDomTextIn(jsonPreview, /"species":\s*"goat"/i, "goat-scoped serialized rows");
  await dialog.getByRole("button", { name: /^Sheep$/i }).click();
  await expectVisibleTextIn(dialog, /6\s+on/i, "sheep-scoped vaccine count");
  await expectDomTextIn(jsonPreview, /"species":\s*"sheep"/i, "sheep-scoped serialized rows");
  await dialog.getByRole("button", { name: /^Goats \+ sheep$/i }).click();
  await expectVisibleTextIn(dialog, /7\s+on/i, "all-species vaccine count restored");

  const blueTongueCard = dialog.locator("button.card").filter({ hasText: "Blue Tongue" }).first();
  await blueTongueCard.getByRole("switch").click();
  await expectVisibleTextIn(dialog, /6\s+on/i, "vaccine toggle off count");
  await blueTongueCard.getByRole("switch").click();
  await expectVisibleTextIn(dialog, /7\s+on/i, "vaccine toggle on count");

  const goatPoxCard = dialog.locator("button.card").filter({ hasText: "Goat Pox" }).first();
  await goatPoxCard.click();
  await expectVisibleTextIn(dialog, /Timing for selected vaccine/i, "selected vaccine timing section");
  await expectDomTextIn(dialog, /20w/i, "Goat Pox live-live spacing effective week");
  await expectDomTextIn(dialog, /Revaccination/i, "revaccination fact");
  const proofInput = dialog.locator('input[aria-label="proof_policy"]').first();
  await proofInput.fill("shed,vial,dose,lot,qty,video");
  await expectVisibleTextIn(dialog, /video/i, "proof token chip");

  await dialog.getByRole("button", { name: /Preview Impact/i }).click();
  await expectVisibleTextIn(
    dialog,
    /Eligible goats/i,
    "config impact eligible goats",
  );
  await expectVisibleTextIn(
    dialog,
    /Obligations \/ cycle/i,
    "config impact obligations",
  );

  await dialog.getByRole("button", { name: /Save draft/i }).click();
  await expectVisibleTextIn(dialog, /draft saved/i, "config draft saved");
  await expectVisibleTextIn(
    dialog,
    /7 rows \/ .* schedule cells/i,
    "config matrix rows persisted",
  );

  const publish = dialog.getByRole("button", { name: /^Publish$/i }).last();
  await waitForEnabled(publish, "Config Publish after valid save");
  await publish.click();
  await expectVisibleTextIn(
    dialog,
    /published vaccination matrix/i,
    "config matrix rows published",
  );

  await goto(page, "/config?scope_mode=company&category=vaccination");
  const search = page.locator(".tsearch input").first();
  await expectAtLeastOne("Config search", search);
  await search.fill(protocolName);
  const row = page
    .locator("tbody tr")
    .filter({ hasText: protocolName })
    .first();
  await row.waitFor({ state: "visible", timeout: 15_000 });
  await expectVisibleTextIn(row, /published/i, "published config row");
  await row.click();
  const drawer = page.locator(".drawer.on").first();
  await drawer.waitFor({ state: "visible", timeout: 10_000 });
  await expectVisibleTextIn(
    drawer,
    new RegExp(escapeRegExp(protocolName), "i"),
    "config drawer protocol name",
  );
  await expectVisibleTextIn(
    drawer,
    /ET\+TT/i,
    "config drawer first vaccine code",
  );
  await expectVisibleTextIn(
    drawer,
    /required_proofs/i,
    "config drawer proof policy",
  );
  await expectVisibleTextIn(
    drawer,
    /schedule_note/i,
    "config drawer schedule note",
  );
  await expectVisibleTextIn(
    drawer,
    /vial_doses/i,
    "config drawer vial dose metadata",
  );
  await expectVisibleTextIn(
    drawer,
    /revaccination_interval_days/i,
    "config drawer revaccination metadata",
  );

  await expectVisibleTextIn(drawer, /Goat Pox/i, "config drawer Goat Pox matrix cell");
  await expectVisibleTextIn(drawer, /Blue Tongue/i, "config drawer Blue Tongue matrix cell");
}

async function openSopBuilder(page) {
  await page
    .locator(".sopstep")
    .first()
    .waitFor({ state: "visible", timeout: 5_000 })
    .catch(() => undefined);
  const pageBuilder = page.locator("main").first();
  if ((await pageBuilder.locator(".sopstep").count()) > 0) {
    return pageBuilder;
  }
  let dialog = page
    .getByRole("dialog", { name: /New SOP form builder/i })
    .first();
  if (
    (await dialog.count()) === 0 ||
    !(await dialog.isVisible().catch(() => false))
  ) {
    await page
      .getByRole("button", { name: /New SOP/i })
      .first()
      .click();
    dialog = page
      .getByRole("dialog", { name: /New SOP form builder/i })
      .first();
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
  await setConditional(
    dialog.locator(".sopstep").nth(3),
    "2",
    "block_if_empty",
  );
}

async function setConditional(step, showIf, onAnswer) {
  const selects = step.locator(".ss-cond select");
  if ((await selects.count()) < 2)
    throw new Error("conditional controls missing from SOP step");
  await selects.nth(0).selectOption(showIf);
  await selects.nth(1).selectOption(onAnswer);
}

async function saveDryRunPublish(dialog, savedPattern, label) {
  await dialog
    .getByRole("button", { name: /Save draft|Re-save draft/i })
    .click();
  await expectVisibleTextIn(dialog, savedPattern, `${label} save`);
  await expectVisibleTextIn(
    dialog,
    /form_dsl valid/i,
    `${label} backend validation`,
  );

  await dialog.getByRole("button", { name: /Dry-run/i }).click();
  await expectVisibleTextIn(dialog, /dry-run/i, `${label} dry-run`);
  await expectVisibleTextIn(dialog, /workflow:/i, `${label} dry-run workflow`);

  const publish = dialog.getByRole("button", { name: /Publish/i }).first();
  await expectAtLeastOne(`${label} publish`, publish);
  if (await publish.isDisabled()) {
    throw new Error(
      `${label} Publish stayed disabled: ${(await publish.getAttribute("title")) ?? ""}`,
    );
  }
  await publish.click();
  await Promise.race([
    dialog.waitFor({ state: "hidden", timeout: 20_000 }).catch(() => undefined),
    dialog
      .page()
      .waitForURL((url) => !url.searchParams.has("new") && !url.searchParams.has("compose"), {
        timeout: 20_000,
      })
      .catch(() => undefined),
  ]);
}

async function openSopDetail(page, sopName) {
  await goto(page, "/sops?scope_mode=company");
  const search = page.locator(".tsearch input").first();
  await expectAtLeastOne("SOP search", search);
  await search.fill(sopName);
  const card = page
    .locator("#sopCards .card")
    .filter({ hasText: sopName })
    .first();
  await card.waitFor({ state: "visible", timeout: 15_000 });
  await card.click();
  const dialog = page.locator('[role="dialog"]').first();
  await dialog.waitFor({ state: "visible", timeout: 10_000 });
  await expectVisibleTextIn(
    dialog,
    new RegExp(escapeRegExp(sopName), "i"),
    "SOP detail name",
  );
}

async function goto(page, path) {
  const response = await page.goto(`${appBaseUrl}${path}`, {
    waitUntil: "networkidle",
    timeout: 30_000,
  });
  if (response && !response.ok())
    throw new Error(`${path} returned HTTP ${response.status()}`);
  await assertHealthy(page, path);
}

async function assertHealthy(page, label) {
  await page
    .locator("main")
    .first()
    .waitFor({ state: "visible", timeout: 10_000 });
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
    if (text.includes(bad))
      throw new Error(`${label} contains unhealthy text: ${bad}`);
  }
}

async function expectVisibleTextIn(locator, pattern, label) {
  await locator
    .getByText(pattern)
    .first()
    .waitFor({ state: "visible", timeout: 15_000 })
    .catch(async (error) => {
      const text = await locator.innerText().catch(() => "");
      throw new Error(
        `${label} missing visible text ${pattern}; text=${text.replace(/\s+/g, " ").slice(0, 900)}`,
        { cause: error },
      );
    });
}

async function expectDomTextIn(locator, pattern, label) {
  const deadline = Date.now() + 15_000;
  let text = "";
  while (Date.now() < deadline) {
    text = await locator
      .evaluate((node) => node.textContent ?? "")
      .catch(() => "");
    if (pattern.test(text)) return;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(
    `${label} missing DOM text ${pattern}; text=${text.replace(/\s+/g, " ").slice(0, 900)}`,
  );
}

async function expectAtLeastOne(label, locator) {
  const count = await locator.count();
  if (count < 1)
    throw new Error(`${label} expected at least one element, got ${count}`);
}

async function expectInputValue(locator, expected, label) {
  await expectAtLeastOne(label, locator);
  const actual = await locator.first().inputValue();
  if (actual !== expected)
    throw new Error(`${label} expected value ${expected}, got ${actual}`);
}

async function waitForEnabled(locator, label) {
  const deadline = Date.now() + 15_000;
  let title = "";
  while (Date.now() < deadline) {
    if (!(await locator.isDisabled())) return;
    title = (await locator.getAttribute("title")) ?? "";
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`${label} stayed disabled: ${title}`);
}

async function selectOptionByText(select, text, label) {
  await expectAtLeastOne(label, select);
  const value = await select.evaluate((node, needle) => {
    const options = Array.from(node.options);
    const match = options.find((option) =>
      option.textContent?.includes(String(needle)),
    );
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
    lines.push(
      `| ${r.status} | ${r.name} | ${r.ms}ms | ${r.error ? r.error.replaceAll("|", "\\|") : ""} |`,
    );
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
  return (
    pathname === "/api/auth/firebase-config" || pathname === "/api/auth/session"
  );
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
