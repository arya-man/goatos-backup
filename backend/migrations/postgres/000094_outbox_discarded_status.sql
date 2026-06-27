-- +goose Up

ALTER TABLE outbox_messages
  DROP CONSTRAINT IF EXISTS outbox_messages_status_check;

ALTER TABLE outbox_messages
  ADD CONSTRAINT outbox_messages_status_check
  CHECK (status IN ('pending', 'publishing', 'published', 'failed', 'dead_letter', 'discarded'));

CREATE INDEX IF NOT EXISTS outbox_messages_discarded_idx
  ON outbox_messages (tenant_id, status, updated_at DESC, outbox_id DESC)
  WHERE status = 'discarded';

CREATE TABLE IF NOT EXISTS outbox_dlq_actions (
  action_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  idempotency_key text NOT NULL,
  action text NOT NULL,
  request_hash text NOT NULL,
  reason text NOT NULL DEFAULT '',
  outbox_ids text[] NOT NULL,
  status text NOT NULL DEFAULT 'running',
  updated_count bigint NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT outbox_dlq_actions_action_check CHECK (action IN ('replay', 'discard')),
  CONSTRAINT outbox_dlq_actions_status_check CHECK (status IN ('running', 'completed')),
  CONSTRAINT outbox_dlq_actions_updated_count_check CHECK (updated_count >= 0),
  CONSTRAINT outbox_dlq_actions_key_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS outbox_dlq_actions_tenant_created_idx
  ON outbox_dlq_actions (tenant_id, created_at DESC, action_id DESC);

-- +goose Down

UPDATE outbox_messages
SET status = 'dead_letter',
    last_error = COALESCE(NULLIF(last_error, ''), 'discarded_status_rollback'),
    updated_at = now()
WHERE status = 'discarded';

DROP INDEX IF EXISTS outbox_messages_discarded_idx;
DROP INDEX IF EXISTS outbox_dlq_actions_tenant_created_idx;
DROP TABLE IF EXISTS outbox_dlq_actions;

ALTER TABLE outbox_messages
  DROP CONSTRAINT IF EXISTS outbox_messages_status_check;

ALTER TABLE outbox_messages
  ADD CONSTRAINT outbox_messages_status_check
  CHECK (status IN ('pending', 'publishing', 'published', 'failed', 'dead_letter'));
