# STG latency program (started 2026-09-24)

Read this first before working on goatos-stg API or DB slowness, admin-web
"could not be loaded" / "read failed" errors, or login stalls. It records what
has already been found, so later sessions do not repeat the same analysis.

**PR:** vgoats/goatos#389 (`perf/stg-burst-and-login`). Every fix goes into this PR.

## Budget (maintainer rule, applies to the p95 of every API)

| p95 | Verdict |
|---|---|
| 50–100 ms | target |
| up to 200–300 ms | acceptable |
| over 500 ms | **never**; the guard blocks landing |

## Root causes found, most impact first

1. **The analytics rollup floods the disk.** The Cloud Run job
   `goatos-stg-analytics-rollup` rescans `analytics.app_events`
   (2.6 GB, 1.58M rows from 68 devices, never pruned). Each run then fails with
   a BigQuery 403 on `firebase_crashlytics.sg_mesha_goatos_ANDROID`, so
   `analytics-rollup-dispatch` refires it about every 40 min. Each run saturates
   the db-g1-small's roughly 600 IOPS for 2–3 min, and every request waiting on
   disk reaches its 15s timeout. This is what causes the 15–30s spikes in Grafana.
2. **Stg runs on a much smaller database machine than OCI.** Cloud SQL
   `db-g1-small` has a shared vCPU, 1.7 GB RAM, `shared_buffers` 128 MB and a
   20 GB PD-SSD. The OCI clone has 4 cores and 22 GB RAM. With 8 GB of data,
   OCI serves reads from RAM while stg goes to disk. Most of those 8 GB are
   junk: `app_events` 2.6 GB, delivered outbox rows about 1 GB,
   `audit_log` 0.8 GB, notifications 0.6 GB, and 241 unused indexes.
3. **Full scans of small tables, over and over.** `obligation_instances` has
   seen 14B rows read by seq scans (the table has 86k rows), `goats` 10.3B,
   `weighing_observations` 2.3B. About 280 foreign keys have no index.
   `random_page_cost` is 4, which is wrong for SSD. JIT is on.
4. **Wasteful queries:**
   - FCR compared every feed row with every price, about 11M comparisons.
   - The notification unread count scans each user's whole history, up to
     96k rows per call.
   - The work-board "any vaccination today" check reads all of `obligation_instances`.
   - Calendar targets take 7s because of a bad nested loop.
5. **Caches that never hit.** The vaccination read caches include the request
   time down to the nanosecond in their key. The weighing caches start cold on
   each instance, and concurrent misses are not shared.
6. **Live monitor fan-out.** Herd Signals re-reads all tags for every viewer on
   every gateway batch, about every 2s. Its LISTEN connection is held forever
   from the main pool.
7. **Fixes never deployed.** Stg ran `a67be34c0781` (Sep 23) while main was 116
   commits ahead, so every "still broken" report was testing old code.
8. **Guard blind spots.** The existing guards check code patterns or
   hand-listed queries and routes only. See `audit/guardrails-design.md`.

## Where to find each piece

| What | File |
|---|---|
| Work queue: waves, status, owner branch | `QUEUE.md` |
| DB-wide audit: table sizes, seq scans, indexes, bloat, settings | `audit/db-wide.md` |
| Per-endpoint audits: SQL, tables, EXPLAIN, grade, fix | `audit/part-*.md`, `audit/notifications-auth-admin.md` |
| Guardrail design: which guards, skills and docs to add, and why the old ones missed | `audit/guardrails-design.md` |
| BEFORE baseline: stg 24h per-path p50/p95/max/5xx, Cloud SQL metrics | `baseline/` |

The before/after report will be added here as `REPORT.md` after deploy. It is
measured with the same capture script as the baseline.

## Rules for anyone continuing this work (Claude or Codex)

- Check `QUEUE.md` and the git history of the touched code before starting, so
  you don't redo fixes that were already tried.
- Write a failing test first. Show before/after `EXPLAIN (ANALYZE, BUFFERS)`
  and endpoint timings. A judge agent reviews every push to #389.
- The OCI clone at `127.0.0.1:15432` is read-only for audits. Write only to a
  throwaway DB. Real stg is read-only, through `mesha_ceo_readonly`.
- Never add analytics, event or log data to the operational Postgres without a
  retention and archive job. Old events are archived to GCS cold storage
  (Parquet, Coldline, then Archive) before they are pruned.
- Landing is only through `make land-main`, then the exact main SHA is
  deployed to stg. See AGENTS.md "Main Merge Requires Exact-SHA CI Evidence".

## Pending maintainer decisions

- IAM: grant `roles/bigquery.dataViewer` on the `firebase_crashlytics`,
  `firebase_sessions` and `firebase_performance` datasets to
  `goatos-stg-analytics-rollup@goatos-stg.iam.gserviceaccount.com`.
- Cloud SQL flags (restarts the DB for about 1 min): `random_page_cost=1.1`,
  `jit=off`, and pg_stat_statements / Query Insights on.
- Should canceled obligations count in the vaccination command board totals?
  88% of `obligation_instances` rows are canceled.
- A tier upgrade (`db-custom-1-3840`, about +₹2k/month) only if the budget is
  still missed after the free fixes are deployed.
