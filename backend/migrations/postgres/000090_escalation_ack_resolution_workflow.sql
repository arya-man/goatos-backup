-- +goose Up
-- +goose StatementBegin
ALTER TABLE obligation_escalations
  ADD COLUMN acknowledged_by uuid NULL,
  ADD COLUMN resolved_by uuid NULL,
  ADD COLUMN acknowledgement_note text NOT NULL DEFAULT '',
  ADD COLUMN resolution_note text NOT NULL DEFAULT '';

ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;
ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
  CHECK (event_type IN (
    'scheduled', 'became_due', 'dispatched', 'completed', 'missed', 'waived',
    'escalated', 'escalation_acknowledged', 'escalation_resolved',
    'canceled', 'deferred', 'rescoped'
  ));

CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.aggregate_type = 'calendar_notification' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM notification_requests
      WHERE tenant_id = NEW.tenant_id
        AND notification_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'calendar notification outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_snooze' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM calendar_snoozes
      WHERE tenant_id = NEW.tenant_id
        AND snooze_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'calendar snooze outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'obligation_escalation' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM obligation_escalations
      WHERE tenant_id = NEW.tenant_id
        AND escalation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'obligation escalation outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'correction_request' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM identity_correction_requests
      WHERE tenant_id = NEW.tenant_id
        AND correction_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'correction request outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM goat_identity_events
    WHERE tenant_id = NEW.tenant_id
      AND identity_event_id = NEW.event_id
  ) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM obligation_status_events
WHERE event_type IN ('escalation_acknowledged', 'escalation_resolved');

ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;
ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
  CHECK (event_type IN (
    'scheduled', 'became_due', 'dispatched', 'completed', 'missed', 'waived',
    'escalated', 'canceled', 'deferred', 'rescoped'
  ));

CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.aggregate_type = 'calendar_notification' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM notification_requests
      WHERE tenant_id = NEW.tenant_id
        AND notification_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'calendar notification outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'calendar_snooze' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM calendar_snoozes
      WHERE tenant_id = NEW.tenant_id
        AND snooze_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'calendar snooze outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'correction_request' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM identity_correction_requests
      WHERE tenant_id = NEW.tenant_id
        AND correction_request_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'correction request outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM goat_identity_events
    WHERE tenant_id = NEW.tenant_id
      AND identity_event_id = NEW.event_id
  ) THEN
    RAISE EXCEPTION 'outbox event % does not exist for tenant %', NEW.event_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  RETURN NEW;
END;
$$;

ALTER TABLE obligation_escalations
  DROP COLUMN IF EXISTS resolution_note,
  DROP COLUMN IF EXISTS acknowledgement_note,
  DROP COLUMN IF EXISTS resolved_by,
  DROP COLUMN IF EXISTS acknowledged_by;
-- +goose StatementEnd
