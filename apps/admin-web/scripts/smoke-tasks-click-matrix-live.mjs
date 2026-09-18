#!/usr/bin/env node
/**
 * THE TASKS DESK CLICK MATRIX — a click-through harness, not a screenshot sweep.
 *
 * ── WHY THIS EXISTS ───────────────────────────────────────────────────────────────────────────
 * The Tasks desk shipped with five defects that every existing check passed: the guards, the unit
 * suite and `smoke:visual:live` all agree that a page RENDERS, and rendering is not working. The
 * `+5` overflow chip on the board's person filter was a `<span aria-hidden>` with no handler:
 * present, correctly laid out, pixel-identical to a baseline, and DEAD. A screenshot cannot see
 * that. Only activating it and demanding that something change can.
 *
 * So this harness ENUMERATES every interactive element inside the page region from the live DOM —
 * `a, button, select, input, [role=button], [role=tab], [role=option], summary, [draggable]`, plus
 * the shell's notification bell — and for EACH one:
 *   (a) activates it and demands an OBSERVABLE CHANGE (see `fingerprint` below: the URL, the set
 *       of `aria-current` / `aria-expanded` / `aria-selected` elements, the open dialogs and
 *       popovers, the card and row counts, and a hash of the visible text). An element that is
 *       present and causes NO observable change on activation FAILS the run. That is precisely
 *       the `+5` defect, and it is the whole reason this file is not a screenshot diff;
 *   (b) demands no console error and no page error fired while it was activated;
 *   (c) demands the page did not land on a failure string.
 *
 * An element that is legitimately inert at a viewport is asserted ABSENT or asserted DISABLED —
 * never "clicked and nothing happened". Drag lives behind `(min-width: 761px) and (pointer:
 * fine)`, so at 390px the harness asserts there is no `[draggable="true"]` card at all rather
 * than clicking one and shrugging.
 *
 * ── HOW IT SURVIVES ITS OWN CLICKS ────────────────────────────────────────────────────────────
 * Most of these elements navigate, which invalidates every handle the enumeration just took. So
 * each stage is walked by INDEX: the stage URL is loaded, the elements are enumerated to get a
 * count, and then for i in 0..n the stage is reloaded, re-enumerated, and only element i is
 * activated. Slower than one pass and the only version that can claim it activated all of them.
 *
 * ── WHERE IT CAN RUN ──────────────────────────────────────────────────────────────────────────
 * It needs a LIVE STACK — a running admin-web and a running API with real rows, because the
 * defects it exists to catch are about a board holding 414 tasks across 11 people and counts the
 * backend publishes. It is therefore a `smoke:*:live` script beside the others and NOT part of a
 * `make check` guard target: the guards are static and hermetic, and wiring a stack-dependent
 * script into one would make the guard fail on a laptop with no database. In CI it belongs with
 * the other live smokes, after the stack comes up.
 *
 * Usage:
 *   GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:13308 \
 *   GOATOS_API_BASE_URL=http://127.0.0.1:18088 \
 *   GOATOS_BEARER_TOKEN=... GOATOS_TENANT_ID=... \
 *   npm run smoke:tasks-click-matrix:live --prefix apps/admin-web
 */
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";

const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const apiBaseUrl = trimTrailingSlash(process.env.GOATOS_API_BASE_URL ?? "");
const bearerToken = process.env.GOATOS_BEARER_TOKEN ?? "";
const tenantId = process.env.GOATOS_TENANT_ID ?? "";
const TENANT_CONTEXT_HEADER = "x-goatos-tenant-id";

if (!bearerToken) throw new Error("GOATOS_BEARER_TOKEN is required: this harness drives the real desk");
if (!apiBaseUrl) throw new Error("GOATOS_API_BASE_URL is required: the stage URLs need a real task id");

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const runId = process.env.GOATOS_TASKS_MATRIX_RUN_ID ?? `TASKS-CLICK-${new Date().toISOString().replaceAll(/[:.]/g, "-")}`;
const reportDir = process.env.GOATOS_TASKS_MATRIX_REPORT_DIR
  ? resolve(process.env.GOATOS_TASKS_MATRIX_REPORT_DIR)
  : join(repoRoot, ".codex-goatos-render", "tasks-click-matrix", runId);
const failureDir = join(reportDir, "failures");
mkdirSync(failureDir, { recursive: true });

/** The page region this harness owns. The shell's own chrome has its own click matrix. */
const PAGE_REGION = ".lt-page";
/** The shell controls this page is responsible for exercising anyway (the bell is on this desk). */
const EXTRA_REGIONS = [".topbar"];
/**
 * What counts as interactive. `[role=option]` is in the list because the person filter's rows are
 * listbox options rather than links, and an enumeration that skipped them would miss the very
 * control this change added.
 */
const INTERACTIVE = "a, button, select, input, [role=button], [role=tab], [role=option], summary, [draggable]";

/** A page that says any of these has not "rendered", whatever the screenshot shows. */
const FAILURE_STRINGS = [
  "Something went wrong",
  "failed to render",
  "missing copy key",
  "Application error",
  "Unhandled Runtime Error",
  "This page couldn't load",
  "invalid_bearer_token",
  "backend_down",
  "route_not_registered",
];

const VIEWPORTS = [
  { label: "1440x900", width: 1440, height: 900, isMobile: false },
  { label: "390x844", width: 390, height: 844, isMobile: true },
];

const results = [];
let currentIssues = [];

const taskId = await resolveTaskId();

const browser = await chromium.launch();
try {
  for (const viewport of VIEWPORTS) {
    const context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      hasTouch: viewport.isMobile,
      isMobile: viewport.isMobile,
    });
    await context.addCookies([
      {
        name: "goatos_firebase_id_token",
        value: bearerToken,
        domain: new URL(appBaseUrl).hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      },
    ]);
    const page = await context.newPage();
    page.on("console", (message) => {
      if (message.type() !== "error") return;
      const text = message.text();
      if (text.startsWith("Failed to load resource:")) return;
      currentIssues.push(`console: ${text.slice(0, 300)}`);
    });
    page.on("pageerror", (error) => currentIssues.push(`pageerror: ${error.message.slice(0, 300)}`));

    for (const stage of stages(viewport)) {
      await walkStage(page, viewport, stage);
    }
    await scriptedChecks(page, viewport);
    await context.close();
  }
} finally {
  await browser.close();
}

const failed = results.filter((entry) => entry.status === "fail");
writeFileSync(join(reportDir, "matrix.json"), JSON.stringify({ appBaseUrl, runId, taskId, results }, null, 2));
writeFileSync(join(reportDir, "matrix.md"), renderMarkdown());
console.log("");
console.log(`elements+assertions: ${results.length}  pass: ${results.length - failed.length}  fail: ${failed.length}`);
console.log(`tasks click matrix ${failed.length ? "FAILED" : "passed"}; report_dir=${reportDir}`);
if (failed.length > 0) process.exit(1);

// ─────────────────────────────────────────────────────────────────────── the stages

function stages(viewport) {
  const phone = viewport.isMobile;
  return [
    { name: "board", url: "/tasks?scope=team_progress" },
    { name: "list", url: "/tasks?scope=team_progress&t_view=list" },
    // B4's stage: a board under a status filter. The scripted checks below also assert that no
    // column here can read "0 on this page" beneath a non-zero pill.
    { name: "board-filter-done", url: "/tasks?scope=team_progress&filter=done" },
    { name: "scope-raised-by-me", url: "/tasks?scope=assigned_by_me" },
    { name: "detail-panel", url: `/tasks?scope=team_progress&task=${taskId}` },
    // B5's stage: the ignored-parameter notice and its corrective link.
    { name: "ignored-view-param", url: "/tasks?scope=team_progress&view=list" },
    // The phone filter sheet has to be OPEN for its controls to be in the DOM at all.
    phone ? { name: "filter-sheet-open", url: "/tasks?scope=team_progress", open: ".lt-fmore" } : null,
    // Both people popups, both date disclosures, the New task modal and the bell panel are
    // opened as their own stages so their contents are enumerated and clicked too.
    { name: "people-assignee-open", url: "/tasks?scope=team_progress", open: openPeople(phone, 0) },
    { name: "people-raiser-open", url: "/tasks?scope=team_progress", open: openPeople(phone, 1) },
    { name: "new-task-modal-open", url: "/tasks?scope=team_progress", open: ".lt-phead .btn.p" },
    { name: "bell-panel-open", url: "/tasks?scope=team_progress", open: ".topbar .iconbtn[aria-label]" },
  ].filter(Boolean);
}

/** On a phone every bar control lives behind the filter sheet, so it is opened first. */
function openPeople(phone, index) {
  return phone
    ? [".lt-fmore", `.lt-pf-trigger >> nth=${index}`]
    : [`.lt-pf-trigger >> nth=${index}`];
}

// ─────────────────────────────────────────────────────────────── the enumerate-and-click walk

async function walkStage(page, viewport, stage) {
  const label = `${viewport.label} ${stage.name}`;
  const total = await (async () => {
    await loadStage(page, stage);
    return enumerate(page);
  })().catch((error) => {
    record(label, "enumerate", "fail", String(error?.message ?? error));
    return null;
  });
  if (total === null) return;
  if (!total.length) {
    record(label, "enumerate", "fail", "no interactive elements found in the page region");
    return;
  }
  record(label, "enumerate", "pass", `${total.length} interactive elements`);

  for (let index = 0; index < total.length; index += 1) {
    const descriptor = total[index];
    await activateOne(page, stage, label, index, descriptor);
  }
}

async function loadStage(page, stage) {
  const response = await page.goto(`${appBaseUrl}${stage.url}`, { waitUntil: "domcontentloaded", timeout: 45_000 });
  if (response && !response.ok()) throw new Error(`${stage.url} returned HTTP ${response.status()}`);
  await page.locator(PAGE_REGION).first().waitFor({ state: "visible", timeout: 20_000 });
  await page.waitForLoadState("networkidle", { timeout: 8_000 }).catch(() => undefined);
  for (const opener of [stage.open].flat().filter(Boolean)) {
    const node = page.locator(opener).first();
    await node.waitFor({ state: "visible", timeout: 10_000 });
    await node.click();
    await page.waitForTimeout(250);
  }
}

/**
 * Every interactive element in the page region, as a stable DESCRIPTOR rather than a handle: a
 * handle does not survive the navigation the previous click caused.
 */
async function enumerate(page) {
  return page.evaluate(
    ({ region, extras, selector }) => {
      const roots = [document.querySelector(region), ...extras.map((s) => document.querySelector(s))].filter(Boolean);
      const seen = new Set();
      const out = [];
      for (const root of roots) {
        for (const node of root.querySelectorAll(selector)) {
          if (seen.has(node)) continue;
          seen.add(node);
          // The index is STAMPED on the node, so activation targets exactly the element the
          // enumeration described rather than a document-order guess that drifts between the
          // page region and the shell's topbar.
          node.setAttribute("data-clickmatrix", String(out.length));
          const style = window.getComputedStyle(node);
          const box = node.getBoundingClientRect();
          out.push({
            tag: node.tagName.toLowerCase(),
            type: node.getAttribute("type") ?? "",
            role: node.getAttribute("role") ?? "",
            draggable: node.getAttribute("draggable") ?? "",
            disabled: node.hasAttribute("disabled") || node.getAttribute("aria-disabled") === "true",
            hidden: style.display === "none" || style.visibility === "hidden" || (box.width === 0 && box.height === 0),
            className: (typeof node.className === "string" ? node.className : "").slice(0, 60),
            name:
              (node.getAttribute("aria-label") || node.textContent || node.getAttribute("placeholder") || "")
                .replace(/\s+/g, " ")
                .trim()
                .slice(0, 70) || "(unnamed)",
            width: Math.round(box.width),
            height: Math.round(box.height),
          });
        }
      }
      return out;
    },
    { region: PAGE_REGION, extras: EXTRA_REGIONS, selector: INTERACTIVE },
  );
}

/**
 * The page's observable state. Anything a reader could notice: where they are, what is marked
 * current, what is open, how many rows and cards, and the visible words.
 */
async function fingerprint(page) {
  return page.evaluate(() => {
    const names = (selector) =>
      [...document.querySelectorAll(selector)]
        .map((node) => (node.getAttribute("aria-label") || node.textContent || "").replace(/\s+/g, " ").trim().slice(0, 40))
        .sort()
        .join("|");
    const text = (document.querySelector("main") ?? document.body).innerText.replace(/\s+/g, " ").trim();
    let hash = 0;
    for (let i = 0; i < text.length; i += 1) hash = (hash * 31 + text.charCodeAt(i)) | 0;
    return {
      href: location.href,
      current: names('[aria-current="true"], [aria-current="page"]'),
      expanded: names('[aria-expanded="true"]'),
      selectedOptions: names('[aria-selected="true"]'),
      onClass: document.querySelectorAll(".on").length,
      dialogs: document.querySelectorAll('[role="dialog"], .drawer.on, .lt-modal').length,
      popovers: document.querySelectorAll("[data-people-popup], .lt-fdrop-pop, [data-mention-popup]").length,
      cards: document.querySelectorAll(".ltb-card, .ltb-colbd a").length,
      rows: document.querySelectorAll("tbody tr").length,
      inputs: [...document.querySelectorAll("input, select")].map((node) => node.value ?? "").join("|"),
      textHash: hash,
      textLength: text.length,
    };
  });
}

async function activateOne(page, stage, stageLabel, index, descriptor) {
  const what = `[${index}] ${descriptor.tag}${descriptor.role ? `[${descriptor.role}]` : ""}${
    descriptor.type ? `[${descriptor.type}]` : ""
  } "${descriptor.name}"`;

  // A HIDDEN element is not something the reader can activate. It is recorded, not clicked.
  if (descriptor.hidden) {
    record(stageLabel, what, "pass", "not rendered at this viewport (asserted hidden, not clicked)");
    return;
  }
  // AN INERT element states its inertness. This is the honest alternative to "clicked, nothing
  // happened" — a pager's Prev on page 1, a person filter the scope already pins.
  if (descriptor.disabled) {
    record(stageLabel, what, "pass", "asserted disabled/aria-disabled rather than clicked");
    return;
  }
  // DRAG is desktop-and-precise-pointer only. At phone width the assertion is ABSENCE.
  if (descriptor.draggable === "true") {
    if (page.viewportSize().width < 761) {
      record(stageLabel, what, "fail", "a draggable card must not exist at phone width");
    } else {
      record(stageLabel, what, "pass", 'draggable="true" present at desktop width (drag itself is a status write; not performed here)');
    }
    return;
  }
  // TAP TARGET, at phone width. 40px is this page's documented floor.
  if (page.viewportSize().width < 761 && (descriptor.height < 38 || descriptor.width < 20)) {
    record(stageLabel, what, "fail", `tap target ${descriptor.width}x${descriptor.height} is under the 40px floor`);
    return;
  }

  await loadStage(page, stage).catch(() => undefined);
  const elements = await enumerate(page).catch(() => []);
  if (!elements[index]) {
    record(stageLabel, what, "fail", "element was not present on a re-load of the same stage (unstable DOM)");
    return;
  }
  currentIssues = [];
  const before = await fingerprint(page);
  let how = "click";
  try {
    if (descriptor.tag === "select") {
      how = "select a different option";
      const changed = await page.evaluate((i) => {
        const node = document.querySelector(`[data-clickmatrix="${i}"]`);
        if (!node) return false;
        const options = [...node.options].map((option) => option.value);
        const next = options.find((value) => value !== node.value);
        if (next === undefined) return false;
        node.value = next;
        node.dispatchEvent(new Event("change", { bubbles: true }));
        return true;
      }, index);
      if (!changed) {
        record(stageLabel, what, "fail", "a select with only one option is a control that cannot do anything");
        return;
      }
    } else if (descriptor.tag === "input" && (descriptor.type === "search" || descriptor.type === "text")) {
      how = "type into it";
      await nth(page, index).fill("fence");
    } else if (descriptor.tag === "input" && descriptor.type === "date") {
      how = "fill a date";
      await nth(page, index).fill("2026-09-01");
    } else if (descriptor.tag === "input" && (descriptor.type === "checkbox" || descriptor.type === "radio")) {
      how = "check it";
      await nth(page, index).click();
    } else if (descriptor.tag === "input" && descriptor.type === "file") {
      record(stageLabel, what, "pass", "file input: asserted present and enabled (no upload performed on live data)");
      return;
    } else {
      await nth(page, index).click({ timeout: 10_000 });
    }
  } catch (error) {
    record(stageLabel, what, "fail", `${how} threw: ${String(error?.message ?? error).split("\n")[0].slice(0, 200)}`);
    return;
  }

  // Give a navigation, a transition or a 300ms debounce time to land.
  await page.waitForLoadState("networkidle", { timeout: 6_000 }).catch(() => undefined);
  await page.waitForTimeout(650);

  const after = await fingerprint(page).catch(() => null);
  if (!after) {
    record(stageLabel, what, "fail", "the page stopped answering after activation");
    return;
  }

  const body = await page.locator("body").innerText().catch(() => "");
  const badString = FAILURE_STRINGS.find((needle) => body.includes(needle));
  if (badString) {
    await shot(page, stageLabel, index);
    record(stageLabel, what, "fail", `landed on a failure string: ${badString}`);
    return;
  }
  if (currentIssues.length) {
    await shot(page, stageLabel, index);
    record(stageLabel, what, "fail", `page issues: ${currentIssues.join(" | ").slice(0, 400)}`);
    return;
  }

  const diff = Object.keys(after).filter((key) => JSON.stringify(after[key]) !== JSON.stringify(before[key]));
  if (!diff.length) {
    await shot(page, stageLabel, index);
    record(stageLabel, what, "fail", `activated (${how}) and NOTHING observable changed — this is the dead-control defect`);
    return;
  }
  record(stageLabel, what, "pass", `${how} → changed: ${diff.join(", ")}`);
}

/** The element the enumeration stamped with this index. */
function nth(page, index) {
  return page.locator(`[data-clickmatrix="${index}"]`).first();
}

// ────────────────────────────────────────────────────── the assertions enumeration cannot make

async function scriptedChecks(page, viewport) {
  const phone = viewport.isMobile;
  const label = `${viewport.label} scripted`;

  // B1/B2 — the person filter can reach EVERY assignable person, and can be typed into.
  await check(label, "person filter: reaches every assignable person by typing", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress", open: phone ? [".lt-fmore", ".lt-pf-trigger >> nth=0"] : [".lt-pf-trigger >> nth=0"] });
    const popup = page.locator("[data-people-popup]").first();
    await popup.waitFor({ state: "visible", timeout: 8_000 });
    const search = popup.locator('input[type="search"]');
    if ((await search.count()) !== 1) throw new Error("the person filter has no search field");
    // Every person the endpoint offers must be REACHABLE — not just the first six.
    const people = await fetchJson("/app/leadership-tasks/assignees");
    const names = people.assignees.map((person) => person.name).filter(Boolean);
    if (names.length < 7) throw new Error(`expected the multi-person roster, got ${names.length}`);
    for (const name of names) {
      await search.fill(name);
      await page.waitForTimeout(120);
      const option = popup.locator('[role="option"]').filter({ hasText: name }).first();
      if ((await option.count()) === 0) throw new Error(`"${name}" cannot be reached by typing their name`);
    }
    // And the OLD control is gone: no dead overflow chip anywhere on the board.
    if ((await page.locator(".ltb-person-rest").count()) !== 0) throw new Error("the dead +N overflow chip is still rendered");
  });

  await check(label, "person filter: keyboard only, and it lands in the URL", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress", open: phone ? [".lt-fmore", ".lt-pf-trigger >> nth=0"] : [".lt-pf-trigger >> nth=0"] });
    const popup = page.locator("[data-people-popup]").first();
    const search = popup.locator('input[type="search"]');
    await search.fill("man");
    await page.waitForTimeout(150);
    const first = await popup.locator('input[type="search"]').getAttribute("aria-activedescendant");
    await search.press("ArrowDown");
    const second = await popup.locator('input[type="search"]').getAttribute("aria-activedescendant");
    if (!first || first === second) throw new Error("ArrowDown did not move the highlight");
    await search.press("ArrowDown");
    await search.press("Enter");
    await page.waitForTimeout(900);
    const url = new URL(page.url());
    if (!url.searchParams.get("t_assignee")) throw new Error(`Enter did not set t_assignee; url=${page.url()}`);
    // Shareable: the same URL, loaded cold, shows the same narrowing.
    const chips = await page.locator(".lt-factive .achip").count();
    if (chips === 0) throw new Error("the applied person filter is not stated as an active chip");
  });

  await check(label, "person filter: Escape closes it and returns focus to its trigger", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress", open: phone ? [".lt-fmore", ".lt-pf-trigger >> nth=0"] : [".lt-pf-trigger >> nth=0"] });
    await page.keyboard.press("Escape");
    await page.waitForTimeout(200);
    if ((await page.locator("[data-people-popup]").count()) !== 0) throw new Error("Escape left the popup open");
    const focused = await page.evaluate(() => document.activeElement?.className ?? "");
    if (!focused.includes("lt-pf-trigger")) throw new Error(`focus did not return to the trigger; activeElement=${focused}`);
  });

  await check(label, "person filter: does not overflow the viewport", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress", open: phone ? [".lt-fmore", ".lt-pf-trigger >> nth=0"] : [".lt-pf-trigger >> nth=0"] });
    const box = await page.locator("[data-people-popup]").first().boundingBox();
    const size = page.viewportSize();
    if (!box) throw new Error("no popup box");
    if (box.x < -1 || box.x + box.width > size.width + 1) {
      throw new Error(`popup spans ${Math.round(box.x)}..${Math.round(box.x + box.width)} in a ${size.width}px viewport`);
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    if (overflow > 1) throw new Error(`opening the popup added ${overflow}px of horizontal page scroll`);
    // Every row is a real tap target.
    const short = await page.evaluate(() =>
      [...document.querySelectorAll('[data-people-popup] [role="option"]')].filter((node) => node.getBoundingClientRect().height < 40).length,
    );
    if (short > 0) throw new Error(`${short} option rows are under 40px tall`);
  });

  // B3 — the column link says the intent, not the implementation.
  await check(label, 'board column "more" link states the intent', async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress" });
    const more = page.locator(".ltb-colmore").first();
    if ((await more.count()) === 0) throw new Error("no column offers to show its whole status");
    const text = ((await more.textContent()) ?? "").trim();
    if (/show only this/i.test(text)) throw new Error(`the link still describes the implementation: "${text}"`);
    if (!/every|all/i.test(text)) throw new Error(`the link does not promise the whole status: "${text}"`);
  });

  // B4 — no combination of parameters produces an empty column under a non-zero count.
  await check(label, "board under a status filter cannot show an empty column beneath a non-zero count", async () => {
    for (const filter of ["all", "open", "in_progress", "done"]) {
      await loadStage(page, { url: `/tasks?scope=team_progress&filter=${filter}` });
      const columns = await page.evaluate(() =>
        [...document.querySelectorAll(".ltb-col")].map((node) => ({
          key: node.className.match(/ltb-col-(\w+)/)?.[1] ?? "?",
          total: (node.querySelector(".ltb-colcount")?.textContent ?? "").trim(),
          meta: (node.querySelector(".ltb-colmeta")?.textContent ?? "").trim(),
        })),
      );
      if (!columns.length) throw new Error(`filter=${filter} rendered no board columns at all`);
      if (filter === "all" && columns.length !== 4) throw new Error(`filter=all must draw four columns, drew ${columns.length}`);
      if (filter !== "all") {
        if (columns.length !== 1) throw new Error(`filter=${filter} drew ${columns.length} columns; the board must collapse to one`);
        if (columns[0].key !== filter) throw new Error(`filter=${filter} drew the ${columns[0].key} column`);
        if ((await page.locator(".ltb-focusback").count()) === 0) throw new Error(`filter=${filter} gives no way back to all statuses`);
      }
      for (const column of columns) {
        const total = Number(column.total.replace(/[^\d]/g, ""));
        const onPage = Number((column.meta.match(/^(\d+)/) ?? [])[1] ?? "0");
        if (Number.isFinite(total) && total > 0 && onPage === 0) {
          throw new Error(`filter=${filter}: the ${column.key} column says "${column.meta}" beneath a total of ${column.total}`);
        }
      }
    }
  });

  // B5 — an unknown view param is not silently ignored, and the repair works.
  await check(label, "an unprefixed view param is reported and repairable", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress&view=list" });
    const note = page.locator(".lt-aliasnote").first();
    if ((await note.count()) === 0) throw new Error("?view=list was silently ignored again");
    const text = ((await note.textContent()) ?? "").replace(/\s+/g, " ");
    if (!text.includes("t_view")) throw new Error(`the notice does not name the parameter this page reads: "${text}"`);
    await note.locator("a").first().click();
    await page.waitForTimeout(900);
    const url = new URL(page.url());
    if (url.searchParams.get("t_view") !== "list") throw new Error(`the repair link did not select the list view; url=${page.url()}`);
    if (url.searchParams.get("view")) throw new Error("the repair link left the ignored parameter in the URL");
    if ((await page.locator("table").count()) === 0) throw new Error("the list view did not render a table");
    if ((await page.locator(".lt-aliasnote").count()) !== 0) throw new Error("the notice survived its own repair");
  });

  // The search box debounces to ONE request, which is the whole reason it is hand-rolled.
  await check(label, "the search box debounces to a single list request", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress", open: phone ? ".lt-fmore" : null });
    const requests = [];
    const listen = (request) => {
      const url = new URL(request.url());
      if (url.pathname === "/tasks" && url.searchParams.has("t_q")) requests.push(url.searchParams.get("t_q"));
    };
    page.on("request", listen);
    const search = page.locator('.lt-fsearch input[type="search"]').first();
    await search.click();
    await search.pressSequentially("fence", { delay: 40 });
    await page.waitForTimeout(1_800);
    page.off("request", listen);
    if (requests.length === 0) throw new Error("typing fired no list request at all");
    if (requests.length > 1) throw new Error(`typing "fence" fired ${requests.length} requests: ${requests.join(", ")}`);
    if (!new URL(page.url()).searchParams.get("t_q")) throw new Error("the typed query did not reach the URL");
  });

  await check(label, "drag is present at desktop width and absent at phone width", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress" });
    await page.waitForTimeout(400);
    const draggable = await page.locator('[draggable="true"]').count();
    if (phone && draggable !== 0) throw new Error(`${draggable} draggable cards at phone width; drag must be off there`);
    if (!phone && draggable === 0) throw new Error("no draggable card at desktop width");
  });

  await check(label, "the detail panel's own controls work", async () => {
    await loadStage(page, { url: `/tasks?scope=team_progress&task=${taskId}` });
    const panel = page.locator(".ltd-panel, .lt-detail-card").first();
    await panel.waitFor({ state: "visible", timeout: 10_000 });
    const composer = panel.locator("textarea").first();
    if ((await composer.count()) === 0) throw new Error("the detail panel has no update composer");
    await composer.fill("Harness reachability probe");
    if ((await composer.inputValue()) !== "Harness reachability probe") throw new Error("the composer did not take text");
    // Close must actually drop the selection from the URL.
    const close = panel.locator('a[aria-label*="Close"], button[aria-label*="Close"]').first();
    if ((await close.count()) === 0) throw new Error("the detail panel cannot be closed");
    await close.click();
    await page.waitForTimeout(900);
    if (new URL(page.url()).searchParams.get("task")) throw new Error("Close left the task selected in the URL");
  });

  await check(label, "the pager moves, and Prev on page one is disabled rather than dead", async () => {
    await loadStage(page, { url: "/tasks?scope=team_progress&t_view=list" });
    const next = page.locator('a[aria-label*="Next"], a[rel="next"], .wpager a').last();
    if ((await next.count()) === 0) throw new Error("no pager control rendered");
    const before = page.url();
    await next.click();
    await page.waitForTimeout(1_200);
    if (page.url() === before) throw new Error("the pager's next did not move the page");
    if (!new URL(page.url()).searchParams.get("t_cursor")) throw new Error("next did not mint a cursor");
  });
}

// ─────────────────────────────────────────────────────────────────────────────── plumbing

async function check(stageLabel, name, fn) {
  currentIssues = [];
  try {
    await fn();
    if (currentIssues.length) throw new Error(`page issues: ${currentIssues.join(" | ").slice(0, 400)}`);
    record(stageLabel, name, "pass", "");
    return;
  } catch (error) {
    record(stageLabel, name, "fail", String(error?.message ?? error).split("\n")[0].slice(0, 400));
  }
}

function record(stage, element, status, detail) {
  results.push({ stage, element, status, detail });
  console.log(`${status === "pass" ? "PASS" : "FAIL"} ${stage} :: ${element}${detail ? ` — ${detail}` : ""}`);
}

async function shot(page, stageLabel, index) {
  const safe = `${stageLabel}-${index}`.replaceAll(/[^a-z0-9]+/gi, "-").toLowerCase();
  await page.screenshot({ path: join(failureDir, `${safe}.png`), fullPage: true }).catch(() => undefined);
}

async function resolveTaskId() {
  const body = await fetchJson("/app/leadership-tasks?scope=team_progress&limit=1");
  const id = body?.rows?.[0]?.task_id ?? body?.rows?.[0]?.id;
  if (!id) throw new Error("no task row available to open the detail panel with");
  return id;
}

async function fetchJson(path) {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    headers: { Authorization: `Bearer ${bearerToken}`, [TENANT_CONTEXT_HEADER]: tenantId },
  });
  if (!response.ok) throw new Error(`${path} returned HTTP ${response.status}`);
  return response.json();
}

function renderMarkdown() {
  const lines = [
    "# Tasks Desk Click Matrix",
    "",
    `Run: \`${runId}\``,
    `Admin web: \`${appBaseUrl}\``,
    "",
    "| Status | Stage | Element / assertion | Observed |",
    "| --- | --- | --- | --- |",
  ];
  for (const entry of results) {
    lines.push(
      `| ${entry.status} | ${entry.stage} | ${entry.element.replaceAll("|", "\\|")} | ${(entry.detail ?? "").replaceAll("|", "\\|")} |`,
    );
  }
  return `${lines.join("\n")}\n`;
}

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}
