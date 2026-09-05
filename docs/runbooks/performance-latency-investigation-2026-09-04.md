# GoatOS Admin Latency Investigation - 2026-09-04

This note exists so future Codex/Claude work can resume without relying on chat history.

## Hard Constraints

- Do not deploy STG from this work unless Ravi explicitly reverses that instruction.
- Do not run STG migrations, destructive SQL, deletes, truncates, resets, or data rewrites.
- Data checks against shared databases must be read-only.
- For Google/Firebase/GCloud/Drive, use the `ravi@mesha.sg` account and project `goatos-stg` unless Ravi says otherwise.
- Local browser checks should use the CEO/CXO session in Chrome and the local admin-web branch.

## Current Environment Observed

- Repo/worktree: `/Users/raviteja/mesha/goatos-pr175-land`
- Branch: `main`
- GCloud/Firebase account confirmed earlier: `ravi@mesha.sg`
- Firebase project under review: `goatos-stg`
- Android app: `android:sg.mesha.goatos`
- Web app: admin web Firebase app in `goatos-stg`
- Local API test ports seen:
  - `127.0.0.1:18081`
  - `127.0.0.1:18082`
  - `127.0.0.1:18083`
  - `127.0.0.1:18084`
- Local Chrome/admin-web observed on `http://127.0.0.1:3300`
- The machine had both a Cloud SQL proxy and an OCI SSH tunnel open. Verify which DB a local API uses before claiming OCI parity.
- Local API startup reported DB migration drift: DB `000247`, binary `000252`. That limits clean stock after-metrics unless the selected local DB schema matches the branch.
- 2026-09-04 21:46 IST: the approved OCI clone on `127.0.0.1:15432` was migrated
  from `000247` to `000252` after confirming the port was the SSH tunnel, not Cloud SQL.
  This applied pending migrations `000247_breeding_director_role` through
  `000252_leadership_tasks`. No STG deployment and no STG migration was run.

## Telemetry Source

STG admin-web backend fetch telemetry was pulled from Cloud Logging:

```sh
gcloud logging read 'resource.type="cloud_run_revision" resource.labels.service_name="goatos-admin-web-stg" jsonPayload.event_name="admin_backend_api_fetch"' \
  --project=goatos-stg \
  --freshness=24h \
  --limit=3000 \
  --format=json
```

The normalized source table was saved outside the repo at:

```text
/tmp/goatos-stg-admin-web-fetch-normalized-p95.md
```

The expanded latency manifest created from that telemetry is:

```text
tools/perf/hot-paths.stg-slow.json
```

It covers the observed STG endpoint groups with p95 above 1s plus the earlier ten analytics hot paths.

## Slow Endpoint Groups Observed From STG Telemetry

Highest p95 groups from the 24h sample included. A later 12h pull showed the same shape, with feed internal tabs much worse during user testing:

| Endpoint group | STG p95 ms |
| --- | ---: |
| `GET /control-tower/vaccination` | 15856 |
| `GET /calendar/vaccination/events` | 15525 |
| `GET /vaccination/command/cohort-matrix` | 7092 |
| `GET /vaccination/command/drives` | 6362 |
| `GET /vaccination/live-tracker` | 6001 |
| `GET /counts/breakdown` | 4671 |
| `GET /verification/queue` | 4323 |
| `GET /vaccination/adherence` | 3813 |
| `GET /weighing/leadership/growth` | 3793 |
| `GET /vaccination/command/shed-dose-matrix` | 3569 |
| `GET /admin/locations` | 3271 |
| `GET /verification/oversight-analytics` | 3249 |
| `GET /feed-analytics/execution` | 1348 |
| `GET /weighing/shed-weights` | 1277 |
| `GET /feed-analytics/directed` | 1274 |
| `GET /feed-analytics/stock` | 1197 |
| `GET /weighing/weighing-dates` | 1066 |

Later 12h live telemetry highlights:

| Endpoint group | 12h STG/live p95 ms |
| --- | ---: |
| `GET /control-tower/vaccination` | 17892 |
| `GET /calendar/vaccination/events` | 16685 |
| `GET /feed-analytics/stock` | 15947 |
| `GET /feed-analytics/directed` | 15447 |
| `GET /feed-analytics/execution` | 15031 |
| `GET /feed-analytics/shed-feed` | 5156 |
| `GET /weighing/shed-weights` | 3995 |
| `GET /weighing/leadership/growth` | 3793 |
| `GET /feed-analytics/experiment` | 2261 |

Do not present only the earlier ten analytics URLs as the whole problem. The slow set is broader.

## No-Projection Fixes In Progress

Current local patches are limited to no-projection improvements:

- Feed analytics:
  - Keep short read cache for read-only directed/execution/shed-feed analytics.
  - Do not cache `StockAnalytics`; stock must reflect purchase delivery changes immediately.
  - Stock counts must use `stock_kg` and `delivery_status = 'reached'`, not `quantity_kg`.
  - Guard test updated to forbid reintroducing stock cache and require the reached-stock rule.
  - Perf manifests should benchmark execution analytics by rendered section:
    `sections=days,consumption,distribution_completions` for the rolling execution payload and
    `sections=packing_variance` for the day-pinned mismatch table. These are the same backend
    aggregates split by the existing section gate; no projection table or graph number changes.
  - Spend/pricing lookups must use `p.depletes_from <= di.feed_day` and order by
    `depletes_from`, not only `purchase_date`. A reached purchase bought earlier but depleted
    later must not price/feed-balance older directed rows.
- Verification:
  - Parallelize verification queue rows and filter options.
  - Add short in-process cache for filter options and oversight analytics.
  - Invalidate verification read cache on verification writes.
  - Parallelize proof media signed URL resolution with bounded concurrency.
- Vaccination/live tracker:
  - `/app/vaccination/execution` defaults to a mobile-sized read: limit `20`, card summaries off unless explicitly requested.
  - Live tracker operator proof/scan counts are also derived from already-loaded live cells.
  - A worker attempted to make actor attribution best-effort and to hard-disable command-board optional sections. A judge rejected those as correctness regressions.
  - The unsafe command-board shortcut was reverted: optional sections are loaded and only marked unavailable on real section failure.
  - The silent best-effort actor lookup was reverted: actor lookup errors still fail the live-tracker response instead of hiding evidence-only operators.

## Guards Added

- `tools/perf/hot-paths.stg-slow.json` adds a broad STG-slow manifest.
- `tools/perf/api-latency-policy.test.mjs` now checks:
  - committed hot-path manifests include the STG slow/API hot-path list;
  - live tracker uses `business_date`, not `date`;
  - feed execution uses sectioned reads plus bounded `variance_limit` / `completion_limit`;
  - shed-feed analytics does not invent unsupported paging;
  - latency ceilings remain hard-capped.
- `backend/internal/feeddirection/adapters/postgres/analytics_cache_guard_test.go` now checks stock correctness and forbids stock caching.
- `tools/perf/api-latency-policy.test.mjs` now requires every hard-coded over-1s STG
  endpoint family to have an evidence entry in `hot-paths.stg-slow.json`.

## Judge Review Results

Two judge agents reviewed the current work:

- Metrics judge: the earlier ten analytics APIs have valid narrow local pass evidence, but the broad 19-endpoint STG-slow set is not proven fixed. Do not claim the whole app is fast.
- Correctness judge: identified blockers in command-board section removal, feed forecast stock balance, live tracker actor best-effort drop, and verification cache invalidation.

Post-judge patches applied:

- Restored command-board optional sections instead of hardcoding them unavailable.
- Changed feed forecast stock balance to use `feedPurchaseStockKgSQL` (`stock_kg`) with `delivery_status = 'reached'`.
- Changed feed spend/pricing lookups to use `depletes_from <= feed_day` so future reached loads
  do not affect past consumption values.
- Reverted silent best-effort live-tracker actor lookup.
- Added verification cache invalidation after batch close, withdraw, relabel, verdict-applied ack, sampling policy write, and sampling closeout writes.
- Reduced verification read cache TTL to `15s`.

## Validation Already Run In This Pass

```sh
node --test tools/perf/api-latency-policy.test.mjs
go test ./internal/feeddirection/adapters/postgres
go test ./internal/verification/...
```

All three passed after fixing the verification import/formatting issue and the feed stock guard.

Later validation:

```sh
node --test tools/perf/api-latency-policy.test.mjs
git diff --check
GOATOS_SMOKE_ONLY_ROUTES=feed-analytics,weighing-analytics node apps/admin-web/scripts/smoke-visual-live.mjs
```

All passed. The admin-web test suite also passed earlier with `546/546`.

## Measurement Status

- Before metrics: available from STG Cloud Logging telemetry.
- After metrics: partially available, but not clean enough to claim all fixed.
- A full local direct-API benchmark was started against `127.0.0.1:18083`, but the standard gate hung because several endpoints repeatedly reached request/query timeouts.
- A quick per-endpoint probe was run with three samples per endpoint and a 6s cap. Its JSON output is outside the repo at:

```text
/tmp/goatos-stg-slow-latency-after-probe.json
```

- Do not use the first probe run: it accidentally omitted the bearer token from the Node process and every endpoint returned `401 invalid_bearer_token`.
- The corrected probe showed many endpoints still failing or above target; this is not a finished performance fix.
- Because no STG deployment is allowed, Cloud Logging cannot show "after" for local-only code changes.
- If local after metrics are reported, include:
  - API base URL
  - DB target
  - git SHA
  - manifest SHA
  - iterations/warmup
  - failures/timeouts

## Latest Local After Metrics

Run context:

- API base: `http://127.0.0.1:18084`
- Admin web: `http://127.0.0.1:3300`
- DB: OCI Postgres tunnel on `127.0.0.1:15432`
- Schema: migrated to `000252_leadership_tasks`; `feed_purchases` has
  `delivery_status`, `reached_on`, `reached_weight_kg`, and generated `stock_kg`
- Auth/session: CEO/CXO browser session, local bearer token for API gate
- Iterations: broad gate `10` with `4` warmups; focused analytics gate `10` with `4` warmups

Focused Feed/Weighing/Calendar hot paths after local patches:

| Endpoint | Local after p95 ms | Projection needed for this result? |
| --- | ---: | --- |
| `GET /feed-analytics/directed` | 43 | No |
| `GET /feed-analytics/execution?sections=days` | 46 | No |
| `GET /feed-analytics/execution?sections=packing_variance` | 59 | No |
| `GET /feed-analytics/execution?sections=days,consumption,distribution_completions` | 112 | No |
| `GET /feed-analytics/experiment` | 120 | No |
| `GET /feed-analytics/stock` | 173 | No |
| `GET /feed-analytics/shed-feed` | 43 | No |
| `GET /weighing/leadership/growth` | 64 | No |
| `GET /weighing/weight-demographics` | 80 | No |
| `GET /weighing/shed-weights` | 126 | No |
| `GET /calendar/vaccination/events` 7d | 44 | No |
| `GET /calendar/vaccination/events` month page | 70 | No |
| `GET /vaccination/live-tracker` | 43 | No |

Broad STG-slow manifest after local patches:

| Endpoint | Local after p95 ms | Status |
| --- | ---: | --- |
| `GET /calendar/vaccination/events` 7d | 44 | under target |
| `GET /calendar/vaccination/events` month page | 44 | under target |
| `GET /vaccination/command` | 315 | under hard target, above 200 |
| `GET /vaccination/command/drives` | 203 | under hard target, above 200 |
| `GET /vaccination/command/cohort-matrix` | 253 | under hard target, above 200 |
| `GET /vaccination/live-tracker` | 44 | under target |
| `GET /counts/breakdown` | 113 | under target |
| `GET /admin/locations` | 93 | under target |
| `GET /verification/queue` | 366 | under hard target, above 200 |
| `GET /weighing/weighing-dates` | 109 | under target |
| `GET /weighing/leadership/growth` | 63 | under target |
| `GET /weighing/weight-demographics` | 64 | under target |
| `GET /weighing/shed-weights` | 85 | under target |
| `GET /verification/oversight-analytics` | 42 | under target |
| `GET /feed-analytics/execution?sections=days` | 43 | under target |
| `GET /feed-analytics/execution` | 107 | under target |
| `GET /feed-analytics/execution` packing variance | 43 | under target |
| `GET /feed-analytics/experiment` | 118 | under target |
| `GET /feed-analytics/directed` | 44 | under target |
| `GET /feed-analytics/stock` | 197 | under target |
| `GET /feed-analytics/shed-feed` | 195 | under target |
| `GET /control-tower/vaccination` | 975 | still above 500 target |
| `GET /vaccination/command/shed-dose-matrix` | 711 | still above 500 target |
| `GET /vaccination/adherence` | 1128 | still above 1s |

## Projection Decision

No projection is needed for these local improvements:

- Feed directed, feed execution overview, feed execution tab sections, feed experiment, shed-feed.
- Weighing growth/ADG, demographics, shed weights.
- Calendar events and live tracker in the focused local gate.

Projection might be needed after non-projection cleanup for:

- `GET /vaccination/command/shed-dose-matrix`: still `711ms` p95 on OCI after the
  no-projection pass. Query/index/lazy-section work should happen first, but a projection is
  likely if the full matrix must be immediately available on initial navigation.
- `GET /vaccination/adherence`: still `1128ms` p95 on OCI. It needs a deeper read-path pass
  before choosing projection.

Projection is not needed for Feed Stock based on the migrated OCI proof: p95 is `197ms` in the
broad gate and `173ms` in the focused gate.

Projection is not the first fix for:

- Auth/person-access fallback latency.
- Missing indexes.
- Sequential reads that can safely run in parallel.
- Over-fetching large UI sections that can be split by `sections`.
- FE tab waterfalls caused by waiting for non-visible sections.

## Corrected Local Probe Snapshot

Run context:

- API base: `http://127.0.0.1:18083`
- Auth: local bearer token for tenant `00000000-0000-4000-8000-000000000001`
- DB: Cloud SQL proxy on `127.0.0.1:15433`, not proven OCI
- DB migration: `000247_person_access_capability_stock_level`
- Binary expected migration: `000252`
- Samples: 3 per endpoint, 6s cap

Sorted corrected probe results:

| Endpoint | Status | Local p50 ms | Local p95/max ms | Notes |
| --- | --- | ---: | ---: | --- |
| `GET /vaccination/command/shed-dose-matrix` | `ERR,500` | 6000 | 6000 | Timeout/auth grant failure |
| `GET /vaccination/live-tracker` | `ERR,500` | 4506 | 6000 | Timeout/auth grant failure |
| `GET /verification/queue` | `500,ERR` | 6000 | 6000 | Timeout/internal error in this mixed local stack |
| `GET /calendar/vaccination/events` month | `500` | 4694 | 5756 | Calendar request failed |
| `GET /vaccination/command/drives` | `500,200` | 4589 | 5216 | Drive options timeout |
| `GET /weighing/weight-demographics` | `200` | 518 | 4974 | Lands, but spikes |
| `GET /calendar/vaccination/events` 7d | `500` | 4524 | 4765 | Calendar request failed |
| `GET /vaccination/command` | `500` | 4012 | 4763 | Drive options/KPI timeout |
| `GET /weighing/shed-weights` | `200` | 369 | 4542 | Lands, but spikes |
| `GET /feed-analytics/execution` | `500` | 4504 | 4505 | `feed analytics consumption trend rows: timeout` |
| `GET /weighing/leadership/growth` | `500` | 4262 | 4290 | Internal error |
| `GET /vaccination/command/cohort-matrix` | `500` | 3734 | 3775 | Cohort rows timeout |
| `GET /vaccination/adherence` | `500` | 3719 | 3738 | Internal error |
| `GET /control-tower/vaccination` | `500` | 3667 | 3729 | Internal error in corrected probe |
| `GET /verification/oversight-analytics` | `500` | 3554 | 3710 | Timeout in this mixed local stack |
| `GET /counts/breakdown` | `500,200` | 3001 | 3279 | Auth grant lookup failure in one sample |
| `GET /feed-analytics/directed` | `200` | 107 | 1931 | Lands, but cold/spike is still too high |
| `GET /feed-analytics/shed-feed` | `200` | 75 | 1569 | Lands, but cold/spike is still too high |
| `GET /feed-analytics/stock` | `500` | 187 | 410 | Fails because local DB `000247` lacks `delivery_status`/`stock_kg` expected by branch |

## Feed Analytics Findings

- STG telemetry before showed:
  - `GET /feed-analytics/execution`: p95 `1348ms`
  - `GET /feed-analytics/directed`: p95 `1274ms`
  - `GET /feed-analytics/stock`: p95 `1197ms`
  - `GET /feed-analytics/shed-feed`: p95 `419ms` in the normalized 24h STG sample; it is included in the manifest because it was part of the earlier ten analytics hot paths, not because it was over 1s in that STG sample.
- Local corrected probe showed:
  - `execution`: still failing around `4505ms`, with local API log `feed analytics consumption trend rows: timeout: context deadline exceeded`.
  - `directed`: status `200`, p50 `107ms`, p95 `1931ms`.
  - `shed-feed`: status `200`, p50 `75ms`, p95 `1569ms`.
  - `stock`: status `500`, but this is schema drift: DB `000247` lacks `delivery_status` and `stock_kg`.
- Read-only DB inventory:
  - `feed_direction_issues`: `116`
  - `feed_direction_issue_rows`: `55402`
  - `feed_packing_completions`: `4461`
  - `feed_packing_verified_quantities`: `4803`
  - `feed_distribution_completions`: `4704`
  - `feed_external_consumption`: `16`
- `EXPLAIN (ANALYZE, BUFFERS)` for `executionConsumptionSQL` on the same 30-day window completed in about `307ms`. That means the multi-second HTTP timeout is not explained by the SQL plan alone. The local API logs also repeatedly show `person_access_not_provisioned_falling_back_to_role`; auth/person-access fallback and/or shared local DB/API contention must be separated before making another claim.

## Commands To Resume Safely

Check workers:

```sh
git status -sb
git diff --stat
node --test tools/perf/api-latency-policy.test.mjs
cd backend && go test ./internal/feeddirection/adapters/postgres ./internal/verification/... ./internal/vaccinationexecution/...
```

Run broad latency gate only after confirming the local API is listening and its DB/schema are suitable:

```sh
GOATOS_API_BASE_URL=http://127.0.0.1:18083 \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
GOATOS_BEARER_TOKEN="$TOKEN" \
GOATOS_PERF_ITERATIONS=12 \
GOATOS_PERF_WARMUP=5 \
GOATOS_PERF_FAIL_ON_THRESHOLD=false \
node tools/perf/api-latency-gate.mjs \
  --manifest tools/perf/hot-paths.stg-slow.json \
  --output /tmp/goatos-stg-slow-latency-after-local.json
```

Run the narrower feed analytics gate when isolating feed-only backend behavior:

```sh
GOATOS_API_BASE_URL=http://127.0.0.1:18083 \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
GOATOS_BEARER_TOKEN="$TOKEN" \
GOATOS_PERF_ITERATIONS=20 \
GOATOS_PERF_WARMUP=5 \
GOATOS_PERF_FAIL_ON_THRESHOLD=false \
node tools/perf/api-latency-gate.mjs \
  --manifest tools/perf/hot-paths.analytics.json \
  --output /tmp/goatos-feed-analytics-latency-after-local.json
```

If the broad benchmark hangs, identify the active endpoint by temporarily running smaller manifests or adding per-endpoint progress logging to the local copy of the tool. Do not hide timeouts in the final metrics.

## Open Risk

The current branch is behind `origin/main`; rebase/merge must be reviewed before pushing to `main`. Do not push until tests and latency evidence are coherent.

## Feed/Weighing Focus Pass - 2026-09-04 21:11 IST

Ravi asked to make Feed Analytics and Weight Analytics fast without changing numbers, and to compare the same graphs/numbers against the live dashboard. This pass is still local-only; no STG deploy and no DB migration were run.

### Safety Boundary

- No shared DB writes were performed.
- No STG deployment was performed.
- The local API used the real CEO/CXO local token for tenant `00000000-0000-4000-8000-000000000001`.
- The local API was `http://127.0.0.1:18084`.
- The database path was the existing Cloud SQL proxy to `goatos-stg:asia-south1:goatos-stg-core-db` on `127.0.0.1:15432`. If Ravi requires OCI specifically, re-run this same manifest after pointing the local API DSN at the OCI clone and record the DSN label without exposing secrets.
- The DB was still at migration `000247`; branch code expects newer stock columns. Therefore `GET /feed-analytics/stock` is schema-blocked in this local read-only run and must not be reported as a valid after number until the DB schema matches the branch.

### Code Changes In This Focus Pass

- Feed execution UI now requests the already-supported bounded rendered sections for the execution tab:
  `sections=days,consumption,distribution_completions`. The day-pinned mismatch table remains a separate `sections=packing_variance` request.
- Feed analytics perf manifests now enforce that split, including bounded `completion_limit=25` and `variance_limit=25`.
- Weighing analytics cache TTL was reduced to `2s` and write paths invalidate the local read cache after successful commits.
- Weighing heavy analytics reads (`growth`, `shed-weights`, `weight-demographics`) are guarded to use the repository timeout.
- Verification read cache TTL was reduced to `2s` because Cloud Run instances do not share invalidation.
- Broad latency policy now includes the missing STG slow endpoints `/admin/locations` and `/weighing/weighing-dates`.

### Focused Backend After Metrics

Command:

```sh
GOATOS_API_BASE_URL=http://127.0.0.1:18084 \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
GOATOS_BEARER_TOKEN="$TOKEN" \
GOATOS_PERF_ITERATIONS=12 \
GOATOS_PERF_WARMUP=5 \
GOATOS_PERF_TIMEOUT_MS=8000 \
GOATOS_PERF_FAIL_ON_THRESHOLD=false \
node tools/perf/api-latency-gate.mjs \
  --manifest /tmp/goatos-feed-weighing-adminlocations-manifest.json \
  --output /tmp/goatos-feed-weighing-adminlocations-after-12.json
```

Output file:

```text
/tmp/goatos-feed-weighing-adminlocations-after-12.json
```

| Endpoint | STG telemetry before p95 ms | Local after p95 ms | Local p50 ms | Max ms | Status |
| --- | ---: | ---: | ---: | ---: | --- |
| `GET /admin/locations` | 3271 | 188 | 177 | 188 | pass |
| `GET /weighing/weighing-dates` | 1066 | 242 | 234 | 242 | pass |
| `GET /weighing/leadership/growth` | 3793 | 163 | 155 | 163 | pass |
| `GET /weighing/weight-demographics` | 701 in earlier analytics table | 167 | 161 | 167 | pass |
| `GET /feed-analytics/execution` rolling sections | 1348 | 209 | 199 | 209 | pass |
| `GET /feed-analytics/execution` packing variance | 1348 shared group | 129 | 117 | 129 | pass |
| `GET /weighing/shed-weights` | 1277 | 199 | 195 | 199 | pass |
| `GET /feed-analytics/directed` | 1274 | 137 | 118 | 137 | pass |
| `GET /feed-analytics/shed-feed` | 419 in normalized STG sample | 141 | 118 | 141 | pass |
| `GET /feed-analytics/stock` | 1197 | not measured | not measured | not measured | blocked by DB schema `000247` missing branch stock columns |

Current deployed Feed Analytics tab telemetry from `goatos-admin-web-stg` Cloud Logging over the last 6h still shows the live site is slow because these local changes are not deployed:

| Live tab endpoint group | 6h sample count | Live p50 ms | Live p95 ms | Live max ms | Failures |
| --- | ---: | ---: | ---: | ---: | ---: |
| `GET /feed-analytics/stock` | 38 | 818 | 16260 | 18695 | 4 |
| `GET /feed-analytics/directed` | 35 | 1276 | 15946 | 16033 | 4 |
| `GET /feed-analytics/execution` | 36 | 1003 | 15032 | 15150 | 4 |
| `GET /feed-analytics/shed-feed` | 18 | 772 | 7743 | 7743 | 0 |
| `GET /feed-analytics/experiment` | 3 | 1215 | 2261 | 2261 | 0 |

Local branch validation added `feed_experiment_analytics` to the committed latency manifests after this telemetry check; local p95 was `200ms` over 12 samples against `http://127.0.0.1:18084`.

These are backend API hot-path numbers, not a completed browser visual-regression proof. Browser E2E still needs to verify:

- Feed Analytics overview/execution/stock/shed-feed tabs render the same card numbers, tables, and graph points as live.
- Weight Analytics growth/shed/demographics graphs and summary numbers match live.
- Mobile viewport does not overlap filters, graph labels, tabs, or side nav.
- Stock analytics must be re-tested only on a DB schema that contains the branch stock columns.

## Resume Pass - 2026-09-04 23:05 IST

Ravi asked for another judge pass, every page/tab/API coverage, no projection tables, no STG deploy, and PR-only delivery.

### Current Local Truth

- Worktree: `/Users/raviteja/mesha/goatos-pr175-land`
- Local API: `http://127.0.0.1:8080`
- Local admin-web: `http://127.0.0.1:3300`
- DB: OCI Postgres clone via `127.0.0.1:15432`, env file `$HOME/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env`
- Auth: CEO/CXO bearer token for tenant `00000000-0000-4000-8000-000000000001`
- STG deploy: not performed
- Business data writes/deletes/truncates: not performed
- OCI schema state: manually confirmed at `000254_obligation_status_events_terminal_lookup`; index `obligation_status_events_terminal_lookup_idx` exists

### Additional Fixes In This Resume

- Added migration `000254_obligation_status_events_terminal_lookup.sql`:
  `obligation_status_events(tenant_id, obligation_id, occurred_at DESC, obligation_event_id DESC)`
  for terminal vaccination status reconstruction.
- Marked process-integrity and vaccination `completions` / `asof_terminal` CTEs `MATERIALIZED` so each request computes those expensive one-row-per-obligation sets once.
- Parallelized process-integrity row fetch with count/adherence-summary fetch in `ListRows`, reducing Action Center / Protocol Adherence / Control Tower wall-clock time without changing the query results.
- Kept the previous feed/weighing/verification safety guards: no stock cache, stock uses reached `stock_kg`, and caches use invalidation/epoch protection.

### Broad 88-Endpoint Result Is Not A Pass

Command output file:

```text
/tmp/goatos-admin-all-oci-after-parallel.json
```

Run settings:

- Manifest: `tools/perf/hot-paths.admin-all.json`
- Endpoints: `88`
- Iterations: `5`
- Warmup: `3`
- Fail on threshold: `false`
- Result: not a clean pass. This run is exploratory only.

Summary:

| Metric | Before local broad | After `000254` + materialized CTE | After parallel row/count reads |
| --- | ---: | ---: | ---: |
| APIs over 1s p95 | 5 | 1 | 0 |
| APIs over 500ms p95 | 7 | 5 | 4 |
| APIs over 300ms p95 | not recorded | 13 | 13 |

Do not report the table above as “all 88 APIs fixed”. It buckets latency for
successful samples only. The referenced artifact had top-level `passed=false`
and request failures for several endpoints, so it is useful for triage but not
release evidence.

Remaining endpoints above 500ms in the latest full 88-route run:

| Endpoint | Latest p50 ms | Latest p95 ms | Latest max ms | Projection needed first? |
| --- | ---: | ---: | ---: | --- |
| `GET /vaccination/command/shed-dose-matrix` | 644 | 663 | 663 | No first; still has SQL/payload options |
| `GET /vaccination/adherence` | 530 | 590 | 590 | No first; process-integrity query was cut in half, deeper SQL still possible |
| `GET /operations/kernel-health` | 184 | 528 | 528 | No, likely probe/index/health query cleanup |
| `GET /vaccination/action-center` | 510 | 513 | 512 | No first; evidence media/payload/query cleanup likely |

## Feed Sync + Production Chrome Pass - 2026-09-05 00:20 IST

Ravi asked to make local OCI carry the same Feed/Weighing analytics data as STG so local
analytics numbers can be compared against the live dashboard, then keep optimizing without
projection tables.

### Database Safety

- No STG deployment was performed.
- No STG database writes were performed.
- OCI backup before sync:
  `/Users/raviteja/mesha/local-data/goatos-stg-to-oci/backups/20260904-234011/oci-feed-weight-before-sync.dump`
- New sync tool:
  `tools/data/sync-stg-oci-analytics-parity.mjs`
- Feed sync was applied to local OCI only with `--scope=feed --allow-oci-only --apply`.
- The feed sync uses CSV staging, primary-key upserts, no deletes/truncates, and no-op update
  guards. It maps old STG purchases into the newer OCI stock schema as reached stock
  (`delivery_status='reached'`, `reached_on=purchase_date`) so Feed Stock can compute against
  branch code.
- The sync tool must not copy `proof_artifacts`; proof rows include storage pointers and copying
  them without copying object bytes can break evidence availability. The tool also validates tenant
  IDs as UUIDs and refuses sync targets outside the local OCI tunnel `127.0.0.1:15432/goatos`.
- Weighing sync was attempted transactionally and aborted before commit because the current
  OCI schema guard rejected this STG row shape:
  `weighing: operator e2b91f33-f1c0-5814-9b9f-2718de338f1a is not scoped to park 00000000-0000-4000-8000-000000003002`.
  Do not bypass this guard just to make local numbers match.
- The sync tool now redacts database URLs on command failures after a local run exposed the OCI
  DSN in Node's `execFileSync` error text.

### Read-Only Parity Agent Result

Agent Lorentz independently checked STG vs OCI without writing:

- STG identity: Cloud SQL `goatos-stg:asia-south1:goatos-stg-core-db`, DB `goatos`, user
  `goatos_app`, latest schema `000247_person_access_capability_stock_level`.
- OCI identity: `127.0.0.1:15432 -> 10.88.0.2:5432`, DB `goatos`, user `postgres`, latest
  schema `000254_obligation_status_events_terminal_lookup`.
- Feed fact tables now match for direction issues/rows, effective external consumption,
  transport tasks, packing completions, distribution completions, verified quantities, and wastage
  completions.
- Weighing core facts mostly match (`weighing_campaigns`, `weighing_observations`,
  `weighing_shed_observations`), but OCI is still not exact same-data evidence for all Weight
  Analytics because `goats`, `shed_profiles`, `weighing_campaign_sheds`, and
  `weighing_shed_observation_proofs` differ.

### Backend Evidence After Feed Sync

Command output:

```text
/tmp/goatos-analytics-oci-after-feed-sync-rerun.json
```

Run context:

- API: `http://127.0.0.1:8080`
- DB: local OCI tunnel `127.0.0.1:15432`
- Manifest: `tools/perf/hot-paths.analytics.json`
- Iterations/warmup: `15` / `5`
- Result: `passed=true`

| Endpoint | p50 ms | p95 ms | max ms |
| --- | ---: | ---: | ---: |
| `GET /feed-analytics/stock` | 196 | 271 | 271 |
| `GET /weighing/shed-weights` | 104 | 208 | 208 |
| `GET /feed-analytics/experiment` | 135 | 144 | 144 |
| `GET /feed-analytics/execution` rolling | 124 | 136 | 136 |
| `GET /weighing/weight-demographics` | 86 | 130 | 130 |
| `GET /feed-analytics/execution` overview days | 62 | 105 | 105 |
| `GET /feed-analytics/shed-feed` | 63 | 102 | 102 |
| `GET /weighing/leadership/growth` | 83 | 91 | 91 |
| `GET /vaccination/live-tracker` | 65 | 75 | 75 |
| `GET /calendar/vaccination/events` 7d | 63 | 67 | 67 |
| `GET /calendar/vaccination/events` month | 63 | 66 | 66 |
| `GET /feed-analytics/directed` | 62 | 64 | 64 |
| `GET /feed-analytics/execution` packing variance | 62 | 64 | 64 |

Backend Feed/Weight analytics reads are no longer the main reason a tab feels slow locally.

### Production Chrome Evidence

The first production Chrome tab test found a real FE failure: Feed Stock requested the RSC payload
successfully but the URL stayed on overview and `SegmentedLinks` remained `aria-busy=true`, causing
the test to wait 45s. This was a client navigation bug, not a 45s API call.

Fixes added:

- `apps/admin-web/components/segmented-links.tsx`: pending state now clears on current URL/tab
  change and has a bounded fallback. If Next client router does not commit within 750ms, the link
  falls back to real browser navigation.
- `apps/admin-web/scripts/measure-sidebar-click-latency.mjs`: failure rows now write `ok:false`
  explicitly; weighing tab labels were corrected to current backend contract labels `Pen-wise` and
  `Comparison`; sidebar route waits assert the exact pathname and query string so compose/tab/scope
  variants cannot be under-measured as the wrong page.

Production local server:

```text
http://127.0.0.1:3301
```

Chrome tab output after the navigation fix:

```text
/tmp/goatos-feed-weighing-tab-clicks-prod-3301-after-label-fix.ndjson
```

| UI tab click | p50 ms | p95 ms | max ms | Failures |
| --- | ---: | ---: | ---: | ---: |
| Feed Overview | 177 | 216 | 216 | 0 |
| Feed Stock | 1272 | 1279 | 1279 | 0 |
| Feed Per Animal | 1260 | 1372 | 1372 | 0 |
| Feed Experiment | 466 | 1432 | 1432 | 0 |
| Feed Execution | 543 | 1252 | 1252 | 0 |
| Weighing General | 307 | 356 | 356 | 0 |
| Weighing Breed-wise | 1277 | 1533 | 1533 | 0 |
| Weighing Birth-wise | 783 | 1276 | 1276 | 0 |
| Weighing Pen-wise | 831 | 1642 | 1642 | 0 |
| Weighing Weight-wise | 715 | 909 | 909 | 0 |
| Weighing Time-wise | 755 | 856 | 856 | 0 |
| Weighing Comparison | 891 | 985 | 985 | 0 |

Interpretation:

- The stuck/timeout bug is fixed.
- Browser-visible tab clicks are still too slow for the requested target. They are now mostly
  0.7-1.6s because each internal analytics tab is still a server route navigation, even though the
  backend reads are mostly below 300ms p95.
- The next optimization should be frontend tab architecture: render/fetch the tab data once for
  Feed and Weighing analytics and switch panels on the client, or introduce a route cache/prefetch
  strategy that actually commits before fallback. This can be done without projection tables, but
  must be checked carefully so graph/table numbers stay identical.

### Final Strict Tab Result After Link + Prefetch

After judge review, the tab measurement and navigation patches were refined again:

- `SegmentedLinks` now uses Next `<Link prefetch scroll={false}>`, proactively prefetches
  non-current segmented options on mount/update, and cancels/normalizes the hard-navigation fallback.
- The Playwright tab harness now asserts query params and removes tabbar text before matching
  destination content, so it cannot pass just because a tab label already exists.
- The harness can pause after page landing with `ADMIN_WEB_SIDEBAR_SETTLE_MS` to measure the
  user-realistic warmed-tab path after route prefetch has completed.
- This is warmed-prefetch evidence, not cold first-click evidence. Keep the distinction visible in
  every summary.

Command output:

```text
/tmp/goatos-feed-weighing-tab-clicks-prod-3301-strict-prefetch-settle.ndjson
```

Run context:

- Admin web: `http://127.0.0.1:3301`
- API: `http://127.0.0.1:8080`
- DB: local OCI tunnel `127.0.0.1:15432`
- Browser: Playwright Chrome channel, production Next build, CEO/CXO local bearer cookie
- Rounds: `4`
- Settle before tab clicks: `1200ms`
- Failures: `0`

| UI tab click | p50 ms | p95 ms | max ms |
| --- | ---: | ---: | ---: |
| Feed Per Animal | 130 | 260 | 260 |
| Weighing Breed-wise | 103 | 259 | 259 |
| Weighing Birth-wise | 99 | 226 | 226 |
| Feed Experiment | 136 | 219 | 219 |
| Feed Overview | 120 | 161 | 161 |
| Feed Execution | 145 | 157 | 157 |
| Feed Stock | 131 | 137 | 137 |
| Weighing General | 93 | 130 | 130 |
| Weighing Weight-wise | 83 | 108 | 108 |
| Weighing Pen-wise | 88 | 104 | 104 |
| Weighing Comparison | 85 | 103 | 103 |
| Weighing Time-wise | 95 | 100 | 100 |

This is the first clean Chrome evidence for Feed/Weighing tab UX under the requested target.
It depends on route prefetch having a short opportunity to warm after the page lands. Immediate
cold-first clicks still need a deeper client-panel architecture if Ravi wants them below 300ms
without any warmup.

Validation after these patches:

```sh
npm run build
node --check apps/admin-web/scripts/measure-sidebar-click-latency.mjs
node --check tools/data/sync-stg-oci-analytics-parity.mjs
node --test tools/perf/api-latency-evidence.test.mjs
node --test apps/admin-web/scripts/smoke-visual-route-coverage.test.mjs \
  apps/admin-web/features/feed/feed-analytics.test.mjs \
  apps/admin-web/features/weighing/weights-window.test.mjs \
  apps/admin-web/scripts/perf-capture-budget-contract.test.mjs
git diff --check
```

All passed.

Important feed/weighing result from the same latest 88-route run:

| Endpoint | Latest p95 ms |
| --- | ---: |
| `GET /feed-analytics/stock` | 258 |
| `GET /feed-analytics/execution` rolling sections | 135 |
| `GET /feed-analytics/execution` packing variance | 76 |
| `GET /feed-analytics/experiment` | 140 |
| `GET /feed-analytics/directed` | 68 |
| `GET /feed-analytics/shed-feed` | 84 |
| `GET /weighing/shed-weights` | 109 |
| `GET /weighing/weight-demographics` | 84 |
| `GET /weighing/leadership/growth` | 90 |
| `GET /weighing/weighing-dates` | 131 |
| `GET /calendar/vaccination/events` week | 79 |
| `GET /calendar/vaccination/events` month | 71 |
| `GET /vaccination/live-tracker` | 69 |

### Focused Slow-Set Confirmation

Output file:

```text
/tmp/goatos-hot-slow-after-parallel.json
```

Run settings: `20` iterations, `5` warmups.

| Endpoint | Before this resume p95 ms | After this resume p95 ms |
| --- | ---: | ---: |
| `GET /vaccination/adherence` | 923 | 617 |
| `GET /control-tower/vaccination` | 737 | 516 |
| `GET /vaccination/action-center` | 802 | 655 |
| `GET /vaccination/command/shed-dose-matrix` | 811 | 750 |
| `GET /sales/deals` | 159-754 spike in broad run | 159 |

### Judge Status

- Backend judge `Meitner` is reviewing the current dirty diff read-only.
- Frontend/telemetry judge `Helmholtz` is reviewing the current dirty diff read-only.
- Do not push until both judge findings are handled or explicitly documented.

### Remaining Work Before PR

- Run frontend contract tests after any doc/manifest edits.
- Run Chrome visual smoke on `127.0.0.1:3300` with CEO/CXO local session and feed analytics tab routes.
- Run one final broad API gate after any judge-requested changes.
- Commit on a PR branch based on `origin/main`; do not push directly to `main` unless Ravi explicitly confirms direct main push again at that moment.

## 2026-09-05 Resume Status

Current constraints remain unchanged: no STG deploys, no destructive SQL, no data rewrites. All
measurements below are local API measurements against the OCI DB tunnel using the CEO bearer token.

Local API:

- Binary: rebuilt from this dirty worktree.
- API base: `http://127.0.0.1:8080`
- DB target: OCI Postgres tunnel via `127.0.0.1:15432`
- Warning still present: DB migration ledger `000254`, binary migration set `000259`.
- Local pool used for the route-switch gate: `GOATOS_PG_MAX_CONNS=10`.

New no-projection patches in this resume:

- Calendar list cache TTL increased from `30s` to `60s`.
- Process-integrity read/count cache TTL increased from `30s` to `60s`, and `as_of` cache bucketing moved
  from one minute to five minutes to avoid route-switch cache churn.
- Vaccination execution/schedule/operations/shed read cache TTL increased from `30s` to `60s`, with five
  minute `as_of` buckets.
- Vaccination shed summary now caches the fully decorated service response for `60s`; this covers both the
  canonical shed aggregate and the workforce manager/backup decoration.
- Added a shed-summary service guard proving a repeated same-shape route switch only calls the shed repo and
  ownership reader once.

Focused feed/weighing/calendar analytics gate:

Output file:

```text
/tmp/goatos-analytics-oci-after-cache-bucket-seq.json
```

Result: passed.

| Endpoint | Local p95 ms | p99 ms |
| --- | ---: | ---: |
| `GET /vaccination/live-tracker` | 64 | 81 |
| `GET /calendar/vaccination/events` 7d | 53 | 53 |
| `GET /calendar/vaccination/events` month | 57 | 62 |
| `GET /weighing/leadership/growth` | 82 | 92 |
| `GET /weighing/weight-demographics` | 77 | 77 |
| `GET /weighing/shed-weights` | 101 | 102 |
| `GET /feed-analytics/directed` | 62 | 64 |
| `GET /feed-analytics/execution?sections=days` | 54 | 56 |
| `GET /feed-analytics/execution?sections=days,consumption,distribution_completions` | 143 | 147 |
| `GET /feed-analytics/execution?sections=packing_variance` | 67 | 69 |
| `GET /feed-analytics/experiment` | 49 | 63 |
| `GET /feed-analytics/stock` | 85 | 93 |
| `GET /feed-analytics/shed-feed` | 59 | 63 |

Default sidebar/API gate after shed service cache:

Output file:

```text
/tmp/goatos-default-sidebar-oci-after-shed-service-cache-pool10.json
```

Result: one combined-suite failure only: completed-history p95 `824ms`. The same endpoint isolated immediately
afterward passed at p95 `58ms` / p99 `72ms`, so do not present the combined-suite spike as a proven endpoint
regression without a fresh isolated repro.

| Endpoint | Combined gate p95 ms | Isolated p95 ms |
| --- | ---: | ---: |
| `GET /control-tower/vaccination` | 57 | not rerun isolated |
| `GET /vaccination/action-center` | 61 | not rerun isolated |
| `GET /vaccination/adherence` | 59 | not rerun isolated |
| `GET /calendar/vaccination/events` | 58 | not rerun isolated |
| `GET /calendar/vaccination/events?status=completed` | 824 | 58 |
| `GET /calendar/vaccination/events?include_date_markers=true` | 60 | 65 |
| `GET /vaccination/schedule` | 52 | 62 |
| `GET /vaccination/execution` | 55 | not rerun isolated |
| `GET /vaccination/operations` | 58 | not rerun isolated |
| `GET /vaccination/sheds` | 55 | not rerun isolated |

Validation in this resume:

```sh
go test ./internal/calendar/adapters/postgres ./internal/calendar/adapters/http ./internal/processintegrity/adapters/postgres ./internal/processintegrity/adapters/http ./internal/vaccinationexecution/app ./internal/vaccinationexecution/adapters/http ./internal/vaccinationexecution/adapters/postgres -count=1
go test ./internal/vaccinationexecution/app -count=1
```

Both passed.

Active judge agents:

- Backend judge `Peirce` (`01a06ea7-63e0-72d3-84f0-920123029505`) is reviewing backend latency/correctness.
- Frontend/perf judge `Meitner` (`01a06ea7-671c-73c3-8bb5-09ef7efa8f9e`) is reviewing admin-web and perf tooling.

Current pending before PR:

- Wait for Peirce/Meitner and address any blocker findings.
- Run full selected Go + Node tests after judge-driven edits.
- Run Chrome visual/route E2E on `http://127.0.0.1:3300` using the CEO/CXO local session.
- Produce final before/after table from saved JSON outputs and STG telemetry.
- Rebase/cut a clean PR branch from `origin/main`; do not push or deploy STG from this state.

## 2026-09-05 Local OCI Resume: Remaining >500ms Reads

Constraints for this pass:

- No STG deploy.
- No DB mutation.
- Local API only, pointed at the OCI STG clone environment.
- Separate broken manifest rows from real latency rows.

Input evidence:

- Broad admin run: `/tmp/goatos-admin-all-oci-r1.json`.
- Focused analytics run: `/tmp/goatos-analytics-oci-final-r4.json`.
- Targeted local reruns:
  - `/tmp/goatos-targeted-real-latency-r1.json`
  - `/tmp/goatos-targeted-real-latency-r2.json`
  - `/tmp/goatos-targeted-real-latency-r3.json`
  - `/tmp/goatos-weighing-shed-only-r1.json`

False manifest failures from the broad admin run, not latency:

- `feed_direction_preview`: manifest passed `date=`, endpoint requires `target_date=`.
- `feed_packing_worklist`: manifest passed `date=`, endpoint requires `target_date=`.
- Several `feed-config/*` rows returned 400 because the broad manifest omitted required `park_id`.
- `workforce_clock_entries` returned 403 for the CEO token, so it is an authorization/manifest issue.
- `vaccination_operator_assignment_config` returned 409 because the manifest did not pick a park scope.
- `pc_care_tasks` returned 400 due invalid/missing parameters.

Safe no-projection patches made in this pass:

- `backend/internal/growthdirector/adapters/postgres/repository.go`: Growth Director read cache TTL changed from the initial 2s burst to 30s.
- `backend/internal/growthdirector/adapters/postgres/growth_director.go`: keeps the fixed parameter typing and wraps the full weights response in same-key read cache/in-flight coalescing.
- `backend/internal/weighing/adapters/postgres/weighing_dates.go`: added epoch-guarded read cache/in-flight coalescing for the date-picker read. Weighing writes already invalidate this cache epoch.
- `backend/internal/vaccinationexecution/adapters/postgres/repository.go`: vaccination read cache TTL is 30s.
- `backend/internal/vaccinationexecution/adapters/postgres/commandboard.go`: cached the command board summary, cohort matrix, and drive options by tenant/scope/drive/as_of bucket.
- `backend/internal/vaccinationexecution/adapters/postgres/commandboard_drilldown.go`: cached the shed-dose matrix by tenant/scope/drive/as_of bucket.

Focused tests after these patches:

```sh
go test ./internal/growthdirector/... ./internal/weighing/... ./internal/vaccinationexecution/... -count=1
```

Result: passed.

Sorted before/after from `/tmp/goatos-admin-all-oci-r1.json` versus `/tmp/goatos-targeted-real-latency-r3.json`:

| Endpoint | Before p95 | Before p99 | After p95 | After p99 | Status |
| --- | ---: | ---: | ---: | ---: | --- |
| `sales_overview` | 2120.9ms | 2120.9ms | 229.8ms | 448.7ms | pass |
| `vaccination_command_board` | 2012.8ms | 2160.4ms | 86.1ms | 101.4ms | pass |
| `procurement_loadwise_weights` | 1637.0ms | 3080.5ms | 210.0ms | 244.2ms | pass |
| `vaccination_command_shed_dose_matrix` | 1494.7ms | 2504.6ms | 82.4ms | 85.3ms | pass |
| `verification_video_log` | 1301.3ms | 3070.0ms | 142.4ms | 198.0ms | pass |
| `vaccination_command_cohort_matrix` | 1280.1ms | 2292.8ms | 88.4ms | 122.1ms | pass |
| `feed_config_pens` | 1256.2ms | 1475.4ms | 88.4ms | 116.0ms | pass |
| `feed_config_shed_tags` | 1249.5ms | 1353.3ms | 81.9ms | 83.3ms | pass |
| `procurement_vendor_catalog` | 936.1ms | 1944.4ms | 147.1ms | 205.4ms | pass |
| `feed_execution_analytics` | 908.0ms | 2106.9ms | 156.0ms | 165.7ms | pass |
| `verification_sampling` | 437.0ms | 444.4ms | 165.6ms | 378.8ms | pass |
| `vaccination_command_drives` | 221.4ms | 1110.5ms | 85.3ms | 89.0ms | pass |
| `counts_breakdown` | 141.0ms | 141.5ms | 162.7ms | 199.9ms | pass |
| `weighing_dates` | 132.5ms | 136.3ms | 266.3ms | 395.7ms | pass |
| `admin_web_bootstrap` | 106.7ms | 111.7ms | 231.2ms | 265.9ms | pass |
| `counts_herd_analytics` | 78.3ms | 78.7ms | 125.5ms | 198.9ms | pass |
| `feed_execution_overview_days` | 78.0ms | 206.5ms | 82.4ms | 84.3ms | pass |
| `weighing_leadership_growth` | 77.3ms | 84.5ms | 130.2ms | 139.1ms | pass |
| `feed_stock_analytics` | 74.0ms | 75.4ms | 124.9ms | 128.9ms | pass |
| `feed_directed_analytics` | 53.1ms | 58.2ms | 81.8ms | 265.4ms | pass |
| `feed_shed_feed_analytics` | 51.8ms | 58.8ms | 83.2ms | 97.5ms | pass |
| `growth_director_weights` | failed/500 | failed/500 | 124.5ms | 141.9ms | pass |

`weighing_shed_weights` note:

- In the all-suspect R3 sweep it showed p95 `616.2ms` and p99 `3757.4ms`.
- Isolated immediately afterward over 60 measured samples it passed at p95 `172.6ms`, p99 `244.3ms`.
- Treat the all-suspect R3 row as contention noise unless a later isolated repro fails again.

Remaining recommendations without projection:

- Counts: do not add an uninvalidated cache. If counts tails recur, add a small epoch-invalidated read cache to the counts repository and invalidate it after every counts/shifting/milk commit. That is safe but touches many write paths, so it was not bundled into this narrow pass.
- Procurement vendor catalog: R3 passed without a new code patch. If it regresses under concurrent Chrome E2E, add a small repository read cache and invalidate after vendor catalog/vendor writes.
- Vaccination command board: the no-projection fix is now cache/coalescing plus existing query splitting. A deeper SQL rewrite may still be useful later, but current local p95/p99 is under target.
