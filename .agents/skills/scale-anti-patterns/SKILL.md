---
name: scale-anti-patterns
description: >-
  Use when writing OR reviewing backend Go under backend/internal/** (request
  paths, app services, worker repo methods, SQL) for scale safety — the seven
  banned scale anti-patterns and the current 5k-50k release envelope (query-plan
  proof at ~500k obligation rows), with 1-5M kept as the FUTURE certification
  gate. Thin entrypoint: the detailed rules live in the canonical chapters linked
  below. Invoke before touching any query/worker/repo and before pushing.
  Complements `make scale-guard` (the machine gate).
---

# Scale anti-patterns — lens entrypoint

Rule underneath all of them: **compute-on-write (projections), never
compute-on-read.** Fast at ~1k rows, fatal at scale. Machine-blocked by
`make scale-guard`.

This skill is a **table of contents**, not the rulebook. Do not review from the
summary below — open the canonical chapters and read the live detail.

## When this lens applies
- Any change under `backend/internal/**` that adds/edits a query, worker/sweeper,
  repo method, or SQL, especially on `goat`/`event`/`obligation`/`counter`/
  `import`/projection tables.
- Any hot-path read/API, list endpoint, dashboard slice, or cohort/bulk sweep.
- Reviewing a "fix" that raises a timeout or caps a page instead of changing shape.

## Canonical detail (read these — do NOT duplicate here)
- **Review chapter:** [`.agents/skills/goatos-code-review/references/kernel-and-scale.md`](../goatos-code-review/references/kernel-and-scale.md) — scale + idempotency review checklist.
- **Rulebook (the seven + fixes):** [`docs/decisions/scale-anti-patterns.md`](../../../docs/decisions/scale-anti-patterns.md).
- **Release envelope + the 5 scoped screen exemptions:** [`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`](../../../docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
- **Projection serving / freshness:** [`docs/decisions/high-scale-dashboard-projections.md`](../../../docs/decisions/high-scale-dashboard-projections.md).
- **Future 1M gate:** [`docs/decisions/one-million-postgres-readiness.md`](../../../docs/decisions/one-million-postgres-readiness.md).

## Machine gates
- `make scale-guard` — must FAIL on new debt; `tools/scale-guard/baseline.txt` is
  grandfathered debt, not a pass. Registered in `tools/ci/guardrail-manifest.json`.
- `make validate-sqlc-plans` — EXPLAIN proof: no Seq Scan on large tables.
- `make admin-web-request-reads-guard` — the admin-web SSR twin (full-table walk).

## At a glance (detail in the links above)
1. compute-on-read / god-CTE → materialized read model, indexed lookup.
2. capped read-time rollup presented as truth → grouped row in the projector.
3. full (stop-the-world) MV refresh → incremental outbox-delta / version-swap.
4. N+1 query and N+1 fan-out → one set-based statement / `*ByIDs` batch.
5. OFFSET pagination → keyset/cursor, monotonic, forward-progress.
6. non-SARGable predicate → normalized column / expression index / `pg_trgm` GIN.
7. polling full scan / unbounded / non-terminating worker tick → keyset-chunked
   `FOR UPDATE SKIP LOCKED`, resumable cursor, never restart at zero.

Genuinely-bounded case → annotate the exact line `// scale-guard:ignore: <reason>`;
never disable the guard. The five named canonical screen reads are the ONLY
compute-on-read exemption — scoped, plan-tested; see the envelope ADR above.
