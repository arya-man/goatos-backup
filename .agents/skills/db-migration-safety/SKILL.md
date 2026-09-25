---
name: db-migration-safety
description: >-
  Use when writing OR reviewing a Postgres migration, any handwritten or dynamic
  pgx query, a hot-path query, a read-model/projection, or any mutating write path
  (backend/migrations/postgres, backend/internal/**/adapters/postgres, sqlc).
  Covers SQL bind contracts, lock-safe migrations, query-plan proof (no Seq Scan
  on big tables), write-path idempotency, and atomic transition+read-model sync.
  Thin entrypoint: detailed rules live in the canonical chapters linked below.
  Machine gates: make postgres-bind-contract-guard · validate-migrations ·
  validate-sqlc-plans · validate-hot-index-migrations · idempotency-writes-guard ·
  atomic-readmodel-sync-guard.
---

# DB / migration safety — lens entrypoint

Hot-table DDL must never hold a lock that stalls production. Write paths must be
idempotent. A transition and the read model it owns commit together.

This skill is a **table of contents**, not the rulebook. Open the canonical
chapters below for the live detail; do not review from the summary.

## When this lens applies
- Any file under `backend/migrations/postgres/**`.
- Any handwritten pgx query, especially optional clauses, conditional pruning,
  variadic argument construction, or `Batch.Queue` calls.
- A hot-path query or `adapters/postgres` change on a large table
  (`goat`/`event`/`obligation`/`counter`/`import`/projection).
- Any mutating write path (API, worker, importer, webhook, outbox, server action).
- A state transition that also writes a derived read model / projection it owns.

## Canonical detail (read these — do NOT duplicate here)
- **Review chapters:** [`.agents/skills/goatos-code-review/references/backend.md`](../goatos-code-review/references/backend.md) (migrations, pgx/sqlc, idempotency) · [`.agents/skills/goatos-code-review/references/aggregates-and-projections.md`](../goatos-code-review/references/aggregates-and-projections.md) (atomic transition + owned read-model).
- **Go/pgx/PostgreSQL quality baseline:** [`docs/engineering/backend-go-postgres-quality.md`](../../../docs/engineering/backend-go-postgres-quality.md) — context/transaction/row ownership, pool lifecycle, concurrency, sqlc drift, security, and primary official sources.
- **Migration lock-safety + scale shape:** [`docs/decisions/scale-anti-patterns.md`](../../../docs/decisions/scale-anti-patterns.md).
- **Restructure/drop migrations under the envelope:** [`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../../../docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
- **Android Room upgrade contract (sibling):** [`docs/decisions/room-migration-safety.md`](../../../docs/decisions/room-migration-safety.md).
- **Binary-vs-DB drift:** [`docs/decisions/stale-binary-migration-drift-guard.md`](../../../docs/decisions/stale-binary-migration-drift-guard.md).
- **Location-bearing schema (MANDATORY for location tables):** [`docs/decisions/operational-location-convention.md`](../../../docs/decisions/operational-location-convention.md) — when adding a new table that records location (shed, partition, region, etc.), this ADR specifies the required schema columns, composition rules, and worked examples of bugs to avoid.

## Machine gates
- `make postgres-bind-contract-guard` — proves final SQL placeholder shape and
  supplied arguments agree without hardcoding today's maximum placeholder.
- `make validate-migrations` — applies 000001→HEAD on a throwaway Postgres.
- `make validate-hot-index-migrations` — hot-table lock-safety (concurrent index,
  NOT VALID + concurrent VALIDATE, NO TRANSACTION, catalog-only DROP).
- `make validate-sqlc-plans` — no Seq Scan on hot tables.
- `cd backend && sqlc vet -f sqlc.yaml && sqlc diff -f sqlc.yaml` — ordinary
  static SQL/query and generated-code drift checks without a live database.
- `make idempotency-writes-guard` · `make atomic-readmodel-sync-guard` ·
  `make india-date-guard`. All registered in `tools/ci/guardrail-manifest.json`.

## At a glance (detail in the links above)
- **Bind-safe:** prefer sqlc, then `pgx.StrictNamedArgs`; dynamic SQL must use the
  shared bound-query validator and execute every unresolved production shape in
  PostgreSQL. Do not approve a literal max-placeholder assertion as the primary
  contract.
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

## Proven performance patterns (from main + #415)

Fix catalog PP-1..PP-21 (bad/good snippet, source commit, enforcing guard or
"review-only"): [`docs/decisions/scale-anti-patterns.md` → "Proven performance
patterns (from main + #415)"](../../../docs/decisions/scale-anti-patterns.md).
Machine gates added 2026-09-25: `make scale-guard` rules `count-distinct-sort`,
`cte-self-join`, `hand-rolled-read-cache`, `non-sargable-cast` (now `::text IN`),
and `make admin-web-heavy-client-imports-guard`. Baselines only shrink.
Apply the review-only rows (PP-7..PP-21) by hand when reviewing a hot read.
