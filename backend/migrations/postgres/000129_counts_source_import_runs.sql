-- +goose Up
-- Feed Direction G2 / CSG10: durable observability evidence for reviewed typed
-- Counts/Shifting source imports. The import command writes canonical
-- count_base_anchors and shifting_events, while this table records the batch
-- outcome that Feed readiness can cite without trusting stdout.

CREATE TABLE count_source_import_runs (
  count_source_import_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_system text NOT NULL,
  mode text NOT NULL DEFAULT 'execute',
  status text NOT NULL DEFAULT 'running',
  source_ref text NULL,
  source_rows_read integer NOT NULL DEFAULT 0,
  base_anchor_rows integer NOT NULL DEFAULT 0,
  shifting_event_rows integer NOT NULL DEFAULT 0,
  replay_count integer NOT NULL DEFAULT 0,
  failed_row_count integer NOT NULL DEFAULT 0,
  trace_id text NULL,
  last_error text NULL,
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT count_source_import_runs_mode_check CHECK (mode IN ('dry_run', 'execute')),
  CONSTRAINT count_source_import_runs_status_check CHECK (status IN ('running', 'completed', 'failed')),
  CONSTRAINT count_source_import_runs_source_system_check CHECK (source_system IN ('physical_base_count', 'manual_review', 'import', 'feed_shiftings_docx', 'legacy_slack', 'goatos_canonical')),
  CONSTRAINT count_source_import_runs_counts_check CHECK (
    source_rows_read >= 0
    AND base_anchor_rows >= 0
    AND shifting_event_rows >= 0
    AND replay_count >= 0
    AND failed_row_count >= 0
  ),
  CONSTRAINT count_source_import_runs_status_completion_check CHECK (
    (status = 'running' AND completed_at IS NULL)
    OR (status IN ('completed', 'failed') AND completed_at IS NOT NULL)
  )
);

CREATE INDEX count_source_import_runs_tenant_started_idx
  ON count_source_import_runs (tenant_id, started_at DESC, count_source_import_run_id DESC);

CREATE INDEX count_source_import_runs_status_idx
  ON count_source_import_runs (tenant_id, status, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS count_source_import_runs;
