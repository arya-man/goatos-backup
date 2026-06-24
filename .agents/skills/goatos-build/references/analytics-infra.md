# Analytics And Infra Reference

Load this when working on analytics, BI, AI analyst, telemetry, BigQuery,
Tinybird, Cube, dbt, Metabase, GCP infra, or cost controls.

Canonical docs:

- `context/analytics/final-analytics-infra.md`
- `docs/decisions/high-scale-dashboard-projections.md`
- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/frontend/current-admin-web-scope.md`

## Current Rule

Postgres-backed Goat OS APIs and projections are operational truth for product
surfaces. Admin-web and mobile must never query BigQuery, Sheets, GCS,
Firestore, or operational databases directly.

The current admin-web build is not an analytics dashboard rebuild. It is the
vaccination process-integrity slice:

```text
Admin Config
PHC Vaccination
Vaccination execution context scoped by park/shed
Control Tower process-gap summary
```

Control Tower may summarize only gaps, adherence risk, exceptions, owner, and
next action. It must not become a generic KPI or census dashboard.

## Future Analytics Shape

- Outbox -> Pub/Sub is the streaming backbone.
- BigQuery is warehouse/history/training-set infrastructure, not a product UI
  dependency.
- Tinybird can serve hot telemetry/live API use cases.
- Cube is the semantic door for official KPIs.
- dbt transforms curated marts.
- Cost controls are mandatory: partition filters, max bytes billed, quotas,
  marts, caches, and scan monitoring.

Any dashboard/report that slices large data by month, date, breed, farm, shed,
load, category, status, gender, operator, source, or similar dimensions must
follow `docs/decisions/high-scale-dashboard-projections.md`.

## Removed From Active Runtime Direction

Do not revive old dashboard parity or import-review runtime paths:

```text
direct frontend BigQuery queries
old BigQuery dashboard parity as product scope
backend/internal/legacy_sync
backend/internal/legacy_import
counts/mortality/reporting runtime modules
Import Review/Data Quality/Legacy Sync product screens
```

Historical source docs may mention those names. Treat them as migration
archaeology unless the product scope is explicitly reopened.
