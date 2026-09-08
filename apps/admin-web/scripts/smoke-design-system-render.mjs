// Read-only render check for the Mesha design system.
//
// The static guard (scripts/check-design-system.mjs) reads source and cannot see
// LAYOUT. A change to type, tracking or case can still clip text inside a fixed
// cell and ship a page that returns HTTP 200 and renders no error -- that is
// exactly how "Verification pending" shipped clipped in a 175px Control Tower
// cell. This walks the real pages instead.
//
// For every route it waits for document.fonts.ready (so it measures the real
// faces, not the fallback), then asserts: no 500 body, no error boundary, no
// clipped control text, and no horizontal page overflow. It screenshots each
// route for eyeballing.
//
// It NEVER clicks a submit control, so unlike smoke-visual-live.mjs it cannot
// mutate the target database and is safe to point at a shared or OCI-backed
// stack. Usage:
//   ROUTES=/,/action-center APP=http://127.0.0.1:3400 OUT=./shots \
//     node scripts/smoke-design-system-render.mjs
import { chromium } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const BASE = process.env.APP ?? "http://127.0.0.1:3400";
const OUT = process.env.OUT ?? "./shots";
mkdirSync(OUT, { recursive: true });
const routes = process.env.ROUTES.split(",").map(r => r.trim()).filter(Boolean);

const browser = await chromium.launch({ channel: "chrome" });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1 });
const page = await ctx.newPage();
const results = [];

for (const route of routes) {
  const url = `${BASE}${route}${route.includes("?") ? "&" : "?"}scope_mode=company`;
  const name = (route === "/" ? "home" : route.replace(/^\//, "").replace(/\//g, "-"));
  let status = "OK", detail = "";
  try {
    const resp = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 60_000 });
    await page.waitForLoadState("networkidle", { timeout: 15_000 }).catch(() => {});
    await page.evaluate(() => document.fonts.ready).catch(() => {});
    await page.waitForTimeout(600);

    const http = resp?.status() ?? 0;
    const body = await page.locator("body").innerText().catch(() => "");
    if (/Internal Server Error/i.test(body)) { status = "FAIL"; detail = "500 page"; }
    else if (/Something went wrong|This screen failed to render/i.test(body)) { status = "FAIL"; detail = "error boundary"; }
    else if (http >= 400) { status = "FAIL"; detail = `http ${http}`; }
    else {
      const probs = await page.evaluate(() => {
        const out = { clipped: [], overflow: document.documentElement.scrollWidth > window.innerWidth + 2 };
        for (const el of document.querySelectorAll("a,button,.tag,.chip,.btn,.kpi .val")) {
          const r = el.getBoundingClientRect();
          if (r.width === 0 || r.height === 0) continue;
          if (el.scrollWidth > el.clientWidth + 2 && getComputedStyle(el).overflow !== "auto")
            out.clipped.push((el.textContent || "").trim().slice(0, 32) + ` [${el.clientWidth}<${el.scrollWidth}]`);
        }
        return out;
      });
      if (probs.clipped.length) { status = "WARN"; detail = `clipped: ${probs.clipped.slice(0, 3).join(" | ")}`; }
      if (probs.overflow) { status = "WARN"; detail += (detail ? "; " : "") + "page scrolls sideways"; }
    }
    await page.screenshot({ path: join(OUT, `${name}.png`), fullPage: false });
  } catch (e) {
    status = "FAIL"; detail = String(e.message || e).split("\n")[0].slice(0, 120);
    await page.screenshot({ path: join(OUT, `${name}.png`) }).catch(() => {});
  }
  results.push({ route, status, detail });
  console.log(`${status.padEnd(4)} ${route}${detail ? "  -- " + detail : ""}`);
}
await browser.close();
writeFileSync(join(OUT, "results.json"), JSON.stringify(results, null, 1));
const f = results.filter(r => r.status === "FAIL").length, w = results.filter(r => r.status === "WARN").length;
console.log(`\nPASS ${results.length - f - w}   WARN ${w}   FAIL ${f}   of ${results.length}`);
