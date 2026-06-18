# Mortality TRD

Status: draft for implementation review.

## Technical Summary

Mortality is a high-scale dashboard feature. It must follow
`docs/decisions/high-scale-dashboard-projections.md` and the shared
legacy-to-canonical cutover rules in `docs/features/cutover-contract.md`.

Runtime reads are served from Postgres projection tables. Sync/rebuild jobs may
read canonical facts, source rows, and temporary staging tables. Request
handlers must not run raw all-history aggregation queries for chart rendering.

`mortality_events` is required. It is the durable canonical event spine for this
feature, not an optional staging layer. Legacy BQ/Sheets adapters and future
Goat OS SOP/form adapters must normalize into `mortality_events`; projection
logic must read canonical events plus approved denominator projections, not
legacy source row shapes directly.

All legacy farm, shed, housing, and status-location labels must resolve through
the Locations alias service. Mortality must not maintain a private location
mapping table.

## System Diagram

```mermaid
flowchart LR
  BQ["Legacy BigQuery\n(read-only upstream)"]
  Sheets["Legacy Sheets/Drive\n(read-only upstream)"]
  Android["Android Death SOP\nfuture canonical upstream"]
  AppAPI["Goat OS write API\nvalidation + idempotency"]
  Sync["Goat OS mortality sync job\nmanual, RBAC, audited"]
  SourceRows["Postgres mortality_source_rows\nraw source evidence + hashes"]
  Facts["Postgres mortality_events\ncanonical event facts"]
  Review["Review/conflict tables\nunresolved links and disagreements"]
  Worker["Projection rebuild worker"]
  Proj["Postgres mortality_projection_rows\nchart-ready rows"]
  State["Postgres projection state\nfreshness + watermarks"]
  API["Goat OS backend API"]
  UI["admin-web /dashboard/mortality"]
  Cube["Cube metric layer\nlater governed analytics"]

  BQ --> Sync
  Sheets --> Sync
  Android --> AppAPI
  AppAPI --> Facts
  Sync --> SourceRows
  Sync --> Facts
  Sync --> Review
  Facts --> Worker
  Review --> Worker
  Worker --> Proj
  Worker --> State
  Proj --> API
  State --> API
  API --> UI
  Proj -. parity gate .-> Cube
```

## Data Store Matrix

| Store | Used For Mortality v1? | Role |
| --- | --- | --- |
| Postgres | Yes | canonical facts, source rows, projections, review, audit, freshness |
| BigQuery | Yes, upstream only | temporary legacy current-data source during migration |
| Sheets/Drive | Yes, upstream only | temporary legacy source where legacy BQ/views depend on Sheets |
| Cube | Later/optional in v1 | governed semantic metric for `mortality_rate`; requires parity gate |
| Redis | No canonical use | optional only for short-lived cache, job progress, rate limiting, locks |
| Tinybird | No | reserved for hot telemetry/live streams, not Mortality v1 |
| Time-series DB | No | not part of Mortality v1 |

The final runtime app storage is Postgres. BigQuery/Sheets feed Postgres until
Goat OS Android/SOP forms become the primary write path.

## Canonical Event Spine

Mortality follows the same pattern as identity:

```text
mortality_source_rows
  legacy/source evidence and replay material

mortality_events
  durable canonical mortality facts, required

mortality_projection_rows
  dashboard-serving rows

mortality_projection_state
  freshness, watermark, rebuild, and source availability
```

Only the source adapter layer should know legacy BigQuery/Sheets column names.
Projection code should be source-agnostic after facts are normalized into
`mortality_events`.

## Source Inputs

Implementation must inspect the legacy dashboard code and pin actual formulas.
Known source names from the legacy mortality dashboard area include:

- `mortality_total_dev`
- `mortality_overall_breedwise_dev`
- `mortality_this_month_dev`
- `mortality_this_month_farmwise_dev`
- `monthly_mortality_rate`
- `overall_farmwise_mortality_dev`
- `load_Wise_pct_data`
- `deaths_monthly_trend_v`
- `mortality_genderwise`
- `mortality_trend_dev`
- `deaths_fact_dev`
- `mother_litter_size_dev_breedwise`
- `mother_litter_size_dev_overall`
- `mortality_by_litter_size_overall_dev`
- `last_month_mother_mortality_breed_dev`
- `last_month_mother_mortality_litter_dev`
- `last_month_mortality_by_litter_size_view`
- `load_wise_procurement_with_status`
- `breedwise_load_pct`
- `birth_analysis_view` or `mother_kid_facts` where legacy formulas use them

These `_dev` names are legacy oracle candidates, not a guarantee that the dev
view is the live source of product truth forever. Implementation must confirm
the currently served legacy route/source before pinning a formula; if a
production replacement exists, update this list and the formula register.

Source classification must be pinned before implementation. Do not convert every
source row into a mortality event.

| Source class | Current known examples | Allowed writes |
| --- | --- | --- |
| Event-creating sources | `deaths_fact_dev`; abortion fact source once pinned | May create or update `mortality_events` |
| Denominator/reference sources | `load_wise_procurement_with_status`; identity/count projections; source tables used only to derive populations | May update source rows and denominator/projection inputs, never create death events |
| Legacy rollup/parity sources | `mortality_total_dev`; `mortality_overall_breedwise_dev`; `mortality_this_month_dev`; `mortality_this_month_farmwise_dev`; `monthly_mortality_rate`; `overall_farmwise_mortality_dev`; `load_Wise_pct_data`; `mortality_genderwise`; `mortality_trend_dev`; `deaths_monthly_trend_v`; `mother_litter_size_dev_breedwise`; `mother_litter_size_dev_overall`; `mortality_by_litter_size_overall_dev`; `last_month_mother_mortality_breed_dev`; `last_month_mother_mortality_litter_dev`; `last_month_mortality_by_litter_size_view`; `breedwise_load_pct` | Parity oracle, formula pinning, or projection validation only; never create events |

One real death must create at most one `mortality_events` row. Rollup tables can
validate or explain that event, but must not generate additional events for the
same real-world death.

Event-source coverage gate:

- Rollups such as `mortality_total_dev`, `monthly_mortality_rate`, and
  `mortality_this_month_farmwise_dev` may validate shipped numbers, but they
  cannot be the source that creates `mortality_events`.
- Before a section is marked complete, event-creating sources must reproduce
  every required total at that section's grain. If source coverage is not proven,
  the section may appear only in internal/dev as pending or migration-only, and
  it blocks production completion.
- A parity artifact must show which event source covers each legacy rollup
  total and which rollup totals remain uncovered.

Before running a real sync, probe every required source. If a source is blocked
by Drive/BQ permissions or is missing, record `source_unavailable` and do not
write fake zero projections.

## Formula Register

Before coding a chart or summary pill, add an explicit formula row here. The
initial implementation must fill this table from legacy code/SQL, not guess.

Window meanings:

- Overall: all-time legacy window unless the formula row pins another range.
- This Month: current month-to-date legacy window.
- Month-wise: one row per month bucket.

`review_status_filter` pins whether unresolved-but-real events are counted.
For death counts, include `accepted + needs_review` when `needs_review` means
the death is real but identity linkage is unresolved. Exclude `rejected` and
`superseded`.

| Metric/section | Numerator event types | Review status filter | Denominator basis | Window | Source oracle | Exact or reconciled parity |
| --- | --- | --- | --- | --- | --- | --- |
| Total Deaths | death only | accepted + needs_review | none | period-specific | legacy mortality SQL | reconciled |
| Kid Deaths | death only where age class is kid | accepted + needs_review | none | period-specific | legacy mortality SQL | reconciled |
| Adult Deaths | death only where age class is adult | accepted + needs_review | none | period-specific | legacy mortality SQL | reconciled |
| Total Mortality Rate | death only | accepted + needs_review | pinned population denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Kid Mortality Rate | death only where age class is kid | accepted + needs_review | pinned kid population denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Adult Mortality Rate | death only where age class is adult | accepted + needs_review | pinned adult population denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Breed-wise mortality | death only by breed | accepted + needs_review | pinned breed population denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Farm-wise mortality | death only by farm | accepted + needs_review | pinned farm population denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Load-wise mortality | death only by load | accepted + needs_review | pinned load population denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Gender-wise mortality | death only by gender/sex | accepted + needs_review | pinned gender denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Status-wise mortality | death only by animal/status tag | accepted + needs_review | pinned status denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Housing/shed-wise mortality | death only by shed/housing label | accepted + needs_review | pinned housing denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Delivery/litter mortality | death and/or abortion, only as pinned by legacy formula | formula-specific | pinned denominator from legacy formula | period-specific | legacy mortality SQL | to be pinned |
| Trends | death only by month plus rate denominator where shown | accepted + needs_review | pinned monthly denominator from legacy formula | month bucket | legacy mortality SQL | to be pinned |

If a formula row remains `to be pinned`, that metric is not ready to implement.

Total Deaths must not include abortion unless the legacy formula explicitly
proves it does. Delivery/litter charts may show abortion as a separate series;
that does not automatically make abortion part of Total Deaths.

## Event Type Taxonomy

Initial event types:

- `death`: a death/mortality event counted by death totals and mortality rates.
- `abortion`: an abortion/pregnancy-loss event shown only in charts whose
  formula explicitly includes abortion.

Do not add catch-all event types such as `mortality_related` without pinning
which formulas include them. If a source row is ambiguous, keep it in
`mortality_source_rows` and open review instead of placing it into an event type
that can inflate totals.

## Cross-Module Denominators

Population denominators must not be recomputed by scanning goats from Mortality
request handlers.

Where a Mortality rate uses active-goat population, the denominator must come
from the identity/count projection at a pinned watermark or projection version.
Mortality freshness is then composed:

```text
mortality freshness = min(mortality event/source freshness,
                          denominator projection freshness)
```

The API response must expose unavailable/stale denominator sources in the same
freshness envelope as Mortality source availability.

Rate rollout depends on those denominator projections being implemented and
fresh at a pinned watermark. If identity/count denominator projections are not
ready, affected rates may appear only in internal/dev as pending or
source-unavailable and block production completion; they must not be computed
from ad hoc Mortality queries.

## Location Alias Resolution

Mortality must resolve every legacy farm, shed, housing, and status-location
label through the Locations alias resolver using a mortality-specific source
context such as `legacy_bq_mortality`.

Projection rows may cache resolved canonical location IDs and display labels,
but the canonical mapping remains owned by Locations. Unresolved labels create
Location review items or Mortality review items linked to the unresolved alias;
they do not create one-off private mappings inside Mortality.

Phase 1 must preserve the legacy `farm=CBE/CPT` semantics: those labels resolve
to seeded park-scope rows, not duplicate canonical `location_type='farm'` rows.

## Android Death SOP Cutover

The future canonical write path is:

```text
Android Death SOP -> Goat OS API -> backend validation/idempotency
  -> mortality_events + proof/audit/outbox -> projection rebuild
```

The adapter must follow the form-engine proof and correction model for Death
reports, including idempotent app submission, void/reversal behavior, and proof
policy. Slack or sheet automation may be bridged temporarily, but it must submit
through the same backend APIs or a backend-owned adapter.

Android and legacy sources must share source-independent logical event keys so
the same real-world death is not counted twice when both sources are live.
Canonical Android events win for a grain only after the cutover contract marks
coverage complete; until then legacy can fill uncovered grains.

## Blend Mode And Coverage State

Mortality must support mixed-source operation during cutover:

```text
canonical events win when canonical coverage is complete for the grain
legacy fills gaps while canonical coverage is incomplete
same logical death/abortion event must not be served twice
conflicting overlap opens review
```

The minimum coverage grain is tenant + period window + section + metric +
resolved dimension key. If the grain depends on denominator data, the coverage
state must also include the denominator source/version. A tenant-wide
legacy/canonical flag is not enough because Death SOP submissions, historical
BQ deaths, load denominators, litter denominators, and location aliases can
cut over at different times.

Projection rows and projection state must expose `source_composition` so the UI
and parity artifacts can distinguish `legacy_only`, `canonical_only`, and
`blended` sections.

## Adjacent Ownership

Load labels and procurement-load identifiers belong to the procurement/load
owner once that module exists. Delivery, litter, abortion, and birth-derived
denominators belong to the birth/kidding or forms owner once that module exists.
Until those modules are implemented, Mortality may consume pinned legacy
denominator/reference sources but must document the owner-to-be and avoid
creating permanent private master data.

## Proposed Postgres Ownership

Module ownership:

```text
backend/internal/mortality owns mortality source, fact, projection, and review
logic.
backend/internal/legacy_sync may own shared sync run/source status if reused.
backend/internal/permissions owns grants.
backend/platform/audit owns audit_log writes through public APIs.
```

Do not write another module's tables directly from Mortality services. Use
published package APIs or explicit interfaces for identity lookup, permissions,
legacy sync state, audit, and outbox.

## Tables

Use module-owned tables. Names may change during implementation, but the shape
must preserve this contract.

### mortality_source_rows

Stores source evidence from BQ/Sheets with idempotency and replay support.

Required columns:

- tenant_id
- source_system, for example `legacy_bigquery` or `legacy_sheet`
- source_table
- source_row_key
- source_observed_at
- source_watermark
- payload_json
- payload_hash
- row_status: current, superseded, invalid, ignored
- sync_run_id
- created_at
- superseded_at

Indexes:

- unique current row by tenant/source/row key when active
- tenant/source table/source observed date
- payload hash for replay drift checks

### mortality_events

Stores canonical event facts used by projections.

This table is required. Projections must read from this table, not directly from
`mortality_source_rows`.

Required columns:

- tenant_id
- mortality_event_id
- logical_event_key
- event_type: death, abortion
- event_date
- goat_id nullable
- source_goat_identifier nullable
- source_identifier_kind nullable
- age_class: kid, adult, unknown
- breed_key, breed_label
- farm_key, farm_label
- canonical_farm_location_id nullable
- canonical_park_location_id nullable
- canonical_shed_location_id nullable
- canonical_housing_location_id nullable
- load_key, load_label
- delivery_key, delivery_label
- sex nullable
- source_row_id
- event_hash
- review_status: accepted, needs_review, rejected, superseded
- idempotency_key
- created_at / updated_at

Indexes:

- tenant_id, event_date
- tenant_id, event_type, event_date
- tenant_id, goat_id, event_date
- tenant_id, review_status
- unique tenant_id, idempotency_key
- unique tenant_id, logical_event_key when active
- source row uniqueness

Source idempotency key:

```text
source_system + source_table + source_row_key + event_type
```

If a legacy source lacks a stable row ID, derive a deterministic key from the
minimum safe composite needed to avoid double-counting:

```text
source_system + source_table + event_date + source_goat_identifier
  + load/farm/breed context + event_type + source_row_hash
```

Do not collapse two distinct unmatched deaths into one event merely because both
lack a goat_id. Do not create a second event on re-sync for the same source
death row.

`idempotency_key` prevents repeated imports from the same source from creating
duplicates. `logical_event_key` prevents cross-source duplicates during cutover,
for example when a legacy BQ row and an Android Death SOP submission describe
the same real-world death. If a logical key conflict has meaningful dimension or
proof differences, keep one active event according to the cutover contract and
open review for the disagreement.

### mortality_projection_rows

Serves dashboard charts and summary pills.

Required shared contract:

- tenant_id
- period: overall, this-month, month-wise
- period_start nullable
- period_end nullable
- section: summary, breed, farm, load, delivery, trends
- grain: total, breed, farm, load, delivery, month, age_class, sex, status,
  housing
- dimension_key
- dimension_label
- metric_key
- numerator nullable
- denominator nullable
- value numeric
- unit: count, percent, ratio
- sort_order
- projection_version
- sync_run_id
- source_hash
- source_composition: legacy_only, canonical_only, blended
- created_at / updated_at

Indexes:

- tenant_id, period, section, grain, sort_order
- tenant_id, period, section, metric_key
- tenant_id, projection_version

### mortality_projection_state

May be a mortality-specific table or a row in a shared projection-state table.
It must expose the standard freshness envelope.

Required fields:

- tenant_id
- module = mortality
- last_successful_run_id
- last_success_at
- source_watermark
- projection_version
- freshness_status: green, yellow, red, unknown
- serving_state: never_synced, fresh, stale, rebuilding, failed,
  source_unavailable
- source_composition: legacy_only, canonical_only, blended
- row_count
- conflict_count
- unavailable_sources
- rebuild_required
- last_error
- updated_at

## Sync/Rebuild Flow

Manual sync is the v1 operating mode.

Flow:

1. Verify caller has sync permission.
2. Acquire tenant/module lock.
3. Probe all required legacy BQ/Sheets sources.
4. Load source rows into staging.
5. Compute row hashes and validate required columns.
6. Upsert `mortality_source_rows` idempotently.
7. Resolve legacy location labels through Locations aliases.
8. Convert event-creating source rows into `mortality_events`.
9. Compute source idempotency keys and source-independent logical event keys.
10. Deduplicate across legacy and canonical sources using the cutover contract.
11. Link events to Goat OS identity where deterministic.
12. Open or reuse review items for unresolved/ambiguous events.
13. Verify event-source coverage for every shipped rollup grain.
14. Rebuild affected projection rows with source composition.
15. Update projection state and freshness envelope.
16. Write audit/outbox entries.

The command must dry-run by default if exposed as a CLI. Any mutation path must
require an explicit execute flag or API action.

Projection publication must be atomic per tenant/module/run. A failed rebuild
must not leave a mixed set of old and new projection rows visible as fresh.

## Rebuild Strategy

V1 may use a full mortality rebuild because the current source volume is small.
The design must still support scoped rebuild:

- tenant
- period
- section
- source watermark
- changed source rows
- changed event facts
- changed projection version

At one million goats, dashboard requests still read bounded projection rows.
Large raw fact scans are allowed only inside controlled rebuild/replay jobs, not
inside request handlers.

## API Contract

Initial API:

```text
GET /analytics/mortality/dashboard?period=overall|this-month|month-wise
```

This endpoint returns a composed dashboard payload with `summary` and named
`sections`. If the implementation later splits reads for performance, the split
endpoints must still include `period`, `section`, and `grain` filters matching
the projection table contract.

For `period=month-wise`, the default response window is the latest 24 complete
month buckets plus the current month when present. Wider windows require an
explicit `from_month` / `to_month` request and must still cap each section with
bounded projection reads.

Response shape:

```json
{
  "freshness": {
    "as_of": "2026-06-18T00:00:00Z",
    "freshness_status": "green",
    "serving_state": "fresh",
    "stale": false,
    "rebuild_required": false,
    "source_watermark": "2026-06-18",
    "unavailable_sources": [],
    "conflict_count": 0,
    "projection_version": 1,
    "source_composition": "legacy_only"
  },
  "summary": [],
  "sections": {
    "breed": [],
    "farm": [],
    "load": [],
    "delivery": [],
    "gender": [],
    "status": [],
    "housing": [],
    "trends": []
  }
}
```

Do not expose BigQuery table names to the frontend.

Shared freshness envelope:

```text
freshness_status = green | yellow | red | unknown
serving_state = never_synced | fresh | stale | rebuilding | failed | source_unavailable
source_composition = legacy_only | canonical_only | blended
```

`conflict_count` is separate from `freshness_status` and `serving_state`. A
fresh projection can still have open conflicts if the feature intentionally
preserves unresolved events in totals while surfacing review work.

Absent projection rows must not be treated as zero. The read path must join or
load projection state first:

- no successful projection for bucket -> `serving_state` is `never_synced` or
  `source_unavailable`
- successful projection with value 0 -> real zero

Potential admin API:

```text
POST /admin/mortality/sync-runs
GET /admin/mortality/sync-runs/{run_id}
```

These endpoints require admin/sync permissions and must be audited.
Every sync run must record the authenticated actor, tenant, request trace, and
source set in audit/run metadata.

## RBAC Contract

Required permissions, final naming subject to RBAC package review:

- `analytics.mortality.read`
- `analytics.mortality.sync`
- `analytics.mortality.review`

Initial role mapping:

- `ceo_internal`: read, review
- `park_head`: read
- `verifier`: read, review
- `admin`: read, sync, review
- `operator`: no dashboard read unless explicitly granted later

Do not authorize by email inside request handlers. SSO email allowlists may
control who can create a session, but backend authorization must use DB grants.

## Frontend Contract

Admin-web route:

```text
/dashboard/mortality
```

Requirements:

- match legacy UI layout, spacing, tabs, colors, and chart ordering as closely
  as practical
- use Goat OS generated API client or a typed server API wrapper
- show freshness/status in a compact way without replacing chart content
- show source unavailable and rebuild-required states honestly
- no static chart data except in isolated tests/stories
- no BigQuery/Sheets/Drive calls

Visual acceptance requires screenshots:

- legacy full-page screenshot
- new Goat OS full-page screenshot
- comparison notes with mismatches and intentional differences

## Numeric Parity Plan

Numeric parity is separate from screenshot parity.

Required artifact:

```text
.codex-goatos-render/mortality-parity/<timestamp>/comparison-notes.md
.codex-goatos-render/mortality-parity/<timestamp>/canonical-shadow-comparison.md
```

The artifact must include value-by-value rows:

```text
section | metric | dimension | legacy_value | goatos_value | diff | status | reason
grain | logical_event_key | legacy_sources | canonical_sources | decision | status | reason
```

If the legacy dashboard API cannot be reached, extract the legacy SQL/formulas
from the legacy code and run the same formulas against the same snapshot/source
instead. Do not trust only the six headline pills.

Allowed status values:

```text
match
explained_delta
unexplained_delta
pending_source_coverage
```

Known-correct differences must be recorded as `explained_delta`, for example a
Goat OS event total preserving an unmatched historical death that legacy dropped
because identity linkage was unresolved. Any `unexplained_delta` blocks feature
completion. Any `pending_source_coverage` blocks production completion for a
required Mortality section.

Once Cube owns `mortality_rate`, add projection-vs-Cube parity on a fixed
snapshot before calling the metric governed.

The canonical shadow artifact is required before removing any BQ/Sheets source
from a grain. It must show that canonical Android/backend events and denominator
projections either match the legacy oracle or have explained deltas, and that
cross-source dedup prevents duplicated deaths during blended operation.

## Performance Requirements

Hot API reads:

- no raw all-history `GROUP BY`
- no full-herd scans
- bounded projection reads with tenant and section filters
- p95 target under 300 ms in dev-scale data and designed for one million goats

Sync/rebuild jobs:

- chunked reads/writes
- source hashes for idempotency
- explicit transaction boundaries
- safe retry behavior
- no silent partial projection publish

`mortality_events` does not need identity-style partitioning in v1. Death event
cardinality is expected to be hundreds or thousands, not one million hot rows.
The one-million-goat scale risk is in population denominators, which must come
from indexed identity/count projections. If mortality events later grow enough
to need partitioning, add a migration/ADR then.

Indexes must be part of the migration. Query plans for hot reads and expensive
rebuild writes must be reviewed before live deploy.

## Observability

Record:

- sync started/completed/failed
- source probe failures
- source row counts
- fact row counts
- projection row counts
- conflict counts
- rebuild duration
- API latency
- unavailable sources
- stale projection responses

Auth/session audit is separate from Mortality audit. Mortality sync and review
actions still need their own audit entries.

Outbox rows may be written for future warehouse/Cube egress, but no v1 outbox
consumer is required for Mortality dashboard correctness.

## Test Plan

Backend:

- source probe and unavailable-source handling
- idempotent source row upsert
- idempotent death-event creation on repeated sync
- source-independent logical event key prevents legacy + Android double count
- event creation from source row
- event-source coverage gate blocks complete sections when only rollup oracle is
  available
- Locations alias resolution for farm, shed, housing, and status-location
  labels using `legacy_bq_mortality`
- unresolved event creates/reuses review item
- projection rebuild for summary, breed, farm, load, delivery, trends
- API freshness envelope
- shared `freshness_status`, `serving_state`, and `source_composition` fields
- absent projection row does not render as zero
- denominator projection unavailable marks Mortality stale/unavailable
- RBAC allow/deny

Frontend:

- route renders all tabs/sections
- empty/stale/source-unavailable states
- screenshot comparison against legacy
- no text overflow or uneven grid/card layout

Replay/parity:

- numeric legacy-vs-GoatOS comparison artifact
- canonical-vs-legacy shadow comparison artifact
- same-source rerun idempotent
- cross-source rerun deduplicates the same logical death
- changed-source replay does not silently keep stale facts

## Rollout Gates

1. PRD/TRD accepted.
2. Legacy formulas and source list pinned, including kid/adult cutoff,
   denominator source for every rate, and source classification as
   event-creating, denominator/reference, or rollup/parity-only.
3. Event-source coverage proves all required rollup totals can be reproduced
   from event-creating sources.
4. Required BQ/Sheets/Drive source access probed and classified.
5. Locations aliases resolve all legacy Mortality farm/shed/housing/status
   labels needed by shipped sections.
6. Identity/count denominator projection source is pinned for every shipped
   mortality rate.
7. Migrations and API contracts added with `/analytics` read routes,
   `/admin` sync routes, and the shared freshness envelope.
8. Backend sync/rebuild/API tests green.
9. Frontend visual and responsive checks green.
10. Numeric parity artifact reviewed.
11. No required Mortality section remains pending, migration-only, or
    source-unavailable for production completion.
12. Canonical-vs-legacy shadow parity and cross-source dedup tests reviewed
    before any BQ/Sheets source is removed from a grain.
13. Screenshots compared legacy vs Goat OS.
14. Deploy code only.
15. Run manual sync only after source credentials and replay gates are green.

If any required Drive-backed source remains blocked, the affected section must
remain internal/dev pending or source-unavailable, not zero. It blocks
production completion until resolved or explicitly removed from required scope by
product decision.

Scheduled sync remains off until dirty old-snapshot-to-live replay reaches zero
goat-level drift.

## Open Questions

- Which legacy tables are Drive-backed and require special export credentials.
- Which internal implementation order gets to full required legacy scope fastest;
  production completion still requires all required Mortality sections.
- Whether Mortality review items reuse an existing Import Review surface or need
  a Mortality-specific review queue.
- Which module owns load master data during the temporary legacy period and
  after Android cutover.
- Which module owns delivery, litter, abortion, and birth event facts before
  the birth/kidding/forms modules are fully implemented.
