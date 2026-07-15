---
name: db-migration-safety
description: >-
  Use when writing OR reviewing a Postgres migration, a hot-path query, a
  read-model/projection, or any mutating write path (backend/migrations/postgres,
  backend/internal/**/adapters/postgres, sqlc). Covers lock-safe migrations,
  query-plan proof (no Seq Scan on big tables), write-path idempotency, and atomic
  transition+read-model sync. Thin entrypoint: detailed rules live in the canonical
  chapters linked below. Machine gates: make validate-migrations · validate-sqlc-plans ·
  validate-hot-index-migrations · idempotency-writes-guard · atomic-readmodel-sync-guard.
---

# DB / migration safety — lens entrypoint

Hot-table DDL must never hold a lock that stalls production. Write paths must be
idempotent. A transition and the read model it owns commit together.

This skill is a **table of contents**, not the rulebook. Open the canonical
chapters below for the live detail; do not review from the summary.

## When this lens applies
- Any file under `backend/migrations/postgres/**`.
- A hot-path query or `adapters/postgres` change on a large table
  (`goat`/`event`/`obligation`/`counter`/`import`/projection).
- Any mutating write path (API, worker, importer, webhook, outbox, server action).
- A state transition that also writes a derived read model / projection it owns.

## Canonical detail (read these — do NOT duplicate here)
- **Review chapters:** [`.agents/skills/goatos-code-review/references/backend.md`](../goatos-code-review/references/backend.md) (migrations, pgx/sqlc, idempotency) · [`.agents/skills/goatos-code-review/references/aggregates-and-projections.md`](../goatos-code-review/references/aggregates-and-projections.md) (atomic transition + owned read-model).
- **Migration lock-safety + scale shape:** [`docs/decisions/scale-anti-patterns.md`](../../../docs/decisions/scale-anti-patterns.md).
- **Restructure/drop migrations under the envelope:** [`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../../../docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
- **Android Room upgrade contract (sibling):** [`docs/decisions/room-migration-safety.md`](../../../docs/decisions/room-migration-safety.md).
- **Binary-vs-DB drift:** [`docs/decisions/stale-binary-migration-drift-guard.md`](../../../docs/decisions/stale-binary-migration-drift-guard.md).

## Machine gates
- `make validate-migrations` — applies 000001→HEAD on a throwaway Postgres.
- `make validate-hot-index-migrations` — hot-table lock-safety (concurrent index,
  NOT VALID + concurrent VALIDATE, NO TRANSACTION, catalog-only DROP).
- `make validate-sqlc-plans` — no Seq Scan on hot tables.
- `make idempotency-writes-guard` · `make atomic-readmodel-sync-guard` ·
  `make india-date-guard`. All registered in `tools/ci/guardrail-manifest.json`.

## At a glance (detail in the links above)
- **Lock-safe:** bounded `lock_timeout`; `CREATE INDEX CONCURRENTLY` in its own
  `-- +goose NO TRANSACTION` migration; hot-table CHECK/FK via `NOT VALID` + a
  separate concurrent VALIDATE.
- **Separated phases:** add-nullable → chunked backfill → constrain (never one
  lock-holding all-in-one).
- **Resumable/idempotent:** backfill `WHERE` skips filled rows; sequential
  migration numbers (no collision, no manual renumber).
- **Query-plan proof:** EXPLAIN at the ~500k upper bound; prove BOTH the keyset
  list and the summary aggregate shapes.
- **Idempotency:** stable key + fingerprint persisted in the same txn; exact
  replay returns the original with no new side effects; same-key/diff-payload
  rejected.
- **Atomic read-model:** derived upsert + parity check INSIDE the transition txn;
  a sync failure rolls the whole transition back.
- **India business date:** business meaning of a timestamp resolves in
  `Asia/Kolkata` first — UTC never defines a business day.
