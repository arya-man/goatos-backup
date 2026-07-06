-- +goose Up
-- Drive planner decision state (TRD §4.7 / §6A): one-time batching hold + species grouping audit.
ALTER TABLE obligation_instances
  ADD COLUMN IF NOT EXISTS batching_hold_count integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS first_batching_hold_until timestamptz NULL,
  ADD COLUMN IF NOT EXISTS species_grouping_key text NULL;

ALTER TABLE obligation_instances
  DROP CONSTRAINT IF EXISTS obligation_instances_batching_hold_count_check;

ALTER TABLE obligation_instances
  ADD CONSTRAINT obligation_instances_batching_hold_count_check
  CHECK (batching_hold_count >= 0);

-- +goose Down
ALTER TABLE obligation_instances
  DROP CONSTRAINT IF EXISTS obligation_instances_batching_hold_count_check;

ALTER TABLE obligation_instances
  DROP COLUMN IF EXISTS species_grouping_key,
  DROP COLUMN IF EXISTS first_batching_hold_until,
  DROP COLUMN IF EXISTS batching_hold_count;
