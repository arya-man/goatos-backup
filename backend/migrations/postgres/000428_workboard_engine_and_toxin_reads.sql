-- +goose Up
-- +goose NO TRANSACTION
-- The Work Board now rows EVERY shared-engine workflow (births, deaths, pen moves, pen returns,
-- sales, animal and feed purchases, general SOP runs) and every toxin round, for one park and one
-- day (docs/decisions/work-board.md -> "Every module is on the board"). Both reads are bounded by
-- tenant + park first, which neither table had an index for: workflow_instances is keyed by
-- (tenant, module, event_date) for the phone list, and toxin_test_tasks by (tenant, status,
-- created_at) for the tester's list. Built CONCURRENTLY so production writes never block.
--
-- Each read is a UNION of disjoint day arms, and every arm has an index that bounds it by the DAY,
-- never by tenant + park alone: history (completed workflows, accepted rounds) grows forever, so an
-- arm that has to walk it and filter is a hot read that slows every day (review of PR #429).
-- seed-fixture-guard:ignore: read indexes only, no seedable data.
SET lock_timeout = '5s';

-- Raised on the day (state open or completed, event_date = D) and still open from an earlier day
-- (state open, event_date < D). Canceled workflows are never read.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_board_idx
  ON public.workflow_instances (tenant_id, park_id, state, event_date, workflow_id)
  WHERE state <> 'canceled';

-- Completed on the day: updated_at inside D's IST bounds, a range on this index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_instances_board_completed_idx
  ON public.workflow_instances (tenant_id, park_id, updated_at)
  WHERE state = 'completed';

-- A live round, per park code (a round is load-grain and carries its park only as the load's
-- farm_label). Live rounds are what is still owed, so this index stays small.
CREATE INDEX CONCURRENTLY IF NOT EXISTS toxin_test_tasks_board_idx
  ON public.toxin_test_tasks (tenant_id, (upper(btrim(farm_label))), status, created_at, task_id)
  WHERE status IN ('in_progress', 'pending_review');

-- An accepted round is on the board up to the day it was signed off: reviewed_at on or after the
-- start of D, a range on this index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS toxin_test_tasks_board_accepted_idx
  ON public.toxin_test_tasks (tenant_id, (upper(btrim(farm_label))), reviewed_at)
  WHERE status = 'accepted';

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.toxin_test_tasks_board_accepted_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.toxin_test_tasks_board_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_board_completed_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.workflow_instances_board_idx;
