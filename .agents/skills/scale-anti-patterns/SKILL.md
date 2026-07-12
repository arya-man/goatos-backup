---
name: scale-anti-patterns
description: >-
  Use when writing OR reviewing backend Go under backend/internal/** (request
  paths, app services, worker repo methods, SQL) for 1-5M-animal scale safety.
  Detects and fixes the seven banned scale anti-patterns and enforces the 1M
  latency bar. Invoke before touching any query/worker/repo/migration and again
  before pushing. Complements `make scale-guard` (the machine gate) with the
  how-to-fix.
---

# Scale anti-patterns (1M-animal safety)

Rule underneath all of them: **compute-on-write (projections), never
compute-on-read.** Fast at ~1k rows, fatal at 1M. Machine-blocked by
`make scale-guard`; canonical rulebook `docs/decisions/scale-anti-patterns.md`.

## The seven — signature → why fatal → fix

| # | Pattern | Signature to grep/spot | Fix |
|---|---|---|---|
| 1 | **compute-on-read / god-CTE** | big multi-CTE reconstructing derived state per request | materialized read-model/projection updated on write; request does an indexed lookup |
| 2 | **capped read-time rollup as truth** | fetch larger raw page → group in app/frontend → clear cursor → show collapsed count | put the grouped row in the projector; prove with seed/projector E2E |
| 3 | **full (stop-the-world) MV refresh** | `DELETE FROM <proj> WHERE tenant_id` + full reinsert | incremental outbox-delta maintenance or version-swap |
| 4 | **N+1** | `.Query/.QueryRow/.Exec/.SendBatch` inside `for`/`range` | one set-based stmt: `UNNEST`, `INSERT … SELECT`, `CASE` bulk update |
| 5 | **OFFSET pagination** | `LIMIT/OFFSET` with growable offset | keyset/cursor, monotonic, forward-progress |
| 6 | **non-SARGable predicate** | `lower(col) LIKE '%x%'` / function on indexed col | normalized column, expression index, or `pg_trgm` GIN |
| 7 | **polling full scan / unbounded or non-terminating worker tick** | worker loop w/ no cursor advance, or copies all rows | keyset-chunked `FOR UPDATE SKIP LOCKED` claim (copy the obligation/idempotency sweeper) |

## How to detect
- `make scale-guard` — machine gate; **must fail on NEW debt**, not just report baseline.
- `tools/scale-guard/baseline.txt` = the **known existing offenders** (grandfathered w/ owner+issue+expiry). A file/line there is unfixed debt, not a pass. Removing an offender = fix the code, delete its baseline line, re-run `make scale-guard`.
- `make validate-sqlc-plans` — EXPLAIN proof: a hot-path query must show **no Seq Scan** on large tables (goat/event/obligation/counter/import).

## Migrations at scale
`CREATE INDEX CONCURRENTLY` in its own migration; no long locks, no full-table
rewrites, no non-concurrent index on a large table.

## 1M latency bar (release invariant)
Hot read/API: **p50 ≤ 100ms, p95 ≤ 300ms, p99 ≤ 500ms** on realistic
cardinality. p99 ≤ 1000ms only for explicitly-justified non-hot paths. No
percentile proof on realistic volume ⇒ "not proven." User-facing writes ACK
quickly; heavy fanout/rebuild moves to bounded async with correctness proof.

## Genuinely-bounded exception
Annotate the exact line `// scale-guard:ignore: <reason>` — never disable the
guard. Prefer a real set-based fix over an ignore.
