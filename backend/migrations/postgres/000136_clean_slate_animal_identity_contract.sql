-- +goose Up
-- Clean-slate herd identity contract for V1.
--
-- This migration intentionally rejects/cuts the old dashboard/BQ-port identity
-- model instead of preserving it. Dev/test data must be wiped and reseeded as
-- valid GoatOS herd data: goat/sheep species, female/male sex, approved origin,
-- and exactly the two lifetime Animal IDs.

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM goats
    WHERE species NOT IN ('goat', 'sheep')
  ) THEN
    RAISE EXCEPTION 'goats contains invalid species; clean-slate reseed required before applying 000136';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM goats
    WHERE origin_type IS NOT NULL AND origin_type NOT IN ('birth', 'procured', 'imported')
  ) THEN
    RAISE EXCEPTION 'goats contains invalid origin_type; clean-slate reseed required before applying 000136';
  END IF;

  IF EXISTS (
    SELECT 1
    FROM goat_identifiers
    WHERE identifier_type NOT IN ('animal_identifier_1', 'animal_identifier_2')
  ) THEN
    DELETE FROM goat_identifiers
    WHERE identifier_type NOT IN ('animal_identifier_1', 'animal_identifier_2');
  END IF;

  IF EXISTS (
    SELECT 1
    FROM (
      SELECT tenant_id, normalized_value
      FROM goat_identifiers
      GROUP BY tenant_id, normalized_value
      HAVING count(*) > 1
    ) dup
  ) THEN
    RAISE EXCEPTION 'goat_identifiers contains duplicate Animal ID values; clean-slate reseed required before applying 000136';
  END IF;
END $$;

-- Kill the BQ/import-review/dashboard-port objects in the final migrated schema.
-- Clean V1 is reseeded from valid GoatOS facts; it does not keep review queues
-- or mirrors whose only job was to carry old spreadsheets forward.
DROP VIEW IF EXISTS goat_identity_counter_memberships CASCADE;
DROP VIEW IF EXISTS vw_procurement_vaccination_excluded_goats CASCADE;

DROP TABLE IF EXISTS legacy_sync_run_conflicts CASCADE;
DROP TABLE IF EXISTS legacy_sync_source_status CASCADE;
DROP TABLE IF EXISTS legacy_sync_source_watermarks CASCADE;
DROP TABLE IF EXISTS legacy_sync_run_steps CASCADE;
DROP TABLE IF EXISTS legacy_sync_runs CASCADE;
DROP TABLE IF EXISTS legacy_sync_sources CASCADE;

DROP TABLE IF EXISTS identity_match_candidates CASCADE;
DROP TABLE IF EXISTS legacy_import_rows CASCADE;
DROP TABLE IF EXISTS legacy_import_runs CASCADE;
DROP TABLE IF EXISTS legacy_import_policies CASCADE;
DROP TABLE IF EXISTS legacy_status_mappings CASCADE;

DROP TABLE IF EXISTS goat_identity_counter_processed_events CASCADE;
DROP TABLE IF EXISTS goat_identity_counter_projection_state CASCADE;
DROP TABLE IF EXISTS goat_identity_counters CASCADE;

DROP TABLE IF EXISTS counts_current_snapshot_rows CASCADE;
DROP TABLE IF EXISTS counts_projection_rows CASCADE;
DROP TABLE IF EXISTS counts_projection_state CASCADE;
DROP TABLE IF EXISTS counts_source_rows CASCADE;
DROP TABLE IF EXISTS counts_sync_runs CASCADE;
DROP TABLE IF EXISTS feature_coverage_registry CASCADE;

DROP TABLE IF EXISTS mortality_review_items CASCADE;
DROP TABLE IF EXISTS mortality_events CASCADE;
DROP TABLE IF EXISTS mortality_projection_rows CASCADE;
DROP TABLE IF EXISTS mortality_projection_state CASCADE;
DROP TABLE IF EXISTS mortality_source_rows CASCADE;
DROP TABLE IF EXISTS mortality_sync_runs CASCADE;

SELECT set_config('goatos.approved_location_migration_plan', 'clean-slate-v1', false);

DELETE FROM location_aliases
WHERE source_context LIKE 'legacy_%'
   OR source_context IN ('counts_source', 'mortality_source');

DELETE FROM location_capacity_records
WHERE source = 'legacy_bq';

ALTER TABLE location_capacity_records DROP CONSTRAINT IF EXISTS location_capacity_records_source_check;
ALTER TABLE location_capacity_records ADD CONSTRAINT location_capacity_records_source_check
  CHECK (source IN ('manual', 'android_sop', 'import', 'sheds_db'));

ALTER TABLE location_review_items
  DROP COLUMN IF EXISTS sync_run_id;

CREATE OR REPLACE FUNCTION location_seeded_scope_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  approved_plan text;
BEGIN
  approved_plan := current_setting('goatos.approved_location_migration_plan', true);
  IF approved_plan IS NULL OR btrim(approved_plan) = '' THEN
    IF TG_TABLE_NAME = 'locations' THEN
      IF OLD.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
        AND OLD.location_id IN (
          '00000000-0000-4000-8000-000000003001'::uuid,
          '00000000-0000-4000-8000-000000003002'::uuid,
          '00000000-0000-4000-8000-000000003003'::uuid
        ) THEN
        RAISE EXCEPTION 'seeded CBE/CPT/HF location scope requires approved migration plan';
      END IF;
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

DROP INDEX IF EXISTS procurement_load_goats_action_idx;
DROP INDEX IF EXISTS procurement_load_goats_intake_eligibility_idx;
DROP INDEX IF EXISTS procurement_load_goats_source_tag_idx;
DROP INDEX IF EXISTS procurement_load_goats_source_rfid_idx;
ALTER TABLE procurement_load_goats DROP CONSTRAINT IF EXISTS procurement_load_goats_identity_state_check;

ALTER TABLE procurement_load_goats RENAME COLUMN source_tag TO animal_identifier_1;
ALTER TABLE procurement_load_goats RENAME COLUMN source_rfid TO animal_identifier_2;
ALTER TABLE procurement_load_goats DROP COLUMN IF EXISTS temporary_id;
ALTER TABLE procurement_load_goats RENAME COLUMN identity_review_state TO source_entry_state;
ALTER TABLE procurement_load_goats RENAME COLUMN identity_review_ref TO source_entry_ref;

UPDATE procurement_load_goats
SET source_entry_state = CASE source_entry_state
  WHEN 'clean' THEN 'accepted'
  WHEN 'conflict' THEN 'blocked'
  WHEN 'unknown_extra' THEN 'blocked'
  WHEN 'pending' THEN 'pending'
  ELSE 'blocked'
END;

ALTER TABLE procurement_load_goats
  ALTER COLUMN source_entry_state SET DEFAULT 'pending';
ALTER TABLE procurement_load_goats ADD CONSTRAINT procurement_load_goats_source_entry_state_check
  CHECK (source_entry_state IN ('pending', 'accepted', 'blocked'));

CREATE INDEX procurement_load_goats_action_idx
  ON procurement_load_goats(tenant_id, current_state, ownership_state, source_entry_state, updated_at DESC, goat_id);
CREATE INDEX procurement_load_goats_intake_eligibility_idx
  ON procurement_load_goats(tenant_id, load_id, current_state, health_state, source_entry_state, ownership_state, goat_id)
  WHERE loaded_at IS NOT NULL AND arrived_at IS NOT NULL;
CREATE INDEX procurement_load_goats_animal_identifier_1_idx
  ON procurement_load_goats(tenant_id, lower(animal_identifier_1), load_id)
  WHERE animal_identifier_1 IS NOT NULL;
CREATE INDEX procurement_load_goats_animal_identifier_2_idx
  ON procurement_load_goats(tenant_id, animal_identifier_2, load_id)
  WHERE animal_identifier_2 IS NOT NULL;

ALTER TABLE arrival_intake_review_goats RENAME COLUMN source_tag TO animal_identifier_1;
ALTER TABLE arrival_intake_review_goats RENAME COLUMN temporary_id TO animal_identifier_2;

CREATE OR REPLACE VIEW vw_procurement_vaccination_excluded_goats AS
SELECT DISTINCT
  g.tenant_id,
  g.goat_id,
  CASE
    WHEN g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive') THEN g.lifecycle_status
    WHEN g.merged_into_goat_id IS NOT NULL THEN 'merged'
    WHEN plg.source_entry_state <> 'accepted' THEN 'source_entry_' || plg.source_entry_state
    WHEN plg.ownership_state NOT IN ('mesha_owned', 'settled') THEN 'ownership_' || plg.ownership_state
    WHEN plg.health_state <> 'passed' THEN 'health_' || plg.health_state
    WHEN plg.current_state <> 'accepted_herd_intake' THEN plg.current_state
    ELSE 'not_excluded'
  END AS exclusion_reason
FROM goats g
LEFT JOIN procurement_load_goats plg
  ON plg.tenant_id = g.tenant_id
 AND plg.goat_id = g.goat_id
WHERE g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive')
   OR g.merged_into_goat_id IS NOT NULL
   OR (
     plg.goat_id IS NOT NULL
     AND (
       plg.current_state <> 'accepted_herd_intake'
       OR plg.selection_state IN ('source_only', 'candidate', 'rejected', 'deferred', 'blocked', 'arrival_rejected', 'dead', 'sold', 'lost')
       OR plg.source_entry_state <> 'accepted'
       OR plg.ownership_state NOT IN ('mesha_owned', 'settled')
       OR plg.health_state <> 'passed'
     )
   );

CREATE OR REPLACE FUNCTION prevent_merged_goat_normal_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'UPDATE'
    AND OLD.merged_into_goat_id IS NOT NULL
    AND COALESCE(current_setting('goatos.allow_merged_goat_update', true), 'off') <> 'on'
  THEN
    RAISE EXCEPTION 'merged_goat_write_blocked: goat % redirects to %', OLD.goat_id, OLD.merged_into_goat_id
      USING ERRCODE = '23514';
  END IF;

  IF TG_OP = 'UPDATE' AND NEW.display_id <> OLD.display_id THEN
    RAISE EXCEPTION 'display_id is immutable for goat %', OLD.goat_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION block_merged_goat_child_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  target_redirect uuid;
BEGIN
  IF COALESCE(current_setting('goatos.allow_merged_goat_child_write', true), 'off') = 'on' THEN
    RETURN NEW;
  END IF;

  IF NEW.goat_id IS NULL THEN
    RETURN NEW;
  END IF;

  SELECT merged_into_goat_id
    INTO target_redirect
    FROM goats
    WHERE tenant_id = NEW.tenant_id
      AND goat_id = NEW.goat_id;

  IF target_redirect IS NOT NULL THEN
    RAISE EXCEPTION 'merged_goat_child_write_blocked: goat % redirects to %', NEW.goat_id, target_redirect
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION validate_goat_merge_link()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  survivor_redirect uuid;
  merged_redirect uuid;
BEGIN
  SELECT merged_into_goat_id
    INTO survivor_redirect
    FROM goats
    WHERE goat_id = NEW.survivor_goat_id;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'survivor goat % does not exist', NEW.survivor_goat_id
      USING ERRCODE = '23503';
  END IF;

  IF survivor_redirect IS NOT NULL THEN
    RAISE EXCEPTION 'survivor goat % must be live, not merged', NEW.survivor_goat_id
      USING ERRCODE = '23514';
  END IF;

  SELECT merged_into_goat_id
    INTO merged_redirect
    FROM goats
    WHERE goat_id = NEW.merged_goat_id;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'merged goat % does not exist', NEW.merged_goat_id
      USING ERRCODE = '23503';
  END IF;

  IF merged_redirect IS DISTINCT FROM NEW.survivor_goat_id THEN
    RAISE EXCEPTION 'merged goat % must redirect to survivor % before link insert', NEW.merged_goat_id, NEW.survivor_goat_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

DROP INDEX IF EXISTS goats_tenant_breed_display_idx;
DROP INDEX IF EXISTS goats_tenant_breed_sex_display_idx;
DROP INDEX IF EXISTS goats_tenant_lifecycle_display_idx;
DROP INDEX IF EXISTS goats_tenant_sex_display_idx;
ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_identity_state_check;
ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_merge_redirect_shape_check;
ALTER TABLE goats ADD CONSTRAINT goats_merge_redirect_shape_check
  CHECK (merged_into_goat_id IS NULL OR merged_into_goat_id <> goat_id);
ALTER TABLE goats DROP COLUMN IF EXISTS identity_state;

CREATE INDEX goats_tenant_breed_display_idx
  ON goats(tenant_id, breed, display_id)
  WHERE merged_into_goat_id IS NULL;
CREATE INDEX goats_tenant_breed_sex_display_idx
  ON goats(tenant_id, breed, sex, display_id)
  WHERE merged_into_goat_id IS NULL;
CREATE INDEX goats_tenant_lifecycle_display_idx
  ON goats(tenant_id, lifecycle_status, display_id)
  WHERE merged_into_goat_id IS NULL;
CREATE INDEX goats_tenant_sex_display_idx
  ON goats(tenant_id, sex, display_id)
  WHERE merged_into_goat_id IS NULL;

ALTER TABLE identity_conflicts DROP CONSTRAINT IF EXISTS identity_conflicts_type_check;
ALTER TABLE identity_conflicts ADD CONSTRAINT identity_conflicts_type_check
  CHECK (conflict_type IN (
    'duplicate_active_identifier',
    'duplicate_animal_identifier',
    'missing_required_identifier',
    'possible_duplicate_animal',
    'location_mismatch',
    'status_mismatch'
  ));

DELETE FROM identity_correction_requests
WHERE request_type NOT IN (
  'missing_animal_identifier',
  'animal_identifier_reused',
  'animal_identifier_conflict',
  'possible_duplicate',
  'wrong_location',
  'wrong_status',
  'field_verification_result',
  'identifier_seen_but_not_attached'
);
ALTER TABLE identity_correction_requests DROP CONSTRAINT IF EXISTS identity_correction_requests_type_check;
ALTER TABLE identity_correction_requests ADD CONSTRAINT identity_correction_requests_type_check
  CHECK (request_type IN (
    'missing_animal_identifier',
    'animal_identifier_reused',
    'animal_identifier_conflict',
    'possible_duplicate',
    'wrong_location',
    'wrong_status',
    'field_verification_result',
    'identifier_seen_but_not_attached'
  ));

ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_species_check;
ALTER TABLE goats ADD CONSTRAINT goats_species_check CHECK (species IN ('goat', 'sheep'));

ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_origin_type_check;
ALTER TABLE goats ADD CONSTRAINT goats_origin_type_check CHECK (origin_type IS NULL OR origin_type IN ('birth', 'procured', 'imported'));

ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_source_confidence_check;
ALTER TABLE goats DROP COLUMN IF EXISTS source_confidence;

ALTER TABLE status_definitions
  DROP COLUMN IF EXISTS legacy_label;

DELETE FROM count_source_import_runs
WHERE source_system NOT IN ('physical_base_count', 'manual_review', 'import', 'feed_shiftings_docx', 'goatos_canonical');

ALTER TABLE count_source_import_runs DROP CONSTRAINT IF EXISTS count_source_import_runs_source_system_check;
ALTER TABLE count_source_import_runs ADD CONSTRAINT count_source_import_runs_source_system_check
  CHECK (source_system IN ('physical_base_count', 'manual_review', 'import', 'feed_shiftings_docx', 'goatos_canonical'));

DELETE FROM shifting_events
WHERE source_system NOT IN ('feed_shiftings_docx', 'manual_review', 'import', 'goatos_canonical');

ALTER TABLE shifting_events DROP CONSTRAINT IF EXISTS shifting_events_source_check;
ALTER TABLE shifting_events ADD CONSTRAINT shifting_events_source_check
  CHECK (source_system IN ('feed_shiftings_docx', 'manual_review', 'import', 'goatos_canonical'));

DELETE FROM workforce_external_identities
WHERE source_system NOT IN ('slack', 'firebase', 'manual')
   OR external_ref_type NOT IN ('slack_user_id', 'email', 'phone', 'staff_label', 'firebase_uid');

ALTER TABLE workforce_external_identities DROP CONSTRAINT IF EXISTS workforce_external_identities_source_system_check;
ALTER TABLE workforce_external_identities ADD CONSTRAINT workforce_external_identities_source_system_check
  CHECK (source_system IN ('slack', 'firebase', 'manual'));

ALTER TABLE workforce_external_identities DROP CONSTRAINT IF EXISTS workforce_external_identities_ref_type_check;
ALTER TABLE workforce_external_identities ADD CONSTRAINT workforce_external_identities_ref_type_check
  CHECK (external_ref_type IN ('slack_user_id', 'email', 'phone', 'staff_label', 'firebase_uid'));

DELETE FROM identifier_policies
WHERE identifier_type NOT IN ('animal_identifier_1', 'animal_identifier_2');

ALTER TABLE identifier_policies DROP CONSTRAINT IF EXISTS identifier_policies_identifier_type_check;
ALTER TABLE identifier_policies ADD CONSTRAINT identifier_policies_identifier_type_check
  CHECK (identifier_type IN ('animal_identifier_1', 'animal_identifier_2'));

ALTER TABLE goat_identifiers DROP CONSTRAINT IF EXISTS goat_identifiers_type_check;
ALTER TABLE goat_identifiers ADD CONSTRAINT goat_identifiers_type_check
  CHECK (identifier_type IN ('animal_identifier_1', 'animal_identifier_2'));

DROP INDEX IF EXISTS goat_identifiers_active_rfid_unique;
DROP INDEX IF EXISTS goat_identifiers_active_scoped_unique;
DROP INDEX IF EXISTS goat_identifiers_lifetime_value_unique;
CREATE UNIQUE INDEX goat_identifiers_lifetime_value_unique
  ON goat_identifiers(tenant_id, normalized_value);

INSERT INTO identifier_policies (
  policy_version,
  identifier_type,
  default_scope_type,
  scope_required,
  active_uniqueness,
  auto_link_allowed,
  primary_allowed,
  unknown_scope_action,
  missing_or_conflicting_scope_action,
  normalizer_version,
  format_validator_version,
  invalid_value_action,
  created_at,
  approved_by
) VALUES
  ('phase1-identifier-v1', 'animal_identifier_1', 'global', true, 'global', false, true, 'reject', 'reject', 'identifier_normalizer_v1', NULL, 'reject', now(), NULL),
  ('phase1-identifier-v1', 'animal_identifier_2', 'global', true, 'global', false, false, 'reject', 'reject', 'identifier_normalizer_v1', NULL, 'reject', now(), NULL)
ON CONFLICT (policy_version, identifier_type) DO UPDATE
SET default_scope_type = EXCLUDED.default_scope_type,
    scope_required = EXCLUDED.scope_required,
    active_uniqueness = EXCLUDED.active_uniqueness,
    auto_link_allowed = EXCLUDED.auto_link_allowed,
    primary_allowed = EXCLUDED.primary_allowed,
    unknown_scope_action = EXCLUDED.unknown_scope_action,
    missing_or_conflicting_scope_action = EXCLUDED.missing_or_conflicting_scope_action,
    normalizer_version = EXCLUDED.normalizer_version,
    format_validator_version = EXCLUDED.format_validator_version,
    invalid_value_action = EXCLUDED.invalid_value_action,
    approved_by = EXCLUDED.approved_by;

-- +goose Down
-- Intentionally no-op. V1 clean-slate identity does not reopen old dashboard
-- identifier types, source confidence, unknown origin, or goat-only species.
DO $$
BEGIN
  RAISE NOTICE '000136 down is intentionally no-op: clean-slate Animal ID contract remains enforced';
END $$;
