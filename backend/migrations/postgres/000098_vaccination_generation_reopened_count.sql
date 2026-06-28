-- +goose Up
-- Persist reopened deferred obligations in generation-run status so manual campaign / publish
-- replays and operator-visible responses do not lose recovery recheck work.

ALTER TABLE vaccination_generation_runs
  ADD COLUMN reopened_count int NOT NULL DEFAULT 0;

ALTER TABLE vaccination_generation_runs
  DROP CONSTRAINT IF EXISTS vaccination_generation_runs_counts_check;

ALTER TABLE vaccination_generation_runs
  ADD CONSTRAINT vaccination_generation_runs_counts_check CHECK (
    generated_count >= 0
    AND deferred_count >= 0
    AND reopened_count >= 0
    AND skipped_no_due_date_count >= 0
    AND suppressed_trusted_history_count >= 0
  );

-- +goose Down
ALTER TABLE vaccination_generation_runs
  DROP CONSTRAINT IF EXISTS vaccination_generation_runs_counts_check;

ALTER TABLE vaccination_generation_runs
  ADD CONSTRAINT vaccination_generation_runs_counts_check CHECK (
    generated_count >= 0
    AND deferred_count >= 0
    AND skipped_no_due_date_count >= 0
    AND suppressed_trusted_history_count >= 0
  );

ALTER TABLE vaccination_generation_runs
  DROP COLUMN IF EXISTS reopened_count;
