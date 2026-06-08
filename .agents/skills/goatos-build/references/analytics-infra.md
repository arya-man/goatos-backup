# Analytics And Infra Reference

Load this when working on analytics, BI, AI analyst, telemetry, BigQuery,
Tinybird, Cube, dbt, Metabase, GCP infra, or cost controls.

Canonical docs:

- `context/analytics/final-analytics-infra.md`
- `context/source-findings/customer-promise-safety-findings.md`
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
- Legacy BigQuery/dashboard table catalog is now captured in the analytics doc
  as parity-test and metric-inventory seed material. It is not operational truth.
- Promise-risk dashboards must include delivery-date eligibility, unresolved
  identity, feed-clearance, promised-weight risk, price-audit failure,
  replacement availability, and open-promise sweeper output.
- Official KPIs still go through Cube; do not revive direct BigQuery queries in
  apps just because a legacy table name exists.
