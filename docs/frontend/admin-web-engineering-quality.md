# Admin-Web Engineering Quality Guardrails

Status: required engineering baseline for `apps/admin-web/**` and the shared
TypeScript packages it imports. Product/IA truth remains in
`context/frontend/**`, `apps/admin-web/AGENTS.md`, and the dashboard mock; this
document owns framework, runtime, testing, accessibility, and CI practice.

Shared operational read models must follow
`docs/architecture/operational-read-model-contract.md`. Admin-web renders
backend-owned facts for Calendar, Control Tower, Action Center, Protocol
Adherence, Workflows, and detail pages; it must not invent local grain semantics
or hide mismatched backend/mobile/reporting numbers with a screen-only rule.

## Source Policy

Use current primary documentation, then confirm the committed versions and
configuration in this repository. Context7 is a documentation index and search
accelerator, not a replacement for a framework's primary source.

Primary references:

- [Next.js Server and Client Components](https://nextjs.org/docs/app/getting-started/server-and-client-components)
- [Next.js data security](https://nextjs.org/docs/app/guides/data-security)
- [Next.js error handling](https://nextjs.org/docs/app/getting-started/error-handling)
- [React: Keeping Components Pure](https://react.dev/learn/keeping-components-pure)
- [React: You Might Not Need an Effect](https://react.dev/learn/you-might-not-need-an-effect)
- [TypeScript `strict`](https://www.typescriptlang.org/tsconfig/strict.html)
- [TypeScript `noUncheckedIndexedAccess`](https://www.typescriptlang.org/tsconfig/noUncheckedIndexedAccess.html)
- [Node.js test runner](https://nodejs.org/docs/latest-v24.x/api/test.html)
- [TanStack Query keys](https://tanstack.com/query/v5/docs/framework/react/guides/query-keys)
- [TanStack Query invalidation from mutations](https://tanstack.com/query/v5/docs/framework/react/guides/invalidations-from-mutations)
- [Playwright best practices](https://playwright.dev/docs/best-practices)
- [Playwright accessibility testing](https://playwright.dev/docs/accessibility-testing)
- [Playwright visual comparisons](https://playwright.dev/docs/test-snapshots)

When a framework is upgraded, query the matching current Context7 library and
re-open the primary pages above. Do not copy an unversioned example into product
code without comparing it to `apps/admin-web/package.json`, the lockfile, and the
framework release notes.

## Repository Stack (Inspected 2026-07-20)

| Layer | Committed implementation |
| --- | --- |
| Runtime/build | npm lockfile; Node 24 in GitHub CI; Next.js standalone output |
| Framework | Next.js 16 App Router; React and React DOM 19 |
| Language | TypeScript with `strict`, `noEmit`, `isolatedModules`, bundler resolution |
| Server data | Server Components and `server-only` adapters over `@goatos/api-client` |
| Client server-state | TanStack Query 5, only where a Client Component actually owns live server-state interaction |
| UI | Mock-derived CSS/design tokens, Base UI where applicable, Lucide icons |
| Static checks | ESLint 9 with Next core-web-vitals and TypeScript rules; `tsc --noEmit`; repo mock/IA/request-plan guards |
| Unit tests | Node built-in test runner and `node:assert` |
| Browser/visual | Playwright, axe-core, pixelmatch, PNG baselines, desktop and narrow viewports |

Version numbers are inventory, not an instruction to upgrade opportunistically.
Dependency upgrades are deliberate, lockfile-backed changes with the full gate.

## Server-First Next.js Contract

1. Pages and layouts remain Server Components by default. Add `'use client'`
   only at the smallest interactive leaf that needs state, event handlers,
   effects, or browser APIs. A client boundary pulls its import graph into the
   browser bundle.
2. Privileged reads stay in `server-only` modules under `lib/api/**` or another
   approved server adapter. Never import backend tokens, tenant credentials,
   database clients, or unrestricted DTOs into a Client Component.
3. Pass the smallest serializable view model across the server/client boundary.
   Do not pass entire backend records merely because the type is available.
4. Server Actions and Route Handlers are public request surfaces. Every action
   independently authenticates, authorizes, validates all form/query/header
   input, and sends the backend's stable idempotency key. A page-level auth check
   is not inherited by an action.
5. Rendering is pure. Never mutate, log out, write a cookie, revalidate, or send
   a command as a render-time side effect. Writes happen in an explicit action or
   handler; successful writes re-read or revalidate backend-owned truth.
6. Expected validation/business failures are typed return values rendered in
   the page. Uncaught failures use route-segment `error.tsx` boundaries with a
   visible retry path. Data routes preserve separate loading, empty-success,
   permission-denied, contract-unavailable, and unexpected-error states.
7. Do not add caching by habit. Any cache must name its scope (tenant, role,
   locale, park/date/filter), invalidation source, and stale-data behavior. A
   cache that can cross an authorization or tenant boundary is release-blocking.

These framework rules sit below the stronger Goat OS rule: the generated
backend contract owns business truth, RBAC, labels, options, and commands.

## React State And Effects

- Components and render helpers are pure: same props/state produce the same
  result, and render does not mutate values created outside the render.
- Derive display data during render. Do not use an Effect to copy props, query
  results, filters, totals, or status into a second state variable.
- Handle user actions in event handlers or form/Server Actions, not an Effect
  that guesses which event occurred.
- Use an Effect only to synchronize an external system. Every subscription,
  timer, observer, or request has cleanup/cancellation and remains correct when
  development Strict Mode mounts or invokes it again.
- React local state owns only transient UI state. Backend state remains in the
  Server Component response or TanStack Query cache; it is never mirrored into
  a second client-authoritative store.
- Forms expose a real label/name, pending state, disabled reason, success state,
  and field/global error state. Double-submit protection does not replace
  backend idempotency.

## TypeScript And Node

- `strict`, `noEmit`, and `isolatedModules` stay enabled. Do not weaken the
  project configuration or add broad `any`, `@ts-ignore`, non-null assertions,
  or `skip` comments to make a gate pass.
- Boundary data is `unknown` until validated or supplied by the generated API
  client. Do not hand-copy backend DTOs.
- Exhaustive unions use a `never` check; optional values are handled explicitly.
- `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes` are the target
  hardening flags. Enable them only through a dedicated, fully green debt-removal
  change; do not silently switch them on inside unrelated feature work.
- One Node major must run local validation, GitHub CI, Docker build, and Docker
  runtime. `npm ci` and the committed lockfile are mandatory in CI and images.
- Unit tests use `node:test`, `node:assert/strict`, isolated fixtures, and mocked
  clocks for date/timer behavior. Tests must not depend on wall-clock sleeps,
  developer timezone, execution order, real credentials, or shared databases.

## TanStack Query

Server Components remain the default read path. When a Client Component needs
TanStack Query:

- every variable used by `queryFn` that changes the result (tenant/scope, role,
  park, selected date window, filters, cursor, locale) is present in the query
  key;
- keys come from a feature query-key factory rather than ad-hoc arrays scattered
  across components;
- queries are disabled until required identifiers exist instead of sending
  placeholder/fallback IDs;
- successful mutations await invalidation of every affected key, or install the
  exact backend response; terminal state is not invented optimistically;
- errors remain explicit UI states and are never converted to `[]`, `0`, or a
  success-looking empty card;
- request cancellation and component unmount do not publish stale responses into
  a newly selected scope/window.

## UI, Navigation, And Accessibility

Framework correctness does not prove a usable screen. For every changed route,
drawer, modal, table, form, and navigation surface:

- a same-page drawer/modal/popover must use `LocalOverlayLink` plus client-local
  open/closed state. When complete detail data is already loaded, pass it into
  the local boundary. When detail is server-only, open the shell immediately
  from list summary data and fetch only that detail through an authenticated
  Server Action/Route Handler inside the open drawer. Its real href
  must remain a refreshable deep link, but an ordinary click must not issue a
  Next.js document/RSC request, refetch the route, or trigger the global
  route-pending UI;
- opening such an overlay pushes a history/hash-backed entry; Back, Escape,
  outside click, and its close control close locally and restore trigger focus.
  Do not implement the scrim or close control as a Next `Link` merely to change
  a search parameter. `make admin-web-local-overlay-guard` enforces a zero
  baseline for route-driven overlay open/close controls across the whole feature
  tree and requires its adversarial self-test. Never add a legacy allowance;

- an overlay whose open state lives in the URL re-renders the page on every step,
  so a page-level entry animation replays underneath it and the overlay reads as
  janky rather than smooth. This bit the verifier review modal on 2026-08-06 —
  `.screen { animation: fade .25s ease }` replayed on every `vi_row` change, so
  opening the modal and each Prev/Next flashed the whole page. Suppress the page
  animation while an overlay is open, e.g.
  `.screen:has(.vr-modal-scrim.on) { animation: none; }`.
  `make overlay-motion-guard` enforces this and ships the regression itself as a
  self-test fixture. No unit test can see this class of bug: nothing throws, and
  the defect is the interaction between an entry animation and URL-held overlay
  state;

- compare rendered desktop and narrow screenshots to
  `mock/goatos-dashboard-mock.html`; verify alignment, spacing, card/table
  anatomy, wrapping, overflow, focus, hover, active, empty, loading, and error
  states;
- when a route/table/chip row/drawer/modal/popover/navigation path changes,
  reproduce the exact URL and viewport under review and inspect for right-edge
  column clipping, horizontal overflow, clipped chips, wrong selected nav item,
  wrong row-click destination, outside-click close failure, and close-button
  failure;
- use semantic HTML first (`button`, `a`, `label`, headings, lists, tables,
  dialogs). Do not attach click behavior to a `div`/`span` without the full
  keyboard and accessibility contract;
- every interactive control has an accessible name, visible keyboard focus, and
  a usable target; icon-only controls have an explicit label;
- modal focus is trapped, Escape/back closes only when allowed, initial focus is
  intentional, and closing restores focus to the trigger;
- color is not the only status signal; text/icon semantics and contrast survive
  every state and viewport;
- tables own horizontal overflow and stable column widths; values must not wrap
  character-by-character or escape cards;
- raw vaccination config/protocol tokens must never be sent as backend
  presentation copy or shown as visible UI copy.
  Strings such as `et_tt`, `et_tt_adult_w2`, `ppr_booster`, `blue_tongue_first`,
  `Preventive Care Vaccination Matrix`, and similar backend keys are valid
  inside backend config, raw storage/API contracts, non-UI tests, and dedicated
  display-mapping helpers only. Backend display fields (`driveName`,
  `vaccineLabel`, `vaccine_labels`, card titles/subtitles, alerts), admin-web
  pages, drawers, chips, tables, schedules, alerts, action rows, visual
  fixtures, and generated galleries must render human labels such as `ET+TT`,
  `PPR · Booster`, `Blue Tongue`, and `Goat Pox`. Run
  `make ui-vaccine-labels-guard`; it is wired into `make guardrails` and local
  CI.
- tables, chip groups, drawers, popovers, and side panels must be reviewed at the
  exact route/viewport being changed. Verify no right-edge clipping, no accidental
  whole-page horizontal scroll, no clipped status/actions column, working outside
  click/back close behavior, correct sidebar highlight, and scoped drilldowns that
  show only the records for the clicked row.
- navigation tests assert the user-visible route and selected state, including
  direct URL, refresh, Back/Forward, role-denied, and narrow viewport behavior.

Automated axe checks catch only part of accessibility. A frontend release still
requires keyboard review, visible-focus review, screen-width review, and human
inspection of the generated screenshots.

## Browser And Visual Test Rules

- Prefer Playwright role/label/text locators for interactions. CSS locators are
  allowed for pixel/layout anatomy assertions that have no user-facing semantic
  equivalent; XPath is not.
- Use Playwright web-first assertions and actionability waiting. Do not add
  fixed `waitForTimeout` sleeps for application state; wait for the visible
  result, URL, response, or stable semantic condition.
- Each E2E is isolated and proves user-visible behavior, not implementation
  details. Use a disposable database for mutating flows.
- Visual baselines are generated and compared in the same OS/browser/font
  environment. Update a baseline only after reviewing the diff; never accept a
  broad baseline rewrite to hide a regression.
- `assertA11y`/axe must run on every in-scope route and on open modal/drawer
  states, not only the initial page shell.
- Failure evidence includes screenshot, diff image, URL/role/scope, console and
  network errors, and a Playwright trace for flaky/failed flows.

## Required Gate

Fast/static gate for every admin-web change:

```bash
npm --prefix apps/admin-web ci
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run test
npm --prefix apps/admin-web run check:mock-fidelity
GOATOS_BEARER_TOKEN=sentinel-mesha-admin-token npm --prefix apps/admin-web run build
```

Rendered gate whenever route/UI/CSS/navigation/error/loading behavior changes:

```bash
npm --prefix apps/admin-web run smoke:visual:live
```

Use the seeded isolated backend/database required by the touched flow. Open the
screenshots and diffs; a command exit code alone is not visual review.

When a user reports or asks to fix a frontend/UI defect, the acceptance check is
the rendered browser flow, not the code diff. Recreate the reported URL,
viewport, scope, filters, drawer/modal/popover state, and click path. Verify the
screen in the same user lens: active sidebar item, right-edge/status columns,
chip text, table overflow, drawer outside-click/back/X close, drilldown
destination, and scoped records. If this cannot be rendered locally, the handoff
must say that explicitly; otherwise do not claim the frontend issue is fixed.

## Static/CI Recurrence Checks

The shared guard and CI owners should keep the following executable:

1. Node parity: package/CI/Docker build/Docker runtime use one pinned major.
2. TypeScript baseline: `strict`, `noEmit`, and `isolatedModules` cannot regress.
3. Server boundary: Client Components cannot import `server-only` adapters,
   unprefixed secrets, database SDKs, or raw backend clients.
4. Action boundary: new Server Actions have input validation, authorization
   delegation, and stable idempotency coverage.
5. Error-state coverage: every data-bearing admin route is covered by an
   intentional visible boundary; backend failure cannot become empty success.
6. Test discovery: the Node test command includes `features/**`, `lib/**`, and
   `scripts/**` tests, so a test file cannot exist outside the CI glob.
7. Browser stability: changed Playwright flows cannot introduce fixed sleeps,
   XPath selectors, or manual non-retrying visibility assertions.
8. Visual/a11y evidence: UI changes run the live route smoke with axe, desktop
   and narrow screenshots, and reviewed pixel diffs.

Every new static guard needs an adversarial self-test and registration in the
shared guardrail manifest plus local/hosted CI. A prose rule without an executed
failing fixture is guidance, not recurrence protection.

## Known Parity Gaps At Inspection

These are not waivers:

- GitHub admin-web jobs use Node 24 while `apps/admin-web/Dockerfile` builds and
  runs Node 22. Align Docker to the pinned major and guard the invariant.
- The npm unit-test glob covers `features/**/*.test.mjs` but not tests under
  `lib/**` or `scripts/**`; expand discovery and prove those files execute.
- The App Router has loading UI but no committed `error.tsx`/`global-error.tsx`
  boundary. Add a visible admin error boundary and an E2E that forces an API
  failure without rendering a false empty-success state.
- Existing browser scripts contain fixed sleeps. Replace them incrementally with
  web-first conditions and block new fixed sleeps immediately.
