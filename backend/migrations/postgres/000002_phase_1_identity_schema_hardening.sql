-- +goose Up
ALTER TABLE goats
  ADD CONSTRAINT goats_tenant_goat_unique UNIQUE (tenant_id, goat_id);

ALTER TABLE locations
  ADD CONSTRAINT locations_tenant_location_unique UNIQUE (tenant_id, location_id);

ALTER TABLE locations
  ADD CONSTRAINT locations_parent_location_tenant_fk FOREIGN KEY (tenant_id, parent_location_id) REFERENCES locations(tenant_id, location_id);

ALTER TABLE location_aliases
  ADD CONSTRAINT location_aliases_canonical_location_tenant_fk FOREIGN KEY (tenant_id, canonical_location_id) REFERENCES locations(tenant_id, location_id);

ALTER TABLE identity_decisions
  ADD CONSTRAINT identity_decisions_tenant_decision_unique UNIQUE (tenant_id, decision_id);

ALTER TABLE goat_identifiers
  ADD CONSTRAINT goat_identifiers_tenant_identifier_unique UNIQUE (tenant_id, identifier_id);

ALTER TABLE legacy_import_runs
  ADD CONSTRAINT legacy_import_runs_tenant_run_unique UNIQUE (tenant_id, import_run_id);

ALTER TABLE legacy_import_rows
  ADD CONSTRAINT legacy_import_rows_tenant_row_unique UNIQUE (tenant_id, legacy_row_id);

ALTER TABLE identity_conflicts
  ADD CONSTRAINT identity_conflicts_tenant_conflict_unique UNIQUE (tenant_id, conflict_id);

ALTER TABLE goat_identity_events
  ADD CONSTRAINT goat_identity_events_tenant_event_recorded_unique UNIQUE (tenant_id, identity_event_id, recorded_at);

ALTER TABLE goats
  ADD CONSTRAINT goats_current_location_tenant_fk FOREIGN KEY (tenant_id, current_location_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goats_farm_tenant_fk FOREIGN KEY (tenant_id, farm_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goats_park_tenant_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goats_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goats_cohort_tenant_fk FOREIGN KEY (tenant_id, cohort_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goats_merged_into_tenant_fk FOREIGN KEY (tenant_id, merged_into_goat_id) REFERENCES goats(tenant_id, goat_id);

ALTER TABLE goat_identifiers
  ADD CONSTRAINT goat_identifiers_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id);

ALTER TABLE goat_location_history
  ADD CONSTRAINT goat_location_history_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT goat_location_history_from_location_tenant_fk FOREIGN KEY (tenant_id, from_location_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_location_history_to_location_tenant_fk FOREIGN KEY (tenant_id, to_location_id) REFERENCES locations(tenant_id, location_id);

ALTER TABLE goat_ownership
  ADD CONSTRAINT goat_ownership_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT goat_ownership_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE goat_custody_history
  ADD CONSTRAINT goat_custody_history_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT goat_custody_history_from_location_tenant_fk FOREIGN KEY (tenant_id, from_location_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_custody_history_to_location_tenant_fk FOREIGN KEY (tenant_id, to_location_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_custody_history_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE legacy_import_rows
  ADD CONSTRAINT legacy_import_rows_run_tenant_fk FOREIGN KEY (tenant_id, import_run_id) REFERENCES legacy_import_runs(tenant_id, import_run_id),
  ADD CONSTRAINT legacy_import_rows_matched_goat_tenant_fk FOREIGN KEY (tenant_id, matched_goat_id) REFERENCES goats(tenant_id, goat_id);

ALTER TABLE identity_match_candidates
  ADD CONSTRAINT identity_match_candidates_legacy_row_tenant_fk FOREIGN KEY (tenant_id, legacy_row_id) REFERENCES legacy_import_rows(tenant_id, legacy_row_id),
  ADD CONSTRAINT identity_match_candidates_proposed_goat_tenant_fk FOREIGN KEY (tenant_id, proposed_goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT identity_match_candidates_candidate_goat_tenant_fk FOREIGN KEY (tenant_id, candidate_goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT identity_match_candidates_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE identity_conflicts
  ADD CONSTRAINT identity_conflicts_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE identity_conflict_goats
  ADD CONSTRAINT identity_conflict_goats_conflict_tenant_fk FOREIGN KEY (tenant_id, conflict_id) REFERENCES identity_conflicts(tenant_id, conflict_id) ON DELETE CASCADE,
  ADD CONSTRAINT identity_conflict_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id);

ALTER TABLE identity_conflict_source_records
  ADD CONSTRAINT identity_conflict_source_records_conflict_tenant_fk FOREIGN KEY (tenant_id, conflict_id) REFERENCES identity_conflicts(tenant_id, conflict_id) ON DELETE CASCADE;

ALTER TABLE identity_decision_goats
  ADD CONSTRAINT identity_decision_goats_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id) ON DELETE CASCADE,
  ADD CONSTRAINT identity_decision_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id);

ALTER TABLE identity_decision_identifiers
  ADD CONSTRAINT identity_decision_identifiers_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id) ON DELETE CASCADE,
  ADD CONSTRAINT identity_decision_identifiers_identifier_tenant_fk FOREIGN KEY (tenant_id, identifier_id) REFERENCES goat_identifiers(tenant_id, identifier_id);

ALTER TABLE identity_decision_events
  ADD CONSTRAINT identity_decision_events_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id) ON DELETE CASCADE,
  ADD CONSTRAINT identity_decision_events_event_tenant_fk FOREIGN KEY (tenant_id, event_id, event_recorded_at) REFERENCES goat_identity_events(tenant_id, identity_event_id, recorded_at);

ALTER TABLE identity_decision_media
  ADD CONSTRAINT identity_decision_media_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id) ON DELETE CASCADE;

ALTER TABLE goat_merge_links
  ADD CONSTRAINT goat_merge_links_survivor_tenant_fk FOREIGN KEY (tenant_id, survivor_goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT goat_merge_links_merged_tenant_fk FOREIGN KEY (tenant_id, merged_goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT goat_merge_links_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE identity_correction_requests
  ADD CONSTRAINT identity_correction_requests_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT identity_correction_requests_farm_tenant_fk FOREIGN KEY (tenant_id, farm_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT identity_correction_requests_park_tenant_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT identity_correction_requests_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT identity_correction_requests_cohort_tenant_fk FOREIGN KEY (tenant_id, cohort_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT identity_correction_requests_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE goat_identity_events
  ADD CONSTRAINT goat_identity_events_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  ADD CONSTRAINT goat_identity_events_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

ALTER TABLE goat_identity_counters
  ADD CONSTRAINT goat_identity_counters_farm_tenant_fk FOREIGN KEY (tenant_id, farm_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_identity_counters_park_tenant_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_identity_counters_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_identity_counters_cohort_tenant_fk FOREIGN KEY (tenant_id, cohort_id) REFERENCES locations(tenant_id, location_id),
  ADD CONSTRAINT goat_identity_counters_import_run_tenant_fk FOREIGN KEY (tenant_id, source_import_run_id) REFERENCES legacy_import_runs(tenant_id, import_run_id);

ALTER TABLE audit_log
  ADD CONSTRAINT audit_log_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id);

DROP INDEX goat_identifiers_active_scoped_unique;
CREATE UNIQUE INDEX goat_identifiers_active_scoped_unique
  ON goat_identifiers(tenant_id, identifier_type, normalized_value, scope_key)
  WHERE identifier_type IN ('old_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id')
    AND status = 'active';

ALTER TABLE legacy_import_rows
  DROP CONSTRAINT legacy_import_rows_source_version_unique,
  ADD CONSTRAINT legacy_import_rows_source_version_unique
    UNIQUE (tenant_id, source_system, source_dataset, source_row_key, source_row_version_hash);

CREATE OR REPLACE FUNCTION prevent_goat_hard_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'goat_hard_delete_blocked: goat % must be merged, inactive, or corrected without DELETE', OLD.goat_id
    USING ERRCODE = '23514';
END;
$$;

CREATE TRIGGER goats_prevent_hard_delete_trg
BEFORE DELETE ON goats
FOR EACH ROW
EXECUTE FUNCTION prevent_goat_hard_delete();

CREATE OR REPLACE FUNCTION block_merged_goat_child_write()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  target_state text;
  target_redirect uuid;
BEGIN
  IF COALESCE(current_setting('goatos.allow_merged_goat_child_write', true), 'off') = 'on' THEN
    RETURN NEW;
  END IF;

  IF NEW.goat_id IS NULL THEN
    RETURN NEW;
  END IF;

  SELECT identity_state, merged_into_goat_id
    INTO target_state, target_redirect
    FROM goats
    WHERE tenant_id = NEW.tenant_id
      AND goat_id = NEW.goat_id;

  IF target_state = 'merged' THEN
    RAISE EXCEPTION 'merged_goat_child_write_blocked: goat % redirects to %', NEW.goat_id, target_redirect
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER goat_identifiers_block_merged_goat_child_write_trg
BEFORE INSERT OR UPDATE ON goat_identifiers
FOR EACH ROW
EXECUTE FUNCTION block_merged_goat_child_write();

CREATE TRIGGER goat_ownership_block_merged_goat_child_write_trg
BEFORE INSERT OR UPDATE ON goat_ownership
FOR EACH ROW
EXECUTE FUNCTION block_merged_goat_child_write();

CREATE TRIGGER goat_custody_history_block_merged_goat_child_write_trg
BEFORE INSERT OR UPDATE ON goat_custody_history
FOR EACH ROW
EXECUTE FUNCTION block_merged_goat_child_write();

CREATE TRIGGER goat_location_history_block_merged_goat_child_write_trg
BEFORE INSERT OR UPDATE ON goat_location_history
FOR EACH ROW
EXECUTE FUNCTION block_merged_goat_child_write();

CREATE TRIGGER goat_identity_events_block_merged_goat_child_write_trg
BEFORE INSERT OR UPDATE ON goat_identity_events
FOR EACH ROW
EXECUTE FUNCTION block_merged_goat_child_write();

CREATE TRIGGER identity_correction_requests_block_merged_goat_child_write_trg
BEFORE INSERT OR UPDATE ON identity_correction_requests
FOR EACH ROW
EXECUTE FUNCTION block_merged_goat_child_write();

CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM goat_identity_events
    WHERE tenant_id = NEW.tenant_id
      AND identity_event_id = NEW.event_id
  ) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER outbox_messages_validate_event_tenant_trg
BEFORE INSERT OR UPDATE OF tenant_id, event_id ON outbox_messages
FOR EACH ROW
EXECUTE FUNCTION validate_outbox_event_tenant();

CREATE OR REPLACE FUNCTION validate_user_scope_grant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  location_type_found text;
BEGIN
  IF NEW.scope_type = 'tenant' THEN
    IF NEW.scope_id <> NEW.tenant_id THEN
      RAISE EXCEPTION 'tenant scope grant must use tenant_id as scope_id'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.scope_type = 'custodian_party' THEN
    IF NOT EXISTS (SELECT 1 FROM parties WHERE party_id = NEW.scope_id) THEN
      RAISE EXCEPTION 'custodian party scope % does not exist', NEW.scope_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  SELECT location_type
    INTO location_type_found
    FROM locations
    WHERE tenant_id = NEW.tenant_id
      AND location_id = NEW.scope_id;

  IF location_type_found IS NULL THEN
    RAISE EXCEPTION 'location scope % does not exist for tenant %', NEW.scope_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF location_type_found <> NEW.scope_type THEN
    RAISE EXCEPTION 'scope type % does not match location type % for scope %', NEW.scope_type, location_type_found, NEW.scope_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER user_scope_grants_validate_scope_trg
BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON user_scope_grants
FOR EACH ROW
EXECUTE FUNCTION validate_user_scope_grant();

CREATE OR REPLACE FUNCTION check_goat_active_ownership_total()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  affected_goat_ids uuid[];
  affected_goat_id uuid;
  total_bps int;
BEGIN
  IF TG_OP = 'INSERT' THEN
    affected_goat_ids := ARRAY[NEW.goat_id];
  ELSIF TG_OP = 'DELETE' THEN
    affected_goat_ids := ARRAY[OLD.goat_id];
  ELSE
    affected_goat_ids := ARRAY[OLD.goat_id];
    IF NEW.goat_id IS DISTINCT FROM OLD.goat_id THEN
      affected_goat_ids := affected_goat_ids || NEW.goat_id;
    END IF;
  END IF;

  FOREACH affected_goat_id IN ARRAY affected_goat_ids LOOP
    SELECT COALESCE(sum(share_bps), 0)
      INTO total_bps
      FROM goat_ownership
      WHERE goat_id = affected_goat_id
        AND status = 'active'
        AND valid_to IS NULL;

    IF total_bps <> 10000 THEN
      RAISE EXCEPTION 'active ownership shares for goat % sum to %, expected 10000', affected_goat_id, total_bps
        USING ERRCODE = '23514';
    END IF;
  END LOOP;

  RETURN COALESCE(NEW, OLD);
END;
$$;

-- +goose Down
CREATE OR REPLACE FUNCTION check_goat_active_ownership_total()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  affected_goat_id uuid;
  total_bps int;
BEGIN
  affected_goat_id := COALESCE(NEW.goat_id, OLD.goat_id);

  SELECT COALESCE(sum(share_bps), 0)
    INTO total_bps
    FROM goat_ownership
    WHERE goat_id = affected_goat_id
      AND status = 'active'
      AND valid_to IS NULL;

  IF total_bps <> 10000 THEN
    RAISE EXCEPTION 'active ownership shares for goat % sum to %, expected 10000', affected_goat_id, total_bps
      USING ERRCODE = '23514';
  END IF;

  RETURN COALESCE(NEW, OLD);
END;
$$;

DROP TRIGGER IF EXISTS user_scope_grants_validate_scope_trg ON user_scope_grants;
DROP FUNCTION IF EXISTS validate_user_scope_grant();

DROP TRIGGER IF EXISTS outbox_messages_validate_event_tenant_trg ON outbox_messages;
DROP FUNCTION IF EXISTS validate_outbox_event_tenant();

DROP TRIGGER IF EXISTS identity_correction_requests_block_merged_goat_child_write_trg ON identity_correction_requests;
DROP TRIGGER IF EXISTS goat_identity_events_block_merged_goat_child_write_trg ON goat_identity_events;
DROP TRIGGER IF EXISTS goat_location_history_block_merged_goat_child_write_trg ON goat_location_history;
DROP TRIGGER IF EXISTS goat_custody_history_block_merged_goat_child_write_trg ON goat_custody_history;
DROP TRIGGER IF EXISTS goat_ownership_block_merged_goat_child_write_trg ON goat_ownership;
DROP TRIGGER IF EXISTS goat_identifiers_block_merged_goat_child_write_trg ON goat_identifiers;
DROP FUNCTION IF EXISTS block_merged_goat_child_write();

DROP TRIGGER IF EXISTS goats_prevent_hard_delete_trg ON goats;
DROP FUNCTION IF EXISTS prevent_goat_hard_delete();

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_decision_tenant_fk;

ALTER TABLE goat_identity_counters
  DROP CONSTRAINT IF EXISTS goat_identity_counters_import_run_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_counters_cohort_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_counters_shed_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_counters_park_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_counters_farm_tenant_fk;

ALTER TABLE goat_identity_events
  DROP CONSTRAINT IF EXISTS goat_identity_events_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_identity_events_goat_tenant_fk;

ALTER TABLE identity_correction_requests
  DROP CONSTRAINT IF EXISTS identity_correction_requests_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_correction_requests_cohort_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_correction_requests_shed_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_correction_requests_park_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_correction_requests_farm_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_correction_requests_goat_tenant_fk;

ALTER TABLE goat_merge_links
  DROP CONSTRAINT IF EXISTS goat_merge_links_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_merge_links_merged_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_merge_links_survivor_tenant_fk;

ALTER TABLE identity_decision_media DROP CONSTRAINT IF EXISTS identity_decision_media_decision_tenant_fk;

ALTER TABLE identity_decision_events
  DROP CONSTRAINT IF EXISTS identity_decision_events_event_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_decision_events_decision_tenant_fk;

ALTER TABLE identity_decision_identifiers
  DROP CONSTRAINT IF EXISTS identity_decision_identifiers_identifier_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_decision_identifiers_decision_tenant_fk;

ALTER TABLE identity_decision_goats
  DROP CONSTRAINT IF EXISTS identity_decision_goats_goat_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_decision_goats_decision_tenant_fk;

ALTER TABLE identity_conflict_source_records DROP CONSTRAINT IF EXISTS identity_conflict_source_records_conflict_tenant_fk;

ALTER TABLE identity_conflict_goats
  DROP CONSTRAINT IF EXISTS identity_conflict_goats_goat_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_conflict_goats_conflict_tenant_fk;

ALTER TABLE identity_conflicts DROP CONSTRAINT IF EXISTS identity_conflicts_decision_tenant_fk;

ALTER TABLE identity_match_candidates
  DROP CONSTRAINT IF EXISTS identity_match_candidates_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_match_candidates_candidate_goat_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_match_candidates_proposed_goat_tenant_fk,
  DROP CONSTRAINT IF EXISTS identity_match_candidates_legacy_row_tenant_fk;

ALTER TABLE legacy_import_rows
  DROP CONSTRAINT IF EXISTS legacy_import_rows_matched_goat_tenant_fk,
  DROP CONSTRAINT IF EXISTS legacy_import_rows_run_tenant_fk;

ALTER TABLE goat_custody_history
  DROP CONSTRAINT IF EXISTS goat_custody_history_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_custody_history_to_location_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_custody_history_from_location_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_custody_history_goat_tenant_fk;

ALTER TABLE goat_ownership
  DROP CONSTRAINT IF EXISTS goat_ownership_decision_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_ownership_goat_tenant_fk;

ALTER TABLE goat_location_history
  DROP CONSTRAINT IF EXISTS goat_location_history_to_location_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_location_history_from_location_tenant_fk,
  DROP CONSTRAINT IF EXISTS goat_location_history_goat_tenant_fk;

ALTER TABLE goat_identifiers DROP CONSTRAINT IF EXISTS goat_identifiers_goat_tenant_fk;

ALTER TABLE goats
  DROP CONSTRAINT IF EXISTS goats_merged_into_tenant_fk,
  DROP CONSTRAINT IF EXISTS goats_cohort_tenant_fk,
  DROP CONSTRAINT IF EXISTS goats_shed_tenant_fk,
  DROP CONSTRAINT IF EXISTS goats_park_tenant_fk,
  DROP CONSTRAINT IF EXISTS goats_farm_tenant_fk,
  DROP CONSTRAINT IF EXISTS goats_current_location_tenant_fk;

ALTER TABLE location_aliases DROP CONSTRAINT IF EXISTS location_aliases_canonical_location_tenant_fk;
ALTER TABLE locations DROP CONSTRAINT IF EXISTS locations_parent_location_tenant_fk;

ALTER TABLE legacy_import_rows
  DROP CONSTRAINT IF EXISTS legacy_import_rows_source_version_unique,
  ADD CONSTRAINT legacy_import_rows_source_version_unique UNIQUE (source_row_key, source_row_version_hash);

DROP INDEX IF EXISTS goat_identifiers_active_scoped_unique;
CREATE UNIQUE INDEX goat_identifiers_active_scoped_unique
  ON goat_identifiers(identifier_type, normalized_value, scope_key)
  WHERE identifier_type IN ('old_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id')
    AND status = 'active';

ALTER TABLE goat_identity_events DROP CONSTRAINT IF EXISTS goat_identity_events_tenant_event_recorded_unique;
ALTER TABLE identity_conflicts DROP CONSTRAINT IF EXISTS identity_conflicts_tenant_conflict_unique;
ALTER TABLE legacy_import_rows DROP CONSTRAINT IF EXISTS legacy_import_rows_tenant_row_unique;
ALTER TABLE legacy_import_runs DROP CONSTRAINT IF EXISTS legacy_import_runs_tenant_run_unique;
ALTER TABLE goat_identifiers DROP CONSTRAINT IF EXISTS goat_identifiers_tenant_identifier_unique;
ALTER TABLE identity_decisions DROP CONSTRAINT IF EXISTS identity_decisions_tenant_decision_unique;
ALTER TABLE locations DROP CONSTRAINT IF EXISTS locations_tenant_location_unique;
ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_tenant_goat_unique;
