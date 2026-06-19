-- +goose Up
-- Mortality-owned sync-run ledger. counts_sync_runs was split out into the
-- Counts block (000032_counts_sync_runs.sql) so each module owns its own table
-- and the Counts slice does not depend on a Mortality-range migration.
CREATE TABLE mortality_sync_runs (
  sync_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  requested_by uuid NOT NULL,
  mode text NOT NULL,
  status text NOT NULL,
  source_rows_read integer NOT NULL DEFAULT 0,
  events_upserted integer NOT NULL DEFAULT 0,
  projection_rows_written integer NOT NULL DEFAULT 0,
  rows_skipped integer NOT NULL DEFAULT 0,
  dedup_candidate_events integer NOT NULL DEFAULT 0,
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
  CONSTRAINT mortality_sync_runs_mode_check CHECK (mode IN ('dry_run', 'execute')),
  CONSTRAINT mortality_sync_runs_status_check CHECK (status IN ('running', 'completed', 'failed', 'source_unavailable')),
  CONSTRAINT mortality_sync_runs_freshness_check CHECK (freshness_status IN ('green', 'yellow', 'red', 'unknown')),
  CONSTRAINT mortality_sync_runs_serving_check CHECK (serving_state IN ('never_synced', 'fresh', 'stale', 'rebuilding', 'failed', 'source_unavailable')),
  CONSTRAINT mortality_sync_runs_counts_check CHECK (
    source_rows_read >= 0
    AND events_upserted >= 0
    AND projection_rows_written >= 0
    AND rows_skipped >= 0
    AND dedup_candidate_events >= 0
    AND unresolved_location_labels >= 0
  ),
  CONSTRAINT mortality_sync_runs_unavailable_array_check CHECK (jsonb_typeof(unavailable_sources) = 'array')
);

CREATE INDEX mortality_sync_runs_tenant_created_idx
  ON mortality_sync_runs (tenant_id, created_at DESC, sync_run_id DESC);

-- +goose Down
DROP TABLE IF EXISTS mortality_sync_runs;
