-- +goose Up
ALTER TABLE obligation_instances
  ADD COLUMN IF NOT EXISTS batching_hold_count integer NULL,
  ADD COLUMN IF NOT EXISTS first_batching_hold_until timestamptz NULL;

ALTER TABLE obligation_instances
  ALTER COLUMN batching_hold_count SET DEFAULT 0;

-- +goose Down
ALTER TABLE obligation_instances
  ALTER COLUMN batching_hold_count DROP DEFAULT;

ALTER TABLE obligation_instances
  DROP COLUMN IF EXISTS first_batching_hold_until,
  DROP COLUMN IF EXISTS batching_hold_count;
