# Analytics Agent Context

Read first:

- `../context/analytics/final-analytics-infra.md`
- `../context/architecture/final-architecture.md`

Purpose:

- dbt transforms, Cube semantic layer, Tinybird pipes, Metabase setup, and
  analytics guardrails.

Do:

- Keep official metrics in Cube.
- Keep dbt as transforms/tests only.
- Guard BigQuery scans and telemetry retention.

Do not:

- Do not let AI, dashboards, or BI query raw Postgres.
- Do not define the same KPI in multiple places.
