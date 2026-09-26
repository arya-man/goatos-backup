#!/usr/bin/env node
// check-design-system.mjs — STATIC design-system guard for admin-web.
//
// Encodes the redesign contract (.agents/skills/design-system/SKILL.md, docs/design/README.md)
// as machine checks over app/, components/ and features/. No app, no browser, no network:
// it runs on every commit in tools/ci/run-local-ci.sh (`npm run design:guard`).
//
// Two tiers:
//   P0 (never waivable): another product's palette in ANY file, a diff in the two theme
//      token files against origin/main, a Google Fonts <link>.
//   Waivable: native <select>/<input type=date>, window.confirm/alert, hex colours in TSX,
//      Tailwind palette classes, "F2" literals, fixed px widths >= 480, description prose
//      under a heading, a chart without a tooltip / with animation disabled, an admin
//      page.tsx without a sibling loading.tsx, and (soft, once PageHeader is resolvable) a
//      page that does not render inside a `.kit-page` column with a PageHeader.
//
// Known debt is listed by key in scripts/check-design-system-waivers/design-system-waivers.json
// so the guard is GREEN on today's code and RED the moment a new instance appears.
//   --update-baseline   rewrite the waiver file from the current findings (deliberate only)
//   --report            print every finding including waived ones
//   --json <path>       also write the report there
//   --self-test         prove each check detects a synthetic violation, then exit
//
// Finding key = check|file|normalized-line (no line numbers, so a waiver survives edits
// elsewhere in the file but dies when the offending line itself changes).

import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { muiPaletteLockFindings, templateNeutralFindings } from "./lib/mui-palette-lock.mjs";
import { BRAND_LOCK, TOKEN_FILE, isDriftRemoval, retiredNeutralFindings, primaryStateFindings, themeLockFindings } from "./lib/design-palette.mjs";
import {
  RATCHET_CHECKS,
  cssDeclFindings,
  evaluateRatchet,
  elementFindings,
  fixedOverlayFindings,
  isTokenOwner,
  jsxStyleFindings,
  kitStoryFindings,
  menuSurfaceFindings,
  tapTargetFindings,
} from "./lib/design-kit-ratchet.mjs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(scriptDir, "..");
const repoRoot = resolve(appDir, "../..");
const WAIVER_FILE = join(scriptDir, "check-design-system-waivers", "design-system-waivers.json");

const args = parseArgs(process.argv.slice(2));

// ── Checks ────────────────────────────────────────────────────────────────────

const SCAN_DIRS = ["app", "components", "features", "lib"];
const CODE_EXT = new Set([".tsx", ".ts"]);
const STYLE_EXT = new Set([".css"]);
const THEME_FILES = new Set(["app/mesha-theme.css", "app/minimal-theme.css"]);
// The kit's own styled wrapper around <select> is the ONE allowed native select.
const NATIVE_SELECT_ALLOWED = new Set();
// Licensed MUI Minimal template code copied in verbatim (Phase 2 of the MUI migration). Its sizes,
// radii and type come from the MUI theme (theme.spacing / shape / typography), not from
// app/minimal-tokens.css, so the ratchet-tier size/radius/font/tap checks do not apply there.
// Every P0 and waivable check (foreign palette, hex colours, native elements …) still does.
const TEMPLATE_CODE_DIRS = ["components/minimal/"];
const isTemplateCode = (rel) => TEMPLATE_CODE_DIRS.some((dir) => rel.startsWith(dir));
// Lifecycle display mapping owns the legacy "F2" code so it can translate it; nothing else may.
const F2_ALLOWED = new Set(["lib/stage-display.ts", "lib/stage-labels.ts"]);

const FOREIGN_PALETTE = /#(0A9F6C|4FD89A|131A21|1B242E)\b/i;
const TAILWIND_PALETTE =
  /(?:^|[\s"'`:/])(?:hover:|focus:|dark:|md:|lg:)*(?:bg|text|border|from|to|via|ring|fill|stroke|outline|decoration|divide|placeholder|accent|caret|shadow)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-(?:50|[1-9]50|[1-9]00)\b/;
const HEX_COLOUR = /(?<![\w&])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])/;
const NATIVE_SELECT = /<select(?:\s|>)/;
const NATIVE_DATE = /type=["'](?:date|datetime-local|month|week)["']/;
const WINDOW_CONFIRM = /(?:window\.|globalThis\.|^|[^\w.$])(?:confirm|alert)\(/;
const F2_LITERAL = /(?:["'`]F2["'`]|>F2<)/;
const FIXED_PX_WIDTH = /(?:\b(?:width|minWidth|min-width)\s*[:=]\s*["'{]?\s*([4-9]\d{2}|\d{4,})px|\bw-\[([4-9]\d{2}|\d{4,})px\]|\bwidth=\{\s*([4-9]\d{2}|\d{4,})\s*\})/;
const HEADING_OPEN = /<(?:h[1-3]|PageHeader|CardHeader)\b/;
const PROSE_PARA = /<p\b[^>]*className=["'][^"']*\b(?:muted|sub|desc|description|help|hint|lead|explain|intro)\b/;
const CHART_ROOT = /<(?:LineChart|BarChart|AreaChart|PieChart|ComposedChart|RadialBarChart|ScatterChart)\b/;
const CHART_TOOLTIP = /<Tooltip\b|ChartTooltipCard|content=\{/;
const CHART_NO_ANIM = /isAnimationActive=\{false\}/;
const GOOGLE_FONTS = /fonts\.googleapis\.com|fonts\.gstatic\.com/;
// Charts are Apex (via components/minimal/chart or components/kit) or the two inline SVG helpers
// (svg-bars, svg-series). Anything else is a hand-rolled chart lib the palette lock and the
// tooltip/hover pattern have not been proven against.
const RAW_CHART_LIB = /from\s+["'](?:recharts|d3|d3-[a-z-]+|chart\.js|chartjs-[a-z-]+|@?nivo\/[a-z-]+|victory(?:-[a-z-]+)?|@visx\/[a-z-]+|echarts|@antv\/g2|lightweight-charts|highcharts)["']/;
// Numbers reach the screen through lib/format: a 3+ decimal toFixed, or a raw `${value} kg`-style
// template with no formatter in it, is the static signature of the runtime `raw-float` finding.
const RAW_FLOAT_FIXED = /\.toFixed\(\s*(?:[3-9]|[1-9]\d)\s*\)/;
const RAW_UNIT_TEMPLATE = /\$\{(?![^}]*(?:fmt|format|Format|dash|num\(|kg\(|toLocaleString|toFixed|Intl|round|Math))[^}]*\}\s*(?:kg|g\/day|g|₹|INR)`/;
// A white / near-white surface literal paints a light box inside the dark shell (the pastel KPI
// cards Ravi flagged on /sales/sold). Surfaces come from the theme (Card = background.paper). Only
// the template's AnalyticsWidgetSummary (pastel in both modes, contents on the light scheme) may.
const LIGHT_SURFACE = /(?:bgcolor|backgroundColor|background)\s*:\s*["'`](?:common\.white|#fff(?:fff)?|white|grey\.(?:50|100|200))["'`]/;
const LIGHT_SURFACE_ALLOWED = new Set(["components/minimal/widgets/analytics-widget-summary.tsx", "components/minimal/widgets/kpi-card.tsx"]);
// Legacy stylesheets only shrink; a rule there that selects a MUI class and sets a colour fights the
// theme in one of the two modes. MUI colours come from the theme palette (theme/core).
const LEGACY_CSS = new Set(["app/frame.css", "app/minimal-theme.css", "app/mesha-theme.css", "app/menu-surface.css", "app/globals.css"]);
const COLOUR_DECL = /(?:^|[;{\s])(?:color|background(?:-color|-image)?|border(?:-(?:top|right|bottom|left))?(?:-color)?|fill|stroke|outline(?:-color)?)\s*:/;
function legacyMuiColourFindings(text) {
  const out = [];
  const src = text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(src))) {
    const selector = m[1].trim();
    if (selector.startsWith("@")) continue;
    // `:not(.Mui…)` / `:where(:not(.Mui…))` EXCLUDE MUI parts; only a positive MUI selector counts.
    const positive = selector.replace(/:(?:where|is)\(\s*:not\([^()]*\)\s*\)|:not\([^()]*\)/g, "");
    if (!/\.Mui[A-Z]/.test(positive)) continue;
    // A reset that only clears legacy paint (transparent / none / 0 / inherit) is not a colour.
    const paints = m[2].split(";").filter((decl) => COLOUR_DECL.test(` ${decl}`) && !/:\s*(?:transparent|none|0|0px|inherit|initial|unset|currentcolor)\s*(?:!important)?\s*$/i.test(decl));
    if (!paints.length) continue;
    const line = src.slice(0, m.index + m[0].indexOf(selector)).split("\n").length;
    out.push({ line, snippet: selector.slice(0, 160) });
  }
  return out;
}
// Pure black/white are mask/opacity helpers, not palette colours.
const NEUTRAL_HEX = /^#(?:000|fff|000000|ffffff)$/i;

// User-visible copy must never leak implementation vocabulary (owner-mandated after
// "Firebase sign-in is not configured for this admin deployment." reached the UI).
// "Config" capitalised is the product's own module name (Sales Config, Feed Config) and is allowed;
// lowercase "config"/"configured" is implementation talk. HTTP codes only count as "HTTP 500"/"a 502".
const TECH_WORDS = /\b(?:Firebase|deployment|configured|config|backend|API|endpoint|contract|token|bearer|HTTP|(?:HTTP|status|error|code)\s+[45]\d\d|migration|schema|null|undefined|NaN|stack|trace|reference number|not wired|not deployed|internal error|something went wrong)\b/;
const TECH_WORDS_CI = /\b(?:firebase|deployment|configured|backend|endpoint|bearer|migration|schema|undefined|reference number|not wired|not deployed|internal error|something went wrong)\b/i;
const TECH_JSX_TEXT = /(?:^|[\w"'})\]])>\s*([^<>{}]*[A-Za-z][^<>{}]*)\s*<\/?[A-Za-z]/;
const TECH_STRING_LITERAL = /(?:throw new Error\(|new Error\(|message\s*[:=]\s*|title\s*[:=]\s*|label\s*[:=]\s*|body\s*[:=]\s*|hint\s*[:=]\s*|description\s*[:=]\s*|placeholder=|title=|aria-label=)\s*["'`]([^"'`]{4,})["'`]/g;
const TECH_COPY_ENTRY = /^\s*["'][\w.-]+["']\s*:\s*["'`]([^"'`]{2,})["'`]/;
const TECH_GO_ENTRY = /^\s*"[\w.-]+":\s*"((?:[^"\\]|\\.)+)"/;
// Go struct fields that reach the screen verbatim (DisplayRule.Summary, error Detail/Message on
// contract responses): `Summary: "..."` / `Detail: "..."` / `Message: "..."`, including the
// continuation of a `+`-joined literal. Same technical-copy rule as the copy tables.
const TECH_GO_FIELD = /^\s*(?:Summary|Detail|Message)\s*:\s*"((?:[^"\\]|\\.){2,})"/;
const TECH_COPY_FILE = /(?:^|\/)[\w-]*copy[\w-]*\.tsx?$|\/copy\//;
// The shared error boundaries are the ONE place "something went wrong" is the right copy.
const TECH_ALLOWED_FILES = new Set(["components/observability/error-boundary.tsx", "app/global-error.tsx"]);
const TECH_ALLOWED_PHRASES_IN_BOUNDARY = /something went wrong|internal error/i;

const CHECKS = {
  "foreign-palette": { tier: "p0", why: "another product's colour; Mesha palette is locked" },
  "theme-token-drift": { tier: "p0", why: "a colour value in the theme files was removed vs origin/main" },
  "brand-lock": { tier: "p0", why: "a locked Mesha brand/neutral token (dark or light) changed or is missing in app/mesha-theme.css" },
  "non-brand-selected": { tier: "p0", why: "a primary/selected/active state must fill with var(--brand)/var(--primary) and use var(--on-brand) text" },
  "retired-neutral-literal": { tier: "p0", why: "the old Mesha green-tinted neutrals are retired (Ravi 2026-09-27): neutrals are the template greys; use the theme (background/text/divider/grey) or var(--grey-N) / rgb(var(--g500-rgb)/a)" },
  "template-neutrals": { tier: "p0", why: "theme/theme-config.ts grey + surfaces/ink and app/minimal-tokens.css --grey-N must be exactly the MUI Minimal template's values" },
  "light-surface-literal": { tier: "p0", why: "a white/near-white surface literal (common.white, #fff, grey.50-200) is a light box in dark mode; use the theme surface (Card/Paper = background.paper) or a varAlpha tint of a palette channel. Only the template AnalyticsWidgetSummary may" },
  "legacy-css-mui-colour": { tier: "p0", why: "a legacy stylesheet (frame/minimal-theme/mesha-theme/menu-surface/globals.css) selects a .Mui* class and sets a colour/background/border; MUI colours come from the theme palette only" },
  "google-fonts-link": { tier: "p0", why: "fonts are self-hosted via next/font; no Google Fonts link" },
  "native-select": { tier: "waivable", why: "use MUI TextField select / LinkSelect / template CustomPopover + MenuList" },
  "native-date-input": { tier: "waivable", why: "use kit DateRangeField" },
  "window-confirm": { tier: "waivable", why: "use kit Dialog (confirm) — never window.confirm/alert" },
  "hex-colour-in-code": { tier: "waivable", why: "use var(--token); no colour literals in TSX/TS" },
  "hex-colour-in-css": { tier: "waivable", why: "only the two theme files may define colour literals" },
  "tailwind-palette-class": { tier: "waivable", why: "no raw Tailwind palette classes; tokens only" },
  "f2-literal": { tier: "waivable", why: "never show the legacy 'F2' code; lifecycle names only" },
  "fixed-px-width": { tier: "waivable", why: "fixed px width >= 480 cannot fit a 390px phone; use min(…, 100%)" },
  "prose-under-title": { tier: "waivable", why: "no explanatory paragraph under a title/card header" },
  "raw-float-format": { tier: "waivable", why: "numbers render through lib/format (max 2 decimals); no toFixed(3+) or raw `${n} kg` templates" },
  "chart-without-tooltip": { tier: "waivable", why: "every chart needs the shared tooltip card" },
  "chart-animation-disabled": { tier: "waivable", why: "charts draw in on mount (reduced-motion handles the rest)" },
  "missing-loading-tsx": { tier: "waivable", why: "every admin page needs a shape-matched loading.tsx skeleton" },
  "page-outside-shell": { tier: "soft", why: "every admin page renders inside a `.kit-page` column with a PageHeader" },
  "technical-copy": { tier: "waivable", why: "user-visible copy must not name Firebase/config/backend/API/tokens/HTTP codes/null/NaN/stack — say what the person can do instead" },
  "raw-chart-lib": { tier: "waivable", why: "only Apex (components/minimal/chart, components/kit) and the two inline helpers (svg-bars/svg-series) are palette-locked and hover-proven; recharts/d3/chart.js/nivo/victory/visx/echarts/highcharts are refused" },
  "route-template-map-missing": { tier: "waivable", why: "every route in scripts/smoke-visual-live.mjs must have an entry in docs/design/route-template-map.json so the MUI Minimal template section it is built on is discoverable" },
  "section-client-boundary": { tier: "p0", why: "a template section under components/minimal/sections/ that uses hooks or a function sx/theme callback must start with 'use client'; a server page rendering it would otherwise pass a function to a client component and crash at render (typecheck cannot see it)" },
  "page-template-no-pastel": { tier: "p0", why: "a page listed in docs/design/page-template-map.md must not use KpiCard variant tint/gradient or AnalyticsWidgetSummary (pastel in dark); KPI rows are the template Ecommerce/Course/Banking widget summaries" },
  "unsourced-minimal-file": { tier: "p0", why: "components/minimal/ holds template-derived code only; every file needs an entry in docs/design/template-sources.json mapping it to a Minimal template source path" },
  "page-template-map": { tier: "p0", why: "every route row in docs/design/page-template-map.md names the feature files that render it and the template section modules they must compose; a mapped page that stops importing one of its template sections (or maps to a file that no longer exists) has drifted back to hand-made UI" },
  "legacy-kit-import": { tier: "p0", why: "the hand-built components/kit is retired; import the template (components/minimal), MUI, or a components/app behaviour wrapper instead — components/kit must not come back" },
  "client-api-without-use-client": { tier: "p0", why: "a module that calls a client-only React/Next API (useState/useEffect/useRef/useTransition/useRouter/useSearchParams/usePathname/useLinkStatus …) or wires a JSX event handler (onClick={…}) must start with \"use client\"; otherwise a server component that imports it breaks `next build` (typecheck does not catch it)" },
  "app-wrapper-css-import": { tier: "p0", why: "components/app/ holds thin behaviour wrappers over template + MUI components only; they must not import .css / .module.css — style through the template component's props/theme instead" },
  // MUI Minimal kit + token enforcement (scripts/lib/design-kit-ratchet.mjs). Ratchet tier:
  // counted per check|file against an explicit allowance that may only shrink.
  ...Object.fromEntries(Object.entries(RATCHET_CHECKS).map(([check, why]) => [check, { tier: "ratchet", why }])),
};

// guard: client-api-without-use-client (2026-09-27: a re-export of next/link's useLinkStatus in a
// server-importable module broke `next build` on the PR head while typecheck stayed green).
const CLIENT_ONLY_API = /\b(useState|useEffect|useLayoutEffect|useReducer|useRef|useTransition|useOptimistic|useActionState|useFormStatus|useRouter|useSearchParams|usePathname|useLinkStatus|useSelectedLayoutSegments?)\b/;
const JSX_HANDLER = /\son[A-Z][A-Za-z]+=\{/;
const USE_CLIENT_FIRST = /^\s*(?:\/\/[^\n]*\n\s*|\/\*[\s\S]*?\*\/\s*)*["']use client["']/;
function clientApiFindings(text, rel) {
  if (USE_CLIENT_FIRST.test(text) || /^\s*["']use server["']/.test(text)) return [];
  const out = [];
  text.split("\n").forEach((line, index) => {
    if (/^\s*(\/\/|\*|\/\*)/.test(line)) return;
    if (CLIENT_ONLY_API.test(line) || (rel.endsWith(".tsx") && JSX_HANDLER.test(line))) out.push({ line: index + 1, snippet: line.trim() });
  });
  return out.slice(0, 1);
}

function runGuard(root, { themeDiff }) {
  const findings = [];
  const notes = [];
  const files = SCAN_DIRS.flatMap((dir) => walk(join(root, dir))).map((abs) => ({ abs, rel: toRel(root, abs) }));

  for (const file of files) {
    const ext = file.rel.slice(file.rel.lastIndexOf("."));
    if (!CODE_EXT.has(ext) && !STYLE_EXT.has(ext)) continue;
    if (/\.(test|stories)\.(tsx?|mjs)$/.test(file.rel) || file.rel.includes(`${sep}__tests__${sep}`)) continue;
    const text = readFileSync(file.abs, "utf8");
    const lines = text.split("\n");
    const isTheme = THEME_FILES.has(file.rel);

    // P0 across every file type.
    lines.forEach((line, index) => {
      if (FOREIGN_PALETTE.test(line)) findings.push(finding("foreign-palette", file.rel, index + 1, line));
      if (GOOGLE_FONTS.test(line)) findings.push(finding("google-fonts-link", file.rel, index + 1, line));
    });
    for (const hit of retiredNeutralFindings(file.rel, text)) findings.push(finding("retired-neutral-literal", file.rel, hit.line, hit.snippet));
    if (STYLE_EXT.has(ext) || file.rel.endsWith("-styles.tsx")) {
      for (const hit of primaryStateFindings(text)) findings.push(finding("non-brand-selected", file.rel, hit.line, hit.snippet));
    }

    if (STYLE_EXT.has(ext)) {
      if (LEGACY_CSS.has(file.rel)) for (const hit of legacyMuiColourFindings(text)) findings.push(finding("legacy-css-mui-colour", file.rel, hit.line, hit.snippet));
      if (isTheme) continue;
      lines.forEach((line, index) => {
        if (isComment(line)) return;
        // The token file owns the grey scale (Mesha neutrals; the only colour literals outside the theme files).
        if (file.rel !== TOKEN_FILE && hasPaletteHex(stripUrls(line))) findings.push(finding("hex-colour-in-css", file.rel, index + 1, line));
        if (!isTokenOwner(file.rel)) for (const check of new Set(cssDeclFindings(line))) findings.push(finding(check, file.rel, index + 1, line));
      });
      if (!isTokenOwner(file.rel)) for (const hit of tapTargetFindings(text)) findings.push(finding("phone-tap-target", file.rel, hit.line, hit.snippet));
      continue;
    }

    // Code files.
    for (const hit of clientApiFindings(text, file.rel)) findings.push(finding("client-api-without-use-client", file.rel, hit.line, hit.snippet));
    for (const hit of fixedOverlayFindings(text, file.rel)) findings.push(finding("fixed-overlay-no-portal", file.rel, hit.line, hit.snippet));
    for (const hit of menuSurfaceFindings(text, file.rel)) findings.push(finding("raw-menu", file.rel, hit.line, hit.snippet));
    let chartFile = false;
    let hasTooltip = false;
    let inBlockComment = false;
    lines.forEach((raw, index) => {
      const lineNo = index + 1;
      let line = raw;
      if (inBlockComment) {
        if (line.includes("*/")) {
          inBlockComment = false;
          line = line.slice(line.indexOf("*/") + 2);
        } else return;
      }
      if (line.includes("/*") && !line.includes("*/")) {
        inBlockComment = true;
        line = line.slice(0, line.indexOf("/*"));
      }
      if (isComment(line)) return;
      const code = stripLineComment(line);

      if (NATIVE_SELECT.test(code) && !NATIVE_SELECT_ALLOWED.has(file.rel)) findings.push(finding("native-select", file.rel, lineNo, raw));
      if (NATIVE_DATE.test(code)) findings.push(finding("native-date-input", file.rel, lineNo, raw));
      if (WINDOW_CONFIRM.test(code)) findings.push(finding("window-confirm", file.rel, lineNo, raw));
      if (hasPaletteHex(stripUrls(code))) findings.push(finding("hex-colour-in-code", file.rel, lineNo, raw));
      if (LIGHT_SURFACE.test(code) && !LIGHT_SURFACE_ALLOWED.has(file.rel)) findings.push(finding("light-surface-literal", file.rel, lineNo, raw));
      if (TAILWIND_PALETTE.test(code)) findings.push(finding("tailwind-palette-class", file.rel, lineNo, raw));
      if (F2_LITERAL.test(code) && !F2_ALLOWED.has(file.rel)) findings.push(finding("f2-literal", file.rel, lineNo, raw));
      if (FIXED_PX_WIDTH.test(code) && !/max-?[wW]idth|overflow/.test(code)) findings.push(finding("fixed-px-width", file.rel, lineNo, raw));
      if (CHART_NO_ANIM.test(code)) findings.push(finding("chart-animation-disabled", file.rel, lineNo, raw));
      if (RAW_FLOAT_FIXED.test(code) || RAW_UNIT_TEMPLATE.test(code)) findings.push(finding("raw-float-format", file.rel, lineNo, raw));
      if (RAW_CHART_LIB.test(code)) findings.push(finding("raw-chart-lib", file.rel, lineNo, raw));
      for (const check of new Set([...cssDeclFindings(code), ...jsxStyleFindings(code), ...elementFindings(code, file.rel)])) {
        findings.push(finding(check, file.rel, lineNo, raw));
      }
      for (const text of userVisibleStrings(code, file.rel)) {
        if (!TECH_WORDS.test(text) && !TECH_WORDS_CI.test(text)) continue;
        if (TECH_ALLOWED_FILES.has(file.rel) && TECH_ALLOWED_PHRASES_IN_BOUNDARY.test(text)) continue;
        if (/aria-hidden|sr-only/.test(code) && /aria-label=/.test(code)) continue; // icon labels
        findings.push(finding("technical-copy", file.rel, lineNo, raw));
        break;
      }
      if (CHART_ROOT.test(code)) chartFile = true;
      if (CHART_TOOLTIP.test(code)) hasTooltip = true;
      if (HEADING_OPEN.test(code)) {
        // A muted/description paragraph within the next 3 lines of a heading is prose under a title.
        for (let look = 1; look <= 3 && index + look < lines.length; look += 1) {
          const next = lines[index + look];
          if (PROSE_PARA.test(next)) {
            findings.push(finding("prose-under-title", file.rel, lineNo + look, next));
            break;
          }
        }
      }
    });
    if (chartFile && !hasTooltip) findings.push(finding("chart-without-tooltip", file.rel, 1, "<chart root without <Tooltip>/ChartTooltipCard>"));
  }

  // Backend copy tables reachable from this repo (backend/internal/adminui): the same rule.
  const adminUiDir = resolve(root, "../../backend/internal/adminui");
  for (const abs of walk(adminUiDir).filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))) {
    const rel = `backend/internal/adminui/${toRel(adminUiDir, abs)}`;
    readFileSync(abs, "utf8").split("\n").forEach((line, index) => {
      const m = TECH_GO_ENTRY.exec(line) ?? TECH_GO_FIELD.exec(line);
      if (m && (TECH_WORDS.test(m[1]) || TECH_WORDS_CI.test(m[1])) && !/^\s*\/\//.test(line)) findings.push(finding("technical-copy", rel, index + 1, line));
    });
  }

  // Every admin page has a sibling loading.tsx; and (soft) renders inside the page frame
  // (a `.kit-page` column and a PageHeader).
  const adminDir = join(root, "app", "(admin)");
  const shellExported = kitExports(root, "PageHeader");
  if (!shellExported) notes.push("page-outside-shell: skipped — PageHeader is not resolvable in components/kit or components/app yet");
  for (const pageAbs of walk(adminDir).filter((abs) => abs.endsWith(`${sep}page.tsx`))) {
    const pageDir = dirname(pageAbs);
    const rel = toRel(root, pageAbs);
    if (!existsSync(join(pageDir, "loading.tsx"))) findings.push(finding("missing-loading-tsx", rel, 1, "no sibling loading.tsx"));
    if (shellExported && !rendersInsideShell(root, pageAbs)) findings.push(finding("page-outside-shell", rel, 1, "neither the page nor its feature entry uses PageHeader / .kit-page"));
  }

  for (const hit of kitStoryFindings(root)) findings.push(finding("kit-missing-story", hit.file, 1, hit.snippet));

  // Every non-dynamic route in scripts/smoke-visual-live.mjs must be covered by a matching
  // area in docs/design/route-template-map.json — a NEW page must land its template mapping
  // in the same change. Dynamic routes (goat/load/workflow ids) share the parent's area.
  {
    const mapFile = resolve(root, "../../docs/design/route-template-map.json");
    const smoke = join(root, "scripts", "smoke-visual-live.mjs");
    if (existsSync(mapFile) && existsSync(smoke)) {
      let areas = [];
      try { areas = JSON.parse(readFileSync(mapFile, "utf8")).areas ?? []; } catch {}
      const prefixRegexes = areas.flatMap((a) => (a.route_prefixes ?? []).map((p) => new RegExp(`^${p}`)));
      const source = readFileSync(smoke, "utf8");
      const buildBlock = source.match(/function buildRoutes\([^)]*\) \{[\s\S]*?(?:^\}|pagerMinimums = new Map)/m)?.[0] ?? source;
      const names = new Set();
      for (const [, name] of buildBlock.matchAll(/name:\s*"([^"]+)"/g)) names.add(name);
      for (const name of names) {
        if (!prefixRegexes.some((re) => re.test(name))) findings.push(finding("route-template-map-missing", "scripts/smoke-visual-live.mjs", 1, `route "${name}" has no area in docs/design/route-template-map.json`));
      }
    } else if (!existsSync(mapFile)) {
      findings.push(finding("route-template-map-missing", "docs/design/route-template-map.json", 1, "route-template-map.json is missing"));
    }
  }

  // Pages mapped in docs/design/page-template-map.md must not fall back to the pastel KpiCard
  // tint/gradient or AnalyticsWidgetSummary (the look Ravi rejected on /sales/sold). The import
  // side of the map is `page-template-map` below.
  for (const hit of pageTemplatePastelFindings(root)) findings.push(finding("page-template-no-pastel", hit.file, hit.line, hit.snippet));
  {
    const sectionsDir = join(root, "components", "minimal", "sections");
    if (existsSync(sectionsDir)) {
      for (const abs of walk(sectionsDir).filter((f) => /\.tsx?$/.test(f))) {
        const text = readFileSync(abs, "utf8");
        if (/^\s*['"]use client['"]/.test(text)) continue;
        const lines = text.split("\n");
        const at = lines.findIndex((line) => !/^\s*(\/\/|\*)/.test(line) && /\(theme\)\s*=>|\buse(?:Theme|State|Callback|Effect|Memo|Chart)\(/.test(line));
        if (at >= 0) findings.push(finding("section-client-boundary", toRel(root, abs), at + 1, lines[at]));
      }
    }
  }

  // components/minimal/ holds ONLY template-derived code (verbatim, near-verbatim, or a
  // structural adaptation). Every file must have a source entry in
  // docs/design/template-sources.json naming the template path it derives from. A hand-made
  // wrapper hides itself from the guard if dropped here, so a missing entry fails p0.
  {
    const manifestFile = resolve(root, "../../docs/design/template-sources.json");
    const minimalDir = join(root, "components", "minimal");
    if (existsSync(manifestFile) && existsSync(minimalDir)) {
      let sources = {};
      try { sources = JSON.parse(readFileSync(manifestFile, "utf8")).sources ?? {}; } catch {}
      const mapped = new Set(Object.keys(sources));
      for (const abs of walk(minimalDir)) {
        const rel = toRel(root, abs);
        if (!mapped.has(rel)) findings.push(finding("unsourced-minimal-file", rel, 1, `no entry in docs/design/template-sources.json`));
      }
      for (const rel of mapped) {
        const abs = join(root, rel);
        if (!existsSync(abs)) findings.push(finding("unsourced-minimal-file", "docs/design/template-sources.json", 1, `stale entry: ${rel} no longer exists`));
      }
    } else if (!existsSync(manifestFile)) {
      findings.push(finding("unsourced-minimal-file", "docs/design/template-sources.json", 1, "template-sources.json is missing"));
    }
  }

  // docs/design/page-template-map.md: route -> template page -> template section components.
  // Machine-read rows are `| /route | template page | feature files | section modules |` where the
  // last two columns are comma-separated backticked paths. The union of the feature files' imports
  // must include every listed section module (matched as an import specifier `@/<module>` prefix),
  // so a page cannot silently fall back to hand-made cards after it was mapped.
  {
    const local = join(root, "docs", "design", "page-template-map.md");
    const mapFile = existsSync(local) ? local : resolve(root, "../../docs/design/page-template-map.md");
    if (existsSync(mapFile)) {
      const mapRel = existsSync(local) ? "docs/design/page-template-map.md" : "../../docs/design/page-template-map.md";
      readFileSync(mapFile, "utf8").split("\n").forEach((line, index) => {
        const cells = line.split("|").map((c) => c.trim());
        if (cells.length < 6 || !cells[1].startsWith("`/")) return;
        const ticks = (cell) => [...cell.matchAll(/`([^`]+)`/g)].map((m) => m[1]);
        const route = ticks(cells[1])[0];
        const files = ticks(cells[3]);
        const modules = ticks(cells[4]);
        let imports = "";
        for (const file of files) {
          const abs = join(root, file);
          if (!existsSync(abs)) {
            findings.push(finding("page-template-map", mapRel, index + 1, `${route}: mapped file ${file} does not exist`));
            continue;
          }
          imports += readFileSync(abs, "utf8");
        }
        for (const mod of modules) {
          const spec = new RegExp(`from\\s+["']@/${mod.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?:["'/])`);
          if (!spec.test(imports)) findings.push(finding("page-template-map", mapRel, index + 1, `${route}: none of ${files.join(", ")} imports @/${mod}`));
        }
      });
    }
  }

  // components/app/ holds thin behaviour wrappers over template + MUI components (Caption's
  // long-text→InfoHint swap, InfoHint's server-safe glyph+tooltip, PageHeader's route/crumb wiring,
  // page skeleton layouts, EmptyState copy defaults). They must render ONLY template/MUI parts and
  // must not import stylesheets — the design/theme drives their look.
  {
    const appDir = join(root, "components", "app");
    if (existsSync(appDir)) {
      for (const abs of walk(appDir)) {
        const rel = toRel(root, abs);
        if (!CODE_EXT.has(rel.slice(rel.lastIndexOf(".")))) continue;
        if (/\.(test|stories)\.tsx?$/.test(rel)) continue;
        const lines = readFileSync(abs, "utf8").split("\n");
        lines.forEach((line, index) => {
          if (/from\s+["'][^"']*\.(?:module\.)?css["']/.test(line) || /^\s*import\s+["'][^"']*\.(?:module\.)?css["']/.test(line)) {
            findings.push(finding("app-wrapper-css-import", rel, index + 1, line));
          }
        });
      }
    }
  }

  // components/kit/ (the old hand-built kit) is retired. Any import that resolves into it — the
  // "@/components/kit" alias or a relative path, in app code, stories or Storybook config — fails
  // P0, and so does the directory itself reappearing.
  {
    const KIT_SPEC = /(?:\bfrom\s*|\bimport\s*\(\s*|^\s*import\s+|\brequire\(\s*)["']([^"']+)["']/g;
    const intoKit = (fromAbs, spec) => {
      let target;
      if (spec.startsWith("@/")) target = spec.slice(2);
      else if (spec.startsWith(".")) target = toRel(root, resolve(dirname(fromAbs), spec));
      else return false;
      return target === "components/kit" || target.startsWith("components/kit/");
    };
    for (const abs of [...SCAN_DIRS, "stories", ".storybook"].flatMap((dir) => walk(join(root, dir)))) {
      const rel = toRel(root, abs);
      if (rel.startsWith("components/kit/") || !/\.(?:tsx?|mjs|js)$/.test(rel)) continue;
      readFileSync(abs, "utf8").split("\n").forEach((line, index) => {
        for (const m of line.matchAll(KIT_SPEC)) {
          if (intoKit(abs, m[1])) {
            findings.push(finding("legacy-kit-import", rel, index + 1, line));
            break;
          }
        }
      });
    }
    if (existsSync(join(root, "components", "kit"))) findings.push(finding("legacy-kit-import", "components/kit", 1, "components/kit/ exists; the hand-built kit is retired"));
  }

  // Brand lock: the palette values from docs/design/README.md must exist verbatim in mesha-theme.css.
  const meshaTheme = join(root, "app", "mesha-theme.css");
  if (existsSync(meshaTheme)) {
    const themeText = readFileSync(meshaTheme, "utf8").toUpperCase();
    for (const locked of BRAND_LOCK) {
      if (!themeText.includes(locked)) findings.push(finding("brand-lock", "app/mesha-theme.css", 1, `locked colour ${locked} is missing`));
    }
    for (const msg of themeLockFindings(readFileSync(meshaTheme, "utf8"))) findings.push(finding("brand-lock", "app/mesha-theme.css", 1, msg));
  }

  // MUI Minimal theme (theme/, layouts/ — template-derived, Minimal v7.7.0 next-ts). Those files are
  // allowed template code and are NOT in SCAN_DIRS, but their palette is still the locked Mesha one:
  // brand/surface hexes must match app/mesha-theme.css and no Minimal default brand hex may survive.
  for (const msg of muiPaletteLockFindings(root)) findings.push(finding("brand-lock", "theme/theme-config.ts", 1, msg));
  for (const msg of templateNeutralFindings(root)) findings.push(finding("template-neutrals", "theme/theme-config.ts", 1, msg));

  if (themeDiff) {
    for (const theme of THEME_FILES) {
      const diff = spawnSync("git", ["diff", "--quiet", "origin/main", "--", join("apps/admin-web", theme)], { cwd: repoRoot });
      if (diff.status === 128) {
        notes.push(`theme-token-drift: skipped — origin/main is not available in this checkout`);
        break;
      }
      if (diff.status === 1) {
        // A diff is only a P0 when a colour VALUE moved; adding non-colour rules is allowed.
        const patch = spawnSync("git", ["diff", "origin/main", "--", join("apps/admin-web", theme)], { cwd: repoRoot, encoding: "utf8" }).stdout ?? "";
        // Renaming a token is fine; a colour VALUE that existed on main and is gone is not.
        const current = readFileSync(join(root, theme), "utf8").toLowerCase();
        const removedValues = new Set();
        for (const l of patch.split("\n")) {
          if (!l.startsWith("-") || l.startsWith("---")) continue;
          for (const m of l.matchAll(/#[0-9a-f]{6}\b/gi)) removedValues.add(m[0].toLowerCase()); // hex only: rgba() shadows/overlays are not palette
        }
        for (const value of removedValues) {
          if (isDriftRemoval(value) && !current.replace(/\s+/g, "").includes(value)) findings.push(finding("theme-token-drift", theme, 1, `colour value ${value} removed`));
        }
      }
    }
  }
  const templateExempt = findings.filter((f) => isTemplateCode(f.file) && CHECKS[f.check].tier === "ratchet");
  if (templateExempt.length) notes.push(`template-code: ${templateExempt.length} ratchet finding(s) exempt under ${TEMPLATE_CODE_DIRS.join(", ")}`);
  return { findings: findings.filter((f) => !templateExempt.includes(f)), notes, scanned: files.length };
}

function rendersInsideShell(root, pageAbs) {
  const text = readFileSync(pageAbs, "utf8");
  if (/PageShell|PageHeader|kit-page\b/.test(text)) return true;
  // Follow `@/features/<name>` and relative imports one level: the page usually just renders
  // a feature entry component that owns the shell.
  const imports = [...text.matchAll(/from\s+["']([^"']+)["']/g)].map((m) => m[1]);
  for (const spec of imports) {
    let target = null;
    if (spec.startsWith("@/")) target = join(root, spec.slice(2));
    else if (spec.startsWith(".")) target = resolve(dirname(pageAbs), spec);
    if (!target) continue;
    for (const candidate of [target, `${target}.tsx`, `${target}.ts`, join(target, "index.ts"), join(target, "index.tsx")]) {
      if (existsSync(candidate) && statSync(candidate).isFile()) {
        const body = readFileSync(candidate, "utf8");
        if (/PageShell|PageHeader|kit-page\b/.test(body)) return true;
        // index.ts barrels: look one more hop into the files it re-exports.
        if (/index\.tsx?$/.test(candidate)) {
          for (const inner of [...body.matchAll(/from\s+["'](\.[^"']+)["']/g)].map((m) => m[1])) {
            const innerAbs = resolve(dirname(candidate), inner);
            for (const c of [`${innerAbs}.tsx`, `${innerAbs}.ts`, join(innerAbs, "index.tsx"), join(innerAbs, "index.ts")]) {
              if (existsSync(c) && /PageShell|PageHeader|kit-page\b/.test(readFileSync(c, "utf8"))) return true;
            }
          }
        }
        break;
      }
    }
  }
  return false;
}

/** Strings a person can read: JSX text, error/label/title/body literals, copy-module entries. */
function userVisibleStrings(code, rel) {
  const out = [];
  const jsx = TECH_JSX_TEXT.exec(code);
  if (jsx && jsx[1].trim().length >= 4) out.push(jsx[1].trim());
  for (const m of code.matchAll(TECH_STRING_LITERAL)) out.push(m[1]);
  if (TECH_COPY_FILE.test(rel)) {
    const m = TECH_COPY_ENTRY.exec(code);
    if (m) out.push(m[1]);
  }
  return out;
}

function kitExports(root, name) {
  const index = join(root, "components", "kit", "index.ts");
  const pattern = new RegExp(`\\b${name}\\b`);
  if (existsSync(index) && pattern.test(readFileSync(index, "utf8"))) return true;
  for (const rel of ["components/kit/page-header.tsx", "components/app/page-header.tsx"]) {
    const file = join(root, rel);
    if (existsSync(file) && pattern.test(readFileSync(file, "utf8"))) return true;
  }
  return false;
}

// ── Reporting / waivers ──────────────────────────────────────────────────────

function finish({ findings, notes, scanned }) {
  const waiverFile = existsSync(WAIVER_FILE) ? JSON.parse(readFileSync(WAIVER_FILE, "utf8")) : { waived: [], reasons: {} };
  const reasons = waiverFile.reasons ?? {};
  // technical-copy is waivable only with a written reason ("genuinely unreachable in prod").
  const waived = new Set((waiverFile.waived ?? []).filter((key) => !key.startsWith("technical-copy|") || reasons[key]));
  const p0 = findings.filter((f) => CHECKS[f.check].tier === "p0");
  const ratchet = evaluateRatchet(findings.filter((f) => CHECKS[f.check].tier === "ratchet"), waiverFile.ratchet ?? {});
  const waivable = findings.filter((f) => CHECKS[f.check].tier !== "p0" && CHECKS[f.check].tier !== "ratchet");
  const unwaived = waivable.filter((f) => !waived.has(f.key));
  const stale = [...waived].filter((key) => !findings.some((f) => f.key === key));

  if (args.updateBaseline) {
    mkdirSync(dirname(WAIVER_FILE), { recursive: true });
    writeFileSync(
      WAIVER_FILE,
      `${JSON.stringify(
        {
          note: "Accepted design-system debt. Every entry is a KNOWN violation of .agents/skills/design-system/SKILL.md that a future change must not multiply. Shrink this list; never grow it to land a change. P0 checks (foreign-palette, theme-token-drift, google-fonts-link) can never be waived.",
          updated_at: new Date().toISOString(),
          waived: waivable.filter((f) => f.check !== "technical-copy" || reasons[f.key]).map((f) => f.key).sort(),
          reasons: Object.fromEntries(Object.entries(reasons).filter(([key]) => waivable.some((f) => f.key === key))),
          ratchet: ratchet.nextBaseline,
        },
        null,
        2,
      )}\n`,
    );
    console.log(`design_system_waivers_written=${waivable.length} -> ${toRel(repoRoot, WAIVER_FILE)}`);
  }

  const summary = {
    lane: "admin-web-design-system",
    scanned_files: scanned,
    findings_total: findings.length,
    p0: p0.length,
    waived: waivable.length - unwaived.length,
    stale_waivers: stale.length,
    ratchet_files_allowed: Object.keys(waiverFile.ratchet ?? {}).length,
    ratchet_over: ratchet.over.length,
    ratchet_shrinkable: ratchet.shrinkable.length,
    failures: p0.length + unwaived.length + ratchet.over.length,
    by_check: countBy([...p0, ...unwaived, ...ratchet.over.map((o) => o.sample)], (f) => f.check),
    notes,
  };
  if (args.json) {
    mkdirSync(dirname(args.json), { recursive: true });
    writeFileSync(args.json, `${JSON.stringify({ ...summary, findings: args.report ? findings : [...p0, ...unwaived] }, null, 2)}\n`);
  }
  console.log(JSON.stringify(summary, null, 2));
  for (const note of notes) console.log(`-- ${note}`);
  if (args.report) {
    for (const f of findings) console.log(`${waived.has(f.key) ? "waived " : "       "} ${f.check} ${f.file}:${f.line}  ${f.snippet}`);
  }
  if (stale.length > 0) {
    console.log(`-- ${stale.length} waiver(s) no longer match a finding (debt paid down — run --update-baseline to shrink the list):`);
    for (const key of stale.slice(0, 10)) console.log(`   ${key}`);
  }
  if (ratchet.shrinkable.length > 0) {
    console.log(`-- ${ratchet.shrinkable.length} ratchet allowance(s) can shrink (debt paid down — run --update-baseline):`);
    for (const s of ratchet.shrinkable.slice(0, 10)) console.log(`   ${s.key} ${s.allowed} -> ${s.count}`);
  }
  if (summary.failures === 0 || args.updateBaseline) {
    console.log(`design_system_guard=OK waived=${summary.waived}`);
    return;
  }
  console.error(`design_system_guard=FAIL failures=${summary.failures} (p0=${p0.length})`);
  for (const f of [...p0, ...unwaived].slice(0, 60)) {
    console.error(`  - [${CHECKS[f.check].tier}] ${f.check} ${f.file}:${f.line}\n      ${f.snippet}\n      -> ${CHECKS[f.check].why}`);
  }
  for (const o of ratchet.over.slice(0, 40)) {
    console.error(`  - [ratchet] ${o.key}: ${o.count} found, ${o.allowed} allowed (new instance e.g. line ${o.sample.line})\n      ${o.sample.snippet}\n      -> ${CHECKS[o.sample.check].why}`);
  }
  process.exit(1);
}

function finding(check, file, line, snippet) {
  const normalized = String(snippet).trim().replace(/\s+/g, " ").slice(0, 160);
  return { check, file, line, snippet: normalized, key: `${check}|${file}|${normalized}` };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function walk(dir) {
  if (!existsSync(dir)) return [];
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const abs = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(abs));
    else out.push(abs);
  }
  return out.sort();
}
function toRel(root, abs) {
  return relative(root, abs).split(sep).join("/");
}
function isComment(line) {
  const t = line.trim();
  return t.startsWith("//") || t.startsWith("*") || t.startsWith("/*") || t.startsWith("{/*");
}
function stripLineComment(line) {
  const at = line.indexOf("//");
  return at >= 0 && !/https?:\/\//.test(line.slice(0, at + 2)) ? line.slice(0, at) : line;
}
/** Rows of the machine-read table in docs/design/page-template-map.md. */
function pageTemplateRows(text) {
  const rows = [];
  for (const line of text.split("\n")) {
    const cells = line.split("|").map((cell) => cell.trim());
    if (cells.length < 6 || !/^`\//.test(cells[1] ?? "")) continue;
    const ticks = (cell) => [...cell.matchAll(/`([^`]+)`/g)].map((m) => m[1]);
    rows.push({ route: ticks(cells[1])[0], files: ticks(cells[3]), sections: ticks(cells[4]) });
  }
  return rows;
}
function pageTemplatePastelFindings(root) {
  const local = join(root, "docs", "design", "page-template-map.md");
  const mapFile = existsSync(local) ? local : resolve(root, "../../docs/design/page-template-map.md");
  if (!existsSync(mapFile)) return [];
  const out = [];
  for (const row of pageTemplateRows(readFileSync(mapFile, "utf8"))) {
    const sources = row.files.map((rel) => ({ rel, text: existsSync(join(root, rel)) ? readFileSync(join(root, rel), "utf8") : null }));
    for (const source of sources) {
      if (source.text === null) continue; // a missing file is `page-template-map`'s finding
      source.text.split("\n").forEach((line, index) => {
        if (/^\s*(\/\/|\*)/.test(line)) return;
        // KpiCard's default is the template Ecommerce/Course anatomy; only its tint/gradient
        // variants (AnalyticsWidgetSummary, pastel in dark) are refused here.
        if (/\bAnalyticsWidgetSummary\b/.test(line) || (/variant=["'](?:tint|gradient)["']/.test(line) && /KpiCard/.test(source.text))) {
          out.push({ file: source.rel, line: index + 1, snippet: `${row.route}: pastel widget instead of the template section: ${line.trim()}` });
        }
      });
    }
  }
  return out;
}
function hasPaletteHex(text) {
  return [...text.matchAll(new RegExp(HEX_COLOUR.source, "g"))].some((m) => !NEUTRAL_HEX.test(m[0]));
}
function stripUrls(line) {
  return line.replace(/https?:\/\/\S+/g, "").replace(/url\([^)]*\)/g, "");
}
function countBy(items, keyOf) {
  const out = {};
  for (const item of items) out[keyOf(item)] = (out[keyOf(item)] ?? 0) + 1;
  return Object.fromEntries(Object.entries(out).sort());
}
function parseArgs(argv) {
  const parsed = { updateBaseline: false, report: false, json: undefined, selfTest: false, noThemeDiff: false };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--update-baseline") parsed.updateBaseline = true;
    else if (arg === "--report") parsed.report = true;
    else if (arg === "--json") parsed.json = resolve(argv[++i]);
    else if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--no-theme-diff") parsed.noThemeDiff = true;
    else throw new Error(`Unknown argument: ${arg}`);
  }
  return parsed;
}

// ── Self-test: every check must catch a synthetic violation ──────────────────
async function selfTest() {
  const root = mkdtempSync(join(tmpdir(), "design-system-selftest-"));
  const put = (rel, text) => {
    mkdirSync(join(root, dirname(rel)), { recursive: true });
    writeFileSync(join(root, rel), text);
  };
  put("theme/theme-config.ts", "grey: {\n      50: '#F4F7F2',\n    },\n  surfaces: {}\n");
  put("components/app/page-header.tsx", 'export function PageHeader() { return null; }\n');
  put("components/kit/index.ts", 'export { BarList } from "./bar-list";\n');
  put("components/bad.tsx", [
    'const a = <select><option>x</option></select>;',
    'const b = <input type="date" />;',
    'if (window.confirm("sure?")) {}',
    'const c = <div style={{ color: "#0A9F6C" }} />;',
    'const d = <div style={{ color: "#ff0000" }} />;',
    'const e = <div className="bg-emerald-500 text-slate-400" />;',
    'const f = "F2";',
    'const g = <div style={{ width: 640 }} className="w-[720px]" />;',
    '<h2>Title</h2>',
    '<p className="muted">Explains the title in a paragraph.</p>',
    '<BarChart data={d}><Bar isAnimationActive={false} /></BarChart>',
    '<link href="https://fonts.googleapis.com/css2?family=Public+Sans" />',
    'const h = `${weight} kg`; const i = value.toFixed(4);',
    'throw new Error("Firebase sign-in is not configured for this deployment.");',
    '<table><tbody /></table>',
    '<button onClick={go}>Go</button>',
    '<input value={v} />',
    '<div role="tablist" />',
    '<div role="dialog" aria-modal="true" />',
    '<span role="tooltip">tip</span>',
    '<span className="chip ok">Done</span>',
    '<input type="checkbox" checked={on} />',
    '<div className="alert warn">Careful</div>',
    '<span className="av">RT</span>',
    '<div style={{ fontSize: 13, borderRadius: 10, boxShadow: "0 2px 4px red", padding: 12 }} />',
    '<div style={{ position: "fixed", inset: 0 }} />',
    '<div className="parkmenu" role="menu" />',
    'import { LineChart } from "recharts";',
    '<Card sx={{ backgroundColor: "common.white" }} />',
  ].join("\n"));
  put("app/frame.css", ".wrap .MuiCard-root{background:var(--paper)}\n.fld label:where(:not(.MuiFormLabel-root)){color:var(--muted)}\n.MuiInputBase-input{border:0;background-color:transparent}\n");
  put("components/bad.css", ".x { color: #abcdef; }\n.g{background:#0E1512}\n.y{padding:12px;border-radius:10px;box-shadow:0 4px 8px black;font-size:13px}\n@media (max-width:600px){\n.btn{min-height:32px}\n}\n.metricseg a.on{background:var(--paper)}\n");
  // Template code: ratchet-tier sizes are exempt, a foreign palette is still P0.
  put("components/minimal/tpl.tsx", 'const t = <div style={{ fontSize: 13, borderRadius: 10, padding: 12 }} />;\n');
  put("components/app/bad.tsx", 'import styles from "./bad.module.css";\nexport function Bad() { return <div className={styles.x} />; }\n');
  put("components/server-hook.ts", 'export { useLinkStatus } from "next/link";\n');
  put("components/client-ok.tsx", '"use client";\nimport { useState } from "react";\nexport function Ok() { const [a] = useState(0); return <b onClick={() => a}>x</b>; }\n');
  put("components/kit/lonely.tsx", "export function Lonely() { return null; }\n");
  put("features/uses-kit.tsx", 'import { Card } from "@/components/kit";\nexport const x = Card;\n');
  put("docs/design/page-template-map.md", "| Route | Template | Files | Sections |\n|---|---|---|---|\n| `/foo` | user list | `features/foo-page.tsx` | `components/minimal/table` |\n");
  put("features/foo-page.tsx", 'import { KpiCard } from "@/components/minimal/widgets";\nexport const x = KpiCard;\n');
  put("app/(admin)/foo/page.tsx", 'export default function Page() { return <div />; }\n');
  put("app/(admin)/ok/page.tsx", 'import { PageHeader } from "@/components/app/page-header";\nexport default function Page() { return <div className="kit-page"><PageHeader /></div>; }\n');
  put("app/(admin)/ok/loading.tsx", "export default function L() { return null; }\n");
  put("docs/design/page-template-map.md", [
    "| Route | Template | Files | Sections |",
    "|---|---|---|---|",
    "| `/foo` | user list | `features/foo-page.tsx` | `components/minimal/table` |",
    "| `/pastel` | Ecommerce overview | `features/pastel-page.tsx` | `components/minimal/widgets` |",
  ].join("\n"));
  put("features/pastel-page.tsx", 'import { KpiCard } from "@/components/minimal/widgets";\nexport const P = () => <KpiCard variant="tint" label="x" value={1} />;\n');
  put("components/minimal/sections/overview/demo/server-section.tsx", "export const S = () => <LinearProgress sx={[(theme) => ({ height: 8 })]} />;\n");
  const { findings } = runGuard(root, { themeDiff: false });
  const got = new Set(findings.map((f) => f.check));
  const expected = Object.keys(CHECKS).filter((c) => c !== "theme-token-drift" && c !== "brand-lock");
  const missing = expected.filter((c) => !got.has(c));
  const okPageFlagged = findings.some((f) => f.file === "app/(admin)/ok/page.tsx");
  if (findings.some((f) => f.check === "client-api-without-use-client" && f.file === "components/client-ok.tsx")) {
    console.error("design_system_self_test=FAIL client-api-without-use-client flagged a \"use client\" module");
    process.exit(1);
  }
  // A `:where(:not(.Mui…))` selector excludes MUI parts: it must not count as a MUI colour rule.
  if (findings.some((f) => f.check === "legacy-css-mui-colour" && f.line !== 1)) {
    console.error("design_system_self_test=FAIL legacy-css-mui-colour flagged a :not(.Mui…) exclusion or a transparent reset");
    process.exit(1);
  }
  const tplRatchet = findings.filter((f) => f.file === "components/minimal/tpl.tsx" && CHECKS[f.check].tier === "ratchet");
  if (tplRatchet.length) {
    console.error(`design_system_self_test=FAIL template-code ratchet not exempt: ${tplRatchet.map((f) => f.check).join(",")}`);
    process.exit(1);
  }
  rmSync(root, { recursive: true, force: true });
  if (missing.length || okPageFlagged) {
    console.error(`design_system_self_test=FAIL missing=${missing.join(",") || "none"} okPageFlagged=${okPageFlagged}`);
    process.exit(1);
  }
  console.log(`design_system_self_test=OK checks=${expected.length}`);
}

// ── Entry ─────────────────────────────────────────────────────────────────────
if (args.selfTest) {
  await selfTest();
  process.exit(0);
}
finish(runGuard(appDir, { themeDiff: !args.noThemeDiff }));

