// Per-commit feature assertions: one read-only check for every user-visible web
// feature or fix shipped since 2026-08-01, so a feature that silently disappears
// from production is reported with a red-boxed screenshot.
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { compareReadings } from "./reading-comparison.mjs";

const here = dirname(fileURLToPath(import.meta.url));
// Owned by vgoats/mesha-ops (dashboard-automation/tooling/feature-assertions.json); present only
// when that tooling is overlaid at tools/dashboard-automation. Absent => no assertions.
export const manifestPath = join(here, "../../../../tools/dashboard-automation/feature-assertions.json");

// Same write-guard as the overlay journeys: steps may only open, switch or reveal.
export const WRITE_WORDS = /\b(save|approve|reject|delete|remove|retire|submit|upload|download|export|assign|mark|confirm|create|add|publish|send|record|register|apply changes|sign out|log ?out)\b/i;

export function loadFeatureAssertions(path = manifestPath) {
  if (!existsSync(path)) return [];
  const all = JSON.parse(readFileSync(path, "utf8"));
  return all
    .filter((entry) => ["assert", "data-dependent", "mobile-only"].includes(entry.status))
    .map((entry) => (entry.status === "mobile-only" ? { ...entry, status: "assert", viewports: ["mobile"] } : entry));
}

// A feature merged after the deployed build simply is not on production yet:
// that is "awaiting deploy", not a broken feature, and must never page anyone.
const deployedCache = new Map();
export function isAwaitingDeploy(sha, deployedSha, repoRoot = join(here, "../../../..")) {
  if (!sha || !deployedSha) return false;
  const key = `${sha}|${deployedSha}`;
  if (deployedCache.has(key)) return deployedCache.get(key);
  const run = (args) => spawnSync("git", ["-C", repoRoot, ...args], { encoding: "utf8" });
  const known = run(["cat-file", "-e", `${sha}^{commit}`]).status === 0 && run(["cat-file", "-e", `${deployedSha}^{commit}`]).status === 0;
  // Unknown commits (shallow clone, unfetched build) must not be treated as awaiting deploy.
  const answer = known ? run(["merge-base", "--is-ancestor", sha, deployedSha]).status !== 0 : false;
  deployedCache.set(key, answer);
  return answer;
}

function locatorFor(page, target) {
  if (target.css) return page.locator(target.css);
  if (target.label) return page.getByLabel(target.label, { exact: false });
  if (target.text) return page.getByText(target.text, { exact: false });
  throw new Error(`assertion target needs css, label or text: ${JSON.stringify(target)}`);
}

/**
 * Does this expectation compare a VALUE, or only ask whether something is on the page?
 *
 * "Is it visible", "at least one of these", "this must not appear" and "the address contains"
 * all hold on a page whose figures are wrong: they prove the screen was reached, not that it is
 * right. An exact string, a comparison between two figures, a figure that must not move, and a
 * filter that must be carried into the dialog it opens can all be wrong while everything renders.
 */
export function isValueExpect(expect = {}) {
  return Boolean(expect.equals || expect.compare || expect.stable || expect.carriesIntoDialog);
}

/** The first number in a piece of text, commas ignored. */
export function numberIn(text) {
  if (text === null || text === undefined) return null;
  const match = String(text).replace(/,/g, "").match(/-?\d+(?:\.\d+)?/);
  return match ? Number(match[0]) : null;
}

/** What a control SHOWS: a select's chosen option, an input's value, otherwise its text. */
async function shownValue(loc) {
  return loc.evaluate((el) => {
    if (el instanceof HTMLSelectElement) return el.selectedOptions[0]?.textContent ?? "";
    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return el.value;
    return el.innerText ?? el.textContent ?? "";
  });
}

/** The text of an element, and the first number in it. */
async function readCell(page, target) {
  const loc = locatorFor(page, target).first();
  const shown = await loc.waitFor({ state: "visible", timeout: 5_000 }).then(
    () => shownValue(loc).catch(() => null),
    () => null,
  );
  if (shown === null) return { text: null, number: null };
  return { text: String(shown).trim(), number: numberIn(shown) };
}

/**
 * One side of a comparison, as a NUMBER.
 *
 *   (nothing)       the first number in the first match
 *   all: "sum"      every visible match's number, added up (a row with no number => unreadable)
 *   all: "count"    how many matches are visible
 *   ratio: {part, whole}, times?   part / whole (* times), e.g. a percentage
 *
 * A side that cannot be read returns number null; a comparison with a null side is reported as
 * "not visible", never as two figures agreeing.
 */
async function readSide(page, target) {
  if (target?.ratio) {
    const part = await readSide(page, target.ratio.part);
    const whole = await readSide(page, target.ratio.whole);
    if (part.number === null || whole.number === null || whole.number === 0) return { number: null, values: null, how: "ratio" };
    const number = (part.number / whole.number) * (target.times ?? 1);
    return { number, values: number, how: "ratio" };
  }
  if (target?.all === "count" || target?.all === "sum") {
    const loc = locatorFor(page, target);
    const total = await loc.count().catch(() => 0);
    const values = [];
    let carriedAFigure = false;
    for (let i = 0; i < total; i += 1) {
      const one = loc.nth(i);
      if (!(await one.isVisible().catch(() => false))) continue;
      const value = numberIn(await one.innerText().catch(() => null));
      if (target.all === "count") {
        if (value !== null) carriedAFigure = true;
        values.push(1);
      } else {
        // A row with no number in it is not a zero; treating it as one lets a sum drift below its total.
        if (value === null) return { number: null, values: null, how: "sum" };
        values.push(value);
      }
    }
    // Nothing matched is NOT a count of zero: two readings of nothing would agree.
    if (!values.length) return { number: null, values: null, how: target.all };
    const number = target.all === "count" ? values.length : values.reduce((n, v) => n + v, 0);
    return { number, values, carriedAFigure, how: target.all };
  }
  const { number } = await readCell(page, target);
  return { number, values: number, how: "first" };
}

/** Where an opened drawer or dialog lives. Overridable per expectation with `dialog: { css }`. */
export const DEFAULT_DIALOG_CSS = '[role="dialog"]:visible, dialog[open], [aria-modal="true"]:visible, aside.drawer.on';

/** Compare two shown values: "digits" (default) ignores separators and wording, "text" is exact after trim. */
export function sameShownValue(a, b, mode = "digits") {
  if (a === null || b === null || a === undefined || b === undefined) return false;
  if (mode === "text") return String(a).replace(/\s+/g, " ").trim() === String(b).replace(/\s+/g, " ").trim();
  const digits = (v) => String(v).replace(/\D+/g, " ").trim();
  const da = digits(a);
  // No digits at all (e.g. "Male"): fall back to case-insensitive text.
  if (!da) return String(a).trim().toLowerCase() === String(b).trim().toLowerCase();
  return da === digits(b);
}

/**
 * The opener of a dialog may carry a write-shaped word ("Download weights") ONLY when the markup
 * says it opens something rather than acting: aria-haspopup, aria-expanded or aria-controls. A
 * type=submit control, or anything without that proof, is refused.
 */
export function openerIsDisclosure({ text = "", hasPopup = null, expanded = null, controls = null, type = null } = {}) {
  if (String(type).toLowerCase() === "submit") return false;
  if (!WRITE_WORDS.test(text)) return true;
  return Boolean((hasPopup && hasPopup !== "false") || expanded !== null || controls);
}

/** Read-only filter change: a click on a filter control, or choosing an option in a <select>. */
async function runFilterStep(page, step) {
  if (step.select) {
    const label = step.select.label ?? step.select.text ?? step.select.css ?? "";
    if (WRITE_WORDS.test(label) || WRITE_WORDS.test(String(step.option ?? ""))) throw new Error(`refused write-shaped step "${label}"`);
    const loc = locatorFor(page, step.select).first();
    await loc.waitFor({ state: "visible", timeout: 5_000 });
    const isSelect = await loc.evaluate((el) => el instanceof HTMLSelectElement).catch(() => false);
    if (!isSelect) throw new Error(`filter step target is not a <select>: ${label}`);
    await loc.selectOption({ label: String(step.option) }).catch(() => loc.selectOption(String(step.option)));
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    return;
  }
  await runStep(page, step);
}

/**
 * Set a page filter AFTER load, open a dialog/drawer, and require the dialog to show the same
 * value the page now shows. Catches a dialog that was seeded from the first render (or the URL
 * at load) and never follows the filter the user changed.
 *
 * Read-only by construction: `set` steps may only click write-free filter controls or choose a
 * <select> option; `open` must be a disclosure (see openerIsDisclosure); nothing inside the
 * dialog is ever clicked, typed into or submitted - it is only read.
 */
async function checkCarriesIntoDialog(page, spec) {
  const { label = "the filter", set = [], open, page: pageTarget, inDialog, dialog, compare = "digits" } = spec;
  if (!open || !pageTarget || !inDialog) throw new Error("carriesIntoDialog needs open, page and inDialog targets");
  const before = await readCell(page, pageTarget);
  if (before.text === null) return { what: `not visible: ${pageTarget.text ?? pageTarget.label ?? pageTarget.css}`, loc: null };
  for (const step of set) await runFilterStep(page, step);
  const after = await readCell(page, pageTarget);
  if (after.text === null) return { what: `${label} disappeared from the page after it was changed`, loc: null };
  if (set.length && sameShownValue(before.text, after.text, compare)) {
    return { what: `changing ${label} did not change what the page shows ("${after.text}"), so the dialog could not be tested against a changed filter`, loc: locatorFor(page, pageTarget).first() };
  }

  const opener = locatorFor(page, open).first();
  await opener.waitFor({ state: "visible", timeout: 5_000 });
  const facts = await opener.evaluate((el) => ({
    text: `${el.innerText ?? ""} ${el.getAttribute("aria-label") ?? ""}`,
    hasPopup: el.getAttribute("aria-haspopup"),
    expanded: el.getAttribute("aria-expanded"),
    controls: el.getAttribute("aria-controls"),
    type: el.getAttribute("type"),
  }));
  if (!openerIsDisclosure(facts)) throw new Error(`refused write-shaped control "${facts.text.trim().slice(0, 40)}"`);
  await opener.click({ timeout: 5_000 });
  const dialogLoc = page.locator(dialog?.css ?? DEFAULT_DIALOG_CSS).last();
  const opened = await dialogLoc.waitFor({ state: "visible", timeout: 5_000 }).then(() => true, () => false);
  if (!opened) return { what: `${open.text ?? open.label ?? open.css} did not open a dialog`, loc: opener };
  const field = locatorFor(dialogLoc, inDialog).first();
  const inside = await field.waitFor({ state: "visible", timeout: 5_000 }).then(() => shownValue(field).catch(() => null), () => null);
  if (inside === null) return { what: `not visible in the dialog: ${inDialog.text ?? inDialog.label ?? inDialog.css}`, loc: dialogLoc };
  if (sameShownValue(after.text, inside, compare)) return null;
  return { what: `${label} on the page reads "${after.text}" but the dialog it opens reads "${String(inside).trim()}" — the dialog did not carry the filter the user set`, loc: field };
}

async function runStep(page, step) {
  if (step.select) return runFilterStep(page, step);
  const target = step.click;
  if (!target) return;
  const label = target.text ?? target.css ?? "";
  if (WRITE_WORDS.test(label)) throw new Error(`refused write-shaped step "${label}"`);
  const loc = locatorFor(page, target).first();
  await loc.waitFor({ state: "visible", timeout: 5_000 });
  const text = (await loc.innerText().catch(() => "")) + " " + ((await loc.getAttribute("aria-label").catch(() => "")) ?? "");
  if (WRITE_WORDS.test(text)) throw new Error(`refused write-shaped control "${text.trim().slice(0, 40)}"`);
  await loc.click({ timeout: 5_000 });
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
}

/** eq / lte / gte with a tolerance; null means the comparison held. */
export function compareVerdict(a, b, op = "eq", tolerance = 0, loc = null) {
  const ok = op === "lte" ? a <= b + tolerance
    : op === "gte" ? a + tolerance >= b
      : Math.abs(a - b) <= tolerance;
  if (ok) return null;
  const said = op === "lte" ? "must not be more than" : op === "gte" ? "must not be less than" : "must equal";
  return { what: `${a} ${said} ${b}`, loc };
}

async function checkExpect(page, expect) {
  if (expect.visible) {
    const loc = locatorFor(page, expect.visible).first();
    const ok = await loc.waitFor({ state: "visible", timeout: 5_000 }).then(() => true, () => false);
    return ok ? null : { what: `not visible: ${expect.visible.text ?? expect.visible.css}`, loc: null };
  }
  if (expect.absent) {
    const loc = locatorFor(page, expect.absent);
    const n = await loc.count();
    for (let i = 0; i < n; i += 1) {
      if (await loc.nth(i).isVisible().catch(() => false)) return { what: `should not appear: ${expect.absent.text ?? expect.absent.css}`, loc: loc.nth(i) };
    }
    return null;
  }
  if (expect.count) {
    const n = await page.locator(expect.count.css).count();
    if (expect.count.max !== undefined && n > expect.count.max) return { what: `expected at most ${expect.count.max} of ${expect.count.css}, found ${n}`, loc: null };
    const min = expect.count.min ?? (expect.count.max !== undefined ? 0 : 1);
    return n >= min ? null : { what: `expected at least ${min} of ${expect.count.css}, found ${n}`, loc: null };
  }
  if (expect.url) {
    return page.url().includes(expect.url.contains) ? null : { what: `address should contain ${expect.url.contains}`, loc: null };
  }
  // ------------------------------------------------------------ value checks: these can be WRONG
  if (expect.equals) {
    const { text } = await readCell(page, expect.equals);
    if (text === null) return { what: `not visible: ${expect.equals.text ?? expect.equals.label ?? expect.equals.css}`, loc: null };
    return text === String(expect.equals.is).trim() ? null : { what: `should read "${expect.equals.is}", reads "${text}"`, loc: locatorFor(page, expect.equals).first() };
  }
  if (expect.compare) {
    const { left, right, op = "eq", tolerance = 0 } = expect.compare;
    const a = await readSide(page, left);
    const b = await readSide(page, right);
    if (a.number === null || b.number === null) {
      const missing = a.number === null ? left : right;
      return { what: `not visible: ${missing.text ?? missing.label ?? missing.css ?? "a ratio side"}`, loc: null };
    }
    return compareVerdict(a.number, b.number, op, tolerance, left.ratio ? null : locatorFor(page, left).first());
  }
  if (expect.stable) {
    // Read a figure, do something that must not change it (page two, a sort), read it again.
    // A summary computed from the rows on screen passes every presence check and still reports a
    // different total on page two.
    const { target, through = [], label = "this figure" } = expect.stable;
    const before = await readSide(page, target);
    if (before.values === null) return { what: `not visible: ${target.text ?? target.label ?? target.css}`, loc: null };
    // A count of visible-but-empty boxes is not a reading of figures.
    if (target?.all === "count" && before.carriedAFigure === false) return { what: `not visible: ${target.text ?? target.css}`, loc: null };
    for (const step of through) await runStep(page, step);
    const after = await readSide(page, target);
    if (after.values === null) return { what: `${label} disappeared after the page changed`, loc: null };
    const verdict = compareReadings(before.values, after.values, { conditions: "deliberate-action", label, all: target?.all });
    if (verdict.agreed) return null;
    return { what: `${verdict.verdict} — a summary must describe the whole filter, not the rows on screen`, loc: locatorFor(page, target).first() };
  }
  if (expect.carriesIntoDialog) return checkCarriesIntoDialog(page, expect.carriesIntoDialog);
  if (expect.layout?.mode === "track-below-label-full-width") {
    const target = expect.layout;
    const row = page.locator(target.css).first();
    await row.waitFor({ state: "visible", timeout: 5_000 });
    const result = await row.evaluate((el, { labelCss, trackCss }) => {
      const label = el.querySelector(labelCss);
      const track = el.querySelector(trackCss);
      if (!label || !track) return { ok: false, reason: `missing ${!label ? labelCss : trackCss}` };
      const rowRect = el.getBoundingClientRect();
      const labelRect = label.getBoundingClientRect();
      const trackRect = track.getBoundingClientRect();
      const trackBelowLabel = trackRect.top >= labelRect.bottom - 1;
      const trackNearlyFullWidth = trackRect.left <= rowRect.left + 2 && trackRect.right >= rowRect.right - 2;
      return {
        ok: trackBelowLabel && trackNearlyFullWidth,
        reason: `row=${Math.round(rowRect.width)} labelBottom=${Math.round(labelRect.bottom)} trackTop=${Math.round(trackRect.top)} trackWidth=${Math.round(trackRect.width)}`,
      };
    }, { labelCss: target.labelCss ?? ".wbl", trackCss: target.trackCss ?? ".wbt" });
    return result.ok ? null : { what: `${target.css} expected track below label and full width (${result.reason})`, loc: row };
  }
  return null;
}

// Returns nothing when all pass; throws one readable error listing every missing feature.
// `entries` is optional (defaults to the overlaid mesha-ops manifest) so callers and tests can pass their own.
export async function assertFeaturesPresent(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p, reload, deployedSha, entries: given }) {
  const entries = (given ?? loadFeatureAssertions()).filter((e) => e.route === routeName && (e.viewports ?? ["laptop", "mobile"]).includes(viewportLabel));
  if (entries.length === 0) return;
  const missing = [];
  const awaiting = [];
  // Earlier checks (overlays, safe clicks) leave drawers open; start from a clean page.
  if (reload) await reload().catch(() => {});
  // Order no-click checks first, then reload before each clicking check so every check starts clean.
  entries.sort((a, b) => (a.steps?.length ? 1 : 0) - (b.steps?.length ? 1 : 0));
  for (const entry of entries) {
    if (entry.needsRoute) { console.log(`feature_assertion_skip=${routeName}:${viewportLabel}:${entry.sha}:needs route ${entry.needsRoute}`); continue; }
    if (deployedSha && isAwaitingDeploy(entry.sha, deployedSha)) { awaiting.push(entry); continue; }
    try {
      // One attempt = fresh load, replay the steps, check the expects.
      const attempt = async () => {
        if (entry.steps?.length && reload) await reload();
        for (const step of entry.steps ?? []) await runStep(page, step);
        for (const expect of entry.expect ?? []) {
          const miss = await checkExpect(page, expect);
          if (miss) {
            // Data-dependent features render only when the page has rows: absence is not a failure,
            // but something that must NOT appear is still a failure.
            if (entry.status === "data-dependent" && /^(not visible|expected at least)/.test(miss.what)) return null;
            return miss;
          }
        }
        return null;
      };
      let miss = await attempt();
      // The first click after a fresh load can land before React has attached its handler, so the
      // control is visible, the click is a no-op, and the view never changes. That looked like a
      // missing feature (acbb15186 on laptop, passing on mobile, with every sibling check on the
      // same route green). Replay a clicking entry once before calling it broken: a feature that is
      // genuinely gone fails both times, and the retry is only paid on a failure.
      if (miss && entry.steps?.length && reload) miss = await attempt();
      if (miss) {
        missing.push({ entry, miss });
        if (miss.loc) await miss.loc.evaluate((el) => el.setAttribute("data-smoke-issue", "feature")).catch(() => {});
      }
    } catch (error) {
      const message = String(error?.message ?? error);
      // The write guard stopping a step is a safety skip, not a missing feature.
      if (message.startsWith("refused ")) { console.log(`feature_assertion_skip=${routeName}:${viewportLabel}:${entry.sha}:${message.slice(0, 80)}`); continue; }
      if (entry.status !== "data-dependent") missing.push({ entry, miss: { what: String(error?.message ?? error).split("\n")[0] } });
    }
  }
  console.log(`feature_assertions=${routeName}:${viewportLabel}:${entries.length - missing.length - awaiting.length}/${entries.length - awaiting.length}${awaiting.length ? ` awaiting_deploy=${awaiting.length}` : ""}`);
  for (const entry of awaiting) console.log(`feature_awaiting_deploy=${viewportLabel}:${routeName}|${entry.sha}|${entry.title}`);
  for (const m of missing) console.log(`feature_missing=${viewportLabel}:${routeName}|${m.entry.sha}|${m.entry.title}|${m.miss.what}`);
  if (missing.length === 0) return;
  await page.addStyleTag({ content: "[data-smoke-issue]{outline:3px solid #e11d48 !important;outline-offset:1px}" }).catch(() => {});
  const shot = join(screenshotDir, `${viewportLabel}-${routeName}-feature-missing.png`);
  await page.screenshot({ path: shot, fullPage: false }).catch(() => {});
  console.log(`screenshot_path=${relativeToRepo(shot)}`);
  // Name WHICH expectation failed (a value check's sentence says what it read), not just the title.
  throw new Error(`${routeName} ${viewportLabel} feature missing: ${missing.slice(0, 4).map((m) => `${m.entry.title} [${m.entry.sha}]${m.miss?.what ? ` — ${m.miss.what}` : ""}`).join("; ")}${missing.length > 4 ? ` (+${missing.length - 4} more)` : ""}`);
}
