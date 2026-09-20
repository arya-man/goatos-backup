-- +goose Up
-- Authored toxin procedures allow 1..20 steps. Preserve the positive bounded
-- step identity while allowing the completions the procedure validator accepts.
ALTER TABLE public.toxin_test_step_completions
  DROP CONSTRAINT toxin_test_step_completions_step_no_check,
  ADD CONSTRAINT toxin_test_step_completions_step_no_check CHECK (step_no BETWEEN 1 AND 20);

-- +goose Down
-- Refuse rollback if longer procedures have recorded evidence; never delete it.
ALTER TABLE public.toxin_test_step_completions
  DROP CONSTRAINT toxin_test_step_completions_step_no_check,
  ADD CONSTRAINT toxin_test_step_completions_step_no_check CHECK (step_no BETWEEN 1 AND 7);
