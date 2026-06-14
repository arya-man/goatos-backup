# Analytics And Infra Reference

Load this when working on analytics, BI, AI analyst, telemetry, BigQuery,
Tinybird, Cube, dbt, Metabase, GCP infra, or cost controls.

Canonical docs:

- `context/analytics/final-analytics-infra.md`
- `context/source-findings/customer-promise-safety-findings.md`
- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`

Rules:

- Postgres is operational truth for Goat OS apps and dashboards.
- Outbox -> Pub/Sub is the streaming backbone.
- BigQuery is warehouse/history/training-set layer.
- Tinybird is hot telemetry/live API layer.
- dbt transforms only.
- Cube is the single semantic door for official KPIs.
- Dashboards must not query raw BigQuery facts directly.
- Guard BigQuery cost with partition filters, max bytes billed, quotas, marts,
  BI Engine/caches, and scan monitoring.
- Media storage/egress is a first-class cost line.
- Legacy BigQuery/dashboard data is a temporary upstream for current-data
  reconciliation/backfill until Goat OS Android/backend workflows become the
  primary write path. Treat it as read-only source input to backend sync jobs,
  not as a dashboard runtime dependency. Apps still read Postgres-backed Goat OS
  APIs/Cube-facing metrics, never raw BQ. The current committed bridge is
  `backend/cmd/bq-reconcile`, which consumes read-only BQ event and
  latest-location exports and writes audited lifecycle/current-location updates
  plus BQ-backed attribute fills/review conflicts into Postgres before counters
  are rebuilt. It fills blank attributes from deterministic BQ evidence and
  normalizes same-meaning breed labels, but nonblank passport sex/breed
  disagreements with BQ become review conflicts rather than silent overwrites.
  When an import run is supplied, it may also fill BQ-backed sole-reason
  `blank_gender` staging rows so normal RFID apply can create the passports.
- AI analyst work is blocked until canonical dbt marts, Cube metrics, dashboard
  parity tests, an analytics skill, offline evals, and provenance/freshness
  footers exist. AI uses Cube first, curated marts second, and raw SQL only for
  debugging or migration investigation.
- The future analytics skill needs per-domain reference docs with canonical
  metrics, dimensions, key tables, grain, join keys, required filters, gotchas,
  and common query patterns, plus a separate analyst workflow guide for
  clarify -> source selection -> query -> adversarial review -> provenance.
- Analytics evals must record skill version, git SHA, model ID, pass/fail,
  token count, latency, and timestamp. Stakeholder corrections from Slack or
  WhatsApp become candidate eval/reference-doc updates after human review.
- Phase 10 metric inventory should distill `goatos/apps/admin-web/app/api`,
  live `dashboard/app/api`, `vgoats-dashboard/app/api`,
  `slack-automation-scripts/Dashboard Charts - BigQuery Mapping.docx`, and
  `source-material/goatOS.docx`. These assets are source material only.
- Promise-risk dashboards must include delivery-date eligibility, unresolved
  identity, feed-clearance, promised-weight risk, price-audit failure,
  replacement availability, and open-promise sweeper output.
- Official KPIs still go through Cube; do not revive direct BigQuery queries in
  apps just because a legacy table name exists.
