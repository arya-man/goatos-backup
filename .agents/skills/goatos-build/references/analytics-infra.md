# Analytics And Infra Reference

Load this when working on analytics, BI, AI analyst, telemetry, BigQuery,
Tinybird, Cube, dbt, Metabase, GCP infra, or cost controls.

Canonical docs:

- `context/analytics/final-analytics-infra.md`
- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`

Rules:

- Postgres is operational truth.
- Outbox -> Pub/Sub is the streaming backbone.
- BigQuery is warehouse/history/training-set layer.
- Tinybird is hot telemetry/live API layer.
- dbt transforms only.
- Cube is the single semantic door for official KPIs.
- Dashboards must not query raw BigQuery facts directly.
- Guard BigQuery cost with partition filters, max bytes billed, quotas, marts,
  BI Engine/caches, and scan monitoring.
- Media storage/egress is a first-class cost line.

