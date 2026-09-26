// guard: url-keyed-panel (Ravi 2026-09-27: "tab switch is not smooth like the template. Now the tab
// transition HANGS. Just switch the tab and show shimmer for the content to load").
//
// A URL-driven control (a tab strip / segment / chip / select / pager / date filter whose state lives
// in a search param) navigates with router.push inside a transition. React keeps the OLD page on
// screen until the server answers, so a page that renders its data straight into the tree looks
// frozen for the whole round trip. The fix is structural: every data panel of such a page renders
// through `UrlSuspense` (components/app/url-suspense.tsx), keyed by the params it reads. Its client
// half swaps the panel to its skeleton the moment the click happens; the header, crumbs, tabs and
// filters stay mounted; the content streams in when the server answers.
//
// Static rule: for every admin page.tsx, walk the feature modules it renders (named imports resolved
// through index barrels, then every import of those files inside features/). If any of them renders a
// URL-driven control, one of them (or the page) must render <UrlSuspense>.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";

/** Markers of a control that changes a search param on the same page. */
export const URL_CONTROL_PATTERNS = [
  [/<SegmentedLinks\b/, "SegmentedLinks"],
  [/<LinkSelect\b/, "LinkSelect"],
  [/<TablePaginationLinks\b/, "TablePaginationLinks"],
  [/<WorklistFilters\b/, "WorklistFilters"],
  [/<FeedFilters\b/, "WorklistFilters (FeedFilters alias)"],
  [/<WindowDateFilter\b/, "WindowDateFilter"],
  [/\buseUrlTabNav\(/, "useUrlTabNav"],
  [/\buseUrlNavigate\(/, "useUrlNavigate"],
  [/\buseUrlSort\(/, "useUrlSort"],
];
/** A tab strip is URL-driven when its items carry hrefs (client-state tabs have none). */
const TAB_STRIP = /<(?:AnimatedTabs|SegmentTabs)\b/;
const HREF_ITEM = /\bhref\s*:/;

/**
 * Routes whose strips only move between ROUTES (each tab is its own page with its own loading.tsx),
 * or that have no data behind the strip. Each entry needs the reason; the list may only shrink.
 */
export const URL_KEYED_PANEL_EXEMPT = {
  "/vaccination/plan": "its only TablePaginationLinks is a hideActions footer label (one page, no hrefs); nothing on the page changes a search param",
};

/**
 * Routes still being converted (TABS3, PR #294). SHRINK-ONLY: a route here that already renders
 * UrlSuspense is itself a finding (drop it from the list), and nothing may be added.
 */
export const URL_KEYED_PANEL_PENDING = new Set([]);

function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " ")).replace(/(^|[^:"'`])\/\/.*$/gm, "$1");
}

/** Which URL-driven control a module renders, or null. */
export function urlControlIn(text) {
  const code = stripComments(text);
  for (const [pattern, name] of URL_CONTROL_PATTERNS) if (pattern.test(code)) return name;
  if (TAB_STRIP.test(code) && HREF_ITEM.test(code)) return "AnimatedTabs/SegmentTabs with hrefs";
  return null;
}

export function rendersUrlSuspense(text) {
  return /<UrlSuspense\b/.test(stripComments(text));
}

function resolveFile(base) {
  for (const candidate of [base, `${base}.tsx`, `${base}.ts`, join(base, "index.tsx"), join(base, "index.ts")]) {
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  }
  return null;
}

function resolveSpec(root, fromFile, spec) {
  if (spec.startsWith("@/")) return resolveFile(join(root, spec.slice(2)));
  if (spec.startsWith(".")) return resolveFile(resolve(dirname(fromFile), spec));
  return null;
}

/** `import { A, B as C } from "x"` / `import D from "x"` / `export { E } from "x"` → [{ names, spec }]. */
function importsOf(text) {
  const out = [];
  const code = stripComments(text);
  for (const m of code.matchAll(/(?:import|export)\s+(type\s+)?([\s\S]*?)\s+from\s+["']([^"']+)["']/g)) {
    if (m[1]) continue;
    const clause = m[2];
    const names = [];
    const braces = clause.match(/\{([\s\S]*)\}/);
    if (braces) {
      for (const part of braces[1].split(",")) {
        const name = part.trim().replace(/^type\s+/, "").split(/\s+as\s+/)[0].trim();
        if (name) names.push(name);
      }
    }
    const star = /^\*/.test(clause.trim());
    out.push({ names, spec: m[3], star });
  }
  for (const m of code.matchAll(/import\s+["']([^"']+)["']/g)) out.push({ names: [], spec: m[1], star: false });
  return out;
}

const isBarrel = (file) => /[\\/]index\.tsx?$/.test(file);
const definesName = (text, name) => new RegExp(`export\\s+(?:async\\s+)?(?:function|const|class|let)\\s+${name}\\b|export\\s*\\{[^}]*\\b${name}\\b[^}]*\\}(?!\\s*from)`).test(text);

/** The module(s) a barrel really serves `name` from. */
function throughBarrel(root, barrel, name, depth = 0) {
  if (depth > 3) return [];
  const text = readFileSync(barrel, "utf8");
  const out = [];
  for (const entry of importsOf(text)) {
    const target = resolveSpec(root, barrel, entry.spec);
    if (!target) continue;
    const named = entry.names.includes(name);
    const star = entry.star || (/export\s*\*\s*from/.test(text) && entry.names.length === 0);
    if (!named && !star) continue;
    if (isBarrel(target)) out.push(...throughBarrel(root, target, name, depth + 1));
    else if (named || definesName(readFileSync(target, "utf8"), name)) out.push(target);
  }
  return out;
}

/** Feature modules a page renders (page itself included). */
export function pageModules(root, pageAbs, maxDepth = 5) {
  const seen = new Set([pageAbs]);
  const featuresDir = join(root, "features") + sep;
  const appDir = join(root, "app") + sep;
  const queue = [[pageAbs, 0]];
  while (queue.length) {
    const [file, depth] = queue.shift();
    if (depth >= maxDepth) continue;
    for (const entry of importsOf(readFileSync(file, "utf8"))) {
      const target = resolveSpec(root, file, entry.spec);
      if (!target || !(target.startsWith(featuresDir) || target.startsWith(appDir))) continue;
      // A barrel serves each name from the file it re-exports, or defines it itself.
      const localDecl = (name) => new RegExp(`export\\s+(?:async\\s+)?(?:function|const|class|let)\\s+${name}\\b`).test(readFileSync(target, "utf8"));
      const targets = isBarrel(target)
        ? [...entry.names.flatMap((name) => throughBarrel(root, target, name)), ...(entry.names.some(localDecl) ? [target] : [])]
        : [target];
      for (const t of targets) {
        if (seen.has(t)) continue;
        seen.add(t);
        queue.push([t, depth + 1]);
      }
    }
  }
  return [...seen];
}

const rel = (root, abs) => relative(root, abs).split(sep).join("/");

function walkPages(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const name of readdirSync(dir)) {
    const abs = join(dir, name);
    if (statSync(abs).isDirectory()) walkPages(abs, out);
    else if (name === "page.tsx") out.push(abs);
  }
  return out;
}

/** Findings: pages with a URL-driven control and no UrlSuspense panel. */
export function urlKeyedPanelFindings(root) {
  const out = [];
  for (const page of walkPages(join(root, "app", "(admin)")).sort()) {
    const route = `/${rel(join(root, "app", "(admin)"), dirname(page))}`.replace(/\/\([^)]+\)/g, "").replace(/\/$/, "") || "/";
    if (URL_KEYED_PANEL_EXEMPT[route]) continue;
    const modules = pageModules(root, page);
    if (URL_KEYED_PANEL_PENDING.has(route)) {
      if (modules.some((file) => rendersUrlSuspense(readFileSync(file, "utf8")))) {
        out.push({ file: rel(root, page), line: 1, snippet: `${route}: renders UrlSuspense now — remove it from URL_KEYED_PANEL_PENDING (scripts/lib/url-keyed-panel.mjs)` });
      }
      continue;
    }
    let control = null;
    for (const file of modules) {
      const name = urlControlIn(readFileSync(file, "utf8"));
      if (name) {
        control = { name, file: rel(root, file) };
        break;
      }
    }
    if (!control) continue;
    if (modules.some((file) => rendersUrlSuspense(readFileSync(file, "utf8")))) continue;
    out.push({
      file: rel(root, page),
      line: 1,
      snippet: `${route}: renders a URL-driven control (${control.name} in ${control.file}) but no data panel through <UrlSuspense> — a click holds the old page until the server answers`,
    });
  }
  return out;
}
