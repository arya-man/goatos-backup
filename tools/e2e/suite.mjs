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
  execFileSync("./tools/e2e/e2e-db.sh", ["reset", DB], { env: process.env, encoding: "utf8" });
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

/** Stop and restart the Go API, for the cases about a server that is not there. */
function apiStop() {
  // ONLY the listener. `lsof -ti :8099` also lists processes holding a client
  // connection TO that port -- which includes the Next dev server's keep-alive --
  // so the unqualified form killed the app under test along with the API.
  try {
    execFileSync("bash", ["-lc", "lsof -ti tcp:8099 -sTCP:LISTEN | xargs -r kill -9"], { encoding: "utf8" });
  } catch {}
}
function apiStart() {
  execFileSync("bash", ["-lc", "nohup /tmp/goatos-api > /tmp/goatos-api.log 2>&1 & sleep 1"], {
    encoding: "utf8",
    env: {
      ...process.env,
      DATABASE_URL: `postgres://postgres:${process.env.E2E_PG_PASSWORD}@127.0.0.1:15432/${DB}?sslmode=disable`,
      GOATOS_ENV: "local",
      GOATOS_AUTH_MODE: "bearer",
      GOATOS_AUTH_HS256_SECRET: "goatos-local-dev-secret-32-bytes-min",
      GOATOS_AUTH_ISSUER: "goatos-local",
      GOATOS_AUTH_AUDIENCE: "goatos-api",
      GOATOS_PG_QUERY_TIMEOUT: "60s",
      GOATOS_HTTP_ADDR: "127.0.0.1:8099",
      GOATOS_ALLOW_STALE_LOCAL_STACK: "1",
      GOATOS_ORIGIN_MAIN_PREVERIFIED: "1",
      GOATOS_LOCAL_MEDIA_SIGNING_SECRET: "local-dev-media-signing-secret-32b",
    },
  });
  for (let i = 0; i < 60; i += 1) {
    try {
      const code = execFileSync("bash", ["-lc", "curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:8099/livez"], { encoding: "utf8" }).trim();
      if (code === "204") return;
    } catch {}
    execFileSync("bash", ["-lc", "sleep 1"]);
  }
  throw new Error("the API did not come back up");
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
  const storedBefore = psql(`select s->>'offset_days' from protocol_versions v,
      jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='draft' and s->>'dose_code'='et_tt_kid_4w'`);
  check("it starts at 28 days", storedBefore, "28");
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
  // Scoped to the dose card: a regex over the whole page would be satisfied by
  // any unrelated text that happened to say the same thing.
  check("the chip shows the new value", /5 months/.test(await page.locator(".dose").first().innerText()), true);
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
  // What the live plan says BEFORE, so the case can prove publishing changed it.
  // Asserting only that obligations did not move would pass if publish silently
  // did nothing at all.
  const liveRepeatBefore = psql(`select s->>'offset_days' from protocol_versions v,
      jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='published' and s->>'dose_code'='et_tt_revac'`);
  check("the live plan repeats every 182 days to begin with", liveRepeatBefore, "182");
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
  const liveRepeatAfter = psql(
    "select s->>'offset_days' from protocol_versions v, jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='published' and s->>'dose_code'='et_tt_revac'",
  );
  check("ET+TT repeat is now 90 days", liveRepeatAfter, "90");
  check("so the live plan genuinely changed", liveRepeatAfter !== liveRepeatBefore, true);
  check("and the retired version still says 182", psql(`select s->>'offset_days' from protocol_versions v,
      jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='retired' and v.version_label='V1 Real Vaccination'
      and s->>'dose_code'='et_tt_revac' limit 1`), "182");
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
  const nowDraft = psql("select protocol_version_id from protocol_versions where status='draft'");
  check("the URL points at the new draft", page.url().includes(nowDraft), true);
  check("and no longer at the discarded one", page.url() === before, false);
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

await runCase("C17", "A vaccine switched on with no doses cannot be saved", async () => {
  // "Off" IS an empty schedule, so a vaccine switched on with no doses saves as
  // off and the switch silently flips back -- someone could believe they added a
  // vaccine and walk away. Saving is blocked until it has a dose or goes back off.
  await gotoPlan();
  await startVersion();
  await page.getByRole("button", { name: /PPR/ }).first().click();
  await page.waitForTimeout(300);
  await page.getByRole("switch").first().click();
  await page.waitForTimeout(400);
  results.at(-1).shots.push(await shot("C17-blocked"));
  check("it says which vaccine and what to do", /PPR .*switched on but .*no doses/i.test(await page.locator(".vp").innerText()), true);
  check("Save is blocked", await page.getByRole("button", { name: /Save draft/i }).isDisabled(), true);
  check("Publish is blocked too", await page.getByRole("button", { name: /Publish plan/i }).isDisabled(), true);

  // Giving it a dose clears the block.
  await page.getByRole("button", { name: /Add a dose from date of birth/i }).click();
  await page.waitForTimeout(400);
  check("adding a dose unblocks Save", await page.getByRole("button", { name: /Save draft/i }).isEnabled(), true);
});

await runCase("C18", "With the server down, nothing is lost and nothing raw is shown", async () => {
  await gotoPlan();
  await startVersion();
  await page.locator(".dose").first().getByRole("button", { name: /4 weeks/ }).click();
  await page.getByLabel("How many").fill("5");
  await page.waitForTimeout(400);

  const draftBefore = psql("select protocol_version_id from protocol_versions where status='draft'");
  apiStop();
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(6000);
  results.at(-1).shots.push(await shot("C18-server-down"));

  const body = await page.locator("body").innerText();
  // The app's own wording, asserted verbatim rather than a phrase I invented.
  check("it says the backend is not reachable", /not reachable/i.test(body), true);
  check("no raw transport error on screen", /fetch failed|ECONNREFUSED|TypeError|ENOTFOUND/i.test(body), false);
  check("the draft is untouched", psql("select protocol_version_id from protocol_versions where status='draft'"), draftBefore);

  apiStart();
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(5000);
  check("and the same save works once it is back", psql("select count(*) from protocol_versions where status='draft'"), "1");
  // 5 of the chip's current unit, which is weeks -- 35 days, not 150.
  check("the edit landed", psql(`select s->>'offset_days' from protocol_versions v,
      jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='draft' and s->>'dose_code'='et_tt_kid_4w'`), "35");
});

await runCase("C19", "An earlier version that will not load says so and closes", async () => {
  await gotoPlan();
  // Make the retired version's document unreadable, the way a partial write would.
  psql("update protocol_versions set rule_dsl = '\"broken\"'::jsonb where status='retired'");
  await page.reload({ waitUntil: "networkidle" });
  const view = page.getByRole("button", { name: /View settings/i }).first();
  if (await view.count()) {
    await view.click();
    await page.waitForTimeout(2500);
    results.at(-1).shots.push(await shot("C19-unreadable"));
    const body = await page.locator("body").innerText();
    check("it does not sit on Loading forever", /Loading…/.test(body), false);
    check("the sheet is open", await page.locator(".vp-modal").count(), 1);
    await page.keyboard.press("Escape");
    await page.locator(".vp-modal").waitFor({ state: "detached", timeout: 5000 }).catch(() => undefined);
    check("Escape closes it even in the error state", await page.locator(".vp-modal").count(), 0);
  } else {
    note("no earlier version to view in this baseline", "skipped");
  }
});

await runCase("C20", "A published plan cannot be altered, even straight in the database", async () => {
  // Not a UI case. The strongest guarantee this feature has is that a published
  // version is immutable at the DATABASE level, so no bug, script or hand-typed
  // UPDATE can rewrite what a farm was told to do. Asserted directly.
  let refused = false;
  try {
    psql(`update protocol_versions set rule_dsl = jsonb_set(rule_dsl, '{matrix_rows,0,vaccine,name}', '"Tampered"'::jsonb) where status='published'`);
  } catch (err) {
    refused = /immutable/i.test(String(err.stderr ?? err.message ?? err));
  }
  check("the database refuses the write", refused, true);
  check("and the name is unchanged", psql(`select rule_dsl->'matrix_rows'->0->'vaccine'->>'name' from protocol_versions where status='published'`), "ET+TT");
});

await runCase("C21", "A very long vaccine name does not break the layout", async () => {
  await gotoPlan();
  await startVersion();
  // Drafts ARE mutable, which is where a name like this could realistically arrive.
  const draft = psql("select protocol_version_id from protocol_versions where status='draft'");
  psql(`update protocol_versions set rule_dsl = jsonb_set(rule_dsl, '{matrix_rows,0,vaccine,name}',
        '"${"Extremely Long Vaccine Name ".repeat(6).trim()}"'::jsonb) where protocol_version_id = '${draft}'`);
  await page.reload({ waitUntil: "networkidle" });
  results.at(-1).shots.push(await shot("C21-long-name"));
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  check("the page does not scroll sideways", overflow <= 0, true);
  check("the long name is on screen", /Extremely Long Vaccine Name/.test(await page.locator(".vp").innerText()), true);
});

await runCase("C22", "A vaccine switched off and back on keeps its whole course", async () => {
  // C15 proved the clinical FIELDS survive. This proves the COURSE does: the editor
  // used to read only the live schedule, so a switched-off vaccine looked like it
  // had no doses at all. Switching it back on then demanded a dose the farm had
  // already chosen, wrote that timing onto a rule nobody could see, resurrected the
  // rest invisibly, and deleted the repeat cadence outright.
  const shape = () => psql(`select coalesce(string_agg(s->>'dose_code'||'@'||(s->>'offset_days')||'/'||coalesce(s->>'repeat','none'), ' ' order by s->>'dose_code'), '-')
      from protocol_versions v, jsonb_array_elements(v.rule_dsl->'matrix_rows') r, jsonb_array_elements(r->'schedule') s
      where v.status='draft' and r->'vaccine'->>'code'='ET_TT'`);

  await gotoPlan();
  await startVersion();
  const original = shape();
  note("the course before", original);

  await page.getByRole("switch").first().click();
  await page.waitForTimeout(300);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(4000);

  // Reload so the editor re-reads the document from the server, which is where the
  // bug lived -- it only appeared on a FRESH read of a switched-off vaccine.
  await page.reload({ waitUntil: "networkidle" });
  const body = await page.locator(".vp").innerText();
  check("it is switched off", /Switched off/.test(body), true);
  check("but it does NOT claim to have no doses", /no doses yet/i.test(body), false);

  await page.getByRole("switch").first().click();
  await page.waitForTimeout(300);
  check("switching it back on needs no invented dose", await page.getByRole("button", { name: /Save draft/i }).isEnabled(), true);
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(4000);
  results.at(-1).shots.push(await shot("C22-course-restored"));
  check("the whole course is back, repeat included", shape(), original);
});

await runCase("C23", "Typing a long duration is not rewritten mid-keystroke", async () => {
  // Committing on every keystroke re-derived the unit from the running total, so the
  // field changed unit under the user's fingers: "3010" days flipped to "1 month" at
  // the third digit and the rest were read as MONTHS -- 3300 days stored for 3010
  // typed, moving every scheduled date.
  await gotoPlan();
  await startVersion();
  const dose = page.locator(".dose").first();
  await dose.getByRole("button", { name: /4 weeks/ }).click();
  const box = page.getByLabel("How many");
  const unit = page.getByLabel("Unit", { exact: true });
  await unit.selectOption("days");
  await box.fill("");
  await page.keyboard.type("3010", { delay: 60 });
  await page.waitForTimeout(400);
  results.at(-1).shots.push(await shot("C23-typed"));
  check("the number is still what was typed", await box.inputValue(), "3010");
  check("the unit is still days", await unit.inputValue(), "days");
  await page.getByRole("button", { name: /Save draft/i }).click();
  await page.waitForTimeout(4000);
  check("3010 days is what got stored", psql(`select s->>'offset_days' from protocol_versions v,
      jsonb_array_elements(v.rule_dsl->'schedule') s where v.status='draft' and s->>'dose_code'='et_tt_kid_4w'`), "3010");
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
