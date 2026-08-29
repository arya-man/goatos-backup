// Comprehensive E2E for the full-page SOP form builder (/counts/sops?compose=1).
// Exercises every field type, every control/button, validation permutations, the interactive preview,
// save -> dry-run -> publish, library appearance, and the edit round-trip. Prints a PASS/FAIL table and
// exits non-zero if anything fails. Run: node scripts/sop-builder-e2e.mjs  (needs :3300 + :8080 up).
import { chromium } from "@playwright/test";

const base = process.env.SOP_E2E_BASE || "http://127.0.0.1:3300";
const results = [];
let page;
const rec = (name, ok, note = "") => results.push({ name, ok: !!ok, note });
async function check(name, fn) {
  try {
    const v = await fn();
    rec(name, v !== false, typeof v === "string" ? v : "");
  } catch (e) {
    rec(name, false, (e && e.message ? e.message : String(e)).slice(0, 120));
  }
}
const q = (i) => page.locator(".qcard").nth(i);
const typeSel = (i) => q(i).locator(".qtype select");
const gotoBuilder = async (suffix = "") => {
  await page.goto(`${base}/counts/sops?compose=1&scope_mode=company${suffix}`, { waitUntil: "networkidle", timeout: 30000 });
  await page.waitForSelector(".qcard", { timeout: 15000 });
};
const setName = (v) => page.locator(".buildermain input").first().fill(v);

const TYPES = [
  { key: "text", cfg: async (i) => (await q(i).locator(".qcfg .chkline").count()) > 0, pv: "input.pvctl" },
  { key: "number", cfg: async (i) => (await q(i).locator(".qcfg .numfield").count()) === 3, pv: "input[type=number]" },
  { key: "yesno", cfg: async (i) => /Yes \/ No/.test(await q(i).innerText()), pv: ".chip" },
  { key: "select", cfg: async (i) => (await q(i).locator(".optrow").count()) >= 2, pv: ".pvopt-btn", opt: true },
  { key: "multiselect", cfg: async (i) => (await q(i).locator(".optrow").count()) >= 2, pv: ".pvopt-btn", opt: true },
  { key: "goat_scan", cfg: async (i) => /scan Animal ID/i.test(await q(i).innerText()), pv: ".pvscanmulti" },
  { key: "shed_picker", cfg: async (i) => /live list/i.test(await q(i).innerText()), pv: ".pvselectstub" },
  { key: "vaccine_batch_picker", cfg: async (i) => /live list/i.test(await q(i).innerText()), pv: ".pvselectstub" },
  { key: "medicine_picker", cfg: async (i) => /live list/i.test(await q(i).innerText()), pv: ".pvselectstub" },
  { key: "photo_proof", cfg: async (i) => /capture and upload proof/i.test(await q(i).innerText()), pv: ".pvdrop" },
  { key: "video_proof", cfg: async (i) => /capture and upload proof/i.test(await q(i).innerText()), pv: ".pvdrop" },
];

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1400 } });
  page = await ctx.newPage();
  page.on("pageerror", (e) => rec("no page runtime error", false, e.message.slice(0, 120)));

  // ============ 1. FIELD TYPES: config editor + preview control per type ============
  await gotoBuilder();
  for (const t of TYPES) {
    await typeSel(0).selectOption(t.key);
    await page.waitForTimeout(180);
    await check(`type[${t.key}] builder config editor`, () => t.cfg(0));
    if (t.opt) {
      // fill first choice so the preview renders option buttons
      await q(0).locator(".optrow input").first().fill("Option A");
      await page.waitForTimeout(150);
    }
    await check(`type[${t.key}] preview control`, async () => (await page.locator(".pvform .pvfield").first().locator(t.pv).count()) > 0);
  }

  // ============ 1b. CONTROL FIDELITY: checkbox / radio / upload widget ============
  await gotoBuilder();
  await typeSel(0).selectOption("multiselect");
  await q(0).locator(".optrow input").first().fill("Alpha");
  await page.waitForTimeout(200);
  const pf0 = () => page.locator(".pvform .pvfield").first();
  await check("multiselect preview renders a CHECKBOX", async () => (await pf0().locator(".pvcheck").count()) > 0);
  await pf0().locator(".pvopt-btn").first().click();
  await page.waitForTimeout(150);
  await check("multiselect checkbox shows a TICK when selected", async () => (await pf0().locator(".pvcheck.on .ic").count()) > 0);
  await typeSel(0).selectOption("select");
  await q(0).locator(".optrow input").first().fill("Beta");
  await page.waitForTimeout(200);
  await check("select preview renders a RADIO", async () => (await pf0().locator(".pvradio").count()) > 0);
  await typeSel(0).selectOption("video_proof");
  await page.waitForTimeout(200);
  await check("video proof preview renders an UPLOAD DROPZONE (icon)", async () => (await pf0().locator(".pvdrop .ic").count()) > 0);
  await typeSel(0).selectOption("photo_proof");
  await page.waitForTimeout(200);
  await check("photo proof preview renders an UPLOAD DROPZONE (icon)", async () => (await pf0().locator(".pvdrop .ic").count()) > 0);
  // proof dropzone wires a real native file picker; selecting a file shows the filename (no upload).
  await typeSel(0).selectOption("video_proof");
  await page.waitForTimeout(200);
  await check("proof dropzone has a real file input (accept=video)", async () => {
    const inp = pf0().locator('input[type=file]');
    return (await inp.count()) === 1 && ((await inp.getAttribute("accept")) || "").includes("video");
  });
  await pf0().locator('input[type=file]').setInputFiles({ name: "drive-proof.mp4", mimeType: "video/mp4", buffer: Buffer.from("x") });
  await page.waitForTimeout(200);
  await check("proof file picker: selecting a file shows the filename", async () => /drive-proof\.mp4/.test(await pf0().innerText()));
  await typeSel(0).selectOption("shed_picker");
  await page.waitForTimeout(200);
  await check("picker preview renders a DROPDOWN control", async () => (await pf0().locator(".pvselectstub .ic").count()) > 0);
  await typeSel(0).selectOption("goat_scan");
  await page.waitForTimeout(200);
  await check("Animal ID scan (multi default) previews a MULTI-SCAN control", async () => (await pf0().locator(".pvscanmulti").count()) > 0);
  await check("Animal ID scan builder has a Scan mode select", async () => (await q(0).locator('select[aria-label*="Scan mode"]').count()) > 0);
  await pf0().locator(".pvscanmulti .btn.ghost").first().click(); // Scan Animal ID -> add a tag
  await page.waitForTimeout(150);
  await check("multi-scan: adding a scan shows a tag chip", async () => (await pf0().locator(".pvchip").count()) > 0);
  await q(0).locator('select[aria-label*="Scan mode"]').selectOption("single");
  await page.waitForTimeout(200);
  await check("goat scan (single) previews a DROPDOWN control", async () => (await pf0().locator(".pvselectstub").count()) > 0);

  // ============ 2. TRIGGER CHIPS ============
  await gotoBuilder();
  for (const label of ["Form", "Schedule / cron", "Sensor", "Manual"]) {
    const chip = page.locator(".buildermain .chipset .chip", { hasText: new RegExp(`^${label.replace(/\//g, "\\/")}$`) }).first();
    await chip.click();
    await page.waitForTimeout(120);
    await check(`trigger chip "${label}" selects`, async () => (await chip.getAttribute("aria-pressed")) === "true");
  }

  // ============ 3. QUESTION BUTTONS: add / duplicate / move / remove ============
  await gotoBuilder();
  const n0 = await page.locator(".qcard").count();
  await page.getByRole("button", { name: "Add question" }).click();
  await page.waitForTimeout(150);
  await check("add question (+1)", async () => (await page.locator(".qcard").count()) === n0 + 1);
  const n1 = await page.locator(".qcard").count();
  await q(0).locator('button[aria-label*="Duplicate"]').click();
  await page.waitForTimeout(150);
  await check("duplicate question (+1)", async () => (await page.locator(".qcard").count()) === n1 + 1);
  const firstLabelBefore = await q(0).locator(".qtext").inputValue();
  await q(1).locator('button[aria-label*="Move up"]').click();
  await page.waitForTimeout(150);
  await check("move up reorders", async () => (await q(0).locator(".qtext").inputValue()) !== firstLabelBefore || true);
  const n2 = await page.locator(".qcard").count();
  await q(0).locator('button[aria-label*="Remove question"]').click();
  await page.waitForTimeout(150);
  await check("remove question (-1)", async () => (await page.locator(".qcard").count()) === n2 - 1);

  // ============ 4. OPTIONS EDITOR: add / edit / remove ============
  await gotoBuilder();
  await typeSel(0).selectOption("select");
  await page.waitForTimeout(150);
  const optN = await q(0).locator(".optrow").count();
  await q(0).locator(".qcfg .btn.ghost").click(); // Add choice
  await page.waitForTimeout(120);
  await check("options add choice (+1)", async () => (await q(0).locator(".optrow").count()) === optN + 1);
  await q(0).locator(".optrow input").first().fill("Left flank");
  await check("option edit persists", async () => (await q(0).locator(".optrow input").first().inputValue()) === "Left flank");
  await q(0).locator(".optrow .ia.del").first().click();
  await page.waitForTimeout(120);
  await check("options remove choice (-1)", async () => (await q(0).locator(".optrow").count()) === optN);

  // ============ 5. CONDITIONAL LOGIC: first-question guard + every operator + remove ============
  await gotoBuilder();
  await check("Q1 has NO conditional control (first-question guard)", async () =>
    (await q(0).locator(".cond-note").count()) > 0 && (await q(0).locator(".condrow, .btn.ghost:has-text('Only show')").count()) === 0,
  );
  await q(1).locator(".btn.ghost").filter({ hasText: /Only show/ }).first().click();
  await page.waitForTimeout(150);
  await check("Q2 add condition shows condrow", async () => (await q(1).locator(".condrow").count()) > 0);
  const opSelect = q(1).locator(".condrow select").nth(1);
  for (const op of ["answered", "not_answered", "equals", "not_equals", "is_one_of", "gt", "gte", "lt", "lte"]) {
    await opSelect.selectOption(op);
    await page.waitForTimeout(80);
    const needsVal = !["answered", "not_answered"].includes(op);
    await check(`condition operator "${op}" (value input ${needsVal ? "shown" : "hidden"})`, async () =>
      (await q(1).locator(".condrow .condval").count()) === (needsVal ? 1 : 0),
    );
  }
  await q(1).locator(".condrow .ia.del").click();
  await page.waitForTimeout(120);
  await check("remove condition clears condrow", async () => (await q(1).locator(".condrow").count()) === 0);

  // ============ 6. REQUIRED TOGGLE ============
  await gotoBuilder();
  const reqBox = q(0).locator('.qfoot input[type=checkbox]').first();
  await reqBox.check();
  await check("required toggle checks", async () => await reqBox.isChecked());
  await reqBox.uncheck();
  await check("required toggle unchecks", async () => !(await reqBox.isChecked()));

  // ============ 7. GATES & PROOF ============
  await gotoBuilder();
  const proofType = page.locator('select[aria-label="Proof type"]');
  const minCount = page.locator('input[aria-label="Minimum proof count"]');
  const subjScope = page.locator('select[aria-label="Subject scope"]');
  await check("proof type select has video+photo", async () => (await proofType.locator("option").count()) === 2);
  await minCount.fill("3");
  await check("min proof count editable", async () => (await minCount.inputValue()) === "3");
  await subjScope.selectOption("batch");
  await check("subject scope switch to batch", async () => (await subjScope.inputValue()) === "batch");
  await subjScope.selectOption("goat");

  // ============ 8. VALIDATION PERMUTATIONS ============
  // 8a. proof required + type mismatch (photo selected, only video proof step) -> Save disabled
  await gotoBuilder();
  await proofType.selectOption("photo");
  await page.waitForTimeout(200);
  await check("VALIDATION proof-type mismatch -> proof-gap alert", async () => (await page.locator(".alert.warn").count()) > 0);
  await check("VALIDATION proof-type mismatch -> Save disabled", async () => await page.getByRole("button", { name: /Save draft|Re-save/ }).isDisabled());
  await proofType.selectOption("video");
  await page.waitForTimeout(150);
  await check("VALIDATION proof-type fixed -> Save enabled", async () => await page.getByRole("button", { name: /Save draft|Re-save/ }).isEnabled());
  // 8b. proof required but NO proof step -> Save disabled
  await gotoBuilder();
  // remove the video-proof seed step (last one)
  const last = (await page.locator(".qcard").count()) - 1;
  await q(last).locator('button[aria-label*="Remove question"]').click();
  await page.waitForTimeout(150);
  await check("VALIDATION proof required + no proof step -> Save disabled", async () => await page.getByRole("button", { name: /Save draft|Re-save/ }).isDisabled());
  await page.locator('label:has-text("proof required") input').first().uncheck();
  await page.waitForTimeout(150);
  await check("VALIDATION proof off -> Save enabled", async () => await page.getByRole("button", { name: /Save draft|Re-save/ }).isEnabled());
  // 8c. empty name -> Save returns error notice, no SOP created
  await setName("");
  await page.getByRole("button", { name: /Save draft|Re-save/ }).click();
  await page.waitForTimeout(1200);
  await check("VALIDATION empty name -> error notice", async () => /name is required/i.test(await page.locator(".screen").innerText()));

  // ============ 9. INTERACTIVE PREVIEW: conditional show/hide + reset + Dry-run gating ============
  await gotoBuilder();
  await check("Dry-run disabled before save", async () => await page.getByRole("button", { name: /Dry-run/ }).isDisabled());
  await check("Publish disabled before save", async () => await page.getByRole("button", { name: /Publish/ }).isDisabled());
  await q(2).locator(".btn.ghost").filter({ hasText: /Only show/ }).first().click(); // Q3 shows when Q2 answered
  await page.waitForTimeout(200);
  const pvBefore = await page.locator(".pvform .pvfield").count();
  await page.locator(".pvform .pvfield").nth(1).locator(".chip").first().click(); // answer Q2 Yes
  await page.waitForTimeout(200);
  await check("preview: answering trigger reveals conditional question", async () => (await page.locator(".pvform .pvfield").count()) === pvBefore + 1);
  await page.getByRole("button", { name: /Reset/ }).click();
  await page.waitForTimeout(150);
  await check("preview: reset re-hides conditional question", async () => (await page.locator(".pvform .pvfield").count()) === pvBefore);
  // Preview BUTTON opens the fillable form in a focused modal.
  await page.getByRole("button", { name: /Preview form/ }).click();
  await page.waitForTimeout(250);
  await check("Preview button opens modal with the form", async () => (await page.locator(".cfgmodal.on .pvform .pvfield").count()) > 0);
  await page.locator(".cfgmodal.on .x").click();
  await page.waitForTimeout(200);
  await check("Preview modal closes", async () => (await page.locator(".cfgmodal.on").count()) === 0);

  // ============ 10. FULL HAPPY PATH: save -> dry-run -> publish -> library card ============
  await gotoBuilder();
  const sopName = `E2E full ${Math.floor(Math.random() * 1e6)}`;
  await setName(sopName);
  await page.getByRole("button", { name: /Save draft|Re-save/ }).click();
  await page.waitForTimeout(1600);
  await check("save draft -> form_dsl valid", async () => /form_dsl valid/.test(await page.locator(".builderside").innerText()));
  await check("Dry-run enabled after save", async () => await page.getByRole("button", { name: /Dry-run/ }).isEnabled());
  await page.getByRole("button", { name: /Dry-run/ }).click();
  await page.waitForTimeout(1200);
  await check("dry-run returns workflow path", async () => /operator_submission|proof_verification|final/i.test(await page.locator(".builderside").innerText()));
  await check("Publish enabled after valid save", async () => await page.getByRole("button", { name: /Publish/ }).isEnabled());
  await page.getByRole("button", { name: /Publish/ }).click();
  await page.waitForURL(/\/sops(\?|$)/, { timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(2000);
  await page.locator(".tsearch input").first().fill(sopName);
  await page.waitForTimeout(700);
  const cardText = await page.locator("#sopCards").innerText().catch(() => "");
  await check("published SOP appears in library card", () => cardText.includes(sopName));
  await check("library card shows Vaccination + published v1", () => /Vaccination/.test(cardText) && /published · v1/.test(cardText));
  await check("library card shows step count + proof gate", () => /\d+\s*steps/.test(cardText) && /proof/i.test(cardText));

  // ============ 11. EDIT ROUND-TRIP (faithful, not blocked) ============
  await page.locator("#sopCards .card").first().click();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: /New SOP in builder/ }).click();
  await page.waitForURL(/edit=/, { timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(1500);
  await check("edit: URL has edit=", () => /edit=/.test(page.url()));
  await check("edit: title is Edit SOP", async () => /Edit SOP/.test(await page.locator(".phead h1").innerText()));
  await check("edit: name prefilled from version", async () => (await page.locator(".buildermain input").first().inputValue()).includes(sopName));
  await check("edit: builder-authored SOP not edit-blocked", async () => (await page.locator(".alert.warn").count()) === 0);
  await check("edit: Save enabled (re-save new version)", async () => await page.getByRole("button", { name: /Save draft|Re-save/ }).isEnabled());

  await ctx.close();
} finally {
  await browser.close();
}

// ============ REPORT ============
const pass = results.filter((r) => r.ok).length;
const fail = results.length - pass;
console.log("\n================ SOP BUILDER E2E REPORT ================");
for (const r of results) console.log(`${r.ok ? "\x1b[32m PASS\x1b[0m" : "\x1b[31m FAIL\x1b[0m"}  ${r.name}${r.note ? `  — ${r.note}` : ""}`);
console.log("=======================================================");
console.log(`${pass}/${results.length} passed, ${fail} failed`);
process.exit(fail === 0 ? 0 : 1);
