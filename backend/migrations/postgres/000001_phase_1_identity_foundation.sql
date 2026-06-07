-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SEQUENCE goat_display_id_seq AS bigint START WITH 1 INCREMENT BY 1 NO CYCLE;

CREATE OR REPLACE FUNCTION next_goat_display_id()
RETURNS text
LANGUAGE sql
AS $$
  SELECT 'G-' || lpad(nextval('goat_display_id_seq')::text, 6, '0');
$$;

CREATE TABLE tenants (
  tenant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT tenants_status_check CHECK (status IN ('active', 'inactive'))
);

CREATE TABLE parties (
  party_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  party_type text NOT NULL,
  display_name text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT parties_party_type_check CHECK (party_type IN ('org', 'person', 'token_pool', 'system')),
  CONSTRAINT parties_status_check CHECK (status IN ('active', 'inactive', 'review'))
);

CREATE TABLE orgs (
  party_id uuid PRIMARY KEY REFERENCES parties(party_id),
  org_type text NOT NULL,
  legal_name text NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT orgs_org_type_check CHECK (org_type IN ('mesha', 'farm_operator', 'vendor', 'franchisee', 'lender', 'partner')),
  CONSTRAINT orgs_status_check CHECK (status IN ('active', 'inactive', 'review'))
);

CREATE TABLE breeds (
  breed_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  species text NOT NULL DEFAULT 'goat',
  canonical_name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  review_notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT breeds_status_check CHECK (status IN ('active', 'review', 'inactive')),
  CONSTRAINT breeds_unique_name UNIQUE (species, canonical_name)
);

CREATE TABLE breed_aliases (
  alias_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  breed_id uuid NOT NULL REFERENCES breeds(breed_id),
  alias text NOT NULL,
  normalized_alias text NOT NULL,
  source_system text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT breed_aliases_unique_alias UNIQUE (normalized_alias, source_system)
);

CREATE TABLE status_definitions (
  status_code text PRIMARY KEY,
  axis text NOT NULL,
  display_name text NOT NULL,
  short_label text NOT NULL,
  description text NULL,
  legacy_label text NULL,
  sort_order int NOT NULL DEFAULT 0,
  active boolean NOT NULL DEFAULT true,
  expected_duration_days int NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT status_definitions_axis_check CHECK (axis IN ('lifecycle', 'reproductive', 'growth_cohort', 'management', 'health')),
  CONSTRAINT status_definitions_expected_duration_check CHECK (expected_duration_days IS NULL OR expected_duration_days > 0)
);

CREATE INDEX status_definitions_axis_active_idx ON status_definitions(axis, active, sort_order);

CREATE TABLE legacy_status_mappings (
  mapping_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system text NOT NULL,
  raw_label text NOT NULL,
  normalized_raw_label text NOT NULL,
  lifecycle_status text NULL,
  reproductive_status text NULL,
  growth_cohort_tag text NULL,
  management_stage text NULL,
  health_status text NULL,
  sex_override text NULL,
  display_status_code text NULL REFERENCES status_definitions(status_code),
  confidence text NOT NULL,
  review_required boolean NOT NULL DEFAULT false,
  notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT legacy_status_mappings_confidence_check CHECK (confidence IN ('high', 'medium', 'low')),
  CONSTRAINT legacy_status_mappings_unique_label UNIQUE (source_system, normalized_raw_label)
);

CREATE INDEX legacy_status_mappings_lookup_idx ON legacy_status_mappings(source_system, normalized_raw_label);

CREATE TABLE identifier_policy_versions (
  policy_version text PRIMARY KEY,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  approved_at timestamptz NULL,
  approved_by uuid NULL,
  CONSTRAINT identifier_policy_versions_status_check CHECK (status IN ('draft', 'approved', 'retired'))
);

CREATE TABLE identifier_policies (
  policy_version text NOT NULL,
  identifier_type text NOT NULL,
  default_scope_type text NOT NULL,
  scope_required boolean NOT NULL,
  active_uniqueness text NOT NULL,
  auto_link_allowed boolean NOT NULL,
  primary_allowed boolean NOT NULL,
  unknown_scope_action text NOT NULL,
  missing_or_conflicting_scope_action text NOT NULL,
  normalizer_version text NOT NULL,
  format_validator_version text NULL,
  invalid_value_action text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  approved_by uuid NULL,
  PRIMARY KEY (policy_version, identifier_type),
  CONSTRAINT identifier_policies_version_fk FOREIGN KEY (policy_version) REFERENCES identifier_policy_versions(policy_version),
  CONSTRAINT identifier_policies_identifier_type_check CHECK (identifier_type IN ('old_tag', 'rfid', 'visual_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id')),
  CONSTRAINT identifier_policies_active_uniqueness_check CHECK (active_uniqueness IN ('global', 'scoped', 'non_unique')),
  CONSTRAINT identifier_policies_unknown_scope_action_check CHECK (unknown_scope_action IN ('review', 'reject')),
  CONSTRAINT identifier_policies_missing_scope_action_check CHECK (missing_or_conflicting_scope_action IN ('review', 'reject')),
  CONSTRAINT identifier_policies_invalid_value_action_check CHECK (invalid_value_action IN ('review', 'reject'))
);

CREATE TABLE legacy_import_policies (
  policy_version text PRIMARY KEY,
  source_system text NOT NULL,
  source_dataset text NOT NULL,
  identifier_policy_version text NOT NULL,
  source_key_recipe jsonb NOT NULL,
  source_key_recipe_version text NOT NULL,
  hash_recipe jsonb NOT NULL,
  hash_recipe_version text NOT NULL,
  field_diff_policy jsonb NOT NULL,
  auto_link_policy jsonb NOT NULL,
  normalizer_version text NOT NULL,
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  approved_at timestamptz NULL,
  approved_by uuid NULL,
  CONSTRAINT legacy_import_policies_status_check CHECK (status IN ('draft', 'approved', 'retired')),
  CONSTRAINT legacy_import_policies_identifier_policy_fk FOREIGN KEY (identifier_policy_version) REFERENCES identifier_policy_versions(policy_version)
);

CREATE TABLE locations (
  location_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  location_type text NOT NULL,
  location_code text NULL,
  name text NOT NULL,
  parent_location_id uuid NULL REFERENCES locations(location_id),
  country text NOT NULL DEFAULT 'IN',
  state_region text NULL,
  district text NULL,
  pincode text NULL,
  lat numeric NULL,
  lng numeric NULL,
  timezone text NOT NULL DEFAULT 'Asia/Kolkata',
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT locations_type_check CHECK (location_type IN ('farm', 'park', 'shed', 'cohort', 'pen', 'unknown')),
  CONSTRAINT locations_status_check CHECK (status IN ('active', 'inactive', 'staging', 'review')),
  CONSTRAINT locations_lat_check CHECK (lat IS NULL OR (lat >= -90 AND lat <= 90)),
  CONSTRAINT locations_lng_check CHECK (lng IS NULL OR (lng >= -180 AND lng <= 180)),
  CONSTRAINT locations_unique_code UNIQUE (tenant_id, location_code)
);

CREATE INDEX locations_tenant_type_idx ON locations(tenant_id, location_type, status);
CREATE INDEX locations_parent_idx ON locations(parent_location_id);

CREATE TABLE location_aliases (
  alias_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  alias_code text NOT NULL,
  canonical_location_id uuid NOT NULL REFERENCES locations(location_id),
  source_context text NOT NULL,
  notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT location_aliases_unique_alias UNIQUE (tenant_id, alias_code, source_context)
);

CREATE TABLE identity_decisions (
  decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  decision_type text NOT NULL,
  decision_result text NOT NULL,
  decision_state text NOT NULL,
  decided_by_type text NOT NULL,
  decided_by uuid NULL,
  policy_version text NOT NULL,
  source_record_ids text[] NULL,
  source_identifier_ids uuid[] NULL,
  source_goat_ids uuid[] NULL,
  source_event_ids uuid[] NULL,
  source_media_ids uuid[] NULL,
  model_version text NULL,
  confidence numeric NULL,
  reviewer_id uuid NULL,
  evidence jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  approved_at timestamptz NULL,
  decided_at timestamptz NULL,
  CONSTRAINT identity_decisions_decision_type_check CHECK (decision_type IN ('create_goat', 'attach_identifier', 'retire_identifier', 'mark_identifier_disputed', 'merge_goats', 'batch_merge_goats', 'reject_match', 'request_field_verification')),
  CONSTRAINT identity_decisions_decision_state_check CHECK (decision_state IN ('proposed', 'approved', 'rejected', 'needs_review')),
  CONSTRAINT identity_decisions_decided_by_type_check CHECK (decided_by_type IN ('human', 'system_rule', 'import_policy', 'ai_proposal')),
  CONSTRAINT identity_decisions_confidence_check CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
  CONSTRAINT identity_decisions_ai_not_approved_check CHECK (decided_by_type <> 'ai_proposal' OR decision_state IN ('proposed', 'needs_review'))
);

CREATE INDEX identity_decisions_tenant_type_state_idx ON identity_decisions(tenant_id, decision_type, decision_state, created_at DESC);

CREATE TABLE goats (
  goat_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  display_id text NOT NULL DEFAULT next_goat_display_id(),
  species text NOT NULL DEFAULT 'goat',
  breed text NULL,
  breed_id uuid NULL REFERENCES breeds(breed_id),
  sex text NULL,
  approx_dob date NULL,
  age_band text NULL,
  lifecycle_status text NOT NULL,
  reproductive_status text NULL,
  growth_cohort_tag text NULL,
  management_stage text NULL,
  health_status text NULL,
  identity_state text NOT NULL,
  custodian_party_id uuid NOT NULL REFERENCES parties(party_id),
  current_location_id uuid NULL REFERENCES locations(location_id),
  farm_id uuid NULL REFERENCES locations(location_id),
  park_id uuid NULL REFERENCES locations(location_id),
  shed_id uuid NULL REFERENCES locations(location_id),
  cohort_id uuid NULL REFERENCES locations(location_id),
  merged_into_goat_id uuid NULL REFERENCES goats(goat_id),
  source_confidence numeric NULL,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NULL,
  CONSTRAINT goats_display_id_unique UNIQUE (display_id),
  CONSTRAINT goats_display_id_format_check CHECK (display_id ~ '^G-[0-9]{6,}$'),
  CONSTRAINT goats_species_check CHECK (species = 'goat'),
  CONSTRAINT goats_sex_check CHECK (sex IS NULL OR sex IN ('female', 'male', 'unknown')),
  CONSTRAINT goats_identity_state_check CHECK (identity_state IN ('clean', 'needs_review', 'disputed', 'merged', 'inactive')),
  CONSTRAINT goats_source_confidence_check CHECK (source_confidence IS NULL OR (source_confidence >= 0 AND source_confidence <= 1)),
  CONSTRAINT goats_row_version_check CHECK (row_version >= 1),
  CONSTRAINT goats_merge_redirect_shape_check CHECK (
    (identity_state = 'merged' AND merged_into_goat_id IS NOT NULL AND merged_into_goat_id <> goat_id)
    OR
    (identity_state <> 'merged' AND merged_into_goat_id IS NULL)
  )
);

CREATE INDEX goats_tenant_display_idx ON goats(tenant_id, display_id);
CREATE INDEX goats_tenant_lifecycle_idx ON goats(tenant_id, lifecycle_status);
CREATE INDEX goats_tenant_custodian_lifecycle_idx ON goats(tenant_id, custodian_party_id, lifecycle_status);
CREATE INDEX goats_tenant_reproductive_idx ON goats(tenant_id, reproductive_status);
CREATE INDEX goats_tenant_growth_idx ON goats(tenant_id, growth_cohort_tag);
CREATE INDEX goats_tenant_management_idx ON goats(tenant_id, management_stage);
CREATE INDEX goats_tenant_health_idx ON goats(tenant_id, health_status);
CREATE INDEX goats_current_location_lifecycle_idx ON goats(current_location_id, lifecycle_status);
CREATE INDEX goats_farm_lifecycle_idx ON goats(farm_id, lifecycle_status);
CREATE INDEX goats_park_lifecycle_idx ON goats(park_id, lifecycle_status);
CREATE INDEX goats_shed_lifecycle_idx ON goats(shed_id, lifecycle_status);
CREATE INDEX goats_cohort_lifecycle_idx ON goats(cohort_id, lifecycle_status);
CREATE INDEX goats_breed_sex_lifecycle_idx ON goats(breed_id, sex, lifecycle_status);
CREATE INDEX goats_breed_text_sex_idx ON goats(breed, sex);
CREATE INDEX goats_merged_into_idx ON goats(merged_into_goat_id);

CREATE TABLE goat_identifiers (
  identifier_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  identifier_type text NOT NULL,
  identifier_value text NOT NULL,
  normalized_value text NOT NULL,
  scope_key text NOT NULL,
  is_primary_for_goat boolean NOT NULL DEFAULT false,
  status text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_to timestamptz NULL,
  source_system text NULL,
  source_record_id text NULL,
  normalizer_version text NOT NULL,
  confidence numeric NULL,
  approved_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT goat_identifiers_type_check CHECK (identifier_type IN ('old_tag', 'rfid', 'visual_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id')),
  CONSTRAINT goat_identifiers_status_check CHECK (status IN ('active', 'retired', 'disputed', 'duplicate', 'invalid')),
  CONSTRAINT goat_identifiers_confidence_check CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
  CONSTRAINT goat_identifiers_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from),
  CONSTRAINT goat_identifiers_scope_key_check CHECK (length(scope_key) > 0)
);

CREATE UNIQUE INDEX goat_identifiers_active_rfid_unique
  ON goat_identifiers(normalized_value)
  WHERE identifier_type = 'rfid' AND status = 'active';

CREATE UNIQUE INDEX goat_identifiers_active_scoped_unique
  ON goat_identifiers(identifier_type, normalized_value, scope_key)
  WHERE identifier_type IN ('old_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id')
    AND status = 'active';

CREATE UNIQUE INDEX goat_identifiers_primary_per_goat_unique
  ON goat_identifiers(goat_id, identifier_type)
  WHERE is_primary_for_goat AND status = 'active';

CREATE INDEX goat_identifiers_lookup_idx ON goat_identifiers(tenant_id, identifier_type, normalized_value, scope_key, status);
CREATE INDEX goat_identifiers_goat_status_idx ON goat_identifiers(goat_id, status);
CREATE INDEX goat_identifiers_source_idx ON goat_identifiers(source_system, source_record_id);

CREATE TABLE goat_location_history (
  location_history_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  from_location_id uuid NULL REFERENCES locations(location_id),
  to_location_id uuid NOT NULL REFERENCES locations(location_id),
  reason text NULL,
  occurred_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_id uuid NULL,
  source_record_id text NULL
);

CREATE INDEX goat_location_history_goat_timeline_idx ON goat_location_history(goat_id, occurred_at DESC);
CREATE INDEX goat_location_history_to_location_idx ON goat_location_history(to_location_id, occurred_at DESC);

CREATE TABLE goat_ownership (
  ownership_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  owner_party_id uuid NOT NULL REFERENCES parties(party_id),
  share_bps int NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_to timestamptz NULL,
  status text NOT NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NULL,
  CONSTRAINT goat_ownership_share_bps_check CHECK (share_bps >= 0 AND share_bps <= 10000),
  CONSTRAINT goat_ownership_status_check CHECK (status IN ('active', 'inactive', 'pending_review', 'shared_pending')),
  CONSTRAINT goat_ownership_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE INDEX goat_ownership_goat_active_idx ON goat_ownership(goat_id, status, valid_to);
CREATE INDEX goat_ownership_owner_idx ON goat_ownership(owner_party_id, status);

CREATE TABLE goat_custody_history (
  custody_history_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  custodian_party_id uuid NOT NULL REFERENCES parties(party_id),
  from_location_id uuid NULL REFERENCES locations(location_id),
  to_location_id uuid NULL REFERENCES locations(location_id),
  valid_from timestamptz NOT NULL,
  valid_to timestamptz NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  reason text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NULL,
  CONSTRAINT goat_custody_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE INDEX goat_custody_history_goat_current_idx ON goat_custody_history(goat_id, valid_to);
CREATE INDEX goat_custody_history_custodian_idx ON goat_custody_history(custodian_party_id, valid_to);

CREATE TABLE legacy_import_runs (
  import_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_name text NOT NULL,
  source_system text NOT NULL,
  source_dataset text NOT NULL,
  source_file_ref text NULL,
  source_file_hash text NULL,
  policy_version text NOT NULL REFERENCES legacy_import_policies(policy_version),
  dry_run boolean NOT NULL DEFAULT false,
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  status text NOT NULL,
  row_count int NOT NULL DEFAULT 0,
  created_goat_count int NOT NULL DEFAULT 0,
  updated_goat_count int NOT NULL DEFAULT 0,
  conflict_count int NOT NULL DEFAULT 0,
  error_count int NOT NULL DEFAULT 0,
  started_by uuid NULL,
  CONSTRAINT legacy_import_runs_status_check CHECK (status IN ('staged', 'running', 'completed', 'failed', 'canceled')),
  CONSTRAINT legacy_import_runs_counts_check CHECK (
    row_count >= 0 AND created_goat_count >= 0 AND updated_goat_count >= 0 AND conflict_count >= 0 AND error_count >= 0
  )
);

CREATE INDEX legacy_import_runs_policy_status_idx ON legacy_import_runs(policy_version, status, started_at DESC);

CREATE TABLE legacy_import_rows (
  legacy_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  import_run_id uuid NOT NULL REFERENCES legacy_import_runs(import_run_id),
  row_number int NOT NULL,
  source_system text NOT NULL,
  source_dataset text NOT NULL,
  source_record_id text NULL,
  source_row_key text NOT NULL,
  source_key_recipe_version text NOT NULL,
  source_row_version_hash text NOT NULL,
  hash_recipe_version text NOT NULL,
  raw_payload jsonb NOT NULL,
  normalized_payload jsonb NOT NULL,
  processing_state text NOT NULL,
  matched_goat_id uuid NULL REFERENCES goats(goat_id),
  error_reason text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT legacy_import_rows_row_number_check CHECK (row_number > 0),
  CONSTRAINT legacy_import_rows_processing_state_check CHECK (processing_state IN ('pending', 'auto_linked', 'created_goat', 'needs_review', 'rejected', 'error')),
  CONSTRAINT legacy_import_rows_source_version_unique UNIQUE (source_row_key, source_row_version_hash)
);

CREATE INDEX legacy_import_rows_run_state_idx ON legacy_import_rows(import_run_id, processing_state, row_number);
CREATE INDEX legacy_import_rows_source_key_idx ON legacy_import_rows(source_row_key);
CREATE INDEX legacy_import_rows_matched_goat_idx ON legacy_import_rows(matched_goat_id);

CREATE TABLE identity_match_candidates (
  candidate_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  legacy_row_id uuid NULL REFERENCES legacy_import_rows(legacy_row_id),
  proposed_goat_id uuid NULL REFERENCES goats(goat_id),
  candidate_goat_id uuid NULL REFERENCES goats(goat_id),
  match_score numeric NOT NULL,
  match_reasons jsonb NOT NULL,
  state text NOT NULL,
  created_by text NOT NULL,
  reviewed_by uuid NULL,
  reviewed_at timestamptz NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT identity_match_candidates_score_check CHECK (match_score >= 0 AND match_score <= 1),
  CONSTRAINT identity_match_candidates_state_check CHECK (state IN ('proposed', 'approved', 'rejected', 'needs_review', 'expired')),
  CONSTRAINT identity_match_candidates_created_by_check CHECK (created_by IN ('system_rule', 'human', 'ai_proposal', 'import_policy'))
);

CREATE INDEX identity_match_candidates_queue_idx ON identity_match_candidates(tenant_id, state, created_at DESC);
CREATE INDEX identity_match_candidates_goats_idx ON identity_match_candidates(proposed_goat_id, candidate_goat_id);

CREATE TABLE identity_conflicts (
  conflict_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  conflict_type text NOT NULL,
  severity text NOT NULL,
  state text NOT NULL,
  identifier_type text NULL,
  identifier_value text NULL,
  goat_ids uuid[] NOT NULL DEFAULT '{}',
  source_record_ids text[] NULL,
  evidence jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz NULL,
  resolved_by uuid NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  CONSTRAINT identity_conflicts_type_check CHECK (conflict_type IN ('duplicate_active_identifier', 'missing_required_identifier', 'tagless_goat_review', 'rfid_already_linked', 'old_tag_reused', 'possible_duplicate_goat', 'location_mismatch', 'status_mismatch')),
  CONSTRAINT identity_conflicts_severity_check CHECK (severity IN ('low', 'medium', 'high', 'critical')),
  CONSTRAINT identity_conflicts_state_check CHECK (state IN ('open', 'needs_field_check', 'resolved', 'rejected', 'closed'))
);

CREATE INDEX identity_conflicts_queue_idx ON identity_conflicts(tenant_id, state, severity, created_at DESC);
CREATE INDEX identity_conflicts_identifier_idx ON identity_conflicts(identifier_type, identifier_value);
CREATE INDEX identity_conflicts_decision_idx ON identity_conflicts(decision_id);

CREATE TABLE identity_conflict_goats (
  conflict_id uuid NOT NULL REFERENCES identity_conflicts(conflict_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  role text NOT NULL DEFAULT 'affected',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (conflict_id, goat_id)
);

CREATE INDEX identity_conflict_goats_goat_idx ON identity_conflict_goats(goat_id);

CREATE TABLE identity_conflict_source_records (
  conflict_id uuid NOT NULL REFERENCES identity_conflicts(conflict_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_system text NOT NULL,
  source_record_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (conflict_id, source_system, source_record_id)
);

CREATE TABLE identity_decision_goats (
  decision_id uuid NOT NULL REFERENCES identity_decisions(decision_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  role text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (decision_id, goat_id, role)
);

CREATE INDEX identity_decision_goats_goat_idx ON identity_decision_goats(goat_id);

CREATE TABLE identity_decision_identifiers (
  decision_identifier_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  decision_id uuid NOT NULL REFERENCES identity_decisions(decision_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  identifier_id uuid NULL REFERENCES goat_identifiers(identifier_id),
  identifier_type text NULL,
  identifier_value text NULL,
  action text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT identity_decision_identifiers_action_check CHECK (action IN ('attach', 'retire', 'dispute', 'transfer', 'preserve', 'reject'))
);

CREATE INDEX identity_decision_identifiers_decision_idx ON identity_decision_identifiers(decision_id);
CREATE INDEX identity_decision_identifiers_identifier_idx ON identity_decision_identifiers(identifier_id);

CREATE TABLE identity_decision_events (
  decision_id uuid NOT NULL REFERENCES identity_decisions(decision_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  event_id uuid NOT NULL,
  event_recorded_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (decision_id, event_id, event_recorded_at)
);

CREATE TABLE identity_decision_media (
  decision_id uuid NOT NULL REFERENCES identity_decisions(decision_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  media_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (decision_id, media_id)
);

CREATE TABLE goat_merge_links (
  merge_link_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  survivor_goat_id uuid NOT NULL REFERENCES goats(goat_id),
  merged_goat_id uuid NOT NULL REFERENCES goats(goat_id),
  decision_id uuid NOT NULL REFERENCES identity_decisions(decision_id),
  reason text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid NOT NULL,
  CONSTRAINT goat_merge_links_merged_unique UNIQUE (merged_goat_id),
  CONSTRAINT goat_merge_links_distinct_goats_check CHECK (merged_goat_id <> survivor_goat_id)
);

CREATE INDEX goat_merge_links_survivor_idx ON goat_merge_links(survivor_goat_id);

CREATE TABLE identity_correction_requests (
  correction_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  request_type text NOT NULL,
  state text NOT NULL,
  goat_id uuid NULL REFERENCES goats(goat_id),
  identifier_type text NULL,
  identifier_value text NULL,
  farm_id uuid NULL REFERENCES locations(location_id),
  park_id uuid NULL REFERENCES locations(location_id),
  shed_id uuid NULL REFERENCES locations(location_id),
  cohort_id uuid NULL REFERENCES locations(location_id),
  description text NOT NULL,
  evidence jsonb NOT NULL,
  requested_by uuid NOT NULL,
  assigned_reviewer_id uuid NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz NULL,
  CONSTRAINT identity_correction_requests_type_check CHECK (request_type IN ('missing_tag', 'tag_reused', 'rfid_conflict', 'possible_duplicate', 'wrong_location', 'wrong_status', 'field_verification_result', 'identifier_seen_but_not_attached')),
  CONSTRAINT identity_correction_requests_state_check CHECK (state IN ('open', 'assigned', 'needs_field_check', 'approved', 'rejected', 'closed'))
);

CREATE INDEX identity_correction_requests_queue_idx ON identity_correction_requests(tenant_id, state, created_at DESC);
CREATE INDEX identity_correction_requests_goat_idx ON identity_correction_requests(goat_id, state);
CREATE INDEX identity_correction_requests_identifier_idx ON identity_correction_requests(identifier_type, identifier_value);

CREATE TABLE idempotency_keys (
  idempotency_key text PRIMARY KEY,
  tenant_id uuid NULL REFERENCES tenants(tenant_id),
  scope text NOT NULL,
  request_hash text NOT NULL,
  status text NOT NULL,
  result_type text NULL,
  result_id uuid NULL,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  expires_at timestamptz NULL,
  CONSTRAINT idempotency_keys_status_check CHECK (status IN ('started', 'completed', 'failed')),
  CONSTRAINT idempotency_keys_completed_shape_check CHECK ((status <> 'completed') OR completed_at IS NOT NULL)
);

CREATE INDEX idempotency_keys_expires_idx ON idempotency_keys(expires_at);
CREATE INDEX idempotency_keys_scope_idx ON idempotency_keys(scope, first_seen_at DESC);

CREATE TABLE goat_identity_events (
  identity_event_id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL REFERENCES goats(goat_id),
  event_type text NOT NULL,
  event_version int NOT NULL,
  occurred_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_id uuid NULL,
  source_system text NULL,
  source_record_id text NULL,
  payload jsonb NOT NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  idempotency_key text NOT NULL,
  PRIMARY KEY (identity_event_id, recorded_at),
  CONSTRAINT goat_identity_events_version_check CHECK (event_version > 0)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE goat_identity_events_2026_06 PARTITION OF goat_identity_events
  FOR VALUES FROM ('2026-06-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
CREATE TABLE goat_identity_events_2026_07 PARTITION OF goat_identity_events
  FOR VALUES FROM ('2026-07-01 00:00:00+00') TO ('2026-08-01 00:00:00+00');
CREATE TABLE goat_identity_events_2026_08 PARTITION OF goat_identity_events
  FOR VALUES FROM ('2026-08-01 00:00:00+00') TO ('2026-09-01 00:00:00+00');
CREATE TABLE goat_identity_events_2026_09 PARTITION OF goat_identity_events
  FOR VALUES FROM ('2026-09-01 00:00:00+00') TO ('2026-10-01 00:00:00+00');
CREATE TABLE goat_identity_events_default PARTITION OF goat_identity_events DEFAULT;

CREATE INDEX goat_identity_events_goat_timeline_idx ON goat_identity_events(goat_id, occurred_at DESC);
CREATE INDEX goat_identity_events_tenant_type_recorded_idx ON goat_identity_events(tenant_id, event_type, recorded_at DESC);
CREATE INDEX goat_identity_events_idempotency_idx ON goat_identity_events(idempotency_key);

ALTER TABLE identity_decision_events
  ADD CONSTRAINT identity_decision_events_event_fk
  FOREIGN KEY (event_id, event_recorded_at) REFERENCES goat_identity_events(identity_event_id, recorded_at);

CREATE TABLE outbox_messages (
  outbox_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  event_id uuid NOT NULL,
  event_type text NOT NULL,
  schema_version text NOT NULL,
  aggregate_type text NOT NULL,
  aggregate_id uuid NOT NULL,
  topic text NOT NULL,
  payload jsonb NOT NULL,
  headers jsonb NOT NULL,
  idempotency_key text NOT NULL,
  trace_id text NULL,
  status text NOT NULL,
  attempt_count int NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NULL,
  last_error text NULL,
  published_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT outbox_messages_event_unique UNIQUE (event_id),
  CONSTRAINT outbox_messages_status_check CHECK (status IN ('pending', 'publishing', 'published', 'failed', 'dead_letter')),
  CONSTRAINT outbox_messages_attempt_count_check CHECK (attempt_count >= 0)
);

CREATE INDEX outbox_messages_status_next_attempt_idx ON outbox_messages(status, next_attempt_at, created_at);
CREATE INDEX outbox_messages_aggregate_idx ON outbox_messages(aggregate_type, aggregate_id, created_at);
CREATE INDEX outbox_messages_created_at_idx ON outbox_messages(created_at);

CREATE TABLE goat_identity_counters (
  counter_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  counter_grain text NOT NULL,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  custodian_party_id uuid NULL REFERENCES parties(party_id),
  farm_id uuid NULL REFERENCES locations(location_id),
  park_id uuid NULL REFERENCES locations(location_id),
  shed_id uuid NULL REFERENCES locations(location_id),
  cohort_id uuid NULL REFERENCES locations(location_id),
  lifecycle_status text NULL,
  reproductive_status text NULL,
  growth_cohort_tag text NULL,
  management_stage text NULL,
  health_status text NULL,
  identity_state text NULL,
  breed_id uuid NULL REFERENCES breeds(breed_id),
  sex text NULL,
  count_value bigint NOT NULL,
  as_of_recorded_at timestamptz NULL,
  source_import_run_id uuid NULL REFERENCES legacy_import_runs(import_run_id),
  is_rebuilding boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT goat_identity_counters_grain_check CHECK (counter_grain IN ('tenant_lifecycle', 'custodian_lifecycle', 'custodian_identity', 'park_lifecycle', 'shed_lifecycle', 'breed_sex_lifecycle', 'health_status', 'growth_cohort', 'management_stage', 'reproductive_status')),
  CONSTRAINT goat_identity_counters_count_value_check CHECK (count_value >= 0),
  CONSTRAINT goat_identity_counters_identity_state_check CHECK (identity_state IS NULL OR identity_state IN ('clean', 'needs_review', 'disputed', 'merged', 'inactive'))
);

CREATE UNIQUE INDEX goat_identity_counters_grain_unique
  ON goat_identity_counters(
    counter_grain,
    tenant_id,
    custodian_party_id,
    farm_id,
    park_id,
    shed_id,
    cohort_id,
    lifecycle_status,
    reproductive_status,
    growth_cohort_tag,
    management_stage,
    health_status,
    identity_state,
    breed_id,
    sex
  ) NULLS NOT DISTINCT;

CREATE INDEX goat_identity_counters_lookup_idx ON goat_identity_counters(counter_grain, tenant_id, updated_at DESC);
CREATE INDEX goat_identity_counters_rebuild_idx ON goat_identity_counters(is_rebuilding, updated_at DESC);

CREATE TABLE audit_log (
  audit_id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NULL REFERENCES tenants(tenant_id),
  actor_id uuid NULL,
  actor_type text NOT NULL,
  action text NOT NULL,
  resource_type text NOT NULL,
  resource_id uuid NULL,
  scope_type text NULL,
  scope_id uuid NULL,
  decision_id uuid NULL REFERENCES identity_decisions(decision_id),
  before_state jsonb NULL,
  after_state jsonb NULL,
  metadata jsonb NOT NULL,
  trace_id text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  recorded_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (audit_id, recorded_at)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE audit_log_2026_06 PARTITION OF audit_log
  FOR VALUES FROM ('2026-06-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
CREATE TABLE audit_log_2026_07 PARTITION OF audit_log
  FOR VALUES FROM ('2026-07-01 00:00:00+00') TO ('2026-08-01 00:00:00+00');
CREATE TABLE audit_log_2026_08 PARTITION OF audit_log
  FOR VALUES FROM ('2026-08-01 00:00:00+00') TO ('2026-09-01 00:00:00+00');
CREATE TABLE audit_log_2026_09 PARTITION OF audit_log
  FOR VALUES FROM ('2026-09-01 00:00:00+00') TO ('2026-10-01 00:00:00+00');
CREATE TABLE audit_log_default PARTITION OF audit_log DEFAULT;

CREATE INDEX audit_log_resource_idx ON audit_log(resource_type, resource_id, created_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log(actor_id, created_at DESC);
CREATE INDEX audit_log_tenant_action_idx ON audit_log(tenant_id, action, recorded_at DESC);

CREATE TABLE user_scope_grants (
  grant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  user_id uuid NOT NULL,
  role text NOT NULL,
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  status text NOT NULL,
  valid_from timestamptz NOT NULL,
  valid_to timestamptz NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_scope_grants_role_check CHECK (role IN ('admin', 'park_head', 'operator', 'verifier', 'ceo_internal')),
  CONSTRAINT user_scope_grants_scope_type_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT user_scope_grants_status_check CHECK (status IN ('active', 'inactive', 'revoked')),
  CONSTRAINT user_scope_grants_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE INDEX user_scope_grants_user_active_idx ON user_scope_grants(user_id, status, valid_from, valid_to);
CREATE INDEX user_scope_grants_scope_idx ON user_scope_grants(tenant_id, scope_type, scope_id, role, status);

CREATE OR REPLACE FUNCTION prevent_merged_goat_normal_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'UPDATE'
    AND OLD.identity_state = 'merged'
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

CREATE TRIGGER goats_prevent_merged_write_trg
BEFORE UPDATE ON goats
FOR EACH ROW
EXECUTE FUNCTION prevent_merged_goat_normal_update();

CREATE OR REPLACE FUNCTION validate_goat_merge_link()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  survivor_state text;
  survivor_redirect uuid;
  merged_state text;
  merged_redirect uuid;
BEGIN
  SELECT identity_state, merged_into_goat_id
    INTO survivor_state, survivor_redirect
    FROM goats
    WHERE goat_id = NEW.survivor_goat_id;

  IF survivor_state IS NULL THEN
    RAISE EXCEPTION 'survivor goat % does not exist', NEW.survivor_goat_id
      USING ERRCODE = '23503';
  END IF;

  IF survivor_state = 'merged' OR survivor_redirect IS NOT NULL THEN
    RAISE EXCEPTION 'survivor goat % must be live, not merged', NEW.survivor_goat_id
      USING ERRCODE = '23514';
  END IF;

  SELECT identity_state, merged_into_goat_id
    INTO merged_state, merged_redirect
    FROM goats
    WHERE goat_id = NEW.merged_goat_id;

  IF merged_state IS NULL THEN
    RAISE EXCEPTION 'merged goat % does not exist', NEW.merged_goat_id
      USING ERRCODE = '23503';
  END IF;

  IF merged_state <> 'merged' OR merged_redirect IS DISTINCT FROM NEW.survivor_goat_id THEN
    RAISE EXCEPTION 'merged goat % must redirect to survivor % before link insert', NEW.merged_goat_id, NEW.survivor_goat_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER goat_merge_links_validate_trg
BEFORE INSERT OR UPDATE ON goat_merge_links
FOR EACH ROW
EXECUTE FUNCTION validate_goat_merge_link();

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

CREATE CONSTRAINT TRIGGER goat_ownership_active_share_total_trg
AFTER INSERT OR UPDATE OR DELETE ON goat_ownership
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_goat_active_ownership_total();

INSERT INTO tenants (tenant_id, name, status, created_at, updated_at) VALUES
  ('00000000-0000-4000-8000-000000000001', 'Mesha', 'active', now(), now());

INSERT INTO parties (party_id, party_type, display_name, status, created_at, updated_at) VALUES
  ('00000000-0000-4000-8000-000000001001', 'org', 'Mesha', 'active', now(), now()),
  ('00000000-0000-4000-8000-000000001101', 'org', 'Rajasthan Farms', 'review', now(), now()),
  ('00000000-0000-4000-8000-000000001102', 'org', 'Gokul Agronomics', 'review', now(), now()),
  ('00000000-0000-4000-8000-000000001103', 'org', 'Goat World', 'review', now(), now()),
  ('00000000-0000-4000-8000-000000001104', 'org', 'Bhopal Agro', 'review', now(), now());

INSERT INTO orgs (party_id, org_type, legal_name, status, created_at, updated_at) VALUES
  ('00000000-0000-4000-8000-000000001001', 'mesha', 'Mesha', 'active', now(), now()),
  ('00000000-0000-4000-8000-000000001101', 'vendor', 'Rajasthan Farms', 'review', now(), now()),
  ('00000000-0000-4000-8000-000000001102', 'vendor', 'Gokul Agronomics', 'review', now(), now()),
  ('00000000-0000-4000-8000-000000001103', 'vendor', 'Goat World', 'review', now(), now()),
  ('00000000-0000-4000-8000-000000001104', 'vendor', 'Bhopal Agro', 'review', now(), now());

INSERT INTO breeds (breed_id, species, canonical_name, status, review_notes, created_at, updated_at) VALUES
  ('00000000-0000-4000-8000-000000002001', 'goat', 'Malai', 'active', NULL, now(), now()),
  ('00000000-0000-4000-8000-000000002002', 'goat', 'Beetal', 'active', NULL, now(), now()),
  ('00000000-0000-4000-8000-000000002003', 'goat', 'Sojat', 'active', NULL, now(), now()),
  ('00000000-0000-4000-8000-000000002004', 'goat', 'Osmanabadi', 'active', NULL, now(), now()),
  ('00000000-0000-4000-8000-000000002005', 'goat', 'Boer', 'active', NULL, now(), now()),
  ('00000000-0000-4000-8000-000000002006', 'sheep', 'Anantapur Sheep', 'review', 'Source label is species/breed reference, not Goat Passport default species.', now(), now()),
  ('00000000-0000-4000-8000-000000002007', 'goat', 'Anantapur', 'review', 'Legacy dashboard constant; keep reviewable alias/reference row.', now(), now()),
  ('00000000-0000-4000-8000-000000002008', 'goat', 'Kenguri', 'review', 'Legacy dashboard constant; keep reviewable alias/reference row.', now(), now());

INSERT INTO breed_aliases (breed_id, alias, normalized_alias, source_system, created_at)
SELECT breed_id, canonical_name, lower(regexp_replace(canonical_name, '\s+', '_', 'g')), 'phase1_seed', now()
FROM breeds;

INSERT INTO status_definitions (status_code, axis, display_name, short_label, description, legacy_label, sort_order, active, expected_duration_days, created_at, updated_at) VALUES
  ('alive', 'lifecycle', 'Alive', 'Alive', 'Canonical active lifecycle state.', NULL, 10, true, NULL, now(), now()),
  ('dead', 'lifecycle', 'Dead', 'Dead', 'Deceased lifecycle state.', NULL, 90, true, NULL, now(), now()),
  ('sold', 'lifecycle', 'Sold', 'Sold', 'Exited through sale; future sales workflow owns behavior.', NULL, 100, true, NULL, now(), now()),
  ('merged', 'lifecycle', 'Merged', 'Merged', 'Redirected duplicate identity.', NULL, 110, true, NULL, now(), now()),
  ('inactive', 'lifecycle', 'Inactive', 'Inactive', 'Inactive identity state.', NULL, 120, true, NULL, now(), now()),
  ('K0', 'growth_cohort', 'K0 - Newborn', 'K0', 'Newborn kids with mother.', 'K0', 10, true, 1, now(), now()),
  ('K1', 'growth_cohort', 'K1 - Bottle milk training', 'K1', 'Bottle milk training.', 'K1', 20, true, 7, now(), now()),
  ('K2', 'growth_cohort', 'K2 - Milk drinking', 'K2', 'Milk drinking after training.', 'K2', 30, true, 42, now(), now()),
  ('K3', 'growth_cohort', 'K3 - Weaning', 'K3', 'Weaning to solid feed.', 'K3', 40, true, NULL, now(), now()),
  ('F2', 'growth_cohort', 'F2 - Fattening', 'F2', 'Post-weaning fattening stage.', 'F2', 50, true, NULL, now(), now()),
  ('warmup', 'management', 'Warmup - Adaptation', 'Warmup', 'Source holding or park transition adaptation.', 'Warmup', 10, true, 14, now(), now()),
  ('m0_post_delivery', 'management', 'M0 - Post-delivery mother', 'M0', 'Mother post-delivery management stage.', 'M0', 20, true, NULL, now(), now()),
  ('pregnant', 'reproductive', 'Pregnant', 'Pregnant', NULL, 'Pregnant', 10, true, NULL, now(), now()),
  ('non_pregnant', 'reproductive', 'Non-pregnant', 'Non-pregnant', NULL, 'Non-Pregnant', 20, true, NULL, now(), now()),
  ('mother', 'reproductive', 'Mother', 'Mother', NULL, 'Mother', 30, true, NULL, now(), now()),
  ('milking', 'reproductive', 'Milking', 'Milking', NULL, 'Milking', 40, true, NULL, now(), now()),
  ('buck', 'reproductive', 'Buck', 'Buck', 'Adult male breeding role.', 'Buck', 50, true, NULL, now(), now()),
  ('healthy', 'health', 'Healthy', 'Healthy', NULL, NULL, 10, true, NULL, now(), now()),
  ('icu', 'health', 'ICU', 'ICU', 'Serious illness health status; Phase 1 stores only.', 'ICU', 90, true, NULL, now(), now()),
  ('quarantine', 'health', 'Quarantine', 'Quarantine', 'Quarantine health status; Phase 1 stores only.', 'Quarantine', 95, true, NULL, now(), now()),
  ('under_treatment', 'health', 'Under treatment', 'Treatment', 'Treatment status; Phase 1 stores only.', NULL, 80, true, NULL, now(), now());

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
  ('legacy_rfid_db', 'Fattening', 'fattening', 'alive', NULL, 'F2', NULL, NULL, NULL, 'F2', 'high', false, 'Maps to F2 growth cohort only.', now(), now()),
  ('legacy_rfid_db', 'F2-Male', 'f2-male', 'alive', NULL, 'F2', NULL, NULL, NULL, 'F2', 'medium', true, 'F2 suffix is contaminated sex evidence; source Gender remains sex truth.', now(), now()),
  ('legacy_rfid_db', 'F2-Female', 'f2-female', 'alive', NULL, 'F2', NULL, NULL, NULL, 'F2', 'medium', true, 'F2 suffix is contaminated sex evidence; source Gender remains sex truth.', now(), now()),
  ('legacy_rfid_db', 'ICU-Non-Pregnant', 'icu-non-pregnant', 'alive', 'non_pregnant', NULL, NULL, 'icu', NULL, 'icu', 'high', false, 'Compound label split into health and reproductive axes.', now(), now()),
  ('legacy_rfid_db', 'M0', 'm0', 'alive', 'mother', NULL, 'm0_post_delivery', NULL, NULL, 'm0_post_delivery', 'high', false, 'M0 is mother/post-delivery management, not growth cohort.', now(), now()),
  ('legacy_rfid_db', 'Warmup', 'warmup', 'alive', NULL, NULL, 'warmup', NULL, NULL, 'warmup', 'high', false, 'Warmup is a management stage.', now(), now());

INSERT INTO identifier_policy_versions (policy_version, status, created_at, approved_at, approved_by) VALUES
  ('phase1-identifier-v1', 'approved', now(), now(), NULL);

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
  ('phase1-identifier-v1', 'rfid', 'global', true, 'global', true, true, 'review', 'review', 'identifier_normalizer_v1', 'rfid_v1', 'review', now(), NULL),
  ('phase1-identifier-v1', 'old_tag', 'park', true, 'scoped', true, true, 'review', 'review', 'identifier_normalizer_v1', NULL, 'review', now(), NULL),
  ('phase1-identifier-v1', 'visual_tag', 'unknown', false, 'non_unique', false, false, 'review', 'review', 'identifier_normalizer_v1', NULL, 'review', now(), NULL),
  ('phase1-identifier-v1', 'sheet_row_id', 'source_system', true, 'scoped', false, false, 'review', 'review', 'identifier_normalizer_v1', NULL, 'review', now(), NULL),
  ('phase1-identifier-v1', 'purchase_load_id', 'purchase_load', true, 'scoped', false, false, 'review', 'review', 'identifier_normalizer_v1', NULL, 'review', now(), NULL),
  ('phase1-identifier-v1', 'temp_field_id', 'global', true, 'scoped', false, true, 'review', 'review', 'identifier_normalizer_v1', NULL, 'review', now(), NULL),
  ('phase1-identifier-v1', 'external_system_id', 'source_system', true, 'scoped', false, false, 'review', 'review', 'identifier_normalizer_v1', NULL, 'review', now(), NULL);

INSERT INTO legacy_import_policies (
  policy_version,
  source_system,
  source_dataset,
  identifier_policy_version,
  source_key_recipe,
  source_key_recipe_version,
  hash_recipe,
  hash_recipe_version,
  field_diff_policy,
  auto_link_policy,
  normalizer_version,
  status,
  created_at,
  approved_at,
  approved_by
) VALUES (
  'phase1-rfid-db-import-v1',
  'legacy_rfid_db',
  'rfid_db_first_import',
  'phase1-identifier-v1',
  '{"strategy":"stable_identity_projection","fields":["source_system","source_dataset","normalized_old_tag","normalized_park_code","rfid"],"forbidden_fields":["row_number","sorted_position","export_line_number"]}'::jsonb,
  'source_key_recipe_v1',
  '{"strategy":"normalized_meaningful_fields","include_fields":["farm","origin_farm","old_tag_id","rfid","breed","gender","shed","shed_tag","age"],"exclude_fields":["exported_at","formatting","row_number","formula_timestamp"]}'::jsonb,
  'hash_recipe_v1',
  '{"identifier_change":"review","breed_change":"review","sex_change":"review","status_axis_change":"review","shed_change":"review","source_missing":"ignore"}'::jsonb,
  '{"rfid":"auto_link_if_unique_and_valid","old_tag":"auto_link_only_with_normalized_park_scope","visual_tag":"review","tagless":"later_import_pass"}'::jsonb,
  'legacy_rfid_normalizer_v1',
  'approved',
  now(),
  now(),
  NULL
);

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, country, state_region, district, pincode, lat, lng, timezone, status, created_at) VALUES
  ('00000000-0000-4000-8000-000000003000', '00000000-0000-4000-8000-000000000001', 'unknown', 'UNKNOWN', 'Unknown staging location', NULL, 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'staging', now()),
  ('00000000-0000-4000-8000-000000003001', '00000000-0000-4000-8000-000000000001', 'park', 'CBE', 'Coimbatore', NULL, 'IN', 'Tamil Nadu', NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', now()),
  ('00000000-0000-4000-8000-000000003002', '00000000-0000-4000-8000-000000000001', 'park', 'CPT', 'Channapatna', NULL, 'IN', 'Karnataka', NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', now()),
  ('00000000-0000-4000-8000-000000003003', '00000000-0000-4000-8000-000000000001', 'unknown', 'HF_UNKNOWN', 'Unknown holding farm/source location', NULL, 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'review', now());

INSERT INTO location_aliases (tenant_id, alias_code, canonical_location_id, source_context, notes, created_at) VALUES
  ('00000000-0000-4000-8000-000000000001', 'CBE', '00000000-0000-4000-8000-000000003001', 'legacy_location_code', 'Canonical Coimbatore code.', now()),
  ('00000000-0000-4000-8000-000000000001', 'CJB', '00000000-0000-4000-8000-000000003001', 'legacy_location_code', 'Historic alias for CBE; preserve original source code as evidence.', now()),
  ('00000000-0000-4000-8000-000000000001', 'CPT', '00000000-0000-4000-8000-000000003002', 'legacy_location_code', 'Canonical Channapatna code.', now()),
  ('00000000-0000-4000-8000-000000000001', 'BLR', '00000000-0000-4000-8000-000000003002', 'legacy_location_code', 'Historic alias for CPT; preserve original source code as evidence.', now()),
  ('00000000-0000-4000-8000-000000000001', 'HF', '00000000-0000-4000-8000-000000003003', 'legacy_location_code', 'Holding Farm source/holding context; not ownership truth by itself.', now()),
  ('00000000-0000-4000-8000-000000000001', 'Holding Farm', '00000000-0000-4000-8000-000000003003', 'legacy_location_code', 'Holding Farm source/holding context; not ownership truth by itself.', now());

-- +goose Down
DROP TRIGGER IF EXISTS goat_ownership_active_share_total_trg ON goat_ownership;
DROP FUNCTION IF EXISTS check_goat_active_ownership_total();
DROP TRIGGER IF EXISTS goat_merge_links_validate_trg ON goat_merge_links;
DROP FUNCTION IF EXISTS validate_goat_merge_link();
DROP TRIGGER IF EXISTS goats_prevent_merged_write_trg ON goats;
DROP FUNCTION IF EXISTS prevent_merged_goat_normal_update();

DROP TABLE IF EXISTS user_scope_grants;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS goat_identity_counters;
DROP TABLE IF EXISTS outbox_messages;
DROP TABLE IF EXISTS identity_decision_events;
DROP TABLE IF EXISTS goat_identity_events;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS identity_correction_requests;
DROP TABLE IF EXISTS goat_merge_links;
DROP TABLE IF EXISTS identity_decision_media;
DROP TABLE IF EXISTS identity_decision_identifiers;
DROP TABLE IF EXISTS identity_decision_goats;
DROP TABLE IF EXISTS identity_conflict_source_records;
DROP TABLE IF EXISTS identity_conflict_goats;
DROP TABLE IF EXISTS identity_conflicts;
DROP TABLE IF EXISTS identity_match_candidates;
DROP TABLE IF EXISTS legacy_import_rows;
DROP TABLE IF EXISTS legacy_import_runs;
DROP TABLE IF EXISTS goat_custody_history;
DROP TABLE IF EXISTS goat_ownership;
DROP TABLE IF EXISTS goat_location_history;
DROP TABLE IF EXISTS goat_identifiers;
DROP TABLE IF EXISTS goats;
DROP TABLE IF EXISTS identity_decisions;
DROP TABLE IF EXISTS location_aliases;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS legacy_import_policies;
DROP TABLE IF EXISTS identifier_policies;
DROP TABLE IF EXISTS identifier_policy_versions;
DROP TABLE IF EXISTS legacy_status_mappings;
DROP TABLE IF EXISTS status_definitions;
DROP TABLE IF EXISTS breed_aliases;
DROP TABLE IF EXISTS breeds;
DROP TABLE IF EXISTS orgs;
DROP TABLE IF EXISTS parties;
DROP TABLE IF EXISTS tenants;
DROP FUNCTION IF EXISTS next_goat_display_id();
DROP SEQUENCE IF EXISTS goat_display_id_seq;
