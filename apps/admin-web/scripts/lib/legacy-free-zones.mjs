// legacy-free-zones.mjs — files that have been fully moved onto the MUI Minimal template stay there.
//
// J1 (PR #294 code lens) found the legacy stylesheets still carrying the UI: hand-made `.card/.hd/.bd`
// shells, `.btn`/`.qcard`/`.hrow` classes, native <button>/<input>/<select>/<table>, inline style={{}},
// lucide icons and feature .css files. Once a file is converted it is listed in
// scripts/legacy-free-zones.json, and from then on it may contain NONE of:
//   - a className token a legacy stylesheet defined (the frozen denylist
//     scripts/legacy-class-denylist.json, taken at 7e181ce32; documented JS / sx hooks exempt)
//   - a `style={...}` prop (theme sx instead)
//   - a native <button|input|select|textarea|table|thead|tbody|tfoot|tr|td|th> (MUI / template
//     components; a hidden `<input type="file">` behind a template upload Button and a
//     `<input type="hidden">` form field, which draws nothing, are allowed)
//   - an import from lucide-react (template Iconify instead)
//   - an import of a .css file (sx instead)
//   - a hex or rgb()/rgba() colour literal (theme palette tokens instead)
//   - an embedded stylesheet: a <style> element or <GlobalStyles styles={`...css`}> string
//     (a feature stylesheet moved into a TSX string is still a feature stylesheet: sx instead)
// A zone entry is a file path or a directory prefix ending in "/". Tests and stories are skipped.
// Zones only grow; removing one to make a regression pass is the thing this guard exists to stop.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { bannedLegacyClasses } from "./legacy-class-denylist.mjs";

export const ZONES_FILE = "scripts/legacy-free-zones.json";
const NATIVE = /<(button|input|select|textarea|table|thead|tbody|tfoot|tr|td|th)(?=[\s>/]|$)/gm;
const STYLE_PROP = /\bstyle=\{/g;
const LUCIDE = /from\s+["']lucide-react["']/g;
const CSS_IMPORT = /import\s+(?:[\w{}\s,*]+\s+from\s+)?["'][^"']+\.css["']/g;
const HEX = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])/g;
const STYLE_BLOCK = /<style(?=[\s>])|<GlobalStyles\b[^>]*\bstyles=\{\s*[`"']/g;
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

/**
 * Every legacy class name (J1B P2-2): the FROZEN denylist scripts/legacy-class-denylist.json (the
 * class selectors the legacy stylesheets defined at 7e181ce32) minus its documented hooks. It used to
 * be read from the stylesheets that exist now, which FIXJ6 deleted, so the zone rule lost its teeth.
 */
export function legacySelectors(root) {
  return bannedLegacyClasses(root);
}

export function readZones(root) {
  const file = join(root, ZONES_FILE);
  if (!existsSync(file)) return [];
  return JSON.parse(readFileSync(file, "utf8")).zones ?? [];
}

/**
 * J1B P2-3 (FIXJ7): the zones cover everything under app/, components/, features/ and layouts/;
 * `exempt` ({ path-or-prefix: reason }) names what stays outside: the verbatim template
 * (components/minimal/, layouts/template/) and the few files that still carry a baselined construct.
 * Exemptions only SHRINK: an exempt FILE that is clean now fails (`stale exemption`) until removed.
 */
export function readExempt(root) {
  const file = join(root, ZONES_FILE);
  if (!existsSync(file)) return {};
  return JSON.parse(readFileSync(file, "utf8")).exempt ?? {};
}
const isExempt = (rel, exempt) => Object.keys(exempt).some((e) => (e.endsWith("/") ? rel.startsWith(e) : rel === e));

export function zoneFiles(root, zones, exempt = {}) {
  const files = new Set();
  for (const zone of zones) {
    const abs = join(root, zone);
    if (!existsSync(abs)) continue;
    const list = statSync(abs).isDirectory() ? walk(abs) : [abs];
    for (const f of list) {
      const r = rel(root, f);
      if (!/\.(tsx|ts)$/.test(r) || /\.(test|stories|spec)\.[tj]sx?$/.test(r) || r.endsWith(".d.ts")) continue;
      if (isExempt(r, exempt)) continue;
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
  for (const m of text.matchAll(STYLE_BLOCK)) push(m.index, "embedded stylesheet (<style> / string GlobalStyles): move the rules to theme sx");
  for (const m of text.matchAll(RGB)) push(m.index, "rgb()/rgba() colour: use a theme palette token (varAlpha)");
  return out;
}

export function legacyFreeZoneFindings(root) {
  const zones = readZones(root);
  const out = [];
  for (const zone of zones) if (!existsSync(join(root, zone))) out.push({ file: ZONES_FILE, line: 1, snippet: `zone "${zone}" does not exist (rename the entry with the file; zones never shrink)` });
  const exempt = readExempt(root);
  const files = zoneFiles(root, zones, exempt);
  const selectors = legacySelectors(root);
  for (const file of files) out.push(...legacyZoneFindingsFor(file, readFileSync(join(root, file), "utf8"), selectors));
  // A file exemption that has no finding left is stale: the file joins the zones (exemptions shrink).
  for (const [entry, reason] of Object.entries(exempt)) {
    if (!reason || typeof reason !== "string") out.push({ file: ZONES_FILE, line: 1, snippet: `exemption "${entry}" has no reason` });
    if (entry.endsWith("/")) continue;
    const abs = join(root, entry);
    if (!existsSync(abs)) { out.push({ file: ZONES_FILE, line: 1, snippet: `exempt file "${entry}" does not exist: remove the exemption` }); continue; }
    if (!legacyZoneFindingsFor(entry, readFileSync(abs, "utf8"), selectors).length) {
      out.push({ file: ZONES_FILE, line: 1, snippet: `stale exemption "${entry}": the file is clean now, remove it from "exempt" (exemptions only shrink)` });
    }
  }
  return out;
}
