## Counts / Alerts / Health endpoints: DB audit (read-only)

Method: SQL pulled straight from the Go constants in `goatos-pr-burst/backend`, real params (tenant `...0001`, park `...3001` = 995 goats, window 2026-03-24..2026-09-24, busiest counts-alert member `04b544bc…`). Each statement ran as `BEGIN READ ONLY; EXPLAIN (ANALYZE, BUFFERS) …; ROLLBACK`. The hot ones were also run as `PREPARE` + `plan_cache_mode=force_generic_plan`, because pgx caches prepared statements. Plans are in `scratchpad/plans/*.txt`.
Clone size: goats 1,741 (1,569 alive), locations 179, notification_requests 228k (179k in the last 30 d), feed_direction_issue_rows 83k. **health_treatment_sessions is empty on the clone**, so for health only the plan shape was checked.
DB ms below = planning + execution for all statements in one request, custom plan. Generic plans came out the same or faster.

| endpoint | handler file:func | #SQL/req | DB ms | seq scans (>1k rows) / spills | grade | fix |
|---|---|---|---|---|---|---|
| GET /herd-register/summary | counts/adapters/http/handler.go:GetSummary → repository.go:GetHerdRegisterSummary | 1 | ~11 (6 plan + 5 exec) | goats seq scan (1.7k); NOT EXISTS probes goat_identifiers 767x via index | PASS | – |
| GET /counts/breakdown | handler.go:GetBreakdown → repository.go:GetCountsBreakdown (pgx.Batch, 1 RTT) | 4 (page/pens, charts, facets, loads) | ~90 (41 plan + 50 exec; facets 23 ms exec) | goats seq scanned 8x per request (facets 6x, page 1x, charts 1x), goat_shed_partitions 4x, goat_identifiers 3.2k 1x; no spills | PASS (close to OK) | facets: build the `goats ⋈ gsp` base once in a MATERIALIZED CTE; see note |
| GET /counts/herd-analytics | handler.go:GetHerdAnalytics → herd_analytics.go:GetHerdAnalytics (batch) | 2 | ~22 (13.5 plan + 8.7 exec) | goats 2x | PASS | – |
| GET /counts/mortality | handler.go:GetMortality → app/mortality.go → mortality.go:GetMortality (batch) | 3 (population, deaths, recent) | ~66 (45 plan + 22 exec) | goats 3x, goat_shed_partitions 1x; quicksorts in memory only | PASS | planning is 2/3 of the cost; see note |
| GET /counts/milk-preparation and /app/counts/milk-preparation | handler.go:GetMilkPreparation → milk_preparation.go:GetMilkPreparation | 1 | ~13 | goats 1x, gsp 1x | PASS | – |
| POST /app/counts/milk-preparation/submit | handler.go:SubmitMilkPreparation | ~4-6 tx stmts + proof validation + verification enqueue | not run (write) | idempotency lookups use keys | n/a | – |
| GET /app/counts/milk-feeding/tasks | handler.go:ListMilkFeedingTasks → milk_feeding.go:ListMilkFeedingTasks | 1 | ~5 | none | PASS | – |
| POST /app/counts/milk-feeding/tasks/{id}/submit | handler.go:SubmitMilkFeeding → milk_feeding.go:SubmitMilkFeeding | 5-7 in one tx (idempotency select, FOR UPDATE task, goats ANY(), update, watchlist FOR UPDATE, attempts insert) | not run (write) | PK/idempotency keyed | n/a | – |
| GET /app/counts/alerts | handler.go:ListAlerts → counts/adapters/postgres/alerts.go:ListAlerts | 1 | ~7 (5.6 plan + 1.5 exec) | none; uses `notification_requests_counts_alerts_idx` (partial) | PASS | – |
| GET /alerts/rows | alerts/adapters/http/handler.go:Rows → alerts/app/service.go:List | ~10-12 (config, event rules, park names in parallel; penFeedDay x2, penMovements, LowStockFeeds, 1 per enabled event kind: births, exits, added, shifting, feed purchases), fan-out cap 4 | ~135 summed (LowStockFeeds alone 72 = 11.5 plan + 60.6 exec; the rest are 0.1-3.5 exec, 2-10 plan) | **feed_direction_issue_rows parallel seq scan over 83k rows (all history)** in feedLowStockSQL | OK | roll up the low-stock read or cache it (see note) |
| GET /alerts/config | alerts handler.go:GetConfig | 2 (listRuleConfigSQL, listEventRulesSQL) | ~5 | workforce_members (42 rows) only | PASS | – |
| PUT/POST/DELETE /alerts/config* | SetConfig / CreateEventRule / UpdateEventRule / DeleteEventRule | 3-4 in a tx (idempotency reserve/select/complete + upsert) | not run (write) | keyed | n/a | – |
| GET /app/health/work-items | health/adapters/http/handler.go:ListWorkItems → health/adapters/postgres/repository.go:ListWorkItems | 1 | ~12 (11.8 plan, table empty) | none; uses health_sessions_worklist_idx, locations unique index, steps order idx | PASS (volume not verified) | – |
| GET /app/health/work-items/{id} | GetWorkItem → repository.go:GetWorkItem | 2 (header row, steps) | <15 expected | keyed by PK | PASS | – |
| POST /app/health/work-items/{id}/complete | CompleteWorkItem → service.CompleteWorkItem | tx + treatment-verification enqueue | not run (write) | keyed | n/a | – |

### Main finding: the known stg p50/p95 numbers are not explained by DB time on these queries

Every GET in scope finishes in **≤ 135 ms of DB time on the clone**. The four known-slow endpoints measure mortality ~66 ms, herd-analytics ~22 ms, breakdown ~90 ms and alerts/rows ~135 ms. Stg shows 1.1-4.6 s, which leaves 1-4 s spent outside the SQL. Likely causes, in order:

1. **Waiting for a pool connection.** Other workloads hold connections (below). alerts/rows makes this worse because it takes up to 4 connections per request (`listConcurrency = 4`, plus 3 setup goroutines).
2. **Round trips from a Cloud Run instance to the DB.** The batched endpoints use one round trip, but alerts/rows makes about 10 sequential and parallel calls.
3. **Planning cost.** Planning is 40-70% of DB ms here: mortality's 3 statements take 45 ms to plan and 22 ms to run. Planning is paid again whenever a request gets a fresh connection, because pgx's statement cache is per connection.

What to do: capture `pgxpool.Stat()` (AcquireDuration, EmptyAcquireCount) and a server-timing split (acquire / db / encode) on stg before indexing anything else here.

### Where the hot table stats come from

- **locations (41M seq scans / 6.2B tuples): not these endpoints.** Every in-scope plan hashes locations once per statement (`loops=1`, 179 rows). Per request that is about 3 scans for mortality, 7-8 for breakdown and 1 for herd-analytics, so at most ~1.5k tuples per request. That cannot add up to 41M scans. The source has to be a nested loop that re-scans locations for every outer row somewhere else. Candidates: `platform/oploc/resolve.go`, `obligation/*`, `calendar/*`, `workforce/*`. A plan-level search for `Seq Scan on locations … loops=N` is needed.
- **notification_requests (1.5B seq tuples): not these endpoints.** `/app/counts/alerts` uses the partial `notification_requests_counts_alerts_idx` under both custom and generic plans. `/alerts/rows` never reads the table. With 22k seq scans × ~70k average rows, the likely culprit is a notification dispatcher or queue query that ignores the `status IN ('queued','failed')` partial indexes, or another module's alert feed whose `message_key` prefix has no partial index (only counts/feed/vaccination/weighing have one).

### Notes on endpoints that did not PASS, and cheap wins

**/alerts/rows (OK, ~135 ms, stg 487/3231)**
- `feeddirection/adapters/postgres/analytics.go:LowStockFeeds` (feedLowStockSQL) sums **every locked issue row ever** (83k rows, parallel seq scan, 51 ms GroupAggregate into 700 groups) to get the balance and average daily use. That cost grows with history on every alerts load.
  - Fix A (preferred), a rollup: a `feed_issue_daily_totals(tenant_id, park_id, feed_item_key, feed_day, kg)` table maintained when an issue is locked. The query then reads about 700 rows instead of 83k and should come in under 5 ms.
  - Fix B, a cache: low stock does not depend on the park or date parameters (`LowStock(tenantID, withinDays)`), so cache it per tenant for 60-300 s in the alerts service.
  - Fix C, an index-only interim: `CREATE INDEX CONCURRENTLY feed_direction_issue_rows_tenant_issue_qty_idx ON feed_direction_issue_rows (tenant_id, feed_direction_issue_id) INCLUDE (feed_item_key, quantity_kg) WHERE quantity_kg IS NOT NULL;`. This allows an index-only scan joined to the 180 locked issues. It saves heap reads but still touches every row.
- Fan-out: 10-12 statements, up to 4 connections at once. Cut the pressure by batching the 5 event reads and 2 penFeedDay reads into one `pgx.Batch` on one connection (one round trip, one connection). Also set `listConcurrency` to 2 on stg.

**/counts/breakdown (PASS at 90 ms, stg p95 4203)**
- `countsBreakdownFacetsSQL` scans `goats` 6 times and `goat_shed_partitions` twice in 23 ms. Change it to a single `WITH base AS MATERIALIZED (goats ⋈ gsp ⋈ locations WHERE tenant AND lifecycle)` and derive each facet from `base`. That halves exec time and cuts planning time.
- The facets do not depend on paging, so they can be cached per (tenant, lifecycle) for 60 s. Paging and filter clicks would then skip about 34 ms (plan + exec).

**/counts/mortality (PASS at 66 ms, stg 1156/4575) and /counts/herd-analytics (PASS at 22 ms, stg 1095/1990)**
- No index would help: these are whole-herd rollups over 1.7k goats, all hash joins, sorts under 100 kB in memory.
- The 4.5 s p95 is not the SQL. It is connection acquire, or the first-use prepare on a cold connection (45 ms planning × 3 statements).
- If herd growth takes exec time past 100 ms, roll up the daily per-goat snapshot. At today's size, keep the SQL and fix pool and latency first.

**Index proposals (none needed for the in-scope reads today)**
- None of the in-scope reads needs a new index at this data size. Their seq scans are on tables of 2-3k rows that are read in full by design, so an index would not win.
- If goats grow past about 20k, add `CREATE INDEX CONCURRENTLY goats_tenant_live_or_dead_idx ON goats (tenant_id, park_id) INCLUDE (lifecycle_status, exit_reason, exited_at, updated_at, breed, management_stage, sex, species, age_band, shed_id, origin_type) WHERE merged_into_goat_id IS NULL;` for mortality, breakdown and herd-analytics.
