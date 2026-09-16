# App-wide mobile network summaries

Optional source: Firebase Performance Monitoring BigQuery export, configured with
`GOATOS_PERFORMANCE_BQ_TABLE` / `--performance-bq-table` as one explicit
`project.dataset.table`. The project must match the configured BigQuery project.
`--source-app-id` identifies the Android app; these exports have no tenant identity.

Official schema: https://firebase.google.com/docs/perf-mon/bigquery-export

The stage selects `NETWORK_REQUEST` rows by client `event_timestamp` in the same
Asia/Kolkata business-day bounds as the other rollups. `event_name` is Firebase's
categorized URL pattern, not a backend route template. `app_display_version` is
Android versionName. HTTP methods and response classes have bounded categories.
`network_info.response_completed_time_us / 1000.0` is the elapsed time from the
client request start until response completion in milliseconds. This is full
client-observed request duration, not TCP RTT or isolated backend execution time.

Only samples with a present, nonnegative completed-response duration contribute.
`total_requests` counts those exported samples, not every attempted request or all
backend requests. No unsupported event-ID deduplication is invented: the source
schema defines each row as a performance event. Daily p50/p95/p99 use BigQuery
APPROX_QUANTILES within app version / URL pattern / method / response class.
Do not average daily percentiles to obtain a range percentile.

The optional stage shares the configured BigQuery location and maximum bytes
billed. A successful read replaces the app/day partition atomically under a
transaction advisory lock. Replays are idempotent; failed writes retain old rows.
An absent export configuration logs `performance_export_unavailable` and creates
no synthetic rows. Configured query/schema failures fail the rollup audit.

The source schema and local PostgreSQL replay/rollback are tested. A real
Performance export is not currently available, so live BigQuery query execution
and resulting Grafana data remain deployment verification requirements.
