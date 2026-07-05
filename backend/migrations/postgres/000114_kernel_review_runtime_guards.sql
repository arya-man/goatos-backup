-- +goose Up
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_channel_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_channel_check
  CHECK (channel IN ('local-stub', 'push_fcm', 'slack', 'email', 'webhook', 'incident', 'opsgenie', 'pagerduty'));

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_status_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_status_check
  CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'exhausted', 'suppressed', 'read'));

UPDATE idempotency_keys
SET expires_at = COALESCE(completed_at, first_seen_at, now()) + interval '90 days'
WHERE expires_at IS NULL;

ALTER TABLE idempotency_keys
  ALTER COLUMN expires_at SET DEFAULT (now() + interval '90 days');

WITH ranked AS (
  SELECT
    ctid,
    row_number() OVER (
      PARTITION BY tenant_id, idempotency_key
      ORDER BY created_at ASC, outbox_id ASC
    ) AS rn
  FROM outbox_messages
  WHERE event_type = 'protocol.version.published'
)
DELETE FROM outbox_messages om
USING ranked r
WHERE om.ctid = r.ctid
  AND r.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_protocol_published_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'protocol.version.published';

CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_protocol_retired_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'protocol.version.retired';

CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_obligation_canceled_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'goat.obligations_canceled';

CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_obligation_rescoped_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'obligation.rescoped';

UPDATE obligation_batches ob
SET context = context || jsonb_build_object(
      'stock_reservation',
      jsonb_build_object('state', 'reserved', 'migrated_at', now()::text)
    ),
    updated_at = now(),
    row_version = row_version + 1
WHERE ob.status = 'planned'
  AND ob.sop_task_id IS NULL
  AND NOT (ob.context ? 'stock_reservation')
  AND EXISTS (
    SELECT 1
    FROM inventory_stock_movements ism
    WHERE ism.tenant_id = ob.tenant_id
      AND ism.batch_id = ob.batch_id
      AND ism.movement_type = 'reserve'
  );

CREATE UNIQUE INDEX IF NOT EXISTS obligation_batches_unfinalized_planned_unique_idx
  ON obligation_batches (
    tenant_id,
    protocol_version_id,
    scope_type,
    scope_id,
    COALESCE(session, ''),
    COALESCE(planned_date, '-infinity'::date),
    COALESCE(window_start, '-infinity'::timestamptz),
    COALESCE(window_end, '-infinity'::timestamptz)
  )
  WHERE status = 'planned'
    AND sop_task_id IS NULL
    AND NOT (context ? 'stock_reservation');

CREATE OR REPLACE FUNCTION mark_obligation_batch_stock_reservation_from_movement()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.batch_id IS NOT NULL AND NEW.movement_type = 'reserve' THEN
    UPDATE obligation_batches
    SET context = context || jsonb_build_object(
          'stock_reservation',
          jsonb_build_object('state', 'reserved', 'reserved_at', now()::text)
        ),
        updated_at = now(),
        row_version = row_version + 1
    WHERE tenant_id = NEW.tenant_id
      AND batch_id = NEW.batch_id
      AND NOT (context ? 'stock_reservation');
  END IF;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS inventory_stock_movements_batch_reserve_marker_trg ON inventory_stock_movements;
CREATE TRIGGER inventory_stock_movements_batch_reserve_marker_trg
  AFTER INSERT ON inventory_stock_movements
  FOR EACH ROW EXECUTE FUNCTION mark_obligation_batch_stock_reservation_from_movement();

-- +goose Down
DROP TRIGGER IF EXISTS inventory_stock_movements_batch_reserve_marker_trg ON inventory_stock_movements;
DROP FUNCTION IF EXISTS mark_obligation_batch_stock_reservation_from_movement();

DROP INDEX IF EXISTS obligation_batches_unfinalized_planned_unique_idx;

DROP INDEX IF EXISTS outbox_messages_obligation_rescoped_idempotency_idx;
DROP INDEX IF EXISTS outbox_messages_obligation_canceled_idempotency_idx;
DROP INDEX IF EXISTS outbox_messages_protocol_retired_idempotency_idx;
DROP INDEX IF EXISTS outbox_messages_protocol_published_idempotency_idx;

ALTER TABLE idempotency_keys
  ALTER COLUMN expires_at DROP DEFAULT;

UPDATE idempotency_keys
SET expires_at = NULL
WHERE expires_at IS NOT NULL;

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_status_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_status_check
  CHECK (status IN ('queued', 'sending', 'sent', 'failed', 'suppressed', 'read'));

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_channel_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_channel_check
  CHECK (channel IN ('local-stub', 'push_fcm', 'slack', 'email', 'webhook'));
