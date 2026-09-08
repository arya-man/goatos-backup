-- +goose Up
ALTER TABLE public.leadership_tasks
  ADD COLUMN IF NOT EXISTS raised_by_designation text;

COMMENT ON COLUMN public.leadership_tasks.raised_by_designation IS
  'Director designation the raiser held when the task was created; used to gate leadership.task_done notification audiences by the actual addressed title.';

-- +goose Down
ALTER TABLE public.leadership_tasks
  DROP COLUMN IF EXISTS raised_by_designation;
