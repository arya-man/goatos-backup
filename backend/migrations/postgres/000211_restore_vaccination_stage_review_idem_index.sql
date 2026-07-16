-- +goose Up
-- Forward repair for environments that already applied the originally shipped 000210, whose Up
-- section dropped this index. Editing 000210 cannot repair those databases because goose records the
-- version as applied and will not execute it again.
--
-- Keep this index alongside vaccination_stage_review_items_open_goat_unique until every predecessor
-- writer using ON CONFLICT (tenant_id, idempotency_key) has been drained.
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_stage_review_items_open_idem_unique
  ON vaccination_stage_review_items (tenant_id, idempotency_key)
  WHERE status = 'open';

-- +goose Down
-- Deliberately retained on rollback: this index predates 000211 and is required by still-live
-- predecessor writers. Dropping it here would recreate the rollout outage this repair closes.
SELECT 1;
