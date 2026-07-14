---
name: scale-anti-patterns
description: >-
  Use when writing OR reviewing backend Go under backend/internal/** (request
  paths, app services, worker repo methods, SQL) for scale safety. Detects and
  fixes the seven banned scale anti-patterns and enforces the current 5k-50k
  release envelope (query-plan proof at the upper bound ~500k obligation rows),
  with the 1M/1-5M latency bar kept as the FUTURE certification gate. Invoke
  before touching any query/worker/repo/migration and again before pushing.
  Complements `make scale-guard` (the machine gate) with the how-to-fix.
---

# Scale anti-patterns (5k-50k envelope now, 1M-animal certification later)

Rule underneath all of them: **compute-on-write (projections), never
compute-on-read.** Fast at ~1k rows, fatal at scale. Machine-blocked by
`make scale-guard`; canonical rulebook `docs/decisions/scale-anti-patterns.md`.

**Scope reframe, not a safety downgrade.** The accepted ADR
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md` sets the CURRENT
release target to 5,000-50,000 animals and narrows the 1M/1-5M topology to
FUTURE certification work. All seven anti-patterns below stay **banned in
`backend/internal/**`** exactly as before — with ONE scoped, plan-tested
exemption for five named canonical screen reads (see "Scale-guard reconciliation"
at the end). `compute-on-read` stays banned everywhere else, and every other
migration/idempotency/worker/mobile bar is unchanged.

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

## Latency bar (current envelope + future certification)
Hot read/API: **p50 ≤ 100ms, p95 ≤ 300ms, p99 ≤ 500ms** on realistic
cardinality. p99 ≤ 1000ms only for explicitly-justified non-hot paths. No
percentile proof on realistic volume ⇒ "not proven." User-facing writes ACK
quickly; heavy fanout/rebuild moves to bounded async with correctness proof.

- **Current release gate (5k-50k):** prove the percentile bar at the upper bound
  of the envelope — **~500k obligation rows** (50k animals × retained obligations,
  the top row of the ADR's obligation-volume table), with single-worker
  cadence/backlog validation. A green plan at the 5k list case is NOT proof for
  the 50k aggregate; run the aggregate/summary path against the ~500k row count.
- **Future certification gate (1M / 1-5M):** the full 1M-animal / 1-5M topology
  bar is retained as the certification target for later scale-out work, not
  deleted. It is future work per the ADR, not a present release requirement, but
  it still governs any design that claims million-scale readiness.

## Genuinely-bounded exception
Annotate the exact line `// scale-guard:ignore: <reason>` — never disable the
guard. Prefer a real set-based fix over an ignore.

## Scale-guard reconciliation (5k-50k screen reads)
Under the accepted ADR
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, screens read
canonical indexed SQL by default (list = keyset ~20; summary = indexed
aggregate). **Zero** of the five named screen projections
(`calendar_event_projections`, `process_integrity_projection_rows`,
`vaccination_shed_projection_rows`, `vaccination_execution_projection_rows`,
`vaccination_operations_projection_rows`) run in the active runtime;
`vaccination_eligibility_rollups` and the counts summaries survive.

Serving those five screens from canonical tables per request is the
`compute-on-read` / god-CTE shape that anti-pattern #1 bans and `make scale-guard`
blocks. It is reconciled — **not** by disabling the guard — as follows:

- The five named read paths (Calendar, process-integrity, shed, execution,
  operations) are **EXEMPTED** via a scoped annotation on each read:
  `// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
  (plus a matching `tools/scale-guard/baseline.txt` entry if the guard requires
  one). The guard is **NOT globally disabled** and stays fully active for every
  other path in `backend/internal/**`; `compute-on-read` stays banned everywhere
  else.
- The exemption is scoped to reads that are **query-plan-tested** for both read
  shapes — the keyset ~20 list AND the indexed summary aggregate run against the
  upper-bound ~500k obligation rows. An exempted read with no query-plan test is a
  defect, not a pass.
- When a screen later earns its own projection (see the ADR's scale-out ladder),
  its annotation is removed and that read returns under full guard enforcement.

All other rules on this page — the seven anti-patterns, the migration
lock-safety guidance, and the latency bar — are unchanged by this exemption.
