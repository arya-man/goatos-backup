---
name: design-system
description: >-
  Use before, during and after ANY change under apps/admin-web/** that a person can see — a new
  page or feature, a route, table, chart, KPI card, filter bar, drawer, dialog, skeleton, tab
  strip, sidebar item, or the theme CSS — and when reviewing one. Holds the admin-web redesign
  contract: the locked Mesha palette and fonts, the page frame (PageShell/PageHeader), the kit
  components every screen is built from (Card/TableCard/BarList/KpiWidget/Skeleton/TemplateTabs/
  FilterBar/Overlay), the motion constants, the banned patterns, and the exact guard commands
  that must be green before a push. Machine backing: `npm run design:guard` (static),
  `npm run visual:stories` and `npm run visual:routes` (Storybook + route visual regression with
  render-integrity checks), beside the existing `smoke:webview` mobile guard.
version: 0.1.0
user-invocable: true
argument-hint: "[route, feature or component being changed]"
---

# Design system (admin-web)

The dashboard is being rebuilt on ONE frame and ONE kit so that every existing route and every
NEW feature looks like the same product, on a 1440px laptop and inside the 390px Android WebView.
Style reference is the Minimal (MUI) dashboard — **structure, density and motion only. Never its
palette, code, images or copy.** Owner rule: *"web and mobile must render properly — no breaking
graphs, tags, numbers, nothing."*

This skill is the contract in agent-facing form. The prose sources are
`docs/design/README.md` (brand lock + verification layers) and
`docs/progress/admin-web-redesign-progress.md` (what has landed). References:

- `references/tokens-and-fonts.md` — the locked palette, the token names, fonts.
- `references/components.md` — the frame and each kit component: when, props, what "done" is.
- `references/motion.md` — the timing constants.
- `references/banned.md` — every banned pattern with the check that catches it and the fix.

---

## The commands (non-negotiable before any UI push)

```bash
# 1. static — no app, no browser, every commit. Brand lock, banned patterns, loading.tsx,
#    PageShell adoption. Green today via the waiver file; red on any NEW instance.
npm --prefix apps/admin-web run design:guard

# 2. component visual regression — every Storybook story at 1440 + 390, dark + light, with the
#    render-integrity probe (overflow, clipped text, empty chart svg, NaN/undefined/F2 text,
#    raw floats, fonts, console errors). Needs Storybook: `npm run storybook` (port 6007).
GOATOS_STORYBOOK_URL=http://127.0.0.1:6007 npm --prefix apps/admin-web run visual:stories

# 3. route visual regression — every smoke route at desktop 1440 / phone 390 / Pixel-5 WebView
#    393, dark + light, same probe, same baseline contract. Needs a live app + backend.
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 GOATOS_BEARER_TOKEN=... npm --prefix apps/admin-web run visual:routes

# 4. the mobile/WebView taxonomy guard and the perf budget stay mandatory
npm --prefix apps/admin-web run smoke:webview:static
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 npm --prefix apps/admin-web run smoke:webview
npm --prefix apps/admin-web run test   # includes perf-capture-budget-contract.test.mjs

# focused runs while iterating
node apps/admin-web/scripts/smoke-stories-visual.mjs --storybook-url http://127.0.0.1:6007 --only kit-kpicard
node apps/admin-web/scripts/smoke-routes-visual.mjs --only alerts,verify --profiles webview --themes dark
node apps/admin-web/scripts/check-design-system.mjs --report
```

Every lane writes PNGs + `summary.json` + `integrity.json` under `.codex-goatos-render/…/<timestamp>/`
and a **diff image per failing capture**. Open them. A JSON summary is not visual proof.

### R2 visual gate (`make admin-web-visual-gate`)

The R2 visual gate (2026-09-27) sits on top of these lanes: `make admin-web-visual-gate`
(= `npm --prefix apps/admin-web run visual:gate`, script `tools/ci/admin-web-visual-gate.sh`)
builds admin-web from the checkout, starts it on a free port against the local API
(`GOATOS_API_BASE_URL`; or audits `GOATOS_ADMIN_WEB_BASE_URL`) and runs
`apps/admin-web/scripts/r2-visual-audit.mjs` over EVERY `app/(admin)` route at 1440 dark, 1440 light
and 390 dark. It INTERACTS: clicks every tab and filter (MutationObserver + 100ms sampling + CDP
screencast frames), opens drawers/dialogs, soft-navigates with the RSC response held to capture
`loading.tsx`, and compares against the MUI Minimal template on :3480 (side-by-sides per the page
map). P0 patterns fail the gate: full-page skeleton flash or document reload on a tab/filter change,
bright background (luminance > 0.5) in dark mode, a colour outside the theme palette (the CDP rule +
stylesheet that sets it is named), drawer content clipped or no backdrop, skeleton-vs-loaded block
IoU < 0.8 or a block missing/extra, tap target < 44px at 390, page sideways scroll, crash / HTTP >= 400.
Skeleton twins (TR1): PageHeaderSkeleton = the verbatim CustomBreadcrumbs; the default KpiRowSkeleton
card = the KpiWidget course card (corner icon tile); a card grid is compared by its first card (count is
data); a `.screen` PageSkeleton keeps its gap on an inner grid. A state-only segment option (the
"Custom" window) is `disabled` unless it is the served state (guard `window-custom-state`).
Existing P0 debt is the shrink-only baseline `apps/admin-web/scripts/r2-visual-audit-baseline.json`
(pattern -> route count): a NEW pattern or one reaching MORE routes fails; `GOATOS_VISUAL_GATE_STRICT=1`
fails on every P0. Shrink it with `node apps/admin-web/scripts/r2-visual-audit.mjs --write-baseline`
after a fix; growing it to land a change is a review finding. It runs in the `admin-web` ci-local job
(when `GOATOS_ADMIN_WEB_BASE_URL` is set, so `make land-check` / `make land-main` run it) and from the
pre-push hook for pushes touching admin-web UI (opt out only with `GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1`,
stated in the PR). Output: `~/mesha/redesign-shots/r2/audit-<timestamp>/report.md` + `report.json`,
failures ranked by PATTERN across routes (e.g. "KPI card bg in dark: 23 routes"), plus `sbs/`,
`frames/`, `drawers/`, `skeleton/` images — open them. New checks from other work plug in as
`apps/admin-web/scripts/r2-audit-checks/<name>.mjs` (default export `{ name, p0, profiles, run(page, ctx) }`)
and land in the same pattern summary.

### Baselines and waivers (deliberate, never casual)

- Story/route baselines: `apps/admin-web/visual-baselines/{stories,routes}/manifest.json`
  (committed fingerprints) + PNGs in `.codex-goatos-render/admin-web-{story,route}-baselines`
  (local, gitignored). Rewrite with `npm run visual:stories:update-baseline` /
  `visual:routes:update-baseline` **only for an intended change**, with the changed PNGs opened
  and the reason in the PR body. Never update the route baseline while the backend is down.
- Static debt: `apps/admin-web/scripts/check-design-system-waivers/design-system-waivers.json`.
  Integrity debt: `apps/admin-web/visual-baselines/{stories,routes}/waivers.json`.
  **A waiver list that grew in a UI change is a review finding.** P0 checks (foreign palette,
  theme token drift, Google Fonts link, brand lock) cannot be waived at all.

---

## The contract in one screen

**Brand lock (P0).** Palette lives in `apps/admin-web/app/mesha-theme.css` and does not move:
brand `#7CCB45`/`#69BA37` (dark), `#54A02C`/`#44831F` (light); bg `#0E1512`/`#FFFFFF`;
panels `#161F1A`/`#1D2820`; sidebar `#0A0F0C`; on-brand ink `#08130B`/`#FFFFFF`. Use
`var(--…)` tokens only. Fonts: Public Sans (body) + Barlow (display) via next/font — no
Google Fonts `<link>`. Banned foreign values: `#0A9F6C #4FD89A #131A21 #1B242E`.

**Frame.** Every `app/(admin)/**/page.tsx` renders inside `PageShell` (24px rhythm, 16 at
≤768) with one `PageHeader` (eyebrow · title · crumbs · actions · tabs · toolbar; **no
description slot**), and has a sibling `loading.tsx` whose skeleton uses the same shell so it
cannot bleed to the edges and matches the final layout's shape.

**Kit.** `Card` (r16, p24, hairline, elevation-1) · `TableCard` (header band, 56/44px rows,
StatusChip, portaled RowMenu, `TableFooter`, horizontal overflow scrolls inside the card) ·
`BarList` (label · track · value, grows from zero, centred axis for negatives, shared tooltip) ·
`KpiWidget` (components/app adapter over the verbatim template Course/Ecommerce widget summary; figure + visible sub-line) ·
`TemplateTabs` (components/app adapter: template list Tabs + Label counts, useUrlTabNav; TabPanel = MUI Stack, no crossfade) ·
`FilterBar` (search · kit `SelectField`/`DateRangeField` · actions · summary) ·
MUI `Dialog` (DialogTitle/Content/Actions) / `Drawer` (components/app/drawer) / template `CustomPopover` + `MenuList` (row actions: `components/app/row-menu`) ·
`components/app/skeletons` blocks (the only loading shapes; guard `hand-drawn-skeleton`). Charts: draw-in, floating tooltip card, rounded
caps, dashed grid, gradient area, donut with centre total.

**Right drawers are the template temporary Drawer** (Ravi R2-4): `MinimalDrawer` (components/app/drawer) or `DetailDrawer` (components/app/detail-drawer) only — portalled, anchor right, visible backdrop (never `invisibleBackdrop` / `backdrop: { invisible: true }`), template paper width 320 (filters) / 360 (settings) / 420 (notifications) / 480 (details, forms; `{ xs: 1, sm: 480 }`), sticky header title + close, Scrollbar body, footer actions. No raw MUI `<Drawer>` for a right drawer, no hand-rolled `<aside className="drawer">` + `.scrim`. Wide content never squeezes or clips at the drawer edge: a table sits in `DrawerTableScroll` (own Scrollbar, `Table sx={{ minWidth }}`) and scrolls sideways. Guards: design:guard `drawer-off-template` (static), `scripts/r2-drawer-audit.mjs` (runtime: width, backdrop, no child overflow without its own scroll container).

**Motion.** Page-enter stagger 400ms / 60ms apart / 8px rise; bars 600ms / 40ms stagger;
charts ~800ms easeInOutSine / 150ms series stagger; tabs slide; dialog scale from 0.96; drawer
slide; popover fade + 4px; button press 0.98. `prefers-reduced-motion` → none.

**Banned.** Native `<select>` / `<input type=date>` (outside the kit wrapper), `window.confirm`
/ `alert`, hex colours in TSX/CSS (outside the two theme files), Tailwind palette classes,
the legacy "F2" code, fixed px widths ≥ 480 that cannot fit 390, explanatory prose under a
title or card header, a chart without the tooltip card or with `isAnimationActive={false}`, a
page without `loading.tsx`, a page outside `PageShell`.

---

## Building a NEW feature — the order

1. Read `references/components.md`; sketch the page as PageHeader + sections (KpiGrid /
   Card / TableCard / BarList). If a section needs a component the kit lacks, add it to the
   kit **with a story** first — never a one-off inside `features/`.
2. Write `page.tsx` + `loading.tsx` together; the skeleton mirrors the final layout.
3. Every state gets a Storybook story: default, empty, loading, error, mobile (390). Stories
   live in `apps/admin-web/stories/kit/*.stories.tsx` (kit) or beside the feature.
4. Add the route(s) to `scripts/smoke-visual-live.mjs` — the webview guard and the route
   visual lane read their route list from there; an unlisted route is uncovered.
5. Run the four commands above. Fix findings; do not waive them. Update baselines only for
   the captures you intended to change, and open those PNGs.
6. Hand off with the desktop AND the 390px screenshot for every touched state, both themes.

## What a reviewer must refuse

- A UI change whose proof is a 1440px screenshot only, or a JSON summary with no PNG opened.
- A grown waiver file or a rewritten baseline with no sentence explaining which capture and why.
- A new page without `loading.tsx`, outside `PageShell`, or with a paragraph under its title.
- A colour literal or a Tailwind palette class anywhere; any edit to a theme token value.
- A new component in `features/` that duplicates a kit component, or a kit change without a
  story and a green `visual:stories` run.
- A chart with no tooltip, no draw-in, or category labels that ellipsis/`----` at 390px.
- A chart imported from anything except Apex (`components/minimal/chart`, `components/kit`) or the
  two inline helpers (`svg-bars`, `svg-series`) — `raw-chart-lib` refuses recharts, d3, chart.js,
  nivo, victory, visx, echarts, highcharts.
- A NEW page without an area in `docs/design/route-template-map.json`, or a route added to
  `scripts/smoke-visual-live.mjs` without a matching `route_prefixes` entry
  (`route-template-map-missing`).

Neutrals are the MUI Minimal TEMPLATE's (Ravi 2026-09-27): grey scale `--grey-50…900` = `#FCFDFD…#141A21`, dark surfaces `#141A21` / paper `#1C252E` / neutral `#28323D`, light `#FFFFFF` / neutral `#F4F6F8`, text and divider as the template derives them; only brand + status hues are Mesha. `theme/theme-config.ts` and `app/minimal-tokens.css` must carry exactly those values (P0 `template-neutrals`); the retired green-tinted neutrals (`#0E1512`, `#161F1A`, `#94A89A`, `#F4F7F2` …) fail everywhere (P0 `retired-neutral-literal`). Write greys as `var(--grey-N)` / `rgb(var(--g500-rgb)/a)` or theme tokens.

## Tab, filter and pager clicks never reload the page

- **Tab / filter / pager clicks are soft navigations (guard: `soft-navigation`,
  `components/app/soft-navigation-guard.test.mjs`).** Links come from `next/link` or
  `@/components/no-prefetch-link`, never `next/dist/client/link` (the Pages Router Link: in `app/` it
  has no router and the browser reloads the whole document, painting the route skeleton on every
  click). GET filter/search forms use `<Form>` from `next/form`, never a native `<form method="get">`.
  URL-driven tab strips navigate through `useUrlTabNav` (`components/app/use-url-tab-nav.ts`,
  already inside `TemplateTabs` (components/app/template-tabs.tsx), `UrlTabs`, `SegmentTabs` (components/app/list), `SegmentedLinks`); programmatic filters / selects /
  date pickers / sort headers use `useUrlNavigate` (every `router.push/replace` under the shell also
  announces itself through `UrlNavRouter`). `loading.tsx` is for the first entry only.
- **A tab / filter click never hangs: panels are URL-keyed (guard: `url-keyed-panel` in design:guard,
  P0 with self-test; runtime: `interact|*|tab-not-selected` / `interact|*|stale-panel` in
  `scripts/r2-visual-audit.mjs`).** Ravi 2026-09-27: "the tab transition HANGS. Just switch the tab
  and show shimmer for the content to load." Every page that renders a URL-driven tab strip / segment
  / chip / select / pager / date filter renders its data panels through `UrlSuspense`
  (`components/app/url-suspense.tsx`) keyed by the params the panel reads (`watch`, or
  `[ALL_PARAMS]` + `ignore` for drawer / export params; `fallbackBy` gives each tab its own
  skeleton). Within one frame of the click the pressed tab is selected and the panel is its skeleton
  (shared blocks, same shape as the loaded panel); header / crumbs / tabs / filters stay mounted;
  content streams in when ready (an answer inside 30ms shows directly, no skeleton flash). Never keep
  the old panel on screen while the server answers, never a full-page `loading.tsx` skeleton, never a
  document reload. Best: the panel is an async server component inside `UrlSuspense`, so the new
  header/tabs stream before the panel data; a page that already awaited its data may wrap its panel
  JSX in place (the click-time swap alone keeps the click instant).

## Loading skeletons match the page (R2 item 7, 2026-09-27)

- **Loading shapes are composed, never drawn (guard: `hand-drawn-skeleton` in `design:guard`, P0; runtime:
  `scripts/r2-skeleton-iou.mjs` + the `skeleton` check of `scripts/r2-visual-audit.mjs`).** Every
  `app/(admin)/**/loading.tsx` and every in-page Suspense / panel fallback composes ONLY the blocks in
  `components/app/skeletons` (PageSkeleton, PageHeaderSkeleton, TabsSkeleton, FilterCardSkeleton,
  KpiRowSkeleton, ChartCardSkeleton, TableSkeleton, CardGridSkeleton, StatStripSkeleton, …), which render
  the SAME parts as the page (CustomBreadcrumbs anatomy, MUI Tabs, KpiGrid, Card + CardHeader, Table,
  the 64px template pager, the job-list grid). The loading.tsx mirrors its page 1:1 — same root class,
  same blocks in the same order, the page's own column / field / KPI / tab counts and rows per page,
  read from a shared `features/**/*-layout.ts` constant when the page has one. A page streamed behind
  `<Suspense>` uses its route's `loading.tsx` (or a `features/**/*skeleton*.tsx` built from the blocks)
  as the fallback, so a hard load and a panel stream paint one shape. No MUI `Skeleton` outside the
  blocks, no raw elements / inline style in a skeleton composition, no `.skel` / `.kit-sk-*` CSS. A
  block the page renders only with data sits in `OptionalSkeleton`. Proof: skeleton vs loaded
  top-level block IoU ≥ 0.8 at 1440 and 390, dark and light
  (`node scripts/r2-skeleton-iou.mjs --base <url>`, side-by-sides + overlay per route).

## Theme surfaces in both modes (R2, 2026-09-27)

- **Surfaces and colours come from the theme, in BOTH modes.** KPI/widget cards are the VERBATIM template
  widget summaries fed through `components/app/kpi-widget.tsx` (KpiWidget; client drill-in
  `kpi-widget-action.tsx`; row `components/app/kpi-grid.tsx`): EcommerceWidgetSummary for a real
  weekly series + percent, CourseWidgetSummary otherwise. KpiCard / StatStrip are deleted. Never paint a
  surface `common.white` / `#fff` / `grey.50-200` (P0 `light-surface-literal`; the dark shell shows a
  light box) and never select a `.Mui*` class in the legacy stylesheets to set a colour, background
  or border (P0 `legacy-css-mui-colour`; frame/minimal-theme/mesha-theme/menu-surface/globals.css
  only shrink). Tints are `varAlpha(theme.vars.palette.<c>.<x>Channel, a)` over the paper.
  A legacy rule that paints a bare `th`/`td`/`tr` repaints MUI tables too: exclude MUI parts
  (`td:not(.MuiTableCell-root)`, waivable `legacy-table-paint`), and no `rgb()`/`rgba()` colour
  literal in TSX (waivable `rgb-colour-in-code`).
## Template-fidelity guards (AFIX12, 2026-09-27)

`components/app/template-fidelity-guards.test.mjs` (runs in `npm test`), one rule id per recurring
audit defect on PR #294:

- `brand-primary-contained`: every contained `Button` names its colour (`color="primary"` for the
  main action). The theme default is the template's `inherit` (near-black / white), which broke the
  locked Mesha green on Sign in, Save password, Add disease, Add city.
- `mesha-logo-mark`: the logo tile is the shell's `मे` mark (Logo default); no `"M"` override;
  `global-error` never forces dark, runs `THEME_BOOT_SCRIPT` and loads `theme/fonts.css`.
- `form-submit-respects-field-guard`: a form holding a required `ThemedDatePicker` checks
  `event.defaultPrevented` before posting from `onSubmit` (the picker refuses the submit itself).
- `refusal-actions-never-throw`: a server action typed `Promise<…Error | undefined>` returns a
  refusal for a blank user field; `requiredString` (throws → "Something went wrong") only for `…_id`.
- `breakpoint-display-in-sx`: phone/desktop swaps of a `DataTable` use `sx` display breakpoints,
  never a stylesheet `display:none` on its class (DataTable's emotion styles win).
- `css-token-defined`: every `var(--token)` a component reads is defined in a stylesheet, set locally,
  MUI-generated, or has a fallback. Deleting a token means replacing its readers with theme values.
- `chart-ramp-distinct`: the categorical ramp (`components/app/chart-colors.ts`) has 7 distinct hues
  and no error red.
- `page-header-action-slot`: no stylesheet restyles `PageHeader` layout through a className.
- `no-card-in-card`: stacked phone table rows are divider rows (`border-bottom`), never bordered,
  rounded cards inside the table card; a KPI deck never sits inside another card (known offenders
  are a shrinking list in the test).
- `template-filter-toolbar`: filter bars are the template list toolbar (outlined TextField selects,
  MUI Chips with Clear); rows per page lives only in the table pager.
- `no-card-in-card` also covers the sx half: `features/procurement/procurement-sx.ts` phone load rows (`tr`) are dashed divider rows, never `border: 1px` + `borderRadius` cards inside the Loads card.
- `sales-config-no-legacy-css` (`features/procurement/market-config.test.mjs`): a class a converted /sales/config file still renders is not styled by `mesha-theme.css` / `frame.css` / `minimal-theme.css` (`.sellable-product-row input{box-sizing:border-box}` collapsed the MUI Item name input; `.market-config-line input` double-bordered every TextField). Delete the legacy rule when a page moves to the template.
- `sop-no-internal-codes` (`features/sops/sop-internal-codes.test.mjs`): SOP editors print no capture/question key or choice value (`return_to_pen`, `purchase_date`), the SOP detail dialog shows no SOP code and words the field type; the dialog is full screen at xs.
- `sop-editor-template-fields` (same test): SOP editor text fields are MUI outlined TextFields with their own `label` (no `.numlbl` label-above wrapper), and the legacy `.qcard input` paint excludes `.MuiInputBase-input`. In the weighing, inspection, shifting, PC Care, feed and toxin editors there is no `.numfield`/`.numlbl` wrapper, no native `<textarea>`/`<select>` (MuiTextField multiline / select, InlineSelect) and no `.qcfg-title` span (template `Typography variant="subtitle2"` group heading). `sop-select-option-title` (same test): SelectOption carries an optional `title` (backend option description) rendered on the MenuItem; a native select moved onto InlineSelect/FieldSelect keeps it.
- `sales-chart-phone-axis` (features/procurement/sales-chart-phone-axis.test.mjs): a month-axis template chart on a page passes template chart options (`responsive` below 600px: flat labels, `hideOverlappingLabels`, bounded `tickAmount`) so its ticks stay inside the card at 390; never patch the template card in components/minimal for it.
- `procurement-template-tabs` (features/procurement/sales-no-flicker.test.mjs): procurement and sales pages use UrlTabs (URL status/filter tabs with Label counts, useUrlTabNav), SegmentTabs (chip strips, `keepScroll`) or plain MUI Tabs (client state); never AnimatedTabs / StatStrip / KpiCard.
- `sop-library-skeleton-kpi-row` / `market-loading-mirrors-panels` (sop-internal-codes / sales-no-flicker tests): a route loading.tsx uses the same skeleton shapes as the page's own UrlSuspense fallbacks and the loaded blocks (KpiGrid + KpiWidget/CourseWidgetSummary -> `KpiRowSkeleton hero` without `icon`, never the retired StatStripSkeleton), and the page stacks its blocks with the same gap as its PageSkeleton.
- `sop-flow-phone-fit` (same test): the SOP Flow canvas keeps a 70% zoom floor under 600px and centres the scaled flow in a sizer of the scaled size.
- `labelled-filter-fields`: every WorklistFilters field (compare operator and value) shows its own
  label at a width that does not cut it.
- `iconify-offline-set` (`components/app/iconify-offline-set.test.mjs`): every literal Iconify name is registered in `layouts/template/iconify/icon-sets.ts`; an unregistered name loads from the Iconify API at runtime (flicker, missing offline / in the webview). Pick a registered icon or add its JSON.
- `info-hint-tap-target` (`components/app/info-hint-tap.test.mjs`): the shared InfoHint "i" has a 44px phone tap box (negative margin keeps the glyph footprint); never shrink it back to the 24px glyph at xs.
- `feature-server-fn-sx` (design:guard, p0): a `features/**` / `app/**` module without `"use client"` never passes a function `sx` (`(theme) => …`); as a Server Component it crashes the route ("Functions cannot be passed directly to Client Components", the /goats/[id] P0). Put themed blocks in a client file or use object sx.
- `feed-config-template-anatomy` (`features/feed/feed-config-template-anatomy.test.mjs`): /feed/config writes open the template quick-edit Dialog (DialogTitle + subject, TextField/FormSelect with `label` + `helperText` in a grid, outlined Cancel + contained Apply), never an inline form in a cell or header; session feeds are soft Chips with delete, adds are template Buttons, the session plan edit is the CardHeader action; tables are Card + CardHeader + Scrollbar + TableHeadCustom; Alerts hold text only (no table inside); no `.feed-table` / `.feed-scroll` / `.fld` / raw `<label>` / `small muted` / inline `style`.
- `kpi-map-truth` (`components/app/kpi-widget.test.mjs`): an Ecommerce-overview row in docs/design/page-template-map.md names the widget each KPI tile really renders (EcommerceWidgetSummary only with a weekly series; CourseWidgetSummary for trend-less tiles; AppWidgetSummary 7d; BookingWidgetSummary month).
- `table-scroll-template` (`components/app/table-scroll-template.test.mjs`): a table wider than its card scrolls in the template TableContainer or Scrollbar, never a bare `overflowX: "auto"` Box/div (shrink-only allowlist for files not yet converted).
- `chart-bar-shared-tooltip` (`components/app/chart-bar-tooltip.test.mjs`): one-measure BalanceStatisticsCards use the shared Apex tooltip (clamped inside the plot; a per-bar tooltip flips off a 390 viewport), formatter restated from the section; ConversionRatesCard pins Apex's fixed tooltip top-left below `sm` (useMediaQuery noSsr) and wraps long titles (escaped); no CSS on `.apexcharts-tooltip` (design:guard chart-tooltip-css).
- `herd-register-list-anatomy` (`features/counts/herd-register-list.test.mjs`): /counts/herd is the template user list: status Tabs with Label counts (`?status=`), URL-sorted Display ID head (`UrlSortHead`, backend `order=asc|desc` keyset), avatar lead cell, `minWidth: 960` table in the card's Scrollbar; no checkbox column without a bulk action.
- `kpi-long-figure` (`lib/kpi-figure.test.mjs`): the KPI adapter compacts a figure of a lakh or more (lakh / crore, 2 decimals) before the verbatim widget prints it, so it never runs under the corner icon / sparkline at 1440 or 390; the scale word and the exact value lead the visible sub-line ("₹ lakh · 2,84,663.25").
- `vaccination-plan-template` (`features/vaccination-plan/responsive-css.test.mjs`): /vaccination/plan and /plan/edit stay on template parts (CourseWidgetSummary KPI row, live table in UrlSuspense keyed by `page`, Card + CardHeader, TableHeadCustom, MUI Dialog / ToggleButtonGroup / Chip, Grid md 4 / md 8); no `.vplan` / `.vp-*` CSS or classes, no raw controls, no fixed-position div modals.
- `vaccination-template-anatomy` (`features/preventive-care-vaccination/vaccination-template-anatomy.test.mjs`, TR1-#10/#21): /vaccination matrices are template table Cards (CardHeader + InfoTip, Label legend, Scrollbar table, soft Label cells, 10-row pager; cohort farms as Tabs + Label counts); no `.cbm-*` class/rule, no colour literal. `theme-token-drift` guards palette tokens only; deleting an off-palette legacy rule literal is the fix.
- `routine-drawer-template`, `dark-alert-tint`, `kanban-card-raised`: the routine drawer renders only
  MUI form parts; dark standard Alerts are a 16% main tint (the locked dark `darker` steps are mid
  tones); work-board cards are raised paper with the amber needs-attention border.

- **Client-only APIs need `"use client"`; `next build` gates every push (guards:
  `client-api-without-use-client` in design:guard; the pre-push admin-web visual gate builds).** A module that calls useState/useEffect/useRef/useTransition/useRouter/
  useSearchParams/usePathname/useLinkStatus or wires a JSX `onX={…}` handler starts with
  `"use client"` (a server module that re-exported next/link's useLinkStatus broke `next build`
  on 2026-09-27). Typecheck does not catch this; only `next build` and this guard do.
- **No function crosses the server/client line (guard: `server-function-prop`, design:guard p0).** A
  SERVER module (reached from an app/ page/layout/loading without passing a `"use client"` file)
  never hands an MUI element a function `sx={(theme) => …}` / `sx={[(theme) => …]}`: MUI parts are
  client components, so the page renders "Something went wrong" ("Functions cannot be passed
  directly to Client Components", /sales/sold digest 3801663639) while typecheck and `next build`
  pass. Use an object sx with theme tokens, or make the module `"use client"`.
- **The shell is gated on every push (guard: r2 visual gate `shell|*`, scripts/r2-audit-checks/shell.mjs).**
  Sidebar root items + subheaders start at nav.left + 16px with padding-left 12px (template
  NavSectionVertical: content on the logo column), the active item is a translucent primary tint,
  header controls are transparent template IconButtons, no filter control or its floating label
  overlaps the Tabs strip, sort headers are TableSortLabel in the header colour (never link blue /
  underlined), and every stylesheet loads. Run `npm --prefix apps/admin-web run visual:gate -- --fast`
  before each push (the pre-push hook runs it: 5 shell routes + the routes your change touches; any
  new failure on those routes fails). Never `next build` into the `.next` a live server is serving:
  it serves UA defaults (40px list indent, grey buttonface squares, blue links) until restarted.

## TR-1 list and job-list rules (FXC, 2026-09-28)

- `list-toolbar-kebab` (`features/leave/toolbar-outside-panel.test.mjs`): a list toolbar ends in ONE
  ⋮ popover (RowMenu / OrderTableToolbar `menuActions`). Export, Columns, Reset, Sheet, Workbook are
  menu items, never green text / outlined buttons beside it. The Dense switch lives only in the table
  footer; a "shown / total" count shows only beside active filter chips (no stray "0 / 0").
- `config-register-toolbar-menu` (`features/configuration/register-toolbar.test.mjs`): a register
  CardHeader carries its title only; secondary list actions sit in the toolbar ⋮ and open their
  same-page drawers through `pushLocalOverlayUrl`.
- `sop-library-template-job-list` (`features/sops/sop-internal-codes.test.mjs`): the SOP libraries are
  the template job list: no KPI row, a page-level toolbar with no card (FilterBar `bare`), cards
  rendered through the template-derived `JobList` / `JobItem` slots (letter Avatar in the logo slot,
  status Label as meta, facets as facts, View / Edit in the card ⋮), no legacy wrapper classes
  (`kit-enter`, `sop-kit`) and no inline `style` on Alerts. The loading skeleton mirrors it.
- `segment-tabs-rounded` (`features/procurement/sales-no-flicker.test.mjs`): a standalone
  SegmentTabs strip is rounded (`var(--r-xl)`) like the template BankingOverview custom Tabs.
- `url-panel-min-width` (r2 plugin `column-fit`, P0 `layout|wider-than-column`): no page block is wider
  than the content column; `UrlPanel`'s `display: contents` box gives its children `min-width: 0`.
- `kpi-caption-not-title` (`features/procurement/animal-purchases.test.mjs`): a KPI widget caption
  never repeats its title ("Loads / Loads"); it carries extra information (e.g. "N+" when more loads
  exist than shown) or is omitted.
- `summary-strip-no-empty-share` (`features/verification-review/header-actions-tr1.test.mjs`): an
  InvoiceAnalytic summary strip prints a share caption only when there is a total to share (no "0%"
  in every cell). /verify's header side-panel buttons are outlined secondary buttons, not contained.
- `job-item-menu-label` (`features/sops/sop-internal-codes.test.mjs`): the JobItem ⋮ is icon-only, so
  it takes a declared `menuLabel` (aria-label, allowProps `IconButton:aria-label`), required by type
  whenever `menuActions` is set; SOP cards pass "More: <SOP name>".
- `sop-paging-client-only` (`features/sops/sop-internal-codes.test.mjs`): a card list that is already
  whole on the client pages through `JobList` `pagination.onSelect` + `window.history.replaceState`
  on the `page` param (deep links still open that page); never `router.replace` / Next Link per page
  click, which costs a server round trip that re-reads the whole list.

## Production bug CLASSES as guards (2026-09-26)

`scripts/lib/visual-pattern-guards.mjs` codifies six recurring visible defect classes as automated
checks, wired into the route visual lane so a new instance fails at push, not in production:

- `P-text-icon-overlap` — text drawn over an icon sibling in a flex/grid row.
- `P-wide-table-no-wrapper` — table wider than viewport with no `overflow-x` ancestor; every lane.
- `P-chart-axis-tiny` — chart axis / legend / SVG text below 11px on any viewport.
- `P-pinned-bar-blur-flicker` — sticky/fixed bar with `backdrop-filter` above actually-scrolling
  content (Android WebView repaint hazard).
- `P-drawer-filter-mismatch` — an open overlay whose `data-drawer-filters` / `data-export-filters`
  differ from the page's `data-page-filters`.
- `P-chart-hover-remount` — tooltip missing after hover, or re-mounted between three rAF ticks.

`docs/design/route-template-map.json` maps every admin-web area to its MUI Minimal v7.7.0 template
section (template lives at `~/mesha/mui/Minimal_TypeScript_v7.7.0`; licensed, **not** committed).
Full pattern → guard table and the how-to-add-a-page ordering: `docs/design/README.md` §5b + §5c.

## Page ↔ template map (guard: page-template-map)

- Every admin-web route has a row in `docs/design/page-template-map.md`: route → template page → the
  template section components per block, and the feature files that must import them. Build pages by
  composing those sections (copied verbatim into `components/minimal/`), fed our data and labels.
- Anti-pattern: hand-made KPI boxes / pastel `AnalyticsWidgetSummary` tint cards in dark, hand-built
  list cards instead of the template table anatomy (Card, Tabs with Label counts, toolbar,
  TableHeadCustom, TablePaginationCustom/Links). Use EcommerceWidgetSummary/CourseWidgetSummary/BankingWidgetSummary.
- Review check: a changed page that drops a mapped template import, or a new page with no map row, is a
  blocker; `design:guard` (`page-template-map`, p0) enforces the listed imports.
- **Template files are verbatim (guard `template-verbatim`, design:guard p0, self-test).** Ravi 2026-09-27: "use the SAME mesha-ui template across the pages and just put our content." Every file mapped in `docs/design/template-sources.json` (`components/minimal/**`, `layouts/**`) equals its source in `~/mesha/mesha-ui/vendor/minimal/Minimal_TypeScript_v7.7.0/next-ts` byte-for-byte, except import paths (`src/...` -> `@/components/minimal/...`, `@/layouts/...`, `@/theme/...`; package imports unchanged) and a leading `"use client"`. No prop, sx, copy, comment or behaviour edits inside them; Mesha colours come only from the theme. The manifest stores the sha256 of each normalised template source (the template is not in CI); refresh with `node apps/admin-web/scripts/refresh-template-hashes.mjs` after copying a new template file. A component with no true template source never lives under `components/minimal/` (no KpiCard / StatStrip / progress-item / AnimatedTabs wearing a template path): use the real template component (EcommerceWidgetSummary / CourseWidgetSummary / BookingWidgetSummary, the template Tabs + Label anatomy, EcommerceSalesOverview progress rows). Product behaviour (URL-linked tabs/pagination, data mapping, i18n labels) lives in `components/app/` adapters or feature files that only render template/MUI components and pass props/children (no own CSS, no raw px/colours). Remaining drift is the shrink-only list in `docs/design/template-verbatim-baseline.json` (a healed file must be removed; nothing may be added). The drift baseline keys each listed file on the sha256 of its bytes: editing a baselined file fails too (restore it to the template and drop the entry; never re-record a hash).
- **No pastel fallback on mapped pages (guard `page-template-no-pastel`, p0).** A page with a row in `docs/design/page-template-map.md` must not render `AnalyticsWidgetSummary`; KPI rows are `EcommerceWidgetSummary` / `CourseWidgetSummary` (via `components/app/kpi-widget.tsx`), charts are template chart cards (CardHeader + select), lists use the template table anatomy.
- **Template sections keep their client boundary (guard `section-client-boundary`, p0).** A file under `apps/admin-web/components/minimal/sections/` that calls a hook or passes a function `sx`/`(theme) =>` callback must start with `'use client'`; a server page rendering it would pass a function across the RSC boundary and crash at render. `next build` must pass before every push. Modules that import `useLinkStatus` / `useRouter` / `useSearchParams` / `usePathname` straight from `next/link` or `next/navigation` must start with `'use client'` (test `client-only-hook-directive`, `components/client-only-hooks-directive.test.mjs`).
- **No legacy card shells on mapped pages (guard `page-template-legacy-card`, p0).** A page with a row in `docs/design/page-template-map.md` must not render `className="card"` / `"wchart"` / `"wtable"` / `"kpi"` sections or `<h2 className="h">` headings: every block is a template section card (Card + CardHeader, e.g. `BankingBalanceStatistics`, `AnalyticsWebsiteVisits`, `EcommerceSaleByGender`) fed our data, and the legacy CSS behind those classes is deleted as pages stop using it.
- **Sidebar and header are the template dashboard layout (guard `shell-nav-template`, p0; test `components/sidebar-viewport.test.mjs`).** The nav is `NavSectionVertical` / `NavSectionMini` (layouts/template/nav-section, verbatim) inside the template-derived `layouts/app/dashboard/nav-vertical.tsx` / `nav-mobile.tsx`, fed by the backend bootstrap nav. The whole nav scrolls in the template `Scrollbar`, logo fixed; only the active group opens (no `default_open` subtrees); no custom nav footer (`navBottom`, `msh-foot`, `navigation.footer` - the template only has the optional NavUpgrade card, which we do not use); the phone nav is the template drawer (`var(--layout-nav-mobile-width)` over the template backdrop, no full-width/opaque scrim, no extra close button; Android Back closes it). Header right order follows the template: notifications (IconButton + Badge + solar bell) -> theme toggle (template Settings slot) -> account; the park scope is the template-derived WorkspacesPopover (trigger and list, options are scope links) in the header left slot (`layouts/app/components/workspaces-popover.tsx`).
- **Server-rendered sections need 'use client' for function sx (guard `section-server-fn-sx`, p0).** A template section under `components/minimal/sections/` is rendered straight from Server Component pages; if it styles with `sx={(theme) => …}` / `sx={[(theme) => …]}` it must start with `'use client'`, or the function crosses the server/client boundary and the page throws "Functions cannot be passed directly to Client Components" (whole route falls back to client rendering or 500s).

## Charts: template Chart + useChart, verbatim (R2CHARTS, 2026-09-27)

**Charts are the template's `Chart` + `useChart`, verbatim (Ravi 2026-09-27, R2CHARTS).** The
  /sales/sold month-by-month bug class (grey hover column, clipped tooltip, raw "bar chart with 1 data
  series" text, a figure on every bar, fat neon bars, two-line "Apr 2025" axis, three charts in one
  card under overline sub-headings) is guarded, P0, in `design:guard`
  (`scripts/lib/chart-template-guards.mjs`): `chart-wrapper-verbatim` (components/minimal/chart
  chart.tsx / use-chart.ts / styles.css are the template's bytes, sha256-pinned),
  `chart-data-labels` (no `dataLabels.enabled` / bar `total` labels in our charts),
  `chart-states-override` (no `states:`), `chart-bypasses-usechart` (ApexCharts only via
  Chart + useChart), `chart-raw-colour` (`colors`/`fillColor` never a raw `var(--…)`, hex or
  `color-mix()`: resolve palette channels with `chartColor` / `chartRamp` from
  `components/app/chart-colors`; bars lead with `primary.dark`), `chart-tooltip-css` (no
  `.apexcharts-*` CSS outside the template chart styles). Runtime half: the r2-visual-audit plugin
  `scripts/r2-audit-checks/chart-hover.mjs` hovers every chart and fails on a clipped tooltip or the
  raw a11y string (`components/app/apex-globals.ts` turns ApexCharts' SVG `<title>` off). One chart
  per card with the template anatomy (CardHeader title/subheader + ChartSelect action, ChartLegends
  with totals, then the chart: EcommerceYearlySales / AppAreaInstalled); short month labels on the
  axis with the year or range in the select; no plot scrolls sideways inside its card. A shared (whole-column) tooltip stays at
  most `SHARED_TIP_MAX_SERIES` (6) rows; a stacked chart with more series uses the per-segment
  tooltip (`shared: false, intersect: true`) so it never outgrows its card (test `chart-tooltip-fits`).
- **Selected tiles, kanban cards, readable ledgers (TR1-#26/#29).** A selected KPI tile uses the template selected-card ring (`0 0 0 2px text.primary`), never a coloured outline (`action-center-kpi-selected`). Board cards are template kanban items with at most two Labels, other facts as caption lines, no placeholder box in empty columns (`action-center-card-anatomy`). Table cells wrap; no ellipsis-only cell text, and column widths fit the 1440 content column before the Scrollbar takes over (`adherence-ledger-readable`).
- **Page rhythm / raw codes / Ask Mesha dock / blur / preflight (R3OPS, TR1).** Page root = `screen on` grid, never a fragment starting with PageHeader (r2 audit `rhythm|header-gap`). No snake_case backend code in a Label/Chip (`rhythm|raw-code-label`; humanizeEnum/optionLabel). Ask Mesha docks in the header at every width as a template IconButton, never a floating bubble, and the header slot is tracked live (`ask-mesha-docked`). A page with no nav leaf opens and highlights its module group (`nav-module-fallback`). The phone 44px floor covers Chip / ToggleButton / date-range clear width; the r2 tap check skips only visually hidden `.sr-only` controls (`tap-skip-visually-hidden`); anchored template popovers are not dialogs for the backdrop rule (`popover-not-dialog`); no hand-made `.modal` shell, dialogs are MUI Dialog (`action-center-filters-dialog`). The header park switcher shows on every park-scoped route and hides only on routes tagged own-filter / no-park in the one list in mesha-shell.tsx (`top-bar-park-rule`). No backdrop blur on scrims/veils (`no-blur-scrim`). Template pseudo elements that set `borderWidth` also set `borderStyle` (Tailwind preflight; `preflight-pseudo-border`).
- **Kanban + calendar anatomy (TR1-#24/#25).** Kanban cards are the template item: priority arrow, one-line name, one caption line, ItemInfo row (readings, avatars); no chip strip, no progress bar, and an empty column is the bare list (guard `kanban-template-card`). The calendar card holds only CalendarRoot: toolbar with solid error "Today" + filter icon; window / owner / workstream in the template 320px filters drawer with CalendarFiltersResult chips when applied; no heading toggle, no chip row, no tab row, no dead "Add event" (guard `calendar-template-toolbar`).
