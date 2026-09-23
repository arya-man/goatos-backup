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
  await loc.waitFor({ state: "visible", timeout: 5_000 });
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
export async function assertFeaturesPresent(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p, reload, deployedSha }) {
  const entries = loadFeatureAssertions().filter((e) => e.route === routeName && (e.viewports ?? ["laptop", "mobile"]).includes(viewportLabel));
  if (entries.length === 0) return;
  const missing = [];
  const awaiting = [];
  const needsReview = [];
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
      if (message.startsWith(NEEDS_STEP_PREFIX)) {
        needsReview.push({ entry, why: message.slice(NEEDS_STEP_PREFIX.length) });
        console.log(`feature_assertion_needs_step=${viewportLabel}:${routeName}|${entry.sha}|${entry.title}|${message.slice(NEEDS_STEP_PREFIX.length, NEEDS_STEP_PREFIX.length + 80)}`);
        continue;
      }
      if (entry.status !== "data-dependent") missing.push({ entry, miss: { what: String(error?.message ?? error).split("\n")[0] } });
    }
  }
  console.log(`feature_assertions=${routeName}:${viewportLabel}:${entries.length - missing.length - awaiting.length}/${entries.length - awaiting.length}${awaiting.length ? ` awaiting_deploy=${awaiting.length}` : ""}`);
  for (const entry of awaiting) console.log(`feature_awaiting_deploy=${viewportLabel}:${routeName}|${entry.sha}|${entry.title}`);
  for (const m of missing) console.log(`feature_missing=${viewportLabel}:${routeName}|${m.entry.sha}|${m.entry.title}|${m.miss.what}`);
  // Checks nobody finished writing are reported separately and quietly: they say
  // nothing about whether the farm's screens work.
  if (needsReview.length > 0) {
    console.log(`feature_assertions_need_review=${routeName}:${viewportLabel}:${needsReview.length}`);
  }
  if (missing.length === 0) {
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
  throw new Error(`${routeName} ${viewportLabel} feature missing: ${missing.slice(0, 4).map(say).join("; ")}${missing.length > 4 ? ` (+${missing.length - 4} more)` : ""}`);
}
