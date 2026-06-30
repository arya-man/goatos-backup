# Counts/Shifting Closure - TRD

**Status:** Draft v1, technical design sketch for Feed Direction gate `G2`
**Date:** 2026-06-30
**Companions:** [Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md),
[Feed Direction Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md), and
[Feed Direction Build-To-Done Goal](./BUILD-TO-DONE-GOAL.md)

## 1. Technical Goal

Provide a Counts/Shifting application contract that Feed Direction can consume
without reading raw Counting DB rows, raw Sheets, or per-goat history scans.

Source authority for feed-facing count behavior is
`Feed, Shiftings and Count.docx` v1.1. It controls the one-day projection,
physical Base Count adoption, Diff source semantics, and any Feed Direction
timing assumptions. Legacy App Script/Sheet timing is audit evidence for cutover,
not a GoatOS schedule unless explicitly approved against that docx.
`Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx` are
fixture/import evidence only. Their tabs, formulas, comparison sheets, processed
flags, and script transforms are not Counts/Shifting runtime truth.

```text
physical Base Count anchor
  -> append-only ShiftingEvent ledger
  -> structured cohort/stage impacts
  -> realized count_as_of
  -> one-day projected_count_for
  -> projection exceptions and audit
```

## 2. Application Port

```go
type CountProjectionProvider interface {
    CountAsOf(ctx context.Context, req CountAsOfRequest) (CountProjection, error)
    ProjectedCountFor(ctx context.Context, req ProjectedCountRequest) (CountProjection, error)
}
```

Request shape:

```text
tenant_id
park_id
as_of or target_date
grain: shed + breed
reviewed_ration_context_resolution: resolved | blocked
horizon: realized | one_day_projection
consumer: feed_direction
consumer_version / protocol_version_id
```

Response row shape:

```text
tenant_id
park_id
shed_id
breed_id
ration_context_resolution_state
resolved_ration_context_ids nullable
ration_context_blocker_reason nullable
head_count
projection_horizon
source_contract_version
source_hash
base_count_anchor_id
included_shifting_event_ids_hash
exception_count
```

The provider must fail closed. If source data is insufficient, it returns a
typed blocker/exception instead of silently dropping or inventing counts.

Implementation status as of 2026-06-30:

- `backend/internal/counts` now exposes the provider shape over immutable
  `count_projection_snapshots` and bounded `count_projection_snapshot_rows`.
- `CountAsOf` reads `horizon='count_as_of'`; `ProjectedCountFor` reads
  `horizon='feed_target_date'`.
- `RecomputeProjectionSnapshot` now builds immutable snapshots from bounded Base
  Count anchors plus horizon-filtered ShiftingEvent impacts. `count_as_of`
  includes only applied events; `feed_target_date` includes authorized/applied
  events effective on the target date.
- `backend/cmd/counts-projection-recompute` provides the scheduler-facing
  one-shot worker entrypoint for tenant + park + as-of + target-date bounded
  recompute of `count_as_of`, `feed_target_date`, or both horizons.
- New Base Count anchors and ShiftingEvents emit idempotent transactional
  outbox events (`counts.base_count_anchor.recorded` and
  `counts.shifting_event.recorded`) with tenant/park/shed visibility, giving the
  recompute worker a durable invalidation signal.
- `countsapp.ProjectionInputHandler` consumes those events through both
  `backend/cmd/domain-event-consumer` and the local/dev `outbox-relay`
  in-process eventbus publisher. It recomputes bounded `count_as_of` and
  `feed_target_date` snapshots for the event day and each affected park,
  including both source and destination parks for cross-park shifting.
- `count_dimension_aliases` stores reviewed, source-evidenced aliases for
  Counts dimensions (`breed`, `stage_tag`, `age_class`, `sex`, `shed_tag`).
  Projection recompute resolves approved breed/stage aliases before Feed sees
  rows; unreviewed aliases emit open `alias_conflict` exceptions and keep the
  affected row blocked.
- `count_projection_exceptions` now carries work metadata (`work_type`,
  `work_state`, `due_at`, `next_action`, `evidence_link`) and repeated open
  exceptions relink to the latest snapshot on upsert. This makes G2 blockers
  owner/action visible to Feed reads instead of stranded on stale snapshots.
- Physical Base Count adoption now reconciles the new anchor against the
  previous adopted shed + breed anchor plus applied ShiftingEvents in the
  bounded window. Unexpected deltas create open `unreported_shifting` or
  `count_mismatch` exception work and move the new anchor to
  `discrepancy_state=investigating` without blocking adoption.
- `ResolveProjectionException` now closes Counts/Shifting exceptions through an
  idempotent reviewed workflow. Each `resolve` or `dismiss` action writes
  `count_projection_exception_resolutions`, updates the exception `status`,
  `work_state`, `resolved_at`, actor/ref/reason fields, and clears the linked
  Base Count discrepancy when the exception came from a physical-count mismatch.
- `ListProjectionExceptions` now exposes a bounded, status/location/filter
  queue over exception work with keyset cursor pagination. This is the read
  path operators and later command lenses use to find high-risk blockers such
  as shifted pregnant destination shortages, alias conflicts, and count
  mismatches without scanning herd-level source data.
- `POST /feed-direction/counts-projection/exceptions/{exception_id}/resolve`
  and `/dismiss`, plus `GET /feed-direction/counts-projection/exceptions`,
  expose that review path through the protected Feed Direction API with
  route-permission registration and OpenAPI/client contract coverage. The list
  route is read-only; resolve/dismiss requires authenticated actor,
  `Idempotency-Key`, reason, and optional resolution reference.
- Projection rows now carry `base_count_anchor_id`,
  `included_shifting_event_ids_hash`, source row hash, contract hash, and
  ration-context resolution state.
- Shifted pregnant, lactating, and warm-up impacts are projected into the
  destination shed row. If the destination ration context is unresolved, the row
  stays blocked and high-risk movement emits a critical `destination_shortage`
  exception so Feed cannot generate normal shed-average quantities.
- Missing snapshots return an explicit `missing_projection_snapshot` blocker
  instead of empty success.
- A recompute with no adopted Base Count anchors writes a blocked snapshot with
  `missing_base_count`, not an empty ready snapshot.
- `GET /feed-direction/readiness` is wired to the Counts/Shifting readiness
  provider so `CSG1`-`CSG10` can move independently under Feed gate `G2`.
- This does **not** close `G2`: source import/adapters, owner-approved alias
  mapping coverage/admin review, scheduled/import-wide mismatch scans, shared
  command-lens UX/outbox fanout, observability, query-plan/synthetic-scale
  proof, and seeded local E2E remain blockers.

## 3. Candidate Persistence

Preferred aggregate-ledger tables:

| Table | Purpose |
| --- | --- |
| `count_base_anchors` | Physical count anchor by tenant, park, shed, breed, counted_at, actor/source/evidence hash |
| `shifting_events` | Append-only movement header with logical key, priority, category, source/destination, raised/effective time, authorization, proof, verification, status |
| `shifting_event_impacts` | Structured stage/cohort deltas; no free-text comments as policy truth |
| `count_projection_snapshots` | Optional cached projection by tenant, park, date/as_of, grain, horizon, source hash |
| `count_projection_exceptions` | Durable process work for ambiguous impact, unreported shifting, count mismatch, alias conflict, or insufficient source |
| `count_projection_exception_resolutions` | Idempotent reviewed `resolve`/`dismiss` audit for projection exception work |

Alternative per-goat derivation is allowed only after RFID-to-shed association
can be proven from committed GoatOS identity/location state. The derivation must
use active `goat_identifiers`, authoritative `goat_location_history`, and
reviewed location/stage reference data to produce the same aggregate output
without full-herd scans. Even then, Feed still consumes this port and snapshot
shape, and unresolved RFID, identifier, or location confidence fails closed.

Initial closure must use aggregate ledger/projection output at shed + breed
grain. Ration context must be resolved from reviewed shed/cohort reference data
or returned as a blocker; it is not a separate Base Count grain. Feed Transfer KT
constraint tables may define breed/tag/energy nutrition cohorts without shed
placement, so they cannot by themselves prove the count-row ration context.
Feed Directions Automation workbook tabs may show how the legacy sheet tried to
join counts to validation/supply-planning formulas, but that join logic belongs
in a reviewed Feed resolver and protocol config, not in Counts/Shifting.
Per-goat derivation is a future replacement only after RFID-to-shed is
implemented, confidence-gated, scale-tested, and owner-approved.

## 4. Horizon Rules

`CountAsOf(as_of)` includes only the configured realized/applied state.

`ProjectedCountFor(target_date)` may include authorized/directed
future-effective ShiftingEvents whose `effective_at` falls inside the target
date. Pending unauthorized, rejected, canceled, unresolved, or free-text-only
events remain visible process work and do not change quantities.

A new physical Base Count becomes the next anchor immediately. The discrepancy
between replayed count and physical count becomes investigation work; it does not
block the physical anchor.
Base Count cadence is reviewed policy or schedule config. Source history moved
from roughly weekly to roughly monthly physical counts, so do not hardcode a
fixed weekly or monthly cadence in the worker/projection model.

## 5. Idempotency And Replay

Required keys:

```text
base_count_anchor: tenant + park + shed + breed + counted_at + source_hash
shifting_event: tenant + logical_shifting_event_key
shifting_impact: tenant + shifting_event_id + grain_key
projection_snapshot: tenant + horizon + park + date/as_of + grain + source_hash
projection_exception: tenant + exception_type + source_key + grain_key
```

Legacy 3-day applied-event dedupe and file-id dedupe are parity clues only.
GoatOS requires durable keys, replay tests, and no double-application after
redelivery or import reruns.

## 6. Alias And Source Normalization

Counts/Shifting must normalize breed and stage/tag aliases before projection
output is consumed by Feed. Known review items include:

- SIROHI -> Beetal or the approved replacement alias mapping.
- F2/Fattening stage aliases.
- Warmup stage tags and their approved ration/projection handling.
- K0 Mother-vs-Kid ambiguity; unresolved cases fail closed.

Aliases must carry source reference, review_status, reviewer, effective range,
and approval metadata when they affect count or ration selection.

Implementation status as of 2026-06-30:

- Reviewed aliases live in `count_dimension_aliases`; no legacy workbook alias
  is approved automatically.
- Breed normalization first checks approved Counts aliases, then active canonical
  `breed_aliases` for pass-through labels such as Beetal.
- Stage/tag aliases require approved Counts alias rows. Values such as K0,
  F2/Fattening, Warmup, Pregnant, and Mother/M0 remain blocked until reviewed
  for the specific Feed Direction source context.
- Unreviewed aliases do not get silently "fixed"; recompute produces
  `alias_conflict` exceptions and includes those exceptions in the projection
  source hash.

## 7. Exception Work

Counts/Shifting creates durable process-exception work for:

- missing structured cohort/stage impact;
- unreported shifting or count mismatch;
- alias conflict;
- insufficient source rows for a requested grain;
- replay conflict where the same logical event has a different payload;
- projection stale because Base Count or ShiftingEvents changed after snapshot.

Implementation status as of 2026-06-30:

- Projection exceptions are durable and expose owner/action work metadata:
  `owner_ref`, `work_type`, `work_state`, `due_at`, `next_action`, and
  `evidence_link`.
- Critical exceptions are due immediately from the projection `as_of`; blocking
  exceptions default to a two-hour deadline; warning exceptions default to a
  one-day deadline.
- Open exception upsert moves repeated exceptions to the newest snapshot so a
  latest Feed projection cannot appear clean because the blocker is still
  attached to an older snapshot.
- Backend exception review now supports a bounded list/read route plus
  idempotent `resolve` and `dismiss` actions with actor, reason, optional
  resolution reference, audit row, closed exception state, and linked Base Count
  discrepancy cleanup when applicable.
- The protected Feed Direction API now exposes the exception queue and close
  actions, registered in the route-permission matrix and OpenAPI. Full
  command-lens subscription, outbox event fanout, assignment policy, and
  review-surface UX remain G2 blockers.
- New physical Base Count anchors compare against the previous adopted anchor
  plus applied shifting ledger net for the same tenant, park, shed, and breed.
  A matching delta stays clean; an unexplained delta creates `unreported_shifting`
  or `count_mismatch` work and the Feed projection read includes that open
  tenant/park exception even before it is attached to a recomputed snapshot.

Feed generation consumes the exception count/hash and blocks or narrows
generation according to policy; it does not hide the problem.

## 8. Worker And Projection Shape

Required paths:

| Path | Trigger | Behavior |
| --- | --- | --- |
| Base Count import/record | API/import | Write anchor, emit idempotent `counts.base_count_anchor.recorded` outbox event, audit, invalidate projections |
| Shifting event ingest | API/import/outbox | Upsert event and impacts, emit idempotent `counts.shifting_event.recorded` outbox event, audit, invalidate projections |
| Projection recompute | Scheduler/outbox | Run `backend/cmd/counts-projection-recompute` or the registered `countsapp.ProjectionInputHandler` consumer for tenant + park + as-of + target-date bounded horizons; write snapshot or exception |
| Count mismatch scan | Base Count adoption plus scheduler/import compare | On new physical Base Count, compare previous adopted anchor + applied shifting net and create `unreported_shifting`/`count_mismatch` work for unexpected deltas. Scheduled/import-wide scan remains to catch stale historical gaps. |
| Feed projection read | API/app port | Return rows or typed blocker with source hash |

All workers must be tenant/park/date/grain bounded and replay-safe.

Worker observability is part of `CSG10`, not a future ops cleanup. Counts/Shifting
must expose or emit base-count import latency, shifting ingest latency,
projection recompute latency, stale-projection age, queue/outbox lag, retry
counts, DLQ counts, exception counts by type, and query-plan failures. Those
signals roll into Feed gate `G2` and the same monitoring slice used by Feed
generation workers.

## 9. Readiness Roll-Up

`CSG1`-`CSG10` are the subgate contract for Feed gate `G2`. A readiness adapter
must return each subgate with status, owner, evidence pointer, blocker reason,
and last_checked_at so `GET /feed-direction/readiness` can show the breakdown
instead of a single opaque Counts/Shifting status.

## 10. Tests

Non-negotiable tests:

- Base Count adoption despite discrepancy investigation.
- Base Count cadence read from policy/config instead of a hardcoded interval.
- ShiftingEvent idempotency by logical key.
- Same key/different payload conflict.
- Missing structured impact fails closed.
- K0 ambiguity fails closed.
- Alias normalization before projection output.
- Realized vs one-day projection split.
- Future-effective authorized move included only in projection horizon.
- Pending/rejected/canceled/unresolved movement excluded.
- Shifted pregnant/lactating/warm-up destination rows fail closed with
  `destination_shortage` until reviewed destination ration context exists.
- Count mismatch/unreported shifting creates exception work.
- Projection snapshot source hash changes after input change.
- Feed consumes immutable projection snapshot and does not mutate past runs.
- Initial Feed projection output stays aggregate shed + breed grain, with ration
  context either resolved from reviewed source-backed context or blocked with an
  explicit reason, and does not depend on RFID-to-shed per-goat derivation.
- Query-plan checks for widest allowed projection/read paths.
- Worker observability checks for latency, lag, retry, DLQ, exception, and
  stale-projection metrics.
- Seeded local E2E coverage for Base Count, realized ShiftingEvent, one-day
  projection, fail-closed exception, and Feed generation consumption of the
  immutable projection snapshot.
- High-effort review-agent findings from
  [BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md) are resolved or explicitly
  owner-deferred before Counts/Shifting can turn `G2` green.
