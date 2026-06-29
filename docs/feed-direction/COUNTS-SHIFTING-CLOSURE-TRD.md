# Counts/Shifting Closure - TRD

**Status:** Draft v1, technical design sketch for Feed Direction gate `G2`
**Date:** 2026-06-30
**Companions:** [Counts/Shifting Closure PRD](./COUNTS-SHIFTING-CLOSURE-PRD.md),
[Feed Direction Dependency Closure TRD](./DEPENDENCY-CLOSURE-TRD.md)

## 1. Technical Goal

Provide a Counts/Shifting application contract that Feed Direction can consume
without reading raw Counting DB rows, raw Sheets, or per-goat history scans.

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
grain: shed + breed + stage/tag
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
stage_tag_id
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

## 3. Candidate Persistence

Preferred aggregate-ledger tables:

| Table | Purpose |
| --- | --- |
| `count_base_anchors` | Physical count anchor by tenant, park, shed, breed, stage/tag, counted_at, actor/source/evidence hash |
| `shifting_events` | Append-only movement header with logical key, priority, category, source/destination, raised/effective time, authorization, proof, verification, status |
| `shifting_event_impacts` | Structured stage/cohort deltas; no free-text comments as policy truth |
| `count_projection_snapshots` | Optional cached projection by tenant, park, date/as_of, grain, horizon, source hash |
| `count_projection_exceptions` | Durable process work for ambiguous impact, unreported shifting, count mismatch, alias conflict, or insufficient source |

Alternative per-goat derivation is allowed only after RFID-to-shed association
and goat location history can prove the same aggregate output without full-herd
scans. Even then, Feed still consumes this port and snapshot shape.

## 4. Horizon Rules

`CountAsOf(as_of)` includes only the configured realized/applied state.

`ProjectedCountFor(target_date)` may include authorized/directed
future-effective ShiftingEvents whose `effective_at` falls inside the target
date. Pending unauthorized, rejected, canceled, unresolved, or free-text-only
events remain visible process work and do not change quantities.

A new physical Base Count becomes the next anchor immediately. The discrepancy
between replayed count and physical count becomes investigation work; it does not
block the physical anchor.

## 5. Idempotency And Replay

Required keys:

```text
base_count_anchor: tenant + park + shed + breed + stage_tag + counted_at + source_hash
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

## 7. Exception Work

Counts/Shifting creates durable process-exception work for:

- missing structured cohort/stage impact;
- unreported shifting or count mismatch;
- alias conflict;
- insufficient source rows for a requested grain;
- replay conflict where the same logical event has a different payload;
- projection stale because Base Count or ShiftingEvents changed after snapshot.

Each exception must include owner, due/deadline policy, audit, outbox, and read
model visibility. Feed generation consumes the exception count/hash and blocks
or narrows generation according to policy; it does not hide the problem.

## 8. Worker And Projection Shape

Required paths:

| Path | Trigger | Behavior |
| --- | --- | --- |
| Base Count import/record | API/import | Write anchor, audit, outbox, invalidate projections |
| Shifting event ingest | API/import/outbox | Upsert event and impacts, audit, outbox, invalidate projections |
| Projection recompute | Scheduler/outbox | Recompute bounded grains, write snapshot or exception |
| Count mismatch scan | Scheduler/import compare | Detect unexpected deltas/unreported shifting and create exception work |
| Feed projection read | API/app port | Return rows or typed blocker with source hash |

All workers must be tenant/park/date/grain bounded and replay-safe.

## 9. Tests

Non-negotiable tests:

- Base Count adoption despite discrepancy investigation.
- ShiftingEvent idempotency by logical key.
- Same key/different payload conflict.
- Missing structured impact fails closed.
- K0 ambiguity fails closed.
- Alias normalization before projection output.
- Realized vs one-day projection split.
- Future-effective authorized move included only in projection horizon.
- Pending/rejected/canceled/unresolved movement excluded.
- Count mismatch/unreported shifting creates exception work.
- Projection snapshot source hash changes after input change.
- Feed consumes immutable projection snapshot and does not mutate past runs.
- Query-plan checks for widest allowed projection/read paths.
