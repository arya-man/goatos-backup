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
Navigation shows the target's skeleton at once (guard `pending-route-skeleton`): a path-changing link
click, any router.push / replace below the shell (announced by UrlNavRouter) and the header back arrow
pass the target href to the shell, which paints `PendingRouteSkeleton` (ROUTE_SKELETONS registry: every
loading.tsx is registered) in the same frame; a URL-branching loading shape reads
`usePendingRouteSearch()` first. A route with a library/editor split keeps ONE predicate for the page,
its UrlSuspense `fallbackBy` ("a|b" = any param is "1") and its loading.tsx (guard
`sop-editor-predicate`: SOP builder = compose=1 or new=1). Tab strips are never `memo()` (guard
`tab-strip-no-memo`: a memo'd client component remounts on every RSC navigation); a pure view switch
inside a URL panel is a LocalViewToggle, not a server round trip (guard `gain-view-local`). The audit's
chart-black rule reads drawn marks (path/rect/circle/polygon) only. No dimming while navigating
(guard `pending-dim`, P0): the affected area shows its skeleton; nothing fades the old content. No feature
toggles `.wfbusy` (stale clickable rows); the shell's pending skeleton wrapper restates the 24px page grid for
the root it wraps (guard `pending-skeleton-root-gap`, one mechanism; PageSkeleton adds no gap of its own); skeleton twins are checked at 1440 AND 390 (`--skeleton-profiles 1440-dark,390-dark`).
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
- **A tab / filter click keeps the scroll position (TR3-P1-3; guard `tab-scroll-kept`: r2 audit P0 `interact|*|scroll-jump`, test `scripts/r2-visual-audit.test.mjs`).** Same-route tabs / filters navigate with `scroll: false` (UrlTabs, LinkSelect, router.push) or are client state (the /vaccination cohort park tabs), and the panel's skeleton twin holds the loaded height, so the page does not move: scrollY after settling stays within 150px of scrollY at the click (after any pre-scroll), except when a shorter result only clamps the page to its new bottom. Reproduce with a wheel scroll + mouse click, not a locator click (Playwright's scrollIntoView pre-scroll is not a jump).
- **The pre-push lane runs the skeleton twin check on touched routes (TR3 FINAL; guard `skeleton-on-touched`, same test).** `admin-web-visual-gate.sh --pre-push` / `--fast` runs `skeleton` at 1440 dark AND 390 dark on every route the push touches, on top of scan + interactions: a layout change to a page ships its loading twin in the same push, or the gate fails.
- **A tab / filter click never hangs: panels are URL-keyed (guard: `url-keyed-panel` in design:guard,
  P0 with self-test; runtime: `interact|*|tab-not-selected` / `interact|*|stale-panel` in
  `scripts/r2-visual-audit.mjs`).** Ravi 2026-09-27: "the tab transition HANGS. Just switch the tab
  and show shimmer for the content to load." Every page that renders a URL-driven tab strip / segment
  / chip / select / pager / date filter renders its data panels through `UrlSuspense`
  (`components/app/url-suspense.tsx`) keyed by the params the panel reads (`watch`, or
  `[ALL_PARAMS]` + `ignore` for drawer / export params; `fallbackBy` gives each tab its own
  skeleton). Within one frame of the click the pressed tab is selected and the panel is its skeleton
  (shared blocks, same shape as the loaded panel); header / crumbs / tabs / filters stay mounted;
  content streams in when ready (the skeleton commits with the click itself, no timer: a 30ms delay starved behind the router transition and left the old panel on screen, J3 P0-1, guard `url-panel-no-timer`). Never keep
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
- `template-date-fields` (`components/app/template-date-fields.test.mjs`): filter-bar dates are the template MUI X DatePicker (DD/MM/YYYY, trailing calendar icon): one field in WorklistFilters, a start / end pair in DateRangeField (MUI Stack row from sm, stacked full width on phones); never a custom calendar as a TextField inputComponent, no `.kit-daterange*` CSS.
- `tasks-one-tab-strip` (`features/leadership-tasks/tasks-one-tab-strip.test.mjs`): /tasks has one tab strip (the card's status tabs); the scope is the toolbar's first select and the board / list switch sits with the page action.
- `counts-breakdown-template-rows` (`features/counts/counts-breakdown-rows.test.mjs`): /counts/breakdown pen rows are the template collapsible row (expand IconButton + rotating arrow, soft Label composition capped with "+N", combinations as TableRows on `background.neutral` with dashed dividers); no green open-row fill, `.dimchip`, `.xtoggle`, raw `<tr>/<td>`.
- `list-no-kpi-row` (`features/approvals/no-kpi-row.test.mjs`): /approvals and /alerts (template order list) carry no KPI tiles; counts live in the tab Labels, the alerts checks-run figure closes the toolbar.
- `template-table-padding` (`components/app/table-cell-padding.test.mjs`): table cells keep the template padding (MUI 16px; `size="small"` / Dense 6px 16px); no global stylesheet re-pads first / last cells to 24px or pins header heights.
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
- **Root special files render without the root layout (guards: `global-error-prerender-no-providers`,
  `scripts/global-error-prerender.test.mjs` in npm test; `prerender-repeat`, `npm run check:prerender-repeat`
  in ci-local).** Next 16 prerenders `/_global-error` from a tree with NO app/layout.tsx but WITH the root
  `app/loading.tsx` as the Suspense fallback; React draws that fallback only when the page chunk is not
  ready yet, so a loading tree that reads `theme.vars` failed `next build` intermittently ("Cannot read
  properties of undefined (reading 'palette')", FIXJ-BUILD on 7e181ce32). A root `loading.tsx` wraps its
  content in `EnsureAppTheme` (theme/ensure-app-theme.tsx: pass-through under the root layout, the app
  provider stack otherwise) and `global-error.tsx` mounts `AppThemeProvider` itself. A build that passed
  once is not proof for a race: the prerender-repeat check runs the prerender step 3x from one compile.
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
- **Board drag works on touch; the lifted card is paper (guard `task-board-touch-dnd`).** /tasks drags with dnd-kit (Mouse 5px, Touch 200ms/5px, Keyboard Space), never HTML5 `draggable`; the DragOverlay is the same card on `background.paper` + `customShadows.z24` + slight tilt/scale, the source slot the template `--dragging` placeholder. Proof: `npm run e2e:task-board-dnd` (1440 mouse + 390 touch, writes held and aborted) and the r2 plugin `task-board-dnd`.
- **Kanban + calendar anatomy (TR1-#24/#25).** Kanban cards are the template item: priority arrow, one-line name, one caption line, ItemInfo row (readings, avatars); no chip strip, no progress bar, and an empty column is the bare list (guard `kanban-template-card`). The calendar card holds only CalendarRoot: toolbar with solid error "Today" + filter icon; window / owner / workstream in the template 320px filters drawer with CalendarFiltersResult chips when applied; no heading toggle, no chip row, no tab row, no dead "Add event" (guard `calendar-template-toolbar`).
- **Converted files stay legacy-free (FIXJ3, J1 P0-1/P0-2/P1-1..3/P1-5; guard `legacy-free-zone`, p0, `apps/admin-web/scripts/lib/legacy-free-zones.mjs` + `scripts/legacy-free-zones.test.mjs`).** A file (or directory prefix) listed in `apps/admin-web/scripts/legacy-free-zones.json` may not use a className that any legacy stylesheet or CSS module defines (`card`/`hd`/`bd`/`btn`/`qcard`/`hrow`/`muted`...), a `style={...}` prop, a native `<button>/<input>/<select>/<textarea>/<table>/<tr>/<td>/<th>` (a hidden `<input type="file">` inside a template upload Button and an `<input type="hidden">` form field are fine: neither draws anything), a `lucide-react` icon, a `.css` import, a hex/rgb colour literal, or an embedded stylesheet (`<style>` element or string `GlobalStyles`; a feature stylesheet moved into a TSX string is still a feature stylesheet). Build with template sections / MUI (Card + CardHeader, Button, IconButton, TextField select/multiline, Table + TableHeadCustom), template Iconify icons and theme sx. When you convert a file, add it to the zones list in the same commit and delete the legacy selectors it stopped using once `grep -rn` proves no other file uses them. Zones only grow.
- **Shared adapters are thin template wrappers (FIXJ4, J1 P1-4; guard `legacy-free-zone` on the adapter files listed in `apps/admin-web/scripts/legacy-free-zones.json`, plus the `legacy-class-use` / `inline-style-prop` / `lucide-import` ratchets).** `components/ui-primitives` (Tag = template Label, ClipText = Box sx), `components/data-table` (IdentityCell = template user-list name cell, actions cell sx, `visuallyHidden`), `components/themed-date-picker` (the MUI X DatePicker, DD/MM/YYYY, ISO hidden input; no `<details>` + `.move-date-*`, no CSS module), `components/review-queue/review-queue-ui` (StatusChip = soft Label), the shell-unavailable / route-error screens (`components/app/state-panel.tsx`, never `.kit-state*`) render only MUI / template parts with theme sx and keep their public props, so callers do not move. A `components/app` or `components/*` adapter never renders a legacy stylesheet class, a CSS module, `style={}` or a lucide icon; the legacy rules they used are deleted with them (`npm run design:guard:update-baseline` in the same commit).
- **Icons are template Iconify only; lucide-react is gone (FIXJ4, J1 P1-3; guard `lucide-banned`, design:guard p0 with self-test: any `lucide-react` import or a `lucide-react` entry in apps/admin-web/package.json fails, and the finding names the replacement).** The ONE lucide -> Iconify mapping table is `apps/admin-web/scripts/lib/lucide-iconify-map.mjs` (template names; spinners are MUI `CircularProgress`). `layouts/template/iconify/icon-sets.ts` is verbatim template code (sha256-pinned), so an icon the template set lacks goes in the extra offline registry `apps/admin-web/components/app/iconify-extra.ts` (exact Iconify JSON body, solar first) and renders through `AppIcon` (`components/app/app-icon.tsx`: template names fall through to `Iconify`, extra names are registered offline, no network fetch); guard `iconify-offline-set` accepts names from both files and nothing else. Table pagers are `TablePaginationLinks` (`data-pager` hook for the smoke / webview lanes; 44px phone taps come from the shell's PhoneTapStyles, never a pager-specific rule), never the legacy `.pager2` class; SegmentTabs / LocalViewToggle never carry `.metricseg`; the URL-nav event is `URL_NAV_EVENT` (`url-nav:navigate`).
- **URL panels keep the page still (FIXJ4, /operations/audit P0s: tab moved 108px, actor filter scrolled 562px, strip / operator links reloaded the document; guards `url-panel-click-after-react` + `url-panel-holds-page-height` in components/app/url-panel.test.mjs, `audit-strip-no-simplebar` in features/operations-audit/audit-analytics.test.mjs, runtime r2 interact full-reload / scroll-jump / fallback-jump).** UrlPanel hears link clicks and GET submits on `window` in the bubble phase, AFTER React dispatched them: a capture listener swapped the panel to its skeleton first, unmounting a link inside the panel before next/link saw the click, and the browser followed the href as a full reload. From the click until every pending panel shows content again, a scrolled page keeps its height (`document.body` min-height), so a swap never clamps the page to the top; the floor lifts after, and a shorter result only clamps to its new bottom. A block that sits ABOVE a URL-keyed tab strip and is itself re-keyed never mounts the template Scrollbar (SimpleBar is 0px tall for its first client frame); it scrolls sideways in a plain overflow box. A toolbar search narrower than its hint below sm passes `SearchTextField phonePlaceholder` (the contract's `filter.search_label`), never a clipped placeholder (r2 text-fit|placeholder-clipped). E2E scripts drive the template UI through roles, labels and `data-testid` hooks, never a legacy class the template parts no longer render (guard `sop-builder-e2e-selectors`, apps/admin-web/scripts/sop-builder-e2e-selectors.test.mjs); `PagedRows headCells` renders the template TableHeadCustom (the /sales loadwise register), header cells aligned with their body cells.
- **Local CI is strict: every push, every branch, no silent skip (FIXJ-CI, Ravi 2026-09-30 "enforce local CI/CD strictly"; J1 P0-4 + CI gaps; guards `push-hook-freshness` (lanes + feature-branch execution probe, self-test `tools/ci/check-push-hook-freshness.test.sh`) and the skip ledger).** `make ai-setup` installs `tools/agent-hooks/pre-push.hook` verbatim. On EVERY push to ANY branch whose commits change an admin-web input (`apps/admin-web/**`, `docs/design/**`, package files, `tools/ci/admin-web-*`, `backend/internal/adminui/**`) it runs `tools/ci/admin-web-push-gate.sh`: design:guard, typecheck, npm test, next build and the visual gate (shell + touched routes, skeleton-on-touched). A missing guard or gate script FAILS the push. The lanes judge the work tree, so push from the worktree whose HEAD is the pushed commit with clean admin-web inputs. Run the lanes ahead of the push, one command each (the watchdog kills a command after 10 min): `tools/ci/admin-web-push-gate.sh --run design-guard|typecheck|unit-tests|next-build|visual-gate`; the push then reuses every PASS for the same admin-web input tree. `tools/ci/check-push-hook-freshness.sh` (run-local-ci common) fails when the installed hook or any installed copy is stale, a lane is missing, or the hook does not block an admin-web push to a feature branch. Never `git push --no-verify`.
- **A skip needs a written reason and lands in the skip ledger (`tools/ci/goatos-skip-ledger.sh`).** `GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1`, `GOATOS_FAST_LOCAL_CI=1`, `GOATOS_CI_ONLY_STEP=...` and pointing `GOATOS_ADMIN_WEB_BASE_URL` at an already-running app for the visual gate are REFUSED unless `GOATOS_SKIP_REASON="..."` (>= 12 chars) is set; each skip is appended to `<git-common-dir>/goatos-ci/skip-ledger.tsv` and printed in the run-local-ci / push-gate summary. A lane that cannot run for a missing prerequisite (no live app) is a SKIP row + ledger entry, never an `echo`. A skipped lane proves nothing: say so in the handoff.
- **Legacy debt only shrinks, per file, everywhere (FIXJ-CI; design:guard ratchet checks with self-test, `apps/admin-web/scripts/lib/shrink-ratchets.mjs`).** Baselined 2026-09-30 in `scripts/check-design-system-waivers/design-system-waivers.json` (`check|file`): `legacy-class-use` (className tokens a legacy stylesheet defines: frame / minimal-theme / mesha-theme / globals / feature .css), `legacy-card-reachable` (card / hd / bd / wchart / wtable / kpi / chartcard shells, `<h2 className="h">`, in any file an `app/**/page.tsx` imports, not only page-template-map files), `native-control` (native select / input / button / textarea / table, whole-file multi-line match), `inline-style-prop` (`style={…}`), `lucide-import` (the template uses Iconify), `raw-px-hex-literal` (raw `12px` / `#hex` in TS/TSX), `legacy-css-rules` (style RULES per stylesheet incl. features/**/*.css and *.module.css; `legacy-css-ceiling` reads the same ceilings). New code never adds to any of them: a new file has 0, a file may not exceed its count, and an allowance above the current count (or a stale waiver) FAILS until you lower it in the same change with `npm run design:guard:update-baseline` (it only lowers and never adds a waiver). Delete a legacy selector only after proving it dead (no string literal that can carry a class names it; see the FIXJ-CI proof method in the commit log) and lower `legacy-css-rules` with it.
- **/herd-signals is template-only (FIXJ2-HS, J1 P0-1/P0-2/P1-1..3; guard `fixj2-area-template-only`, `apps/admin-web/features/herd-signals/herd-signals-area-template-only.test.mjs`, npm test, self-test; plus `legacy-free-zone` on `features/herd-signals/` and `app/(admin)/herd-signals/`).** Every herd-signals .tsx has no className, no `style={{`, no native control / table markup, no lucide, no hand `<svg>`, no `datetime-local`; the full-screen history range is the MUI X DateTimePicker (DD/MM/YYYY HH:mm) beside a ToggleButtonGroup. The responsive tables (phone field grid labelled by `data-l`, desktop column floors, sticky first column, selectable rows, delta tones) live in `features/herd-signals/herd-signals-sx.ts`; no `.herd-signals-page` / `.hs-*` rule may return to the legacy stylesheets. Icons come only from the registered offline set (layouts/template/iconify/icon-sets.ts is template-verbatim, so a new name cannot be added there).
- **/people is template-only (FIXJ2, J1 P0-3/P1-1..3; guards `legacy-free-zone` on every `features/people/*` file but the `screen on` page root, `operators-roster-template-only` in `features/people/vaccination-operators-screen.contract.test.mjs`, `notification-matrix-header-floor`, the 44px Checkbox hit box in `features/mobile-smoke-targets-contract.test.mjs`).** The Vaccination operators roster and drive assignment are template Cards (CardHeader action, TableHeadCustom in a Scrollbar with a width floor so the weekly-schedule column scrolls inside the card instead of clipping at 1440, soft Labels); caps are MUI TextFields + IconButton, the operator picker is ToggleButtons, the leave picker is the MUI X DateCalendar with a Day slot (past / booked / own days disabled, span band), and save feedback is a Snackbar + Alert, never a `#toast` div or `dangerouslySetInnerHTML`. The access editor's pills are pressable template Chips (`aria-pressed`), the notifications matrix is a Card + Scrollbar table with Labels, and links over LocalOverlayLink are `MuiLink component={LocalOverlayLink}` in sx, never `.celllink`. No `.fld` / `.note` / `.callout` / `.pa-*` / `.cal-*` / `.lv*` class, CSS module, inline style, native control or lucide icon comes back; the legacy rules behind them are deleted.
- **FIXJ1 judge fixes (J2/J3 on 7e181ce32; each has its guard).** (1) A header action that exists on one tab only moves the tab strip: /people "Add person" renders on every desk (opens the drawer in place on All People, navigates there with it open elsewhere; test `people-header-action-every-desk`). (2) An Alert with two or more action buttons is `ActionAlert` (components/app/action-alert: actions drop under the message on phones, labels never wrap; test `alert-actions-stack`). (3) No dead primary: a primary the principal cannot use is not rendered disabled; show the backend reason as visible text in its slot (/tasks "New task" without assignees; test `tasks-new-task-reason`, r2 `text-fit|dead-primary`). (4) Every `page.tsx` under `app/` has a row in docs/design/page-template-map.md, mapped or in "Routes with no template page of their own" (design:guard `page-template-map-coverage`, p0); /tasks-preview (fake fixture shell) is deleted. (5) No raw ids as text: no 8-hex hash, UUID or snake_case table name in a cell / card header / list line (goat passport `goat_identity_events`, obligation hashes, the green `.gid` chip); empty states are `EmptyState` (EmptyContent), never a bare Typography line (test `passport-empty-and-ids`, r2 `text-fit|raw-id-text`). (6) Button labels hold one line and unrotated chart axis labels never intersect: a category chart with long labels passes `responsive` (below 600px: short labels, rotate -45, trim) (r2 `text-fit|button-label-wrap`, `text-fit|axis-label-overlap`). (7) Tab / filter interactions are audited at 1440 dark AND 390 dark (taps), the stale-panel check reads only the panel that went `aria-busy` (it must itself carry `data-url-panel-pending`; a skeleton elsewhere no longer counts), and a click fails `interact|*|fallback-jump` when the pressed control moves > 8px at any 100ms sample of the transition or `fallback-shape` when the click-time panel skeleton's union box has IoU < 0.8 with the landed panel (r2-visual-audit, P0, `fallbackTwinFails` self-test). (8) One template widget kind per KPI row: a month trend stays on the Course card with the change leading its sub-line ("+400% · …"; KpiWidget no longer renders BookingWidgetSummary), and a deck's short row fills its width (/feed/analytics 3 App + 2 Course at md 6) (test `kpi-row-one-kind` in kpi-widget.test.mjs, r2 plugin `kpi-row|mixed-kind` / `kpi-row|empty-slot`, P0). (9) Form dates are `FormDateField` (components/app: MUI X DatePicker, floating label, DD/MM/YYYY, ISO hidden value, submit refusal), never a caption above a summary button that repeats it; required selects show the label asterisk (FormSelect) and backend copy never spells "(required)"; no disabled submit when a form cannot post (the Alerts say why) (tests `form-date-field-template`, `herd-register-dialog-form`). (10) A tab strip whose backend serves only the active filter's total carries the count Label on the SHOWN tab only, the same on every tab (never page-1 row counts) (test `shown-tab-count`). (11) P2 polish: drawers are 320 / 480 (the Weights export form is 480, its period named once; test `weights-export-drawer`); no note names a control that is not on screen (care-coverage "then Apply"; `care-coverage-no-apply-note`); select values read like values ("All", `select-value-sentence-case`); the back arrow keeps an 8px gap to its title in page and twin (`back-arrow-gap`); list footers are the template pager, "1–n" (`template-pager-footer`); placeholders fit their field (r2 `text-fit|placeholder-clipped`); wide tables wrap long text cells instead of cutting the last header (`audit-table-fits`), and breakdown headers hold one line (`farm-born-headers-one-line`); horizontal-bar value axes use four ticks on phones. (12) A URL panel's skeleton commits in the same update as its pending state (no timer between click and shimmer: the 30ms delay starved behind the router transition, the J3 P0-1 root cause; test `url-panel-no-timer`). (13) Toolbar dates are `FormDateField` too (/people Clock; `clock-date-field`); wide matrix headers break at words onto two lines, then ellipsis + Tooltip (`notification-matrix-header-floor`); on phones every toolbar control is full width (LinkSelect `fullWidth` in an `orderToolbarFilterSx` box; `audit-toolbar-phone`); wide tables keep their first column sticky through `STICKY_FIRST_COLUMN_SX` (`sticky-first-column`).
- **FIXJ5 phone chart axes (LAND3 P0 on a9212b999).** Below sm a chart axis never prints labels into each other: day axes on the shared series adapters (components/series-charts.tsx `SeriesLinesChart` / `StackedColumnsChart`) take `PHONE_DAY_AXIS_LABELS` (five flat dd/mm ticks, `hideOverlappingLabels`, the tooltip keeps the full date) instead of a -45deg fan of full dates; a horizontal-bar value axis (Feed mix) keeps three ticks with `hideOverlappingLabels` and narrower category labels. Fix it in the page / adapter chart options, never in components/minimal (test `phone-day-axis` in components/series-charts-phone-axis.test.mjs). The r2 `text-fit|axis-label-overlap` check (Apex label text read from its tspan, so details no longer print "80,00080,000") declares no profile filter, and the FULL audit runs it on every app/(admin) route at 1440 dark, 1440 light and 390 dark; the fast pre-push lane caps touched routes at 8, so a chart route past the cap (/feed/analytics) is judged by the full run (test `chart-label-every-route` in text-fit.test.mjs).
- **/tasks header twin (FIXJ5, revised by FIXJ6 on a coordinator decision).** The loading twin mirrors the NORMAL header: the board / list toggle (101x54 below md, 73x40 up) and the 108x36 "New task" button (`TASK_HEADER_ACTION_WIDTHS` / `_HEIGHTS` in features/leadership-tasks/tasks-layout.ts). The rare "No one can be given a task" note (no assignable people, seen with local data) keeps its own full row below md on the page (`TASK_NO_ASSIGNEES_NOTE_*`) but is not drawn by the skeleton; mirror the state real users see, not the local fixture. `PageHeaderSkeleton` action widths may be strings and are capped at the row (`maxWidth: 1`) (test `tasks-loading-mirror`).
- **A converted page root is `PageRoot` (FIXJ3; guard `page-root-sx`, `apps/admin-web/components/app/page-root.test.mjs`).** `components/app/page-root.tsx` is the page column grid (24px gap, min-width 0) as theme sx with a `data-page-root` hook, never `className="screen on"`; its loading twin is `PageSkeleton root="page-root"`, and the r2 audit (page-rhythm, skeleton block walk) anchors on `[data-page-root]` like `.screen`.
- **No `screen on` page root anywhere (FIXJ6; guard `page-root-sx`, `apps/admin-web/components/app/page-root.test.mjs` "no page root or skeleton twin uses the legacy screen/on classes").** Every page root is `PageRoot`; `PageSkeleton` defaults to `root="page-root"` (the only other root is `""` for a bare Box page). The legacy `.screen.on` / `.wrap>.screen` grid died with frame.css and mesha-theme.css.
- **The legacy stylesheets are gone; tokens are theme values (FIXJ6; guards `legacy-css-ceiling` in `apps/admin-web/scripts/legacy-css-ceiling.test.mjs` (the files stay absent, no ceiling, no import), design:guard `brand-lock` reading `theme/mesha-tokens.ts`, `page-root-sx`, `pending-route-skeleton`, `plan-header-action-width` in `features/sk3-skeleton-twins.test.mjs`).** `app/frame.css`, `app/minimal-theme.css`, `app/mesha-theme.css` and `layouts/mesha-layout.css` are deleted and never come back (a merge from main that re-adds one is template-fied in the merge). The Mesha palette + shell tokens live in `theme/mesha-tokens.ts` (`MESHA_TOKENS_DARK` / `_LIGHT`) and `theme/app-baseline.tsx` (`AppBaseline`, rendered once in the theme stack) emits them as CSS variables plus element-level baseline only: body type, anchors, placeholders, the WebView phone contract (16px inputs, 44px tap floor in the page content, 540px table floor below 861px, header / nav drawer taps via template layout classes), route-busy / route-pending, theme hand-off, reduced motion, and the `data-dense` Dense switch. Never add a page class there. The shell frame is the template `DashboardContent` + an sx page column `[data-page-column]` (no `.main` / `.wrap`); every page root is `PageRoot`. Table column heads hold one line from the theme (`MuiTableCell` head `whiteSpace: nowrap`); a page that wants wrapping heads sets it in sx. A class string that remains (`msh-side`, DataTable `meta.cellClassName` such as `lt-days-col`) is a JS / sx hook with no stylesheet behind it. Tests that asserted a legacy rule read `legacyCss()` (`scripts/lib/legacy-css.mjs`, "" for a deleted file) and assert the template part instead. A header action that changes with state keeps one slot width in both states so its twin matches (/vaccination/plan `PLAN_HEADER_ACTION_WIDTH`).
- **FIXJ10 judge fixes (J2B on c4bd347ff; each has its guard).** (1) Drawer / dialog form fields share one width: every field in a form column is `fullWidth` like the template form TextFields; never `minWidth={0}` on a column-body select (`PeopleFormSelect fullWidth`; /people Add person Department + Designation had collapsed to ~60px). Guard: r2 drawers lane `drawer|field-collapsed` (P0, `probeFieldWidths` in `apps/admin-web/scripts/r2-audit-checks/text-fit.mjs`; a field narrower than 40% of the widest field in the overlay). (2) DECIDED no dead controls covers page bodies and drawers, not only the header: no dimmed idle Apply / Save / contained button anywhere. A staged filter bar (`WorklistFilters deferApply`, the vendors bar) renders Apply only once something is staged and shows `loading` while it lands; a matrix / form Save renders once something changed; a slider Apply keeps its slot but is `visibility: hidden` + out of the tab order until moved; a row action a seat cannot use is replaced by its reason as text. Guard: r2 text-fit `dead-primary` over the whole page content (`probeDeadControls`, P0) and the drawers lane `drawer|dead-control` (P0) on every opened overlay; pagers and a loading button pass. (3) Screens behind a pick are audited: `PICK_STATES` in `apps/admin-web/scripts/r2-visual-audit.mjs` (the /people Vaccination roster after a park pick) runs the text-fit + dead-control probes on the picked state (guard `pick-state-audit`). A table-cell row action is one line (`whiteSpace: nowrap`), and an inline cell edit is a small template TextField with its unit / save tick as an end adornment.
