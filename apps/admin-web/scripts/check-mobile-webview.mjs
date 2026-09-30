#!/usr/bin/env node
// Mobile / Android-WebView regression guard for admin-web.
//
// WHY THIS EXISTS
// ---------------
// The same seven defect classes have been re-fixed ~40 times across Aug-Sep 2026
// (horizontal overflow at phone width, clipped control/cell text, missing or
// fallback-marked chart labels, sub-44px tap targets, drawer/overlay scroll traps,
// `100vh` instead of `100dvh`, filter rows collapsing wrong). Each fix was a
// one-route patch; nothing stopped the next feature from reintroducing it.
//
// This guard turns that taxonomy into an executable contract. It loads EVERY route
// in the visual smoke route list (parsed from smoke-visual-live.mjs so coverage can
// never drift from a second hand-maintained copy) at BOTH a laptop viewport and a
// real Android-Chrome device profile (Pixel 5), in BOTH themes, and asserts the
// detectable signature of each class. Failures produce a JSON report plus a PNG.
//
// Known, accepted debt lives in a BASELINE file of waived finding keys, so the guard
// is green today and fails the moment a NEW instance appears. Waiving is deliberate:
// it requires running with --update-baseline and committing the diff.
//
// Usage:
//   node scripts/check-mobile-webview.mjs
//   node scripts/check-mobile-webview.mjs --routes control-tower,feed-analytics
//   node scripts/check-mobile-webview.mjs --baseline-dir <dir> --require-baseline
//   node scripts/check-mobile-webview.mjs --baseline-dir <dir> --update-baseline
//   node scripts/check-mobile-webview.mjs --extra-routes fixtures.json   (name/url pairs, file:// ok)
//   node scripts/check-mobile-webview.mjs --static-only   (source checks only; no app, no browser)
//
// Env: GOATOS_ADMIN_WEB_BASE_URL (default http://127.0.0.1:3300), GOATOS_BEARER_TOKEN
// (optional; set as the auth cookie when present), GOATOS_WEBVIEW_BROWSER_CHANNEL.

import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { scheduler } from "node:timers/promises";
import { chromium, devices } from "@playwright/test";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(scriptDir, "..");
const repoRoot = resolve(appDir, "../..");

const args = parseArgs(process.argv.slice(2));
const appBaseUrl = trimTrailingSlash(
  args.baseUrl ?? process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300",
);
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
const navigationTimeoutMs = Number(process.env.GOATOS_WEBVIEW_NAVIGATION_TIMEOUT_MS ?? 45_000);
const baselineDir = normalizeRepoPath(args.baselineDir ?? process.env.GOATOS_WEBVIEW_BASELINE_DIR);
const updateBaseline = args.updateBaseline || process.env.GOATOS_WEBVIEW_UPDATE_BASELINE === "1";
const requireBaseline = args.requireBaseline || process.env.GOATOS_WEBVIEW_REQUIRE_BASELINE === "1";
const baselineFile = baselineDir ? join(baselineDir, "mobile-webview-waivers.json") : undefined;

const outDir = join(
  repoRoot,
  ".codex-goatos-render",
  "admin-web-webview",
  new Date().toISOString().replaceAll(/[:.]/g, "-"),
);

// ── Device matrix ────────────────────────────────────────────────────────────
// The mobile lane is a REAL Android-Chrome profile, not a narrow desktop window:
// the Android WebView is what ships, and UA/touch/deviceScaleFactor change layout
// (native date inputs, tap heuristics, `dvh` resolution, font boosting).
const pixel5 = devices["Pixel 5"];
const VIEWPORTS = [
  { label: "laptop", viewport: { width: 1440, height: 900 }, isMobile: false, hasTouch: false, deviceScaleFactor: 1 },
  {
    label: "android",
    viewport: { width: 393, height: 851 },
    isMobile: true,
    hasTouch: true,
    deviceScaleFactor: pixel5?.deviceScaleFactor ?? 2.75,
    userAgent:
      pixel5?.userAgent ??
      "Mozilla/5.0 (Linux; Android 11; Pixel 5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Mobile Safari/537.36",
  },
];
const THEMES = ["dark", "light"];
const THEME_STORAGE_KEY = "mesha.shell.theme";
const TAP_TARGET_MIN_PX = 44;

// ── Route source: parsed from smoke-visual-live.mjs, never re-typed ──────────
function loadSmokeRoutes() {
  const source = readFileSync(join(scriptDir, "smoke-visual-live.mjs"), "utf8");
  const block =
    source.match(/function buildRoutes\([^)]*\) \{[\s\S]*?const pagerMinimums = new Map/)?.[0] ?? "";
  if (!block) throw new Error("could not locate buildRoutes(...) in smoke-visual-live.mjs — route source moved");
  const year = String(new Date().getFullYear());
  const entries = [];
  for (const [, name, quote, path] of block.matchAll(/name:\s*"([^"]+)"[\s\S]{0,500}?path:\s*([`"])([^`"]+)/g)) {
    let resolved = path;
    if (quote === "`") {
      resolved = resolved
        .replace(/\$\{new Date\(\)\.getFullYear\(\)\}/g, year)
        .replace(/\$\{smokeWideWindowFrom\}/g, isoDaysAgo(43))
        .replace(/\$\{smokeWideWindowTo\}/g, isoDaysAgo(0));
    }
    if (resolved.includes("${")) {
      // Needs a live fixture id resolved by the smoke sweep (goat/load/workflow ids).
      entries.push({ name, path: resolved, skip: "dynamic-fixture-id" });
      continue;
    }
    entries.push({ name, path: resolved });
  }
  if (entries.length < 20) throw new Error(`route parse produced only ${entries.length} routes — regex drifted`);
  return entries;
}

function isoDaysAgo(days) {
  return new Date(Date.now() - days * 86_400_000).toISOString().slice(0, 10);
}

const allRoutes = loadSmokeRoutes();
const extraRoutes = args.extraRoutes
  ? JSON.parse(readFileSync(normalizeRepoPath(args.extraRoutes), "utf8")).map((r) => ({ ...r, extra: true }))
  : [];
const knownNames = [...allRoutes, ...extraRoutes].map((r) => r.name);
if (args.routes.length) {
  const unknown = args.routes.filter((n) => !knownNames.includes(n));
  if (unknown.length) {
    throw new Error(`--routes has unknown route(s): ${unknown.join(", ")}\nvalid: ${knownNames.join(", ")}`);
  }
}
const selectedRoutes = [...allRoutes, ...extraRoutes].filter(
  (r) => args.routes.length === 0 || args.routes.includes(r.name),
);

// ── Findings ────────────────────────────────────────────────────────────────
const findings = [];
const skipped = [];
function record(f) {
  findings.push({ ...f, key: `${f.check}|${f.viewport}|${f.theme}|${f.route}|${f.target}` });
}

// ── Static (source) checks — run once, no browser needed ─────────────────────
function runStaticChecks() {
  const cssFiles = [];
  for (const dir of [join(appDir, "app"), join(appDir, "components")]) {
    if (!existsSync(dir)) continue;
    for (const name of readdirSync(dir)) if (name.endsWith(".css")) cssFiles.push(join(dir, name));
  }
  for (const file of cssFiles) {
    const rel = file.slice(repoRoot.length + 1);
    const lines = readFileSync(file, "utf8").split("\n");
    lines.forEach((line, index) => {
      // Class 6: viewport-height units that the Android WebView URL bar breaks.
      // A line that also names dvh/svh is a deliberate fallback pair and is fine.
      if (/\b(100vh|calc\([^)]*100vh)/.test(line) && !/dvh|svh/.test(line)) {
        record({
          check: "vh-instead-of-dvh",
          class: 6,
          viewport: "static",
          theme: "static",
          route: rel,
          target: `${rel}:${index + 1}`,
          detail: `100vh without a dvh/svh companion — the Android WebView URL bar cuts the bottom row: ${line.trim().slice(0, 160)}`,
        });
      }
      // Secondary: backdrop-filter with no @supports fallback is a blank panel in older WebViews.
      if (/backdrop-filter\s*:/.test(line) && !/@supports/.test(lines.slice(Math.max(0, index - 6), index).join("\n"))) {
        record({
          check: "backdrop-filter-without-supports",
          class: "secondary",
          viewport: "static",
          theme: "static",
          route: rel,
          target: `${rel}:${index + 1}`,
          detail: `backdrop-filter with no @supports guard above it: ${line.trim().slice(0, 160)}`,
        });
      }
    });
  }
}

// ── In-page probe ───────────────────────────────────────────────────────────
// Everything the taxonomy declared "detectable" lives here, in one evaluate so a
// route costs one round trip. Returns a flat list of {check, class, target, detail}.
const PROBE = (options) => {
  const TAP_MIN = options.tapMin;
  const isMobile = options.isMobile;
  const out = [];
  // Chrome (and the Android WebView) SHRINK-TO-FIT a page whose content is wider than the
  // device: innerWidth silently grows past device-width and every overflow measured against
  // innerWidth then looks fine while the user is really pinch-zoomed out. Measure against the
  // width we configured, and treat the widening itself as the class-1 defect it is.
  const deviceWidth = options.deviceWidth || window.innerWidth;
  const vw = Math.min(window.innerWidth, deviceWidth);
  const vh = window.innerHeight;
  const add = (check, klass, target, detail) => out.push({ check, class: klass, target, detail });

  const cssPath = (el) => {
    const parts = [];
    let node = el;
    while (node && node.nodeType === 1 && parts.length < 5) {
      let seg = node.tagName.toLowerCase();
      if (node.id) { seg += "#" + node.id; parts.unshift(seg); break; }
      const cls = (node.getAttribute("class") || "").trim().split(/\s+/).filter(Boolean).slice(0, 2);
      if (cls.length) seg += "." + cls.join(".");
      parts.unshift(seg);
      node = node.parentElement;
    }
    return parts.join(">");
  };
  const visible = (el) => {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden" || Number(cs.opacity) === 0) return false;
    if (el.closest("[aria-hidden='true'],[hidden]")) return false;
    const r = el.getBoundingClientRect();
    return r.width > 0.5 && r.height > 0.5;
  };
  const scrollXAncestor = (el) => {
    let n = el.parentElement;
    while (n && n !== document.documentElement) {
      const cs = getComputedStyle(n);
      if (/(auto|scroll)/.test(cs.overflowX)) return n;
      n = n.parentElement;
    }
    return null;
  };
  const scrollingAncestor = (el) => {
    let n = el.parentElement;
    while (n && n !== document.documentElement) {
      const cs = getComputedStyle(n);
      if (/(auto|scroll|hidden)/.test(cs.overflowY) || /(auto|scroll|hidden)/.test(cs.overflowX)) return n;
      n = n.parentElement;
    }
    return null;
  };

  // ── Class 1: horizontal page scroll ───────────────────────────────────────
  const docW = document.documentElement.scrollWidth;
  if (docW > vw + 1) {
    add("page-horizontal-scroll", 1, "document", "documentElement.scrollWidth " + docW + " > device width " + vw);
  }
  if (window.innerWidth > deviceWidth + 1) {
    add(
      "viewport-shrink-to-fit",
      1,
      "document",
      "layout viewport widened to " + window.innerWidth + " on a " + deviceWidth + "px device — content forced a zoom-out",
    );
  }

  // ── Class 1: any element bleeding past the viewport edge ──────────────────
  const overflowing = [];
  for (const el of document.body.querySelectorAll("*")) {
    if (!visible(el)) continue;
    const cs = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    // A fully off-canvas element is a closed drawer, not a bleed.
    if (r.left >= vw || r.right <= 0) continue;
    if (r.right <= vw + 1 && r.left >= -1) continue;
    // Living inside a horizontal scroller is the SUPPORTED pattern (wide table in a card).
    if (scrollXAncestor(el)) continue;
    if (cs.position === "fixed" && r.width >= vw) continue; // full-bleed fixed chrome
    overflowing.push({ el, r });
  }
  // Report only the outermost offender of each subtree; the children are consequences.
  for (const { el, r } of overflowing) {
    if (overflowing.some((o) => o.el !== el && o.el.contains(el))) continue;
    add(
      "element-overflows-viewport",
      1,
      cssPath(el),
      "rect " + Math.round(r.left) + ".." + Math.round(r.right) + " escapes viewport width " + vw,
    );
  }

  // ── Class 1/8: wide tables must scroll INSIDE their card ──────────────────
  for (const table of document.querySelectorAll("table")) {
    if (!visible(table)) continue;
    const needsScroll = table.scrollWidth > vw + 1 || table.getBoundingClientRect().width > vw + 1;
    const owner = scrollXAncestor(table);
    if (!needsScroll) continue;
    if (!owner) {
      add("table-not-scroll-contained", 1, cssPath(table), "table wider than the viewport with no overflow-x ancestor");
      continue;
    }
    const or = owner.getBoundingClientRect();
    if (or.right > vw + 1 || or.left < -1) {
      add("table-not-scroll-contained", 1, cssPath(owner), "table scroll container itself escapes the viewport");
    }
    const card = owner.closest(".card,.panel,.sec,section");
    if (card) {
      const cr = card.getBoundingClientRect();
      if (or.right > cr.right + 2) {
        add("table-not-scroll-contained", 1, cssPath(owner), "table scroller spills past its card's right edge");
      }
    }
  }

  // ── Class 2: clipped / truncated text in controls, cells and labels ───────
  const textSel = "a,button,th,td,.tag,.lab,.nm,.chip,.pill,.kpi .v,label,legend,[role=button]";
  for (const el of document.querySelectorAll(textSel)) {
    if (!visible(el)) continue;
    if (el.classList.contains("sr-only")) continue; // clipped to 1px by design (screen readers only)
    if (el.querySelector(textSel)) continue; // measure leaves, not wrappers
    const cs = getComputedStyle(el);
    const horizontallyClipped = el.scrollWidth > el.clientWidth + 2 && /hidden|clip/.test(cs.overflowX);
    const verticallyClipped = el.scrollHeight > el.clientHeight + 8 && /hidden|clip/.test(cs.overflowY);
    if (!horizontallyClipped && !verticallyClipped) continue;
    // An explicit title/aria-label is the accepted escape hatch (full text on hover/AT).
    if (el.title || el.getAttribute("aria-label")) continue;
    add(
      "clipped-text",
      2,
      cssPath(el),
      (horizontallyClipped ? "scrollWidth " + el.scrollWidth + " > clientWidth " + el.clientWidth : "text clipped vertically") +
        " :: " + (el.textContent || "").trim().slice(0, 60),
    );
  }

  // ── Class 3: chart / axis labels ──────────────────────────────────────────
  const badLabel = (t) => t.length === 0 || /^[-–—.…]{2,}$/.test(t) || /^(NaN|undefined|null|Infinity)$/.test(t);
  for (const cols of document.querySelectorAll(".gcols")) {
    if (!visible(cols)) continue;
    const cells = cols.querySelectorAll(".gcol");
    const labels = cols.querySelectorAll(".gclab");
    if (labels.length < cells.length) {
      add("chart-label-count", 3, cssPath(cols), "axis labels " + labels.length + " < data columns " + cells.length);
    }
    for (const lab of labels) {
      const t = (lab.textContent || "").trim();
      if (badLabel(t)) {
        add("chart-label-empty-or-fallback", 3, cssPath(lab), "axis label renders as " + JSON.stringify(t));
        continue;
      }
      if (!visible(lab)) {
        add("chart-label-invisible", 3, cssPath(lab), "axis label " + JSON.stringify(t) + " is not visible");
        continue;
      }
      if (lab.scrollWidth > lab.clientWidth + 2 || lab.scrollHeight > lab.clientHeight + 4) {
        add("chart-label-clipped", 3, cssPath(lab), "axis label " + JSON.stringify(t) + " is clipped");
      }
    }
    // A bar slot with no value must be hidden, never painted as a fallback dash.
    for (const gcb of cols.querySelectorAll(".gcb")) {
      const empty = gcb.querySelector(".gcbar.none") && !gcb.querySelector(".gcval");
      if (empty && getComputedStyle(gcb).visibility !== "hidden") {
        add("chart-empty-slot-visible", 3, cssPath(gcb), "empty series slot painted instead of visibility:hidden");
      }
    }
    const leg = cols.parentElement ? cols.parentElement.querySelector(".gleg") : null;
    if (leg && leg.scrollWidth > leg.clientWidth + 2) {
      add("chart-legend-clipped", 3, cssPath(leg), "legend row overflows: " + leg.scrollWidth + " > " + leg.clientWidth);
    }
  }
  for (const svg of document.querySelectorAll("svg")) {
    if (!visible(svg)) continue;
    const vb = svg.viewBox && svg.viewBox.baseVal ? svg.viewBox.baseVal : null;
    for (const text of svg.querySelectorAll("text")) {
      const t = (text.textContent || "").trim();
      if (badLabel(t)) {
        add("chart-label-empty-or-fallback", 3, cssPath(text), "svg label renders as " + JSON.stringify(t));
        continue;
      }
      let bbox = null;
      try { bbox = text.getBBox(); } catch { bbox = null; }
      if (bbox && bbox.width === 0) {
        add("chart-label-zero-width", 3, cssPath(text), "svg label " + JSON.stringify(t) + " measures 0 wide");
      }
      if (Number(getComputedStyle(text).opacity) === 0) {
        add("chart-label-invisible", 3, cssPath(text), "svg label " + JSON.stringify(t) + " has opacity 0");
      }
      if (bbox && vb && (bbox.x < vb.x - 1 || bbox.x + bbox.width > vb.x + vb.width + 1)) {
        add("chart-label-outside-viewbox", 3, cssPath(text), "svg label " + JSON.stringify(t) + " is drawn outside the viewBox");
      }
    }
  }

  // ── Class 4: tap targets (mobile only) ───────────────────────────────────
  if (isMobile) {
    const tapSel = "a[href],button,[role=button],summary,select,input:not([type=hidden]),label[for],.chip";
    const targets = [];
    for (const el of document.querySelectorAll(tapSel)) {
      if (!visible(el)) continue;
      if (el.disabled || el.getAttribute("aria-disabled") === "true") continue;
      if (el.closest("table")) continue; // cell links are covered by clipped-text, not tap sizing
      if (el.classList.contains("sr-only") || el.closest(".sr-only")) continue; // screen-reader-only labels are not touch targets
      const r = el.getBoundingClientRect();
      targets.push({ el, r });
      if (r.height < TAP_MIN - 0.5 || r.width < TAP_MIN - 0.5) {
        add(
          "tap-target-too-small",
          4,
          cssPath(el),
          Math.round(r.width) + "x" + Math.round(r.height) + " is under the " + TAP_MIN + "px touch minimum",
        );
      }
    }
    for (let i = 0; i < targets.length; i += 1) {
      for (let j = i + 1; j < targets.length; j += 1) {
        const a = targets[i], b = targets[j];
        if (a.el.contains(b.el) || b.el.contains(a.el)) continue;
        const dx = Math.abs((a.r.left + a.r.right) / 2 - (b.r.left + b.r.right) / 2);
        const dy = Math.abs((a.r.top + a.r.bottom) / 2 - (b.r.top + b.r.bottom) / 2);
        if (dx < 8 && dy < 8) {
          add("tap-targets-overlap", 4, cssPath(a.el), "centre within 8px of " + cssPath(b.el));
        }
      }
    }
    // Class 7: filter bars / form rows that collapse wrong at phone width.
    for (const el of document.querySelectorAll("select,input[type=date],input[type=datetime-local],input[type=search],input[type=text]")) {
      if (!visible(el)) continue;
      const r = el.getBoundingClientRect();
      if (r.width < 96) {
        add("form-control-too-narrow", 7, cssPath(el), "control is only " + Math.round(r.width) + "px wide at phone width");
      }
      if (/date|time/.test(el.getAttribute("type") || "") && r.height < 38) {
        add("date-input-too-short", 7, cssPath(el), "native date input is only " + Math.round(r.height) + "px tall");
      }
    }
    // Secondary: text that the Android WebView renders below legibility.
    for (const el of document.querySelectorAll(".gclab,.gcsub,.gleg *,.small,.muted,figcaption")) {
      if (!visible(el) || !(el.textContent || "").trim()) continue;
      const size = Number.parseFloat(getComputedStyle(el).fontSize);
      if (Number.isFinite(size) && size < 10) {
        add("font-below-legibility", "secondary", cssPath(el), "computed font-size " + size + "px at phone width");
      }
    }
  }

  // ── Class 5: sticky that is silently dead, and scroll traps ──────────────
  for (const el of document.querySelectorAll("*")) {
    const cs = getComputedStyle(el);
    if (cs.position === "sticky") {
      const anc = scrollingAncestor(el);
      if (anc) {
        const acs = getComputedStyle(anc);
        if (acs.overflowY === "hidden" && acs.overflowX === "hidden") {
          add("sticky-inside-overflow-hidden", 5, cssPath(el), "position:sticky whose scroll parent is overflow:hidden — dead in WebView");
        }
      }
      continue;
    }
    if (!visible(el)) continue;
    if (el.scrollHeight > el.clientHeight + 4 && cs.overflowY === "hidden" && el.clientHeight > 40) {
      add("scroll-trap", 5, cssPath(el), "content " + el.scrollHeight + "px tall inside a " + el.clientHeight + "px overflow:hidden box");
    }
  }

  // ── Class 5: popovers / menus / dialogs must stay on screen, above their
  // backdrop, and be the element a tap actually lands on.
  for (const el of document.querySelectorAll("[role=dialog],dialog[open],.popover,.menu,.drawer,.top-date-popover,.parkmenu")) {
    if (!visible(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.right > vw + 1 || r.left < -1) {
      add("overlay-offscreen", 5, cssPath(el), "overlay spans " + Math.round(r.left) + ".." + Math.round(r.right) + " outside viewport " + vw);
    }
    const z = Number(getComputedStyle(el).zIndex) || 0;
    for (const scrim of document.querySelectorAll(".backdrop,.scrim,.overlay,.modal-backdrop")) {
      if (!visible(scrim) || scrim.contains(el)) continue;
      const sz = Number(getComputedStyle(scrim).zIndex) || 0;
      if (sz >= z) {
        add("overlay-below-backdrop", 5, cssPath(el), "overlay z-index " + z + " is not above backdrop z-index " + sz);
      }
    }
    const cx = Math.min(vw - 2, Math.max(2, (r.left + r.right) / 2));
    const cy = Math.min(vh - 2, Math.max(2, (r.top + r.bottom) / 2));
    const hit = document.elementFromPoint(cx, cy);
    if (hit && !el.contains(hit) && hit !== el) {
      add("overlay-not-clickable", 5, cssPath(el), "a tap at the overlay's centre lands on " + cssPath(hit));
    }
  }

  // ── Class 6 runtime twin: a fixed bottom bar with no safe-area inset ─────
  for (const el of document.querySelectorAll("*")) {
    const cs = getComputedStyle(el);
    if (cs.position !== "fixed") continue;
    if (!visible(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.bottom < vh - 4) continue;
    const declared = (el.style.paddingBottom || "") + " " + (cs.paddingBottom || "");
    if (!/env\(|constant\(/.test(el.getAttribute("style") || "") && Number.parseFloat(cs.paddingBottom || "0") < 1) {
      add("fixed-bottom-without-safe-area", 6, cssPath(el), "fixed bottom element with no safe-area-inset padding (" + declared.trim() + ")");
    }
  }

  // ── Pagination must be fully reachable ───────────────────────────────────
  for (const pager of document.querySelectorAll(".pager2,.table-footer-pager,.pager,[data-pager]")) {
    if (!visible(pager)) continue;
    const pr = pager.getBoundingClientRect();
    if (pr.right > vw + 1 || pr.left < -1) {
      add("pagination-unreachable", 4, cssPath(pager), "pager escapes the viewport horizontally");
    }
    for (const btn of pager.querySelectorAll("button,a[href]")) {
      if (!visible(btn)) continue;
      const br = btn.getBoundingClientRect();
      if (br.right > vw + 1 || br.left < -1) {
        add("pagination-unreachable", 4, cssPath(btn), "pager control is cut off at the viewport edge");
      }
      if (isMobile && (br.height < TAP_MIN - 0.5 || br.width < TAP_MIN - 0.5)) {
        add("pagination-unreachable", 4, cssPath(btn), "pager control " + Math.round(br.width) + "x" + Math.round(br.height) + " is under " + TAP_MIN + "px");
      }
    }
  }

  return out;
};

// A second pass, after scrolling: sticky chrome must actually stick.
const STICKY_PROBE = () => {
  const out = [];
  const cssPath = (el) => {
    const parts = [];
    let node = el;
    while (node && node.nodeType === 1 && parts.length < 4) {
      const cls = (node.getAttribute("class") || "").trim().split(/\s+/).filter(Boolean).slice(0, 2);
      parts.unshift(node.tagName.toLowerCase() + (cls.length ? "." + cls.join(".") : ""));
      node = node.parentElement;
    }
    return parts.join(">");
  };
  for (const el of document.querySelectorAll("*")) {
    const cs = getComputedStyle(el);
    if (cs.position !== "sticky") continue;
    const top = Number.parseFloat(cs.top);
    if (!Number.isFinite(top)) continue;
    const r = el.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) continue;
    // It is still inside its container's scroll range (we only scrolled 600px), so it
    // must be pinned at its declared offset, not scrolled away above the fold.
    if (r.top < top - 2) {
      out.push({
        check: "sticky-does-not-stick",
        class: 5,
        target: cssPath(el),
        detail: "sticky top:" + top + " but after scrolling it sits at " + Math.round(r.top),
      });
    }
  }
  return out;
};

// ── Run ─────────────────────────────────────────────────────────────────────
mkdirSync(outDir, { recursive: true });
runStaticChecks();

// --static-only is the lane that always runs in CI: it needs no app, no browser and no
// seeded backend, so the source-level half of the taxonomy (class 6 `100vh`, backdrop-filter
// fallbacks) is enforced on every commit. The live sweep runs when a base URL is available.
let browser = null;
let routesVisited = 0;
if (!args.staticOnly) {
await waitForApp(appBaseUrl);
browser = await chromium.launch({ channel: process.env.GOATOS_WEBVIEW_BROWSER_CHANNEL || "chrome" });
try {
  for (const device of VIEWPORTS) {
    for (const theme of THEMES) {
      const context = await browser.newContext({
        viewport: device.viewport,
        isMobile: device.isMobile,
        hasTouch: device.hasTouch,
        deviceScaleFactor: device.deviceScaleFactor,
        userAgent: device.userAgent,
      });
      await context.addInitScript(
        ([key, value]) => {
          try {
            window.localStorage.setItem(key, value);
          } catch {
            /* storage blocked: the boot script falls back to dark */
          }
        },
        [THEME_STORAGE_KEY, theme],
      );
      if (bearerToken) {
        const cookieUrl = new URL(appBaseUrl);
        await context.addCookies([
          {
            name: "goatos_firebase_id_token",
            value: bearerToken,
            domain: cookieUrl.hostname,
            path: "/",
            httpOnly: true,
            sameSite: "Lax",
            expires: Math.floor(Date.now() / 1000) + 3600,
          },
        ]);
      }
      for (const route of selectedRoutes) {
        if (route.skip) {
          if (device.label === "laptop" && theme === "dark") skipped.push({ name: route.name, reason: route.skip });
          continue;
        }
        const label = `${device.label}:${theme}:${route.name}`;
        const page = await context.newPage();
        try {
          const url = route.url ?? `${appBaseUrl}${route.path}`;
          const response = await page.goto(url, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
          const status = response?.status() ?? 0;
          if (status >= 400 || status === 0) {
            throw new Error(`HTTP ${status || "unknown"} for ${url}`);
          }
          await page.waitForLoadState("networkidle", { timeout: 6_000 }).catch(() => {});
          // Settle on state, not time: the loading markers gone and the theme attribute applied.
          await page.waitForFunction(() => !document.querySelector('[data-loading="true"], .MuiSkeleton-root, [data-skel-root], .skeleton') && document.documentElement.hasAttribute("data-theme"), { timeout: 8_000 }).catch(() => {});
          const appliedTheme = await page.evaluate(() => document.documentElement.getAttribute("data-theme"));
          if (route.url === undefined && appliedTheme !== theme) {
            record({
              check: "theme-not-applied",
              class: "harness",
              viewport: device.label,
              theme,
              route: route.name,
              target: "documentElement",
              detail: `expected data-theme=${theme}, got ${appliedTheme}`,
            });
          }
          const probeResults = await page.evaluate(PROBE, {
            tapMin: TAP_TARGET_MIN_PX,
            isMobile: device.isMobile,
            deviceWidth: device.viewport.width,
          });
          await page.evaluate(() => window.scrollBy(0, 600));
          // Wait for the scroll to land (scrollY reflects it after the next frame), not a timer.
          await page.waitForFunction(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true)))), { timeout: 2_000 }).catch(() => {});
          const stickyResults = await page.evaluate(STICKY_PROBE);
          const routeFindings = [...probeResults, ...stickyResults];
          if (routeFindings.length) {
            const png = join(outDir, `${device.label}-${theme}-${route.name}.png`);
            await page.screenshot({ path: png, fullPage: true }).catch(() => {});
            for (const f of routeFindings) {
              record({ ...f, viewport: device.label, theme, route: route.name, evidence: png.slice(repoRoot.length + 1) });
            }
          }
          routesVisited += 1;
          console.log(`webview_route_done=${label} findings=${routeFindings.length}`);
        } catch (error) {
          record({
            check: "route-failed-to-load",
            class: "harness",
            viewport: device.label,
            theme,
            route: route.name,
            target: route.path ?? route.url,
            detail: String(error && error.message ? error.message : error),
          });
          console.log(`webview_route_error=${label}`);
        } finally {
          await page.close().catch(() => {});
        }
      }
      await context.close();
    }
  }
} finally {
  await browser.close();
}
}

// ── Baseline (deliberate waivers) ───────────────────────────────────────────
let waived = new Set();
if (baselineFile) {
  if (existsSync(baselineFile)) {
    waived = new Set(JSON.parse(readFileSync(baselineFile, "utf8")).waived ?? []);
  } else if (requireBaseline && !updateBaseline) {
    console.error(`--require-baseline was passed but no baseline exists at ${baselineFile}`);
    process.exit(2);
  }
} else if (requireBaseline) {
  console.error("--require-baseline needs --baseline-dir");
  process.exit(2);
}

const unwaived = findings.filter((f) => !waived.has(f.key));
const report = {
  generated_at: new Date().toISOString(),
  base_url: appBaseUrl,
  tap_target_min_px: TAP_TARGET_MIN_PX,
  viewports: VIEWPORTS.map((v) => ({ label: v.label, ...v.viewport, isMobile: v.isMobile })),
  themes: THEMES,
  mode: args.staticOnly ? "static-only" : "full",
  routes_selected: args.staticOnly ? 0 : selectedRoutes.length,
  routes_visited: routesVisited,
  routes_skipped: skipped,
  baseline_file: baselineFile ? baselineFile.slice(repoRoot.length + 1) : null,
  waived_count: findings.length - unwaived.length,
  failure_count: unwaived.length,
  findings_by_check: countBy(unwaived, (f) => f.check),
  findings_by_class: countBy(unwaived, (f) => String(f.class)),
  findings: unwaived,
};
const reportPath = join(outDir, "report.json");
writeFileSync(reportPath, `${JSON.stringify(report, null, 2)}\n`);

if (updateBaseline) {
  if (!baselineFile) {
    console.error("--update-baseline needs --baseline-dir");
    process.exit(2);
  }
  mkdirSync(dirname(baselineFile), { recursive: true });
  writeFileSync(
    baselineFile,
    `${JSON.stringify(
      {
        note: "Accepted mobile/WebView debt. Every entry is a KNOWN defect that a future change must not multiply. Shrink this list; never grow it casually.",
        updated_at: new Date().toISOString(),
        waived: findings.map((f) => f.key).sort(),
      },
      null,
      2,
    )}\n`,
  );
  console.log(`mobile_webview_baseline_written=${baselineFile} entries=${findings.length}`);
  console.log(`mobile_webview_report=${reportPath}`);
  process.exit(0);
}

console.log(`mobile_webview_report=${reportPath}`);
console.log(`mobile_webview_routes_visited=${routesVisited}`);
console.log(`mobile_webview_waived=${findings.length - unwaived.length}`);
if (unwaived.length === 0) {
  console.log("mobile_webview_guard=PASS");
  process.exit(0);
}
console.error(`mobile_webview_guard=FAIL failures=${unwaived.length}`);
for (const f of unwaived.slice(0, 40)) {
  console.error(`  [class ${f.class}] ${f.check} ${f.viewport}/${f.theme}/${f.route} :: ${f.target} :: ${f.detail}`);
}
if (unwaived.length > 40) console.error(`  ... ${unwaived.length - 40} more in ${reportPath}`);
process.exit(1);

// ── helpers ─────────────────────────────────────────────────────────────────
function countBy(list, key) {
  const out = {};
  for (const item of list) {
    const k = key(item);
    out[k] = (out[k] ?? 0) + 1;
  }
  return Object.fromEntries(Object.entries(out).sort((a, b) => b[1] - a[1]));
}

function trimTrailingSlash(value) {
  return value.endsWith("/") ? value.slice(0, -1) : value;
}

function normalizeRepoPath(path) {
  if (!path) return undefined;
  return isAbsolute(path) ? path : join(repoRoot, path);
}

async function waitForApp(baseUrl) {
  const deadline = Date.now() + 60_000;
  for (;;) {
    try {
      const response = await fetch(baseUrl, { redirect: "manual" });
      if (response.status < 500) return;
    } catch {
      /* not up yet */
    }
    if (Date.now() > deadline) throw new Error(`admin-web did not answer at ${baseUrl} within 60s`);
    // Server-readiness poll (no browser involved): a bounded retry interval, not a page wait.
    await scheduler.wait(1_000);
  }
}

function parseArgs(argv) {
  const parsed = {
    routes: [],
    baselineDir: undefined,
    updateBaseline: false,
    requireBaseline: false,
    baseUrl: undefined,
    extraRoutes: undefined,
    staticOnly: false,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--routes") {
      parsed.routes = String(argv[++index] ?? "")
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      if (parsed.routes.length === 0) throw new Error("--routes was passed but named no routes");
    } else if (arg === "--baseline-dir") {
      parsed.baselineDir = argv[++index];
    } else if (arg === "--update-baseline") {
      parsed.updateBaseline = true;
    } else if (arg === "--require-baseline") {
      parsed.requireBaseline = true;
    } else if (arg === "--base-url") {
      parsed.baseUrl = argv[++index];
    } else if (arg === "--extra-routes") {
      parsed.extraRoutes = argv[++index];
    } else if (arg === "--static-only") {
      parsed.staticOnly = true;
    } else {
      throw new Error(`Unknown argument: ${arg}`);
    }
  }
  return parsed;
}
