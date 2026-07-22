-- +goose Up
ALTER TABLE vaccination_drive_assignments
  ADD COLUMN IF NOT EXISTS vaccine_rule_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
  ADD COLUMN IF NOT EXISTS total_doses integer NOT NULL DEFAULT 0;

ALTER TABLE vaccination_drive_assignments
  DROP CONSTRAINT IF EXISTS vaccination_drive_assignments_total_doses_check,
  ADD CONSTRAINT vaccination_drive_assignments_total_doses_check
    CHECK (total_doses >= 0);

-- +goose Down
ALTER TABLE vaccination_drive_assignments
  DROP CONSTRAINT IF EXISTS vaccination_drive_assignments_total_doses_check,
  DROP COLUMN IF EXISTS total_doses,
  DROP COLUMN IF EXISTS vaccine_rule_ids;
