-- +goose Up
-- +goose NO TRANSACTION
-- The Work Board now rows EVERY shared-engine workflow (births, deaths, pen moves, pen returns,
-- sales, animal and feed purchases, general SOP runs) and every toxin round, for one park and one
-- day (docs/decisions/work-board.md -> "Every module is on the board"). Both reads are bounded by
-- tenant + park first, which neither table had an index for: workflow_instances is keyed by
-- (tenant, module, event_date) for the phone list, and toxin_test_tasks by (tenant, status,
-- created_at) for the tester's list. Built CONCURRENTLY so production writes never block.
-- seed-fixture-guard:ignore: read indexes only, no seedable data.
SET lock_timeout = '5s';

-- The board's day predicate: raised on the day, still open from an earlier day, or completed that
-- day. Canceled workflows are never read.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_board_idx
  ON public.workflow_instances (tenant_id, park_id, state, event_date, workflow_id)
  WHERE state <> 'canceled';

-- A live or signed-off round, per park code (a round is load-grain and carries its park only as
-- the load's farm_label). Cancelled rounds are history and never on the board.
CREATE INDEX CONCURRENTLY IF NOT EXISTS toxin_test_tasks_board_idx
  ON public.toxin_test_tasks (tenant_id, (upper(btrim(farm_label))), status, created_at, task_id)
  WHERE status <> 'cancelled';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.toxin_test_tasks_board_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_board_idx;
