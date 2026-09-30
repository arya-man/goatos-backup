// shrink-ratchets.mjs — J1 CI gaps (FIXJ-CI, Ravi 2026-09-30: "update skills, guardrails, enforce
// local CI/CD strictly"). Every admin-web file is counted for the legacy constructs the redesign is
// removing, and design:guard compares each count with its allowance in
// scripts/check-design-system-waivers/design-system-waivers.json ("ratchet", `check|file`):
// a file may never go over, a new file has 0, and an allowance above the current count FAILS until
// it is lowered (npm run design:guard:update-baseline), so a baseline only ever goes down.
//
//   legacy-class-use       className tokens defined by a legacy stylesheet (app/frame.css,
//                          minimal-theme.css, mesha-theme.css, globals.css, feature / component /
//                          layout .css), counted per token use
//   legacy-card-reachable  the hand-made card shell (className card / hd / bd / wchart / wtable /
//                          kpi / chartcard, <h2 className="h">) in any file reachable by imports
//                          from an app/**/page.tsx, not only the files named in page-template-map
//   native-control         native <select> / <input> / <button> / <textarea> / <table>, matched
//                          over the WHOLE file so a tag that ends its line (`<select` + newline,
//                          the 8 health selects J1 found) counts; hidden / file inputs excluded
//   inline-style-prop      style={…} props
//   lucide-import          icons imported from lucide-react (the template uses Iconify)
//   raw-px-hex-literal     raw `12px` and #hex colour literals in .ts / .tsx
//   legacy-css-rules       style RULES per stylesheet (not lines: deleting blank lines or comments
//                          no longer satisfies it), every .css outside components/minimal and
//                          layouts/template, CSS modules included
//
// The template itself (components/minimal, components/app/sections) is exempt through the guard's
// template-code rule; layouts/template is not scanned.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";

export const SHRINK_RATCHET_CHECKS = {
  "legacy-class-use": "a className token a legacy stylesheet defines (frame / minimal-theme / mesha-theme / globals / feature .css); build with template sections + MUI + theme sx. Shrink-only per file",
  "legacy-card-reachable": "hand-made card shell (className card/hd/bd/wchart/wtable/kpi/chartcard, <h2 className=\"h\">) in a file a route page imports; use the template section card (Card + CardHeader). Shrink-only per file",
  "native-control": "native <select>/<input>/<button>/<textarea>/<table> (whole-file, multi-line match); use MUI TextField select / Button / IconButton / Table + TableHeadCustom. Shrink-only per file",
  "inline-style-prop": "inline style={…} prop; use the theme sx. Shrink-only per file",
  "lucide-import": "lucide-react icon; the template uses Iconify (layouts/template/iconify, registered offline set). Shrink-only per file",
  "raw-px-hex-literal": "raw px or #hex literal in TS/TSX; use theme spacing / typography / palette tokens. Shrink-only per file",
  "legacy-css-rules": "a style rule in a legacy stylesheet or CSS module; style through template components + theme sx. Rule count per stylesheet is shrink-only, a new stylesheet has 0",
};

const LEGACY_APP_CSS = ["app/frame.css", "app/minimal-theme.css", "app/mesha-theme.css", "app/globals.css"];
const CARD_TOKENS = new Set(["card", "hd", "bd", "wchart", "wtable", "kpi", "chartcard"]);
const NATIVE = /<(select|input|button|textarea|table)(?=[\s>/]|$)/g;
const STYLE_PROP = /\bstyle=\{/g;
const LUCIDE_IMPORT = /import\s+(?:type\s+)?\{([^}]*)\}\s*from\s*["']lucide-react(?:\/[^"']*)?["']|import\s+(\w+)\s+from\s*["']lucide-react(?:\/[^"']*)?["']/g;
const PX = /(?<![\w.#-])(?!0px\b)\d+(?:\.\d+)?px\b/g;
const HEX = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])/g;

function walk(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const abs = join(dir, entry.name);
    if (entry.isDirectory()) walk(abs, out);
    else out.push(abs);
  }
  return out;
}
const toRel = (root, abs) => relative(root, abs).split(sep).join("/");
const lineOf = (text, index) => text.slice(0, index).split("\n").length;
/** Comments blanked (newlines kept, so line numbers hold). A `//` after `:` (URLs) or inside a quote-led run is kept. */
export function stripComments(text) {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .replace(/(^|[^:"'`\\])\/\/.*$/gm, (m, p) => p + " ".repeat(m.length - p.length));
}

/** Stylesheets the ratchets cover: every .css outside the verbatim template. */
export function stylesheetFiles(root) {
  return ["app", "components", "features", "layouts", "lib", "theme", "styles"]
    .flatMap((d) => walk(join(root, d)))
    .map((abs) => toRel(root, abs))
    .filter((rel) => rel.endsWith(".css") && !rel.startsWith("components/minimal/") && !rel.startsWith("layouts/template/"));
}

/** Class names a legacy stylesheet defines (selectors only; MUI / Apex / FullCalendar parts excluded). */
export function legacyClassNames(root) {
  const out = new Set();
  for (const rel of stylesheetFiles(root)) {
    if (rel.endsWith(".module.css")) continue; // module classes are hashed, reached as styles.x
    if (rel === "theme/fonts.css" || rel === "app/minimal-tokens.css") continue;
    const text = readFileSync(join(root, rel), "utf8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/url\([^)]*\)/g, "");
    for (const m of text.matchAll(/([^{}]+)\{/g)) {
      if (m[1].trim().startsWith("@")) continue;
      const sel = m[1].replace(/:(?:not)\((?:[^()]|\([^()]*\))*\)/g, "");
      for (const c of sel.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) if (!/^(?:Mui|apexcharts|fc-|simplebar|iconify)/.test(c[1])) out.add(c[1]);
    }
  }
  return out;
}

/** Style rules per stylesheet: leaf `selector { declarations }` blocks outside @keyframes / @font-face. */
export function cssRuleFindings(text) {
  const src = text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
  const out = [];
  const stack = []; // { prelude, start, hasChild }
  let preludeStart = 0;
  for (let i = 0; i < src.length; i += 1) {
    const ch = src[i];
    if (ch === "{") {
      const prelude = src.slice(preludeStart, i).trim();
      if (stack.length) stack[stack.length - 1].hasChild = true;
      stack.push({ prelude, start: i, hasChild: false });
      preludeStart = i + 1;
    } else if (ch === "}") {
      const block = stack.pop();
      preludeStart = i + 1;
      if (!block || block.hasChild || !block.prelude || block.prelude.startsWith("@")) continue;
      if (stack.some((b) => /^@(?:-\w+-)?(?:keyframes|font-face)\b/.test(b.prelude))) continue;
      out.push({ line: lineOf(src, block.start), snippet: block.prelude.replace(/\s+/g, " ").slice(0, 120) });
    } else if (ch === ";" && !stack.length) {
      preludeStart = i + 1; // top-level @import / @charset
    }
  }
  return out;
}

/** The JS expression of each className prop, balanced (className="…", ={…}, : "…" in objects). */
function classNameExpressions(text) {
  const out = [];
  const re = /\bclassName\s*(?:=|:)\s*/g;
  let m;
  while ((m = re.exec(text))) {
    let i = m.index + m[0].length;
    const open = text[i];
    if (open === '"' || open === "'" || open === "`") {
      const end = text.indexOf(open, i + 1);
      if (end > i) out.push({ index: m.index, expr: text.slice(i, end + 1) });
      continue;
    }
    if (open !== "{" && !/[\w(]/.test(open ?? "")) continue;
    // object form (`className: cond ? "a" : "b",`) or JSX `{…}`: read to the balanced end
    let depth = 0;
    const start = i;
    for (; i < text.length; i += 1) {
      const ch = text[i];
      if (ch === '"' || ch === "'" || ch === "`") {
        const end = text.indexOf(ch, i + 1);
        if (end < 0) break;
        i = end;
        continue;
      }
      if (ch === "{" || ch === "(" || ch === "[") depth += 1;
      else if (ch === "}" || ch === ")" || ch === "]") {
        depth -= 1;
        if (depth <= 0 && open === "{") { i += 1; break; }
        if (depth < 0) break;
      } else if (depth === 0 && open !== "{" && (ch === "," || ch === "\n" || ch === ";")) break;
    }
    out.push({ index: m.index, expr: text.slice(start, i) });
  }
  return out;
}
function classTokens(expr) {
  const tokens = [];
  for (const s of expr.matchAll(/(["'`])((?:(?!\1)[\s\S])*)\1/g)) {
    for (const t of s[2].replace(/\$\{[^}]*\}/g, " ").split(/\s+/)) if (t && /^-?[_a-zA-Z][\w-]*$/.test(t)) tokens.push(t);
  }
  return tokens;
}

/** Ratchet findings for one TS/TSX source. */
export function shrinkRatchetFindingsFor(rel, source, { legacyClasses, reachable }) {
  const text = stripComments(source);
  const out = [];
  const push = (check, index, snippet) => out.push({ check, line: lineOf(text, index), snippet });
  if (rel.endsWith(".tsx")) {
    for (const { index, expr } of classNameExpressions(text)) {
      for (const token of classTokens(expr)) {
        if (legacyClasses.has(token)) push("legacy-class-use", index, `legacy class "${token}": ${expr.replace(/\s+/g, " ").slice(0, 100)}`);
        if (reachable && CARD_TOKENS.has(token)) push("legacy-card-reachable", index, `card shell "${token}": ${expr.replace(/\s+/g, " ").slice(0, 100)}`);
      }
    }
    if (reachable) for (const m of text.matchAll(/<h2\s+className=["']h["']/g)) push("legacy-card-reachable", m.index, '<h2 className="h">');
    for (const m of text.matchAll(NATIVE)) {
      const tagEnd = text.indexOf(">", m.index);
      const tag = text.slice(m.index, tagEnd < 0 ? m.index + 200 : tagEnd);
      if (m[1] === "input" && /type=["'](?:hidden|file)["']/.test(tag)) continue;
      push("native-control", m.index, `<${m[1]}${tag.slice(m[1].length + 1, 60).replace(/\s+/g, " ")}`);
    }
    for (const m of text.matchAll(STYLE_PROP)) push("inline-style-prop", m.index, text.slice(m.index, m.index + 80).replace(/\s+/g, " "));
  }
  for (const m of text.matchAll(LUCIDE_IMPORT)) {
    const names = m[1] ? m[1].split(",").map((s) => s.trim()).filter(Boolean) : [m[2]];
    for (const name of names) push("lucide-import", m.index, `lucide-react ${name}`);
  }
  for (const m of text.matchAll(PX)) push("raw-px-hex-literal", m.index, `raw ${m[0]}`);
  for (const m of text.matchAll(HEX)) push("raw-px-hex-literal", m.index, `hex ${m[0]}`);
  return out;
}

function resolveSpec(root, spec, from) {
  let base;
  if (spec.startsWith("@/")) base = join(root, spec.slice(2));
  else if (spec.startsWith(".")) base = join(dirname(from), spec);
  else return null;
  for (const c of [base, `${base}.tsx`, `${base}.ts`, join(base, "index.tsx"), join(base, "index.ts")]) {
    if (/\.tsx?$/.test(c) && existsSync(c) && statSync(c).isFile()) return c;
  }
  return null;
}
/** Files reachable by static imports / re-exports / dynamic import() from every app/**\/page.tsx. */
export function filesReachableFromPages(root) {
  const seen = new Set();
  const queue = walk(join(root, "app")).filter((f) => /(^|[\\/])page\.tsx$/.test(f));
  while (queue.length) {
    const abs = queue.pop();
    if (seen.has(abs)) continue;
    seen.add(abs);
    let text;
    try { text = readFileSync(abs, "utf8"); } catch { continue; }
    for (const m of text.matchAll(/(?:\bfrom\s*|\bimport\s*\(\s*|^\s*import\s+)["']([^"']+)["']/gm)) {
      const r = resolveSpec(root, m[1], abs);
      if (r && !seen.has(r)) queue.push(r);
    }
  }
  return new Set([...seen].map((abs) => toRel(root, abs)));
}

/** Every shrink-ratchet finding under root: { check, file, line, snippet }. */
export function shrinkRatchetFindings(root, { isExempt = () => false } = {}) {
  const out = [];
  const legacyClasses = legacyClassNames(root);
  const reachable = filesReachableFromPages(root);
  const code = ["app", "components", "features", "lib", "layouts"]
    .flatMap((d) => walk(join(root, d)))
    .map((abs) => toRel(root, abs))
    .filter((rel) => /\.tsx?$/.test(rel) && !rel.endsWith(".d.ts") && !/\.(test|stories|spec)\.tsx?$/.test(rel) && !rel.startsWith("layouts/template/") && !isExempt(rel));
  for (const rel of code) {
    for (const f of shrinkRatchetFindingsFor(rel, readFileSync(join(root, rel), "utf8"), { legacyClasses, reachable: reachable.has(rel) })) out.push({ ...f, file: rel });
  }
  for (const rel of stylesheetFiles(root)) {
    if (isExempt(rel) || rel === "theme/fonts.css") continue;
    for (const f of cssRuleFindings(readFileSync(join(root, rel), "utf8"))) out.push({ check: "legacy-css-rules", file: rel, ...f });
  }
  return out;
}

export { LEGACY_APP_CSS };
