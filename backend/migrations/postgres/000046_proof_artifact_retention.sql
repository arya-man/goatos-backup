-- +goose Up
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: adds runtime proof_artifacts retention bookkeeping; seed SOP proof_policy already carries retention_policy and no fixture/source shape change is needed expiry=2026-10-31
ALTER TABLE proof_artifacts
  ADD COLUMN IF NOT EXISTS retention_policy text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS retention_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS upload_expires_at timestamptz;

-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: constrains the runtime retention_policy values added above; no seed fixture/source shape change is needed expiry=2026-10-31
SET lock_timeout = '5s';
ALTER TABLE proof_artifacts
  ADD CONSTRAINT proof_artifacts_retention_policy_check
  CHECK (retention_policy = ANY (ARRAY[''::text, 'operational_90d'::text, 'standard_1y'::text, 'critical_7y'::text, 'legal_hold'::text])) NOT VALID;

-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: validates runtime retention_policy values only; no seed fixture/source shape change is needed expiry=2026-10-31
SET lock_timeout = '5s';
ALTER TABLE proof_artifacts
  VALIDATE CONSTRAINT proof_artifacts_retention_policy_check;

CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_retention_expiry_idx
  ON proof_artifacts (retention_expires_at, tenant_id, proof_id)
  WHERE retention_expires_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_abandoned_upload_idx
  ON proof_artifacts (upload_expires_at, tenant_id, proof_id)
  WHERE upload_state IN ('pending', 'uploading')
    AND upload_expires_at IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS proof_artifacts_abandoned_upload_idx;
DROP INDEX CONCURRENTLY IF EXISTS proof_artifacts_retention_expiry_idx;
-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: rollback removes runtime retention columns only; seed SOP proof_policy is unchanged expiry=2026-10-31
SET lock_timeout = '5s';
ALTER TABLE proof_artifacts
  DROP CONSTRAINT IF EXISTS proof_artifacts_retention_policy_check,
  DROP COLUMN IF EXISTS upload_expires_at,
  DROP COLUMN IF EXISTS retention_expires_at,
  DROP COLUMN IF EXISTS retention_policy;
