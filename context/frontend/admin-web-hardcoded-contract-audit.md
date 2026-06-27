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

- Added request-context bootstrap compilation: `/admin-web/bootstrap` now
  receives tenant, actor, active grants, and trace from backend auth middleware.
- Added an adminui Postgres family repository that compiles active parks,
  protocol categories, active/review goat breeds, active health/reproductive
  status vocabularies, defer states, and published SOP labels into the bootstrap
  contract.
- Added backend contract metadata: deterministic `contract_revision`,
  `family_hashes`, and `cache_policy` with an in-process TTL and Redis TTL hint.
- Made top-bar park selector options and optional `park_display_chips`
  DB-compiled from active `locations`, keyed by `location_id`.
- Made navigation enabled/disabled state and role preview/lenses derive from
  request grants instead of being the same static contract for every role.
- Removed the AdminShell's separate top-bar `/admin/locations` read; shell park
  options now come from `/admin-web/bootstrap`.
- Removed static phase-1 park UUID constants from
  `backend/internal/adminui/app/service.go`.
- Stopped publishing static `park_display_chips`; the group remains present but
  empty only when the tenant has no active DB parks or the DB family read fails.
- Stopped publishing static `park:CBE` / `park:CPT` Config scopes. The
  compiler now fills `rule_scopes` from active DB `locations`, keyed as
  `park:<location_id>`, and the rule editor renders those contract options.
- Stopped falling back to static Config authoring vocab when DB-backed families
  are empty. `rule_categories`, breeds, health/reproductive statuses,
  defer-states, and SOP labels are always replaced by DB-compiled options; empty
  families now render as empty or sentinel-only states (`all`/`any`) instead of
  fake values such as sample breeds or SOP labels.
- Removed static feed-item names from the Config contract. Until feed items have
  a governed DB family, the authoring contract exposes only the intentional
  `custom` sentinel rather than live-looking ration names.
- Replaced CBE-specific role-lens labels and hardcoded person preview; role
  preview/lenses are now compiled from request grants.
- Removed frontend CBE defaulting in `MeshaShell`; default park now follows the
  DB-returned park order.
- Removed duplicate frontend `ROLE_LENSES`; Audit Log now consumes bootstrap
  `role_lenses`.
- Removed frontend publishable-source allowlists from Config. Source badges,
  table publishability status, and Publish disabled reasons now consume
  backend-owned `source_systems` option metadata and contract copy; the server
  action lets the protocol API return the authoritative `not_publishable`
  reason.
- Removed unused legacy `apps/admin-web/lib/constants.ts` containing farm tabs
  and shed-capacity truth.
- Added guard coverage for frontend live-location literals and backend bootstrap
  hardcoded UUID/CBE/CPT/person regressions.

## Remaining Required Work

1. Redis/event invalidation.
   The compiler publishes family hashes and a Redis TTL hint, and uses bounded
   in-process caching. Production Redis invalidation still needs config-change
   events/family revision rows for immediate cache misses.

2. Governed config tables for still-static bounded product vocabularies.
   Some finite protocol UI values still come from backend product contract shape
   or DB CHECK constraints rather than first-class governed config tables. Those
   include trigger/repeat/catch-up policies, source-system publishing vocabulary,
   feed class/unit/inventory policies, and stable UI tab/status taxonomies.
   They are no longer frontend-owned, but the next schema pass should give them
   explicit DB family tables where the business needs governance.

3. E2E contract assertions.
   For every active page, fetch `/admin-web/bootstrap` and assert nav labels,
   page titles, table columns, chips, tabs, filters, role lenses, and dropdowns
   are either in the contract or in the row/object returned by a backend API.

## Audit Findings To Keep Watching

- `features/config/rule-dsl.ts` still contains pure JSON builder defaults such
  as version-level `next_due_basis` and stage-source field names. Frontend
  publishability now uses contract metadata, but source-system policy itself
  still needs a governed DB family table before Config can be called fully
  DB-governed.
- Audit Log still has local filter wiring arrays for operation families/status
  tabs. Labels are contract-owned, but filter membership/action semantics should
  be represented in backend controls/options.
- Procurement/counts server actions still have local enum allow-lists used to
  validate submitted backend enum values. They are acceptable only as defensive
  request validation if mirrored by generated OpenAPI/contract enums.
