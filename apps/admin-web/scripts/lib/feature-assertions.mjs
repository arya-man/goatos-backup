// Per-commit feature assertions: one read-only check for every user-visible web
// feature or fix shipped since 2026-08-01, so a feature that silently disappears
// from production is reported with a red-boxed screenshot.
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
  return all.filter((entry) => entry.status === "assert" || entry.status === "data-dependent");
}

function locatorFor(page, target) {
  if (target.css) return page.locator(target.css);
  if (target.text) return page.getByText(target.text, { exact: false });
  throw new Error(`assertion target needs css or text: ${JSON.stringify(target)}`);
}

async function runStep(page, step) {
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
    return n >= (expect.count.min ?? 1) ? null : { what: `expected at least ${expect.count.min ?? 1} of ${expect.count.css}, found ${n}`, loc: null };
  }
  return null;
}

// Returns nothing when all pass; throws one readable error listing every missing feature.
export async function assertFeaturesPresent(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p, reload }) {
  const entries = loadFeatureAssertions().filter((e) => e.route === routeName && (e.viewports ?? ["laptop", "mobile"]).includes(viewportLabel));
  if (entries.length === 0) return;
  const missing = [];
  for (const entry of entries) {
    try {
      if (entry.steps?.length && reload) await reload();
      for (const step of entry.steps ?? []) await runStep(page, step);
      for (const expect of entry.expect ?? []) {
        const miss = await checkExpect(page, expect);
        if (miss) {
          if (entry.status === "data-dependent" && miss.what.startsWith("not visible")) break;
          missing.push({ entry, miss });
          if (miss.loc) await miss.loc.evaluate((el) => el.setAttribute("data-smoke-issue", "feature")).catch(() => {});
          break;
        }
      }
    } catch (error) {
      const message = String(error?.message ?? error);
      // The write guard stopping a step is a safety skip, not a missing feature.
      if (message.startsWith("refused ")) { console.log(`feature_assertion_skip=${routeName}:${viewportLabel}:${entry.sha}:${message.slice(0, 80)}`); continue; }
      if (entry.status !== "data-dependent") missing.push({ entry, miss: { what: String(error?.message ?? error).split("\n")[0] } });
    }
  }
  console.log(`feature_assertions=${routeName}:${viewportLabel}:${entries.length - missing.length}/${entries.length}`);
  if (missing.length === 0) return;
  await page.addStyleTag({ content: "[data-smoke-issue]{outline:3px solid #e11d48 !important;outline-offset:1px}" }).catch(() => {});
  const shot = join(screenshotDir, `${viewportLabel}-${routeName}-feature-missing.png`);
  await page.screenshot({ path: shot, fullPage: false }).catch(() => {});
  console.log(`screenshot_path=${relativeToRepo(shot)}`);
  throw new Error(`${routeName} ${viewportLabel} feature missing: ${missing.slice(0, 4).map((m) => `${m.entry.title} [${m.entry.sha}]`).join("; ")}${missing.length > 4 ? ` (+${missing.length - 4} more)` : ""}`);
}
