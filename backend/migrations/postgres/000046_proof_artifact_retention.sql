-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: adds runtime proof_artifacts retention bookkeeping; seed SOP proof_policy already carries retention_policy and no fixture/source shape change is needed expiry=2026-10-31
ALTER TABLE proof_artifacts
  ADD COLUMN IF NOT EXISTS retention_policy text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS retention_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS upload_expires_at timestamptz;

-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: constrains the runtime retention_policy values added above; no seed fixture/source shape change is needed expiry=2026-10-31
ALTER TABLE proof_artifacts
  ADD CONSTRAINT proof_artifacts_retention_policy_check
  CHECK (retention_policy = ANY (ARRAY[''::text, 'operational_90d'::text, 'standard_1y'::text, 'critical_7y'::text, 'legal_hold'::text]));

CREATE INDEX IF NOT EXISTS proof_artifacts_retention_expiry_idx
  ON proof_artifacts (retention_expires_at, tenant_id, proof_id)
  WHERE retention_expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS proof_artifacts_abandoned_upload_idx
  ON proof_artifacts (upload_expires_at, tenant_id, proof_id)
  WHERE upload_state IN ('pending', 'uploading')
    AND upload_expires_at IS NOT NULL;
