-- +goose Up
-- +goose NO TRANSACTION
-- Keep the process-integrity read model serving a stable projection version while
-- the projector builds the next version. This removes the old whole-tenant live
-- delete/reinsert behavior from the serving path.
ALTER TABLE process_integrity_projection_state
  ADD COLUMN IF NOT EXISTS serving_projection_version bigint;

UPDATE process_integrity_projection_state
SET serving_projection_version = projection_version
WHERE serving_projection_version IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_version_row_uidx
  ON process_integrity_projection_rows (tenant_id, projection_version, row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_serving_hot_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, sort_priority, due_at, row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_serving_work_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, work_state, sort_priority, due_at, row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_serving_scope_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, park_id, shed_id, due_at, row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_serving_severity_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, severity, sort_priority, due_at, row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_serving_protocol_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, protocol_version_id, due_at, row_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_serving_due_idx
  ON process_integrity_projection_rows (tenant_id, projection_version, category, due_at, row_id);

DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_row_uidx;

COMMENT ON COLUMN process_integrity_projection_state.serving_projection_version IS
  'Projection version currently served by Control Tower/Action Center reads. Recompute builds a new projection_version, then atomically flips this pointer.';

-- +goose Down
-- +goose NO TRANSACTION
DELETE FROM process_integrity_projection_rows rows
USING process_integrity_projection_state state
WHERE rows.tenant_id = state.tenant_id
  AND rows.projection_version <> COALESCE(state.serving_projection_version, state.projection_version);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS process_integrity_projection_rows_row_uidx
  ON process_integrity_projection_rows (tenant_id, row_id);

DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_serving_due_idx;
DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_serving_protocol_idx;
DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_serving_severity_idx;
DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_serving_scope_idx;
DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_serving_work_idx;
DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_serving_hot_idx;
DROP INDEX CONCURRENTLY IF EXISTS process_integrity_projection_rows_version_row_uidx;

ALTER TABLE process_integrity_projection_state
  DROP COLUMN IF EXISTS serving_projection_version;
