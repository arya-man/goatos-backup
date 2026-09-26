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
  already inside `AnimatedTabs`, `SegmentTabs`, `SegmentedLinks`): `router.push(href,{scroll:false})`
  in a transition, the pressed tab selected at once, the header / crumbs / tabs / filters and the old
  panel kept on screen (dimmed at most) until the new page is ready. Never hide the live panel or
  swap the page for a skeleton on a same-route param change; `loading.tsx` is for the first entry.
- A restyle never introduces new UI behaviour; never reopen an item in `docs/design/redesign-regression-guard.md`.
- Mobile webview: tap targets >= `var(--tap-min)` (44px), fixed overlays through kit `BodyPortal` /
  `Sheet` / `Dialog`, `100dvh`, no sideways page scroll.
- Legacy debt is a shrink-only per-file ratchet in `design-system-waivers.json`; never raise it.
- Production bug CLASSES are automated guards. `scripts/lib/visual-pattern-guards.mjs` (route visual
  lane) adds `P-text-icon-overlap`, `P-wide-table-no-wrapper`, `P-chart-axis-tiny` (<11px),
  `P-pinned-bar-blur-flicker`, `P-drawer-filter-mismatch` and `P-chart-hover-remount`. `raw-chart-lib`
  refuses recharts/d3/chart.js/nivo/victory/visx/echarts/highcharts — charts are Apex (via
  `components/minimal/chart` or `components/kit`) or the two inline helpers (`svg-bars`, `svg-series`).
  Full pattern → guard table: `docs/design/README.md` §5b.
- MUI Minimal template is the reference. Every non-dynamic route in `scripts/smoke-visual-live.mjs`
  matches a `route_prefixes` entry in `docs/design/route-template-map.json`; a NEW page must add its
  area in the same change or fail `route-template-map-missing`. Template lives at
  `~/mesha/mui/Minimal_TypeScript_v7.7.0` (Ravi laptop; licensed, NOT in the repo). Palette is
  locked to Mesha green; template gives structure, density, motion and interaction patterns only.

## Proven performance patterns (from main + #415)

Fix catalog PP-1..PP-21 (bad/good snippet, source commit, enforcing guard or
"review-only"): [`docs/decisions/scale-anti-patterns.md` → "Proven performance
patterns (from main + #415)"](../../../docs/decisions/scale-anti-patterns.md).
Machine gates added 2026-09-25: `make scale-guard` rules `count-distinct-sort`,
`cte-self-join`, `hand-rolled-read-cache`, `non-sargable-cast` (now `::text IN`),
and `make admin-web-heavy-client-imports-guard`. Baselines only shrink.
Apply the review-only rows (PP-7..PP-21) by hand when reviewing a hot read.
