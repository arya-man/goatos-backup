# Performance guardrails design (PR vgoats/goatos#389 follow-up)

Repo: ~/mesha/goatos-pr-burst. Read-only design. Budget: every API target <=100ms, 200ms (p90) / 300ms (p95) allowed, 500ms absolute (p99/max), on db-g1-small.

## 0. Inventory: what exists and why it missed each defect

| Existing control | What it catches | Why it missed |
|---|---|---|
| `docs/agent-rules/scale-performance.md` (190 lines) | Prose: 5k-50k envelope, compute-on-write, N+1, OFFSET, non-SARGable, polling scans, grain/fan-out rules for aggregates | Advisory text. Says nothing about retention, analytics in OLTP, pool lifetimes, job failure modes, deploy drift. Explicitly admits "gates run at ~1k rows". |
| `make scale-guard` (`tools/scale-guard/scaleguard.go`, rules: n-plus-one, n-plus-one-fanout, god-cte, full-mv-refresh, offset-pagination, non-sargable-like/cast, loop-no-cursor, read-rollup-truth, cte-limit-outside, hot-path-inline-sql) | Regex/AST shapes in `backend/internal/**` Go | Syntactic only. The FCR 16,922x649 blowup is a legal-looking join; unread-count over whole history is a legal `COUNT(*) WHERE user_id=$1`; seq scans are a planner property, invisible to text. `backend/cmd/**` (analytics-rollup) is out of scope. 224-line baseline normalises debt. |
| `make validate-sqlc-plans` (`backend/tests/integration/validate-sqlc-query-plans.sh`) | Hand-written `explain_must_use_index` assertions for ~a dozen named queries (GoatByID, GoatSearchDisplay, obligation claim, dirty-scopes) on a fixture | Opt-in per query. obligation_instances/goats seq-reads came from queries nobody registered. No row-estimate, spill, or FK-index checks. Needs Docker or OCI DSN. |
| `make commandboard-query-plan-guard` | Plan gate for one endpoint family | Single surface. |
| `make api-latency-policy-test` / `api-latency-gate` (`tools/perf/api-latency-policy.mjs`: p90 300, p95 500, p99 500; `hot-paths.*.json`) | Policy-module unit tests (ci-local); live gate against manifests | ci-local runs only the *unit tests*, not the gate. Gate covers routes someone listed in a manifest, at ~1k rows, and is not required by land-main. No 100ms target / warn tier. herd-signals live and notification unread count not in any manifest. |
| `request-path-evidence.mjs` + `request-path-usage.sql` | pg_stat_statements evidence per request path | Evidence tool, not a gate. |
| `make aggregate-projection-guard` | Requires `projection-review:` marker near JOIN+aggregate changes | Marker = assertion, not measurement; FCR comparison is not a COUNT/SUM aggregate. |
| `make postgres-bind-contract-guard` | Bind typing / column-side cast regressions | Unrelated to row volume. |
| `make worker-stage-budgets-guard` | Outbox/notification batch limits and cadence in kernel-worker tf | Only kernel-worker; no Cloud Run *jobs*, no external-dependency fatality, no retry/backoff, no conn cap. analytics-rollup crash-looping on BigQuery 403 was invisible. |
| `make deployed-job-flags-guard` | tf job args vs `deploy/runtime/workers.json` vs binary flags | Proves the job *can start*; says nothing about what it does per run or whether a failure is fatal. |
| `make stg-promotion-guard` (`tools/ci/check-stg-promotion.mjs`) | Push/promotion path hygiene to stg | Validates *how* you promote, not *that* main got promoted. Merged perf fixes sat undeployed. |
| `make scale-certification-docs-guard`, `scale-cert`, `scale-kernel-gate(-smoke)` | Heavy 500k certification for Calendar/reminder and bulk-status kernel | Not in ci-local inner loop; scoped to two surfaces. |
| `docs/agent-rules/observability.md` | Firebase analytics events, telemetry, burst caches only for analytics summaries | Encourages analytics events; never says where they may be stored or for how long. |
| `docs/agent-rules/ci-landing-release.md`, `make land-main`, `local-ci-evidence-guard` | Exact-SHA ci-local receipt before merge | Receipt proves ci-local; ci-local contains no DB-volume perf gate by default. Nothing after merge. |
| Skills: `scale-anti-patterns`, `kernel-scale-lens`, `goatos-code-review` (+ `references/kernel-and-scale.md`, `review-lens-ledger.md`), `db-migration-safety`, `goatos-herd-signals` | Author/reviewer guidance | No required EXPLAIN evidence; no checklist item for retention, pool lifetime, per-viewer fan-out, job failure isolation, or deploy status. |
| `make review-lens-ledger-guard` | Ledger format for review lenses | Only enforces that lenses were recorded, not a perf lens. |
| `.github/PULL_REQUEST_TEMPLATE.md` | "Bind-guard performance judge ... API latency evidence, or N/A" | Free-text; N/A is accepted without machine check. |

Pool.Acquire call sites today (non-test): obligation/operator_recompute.go, obligation/visit_shot_lock.go, ceoai/{app,safety}/concurrency.go, ceoai/app/orchestrator.go, ceoai/safety/layer.go, ceoai/sqlguard/executor.go, herdsignals/adapters/postgres/live_notifications.go, platform/worker/stagelock.go, verification/adapters/postgres/repository.go. None is guarded.

Root pattern: every existing gate is either (a) textual and blind to data volume, or (b) volume-aware but opt-in per query/route. The new guards invert (b) to opt-out: **every** query and **every** GET route is measured unless allowlisted with a justification.

## A. Guards

Common conventions (reuse existing ones):
- Implementation in `tools/perf/` (node) or `tools/agent-hooks/check-*.mjs`, each with `--self-test` containing adversarial fixtures (per scale-performance.md "guard false-green" rule), registered via `guardrail-registration-guard`.
- Escape hatch format, uniform across guards: allowlist JSON entry `{ "id", "reason", "owner", "expires": "YYYY-MM-DD", "evidence": "<EXPLAIN/latency link>" }` or inline `-- perf-budget:allow <rule> reason="..." owner=@x expires=YYYY-MM-DD` (SQL) / `// perf-budget:allow ...` (Go). Guard fails on missing reason (<20 chars), missing owner, or past expiry. Baselines ratchet down only (copy `exception-guard-ratchet-v2` mechanics).

### A1. `query-plan-budget-guard` (new; supersedes the ad hoc list in validate-sqlc-plans)
- Files: `tools/perf/query-plan-budget/` (Go, so it can load sqlc catalog): `main.go`, `seed.sql` generator, `budget.yaml`, `allowlist.json`.
- Seed: extend `tools/perf/seed-scale-shaped-projections.sql` into `tools/perf/seed-realistic.sql` sized to the stg top-end (from pg_class.reltuples snapshot, committed as `tools/perf/table-sizes.stg.json`): goats 50k, obligation_instances 500k, notifications 2M (skewed: 20 users own 60%), feed rows 20k, prices 1k, herd_signal events 5M (partitioned), analytics tables 0 (see A3). Then `ANALYZE`. Use `pg_stats` injection for tables too costly to materialise (set `reltuples/relpages` via `pg_class` update in the scratch DB) so the planner sees production cardinality cheaply.
- Enumerate every query in `backend/internal/**/queries/*.sql` (sqlc) plus inline SQL found by scale-guard's `hot-path-inline-sql` extractor. Bind params with representative values from seed (`budget.yaml` param generators per type).
- Run `EXPLAIN (FORMAT JSON)` for all; `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` for queries tagged `-- name: X :many/:one` on request paths (Go call graph from `cmd/goatos-api`).
- Fail rules:
  - `seq-scan-large`: Seq Scan on relation with reltuples > 10,000 (5,000 for request-path queries).
  - `row-blowup`: any node rows_removed_by_filter + rows / actual_rows_out > 100, or estimated join output > 10x the larger input (catches FCR 16,922x649); plan total rows examined / rows returned > 1,000.
  - `temp-spill`: Sort Method external / Hash Batches > 1 / temp_blks_written > 0 at work_mem=4MB (db-g1-small default).
  - `unbounded-history`: request-path query on a table listed in `append_only_tables` (A3) without a bounded time predicate or LIMIT and without an index whose leading columns match (catches unread count scanning whole history).
  - `missing-fk-index`: static catalog check, every FK column and every column in a WHERE equality of a request-path query has a btree whose leading column matches.
  - `cost-ceiling`: total cost > 50,000 for request paths.
- Runtime: needs Postgres. Laptop Docker or `GOATOS_SQLC_PLAN_ADMIN_DSN` (OCI), same resolution code as `validate-sqlc-query-plans.sh` (reuse, don't fork). Diff-scoped mode `--changed` (queries whose file or referenced tables changed since merge-base) for ci-local; full run in land-main. Target <90s full.
- Existing named assertions in validate-sqlc-query-plans.sh stay as stricter per-query checks.

### A2. `api-latency-budget` (extend existing `tools/perf/api-latency-*`)
- Change `api-latency-policy.mjs`: tiers `target_ms:100` (warn), `p90_ms:200`, `p95_ms:300`, `max_ms:500` (fail). Warn is surfaced in receipt and PR evidence.
- New `tools/perf/route-inventory.mjs`: enumerate every GET route from the chi router (or `openapi` spec used by api-client-generate); fail `route-not-budgeted` if a route is absent from `tools/perf/hot-paths.all.json` and not allowlisted. Auto-generate request templates for parameterless routes; others need a fixture entry.
- Must cover explicitly: notifications unread count, herd-signals live (with N=20 concurrent viewers and 50 events/s injected: new `tools/perf/fanout-viewers.mjs`, fail if DB queries per event > 1 (per-viewer re-read) measured via pg_stat_statements delta).
- Runs against the seeded DB from A1 with the api binary started locally, 30 samples/route after 5 warmups, pool size matching stg (`GOATOS_DB_MAX_CONNS`), with a `pg_sleep`-free cold cache pass. Throttled CPU (`taskset`/`GOMAXPROCS=1`) to approximate db-g1-small shared vCPU.
- Make target: `make api-latency-budget` (new) = inventory check (fast, offline) + gate (DB). ci-local runs inventory always, gate diff-scoped (routes whose handler package changed); land-main runs full.

### A3. `oltp-hygiene-guard` (new, static, fast)
- File: `tools/agent-hooks/check-oltp-hygiene.mjs`, data `deploy/runtime/table-retention.json`.
- Rules:
  - `retention-required`: any migration `CREATE TABLE` whose name/schema matches `(event|events|log|logs|audit|history|telemetry|signal|analytics|_raw)` or which has no UPDATE path and has a timestamp column (heuristic, plus explicit `-- table-class: append-only`) must have an entry in `table-retention.json`: `{table, retention_days, prune_job (binary in workers.json), partition_key|index}`. The guard verifies the prune job exists in `deploy/runtime/workers.json` and its tf schedule exists. Backfill entries now for analytics.app_events, herd-signal events, notifications, domain_event_processed, idempotency keys, outbox.
  - `analytics-schema-ban`: no `INSERT/COPY INTO analytics.*` from `backend/internal/**` or `cmd/goatos-api`; analytics goes to BigQuery/Firebase directly. Only `cmd/analytics-rollup` may read analytics.*, and only with a watermark (A4). New tables in `analytics` schema fail unless allowlisted with retention <= 30 days.
  - `pool-acquire-allowlist`: `pool.Acquire(` / `.Hijack()` / `LISTEN` outside `tools/agent-hooks/pool-acquire-allowlist.json` fails. Allowlisted entries must show `defer conn.Release()` in the same function or be a declared dedicated connection (`platform/worker/stagelock.go`, `herdsignals/.../live_notifications.go`) documented with max concurrent holds. Also require `pgxpool.Config.MaxConnLifetime` and `MaxConnIdleTime` set in pool constructors, and `statement_timeout`/`idle_in_transaction_session_timeout` in the DSN/after-connect hook.
  - `unread-count-shape`: counts over append-only tables on request paths must hit a counter/projection or a partial index (`WHERE read_at IS NULL`); enforced via A1 `unbounded-history`, listed here for docs.
- Runs everywhere, <2s, ci-local `guardrails` block.

### A4. `job-safety-guard` (new; extends worker-stage-budgets + deployed-job-flags)
- File: `tools/agent-hooks/check-job-safety.mjs`; declarations added to `deploy/runtime/workers.json` per job: `{ idempotent: true, watermark: {table, column}, external_deps: [{name, fatal:false}], retry: {max_attempts, backoff}, max_pg_conns, max_runtime_s }`.
- Checks: (1) every Cloud Run job in `infra/envs/{dev,stg}/cloud_run_jobs.tf` has a manifest entry with all fields; (2) `max_retries` in tf <= manifest and task_timeout set; (3) `max_pg_conns` <= 3 and the binary reads it (grep for the env name in `backend/cmd/<job>`); (4) watermark: the job's main package references a watermark read/write (`<job>_watermarks` table or `job_watermarks` shared table; new migration) — full rescans (`SELECT ... FROM <append-only table>` without watermark predicate) fail via A1 run on job queries; (5) external deps: calls to bigquery/storage/http clients must be wrapped by `platform/jobs.NonFatal(dep, fn)` which logs+metrics and returns success-with-degraded; `log.Fatal`/`os.Exit` after an external call fails; (6) crash-loop alert: each job must have a Grafana alert on consecutive failures (checked against grafana-durability-guard's dashboard JSON).
- Static, fast, ci-local.

### A5. `stg-drift-check` (new; runtime, not a PR gate)
- File: `tools/ci/check-stg-drift.mjs`, scheduled workflow `.github/workflows/stg-drift.yml` (hourly) plus Slack bot `/drift`.
- Reads deployed SHA from Cloud Run revision label `commit-sha` (goatos-api, admin-web, kernel-worker, each job image tag) and compares with `origin/main`. Alert to Slack when lag > 24h AND lag contains a commit touching `backend/**`, `infra/**`, `migrations/**`; escalate at 72h. Commits tagged `perf:` or touching `tools/perf/**`/migrations with indexes alert at 4h.
- Also `make stg-drift` for local; land-main prints current drift in the receipt (informational, never blocks merge — deploy authority stays with the launcher/Slack button per CLAUDE.md).
- Deployed-perf closure: PRs whose title/labels say `perf` must add a `deployed-verification:` line in a follow-up ledger (`docs/perf/deploy-ledger.md`) after the drift check sees the SHA live and the latency gate re-ran on stg (`hot-paths.stg-slow.json`).

### A6. Production feedback (cheap, recommended)
- `tools/perf/stg-top-queries.mjs`: weekly pg_stat_statements pull (existing `request-path-usage.sql`) failing on any statement mean > 100ms or total_time share > 10%, and `pg_stat_user_tables.seq_tup_read` growth > 1B/week on any table (would have flagged 14B/10B). Posts to Slack; opens a ledger item.

## B. Skills (repo format: `.agents/skills/<name>/SKILL.md` with YAML `name`/`description` frontmatter, thin entrypoint linking canonical docs; mirror to `.claude/skills/<name>/`)

### B1. `.agents/skills/perf-budget-dev/SKILL.md`
Frontmatter description: "Use when writing any backend query, endpoint, worker/job, migration, or event/log table in goatos. Enforces the 100/200/300/500ms budget on db-g1-small..." Body sections:
1. Budget table and what "request path" means.
2. Before writing: identify table sizes from `tools/perf/table-sizes.stg.json`; classify table (OLTP / append-only / analytics).
3. Query recipes: index-first (leading columns = equality then range), keyset pagination, bounded history windows (`created_at > now()-interval`), counters/projections for counts (unread count -> `notification_unread_counters` maintained on write), pre-aggregate the many side before joining (FCR: join prices by effective date via LATERAL/range index, never cross-compare), `= ANY($1::uuid[])` batching instead of N+1.
4. Evidence: run `make query-plan-budget QUERIES=<name>` and paste EXPLAIN (ANALYZE, BUFFERS) summary; run `make api-latency-budget ROUTES=<id>`.
5. Caching: only with explicit TTL + max entries + invalidation on write; per-viewer live streams must fan out from one shared read (broadcast), never re-query per subscriber.
6. Connections: `pool.Query` default; `Acquire` only with `defer Release` in same function; dedicated long-lived conns need allowlist + separate small pool.
7. Jobs: watermark, NonFatal external deps, backoff, conn cap, idempotent upserts.
8. Escape-hatch format.

### B2. `.agents/skills/perf-budget-review/SKILL.md` (and add as lens in `goatos-code-review/references/kernel-and-scale.md` + `review-lens-ledger.md`)
Blocking checklist (any "no" = REQUEST CHANGES):
- B-1 Every new/changed query has plan-budget output at seeded scale; no seq scan > 5k rows on request path.
- B-2 No join whose output can exceed max(input) x10 without pre-aggregation.
- B-3 Every read of an append-only table is time-bounded or served from a counter/projection.
- B-4 New append-only/event/log tables have retention + prune job in table-retention.json.
- B-5 No analytics writes into Postgres from request paths.
- B-6 No per-viewer or per-event re-read; live streams share one query per event.
- B-7 No Acquire without same-function Release unless allowlisted.
- B-8 Jobs: watermark, external dep non-fatal, retry backoff, conn cap.
- B-9 Latency evidence for touched routes: p95 <= 300, max <= 500; > 100 needs a sentence of justification.
- B-10 Perf-fix PRs name the deploy step and post-deploy verification.
Non-blocking: index bloat, cache sizing, warn-tier routes. The review ledger gets a `perf-budget` lens row; `review-lens-ledger-guard` extended to require it when diff touches `backend/**/queries`, `migrations`, `cmd/*`, or `infra/**/cloud_run_jobs.tf`.

## C. Docs
- `docs/agent-rules/scale-performance.md`: add section "Performance budget (binding)" linking the contract; add anti-patterns: analytics-in-OLTP, unbounded history, cartesian compare, per-viewer re-read, held pool conns, fatal external deps in jobs, merged-not-deployed. Replace "gates run at ~1k rows" with seeded stg-size numbers.
- New `docs/decisions/performance-budget-contract.md`: budgets and tiers; hardware assumption (db-g1-small, 1.7GB RAM, shared vCPU, ~25 conns usable); table classes and retention defaults (events 30d, notifications read >90d pruned, analytics never in PG beyond 7d staging); pool policy (max conns per service, statement_timeout 5s API / 60s jobs, idle_in_tx 10s); job contract; allowlist/expiry rules; ownership.
- `docs/agent-rules/observability.md`: analytics destination rule (BigQuery/Firebase, not Postgres), required job alerts.
- `docs/agent-rules/ci-landing-release.md`: which perf gates run in ci-local vs land-main vs scheduled; drift SLA.
- `.github/PULL_REQUEST_TEMPLATE.md`: new "Performance budget" section:
  ```
  ## Performance budget
  - Tables touched + class (OLTP/append-only/analytics) and seeded row counts:
  - Before/after EXPLAIN (ANALYZE, BUFFERS) summary per changed query (node, rows, loops, buffers, spill):
  - Route p50/p95/max before -> after (`make api-latency-budget` output path):
  - New append-only table retention entry / prune job:
  - Pool/Acquire, job watermark/non-fatal deps changes:
  - Deploy + post-deploy verification plan (perf fixes):
  - perf-budget:allow entries added (reason, owner, expiry):
  ```
  Enforced by extending `local-ci-evidence-guard` or a new `pr-perf-evidence-guard` (checks PR body via gh when the diff touches queries/routes; fails on empty or bare `N/A`).

## D. Rollout order and effort

| # | Item | Where it runs | Speed | Effort |
|---|---|---|---|---|
| 1 | Policy tiers in `api-latency-policy.mjs` + contract doc + scale-performance.md | ci-local (unit tests) | fast | 0.5d |
| 2 | `oltp-hygiene-guard` + `table-retention.json` backfill + pool allowlist (fix analytics.app_events retention first) | ci-local + land-main, static | <2s | 1.5d |
| 3 | `job-safety-guard` + workers.json schema + `platform/jobs.NonFatal` + watermark table (fix analytics-rollup) | ci-local + land-main, static | <2s | 2d |
| 4 | `stg-drift-check` scheduled + land-main receipt line | GH scheduled / Slack | n/a | 1d |
| 5 | Skills B1/B2 + review-lens ledger extension + PR template + evidence guard | ci-local static | fast | 1d |
| 6 | Realistic seed + `query-plan-budget-guard` (static FK/index rules first, EXPLAIN rules second, ratcheting baseline of current violations) | FK/index static: ci-local. EXPLAIN: Docker locally or OCI throwaway DB (`GOATOS_SQLC_PLAN_ADMIN_DSN`); diff-scoped in ci-local, full in land-main | 30-90s | 3-4d |
| 7 | Route inventory (static, ci-local) + `api-latency-budget` gate incl. fan-out viewers test | Needs DB + API binary: OCI throwaway DB; diff-scoped ci-local, full land-main | 3-8 min full | 3d |
| 8 | Weekly stg top-queries/seq_tup_read report | scheduled | n/a | 0.5d |

Fast/local (no DB): oltp-hygiene, job-safety, route-inventory, FK-index static check, PR evidence guard, policy tests, skills/doc guards.
Needs OCI throwaway DB (or Docker): query-plan-budget EXPLAIN rules, api-latency-budget gate, fan-out viewers test. Reuse the DSN resolution already in run-local-ci.sh `query-plans` job so land-main fails closed (not skips) when no DB is reachable, matching validate-sqlc-plans.

Mapping defects -> guards: (1) A3 retention + analytics ban, A4 watermark/non-fatal; (2) A1 row-blowup; (3) A1 seq-scan-large + A6; (4) A1 unbounded-history + A3; (5) A2 fan-out viewers + B-6; (6) A3 pool-acquire + timeouts; (7) A5; (8) A2 tiers + route inventory.

Total ~13-15 engineer-days; items 1-5 (~6d) close defects 1, 4 (partially), 6, 7 without any DB dependency.
