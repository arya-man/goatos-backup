-- +goose Up
-- Generic Calendar projection/action state for the PHC vaccination slice.
-- Calendar reads dated human work from this projection; vaccination truth stays in protocol,
-- obligation, SOP, proof, inventory, and audit tables.

CREATE TABLE calendar_event_projections (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  event_id text NOT NULL,
  slice_key text NOT NULL,
  event_type text NOT NULL,
  owner_key text NOT NULL,
  title text NOT NULL,
  subtitle text NOT NULL DEFAULT '',
  status text NOT NULL,
  severity text NOT NULL DEFAULT 'info',
  due_at timestamptz NULL,
  window_start timestamptz NULL,
  window_end timestamptz NULL,
  timezone text NOT NULL DEFAULT 'Asia/Kolkata',
  timezone_source text NOT NULL DEFAULT 'fallback',
  park_id uuid NULL,
  park_code text NULL,
  shed_id uuid NULL,
  shed_name text NULL,
  cohort_id uuid NULL,
  cohort_name text NULL,
  target_type text NOT NULL,
  target_count int NOT NULL DEFAULT 0,
  protocol_id uuid NULL,
  protocol_version_id uuid NULL,
  rule_id uuid NULL,
  vaccine_name text NULL,
  dose_code text NULL,
  source_backed boolean NOT NULL DEFAULT false,
  source_label text NOT NULL DEFAULT '',
  source_target_type text NOT NULL DEFAULT 'calendar_event',
  source_target_id uuid NULL,
  assignee_label text NULL,
  executor_role text NULL,
  verifier_label text NULL,
  reminder_state text NOT NULL DEFAULT 'not_scheduled',
  primary_notification_channel text NOT NULL DEFAULT 'not configured',
  escalation_state text NOT NULL DEFAULT 'none',
  system boolean NOT NULL DEFAULT false,
  cross_cutting boolean NOT NULL DEFAULT false,
  links jsonb NOT NULL DEFAULT '{}'::jsonb,
  detail jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_id),
  CONSTRAINT calendar_event_slice_check CHECK (slice_key IN ('vaccination')),
  CONSTRAINT calendar_event_type_check CHECK (event_type IN (
    'vaccination_dose_due',
    'vaccination_drive',
    'vaccination_campaign',
    'vaccination_booster_due',
    'vaccination_defer_review',
    'vaccination_evidence_review',
    'vaccination_proof_verification',
    'vaccination_rework_due',
    'vaccine_stock_readiness',
    'vaccine_cold_chain_check',
    'vaccine_reorder_expiry_grn',
    'phc_stock_anti_misuse',
    'vaccination_config_activation_review'
  )),
  CONSTRAINT calendar_event_owner_check CHECK (owner_key IN ('phc', 'inventory', 'admin_data_ops')),
  CONSTRAINT calendar_event_status_check CHECK (status IN (
    'scheduled', 'due', 'overdue', 'in_progress', 'proof_pending',
    'verification_pending', 'rejected', 'rework_due', 'deferred', 'blocked',
    'completed', 'canceled'
  )),
  CONSTRAINT calendar_event_severity_check CHECK (severity IN ('info', 'warning', 'critical')),
  CONSTRAINT calendar_event_target_count_check CHECK (target_count >= 0),
  CONSTRAINT calendar_event_links_object_check CHECK (jsonb_typeof(links) = 'object'),
  CONSTRAINT calendar_event_detail_object_check CHECK (jsonb_typeof(detail) = 'object'),
  CONSTRAINT calendar_event_human_action_check CHECK (
    system
    OR (
      due_at IS NOT NULL
      AND owner_key IN ('phc', 'inventory', 'admin_data_ops')
      AND (executor_role IS NOT NULL OR assignee_label IS NOT NULL OR verifier_label IS NOT NULL)
    )
  ),
  CONSTRAINT calendar_event_window_check CHECK (window_end IS NULL OR window_start IS NULL OR window_end >= window_start),
  CONSTRAINT calendar_event_location_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT calendar_event_location_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT calendar_event_protocol_fk FOREIGN KEY (tenant_id, protocol_id) REFERENCES protocol_definitions(tenant_id, protocol_id),
  CONSTRAINT calendar_event_version_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES protocol_versions(tenant_id, protocol_version_id),
  CONSTRAINT calendar_event_rule_fk FOREIGN KEY (tenant_id, rule_id) REFERENCES protocol_rules(tenant_id, rule_id)
);

CREATE INDEX calendar_event_projections_hot_list_idx
  ON calendar_event_projections (tenant_id, slice_key, system, due_at, event_id)
  INCLUDE (owner_key, status, severity, park_id, shed_id)
  WHERE system = false AND due_at IS NOT NULL;

CREATE INDEX calendar_event_projections_owner_window_idx
  ON calendar_event_projections (tenant_id, slice_key, owner_key, due_at, event_id)
  WHERE system = false AND due_at IS NOT NULL;

CREATE INDEX calendar_event_projections_status_window_idx
  ON calendar_event_projections (tenant_id, slice_key, status, due_at, event_id)
  WHERE system = false AND due_at IS NOT NULL;

CREATE INDEX calendar_event_projections_scope_window_idx
  ON calendar_event_projections (tenant_id, slice_key, park_id, shed_id, due_at, event_id)
  WHERE system = false AND due_at IS NOT NULL;

CREATE INDEX calendar_event_projections_due_reminder_idx
  ON calendar_event_projections (tenant_id, slice_key, reminder_state, due_at, event_id)
  WHERE system = false
    AND due_at IS NOT NULL
    AND status IN ('scheduled', 'due', 'overdue', 'in_progress', 'proof_pending', 'verification_pending', 'rework_due', 'deferred', 'blocked');

CREATE TABLE notification_requests (
  notification_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  calendar_event_id text NOT NULL,
  target_type text NOT NULL,
  target_id uuid NULL,
  notification_type text NOT NULL,
  channel text NOT NULL,
  recipient_ref text NULL,
  title text NOT NULL,
  body text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'queued',
  requested_by uuid NULL,
  requested_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz NULL,
  read_at timestamptz NULL,
  failure_reason text NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  trace_id text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT notification_requests_type_check CHECK (notification_type IN ('reminder', 'nudge', 'escalation')),
  CONSTRAINT notification_requests_channel_check CHECK (channel IN ('local-stub', 'push_fcm', 'slack', 'email', 'webhook')),
  CONSTRAINT notification_requests_status_check CHECK (status IN ('queued', 'sent', 'failed', 'suppressed', 'read')),
  CONSTRAINT notification_requests_context_object_check CHECK (jsonb_typeof(context) = 'object'),
  CONSTRAINT notification_requests_event_fk FOREIGN KEY (tenant_id, calendar_event_id)
    REFERENCES calendar_event_projections(tenant_id, event_id),
  CONSTRAINT notification_requests_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX notification_requests_event_idx
  ON notification_requests (tenant_id, calendar_event_id, requested_at DESC, notification_request_id DESC);

CREATE INDEX notification_requests_queue_idx
  ON notification_requests (tenant_id, status, requested_at, notification_request_id)
  WHERE status IN ('queued', 'failed');

CREATE TABLE calendar_snoozes (
  snooze_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  calendar_event_id text NOT NULL,
  target_type text NOT NULL,
  target_id uuid NULL,
  snooze_until timestamptz NOT NULL,
  reason text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  replaced_by_snooze_id uuid NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  trace_id text NULL,
  CONSTRAINT calendar_snoozes_status_check CHECK (status IN ('active', 'replaced', 'expired', 'canceled')),
  CONSTRAINT calendar_snoozes_context_object_check CHECK (jsonb_typeof(context) = 'object'),
  CONSTRAINT calendar_snoozes_event_fk FOREIGN KEY (tenant_id, calendar_event_id)
    REFERENCES calendar_event_projections(tenant_id, event_id),
  CONSTRAINT calendar_snoozes_replaced_fk FOREIGN KEY (replaced_by_snooze_id) REFERENCES calendar_snoozes(snooze_id),
  CONSTRAINT calendar_snoozes_idempotency_unique UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX calendar_snoozes_event_idx
  ON calendar_snoozes (tenant_id, calendar_event_id, created_at DESC, snooze_id DESC);

CREATE INDEX calendar_snoozes_active_idx
  ON calendar_snoozes (tenant_id, calendar_event_id, snooze_until)
  WHERE status = 'active';

CREATE INDEX audit_log_tenant_calendar_event_recorded_idx
  ON audit_log (tenant_id, (metadata->>'calendar_event_id'), recorded_at DESC, audit_id DESC)
  WHERE metadata ? 'calendar_event_id';

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

-- +goose Down
CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
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

DROP INDEX IF EXISTS audit_log_tenant_calendar_event_recorded_idx;
DROP TABLE IF EXISTS calendar_snoozes;
DROP TABLE IF EXISTS notification_requests;
DROP TABLE IF EXISTS calendar_event_projections;
