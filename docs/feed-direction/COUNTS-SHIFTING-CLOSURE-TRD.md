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
Base shed-tag/cohort semantics come from
`context/source-findings/goats-and-parks-source-findings.md`; Counts/Shifting
must not invent or silently alias K0/K1/K2/K3, warm-up, pregnant, lactating,
mother, milking, fattening, ICU/quarantine, buck, flushing, or breeding tags.
`Counting DB - values only.xlsx` and `Feed Directions Automation DB.xlsx` are
fixture/import evidence only. Their tabs, formulas, comparison sheets, processed
flags, and script transforms are not Counts/Shifting runtime truth.
`context/source-findings/sheds-db-source-findings.md` is legacy/manual evidence
for shed tags, capacity-like values, and potential tags. It must resolve into
reviewed, effective-dated Location/Park profile data before Counts or Feed uses
it; manual Sheds DB values are not a runtime table to copy.

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
- `count_projection_recompute_runs` records every service-level recompute run
  from the CLI worker or domain-event consumer with tenant, park, horizon,
  target date, as-of time, status, projection status, snapshot reference, row
  count, exception count, source contract, generated-by, trace id, timestamps,
  and last error. Successful runs update `CSG10` readiness evidence to
  `pending`; failed runs update it to `blocked`. This still does not make `G2`
  green without source parity, observability breadth, and seeded local E2E.
- New Base Count anchors and ShiftingEvents emit idempotent transactional
  outbox events (`counts.base_count_anchor.recorded` and
  `counts.shifting_event.recorded`) with tenant/park/shed visibility, giving the
  recompute worker a durable invalidation signal. New Base Count anchor writes
  update `CSG1` readiness to ready with `count_base_anchors:<id>` evidence and
  `CSG5` to ready because anchors are adopted immediately while discrepancy
  investigation opens owner-visible exception work instead of blocking adoption.
- New ShiftingEvent writes update `CSG2` readiness to ready with
  `shifting_events:<id>` evidence and, when structured impacts are present,
  `CSG3` to ready with `shifting_event_impacts:<id>` evidence. Missing or
  ambiguous impacts still fail closed at the service boundary and must not
  change projection truth.
- `backend/cmd/counts-source-import` accepts reviewed typed JSONL rows for
  `base_count_anchor` and `shifting_event`, then writes through the canonical
  Counts service path. It derives deterministic source hashes, payload hashes,
  idempotency keys, and request fingerprints when source review did not provide
  them; requires explicit `authorization_state` and `event_status` for imported
  ShiftingEvents; infers a pregnancy or warm-up movement category from structured
  impact counts only when the row omits category; and leaves ration context
  unresolved unless the reviewed row supplies a resolved context. A shifted
  pregnant/lactating/warm-up import therefore blocks later Feed generation via
  projection exceptions instead of guessing a destination shed quantity.
- `count_source_import_runs` records execute-mode typed import batches with
  source row count, successful Base Count rows, successful ShiftingEvent rows,
  replay count, failure count, source reference, trace id, status, timestamps,
  and last error. `counts-source-import` updates `CSG10` readiness evidence to
  `pending` on success and `blocked` on failure; success still leaves source
  parity, observability breadth, and seeded local E2E open. When a successful
  import records replayed rows, it updates `CSG8` to `pending` with
  `count_source_import_runs:<id>` evidence; this is replay proof, not full G2
  closure.
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
- Projection snapshot writes now refresh `CSG7` readiness evidence from the
  snapshot itself. If the snapshot contains an `alias_conflict`, `CSG7` becomes
  `blocked` with `count_projection_snapshots:<id>` evidence. If the snapshot is
  non-empty and has no alias conflicts, `CSG7` becomes `pending`, not `ready`,
  because full source workbook parity, Sheds DB profile-tag coverage, and
  owner-approved alias review remain required.
- `backend/cmd/counts-alias-coverage-check` checks a sanitized required-alias
  file such as `backend/testdata/counts/sheds-db-required-profile-tags.json`
  against approved `count_dimension_aliases` rows. Missing required aliases
  make `CSG7` `blocked`; complete coverage makes `CSG7` `pending`, never
  `ready`. This is how Sheds DB profile-tag coverage becomes executable
  evidence without importing the raw workbook as runtime truth.
- `backend/cmd/counts-source-parity-check` compares sanitized source parity
  fixtures, such as `backend/testdata/counts/source-parity-sample.json`, against
  the canonical `CountAsOf`/`ProjectedCountFor` read path. Missing, mismatched,
  truncated, or unexpected exact-mode rows make `CSG10` `blocked`; passing
  fixtures make `CSG10` `pending`, not `ready`.
- Locations now accepts reviewed `sheds_db` source evidence for location aliases
  and capacity records, backed by the Postgres capacity-source constraint. This
  lets Sheds DB become governed Location/Park profile data instead of a raw
  runtime spreadsheet.
- `backend/cmd/location-profile-source-import` accepts reviewed typed JSONL rows
  for `location_alias`, `location_capacity`, and `location_review_item`, defaults
  the source to `sheds_db`, derives idempotency when omitted, supports dry-run
  validation without a database, and writes through the Locations service in
  execute mode. It refuses to infer canonical locations from raw labels; unresolved
  labels stay review work.
- `count_projection_exceptions` now carries work metadata (`work_type`,
  `work_state`, `due_at`, `next_action`, `evidence_link`) and repeated open
  exceptions relink to the latest snapshot on upsert. This makes G2 blockers
  owner/action visible to Feed reads instead of stranded on stale snapshots.
- New projection snapshots update `CSG9` readiness to ready with
  `count_projection_snapshots:<id>` evidence. They also update `CSG4`: one
  horizon remains pending, and `CSG4` turns ready only after both
  `count_as_of` and `feed_target_date` snapshot horizons exist for the tenant.
- Canonical idempotent replays of Base Count anchors, ShiftingEvents, and
  projection snapshots refresh readiness evidence and update `CSG8` to
  `pending`. `CSG8` remains pending until typed source replay, projection
  recompute replay, source parity, and seeded local E2E prove no double
  application across the full path.
- Projection exception open, relink/update, and close state changes now emit
  transactional outbox events
  (`counts.projection_exception.opened`,
  `counts.projection_exception.updated`, and
  `counts.projection_exception.closed`) with tenant/park/shed visibility,
  owner, severity, due time, next action, evidence link, and resolution fields
  where applicable. This gives shared command-lens projectors a durable
  subscription source without polling Counts tables.
- The shared process-integrity read model now projects open/closed Counts
  projection exceptions as `category=feed_direction` rows for the top-level
  Action Center and Workflows APIs. Row IDs use
  `feed_projection_exception:{count_projection_exception_id}`, and
  `/action-center/obligations?category=feed_direction` plus
  `/workflows/{row_id}?category=feed_direction` can show safety blockers such
  as pregnant destination-shed shortages without creating nested Feed-owned
  command routes.
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
- `backend/cmd/counts-mismatch-scan` now exposes the scheduler/import-wide
  comparison path for stale historical/imported anchors. It is tenant-scoped,
  optional park/shed scoped, counted-at-window bounded, cursorable, and limited
  to 500 anchors per page. Each page writes `count_mismatch_scan_runs` durable
  run evidence and updates readiness: `CSG6` is ready only after a successful
  page with no next cursor, while `CSG10` remains pending until the broader
  observability, query-plan, source-parity, and seeded-E2E evidence exists.
- `000128_counts_mismatch_scan_anchor_indexes.sql` adds dedicated mismatch-scan
  anchor indexes so stale/imported Base Count comparisons have tenant/window/
  cursor bounded index paths, including optional park/shed scoped scans.
- `backend/cmd/counts-query-plan-check` runs `EXPLAIN (FORMAT JSON)` with
  sequential scans disabled to prove expected index paths exist for projection
  anchors, Feed-target ShiftingEvent windows, projection snapshot lookup,
  projection-row hot reads, and mismatch-scan anchor paging. Passing checks
  update `CSG10` to `pending` with query-plan evidence; failed checks update it
  to `blocked`. Passing this command does not make `CSG10` ready until source
  parity, observability breadth, and seeded local E2E also exist.
- `GET /feed-direction/readiness` is wired to the Counts/Shifting readiness
  provider so `CSG1`-`CSG10` can move independently under Feed gate `G2`.
- This does **not** close `G2`: raw workbook/XLSX mapping and parity fixtures,
  Sheds DB owner-approved coverage/admin review and seeded publish evidence,
  Shifting/Count source-adapter hardening beyond typed JSONL, production
  scheduling/metrics for the mismatch scan, assignment policy, polished
  command-lens UX, observability,
  query-plan/synthetic-scale proof, and seeded local E2E remain blockers.

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
- Backend exception state changes now emit durable outbox events for opened,
  updated/relinked, and closed projection exceptions. The outbox event is in the
  same Postgres transaction as the exception write, with a tenant validator for
  the `count_projection_exception` aggregate.
- The protected Feed Direction API now exposes the exception queue and close
  actions, registered in the route-permission matrix and OpenAPI. Full
  command-lens subscription/projector, assignment policy, and review-surface UX
  remain G2 blockers.
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
| Base Count import/record | API/import | Write anchor, emit idempotent `counts.base_count_anchor.recorded` outbox event, record `count_source_import_runs` batch evidence, audit, invalidate projections |
| Shifting event ingest | API/import/outbox | Upsert event and impacts, emit idempotent `counts.shifting_event.recorded` outbox event, record `count_source_import_runs` batch evidence, audit, invalidate projections |
| Projection recompute | Scheduler/outbox | Run `backend/cmd/counts-projection-recompute` or the registered `countsapp.ProjectionInputHandler` consumer for tenant + park + as-of + target-date bounded horizons; write snapshot or exception; record `count_projection_recompute_runs` status, counts, timing, trace, and `CSG10` evidence |
| Count mismatch scan | Base Count adoption plus `counts-mismatch-scan` scheduler/import compare | On new physical Base Count, compare previous adopted anchor + applied shifting net and create `unreported_shifting`/`count_mismatch` work for unexpected deltas. The bounded worker command pages through stale historical/imported anchors with tenant/window/limit/cursor guards, writes the same exception work, records `count_mismatch_scan_runs`, and updates `CSG6`/`CSG10` readiness evidence. The anchor page now has dedicated mismatch-scan indexes and query-plan coverage. |
| Alias coverage proof | `counts-alias-coverage-check` | Check source-required aliases such as Sheds DB profile tags against approved `count_dimension_aliases`; update `CSG7` readiness evidence without turning it ready. |
| Source parity proof | `counts-source-parity-check` | Compare sanitized source parity fixtures against canonical projection reads; update `CSG10` readiness evidence without turning it ready. |
| Query-plan proof | `counts-query-plan-check` | Prove expected Postgres index paths for Counts hot reads and update `CSG10` readiness evidence; never use this alone as G2 completion proof. |
| Exception fanout/read model | Projection exception open/update/close plus process-integrity query | Emit `counts.projection_exception.*` outbox events and project `category=feed_direction` exception rows into top-level Action Center/Workflows; assignment policy, frontend UX, and broader Calendar/Protocol Adherence/Control Tower mapping remain separate closure work. |
| Feed projection read | API/app port | Return rows or typed blocker with source hash |

All workers must be tenant/park/date/grain bounded and replay-safe.

Worker observability is part of `CSG10`, not a future ops cleanup. Counts/Shifting
must expose or emit source rows read, import status/latency, successful
Base Count and ShiftingEvent rows, replay counts, failed row counts, recompute
run status/latency, projection row counts, projection exception counts,
stale-projection age, queue/outbox lag, retry counts, DLQ counts, exception
counts by type, and query-plan failures. Those signals roll into Feed gate `G2`
and the same monitoring slice used by Feed generation workers.

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
- Count mismatch/unreported shifting creates exception work from both live Base
  Count adoption and stale imported/historical scan paths, with durable
  `count_mismatch_scan_runs` evidence and readiness updates.
- Projection snapshot source hash changes after input change.
- Feed consumes immutable projection snapshot and does not mutate past runs.
- Initial Feed projection output stays aggregate shed + breed grain, with ration
  context either resolved from reviewed source-backed context or blocked with an
  explicit reason, and does not depend on RFID-to-shed per-goat derivation.
- Query-plan checks for widest allowed projection/read paths.
- Source import run observability records source rows read, successful Base
  Count/Shifting rows, replays, failed rows, source reference, status, last
  error, and `CSG10` evidence without making G2 green by itself.
- Projection recompute run observability records tenant, park, horizon, target
  date, as-of time, status, projection status, snapshot reference, row count,
  exception count, trace id, last error, and `CSG10` evidence without making G2
  green by itself.
- Worker observability checks for latency, lag, retry, DLQ, exception, and
  stale-projection metrics.
- Seeded local E2E coverage for Base Count, realized ShiftingEvent, one-day
  projection, fail-closed exception, and Feed generation consumption of the
  immutable projection snapshot.
- High-effort review-agent findings from
  [BUILD-TO-DONE-GOAL.md](./BUILD-TO-DONE-GOAL.md) are resolved or explicitly
  owner-deferred before Counts/Shifting can turn `G2` green.
