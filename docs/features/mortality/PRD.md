# Mortality PRD

Status: draft for implementation review.

## Summary

Mortality brings the legacy mortality dashboard into Goat OS as a backend-owned,
Postgres-served dashboard.

The frontend should visually match the legacy Mortality screen, but the runtime
data path must not copy the legacy implementation. Legacy BigQuery and Sheets
are temporary upstream sources during migration. Goat OS owns ingestion,
validation, projection, freshness, audit, RBAC, and API serving.

Core rule:

```text
Legacy BQ/Sheets -> Goat OS backend sync -> Postgres facts/projections
  -> Goat OS API -> admin-web Mortality UI
```

Never:

```text
admin-web -> BigQuery / Sheets / Drive / raw database
```

## References

- `docs/decisions/high-scale-dashboard-projections.md`
- `docs/features/cutover-contract.md`
- `docs/features/locations/PRD.md`
- `docs/features/locations/TRD.md`
- `context/analytics/final-analytics-infra.md`
- `context/forms/final-forms-sop-engine.md`
- Legacy UI reference:
  `https://dashboard--goatos-sheets.us-central1.hosted.app/mortality`
- Legacy code reference, read-only:
  `/Users/ravi/mesha/dashboard/app/(dashboard)/mortality/page.tsx`
- Legacy API/query reference, read-only:
  `/Users/ravi/mesha/dashboard/app/api/mortality/`

## Product Goals

- Match the legacy Mortality screen closely enough that users recognize the same
  workflow and numbers.
- Serve the dashboard from Goat OS backend APIs, not from browser-side BigQuery
  or Sheets access.
- Keep page reads fast at one million goats by reading projection rows, not by
  scanning facts on every request.
- Make source freshness and unavailable sources visible on the page.
- Preserve event-based mortality semantics from the legacy dashboard.
- Produce numeric and screenshot parity artifacts before calling the feature
  complete.
- Keep the feature safe while Goat OS is still syncing from BQ/Sheets and before
  Android SOP forms become canonical writes.

## Non-Goals

- Do not enable 15-minute scheduled legacy sync for Mortality v1.
- Do not make BigQuery, Sheets, Drive, Redis, Cube, or a frontend app the
  runtime source of product truth.
- Do not refill or mutate Google dev data as part of a UI-only build.
- Do not force Mortality death counts to equal the Counts page dead-passport
  total.
- Do not invent missing death, breed, load, farm, or denominator values when a
  source is unavailable.
- Do not ship fake chart parity by using static frontend data.

## Users

```text
CEO/internal user
  reviews mortality headline metrics, trend movement, breed/farm/load patterns,
  and operational hotspots.

Data reviewer/admin
  checks source freshness, unresolved mortality events, denominator issues, and
  review queues before accepting numbers.

Operations lead
  uses Mortality charts to understand where intervention or SOP follow-up is
  needed.
```

## Metric Semantics

Mortality numbers are event-based.

This is intentional:

```text
Mortality Total Deaths = all-time death/mortality events.
Counts page dead = current goat passports whose lifecycle is dead.
```

Those two numbers are not expected to match. Do not reconcile Mortality Total
Deaths by counting `goats.lifecycle = 'dead'`.

Reason:

- A death event can exist even if the exact current passport is unresolved.
- Historical death and abortion events are lifetime operational facts.
- The Counts page is a current-state identity/passport view.
- Legacy dashboard mortality rates are based on event facts and their own
  denominators, not only current passport state.

Every rate must retain:

- numerator
- denominator
- final value
- unit
- source evidence or source hash

Before implementation starts, the feature must add a Mortality Formula Register
to the TRD. That register must pin, for every summary pill and chart section:

- numerator event definition
- denominator basis
- time window
- source table/query
- null/unknown handling
- whether the value is expected to match legacy exactly or through an explained
  reconciliation delta

Do not build charts from labels alone. A chart title like "Kid Mortality Rate"
is not a metric definition.

## Required Dashboard Scope

The target UI is the legacy Mortality screen:

- top navigation tabs:
  - Overall
  - This Month
  - Month-wise
- summary pills:
  - Total Deaths
  - Kid Deaths
  - Adult Deaths
  - Total Mortality Rate
  - Kid Mortality Rate
  - Adult Mortality Rate
- section tabs:
  - Breed-wise
  - Farm-wise
  - Load-wise
  - By Delivery
  - Trends
- supporting trend/analysis sections:
  - Gender-wise
  - Status-wise
  - Housing/shed-wise
- Breed-wise sub-tabs:
  - By Breed
  - Within Breed

Production completion means the full legacy-visible Mortality scope above is
implemented, numerically reconciled, visually compared, and served from Goat OS
APIs. Internal/dev builds may expose pending sections while source coverage is
being proven, but pending, migration-only, or intentionally dropped required
sections block production completion.

## Source Of Truth And Storage Decision

Mortality v1 uses these data stores:

| Layer | Store | Role |
| --- | --- | --- |
| Runtime app truth | Postgres | canonical Goat OS facts, source rows, review state, projections, freshness |
| Dashboard serving | Postgres projection tables | chart-ready rows read by Goat OS APIs |
| Temporary upstream | BigQuery and Sheets/Drive from legacy | read-only migration inputs until Goat OS SOP/mobile owns writes |
| Future upstream | Android Death SOP through Goat OS APIs | canonical write path after cutover; replaces Slack/BQ/Sheets as product input |
| Governed analytics | Cube, later | official semantic metric layer for leadership/AI/Metabase, with parity gate |
| Warehouse/history | BigQuery, later downstream | analytics history after Goat OS emits events/outbox |
| Cache/job aid | Redis, optional | short-lived API cache, job progress, locks, or rate limiting only |

Mortality v1 does not need:

- a new time-series database
- Tinybird
- Redis as canonical storage
- frontend BigQuery access
- one-off in-memory chart computation on every request

If Mortality later needs high-volume live telemetry, that must be a separate ADR.

## Android Death SOP Cutover

Mortality must follow the shared cutover contract in
`docs/features/cutover-contract.md`.

During Phase 1, legacy BigQuery and Sheets can fill missing dashboard grains.
When Android Death SOP submissions become available through Goat OS APIs,
canonical death facts win for a grain only after coverage is complete for that
grain. Legacy remains a gap filler for uncovered historical ranges until the
shadow parity gate proves it can be removed.

The Android cutover path is:

```text
Android Death SOP -> Goat OS API -> backend validation/idempotency
  -> mortality_events + proof/audit/outbox -> projections
```

The Death SOP implementation must carry the form-engine proof, correction,
void/reversal, and idempotent-submission model. Slack or sheet automation may be
bridged temporarily, but it must not become a second product write path around
the backend.

## Source Freshness And Availability

Every Mortality API response must expose freshness using the standard dashboard
envelope from `docs/decisions/high-scale-dashboard-projections.md`.

`freshness_status` is the shared traffic-light value:
`green`, `yellow`, `red`, or `unknown`. Operational states such as
`never_synced`, `fresh`, `stale`, `rebuilding`, `failed`, and
`source_unavailable` belong in `serving_state`, not in a second
`freshness_status` enum.

The UI must distinguish:

- never synced
- fresh
- stale
- source unavailable
- rebuild required
- conflicts open

If a BigQuery/Drive source is unavailable, the sync must record the unavailable
source and keep prior projections stale. It must not silently write zeros.

The UI/API must distinguish "no projection has ever run" from "the true value is
zero." Absence of a projection row is not the same as `0` deaths.

## Review And Conflict Behavior

Mortality can produce review items when source facts cannot be safely attached
to Goat OS identity or when source data conflicts with canonical facts.

Examples:

- death event exists but cannot be linked to one goat
- source gives conflicting breed/farm/load for the same event key
- denominator source is missing or ambiguous
- source row changed after a prior reviewed decision
- legacy and Android/canonical sources overlap for the same real-world death but
  disagree on dimensions, proof, or status

The review model applies to source facts and linkages. It does not mean humans
edit aggregate chart totals directly. Aggregates are rebuilt from canonical
mortality events that each metric explicitly includes.

For death totals, `needs_review` does not automatically mean "do not count."
If the event is a real death but the goat identity link is unresolved, it must
remain in Total Deaths while the identity linkage stays in review. Rejected and
superseded events are excluded.

Numeric parity with legacy is a reconciliation gate, not blind equality. If Goat
OS correctly preserves a mortality event that legacy dropped because identity
linkage was unresolved, that delta must be recorded as explained. Unexplained
deltas block completion.

## RBAC

Mortality should be visible only to authenticated, granted Goat OS users.

Initial permissions should be explicit:

- read Mortality dashboard
- trigger manual Mortality/legacy sync
- resolve Mortality review items

The implementation may map these to existing roles such as `ceo_internal` and
`admin`, but the API contract must not rely on email alone after session
creation. Email allowlists can gate SSO entry; DB grants remain authorization.

Dashboard read access must stay aligned with the other Goat OS analytics
features unless product explicitly narrows it. Counts and Mortality should not
silently drift into different read audiences for the same leadership dashboard
surface.

## Acceptance Criteria

Mortality is complete only when all of these are true:

- The PRD and TRD were read before implementation.
- The implemented data path is backend-owned and Postgres-served.
- No admin-web code reads BigQuery, Sheets, Drive, or raw DB directly.
- Legacy formulas and denominators are pinned from legacy code or legacy SQL.
- The Mortality Formula Register exists and covers every required chart/summary
  metric.
- Mortality events are stored as required canonical facts before projections are
  built.
- Every farm, shed, housing, and status-location label is resolved through the
  Locations alias resolver, including legacy mortality aliases.
- Event-source coverage proves canonical events can reproduce every required
  legacy mortality total.
- No required Mortality section remains pending, migration-only, or intentionally
  dropped for production completion.
- Rate denominators come from pinned identity/count projections or explicitly
  pinned legacy denominator sources, not from ad hoc request-time scans.
- Canonical-vs-legacy shadow parity passes for the same grain before any
  BQ/Sheets source is removed from that grain.
- Numeric parity artifact is written value-by-value.
- Numeric parity labels each delta as match, explained, or unexplained.
- Screenshot comparison artifact shows legacy vs Goat OS for the full page.
- API responses include freshness and source availability.
- Hot dashboard reads use projection tables, not raw fact `GROUP BY` scans.
- Tests cover source ingestion, projection rebuild, API contract, RBAC, and at
  least one unresolved-link review case.
- `git diff --check`, relevant backend tests, frontend typecheck/lint, and
  guardrails pass.
