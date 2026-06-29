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

## 3. Candidate Persistence

Preferred aggregate-ledger tables:

| Table | Purpose |
| --- | --- |
| `count_base_anchors` | Physical count anchor by tenant, park, shed, breed, counted_at, actor/source/evidence hash |
| `shifting_events` | Append-only movement header with logical key, priority, category, source/destination, raised/effective time, authorization, proof, verification, status |
| `shifting_event_impacts` | Structured stage/cohort deltas; no free-text comments as policy truth |
| `count_projection_snapshots` | Optional cached projection by tenant, park, date/as_of, grain, horizon, source hash |
| `count_projection_exceptions` | Durable process work for ambiguous impact, unreported shifting, count mismatch, alias conflict, or insufficient source |

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
