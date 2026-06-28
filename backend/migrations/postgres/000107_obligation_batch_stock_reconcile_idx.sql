-- +goose Up
-- Index batches whose planned membership changed after stock reservation so the
-- stock reconciler can release held doses without scanning all batches.

CREATE INDEX IF NOT EXISTS obligation_batches_stock_reconcile_required_idx
  ON obligation_batches(tenant_id, updated_at, batch_id)
  WHERE (
    context #>> '{defer_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{shift_repair,state}' = 'stock_reconcile_required'
    OR context #>> '{cancel_repair,state}' = 'stock_reconcile_required'
  );

-- +goose Down
DROP INDEX IF EXISTS obligation_batches_stock_reconcile_required_idx;
