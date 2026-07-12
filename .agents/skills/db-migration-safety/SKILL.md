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

# DB / migration safety (1M-scale)

## Lock-safe migrations
On large tables: `CREATE INDEX CONCURRENTLY` in its own migration; NO long locks,
NO full-table rewrites, NO non-concurrent index. Sequential migration numbers
(check backend/migrations/postgres for the next free number — a collision breaks
ordering). Goose Up/Down; Down restores prior state (verify byte-for-byte). Gates:
`make validate-migrations` (applies 000001→HEAD on a throwaway Postgres) +
`make validate-hot-index-migrations` (hot-table index safety).

## Query-plan proof (no Seq Scan on hot tables)
Every hot-path query on a large table (goat/event/obligation/counter/import/
projection) must show **no Seq Scan** under EXPLAIN. Add/extend
`make validate-sqlc-plans` coverage whenever a query touches those tables. A green
plan proves SHAPE, not "1M-proven" (plans run at ~1k rows). Non-sargable
`lower(col) LIKE '%x%'` → normalized column / `pg_trgm` GIN expression index.

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
non-terminating worker tick — `make scale-guard`. Projections updated on write;
request does an indexed lookup.

## India business calendar
Business meaning derived from timestamps (scheduling, due/missed buckets, reminder
keys, reporting groups) converts to `Asia/Kolkata` first — UTC never defines a
Goat OS business day. Machine: `make india-date-guard`.
