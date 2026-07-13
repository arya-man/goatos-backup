# Kernel Pipeline — Log-Based Metrics

**Status**: Production-ready as of 2026-07-13  
**Alternative**: [LESSONS_AND_GUARDS.md#finding-22](./LESSONS_AND_GUARDS.md) (path #1 — log-based metrics; sidecar collector on Jobs deferred)

## Overview

The kernel operational pipeline (event → transaction → outbox → obligation → sweeper → notify) is now instrumented via **Cloud Logging log-based metrics** built from the structured logs emitted by kernel worker Cloud Run Jobs. This resolves the blocker that prevented a collector sidecar from running alongside stateless Jobs.

All metrics are DELTA (event count) or distribution type, queried through the **Google Cloud Monitoring datasource** in Grafana. No Prometheus scraper is required; log entry filtering and value extraction happen automatically on ingest.

## Architecture

**Why log-based metrics instead of a sidecar?**

Cloud Run Jobs execute to completion and shut down immediately. A sidecar that pushes metrics lives only during execution and cannot push post-completion. Log-based metrics extract values from application logs at ingest time (by Cloud Logging's pipeline) and appear in Cloud Monitoring, bypassing the push/scrape question entirely.

**Data flow**: Backend worker logs structured JSON/text → Cloud Logging captures and filters → Cloud Monitoring extracts numeric field values (value-extraction) → Grafana queries via Monitoring API.

## Log Shapes and Filters

### Outbox Relay

**Log line** (structured JSON):
```json
{
  "msg": "outbox relay run complete",
  "batches_processed": 1,
  "claimed": 0,
  "published": 10,
  "retry_scheduled": 0,
  "failed": 0,
  "reclaimed_stale": 0,
  "dead_letter": 0,
  ...
}
```

**Log filter**:
```
resource.type=cloud_run_job
AND resource.labels.project_id=PROJECT
AND resource.labels.job_name=PROJECT-outbox-relay
AND jsonPayload.msg="outbox relay run complete"
```

### Notification Dispatcher

**Log line** (structured JSON):
```json
{
  "msg": "notification dispatch complete",
  "tenant_id": "...",
  "claimed": 5,
  "reclaimed_stale": 0,
  "sent": 3,
  "failed": 1,
  "exhausted": 0,
  ...
}
```

**Log filter**:
```
resource.type=cloud_run_job
AND resource.labels.project_id=PROJECT
AND resource.labels.job_name=PROJECT-notification-dispatcher
AND jsonPayload.msg="notification dispatch complete"
```

### Obligation Sweeper

**Log line** (plain text via `fmt.Printf`):
```
swept version=26029a08-4bec-56ba-a28b-e2330ee9077b batches=0 obligations=0 park_batches=0 park_obligations=0
```

**Log filter**:
```
resource.type=cloud_run_job
AND resource.labels.project_id=PROJECT
AND resource.labels.job_name=PROJECT-obligation-sweeper
AND textPayload=~"^swept version="
```

**Limitation**: Cloud Logging's `REGEXP_EXTRACT_ALL()` on `textPayload` currently has parsing constraints. For now, we count sweeper runs only (INT64 counter), not individual obligation/batch counts. A future enhancement: migrate obligation-sweeper to emit structured JSON (same pattern as outbox-relay and notification-dispatcher) to enable value extraction.

## Metrics Reference

### Outbox Relay (7 metrics)

| Metric Name | Type | Source | Description |
|---|---|---|---|
| `goatos_kernel_outbox_published` | DISTRIBUTION (DELTA) | `jsonPayload.published` | Events successfully published to Pub/Sub |
| `goatos_kernel_outbox_failed` | DISTRIBUTION (DELTA) | `jsonPayload.failed` | Events that failed (will retry on next run) |
| `goatos_kernel_outbox_dead_letter` | DISTRIBUTION (DELTA) | `jsonPayload.dead_letter` | Events exhausted retries (moved to DLQ) |
| `goatos_kernel_outbox_claimed` | DISTRIBUTION (DELTA) | `jsonPayload.claimed` | Events claimed for processing in this batch |
| `goatos_kernel_outbox_reclaimed` | DISTRIBUTION (DELTA) | `jsonPayload.reclaimed_stale` | Stale leases reclaimed (retry on timeout) |
| `goatos_kernel_outbox_retry_scheduled` | DISTRIBUTION (DELTA) | `jsonPayload.retry_scheduled` | Events scheduled for future retry |
| `goatos_kernel_outbox_batches` | DISTRIBUTION (DELTA) | `jsonPayload.batches_processed` | Number of batches processed in run |

### Notification Dispatcher (5 metrics)

| Metric Name | Type | Source | Description |
|---|---|---|---|
| `goatos_kernel_notify_sent` | DISTRIBUTION (DELTA) | `jsonPayload.sent` | Notifications sent successfully |
| `goatos_kernel_notify_failed` | DISTRIBUTION (DELTA) | `jsonPayload.failed` | Notifications failed (will retry) |
| `goatos_kernel_notify_exhausted` | DISTRIBUTION (DELTA) | `jsonPayload.exhausted` | Notifications exhausted all retries |
| `goatos_kernel_notify_claimed` | DISTRIBUTION (DELTA) | `jsonPayload.claimed` | Notifications claimed for processing |
| `goatos_kernel_notify_reclaimed` | DISTRIBUTION (DELTA) | `jsonPayload.reclaimed_stale` | Stale leases reclaimed |

### Obligation Sweeper (2 metrics live today)

| Metric Name | Type | Source | Description |
|---|---|---|---|
| `goatos_kernel_sweeper_runs` | INT64 COUNTER (DELTA) | Log line count | Number of sweeper runs (result log line presence) |
| `goatos_kernel_sweeper_obligations_v2` | INT64 COUNTER (DELTA) | Log line count | **KNOWN DUPLICATE** — same filter/description as `goatos_kernel_sweeper_runs` (`textPayload=~"^swept version="`, no value extractor). Despite the name, it does NOT extract an obligations-per-run count yet; it is currently indistinguishable from `sweeper_runs`. Verify with the maintainer whether this was an accidental re-run of the creation script under a new name (candidate for deletion) or an intentional placeholder ahead of the real value-extraction work below — do not build a dashboard panel that assumes it already reports a real obligations count.

**Total live metric count: 14** (12 DISTRIBUTION + 2 INT64 — not "13 DISTRIBUTION + 1 INT64" as
stated elsewhere; see `LESSONS_AND_GUARDS.md#finding-22`, which undercounts the INT64 group by one
because of the `_v2` duplicate above).

**Future**: When sweeper emits JSON like the other workers, we will add (or convert the `_v2`
metric above into) real value-extracted metrics:
- `goatos_kernel_sweeper_obligations` (count per run)
- `goatos_kernel_sweeper_batches` (count per run)
- `goatos_kernel_sweeper_park_obligations` (park-scoped)
- `goatos_kernel_sweeper_park_batches` (park-scoped)

## How to Create Metrics in a New Environment

### One-time setup

```bash
# From the repository root:
bash infra/observability/kernel-log-metrics.sh goatos-prod asia-south1
```

The script is **idempotent**: it checks metric existence before creating, so re-running is safe.

### What the script does

1. Checks if each metric already exists (via `gcloud logging metrics describe`)
2. Generates JSON config files with proper log filters and value extractors
3. Calls `gcloud logging metrics create` with the config
4. Reports which metrics were created or already existed

### Manual metric creation (for debugging)

If you need to inspect or manually create a single metric:

```bash
# List existing metrics
gcloud logging metrics list --project=PROJECT

# Describe a metric
gcloud logging metrics describe goatos_kernel_outbox_published --project=PROJECT

# Delete (for testing)
gcloud logging metrics delete goatos_kernel_outbox_published --project=PROJECT

# Create from YAML config
cat > /tmp/metric.json << 'EOF'
{
  "name": "projects/PROJECT/metrics/user/goatos_kernel_outbox_published",
  "description": "Outbox relay: events published to Pub/Sub",
  "filter": "resource.type=cloud_run_job AND resource.labels.project_id=PROJECT AND ...",
  "metricDescriptor": {
    "metricKind": "DELTA",
    "valueType": "DISTRIBUTION",
    "unit": "1",
    "displayName": "goatos_kernel_outbox_published"
  },
  "valueExtractor": "EXTRACT(jsonPayload.published)",
  "bucketOptions": {
    "exponentialBuckets": {
      "numFiniteBuckets": 64,
      "growthFactor": 2,
      "scale": 1
    }
  }
}
EOF

gcloud logging metrics create goatos_kernel_outbox_published \
  --config-from-file=/tmp/metric.json \
  --project=PROJECT
```

## Querying in Grafana

### Datasource

All kernel metrics are queried through the **Google Cloud Monitoring** datasource (type: Stackdriver).

### Query pattern (timeSeriesList)

```
Resource type: cloud_run_job
Metric type: logging.googleapis.com/user/goatos_kernel_outbox_published (etc.)
Aligner: ALIGN_DELTA (for rates) or ALIGN_MAX (for gauges)
Reducer: REDUCE_SUM (across multiple job executions)
Period: 60s to 300s
```

### Example: Outbox published rate

```
metric.type = logging.googleapis.com/user/goatos_kernel_outbox_published
resource.type = cloud_run_job
Aligner: ALIGN_DELTA
Reducer: REDUCE_SUM
Period: 60s
```

### Example: Notification failure ratio (MQL)

```mql
{
  sent: fetch cloud_run_job :: 'logging.googleapis.com/user/goatos_kernel_notify_sent'
    | align delta(1m)
    | every 1m
    | group_by [], [value: sum(val())]
  failed: fetch cloud_run_job :: 'logging.googleapis.com/user/goatos_kernel_notify_failed'
    | align delta(1m)
    | every 1m
    | group_by [], [value: sum(val())]
}
| join
| value 100 * val(1) / (val(0) + val(1))
```

## Freshness and Coverage

- **Latency**: A result log appears in Cloud Logging ~200ms after being written. Metric value extraction happens synchronously on ingest, so a metric data point is queryable ~1-2 seconds after the log line is written.
- **Coverage**: Every job execution that emits a result log contributes one data point per metric. Scheduled jobs run every 1-5 minutes (per scheduler config), so expect data every 1-5 minutes under normal conditions.
- **Staleness**: If a job fails or crashes before emitting the result log, that run's metrics will not have a data point. The Grafana dashboard will show a gap.

## Troubleshooting

### Metric exists but shows no data in Grafana

1. **Verify the log is being generated**:
   ```bash
   gcloud logging read 'resource.type=cloud_run_job AND resource.labels.job_name=goatos-stg-outbox-relay AND jsonPayload.msg="outbox relay run complete"' \
     --project=goatos-stg --limit=3 --format=json | jq '.[].jsonPayload'
   ```

2. **Check metric config** (filter and extractor):
   ```bash
   gcloud logging metrics describe goatos_kernel_outbox_published --project=goatos-stg
   ```
   Verify that the log line matches the metric's filter.

3. **Verify time range**: Grafana's dashboard uses `now-6h` by default. If metrics are new, change to `now-1h` or `now-30m`.

4. **Check Grafana datasource connection**:
   - Go to Grafana → Configuration → Data Sources → Google Cloud Monitoring
   - Verify the project ID and authentication

### Script fails: "Bucket options are required for DISTRIBUTION"

Make sure the Python script is generating proper JSON config with `bucketOptions` for DISTRIBUTION metrics. Re-run the script.

### Script fails: "Failed to parse YAML"

The shell is incorrectly escaping quotes in filter strings. Use the provided `kernel-log-metrics.sh` script, which handles escaping via Python's `json` module.

## Future Enhancements

1. **Obligation sweeper value extraction**: Migrate sweeper to emit `jsonPayload` like outbox-relay and notification-dispatcher, enabling extraction of obligations/batches per run.

2. **Consumer lag from domain events**: Wire `domain-event-consumer` to emit a structured result log with consumer lag / processed events, replacing the Prometheus gauge if possible.

3. **Derived metrics**: Create Cloud Monitoring alert policies that compute failure ratios (e.g., `rate(failures) / rate(claimed)`) and page SREs when thresholds are crossed.

4. **Custom dashboards**: In addition to the main kernel-pipeline dashboard, create operational runbooks linked directly to specific metric spike patterns (e.g., "outbox dead-letter spike → [start here]").

## References

- **Backend worker code**:
  - `backend/cmd/outbox-relay/main.go` (line 101): result log with all fields
  - `backend/cmd/notification-dispatcher/main.go` (line 93): result log
  - `backend/cmd/obligation-sweeper/main.go` (line 159): plain-text result log

- **Production setup**:
  - Run `bash infra/observability/kernel-log-metrics.sh goatos-prod asia-south1` before first prod scheduler job execution

- **Grafana dashboard**:
  - `infra/grafana/dashboards/03-kernel-pipeline.json` (uses these metrics in panels 6, 14, and future panels for sweeper)

- **Alert policies**:
  - `infra/envs/stg/monitoring.tf` and `infra/envs/prod/monitoring.tf` reference these metric types and define SLO thresholds
