-- +goose Up
-- +goose NO TRANSACTION
-- Keep Calendar's bounded exception catch-up path index-selective.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_obligation_instances_calendar_exceptions_due
ON public.obligation_instances (tenant_id, status, due_at)
WHERE batch_id IS NULL
  AND status IN ('missed', 'in_progress', 'deferred');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.idx_obligation_instances_calendar_exceptions_due;
