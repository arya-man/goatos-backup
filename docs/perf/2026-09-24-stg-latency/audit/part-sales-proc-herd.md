# DB perf audit: sales/*, procurement/*, app/procurement/*, herd-signals/*

Method: `EXPLAIN (ANALYZE, BUFFERS)` inside `BEGIN READ ONLY … ROLLBACK` on the OCI clone, tenant `00000000-0000-4000-8000-000000000001` (the only tenant with data). SQL was taken verbatim from the Go consts or builders, with realistic params from real rows. The clone's herd-signal data stops at 2026-09-04 16:34 UTC, so every `now()` in herd-signals SQL was replaced by `'2026-09-04 16:35+00'`. Otherwise the 24h and 15m windows would be empty and the costs would look too low.
"DB ms" = planning + execution summed over every statement in the request. psql does not cache plans. pgx's statement cache can drop planning after 5 executions, but only when Postgres picks a generic plan, so treat planning as an upper bound.

**Data volumes (why almost everything passes):** every sales and procurement table has fewer than 700 rows. goats has 1.7k, goat_identifiers 3.4k and weighing_observations 2.8k. Herd signals has 404k activity windows, about 1.5M packets across daily partitions, and **only 20 tags** in `herd_signal_tag_latest`. That last number matters most: every herd-signals cost scales linearly with tag count.

## Summary table

| Endpoint | SQL stmts / req | N+1? | DB ms (plan+exec) | Grade | Notes |
|---|---|---|---|---|---|
| GET /sales/overview | 12 (8 goroutines in parallel; attachDealLines serial after closedDeals) | no | sum ≈52, critical path ≈33 | PASS | farmValuation 10.8 plan + 20.0 exec dominates. See p95 gap below. |
| GET /sales/options | 1-2 | no | ≈3 | PASS | |
| GET /sales/valuation-assumptions | 1 | no | ≈2 | PASS | PK lookup, 1 row |
| PUT /sales/valuation-assumptions | idempotency + upsert | no | reads ≈3 | PASS | write not executed |
| GET /sales/deals | 4 (page, payments =ANY, lines =ANY, count) | no (batched) | ≈8 | PASS | 75 rows |
| POST /sales/deals, …/payments, PUT/DELETE payment, …/status | idem-key lookup + FOR UPDATE by PK + write + outbox + getDeal re-read (3 stmts) | no | reads ≈8 | PASS | writes described, not run |
| GET /sales/buyer-leads | 2 (page + count) | no | ≈5 | PASS | seq on 432 rows, fine |
| POST buyer-leads / fpo-leads (+status) | lookup + write | no | ≈3 | PASS | |
| GET /sales/fpo-leads | 2 | no | ≈4 | PASS | |
| POST market-benchmarks / sold-tags / weight-checks | insert (sold-tags: allocation lookups) | no | ≈3-5 | PASS | |
| GET /procurement/buyer-analytics | 1 | no | 11.2 (5.5+5.7) | PASS | seq procurement_vendors 677 |
| GET /procurement/farm-born-sales | 2 (animals + options) | no | ≈72 (animals 14.4+25.5, options 9.7+22.4) | PASS | seq goat_identifiers 3.2k, goats; half of it is planning |
| GET /procurement/loadwise-sales, /loadwise-weights | 5 (alertDays, loadwiseSales, costLines =ANY, overallAvg, unsold price) | no | ≈31 (loadwiseSales 15.6+4.6) | PASS | seq goats 1741 |
| PUT /procurement/loads/{id}/cost | lock + delete/insert cost lines | no | reads ≈3 | PASS | |
| GET /procurement/feed-purchases | 3 (page, payments =ANY, totals) | no | ≈8.5 | PASS | |
| GET /procurement/feed-purchase-form, -options | 1-3 | no | ≈5 | PASS | |
| POST/PUT feed-purchases (+payments, delivery, status) | lock by PK + write + outbox | no | reads ≈3 | PASS | |
| GET /procurement/vendors | 2 | no | ≈7 (search via trgm ≈5) | PASS | |
| GET /procurement/vendors/{id} | 1 | no | ≈3 | PASS | |
| GET vendor-catalog / vendor-options / vendor-form | 1 each | no | ≈3-5 | PASS | |
| POST/PUT vendors, /status | row_version check + write | no | ≈3 | PASS | |
| GET /procurement/source-entry/loads | 1 | no | 4.7 | PASS | 9 loads |
| GET /procurement/source-entry/loads/{id} | 9 serial (load + 8 child lists) | no (fixed fan-out) | ≈23 | PASS | could be 1-2 stmts |
| POST source-entry/* (goats, HF evidence, health, decision, dispatch, arrival, accept) | tx: lock load + per-goat writes | AcceptIntake loops per goat | reads ≈3-10 | PASS today | AcceptIntake per-goat inserts scale with load size (≈30-50 goats) |
| GET /app/procurement/animal-purchases/options | 2 (breeds + catalog) | no | ≈7 | PASS | seq goats 1569 |
| GET …/animal-purchases/loads, /loads/{id} | 1 | no | ≈2 | PASS | |
| GET …/loads/{id}/animals | 2 (page + media =ANY) | no | ≈7 | PASS | |
| POST …/loads, …/animals | idem + lock + insert + media loop (1 insert per media ref) | small loop | ≈5 | PASS | |
| GET /procurement/animal-purchases/review | 3 (counts, page, media) | no | ≈7 | PASS | |
| POST …/animals/{id}/decision | guard + update + actor name | no | ≈4 | PASS | |
| **GET /herd-signals/live** | **≈15 serial** | **yes: duplicate enrichment** | **≈220** | **OK** (20 tags). Projected FAIL at ~200+ tags | see notes |
| **GET /herd-signals/live/stream** | snapshot = full ListLive per notify per subscriber; heartbeat is free | yes | **≈220 per tick per subscriber** | **OK** (projected FAIL) | |
| GET /herd-signals/tags/{id}/timeline | 2 (tag latest + windows) | no | 1d@60s ≈8; 7d@60s ≈33; 7d@300s ≈11 | PASS | PK index range scan |
| GET /herd-signals/tags/{id}/activity | scope + ≤5 per-goat event lists + windows | no | ≈50 | PASS | |
| GET /herd-signals/gateways | 5 | no | ≈12 | PASS | |
| GET /herd-signals/insights | 8 serial | no | ≈64 | PASS | weight card 4.4+18.5 is the hot one |
| GET /herd-signals/export.csv | page walk + enrichment per page | per page | ≈150-200 at 20 tags | OK | streamed |
| POST /herd-signals/packets | tx, FOR UPDATE per tag, window upserts | per-tag loop | write path, not executed | n/a | per tag: latest upsert + delta sums over windows |
| POST tag-mappings(/replace,/unmap), heartbeats | lock + write | no | ≈3-10 | PASS | |

No sort or hash spilled to disk on any statement: every sort was an in-memory quicksort of 272kB or less.

## Per-endpoint notes

### sales/overview (stg p50 162 / p95 1619 ms)
Path: `sales_handler.GetOverview` → `SalesService.GetOverview` → `Repository.GetOverview` (`internal/sales/adapters/postgres/overview_repository.go`). An errgroup runs 8 goroutines, each on its own pool connection:
- closedDeals 1.9+0.25, then attachDealLines about 2 ms (serial, same goroutine)
- buyerPipeline: 2 statements, 2.2 + 2.5
- fpoPipeline: 2 statements, about 1.9 + 2
- tagRoster: 2 statements, 1.5 + 1.3
- weightAudit 1.1
- marketBenchmarks 1.5
- measuredSoldWeights 3.3
- **farmValuation 10.8 plan + 20.0 exec**. Seq scans weighing_observations (2.5k rows, sorted 272kB), goat_identifiers 3.2k and goats 1.6k. These are whole-tenant reads, so the seq scans are legitimate at this size.

The DB critical path is about 33 ms, which is PASS. **The DB does not explain the gap to p95 1619.** Probable causes, in order:
1. **Fan-out pool pressure.** One request takes 8 connections at the same moment. The pool config is `MaxConns` 10 / `MinConns` 3 (per `platform/postgres` defaults). With 2 concurrent overview requests plus any other traffic, `pool.Acquire` waits. On a fresh Cloud Run instance, 5 or more new connections are also dialed, and each costs a TLS or Cloud SQL connector handshake of about 100-300 ms.
2. Cold instance: first-use planning with no statement cache, plus JIT/catalog warmup.
3. About 12 network round trips, 8 of them concurrent.

Fixes:
- (a) Fold the 7 small reads (everything except farmValuation) into **one statement** with CTEs or `json_build_object` subselects, or run them serially on a single `pool.Acquire`'d connection. That takes the connection demand from 8 to 2 and the round trips from 12 to 2.
- (b) Cache farmValuation per tenant+farm for 60 s. It is a whole-herd valuation card and does not need second-level freshness.
- (c) Raise `MinConns` to about 6, or cap overview fan-out with a semaphore of 2.
- (d) Watch pgxpool `acquire_duration` / `empty_acquire_count` (already exported in `platform/postgres/metrics.go`) to confirm.

### sales/deals, leads, writes
All batched with `= ANY`, so there is no N+1. Tables have 75-432 rows, so seq scans are cheaper than index scans. Nothing to change.

### procurement/farm-born-sales (≈72 ms, PASS)
About 24 ms of the total is planning for two large CTE statements that share `farmBornPopulationSQL`. Optional improvements:
- Compute the population once and derive the options from the same rows in Go, which removes one statement (about 32 ms).
- The `ident` CTE seq-scans goat_identifiers (3.2k rows). Once the tenant grows, this partial index serves `DISTINCT ON (goat_id)`. `pg_indexes` has no index with this key order and predicate:
  ```sql
  CREATE INDEX CONCURRENTLY goat_identifiers_active_tag_by_goat_idx
    ON public.goat_identifiers (tenant_id, goat_id, identifier_type, created_at DESC)
    INCLUDE (identifier_value)
    WHERE status = 'active' AND identifier_type IN ('animal_identifier_1','animal_identifier_2');
  ```

### procurement/source-entry/loads/{id} (≈23 ms)
The fan-out is fixed (9 serial queries), so it is not N+1. It could be sent as one `pgx.Batch` to save 8 round trips, which is about 10-20 ms of network on Cloud Run.

### herd-signals/live (OK today, will FAIL with more tags)
Path: `handler.ListLive` → `Service.ListLive` (`internal/herdsignals/app/service.go:181`) → repo. Statements per request when there is no `risk_state` filter:

1. `ListTagsLatestPage`: page query, 9.5 plan + 3.2 exec. The tagLocationJoin LATERAL probes the goat_identifiers unique index, then hash-joins goats (seq scan, 1.7k rows).
2. `computeSummary` 9.5 + 1.5
3. `listAllTagsLatest` (cohort walk in pages of 5000): another page query plus **another computeSummary on every page**. The cohort discards that summary, so it is wasted work: about 24 ms.
4. `enrichTagsBatch(cohort)`: 5 statements
   - ResolveTagsBatch 4.2
   - GetGoatsByIDs ≈3
   - GetShedLocations ≈3
   - GetBaselineDeltas 4.2 + 0.7
   - **GetBatteryHistory 44.2 plan + 9.5 exec**. It runs two LATERAL LIMIT 1 probes per tag across about 10 daily packet partitions (Merge Append, 20 loops).
5. `enrichTagsBatch(page)`: **the same 5 statements again for tags that are already in the cohort.**
6. `GetMotionDeltas24h`: 3.0 + 14.3. It index-range-scans the pkey and reads 5.3k windows for 20 tags.

Total is about 220 ms. Scaling: battery (2 × tags × partitions probes), motion24h (288 windows × tags) and the duplicate enrichment are all linear in tag count. A rough extrapolation to about 200 tags gives more than 1 s.

Fixes, in priority order:
1. **Remove the duplicate enrichment.** Enrich the cohort once, then select the page items from `cohortItems` by tag_id and apply `riskGroupStats`. That saves about 75 ms (5 statements).
2. **Skip `computeSummary` inside `listAllTagsLatest`.** Call `ListTagsLatestPage` directly, since `ListTagsLatest` wraps the summary. Saves about 11 ms per cohort page.
3. **Stop reading battery history from raw packets.** Move it to the latest-state table: keep a `battery_mv_7d_first`/`battery_first_seen_at` pair updated at ingest in `updateTagLatest`, or read it from a daily rollup. As a stop-gap, rewrite both LATERALs as one pass:
   ```sql
   SELECT tag_id,
          (array_agg(battery_mv ORDER BY received_at ASC))[1],  min(received_at),
          (array_agg(battery_mv ORDER BY received_at DESC))[1], max(received_at)
   FROM public.herd_signal_packets
   WHERE tenant_id=$1 AND tag_id = ANY($2) AND battery_mv IS NOT NULL
     AND received_date >= (now()::date - $3) AND received_at >= now() - make_interval(days=>$3)
   GROUP BY tag_id
   ```
   The 44 ms planning comes from partition count and is paid on every call. The rollup removes it.
4. motion24h and baseline both read the same 24h × 300s windows. Merge them into one statement with `sum(motion_delta)` and `percentile_disc(0.75)`. Or keep a 24h running sum on `herd_signal_tag_latest` at ingest, since the ingest path already computes window deltas in `sumActivityWindowDeltaTx`.
5. A covering index would make the motion24h and baseline scans index-only. The current pkey has `bucket_start` before `bucket_seconds`, so it filters the 60s rows:
   ```sql
   CREATE INDEX CONCURRENTLY herd_signal_activity_windows_tag_grain_start_idx
     ON public.herd_signal_activity_windows (tenant_id, tag_id, bucket_seconds, bucket_start DESC)
     INCLUDE (motion_delta, packet_count, gap_delta);
   ```
   The existing `(tenant_id, bucket_seconds, bucket_start) WHERE gateway_id IS NOT NULL` index is keyed by tenant rather than tag, so it does not cover this.

### herd-signals/live/stream
The per-tick cost equals one full `ListLive` (about 220 ms), run **independently for each subscriber** on every LISTEN notify. The hub channel has a buffer of 1, so bursts coalesce per subscriber but not across subscribers. The 25 s heartbeat does no DB work.

Fix: build the snapshot once per (tenant, filter-key) per notify and fan it out, or debounce notifies to 1-2 s per tenant. With N open dashboards the DB cost is currently N × 220 ms per ingest batch.

### herd-signals/insights (≈64 ms)
8 serial statements. The weight card (4.4 + 18.5) is a nested loop: 1,111 weighings in 24h, each probing tag_latest with an OR on `upper(btrim())`. It scales with weighings × tags. Rewrite it to drive from tag_latest and probe the existing expression index `weighing_observations_demo_tag_window_idx (tenant_id, lower(btrim(scanned_identifier)), accepted_at DESC)`:
```sql
SELECT count(DISTINCT w.sid) FROM public.herd_signal_tag_latest tl
CROSS JOIN LATERAL (SELECT lower(btrim(wo.scanned_identifier)) sid FROM public.weighing_observations wo
  WHERE wo.tenant_id = tl.tenant_id
    AND lower(btrim(wo.scanned_identifier)) IN (lower(btrim(tl.tag_id)), lower(btrim(coalesce(tl.tag_mac,''))))
    AND wo.accepted_at >= greatest(now() - interval '24 hours', tl.animal_monitoring_since)
    AND btrim(wo.scanned_identifier) <> '' AND wo.verification_status <> 'rejected') w
WHERE tl.tenant_id = $1 AND tl.animal_monitoring_since IS NOT NULL
```
This adds a `verification_status <> 'rejected'` predicate so the partial index matches. Confirm that semantics change is acceptable. It could also run as one `pgx.Batch`.

### herd-signals/timeline, activity, gateways, export
- **timeline:** PK range scan. 1,405 rows over 1 day at 60 s take 3.6 ms; 10k rows over 7 days at 60 s take 29 ms.
- **activity:** per-goat indexed lists of about 7 ms each.
- **gateways:** tiny.
- **export.csv:** reuses the page walk plus enrichment, so fixes 1 and 3 under live apply here too.

### Out of scope / not verified
- `procurement` ListWorkRows / GetWorkRow (`procurement_work*SQL`) are reached through app/service.go, not through the routes in scope.
- The POST/PUT/DELETE write statements were read in code but not executed.
