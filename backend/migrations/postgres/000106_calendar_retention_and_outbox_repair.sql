-- +goose Up
-- +goose StatementBegin
-- Keep Calendar as a hot operational projection, repair the 000105 admin-config
-- coalescing shape for DBs that may already have applied the earlier version,
-- and make vaccination completion a first-class outbox event.

CREATE TABLE IF NOT EXISTS admin_ui_config_family_change_queue (
  transaction_id bigint NOT NULL,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  family_key text NOT NULL,
  changed_by uuid NULL,
  source text NOT NULL DEFAULT 'system',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  change_count int NOT NULL DEFAULT 1,
  queued_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (transaction_id, tenant_id, family_key),
  CONSTRAINT admin_ui_config_family_change_queue_family_check CHECK (btrim(family_key) <> ''),
  CONSTRAINT admin_ui_config_family_change_queue_source_check CHECK (btrim(source) <> ''),
  CONSTRAINT admin_ui_config_family_change_queue_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT admin_ui_config_family_change_queue_count_check CHECK (change_count >= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_config_changed_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'config.changed';

CREATE UNIQUE INDEX IF NOT EXISTS outbox_messages_vaccination_completed_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'vaccination.completed';

CREATE OR REPLACE FUNCTION validate_outbox_event_tenant()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
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

CREATE OR REPLACE FUNCTION admin_ui_deterministic_config_event_uuid(
  p_tenant_id uuid,
  p_family_key text,
  p_revision bigint
) RETURNS uuid
LANGUAGE sql
IMMUTABLE
AS $$
  WITH digest AS (
    SELECT md5(
      'admin-ui-config:' ||
      p_tenant_id::text || ':' ||
      btrim(COALESCE(p_family_key, '')) || ':' ||
      COALESCE(p_revision, 0)::text
    ) AS h
  )
  SELECT (
    substr(h, 1, 8) || '-' ||
    substr(h, 9, 4) || '-' ||
    substr(h, 13, 4) || '-' ||
    substr(h, 17, 4) || '-' ||
    substr(h, 21, 12)
  )::uuid
  FROM digest
$$;

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

CREATE OR REPLACE FUNCTION admin_ui_bump_config_family(
  p_tenant_id uuid,
  p_family_key text,
  p_changed_by uuid DEFAULT NULL,
  p_source text DEFAULT 'system',
  p_metadata jsonb DEFAULT '{}'::jsonb
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  family text := btrim(COALESCE(p_family_key, ''));
  normalized_source text := COALESCE(NULLIF(btrim(p_source), ''), 'system');
  normalized_metadata jsonb := COALESCE(p_metadata, '{}'::jsonb);
BEGIN
  IF p_tenant_id IS NULL OR family = '' THEN
    RETURN;
  END IF;

  INSERT INTO admin_ui_config_family_change_queue (
    transaction_id,
    tenant_id,
    family_key,
    changed_by,
    source,
    metadata,
    change_count
  ) VALUES (
    txid_current(),
    p_tenant_id,
    family,
    p_changed_by,
    normalized_source,
    normalized_metadata,
    1
  )
  ON CONFLICT (transaction_id, tenant_id, family_key) DO UPDATE
  SET changed_by = COALESCE(EXCLUDED.changed_by, admin_ui_config_family_change_queue.changed_by),
      source = CASE
        WHEN admin_ui_config_family_change_queue.source = EXCLUDED.source THEN admin_ui_config_family_change_queue.source
        ELSE 'coalesced'
      END,
      metadata = EXCLUDED.metadata,
      change_count = admin_ui_config_family_change_queue.change_count + 1,
      updated_at = now();
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_flush_config_family_change_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  queued admin_ui_config_family_change_queue%ROWTYPE;
  flush_metadata jsonb;
BEGIN
  DELETE FROM admin_ui_config_family_change_queue
  WHERE transaction_id = NEW.transaction_id
    AND tenant_id = NEW.tenant_id
    AND family_key = NEW.family_key
  RETURNING * INTO queued;

  IF NOT FOUND THEN
    RETURN NULL;
  END IF;

  flush_metadata := queued.metadata || jsonb_build_object('coalesced_change_count', queued.change_count);
  PERFORM admin_ui_emit_config_family_change(
    queued.tenant_id,
    queued.family_key,
    queued.changed_by,
    queued.source,
    flush_metadata
  );

  RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS admin_ui_config_family_change_queue_flush_trg ON admin_ui_config_family_change_queue;

CREATE CONSTRAINT TRIGGER admin_ui_config_family_change_queue_flush_trg
  AFTER INSERT OR UPDATE ON admin_ui_config_family_change_queue
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION admin_ui_flush_config_family_change_trg();

CREATE TABLE IF NOT EXISTS calendar_event_identities (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  event_id text NOT NULL,
  slice_key text NOT NULL DEFAULT 'vaccination',
  source_target_type text NULL,
  source_target_id uuid NULL,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, event_id),
  CONSTRAINT calendar_event_identities_slice_check CHECK (slice_key IN ('vaccination')),
  CONSTRAINT calendar_event_identities_event_id_check CHECK (btrim(event_id) <> '')
);

INSERT INTO calendar_event_identities (
  tenant_id,
  event_id,
  slice_key,
  source_target_type,
  source_target_id,
  first_seen_at,
  last_seen_at
)
SELECT
  tenant_id,
  event_id,
  slice_key,
  source_target_type,
  source_target_id,
  created_at,
  updated_at
FROM calendar_event_projections
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET slice_key = EXCLUDED.slice_key,
    source_target_type = EXCLUDED.source_target_type,
    source_target_id = EXCLUDED.source_target_id,
    last_seen_at = GREATEST(calendar_event_identities.last_seen_at, EXCLUDED.last_seen_at);

INSERT INTO calendar_event_identities (tenant_id, event_id, slice_key, first_seen_at, last_seen_at)
SELECT tenant_id, calendar_event_id, 'vaccination', min(created_at), max(updated_at)
FROM notification_requests
GROUP BY tenant_id, calendar_event_id
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET last_seen_at = GREATEST(calendar_event_identities.last_seen_at, EXCLUDED.last_seen_at);

INSERT INTO calendar_event_identities (tenant_id, event_id, slice_key, first_seen_at, last_seen_at)
SELECT tenant_id, calendar_event_id, 'vaccination', min(created_at), max(created_at)
FROM calendar_snoozes
GROUP BY tenant_id, calendar_event_id
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET last_seen_at = GREATEST(calendar_event_identities.last_seen_at, EXCLUDED.last_seen_at);

CREATE OR REPLACE FUNCTION calendar_event_projection_identity_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  INSERT INTO calendar_event_identities (
    tenant_id,
    event_id,
    slice_key,
    source_target_type,
    source_target_id,
    first_seen_at,
    last_seen_at
  ) VALUES (
    NEW.tenant_id,
    NEW.event_id,
    NEW.slice_key,
    NEW.source_target_type,
    NEW.source_target_id,
    COALESCE(NEW.created_at, now()),
    now()
  )
  ON CONFLICT (tenant_id, event_id) DO UPDATE
  SET slice_key = EXCLUDED.slice_key,
      source_target_type = EXCLUDED.source_target_type,
      source_target_id = EXCLUDED.source_target_id,
      last_seen_at = now();

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS calendar_event_projections_identity_trg ON calendar_event_projections;

CREATE TRIGGER calendar_event_projections_identity_trg
  BEFORE INSERT OR UPDATE OF slice_key, source_target_type, source_target_id ON calendar_event_projections
  FOR EACH ROW EXECUTE FUNCTION calendar_event_projection_identity_trg();

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_event_fk;
ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_event_identity_fk;
ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_event_identity_fk
  FOREIGN KEY (tenant_id, calendar_event_id)
  REFERENCES calendar_event_identities(tenant_id, event_id)
  NOT VALID;
ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_event_identity_fk;

ALTER TABLE calendar_snoozes
  DROP CONSTRAINT IF EXISTS calendar_snoozes_event_fk;
ALTER TABLE calendar_snoozes
  DROP CONSTRAINT IF EXISTS calendar_snoozes_event_identity_fk;
ALTER TABLE calendar_snoozes
  ADD CONSTRAINT calendar_snoozes_event_identity_fk
  FOREIGN KEY (tenant_id, calendar_event_id)
  REFERENCES calendar_event_identities(tenant_id, event_id)
  NOT VALID;
ALTER TABLE calendar_snoozes
  VALIDATE CONSTRAINT calendar_snoozes_event_identity_fk;

CREATE INDEX IF NOT EXISTS calendar_event_projections_closed_prune_idx
  ON calendar_event_projections (tenant_id, slice_key, due_at, event_id)
  WHERE system = false
    AND status IN ('completed', 'canceled')
    AND due_at IS NOT NULL;

CREATE OR REPLACE FUNCTION calendar_prune_closed_vaccination_projection(
  p_tenant_id uuid,
  p_cutoff timestamptz DEFAULT now() - interval '90 days',
  p_limit int DEFAULT 1000
) RETURNS int
LANGUAGE plpgsql
AS $$
DECLARE
  deleted_count int;
BEGIN
  WITH doomed AS (
    SELECT ctid
    FROM calendar_event_projections
    WHERE tenant_id = p_tenant_id
      AND slice_key = 'vaccination'
      AND system = false
      AND status IN ('completed', 'canceled')
      AND due_at < p_cutoff
    ORDER BY due_at ASC, event_id ASC
    LIMIT GREATEST(1, LEAST(COALESCE(p_limit, 1000), 1000))
  ),
  deleted AS (
    DELETE FROM calendar_event_projections cep
    USING doomed
    WHERE cep.ctid = doomed.ctid
    RETURNING 1
  )
  SELECT count(*)::int INTO deleted_count
  FROM deleted;

  RETURN COALESCE(deleted_count, 0);
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- This migration is intentionally defensive. Rolling it back would reintroduce
-- unsafe FK coupling and admin-config drift on databases that needed the repair.
SELECT 1;
-- +goose StatementEnd
