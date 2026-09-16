# App-wide Firebase crash summaries

Requires Crashlytics BigQuery export with **Include sessions**. Configure both
`GOATOS_CRASHLYTICS_BQ_TABLE` and `GOATOS_CRASHLYTICS_SESSIONS_TABLE`, plus
`GOATOS_ANALYTICS_SOURCE_APP_ID=sg.mesha.goatos` for the production Android app.
The operator must verify both tables belong to that app. Table IDs are explicit,
validated identifiers; BigQuery region must match the actual export datasets.

Schema reference: https://firebase.google.com/docs/crashlytics/bigquery-dataset-schema
(verified September 16, 2026). Sessions use `session_id`, `instance_id`,
`event_type`, and `crashlytics_data_collection_enabled`; crashes use
`firebase_session_id` and `error_type`. Only collection-enabled SESSION_START
records contribute denominators. Installation IDs are installations, not people.
Repeated events cannot multiply either numerator or denominator.

The summary is **app-wide across all tenants**. It never writes legacy
`analytics.crash_daily` or uses GA4/app-events activity as a denominator.
Ratios describe sessions started in the business day and fatal events observed
within that same day. A fatal event after midnight for a previous-day session is
outside this day-window metric; these are not complete lifetime cohort rates or
an exact reproduction of the Firebase console. Both scans have timestamp bounds
and the shared BigQuery MaxBytesBilled cap. Recent-day replacement repairs late
export arrival. Zero denominators are unavailable, never synthetic 100%.

Both exports absent: explicit `crash_export_unavailable` log and no summary rows.
Only one table configured: fail clearly. Query/schema failures abort the crash
stage. Daily replacement is transactional and does not alter journey tables.

Current staging export datasets are absent; SQL/schema unit checks are not live
export E2E proof. Enabling the capability requires real schema readback, a bounded
query with billed-byte receipt, matched session/fatal count verification, and
Grafana datasource readback. Keep the dashboard unavailable until that evidence
exists. Do not describe deployment of this adapter as completed Firebase export
setup.
