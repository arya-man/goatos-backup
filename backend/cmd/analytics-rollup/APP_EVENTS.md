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
- Proof/camera cohorts additionally require `proof_id` / `request_token`.
  Business cohorts additionally require `group_key`. A cohort is counted once
  per starting business day; repeated events/retries are not new conversions.
- Completions can occur into the next day, at most 24 hours after cohort start (feature-open for business flows, independent of earlier session events).
  Only starts on the requested day are attributed there. Unfinished cohorts are
  drop-offs only after their 24-hour window expires; younger ones are pending.
  An empty completion population has no measured latency; schema stores zero,
  so dashboards must filter on `completions > 0` for latency panels.

| Funnel/journey key | Ordered funnel | Journey duration |
|---|---|---|
| `proof_delivery` | capture completed → processing completed → upload started → upload completed | capture → upload completed |
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
000318 builds a narrow tenant/event-time index concurrently. Intermediate stages
use an indexed temporary table; only aggregated summaries leave PostgreSQL.

## Proof

`GOATOS_RUN_POSTGRES_TESTS=1 go test -count=1 -v ./cmd/analytics-rollup`
uses the normal isolated Postgres harness (external throwaway server supported).
Tests cover tenant/device/session boundaries, missing identity, legacy duplicate
IDs, ordering, wrong outbox items, midnight, late acknowledgements, idempotence,
rollback after final-write failure, exact WAU, and a 46,000-event volume fixture.
