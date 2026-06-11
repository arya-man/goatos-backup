-- +goose Up
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS legacy_import_rows_run_keyset_idx
  ON legacy_import_rows (tenant_id, import_run_id, row_number, legacy_row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS legacy_import_rows_run_state_keyset_idx
  ON legacy_import_rows (tenant_id, import_run_id, processing_state, row_number, legacy_row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS legacy_import_rows_processing_reasons_gin_idx
  ON legacy_import_rows
  USING GIN ((normalized_payload -> 'processing_reasons'));

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS legacy_import_rows_processing_reasons_gin_idx;
DROP INDEX CONCURRENTLY IF EXISTS legacy_import_rows_run_state_keyset_idx;
DROP INDEX CONCURRENTLY IF EXISTS legacy_import_rows_run_keyset_idx;
