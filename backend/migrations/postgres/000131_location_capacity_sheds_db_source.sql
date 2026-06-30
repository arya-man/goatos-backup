-- +goose Up
ALTER TABLE location_capacity_records
  DROP CONSTRAINT IF EXISTS location_capacity_records_source_check;

ALTER TABLE location_capacity_records
  ADD CONSTRAINT location_capacity_records_source_check
  CHECK (source IN ('manual', 'legacy_bq', 'android_sop', 'import', 'sheds_db'));

-- +goose Down
ALTER TABLE location_capacity_records
  DROP CONSTRAINT IF EXISTS location_capacity_records_source_check;

ALTER TABLE location_capacity_records
  ADD CONSTRAINT location_capacity_records_source_check
  CHECK (source IN ('manual', 'legacy_bq', 'android_sop', 'import'));
