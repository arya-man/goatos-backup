-- +goose Up
-- Feed Direction G2 / CSG6+CSG10: durable run evidence for the bounded
-- Counts/Shifting mismatch scan. This lets readiness prove the stale
-- imported/historical scan path from Postgres state instead of worker logs.

CREATE TABLE count_mismatch_scan_runs (
  count_mismatch_scan_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  park_id uuid NULL REFERENCES locations(location_id),
  shed_id uuid NULL REFERENCES locations(location_id),
  counted_after timestamptz NULL,
  counted_before timestamptz NOT NULL,
  cursor_counted_at timestamptz NULL,
  cursor_anchor_id uuid NULL,
  status text NOT NULL DEFAULT 'running',
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  scanned_anchor_count integer NOT NULL DEFAULT 0,
  exception_write_count integer NOT NULL DEFAULT 0,
  investigating_anchor_count integer NOT NULL DEFAULT 0,
  next_cursor_counted_at timestamptz NULL,
  next_cursor_anchor_id uuid NULL,
  last_error text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT count_mismatch_scan_runs_tenant_park_fk
    FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_mismatch_scan_runs_tenant_shed_fk
    FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT count_mismatch_scan_runs_status_check CHECK (status IN ('running', 'completed', 'failed')),
  CONSTRAINT count_mismatch_scan_runs_counts_check CHECK (
    scanned_anchor_count >= 0
    AND exception_write_count >= 0
    AND investigating_anchor_count >= 0
  ),
  CONSTRAINT count_mismatch_scan_runs_cursor_pair_check CHECK (
    (cursor_counted_at IS NULL AND cursor_anchor_id IS NULL)
    OR (cursor_counted_at IS NOT NULL AND cursor_anchor_id IS NOT NULL)
  ),
  CONSTRAINT count_mismatch_scan_runs_next_cursor_pair_check CHECK (
    (next_cursor_counted_at IS NULL AND next_cursor_anchor_id IS NULL)
    OR (next_cursor_counted_at IS NOT NULL AND next_cursor_anchor_id IS NOT NULL)
  ),
  CONSTRAINT count_mismatch_scan_runs_window_check CHECK (
    counted_after IS NULL OR counted_after < counted_before
  )
);

CREATE INDEX count_mismatch_scan_runs_tenant_started_idx
  ON count_mismatch_scan_runs (tenant_id, started_at DESC, count_mismatch_scan_run_id DESC);

CREATE INDEX count_mismatch_scan_runs_status_idx
  ON count_mismatch_scan_runs (tenant_id, status, updated_at DESC);

CREATE INDEX count_mismatch_scan_runs_scope_idx
  ON count_mismatch_scan_runs (tenant_id, park_id, shed_id, counted_before DESC);

-- +goose Down
DROP INDEX IF EXISTS count_mismatch_scan_runs_scope_idx;
DROP INDEX IF EXISTS count_mismatch_scan_runs_status_idx;
DROP INDEX IF EXISTS count_mismatch_scan_runs_tenant_started_idx;
DROP TABLE IF EXISTS count_mismatch_scan_runs;
