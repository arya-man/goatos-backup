-- Proof upload completion reads these columns in backend/internal/proof.
-- Keep this migration idempotent because clean-slate baseline DBs already include them.
-- +goose Up
ALTER TABLE public.proof_artifacts
  ADD COLUMN IF NOT EXISTS retention_policy text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS retention_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS upload_expires_at timestamptz;

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'proof_artifacts_retention_policy_check'
      AND conrelid = 'public.proof_artifacts'::regclass
  ) THEN
    ALTER TABLE public.proof_artifacts
      ADD CONSTRAINT proof_artifacts_retention_policy_check
      CHECK (retention_policy = ANY (ARRAY[
        ''::text,
        'operational_90d'::text,
        'standard_1y'::text,
        'critical_7y'::text,
        'legal_hold'::text
      ])) NOT VALID;
  END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE public.proof_artifacts
  VALIDATE CONSTRAINT proof_artifacts_retention_policy_check;

CREATE INDEX IF NOT EXISTS proof_artifacts_retention_expiry_idx
  ON public.proof_artifacts (retention_expires_at, tenant_id, proof_id)
  WHERE retention_expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS proof_artifacts_abandoned_upload_idx
  ON public.proof_artifacts (upload_expires_at, tenant_id, proof_id)
  WHERE upload_state IN ('pending', 'uploading')
    AND upload_expires_at IS NOT NULL;
