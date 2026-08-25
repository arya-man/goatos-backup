-- +goose Up
-- seed-migration-guard:ignore owner=raviteja issue=vaccination-procurement-purpose-prefilter reason=compiled-execution-index-default-all-no-seed-source-impact expiry=2026-11-30
ALTER TABLE protocol_rule_dimensions
  ADD COLUMN IF NOT EXISTS procurement_purpose text DEFAULT 'all' NOT NULL;

-- seed-migration-guard:ignore owner=raviteja issue=vaccination-procurement-purpose-prefilter reason=compiled-execution-index-default-all-no-seed-source-impact expiry=2026-11-30
ALTER TABLE protocol_rule_dimensions
  DROP CONSTRAINT IF EXISTS protocol_rule_dimensions_procurement_purpose_check;

-- seed-migration-guard:ignore owner=raviteja issue=vaccination-procurement-purpose-prefilter reason=compiled-execution-index-default-all-no-seed-source-impact expiry=2026-11-30
ALTER TABLE protocol_rule_dimensions
  ADD CONSTRAINT protocol_rule_dimensions_procurement_purpose_check
  CHECK (procurement_purpose <> '');

DROP INDEX IF EXISTS protocol_rule_dimensions_match_folded_idx;
CREATE INDEX protocol_rule_dimensions_match_folded_idx
  ON protocol_rule_dimensions (
    tenant_id,
    protocol_version_id,
    category,
    species,
    lower(animal_stage),
    sex,
    lower(breed),
    procurement_purpose
  );

-- +goose Down
DROP INDEX IF EXISTS protocol_rule_dimensions_match_folded_idx;
CREATE INDEX protocol_rule_dimensions_match_folded_idx
  ON protocol_rule_dimensions (
    tenant_id,
    protocol_version_id,
    category,
    species,
    lower(animal_stage),
    sex,
    lower(breed)
  );

-- seed-migration-guard:ignore owner=raviteja issue=vaccination-procurement-purpose-prefilter reason=compiled-execution-index-default-all-no-seed-source-impact expiry=2026-11-30
ALTER TABLE protocol_rule_dimensions
  DROP CONSTRAINT IF EXISTS protocol_rule_dimensions_procurement_purpose_check;

-- seed-migration-guard:ignore owner=raviteja issue=vaccination-procurement-purpose-prefilter reason=compiled-execution-index-default-all-no-seed-source-impact expiry=2026-11-30
ALTER TABLE protocol_rule_dimensions
  DROP COLUMN IF EXISTS procurement_purpose;
