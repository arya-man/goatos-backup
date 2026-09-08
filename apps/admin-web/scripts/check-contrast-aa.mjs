#!/usr/bin/env node
// check-contrast-aa — measures WCAG 2.1 AA text contrast on the RENDERED admin, in BOTH
// themes, for every control the design system styles.
//
// WHY THIS EXISTS
// ---------------
// The design system is a two-theme system: `.light` on <html> swaps every colour token
// (components/mesha-shell.tsx toggles it). A token that reads fine on the near-black ground
// can fall under 4.5:1 on ivory, and nothing in the static design-system guard can see that,
// because the failure only exists once a browser has resolved var(), color-mix() and the
// cascade. So this asks Chromium.
//
// THREE MEASUREMENT TRAPS, ALL OF WHICH PRODUCED PHANTOM FAILURES BEFORE THEY WERE FIXED.
// Anyone editing the sampling below should read these first; each one reported a confident,
// specific, entirely fictional defect, and two of them survived into a review round.
//
//   1. TRANSITIONS. Controls carry `transition: background .3s, color .3s, border-color .3s`.
//      Sampling right after the class toggle reads a colour that is 40% of the way between the
//      two themes, so a perfectly good control reports 1.00:1 (fg mid-way == bg mid-way). This
//      alone manufactured 8 "failures". Fixed by polling until every sampled colour has held
//      still for three consecutive animation frames.
//   2. MODERN COLOUR SYNTAX. Chrome returns `color(srgb 0.94 0.82 0.80 / .1)` for anything
//      that went through color-mix() — 0..1 channels, not 0..255. Reading those as 8-bit turns
//      a pale pink status tag into near-black and reports 1.04:1. parseColor() below
//      normalises color()/oklab()/oklch()/lab()/lch() as well as rgb().
//   3. SUBTREES THE BROWSER IS NOT RENDERING. A closed <dialog>, a content-visibility:auto
//      block or visibility:hidden keeps its layout box but does NOT recompute its styles on a
//      theme switch, so it still holds the other theme's colours. Those elements are invisible
//      to the user and must be skipped, not reported.
//
// A finding from this guard is only real if the element is on screen, painted, and settled.
//
// USAGE
//   node scripts/check-contrast-aa.mjs              # needs a running admin-web (APP, default :3318)
//   node scripts/check-contrast-aa.mjs --self-test  # pure-function checks, no browser
import { chromium } from "@playwright/test";

// The colour maths runs identically in Node (for the self-test) and in the page.
export function parseColor(c) {
  const nums = (String(c).match(/-?[\d.]+(?:e[-+]?\d+)?/gi) || []).map(Number);
  if (nums.length < 3) return null;
  const unitScaled = /^\s*(?:color|oklab|oklch|lab|lch)\(/i.test(String(c));
  const ch = unitScaled
    ? nums.slice(0, 3).map((v) => Math.max(0, Math.min(1, v)) * 255)
    : nums.slice(0, 3);
  return { ch, a: nums.length > 3 ? nums[3] : 1 };
}

export function relativeLuminance(c) {
  const p = parseColor(c);
  if (!p) return null;
  const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  return 0.2126 * f(p.ch[0]) + 0.7152 * f(p.ch[1]) + 0.0722 * f(p.ch[2]);
}

export function contrastRatio(a, b) {
  const [hi, lo] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x);
  return Number(((hi + 0.05) / (lo + 0.05)).toFixed(2));
}

// WCAG 2.1: 3:1 for large text (>=24px, or >=18.66px bold), 4.5:1 otherwise.
export function requiredRatio(fontSizePx, fontWeight) {
  const large = fontSizePx >= 24 || (fontSizePx >= 18.66 && Number(fontWeight) >= 700);
  return large ? 3 : 4.5;
}

const SELECTOR = [
  "button", "a.btn", ".btn", ".tag", ".chip", ".schip", ".leaf", ".nav",
  ".segs button", ".subtabs a", ".subtabs button", ".metricseg button",
  ".kpi .lab", ".kpi .val", "th", ".sub",
].join(",");

if (process.argv.includes("--self-test")) {
  const eq = (got, want, what) => {
    if (JSON.stringify(got) !== JSON.stringify(want)) {
      console.error(`contrast-aa self-test FAILED: ${what}\n  got  ${JSON.stringify(got)}\n  want ${JSON.stringify(want)}`);
      process.exit(1);
    }
  };
  // Trap 2: unit-scaled syntaxes must not be read as 8-bit.
  eq(parseColor("color(srgb 0.944314 0.825098 0.800784 / 0.1)").ch.map(Math.round), [241, 210, 204], "color(srgb) channels");
  eq(parseColor("rgb(236, 232, 221)").ch, [236, 232, 221], "rgb() channels");
  eq(parseColor("rgba(236, 232, 221, 0.55)").a, 0.55, "rgba() alpha");
  eq(parseColor("oklab(0.72 0.10 0.04 / 0.44)").a, 0.44, "oklab() alpha");
  eq(parseColor("transparent"), null, "keywords are not parseable");
  // The exact pale-pink-on-near-black tag that trap 2 mis-reported as 1.04:1.
  const tag = contrastRatio("color(srgb 0.944314 0.825098 0.800784)", "rgb(7, 7, 6)");
  if (!(tag > 12)) { console.error(`contrast-aa self-test FAILED: danger tag should be high contrast, got ${tag}`); process.exit(1); }
  eq(contrastRatio("rgb(0,0,0)", "rgb(255,255,255)"), 21, "black on white is 21:1");
  eq(contrastRatio("rgb(255,255,255)", "rgb(255,255,255)"), 1, "same colour is 1:1");
  eq(contrastRatio("rgb(255,255,255)", "rgb(0,0,0)"), 21, "order does not matter");
  eq(requiredRatio(11, 500), 4.5, "small text needs 4.5");
  eq(requiredRatio(44, 400), 3, "large text needs 3");
  eq(requiredRatio(19, 700), 3, "bold 19px counts as large");
  eq(requiredRatio(19, 500), 4.5, "non-bold 19px does not");
  console.log("contrast-aa self-test: PASS (colour maths only -- run without --self-test to drive the real browser)");
  process.exit(0);
}

const APP = process.env.APP ?? "http://127.0.0.1:3318";
const ROUTES = (process.env.ROUTES ?? "/,/action-center,/verify,/workflows,/calendar,/herd-signals,/approvals")
  .split(",").map((r) => r.trim()).filter(Boolean);

const probe = await fetch(`${APP}/`).then((r) => r.status, () => null);
if (probe === null) {
  console.error(`contrast-aa: admin-web is not reachable at ${APP}. This guard never starts a server; start one and re-run (APP=... to point elsewhere).`);
  process.exit(1);
}

const browser = await chromium.launch({ channel: "chrome" });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await ctx.newPage();
const findings = [];

for (const theme of ["dark", "light"]) {
  for (const route of ROUTES) {
    const url = `${APP}${route}${route.includes("?") ? "&" : "?"}scope_mode=company`;
    try {
      await page.goto(url, { waitUntil: "domcontentloaded", timeout: 60_000 });
      await page.waitForLoadState("networkidle", { timeout: 12_000 }).catch(() => {});
      await page.evaluate((t) => document.documentElement.classList.toggle("light", t === "light"), theme);
      await page.evaluate(() => document.fonts.ready).catch(() => {});
      // Trap 1: hold until every sampled colour has stopped moving.
      await page.evaluate(() => { window.__snap = null; window.__still = 0; });
      await page.waitForFunction((sel) => {
        const now = [...document.querySelectorAll(sel)]
          .map((e) => { const s = getComputedStyle(e); return s.color + s.backgroundColor + s.borderTopColor; })
          .join("|");
        const same = window.__snap === now;
        window.__snap = now;
        window.__still = same ? window.__still + 1 : 0;
        return window.__still >= 3;
      }, SELECTOR, { timeout: 10_000, polling: "raf" }).catch(() => {});

      const bad = await page.evaluate(
        ({ sel, src }) => {
          const { parseColor, relativeLuminance, contrastRatio, requiredRatio } = eval(`(${src})`);
          const solidBehind = (el) => {
            for (let n = el; n; n = n.parentElement) {
              const p = parseColor(getComputedStyle(n).backgroundColor);
              if (p && p.a > 0.85) return `rgb(${p.ch.map(Math.round).join(", ")})`;
            }
            const p = parseColor(getComputedStyle(document.body).backgroundColor);
            return p ? `rgb(${p.ch.map(Math.round).join(", ")})` : "rgb(255, 255, 255)";
          };
          const over = (fg, bg) => {
            const F = parseColor(fg), B = parseColor(bg);
            if (!F || !B) return bg;
            return `rgb(${[0, 1, 2].map((i) => Math.round(F.ch[i] * F.a + B.ch[i] * (1 - F.a))).join(", ")})`;
          };
          const out = [], seen = new Set();
          for (const el of document.querySelectorAll(sel)) {
            const r = el.getBoundingClientRect();
            if (r.width < 4 || r.height < 4) continue;
            // Trap 3: a box the browser is not painting holds the other theme's colours.
            if (el.checkVisibility && !el.checkVisibility({ contentVisibilityAuto: true, opacityProperty: true, visibilityProperty: true })) continue;
            const text = (el.innerText || "").trim();
            if (!text) continue;
            const s = getComputedStyle(el);
            const key = `${el.className}|${s.color}|${s.backgroundColor}`;
            if (seen.has(key)) continue;
            seen.add(key);
            const bg = solidBehind(el);
            const ratio = contrastRatio(over(s.color, bg), bg);
            const need = requiredRatio(parseFloat(s.fontSize), s.fontWeight);
            if (Number.isFinite(ratio) && ratio < need) {
              out.push({ cls: String(el.className || el.tagName).slice(0, 70), text: text.slice(0, 30), fg: s.color, bg, ratio, need, px: parseFloat(s.fontSize) });
            }
          }
          return out;
        },
        { sel: SELECTOR, src: `{parseColor:${parseColor},relativeLuminance:${relativeLuminance},contrastRatio:${contrastRatio},requiredRatio:${requiredRatio}}` },
      );
      for (const f of bad) findings.push({ theme, route, ...f });
    } catch (err) {
      findings.push({ theme, route, cls: "ROUTE ERROR", text: String(err).slice(0, 120), ratio: 0, need: 0 });
    }
  }
}
await browser.close();

const unique = [...new Map(findings.map((f) => [`${f.theme}|${f.cls}|${f.fg}|${f.bg}`, f])).values()]
  .sort((a, b) => a.ratio - b.ratio);

if (unique.length) {
  console.error(`contrast-aa FAILED: ${unique.length} control(s) below WCAG AA across ${ROUTES.length} route(s) x 2 themes`);
  for (const f of unique) {
    console.error(`  ${String(f.ratio).padStart(6)}:1 (needs ${f.need}:1)  [${f.theme}] ${f.route}  .${f.cls}  "${f.text}"  fg=${f.fg} on ${f.bg} @${f.px}px`);
  }
  process.exit(1);
}
console.log(`contrast-aa: ok (${ROUTES.length} route(s) x 2 themes; every rendered, settled control meets WCAG AA)`);
