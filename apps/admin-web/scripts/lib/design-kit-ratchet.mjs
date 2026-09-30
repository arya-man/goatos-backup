// design-kit-ratchet.mjs — MUI Minimal kit + token enforcement for admin-web.
//
// Imported by scripts/check-design-system.mjs (`npm run design:guard`). Each check returns
// findings; the guard counts them per `check|file` and compares with the explicit per-file
// allowance in scripts/check-design-system-waivers/design-system-waivers.json ("ratchet").
// A file may never exceed its allowance, and a file with no allowance (every NEW file) must be
// clean. Allowances only ever shrink: --update-baseline refuses to raise one.
//
// Spec: docs/design/mui-minimal-spec.md. Tokens: app/minimal-tokens.css.

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { basename, join, relative, sep } from "node:path";
import { SHRINK_RATCHET_CHECKS } from "./shrink-ratchets.mjs";

export const TOKENS_FILE = "app/minimal-tokens.css";
// Colour/theme token files own literal values; they are checked by the P0 palette rules.
const TOKEN_OWNERS = new Set([TOKENS_FILE, "theme/mesha-tokens.ts", "app/globals.css"]);
// Kit and shared primitives are the only places allowed to render the raw element they wrap.
const PRIMITIVE_OWNERS = [
  /^components\/kit\//,
  /^components\/app\/time-field\.tsx$/,
  /^components\/app\/date-time-field\.tsx$/,
  /^components\/app\/date-range-field\.tsx$/,
  /^components\/data-table\.tsx$/,
  /^components\/dense-table\.tsx$/,
  /^components\/ui-primitives\.tsx$/,
  /^components\/form-select\.tsx$/,
];

export const RATCHET_CHECKS = {
  "raw-px": "raw px spacing/size literal; use a token from app/minimal-tokens.css (var(--sp-*), --btn-h, --input-h, --table-row-h …)",
  "raw-radius": "raw border-radius; use var(--r-sm|--r-md|--r-lg|--r-xl|--r-pill|--r-round) or a --radius-* token",
  "raw-shadow": "raw box-shadow; use var(--shadow-z1…z24|--shadow-card|--shadow-dropdown|--shadow-dialog)",
  "raw-font-size": "raw font-size; use the type scale var(--fs-h1…h6|--fs-body1|--fs-body2|--fs-caption …)",
  "raw-table": "raw <table> in feature code; use DataTable / DenseTable (components/data-table, dense-table)",
  "raw-input": "raw text-like <input> in feature code; use kit TextField (dates: DateRangeField/DateTimeField/TimeField). Checkbox/radio/hidden/file/range stay native",
  "raw-button": "raw <button> in feature code; use MUI Button (or RowMenu / MenuItem / TemplateTabs)",
  "raw-tabs": "hand-rolled role=tablist; use kit TemplateTabs or template SegmentTabs",
  "raw-dialog": "hand-rolled dialog/sheet; use MUI Dialog (template custom-dialog pattern) / Drawer (components/app/drawer) / CustomPopover",
  "raw-tooltip": "hand-rolled role=tooltip; use kit Tooltip / InfoHint / ChartTooltipCard",
  "raw-checkbox": "native checkbox/radio/range in feature code; use kit Checkbox / Radio / Switch / Slider",
  "raw-alert": "hand-rolled .alert banner; use kit Alert (severity + variant)",
  "raw-avatar": "hand-rolled .av avatar; use kit Avatar",
  "raw-chip": "hand-rolled chip/label; use Tag (components/ui-primitives) with a tone",
  "fixed-overlay-no-portal": "position:fixed overlay rendered in place; wrap in BodyPortal (or use MUI Dialog / Drawer / Popover)",
  "phone-tap-target": "control smaller than 44px inside a phone media query; use var(--tap-min)",
  "raw-menu": "menu / listbox / popover panel not on the template dropdown; use MUI TextField select / RowMenu (components/app/row-menu) / CustomPopover + MenuList (components/minimal/custom-popover), MUI Menu/Popover, or DropdownPaper (components/app/dropdown-paper) for an in-place list",
  "kit-missing-story": "kit component has no Storybook story; add stories/kit/<Name>.stories.tsx (desktop+390, light+dark, states)",
};

// ── CSS declaration scanning (CSS files AND CSS-in-TSX template strings) ─────
const SIZE_PROPS = "(?:width|height|min-width|min-height|max-width|max-height|padding(?:-(?:top|right|bottom|left|inline|block))?|margin(?:-(?:top|right|bottom|left|inline|block))?|gap|row-gap|column-gap|inset|top|right|bottom|left)";
const DECL = new RegExp(`(?:^|[;{\\s])(${SIZE_PROPS}|border-radius|box-shadow|font-size)\\s*:\\s*([^;{}]+)`, "g");
// 0/1/2px are hairlines/borders/offsets, not layout values.
const PX_VALUE = /(?<![\w.-])(?:[3-9]|\d{2,})(?:\.\d+)?px\b/;
const RADIUS_OK = /^(?:0|inherit|initial|unset|50%|100%|var\(--[\w-]+\)(?:\s+var\(--[\w-]+\))*)$/;

export function cssDeclFindings(line) {
  const out = [];
  for (const m of line.matchAll(DECL)) {
    const prop = m[1];
    const value = m[2].replace(/!important/, "").trim();
    if (prop === "box-shadow") {
      if (!shadowIsTokenised(value)) out.push("raw-shadow");
    } else if (prop === "border-radius") {
      if (!RADIUS_OK.test(value) && /\d/.test(value)) out.push("raw-radius");
    } else if (prop === "font-size") {
      if (/\d(?:px|rem|em)\b/.test(value) && !/var\(--/.test(value)) out.push("raw-font-size");
    } else if (PX_VALUE.test(value.replace(/var\([^)]*\)/g, ""))) {
      out.push("raw-px");
    }
  }
  return out;
}

// Each comma-separated layer must be a token or a 0-offset ring (focus/hairline outline).
function shadowIsTokenised(value) {
  if (/^(?:none|inherit|initial|unset)$/.test(value)) return true;
  const layers = value.split(/,(?![^(]*\))/).map((l) => l.trim());
  return layers.every((l) => /^(?:inset\s+)?var\(--[\w-]+\)$/.test(l) || /^(?:inset\s+)?0 0 0 [\d.]+px\s+\S/.test(l));
}

// React inline styles: style={{ fontSize: 13, borderRadius: 10, boxShadow: "…", padding: 12 }}.
const JSX_STYLE = /\b(fontSize|borderRadius|boxShadow|padding(?:Top|Right|Bottom|Left|Inline|Block)?|margin(?:Top|Right|Bottom|Left|Inline|Block)?|gap|height|minHeight)\s*:\s*("[^"]*"|'[^']*'|`[^`]*`|-?\d+(?:\.\d+)?)/g;
// Tailwind arbitrary values: text-[13px], rounded-[10px], p-[12px], shadow-[…].
const TW_ARBITRARY = /(?:^|[\s"'`])(?:[a-z]+:)*(text|rounded(?:-[trbl]{1,2})?|shadow|p[xytrbl]?|m[xytrbl]?|gap(?:-[xy])?|h|min-h)-\[([^\]]+)\]/g;

export function jsxStyleFindings(code) {
  const out = [];
  for (const m of code.matchAll(JSX_STYLE)) {
    const prop = m[1];
    const raw = m[2].replace(/^["'`]|["'`]$/g, "");
    if (raw.includes("var(--") || raw.includes("${")) continue;
    const numeric = /^-?\d+(?:\.\d+)?$/.test(raw);
    if (prop === "boxShadow") { if (raw !== "none") out.push("raw-shadow"); continue; }
    // J1B P2-1 (FIXJ7): a NUMBER in sx is the theme's scale, not a raw px literal: borderRadius
    // multiplies theme.shape.borderRadius and padding / margin / gap multiply theme.spacing. Those are
    // exactly the theme values the css-var-token ratchet moves the var(--r-*) / var(--sp-*) reads onto.
    if (prop === "borderRadius") { if (!numeric && /\d+px/.test(raw)) out.push("raw-radius"); continue; }
    if (numeric && /^(?:padding|margin|gap)/.test(prop)) continue;
    if (prop === "fontSize") { if (numeric || /\d(?:px|rem|em)/.test(raw)) out.push("raw-font-size"); continue; }
    if ((numeric && Math.abs(Number(raw)) >= 3) || PX_VALUE.test(raw)) out.push("raw-px");
  }
  for (const m of code.matchAll(TW_ARBITRARY)) {
    const kind = m[1];
    const value = m[2];
    if (value.startsWith("var(--")) continue;
    if (kind === "text") { if (/\d(?:px|rem)/.test(value)) out.push("raw-font-size"); }
    else if (kind.startsWith("rounded")) out.push("raw-radius");
    else if (kind === "shadow") out.push("raw-shadow");
    else if (PX_VALUE.test(value)) out.push("raw-px");
  }
  return out;
}

// ── Raw elements where a kit component exists ────────────────────────────────
const RAW_ELEMENTS = [
  ["raw-table", /<table[\s>]/],
  ["raw-button", /<button[\s>]/],
  ["raw-input", /<input\b(?![^>]*type=["'](?:checkbox|radio|hidden|file|range|color)["'])/],
  ["raw-tabs", /role=["']tablist["']/],
  ["raw-dialog", /<dialog[\s>]|role=["'](?:alert)?dialog["']|aria-modal=/],
  ["raw-tooltip", /role=["']tooltip["']/],
  ["raw-chip", /className=["'`][^"'`]*(?<![\w-])(?:chip|status-chip|label-chip)(?![\w-])/],
  ["raw-checkbox", /<input\b[^>]*type=["'](?:checkbox|radio|range)["']/],
  ["raw-alert", /className=["'`][^"'`]*(?<![\w-])alert(?![\w-])/],
  ["raw-avatar", /className=["'`][^"'`]*(?<![\w-])av(?![\w-])/],
];

export function isPrimitiveOwner(rel) {
  return PRIMITIVE_OWNERS.some((re) => re.test(rel));
}

export function elementFindings(code, rel) {
  if (isPrimitiveOwner(rel)) return [];
  return RAW_ELEMENTS.filter(([, re]) => re.test(code)).map(([check]) => check);
}

// ── Dropdown panels: every menu, listbox and popover is the template dropdown ──
// A panel is an opening tag with role="menu"/"listbox" or a class token that names a menu or a
// popover (`parkmenu`, `avmenu`, `menu`, `x-menu`, `x-pop`, `x-popover`, `pop`). It passes when the
// tag IS a template/MUI dropdown component (CustomPopover, MUI Menu/MenuList/Popover, DropdownPaper).
const MENU_ROLE = /role=["'](?:menu|listbox)["']/g;
const MENU_CLASS_ATTR = /className=(?:"([^"]*)"|\{`([^`]*)`\})/g;
const MENU_TOKEN = /^(?:menu|[\w-]+-menu|(?:park|av|user|row|sort|ctx)menu|[\w-]+-pop(?:over)?|pop)$/;
const ON_TEMPLATE_DROPDOWN = /^<(?:CustomPopover|Popover|Menu|MenuList|DropdownPaper)\b/;
const TEMPLATE_DROPDOWN_OPEN = /<(?:CustomPopover|Popover|Menu|DropdownPaper)\b/g;
const TEMPLATE_DROPDOWN_CLOSE = /<\/(?:CustomPopover|Popover|Menu|DropdownPaper)>/;

/** Opening tag around `index`: from the last `<Tag` before it to the next `<` after it. */
function openingTagAt(text, index) {
  let start = index;
  while (start > 0 && !(text[start] === "<" && /[A-Za-z]/.test(text[start + 1] ?? ""))) start -= 1;
  const next = text.indexOf("<", index);
  return { start, tag: text.slice(start, next === -1 ? text.length : next) };
}

export function menuSurfaceFindings(text, rel) {
  if (isPrimitiveOwner(rel) || rel.endsWith(".css")) return [];
  const seen = new Set();
  const out = [];
  const consider = (index) => {
    const { start, tag } = openingTagAt(text, index);
    if (seen.has(start)) return;
    seen.add(start);
    if (ON_TEMPLATE_DROPDOWN.test(tag)) return;
    // Tooltips are raw-tooltip's business, not a dropdown.
    if (/role=["']tooltip["']/.test(tag)) return;
    // The list INSIDE a template dropdown (a listbox under the search box of a picker) is already
    // on it: the nearest enclosing dropdown opened within the last few lines and was not closed.
    const before = text.slice(Math.max(0, start - 2500), start);
    const opened = Math.max(-1, ...[...before.matchAll(TEMPLATE_DROPDOWN_OPEN)].map((m) => m.index));
    if (opened >= 0 && !TEMPLATE_DROPDOWN_CLOSE.test(before.slice(opened))) return;
    const line = text.slice(0, start).split("\n").length;
    out.push({ line, snippet: text.split("\n")[line - 1] ?? "" });
  };
  for (const m of text.matchAll(MENU_ROLE)) consider(m.index);
  for (const m of text.matchAll(MENU_CLASS_ATTR)) {
    const classes = (m[1] ?? m[2] ?? "").replace(/\$\{[^}]*\}/g, " ").split(/\s+/).filter(Boolean);
    if (classes.some((c) => MENU_TOKEN.test(c))) consider(m.index);
  }
  return out;
}

// position:fixed in a component that neither portals nor uses a kit overlay.
const FIXED = /position\s*:\s*["']?fixed|(?:className=["'`][^"'`]*\bfixed\b)/;
const PORTALLED = /\bBodyPortal\b|\bcreatePortal\b|<(?:Dialog|Drawer|MinimalDrawer|Popover|CustomPopover)\b/;
export function fixedOverlayFindings(text, rel) {
  if (isPrimitiveOwner(rel) || rel.endsWith(".css")) return [];
  const lines = text.split("\n");
  if (PORTALLED.test(text)) return [];
  return lines.flatMap((line, i) => (FIXED.test(line) && !/^\s*(?:\/\/|\*)/.test(line) ? [{ line: i + 1, snippet: line }] : []));
}

// ── Phone tap targets: inside @media (max-width: <=880px), a control rule whose
//    height/min-height/width is under 44px. Statically detectable only in CSS. ──
const CONTROL_SELECTOR = /(?:button|\.btn\b|\.iconbtn|\[role=["']?(?:button|tab)|\.tab\b|\.chip\b|\.pm-item|\.leaf\b|\.nav\b|input|select)/;
export function tapTargetFindings(cssText) {
  const out = [];
  const lines = cssText.split("\n");
  let depth = 0;
  let phoneDepth = -1;
  lines.forEach((line, i) => {
    const media = /@media[^{]*max-width\s*:\s*(\d+)px/.exec(line);
    if (media && Number(media[1]) <= 880 && phoneDepth < 0) phoneDepth = depth;
    if (phoneDepth >= 0) {
      for (const rule of line.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
        if (!CONTROL_SELECTOR.test(rule[1]) || /::?(?:before|after)|\bsvg\b|\.ic\b|icon/.test(rule[1])) continue;
        for (const d of rule[2].matchAll(/(?:^|;)\s*(min-height|height)\s*:\s*(\d+(?:\.\d+)?)px/g)) {
          if (Number(d[2]) < 44 && Number(d[2]) > 16) out.push({ line: i + 1, snippet: rule[0] });
        }
      }
    }
    depth += (line.match(/\{/g) ?? []).length - (line.match(/\}/g) ?? []).length;
    if (phoneDepth >= 0 && depth <= phoneDepth) phoneDepth = -1;
  });
  return out;
}

// ── Kit components need a story ──────────────────────────────────────────────
const KIT_NON_COMPONENT = new Set(["index.ts", "theme.ts", "tone.ts", "use-in-view-once.ts"]);
export function kitStoryFindings(root) {
  const kitDir = join(root, "components", "kit");
  const storiesDir = join(root, "stories");
  if (!existsSync(kitDir)) return [];
  const storyText = walkFiles(storiesDir)
    .filter((f) => /\.stories\.tsx?$/.test(f))
    .map((f) => readFileSync(f, "utf8"))
    .join("\n");
  const out = [];
  for (const name of readdirSync(kitDir).sort()) {
    if (!/\.tsx$/.test(name) || KIT_NON_COMPONENT.has(name)) continue;
    const text = readFileSync(join(kitDir, name), "utf8");
    const exported = [...text.matchAll(/export\s+(?:default\s+)?function\s+([A-Z]\w*)/g)].map((m) => m[1]);
    if (exported.length === 0) continue;
    const stem = basename(name, ".tsx");
    const referenced = storyText.includes(`kit/${stem}"`) || exported.some((e) => new RegExp(`<${e}\\b|\\bcomponent:\\s*${e}\\b`).test(storyText));
    if (!referenced) out.push({ file: `components/kit/${name}`, snippet: `no story renders ${exported.join(", ")}` });
  }
  return out;
}


/**
 * Ratchet: count per check|file; fail when a file exceeds its explicit allowance (a file with no
 * allowance has 0). --update-baseline may only LOWER an allowance; seeding new allowances needs
 * DESIGN_RATCHET_SEED=1 and a reason per entry (the one-off legacy inventory), never to land a change.
 */
export function evaluateRatchet(ratchetFindings, allowances, { seed = process.env.DESIGN_RATCHET_SEED === "1" } = {}) {
  const groups = new Map();
  for (const f of ratchetFindings) {
    const key = `${f.check}|${f.file}`;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(f);
  }
  const over = [];
  const shrinkable = [];
  const nextBaseline = {};
  for (const [key, list] of groups) {
    const entry = allowances[key];
    const allowed = entry?.allowed ?? 0;
    if (list.length > allowed) over.push({ key, count: list.length, allowed, sample: list[list.length - 1] });
    if (entry && list.length < allowed) shrinkable.push({ key, count: list.length, allowed });
    if (entry) nextBaseline[key] = { allowed: Math.min(allowed, list.length), reason: entry.reason };
    else if (seed) nextBaseline[key] = { allowed: list.length, reason: legacyReason(list[0].check) };
  }
  // An allowance whose file has NO finding left is slack too (FIXJ-CI: 241 such entries had piled
  // up unseen, because only files with findings were compared). It is dropped from the baseline.
  for (const [key, entry] of Object.entries(allowances)) {
    if (!groups.has(key) && (entry?.allowed ?? 0) > 0) shrinkable.push({ key, count: 0, allowed: entry.allowed });
  }
  return { over, shrinkable, nextBaseline: Object.fromEntries(Object.entries(nextBaseline).sort(([a], [b]) => a.localeCompare(b))) };
}

function legacyReason(check) {
  if (SHRINK_RATCHET_CHECKS[check]) return `baselined 2026-09-30 at the current count (FIXJ-CI, J1 CI gaps); shrink-only: ${SHRINK_RATCHET_CHECKS[check]}`;
  return `pre-kit legacy (inventoried 2026-09-25 when the Minimal kit guard landed); migrate: ${RATCHET_CHECKS[check]}`;
}

export function isTokenOwner(rel) {
  return TOKEN_OWNERS.has(rel);
}

function walkFiles(dir) {
  if (!existsSync(dir)) return [];
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.name === "node_modules" || e.name.startsWith(".") ? [] : e.isDirectory() ? walkFiles(join(dir, e.name)) : [join(dir, e.name)],
  );
}

export function toRel(root, abs) {
  return relative(root, abs).split(sep).join("/");
}
