-- +goose Up
-- Feed Direction G2 readiness uses prefix families such as
-- counts-alias-coverage-check:* and location-profile-coverage-check:* to prove
-- composite CSG subgate evidence without scanning the whole tenant ledger.

CREATE INDEX counts_shifting_readiness_evidence_prefix_idx
  ON counts_shifting_readiness_evidence (
    tenant_id,
    subgate_id,
    evidence_ref text_pattern_ops,
    recorded_at DESC
  );

-- +goose Down
DROP INDEX IF EXISTS counts_shifting_readiness_evidence_prefix_idx;
