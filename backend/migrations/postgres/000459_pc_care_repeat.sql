-- +goose Up
-- 000459_pc_care_repeat.sql
--
-- PC CARE REPEAT (maintainer instruction 2026-09-30): each Preventive Care SOP card may say
-- "repeat every N days". The pc-care-repeat kernel stage then plans the NEXT task of that work
-- for the same pen(s) and the same operator(s), N days after the last task's PLANNED date,
-- whether or not the last one is finished. If none of the last operators can still do it
-- (left, inactive, moved park) that pen is skipped and the person who planned the last task is
-- alerted -- nobody is substituted.
--
-- Rollout-safe, every change additive:
--
--   1. pc_care_tasks.repeat_of_task_id names the task a repeat was made from. Its unique index
--      ("a task is repeated at most once" as a database fact, so two racing ticks cannot both
--      insert) is built CONCURRENTLY in 000460, a NO TRANSACTION migration of its own.
--   2. pc_care_repeat_skips remembers a source the stage could not repeat (and alerted about),
--      so the alert fires once and the stage stops re-examining it. A planner who plans that pen
--      again by hand starts a new chain from their own task.
--
-- seed-fixture-guard:ignore: nullable column + ledger table for a kernel-created follow-up; no
-- vaccination / HRMS / goats schema moves.

-- A nullable column with no default is a catalog-only change; the lock is bounded anyway.
SET lock_timeout = '5s';
ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS repeat_of_task_id uuid;
RESET lock_timeout;

COMMENT ON COLUMN public.pc_care_tasks.repeat_of_task_id IS
  'The task this one was repeated from by the pc-care-repeat stage (SOP card repeat_every_days, 2026-09-30); NULL for a task a person planned.';


CREATE TABLE IF NOT EXISTS public.pc_care_repeat_skips (
  tenant_id      uuid NOT NULL REFERENCES public.tenants (tenant_id),
  source_task_id uuid NOT NULL,
  reason         text NOT NULL CHECK (reason IN ('no_operator_available', 'removal_window_closed')),
  due_date       date NOT NULL,
  alerted_user   uuid,
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, source_task_id)
);

COMMENT ON TABLE public.pc_care_repeat_skips IS
  'A PC Care task the repeat stage could not repeat (none of its operators can still do the work); the planner was alerted once. 2026-09-30.';

-- +goose Down
DROP TABLE IF EXISTS public.pc_care_repeat_skips;
SET lock_timeout = '5s';
ALTER TABLE public.pc_care_tasks DROP COLUMN IF EXISTS repeat_of_task_id;
RESET lock_timeout;
