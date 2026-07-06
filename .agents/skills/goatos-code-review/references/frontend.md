# Frontend Review — admin-web (SSR-first Next.js)

Frontend lives in `apps/admin-web/` — SSR-first Next.js (App Router) with shared
`packages/`. Architecture: `context/frontend/final-frontend-mobile-backend-architecture.md`.
Scope lock: `context/frontend/current-admin-web-scope.md`. Contract law:
`context/frontend/admin-web-backend-ui-contract.md`. Repo rules:
`apps/admin-web/AGENTS.md`. (Do not hardcode framework versions when reviewing —
read the architecture doc; versions drift.)

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

Check for:
- [ ] No hardcoded page titles / section labels / table headers / filter labels /
      chips / disabled reasons in JSX — they come from `AdminWebPageContract` /
      the `/admin-web/bootstrap` contract (via `lib/api/server.ts`)
- [ ] Live/domain values (parks, sheds, breeds, SOP labels, operators, role scopes)
      come from backend contract compiled from Postgres — never hardcoded, never
      relabeled by `admin_ui_config_entries`
- [ ] No frontend local default that an async config later replaces
- [ ] If `/admin-web/bootstrap` or a page contract is unavailable, business UI
      blocks on a contract-unavailable/error state; no local fallback IA/product
      strings take over
- [ ] Any temporary hardcoded exception is documented in `context/frontend/` before shipping
- [ ] Backend owns canonical due/overdue/escalation/verification state; frontend renders it (no client-authoritative process state)

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
- [ ] Reuses old admin UI: `admin-primitives.tsx`, old cyan/slate palette
      (`#14f1d9`, `#334155`, `#8899AA`), emoji icons, collapse-to-KPI layouts
- [ ] Hardcoded hex instead of theme tokens (`--brand`, `--panel`, `--line`, `--sidebar-2`, status tokens)
- [ ] Emoji/dingbat glyphs in in-scope screens instead of SVG (lucide) icons
- [ ] A control that can't be backed yet is SIMPLIFIED instead of kept at the
      mock's visual richness and `disabled` with an `aria-disabled` reason
      (disable ≠ simplify)
- [ ] Missing data rendered as a silent empty array instead of a visible error/alert band
- [ ] Dead "X · Y" microcopy — every element should be a filter, clickable, clear
      info, or removed
- [ ] "mock" naming leaking into app code

Required checks before push:
```bash
npm --prefix apps/admin-web run check:mock-fidelity   # = check:ia-guard + check:ui-contract + mock-fidelity scan
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run build
```
A green build / passing `check:mock-fidelity` is NOT visual proof — it only scans
for banned emoji/hex/old imports and IA/contract violations. For frontend changes,
require rendered visual QA: run `npm --prefix apps/admin-web run smoke:visual:live`
(local backend + admin-web up), open the screenshots under
`.codex-goatos-render/admin-web-screenshots/`, and compare against the mock
element-by-element (sidebar/nav alignment, spacing, card padding, table density,
label clipping, desktop/narrow responsive). `build` runs the token-leak guard;
if token-leak-sensitive code changed, confirm the guard actually had the needed
environment/token fixture instead of treating a skipped guard as proof.

Additional targeted scripts when the touched surface matches them:
- `npm --prefix apps/admin-web run check:herd-import-security` for herd import /
  local file parsing / source-entry security work
- `npm --prefix apps/admin-web run check:goal1-frontend` for Goal 1 coverage
- `npm --prefix apps/admin-web run smoke:vaccination-click-matrix:live` for
  vaccination matrix/interaction changes
- `npm --prefix apps/admin-web run e2e:sop-builder` for SOP builder flows

## IA / product taxonomy guardrails (from AGENTS.md — non-negotiable)

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
- [ ] Vaccination execution/detail is scoped by the top-bar park/scope chrome and
      backend row-click contract; no hidden Parks sidebar/product route is created
- [ ] Preventive Care (the vertical) does not use the syringe icon (that belongs
      to the Vaccination module); Control Tower shows gaps/adherence/exceptions,
      not raw census KPIs (Counts is a separate vertical)

## State

- [ ] TanStack Query for server state; React local state for UI-only (open drawer, selected row)
- [ ] Mutations carry an idempotency key; backend validates and dedupes
- [ ] No cross-feature deep imports (`features/x/...` imported inside `features/y/`) — share via `components/`, `lib/`, `packages/`

## Error, Loading, and Accessibility

- [ ] API/backend failures render visible alert/error states, not empty arrays or
      collapsed pages that imply no work exists
- [ ] Routes with data dependencies have loading, empty-success, and
      contract-unavailable states that preserve the mock's structure
- [ ] App Router route groups that can fail have an `error.tsx` / boundary or an
      equivalent visible error surface
- [ ] Visual smoke/a11y checks cover contrast, focus, target size, clipping, and
      narrow viewport layout when UI changed
