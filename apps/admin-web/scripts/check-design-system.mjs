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

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { muiPaletteLockFindings, templateNeutralFindings } from "./lib/mui-palette-lock.mjs";
import { CHART_TEMPLATE_CHECKS, CHART_TEMPLATE_SELFTEST, chartTemplateFindings } from "./lib/chart-template-guards.mjs";
import { BRAND_LOCK, PALETTE_FILE, TOKEN_FILE, isDriftRemoval, paletteCssText, removedTokenHexes, retiredNeutralFindings, primaryStateFindings, themeLockFindings } from "./lib/design-palette.mjs";
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
import { drawerTagLines, drawerTemplateFindings, onlyTemplateDrawerWidths } from "./lib/drawer-template.mjs";
import { urlKeyedPanelFindings } from "./lib/url-keyed-panel.mjs";
import { templateHash, templateVerbatimFindings } from "./lib/template-verbatim.mjs";
import { anatomy as templateAnatomy, templateDerivedFindings } from "./lib/template-derived.mjs";
import { legacyFreeZoneFindings } from "./lib/legacy-free-zones.mjs";
import { SHRINK_RATCHET_CHECKS, shrinkRatchetFindings } from "./lib/shrink-ratchets.mjs";
import { lucideBannedFindings } from "./lib/lucide-iconify-map.mjs";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(scriptDir, "..");
const repoRoot = resolve(appDir, "../..");
const WAIVER_FILE = join(scriptDir, "check-design-system-waivers", "design-system-waivers.json");

const args = parseArgs(process.argv.slice(2));

// ── Checks ────────────────────────────────────────────────────────────────────

const SCAN_DIRS = ["app", "components", "features", "lib"];
const CODE_EXT = new Set([".tsx", ".ts"]);
const STYLE_EXT = new Set([".css"]);
// FIXJ6: the palette tokens live in theme/mesha-tokens.ts (app/mesha-theme.css + minimal-theme.css are deleted).
const THEME_FILES = new Set(["theme/mesha-tokens.ts"]);
// The kit's own styled wrapper around <select> is the ONE allowed native select.
const NATIVE_SELECT_ALLOWED = new Set();
// Licensed MUI Minimal template code copied in verbatim (Phase 2 of the MUI migration). Its sizes,
// radii and type come from the MUI theme (theme.spacing / shape / typography), not from
// app/minimal-tokens.css, so the ratchet-tier size/radius/font/tap checks do not apply there.
// Every P0 and waivable check (foreign palette, hex colours, native elements …) still does.
// components/app/sections holds template-DERIVED sections (docs/design/template-derived.json): their
// markup + sx are the template's by guard (template-derived-anatomy), so the template's own literals stay.
const TEMPLATE_CODE_DIRS = ["components/minimal/", "components/app/sections/"];
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
// rgb()/rgba() literals in TSX/TS are colours outside the theme just like hex (health-types'
// `var(--line, rgba(255,255,255,.08))` fallback, a black `rgba(0,0,0,.6)` scrim): use var(--token)
// or the theme palette with varAlpha().
const RGB_COLOUR = /rgba?\(\s*\d{1,3}\s*[,\s]\s*\d{1,3}/;
const LIGHT_SURFACE = /(?:bgcolor|backgroundColor|background)\s*:\s*["'`](?:common\.white|#fff(?:fff)?|white|grey\.(?:50|100|200))["'`]/;
const LIGHT_SURFACE_ALLOWED = new Set();
// Legacy stylesheets only shrink; a rule there that selects a MUI class and sets a colour fights the
// theme in one of the two modes. MUI colours come from the theme palette (theme/core).
const LEGACY_CSS = new Set(["app/menu-surface.css", "app/globals.css"]);
const COLOUR_DECL = /(?:^|[;{\s])(?:color|background(?:-color|-image)?|border(?:-(?:top|right|bottom|left))?(?:-color)?|fill|stroke|outline(?:-color)?)\s*:/;
// A legacy rule that paints a bare th/td/tr also paints every MUI TableCell/TableRow (element +
// class specificity beats the theme's single class), so MUI tables stop matching the template in
// one mode. New ones must exclude MUI parts: `td:not(.MuiTableCell-root)`, `tr:not(.MuiTableRow-root)`.
const TABLE_TAIL = /(?:^|[\s>+~])(?:th|td|tr)(?:\[[^\]]*\]|\.[\w-]+|:[\w-]+(?:\([^()]*\))?)*$/;
function legacyTablePaintFindings(text) {
  const out = [];
  const src = text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(src))) {
    const selector = m[1].trim();
    if (selector.startsWith("@")) continue;
    const paints = m[2].split(";").filter((decl) => COLOUR_DECL.test(` ${decl}`) && !/:\s*(?:transparent|none|0|0px|inherit|initial|unset|currentcolor)\s*(?:!important)?\s*$/i.test(decl));
    if (!paints.length) continue;
    for (const part of selector.split(",")) {
      const sel = part.trim();
      if (/Mui/.test(sel) || !TABLE_TAIL.test(sel)) continue;
      const line = src.slice(0, m.index + m[0].indexOf(selector)).split("\n").length;
      out.push({ line, snippet: sel.slice(0, 160) });
    }
  }
  return out;
}
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
  "brand-lock": { tier: "p0", why: "a locked Mesha brand/neutral token (dark or light) changed or is missing in theme/mesha-tokens.ts" },
  "non-brand-selected": { tier: "p0", why: "a primary/selected/active state must fill with var(--brand)/var(--primary) and use var(--on-brand) text" },
  "retired-neutral-literal": { tier: "p0", why: "the old Mesha green-tinted neutrals are retired (Ravi 2026-09-27): neutrals are the template greys; use the theme (background/text/divider/grey) or var(--grey-N) / rgb(var(--g500-rgb)/a)" },
  "template-neutrals": { tier: "p0", why: "theme/theme-config.ts grey + surfaces/ink and app/minimal-tokens.css --grey-N must be exactly the MUI Minimal template's values" },
  "light-surface-literal": { tier: "p0", why: "a white/near-white surface literal (common.white, #fff, grey.50-200) is a light box in dark mode; use the theme surface (Card/Paper = background.paper) or a varAlpha tint of a palette channel. Only the template AnalyticsWidgetSummary may" },
  "legacy-table-paint": { tier: "waivable", why: "a legacy stylesheet rule paints a bare th/td/tr, which also repaints MUI TableCell/TableRow; exclude MUI parts (td:not(.MuiTableCell-root)) or move the table to the template table" },
  "legacy-css-mui-colour": { tier: "p0", why: "a legacy stylesheet (frame/minimal-theme/mesha-theme/menu-surface/globals.css) selects a .Mui* class and sets a colour/background/border; MUI colours come from the theme palette only" },
  "google-fonts-link": { tier: "p0", why: "fonts are self-hosted via next/font; no Google Fonts link" },
  "native-select": { tier: "waivable", why: "use MUI TextField select / LinkSelect / template CustomPopover + MenuList" },
  "native-date-input": { tier: "waivable", why: "use kit DateRangeField" },
  "window-confirm": { tier: "waivable", why: "use kit Dialog (confirm) — never window.confirm/alert" },
  "hex-colour-in-code": { tier: "waivable", why: "use var(--token); no colour literals in TSX/TS" },
  "rgb-colour-in-code": { tier: "waivable", why: "no rgb()/rgba() colour literals in TSX/TS; use var(--token) or varAlpha(theme.vars.palette.<c>.<x>Channel, a)" },
  "hex-colour-in-css": { tier: "waivable", why: "only the two theme files may define colour literals" },
  "tailwind-palette-class": { tier: "waivable", why: "no raw Tailwind palette classes; tokens only" },
  "f2-literal": { tier: "waivable", why: "never show the legacy 'F2' code; lifecycle names only" },
  "fixed-px-width": { tier: "waivable", why: "fixed px width >= 480 cannot fit a 390px phone; use min(…, 100%) (a template drawer width on MinimalDrawer/DetailDrawer is allowed: phones get the full width)" },
  "drawer-off-template": { tier: "waivable", why: "right drawer not on the template temporary Drawer: use MinimalDrawer / DetailDrawer (portal, visible backdrop, template width 320/360/420/480, header+close, Scrollbar body, footer); wide tables scroll in DrawerTableScroll (Ravi R2-4)" },
  "progress-bar-px-min-width": { tier: "p0", why: "a template progress bar (LinearProgress) takes its length from its row/column (EcommerceSalesOverview); a raw px minWidth on the bar overflows narrow cells and drifts from the template — size the column (TableCell width %) instead" },
  "sx-hidden-full-width": { tier: "p0", why: "an sx visually-hidden box with width: 1 / height: 1 is 100% wide in MUI (1 = 100%), so an absolute live region pushes the page sideways; use visuallyHidden from @mui/utils" },
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
  "page-template-legacy-card": { tier: "p0", why: "a page listed in docs/design/page-template-map.md must not render the legacy hand-made card markup (className \"card\"/\"wchart\"/\"wtable\"/\"kpi\", <h2 className=\"h\">); every block is a template section card (Card + CardHeader) fed our data" },
  "lucide-banned": { tier: "p0", why: "lucide-react is not a dependency of admin-web: every icon is the template Iconify (`<Iconify icon=\"...\" />`, registered offline set). The one lucide -> Iconify mapping table is scripts/lib/lucide-iconify-map.mjs; the finding names the replacement (FIXJ4, J1 P1-3)" },
  "legacy-free-zone": { tier: "p0", why: "a file listed in scripts/legacy-free-zones.json (converted onto the template) has no legacy stylesheet class, no style={} prop, no native button/input/select/textarea/table, no lucide-react icon, no .css import and no hex/rgb colour literal: template/MUI components, Iconify and theme sx only (J1 P0-1/P0-2/P1-1..3/P1-5; zones only grow)" },
  "shell-nav-template": { tier: "p0", why: "the sidebar is the template NavSectionVertical/NavSectionMini inside layouts/app/dashboard nav-vertical/nav-mobile (whole nav in the template Scrollbar, template 288px mobile drawer over the template backdrop); no custom footer (navBottom / msh-foot / navigation.footer), no default-open subtrees, no full-width/opaque phone menu or extra close button" },
  "unsourced-minimal-file": { tier: "p0", why: "components/minimal/ holds template-derived code only; every file needs an entry in docs/design/template-sources.json mapping it to a Minimal template source path" },
  "template-derived-anatomy": { tier: "p0", why: "a template-derived section (docs/design/template-derived.json: demo wiring turned into props, lives in components/app/sections) keeps the template's markup and styles: its JSX element sequence and sx keys equal the template source's (recorded in the manifest; refresh with node scripts/refresh-template-derived.mjs). Only data / props may differ" },
  "template-verbatim": { tier: "p0", why: "every file mapped in docs/design/template-sources.json (components/minimal/**, layouts/**) equals its MUI Minimal template source byte-for-byte except import paths and a \"use client\" line (sha256 of the normalised template source is stored there; refresh with node scripts/refresh-template-hashes.mjs). Product behaviour (URL links, data shapes, copy) goes in components/app adapters or feature files that pass props to the verbatim template component; a hand-made component never wears a template path. Existing drift: docs/design/template-verbatim-baseline.json, shrink-only" },
  "feature-server-fn-sx": { tier: "p0", why: "a feature/app module without 'use client' passes a function sx ((theme) => …) to an MUI part: as a Server Component the function cannot cross to the client part (\"Functions cannot be passed directly to Client Components\") and the route crashes to \"Something went wrong\" (/goats/[id], FJ1 P0-1). Move the themed block into a 'use client' file or use an object sx with theme vars" },
  "section-server-fn-sx": { tier: "p0", why: "a template section under components/minimal/sections/ that styles with a function sx ((theme) => …) must start with 'use client': a Server Component page renders it, and a function prop cannot cross to the client MUI part (\"Functions cannot be passed directly to Client Components\", the whole page falls back to client rendering or 500s)" },
  "page-template-map-coverage": { tier: "p0", why: "every page under app/ (app/(admin)/**, /login, /auth/**) has a row in docs/design/page-template-map.md: a mapped row naming its template page, or a row in the \"Routes with no template page of their own\" table (redirect-only / auth) saying why. An unmapped page drifts unseen (J2 P1-2: /tasks-preview shipped a fake shell nobody mapped)" },
  "page-template-map": { tier: "p0", why: "every route row in docs/design/page-template-map.md names the feature files that render it and the template section modules they must compose; a mapped page that stops importing one of its template sections (or maps to a file that no longer exists) has drifted back to hand-made UI" },
  "legacy-kit-import": { tier: "p0", why: "the hand-built components/kit is retired; import the template (components/minimal), MUI, or a components/app behaviour wrapper instead — components/kit must not come back" },
  "server-element-prop": { tier: "p0", why: "a SERVER module passes a JSX element in an MUI prop that MUI clones (Stack divider, Tab/Chip icon, Chip avatar/deleteIcon, Checkbox/Radio checkedIcon): it arrives as a lazy RSC reference while its client chunk loads, cloneElement yields an undefined type and the page crashes on some loads (FJ1 P0-1 /goats/[goat_id]); use components/app/divided-stack (client) or move the element into a \"use client\" leaf" },
  "server-function-prop": { tier: "p0", why: "a SERVER module (reachable from an app/ page/layout/loading without crossing a \"use client\" file) passes a function sx / (theme) => callback to an MUI element: MUI parts are client components, so the render crashes with 'Functions cannot be passed directly to Client Components' (/sales/sold, digest 3801663639) while typecheck and next build stay green. Mark the module \"use client\" or use an object sx with theme tokens" },
  "client-api-without-use-client": { tier: "p0", why: "a module that calls a client-only React/Next API (useState/useEffect/useRef/useTransition/useRouter/useSearchParams/usePathname/useLinkStatus …) or wires a JSX event handler (onClick={…}) must start with \"use client\"; otherwise a server component that imports it breaks `next build` (typecheck does not catch it)" },
  "pending-dim": { tier: "p0", why: "no dimming while navigating (Ravi 2026-09-28: 'just switch and show shimmer'): a tab / segment / filter / sort / pager / path navigation swaps the affected area to its skeleton (UrlSuspense panel, the shell's pending route skeleton); nothing fades or dims the old content (no `opacity: <pending> ? …`, no CSS opacity / filter on [data-nav-pending] / [aria-busy])" },
  "hand-drawn-skeleton": { tier: "p0", why: "loading shapes come only from the shared blocks in components/app/skeletons (they render the same Card/Grid/Tabs/Table parts as the page): a loading.tsx or a features/**/*skeleton*.tsx composes those blocks and nothing else (no MUI Skeleton, no raw elements, no inline style, no legacy .skel/.kit-sk classes), and no other app code draws its own MUI Skeleton" },
  "url-keyed-panel": { tier: "p0", why: "a page with a URL-driven tab strip / segment / chip / select / pager / date filter must render its data panels through <UrlSuspense> (components/app/url-suspense.tsx) keyed by the params they read, and every such control navigates through useUrlTabNav / useUrlNavigate / announceUrlNav: the click moves the tab and swaps the panel to its skeleton in the same frame, header/tabs/filters stay mounted, content streams in (Ravi 2026-09-27: 'the tab transition HANGS')" },
  "app-wrapper-css-import": { tier: "p0", why: "components/app/ holds thin behaviour wrappers over template + MUI components only; they must not import .css / .module.css — style through the template component's props/theme instead" },
  // Charts are the template's Chart + useChart, verbatim, palette colours (scripts/lib/chart-template-guards.mjs).
  ...Object.fromEntries(Object.entries(CHART_TEMPLATE_CHECKS).map(([check, why]) => [check, { tier: "p0", why }])),
  // MUI Minimal kit + token enforcement (scripts/lib/design-kit-ratchet.mjs). Ratchet tier:
  // counted per check|file against an explicit allowance that may only shrink.
  ...Object.fromEntries(Object.entries(RATCHET_CHECKS).map(([check, why]) => [check, { tier: "ratchet", why }])),
  // J1 CI gaps (FIXJ-CI): legacy class use, reachable card shells, whole-file native controls, style
  // props, lucide icons, raw px/hex, stylesheet rule counts. Same ratchet: shrink-only per file.
  ...Object.fromEntries(Object.entries(SHRINK_RATCHET_CHECKS).map(([check, why]) => [check, { tier: "ratchet", why }])),
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

const SERVER_ENTRY = /(^|\/)(page|layout|loading|template|not-found|default)\.tsx$/;
const FUNCTION_SX = /\bsx=\{\s*\(|\bsx=\{\s*\[[^\]]*=>|\(\s*theme\s*\)\s*=>/;
// A component reference as an MUI `component` prop (`<Tab component={Link}>`) is a forwardRef object
// with a render function: from a server module it crashes the page exactly like a function sx
// ("{$$typeof, render: function}", FJ1 P0-1 /goats/[goat_id]).
// A "use client" module export is a client reference and crosses fine; a forwardRef from a server
// module (or defined in the page itself) does not. Package imports (next/link, @mui/*) are clients.
// MUI clones these element props (Stack joins `divider`; Tab / Chip / Checkbox / Radio clone `icon`,
// `avatar`, `deleteIcon`, `checkedIcon`). From a server module the element arrives as a lazy RSC
// reference while its client chunk loads, cloneElement yields an undefined type and the page shows
// "Something went wrong" on some loads only (FJ1 P0-1 /goats/[goat_id], Stack divider in the
// streamed vaccination strip). Children and directly-rendered props (startIcon, action, title) are safe.
const SERVER_CLONED_ELEMENT_PROP = /\b(divider|control|icon|avatar|deleteIcon|checkedIcon|indeterminateIcon)=\{\s*</g;
// Only the MUI parts that cloneElement the prop; our own components (EmptyState icon, …) render it as-is.
const CLONING_MUI_TAGS = new Set(["Stack", "FormControlLabel", "Chip", "Tab", "Checkbox", "Radio", "Switch", "BottomNavigationAction", "SpeedDialAction", "StepLabel", "Rating"]);
function serverClonedElementLines(text, lines) {
  const hits = [];
  for (const m of text.matchAll(SERVER_CLONED_ELEMENT_PROP)) {
    const before = text.slice(0, m.index);
    const tag = [...before.matchAll(/<([A-Z]\w*)\b/g)].at(-1)?.[1];
    if (!tag || !CLONING_MUI_TAGS.has(tag)) continue;
    const line = before.split("\n").length - 1;
    if (/^\s*(\/\/|\*|\/\*)/.test(lines[line])) continue;
    hits.push(line);
  }
  return hits;
}
const COMPONENT_REF_PROP = /\bcomponent=\{\s*([A-Z]\w*)[\w.]*\s*\}/g;
function serverComponentRefLine(text, lines, abs, resolveSpec) {
  const imported = new Map();
  for (const m of text.matchAll(/import\s+(?!type\s)([^'"`;]*?)\s+from\s*["']([^"']+)["']/g)) {
    for (const name of m[1].replace(/[{}]/g, ",").split(",").map((part) => part.trim().split(/\s+as\s+/).pop()).filter(Boolean)) imported.set(name, m[2]);
  }
  return lines.findIndex((line) => {
    if (/^\s*(\/\/|\*|\/\*)/.test(line)) return false;
    return [...line.matchAll(COMPONENT_REF_PROP)].some((m) => {
      const spec = imported.get(m[1]);
      if (!spec) return true;
      const target = resolveSpec(spec, abs);
      if (!target) return false;
      try { return !USE_CLIENT_FIRST.test(readFileSync(target, "utf8")); } catch { return false; }
    });
  });
}
function serverFunctionPropFindings(root) {
  const appDir = join(root, "app");
  if (!existsSync(appDir)) return [];
  const resolveSpec = (spec, from) => {
    let base;
    if (spec.startsWith("@/")) base = join(root, spec.slice(2));
    else if (spec.startsWith(".")) base = join(dirname(from), spec);
    else return null;
    for (const c of [base, `${base}.tsx`, `${base}.ts`, join(base, "index.tsx"), join(base, "index.ts")]) {
      if (/\.tsx?$/.test(c) && existsSync(c) && statSync(c).isFile()) return c;
    }
    return null;
  };
  const seen = new Set();
  const queue = walk(appDir).filter((f) => SERVER_ENTRY.test(f.split(sep).join("/")));
  const out = [];
  while (queue.length) {
    const abs = queue.pop();
    if (seen.has(abs)) continue;
    seen.add(abs);
    let text;
    try { text = readFileSync(abs, "utf8"); } catch { continue; }
    if (USE_CLIENT_FIRST.test(text) || /^\s*["']use server["']/.test(text)) continue;
    if (abs.endsWith(".tsx")) {
      const lines = text.split("\n");
      const at = lines.findIndex((line) => !/^\s*(\/\/|\*|\/\*)/.test(line) && FUNCTION_SX.test(line));
      if (at >= 0) out.push({ file: toRel(root, abs), line: at + 1, snippet: lines[at] });
      const ref = at >= 0 ? -1 : serverComponentRefLine(text, lines, abs, resolveSpec);
      if (ref >= 0) out.push({ file: toRel(root, abs), line: ref + 1, snippet: lines[ref] });
      for (const cloned of serverClonedElementLines(text, lines)) out.push({ file: toRel(root, abs), line: cloned + 1, snippet: lines[cloned], check: "server-element-prop" });
    }
    for (const m of text.matchAll(/(?:import|export)\s[^'"`;]*?from\s*["']([^"']+)["']/g)) {
      if (/^\s*import\s+type\s/.test(m[0]) || /^\s*export\s+type\s/.test(m[0])) continue;
      const r = resolveSpec(m[1], abs);
      if (r && !seen.has(r)) queue.push(r);
    }
  }
  return out;
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
      if (LEGACY_CSS.has(file.rel)) for (const hit of legacyTablePaintFindings(text)) findings.push(finding("legacy-table-paint", file.rel, hit.line, hit.snippet));
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
    for (const hit of drawerTemplateFindings(text, file.rel)) findings.push(finding("drawer-off-template", file.rel, hit.line, hit.snippet));
    const drawerLines = drawerTagLines(text);
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
      if (RGB_COLOUR.test(code) && !isTemplateCode(file.rel)) findings.push(finding("rgb-colour-in-code", file.rel, lineNo, raw));
      if (LIGHT_SURFACE.test(code) && !LIGHT_SURFACE_ALLOWED.has(file.rel)) findings.push(finding("light-surface-literal", file.rel, lineNo, raw));
      if (TAILWIND_PALETTE.test(code)) findings.push(finding("tailwind-palette-class", file.rel, lineNo, raw));
      if (F2_LITERAL.test(code) && !F2_ALLOWED.has(file.rel)) findings.push(finding("f2-literal", file.rel, lineNo, raw));
      if (FIXED_PX_WIDTH.test(code) && !/max-?[wW]idth|overflow/.test(code) && !(drawerLines.has(lineNo) && onlyTemplateDrawerWidths(code))) findings.push(finding("fixed-px-width", file.rel, lineNo, raw));
      if (/position:\s*["']absolute["'][^}]*\bwidth:\s*1\s*,[^}]*\bheight:\s*1\b/.test(code) || /\bwidth:\s*1\s*,\s*height:\s*1\b[^}]*clipPath/.test(code)) findings.push(finding("sx-hidden-full-width", file.rel, lineNo, raw));
      if (/<LinearProgress\b|LinearProgress[^\n]*sx=/.test(code) && /\bminWidth:\s*[1-9]\d*\b/.test(code) || (/^\s*sx=\{\{[^}]*\bheight:\s*\d+\s*,\s*minWidth:\s*[1-9]\d*/.test(code) && /LinearProgress/.test(lines.slice(Math.max(0, index - 8), index).join("\n")))) findings.push(finding("progress-bar-px-min-width", file.rel, lineNo, raw));
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

  // Chart template guards: verbatim wrapper, no data labels / states override / raw colours /
  // apex CSS, no ApexCharts outside useChart.
  {
    const scanned = files.filter((f) => !/\.(test|stories)\.(tsx?|mjs)$/.test(f.rel) && (CODE_EXT.has(f.rel.slice(f.rel.lastIndexOf("."))) || STYLE_EXT.has(f.rel.slice(f.rel.lastIndexOf(".")))));
    for (const hit of chartTemplateFindings(root, scanned)) findings.push(finding(hit.check, hit.file, hit.line, hit.snippet));
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

  // guard: server-function-prop (INTEGRATOR 2026-09-27). Walk the SERVER module graph from every
  // app/ route entry (page/layout/loading/template/not-found/default), stopping at "use client"
  // files, and refuse a function sx / (theme) => callback in any server module: it is a function
  // handed to an MUI client component. Generalises section-client-boundary to every folder.
  for (const hit of serverFunctionPropFindings(root)) findings.push(finding(hit.check ?? "server-function-prop", hit.file, hit.line, hit.snippet));

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
        const at = lines.findIndex((line) => !/^\s*(\/\/|\*)/.test(line) && /\(theme\)\s*=>|\(\)\s*=>\s*\(\{|\buse(?:Theme|State|Callback|Effect|Memo|Chart)\(/.test(line));
        if (at >= 0) findings.push(finding("section-client-boundary", toRel(root, abs), at + 1, lines[at]));
      }
    }
  }
  // ...nor keep the legacy hand-made card shells (`section.card.wchart` + `h2.h`) the template
  // section cards replaced (R3: "the cards are hand-made"). Styling them lives in frame.css /
  // mesha-theme.css, which is exactly the legacy CSS a mapped page must stop depending on.
  for (const hit of pageTemplateLegacyCardFindings(root)) findings.push(finding("page-template-legacy-card", hit.file, hit.line, hit.snippet));

  // J1 CI gaps: shrink-only per-file counts over every file (scripts/lib/shrink-ratchets.mjs). The
  // verbatim template (components/minimal, components/app/sections, layouts mapped in
  // docs/design/template-sources.json or template-derived.json) is exempt.
  {
    const docs = existsSync(join(root, "docs", "design", "template-sources.json")) ? join(root, "docs", "design") : resolve(root, "../../docs/design");
    const mapped = new Set();
    for (const name of ["template-sources.json", "template-derived.json"]) {
      try {
        const j = JSON.parse(readFileSync(join(docs, name), "utf8"));
        for (const k of Object.keys(j.sources ?? j.files ?? {})) mapped.add(k);
      } catch {}
    }
    const isExempt = (rel) => isTemplateCode(rel) || (rel.startsWith("layouts/") && mapped.has(rel));
    for (const hit of shrinkRatchetFindings(root, { isExempt })) findings.push(finding(hit.check, hit.file, hit.line, hit.snippet));
  }

  // FIXJ4 (J1 P1-3): lucide-react is gone for good. Any import of it (a file) or a dependency entry
  // (apps/admin-web/package.json) fails p0, naming the template Iconify replacement.
  for (const hit of lucideBannedFindings(root)) findings.push(finding("lucide-banned", hit.file, hit.line, hit.snippet));

  // J1: files moved onto the template stay there (scripts/lib/legacy-free-zones.mjs).
  for (const hit of legacyFreeZoneFindings(root)) findings.push(finding("legacy-free-zone", hit.file, hit.line, hit.snippet));

  // A URL-driven control over data rendered straight into the tree holds the old page on screen for
  // the whole round trip (the "tab transition HANGS" report): see scripts/lib/url-keyed-panel.mjs.
  for (const hit of urlKeyedPanelFindings(root)) findings.push(finding("url-keyed-panel", hit.file, hit.line, hit.snippet));

  // R2 item 5: the sidebar must stay the template NavSection (see shellNavTemplateFindings).
  for (const hit of shellNavTemplateFindings(root)) findings.push(finding("shell-nav-template", hit.file, hit.line, hit.snippet));

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

  // template-verbatim (Ravi 2026-09-27 "use the SAME mesha-ui template across the pages and just put
  // our content"): mapped files equal the template source except import paths / "use client".
  {
    const localDocs = join(root, "docs", "design");
    const docsDir = existsSync(join(localDocs, "template-sources.json")) ? localDocs : resolve(root, "../../docs/design");
    for (const hit of templateVerbatimFindings(root, join(docsDir, "template-sources.json"), join(docsDir, "template-verbatim-baseline.json"))) {
      findings.push(finding("template-verbatim", hit.file, hit.line, hit.snippet));
    }
    for (const hit of templateDerivedFindings(root, join(docsDir, "template-derived.json"))) {
      findings.push(finding("template-derived-anatomy", hit.file, hit.line, hit.snippet));
    }
  }

  // Template sections are rendered straight from Server Component pages. A function sx
  // ((theme) => …) inside a section that is not a client module is serialized as a prop to the
  // client MUI part and throws at render ("Functions cannot be passed directly to Client
  // Components"), so such a section must be 'use client'.
  {
    const sectionsDir = join(root, "components", "minimal", "sections");
    if (existsSync(sectionsDir)) {
      for (const abs of walk(sectionsDir)) {
        const rel = toRel(root, abs);
        if (!/\.tsx$/.test(rel)) continue;
        const text = readFileSync(abs, "utf8");
        if (/^\s*['"]use client['"]/.test(text)) continue;
        const lines = text.split("\n");
        const at = lines.findIndex((line) => /\(\s*\{?\s*theme\s*\}?\s*\)\s*=>/.test(line));
        if (at >= 0) findings.push(finding("section-server-fn-sx", rel, at + 1, lines[at]));
      }
    }
  }

  // Feature / app modules without 'use client' are Server Components when a page renders them; a
  // function sx there crashes the route at render (the /goats/[id] P0). Same test as the section
  // rule, applied to features/** and app/**.
  for (const dir of ["features", "app"]) {
    const base = join(root, dir);
    if (!existsSync(base)) continue;
    for (const abs of walk(base)) {
      const rel = toRel(root, abs);
      if (!/\.tsx$/.test(rel)) continue;
      const text = readFileSync(abs, "utf8");
      if (/^\s*(\/\/[^\n]*\n\s*)*['"]use client['"]/.test(text)) continue;
      const lines = text.split("\n");
      const at = lines.findIndex((line) => /sx=\{\s*\[?\s*\(\s*\{?\s*(theme|t)\s*\}?(\s*:\s*Theme)?\s*\)\s*=>/.test(line));
      if (at >= 0) findings.push(finding("feature-server-fn-sx", rel, at + 1, lines[at]));
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
          const text = readFileSync(abs, "utf8");
          imports += text;
          // components/app adapters only pass our data into template sections (e.g. kpi-widget ->
          // CourseWidgetSummary / EcommerceWidgetSummary), so their imports count one level deep.
          // Two levels, so an adapter folder's index re-exporting its own files (./calendar-root) counts.
          const follow = (source, fromDir, depth) => {
            const specs = [...source.matchAll(/from\s+["'](@\/components\/app\/[\w/.-]+|\.\.?\/[\w/.-]+)["']/g)].map((m) => m[1]);
            for (const spec of specs) {
              if (spec.startsWith(".") && !fromDir) continue;
              const base = spec.startsWith("@/") ? join(root, spec.slice(2)) : join(fromDir, spec);
              for (const ext of [".tsx", ".ts", "/index.ts", "/index.tsx"]) {
                const adapter = base + ext;
                if (!existsSync(adapter)) continue;
                const body = readFileSync(adapter, "utf8");
                imports += body;
                if (depth > 1) follow(body, dirname(adapter), depth - 1);
              }
            }
          };
          follow(text, null, 2);
        }
        for (const mod of modules) {
          const spec = new RegExp(`from\\s+["']@/${mod.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?:["'/])`);
          if (!spec.test(imports)) findings.push(finding("page-template-map", mapRel, index + 1, `${route}: none of ${files.join(", ")} imports @/${mod}`));
        }
      });
      // page-template-map-coverage: every page.tsx under app/ has a row (mapped or declared unmapped).
      const listed = new Set();
      for (const line of readFileSync(mapFile, "utf8").split("\n")) {
        const first = line.split("|")[1]?.trim() ?? "";
        for (const m of first.matchAll(/`(\/[^`]*)`/g)) listed.add(m[1]);
      }
      const appRoot = join(root, "app");
      const pages = [];
      const walkPages = (dir, segs) => {
        if (!existsSync(dir)) return;
        for (const name of readdirSync(dir)) {
          const full = join(dir, name);
          if (statSync(full).isDirectory()) {
            if (name.startsWith("_") || name.startsWith("@") || (segs.length === 0 && name === "api")) continue;
            walkPages(full, /^\(.*\)$/.test(name) ? segs : [...segs, name]);
          } else if (/^page\.(tsx|ts|jsx|js)$/.test(name)) pages.push({ route: `/${segs.join("/")}`, file: relative(root, full) });
        }
      };
      walkPages(appRoot, []);
      for (const page of pages) {
        if (!listed.has(page.route)) findings.push(finding("page-template-map-coverage", mapRel, 1, `${page.route} (${page.file}) has no row in page-template-map.md`));
      }
    }
  }

  // Loading shapes (R2 item 7): every route loading.tsx and panel fallback composes the shared
  // blocks in components/app/skeletons, which render the page's own layout parts. A hand-drawn
  // placeholder (its own MUI Skeleton, raw divs with inline sizes, the retired .skel/.kit-sk CSS) is
  // how every page ended up shimmering a shape unlike the content it loads.
  {
    const MUI_SKELETON_IMPORT = /from\s+["']@mui\/material\/Skeleton["']|import\s*\{[^}]*\bSkeleton\b[^}]*\}\s*from\s+["']@mui\/material["']/;
    const LEGACY_SKEL_CLASS = /className=["'{`][^"'}`]*(?<![\w-])(?:skel|skelrow|kit-sk-[\w-]+|kit-skeleton)(?![\w-])/;
    const INTRINSIC_JSX = /<(?:div|span|section|article|ul|li|p|header|aside|table|tr|td|th)\b/;
    const isComposition = (rel) => /^app\/\(admin\)\/.*\/loading\.tsx$/.test(rel) || /^features\/.*skeleton[\w-]*\.tsx$/.test(rel);
    // The blocks themselves, the template code, and the shell chrome placeholder (sidebar + top bar,
    // not a page shape) may use MUI Skeleton directly.
    const mayDrawSkeleton = (rel) => rel.startsWith("components/app/skeletons/") || rel.startsWith("components/minimal/") || rel === "components/shell-skeleton.tsx";
    for (const abs of SCAN_DIRS.flatMap((dir) => walk(join(root, dir)))) {
      const rel = toRel(root, abs);
      if (!/\.tsx?$/.test(rel) || /\.(test|stories)\.tsx?$/.test(rel)) continue;
      const lines = readFileSync(abs, "utf8").split("\n");
      const composition = isComposition(rel);
      lines.forEach((line, index) => {
        const code = line.replace(/\/\/.*$/, "");
        if (!mayDrawSkeleton(rel) && MUI_SKELETON_IMPORT.test(code)) findings.push(finding("hand-drawn-skeleton", rel, index + 1, line));
        else if (LEGACY_SKEL_CLASS.test(code)) findings.push(finding("hand-drawn-skeleton", rel, index + 1, line));
        else if (composition && (INTRINSIC_JSX.test(code) || /\bstyle=\{/.test(code))) findings.push(finding("hand-drawn-skeleton", rel, index + 1, line));
      });
    }
  }

  // pending-dim (Ravi 2026-09-28, "just switch and show shimmer"): a pending navigation never dims or
  // fades the old content; the affected area shows its skeleton. Flags an opacity / filter tied to a
  // pending / busy flag in TS(X), and CSS opacity / filter on [data-nav-pending] or [aria-busy].
  {
    const TSX_DIM = /\b(?:opacity|filter)\s*:\s*\(?\s*(?:[\w.?]*\b(?:is)?[pP]ending\b|[\w.?]*\b(?:is)?[bB]usy\b|[\w.?]*navPending\b)[\w.?]*\s*\)?\s*\?/;
    // REVIEW-42 O65: nothing sets the retired page-column dim hook at all.
    const NAV_PENDING_ATTR = /setAttribute\(\s*["']data-nav-pending["']|data-nav-pending=/;
    const TSX_SPREAD_DIM = /\b(?:is)?[pP]ending\b[^?\n]*\?\s*\{[^}]*\bopacity\s*:/;
    // REVIEW-43: a feature toggling the retired `.wfbusy` hold class keeps the old rows on screen,
    // clickable, while the next answer loads; the area shows its skeleton instead.
    const FEATURE_WFBUSY = /\?\s*["'`]\s*wfbusy\b|["'`]\s*wfbusy\s*["'`]\s*:/;
    // CSS: a pending-navigation hook ([data-nav-pending], a busy page root under .wrap) that lowers
    // opacity or filters. `opacity:1` resets (skeleton roots painting at once) are not dims.
    const CSS_DIM = /(?:\[data-nav-pending[^\]]*\]|\.wrap[^{,]*\[aria-busy[^\]]*\]|\.[\w-]*busy\b[^{,]*)[^{]*\{[^}]*(?:opacity\s*:\s*(?:0?\.\d|0\b)|filter\s*:\s*[^;}]*opacity)/;
    for (const abs of SCAN_DIRS.flatMap((dir) => walk(join(root, dir)))) {
      const rel = toRel(root, abs);
      if (rel.startsWith("components/minimal/") || /\.(test|stories)\.[tj]sx?$/.test(rel)) continue;
      const isCss = /\.css$/.test(rel);
      if (!isCss && !/\.tsx?$/.test(rel)) continue;
      readFileSync(abs, "utf8").split("\n").forEach((line, index) => {
        const code = isCss ? line : line.replace(/\/\/.*$/, "");
        if (isCss ? CSS_DIM.test(code) : TSX_DIM.test(code) || TSX_SPREAD_DIM.test(code) || NAV_PENDING_ATTR.test(code) || (rel.startsWith("features/") && FEATURE_WFBUSY.test(code))) findings.push(finding("pending-dim", rel, index + 1, line));
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

  // Brand lock: the palette values from docs/design/README.md must exist verbatim in the palette file
  // (theme/mesha-tokens.ts since FIXJ6; it is required).
  {
    const paletteText = paletteCssText(root);
    if (!paletteText) findings.push(finding("brand-lock", PALETTE_FILE, 1, `${PALETTE_FILE} is missing: the locked Mesha palette has no source`));
    else {
      const upper = paletteText.toUpperCase();
      for (const locked of BRAND_LOCK) if (!upper.includes(locked)) findings.push(finding("brand-lock", PALETTE_FILE, 1, `locked colour ${locked} is missing`));
      for (const msg of themeLockFindings(paletteText)) findings.push(finding("brand-lock", PALETTE_FILE, 1, msg));
    }
  }

  // MUI Minimal theme (theme/, layouts/ — template-derived, Minimal v7.7.0 next-ts). Those files are
  // allowed template code and are NOT in SCAN_DIRS, but their palette is still the locked Mesha one:
  // brand/surface hexes must match theme/mesha-tokens.ts and no Minimal default brand hex may survive.
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
        for (const value of removedTokenHexes(patch)) removedValues.add(value);
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
          // Shrink only (FIXJ-CI): keep a waiver that is still listed AND still found; a NEW finding is
          // never waived by --update-baseline (it used to be, which silently grew the list).
          waived: nextWaivers(waivable, waived),
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
    // Stale waivers and ratchet slack FAIL (FIXJ-CI, Ravi 2026-09-30 "a baseline can only go down"):
    // paid-down debt must leave the baseline in the same change, or the slack lets it grow back.
    failures: p0.length + unwaived.length + ratchet.over.length + stale.length + ratchet.shrinkable.length,
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
  if (stale.length > 0 && !args.updateBaseline) {
    console.error(`!! ${stale.length} stale waiver(s): debt paid down but still listed. Shrink the list in this change: npm run design:guard:update-baseline`);
    for (const key of stale.slice(0, 20)) console.error(`   ${key}`);
  }
  if (ratchet.shrinkable.length > 0 && !args.updateBaseline) {
    console.error(`!! ${ratchet.shrinkable.length} ratchet allowance(s) above the current count: lower them in this change (npm run design:guard:update-baseline):`);
    for (const s of ratchet.shrinkable.slice(0, 20)) console.error(`   ${s.key} ${s.allowed} -> ${s.count}`);
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

/** --update-baseline waiver list: still listed AND still found. Never adds a key (shrink only). */
function nextWaivers(waivable, waived) {
  return [...new Set(waivable.filter((f) => waived.has(f.key)).map((f) => f.key))].sort();
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
      // Comments (block, JSX {/* */} and line) never count; newlines are kept so line numbers hold.
      const code = source.text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " ")).replace(/(^|[^:])\/\/.*$/gm, "$1");
      code.split("\n").forEach((line, index) => {
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
const LEGACY_CARD_MARKUP = /className=\{?["'`](?:card|wchart|wtable|kpi|chartcard)(?:[\s"'`]|$)|className=\{?["'`][^"'`]*\b(?:wchart|wtable)\b|<h2 className=["']h["']/;
function pageTemplateLegacyCardFindings(root) {
  const local = join(root, "docs", "design", "page-template-map.md");
  const mapFile = existsSync(local) ? local : resolve(root, "../../docs/design/page-template-map.md");
  if (!existsSync(mapFile)) return [];
  const out = [];
  for (const row of pageTemplateRows(readFileSync(mapFile, "utf8"))) {
    for (const rel of row.files) {
      if (!existsSync(join(root, rel))) continue; // a missing file is `page-template-map`'s finding
      readFileSync(join(root, rel), "utf8").split("\n").forEach((line, index) => {
        if (/^\s*(\/\/|\*)/.test(line)) return;
        if (LEGACY_CARD_MARKUP.test(line)) {
          out.push({ file: rel, line: index + 1, snippet: `${row.route}: legacy hand-made card markup instead of a template section card: ${line.trim()}` });
        }
      });
    }
  }
  return out;
}
// R2 item 5 (Ravi): the sidebar drifted from the template (custom "Mesha · goat operating system"
// footer, every default_open subtree expanded, full-width opaque phone menu with its own close
// button). layouts/app/dashboard nav-vertical / nav-mobile must render the template NavSectionVertical
// (and NavSectionMini) inside the template Scrollbar, and none of the shell deviations may return.
function shellNavTemplateFindings(root) {
  const dash = join(root, "layouts", "app", "dashboard");
  if (!existsSync(dash)) return [];
  const out = [];
  const read = (rel) => (existsSync(join(root, rel)) ? readFileSync(join(root, rel), "utf8") : null);
  const requireIn = (rel, pattern, what) => {
    const text = read(rel);
    if (text === null) out.push({ file: rel, line: 1, snippet: `missing: the shell nav file must exist to render the template ${what}` });
    else if (!pattern.test(text)) out.push({ file: rel, line: 1, snippet: `must render the template ${what}` });
  };
  requireIn("layouts/app/dashboard/nav-vertical.tsx", /<NavSectionVertical\b/, "NavSectionVertical");
  requireIn("layouts/app/dashboard/nav-vertical.tsx", /<NavSectionMini\b/, "NavSectionMini");
  requireIn("layouts/app/dashboard/nav-vertical.tsx", /<Scrollbar fillContent>/, "Scrollbar (the whole nav scrolls, logo fixed)");
  requireIn("layouts/app/dashboard/nav-mobile.tsx", /<NavSectionVertical\b/, "NavSectionVertical");
  requireIn("layouts/app/dashboard/nav-mobile.tsx", /<Scrollbar fillContent>/, "Scrollbar");
  const banned = [
    [/\bnavBottom\b|msh-foot|navigation\.footer/, "custom nav footer (the template has only the optional NavUpgrade card, which we do not use)"],
    [/\bdefaultOpen\b|default_open/, "default-open nav subtree (template opens only the active group)"],
    [/data-nav-close/, "extra close button in the phone nav drawer (template drawer closes on backdrop / Escape / Back)"],
    [/100vw/, "full-width phone nav drawer (template width is var(--layout-nav-mobile-width))"],
    [/backdrop:\s*\{\s*sx:/, "custom phone nav backdrop (template backdrop)"],
  ];
  const files = ["components/mesha-shell.tsx", ...["layout.tsx", "nav-vertical.tsx", "nav-mobile.tsx"].map((f) => `layouts/app/dashboard/${f}`)];
  const navSection = join(root, "layouts", "template", "nav-section");
  if (existsSync(navSection)) files.push(...walk(navSection).map((abs) => toRel(root, abs)));
  for (const rel of files) {
    const text = read(rel);
    if (text === null) continue;
    text.split("\n").forEach((line, index) => {
      if (/^\s*(\/\/|\*|\/\*)/.test(line)) return;
      for (const [pattern, what] of banned) if (pattern.test(line)) out.push({ file: rel, line: index + 1, snippet: `${what}: ${line.trim()}` });
    });
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
    '<Box component="p" role="status" sx={{ position: "absolute", width: 1, height: 1, overflow: "hidden", clipPath: "inset(50%)" }} />',
    '<span className="chip ok">Done</span>',
    '<input type="checkbox" checked={on} />',
    '<div className="alert warn">Careful</div>',
    '<span className="av">RT</span>',
    '<div style={{ fontSize: 13, borderRadius: 10, boxShadow: "0 2px 4px red", padding: 12 }} />',
    '<div style={{ position: "fixed", inset: 0 }} />',
    '<div className="parkmenu" role="menu" />',
    'import { LineChart } from "recharts";',
    '<Card sx={{ backgroundColor: "common.white" }} />',
    '<div style={{ background: "rgba(0,0,0,.6)" }} />',
    '<LinearProgress variant="determinate" value={v} sx={{ height: 8, minWidth: 80 }} />',
  ].join("\n"));
  put("app/globals.css", ".wrap .MuiCard-root{background:var(--paper)}\n.fld label:where(:not(.MuiFormLabel-root)){color:var(--muted)}\n.MuiInputBase-input{border:0;background-color:transparent}\n.main th{color:var(--fg-muted)}\n.main td:not(.MuiTableCell-root){border-bottom:1px dashed var(--line)}\n");
  put("components/bad.css", ".x { color: #abcdef; }\n.g{background:#0E1512}\n.y{padding:12px;border-radius:10px;box-shadow:0 4px 8px black;font-size:13px}\n@media (max-width:600px){\n.btn{min-height:32px}\n}\n.metricseg a.on{background:var(--paper)}\n");
  // A template drawer width on the template drawer is allowed (no fixed-px-width, no drawer finding).
  put("features/dim-bad.tsx", 'export const X = ({ isPending }) => <Box sx={{ opacity: isPending ? 0.6 : 1 }} />;\n');
  put("features/dim-attr.tsx", 'export const Z = (el) => el.setAttribute("data-nav-pending", "true");\n');
  put("features/dim-wfbusy.tsx", 'export const W = ({ busy }) => <div className={`tblwrap${busy ? " wfbusy" : ""}`} />;\n');
  put("scripts/legacy-free-zones.json", JSON.stringify({ zones: ["features/zone-bad.tsx"] }));
  put("features/zone-bad.tsx", 'export const Q = () => <div style={{ padding: 3 }}><button>x</button></div>;\n');
  put("features/dim-ok.tsx", 'export const Y = ({ flashing }) => <Box sx={{ opacity: flashing ? 1 : 0 }} />;\n');
  put("app/dim-bad.css", '.wrap[data-nav-pending]>*{filter:opacity(.6)}\n.x-busy tbody{opacity:.6}\n');
  put("features/ok-drawer.tsx", [
    '"use client";',
    'import { MinimalDrawer } from "@/components/app/drawer";',
    'export const d = (',
    '  <MinimalDrawer',
    '    open={open}',
    '    onClose={close}',
    '    title="Tag animals to sale"',
    '    width={480}',
    '  >',
    '    <div />',
    '  </MinimalDrawer>',
    ');',
  ].join("\n"));
  // Off-template drawer widths / transparent backdrop / raw MUI Drawer are each caught.
  put("features/bad-drawer.tsx", [
    'const DRAWER_WIDTH = 380;',
    'export const a = <MinimalDrawer open onClose={close} title="x" width={DRAWER_WIDTH} />;',
    'export const b = <MinimalDrawer open onClose={close} title="x" width={640} />;',
    'export const c = <DetailDrawer open onClose={close} title="x" closeLabel="Close" size="xl" />;',
    'export const e = <MinimalDrawer open onClose={close} title="x" invisibleBackdrop />;',
    'export const f = <Drawer open={open} onClose={close} PaperProps={{ sx: { width: 420 } }} />;',
    'export const g = <Drawer anchor="left" open={open} onClose={close} />;',
    'export const h = <aside className={`drawer${open ? " on" : ""}`} aria-label="Tag detail" />;',
  ].join("\n"));
  // Template code: ratchet-tier sizes are exempt, a foreign palette is still P0.
  put("features/server-fn-sx-page.tsx", 'import Box from "@mui/material/Box";\nexport function P() { return <Box sx={(theme) => ({ color: theme.palette.text.primary })} />; }\n');
  put("components/minimal/sections/order/server-fn-sx.tsx", 'import Box from "@mui/material/Box";\nexport const X = () => <Box sx={(theme) => ({ color: theme.palette.text.primary })} />;\n');
  put("components/minimal/tpl.tsx", 'const t = <div style={{ fontSize: 13, borderRadius: 10, padding: 12 }} />;\n');
  put("components/app/bad.tsx", 'import styles from "./bad.module.css";\nexport function Bad() { return <div className={styles.x} />; }\n');
  put("components/server-hook.ts", 'export { useLinkStatus } from "next/link";\n');
  put("components/client-ok.tsx", '"use client";\nimport { useState } from "react";\nexport function Ok() { const [a] = useState(0); return <b onClick={() => a}>x</b>; }\n');
  put("components/kit/lonely.tsx", "export function Lonely() { return null; }\n");
  put("features/uses-kit.tsx", 'import { Card } from "@/components/kit";\nexport const x = Card;\n');
  put("docs/design/page-template-map.md", "| Route | Template | Files | Sections |\n|---|---|---|---|\n| `/foo` | user list | `features/foo-page.tsx` | `components/minimal/table` |\n");
  put("features/foo-page.tsx", 'import { KpiCard } from "@/components/minimal/widgets";\nexport const x = KpiCard;\n');
  put("app/(admin)/foo/page.tsx", 'export default function Page() { return <div />; }\n');
  put("app/(admin)/ok/page.tsx", 'import { PageHeader } from "@/components/app/page-header";\nimport { PageRoot } from "@/components/app/page-root";\nexport default function Page() { return <PageRoot><PageHeader /></PageRoot>; }\n');
  put("app/(admin)/ok/loading.tsx", "export default function L() { return null; }\n");
  put("app/(admin)/drawn/loading.tsx", 'import Skeleton from "@mui/material/Skeleton";\nexport default function L() { return <div style={{ height: 40 }}><Skeleton /></div>; }\n');
  put("app/(admin)/composed/loading.tsx", 'import { PageSkeleton, TableSkeleton } from "@/components/app/skeletons";\nexport default function L() { return <PageSkeleton><TableSkeleton columns={4} /></PageSkeleton>; }\n');
  put("docs/design/page-template-map.md", [
    "| Route | Template | Files | Sections |",
    "|---|---|---|---|",
    "| `/foo` | user list | `features/foo-page.tsx` | `components/minimal/table` |",
    "| `/pastel` | Ecommerce overview | `features/pastel-page.tsx` | `components/minimal/widgets` |",
    "| `/legacy` | Ecommerce overview | `features/legacy-card-page.tsx` | `components/minimal/widgets` |",
  ].join("\n"));
  put("features/legacy-card-page.tsx", 'import { EcommerceWidgetSummary } from "@/components/minimal/widgets";\nexport const L = () => <section className="card wchart"><h2 className="h">x</h2><EcommerceWidgetSummary title="x" total={1} /></section>;\n');
  put("features/pastel-page.tsx", 'import { KpiCard } from "@/components/minimal/widgets";\nexport const P = () => <KpiCard variant="tint" label="x" value={1} />;\n');
  put("components/minimal/sections/overview/demo/server-section.tsx", "export const S = () => <LinearProgress sx={[(theme) => ({ height: 8 })]} />;\n");
  // zero-arg sx callback (template BookingWidgetSummary `sx={[() => ({ p: 2 }), ...]}`) crashed /counts/mortality
  put("components/minimal/sections/overview/demo/server-zero-arg-sx.tsx", "export const Z = ({ sx }) => <Card sx={[() => ({ p: 2 }), sx]} />;\n");
  put("layouts/app/dashboard/nav-vertical.tsx", "export const V = () => <Scrollbar fillContent><NavSectionVertical data={d} /></Scrollbar>;\n");
  put("layouts/app/dashboard/nav-mobile.tsx", "export const M = () => <Drawer slotProps={{ backdrop: { sx: { bgcolor: 'var(--bg)' } }, paper: { sx: { width: '100vw' } } }}><NavSectionVertical data={d} /></Drawer>;\n");
  put("layouts/app/dashboard/layout.tsx", 'export const L = () => <NavMobile slots={{ bottomArea: navBottom }} />;\n');
  for (const [rel, text] of Object.entries(CHART_TEMPLATE_SELFTEST)) put(rel, text);
  // url-keyed-panel: a page whose feature (through the barrel) renders a URL strip with no UrlSuspense
  // is caught; the same page with its panel inside UrlSuspense is not.
  put("features/tabbed/index.ts", 'export { TabbedPage } from "./tabbed-page";\nexport { KeyedPage } from "./keyed-page";\n');
  put("features/tabbed/tabbed-page.tsx", 'import { SegmentedLinks } from "@/components/segmented-links";\nexport async function TabbedPage() { const d = await read(); return <><SegmentedLinks options={[]} /><Card>{d}</Card></>; }\n');
  put("features/tabbed/keyed-page.tsx", 'import { SegmentedLinks } from "@/components/segmented-links";\nimport { UrlSuspense } from "@/components/app/url-suspense";\nexport function KeyedPage({ sp }) { return <><SegmentedLinks options={[]} /><UrlSuspense searchParams={sp} watch={["tab"]} fallback={null}><Panel /></UrlSuspense></>; }\n');
  put("app/(admin)/tabbed/page.tsx", 'import { TabbedPage } from "@/features/tabbed";\nexport default function Page() { return <TabbedPage />; }\n');
  put("app/(admin)/tabbed/loading.tsx", "export default function L() { return null; }\n");
  put("app/(admin)/keyed/page.tsx", 'import { KeyedPage } from "@/features/tabbed";\nexport default function Page() { return <KeyedPage />; }\n');
  put("app/(admin)/keyed/loading.tsx", "export default function L() { return null; }\n");
  // server-function-prop: a page renders a server module with a function sx (flagged); a client
  // module with the same sx and a server module reached only through it are not.
  put("app/(admin)/sfp/page.tsx", 'import { Bars } from "@/features/sfp/bars";\nimport { ClientBox } from "@/features/sfp/client-box";\nexport default function Page() { return <div className="kit-page"><Bars /><ClientBox /></div>; }\n');
  put("app/(admin)/sfp/loading.tsx", "export default function L() { return null; }\n");
  put("features/sfp/bars.tsx", 'import LinearProgress from "@mui/material/LinearProgress";\nexport const Bars = () => <LinearProgress sx={(theme) => ({ height: 8 })} />;\n');
  put("features/sfp/client-box.tsx", '"use client";\nimport { Inner } from "./inner";\nexport const ClientBox = () => <Inner />;\n');
  put("features/sfp/inner.tsx", 'import Box from "@mui/material/Box";\nexport const Inner = () => <Box sx={[(theme) => ({ p: 1 })]} />;\n');
  // ...and a server module handing Tab a forwardRef from a NON-client module (flagged, FJ1 P0-1),
  // while a "use client" link export and a package component stay clean.
  put("app/(admin)/sfp2/page.tsx", 'import { TabsRow } from "@/features/sfp/tabs-row";\nexport default function Page() { return <div className="kit-page"><TabsRow /></div>; }\n');
  put("app/(admin)/sfp2/loading.tsx", "export default function L() { return null; }\n");
  put("features/sfp/tabs-row.tsx", 'import Tab from "@mui/material/Tab";\nimport NextLink from "next/link";\nimport ClientLink from "./client-link";\nimport ServerLink from "./server-link";\nexport const TabsRow = () => (\n  <>\n    <Tab component={NextLink} href="/a" />\n    <Tab component={ClientLink} href="/b" />\n    <Tab component={ServerLink} href="/c" />\n  </>\n);\n');
  put("features/sfp/client-link.tsx", '"use client";\nimport { forwardRef } from "react";\nexport default forwardRef<HTMLAnchorElement>(function L(p, ref) { return <a ref={ref} {...p} />; });\n');
  put("features/sfp/server-link.tsx", 'import { forwardRef } from "react";\nexport default forwardRef<HTMLAnchorElement>(function L(p, ref) { return <a ref={ref} {...p} />; });\n');
  // server-element-prop: a server module hands Stack a divider element (flagged, /goats/[goat_id]);
  // the same prop in a "use client" module, and an element as children, stay clean.
  put("app/(admin)/sep/page.tsx", 'import { Rows } from "@/features/sep/rows";\nimport { ClientRows } from "@/features/sep/client-rows";\nexport default function Page() { return <div className="kit-page"><Rows /><ClientRows /></div>; }\n');
  put("app/(admin)/sep/loading.tsx", "export default function L() { return null; }\n");
  put("features/sep/rows.tsx", 'import Stack from "@mui/material/Stack";\nimport Divider from "@mui/material/Divider";\nexport const Rows = () => (\n  <Stack>\n    <Divider />\n    <Stack divider={<Divider />} />\n  </Stack>\n);\n');
  put("features/sep/client-rows.tsx", '"use client";\nimport Stack from "@mui/material/Stack";\nimport Divider from "@mui/material/Divider";\nexport const ClientRows = () => <Stack divider={<Divider />} />;\n');
  // template-verbatim: tpl-ok.tsx differs only by import path + "use client" (clean); tpl-drift.tsx
  // changed a copy string (flagged); tpl-pkg.tsx swapped a package import (flagged); tpl-listed.tsx
  // drifts but is in the baseline with its current sha256 (allowed); tpl-healed.tsx is in the baseline
  // yet verbatim (flagged); tpl-edited.tsx is baselined but its bytes changed since (flagged).
  {
    const tplSrc = "import Box from '@mui/material/Box';\n\nimport { fNumber } from 'src/utils/format-number';\n\nexport const W = ({ n }: { n: number }) => <Box>{fNumber(n)} last week</Box>;\n";
    const tplHash = templateHash(tplSrc);
    const imports = ["@mui/material/Box", "src/utils/format-number"];
    const mine = tplSrc.replace("src/utils/format-number", "@/components/minimal/_shared/format-number");
    put("components/minimal/tpl-ok.tsx", `'use client';\n\n${mine}`);
    put("components/minimal/tpl-drift.tsx", mine.replace("last week", "this period"));
    put("components/minimal/tpl-pkg.tsx", mine.replace("@mui/material/Box", "@/components/app/box"));
    put("components/minimal/tpl-listed.tsx", mine.replace("last week", "custom"));
    put("components/minimal/tpl-healed.tsx", mine);
    put("components/minimal/tpl-edited.tsx", mine.replace("last week", "custom, then hand-edited"));
    const files = ["tpl-ok", "tpl-drift", "tpl-pkg", "tpl-listed", "tpl-healed", "tpl-edited"].map((n) => `components/minimal/${n}.tsx`);
    put("docs/design/template-sources.json", JSON.stringify({
      sources: Object.fromEntries(files.map((f) => [f, "src/sections/demo/w.tsx"])),
      verbatim: Object.fromEntries(files.map((f) => [f, { sha256: tplHash, imports }])),
    }));
    const sha = (text) => createHash("sha256").update(text).digest("hex");
    {
      // REVIEW-23 O29: the reviewer's four scratch edits (height 320->200, px 3->1, Chart type
      // bar->line, an added style) must each fail; a data-only edit passes.
      const tplDerived = "export const W = ({ v }) => <Card sx={{ p: 3 }}><CardHeader title={v.title} sx={{ px: 3 }} /><Box sx={{ typography: 'h3' }}>{v.total}</Box><Chart type=\"bar\" sx={{ height: 320 }} /></Card>;\n";
      const a = templateAnatomy(tplDerived);
      const entry = { source: "src/sections/demo/w.tsx", replaced: "total formatting", ...a };
      const variants = {
        "derived-ok": tplDerived.replace("v.total", "fmt(v.total)"),
        "derived-drift": tplDerived.replace("<Box sx={{ typography: 'h3' }}>", "<Box sx={{ typography: 'h3', color: 'red' }}><span>").replace("</Box>", "</span></Box>"),
        "derived-height": tplDerived.replace("height: 320", "height: 200"),
        "derived-px": tplDerived.replace("px: 3", "px: 1"),
        "derived-type": tplDerived.replace('type="bar"', 'type="line"'),
        "derived-style": tplDerived.replace("<Box sx=", "<Box style={{ color: 'red' }} sx="),
        // REVIEW-25: a declared override (slotProps.row on the Box) passes; an undeclared slot name
        // (slotProps.label) fails; an added prop on a template element (disabled) fails.
        "derived-override": tplDerived.replace("<Box sx={{ typography: 'h3' }}>", "<Box sx={mergeSx({ typography: 'h3' }, slotProps?.row)}>"),
        "derived-undeclared": tplDerived.replace("<Box sx={{ typography: 'h3' }}>", "<Box sx={mergeSx({ typography: 'h3' }, slotProps?.label)}>"),
        "derived-addprop": tplDerived.replace('<Chart type="bar"', '<Chart disabled type="bar"'),
        // REVIEW-26 O32: allowProps are keyed by element; a spread carrying sx is drift.
        "derived-allowed-el": tplDerived.replace('<Chart type="bar"', '<Chart aria-label="x" type="bar"'),
        "derived-wrong-el": tplDerived.replace("<Box sx={{ typography: 'h3' }}>", "<Box aria-label=\"x\" sx={{ typography: 'h3' }}>"),
        "derived-spread-sx": tplDerived.replace("<Box sx={{ typography: 'h3' }}>", "<Box {...{ sx: { p: 9 } }} sx={{ typography: 'h3' }}>"),
        // REVIEW-28 O34: a slot strip pins the slot's exact markup; an edit inside it (p: 9) fails.
        "derived-slot-ok": tplDerived.replace("<Box sx={{ typography: 'h3' }}>{v.total}</Box>", "<Box sx={{ typography: 'h3' }}>{v.total}</Box>{lines.map((l) => (<Box key={l} sx={mergeSx({ color: 'text.secondary' }, slotProps?.line)}>{l}</Box>))}"),
        "derived-slot-edit": tplDerived.replace("<Box sx={{ typography: 'h3' }}>{v.total}</Box>", "<Box sx={{ typography: 'h3' }}>{v.total}</Box>{lines.map((l) => (<Box key={l} sx={mergeSx({ color: 'text.secondary', p: 9 }, slotProps?.line)}>{l}</Box>))}"),
        // REVIEW-33 O40: a conditional caption slot is pinned with its literal sx; a style edit inside it fails.
        "derived-slot-cond-ok": tplDerived.replace("{v.total}</Box>", "{v.total}{v.code ? (<Box component=\"span\" sx={{ typography: 'caption' }}>{v.code}</Box>) : null}</Box>"),
        "derived-slot-cond-edit": tplDerived.replace("{v.total}</Box>", "{v.total}{v.code ? (<Box component=\"span\" sx={{ typography: 'h3', color: 'red', p: 9 }}>{v.code}</Box>) : null}</Box>"),
      };
      const files = {};
      const declared = { ...entry, strip: [["mergeSx\\((\\{ typography: 'h3' \\}), slotProps\\?\\.row\\)", "$1"]] };
      for (const [name, text] of Object.entries(variants)) {
        put(`components/app/sections/demo/${name}.tsx`, text);
        const keyedAllow = { ...entry, allowProps: ["Chart:aria-label", "Box:..."] };
        const pinned = { ...entry, strip: ["\\{lines\\.map\\(\\(l\\) => \\(<Box key=\\{l\\} sx=\\{mergeSx\\(\\{ color: 'text\\.secondary' \\}, slotProps\\?\\.line\\)\\}>\\{l\\}<\\/Box>\\)\\)\\}", "\\{v\\.code \\? \\(<Box component=\"span\" sx=\\{\\{ typography: 'caption' \\}\\}>\\{v\\.code\\}<\\/Box>\\) : null\\}"] };
        files[`components/app/sections/demo/${name}.tsx`] =
          name === "derived-override" || name === "derived-undeclared" ? declared : name.startsWith("derived-allowed") || name === "derived-wrong-el" || name === "derived-spread-sx" ? keyedAllow : name.startsWith("derived-slot") ? pinned : entry;
      }
      // REVIEW-33: sx nested in slotProps (paper width) is anatomy too: an undeclared 240 -> 280 fails,
      // a declared one (strip) passes, a data-only edit passes.
      const tplSlot = "export const P = ({ open, items }) => <CustomPopover open={open} slotProps={{ arrow: { placement: 'top-left' }, paper: { sx: { mt: 0.5, width: 240 } } }}><MenuList>{items}</MenuList></CustomPopover>;\n";
      const slotEntry = { source: "src/layouts/demo/p.tsx", replaced: "items", ...templateAnatomy(tplSlot) };
      const slotVariants = {
        "derived-slotsx-ok": [tplSlot.replace("{items}", "{items.slice(0, 5)}"), slotEntry],
        "derived-slotsx-width": [tplSlot.replace("width: 240", "width: 280"), slotEntry],
        "derived-slotsx-declared": [tplSlot.replace("width: 240", "width: 280"), { ...slotEntry, strip: [["width: 280 \\} \\}", "width: 240 } }"]] }],
      };
      for (const [name, [text, e]] of Object.entries(slotVariants)) {
        put(`components/app/sections/demo/${name}.tsx`, text);
        files[`components/app/sections/demo/${name}.tsx`] = e;
      }
      put("docs/design/template-derived.json", JSON.stringify({ files }));
    }
    put("docs/design/template-verbatim-baseline.json", JSON.stringify({ drift: {
      "components/minimal/tpl-listed.tsx": sha(mine.replace("last week", "custom")),
      "components/minimal/tpl-healed.tsx": sha(mine),
      "components/minimal/tpl-edited.tsx": sha(mine.replace("last week", "custom")),
    } }));
  }
  // Shrink-only ratchets (FIXJ-CI, J1 CI gaps): a legacy class, a card shell reached from a page
  // through a feature file NOT in page-template-map (and an orphan one that is not flagged), lucide
  // icons, a `<select` that ends its line, style props, raw px/hex, stylesheet rules.
  put("features/legacy-class.tsx", 'export const L = () => <div className="wrap fld">x</div>;\n');
  // J1B P0-1 / P2-2: the frozen denylist bites in stories too (the legacy sheets are deleted, so the
  // list no longer comes from them), and a documented hook (msh-side) is exempt.
  put("stories/Bad.stories.tsx", 'export const B = () => <button className="chip on">x</button>;\n');
  put("features/hooked.tsx", 'export const H = () => <nav className="msh-side">x</nav>;\n');
  put("app/(admin)/cardy/page.tsx", 'import { Panel } from "@/features/cardy";\nexport default function Page() { return <Panel />; }\n');
  put("app/(admin)/cardy/loading.tsx", "export default function L() { return null; }\n");
  put("features/cardy/index.ts", 'export { Panel } from "./panel";\n');
  put("features/cardy/panel.tsx", 'export const Panel = () => (\n  <section className="card">\n    <div className={`hd ${x}`} />\n  </section>\n);\n');
  put("features/orphan-card.tsx", 'export const O = () => <section className="card" />;\n');
  put("features/icons.tsx", 'import { Check, X as Close } from "lucide-react";\nexport const I = () => <Check />;\n');
  put("package.json", JSON.stringify({ name: "fixture", dependencies: { "lucide-react": "1.0.0" } }));
  put("features/multiline-select.tsx", 'export const S = () => (\n  <select\n    value={v}\n  />\n);\nexport const T = () => <div style={{ width: "12px", color: "#abcdef" }} />;\n');
  const { findings } = runGuard(root, { themeDiff: false });
  {
    const hits = (check, file) => findings.filter((f) => f.check === check && f.file === file).length;
    const problems = [];
    if (hits("legacy-class-use", "features/legacy-class.tsx") !== 2) problems.push(`legacy-class-use wrap+fld=${hits("legacy-class-use", "features/legacy-class.tsx")} (want 2)`);
    if (hits("legacy-class-use", "stories/Bad.stories.tsx") !== 2) problems.push(`legacy-class-use story chip+on=${hits("legacy-class-use", "stories/Bad.stories.tsx")} (want 2)`);
    if (hits("legacy-class-use", "features/hooked.tsx") !== 0) problems.push("legacy-class-use flagged the documented hook msh-side");
    if (hits("legacy-card-reachable", "features/cardy/panel.tsx") !== 2) problems.push(`legacy-card-reachable panel=${hits("legacy-card-reachable", "features/cardy/panel.tsx")} (want 2: card + hd through the barrel)`);
    if (hits("legacy-card-reachable", "features/orphan-card.tsx") !== 0) problems.push("legacy-card-reachable flagged a file no page imports");
    if (hits("lucide-import", "features/icons.tsx") !== 2) problems.push(`lucide-import=${hits("lucide-import", "features/icons.tsx")} (want 2)`);
    if (hits("lucide-banned", "features/icons.tsx") !== 1) problems.push(`lucide-banned import=${hits("lucide-banned", "features/icons.tsx")} (want 1)`);
    if (hits("lucide-banned", "package.json") !== 1) problems.push(`lucide-banned dependency=${hits("lucide-banned", "package.json")} (want 1)`);
    if (hits("native-control", "features/multiline-select.tsx") !== 1) problems.push("native-control missed a <select that ends its line");
    if (hits("inline-style-prop", "features/multiline-select.tsx") !== 1) problems.push("inline-style-prop missed style={{");
    if (hits("raw-px-hex-literal", "features/multiline-select.tsx") !== 2) problems.push(`raw-px-hex-literal=${hits("raw-px-hex-literal", "features/multiline-select.tsx")} (want 2)`);
    if (hits("legacy-css-rules", "components/bad.css") < 5) problems.push(`legacy-css-rules components/bad.css=${hits("legacy-css-rules", "components/bad.css")} (want >= 5 rules, @media children counted)`);
    // Ratchet semantics: over fails, slack (incl. a file with no finding left) is reported, a new file has 0.
    const r = evaluateRatchet(
      [{ check: "inline-style-prop", file: "a.tsx" }, { check: "inline-style-prop", file: "a.tsx" }, { check: "inline-style-prop", file: "b.tsx" }, { check: "inline-style-prop", file: "new.tsx" }],
      { "inline-style-prop|a.tsx": { allowed: 1 }, "inline-style-prop|b.tsx": { allowed: 3 }, "inline-style-prop|gone.tsx": { allowed: 2 } },
      { seed: false },
    );
    const over = r.over.map((o) => o.key).sort().join(",");
    const slack = r.shrinkable.map((o) => o.key).sort().join(",");
    if (over !== "inline-style-prop|a.tsx,inline-style-prop|new.tsx") problems.push(`ratchet over=${over}`);
    if (slack !== "inline-style-prop|b.tsx,inline-style-prop|gone.tsx") problems.push(`ratchet slack=${slack}`);
    if (r.nextBaseline["inline-style-prop|gone.tsx"] || r.nextBaseline["inline-style-prop|new.tsx"] || r.nextBaseline["inline-style-prop|b.tsx"]?.allowed !== 1) problems.push("ratchet next baseline must drop zero-finding files, never seed a new file, and lower b.tsx to 1");
    // --update-baseline never adds a waiver.
    const nw = nextWaivers([{ key: "a" }, { key: "new" }], new Set(["a", "stale"]));
    if (nw.join(",") !== "a") problems.push(`update-baseline waivers=${nw.join(",")} (want a: keep listed+found, drop stale, never add new)`);
    if (problems.length) {
      console.error(`design_system_self_test=FAIL shrink ratchets: ${problems.join("; ")}`);
      process.exit(1);
    }
  }
  const got = new Set(findings.map((f) => f.check));
  const expected = Object.keys(CHECKS).filter((c) => c !== "theme-token-drift" && c !== "brand-lock");
  const missing = expected.filter((c) => !got.has(c));
  const okPageFlagged = findings.some((f) => f.file === "app/(admin)/ok/page.tsx");
  if (!findings.some((f) => f.check === "pending-dim" && f.file === "features/dim-bad.tsx") || !findings.some((f) => f.check === "pending-dim" && f.file === "app/dim-bad.css") || !findings.some((f) => f.check === "pending-dim" && f.file === "features/dim-attr.tsx") || !findings.some((f) => f.check === "pending-dim" && f.file === "features/dim-wfbusy.tsx")) {
    console.error("design_system_self_test=FAIL pending-dim missed an opacity tied to a pending flag or a [data-nav-pending] CSS dim");
    process.exit(1);
  }
  if (findings.some((f) => f.check === "pending-dim" && f.file === "features/dim-ok.tsx")) {
    console.error("design_system_self_test=FAIL pending-dim flagged an opacity that is not a pending-navigation dim");
    process.exit(1);
  }
  const sfpRef = findings.find((f) => f.check === "server-function-prop" && f.file === "features/sfp/tabs-row.tsx");
  if (!sfpRef || !/component=\{ServerLink\}/.test(sfpRef.snippet)) {
    console.error("design_system_self_test=FAIL server-function-prop did not flag a server-module forwardRef passed as Tab component");
    process.exit(1);
  }
  if (findings.some((f) => f.check === "server-function-prop" && f.file !== "features/sfp/bars.tsx" && f.file !== "features/sfp/tabs-row.tsx")) {
    console.error(`design_system_self_test=FAIL server-function-prop flagged a client-only module: ${findings.filter((f) => f.check === "server-function-prop").map((f) => f.file).join(", ")}`);
    process.exit(1);
  }
  if (findings.some((f) => f.check === "client-api-without-use-client" && f.file === "components/client-ok.tsx")) {
    console.error("design_system_self_test=FAIL client-api-without-use-client flagged a \"use client\" module");
    process.exit(1);
  }
  if (findings.some((f) => f.check === "hand-drawn-skeleton" && f.file === "app/(admin)/composed/loading.tsx")) {
    console.error("design_system_self_test=FAIL hand-drawn-skeleton flagged a loading.tsx composed only from components/app/skeletons");
    process.exit(1);
  }
  // A `:where(:not(.Mui…))` selector excludes MUI parts: it must not count as a MUI colour rule.
  if (findings.some((f) => f.check === "legacy-table-paint" && f.line === 5)) {
    console.error("design_system_self_test=FAIL legacy-table-paint flagged a td:not(.MuiTableCell-root) rule");
    process.exit(1);
  }
  if (findings.some((f) => f.check === "legacy-css-mui-colour" && f.line !== 1)) {
    console.error("design_system_self_test=FAIL legacy-css-mui-colour flagged a :not(.Mui…) exclusion or a transparent reset");
    process.exit(1);
  }
  const okDrawer = findings.filter((f) => f.file === "features/ok-drawer.tsx" && (f.check === "drawer-off-template" || f.check === "fixed-px-width"));
  const badDrawerLines = findings.filter((f) => f.file === "features/bad-drawer.tsx" && f.check === "drawer-off-template").map((f) => f.line).sort((x, y) => x - y);
  if (okDrawer.length || badDrawerLines.join(",") !== "2,3,4,5,6,8") {
    console.error(`design_system_self_test=FAIL drawer-off-template okDrawer=${okDrawer.map((f) => f.check).join(",") || "none"} badDrawerLines=${badDrawerLines.join(",")} (want 2,3,4,5,6,8)`);
    process.exit(1);
  }
  const keyed = findings.filter((f) => f.check === "url-keyed-panel").map((f) => f.file);
  if (!keyed.includes("app/(admin)/tabbed/page.tsx") || keyed.includes("app/(admin)/keyed/page.tsx")) {
    console.error(`design_system_self_test=FAIL url-keyed-panel flagged=${keyed.join(",") || "none"} (want app/(admin)/tabbed/page.tsx only)`);
    process.exit(1);
  }
  if (!findings.some((f) => f.check === "section-client-boundary" && f.file === "components/minimal/sections/overview/demo/server-zero-arg-sx.tsx")) {
    console.error("design_system_self_test=FAIL section-client-boundary missed a zero-arg sx callback (() => ({ … }))");
    process.exit(1);
  }
  const derivedHits = findings.filter((f) => f.check === "template-derived-anatomy").map((f) => f.file);
  const wantDerived = ["derived-drift", "derived-height", "derived-px", "derived-type", "derived-style", "derived-undeclared", "derived-addprop", "derived-wrong-el", "derived-spread-sx", "derived-slot-edit", "derived-slot-cond-edit", "derived-slotsx-width"].map((n) => `components/app/sections/demo/${n}.tsx`);
  if (["derived-ok", "derived-override", "derived-allowed-el", "derived-slot-ok", "derived-slot-cond-ok", "derived-slotsx-ok", "derived-slotsx-declared"].some((n) => derivedHits.includes(`components/app/sections/demo/${n}.tsx`)) || wantDerived.some((f) => !derivedHits.includes(f))) {
    console.error(`design_system_self_test=FAIL template-derived-anatomy flagged=${[...new Set(derivedHits)].join(",") || "none"} (want ${wantDerived.join(",")})`);
    process.exit(1);
  }
  const verbatimHits = findings.filter((f) => f.check === "template-verbatim").map((f) => `${f.file}:${f.snippet.includes("tpl-healed") ? "healed" : ""}`).sort();
  const wantVerbatim = ["components/minimal/tpl-drift.tsx:", "components/minimal/tpl-edited.tsx:", "components/minimal/tpl-pkg.tsx:", "docs/design/template-verbatim-baseline.json:healed"];
  if (verbatimHits.join("|") !== wantVerbatim.join("|")) {
    console.error(`design_system_self_test=FAIL template-verbatim flagged=${verbatimHits.join(",") || "none"} (want ${wantVerbatim.join(",")})`);
    process.exit(1);
  }
  const coverage = findings.filter((f) => f.check === "page-template-map-coverage").map((f) => f.snippet);
  if (!coverage.some((m) => m.startsWith("/tabbed ")) || coverage.some((m) => m.startsWith("/foo "))) {
    console.error(`design_system_self_test=FAIL page-template-map-coverage flagged=${coverage.join(" | ") || "none"} (want /tabbed flagged, /foo not)`);
    process.exit(1);
  }
  const sepHits = findings.filter((f) => f.check === "server-element-prop").map((f) => `${f.file}:${f.line}`);
  if (sepHits.join("|") !== "features/sep/rows.tsx:6") {
    console.error(`design_system_self_test=FAIL server-element-prop flagged=${sepHits.join(",") || "none"} (want features/sep/rows.tsx:6)`);
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

