-- +goose Up
-- Admin-web bootstrap stable config values and revision ledger.
--
-- Postgres remains the canonical source of configurable display values. The
-- entries table carries stable key/value UI config that rarely changes (nav
-- titles, page titles, copy, table labels, option/chip labels). Live tenant data
-- still stays in its owning tables and is compiled separately. The revision
-- table is only an invalidation pointer used by the backend contract compiler
-- and cache keys so config writes miss compiled admin-web caches immediately.

CREATE TABLE admin_ui_config_entries (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  locale text NOT NULL DEFAULT 'default',
  route_id text NOT NULL DEFAULT '',
  config_key text NOT NULL,
  config_value text NOT NULL,
  value_kind text NOT NULL DEFAULT 'text',
  status text NOT NULL DEFAULT 'active',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, locale, route_id, config_key),
  CONSTRAINT admin_ui_config_entries_locale_check CHECK (btrim(locale) <> ''),
  CONSTRAINT admin_ui_config_entries_key_check CHECK (btrim(config_key) <> ''),
  CONSTRAINT admin_ui_config_entries_kind_check CHECK (value_kind IN ('text', 'label', 'title', 'copy', 'tone', 'disabled_reason')),
  CONSTRAINT admin_ui_config_entries_status_check CHECK (status IN ('active', 'retired')),
  CONSTRAINT admin_ui_config_entries_row_version_check CHECK (row_version >= 1),
  CONSTRAINT admin_ui_config_entries_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX admin_ui_config_entries_route_idx
  ON admin_ui_config_entries (tenant_id, locale, route_id, status, updated_at DESC);

CREATE TABLE admin_ui_config_family_revisions (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  family_key text NOT NULL,
  revision bigint NOT NULL DEFAULT 1,
  content_hash text NOT NULL DEFAULT '',
  changed_at timestamptz NOT NULL DEFAULT now(),
  changed_by uuid NULL,
  source text NOT NULL DEFAULT 'system',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (tenant_id, family_key),
  CONSTRAINT admin_ui_config_family_key_check CHECK (btrim(family_key) <> ''),
  CONSTRAINT admin_ui_config_family_revision_check CHECK (revision >= 1),
  CONSTRAINT admin_ui_config_family_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX admin_ui_config_family_changed_idx
  ON admin_ui_config_family_revisions (tenant_id, changed_at DESC, family_key);

CREATE TABLE admin_ui_config_family_change_queue (
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

CREATE UNIQUE INDEX outbox_messages_config_changed_idempotency_idx
  ON outbox_messages (tenant_id, idempotency_key)
  WHERE event_type = 'config.changed';

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

CREATE CONSTRAINT TRIGGER admin_ui_config_family_change_queue_flush_trg
  AFTER INSERT OR UPDATE ON admin_ui_config_family_change_queue
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION admin_ui_flush_config_family_change_trg();

CREATE OR REPLACE FUNCTION admin_ui_bump_config_family_for_all_tenants(
  p_family_key text,
  p_source text DEFAULT 'system',
  p_metadata jsonb DEFAULT '{}'::jsonb
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  tenant_row record;
BEGIN
  FOR tenant_row IN SELECT tenant_id FROM tenants LOOP
    PERFORM admin_ui_bump_config_family(
      tenant_row.tenant_id,
      p_family_key,
      NULL,
      p_source,
      p_metadata
    );
  END LOOP;
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_bump_row_family_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  family text := TG_ARGV[0];
  row_tenant uuid;
BEGIN
  IF TG_OP = 'DELETE' THEN
    row_tenant := OLD.tenant_id;
  ELSE
    row_tenant := NEW.tenant_id;
  END IF;
  PERFORM admin_ui_bump_config_family(
    row_tenant,
    family,
    NULL,
    TG_TABLE_NAME || '.' || lower(TG_OP),
    jsonb_build_object('table', TG_TABLE_NAME, 'operation', TG_OP)
  );

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_bump_global_family_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  family text := TG_ARGV[0];
BEGIN
  PERFORM admin_ui_bump_config_family_for_all_tenants(
    family,
    TG_TABLE_NAME || '.' || lower(TG_OP),
    jsonb_build_object('table', TG_TABLE_NAME, 'operation', TG_OP)
  );

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_bump_status_family_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  old_axis text;
  new_axis text;
  source text := TG_TABLE_NAME || '.' || lower(TG_OP);
  metadata jsonb := jsonb_build_object('table', TG_TABLE_NAME, 'operation', TG_OP);
BEGIN
  IF TG_OP <> 'INSERT' THEN
    old_axis := NULLIF(COALESCE(OLD.axis, ''), '');
  END IF;
  IF TG_OP <> 'DELETE' THEN
    new_axis := NULLIF(COALESCE(NEW.axis, ''), '');
  END IF;

  IF old_axis IS NOT NULL THEN
    PERFORM admin_ui_bump_config_family_for_all_tenants('status:' || old_axis, source, metadata);
  END IF;
  IF new_axis IS NOT NULL AND new_axis IS DISTINCT FROM old_axis THEN
    PERFORM admin_ui_bump_config_family_for_all_tenants('status:' || new_axis, source, metadata);
  END IF;
  PERFORM admin_ui_bump_config_family_for_all_tenants('config', source, metadata);

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_bump_protocol_family_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  row_tenant uuid;
  protocol_version uuid;
  protocol_ref uuid;
  old_category text;
  new_category text;
  source text := TG_TABLE_NAME || '.' || lower(TG_OP);
  metadata jsonb := jsonb_build_object('table', TG_TABLE_NAME, 'operation', TG_OP);
BEGIN
  IF TG_OP = 'DELETE' THEN
    row_tenant := OLD.tenant_id;
  ELSE
    row_tenant := NEW.tenant_id;
  END IF;

  IF TG_TABLE_NAME = 'protocol_definitions' THEN
    IF TG_OP <> 'INSERT' THEN
      old_category := NULLIF(COALESCE(OLD.category, ''), '');
    END IF;
    IF TG_OP <> 'DELETE' THEN
      new_category := NULLIF(COALESCE(NEW.category, ''), '');
    END IF;
  ELSIF TG_TABLE_NAME = 'protocol_versions' THEN
    IF TG_OP = 'DELETE' THEN
      protocol_ref := OLD.protocol_id;
    ELSE
      protocol_ref := NEW.protocol_id;
    END IF;
    SELECT category INTO new_category
    FROM protocol_definitions
    WHERE tenant_id = row_tenant
      AND protocol_id = protocol_ref;
  ELSE
    IF TG_OP = 'DELETE' THEN
      protocol_version := OLD.protocol_version_id;
    ELSE
      protocol_version := NEW.protocol_version_id;
    END IF;
    SELECT pd.category INTO new_category
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = row_tenant
      AND pv.protocol_version_id = protocol_version;
  END IF;

  IF old_category IS NOT NULL THEN
    PERFORM admin_ui_bump_config_family(row_tenant, 'protocols:' || old_category, NULL, source, metadata);
  END IF;
  IF new_category IS NOT NULL AND new_category IS DISTINCT FROM old_category THEN
    PERFORM admin_ui_bump_config_family(row_tenant, 'protocols:' || new_category, NULL, source, metadata);
  END IF;
  PERFORM admin_ui_bump_config_family(row_tenant, 'config', NULL, source, metadata);

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_sop_family_key(sop_code text)
RETURNS text
LANGUAGE sql
IMMUTABLE
AS $$
  SELECT CASE
    WHEN btrim(COALESCE(sop_code, '')) = '' THEN 'sops'
    WHEN strpos(btrim(sop_code), '.') > 0 THEN 'sops:' || split_part(btrim(sop_code), '.', 1)
    ELSE 'sops'
  END
$$;

CREATE OR REPLACE FUNCTION admin_ui_bump_sop_family_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  row_tenant uuid;
  sop_ref uuid;
  old_family text;
  new_family text;
  source text := TG_TABLE_NAME || '.' || lower(TG_OP);
  metadata jsonb := jsonb_build_object('table', TG_TABLE_NAME, 'operation', TG_OP);
BEGIN
  IF TG_OP = 'DELETE' THEN
    row_tenant := OLD.tenant_id;
  ELSE
    row_tenant := NEW.tenant_id;
  END IF;

  IF TG_TABLE_NAME = 'sop_definitions' THEN
    IF TG_OP <> 'INSERT' THEN
      old_family := admin_ui_sop_family_key(OLD.code);
    END IF;
    IF TG_OP <> 'DELETE' THEN
      new_family := admin_ui_sop_family_key(NEW.code);
    END IF;
  ELSE
    IF TG_OP = 'DELETE' THEN
      sop_ref := OLD.sop_id;
    ELSE
      sop_ref := NEW.sop_id;
    END IF;
    SELECT admin_ui_sop_family_key(code) INTO new_family
    FROM sop_definitions
    WHERE tenant_id = row_tenant
      AND sop_id = sop_ref;
  END IF;

  IF old_family IS NOT NULL THEN
    PERFORM admin_ui_bump_config_family(row_tenant, old_family, NULL, source, metadata);
  END IF;
  IF new_family IS NOT NULL AND new_family IS DISTINCT FROM old_family THEN
    PERFORM admin_ui_bump_config_family(row_tenant, new_family, NULL, source, metadata);
  END IF;
  PERFORM admin_ui_bump_config_family(row_tenant, 'config', NULL, source, metadata);

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION admin_ui_bump_feed_item_family_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  row_tenant uuid;
  old_category text;
  new_category text;
  source text := TG_TABLE_NAME || '.' || lower(TG_OP);
  metadata jsonb := jsonb_build_object('table', TG_TABLE_NAME, 'operation', TG_OP);
BEGIN
  IF TG_OP = 'DELETE' THEN
    row_tenant := OLD.tenant_id;
  ELSE
    row_tenant := NEW.tenant_id;
  END IF;
  IF TG_OP <> 'INSERT' THEN
    old_category := COALESCE(OLD.category, '');
  END IF;
  IF TG_OP <> 'DELETE' THEN
    new_category := COALESCE(NEW.category, '');
  END IF;

  IF new_category = 'feed' OR old_category = 'feed' THEN
    PERFORM admin_ui_bump_config_family(row_tenant, 'feed-items', NULL, source, metadata);
    PERFORM admin_ui_bump_config_family(row_tenant, 'config', NULL, source, metadata);
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER admin_ui_locations_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON locations
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('locations');

CREATE TRIGGER admin_ui_config_entries_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON admin_ui_config_entries
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('admin-ui-config');

CREATE TRIGGER admin_ui_animal_stages_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON animal_stage_lookup
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('animal-stages');

CREATE TRIGGER admin_ui_animal_stages_protocol_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON animal_stage_lookup
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('protocols:vaccination');

CREATE TRIGGER admin_ui_animal_stages_config_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON animal_stage_lookup
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('config');

CREATE TRIGGER admin_ui_user_scope_grants_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON user_scope_grants
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('permissions');

CREATE TRIGGER admin_ui_pending_email_grants_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON auth_pending_email_grants
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_row_family_trg('permissions');

CREATE TRIGGER admin_ui_breeds_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON breeds
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_global_family_trg('breeds');

CREATE TRIGGER admin_ui_status_definitions_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON status_definitions
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_status_family_trg();

CREATE TRIGGER admin_ui_protocol_definitions_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON protocol_definitions
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_protocol_family_trg();

CREATE TRIGGER admin_ui_protocol_versions_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON protocol_versions
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_protocol_family_trg();

CREATE TRIGGER admin_ui_protocol_rules_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON protocol_rules
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_protocol_family_trg();

CREATE TRIGGER admin_ui_protocol_triggers_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON protocol_triggers
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_protocol_family_trg();

CREATE TRIGGER admin_ui_sop_definitions_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON sop_definitions
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_sop_family_trg();

CREATE TRIGGER admin_ui_sop_versions_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON sop_versions
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_sop_family_trg();

CREATE TRIGGER admin_ui_inventory_feed_items_revision_trg
  AFTER INSERT OR UPDATE OR DELETE ON inventory_items
  FOR EACH ROW EXECUTE FUNCTION admin_ui_bump_feed_item_family_trg();

-- +goose Down
DROP TRIGGER IF EXISTS admin_ui_config_entries_revision_trg ON admin_ui_config_entries;
DROP TRIGGER IF EXISTS admin_ui_inventory_feed_items_revision_trg ON inventory_items;
DROP TRIGGER IF EXISTS admin_ui_sop_versions_revision_trg ON sop_versions;
DROP TRIGGER IF EXISTS admin_ui_sop_definitions_revision_trg ON sop_definitions;
DROP TRIGGER IF EXISTS admin_ui_protocol_triggers_revision_trg ON protocol_triggers;
DROP TRIGGER IF EXISTS admin_ui_protocol_rules_revision_trg ON protocol_rules;
DROP TRIGGER IF EXISTS admin_ui_protocol_versions_revision_trg ON protocol_versions;
DROP TRIGGER IF EXISTS admin_ui_protocol_definitions_revision_trg ON protocol_definitions;
DROP TRIGGER IF EXISTS admin_ui_status_definitions_revision_trg ON status_definitions;
DROP TRIGGER IF EXISTS admin_ui_breeds_revision_trg ON breeds;
DROP TRIGGER IF EXISTS admin_ui_pending_email_grants_revision_trg ON auth_pending_email_grants;
DROP TRIGGER IF EXISTS admin_ui_user_scope_grants_revision_trg ON user_scope_grants;
DROP TRIGGER IF EXISTS admin_ui_animal_stages_config_revision_trg ON animal_stage_lookup;
DROP TRIGGER IF EXISTS admin_ui_animal_stages_protocol_revision_trg ON animal_stage_lookup;
DROP TRIGGER IF EXISTS admin_ui_animal_stages_revision_trg ON animal_stage_lookup;
DROP TRIGGER IF EXISTS admin_ui_locations_revision_trg ON locations;

DROP FUNCTION IF EXISTS admin_ui_bump_feed_item_family_trg();
DROP FUNCTION IF EXISTS admin_ui_bump_sop_family_trg();
DROP FUNCTION IF EXISTS admin_ui_sop_family_key(text);
DROP FUNCTION IF EXISTS admin_ui_bump_protocol_family_trg();
DROP FUNCTION IF EXISTS admin_ui_bump_status_family_trg();
DROP FUNCTION IF EXISTS admin_ui_bump_global_family_trg();
DROP FUNCTION IF EXISTS admin_ui_bump_row_family_trg();
DROP FUNCTION IF EXISTS admin_ui_bump_config_family_for_all_tenants(text, text, jsonb);
DROP TRIGGER IF EXISTS admin_ui_config_family_change_queue_flush_trg ON admin_ui_config_family_change_queue;
DROP FUNCTION IF EXISTS admin_ui_bump_config_family(uuid, text, uuid, text, jsonb);
DROP FUNCTION IF EXISTS admin_ui_flush_config_family_change_trg();
DROP FUNCTION IF EXISTS admin_ui_emit_config_family_change(uuid, text, uuid, text, jsonb);
DROP FUNCTION IF EXISTS admin_ui_deterministic_config_event_uuid(uuid, text, bigint);
DROP TABLE IF EXISTS admin_ui_config_family_change_queue;
DROP INDEX IF EXISTS outbox_messages_config_changed_idempotency_idx;

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

DROP INDEX IF EXISTS admin_ui_config_family_changed_idx;
DROP TABLE IF EXISTS admin_ui_config_family_revisions;
DROP INDEX IF EXISTS admin_ui_config_entries_route_idx;
DROP TABLE IF EXISTS admin_ui_config_entries;
