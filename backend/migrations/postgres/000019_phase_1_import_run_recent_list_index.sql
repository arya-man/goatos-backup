-- +goose Up
CREATE INDEX IF NOT EXISTS legacy_import_runs_tenant_started_idx
  ON legacy_import_runs (tenant_id, started_at DESC, import_run_id DESC);

-- +goose Down
DROP INDEX IF EXISTS legacy_import_runs_tenant_started_idx;
