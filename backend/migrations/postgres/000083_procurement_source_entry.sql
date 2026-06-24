-- +goose Up
-- Procurement/source-entry truth for goats whose journey starts at purchase/source holding.
-- The accepted herd intake transition is the only path that makes a procured/source goat
-- available for post-arrival PHC vaccination generation.

CREATE TABLE procurement_loads (
  load_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_party_id uuid NOT NULL REFERENCES parties(party_id),
  source_location_id uuid NULL REFERENCES locations(location_id),
  expected_count int NOT NULL DEFAULT 0,
  purchase_date date NULL,
  planned_dispatch_at timestamptz NULL,
  status text NOT NULL DEFAULT 'source_warmup',
  notes text NOT NULL DEFAULT '',
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_key text NOT NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT procurement_loads_expected_count_check CHECK (expected_count >= 0),
  CONSTRAINT procurement_loads_status_check CHECK (status IN (
    'source_warmup', 'health_pending', 'pre_dispatch_pending', 'dispatch_ready',
    'in_transit', 'arrival_review', 'accepted_intake', 'rejected', 'deferred',
    'blocked', 'canceled'
  )),
  CONSTRAINT procurement_loads_context_object_check CHECK (jsonb_typeof(context) = 'object'),
  CONSTRAINT procurement_loads_row_version_check CHECK (row_version >= 1),
  CONSTRAINT procurement_loads_tenant_id_unique UNIQUE (tenant_id, load_id),
  CONSTRAINT procurement_loads_idempotency_unique UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT procurement_loads_source_location_tenant_fk FOREIGN KEY (tenant_id, source_location_id) REFERENCES locations(tenant_id, location_id)
);

CREATE INDEX procurement_loads_board_idx
  ON procurement_loads(tenant_id, status, updated_at DESC, load_id DESC);
CREATE INDEX procurement_loads_page_idx
  ON procurement_loads(tenant_id, updated_at DESC, load_id DESC);
CREATE INDEX procurement_loads_source_idx
  ON procurement_loads(tenant_id, source_party_id, purchase_date DESC, load_id DESC);

CREATE TABLE procurement_load_goats (
  load_goat_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  load_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  source_tag text NULL,
  source_rfid text NULL,
  temporary_id text NULL,
  selection_state text NOT NULL DEFAULT 'candidate',
  selection_reason text NOT NULL DEFAULT '',
  current_state text NOT NULL DEFAULT 'source_candidate',
  identity_review_state text NOT NULL DEFAULT 'pending',
  identity_review_ref text NULL,
  ownership_state text NOT NULL DEFAULT 'pending',
  health_state text NOT NULL DEFAULT 'pending',
  warmup_started_at timestamptz NULL,
  warmup_ended_at timestamptz NULL,
  warmup_days int NULL,
  holding_location_id uuid NULL,
  loaded_at timestamptz NULL,
  arrived_at timestamptz NULL,
  intake_accepted_at timestamptz NULL,
  exit_reason text NULL,
  proof_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT procurement_load_goats_selection_state_check CHECK (selection_state IN (
    'source_only', 'candidate', 'purchased', 'accepted', 'rejected', 'deferred',
    'blocked', 'loaded', 'arrival_accepted', 'arrival_rejected',
    'accepted_herd_intake', 'dead', 'sold', 'lost'
  )),
  CONSTRAINT procurement_load_goats_current_state_check CHECK (current_state IN (
    'source_holding', 'source_warmup', 'source_candidate', 'source_health_pending',
    'source_health_passed', 'source_health_failed', 'source_rejected',
    'pre_dispatch_pending', 'pre_dispatch_accepted', 'pre_dispatch_rejected',
    'pre_dispatch_deferred', 'pre_dispatch_blocked', 'dispatch_ready',
    'loading_pending', 'loaded', 'in_transit', 'arrival_review_pending',
    'arrival_accepted', 'arrival_rejected', 'accepted_herd_intake',
    'dead', 'sold', 'lost', 'canceled'
  )),
  CONSTRAINT procurement_load_goats_identity_state_check CHECK (identity_review_state IN ('pending', 'clean', 'conflict', 'unknown_extra')),
  CONSTRAINT procurement_load_goats_ownership_state_check CHECK (ownership_state IN ('pending', 'shared_pending', 'mesha_owned', 'blocked', 'not_owned', 'settled')),
  CONSTRAINT procurement_load_goats_health_state_check CHECK (health_state IN ('pending', 'passed', 'failed', 'deferred')),
  CONSTRAINT procurement_load_goats_exit_reason_check CHECK (exit_reason IS NULL OR exit_reason IN ('died', 'sold', 'lost', 'canceled')),
  CONSTRAINT procurement_load_goats_warmup_days_check CHECK (warmup_days IS NULL OR warmup_days >= 0),
  CONSTRAINT procurement_load_goats_warmup_window_check CHECK (warmup_ended_at IS NULL OR warmup_started_at IS NULL OR warmup_ended_at >= warmup_started_at),
  CONSTRAINT procurement_load_goats_proof_refs_array_check CHECK (jsonb_typeof(proof_refs) = 'array'),
  CONSTRAINT procurement_load_goats_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT procurement_load_goats_row_version_check CHECK (row_version >= 1),
  CONSTRAINT procurement_load_goats_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT procurement_load_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT procurement_load_goats_holding_location_tenant_fk FOREIGN KEY (tenant_id, holding_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT procurement_load_goats_unique_goat_per_load UNIQUE (tenant_id, load_id, goat_id)
);

CREATE INDEX procurement_load_goats_load_state_idx
  ON procurement_load_goats(tenant_id, load_id, current_state, goat_id);
CREATE INDEX procurement_load_goats_load_detail_idx
  ON procurement_load_goats(tenant_id, load_id, created_at, goat_id);
CREATE INDEX procurement_load_goats_goat_state_idx
  ON procurement_load_goats(tenant_id, goat_id, current_state, updated_at DESC);
CREATE INDEX procurement_load_goats_action_idx
  ON procurement_load_goats(tenant_id, current_state, ownership_state, identity_review_state, updated_at DESC, goat_id);
CREATE INDEX procurement_load_goats_work_idx
  ON procurement_load_goats(tenant_id, updated_at DESC, load_goat_id DESC);
CREATE INDEX procurement_load_goats_intake_eligibility_idx
  ON procurement_load_goats(tenant_id, load_id, current_state, health_state, identity_review_state, ownership_state, goat_id)
  WHERE loaded_at IS NOT NULL AND arrived_at IS NOT NULL;
CREATE INDEX procurement_load_goats_source_tag_idx
  ON procurement_load_goats(tenant_id, lower(source_tag), load_id)
  WHERE source_tag IS NOT NULL;
CREATE INDEX procurement_load_goats_source_rfid_idx
  ON procurement_load_goats(tenant_id, source_rfid, load_id)
  WHERE source_rfid IS NOT NULL;

CREATE TABLE source_holding_stays (
  stay_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL,
  load_id uuid NOT NULL,
  holding_location_id uuid NOT NULL,
  started_at timestamptz NOT NULL,
  ended_at timestamptz NULL,
  warmup_state text NOT NULL DEFAULT 'in_progress',
  warmup_days int NULL,
  health_state text NOT NULL DEFAULT 'pending',
  ownership_state text NOT NULL DEFAULT 'pending',
  status text NOT NULL DEFAULT 'open',
  proof_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT source_holding_stays_warmup_state_check CHECK (warmup_state IN ('not_started', 'in_progress', 'completed', 'outside_normal_window')),
  CONSTRAINT source_holding_stays_health_state_check CHECK (health_state IN ('pending', 'passed', 'failed', 'deferred')),
  CONSTRAINT source_holding_stays_ownership_state_check CHECK (ownership_state IN ('pending', 'shared_pending', 'mesha_owned', 'blocked', 'not_owned', 'settled')),
  CONSTRAINT source_holding_stays_status_check CHECK (status IN ('open', 'closed', 'canceled')),
  CONSTRAINT source_holding_stays_warmup_days_check CHECK (warmup_days IS NULL OR warmup_days >= 0),
  CONSTRAINT source_holding_stays_window_check CHECK (ended_at IS NULL OR ended_at >= started_at),
  CONSTRAINT source_holding_stays_proof_refs_array_check CHECK (jsonb_typeof(proof_refs) = 'array'),
  CONSTRAINT source_holding_stays_row_version_check CHECK (row_version >= 1),
  CONSTRAINT source_holding_stays_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT source_holding_stays_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT source_holding_stays_location_tenant_fk FOREIGN KEY (tenant_id, holding_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT source_holding_stays_unique_window UNIQUE (tenant_id, load_id, goat_id, holding_location_id, started_at)
);

CREATE INDEX source_holding_stays_load_idx
  ON source_holding_stays(tenant_id, load_id, status, started_at DESC);
CREATE INDEX source_holding_stays_goat_idx
  ON source_holding_stays(tenant_id, goat_id, status, started_at DESC);
CREATE INDEX source_holding_stays_warmup_idx
  ON source_holding_stays(tenant_id, warmup_state, warmup_days, status);

CREATE TABLE procurement_source_health_checks (
  health_check_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL,
  load_id uuid NOT NULL,
  health_state text NOT NULL,
  reason text NOT NULL DEFAULT '',
  checked_by uuid NULL,
  checked_at timestamptz NOT NULL,
  proof_ref_id uuid NULL REFERENCES proof_artifacts(proof_id),
  sop_task_id uuid NULL,
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT procurement_source_health_checks_state_check CHECK (health_state IN ('passed', 'failed', 'deferred')),
  CONSTRAINT procurement_source_health_checks_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT procurement_source_health_checks_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT procurement_source_health_checks_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES sop_tasks(tenant_id, task_id),
  CONSTRAINT procurement_source_health_checks_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX procurement_source_health_checks_goat_idx
  ON procurement_source_health_checks(tenant_id, goat_id, checked_at DESC);

CREATE TABLE source_entry_decisions (
  decision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  goat_id uuid NOT NULL,
  load_id uuid NOT NULL,
  decision_stage text NOT NULL,
  decision_type text NOT NULL,
  reason text NOT NULL DEFAULT '',
  decided_by uuid NULL,
  decided_at timestamptz NOT NULL,
  proof_ref_id uuid NULL REFERENCES proof_artifacts(proof_id),
  sop_task_id uuid NULL,
  owner_id uuid NULL,
  resume_condition text NULL,
  idempotency_key text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT source_entry_decisions_stage_check CHECK (decision_stage IN ('source_selection', 'source_health', 'pre_dispatch', 'arrival_gate', 'accepted_intake', 'exit')),
  CONSTRAINT source_entry_decisions_type_check CHECK (decision_type IN ('accepted', 'rejected', 'deferred', 'blocked')),
  CONSTRAINT source_entry_decisions_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT source_entry_decisions_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT source_entry_decisions_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT source_entry_decisions_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES sop_tasks(tenant_id, task_id),
  CONSTRAINT source_entry_decisions_owner_fk FOREIGN KEY (owner_id) REFERENCES workforce_members(workforce_member_id),
  CONSTRAINT source_entry_decisions_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX source_entry_decisions_load_stage_idx
  ON source_entry_decisions(tenant_id, load_id, decision_stage, decided_at DESC);
CREATE INDEX source_entry_decisions_goat_idx
  ON source_entry_decisions(tenant_id, goat_id, decided_at DESC);

CREATE TABLE transit_handoffs (
  handoff_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  load_id uuid NOT NULL,
  from_location_id uuid NULL,
  to_location_id uuid NOT NULL,
  loaded_count int NOT NULL DEFAULT 0,
  dispatched_at timestamptz NOT NULL,
  arrived_at timestamptz NULL,
  proof_ref_id uuid NULL REFERENCES proof_artifacts(proof_id),
  discrepancy_state text NOT NULL DEFAULT 'none',
  status text NOT NULL DEFAULT 'in_transit',
  idempotency_key text NOT NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT transit_handoffs_loaded_count_check CHECK (loaded_count >= 0),
  CONSTRAINT transit_handoffs_status_check CHECK (status IN ('planned', 'in_transit', 'arrived', 'canceled')),
  CONSTRAINT transit_handoffs_discrepancy_check CHECK (discrepancy_state IN ('none', 'partial_load', 'accepted_not_loaded', 'missing', 'extra', 'mismatch', 'blocked')),
  CONSTRAINT transit_handoffs_row_version_check CHECK (row_version >= 1),
  CONSTRAINT transit_handoffs_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT transit_handoffs_from_location_tenant_fk FOREIGN KEY (tenant_id, from_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT transit_handoffs_to_location_tenant_fk FOREIGN KEY (tenant_id, to_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT transit_handoffs_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX transit_handoffs_load_idx
  ON transit_handoffs(tenant_id, load_id, status, dispatched_at DESC);
CREATE INDEX transit_handoffs_load_proof_idx
  ON transit_handoffs(tenant_id, load_id, status, proof_ref_id)
  WHERE proof_ref_id IS NOT NULL;

CREATE TABLE arrival_intake_reviews (
  review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  load_id uuid NOT NULL,
  park_location_id uuid NOT NULL,
  expected_count int NOT NULL DEFAULT 0,
  loaded_count int NOT NULL DEFAULT 0,
  arrived_count int NOT NULL DEFAULT 0,
  matched_count int NOT NULL DEFAULT 0,
  missing_count int NOT NULL DEFAULT 0,
  extra_count int NOT NULL DEFAULT 0,
  rejected_count int NOT NULL DEFAULT 0,
  health_flags jsonb NOT NULL DEFAULT '[]'::jsonb,
  weight_flags jsonb NOT NULL DEFAULT '[]'::jsonb,
  media_proof_id uuid NULL REFERENCES proof_artifacts(proof_id),
  status text NOT NULL DEFAULT 'pending',
  reviewed_by uuid NULL,
  reviewed_at timestamptz NOT NULL,
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT arrival_intake_reviews_counts_check CHECK (
    expected_count >= 0 AND loaded_count >= 0 AND arrived_count >= 0 AND matched_count >= 0
    AND missing_count >= 0 AND extra_count >= 0 AND rejected_count >= 0
  ),
  CONSTRAINT arrival_intake_reviews_status_check CHECK (status IN ('pending', 'mismatch', 'accepted', 'rejected', 'deferred', 'blocked')),
  CONSTRAINT arrival_intake_reviews_health_flags_array_check CHECK (jsonb_typeof(health_flags) = 'array'),
  CONSTRAINT arrival_intake_reviews_weight_flags_array_check CHECK (jsonb_typeof(weight_flags) = 'array'),
  CONSTRAINT arrival_intake_reviews_row_version_check CHECK (row_version >= 1),
  CONSTRAINT arrival_intake_reviews_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT arrival_intake_reviews_park_tenant_fk FOREIGN KEY (tenant_id, park_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT arrival_intake_reviews_tenant_id_unique UNIQUE (tenant_id, review_id),
  CONSTRAINT arrival_intake_reviews_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX arrival_intake_reviews_load_idx
  ON arrival_intake_reviews(tenant_id, load_id, status, reviewed_at DESC);

CREATE TABLE arrival_intake_review_goats (
  review_goat_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  review_id uuid NOT NULL,
  load_id uuid NOT NULL,
  goat_id uuid NULL,
  temporary_id text NULL,
  source_tag text NULL,
  item_key text NOT NULL,
  arrival_state text NOT NULL,
  health_flag text NULL,
  weight_flag text NULL,
  proof_ref_id uuid NULL REFERENCES proof_artifacts(proof_id),
  notes text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT arrival_intake_review_goats_state_check CHECK (arrival_state IN (
    'matched', 'missing', 'extra_unresolved', 'health_flag', 'weight_flag',
    'accepted', 'rejected', 'deferred', 'blocked'
  )),
  CONSTRAINT arrival_intake_review_goats_item_key_check CHECK (btrim(item_key) <> ''),
  CONSTRAINT arrival_intake_review_goats_review_tenant_fk FOREIGN KEY (tenant_id, review_id) REFERENCES arrival_intake_reviews(tenant_id, review_id),
  CONSTRAINT arrival_intake_review_goats_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT arrival_intake_review_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT arrival_intake_review_goats_item_unique UNIQUE (tenant_id, review_id, item_key)
);

CREATE INDEX arrival_intake_review_goats_review_idx
  ON arrival_intake_review_goats(tenant_id, review_id, arrival_state, review_goat_id);
CREATE INDEX arrival_intake_review_goats_load_idx
  ON arrival_intake_review_goats(tenant_id, load_id, arrival_state, goat_id);

CREATE TABLE procurement_phc_handoffs (
  handoff_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  load_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  accepted_at timestamptz NOT NULL,
  park_location_id uuid NOT NULL,
  shed_location_id uuid NOT NULL,
  entry_date date NOT NULL,
  trusted_vaccination_history jsonb NOT NULL DEFAULT '[]'::jsonb,
  intake_health_signal text NULL,
  event_status text NOT NULL DEFAULT 'pending',
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT procurement_phc_handoffs_history_array_check CHECK (jsonb_typeof(trusted_vaccination_history) = 'array'),
  CONSTRAINT procurement_phc_handoffs_status_check CHECK (event_status IN ('pending', 'emitted', 'canceled')),
  CONSTRAINT procurement_phc_handoffs_signal_check CHECK (intake_health_signal IS NULL OR intake_health_signal IN ('clear', 'defer', 'quarantine', 'review')),
  CONSTRAINT procurement_phc_handoffs_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT procurement_phc_handoffs_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT procurement_phc_handoffs_park_tenant_fk FOREIGN KEY (tenant_id, park_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT procurement_phc_handoffs_shed_tenant_fk FOREIGN KEY (tenant_id, shed_location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT procurement_phc_handoffs_idempotency_unique UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT procurement_phc_handoffs_one_per_goat UNIQUE (tenant_id, load_id, goat_id)
);

CREATE INDEX procurement_phc_handoffs_pending_idx
  ON procurement_phc_handoffs(tenant_id, event_status, accepted_at, handoff_id);
CREATE INDEX procurement_phc_handoffs_goat_idx
  ON procurement_phc_handoffs(tenant_id, goat_id, accepted_at DESC);

CREATE VIEW vw_procurement_vaccination_excluded_goats AS
SELECT DISTINCT
  g.tenant_id,
  g.goat_id,
  CASE
    WHEN g.lifecycle_status IN ('dead', 'sold', 'lost', 'culled', 'transferred', 'merged', 'inactive') THEN g.lifecycle_status
    WHEN g.identity_state IN ('disputed', 'merged', 'inactive') THEN 'identity_conflict'
    WHEN plg.identity_review_state <> 'clean' THEN 'identity_' || plg.identity_review_state
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
   OR g.identity_state IN ('disputed', 'merged', 'inactive')
   OR (
     plg.goat_id IS NOT NULL
     AND (
       plg.current_state <> 'accepted_herd_intake'
       OR plg.selection_state IN ('source_only', 'candidate', 'rejected', 'deferred', 'blocked', 'arrival_rejected', 'dead', 'sold', 'lost')
       OR plg.identity_review_state <> 'clean'
       OR plg.ownership_state NOT IN ('mesha_owned', 'settled')
       OR plg.health_state <> 'passed'
     )
   );

CREATE OR REPLACE FUNCTION block_active_vaccination_for_procurement_excluded_goat()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  excluded_reason text;
  is_vaccination boolean;
BEGIN
  IF NEW.target_type <> 'goat'
     OR NEW.status NOT IN ('scheduled', 'due', 'in_progress', 'missed', 'waived') THEN
    RETURN NEW;
  END IF;

  SELECT EXISTS (
    SELECT 1
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = NEW.tenant_id
      AND pv.protocol_version_id = NEW.protocol_version_id
      AND pd.category = 'vaccination'
  ) INTO is_vaccination;

  IF NOT is_vaccination THEN
    RETURN NEW;
  END IF;

  SELECT exclusion_reason
    INTO excluded_reason
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = NEW.tenant_id
      AND ex.goat_id = NEW.target_id
    LIMIT 1;

  IF excluded_reason IS NOT NULL THEN
    RAISE EXCEPTION 'vaccination_obligation_blocked_for_procurement_excluded_goat: goat %, reason %', NEW.target_id, excluded_reason
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

CREATE TRIGGER obligation_instances_procurement_vaccination_guard_trg
  BEFORE INSERT OR UPDATE OF target_type, target_id, status, protocol_version_id
  ON obligation_instances
  FOR EACH ROW EXECUTE FUNCTION block_active_vaccination_for_procurement_excluded_goat();

UPDATE obligation_instances oi
SET status = 'canceled',
    updated_at = now(),
    row_version = row_version + 1
WHERE oi.target_type = 'goat'
  AND oi.status IN ('scheduled', 'due', 'in_progress', 'missed', 'waived')
  AND EXISTS (
    SELECT 1
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = oi.tenant_id
      AND pv.protocol_version_id = oi.protocol_version_id
      AND pd.category = 'vaccination'
  )
  AND EXISTS (
    SELECT 1
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = oi.tenant_id
      AND ex.goat_id = oi.target_id
  );

-- +goose Down
DROP TRIGGER IF EXISTS obligation_instances_procurement_vaccination_guard_trg ON obligation_instances;
DROP FUNCTION IF EXISTS block_active_vaccination_for_procurement_excluded_goat();
DROP VIEW IF EXISTS vw_procurement_vaccination_excluded_goats;
DROP TABLE IF EXISTS procurement_phc_handoffs;
DROP TABLE IF EXISTS arrival_intake_review_goats;
DROP TABLE IF EXISTS arrival_intake_reviews;
DROP TABLE IF EXISTS transit_handoffs;
DROP TABLE IF EXISTS source_entry_decisions;
DROP TABLE IF EXISTS procurement_source_health_checks;
DROP TABLE IF EXISTS source_holding_stays;
DROP TABLE IF EXISTS procurement_load_goats;
DROP TABLE IF EXISTS procurement_loads;
