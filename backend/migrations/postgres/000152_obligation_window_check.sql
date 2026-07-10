-- +goose Up
ALTER TABLE obligation_instances
  ADD CONSTRAINT obligation_instances_window_check
  CHECK (window_end IS NULL OR window_start IS NULL OR window_end >= window_start);

-- +goose Down
ALTER TABLE obligation_instances
  DROP CONSTRAINT IF EXISTS obligation_instances_window_check;
