-- +goose Up
CREATE INDEX IF NOT EXISTS vaccination_completions_trusted_evidence_lookup_idx
  ON vaccination_completions (tenant_id, goat_id, obligation_id, administered_at, verified_at)
  WHERE status = 'accepted' AND verified_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS obligation_batches_nearest_planned_lookup_idx
  ON obligation_batches (tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date)
  WHERE status = 'planned' AND sop_task_id IS NULL AND NOT (context ? 'stock_reservation');

-- +goose Down
DROP INDEX IF EXISTS vaccination_completions_trusted_evidence_lookup_idx;
DROP INDEX IF EXISTS obligation_batches_nearest_planned_lookup_idx;
