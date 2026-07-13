-- +goose Up
-- Per-attempt notification delivery ledger (docs/decisions/vaccination-notification-rules.md
-- audit section, gap 1): notification_requests only keeps the LATEST failure_reason + a running
-- delivery_attempts count, so a request that failed twice then succeeded loses its earlier
-- failure evidence. This table is an append-only ledger of every dispatch attempt (success and
-- failure), written in the SAME transaction as the notification_requests status flip
-- (repository.go MarkSent / MarkFailed), so there is never an attempt row without a status
-- change and never a status change without an attempt row.
--
-- Plain CREATE TABLE + CREATE INDEX is lock-safe here: the table is brand new and empty at
-- migration time, so no ACCESS EXCLUSIVE contention against live traffic is possible. The table
-- is registered as hot in validate-hot-index-migrations.sh (same write-per-dispatch-attempt rate
-- as notification_requests) so any FUTURE migration that alters it is forced through the
-- CONCURRENTLY / NOT VALID+VALIDATE lock-safe patterns.
CREATE TABLE notification_delivery_attempts (
  attempt_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  notification_request_id uuid NOT NULL REFERENCES notification_requests(notification_request_id),
  attempt_no int NOT NULL,
  channel text NOT NULL,
  attempted_at timestamptz NOT NULL,
  result text NOT NULL,
  provider_message_id text NULL,
  error text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT notification_delivery_attempts_result_check CHECK (result IN ('sent', 'failed')),
  CONSTRAINT notification_delivery_attempts_attempt_no_check CHECK (attempt_no > 0),
  CONSTRAINT notification_delivery_attempts_unique UNIQUE (tenant_id, notification_request_id, attempt_no)
);

CREATE INDEX notification_delivery_attempts_request_idx
  ON notification_delivery_attempts (tenant_id, notification_request_id, attempt_no);

-- +goose Down
DROP TABLE IF EXISTS notification_delivery_attempts;
