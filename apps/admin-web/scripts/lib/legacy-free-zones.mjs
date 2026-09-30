// legacy-free-zones.mjs — files that have been fully moved onto the MUI Minimal template stay there.
//
// J1 (PR #294 code lens) found the legacy stylesheets still carrying the UI: hand-made `.card/.hd/.bd`
// shells, `.btn`/`.qcard`/`.hrow` classes, native <button>/<input>/<select>/<table>, inline style={{}},
// lucide icons and feature .css files. Once a file is converted it is listed in
// scripts/legacy-free-zones.json, and from then on it may contain NONE of:
//   - a className token that a legacy stylesheet defines (app/*.css, features/**/*.css,
//     components/**/*.css outside components/minimal, layouts/**/*.css, CSS modules)
//   - a `style={...}` prop (theme sx instead)
//   - a native <button|input|select|textarea|table|thead|tbody|tfoot|tr|td|th> (MUI / template
//     components; a hidden `<input type="file">` behind a template upload Button and a
//     `<input type="hidden">` form field, which draws nothing, are allowed)
//   - an import from lucide-react (template Iconify instead)
//   - an import of a .css file (sx instead)
//   - a hex or rgb()/rgba() colour literal (theme palette tokens instead)
// A zone entry is a file path or a directory prefix ending in "/". Tests and stories are skipped.
// Zones only grow; removing one to make a regression pass is the thing this guard exists to stop.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

export const ZONES_FILE = "scripts/legacy-free-zones.json";
const NATIVE = /<(button|input|select|textarea|table|thead|tbody|tfoot|tr|td|th)(?=[\s>/]|$)/gm;
const STYLE_PROP = /\bstyle=\{/g;
const LUCIDE = /from\s+["']lucide-react["']/g;
const CSS_IMPORT = /import\s+(?:[\w{}\s,*]+\s+from\s+)?["'][^"']+\.css["']/g;
const HEX = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])/g;
const RGB = /rgba?\(\s*\d{1,3}\s*[,\s]\s*\d{1,3}/g;

function walk(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === ".next") continue;
    const abs = join(dir, entry.name);
    if (entry.isDirectory()) walk(abs, out);
    else out.push(abs);
  }
  return out;
}
const rel = (root, abs) => relative(root, abs).split(sep).join("/");

/** Every class selector a legacy stylesheet (or CSS module) in the app still defines. */
export function legacySelectors(root) {
  const css = [
    ...["app"].flatMap((d) => walk(join(root, d)).filter((f) => f.endsWith(".css") && !f.includes(`${sep}(`))),
    ...["features", "components", "layouts"].flatMap((d) => walk(join(root, d)).filter((f) => f.endsWith(".css"))),
  ].filter((abs) => !rel(root, abs).startsWith("components/minimal/"));
  const out = new Set();
  for (const abs of css) {
    const text = readFileSync(abs, "utf8").replace(/\/\*[\s\S]*?\*\//g, "").replace(/url\([^)]*\)/g, "");
    // Selectors only (the part before each `{`), so a `.5s` or `1.25rem` in a declaration never counts.
    for (const m of text.matchAll(/([^{}]+)\{/g)) {
      if (m[1].trim().startsWith("@")) continue;
      for (const c of m[1].matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) out.add(c[1]);
    }
  }
  return out;
}

export function readZones(root) {
  const file = join(root, ZONES_FILE);
  if (!existsSync(file)) return [];
  return JSON.parse(readFileSync(file, "utf8")).zones ?? [];
}

export function zoneFiles(root, zones) {
  const files = new Set();
  for (const zone of zones) {
    const abs = join(root, zone);
    if (!existsSync(abs)) continue;
    const list = statSync(abs).isDirectory() ? walk(abs) : [abs];
    for (const f of list) {
      const r = rel(root, f);
      if (!/\.(tsx|ts)$/.test(r) || /\.(test|stories|spec)\.[tj]sx?$/.test(r) || r.endsWith(".d.ts")) continue;
      files.add(r);
    }
  }
  return [...files].sort();
}

const lineOf = (text, index) => text.slice(0, index).split("\n").length;
const stripComments = (text) =>
  text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " ")).replace(/(^|[^:"'`\\])\/\/.*$/gm, (m, p) => p + " ".repeat(m.length - p.length));

/** Findings for one file's source. `selectors` = legacySelectors(root). */
export function legacyZoneFindingsFor(file, source, selectors) {
  const text = stripComments(source);
  const out = [];
  const push = (index, what) => out.push({ file, line: lineOf(text, index), snippet: what });
  // className="a b" / className={"a"} / className={`a ${b}`} / className={cx("a", on && "b")} / className: "a"
  const classRe = /className(?:=|:\s*)(\{[^}]*\}|"[^"]*"|'[^']*'|`[^`]*`)/g;
  for (const m of text.matchAll(classRe)) {
    for (const s of m[1].matchAll(/(["'`])((?:(?!\1).)*)\1/g)) {
      for (const token of s[2].replace(/\$\{[^}]*\}/g, " ").split(/\s+/)) {
        if (token && selectors.has(token)) push(m.index, `legacy class "${token}" (a legacy stylesheet rule): use template/MUI components + theme sx`);
      }
    }
  }
  for (const m of text.matchAll(STYLE_PROP)) push(m.index, "inline style={...} prop: use theme sx");
  for (const m of text.matchAll(NATIVE)) {
    const tagEnd = text.indexOf(">", m.index);
    const tag = text.slice(m.index, tagEnd < 0 ? undefined : tagEnd);
    if (m[1] === "input" && /type=["'](?:file|hidden)["']/.test(tag)) continue;
    push(m.index, `native <${m[1]}>: use the MUI / template component`);
  }
  for (const m of text.matchAll(LUCIDE)) push(m.index, "lucide-react icon: use the template Iconify icon");
  for (const m of text.matchAll(CSS_IMPORT)) push(m.index, "stylesheet import: move the styles to theme sx");
  for (const m of text.matchAll(HEX)) push(m.index, `hex colour ${m[0]}: use a theme palette token`);
  for (const m of text.matchAll(RGB)) push(m.index, "rgb()/rgba() colour: use a theme palette token (varAlpha)");
  return out;
}

export function legacyFreeZoneFindings(root) {
  const zones = readZones(root);
  const out = [];
  for (const zone of zones) if (!existsSync(join(root, zone))) out.push({ file: ZONES_FILE, line: 1, snippet: `zone "${zone}" does not exist (rename the entry with the file; zones never shrink)` });
  const files = zoneFiles(root, zones);
  if (!files.length) return out;
  const selectors = legacySelectors(root);
  for (const file of files) out.push(...legacyZoneFindingsFor(file, readFileSync(join(root, file), "utf8"), selectors));
  return out;
}
