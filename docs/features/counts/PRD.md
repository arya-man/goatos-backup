# Counts PRD

Status: draft for implementation review.

## Summary

Counts brings the legacy Counts dashboard into Goat OS as a backend-owned,
Postgres-served feature that matches the legacy UI and numbers while replacing
the runtime data path.

Initial migration path:

```text
Legacy BigQuery/Sheets -> Goat OS backend sync -> Postgres source/snapshot/projection tables
  -> Goat OS API -> admin-web Counts UI
```

Final operating path:

```text
Android SOPs + Goat OS backend commands -> canonical Goat OS facts/events
  -> Postgres projections -> Goat OS API -> admin-web Counts UI
```

Never:

```text
admin-web -> BigQuery / Sheets / Drive / raw database
```

Legacy aggregate count rows may be used to serve dashboard parity during
migration. They must not be used to invent goat passports, identifiers, birth
events, death events, or animal-level history.

## References

- `docs/decisions/high-scale-dashboard-projections.md`
- `docs/features/cutover-contract.md`
- `context/analytics/final-analytics-infra.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`
- Legacy UI reference:
  `https://dashboard--goatos-sheets.us-central1.hosted.app/counts/overall`
- Legacy route reference, read-only:
  `<mesha-workspace>/dashboard/app/(dashboard)/counts/_components/counts-dashboard.tsx`
- Legacy API/query reference, read-only:
  `<mesha-workspace>/dashboard/app/api/counts/`
- Current Goat OS Counts route:
  `apps/admin-web/features/identity-counts/index.tsx`
- Current Goat OS identity count API contract:
  `contracts/openapi/analytics-api.yaml`

## Product Goals

- Match the legacy Counts screen closely enough that users recognize the same
  workflow, tabs, charts, and numbers.
- Serve Counts from Goat OS backend APIs, not from frontend BigQuery, Sheets,
  Drive, Firestore, or database access.
- Keep page reads fast at one million goats by reading projection rows, not by
  scanning goats or source rows on every request.
- Make freshness, source availability, rebuild state, and open conflicts visible
  without replacing the dashboard experience.
- Support the migration period where BQ/Sheets are upstream, then allow those
  sources to go away when Android SOPs and backend commands own writes.
- Produce numeric parity and screenshot parity artifacts before calling the
  feature complete.

## Non-Goals

- Do not create goat passports from aggregate counting rows.
- Do not keep BigQuery or Sheets as a permanent runtime dependency.
- Do not build dashboard charts from static frontend data.
- Do not make Redis, a time-series database, Cube, BigQuery, or the frontend app
  canonical storage for Counts.
- Do not silently render missing data as zero.
- Do not rebuild the entire one-million-goat herd on every page load.
- Do not replace all SOP modules in this feature. Counts consumes canonical
  facts from identity, location, stage/status, weighing, health, birth, death,
  sale, and shifting modules as they land.

## Users

```text
CEO/internal user
  reviews current herd size, location split, status/stage split, breed split,
  age split, gender split, total weight, and farm value.

Data reviewer/admin
  checks freshness, source drift, missing source rows, stale projections, and
  aggregate-vs-canonical reconciliation gaps.

Park head / operations lead
  uses farm, shed, stage, age, and gender breakdowns to plan feeding, movement,
  fattening, and daily work. In the current role model, this persona maps to
  `park_head` for dashboard reads unless a separate operations role is added.
```

## Required Dashboard Scope

The target UI is the legacy Counts screen with these tabs:

- Overall
- Core Farms
- CBE
- CPT
- Holdings

The target Overall/Core Farms dashboard must include:

- KPI cards:
  - Total Active Goats
  - Farm Value
  - Total Weight
  - Average Weight per Goat
  - Breeds Tracked
- Count by Status
- Count by Breed
- Status by Breed
- Count by Farm
- Count Distribution by Farm
- Count by Age
- Adults by Gender
- Kids by Gender
- K0 to K3 Kids by Gender
- Fattening by Gender
- Fattening Kids Male - Breedwise, Core Farms only
- Fattening Kids Female - Breedwise, Core Farms only

The target single-location tabs must keep the same chart semantics while
filtering to CBE, CPT, or Holdings.

Current Goat OS identity-count charts, such as location mapped and gender, may
remain as supporting panels only if they do not replace required legacy parity
sections.

## Required Scope Manifest

Production completion requires all legacy Counts tabs and sections below. None
of these may remain pending, hidden, or migration-only:

- Tabs: Overall, Core Farms, CBE, CPT, Holdings.
- KPI cards: Total Active Goats, Farm Value, Total Weight, Average Weight per
  Goat, Breeds Tracked.
- Charts/sections: Count by Status, Count by Breed, Status by Breed, Count by
  Farm, Count Distribution by Farm, Count by Age, Adults by Gender, Kids by
  Gender, K0 to K3 Kids by Gender, Fattening by Gender, Fattening Kids Male -
  Breedwise for Core Farms, Fattening Kids Female - Breedwise for Core Farms.
- Filters and tab behavior: legacy-equivalent semantics for Overall/Core Farms,
  CBE, CPT, and Holdings.
- Freshness/source/review surfaces: source unavailable, stale, rebuilding,
  never-synced, conflict count, and `summary_source_date` visibility.

Supporting scope is allowed only when it does not replace required legacy
parity. Current Goat OS identity-count panels such as location mapped and gender
can remain supporting panels, but they do not satisfy the required legacy Counts
sections above and do not block completion if intentionally omitted.

## Location Management Dependency

Counts and Locations should be built together. Counts must consume the shared
Locations feature defined in `docs/features/locations/PRD.md` and
`docs/features/locations/TRD.md`, not maintain a private farm/shed mapping.

Legacy does not provide location CRUD. It derives farms, sheds, shed tags, and
capacities from BQ labels and static frontend maps. Goat OS needs a Locations
tab so Counts can resolve legacy labels to canonical location IDs and so future
Android SOPs can keep using the same location tree after BQ/Sheets go away.

Counts completion depends on:

- canonical farm/park/shed/holding records
- source aliases for legacy BQ/Sheets labels
- capacity records for shed/farm occupancy and vacancy
- review flow for unknown or conflicting location labels
- projection invalidation when location names, parents, aliases, or capacity
  records change

## Legacy Source Inputs

The legacy Counts route currently uses these source shapes:

| Source | Legacy role | Fields used |
| --- | --- | --- |
| `ceo_dashboard.counting_db_with_holding_dev` | Current daily count detail | `date`, `farm`, `shed`, `shed_tag`, `breed`, `age`, `goat_count`, `staff`, `shed_name` |
| `farm.daily_summary_dev` | Count, weight, and value KPI summary | total, CBE, CPT, procurement count/weight/value columns |
| `ceo_dashboard.counting_kpis_daily` | Age and gender KPI rows | `as_of_date`, `metric_name`, `farm`, `animal_type`, `fattening_gender`, `total_count` |
| `ceo_dashboard.core_farm_genderwise` | Core Farms fattening/kids breed and gender rows | `farm`, `breed`, `gender`, `total_count` |

Required location/capacity sources for the Counts + Locations delivery slice:

| Source | Legacy role | Fields used |
| --- | --- | --- |
| `ceo_dashboard.counting_shed_capacity_status_dev` | Per-shed capacity and housing status | `farm`, `shed`, `capacity`, plus status fields when present |
| `ceo_dashboard.shed_capacity_count_dev` | Farm-level capacity and vacancy | `farm`, `total_capacity_farm`, `total_vacancy_farm` |

Adjacent location evidence for future feature parity:

| Source | Legacy role | Fields used |
| --- | --- | --- |
| `feedDB.last_7_days_feed_per_animal_shedwise` | Feed by housing unit | `farm`, `shed`, `date`, feed-per-animal fields |
| `ceo_dashboard.vaccination_dashboard` | Vaccination by farm/shed/status/age | `farm`, `shed`, `shed_tag`, `age`, `animal_type`, vaccine fields |

Counts implementation must probe the four Counts metric sources and the two
required capacity sources before real sync. Feed and Vaccination evidence should
be cataloged for Locations compatibility, but unavailable Feed/Vaccination
sources must not block Counts parity unless their feature slice explicitly
depends on them. If a required source is blocked or unavailable, record source
unavailable and keep the affected sections stale or pending in internal/dev
review only. Do not write fake zeros.

Required live-source access is owned by the data/source owner and the feature
implementer together. If a required BQ/Sheets/Drive source remains blocked,
Counts cannot be production-complete unless product explicitly removes the
affected section from required scope in this PRD/TRD.

## Metric Semantics

Counts is primarily a current-state dashboard.

The legacy page shows a daily snapshot, currently using yesterday in IST when no
date is provided. The feature must preserve this behavior until the product
chooses a different reporting cutoff.

Legacy has one important date quirk: detail rows and age/gender KPI rows honor
the selected `date`, but `daily_summary_dev` KPI summary rows are always read
from yesterday in IST. Counts must either replicate that quirk in legacy-parity
mode or record an explicit explained delta. API responses should expose the
summary source date separately from the selected snapshot date.

Required semantics:

- `Total Active Goats`: total current active animals for the selected tab and
  `summary_source_date` in legacy-parity mode.
- `Farm Value`: legacy total weight value for the selected tab and
  `summary_source_date` during migration; after cutover it must come from the
  canonical valuation policy or valuation facts.
- `Total Weight`: legacy total weight for the selected tab and
  `summary_source_date` during migration; after cutover it must come from
  canonical weighing facts or approved modeled weight projections.
- `Average Weight per Goat`: `Total Weight / Total Active Goats`.
- `Breeds Tracked`: distinct non-empty breed labels with positive count.
- `Count by Status`: sum `goat_count` by legacy `shed_tag`/normalized status
  label.
- `Count by Breed`: sum `goat_count` by breed label.
- `Status by Breed`: cross-tab count by status and breed.
- `Count by Farm`: sum `goat_count` by farm bucket.
- `Count by Age`: adult vs kid counts from `counting_kpis_daily` when present,
  not guessed from legacy `age` labels that may contain gender-like holding
  labels.
- `Adults by Gender`: `adults_by_gender`.
- `Kids by Gender`: legacy UI combines `kids_by_gender` and
  `genderwise_fattening_count`.
- `K0 to K3 Kids by Gender`: `kids_by_gender`.
- `Fattening by Gender`: `genderwise_fattening_count`.
- `Fattening Kids Breedwise`: `core_farm_genderwise`, split by gender and
  grouped by breed.

Every projected value must retain source evidence and freshness. Absence of a
projection row is not zero.

## Source Of Truth And Storage Decision

Counts v1 uses these stores:

| Layer | Store | Role |
| --- | --- | --- |
| Runtime app truth | Postgres | source rows, normalized snapshots, projections, freshness, audit, review |
| Existing identity truth | Postgres | `goats`, `breeds`, `locations`, `goat_identity_events`, `goat_identity_counters` |
| Temporary upstream | BigQuery and Sheets/Drive through legacy | read-only migration input until Android/BE owns writes |
| Future upstream | Android SOPs and backend commands | canonical identity, location, stage/status, weighing, valuation, and lifecycle writes |
| Governed analytics | Cube, later | official semantic metric layer with parity gates |
| Warehouse/history | BigQuery, later downstream | historical analytics after Goat OS emits facts/events |
| Cache/job aid | Redis, optional | short-lived API cache, job progress, advisory-lock fallback, or rate limiting only |

Counts v1 does not need a new time-series database. Postgres projection tables
with date/month buckets are sufficient. If future telemetry needs sub-minute
live herd movement streams, that is a separate ADR.

## Android SOP Cutover

Slack/Sheets/App Script SOPs are legacy input and migration evidence only. The
final Counts dashboard must be fed by Android app and backend workflows such as:

- daily or periodic shed count verification
- goat location/shifting updates
- status/stage transitions
- gender/breed corrections through identity review
- weighing submissions
- valuation inputs or approved valuation rules
- birth, death, sale, and inactive lifecycle events from their owning modules
- location master CRUD, alias mapping, and capacity changes
- shifting/location events

The Counts projection logic must not care whether a snapshot came from legacy BQ
or Goat OS canonical writes. Source adapters can change; the API and frontend
contract should remain stable.

During cutover, Counts must follow `docs/features/cutover-contract.md` rather
than switching the whole tenant at once. Canonical facts win for a
date/location/metric grain only after that grain has complete canonical coverage;
legacy rows fill gaps until then. If both sources describe the same shed/day or
metric bucket, the serving projection must dedupe by logical count fact and
either prefer canonical evidence or open a reconciliation gap.

Canonical daily snapshots must be explicitly materialized from continuous Goat
OS events before BQ/Sheets are removed. The default cutover candidate is:

```text
snapshot_date D = latest accepted canonical state per goat/fact at the approved
Asia/Kolkata cutoff for date D
```

If product chooses a different cutoff or age/status policy, update the formula
register and parity artifacts before cutover.

## Freshness And Availability

Every Counts API response must expose the standard dashboard freshness envelope
from `docs/decisions/high-scale-dashboard-projections.md`.

The shared legacy-sync runtime already uses `green`, `yellow`, `red`, and
`unknown` as freshness values. Counts must not invent a conflicting
freshness vocabulary. The `freshness_status` field uses the shared values.
User-facing states such as rebuilding or source unavailable are serving states
layered on top of that shared freshness value.

The UI must distinguish these serving states:

- never synced
- fresh
- stale
- rebuilding
- failed
- source unavailable
- conflicts open, derived from `conflict_count > 0` and not a freshness value

If a source is unavailable, keep prior projections visible as stale when safe.
Do not overwrite a prior good projection with zeros.

## Review And Conflict Behavior

Counts can produce review items or reconciliation gaps when:

- legacy aggregate count differs from canonical Goat OS count for the same date
  and dimension
- legacy and canonical facts overlap for the same logical count fact but disagree
- required source columns are missing
- source row hashes changed unexpectedly
- BQ/Sheets source returns duplicate keys
- farm, shed, status, breed, age, or gender labels cannot be mapped
- weight/value KPI summary conflicts with detail count totals

Humans do not edit aggregate dashboard values directly. They fix source mapping,
canonical goat data, valuation rules, or source sync configuration, then the
projection is rebuilt.

## RBAC

Counts is visible only to authenticated, granted Goat OS users.

Required permissions:

- `analytics.counts.read`
- `analytics.counts.sync`
- `analytics.counts.review`

Initial role mapping:

- `analytics.counts.read`: `admin`, `verifier`, `park_head`, `ceo_internal`
- `analytics.counts.review`: `admin`, `verifier`, `ceo_internal`
- `analytics.counts.sync`: `admin`, `ceo_internal`

`operator` does not receive dashboard Counts access by default. Operator-facing
Android flows should use scoped SOP APIs, not the admin dashboard permission.

Backend authorization must use tenant-scope grants. Do not authorize Counts by
email inside request handlers.

## Acceptance Criteria

Counts is complete only when all of these are true:

- PRD/TRD are accepted before implementation starts.
- The data path is backend-owned and Postgres-served.
- No admin-web code reads BigQuery, Sheets, Drive, or raw databases directly.
- Legacy source list and formulas are pinned.
- Projection tables, projection state, and source row evidence exist.
- API responses include freshness, source availability, and conflict counts.
- Overall, Core Farms, CBE, CPT, and Holdings tabs render with legacy-equivalent
  charts and filters.
- Locations feature is present or being built in the same slice, and Counts
  source labels resolve through canonical locations and aliases.
- Numeric parity artifact compares legacy vs Goat OS value by value.
- Canonical-vs-legacy shadow parity artifact proves Android/backend facts rebuild
  the same values for any section where BQ/Sheets will be removed.
- Audited coverage state exists for any section/grain promoted to canonical-only
  serving.
- Screenshot parity artifact compares legacy vs Goat OS desktop and narrow
  views.
- No required legacy Counts metric or section remains unmatched, pending, or
  hidden for production completion. Internal/dev builds may mark pending source
  work honestly, but pending work blocks completion.
- Hot dashboard reads use bounded projection queries.
- Sync/rebuild jobs are idempotent and retry-safe.
- One-million-goat scale checks are documented with query plans or load tests
  for hot reads and rebuild paths.
- BigQuery/Sheets can be removed later section by section by passing the shared
  cutover contract, not by rewriting the frontend.
