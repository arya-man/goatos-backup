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
 * `a, button, select, input, textarea, [role=button], [role=tab], [role=option], summary,
 * [draggable=true]` — plus the shell's notification bell, and for EACH one:
 *   (a) activates it and demands an OBSERVABLE CHANGE (see `fingerprint`: the URL, the set of
 *       aria-current / aria-expanded / aria-selected / aria-pressed / checked elements, the open
 *       dialogs and popovers, the card and row counts, the requests fired, and a hash of the
 *       visible text). An element that is present and causes NO observable change on activation
 *       FAILS the run. That is precisely the `+5` defect;
 *   (b) demands no console error and no page error fired while it was activated;
 *   (c) demands the page did not land on a failure string.
 *
 * An element that is legitimately inert at a viewport is asserted ABSENT or asserted DISABLED —
 * never "clicked and nothing happened". Drag lives behind `(min-width: 761px) and (pointer:
 * fine)`, so at 390px the harness asserts there is no `[draggable="true"]` card at all. An element
 * hidden at one viewport must be activated at some viewport, or the run fails for coverage.
 *
 * ── HOW IT SURVIVES ITS OWN CLICKS ────────────────────────────────────────────────────────────
 * Most controls on this desk navigate. A single pass that clicked element 0 would be on a different
 * page for element 1. So each stage is walked by INDEX: the stage URL is loaded, the elements are
 * enumerated, and for i in 0..n the stage is reloaded, re-enumerated, and only element i is
 * activated. Slower than one pass and the only version that can claim it activated all of them.
 *
 * ── NON-DESTRUCTIVE ───────────────────────────────────────────────────────────────────────────
 * It runs against a shared database. It never creates a task, never changes a status, never
 * posts a comment. Submit buttons on VALID forms (status transitions, Edit → Save, Send with text)
 * are asserted enabled and reachable and NOT submitted; submit buttons on INVALID forms (Send
 * with nothing typed, Create with empty required fields) are clicked and the browser's own refusal
 * is the observed change. Drag is exercised as dragstart/dragend without a drop.
 *
 * ── WHERE IT CAN RUN ──────────────────────────────────────────────────────────────────────────
 * It needs a LIVE STACK — a running admin-web and API with real rows. It is a `smoke:*:live`
 * script beside `smoke:vaccination-click-matrix:live` and, like that one, NOT part of a hermetic
 * guard target: `ci-local` has no browser and no database.
 *
 * Usage:
 *   GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:13308 \
 *   GOATOS_SMOKE_TOKEN_FILE=/path/to/token.txt   # or GOATOS_SMOKE_TOKEN=<jwt> \
 *   GOATOS_TASKS_MATRIX_REPORT_DIR=/tmp/tasks-matrix   # optional \
 *   npm --prefix apps/admin-web run smoke:tasks-click-matrix:live
 */
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "@playwright/test";
import {
  DRAG_MIN_WIDTH,
  PHONE_TAP_FLOOR,
  coverageGaps,
  elementKey,
  findFailureString,
  observedChange,
  planActivation,
  renderMarkdown,
  requiresBeyondValue,
  summarize,
  trimTrailingSlash,
} from "./lib/tasks-click-matrix.mjs";

const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const token = resolveToken();
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const runId = process.env.GOATOS_TASKS_MATRIX_RUN_ID ?? `TASKS-CLICK-${new Date().toISOString().replaceAll(/[:.]/g, "-")}`;
const reportDir = process.env.GOATOS_TASKS_MATRIX_REPORT_DIR
  ? resolve(process.env.GOATOS_TASKS_MATRIX_REPORT_DIR)
  : join(repoRoot, ".codex-goatos-render", "tasks-click-matrix", runId);
const failureDir = join(reportDir, "failures");
mkdirSync(failureDir, { recursive: true });

/** The page region this harness owns. */
const PAGE_REGION = ".lt-page";
/** Shell controls this page is responsible for anyway: the bell lives on this desk. */
const EXTRA_SELECTORS = ['.top button[aria-haspopup="dialog"]', ".top .parkmenu[role=dialog]"];
const INTERACTIVE = "a, button, select, input:not([type=hidden]), textarea, [role=button], [role=tab], [role=option], summary, [draggable=true]";
/** How long an activation is given to show something: the Next dev router can take >1s to land a `router.replace`. */
const SETTLE_MS = 4_000;
const SCOPE = "team_progress";

const VIEWPORTS = [
  { label: "1440x900", width: 1440, height: 900, isMobile: false },
  { label: "390x844", width: 390, height: 844, isMobile: true },
].filter((v) => !process.env.GOATOS_TASKS_MATRIX_VIEWPORTS || process.env.GOATOS_TASKS_MATRIX_VIEWPORTS.split(",").includes(v.label));
/** Restrict to named stages while developing the harness; unset runs the whole matrix. */
const ONLY_STAGES = process.env.GOATOS_TASKS_MATRIX_STAGES ? new Set(process.env.GOATOS_TASKS_MATRIX_STAGES.split(",")) : null;
/**
 * How many instances of the SAME control kind (see `elementKey`) are activated per stage. A
 * board of 25 cards is one component rendered 25 times; the first three prove it and the rest
 * are recorded as sampled. `0` activates every instance (the exhaustive run).
 */
const SAME_KIND_CAP = Number(process.env.GOATOS_TASKS_MATRIX_SAME_KIND_CAP ?? "3");

const results = [];
let currentIssues = [];
let requestCount = 0;
let viewportLabel = "";
let pageGone = false;

const browser = await chromium.launch();
try {
  for (const viewport of VIEWPORTS) {
    viewportLabel = viewport.label;
    const context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      hasTouch: viewport.isMobile,
      isMobile: viewport.isMobile,
    });
    await context.addCookies([
      {
        name: "goatos_firebase_id_token",
        value: token,
        domain: new URL(appBaseUrl).hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      },
    ]);
    const page = await context.newPage();
    // A closed browser is not a hundred dead controls: abort instead of recording each one.
    // (Reset per viewport: the previous context is closed deliberately at the end of its walk.)
    pageGone = false;
    page.on("close", () => { pageGone = true; });
    page.on("console", (message) => {
      if (message.type() !== "error") return;
      const text = message.text();
      if (isExpectedOptionalLocalAuthNoise(text)) return;
      currentIssues.push(`console: ${text.replace(/\s+/g, " ").slice(0, 700)}`);
    });
    page.on("pageerror", (error) => currentIssues.push(`pageerror: ${error.message.slice(0, 300)}`));
    page.on("request", (request) => {
      if (countsAsRequest(request.url())) requestCount += 1;
    });
    page.on("response", (response) => {
      if (response.status() < 500) return;
      if (isExpectedOptionalLocalAuthResponse(response.url())) return;
      currentIssues.push(`HTTP ${response.status()} ${new URL(response.url()).pathname}`);
    });

    const task = await resolveTask(page);
    for (const stage of stages(viewport, task)) {
      if (pageGone) throw new Error("the browser page closed mid-run; aborting rather than recording every remaining control as dead");
      if (ONLY_STAGES && !ONLY_STAGES.has(stage.name)) continue;
      await walkStage(page, viewport, stage);
    }
    if (!ONLY_STAGES || ONLY_STAGES.has("scripted")) await scriptedChecks(page, viewport, task);
    await context.close();
  }
} finally {
  await browser.close();
}

for (const gap of coverageGaps(results)) {
  record({ viewport: gap.viewport, stage: "coverage", element: gap.element, how: "coverage", status: "fail", detail: "hidden at this viewport and never activated at any viewport: the matrix never proved this control" });
}

const failed = results.filter((entry) => entry.status === "fail");
writeFileSync(join(reportDir, "matrix.json"), JSON.stringify({ appBaseUrl, runId, summary: summarize(results), results }, null, 2));
writeFileSync(join(reportDir, "matrix.md"), renderMarkdown({ runId, appBaseUrl, results }));
console.log("");
for (const [label, counts] of Object.entries(summarize(results))) {
  console.log(`${label}: pass ${counts.pass}  fail ${counts.fail}  (${counts.total})`);
}
console.log(`tasks click matrix ${failed.length ? "FAILED" : "passed"}; report_dir=${reportDir}`);
if (failed.length > 0) process.exit(1);

// ─────────────────────────────────────────────────────────────────────── the stages

function stages(viewport, task) {
  const phone = viewport.isMobile;
  const base = `/tasks?scope=${SCOPE}`;
  // The detail panel only renders a task that is ON the current page of its scope (the page
  // finds it in the rows it already has), so the detail stages open it from the scope it was
  // found in.
  const detailUrl = `/tasks?scope=${task.scope}&task=${task.id}`;
  return [
    { name: "board", url: base },
    { name: "list", url: `${base}&t_view=list` },
    { name: "scope-for-me", url: "/tasks?scope=assigned_to_me" },
    { name: "scope-raised-by-me", url: "/tasks?scope=assigned_by_me" },
    // B4's stage: a board under a status filter.
    { name: "board-filter-done", url: `${base}&filter=done` },
    // Active-filter chips and Clear only exist once something narrows the list.
    { name: "active-chips", url: `${base}&filter=open&t_deadline_from=2026-01-01&t_deadline_to=2026-12-31&t_q=feed` },
    { name: "list-page-2", url: `${base}&t_view=list&t_limit=5`, open: [".lt-page a.btn:has-text('Next')"] },
    { name: "detail-panel", url: detailUrl },
    { name: "detail-edit-modal-open", url: detailUrl, open: ['.ltd-top-actions button[aria-haspopup="dialog"]'] },
    // B5's stage: the ignored-parameter notice and its corrective link.
    { name: "ignored-view-param", url: `${base}&view=list` },
    // The phone filter sheet has to be OPEN for its controls to be in the DOM at all.
    phone ? { name: "filter-sheet-open", url: base, open: [".lt-fmore"] } : null,
    { name: "people-assignee-open", url: base, open: openInBar(phone, ".lt-pf-trigger >> nth=0") },
    { name: "people-raiser-open", url: base, open: openInBar(phone, ".lt-pf-trigger >> nth=1") },
    { name: "date-deadline-open", url: base, open: openInBar(phone, ".lt-fdrop >> nth=0 >> button") },
    { name: "date-raised-open", url: base, open: openInBar(phone, ".lt-fdrop >> nth=1 >> button") },
    { name: "new-task-modal-open", url: base, open: [".lt-page .btn.p:has-text('New task')"] },
    { name: "bell-panel-open", url: base, open: ['.top button[aria-haspopup="dialog"]'] },
  ].filter(Boolean);
}

/** On a phone every bar control lives behind the filter sheet, so it is opened first. */
function openInBar(phone, selector) {
  return phone ? [".lt-fmore", selector] : [selector];
}

// ─────────────────────────────────────────────────────────────── the enumerate-and-click walk

async function walkStage(page, viewport, stage) {
  const stageLabel = stage.name;
  let elements;
  try {
    await loadStage(page, stage);
    elements = await enumerate(page);
  } catch (error) {
    await shot(page, stageLabel, "enumerate");
    record({ viewport: viewport.label, stage: stageLabel, element: "(stage)", how: "enumerate", status: "fail", detail: firstLine(error) });
    return;
  }
  if (!elements.length) {
    record({ viewport: viewport.label, stage: stageLabel, element: "(stage)", how: "enumerate", status: "fail", detail: "no interactive elements found in the page region" });
    return;
  }
  record({ viewport: viewport.label, stage: stageLabel, element: "(stage)", how: "enumerate", status: "pass", detail: `${elements.length} interactive elements` });

  const seenKinds = new Map();
  for (let index = 0; index < elements.length; index += 1) {
    const key = elementKey(elements[index]);
    const seen = seenKinds.get(key) ?? 0;
    seenKinds.set(key, seen + 1);
    if (SAME_KIND_CAP > 0 && seen >= SAME_KIND_CAP && !elements[index].hidden && !elements[index].disabled) {
      record({ viewport: viewport.label, stage: stageLabel, element: describe(index, elements[index]), key, how: "sampled", status: "pass", detail: `same control kind as ${SAME_KIND_CAP} already activated on this stage (GOATOS_TASKS_MATRIX_SAME_KIND_CAP=0 activates all)` });
      continue;
    }
    // `seen` is this element's ordinal among its kind, which is how it is found again after a
    // reload even when an unrelated element (a banner, a notice) shifts every index.
    await activateOne(page, viewport, stage, index, elements[index], seen);
  }
}

async function loadStage(page, stage) {
  const response = await page.goto(`${appBaseUrl}${stage.url}`, { waitUntil: "domcontentloaded", timeout: 45_000 });
  if (response && !response.ok()) throw new Error(`${stage.url} returned HTTP ${response.status()}`);
  await page.locator(PAGE_REGION).first().waitFor({ state: "visible", timeout: 20_000 });
  await page.waitForLoadState("networkidle", { timeout: 4_000 }).catch(() => undefined);
  // Hydration: the board's drag capability and the bell are client-decided.
  await page.waitForTimeout(400);
  for (const opener of stage.open ?? []) {
    const node = page.locator(opener).first();
    await node.waitFor({ state: "visible", timeout: 10_000 });
    await node.click();
    await page.waitForLoadState("networkidle", { timeout: 6_000 }).catch(() => undefined);
    await page.waitForTimeout(350);
  }
}

/**
 * Every interactive element in the page region, as a stable DESCRIPTOR rather than a handle: a
 * handle does not survive the navigation the previous click caused. The index is STAMPED on the
 * node so activation targets exactly the element the enumeration described.
 */
async function enumerate(page) {
  return page.evaluate(
    ({ region, extras, selector }) => {
      const roots = [document.querySelector(region), ...extras.map((s) => document.querySelector(s))].filter(Boolean);
      const seen = new Set();
      const out = [];
      const isHidden = (node) => {
        if (typeof node.checkVisibility === "function") {
          return !node.checkVisibility({ checkVisibilityCSS: true, checkOpacity: false });
        }
        const style = window.getComputedStyle(node);
        return style.display === "none" || style.visibility === "hidden" || node.getClientRects().length === 0;
      };
      for (const root of roots) {
        const nodes = root.matches(selector) ? [root, ...root.querySelectorAll(selector)] : [...root.querySelectorAll(selector)];
        for (const node of nodes) {
          if (seen.has(node)) continue;
          seen.add(node);
          node.setAttribute("data-clickmatrix", String(out.length));
          const box = node.getBoundingClientRect();
          const form = node.form ?? node.closest("form");
          // What a tap at the element's centre would actually hit. Behind a scrim, that is the
          // scrim — which is the overlay doing its job, not a dead control.
          const hit = box.width > 0 && box.height > 0 ? document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2) : null;
          const covered = Boolean(hit) && hit !== node && !node.contains(hit) && !hit.contains(node);
          out.push({
            tag: node.tagName.toLowerCase(),
            type: node.getAttribute("type") ?? "",
            role: node.getAttribute("role") ?? "",
            draggable: node.getAttribute("draggable") ?? "",
            disabled: node.hasAttribute("disabled") || node.getAttribute("aria-disabled") === "true",
            hidden: isHidden(node),
            covered,
            formValid: form ? form.checkValidity() : null,
            className: (typeof node.className === "string" ? node.className : "").slice(0, 80),
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
    { region: PAGE_REGION, extras: EXTRA_SELECTORS, selector: INTERACTIVE },
  );
}

/**
 * The page's observable state. Anything a reader could notice: where they are, what is marked
 * current, what is open, how many rows and cards, the visible words — and whether the page talked
 * to the server.
 */
async function fingerprint(page) {
  const dom = await page.evaluate(() => {
    const names = (selector) =>
      [...document.querySelectorAll(selector)]
        .map((node) => (node.getAttribute("aria-label") || node.textContent || "").replace(/\s+/g, " ").trim().slice(0, 40))
        .sort()
        .join("|");
    const text = (document.querySelector("main") ?? document.body).innerText.replace(/\s+/g, " ").trim();
    let hash = 0;
    for (let i = 0; i < text.length; i += 1) hash = (hash * 31 + text.charCodeAt(i)) | 0;
    const active = document.activeElement;
    return {
      href: location.href,
      current: names('[aria-current="true"], [aria-current="page"]'),
      expanded: names('[aria-expanded="true"]'),
      selected: names('[aria-selected="true"]'),
      pressed: names('[aria-pressed="true"]'),
      checked: [...document.querySelectorAll("input:checked, [aria-checked=true]")].length,
      onClass: document.querySelectorAll(".on").length,
      dialogs: document.querySelectorAll('[role="dialog"].on, .lt-modal, .drawer.on, .lt-fgroup.open').length,
      popovers: document.querySelectorAll("[data-people-popup], .lt-fdrop-pop, [data-mention-popup]").length,
      detailsOpen: document.querySelectorAll("details[open]").length,
      cards: document.querySelectorAll(".ltb-card").length,
      rows: document.querySelectorAll("tbody tr").length,
      dragging: document.querySelectorAll(".ltb-card.is-dragging, .ltb-col.is-over, .ltb-col.is-drop").length,
      inputs: [...document.querySelectorAll("input, select, textarea")].map((node) => node.value ?? "").join("|"),
      invalidFocused: Boolean(active && active.matches?.(":invalid")),
      textHash: hash,
      textLength: text.length,
    };
  });
  return { ...dom, requests: requestCount };
}

async function activateOne(page, viewport, stage, index, descriptor, ordinal = 0) {
  const stageLabel = stage.name;
  const key = elementKey(descriptor);
  const element = describe(index, descriptor);
  const base = { viewport: viewport.label, stage: stageLabel, element, key };
  const plan = planActivation(descriptor, viewport.width, Boolean(stage.open?.length));

  if (plan.how === "hidden") {
    record({ ...base, how: "hidden", status: "pass", detail: plan.reason });
    return;
  }
  if (plan.how === "covered") {
    record({ ...base, how: "covered", status: "pass", detail: plan.reason });
    return;
  }
  if (plan.how === "disabled") {
    record({ ...base, how: "disabled", status: "pass", detail: plan.reason });
    return;
  }
  if (plan.how === "drag-absent") {
    await shot(page, stageLabel, index);
    record({ ...base, how: "drag-absent", status: "fail", detail: plan.reason });
    return;
  }
  if (plan.how === "file") {
    record({ ...base, how: "file", status: "pass", detail: plan.reason });
    return;
  }
  // TAP TARGET at phone width is a separate finding; the control is still activated.
  if (viewport.isMobile && (descriptor.height < PHONE_TAP_FLOOR || descriptor.width < 24)) {
    record({ ...base, how: "tap-target", status: "fail", detail: `tap target ${descriptor.width}x${descriptor.height} is under the ${PHONE_TAP_FLOOR}px floor` });
  }
  if (plan.how === "write-guard") {
    record({ ...base, how: "write-guard", status: "pass", detail: plan.reason });
    return;
  }

  // Fresh stage, so this is the ONLY activation that has happened on it.
  try {
    await loadStage(page, stage);
  } catch (error) {
    await shot(page, stageLabel, index);
    const body = await page.locator("body").innerText().catch(() => "");
    record({ ...base, how: plan.how, status: "fail", detail: `stage reload failed: ${firstLine(error)} — body: ${body.replace(/\s+/g, " ").slice(0, 160)}` });
    return;
  }
  const elements = await enumerate(page).catch(() => []);
  // Find this element again by its kind and ordinal, so an inserted banner does not turn every
  // control after it into a false "unstable DOM".
  let found = -1;
  let seen = 0;
  for (let i = 0; i < elements.length; i += 1) {
    if (elementKey(elements[i]) !== key) continue;
    if (seen === ordinal) { found = i; break; }
    seen += 1;
  }
  if (found < 0) {
    record({ ...base, how: plan.how, status: "fail", detail: "element was not present on a re-load of the same stage (unstable DOM)" });
    return;
  }
  index = found;
  if (elements[index].hidden) {
    record({ ...base, how: plan.how, status: "fail", detail: "element became hidden on a re-load of the same stage (unstable DOM)" });
    return;
  }

  currentIssues = [];
  const before = await fingerprint(page);
  const target = nth(page, index);
  const ignore = [];
  try {
    switch (plan.how) {
      case "select": {
        const values = await target.evaluate((node) => [...node.options].map((o) => o.value));
        const next = await target.evaluate((node) => [...node.options].map((o) => o.value).find((v) => v !== node.value));
        if (next === undefined) {
          record({ ...base, how: plan.how, status: "fail", detail: `a select with only one option (${values.join(",")}) cannot do anything` });
          return;
        }
        await target.selectOption(next);
        break;
      }
      case "type":
        await target.click({ timeout: 8_000 });
        await target.pressSequentially("fence", { delay: 30 });
        // The toolbar search is a navigation: its own value, its clear button and the text it
        // paints do not count. Only the URL, the list, or a request does.
        if (requiresBeyondValue(descriptor)) ignore.push("inputs", "textHash", "textLength", "onClass");
        break;
      case "date":
        await target.fill(descriptor.type === "datetime-local" ? "2026-10-01T10:00" : descriptor.type === "time" ? "10:00" : descriptor.type === "month" ? "2026-10" : descriptor.type === "week" ? "2026-W40" : "2026-09-01");
        break;
      case "toggle":
        await target.click({ timeout: 8_000 });
        break;
      case "drag": {
        const dataTransfer = await page.evaluateHandle(() => new DataTransfer());
        await target.dispatchEvent("dragstart", { dataTransfer });
        const during = await settleKey(page, before, "dragging");
        await target.dispatchEvent("dragend", { dataTransfer });
        const after = await settleKey(page, during, "dragging");
        const started = observedChange(before, during, ["requests"]);
        const cleared = observedChange(during, after, ["requests"]);
        if (!started.length) {
          await shot(page, stageLabel, index);
          record({ ...base, how: plan.how, status: "fail", detail: "dragstart put the board in no visible drag state" });
        } else if (!cleared.length) {
          await shot(page, stageLabel, index);
          record({ ...base, how: plan.how, status: "fail", detail: `dragend left the board in its drag state (${started.join(", ")})` });
        } else {
          record({ ...base, how: plan.how, status: currentIssues.length ? "fail" : "pass", detail: currentIssues.length ? `page issues: ${currentIssues.join(" | ")}` : `dragstart → ${started.join(", ")}; dragend cleared it` });
        }
        return;
      }
      case "submit-invalid":
      case "click":
      default:
        await target.click({ timeout: 8_000 });
        break;
    }
  } catch (error) {
    await shot(page, stageLabel, index);
    record({ ...base, how: plan.how, status: "fail", detail: `${plan.reason} threw: ${firstLine(error)}` });
    return;
  }

  // Give a navigation, a transition or the 300ms search debounce time to land: poll for a
  // change rather than sleep a fixed time, so a dead control costs the full wait and a live
  // one is recorded as soon as it moves.
  const after = await settle(page, before, ignore).catch(() => null);
  if (!after) {
    record({ ...base, how: plan.how, status: "fail", detail: "the page stopped answering after activation" });
    return;
  }
  const body = await page.locator("body").innerText().catch(() => "");
  const badString = findFailureString(body);
  if (badString) {
    await shot(page, stageLabel, index);
    record({ ...base, how: plan.how, status: "fail", detail: `landed on a failure string: ${badString}` });
    return;
  }
  if (currentIssues.length) {
    await shot(page, stageLabel, index);
    record({ ...base, how: plan.how, status: "fail", detail: `page issues: ${currentIssues.join(" | ").slice(0, 400)}` });
    return;
  }

  if (plan.how === "submit-invalid") {
    if (after.requests !== before.requests) {
      await shot(page, stageLabel, index);
      record({ ...base, how: plan.how, status: "fail", detail: "submit on an invalid form sent a request" });
      return;
    }
    if (!after.invalidFocused) {
      await shot(page, stageLabel, index);
      record({ ...base, how: plan.how, status: "fail", detail: "submit on an invalid form did not focus the invalid field (silent refusal)" });
      return;
    }
    record({ ...base, how: plan.how, status: "pass", detail: "browser refused the submit and focused the invalid field; no request sent" });
    return;
  }

  const diff = observedChange(before, after, ignore);
  if (!diff.length) {
    await shot(page, stageLabel, index);
    record({ ...base, how: plan.how, status: "fail", detail: `activated (${plan.reason}) and NOTHING observable changed — this is the dead-control defect` });
    return;
  }
  record({ ...base, how: plan.how, status: "pass", detail: `${plan.reason} → changed: ${diff.join(", ")}` });
}

/**
 * Poll the fingerprint until something outside `ignore` changes, or SETTLE_MS passes. Always
 * takes one extra reading after the change so a navigation mid-flight is not read half-done.
 */
async function settle(page, before, ignore = []) {
  const started = Date.now();
  // A full navigation destroys the execution context mid-read; that is a change in progress,
  // not a dead page, so a failed reading is retried until the deadline.
  const read = () => fingerprint(page).catch(() => null);
  let after = await read();
  while (Date.now() - started < SETTLE_MS) {
    // A request alone does not stop the wait: a navigation's fetch fires long before its URL
    // and content land, and reading the page then would call a working link "requests only".
    if (after && observedChange(before, after, [...ignore, "requests"]).length) {
      await page.waitForLoadState("networkidle", { timeout: 3_000 }).catch(() => undefined);
      await page.waitForTimeout(250);
      return (await read()) ?? after;
    }
    await page.waitForTimeout(200);
    after = await read();
  }
  return after;
}

/** Poll until one fingerprint key differs from `before` (a React state flip), up to 2s. */
async function settleKey(page, before, key) {
  const started = Date.now();
  let after = await fingerprint(page);
  while (JSON.stringify(after[key]) === JSON.stringify(before[key]) && Date.now() - started < 2_000) {
    await page.waitForTimeout(100);
    after = await fingerprint(page);
  }
  return after;
}

/** Poll until the URL has `key` equal to `value` (or, with value null, absent). Returns the URL. */
async function waitForParam(page, key, value, timeout = SETTLE_MS + 2_000) {
  const started = Date.now();
  for (;;) {
    const url = new URL(page.url());
    const got = url.searchParams.get(key);
    if (value === null ? got === null || got === "" : got === value) {
      await page.waitForLoadState("networkidle", { timeout: 3_000 }).catch(() => undefined);
      return url;
    }
    if (Date.now() - started > timeout) return url;
    await page.waitForTimeout(150);
  }
}

function describe(index, descriptor) {
  return `[${index}] ${descriptor.tag}${descriptor.role ? `[${descriptor.role}]` : ""}${descriptor.type ? `[${descriptor.type}]` : ""} "${descriptor.name}"`;
}

/** The element the enumeration stamped with this index. */
function nth(page, index) {
  return page.locator(`[data-clickmatrix="${index}"]`).first();
}

// ────────────────────────────────────────────────────── the assertions enumeration cannot make

async function scriptedChecks(page, viewport, task) {
  const phone = viewport.isMobile;
  const base = `/tasks?scope=${SCOPE}`;
  const peopleOpen = (index) => ({ url: base, open: openInBar(phone, `.lt-pf-trigger >> nth=${index}`) });

  // B1/B2 — the person filter can reach EVERY assignable person by typing their name.
  await check(page, viewport, "person filter: reaches every assignable person by typing", async () => {
    await loadStage(page, peopleOpen(0));
    const popup = page.locator("[data-people-popup]").first();
    await popup.waitFor({ state: "visible", timeout: 8_000 });
    const search = popup.locator('input[type="search"]');
    if ((await search.count()) !== 1) throw new Error("the person filter has no search field");
    const names = (await popup.locator('[role="option"] .lt-pf-name, [role="option"]').allInnerTexts())
      .map((t) => t.split("\n")[0].trim())
      .filter((t) => t && !/^all$/i.test(t));
    if (names.length < 7) throw new Error(`expected the multi-person roster, got ${names.length}: ${names.join(", ")}`);
    for (const name of names) {
      await search.fill(name);
      await page.waitForTimeout(120);
      const option = popup.locator('[role="option"]').filter({ hasText: name }).first();
      if ((await option.count()) === 0) throw new Error(`"${name}" cannot be reached by typing their name`);
    }
    if ((await page.locator(".ltb-person-rest, .lt-person-rest").count()) !== 0) throw new Error("the dead +N overflow chip is still rendered");
    return `${names.length} people reachable by name`;
  });

  await check(page, viewport, "person filter: type, pick, chip appears, clear", async () => {
    await loadStage(page, peopleOpen(0));
    const popup = page.locator("[data-people-popup]").first();
    const search = popup.locator('input[type="search"]');
    const second = popup.locator('[role="option"]').nth(2);
    const pickName = (await second.innerText()).split("\n")[0].trim();
    await search.fill(pickName.slice(0, 3));
    await page.waitForTimeout(150);
    await popup.locator('[role="option"]').filter({ hasText: pickName }).first().click();
    await page.waitForTimeout(SETTLE_MS / 4);
    await page.waitForLoadState("networkidle", { timeout: 6_000 }).catch(() => undefined);
    const picked = new URL(page.url()).searchParams.get("t_assignee");
    if (!picked) throw new Error(`picking "${pickName}" did not set t_assignee; url=${page.url()}`);
    if (phone) await page.locator(".lt-fmore").first().click().catch(() => undefined);
    const chip = page.locator(".lt-factive .achip").filter({ hasText: pickName.slice(0, 4) }).first();
    if ((await chip.count()) === 0) throw new Error(`the applied person filter is not stated as an active chip for "${pickName}"`);
    await chip.locator("button").first().click();
    if ((await waitForParam(page, "t_assignee", null)).searchParams.get("t_assignee")) throw new Error("removing the chip left t_assignee in the URL");
    return `picked ${pickName} → t_assignee=${picked} → chip removed it`;
  });

  await check(page, viewport, "person filter: keyboard only, and it lands in the URL", async () => {
    await loadStage(page, peopleOpen(1));
    const popup = page.locator("[data-people-popup]").first();
    const search = popup.locator('input[type="search"]');
    await search.fill("a");
    await page.waitForTimeout(150);
    const first = await search.getAttribute("aria-activedescendant");
    await search.press("ArrowDown");
    const second = await search.getAttribute("aria-activedescendant");
    if (!first || first === second) throw new Error("ArrowDown did not move the highlight");
    await search.press("Enter");
    await page.waitForTimeout(SETTLE_MS / 4);
    await page.waitForLoadState("networkidle", { timeout: 6_000 }).catch(() => undefined);
    const url = new URL(page.url());
    if (!url.searchParams.get("t_raiser")) throw new Error(`Enter did not set t_raiser; url=${page.url()}`);
    return `t_raiser=${url.searchParams.get("t_raiser")}`;
  });

  await check(page, viewport, "person filter: Escape closes it and returns focus to its trigger", async () => {
    await loadStage(page, peopleOpen(0));
    await page.keyboard.press("Escape");
    await page.waitForTimeout(200);
    if ((await page.locator("[data-people-popup]").count()) !== 0) throw new Error("Escape left the popup open");
    const focused = await page.evaluate(() => document.activeElement?.className ?? "");
    if (!focused.includes("lt-pf-trigger")) throw new Error(`focus did not return to the trigger; activeElement=${focused}`);
  });

  await check(page, viewport, "person filter: does not overflow the viewport; rows are tap targets", async () => {
    await loadStage(page, peopleOpen(0));
    const box = await page.locator("[data-people-popup]").first().boundingBox();
    const size = page.viewportSize();
    if (!box) throw new Error("no popup box");
    if (box.x < -1 || box.x + box.width > size.width + 1) {
      throw new Error(`popup spans ${Math.round(box.x)}..${Math.round(box.x + box.width)} in a ${size.width}px viewport`);
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    if (overflow > 1) throw new Error(`opening the popup added ${overflow}px of horizontal page scroll`);
    const short = await page.evaluate(
      (floor) => [...document.querySelectorAll('[data-people-popup] [role="option"]')].filter((node) => node.getBoundingClientRect().height < floor).length,
      PHONE_TAP_FLOOR,
    );
    if (short > 0) throw new Error(`${short} option rows are under ${PHONE_TAP_FLOOR}px tall`);
  });

  // B3 — the column link says the intent, not the implementation.
  await check(page, viewport, 'board column "more" link states the intent', async () => {
    await loadStage(page, { url: base });
    const more = page.locator(".ltb-colmore").first();
    if ((await more.count()) === 0) throw new Error("no column offers to show its whole status");
    const text = ((await more.textContent()) ?? "").trim();
    if (/show only this/i.test(text)) throw new Error(`the link still describes the implementation: "${text}"`);
    if (!/every|all/i.test(text)) throw new Error(`the link does not promise the whole status: "${text}"`);
    return `"${text}"`;
  });

  // B4 — no combination of parameters produces an empty column under a non-zero count.
  await check(page, viewport, "board under a status filter cannot show an empty column beneath a non-zero count", async () => {
    const seen = [];
    for (const filter of ["all", "open", "in_progress", "done"]) {
      await loadStage(page, { url: `${base}&filter=${filter}` });
      const columns = await page.evaluate(() =>
        [...document.querySelectorAll(".ltb-col")].map((node) => ({
          key: node.className.match(/ltb-col-(\w+)/)?.[1] ?? "?",
          total: (node.querySelector(".ltb-colcount")?.textContent ?? "").trim(),
          meta: (node.querySelector(".ltb-colmeta")?.textContent ?? "").trim(),
          cards: node.querySelectorAll(".ltb-card").length,
        })),
      );
      if (!columns.length) throw new Error(`filter=${filter} rendered no board columns at all`);
      seen.push(`${filter}:${columns.map((c) => `${c.key}=${c.cards}/${c.total}`).join(",")}`);
      if (filter === "all" && columns.length !== 4) throw new Error(`filter=all must draw four columns, drew ${columns.length}`);
      for (const column of columns) {
        const total = Number(column.total.replace(/[^\d]/g, ""));
        if (Number.isFinite(total) && total > 0 && column.cards === 0) {
          throw new Error(`filter=${filter}: the ${column.key} column is empty ("${column.meta}") beneath a total of ${column.total}`);
        }
      }
    }
    return seen.join(" · ");
  });

  // B5 — `?view=list` is honoured as an alias, and any OTHER unprefixed lookalike is named
  // out loud with a repair link rather than silently ignored.
  await check(page, viewport, "?view=list is honoured, and an unread lookalike param is reported and repairable", async () => {
    await loadStage(page, { url: `${base}&view=list` });
    if ((await page.locator("table").count()) === 0) throw new Error("?view=list did not render the list view (alias not honoured)");
    await loadStage(page, { url: `${base}&sort=deadline_asc` });
    const note = page.locator(".lt-aliasnote").first();
    if ((await note.count()) === 0) throw new Error("?sort=deadline_asc was silently ignored: no notice");
    const text = ((await note.textContent()) ?? "").replace(/\s+/g, " ");
    if (!text.includes("t_sort")) throw new Error(`the notice does not name the parameter this page reads: "${text}"`);
    await note.locator("a").first().click();
    const url = await waitForParam(page, "t_sort", "deadline_asc");
    if (url.searchParams.get("t_sort") !== "deadline_asc") throw new Error(`the repair link did not apply the sort; url=${page.url()}`);
    if (url.searchParams.get("sort")) throw new Error("the repair link left the ignored parameter in the URL");
    if ((await page.locator(".lt-aliasnote").count()) !== 0) throw new Error("the notice survived its own repair");
    return "view alias honoured; sort lookalike reported and repaired";
  });

  // The search box debounces to ONE list request.
  await check(page, viewport, "the search box debounces to a single list request", async () => {
    await loadStage(page, { url: base });
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
    if (new URL(page.url()).searchParams.get("t_q") !== "fence") throw new Error(`the typed query did not reach the URL: ${page.url()}`);
    return `1 request, t_q=${requests[0]}`;
  });

  await check(page, viewport, "the search box clear control empties the query", async () => {
    await loadStage(page, { url: `${base}&t_q=fence` });
    const clear = page.locator(".lt-qclr").first();
    if ((await clear.count()) === 0) throw new Error("no clear control beside a non-empty search");
    await clear.click();
    if ((await waitForParam(page, "t_q", null)).searchParams.get("t_q")) throw new Error(`clear left t_q in the URL: ${page.url()}`);
  });

  await check(page, viewport, "date disclosure: apply lands both bounds in the URL, clear removes them", async () => {
    await loadStage(page, { url: base, open: openInBar(phone, ".lt-fdrop >> nth=0 >> button") });
    const pop = page.locator(".lt-fdrop-pop").first();
    await pop.waitFor({ state: "visible", timeout: 5_000 });
    const dates = pop.locator('input[type="date"]');
    if ((await dates.count()) !== 2) throw new Error(`expected 2 date inputs, got ${await dates.count()}`);
    await dates.nth(0).fill("2026-01-01");
    await dates.nth(1).fill("2026-12-31");
    await pop.locator("button.btn.p").first().click();
    let url = await waitForParam(page, "t_deadline_to", "2026-12-31");
    if (url.searchParams.get("t_deadline_from") !== "2026-01-01" || url.searchParams.get("t_deadline_to") !== "2026-12-31") {
      throw new Error(`apply did not land both bounds: ${page.url()}`);
    }
    if (phone) await page.locator(".lt-fmore").first().click();
    await page.locator(".lt-fdrop >> nth=0 >> button").first().click();
    await page.locator(".lt-fdrop-pop").first().waitFor({ state: "visible", timeout: 5_000 });
    const clear = page.locator(".lt-fdrop-pop .lt-fdrop-act button:not(.p)").first();
    if ((await clear.count()) === 0) throw new Error("an applied range offers no Clear");
    await clear.click();
    url = await waitForParam(page, "t_deadline_to", null);
    if (url.searchParams.get("t_deadline_from") || url.searchParams.get("t_deadline_to")) throw new Error(`clear left the bounds: ${page.url()}`);
  });

  await check(page, viewport, "active filters: Clear removes every narrowing at once", async () => {
    await loadStage(page, { url: `${base}&filter=open&t_q=feed&t_raised_from=2026-01-01&t_raised_to=2026-12-31` });
    if (phone) await page.locator(".lt-fmore").first().click();
    const clear = page.locator(".lt-fclear").first();
    if ((await clear.count()) === 0) throw new Error("no Clear control under active filters");
    await clear.click();
    const url = await waitForParam(page, "t_raised_to", null);
    const left = ["t_q", "t_raised_from", "t_raised_to"].filter((k) => url.searchParams.get(k));
    if (left.length) throw new Error(`Clear left ${left.join(", ")} in the URL: ${page.url()}`);
  });

  await check(page, viewport, "sort select changes the URL and the first card", async () => {
    await loadStage(page, { url: base, open: phone ? [".lt-fmore"] : [] });
    const firstBefore = await page.locator(".ltb-card").first().getAttribute("href");
    const select = page.locator(".lt-fsel select").first();
    const next = await select.evaluate((node) => [...node.options].map((o) => o.value).find((v) => v !== node.value));
    await select.selectOption(next);
    if ((await waitForParam(page, "t_sort", next)).searchParams.get("t_sort") !== next) throw new Error(`t_sort was not set to ${next}: ${page.url()}`);
    const firstAfter = await page.locator(".ltb-card").first().getAttribute("href");
    if (firstBefore === firstAfter) throw new Error(`sort=${next} left the same first card`);
    return `t_sort=${next}`;
  });

  await check(page, viewport, "drag is present at desktop width and absent at phone width", async () => {
    await loadStage(page, { url: base });
    const draggable = await page.locator('[draggable="true"]').count();
    if (phone && draggable !== 0) throw new Error(`${draggable} draggable cards at phone width; drag must be off there`);
    if (!phone && draggable === 0) throw new Error("no draggable card at desktop width");
    return `${draggable} draggable cards`;
  });

  await check(page, viewport, "card → detail panel → Edit opens/closes → composer takes text → @ opens the picker → Close drops the selection", async () => {
    await loadStage(page, { url: `/tasks?scope=${task.scope}` });
    const card = page.locator(`.ltb-card[href*="task=${task.id}"]`).first();
    if ((await card.count()) === 0) throw new Error(`the resolved task ${task.id} has no card on the ${task.scope} board`);
    await card.click();
    const panel = page.locator(".ltd-panel").first();
    await panel.waitFor({ state: "visible", timeout: 10_000 });
    if (!new URL(page.url()).searchParams.get("task")) throw new Error("clicking a card did not select it in the URL");

    const edit = panel.locator('.ltd-top-actions button[aria-haspopup="dialog"]').first();
    if ((await edit.count()) === 0) throw new Error("the detail panel offers no Edit for an actor who can edit");
    await edit.click();
    await page.locator(".lt-modal[role=dialog]").first().waitFor({ state: "visible", timeout: 5_000 });
    await page.locator(".lt-modal-hd button[aria-label]").first().click();
    await page.waitForTimeout(300);
    if ((await page.locator(".lt-modal[role=dialog]").count()) !== 0) throw new Error("the Edit modal's Close left it open");

    const statusButtons = panel.locator(".lt-status-actions button[type=submit]");
    if ((await statusButtons.count()) === 0) throw new Error("no status transition offered on the detail panel");

    const composer = panel.locator("textarea").first();
    if ((await composer.count()) === 0) throw new Error("the detail panel has no update composer");
    await composer.click();
    await composer.pressSequentially("Harness probe @", { delay: 20 });
    await page.waitForTimeout(400);
    if ((await page.locator("[data-mention-popup]").count()) === 0) throw new Error("typing @ did not open the mention picker");
    await page.keyboard.press("Escape");
    await page.waitForTimeout(200);

    const close = panel.locator(".ltd-close").first();
    if ((await close.count()) === 0) throw new Error("the detail panel cannot be closed");
    await close.click();
    if ((await waitForParam(page, "task", null)).searchParams.get("task")) throw new Error("Close left the task selected in the URL");
    return `${await statusButtons.count()} status actions offered (not submitted)`;
  });

  await check(page, viewport, "the pager moves forward and back, and Prev on page one is disabled rather than dead", async () => {
    await loadStage(page, { url: `${base}&t_view=list&t_limit=5` });
    const prev = page.locator(".lt-page .btn:has-text('Previous'), .lt-page .btn:has-text('Prev')").first();
    if ((await prev.count()) === 0) throw new Error("page one renders no Previous control at all (it should be present and disabled)");
    const disabled = (await prev.getAttribute("aria-disabled")) === "true" || (await prev.getAttribute("disabled")) !== null;
    if (!disabled) throw new Error("Previous on page one is rendered enabled");
    const next = page.locator(".lt-page a.btn:has-text('Next')").first();
    if ((await next.count()) === 0) throw new Error("no Next pager control rendered");
    const before = await page.locator("tbody tr").first().innerText();
    await next.click();
    const url = await waitForParam(page, "t_page", "2");
    if (!url.searchParams.get("t_cursor")) throw new Error(`next did not mint a cursor: ${page.url()}`);
    const after = await page.locator("tbody tr").first().innerText();
    if (before === after) throw new Error("page 2 shows the same first row as page 1");
    const back = page.locator(".lt-page a.btn:has-text('Previous'), .lt-page a.btn:has-text('Prev')").first();
    if ((await back.count()) === 0) throw new Error("page 2 offers no way back");
    await back.click();
    await waitForParam(page, "t_page", null);
    const again = await page.locator("tbody tr").first().innerText();
    if (again !== before) throw new Error("Prev did not return to page one's rows");
    return `page=${url.searchParams.get("t_page")}`;
  });

  await check(page, viewport, "the bell opens its panel, the panel closes, aria-expanded tracks it", async () => {
    await loadStage(page, { url: base });
    const bell = page.locator('.top button[aria-haspopup="dialog"]').first();
    if ((await bell.count()) === 0) throw new Error("no notification bell in the top bar");
    if ((await bell.getAttribute("aria-expanded")) !== "false") throw new Error("bell does not start collapsed");
    await bell.click();
    await page.locator(".top .parkmenu.on[role=dialog]").first().waitFor({ state: "visible", timeout: 5_000 }).catch(() => undefined);
    if ((await bell.getAttribute("aria-expanded")) !== "true") throw new Error("bell did not report expanded after a click");
    const panel = page.locator(".top .parkmenu.on[role=dialog]").first();
    if ((await panel.count()) === 0) throw new Error("the bell's panel did not open");
    const close = panel.locator("button[aria-label]").first();
    await close.click();
    await page.waitForTimeout(300);
    if ((await bell.getAttribute("aria-expanded")) !== "false") throw new Error("closing the panel did not collapse the bell");
  });

  await check(page, viewport, "New task modal opens, empty Create is refused by the browser, Close dismisses it", async () => {
    await loadStage(page, { url: base });
    await page.locator(".lt-page .btn.p:has-text('New task')").first().click();
    const modal = page.locator(".lt-modal[role=dialog]").first();
    await modal.waitFor({ state: "visible", timeout: 5_000 });
    const before = requestCount;
    await modal.locator("button[type=submit]").first().click();
    await page.waitForTimeout(400);
    if (requestCount !== before) throw new Error("an empty Create sent a request");
    const invalid = await page.evaluate(() => Boolean(document.activeElement?.matches?.(":invalid")));
    if (!invalid) throw new Error("an empty Create did not focus the missing field");
    await page.locator(".lt-modal-hd button[aria-label]").first().click();
    await page.waitForTimeout(300);
    if ((await page.locator(".lt-modal[role=dialog]").count()) !== 0) throw new Error("Close left the New task modal open");
  });

  await check(page, viewport, "every scope tab is reachable and marks itself current", async () => {
    for (const scope of ["assigned_to_me", "assigned_by_me", "team_progress"]) {
      await loadStage(page, { url: base });
      const tab = page.locator(`.lt-page a[href*="scope=${scope}"]`).first();
      await tab.click();
      if ((await waitForParam(page, "scope", scope)).searchParams.get("scope") !== scope) throw new Error(`tab did not land on scope=${scope}`);
      const marked = await page.locator(`.lt-page a[href*="scope=${scope}"].on, .lt-page a[href*="scope=${scope}"][aria-current]`).count();
      if (!marked) throw new Error(`scope=${scope} tab is not marked current after selection`);
    }
  });
}

// ─────────────────────────────────────────────────────────────────────────────── plumbing

async function check(page, viewport, name, fn) {
  if (pageGone) throw new Error("the browser page closed mid-run");
  currentIssues = [];
  try {
    const detail = await fn();
    if (currentIssues.length) throw new Error(`page issues: ${currentIssues.join(" | ").slice(0, 400)}`);
    record({ viewport: viewport.label, stage: "scripted", element: name, how: "scripted", status: "pass", detail: detail ?? "" });
  } catch (error) {
    await shot(page, "scripted", name);
    record({ viewport: viewport.label, stage: "scripted", element: name, how: "scripted", status: "fail", detail: firstLine(error) });
  }
}

function record(entry) {
  results.push(entry);
  console.log(`${entry.status === "pass" ? "PASS" : "FAIL"} ${entry.viewport} ${entry.stage} :: ${entry.element}${entry.detail ? ` — ${entry.detail}` : ""}`);
}

async function shot(page, stageLabel, index) {
  const safe = `${viewportLabel}-${stageLabel}-${index}`.replaceAll(/[^a-z0-9]+/gi, "-").toLowerCase().slice(0, 120);
  await page.screenshot({ path: join(failureDir, `${safe}.png`), fullPage: true }).catch(() => undefined);
}

function firstLine(error) {
  return String(error?.message ?? error).split("\n")[0].slice(0, 400);
}

/**
 * A real task id, read from the board the way a reader finds one: the first card's link. It is
 * taken from "Raised by me" first so the detail panel carries Edit for this actor, and falls
 * back to the team board.
 */
async function resolveTask(page) {
  for (const scope of ["assigned_by_me", SCOPE]) {
    await loadStage(page, { url: `/tasks?scope=${scope}` });
    const href = await page.locator(".ltb-card").first().getAttribute("href").catch(() => null);
    const id = href ? new URL(href, appBaseUrl).searchParams.get("task") : null;
    if (id) return { id, scope };
  }
  throw new Error("no task card on the board to open the detail panel with");
}

function resolveToken() {
  const inline = process.env.GOATOS_SMOKE_TOKEN ?? process.env.GOATOS_BEARER_TOKEN ?? "";
  if (inline) return inline.trim();
  const file = process.env.GOATOS_SMOKE_TOKEN_FILE ?? "";
  if (file) return readFileSync(file, "utf8").trim();
  throw new Error("GOATOS_SMOKE_TOKEN (or GOATOS_SMOKE_TOKEN_FILE) is required: this harness drives the real desk as a signed-in actor");
}

/** Requests the page made on its own behalf. Dev-server chatter is not evidence of a control. */
function countsAsRequest(url) {
  const { pathname } = new URL(url);
  if (pathname.startsWith("/_next/")) return false;
  if (pathname === "/favicon.ico") return false;
  // Beacons the shell fires on its own schedule are not evidence that a control did anything.
  if (pathname === "/api/admin-web/performance-events" || pathname === "/api/ceo-ai/starters") return false;
  if (isExpectedOptionalLocalAuthResponse(url)) return false;
  return true;
}

function isExpectedOptionalLocalAuthResponse(url) {
  const pathname = new URL(url).pathname;
  return pathname === "/api/auth/firebase-config" || pathname === "/api/auth/session";
}

function isExpectedOptionalLocalAuthNoise(text) {
  return text.startsWith("Failed to load resource:");
}
