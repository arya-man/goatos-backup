# STG latency program (started 2026-09-24)

Read this first before working on goatos-stg API or DB slowness, admin-web
"could not be loaded" / "read failed" errors, or login stalls. It records what
has already been found, so later sessions do not repeat the same analysis.

**PR:** vgoats/goatos#389 (`perf/stg-burst-and-login`). Every fix goes into this PR.

**Continuing in a new session? Read `HANDOFF.md` first.**

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


## Banned patterns (every agent, Claude or Codex, and every human)

These caused this incident. Reviewers and judges must BLOCK a change that adds one.

1. **Aggregating an unbounded, ever-growing history on a request path.** Example:
   the notification unread count ran `COUNT(DISTINCT ...)` over every notification a
   user ever got, on every page load. Keep a stored counter or read-state, updated
   in the same transaction as the write, and read it with one primary-key lookup.
   Every history table must have read/expiry semantics, such as mark-read,
   auto-resolve when the source item is decided, or retention.
2. **Operational Postgres used as an analytics or event store.** Event, log and
   telemetry tables need a retention job that archives to cold storage before
   pruning. Analytics are computed in BigQuery, not by rescanning Postgres.
3. **Joins that multiply rows before they filter.** FCR compared 16,922 × 649
   rows. Resolve 1:1 lookups first, then aggregate.
4. **Per-viewer recompute on every event.** Coalesce by key and time window, and
   broadcast one result to all viewers.
5. **Cache keys containing the request time.** Such a cache never hits. Key on
   business date, plus a time bucket where the data changes.
6. **Connections held forever from the shared pool.** Use a dedicated
   connection or a dedicated pool.
7. **Jobs that crash-loop on an outside dependency, or rescan everything each
   run.** Use a watermark, make outside failures non-fatal, and back off.
8. **Full copies of the stg DB to other environments.** Use a delta-only sync that
   skips junk tables (see "OCI sync" below).
9. **Unbounded lists returned by an API or fetched by admin-web/Android.**
   Examples: the notifications history, `ListLive` walking 50k tags for
   `limit=1`, the ±2y calendar window, the vaccination command reading all 86k
   obligations. Paginate server-side with a keyset cursor (no OFFSET on large
   tables), a server-enforced default (e.g. 50) and hard max (e.g. 200) page
   size, a stable sort with a unique tiebreaker, and totals from a stored
   counter or a separate cheap count. Clients load more on scroll or next page;
   Android keeps its one-page fetch cap and Room paging. Export or "show all"
   goes through an async export job.

The full catalog (these 9 plus the audit-found patterns, with the evidence each
needs) is `.agents/skills/scale-anti-patterns/SKILL.md` ("STG latency catalog").

## OCI sync (delta-only)

The OCI clone is refreshed from stg by a **delta-only** sync:
- Rows are copied per table since a watermark (`updated_at` or primary key).
- The data of `analytics.app_events`, `outbox_messages`, `audit_log` and
  delivered `notification_*` is excluded.
- Transfers are compressed.

Full `pg_dump`s over the internet are banned: they cost about ₹18/GB in Cloud SQL
egress, with 13–16 GB days on 4 and 21 Sep. The tool and its runbook are tracked
in `QUEUE.md` (Wave O).

## Maintainer decisions (2026-09-24)

1. Claude runs `make land-main` for #389 once every judge is clean, then deploys that exact landed SHA to stg with the guarded launcher. **Do not land or deploy until the analytics-rollup judge findings are fixed and re-judged.**
2. DB settings: `random_page_cost=1.1` and Query Insights **applied live on 2026-09-24**, now mirrored in `infra/envs/stg/cloud_sql.tf`. The Cloud SQL `jit` flag is unsupported; the API pool already runs with JIT off.
3. Canceled vaccination obligations **do not count** in the command-board totals (queued in Wave 2 vaccination).
4. Terraform for the analytics archive bucket/IAM: apply after the fixed rollup is green. Firebase to BigQuery export: optional.

## Still pending

- IAM: grant `roles/bigquery.dataViewer` on the `firebase_crashlytics`,
  `firebase_sessions` and `firebase_performance` datasets to
  `goatos-stg-analytics-rollup@goatos-stg.iam.gserviceaccount.com`.
- Cloud SQL flags (restarts the DB for about 1 min): `random_page_cost=1.1`,
  `jit=off`, and pg_stat_statements / Query Insights on.
- Should canceled obligations count in the vaccination command board totals?
  88% of `obligation_instances` rows are canceled.
- A tier upgrade (`db-custom-1-3840`, about +₹2k/month) only if the budget is
  still missed after the free fixes are deployed.
