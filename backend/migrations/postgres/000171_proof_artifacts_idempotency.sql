-- +goose Up
-- MOB-002 byte-upload hardening: a mobile outbox retry of the metadata-registration write
-- (POST /app/proofs/uploads) must never mint a second backend Artifact/object for the same
-- captured video. idempotency_key is the SAME stable per-row key the mobile outbox already
-- sends verbatim on every retry (Idempotency-Key header); request_fingerprint guards against a
-- same-key/different-payload replay (AGENTS.md write-path idempotency rule) instead of silently
-- returning an unrelated proof.
ALTER TABLE proof_artifacts
  ADD COLUMN idempotency_key text NULL,
  ADD COLUMN request_fingerprint text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX proof_artifacts_tenant_idempotency_key_unique_idx
  ON proof_artifacts (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS proof_artifacts_tenant_idempotency_key_unique_idx;
ALTER TABLE proof_artifacts
  DROP COLUMN IF EXISTS idempotency_key,
  DROP COLUMN IF EXISTS request_fingerprint;
