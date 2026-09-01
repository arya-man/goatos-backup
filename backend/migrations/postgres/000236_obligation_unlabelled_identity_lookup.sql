-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational vaccination obligation index only; no source fixture schema or HRMS seed contract changes.
-- Speeds the vaccination generation fallback that adopts old open rows written before
-- obligation_instances.rule_identity_key existed. The lookup is per animal/rule sequence and
-- only runs for unlabelled active rows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_instances_unlabelled_identity_lookup_idx
  ON obligation_instances (tenant_id, target_type, target_id, "sequence", rule_id, due_at, obligation_id)
  WHERE rule_identity_key IS NULL
    AND status IN ('scheduled', 'due', 'in_progress', 'deferred');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS obligation_instances_unlabelled_identity_lookup_idx;
