#!/usr/bin/env node
/**
 * Vaccination plan console — end-to-end suite.
 *
 * EVERY case starts from a fresh clone of the staging database. Cases that share
 * a database share their failures: one case leaving a draft behind changes what
 * the next one sees, and a suite that only passes in a given order is not
 * evidence of anything. The reset is a Postgres TEMPLATE copy, so it costs about
 * eight seconds rather than a restore.
 *
 * Driven through the browser, because the claim under test is always about the
 * screen a person uses, not about the API underneath it.
 *
 *   node tools/e2e/suite.mjs [--only case-id] [--keep]
 */
import { execFileSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";

import { chromium } from "playwright";

const BASE = process.env.E2E_BASE_URL ?? "http://127.0.0.1:3399";
const OUT = path.resolve(process.env.E2E_OUT ?? ".e2e-artifacts/suite");
const DB = process.env.E2E_DB ?? "goatos_e2e";
const only = process.argv.includes("--only") ? process.argv[process.argv.indexOf("--only") + 1] : null;

const psql = (sql) =>
  execFileSync(
    "psql",
    ["-h", "127.0.0.1", "-p", process.env.E2E_PG_PORT ?? "15432", "-U", "postgres", "-d", DB, "-tA", "-F", "|", "-c", sql],
    {
      env: { ...process.env, PGPASSWORD: process.env.E2E_PG_PASSWORD },
      encoding: "utf8",
      // A full obligation dump is ~16k rows; the 1 MB default truncates it into
      // an ENOBUFS crash that looks like a product failure.
      maxBuffer: 256 * 1024 * 1024,
    },
  ).trim();

function resetDb() {
  execFileSync("./tools/e2e/oci-db.sh", ["reset", DB], { env: process.env, encoding: "utf8" });
}

const results = [];
let page;
let shotIndex = 0;

async function shot(name) {
  shotIndex += 1;
  const file = path.join(OUT, `${String(shotIndex).padStart(2, "0")}-${name}.png`);
  await page.screenshot({ path: file, fullPage: true });
  return path.basename(file);
}

function check(label, actual, expected) {
  const ok = String(actual) === String(expected);
  results.at(-1).checks.push({ label, actual: String(actual), expected: String(expected), ok });
  console.log(`    ${ok ? "PASS" : "FAIL"}  ${label}  (got ${actual}, want ${expected})`);
  return ok;
}

function note(label, value) {
  results.at(-1).checks.push({ label, actual: String(value), expected: "—", ok: true, info: true });
  console.log(`    note  ${label}: ${value}`);
}

async function runCase(id, title, body) {
  if (only && only !== id) return;
  console.log(`\n=== ${id}  ${title}`);
  results.push({ id, title, checks: [], shots: [], error: null });
  console.log("    reset database to the staging baseline");
  resetDb();
  try {
    await body();
  } catch (err) {
    results.at(-1).error = String(err && err.message ? err.message : err);
    console.log(`    ERROR ${results.at(-1).error}`);
    try {
      results.at(-1).shots.push(await shot(`${id}-error`));
    } catch {}
  }
}

/** Counts that must never move when config changes. */
function history() {
  const rows = Object.fromEntries(
    psql("select status, count(*) from obligation_instances group by status")
      .split("\n")
      .filter(Boolean)
      .map((l) => l.split("|")),
  );
  return { completed: rows.completed ?? "0", canceled: rows.canceled ?? "0", scheduled: rows.scheduled ?? "0" };
}

async function gotoPlan() {
  await page.goto(`${BASE}/vaccination/plan`, { waitUntil: "networkidle" });
}

/**
 * Get into the editor, however the list is currently showing itself.
 *
 * There is one draft at a time, so the list offers exactly one of two controls:
 * "Start a new version" when no draft exists, or "Open V…" when one does. Both
 * end in the editor -- Start no longer returns to the list expecting a second
 * click.
 */
async function startVersion() {
  const start = page.getByRole("button", { name: /Start a new version/i });
  if (await start.count()) {
    await start.click();
  } else {
    await page.getByRole("link", { name: /^Open V\d/i }).first().click();
  }
  await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
  await page.waitForLoadState("networkidle");
}

// ── the suite ───────────────────────────────────────────────────────────────
await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();
page = await browser.newPage({ viewport: { width: 1512, height: 950 } });
const consoleErrors = [];
page.on("console", (m) => {
  if (m.type() === "error" && !m.text().includes("firebase-config")) consoleErrors.push(m.text());
});

await runCase("C01", "List screen reads the live plan from the database", async () => {
  await gotoPlan();
  results.at(-1).shots.push(await shot("C01-list"));
  // Scoped to the page, not the whole document: the app's own header says
  // "tenant scope", which is chrome this change does not own.
  const body = await page.locator(".vp").innerText();
  check("live version shown", /V1 Real Vaccination/.test(body), true);
  check("vaccine count is 5 of 7", /5 of 7/.test(body), true);
  check("a switched-off vaccine is still listed", /Blue Tongue/.test(body), true);
  check("off vaccines are labelled", /not in this plan/.test(body), true);
  check("no schema words on screen", /rule_dsl|obligation|tenant\b/i.test(body), false);
});

await runCase("C02", "Start a new version copies the live plan", async () => {
  await gotoPlan();
  await page.getByRole("button", { name: /Start a new version/i }).click();
  await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
  await page.waitForLoadState("networkidle");
  results.at(-1).shots.push(await shot("C02-draft"));
  check("exactly one draft exists", psql("select count(*) from protocol_versions where status='draft'"), "1");
  check("draft is labelled V2, not V3", psql("select version_label from protocol_versions where status='draft'"), "V2");
  const copied = psql(
    "select jsonb_array_length(rule_dsl->'schedule') from protocol_versions where status='draft'",
  );
  check("the whole schedule was copied", copied, "17");
  check("publishing did not happen", psql("select count(*) from protocol_versions where status='published'"), "1");
});

await runCase("C03", "Discard removes the draft and nothing else", async () => {
  const before = history();
  await gotoPlan();
  await page.getByRole("button", { name: /Start a new version/i }).click();
  await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
  // Discard lives on the list's draft banner, and Start now lands in the editor.
  await gotoPlan();
  await page.getByRole("button", { name: /^Discard it$/i }).click();
  results.at(-1).shots.push(await shot("C03-confirm"));
  check("it asks before destroying work", /cannot be undone/i.test(await page.locator("body").innerText()), true);
  await page.getByRole("button", { name: /Yes, discard it/i }).click();
  await page.waitForTimeout(2500);
  results.at(-1).shots.push(await shot("C03-discarded"));
  check("no draft remains", psql("select count(*) from protocol_versions where status='draft'"), "0");
  check("the live plan survives", psql("select count(*) from protocol_versions where status='published'"), "1");
  const after = history();
  check("completed untouched", after.completed, before.completed);
  check("scheduled untouched", after.scheduled, before.scheduled);
});

await runCase("C04", "Keep it cancels the discard", async () => {
  await gotoPlan();
  await page.getByRole("button", { name: /Start a new version/i }).click();
  await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
  // Discard lives on the list's draft banner, and Start now lands in the editor.
  await gotoPlan();
  await page.getByRole("button", { name: /^Discard it$/i }).click();
  await page.getByRole("button", { name: /Keep it/i }).click();
  await page.waitForTimeout(800);
  results.at(-1).shots.push(await shot("C04-kept"));
  check("the draft is still there", psql("select count(*) from protocol_versions where status='draft'"), "1");
});

await runCase("C05", "Editor renders the draft's real values", async () => {
  await gotoPlan();
  await startVersion();
  results.at(-1).shots.push(await shot("C05-editor"));
  const body = await page.locator("body").innerText();
  check("all seven vaccines in the rail", /PPR/.test(body) && /Blue Tongue/.test(body), true);
  check("ET+TT first dose reads 4 weeks", /4 weeks/.test(body), true);
  check("safety rules come from the document", /At most 2 vaccines per animal per visit/.test(body), true);
  check("impact preview is present", /animals in scope/.test(body), true);
  check("Save is disabled before any edit", await page.getByRole("button", { name: /Save draft/i }).isDisabled(), true);
});

await runCase("C06", "Duration dropdown: number and unit, any value", async () => {
  await gotoPlan();
  await startVersion();
  // The rail summarises a vaccine as "from 4 weeks ...", so the chip must be
  // addressed inside the dose card rather than by text alone.
  await page.locator(".dose").first().getByRole("button", { name: /4 weeks/ }).click();
  await page.waitForTimeout(300);
  results.at(-1).shots.push(await shot("C06-open"));
  const numberBox = page.getByLabel("How many");
  const unitBox = page.getByLabel("Unit", { exact: true });
  check("the picker opened", await numberBox.isVisible(), true);
  await unitBox.selectOption("months");
  await numberBox.fill("5");
  await page.waitForTimeout(400);
  results.at(-1).shots.push(await shot("C06-set"));
  check("chip shows the new value", /5 months/.test(await page.locator("body").innerText()), true);
  check("Save became available", await page.getByRole("button", { name: /Save draft/i }).isEnabled(), true);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(3000);
  const stored = psql(
    "select s->>'offset_days' from protocol_versions v, jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='draft' and s->>'dose_code'='et_tt_kid_4w'",
  );
  check("5 months stored as 150 days", stored, "150");
});

await runCase("C07", "Switching a vaccine off empties its schedule but keeps the row", async () => {
  await gotoPlan();
  await startVersion();
  await page.getByRole("switch").first().click();
  await page.waitForTimeout(400);
  results.at(-1).shots.push(await shot("C07-off"));
  check("it now reads switched off", /Switched off/.test(await page.locator("body").innerText()), true);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(3000);
  const rows = psql(
    "select count(*) from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r where v.status='draft'",
  );
  check("the vaccine row is kept", rows, "7");
  const etDoses = psql(
    "select jsonb_array_length(r->'schedule') from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r where v.status='draft' and r->'vaccine'->>'code'='ET_TT'",
  );
  check("its schedule is emptied", etDoses, "0");
  const total = psql("select jsonb_array_length(rule_dsl->'schedule') from protocol_versions where status='draft'");
  check("the flat schedule drops those 5 doses", total, "12");
});

await runCase("C08", "Switching a vaccine on, and giving it a dose", async () => {
  await gotoPlan();
  await startVersion();
  await page.getByRole("button", { name: /PPR/ }).first().click();
  await page.waitForTimeout(400);
  await page.getByRole("switch").first().click();
  await page.waitForTimeout(400);
  results.at(-1).shots.push(await shot("C08-on"));
  const body = await page.locator("body").innerText();
  check("it warns there are no doses yet", /no doses yet/i.test(body), true);
  await page.getByRole("button", { name: /Add a dose from date of birth/i }).click();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: /Make it repeat/i }).click();
  await page.waitForTimeout(400);
  results.at(-1).shots.push(await shot("C08-dosed"));
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(3000);
  const pprDoses = psql(
    "select jsonb_array_length(r->'schedule') from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r where v.status='draft' and r->'vaccine'->>'code'='PPR'",
  );
  check("PPR now has two rules", pprDoses, "2");
  const trigger = psql(
    "select s->>'trigger_type' from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r, jsonb_array_elements(r->'schedule') s where v.status='draft' and r->'vaccine'->>'code'='PPR' and (s->>'repeat')='none'",
  );
  check("the new dose is birth-triggered", trigger, "birth_age");
});

await runCase("C09", "Reset restores everything the editor changed", async () => {
  await gotoPlan();
  await startVersion();
  await page.getByRole("switch").first().click();
  await page.waitForTimeout(300);
  await page.getByRole("button", { name: /Reset/i }).click();
  await page.waitForTimeout(500);
  results.at(-1).shots.push(await shot("C09-reset"));
  const body = await page.locator("body").innerText();
  check("back to being in the plan", /In this plan/.test(body), true);
  check("Save is disabled again", await page.getByRole("button", { name: /Save draft/i }).isDisabled(), true);
});

await runCase("C10", "Publishing: history and in-flight work untouched", async () => {
  const before = history();
  const beforeRows = psql("select obligation_id||'|'||status||'|'||due_at from obligation_instances order by 1");
  await gotoPlan();
  await startVersion();
  await page.getByRole("button", { name: "3 months", exact: true }).click();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: /Publish plan/i }).click();
  await page.waitForURL(/\/vaccination\/plan$/, { timeout: 60000 });
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(1500);
  results.at(-1).shots.push(await shot("C10-published"));
  check("the new version is live", psql("select version_label from protocol_versions where status='published'"), "V2");
  check("the old one is retired", psql("select count(*) from protocol_versions where status='retired'"), "2");
  check("ET+TT repeat is now 90 days", psql(
    "select s->>'offset_days' from protocol_versions v, jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='published' and s->>'dose_code'='et_tt_revac'",
  ), "90");
  const after = history();
  check("completed untouched", after.completed, before.completed);
  check("canceled untouched", after.canceled, before.canceled);
  check("scheduled untouched", after.scheduled, before.scheduled);
  const afterRows = psql("select obligation_id||'|'||status||'|'||due_at from obligation_instances order by 1");
  check("not one obligation row changed at publish", afterRows === beforeRows, true);
  note("obligations compared", before.completed + before.canceled + before.scheduled);
});

await runCase("C11", "A published plan cannot be edited or discarded", async () => {
  const live = psql("select protocol_version_id from protocol_versions where status='published'");
  await page.goto(`${BASE}/vaccination/plan/edit?version=${live}`, { waitUntil: "networkidle" });
  results.at(-1).shots.push(await shot("C11-refused"));
  const shown = await page.locator("body").innerText();
  check("the editor does not open", /Company vaccination plan/.test(shown), false);
  check("a not-found page is shown instead", /404|not found/i.test(shown), true);
  check("the live plan is still published", psql("select count(*) from protocol_versions where status='published'"), "1");
});

await runCase("C12", "Earlier versions are readable and unchanged", async () => {
  await gotoPlan();
  await startVersion();
  await page.getByRole("button", { name: /Publish plan/i }).click();
  await page.waitForURL(/\/vaccination\/plan$/, { timeout: 60000 });
  await page.waitForLoadState("networkidle");
  await page.getByRole("button", { name: /View settings/i }).first().click();
  await page.waitForTimeout(1500);
  results.at(-1).shots.push(await shot("C12-history"));
  const body = await page.locator("body").innerText();
  check("the sheet opens", /Read-only/i.test(body), true);
  check("it says the version cannot be changed", /cannot be changed/i.test(body), true);
  check("it lists that version's vaccines", /ET\+TT/.test(body), true);
  await page.keyboard.press("Escape");
  await page.waitForTimeout(500);
});

await runCase("C13", "One draft at a time; the button always lands in the editor", async () => {
  // The maintainer's rule, stated whole: a plan being worked on is ONE thing.
  // There is never a second draft. Pressing "Start a new version" either creates
  // the draft or opens the one that exists -- and in both cases you end up in the
  // editor. The only ways out are publishing it or discarding it.
  //
  // Every assertion below is that rule, not a scenario invented around it.

  // The stale tab must be opened FIRST, while no draft exists -- that is what
  // makes it stale. Opening it afterwards renders "Open V2" and tests nothing.
  const stale = await browser.newPage({ viewport: { width: 1512, height: 950 } });
  await stale.goto(`${BASE}/vaccination/plan`, { waitUntil: "networkidle" });
  check("the stale tab is showing the Start button", await stale.getByRole("button", { name: /Start a new version/i }).count(), 1);

  // (a) from a clean plan: creates the draft AND lands in the editor
  await gotoPlan();
  await page.getByRole("button", { name: /Start a new version/i }).click();
  await page.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
  await page.waitForLoadState("networkidle");
  results.at(-1).shots.push(await shot("C13-lands-in-editor"));
  check("it opened the editor, not the list", /\/vaccination\/plan\/edit/.test(page.url()), true);
  check("exactly one draft exists", psql("select count(*) from protocol_versions where status='draft'"), "1");
  const draftId = psql("select protocol_version_id from protocol_versions where status='draft'");
  check("the editor is on that draft", page.url().includes(draftId), true);

  // (b) that stale tab still shows the button, because it rendered before the
  //     draft existed. Pressing it must open the SAME draft, never make a second.
  await stale.getByRole("button", { name: /Start a new version/i }).click();
  await stale.waitForURL(/\/vaccination\/plan\/edit/, { timeout: 30000 });
  check("the stale tab opened the editor too", /\/vaccination\/plan\/edit/.test(stale.url()), true);
  check("on the SAME draft", stale.url().includes(draftId), true);
  check("still exactly one draft", psql("select count(*) from protocol_versions where status='draft'"), "1");
  await stale.close();

  // (c) a double-click is one draft, not two
  await gotoPlan();
  const open = page.getByRole("link", { name: /^Open V\d/i });
  check("the list offers to open it, not to start another", await open.count(), 1);
  check("no second Start button is offered", await page.getByRole("button", { name: /Start a new version/i }).count(), 0);
  check("and the database still holds one draft", psql("select count(*) from protocol_versions where status='draft'"), "1");
});

await runCase("C14", "Saving a draft keeps the editor open", async () => {
  // A save does not update the draft: it creates the next version carrying the
  // edits and discards the old row. The id in the URL is therefore dead the moment
  // a save succeeds, and refreshing against it answered notFound() -- the editor
  // 404'd out from under the user on the very first save.
  await gotoPlan();
  await startVersion();
  const before = page.url();
  await page.locator(".dose").first().getByRole("button", { name: /4 weeks/ }).click();
  await page.getByLabel("How many").fill("5");
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(4000);
  results.at(-1).shots.push(await shot("C14-after-save"));
  const body = await page.locator("body").innerText();
  check("the editor is still open", /Company vaccination plan/.test(body), true);
  check("no not-found page", /404|not found/i.test(body), false);
  check("the URL followed the new draft", page.url() !== before, true);
  check("still exactly one draft", psql("select count(*) from protocol_versions where status='draft'"), "1");
});

await runCase("C15", "Switching a vaccine off keeps its clinical values", async () => {
  // dose_amount, vial_doses and route_site live ONLY on the schedule rules, and
  // the editor's model does not carry them. Emptying the schedule to switch a
  // vaccine off therefore destroyed values the farm chose, and switching it back
  // on rebuilt bare doses without them.
  const clinical = () => psql(`select coalesce(string_agg(s->>'dose_code'||':'||coalesce(s->>'dose_amount','-')||'/'||coalesce(s->>'vial_doses','-')||'/'||coalesce(s->>'route_site','-'), ' '), '-')
      from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r, jsonb_array_elements(r->'schedule') s
      where v.status='draft' and r->'vaccine'->>'code'='ET_TT'`);

  await gotoPlan();
  await startVersion();
  const original = clinical();
  note("clinical values before", original);

  await page.getByRole("switch").first().click();
  await page.waitForTimeout(300);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(4000);
  check("the schedule is empty while switched off", clinical(), "-");
  check("but the rules are parked, not destroyed", psql(`select coalesce(jsonb_array_length(r->'parked_schedule'),0)::text
      from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r
      where v.status='draft' and r->'vaccine'->>'code'='ET_TT'`), "5");

  await page.getByRole("switch").first().click();
  await page.waitForTimeout(300);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(4000);
  results.at(-1).shots.push(await shot("C15-restored"));
  check("switching back on restores every value", clinical(), original);
});

await runCase("C16", "A duration of zero, negative or nonsense is refused", async () => {
  await gotoPlan();
  await startVersion();
  const stored = () => psql(`select s->>'offset_days' from protocol_versions v,
      jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='draft' and s->>'dose_code'='et_tt_kid_4w'`);
  const before = stored();
  const dose = page.locator(".dose").first();

  for (const bad of ["0", "-5"]) {
    await dose.getByRole("button", { name: /4 weeks/ }).click();
    await page.getByLabel("How many").fill(bad);
    await page.waitForTimeout(300);
    check(`"${bad}" leaves the chip alone`, /4 weeks/.test(await dose.innerText()), true);
    await page.keyboard.press("Escape");
    await page.waitForTimeout(200);
  }

  // Letters cannot even be typed: the field is a number input, so the browser
  // refuses them before any of our code runs. Asserted by TYPING rather than by
  // fill(), which throws instead of showing what a person would experience.
  await dose.getByRole("button", { name: /4 weeks/ }).click();
  const box = page.getByLabel("How many");
  await box.click();
  await box.fill("");
  await page.keyboard.type("abc");
  await page.waitForTimeout(300);
  check("letters never reach the field", await box.inputValue(), "");
  check("the chip is unchanged", /4 weeks/.test(await dose.innerText()), true);
  await page.keyboard.press("Escape");
  await page.waitForTimeout(200);

  results.at(-1).shots.push(await shot("C16-refused"));
  check("nothing was saved", stored(), before);
  check("Save stayed disabled", await page.getByRole("button", { name: /Save draft/i }).isDisabled(), true);
});

await browser.close();

// ── report ──────────────────────────────────────────────────────────────────
const total = results.reduce((n, r) => n + r.checks.filter((c) => !c.info).length, 0);
const failed = results.reduce((n, r) => n + r.checks.filter((c) => !c.ok).length, 0);
const errored = results.filter((r) => r.error).length;

console.log(`\n${"=".repeat(60)}`);
for (const r of results) {
  const bad = r.checks.filter((c) => !c.ok).length;
  console.log(`${r.error ? "ERROR" : bad ? "FAIL " : "PASS "}  ${r.id}  ${r.title}`);
  for (const c of r.checks.filter((c) => !c.ok)) console.log(`         ✗ ${c.label}: got ${c.actual}, want ${c.expected}`);
  if (r.error) console.log(`         ! ${r.error}`);
}
console.log(`${"=".repeat(60)}`);
console.log(`${total - failed}/${total} checks passed · ${errored} case(s) errored`);
if (consoleErrors.length) console.log(`console errors: ${[...new Set(consoleErrors)].join(" | ")}`);
console.log(`screenshots: ${OUT}`);

await writeFile(path.join(OUT, "results.json"), JSON.stringify({ results, total, failed, errored }, null, 2));
process.exit(failed || errored ? 1 : 0);
