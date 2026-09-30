-- +goose Up
-- +goose NO TRANSACTION
-- 000460_pc_care_repeat_uidx.sql
--
-- PC CARE REPEAT (2026-09-30, docs/decisions/pc-care-repeat.md): a task is repeated at most once,
-- as a database fact -- two kernel ticks racing on the same source cannot both insert. Built
-- CONCURRENTLY in a NO TRANSACTION migration (PR #457 review; the 000208 / 000339 shape) so
-- writes to pc_care_tasks are never blocked while it builds. Until it exists the repeat read
-- already excludes a source that has a repeat, so nothing depends on it being instant.
--
-- seed-fixture-guard:ignore: index only.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS pc_care_tasks_repeat_of_uidx
  ON public.pc_care_tasks (tenant_id, repeat_of_task_id)
  WHERE repeat_of_task_id IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.pc_care_tasks_repeat_of_uidx;
