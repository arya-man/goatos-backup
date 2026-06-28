-- +goose Up
-- Index the per-batch reserve-movement idempotency guard (CountBatchReserveMovements) so the
-- multi-lot ReserveForBatch check is an index lookup, not a sequential scan of the ever-growing
-- inventory_stock_movements ledger at million-goat scale. Partial on batch_id IS NOT NULL because
-- only batch-attached movements (reservations/consumption) are ever looked up by batch.
CREATE INDEX inventory_stock_movements_batch_idx
  ON inventory_stock_movements (tenant_id, batch_id)
  WHERE batch_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS inventory_stock_movements_batch_idx;
