---
name: scale-anti-patterns
description: >-
  Use when writing OR reviewing backend Go under backend/internal/** (request
  paths, app services, worker repo methods, SQL) for scale safety — the seven
  banned scale anti-patterns and the current 5k-50k release envelope (query-plan
  proof at ~500k obligation rows), with 1-5M kept as the FUTURE certification
  gate. Also the CANONICAL catalog of the 2026-09-24 STG latency banned
  patterns (P1-P25: API p95 budget 50-100ms target, 200-300ms acceptable, 500ms
  hard max). Thin entrypoint: the detailed rules live in the canonical chapters linked
  below. Invoke before touching any query/worker/repo and before pushing.
  Complements `make scale-guard` (the machine gate).
---

# Scale anti-patterns — lens entrypoint

Hot API/SSR serving reads have a hard sub-500ms budget. Seconds-class responses
are a production bug, not a review note: fix the query/API shape, endpoint grain,
projection/read model, or batching before pushing.

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
- Any admin-web operational navigation/link touching authenticated SSR/API-read
  pages. Next.js route prefetch is an implicit request read; use the
  no-prefetch Link wrapper and keep heavy reads user-triggered.
- Reviewing a "fix" that raises a timeout or caps a page instead of changing shape.
- Any schedule, calendar, freshness, date-window, or month/year filter logic in
  frontend or backend code. Business calendar logic must be explicit about the
  Goat OS business timezone; server-local date extraction is a correctness
  anti-pattern.
- Any staging deploy or E2E handoff after backend, admin-web, worker, job, seed,
  migration, or read-model changes. A mixed-SHA STG environment is a scale and
  correctness anti-pattern, not a valid debug target.
- Any measured API/SSR page load at or above 500ms, or any "fix" that keeps a
  narrow screen on a broad catch-all endpoint and depends on loading UI to mask
  the delay.

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
- `node tools/perf/api-latency-policy.test.mjs` and the API latency gate — hard
  budget is p90 <= 300ms and p95/p99 <= 500ms for hot serving reads; do not
  relax manifests above that ceiling.
- `make admin-web-request-reads-guard` — the admin-web SSR twin (full-table walk).
- `make admin-web-prefetch-guard` — blocks implicit admin-web request reads from
  direct `next/link` route prefetch or `prefetch={true}`.
- `make calendar-endpoint-grain-guard` — blocks backend/admin-web/mobile/contract/
  DB wiring that feeds a narrow vaccination schedule/full-schedule surface from
  the broad Calendar events endpoint.
- `tools/deploy/stg-clouddeploy-release.sh` — for break-glass local STG repair,
  waits for Cloud Deploy and verifies API, admin-web, worker, migration, DLQ, and
  analytics images all match the same current main SHA. Never skip rollout/image
  parity for an E2E handoff.

## At a glance (detail in the links above)
1. compute-on-read / god-CTE → materialized read model, indexed lookup.
2. capped read-time rollup presented as truth → grouped row in the projector.
3. full (stop-the-world) MV refresh → incremental outbox-delta / version-swap.
4. N+1 query and N+1 fan-out → one set-based statement / `*ByIDs` batch.
5. OFFSET pagination → keyset/cursor, monotonic, forward-progress.
6. non-SARGable predicate → normalized column / expression index / `pg_trgm` GIN.
7. polling full scan / unbounded / non-terminating worker tick → keyset-chunked
   `FOR UPDATE SKIP LOCKED`, resumable cursor, never restart at zero.
8. business-calendar hardcode / server-local date extraction → use an explicit
   business timezone (`Asia/Kolkata` today) for month/year/window checks and add
   boundary tests for IST midnight crossing UTC day/month.
9. admin-web route prefetch of heavy SSR/API-read pages → use
   `@/components/no-prefetch-link`; reads happen after click, not on link
   visibility/hover.
10. broad endpoint reused for narrow screen → call/build the endpoint whose
    contract matches the screen grain/window. Example: Full Schedule reads
    `/vaccination/schedule`, not the broad Calendar events union.
11. per-actor capacity read from the DB and spent without subtracting what the
    CURRENT run already reserved for the same `(tenant, park, date, actor)` key →
    net the in-session reservation ledger at the SELECTION layer, not only at the
    later split layer. A cached pre-run snapshot re-read by a second pass
    over-selects past the cap (observed 221-223 vs a 200/operator/day cap).
12. event handler registered on a bus nothing real dispatches to → register every
    `Register(bus eventbus.Bus)` type on EVERY durable bus
    (`backend/internal/kernelstages/bus.go` AND
    `backend/cmd/domain-event-consumer/main.go`). `bootstrap/api.go` and
    `domainconsumer/wiring/bus.go` are not production dispatch; a green E2E that
    builds its own bus proves handler logic, never wiring.
13. write that mutates scheduling-relevant state (operator cap, week-off,
    status/validity, tenant capacity config, operator-assignment config) without
    enqueuing its cascade event (`vaccination.capacity.changed` /
    `vaccination.roster.changed`) in the SAME transaction → already-planned future
    work keeps the stale config forever. Coverage is per WRITE PATH; "another
    endpoint emits it" is not coverage.

`make cascade-event-wiring-guard` (`tools/agent-hooks/check-cascade-event-wiring.mjs`)
enforces 11-13 statically; see `docs/decisions/scale-anti-patterns.md` ->
"Operator-cascade wiring anti-patterns".

Genuinely-bounded case → annotate the exact line `// scale-guard:ignore: <reason>`;
never disable the guard. The five named canonical screen reads are the ONLY
compute-on-read exemption — scoped, plan-tested; see the envelope ADR above.

STG handoff rule → no stale-content exceptions. Before any deployed-STG E2E claim,
verify `HEAD == origin/main` and every STG service/job image matches that exact
SHA. If `origin/main` moves during or after rollout, redeploy and re-verify the
new SHA before debugging UI behavior.

## STG latency catalog (P1-P25) — canonical, 2026-09-24 incident

This is the ONE catalog. Rule docs, review/build skills and the PR template link
here; do not copy it. Incident record, root causes and audits:
[`docs/perf/2026-09-24-stg-latency/README.md`](../../../docs/perf/2026-09-24-stg-latency/README.md)
("Banned patterns" 1-9) and `audit/*.md` next to it.

**Budget (every API, p95, measured on stg-sized data on `db-g1-small`):**
50-100ms target; 200-300ms acceptable; **500ms hard max, never shipped**.
Anything > 100ms needs a one-line justification in the PR.

**Evidence a change must attach when it touches a query, route, job or table**
(the "perf packet"): before/after `EXPLAIN (ANALYZE, BUFFERS)` on stg-sized data
(OCI clone read-only, or a throwaway DB), endpoint p50/p95 before/after on
realistic params, statements per request, retention/prune job for any new
append-only table, and for list routes the default + max page size. Adding any
P-item below is a BLOCKING review finding unless it carries a
`perf-budget:allow <rule> reason="..." owner=@x expires=YYYY-MM-DD` exemption.

Format: **bad (seen in this incident)** -> do instead. Evidence = the perf packet
plus the item-specific proof named.

### README banned patterns (1-9)
- **P1 Unbounded history aggregate on a request path.** Bad: notification unread
  count ran `COUNT(DISTINCT ...)` over a member's whole history (up to 96k rows,
  ~610MB buffers per call). Do: stored counter/read-state updated in the same tx
  as the write; read by PK. Every history table has mark-read / auto-resolve /
  retention. Proof: EXPLAIN shows an index/PK lookup, not an aggregate.
- **P2 Operational Postgres as an analytics/event store.** Bad:
  `analytics.app_events` 2.6GB/1.58M rows, never pruned, rescanned by the rollup.
  Do: analytics in BigQuery/Firebase; any event/log/telemetry table ships a
  retention job that archives to GCS cold storage before pruning. Proof: retention
  entry + prune job named in the PR.
- **P3 Joins that multiply rows before filtering.** Bad: FCR compared 16,922 feed
  rows x 649 prices (~11M comparisons). Do: resolve 1:1 lookups first (LATERAL /
  range index on effective date), aggregate the many side before joining. Proof:
  no plan node whose rows exceed 10x its larger input.
- **P4 Per-viewer recompute on every event.** Bad: Herd Signals live re-read all
  tags per viewer every gateway batch (~2s). Do: coalesce by key + time window,
  compute once, broadcast. Proof: DB statements per event independent of viewers.
- **P5 Cache key containing request time.** Bad: vaccination read caches keyed on
  nanosecond `now()` -> 0% hits. Do: key on business date (+ time bucket), single-
  flight concurrent misses. Proof: hit ratio from a repeated-call test.
- **P6 Connection held forever from the shared pool.** Bad: Herd Signals LISTEN
  held a main-pool conn permanently. Do: dedicated conn / small dedicated pool;
  `Acquire` only with `defer Release` in the same function. Proof: pool config.
- **P7 Job crash-loops on an outside dependency or rescans everything.** Bad:
  analytics-rollup rescanned all events, then died on a BigQuery 403; dispatcher
  refired it every ~40 min, saturating ~600 IOPS for 2-3 min each time. Do:
  watermark, outside failures non-fatal (degraded success + metric), backoff, conn
  cap (<=3). Proof: test with the dependency failing.
- **P8 Full copies of the stg DB to other environments.** Bad: full `pg_dump`s to
  OCI, 13-16GB/day of Cloud SQL egress. Do: delta-only sync by watermark, junk
  tables excluded, compressed (README "OCI sync"). Proof: sync bytes per run.
- **P9 Unbounded list returned or fetched (admin-web or Android).** Bad: the
  notifications history; `ListLive` walking 50k tags for `limit=1`; the ±2y
  calendar window; the vaccination command reading all 86k obligations. Do:
  server-side keyset/cursor pagination (no OFFSET on large tables); server-enforced
  default page size (e.g. 50) and hard max (e.g. 200); stable sort with a unique
  tiebreaker; totals from a stored counter or a separate cheap count, never a full
  scan per page; client loads more on scroll/next page; Android keeps its one-page
  fetch cap + Room paging (`docs/agent-rules/android.md`); export / "show all" is
  an async export job, never the request path. Proof: route declares page params
  + max; EXPLAIN of page 1 and a deep cursor page.

### Found by the audits (`audit/*.md`)
- **P10 N+1 / many sequential statements per request.** Bad: `/app/bootstrap` 6
  serial reads; configuration registers 21 serial counts; work-board page 20-45
  statements; herd-signals live ~15. Do: one set-based query, `= ANY($1)`, or one
  `pgx.Batch` (1 round trip). Proof: statements per request before/after.
- **P11 JIT on for OLTP.** Bad: `jit=on` added 0.8-1.0s to action-center/workflows
  and 20-66ms to tiny admin lists. Do: `jit=off` for the app role/DB. Proof:
  `SHOW jit` on the target env.
- **P12 Re-planning complex statements every call.** Bad: vaccination execution
  used `QueryExecModeExec`, forcing custom plans costing 20-93ms each. Do: default
  statement cache / `CacheDescribe`; test `plan_cache_mode=force_generic_plan`.
  Proof: EXPLAIN planning time vs execution time.
- **P13 Per-request lookups of static config tables.** Bad: `protocol_definitions`
  seq-read 40.6M times, `protocol_versions` 34.8M, `workforce_members` 12.4M. Do:
  in-process cache with TTL + max entries + event/write eviction. Proof: stmt
  count and an invalidation test.
- **P14 Unindexed FK / filter columns.** Bad: ~280 FKs without an index;
  `obligation_instances` (86k rows) seq-read 14B rows, `goats` 10.3B. Do: btree
  whose leading columns match the equality filters (migration, `CONCURRENTLY`).
  Proof: EXPLAIN shows Index Scan at stg size.
- **P15 OR across tables / identity shapes defeating indexes.** Bad: work-board
  "any vaccination today" OR across 3 tables (228ms -> 5ms after rewrite);
  calendar event detail OR over 4 id shapes. Do: parse the id, UNION ALL / EXISTS
  per branch, each index-backed. Proof: EXPLAIN per branch.
- **P16 Unbounded date ranges (±2y) or no lower bound.** Bad: calendar
  `eventExists` evaluated the full canonical CTE over ±2y (events/{id} 2.8s,
  history 1.3s); action-center with no lower bound. Do: narrowest window the
  contract allows (±1 day for a known date; explicit lower bound always). Proof:
  EXPLAIN with the bounded window.
- **P17 Count query duplicating the row query.** Bad: calendar rail ran the row
  CTE and again for `count(*)`; workbook export count. Do: one pass
  (`count(*) OVER()` + LIMIT) or a stored counter. Proof: statements per request.
- **P18 Dead-row bloat on queue tables without autovacuum tuning.** Bad:
  `obligation_instances` 86.6k live / 347k dead tuples, stale stats; 14% dead on
  `domain_event_processed_events`. Do: per-table `autovacuum_vacuum_scale_factor`
  (e.g. 0.02) in the migration that creates a high-churn table; fix the churning
  writer. Proof: `pg_stat_user_tables` dead_pct.
- **P19 Blocking UI/login on telemetry or best-effort calls.** Bad: Android login
  waited on `POST /auth/session-events` (512ms p95 under load). Do:
  fire-and-forget after navigation, bounded queue, never on the critical path.
  Proof: login flow timing with the call failing/slow.
- **P20 Generic error copy hiding which module failed.** Bad: admin-web "could not
  be loaded" / "read failed" with no module or timeout reason. Do: module-specific
  copy (`Herd Signals timed out`) and a retry per module. Proof: screenshot of
  the error state.
- **P21 Client timeout shorter than server work, no abort propagation.** Bad:
  client gives up while the server keeps running a 15s query; retries stack. Do:
  fix the server shape first; pass `AbortSignal` / request ctx through to pgx so
  cancellation stops the query. Proof: canceled request ends its DB statement.
- **P22 admin-web `no-store` fan-out without coalescing.** Bad: many `no-store`
  reads per page per viewer, each hitting the API cold. Do: dedupe identical
  in-flight reads, short server/burst cache on business-date keys, one aggregate
  endpoint for a page. Proof: request count per page load.
- **P23 Polling fallbacks that multiply load.** Bad: live screens polling every
  few seconds per tab on top of the stream. Do: poll only when the stream is down,
  with backoff + jitter, pause when hidden. Proof: requests/min per open tab.
- **P24 Perf fixes not deployed (drift).** Bad: stg ran `a67be34c0781` while main
  was 116 commits ahead; every "still broken" report tested old code. Do: land via
  `make land-main`, deploy the exact main SHA, re-measure on stg. Proof: deployed
  revision SHA == main SHA + after timings.
- **P25 Heavy request fed by the wrong grain / whole-set projection.** Bad:
  vaccination command read all 86k obligations, 88% canceled. Do: filter to the
  screen's grain with a partial index / projection. Proof: rows read vs returned.
