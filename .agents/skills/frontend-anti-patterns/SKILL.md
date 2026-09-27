---
name: frontend-anti-patterns
description: >-
  Use when writing OR reviewing admin-web (apps/admin-web/**) — pages, SSR data
  reads, nav, labels, dashboards, same-page drawers, sidebars, modals, and
  popovers. Covers the backend-owns-the-contract golden
  rule, Next.js/React/TypeScript engineering quality, no SSR full-table request
  reads, selected-window fetch=render, mock fidelity, and projection-backed
  dashboards. Thin entrypoint: detailed rules live in the canonical chapters
  linked below. Invoke before touching an admin-web page/route/data-read and
  before pushing. Machine gates: npm run check:mock-fidelity + make
  admin-web-request-reads-guard + admin-web-local-overlay-guard +
  admin-web-interaction-patterns-guard.
---

# Frontend (admin-web) anti-patterns — lens entrypoint

Admin-web is a RENDERER, not a product-truth owner. The backend contract owns
what is visible; the client owns layout and local UI state.

Every operator-facing admin-web route has a hard sub-500ms hot-load budget when
served from the local/perf stack. A seconds-class SSR/API read is not solved by a
skeleton, spinner, prefetch, or client cache. Repoint the page to the narrow
backend contract that matches the screen grain/window, or fix the backend
serving read before pushing.

This skill is a **table of contents**, not the rulebook. Open the canonical
chapters below; do not review from the summary.

## When this lens applies
- Any change under `apps/admin-web/**` (pages, SSR data reads, nav, labels,
  drawers, dashboards) or `packages/ui` / `packages/rbac` / `packages/forms-dsl`.
- Any date-range / week / month picker feeding a fetch and a render.
- Any dashboard that slices by month/date/breed/farm/shed/status/etc.
- Any route/load measurement at or above 500ms, especially when a narrow page is
  fed by a broad catch-all endpoint.

## Canonical detail (read these — do NOT duplicate here)
- **Review chapter:** [`.agents/skills/goatos-code-review/references/frontend.md`](../goatos-code-review/references/frontend.md) (+ [`.agents/skills/goatos-code-review/references/mobile.md`](../goatos-code-review/references/mobile.md) for the mobile twin).
- **Selected-window fetch=render + reminder completeness:** [`docs/decisions/calendar-ownership.md`](../../../docs/decisions/calendar-ownership.md).
- **Projection-backed dashboards:** [`docs/decisions/high-scale-dashboard-projections.md`](../../../docs/decisions/high-scale-dashboard-projections.md).
- **Over-fetch anti-patterns (twin):** [`docs/decisions/mobile-data-fetch-anti-patterns.md`](../../../docs/decisions/mobile-data-fetch-anti-patterns.md).
- **Nav composition:** [`docs/decisions/role-module-nav-composition.md`](../../../docs/decisions/role-module-nav-composition.md) + the `nav-composition` skill.
- **Contract authority:** AGENTS.md golden frontend rule · [`context/frontend/current-admin-web-scope.md`](../../../context/frontend/current-admin-web-scope.md) · the mock `mock/goatos-dashboard-mock.html`.
- **Framework/runtime/testing quality:** [`docs/frontend/admin-web-engineering-quality.md`](../../../docs/frontend/admin-web-engineering-quality.md) — Next.js server/client boundaries, React purity/effects, TypeScript/Node, query keys, error states, semantic UI, Playwright/axe/visual proof, and CI recurrence checks.

## Machine gates
- `npm --prefix apps/admin-web run check:mock-fidelity` — mandatory before any
  frontend push (IA guard, UI-contract literals, serial-await, request-plan, mock).
- `make admin-web-request-reads-guard` — no SSR full-table request read.
- `make admin-web-sectioned-aggregate-reads-guard` — no admin-web page may call a
  sectionable aggregate endpoint for the whole payload when it renders only a
  few sections. Pass `sections` matching the rendered widgets, or add an adjacent
  `sectioned-aggregate-reads:allow reason=<why>` exception only when the route
  truly renders the full aggregate payload.
- API/SSR latency evidence when a page data read changes — p90 <= 300ms and
  p95/p99 <= 500ms. A green `ci-local` build is not latency evidence unless the
  latency gate ran against a live stack and recorded samples.
- `make admin-web-local-overlay-guard` — zero route-driven same-page overlays;
  no Next/native open, close, veil, or schedule-drawer navigation baseline.
- `make admin-web-interaction-patterns-guard` — no native date/time input, no
  ARIA-faked checkbox, no server action that returns a row AND revalidates the
  route, no `view=` link for a Board/List toggle. Whole-tree, shrink-only
  baseline. [`docs/decisions/admin-web-interaction-patterns.md`](../../../docs/decisions/admin-web-interaction-patterns.md).
- `node tools/agent-hooks/check-refresh-binding.mjs` — selected-window binding.
- `make calendar-endpoint-grain-guard` — no narrow schedule/full-schedule surface
  wired to the broad Calendar events endpoint.
  All registered in `tools/ci/guardrail-manifest.json`.
- `npm --prefix apps/admin-web run lint && npm --prefix apps/admin-web run typecheck && npm --prefix apps/admin-web run test` — framework and unit baseline.
- `GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build` — production build with the token-leak check actually exercised.
- `npm --prefix apps/admin-web run smoke:visual:live` — required for changed UI/navigation/error/loading behavior; inspect screenshots and a11y output.

## At a glance (detail in the links above)
- **Backend owns the contract:** nav, titles, labels, filter/sort/page-size,
  chips, row-click params, drawer/action labels, empty/error copy, disabled
  reasons, summary-vs-detail — never hardcoded in a page.
- **Partition display rule (MANDATORY):** when a shed partition exists (`Castro 1` +
  `Castro 2` as separate physical locations, or `Godel 1 - Part 3` as a partition),
  render the partition label in product displays, never collapse into parent. Full rule,
  worked examples, and schema requirements: [`docs/decisions/operational-location-convention.md`](../../../docs/decisions/operational-location-convention.md).
- **No SSR full-table request reads:** don't drain a paginated endpoint cursor-by-
  cursor into one array (the `searchAllGoats` walk); read a projection/summary.
- **No broad endpoint for a narrow screen:** a month schedule page must not call
  a broad calendar/events union and then reshape it. Use the endpoint whose
  contract owns that screen's grain/window.
- **No full sectionable aggregate for a narrow page:** if the endpoint accepts
  `sections`, the rendered page must pass exactly the sections it displays. Do
  not make a landing page pay for hidden analytics tabs, leaderboard branches,
  or full-route aggregate payloads.
- **Selected-window drives fetch AND render:** one window for both; no implicit
  `now`/`today` substituted server-side; render the actual-returned window.
- **Reminder/candidate completeness:** a paginated reminder loop must reach every
  candidate or clearly mark "partial" — no silent page-size truncation.
- **Dashboards → projection:** slice-by-dimension is projection-backed, not
  compute-on-read.
- **Command-lens authority:** Control Tower / Action Center / Calendar / Protocol
  Adherence / Workflows are top-level only; feed via `?domain=`/`?category=`.
- **Server-first App Router:** pages/layouts stay Server Components; place
  `'use client'` on the smallest interactive leaf and keep tokens/adapters in
  `server-only` modules.
- **Same-page overlays are local:** ordinary open/close must not navigate or
  request an RSC payload. Use `LocalOverlayLink` plus a narrow local controller;
  preserve Back/Escape/outside/X and trigger-focus restoration. If detail was
  not in the list response, open from the summary immediately and fetch only
  the detail inside the drawer. A query-only Next `Link`, native anchor/form, or
  router push used to toggle an overlay is a merge-blocking anti-pattern.
- **A click costs what it changes (2026-09-18, Tasks page):** a card click, a
  status change in a drawer and a Board/List toggle each re-rendered the whole
  route (~0.9 s + skeleton flash) for rows already on screen. Now: the drawer
  host intercepts the click (capture phase) and opens client-locally, fetching
  only the missing detail inside the drawer; an in-place write RETURNS the row
  and the client publishes it to the feature row store — it never also
  `revalidatePath`s (that is the flicker); a view toggle is client state with
  `replaceLocalOverlayUrl`. Reference: `features/leadership-tasks/task-drawer-host.tsx`,
  `task-row-store.ts`, `task-view-switch.tsx`.
- **Controls are the console's own:** a tick is a real `<input type="checkbox">`
  in a `<label>` (never a coloured square or `aria-checked` button); a date is
  `ThemedDatePicker` (never `<input type="date">`); a confirm is two buttons in
  place (never `window.confirm`).
- **Public write surfaces:** re-authenticate, authorize, validate, and preserve a
  stable idempotency key inside every Server Action/Route Handler; page auth and
  a disabled button are not security boundaries.
- **React state:** derive render data directly; Effects synchronize external
  systems and clean up. Do not mirror server/query data into local state.
- **Query correctness:** every result-changing scope/window/filter/cursor belongs
  in the TanStack query key; await invalidation after successful mutations.
- **Visible failure:** data routes need distinct loading, empty-success,
  permission, contract-unavailable, and unexpected-error UI plus an App Router
  error boundary.
- **Browser proof:** prefer role/label locators and web-first assertions; no new
  fixed sleeps. Axe plus pixel screenshots do not replace keyboard and visual
  review.

## UI must match the MUI Minimal kit (machine gate: `npm --prefix apps/admin-web run design:guard`)

- Use `components/kit` + `DataTable`/`DenseTable`/`Tag` and `var(--…)` tokens from
  `apps/admin-web/app/minimal-tokens.css` only. Raw px/radius/shadow/font-size literals and raw
  `<table>`/`<button>`/`<select>`/tablist/dialog/tooltip/chip markup in feature code fail the guard.
- New component: spec (`docs/design/mui-minimal-spec.md` + token) -> kit -> story (states + 390px,
  light/dark) -> baseline. A kit component without a story fails `kit-missing-story`.
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
  - `vaccination-plan-template` (`features/vaccination-plan/responsive-css.test.mjs`): /vaccination/plan and /plan/edit stay on template parts (CourseWidgetSummary KPI row, live table in UrlSuspense keyed by `page`, Card + CardHeader, TableHeadCustom, MUI Dialog / ToggleButtonGroup / Chip, Grid md 4 / md 8); no `.vplan` / `.vp-*` CSS or classes, no raw controls, no fixed-position div modals.
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
  `<section>` come back; `lt-truncnote` is only the visible-reason marker.
- **Every info "i" is the shared InfoTip (guard: `info-tip-tap`, npm test).** No local
  `function InfoTip` / `InfoTooltip`, no `.ihelp` / `.tipwrap` button inside a hover Tooltip: those open
  on hover only, so a tap in the Android WebView does nothing (/herd-signals column help, REVIEW-15
  O19). Use `components/app/info-tip.tsx` (controlled Tooltip, 44px IconButton, opens on tap).
- **/herd-signals shells are template anatomy (guard: `herd-signals-template-anatomy`, npm test).**
  Board / Alerts / Gateways / Insights render MUI Card + CardHeader, Alert, Grid and Label; no legacy
  card / hd / bd / banner / gwcard / insight / rowitem / pager / btn classes.
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
- A restyle never introduces new UI behaviour; never reopen an item in `docs/design/redesign-regression-guard.md`.
- Mobile webview: tap targets >= `var(--tap-min)` (44px), fixed overlays through kit `BodyPortal` /
  `Sheet` / `Dialog`, `100dvh`, no sideways page scroll.
- Anti-pattern (R2-4, /sales/config "Tag animals to sale"): a right drawer that squeezes its table and clips the Total column, with no backdrop. **Right drawers are the template temporary Drawer** (Ravi R2-4): `MinimalDrawer` (components/app/drawer) or `DetailDrawer` (components/app/detail-drawer) only — portalled, anchor right, visible backdrop (never `invisibleBackdrop` / `backdrop: { invisible: true }`), template paper width 320 (filters) / 360 (settings) / 420 (notifications) / 480 (details, forms; `{ xs: 1, sm: 480 }`), sticky header title + close, Scrollbar body, footer actions. No raw MUI `<Drawer>` for a right drawer, no hand-rolled `<aside className="drawer">` + `.scrim`. Wide content never squeezes or clips at the drawer edge: a table sits in `DrawerTableScroll` (own Scrollbar, `Table sx={{ minWidth }}`) and scrolls sideways. Guards: design:guard `drawer-off-template` (static), `scripts/r2-drawer-audit.mjs` (runtime: width, backdrop, no child overflow without its own scroll container).
- Legacy debt is a shrink-only per-file ratchet in `design-system-waivers.json`; never raise it.
- Production bug CLASSES are automated guards. `scripts/lib/visual-pattern-guards.mjs` (route visual
  lane) adds `P-text-icon-overlap`, `P-wide-table-no-wrapper`, `P-chart-axis-tiny` (<11px),
  `P-pinned-bar-blur-flicker`, `P-drawer-filter-mismatch` and `P-chart-hover-remount`. `raw-chart-lib`
  refuses recharts/d3/chart.js/nivo/victory/visx/echarts/highcharts — charts are Apex (via
  `components/minimal/chart` or `components/kit`) or the two inline helpers (`svg-bars`, `svg-series`).
  Full pattern → guard table: `docs/design/README.md` §5b.
- Green-tinted neutrals or hand-picked greys: neutrals are the template's (P0 `template-neutrals`,
  `retired-neutral-literal`).
- Pastel/white KPI boxes in dark mode and legacy CSS recolouring MUI parts (R2). KPI/widget cards are the VERBATIM template
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
 in `scripts/smoke-visual-live.mjs`
  matches a `route_prefixes` entry in `docs/design/route-template-map.json`; a NEW page must add its
  area in the same change or fail `route-template-map-missing`. Template lives at
  `~/mesha/mui/Minimal_TypeScript_v7.7.0` (Ravi laptop; licensed, NOT in the repo). Palette is
  locked to Mesha green; template gives structure, density, motion and interaction patterns only.
- **Template files are verbatim (guard `template-verbatim`, design:guard p0, self-test).** Ravi 2026-09-27: "use the SAME mesha-ui template across the pages and just put our content." Every file mapped in `docs/design/template-sources.json` (`components/minimal/**`, `layouts/**`) equals its source in `~/mesha/mesha-ui/vendor/minimal/Minimal_TypeScript_v7.7.0/next-ts` byte-for-byte, except import paths (`src/...` -> `@/components/minimal/...`, `@/layouts/...`, `@/theme/...`; package imports unchanged) and a leading `"use client"`. No prop, sx, copy, comment or behaviour edits inside them; Mesha colours come only from the theme. The manifest stores the sha256 of each normalised template source (the template is not in CI); refresh with `node apps/admin-web/scripts/refresh-template-hashes.mjs` after copying a new template file. A component with no true template source never lives under `components/minimal/` (no KpiCard / StatStrip / progress-item / AnimatedTabs wearing a template path): use the real template component (EcommerceWidgetSummary / CourseWidgetSummary / BookingWidgetSummary, the template Tabs + Label anatomy, EcommerceSalesOverview progress rows). Product behaviour (URL-linked tabs/pagination, data mapping, i18n labels) lives in `components/app/` adapters or feature files that only render template/MUI components and pass props/children (no own CSS, no raw px/colours). Remaining drift is the shrink-only list in `docs/design/template-verbatim-baseline.json` (a healed file must be removed; nothing may be added). The drift baseline keys each listed file on the sha256 of its bytes: editing a baselined file fails too (restore it to the template and drop the entry; never re-record a hash).
- **No pastel fallback on mapped pages (guard `page-template-no-pastel`, p0).** A page with a row in `docs/design/page-template-map.md` must not render `AnalyticsWidgetSummary`; KPI rows are `EcommerceWidgetSummary` / `CourseWidgetSummary` (via `components/app/kpi-widget.tsx`), charts are template chart cards (CardHeader + select), lists use the template table anatomy.
- **Template sections keep their client boundary (guard `section-client-boundary`, p0).** A file under `apps/admin-web/components/minimal/sections/` that calls a hook or passes a function `sx`/`(theme) =>` callback must start with `'use client'`; a server page rendering it would pass a function across the RSC boundary and crash at render. `next build` must pass before every push. Modules that import `useLinkStatus` / `useRouter` / `useSearchParams` / `usePathname` straight from `next/link` or `next/navigation` must start with `'use client'` (test `client-only-hook-directive`, `components/client-only-hooks-directive.test.mjs`).
- **No legacy card shells on mapped pages (guard `page-template-legacy-card`, p0).** A page with a row in `docs/design/page-template-map.md` must not render `className="card"` / `"wchart"` / `"wtable"` / `"kpi"` sections or `<h2 className="h">` headings: every block is a template section card (Card + CardHeader, e.g. `BankingBalanceStatistics`, `AnalyticsWebsiteVisits`, `EcommerceSaleByGender`) fed our data, and the legacy CSS behind those classes is deleted as pages stop using it.
- **Wide matrices never crush their column headers (test `notification-matrix-header-floor`).** A table with one column per designation/role/day gives each column a readable floor (`Table sx={{ minWidth: fixed + floor * n }}`, `<col style={{ width: floor }}>`), scrolls sideways inside its card with the label column sticky, and keeps headers on one line (`nowrap` + ellipsis + Tooltip with the full label). Never shrink header text (10px) or let it wrap letter by letter to fit.
- **Third-party grids are shielded from the legacy table rules (test `calendar-legacy-table-reset`).** The shell's legacy `.wrap/.screen/.main th|td` rules also hit raw `<th>/<td>` inside FullCalendar and similar widgets (grey band, 48px+ rows, padding). Reset them at higher specificity inside the widget root (`CalendarRoot` `legacyTableReset`), never by adding rules to the legacy CSS files. The month grid is `height="auto"` at md+ (no inner scroller clipping the last week); calendar titles use `lib/format` DD/MM/YYYY.
- **File pickers are the template Upload area (test `config-drawer-upload-template`).** A feature never renders the browser's bare `<input type="file">` next to a legacy `.btn`: use `UploadFile` (`components/minimal/upload`, template UploadDefault anatomy, the native input hidden inside so refs/forms read it as before) and a kit `Button` for the action. Required form fields pass `required` and let MUI add the one asterisk (never a hand-typed `" *"`).
- **Sidebar and header are the template dashboard layout (guard `shell-nav-template`, p0; test `components/sidebar-viewport.test.mjs`).** The nav is `NavSectionVertical` / `NavSectionMini` (layouts/template/nav-section, verbatim) inside the template-derived `layouts/app/dashboard/nav-vertical.tsx` / `nav-mobile.tsx`, fed by the backend bootstrap nav. The whole nav scrolls in the template `Scrollbar`, logo fixed; only the active group opens (no `default_open` subtrees); no custom nav footer (`navBottom`, `msh-foot`, `navigation.footer` - the template only has the optional NavUpgrade card, which we do not use); the phone nav is the template drawer (`var(--layout-nav-mobile-width)` over the template backdrop, no full-width/opaque scrim, no extra close button; Android Back closes it). Header right order follows the template: notifications (IconButton + Badge + solar bell) -> theme toggle (template Settings slot) -> account; the park scope is the template WorkspacesPopover trigger in the header left slot (`layouts/components/workspaces-button.tsx`).

- **Page rhythm / raw codes / phone FAB / blur / preflight (R3OPS).** Page root = `screen on` grid, never a fragment starting with PageHeader (r2 audit `rhythm|header-gap`, which also sees through a header wrapped alone in a div: `header-gap-wrapped`). No snake_case backend code in a Label/Chip (`rhythm|raw-code-label`; humanizeEnum/optionLabel). Shell keeps <=620px bottom clearance for the floating Ask Mesha bubble (`phone-fab-clearance`). No backdrop blur on scrims/veils (`no-blur-scrim`). Template pseudo elements that set `borderWidth` also set `borderStyle` (Tailwind preflight; `preflight-pseudo-border`).
- **Phone list rows stack (R3OPS-2).** Below sm drop secondary columns (header + cell class) and repeat assignee/status in the first cell; never leave a column clipped at the card edge (guard `tasks-phone-stacked-row`).
- **Overlay journeys: required triggers, no scroll jump (R3OPS-3).** Always-present overlay triggers are `required: true` in `scripts/lib/overlay-journeys.mjs` (stale selector = failure, not `overlay_skip`); opening any overlay must leave window scroll unchanged (`overlay-no-scroll-jump`); LocalOverlayLink drawer triggers carry `aria-haspopup="dialog"`.
- **No letter-stacked text (R3OPS-3).** r2 audit P0 `text|letter-stack`: a text leaf under 2.2em wide and over 3 lines tall fails. Tables use Scrollbar + `minWidth` + nowrap identity cells; phones get stacked rows.
- **Toolbars and pagers (R3OPS-3).** r2 audit P0 `controls|label-doubled`, `controls|control-overlap`, `controls|pager-clipped`. One label per field, no overlapping controls, arrows inside the card; phone search owns its row.
- **Early-return screens stream too (R3OPS-3).** Header/tabs render without await; the read sits in an async panel inside `UrlSuspense` (`toxin-panel-suspense`). Drawer filters and downloads seed from the page park (`video-log-park-seed`).
- **Toolbars outside the keyed panel (R3OPS-3).** Toolbar row before `UrlSuspense`, rows + pager inside; cross-boundary state via a per-card context (`leave-toolbar-outside-panel`, `routines-toolbar-outside-panel`).
- **Caveats are info tooltips (R3OPS-3).** `InfoTip` (controlled Tooltip, 44px `eva:info-outline` IconButton, tap-to-open, text as aria-label) beside the control (`action-center-paging-tooltip`, `info-tip-tap`).

## Proven performance patterns (from main + #415)

Fix catalog PP-1..PP-21 (bad/good snippet, source commit, enforcing guard or
"review-only"): [`docs/decisions/scale-anti-patterns.md` → "Proven performance
patterns (from main + #415)"](../../../docs/decisions/scale-anti-patterns.md).
Machine gates added 2026-09-25: `make scale-guard` rules `count-distinct-sort`,
`cte-self-join`, `hand-rolled-read-cache`, `non-sargable-cast` (now `::text IN`),
and `make admin-web-heavy-client-imports-guard`. Baselines only shrink.
Apply the review-only rows (PP-7..PP-21) by hand when reviewing a hot read.

## Page ↔ template map (guard: page-template-map)

- Every admin-web route has a row in `docs/design/page-template-map.md`: route → template page → the
  template section components per block, and the feature files that must import them. Build pages by
  composing those sections (copied verbatim into `components/minimal/`), fed our data and labels.
- Anti-pattern: hand-made KPI boxes / pastel `AnalyticsWidgetSummary` tint cards in dark, hand-built
  list cards instead of the template table anatomy (Card, Tabs with Label counts, toolbar,
  TableHeadCustom, TablePaginationCustom/Links). Use EcommerceWidgetSummary/CourseWidgetSummary/BankingWidgetSummary.
- Review check: a changed page that drops a mapped template import, or a new page with no map row, is a
  blocker; `design:guard` (`page-template-map`, p0) enforces the listed imports.
- **Server-rendered sections need 'use client' for function sx (guard `section-server-fn-sx`, p0).** A template section under `components/minimal/sections/` is rendered straight from Server Component pages; if it styles with `sx={(theme) => …}` / `sx={[(theme) => …]}` it must start with `'use client'`, or the function crosses the server/client boundary and the page throws "Functions cannot be passed directly to Client Components" (whole route falls back to client rendering or 500s).
- **Progress bars take their length from the column, never a px floor (guard `progress-bar-px-min-width`, p0).** A template progress row (EcommerceSalesOverview `LinearProgress`, height 8, grey-500 16% track) fills its row or table column; size the column (`TableCell sx={{ width: "32%" }}`) and let the bar fill it. A raw `minWidth: 80` on the bar overflows narrow cells.
- **A chart colour is a CSS colour, never a palette key (audit `chart-black`, P0).** `chart.colors` on a template chart takes resolved colours (`var(--palette-primary-main)`, or the section default); a palette KEY such as `seriesColorVar(i)` ("primary", "info.light") never resolves in Apex and paints the series and legend dots black. Omit `colors` to keep the template default pair.
- **Retired legacy CSS stays deleted (test `legacy-card-css-deleted`).** When pages stop rendering a legacy class, delete its rules from frame.css / mesha-theme.css / minimal-theme.css; `.wchart`, `.wtable`, `.g2`-`.g6` and `.wb-dialog .it .car` are gone and the test fails if a rule or a product className brings one back.

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
