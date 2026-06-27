-- +goose Up
-- Durable status for existing-cohort vaccination generation. This makes
-- protocol publish -> due-list generation observable, retryable, and audited
-- through product/ops surfaces instead of relying on worker logs.

CREATE TABLE vaccination_generation_runs (
  run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  protocol_version_id uuid NOT NULL,
  trigger_type text NOT NULL,
  trigger_ref text NULL,
  status text NOT NULL DEFAULT 'running',
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  generated_count int NOT NULL DEFAULT 0,
  deferred_count int NOT NULL DEFAULT 0,
  skipped_no_due_date_count int NOT NULL DEFAULT 0,
  suppressed_trusted_history_count int NOT NULL DEFAULT 0,
  cursor_goat_id uuid NULL,
  last_error text NULL,
  idempotency_key text NOT NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT vaccination_generation_runs_status_check CHECK (status IN ('queued', 'running', 'completed', 'failed')),
  CONSTRAINT vaccination_generation_runs_trigger_check CHECK (trigger_type IN ('publish', 'cli', 'goat_created', 'stage_changed', 'manual_campaign', 'retry')),
  CONSTRAINT vaccination_generation_runs_counts_check CHECK (
    generated_count >= 0
    AND deferred_count >= 0
    AND skipped_no_due_date_count >= 0
    AND suppressed_trusted_history_count >= 0
  ),
  CONSTRAINT vaccination_generation_runs_row_version_check CHECK (row_version >= 1),
  CONSTRAINT vaccination_generation_runs_version_fk FOREIGN KEY (tenant_id, protocol_version_id)
    REFERENCES protocol_versions(tenant_id, protocol_version_id),
  CONSTRAINT vaccination_generation_runs_cursor_goat_fk FOREIGN KEY (tenant_id, cursor_goat_id)
    REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT vaccination_generation_runs_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX vaccination_generation_runs_version_idx
  ON vaccination_generation_runs(tenant_id, protocol_version_id, started_at DESC);

CREATE INDEX vaccination_generation_runs_status_idx
  ON vaccination_generation_runs(tenant_id, status, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS vaccination_generation_runs_status_idx;
DROP INDEX IF EXISTS vaccination_generation_runs_version_idx;
DROP TABLE IF EXISTS vaccination_generation_runs;
