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
  content streams in when ready (the skeleton commits with the click itself, no timer: a 30ms delay starved behind the router transition and left the old panel on screen, J3 P0-1, guard `url-panel-no-timer`). Never keep
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
- **Wide matrices never crush their column headers (test `notification-matrix-header-floor`).** A table with one column per designation/role/day gives each column a readable floor (`Table sx={{ minWidth: fixed + floor * n }}`, `<Box component="col" sx={{ width: floor }}>`), scrolls sideways inside its card with the label column sticky, and keeps headers on one line (`nowrap` + ellipsis + Tooltip with the full label). Never shrink header text (10px) or let it wrap letter by letter to fit.
- **Third-party grids are shielded from the legacy table rules (test `calendar-legacy-table-reset`).** The shell's legacy `.wrap/.screen/.main th|td` rules also hit raw `<th>/<td>` inside FullCalendar and similar widgets (grey band, 48px+ rows, padding). Reset them at higher specificity inside the widget root (`CalendarRoot` `legacyTableReset`), never by adding rules to the legacy CSS files. The month grid is `height="auto"` at md+ (no inner scroller clipping the last week); calendar titles use `lib/format` DD/MM/YYYY.
- **File pickers are the template Upload area (test `config-drawer-upload-template`).** A feature never renders the browser's bare `<input type="file">` next to a legacy `.btn`: use `UploadFile` (`components/minimal/upload`, template UploadDefault anatomy, the native input hidden inside so refs/forms read it as before) and a kit `Button` for the action. Required form fields pass `required` and let MUI add the one asterisk (never a hand-typed `" *"`).
- **Sidebar and header are the template dashboard layout (guard `shell-nav-template`, p0; test `components/sidebar-viewport.test.mjs`).** The nav is `NavSectionVertical` / `NavSectionMini` (layouts/template/nav-section, verbatim) inside the template-derived `layouts/app/dashboard/nav-vertical.tsx` / `nav-mobile.tsx`, fed by the backend bootstrap nav. The whole nav scrolls in the template `Scrollbar`, logo fixed; only the active group opens (no `default_open` subtrees); no custom nav footer (`navBottom`, `msh-foot`, `navigation.footer` - the template only has the optional NavUpgrade card, which we do not use); the phone nav is the template drawer (`var(--layout-nav-mobile-width)` over the template backdrop, no full-width/opaque scrim, no extra close button; Android Back closes it). Header right order follows the template: notifications (IconButton + Badge + solar bell) -> theme toggle (template Settings slot) -> account; the park scope is the template-derived WorkspacesPopover (trigger and list, options are scope links) in the header left slot (`layouts/app/components/workspaces-popover.tsx`).
- **Park switcher (test `components/park-switcher.test.mjs`, `lib/scope.test.mjs`; guard `template-derived-anatomy`).** The header WorkspacesPopover option name wraps (declared override, never `noWrap` in the 240px list) and shows the park code the shell passes (`code`, skipped when it equals the name). A `?park=<id>` outside the shell's park list reads the `scope.selected_park` copy (the `fallback` prop, `pickScopeOption`) and marks no option selected; it never reads or selects "All parks". The list is 280 wide with a 360 scroll cap (declared), so one long park plus All parks show whole; the code caption strip pins its exact Box line and sx (self-test `derived-slot-cond-edit`). Styles passed through slotProps (`slotProps.paper.sx`, any `slotProps.*.sx`) are anatomy too (manifest `slotSx`, self-test `derived-slotsx-width`): a paper width change needs a declared strip.
- **One CTA per destination (backend test `TestVaccinationBoardOpenCTAsAreDistinct`).** Two buttons with the same copy and href (the Action Center empty state once showed "Open the vaccination plan" twice) collapse to one; a page contract never publishes two `action.open_*` keys that read the same.

- **Page rhythm / raw codes / Ask Mesha dock / blur / preflight (R3OPS, TR1).** Page root = `screen on` grid, never a fragment starting with PageHeader (r2 audit `rhythm|header-gap`, which also sees through a header wrapped alone in a div: `header-gap-wrapped`). No snake_case backend code in a Label/Chip (`rhythm|raw-code-label`; humanizeEnum/optionLabel). Ask Mesha docks in the header at every width as a template IconButton, never a floating bubble, and the header slot is tracked live (`ask-mesha-docked`). A page with no nav leaf opens and highlights its module group (`nav-module-fallback`). The phone 44px floor covers Chip / ToggleButton / date-range clear width; the r2 tap check skips only visually hidden `.sr-only` controls (`tap-skip-visually-hidden`); anchored template popovers are not dialogs for the backdrop rule (`popover-not-dialog`); no hand-made `.modal` shell, dialogs are MUI Dialog (`action-center-filters-dialog`). The header park switcher shows on every park-scoped route and hides only on routes tagged own-filter / no-park in the one list in mesha-shell.tsx (`top-bar-park-rule`). No backdrop blur on scrims/veils (`no-blur-scrim`). Template pseudo elements that set `borderWidth` also set `borderStyle` (Tailwind preflight; `preflight-pseudo-border`).
- **Phone list rows stack (R3OPS-2).** Below sm drop secondary columns (header + cell class) and repeat assignee/status in the first cell; never leave a column clipped at the card edge (guard `tasks-phone-stacked-row`).
- **Overlay journeys: required triggers, no scroll jump (R3OPS-3).** Always-present overlay triggers are `required: true` in `scripts/lib/overlay-journeys.mjs` (stale selector = failure, not `overlay_skip`); opening any overlay must leave window scroll unchanged (`overlay-no-scroll-jump`); LocalOverlayLink drawer triggers carry `aria-haspopup="dialog"`.
- **No letter-stacked text (R3OPS-3).** r2 audit P0 `text|letter-stack`: a text leaf under 2.2em wide and over 3 lines tall fails. Tables use Scrollbar + `minWidth` + nowrap identity cells; phones get stacked rows.
- **Toolbars and pagers (R3OPS-3).** r2 audit P0 `controls|label-doubled`, `controls|control-overlap`, `controls|pager-clipped`. One label per field, no overlapping controls, arrows inside the card; phone search owns its row.
- **Early-return screens stream too (R3OPS-3).** Header/tabs render without await; the read sits in an async panel inside `UrlSuspense` (`toxin-panel-suspense`). Drawer filters and downloads seed from the page park (`video-log-park-seed`).
- **Toolbars outside the keyed panel (R3OPS-3).** Toolbar row before `UrlSuspense`, rows + pager inside; cross-boundary state via a per-card context (`leave-toolbar-outside-panel`, `routines-toolbar-outside-panel`).
- **Drag must work on touch; never native HTML5 drag (guard `task-board-touch-dnd`).** Touch browsers never fire `dragstart`/`drop`, and the HTML5 drag image of a background-less `<a>` is a transparent ghost. Drag with `@dnd-kit/core` (Mouse distance 5, Touch delay 200 / tolerance 5, Keyboard) and a portalled `DragOverlay` that renders the same card on `background.paper` with the template lift. An e2e that drags must hold and abort the write, never let it reach a shared API.
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
- **Converted files stay legacy-free (FIXJ3, J1 P0-1/P0-2/P1-1..3/P1-5; guard `legacy-free-zone`, p0, `apps/admin-web/scripts/lib/legacy-free-zones.mjs` + `scripts/legacy-free-zones.test.mjs`).** A file (or directory prefix) listed in `apps/admin-web/scripts/legacy-free-zones.json` may not use a className that any legacy stylesheet or CSS module defines (`card`/`hd`/`bd`/`btn`/`qcard`/`hrow`/`muted`...), a `style={...}` prop, a native `<button>/<input>/<select>/<textarea>/<table>/<tr>/<td>/<th>` (a hidden `<input type="file">` inside a template upload Button and an `<input type="hidden">` form field are fine: neither draws anything), a `lucide-react` icon, a `.css` import, a hex/rgb colour literal, or an embedded stylesheet (`<style>` element or string `GlobalStyles`; a feature stylesheet moved into a TSX string is still a feature stylesheet). Build with template sections / MUI (Card + CardHeader, Button, IconButton, TextField select/multiline, Table + TableHeadCustom), template Iconify icons and theme sx. When you convert a file, add it to the zones list in the same commit and delete the legacy selectors it stopped using once `grep -rn` proves no other file uses them. Zones only grow.
- **Shared adapters are thin template wrappers (FIXJ4, J1 P1-4; guard `legacy-free-zone` on the adapter files listed in `apps/admin-web/scripts/legacy-free-zones.json`, plus the `legacy-class-use` / `inline-style-prop` / `lucide-import` ratchets).** `components/ui-primitives` (Tag = template Label, ClipText = Box sx), `components/data-table` (IdentityCell = template user-list name cell, actions cell sx, `visuallyHidden`), `components/themed-date-picker` (the MUI X DatePicker, DD/MM/YYYY, ISO hidden input; no `<details>` + `.move-date-*`, no CSS module), `components/review-queue/review-queue-ui` (StatusChip = soft Label), the shell-unavailable / route-error screens (`components/app/state-panel.tsx`, never `.kit-state*`) render only MUI / template parts with theme sx and keep their public props, so callers do not move. A `components/app` or `components/*` adapter never renders a legacy stylesheet class, a CSS module, `style={}` or a lucide icon; the legacy rules they used are deleted with them (`npm run design:guard:update-baseline` in the same commit).
- **Icons are template Iconify only; lucide-react is gone (FIXJ4, J1 P1-3; guard `lucide-banned`, design:guard p0 with self-test: any `lucide-react` import or a `lucide-react` entry in apps/admin-web/package.json fails, and the finding names the replacement).** The ONE lucide -> Iconify mapping table is `apps/admin-web/scripts/lib/lucide-iconify-map.mjs` (template names; spinners are MUI `CircularProgress`). `layouts/template/iconify/icon-sets.ts` is verbatim template code (sha256-pinned), so an icon the template set lacks goes in the extra offline registry `apps/admin-web/components/app/iconify-extra.ts` (exact Iconify JSON body, solar first) and renders through `AppIcon` (`components/app/app-icon.tsx`: template names fall through to `Iconify`, extra names are registered offline, no network fetch); guard `iconify-offline-set` accepts names from both files and nothing else. Table pagers are `TablePaginationLinks` (`data-pager` hook for the smoke / webview lanes; 44px phone taps come from the shell's PhoneTapStyles, never a pager-specific rule), never the legacy `.pager2` class; SegmentTabs / LocalViewToggle never carry `.metricseg`; the URL-nav event is `URL_NAV_EVENT` (`url-nav:navigate`).
- **URL panels keep the page still (FIXJ4, /operations/audit P0s: tab moved 108px, actor filter scrolled 562px, strip / operator links reloaded the document; guards `url-panel-click-after-react` + `url-panel-holds-page-height` in components/app/url-panel.test.mjs, `audit-strip-no-simplebar` in features/operations-audit/audit-analytics.test.mjs, runtime r2 interact full-reload / scroll-jump / fallback-jump).** UrlPanel hears link clicks and GET submits on `window` in the bubble phase, AFTER React dispatched them: a capture listener swapped the panel to its skeleton first, unmounting a link inside the panel before next/link saw the click, and the browser followed the href as a full reload. From the click until every pending panel shows content again, a scrolled page keeps its height (`document.body` min-height), so a swap never clamps the page to the top; the floor lifts after, and a shorter result only clamps to its new bottom. A block that sits ABOVE a URL-keyed tab strip and is itself re-keyed never mounts the template Scrollbar (SimpleBar is 0px tall for its first client frame); it scrolls sideways in a plain overflow box. A toolbar search narrower than its hint below sm passes `SearchTextField phonePlaceholder` (the contract's `filter.search_label`), never a clipped placeholder (r2 text-fit|placeholder-clipped). E2E scripts drive the template UI through roles, labels and `data-testid` hooks, never a legacy class the template parts no longer render (guard `sop-builder-e2e-selectors`, apps/admin-web/scripts/sop-builder-e2e-selectors.test.mjs); `PagedRows headCells` renders the template TableHeadCustom (the /sales loadwise register), header cells aligned with their body cells.
- **Local CI is strict: every push, every branch, no silent skip (FIXJ-CI, Ravi 2026-09-30 "enforce local CI/CD strictly"; J1 P0-4 + CI gaps; guards `push-hook-freshness` (lanes + feature-branch execution probe, self-test `tools/ci/check-push-hook-freshness.test.sh`) and the skip ledger).** The shared `<git-common-dir>/hooks/pre-push` is the thin shim `tools/agent-hooks/pre-push.shim` (`make push-hooks-install` / `make ai-setup`, FIXJ8, J1B P0-2): on every push it runs the PUSHING worktree's own committed `tools/agent-hooks/pre-push.hook` (legacy stg/main guard for a branch without one), so each branch enforces its own rules and no branch's install downgrades another. On EVERY push to ANY branch whose commits change an admin-web input (`apps/admin-web/**`, `docs/design/**`, package files, `tools/ci/admin-web-*`, `backend/internal/adminui/**`) it runs `tools/ci/admin-web-push-gate.sh`: design:guard, typecheck, npm test, next build and the visual gate (shell + touched routes, skeleton-on-touched). A missing guard or gate script FAILS the push. The lanes judge the work tree, so push from the worktree whose HEAD is the pushed commit with clean admin-web inputs. Run the lanes ahead of the push, one command each (the watchdog kills a command after 10 min): `tools/ci/admin-web-push-gate.sh --run design-guard|typecheck|unit-tests|next-build|visual-gate`; the push then reuses every PASS for the same admin-web input tree. Every push writes a gate receipt (`tools/ci/admin-web-push-receipt.mjs`: lanes, pass / reused / skip + reason, SHA, covered commits) and posts it on the PR as the `goatos/push-gate` status (red with the reason when a lane was skipped); `make land-main` REFUSES a range with a commit no receipt covers (fix: `tools/ci/admin-web-push-gate.sh --certify origin/main`). `tools/ci/check-push-hook-freshness.sh` (run-local-ci common) fails when the installed pre-push is not the shim, the shim does not resolve this worktree's hook, or its real test pushes are not refused (main without a receipt, admin-web feature push) or the legacy fallback is broken; a lane missing also fails. Never `git push --no-verify`.
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
- **Storybook renders on the theme stack, builds on every push that can break it, and uses no legacy class (FIXJ7, J1B P0-1; guards `legacy-css-ceiling` "imported nowhere" (every .css / .ts / .tsx / .mjs / .html in the app, `.storybook/` and `stories/` included, self-test), `legacy-css-rules` over `.storybook/` + `stories/` stylesheets (preview.css ceiling 0), design:guard `legacy-class-use` over stories + `.storybook` (self-test `stories/Bad.stories.tsx`), push-gate lane `storybook-build`).** `.storybook/preview.css` only loads fonts (`./fonts.css`, `theme/fonts.css`) and the verbatim chart styles; the canvas, type and palette come from `AppThemeStack` in preview.tsx. A story is built from MUI / template parts (Chip, Label, TextField, Table, PageRoot, theme sx), never a legacy class (`chip`, `tag`, `fld`, `tbl`, `main`, `kit-page`, `small`, `muted`...): the legacy stylesheets are gone, so such a story renders unstyled. `tools/ci/admin-web-push-gate.sh` runs `storybook-build` (`npm run build-storybook`, build slot) whenever the pushed commits touch `stories/`, `.storybook/`, `components/`, `theme/`, the app package.json or the lockfile (`--run storybook-build` always builds); FIXJ6's deletion broke `storybook build` and no push lane noticed. The legacy class list is FROZEN in `apps/admin-web/scripts/legacy-class-denylist.json` (every class the legacy stylesheets defined at 7e181ce32, J1B P2-2), read by `legacy-free-zone` and `legacy-class-use` through `scripts/lib/legacy-class-denylist.mjs`; its `hooks` (msh-side, lt-* / ltd-* tasks hooks, vr-* verification hooks) are JS / sx / test hooks with no stylesheet and the only exemptions. Never shrink the list; add a hook only with its reason.
- **Ask Mesha is the template chat app (FIXJ9, J1B P1-2; guard `ask-mesha-template-chat`, `apps/admin-web/features/ceo-ai/ceo-ai-template-chat.test.mjs`, plus design:guard `template-derived-anatomy` / `template-verbatim` and `legacy-free-zone` on `features/ceo-ai/` and `components/app/sections/chat/`).** The assistant panel composes Minimal `sections/chat`: `ChatLayout` (verbatim, `components/minimal/sections/chat/layout.tsx`, with `styles.tsx` and `hooks/use-collapse-nav.ts`) and the template-derived `ChatNav` / `ChatNavItem` (chat history), `ChatHeaderDetails` (assistant identity, window controls in `actions`, Rename in `menuActions`, Delete via `onDelete`), `ChatMessageList` / `ChatMessageItem` (one item per turn; an answer carries its steps, live watch card, markdown, chart, sources and the Copy action) and `ChatMessageInput` (attach, voice, send / stop) in `components/app/sections/chat/`, each declared in `docs/design/template-derived.json`. Answers render on the verbatim template `MarkdownRoot` (`components/minimal/markdown`), never the template `<Markdown>` (it runs rehype-raw on model output). `ceo-ai-styles.tsx` is deleted: no file in `features/ceo-ai` exports `*Sx` constants (sx-as-stylesheet) and the panel uses no `ButtonBase` / `InputBase`; renames and deletes are template Dialog / ConfirmDialog, attachments are Chips, the watch card is Label + small Table. The window stays below the app top bar on laptops (normal 960x720, max fills below the bar, min is the 72px header bar); on phones it is a full-screen sheet and the chats open in the template drawer behind a visible scrim.
- **FIXJ11 judge fixes (J3B on c4bd347ff; each has its guard).** (1) Phone tables keep their identity and action columns: below the theme `sm` breakpoint the first cell of EVERY page-content table, and the last cell when it is the row's action column (`[data-row-menu]` ⋮ or a bare IconButton; a pinned data column beside a wide identity column covered the whole 358px scroller), is sticky (left 0 / right 0, paper body, neutral head, above the scrolling cells; a `colSpan` cell never pins) through ONE AppBaseline rule, `phoneStickyEdgeCells` (components/app/table/sticky-first-column.ts), for PagedRows, DataTable, DenseTable and raw tables alike, inside `:where()` (zero specificity, so a cell sx still wins: a cell that sets `position` itself must be `sticky` below sm, as the /vaccination pen table's checkbox / ⋮ cells are). Never put it in a component `sx`: emotion prefixes the class onto `:where(&…)` and the rule never matches. A table that pins its own columns opts out with `data-sticky-edges="off"` (guard `sticky-edges-phone` in sticky-first-column.test.mjs; runtime r2 plugin `scripts/r2-audit-checks/table-scroll.mjs`, P0 at 390: `table-scroll|sticky-identity` after scrollLeft 160 the first and last body cells keep their edge, paint opaque and win elementFromPoint; `table-scroll|table-scroll-trap` a table's sideways scroller never also scrolls vertically). (2) A pager tap at 390 keeps the pager under the thumb: the URL-panel fallback is the SAME layout the rows render at that width (a phone card list is twinned by `StackedRowsSkeleton`, the desktop table by `TableSkeleton hideBelow="sm"`, template user rows by `TableSkeleton lead="avatar"`) and includes the pager (`PagerSkeleton`) when the page has one; both pagers (TablePaginationLinks, TableFooter) remember where the tap landed and scroll the page by the difference once the next page has laid out (`components/app/table/pager-anchor.ts`, test `pager-under-thumb`). r2 interact taps the Next arrow of up to two pagers per route at 390 (pager mid-screen, fresh load, outside the interaction cap) and fails P0 `interact|Pager|pager-jump` when the pager moves > 40px at any 100ms sample or once settled (page scroll while the pager is swapped out) and `interact|Pager|pager-shape` when the click-time skeleton's IoU with the landed rows is < 0.8 (`pagerFails` self-test). (3) A tab strip inside a card never swaps a panel outside that card: URL panel keys read the top-bar scope as lib/scope reads it (`scope_mode` absent = company, or park with a park set; `park=all` = none), so a link that spells the default out (`scopeHref` always writes `scope_mode=company`) is the same panel (test `scope-default-equivalent` in url-tab-nav.test.mjs); r2 fails P0 `interact|*|fallback-outside` when a `[data-url-panel]` outside the pressed tab's `.MuiCard-root` goes pending (`outsidePanelFails` self-test). (4) Pager copy: a range noun the contract serves plural is never doubled (`pagerNoun`, test `pager-noun-plural`; "1–10 Workflowss"), and a worklist that pages by pen counts pens in its range (`WorklistPager pageUnits`, /feed/direction + /feed/packing, test `pager-range-units`). (5) A visually hidden label in sx is `visuallyHidden` from `@mui/utils`, never a hand-rolled `{ height: 1, width: 1, margin: -1, clip }` (sx reads 1 as 100% and -1 as a spacing step: a full-cell absolute box that made the /vaccination/care-coverage table scroller trap 19px of vertical swipe; test `visually-hidden-sx`); a table's sideways scroller is never also a vertical window below md (/routines `maxHeight: { md: "62vh" }`), and a list that stacks its rows on phones opts out of AppBaseline's 540px table floor (`"&&": { minWidth: { xs: 0, sm: … } }`, /approvals), both caught by r2 `table-scroll`. (6) A data-sized KPI deck never leaves dead slots: KpiGrid widens a short last row to fill it (`kpiTileSize`, test `kpi-short-row-fills`; /counts/breakdown 3 + 3 + 1), and a deck's URL-panel twin draws the deck on screen (`BreakdownKpiSkeleton count`), so a filter click does not move the filters by a KPI row.
- **No Tailwind, no shadcn: global styles are the MUI theme (FIXJ7, J1B P1-1; guard `tailwind-banned`, design:guard p0 with self-test, `apps/admin-web/scripts/lib/tailwind-banned.mjs`; `legacy-css-ceiling` "globals.css stays deleted").** `app/globals.css`, `tailwind.config.ts`, `postcss.config.mjs`, `components.json` and `lib/utils.ts` (`cn`) are deleted, and `tailwindcss`, `@tailwindcss/postcss`, `@tailwindcss/vite`, `tailwind-merge`, `shadcn`, `tw-animate-css`, `clsx` left package.json. The guard fails on `@import "tailwindcss"` / `@tailwind` / `@apply` / `@config` / `@source` in any stylesheet, a tailwind.config / components.json / Tailwind postcss or Vite plugin, an import or dependency of those packages, and `font-sans` / `antialiased` on `<html>`/`<body>`. App-wide element rules live in the theme: `theme/core/components/css-baseline.tsx` (`appCssBaseline`: the template's own global baseline, `ul` reset + `img` cap, and the thin native scrollbar from the palette), restated by `theme/with-settings/update-components.ts` because MUI's deep merge replaces a `styleOverrides` function; element-level WebView / body rules stay in `theme/app-baseline.tsx`. Never add a global stylesheet for a reset: a raw element that needs margins or bullets sets them in its sx.
- **Legacy-free by default, hooks that query live classes, drawer roles, template dates (FIXJ7, J1B P2-3/P2-4/P2-5; guards `legacy-free-zone` root zones + `exempt` (stale exemption fails, self-test in `scripts/legacy-free-zones.test.mjs`), `stale-hook-selectors` (`scripts/stale-hook-selectors.test.mjs`), design:guard `raw-dialog` drawer-tag exemption (self-test `features/drawer-role.tsx`), `template-date-fields` "no native date input anywhere").** `scripts/legacy-free-zones.json` zones `app/`, `components/`, `features/` and `layouts/` as a whole; `exempt` names what stays outside, each with a reason (the verbatim template, the root layouts' font / token / chart stylesheet imports, the four baselined style props, the hidden submitter button), and an exempt file that turns clean must leave the list in the same change. A DOM hook (`components/app/scroll-edges.tsx` `SCROLL_EDGE_SELECTOR`, the /tasks click-matrix smoke fingerprint and overlay probe) only queries classes the app still renders; open overlays are counted through the MUI Dialog / Drawer / Popover parts, never `.drawer.on` / `.lt-fgroup.open` / `.on`. `role="dialog"` on a template drawer's own opening tag (`<MinimalDrawer role="dialog">`) is the part's role, not a hand-rolled dialog, and never takes a `raw-dialog` allowance. No feature renders a native date / datetime / month / week input: a date in a table row is `ThemedDatePicker` with `form={id}` (posts the ISO day with the row's hidden edit form) and `size="small"`.
- **No parallel CSS-variable token system in components (FIXJ7, J1B P2-1; ratchet `css-var-token` in `apps/admin-web/scripts/lib/shrink-ratchets.mjs`, design:guard self-test `features/token-reads.tsx`, shrink-only per file, target 0).** A `var(--…)` read of a Mesha token (a key of `theme/mesha-tokens.ts` or a custom property of `app/minimal-tokens.css`: `--sp-*`, `--r-*`, `--tap-min`, `--brand`, `--info`, `--danger`, `--muted`, `--line`, ...) in TS/TSX outside `theme/` is counted per file and may only go down. Use the theme: spacing numbers in sx (`p: 1`, `gap: 1.5` = theme.spacing), `borderRadius: 1.5` (theme.shape x 1.5), palette keys on colour props (`color: "info.main"`, `borderColor: "divider"`, `bgcolor: "background.paper"`), `TAP_MIN` from `theme/tap-target.ts` for the 44px WebView floor, and where a STRING is needed (calc sizes, tone maps, borders, gradients, chart series) exactly what the theme itself emits: `calc(2 * var(--spacing))` (= `theme.spacing(2)`), `calc(1.5 * var(--shape-borderRadius))`, `var(--palette-primary-main)` (= `theme.vars.palette.primary.main`), never `var(--sp-2)` / `var(--r-lg)` / `var(--brand)`. The kit ratchet no longer counts a numeric `borderRadius` / `padding*` / `margin*` / `gap` as raw px: those numbers ARE the theme scale.
