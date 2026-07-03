# Locations PRD

Status: draft for implementation review.

## Summary

Locations is the Goat OS master-data feature for farms, parks, sheds, pens,
cohorts, holding areas, quarantine areas, ICU areas, aliases, and capacity.

Legacy dashboards do not have a real location-management screen. They infer
location data from BigQuery labels, static frontend maps, and dashboard-specific
queries. Goat OS needs a canonical, auditable location master so Counts, Infra,
Feed, Vaccination, Shiftings, Mortality, Health, Herd Search, Goat Passport,
RBAC scopes, and Android SOP routing all agree on the same location tree.

Build relationship:

```text
Locations master + aliases + capacity
  -> Counts/Infra projections
  -> Goat OS dashboard and Android SOP routing
```

Counts and Locations should be built in the same delivery slice, but Locations
is its own feature because other modules depend on it.

## References

- `docs/decisions/high-scale-dashboard-projections.md`
- `docs/features/cutover-contract.md`
- `docs/decisions/high-scale-dashboard-projections.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- Existing schema:
  `backend/migrations/postgres/000001_phase_1_identity_foundation.sql`
- Existing legacy BQ shed seed:
  `backend/migrations/postgres/000017_phase_1_bq_dashboard_shed_locations.sql`
- Legacy Infra code, read-only:
  `<mesha-workspace>/dashboard/app/(dashboard)/infra/`
- Legacy Infra APIs, read-only:
  `<mesha-workspace>/dashboard/app/api/infra/`
- Legacy Counts APIs, read-only:
  `<mesha-workspace>/dashboard/app/api/counts/`

## Product Goals

- Provide one canonical tenant-scoped location tree for Goat OS.
- Support full create, read, update, retire, and safe-delete behavior for
  locations.
- Manage aliases from legacy BQ/Sheets, Android SOP labels, vendor/import
  labels, and manual labels. Animal IDs are globally single-use for life and
  are never scoped to locations/parks.
- Manage shed/housing capacity with effective dates.
- Show current occupancy, capacity, vacancy, and over-capacity warnings without
  scanning goats from the frontend.
- Prevent duplicate active locations caused by spelling/case/source-label drift.
- Provide usage checks before retirement or parent changes.
- Make location changes audited, RBAC-protected, and projection-aware.
- Let BQ/Sheets go away later without rewriting dashboards.

## Non-Goals

- Do not build location CRUD inside Counts as private dashboard state.
- Do not hard-delete referenced production locations.
- Do not use Redis, BigQuery, Sheets, or frontend local state as canonical
  location storage.
- Do not silently auto-create active locations from unknown legacy labels.
- Do not recalculate current occupancy by scanning all goats in browser code.
- Do not solve every future GIS/facility-management feature in v1.

## Users

```text
Admin
  creates and maintains location tree, aliases, capacities, and retirements.

CEO/internal user
  reads location occupancy and capacity for operational review.

Data reviewer
  resolves unknown or conflicting source labels before dashboard sync trusts
  them.

Park head
  reads location lists for assigned scope and may request or perform limited
  updates only when explicitly granted.

Operator
  uses scoped Android location lookup through SOP APIs. Operator access to the
  admin Locations tab is not granted by default.
```

## Required Admin Surface

Add a first-class admin tab:

```text
Locations
```

Required views:

- searchable location table
- farm/park/shed/pen/cohort tree
- detail page or drawer
- alias editor
- capacity editor with effective dates
- usage/references panel
- unknown/conflicting alias review queue
- occupancy/capacity/vacancy panel

This screen is not legacy parity. It is a new Goat OS management surface needed
because legacy dashboards only had derived location labels.

## Required CRUD Scope

Location records must support:

- create location
- edit name, code, type, parent, status, address/geo/timezone, display order,
  and operational notes
- inactivate/retire location
- safe hard-delete only for unreferenced staging/review locations
- list/search by code, name, type, parent, status, and alias
- view tree and breadcrumb path
- view children and active descendants
- view current references and usage before risky changes

Alias records must support:

- create alias
- edit alias source context and notes
- retire/delete alias when safe
- detect conflicts where one source label maps to multiple canonical locations
- resolve unknown legacy labels into canonical locations

Capacity records must support:

- create capacity value for a location
- edit or close effective dates
- view capacity history
- preserve historical capacity evidence from legacy BQ during migration

## Location Types And Operational Classes

V1 must support these physical `locations.location_type` values:

- `farm`
- `park`
- `shed`
- `pen`
- `cohort`
- `unknown`

V1 must also support these operational location classes through explicit
operational attributes on a physical location:

- `holding`
- `quarantine`
- `icu`

Do not add first-class `location_type` values for `holding`, `quarantine`, or
`icu` in this slice unless the TRD is updated with a migration and downstream
compatibility plan. This keeps v1 compatible with the current Phase 1 schema
while still making those operational states queryable and auditable.

## Required Scope Manifest

Production completion requires all Locations scope below. None of these may
remain pending, hidden, or migration-only:

- Admin-web Locations tab with table/tree/detail, parent/breadcrumb, alias,
  capacity, usage, and review flows.
- Tenant-scoped backend CRUD, list/search/detail, alias, capacity, usage, review,
  retire/inactivate, and safe hard-delete APIs with generated clients.
- Supported `location_type` values: farm, park, shed, pen, cohort, unknown.
- Operational attributes/classes for holding, quarantine, and ICU without adding
  incompatible first-class location types.
- Alias source contexts for Counts, Mortality, BQ dashboard shed labels, old-tag
  legacy codes, and required migration labels.
- Seeded CBE/CPT/HF scope anchor and seeded alias protections.
- Usage checks across goats, location history, projections, aliases, SOP
  submissions, imports, child locations, and RBAC grants.
- Projection invalidation/rebuild hooks for Counts/Infra and alias resolution for
  Mortality and other legacy-label dashboards.

Supporting scope includes future Feed, Vaccination, and other dashboard aliases
that are cataloged for compatibility but not yet part of an active feature
slice. Supporting aliases may be reviewed later, but they do not replace the
required Counts/Mortality/Infra label-resolution scope above.

## Legacy Evidence

Legacy dashboards use location-like labels across several sources:

| Source | Evidence |
| --- | --- |
| Counts detail | `farm`, `shed`, `shed_tag`, `shed_name` from `counting_db_with_holding_dev` |
| Infra detail | `shed`, `shed_tag`, `breed`, count grouped from Counts detail |
| Infra capacity | `counting_shed_capacity_status_dev` and `shed_capacity_count_dev` |
| Static frontend fallback | `SHED_CAPACITIES` in legacy constants |
| Feed housing | `last_7_days_feed_per_animal_shedwise.shed` plus count-table `shed_tag` lookup |
| Vaccination | `vaccination_dashboard` grouped by `farm`, `shed`, `shed_tag`, `age`, `animal_type` |
| Mortality | `mortality_trend_dev` and `deaths_fact_dev` use `shed` and `shed_tag` |
| Shiftings | CBE/CPT current-stage views use farm-specific stage aging |

These are discovery inputs and parity oracles. They are not final canonical
truth. Unknown source labels should create review work, not duplicate active
locations.

Required live-source access for legacy label/capacity evidence is owned by the
data/source owner and the feature implementer together. If a required BQ/Sheets
or Drive-backed source remains blocked, Locations cannot be production-complete
unless product explicitly removes the affected label/capacity scope from this
PRD/TRD.

For Phase 1 parity, legacy `farm` labels such as `CBE` and `CPT` resolve to the
existing seeded park-scope rows, not to new `location_type = farm` records. Those
rows also define old-tag scope, so they must not be edited as ordinary
dashboard labels. `HF` / `Holding Farm` remains a reviewed holding/source
context until a product-owned location policy replaces it.

## Source Of Truth

Locations v1 uses:

| Layer | Store | Role |
| --- | --- | --- |
| Runtime truth | Postgres | locations, aliases, capacity, audit, usage checks |
| Temporary upstream | BigQuery/Sheets | legacy label and capacity evidence |
| Future upstream | Android SOP/backend commands | location updates, count verification, shifting events |
| Dashboard serving | Postgres projections | occupancy, capacity, vacancy, unknown-label counts |
| Cache/job aid | Redis optional | short-lived cache or locks only, never canonical |

## Review Behavior

Create review items when:

- source label does not map to a canonical location
- one alias maps to multiple possible locations
- one canonical location receives contradictory parent/type evidence
- capacity source conflicts with active capacity record
- a requested parent change would move active goats unexpectedly
- a retirement request has active goats, active child locations, RBAC grants, or
  open SOP dependencies
- a requested change touches seeded CBE/CPT/HF scope rows or seeded BQ dashboard
  shed aliases without an approved migration plan

An approved migration plan is a required artifact for seeded CBE/CPT/HF or
seeded-alias changes. It must name affected IDs/aliases, old-tag scope impact,
dashboard projection impact, parity fixtures, rollback path, approving actor,
and audit/run id.

Humans resolve labels and policies. Projection jobs then rebuild Counts/Infra.

## RBAC

Required permissions:

- `locations.read`
- `locations.write`
- `locations.review`
- `locations.retire`

Initial role mapping:

- `admin`: read, write, review, retire
- `ceo_internal`: read
- `verifier`: read and review if data-review ownership is confirmed
- `park_head`: read for assigned scope; write only when explicitly granted
- `operator`: no admin Locations access by default; use scoped Android SOP
  location lookup if needed

Authorization must use tenant-scope grants and fail closed.

## Acceptance Criteria

Locations is complete only when:

- PRD/TRD are accepted before implementation.
- Admin-web has a Locations tab with table/tree/detail/alias/capacity/usage
  flows.
- Backend has tenant-scoped CRUD APIs with generated OpenAPI clients.
- Location aliases are managed and conflict-detecting.
- Capacity is effective-dated and audited.
- Referenced locations cannot be hard-deleted.
- Parent cycles and cross-tenant references are blocked.
- Usage checks cover goats, location history, projections, aliases, SOP
  submissions, imports, child locations, and RBAC grants as applicable.
- Counts sync resolves farm/shed labels through aliases.
- Mortality and other legacy-label dashboards resolve farm/shed/housing labels
  through aliases instead of private maps.
- Seeded CBE/CPT/HF scope rows and seeded BQ shed aliases are protected from
  accidental edit, retire, merge, or remap.
- Counts/Infra projections are marked stale or rebuilt after relevant location
  changes.
- Hot reads are indexed and paginated for one-million-goat scale.
- No required Locations CRUD, alias, capacity, usage, review, or seeded-scope
  protection behavior remains pending for production completion.
- Numeric and visual Counts parity can run using canonical location mappings.
