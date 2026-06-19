-- +goose Up
-- counts_sync_runs is a Counts-owned table (written by internal/counts). It lives
-- in the Counts migration block (000030-000039) so the Counts slice never depends
-- on a later Mortality/shared migration. Integration order is Locations -> Counts
-- -> Mortality; this keeps the table available before any Mortality migration.
CREATE TABLE counts_sync_runs (
  sync_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  requested_by uuid NOT NULL,
  mode text NOT NULL,
  status text NOT NULL,
  snapshot_date date NULL,
  source_rows_read integer NOT NULL DEFAULT 0,
  projection_rows_written integer NOT NULL DEFAULT 0,
  rows_skipped integer NOT NULL DEFAULT 0,
  unresolved_location_labels integer NOT NULL DEFAULT 0,
  freshness_status text NOT NULL DEFAULT 'unknown',
  serving_state text NOT NULL DEFAULT 'never_synced',
  source_watermark text NULL,
  unavailable_sources jsonb NOT NULL DEFAULT '[]'::jsonb,
  trace_id text NULL,
  last_error text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  CONSTRAINT counts_sync_runs_mode_check CHECK (mode IN ('dry_run', 'execute')),
  CONSTRAINT counts_sync_runs_status_check CHECK (status IN ('running', 'completed', 'failed', 'source_unavailable')),
  CONSTRAINT counts_sync_runs_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT counts_sync_runs_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed', 'source_unavailable')),
  CONSTRAINT counts_sync_runs_counts_check CHECK (
    source_rows_read >= 0
    AND projection_rows_written >= 0
    AND rows_skipped >= 0
    AND unresolved_location_labels >= 0
  ),
  CONSTRAINT counts_sync_runs_unavailable_array_check CHECK (jsonb_typeof(unavailable_sources) = 'array')
);

CREATE INDEX counts_sync_runs_tenant_created_idx
  ON counts_sync_runs (tenant_id, created_at DESC, sync_run_id DESC);

-- +goose Down
DROP TABLE IF EXISTS counts_sync_runs;
