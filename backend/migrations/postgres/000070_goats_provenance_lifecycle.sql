-- +goose Up
-- Phase 0 · Goats provenance + lifecycle hardening (schema only; no rule values, no obligations).
-- Adds birth/origin/exit provenance columns and tightens free-text lifecycle/health/stage
-- columns to closed CHECK enums. Tagging stays in goat_identifiers, surfaced as a view.
--
-- dev apply: the lifecycle/health/management enum CHECKs are added NOT VALID so applying
-- to goatos-dev never scans or rejects existing rows (they enforce all new/updated rows
-- immediately). Enumerate + map the distinct dev values, then run `VALIDATE CONSTRAINT` in a
-- follow-up migration. dob is backfilled from the existing approx_dob (canonical age source).

ALTER TABLE goats
  ADD COLUMN dob date NULL,
  ADD COLUMN dob_estimated boolean NOT NULL DEFAULT true,
  ADD COLUMN origin_type text NULL,
  ADD COLUMN entry_date date NULL,
  ADD COLUMN exited_at timestamptz NULL,
  ADD COLUMN exit_reason text NULL;

-- Age source-of-truth: approx_dob (existing column, populated for migrated goats) is the
-- canonical birth date; backfill the new dob from it so age-based PC obligations generate
-- for existing goats. approx_dob is retained as provenance. New intake sets dob directly.
UPDATE goats SET dob = approx_dob, dob_estimated = true
WHERE dob IS NULL AND approx_dob IS NOT NULL;

-- Constraints on the NEW (all-NULL for existing rows) columns are safe as VALID.
ALTER TABLE goats
  ADD CONSTRAINT goats_origin_type_check
    CHECK (origin_type IS NULL OR origin_type IN ('birth', 'procured', 'imported', 'unknown')),
  ADD CONSTRAINT goats_exit_reason_check
    CHECK (exit_reason IS NULL OR exit_reason IN ('sold', 'died', 'culled', 'transferred', 'lost')),
  -- exited_at is NULL for every existing row, so this passes the existing-row scan.
  ADD CONSTRAINT goats_exited_lifecycle_check
    CHECK (exited_at IS NULL OR lifecycle_status IN (
      'dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive'));

-- Enum tightening on EXISTING free-text columns: NOT VALID so apply never scans/rejects
-- legacy dev rows. Enforced immediately for all new/updated rows. Run
-- `VALIDATE CONSTRAINT` only AFTER dev distinct values are enumerated/mapped (header note).
ALTER TABLE goats
  ADD CONSTRAINT goats_lifecycle_status_check
    CHECK (lifecycle_status IN (
      'alive', 'sick', 'under_treatment', 'quarantine', 'icu',
      'dead', 'sold', 'culled', 'transferred', 'lost', 'merged', 'inactive')) NOT VALID;
ALTER TABLE goats
  ADD CONSTRAINT goats_health_status_check
    CHECK (health_status IS NULL OR health_status IN (
      'healthy', 'sick', 'under_treatment', 'recovering', 'quarantine', 'icu')) NOT VALID;
-- NOTE: management_stage is intentionally NOT tightened to a static CHECK enum. Its vocabulary is
-- data-driven via animal_stage_lookup.stage_code (000071) and varies by tenant; a hardcoded list
-- would reject legitimate values. Stage integrity is enforced by referencing animal_stage_lookup
-- (Phase 1, once stages are seeded), not by a static enum on goats.

-- Tagging = active identifiers, exposed as a view (no column on goats).
CREATE VIEW vw_goat_tagging AS
SELECT
  gi.tenant_id,
  gi.goat_id,
  gi.identifier_id,
  gi.identifier_type,
  gi.identifier_value,
  gi.normalized_value,
  gi.scope_key,
  gi.is_primary_for_goat,
  gi.status,
  gi.valid_from,
  gi.valid_to
FROM goat_identifiers gi
WHERE gi.status = 'active';

-- +goose Down
DROP VIEW IF EXISTS vw_goat_tagging;
ALTER TABLE goats
  DROP CONSTRAINT IF EXISTS goats_exited_lifecycle_check,
  DROP CONSTRAINT IF EXISTS goats_health_status_check,
  DROP CONSTRAINT IF EXISTS goats_lifecycle_status_check,
  DROP CONSTRAINT IF EXISTS goats_exit_reason_check,
  DROP CONSTRAINT IF EXISTS goats_origin_type_check;
ALTER TABLE goats
  DROP COLUMN IF EXISTS exit_reason,
  DROP COLUMN IF EXISTS exited_at,
  DROP COLUMN IF EXISTS entry_date,
  DROP COLUMN IF EXISTS origin_type,
  DROP COLUMN IF EXISTS dob_estimated,
  DROP COLUMN IF EXISTS dob;
