-- +goose Up
-- Phase 1A · Vaccination module. The dose-administered record (vaccination_completions) plus the
-- feature-coverage registry entry and a vaccination SOP execution/proof SKELETON (structure only,
-- NO vaccine schedule values). Builds on the Phase 0 generic engine; all cross-table refs use
-- tenant-safe composite (tenant_id, *) FKs.

-- Register vaccination as a covered feature module.
ALTER TABLE feature_coverage_registry DROP CONSTRAINT IF EXISTS feature_coverage_registry_module_check;
ALTER TABLE feature_coverage_registry
  ADD CONSTRAINT feature_coverage_registry_module_check
    CHECK (feature_module IN ('counts', 'mortality', 'locations', 'vaccination'));

-- Tenant-safe composite key on the SOP submission item table so completions can reference proof
-- via a composite (tenant_id, item_id) FK (item_id alone is the PK).
ALTER TABLE sop_submission_items ADD CONSTRAINT sop_submission_items_tenant_id_unique UNIQUE (tenant_id, item_id);

CREATE TABLE vaccination_completions (
  completion_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  obligation_id uuid NOT NULL,
  batch_id uuid NULL,
  goat_id uuid NOT NULL,
  sop_submission_item_id uuid NULL,
  vaccine_inventory_lot_id uuid NULL,
  doses int NULL,
  dose_ml_given numeric NULL,
  route_site text NULL,
  adverse_reaction boolean NOT NULL DEFAULT false,
  adverse_reaction_problem_id uuid NULL, -- references a future health problem catalog; no FK yet
  cold_chain_verified boolean NOT NULL DEFAULT false,
  administered_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'recorded',
  verified_by uuid NULL,
  verified_at timestamptz NULL,
  rejection_reason text NULL,
  withdrawal_until_date date NULL,
  recorded_by uuid NULL,
  idempotency_key text NOT NULL,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_completions_status_check CHECK (status IN ('recorded', 'accepted', 'rejected', 'reversed')),
  CONSTRAINT vaccination_completions_doses_check CHECK (doses IS NULL OR doses > 0),
  CONSTRAINT vaccination_completions_dose_ml_check CHECK (dose_ml_given IS NULL OR dose_ml_given > 0),
  CONSTRAINT vaccination_completions_row_version_check CHECK (row_version >= 1),
  -- Double-submit guard: one accepted/recorded completion per obligation+goat.
  CONSTRAINT vaccination_completions_obligation_goat_unique UNIQUE (tenant_id, obligation_id, goat_id),
  CONSTRAINT vaccination_completions_idempotency_unique UNIQUE (tenant_id, idempotency_key),
  CONSTRAINT vaccination_completions_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES obligation_instances(tenant_id, obligation_id),
  CONSTRAINT vaccination_completions_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES obligation_batches(tenant_id, batch_id),
  CONSTRAINT vaccination_completions_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id),
  CONSTRAINT vaccination_completions_submission_item_tenant_fk FOREIGN KEY (tenant_id, sop_submission_item_id) REFERENCES sop_submission_items(tenant_id, item_id),
  CONSTRAINT vaccination_completions_lot_tenant_fk FOREIGN KEY (tenant_id, vaccine_inventory_lot_id) REFERENCES inventory_stock(tenant_id, stock_id)
);

-- Goat Passport vaccination history (most-recent first) + per-obligation / per-batch lookups.
CREATE INDEX vaccination_completions_goat_history_idx ON vaccination_completions(tenant_id, goat_id, administered_at DESC);
CREATE INDEX vaccination_completions_obligation_idx ON vaccination_completions(tenant_id, obligation_id);
CREATE INDEX vaccination_completions_batch_idx ON vaccination_completions(tenant_id, batch_id, status);

-- Vaccination SOP execution/proof skeleton (structure only — NO schedule/vaccine values).
INSERT INTO sop_definitions (sop_id, tenant_id, code, name, description, status)
VALUES ('b0000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001',
        'vaccination.drive', 'Vaccination drive', 'Shed vaccination drive execution + proof skeleton.', 'draft')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ('b0000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001',
        'b0000000-0000-4000-8000-000000000001', 1, 'v1-skeleton', 'draft',
        '{"steps":[{"key":"shed_video","type":"video","label":"Shed drive video"},{"key":"vial_lot","type":"scan","label":"Vial / lot proof"},{"key":"cold_chain","type":"yesno","label":"Cold chain verified"},{"key":"dose","type":"number","label":"Dose"},{"key":"route_site","type":"text","label":"Route / site"},{"key":"administered_at","type":"datetime","label":"Administered at"},{"key":"adverse_reaction","type":"yesno","label":"Adverse reaction"},{"key":"est_vs_used","type":"number","label":"Estimated vs used quantity"},{"key":"verifier_review","type":"review","label":"Verifier review"}]}'::jsonb,
        '{"required":["shed_video","vial_lot","cold_chain","dose"],"verify_capability":"proof.verify"}'::jsonb)
ON CONFLICT DO NOTHING;

-- +goose Down
DROP INDEX IF EXISTS vaccination_completions_batch_idx;
DROP INDEX IF EXISTS vaccination_completions_obligation_idx;
DROP INDEX IF EXISTS vaccination_completions_goat_history_idx;
DROP TABLE IF EXISTS vaccination_completions;
ALTER TABLE sop_submission_items DROP CONSTRAINT IF EXISTS sop_submission_items_tenant_id_unique;
DELETE FROM sop_versions WHERE sop_version_id = 'b0000000-0000-4000-8000-000000000002';
DELETE FROM sop_definitions WHERE sop_id = 'b0000000-0000-4000-8000-000000000001';
ALTER TABLE feature_coverage_registry DROP CONSTRAINT IF EXISTS feature_coverage_registry_module_check;
ALTER TABLE feature_coverage_registry
  ADD CONSTRAINT feature_coverage_registry_module_check
    CHECK (feature_module IN ('counts', 'mortality', 'locations'));
