-- +goose Up
-- LEADERSHIP TASK DEADLINE (maintainer decision 2026-09-14): every task raised from the task
-- form carries a deadline -- a date AND time, entered by the raiser and editable by the raiser
-- while the task is open for work. The screens show one big number beside the task, the
-- farm-calendar days taken since the raise day, green within the deadline and red past it;
-- a finished task freezes that clock at its finish instant. The number and its colour are
-- composed by the backend from this column and the row's raised_at / done_at / cancelled_at
-- (backend/internal/leadershiptasks/domain/deadline.go); nothing is stored twice.
--
-- NULL is a task raised before this existed, or by the Work Board flag, which has no form to
-- ask: such a task shows no counter until its raiser sets a deadline. No backfill invents one.
--
-- Seed coupling note (docs/runbooks/initial-seed-migration-coupling.md): leadership_tasks is
-- an OPERATIONAL table born at runtime; no seed command hand-fills it.
-- seed-fixture-guard:ignore: runtime leadership-raised task column, not seed input.
ALTER TABLE public.leadership_tasks
  ADD COLUMN IF NOT EXISTS deadline_at timestamptz;

ALTER TABLE public.leadership_tasks
  DROP CONSTRAINT IF EXISTS leadership_tasks_deadline_after_raise;
ALTER TABLE public.leadership_tasks
  ADD CONSTRAINT leadership_tasks_deadline_after_raise
  CHECK (deadline_at IS NULL OR deadline_at > raised_at);

COMMENT ON COLUMN public.leadership_tasks.deadline_at IS
  'Date and time the raiser asked for the task by; NULL for a task raised without one. The day counter and its green/red tone are derived from this and raised_at/done_at/cancelled_at at read time.';

-- +goose Down
ALTER TABLE public.leadership_tasks
  DROP CONSTRAINT IF EXISTS leadership_tasks_deadline_after_raise;
ALTER TABLE public.leadership_tasks
  DROP COLUMN IF EXISTS deadline_at;
