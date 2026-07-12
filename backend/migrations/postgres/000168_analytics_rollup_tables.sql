-- +goose Up
-- GA4 -> BigQuery -> Postgres analytics rollup read models
-- (docs/observability/OBSERVABILITY_DESIGN.md section 2.6,
-- infra/envs/stg/analytics_rollup.tf).
--
-- Cost lever: the analytics-rollup Cloud Run Job aggregates GA4/Crashlytics
-- BigQuery export data ONCE per scheduled run (single partition scan,
-- GROUP BY in BigQuery, MaxBytesBilled-capped) and upserts compact daily
-- rollups here. Grafana's Postgres datasource then reads these tables on
-- every dashboard load/refresh WITHOUT re-scanning BigQuery, which is the
-- entire point of this schema (BQ query cost is paid once per day per
-- rollup, not once per dashboard view).
--
-- All rollup tables are:
--   - tenant-scoped (tenant_id first column, matching every other Goat OS
--     read model's multi-tenant discipline),
--   - indexed on (tenant_id, event_date) for the common "this tenant, this
--     day/range" dashboard query shape,
--   - written ONLY by the analytics-rollup Cloud Run Job
--     (backend/cmd/analytics-rollup) via idempotent
--     INSERT ... ON CONFLICT ... DO UPDATE keyed on the natural grain, so a
--     re-run for the same source_date overwrites rather than double-counts,
--   - additive: creating a new `analytics` schema plus five new tables only,
--     no change to any existing schema/table.
--
-- Grafana itself only ever SELECTs from these tables via the read-only
-- `goatos_grafana_ro` Postgres role (see docs/observability/INFRA.md section
-- 4) - that role grant is provisioned out-of-band, not by this migration.
CREATE SCHEMA IF NOT EXISTS analytics;

COMMENT ON SCHEMA analytics IS
  'Read models rolled up from GA4/Crashlytics BigQuery exports by the analytics-rollup Cloud Run Job. Written only by that job; read by Grafana (goatos_grafana_ro, read-only) and any ad-hoc reporting. Never written from request-path handlers.';

-- Funnel step conversion, one row per (tenant, day, funnel, step). Grain
-- matches the login -> bootstrap -> drive-open -> scan ->
-- vaccination-capture -> submit funnel in OBSERVABILITY_DESIGN.md section
-- 2.5/2.6; funnel_key lets future funnels (e.g. a web admin funnel) share
-- the same table without a schema change.
CREATE TABLE analytics.funnel_daily (
  tenant_id     uuid NOT NULL,
  event_date    date NOT NULL,
  funnel_key    text NOT NULL,
  step_key      text NOT NULL,
  step_index    integer NOT NULL,
  users         bigint NOT NULL DEFAULT 0,
  sessions      bigint NOT NULL DEFAULT 0,
  conversions   bigint NOT NULL DEFAULT 0,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_date, funnel_key, step_key),
  CONSTRAINT funnel_daily_step_index_check CHECK (step_index >= 0),
  CONSTRAINT funnel_daily_counts_check CHECK (users >= 0 AND sessions >= 0 AND conversions >= 0)
);

COMMENT ON TABLE analytics.funnel_daily IS
  'Daily funnel step conversion rolled up from GA4 BigQuery export. One row per (tenant, day, funnel, step); step_index orders steps for the funnel chart/table.';

CREATE INDEX funnel_daily_tenant_event_date_idx
  ON analytics.funnel_daily (tenant_id, event_date);

-- Journey/latency rollup, one row per (tenant, day, journey) - e.g. a named
-- multi-step user journey (screen-to-screen, request-to-completion) whose
-- p50/p90/p99 duration and completion/drop-off counts matter for the
-- Mobile/Kernel dashboards.
CREATE TABLE analytics.journey_daily (
  tenant_id     uuid NOT NULL,
  event_date    date NOT NULL,
  journey_key   text NOT NULL,
  p50_ms        bigint NOT NULL DEFAULT 0,
  p90_ms        bigint NOT NULL DEFAULT 0,
  p99_ms        bigint NOT NULL DEFAULT 0,
  completions   bigint NOT NULL DEFAULT 0,
  drop_offs     bigint NOT NULL DEFAULT 0,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_date, journey_key),
  CONSTRAINT journey_daily_latency_check CHECK (p50_ms >= 0 AND p90_ms >= 0 AND p99_ms >= 0),
  CONSTRAINT journey_daily_counts_check CHECK (completions >= 0 AND drop_offs >= 0)
);

COMMENT ON TABLE analytics.journey_daily IS
  'Daily journey duration percentiles (p50/p90/p99) plus completion/drop-off counts rolled up from GA4 BigQuery export. One row per (tenant, day, journey_key).';

CREATE INDEX journey_daily_tenant_event_date_idx
  ON analytics.journey_daily (tenant_id, event_date);

-- Overall app engagement, one row per (tenant, day). DAU/session counts for
-- the top-line Mobile dashboard tile.
CREATE TABLE analytics.engagement_daily (
  tenant_id        uuid NOT NULL,
  event_date       date NOT NULL,
  dau              bigint NOT NULL DEFAULT 0,
  wau_approx       bigint NOT NULL DEFAULT 0,
  sessions         bigint NOT NULL DEFAULT 0,
  avg_session_ms   bigint NOT NULL DEFAULT 0,
  updated_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_date),
  CONSTRAINT engagement_daily_counts_check CHECK (dau >= 0 AND wau_approx >= 0 AND sessions >= 0 AND avg_session_ms >= 0)
);

COMMENT ON TABLE analytics.engagement_daily IS
  'Daily active users, an approximate trailing-7-day active user count (wau_approx), session count, and average session duration, rolled up from GA4 BigQuery export. One row per (tenant, day).';

CREATE INDEX engagement_daily_tenant_event_date_idx
  ON analytics.engagement_daily (tenant_id, event_date);

-- Crash-free rate + volumes, one row per (tenant, day, app_version). Sourced
-- from the Crashlytics BigQuery export when linked; app_version is part of
-- the grain because crash-free rate is meaningfully different per release.
CREATE TABLE analytics.crash_daily (
  tenant_id                  uuid NOT NULL,
  event_date                 date NOT NULL,
  app_version                text NOT NULL DEFAULT '',
  crash_free_users_pct       numeric(6, 3) NOT NULL DEFAULT 100,
  crash_free_sessions_pct    numeric(6, 3) NOT NULL DEFAULT 100,
  fatal_count                bigint NOT NULL DEFAULT 0,
  nonfatal_count             bigint NOT NULL DEFAULT 0,
  top_issues                 jsonb NOT NULL DEFAULT '[]'::jsonb,
  updated_at                 timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_date, app_version),
  CONSTRAINT crash_daily_pct_check CHECK (
    crash_free_users_pct BETWEEN 0 AND 100 AND crash_free_sessions_pct BETWEEN 0 AND 100
  ),
  CONSTRAINT crash_daily_counts_check CHECK (fatal_count >= 0 AND nonfatal_count >= 0)
);

COMMENT ON TABLE analytics.crash_daily IS
  'Daily crash-free rate and crash volume per app_version, rolled up from the Crashlytics BigQuery export. top_issues is a small JSON array of {issue_id, title, count} for the top crash signatures that day (bounded list, not a full crash dump).';

CREATE INDEX crash_daily_tenant_event_date_idx
  ON analytics.crash_daily (tenant_id, event_date);

-- Audit trail of every analytics-rollup run, independent of tenant scoping
-- (a run processes one source_date, potentially across tenants/tables in
-- one job invocation). This is the idempotency + observability record: the
-- job checks/writes one row here per (source_date) run so re-runs are
-- traceable, and so a Grafana panel or on-call check can see "did today's
-- rollup run, how long did it take, how many bytes did it bill, did it
-- fail."
CREATE TABLE analytics.rollup_run (
  run_id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  source_date      date NOT NULL,
  started_at       timestamptz NOT NULL DEFAULT now(),
  finished_at      timestamptz,
  rows_written     bigint NOT NULL DEFAULT 0,
  bytes_billed     bigint NOT NULL DEFAULT 0,
  status           text NOT NULL DEFAULT 'running',
  error            text,
  CONSTRAINT rollup_run_status_check CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
  CONSTRAINT rollup_run_rows_written_check CHECK (rows_written >= 0),
  CONSTRAINT rollup_run_bytes_billed_check CHECK (bytes_billed >= 0)
);

COMMENT ON TABLE analytics.rollup_run IS
  'Audit trail of each analytics-rollup job invocation: one row per run, recording source_date, timing, rows written, BigQuery bytes billed, and outcome (running/succeeded/failed/skipped). status=skipped covers the case where GOATOS_GA4_EXPORT_DATASET is unset (GA4 not yet linked in the Firebase console) - the job exits 0 without querying BigQuery.';

CREATE INDEX rollup_run_source_date_idx
  ON analytics.rollup_run (source_date, started_at DESC);

-- +goose Down
DROP TABLE IF EXISTS analytics.rollup_run;
DROP TABLE IF EXISTS analytics.crash_daily;
DROP TABLE IF EXISTS analytics.engagement_daily;
DROP TABLE IF EXISTS analytics.journey_daily;
DROP TABLE IF EXISTS analytics.funnel_daily;
DROP SCHEMA IF EXISTS analytics;
