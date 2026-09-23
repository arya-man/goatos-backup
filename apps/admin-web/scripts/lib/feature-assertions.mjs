// Per-commit feature assertions: one read-only check for every user-visible web
// feature or fix shipped since 2026-08-01, so a feature that silently disappears
// from production is reported with a red-boxed screenshot.
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const manifestPath = join(here, "../../../../tools/dashboard-automation/feature-assertions.json");

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
  if (target.text) return page.getByText(target.text, { exact: false });
  throw new Error(`assertion target needs css or text: ${JSON.stringify(target)}`);
}

/** A step written as a sentence instead of a target the browser can find. */
export const NEEDS_STEP_PREFIX = "needs-step: ";
/** The control a step has to click is not on the page: that is the product, not the harness. */
export const STEP_TARGET_PREFIX = "step-target: ";

/**
 * Does this expectation compare a VALUE, or only ask whether something is on the page?
 *
 * "Is it visible", "at least one of these", "this must not appear" and "the address contains"
 * all hold on a page whose figures are wrong, so they are smoke: they prove the screen was
 * reached, not that it is right. An exact string and a comparison between two figures can be
 * wrong while everything still renders, so they are the ones that can catch a regression.
 *
 * check-coverage-since-aug1.mjs imports this so the ledger and the runner cannot drift apart.
 */
export function isValueExpect(expect = {}) {
  if (expect.equals) return true;
  if (expect.compare) return true;
  // A figure that must not change when the page does is a value claim too: it
  // fails on a page that renders perfectly and reports a different total on
  // page two, which is the capped read-time rollup this repo bans by name.
  if (expect.stable) return true;
  return false;
}

/** The first number in a piece of text, commas ignored. */
export function numberIn(text) {
  if (text === null || text === undefined) return null;
  const match = String(text).replace(/,/g, "").match(/-?\d+(?:\.\d+)?/);
  return match ? Number(match[0]) : null;
}

/** The text of an element, and the first number in it. */
async function readCell(page, target) {
  const loc = locatorFor(page, target).first();
  const shown = await loc.waitFor({ state: "visible", timeout: 5_000 }).then(
    () => loc.innerText().catch(() => null),
    () => null,
  );
  if (shown === null) return { text: null, number: null };
  return { text: String(shown).trim(), number: numberIn(shown) };
}

/**
 * One side of a comparison, as a NUMBER.
 *
 * A single cell was the only thing the engine could read, which is why every
 * invariant worth asserting on this product was inexpressible: the ones that
 * actually catch wrong figures are "this total equals the sum of those rows"
 * and "this count equals the number of rows listed".
 *
 *   (nothing)      the first number in the first match          — as before
 *   all: "sum"     every match's number, added up
 *   all: "count"   how many matches are on the screen
 *
 * A side that cannot be read returns null, and a comparison with a null side is
 * NOT a failure — it is "could not be judged". A page that has not drawn its
 * rows yet must never be accused of disagreeing with itself.
 */
async function readSide(page, target) {
  // A side may be a RATIO of two other sides — "the ring says 64%, and 64% is
  // what 160 out of 250 is". Percentages are the commonest figure on these
  // screens and none of them could be checked against the numbers they are
  // derived from, because a side could only ever be one cell.
  if (target?.ratio) {
    const part = await readSide(page, target.ratio.part);
    const whole = await readSide(page, target.ratio.whole);
    if (part.number === null || whole.number === null) return { number: null, how: "ratio" };
    // Nothing out of nothing is not zero per cent, it is not a question. A page
    // with no animals in the drive must not be accused of a wrong percentage.
    if (whole.number === 0) return { number: null, how: "ratio", why: "there is nothing to take a share of" };
    return { number: (part.number / whole.number) * (target.times ?? 1), how: "ratio" };
  }
  if (target?.all === "count") {
    const loc = locatorFor(page, target);
    const total = await loc.count().catch(() => null);
    if (total === null) return { number: null, how: "count" };
    let seen = 0;
    for (let i = 0; i < total; i += 1) {
      if (await loc.nth(i).isVisible().catch(() => false)) seen += 1;
    }
    return { number: seen, how: "count" };
  }
  if (target?.all === "sum") {
    const loc = locatorFor(page, target);
    const total = await loc.count().catch(() => null);
    if (!total) return { number: null, how: "sum" };
    let sum = 0;
    let seen = 0;
    for (let i = 0; i < total; i += 1) {
      const one = loc.nth(i);
      if (!(await one.isVisible().catch(() => false))) continue;
      const value = numberIn(await one.innerText().catch(() => null));
      // A row with no number in it is not a zero. Treating it as one is how a
      // sum quietly drifts below the total it is checked against.
      if (value === null) return { number: null, how: "sum", why: "one of the rows has no figure in it" };
      sum += value;
      seen += 1;
    }
    return { number: seen ? sum : null, how: "sum" };
  }
  const { number } = await readCell(page, target);
  return { number, how: "first" };
}

async function runStep(page, step) {
  const target = step.click;
  if (!target) return;
  // A step like { click: "the New task button to open the compose modal" } is a note to a
  // human, not something a browser can click: locatorFor would throw and the entry would be
  // reported as a MISSING FEATURE on every run, for ever, while the button sits on the page.
  // An assertion nobody finished writing is a check that needs review, not a broken product.
  if (typeof target === "string" || (!target.css && !target.text)) {
    throw new Error(`${NEEDS_STEP_PREFIX}${typeof target === "string" ? target : JSON.stringify(target)}`);
  }
  const label = target.text ?? target.css ?? "";
  if (WRITE_WORDS.test(label)) throw new Error(`refused write-shaped step "${label}"`);
  const loc = locatorFor(page, target).first();
  // A control that never appears is a product finding, not a harness fault: say which it is,
  // so the two never get mixed up downstream.
  await loc.waitFor({ state: "visible", timeout: 5_000 }).catch(() => {
    throw new Error(`${STEP_TARGET_PREFIX}${label || JSON.stringify(target)}`);
  });
  const text = (await loc.innerText().catch(() => "")) + " " + ((await loc.getAttribute("aria-label").catch(() => "")) ?? "");
  if (WRITE_WORDS.test(text)) throw new Error(`refused write-shaped control "${text.trim().slice(0, 40)}"`);
  await loc.click({ timeout: 5_000 });
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
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
  if (expect.stable) {
    // Read a figure, do something that must not change it, read it again.
    //
    // "Pagination changes rows only, never summary truth." A summary computed
    // from the rows currently on screen passes every presence check ever
    // written and still reports a different total on page two.
    const { target, through = [], label = "this figure" } = expect.stable;
    const before = await readSide(page, target);
    if (before.number === null) return { what: `not visible: ${target.text ?? target.css}`, loc: null };
    for (const step of through) await runStep(page, step);
    const after = await readSide(page, target);
    if (after.number === null) {
      return { what: `${label} disappeared after the page changed`, loc: null };
    }
    return before.number === after.number
      ? null
      : { what: `${label} reads ${before.number}, then ${after.number} after the page changed — a summary must describe the whole filter, not the rows on screen`, loc: locatorFor(page, target).first() };
  }

  // ------------------------------------------------------------------ expectations that can be wrong
  // Everything above holds on a page whose figures are nonsense. These two do not.
  if (expect.equals) {
    const { text } = await readCell(page, expect.equals);
    if (text === null) return { what: `not visible: ${expect.equals.text ?? expect.equals.css}`, loc: null };
    return text === String(expect.equals.is).trim() ? null : { what: `should read "${expect.equals.is}", reads "${text}"`, loc: locatorFor(page, expect.equals).first() };
  }
  if (expect.compare) {
    const { left, right, op = "eq", tolerance = 0 } = expect.compare;
    const a = await readSide(page, left);
    const b = await readSide(page, right);
    if (a.number === null || b.number === null) {
      const missing = a.number === null ? left : right;
      return { what: `not visible: ${missing.text ?? missing.css}`, loc: null };
    }
    const ok = op === "lte" ? a.number <= b.number + tolerance
      : op === "gte" ? a.number + tolerance >= b.number
        : Math.abs(a.number - b.number) <= tolerance;
    const said = op === "lte" ? "must not be more than" : op === "gte" ? "must not be less than" : "must equal";
    return ok ? null : { what: `${a.number} ${said} ${b.number}`, loc: locatorFor(page, left).first() };
  }
  return null;
}

/**
 * Did the screen have the rows this data-dependent check is drawn from?
 *
 * Only an entry that says how to tell can be judged. Guessing - "the page has some rows, so the
 * feature's rows must be there too" - is how a correct page gets accused, and a check that fires
 * on a correct page is worse than no check. So without a `dataProbe` the answer is "cannot tell",
 * which is not-attempted: never a pass, never an accusation.
 */
export async function probeData(page, entry) {
  const probe = entry.dataProbe;
  if (!probe) {
    return { present: false, why: "this check only holds when the screen has particular rows, and the check does not say how to tell whether it does, so it was never put to the test" };
  }
  const min = probe.min ?? 1;
  const loc = locatorFor(page, probe);
  const total = await loc.count().catch(() => 0);
  let seen = 0;
  for (let i = 0; i < total && seen < min; i += 1) {
    if (await loc.nth(i).isVisible().catch(() => false)) seen += 1;
  }
  if (seen >= min) return { present: true, why: "" };
  return { present: false, why: "the screen had none of the rows this feature is drawn from, so there was nothing to judge" };
}

/** A miss that only says "it is not on the page" - the shape a screen with no rows produces. */
const ABSENCE_MISS = /^(not visible|expected at least)/;

// Returns nothing when all pass; throws one readable error listing every missing feature.
//
// THREE OUTCOMES, NEVER TWO. A data-dependent entry used to report green whenever its target was
// not on the page, on the theory that the rows may simply not be there today. Measured against
// the weighing and vaccination screens that swallowed 178 of 276 assertions on a page with
// nothing drawn on it at all, including a plan editor reporting 17 of 17 passed against a blank
// screen. A check that did not run must never render a verdict:
//
//   the screen had its rows and the check held      -> pass
//   the screen had its rows and the check failed    -> fail, with a red-boxed screenshot
//   the screen had no rows to judge                 -> not-attempted, never a pass
//
// and a throw from the assertion machinery itself is a harness fault, reported as one and never
// swallowed - for every status, not just the ones we happened to think of.
export async function assertFeaturesPresent(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p, reload, deployedSha, entries: given }) {
  const entries = (given ?? loadFeatureAssertions()).filter((e) => e.route === routeName && (e.viewports ?? ["laptop", "mobile"]).includes(viewportLabel));
  if (entries.length === 0) {
    // Silence is a verdict nobody earned. This page at this width was reloaded and
    // nothing was asserted about it — say so, so the gap is countable instead of
    // invisible in a run that otherwise reads clean.
    console.log(`feature_assertions_none=${routeName}:${viewportLabel}:no reload check is written for this page at this width`);
    return;
  }
  const missing = [];
  const awaiting = [];
  const needsReview = [];
  const notAttempted = [];
  const harnessFaults = [];
  // Entries this run stepped over: a route it was not on, or a step the write guard refused.
  // They are not passes either, so they come out of the denominator rather than inflating it.
  let skipped = 0;
  // Earlier checks (overlays, safe clicks) leave drawers open; start from a clean page.
  if (reload) await reload().catch(() => {});
  // Order no-click checks first, then reload before each clicking check so every check starts clean.
  entries.sort((a, b) => (a.steps?.length ? 1 : 0) - (b.steps?.length ? 1 : 0));
  for (const entry of entries) {
    if (entry.needsRoute) { skipped += 1; console.log(`feature_assertion_skip=${routeName}:${viewportLabel}:${entry.sha}:needs route ${entry.needsRoute}`); continue; }
    if (deployedSha && isAwaitingDeploy(entry.sha, deployedSha)) { awaiting.push(entry); continue; }
    try {
      // One attempt = fresh load, replay the steps, check the expects.
      // It answers { miss } (the product is wrong), { skipped } (nothing to judge), or null (pass).
      const attempt = async () => {
        if (entry.steps?.length && reload) await reload();
        for (const step of entry.steps ?? []) await runStep(page, step);
        for (const expect of entry.expect ?? []) {
          const miss = await checkExpect(page, expect);
          if (!miss) continue;
          // A data-dependent feature draws only when the screen has its rows. "It is not there"
          // is then either a real regression or a day with nothing to draw - and which one it is
          // decides between a finding and not-attempted. It is NEVER a pass. Something that must
          // NOT appear is a failure whatever the screen holds.
          if (entry.status === "data-dependent" && ABSENCE_MISS.test(miss.what)) {
            const data = await probeData(page, entry);
            return data.present ? { miss } : { skipped: data.why };
          }
          return { miss };
        }
        return null;
      };
      let result = await attempt();
      // The first click after a fresh load can land before React has attached its handler, so the
      // control is visible, the click is a no-op, and the view never changes. That looked like a
      // missing feature (acbb15186 on laptop, passing on mobile, with every sibling check on the
      // same route green). Replay a clicking entry once before calling it broken: a feature that is
      // genuinely gone fails both times, and the retry is only paid on a failure.
      if (result?.miss && entry.steps?.length && reload) result = await attempt();
      if (result?.skipped) {
        notAttempted.push({ entry, why: result.skipped });
      } else if (result?.miss) {
        missing.push({ entry, miss: result.miss });
        if (result.miss.loc) await result.miss.loc.evaluate((el) => el.setAttribute("data-smoke-issue", "feature")).catch(() => {});
      }
    } catch (error) {
      const message = String(error?.message ?? error);
      // The write guard stopping a step is a safety skip, not a missing feature.
      if (message.startsWith("refused ")) { skipped += 1; console.log(`feature_assertion_skip=${routeName}:${viewportLabel}:${entry.sha}:${message.slice(0, 80)}`); continue; }
      if (message.startsWith(NEEDS_STEP_PREFIX)) {
        needsReview.push({ entry, why: message.slice(NEEDS_STEP_PREFIX.length) });
        console.log(`feature_assertion_needs_step=${viewportLabel}:${routeName}|${entry.sha}|${entry.title}|${message.slice(NEEDS_STEP_PREFIX.length, NEEDS_STEP_PREFIX.length + 80)}`);
        continue;
      }
      if (message.startsWith(STEP_TARGET_PREFIX)) {
        // The control this check has to click is not on the screen. For a data-dependent entry
        // that can be a screen with nothing on it, so it is judged the same way as a missing
        // target: rows or no rows, never a silent pass.
        const what = `the control this check has to open is not on the page: ${message.slice(STEP_TARGET_PREFIX.length)}`;
        if (entry.status === "data-dependent") {
          const data = await probeData(page, entry).catch(() => ({ present: false, why: "the screen could not be read, so nothing was proved either way" }));
          if (data.present) missing.push({ entry, miss: { what } });
          else notAttempted.push({ entry, why: data.why });
        } else {
          missing.push({ entry, miss: { what } });
        }
        continue;
      }
      // Anything else is the machinery failing, not the farm's screen. A swallowed error is a
      // verdict nobody earned: report it as a harness fault, for every status.
      harnessFaults.push({ entry, why: message.split("\n")[0] });
    }
  }
  // Only entries this run actually put to the test are in the denominator. A check that did not
  // run must never render a verdict - and it must not quietly pad the score either.
  const judged = entries.length - awaiting.length - notAttempted.length - harnessFaults.length - needsReview.length - skipped;
  console.log(`feature_assertions=${routeName}:${viewportLabel}:${judged - missing.length}/${judged}`
    + `${notAttempted.length ? ` not_attempted=${notAttempted.length}` : ""}`
    + `${harnessFaults.length ? ` harness_faults=${harnessFaults.length}` : ""}`
    + `${awaiting.length ? ` awaiting_deploy=${awaiting.length}` : ""}`);
  for (const entry of awaiting) console.log(`feature_awaiting_deploy=${viewportLabel}:${routeName}|${entry.sha}|${entry.title}`);
  for (const m of missing) console.log(`feature_missing=${viewportLabel}:${routeName}|${m.entry.sha}|${m.entry.title}|${m.miss.what}`);
  for (const n of notAttempted) console.log(`feature_not_attempted=${viewportLabel}:${routeName}|${n.entry.sha}|${n.entry.title}|${n.why}`);
  for (const h of harnessFaults) console.log(`feature_assertion_harness_fault=${viewportLabel}:${routeName}|${h.entry.sha}|${h.entry.title}|${h.why}`);
  // Checks nobody finished writing are reported separately and quietly: they say
  // nothing about whether the farm's screens work.
  if (needsReview.length > 0) {
    console.log(`feature_assertions_need_review=${routeName}:${viewportLabel}:${needsReview.length}`);
  }
  if (missing.length === 0) {
    // A fault in the machinery is reported as a fault in the machinery. It is not a finding about
    // the farm's screens, and it is not silence either.
    if (harnessFaults.length > 0) {
      throw new Error(
        `${routeName} ${viewportLabel} harness fault, ${harnessFaults.length} check(s) could not be run: ${harnessFaults
          .slice(0, 3)
          .map((h) => `${h.entry.title} (${h.why})`)
          .join("; ")}`,
      );
    }
    if (needsReview.length > 0) {
      throw new Error(
        `${routeName} ${viewportLabel} ${needsReview.length} assertion(s) need review: ${needsReview
          .slice(0, 3)
          .map((r) => r.entry.title)
          .join("; ")}`,
      );
    }
    return;
  }
  await page.addStyleTag({ content: "[data-smoke-issue]{outline:3px solid #e11d48 !important;outline-offset:1px}" }).catch(() => {});
  const shot = join(screenshotDir, `${viewportLabel}-${routeName}-feature-missing.png`);
  await page.screenshot({ path: shot, fullPage: false }).catch(() => {});
  console.log(`screenshot_path=${relativeToRepo(shot)}`);
  // Say WHICH expectation failed, not just the entry's title.
  //
  // An entry can assert several things at once. "Feed Config has an add feed item control"
  // covers both the Add feed item button and the Feed items heading, so when only the heading
  // was gone the sweep still reported the button as missing — and the screenshot showed the
  // button, several times over. Naming the part that actually failed is the difference between
  // a finding someone can act on and one they dismiss.
  const say = (m) => {
    const what = String(m.miss?.what ?? "");
    const quoted = what.match(/^(?:not visible|should not appear): (.+)$/);
    if (quoted) return `${m.entry.title} — "${quoted[1]}" is not on the page`;
    if (what) return `${m.entry.title} — ${what}`;
    return m.entry.title;
  };
  // A harness fault alongside real findings is still said out loud: it is the difference between
  // "these four things are wrong" and "these four things are wrong and one check never ran".
  const alsoFaulted = harnessFaults.length ? `; and ${harnessFaults.length} check(s) could not be run at all` : "";
  throw new Error(`${routeName} ${viewportLabel} feature missing: ${missing.slice(0, 4).map(say).join("; ")}${missing.length > 4 ? ` (+${missing.length - 4} more)` : ""}${alsoFaulted}`);
}


// ---------------------------------------------------------------------------
// Coverage of the reload checks, as a fraction with a reason on every gap
// ---------------------------------------------------------------------------
//
// The runner printed `feature_assertions=route:viewport:6/6` per page — a list of
// hits. Six of six is a fine number and says nothing about the 115 route/viewport
// pairs that had no reload check at all, which is the half of the surface that made
// a sweep read clean while covering none of it.
//
// So the number this exports is a FRACTION over every route at every viewport, and
// every pair outside the numerator carries its own sentence. Pure and fast: no
// browser, no network, so it can run on the default path.

/** Statuses `loadFeatureAssertions` refuses to run, and why each one cannot be judged. */
export const UNRUNNABLE_STATUS_REASONS = Object.freeze({
  "screenshot-only": "its evidence is a screenshot a person looks at, not something the browser can decide",
  "not-visually-assertable": "what it changed leaves no mark on the screen this check could find",
  "not-read-only": "proving it would mean pressing a control that writes, which a production sweep may not do",
  "needs-assertion": "nobody has finished writing what this check should look for",
  superseded: "a later commit replaced what this one shipped",
  rejected: "it was looked at and judged not worth a check",
  "console-check": "it is judged from the browser console, not from the page",
});

/**
 * @param {object} args
 * @param {Array<{name:string}>} args.routes every route the sweep visits
 * @param {string[]} args.viewports every width it visits them at
 * @param {Array} args.runnable entries the runner will execute (loadFeatureAssertions())
 * @param {Array} args.manifest every entry in the manifest, runnable or not
 */
export function reloadCoverage({ routes, viewports, runnable, manifest }) {
  const routeNames = new Set(routes.map((r) => r.name));
  const byPair = new Map();
  for (const entry of runnable) {
    for (const viewport of entry.viewports ?? ["laptop", "mobile"]) {
      const key = `${viewport}:${entry.route}`;
      if (!byPair.has(key)) byPair.set(key, []);
      byPair.get(key).push(entry);
    }
  }
  // An entry aimed at a page the sweep never opens can never run, and nothing in a
  // run mentions it. It is not a check; it is a check-shaped hole.
  //
  // An entry with NO route is a different thing and must not be counted as one: it was
  // never aimed at a page in the first place. Folding the two together produced 42
  // "can never run" entries that were all just unrouted — noise, and the kind that
  // makes a real orphan invisible among them.
  const unreachableEntries = manifest
    .filter((e) => e.route && !routeNames.has(e.route))
    .map((e) => ({ sha: e.sha, route: e.route, why: `this check is written for "${e.route}", which is not a page the sweep opens, so it can never run` }));
  const unroutedEntries = manifest
    .filter((e) => !e.route)
    .map((e) => ({ sha: e.sha, status: e.status, why: "this entry names no page, so there is nowhere to run it" }));
  // Same for a width the sweep does not visit.
  const unreachableViewports = runnable
    .flatMap((e) => (e.viewports ?? ["laptop", "mobile"]).filter((v) => !viewports.includes(v)).map((v) => ({ sha: e.sha, route: e.route, why: `this check is written for the ${v} width, which this sweep does not visit` })));

  const covered = [];
  const gaps = [];
  for (const viewport of viewports) {
    for (const route of routes) {
      const key = `${viewport}:${route.name}`;
      if (byPair.has(key)) { covered.push(key); continue; }
      // Why is this pair empty? Answer it for THIS page, never in a group.
      const here = manifest.filter((e) => e.route === route.name);
      if (here.length === 0) {
        gaps.push({ route: route.name, viewport, why: `nothing shipped on "${route.name}" since 2026-08-01 has a check written for it, so this page at the ${viewport} width is not checked` });
        continue;
      }
      const otherWidth = runnable.filter((e) => e.route === route.name);
      if (otherWidth.length > 0) {
        const widths = [...new Set(otherWidth.flatMap((e) => e.viewports ?? ["laptop", "mobile"]))];
        gaps.push({ route: route.name, viewport, why: `"${route.name}" has ${otherWidth.length} check(s), all written for the ${widths.join(" and ")} width only; nobody has said what the ${viewport} width should show, and widening them without knowing would accuse a correct page` });
        continue;
      }
      const statuses = [...new Set(here.map((e) => e.status))];
      gaps.push({
        route: route.name,
        viewport,
        why: `"${route.name}" has ${here.length} entr(y/ies) and none is runnable at the ${viewport} width: ${statuses.map((st) => `${st} — ${UNRUNNABLE_STATUS_REASONS[st] ?? "no reason is recorded for this status"}`).join("; ")}`,
      });
    }
  }
  const total = routes.length * viewports.length;
  return {
    pairsExpected: total,
    pairsWithACheck: covered.length,
    fraction: `${covered.length}/${total} page/width pairs carry a reload check`,
    gaps,
    unreachableEntries,
    unroutedEntries,
    unreachableViewports,
  };
}
