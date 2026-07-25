# Frontend Review — admin-web (SSR-first Next.js)

Frontend lives in `apps/admin-web/` — SSR-first Next.js (App Router) with shared
`packages/`. Architecture: `context/frontend/final-frontend-mobile-backend-architecture.md`.
Scope lock: `context/frontend/current-admin-web-scope.md`. Contract law:
`context/frontend/admin-web-backend-ui-contract.md`. Repo rules:
`apps/admin-web/AGENTS.md`. Framework/runtime/testing baseline:
`docs/frontend/admin-web-engineering-quality.md`. (Do not hardcode framework
versions when reviewing — read `package.json`, the lockfile, and CI; versions
drift.)

> **Verify-against-source, not memory.** The concrete script names, contract
> field names, route names, and taxonomy below are anchors that may drift.
> Confirm each against the committed source it names — `apps/admin-web/package.json`
> for scripts, `apps/admin-web/scripts/` for what a guard actually checks,
> `context/frontend/*` for contract/IA law, `mock/goatos-dashboard-mock.html`
> for UI truth — at review time. Values shown are illustrative of the pattern.

## Layout

```
apps/admin-web/
  app/                 # App Router routes + layouts (route = file hierarchy)
  features/            # feature modules (control-tower, action-center, calendar,
                       #   protocol-adherence, preventive-care-vaccination,
                       #   vaccination-execution, procurement, counts,
                       #   operations-audit, config, sops, …)
  components/          # shared UI (shell, nav, drawers)
  lib/
    api/               # server.ts (SSR fetch + contract), mutation adapters
    auth/              # firebase-client, local-dev-token
  scripts/             # check-mock-fidelity.mjs, check-ia-guard.mjs,
                       #   check-ui-contract-literals.mjs, smoke-visual-live.mjs
packages/              # api-client (generated), ui, rbac, forms-dsl, …
```

## Golden rule — backend owns the contract, frontend renders

The admin-web (and operator mobile) are renderers, not product-truth owners. This
is the highest-frequency frontend finding. Backend OpenAPI/app contracts must own:
navigation, route availability, page titles, section/table labels, filter/sort/
page-size semantics, chips/tabs, row-click params, drawer/action labels,
empty/error copy, disabled reasons, and summary-vs-detail field sets.

Frontend may own only: layout, CSS, responsive density, icon-token rendering,
focus/hover state, and local open/closed or selected-row state.

Business writes are commands, not frontend side effects. Any admin-web route that
adds/edits/imports/moves/closes business state must point to a backend command
registered in `context/architecture/domain-event-registry.json`; the downstream
event/outbox/consumer/E2E proof belongs to the backend contract. Frontend must
send an idempotency key where the contract requires one and render the backend
result/error; it must not schedule local follow-up work.

Check for:
- [ ] No hardcoded page titles / section labels / table headers / filter labels /
      chips / disabled reasons in JSX — they come from the page contract /
      the `/admin-web/bootstrap` contract (via `lib/api/server.ts`). Verify the
      exact contract type name against `context/frontend/admin-web-backend-ui-contract.md`
- [ ] Live/domain values (parks, sheds, breeds, SOP labels, operators, role scopes)
      come from backend contract compiled from Postgres — never hardcoded, never
      relabeled by runtime UI-config entries
- [ ] No frontend local default that an async config later replaces
- [ ] **No-local-fallback IA (merge-blocking):** if `/admin-web/bootstrap` or a
      page contract is unavailable, business UI blocks on an explicit
      contract-unavailable/error state; it must NOT ship a local fallback IA,
      product strings, nav, or seeded cards that take over when the contract is
      missing (shipping a local fallback IA is a blocking product-truth bug)
- [ ] Any temporary hardcoded exception is documented in `context/frontend/` before shipping
- [ ] **No fabricated success (merge-blocking):** a save/submit/confirm handler
      must invoke a real `@goatos/api-client` / `lib/api/*` method and render the
      server result. A handler that mutates local state and shows a success toast
      with no network round trip is a lie to the operator, even when its entry
      point is currently disabled. Concrete failure: `saveCap` in
      `apps/admin-web/features/people/vaccination-operators-screen.tsx:276-289`
      sets local state and toasts `Saved · cap set to N/day`; the `Edit` button at
      `:632-641` is correctly disabled-with-reason, but the Save/Cancel/input
      controls stay in the DOM and keyboard-reachable, so re-enabling `Edit` alone
      silently ships the lie. Disabled-with-reason covers the *entry point*, never
      the handler — when there is no endpoint, delete the write markup rather than
      leave a dormant fake. Test shape: assert the handler calls a network client
      method (same grep-style pattern as `positions-panel.test.mjs`).
- [ ] Backend owns canonical due/overdue/escalation/verification state; frontend renders it (no client-authoritative process state)
- [ ] Bootstrap/admin-ui-config/feature-flag changes keep Postgres canonical:
      `/admin-web/bootstrap` and page contracts expose `contract_revision`/ETag
      and family hashes for tenant/role/locale-sensitive families; config writes
      bump the relevant family revision and emit `config.changed`; Redis or
      in-process caches are revision-keyed acceleration only, never truth.

## Data access (bans)

- [ ] No direct BigQuery / Sheets / Firestore / GCS / operational-DB reads from the frontend
- [ ] All data through the generated `@goatos/api-client` or `lib/api/*` server adapters
- [ ] SSR data loaders fetch server-side; bearer token never reaches the browser bundle
- [ ] RBAC is server-enforced on every query/mutation; `packages/rbac` gating is UX-only, never an auth boundary

## Mock fidelity (mandatory gate)

The ONLY admin-web UI/UX source of truth is the mock
`mock/goatos-dashboard-mock.html`. PORT its structure — layout, table shapes,
empty states, icon system, spacing, density — not just its colors. Never reuse /
adapt / recolor old admin UI.

CRITICAL / HIGH frontend findings:
- [ ] Reuses old admin UI: `admin-primitives.tsx`, old cyan/slate palette,
      emoji icons, collapse-to-KPI layouts (verify the banned-palette hex set
      against the current `check-mock-fidelity.mjs` scan list — the list drifts)
- [ ] Hardcoded hex instead of theme tokens (`--brand`, `--panel`, `--line`,
      `--sidebar-2`, status tokens) — verify the token names against the theme file
- [ ] Emoji/dingbat glyphs in in-scope screens instead of SVG (lucide) icons
- [ ] A control that can't be backed yet is SIMPLIFIED instead of kept at the
      mock's visual richness and `disabled` with an `aria-disabled` reason
      (disable ≠ simplify)
- [ ] Missing data / a swallowed API failure rendered as a silent empty array
      instead of a visible error/alert band (see Error/Loading section — this is
      a merge-blocking product-truth bug, not a nit)
- [ ] Dead "X · Y" microcopy — every element should be a filter, clickable, clear
      info, or removed
- [ ] "mock" naming leaking into app code

### Required checks before push

Build is MANDATORY, not optional — a change that has not been built has not been
reviewed.

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run build
```

`check:mock-fidelity` composes three guards
(`check-ia-guard.mjs && check-ui-contract-literals.mjs && check-mock-fidelity.mjs`
— confirm the current chain in `apps/admin-web/package.json`). It scans for banned
emoji/hex/old imports and IA/contract-literal violations. It is a static scan, so:

- **A green `check:mock-fidelity` is NOT visual proof** and NOT a11y proof — it
  does not render pixels and does not run any accessibility assertion.
- **A green `build` is NOT proof the token-leak guard ran.** The SSR token-leak
  guard skips silently (early-returns exit 0) when its env token is unset
  (`GOATOS_BEARER_TOKEN` per VERIFIED FACTS; confirm the var and skip behavior in
  `apps/admin-web/scripts/check-token-leak.mjs`). If token-leak-sensitive code
  changed, confirm the guard actually EXECUTED with the token fixture present —
  do not treat a skipped guard as a pass.

For any frontend change, require rendered visual QA:

```bash
npm --prefix apps/admin-web run smoke:visual:live   # local backend + admin-web up
```

`smoke:visual:live` runs a real accessibility assertion (`assertA11y`, per
VERIFIED FACTS — confirm it is still called in
`apps/admin-web/scripts/smoke-visual-live.mjs`) that `check:mock-fidelity` does
NOT cover. A11y (WCAG-AA contrast + minimum target size) is only actually
exercised on this path, so a frontend UI change that changed contrast/target size
without running the live smoke has not had its a11y verified. Open the screenshots
under `.codex-goatos-render/admin-web-screenshots/` and compare against the mock
element-by-element (sidebar/nav alignment, spacing, card padding, table density,
label clipping, desktop/narrow responsive).

### Additional targeted guard/smoke scripts

Run when the touched surface matches (all confirmed real in
`apps/admin-web/package.json` per VERIFIED FACTS — verify a script still exists
before requiring it):

- `npm --prefix apps/admin-web run check:herd-import-security` — herd import /
  local file parsing / source-entry security work
- `npm --prefix apps/admin-web run check:goal1-frontend` — Goal 1 frontend coverage
- `npm --prefix apps/admin-web run smoke:vaccination-click-matrix:live` —
  vaccination matrix / interaction changes
- `npm --prefix apps/admin-web run e2e:sop-builder` — SOP builder flows

## IA / product taxonomy guardrails (from AGENTS.md — non-negotiable)

Taxonomy names below (verticals, modules, command lenses) are anchors — confirm
the current set against `apps/admin-web/AGENTS.md` and
`context/frontend/current-admin-web-scope.md` before treating an omission as a bug.

- [ ] Command lenses (Control Tower, Action Center, Calendar, Protocol Adherence,
      Workflows) are TOP-LEVEL only — never nested under a vertical
      (no `/vaccination/action-center`, `/vaccination/config`, `/parks/vaccination`)
- [ ] Config and SOP Library are top-level Admin/Data-Ops authority screens
      (CEO/COO/superadmin); a vertical links via `?category=vaccination` /
      `?domain=procurement`, it does not own a nested copy
- [ ] Config UI stays category/schema-driven — changing category changes fields +
      `rule_dsl`; no vaccination fields shown for `feed_direction`
- [ ] Taxonomy respected: Vertical (Preventive Care, Parks, Procurement, …) vs
      Module (Vaccination, Source Entry, …) vs Command lens. Parks is a scope
      dimension, not a vaccination route owner. Vaccination execution renders
      inside `/vaccination`, not as a Parks route
- [ ] Vaccination execution/detail is scoped by the **top-bar park/scope chrome**
      and backend row-click contract; no hidden Parks sidebar/product route is
      created. The scope chrome (park/scope selector in the top bar) must drive
      what a scoped screen shows — a screen that ignores the top-bar scope or
      hardcodes its own scope filter is a finding
- [ ] Preventive Care (the vertical) does not use the syringe icon (that belongs
      to the Vaccination module); Control Tower shows gaps/adherence/exceptions,
      not raw census KPIs (Counts is a separate vertical)

## Selected date window drives fetch AND render (no mismatch)

When a page has a date-range picker (week view, 45-day filter, month picker), the
SAME window selected by the user must drive BOTH the API fetch and the UI render.
A mismatch where fetch uses one range and render another creates silent wrong-
empty states or missed-work visibility.

Review checkpoints:
- [ ] A week picker / month picker / date-range input sets a window (start/end dates or `date_from`/`date_to`)
- [ ] That EXACT window is sent to the API fetch (`?date_from=...&date_to=...` or `?window=...`)
- [ ] The rendered calendar/grid covers the SAME window — no off-by-one days, no
      implicit `today` assumption that diverges from the selected window
- [ ] If the backend returns data from a slightly-different window than requested
      (e.g. aligned to week boundaries), the contract exposes the actual returned
      window, and the UI renders only that actual window (not the requested one)
- [ ] Tests cover window mismatches: select a past week, verify fetch uses that
      week, verify no today-hardcoded data is shown, verify a future-scheduled item
      does not appear
- [ ] Pagination / scroll-more uses the same window boundary, not an implicit
      `date_to: now` that grows during scroll

## State

- [ ] TanStack Query for server state; React local state for UI-only (open drawer, selected row)
- [ ] Same-page drawer/modal/popover open and close are owned by a narrow client
      boundary (`LocalOverlayLink`/local history), never a Next `Link`, native
      anchor/form submission, or router push that re-runs the page's Server
      Components. Back, Escape, outside click, X, close-button animation, and
      trigger-focus restoration all work without a document/RSC request
- [ ] A detail-only overlay opens immediately from its list summary and loads
      only the missing detail through an authenticated Server Action/Route
      Handler inside the open shell; it does not refetch or skeletonize the page
- [ ] `make admin-web-local-overlay-guard` passes with a zero legacy baseline;
      new bypass patterns extend the adversarial self-test, never an allowlist
- [ ] No cross-feature deep imports (`features/x/...` imported inside `features/y/`) — share via `components/`, `lib/`, `packages/`
- [ ] Server Components remain the default; `'use client'` appears only at the
      smallest interactive boundary and does not pull privileged adapters,
      secrets, or unnecessary feature trees into the browser bundle
- [ ] Components/render helpers are pure; derived display state is calculated
      during render, while Effects are reserved for external synchronization
      with cleanup and Strict Mode-safe behavior
- [ ] Every result-changing tenant/role/park/window/filter/cursor value used by a
      TanStack `queryFn` is present in its query key; successful mutations await
      invalidation of all affected keys

### Write-path / server-action idempotency (frontend surface)

Every UI-triggered write — mutation adapter, Next.js server action, or form
submit — is a write path and inherits the repo idempotency contract. Check:

- [ ] Each mutation / server action carries or derives a stable idempotency key
      sent to the backend; the backend (not the client) validates and dedupes
- [ ] The key is stable across an accidental double-submit / retry (button
      double-click, React strict-mode re-invoke, network retry) — a freshly
      minted-per-render key defeats dedupe
- [ ] A same-key exact replay returns the original result and does not re-fire
      side effects; the UI reflects the deduped result, not a second success toast
- [ ] The client does not treat itself as the source of truth for the outcome —
      it re-reads backend state rather than optimistically inventing a terminal
      status the backend never confirmed
- [ ] Every Server Action and Route Handler is reviewed as a directly callable
      request surface: validate untrusted input and independently re-check auth
      and authorization; page-level auth and hidden/disabled UI do not carry over

## Error, Loading, and Accessibility

A swallowed API/backend failure rendered as an empty array (or a collapsed page
that implies "no work exists") is a **merge-blocking product-truth bug**, not a
style nit — it lies to an operator about herd state. Require explicit states.

- [ ] API/backend failures render a visible alert/error state, never an empty
      array or collapsed page that implies no work exists
- [ ] Routes with data dependencies have distinct loading, empty-success, and
      contract-unavailable/error states, each preserving the mock's structure
      (an error/contract-unavailable state is a real designed surface, not a bare
      thrown error or a blank page)
- [ ] App Router route groups that can fail have an `error.tsx` boundary (or an
      equivalent explicit visible error surface) — an unhandled throw that shows
      the framework's default error UI does not satisfy this
- [ ] Business UI blocks on the `/admin-web/bootstrap` contract rather than
      shipping a local fallback when the contract is unavailable (see Golden rule)
- [ ] **a11y (only truly checked by `smoke:visual:live`):** WCAG-AA color
      contrast, minimum target size, visible focus, no label clipping, and narrow
      viewport layout when UI changed. Because `check:mock-fidelity` does not
      assert a11y, a UI change that altered contrast/target size must have run the
      live smoke a11y assertion — a green fidelity/build alone does not prove it
- [ ] Playwright interactions prefer role/label locators and web-first
      assertions. CSS selectors are reserved for visual anatomy assertions;
      XPath, fixed sleeps for app state, and manual non-retrying visibility
      checks are findings
- [ ] Rendered proof includes desktop and narrow screenshots plus open
      modal/drawer states. Axe output is reviewed, and keyboard/focus behavior is
      checked manually because automated accessibility covers only part of the
      surface
