-- +goose Up
-- +goose NO TRANSACTION
-- Add index to support ListPlannedBatchesNeedingFinalization keyset pagination.
-- The query keysets by (created_at, batch_id) to find batches needing task/stock finalization.
-- This partial index covers the filtered predicate path (tenant_id, protocol_version_id, status='planned')
-- and the sort/pagination columns to avoid sequential table scans at scale.
-- CONCURRENTLY requires NO TRANSACTION (goose wraps migrations in a txn by default).
CREATE INDEX CONCURRENTLY IF NOT EXISTS planned_batch_finalization_keyset_idx
  ON obligation_batches (tenant_id, protocol_version_id, created_at, batch_id)
  WHERE status = 'planned';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS planned_batch_finalization_keyset_idx;
