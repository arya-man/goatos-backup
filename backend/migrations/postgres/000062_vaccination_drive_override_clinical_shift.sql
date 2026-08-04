-- +goose Up
ALTER TABLE vaccination_drive_date_overrides
  ADD COLUMN IF NOT EXISTS requested_override_date date,
  ADD COLUMN IF NOT EXISTS shift_reason text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS clinical_shift_metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE vaccination_drive_date_overrides
SET requested_override_date = override_date
WHERE requested_override_date IS NULL;

ALTER TABLE vaccination_drive_date_overrides
  ALTER COLUMN requested_override_date SET NOT NULL;

ALTER TABLE vaccination_drive_date_overrides
  ADD CONSTRAINT vaccination_drive_date_overrides_metadata_object_check
  CHECK (jsonb_typeof(clinical_shift_metadata) = 'object');

-- +goose Down
ALTER TABLE vaccination_drive_date_overrides
  DROP CONSTRAINT IF EXISTS vaccination_drive_date_overrides_metadata_object_check;

ALTER TABLE vaccination_drive_date_overrides
  DROP COLUMN IF EXISTS clinical_shift_metadata,
  DROP COLUMN IF EXISTS shift_reason,
  DROP COLUMN IF EXISTS requested_override_date;
