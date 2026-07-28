-- +goose Up
-- +goose NO TRANSACTION
-- The Birth/Death header asks for only the five most recent previous business dates that still
-- have actionable overdue work. workflow_instances grows with herd history, so keep the lookup on
-- the open-card subset and create the index without blocking production writes.
SET lock_timeout = '5s';

CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_overdue_dates_idx
  ON public.workflow_instances (tenant_id, module, event_date DESC, next_due_at)
  WHERE state = 'open' AND next_due_at IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_overdue_dates_idx;
