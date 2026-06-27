-- +goose Up
-- Operational-kernel closure for Calendar/Vaccination alerts:
-- durable notification delivery leases/retries plus idempotent SLA escalations.

ALTER TABLE notification_requests
  ADD COLUMN delivery_attempts int NOT NULL DEFAULT 0,
  ADD COLUMN next_attempt_at timestamptz NULL,
  ADD COLUMN leased_at timestamptz NULL,
  ADD COLUMN lease_token uuid NULL,
  ADD COLUMN delivered_by text NULL;

ALTER TABLE notification_requests
  DROP CONSTRAINT notification_requests_status_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_status_check
  CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'suppressed', 'read'));

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_delivery_attempts_check
  CHECK (delivery_attempts >= 0);

DROP INDEX IF EXISTS notification_requests_queue_idx;

CREATE INDEX notification_requests_queue_idx
  ON notification_requests (tenant_id, status, COALESCE(next_attempt_at, requested_at), notification_request_id)
  WHERE status IN ('queued', 'failed');

CREATE INDEX notification_requests_sending_lease_idx
  ON notification_requests (tenant_id, leased_at, notification_request_id)
  WHERE status = 'sending';

CREATE UNIQUE INDEX obligation_escalations_open_level_unique
  ON obligation_escalations (tenant_id, obligation_id, level)
  WHERE status IN ('open', 'acknowledged');

CREATE INDEX obligation_escalations_obligation_level_idx
  ON obligation_escalations (tenant_id, obligation_id, level, status);

-- +goose Down
DROP INDEX IF EXISTS obligation_escalations_obligation_level_idx;
DROP INDEX IF EXISTS obligation_escalations_open_level_unique;
DROP INDEX IF EXISTS notification_requests_sending_lease_idx;

DROP INDEX IF EXISTS notification_requests_queue_idx;

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_delivery_attempts_check;

ALTER TABLE notification_requests
  DROP CONSTRAINT notification_requests_status_check;

UPDATE notification_requests
SET status = 'queued',
    updated_at = now()
WHERE status = 'sending';

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_status_check
  CHECK (status IN ('queued', 'sent', 'failed', 'suppressed', 'read'));

CREATE INDEX notification_requests_queue_idx
  ON notification_requests (tenant_id, status, requested_at, notification_request_id)
  WHERE status IN ('queued', 'failed');

ALTER TABLE notification_requests
  DROP COLUMN IF EXISTS delivered_by,
  DROP COLUMN IF EXISTS lease_token,
  DROP COLUMN IF EXISTS leased_at,
  DROP COLUMN IF EXISTS next_attempt_at,
  DROP COLUMN IF EXISTS delivery_attempts;
