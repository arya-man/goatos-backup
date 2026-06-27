-- +goose Up

ALTER TABLE outbox_messages
  ADD COLUMN IF NOT EXISTS replay_count int NOT NULL DEFAULT 0;

ALTER TABLE outbox_messages
  DROP CONSTRAINT IF EXISTS outbox_messages_replay_count_check;

ALTER TABLE outbox_messages
  ADD CONSTRAINT outbox_messages_replay_count_check CHECK (replay_count >= 0);

CREATE INDEX IF NOT EXISTS outbox_messages_replay_guard_idx
  ON outbox_messages (tenant_id, status, replay_count, updated_at)
  WHERE status IN ('dead_letter', 'failed');

-- +goose Down

DROP INDEX IF EXISTS outbox_messages_replay_guard_idx;

ALTER TABLE outbox_messages
  DROP CONSTRAINT IF EXISTS outbox_messages_replay_count_check;

ALTER TABLE outbox_messages
  DROP COLUMN IF EXISTS replay_count;
