-- +goose Up
-- MarkMissedBefore considers open in-progress obligations whose batch is no longer in progress.
-- The deadline index must cover the same status predicate or large due scans fall back to the
-- broader open set exactly when a missed backlog is largest.
DROP INDEX IF EXISTS obligation_instances_missed_deadline_idx;

CREATE INDEX obligation_instances_missed_deadline_idx
  ON obligation_instances (tenant_id, status, (COALESCE(window_end, due_at)), obligation_id)
  WHERE status IN ('scheduled', 'due', 'in_progress');

-- +goose Down
DROP INDEX IF EXISTS obligation_instances_missed_deadline_idx;

CREATE INDEX obligation_instances_missed_deadline_idx
  ON obligation_instances (tenant_id, status, (COALESCE(window_end, due_at)), obligation_id)
  WHERE status IN ('scheduled', 'due');
