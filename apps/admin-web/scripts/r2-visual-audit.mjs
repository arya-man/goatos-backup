#!/usr/bin/env node
// r2-visual-audit.mjs — R2 interactive visual audit of every app/(admin) route against the
// MUI Minimal template (see ~/mesha/ravi-r2-verify.md). Read-only: it never submits a form,
// never clicks destructive actions and never restarts either server.
//
// What it checks (every finding is grouped into a PATTERN across routes, ranked by route count):
//   scan       per route x profile (1440 dark, 1440 light, 390 dark):
//                - dark: element backgrounds with luminance > 0.5 that are not brand/primary buttons/chips
//                - text contrast below WCAG 4.5:1 (3:1 for large text)
//                - colours not in the theme palette (every CSS custom property resolved at runtime:
//                  MUI theme vars + mesha-theme.css / minimal-tokens.css tokens), naming the
//                  stylesheet + rule that sets it via CDP CSS.getMatchedStylesForNode
//                - 390 only: tap targets smaller than 44x44
//   interact   1440 dark: clicks every tab and filter (links that change searchParams, selects,
//              toggle buttons, chips) and watches with a MutationObserver + 100ms sampling +
//              CDP screencast frames: full-page skeleton flash, header/tabs remount, layout shift.
//   drawers    1440 dark: opens drawer/dialog triggers: width vs template drawer widths,
//              backdrop present, children overflowing / clipped, bright backgrounds inside.
//   skeleton   1440 dark: soft-navigates to the route with the RSC response held so loading.tsx
//              shows, then compares the top-level block boxes (header, tabs, filters, KPI row,
//              charts, table/card grid) of skeleton vs loaded: IoU < 0.8, missing or extra = fail.
//   sbs        side-by-side ours | mapped template page (~/mesha/mui-page-map.md) per profile.
//
// Output: <out>/report.json + report.md (+ shots/, sbs/, frames/, drawers/, skeleton/).
//
//   node scripts/r2-visual-audit.mjs [--base http://127.0.0.1:3450] [--template http://127.0.0.1:3480]
//        [--out <dir>] [--only sales,weighing] [--concurrency 5] [--checks scan,interact,drawers,skeleton,sbs]
//        [--page-map ~/mesha/mui-page-map.md] [--query scope_mode=company] [--max-interactions 10]
//        [--skeleton-profiles 1440-dark,390-dark] [--skeleton-nav push|click]

import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const appRoot = resolve(scriptDir, "..");

/**
 * chart-light-scheme: brand/status hexes that exist ONLY in the light palette (theme/theme-config.ts
 * `palette` minus `paletteDark`). A chart mark painted one of them in DARK mode read theme.palette
 * before it followed the active scheme (the #54A02C bars on /sales/sold, R3SP2 2026-09-27).
 */
export function lightOnlyPaletteHexes(configText) {
  const block = (name) => {
    const at = configText.indexOf(`${name}: {`);
    if (at < 0) return "";
    let depth = 0;
    for (let i = configText.indexOf("{", at); i < configText.length; i++) {
      if (configText[i] === "{") depth++;
      else if (configText[i] === "}" && --depth === 0) return configText.slice(at, i);
    }
    return "";
  };
  const hexes = (text) => new Set([...text.matchAll(/(?:lighter|light|main|dark|darker):\s*'(#[0-9A-Fa-f]{6})'/g)].map((m) => m[1].toLowerCase()));
  const light = hexes(block("palette"));
  const dark = hexes(block("paletteDark"));
  return [...light].filter((h) => !dark.has(h));
}
let LIGHT_ONLY_HEXES = [];
/** Chart marks the server-paint scheme probe reads (ssr-scheme; Apex draws only after hydration). */
export const CHART_SCHEME_SCOPE_SERVER = ".minimal__chart__legends__item__dot, .minimal__chart__legends__item__icon";
try { LIGHT_ONLY_HEXES = lightOnlyPaletteHexes(readFileSync(join(appRoot, "theme", "theme-config.ts"), "utf8")); } catch {}

export const PROFILES = [
  { label: "1440-dark", width: 1440, height: 900, theme: "dark", mobile: false },
  { label: "1440-light", width: 1440, height: 900, theme: "light", mobile: false },
  { label: "390-dark", width: 390, height: 844, theme: "dark", mobile: true },
];
const ALL_CHECKS = ["scan", "interact", "drawers", "skeleton", "sbs"];
const CONTENT_ROOT = ".minimal__layout__main__content, main";
/** Page-level wrappers the skeleton block walk always descends through (copied into r2PageLib; the test pins the copy). */
export const PAGE_WRAPPER_SELECTOR = "main, .minimal__layout__main__content, .msh-wrap, .screen, [data-page-root], [data-skel-root]";
const OUR_THEME_KEYS = ["mesha.shell.theme", "goatos-theme"];
const TEMPLATE_THEME_KEY = "theme-mode";
// Template drawer widths (Minimal v7.7.0 next-ts): kanban-details { xs: 1, sm: 480 },
// calendar-filters / file-manager-file-details 320. The kanban one is re-measured live.
const TEMPLATE_DRAWER_WIDTHS = [320, 480];

// ---------------------------------------------------------------------------------------------
// pure helpers (exported for scripts/r2-visual-audit.test.mjs)

export function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith("--")) continue;
    const key = a.slice(2);
    const next = argv[i + 1];
    if (next === undefined || next.startsWith("--")) out[key] = true;
    else { out[key] = next; i++; }
  }
  return out;
}

/** Walk app/(admin) (or any app dir) for page.tsx and turn folders into routes. */
export function discoverRoutes(adminDir) {
  const routes = [];
  const walk = (dir, segs) => {
    for (const name of readdirSync(dir)) {
      const full = join(dir, name);
      if (statSync(full).isDirectory()) {
        if (name.startsWith("_") || name.startsWith("@")) continue;
        walk(full, /^\(.*\)$/.test(name) ? segs : [...segs, name]);
      } else if (/^page\.(tsx|ts|jsx|js)$/.test(name)) {
        const route = "/" + segs.join("/");
        const params = segs.filter((s) => /^\[.+\]$/.test(s)).map((s) => s.replace(/^\[+\.{0,3}|\]+$/g, ""));
        const source = readFileSync(full, "utf8");
        routes.push({
          route,
          file: full,
          dynamic: params.length > 0,
          params,
          pattern: routePattern(route),
          redirectOnly: /\bredirect\(/.test(source) && !/<[A-Z][A-Za-z]*[\s/>]/.test(source),
          hasLoading: existsSync(join(dir, "loading.tsx")),
        });
      }
    }
  };
  walk(adminDir, []);
  return routes.sort((a, b) => a.route.localeCompare(b.route));
}

export function routePattern(route) {
  const body = route.split("/").map((s) => (/^\[.+\]$/.test(s) ? "([^/?#]+)" : s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))).join("/");
  return new RegExp(`^${body || "/"}$`);
}

/** Parse the route | template table rows of mui-page-map.md -> Map(route -> template path | null). */
export function parsePageMap(text) {
  const raw = new Map();
  for (const line of text.split("\n")) {
    const m = /^\|\s*`([^`]+)`[^|]*\|\s*([^|]*)\|/.exec(line);
    if (!m) continue;
    const route = m[1].trim();
    if (!route.startsWith("/")) continue;
    const cell = m[2].trim();
    if (/redirect-only/i.test(cell)) raw.set(route, { redirect: true });
    else if (/^as\s+`([^`]+)`/i.test(cell)) raw.set(route, { alias: /^as\s+`([^`]+)`/i.exec(cell)[1] });
    else {
      const t = /`(\/dashboard[^`\s]*)`/.exec(cell);
      raw.set(route, t ? { template: t[1] } : {});
    }
  }
  const out = new Map();
  for (const [route, v] of raw) {
    let cur = v;
    for (let hop = 0; hop < 5 && cur?.alias; hop++) cur = raw.get(cur.alias);
    out.set(route, cur?.redirect ? null : cur?.template ?? null);
  }
  return out;
}

export function templateFor(route, map) {
  if (map.has(route)) return map.get(route) ?? "/dashboard";
  // nearest mapped ancestor, else the app dashboard
  const segs = route.split("/");
  while (segs.length > 1) {
    segs.pop();
    const up = segs.join("/") || "/";
    if (map.get(up)) return map.get(up);
  }
  return "/dashboard";
}

export function iou(a, b) {
  const x1 = Math.max(a.x, b.x), y1 = Math.max(a.y, b.y);
  const x2 = Math.min(a.x + a.w, b.x + b.w), y2 = Math.min(a.y + a.h, b.y + b.h);
  const inter = Math.max(0, x2 - x1) * Math.max(0, y2 - y1);
  const union = a.w * a.h + b.w * b.h - inter;
  return union > 0 ? inter / union : 0;
}

/**
 * Greedy IoU matching of skeleton blocks to loaded blocks. When the skeleton carries optional blocks
 * (OptionalSkeleton: rendered by the page only with data) and the page has fewer blocks, the match
 * is also tried without them and the pairing with fewer failures wins.
 */
export function compareBlocks(skel, loaded, threshold = 0.8) {
  const full = greedyBlocks(skel, loaded, threshold);
  if (!skel.some((s) => s.optional) || skel.length <= loaded.length) return full;
  const lean = greedyBlocks(skel.filter((s) => !s.optional), loaded, threshold);
  const fails = (c) => c.mismatched.length + c.missing.length + c.extra.length;
  return fails(lean) <= fails(full) ? lean : full;
}

/**
 * IoU of two top-level blocks. A repeated-item card grid (a CSS grid of equal-width cards: template
 * job / product / tour lists) carries its first item as `item`: its item count is data (one page of
 * placeholder cards vs the SOPs the module really has), so a grid is judged by its ITEM shape — the
 * first card's box (column count, card height) — against the other side's first card, or against a
 * lone loaded card (a one-item grid is visited down to the card itself). A wrong card anatomy or
 * column count still fails. Table cards likewise compare their chrome (`head`: card top to the
 * bottom of the table head) when both sides have a table.
 */
export function blockIou(s, l) {
  let v = iou(s, l);
  // A table card's row count is data too (a page of placeholder rows vs the rows the day has): two
  // table cards are judged by their chrome above the rows (header, tabs, toolbar, table head).
  if (s.head && l.head) v = Math.max(v, iou(s.head, l.head));
  if (s.item && l.item) v = Math.max(v, iou(s.item, l.item));
  else if (s.item) v = Math.max(v, iou(s.item, l));
  else if (l.item) v = Math.max(v, iou(s, l.item));
  return v;
}

function greedyBlocks(skel, loaded, threshold) {
  const pairs = [];
  skel.forEach((s, i) => loaded.forEach((l, j) => { const v = blockIou(s, l); if (v > 0.1) pairs.push({ i, j, v }); }));
  pairs.sort((p, q) => q.v - p.v);
  const usedS = new Set(), usedL = new Set(), matches = [];
  for (const p of pairs) {
    if (usedS.has(p.i) || usedL.has(p.j)) continue;
    usedS.add(p.i); usedL.add(p.j);
    matches.push({ skel: skel[p.i], loaded: loaded[p.j], iou: Math.round(p.v * 100) / 100 });
  }
  return {
    matches,
    mismatched: matches.filter((m) => m.iou < threshold),
    missing: loaded.filter((_, j) => !usedL.has(j)),
    // A skeleton block wrapped in OptionalSkeleton (components/app/skeletons) stands for a block the
    // page renders only with data (a KPI deck hidden at all-zero); its absence is not an extra.
    extra: skel.filter((s, i) => !usedS.has(i) && !s.optional),
  };
}

/**
 * tab-scroll-kept (TR3-P1-3): a same-route tab / filter click keeps the reader where they were.
 * `scroll` = page scrollY at the click, after settling, and the max scroll after settling. A move of
 * more than SCROLL_JUMP_PX is a jump, unless the page only got shorter and the browser clamped to
 * its new bottom (a filter with fewer rows near the page end; the control is still on screen).
 */
export const SCROLL_JUMP_PX = 150;
export function scrollJumped(scroll) {
  if (!scroll) return false;
  const { clickScroll, nowScroll, maxScroll } = scroll;
  if (Math.abs(nowScroll - clickScroll) <= SCROLL_JUMP_PX) return false;
  const clamped = nowScroll < clickScroll && nowScroll >= maxScroll - 2;
  return !clamped;
}

/**
 * panel-fallback-twin (J3 P1-2): the skeleton a same-route click swaps into a URL panel (at +150ms)
 * must have the landed panel's shape: union-box IoU >= FALLBACK_TWIN_IOU. And the pressed control
 * must not move more than FALLBACK_JUMP_PX at any 100ms sample of the transition (fallback-jump).
 */
export const FALLBACK_TWIN_IOU = 0.8;
export const FALLBACK_JUMP_PX = 8;
export function boxIou(a, b) {
  if (!a || !b) return 1;
  const ix = Math.max(0, Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x));
  const iy = Math.max(0, Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y));
  const inter = ix * iy;
  const uni = a.w * a.h + b.w * b.h - inter;
  return uni > 0 ? inter / uni : 1;
}
export function fallbackTwinFails(watch) {
  const out = [];
  if (watch?.maxTargetShift > FALLBACK_JUMP_PX) out.push(["fallback-jump", `the pressed control moved ${watch.maxTargetShift}px during the transition (skeleton or header of another height)`]);
  if (watch?.fallback) {
    const iou = boxIou(watch.fallback.skeleton, watch.fallback.loaded);
    if (iou < FALLBACK_TWIN_IOU) out.push(["fallback-shape", `the click-time panel skeleton does not match the landed panel (IoU ${iou.toFixed(2)}: ${watch.fallback.skeleton.w}x${watch.fallback.skeleton.h} vs ${watch.fallback.loaded.w}x${watch.fallback.loaded.h})`]);
  }
  return out;
}

// P0 = blocks the visual gate (make admin-web-visual-gate / npm run visual:gate).
export const P0_PATTERNS = [
  /^interact\|[^|]+\|(skeleton-flash|full-reload)$/, // full-page skeleton flash / document reload on a tab / filter change
  // url-keyed-panel (Ravi 2026-09-27 "the tab transition HANGS"): 150ms after a same-route tab /
  // filter click the pressed tab is selected and the panel is its skeleton (or the answer landed).
  /^interact\|[^|]+\|(tab-not-selected|stale-panel)$/,
  // tab-scroll-kept (TR3-P1-3): a tab / filter click must not move the page scroll (clamping to a
  // shorter page's bottom excepted; scroll:false + a panel skeleton twin of the same height).
  /^interact\|[^|]+\|scroll-jump$/,
  // panel-fallback-twin (J3 P1-1/P1-2): the pressed control stays put through the transition and
  // the click-time panel skeleton has the landed panel's shape (1440 dark AND 390 dark).
  /^interact\|[^|]+\|(fallback-jump|fallback-shape)$/,
  /^dark-bright-bg\|/,
  /^off-palette\|/,
  /^chart-black\|/, // a chart series / legend mark whose colour never resolved (paints black)
  /^chart-light-scheme\|/, // a dark-mode chart painted a light-scheme-only palette colour
  /^drawer\|(overflow\|clipped|no-backdrop)/,
  /^skeleton\|(mismatch|missing|extra)/, // skeleton vs loaded block IoU < 0.8, block missing / extra
  /^tap\|/,
  /^sideways-scroll\|/,
  /^route\|error$/, // crash / HTTP >= 400
];
export const isP0 = (pattern) => P0_PATTERNS.some((re) => re.test(pattern));

/**
 * Gate decision. Without a baseline (or strict) every P0 pattern fails. With a baseline
 * (pattern -> route count, shrink-only like the design-system ratchet) a P0 pattern fails
 * when it is new or reaches more routes than recorded.
 *
 * Per-route ratchet (INTEGRATOR 2026-09-27): when `strictRoutes` (the shell routes plus the routes
 * the push touched) and a per-route baseline (`routeBaseline`: route -> [pattern]) are given, ANY
 * failure pattern — P0 or not — that a strict route shows and its baseline entry does not list
 * fails too. Shell checks (`shell|…`) are P0, so a new shell failure fails on every route.
 */
export function gateFailures(patterns, baseline, strict, { strictRoutes = null, routeBaseline = null } = {}) {
  const p0 = patterns.filter((p) => p.p0);
  const out = (strict || !baseline)
    ? p0.map((p) => ({ ...p, why: "P0" }))
    : p0.filter((p) => !(p.pattern in baseline) || p.routeCount > baseline[p.pattern]).map((p) => ({ ...p, why: p.pattern in baseline ? `grew ${baseline[p.pattern]} -> ${p.routeCount} routes` : "new P0 pattern" }));
  if (!strictRoutes || !routeBaseline) return out;
  const seen = new Set(out.map((p) => p.pattern));
  for (const p of patterns) {
    if (seen.has(p.pattern)) continue;
    const fresh = p.routes.filter((r) => strictRoutes.has(r) && !(routeBaseline[r] || []).includes(p.pattern));
    if (fresh.length) { out.push({ ...p, why: `new on ${fresh.join(", ")} (touched/shell route, any new failure fails)` }); seen.add(p.pattern); }
  }
  return out;
}

// Five routes that exercise the whole shell (sidebar groups, header, tabs + filter toolbar, sort
// headers, KPI rows, charts, a table pager). Every fast run audits them, whatever the push touched.
export const SHELL_ROUTES = ["/verify", "/approvals", "/sales/sold", "/weighing/analytics", "/counts/herd"];

const IMPORT_RE = /(?:import|export)\s[^'"`;]*?from\s*["']([^"']+)["']|import\(\s*["']([^"']+)["']\s*\)|^\s*import\s*["']([^"']+)["']/gm;

/** Resolve a module specifier from `fromFile` to an app file (repo-relative to appRoot) or null. */
export function resolveImport(spec, fromFile, appRoot, exists = existsSync) {
  let base;
  if (spec.startsWith("@/")) base = join(appRoot, spec.slice(2));
  else if (spec.startsWith(".")) base = resolve(dirname(fromFile), spec);
  else return null;
  for (const cand of [base, `${base}.tsx`, `${base}.ts`, `${base}.mjs`, `${base}.js`, `${base}.css`, join(base, "index.tsx"), join(base, "index.ts")]) {
    if (/\.(tsx?|mjs|jsx?|css)$/.test(cand) && exists(cand) && !(exists(cand) && statSync(cand).isDirectory())) return cand;
  }
  return null;
}

/**
 * Which audited routes does a set of changed files touch? A route is touched when a changed file is
 * its page / loading / a segment layout, or anything those import (transitively, app files only).
 * Files the root and (admin) layouts import, global CSS, theme/ and layouts/ are the SHELL: a change
 * there is `shell: true` (the SHELL_ROUTES cover it). Returns routes ordered by import distance.
 */
export function routesForFiles(changed, routes, appRoot, { read = (f) => readFileSync(f, "utf8"), exists = existsSync } = {}) {
  const abs = new Set(changed.map((f) => resolve(appRoot, f.replace(/^apps\/admin-web\//, ""))));
  const cache = new Map();
  const importsOf = (file) => {
    if (cache.has(file)) return cache.get(file);
    const list = [];
    cache.set(file, list);
    if (!/\.(tsx?|mjs|jsx?)$/.test(file)) return list;
    let text = ""; try { text = read(file); } catch { return list; }
    for (const m of text.matchAll(IMPORT_RE)) { const r = resolveImport(m[1] || m[2] || m[3], file, appRoot, exists); if (r) list.push(r); }
    return list;
  };
  /** BFS distance from the entry files to the nearest changed file (Infinity when none). */
  const distance = (entries) => {
    const seen = new Set(); let frontier = entries.filter((e) => exists(e)); let d = 0;
    while (frontier.length && d < 40) {
      for (const f of frontier) if (abs.has(f)) return d;
      const next = [];
      for (const f of frontier) { seen.add(f); for (const i of importsOf(f)) if (!seen.has(i)) { seen.add(i); next.push(i); } }
      frontier = next; d++;
    }
    return Infinity;
  };
  const shellEntries = [join(appRoot, "app", "layout.tsx"), join(appRoot, "app", "(admin)", "layout.tsx"), join(appRoot, "app", "(admin)", "loading.tsx")];
  const shell = distance(shellEntries) < Infinity || [...abs].some((f) => /\/(theme|layouts)\//.test(f) || (/\/app\/[^/]+\.css$/.test(f)));
  const touched = [];
  for (const r of routes) {
    const dir = dirname(r.file);
    const segs = [];
    for (let d = dir; d.startsWith(join(appRoot, "app", "(admin)")) && d !== join(appRoot, "app", "(admin)"); d = dirname(d)) segs.push(join(d, "layout.tsx"), join(d, "loading.tsx"), join(d, "template.tsx"));
    const dist = distance([r.file, ...segs]);
    if (dist < Infinity) touched.push({ route: r.route, distance: dist });
  }
  touched.sort((a, b) => a.distance - b.distance || a.route.localeCompare(b.route));
  return { shell, routes: touched };
}

/** skeleton-on-touched: the routes the fast lane runs the skeleton twin check on = every touched route. */
export function skeletonRoutesFor(touched) {
  return new Set(touched.map((t) => t.route));
}

/** Fast-run route set: the shell routes + the touched routes (closest first), capped. */
export function fastRouteSet(touched, { shellRoutes = SHELL_ROUTES, cap = 8 } = {}) {
  const extra = touched.map((t) => t.route).filter((r) => !shellRoutes.includes(r));
  return { routes: [...shellRoutes, ...extra.slice(0, cap)], skipped: extra.slice(cap) };
}

/** Group findings into patterns ranked by distinct routes, then occurrences. */
export function groupPatterns(findings) {
  const by = new Map();
  for (const f of findings) {
    if (f.severity === "info") continue;
    let p = by.get(f.pattern);
    if (!p) { p = { pattern: f.pattern, label: f.label, check: f.check, p0: !!f.p0 || isP0(f.pattern), routes: new Set(), profiles: new Set(), count: 0, examples: [] }; by.set(f.pattern, p); }
    p.routes.add(f.route);
    if (f.profile) p.profiles.add(f.profile);
    p.count += f.count ?? 1;
    if (p.examples.length < 8 && !p.examples.some((e) => e.route === f.route)) p.examples.push({ route: f.route, profile: f.profile, detail: f.detail, rule: f.rule, evidence: f.evidence });
  }
  return [...by.values()]
    .map((p) => ({ ...p, routes: [...p.routes].sort(), profiles: [...p.profiles].sort(), routeCount: p.routes.size }))
    .sort((a, b) => b.routeCount - a.routeCount || b.count - a.count || a.label.localeCompare(b.label));
}

export function slug(route) {
  return (route === "/" ? "home" : route.replace(/^\//, "").replace(/[^a-zA-Z0-9]+/g, "_")).slice(0, 80);
}

const normSel = (s) => s.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\s+/g, "").toLowerCase();

/** Index repo CSS so a CDP rule from a hashed Next chunk can be named by its source file. */
export function buildCssIndex(root) {
  const files = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      if (name === "node_modules" || name.startsWith(".")) continue;
      const full = join(dir, name);
      const st = statSync(full);
      if (st.isDirectory()) walk(full);
      else if (name.endsWith(".css")) files.push({ file: relative(root, full), text: normSel(readFileSync(full, "utf8")) });
    }
  };
  for (const d of ["app", "components", "features", "styles", "lib"]) if (existsSync(join(root, d))) walk(join(root, d));
  const cache = new Map();
  return (selectorText) => {
    if (cache.has(selectorText)) return cache.get(selectorText);
    const parts = splitSelectors(selectorText).map(normSel).filter(Boolean);
    let best = null, bestScore = 0;
    for (const f of files) {
      let score = 0;
      for (const p of parts) if (f.text.includes(p + "{") || f.text.includes(p + ",")) score++;
      if (score > bestScore) { best = f.file; bestScore = score; }
    }
    cache.set(selectorText, best);
    return best;
  };
}

export function splitSelectors(text) {
  const out = []; let depth = 0, cur = "";
  for (const ch of text) {
    if (ch === "(" || ch === "[") depth++;
    if (ch === ")" || ch === "]") depth--;
    if (ch === "," && depth === 0) { out.push(cur.trim()); cur = ""; } else cur += ch;
  }
  if (cur.trim()) out.push(cur.trim());
  return out;
}

/** A visually hidden control (`.sr-only`: ~1px box, clip rect 0 or clip-path inset) shows nothing to tap. */
export function isVisuallyHidden(cs, r) {
  const clipped = /rect\(0(px)?,? 0(px)?,? 0(px)?,? 0(px)?\)/.test(cs.clip || "") || /inset\(50%\)/.test(cs.clipPath || "");
  return clipped && (r.width <= 2 || r.height <= 2 || cs.overflow === "hidden");
}

// ---------------------------------------------------------------------------------------------
// in-page library (installed with addInitScript; must be self-contained)

function r2PageLib() {
  if (window.__r2lib) return;
  /** A painted SVG shape (not a group / svg wrapper): the elements a chart colour lands on. */
  function isChartMark(el) { return /^(path|rect|circle|ellipse|polygon|polyline)$/i.test(el.tagName); }
  const ROOT = ".minimal__layout__main__content, main";
  const PAGE_WRAPPER_SELECTOR = "main, .minimal__layout__main__content, .msh-wrap, .screen, [data-page-root], [data-skel-root]";
  const SKEL = ".MuiSkeleton-root, [data-skeleton], [class*='skeleton'], [class*='Skeleton']";
  let lastMutation = performance.now();
  const startObserver = () => {
    try { new MutationObserver(() => { lastMutation = performance.now(); }).observe(document.documentElement, { childList: true, subtree: true, attributes: true, characterData: true }); } catch {}
  };
  if (document.documentElement) startObserver(); else document.addEventListener("DOMContentLoaded", startObserver);

  const canvas = document.createElement("canvas"); canvas.width = canvas.height = 1;
  const ctx2d = canvas.getContext("2d", { willReadFrequently: true });
  const colorCache = new Map();
  function parseColor(s) {
    if (!s) return null;
    s = String(s).trim();
    if (colorCache.has(s)) return colorCache.get(s);
    let out = null;
    if (s === "transparent" || s === "none") out = [0, 0, 0, 0];
    let m = /^rgba?\(([^)]+)\)$/.exec(s);
    if (!out && m) { const p = m[1].split(/[\s,/]+/).filter(Boolean).map((v) => (v.endsWith("%") ? parseFloat(v) / 100 : parseFloat(v))); out = [p[0], p[1], p[2], p[3] ?? 1]; }
    m = /^color\(srgb\s+([^)]+)\)$/.exec(s);
    if (!out && m) { const p = m[1].split(/[\s/]+/).filter(Boolean).map(parseFloat); out = [p[0] * 255, p[1] * 255, p[2] * 255, p[3] ?? 1]; }
    if (!out && /^#|^[a-z]+\(|^[a-z]+$/i.test(s) && ctx2d) {
      ctx2d.clearRect(0, 0, 1, 1); ctx2d.fillStyle = "rgba(0,0,0,0)"; ctx2d.fillStyle = s;
      ctx2d.fillRect(0, 0, 1, 1);
      const d = ctx2d.getImageData(0, 0, 1, 1).data;
      out = d[3] === 0 && s !== "transparent" ? null : [d[0], d[1], d[2], d[3] / 255];
    }
    colorCache.set(s, out);
    return out;
  }
  const hex = (c) => (c ? "#" + c.slice(0, 3).map((v) => Math.round(v).toString(16).padStart(2, "0")).join("") + (c[3] < 0.999 ? `@${Math.round(c[3] * 100) / 100}` : "") : "?");
  const lin = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  const lum = (c) => 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2]);
  const contrast = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
  const over = (top, bottom) => { const a = top[3]; return [top[0] * a + bottom[0] * (1 - a), top[1] * a + bottom[1] * (1 - a), top[2] * a + bottom[2] * (1 - a), 1]; };

  function sig(el) {
    if (!el || !el.tagName) return "?";
    const cls = [...el.classList].filter((c) => !/^css-|^Mui-(?:focused|focusVisible|selected|active|checked|expanded)$|^jsx-|^__|^\d/.test(c) && c.length < 48);
    cls.sort((a, b) => rank(a) - rank(b));
    const role = el.getAttribute("role");
    return el.tagName.toLowerCase() + (role ? `[role=${role}]` : "") + (cls.length ? "." + cls.slice(0, 3).join(".") : "");
  }
  function rank(c) { return /^Mui[A-Za-z]+-root$/.test(c) ? 0 : /^minimal__/.test(c) ? 1 : /^Mui/.test(c) ? 3 : 2; }
  function path(el) {
    const parts = []; let cur = el;
    for (let i = 0; cur && cur.tagName && i < 4 && cur !== document.body; i++, cur = cur.parentElement) parts.unshift(sig(cur));
    return parts.join(" > ");
  }
  const root = () => document.querySelector(ROOT) || document.body || document.documentElement;
  const visible = (el) => { try { return el.checkVisibility({ opacityProperty: true, visibilityProperty: true }); } catch { return !!el.getClientRects().length; } };
  // Same body as the exported isVisuallyHidden (the page lib must be self-contained; the test pins the copy).
  const isVisuallyHidden = (cs, r) => {
    const clipped = /rect\(0(px)?,? 0(px)?,? 0(px)?,? 0(px)?\)/.test(cs.clip || "") || /inset\(50%\)/.test(cs.clipPath || "");
    return clipped && (r.width <= 2 || r.height <= 2 || cs.overflow === "hidden");
  };
  const text = (el) => [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join("").trim();

  function skeletonCount() {
    const r = root();
    return [...r.querySelectorAll(SKEL)].filter((e) => { const b = e.getBoundingClientRect(); return b.width > 20 && b.height > 6 && b.bottom > 0 && b.top < innerHeight && visible(e); }).length;
  }
  function quiet(ms) { return performance.now() - lastMutation > ms; }
  function ready() { return document.readyState === "complete" && skeletonCount() === 0 && !!root().querySelector("h1, h2, h3, h4, h5, h6, table, [role=tablist], .MuiCard-root"); }
  function settled(ms) { return ready() && quiet(ms); }

  function palette() {
    const names = new Set();
    const visit = (rules) => {
      for (const r of rules) {
        if (r.style) for (let i = 0; i < r.style.length; i++) { const n = r.style[i]; if (n.startsWith("--")) names.add(n); }
        if (r.cssRules) visit(r.cssRules);
      }
    };
    for (const s of document.styleSheets) { try { visit(s.cssRules); } catch {} }
    const cs = [getComputedStyle(document.documentElement), getComputedStyle(document.body)];
    const r = root(); if (r) cs.push(getComputedStyle(r));
    const colors = []; const named = [];
    for (const n of names) {
      for (const c of cs) {
        const v = c.getPropertyValue(n).trim();
        if (!v) continue;
        let col = null;
        const ch = /^(\d+(?:\.\d+)?)[\s,]+(\d+(?:\.\d+)?)[\s,]+(\d+(?:\.\d+)?)$/.exec(v);
        if (ch) col = [+ch[1], +ch[2], +ch[3], 1];
        else if (/^(#|rgb|hsl|color|oklch|oklab|lab|lch|hwb)/i.test(v)) col = parseColor(v);
        if (col && col[3] > 0) { colors.push(col); named.push(n); }
      }
    }
    colors.push([255, 255, 255, 1], [0, 0, 0, 1]); named.push("white", "black");
    return { colors, named };
  }
  function nearest(pal, c) {
    let best = 1e9, name = null;
    for (let i = 0; i < pal.colors.length; i++) {
      const p = pal.colors[i];
      const d = Math.max(Math.abs(p[0] - c[0]), Math.abs(p[1] - c[1]), Math.abs(p[2] - c[2]));
      if (d < best) { best = d; name = pal.named[i]; }
    }
    return { dist: best, name };
  }
  function pageBg() {
    for (const el of [document.body, document.documentElement]) { const c = parseColor(getComputedStyle(el).backgroundColor); if (c && c[3] > 0.99) return c; }
    const v = getComputedStyle(document.documentElement).getPropertyValue("--bg").trim();
    return parseColor(v) || (document.documentElement.dataset.theme === "light" ? [255, 255, 255, 1] : [14, 21, 18, 1]);
  }
  /** composite background behind el; null when an image/gradient makes it unknowable */
  function effectiveBg(el, base) {
    const layers = []; let cur = el;
    while (cur && cur.nodeType === 1) {
      const cs = getComputedStyle(cur);
      if (cs.backgroundImage && cs.backgroundImage !== "none" && !/^url\(.*\.svg/.test(cs.backgroundImage)) return null;
      const c = parseColor(cs.backgroundColor);
      if (c && c[3] > 0) { layers.push(c); if (c[3] > 0.99) break; }
      cur = cur.parentElement;
    }
    let bg = base;
    for (let i = layers.length - 1; i >= 0; i--) bg = over(layers[i], bg);
    return bg;
  }
  function opacityChain(el) { let o = 1; for (let c = el; c && c.nodeType === 1; c = c.parentElement) o *= parseFloat(getComputedStyle(c).opacity) || 0; return o; }

  const DELIBERATE = "button, [role=button], a, input, select, [role=tab], [role=switch], [role=checkbox], [role=radio], .MuiChip-root, .MuiButton-root, .MuiFab-root, .MuiToggleButton-root, .MuiBadge-badge, .MuiAvatar-root, .MuiSwitch-root, .MuiSlider-root, .MuiLinearProgress-root, .MuiCircularProgress-root, .minimal__label__root, .MuiTabs-indicator, .MuiTooltip-tooltip, .MuiSnackbar-root, .MuiPaginationItem-root, .MuiStepIcon-root, svg, img, video, canvas, picture";
  const INTERACTIVE = "a[href], button, [role=button], [role=tab], [role=checkbox], [role=radio], [role=switch], [role=menuitem], [role=combobox], input:not([type=hidden]), select, textarea, summary, [tabindex='0']";

  /**
   * Computed-style scan. opts: { scope, theme, tap, max }.
   * Returns deduped findings; offenders get data-r2-q so CDP can name the CSS rule.
   */
  const CHART_SCHEME_SCOPE = ".apexcharts-canvas, .minimal__chart__legends__item__dot, .minimal__chart__legends__item__icon";
  function scan(opts) {
    const scopeEl = opts.scope ? document.querySelector(opts.scope) : document.body;
    if (!scopeEl) return { findings: [], scanned: 0 };
    document.querySelectorAll("[data-r2-q]").forEach((e) => e.removeAttribute("data-r2-q"));
    const pal = palette();
    const base = pageBg();
    const all = scopeEl.querySelectorAll("*");
    const seen = new Map(); const findings = []; let q = 0; let scanned = 0;
    const add = (check, el, detail, extra = {}) => {
      const s = sig(el);
      const key = `${check}|${s}|${extra.value ?? ""}|${extra.prop ?? ""}`;
      if (seen.has(key)) { seen.get(key).count++; return; }
      const f = { check, sig: s, path: path(el), detail, count: 1, text: (el.innerText || el.getAttribute("aria-label") || "").trim().slice(0, 40), ...extra };
      if (extra.prop && q < 50) { f.q = String(++q); el.setAttribute("data-r2-q", (el.getAttribute("data-r2-q") ? el.getAttribute("data-r2-q") + " " : "") + f.q); }
      seen.set(key, f); findings.push(f);
    };
    const max = opts.max || 6000;
    for (let i = 0; i < all.length && scanned < max; i++) {
      const el = all[i];
      if (el.closest("[aria-hidden=true], .MuiSkeleton-root, noscript, script, style, template")) continue;
      const r = el.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) continue;
      if (!visible(el)) continue;
      scanned++;
      const cs = getComputedStyle(el);
      const inSvg = el instanceof SVGElement;
      const own = text(el);
      const disabled = !!el.closest(".Mui-disabled, [aria-disabled=true], :disabled");
      // off-palette
      const props = [];
      if (!inSvg) {
        if (own) props.push(["color", cs.color]);
        props.push(["background-color", cs.backgroundColor]);
        for (const side of ["top", "right", "bottom", "left"]) if (parseFloat(cs[`border-${side}-width`]) > 0 && cs[`border-${side}-style`] !== "none") props.push([`border-${side}-color`, cs[`border-${side}-color`]]);
      } else if (!(el instanceof SVGSVGElement) && el.closest(".apexcharts-canvas, .recharts-wrapper, [class*='chart' i], [class*='spark' i], [class*='svg-bars'], [class*='svg-series']")) {
        // chart marks only: template illustrations / emoji icons carry their own artwork fills
        if (cs.fill && cs.fill !== "none" && !cs.fill.startsWith("url")) props.push(["fill", cs.fill]);
        if (cs.stroke && cs.stroke !== "none" && !cs.stroke.startsWith("url")) props.push(["stroke", cs.stroke]);
      }
      // chart-black (R3CNT 2026-09-27): an Apex series / legend mark painted pure black means its
      // colour never resolved (a palette KEY such as "primary" handed to chart.colors). Black is in
      // the palette (common.black), so off-palette alone cannot see it.
      // Only DRAWN marks count (TR1-#3): a `g.apexcharts-series` group or the legend marker's `svg`
      // wrapper has the initial computed fill (black) by inheritance but paints nothing itself; the
      // coloured path / rect inside it is the mark. A drawn mark with no own colour still paints black
      // and is still caught (fill-opacity 0 / fully transparent marks paint nothing and are skipped).
      if (inSvg && isChartMark(el) && el.closest(".apexcharts-series, .apexcharts-legend-marker")) {
        const f = parseColor(cs.fill);
        const fo = parseFloat(cs.fillOpacity || "1") * parseFloat(cs.opacity || "1");
        if (f && f[3] > 0.5 && fo > 0.05 && f[0] === 0 && f[1] === 0 && f[2] === 0) add("chart-black", el, `fill ${hex(f)} on a chart series mark`, { prop: "fill", value: hex(f) });
      } else if (!inSvg && el.closest(".apexcharts-legend-marker")) {
        const b = parseColor(cs.backgroundColor);
        if (b && b[3] > 0.5 && b[0] === 0 && b[1] === 0 && b[2] === 0) add("chart-black", el, `background ${hex(b)} on a chart legend marker`, { prop: "background-color", value: hex(b) });
      }
      // N4 (TR-2): the template ChartLegends dot / icon is a chart mark too (legend colours come from
      // the same palette read as the series), so a light-only hex there is the same defect.
      if (opts.theme === "dark" && opts.lightOnly && opts.lightOnly.length && el.closest(CHART_SCHEME_SCOPE)) {
        for (const [prop, val] of [["fill", cs.fill], ["stroke", cs.stroke], ["background-color", cs.backgroundColor]]) {
          const c = parseColor(val);
          if (c && c[3] > 0.2 && opts.lightOnly.includes(hex(c).slice(0, 7).toLowerCase())) add("chart-light-scheme", el, `${prop} ${hex(c)} is a light-scheme palette colour on a dark chart`, { prop, value: hex(c).slice(0, 7).toLowerCase() });
        }
      }
      const doneBorder = new Set();
      for (const [prop, val] of props) {
        const c = parseColor(val);
        if (!c || c[3] < 0.04) continue;
        const n = nearest(pal, c);
        if (n.dist <= 4) continue;
        const pname = prop.startsWith("border-") ? "border-color" : prop;
        if (pname === "border-color") { if (doneBorder.has(hex(c))) continue; doneBorder.add(hex(c)); }
        add("off-palette", el, `${pname} ${hex(c)} (nearest token ${n.name} Δ${Math.round(n.dist)})`, { prop, value: hex(c) });
      }
      if (inSvg) continue;
      // bright background in dark
      if (opts.theme === "dark") {
        const bg = parseColor(cs.backgroundColor);
        if (bg && bg[3] >= 0.35 && r.width * r.height >= 600 && Math.min(r.width, r.height) >= 12) {
          const comp = over(bg, effectiveBg(el.parentElement, base) || base);
          if (lum(comp) > 0.5 && !el.matches(DELIBERATE) && !el.closest(".MuiButton-root, .MuiChip-root, .MuiFab-root, .MuiTooltip-tooltip")) {
            add("dark-bright-bg", el, `bg ${hex(bg)} luminance ${lum(comp).toFixed(2)} on ${Math.round(r.width)}x${Math.round(r.height)}`, { prop: "background-color", value: hex(bg) });
          }
        }
      }
      // contrast
      if (own && !disabled && el.tagName !== "OPTION" && !el.closest("svg")) {
        const op = opacityChain(el);
        const fg = parseColor(cs.color);
        const bg = effectiveBg(el, base);
        if (fg && bg && op > 0.5) {
          const f2 = over([fg[0], fg[1], fg[2], fg[3] * op], bg);
          const ratio = contrast(f2, bg);
          const size = parseFloat(cs.fontSize); const weight = parseInt(cs.fontWeight, 10) || 400;
          const large = size >= 24 || (size >= 18.66 && weight >= 700);
          const need = large ? 3 : 4.5;
          if (ratio < need - 0.05) add("contrast", el, `${ratio.toFixed(2)}:1 < ${need}:1 (${hex(fg)} on ${hex(bg)}, ${size}px)`, { prop: "color", value: `${hex(fg)}/${hex(bg)}`, ratio });
        }
      }
    }
    // tap targets
    if (opts.tap) {
      for (const el of scopeEl.querySelectorAll(INTERACTIVE)) {
        if (!visible(el) || el.closest("[aria-hidden=true]")) continue;
        let target = el;
        let r = el.getBoundingClientRect();
        if (r.width < 1 || r.height < 1) continue;
        const cs = getComputedStyle(el);
        // A visually hidden control (`.sr-only`: 1px, clip rect 0) is not a tap target: nothing shows on
        // screen to tap. It stays for keyboard / screen-reader users (e.g. the implicit "Apply search"
        // submit on /leave and /routines). guard: tap-skip-visually-hidden (r2-visual-audit.test.mjs)
        if (isVisuallyHidden(cs, r)) continue;
        if (el.tagName === "INPUT" && (cs.opacity === "0" || /checkbox|radio/.test(el.type))) { target = el.closest(".MuiButtonBase-root, label") || el.parentElement; r = target.getBoundingClientRect(); }
        const bigAncestor = target.parentElement?.closest(INTERACTIVE);
        if (bigAncestor) { const br = bigAncestor.getBoundingClientRect(); if (br.width >= 44 && br.height >= 44) continue; }
        if (el.tagName === "A" && cs.display === "inline") { const p = el.parentElement; if (p && (p.innerText || "").trim().length > (el.innerText || "").trim().length + 8) continue; }
        let w = r.width, h = r.height;
        for (const pseudo of ["::before", "::after"]) {
          const ps = getComputedStyle(target, pseudo);
          if (ps.content === "none" || ps.position !== "absolute") continue;
          const t = parseFloat(ps.top), b = parseFloat(ps.bottom), lft = parseFloat(ps.left), rt = parseFloat(ps.right);
          if ([t, b, lft, rt].every(Number.isFinite)) { w = Math.max(w, r.width - lft - rt); h = Math.max(h, r.height - t - b); }
          else { const pw = parseFloat(ps.width), ph = parseFloat(ps.height); if (pw) w = Math.max(w, pw); if (ph) h = Math.max(h, ph); }
          const mh = parseFloat(ps.minHeight), mw = parseFloat(ps.minWidth); if (mh) h = Math.max(h, mh); if (mw) w = Math.max(w, mw);
        }
        if (w < 43.5 || h < 43.5) add("tap-target", target, `${Math.round(w)}x${Math.round(h)} < 44x44`, { value: `${Math.round(w)}x${Math.round(h)}` });
      }
    }
    const doc = document.scrollingElement || document.documentElement;
    return { findings, scanned, paletteSize: pal.colors.length, sideways: doc.scrollWidth > innerWidth + 1 ? { scrollWidth: doc.scrollWidth, viewport: innerWidth } : null };
  }

  /** Top-level blocks of the content column (for skeleton vs loaded). */
  function blocks() {
    const out = []; const r0 = root(); const vh = innerHeight;
    const r0w = r0.getBoundingClientRect().width;
    const surface = (el, cs) => {
      const eb = el.getBoundingClientRect();
      // page-level wrappers (content column, .screen, Container) are never blocks themselves -- by
      // selector too, not only by size: a short loaded page (the /approvals queue with one row, 508px)
      // left its content Container under the 60%-height cut, so the whole page read as ONE block
      // against the skeleton's header + card (TR3-P0-4; guard: skeleton-page-wrapper-selector).
      if (el.matches(PAGE_WRAPPER_SELECTOR)) return false;
      if (eb.width >= r0w * 0.9 && eb.height >= vh * 0.6 && !el.matches(".MuiSkeleton-root, table, .MuiCard-root")) return false;
      if (el.matches(".MuiSkeleton-root, table, svg, canvas, img, video, h1, h2, h3, h4, h5, h6, p, button, a, input, [role=tablist], .MuiCard-root, .MuiPaper-root")) return true;
      const bg = parseColor(cs.backgroundColor);
      if ((bg && bg[3] > 0.05) || cs.boxShadow !== "none" || (cs.backgroundImage && cs.backgroundImage !== "none")) return true;
      return ["top", "right", "bottom", "left"].some((s) => parseFloat(cs[`border-${s}-width`]) > 0 && cs[`border-${s}-style`] !== "none");
    };
    // `display: contents` wrappers are layout-transparent: their children are the blocks
    // (OptionalSkeleton marks its children optional with data-skel-optional).
    const flat = (c) => (getComputedStyle(c).display === "contents" ? [...c.children].flatMap(flat) : [c]);
    const kids = (el) => [...el.children].flatMap(flat).filter((c) => { const b = c.getBoundingClientRect(); return b.width >= 40 && b.height >= 12 && visible(c); });
    const stackedVertically = (list) => {
      const rs = list.map((c) => c.getBoundingClientRect()).sort((a, b) => a.top - b.top);
      for (let i = 1; i < rs.length; i++) if (rs[i].top < rs[i - 1].bottom - 2) return false;
      return true;
    };
    const kind = (el) => {
      if (el.matches(".MuiSkeleton-root") || el.querySelector(".MuiSkeleton-root")) return "skeleton";
      if (el.matches("[role=tablist]") || el.querySelector("[role=tablist]")) return "tabs";
      if (el.querySelector("table, [role=grid]")) return "table";
      if (el.querySelector("svg.recharts-surface, canvas, .apexcharts-canvas")) return "chart";
      const b = el.getBoundingClientRect();
      if (b.height < 220 && el.querySelector("input, select, [role=combobox]")) return "filters";
      if (b.top < 260 && el.querySelector("h1, h2, h3, h4") && b.height < 160) return "header";
      if (kids(el).length >= 3 && b.height < 280 && !stackedVertically(kids(el))) return "kpi-row";
      if (!stackedVertically(kids(el)) && kids(el).length >= 2) return "card-grid";
      return "block";
    };
    const visit = (el, depth) => {
      for (const ch of kids(el)) {
        const b = ch.getBoundingClientRect();
        if (b.top >= vh || b.bottom <= 0) continue;
        const cs = getComputedStyle(ch);
        const cks = kids(ch);
        if (depth < 8 && !surface(ch, cs) && cks.length > 0 && stackedVertically(cks)) { visit(ch, depth + 1); continue; }
        if (b.width * b.height < 2400) continue;
        const box = { x: Math.round(b.left), y: Math.round(b.top), w: Math.round(b.width), h: Math.round(Math.min(b.bottom, vh) - b.top), kind: kind(ch), sig: sig(ch), optional: Boolean(ch.closest("[data-skel-optional]")) };
        // repeated-item card grid (see blockIou): a CSS grid of >= 2 equal-width cards
        if (cs.display === "grid" && cks.length >= 2) {
          const rs = cks.map((c) => c.getBoundingClientRect());
          if (rs.every((r) => Math.abs(r.width - rs[0].width) < 2)) box.item = { x: Math.round(rs[0].left), y: Math.round(rs[0].top), w: Math.round(rs[0].width), h: Math.round(Math.min(rs[0].bottom, vh) - rs[0].top) };
        }
        // table card (see blockIou): the chrome above the rows (card header, tabs, toolbar, table head)
        const thead = ch.querySelector("thead");
        if (thead && box.kind !== "chart") {
          const hb = thead.getBoundingClientRect();
          if (hb.bottom > b.top && hb.bottom <= Math.min(b.bottom, vh)) box.head = { x: box.x, y: box.y, w: box.w, h: Math.round(hb.bottom - b.top) };
        }
        out.push(box);
      }
    };
    visit(r0, 0);
    return out;
  }

  const docRect = (el) => { if (!el) return null; const b = el.getBoundingClientRect(); return { top: b.top + scrollY, left: b.left + scrollX }; };
  /** Arm the remount / skeleton watch before an interaction (`id` = the data-r2-i of the control). */
  function armWatch(id) {
    const r0 = root();
    const header = r0.querySelector("header") || r0.querySelector("h1, h2, h3, h4")?.parentElement || null;
    const tabs = r0.querySelector("[role=tablist]");
    const w = { header, tabs, headerRect: docRect(header), tabsRect: docRect(tabs), maxSkel: 0, headerGone: false, tabsGone: false, cls: 0, samples: 0, skelWhileHeaderGone: false, url0: location.href, early: [] };
    // url-keyed-panel: 150ms after every click (the LAST one is the navigating click: a select's
    // option, a tab), is the pressed control selected, and is the panel its skeleton or already the
    // answer? A panel still showing the old content without a skeleton is the "hang".
    const target = id ? document.querySelector(`[data-r2-i="${id}"]`) : null;
    w.target = target;
    w.maxTargetShift = 0;
    w.onClick = () => {
      const t0 = performance.now();
      // tab-scroll-kept: where the page and the pressed control sat at the moment of the (last) click,
      // AFTER any pre-scroll that brought the control into view.
      w.clickScroll = scrollY;
      w.clickTop = target && target.isConnected ? target.getBoundingClientRect().top : null;
      w.clickDocTop = target && target.isConnected ? docRect(target).top : null;
      setTimeout(() => {
        const r = root();
        const landed = location.href !== w.url0;
        // stale-panel-scoped (J3 P0-1): the panel that went aria-busy must itself show its skeleton
        // (data-url-panel-pending). Any skeleton elsewhere on the page (an unrelated streaming card)
        // no longer counts, which is how the /people Notifications hang passed as "flaky".
        const busy = [...r.querySelectorAll("[data-url-panel][aria-busy=true]")];
        const skel = busy.length ? busy.every((p) => p.hasAttribute("data-url-panel-pending")) : !!r.querySelector("[data-url-panel-pending], [data-panel-skeleton]");
        const sel = target && target.isConnected ? target.getAttribute("aria-selected") === "true" || target.classList.contains("Mui-selected") || target.getAttribute("aria-pressed") === "true" || target.getAttribute("aria-current") === "page" : null;
        // panel-fallback-twin (J3 P1-2): the skeleton a click swapped in, as the union box of the
        // pending panels' visible children, to compare with the same panels once the answer landed.
        const pending = busy.filter((p) => p.hasAttribute("data-url-panel-pending"));
        w.fallbackPanels = pending;
        w.fallbackBox = pending.length ? unionBox(pending) : null;
        w.early.push({ ms: Math.round(performance.now() - t0), landed, skel, selected: sel, busy: busy.length });
      }, 150);
    };
    document.addEventListener("click", w.onClick, true);
    const sample = () => {
      w.samples++;
      const rr = root().getBoundingClientRect();
      const x0 = Math.max(0, rr.left), y0 = Math.max(0, rr.top), x1 = Math.min(innerWidth, rr.right), y1 = Math.min(innerHeight, rr.bottom);
      let hit = 0, tot = 0;
      for (let gx = 0; gx < 16; gx++) for (let gy = 0; gy < 10; gy++) {
        const x = x0 + ((gx + 0.5) * (x1 - x0)) / 16, y = y0 + ((gy + 0.5) * (y1 - y0)) / 10;
        const e = document.elementFromPoint(x, y); tot++;
        // A URL-keyed panel's own skeleton is the intended state, not a full-page skeleton flash.
        if (e && e.closest(SKEL) && !e.closest("[data-url-panel]")) hit++;
      }
      const cov = tot ? hit / tot : 0;
      w.maxSkel = Math.max(w.maxSkel, cov);
      if (header && !header.isConnected) { w.headerGone = true; if (cov > 0.05) w.skelWhileHeaderGone = true; }
      // fallback-jump (J3 P1-1/P1-2): the pressed control must stay under the finger for the whole
      // transition, not just after it settles (a skeleton of another height moved the /vaccination
      // pen tabs 60px down and back; a header action on one desk only moved the /people strip).
      if (w.clickDocTop != null && target && target.isConnected) w.maxTargetShift = Math.max(w.maxTargetShift, Math.abs(docRect(target).top - w.clickDocTop));
      if (tabs && !tabs.isConnected) w.tabsGone = true;
    };
    w.timer = setInterval(sample, 100);
    try { w.po = new PerformanceObserver((list) => { for (const e of list.getEntries()) w.cls += e.value; }); w.po.observe({ type: "layout-shift", buffered: false }); } catch {}
    window.__r2w = w;
    return { header: header ? sig(header) : null, tabs: tabs ? sig(tabs) : null };
  }
  /** Union box (doc coords, clipped to the visible viewport: what the thumb sees) of the visible children of `display: contents` panels. */
  function unionBox(panels) {
    let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
    const flat = (c) => (getComputedStyle(c).display === "contents" ? [...c.children].flatMap(flat) : [c]);
    for (const p of panels) for (const c of [...p.children].flatMap(flat)) {
      const b = c.getBoundingClientRect();
      if (b.width < 4 || b.height < 4) continue;
      if (b.top >= innerHeight || b.bottom <= 0) continue;
      x0 = Math.min(x0, b.left); y0 = Math.min(y0, Math.max(b.top, 0) + scrollY); x1 = Math.max(x1, b.right); y1 = Math.max(y1, Math.min(b.bottom, innerHeight) + scrollY);
    }
    return x1 > x0 && y1 > y0 ? { x: Math.round(x0), y: Math.round(y0), w: Math.round(x1 - x0), h: Math.round(y1 - y0) } : null;
  }
  function readWatch() {
    const w = window.__r2w; if (!w) return null;
    const landedBox = w.fallbackPanels && w.fallbackPanels.every((p) => p.isConnected) ? unionBox(w.fallbackPanels) : null;
    clearInterval(w.timer); try { w.po?.disconnect(); } catch {}
    document.removeEventListener("click", w.onClick, true);
    const shift = (el, r) => { if (!el || !el.isConnected || !r) return 0; const n = docRect(el); return Math.max(Math.abs(n.top - r.top), Math.abs(n.left - r.left)); };
    return { maxSkel: Math.round(w.maxSkel * 100) / 100, headerGone: w.headerGone || (w.header ? !w.header.isConnected : false), tabsGone: w.tabsGone || (w.tabs ? !w.tabs.isConnected : false), cls: Math.round(w.cls * 1000) / 1000, headerShift: Math.round(shift(w.header, w.headerRect)), tabsShift: Math.round(shift(w.tabs, w.tabsRect)), skelWhileHeaderGone: w.skelWhileHeaderGone, urlChanged: location.href !== w.url0, url: location.href, samples: w.samples, early: w.early.length ? w.early[w.early.length - 1] : null, maxTargetShift: Math.round(w.maxTargetShift || 0), fallback: w.fallbackBox && landedBox ? { skeleton: w.fallbackBox, loaded: landedBox } : null, scroll: w.clickScroll == null ? null : { clickScroll: Math.round(w.clickScroll), nowScroll: Math.round(scrollY), maxScroll: Math.round(document.documentElement.scrollHeight - innerHeight) } };
  }

  /** Tab + filter candidates in the content column (marked with data-r2-i). */
  function interactionCandidates(maxTabs, maxFilters) {
    document.querySelectorAll("[data-r2-i]").forEach((e) => e.removeAttribute("data-r2-i"));
    const r0 = root(); const out = []; let i = 0;
    const mark = (el, c) => { el.setAttribute("data-r2-i", String(++i)); out.push({ ...c, id: String(i), label: (el.innerText || el.getAttribute("aria-label") || "").trim().replace(/\s+/g, " ").slice(0, 40) }); };
    const inView = (el) => { const b = el.getBoundingClientRect(); return b.width > 0 && b.height > 0 && visible(el); };
    const tabs = [...r0.querySelectorAll("[role=tab]")].filter((t) => inView(t) && t.getAttribute("aria-selected") !== "true" && !t.matches(".Mui-disabled, [aria-disabled=true]"));
    tabs.slice(0, maxTabs).forEach((t) => mark(t, { kind: "tab", href: t.getAttribute("href") }));
    let n = 0;
    const here = location.pathname;
    for (const a of r0.querySelectorAll("a[href]")) {
      if (n >= Math.ceil(maxFilters / 2)) break;
      if (a.closest("[role=tablist], table tbody, nav, .MuiTablePagination-root, .MuiPagination-root, [role=dialog]") || !inView(a)) continue;
      let u; try { u = new URL(a.getAttribute("href"), location.href); } catch { continue; }
      if (u.origin !== location.origin || u.pathname !== here || u.search === location.search || !u.search) continue;
      if (/[?&](drawer|compose|modal|dialog)=/.test(u.search) || TRIGGER_RE.test((a.innerText || "").trim())) continue;
      if (DESTRUCTIVE_RE.test(a.innerText || "")) continue;
      mark(a, { kind: "filter-link", href: a.getAttribute("href") }); n++;
    }
    let s = 0;
    for (const el of r0.querySelectorAll("[role=combobox], select")) {
      if (s >= 2) break;
      if (el.closest("table, [role=dialog]") || !inView(el) || el.matches("input[type=text], input:not([readonly])[role=combobox]")) continue;
      mark(el, { kind: el.tagName === "SELECT" ? "filter-native-select" : "filter-select" }); s++;
    }
    let t = 0;
    for (const el of r0.querySelectorAll("button[aria-pressed=false], .MuiToggleButton-root:not(.Mui-selected), .MuiChip-clickable")) {
      if (t >= 2) break;
      if (el.closest("table, [role=dialog]") || !inView(el) || DESTRUCTIVE_RE.test(el.innerText || "")) continue;
      mark(el, { kind: "filter-toggle" }); t++;
    }
    return out;
  }
  const DESTRUCTIVE_RE = /delete|remove|replay|approve|reject|publish|save|submit|sign ?out|log ?out|archive|discard|confirm|reset|send|mark|verify|accept|cancel/i;
  const TRIGGER_RE = /^(\+\s*)?(add|new|filter|filters|tag|edit|view|details|open|create|record|import|move|assign|more|show|history|passport|manage|configure|columns)\b/i;

  /** Drawer/dialog triggers (marked with data-r2-d). */
  function drawerTriggers(max) {
    document.querySelectorAll("[data-r2-d]").forEach((e) => e.removeAttribute("data-r2-d"));
    const r0 = root(); const out = []; let i = 0;
    const ok = (el) => { const b = el.getBoundingClientRect(); return b.width > 0 && b.height > 0 && visible(el) && !el.closest("nav, [role=dialog], .MuiDrawer-root"); };
    for (const el of r0.querySelectorAll("button, [role=button], a[aria-haspopup], [aria-haspopup=dialog], a[href*='drawer=']")) {
      if (out.length >= max) break;
      const label = (el.innerText || el.getAttribute("aria-label") || el.getAttribute("title") || "").trim().replace(/\s+/g, " ");
      if (!ok(el) || !label || DESTRUCTIVE_RE.test(label) || el.getAttribute("role") === "tab" || el.closest("[role=tablist], .MuiTablePagination-root")) continue;
      if (!(TRIGGER_RE.test(label) || el.getAttribute("aria-haspopup") === "dialog" || (el.getAttribute("href") || "").includes("drawer="))) continue;
      if (el.tagName === "A" && el.getAttribute("href") && !el.getAttribute("href").startsWith("#") && !el.getAttribute("href").includes("drawer=")) continue;
      el.setAttribute("data-r2-d", String(++i)); out.push({ id: String(i), label: label.slice(0, 40), kind: "button" });
    }
    const row = [...r0.querySelectorAll("tbody tr, .MuiCard-root")].find((r) => ok(r) && getComputedStyle(r).cursor === "pointer" && !r.querySelector("a[href]:only-child"));
    if (row && out.length < max + 1) { row.setAttribute("data-r2-d", String(++i)); out.push({ id: String(i), label: (row.innerText || "").trim().replace(/\s+/g, " ").slice(0, 40), kind: "row" }); }
    return out;
  }

  /** Measure the open drawer/dialog. */
  function overlayInfo() {
    // An anchored template popover (CustomPopover / MUI Popover, e.g. the /counts/breakdown tag picker)
    // is not a dialog: the template draws it over an INVISIBLE backdrop by design, so it is never held
    // to the dialog backdrop rule (TR1-#7). guard: popover-not-dialog (r2-visual-audit.test.mjs)
    const papers = [...document.querySelectorAll(".MuiDrawer-paper, .MuiDialog-paper, [role=dialog]")].filter((p) => { const b = p.getBoundingClientRect(); return b.width > 40 && b.height > 40 && visible(p) && !p.closest("nav, .minimal__layout__nav, [class*='nav__vertical']") && !p.closest(".MuiPopover-root"); });
    const paper = papers.find((p) => p.matches(".MuiDrawer-paper, .MuiDialog-paper")) || papers[0];
    if (!paper) return null;
    document.querySelectorAll("[data-r2-paper]").forEach((e) => e.removeAttribute("data-r2-paper"));
    paper.setAttribute("data-r2-paper", "1");
    const pr = paper.getBoundingClientRect();
    const isDrawer = paper.matches(".MuiDrawer-paper") || !!paper.closest(".MuiDrawer-root");
    const anchor = isDrawer ? ([...paper.classList].find((c) => /paperAnchor(Right|Left|Top|Bottom)$/.test(c)) || "").replace(/.*paperAnchor/, "").toLowerCase() || (pr.right >= innerWidth - 1 ? "right" : pr.left <= 1 ? "left" : "bottom") : "dialog";
    const backdrops = [...document.querySelectorAll(".MuiBackdrop-root")].filter((b) => !b.classList.contains("MuiBackdrop-invisible") && visible(b) && b.closest(".MuiModal-root, .MuiDrawer-root, .MuiDialog-root"));
    const bd = backdrops.map((b) => { const cs = getComputedStyle(b); const c = parseColor(cs.backgroundColor); return { alpha: c ? c[3] : 0, opacity: parseFloat(cs.opacity) }; });
    const backdrop = bd.some((b) => b.alpha * b.opacity > 0.05);
    const overflow = [];
    if (paper.scrollWidth > paper.clientWidth + 2 && getComputedStyle(paper).overflowX !== "hidden") overflow.push({ sig: sig(paper), detail: `paper scrolls sideways (${paper.scrollWidth} > ${paper.clientWidth})` });
    const scrollableAncestor = (el) => { for (let c = el.parentElement; c && c !== paper; c = c.parentElement) { const ox = getComputedStyle(c).overflowX; if (ox === "auto" || ox === "scroll") return c; } return null; };
    for (const el of paper.querySelectorAll("*")) {
      if (overflow.length >= 6) break;
      if (!visible(el)) continue;
      const cs = getComputedStyle(el); const b = el.getBoundingClientRect();
      if (b.width < 1) continue;
      if (el.scrollWidth > el.clientWidth + 2 && el.clientWidth > 0 && (cs.overflowX === "hidden" || cs.overflowX === "clip") && cs.textOverflow !== "ellipsis" && el.matches("table, [class*='table' i], [class*='Table'], .MuiCard-root, .MuiPaper-root, div")) {
        if (el.querySelector("table") || el.matches("table")) overflow.push({ sig: sig(el), detail: `clips ${el.scrollWidth - el.clientWidth}px of content (overflow-x ${cs.overflowX})` });
      }
      if ((b.right > pr.right + 2 || b.left < pr.left - 2) && !scrollableAncestor(el) && el.matches("table, th, td, button, input, .MuiCard-root, .MuiTextField-root")) overflow.push({ sig: sig(el), detail: `sticks out of the ${Math.round(pr.width)}px paper by ${Math.round(Math.max(b.right - pr.right, pr.left - b.left))}px` });
      if (el.matches("table") && scrollableAncestor(el) && scrollableAncestor(el).scrollWidth > scrollableAncestor(el).clientWidth + 2 && isDrawer) overflow.push({ sig: sig(el), detail: `table needs sideways scroll inside the drawer (${scrollableAncestor(el).scrollWidth} > ${scrollableAncestor(el).clientWidth})`, soft: true });
    }
    return { kind: isDrawer ? "drawer" : "dialog", anchor, width: Math.round(pr.width), height: Math.round(pr.height), backdrop, backdrops: bd, overflow, sig: sig(paper), title: (paper.querySelector("h1,h2,h3,h4,h5,h6,.MuiDialogTitle-root,.MuiTypography-h6")?.innerText || "").trim().slice(0, 50) };
  }

  function links() { return [...new Set([...document.querySelectorAll("a[href]")].map((a) => a.href))]; }
  function errorState() {
    const t = (document.body?.innerText || "").slice(0, 4000);
    const m = /Application error|Something went wrong|Unhandled Runtime Error|This page could(?:n.t| not) load|Internal Server Error/i.exec(t);
    return m ? m[0] : null;
  }

  window.__r2lib = { scan, blocks, armWatch, readWatch, interactionCandidates, drawerTriggers, overlayInfo, links, errorState, settled, ready, quiet, skeletonCount };
}

// ---------------------------------------------------------------------------------------------
// runtime

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const unrefSleep = (ms) => new Promise((r) => { const t = setTimeout(r, ms); t.unref?.(); });
const withTimeout = (p, ms, fallback) => Promise.race([p, unrefSleep(ms).then(() => fallback)]);

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const base = String(args.base ?? process.env.R2_AUDIT_BASE_URL ?? "http://127.0.0.1:3450").replace(/\/$/, "");
  const templateBase = String(args.template ?? process.env.R2_AUDIT_TEMPLATE_URL ?? "http://127.0.0.1:3480").replace(/\/$/, "");
  const stamp = new Date().toISOString().replace(/[-:]/g, "").replace("T", "-").slice(0, 15);
  const outDir = resolve(String(args.out ?? join(process.env.R2_AUDIT_OUT_ROOT ?? join(homedir(), "mesha/redesign-shots/r2"), `audit-${stamp}`)));
  const pageMapPath = String(args["page-map"] ?? join(homedir(), "mesha/mui-page-map.md"));
  const concurrency = Number(args.concurrency ?? 5);
  // --fast: the pre-push lane (~2-3 min): shell routes + touched routes, scan (3 profiles, shell
  // checks included) + tab/filter interactions + the skeleton twin check on the TOUCHED routes
  // (skeleton-on-touched); drawers / side-by-sides stay in the full run.
  const fast = !!args.fast;
  // --skeleton-profiles 1440-dark,390-dark: the skeleton twin check per profile (default 1440 dark).
  const skeletonNav = String(args["skeleton-nav"] ?? "push");
  // skeleton-on-touched (TR3 FINAL): the fast / pre-push lane also runs the skeleton twin check on
  // every TOUCHED route, at 1440 dark AND 390 dark (TR-3's P0s were layout fixes that changed a
  // page without its loading twin).
  const skeletonProfiles = args["skeleton-profiles"] ? String(args["skeleton-profiles"]).split(",").map((l) => PROFILES.find((p) => p.label === l.trim())).filter(Boolean)
    : fast ? ["1440-dark", "390-dark"].map((l) => PROFILES.find((p) => p.label === l)).filter(Boolean) : [PROFILES[0]];
  // interact-390 (J3 CI gap 4): tab / filter interactions run at 1440 dark AND 390 dark (touch taps);
  // /people's header jump and the /vaccination pen-tab skeleton jump were phone-only.
  const interactProfiles = (args["interact-profiles"] ? String(args["interact-profiles"]).split(",") : ["1440-dark", "390-dark"]).map((l) => PROFILES.find((p) => p.label === l.trim())).filter(Boolean);
  const checks = new Set(args.checks ? String(args.checks).split(",") : fast ? ["scan", "interact"] : ALL_CHECKS);
  const only = args.only ? String(args.only).split(",").map((s) => s.trim()).filter(Boolean) : null;
  const exactRoutes = args.routes ? String(args.routes).split(",").map((s) => s.trim()).filter(Boolean) : null;
  const touchedFiles = args["touched-files-from"] ? readFileSync(String(args["touched-files-from"]), "utf8").split("\n").map((s) => s.trim()).filter(Boolean)
    : args["touched-files"] ? String(args["touched-files"]).split(",").map((s) => s.trim()).filter(Boolean) : null;
  const query = args.query === undefined ? "scope_mode=company" : args.query === true ? "" : String(args.query);
  const maxInteractions = Number(args["max-interactions"] ?? (fast ? 4 : 10));
  const gate = !!args.gate;
  const baselinePath = args.baseline === undefined ? join(scriptDir, "r2-visual-audit-baseline.json") : String(args.baseline);
  // Extra checks from other workers: every scripts/r2-audit-checks/*.mjs exporting
  // default { name, p0?, profiles?: ["1440-dark", ...], run(page, ctx) -> [{ label, pattern?, detail, p0? }] }
  // is run inside each scan job and folded into the same pattern summary.
  const plugins = [];
  const pluginDir = join(scriptDir, "r2-audit-checks");
  if (existsSync(pluginDir)) for (const f of readdirSync(pluginDir).filter((n) => n.endsWith(".mjs") && !n.endsWith(".test.mjs")).sort()) plugins.push((await import(pathToFileURL(join(pluginDir, f)).href)).default);
  for (const d of ["shots", "sbs", "frames", "drawers", "skeleton"]) mkdirSync(join(outDir, d), { recursive: true });

  const { chromium } = await import("@playwright/test");
  const sharp = (await import("sharp")).default;
  const cssIndex = buildCssIndex(appRoot);
  const pageMap = existsSync(pageMapPath) ? parsePageMap(readFileSync(pageMapPath, "utf8")) : new Map();
  let routes = discoverRoutes(join(appRoot, "app", "(admin)"));
  const allRoutes = routes;
  // strictRoutes: the routes where ANY new failure fails the gate (shell routes + touched routes)
  let strictRoutes = null;
  let skeletonTouched = null;
  if (touchedFiles || fast) {
    const t = touchedFiles ? routesForFiles(touchedFiles, allRoutes, appRoot) : { shell: false, routes: [] };
    const set = fastRouteSet(t.routes, { cap: Number(args.cap ?? 8) });
    strictRoutes = new Set([...SHELL_ROUTES, ...t.routes.map((r) => r.route)]);
    if (fast && !args.checks) skeletonTouched = skeletonRoutesFor(t.routes);
    console.log(`r2-visual-audit: ${touchedFiles ? touchedFiles.length : 0} touched files -> shell ${t.shell ? "TOUCHED" : "untouched"}, ${t.routes.length} touched routes${set.skipped.length ? ` (${set.skipped.length} beyond the fast cap, left to the full run: ${set.skipped.slice(0, 12).join(" ")}${set.skipped.length > 12 ? " …" : ""})` : ""}`);
    if (fast && !exactRoutes && !only) routes = allRoutes.filter((r) => set.routes.includes(r.route));
  }
  if (exactRoutes) routes = allRoutes.filter((r) => exactRoutes.includes(r.route));
  if (only) routes = routes.filter((r) => only.some((o) => r.route.includes(o)));

  const t0 = Date.now();
  const findings = [];
  const routeInfo = new Map(routes.map((r) => [r.route, { route: r.route, file: relative(appRoot, r.file), template: templateFor(r.route, pageMap), dynamic: r.dynamic, url: null, status: "pending", redirectTo: null, errors: [], hasLoading: r.hasLoading }]));
  const collectedLinks = new Set();
  const log = (...a) => console.log(`[${((Date.now() - t0) / 1000).toFixed(0).padStart(4)}s]`, ...a);

  const add = (f) => findings.push({ severity: "fail", count: 1, ...f });

  // R2_CHROME_EXE: a local Chrome for Testing when the pinned Playwright browser is not installed.
  const browser = await chromium.launch({ args: ["--disable-dev-shm-usage"], ...(process.env.R2_CHROME_EXE ? { executablePath: process.env.R2_CHROME_EXE } : {}) });
  const newContext = async (profile, forTemplate) => {
    const ctx = await browser.newContext({
      viewport: { width: profile.width, height: profile.height },
      deviceScaleFactor: 1,
      isMobile: profile.mobile,
      hasTouch: profile.mobile,
      colorScheme: profile.theme,
      reducedMotion: "no-preference",
    });
    await ctx.addInitScript(([keys, theme]) => { try { for (const k of keys) localStorage.setItem(k, theme); } catch {} }, [forTemplate ? [TEMPLATE_THEME_KEY] : OUR_THEME_KEYS, profile.theme]);
    await ctx.addInitScript(r2PageLib);
    return ctx;
  };

  async function gotoSettled(page, url, { maxSkeleton = 15000 } = {}) {
    let status = 0;
    try {
      const resp = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 45000 });
      status = resp?.status() ?? 0;
    } catch (e) {
      return { status: 0, error: String(e.message).split("\n")[0] };
    }
    await page.waitForFunction(() => window.__r2lib && window.__r2lib.ready(), null, { timeout: maxSkeleton, polling: 150 }).catch(() => {});
    await page.waitForFunction(() => window.__r2lib && window.__r2lib.quiet(400), null, { timeout: 3500, polling: 150 }).catch(() => {});
    await page.waitForLoadState("networkidle", { timeout: 1500 }).catch(() => {});
    const stuck = await page.evaluate(() => window.__r2lib?.skeletonCount() ?? 0).catch(() => 0);
    const error = await page.evaluate(() => window.__r2lib?.errorState()).catch(() => null);
    return { status, stuckSkeleton: stuck, error };
  }

  /** evaluate that survives a navigation racing the call (retries after the page is ready). */
  async function safeEval(page, fn, arg, fallback) {
    for (let i = 0; i < 4; i++) {
      try { return await page.evaluate(fn, arg); }
      catch (e) {
        if (!/context was destroyed|Execution context|null|navigation|__r2lib/i.test(String(e.message)) || i === 3) { if (fallback !== undefined) return fallback; throw e; }
        await page.waitForLoadState("domcontentloaded").catch(() => {});
        await page.waitForFunction(() => window.__r2lib && window.__r2lib.ready(), null, { timeout: 8000, polling: 150 }).catch(() => {});
      }
    }
    return fallback;
  }
  const urlFor = (path) => {
    const u = new URL(path, base);
    if (query) for (const [k, v] of new URLSearchParams(query)) if (!u.searchParams.has(k)) u.searchParams.set(k, v);
    return u.toString();
  };
  const redirected = (page, route) => {
    const p = new URL(page.url()).pathname;
    return p !== route.route && !routePattern(route.route).test(p) ? p : null;
  };

  // ---- CDP rule naming ---------------------------------------------------------------------
  const PROP_ALIASES = {
    "background-color": ["background-color", "background"],
    color: ["color"],
    fill: ["fill"],
    stroke: ["stroke"],
  };
  for (const s of ["top", "right", "bottom", "left"]) PROP_ALIASES[`border-${s}-color`] = [`border-${s}-color`, "border-color", `border-${s}`, "border"];
  async function nameRules(page, scanFindings) {
    const wanted = scanFindings.filter((f) => f.q && f.prop);
    if (!wanted.length) return;
    let client;
    try {
      client = await page.context().newCDPSession(page);
      const sheets = new Map();
      client.on("CSS.styleSheetAdded", ({ header }) => sheets.set(header.styleSheetId, header));
      await client.send("DOM.enable");
      await client.send("CSS.enable");
      const { root } = await client.send("DOM.getDocument", { depth: 0 });
      const { nodeIds } = await client.send("DOM.querySelectorAll", { nodeId: root.nodeId, selector: "[data-r2-q]" });
      const byQ = new Map(wanted.map((f) => [f.q, f]));
      for (const nodeId of nodeIds) {
        const { attributes } = await client.send("DOM.getAttributes", { nodeId });
        const qs = (attributes[attributes.indexOf("data-r2-q") + 1] || "").split(" ");
        let matched;
        try { matched = await client.send("CSS.getMatchedStylesForNode", { nodeId }); } catch { continue; }
        for (const q of qs) {
          const f = byQ.get(q);
          if (!f) continue;
          const hit = pickRule(matched, f.prop);
          if (!hit) { f.rule = { sheet: "(inherited/default)", selector: "", value: "" }; continue; }
          if (hit.inline) { f.rule = { sheet: "inline style attribute (React style / sx)", selector: "", value: hit.p.value }; continue; }
          const header = sheets.get(hit.r.styleSheetId) || {};
          const url = header.sourceURL || "";
          let sheet;
          if (/\/_next\/static\/.*\.css/.test(url)) sheet = cssIndex(hit.r.selectorList.text) || url.replace(/^.*\/_next\//, "_next/");
          else if (!url || header.isInline) sheet = /^\.css-/.test(hit.r.selectorList.text) ? "emotion (MUI sx/styled)" : "inline <style>";
          else sheet = url.replace(/^https?:\/\/[^/]+\//, "");
          f.rule = { sheet, selector: hit.r.selectorList.text.slice(0, 160), value: hit.p.value };
        }
      }
    } catch (e) {
      log("  cdp rule naming failed:", String(e.message).slice(0, 120));
    } finally {
      await client?.detach().catch(() => {});
    }
  }
  function pickRule(matched, prop) {
    const names = PROP_ALIASES[prop] || [prop];
    const search = (rules, inline) => {
      let best = null;
      for (let i = (rules || []).length - 1; i >= 0; i--) {
        const r = rules[i].rule;
        if (r.origin === "user-agent") continue;
        const p = [...r.style.cssProperties].reverse().find((x) => names.includes(x.name) && !x.disabled && x.parsedOk !== false && x.value);
        if (p) { if (p.important) return { r, p }; best ??= { r, p }; }
      }
      const ip = inline?.cssProperties?.find((x) => names.includes(x.name) && x.value);
      if (ip) return { inline: true, p: ip };
      return best;
    };
    const own = search(matched.matchedCSSRules, matched.inlineStyle);
    if (own || prop !== "color") return own;
    for (const inh of matched.inherited || []) { const h = search(inh.matchedCSSRules, inh.inlineStyle); if (h) return h; }
    return null;
  }

  // ---- template captures (cached) -----------------------------------------------------------
  const templateResolved = new Map();
  async function resolveTemplatePath(tpl) {
    if (!/\[/.test(tpl)) return tpl;
    if (templateResolved.has(tpl)) return templateResolved.get(tpl);
    const p = (async () => {
      const parent = tpl.split("/[")[0];
      const ctx = await newContext(PROFILES[0], true);
      try {
        const page = await ctx.newPage();
        await page.goto(templateBase + parent, { waitUntil: "networkidle", timeout: 30000 }).catch(() => {});
        const hrefs = await page.evaluate(() => [...document.querySelectorAll("a[href]")].map((a) => a.getAttribute("href"))).catch(() => []);
        const re = new RegExp("^" + tpl.replace(/\[[^\]]+\]/g, "([^/?#]+)") + "$");
        const cand = hrefs.filter((h) => re.test(h || "")).find((h) => !/\/(list|new|cards|create|account|profile)$/.test(h));
        return cand || parent;
      } finally { await ctx.close(); }
    })();
    templateResolved.set(tpl, p);
    return p;
  }
  const templateShots = new Map();
  function templateShot(tpl, profile) {
    const key = `${tpl}|${profile.label}`;
    if (!templateShots.has(key)) {
      templateShots.set(key, (async () => {
        const path = await resolveTemplatePath(tpl);
        const ctx = await newContext(profile, true);
        try {
          const page = await ctx.newPage();
          await page.goto(templateBase + path, { waitUntil: "networkidle", timeout: 40000 }).catch(() => {});
          await sleep(600);
          const file = join(outDir, "shots", `tpl_${slug(path)}__${profile.label}.jpg`);
          await page.screenshot({ path: file, type: "jpeg", quality: 80 });
          return { file, path };
        } catch { return null; } finally { await ctx.close(); }
      })());
    }
    return templateShots.get(key);
  }
  async function sideBySide(left, right, leftLabel, rightLabel, outFile, boxes) {
    const [a, b] = await Promise.all([sharp(left).metadata(), sharp(right).metadata()]);
    const gap = 16, head = 28;
    const W = a.width + b.width + gap, H = Math.max(a.height, b.height) + head;
    const esc = (s) => String(s).replace(/[<>&"]/g, (c) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;" })[c]);
    let overlay = `<svg width="${W}" height="${H}" xmlns="http://www.w3.org/2000/svg"><rect width="${W}" height="${head}" fill="black"/><text x="8" y="19" font-family="Helvetica" font-size="14" fill="white">${esc(leftLabel)}</text><text x="${a.width + gap + 8}" y="19" font-family="Helvetica" font-size="14" fill="white">${esc(rightLabel)}</text>`;
    for (const bx of boxes || []) {
      const dx = bx.side === "right" ? a.width + gap : 0;
      overlay += `<rect x="${bx.x + dx}" y="${bx.y + head}" width="${bx.w}" height="${bx.h}" fill="none" stroke="${bx.bad ? "red" : "lime"}" stroke-width="2"/><text x="${bx.x + dx + 4}" y="${bx.y + head + 14}" font-family="Helvetica" font-size="12" fill="${bx.bad ? "red" : "lime"}">${esc(bx.label || "")}</text>`;
    }
    overlay += "</svg>";
    await sharp({ create: { width: W, height: H, channels: 3, background: { r: 40, g: 40, b: 40 } } })
      .composite([{ input: left, left: 0, top: head }, { input: right, left: a.width + gap, top: head }, { input: Buffer.from(overlay), left: 0, top: 0 }])
      .jpeg({ quality: 80 })
      .toFile(outFile);
  }
  async function contactSheet(frames, outFile) {
    const pick = frames.length > 12 ? frames.filter((_, i) => i % Math.ceil(frames.length / 12) === 0).slice(0, 12) : frames;
    if (!pick.length) return null;
    const imgs = await Promise.all(pick.map((f) => sharp(Buffer.from(f, "base64")).resize({ width: 360 }).jpeg({ quality: 70 }).toBuffer({ resolveWithObject: true })));
    const cw = 360, ch = Math.max(...imgs.map((i) => i.info.height)), cols = 4, rows = Math.ceil(imgs.length / cols);
    await sharp({ create: { width: cols * cw, height: rows * ch, channels: 3, background: { r: 0, g: 0, b: 0 } } })
      .composite(imgs.map((im, i) => ({ input: im.data, left: (i % cols) * cw, top: Math.floor(i / cols) * ch })))
      .jpeg({ quality: 70 }).toFile(outFile);
    return outFile;
  }
  const rel = (p) => (p ? relative(outDir, p) : null);

  // ---- template drawer reference -------------------------------------------------------------
  const templateDrawer = { widths: [...TEMPLATE_DRAWER_WIDTHS], shot: null };
  if (checks.has("drawers")) {
    const ctx = await newContext(PROFILES[0], true);
    try {
      const page = await ctx.newPage();
      await page.goto(templateBase + "/dashboard/kanban", { waitUntil: "networkidle", timeout: 30000 });
      await page.locator(".minimal__kanban__item__root").first().click({ timeout: 5000 });
      await page.waitForSelector(".MuiDrawer-paper", { timeout: 5000 });
      await sleep(500);
      const info = await page.evaluate(() => window.__r2lib.overlayInfo());
      if (info?.width) templateDrawer.widths = [...new Set([320, info.width])];
      templateDrawer.shot = join(outDir, "drawers", "template_kanban-details__1440-dark.jpg");
      await page.screenshot({ path: templateDrawer.shot, type: "jpeg", quality: 80 });
      templateDrawer.backdrop = info?.backdrop;
      log(`template drawer reference: widths ${templateDrawer.widths.join("/")}, backdrop ${info?.backdrop}`);
    } catch (e) {
      log("template drawer reference failed, using constants:", String(e.message).split("\n")[0]);
    } finally { await ctx.close(); }
  }

  // ---- jobs ------------------------------------------------------------------------------------
  async function scanJob(route, path, profile) {
    const info = routeInfo.get(route.route);
    const ctx = await newContext(profile, false);
    try {
      const page = await ctx.newPage();
      const url = urlFor(path);
      const res = await gotoSettled(page, url);
      if (profile.label === "1440-dark") { info.url = url; info.status = res.error ? "error" : "ok"; }
      const moved = redirected(page, route);
      if (moved) { info.status = "redirect"; info.redirectTo = moved; return; }
      if (res.error || res.status >= 400) {
        info.errors.push(`${profile.label}: ${res.error || "HTTP " + res.status}`);
        add({ check: "route", pattern: "route|error", label: "Route fails to render (error / HTTP >= 400)", route: route.route, profile: profile.label, detail: res.error || `HTTP ${res.status}` });
        if (!res.status) return;
      }
      if (res.stuckSkeleton) add({ check: "route", pattern: "route|stuck-skeleton", label: "Skeleton still visible after 15s", route: route.route, profile: profile.label, detail: `${res.stuckSkeleton} skeleton elements` });
      if (profile.label === "1440-dark") for (const l of await page.evaluate(() => window.__r2lib.links()).catch(() => [])) collectedLinks.add(l);
      if (checks.has("scan")) {
        const out = await safeEval(page, (o) => window.__r2lib.scan(o), { theme: profile.theme, tap: profile.mobile, lightOnly: LIGHT_ONLY_HEXES });
        await nameRules(page, out.findings);
        for (const f of out.findings) recordScan(f, route.route, profile.label, "page");
        if (out.sideways) add({ check: "scan", pattern: `sideways-scroll|${profile.mobile ? "390" : "1440"}`, label: `Page scrolls sideways at ${profile.width}`, route: route.route, profile: profile.label, detail: `document ${out.sideways.scrollWidth}px wide in a ${out.sideways.viewport}px viewport` });
      }
      // ssr-scheme (N4, TR-2): the SERVER paint in dark, before React hydrates. Chart colours read in
      // JS are the light scheme until the client knows the mode, so a legend painted from them shows
      // light-only greens in dark on every slow load (TR-2 caught it only at 390, by timing). Load the
      // page with the Next.js client chunks blocked (inline scripts still set data-theme) and scan
      // the chart marks for light-only hexes: deterministic, not a race.
      if (checks.has("scan") && profile.theme === "dark" && LIGHT_ONLY_HEXES.length) {
        const sctx = await newContext(profile, false);
        try {
          await sctx.route(/\/_next\/static\/chunks\//, (r) => r.abort());
          const sp = await sctx.newPage();
          await sp.goto(url, { waitUntil: "load", timeout: 45000 }).catch(() => {});
          const hits = await sp.evaluate(([scope, light]) => {
            const out = [];
            const hex = (v) => { const m = /rgba?\(([\d.]+),\s*([\d.]+),\s*([\d.]+)(?:,\s*([\d.]+))?/.exec(v || ""); return m && (m[4] === undefined || +m[4] > 0.2) ? "#" + [m[1], m[2], m[3]].map((n) => Math.round(+n).toString(16).padStart(2, "0")).join("") : null; };
            for (const el of document.querySelectorAll(scope)) {
              const cs = getComputedStyle(el);
              for (const [prop, val] of [["background-color", cs.backgroundColor], ["color", cs.color], ["fill", cs.fill]]) {
                const h = hex(val);
                if (h && light.includes(h)) out.push({ prop, value: h, sig: el.className && typeof el.className === "string" ? el.className.split(" ")[0] : el.tagName.toLowerCase() });
              }
            }
            return out;
          }, [CHART_SCHEME_SCOPE_SERVER, LIGHT_ONLY_HEXES]).catch(() => []);
          const seenHex = new Set();
          for (const h of hits) {
            if (seenHex.has(h.value)) continue;
            seenHex.add(h.value);
            add({ check: "scan", route: route.route, profile: profile.label, pattern: `chart-light-scheme|${h.value}`, label: `Light-scheme colour ${h.value} on a dark chart before hydration (${h.sig})`, detail: `${h.prop} ${h.value} in the server paint (client chunks blocked)` });
          }
        } finally { await sctx.close(); }
      }
      for (const plug of plugins) {
        if (plug.profiles && !plug.profiles.includes(profile.label)) continue;
        try {
          for (const f of (await plug.run(page, { route: route.route, profile, theme: profile.theme, outDir })) || []) add({ check: plug.name, route: route.route, profile: profile.label, ...f, pattern: `${plug.name}|${f.pattern ?? f.label}`, p0: f.p0 ?? plug.p0 ?? false });
        } catch (e) { add({ check: "audit", severity: "info", pattern: "audit|plugin-error", label: "Audit plugin error", route: route.route, detail: `${plug.name}: ${String(e.message).slice(0, 160)}` }); }
      }
      if (checks.has("sbs")) {
        const shot = join(outDir, "shots", `${slug(route.route)}__${profile.label}.jpg`);
        await page.screenshot({ path: shot, type: "jpeg", quality: 80 });
        const tpl = await templateShot(info.template, profile);
        if (tpl) {
          const sbs = join(outDir, "sbs", `${slug(route.route)}__${profile.label}.jpg`);
          await sideBySide(shot, tpl.file, `ours ${path} (${profile.label})`, `template ${tpl.path}`, sbs).catch(() => {});
          (info.sbs ??= {})[profile.label] = rel(sbs);
        }
      }
    } finally { await ctx.close(); }
  }

  function recordScan(f, route, profile, context) {
    // emotion class hashes differ per sx object, so group emotion-set colours by the component signature
    const loc = f.rule ? (/^emotion/.test(f.rule.sheet) ? `emotion sx/styled on ${f.sig}` : `${f.rule.sheet}${f.rule.selector ? " `" + f.rule.selector + "`" : ""}`) : null;
    const where = context === "drawer" ? " (in drawer)" : "";
    const base = { check: f.check, route, profile, context, detail: `${f.detail} — ${f.path}${f.text ? ` "${f.text}"` : ""}`, rule: f.rule ? { ...f.rule, loc } : undefined, count: f.count };
    if (f.check === "off-palette") {
      const key = loc ? `${f.prop.replace(/-(top|right|bottom|left)-/, "-")}|${f.value}|${loc}` : `${f.prop}|${f.value}|${f.sig}`;
      add({ ...base, pattern: `off-palette|${key}`, label: `Off-palette ${f.prop.replace(/-(top|right|bottom|left)-/, "-")} ${f.value}${loc ? ` set by ${loc}` : ` on ${f.sig}`}` });
    } else if (f.check === "dark-bright-bg") {
      add({ ...base, pattern: `dark-bright-bg|${f.sig}|${loc || f.value}${where}`, label: `Bright background in dark${where}: ${f.sig} ${f.value}${loc ? ` (${loc})` : ""}` });
    } else if (f.check === "contrast") {
      add({ ...base, pattern: `contrast|${f.sig}|${f.value}`, label: `Text contrast below WCAG${where}: ${f.sig} ${f.value} (${profile.includes("light") ? "light" : "dark"})` });
    } else if (f.check === "chart-light-scheme") {
      add({ ...base, pattern: `chart-light-scheme|${f.value}`, label: `Light-scheme colour ${f.value} on a dark chart (${f.sig})` });
    } else if (f.check === "chart-black") {
      add({ ...base, pattern: `chart-black|${f.sig}`, label: `Chart series painted black (unresolved colour)${where}: ${f.sig}` });
    } else if (f.check === "tap-target") {
      add({ ...base, pattern: `tap|${f.sig}`, label: `Tap target < 44px at 390${where}: ${f.sig}` });
    }
  }

  async function interactJob(route, path, profile = PROFILES[0]) {
    const ctx = await newContext(profile, false);
    const info = routeInfo.get(route.route);
    try {
      const page = await ctx.newPage();
      const url = urlFor(path);
      const res = await gotoSettled(page, url);
      if (res.error || redirected(page, route)) return;
      const client = await ctx.newCDPSession(page);
      let frames = [];
      client.on("Page.screencastFrame", ({ data, sessionId }) => { frames.push(data); client.send("Page.screencastFrameAck", { sessionId }).catch(() => {}); });
      await page.waitForFunction(() => window.__r2lib.quiet(800), null, { timeout: 5000, polling: 200 }).catch(() => {});
      const first = await safeEval(page, ([t, f]) => window.__r2lib.interactionCandidates(t, f), [6, maxInteractions], []);
      const tabs = first.filter((c) => c.kind === "tab");
      if (process.env.R2_DEBUG) log("candidates", route.route, JSON.stringify(first), await page.evaluate(() => [...document.querySelectorAll("[role=tab]")].map((t) => `${t.getAttribute("aria-selected")}:${t.checkVisibility({ opacityProperty: true, visibilityProperty: true })}:${!!t.closest(".minimal__layout__main__content, main")}`).join(" ")));
      const filters = first.filter((c) => c.kind !== "tab");
      let done = 0; const results = [];
      const run = async (cand, reloadFirst) => {
        if (done >= maxInteractions) return;
        if (reloadFirst) await gotoSettled(page, url);
        const now = await safeEval(page, ([t, f]) => window.__r2lib.interactionCandidates(t, f), [6, maxInteractions], []);
        const cur = now.find((c) => c.kind === cand.kind && c.label === cand.label && (c.href ?? null) === (cand.href ?? null)) || now.find((c) => c.kind === cand.kind && c.label === cand.label);
        if (!cur) return;
        done++;
        if (!(await safeEval(page, (id) => !!window.__r2lib.armWatch(id), cur.id, false))) return;
        frames = [];
        await client.send("Page.startScreencast", { format: "jpeg", quality: 50, maxWidth: 720, everyNthFrame: 1 }).catch(() => {});
        const loc = page.locator(`[data-r2-i="${cur.id}"]`);
        try {
          if (cur.kind === "filter-native-select") {
            const opts = await loc.evaluate((s) => [...s.options].map((o) => o.value));
            const curVal = await loc.inputValue();
            const next = opts.find((v) => v !== curVal);
            if (next !== undefined) await loc.selectOption(next, { timeout: 4000 });
          } else if (cur.kind === "filter-select") {
            await loc.click({ timeout: 4000 });
            const opt = page.locator("[role=listbox] [role=option]:not([aria-selected=true]):not(.Mui-disabled)").first();
            await opt.click({ timeout: 3000 });
          } else if (profile.mobile) {
            // 390: a finger tap (touch events + the synthesized click), as a phone user presses it.
            await loc.tap({ timeout: 3000 }).catch(async () => { await loc.evaluate((el) => el.click()); });
          } else {
            await loc.click({ timeout: 3000 }).catch(async () => { await loc.evaluate((el) => el.click()); });
          }
        } catch (e) {
          await client.send("Page.stopScreencast").catch(() => {});
          await page.keyboard.press("Escape").catch(() => {});
          await page.evaluate(() => window.__r2lib.readWatch()).catch(() => null);
          results.push({ ...cur, error: String(e.message).split("\n")[0].slice(0, 120) });
          return;
        }
        await sleep(250);
        await page.waitForFunction(() => window.__r2lib && window.__r2lib.settled(500), null, { timeout: 9000, polling: 100 }).catch(() => {});
        await client.send("Page.stopScreencast").catch(() => {});
        let w = await safeEval(page, () => window.__r2lib.readWatch(), undefined, null);
        // no watch state left = the click loaded a NEW DOCUMENT (plain <a href>, not a client transition)
        const reloaded = !w;
        if (reloaded) w = { maxSkel: 0, headerGone: true, tabsGone: true, cls: 0, headerShift: 0, tabsShift: 0, skelWhileHeaderGone: false, url: page.url(), urlChanged: page.url() !== url };
        const routeChange = w.urlChanged && new URL(w.url).pathname !== new URL(url).pathname;
        const kindLabel = cur.kind === "tab" ? (routeChange ? "Route tab" : "Tab") : cur.kind === "filter-link" ? "Filter link" : cur.kind === "filter-toggle" ? "Filter toggle/chip" : "Filter select";
        const fails = [];
        if (reloaded) fails.push(["full-reload", "full document reload (whole page, shell and header re-render)"]);
        else if (w.maxSkel >= 0.45 || w.skelWhileHeaderGone) fails.push(["skeleton-flash", `full-page skeleton flash (${Math.round(w.maxSkel * 100)}% of the content column)`]);
        if (w.headerGone && !reloaded) fails.push(["header-remount", "page header remounted"]);
        if (w.tabsGone && !reloaded) fails.push(["tabs-remount", "tabs remounted"]);
        if (!w.headerGone && (w.headerShift > 4 || w.tabsShift > 4)) fails.push(["layout-jump", `header/tabs moved ${Math.max(w.headerShift, w.tabsShift)}px`]);
        if (w.cls > 0.1) fails.push(["layout-shift", `layout shift ${w.cls}`]);
        if (!reloaded && !routeChange && scrollJumped(w.scroll)) fails.push(["scroll-jump", `the page scrolled ${Math.abs(w.scroll.nowScroll - w.scroll.clickScroll)}px after the click (scroll ${w.scroll.clickScroll} -> ${w.scroll.nowScroll})`]);
        // url-keyed-panel: a same-route navigation must move the tab and show the panel skeleton
        // (or the answer) within 150ms — never the old panel frozen under a new tab.
        if (!reloaded && w.urlChanged && !routeChange && w.early) {
          if (cur.kind === "tab" && w.early.selected === false) fails.push(["tab-not-selected", "the pressed tab was not selected within 150ms"]);
          if (!w.early.landed && !w.early.skel) fails.push(["stale-panel", "150ms after the click the panel still showed the old content without a skeleton (the tab transition hangs)"]);
        }
        if (!reloaded && !routeChange) fails.push(...fallbackTwinFails(w));
        let evidence = null;
        if (fails.length) evidence = rel(await contactSheet(frames, join(outDir, "frames", `${slug(route.route)}__${done}_${cur.kind}.jpg`)).catch(() => null));
        for (const [code, msg] of fails) add({ check: "interact", pattern: `interact|${kindLabel}|${code}`, label: `${kindLabel} click → ${msg.replace(/ \(.*\)$| \d+(\.\d+)?px$| [\d.]+$/, "")}`, route: route.route, profile: profile.label, detail: `"${cur.label}" ${cur.href ?? ""} → ${msg}`, evidence });
        results.push({ kind: cur.kind, label: cur.label, href: cur.href, watch: w, fails: fails.map((f) => f[0]), evidence });
        if (routeChange) await gotoSettled(page, url);
        for (const l of await page.evaluate(() => window.__r2lib.links()).catch(() => [])) collectedLinks.add(l);
      };
      // tabs are re-listed before every click: panels stream in late and each click changes the selected tab
      const triedTabs = new Set();
      for (let k = 0; k < 6; k++) {
        const list = await safeEval(page, ([t, f]) => window.__r2lib.interactionCandidates(t, f), [8, maxInteractions], []);
        const t = list.find((c) => c.kind === "tab" && !triedTabs.has(c.label)) || (k === 0 ? tabs.find((c) => !triedTabs.has(c.label)) : null);
        if (!t) break;
        triedTabs.add(t.label);
        await run(t, false);
      }
      let firstFilter = true;
      for (const f of filters) { await run(f, page.url() !== url || (firstFilter && triedTabs.size > 0)); firstFilter = false; }
      info.interactions = results;
    } finally { await ctx.close(); }
  }

  async function drawerJob(route, path) {
    const profile = PROFILES[0];
    const ctx = await newContext(profile, false);
    const info = routeInfo.get(route.route);
    try {
      const page = await ctx.newPage();
      const url = urlFor(path);
      const res = await gotoSettled(page, url);
      if (res.error || redirected(page, route)) return;
      const triggers = await safeEval(page, () => window.__r2lib.drawerTriggers(4), undefined, []);
      const results = [];
      for (const [n, trig] of triggers.entries()) {
        // reload only when the previous overlay did not close or the URL moved
        if (n > 0 && ((await safeEval(page, () => !!window.__r2lib.overlayInfo(), undefined, true)) || page.url() !== url)) await gotoSettled(page, url);
        const now = await safeEval(page, () => window.__r2lib.drawerTriggers(4), undefined, []);
        const cur = now.find((c) => c.label === trig.label && c.kind === trig.kind);
        if (!cur) continue;
        const dl = page.locator(`[data-r2-d="${cur.id}"]`);
        try { await dl.click({ timeout: 3000 }).catch(async () => { await dl.evaluate((el) => el.click()); }); } catch { continue; }
        await page.waitForSelector(".MuiDrawer-paper, .MuiDialog-paper, [role=dialog]", { state: "visible", timeout: 3000 }).catch(() => {});
        await sleep(700);
        if (new URL(page.url()).pathname !== new URL(url).pathname) { for (const l of [page.url()]) collectedLinks.add(l); continue; }
        const o = await page.evaluate(() => window.__r2lib.overlayInfo()).catch(() => null);
        if (!o) continue;
        for (const l of await page.evaluate(() => window.__r2lib.links()).catch(() => [])) collectedLinks.add(l);
        const shot = join(outDir, "drawers", `${slug(route.route)}__${n + 1}.jpg`);
        await page.screenshot({ path: shot, type: "jpeg", quality: 80 });
        let evidence = rel(shot);
        if (templateDrawer.shot && o.kind === "drawer") {
          const sbs = join(outDir, "drawers", `${slug(route.route)}__${n + 1}__vs_template.jpg`);
          await sideBySide(shot, templateDrawer.shot, `ours ${route.route} "${cur.label}" ${o.width}px`, `template kanban details ${templateDrawer.widths.join("/")}px`, sbs).then(() => { evidence = rel(sbs); }).catch(() => {});
        }
        const what = `${o.kind} "${o.title || cur.label}"`;
        if (o.kind === "drawer" && (o.anchor === "right" || o.anchor === "left")) {
          const near = templateDrawer.widths.reduce((b, w) => (Math.abs(w - o.width) < Math.abs(b - o.width) ? w : b), templateDrawer.widths[0]);
          if (Math.abs(near - o.width) > 8) add({ check: "drawers", pattern: `drawer|width|${o.width < near ? "narrower" : "wider"}`, label: `Drawer width off template (${templateDrawer.widths.join("/")}px): ${o.width < near ? "narrower" : "wider"}`, route: route.route, profile: profile.label, detail: `${what} ${o.width}px vs template ${near}px`, evidence });
        }
        if (!o.backdrop) add({ check: "drawers", pattern: `drawer|no-backdrop|${o.kind}`, label: `${o.kind === "drawer" ? "Drawer" : "Dialog"} opens without a backdrop`, route: route.route, profile: profile.label, detail: `${what} (${o.sig})`, evidence });
        for (const ov of o.overflow) add({ check: "drawers", pattern: `drawer|overflow|${ov.soft ? "table-scroll" : "clipped"}`, label: ov.soft ? "Table squeezed into a sideways-scrolling drawer" : "Drawer/dialog content overflows or is clipped", route: route.route, profile: profile.label, detail: `${what}: ${ov.sig} ${ov.detail}`, evidence });
        if (checks.has("scan")) {
          const out = await page.evaluate((o2) => window.__r2lib.scan(o2), { theme: "dark", tap: false, scope: "[data-r2-paper]" });
          await nameRules(page, out.findings);
          for (const f of out.findings) if (f.check !== "off-palette") recordScan(f, route.route, profile.label, "drawer");
        }
        results.push({ trigger: cur.label, ...o, evidence });
        await page.keyboard.press("Escape").catch(() => {});
        await sleep(400);
      }
      info.overlays = results;
    } finally { await ctx.close(); }
  }

  async function skeletonJob(route, path, profile = PROFILES[0]) {
    const tag = profile === PROFILES[0] ? "" : `__${profile.label}`;
    const info = routeInfo.get(route.route);
    const ctx = await newContext(profile, false);
    try {
      const page = await ctx.newPage();
      const startRoute = route.route.startsWith("/leave") ? "/routines" : "/leave";
      const res = await gotoSettled(page, urlFor(startRoute));
      if (res.error) return;
      const target = urlFor(path);
      const targetRel = target.slice(base.length);
      let hold = false, release;
      const gate = new Promise((r) => { release = r; });
      await page.route("**/*", async (r) => {
        const h = r.request().headers();
        const isPrefetch = Object.keys(h).some((k) => /^next-router-(segment-)?prefetch$/.test(k));
        if (hold && h.rsc === "1" && !isPrefetch) await withTimeout(gate, 8000);
        await r.continue().catch(() => {});
      });
      await page.evaluate((u) => window.next?.router?.prefetch?.(u), targetRel).catch(() => {});
      await sleep(1500);
      hold = true;
      // --skeleton-nav click: a link click (the shell paints the target's registry skeleton, as a
      // sidebar click does); works on `next dev`, where nothing is prefetched. Default: router.push.
      const pushed = await page.evaluate(([u, click]) => {
        if (!window.next?.router?.push) return false;
        if (!click) { window.next.router.push(u); return true; }
        const a = document.createElement("a");
        a.href = u; a.textContent = "r2 skeleton nav"; a.style.cssText = "position:fixed;left:0;top:0;opacity:0";
        a.addEventListener("click", (e) => { e.preventDefault(); window.next.router.push(u); a.remove(); });
        // in a [data-page-header] wrapper: the shell starts no back trail (as router.push)
        const w = document.createElement("div"); w.dataset.pageHeader = ""; w.appendChild(a); document.body.appendChild(w); a.click(); w.remove(); return true;
      }, [targetRel, skeletonNav === "click"]).catch(() => false);
      if (!pushed) { release(); add({ check: "skeleton", severity: "info", pattern: "skeleton|no-router", label: "Skeleton not captured (no client router)", route: route.route, profile: profile.label, detail: "window.next.router missing" }); return; }
      const seen = await page.waitForFunction(() => window.__r2lib.skeletonCount() > 0, null, { timeout: 4000, polling: 50 }).then(() => true).catch(() => false);
      let skel = [], skelShot = null;
      if (seen) {
        await sleep(300);
        skel = await safeEval(page, () => window.__r2lib.blocks(), undefined, []);
        skelShot = join(outDir, "skeleton", `${slug(route.route)}${tag}__skeleton.jpg`);
        await page.screenshot({ path: skelShot, type: "jpeg", quality: 80 });
      }
      hold = false; release();
      await page.waitForURL((u) => new URL(u).pathname === new URL(target).pathname, { timeout: 15000 }).catch(() => {});
      await page.waitForFunction(() => window.__r2lib && window.__r2lib.settled(500), null, { timeout: 15000, polling: 150 }).catch(() => {});
      if (redirected(page, route)) return;
      if (!seen) {
        add({ check: "skeleton", pattern: "skeleton|not-shown", label: "No loading skeleton on navigation (old page stays until data arrives)", route: route.route, profile: profile.label, detail: `navigating ${startRoute} → ${route.route} with the RSC response held showed no skeleton${route.hasLoading ? "" : " (route has no loading.tsx)"}` });
        return;
      }
      const loaded = await safeEval(page, () => window.__r2lib.blocks(), undefined, []);
      const loadedShot = join(outDir, "skeleton", `${slug(route.route)}${tag}__loaded.jpg`);
      await page.screenshot({ path: loadedShot, type: "jpeg", quality: 80 });
      const cmp = compareBlocks(skel, loaded);
      const boxes = [
        ...skel.map((b) => ({ ...b, side: "left", bad: cmp.extra.includes(b) || cmp.mismatched.some((m) => m.skel === b), label: b.kind })),
        ...loaded.map((b) => ({ ...b, side: "right", bad: cmp.missing.includes(b) || cmp.mismatched.some((m) => m.loaded === b), label: b.kind })),
      ];
      const sbs = join(outDir, "skeleton", `${slug(route.route)}${tag}__skeleton_vs_loaded.jpg`);
      await sideBySide(skelShot, loadedShot, `skeleton ${route.route}`, "loaded", sbs, boxes).catch(() => {});
      const evidence = rel(sbs);
      for (const m of cmp.mismatched) add({ check: "skeleton", pattern: `skeleton|mismatch|${m.loaded.kind}`, label: `Skeleton block shape ≠ loaded (IoU < 0.8): ${m.loaded.kind}`, route: route.route, profile: profile.label, detail: `IoU ${m.iou}: skeleton ${m.skel.w}x${m.skel.h}@${m.skel.x},${m.skel.y} vs loaded ${m.loaded.w}x${m.loaded.h}@${m.loaded.x},${m.loaded.y} (${m.loaded.sig})`, evidence });
      for (const l of cmp.missing) add({ check: "skeleton", pattern: `skeleton|missing|${l.kind}`, label: `Loaded block has no skeleton counterpart: ${l.kind}`, route: route.route, profile: profile.label, detail: `${l.w}x${l.h}@${l.x},${l.y} ${l.sig}`, evidence });
      for (const s of cmp.extra) add({ check: "skeleton", pattern: "skeleton|extra", label: "Skeleton block with no loaded counterpart", route: route.route, profile: profile.label, detail: `${s.w}x${s.h}@${s.x},${s.y} ${s.sig}`, evidence });
      info.skeleton = { blocks: { skeleton: skel.length, loaded: loaded.length }, matches: cmp.matches.map((m) => ({ kind: m.loaded.kind, iou: m.iou })), missing: cmp.missing.length, extra: cmp.extra.length, evidence };
    } finally { await ctx.close(); }
  }

  async function pool(jobs) {
    let next = 0, done = 0;
    const worker = async () => {
      while (next < jobs.length) {
        const j = jobs[next++];
        const t = Date.now();
        try { await withTimeout(j.run(), 240000, "timeout").then((r) => { if (r === "timeout") throw new Error("job timeout 240s"); }); }
        catch (e) {
          routeInfo.get(j.route)?.errors.push(`${j.name}: ${String(e.message).split("\n")[0].slice(0, 160)}`);
          add({ check: "audit", severity: "info", pattern: "audit|job-error", label: "Audit job error", route: j.route, detail: `${j.name}: ${String(e.message).split("\n")[0].slice(0, 160)}` });
        }
        done++;
        log(`${String(done).padStart(3)}/${jobs.length} ${j.name} ${j.route} ${((Date.now() - t) / 1000).toFixed(1)}s`);
      }
    };
    await Promise.all(Array.from({ length: Math.min(concurrency, jobs.length) }, worker));
  }
  const jobsFor = (route, path) => {
    const js = [];
    if (checks.has("interact")) for (const p of interactProfiles) js.push({ name: p === PROFILES[0] ? "interact" : `interact ${p.label}`, route: route.route, run: () => interactJob(route, path, p) });
    if (checks.has("drawers")) js.push({ name: "drawers", route: route.route, run: () => drawerJob(route, path) });
    if (checks.has("skeleton") || skeletonTouched?.has(route.route)) for (const p of skeletonProfiles) js.push({ name: `skeleton ${p.label}`, route: route.route, run: () => skeletonJob(route, path, p) });
    if (checks.has("scan") || checks.has("sbs")) for (const p of PROFILES) js.push({ name: `scan ${p.label}`, route: route.route, run: () => scanJob(route, path, p) });
    return js;
  };

  // Phase A: static routes (redirect-only pages get one scan to record the redirect).
  const staticRoutes = routes.filter((r) => !r.dynamic);
  const jobsA = [];
  for (const r of staticRoutes) {
    if (r.redirectOnly) jobsA.push({ name: "scan 1440-dark", route: r.route, run: () => scanJob(r, r.route, PROFILES[0]) });
    else jobsA.push(...jobsFor(r, r.route));
  }
  const jobRank = (j) => (j.name.startsWith("interact") ? 0 : j.name === "drawers" ? 1 : 2);
  jobsA.sort((a, b) => jobRank(a) - jobRank(b));
  log(`phase A: ${staticRoutes.length} static routes, ${jobsA.length} jobs, concurrency ${concurrency}`);
  await pool(jobsA);

  // Phase B: dynamic routes with sample ids from the pages' own links.
  const jobsB = [];
  for (const r of routes.filter((x) => x.dynamic)) {
    const counts = new Map();
    for (const l of collectedLinks) {
      let u; try { u = new URL(l); } catch { continue; }
      if (u.origin !== new URL(base).origin || !r.pattern.test(u.pathname)) continue;
      counts.set(u.pathname + u.search, (counts.get(u.pathname + u.search) ?? 0) + 1);
    }
    const sample = [...counts.entries()].sort((a, b) => b[1] - a[1])[0]?.[0];
    const info = routeInfo.get(r.route);
    if (!sample) {
      info.status = "no-sample";
      add({ check: "route", severity: "info", pattern: "route|no-sample-id", label: "Dynamic route not audited: no link with a sample id found", route: r.route, detail: "no page linked to this route during the crawl" });
      continue;
    }
    info.sample = sample;
    jobsB.push(...jobsFor(r, sample));
  }
  if (jobsB.length) { log(`phase B: ${jobsB.length} jobs for dynamic routes`); await pool(jobsB); }
  await browser.close();

  // ---- report ----------------------------------------------------------------------------------
  const patterns = groupPatterns(findings);
  const failures = findings.filter((f) => f.severity !== "info");
  const byCheck = {};
  for (const p of patterns) (byCheck[p.check] ??= { patterns: 0, routes: new Set(), occurrences: 0 }), byCheck[p.check].patterns++, byCheck[p.check].occurrences += p.count, p.routes.forEach((r) => byCheck[p.check].routes.add(r));
  const report = {
    meta: {
      base, template: templateBase, out: outDir, startedAt: new Date(t0).toISOString(), durationSec: Math.round((Date.now() - t0) / 1000),
      profiles: PROFILES.map((p) => p.label), checks: [...checks], concurrency, query, pageMap: existsSync(pageMapPath) ? pageMapPath : null,
      templateDrawerWidths: templateDrawer.widths, routes: routes.length,
      audited: [...routeInfo.values()].filter((r) => r.status === "ok" || r.status === "error").length,
    },
    summary: {
      patterns: patterns.length, failures: failures.length,
      byCheck: Object.fromEntries(Object.entries(byCheck).map(([k, v]) => [k, { patterns: v.patterns, routes: v.routes.size, occurrences: v.occurrences }])),
    },
    patterns,
    routes: [...routeInfo.values()],
    findings,
  };
  let baseline = null, routeBaseline = null;
  if (!args.strict && existsSync(baselinePath)) { try { const b = JSON.parse(readFileSync(baselinePath, "utf8")); baseline = b.patterns ?? null; routeBaseline = b.routes ?? null; } catch {} }
  // a partial run (fast / --routes / --only) judges P0 growth only among the routes it audited
  const partial = !!(only || exactRoutes || (fast && strictRoutes));
  const scopedBaseline = baseline && partial && routeBaseline
    ? Object.fromEntries(Object.keys(baseline).map((k) => [k, routes.filter((r) => (routeBaseline[r.route] || []).includes(k)).length]))
    : baseline;
  if (partial && !routeBaseline) log("baseline has no per-route section yet: partial run judges P0 against whole-run counts (run a full audit with --write-baseline)");
  const gateFails = gateFailures(patterns, scopedBaseline, !!args.strict, { strictRoutes, routeBaseline });
  report.gate = { p0Patterns: patterns.filter((p) => p.p0).length, baseline: baseline ? baselinePath : null, strict: !!args.strict, fast, strictRoutes: strictRoutes ? [...strictRoutes] : null, failing: gateFails.map((p) => ({ pattern: p.pattern, label: p.label, routeCount: p.routeCount, routes: p.routes, why: p.why })) };
  if (args["write-baseline"]) {
    if (partial) log("refusing --write-baseline on a partial run (--fast / --routes / --only)");
    else {
      const prev = baseline ?? {};
      const next = Object.fromEntries(patterns.filter((p) => p.p0).map((p) => [p.pattern, p.routeCount]));
      const grown = Object.entries(next).filter(([k, v]) => k in prev && v > prev[k]);
      const perRoute = {};
      for (const p of patterns) for (const r of p.routes) (perRoute[r] ??= []).push(p.pattern);
      for (const r of Object.keys(perRoute)) perRoute[r].sort();
      writeFileSync(baselinePath, JSON.stringify({ note: "r2-visual-audit debt. patterns: P0 pattern -> route count; routes: route -> every failure pattern it showed (the per-route ratchet for shell + touched routes). Shrink-only: fix patterns and re-run a FULL audit with --write-baseline; never add or grow an entry to land a change.", base: base, generatedAt: new Date().toISOString(), patterns: next, routes: Object.fromEntries(Object.entries(perRoute).sort()) }, null, 2) + "\n");
      log(`wrote baseline ${baselinePath} (${Object.keys(next).length} P0 patterns, ${Object.keys(perRoute).length} routes${grown.length ? `, ${grown.length} GREW` : ""})`);
    }
  }
  writeFileSync(join(outDir, "report.json"), JSON.stringify(report, null, 2));
  writeFileSync(join(outDir, "report.md"), renderMarkdown(report));
  log(`done: ${patterns.length} failure patterns, ${failures.length} failures -> ${join(outDir, "report.md")}`);
  console.log(`R2AUDIT_REPORT ${join(outDir, "report.json")}`);
  if (gate) {
    if (gateFails.length) {
      console.error(`\nVISUAL GATE FAILED: ${gateFails.length} P0 pattern(s)${baseline ? " new or grown vs baseline" : ""}:`);
      for (const p of gateFails.slice(0, 40)) console.error(`  - [${p.routeCount} routes] ${p.label} (${p.why})`);
      console.error(`Report: ${join(outDir, "report.md")}`);
      process.exitCode = 1;
    } else console.log(`VISUAL GATE OK: no ${baseline ? "new or grown " : ""}P0 patterns`);
  }
}

export function renderMarkdown(report) {
  const { meta, summary, patterns, routes } = report;
  const esc = (s) => String(s ?? "").replace(/\|/g, "\\|").replace(/\n/g, " ");
  const lines = [];
  lines.push(`# R2 visual audit — ${meta.base}`, "");
  lines.push(`Template ${meta.template} · ${meta.routes} routes (${meta.audited} audited) · profiles ${meta.profiles.join(", ")} · checks ${meta.checks.join(", ")} · ${meta.durationSec}s · started ${meta.startedAt}`, "");
  lines.push(`**${summary.patterns} failure patterns, ${summary.failures} failures.** Template drawer widths: ${meta.templateDrawerWidths.join("/")}px.`, "");
  if (report.gate) lines.push(`Gate: ${report.gate.p0Patterns} P0 patterns; ${report.gate.failing.length} failing ${report.gate.baseline ? "(new or grown vs baseline)" : "(strict)"}.`, "", ...report.gate.failing.slice(0, 40).map((f) => `- GATE FAIL [${f.routeCount} routes] ${esc(f.label)} — ${f.why}`), "");
  lines.push("| Check | Patterns | Routes | Occurrences |", "|---|---:|---:|---:|");
  for (const [k, v] of Object.entries(summary.byCheck)) lines.push(`| ${k} | ${v.patterns} | ${v.routes} | ${v.occurrences} |`);
  lines.push("", "## Patterns ranked by route count", "", "| # | P0 | Pattern | Check | Routes | Occ. | Profiles | Example routes |", "|---:|---|---|---|---:|---:|---|---|");
  patterns.forEach((p, i) => lines.push(`| ${i + 1} | ${p.p0 ? "P0" : ""} | ${esc(p.label)} | ${p.check} | ${p.routeCount} | ${p.count} | ${p.profiles.join(", ")} | ${esc(p.routes.slice(0, 6).join(", "))}${p.routes.length > 6 ? ", …" : ""} |`));
  lines.push("", "## Pattern details (top 60)", "");
  for (const p of patterns.slice(0, 60)) {
    lines.push(`### ${esc(p.label)} — ${p.routeCount} routes`, "");
    if (p.examples[0]?.rule) lines.push(`Rule: \`${p.examples[0].rule.sheet}\` ${p.examples[0].rule.selector ? "`" + p.examples[0].rule.selector + "`" : ""} ${p.examples[0].rule.value ? "→ `" + p.examples[0].rule.value + "`" : ""}`, "");
    for (const e of p.examples.slice(0, 5)) lines.push(`- \`${e.route}\` ${e.profile ?? ""}: ${esc(e.detail)}${e.evidence ? ` ([evidence](${e.evidence}))` : ""}`);
    lines.push(`- routes: ${p.routes.join(", ")}`, "");
  }
  lines.push("## Routes", "", "| Route | Status | Template | Failures | Side by side (1440 dark) |", "|---|---|---|---:|---|");
  const perRoute = new Map();
  for (const f of report.findings) if (f.severity !== "info") perRoute.set(f.route, (perRoute.get(f.route) ?? 0) + 1);
  for (const r of routes) lines.push(`| \`${r.route}\`${r.sample ? ` (${esc(r.sample)})` : ""} | ${r.status}${r.redirectTo ? ` → ${r.redirectTo}` : ""}${r.errors.length ? ` ⚠ ${esc(r.errors[0])}` : ""} | ${r.template ?? ""} | ${perRoute.get(r.route) ?? 0} | ${r.sbs?.["1440-dark"] ? `[sbs](${r.sbs["1440-dark"]})` : ""} |`);
  const infos = report.findings.filter((f) => f.severity === "info");
  if (infos.length) {
    lines.push("", "## Not audited / info", "");
    for (const f of infos) lines.push(`- \`${f.route}\` ${f.label}: ${esc(f.detail)}`);
  }
  return lines.join("\n") + "\n";
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main().catch((e) => { console.error(e); process.exit(1); });
}
