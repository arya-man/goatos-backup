-- +goose Up
-- +goose NO TRANSACTION
-- Covering index for the batch lookup in ShedCompletionSummary (and shedCompletionVaccineBreakdown):
--   SELECT ... FROM obligation_batches ob WHERE ob.tenant_id = $1 AND ob.sop_task_id = $2
-- No existing obligation_batches index leads with (tenant_id, sop_task_id) -- the closest,
-- obligation_batches_scope_idx, leads with (tenant_id, scope_type, scope_id, status) and does not
-- cover a sop_task_id equality probe -- so the read planned a sequential scan on obligation_batches.
-- Add a partial index keyed on (tenant_id, sop_task_id) so the shed-completion read is an index
-- probe. Partial on sop_task_id IS NOT NULL because planned batches carry a NULL sop_task_id until
-- a task is materialized; only task-linked rows are ever probed here, keeping the index small.
--
-- Lock-safe: CREATE INDEX CONCURRENTLY (cannot run inside a transaction -- hence NO TRANSACTION),
-- IF NOT EXISTS so re-runs / forward-compat databases are a no-op. Mirrors the 000004 concurrent
-- index pattern.
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_batches_sop_task_idx
  ON public.obligation_batches USING btree (tenant_id, sop_task_id)
  WHERE (sop_task_id IS NOT NULL);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.obligation_batches_sop_task_idx;
