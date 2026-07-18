-- +goose Up
-- Keep already-created databases compatible with the vaccination capacity enum rename.
-- Clean-slate DBs get this from the baseline; dev/stg DBs that already applied the baseline
-- need a forward migration instead of relying on edited historical SQL.

-- Drop old constraint first to allow the update.
ALTER TABLE vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_overflow_check;

-- Now update rows to the new enum value.
UPDATE vaccination_capacity_config
SET overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap'
WHERE overflow_policy = 'split_within_safe_window_then_mark_needs_review';

-- Add the new constraint on the updated value.
ALTER TABLE vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_overflow_check
  CHECK (overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap');

-- +goose Down
-- Revert the migration: drop new constraint, restore old value, add old constraint back.
ALTER TABLE vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_overflow_check;

UPDATE vaccination_capacity_config
SET overflow_policy = 'split_within_safe_window_then_mark_needs_review'
WHERE overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap';

ALTER TABLE vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_overflow_check
  CHECK (overflow_policy = 'split_within_safe_window_then_mark_needs_review');
