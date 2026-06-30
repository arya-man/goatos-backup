-- +goose Up
-- Feed Direction G2 / CSG10: durable observability evidence for bounded
-- Counts/Shifting projection recompute worker runs. Feed readiness must be
-- able to cite projection status, row counts, exception counts, timing, and
-- errors without trusting stdout.

CREATE TABLE count_projection_recompute_runs (
  count_projection_recompute_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  park_id uuid NOT NULL REFERENCES locations(location_id),
  horizon text NOT NULL,
  target_date date NOT NULL,
  as_of timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'running',
  projection_status text NULL,
  snapshot_id uuid NULL REFERENCES count_projection_snapshots(count_projection_snapshot_id) ON DELETE SET NULL,
  row_count integer NOT NULL DEFAULT 0,
  exception_count integer NOT NULL DEFAULT 0,
  source_contract_version text NOT NULL,
  generated_by text NOT NULL,
  trace_id text NULL,
  last_error text NULL,
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT count_projection_recompute_runs_horizon_check CHECK (horizon IN ('count_as_of', 'feed_target_date')),
  CONSTRAINT count_projection_recompute_runs_status_check CHECK (status IN ('running', 'completed', 'failed')),
  CONSTRAINT count_projection_recompute_runs_projection_status_check CHECK (
    projection_status IS NULL OR projection_status IN ('ready', 'blocked', 'stale', 'failed')
  ),
  CONSTRAINT count_projection_recompute_runs_counts_check CHECK (row_count >= 0 AND exception_count >= 0),
  CONSTRAINT count_projection_recompute_runs_contract_check CHECK (btrim(source_contract_version) <> ''),
  CONSTRAINT count_projection_recompute_runs_generated_by_check CHECK (btrim(generated_by) <> ''),
  CONSTRAINT count_projection_recompute_runs_completion_check CHECK (
    (status = 'running' AND completed_at IS NULL)
    OR (status IN ('completed', 'failed') AND completed_at IS NOT NULL)
  )
);

CREATE INDEX count_projection_recompute_runs_tenant_started_idx
  ON count_projection_recompute_runs (tenant_id, started_at DESC, count_projection_recompute_run_id DESC);

CREATE INDEX count_projection_recompute_runs_scope_idx
  ON count_projection_recompute_runs (tenant_id, park_id, horizon, target_date DESC, status, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS count_projection_recompute_runs;
