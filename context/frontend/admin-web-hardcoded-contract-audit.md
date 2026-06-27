# Admin-Web Hardcoded Contract Audit

Date: 2026-06-27

Purpose: track places where admin-web or the adminui bootstrap contract still
risks owning product/data truth outside the database-backed backend contract.

## Rule

Frontend is a renderer. Backend code is a contract compiler. Postgres is the
source for live tenant data and business-managed vocabularies.

Backend code may temporarily hold stable product contract shape while the
config compiler is being built, but must not hardcode live data:

- tenant/user/location/goat/shed/vendor/operator UUIDs
- park/location codes or names such as CBE/CPT/Coimbatore/Channapatna
- person names
- shed/farm capacities
- DB-backed dropdown vocabularies or source-backed rule choices

## Fixed In Current Pass

- Removed static phase-1 park UUID constants from
  `backend/internal/adminui/app/service.go`.
- Stopped publishing static `park_display_chips`; the group remains present but
  empty until DB-compiled location options land.
- Stopped publishing static `park:CBE` / `park:CPT` Config scopes. Phase-0
  publishes tenant scope only; park scopes must be compiled from `locations`.
- Replaced CBE-specific role-lens labels and hardcoded person preview with
  generic labels until request-context actor/role compilation lands.
- Removed frontend CBE defaulting in `MeshaShell`; default park now follows the
  DB-returned park order.
- Removed duplicate frontend `ROLE_LENSES`; Audit Log now consumes bootstrap
  `role_lenses`.
- Removed unused legacy `apps/admin-web/lib/constants.ts` containing farm tabs
  and shed-capacity truth.
- Added guard coverage for frontend live-location literals and backend bootstrap
  hardcoded UUID/CBE/CPT/person regressions.

## Remaining Required Work

1. Request-context bootstrap compiler.
   `GET /admin-web/bootstrap` must take authenticated tenant, actor, role,
   capabilities, locale, and default scope. Static `Bootstrap()` is only a phase-0
   bridge.

2. DB-backed location family.
   Compile `top_bar.park_selector.options`, `park_display_chips`, Config
   `rule_scopes`, default park scope, and park display aliases from
   `locations`/`location_aliases`, keyed by `location_id`.

3. DB-backed permission/role family.
   Compile nav visibility, action enablement, disabled reasons, role lenses, and
   role preview from permissions/actor context. Backend RBAC remains authority.

4. DB-backed protocol/config vocabularies.
   Move source-backed Config dropdowns and publishability rules into backend
   contract/DB families. Frontend `rule-dsl.ts` may keep pure validators and JSON
   builders, but not visible option lists or source authority.

5. E2E contract assertions.
   For every active page, fetch `/admin-web/bootstrap` and assert nav labels,
   page titles, table columns, chips, tabs, filters, role lenses, and dropdowns
   are either in the contract or in the row/object returned by a backend API.

## Audit Findings To Keep Watching

- `features/config/rule-dsl.ts` still contains finite rule defaults and
  publishability validators. Some are product-contract data and should move to
  backend config families; some are pure JSON builder defaults. Review item 4
  before calling Config fully DB-driven.
- Audit Log still has local filter wiring arrays for operation families/status
  tabs. Labels are contract-owned, but filter membership/action semantics should
  be represented in backend controls/options.
- Procurement/counts server actions still have local enum allow-lists used to
  validate submitted backend enum values. They are acceptable only as defensive
  request validation if mirrored by generated OpenAPI/contract enums.
