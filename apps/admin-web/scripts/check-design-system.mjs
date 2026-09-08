#!/usr/bin/env node
// check-design-system.mjs — static gate that keeps admin-web on the Mesha design system.
//
// The design system is a small, closed vocabulary: colours come from the token block at the top of
// app/mesha-theme.css, radii are 2px on surfaces and a full pill on controls, type comes from
// var(--f)/var(--f-serif)/var(--fm). Every rule below is a defect that ALREADY shipped on this
// branch, so the guard exists to keep the fix from being undone by the next feature:
//
//   raw-colour        a hex/rgb()/hsl() literal outside the sanctioned token blocks — a colour that
//                     cannot follow the theme toggle or the print restatement.
//   tailwind-palette  a default-Tailwind palette utility (bg-slate-*, text-gray-*, bg-white, …).
//                     Those greys are not the Mesha ivory/near-black and never change with theme.
//   hsl-triplet       var(--card)/var(--border)/… used OUTSIDE hsl(). Those shadcn tokens are BARE
//                     HSL TRIPLETS in app/globals.css; used raw the declaration is invalid CSS and
//                     is silently dropped. This shipped a login screen with transparent inputs.
//   off-scale-radius  a numeric radius that is neither the 2px surface step nor the pill.
//   undefined-var     var(--x) where --x is never declared anywhere in admin-web. This shipped
//                     --media-matte (letterbox rendered transparent), --panel-strong and --panel2.
//   hardcoded-font    font-family naming a literal face instead of the three type tokens.
//
// Modes:
//   (default)     scan app/**, components/**, features/**
//   --self-test   run the built-in violating/clean fixtures (incl. evasions) and exit
//
// Run:  npm --prefix apps/admin-web run check:design-system
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(import.meta.dirname, "..");
const SCAN_PATHS = ["app", "components", "features"];
const SCAN_EXT = /\.(tsx|ts|css)$/;

// Files exempt from the colour/font rules entirely, each with the reason it cannot use tokens.
const SKIP_PATH_PARTS = [
  // The root error boundary REPLACES the root layout, so neither mesha-theme.css nor the
  // @font-face blocks load there: there is nothing on the page to resolve a var() against, so the
  // design system's values are deliberately restated as literals. Documented in the file itself.
  "app/global-error.css",
];

// Per-line exceptions. Every entry MUST carry a reason; an entry without one fails the guard.
const ALLOWLIST = [
  {
    rule: "raw-colour",
    file: "app/mesha-theme.css",
    contains: "-webkit-mask-image:radial-gradient",
    reason:
      "#000 here is a MASK stop, not a colour: -webkit-mask-image/mask-image use the alpha channel of the gradient to fade the login grid out. Any theme token would be wrong (the mask must stay fully opaque) and a var() would make the fade follow the theme, which it must not.",
  },
  {
    rule: "raw-colour",
    file: "features/ceo-ai/ceo-ai-styles.tsx",
    contains: "<path fill=",
    reason:
      "Twemoji goat artwork. These are the illustration's own palette inside a fixed SVG drawing (CC-BY Twemoji), not UI chrome — retinting the artwork to design tokens would destroy the drawing.",
  },
  {
    rule: "raw-colour",
    file: "features/ceo-ai/ceo-ai-styles.tsx",
    contains: "<circle fill=",
    reason: "Same Twemoji goat artwork as above — the eye of the drawing, not a themable surface.",
  },
];

// ── Rule vocabulary ───────────────────────────────────────────────────────────────────────────
const TOKEN_HINT = "var(--bg|--panel|--panel-2|--ink|--muted|--line|--brand|--value|--ok|--warn|--danger|…) from app/mesha-theme.css";
const TAILWIND_PALETTE = [
  "slate", "gray", "zinc", "neutral", "stone", "red", "orange", "amber", "yellow", "lime", "green",
  "emerald", "teal", "cyan", "sky", "blue", "indigo", "violet", "purple", "fuchsia", "pink", "rose",
];
const TAILWIND_PREFIX = "bg|text|border|ring|fill|stroke|divide|outline|decoration|placeholder|accent|caret|shadow|from|via|to";
// bg-slate-500, and the concatenation evasion `"bg-slate-" + shade` / `bg-slate` alone.
const TAILWIND_UTILITY = new RegExp(`\\b(?:${TAILWIND_PREFIX})-(?:${TAILWIND_PALETTE.join("|")})(?![a-z])`, "g");
// A bare shade fragment, so `"bg-" + "slate-500"` and `` `${prefix}slate-500` `` are caught too.
const TAILWIND_SHADE = new RegExp(`\\b(?:${TAILWIND_PALETTE.join("|")})-(?:50|100|200|300|400|500|600|700|800|900|950)\\b`, "g");
const TAILWIND_MONO = new RegExp(`\\b(?:${TAILWIND_PREFIX})-(?:white|black)\\b`, "g");
// Candidate shadcn tokens. Whether each is actually a trap is decided from the declarations found
// on disk (see collectTokenFacts): a name is a trap only while EVERY declaration of it is a bare
// HSL triplet. --ring, for example, is re-declared as a real rgba() in mesha-theme.css.
const HSL_TRIPLET_CANDIDATES = [
  "card", "border", "background", "foreground", "input", "popover", "primary", "secondary",
  "accent", "destructive", "muted-foreground", "ring",
];
// The system's radius scale: 2px on surfaces, a full pill on controls. Nothing in between.
// The design system's radius scale is sm 2px / md 4px / lg 1rem / pill (see
// tokens/effects.css in the design system: --radius-sm/-md/-lg/-pill). An earlier
// revision of this guard enforced "2px or pill, nothing between", which was an
// invention -- the system does define a 4px and a 1rem step.
const RADIUS_OK = new Set(["var(--r)", "var(--r2)", "var(--r-md)", "var(--r-lg)", "var(--r-pill)",
  "0", "0px", "2px", "4px", "1rem", "16px", "50%", "999px", "9999px", "inherit", "initial", "unset"]);
const FONT_TOKENS = /var\(\s*--(?:f|f-serif|fm|font-sans|font-serif|font-mono)\s*\)/;
const HEX = /#[0-9a-fA-F]{3,8}\b/g;
const COLOUR_FN = /\b(?:rgba?|hsla?)\(([^()]*(?:\([^()]*\)[^()]*)*)\)/g;
const VAR_USE = /var\(\s*(--[A-Za-z0-9_-]+)/g;
const DECL_CSS = /(--[A-Za-z0-9_-]+)\s*:/g;
const DECL_JS = /["'](--[A-Za-z0-9_-]+)["']\s*:/g;

// ── Source normalisation ──────────────────────────────────────────────────────────────────────
// Comments are not shipped CSS/markup, so a hex inside one is not a defect (globals.css annotates
// every HSL triplet with the hex it came from). Block comments are blanked line-by-line so line
// numbers stay exact.
function stripComments(source, isCss) {
  let out = source.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
  if (!isCss) out = out.split("\n").map((line) => line.replace(/(^|[^:])\/\/.*$/, "$1")).join("\n");
  return out;
}

// Regions of a stylesheet where colour literals ARE the design system: the :root token blocks and
// the @media print restatement of them. Returns a Set of 1-indexed sanctioned line numbers.
function sanctionedLines(source) {
  const lines = source.split("\n");
  const sanctioned = new Set();
  let depth = 0;
  let regionDepth = null;
  lines.forEach((line, i) => {
    const opensRegion =
      regionDepth === null &&
      (/^\s*@media\s+print\b/.test(line) || /^\s*:root(?:\.[A-Za-z0-9_-]+)?\s*(?:,\s*:root(?:\.[A-Za-z0-9_-]+)?\s*)*\{/.test(line) || /^\s*@font-face\b/.test(line));
    if (opensRegion) regionDepth = depth;
    if (regionDepth !== null) sanctioned.add(i + 1);
    for (const ch of line) {
      if (ch === "{") depth += 1;
      else if (ch === "}") depth -= 1;
    }
    if (regionDepth !== null && depth <= regionDepth) regionDepth = null;
  });
  return sanctioned;
}

function isAllowed(rel, text, rule) {
  return ALLOWLIST.some((entry) => entry.rule === rule && rel.endsWith(entry.file) && text.includes(entry.contains));
}

// ── The checker ───────────────────────────────────────────────────────────────────────────────
// facts: { declared: Set<string>, traps: Set<string> }
export function findingsForSource(rel, rawSource, facts) {
  const isCss = rel.endsWith(".css");
  const isStyleCarrier = isCss || /\.tsx?$/.test(rel);
  if (!isStyleCarrier) return [];
  const skipped = SKIP_PATH_PARTS.some((part) => rel.includes(part));
  const source = stripComments(rawSource, isCss);
  const sanctioned = isCss ? sanctionedLines(source) : new Set();
  const findings = [];
  const add = (line, rule, message) => findings.push({ line, rule, message });

  source.split("\n").forEach((text, i) => {
    const line = i + 1;
    if (!text.trim()) return;
    const inTokenBlock = sanctioned.has(line);

    // 1) raw colour literals
    if (!skipped && !inTokenBlock && !isAllowed(rel, text, "raw-colour")) {
      for (const m of text.matchAll(HEX)) {
        add(line, "raw-colour", `raw colour literal ${m[0]}  ->  use a Mesha token: ${TOKEN_HINT}`);
      }
      for (const m of text.matchAll(COLOUR_FN)) {
        // hsl(var(--card) / .4) and rgb(var(--x)) are the sanctioned token wrappers, not literals.
        if (m[1].includes("var(")) continue;
        add(line, "raw-colour", `raw colour literal ${m[0]}  ->  use a Mesha token: ${TOKEN_HINT}`);
      }
    }

    // 2) default Tailwind palette utilities
    for (const re of [TAILWIND_UTILITY, TAILWIND_SHADE, TAILWIND_MONO]) {
      re.lastIndex = 0;
      for (const m of text.matchAll(re)) {
        if (isAllowed(rel, text, "tailwind-palette")) continue;
        add(line, "tailwind-palette", `default Tailwind palette class "${m[0]}"  ->  use a Mesha class/token (var(--panel), var(--ink), var(--line)); the default palette ignores the theme toggle`);
      }
    }

    // 3) the shadcn HSL-triplet trap
    for (const m of text.matchAll(/var\(\s*(--[A-Za-z0-9_-]+)\s*\)/g)) {
      const name = m[1].slice(2);
      if (!facts.traps.has(name)) continue;
      const before = text.slice(0, m.index);
      if (/hsla?\(\s*$/.test(before)) continue;
      if (isAllowed(rel, text, "hsl-triplet")) continue;
      add(line, "hsl-triplet", `var(--${name}) used outside hsl() — --${name} is a BARE HSL TRIPLET in app/globals.css, so this declaration is invalid CSS and is dropped silently  ->  write hsl(var(--${name})) or, better, use the mesha-theme token (${TOKEN_HINT})`);
    }

    // 4) off-scale border radii
    const radiusValues = [];
    for (const m of text.matchAll(/border-radius\s*:\s*([^;}\n]+)/g)) radiusValues.push(m[1]);
    for (const m of text.matchAll(/borderRadius\s*:\s*([^,}\n]+)/g)) radiusValues.push(m[1]);
    for (const raw of radiusValues) {
      const value = raw.trim().replace(/^["'`]|["'`]$/g, "").trim();
      if (!value || value.startsWith("{") || /^[A-Za-z_$][\w$.]*$/.test(value)) continue; // a variable, not a literal
      const parts = value.split(/[\s/]+/).filter(Boolean);
      const bad = parts.filter((p) => !RADIUS_OK.has(p) && !/^var\(--r/.test(p));
      if (!bad.length) continue;
      if (isAllowed(rel, text, "off-scale-radius")) continue;
      add(line, "off-scale-radius", `border radius "${value}" is off the system scale  ->  use var(--r) 2px, var(--r-md) 4px, var(--r-lg) 1rem, or var(--r-pill) (controls), 50% (circles) or 0`);
    }

    // 5) undefined custom properties
    for (const m of text.matchAll(VAR_USE)) {
      if (facts.declared.has(m[1])) continue;
      if (isAllowed(rel, text, "undefined-var")) continue;
      add(line, "undefined-var", `var(${m[1]}) is never declared in admin-web, so the property resolves to nothing and the declaration is dropped  ->  declare ${m[1]} in the app/mesha-theme.css token block (and its @media print restatement) or use an existing token: ${TOKEN_HINT}`);
    }

    // 6) hardcoded font stacks
    if (!skipped && !inTokenBlock) {
      for (const m of text.matchAll(/font(?:-f|F)amily\s*:\s*([^;}\n]+)/g)) {
        const value = m[1].trim();
        if (FONT_TOKENS.test(value)) continue;
        if (/^(?:inherit|initial|unset|revert)\b/.test(value)) continue;
        if (/^[A-Za-z_$][\w$.]*[,}]?$/.test(value)) continue; // a variable, not a literal stack
        if (isAllowed(rel, text, "hardcoded-font")) continue;
        add(line, "hardcoded-font", `hardcoded font stack ${value.replace(/["'`]/g, '"').slice(0, 60)}  ->  use var(--f) (Instrument Sans), var(--f-serif) (Instrument Serif) or var(--fm) (Spline Sans Mono)`);
      }
    }
  });

  // A `font:` shorthand carrying a literal face is the same defect wearing a different hat.
  source.split("\n").forEach((text, i) => {
    if (skipped) return;
    if (isCss && sanctionedLines(source).has(i + 1)) return;
    for (const m of text.matchAll(/(?:^|[;{])\s*font\s*:\s*([^;}\n]+)/g)) {
      const value = m[1];
      if (FONT_TOKENS.test(value) || /\binherit\b/.test(value)) continue;
      if (!/["']/.test(value)) continue;
      if (isAllowed(rel, text, "hardcoded-font")) continue;
      findings.push({ line: i + 1, rule: "hardcoded-font", message: `font shorthand names a literal face (${value.trim().slice(0, 60)})  ->  use var(--f) / var(--f-serif) / var(--fm)` });
    }
  });

  return findings;
}

// ── Repo facts ────────────────────────────────────────────────────────────────────────────────
function walk(path, out) {
  if (!existsSync(path)) return;
  const st = statSync(path);
  if (st.isDirectory()) {
    if (/(^|\/)(node_modules|\.next|build)$/.test(path)) return;
    for (const entry of readdirSync(path)) walk(join(path, entry), out);
    return;
  }
  if (SCAN_EXT.test(path)) out.push(path);
}

// Declared custom properties, plus which shadcn candidates are genuinely bare HSL triplets.
function collectTokenFacts(files) {
  const declared = new Set();
  const declaredValues = new Map();
  for (const file of files) {
    const isCss = file.endsWith(".css");
    const source = stripComments(readFileSync(file, "utf8"), isCss);
    for (const m of source.matchAll(/(--[A-Za-z0-9_-]+)\s*:\s*([^;}\n]*)/g)) {
      declared.add(m[1]);
      if (!declaredValues.has(m[1])) declaredValues.set(m[1], []);
      declaredValues.get(m[1]).push(m[2].trim());
    }
    for (const m of source.matchAll(DECL_JS)) declared.add(m[1]);
    for (const m of source.matchAll(DECL_CSS)) declared.add(m[1]);
    for (const m of source.matchAll(/setProperty\(\s*["'](--[A-Za-z0-9_-]+)["']/g)) declared.add(m[1]);
  }
  const traps = new Set();
  for (const name of HSL_TRIPLET_CANDIDATES) {
    const values = declaredValues.get(`--${name}`) ?? [];
    if (!values.length) continue;
    // A bare triplet looks like `60 8% 3%` — no function, no hex, no var().
    const allBare = values.every((v) => /^-?[\d.]+\s+[\d.]+%\s+[\d.]+%$/.test(v));
    if (allBare) traps.add(name);
  }
  return { declared, traps };
}

// ── Self-test ─────────────────────────────────────────────────────────────────────────────────
function selfTest() {
  const facts = {
    declared: new Set(["--r", "--r-pill", "--brand", "--ink", "--panel", "--f", "--fm", "--card", "--border", "--ring"]),
    traps: new Set(["card", "border", "background", "input"]),
  };
  const bad = [
    // 1) raw colour, including the uppercase-hex and template-literal evasions
    ["features/x/a.tsx", 'const s = { color: "#AABBCC" };', "raw-colour"],
    ["features/x/a.tsx", "const css = `.z{background:#ff0000}`;", "raw-colour"],
    ["features/x/a.tsx", 'const s = { background: "rgba(12, 34, 56, .4)" };', "raw-colour"],
    ["app/z.css", ".z{color:hsl(95 55% 53%)}", "raw-colour"],
    // 2) Tailwind palette, plus the string-concatenation evasion
    ["features/x/a.tsx", '<div className="bg-slate-800 text-gray-400" />', "tailwind-palette"],
    ["features/x/a.tsx", 'const cls = "bg-" + "slate-500";', "tailwind-palette"],
    ["features/x/a.tsx", '<div className="bg-white text-black" />', "tailwind-palette"],
    ["features/x/a.tsx", "const cls = `border-zinc-${shade}`;", "tailwind-palette"],
    // 3) the HSL-triplet trap, including the spaced-var evasion
    ["features/x/a.tsx", 'style={{ background: "var(--card)" }}', "hsl-triplet"],
    ["app/z.css", ".z{border:1px solid var( --border )}", "hsl-triplet"],
    // 4) off-scale radii
    ["features/x/a.tsx", "style={{ borderRadius: 8 }}", "off-scale-radius"],
    ["app/z.css", ".z{border-radius: 12px}", "off-scale-radius"],
    ["app/z.css", ".z{border-radius:2px 6px 2px 2px}", "off-scale-radius"],
    // 5) undefined custom property
    ["app/z.css", ".z{background:var(--media-matte)}", "undefined-var"],
    ["features/x/a.tsx", 'style={{ color: "var(--panel-strong)" }}', "undefined-var"],
    // 6) hardcoded font stacks
    ["app/z.css", '.z{font-family:"Inter",sans-serif}', "hardcoded-font"],
    ["features/x/a.tsx", 'const s = { fontFamily: "Helvetica, Arial, sans-serif" };', "hardcoded-font"],
    ["app/z.css", '.z{font:14px/1.45 "Inter",sans-serif}', "hardcoded-font"],
  ];
  for (const [rel, src, rule] of bad) {
    const found = findingsForSource(rel, src, facts);
    if (!found.some((f) => f.rule === rule)) {
      throw new Error(`self-test: '${rule}' NOT flagged for ${rel}: ${src.slice(0, 70)}`);
    }
  }

  const good = [
    ["features/x/a.tsx", 'style={{ background: "var(--panel)", color: "var(--ink)", borderRadius: "var(--r)" }}'],
    ["features/x/a.tsx", '<div className="bg-[var(--panel)] text-[var(--ink)]" />'],
    ["features/x/a.tsx", "// a comment mentioning #ff0000 and bg-slate-800 is not shipped CSS"],
    ["app/z.css", ".z{background:hsl(var(--card));border-color:hsl(var(--border) / .4)}"],
    ["app/z.css", ".z{border-radius:var(--r-pill);background:var(--brand)}"],
    ["app/z.css", ".z{border-radius:2px}"],
    ["app/z.css", ".z{border-radius:50%}"],
    ["app/z.css", ".z{font-family:var(--fm)}"],
    ["app/z.css", ".z{font:14px/1.45 var(--f)}"],
    ["app/z.css", ".z{background:color-mix(in srgb,var(--brand) 7%,transparent)}"],
    ["app/z.css", "/* #ff0000 and bg-slate-800 inside a comment */\n.z{color:var(--ink)}"],
    // the sanctioned token blocks and the print restatement
    ["app/mesha-theme.css", ":root{\n  --bg:#070706; --ink:#ece8dd; --muted:rgba(236,232,221,.72);\n}"],
    ["app/mesha-theme.css", "@media print{\n  :root,:root.light{ --bg:#fff; --ink:#141410; }\n  body{background:#fff;color:#141410}\n}"],
    ["app/mesha-theme.css", '@font-face{font-family:"Instrument Sans";src:url("/fonts/x.woff2") format("woff2")}'],
    // the deliberately self-contained error-boundary stylesheet
    ["app/global-error.css", 'body{background:#070706;color:#ece8dd;font-family:"Instrument Sans",system-ui,sans-serif}'],
    // non-colour words that merely look like violations
    ["features/x/a.tsx", 'const id = "background-refresh";'],
    ["features/x/a.tsx", 'const label = "greenhouse";'],
  ];
  for (const [rel, src] of good) {
    const found = findingsForSource(rel, src, facts);
    if (found.length) {
      throw new Error(`self-test: false positive on ${rel}: ${src.slice(0, 70)} -> ${found.map((f) => `${f.rule}(${f.message.slice(0, 40)})`).join(", ")}`);
    }
  }

  // The trap set must be derived, not assumed: a candidate re-declared as a real colour is safe.
  const derived = collectTokenFactsFromSources([
    ["app/globals.css", ":root{--card: 60 10% 4%; --ring: 95 55% 53%;}"],
    ["app/mesha-theme.css", ":root{--ring:rgba(122,201,67,.32)}"],
  ]);
  if (!derived.traps.has("card")) throw new Error("self-test: --card should be a bare-triplet trap");
  if (derived.traps.has("ring")) throw new Error("self-test: --ring is re-declared as rgba() and must NOT be treated as a trap");

  // Every allowlist entry must carry a reason, or the escape hatch is not an escape hatch.
  for (const entry of ALLOWLIST) {
    if (!entry.reason || entry.reason.trim().length < 20) {
      throw new Error(`self-test: allowlist entry ${entry.rule}/${entry.file} needs a written reason`);
    }
    if (!entry.rule || !entry.file || !entry.contains) {
      throw new Error("self-test: allowlist entry needs rule, file and contains");
    }
  }

  console.log(`design-system self-test: ok (${bad.length} violating fixtures flagged, ${good.length} clean fixtures passed)`);
}

// Test seam so the self-test can exercise fact collection without touching the filesystem.
function collectTokenFactsFromSources(pairs) {
  const declaredValues = new Map();
  const declared = new Set();
  for (const [file, source] of pairs) {
    const stripped = stripComments(source, file.endsWith(".css"));
    for (const m of stripped.matchAll(/(--[A-Za-z0-9_-]+)\s*:\s*([^;}\n]*)/g)) {
      declared.add(m[1]);
      if (!declaredValues.has(m[1])) declaredValues.set(m[1], []);
      declaredValues.get(m[1]).push(m[2].trim());
    }
  }
  const traps = new Set();
  for (const name of HSL_TRIPLET_CANDIDATES) {
    const values = declaredValues.get(`--${name}`) ?? [];
    if (values.length && values.every((v) => /^-?[\d.]+\s+[\d.]+%\s+[\d.]+%$/.test(v))) traps.add(name);
  }
  return { declared, traps };
}

function runScan() {
  const files = [];
  for (const scanPath of SCAN_PATHS) walk(join(ROOT, scanPath), files);
  const facts = collectTokenFacts(files);

  const findings = [];
  for (const file of files) {
    const rel = relative(ROOT, file).replaceAll("\\", "/");
    for (const f of findingsForSource(rel, readFileSync(file, "utf8"), facts)) findings.push({ ...f, rel });
  }

  if (findings.length > 0) {
    console.error("\u2716 design-system check FAILED \u2014 admin-web must stay on the Mesha design system:");
    console.error("  tokens: apps/admin-web/app/mesha-theme.css (:root block + its @media print restatement)\n");
    for (const f of findings.sort((a, b) => a.rel.localeCompare(b.rel) || a.line - b.line)) {
      console.error(`  ${f.rel}:${f.line}  [${f.rule}] ${f.message}`);
    }
    console.error(
      `\n${findings.length} issue(s). Replace each literal with the design system token named above; if a line genuinely cannot use a token (illustration artwork, a mask stop, a stylesheet that loads without the theme), add it to ALLOWLIST in scripts/check-design-system.mjs WITH a written reason.`,
    );
    process.exit(1);
  }

  console.log(`\u2713 design-system check passed \u2014 ${files.length} file(s) scanned; no off-system colours, palettes, radii, fonts or dangling tokens.`);
}

// Imported as a module (test seam): expose the checker without running the repo scan.
const RUN_AS_SCRIPT = process.argv[1] ? resolve(process.argv[1]) === fileURLToPath(import.meta.url) : false;

if (RUN_AS_SCRIPT) {
  if (process.argv.includes("--self-test")) {
    try {
      selfTest();
    } catch (error) {
      console.error(`\u2716 design-system self-test FAILED: ${error.message}`);
      process.exit(1);
    }
    process.exit(0);
  }
  runScan();
}
