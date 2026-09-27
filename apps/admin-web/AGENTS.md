# Admin Web Agent Context

Read first:

- `../../context/frontend/current-admin-web-scope.md`
- `../../mock/goatos-dashboard-mock.html`

## Local Dev Server Safety

> **Standing maintainer authorization (this workspace).** The workspace owner has
> granted agents (Claude and Codex) **full, standing authority to stop, restart,
> re-port, or `next build` over the local dev servers (`:3300` admin-web, `:8080`
> backend) WITHOUT asking first.** Do not pause to request permission for a
> restart/rebuild — just do it when the work needs it (e.g. a clean build, or an
> expired local token that makes routes redirect to `/login`; restart with
> `npm run dev:local`, which re-mints a fresh dev token). The only remaining
> discipline below is operational hygiene, NOT a reason to ask: restore the
> server cleanly on the **same port** afterward, never silently change ports, and
> confirm routes return their expected status before reporting done. (This
> standing grant applies to this maintainer's machine; a contributor on a shared
> box should still avoid clobbering a server they did not start.)

The local frontend should stay running and healthy in normal use. A developer is
normally running `next dev` on a fixed port (commonly `:3300`) with a live
browser tab. The rules below keep restarts clean — they are not a blanket ban,
given the standing authorization above.

### Always-On Local Stack

For Codex and Claude on this workspace, local services required for the task are
persistent working infrastructure, not disposable command output. Before starting
admin-web, backend, workers, proxies, emulators, or local database containers,
check whether they are already running. Prefer the repo service wrapper from the
repo root:

```bash
make dev-local-service-start
make dev-local-service-status
make dev-local-service-logs
```

Leave needed services running when the task is done unless the user explicitly
asks to stop them or stopping is required to prevent machine damage/data loss. If
a restart is needed, restart on the same ports (`:3300` admin-web, `:8080`
backend) and verify health before reporting done. Do not finish with a needed app
stack stopped, and do not leave required servers running only as foreground Codex
terminal sessions that block the final response; use the service wrapper or an
equivalent supervisor and report the URL/port in the status/final update.

The shared browser stack is the clean checkout at **exact origin/main** on
admin-web `:3300`, API `:8080`, and `goatos-local-current` database `goatos`.
Treat those three as one atomic service; do not mix a feature-worktree frontend,
another API, or an ambient database URL into it. An isolated E2E stack uses its
own non-shared FE/BE ports plus a throwaway DB and must not be stopped or modified
while recovering the shared service. `make local-stack-service-guard` enforces
this boundary in local CI for both Claude and Codex.

> **Local bearer token now self-refreshes — every dev start must use the wrapper,
> including custom ports.** In local bearer mode (`GOATOS_ENV=local` + `GOATOS_AUTH_MODE=bearer`),
> SSR self-mints a fresh short-lived HS256 token per request via
> `lib/api/local-dev-token.ts` (matching `backend/internal/platform/auth.MintHS256Token`,
> strictly local-only). `npm run dev` and `npm run dev:local` must both route through
> `scripts/run-local-next.mjs`; custom ports such as `npm run dev -- --port 3318`
> still use that wrapper. Starting plain `next dev` bypasses `GOATOS_AUTH_*` env and
> causes `/admin-web/bootstrap` to fail with `invalid_bearer_token`, so it is an
> anti-pattern. The static `GOATOS_BEARER_TOKEN` is only a fallback. Restart for
> code/build reasons, not to refresh an expired token.

## URL-Driven Filter Responsiveness

Server-filtered worklists may keep filter state in the URL, but the operator
must never see a selected value bounce back to an old server prop while the App
Router refresh is pending. Prefer `WorklistFilters`. Any client component that
combines `useSearchParams`, a native `<select>`, and `router.push` /
`router.replace` must wrap the navigation in `useTransition` and render an
optimistic selected value immediately. Run `make frontend-foundations-guard`
after touching these controls; it includes the stale-select regression that hit
Feed Config.

### Top-Bar Scope And Page-Local Filter Guard

When reviewing or changing the admin-web top bar, park selector, shell scope
links, or any route with its own farm/park/date controls, block the PR unless
the route actually consumes the chrome scope it displays. Do not show the global
park selector on authority/config pages, SOP authoring surfaces, fixed-detail
pages, or pages with their own local farm/park selector unless the page read path
uses the top-bar `park` / `scope_mode` as the effective backend scope.

Changing top-bar scope must preserve page-local filters without corrupting their
shape. Complete paired windows such as `from` + `to` must survive together; a
one-sided half window must be removed or canonicalized before navigation; and
repeated query params must remain repeated instead of being collapsed through
`Object.fromEntries`. Add or update focused source tests for these invariants
whenever touching `components/mesha-shell.tsx`, `lib/scope.ts`, page-local date
filters, or route chrome suppression lists.

Operational hygiene when you do restart/rebuild (so a restart is clean, not
destructive):

- If you `rm -rf .next` (or delete `.next/dev`/`.next/cache`) under a live
  `next dev`, you corrupt its route manifest/chunks and every route serves
  `This page couldn't load` / `404`. That is fine PROVIDED you then restart the
  dev server so it rebuilds — do not leave it in the broken state. Prefer not
  nuking `.next` when a surgical fix works, but if you do, restart.
- When you stop/restart/re-port the dev server, bring it back on the **same
  port** (`:3300`) — never silently move it to a different port.
- A production `next build` writes to the same `.next` a live `next dev` owns, so
  stop the dev server first, run the build, then restart `next dev`. (Or build a
  throwaway copy on a separate port.) Either is allowed under the standing grant
  — just don't run `next build` concurrently with the live dev server on the same
  `.next`.

Making `tsc --noEmit` / lint / `check:mock-fidelity` pass after deleting or
renaming routes does NOT require a clean `.next`:

- Stale `.next/types/validator.ts` (and `.next/dev/types`) entries that point at
  deleted routes are regenerated by the running `next dev` on the next request.
  Typecheck/lint/mock-fidelity do not need a clean `.next`, so you usually don't
  need to restart just for those — but you MAY restart if it's faster.

If you break the dev server: say so, restart it on the SAME port, and confirm
every route returns its expected status before reporting done.

## UI must match the MUI Minimal kit (enforced, 2026-09-25)

Spec: `docs/design/mui-minimal-spec.md`. Tokens: `app/minimal-tokens.css`. Gate: `npm run design:guard`.

- **Kit components + tokens only.** Build screens from `components/kit` (Button, TemplateTabs/UrlTabs,
  MUI TextField select, DateRangeField/DateTimeField/TimeField, InfoHint, EmptyState, TableFooter/PagedRows),
  MUI Dialog/Drawer + template CustomPopover/MenuList (row actions: `components/app/row-menu`) and `DataTable`/`DenseTable`/`Tag`. Raw `<table>`, `<button>`,
  `<select>`, `role="tablist"`, `role="dialog"`, `role="tooltip"` and hand-rolled `chip` classes in
  feature code fail the guard. Sizes, radii, shadows and font sizes come from `var(--…)` tokens; a raw
  px/radius/shadow/font-size literal outside `app/minimal-tokens.css` fails the guard. Colours stay the
  locked Mesha green palette in `app/mesha-theme.css`.
- **Adding a component:** spec (add the measured value to the spec + a token in `minimal-tokens.css`)
  → kit (`components/kit/<name>.tsx`, exported from `index.ts`) → story (`stories/kit/<Name>.stories.tsx`
  with default/hover/focus/disabled/error/empty/loading and a 390px variant; the lane captures light and
  dark) → baseline (`npm run visual:stories:update-baseline -- --only <story>`, PNGs opened). A kit file
  with no story fails `kit-missing-story`.
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
- **Template-fidelity guards (`components/app/template-fidelity-guards.test.mjs`, runs in `npm test`).**
  One rule id per recurring audit defect on PR #294:
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
  - `chart-ramp-distinct`: the categorical ramp (`components/app/chart-colors.ts`, palette channels)
    has no repeated channel, four different hues first, and no error red.
  - `page-header-action-slot`: no stylesheet restyles `PageHeader` layout through a className.
  - `no-card-in-card`: stacked phone table rows are divider rows (`border-bottom`), never bordered,
    rounded cards inside the table card; a KPI deck never sits inside another card (known offenders
    are a shrinking list in the test).
  - `template-filter-toolbar`: filter bars are the template list toolbar (outlined TextField selects,
    MUI Chips with Clear); rows per page lives only in the table pager.
  - `labelled-filter-fields`: every WorklistFilters field (compare operator and value) shows its own
    label at a width that does not cut it.
  - `iconify-offline-set` (`components/app/iconify-offline-set.test.mjs`): every literal Iconify name is registered in `layouts/template/iconify/icon-sets.ts`; an unregistered name loads from the Iconify API at runtime (flicker, missing offline / in the webview). Pick a registered icon or add its JSON.
  - `info-hint-tap-target` (`components/app/info-hint-tap.test.mjs`): the shared InfoHint "i" has a 44px phone tap box (negative margin keeps the glyph footprint); never shrink it back to the 24px glyph at xs.
  - `feature-server-fn-sx` (design:guard, p0): a `features/**` / `app/**` module without `"use client"` never passes a function `sx` (`(theme) => …`); as a Server Component it crashes the route ("Functions cannot be passed directly to Client Components", the /goats/[id] P0). Put themed blocks in a client file or use object sx.
  - `no-card-in-card` also covers the sx half: `features/procurement/procurement-sx.ts` phone load rows (`tr`) are dashed divider rows, never `border: 1px` + `borderRadius` cards inside the Loads card.
  - `sales-config-no-legacy-css` (`features/procurement/market-config.test.mjs`): a class a converted /sales/config file still renders is not styled by `mesha-theme.css` / `frame.css` / `minimal-theme.css` (`.sellable-product-row input{box-sizing:border-box}` collapsed the MUI Item name input; `.market-config-line input` double-bordered every TextField). Delete the legacy rule when a page moves to the template.
  - `sop-no-internal-codes` (`features/sops/sop-internal-codes.test.mjs`): SOP editors print no capture/question key or choice value (`return_to_pen`, `purchase_date`), the SOP detail dialog shows no SOP code and words the field type; the dialog is full screen at xs.
  - `sop-editor-template-fields` (same test): SOP editor text fields are MUI outlined TextFields with their own `label` (no `.numlbl` label-above wrapper), and the legacy `.qcard input` paint excludes `.MuiInputBase-input`. In the weighing, inspection, shifting, PC Care, feed and toxin editors there is no `.numfield`/`.numlbl` wrapper, no native `<textarea>`/`<select>` (MuiTextField multiline / select, InlineSelect) and no `.qcfg-title` span (template `Typography variant="subtitle2"` group heading). `sop-select-option-title` (same test): SelectOption carries an optional `title` (backend option description) rendered on the MenuItem; a native select moved onto InlineSelect/FieldSelect keeps it.
  - `sales-chart-phone-axis` (features/procurement/sales-chart-phone-axis.test.mjs): a month-axis template chart on a page passes template chart options (`responsive` below 600px: flat labels, `hideOverlappingLabels`, bounded `tickAmount`) so its ticks stay inside the card at 390; never patch the template card in components/minimal for it.
  - `procurement-template-tabs` (features/procurement/sales-no-flicker.test.mjs): procurement and sales pages use UrlTabs (URL status/filter tabs with Label counts, useUrlTabNav), SegmentTabs (chip strips, `keepScroll`) or plain MUI Tabs (client state); never AnimatedTabs / StatStrip / KpiCard.
  - `sop-library-skeleton-kpi-row` / `market-loading-mirrors-panels` (sop-internal-codes / sales-no-flicker tests): a route loading.tsx uses the same skeleton shapes as the page's own UrlSuspense fallbacks and the loaded blocks (KpiGrid + KpiWidget/CourseWidgetSummary -> `KpiRowSkeleton hero` without `icon`, never the retired StatStripSkeleton), and the page stacks its blocks with the same gap as its PageSkeleton.
  - `sop-flow-phone-fit` (same test): the SOP Flow canvas keeps a 70% zoom floor under 600px and centres the scaled flow in a sizer of the scaled size.
  - `feed-config-template-anatomy` (`features/feed/feed-config-template-anatomy.test.mjs`): every /feed/config write opens the template quick-edit Dialog (DialogTitle + subject, DialogContent, outlined Cancel + contained Apply), never an inline form in a table cell or card header; declared session feeds are soft Chips with a delete, adds are template Buttons ("Add a feed"), the session plan edit is the CardHeader action IconButton; template Iconify icons, no lucide Pencil/Trash/Plus, no `.tag` chips.
  - `kpi-map-truth` (`components/app/kpi-widget.test.mjs`): an Ecommerce-overview row in docs/design/page-template-map.md names the widget each KPI tile really renders through the adapter (EcommerceWidgetSummary only with a weekly series, CourseWidgetSummary for trend-less tiles, AppWidgetSummary for 7d, BookingWidgetSummary for month).
  - `vaccination-plan-template` (`features/vaccination-plan/responsive-css.test.mjs`): /vaccination/plan and /plan/edit stay on template parts (CourseWidgetSummary KPI row, live table in UrlSuspense keyed by `page`, Card + CardHeader, TableHeadCustom, MUI Dialog / ToggleButtonGroup / Chip, Grid md 4 / md 8); no `.vplan` / `.vp-*` CSS or classes, no raw controls, no fixed-position div modals.
  - `vaccination-template-anatomy` (`features/preventive-care-vaccination/vaccination-template-anatomy.test.mjs`, TR1-#10/#21): /vaccination matrices are template table Cards (`MatrixCard`: CardHeader + InfoTip, Label legend, Scrollbar table, soft Label state cells, 10-row PagedRows pager; cohort farms as Tabs with Label counts; status filters soft Chips). No `.cbm-*` class or rule, no raw controls, no colour literal; the TR1-#10 off-palette hexes stay deleted. `theme-token-drift` protects palette TOKENS (`--name: #hex`); deleting an off-palette rule literal from a legacy stylesheet is the fix, not drift (`removedTokenHexes`, test in `scripts/check-design-system-palette.test.mjs`).
  - `routine-drawer-template`, `dark-alert-tint`, `kanban-card-raised`: the routine drawer renders only
    MUI form parts; dark standard Alerts are a 16% main tint (the locked dark `darker` steps are mid
    tones); work-board cards are raised paper with the amber needs-attention border.
- **Client-only APIs need `"use client"`; `next build` gates every push (guards:
  `client-api-without-use-client` in design:guard; the pre-push admin-web visual gate builds).** A module that calls useState/useEffect/useRef/useTransition/useRouter/
  useSearchParams/usePathname/useLinkStatus or wires a JSX `onX={…}` handler starts with
  `"use client"` (a server module that re-exported next/link's useLinkStatus broke `next build`
  on 2026-09-27). Typecheck does not catch this; only `next build` and this guard do.
- **Charts are the template's `Chart` + `useChart`, verbatim (Ravi 2026-09-27, R2CHARTS).** The
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
- **No function crosses the server/client line (guard: `server-function-prop`, design:guard p0).** A
  SERVER module (reached from an app/ page/layout/loading without passing a `"use client"` file)
  never hands an MUI element a function `sx={(theme) => …}` / `sx={[(theme) => …]}`: MUI parts are
  client components, so the page renders "Something went wrong" ("Functions cannot be passed
  directly to Client Components", /sales/sold digest 3801663639) while typecheck and `next build`
  pass. Use an object sx with theme tokens, or make the module `"use client"`.
  The same holds for a component reference: `<Tab component={Link}>` from a server module is a
  forwardRef object (`{$$typeof, render: function}`) unless `Link` is the export of a `"use client"`
  module, so /goats/[goat_id] crashed at every size while no-prefetch-link lacked the directive
  (FJ1 P0-1). The guard resolves every `component={X}` in a server module and fails when X is
  defined locally or imported from a non-client local module.
- **No cloned element prop from a server module (guard: `server-element-prop`, design:guard p0).** A
  SERVER module never passes a JSX element in a prop that the MUI part `cloneElement`s: Stack
  `divider`, FormControlLabel `control`, Tab/Chip `icon`/`avatar`/`deleteIcon`, Checkbox/Radio
  `checkedIcon`. The element crosses as a lazy RSC reference, the clone gets an undefined type and the
  page shows "Something went wrong" ("Element type is invalid ... got: undefined", /goats/[goat_id]
  from `<Stack divider={<Divider />}>` in the vaccination passport strip). Use
  `components/app/divided-stack` (`<DividedStack dividerOrientation="vertical">`) or a `"use client"`
  leaf that builds the element (PassportFormCheckbox). Children and directly-rendered props are fine.
- **/vaccination/live-tracker is template anatomy (guard: `live-tracker-template-anatomy`, npm test).**
  Operators / pens / combo are template table Cards (CardHeader, Scrollbar, PagedRows pager,
  LinearProgress closure cells), the rail is Card lists, error / empty states are Alerts, header
  controls are MUI Buttons. No legacy card / hd / bd / note / btn / lt-* classes and no raw
  `<section>` come back; `lt-truncnote` is only the visible-reason marker. The drive day, parks running and the live poller sit in the template AppWelcome row (template-derived, no demo image); the header carries only Full Schedule + Command Board; an empty day is welcome text with Reset, never an info Alert banner (guard `live-tracker-app-overview`, TR1-#31).
- **Every info "i" is the shared InfoTip (guard: `info-tip-tap`, npm test).** No local
  `function InfoTip` / `InfoTooltip`, no `.ihelp` / `.tipwrap` button inside a hover Tooltip: those open
  on hover only, so a tap in the Android WebView does nothing (/herd-signals column help, REVIEW-15
  O19). Use `components/app/info-tip.tsx` (controlled Tooltip, 44px IconButton, opens on tap).
- **/herd-signals shells are template anatomy (guard: `herd-signals-template-anatomy`, npm test).**
  Board / Alerts / Gateways / Insights render MUI Card + CardHeader, Alert, Grid and Label; no legacy
  card / hd / bd / banner / gwcard / insight / rowitem / pager / btn classes. Header live control is a soft Button (success / warning / neutral) with the "Updated … IST · stream …" line in its Tooltip, Export an outlined Button; no LIVE pill or header meta line. Eight KPI tiles = two rows of four (KpiGrid `n % 4 === 0` -> md 3; guard `herd-signals-live-header`, TR1-#30).
- **Course widget icons are masks (guard: `mask-icon-not-img`, npm test).** COURSE_WIDGET_ICONS svgs
  render through SvgColor with a tone gradient, never `<Box component="img">` (they paint black).
- **The shell is gated on every push (guard: r2 visual gate `shell|*`, scripts/r2-audit-checks/shell.mjs).**
  Sidebar root items + subheaders start at nav.left + 16px with padding-left 12px (template
  NavSectionVertical: content on the logo column), the active item is a translucent primary tint,
  header controls are transparent template IconButtons, no filter control or its floating label
  overlaps the Tabs strip, sort headers are TableSortLabel in the header colour (never link blue /
  underlined), and every stylesheet loads. Run `npm --prefix apps/admin-web run visual:gate -- --fast`
  before each push (the pre-push hook runs it: 5 shell routes + the routes your change touches; any
  new failure on those routes fails). Never `next build` into the `.next` a live server is serving:
  it serves UA defaults (40px list indent, grey buttonface squares, blue links) until restarted.
- **A restyle never introduces new UI behaviour.** Changing how something looks must not change what it
  does (clicks, routes, fetches, copy, which fields show). Behaviour changes are separate PRs.
- **Never reopen a regression-guard item.** The invariants in `docs/design/redesign-regression-guard.md` (tooltips
  portaled, charts draw in once, tap targets, sticky axes, drawer layering …) are closed; a change that
  undoes one is a defect even if it matches the spec.
- **Mobile webview rules:** every control ≥ `var(--tap-min)` (44px) at phone width, fixed overlays render
  through kit `BodyPortal`/`Sheet`/`Dialog` (never `position:fixed` in place), `100dvh` not `100vh`, no
  page sideways scroll; wide tables scroll inside their own card.
- **Right drawers are the template temporary Drawer** (Ravi R2-4): `MinimalDrawer` (components/app/drawer) or `DetailDrawer` (components/app/detail-drawer) only — portalled, anchor right, visible backdrop (never `invisibleBackdrop` / `backdrop: { invisible: true }`), template paper width 320 (filters) / 360 (settings) / 420 (notifications) / 480 (details, forms; `{ xs: 1, sm: 480 }`), sticky header title + close, Scrollbar body, footer actions. No raw MUI `<Drawer>` for a right drawer, no hand-rolled `<aside className="drawer">` + `.scrim`. Wide content never squeezes or clips at the drawer edge: a table sits in `DrawerTableScroll` (own Scrollbar, `Table sx={{ minWidth }}`) and scrolls sideways. Guards: design:guard `drawer-off-template` (static), `scripts/r2-drawer-audit.mjs` (runtime: width, backdrop, no child overflow without its own scroll container).
- **Existing debt is a ratchet, not a waiver.** `design-system-waivers.json` → `ratchet` lists
  `check|file` with an allowed count and reason. A file may only go down; new files have 0. Never raise
  a count or add an entry to land a change (`--update-baseline` refuses to).
- **MUI Minimal template is the reference.** `~/mesha/mui/Minimal_TypeScript_v7.7.0` on Ravi's
  laptop (licensed source, **NOT** committed to the repo). Every admin-web area maps to a template
  SECTION in `docs/design/route-template-map.json`; a NEW page must add its area in the same change
  or fail `route-template-map-missing`. Palette is the locked Mesha green — template gives structure,
  density, motion and interaction patterns only, never brand colours, images or copy.
- **Every page names its template page and composes its section components.** `docs/design/page-template-map.md`
  maps route → template page (overview/user/order/kanban/calendar…) → the template section components
  per block, copied verbatim under `components/minimal/` (with `template-sources.json` entries). KPI rows are
  `EcommerceWidgetSummary` / `CourseWidgetSummary` / `BankingWidgetSummary` (never the pastel
  AnalyticsWidgetSummary tint in dark); lists use the template table anatomy (Card, Tabs + Label counts,
  toolbar, TableHeadCustom, pagination). `design:guard` rule `page-template-map` fails when a mapped page
  stops importing one of its listed template modules or a mapped file disappears.
- **Template sections with a function sx are client modules (guard `section-server-fn-sx`, p0).** Pages render
  `components/minimal/sections/**` from Server Components; a section using `sx={(theme) => …}` without `'use client'`
  throws "Functions cannot be passed directly to Client Components" at render. Put `'use client'` at the top.
- **Neutrals are the template's, brand is Mesha (Ravi 2026-09-27).** Grey scale, surfaces, text,
  divider and action values are exactly the MUI Minimal template's (dark bg `#141A21`, paper `#1C252E`);
  primary/status hues stay Mesha. P0 `template-neutrals` pins `theme/theme-config.ts` +
  `app/minimal-tokens.css`; P0 `retired-neutral-literal` bans the old green-tinted neutrals everywhere.
- **Surfaces and colours come from the theme, in BOTH modes.** KPI/widget cards are the VERBATIM template
  widget summaries (components/minimal/sections/overview/{course,e-commerce}/*-widget-summary.tsx),
  fed through the adapter `components/app/kpi-widget.tsx` (`KpiWidget`; client drill-in:
  `kpi-widget-action.tsx`; row: `components/app/kpi-grid.tsx`): EcommerceWidgetSummary for a real
  weekly series + percent, CourseWidgetSummary (icon corner) otherwise. KpiCard / StatStrip and the
  components/minimal/widgets/* fakes are deleted; never re-create them. Never paint a
  surface `common.white` / `#fff` / `grey.50-200` (P0 `light-surface-literal`; the dark shell shows a
  light box) and never select a `.Mui*` class in the legacy stylesheets to set a colour, background
  or border (P0 `legacy-css-mui-colour`; frame/minimal-theme/mesha-theme/menu-surface/globals.css
  only shrink). Tints are `varAlpha(theme.vars.palette.<c>.<x>Channel, a)` over the paper.
  A legacy rule that paints a bare `th`/`td`/`tr` repaints MUI tables too: exclude MUI parts
  (`td:not(.MuiTableCell-root)`, waivable `legacy-table-paint`), and no `rgb()`/`rgba()` colour
  literal in TSX (waivable `rgb-colour-in-code`).
- **Production bug CLASSES are automated guards.** `scripts/lib/visual-pattern-guards.mjs` (route
  visual lane) adds `P-text-icon-overlap`, `P-wide-table-no-wrapper`, `P-chart-axis-tiny` (<11px),
  `P-pinned-bar-blur-flicker`, `P-drawer-filter-mismatch` and `P-chart-hover-remount`. `raw-chart-lib`
  refuses recharts/d3/chart.js/nivo/victory/visx/echarts/highcharts — charts are Apex (via
  `components/minimal/chart` or `components/kit`) or the two inline helpers (`svg-bars`, `svg-series`).
  Full pattern → guard table: `docs/design/README.md` §5b.
- **Template files are verbatim (guard `template-verbatim`, design:guard p0, self-test).** Ravi 2026-09-27: "use the SAME mesha-ui template across the pages and just put our content." Every file mapped in `docs/design/template-sources.json` (`components/minimal/**`, `layouts/**`) equals its source in `~/mesha/mesha-ui/vendor/minimal/Minimal_TypeScript_v7.7.0/next-ts` byte-for-byte, except import paths (`src/...` -> `@/components/minimal/...`, `@/layouts/...`, `@/theme/...`; package imports unchanged) and a leading `"use client"`. No prop, sx, copy, comment or behaviour edits inside them; Mesha colours come only from the theme. The manifest stores the sha256 of each normalised template source (the template is not in CI); refresh with `node apps/admin-web/scripts/refresh-template-hashes.mjs` after copying a new template file. A component with no true template source never lives under `components/minimal/` (no KpiCard / StatStrip / progress-item / AnimatedTabs wearing a template path): use the real template component (EcommerceWidgetSummary / CourseWidgetSummary / BookingWidgetSummary, the template Tabs + Label anatomy, EcommerceSalesOverview progress rows). Product behaviour (URL-linked tabs/pagination, data mapping, i18n labels) lives in `components/app/` adapters or feature files that only render template/MUI components and pass props/children (no own CSS, no raw px/colours). Remaining drift is the shrink-only list in `docs/design/template-verbatim-baseline.json` (a healed file must be removed; nothing may be added). The drift baseline keys each listed file on the sha256 of its bytes: editing a baselined file fails too (restore it to the template and drop the entry; never re-record a hash). The tab strips and list toolbar pieces (TemplateTabs, SegmentTabs, LabelTabs, ListToolbar, FilterChip, SearchTextField) are components/app adapters over MUI Tabs / Chip / TextField + template Label, never files under components/minimal.
- **Template-derived sections (guard `template-derived-anatomy`, design:guard p0, self-test; test `no-template-demo-literals`).** A template section whose DEMO wiring (fixed demo values, a hard-coded default select such as '2023' / 'Yearly', demo model types / copy) cannot carry our data through its props is copied to `components/app/sections/<template path>` and listed in `docs/design/template-derived.json` (source + one line of what became props). Only that wiring changes: its JSX element sequence, sx keys AND values, and literal JSX props (type, variant, component, style, size, color …) must equal the template's (manifest records them; optional `strip` slot regexes are removed first; `allowProps` are keyed by element (`IconButton:aria-label`), a spread carrying sx/style is always drift; `node apps/admin-web/scripts/refresh-template-derived.mjs`). A select starts on the first real series, so a chart never starts empty; empty states live in a components/app adapter (e.g. `BalanceStatisticsCard`), never as a new branch in the derived file. No demo literal (6789 / 1234 / 1012, '2023', 'Yearly', 'Order total', 'Earning', 'Refunded', Request / Transfer buttons) may appear in page source. Shell layouts follow the same rule under `layouts/app/` (dashboard layout / nav-vertical / nav-mobile, auth-split layout, logo, WorkspacesPopover): WebView invariants (safe-area pads, 44px taps, Back closes) are declared overrides pinned in `strip`, and a template demo control a shell slot replaces is removed via `templateStrip`. A verbatim template section whose defaults are demo copy (auth-split section 'Manage the job') gets Mesha copy from every caller (test `no-template-demo-literals`).
- **No pastel fallback on mapped pages (guard `page-template-no-pastel`, p0).** A page with a row in `docs/design/page-template-map.md` must not render `AnalyticsWidgetSummary`; KPI rows are `EcommerceWidgetSummary` / `CourseWidgetSummary` (via `components/app/kpi-widget.tsx`), charts are template chart cards (CardHeader + select), lists use the template table anatomy.
- **Template sections keep their client boundary (guard `section-client-boundary`, p0).** A file under `apps/admin-web/components/minimal/sections/` that calls a hook or passes a function `sx`/`(theme) =>` callback must start with `'use client'`; a server page rendering it would pass a function across the RSC boundary and crash at render. `next build` must pass before every push. Modules that import `useLinkStatus` / `useRouter` / `useSearchParams` / `usePathname` straight from `next/link` or `next/navigation` must start with `'use client'` (test `client-only-hook-directive`, `components/client-only-hooks-directive.test.mjs`).
- **No legacy card shells on mapped pages (guard `page-template-legacy-card`, p0).** A page with a row in `docs/design/page-template-map.md` must not render `className="card"` / `"wchart"` / `"wtable"` / `"kpi"` sections or `<h2 className="h">` headings: every block is a template section card (Card + CardHeader, e.g. `BankingBalanceStatistics`, `AnalyticsWebsiteVisits`, `EcommerceSaleByGender`) fed our data, and the legacy CSS behind those classes is deleted as pages stop using it. Once gone it stays gone (test `legacy-card-css-deleted` in `components/app/template-fidelity-guards.test.mjs`): no `.wchart` / `.wtable` / `.g2`-`.g6` / `.wb-dialog .it .car` rule in frame.css / mesha-theme.css / minimal-theme.css and no product className naming them.
- **Wide matrices never crush their column headers (test `notification-matrix-header-floor`).** A table with one column per designation/role/day gives each column a readable floor (`Table sx={{ minWidth: fixed + floor * n }}`, `<col style={{ width: floor }}>`), scrolls sideways inside its card with the label column sticky, and keeps headers on one line (`nowrap` + ellipsis + Tooltip with the full label). Never shrink header text (10px) or let it wrap letter by letter to fit.
- **Progress bars take their length from the column, never a px floor (guard `progress-bar-px-min-width`, p0).** A template progress row (EcommerceSalesOverview `LinearProgress`, height 8, grey-500 16% track) fills its row or table column; size the column (`TableCell sx={{ width: "32%" }}`) and let the bar fill it. A raw `minWidth: 80` on the bar overflows narrow cells.
- **A chart colour is a CSS colour, never a palette key (audit `chart-black`, P0).** `chart.colors` on a template chart takes resolved colours (`var(--palette-primary-main)`, or the section default); a palette KEY such as `seriesColorVar(i)` ("primary", "info.light") never resolves in Apex and paints the series and legend dots black. Omit `colors` to keep the template default pair.
- **Side-by-side fixes on /weighing/weights and /counts/mortality (R3CNT).** A KPI row (`KpiGrid`) is a page row, never inside another Card (test `kpi-deck-not-in-card`, shrink-only list). Category-axis labels that would rotate go on two lines (`[name, count]` categories, `labels: { rotate: 0 }`; test in `features/weighing/gain-thresholds.test.mjs`). Rate-table rows show words, not raw codes (`bucketLabel` -> `humanizeEnum`), column heads are capitalised and row labels hold one line (`features/counts/mortality-template.test.mjs`).
- **Third-party grids are shielded from the legacy table rules (test `calendar-legacy-table-reset`).** The shell's legacy `.wrap/.screen/.main th|td` rules also hit raw `<th>/<td>` inside FullCalendar and similar widgets (grey band, 48px+ rows, padding). Reset them at higher specificity inside the widget root (`CalendarRoot` `legacyTableReset`), never by adding rules to the legacy CSS files. The month grid is `height="auto"` at md+ (no inner scroller clipping the last week); calendar titles use `lib/format` DD/MM/YYYY.
- **Calendar is the template calendar view (TR1-#25; test `features/calendar/calendar-template-toolbar.test.mjs`, guard `calendar-template-toolbar`).** The toolbar is view toggle, prev / title / next, a solid `error` contained small "Today" and the `ic:round-filter-list` filter IconButton with the reset dot (rendered only when the page wires a drawer). Window (upcoming / history), owner lane and workstream live in the template 320px filters drawer (`features/calendar/calendar-filters.tsx`); applied ones show as `LinkFiltersResult` chips above the card. No segmented toggle in the heading, no chip row above the card, no tab row inside it, and no "Add event" (no Mesha action).
- **Kanban cards are the template item (TR1-#24; test `features/work-board/kanban-template-card.test.mjs`, guards `kanban-template-card`, `tasks-new-task-reason`).** A disabled /tasks "New task" (no assignable people) carries its backend reason (`new.no_assignees`) in a tap-opening Tooltip, never a silent grey button. /work-board and /tasks cards are ItemStatus + ItemName + a caption line + the ItemInfo row (clock / readings / avatars); no Label or Chip strip and no hand-made progress bar on a card. An empty column is the bare template list (screen-reader text only), never a dashed "Nothing here" box.
- **File pickers are the template Upload area (test `config-drawer-upload-template`).** A feature never renders the browser's bare `<input type="file">` next to a legacy `.btn`: use `UploadFile` (`components/minimal/upload`, template UploadDefault anatomy, the native input hidden inside so refs/forms read it as before) and a kit `Button` for the action. Required form fields pass `required` and let MUI add the one asterisk (never a hand-typed `" *"`).
- **Sidebar and header are the template dashboard layout (guard `shell-nav-template`, p0; test `components/sidebar-viewport.test.mjs`).** The nav is `NavSectionVertical` / `NavSectionMini` (layouts/template/nav-section, verbatim) inside the template-derived `layouts/app/dashboard/nav-vertical.tsx` / `nav-mobile.tsx`, fed by the backend bootstrap nav. The whole nav scrolls in the template `Scrollbar`, logo fixed; only the active group opens (no `default_open` subtrees); no custom nav footer (`navBottom`, `msh-foot`, `navigation.footer` - the template only has the optional NavUpgrade card, which we do not use); the phone nav is the template drawer (`var(--layout-nav-mobile-width)` over the template backdrop, no full-width/opaque scrim, no extra close button; Android Back closes it). Header right order follows the template: notifications (IconButton + Badge + solar bell) -> theme toggle (template Settings slot) -> account; the park scope is the template-derived WorkspacesPopover (trigger and list, options are scope links) in the header left slot (`layouts/app/components/workspaces-popover.tsx`).
- **Park switcher (test `components/park-switcher.test.mjs`, `lib/scope.test.mjs`; guard `template-derived-anatomy`).** The header WorkspacesPopover option name wraps (declared override, never `noWrap` in the 240px list) and shows the park code the shell passes (`code`, skipped when it equals the name). A `?park=<id>` outside the shell's park list reads the `scope.selected_park` copy (the `fallback` prop, `pickScopeOption`) and marks no option selected; it never reads or selects "All parks". The list is 280 wide with a 360 scroll cap (declared), so one long park plus All parks show whole; the code caption strip pins its exact Box line and sx (self-test `derived-slot-cond-edit`). Styles passed through slotProps (`slotProps.paper.sx`, any `slotProps.*.sx`) are anatomy too (manifest `slotSx`, self-test `derived-slotsx-width`): a paper width change needs a declared strip.
- **Shell nav + tap floor (TR1).** A routed page with no nav leaf of its own (withheld /counts/herd, /weighing/weights) opens and highlights its module group (`lib/nav-module-group.ts`, test `nav-module-fallback`). The PhoneTapStyles 44px floor covers width as well as height for Chip, ToggleButton and the date-range clear (a one-word chip was 41px wide); the r2 tap check skips visually hidden `.sr-only` controls only (test `tap-skip-visually-hidden`).
- **One CTA per destination (backend test `TestVaccinationBoardOpenCTAsAreDistinct`).** Two buttons with the same copy and href (the Action Center empty state once showed "Open the vaccination plan" twice) collapse to one; a page contract never publishes two `action.open_*` keys that read the same.
- **Page rhythm, raw codes, phone FAB, blur, preflight (R3OPS; r2 audit plugin `scripts/r2-audit-checks/page-rhythm.mjs` P0 `rhythm|header-gap` / `rhythm|raw-code-label`; tests `page-rhythm.test.mjs`, `components/app/preflight-border.test.mjs`).** A page root is the `screen on` grid (never a bare fragment with the PageHeader first: the summary card glued to the breadcrumbs on /verify). The header-gap probe also measures a PageHeader wrapped alone in a div (the first block is the wrapper's next sibling; guard `header-gap-wrapped`): a block `.screen` root or a plain Box root needs its own column gap (/tasks, the SOP builder read 0px). A Label/Chip never shows a snake_case backend code (`Not_started`): run it through `humanizeEnum` / `optionLabel`. Ask Mesha docks in the header at EVERY width as a template header IconButton (goat mark, like the template's language flag); there is no floating bubble under the shell and no bottom clearance for one (TR1-#13, test `ask-mesha-docked`, features/ceo-ai/ceo-ai-dock.test.mjs); the header slot is tracked live, never looked up once (TR1-#12). No `backdrop-filter` blur on scrims/loading veils (Android WebView flicker; legacy blur declarations only shrink, `no-blur-scrim`). Tailwind preflight gives every `::before/::after` `border-style:solid`: a template pseudo element that sets `borderWidth` must also set `borderStyle` (`preflight-pseudo-border`; the /tasks kanban columns had a white ring).
- **Phone list tables never clip a column (R3OPS-2; test `features/leadership-tasks/tasks-phone-viewport.test.mjs` guard `tasks-phone-stacked-row`).** Below sm a list either stacks (secondary columns carry a class on header AND cell, hidden at xs, and the first cell repeats their values) or scrolls sideways inside the template Scrollbar with the cut visible; a column cut mid-word at the card edge is a defect.
- **Overlay journeys fail, never skip, on stale selectors; opening never scrolls the page (R3OPS-3; test `scripts/lib/overlay-journeys.test.mjs`, guard `overlay-no-scroll-jump`).** A journey whose trigger always renders (e.g. /people Access, Add person) is `required: true`, so a selector left on a removed legacy class (`button.btn.sm.ghost`) fails the live smoke instead of logging `overlay_skip`. Every journey fails when the page scroll moves between click and open: a drawer/dialog that renders in flow (no portal) or a link without `scroll={false}` shows up as a jump. A LocalOverlayLink that opens a drawer carries `aria-haspopup="dialog"`.
- **No letter-stacked text (R3OPS-3; r2 audit plugin `scripts/r2-audit-checks/letter-stack.mjs` P0 `text|letter-stack`, test `letter-stack.test.mjs`).** A column squeezed under ~2 characters breaks words one letter per line (FJ1-P0-3, the /calendar/drive animal roster). Fix at the layout: template table kit (Scrollbar + Table `minWidth` + nowrap identity cells) on laptops and a stacked row per record on phones; never shrink the font or add `word-break: break-all`.
- **List toolbars and pagers stay intact (R3OPS-3; r2 audit plugin `scripts/r2-audit-checks/controls.mjs` P0 `controls|label-doubled` / `controls|control-overlap` / `controls|pager-clipped`, test `controls.test.mjs`; /tasks test `tasks-phone-viewport.test.mjs` guard `tasks-phone-search-row`).** A field says its label once (TextField `label`, never an extra caption above it); toolbar controls on one layer never overlap; pager arrows stay inside the card and the pager never scrolls sideways. At phone width the keyword field owns its row and secondary buttons (Dates) wrap under it; a stacked phone row renders nothing for an empty value (no leading gap).
- **A screen that replaces a page branch still streams its panel (R3OPS-3; test `features/verification-review/toxin-review-render.test.mjs` guards `toxin-panel-suspense`, `video-log-park-seed`).** An early-return screen (the /verify Toxin tab) renders its header + tabs with no await and puts its read in an async panel inside `UrlSuspense` with the shared `TableSkeleton`, so switching to it or paging it never blanks the page. Drawers seed their filters from the page scope (`parkFilter = own param || scope.parkId`) and their downloads follow the same value.
- **Filter toolbars live outside the keyed panel (R3OPS-3; test `features/leave/toolbar-outside-panel.test.mjs` guards `leave-toolbar-outside-panel`, `routines-toolbar-outside-panel`).** Only rows + pager sit inside `UrlSuspense`; the toolbar row renders before it, so a filter change never swaps the control just used to a skeleton and an empty result still shows the filters. State shared across the boundary (dense switch) goes through a per-card client context, not props; the panel fallback draws no second toolbar.
- **Action Center board and Protocol Adherence ledger are template anatomy (TR1-#26/#29; test `features/process-integrity/action-center-template-anatomy.test.mjs`, guards `action-center-kpi-selected`, `action-center-card-anatomy`, `adherence-ledger-readable`).** A selected quick tile is the template selected-card ring (`0 0 0 2px text.primary`, checkout/payment), never a status-coloured outline. The status board is the template kanban (`components/app/kanban` columns + item anatomy: priority arrow, name, caption lines, `ItemInfo` owner); a card carries at most two soft Labels (`MAX_CARD_LABELS`) and every other fact as a caption line; empty columns have no placeholder box; the legacy `.taskboard` / `.tcol` / `.task` / `.sla` CSS stays deleted. The adherence ledger's column widths fit the 1440 content column (Evidence on screen), the table scrolls in the template Scrollbar below that, and its cells wrap instead of cutting text with an ellipsis. REVIEW-35 (same test): the Filters / My tasks panel hides cards through `ACTION_CENTER_CARD_SELECTOR` (`action-center-card-selector`), the route loading.tsx renders the page's own `VIEW_SKELETON` from `action-center-skeletons.tsx` (`action-center-loading-mirrors-page`), and the 12-state adherence strip is `UrlTabs scrollButtons="auto"` with 44px arrows on phones (`adherence-tabs-scroll-buttons`).
- **Qualifying notes are info tooltips (R3OPS-3; test `features/process-integrity/action-center-paging-tooltip.test.mjs`, guard `action-center-paging-tooltip`).** A caveat about a control (the /action-center "board shows one page" note) is `InfoTip` (`components/app/info-tip.tsx`: controlled template Tooltip on a 44px info IconButton beside that control; a tap opens it in the webview, where MUI's zero-delay touch open is cancelled by touchend; guard `info-tip-tap`), with the text as its accessible name; never a loose body2 line between the toolbar and the content.
- **TR-1 fixes (FXC).** (a) Page blocks fit the column (guard `url-panel-min-width`: r2 audit plugin `scripts/r2-audit-checks/column-fit.mjs` P0 `layout|wider-than-column` + test): `UrlPanel`'s `display: contents` box gives its children `min-width: 0`, because the page grid's `> *` rule never reaches through it and one wide table sized the whole page (/sales/loads 1561px in 1060px, which also pushed chart tooltips off a 390 screen). (b) A copy key a page renders on load but that is newer than the oldest serving API has a route `COPY_FALLBACKS` entry equal to the backend copy (guard `source-entry-copy-fallback`, `features/procurement/source-entry-copy-fallback.test.mjs`): /procurement/source-entry crashed to the error boundary on the API one release behind. (c) OrderDetailsHistory wraps the caller `body` in its own block so the time caption keeps its line (guard `order-history-body-block`, `components/app/order-history-body-block.test.mjs`; /workflows "tasksNEXT").
- **Adding a NEW page (ordering):** template section (`docs/design/route-template-map.json`) →
  minimal component (`components/minimal/<area>` or a kit component) → story (states + 390 + light/dark)
  → route in `scripts/smoke-visual-live.mjs` → baseline. Verify at 1440 / 390 / 412, dark + light,
  chart hover interactive, before push.

## Product Taxonomy (READ FIRST — do not rename these)

These words have fixed meanings in Goat OS:

- **Vertical** = business operating domain/department. Examples: Preventive Care (PC), Parks,
  Procurement, Admin/Data Ops, Counts, Breeding, Inventory, HR/People, Farmer
  Network.
- **Module** = a concrete workflow/product inside a vertical. Examples:
  Preventive Care (PC) -> Vaccination, Preventive Care (PC) -> future Treatment/Deworming, Procurement -> Source
  Entry, or future Parks modules. Parks is a scope/context dimension for
  vaccination execution, not the owner of a vaccination module.
- **Operational module screen** = the page where that module's work happens.
  Examples: `/vaccination` for Preventive Care (PC) -> Vaccination, including operations, status
  matrix, cohort detail, and park/shed execution context, and
  `/procurement/source-entry` for Procurement -> Source Entry. Parks is NOT a
  vaccination product route, module, sidebar entry, or nested command screen:
  park/shed execution context renders ONLY INSIDE `/vaccination`, scoped by the
  top-bar park dropdown.
- **Command lens** = top-level cross-module screen, not a vertical and not a
  module. Control Tower, Action Center, Calendar, Protocol Adherence, and
  Workflows are command lenses. They summarize/filter work emitted by modules.
- **Authority screen** = top-level Admin/Data Ops authoring surface. Config
  (`/config`) is the authority screen. The old SOP Library (`/sops`) is RETIRED
  (SOP split, maintainer decision 2026-08-18): SOPs are per-module
  module-surfaces at `/vaccination/sops`, `/counts/sops`, and `/feed/sops`.

Do not say "module/vertical" as if they are interchangeable. Preventive Care (PC) is the
vertical. Vaccination is the module under Preventive Care (PC). Procurement is the vertical.
Source Entry is the module under Procurement.

## Platform Model (READ FIRST — this is NOT a vaccination app)

Goat OS is a **generic, multi-vertical OS**. Generic engine (protocol rules,
obligations, SOP tasks, proof/media, verification, process-integrity) →
**verticals** (e.g. Preventive Care (PC)) → **modules** (Vaccination today; Feed Direction,
deworming, and others next, on the **same generic engine**).

**Vaccination is one module under Preventive Care (PC) — the current visible build slice, not the
app's identity.** "Vaccination-only" always means the current visible *content
slice*, never that the app/engine/shell/Config/SOP layer is vaccination-specific.
Keep those layers generic and category/schema-driven. Never treat vaccination as a
global/app-wide scope (no app-wide "Vaccination" badge/label); the active module is
shown by the sidebar nav + page crumb (`Preventive Care (PC) › Vaccination`). Do not bake vaccination
into generic layers; do not show unbuilt modules as live.

## Non-Negotiable IA Rule

**Vaccination-only is data scope, not UI hierarchy. Mock hierarchy wins.**

Control Tower, Action Center, Calendar, Protocol Adherence, and Workflows are
**top-level command-room screens** (`/`, `/action-center`, `/calendar`,
`/protocol-adherence`, `/workflows`), exactly as the mock places them. Config is
also a top-level Admin / Data Ops authority screen (`/config`). They are NOT
tabs or redirects nested inside the
Preventive Care (PC) Vaccination module, Procurement/source-entry, Parks, or any future vertical. The
selected module/domain filters their *content*; it does not move them under a
vertical. Do not reintroduce an Action Center / Calendar / Adherence /
Verification tab strip inside `/vaccination`.

This is a global IA rule, not just a Preventive Care (PC) rule. Any module under Preventive Care (PC), Parks,
Procurement, or a future vertical may feed these top-level command/authority
screens through a selected domain/filter/lens, but must not duplicate them as
nested routes, redirects, tabs, or nav items. Do not create
`/vaccination/adherence`, `/vaccination/calendar`, `/vaccination/workflows`,
`/vaccination/config`, `/procurement/source-entry/action-center`,
`/procurement/source-entry/protocol-adherence`,
`/procurement/source-entry/control-tower`, or
`/procurement/source-entry/workflows`.

## Current Scope

Admin-web is built around the vaccination process-integrity slice:

- **Command-room (top-level)**: Control Tower `/`, Action Center
  `/action-center`, Calendar `/calendar`, Protocol Adherence
  `/protocol-adherence`, and Workflows `/workflows` (+ drilldown
  `/workflows/{row_id}`). These read the canonical process-integrity contracts
  (Control Tower / Action Center / Adherence / Workflow), filtered to
  vaccination data. Calendar is implemented as the top-level vaccination
  due-work slice with its page, primary nav, mock-fidelity scan coverage, and
  live-smoke script coverage path wired.
- **Preventive Care (PC) / Vaccination**: the module surface at `/vaccination` only. It must be
  mock-faithful: SOP / Import sheet / New drive actions, Target -> Group -> Route
  -> Execute chain, vaccination status matrix, per-cohort detail, drive/shed-event
  execution, proof/verification/rework states, and honest empty states when data
  is absent. It links OUT to the command screens; it does not embed them.
- **Admin / Data Ops**: generic protocol config at `/config` and the CEO/admin
  business Audit Log at `/operations/audit`. SOP pages moved to their modules
  (SOP split, maintainer decision 2026-08-18): `/vaccination/sops`,
  `/counts/sops` (Herd Operations SOP), and `/feed/sops`, each with the
  form-builder at `?compose=1`.
- **Vaccination execution context**: park/shed/stage/defer/blocker/owner context
  renders INSIDE Preventive Care (PC) / Vaccination at `/vaccination`, scoped by
  the top-bar park dropdown. It is powered by the execution read-model endpoints
  (`/vaccination/execution`, `/vaccination/execution/sheds/{shed_id}`), not by Parks
  routes. Parks is NOT a separate visible vaccination module, sidebar entry, or
  product route. Do not introduce or preserve `/parks/vaccination` frontend
  routes, redirects, API contracts, feature folders, or navigation. Do not build
  generic Parks.
- **Goat Passport**: contextual drilldown at `/goats/{goat_id}` only.
- **Vaccination trigger-closure active surfaces**: Counts -> Herd Register at
  `/counts/herd` and Admin / Data Ops -> Audit Log at `/operations/audit` are
  active for proving the real vaccination cascade from a business trigger and
  inspecting the resulting business audit chain. This active slice includes the
  dependency closure required for those surfaces to actually work:
  location/park/shed selectors, lookup choices, identifier validation/conflict
  states, bulk preview row errors, limited backend-backed count cards,
  entity-history links, audit filters, and generated-client plumbing. It does
  not authorize unrelated Counts modules, old Operations, old `/herd`, or old
  import/review surfaces. The Counts sidebar shows exactly two leaves in this
  slice — `Herd Register` (`/counts/herd`) and `Counts Breakdown`
  (`/counts/breakdown`); do not show disabled `Tagging & identity`, `Weights &
  ADG`, or `Count reconciliation` leaves for mock fidelity. Herd Register’s leaf is currently
  withheld (its route stays reachable), so the two visible leaves are `Herd Analytics`
  (`/counts/analytics`) and `Counts Breakdown` (`/counts/breakdown`).
- **Herd Analytics** (`/counts/analytics`) is the Counts leadership read, opened by maintainer
  decision 2026-08-20. Two questions, one screen: what the herd IS right now (breed, pen tag,
  sex, kid/adult, farm) and what CHANGED it month by month (births in, deaths and sales out,
  other exits, pen movements within). One round trip to `GET /counts/herd-analytics`; the page
  derives no count of its own and reads whole-window `totals` from the response rather than
  re-summing `months`. Composition uses the same live population `/counts/breakdown` reports, so
  the two Counts screens can never disagree about the denominator. Charts are the shared inline
  SVG marks in `components/svg-series.tsx` + `components/svg-bars.tsx` — SERVER components,
  `var(--*)` series colours, recharts still at zero importers. Composition cards are FULL WIDTH
  (`.herd-analytics-charts`), never the mock’s 340px `.charts` masonry, which squeezes the wide
  SvgBars viewBox down to unreadable labels.
- **Counts Breakdown** (`/counts/breakdown`) is the census surface, reopened by
  explicit maintainer decision (2026-07-18). Live head counts grouped by
  farm x stage x breed x gender x shed, plus the mock's `.charts` /
  `.chartcard` distribution row. Three rules that are easy to break:
  (a) charts are the mock's inline-SVG `svgHBars` anatomy ported to
  `components/svg-bars.tsx` — a SERVER component with `var(--*)` series colours.
  recharts is in `package.json` with zero importers; do not make this its first
  use. (b) The table shows a real total and numbered pages. That is NOT a
  violation of failure mode 6c's cursor rule — 6c bans `COUNT(*)` over PER-GOAT
  rows; these are pre-aggregated grain combinations and the totals are SQL
  window functions over the full grouped set. (c) Stage is raw
  `goats.management_stage`. Near-duplicate source labels (`ICU-Kid` vs
  `ICU-Kids`) render as separate rows on purpose — do not "fix" this in the UI;
  `count_dimension_aliases` is the source-side remedy.
- **Audit meaning split**: backend/platform `audit_log` is internal debug,
  replay, idempotency, proof, and investigation infrastructure. It can carry raw
  action names, UUIDs, metadata, trace IDs, and domain/module/category fields and
  does not need a dashboard UI. The visible CEO/admin Audit Log is a business
  projection under Admin / Data Ops: who did what, where, with what proof, and
  what result. Do not expose raw developer fields as the main dashboard UX.
  Match the mock's business controls: KPI cards, operation-family chips,
  `Viewing as` role/span preview, search/status tabs, Operators/span,
  Anomalies only, Activity trail, cursor pagination, row history links, and
  disabled export until a backend export exists. Show only real events for built
  surfaces; do not fake future operation families or totals.
- **Audit finish rule**: `/operations/audit` list/summary contracts,
  generated-client types, backend `internal/operationsaudit`, and the
  `features/operations-audit` page already exist. Do not rebuild them from
  scratch. First diff current behavior against the mock and docs, then close
  only concrete gaps. Backend work is verify-only unless the generated contract
  is missing data that the business Audit Log cannot derive from existing row
  metadata; any such contract change must be additive.
- **Role preview deferred**: the top-bar account menu currently shows only the
  authenticated backend role/scope from `GET /admin-web/bootstrap` plus Sign out.
  Do not reintroduce selectable role-preview choices until the selected lens is
  round-tripped through backend bootstrap/read-model compilation and page content.
  Future Audit Log `Viewing as` must stay synced to that backend contract; do not
  expand the old frontend-only helper or re-declare a separate Audit-only list.
- **Audit ownership**: only one agent may edit `apps/admin-web/features/
  operations-audit`, `apps/admin-web/app/(admin)/operations/audit`,
  `backend/internal/operationsaudit`, and `/operations/audit` OpenAPI/generated
  client artifacts during an Audit Log finish pass.
- **Architecture posture**: active admin-web architecture is accepted for this
  slice: generated clients through `lib/api/*`, no raw backend URLs, no direct
  datastore access, no hand-written DTOs, and no local route-handler business
  mutations. `apps/investor-web-shadow` is a legacy/reference snapshot; never
  copy its direct BigQuery or local API-route patterns into active admin-web.
  Component SRP debt is follow-up work, not a reason to rewrite large surfaces
  during Audit/Herd/Register pre-E2E closure. Use the backend admin-web
  bootstrap contract for shared shell/page/role-lens text.
- **Procurement / Source Entry**: `/procurement/source-entry` and
  `/procurement/source-entry/loads/{load_id}` are active for the supplier
  Holding Farm warmup -> accepted-intake branch and the dependencies that branch
  requires. Do not add nested procurement command-room routes.

The status model underneath every screen is the same: due, overdue, blocked,
proof-pending, verification-pending, rejected, deferred, and blocked.

## Backend-Driven UI Contract Rule

Admin-web is a renderer, not the product-truth owner. Visible navigation, route
availability, page titles, section/table labels, filter/sort/page-size
semantics, chips/tabs, row-click params, drawer/action labels, empty/error copy,
disabled reasons, and summary/detail field sets must come from
`GET /admin-web/bootstrap` and the generated `AdminWebBootstrapResponse` /
`AdminWebPageContract`.

Frontend code may own layout, CSS, responsive density, icon-token rendering,
hover/focus state, and local open/closed or selected-row state. It may choose
how much of a backend object to show in a compact row versus a drawer, but only
from backend-declared summary/detail fields. Do not add local visible literals
or option arrays in page components. Extend
`backend/internal/adminui/app/service.go`, `contracts/openapi/app-api.yaml`, and
the generated client instead, then consume through
`apps/admin-web/lib/admin-ui-contract.ts`.

Same-page overlays whose detail record is already in the rendered data set must
use `components/local-overlay-link.tsx` and a narrow client-local drawer state.
Keep a real hash deep link and Back/Forward semantics, but do not use a query-only
Next Link to re-run the route Server Component on open or close. The production
regression must prove one-click open, Back/Escape/scrim/X close, no global
route-pending flash, and zero Action Center/document/RSC requests. Run
`make admin-web-local-overlay-guard`; its legacy baseline may only decrease.

Any admin-web table or roster that lists individual goats must open the shared
Goat Passport local drawer from each row/cell using `LocalOverlayLink`. That
drawer must include the goat-wise vaccination passport (`/api/goats/{goat_id}/vaccination-passport`):
next due, open obligations, and vaccination history. Do not ship a plain roster
table that dead-ends on click, opens a separate route for row preview, or omits
vaccination history while another goat roster shows it. Add or extend the local
drawer guard for every new goat roster surface.

For live tenant/business data, even backend code is not the source of truth.
Locations, park codes/names, UUIDs, people, sheds, capacities, role/actor scope,
permissions, and DB-backed dropdown values must be read from Postgres or
source-backed config, then compiled into the admin-web contract. Never add CBE/
CPT-style constants, seed UUIDs, person names, or farm/shed capacities in React
or static backend contract code.

Stable UI values that rarely change (nav titles, page titles, table/filter
labels, chips/tabs, empty/error copy, disabled reasons) still belong to the
backend contract, not React. When those values need runtime governance, store
them as tenant-scoped `admin_ui_config_entries` rows and let
`/admin-web/bootstrap` compile them once with `contract_revision`,
`family_hashes`, and `cache_policy`. The frontend must block business UI until
bootstrap succeeds; it must not render local defaults and then replace them with
async config. Live/domain values such as parks, sheds, animal stages, protocol
vocabularies, SOP labels, grants, and feed items stay in their owning Postgres
tables and are compiled as DB families; UI config entries must not relabel those
live/module-owned option groups or override semantic option metadata such as
source-system publishability.

The only current exceptions are pre-contract auth screens and the emergency
contract-unavailable shell, documented in
`context/frontend/admin-web-backend-ui-contract.md`. Run
`npm --prefix apps/admin-web run check:ui-contract` before handoff or push.

## Date Display Rule (maintainer decision 2026-09-10)

Every VISIBLE date renders **DD/MM/YYYY**, with slashes, in full — in a table, a
card, a drawer, and on a **chart axis**, which no longer has a compact form of its
own. Timestamps render **DD/MM/YYYY HH:MM**. Use `lib/format.ts` `fmtDate` /
`fmtDateTime` / `dateTime`; do not hand-roll a date with `toLocaleDateString` or a
local `Intl.DateTimeFormat`.

This SUPERSEDES the 2026-08-21 rule (DD-MM-YYYY with dashes, plus a compact
`dd-mm-yy` axis). That rule governed admin-web ALONE and disagreed with the
backend's own `biztime.FarmDate`, which already emitted `02/01/2006` in
notification copy, and with Android, which rendered `14 Aug` for the same drive
this console called `14-08-2026`. One fact must not have three shapes.

Never render a wire field like `feed_day`, `*_date`, or `*_day` directly into JSX
text — that ships the API's ISO string to the operator's eyes. Wire formats
themselves (query params, API payloads, React keys, export filenames,
`todayIso`/`istDayPlus` arithmetic) stay ISO `YYYY-MM-DD` and must NOT be
reformatted.

A **month heading** (`Aug 2026`) and a bare **weekday** (`Mon`) are not dates —
they have no day component — and keep their own form.

Machine gate: `make date-format-guard` (also refuses the verbatim template format-time formatters fDate/fDateTime/fDateRangeShortLabel and useDateRangePicker().label/.shortLabel outside components/minimal + stories: they print "DD MMM YYYY"; use fmtDate) (repo-wide: admin-web, Android and backend
copy; canaries on all three shared helpers + scans for a screen hand-writing its
own date shape). Laundering a date through an intermediate variable before it
reaches JSX is a stated blind spot that review owns. Canonical prose:
`docs/decisions/date-display-format.md`.

## UI Source Of Truth

`../../mock/goatos-dashboard-mock.html` is the only admin-web UI/UX source of
truth. Port its layout, table shapes, empty states, icon system, spacing, font
scale, and density. It is not a color theme.

**RULE — default to the mock's full UX richness; diverge only for a documented
reason (do not make the maintainer repeat this):** when the mock looks good, do not
ship a lazier/plainer screen. Porting a mock element as a bare/simpler substitute out
of oversight is a defect — e.g. shipping a bare `All parks` / `As of <date>` pill where
the mock has a `Company-wide | Park-wise` toggle + `CBE · all sheds` selector +
`Date range … · data <date> · ⚠ Nd old` freshness chip; or a thin Filters button where
the mock's Counts/Herd filter modal has Park/Sex/Breed/Age Cohort/Shed/Pregnancy-
Lactation/Status/Identity Review/Origin Farm/Days-in-Stage/Weight/ADG + Clear all/Apply.
But this is NOT blind 1:1 pixel-copying: not every mock element must match. The app
diverges on purpose for **business reasons documented in the wiki**
(`/Users/ravi/mesha/wiki` + Graphify `mesha_docs_graph`), the scope-minimal/vaccination-
data-scope lock, and backend honesty (no fake rows/totals). **Read the relevant wiki/
business doc before deciding what to match vs diverge.** When you do diverge, do it
deliberately: a control the backend cannot power yet stays at the mock's look and is
disabled-with-reason (`aria-disabled`/title) — disable ≠ simplify, never a bare pill —
and an intentional business divergence must be grounded in a doc, not a guess. Green
build / `check:mock-fidelity` are NOT visual proof — diff against the mock and judge each
element against the documented business intent.

### Mock Component Anatomy Rule

Mock fidelity means porting the mock's component anatomy, not only importing its
CSS classes, matching colors, or using a similar shell.

For every in-scope screen, sidebar/nav, top-bar control, drawer, modal, card,
table, filter, and click action:

1. Inspect the exact mock markup/function AND the mock's CSS for that surface in
   `../../mock/goatos-dashboard-mock.html` before editing the app component or
   `app/mesha-theme.css`. Anatomy includes interaction states (`:hover`, `.on`/
   active, `.open`, focus), not just the resting markup.
2. Extract the visible structure: header, status block, assignee/due block,
   priority/status chips, linked tags, checklist/stepper, proof pills, form
   fields, footer action row, nav hover/active/group-open states, control
   sizing/padding, empty state, and disabled-with-reason states.
3. Implement that structure in the app with real backend data, or with a visibly
   disabled mock-shaped control when the backend contract is missing.
4. Do not replace a rich mock component with a flat key/value grid, generic
   `helpgrid`, bare button row, or simpler local invention just because it is
   easier.
5. If a mock subcomponent is already ported in CSS (`.drawer`, `.metagrid`,
   `.stepper`, `.step`, `.no`, `.ln`, `.ct`, `.vp`, `.fld`, `.chip`, `.tag`),
   use the same anatomy the mock uses for that class. Existing CSS is not proof
   of fidelity when the JSX body is a flatter substitute.
6. Before calling a surface done, produce a ledger entry for each route/drawer/
   modal: mock source selector or function, app component path, visible anatomy
   matched, backend-backed fields, disabled backend gaps, and screenshot proof.
7. Responsive proof is part of the anatomy, not a later polish pass. For every
   touched route, drawer, fullscreen overlay, dense table, sticky bar, or side
   rail, verify both laptop and phone widths. A page that looks acceptable at
   1440px but clips controls, forces page-wide horizontal scroll, hides actions,
   or leaves a desktop-width drawer/table on phone is not ready for handoff.

Known failure mode 1 (drawer body): Action Center / vaccination drawers used the
mock `.drawer` shell but rendered a flat `helpgrid`/metadata body plus a few
buttons. The mock's task/action drawer body is richer: computed-status block,
assignee + due, severity/priority chips, SOP checklist stepper with numbered/
check circles, video/proof pills, linked tags, and a full action footer. That
must be rebuilt from the mock anatomy, not approximated.

Known failure mode 2 (interaction states / control sizing): the sidebar nav was
"ported" by copying the mock's `.nav`/`.ggrp`/`.leaf` markup + classes, but the
hover states in `app/mesha-theme.css` were silently changed from the mock's
`background:var(--sidebar-2)` (a clearly lighter row) to a near-invisible
`color-mix(... var(--brand) 8% , var(--sidebar))`, so hover read as dead. Badges
were hardcoded (`#9f2f2f`) instead of `var(--danger)`, and top-bar controls
(`.iconbtn`/`.pscope`/`.parkpick`/`.me`) were bloated to a uniform `height:42px`
instead of the mock's compact sizes (38px icon button, content-sized pills).
Porting markup is not enough — the mock's `:hover`/active/sizing values are part
of the anatomy. The narrow (≤600px) ≥40px touch-target overrides belong in the
narrow media query only; they must not bloat the desktop control sizes. This is
now enforced: `scripts/check-mock-fidelity.mjs` fails if a nav `:hover` uses a
faint brand `color-mix` instead of `var(--sidebar-2)`.

Known failure mode 3 (right anatomy, WRONG content): the Action Center drawer's
"Checklist · SOP" was rebuilt with the mock's `.stepper` anatomy but fed the
obligation LIFECYCLE chain (Config published → Obligation generated → Batch →
SOP task → Proof → Verification → Completion → Booster) instead of the SOP's
PROCEDURE steps. The mock's drawer checklist is the SOP's operator steps (e.g.
"Drive scheduled", "Per-shed administration" + a "video proof required" pill,
"Consume posted", "Coverage + booster") — what the operator does, with per-step
proof gates — NOT the system lifecycle. The lifecycle chain belongs to the
Workflow record (`/workflows/{row_id}`), not the drawer. Drawer SOP steps come
from `features/preventive-care-vaccination/vaccination-sop-steps.ts` (shared with the Preventive Care (PC)
SOP quick-view) rendered by `features/process-integrity/sop-checklist.tsx` with
done/current derived read-only from the computed obligation states. Matching the
component shell + class is not enough — the checklist must show the SOP's steps,
and proof gates must render as the mock's `video proof required` pill.

Known failure mode 4 (dead rows / flat record drawers): in the mock, list/table
rows are tap-to-open — clicking a row (or matrix cell) opens a RECORD drawer.
Two regressions: (a) the Protocol Adherence ledger rows had NO click at all
(only the next-action cell linked out), so there was no record drawer; (b) the
vaccination status-matrix, per-cohort detail, shed-execution, and supplier-warmup
drawers DID open but rendered a flat `helpgrid` key/value stack instead of the
mock's RECORD drawer `.metagrid` (a 2-col grid of uppercase-key cells — for
adherence: Expected / Actual / Gap / Severity / Owner / Next action / Evidence).
Rules: every vaccination-flow list row/cell must be a `.celllink` → a drawer
keyed by a `?…_row`/`?…_record` search param (server-rendered, same pattern as
Action Center's `ac_row`); and record-drawer bodies use `.metagrid` (RECORD
anatomy), never `helpgrid`/a flat stack. The row-param drawer needs `scope_mode`
present in the URL or the shell scope-normalize drops the param (failure mode 2's
cousin) — in-app clicks already carry scope, so this only bites direct URLs.

Known failure mode 4b (linked table cells shredded into characters): `.celllink`
defaults to `overflow-wrap:anywhere` for card/free-text safety, but dense admin
tables own horizontal scrolling. Any table that makes cells clickable with
`.celllink` must add a table-specific nowrap override for `th`, `td`, and
`td .celllink` (`white-space:nowrap; overflow-wrap:normal; word-break:normal`)
plus per-column min-widths. Do not let short business values such as `female`,
`Adult`, `381d`, RFIDs, or dates wrap vertically. The regression guard for this
exact anti-pattern lives in `features/vaccination-sheds/shed-roster-table-wrap.test.mjs`.

Known failure mode 5 (overflow + wrong chain widget): (a) the drawer footer
(`.df`) action row was plain `flex` (no wrap) — a long primary label ("Assign
operator / owner chain") wrapped 3-lines tall and the last action (Escalate) was
clipped off the right edge. The mock has this defect too; the fix is better than
the mock: `.drawer .df{flex-wrap:wrap}` + `.df .btn{flex:0 0 auto;white-space:nowrap;min-height:38px}`
so every button is one line, equal height, and nothing is ever clipped. (b) the
Workflows chain map was rendered as a flat `.chain`/`.cstep` 7-card grid (every
card identical, no progress) instead of the mock's chain anatomy: a `.ostages`
stage-pill row (done/current highlighted) + the done/current/next/pending/blocked
`.legend` + the `.otree` of `.onode` cards (numbered/check `.dotn`, `.obody`,
`YOU ARE HERE`/`NEXT` badges, `.cur`/`.blocked`/`.pending` states) computed from
the obligation lifecycle. `.chain`/`.cstep` is only for the linear
Target→Group→Route→Execute drive band on `/vaccination`, NOT the workflow chain
map. Note: `.onode.pending` must not use the mock's `opacity:.62` (it fails the
visual smoke's WCAG-AA check on the meta text) — mark pending with a dashed
border + muted title instead.

Known failure mode 6 (stale old-admin components + data-vs-UI confusion):
(a) Dead old-admin/shadcn primitives lingered under `components/` (`cursor-pagination.tsx`,
`ui/error-state.tsx`, `ui/loading-state.tsx`, `ui/{button,card,table,tabs}.tsx`) — Tailwind
utilities + the banned cyan/slate palette (`#14F1D9`, `#334155`, `#8899AA`), a foreign design
system, never mesha-ported. The fidelity gate only scanned `components/mesha-shell.tsx`, so they
slipped through. Fixed: deleted them all (0 importers) and broadened `check-mock-fidelity.mjs`
SCAN to the whole `components/` dir so old-admin palette anywhere in components now fails CI.
(b) The herd register pager was a verbose `.bd` sentence; the mock uses the `.pager2` footer bar
(range/meta left via `margin-right:auto`, pager buttons right, top border) — use `.pager2`.
(c) DATA / SCALE ≠ UI bug, and must be explained as such, not "fixed" by faking: the vaccination
status matrix shows only the cohorts × PUBLISHED protocols that actually exist in the local DB
(e.g. 1 protocol × 2 cohorts), not the mock's 5-vaccine × 7-cohort sample; the herd's empty
WT/health/breeding cells are real goats lacking that data; and the herd uses CURSOR pagination
with NO total count (million-goat scale forbids `COUNT(*)`), so the mock's "1–10 of 39 / numbered
pages / 1,160 goats" is intentionally replaced by "Page N · M rows · server-paginated at scale".
Do not seed fake protocols/rows/totals to match the mock's sample density.

Do not reuse, adapt, recolor, or recreate the old admin UI. The old
`admin-primitives` component, old chart/layout components, and old dashboard
routes have been deleted.

Required before frontend handoff:

```bash
npm run check:mock-fidelity
npm run lint
npm run typecheck
npm run build
npm test
```

When local backend/admin-web can run:

```bash
npm run smoke:visual:live
npm run responsive:guard
```

Open the generated screenshots under
`.codex-goatos-render/admin-web-screenshots/` before claiming visual QA.

### Component visual regression (Storybook) — required for every UI change

Route sweeps catch a page that breaks; they do not catch a kit component that
quietly changes everywhere. `scripts/smoke-stories-visual.mjs` is the
component-level half and needs NO live app or API:

```bash
npm run storybook                 # http://localhost:6007, for authoring
npm run smoke:stories             # build + capture, no baseline compare
npm run smoke:stories:baseline    # THE GATE: compare against committed baselines
npm run smoke:stories:update-baseline   # ONLY for an intended visual change
npm run smoke:visual:all          # stories + route baselines + sales tolerance
# focused while iterating:
node scripts/smoke-stories-visual.mjs --no-build --only kit-tables
```

It builds `storybook-static`, serves it locally, and drives Playwright over
EVERY story at **1440x900 (desktop) and 390x844 (mobile)** in **both themes**
(4 captures per story), diffing each against
`.codex-goatos-render/admin-web-story-baselines/<storyId>__<viewport>__<theme>.png`
with pixelmatch (`--max-diff-ratio`, default 0.01). Play/interaction functions
run as part of the lane: a story whose play function throws fails the run, so
tabs, pagination, row menus, selects, dialogs and drawers are asserted as
behaviour, not just as pixels. Evidence + `summary.json` +
`diffs/<name>.png` land in
`.codex-goatos-render/admin-web-story-screenshots/<timestamp>/`.

Flags mirror `smoke-visual-live.mjs` exactly: `--baseline-dir`,
`--update-baseline`, `--require-baseline`, `--max-diff-ratio`; lane-specific:
`--only`, `--viewports desktop,laptop,tablet,mobile`, `--themes dark,light`,
`--no-build`, `--port`. Requires Node 24 (`nvm use 24`) and
`npx playwright install chromium`.

**Rules for Claude, Codex and any other agent — existing AND future work:**

1. Any change to `components/kit/**`, `features/**` or a page's visual shell
   requires a story in `stories/` covering its real states (default, selected,
   disabled, loading, empty, error, long text, many rows) and a 390px variant
   for every table, tab strip, popup/modal/drawer, pagination control and
   labelled chart.
2. `npm run smoke:stories:baseline` must pass on BOTH viewports in BOTH themes
   before any UI push. Desktop-only proof is not proof.
3. A baseline is rewritten only when the visual change is INTENDED: run
   `npm run smoke:stories:update-baseline`, then open the changed PNGs and say
   in the PR/handoff what changed and why. A silent baseline rewrite is a review
   finding, the same as a silent waiver.
4. A new route, tab, drawer-owned URL state or dynamic route still requires the
   route lanes to be updated in the same change (`smoke-visual-live.mjs` +
   `smoke-visual-route-coverage.test.mjs`), and its 390px lane must be green.
5. Never change brand tokens/hex in `app/mesha-theme.css` / `app/minimal-theme.css`
   to make a diff go away.

Both lanes are registered in `tools/ci/run-local-ci.sh` under the `admin-web`
job: the story lane always runs (skipped only under `GOATOS_FAST_LOCAL_CI=1`,
which therefore earns no landing receipt), and the route visual + sales
tolerance lanes run when `GOATOS_ADMIN_WEB_BASE_URL` is set.

For any admin-web change or review that can affect rendered UI, the visual QA
scope is laptop plus mobile, not desktop-only. Check every affected page,
nested page tab, left/right sidebar state, drawer/modal/popover, dynamic detail
route, chart, table, KPI card, legend, and horizontal-scroll region. If a page,
tab, drawer-owned URL state, or dynamic route is added or changed, update both
`scripts/smoke-visual-live.mjs` and
`scripts/smoke-visual-route-coverage.test.mjs` in the same change. Treat missing
responsive guard coverage as a review finding.

Do not present screenshots as proof until you have visually opened and confirmed
they show the intended page/state, not login, loading, an error, or a stale
route.

Mobile/WebView regression lanes (mandatory for every browser-visible change):

```bash
npm run smoke:webview:static                             # no app/browser needed
GOATOS_ADMIN_WEB_BASE_URL=http://127.0.0.1:3300 npm run smoke:webview
npm run smoke:webview:report                             # nothing waived; what a reviewer reads
node scripts/check-mobile-webview.mjs --routes <route>   # focused, while iterating
```

`scripts/check-mobile-webview.mjs` sweeps every smoke route at 1440x900 AND a
Pixel 5 Android-Chrome profile (393x851, mobile UA, touch, deviceScaleFactor), in
both themes, and asserts the seven recurring defect classes: no sideways page
scroll or shrink-to-fit zoom, no element off the viewport, every chart
axis/category label present, non-empty, visible, unclipped and never `----`, no
tap target under 44px, sticky headers that stick, overlays above their backdrop
and clickable, wide tables scrolling inside their card, pagination reachable,
`100dvh` instead of `100vh`, and filter controls still usable at phone width.
Evidence lands in `.codex-goatos-render/admin-web-webview/<timestamp>/`
(`report.json` + a PNG per failing route/viewport/theme) -- open the 393px PNGs
before claiming visual QA. Waivers live in
`scripts/check-mobile-webview-waivers/mobile-webview-waivers.json`; rewrite them
only with `npm run smoke:webview:update-baseline`, read every added line, and
never while the backend is down (every route would waive a contract-unavailable
page). Taxonomy + fixes: `.agents/skills/mobile-webview-guard/SKILL.md`.

Design-system gate (same standing): `.agents/skills/design-system/SKILL.md`, then
`npm run design:guard` (static), `npm run visual:stories` (every story, 1440 + 390, both
themes, interaction frames, render-integrity) and `npm run visual:routes` (desktop / phone /
WebView profiles, both themes). `docs/design/README.md` §4 has the exact commands and what each
lane asserts. Waivers: `scripts/check-design-system-waivers/`, `visual-baselines/*/waivers.json`
-- shrink-only.

### R2 visual gate (interactive, template-compared)

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
Existing P0 debt is the shrink-only baseline `apps/admin-web/scripts/r2-visual-audit-baseline.json`
(pattern -> route count): a NEW pattern or one reaching MORE routes fails; `GOATOS_VISUAL_GATE_STRICT=1`
fails on every P0. Shrink it with `node apps/admin-web/scripts/r2-visual-audit.mjs --write-baseline`
after a fix; growing it to land a change is a review finding. Per-route ratchet: the baseline also lists every failure
pattern per route, and on the 5 shell routes (`SHELL_ROUTES`) plus every route the change touches (import
graph of the changed files) ANY pattern that route's entry does not list fails, P0 or not. `--fast`
(`npm run visual:gate -- --fast`, the pre-push lane, ~2-3 min incl. no rebuild when `.next` is a clean
`npm run build` of HEAD) audits only those routes with scan + interactions; the full run stays in
`make land-check` / ci-local. `--routes /a,/b` audits exact routes. It runs in the `admin-web` ci-local job
(when `GOATOS_ADMIN_WEB_BASE_URL` is set, so `make land-check` / `make land-main` run it) and from the
pre-push hook for pushes touching admin-web UI (opt out only with `GOATOS_SKIP_ADMIN_WEB_VISUAL_GATE=1`,
stated in the PR). Output: `~/mesha/redesign-shots/r2/audit-<timestamp>/report.md` + `report.json`,
failures ranked by PATTERN across routes (e.g. "KPI card bg in dark: 23 routes"), plus `sbs/`,
`frames/`, `drawers/`, `skeleton/` images — open them. New checks from other work plug in as
`apps/admin-web/scripts/r2-audit-checks/<name>.mjs` (default export `{ name, p0, profiles, run(page, ctx) }`)
and land in the same pattern summary.

The default smoke lets the calendar drive-target roster be empty (logs
`identity_calendar_roster=skipped_no_targets`). To hard-assert the
Display ID / Tag 1 / Tag 2 identity columns on a vaccination drive drawer, run
`npm run smoke:calendar-identity:live`
(`GOATOS_SMOKE_ONLY_ROUTES=calendar GOATOS_SMOKE_STRICT_CALENDAR_IDENTITY=1`).
It currently fails by design — the local seed generates no obligations for drive
`protocol_version`s, so every drive roster is empty — and goes green once that
seed gap is fixed. Scope any run with `GOATOS_SMOKE_ONLY_ROUTES=<name,...>`
(validated up front; unknown names fail fast).

## Scope Chrome Rule

Keep park/date scope in the top bar. Remove repeated park/date/scope chips from
page bodies.

The shell top bar is the single visible owner for park and as-of date scope. If
the selected scope is CBE, the top bar must say CBE; if it is all parks, the top
bar says All parks. Page bodies must not repeat the same scope as another
"Scope", "All parks", or date chip row.

Company-wide vs Park-wise is a presentation lens, not a hidden data-source
switch. Do not invent a separate "global" or "central" dataset unless a concrete
backend contract explicitly returns one. For the current admin-web slice:

```text
Company-wide = all in-scope data shown as one leadership/company rollup.
Park-wise + All parks = the same all in-scope data shown through park/shed
                        breakdown, grouping, or filters.
Park-wise + CBE/shed = only that selected park/shed scope.
```

If the app cannot yet render a meaningful aggregate-vs-breakdown difference,
disable or remove the Company-wide/Park-wise toggle rather than assigning it
fake semantics. "All parks" means everything in the current slice across parks;
it must not exclude invented central/admin rows.

Leadership assistant (CEO/CXO read-only chatbot): admin-web is only the renderer.
The assistant brain is server-owned (`backend/internal/ceoai`); the browser never
calls Cube/Toolbox/Postgres directly, and the leadership gate is a server session
role (`ceo_internal`), not a client-side name regex. Any new admin-web route,
feature, or read that is leadership-relevant must land matching assistant coverage
(a Cube metric / `ceo_ai.*` view / MCP tool / read-API mapping / GenAI
query-class) or a documented exclusion in `docs/ceo-ai/coverage-matrix.md`, in the
same change. HOW-TO: `.agents/skills/goatos-leadership-assistant/SKILL.md`;
enforced by `make leadership-assistant-coverage-guard`. Response fields the
renderer may show are exactly `answer, source, mode, request_id, citations,
conversation_id` (+ streamed tokens); never render step traces / chain-of-thought.

Page bodies may show only page-specific controls:

- Action Center: Status board/SOP queues, domain chips, work-state chips, My
  tasks, Filters, and board columns.
- Protocol Adherence: KPI cards, severity filters, and the expected/actual/gap
  table.
- Workflows: workflow search, filters, catalog, and chain map.
- Preventive Care (PC) / Vaccination park/shed execution section (`#execution`): execution
  filters, the per-park execution table, and the shed drilldown. It carries no
  separate park scope chip — the top bar owns park scope.

If a page needs park filtering, put it behind Filters or update the top-bar
scope. Do not duplicate park/date scope inline.

## Implemented Routes

Only these routes are current implemented product routes:

```text
/login
/                          Control Tower      (top-level command)
/action-center             Action Center      (top-level command)
/protocol-adherence        Protocol Adherence (top-level command)
/workflows                 Workflows          (top-level command)
/workflows/{row_id}        Workflow drilldown
/vaccination               Preventive Care (PC) Vaccination module surface (NOT Action Center);
                           includes status matrix, cohort detail, and execution
/vaccination/execution/sheds/[shedId]
/vaccination/live-tracker   Live Drive Tracker — today's drive at ADMINISTRATION grain: per-operator
                           and per-shed proof progress, combo doses, live activity feed, attention,
                           verification queue. Polls by re-running the server tree (router.refresh).
/procurement/source-entry    Source Entry Board for supplier warmup / accepted intake
/procurement/source-entry/loads/{load_id}
/sales                       Sales board — its OWN top-level vertical, split out of Procurement
                            (maintainer decision 2026-08-27). Animal + manure sales overview,
                            buyers, demand pipeline, sale evidence, deals ledger, record-sale
                            drawer. The record-sale form maps every deal to a vendor from the
                            procurement register, so the page also needs procurement.vendor.read
                            alongside sales.read / sales.write (backend "sales" page contract).
                            Moved from /procurement/sales; there is no redirect.
                            RETIRED 2026-09-11: divided into Sold and Farm value; /sales now
                            only redirects to /sales/sold (query preserved).
/sales/sold                  Sold — headline figures, sold weight bands, month by month, price per
                            kg by breed, buyers, demand pipeline, sale evidence and, last, the
                            deals ledger with its read-only deal drawer (re-homed)
/sales/farm-value            Farm value — total farm value, total meat, Over 35 kg with its error
                            margin, and the by-category breakdown (re-homed)
/counts/herd                 Herd Register for vaccination trigger closure (route live; its
                             sidebar leaf is WITHHELD, maintainer decision 2026-08-20)
/counts/analytics            Herd Analytics — composition now (breed / pen tag / sex / kid-adult /
                             farm) beside month-by-month births, deaths, sales and pen movements
/counts/breakdown            Counts Breakdown census (farm x stage x breed x gender x shed)
/operations/audit            Admin / Data Ops Audit Log (business surface)
/config
/vaccination/sops           Vaccination SOP (module-surface; builder at ?compose=1)
/counts/sops                Herd Operations SOP — birth / death / shifting
/feed/sops                  Feed SOP — distribution / packing / transport
/milk/sops                  Milk SOP — preparation / feeding (SOP split extension, 2026-08-22)
/weighing/sops              Weighing SOP — scan-and-submit session (SOP split extension, 2026-08-22)
/weighing/analytics         ADG Analytics — growth cut seven ways (leaf renamed from "Weights
                            analytics", maintainer request 2026-09-03); its Load-wise tab holds
                            the per-purchased-load purchase-weight vs latest-weighing comparison
                            with the growth multiple (moved from a short-lived /weighing/comparison
                            leaf the same day)
/weighing/weights           Weights read-out (route live; its sidebar leaf is WITHHELD,
                            maintainer request 2026-09-03 — deep link only)
/goats/{goat_id}
/verify                     Verify — cross-module verification evidence (top-level, below Approvals)
/actions                    Compatibility redirect to /verify (route renamed 2026-08-12)
/verification               Compatibility redirect to /verify
/approvals                  Approvals — birth/death/shifting decision queue (top-level; RBAC:
                            director/head/manager/am + admin + ceo_internal + counts_approver)
```

Scope note: `/approvals` is a top-level decision surface, gated server-side by
`counts.approve_access` (the four org tiers + admin + ceo_internal, plus the per-person
`counts_approver` authority). It renders from local literal copy until a backend `approvals` page
contract lands.

It is NO LONGER the only approval surface. The mobile Approvals module returned on 2026-08-05,
superseding the 2026-07-21 decision that had moved approvals to web only. Both surfaces are served
by the same service, the same permission, and the same list/decide logic under different route
prefixes — so a change to approval authority or to the queue's shape affects BOTH, and neither may
grow its own private business truth. The queue response now carries backend-composed
`raised_by_name` / `summary_line`; this page still builds its own readable subject from resolved
location names, and may adopt the shared line later.

`/verify` is the AUTHORITY act screen for the generic Verification vertical
(`context/architecture/verification-module-design.md` + `verifier-app-and-flow.md`):
authorized reviewers browse the standalone Verifier's due/approved/rejected media queue and
act on the linked SOP task (rework / re-assign; penalty note is honestly
disabled — no backend contract exists for it yet). It reads the real, merged
`/verification/queue` contract (generated `AppApiComponents["schemas"]["Verification*"]`
types in `lib/api/server.ts`, no hand-typed shapes) and acts through the EXISTING
`/admin/tasks/{task_id}` `/rework` `/assign` routes.
The backend `verification-review` page contract owns its title, table, drawer
copy, and disabled reasons; the Verification registry owns the action-type and
status filter options. It appears as Verify — a TOP-LEVEL primary nav item directly below
Approvals, not inside any group — and sends park plus its own capture-date filter to the backend.
Named to match the phone, which has always called this Verify (workforce `nav.verify`).

Implemented top-level command route:

```text
/calendar                  Calendar           (top-level command, vaccination due-work slice only)
```

Keep `/calendar` tied to the Preventive Care (PC) Vaccination due-work slice: route, primary nav,
mock-fidelity scan coverage, and `smoke:visual:live` coverage must move
together if the Calendar surface changes.

No nested compatibility redirects are allowed for command-room, authority, or
Vaccination execution screens. Use `/protocol-adherence`, `/workflows/{row_id}`,
`/config?category=vaccination`, `/vaccination`, and
`/vaccination/execution/sheds/[shedId]` directly. `/parks/vaccination` is not an
allowed product route or redirect.

## Hard Rules

- Control Tower / Action Center / Protocol Adherence / Workflows are top-level
  command screens. Config is the top-level Admin / Data Ops
  authority screen. Never nest them as tabs or compatibility redirects under
  Preventive Care (PC) / Vaccination, Procurement/source-entry, Parks, or any future vertical.
  `/vaccination` is the Preventive Care (PC) operations surface and links out to them.
- Never duplicate top-level command screens under procurement/source-entry or any
  other vertical. Use `?domain=procurement` or equivalent top-level filters
  instead of nested command-room routes.
- Preventive Care (PC) is a vertical and must not use the syringe/injection icon.
- Vaccination may use the syringe/injection icon.
- Parks is a vertical, but it does NOT own the Vaccination product route/module.
  Vaccination execution context
  (park/shed/stage/defer/blocker/owner/proof/verification status)
  renders ONLY inside `/vaccination`, scoped by the top-bar park
  dropdown. There is no separate Parks sidebar entry, no nested command screens
  under Parks, and no `/parks/vaccination` product path. The execution read-model
  endpoints `/vaccination/execution` and `/vaccination/execution/sheds/{shed_id}`
  power that data.
- Backend/API failures must surface as a visible error state (an alert band or
  error card), never be swallowed into an empty array that reads as "no data".
  `route_not_registered` and other API errors are bugs to surface, not empty UI.
  Empty-but-OK responses keep every section visible with zero-count badges and
  empty copy — they do not collapse the page to one empty billboard.
- Control Tower must not show raw goat census/count totals or generic dashboard
  KPIs.
- Goat Passport is reached from scoped rows, cohorts, obligations, or known URLs;
  do not add a global million-goat search as the main workflow.
- Backend/API/RBAC/session wiring may be reused; old frontend routes and old
  visual shell must not be reused.
- Reopened active surfaces may build their basic setup dependencies, but only
  against current GoatOS contracts and the mock. Do not reuse deleted old
  dashboard/admin code, old admin primitives, legacy Counting DB runtime shapes,
  or old import-review screens.
- Do not add direct BigQuery, Sheets, GCS, Firestore, or database access from
  frontend code.
- **Every page renders on a phone** (maintainer rule 2026-09-14). The dashboard is
  opened on phones; a change is proven at 1440px AND at 390px (Chrome device
  emulation) after the final edit, and the phone screenshot goes in the handoff.
  Nothing clipped, nothing broken, no sideways page scroll; wide tables/charts
  scroll inside their own `overflow-x: auto` wrapper. Fixed px widths >= 480 on
  ordinary boxes, grids with a px floor over 360, and `overflow-x: hidden` on
  page-level elements are refused by `make admin-web-phone-viewport-guard`
  (baseline is shrink-only); stack layouts under `@media (max-width: 600px)`
  instead. The 390px lane of `npm run smoke:visual:live` is the runtime proof.
  Canonical: `docs/decisions/admin-web-phone-viewport.md`.
- **A click costs what it changes** (maintainer rule 2026-09-18). A same-page
  drawer opens client-locally from the row already on screen (capture-phase
  intercept, `pushLocalOverlayUrl`, detail fetched inside the drawer); an
  in-place write returns the row for the client to apply and never ALSO
  `revalidatePath`s; a Board/List toggle is client state. Controls are the
  console's own: a tick is a real `<input type="checkbox">` in a `<label>`, a
  date is `ThemedDatePicker`. `make admin-web-interaction-patterns-guard`
  (shrink-only baseline) + `make admin-web-local-overlay-guard`. Canonical:
  `docs/decisions/admin-web-interaction-patterns.md`.

## Removed From Active Admin-Web

Do not rebuild these unless the product scope is explicitly reopened:

```text
/locations
/operators
/tasks
/import-review
/data-quality
/data-quality/review-guide
/legacy-sync
/dashboard/mortality
/herd
```

`/sops` was reopened as the Admin / Data Ops SOP Library, then RETIRED by the
SOP split (maintainer decision 2026-08-18) in favour of the per-module pages
`/vaccination/sops`, `/counts/sops`, and `/feed/sops`; do not rebuild a
top-level `/sops`. `/counts/herd` is active for
Herd Register, and `/operations/audit` is active as the Admin / Data Ops business
Audit Log for the vaccination trigger-closure slice.
Old `/herd`, unrelated Counts modules, old `/tasks`, old Operations, old generic
SOP/task pages, and old admin primitives stay removed. For Counts, removed also
means not visible as disabled sidebar placeholders unless a future approved slice
explicitly reopens them.

The live `../../dashboard/` repo remains untouched reference material only.
