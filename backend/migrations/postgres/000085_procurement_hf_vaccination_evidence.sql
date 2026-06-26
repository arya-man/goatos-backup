-- +goose Up
-- Supplier/Holding-Farm vaccination evidence for the procurement -> PHC bridge.
-- Imported evidence remains procurement-owned until reviewed; only trusted rows
-- can suppress matching post-arrival vaccination obligations.

ALTER TABLE procurement_load_goats
  ADD COLUMN purpose text NOT NULL DEFAULT 'unspecified',
  ADD CONSTRAINT procurement_load_goats_purpose_check
    CHECK (purpose IN ('breeding', 'fattening', 'non_breeding', 'unspecified'));

ALTER TABLE source_holding_stays
  ADD COLUMN purpose text NOT NULL DEFAULT 'unspecified',
  ADD CONSTRAINT source_holding_stays_purpose_check
    CHECK (purpose IN ('breeding', 'fattening', 'non_breeding', 'unspecified'));

CREATE INDEX procurement_load_goats_purpose_warmup_idx
  ON procurement_load_goats(tenant_id, purpose, warmup_days, current_state, updated_at DESC);

CREATE INDEX source_holding_stays_purpose_warmup_idx
  ON source_holding_stays(tenant_id, purpose, warmup_days, status, started_at DESC);

CREATE TABLE procurement_hf_vaccination_evidence (
  evidence_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  load_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  protocol_version_id uuid NOT NULL,
  rule_id uuid NOT NULL,
  dose_code text NOT NULL,
  administered_at timestamptz NOT NULL,
  vaccine_name text NOT NULL DEFAULT '',
  lot_number text NOT NULL DEFAULT '',
  proof_ref_id uuid NULL,
  source_ref text NOT NULL DEFAULT '',
  review_status text NOT NULL DEFAULT 'imported',
  reviewed_by uuid NULL,
  reviewed_at timestamptz NULL,
  review_reason text NOT NULL DEFAULT '',
  idempotency_key text NOT NULL,
  imported_by uuid NULL,
  imported_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT procurement_hf_vaccination_evidence_dose_code_check CHECK (btrim(dose_code) <> ''),
  CONSTRAINT procurement_hf_vaccination_evidence_review_status_check
    CHECK (review_status IN ('imported', 'trusted', 'rejected', 'conflicting', 'duplicate')),
  CONSTRAINT procurement_hf_vaccination_evidence_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT procurement_hf_vaccination_evidence_row_version_check CHECK (row_version >= 1),
  CONSTRAINT procurement_hf_vaccination_evidence_load_tenant_fk
    FOREIGN KEY (tenant_id, load_id) REFERENCES procurement_loads(tenant_id, load_id),
  CONSTRAINT procurement_hf_vaccination_evidence_goat_tenant_fk
    FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT procurement_hf_vaccination_evidence_protocol_version_tenant_fk
    FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES protocol_versions(tenant_id, protocol_version_id),
  CONSTRAINT procurement_hf_vaccination_evidence_rule_tenant_fk
    FOREIGN KEY (tenant_id, rule_id) REFERENCES protocol_rules(tenant_id, rule_id),
  CONSTRAINT procurement_hf_vaccination_evidence_proof_fk
    FOREIGN KEY (proof_ref_id) REFERENCES proof_artifacts(proof_id),
  CONSTRAINT procurement_hf_vaccination_evidence_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX procurement_hf_vaccination_evidence_load_idx
  ON procurement_hf_vaccination_evidence(tenant_id, load_id, review_status, administered_at DESC, evidence_id DESC);

CREATE INDEX procurement_hf_vaccination_evidence_goat_idx
  ON procurement_hf_vaccination_evidence(tenant_id, goat_id, review_status, administered_at DESC, evidence_id DESC);

CREATE INDEX procurement_hf_vaccination_evidence_trusted_rule_idx
  ON procurement_hf_vaccination_evidence(tenant_id, goat_id, protocol_version_id, rule_id, dose_code, administered_at DESC)
  WHERE review_status = 'trusted';

-- +goose Down
DROP INDEX IF EXISTS procurement_hf_vaccination_evidence_trusted_rule_idx;
DROP INDEX IF EXISTS procurement_hf_vaccination_evidence_goat_idx;
DROP INDEX IF EXISTS procurement_hf_vaccination_evidence_load_idx;
DROP TABLE IF EXISTS procurement_hf_vaccination_evidence;

DROP INDEX IF EXISTS source_holding_stays_purpose_warmup_idx;
DROP INDEX IF EXISTS procurement_load_goats_purpose_warmup_idx;

ALTER TABLE source_holding_stays
  DROP CONSTRAINT IF EXISTS source_holding_stays_purpose_check,
  DROP COLUMN IF EXISTS purpose;

ALTER TABLE procurement_load_goats
  DROP CONSTRAINT IF EXISTS procurement_load_goats_purpose_check,
  DROP COLUMN IF EXISTS purpose;
