# First-party analytics summaries

Default `--source=app_events` reads only the configured tenant's
`analytics.app_events`. GA4 configuration is unnecessary. Existing explicit
`--source=ga4` is a separate legacy mode; do not mix sources for the same dates.
Crash data is not synthesized from application analytics.

## Definitions

- Business dates use Asia/Kolkata. Event time is `client_event_time`, falling
  back to `received_at` only when absent. Late delivery belongs to its event day.
- Deduplicate by tenant and nonempty `client_event_id` (column, then legacy
  property); otherwise preserve the unique server event ID.
- A work session is `(actor_id, device_id, journey_id)`. Android's journey lasts
  from login to logout, potentially across restarts/days. Missing actor, device,
  or journey excludes a row from sessions and conversion matching. Device column
  falls back to the event property. No anonymous records are combined.
- DAU counts distinct authenticated actors observed on that date. `wau_approx`
  holds **exact** trailing-seven-business-day distinct actors for this source,
  not the old GA4 sum-of-daily-counts approximation. Session counts are daily
  observed work sessions. `avg_session_ms` is first-to-last observed event span
  within the day, **not foreground engaged time**; background sync can extend it.
- Funnels count ordered nondecreasing event-time prefixes per cohort. `users`
  and `sessions` are distinct actors/work sessions reaching that prefix;
  `conversions` counts cohorts reaching it. These measures have different units.
  For conversion percentage divide a step's conversions by step zero conversions.
- Proof/camera cohorts additionally require `proof_id` / `capture_request_id`.
  The camera identity includes a random launcher namespace and its local request
  token, so photos, videos, and launcher/process recreation cannot collide.
  Legacy camera events with only `request_token` are excluded: their counter
  cannot safely identify a capture. Other legacy flows remain eligible. Deploy
  the Android producer before expecting new camera measurements; rerun old days
  to remove previously ambiguous camera summaries, not to reconstruct lost IDs.
  Business cohorts additionally require `group_key`. A cohort is counted once
  per starting business day; repeated events/retries are not new conversions.
- Completions can occur into the next day, at most 24 hours after cohort start (feature-open for business flows, independent of earlier session events).
  Only starts on the requested day are attributed there. Unfinished cohorts are
  drop-offs only after their 24-hour window expires; younger ones are pending.
  An empty completion population has no measured latency; schema stores zero,
  so dashboards must filter on `completions > 0` for latency panels.

| Funnel/journey key | Ordered funnel | Journey duration |
|---|---|---|
| `proof_delivery` | capture completed → upload started → upload completed | capture → upload completed |
| `camera_capture` | camera requested → successfully finalized | requested → finalized (not app startup) |
| `login_to_bootstrap` | login success → bootstrap loaded | login success → bootstrap loaded |
| `feed_distribution` | observed session → opened → enqueued → acknowledged | opened → acknowledged |
| `feed_packing` | observed session → complete opened → enqueued → acknowledged | opened → acknowledged |
| `feed_wastage` | observed session → complete opened → enqueued → acknowledged | opened → acknowledged |
| `feed_transport` | observed session → opened → enqueued → acknowledged | opened → acknowledged |

Business acknowledgement joins the submitted event's exact `outbox_item_id`
to `sync_write_succeeded` in the same actor/device/journey, after submission.
The explicit terminal `action=sync_success` observer is also accepted. A plain
`*_submitted` means local enqueue, not server/business completion. These are
telemetry-confirmed sync outcomes, not independently audited domain records.
Proof delivery does not require `proof_processing_completed`: interrupted-processing
recovery and original-file fallback can successfully upload without that event.
Processing diagnostics remain in the raw events; they do not gate delivery success.

`proof_upload_registered` deliberately does not mark upload completion: source
code registers a proof before byte transfer finishes.

## Execution and backfill

`analytics-rollup --tenant-id <uuid> --source-date YYYY-MM-DD` refreshes one day.
Without a date it ends on yesterday. `--lookback-days=1..7` refreshes a bounded
range ending there; the scheduled 03:15 IST run uses 3 days to repair next-day
completions and recent offline arrivals. Older arrivals require an explicit
backfill (also refresh the following 6 dates for corrected WAU). A backfill invokes each desired
date separately; rerun recent days after delayed offline events arrive or a
24-hour completion window settles. Refresh today explicitly for provisional data.

Each day uses one repeatable-read transaction and a per-tenant advisory lock,
then atomically replaces all three daily summary partitions. It never mutates
raw events or crash summaries. A failed insert rolls back earlier replacements.
Definitions omitted by a newer version cannot leave stale rows for that date.
Raw reads cover two days for journeys and seven days for exact WAU. Migration
000320 builds a narrow tenant/event-time index concurrently. Intermediate stages
use an indexed temporary table; only aggregated summaries leave PostgreSQL.

## I/O safety on the shared Cloud SQL instance (migration 000400)

- Incremental: `analytics.rollup_day_watermark` records, per tenant/day, the
  ingest instant the committed summaries reflect. A lookback day is recomputed
  only when rows arrived after it (probe over `app_events_received_at_idx`), or
  once when it crosses its final instant (D+2 00:00 IST, when drop-offs settle).
  `-force-recompute` backfills regardless.
- Raw reads use a bitmap scan of the tenant/event-time index (insert-ordered
  heap), carry only cohort keys from `properties`, and run with a
  transaction-local `work_mem` (`-work-mem`, default 32MB). Recomputed days are
  separated by `-chunk-pause` (default 5s).
- Optional Firebase/BigQuery exports are isolated: a failure is logged
  (`optional_export_failed`), counted (`kernel.analytics_rollup.export_failures`)
  and audited as `rollup_run.status='degraded'`, and the job exits 0. The kernel
  dispatcher treats `degraded` as done, retries real failures with 40m/80m/160m
  backoff, at most 4 dispatches per source date, never while a run is active.

## Retention and cold archive

`app_events` keeps `GOATOS_ANALYTICS_APP_EVENTS_RETENTION_DAYS` (15) days hot.
Older UTC `received_at` days are exported to
`gs://$GOATOS_ANALYTICS_ARCHIVE_BUCKET/app_events/dt=YYYY-MM-DD/part-<sha16>.jsonl.gz`
(one `to_jsonb` row per line), verified (row count equals the Postgres day,
stored size + MD5 equal the written bytes), recorded in
`analytics.app_events_archive`, and only then deleted in 5,000-row batches with
2s pauses, at most 250,000 rows and 2 days per run. No bucket = no deletion.
Query archived days without restoring (always filter `dt`):

```sql
SELECT event_name, COUNT(*) FROM `goatos-stg.goatos_stg_analytics_rollup.app_events_archive`
WHERE dt BETWEEN '2026-09-01' AND '2026-09-07' GROUP BY 1 ORDER BY 2 DESC;
```

To restore a day into Postgres, download the object and load each line with
`jsonb_populate_record(NULL::analytics.app_events, line::jsonb)`.

## Proof

`GOATOS_RUN_POSTGRES_TESTS=1 go test -count=1 -v ./cmd/analytics-rollup`
uses the normal isolated Postgres harness (external throwaway server supported).
Tests cover tenant/device/session boundaries, missing identity, legacy duplicate
IDs, ordering, wrong outbox items, midnight, late acknowledgements, idempotence,
rollback after final-write failure, exact WAU, and a 46,000-event volume fixture.
