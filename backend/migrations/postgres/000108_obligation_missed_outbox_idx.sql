-- +goose Up
CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_obligation_missed_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'obligation.missed';

-- +goose Down
DROP INDEX IF EXISTS outbox_messages_obligation_missed_idempotency_idx;
