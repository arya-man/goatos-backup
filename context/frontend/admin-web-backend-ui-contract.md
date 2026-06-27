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

Current frontend consumer:

```text
apps/admin-web/components/admin-shell.tsx
apps/admin-web/components/mesha-shell.tsx
apps/admin-web/lib/api/server.ts#getAdminWebBootstrap
```

If this contract is unavailable, admin-web must not silently render a local
fallback IA. It should show a contract-unavailable state with the API/session/
tenant error.

## Ownership Rule

Backend owns:

- visible navigation groups, leaves, hrefs, badge keys, default-open state, and
  disabled reasons
- route labels and breadcrumb page labels
- top-bar product text, scope/range controls, notification disabled reason, and
  role-lens display text
- page titles, subtitles, sections, tables, columns, filters, sort keys,
  page-size options, row-click rules, drawer anatomy, summary/detail field sets,
  empty/error copy, and action availability

Frontend owns:

- mapping backend icon tokens to local icon components
- CSS, responsive layout, density, wrapping, hover/focus state, and theme preview
- local UI state: menu open/closed, drawer selected row search param,
  client-side text input value before submit, modal open state, focus traps
- choosing how much of a backend object to show in compact row/card space versus
  the full drawer, but only from backend-declared summary/detail fields

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
| `/vaccination/execution/sheds/{shed_id}` | `shed-execution` | Shed execution detail | `/vaccination/execution/sheds/{shed_id}` | Full shed context. |
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

Still to migrate page bodies:

- `h1`/`h2`/`h3` text inside feature components.
- table header arrays and local visible-row filter labels.
- filter chip arrangement and enabled/disabled states.
- local sort/page-size semantics where a backend endpoint can support them.
- drawer title/copy/footer action labels.
- Audit Log role-lens chips currently share the old frontend role-lens helper;
  move them to the backend role-lens contract when that page is migrated.

## Page-Body Snapshot for E2E

This is the current visible anatomy that must be preserved or intentionally
changed when each page body starts consuming the backend page contract. E2E
should snapshot the "before" shape, then assert the migrated title/columns/chips
come from `/admin-web/bootstrap` instead of local constants.

| Route | Current body anatomy | Contract migration note |
| --- | --- | --- |
| `/` | `Control Tower`; critical vaccination alert cards; `Open vaccination gaps — gap, severity, owner, next action`; selected alert drawer. | Move page title, section titles, alert field labels, table columns, drawer title/action copy to `control-tower` page contract. |
| `/action-center` | `Action Center`; mode/filter drawer (`My tasks` / `Action Center filters`); work board with `Awaiting verification`; selected work drawer. | Backend must own work-state lanes, filters, chip order, card summary fields, drawer fields, disabled action reasons. |
| `/calendar` | `presentation.page_title`; view tabs, owner tabs, workstream tabs, rhythm day chips, week/month sections, event drawer. | Calendar already has backend presentation semantics; fold the visible tab/chip/table/drawer metadata into the shared admin-web contract for E2E parity. |
| `/protocol-adherence` | `Protocol Adherence`; severity chips; `Vaccination` ledger; visible-row search/filter button; selected adherence record drawer. | Move severity chip order/labels, table columns, row-click param `adh_row`, drawer labels, and workflow/action links to `protocol-adherence` contract. |
| `/workflows` | `Workflows`; workflow catalog/chips; selected `Vaccination workflow chain` / active drive chain. | Backend owns workflow status/stage labels, row summary fields, drilldown route params, and chain section labels. |
| `/workflows/{row_id}` | `Workflow drilldown` fallback or selected workflow title; `Workflow chain`; fchipsbar context. | Backend owns the record title fallback, chain node labels, summary chips, and unavailable-state copy. |
| `/vaccination` | `Vaccination`; `Vaccination status matrix`; `Per-cohort vaccination detail`; `Drive — shed events`; `Supplier warmup — Holding Farm`; record/verify and guidance drawers. | This is the highest-risk page body: backend must own status-matrix protocol cells, cohort table columns, shed-event columns, warmup table columns, filter modal labels, row-click params, and drawer action availability. |
| `/vaccination/execution/sheds/{shed_id}` | Shed detail title; `Work state`; `Drives at this shed`; `Owner chain`; `Blocked / deferred`; `Drive rows`. | Backend object can be large; frontend may compact the summary panels but drawer/detail fields and disabled reasons must come from `shed-execution`. |
| `/procurement/source-entry` | `Source Entry Board`; `Supplier warmup — Holding Farm`; source load rows; selected `Holding-farm load` drawer. | Backend owns load table columns, row summary fields, HF evidence/review/dispatch chips, drawer metagrid fields, and journey/action links. |
| `/procurement/source-entry/loads/{load_id}` | `Load detail` fallback or selected load title; journey timeline; goats in load; pre-dispatch, arrival gate, transit, holding, source health, accepted intake sections. | Treat this as full-detail page for the same load object summarized on `/procurement/source-entry`; no frontend-only field invention. |
| `/counts/herd` | Currently renders `Herd & Lifecycle`; `Herd`; filter modal; selected `Goat Passport` drawer. | Known current mismatch: backend contract title is `Herd Register`. Migration should intentionally replace local body title and table/filter labels with `herd-register` contract values. |
| `/operations/audit` | `Audit Log`; `Span of control`; `Activity trail`; `Advanced (raw) filters`; selected business audit drawer. | Backend owns role-lens chips, filter labels, page-size/cursor rules, activity columns, business drawer labels, and raw metadata placement. |
| `/config` | `Config — Protocol Rules`; `Protocol rules`; search, page-size chips, draft/publish controls; protocol-rule detail process map. | Backend owns section titles, search placeholder, page-size options, rule columns, draft/publish availability, and linked-SOP labels. |
| `/sops` | `SOP Library`; search/domain chips; empty state; SOP cards; new SOP form-builder modal. | Backend owns SOP domain chips, trigger chips, status copy, form-builder vocab, proof/subject/action option labels, and disabled reasons. |
| `/goats/{goat_id}` | `Goat Passport` fallback or goat display ID; summary, warnings, identifiers, evidence, timeline, vaccination passport. | Passport is full-detail for goat objects summarized in Herd Register; backend owns section labels, identifiers/actions availability, timeline labels, and vaccination history table metadata. |

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

## Migration Order

1. Shell/nav/top-bar/route labels. Done in this pass.
2. Page title/subtitle helper consumed by each route feature.
3. Table contract helper for columns, filters, page sizes, row-click params.
4. Drawer contract helper for metagrid field lists and footer actions.
5. Contract drift/E2E assertions that compare rendered text/actions against
   `/admin-web/bootstrap`.
6. Remove old frontend role-lens helper once Audit Log consumes backend
   `role_lenses`.
