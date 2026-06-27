# Admin-Web Backend UI Contract

Date: 2026-06-27

Purpose: make admin-web a renderer of backend-owned UI/product contracts. The
frontend may own layout, responsive behavior, local open/closed state, focus, and
icon rendering from tokens. It must not own business truth, route/module
availability, page titles, table semantics, filter/sort availability, disabled
reasons, or action availability.

## Backend Contract Source

Current backend source:

```text
GET /admin-web/bootstrap
backend/internal/adminui/*
contracts/openapi/app-api.yaml#/components/schemas/AdminWebBootstrapResponse
packages/api-client/src/generated/app-api.ts
```

Current frontend consumers:

```text
apps/admin-web/components/admin-shell.tsx
apps/admin-web/components/mesha-shell.tsx
apps/admin-web/lib/admin-ui-contract.ts
apps/admin-web/lib/api/server.ts#getAdminWebBootstrap
apps/admin-web/features/**/*
```

If this contract is unavailable, admin-web must not silently render a local
fallback IA. It should show a contract-unavailable state with the API/session/
tenant error.

## Config API and Cache Shape

Implementation plan:

```text
context/frontend/admin-web-config-api-implementation-plan.md
```

`/admin-web/bootstrap` is the SSR contract for admin-web business UI. The
frontend should block business rendering on this contract and may show only
loading, auth, or contract-unavailable states before it succeeds. It must not
ship local business strings that are later replaced by async config, because
that causes flicker and quietly creates a second frontend-owned contract.

The bootstrap contract should carry small, stable UI contract data:

- navigation groups/items, route labels, product chrome, role lenses, and
  disabled reasons
- page titles, subtitles, section labels, table columns, filters, sort keys,
  page sizes, row-click rules, drawer anatomy, summary/detail fields, empty
  states, and action labels
- small option groups for chips/dropdowns/tabs such as status, severity,
  proof state, work state, procurement state, calendar bands, and park display
  chips
- default UI semantics such as default tab, default sort, default page size,
  default scope mode, and feature availability for the active tenant/role
- icon, tone, density, and surface-kind tokens that the frontend maps to local
  components/styles

The bootstrap contract must not carry large or volatile data:

- rows/cards/alerts/search results/counts
- large searchable lists such as goats, vendors, operators, inventory lots, or
  media references
- live nav counts
- full protocol/SOP bodies except on the page contract that is explicitly
  rendering those records
- transient form input values that depend on a selected row/object

Those values come from normal domain/read-model APIs. Business-managed config
such as locations, animal stages, protocol versions/rules, SOP versions,
permissions, tenant feature flags, and source-backed vocabularies must be
canonical in Postgres. Backend code may still own stable product UI copy in the
first implementation, but the ownership remains backend-side, not React-side.

Caching rule: Postgres is canonical. Redis/Memorystore may cache compiled
contract JSON only as acceleration, never as truth. A production bootstrap
response should include a contract revision/ETag plus family hashes such as
`chrome`, `page:<route_id>`, `options:<family>`, `locations`, `permissions`,
and `sop/protocol:<category>`. Backend cache keys should include tenant, role,
locale, schema version, and the relevant revision/hash. Config writes or
publish actions bump the affected family revision in Postgres and emit a
`config.changed` event; Redis entries either miss by revision or expire by TTL.
Recommended TTLs are short in-process cache for compiled bootstrap (30-120
seconds) and longer Redis TTL for immutable/versioned published families
(10-60 minutes), while current-pointer lookups stay short or event-invalidated.

If bootstrap grows too large, split without changing ownership:

```text
GET /admin-web/bootstrap
  -> shell/chrome, route index, global small option groups, family hashes
GET /admin-web/pages/{route_id}/contract
  -> route-specific page contract
GET /admin-web/config-index
  -> cheap revision/hash freshness check
```

## Ownership Rule

Backend owns:

- visible navigation groups, leaves, hrefs, badge keys, default-open state, and
  disabled reasons
- route labels and breadcrumb page labels
- top-bar product text, scope/range controls, notification disabled reason, and
  role-lens display text
- global shell/chrome copy exposed as `AdminWebBootstrapResponse.copy`
- page titles, subtitles, sections, tables, columns, filters, sort keys,
  page-size options, row-click rules, drawer anatomy, summary/detail field sets,
  empty/error copy, and action availability
- page copy and option groups exposed as `AdminWebPageContract.copy` and
  `AdminWebPageContract.option_groups`

Frontend owns:

- mapping backend icon tokens to local icon components
- CSS, responsive layout, density, wrapping, hover/focus state, and theme preview
- local UI state: menu open/closed, drawer selected row search param,
  client-side text input value before submit, modal open state, focus traps
- choosing how much of a backend object to show in compact row/card space versus
  the full drawer, but only from backend-declared summary/detail fields

## Stable Option-Key Rule

Option-group keys must match stable backend-emitted identifiers. Do not key a
contract option by mutable display text. Examples:

- park display chips must be keyed by `park_id` or canonical `location_code`,
  not `park_name`
- animal-stage chips must be keyed by stage code/id from `animal_stage_lookup`,
  not local text such as `Kid`
- protocol/SOP/status chips must be keyed by the backend enum/config key that
  appears in the API row

The frontend helpers should stay strict. If a row emits a valid key that is not
present in the relevant backend option group, SSR/render should fail during
validation instead of inventing a frontend fallback. The backend contract must
cover every key its data APIs can emit for the active tenant/scope.

## Current Route Inventory

These are the active admin-web routes covered by the first backend contract:

| Route | Contract route_id | Current surface | Main backend source | Notes |
| --- | --- | --- | --- | --- |
| `/` | `control-tower` | Control Tower | `/control-tower/vaccination` | Leadership exception summary. |
| `/action-center` | `action-center` | Action Center | `/vaccination/action-center` | Board rows open action drawers. |
| `/calendar` | `calendar` | Calendar | `/calendar/vaccination/events` | Already had backend presentation contract; now included in shell/page contract too. |
| `/protocol-adherence` | `protocol-adherence` | Protocol Adherence | `/vaccination/adherence` | Ledger rows open record drawers. |
| `/workflows` | `workflows` | Workflow catalog | `/vaccination/action-center` | Catalog/drilldown split remains. |
| `/workflows/{row_id}` | `workflow-record` | Workflow drilldown | `/vaccination/workflows/{row_id}` | Full chain record. |
| `/vaccination` | `vaccination` | PHC Vaccination | `/vaccination/operations`, `/vaccination/execution` | Status matrix, cohort detail, shed execution, supplier warmup context. |
| `/vaccination/execution/sheds/[shedId]` | `shed-execution` | Shed execution detail | `/vaccination/execution/sheds/{shed_id}` | UI route uses the Next.js `[shedId]` segment; backend API uses `{shed_id}`. |
| `/procurement/source-entry` | `source-entry` | Source Entry Board | `/procurement/source-entry/loads` | Procurement bridge into PHC vaccination. |
| `/procurement/source-entry/loads/{load_id}` | `source-load` | Source load detail | `/procurement/source-entry/loads/{load_id}` | Full source-entry journey. |
| `/counts/herd` | `herd-register` | Herd Register | `/goats/search`, admin goat APIs | Vaccination trigger-closure entry point. |
| `/operations/audit` | `audit-log` | Audit Log | `/operations/audit`, `/operations/audit/summary` | Business audit projection. |
| `/config` | `config` | Protocol Rules | `/protocols`, `/protocols/animal-stages` | Authority screen; category-driven. |
| `/sops` | `sops` | SOP Library | `/admin/sops` | Vaccination SOP slice only. |
| `/goats/{goat_id}` | `goat-passport` | Goat Passport | `/goats/{goat_id}`, `/goats/{goat_id}/passport` | Contextual drilldown. |

## Current Migration State

Done in this pass:

- Shell nav, group labels, route labels, top-bar product labels, disabled reasons,
  role lenses, and page contract metadata are backend-owned through
  `/admin-web/bootstrap`.
- `MeshaShell` consumes the generated `AdminWebBootstrapResponse` instead of
  local nav arrays and local route-label regexes.
- The backend contract includes table/drawer/page metadata for every active
  route so page-body migration can be done route by route without inventing
  shapes.
- Page contracts now include `copy` and `option_groups`, and the generated
  OpenAPI client publishes those fields.
- Page-body consumers now use strict helpers in `apps/admin-web/lib/admin-ui-contract.ts`.
  Missing copy/table/option keys throw rather than silently falling back to
  React-local labels.
- Migrated high-risk page bodies include Control Tower, Action Center,
  Calendar, Protocol Adherence, Workflows, PHC Vaccination, shed execution,
  Source Entry, Source Load, Herd Register, Audit Log, Config, SOP Library, and
  Goat Passport.
- Config rule-editor vocabularies are backend-owned: categories, scopes,
  placeholders, sex/breed/health/lifecycle/reproductive/defer values, missed-dose
  policies, source/review statuses, schedule triggers/repeat/catch-up/SOP labels,
  feed classes/items/units/inventory policies, status chips, modal labels, and
  publish disabled reasons.
- SOP builder vocabularies are backend-owned: trigger chips, seed steps, step
  type labels, conditional action labels, proof types, subject scopes, validation
  copy, dry-run copy, and publish/save/dry-run disabled reasons.
- Procurement Source Entry consumes backend option groups for source-load status
  order/labels/tones, warmup evidence labels, health-selection labels, purpose
  labels, warmup expectation labels/tooltips, and journey stages.
- Calendar consumes backend month/weekday labels and calendar-band copy; event
  drawer/link/status/severity/reminder/escalation text is contract-driven.

Explicit exceptions:

- `components/auth/google-login.tsx` and `components/auth/sign-out-button.tsx`
  are pre-contract auth surfaces. They render before the user can reliably fetch
  `/admin-web/bootstrap`, so their Google/session error text remains local until
  an unauthenticated auth-copy endpoint exists.
- `components/admin-shell.tsx` has a contract-unavailable emergency screen. It
  is intentionally local because the contract request failed; using backend copy
  there would create a circular dependency.
- Locale/time-zone tokens (`en-CA`, `en-GB`, `Asia/Kolkata`) and keyboard event
  strings (`Escape`, `Enter`) are technical implementation constants, not UI
  product copy.

## Page-Body Contract Snapshot for E2E

This is the visible anatomy that must be preserved or intentionally changed as
page contracts evolve. E2E should assert the title/columns/chips come from
`/admin-web/bootstrap` instead of local constants.

| Route | Current body anatomy | Contract migration note |
| --- | --- | --- |
| `/` | `Control Tower`; critical vaccination alert cards; open vaccination gaps; selected alert drawer. | Contract route `control-tower` owns section/table/drawer labels, row actions, empty states, and status labels. |
| `/action-center` | `Action Center`; filters; verification queue; work board; selected work drawer. | Contract route `action-center` owns work-state lanes, filter/chip labels, card/drawer labels, action labels, and disabled reasons. |
| `/calendar` | View tabs, owner/workstream/rhythm chips, week/month sections, event drawer. | Contract route `calendar` owns page copy, month/week labels, status/severity/reminder/escalation labels, link/action copy, and drawer labels. |
| `/protocol-adherence` | Severity chips; adherence ledger; visible-row search/filter; selected adherence drawer. | Contract route `protocol-adherence` owns severity labels, table columns, row-click param, drawer labels, links, and empty states. |
| `/workflows` | Workflow catalog/chips; selected workflow chain / active drive chain. | Contract route `workflows` owns workflow status/stage labels, row summaries, chain labels, empty states, and route links. |
| `/workflows/{row_id}` | Workflow drilldown fallback or selected workflow title; workflow chain; context chips. | Contract route `workflow-record` owns fallback title, chain nodes, action links, and unavailable-state copy. |
| `/vaccination` | Status matrix, cohort detail, shed events, supplier warmup, record/verify and guidance drawers. | Contract route `vaccination` owns section titles, filter labels, table columns, supplier/warmup labels, drawer labels, and action availability. |
| `/vaccination/execution/sheds/[shedId]` | Shed work-state, drives, owner chain, blocked/deferred, drive rows, action drawer. | Contract route `shed-execution` owns summary/detail labels and disabled reasons; frontend may compact the same backend object for layout. |
| `/procurement/source-entry` | Source Entry Board, journey chips, source load rows, selected load drawer. | Contract route `source-entry` owns journey order, load table columns, status chip order/labels/tones, warmup/HF/health labels, drawer fields, and action links. |
| `/procurement/source-entry/loads/{load_id}` | Full source-load detail: timeline, goats, pre-dispatch, arrival, transit, holding, source health, PHC handoff. | Contract route `source-load` owns the full-detail page for the same object summarized on `/procurement/source-entry`. |
| `/counts/herd` | Herd Register summary cards, filter modal, herd table, selected Goat Passport drawer. | Contract route `herd-register` owns title/subtitle, summary/table/filter labels, import/register copy, and passport drawer labels. |
| `/operations/audit` | Audit summary, role/status chips, activity trail, advanced filters, business drawer. | Contract route `audit-log` owns role-lens chips, filter labels, page-size/cursor text, columns, drawer labels, and raw metadata placement. |
| `/config` | Protocol Rules, search/page-size controls, draft/publish modal, process map. | Contract route `config` owns rule columns, status chips, editor vocabularies, publish gates, preview copy, and linked-SOP labels. |
| `/sops` | SOP Library search/domain chips, empty state, cards, new SOP form-builder modal. | Contract route `sops` owns domain/trigger/status/proof/subject/action option labels, seed steps, validation copy, and disabled reasons. |
| `/goats/{goat_id}` | Goat Passport summary, warnings, identifiers, evidence, timeline, vaccination passport. | Contract route `goat-passport` owns section labels, identifier/evidence options, timeline labels, vaccination history labels, and action availability. |

## Summary vs Full Drawer Rule

Many admin-web surfaces use a compact row/card/cell and a rich drawer for the
same backend object. This is allowed, but the split must be explicit:

```text
backend object
  -> backend-declared summary_fields for row/card/cell
  -> backend-declared detail_fields for drawer/metagrid/history/footer actions
  -> frontend chooses responsive layout only
```

Frontend must not invent missing full-detail fields or hide a required backend
detail because the compact row did not show it. Examples:

- Action Center card: compact card can show status, owner, due, next action.
  Drawer shows SOP steps, proof pills, evidence, lifecycle, verification state,
  owner chain, and footer actions from the same object/contract.
- Protocol Adherence row: row shows expected, actual, gap, severity, owner, next
  action, evidence. Drawer shows the full record metagrid and links to the
  workflow/action surfaces.
- Vaccination status matrix cell: cell can show a dose/protocol status count.
  Drawer must use the selected cohort/protocol object and reveal the full
  record/verify context only when IDs needed for actions exist.
- Shed execution row: row can show shed, owner, proof, verification, next step.
  Drawer shows obligation/batch/task/completion IDs when present and disables
  actions with exact backend reasons when absent.
- Source Entry load row: row can show supplier, holding farm, purpose, evidence,
  status. Drawer/detail page shows source health, HF evidence, pre-dispatch,
  arrival, rejected/extra/missing cases, and accepted-intake status.
- Herd Register row: row shows goat identity and operational columns. Passport
  drilldown shows timeline, identifiers, and vaccination passport detail.
- Audit Log row: row shows who/what/where/result. Drawer should be a business
  record drawer, not a raw developer field dump; raw metadata can be secondary.

## E2E Validation Rule

E2E must validate two things separately:

1. Contract conformance: every rendered nav item, page title, table column,
   filter, sort key, chip, drawer, and disabled reason must correspond to a
   backend contract entry.
2. Visual fidelity: the rendered anatomy must still match
   `mock/goatos-dashboard-mock.html` unless a documented backend/business reason
   says otherwise.

Full green for "backend-driven UI" requires both:

```text
GET /admin-web/bootstrap available through generated client
  -> shell renders nav/top-bar/route labels from contract
  -> page bodies render titles/tables/filters/chips/drawers from page contracts
  -> Playwright asserts contract text appears and no local-only active control is present
  -> visual smoke confirms mock-shaped anatomy
```

## Ongoing Guard

1. New admin-web pages must add or extend `AdminWebPageContract.copy`,
   `tables`, `controls`, `drawers`, and `option_groups` before rendering visible
   labels/options in React.
2. Frontend changes should use `copy`, `table`, `tableLabels`,
   `tablePageSizes`, `optionGroup`, `optionLabel`, `optionTitle`, and
   `optionTone` from `apps/admin-web/lib/admin-ui-contract.ts`.
3. Backend contract changes must update `contracts/openapi/app-api.yaml` and
   regenerate `packages/api-client/src/generated/app-api.ts`.
4. E2E should compare rendered nav/page/table/filter/chip/drawer text against
   `/admin-web/bootstrap`, then separately run visual fidelity checks against
   `mock/goatos-dashboard-mock.html`.
5. Any temporary exception must be documented in this file before shipping.
