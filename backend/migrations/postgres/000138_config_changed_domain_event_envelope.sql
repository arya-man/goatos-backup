-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION admin_ui_emit_config_family_change(
  p_tenant_id uuid,
  p_family_key text,
  p_changed_by uuid DEFAULT NULL,
  p_source text DEFAULT 'system',
  p_metadata jsonb DEFAULT '{}'::jsonb
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  next_revision bigint;
  next_hash text;
  event_uuid uuid;
  event_idempotency_key text;
  family text := btrim(COALESCE(p_family_key, ''));
  normalized_source text := COALESCE(NULLIF(btrim(p_source), ''), 'system');
  normalized_metadata jsonb := COALESCE(p_metadata, '{}'::jsonb);
  occurred text;
BEGIN
  IF p_tenant_id IS NULL OR family = '' THEN
    RETURN;
  END IF;

  INSERT INTO admin_ui_config_family_revisions (
    tenant_id,
    family_key,
    revision,
    content_hash,
    changed_at,
    changed_by,
    source,
    metadata
  ) VALUES (
    p_tenant_id,
    family,
    1,
    md5(p_tenant_id::text || ':' || family || ':1:' || clock_timestamp()::text),
    now(),
    p_changed_by,
    normalized_source,
    normalized_metadata
  )
  ON CONFLICT (tenant_id, family_key) DO UPDATE
  SET revision = admin_ui_config_family_revisions.revision + 1,
      content_hash = md5(
        EXCLUDED.tenant_id::text || ':' ||
        EXCLUDED.family_key || ':' ||
        (admin_ui_config_family_revisions.revision + 1)::text || ':' ||
        clock_timestamp()::text
      ),
      changed_at = now(),
      changed_by = EXCLUDED.changed_by,
      source = EXCLUDED.source,
      metadata = EXCLUDED.metadata
  RETURNING revision, content_hash
    INTO next_revision, next_hash;

  event_uuid := admin_ui_deterministic_config_event_uuid(p_tenant_id, family, next_revision);
  event_idempotency_key := 'admin-ui-config:' || p_tenant_id::text || ':' || family || ':' || next_revision::text;
  occurred := to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"');

  INSERT INTO outbox_messages (
    tenant_id,
    event_id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    topic,
    payload,
    headers,
    idempotency_key,
    trace_id,
    status,
    next_attempt_at
  ) VALUES (
    p_tenant_id,
    event_uuid,
    'config.changed',
    '1.0.0',
    'admin_ui_config_family',
    p_tenant_id,
    'config.changed',
    jsonb_build_object(
      'event_id', event_uuid,
      'event_type', 'config.changed',
      'schema_version', '1.0.0',
      'schema_ref', 'contracts/jsonschema/domain-event-envelope.schema.json#config.changed',
      'aggregate_type', 'admin_ui_config_family',
      'aggregate_id', p_tenant_id,
      'occurred_at', occurred,
      'recorded_at', occurred,
      'producer', jsonb_build_object(
        'service', 'postgres',
        'module', 'admin_ui_config_family_revisions',
        'version', NULL
      ),
      'idempotency_key', event_idempotency_key,
      'actor', jsonb_build_object(
        'actor_type', CASE WHEN p_changed_by IS NULL THEN 'system_rule' ELSE 'human' END,
        'actor_id', p_changed_by,
        'actor_ref', NULL
      ),
      'subject_type', 'admin_ui_config_family',
      'subject_id', family || ':' || next_revision::text,
      'visibility_scope', jsonb_build_object('tenant_id', p_tenant_id),
      'evidence_refs', jsonb_build_array(jsonb_build_object(
        'evidence_type', 'event',
        'evidence_id', family || ':' || next_revision::text
      )),
      'payload', jsonb_build_object(
        'tenant_id', p_tenant_id,
        'family_key', family,
        'revision', next_revision,
        'content_hash', next_hash,
        'source', normalized_source,
        'metadata', normalized_metadata
      ),
      'trace_id', event_idempotency_key
    ),
    jsonb_build_object(
      'producer', 'postgres.admin_ui_config_family_revisions',
      'schema_version', '1.0.0'
    ),
    event_idempotency_key,
    event_idempotency_key,
    'pending',
    now()
  )
  ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'config.changed' DO NOTHING;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'count_base_anchor' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM count_base_anchors
      WHERE tenant_id = NEW.tenant_id
        AND base_count_anchor_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'count base anchor outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'shifting_event' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM shifting_events
      WHERE tenant_id = NEW.tenant_id
        AND shifting_event_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'shifting event outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_projection_exception' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM count_projection_exceptions
      WHERE tenant_id = NEW.tenant_id
        AND count_projection_exception_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'count projection exception outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'admin_ui_config_family' THEN
    config_family_key := COALESCE(NEW.payload->>'family_key', NEW.payload #>> '{payload,family_key}');
    IF NOT EXISTS (
      SELECT 1
      FROM admin_ui_config_family_revisions
      WHERE tenant_id = NEW.tenant_id
        AND family_key = config_family_key
    ) THEN
      RAISE EXCEPTION 'admin ui config family outbox aggregate % does not exist for tenant %', config_family_key, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

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

  IF NEW.aggregate_type = 'obligation_instance' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM obligation_instances
      WHERE tenant_id = NEW.tenant_id
        AND obligation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'obligation instance outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'protocol_version' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM protocol_versions
      WHERE tenant_id = NEW.tenant_id
        AND protocol_version_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'protocol version outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
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
CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.aggregate_type = 'count_base_anchor' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM count_base_anchors
      WHERE tenant_id = NEW.tenant_id
        AND base_count_anchor_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'count base anchor outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'shifting_event' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM shifting_events
      WHERE tenant_id = NEW.tenant_id
        AND shifting_event_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'shifting event outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'count_projection_exception' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM count_projection_exceptions
      WHERE tenant_id = NEW.tenant_id
        AND count_projection_exception_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'count projection exception outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'admin_ui_config_family' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM admin_ui_config_family_revisions
      WHERE tenant_id = NEW.tenant_id
        AND family_key = NEW.payload->>'family_key'
    ) THEN
      RAISE EXCEPTION 'admin ui config family outbox aggregate % does not exist for tenant %', NEW.payload->>'family_key', NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

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

  IF NEW.aggregate_type = 'obligation_instance' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM obligation_instances
      WHERE tenant_id = NEW.tenant_id
        AND obligation_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'obligation instance outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'protocol_version' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM protocol_versions
      WHERE tenant_id = NEW.tenant_id
        AND protocol_version_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'protocol version outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION admin_ui_emit_config_family_change(
  p_tenant_id uuid,
  p_family_key text,
  p_changed_by uuid DEFAULT NULL,
  p_source text DEFAULT 'system',
  p_metadata jsonb DEFAULT '{}'::jsonb
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  next_revision bigint;
  next_hash text;
  event_uuid uuid;
  event_idempotency_key text;
  family text := btrim(COALESCE(p_family_key, ''));
  normalized_source text := COALESCE(NULLIF(btrim(p_source), ''), 'system');
  normalized_metadata jsonb := COALESCE(p_metadata, '{}'::jsonb);
BEGIN
  IF p_tenant_id IS NULL OR family = '' THEN
    RETURN;
  END IF;

  INSERT INTO admin_ui_config_family_revisions (
    tenant_id,
    family_key,
    revision,
    content_hash,
    changed_at,
    changed_by,
    source,
    metadata
  ) VALUES (
    p_tenant_id,
    family,
    1,
    md5(p_tenant_id::text || ':' || family || ':1:' || clock_timestamp()::text),
    now(),
    p_changed_by,
    normalized_source,
    normalized_metadata
  )
  ON CONFLICT (tenant_id, family_key) DO UPDATE
  SET revision = admin_ui_config_family_revisions.revision + 1,
      content_hash = md5(
        EXCLUDED.tenant_id::text || ':' ||
        EXCLUDED.family_key || ':' ||
        (admin_ui_config_family_revisions.revision + 1)::text || ':' ||
        clock_timestamp()::text
      ),
      changed_at = now(),
      changed_by = EXCLUDED.changed_by,
      source = EXCLUDED.source,
      metadata = EXCLUDED.metadata
  RETURNING revision, content_hash
    INTO next_revision, next_hash;

  event_uuid := admin_ui_deterministic_config_event_uuid(p_tenant_id, family, next_revision);
  event_idempotency_key := 'admin-ui-config:' || p_tenant_id::text || ':' || family || ':' || next_revision::text;

  INSERT INTO outbox_messages (
    tenant_id,
    event_id,
    event_type,
    schema_version,
    aggregate_type,
    aggregate_id,
    topic,
    payload,
    headers,
    idempotency_key,
    trace_id,
    status,
    next_attempt_at
  ) VALUES (
    p_tenant_id,
    event_uuid,
    'config.changed',
    'v1',
    'admin_ui_config_family',
    p_tenant_id,
    'config.changed',
    jsonb_build_object(
      'tenant_id', p_tenant_id,
      'family_key', family,
      'revision', next_revision,
      'content_hash', next_hash,
      'source', normalized_source,
      'metadata', normalized_metadata
    ),
    jsonb_build_object('producer', 'postgres.admin_ui_config_family_revisions'),
    event_idempotency_key,
    NULL,
    'pending',
    now()
  )
  ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'config.changed' DO NOTHING;
END;
$$;
-- +goose StatementEnd
