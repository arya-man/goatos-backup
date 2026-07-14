---
name: db-migration-safety
description: >-
  Use when writing OR reviewing a Postgres migration, a hot-path query, a
  read-model/projection, or any mutating write path (backend/migrations/postgres,
  backend/internal/**/adapters/postgres, sqlc). Enforces lock-safe migrations,
  query-plan proof (no Seq Scan on big tables), write-path idempotency, and atomic
  transition+read-model sync. Invoke before touching a migration/query and before
  pushing. Machine gates: make validate-migrations · validate-sqlc-plans ·
  validate-hot-index-migrations · idempotency-writes-guard · atomic-readmodel-sync-guard.
---

# DB / migration safety (5k-50k envelope; 1M future gate)

Current release scale target is the **5,000-50,000-animal envelope** per
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md` (the authority).
The **1M / 1-5M** bar is NOT deleted — it stays as the FUTURE certification gate,
reintroduced per that ADR's scale-out ladder when measured workload requires it.
Every lock-safety, idempotency, and atomic-read-model rule below is UNCHANGED;
only the query-plan proof's upper-bound row count is reframed to this envelope.

## Lock-safe migrations
On large tables: `CREATE INDEX CONCURRENTLY` in its own migration; NO long locks,
NO full-table rewrites, NO non-concurrent index. Sequential migration numbers
(check backend/migrations/postgres for the next free number — a collision breaks
ordering). Goose Up/Down; Down restores prior state (verify byte-for-byte). Gates:
`make validate-migrations` (applies 000001→HEAD on a throwaway Postgres) +
`make validate-hot-index-migrations` (hot-table index safety).

The ADR's restructure migrations are EXPECTED and must still clear every gate
above: dropping the five projection tables (`calendar_event_projections`,
`process_integrity_projection_rows`, `vaccination_shed/execution/operations_projection_rows`)
and their state tables, converting the three partitioned parents
(`goat_identity_events`, `audit_log`, `obligation_status_events`) to ordinary
indexed tables, and rewiring notification/snooze foreign keys off
`calendar_event_projections` onto canonical `obligation_instances`/
`obligation_batches`/`sop_tasks`. Current rows are test data (no backfill), but
the DROP/rewire ordering must be lock-safe and the FK rewire must land before the
table drop. `vaccination_eligibility_rollups` and counts summaries SURVIVE — do
not drop them.

## Query-plan proof (no Seq Scan on hot tables)
Every hot-path query on a large table (goat/event/obligation/counter/import/
projection) must show **no Seq Scan** under EXPLAIN. Add/extend
`make validate-sqlc-plans` coverage whenever a query touches those tables.
Non-sargable `lower(col) LIKE '%x%'` → normalized column / `pg_trgm` GIN
expression index.

The **current** proof runs at the 5k-50k envelope's upper bound: ~500k
obligation rows (50k animals × retained obligations), with single-worker
cadence/backlog validation. Distinguish the two read shapes and plan-test BOTH:
list reads are keyset-paginated at ~20 rows (bounded regardless of herd size);
summary aggregates (Control Tower gaps, adherence rollups, process-integrity
counts) cannot be keyset-paginated and must be run against the ~500k upper-bound
row count, NOT just the 5k list case. A green plan at 5k is not proof for the 50k
aggregate. A green plan still proves SHAPE, not "1M-proven"; the **1M / 1-5M**
plan proof remains the FUTURE certification gate (plans currently run at ~1k
rows in CI — see the ADR's runtime-gap section), not a present release
requirement.

## Idempotency (every write path) — machine: idempotency-writes-guard
Persist a stable idempotency key + semantic request fingerprint in the SAME txn as
the side effects. Exact replay returns the original result with no new side effects;
same-key different-payload replay is rejected. `ON CONFLICT DO UPDATE SET
idempotency_key = EXCLUDED.idempotency_key` alone is NOT sufficient when later code
still mutates state. Test: first call, exact replay, same-key different-payload,
downstream dup prevention.

## Atomic transition + owned read-model — machine: atomic-readmodel-sync-guard
A state transition and the sync of a read model it OWNS are ONE transaction. Do the
derived upsert + parity check INSIDE the same txn; a sync failure rolls the whole
transition back (no status flip, no outbox, no audit, no partial read model). Post-
commit best-effort sync only for an already-committed replay. Canonical:
`PublishVersionWithCapacity` + its rollback regression test.

## Scale-shape (see scale-anti-patterns skill)
No god-CTE/compute-on-read, N+1, deep OFFSET, full-table MV refresh, unbounded/
non-terminating worker tick — `make scale-guard`. These stay BANNED across
`backend/internal/**`; compute-on-read stays banned everywhere. The default read
model is canonical indexed SQL: list = keyset ~20; summary = indexed aggregate.

The ADR intentionally serves FIVE named screens (Calendar, process-integrity,
shed, execution, operations) from canonical tables per request. Those five reads
— and ONLY those — are EXEMPTED via scoped
`// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
annotations plus query-plan tests (both list and aggregate shapes; an exempted
read that is not plan-tested is a defect). `make scale-guard` is NOT globally
disabled — it stays fully active for every other path. When a screen later earns
its own projection, remove the annotation and it returns under enforcement.
Projections that remain are updated on write; the request does an indexed lookup.

## India business calendar
Business meaning derived from timestamps (scheduling, due/missed buckets, reminder
keys, reporting groups) converts to `Asia/Kolkata` first — UTC never defines a
Goat OS business day. Machine: `make india-date-guard`.
