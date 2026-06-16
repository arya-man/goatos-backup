-- +goose Up
DROP INDEX IF EXISTS legacy_sync_run_conflicts_unique_open_source_key;

CREATE UNIQUE INDEX IF NOT EXISTS legacy_sync_run_conflicts_unique_run_source_key
  ON legacy_sync_run_conflicts (tenant_id, sync_run_id, source_conflict_key);

-- +goose Down
DROP INDEX IF EXISTS legacy_sync_run_conflicts_unique_run_source_key;

CREATE UNIQUE INDEX IF NOT EXISTS legacy_sync_run_conflicts_unique_open_source_key
  ON legacy_sync_run_conflicts (tenant_id, source_conflict_key);
