-- +goose Up
INSERT INTO legacy_status_mappings (
  source_system,
  raw_label,
  normalized_raw_label,
  lifecycle_status,
  reproductive_status,
  growth_cohort_tag,
  management_stage,
  health_status,
  sex_override,
  display_status_code,
  confidence,
  review_required,
  notes,
  created_at,
  updated_at
) VALUES
  ('legacy_rfid_db', 'K1', 'k1', 'alive', NULL, 'K1', NULL, NULL, NULL, 'K1', 'high', false, 'Plain K1 maps to growth cohort only; source Gender remains sex truth.', now(), now())
ON CONFLICT (source_system, normalized_raw_label) DO UPDATE
SET
  raw_label = EXCLUDED.raw_label,
  lifecycle_status = EXCLUDED.lifecycle_status,
  reproductive_status = EXCLUDED.reproductive_status,
  growth_cohort_tag = EXCLUDED.growth_cohort_tag,
  management_stage = EXCLUDED.management_stage,
  health_status = EXCLUDED.health_status,
  sex_override = EXCLUDED.sex_override,
  display_status_code = EXCLUDED.display_status_code,
  confidence = EXCLUDED.confidence,
  review_required = EXCLUDED.review_required,
  notes = EXCLUDED.notes,
  updated_at = now();

-- +goose Down
DELETE FROM legacy_status_mappings
WHERE source_system = 'legacy_rfid_db'
  AND normalized_raw_label = 'k1'
  AND raw_label = 'K1';
