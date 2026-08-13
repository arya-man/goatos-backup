-- +goose Up
-- Milk Preparation tasks are shed-day facts. Existing park-day rows are retired in place so
-- immutable attempts and audit references remain readable, but can never drive current work.
ALTER TABLE milk_preparation_completions
  ADD COLUMN shed_id uuid;

ALTER TABLE milk_preparation_completions
  DROP CONSTRAINT milk_preparation_completions_status_check;

ALTER TABLE milk_preparation_completions
  ADD CONSTRAINT milk_preparation_completions_status_check
  CHECK (status IN ('pending_verification', 'completed', 'rework', 'retired')) NOT VALID;

UPDATE milk_preparation_completions
SET status = 'retired', updated_at = now();

ALTER TABLE milk_preparation_completions
  VALIDATE CONSTRAINT milk_preparation_completions_status_check;

ALTER TABLE milk_preparation_completions
  ADD CONSTRAINT milk_preparation_completions_shed_required_check
  CHECK (status = 'retired' OR shed_id IS NOT NULL) NOT VALID;
ALTER TABLE milk_preparation_completions
  VALIDATE CONSTRAINT milk_preparation_completions_shed_required_check;

ALTER TABLE milk_preparation_completions
  ADD CONSTRAINT milk_preparation_completions_shed_fk
  FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id) NOT VALID;
ALTER TABLE milk_preparation_completions
  VALIDATE CONSTRAINT milk_preparation_completions_shed_fk;

ALTER TABLE milk_preparation_completions
  DROP CONSTRAINT milk_preparation_completions_natural_uq;

CREATE UNIQUE INDEX milk_preparation_completions_shed_day_uidx
  ON milk_preparation_completions (tenant_id, shed_id, preparation_date);

DROP INDEX milk_preparation_completions_serving_idx;
CREATE INDEX milk_preparation_completions_serving_idx
  ON milk_preparation_completions (tenant_id, preparation_date, park_id, shed_id, status);

-- +goose Down
-- Forward-only business grain correction: restoring park-day rows would merge independent shed
-- evidence and fabricate task state. Retained history therefore cannot be safely rolled back.
SELECT 1;
