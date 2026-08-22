#!/usr/bin/env node
// check-mock-css-parity — a class the implementation RENDERS must actually be STYLED, and
// where the app DOES style it, the styling must actually MATCH the mock.
//
// WHY THIS EXISTS
// ---------------
// Herd Signals was built mock-first: mock/herd-signals-mock.html is the approved design and the
// React components were ported from it, reusing its class names. But porting the MARKUP without
// porting the CSS is silent. `.grid2` is the worked example: the Gateways and Insights tabs both
// render <div className="grid2">, the mock defines
//     .grid2{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:14px}
// and the app theme defined NOTHING. No error, no warning, no failing test — the tabs just
// stacked as full-width blocks and looked nothing like the design.
//
// That "missing entirely" case was the guard's first version. It shipped, and the Live Monitor
// screen STILL looked visibly wrong to the maintainer a fifth time: KPI icons absent, active-KPI
// state copy/styling different, removable filter chips replaced by a plain "Clear filters"
// button, per-card tier badges collapsed to one value, filter-bar order and wrapping different.
// The guard passed clean through all of it, because every class involved DID exist in the app
// stylesheet — its DECLARATIONS just diverged from the mock's. A class that exists but is styled
// wrong is exactly as broken, visually, as a class that was never styled at all, and the first
// version of this guard was blind to that whole failure class.
//
// THE RULE (now two rules)
// -------------------------
// 1. Missing: a component renders a class the mock defines; the app stylesheet must define it
//    too (unchanged from v1).
// 2. Diverged: a component renders a class BOTH the mock and the app stylesheet define; the
//    app's resolved declarations for a tracked set of visually load-bearing properties (colour,
//    background, border, border-radius, font-size, font-weight, padding, margin, gap, display,
//    grid-template*) must match the mock's, after normalising away non-meaningful noise (colour
//    format, shorthand vs. longhand where resolvable, whitespace, declaration order, equivalent
//    zero units, and CSS custom properties that resolve to the mock's literal via :root).
//
// SCOPED RESOLUTION (v3) — the app side of rule 2 is now scope-aware
// --------------------------------------------------------------------
// This app's CSS is deliberately written scoped: `.herd-signals-page .fbar`,
// `.herd-signals-page .drawer .hchart`, because bare classes like `.btn`/`.tag`/`.kpi` are shared
// by dozens of other admin screens and restyling the bare class would break them. v2 only ever
// resolved a selector that was EXACTLY `.classname`, so every correctly-scoped app rule was
// invisible to it — it reported mismatches that were not real, and (once, in this file's own
// history) invited "fixing" a false finding by adding a bare `.classname` duplicate purely so the
// guard could see it, which pollutes a shared global stylesheet just to satisfy a checker. v3
// (getScopedClassRules / resolveScopedProps) instead gathers every APP rule whose selector's
// most specific (rightmost) compound includes the target class — `.foo`, `.a .foo`, `.a .b .foo`
// all count — and resolves the winning declaration the way a browser would: real CSS specificity
// ([ids, classes, elements]) decides, and among equal-specificity rules, later source order wins
// UNLESS the tied rules have different selector text AND disagree on a property's value — that
// specific shape (e.g. `.drawer .hchart` vs `.fs .hchart` sizing the same class differently in
// different places) is legitimate context-dependent styling, not a defect, and is reported as a
// NOTE, never as a mismatch or a failure. A compound like `.btn.dng` only attaches to `dng` (the
// LAST-written class token in its last compound) — never to `btn` — because it only matches
// elements that carry BOTH classes, so its declarations are conditional on the modifier class,
// not something plain `.btn` resolution may borrow. The mock side of the comparison is
// intentionally left on the v1/v2 exact-`.classname` resolver — the mock is one flat page, not a
// multi-screen shared stylesheet, so it has no scoping problem to solve.
//
// WHAT IT STILL DOES NOT CLAIM (read this before trusting a clean run)
// ----------------------------------------------------------------------
// - No media queries. Only top-level rules are read; @media/@supports/@keyframes blocks are
//   stripped before parsing, so a class styled differently at a breakpoint is invisible here.
// - No pseudo-classes, pseudo-elements, or attribute selectors, in either resolver. A mock-side
//   selector is only used when it is EXACTLY `.name` with nothing else attached. An app-side
//   selector is dropped from scoped resolution entirely if it contains `:` or `[` anywhere —
//   `.foo:hover`, `.foo::before`, `.btn[disabled]` are conditional on state or markup this guard
//   cannot evaluate statically, so they are left out of scope rather than guessed at (the
//   single-token existence check below still half-sees `.foo` and `.bar` inside a compound
//   selector regardless of pseudo/attribute parts — see the compound-selector note).
// - No runtime-computed values. Anything set via inline `style={{...}}`, styled-components,
//   CSS-in-JS, or JS-computed class names is invisible.
// - No Tailwind or other utility classes — only classes the MOCK defines are ever in scope,
//   because the mock is not the authority on classes it doesn't mention.
// - Class names built by string concatenation or template interpolation are invisible. Static
//   className literals and the static parts of template literals are covered.
// - Shorthand resolution is best-effort, not a real CSS engine: padding/margin/border-radius
//   1-4-value expansion and a simple border-shorthand splitter are implemented; anything odder
//   (calc(), custom properties nested inside functions, logical properties like
//   padding-inline) is treated as opaque text and compared literally, which can both
//   under-report (misses a real divergence hidden in calc()) and over-report (flags a
//   spacing/timing artefact it can't actually resolve) — those cases are called out per-run as
//   "unresolved" rather than silently passed or silently failed.
// - Compound selectors: the EXISTENCE check (v1, kept) uses a regex that pulls every class TOKEN
//   out of a selector, so `.srcl.inferred{...}` registers both `srcl` and `inferred` as "defined"
//   even though neither is separately selectable. The mock-side DECLARATION check does not have
//   this hole — it only resolves declarations for selectors that are exactly one class, so
//   `inferred` alone resolves to no declarations from that rule and is never compared on
//   manufactured data. The app-side scoped resolver attaches a compound rule like `.a.foo{...}`
//   only to `foo` (the last class token), per the scoped-resolution rule above.
// - This is not a substitute for actually opening both screens side by side. It catches the
//   mechanical class of defect (markup ported, CSS not, or CSS drifted) — it does not catch a
//   mock element being dropped from the markup entirely (no icon rendered at all, a chip
//   component swapped for a different component, a badge collapsed to a single value) unless
//   that swap also drops or renames the class name. Structural fidelity is still a visual review
//   job, not this guard's job.
//
// DELIBERATE MOCK DIVERGENCES (banned reintroductions, not CSS bugs)
// ---------------------------------------------------------------------
// Some mock content is intentionally NOT ported, by maintainer decision, for reasons that have
// nothing to do with CSS. The battery-life estimate ("est. ~1.7 year left", "median ~1.6 year")
// is the current case: GoatOS has no vendor-confirmed discharge curve, so any life estimate is
// invented data, and the maintainer ordered it deleted (see
// apps/admin-web/features/herd-signals/herd-signals-animals-table.tsx). The danger is specific:
// the app's stylesheet already carries `.srcl.inferred` (ported for other Derived/Inferred
// fields), so if the estimate text were reintroduced verbatim it would render CORRECTLY STYLED —
// invisible to both checks above. DELIBERATE_MOCK_DIVERGENCES below is a third, independent scan
// that greps herd-signals component source for the patterns that would signal that specific
// reintroduction, so this stays a machine-enforced fact instead of something only remembered by
// whoever was in the room. Add to this list — do not silently work around a divergence finding.
//
// USAGE
//   node tools/agent-hooks/check-mock-css-parity.mjs            # check every registered module
//   node tools/agent-hooks/check-mock-css-parity.mjs --self-test
import { readFileSync, existsSync } from "node:fs";
import { readdirSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

// Modules built mock-first. Add an entry when a new screen is ported from a mock.
const MODULES = [
  {
    name: "herd-signals",
    componentsDir: "apps/admin-web/features/herd-signals",
    mock: "mock/herd-signals-mock.html",
    stylesheets: ["apps/admin-web/app/mesha-theme.css"],
  },
];

// Divergences the app is DELIBERATELY allowed to have from the mock, for non-CSS reasons.
// Each entry's `forbiddenInComponents` patterns must never match herd-signals component source —
// if one does, that's the banned content creeping back in, not a parity bug to "fix" toward the
// mock. This is intentionally a separate scan from the CSS declaration comparison: the whole
// point is that the CSS comparison would not catch this (see header).
const DELIBERATE_MOCK_DIVERGENCES = [
  {
    id: "battery-life-estimate",
    reason:
      "No vendor-confirmed battery discharge curve exists for GoatOS smart tags, so a life " +
      "estimate ('est. ~1.7 year left', 'median ~1.6 year') is invented data. The maintainer " +
      "ordered the mock's battery-life-estimate text permanently deleted, not reintroduced " +
      "(see apps/admin-web/features/herd-signals/herd-signals-animals-table.tsx). It will not " +
      "trip the CSS checks above because `.srcl.inferred` is already ported for other " +
      "Derived/Inferred fields, so reintroduced text would render styled and clean.",
    forbiddenInComponents: [
      /\bsrcl\s+inferred\b/i,
      /\binferred\s+srcl\b/i,
      /estimated?\s+battery\s+life/i,
      /battery.{0,10}life.{0,10}(left|remaining|estimate)/i,
      /est\.\s*~?\s*\d+(\.\d+)?\s*(year|month)s?\s*left/i,
    ],
  },
];

// ---------------------------------------------------------------------------------------------
// Existence check (v1, unchanged): class names appearing in className="..." / className={`...`}.
// ---------------------------------------------------------------------------------------------
function classesUsedIn(source) {
  const found = new Set();
  const attr = /className\s*=\s*(?:"([^"]*)"|'([^']*)'|\{`([^`]*)`\}|\{"([^"]*)"\}|\{'([^']*)'\})/g;
  let m;
  while ((m = attr.exec(source)) !== null) {
    const raw = m[1] ?? m[2] ?? m[3] ?? m[4] ?? m[5] ?? "";
    // Drop ${...} interpolations; keep the static tokens around them.
    for (const token of raw.replace(/\$\{[^}]*\}/g, " ").split(/\s+/)) {
      const cleaned = token.trim();
      if (cleaned && /^[a-zA-Z][\w-]*$/.test(cleaned)) found.add(cleaned);
    }
  }
  return found;
}

/** Class names that appear as selectors (.foo{...}) in a stylesheet or a mock's <style>. */
function classesDefinedIn(source) {
  const found = new Set();
  const selector = /\.([a-zA-Z][\w-]*)(?=[^{}]*\{)/g;
  let m;
  while ((m = selector.exec(source)) !== null) found.add(m[1]);
  return found;
}

// ---------------------------------------------------------------------------------------------
// Declaration check (v2): resolve, per class, the properties set by rules whose selector is
// EXACTLY that one class (no combinators, no pseudo, no compounding) — this deliberately does
// NOT see `.srcl.inferred`-style compound rules as belonging to `srcl` or `inferred` alone.
// ---------------------------------------------------------------------------------------------

/**
 * Strip /* ... *\/ comments. Required BEFORE rule parsing: the rule regex captures everything
 * between the previous '}' and the next '{' as the selector, so a comment sitting directly above
 * a rule (e.g. "/* Mock's .chartnote: the small print ... *\/\n.foo{...}") gets swallowed into
 * that selector text. A stray ':' inside prose like that ("chartnote:") then trips the
 * pseudo-selector exclusion and silently drops an otherwise-correct rule — this bit a real class
 * (.chartnote) during development of this guard and is exactly the kind of silent miss this file
 * exists to prevent, so it gets fixed at the source rather than special-cased per class.
 */
function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "");
}

/** Strip @media/@supports/@keyframes/@font-face blocks (brace-depth aware) — declared blind spot. */
function stripAtRuleBlocks(rawSource) {
  const source = stripComments(rawSource);
  let out = "";
  let depth = 0;
  let atDepthStart = -1;
  let i = 0;
  while (i < source.length) {
    const ch = source[i];
    if (ch === "@" && depth === 0) {
      // Find the following '{' — everything from '@' to the matching '}' is dropped.
      const braceIdx = source.indexOf("{", i);
      if (braceIdx === -1) {
        out += source.slice(i);
        break;
      }
      let d = 1;
      let j = braceIdx + 1;
      while (j < source.length && d > 0) {
        if (source[j] === "{") d++;
        else if (source[j] === "}") d--;
        j++;
      }
      i = j;
      continue;
    }
    out += ch;
    i++;
  }
  return out;
}

/** Parse `:root{ --x: value; ... }` (all top-level :root blocks, later overrides earlier). */
function parseRootVars(source) {
  const vars = {};
  const rootRe = /:root\s*\{([^}]*)\}/g;
  let m;
  while ((m = rootRe.exec(source)) !== null) {
    for (const decl of m[1].split(";")) {
      const idx = decl.indexOf(":");
      if (idx === -1) continue;
      const name = decl.slice(0, idx).trim();
      const value = decl.slice(idx + 1).trim();
      if (name.startsWith("--") && value) vars[name] = value;
    }
  }
  return vars;
}

/** Resolve var(--x[, fallback]) recursively against a var map. Returns {value, unresolved}. */
function resolveVars(rawValue, varMap, depth = 0) {
  if (depth > 8 || !rawValue) return { value: rawValue, unresolved: false };
  let unresolved = false;
  const varRe = /var\(\s*(--[\w-]+)\s*(?:,\s*([^()]*(?:\([^()]*\)[^()]*)*))?\)/;
  let value = rawValue;
  let m;
  let iterations = 0;
  while ((m = varRe.exec(value)) !== null && iterations < 20) {
    iterations++;
    const [full, name, fallback] = m;
    if (Object.prototype.hasOwnProperty.call(varMap, name)) {
      const resolved = resolveVars(varMap[name], varMap, depth + 1);
      if (resolved.unresolved) unresolved = true;
      value = value.slice(0, m.index) + resolved.value + value.slice(m.index + full.length);
    } else if (fallback !== undefined) {
      const resolved = resolveVars(fallback.trim(), varMap, depth + 1);
      if (resolved.unresolved) unresolved = true;
      value = value.slice(0, m.index) + resolved.value + value.slice(m.index + full.length);
    } else {
      unresolved = true;
      break; // leave the var(...) text in place so the caller can report it verbatim
    }
  }
  return { value, unresolved };
}

/** classname -> [{selectorText, body}] for rules whose selector is exactly `.classname`. */
function getSimpleClassRules(source) {
  const stripped = stripAtRuleBlocks(source);
  const map = new Map();
  const ruleRe = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = ruleRe.exec(stripped)) !== null) {
    const selectors = m[1].split(",").map((s) => s.trim());
    for (const sel of selectors) {
      const single = /^\.([a-zA-Z][\w-]*)$/.exec(sel);
      if (!single) continue; // combinator, pseudo, compound, id, tag, attr — out of scope
      const cls = single[1];
      if (!map.has(cls)) map.set(cls, []);
      map.get(cls).push(m[2]);
    }
  }
  return map;
}

// ---------------------------------------------------------------------------------------------
// Scoped resolution (v3, app stylesheet only): this app's CSS is deliberately written scoped —
// `.herd-signals-page .fbar`, `.herd-signals-page .drawer .hchart` — because bare classes like
// `.btn`, `.tag`, `.kpi` are shared by dozens of other admin screens and restyling the bare class
// globally would break them. getSimpleClassRules() above only ever sees an EXACT `.classname`
// selector, so every correctly-scoped app rule was invisible to it — it reported ~300 mismatches
// that were not real, and an earlier pass "fixed" some of them by adding bare `.classname`
// duplicates purely so this guard could see them, which pollutes a global stylesheet just to
// satisfy a checker. This resolver instead gathers every rule whose selector's MOST SPECIFIC
// (rightmost) compound includes the target class — `.foo`, `.a .foo`, `.a .b .foo` all count,
// but a compound like `.a.foo` counts only for `foo` (the last-written class token), never for
// `a`, because it only matches elements that carry BOTH classes — and resolves declarations the
// way a browser would: higher specificity wins; among rules of EQUAL specificity, later source
// order wins UNLESS the tied rules have different selector text and disagree on a property's
// value, in which case that is a legitimate context-dependent split (e.g. `.drawer .hchart` vs
// `.fs .hchart` sizing the same class differently in different places) and is reported as a NOTE,
// never a mismatch or a failure.
//
// Excluded, same as the existing declared blind spots: any selector containing a pseudo-class /
// pseudo-element (`:`) or an attribute selector (`[`) — those are conditional on state or markup
// this guard cannot evaluate statically, so they are left out of scope rather than guessed at.

/** Split a selector into its compound-selector chain, e.g. "a.b > c.d" -> ["a.b", "c.d"]. */
function splitCompounds(selectorText) {
  return selectorText
    .replace(/[>+~]/g, " ")
    .split(/\s+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/** Real CSS specificity as [ids, classes-ish, elements], for the small subset of selectors we support. */
function selectorSpecificity(selectorText) {
  const ids = (selectorText.match(/#[\w-]+/g) || []).length;
  const classes = (selectorText.match(/\.[\w-]+/g) || []).length;
  let elements = 0;
  for (const compound of splitCompounds(selectorText)) {
    if (/^[a-zA-Z]/.test(compound)) elements += 1; // compound starts with a tag name, not . or #
  }
  return [ids, classes, elements];
}

function cmpSpecificity(a, b) {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] - b[i];
  }
  return 0;
}

/**
 * classname -> [{ props, specificity, order, selectorText }] for every app rule whose selector's
 * rightmost compound includes that class, excluding pseudo/attribute-conditioned selectors.
 */
function getScopedClassRules(source) {
  const stripped = stripAtRuleBlocks(source);
  const map = new Map();
  const ruleRe = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  let order = 0;
  while ((m = ruleRe.exec(stripped)) !== null) {
    const selectors = m[1].split(",").map((s) => s.trim());
    const body = m[2];
    for (const sel of selectors) {
      if (!sel || sel.includes(":") || sel.includes("[")) continue; // pseudo/attr — out of scope
      const compounds = splitCompounds(sel);
      if (compounds.length === 0) continue;
      const last = compounds[compounds.length - 1];
      const lastClasses = (last.match(/\.[a-zA-Z][\w-]*/g) || []).map((c) => c.slice(1));
      if (lastClasses.length === 0) continue; // last compound has no class at all — id/tag only
      // A compound like `.a.foo` only matches elements that carry BOTH classes, so its
      // declarations are conditional on the OTHER class(es) too — e.g. `.btn.dng` and
      // `.btn.ghost` must never bleed into plain `.btn` resolution (each requires a modifier
      // class `.btn` alone doesn't have). Only the last-written class token in the last compound
      // is "the most specific class" this rule resolves for; earlier tokens in the same compound
      // are a condition on that class, not a class this rule is a candidate for on their own.
      const cls = lastClasses[lastClasses.length - 1];
      const props = resolveDeclProps([body]);
      const specificity = selectorSpecificity(sel);
      order += 1;
      if (!map.has(cls)) map.set(cls, []);
      map.get(cls).push({ props, specificity, order, selectorText: sel });
    }
  }
  return map;
}

/**
 * Resolve one class's cascade from its scoped rule entries. Returns the winning prop -> value
 * map plus the set of properties where equal-specificity rules disagreed across different
 * selector contexts (legitimate context-dependent styling — report as a NOTE, not a mismatch).
 */
function resolveScopedProps(entries) {
  const byProp = new Map();
  for (const entry of entries) {
    for (const [prop, value] of Object.entries(entry.props)) {
      if (!byProp.has(prop)) byProp.set(prop, []);
      byProp.get(prop).push({ value, specificity: entry.specificity, order: entry.order, selectorText: entry.selectorText });
    }
  }
  const props = {};
  const ambiguousProps = new Set();
  for (const [prop, candidates] of byProp) {
    let maxSpec = candidates[0].specificity;
    for (const c of candidates) {
      if (cmpSpecificity(c.specificity, maxSpec) > 0) maxSpec = c.specificity;
    }
    const top = candidates.filter((c) => cmpSpecificity(c.specificity, maxSpec) === 0);
    const uniqueValues = new Set(top.map((c) => c.value.trim()));
    if (uniqueValues.size === 1) {
      props[prop] = top[0].value;
      continue;
    }
    const uniqueSelectors = new Set(top.map((c) => c.selectorText));
    if (uniqueSelectors.size > 1) {
      // Same specificity, different scope, genuinely different values — context-dependent, not a bug.
      ambiguousProps.add(prop);
    } else {
      // Same selector repeated (e.g. duplicated rule) — real cascade: latest source order wins.
      top.sort((a, b) => a.order - b.order);
      props[prop] = top[top.length - 1].value;
    }
  }
  return { props, ambiguousProps };
}

/** Flatten a class's ordered declaration bodies into a single prop -> raw value map (cascade). */
function resolveDeclProps(bodies) {
  const props = {};
  for (const body of bodies) {
    for (const decl of body.split(";")) {
      const idx = decl.indexOf(":");
      if (idx === -1) continue;
      const name = decl.slice(0, idx).trim().toLowerCase();
      const value = decl.slice(idx + 1).trim();
      if (name && value) props[name] = value; // later declarations win, like the cascade
    }
  }
  return props;
}

// ---- normalisation helpers -------------------------------------------------------------------

const NAMED_COLORS = {
  transparent: "0,0,0,0",
  white: "255,255,255,1",
  black: "0,0,0,1",
};

function hexToRgba(hex) {
  let h = hex.replace("#", "");
  if (h.length === 3 || h.length === 4) h = h.split("").map((c) => c + c).join("");
  if (h.length !== 6 && h.length !== 8) return null;
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  const a = h.length === 8 ? Math.round((parseInt(h.slice(6, 8), 16) / 255) * 1000) / 1000 : 1;
  if ([r, g, b].some((n) => Number.isNaN(n))) return null;
  return `${r},${g},${b},${a}`;
}

function rgbFnToRgba(value) {
  const m = /^rgba?\(\s*([\d.]+)\s*,?\s*([\d.]+)\s*,?\s*([\d.]+)\s*(?:[,/]\s*([\d.]+%?))?\s*\)$/i.exec(
    value.trim(),
  );
  if (!m) return null;
  const [, r, g, b, aRaw] = m;
  let a = 1;
  if (aRaw !== undefined) a = aRaw.endsWith("%") ? parseFloat(aRaw) / 100 : parseFloat(aRaw);
  return `${Math.round(+r)},${Math.round(+g)},${Math.round(+b)},${Math.round(a * 1000) / 1000}`;
}

/** Normalise a colour-ish CSS value to "r,g,b,a", or null if it can't be parsed as a pure colour. */
function normalizeColor(value) {
  const v = value.trim();
  if (!v) return null;
  if (v.toLowerCase() === "currentcolor" || v.toLowerCase() === "inherit") return v.toLowerCase();
  if (NAMED_COLORS[v.toLowerCase()]) return NAMED_COLORS[v.toLowerCase()];
  if (/^#[0-9a-f]{3,8}$/i.test(v)) return hexToRgba(v);
  if (/^rgba?\(/i.test(v)) return rgbFnToRgba(v);
  return null; // gradients, url(), keywords we don't map — treated as unresolved, not failed
}

/** Generic value normaliser for non-colour tracked props: resolve vars, collapse ws, zero-unit. */
function normalizeGeneric(rawValue, varMap) {
  const { value, unresolved } = resolveVars(rawValue, varMap);
  let v = value.replace(/\s+/g, " ").trim().toLowerCase();
  v = v.replace(/\b0(px|em|rem|%|vh|vw|pt)\b/g, "0"); // 0px === 0 etc.
  return { value: v, unresolved };
}

/** Resolve+normalise a value that MIGHT be a colour; falls back to generic text compare. */
function normalizeMaybeColor(rawValue, varMap) {
  const { value, unresolved } = resolveVars(rawValue, varMap);
  if (unresolved) return { value: value.trim().toLowerCase(), unresolved: true, isColor: false };
  const color = normalizeColor(value);
  if (color) return { value: color, unresolved: false, isColor: true };
  return { value: value.replace(/\s+/g, " ").trim().toLowerCase(), unresolved: false, isColor: false };
}

const BOX_SIDES = ["top", "right", "bottom", "left"]; // also used for radius corner order (tl,tr,br,bl)

function expand4Value(rawValue) {
  const parts = rawValue.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 1) return [parts[0], parts[0], parts[0], parts[0]];
  if (parts.length === 2) return [parts[0], parts[1], parts[0], parts[1]];
  if (parts.length === 3) return [parts[0], parts[1], parts[2], parts[1]];
  if (parts.length >= 4) return [parts[0], parts[1], parts[2], parts[3]];
  return [null, null, null, null];
}

/** Resolve a box property (padding/margin/border-radius) to normalised [top,right,bottom,left]. */
function resolveBox(props, shorthandName, longhandPrefix, varMap) {
  const result = [null, null, null, null];
  let anyResolved = false;
  let anyUnresolved = false;
  if (props[shorthandName]) {
    const { value: resolvedShorthand, unresolved } = resolveVars(props[shorthandName], varMap);
    if (unresolved) anyUnresolved = true;
    const sides = expand4Value(resolvedShorthand);
    for (let i = 0; i < 4; i++) {
      if (sides[i] != null) {
        result[i] = normalizeGeneric(sides[i], varMap).value;
        anyResolved = true;
      }
    }
  }
  BOX_SIDES.forEach((side, i) => {
    const longhand = `${longhandPrefix}-${side}`;
    if (props[longhand] !== undefined) {
      const { value, unresolved } = normalizeGeneric(props[longhand], varMap);
      if (unresolved) anyUnresolved = true;
      result[i] = value;
      anyResolved = true;
    }
  });
  if (!anyResolved) return null;
  return { sides: result, unresolved: anyUnresolved };
}

/** border-radius uses tl/tr/br/bl longhands rather than top/right/bottom/left. */
function resolveRadius(props, varMap) {
  const longhandNames = [
    "border-top-left-radius",
    "border-top-right-radius",
    "border-bottom-right-radius",
    "border-bottom-left-radius",
  ];
  const result = [null, null, null, null];
  let anyResolved = false;
  let anyUnresolved = false;
  if (props["border-radius"]) {
    const { value: resolvedShorthand, unresolved } = resolveVars(props["border-radius"], varMap);
    if (unresolved) anyUnresolved = true;
    const sides = expand4Value(resolvedShorthand);
    for (let i = 0; i < 4; i++) {
      if (sides[i] != null) {
        result[i] = normalizeGeneric(sides[i], varMap).value;
        anyResolved = true;
      }
    }
  }
  longhandNames.forEach((name, i) => {
    if (props[name] !== undefined) {
      const { value, unresolved } = normalizeGeneric(props[name], varMap);
      if (unresolved) anyUnresolved = true;
      result[i] = value;
      anyResolved = true;
    }
  });
  if (!anyResolved) return null;
  return { sides: result, unresolved: anyUnresolved };
}

const BORDER_STYLES = new Set([
  "none", "hidden", "dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset",
]);

/** Best-effort border shorthand splitter: {width, style, color}, longhands override shorthand. */
function resolveBorder(props, varMap) {
  const parts = { width: null, style: null, color: null };
  let anyResolved = false;
  let anyUnresolved = false;
  if (props.border) {
    const { value: resolved, unresolved } = resolveVars(props.border, varMap);
    if (unresolved) anyUnresolved = true;
    for (const token of resolved.split(/\s+/).filter(Boolean)) {
      if (BORDER_STYLES.has(token.toLowerCase())) parts.style = token.toLowerCase();
      else if (/^\d/.test(token) || token === "0") parts.width = normalizeGeneric(token, varMap).value;
      else parts.color = normalizeMaybeColor(token, varMap).value;
    }
    if (parts.width || parts.style || parts.color) anyResolved = true;
  }
  for (const [prop, key] of [
    ["border-width", "width"],
    ["border-style", "style"],
    ["border-color", "color"],
  ]) {
    if (props[prop] !== undefined) {
      const norm = key === "color" ? normalizeMaybeColor(props[prop], varMap) : normalizeGeneric(props[prop], varMap);
      if (norm.unresolved) anyUnresolved = true;
      parts[key] = norm.value;
      anyResolved = true;
    }
  }
  if (!anyResolved) return null;
  return { parts, unresolved: anyUnresolved };
}

const TRACKED_COLOR_PROPS = ["color", "background", "background-color", "border-color"];
const TRACKED_TEXT_PROPS = [
  "font-size", "font-weight", "display", "gap", "row-gap", "column-gap",
  "grid-template", "grid-template-columns", "grid-template-rows", "grid-template-areas",
];

/**
 * Compare one class's resolved declarations between mock and app. Only properties the MOCK
 * actually sets are compared — the mock is authoritative on what matters for that class; if the
 * app additionally sets something the mock doesn't mention, that's not this guard's concern.
 */
function compareClassDeclarations(cls, mockProps, appProps, mockVars, appVars, ambiguousProps = new Set()) {
  const mismatches = [];
  const unresolvedNotes = [];

  for (const prop of TRACKED_COLOR_PROPS) {
    if (mockProps[prop] === undefined) continue;
    if (ambiguousProps.has(prop)) {
      unresolvedNotes.push(
        `${cls}.${prop}: context-dependent in the app (equal-specificity rules under different scopes disagree) — not compared, not a mismatch`,
      );
      continue;
    }
    const mockNorm = normalizeMaybeColor(mockProps[prop], mockVars);
    if (appProps[prop] === undefined) {
      mismatches.push({ prop, mock: mockProps[prop], app: "(not set)" });
      continue;
    }
    const appNorm = normalizeMaybeColor(appProps[prop], appVars);
    if (mockNorm.unresolved || appNorm.unresolved) {
      unresolvedNotes.push(`${cls}.${prop}: could not fully resolve var() — mock="${mockProps[prop]}" app="${appProps[prop]}"`);
      continue;
    }
    if (mockNorm.value !== appNorm.value) {
      mismatches.push({ prop, mock: mockProps[prop], app: appProps[prop] });
    }
  }

  for (const prop of TRACKED_TEXT_PROPS) {
    if (mockProps[prop] === undefined) continue;
    if (ambiguousProps.has(prop)) {
      unresolvedNotes.push(
        `${cls}.${prop}: context-dependent in the app (equal-specificity rules under different scopes disagree) — not compared, not a mismatch`,
      );
      continue;
    }
    const mockNorm = normalizeGeneric(mockProps[prop], mockVars);
    if (appProps[prop] === undefined) {
      mismatches.push({ prop, mock: mockProps[prop], app: "(not set)" });
      continue;
    }
    const appNorm = normalizeGeneric(appProps[prop], appVars);
    if (mockNorm.unresolved || appNorm.unresolved) {
      unresolvedNotes.push(`${cls}.${prop}: could not fully resolve var() — mock="${mockProps[prop]}" app="${appProps[prop]}"`);
      continue;
    }
    if (mockNorm.value !== appNorm.value) {
      mismatches.push({ prop, mock: mockProps[prop], app: appProps[prop] });
    }
  }

  for (const [box, shorthand, prefix] of [
    ["padding", "padding", "padding"],
    ["margin", "margin", "margin"],
  ]) {
    const mockBox = resolveBox(mockProps, shorthand, prefix, mockVars);
    if (!mockBox) continue;
    if ([shorthand, ...BOX_SIDES.map((s) => `${prefix}-${s}`)].some((p) => ambiguousProps.has(p))) {
      unresolvedNotes.push(`${cls}.${box}: context-dependent in the app — not compared, not a mismatch`);
      continue;
    }
    const appBox = resolveBox(appProps, shorthand, prefix, appVars);
    if (!appBox) {
      mismatches.push({ prop: box, mock: mockProps[shorthand] ?? "(longhand)", app: "(not set)" });
      continue;
    }
    if (mockBox.unresolved || appBox.unresolved) {
      unresolvedNotes.push(`${cls}.${box}: could not fully resolve var() on one side`);
      continue;
    }
    for (let i = 0; i < 4; i++) {
      if (mockBox.sides[i] == null) continue;
      if (mockBox.sides[i] !== appBox.sides[i]) {
        mismatches.push({ prop: `${box}-${BOX_SIDES[i]}`, mock: mockBox.sides[i], app: appBox.sides[i] ?? "(not set)" });
      }
    }
  }

  const RADIUS_PROPS = [
    "border-radius",
    "border-top-left-radius",
    "border-top-right-radius",
    "border-bottom-right-radius",
    "border-bottom-left-radius",
  ];
  const mockRadius = resolveRadius(mockProps, mockVars);
  if (mockRadius && RADIUS_PROPS.some((p) => ambiguousProps.has(p))) {
    unresolvedNotes.push(`${cls}.border-radius: context-dependent in the app — not compared, not a mismatch`);
  } else if (mockRadius) {
    const appRadius = resolveRadius(appProps, appVars);
    if (!appRadius) {
      mismatches.push({ prop: "border-radius", mock: mockProps["border-radius"] ?? "(longhand)", app: "(not set)" });
    } else if (mockRadius.unresolved || appRadius.unresolved) {
      unresolvedNotes.push(`${cls}.border-radius: could not fully resolve var() on one side`);
    } else {
      const corners = ["top-left", "top-right", "bottom-right", "bottom-left"];
      for (let i = 0; i < 4; i++) {
        if (mockRadius.sides[i] == null) continue;
        if (mockRadius.sides[i] !== appRadius.sides[i]) {
          mismatches.push({ prop: `border-radius-${corners[i]}`, mock: mockRadius.sides[i], app: appRadius.sides[i] ?? "(not set)" });
        }
      }
    }
  }

  const BORDER_PROPS = ["border", "border-width", "border-style", "border-color"];
  const mockBorder = resolveBorder(mockProps, mockVars);
  if (mockBorder && BORDER_PROPS.some((p) => ambiguousProps.has(p))) {
    unresolvedNotes.push(`${cls}.border: context-dependent in the app — not compared, not a mismatch`);
  } else if (mockBorder) {
    const appBorder = resolveBorder(appProps, appVars);
    if (!appBorder) {
      mismatches.push({ prop: "border", mock: mockProps.border ?? "(longhand)", app: "(not set)" });
    } else if (mockBorder.unresolved || appBorder.unresolved) {
      unresolvedNotes.push(`${cls}.border: could not fully resolve var() on one side`);
    } else {
      for (const part of ["width", "style", "color"]) {
        if (mockBorder.parts[part] == null) continue;
        if (mockBorder.parts[part] !== appBorder.parts[part]) {
          mismatches.push({ prop: `border-${part}`, mock: mockBorder.parts[part], app: appBorder.parts[part] ?? "(not set)" });
        }
      }
    }
  }

  return { mismatches, unresolvedNotes };
}

function walkTsx(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) walkTsx(full, out);
    else if (/\.(tsx|jsx)$/.test(entry.name)) out.push(full);
  }
  return out;
}

function checkModule(mod) {
  const missing = [];
  const mismatches = [];
  const unresolvedNotes = [];
  const banned = [];

  const mockPath = join(repoRoot, mod.mock);
  if (!existsSync(mockPath)) return { missing, mismatches, unresolvedNotes, banned, scanned: 0 };
  const mockSource = readFileSync(mockPath, "utf8");

  const mockClasses = classesDefinedIn(mockSource);
  const mockVars = parseRootVars(mockSource);
  const mockRules = getSimpleClassRules(mockSource);

  const styled = new Set();
  const appVars = {};
  const appScopedRules = new Map(); // cls -> [{props, specificity, order, selectorText}], scope-aware (v3)
  for (const sheet of mod.stylesheets) {
    const p = join(repoRoot, sheet);
    if (!existsSync(p)) continue;
    const sheetSource = readFileSync(p, "utf8");
    for (const c of classesDefinedIn(sheetSource)) styled.add(c);
    Object.assign(appVars, parseRootVars(sheetSource));
    for (const [cls, entries] of getScopedClassRules(sheetSource)) {
      if (!appScopedRules.has(cls)) appScopedRules.set(cls, []);
      appScopedRules.get(cls).push(...entries);
    }
  }

  const files = walkTsx(join(repoRoot, mod.componentsDir));
  for (const file of files) {
    const relFile = file.replace(`${repoRoot}/`, "");
    const source = readFileSync(file, "utf8");

    for (const divergence of DELIBERATE_MOCK_DIVERGENCES) {
      for (const pattern of divergence.forbiddenInComponents) {
        if (pattern.test(source)) {
          banned.push({ file: relFile, id: divergence.id, reason: divergence.reason, pattern: String(pattern) });
        }
      }
    }

    const used = classesUsedIn(source);
    for (const cls of used) {
      if (!mockClasses.has(cls)) continue; // mock is not the authority on this class
      if (!styled.has(cls)) {
        missing.push({ file: relFile, cls, mock: mod.mock });
        continue;
      }
      const mockProps = mockRules.has(cls) ? resolveDeclProps(mockRules.get(cls)) : null;
      if (!mockProps || Object.keys(mockProps).length === 0) continue; // no simple-selector rule to compare (e.g. compound-only)
      const { props: appProps, ambiguousProps } = appScopedRules.has(cls)
        ? resolveScopedProps(appScopedRules.get(cls))
        : { props: {}, ambiguousProps: new Set() };
      const { mismatches: found, unresolvedNotes: notes } = compareClassDeclarations(
        cls,
        mockProps,
        appProps,
        mockVars,
        appVars,
        ambiguousProps,
      );
      for (const f of found) {
        mismatches.push({ file: relFile, cls, mockFile: mod.mock, prop: f.prop, mockValue: f.mock, appValue: f.app });
      }
      unresolvedNotes.push(...notes);
    }
  }
  return { missing, mismatches, unresolvedNotes: [...new Set(unresolvedNotes)], banned, scanned: files.length };
}

function selfTest() {
  const problems = [];

  const usedOk = classesUsedIn('<div className="grid2 card" />');
  const usedTpl = classesUsedIn("<div className={`kpi ${tone}`} />");
  const defined = classesDefinedIn(".grid2{display:grid}\n.kpi .val{font-weight:700}");
  if (!usedOk.has("grid2") || !usedOk.has("card")) problems.push("static className not parsed");
  if (!usedTpl.has("kpi")) problems.push("template-literal static part not parsed");
  if (usedTpl.has("tone")) problems.push("interpolation leaked into class list");
  if (!defined.has("grid2") || !defined.has("kpi") || !defined.has("val")) problems.push("selector parse failed");

  // Declaration comparison: FAIL case — same class name, genuinely different colour.
  {
    const mockCss = ".badge{color:#ff0000;padding:4px 8px}";
    const appCss = ".badge{color:#00ff00;padding:4px 8px}";
    const mockRules = getSimpleClassRules(mockCss);
    const appRules = getSimpleClassRules(appCss);
    const mockProps = resolveDeclProps(mockRules.get("badge"));
    const appProps = resolveDeclProps(appRules.get("badge"));
    const { mismatches } = compareClassDeclarations("badge", mockProps, appProps, {}, {});
    if (!mismatches.some((m) => m.prop === "color")) problems.push("declaration diff did not catch a real colour mismatch");
  }

  // Declaration comparison: PASS case — hex vs rgb() of the same colour must not fire.
  {
    const mockCss = ".chip{color:#ff0000}";
    const appCss = ".chip{color:rgb(255, 0, 0)}";
    const mockProps = resolveDeclProps(getSimpleClassRules(mockCss).get("chip"));
    const appProps = resolveDeclProps(getSimpleClassRules(appCss).get("chip"));
    const { mismatches } = compareClassDeclarations("chip", mockProps, appProps, {}, {});
    if (mismatches.length) problems.push("declaration diff false-positived on hex vs rgb() of the same colour");
  }

  // Declaration comparison: PASS case — var() resolving to the mock's literal must not fire.
  {
    const mockCss = ".pill{background:#7ccb45}";
    const appCss = ".pill{background:var(--brand)}";
    const appVars = { "--brand": "#7ccb45" };
    const mockProps = resolveDeclProps(getSimpleClassRules(mockCss).get("pill"));
    const appProps = resolveDeclProps(getSimpleClassRules(appCss).get("pill"));
    const { mismatches, unresolvedNotes } = compareClassDeclarations("pill", mockProps, appProps, {}, appVars);
    if (mismatches.length) problems.push("declaration diff false-positived on var() resolving to the mock's literal");
    if (unresolvedNotes.length) problems.push("var() that resolves cleanly was reported as unresolved");
  }

  // Declaration comparison: an unresolvable var() must be reported, not silently passed or failed.
  {
    const mockCss = ".ghost{color:#111111}";
    const appCss = ".ghost{color:var(--not-defined-anywhere)}";
    const mockProps = resolveDeclProps(getSimpleClassRules(mockCss).get("ghost"));
    const appProps = resolveDeclProps(getSimpleClassRules(appCss).get("ghost"));
    const { mismatches, unresolvedNotes } = compareClassDeclarations("ghost", mockProps, appProps, {}, {});
    if (mismatches.length) problems.push("unresolvable var() was reported as a hard mismatch instead of unresolved");
    if (!unresolvedNotes.length) problems.push("unresolvable var() was silently dropped instead of being reported");
  }

  // Compound-selector classes (e.g. `.srcl.inferred`) must not manufacture a declaration to compare.
  {
    const mockCss = ".srcl.inferred{background:#a78bf5}";
    const mockRules = getSimpleClassRules(mockCss);
    if (mockRules.has("inferred")) problems.push("compound selector leaked into the simple-class-rule map");
  }

  // Scoped resolution (v3): a scoped app rule matching the mock must PASS — this is the exact
  // shape (`.herd-signals-page .fbar`) the guard used to be blind to and would false-positive on.
  {
    const mockCss = ".fbar{display:flex;gap:8px;padding:10px 14px}";
    const appCss = ".herd-signals-page .fbar{display:flex;gap:8px;padding:10px 14px}";
    const mockProps = resolveDeclProps(getSimpleClassRules(mockCss).get("fbar"));
    const scoped = getScopedClassRules(appCss).get("fbar") || [];
    const { props: appProps, ambiguousProps } = resolveScopedProps(scoped);
    const { mismatches } = compareClassDeclarations("fbar", mockProps, appProps, {}, {}, ambiguousProps);
    if (mismatches.length) problems.push("scoped resolution false-positived on a correctly-scoped rule matching the mock");
  }

  // Scoped resolution: a scoped app rule that genuinely diverges from the mock must FAIL.
  {
    const mockCss = ".fbar{padding:10px 14px}";
    const appCss = ".herd-signals-page .fbar{padding:4px 4px}";
    const mockProps = resolveDeclProps(getSimpleClassRules(mockCss).get("fbar"));
    const scoped = getScopedClassRules(appCss).get("fbar") || [];
    const { props: appProps, ambiguousProps } = resolveScopedProps(scoped);
    const { mismatches } = compareClassDeclarations("fbar", mockProps, appProps, {}, {}, ambiguousProps);
    if (!mismatches.length) problems.push("scoped resolution did not catch a real divergence in a scoped rule");
  }

  // Scoped resolution: a bare app-wide rule must lose to a same-feature scoped override.
  {
    const mockCss = ".btn{padding:7px 12px}";
    const appCss = ".btn{padding:8px 13px}\n.herd-signals-page .fs .btn{padding:7px 12px}";
    const mockProps = resolveDeclProps(getSimpleClassRules(mockCss).get("btn"));
    const scoped = getScopedClassRules(appCss).get("btn") || [];
    const { props: appProps, ambiguousProps } = resolveScopedProps(scoped);
    const { mismatches } = compareClassDeclarations("btn", mockProps, appProps, {}, {}, ambiguousProps);
    if (mismatches.length) problems.push("scoped resolution let a lower-specificity bare rule beat a higher-specificity scoped override");
  }

  // Scoped resolution: a compound modifier rule (`.btn.dng`) must never bleed into the BASE
  // class's resolution — it only matches elements that also carry the modifier class.
  {
    const appCss = ".btn{color:#111}\n.btn.dng{color:#f00}";
    const scoped = getScopedClassRules(appCss).get("btn") || [];
    const { props } = resolveScopedProps(scoped);
    if (props.color !== "#111") problems.push("a compound modifier rule (.btn.dng) leaked into the base class (.btn) resolution");
    const dngScoped = getScopedClassRules(appCss).get("dng") || [];
    if (!dngScoped.length) problems.push("a compound modifier rule (.btn.dng) did not attach to its own last-class token (dng)");
  }

  // Scoped resolution: equal-specificity rules under DIFFERENT scopes that disagree on a property
  // (e.g. `.drawer .hchart` vs `.fs .hchart`) must be reported as a NOTE, never a FAIL — this is
  // the maintainer's explicit example of legitimate context-dependent styling.
  {
    const appCss = ".herd-signals-page .drawer .hchart{height:110px}\n.herd-signals-page .fs .hchart{height:240px}";
    const scoped = getScopedClassRules(appCss).get("hchart") || [];
    const { ambiguousProps } = resolveScopedProps(scoped);
    if (!ambiguousProps.has("height")) problems.push("equal-specificity, different-scope divergence was not flagged as context-dependent");

    // height isn't in the tracked-props list; use a tracked one (gap) to prove the end-to-end NOTE path.
    const mockCss2 = ".x{gap:8px}";
    const appCss2 = ".herd-signals-page .drawer .x{gap:4px}\n.herd-signals-page .fs .x{gap:12px}";
    const mockProps2 = resolveDeclProps(getSimpleClassRules(mockCss2).get("x"));
    const scoped2 = getScopedClassRules(appCss2).get("x") || [];
    const { props: appProps2, ambiguousProps: amb2 } = resolveScopedProps(scoped2);
    const { mismatches: m2, unresolvedNotes: notes2 } = compareClassDeclarations("x", mockProps2, appProps2, {}, {}, amb2);
    if (m2.length) problems.push("context-dependent equal-specificity divergence was reported as a mismatch instead of a note");
    if (!notes2.some((n) => n.includes("context-dependent"))) problems.push("context-dependent divergence produced no NOTE");
  }

  // Scoped resolution: pseudo-class / attribute-conditioned selectors stay out of scope.
  {
    const appCss = ".herd-signals-page .btn:hover{color:red}\n.herd-signals-page .btn[disabled]{color:blue}";
    const scoped = getScopedClassRules(appCss).get("btn") || [];
    if (scoped.length) problems.push("pseudo-class/attribute selector leaked into scoped rule resolution");
  }

  // Regression: a comment sitting directly above a rule, whose prose happens to contain a colon
  // (e.g. "chartnote:"), must not get swallowed into the selector text and trip the pseudo
  // exclusion — this silently dropped a real, correctly-scoped .chartnote rule during development.
  {
    const appCss =
      "/* Mock's .chartnote: the small print under a chart. */\n.herd-signals-page .chartnote{color:#111}";
    const scoped = getScopedClassRules(appCss).get("chartnote") || [];
    if (!scoped.length) problems.push("a comment containing ':' directly above a rule was swallowed into the selector and dropped the rule");
  }

  // Banned-divergence scan: the literal battery-life text must be caught even though its class
  // combo is fully styled in the app theme (the exact scenario the CSS checks would miss).
  {
    const fakeSource = 'export const X = () => <span className="srcl inferred">est. ~1.7 year left</span>;';
    const hit = DELIBERATE_MOCK_DIVERGENCES[0].forbiddenInComponents.some((p) => p.test(fakeSource));
    if (!hit) problems.push("banned-divergence scan did not catch reintroduced battery-life markup");
  }
  {
    const safeSource = 'export const X = () => <span className="srcl derived">Derived</span>;';
    const falseHit = DELIBERATE_MOCK_DIVERGENCES[0].forbiddenInComponents.some((p) => p.test(safeSource));
    if (falseHit) problems.push("banned-divergence scan false-positived on an unrelated srcl usage");
  }

  if (problems.length) {
    console.error(`check-mock-css-parity self-test: FAIL\n- ${problems.join("\n- ")}`);
    process.exit(1);
  }
  console.log("check-mock-css-parity self-test: PASS");
  process.exit(0);
}

if (process.argv.includes("--self-test")) selfTest();

let totalMissing = 0;
let totalMismatches = 0;
let totalBanned = 0;
let scanned = 0;
const allUnresolved = [];

for (const mod of MODULES) {
  const { missing, mismatches, unresolvedNotes, banned, scanned: n } = checkModule(mod);
  scanned += n;
  allUnresolved.push(...unresolvedNotes);

  for (const f of missing) {
    if (totalMissing === 0) console.error("mock-css-parity: a rendered class has no rule in the app stylesheet");
    console.error(
      `- ${f.file}: class "${f.cls}" is defined in ${f.mock} but has NO rule in the app stylesheet — port the mock's CSS, do not ship the markup alone`,
    );
    totalMissing += 1;
  }

  for (const f of mismatches) {
    if (totalMismatches === 0) console.error("mock-css-parity: a rendered class is styled, but its declarations diverge from the mock");
    console.error(
      `- ${f.file}: class "${f.cls}" property "${f.prop}" — mock (${f.mockFile}) has "${f.mockValue}", app has "${f.appValue}"`,
    );
    totalMismatches += 1;
  }

  for (const f of banned) {
    if (totalBanned === 0) console.error("mock-css-parity: a deliberately-deleted mock element has reappeared");
    console.error(`- ${f.file}: matched banned pattern ${f.pattern} for divergence "${f.id}" — ${f.reason}`);
    totalBanned += 1;
  }
}

if (allUnresolved.length) {
  console.error("\nmock-css-parity: could not fully resolve the following (not counted as failures — reported so the gap is visible):");
  for (const note of allUnresolved) console.error(`- ${note}`);
}

const total = totalMissing + totalMismatches + totalBanned;
if (total > 0) {
  console.error(
    `\n${totalMissing} missing-style finding(s), ${totalMismatches} declaration-mismatch finding(s), ${totalBanned} banned-reintroduction finding(s).`,
  );
  console.error(
    "Porting markup from a mock without porting (or matching) its CSS renders unstyled or wrong, not broken: no type error, no test failure, just a screen that does not match the design.",
  );
  process.exit(1);
}
console.log(
  `mock-css-parity: ok (${scanned} component file(s) scanned against ${MODULES.length} mock(s); ${allUnresolved.length} unresolved value(s) noted above)`,
);
