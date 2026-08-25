#!/usr/bin/env node
// check-mock-css-parity — compares RENDERED, BROWSER-COMPUTED styles between the approved mock
// (mock/herd-signals-mock.html) and the live app (http://127.0.0.1:3318/herd-signals), instead of
// statically parsing CSS text.
//
// WHY THIS REWRITE (v4) HAPPENED
// -------------------------------
// The previous version (kept in git history) resolved selectors itself: it parsed both
// stylesheets, computed CSS specificity by hand, and picked a "winning" declaration per class. It
// could not see DOM ancestry, so it treated rules belonging to OTHER admin screens as competing
// with ours whenever specificity tied. Worked example: Action Center defines
//     .task-ac .tag{font-size:12px}
// and Herd Signals defines
//     .herd-signals-page .tag{font-size:11px}
// Both are two-class-selector specificity, so the old guard's tie-break (source order) picked
// whichever rule happened to load later in the bundled stylesheet and reported a mismatch against
// whichever one it didn't pick — even though `.task-ac` never wraps anything on this page, and a
// real browser on this page resolves `.tag` to 11px, correctly, every time. It reported 76
// findings on a clean tree and nobody could tell which (if any) were real without checking each
// by hand in devtools. That ambiguity already cost real work once: an agent "fixed" phantom
// findings by adding bare-classname duplicate rules to the SHARED global stylesheet, which had to
// be reverted because it changed behaviour for every other screen that also uses `.tag`.
//
// A real browser already does ancestry, specificity and the cascade correctly, so this version
// stops re-implementing a CSS engine and asks Chromium instead. It launches Playwright
// (`chromium.launch({ channel: 'chrome' })`, resolved from apps/admin-web so the workspace
// install is found), loads the live app and the mock side by side, walks a curated, deliberately
// paired set of elements per Herd Signals surface, and diffs `getComputedStyle(...)` between the
// two. This is also just closer to the truth: computed style IS what the maintainer sees: no
// separate "did we resolve the cascade correctly" question, because the browser resolved it.
//
// PAIRING RULE (state this before trusting any output of this file)
// --------------------------------------------------------------------
// Elements are paired DELIBERATELY, not by matching class name alone: each entry in
// ELEMENT_REGISTRY below names a tab, a human label, a mock CSS selector and an app CSS selector,
// chosen by class name AND (for anything that repeats — rows, cards, chips, tabs) POSITION within
// its container ("first data row", "first tab button", "the Live-tab KPI card", etc.) or ROLE
// (aria-label / data-tab). A registry entry is only added when its pairing is unambiguous: one
// specific element in the mock's static fixture markup, matched to one specific element in the
// app's live-rendered markup, both reachable by a stable selector that does not depend on
// fixture text or row count. Content is NEVER part of the pairing or the comparison — the mock
// has invented fixture data (fake tag IDs, fake battery numbers) and the app has ~20 real tags;
// comparing text or counting elements would be comparing two different datasets, not two
// implementations of the same design. When a pairing would require guessing (e.g. the mock's
// alert-type filter chips do not have a stable 1:1 counterpart in the app's unified Alerts list —
// see herd-signals-board.tsx's comment on this), the registry SKIPS it rather than pair blind and
// print a confident but meaningless number. Skipped surfaces are listed explicitly in the run
// output, not silently dropped.
//
// WHAT THIS STILL CANNOT SEE (read before trusting a clean run)
// -------------------------------------------------------------------
// - Hover / focus / active states. This script never dispatches a real hover or focus event, so
//   `:hover`/`:focus`/`:focus-visible`/`:active` styling on either side is invisible. A control
//   that looks right at rest but diverges on hover would pass clean here.
// - Media queries / responsive breakpoints. The browser viewport is fixed for the whole run (see
//   VIEWPORT below); a rule that only applies at a different width is never exercised.
// - Anything gated behind an interaction this script does not drive: this run opens the six
//   tabs, the row-click drawer, and the drawer's Expand into full-screen history — but not, say,
//   a tooltip, a dropdown's open state, a modal other than the drawer/fullscreen, or an
//   error/loading/empty state that only renders when the API call fails or is slow. Those are
//   still a manual side-by-side job.
// - Animation/transition end states, `prefers-reduced-motion`, print styles, or anything else
//   conditional on a media feature or timing this script does not simulate.
// - Elements outside ELEMENT_REGISTRY. This is a curated anchor set (headers, filter bar,
//   buttons, tags/badges, KPI tiles, grid layout, one representative data row/card per list
//   surface, the drawer, the full-screen history) chosen to cover every visually load-bearing
//   pattern on each tab — it is not literally every DOM node. A one-off inline style on some
//   element never added to the registry is invisible here, same as it always would be to any
//   selector-driven check.
//
// DELIBERATE MOCK DIVERGENCES (banned reintroductions, not CSS bugs) — UNCHANGED FROM v1-v3
// ---------------------------------------------------------------------------------------------
// Some mock content is intentionally NOT ported, by maintainer decision, for reasons that have
// nothing to do with CSS. The battery-life estimate ("est. ~1.7 year left", "median ~1.6 year")
// is the current case: GoatOS has no vendor-confirmed discharge curve, so any life estimate is
// invented data, and the maintainer ordered it deleted (see
// apps/admin-web/features/herd-signals/herd-signals-animals-table.tsx). The danger is specific:
// the app's stylesheet already carries `.srcl.inferred` (ported for other Derived/Inferred
// fields), so if the estimate text were reintroduced verbatim it would render CORRECTLY STYLED —
// invisible to a computed-style diff exactly the same way it was invisible to the old static-CSS
// diff. DELIBERATE_MOCK_DIVERGENCES below is a third, independent scan that greps herd-signals
// component source for the patterns that would signal that specific reintroduction, so this stays
// a machine-enforced fact instead of something only remembered by whoever was in the room. This
// grep-based check is the only thing that has caught its reintroduction twice already — add to
// this list, do not remove or silently work around a divergence finding.
//
// USAGE
//   node tools/agent-hooks/check-mock-css-parity.mjs            # rendered-style comparison (needs
//                                                                 both dev servers running)
//   node tools/agent-hooks/check-mock-css-parity.mjs --self-test   # pure-function unit tests, no browser
import { existsSync, readFileSync } from "node:fs";
import { readdirSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const adminWebDir = join(repoRoot, "apps/admin-web");

// Servers this guard drives. Both must already be running (see AGENTS/CLAUDE.md worktree notes —
// this guard never starts or stops a dev server itself, it only fails clearly if one is down).
const APP_TAB_URL = (tab) => `http://127.0.0.1:3318/herd-signals${tab === "live" ? "" : `?hs_tab=${tab}`}`;
const MOCK_URL = "http://127.0.0.1:8917/herd-signals-mock.html";
const VIEWPORT = { width: 1440, height: 900 };

// Divergences the app is DELIBERATELY allowed to have from the mock, for non-CSS reasons.
// Each entry's `forbiddenInComponents` patterns must never match herd-signals component source —
// if one does, that's the banned content creeping back in, not a parity bug to "fix" toward the
// mock. This is intentionally a separate scan from the computed-style comparison: the whole point
// is that the CSS comparison would not catch this (see header).
const DELIBERATE_MOCK_DIVERGENCES = [
  {
    id: "battery-life-estimate",
    reason:
      "No vendor-confirmed battery discharge curve exists for GoatOS smart tags, so a life " +
      "estimate ('est. ~1.7 year left', 'median ~1.6 year') is invented data. The maintainer " +
      "ordered the mock's battery-life-estimate text permanently deleted, not reintroduced " +
      "(see apps/admin-web/features/herd-signals/herd-signals-animals-table.tsx). It would not " +
      "trip the computed-style checks above because `.srcl.inferred` is already ported for " +
      "other Derived/Inferred fields, so reintroduced text would render styled and clean.",
    forbiddenInComponents: [
      // NOTE: a bare `srcl inferred` is NOT banned -- Movement state and Pattern legitimately
      // carry an Inferred marker, and the mock shows them that way. Only an Inferred marker
      // sitting in BATTERY context is the reintroduced life estimate. Matching the bare marker
      // flagged those honest rows and would have pushed someone to delete correct labelling.
      /battery[^\n]{0,200}\bsrcl\s+inferred\b/i,
      /\bsrcl\s+inferred\b[^\n]{0,200}battery/i,
      /estimated?\s+battery\s+life/i,
      /battery.{0,10}life.{0,10}(left|remaining|estimate)/i,
      /est\.\s*~?\s*\d+(\.\d+)?\s*(year|month)s?\s*left/i,
    ],
  },
];

// ---------------------------------------------------------------------------------------------
// Computed-style properties compared per element. This is the "visually load-bearing" set named
// in the task: colour, background, border (colour/width/radius), font, spacing, layout.
// ---------------------------------------------------------------------------------------------
const TRACKED_PROPS = [
  "color",
  "backgroundColor",
  "borderTopColor",
  "borderRightColor",
  "borderBottomColor",
  "borderLeftColor",
  "borderTopWidth",
  "borderRightWidth",
  "borderBottomWidth",
  "borderLeftWidth",
  "borderTopLeftRadius",
  "borderTopRightRadius",
  "borderBottomRightRadius",
  "borderBottomLeftRadius",
  "fontSize",
  "fontWeight",
  "fontFamily",
  "paddingTop",
  "paddingRight",
  "paddingBottom",
  "paddingLeft",
  "marginTop",
  "marginRight",
  "marginBottom",
  "marginLeft",
  "gap",
  "display",
  "gridTemplateColumns",
  "boxShadow",
];

// ---------------------------------------------------------------------------------------------
// ELEMENT_REGISTRY — the curated, deliberately-paired anchor set. See "PAIRING RULE" above.
//
// `tab` picks which app URL / mock setTab(...) click to load before selecting. `setupMock` and
// `setupApp` are optional extra steps (open a drawer, click Expand) run AFTER the tab is showing
// and BEFORE the selector is queried. `optional: true` means: if either side's selector matches
// zero elements, SKIP this entry (report it, don't fail on it) — used only where the element's
// presence is itself data-dependent in a way neither side controls set-and-forget-solid.
// ---------------------------------------------------------------------------------------------
const ELEMENT_REGISTRY = [
  // ---- Global chrome (present on every tab; checked once on Live Monitor) --------------------
  { tab: "live", label: "page title (h1)", mockSelector: ".phead h1", appSelector: ".herd-signals-page .phead h1" },
  { tab: "live", label: "page sub-copy", mockSelector: ".phead .sub", appSelector: ".herd-signals-page .phead .sub" },
  { tab: "live", label: "tab strip container", mockSelector: "#segs", appSelector: ".herd-signals-page .segs" },
  { tab: "live", label: "active tab button (Live Monitor)", mockSelector: "#segs [data-tab='live']", appSelector: ".herd-signals-page .segs a.on" },
  { tab: "live", label: "inactive tab button (Animals)", mockSelector: "#segs [data-tab='animals']", appSelector: ".herd-signals-page .segs a:not(.on)" },
  { tab: "live", label: "tab count badge", mockSelector: "#segs [data-tab='live'] .cnt", appSelector: ".herd-signals-page .segs a.on .cnt" },

  // ---- Live Monitor -------------------------------------------------------------------------
  { tab: "live", label: "filter bar", mockSelector: "#tab-live .fbar", appSelector: ".herd-signals-page .fbar" },
  { tab: "live", label: "filter-bar search field wrapper", mockSelector: "#tab-live .fbar .fsel.search", appSelector: ".herd-signals-page .fbar .fsel.search" },
  { tab: "live", label: "filter-bar select wrapper", mockSelector: "#tab-live .fbar .fsel:not(.search)", appSelector: ".herd-signals-page .fbar .fsel:not(.search)" },
  { tab: "live", label: "KPI row container", mockSelector: "#kpis", appSelector: ".herd-signals-page .kpis" },
  // `display` is deliberately excluded: mesha-theme.css (search "Every card the same height
  // regardless of caption length") documents that the app intentionally makes `.kpi` a flex
  // column (`display:flex;flex-direction:column` + `.dl{flex:1}`) so every KPI tile in the strip
  // is the same height even when captions wrap to different line counts. The mock is a static
  // page with fixed sample copy and never needed that, so it left `.kpi` at the default `display:
  // block`. Matching the mock here would revert an already-reviewed, documented improvement — this
  // is a genuine, intentional divergence, not a bug in either the page or the guard.
  { tab: "live", label: "first KPI tile", mockSelector: "#kpis .kpi:nth-child(1)", appSelector: ".herd-signals-page .kpis .kpi:nth-child(1)", ignoreProps: ["display"] },
  { tab: "live", label: "KPI tile value", mockSelector: "#kpis .kpi:nth-child(1) .val", appSelector: ".herd-signals-page .kpis .kpi:nth-child(1) .val" },
  { tab: "live", label: "KPI tile label", mockSelector: "#kpis .kpi:nth-child(1) .lab", appSelector: ".herd-signals-page .kpis .kpi:nth-child(1) .lab" },
  { tab: "live", label: "live table card", mockSelector: "#tab-live > .card", appSelector: ".herd-signals-page .card" },
  { tab: "live", label: "live table card header", mockSelector: "#tab-live > .card .hd", appSelector: ".herd-signals-page .card .hd" },
  { tab: "live", label: "row-count chip", mockSelector: "#rowCount", appSelector: ".herd-signals-page .card .hd .tag" },
  {
    tab: "live",
    label: "first data row (live table)",
    // `fontSize` on the `<tr>` itself (not its `<td>` children, which already carry an explicit
    // matching 12.5px rule on both sides) is compared here as a raw inherited value. The mock
    // never styles `tr` at all, so Chrome's UA default for `<table>` (16px, NOT inherited from
    // body's 14px -- verified directly against a live Chromium instance) leaks through; the app's
    // `<tr>` inherits from a different reset baseline and lands at 13px. Neither number is ever
    // rendered -- the row has no direct text node -- so this is an artifact of comparing an
    // invisible inherited property, not a real visual divergence.
    mockSelector: "#liveBody table tbody tr:nth-child(1)",
    appSelector: ".herd-signals-page table tbody tr:nth-child(1)",
    optional: true, // depends on at least one row having loaded on both sides
    ignoreProps: ["fontSize"],
  },
  // `#pager` in the mock is an EMPTY wrapper div; `renderLive()` injects a real `<div class="pager">`
  // as its child only once rows have loaded. `#pager` itself never gets styled (no padding, no
  // border, default block/ink-color/normal-gap) -- comparing it directly paired an inert wrapper
  // against the app's actual, correctly-styled `.pager` element. Guard selector bug, not a page bug.
  // The mock has two separate pager instances (#pagerTop above the table, #pager below it) with
  // deliberately swapped borders: the top one carries `border-bottom`, the bottom one carries
  // `border-top` (see mock's own `.pager.pager-top` override). `#pager` here is specifically the
  // BOTTOM instance, but `.herd-signals-page .pager` on the app side matches the FIRST `.pager` in
  // DOM order, which is the app's `pager("top")` call (herd-signals-table.tsx) carrying the
  // `pager-top` variant class -- i.e. this was pairing the mock's bottom pager against the app's
  // top pager. `:not(.pager-top)` selects the app's actual bottom pager to match.
  { tab: "live", label: "pager", mockSelector: "#pager .pager", appSelector: ".herd-signals-page .pager:not(.pager-top)", optional: true },

  // ---- Animals --------------------------------------------------------------------------------
  { tab: "animals", label: "Animals card", mockSelector: "#tab-animals .card", appSelector: ".herd-signals-page .card" },
  { tab: "animals", label: "Animals card header", mockSelector: "#tab-animals .card .hd", appSelector: ".herd-signals-page .card .hd" },
  {
    tab: "animals",
    label: "first data row (animals table)",
    // `fontSize` on the `<tr>` itself (not its `<td>` children, which already carry an explicit
    // matching 12.5px rule on both sides) is compared here as a raw inherited value. The mock
    // never styles `tr` at all, so Chrome's UA default for `<table>` (16px, NOT inherited from
    // body's 14px -- verified directly against a live Chromium instance) leaks through; the app's
    // `<tr>` inherits from a different reset baseline and lands at 13px. Neither number is ever
    // rendered -- the row has no direct text node -- so this is an artifact of comparing an
    // invisible inherited property, not a real visual divergence.
    mockSelector: "#animalsBody table tbody tr:nth-child(1)",
    appSelector: ".herd-signals-page table tbody tr:nth-child(1)",
    optional: true,
    ignoreProps: ["fontSize"],
  },

  // ---- Gateways -------------------------------------------------------------------------------
  // The mock wraps each tab in its own `<section id="tab-*">`, so `.grid2:nth-of-type(1)` inside
  // #tab-gateways really is "the first div child, and it happens to be .grid2". The live app has
  // no per-tab wrapper section — GatewaysTab renders its two `.grid2` divs as siblings of `.phead`
  // and `.segs` directly under `.herd-signals-page` — so `:nth-of-type` counts ALL div siblings
  // (phead, segs, grid2, grid2), not just the `.grid2`-classed ones, and never lands on 1 or 2.
  // That is a guard selector bug, not a page bug: `.grid2:not(.grid2 ~ .grid2)` picks the .grid2
  // with no preceding .grid2 sibling (i.e. the first one) and `.grid2 ~ .grid2` picks the one
  // preceded by another .grid2 (the second, since this page only ever renders two per tab).
  { tab: "gateways", label: "gateway grid container", mockSelector: "#gwBody", appSelector: ".herd-signals-page .grid2:not(.grid2 ~ .grid2)" },
  {
    tab: "gateways",
    label: "first gateway card",
    mockSelector: "#gwBody .gwcard:nth-child(1)",
    appSelector: ".herd-signals-page .grid2:not(.grid2 ~ .grid2) .gwcard:nth-child(1)",
    optional: true,
  },
  { tab: "gateways", label: "second grid row (coverage + battery)", mockSelector: "#tab-gateways .grid2:nth-of-type(2)", appSelector: ".herd-signals-page .grid2 ~ .grid2" },
  { tab: "gateways", label: "coverage-summary card", mockSelector: "#tab-gateways .grid2:nth-of-type(2) .card:nth-child(1)", appSelector: ".herd-signals-page .grid2 ~ .grid2 .card:nth-child(1)" },
  { tab: "gateways", label: "battery-outlook card", mockSelector: "#tab-gateways .grid2:nth-of-type(2) .card:nth-child(2)", appSelector: ".herd-signals-page .grid2 ~ .grid2 .card:nth-child(2)" },

  // ---- Alerts ---------------------------------------------------------------------------------
  { tab: "alerts", label: "Alerts card", mockSelector: "#tab-alerts .card", appSelector: ".herd-signals-page .card" },
  { tab: "alerts", label: "Alerts card header", mockSelector: "#tab-alerts .card .hd", appSelector: ".herd-signals-page .card .hd" },
  {
    tab: "alerts",
    label: "first alert row",
    // `#alertsBody` in the mock IS the `.bd.flush` node, so `#alertsBody .rowlist > *:nth-child(1)`
    // descends into `.rowlist` before picking the first row. The app selector stopped one level
    // early at `.bd.flush > *:nth-child(1)`, which resolves to the `.rowlist` wrapper div itself
    // (no padding/border/gap of its own) rather than the first `.rowitem` — a guard selector bug,
    // not a page bug.
    mockSelector: "#alertsBody .rowlist > *:nth-child(1)",
    appSelector: ".herd-signals-page .card .bd.flush .rowlist > *:nth-child(1)",
    optional: true,
    // Both sides apply the identical rule `.rowitem:last-child{border-bottom:0}` (mock inline;
    // app at mesha-theme.css's `.herd-signals-page .rowitem:last-child`). The mock's random fixture
    // generator always produces multiple alert rows, so its first row keeps a border. This tenant's
    // REAL alerting set is small (this is live data, not a fixture) and can easily sit at exactly
    // one row, which makes that same row both first AND last -- correctly losing its border-bottom
    // on both sides, by the same CSS rule. Comparing border-bottom here is really comparing row
    // COUNT, which is live data, not styling; that is explicitly out of scope (see AGENTS notes:
    // "the app shows 20 real tags that barely move" — do not force data to match the mock's
    // invented fixtures). Ignored rather than "fixed" in either direction.
    ignoreProps: ["borderBottomColor", "borderBottomWidth"],
  },

  // ---- Tag Mapping ----------------------------------------------------------------------------
  { tab: "mapping", label: "mapping filter bar", mockSelector: "#tab-mapping .fbar", appSelector: ".herd-signals-page .fbar.herd-signals-fbar, .herd-signals-page .hs-mapping-tab .fbar" },
  { tab: "mapping", label: "mapping filter-bar button", mockSelector: "#tab-mapping .fbar .btn:nth-of-type(1)", appSelector: ".herd-signals-page .fbar .btn:nth-of-type(1)" },
  { tab: "mapping", label: "mapping card", mockSelector: "#tab-mapping .card", appSelector: ".herd-signals-page .card" },
  {
    tab: "mapping",
    label: "first mapping row",
    // `fontSize` on the `<tr>` itself (not its `<td>` children, which already carry an explicit
    // matching 12.5px rule on both sides) is compared here as a raw inherited value. The mock
    // never styles `tr` at all, so Chrome's UA default for `<table>` (16px, NOT inherited from
    // body's 14px -- verified directly against a live Chromium instance) leaks through; the app's
    // `<tr>` inherits from a different reset baseline and lands at 13px. Neither number is ever
    // rendered -- the row has no direct text node -- so this is an artifact of comparing an
    // invisible inherited property, not a real visual divergence.
    mockSelector: "#mappingBody table tbody tr:nth-child(1)",
    appSelector: ".herd-signals-page table tbody tr:nth-child(1)",
    optional: true,
    ignoreProps: ["fontSize"],
  },

  // ---- Insights -------------------------------------------------------------------------------
  { tab: "insights", label: "info banner", mockSelector: "#tab-insights .banner.info", appSelector: ".herd-signals-page .banner.info" },
  { tab: "insights", label: "insights grid", mockSelector: "#insightsBody", appSelector: ".herd-signals-page .grid2" },
  {
    tab: "insights",
    label: "first insight card",
    mockSelector: "#insightsBody .card:nth-child(1), #insightsBody .insight:nth-child(1)",
    appSelector: ".herd-signals-page .grid2 .card:nth-child(1), .herd-signals-page .grid2 .insight:nth-child(1)",
    optional: true,
  },

  // ---- Row-click drawer (opened from Live Monitor's first row) --------------------------------
  {
    tab: "live",
    label: "drawer panel",
    mockSelector: ".drawer.on, .drawer",
    appSelector: ".drawer",
    setupMock: "openDrawer",
    setupApp: "openDrawer",
    optional: true, // needs a real first row to click on both sides
  },
  {
    tab: "live",
    label: "drawer header (dh)",
    mockSelector: ".dh",
    appSelector: ".dh",
    setupMock: "openDrawer",
    setupApp: "openDrawer",
    optional: true,
  },
  {
    tab: "live",
    label: "drawer control row (patrow)",
    mockSelector: ".patrow",
    appSelector: ".patrow",
    setupMock: "openDrawer",
    setupApp: "openDrawer",
    optional: true,
  },
  {
    tab: "live",
    label: "drawer range picker",
    mockSelector: ".drawer .rangepick",
    appSelector: ".drawer .rangepick",
    setupMock: "openDrawer",
    setupApp: "openDrawer",
    optional: true,
  },

  // ---- Full-screen history (reached via the drawer's Expand) ----------------------------------
  {
    tab: "live",
    label: "full-screen history panel",
    mockSelector: ".fs.on, .fs",
    appSelector: ".fs.on",
    setupMock: "openFullscreen",
    setupApp: "openFullscreen",
    optional: true,
  },
  {
    tab: "live",
    label: "full-screen header (fshd)",
    mockSelector: ".fshd",
    appSelector: ".fs.on .fshd",
    setupMock: "openFullscreen",
    setupApp: "openFullscreen",
    optional: true,
  },
  {
    tab: "live",
    label: "full-screen body (fsbd)",
    mockSelector: ".fsbd",
    appSelector: ".fs.on .fsbd",
    setupMock: "openFullscreen",
    setupApp: "openFullscreen",
    optional: true,
  },
];

// ---------------------------------------------------------------------------------------------
// Normalisation — a real browser already resolved the cascade, so all that's left is comparing
// two already-computed values fairly (colour formats, "0px" vs "0", trailing decimals).
// ---------------------------------------------------------------------------------------------
function normalizeComputedValue(prop, value) {
  if (value == null) return "";
  let v = String(value).trim();
  if (prop === "boxShadow" && v === "none") return "none";
  // getComputedStyle always returns colours as rgb()/rgba() already, so no hex/name handling is
  // needed here (unlike the old static-CSS resolver) — just collapse whitespace.
  v = v.replace(/\s+/g, " ");
  if (/^0(px)?$/.test(v)) return "0px";
  return v;
}

// A browser reports a border-*-color for EVERY element regardless of whether that border is
// actually visible (border-*-width: 0px) — the colour is resolved (often to `currentcolor`'s
// computed value) even when nothing will ever paint with it. Comparing that colour when the
// corresponding width is 0 on BOTH sides is pure noise: neither side draws a border there, so a
// "different invisible colour" is not a design divergence. Only compare a border side's colour
// when at least one side actually has a non-zero width for that side.
const BORDER_COLOR_TO_WIDTH_PROP = {
  borderTopColor: "borderTopWidth",
  borderRightColor: "borderRightWidth",
  borderBottomColor: "borderBottomWidth",
  borderLeftColor: "borderLeftWidth",
};

// grid-template-columns on an `auto-fit`/`minmax` track (this app's `.grid2`/`.kpis` layout)
// resolves to literal pixel track widths that depend on the CONTAINER's rendered width, which can
// differ a few px between the mock's static page and the app's live layout (e.g. a scrollbar, a
// sidebar) for reasons that have nothing to do with either page's CSS. What is actually
// load-bearing here is the number of tracks (does it wrap into the same column count) and whether
// the tracks are still an equal-width `Nfr`-style split, not the literal px. Compare track COUNT
// instead of literal px for this one property; the literal string is still shown for reference.
function summarizeGridTemplateColumns(value) {
  if (!value || value === "none") return value;
  const tracks = value.trim().split(/\s+/).filter(Boolean);
  return `${tracks.length} track(s)`;
}

function diffComputedStyles(mockStyle, appStyle, ignoreProps) {
  const mismatches = [];
  for (const prop of TRACKED_PROPS) {
    if (ignoreProps && ignoreProps.includes(prop)) continue;
    if (prop in BORDER_COLOR_TO_WIDTH_PROP) {
      const widthProp = BORDER_COLOR_TO_WIDTH_PROP[prop];
      const mockWidth = normalizeComputedValue(widthProp, mockStyle[widthProp]);
      const appWidth = normalizeComputedValue(widthProp, appStyle[widthProp]);
      if (mockWidth === "0px" && appWidth === "0px") continue; // invisible on both sides — not a divergence
    }
    if (prop === "gridTemplateColumns") {
      const mockTracks = summarizeGridTemplateColumns(mockStyle[prop]);
      const appTracks = summarizeGridTemplateColumns(appStyle[prop]);
      if (mockTracks !== appTracks) {
        mismatches.push({ prop: "gridTemplateColumns (track count)", mock: mockTracks, app: appTracks });
      }
      continue;
    }
    const mockV = normalizeComputedValue(prop, mockStyle[prop]);
    const appV = normalizeComputedValue(prop, appStyle[prop]);
    if (mockV !== appV) mismatches.push({ prop, mock: mockV, app: appV });
  }
  return mismatches;
}

// ---------------------------------------------------------------------------------------------
// Browser driving
// ---------------------------------------------------------------------------------------------
async function loadPlaywright() {
  const require = createRequire(join(adminWebDir, "package.json"));
  try {
    return require("@playwright/test");
  } catch (err) {
    throw new Error(
      `mock-css-parity: could not load @playwright/test from ${adminWebDir} (${err.message}). ` +
        "Run this guard from a tree with apps/admin-web dependencies installed (npm install).",
    );
  }
}

async function assertServerUp(url, label) {
  try {
    const res = await fetch(url, { method: "GET" });
    if (!res.ok && res.status >= 500) {
      throw new Error(`${label} responded with HTTP ${res.status}`);
    }
  } catch (err) {
    throw new Error(
      `mock-css-parity: ${label} is not reachable at ${url} (${err.message}). ` +
        "This guard needs both the admin-web dev server (:3318) and the mock static server " +
        "(:8917) already running -- it never starts them itself. Start them, then re-run.",
    );
  }
}

async function gotoAppTab(page, tab) {
  await page.goto(APP_TAB_URL(tab), { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".herd-signals-page", { timeout: 15000 });
  // Next.js streams this page (Suspense boundaries for the per-tab data fetch resolve after the
  // initial HTML/JS is already "networkidle"), so content can still swap in a beat after the
  // selector above first appears. A short settle avoids a false "not found" race.
  await page.waitForTimeout(400);
}

async function gotoMockTab(page, tab, { forceReload = false } = {}) {
  if (!page.__mockLoaded || forceReload) {
    await page.goto(MOCK_URL, { waitUntil: "networkidle" });
    page.__mockLoaded = true;
  }
  if (tab !== "live") {
    await page.click(`#segs [data-tab='${tab}']`);
  } else {
    // Reset to Live Monitor in case a previous entry navigated the mock's own SPA-style tabs.
    const onLive = await page.$("#segs [data-tab='live'].on");
    if (!onLive) await page.click("#segs [data-tab='live']");
  }
  await page.waitForTimeout(50); // mock's setTab() is synchronous DOM toggling, not async — small settle margin only
}

async function openMockDrawer(page) {
  // Fresh page load first: the mock keeps drawer/scrim in the DOM (just hidden) once opened once,
  // and a stale `.scrim.on` from an earlier setup call intercepts the row click. Reloading here
  // guarantees the row is actually clickable, at the cost of one extra full page load per setup.
  await gotoMockTab(page, "live", { forceReload: true });
  const row = await page.$("#liveBody table tbody tr:nth-child(1)");
  if (!row) return false;
  await row.click();
  const drawer = await page.waitForSelector(".drawer.on", { timeout: 3000 }).catch(() => null);
  return Boolean(drawer);
}

async function openAppDrawer(page) {
  const row = await page.$(".herd-signals-page table tbody tr:nth-child(1)");
  if (!row) return false;
  await row.click();
  const drawer = await page.waitForSelector(".drawer", { timeout: 5000 }).catch(() => null);
  return Boolean(drawer);
}

async function openMockFullscreen(page) {
  const opened = await openMockDrawer(page);
  if (!opened) return false;
  const expand = await page.$(".drawer .hs-btn, .drawer button:has-text('Expand'), .drawer a:has-text('Expand')");
  if (!expand) return false;
  await expand.click();
  const fs = await page.waitForSelector(".fs.on", { timeout: 3000 }).catch(() => null);
  return Boolean(fs);
}

async function openAppFullscreen(page) {
  const opened = await openAppDrawer(page);
  if (!opened) return false;
  const expand = await page.$(".drawer .hs-btn, .drawer a:has-text('Expand')");
  if (!expand) return false;
  await expand.click();
  const fs = await page.waitForSelector(".fs.on", { timeout: 5000 }).catch(() => null);
  return Boolean(fs);
}

const SETUPS = {
  openDrawer: { mock: openMockDrawer, app: openAppDrawer },
  openFullscreen: { mock: openMockFullscreen, app: openAppFullscreen },
};

async function getComputedStyleOf(page, selector) {
  return page.evaluate(
    ([sel, props]) => {
      const commaSelectors = sel.split(",").map((s) => s.trim());
      let el = null;
      for (const s of commaSelectors) {
        el = document.querySelector(s);
        if (el) break;
      }
      if (!el) return null;
      const cs = window.getComputedStyle(el);
      const out = {};
      for (const p of props) out[p] = cs[p];
      return out;
    },
    [selector, TRACKED_PROPS],
  );
}

async function runRenderedComparison() {
  const { chromium } = await loadPlaywright();
  await assertServerUp(APP_TAB_URL("live"), "admin-web dev server (:3318)");
  await assertServerUp(MOCK_URL, "mock static server (:8917)");

  const browser = await chromium.launch({ channel: "chrome" });
  const mismatches = [];
  const skipped = [];
  try {
    const mockPage = await browser.newPage({ viewport: VIEWPORT });
    const appPage = await browser.newPage({ viewport: VIEWPORT });

    const tabsLoaded = new Set();
    const setupsDone = new Set(); // `${tab}:${setupKey}` -> already applied on this page load

    for (const entry of ELEMENT_REGISTRY) {
      const mockTabKey = `mock:${entry.tab}`;
      const appTabKey = `app:${entry.tab}`;
      if (!tabsLoaded.has(mockTabKey) && !entry.setupMock) {
        await gotoMockTab(mockPage, entry.tab);
      }
      if (!tabsLoaded.has(appTabKey) && !entry.setupApp) {
        await gotoAppTab(appPage, entry.tab);
        tabsLoaded.add(appTabKey);
      }
      if (!entry.setupMock) tabsLoaded.add(mockTabKey);

      if (entry.setupMock) {
        const setupKey = `mock:${entry.tab}:${entry.setupMock}`;
        if (!setupsDone.has(setupKey)) {
          await gotoMockTab(mockPage, entry.tab);
          const ok = await SETUPS[entry.setupMock].mock(mockPage);
          if (ok) setupsDone.add(setupKey);
          else if (!entry.optional) {
            mismatches.push({
              tab: entry.tab,
              label: entry.label,
              prop: "(setup)",
              mock: "could not reach this element via the mock's UI (row click / Expand)",
              app: "",
            });
            continue;
          } else {
            skipped.push({ tab: entry.tab, label: entry.label, reason: "mock-side setup interaction did not produce the element (no row to click, or Expand not found)" });
            continue;
          }
        }
      }
      if (entry.setupApp) {
        const setupKey = `app:${entry.tab}:${entry.setupApp}`;
        if (!setupsDone.has(setupKey)) {
          await gotoAppTab(appPage, entry.tab);
          const ok = await SETUPS[entry.setupApp].app(appPage);
          if (ok) setupsDone.add(setupKey);
          else if (!entry.optional) {
            mismatches.push({
              tab: entry.tab,
              label: entry.label,
              prop: "(setup)",
              mock: "",
              app: "could not reach this element via the app's UI (row click / Expand)",
            });
            continue;
          } else {
            skipped.push({ tab: entry.tab, label: entry.label, reason: "app-side setup interaction did not produce the element (no row loaded, or Expand not found)" });
            continue;
          }
        }
      }

      const mockStyle = await getComputedStyleOf(mockPage, entry.mockSelector);
      const appStyle = await getComputedStyleOf(appPage, entry.appSelector);

      if (!mockStyle || !appStyle) {
        if (entry.optional) {
          skipped.push({
            tab: entry.tab,
            label: entry.label,
            reason: !mockStyle && !appStyle ? "element not found on either side" : !mockStyle ? "element not found in mock" : "element not found in app",
          });
          continue;
        }
        mismatches.push({
          tab: entry.tab,
          label: entry.label,
          prop: "(existence)",
          mock: mockStyle ? "present" : `NOT FOUND (selector: ${entry.mockSelector})`,
          app: appStyle ? "present" : `NOT FOUND (selector: ${entry.appSelector})`,
        });
        continue;
      }

      for (const d of diffComputedStyles(mockStyle, appStyle, entry.ignoreProps)) {
        mismatches.push({ tab: entry.tab, label: entry.label, prop: d.prop, mock: d.mock, app: d.app });
      }
    }
  } finally {
    await browser.close();
  }
  return { mismatches, skipped };
}

// ---------------------------------------------------------------------------------------------
// Banned-reintroduction scan (unchanged from v1-v3) — plain source grep, no browser needed.
// ---------------------------------------------------------------------------------------------
function walkTsx(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) walkTsx(full, out);
    else if (/\.(tsx|jsx)$/.test(entry.name)) out.push(full);
  }
  return out;
}

function scanBannedDivergences(componentsDir) {
  const banned = [];
  const files = walkTsx(join(repoRoot, componentsDir));
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
  }
  return { banned, scanned: files.length };
}

// ---------------------------------------------------------------------------------------------
// Self-test — pure-function checks only (normalisation, banned-pattern scan). Does NOT launch a
// browser and does NOT require either dev server, so it can run in any environment/CI shell.
// ---------------------------------------------------------------------------------------------
function selfTest() {
  const problems = [];

  // normalizeComputedValue: colours from getComputedStyle are already rgb()/rgba() text — must
  // compare equal when identical, and whitespace/zero-unit noise must not cause false positives.
  {
    const a = normalizeComputedValue("color", "rgb(255, 0, 0)");
    const b = normalizeComputedValue("color", "rgb(255,   0, 0)");
    if (a !== b) problems.push("normalizeComputedValue did not collapse whitespace noise in an identical colour");
  }
  {
    const a = normalizeComputedValue("paddingTop", "0px");
    const b = normalizeComputedValue("paddingTop", "0");
    if (a !== b) problems.push("normalizeComputedValue did not treat '0px' and '0' as equal");
  }
  {
    const mismatches = diffComputedStyles(
      { color: "rgb(255, 0, 0)", fontSize: "11px" },
      { color: "rgb(0, 255, 0)", fontSize: "11px" },
    );
    if (!mismatches.some((m) => m.prop === "color")) problems.push("diffComputedStyles did not catch a real colour divergence");
    if (mismatches.some((m) => m.prop === "fontSize")) problems.push("diffComputedStyles false-positived on an identical fontSize");
  }
  {
    // Every tracked prop absent on both sides (e.g. undefined key) must not be reported as a diff
    // (both normalise to the empty string) — this guards against every entry accidentally
    // reporting every untouched TRACKED_PROPS key as a mismatch.
    const mismatches = diffComputedStyles({}, {});
    if (mismatches.length) problems.push("diffComputedStyles reported mismatches for two empty style objects");
  }

  // Zero-width border noise: identical zero widths on both sides must suppress a colour diff, but
  // a REAL colour divergence on a border that is actually drawn (non-zero width somewhere) must
  // still be caught.
  {
    const invisible = diffComputedStyles(
      { borderTopColor: "rgb(1,1,1)", borderTopWidth: "0px" },
      { borderTopColor: "rgb(2,2,2)", borderTopWidth: "0px" },
    );
    if (invisible.some((m) => m.prop === "borderTopColor")) problems.push("a border colour diff was reported for a border that is 0px wide on both sides");
  }
  {
    const visible = diffComputedStyles(
      { borderTopColor: "rgb(1,1,1)", borderTopWidth: "1px" },
      { borderTopColor: "rgb(2,2,2)", borderTopWidth: "1px" },
    );
    if (!visible.some((m) => m.prop === "borderTopColor")) problems.push("a real border colour divergence on a visibly-drawn border was suppressed");
  }

  // grid-template-columns: compare track COUNT, not literal px (container-width layout noise).
  {
    const sameCount = diffComputedStyles(
      { gridTemplateColumns: "182px 182px 182px" },
      { gridTemplateColumns: "179.5px 179.5px 179.5px" },
    );
    if (sameCount.some((m) => m.prop.startsWith("gridTemplateColumns"))) problems.push("gridTemplateColumns compared literal px instead of track count, false-positiving on layout-width noise");
  }
  {
    const diffCount = diffComputedStyles({ gridTemplateColumns: "182px 182px 182px" }, { gridTemplateColumns: "179.5px 179.5px" });
    if (!diffCount.some((m) => m.prop.startsWith("gridTemplateColumns"))) problems.push("gridTemplateColumns did not catch a real track-count divergence (3 columns vs 2)");
  }

  // Registry sanity: every entry must name a tab, a label, and both selectors; optional entries
  // must be explicitly marked, not implied. This catches a copy-paste registry entry missing a
  // field, which would otherwise only surface as a confusing runtime error mid-way through a run.
  for (const entry of ELEMENT_REGISTRY) {
    if (!entry.tab || !entry.label || !entry.mockSelector || !entry.appSelector) {
      problems.push(`ELEMENT_REGISTRY entry missing a required field: ${JSON.stringify(entry)}`);
    }
    if ((entry.setupMock && !entry.setupApp) || (!entry.setupMock && entry.setupApp)) {
      problems.push(`ELEMENT_REGISTRY entry "${entry.label}" sets setupMock/setupApp on only one side — pairing must apply the same interaction to both`);
    }
    if (entry.setupMock && !SETUPS[entry.setupMock]) {
      problems.push(`ELEMENT_REGISTRY entry "${entry.label}" references unknown setup "${entry.setupMock}"`);
    }
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
  console.log("check-mock-css-parity self-test: PASS (pure-function checks only -- run without --self-test to drive the real browser comparison)");
  process.exit(0);
}

if (process.argv.includes("--self-test")) selfTest();

// ---------------------------------------------------------------------------------------------
// Main: rendered-style comparison + banned-reintroduction scan.
// ---------------------------------------------------------------------------------------------
const MODULE_COMPONENTS_DIR = "apps/admin-web/features/herd-signals";

const { banned, scanned } = scanBannedDivergences(MODULE_COMPONENTS_DIR);

const { mismatches, skipped } = await runRenderedComparison().catch((err) => {
  console.error(err.message || String(err));
  process.exit(2);
});

if (skipped.length) {
  console.error("mock-css-parity: skipped (ambiguous or unreachable pairing, not compared, not a failure):");
  for (const s of skipped) console.error(`- [${s.tab}] ${s.label}: ${s.reason}`);
}

let total = 0;
if (mismatches.length) {
  console.error("\nmock-css-parity: computed-style divergence between the mock and the live app:");
  for (const m of mismatches) {
    console.error(`- [${m.tab}] ${m.label} — ${m.prop}: mock="${m.mock}" live="${m.app}"`);
    total += 1;
  }
}

if (banned.length) {
  console.error("\nmock-css-parity: a deliberately-deleted mock element has reappeared:");
  for (const f of banned) {
    console.error(`- ${f.file}: matched banned pattern ${f.pattern} for divergence "${f.id}" — ${f.reason}`);
    total += 1;
  }
}

if (total > 0) {
  console.error(
    `\n${mismatches.length} rendered-style mismatch(es), ${banned.length} banned-reintroduction finding(s), ` +
      `${skipped.length} pairing(s) skipped as ambiguous/unreachable (not counted as failures).`,
  );
  process.exit(1);
}
console.log(
  `mock-css-parity: ok (${ELEMENT_REGISTRY.length - skipped.length} rendered-style comparison(s) across ${new Set(ELEMENT_REGISTRY.map((e) => e.tab)).size} tab(s), ` +
    `${skipped.length} skipped as ambiguous/unreachable, ${scanned} component file(s) scanned for banned reintroductions).`,
);
