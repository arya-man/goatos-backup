-- +goose Up
-- SOP tasks created by the obligation sweeper are idempotent by obligation batch.
-- The batch id lives in task context so the SOP table does not need a module-specific column.
CREATE UNIQUE INDEX sop_tasks_obligation_batch_id_unique_idx
  ON sop_tasks (tenant_id, (context ->> 'obligation_batch_id'))
  WHERE context ? 'obligation_batch_id';

-- +goose Down
DROP INDEX IF EXISTS sop_tasks_obligation_batch_id_unique_idx;
