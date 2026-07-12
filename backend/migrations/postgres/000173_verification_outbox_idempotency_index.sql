-- +goose Up
-- +goose NO TRANSACTION
-- Idempotency backstop for the verification status-event seam (build-handover-20260713.md §1 P0
-- 1a): item-pending / verdict-approved / verdict-rework events insert into the EXISTING (hot)
-- outbox_messages table with ON CONFLICT (tenant_id, idempotency_key) DO NOTHING, mirroring the
-- vaccination.completed / config.changed precedents (000106_calendar_retention_and_outbox_repair.sql).
-- outbox_messages is a hot table (backend/tests/integration/validate-hot-index-migrations.sh), so the
-- backing partial unique index must be built CONCURRENTLY outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_verification_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type IN ('verification.item.pending', 'verification.verdict.approved', 'verification.verdict.rework');

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS outbox_messages_verification_idempotency_idx;
