-- +goose Up
CREATE INDEX IF NOT EXISTS obligation_instances_unbatched_due_version_idx
  ON obligation_instances (
    tenant_id,
    protocol_version_id,
    scope_type,
    scope_id,
    rule_id,
    due_at,
    obligation_id
  )
  WHERE batch_id IS NULL
    AND status IN ('scheduled', 'due', 'missed');

-- +goose Down
DROP INDEX IF EXISTS obligation_instances_unbatched_due_version_idx;
