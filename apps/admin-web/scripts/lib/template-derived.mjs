// template-derived-anatomy — a template section whose DEMO WIRING had to become props (fixed demo
// values, a hard-coded default select, demo model types / copy) lives outside components/minimal
// (components/app/sections/<template path>) and is listed in docs/design/template-derived.json with
// its template source and a one-line account of what became props. Everything else must stay the
// template's: this check compares the file's JSX element sequence and its sx keys with the
// template's, recorded in the manifest (the template is not in CI; refresh with
// `node scripts/refresh-template-derived.mjs`). Any markup or style drift fails, except the data /
// prop slots an entry lists in `strip` (regexes removed before comparing; each named in `replaced`).
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

function stripComments(text) {
  return String(text)
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:"'`\\])\/\/.*$/gm, "$1")
    .replace(/\{\s*\/\*[\s\S]*?\*\/\s*\}/g, "");
}

/** JSX opening-tag names in order (`<Card`, `<Box`, `<m.div`), generics / comparisons excluded. */
export function jsxTags(text) {
  const code = stripComments(text);
  const out = [];
  for (const m of code.matchAll(/<([A-Z][\w.]*|[a-z][\w-]*)(?=[\s/>])/g)) {
    const before = code.slice(Math.max(0, m.index - 1), m.index);
    if (/[\w)\]]/.test(before)) continue; // a < b comparisons, Array<T> generics
    out.push(m[1]);
  }
  return out;
}

function matchBrace(code, open) {
  let depth = 0;
  for (let i = open; i < code.length; i++) {
    if (code[i] === "{") depth++;
    else if (code[i] === "}" && --depth === 0) return i;
  }
  return code.length;
}

/** Keys of every `sx={…}` value, in order (nested keys included, values ignored). */
export function sxKeys(text) {
  const code = stripComments(text);
  const out = [];
  for (const m of code.matchAll(/\bsx=\{/g)) {
    const start = m.index + m[0].length - 1;
    const body = code.slice(start, matchBrace(code, start) + 1);
    for (const k of body.matchAll(/(?:^|[{,\s])(['"`]?)([&\w.:\-[\]$>* ()'"`]+?)\1\s*:(?!:)/g)) {
      const key = k[2].trim();
      if (!key || /^\d/.test(key) || /\?/.test(key)) continue;
      out.push(key);
    }
  }
  return out;
}

const norm = (v) => v.replace(/\s+/g, " ").replace(/,\s*([}\]])/g, "$1").replace(/\(\s+/g, "(").replace(/\s+\)/g, ")").trim();

/** Every `sx={…}` value, whitespace-normalised, in order (REVIEW-23 O29: values, not just keys). */
export function sxValues(text) {
  const code = stripComments(text);
  const out = [];
  for (const m of code.matchAll(/\bsx=\{/g)) {
    const start = m.index + m[0].length - 1;
    out.push(norm(code.slice(start + 1, matchBrace(code, start))));
  }
  return out;
}

const LITERAL_PROPS = ["type", "variant", "component", "style", "size", "color", "fullWidth", "noWrap", "underline", "anchor", "orientation", "iconPosition", "align"];

/** Opening-tag texts (`<Chart type="bar" … >`), braces / strings respected. */
function openingTags(code) {
  const out = [];
  for (const m of code.matchAll(/<([A-Z][\w.]*|[a-z][\w-]*)(?=[\s/>])/g)) {
    const before = code.slice(Math.max(0, m.index - 1), m.index);
    if (/[\w)\]]/.test(before)) continue;
    let depth = 0, quote = null, i = m.index + 1;
    for (; i < code.length; i++) {
      const c = code[i];
      if (quote) { if (c === quote && code[i - 1] !== "\\") quote = null; continue; }
      if (c === '"' || c === "'" || c === "`") { if (depth > 0 || c !== "`") quote = c; continue; }
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0) break;
    }
    out.push(code.slice(m.index, i + 1));
  }
  return out;
}

/** Literal JSX props (`type="bar"`, `variant={'h6'}`, `size={56}`, bare `fullWidth`, any `style=`), in order. */
export function literalProps(text) {
  const code = stripComments(text);
  const out = [];
  const re = new RegExp(`\\s(${LITERAL_PROPS.join("|")})(?:=("[^"]*"|'[^']*'|\\{)|(?=[\\s/>]))`, "g");
  for (const tag of openingTags(code)) {
    for (const m of tag.matchAll(re)) {
      const name = m[1];
      if (m[2] === undefined) { out.push(name); continue; }
      if (m[2] !== "{") { out.push(`${name}=${m[2].slice(1, -1)}`); continue; }
      const start = m.index + m[0].length - 1;
      const value = norm(tag.slice(start + 1, matchBrace(tag, start)));
      if (name === "style" || /^(?:'[^']*'|"[^"]*"|-?\d+(?:\.\d+)?|true|false)$/.test(value)) out.push(`${name}=${value.replace(/^['"]|['"]$/g, "")}`);
    }
  }
  return out;
}

export function anatomy(text) {
  return { tags: jsxTags(text), sx: sxKeys(text), sxValues: sxValues(text), props: literalProps(text) };
}

function firstDiff(a, b) {
  const n = Math.max(a.length, b.length);
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return { index: i, want: a[i] ?? "(end)", got: b[i] ?? "(end)" };
  return null;
}

/** [{ file, line, snippet }]. `root` = apps/admin-web. */
export function templateDerivedFindings(root, manifestFile) {
  const hits = [];
  if (!existsSync(manifestFile)) return hits;
  let manifest;
  try {
    manifest = JSON.parse(readFileSync(manifestFile, "utf8"));
  } catch {
    return [{ file: "docs/design/template-derived.json", line: 1, snippet: "template-derived.json is not valid JSON" }];
  }
  for (const [rel, entry] of Object.entries(manifest.files ?? {})) {
    const abs = join(root, rel);
    if (!existsSync(abs)) {
      hits.push({ file: "docs/design/template-derived.json", line: 1, snippet: `stale entry ${rel} (file missing)` });
      continue;
    }
    if (rel.startsWith("components/minimal/")) hits.push({ file: rel, line: 1, snippet: "a template-derived file never lives under components/minimal (that folder is verbatim only)" });
    if (!entry?.source || !entry?.replaced || !Array.isArray(entry.tags) || !Array.isArray(entry.sx)) {
      hits.push({ file: rel, line: 1, snippet: "manifest entry needs source, replaced, tags, sx (run node scripts/refresh-template-derived.mjs)" });
      continue;
    }
    // `strip`: the manifest may list data/prop slots the derived file adds (regex sources, applied
    // before the comparison), e.g. an optional caption line under a row. Each is named in `replaced`.
    let text = readFileSync(abs, "utf8");
    for (const src of entry.strip ?? []) text = text.replace(new RegExp(src, "g"), "");
    const got = anatomy(text);
    const t = firstDiff(entry.tags, got.tags);
    if (t) hits.push({ file: rel, line: 1, snippet: `JSX differs from template ${entry.source} at element #${t.index}: template <${t.want}>, file <${t.got}>` });
    const s = firstDiff(entry.sx, got.sx);
    if (s) hits.push({ file: rel, line: 1, snippet: `sx differs from template ${entry.source} at key #${s.index}: template "${s.want}", file "${s.got}"` });
    if (!Array.isArray(entry.sxValues) || !Array.isArray(entry.props)) {
      hits.push({ file: rel, line: 1, snippet: "manifest entry lacks sxValues/props (run node scripts/refresh-template-derived.mjs)" });
      continue;
    }
    const v = firstDiff(entry.sxValues, got.sxValues);
    if (v) hits.push({ file: rel, line: 1, snippet: `sx value differs from template ${entry.source} at sx #${v.index}: template {${v.want}}, file {${v.got}}` });
    const p = firstDiff(entry.props, got.props);
    if (p) hits.push({ file: rel, line: 1, snippet: `literal JSX prop differs from template ${entry.source} at #${p.index}: template ${p.want}, file ${p.got}` });
  }
  return hits;
}
