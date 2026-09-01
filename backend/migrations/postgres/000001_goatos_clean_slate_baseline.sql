-- +goose Up
-- +goose NO TRANSACTION

-- Collapsed clean-slate baseline generated from migrations 000001..000046.
-- This repository seeds disposable fresh environments, so historical deltas are folded into one baseline.


-- -----------------------------------------------------------------------------
-- Collapsed from 000001_goatos_clean_slate_baseline.sql
-- -----------------------------------------------------------------------------
-- GoatOS clean-slate baseline generated from the verified final schema at 44ed6330.
-- This disposable-project baseline replaces the historical incremental migration chain.

-- baseline: pre-data schema
--
-- PostgreSQL database dump
--

-- Dumped from database version 16.9
-- Dumped by pg_dump version 16.9

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: analytics; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA analytics;

--
-- Name: btree_gist; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;

--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

--
-- Name: admin_ui_bump_config_family(uuid, text, uuid, text, jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_config_family(p_tenant_id uuid, p_family_key text, p_changed_by uuid DEFAULT NULL::uuid, p_source text DEFAULT 'system'::text, p_metadata jsonb DEFAULT '{}'::jsonb) RETURNS void
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

--
-- Name: admin_ui_bump_config_family_for_all_tenants(text, text, jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_config_family_for_all_tenants(p_family_key text, p_source text DEFAULT 'system'::text, p_metadata jsonb DEFAULT '{}'::jsonb) RETURNS void
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

--
-- Name: admin_ui_bump_feed_item_family_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_feed_item_family_trg() RETURNS trigger
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

--
-- Name: admin_ui_bump_global_family_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_global_family_trg() RETURNS trigger
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

--
-- Name: admin_ui_bump_protocol_family_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_protocol_family_trg() RETURNS trigger
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

--
-- Name: admin_ui_bump_row_family_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_row_family_trg() RETURNS trigger
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

--
-- Name: admin_ui_bump_sop_family_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_sop_family_trg() RETURNS trigger
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

--
-- Name: admin_ui_bump_status_family_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_bump_status_family_trg() RETURNS trigger
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

--
-- Name: admin_ui_deterministic_config_event_uuid(uuid, text, bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_deterministic_config_event_uuid(p_tenant_id uuid, p_family_key text, p_revision bigint) RETURNS uuid
    LANGUAGE sql IMMUTABLE
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

--
-- Name: admin_ui_emit_config_family_change(uuid, text, uuid, text, jsonb); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_emit_config_family_change(p_tenant_id uuid, p_family_key text, p_changed_by uuid DEFAULT NULL::uuid, p_source text DEFAULT 'system'::text, p_metadata jsonb DEFAULT '{}'::jsonb) RETURNS void
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

--
-- Name: admin_ui_flush_config_family_change_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_flush_config_family_change_trg() RETURNS trigger
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

--
-- Name: admin_ui_sop_family_key(text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.admin_ui_sop_family_key(sop_code text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
  SELECT CASE
    WHEN btrim(COALESCE(sop_code, '')) = '' THEN 'sops'
    WHEN strpos(btrim(sop_code), '.') > 0 THEN 'sops:' || split_part(btrim(sop_code), '.', 1)
    ELSE 'sops'
  END
$$;

--
-- Name: block_active_vaccination_for_procurement_excluded_goat(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.block_active_vaccination_for_procurement_excluded_goat() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  excluded_reason text;
  is_vaccination boolean;
BEGIN
  IF NEW.target_type <> 'goat'
     OR NEW.status NOT IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed', 'waived') THEN
    RETURN NEW;
  END IF;

  SELECT EXISTS (
    SELECT 1
    FROM protocol_versions pv
    JOIN protocol_definitions pd
      ON pd.tenant_id = pv.tenant_id
     AND pd.protocol_id = pv.protocol_id
    WHERE pv.tenant_id = NEW.tenant_id
      AND pv.protocol_version_id = NEW.protocol_version_id
      AND pd.category = 'vaccination'
  ) INTO is_vaccination;

  IF NOT is_vaccination THEN
    RETURN NEW;
  END IF;

  SELECT exclusion_reason
    INTO excluded_reason
    FROM vw_procurement_vaccination_excluded_goats ex
    WHERE ex.tenant_id = NEW.tenant_id
      AND ex.goat_id = NEW.target_id
    LIMIT 1;

  IF excluded_reason IS NOT NULL THEN
    RAISE EXCEPTION 'vaccination_obligation_blocked_for_procurement_excluded_goat: goat %, reason %', NEW.target_id, excluded_reason
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: block_merged_goat_child_write(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.block_merged_goat_child_write() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  target_redirect uuid;
BEGIN
  IF COALESCE(current_setting('goatos.allow_merged_goat_child_write', true), 'off') = 'on' THEN
    RETURN NEW;
  END IF;

  IF NEW.goat_id IS NULL THEN
    RETURN NEW;
  END IF;

  SELECT merged_into_goat_id
    INTO target_redirect
    FROM goats
    WHERE tenant_id = NEW.tenant_id
      AND goat_id = NEW.goat_id;

  IF target_redirect IS NOT NULL THEN
    RAISE EXCEPTION 'merged_goat_child_write_blocked: goat % redirects to %', NEW.goat_id, target_redirect
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: check_goat_active_ownership_total(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.check_goat_active_ownership_total() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  affected_goat_ids uuid[];
  affected_goat_id uuid;
  total_bps int;
BEGIN
  IF TG_OP = 'INSERT' THEN
    affected_goat_ids := ARRAY[NEW.goat_id];
  ELSIF TG_OP = 'DELETE' THEN
    affected_goat_ids := ARRAY[OLD.goat_id];
  ELSE
    affected_goat_ids := ARRAY[OLD.goat_id];
    IF NEW.goat_id IS DISTINCT FROM OLD.goat_id THEN
      affected_goat_ids := affected_goat_ids || NEW.goat_id;
    END IF;
  END IF;

  FOREACH affected_goat_id IN ARRAY affected_goat_ids LOOP
    SELECT COALESCE(sum(share_bps), 0)
      INTO total_bps
      FROM goat_ownership
      WHERE goat_id = affected_goat_id
        AND status = 'active'
        AND valid_to IS NULL;

    IF total_bps <> 10000 THEN
      RAISE EXCEPTION 'active ownership shares for goat % sum to %, expected 10000', affected_goat_id, total_bps
        USING ERRCODE = '23514';
    END IF;
  END LOOP;

  RETURN COALESCE(NEW, OLD);
END;
$$;

--
-- Name: ensure_protocol_child_version_is_draft(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.ensure_protocol_child_version_is_draft() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  parent_status text;
  child_tenant_id uuid;
  child_version_id uuid;
BEGIN
  IF TG_OP = 'DELETE' THEN
    child_tenant_id := OLD.tenant_id;
    child_version_id := OLD.protocol_version_id;
  ELSE
    child_tenant_id := NEW.tenant_id;
    child_version_id := NEW.protocol_version_id;
  END IF;

  SELECT status
  INTO parent_status
  FROM protocol_versions
  WHERE tenant_id = child_tenant_id
    AND protocol_version_id = child_version_id;

  IF parent_status IS NULL THEN
    RAISE EXCEPTION 'protocol version % does not exist for tenant %', child_version_id, child_tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF parent_status <> 'draft' THEN
    RAISE EXCEPTION 'protocol version % is %, not draft; published config is immutable',
      child_version_id, parent_status
      USING ERRCODE = '23514';
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

--
-- Name: ensure_published_protocol_version_is_immutable(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.ensure_published_protocol_version_is_immutable() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF OLD.status = 'published' THEN
    IF NEW.status NOT IN ('published', 'retired') THEN
      RAISE EXCEPTION 'published protocol version % cannot move back to status %',
        OLD.protocol_version_id, NEW.status
        USING ERRCODE = '23514';
    END IF;

    IF NEW.protocol_id IS DISTINCT FROM OLD.protocol_id
      OR NEW.scope_type IS DISTINCT FROM OLD.scope_type
      OR NEW.scope_id IS DISTINCT FROM OLD.scope_id
      OR NEW.version IS DISTINCT FROM OLD.version
      OR NEW.version_label IS DISTINCT FROM OLD.version_label
      OR NEW.effective_from IS DISTINCT FROM OLD.effective_from
      OR NEW.effective_to IS DISTINCT FROM OLD.effective_to
      OR NEW.rule_dsl IS DISTINCT FROM OLD.rule_dsl
      OR NEW.proof_policy IS DISTINCT FROM OLD.proof_policy
      OR NEW.sop_version_id IS DISTINCT FROM OLD.sop_version_id
      OR NEW.drafted_by IS DISTINCT FROM OLD.drafted_by
      OR NEW.published_by IS DISTINCT FROM OLD.published_by
      OR NEW.published_at IS DISTINCT FROM OLD.published_at THEN
      RAISE EXCEPTION 'published protocol version % is immutable; create a new version for config changes',
        OLD.protocol_version_id
        USING ERRCODE = '23514';
    END IF;
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: goatos_calendar_event_reference_valid(uuid, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.goatos_calendar_event_reference_valid(p_tenant_id uuid, p_calendar_event_id text) RETURNS boolean
    LANGUAGE plpgsql STABLE
    AS $_$
DECLARE
  m text[];
  parsed_uuid uuid;
  parsed_date date;
BEGIN
  IF p_calendar_event_id IS NULL THEN
    RETURN true;
  END IF;

  -- obligation:<uuid> -- individual vaccination_dose_due obligation events.
  m := regexp_match(p_calendar_event_id, '^obligation:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM obligation_instances oi
        WHERE oi.tenant_id = p_tenant_id AND oi.obligation_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- batch:<uuid>
  m := regexp_match(p_calendar_event_id, '^batch:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM obligation_batches ob
        WHERE ob.tenant_id = p_tenant_id AND ob.batch_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- completion:<uuid>
  m := regexp_match(p_calendar_event_id, '^completion:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM vaccination_completions vc
        WHERE vc.tenant_id = p_tenant_id AND vc.completion_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- calendar:<uuid>
  m := regexp_match(p_calendar_event_id, '^calendar:([0-9a-fA-F-]{36})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      RETURN EXISTS (
        SELECT 1 FROM sop_tasks st WHERE st.tenant_id = p_tenant_id AND st.task_id = parsed_uuid
      ) OR EXISTS (
        SELECT 1 FROM protocol_versions pv WHERE pv.tenant_id = p_tenant_id AND pv.protocol_version_id = parsed_uuid
      );
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- parkdrive:park:<uuid>:date:<day>
  m := regexp_match(p_calendar_event_id, '^parkdrive:park:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, parsed_uuid, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- parkdrive:tenant:<uuid>:date:<day>
  m := regexp_match(p_calendar_event_id, '^parkdrive:tenant:([0-9a-fA-F-]{36}):date:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:park:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:park:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, parsed_uuid, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:tenant:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[2]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, parsed_date);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:shed:<uuid>:rule:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:shed:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_uuid := m[1]::uuid;
      parsed_date := m[3]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, parsed_uuid, parsed_date, m[2]::uuid);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- catchup:tenant:<uuid>:rule:<uuid>:due:<day>
  m := regexp_match(p_calendar_event_id, '^catchup:tenant:([0-9a-fA-F-]{36}):rule:([0-9a-fA-F-]{36}):due:(\d{4}-\d{2}-\d{2})$');
  IF m IS NOT NULL THEN
    BEGIN
      parsed_date := m[3]::date;
      RETURN goatos_park_day_has_vaccination_work(p_tenant_id, NULL, NULL, parsed_date, m[2]::uuid);
    EXCEPTION WHEN invalid_text_representation OR datetime_field_overflow OR numeric_value_out_of_range THEN
      RETURN false;
    END;
  END IF;

  -- Unrecognized shape: not one of the canonical naming conventions.
  RETURN false;
END;
$_$;

--
-- Name: goatos_park_day_has_vaccination_work(uuid, uuid, uuid, date, uuid); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.goatos_park_day_has_vaccination_work(p_tenant_id uuid, p_park_id uuid, p_shed_id uuid, p_due_day date, p_rule_id uuid DEFAULT NULL::uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
  -- Unbatched obligations due that business-date, resolved to the requested park/shed/tenant scope
  -- (mirrors targets.go's calendarDriveTargetsSQL unbatched branch and
  -- canonical_read.go's obligation_drive_membership unbatched branches).
  SELECT EXISTS (
    SELECT 1
    FROM obligation_instances oi
    LEFT JOIN locations scope_loc
      ON scope_loc.tenant_id = oi.tenant_id
     AND scope_loc.location_id = oi.scope_id
     AND oi.scope_type IN ('park', 'shed', 'cohort')
    LEFT JOIN locations scope_parent
      ON scope_parent.tenant_id = oi.tenant_id
     AND scope_parent.location_id = scope_loc.parent_location_id
    WHERE oi.tenant_id = p_tenant_id
      AND oi.batch_id IS NULL
      AND oi.status NOT IN ('waived', 'canceled', 'superseded')
      AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date = p_due_day
      AND (p_rule_id IS NULL OR oi.rule_id = p_rule_id)
      AND (
        (p_park_id IS NOT NULL AND (
          (oi.scope_type = 'park' AND scope_loc.location_id = p_park_id)
          OR (oi.scope_type IN ('shed', 'cohort') AND scope_parent.location_id = p_park_id)
        ))
        OR (p_shed_id IS NOT NULL AND (
          (oi.scope_type = 'shed' AND scope_loc.location_id = p_shed_id)
          OR (oi.scope_type = 'cohort' AND scope_parent.location_id = p_shed_id)
        ))
        OR (p_park_id IS NULL AND p_shed_id IS NULL AND scope_loc.location_id IS NULL)
      )
  )
  -- Batched drives whose batch resolves to that park+business-date (mirrors targets.go's
  -- matched_batches CTE: batch scope is resolved from obligation_batches/locations, NOT the member
  -- obligation's own oi.scope_type/scope_id, since AttachObligationsToBatch never syncs it).
  OR EXISTS (
    SELECT 1
    FROM obligation_batches ob
    LEFT JOIN locations shed_loc
      ON shed_loc.tenant_id = ob.tenant_id AND shed_loc.location_id = ob.scope_id
    WHERE ob.tenant_id = p_tenant_id
      AND ob.status NOT IN ('superseded', 'canceled')
      AND (COALESCE(ob.window_start, ob.planned_date::timestamptz, ob.window_end) AT TIME ZONE 'Asia/Kolkata')::date = p_due_day
      AND (
        (p_park_id IS NOT NULL AND shed_loc.parent_location_id = p_park_id)
        OR (p_shed_id IS NOT NULL AND ob.scope_id = p_shed_id)
        OR (p_park_id IS NULL AND p_shed_id IS NULL AND shed_loc.parent_location_id IS NULL)
      )
  );
$$;

--
-- Name: goatos_reconcile_calendar_event_references(uuid, text, text, integer); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.goatos_reconcile_calendar_event_references(p_tenant_id uuid, p_cursor_source_table text DEFAULT ''::text, p_cursor_record_id text DEFAULT ''::text, p_limit integer DEFAULT 1000) RETURNS TABLE(source_table text, record_id uuid, calendar_event_id text, issue text)
    LANGUAGE plpgsql STABLE
    AS $$
DECLARE
  v_cursor_record_id uuid;
BEGIN
  -- Empty/NULL cursor record id means "start of this source_table" (or overall
  -- start when the source_table cursor is empty too).
  IF p_cursor_record_id IS NULL OR p_cursor_record_id = '' THEN
    v_cursor_record_id := NULL;
  ELSE
    v_cursor_record_id := p_cursor_record_id::uuid;
  END IF;

  RETURN QUERY
  SELECT * FROM (
    SELECT
      'notification_requests'::text AS source_table,
      nr.notification_request_id AS record_id,
      nr.calendar_event_id AS calendar_event_id,
      'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text AS issue
    FROM notification_requests nr
    WHERE nr.tenant_id = p_tenant_id
      AND nr.calendar_event_id IS NOT NULL
      AND NOT goatos_calendar_event_reference_valid(p_tenant_id, nr.calendar_event_id)

    UNION ALL

    SELECT
      'calendar_snoozes'::text AS source_table,
      cs.snooze_id AS record_id,
      cs.calendar_event_id AS calendar_event_id,
      'orphaned calendar_event_id: does not map to any canonical obligation/batch/drive/task'::text AS issue
    FROM calendar_snoozes cs
    WHERE cs.tenant_id = p_tenant_id
      AND cs.calendar_event_id IS NOT NULL
      AND NOT goatos_calendar_event_reference_valid(p_tenant_id, cs.calendar_event_id)
  ) combined
  WHERE
    -- Keyset: strictly greater than the cursor in (source_table, record_id) order.
    p_cursor_source_table = ''
    OR combined.source_table > p_cursor_source_table
    OR (
      combined.source_table = p_cursor_source_table
      AND v_cursor_record_id IS NOT NULL
      AND combined.record_id > v_cursor_record_id
    )
  ORDER BY combined.source_table, combined.record_id
  LIMIT p_limit;
END;
$$;

--
-- Name: herd_register_apply_summary_delta(uuid, uuid, uuid, uuid, text, text, text, bigint, bigint, bigint, bigint); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.herd_register_apply_summary_delta(p_tenant_id uuid, p_park_id uuid, p_farm_id uuid, p_current_location_id uuid, p_breed text, p_sex text, p_lifecycle_status text, p_active_delta bigint, p_adult_delta bigint, p_kid_delta bigint, p_untagged_kid_delta bigint) RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
  UPDATE herd_register_summary_projection
  SET active_count = active_count + p_active_delta,
      adult_count = adult_count + p_adult_delta,
      kid_count = kid_count + p_kid_delta,
      untagged_kid_count = untagged_kid_count + p_untagged_kid_delta,
      projected_at = now()
  WHERE tenant_id = p_tenant_id
    AND park_id IS NOT DISTINCT FROM p_park_id
    AND farm_id IS NOT DISTINCT FROM p_farm_id
    AND current_location_id IS NOT DISTINCT FROM p_current_location_id
    AND breed IS NOT DISTINCT FROM p_breed
    AND sex = p_sex
    AND lifecycle_status = p_lifecycle_status;

  IF FOUND THEN
    RETURN;
  END IF;

  IF p_active_delta < 0 OR p_adult_delta < 0 OR p_kid_delta < 0 OR p_untagged_kid_delta < 0 THEN
    RAISE EXCEPTION 'herd register projection drift: missing summary row for tenant %, park %, breed %, sex %, status %',
      p_tenant_id, p_park_id, p_breed, p_sex, p_lifecycle_status;
  END IF;

  INSERT INTO herd_register_summary_projection (
    tenant_id, park_id, farm_id, current_location_id, breed, sex, lifecycle_status,
    active_count, adult_count, kid_count, untagged_kid_count, projected_at
  ) VALUES (
    p_tenant_id, p_park_id, p_farm_id, p_current_location_id, p_breed, p_sex, p_lifecycle_status,
    p_active_delta, p_adult_delta, p_kid_delta, p_untagged_kid_delta, now()
  )
  ON CONFLICT ON CONSTRAINT herd_register_summary_projection_scope_key
  DO UPDATE SET
    active_count = herd_register_summary_projection.active_count + EXCLUDED.active_count,
    adult_count = herd_register_summary_projection.adult_count + EXCLUDED.adult_count,
    kid_count = herd_register_summary_projection.kid_count + EXCLUDED.kid_count,
    untagged_kid_count = herd_register_summary_projection.untagged_kid_count + EXCLUDED.untagged_kid_count,
    projected_at = now();
END;
$$;

--
-- Name: herd_register_goats_after_write_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.herd_register_goats_after_write_trg() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    DELETE FROM herd_register_goat_projection
    WHERE tenant_id = OLD.tenant_id AND goat_id = OLD.goat_id;
    RETURN OLD;
  END IF;

  IF TG_OP = 'UPDATE' AND (OLD.tenant_id, OLD.goat_id) IS DISTINCT FROM (NEW.tenant_id, NEW.goat_id) THEN
    DELETE FROM herd_register_goat_projection
    WHERE tenant_id = OLD.tenant_id AND goat_id = OLD.goat_id;
  END IF;
  PERFORM herd_register_refresh_goat_projection(NEW.tenant_id, NEW.goat_id);
  RETURN NEW;
END;
$$;

--
-- Name: herd_register_identifiers_after_write_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.herd_register_identifiers_after_write_trg() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    PERFORM herd_register_refresh_goat_projection(OLD.tenant_id, OLD.goat_id);
    RETURN OLD;
  ELSIF TG_OP = 'INSERT' THEN
    PERFORM herd_register_refresh_goat_projection(NEW.tenant_id, NEW.goat_id);
    RETURN NEW;
  END IF;

  PERFORM herd_register_refresh_goat_projection(OLD.tenant_id, OLD.goat_id);
  IF (OLD.tenant_id, OLD.goat_id) IS DISTINCT FROM (NEW.tenant_id, NEW.goat_id) THEN
    PERFORM herd_register_refresh_goat_projection(NEW.tenant_id, NEW.goat_id);
  END IF;
  RETURN NEW;
END;
$$;

--
-- Name: herd_register_is_kid(text, text); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.herd_register_is_kid(p_age_band text, p_management_stage text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
  SELECT lower(COALESCE(p_age_band, '')) = 'kid'
      OR (
        lower(COALESCE(p_age_band, '')) NOT IN ('kid', 'adult')
        AND upper(COALESCE(p_management_stage, '')) ~ '^K[0-9]'
      );
$$;

--
-- Name: herd_register_projection_summary_trg(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.herd_register_projection_summary_trg() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    PERFORM herd_register_apply_summary_delta(
      OLD.tenant_id, OLD.park_id, OLD.farm_id, OLD.current_location_id,
      OLD.breed, OLD.sex, OLD.lifecycle_status,
      -1,
      CASE WHEN OLD.is_kid THEN 0 ELSE -1 END,
      CASE WHEN OLD.is_kid THEN -1 ELSE 0 END,
      CASE WHEN OLD.is_kid AND OLD.is_untagged THEN -1 ELSE 0 END
    );
  END IF;

  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    PERFORM herd_register_apply_summary_delta(
      NEW.tenant_id, NEW.park_id, NEW.farm_id, NEW.current_location_id,
      NEW.breed, NEW.sex, NEW.lifecycle_status,
      1,
      CASE WHEN NEW.is_kid THEN 0 ELSE 1 END,
      CASE WHEN NEW.is_kid THEN 1 ELSE 0 END,
      CASE WHEN NEW.is_kid AND NEW.is_untagged THEN 1 ELSE 0 END
    );
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

--
-- Name: herd_register_refresh_goat_projection(uuid, uuid); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.herd_register_refresh_goat_projection(p_tenant_id uuid, p_goat_id uuid) RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
  INSERT INTO herd_register_goat_projection (
    tenant_id, goat_id, display_id, park_id, farm_id, current_location_id,
    breed, sex, lifecycle_status, is_kid, is_untagged, projected_at
  )
  SELECT
    g.tenant_id,
    g.goat_id,
    g.display_id,
    g.park_id,
    g.farm_id,
    g.current_location_id,
    g.breed,
    g.sex,
    g.lifecycle_status,
    herd_register_is_kid(g.age_band, g.management_stage),
    NOT EXISTS (
      SELECT 1
      FROM goat_identifiers gi
      WHERE gi.tenant_id = g.tenant_id
        AND gi.goat_id = g.goat_id
        AND gi.identifier_type = 'animal_identifier_1'
        AND gi.status = 'active'
    ),
    now()
  FROM goats g
  WHERE g.tenant_id = p_tenant_id
    AND g.goat_id = p_goat_id
    AND g.merged_into_goat_id IS NULL
  ON CONFLICT (tenant_id, goat_id)
  DO UPDATE SET
    display_id = EXCLUDED.display_id,
    park_id = EXCLUDED.park_id,
    farm_id = EXCLUDED.farm_id,
    current_location_id = EXCLUDED.current_location_id,
    breed = EXCLUDED.breed,
    sex = EXCLUDED.sex,
    lifecycle_status = EXCLUDED.lifecycle_status,
    is_kid = EXCLUDED.is_kid,
    is_untagged = EXCLUDED.is_untagged,
    projected_at = EXCLUDED.projected_at
  WHERE (
    herd_register_goat_projection.display_id,
    herd_register_goat_projection.park_id,
    herd_register_goat_projection.farm_id,
    herd_register_goat_projection.current_location_id,
    herd_register_goat_projection.breed,
    herd_register_goat_projection.sex,
    herd_register_goat_projection.lifecycle_status,
    herd_register_goat_projection.is_kid,
    herd_register_goat_projection.is_untagged
  ) IS DISTINCT FROM (
    EXCLUDED.display_id,
    EXCLUDED.park_id,
    EXCLUDED.farm_id,
    EXCLUDED.current_location_id,
    EXCLUDED.breed,
    EXCLUDED.sex,
    EXCLUDED.lifecycle_status,
    EXCLUDED.is_kid,
    EXCLUDED.is_untagged
  );

  IF NOT FOUND THEN
    -- NOT FOUND also follows a no-op ON CONFLICT. Delete only when the canonical
    -- goat is absent or merged; otherwise the current projection is already exact.
    DELETE FROM herd_register_goat_projection p
    WHERE p.tenant_id = p_tenant_id
      AND p.goat_id = p_goat_id
      AND NOT EXISTS (
        SELECT 1 FROM goats g
        WHERE g.tenant_id = p_tenant_id
          AND g.goat_id = p_goat_id
          AND g.merged_into_goat_id IS NULL
      );
  END IF;
END;
$$;

--
-- Name: location_seeded_scope_guard(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.location_seeded_scope_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  approved_plan text;
BEGIN
  approved_plan := current_setting('goatos.approved_location_migration_plan', true);
  IF approved_plan IS NULL OR btrim(approved_plan) = '' THEN
    IF TG_TABLE_NAME = 'locations' THEN
      IF OLD.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
        AND OLD.location_id IN (
          '00000000-0000-4000-8000-000000003001'::uuid,
          '00000000-0000-4000-8000-000000003002'::uuid,
          '00000000-0000-4000-8000-000000003003'::uuid
        ) THEN
        RAISE EXCEPTION 'seeded CBE/CPT/HF location scope requires approved migration plan';
      END IF;
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

--
-- Name: mark_obligation_batch_stock_reservation_from_movement(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.mark_obligation_batch_stock_reservation_from_movement() RETURNS trigger
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

--
-- Name: next_goat_display_id(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.next_goat_display_id() RETURNS text
    LANGUAGE sql
    AS $$
  SELECT 'G-' || lpad(v::text, GREATEST(6, length(v::text)), '0')
  FROM (SELECT nextval('goat_display_id_seq') AS v) s;
$$;

--
-- Name: prevent_goat_hard_delete(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.prevent_goat_hard_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  RAISE EXCEPTION 'goat_hard_delete_blocked: goat % must be merged, inactive, or corrected without DELETE', OLD.goat_id
    USING ERRCODE = '23514';
END;
$$;

--
-- Name: prevent_merged_goat_normal_update(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.prevent_merged_goat_normal_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF TG_OP = 'UPDATE'
    AND OLD.merged_into_goat_id IS NOT NULL
    AND COALESCE(current_setting('goatos.allow_merged_goat_update', true), 'off') <> 'on'
  THEN
    RAISE EXCEPTION 'merged_goat_write_blocked: goat % redirects to %', OLD.goat_id, OLD.merged_into_goat_id
      USING ERRCODE = '23514';
  END IF;

  IF TG_OP = 'UPDATE' AND NEW.display_id <> OLD.display_id THEN
    RAISE EXCEPTION 'display_id is immutable for goat %', OLD.goat_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: prevent_published_protocol_version_delete(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.prevent_published_protocol_version_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF OLD.status IN ('published', 'retired') THEN
    RAISE EXCEPTION 'published or retired protocol version % is immutable; create a new draft version',
      OLD.protocol_version_id
      USING ERRCODE = '23514';
  END IF;
  RETURN OLD;
END;
$$;

--
-- Name: record_counts_shifting_readiness_evidence(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.record_counts_shifting_readiness_evidence() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  INSERT INTO counts_shifting_readiness_evidence (
    tenant_id, subgate_id, status, evidence_ref, blocker_reason, implementation_ref, recorded_at
  ) VALUES (
    NEW.tenant_id, NEW.subgate_id, NEW.status, NEW.evidence_ref,
    NEW.blocker_reason, NEW.implementation_ref, now()
  );
  RETURN NEW;
END;
$$;

--
-- Name: reject_overlapping_location_capacity(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.reject_overlapping_location_capacity() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM location_capacity_records existing
    WHERE existing.tenant_id = NEW.tenant_id
      AND existing.location_id = NEW.location_id
      AND existing.capacity_kind = NEW.capacity_kind
      AND existing.capacity_record_id <> COALESCE(NEW.capacity_record_id, '00000000-0000-0000-0000-000000000000'::uuid)
      AND daterange(existing.effective_from, existing.effective_to, '[)') && daterange(NEW.effective_from, NEW.effective_to, '[)')
  ) THEN
    RAISE EXCEPTION 'overlapping capacity record for tenant %, location %, kind %', NEW.tenant_id, NEW.location_id, NEW.capacity_kind;
  END IF;
  RETURN NEW;
END;
$$;

--
-- Name: validate_goat_merge_link(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_goat_merge_link() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  survivor_redirect uuid;
  merged_redirect uuid;
BEGIN
  SELECT merged_into_goat_id
    INTO survivor_redirect
    FROM goats
    WHERE goat_id = NEW.survivor_goat_id;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'survivor goat % does not exist', NEW.survivor_goat_id
      USING ERRCODE = '23503';
  END IF;

  IF survivor_redirect IS NOT NULL THEN
    RAISE EXCEPTION 'survivor goat % must be live, not merged', NEW.survivor_goat_id
      USING ERRCODE = '23514';
  END IF;

  SELECT merged_into_goat_id
    INTO merged_redirect
    FROM goats
    WHERE goat_id = NEW.merged_goat_id;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'merged goat % does not exist', NEW.merged_goat_id
      USING ERRCODE = '23503';
  END IF;

  IF merged_redirect IS DISTINCT FROM NEW.survivor_goat_id THEN
    RAISE EXCEPTION 'merged goat % must redirect to survivor % before link insert', NEW.merged_goat_id, NEW.survivor_goat_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: validate_location_profile_type(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_location_profile_type() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  expected_type text := TG_ARGV[0];
  found_type text;
BEGIN
  SELECT location_type
    INTO found_type
    FROM locations
    WHERE tenant_id = NEW.tenant_id
      AND location_id = NEW.location_id;

  IF found_type IS NULL THEN
    RAISE EXCEPTION 'location % does not exist for tenant %', NEW.location_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF found_type <> expected_type THEN
    RAISE EXCEPTION 'profile expects location_type % but location % is %', expected_type, NEW.location_id, found_type
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: validate_obligation_scope(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_obligation_scope() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  found_type text;
BEGIN
  IF NEW.scope_type = 'tenant' THEN
    IF NEW.scope_id <> NEW.tenant_id THEN
      RAISE EXCEPTION 'tenant scope must use tenant_id as scope_id'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.scope_type = 'custodian_party' THEN
    IF NOT EXISTS (SELECT 1 FROM parties WHERE party_id = NEW.scope_id) THEN
      RAISE EXCEPTION 'custodian party scope % does not exist', NEW.scope_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  SELECT location_type
    INTO found_type
    FROM locations
    WHERE tenant_id = NEW.tenant_id
      AND location_id = NEW.scope_id;

  IF found_type IS NULL THEN
    RAISE EXCEPTION 'location scope % does not exist for tenant %', NEW.scope_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF found_type <> NEW.scope_type THEN
    RAISE EXCEPTION 'scope type % does not match location type % for scope %', NEW.scope_type, found_type, NEW.scope_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: validate_obligation_target(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_obligation_target() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  found_type text;
BEGIN
  IF NEW.target_type = 'tenant' THEN
    IF NEW.target_id <> NEW.tenant_id THEN
      RAISE EXCEPTION 'tenant target must use tenant_id as target_id'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.target_type = 'goat' THEN
    IF NOT EXISTS (SELECT 1 FROM goats WHERE tenant_id = NEW.tenant_id AND goat_id = NEW.target_id) THEN
      RAISE EXCEPTION 'goat target % does not exist for tenant %', NEW.target_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  -- park / shed / cohort
  SELECT location_type
    INTO found_type
    FROM locations
    WHERE tenant_id = NEW.tenant_id
      AND location_id = NEW.target_id;

  IF found_type IS NULL THEN
    RAISE EXCEPTION 'location target % does not exist for tenant %', NEW.target_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF found_type <> NEW.target_type THEN
    RAISE EXCEPTION 'target type % does not match location type % for target %', NEW.target_type, found_type, NEW.target_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: validate_outbox_event_tenant(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_outbox_event_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM verification_items
      WHERE tenant_id = NEW.tenant_id
        AND item_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

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

--
-- Name: validate_protocol_version_scope(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_protocol_version_scope() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  found_type text;
BEGIN
  IF NEW.scope_type = 'tenant' THEN
    RETURN NEW;
  END IF;

  SELECT location_type
    INTO found_type
    FROM locations
    WHERE tenant_id = NEW.tenant_id
      AND location_id = NEW.scope_id;

  IF found_type IS NULL THEN
    RAISE EXCEPTION 'protocol version park scope % does not exist for tenant %', NEW.scope_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF found_type <> 'park' THEN
    RAISE EXCEPTION 'protocol version scope % must be a park, got %', NEW.scope_id, found_type
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: validate_user_scope_grant(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.validate_user_scope_grant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  location_type_found text;
BEGIN
  IF NEW.scope_type = 'tenant' THEN
    IF NEW.scope_id <> NEW.tenant_id THEN
      RAISE EXCEPTION 'tenant scope grant must use tenant_id as scope_id'
        USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
  END IF;

  IF NEW.scope_type = 'custodian_party' THEN
    IF NOT EXISTS (SELECT 1 FROM parties WHERE party_id = NEW.scope_id) THEN
      RAISE EXCEPTION 'custodian party scope % does not exist', NEW.scope_id
        USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
  END IF;

  SELECT location_type
    INTO location_type_found
    FROM locations
    WHERE tenant_id = NEW.tenant_id
      AND location_id = NEW.scope_id;

  IF location_type_found IS NULL THEN
    RAISE EXCEPTION 'location scope % does not exist for tenant %', NEW.scope_id, NEW.tenant_id
      USING ERRCODE = '23503';
  END IF;

  IF location_type_found <> NEW.scope_type THEN
    RAISE EXCEPTION 'scope type % does not match location type % for scope %', NEW.scope_type, location_type_found, NEW.scope_id
      USING ERRCODE = '23514';
  END IF;

  RETURN NEW;
END;
$$;

--
-- Name: verification_items_touch_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.verification_items_touch_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: crash_daily; Type: TABLE; Schema: analytics; Owner: -
--

CREATE TABLE analytics.crash_daily (
    tenant_id uuid NOT NULL,
    event_date date NOT NULL,
    app_version text DEFAULT ''::text NOT NULL,
    crash_free_users_pct numeric(6,3) DEFAULT 100 NOT NULL,
    crash_free_sessions_pct numeric(6,3) DEFAULT 100 NOT NULL,
    fatal_count bigint DEFAULT 0 NOT NULL,
    nonfatal_count bigint DEFAULT 0 NOT NULL,
    top_issues jsonb DEFAULT '[]'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT crash_daily_counts_check CHECK (((fatal_count >= 0) AND (nonfatal_count >= 0))),
    CONSTRAINT crash_daily_pct_check CHECK ((((crash_free_users_pct >= (0)::numeric) AND (crash_free_users_pct <= (100)::numeric)) AND ((crash_free_sessions_pct >= (0)::numeric) AND (crash_free_sessions_pct <= (100)::numeric))))
);

--
-- Name: engagement_daily; Type: TABLE; Schema: analytics; Owner: -
--

CREATE TABLE analytics.engagement_daily (
    tenant_id uuid NOT NULL,
    event_date date NOT NULL,
    dau bigint DEFAULT 0 NOT NULL,
    wau_approx bigint DEFAULT 0 NOT NULL,
    sessions bigint DEFAULT 0 NOT NULL,
    avg_session_ms bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT engagement_daily_counts_check CHECK (((dau >= 0) AND (wau_approx >= 0) AND (sessions >= 0) AND (avg_session_ms >= 0)))
);

--
-- Name: funnel_daily; Type: TABLE; Schema: analytics; Owner: -
--

CREATE TABLE analytics.funnel_daily (
    tenant_id uuid NOT NULL,
    event_date date NOT NULL,
    funnel_key text NOT NULL,
    step_key text NOT NULL,
    step_index integer NOT NULL,
    users bigint DEFAULT 0 NOT NULL,
    sessions bigint DEFAULT 0 NOT NULL,
    conversions bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT funnel_daily_counts_check CHECK (((users >= 0) AND (sessions >= 0) AND (conversions >= 0))),
    CONSTRAINT funnel_daily_step_index_check CHECK ((step_index >= 0))
);

--
-- Name: journey_daily; Type: TABLE; Schema: analytics; Owner: -
--

CREATE TABLE analytics.journey_daily (
    tenant_id uuid NOT NULL,
    event_date date NOT NULL,
    journey_key text NOT NULL,
    p50_ms bigint DEFAULT 0 NOT NULL,
    p90_ms bigint DEFAULT 0 NOT NULL,
    p99_ms bigint DEFAULT 0 NOT NULL,
    completions bigint DEFAULT 0 NOT NULL,
    drop_offs bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT journey_daily_counts_check CHECK (((completions >= 0) AND (drop_offs >= 0))),
    CONSTRAINT journey_daily_latency_check CHECK (((p50_ms >= 0) AND (p90_ms >= 0) AND (p99_ms >= 0)))
);

--
-- Name: rollup_run; Type: TABLE; Schema: analytics; Owner: -
--

CREATE TABLE analytics.rollup_run (
    run_id bigint NOT NULL,
    source_date date NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    rows_written bigint DEFAULT 0 NOT NULL,
    bytes_billed bigint DEFAULT 0 NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    error text,
    CONSTRAINT rollup_run_bytes_billed_check CHECK ((bytes_billed >= 0)),
    CONSTRAINT rollup_run_rows_written_check CHECK ((rows_written >= 0)),
    CONSTRAINT rollup_run_status_check CHECK ((status = ANY (ARRAY['running'::text, 'succeeded'::text, 'failed'::text, 'skipped'::text])))
);

--
-- Name: rollup_run_run_id_seq; Type: SEQUENCE; Schema: analytics; Owner: -
--

ALTER TABLE analytics.rollup_run ALTER COLUMN run_id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME analytics.rollup_run_run_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

--
-- Name: admin_ui_config_entries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_ui_config_entries (
    tenant_id uuid NOT NULL,
    locale text DEFAULT 'default'::text NOT NULL,
    route_id text DEFAULT ''::text NOT NULL,
    config_key text NOT NULL,
    config_value text NOT NULL,
    value_kind text DEFAULT 'text'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT admin_ui_config_entries_key_check CHECK ((btrim(config_key) <> ''::text)),
    CONSTRAINT admin_ui_config_entries_kind_check CHECK ((value_kind = ANY (ARRAY['text'::text, 'label'::text, 'title'::text, 'copy'::text, 'tone'::text, 'disabled_reason'::text]))),
    CONSTRAINT admin_ui_config_entries_locale_check CHECK ((btrim(locale) <> ''::text)),
    CONSTRAINT admin_ui_config_entries_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT admin_ui_config_entries_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT admin_ui_config_entries_status_check CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))
);

--
-- Name: admin_ui_config_family_change_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_ui_config_family_change_queue (
    transaction_id bigint NOT NULL,
    tenant_id uuid NOT NULL,
    family_key text NOT NULL,
    changed_by uuid,
    source text DEFAULT 'system'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    change_count integer DEFAULT 1 NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT admin_ui_config_family_change_queue_count_check CHECK ((change_count >= 1)),
    CONSTRAINT admin_ui_config_family_change_queue_family_check CHECK ((btrim(family_key) <> ''::text)),
    CONSTRAINT admin_ui_config_family_change_queue_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT admin_ui_config_family_change_queue_source_check CHECK ((btrim(source) <> ''::text))
);

--
-- Name: admin_ui_config_family_revisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_ui_config_family_revisions (
    tenant_id uuid NOT NULL,
    family_key text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    content_hash text DEFAULT ''::text NOT NULL,
    changed_at timestamp with time zone DEFAULT now() NOT NULL,
    changed_by uuid,
    source text DEFAULT 'system'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT admin_ui_config_family_key_check CHECK ((btrim(family_key) <> ''::text)),
    CONSTRAINT admin_ui_config_family_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT admin_ui_config_family_revision_check CHECK ((revision >= 1))
);

--
-- Name: animal_stage_lookup; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.animal_stage_lookup (
    animal_stage_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    stage_code text NOT NULL,
    name text NOT NULL,
    min_age_days integer,
    max_age_days integer,
    sort_order integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT animal_stage_lookup_age_check CHECK (((min_age_days IS NULL) OR (max_age_days IS NULL) OR (max_age_days >= min_age_days))),
    CONSTRAINT animal_stage_lookup_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'retired'::text])))
);

--
-- Name: arrival_intake_review_goats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.arrival_intake_review_goats (
    review_goat_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    review_id uuid NOT NULL,
    load_id uuid NOT NULL,
    goat_id uuid,
    animal_identifier_2 text,
    animal_identifier_1 text,
    item_key text NOT NULL,
    arrival_state text NOT NULL,
    health_flag text,
    weight_flag text,
    proof_ref_id uuid,
    notes text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT arrival_intake_review_goats_item_key_check CHECK ((btrim(item_key) <> ''::text)),
    CONSTRAINT arrival_intake_review_goats_state_check CHECK ((arrival_state = ANY (ARRAY['matched'::text, 'missing'::text, 'extra_unresolved'::text, 'health_flag'::text, 'weight_flag'::text, 'accepted'::text, 'rejected'::text, 'deferred'::text, 'blocked'::text])))
);

--
-- Name: arrival_intake_reviews; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.arrival_intake_reviews (
    review_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    load_id uuid NOT NULL,
    park_location_id uuid NOT NULL,
    expected_count integer DEFAULT 0 NOT NULL,
    loaded_count integer DEFAULT 0 NOT NULL,
    arrived_count integer DEFAULT 0 NOT NULL,
    matched_count integer DEFAULT 0 NOT NULL,
    missing_count integer DEFAULT 0 NOT NULL,
    extra_count integer DEFAULT 0 NOT NULL,
    rejected_count integer DEFAULT 0 NOT NULL,
    health_flags jsonb DEFAULT '[]'::jsonb NOT NULL,
    weight_flags jsonb DEFAULT '[]'::jsonb NOT NULL,
    media_proof_id uuid,
    status text DEFAULT 'pending'::text NOT NULL,
    reviewed_by uuid,
    reviewed_at timestamp with time zone NOT NULL,
    idempotency_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT arrival_intake_reviews_counts_check CHECK (((expected_count >= 0) AND (loaded_count >= 0) AND (arrived_count >= 0) AND (matched_count >= 0) AND (missing_count >= 0) AND (extra_count >= 0) AND (rejected_count >= 0))),
    CONSTRAINT arrival_intake_reviews_health_flags_array_check CHECK ((jsonb_typeof(health_flags) = 'array'::text)),
    CONSTRAINT arrival_intake_reviews_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT arrival_intake_reviews_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'mismatch'::text, 'accepted'::text, 'rejected'::text, 'deferred'::text, 'blocked'::text]))),
    CONSTRAINT arrival_intake_reviews_weight_flags_array_check CHECK ((jsonb_typeof(weight_flags) = 'array'::text))
);

--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_log (
    audit_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid,
    actor_id uuid,
    actor_type text NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid,
    scope_type text,
    scope_id uuid,
    decision_id uuid,
    before_state jsonb,
    after_state jsonb,
    metadata jsonb NOT NULL,
    trace_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: auth_pending_email_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auth_pending_email_grants (
    pending_grant_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    email text NOT NULL,
    normalized_email text NOT NULL,
    role text NOT NULL,
    scope_type text DEFAULT 'tenant'::text NOT NULL,
    scope_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    valid_from timestamp with time zone DEFAULT now() NOT NULL,
    valid_to timestamp with time zone,
    source text DEFAULT 'manual'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_claimed_user_id uuid,
    last_claimed_external_subject text,
    last_claimed_at timestamp with time zone,
    claim_count bigint DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT auth_pending_email_grants_email_check CHECK (((normalized_email = lower(btrim(email))) AND (normalized_email <> ''::text) AND (normalized_email !~~ '%,%'::text) AND (normalized_email !~~ '% %'::text) AND (POSITION(('@'::text) IN (normalized_email)) > 1))),
    CONSTRAINT auth_pending_email_grants_scope_check CHECK (((scope_type = 'tenant'::text) AND (scope_id = tenant_id))),
    CONSTRAINT auth_pending_email_grants_status_check CHECK ((status = ANY (ARRAY['active'::text, 'revoked'::text]))),
    CONSTRAINT auth_pending_email_grants_valid_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: breed_aliases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.breed_aliases (
    alias_id uuid DEFAULT gen_random_uuid() NOT NULL,
    breed_id uuid NOT NULL,
    alias text NOT NULL,
    normalized_alias text NOT NULL,
    source_system text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: breeds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.breeds (
    breed_id uuid DEFAULT gen_random_uuid() NOT NULL,
    species text DEFAULT 'goat'::text NOT NULL,
    canonical_name text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    review_notes text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT breeds_status_check CHECK ((status = ANY (ARRAY['active'::text, 'review'::text, 'inactive'::text])))
);

--
-- Name: bulk_status_job; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bulk_status_job (
    bulk_status_job_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    actor_id uuid,
    axis text NOT NULL,
    params jsonb DEFAULT '{}'::jsonb NOT NULL,
    total_rows integer DEFAULT 0 NOT NULL,
    applied_rows integer DEFAULT 0 NOT NULL,
    skipped_rows integer DEFAULT 0 NOT NULL,
    failed_rows integer DEFAULT 0 NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    idempotency_key text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT bulk_status_job_axis_check CHECK ((axis = ANY (ARRAY['reproductive'::text, 'health'::text, 'exit'::text]))),
    CONSTRAINT bulk_status_job_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'running'::text, 'completed'::text, 'failed'::text, 'canceled'::text])))
);

--
-- Name: bulk_status_job_row; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.bulk_status_job_row (
    bulk_status_job_row_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    job_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    axis text NOT NULL,
    target text NOT NULL,
    reason text,
    expected_row_version bigint,
    row_state text DEFAULT 'pending'::text NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    failure_reason text,
    event_id uuid,
    claimed_at timestamp with time zone,
    applied_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT bulk_status_job_row_axis_check CHECK ((axis = ANY (ARRAY['reproductive'::text, 'health'::text, 'exit'::text]))),
    CONSTRAINT bulk_status_job_row_state_check CHECK ((row_state = ANY (ARRAY['pending'::text, 'claimed'::text, 'applied'::text, 'skipped'::text, 'error'::text, 'retry'::text])))
);

--
-- Name: calendar_reconciler_progress; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.calendar_reconciler_progress (
    tenant_id uuid NOT NULL,
    cursor_source_table text DEFAULT ''::text NOT NULL,
    cursor_record_id text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: calendar_snoozes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.calendar_snoozes (
    snooze_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    calendar_event_id text NOT NULL,
    target_type text NOT NULL,
    target_id uuid,
    snooze_until timestamp with time zone NOT NULL,
    reason text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    replaced_by_snooze_id uuid,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    trace_id text,
    CONSTRAINT calendar_snoozes_context_object_check CHECK ((jsonb_typeof(context) = 'object'::text)),
    CONSTRAINT calendar_snoozes_status_check CHECK ((status = ANY (ARRAY['active'::text, 'replaced'::text, 'expired'::text, 'canceled'::text])))
);

--
-- Name: count_base_anchors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_base_anchors (
    base_count_anchor_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    park_id uuid NOT NULL,
    shed_id uuid NOT NULL,
    breed_id uuid,
    breed_key text NOT NULL,
    breed_label text NOT NULL,
    counted_at timestamp with time zone NOT NULL,
    head_count integer NOT NULL,
    source_system text NOT NULL,
    source_ref text NOT NULL,
    source_hash text NOT NULL,
    anchor_state text DEFAULT 'adopted'::text NOT NULL,
    discrepancy_state text DEFAULT 'not_checked'::text NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    recorded_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT count_base_anchors_breed_key_check CHECK ((btrim(breed_key) <> ''::text)),
    CONSTRAINT count_base_anchors_discrepancy_check CHECK ((discrepancy_state = ANY (ARRAY['not_checked'::text, 'investigating'::text, 'resolved'::text]))),
    CONSTRAINT count_base_anchors_head_count_check CHECK ((head_count >= 0)),
    CONSTRAINT count_base_anchors_idem_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT count_base_anchors_source_check CHECK ((source_system = ANY (ARRAY['physical_base_count'::text, 'manual_review'::text, 'import'::text, 'goatos_canonical'::text]))),
    CONSTRAINT count_base_anchors_source_hash_check CHECK ((btrim(source_hash) <> ''::text)),
    CONSTRAINT count_base_anchors_source_ref_check CHECK ((btrim(source_ref) <> ''::text)),
    CONSTRAINT count_base_anchors_state_check CHECK ((anchor_state = ANY (ARRAY['adopted'::text, 'superseded'::text, 'rejected'::text])))
);

--
-- Name: count_dimension_aliases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_dimension_aliases (
    alias_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    dimension text NOT NULL,
    source_system text DEFAULT '*'::text NOT NULL,
    source_value text NOT NULL,
    source_value_norm text NOT NULL,
    canonical_value text NOT NULL,
    canonical_label text,
    review_status text DEFAULT 'draft'::text NOT NULL,
    source_ref text NOT NULL,
    source_hash text NOT NULL,
    approved_by uuid,
    approved_at timestamp with time zone,
    effective_from date DEFAULT '1970-01-01'::date NOT NULL,
    effective_to date,
    notes text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT count_dimension_aliases_canonical_check CHECK ((btrim(canonical_value) <> ''::text)),
    CONSTRAINT count_dimension_aliases_dimension_check CHECK ((dimension = ANY (ARRAY['breed'::text, 'stage_tag'::text, 'age_class'::text, 'sex'::text, 'shed_tag'::text]))),
    CONSTRAINT count_dimension_aliases_effective_range_check CHECK (((effective_to IS NULL) OR (effective_to > effective_from))),
    CONSTRAINT count_dimension_aliases_evidence_check CHECK (((btrim(source_ref) <> ''::text) AND (btrim(source_hash) <> ''::text))),
    CONSTRAINT count_dimension_aliases_review_status_check CHECK ((review_status = ANY (ARRAY['draft'::text, 'approved'::text, 'rejected'::text, 'retired'::text]))),
    CONSTRAINT count_dimension_aliases_source_check CHECK (((btrim(source_system) <> ''::text) AND (btrim(source_value) <> ''::text) AND (btrim(source_value_norm) <> ''::text)))
);

--
-- Name: count_mismatch_scan_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_mismatch_scan_runs (
    count_mismatch_scan_run_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    park_id uuid,
    shed_id uuid,
    counted_after timestamp with time zone,
    counted_before timestamp with time zone NOT NULL,
    cursor_counted_at timestamp with time zone,
    cursor_anchor_id uuid,
    status text DEFAULT 'running'::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    scanned_anchor_count integer DEFAULT 0 NOT NULL,
    exception_write_count integer DEFAULT 0 NOT NULL,
    investigating_anchor_count integer DEFAULT 0 NOT NULL,
    next_cursor_counted_at timestamp with time zone,
    next_cursor_anchor_id uuid,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT count_mismatch_scan_runs_counts_check CHECK (((scanned_anchor_count >= 0) AND (exception_write_count >= 0) AND (investigating_anchor_count >= 0))),
    CONSTRAINT count_mismatch_scan_runs_cursor_pair_check CHECK ((((cursor_counted_at IS NULL) AND (cursor_anchor_id IS NULL)) OR ((cursor_counted_at IS NOT NULL) AND (cursor_anchor_id IS NOT NULL)))),
    CONSTRAINT count_mismatch_scan_runs_next_cursor_pair_check CHECK ((((next_cursor_counted_at IS NULL) AND (next_cursor_anchor_id IS NULL)) OR ((next_cursor_counted_at IS NOT NULL) AND (next_cursor_anchor_id IS NOT NULL)))),
    CONSTRAINT count_mismatch_scan_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'completed'::text, 'failed'::text]))),
    CONSTRAINT count_mismatch_scan_runs_window_check CHECK (((counted_after IS NULL) OR (counted_after < counted_before)))
);

--
-- Name: count_projection_exception_resolutions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_projection_exception_resolutions (
    count_projection_exception_resolution_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    count_projection_exception_id uuid NOT NULL,
    action text NOT NULL,
    resolved_by_ref text NOT NULL,
    resolution_reason text NOT NULL,
    resolution_ref text,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT count_projection_exception_resolutions_action_check CHECK ((action = ANY (ARRAY['resolve'::text, 'dismiss'::text]))),
    CONSTRAINT count_projection_exception_resolutions_actor_check CHECK ((btrim(resolved_by_ref) <> ''::text)),
    CONSTRAINT count_projection_exception_resolutions_idem_check CHECK (((btrim(idempotency_key) <> ''::text) AND (btrim(request_fingerprint) <> ''::text))),
    CONSTRAINT count_projection_exception_resolutions_reason_check CHECK ((btrim(resolution_reason) <> ''::text))
);

--
-- Name: count_projection_exceptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_projection_exceptions (
    count_projection_exception_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    count_projection_snapshot_id uuid,
    exception_type text NOT NULL,
    source_key text NOT NULL,
    grain_key text NOT NULL,
    park_id uuid,
    shed_id uuid,
    breed_key text,
    stage_tag text,
    severity text DEFAULT 'blocking'::text NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    owner_ref text,
    blocker_reason text NOT NULL,
    evidence_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    work_type text DEFAULT 'counts_projection_exception'::text NOT NULL,
    work_state text DEFAULT 'blocked'::text NOT NULL,
    due_at timestamp with time zone DEFAULT now() NOT NULL,
    next_action text DEFAULT 'Review Counts/Shifting projection exception'::text NOT NULL,
    evidence_link text DEFAULT '/feed-direction/counts-projection/exceptions'::text NOT NULL,
    resolution_id uuid,
    resolved_by_ref text,
    resolution_reason text,
    resolution_ref text,
    CONSTRAINT count_projection_exceptions_evidence_link_check CHECK ((btrim(evidence_link) <> ''::text)),
    CONSTRAINT count_projection_exceptions_evidence_object_check CHECK ((jsonb_typeof(evidence_json) = 'object'::text)),
    CONSTRAINT count_projection_exceptions_grain_key_check CHECK ((btrim(grain_key) <> ''::text)),
    CONSTRAINT count_projection_exceptions_next_action_check CHECK ((btrim(next_action) <> ''::text)),
    CONSTRAINT count_projection_exceptions_reason_check CHECK ((btrim(blocker_reason) <> ''::text)),
    CONSTRAINT count_projection_exceptions_severity_check CHECK ((severity = ANY (ARRAY['warning'::text, 'blocking'::text, 'critical'::text]))),
    CONSTRAINT count_projection_exceptions_source_key_check CHECK ((btrim(source_key) <> ''::text)),
    CONSTRAINT count_projection_exceptions_status_check CHECK ((status = ANY (ARRAY['open'::text, 'resolved'::text, 'dismissed'::text]))),
    CONSTRAINT count_projection_exceptions_type_check CHECK ((exception_type = ANY (ARRAY['missing_base_count'::text, 'missing_structured_impact'::text, 'unreported_shifting'::text, 'count_mismatch'::text, 'alias_conflict'::text, 'ration_context_unresolved'::text, 'destination_shortage'::text, 'unsafe_surplus'::text, 'query_plan_unproven'::text, 'missing_projection_snapshot'::text, 'stale_projection'::text]))),
    CONSTRAINT count_projection_exceptions_work_state_check CHECK ((work_state = ANY (ARRAY['blocked'::text, 'resolved'::text, 'dismissed'::text]))),
    CONSTRAINT count_projection_exceptions_work_type_check CHECK ((work_type = 'counts_projection_exception'::text))
);

--
-- Name: count_projection_recompute_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_projection_recompute_runs (
    count_projection_recompute_run_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    park_id uuid NOT NULL,
    horizon text NOT NULL,
    target_date date NOT NULL,
    as_of timestamp with time zone NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    projection_status text,
    snapshot_id uuid,
    row_count integer DEFAULT 0 NOT NULL,
    exception_count integer DEFAULT 0 NOT NULL,
    source_contract_version text NOT NULL,
    generated_by text NOT NULL,
    trace_id text,
    last_error text,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT count_projection_recompute_runs_completion_check CHECK ((((status = 'running'::text) AND (completed_at IS NULL)) OR ((status = ANY (ARRAY['completed'::text, 'failed'::text])) AND (completed_at IS NOT NULL)))),
    CONSTRAINT count_projection_recompute_runs_contract_check CHECK ((btrim(source_contract_version) <> ''::text)),
    CONSTRAINT count_projection_recompute_runs_counts_check CHECK (((row_count >= 0) AND (exception_count >= 0))),
    CONSTRAINT count_projection_recompute_runs_generated_by_check CHECK ((btrim(generated_by) <> ''::text)),
    CONSTRAINT count_projection_recompute_runs_horizon_check CHECK ((horizon = ANY (ARRAY['count_as_of'::text, 'feed_target_date'::text]))),
    CONSTRAINT count_projection_recompute_runs_projection_status_check CHECK (((projection_status IS NULL) OR (projection_status = ANY (ARRAY['ready'::text, 'blocked'::text, 'stale'::text, 'failed'::text])))),
    CONSTRAINT count_projection_recompute_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'completed'::text, 'failed'::text])))
);

--
-- Name: count_projection_snapshot_rows; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_projection_snapshot_rows (
    count_projection_snapshot_row_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    count_projection_snapshot_id uuid NOT NULL,
    park_id uuid NOT NULL,
    shed_id uuid NOT NULL,
    target_date date NOT NULL,
    grain_key text NOT NULL,
    breed_id uuid,
    breed_key text NOT NULL,
    breed_label text NOT NULL,
    stage_tag text,
    age_class text,
    sex text,
    head_count integer NOT NULL,
    pregnant_count integer DEFAULT 0 NOT NULL,
    lactating_count integer DEFAULT 0 NOT NULL,
    warmup_count integer DEFAULT 0 NOT NULL,
    ration_context_resolution_state text DEFAULT 'unresolved'::text NOT NULL,
    ration_context_ref text,
    blocker_reason text,
    source_row_hash text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    base_count_anchor_id uuid,
    included_shifting_event_ids_hash text DEFAULT 'no-shifting-events'::text NOT NULL,
    CONSTRAINT count_projection_snapshot_rows_breed_key_check CHECK ((btrim(breed_key) <> ''::text)),
    CONSTRAINT count_projection_snapshot_rows_count_check CHECK ((head_count >= 0)),
    CONSTRAINT count_projection_snapshot_rows_grain_key_check CHECK ((btrim(grain_key) <> ''::text)),
    CONSTRAINT count_projection_snapshot_rows_hash_check CHECK ((btrim(source_row_hash) <> ''::text)),
    CONSTRAINT count_projection_snapshot_rows_included_shift_hash_check CHECK ((btrim(included_shifting_event_ids_hash) <> ''::text)),
    CONSTRAINT count_projection_snapshot_rows_resolution_check CHECK ((ration_context_resolution_state = ANY (ARRAY['resolved'::text, 'unresolved'::text, 'blocked'::text, 'not_required'::text]))),
    CONSTRAINT count_projection_snapshot_rows_risk_counts_check CHECK (((pregnant_count >= 0) AND (lactating_count >= 0) AND (warmup_count >= 0) AND (pregnant_count <= head_count) AND (lactating_count <= head_count) AND (warmup_count <= head_count)))
);

--
-- Name: count_projection_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_projection_snapshots (
    count_projection_snapshot_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    horizon text NOT NULL,
    park_id uuid NOT NULL,
    target_date date NOT NULL,
    as_of timestamp with time zone NOT NULL,
    projection_status text DEFAULT 'blocked'::text NOT NULL,
    source_contract_version text NOT NULL,
    source_hash text NOT NULL,
    base_anchor_ids_hash text NOT NULL,
    shifting_event_ids_hash text NOT NULL,
    row_count integer DEFAULT 0 NOT NULL,
    exception_count integer DEFAULT 0 NOT NULL,
    generated_by text NOT NULL,
    trace_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT count_projection_snapshots_contract_check CHECK ((btrim(source_contract_version) <> ''::text)),
    CONSTRAINT count_projection_snapshots_hash_check CHECK (((btrim(source_hash) <> ''::text) AND (btrim(base_anchor_ids_hash) <> ''::text) AND (btrim(shifting_event_ids_hash) <> ''::text))),
    CONSTRAINT count_projection_snapshots_horizon_check CHECK ((horizon = ANY (ARRAY['count_as_of'::text, 'feed_target_date'::text]))),
    CONSTRAINT count_projection_snapshots_row_counts_check CHECK (((row_count >= 0) AND (exception_count >= 0))),
    CONSTRAINT count_projection_snapshots_status_check CHECK ((projection_status = ANY (ARRAY['ready'::text, 'blocked'::text, 'stale'::text, 'failed'::text])))
);

--
-- Name: count_source_import_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.count_source_import_runs (
    count_source_import_run_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    source_system text NOT NULL,
    mode text DEFAULT 'execute'::text NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    source_ref text,
    source_rows_read integer DEFAULT 0 NOT NULL,
    base_anchor_rows integer DEFAULT 0 NOT NULL,
    shifting_event_rows integer DEFAULT 0 NOT NULL,
    replay_count integer DEFAULT 0 NOT NULL,
    failed_row_count integer DEFAULT 0 NOT NULL,
    trace_id text,
    last_error text,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT count_source_import_runs_counts_check CHECK (((source_rows_read >= 0) AND (base_anchor_rows >= 0) AND (shifting_event_rows >= 0) AND (replay_count >= 0) AND (failed_row_count >= 0))),
    CONSTRAINT count_source_import_runs_mode_check CHECK ((mode = ANY (ARRAY['dry_run'::text, 'execute'::text]))),
    CONSTRAINT count_source_import_runs_source_system_check CHECK ((source_system = ANY (ARRAY['physical_base_count'::text, 'manual_review'::text, 'import'::text, 'feed_shiftings_docx'::text, 'goatos_canonical'::text]))),
    CONSTRAINT count_source_import_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'completed'::text, 'failed'::text]))),
    CONSTRAINT count_source_import_runs_status_completion_check CHECK ((((status = 'running'::text) AND (completed_at IS NULL)) OR ((status = ANY (ARRAY['completed'::text, 'failed'::text])) AND (completed_at IS NOT NULL))))
);

--
-- Name: counts_shifting_readiness_evidence; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.counts_shifting_readiness_evidence (
    counts_shifting_readiness_evidence_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    subgate_id text NOT NULL,
    status text NOT NULL,
    evidence_ref text NOT NULL,
    blocker_reason text NOT NULL,
    implementation_ref text,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT counts_shifting_readiness_evidence_blocker_check CHECK ((btrim(blocker_reason) <> ''::text)),
    CONSTRAINT counts_shifting_readiness_evidence_ref_check CHECK ((btrim(evidence_ref) <> ''::text)),
    CONSTRAINT counts_shifting_readiness_evidence_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'blocked'::text, 'pending'::text]))),
    CONSTRAINT counts_shifting_readiness_evidence_subgate_id_check CHECK ((subgate_id = ANY (ARRAY['CSG1'::text, 'CSG2'::text, 'CSG3'::text, 'CSG4'::text, 'CSG5'::text, 'CSG6'::text, 'CSG7'::text, 'CSG8'::text, 'CSG9'::text, 'CSG10'::text])))
);

--
-- Name: counts_shifting_readiness_subgates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.counts_shifting_readiness_subgates (
    tenant_id uuid NOT NULL,
    subgate_id text NOT NULL,
    status text DEFAULT 'blocked'::text NOT NULL,
    owner text NOT NULL,
    evidence_ref text NOT NULL,
    blocker_reason text NOT NULL,
    implementation_ref text,
    last_checked_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT counts_shifting_readiness_blocker_check CHECK ((btrim(blocker_reason) <> ''::text)),
    CONSTRAINT counts_shifting_readiness_evidence_check CHECK ((btrim(evidence_ref) <> ''::text)),
    CONSTRAINT counts_shifting_readiness_owner_check CHECK ((btrim(owner) <> ''::text)),
    CONSTRAINT counts_shifting_readiness_status_check CHECK ((status = ANY (ARRAY['ready'::text, 'blocked'::text, 'pending'::text]))),
    CONSTRAINT counts_shifting_readiness_subgate_id_check CHECK ((subgate_id = ANY (ARRAY['CSG1'::text, 'CSG2'::text, 'CSG3'::text, 'CSG4'::text, 'CSG5'::text, 'CSG6'::text, 'CSG7'::text, 'CSG8'::text, 'CSG9'::text, 'CSG10'::text])))
);

--
-- Name: departments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.departments (
    department_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    code text NOT NULL,
    label text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT departments_code_check CHECK ((code ~ '^[a-z][a-z0-9_]*$'::text)),
    CONSTRAINT departments_label_check CHECK ((btrim(label) <> ''::text)),
    CONSTRAINT departments_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))
);

--
-- Name: domain_event_processed_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.domain_event_processed_events (
    tenant_id uuid NOT NULL,
    subscription_id text NOT NULL,
    event_id text NOT NULL,
    event_type text NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    delivery_attempt integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'processing'::text NOT NULL,
    attempt_count integer DEFAULT 1 NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT domain_event_processed_events_attempt_check CHECK ((attempt_count >= 1)),
    CONSTRAINT domain_event_processed_events_delivery_attempt_check CHECK ((delivery_attempt >= 0)),
    CONSTRAINT domain_event_processed_events_status_check CHECK ((status = ANY (ARRAY['processing'::text, 'effects_committed'::text, 'processed'::text, 'failed'::text])))
);

--
-- Name: farm_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.farm_profiles (
    location_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    farm_kind text,
    capacity integer,
    notes text DEFAULT ''::text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT farm_profiles_capacity_check CHECK (((capacity IS NULL) OR (capacity >= 0))),
    CONSTRAINT farm_profiles_kind_check CHECK (((farm_kind IS NULL) OR (farm_kind = ANY (ARRAY['core'::text, 'holding'::text, 'contract'::text])))),
    CONSTRAINT farm_profiles_row_version_check CHECK ((row_version >= 1))
);

--
-- Name: feed_direction_completions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.feed_direction_completions (
    completion_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    obligation_id uuid NOT NULL,
    batch_id uuid,
    shed_id uuid NOT NULL,
    ration_protocol_version_id uuid,
    sop_submission_item_id uuid,
    feed_inventory_lot_id uuid,
    quantity_fed numeric,
    quantity_unit text,
    head_count integer,
    fed_at timestamp with time zone NOT NULL,
    status text DEFAULT 'recorded'::text NOT NULL,
    verified_by uuid,
    verified_at timestamp with time zone,
    rejection_reason text,
    recorded_by uuid,
    idempotency_key text NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT feed_direction_completions_head_count_check CHECK (((head_count IS NULL) OR (head_count >= 0))),
    CONSTRAINT feed_direction_completions_quantity_check CHECK (((quantity_fed IS NULL) OR (quantity_fed > (0)::numeric))),
    CONSTRAINT feed_direction_completions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT feed_direction_completions_status_check CHECK ((status = ANY (ARRAY['recorded'::text, 'accepted'::text, 'rejected'::text, 'reversed'::text])))
);

--
-- Name: goat_custody_history; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goat_custody_history (
    custody_history_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    custodian_party_id uuid NOT NULL,
    from_location_id uuid,
    to_location_id uuid,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone,
    decision_id uuid,
    reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    CONSTRAINT goat_custody_valid_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: goat_display_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.goat_display_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: goat_identifiers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goat_identifiers (
    identifier_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    identifier_type text NOT NULL,
    identifier_value text NOT NULL,
    normalized_value text NOT NULL,
    scope_key text NOT NULL,
    is_primary_for_goat boolean DEFAULT false NOT NULL,
    status text NOT NULL,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone,
    source_system text,
    source_record_id text,
    normalizer_version text NOT NULL,
    confidence numeric,
    approved_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT goat_identifiers_confidence_check CHECK (((confidence IS NULL) OR ((confidence >= (0)::numeric) AND (confidence <= (1)::numeric)))),
    CONSTRAINT goat_identifiers_scope_key_check CHECK ((length(scope_key) > 0)),
    CONSTRAINT goat_identifiers_status_check CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text, 'disputed'::text, 'duplicate'::text, 'invalid'::text]))),
    CONSTRAINT goat_identifiers_type_check CHECK ((identifier_type = ANY (ARRAY['animal_identifier_1'::text, 'animal_identifier_2'::text]))),
    CONSTRAINT goat_identifiers_valid_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: goat_identity_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goat_identity_events (
    identity_event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    event_type text NOT NULL,
    event_version integer NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id uuid,
    source_system text,
    source_record_id text,
    payload jsonb NOT NULL,
    decision_id uuid,
    idempotency_key text NOT NULL,
    CONSTRAINT goat_identity_events_version_check CHECK ((event_version > 0))
);

--
-- Name: goat_location_history; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goat_location_history (
    location_history_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    from_location_id uuid,
    to_location_id uuid NOT NULL,
    reason text,
    occurred_at timestamp with time zone NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id uuid,
    source_record_id text
);

--
-- Name: goat_merge_links; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goat_merge_links (
    merge_link_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    survivor_goat_id uuid NOT NULL,
    merged_goat_id uuid NOT NULL,
    decision_id uuid NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid NOT NULL,
    CONSTRAINT goat_merge_links_distinct_goats_check CHECK ((merged_goat_id <> survivor_goat_id))
);

--
-- Name: goat_ownership; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goat_ownership (
    ownership_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    owner_party_id uuid NOT NULL,
    share_bps integer NOT NULL,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone,
    status text NOT NULL,
    decision_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    CONSTRAINT goat_ownership_share_bps_check CHECK (((share_bps >= 0) AND (share_bps <= 10000))),
    CONSTRAINT goat_ownership_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'pending_review'::text, 'shared_pending'::text]))),
    CONSTRAINT goat_ownership_valid_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: goats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goats (
    goat_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    display_id text DEFAULT public.next_goat_display_id() NOT NULL,
    species text DEFAULT 'goat'::text NOT NULL,
    breed text,
    breed_id uuid,
    sex text NOT NULL,
    approx_dob date,
    age_band text,
    lifecycle_status text NOT NULL,
    reproductive_status text,
    growth_cohort_tag text,
    management_stage text,
    health_status text,
    custodian_party_id uuid NOT NULL,
    current_location_id uuid,
    farm_id uuid,
    park_id uuid,
    shed_id uuid,
    cohort_id uuid,
    merged_into_goat_id uuid,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    dob date,
    dob_estimated boolean DEFAULT true NOT NULL,
    origin_type text,
    entry_date date,
    exited_at timestamp with time zone,
    exit_reason text,
    breeding_date date,
    last_delivery_date date,
    CONSTRAINT goats_display_id_format_check CHECK ((display_id ~ '^G-[0-9]{6,}$'::text)),
    CONSTRAINT goats_exit_reason_check CHECK (((exit_reason IS NULL) OR (exit_reason = ANY (ARRAY['sold'::text, 'died'::text, 'culled'::text, 'transferred'::text, 'lost'::text])))),
    CONSTRAINT goats_exited_lifecycle_check CHECK (((exited_at IS NULL) OR (lifecycle_status = ANY (ARRAY['dead'::text, 'sold'::text, 'culled'::text, 'transferred'::text, 'lost'::text, 'merged'::text, 'inactive'::text])))),
    CONSTRAINT goats_merge_redirect_shape_check CHECK (((merged_into_goat_id IS NULL) OR (merged_into_goat_id <> goat_id))),
    CONSTRAINT goats_origin_type_check CHECK (((origin_type IS NULL) OR (origin_type = ANY (ARRAY['birth'::text, 'procured'::text, 'imported'::text])))),
    CONSTRAINT goats_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT goats_sex_check CHECK ((sex = ANY (ARRAY['female'::text, 'male'::text]))),
    CONSTRAINT goats_species_check CHECK ((species = ANY (ARRAY['goat'::text, 'sheep'::text])))
);

--
-- Name: herd_register_goat_projection; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.herd_register_goat_projection (
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    display_id text NOT NULL,
    park_id uuid,
    farm_id uuid,
    current_location_id uuid,
    breed text,
    sex text NOT NULL,
    lifecycle_status text NOT NULL,
    is_kid boolean NOT NULL,
    is_untagged boolean NOT NULL,
    projected_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: herd_register_summary_projection; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.herd_register_summary_projection (
    herd_register_summary_projection_id bigint NOT NULL,
    tenant_id uuid NOT NULL,
    park_id uuid,
    farm_id uuid,
    current_location_id uuid,
    breed text,
    sex text NOT NULL,
    lifecycle_status text NOT NULL,
    active_count bigint DEFAULT 0 NOT NULL,
    adult_count bigint DEFAULT 0 NOT NULL,
    kid_count bigint DEFAULT 0 NOT NULL,
    untagged_kid_count bigint DEFAULT 0 NOT NULL,
    projected_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT herd_register_summary_projection_nonnegative_check CHECK (((active_count >= 0) AND (adult_count >= 0) AND (kid_count >= 0) AND (untagged_kid_count >= 0)))
);

--
-- Name: herd_register_summary_project_herd_register_summary_project_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.herd_register_summary_projection ALTER COLUMN herd_register_summary_projection_id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.herd_register_summary_project_herd_register_summary_project_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

--
-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.idempotency_keys (
    idempotency_key text NOT NULL,
    tenant_id uuid,
    scope text NOT NULL,
    request_hash text NOT NULL,
    status text NOT NULL,
    result_type text,
    result_id uuid,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    expires_at timestamp with time zone DEFAULT (now() + '90 days'::interval),
    CONSTRAINT idempotency_keys_completed_shape_check CHECK (((status <> 'completed'::text) OR (completed_at IS NOT NULL))),
    CONSTRAINT idempotency_keys_status_check CHECK ((status = ANY (ARRAY['started'::text, 'completed'::text, 'failed'::text])))
);

--
-- Name: identifier_policies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identifier_policies (
    policy_version text NOT NULL,
    identifier_type text NOT NULL,
    default_scope_type text NOT NULL,
    scope_required boolean NOT NULL,
    active_uniqueness text NOT NULL,
    auto_link_allowed boolean NOT NULL,
    primary_allowed boolean NOT NULL,
    unknown_scope_action text NOT NULL,
    missing_or_conflicting_scope_action text NOT NULL,
    normalizer_version text NOT NULL,
    format_validator_version text,
    invalid_value_action text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    approved_by uuid,
    CONSTRAINT identifier_policies_active_uniqueness_check CHECK ((active_uniqueness = ANY (ARRAY['global'::text, 'scoped'::text, 'non_unique'::text]))),
    CONSTRAINT identifier_policies_identifier_type_check CHECK ((identifier_type = ANY (ARRAY['animal_identifier_1'::text, 'animal_identifier_2'::text]))),
    CONSTRAINT identifier_policies_invalid_value_action_check CHECK ((invalid_value_action = ANY (ARRAY['review'::text, 'reject'::text]))),
    CONSTRAINT identifier_policies_missing_scope_action_check CHECK ((missing_or_conflicting_scope_action = ANY (ARRAY['review'::text, 'reject'::text]))),
    CONSTRAINT identifier_policies_unknown_scope_action_check CHECK ((unknown_scope_action = ANY (ARRAY['review'::text, 'reject'::text])))
);

--
-- Name: identifier_policy_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identifier_policy_versions (
    policy_version text NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    approved_at timestamp with time zone,
    approved_by uuid,
    CONSTRAINT identifier_policy_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'approved'::text, 'retired'::text])))
);

--
-- Name: identity_conflict_goats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_conflict_goats (
    conflict_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    role text DEFAULT 'affected'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: identity_conflict_source_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_conflict_source_records (
    conflict_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    source_system text NOT NULL,
    source_record_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: identity_conflicts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_conflicts (
    conflict_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    conflict_type text NOT NULL,
    severity text NOT NULL,
    state text NOT NULL,
    identifier_type text,
    identifier_value text,
    goat_ids uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    source_record_ids text[],
    evidence jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    resolved_by uuid,
    decision_id uuid,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT identity_conflicts_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT identity_conflicts_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT identity_conflicts_state_check CHECK ((state = ANY (ARRAY['open'::text, 'needs_field_check'::text, 'resolved'::text, 'rejected'::text, 'closed'::text]))),
    CONSTRAINT identity_conflicts_type_check CHECK ((conflict_type = ANY (ARRAY['duplicate_active_identifier'::text, 'duplicate_animal_identifier'::text, 'missing_required_identifier'::text, 'possible_duplicate_animal'::text, 'location_mismatch'::text, 'status_mismatch'::text])))
);

--
-- Name: identity_correction_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_correction_requests (
    correction_request_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    request_type text NOT NULL,
    state text NOT NULL,
    goat_id uuid,
    identifier_type text,
    identifier_value text,
    farm_id uuid,
    park_id uuid,
    shed_id uuid,
    cohort_id uuid,
    description text NOT NULL,
    evidence jsonb NOT NULL,
    requested_by uuid NOT NULL,
    assigned_reviewer_id uuid,
    decision_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT identity_correction_requests_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT identity_correction_requests_state_check CHECK ((state = ANY (ARRAY['open'::text, 'assigned'::text, 'needs_field_check'::text, 'approved'::text, 'rejected'::text, 'closed'::text]))),
    CONSTRAINT identity_correction_requests_type_check CHECK ((request_type = ANY (ARRAY['missing_animal_identifier'::text, 'animal_identifier_reused'::text, 'animal_identifier_conflict'::text, 'possible_duplicate'::text, 'wrong_location'::text, 'wrong_status'::text, 'field_verification_result'::text, 'identifier_seen_but_not_attached'::text])))
);

--
-- Name: identity_decision_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_decision_events (
    decision_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    event_id uuid NOT NULL,
    event_recorded_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: identity_decision_goats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_decision_goats (
    decision_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    role text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: identity_decision_identifiers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_decision_identifiers (
    decision_identifier_id uuid DEFAULT gen_random_uuid() NOT NULL,
    decision_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    identifier_id uuid,
    identifier_type text,
    identifier_value text,
    action text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT identity_decision_identifiers_action_check CHECK ((action = ANY (ARRAY['attach'::text, 'retire'::text, 'dispute'::text, 'transfer'::text, 'preserve'::text, 'reject'::text])))
);

--
-- Name: identity_decision_media; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_decision_media (
    decision_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    media_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: identity_decisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.identity_decisions (
    decision_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    decision_type text NOT NULL,
    decision_result text NOT NULL,
    decision_state text NOT NULL,
    decided_by_type text NOT NULL,
    decided_by uuid,
    policy_version text NOT NULL,
    source_record_ids text[],
    source_identifier_ids uuid[],
    source_goat_ids uuid[],
    source_event_ids uuid[],
    source_media_ids uuid[],
    model_version text,
    confidence numeric,
    reviewer_id uuid,
    evidence jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    approved_at timestamp with time zone,
    decided_at timestamp with time zone,
    CONSTRAINT identity_decisions_ai_not_approved_check CHECK (((decided_by_type <> 'ai_proposal'::text) OR (decision_state = ANY (ARRAY['proposed'::text, 'needs_review'::text])))),
    CONSTRAINT identity_decisions_confidence_check CHECK (((confidence IS NULL) OR ((confidence >= (0)::numeric) AND (confidence <= (1)::numeric)))),
    CONSTRAINT identity_decisions_decided_by_type_check CHECK ((decided_by_type = ANY (ARRAY['human'::text, 'system_rule'::text, 'import_policy'::text, 'ai_proposal'::text]))),
    CONSTRAINT identity_decisions_decision_state_check CHECK ((decision_state = ANY (ARRAY['proposed'::text, 'approved'::text, 'rejected'::text, 'needs_review'::text]))),
    CONSTRAINT identity_decisions_decision_type_check CHECK ((decision_type = ANY (ARRAY['create_goat'::text, 'attach_identifier'::text, 'retire_identifier'::text, 'mark_identifier_disputed'::text, 'merge_goats'::text, 'batch_merge_goats'::text, 'reject_match'::text, 'request_field_verification'::text, 'resolve_correction_request'::text, 'move_goat'::text, 'exit_goat'::text, 'stage_goat'::text, 'health_goat'::text, 'reproductive_goat'::text, 'identity_goat'::text])))
);

--
-- Name: inventory_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_items (
    item_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    item_code text NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    base_unit text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT inventory_items_category_check CHECK ((category = ANY (ARRAY['vaccine'::text, 'dewormer'::text, 'medicine'::text, 'feed'::text, 'supplement'::text, 'consumable'::text, 'other'::text]))),
    CONSTRAINT inventory_items_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT inventory_items_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'retired'::text])))
);

--
-- Name: inventory_stock; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock (
    stock_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    item_id uuid NOT NULL,
    location_id uuid NOT NULL,
    lot_code text,
    expiry_date date,
    quantity_in_stock numeric DEFAULT 0 NOT NULL,
    quantity_reserved numeric DEFAULT 0 NOT NULL,
    quantity_unit text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT inventory_stock_qty_check CHECK ((quantity_in_stock >= (0)::numeric)),
    CONSTRAINT inventory_stock_reserved_check CHECK ((quantity_reserved >= (0)::numeric)),
    CONSTRAINT inventory_stock_reserved_le_check CHECK ((quantity_reserved <= quantity_in_stock)),
    CONSTRAINT inventory_stock_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT inventory_stock_status_check CHECK ((status = ANY (ARRAY['active'::text, 'expired'::text, 'quarantined'::text, 'depleted'::text])))
);

--
-- Name: inventory_stock_movements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements (
    movement_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    lot_id uuid NOT NULL,
    item_id uuid NOT NULL,
    location_id uuid NOT NULL,
    movement_type text NOT NULL,
    quantity numeric NOT NULL,
    quantity_unit text NOT NULL,
    batch_id uuid,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id uuid,
    reason text,
    idempotency_key text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > (0)::numeric)),
    CONSTRAINT inventory_stock_movements_type_check CHECK ((movement_type = ANY (ARRAY['receive'::text, 'reserve'::text, 'consume'::text, 'release'::text, 'adjust'::text, 'expire'::text, 'transfer_out'::text, 'transfer_in'::text])))
);

--
-- Name: location_aliases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_aliases (
    alias_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    alias_code text NOT NULL,
    canonical_location_id uuid NOT NULL,
    source_context text NOT NULL,
    notes text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    retired_at timestamp with time zone,
    CONSTRAINT location_aliases_status_check CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text, 'review'::text])))
);

--
-- Name: location_capacity_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_capacity_records (
    capacity_record_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    capacity_kind text NOT NULL,
    capacity_value integer NOT NULL,
    effective_from date NOT NULL,
    effective_to date,
    source text NOT NULL,
    source_ref text,
    notes text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT location_capacity_records_kind_check CHECK ((capacity_kind = ANY (ARRAY['goat_occupancy'::text, 'quarantine'::text, 'feed_trial'::text, 'other'::text]))),
    CONSTRAINT location_capacity_records_positive_check CHECK ((capacity_value > 0)),
    CONSTRAINT location_capacity_records_source_check CHECK ((source = ANY (ARRAY['manual'::text, 'android_sop'::text, 'import'::text, 'sheds_db'::text]))),
    CONSTRAINT location_capacity_records_window_check CHECK (((effective_to IS NULL) OR (effective_to > effective_from)))
);

--
-- Name: location_operational_attributes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_operational_attributes (
    tenant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    usable_for_counts boolean DEFAULT true NOT NULL,
    usable_for_feed boolean DEFAULT true NOT NULL,
    usable_for_vaccination boolean DEFAULT true NOT NULL,
    usable_for_sop boolean DEFAULT true NOT NULL,
    is_holding boolean DEFAULT false NOT NULL,
    is_quarantine boolean DEFAULT false NOT NULL,
    is_icu boolean DEFAULT false NOT NULL,
    display_order integer DEFAULT 0 NOT NULL,
    notes text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: location_projection_invalidations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_projection_invalidations (
    invalidation_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    source_module text NOT NULL,
    projection_module text NOT NULL,
    reason text NOT NULL,
    affected_location_id uuid,
    source_ref text,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    acknowledged_at timestamp with time zone,
    CONSTRAINT location_projection_invalidations_projection_check CHECK ((projection_module = ANY (ARRAY['counts'::text, 'infra'::text, 'mortality'::text, 'feed'::text, 'vaccination'::text, 'other'::text]))),
    CONSTRAINT location_projection_invalidations_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'acknowledged'::text, 'superseded'::text])))
);

--
-- Name: location_review_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.location_review_items (
    review_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    review_type text NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    source_context text,
    source_label text,
    normalized_source_label text,
    canonical_location_id uuid,
    candidate_location_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    evidence_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    evidence_hash text NOT NULL,
    created_by uuid,
    resolved_by uuid,
    resolution_notes text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT location_review_items_candidate_array_check CHECK ((jsonb_typeof(candidate_location_ids) = 'array'::text)),
    CONSTRAINT location_review_items_evidence_object_check CHECK ((jsonb_typeof(evidence_json) = 'object'::text)),
    CONSTRAINT location_review_items_status_check CHECK ((status = ANY (ARRAY['open'::text, 'resolved'::text, 'dismissed'::text]))),
    CONSTRAINT location_review_items_type_check CHECK ((review_type = ANY (ARRAY['unknown_alias'::text, 'alias_conflict'::text, 'parent_type_conflict'::text, 'capacity_conflict'::text, 'retire_blocked'::text, 'usage_conflict'::text])))
);

--
-- Name: locations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.locations (
    location_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    location_type text NOT NULL,
    location_code text,
    name text NOT NULL,
    parent_location_id uuid,
    country text DEFAULT 'IN'::text NOT NULL,
    state_region text,
    district text,
    pincode text,
    lat numeric,
    lng numeric,
    timezone text DEFAULT 'Asia/Kolkata'::text NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    display_order integer DEFAULT 0 NOT NULL,
    operational_notes text,
    retired_at timestamp with time zone,
    retired_by uuid,
    CONSTRAINT locations_lat_check CHECK (((lat IS NULL) OR ((lat >= ('-90'::integer)::numeric) AND (lat <= (90)::numeric)))),
    CONSTRAINT locations_lng_check CHECK (((lng IS NULL) OR ((lng >= ('-180'::integer)::numeric) AND (lng <= (180)::numeric)))),
    CONSTRAINT locations_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'staging'::text, 'review'::text]))),
    CONSTRAINT locations_type_check CHECK ((location_type = ANY (ARRAY['farm'::text, 'park'::text, 'shed'::text, 'cohort'::text, 'pen'::text, 'unknown'::text])))
);

--
-- Name: movement_commands; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.movement_commands (
    command_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    submission_id uuid NOT NULL,
    command_type text NOT NULL,
    state text DEFAULT 'accepted'::text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT movement_commands_payload_object_check CHECK ((jsonb_typeof(payload) = 'object'::text)),
    CONSTRAINT movement_commands_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'accepted'::text, 'failed'::text]))),
    CONSTRAINT movement_commands_type_check CHECK ((command_type = 'shifting.apply'::text))
);

--
-- Name: notification_delivery_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notification_delivery_attempts (
    attempt_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    notification_request_id uuid NOT NULL,
    attempt_no integer NOT NULL,
    channel text NOT NULL,
    attempted_at timestamp with time zone NOT NULL,
    result text NOT NULL,
    provider_message_id text,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT notification_delivery_attempts_attempt_no_check CHECK ((attempt_no > 0)),
    CONSTRAINT notification_delivery_attempts_result_check CHECK ((result = ANY (ARRAY['sent'::text, 'failed'::text])))
);

--
-- Name: notification_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.notification_requests (
    notification_request_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    calendar_event_id text NOT NULL,
    target_type text NOT NULL,
    target_id uuid,
    notification_type text NOT NULL,
    channel text NOT NULL,
    recipient_ref text,
    title text NOT NULL,
    body text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    requested_by uuid,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    read_at timestamp with time zone,
    failure_reason text,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    trace_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    delivery_attempts integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone,
    leased_at timestamp with time zone,
    lease_token uuid,
    delivered_by text,
    CONSTRAINT notification_requests_channel_check CHECK ((channel = ANY (ARRAY['local-stub'::text, 'push_fcm'::text, 'slack'::text, 'email'::text, 'webhook'::text, 'incident'::text, 'opsgenie'::text, 'pagerduty'::text]))),
    CONSTRAINT notification_requests_context_object_check CHECK ((jsonb_typeof(context) = 'object'::text)),
    CONSTRAINT notification_requests_delivery_attempts_check CHECK ((delivery_attempts >= 0)),
    CONSTRAINT notification_requests_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'sending'::text, 'sent'::text, 'failed'::text, 'exhausted'::text, 'suppressed'::text, 'read'::text]))),
    CONSTRAINT notification_requests_type_check CHECK ((notification_type = ANY (ARRAY['reminder'::text, 'nudge'::text, 'escalation'::text, 'verification_pending'::text, 'verification_approved'::text, 'verification_closed'::text, 'rework'::text, 'advance_notice'::text, 'due_today'::text])))
);

--
-- Name: obligation_batches; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.obligation_batches (
    batch_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    session text,
    planned_date date,
    window_start timestamp with time zone,
    window_end timestamp with time zone,
    status text DEFAULT 'planned'::text NOT NULL,
    estimated_targets integer DEFAULT 0 NOT NULL,
    planned_quantity numeric,
    reserved_quantity numeric DEFAULT 0 NOT NULL,
    used_quantity numeric DEFAULT 0 NOT NULL,
    quantity_unit text,
    primary_inventory_lot_id uuid,
    sop_task_id uuid,
    conducted_by uuid,
    proof_ref text,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT obligation_batches_reserved_check CHECK ((reserved_quantity >= (0)::numeric)),
    CONSTRAINT obligation_batches_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT obligation_batches_scope_type_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text]))),
    CONSTRAINT obligation_batches_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'in_progress'::text, 'completed'::text, 'superseded'::text, 'canceled'::text]))),
    CONSTRAINT obligation_batches_used_check CHECK ((used_quantity >= (0)::numeric))
);

--
-- Name: obligation_escalations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.obligation_escalations (
    escalation_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    obligation_id uuid NOT NULL,
    level integer NOT NULL,
    escalated_to_user_id uuid,
    escalated_to_role text,
    reason text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    opened_at timestamp with time zone DEFAULT now() NOT NULL,
    acknowledged_at timestamp with time zone,
    resolved_at timestamp with time zone,
    acknowledged_by uuid,
    resolved_by uuid,
    acknowledgement_note text DEFAULT ''::text NOT NULL,
    resolution_note text DEFAULT ''::text NOT NULL,
    CONSTRAINT obligation_escalations_level_check CHECK ((level >= 1)),
    CONSTRAINT obligation_escalations_role_check CHECK (((escalated_to_role IS NULL) OR (escalated_to_role = ANY (ARRAY['admin'::text, 'park_head'::text, 'pc_director'::text, 'operator'::text, 'verifier'::text, 'ceo_internal'::text])))),
    CONSTRAINT obligation_escalations_status_check CHECK ((status = ANY (ARRAY['open'::text, 'acknowledged'::text, 'resolved'::text, 'expired'::text])))
);

--
-- Name: obligation_goat_shift_watermarks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.obligation_goat_shift_watermarks (
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    last_occurred_at timestamp with time zone NOT NULL,
    last_event_id text NOT NULL,
    last_scope_type text NOT NULL,
    last_scope_id uuid NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: obligation_instances; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.obligation_instances (
    obligation_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    batch_id uuid,
    target_type text NOT NULL,
    target_id uuid NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    due_at timestamp with time zone NOT NULL,
    window_start timestamp with time zone,
    window_end timestamp with time zone,
    status text DEFAULT 'scheduled'::text NOT NULL,
    sop_task_id uuid,
    idempotency_key text NOT NULL,
    generated_by_trigger_id uuid,
    sequence integer DEFAULT 1 NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    batching_hold_count integer DEFAULT 0,
    first_batching_hold_until timestamp with time zone,
    CONSTRAINT obligation_instances_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT obligation_instances_scope_type_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text]))),
    CONSTRAINT obligation_instances_status_check CHECK ((status = ANY (ARRAY['scheduled'::text, 'due'::text, 'in_progress'::text, 'deferred'::text, 'completed'::text, 'missed'::text, 'waived'::text, 'canceled'::text, 'superseded'::text]))),
    CONSTRAINT obligation_instances_target_type_check CHECK ((target_type = ANY (ARRAY['goat'::text, 'cohort'::text, 'shed'::text, 'park'::text, 'tenant'::text]))),
    CONSTRAINT obligation_instances_window_check CHECK (((window_end IS NULL) OR (window_start IS NULL) OR (window_end >= window_start)))
);

--
-- Name: obligation_status_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.obligation_status_events (
    obligation_event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    obligation_id uuid NOT NULL,
    event_type text NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    recorded_at timestamp with time zone DEFAULT now() NOT NULL,
    actor_id uuid,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    idempotency_key text NOT NULL,
    CONSTRAINT obligation_status_events_type_check CHECK ((event_type = ANY (ARRAY['scheduled'::text, 'became_due'::text, 'dispatched'::text, 'in_progress'::text, 'completed'::text, 'missed'::text, 'waived'::text, 'escalated'::text, 'escalation_acknowledged'::text, 'escalation_resolved'::text, 'canceled'::text, 'deferred'::text, 'rescoped'::text])))
);

--
-- Name: org_role_catalog; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.org_role_catalog (
    role_key text NOT NULL,
    tier_code text NOT NULL,
    vertical_code text,
    is_legacy boolean DEFAULT false NOT NULL,
    label text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT org_role_catalog_role_key_shape_check CHECK ((role_key ~ '^[a-z][a-z0-9_]*$'::text)),
    CONSTRAINT org_role_catalog_vertical_or_legacy_check CHECK (((vertical_code IS NOT NULL) OR is_legacy))
);

--
-- Name: org_tiers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.org_tiers (
    tier_code text NOT NULL,
    label text NOT NULL,
    rank smallint NOT NULL
);

--
-- Name: org_verticals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.org_verticals (
    vertical_code text NOT NULL,
    label text NOT NULL,
    sort_order smallint NOT NULL
);

--
-- Name: orgs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.orgs (
    party_id uuid NOT NULL,
    org_type text NOT NULL,
    legal_name text,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT orgs_org_type_check CHECK ((org_type = ANY (ARRAY['mesha'::text, 'farm_operator'::text, 'vendor'::text, 'franchisee'::text, 'lender'::text, 'partner'::text]))),
    CONSTRAINT orgs_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'review'::text])))
);

--
-- Name: outbox_dlq_actions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.outbox_dlq_actions (
    action_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    action text NOT NULL,
    request_hash text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    outbox_ids text[] NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    updated_count bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT outbox_dlq_actions_action_check CHECK ((action = ANY (ARRAY['replay'::text, 'discard'::text]))),
    CONSTRAINT outbox_dlq_actions_status_check CHECK ((status = ANY (ARRAY['running'::text, 'completed'::text]))),
    CONSTRAINT outbox_dlq_actions_updated_count_check CHECK ((updated_count >= 0))
);

--
-- Name: outbox_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.outbox_messages (
    outbox_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    event_id uuid NOT NULL,
    event_type text NOT NULL,
    schema_version text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    topic text NOT NULL,
    payload jsonb NOT NULL,
    headers jsonb NOT NULL,
    idempotency_key text NOT NULL,
    trace_id text,
    status text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone,
    last_error text,
    published_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    replay_count integer DEFAULT 0 NOT NULL,
    CONSTRAINT outbox_messages_attempt_count_check CHECK ((attempt_count >= 0)),
    CONSTRAINT outbox_messages_replay_count_check CHECK ((replay_count >= 0)),
    CONSTRAINT outbox_messages_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'publishing'::text, 'published'::text, 'failed'::text, 'dead_letter'::text, 'discarded'::text])))
);

--
-- Name: park_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.park_profiles (
    location_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    park_code text,
    capacity integer,
    notes text DEFAULT ''::text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT park_profiles_capacity_check CHECK (((capacity IS NULL) OR (capacity >= 0))),
    CONSTRAINT park_profiles_row_version_check CHECK ((row_version >= 1))
);

--
-- Name: parties; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.parties (
    party_id uuid DEFAULT gen_random_uuid() NOT NULL,
    party_type text NOT NULL,
    display_name text NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT parties_party_type_check CHECK ((party_type = ANY (ARRAY['org'::text, 'person'::text, 'token_pool'::text, 'system'::text]))),
    CONSTRAINT parties_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'review'::text])))
);

--
-- Name: position_module_duties; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.position_module_duties (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    position_code text NOT NULL,
    module_code text NOT NULL,
    duty_type text NOT NULL,
    capability_code text,
    effective_from timestamp with time zone DEFAULT now() NOT NULL,
    effective_to timestamp with time zone,
    status text DEFAULT 'active'::text NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT position_module_duties_duty_type_check CHECK ((duty_type = ANY (ARRAY['execute'::text, 'verify'::text, 'manage'::text, 'support'::text]))),
    CONSTRAINT position_module_duties_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))
);

--
-- Name: procurement_hf_vaccination_evidence; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.procurement_hf_vaccination_evidence (
    evidence_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    load_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    dose_code text NOT NULL,
    administered_at timestamp with time zone NOT NULL,
    vaccine_name text DEFAULT ''::text NOT NULL,
    lot_number text DEFAULT ''::text NOT NULL,
    proof_ref_id uuid,
    source_ref text DEFAULT ''::text NOT NULL,
    review_status text DEFAULT 'imported'::text NOT NULL,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    review_reason text DEFAULT ''::text NOT NULL,
    idempotency_key text NOT NULL,
    imported_by uuid,
    imported_at timestamp with time zone DEFAULT now() NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT procurement_hf_vaccination_evidence_dose_code_check CHECK ((btrim(dose_code) <> ''::text)),
    CONSTRAINT procurement_hf_vaccination_evidence_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT procurement_hf_vaccination_evidence_review_status_check CHECK ((review_status = ANY (ARRAY['imported'::text, 'trusted'::text, 'rejected'::text, 'conflicting'::text, 'duplicate'::text]))),
    CONSTRAINT procurement_hf_vaccination_evidence_row_version_check CHECK ((row_version >= 1))
);

--
-- Name: procurement_load_goats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.procurement_load_goats (
    load_goat_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    load_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    animal_identifier_1 text,
    animal_identifier_2 text,
    selection_state text DEFAULT 'candidate'::text NOT NULL,
    selection_reason text DEFAULT ''::text NOT NULL,
    current_state text DEFAULT 'source_candidate'::text NOT NULL,
    source_entry_state text DEFAULT 'pending'::text NOT NULL,
    source_entry_ref text,
    ownership_state text DEFAULT 'pending'::text NOT NULL,
    health_state text DEFAULT 'pending'::text NOT NULL,
    warmup_started_at timestamp with time zone,
    warmup_ended_at timestamp with time zone,
    warmup_days integer,
    holding_location_id uuid,
    loaded_at timestamp with time zone,
    arrived_at timestamp with time zone,
    intake_accepted_at timestamp with time zone,
    exit_reason text,
    proof_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    purpose text DEFAULT 'unspecified'::text NOT NULL,
    CONSTRAINT procurement_load_goats_current_state_check CHECK ((current_state = ANY (ARRAY['source_holding'::text, 'source_warmup'::text, 'source_candidate'::text, 'source_health_pending'::text, 'source_health_passed'::text, 'source_health_failed'::text, 'source_rejected'::text, 'pre_dispatch_pending'::text, 'pre_dispatch_accepted'::text, 'pre_dispatch_rejected'::text, 'pre_dispatch_deferred'::text, 'pre_dispatch_blocked'::text, 'dispatch_ready'::text, 'loading_pending'::text, 'loaded'::text, 'in_transit'::text, 'arrival_review_pending'::text, 'arrival_accepted'::text, 'arrival_rejected'::text, 'accepted_herd_intake'::text, 'dead'::text, 'sold'::text, 'lost'::text, 'canceled'::text]))),
    CONSTRAINT procurement_load_goats_exit_reason_check CHECK (((exit_reason IS NULL) OR (exit_reason = ANY (ARRAY['died'::text, 'sold'::text, 'lost'::text, 'canceled'::text])))),
    CONSTRAINT procurement_load_goats_health_state_check CHECK ((health_state = ANY (ARRAY['pending'::text, 'passed'::text, 'failed'::text, 'deferred'::text]))),
    CONSTRAINT procurement_load_goats_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT procurement_load_goats_ownership_state_check CHECK ((ownership_state = ANY (ARRAY['pending'::text, 'shared_pending'::text, 'mesha_owned'::text, 'blocked'::text, 'not_owned'::text, 'settled'::text]))),
    CONSTRAINT procurement_load_goats_proof_refs_array_check CHECK ((jsonb_typeof(proof_refs) = 'array'::text)),
    CONSTRAINT procurement_load_goats_purpose_check CHECK ((purpose = ANY (ARRAY['breeding'::text, 'fattening'::text, 'non_breeding'::text, 'unspecified'::text]))),
    CONSTRAINT procurement_load_goats_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT procurement_load_goats_selection_state_check CHECK ((selection_state = ANY (ARRAY['source_only'::text, 'candidate'::text, 'purchased'::text, 'accepted'::text, 'rejected'::text, 'deferred'::text, 'blocked'::text, 'loaded'::text, 'arrival_accepted'::text, 'arrival_rejected'::text, 'accepted_herd_intake'::text, 'dead'::text, 'sold'::text, 'lost'::text]))),
    CONSTRAINT procurement_load_goats_source_entry_state_check CHECK ((source_entry_state = ANY (ARRAY['pending'::text, 'accepted'::text, 'blocked'::text]))),
    CONSTRAINT procurement_load_goats_warmup_days_check CHECK (((warmup_days IS NULL) OR (warmup_days >= 0))),
    CONSTRAINT procurement_load_goats_warmup_window_check CHECK (((warmup_ended_at IS NULL) OR (warmup_started_at IS NULL) OR (warmup_ended_at >= warmup_started_at)))
);

--
-- Name: procurement_loads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.procurement_loads (
    load_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    source_party_id uuid NOT NULL,
    source_location_id uuid,
    expected_count integer DEFAULT 0 NOT NULL,
    purchase_date date,
    planned_dispatch_at timestamp with time zone,
    status text DEFAULT 'source_warmup'::text NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    idempotency_key text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT procurement_loads_context_object_check CHECK ((jsonb_typeof(context) = 'object'::text)),
    CONSTRAINT procurement_loads_expected_count_check CHECK ((expected_count >= 0)),
    CONSTRAINT procurement_loads_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT procurement_loads_status_check CHECK ((status = ANY (ARRAY['source_warmup'::text, 'health_pending'::text, 'pre_dispatch_pending'::text, 'dispatch_ready'::text, 'in_transit'::text, 'arrival_review'::text, 'accepted_intake'::text, 'rejected'::text, 'deferred'::text, 'blocked'::text, 'canceled'::text])))
);

--
-- Name: procurement_pc_handoffs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.procurement_pc_handoffs (
    handoff_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    load_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    accepted_at timestamp with time zone NOT NULL,
    park_location_id uuid NOT NULL,
    shed_location_id uuid NOT NULL,
    entry_date date NOT NULL,
    trusted_vaccination_history jsonb DEFAULT '[]'::jsonb NOT NULL,
    intake_health_signal text,
    event_status text DEFAULT 'pending'::text NOT NULL,
    idempotency_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT procurement_pc_handoffs_history_array_check CHECK ((jsonb_typeof(trusted_vaccination_history) = 'array'::text)),
    CONSTRAINT procurement_pc_handoffs_signal_check CHECK (((intake_health_signal IS NULL) OR (intake_health_signal = ANY (ARRAY['clear'::text, 'defer'::text, 'quarantine'::text, 'review'::text])))),
    CONSTRAINT procurement_pc_handoffs_status_check CHECK ((event_status = ANY (ARRAY['pending'::text, 'emitted'::text, 'canceled'::text])))
);

--
-- Name: procurement_source_health_checks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.procurement_source_health_checks (
    health_check_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    load_id uuid NOT NULL,
    health_state text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    checked_by uuid,
    checked_at timestamp with time zone NOT NULL,
    proof_ref_id uuid,
    sop_task_id uuid,
    idempotency_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT procurement_source_health_checks_state_check CHECK ((health_state = ANY (ARRAY['passed'::text, 'failed'::text, 'deferred'::text])))
);

--
-- Name: proof_artifacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.proof_artifacts (
    proof_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    storage_provider text NOT NULL,
    object_key text NOT NULL,
    content_hash text DEFAULT ''::text NOT NULL,
    mime_type text DEFAULT ''::text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    duration_ms bigint,
    upload_state text DEFAULT 'pending'::text NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    subject_type text NOT NULL,
    subject_id uuid,
    proof_type text NOT NULL,
    uploaded_by uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    uploaded_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    idempotency_key text,
    request_fingerprint text DEFAULT ''::text NOT NULL,
    CONSTRAINT proof_artifacts_duration_check CHECK (((duration_ms IS NULL) OR (duration_ms >= 0))),
    CONSTRAINT proof_artifacts_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT proof_artifacts_object_key_check CHECK ((btrim(object_key) <> ''::text)),
    CONSTRAINT proof_artifacts_proof_type_check CHECK ((proof_type = ANY (ARRAY['photo'::text, 'video'::text, 'attachment'::text]))),
    CONSTRAINT proof_artifacts_provider_check CHECK ((storage_provider = ANY (ARRAY['local'::text, 'gcs'::text]))),
    CONSTRAINT proof_artifacts_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT proof_artifacts_scope_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text, 'batch'::text, 'task'::text, 'goat'::text]))),
    CONSTRAINT proof_artifacts_size_check CHECK ((size_bytes >= 0)),
    CONSTRAINT proof_artifacts_subject_check CHECK ((subject_type = ANY (ARRAY['batch'::text, 'goat'::text, 'shed'::text, 'task'::text, 'vial_lot'::text, 'administration'::text, 'other'::text]))),
    CONSTRAINT proof_artifacts_upload_state_check CHECK ((upload_state = ANY (ARRAY['pending'::text, 'uploading'::text, 'completed'::text, 'failed'::text])))
);

--
-- Name: protocol_definitions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.protocol_definitions (
    protocol_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT protocol_definitions_category_check CHECK ((category = ANY (ARRAY['vaccination'::text, 'deworming'::text, 'biosecurity'::text, 'feed_water_testing'::text, 'panel_cleaning'::text, 'sanitization'::text, 'fire_safety'::text, 'sop_video'::text, 'stock_check'::text, 'director_reporting'::text, 'feed_direction'::text]))),
    CONSTRAINT protocol_definitions_code_format_check CHECK ((code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'::text)),
    CONSTRAINT protocol_definitions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT protocol_definitions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'retired'::text])))
);

--
-- Name: protocol_rule_dimensions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.protocol_rule_dimensions (
    protocol_rule_dimension_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    category text NOT NULL,
    ruleset_family text DEFAULT ''::text NOT NULL,
    matrix_row_id text DEFAULT ''::text NOT NULL,
    selector_key text NOT NULL,
    dose_code text DEFAULT ''::text NOT NULL,
    source_dose_code text DEFAULT ''::text NOT NULL,
    vaccine_code text DEFAULT ''::text NOT NULL,
    vaccine_type text DEFAULT ''::text NOT NULL,
    pathogen_class text DEFAULT ''::text NOT NULL,
    compatibility_group text DEFAULT ''::text NOT NULL,
    species text DEFAULT 'all'::text NOT NULL,
    animal_stage text DEFAULT 'all'::text NOT NULL,
    sex text DEFAULT 'all'::text NOT NULL,
    breed text DEFAULT 'all'::text NOT NULL,
    lifecycle text DEFAULT 'alive'::text NOT NULL,
    health text DEFAULT 'any'::text NOT NULL,
    reproductive text DEFAULT 'any'::text NOT NULL,
    min_age_days integer,
    max_age_days integer,
    trigger_type text DEFAULT ''::text NOT NULL,
    sequence integer DEFAULT 0 NOT NULL,
    offset_days integer DEFAULT 0 NOT NULL,
    due_window_days integer DEFAULT 0 NOT NULL,
    min_gap_days integer DEFAULT 0 NOT NULL,
    repeat text DEFAULT 'none'::text NOT NULL,
    catch_up text DEFAULT 'immediate'::text NOT NULL,
    max_delay_days integer DEFAULT 0 NOT NULL,
    revaccination_interval_days integer DEFAULT 0 NOT NULL,
    eligibility_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    vaccine_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    schedule_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT protocol_rule_dimensions_animal_stage_check CHECK ((animal_stage <> ''::text)),
    CONSTRAINT protocol_rule_dimensions_breed_check CHECK ((breed <> ''::text)),
    CONSTRAINT protocol_rule_dimensions_sex_check CHECK ((sex = ANY (ARRAY['female'::text, 'male'::text, 'all'::text]))),
    CONSTRAINT protocol_rule_dimensions_species_check CHECK ((species <> ''::text))
);

--
-- Name: protocol_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.protocol_rules (
    rule_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    dose_code text NOT NULL,
    sequence integer DEFAULT 1 NOT NULL,
    trigger_type text NOT NULL,
    offset_days integer DEFAULT 0 NOT NULL,
    due_window_days integer DEFAULT 0 NOT NULL,
    min_gap_days integer DEFAULT 0 NOT NULL,
    repeat text DEFAULT 'none'::text NOT NULL,
    repeat_until_after_age text,
    catch_up text DEFAULT 'pc_approval'::text NOT NULL,
    eligibility_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    sop_version_id uuid,
    proof_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    withdrawal_days integer,
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT protocol_rules_catch_up_check CHECK ((catch_up = ANY (ARRAY['immediate'::text, 'next_cycle'::text, 'pc_approval'::text, 'defer'::text]))),
    CONSTRAINT protocol_rules_gap_check CHECK ((min_gap_days >= 0)),
    CONSTRAINT protocol_rules_offset_check CHECK ((offset_days >= 0)),
    CONSTRAINT protocol_rules_repeat_check CHECK ((repeat = ANY (ARRAY['none'::text, 'every_n_days'::text, 'yearly'::text]))),
    CONSTRAINT protocol_rules_trigger_type_check CHECK ((trigger_type = ANY (ARRAY['birth_age'::text, 'post_arrival'::text, 'calendar'::text, 'after_previous_completion'::text, 'manual_campaign'::text]))),
    CONSTRAINT protocol_rules_window_check CHECK ((due_window_days >= 0))
);

--
-- Name: protocol_triggers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.protocol_triggers (
    trigger_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    trigger_type text NOT NULL,
    trigger_config jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT protocol_triggers_type_check CHECK ((trigger_type = ANY (ARRAY['schedule'::text, 'goat_lifecycle'::text, 'location_event'::text, 'manual'::text, 'upstream_completion'::text])))
);

--
-- Name: protocol_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.protocol_versions (
    protocol_version_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_id uuid NOT NULL,
    scope_type text DEFAULT 'tenant'::text NOT NULL,
    scope_id uuid,
    version integer NOT NULL,
    version_label text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    effective_from date NOT NULL,
    effective_to date,
    rule_dsl jsonb DEFAULT '{}'::jsonb NOT NULL,
    proof_policy jsonb DEFAULT '{}'::jsonb NOT NULL,
    sop_version_id uuid,
    drafted_by uuid,
    published_by uuid,
    published_at timestamp with time zone,
    retired_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT protocol_versions_effective_range_check CHECK (((effective_to IS NULL) OR (effective_to > effective_from))),
    CONSTRAINT protocol_versions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT protocol_versions_scope_shape_check CHECK ((((scope_type = 'tenant'::text) AND (scope_id IS NULL)) OR ((scope_type = 'park'::text) AND (scope_id IS NOT NULL)))),
    CONSTRAINT protocol_versions_scope_type_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'park'::text]))),
    CONSTRAINT protocol_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'retired'::text]))),
    CONSTRAINT protocol_versions_version_check CHECK ((version > 0))
);

--
-- Name: reminder_cadence_progress; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reminder_cadence_progress (
    tenant_id uuid NOT NULL,
    cursor_due_at timestamp with time zone DEFAULT to_timestamp((0)::double precision) NOT NULL,
    cursor_event_id text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: seed_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.seed_runs (
    seed_run_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    command text NOT NULL,
    state text NOT NULL,
    detail jsonb DEFAULT '{}'::jsonb NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    CONSTRAINT seed_runs_state_check CHECK ((state = ANY (ARRAY['loading'::text, 'generating'::text, 'verified'::text, 'failed'::text])))
);

--
-- Name: shed_lifecycle_status_lookup; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.shed_lifecycle_status_lookup (
    shed_lifecycle_status_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    status_code text NOT NULL,
    name text NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT shed_lifecycle_status_lookup_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'retired'::text])))
);

--
-- Name: shed_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.shed_profiles (
    location_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    animal_stage_id uuid,
    shed_lifecycle_status_id uuid,
    sex text,
    capacity integer,
    has_icu boolean DEFAULT false NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT shed_profiles_capacity_check CHECK (((capacity IS NULL) OR (capacity >= 0))),
    CONSTRAINT shed_profiles_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT shed_profiles_sex_check CHECK (((sex IS NULL) OR (sex = ANY (ARRAY['female'::text, 'male'::text, 'mixed'::text]))))
);

--
-- Name: shifting_event_impacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.shifting_event_impacts (
    shifting_event_impact_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    shifting_event_id uuid NOT NULL,
    grain_key text NOT NULL,
    breed_id uuid,
    breed_key text NOT NULL,
    breed_label text NOT NULL,
    stage_tag text,
    age_class text,
    sex text,
    head_count integer NOT NULL,
    pregnant_count integer DEFAULT 0 NOT NULL,
    lactating_count integer DEFAULT 0 NOT NULL,
    warmup_count integer DEFAULT 0 NOT NULL,
    risk_flags jsonb DEFAULT '{}'::jsonb NOT NULL,
    ration_context_resolution_state text DEFAULT 'unresolved'::text NOT NULL,
    ration_context_ref text,
    blocker_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT shifting_event_impacts_breed_key_check CHECK ((btrim(breed_key) <> ''::text)),
    CONSTRAINT shifting_event_impacts_grain_key_check CHECK ((btrim(grain_key) <> ''::text)),
    CONSTRAINT shifting_event_impacts_head_count_check CHECK ((head_count > 0)),
    CONSTRAINT shifting_event_impacts_resolution_check CHECK ((ration_context_resolution_state = ANY (ARRAY['resolved'::text, 'unresolved'::text, 'blocked'::text, 'not_required'::text]))),
    CONSTRAINT shifting_event_impacts_risk_counts_check CHECK (((pregnant_count >= 0) AND (lactating_count >= 0) AND (warmup_count >= 0) AND (pregnant_count <= head_count) AND (lactating_count <= head_count) AND (warmup_count <= head_count))),
    CONSTRAINT shifting_event_impacts_risk_flags_object_check CHECK ((jsonb_typeof(risk_flags) = 'object'::text))
);

--
-- Name: shifting_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.shifting_events (
    shifting_event_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    logical_shifting_event_key text NOT NULL,
    priority text NOT NULL,
    category text NOT NULL,
    source_park_id uuid,
    source_shed_id uuid,
    destination_park_id uuid NOT NULL,
    destination_shed_id uuid NOT NULL,
    raised_at timestamp with time zone NOT NULL,
    effective_at timestamp with time zone NOT NULL,
    authorized_at timestamp with time zone,
    authorized_by uuid,
    authorization_state text DEFAULT 'pending'::text NOT NULL,
    verification_state text DEFAULT 'unverified'::text NOT NULL,
    event_status text DEFAULT 'pending'::text NOT NULL,
    source_system text NOT NULL,
    source_ref text NOT NULL,
    proof_ref text,
    payload_hash text NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT shifting_events_auth_state_check CHECK ((authorization_state = ANY (ARRAY['pending'::text, 'authorized'::text, 'rejected'::text]))),
    CONSTRAINT shifting_events_category_check CHECK ((category = ANY (ARRAY['routine'::text, 'high_priority'::text, 'pregnancy'::text, 'warmup'::text, 'medical'::text, 'quarantine'::text, 'other'::text]))),
    CONSTRAINT shifting_events_idem_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT shifting_events_key_check CHECK ((btrim(logical_shifting_event_key) <> ''::text)),
    CONSTRAINT shifting_events_payload_hash_check CHECK ((btrim(payload_hash) <> ''::text)),
    CONSTRAINT shifting_events_priority_check CHECK ((priority = ANY (ARRAY['normal'::text, 'high'::text, 'emergency'::text]))),
    CONSTRAINT shifting_events_source_check CHECK ((source_system = ANY (ARRAY['feed_shiftings_docx'::text, 'manual_review'::text, 'import'::text, 'goatos_canonical'::text]))),
    CONSTRAINT shifting_events_source_ref_check CHECK ((btrim(source_ref) <> ''::text)),
    CONSTRAINT shifting_events_status_check CHECK ((event_status = ANY (ARRAY['pending'::text, 'authorized'::text, 'applied'::text, 'rejected'::text, 'canceled'::text, 'unresolved'::text]))),
    CONSTRAINT shifting_events_verification_state_check CHECK ((verification_state = ANY (ARRAY['unverified'::text, 'verified'::text, 'rejected'::text])))
);

--
-- Name: sop_definitions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_definitions (
    sop_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT sop_definitions_code_check CHECK ((code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'::text)),
    CONSTRAINT sop_definitions_name_check CHECK ((btrim(name) <> ''::text)),
    CONSTRAINT sop_definitions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT sop_definitions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'retired'::text])))
);

--
-- Name: sop_submission_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_submission_items (
    item_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    submission_id uuid NOT NULL,
    task_id uuid NOT NULL,
    goat_id uuid,
    item_key text NOT NULL,
    state text NOT NULL,
    result jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_submission_items_key_check CHECK ((btrim(item_key) <> ''::text)),
    CONSTRAINT sop_submission_items_result_object_check CHECK ((jsonb_typeof(result) = 'object'::text)),
    CONSTRAINT sop_submission_items_state_check CHECK ((state = ANY (ARRAY['accepted'::text, 'needs_review'::text, 'rejected'::text, 'skipped'::text])))
);

--
-- Name: sop_submissions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_submissions (
    submission_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    sop_version_id uuid NOT NULL,
    submitted_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    answers jsonb NOT NULL,
    proof_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    state text DEFAULT 'submitted'::text NOT NULL,
    validation_report jsonb DEFAULT '{}'::jsonb NOT NULL,
    submitted_at timestamp with time zone DEFAULT now() NOT NULL,
    accepted_at timestamp with time zone,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT sop_submissions_answers_object_check CHECK ((jsonb_typeof(answers) = 'object'::text)),
    CONSTRAINT sop_submissions_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_submissions_proof_array_check CHECK ((jsonb_typeof(proof_refs) = 'array'::text)),
    CONSTRAINT sop_submissions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT sop_submissions_state_check CHECK ((state = ANY (ARRAY['submitted'::text, 'accepted'::text, 'needs_review'::text, 'rejected'::text, 'voided'::text])))
);

--
-- Name: sop_task_scan_captures; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_task_scan_captures (
    capture_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    field_key text NOT NULL,
    tag text NOT NULL,
    normalized_tag text NOT NULL,
    goat_id uuid,
    obligation_id uuid,
    captured_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    captured_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_scan_captures_field_key_check CHECK ((btrim(field_key) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_normalized_tag_check CHECK ((btrim(normalized_tag) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_tag_check CHECK ((btrim(tag) <> ''::text))
);

--
-- Name: sop_task_scan_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_task_scan_attempts (
    attempt_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    field_key text NOT NULL,
    tag text NOT NULL,
    normalized_tag text NOT NULL,
    goat_id uuid,
    obligation_id uuid,
    outcome text NOT NULL,
    tag_role text DEFAULT 'unknown'::text NOT NULL,
    reason text,
    captured_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    captured_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_scan_attempts_field_key_check CHECK ((btrim(field_key) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_normalized_tag_check CHECK ((btrim(normalized_tag) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_outcome_check CHECK ((outcome = ANY (ARRAY['accepted'::text, 'duplicate'::text, 'not_due'::text, 'unknown'::text]))),
    CONSTRAINT sop_task_scan_attempts_tag_check CHECK ((btrim(tag) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_tag_role_check CHECK ((tag_role = ANY (ARRAY['primary'::text, 'secondary'::text, 'unknown'::text])))
);

--
-- Name: sop_task_review_fanouts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_task_review_fanouts (
    review_fanout_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    task_row_version integer NOT NULL,
    outcome text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    requested_by uuid,
    reason text DEFAULT ''::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_review_fanouts_attempt_check CHECK ((attempt_count >= 0)),
    CONSTRAINT sop_task_review_fanouts_outcome_check CHECK ((outcome = ANY (ARRAY['accepted'::text, 'rework_requested'::text]))),
    CONSTRAINT sop_task_review_fanouts_row_version_check CHECK ((task_row_version >= 1)),
    CONSTRAINT sop_task_review_fanouts_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'completed'::text, 'failed'::text, 'superseded'::text])))
);

--
-- Name: sop_task_submission_fanouts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_task_submission_fanouts (
    submission_fanout_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    submission_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    requested_by uuid,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_submission_fanouts_attempt_check CHECK ((attempt_count >= 0)),
    CONSTRAINT sop_task_submission_fanouts_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'completed'::text, 'failed'::text, 'skipped'::text])))
);

--
-- Name: sop_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_tasks (
    task_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    sop_id uuid NOT NULL,
    sop_version_id uuid NOT NULL,
    task_type text NOT NULL,
    title text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    state text DEFAULT 'assigned'::text NOT NULL,
    assigned_to uuid,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    priority text DEFAULT 'normal'::text NOT NULL,
    due_at timestamp with time zone,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    verified_by uuid,
    verified_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT sop_tasks_context_object_check CHECK ((jsonb_typeof(context) = 'object'::text)),
    CONSTRAINT sop_tasks_priority_check CHECK ((priority = ANY (ARRAY['low'::text, 'normal'::text, 'high'::text, 'urgent'::text]))),
    CONSTRAINT sop_tasks_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT sop_tasks_scope_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text]))),
    CONSTRAINT sop_tasks_state_check CHECK ((state = ANY (ARRAY['queued'::text, 'assigned'::text, 'in_progress'::text, 'submitted'::text, 'accepted'::text, 'needs_review'::text, 'rework_requested'::text, 'rejected'::text, 'canceled'::text]))),
    CONSTRAINT sop_tasks_task_type_check CHECK ((btrim(task_type) <> ''::text)),
    CONSTRAINT sop_tasks_title_check CHECK ((btrim(title) <> ''::text))
);

--
-- Name: sop_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sop_versions (
    sop_version_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    sop_id uuid NOT NULL,
    version integer NOT NULL,
    version_label text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    form_dsl jsonb NOT NULL,
    proof_policy jsonb NOT NULL,
    compatibility jsonb DEFAULT '{}'::jsonb NOT NULL,
    validation_report jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    published_by uuid,
    published_at timestamp with time zone,
    retired_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT sop_versions_form_object_check CHECK ((jsonb_typeof(form_dsl) = 'object'::text)),
    CONSTRAINT sop_versions_label_check CHECK ((btrim(version_label) <> ''::text)),
    CONSTRAINT sop_versions_proof_object_check CHECK ((jsonb_typeof(proof_policy) = 'object'::text)),
    CONSTRAINT sop_versions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT sop_versions_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'retired'::text]))),
    CONSTRAINT sop_versions_version_check CHECK ((version > 0))
);

--
-- Name: source_entry_decisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_entry_decisions (
    decision_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    load_id uuid NOT NULL,
    decision_stage text NOT NULL,
    decision_type text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    decided_by uuid,
    decided_at timestamp with time zone NOT NULL,
    proof_ref_id uuid,
    sop_task_id uuid,
    owner_id uuid,
    resume_condition text,
    idempotency_key text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT source_entry_decisions_metadata_object_check CHECK ((jsonb_typeof(metadata) = 'object'::text)),
    CONSTRAINT source_entry_decisions_stage_check CHECK ((decision_stage = ANY (ARRAY['source_selection'::text, 'source_health'::text, 'pre_dispatch'::text, 'arrival_gate'::text, 'accepted_intake'::text, 'exit'::text]))),
    CONSTRAINT source_entry_decisions_type_check CHECK ((decision_type = ANY (ARRAY['accepted'::text, 'rejected'::text, 'deferred'::text, 'blocked'::text])))
);

--
-- Name: source_holding_stays; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.source_holding_stays (
    stay_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    load_id uuid NOT NULL,
    holding_location_id uuid NOT NULL,
    started_at timestamp with time zone NOT NULL,
    ended_at timestamp with time zone,
    warmup_state text DEFAULT 'in_progress'::text NOT NULL,
    warmup_days integer,
    health_state text DEFAULT 'pending'::text NOT NULL,
    ownership_state text DEFAULT 'pending'::text NOT NULL,
    status text DEFAULT 'open'::text NOT NULL,
    proof_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    purpose text DEFAULT 'unspecified'::text NOT NULL,
    CONSTRAINT source_holding_stays_health_state_check CHECK ((health_state = ANY (ARRAY['pending'::text, 'passed'::text, 'failed'::text, 'deferred'::text]))),
    CONSTRAINT source_holding_stays_ownership_state_check CHECK ((ownership_state = ANY (ARRAY['pending'::text, 'shared_pending'::text, 'mesha_owned'::text, 'blocked'::text, 'not_owned'::text, 'settled'::text]))),
    CONSTRAINT source_holding_stays_proof_refs_array_check CHECK ((jsonb_typeof(proof_refs) = 'array'::text)),
    CONSTRAINT source_holding_stays_purpose_check CHECK ((purpose = ANY (ARRAY['breeding'::text, 'fattening'::text, 'non_breeding'::text, 'unspecified'::text]))),
    CONSTRAINT source_holding_stays_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT source_holding_stays_status_check CHECK ((status = ANY (ARRAY['open'::text, 'closed'::text, 'canceled'::text]))),
    CONSTRAINT source_holding_stays_warmup_days_check CHECK (((warmup_days IS NULL) OR (warmup_days >= 0))),
    CONSTRAINT source_holding_stays_warmup_state_check CHECK ((warmup_state = ANY (ARRAY['not_started'::text, 'in_progress'::text, 'completed'::text, 'outside_normal_window'::text]))),
    CONSTRAINT source_holding_stays_window_check CHECK (((ended_at IS NULL) OR (ended_at >= started_at)))
);

--
-- Name: status_definitions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.status_definitions (
    status_code text NOT NULL,
    axis text NOT NULL,
    display_name text NOT NULL,
    short_label text NOT NULL,
    description text,
    sort_order integer DEFAULT 0 NOT NULL,
    active boolean DEFAULT true NOT NULL,
    expected_duration_days integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT status_definitions_axis_check CHECK ((axis = ANY (ARRAY['lifecycle'::text, 'reproductive'::text, 'growth_cohort'::text, 'management'::text, 'health'::text]))),
    CONSTRAINT status_definitions_expected_duration_check CHECK (((expected_duration_days IS NULL) OR (expected_duration_days > 0)))
);

--
-- Name: tenants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenants (
    tenant_id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT tenants_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))
);

--
-- Name: transit_handoffs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.transit_handoffs (
    handoff_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    load_id uuid NOT NULL,
    from_location_id uuid,
    to_location_id uuid NOT NULL,
    loaded_count integer DEFAULT 0 NOT NULL,
    dispatched_at timestamp with time zone NOT NULL,
    arrived_at timestamp with time zone,
    proof_ref_id uuid,
    discrepancy_state text DEFAULT 'none'::text NOT NULL,
    status text DEFAULT 'in_transit'::text NOT NULL,
    idempotency_key text NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT transit_handoffs_discrepancy_check CHECK ((discrepancy_state = ANY (ARRAY['none'::text, 'partial_load'::text, 'accepted_not_loaded'::text, 'missing'::text, 'extra'::text, 'mismatch'::text, 'blocked'::text]))),
    CONSTRAINT transit_handoffs_loaded_count_check CHECK ((loaded_count >= 0)),
    CONSTRAINT transit_handoffs_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT transit_handoffs_status_check CHECK ((status = ANY (ARRAY['planned'::text, 'in_transit'::text, 'arrived'::text, 'canceled'::text])))
);

--
-- Name: user_scope_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_scope_grants (
    grant_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    status text NOT NULL,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT user_scope_grants_scope_type_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text]))),
    CONSTRAINT user_scope_grants_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'revoked'::text]))),
    CONSTRAINT user_scope_grants_valid_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: vaccination_capacity_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccination_capacity_config (
    tenant_id uuid NOT NULL,
    max_per_day integer NOT NULL,
    capacity_scope text NOT NULL,
    max_buffer_days integer NOT NULL,
    overflow_policy text NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vaccination_capacity_config_buffer_check CHECK ((max_buffer_days >= 0)),
    CONSTRAINT vaccination_capacity_config_max_per_day_check CHECK (((max_per_day >= 1) AND (max_per_day <= 200))),
    CONSTRAINT vaccination_capacity_config_overflow_check CHECK ((overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap'::text)),
    CONSTRAINT vaccination_capacity_config_scope_check CHECK ((capacity_scope = ANY (ARRAY['tenant'::text, 'center'::text, 'shed'::text])))
);

--
-- Name: vaccination_completions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccination_completions (
    completion_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    obligation_id uuid NOT NULL,
    batch_id uuid,
    goat_id uuid NOT NULL,
    sop_submission_item_id uuid,
    vaccine_inventory_lot_id uuid,
    doses integer,
    dose_ml_given numeric,
    route_site text,
    adverse_reaction boolean DEFAULT false NOT NULL,
    adverse_reaction_problem_id uuid,
    cold_chain_verified boolean DEFAULT false NOT NULL,
    administered_at timestamp with time zone NOT NULL,
    status text DEFAULT 'recorded'::text NOT NULL,
    verified_by uuid,
    verified_at timestamp with time zone,
    rejection_reason text,
    withdrawal_until_date date,
    recorded_by uuid,
    idempotency_key text NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vaccination_completions_dose_ml_check CHECK (((dose_ml_given IS NULL) OR (dose_ml_given > (0)::numeric))),
    CONSTRAINT vaccination_completions_doses_check CHECK (((doses IS NULL) OR (doses > 0))),
    CONSTRAINT vaccination_completions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT vaccination_completions_status_check CHECK ((status = ANY (ARRAY['recorded'::text, 'accepted'::text, 'rejected'::text, 'reversed'::text])))
);

--
-- Name: vaccination_eligibility_rollups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccination_eligibility_rollups (
    rollup_id bigint NOT NULL,
    tenant_id uuid NOT NULL,
    park_id uuid,
    shed_id uuid,
    species text DEFAULT ''::text NOT NULL,
    management_stage text DEFAULT ''::text NOT NULL,
    sex text DEFAULT ''::text NOT NULL,
    breed text DEFAULT ''::text NOT NULL,
    health_status text DEFAULT ''::text NOT NULL,
    usable_for_vaccination boolean NOT NULL,
    animal_count bigint NOT NULL,
    source_revision bigint DEFAULT 0 NOT NULL,
    recomputed_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vaccination_eligibility_rollups_count_check CHECK ((animal_count >= 0))
);

--
-- Name: vaccination_eligibility_rollups_rollup_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.vaccination_eligibility_rollups ALTER COLUMN rollup_id ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.vaccination_eligibility_rollups_rollup_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

--
-- Name: vaccination_generation_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccination_generation_runs (
    run_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    trigger_type text NOT NULL,
    trigger_ref text,
    status text DEFAULT 'running'::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    generated_count integer DEFAULT 0 NOT NULL,
    deferred_count integer DEFAULT 0 NOT NULL,
    skipped_no_due_date_count integer DEFAULT 0 NOT NULL,
    suppressed_trusted_history_count integer DEFAULT 0 NOT NULL,
    cursor_goat_id uuid,
    last_error text,
    idempotency_key text NOT NULL,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    reopened_count integer DEFAULT 0 NOT NULL,
    failed_goat_count integer DEFAULT 0 NOT NULL,
    CONSTRAINT vaccination_generation_runs_counts_check CHECK (((generated_count >= 0) AND (deferred_count >= 0) AND (reopened_count >= 0) AND (skipped_no_due_date_count >= 0) AND (suppressed_trusted_history_count >= 0))),
    CONSTRAINT vaccination_generation_runs_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT vaccination_generation_runs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'completed'::text, 'failed'::text]))),
    CONSTRAINT vaccination_generation_runs_trigger_check CHECK ((trigger_type = ANY (ARRAY['publish'::text, 'cli'::text, 'goat_created'::text, 'stage_changed'::text, 'manual_campaign'::text, 'retry'::text])))
);

--
-- Name: vaccination_reminder_cadence_fires; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccination_reminder_cadence_fires (
    tenant_id uuid NOT NULL,
    fire_key text NOT NULL,
    park_id uuid NOT NULL,
    fire_day date NOT NULL,
    notification_type text NOT NULL,
    slot text NOT NULL,
    reminder_number integer DEFAULT 0 NOT NULL,
    obligation_count integer DEFAULT 0 NOT NULL,
    representative_calendar_event_id text NOT NULL,
    queued_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vaccination_reminder_cadence_fires_type_check CHECK ((notification_type = ANY (ARRAY['advance_notice'::text, 'reminder'::text, 'due_today'::text])))
);

--
-- Name: vaccination_source_facts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccination_source_facts (
    source_fact_id uuid NOT NULL,
    tenant_id uuid NOT NULL,
    seed_run_id uuid,
    lineage_key text NOT NULL,
    animal_key text NOT NULL,
    vaccine_header text NOT NULL,
    dose_code text NOT NULL,
    sequence integer NOT NULL,
    source_value text NOT NULL,
    source_date date,
    disposition text NOT NULL,
    obligation_idem text,
    completion_idem text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vaccination_source_facts_disposition_check CHECK ((disposition = ANY (ARRAY['imported_completion'::text, 'scheduled_obligation'::text, 'later_administration_merge'::text, 'excluded_goat_not_placed'::text, 'excluded_vaccine_unrecognized'::text, 'excluded_lifecycle'::text, 'unresolved'::text])))
);

--
-- Name: vaccines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vaccines (
    vaccine_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    item_id uuid NOT NULL,
    disease text,
    manufacturer text,
    doses_per_vial integer,
    withdrawal_days integer,
    context jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT vaccines_doses_check CHECK (((doses_per_vial IS NULL) OR (doses_per_vial > 0))),
    CONSTRAINT vaccines_withdrawal_check CHECK (((withdrawal_days IS NULL) OR (withdrawal_days >= 0)))
);

--
-- Name: verification_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.verification_items (
    item_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    vertical text NOT NULL,
    module text NOT NULL,
    category text NOT NULL,
    source_module text NOT NULL,
    source_task_id uuid,
    source_submission_id uuid,
    source_ref_type text NOT NULL,
    source_ref_id uuid NOT NULL,
    media_refs jsonb DEFAULT '[]'::jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    verdict_reason text,
    operator_id uuid,
    shed_id uuid,
    park_id uuid,
    captured_at timestamp with time zone NOT NULL,
    verified_by uuid,
    verified_at timestamp with time zone,
    subject_label text,
    closed_by uuid,
    closed_at timestamp with time zone,
    idempotency_key text NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT verification_items_idempotency_key_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT verification_items_media_refs_array_check CHECK ((jsonb_typeof(media_refs) = 'array'::text)),
    CONSTRAINT verification_items_subject_label_check CHECK (((subject_label IS NULL) OR (btrim(subject_label) <> ''::text))),
    CONSTRAINT verification_items_closed_pair_check CHECK (((closed_by IS NULL) = (closed_at IS NULL))),
    CONSTRAINT verification_items_closed_approved_check CHECK (((closed_at IS NULL) OR (status = 'approved'::text))),
    CONSTRAINT verification_items_reject_reason_check CHECK (((status <> 'rejected'::text) OR ((verdict_reason IS NOT NULL) AND (btrim(verdict_reason) <> ''::text)))),
    CONSTRAINT verification_items_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT verification_items_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text])))
);

--
-- Name: vw_goat_tagging; Type: VIEW; Schema: public; Owner: -
--

CREATE VIEW public.vw_goat_tagging AS
 SELECT tenant_id,
    goat_id,
    identifier_id,
    identifier_type,
    identifier_value,
    normalized_value,
    scope_key,
    is_primary_for_goat,
    status,
    valid_from,
    valid_to
   FROM public.goat_identifiers gi
  WHERE (status = 'active'::text);

--
-- Name: vw_procurement_vaccination_excluded_goats; Type: VIEW; Schema: public; Owner: -
--

CREATE VIEW public.vw_procurement_vaccination_excluded_goats AS
 SELECT DISTINCT g.tenant_id,
    g.goat_id,
        CASE
            WHEN (g.lifecycle_status = ANY (ARRAY['dead'::text, 'sold'::text, 'lost'::text, 'culled'::text, 'transferred'::text, 'merged'::text, 'inactive'::text])) THEN g.lifecycle_status
            WHEN (g.merged_into_goat_id IS NOT NULL) THEN 'merged'::text
            WHEN (plg.source_entry_state <> 'accepted'::text) THEN ('source_entry_'::text || plg.source_entry_state)
            WHEN (plg.ownership_state <> ALL (ARRAY['mesha_owned'::text, 'settled'::text])) THEN ('ownership_'::text || plg.ownership_state)
            WHEN (plg.health_state <> 'passed'::text) THEN ('health_'::text || plg.health_state)
            WHEN (plg.current_state <> 'accepted_herd_intake'::text) THEN plg.current_state
            ELSE 'not_excluded'::text
        END AS exclusion_reason
   FROM (public.goats g
     LEFT JOIN public.procurement_load_goats plg ON (((plg.tenant_id = g.tenant_id) AND (plg.goat_id = g.goat_id))))
  WHERE ((g.lifecycle_status = ANY (ARRAY['dead'::text, 'sold'::text, 'lost'::text, 'culled'::text, 'transferred'::text, 'merged'::text, 'inactive'::text])) OR (g.merged_into_goat_id IS NOT NULL) OR ((plg.goat_id IS NOT NULL) AND ((plg.current_state <> 'accepted_herd_intake'::text) OR (plg.selection_state = ANY (ARRAY['source_only'::text, 'candidate'::text, 'rejected'::text, 'deferred'::text, 'blocked'::text, 'arrival_rejected'::text, 'dead'::text, 'sold'::text, 'lost'::text])) OR (plg.source_entry_state <> 'accepted'::text) OR (plg.ownership_state <> ALL (ARRAY['mesha_owned'::text, 'settled'::text])) OR (plg.health_state <> 'passed'::text))));

--
-- Name: workforce_absences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_absences (
    absence_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    reason_code text NOT NULL,
    status text DEFAULT 'reported'::text NOT NULL,
    replacement_member_id uuid,
    created_by uuid,
    approved_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    coverage_override_reason text,
    CONSTRAINT workforce_absences_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT workforce_absences_scope_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text, 'center'::text]))),
    CONSTRAINT workforce_absences_status_check CHECK ((status = ANY (ARRAY['reported'::text, 'approved'::text, 'escalation_required'::text, 'rejected'::text, 'canceled'::text]))),
    CONSTRAINT workforce_absences_window_check CHECK ((ends_at > starts_at))
);

--
-- Name: workforce_capabilities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_capabilities (
    capability_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    capability_code text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workforce_capabilities_code_check CHECK ((capability_code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'::text)),
    CONSTRAINT workforce_capabilities_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'retired'::text])))
);

--
-- Name: workforce_external_identities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_external_identities (
    external_identity_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid,
    source_system text NOT NULL,
    source_flow text NOT NULL,
    external_ref_type text NOT NULL,
    external_ref_hash text NOT NULL,
    encrypted_external_ref bytea,
    status text DEFAULT 'candidate'::text NOT NULL,
    confidence numeric DEFAULT 0 NOT NULL,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    observation_count bigint DEFAULT 1 NOT NULL,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    review_reason text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT workforce_external_identities_confidence_check CHECK (((confidence >= (0)::numeric) AND (confidence <= (1)::numeric))),
    CONSTRAINT workforce_external_identities_observation_check CHECK ((observation_count > 0)),
    CONSTRAINT workforce_external_identities_ref_type_check CHECK ((external_ref_type = ANY (ARRAY['slack_user_id'::text, 'email'::text, 'phone'::text, 'staff_label'::text, 'firebase_uid'::text]))),
    CONSTRAINT workforce_external_identities_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT workforce_external_identities_seen_window_check CHECK ((last_seen_at >= first_seen_at)),
    CONSTRAINT workforce_external_identities_source_system_check CHECK ((source_system = ANY (ARRAY['slack'::text, 'firebase'::text, 'manual'::text]))),
    CONSTRAINT workforce_external_identities_status_check CHECK ((status = ANY (ARRAY['candidate'::text, 'mapped'::text, 'rejected'::text, 'conflict'::text, 'retired'::text])))
);

--
-- Name: workforce_member_app_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_member_app_sessions (
    session_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    device_id uuid,
    auth_subject uuid NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    ended_at timestamp with time zone,
    status text DEFAULT 'active'::text NOT NULL,
    app_version text DEFAULT ''::text NOT NULL,
    ip_hash text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT workforce_member_app_sessions_seen_check CHECK ((last_seen_at >= started_at)),
    CONSTRAINT workforce_member_app_sessions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'ended'::text, 'denied'::text])))
);

--
-- Name: workforce_member_capabilities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_member_capabilities (
    member_capability_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    capability_id uuid NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    valid_from timestamp with time zone DEFAULT now() NOT NULL,
    valid_to timestamp with time zone,
    assigned_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT workforce_member_capabilities_scope_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text, 'center'::text]))),
    CONSTRAINT workforce_member_capabilities_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'revoked'::text]))),
    CONSTRAINT workforce_member_capabilities_valid_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: workforce_member_devices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_member_devices (
    device_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    platform text DEFAULT 'android'::text NOT NULL,
    app_install_id text NOT NULL,
    device_public_key_hash text,
    push_token_hash text,
    app_version text NOT NULL,
    os_version text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    registered_by uuid,
    registered_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_by uuid,
    revoked_at timestamp with time zone,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    fcm_token text,
    CONSTRAINT workforce_member_devices_app_install_check CHECK ((btrim(app_install_id) <> ''::text)),
    CONSTRAINT workforce_member_devices_app_version_check CHECK ((btrim(app_version) <> ''::text)),
    CONSTRAINT workforce_member_devices_platform_check CHECK ((platform = 'android'::text)),
    CONSTRAINT workforce_member_devices_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT workforce_member_devices_status_check CHECK ((status = ANY (ARRAY['active'::text, 'revoked'::text, 'lost'::text, 'retired'::text])))
);

--
-- Name: workforce_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_members (
    workforce_member_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    user_id uuid,
    display_code text NOT NULL,
    display_name text NOT NULL,
    status text DEFAULT 'candidate'::text NOT NULL,
    primary_role_hint text DEFAULT 'operator'::text NOT NULL,
    primary_location_id uuid,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    department_id uuid,
    hr_designation_grade text,
    CONSTRAINT workforce_members_display_code_check CHECK ((btrim(display_code) <> ''::text)),
    CONSTRAINT workforce_members_display_name_check CHECK ((btrim(display_name) <> ''::text)),
    CONSTRAINT workforce_members_hr_designation_grade_check CHECK (((hr_designation_grade IS NULL) OR (hr_designation_grade = ANY (ARRAY['cxo'::text, 'director'::text, 'manager'::text, 'assistant_manager'::text])))),
    CONSTRAINT workforce_members_role_hint_check CHECK ((primary_role_hint = ANY (ARRAY['operator'::text, 'park_head'::text, 'pc_director'::text, 'verifier'::text, 'supervisor'::text, 'admin'::text, 'other'::text]))),
    CONSTRAINT workforce_members_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT workforce_members_status_check CHECK ((status = ANY (ARRAY['candidate'::text, 'active'::text, 'inactive'::text, 'suspended'::text, 'left'::text])))
);

--
-- Name: workforce_positions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_positions (
    position_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    position_code text NOT NULL,
    position_tier text NOT NULL,
    is_backup_slot boolean DEFAULT false NOT NULL,
    backup_group_code text,
    week_off_weekday text,
    status text DEFAULT 'active'::text NOT NULL,
    valid_from timestamp with time zone DEFAULT now() NOT NULL,
    valid_to timestamp with time zone,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT workforce_positions_backup_group_check CHECK (((backup_group_code IS NULL) OR (backup_group_code ~ '^[a-z][a-z0-9_]*$'::text))),
    CONSTRAINT workforce_positions_position_code_check CHECK ((position_code ~ '^[a-z][a-z0-9_]*$'::text)),
    CONSTRAINT workforce_positions_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT workforce_positions_scope_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'center'::text, 'shed'::text]))),
    CONSTRAINT workforce_positions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'ended'::text]))),
    CONSTRAINT workforce_positions_tier_check CHECK ((position_tier = ANY (ARRAY['assistant'::text, 'manager'::text, 'head'::text, 'director'::text, 'cxo'::text]))),
    CONSTRAINT workforce_positions_week_off_check CHECK (((week_off_weekday IS NULL) OR (week_off_weekday = ANY (ARRAY['monday'::text, 'tuesday'::text, 'wednesday'::text, 'thursday'::text, 'friday'::text, 'saturday'::text, 'sunday'::text])))),
    CONSTRAINT workforce_positions_window_check CHECK (((valid_to IS NULL) OR (valid_to > valid_from)))
);

--
-- Name: workforce_roster_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workforce_roster_assignments (
    roster_assignment_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    workforce_member_id uuid NOT NULL,
    team_id uuid,
    scope_type text NOT NULL,
    scope_id uuid NOT NULL,
    shift_date date NOT NULL,
    shift_start_at timestamp with time zone NOT NULL,
    shift_end_at timestamp with time zone NOT NULL,
    task_type text,
    status text DEFAULT 'scheduled'::text NOT NULL,
    escalation_owner_user_id uuid,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    row_version integer DEFAULT 1 NOT NULL,
    CONSTRAINT workforce_roster_assignments_row_version_check CHECK ((row_version >= 1)),
    CONSTRAINT workforce_roster_assignments_scope_check CHECK ((scope_type = ANY (ARRAY['tenant'::text, 'custodian_party'::text, 'farm'::text, 'park'::text, 'shed'::text, 'cohort'::text]))),
    CONSTRAINT workforce_roster_assignments_shift_window_check CHECK ((shift_end_at > shift_start_at)),
    CONSTRAINT workforce_roster_assignments_status_check CHECK ((status = ANY (ARRAY['scheduled'::text, 'active'::text, 'completed'::text, 'missed'::text, 'canceled'::text])))
);

--
-- PostgreSQL database dump complete
--

-- baseline: static seed/config data
--
-- PostgreSQL database dump
--

-- Dumped from database version 16.9
-- Dumped by pg_dump version 16.9

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Data for Name: crash_daily; Type: TABLE DATA; Schema: analytics; Owner: -
--

--
-- Data for Name: engagement_daily; Type: TABLE DATA; Schema: analytics; Owner: -
--

--
-- Data for Name: funnel_daily; Type: TABLE DATA; Schema: analytics; Owner: -
--

--
-- Data for Name: journey_daily; Type: TABLE DATA; Schema: analytics; Owner: -
--

--
-- Data for Name: rollup_run; Type: TABLE DATA; Schema: analytics; Owner: -
--

--
-- Data for Name: admin_ui_config_entries; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: admin_ui_config_family_change_queue; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: admin_ui_config_family_revisions; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.admin_ui_config_family_revisions VALUES
	('00000000-0000-4000-8000-000000000001', 'status:health', 1, 'f3ab1f8def8f245bdfff43c381e46985', '2026-07-16 07:30:26.070397+00', NULL, 'status_definitions.insert', '{"table": "status_definitions", "operation": "INSERT", "coalesced_change_count": 2}'),
	('00000000-0000-4000-8000-000000000001', 'sops:vaccination', 1, '06d4ff51e14dce2b5cec21fe6b2b2d69', '2026-07-16 07:30:26.753457+00', NULL, 'sop_versions.update', '{"table": "sop_versions", "operation": "UPDATE", "coalesced_change_count": 1}'),
	('00000000-0000-4000-8000-000000000001', 'config', 2, 'd4debeb5fad3547760da88c9fd7b8783', '2026-07-16 07:30:26.753457+00', NULL, 'sop_versions.update', '{"table": "sop_versions", "operation": "UPDATE", "coalesced_change_count": 1}');

--
-- Data for Name: animal_stage_lookup; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: arrival_intake_review_goats; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: arrival_intake_reviews; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: audit_log; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: auth_pending_email_grants; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: breed_aliases; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.breed_aliases VALUES
	('a23d90ee-1d88-476a-9b41-efef72183c12', '00000000-0000-4000-8000-000000002001', 'Malai', 'malai', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('537177bd-d1f5-416a-8f84-1e1d1953038a', '00000000-0000-4000-8000-000000002002', 'Beetal', 'beetal', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('dff95a17-c659-4921-a646-dbdb799888ce', '00000000-0000-4000-8000-000000002003', 'Sojat', 'sojat', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('534e0684-872e-4457-852f-027a3e1c87fb', '00000000-0000-4000-8000-000000002004', 'Osmanabadi', 'osmanabadi', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('31ac471f-e8b1-49d6-99f4-37d4a7f88906', '00000000-0000-4000-8000-000000002005', 'Boer', 'boer', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('2c54080a-ceb8-486f-857f-2a7fab63101d', '00000000-0000-4000-8000-000000002006', 'Anantapur Sheep', 'anantapur_sheep', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('dc8193d4-0822-48dc-be4a-7e924f88c166', '00000000-0000-4000-8000-000000002007', 'Anantapur', 'anantapur', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('aac53cd1-172c-4a78-8e41-c0d6c0cfcec3', '00000000-0000-4000-8000-000000002008', 'Kenguri', 'kenguri', 'phase1_seed', '2026-07-16 07:30:24.352544+00'),
	('efe957e3-a6c1-404e-9bcf-d588fa167e22', 'e37ae4f2-a77f-4f62-bd85-c84782d0b0ad', 'Sirohi', 'sirohi', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('08772748-9f51-48ca-a881-0f2f0e284e1e', '2e58134d-e0ef-46d8-bb58-13474f80e1dd', 'Malai x Beetal', 'malai_x_beetal', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('183e3145-953d-438c-a32a-dc5d31cdce81', '2e58134d-e0ef-46d8-bb58-13474f80e1dd', 'Beetal x Malai', 'beetal_x_malai', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('8f0e5b6c-4757-4b9e-8657-55263e1dff7c', 'ddb01e41-4d8f-42e3-9fdb-b41847377502', 'Beetal x Sojat', 'beetal_x_sojat', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('e33f5743-d9bd-45af-8a98-c1b157483860', 'e5ad731c-3cfb-4f16-90a1-7933de784497', 'Boer x Beetal', 'boer_x_beetal', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('59f5962f-0a04-4cda-946e-f9e298876aaf', '7d925284-bab6-4f9c-8f25-88e78d0da420', 'Boer x Malai', 'boer_x_malai', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('a9d4bbde-887f-4965-a37f-4436e667cf13', '51064192-cd08-4fbb-948f-14c0ca7448de', 'Boer x Sirohi', 'boer_x_sirohi', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('8e421f23-1656-4055-b9e4-4eb6860c1c85', 'e2f14ece-a34b-4e67-a6cf-e2f3cfe02f66', 'Boer x Sojat', 'boer_x_sojat', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('eff0fe14-90b4-422e-9d42-454c74089714', 'a3327ddf-e02a-4e2b-a2a3-fa4521421dec', 'Sojat x Malai', 'sojat_x_malai', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('ac1f9030-92cd-4c9a-b701-fbbc82ee6687', 'a3327ddf-e02a-4e2b-a2a3-fa4521421dec', 'Malai x Sojat', 'malai_x_sojat', 'legacy_rfid_db', '2026-07-16 07:30:24.894536+00'),
	('c0e67f0e-c221-481a-a883-d513ffd195a0', '00000000-0000-4000-8000-000000002006', 'Anantapur Sheep', 'anantapur_sheep', 'legacy_rfid_db', '2026-07-16 07:30:24.928486+00');

--
-- Data for Name: breeds; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.breeds VALUES
	('00000000-0000-4000-8000-000000002001', 'goat', 'Malai', 'active', NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000002002', 'goat', 'Beetal', 'active', NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000002003', 'goat', 'Sojat', 'active', NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000002004', 'goat', 'Osmanabadi', 'active', NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000002005', 'goat', 'Boer', 'active', NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000002007', 'goat', 'Anantapur', 'review', 'Legacy dashboard constant; keep reviewable alias/reference row.', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000002008', 'goat', 'Kenguri', 'review', 'Legacy dashboard constant; keep reviewable alias/reference row.', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('e37ae4f2-a77f-4f62-bd85-c84782d0b0ad', 'goat', 'Sirohi', 'active', 'Approved Phase 1 RFID source goat breed.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('2e58134d-e0ef-46d8-bb58-13474f80e1dd', 'goat', 'Beetal x Malai', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('ddb01e41-4d8f-42e3-9fdb-b41847377502', 'goat', 'Beetal x Sojat', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('e5ad731c-3cfb-4f16-90a1-7933de784497', 'goat', 'Boer x Beetal', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('7d925284-bab6-4f9c-8f25-88e78d0da420', 'goat', 'Boer x Malai', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('51064192-cd08-4fbb-948f-14c0ca7448de', 'goat', 'Boer x Sirohi', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('e2f14ece-a34b-4e67-a6cf-e2f3cfe02f66', 'goat', 'Boer x Sojat', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('a3327ddf-e02a-4e2b-a2a3-fa4521421dec', 'goat', 'Malai x Sojat', 'active', 'Approved Phase 1 RFID crossbreed simplification; parentage/compound breed modeling deferred.', '2026-07-16 07:30:24.894536+00', '2026-07-16 07:30:24.894536+00'),
	('00000000-0000-4000-8000-000000002006', 'goat', 'Anantapur Sheep', 'active', 'Business-confirmed Mesha goat breed/category from source Breed column; do not block passport creation from this label.', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.928486+00');

--
-- Data for Name: bulk_status_job; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: bulk_status_job_row; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: calendar_reconciler_progress; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: calendar_snoozes; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_base_anchors; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_dimension_aliases; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_mismatch_scan_runs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_projection_exception_resolutions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_projection_exceptions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_projection_recompute_runs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_projection_snapshot_rows; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_projection_snapshots; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: count_source_import_runs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: counts_shifting_readiness_evidence; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: counts_shifting_readiness_subgates; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: departments; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.departments VALUES
	('5dd59531-1a71-4f6c-8a1d-34312d664ad4', '00000000-0000-4000-8000-000000000001', 'procurement', 'Procurement', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('beae3da5-9c77-40e5-897c-2c7a302ef45c', '00000000-0000-4000-8000-000000000001', 'preventive_care', 'Preventive Care', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('7a312853-8296-4815-af04-6253b4b7d218', '00000000-0000-4000-8000-000000000001', 'breeding', 'Breeding', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('803e37ef-1d2c-48cb-9daf-72fc1398a955', '00000000-0000-4000-8000-000000000001', 'health', 'Health', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('5caef732-708b-4e06-b06c-0226e4db7133', '00000000-0000-4000-8000-000000000001', 'growth', 'Growth', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('592ff648-488a-4252-9587-6efd1997d384', '00000000-0000-4000-8000-000000000001', 'infrastructure', 'Infrastructure', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('9107ec7c-143b-46b2-867a-c57459e63fc7', '00000000-0000-4000-8000-000000000001', 'feed', 'Feed', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('f79101ee-48f5-43c0-a767-d33649f8ee05', '00000000-0000-4000-8000-000000000001', 'milk', 'Milk', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00'),
	('17a2f718-b44a-4b9f-bc15-78f950aa8f5c', '00000000-0000-4000-8000-000000000001', 'sales', 'Sales', 'active', '2026-07-16 07:30:26.561155+00', '2026-07-16 07:30:26.561155+00');

--
-- Data for Name: domain_event_processed_events; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: farm_profiles; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: feed_direction_completions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goat_custody_history; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goat_identifiers; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goat_identity_events; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goat_location_history; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goat_merge_links; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goat_ownership; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: goats; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: herd_register_goat_projection; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: herd_register_summary_projection; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: idempotency_keys; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identifier_policies; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.identifier_policies VALUES
	('phase1-identifier-v1', 'animal_identifier_1', 'global', true, 'global', false, true, 'reject', 'reject', 'identifier_normalizer_v1', NULL, 'reject', '2026-07-16 07:30:26.349359+00', NULL),
	('phase1-identifier-v1', 'animal_identifier_2', 'global', true, 'global', false, false, 'reject', 'reject', 'identifier_normalizer_v1', NULL, 'reject', '2026-07-16 07:30:26.349359+00', NULL);

--
-- Data for Name: identifier_policy_versions; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.identifier_policy_versions VALUES
	('phase1-identifier-v1', 'approved', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00', NULL);

--
-- Data for Name: identity_conflict_goats; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_conflict_source_records; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_conflicts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_correction_requests; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_decision_events; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_decision_goats; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_decision_identifiers; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_decision_media; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: identity_decisions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: inventory_items; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: inventory_stock; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: inventory_stock_movements; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: location_aliases; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: location_capacity_records; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: location_operational_attributes; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.location_operational_attributes VALUES
	('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000003000', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000003001', true, true, true, true, false, false, false, 0, 'Seeded Phase 1 scope anchor; migration-plan protected.', '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000003002', true, true, true, true, false, false, false, 0, 'Seeded Phase 1 scope anchor; migration-plan protected.', '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000003003', true, true, true, true, true, false, false, 0, 'Seeded Phase 1 scope anchor; migration-plan protected.', '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '6e769414-4dde-43a5-b30f-fb857392b638', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'c93c636f-08f5-4ee9-aaca-bf7f1efd4788', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '99a847aa-6ab2-41e3-ba31-8a16714fa95a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a9da45a2-c0f5-4157-b12f-7b2aaf0412b7', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '83ced0ed-a9e5-415d-b06c-ad52dabebb7f', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f7e8a700-0fde-48e6-bee0-fadce1f6816d', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9da6b465-489d-43d5-bda9-2e85d168459d', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'c3bcc632-baa3-40dd-b51d-6863d448e436', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '6306d03d-d1df-4bc9-9b1d-06bc9bdc6cfc', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f4c3ff39-92e8-4663-997c-8e02eeefea93', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '3d071720-b0b8-46b3-8d39-d716452c6a95', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '75731dd3-d669-4c12-8f6f-13caa501a5c1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9487caa4-fe05-4a42-ad01-0a6d0565f708', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '7250e591-64c8-478d-b020-0cc4a0ba1d91', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b2a5fd25-4d81-408d-81e0-e07d680cc84a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '268ab2ee-352c-4918-a26d-0ac745848d50', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9dc0befd-9a0b-45c6-8d85-1631315bc4a6', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'ad78d307-1b58-4a35-a765-7dda87462a31', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b2850cfd-f76c-409d-a54e-39211a1d253e', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '7a7e29e0-95f1-4d58-bc31-a908461ec4ff', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'dc208497-043e-4382-afd2-21d066023c37', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'bbeb5191-475f-4dc1-a217-a1ad71f55bcc', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9ab6c229-b1e0-4ba5-96f9-bad7d5a792df', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'c5864d6b-d88a-4aec-8586-d4d52eddeff9', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '0a7d46b6-9167-4e63-9bc3-44130b7d4139', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '8f92244c-ffde-4ea9-a0bd-f21d201c77f8', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'fd566afa-c9b1-45e6-bf03-01aecc2a4808', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '459b94bd-54b0-49a0-999e-7d5aa30f1654', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '7e7b6df1-9a5b-4be7-b341-f861d5e3d158', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e5d83dcf-28c8-4d32-8f5c-6c406a486bf1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '29808fb0-e73d-4c36-b421-2e36611a6829', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a6f38bb5-2382-4695-811b-10e5d511e0ca', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9ceae893-5c23-466b-8b82-637af935bb6d', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9005e6bd-d819-43ae-8f6d-f9a1785cebae', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b3601c9f-d38c-4daa-94b4-6767418bd897', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '3af7a10a-c87c-4df6-9b70-65616c9e0016', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '9714009c-f4fa-478e-9835-e3d79ae138ca', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '14dcec8a-15bb-48c1-a1e8-c0e3b259c7a2', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '59f9d555-c472-4a10-9a3b-4c197be66aa4', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '7edf1f74-85ad-4c99-ae6a-897b9b6a3316', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2159c522-35f4-4490-a18f-62b8b45fb845', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '4843feae-1daa-40bc-8137-2c34b86167c1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'af8e6f19-a13b-4cd9-9d90-b02e0b756dad', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '7f02354a-337b-47b0-b9ef-0ff62623d0bd', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a9ecf757-1dac-4904-ba59-d66e55cf3dde', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '1d4c5159-ad13-4b70-a2cc-650853143c9c', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e259c816-a642-4d75-9886-4f13fbe5d5a0', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'd375cb79-dbcc-4255-beff-a4a64de689cc', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a4ec250f-083b-4fdb-b559-feef726b0f9e', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '47b4e8af-f70a-451e-9159-fed304ebe86d', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '69d53b25-183b-4dfc-8f4a-df0272aff2f8', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '43042c7b-d850-400f-800f-eafd171c2122', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '86e47f9c-fd1d-461d-9b9a-45d3be9bf12d', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b6031aa6-41b5-4d00-ba05-b5c95254227f', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2ebb5cc0-9dcd-4d7f-a4bc-29cedd471e49', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '654260da-956e-4015-bc95-edf3421cae3c', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'c79de5a7-a954-488c-bc05-16b261ec3073', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '5d72bd14-4907-46c9-94ff-7405dc4a7e9a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'c95797f6-577f-4ca7-8df4-7f8a5c506310', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f1b1bad0-47ab-4248-95dc-8fa1472d4fec', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2654ebaf-d11a-4a89-8fd0-7dd848aaec8a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '38ddac27-307c-44eb-a56c-91a6935c3881', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'cbd67ce0-ef0f-44eb-bf80-18fa8a36c44a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b1fa89fd-76b0-4325-a0ba-97ce1ce5bd8f', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'ff2dd02d-b7a3-477b-b616-3bcd636782af', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '99cc07b9-94ae-4d68-aa61-bb9efd683071', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b114a8f1-3640-4d19-a6ce-487318d16f1b', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a7791a88-e707-401d-943b-353acc7ea0fc', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'bdde07a8-aae7-4032-be5a-80f193686bc8', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '634dd132-daa4-4e84-8e94-37d542c7d13a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b6cf7b79-e0a3-4db0-8ab7-f366cf1b2b57', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a7efb805-ae6c-4bdf-b162-0b51c34045c3', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '5e75c070-740e-4a17-b30c-352e051f4bb7', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2649843b-1b78-49e4-b97f-b1ad2249f637', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '0242b46a-4af3-49b6-ae95-1f35f780e814', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '323655db-413b-4709-8600-1e1ddfcbff95', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '6cff3365-2f55-4c38-8a82-bad8d2f502bd', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2b970871-1715-4901-bdc7-a494c058d798', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2fab4267-0225-48d1-a14e-c54f951a791e', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '4ca20708-e804-4807-a312-fe13cb9b5b08', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '1979930e-63a3-4ef6-9afd-fa04a442de4b', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'da1f37fc-a939-4357-a980-6e96914dfee4', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '84bc5d8b-96cd-4de2-a875-b2e19b20ac93', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '0e9518c7-de33-474e-ba90-877cc942d1d7', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '699f819d-7b04-4bd4-ad83-63be42dfc7aa', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2c8596da-0b7b-4f10-9d9f-32410d2a080c', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '21fd7aff-e93f-45fe-8346-d7deea5d6c88', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '6dfaffaf-6186-46ef-adf8-636c3017b324', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '72c34504-2488-45ed-9bb1-8c97168f72c4', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '7e8dee2b-f7f9-4f04-9da0-d550569b9a7f', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '825e5b4e-f7ab-48a4-aae1-b18daf38f312', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'd893e5d4-0911-4f2a-bbe6-b13c87582912', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '3462da24-e767-4e09-8dc7-1fb62ef5ded7', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '751b03bb-eb0f-4fb6-9dab-260603ba7047', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b9abf36f-b0b0-418a-88c0-e506a47b1d79', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2b4ea59c-451e-494e-a636-4577d0e3681a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00');
INSERT INTO public.location_operational_attributes VALUES
	('00000000-0000-4000-8000-000000000001', '4e187b23-f425-46e1-8229-2c336010d2f7', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'dfacb19e-17b7-4c6e-a5d3-d26f428af49f', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'dbbc9b1a-657a-434a-878f-b88c0b68dd14', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f9edc7e3-e82a-465b-9bf6-f78e70290567', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'ebac49b7-a109-4bd6-bc7a-d57228da7daf', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '3505169a-e3a9-49ab-b53b-06a8e7bc7eff', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '8da67e85-191d-48a8-9e4b-25f80d5bade0', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a0455d11-ab30-4342-bf38-66fa17dd67ba', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f998d2b4-db30-48e9-b28c-de9547f3e00f', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'a41bfeab-aecf-4fb7-818f-e0a49dd99906', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '72324bee-894f-428f-a32a-d827cda9a391', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'cea10527-ba5d-4a5f-8566-685949339c9d', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b1f93152-e4cb-4dce-b63a-9506ff2eb715', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '6369d559-41b8-4542-8ba6-b5896c049e97', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'fe565483-d5d0-4dde-9368-c53d4ab10130', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '12d1b43d-7c5f-4bde-a0f7-02263f7a1daa', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '388b27d1-93aa-4089-81ed-efbbf4471a80', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'c30eff0f-e1c6-46a1-a25b-03cdf50f9344', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'd63fbc4b-aff8-4061-a73c-b74c9a3ffa9b', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '18c0d33f-bda4-411f-a2f1-3baa5e6aea05', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e1194938-97f9-42d7-abfd-c0baa9fe4d70', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '419452ea-0126-4adb-b7cc-3cfe6e0555d0', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f9200f60-f088-4d70-ae80-63bdc0f21fa1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e34dfe46-d96f-4fb2-9d0b-58caa98c7261', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '2029cae2-ce86-4846-93c2-bfe589af8a13', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '032a4dc3-c4ad-4599-9a7e-267648b71514', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '87a0e148-6fd7-498b-9c76-5303be9f8cfd', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '59bc65c1-76e2-4476-96fc-c8fdb8d49aaa', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b69c6e20-39c7-4ef3-9700-cd10b7d1801e', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '426cd6df-a139-48c0-ba9e-e2c024d475fc', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'f89ab2f7-ac06-4ac1-bbe2-7b3955ff6d89', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '78406283-0613-4d6a-be16-6c39c1d2b5da', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '12989cab-293c-4f74-886f-42e0fc9aaf2a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'ec371491-a69f-4436-8e38-31edaa2402dd', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '5d44df17-3543-4aa9-ba13-86aafb712fc1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '63c3e4cf-5ea0-4383-8325-884e0a340b66', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '4d180d99-f30d-4b39-bb44-1650cae564f1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '09729942-dae8-4a68-be44-65c9baad4a0e', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '663b3a1a-03f3-434f-91d9-92db868c9a45', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '5f271f67-64e2-4c17-b8f9-42ea8e1e4be8', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e655f2b5-6b8c-46b0-94e4-b3b84b350faa', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'af3cf258-a5e1-443d-8147-a9d008c8508c', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e60ead57-1958-4253-bd74-9a9d6c7eafa1', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'daae1e05-4e93-4706-9f41-6b64f6fc4d02', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b3284943-bc7e-4287-93d5-17b8ea396c16', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '5907a1c0-dd65-477d-a400-b76ef6a98bb6', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '588de0d6-bc76-4011-a7b5-50e3686e4820', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '357b80ed-6e60-4ecd-a9e2-bc27e13b79c5', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '43071c6e-3b00-47a9-860c-1bbacb570575', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'd6ab53f7-6d1e-45f3-95db-5db9e62202c7', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'b2bc5182-3fed-405d-b1f7-ba1bd9381413', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', 'e787777f-ae5c-40ad-a139-0ce1fd32854a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '95253129-b40f-473f-a2a2-e1bbbb52ea1a', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '3b64e012-db90-45cd-b495-70e08b530f70', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '70a7dc11-010b-4f4f-9544-69211c3eb2f6', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '80bfd5da-4b4a-4439-aa83-906c409d32c2', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '653ec526-df9e-4488-96d4-4c55fca6e7cc', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00'),
	('00000000-0000-4000-8000-000000000001', '17cc504a-da61-4cf6-9539-cb2bc1f371e0', true, true, true, true, false, false, false, 0, NULL, '2026-07-16 07:30:25.013386+00');

--
-- Data for Name: location_projection_invalidations; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: location_review_items; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: locations; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.locations VALUES
	('00000000-0000-4000-8000-000000003000', '00000000-0000-4000-8000-000000000001', 'unknown', 'UNKNOWN', 'Unknown staging location', NULL, 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'staging', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('00000000-0000-4000-8000-000000003001', '00000000-0000-4000-8000-000000000001', 'park', 'CBE', 'Coimbatore', NULL, 'IN', 'Tamil Nadu', NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('00000000-0000-4000-8000-000000003002', '00000000-0000-4000-8000-000000000001', 'park', 'CPT', 'Channapatna', NULL, 'IN', 'Karnataka', NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('00000000-0000-4000-8000-000000003003', '00000000-0000-4000-8000-000000000001', 'unknown', 'HF_UNKNOWN', 'Unknown holding farm/source location', NULL, 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('6e769414-4dde-43a5-b30f-fb857392b638', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_5', 'Mandela 1 - Part 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('c93c636f-08f5-4ee9-aaca-bf7f1efd4788', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_8', 'Godel 1 - Part 8', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('99a847aa-6ab2-41e3-ba31-8a16714fa95a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_6', 'Godel 1 - Part 6', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a9da45a2-c0f5-4157-b12f-7b2aaf0412b7', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_1', 'Sumathi 2 - Part 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('83ced0ed-a9e5-415d-b06c-ad52dabebb7f', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_4', 'Yashoda 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f7e8a700-0fde-48e6-bee0-fadce1f6816d', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_7', 'Sumathi 1 - Part 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9da6b465-489d-43d5-bda9-2e85d168459d', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_2', 'Mandela 2 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('c3bcc632-baa3-40dd-b51d-6863d448e436', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_9', 'Godel 1 - Part 9', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('6306d03d-d1df-4bc9-9b1d-06bc9bdc6cfc', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_8', 'Sumathi 2 - Part 8', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f4c3ff39-92e8-4663-997c-8e02eeefea93', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_6', 'Sumathi 1 - Part 6', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('3d071720-b0b8-46b3-8d39-d716452c6a95', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_10', 'Godel 1 - Part 10', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('75731dd3-d669-4c12-8f6f-13caa501a5c1', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_1', 'Mandela 1 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9487caa4-fe05-4a42-ad01-0a6d0565f708', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_6', 'Godel 1 - Part 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('7250e591-64c8-478d-b020-0cc4a0ba1d91', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_5', 'Sumathi 1 - Part 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b2a5fd25-4d81-408d-81e0-e07d680cc84a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_OLD_YASHODA_3', 'Old Yashoda 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('268ab2ee-352c-4918-a26d-0ac745848d50', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GANDHI_1', 'Gandhi 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9dc0befd-9a0b-45c6-8d85-1631315bc4a6', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_7', 'Mandela 1 - Part 7', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('ad78d307-1b58-4a35-a765-7dda87462a31', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_10', 'Sumathi 2 - Part 10', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b2850cfd-f76c-409d-a54e-39211a1d253e', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_3', 'Mandela 1 - Part 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('7a7e29e0-95f1-4d58-bc31-a908461ec4ff', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_10', 'Sumathi 2 - Part 10', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('dc208497-043e-4382-afd2-21d066023c37', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_4', 'Sumathi 1 - Part 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('bbeb5191-475f-4dc1-a217-a1ad71f55bcc', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_3', 'Mandela 1 - Part 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9ab6c229-b1e0-4ba5-96f9-bad7d5a792df', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_4', 'Godel 1 - Part 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('c5864d6b-d88a-4aec-8586-d4d52eddeff9', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_5', 'Sumathi 2 - Part 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('0a7d46b6-9167-4e63-9bc3-44130b7d4139', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_Q3', 'Q3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('8f92244c-ffde-4ea9-a0bd-f21d201c77f8', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_4', 'Godel 1 - Part 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('fd566afa-c9b1-45e6-bf03-01aecc2a4808', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_4', 'Mandela 2 - Part 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('459b94bd-54b0-49a0-999e-7d5aa30f1654', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_9', 'Sumathi 1 - Part 9', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('7e7b6df1-9a5b-4be7-b341-f861d5e3d158', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_8', 'Godel 2 - Part 8', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e5d83dcf-28c8-4d32-8f5c-6c406a486bf1', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_YASHODA_3', 'Yashoda 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('29808fb0-e73d-4c36-b421-2e36611a6829', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GANDHI_3', 'Gandhi 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a6f38bb5-2382-4695-811b-10e5d511e0ca', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_6', 'Sumathi 1 - Part 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9ceae893-5c23-466b-8b82-637af935bb6d', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GANDHI_2', 'Gandhi 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9005e6bd-d819-43ae-8f6d-f9a1785cebae', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_9', 'Godel 1 - Part 9', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b3601c9f-d38c-4daa-94b4-6767418bd897', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_5', 'Sumathi 1 - Part 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('3af7a10a-c87c-4df6-9b70-65616c9e0016', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_2', 'Sumathi 1 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('9714009c-f4fa-478e-9835-e3d79ae138ca', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_3', 'Sumathi 2 - Part 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('14dcec8a-15bb-48c1-a1e8-c0e3b259c7a2', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_3', 'Mandela 2 - Part 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('59f9d555-c472-4a10-9a3b-4c197be66aa4', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_3', 'Yashoda 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('7edf1f74-85ad-4c99-ae6a-897b9b6a3316', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_YASHODA_4', 'Yashoda 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2159c522-35f4-4490-a18f-62b8b45fb845', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_2', 'Mandela 1 - Part 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('4843feae-1daa-40bc-8137-2c34b86167c1', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_1', 'Yashoda 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('af8e6f19-a13b-4cd9-9d90-b02e0b756dad', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_3', 'Godel 2 - Part 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('7f02354a-337b-47b0-b9ef-0ff62623d0bd', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_HO_CHI_MINH_2', 'Ho Chi Minh 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a9ecf757-1dac-4904-ba59-d66e55cf3dde', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_7', 'Sumathi 1 - Part 7', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('1d4c5159-ad13-4b70-a2cc-650853143c9c', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_9', 'Sumathi 2 - Part 9', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e259c816-a642-4d75-9886-4f13fbe5d5a0', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_1', 'Sumathi 1 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('d375cb79-dbcc-4255-beff-a4a64de689cc', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_6', 'Mandela 1 - Part 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a4ec250f-083b-4fdb-b559-feef726b0f9e', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_8', 'Godel 2 - Part 8', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('47b4e8af-f70a-451e-9159-fed304ebe86d', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_HO_CHI_MINH_1', 'Ho Chi Minh 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('69d53b25-183b-4dfc-8f4a-df0272aff2f8', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_8', 'Sumathi 1 - Part 8', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('43042c7b-d850-400f-800f-eafd171c2122', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_2', 'Sumathi 1 - Part 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('86e47f9c-fd1d-461d-9b9a-45d3be9bf12d', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_Q1', 'Q1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b6031aa6-41b5-4d00-ba05-b5c95254227f', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_HO_CHI_MINH_1', 'Ho Chi Minh 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2ebb5cc0-9dcd-4d7f-a4bc-29cedd471e49', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_4', 'Godel 2 - Part 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('654260da-956e-4015-bc95-edf3421cae3c', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_3', 'Godel 1 - Part 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('c79de5a7-a954-488c-bc05-16b261ec3073', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_7', 'Godel 2 - Part 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('5d72bd14-4907-46c9-94ff-7405dc4a7e9a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_2', 'Sumathi 2 - Part 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('c95797f6-577f-4ca7-8df4-7f8a5c506310', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_5', 'Mandela 2 - Part 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f1b1bad0-47ab-4248-95dc-8fa1472d4fec', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GANDHI_1_PART_1', 'Gandhi 1 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2654ebaf-d11a-4a89-8fd0-7dd848aaec8a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_10', 'Sumathi 1 - Part 10', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('38ddac27-307c-44eb-a56c-91a6935c3881', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_OLD_YASHODA_2', 'Old Yashoda 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('cbd67ce0-ef0f-44eb-bf80-18fa8a36c44a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_CASTRO_2', 'Castro 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b1fa89fd-76b0-4325-a0ba-97ce1ce5bd8f', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_6', 'Sumathi 2 - Part 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('ff2dd02d-b7a3-477b-b616-3bcd636782af', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_1', 'Mandela 2 - Part 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('99cc07b9-94ae-4d68-aa61-bb9efd683071', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_4', 'Mandela 1 - Part 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b114a8f1-3640-4d19-a6ce-487318d16f1b', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_4', 'Mandela 1 - Part 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a7791a88-e707-401d-943b-353acc7ea0fc', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_7', 'Mandela 2 - Part 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('bdde07a8-aae7-4032-be5a-80f193686bc8', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_6', 'Sumathi 2 - Part 6', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('634dd132-daa4-4e84-8e94-37d542c7d13a', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_10', 'Sumathi 1 - Part 10', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b6cf7b79-e0a3-4db0-8ab7-f366cf1b2b57', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_10', 'Mandela 2 - Part 10', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a7efb805-ae6c-4bdf-b162-0b51c34045c3', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_9', 'Yashoda 9', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('5e75c070-740e-4a17-b30c-352e051f4bb7', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_10', 'Yashoda 10', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2649843b-1b78-49e4-b97f-b1ad2249f637', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_1', 'Godel 1 - Part 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('0242b46a-4af3-49b6-ae95-1f35f780e814', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_1', 'Mandela 2 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('323655db-413b-4709-8600-1e1ddfcbff95', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_7', 'Mandela 1 - Part 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('6cff3365-2f55-4c38-8a82-bad8d2f502bd', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_YASHODA_2', 'Yashoda 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2b970871-1715-4901-bdc7-a494c058d798', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_6', 'Mandela 2 - Part 6', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2fab4267-0225-48d1-a14e-c54f951a791e', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_6', 'Yashoda 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('4ca20708-e804-4807-a312-fe13cb9b5b08', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_3', 'Mandela 2 - Part 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('1979930e-63a3-4ef6-9afd-fa04a442de4b', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_7', 'Godel 1 - Part 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('da1f37fc-a939-4357-a980-6e96914dfee4', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_1', 'Godel 1 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('84bc5d8b-96cd-4de2-a875-b2e19b20ac93', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_OLD_YASHODA_1', 'Old Yashoda 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('0e9518c7-de33-474e-ba90-877cc942d1d7', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_HO_CHI_MINH_2', 'Ho Chi Minh 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('699f819d-7b04-4bd4-ad83-63be42dfc7aa', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_10', 'Godel 2 - Part 10', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2c8596da-0b7b-4f10-9d9f-32410d2a080c', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_9', 'Mandela 2 - Part 9', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('21fd7aff-e93f-45fe-8346-d7deea5d6c88', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_3', 'Sumathi 2 - Part 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('6dfaffaf-6186-46ef-adf8-636c3017b324', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_2', 'Godel 1 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('72c34504-2488-45ed-9bb1-8c97168f72c4', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_CASTRO_2', 'Castro 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('7e8dee2b-f7f9-4f04-9da0-d550569b9a7f', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_5', 'Sumathi 2 - Part 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('825e5b4e-f7ab-48a4-aae1-b18daf38f312', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_CASTRO_1', 'Castro 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('d893e5d4-0911-4f2a-bbe6-b13c87582912', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_OLD_YASHODA_4', 'Old Yashoda 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('3462da24-e767-4e09-8dc7-1fb62ef5ded7', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_7', 'Sumathi 2 - Part 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('751b03bb-eb0f-4fb6-9dab-260603ba7047', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_CASTRO_3', 'Castro 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b9abf36f-b0b0-418a-88c0-e506a47b1d79', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_5', 'Godel 2 - Part 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2b4ea59c-451e-494e-a636-4577d0e3681a', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_1', 'Sumathi 2 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL);
INSERT INTO public.locations VALUES
	('4e187b23-f425-46e1-8229-2c336010d2f7', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_OLD_YASHODA_5', 'Old Yashoda 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('dfacb19e-17b7-4c6e-a5d3-d26f428af49f', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_8', 'Mandela 1 - Part 8', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('dbbc9b1a-657a-434a-878f-b88c0b68dd14', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_2', 'Mandela 1 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f9edc7e3-e82a-465b-9bf6-f78e70290567', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_10', 'Godel 2 - Part 10', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('ebac49b7-a109-4bd6-bc7a-d57228da7daf', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_10', 'Godel 1 - Part 10', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('3505169a-e3a9-49ab-b53b-06a8e7bc7eff', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_1', 'Godel 2 - Part 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('8da67e85-191d-48a8-9e4b-25f80d5bade0', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_3', 'Sumathi 1 - Part 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a0455d11-ab30-4342-bf38-66fa17dd67ba', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_4', 'Mandela 2 - Part 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f998d2b4-db30-48e9-b28c-de9547f3e00f', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_7', 'Mandela 2 - Part 7', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('a41bfeab-aecf-4fb7-818f-e0a49dd99906', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GANDHI_3', 'Gandhi 3', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('72324bee-894f-428f-a32a-d827cda9a391', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_8', 'Yashoda 8', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('cea10527-ba5d-4a5f-8566-685949339c9d', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_1', 'Sumathi 1 - Part 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b1f93152-e4cb-4dce-b63a-9506ff2eb715', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GANDHI_1_PART_2', 'Gandhi 1 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('6369d559-41b8-4542-8ba6-b5896c049e97', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_2', 'Mandela 2 - Part 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('fe565483-d5d0-4dde-9368-c53d4ab10130', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_2', 'Godel 2 - Part 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('12d1b43d-7c5f-4bde-a0f7-02263f7a1daa', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_8', 'Mandela 2 - Part 8', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('388b27d1-93aa-4089-81ed-efbbf4471a80', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_1_PART_5', 'Godel 1 - Part 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('c30eff0f-e1c6-46a1-a25b-03cdf50f9344', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_9', 'Sumathi 1 - Part 9', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('d63fbc4b-aff8-4061-a73c-b74c9a3ffa9b', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_1', 'Godel 2 - Part 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('18c0d33f-bda4-411f-a2f1-3baa5e6aea05', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_1_PART_5', 'Mandela 1 - Part 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e1194938-97f9-42d7-abfd-c0baa9fe4d70', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_10', 'Mandela 1 - Part 10', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('419452ea-0126-4adb-b7cc-3cfe6e0555d0', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_4', 'Sumathi 1 - Part 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f9200f60-f088-4d70-ae80-63bdc0f21fa1', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_9', 'Sumathi 2 - Part 9', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e34dfe46-d96f-4fb2-9d0b-58caa98c7261', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_YASHODA_1', 'Yashoda 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('2029cae2-ce86-4846-93c2-bfe589af8a13', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_9', 'Godel 2 - Part 9', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('032a4dc3-c4ad-4599-9a7e-267648b71514', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_5', 'Godel 1 - Part 5', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('87a0e148-6fd7-498b-9c76-5303be9f8cfd', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_2', 'Godel 2 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('59bc65c1-76e2-4476-96fc-c8fdb8d49aaa', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_7', 'Sumathi 2 - Part 7', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b69c6e20-39c7-4ef3-9700-cd10b7d1801e', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_7', 'Yashoda 7', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('426cd6df-a139-48c0-ba9e-e2c024d475fc', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_6', 'Godel 2 - Part 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('f89ab2f7-ac06-4ac1-bbe2-7b3955ff6d89', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GANDHI_1', 'Gandhi 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('78406283-0613-4d6a-be16-6c39c1d2b5da', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_2_PART_4', 'Sumathi 2 - Part 4', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('12989cab-293c-4f74-886f-42e0fc9aaf2a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_9', 'Mandela 1 - Part 9', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('ec371491-a69f-4436-8e38-31edaa2402dd', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_6', 'Mandela 1 - Part 6', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('5d44df17-3543-4aa9-ba13-86aafb712fc1', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_1_PART_1', 'Mandela 1 - Part 1', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('63c3e4cf-5ea0-4383-8325-884e0a340b66', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_2', 'Sumathi 2 - Part 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('4d180d99-f30d-4b39-bb44-1650cae564f1', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_4', 'Godel 2 - Part 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('09729942-dae8-4a68-be44-65c9baad4a0e', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_Q2', 'Q2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('663b3a1a-03f3-434f-91d9-92db868c9a45', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GANDHI_2', 'Gandhi 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('5f271f67-64e2-4c17-b8f9-42ea8e1e4be8', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_2', 'Godel 1 - Part 2', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e655f2b5-6b8c-46b0-94e4-b3b84b350faa', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_9', 'Godel 2 - Part 9', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('af3cf258-a5e1-443d-8147-a9d008c8508c', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_1_PART_8', 'Sumathi 1 - Part 8', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e60ead57-1958-4253-bd74-9a9d6c7eafa1', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_2', 'Yashoda 2', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('daae1e05-4e93-4706-9f41-6b64f6fc4d02', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_6', 'Mandela 2 - Part 6', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b3284943-bc7e-4287-93d5-17b8ea396c16', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_7', 'Godel 2 - Part 7', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('5907a1c0-dd65-477d-a400-b76ef6a98bb6', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_GODEL_2_PART_5', 'Godel 2 - Part 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('588de0d6-bc76-4011-a7b5-50e3686e4820', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_MANDELA_2_PART_5', 'Mandela 2 - Part 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('357b80ed-6e60-4ecd-a9e2-bc27e13b79c5', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_MANDELA_2_PART_8', 'Mandela 2 - Part 8', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('43071c6e-3b00-47a9-860c-1bbacb570575', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_CASTRO_1', 'Castro 1', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('d6ab53f7-6d1e-45f3-95db-5db9e62202c7', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_YASHODA_5', 'Yashoda 5', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('b2bc5182-3fed-405d-b1f7-ba1bd9381413', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_SUMATHI_1_PART_3', 'Sumathi 1 - Part 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('e787777f-ae5c-40ad-a139-0ce1fd32854a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_3', 'Godel 1 - Part 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('95253129-b40f-473f-a2a2-e1bbbb52ea1a', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_6', 'Godel 2 - Part 6', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('3b64e012-db90-45cd-b495-70e08b530f70', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_4', 'Sumathi 2 - Part 4', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('70a7dc11-010b-4f4f-9544-69211c3eb2f6', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_8', 'Godel 1 - Part 8', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('80bfd5da-4b4a-4439-aa83-906c409d32c2', '00000000-0000-4000-8000-000000000001', 'shed', 'CBE_SHED_SUMATHI_2_PART_8', 'Sumathi 2 - Part 8', '00000000-0000-4000-8000-000000003001', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('653ec526-df9e-4488-96d4-4c55fca6e7cc', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_2_PART_3', 'Godel 2 - Part 3', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL),
	('17cc504a-da61-4cf6-9539-cb2bc1f371e0', '00000000-0000-4000-8000-000000000001', 'shed', 'CPT_SHED_GODEL_1_PART_7', 'Godel 1 - Part 7', '00000000-0000-4000-8000-000000003002', 'IN', NULL, NULL, NULL, NULL, NULL, 'Asia/Kolkata', 'active', '2026-07-16 07:30:24.942787+00', '2026-07-16 07:30:25.013386+00', 1, 0, NULL, NULL, NULL);

--
-- Data for Name: movement_commands; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: notification_delivery_attempts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: notification_requests; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: obligation_batches; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: obligation_escalations; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: obligation_goat_shift_watermarks; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: obligation_instances; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: obligation_status_events; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: org_role_catalog; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.org_role_catalog VALUES
	('admin', 'ceo_cxo', NULL, true, 'Admin (system/product admin, legacy flat)', '2026-07-16 07:30:26.838905+00'),
	('ceo_internal', 'ceo_cxo', NULL, true, 'CEO / CxO (founder/builder cohort, legacy flat)', '2026-07-16 07:30:26.838905+00'),
	('verifier', 'director', NULL, true, 'Verifier (video verification team, cross-vertical, legacy flat)', '2026-07-16 07:30:26.838905+00'),
	('park_head', 'head', NULL, true, 'Park Head (legacy flat, pre org-role-model)', '2026-07-16 07:30:26.838905+00'),
	('pc_director', 'director', 'preventive_care', true, 'Preventive Care Director (legacy flat)', '2026-07-16 07:30:26.838905+00'),
	('operator', 'am', NULL, true, 'Operator (legacy flat ground executor)', '2026-07-16 07:30:26.838905+00'),
	('director_procurement', 'director', 'procurement', false, 'Director -- Procurement', '2026-07-16 07:30:26.838905+00'),
	('director_preventive_care', 'director', 'preventive_care', false, 'Director -- Preventive Care', '2026-07-16 07:30:26.838905+00'),
	('director_breeding', 'director', 'breeding', false, 'Director -- Breeding', '2026-07-16 07:30:26.838905+00'),
	('director_health', 'director', 'health', false, 'Director -- Health', '2026-07-16 07:30:26.838905+00'),
	('director_growth', 'director', 'growth', false, 'Director -- Growth', '2026-07-16 07:30:26.838905+00'),
	('director_infrastructure', 'director', 'infrastructure', false, 'Director -- Infrastructure', '2026-07-16 07:30:26.838905+00'),
	('director_feed', 'director', 'feed', false, 'Director -- Feed', '2026-07-16 07:30:26.838905+00'),
	('director_milk', 'director', 'milk', false, 'Director -- Milk', '2026-07-16 07:30:26.838905+00'),
	('director_sales', 'director', 'sales', false, 'Director -- Sales', '2026-07-16 07:30:26.838905+00'),
	('head_procurement', 'head', 'procurement', false, 'Head (Ops-Head) -- Procurement', '2026-07-16 07:30:26.838905+00'),
	('head_preventive_care', 'head', 'preventive_care', false, 'Head (Ops-Head) -- Preventive Care', '2026-07-16 07:30:26.838905+00'),
	('head_breeding', 'head', 'breeding', false, 'Head (Ops-Head) -- Breeding', '2026-07-16 07:30:26.838905+00'),
	('head_health', 'head', 'health', false, 'Head (Ops-Head) -- Health', '2026-07-16 07:30:26.838905+00'),
	('head_growth', 'head', 'growth', false, 'Head (Ops-Head) -- Growth', '2026-07-16 07:30:26.838905+00'),
	('head_infrastructure', 'head', 'infrastructure', false, 'Head (Ops-Head) -- Infrastructure', '2026-07-16 07:30:26.838905+00'),
	('head_feed', 'head', 'feed', false, 'Head (Ops-Head) -- Feed', '2026-07-16 07:30:26.838905+00'),
	('head_milk', 'head', 'milk', false, 'Head (Ops-Head) -- Milk', '2026-07-16 07:30:26.838905+00'),
	('head_sales', 'head', 'sales', false, 'Head (Ops-Head) -- Sales', '2026-07-16 07:30:26.838905+00'),
	('manager_procurement', 'manager', 'procurement', false, 'Manager -- Procurement', '2026-07-16 07:30:26.838905+00'),
	('manager_preventive_care', 'manager', 'preventive_care', false, 'Manager -- Preventive Care', '2026-07-16 07:30:26.838905+00'),
	('manager_breeding', 'manager', 'breeding', false, 'Manager -- Breeding', '2026-07-16 07:30:26.838905+00'),
	('manager_health', 'manager', 'health', false, 'Manager -- Health', '2026-07-16 07:30:26.838905+00'),
	('manager_growth', 'manager', 'growth', false, 'Manager -- Growth', '2026-07-16 07:30:26.838905+00'),
	('manager_infrastructure', 'manager', 'infrastructure', false, 'Manager -- Infrastructure', '2026-07-16 07:30:26.838905+00'),
	('manager_feed', 'manager', 'feed', false, 'Manager -- Feed', '2026-07-16 07:30:26.838905+00'),
	('manager_milk', 'manager', 'milk', false, 'Manager -- Milk', '2026-07-16 07:30:26.838905+00'),
	('manager_sales', 'manager', 'sales', false, 'Manager -- Sales', '2026-07-16 07:30:26.838905+00'),
	('am_procurement', 'am', 'procurement', false, 'Assistant Manager -- Procurement', '2026-07-16 07:30:26.838905+00'),
	('am_preventive_care', 'am', 'preventive_care', false, 'Assistant Manager -- Preventive Care', '2026-07-16 07:30:26.838905+00'),
	('am_breeding', 'am', 'breeding', false, 'Assistant Manager -- Breeding', '2026-07-16 07:30:26.838905+00'),
	('am_health', 'am', 'health', false, 'Assistant Manager -- Health', '2026-07-16 07:30:26.838905+00'),
	('am_growth', 'am', 'growth', false, 'Assistant Manager -- Growth', '2026-07-16 07:30:26.838905+00'),
	('am_infrastructure', 'am', 'infrastructure', false, 'Assistant Manager -- Infrastructure', '2026-07-16 07:30:26.838905+00'),
	('am_feed', 'am', 'feed', false, 'Assistant Manager -- Feed', '2026-07-16 07:30:26.838905+00'),
	('am_milk', 'am', 'milk', false, 'Assistant Manager -- Milk', '2026-07-16 07:30:26.838905+00'),
	('am_sales', 'am', 'sales', false, 'Assistant Manager -- Sales', '2026-07-16 07:30:26.838905+00');

--
-- Data for Name: org_tiers; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.org_tiers VALUES
	('ceo_cxo', 'CEO / CxO', 1),
	('director', 'Director', 2),
	('head', 'Head (Ops-Head)', 3),
	('manager', 'Manager', 4),
	('am', 'Assistant Manager', 5);

--
-- Data for Name: org_verticals; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.org_verticals VALUES
	('procurement', 'Procurement', 1),
	('preventive_care', 'Preventive Care', 2),
	('breeding', 'Breeding', 3),
	('health', 'Health', 4),
	('growth', 'Growth', 5),
	('infrastructure', 'Infrastructure', 6),
	('feed', 'Feed', 7),
	('milk', 'Milk', 8),
	('sales', 'Sales', 9);

--
-- Data for Name: orgs; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.orgs VALUES
	('00000000-0000-4000-8000-000000001001', 'mesha', 'Mesha', 'active', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001101', 'vendor', 'Rajasthan Farms', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001102', 'vendor', 'Gokul Agronomics', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001103', 'vendor', 'Goat World', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001104', 'vendor', 'Bhopal Agro', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00');

--
-- Data for Name: outbox_dlq_actions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: outbox_messages; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.outbox_messages VALUES
	('decb8b91-b214-41c1-9ac4-bc85e6f9e9fd', '00000000-0000-4000-8000-000000000001', '10c77605-1dc1-e994-ae30-8bfc3910e345', 'config.changed', '1.0.0', 'admin_ui_config_family', '00000000-0000-4000-8000-000000000001', 'config.changed', '{"actor": {"actor_id": null, "actor_ref": null, "actor_type": "system_rule"}, "payload": {"source": "status_definitions.insert", "metadata": {"table": "status_definitions", "operation": "INSERT", "coalesced_change_count": 2}, "revision": 1, "tenant_id": "00000000-0000-4000-8000-000000000001", "family_key": "status:health", "content_hash": "f3ab1f8def8f245bdfff43c381e46985"}, "event_id": "10c77605-1dc1-e994-ae30-8bfc3910e345", "producer": {"module": "admin_ui_config_family_revisions", "service": "postgres", "version": null}, "trace_id": "admin-ui-config:00000000-0000-4000-8000-000000000001:status:health:1", "event_type": "config.changed", "schema_ref": "contracts/jsonschema/domain-event-envelope.schema.json#config.changed", "subject_id": "status:health:1", "occurred_at": "2026-07-16T07:30:26.070397Z", "recorded_at": "2026-07-16T07:30:26.070397Z", "aggregate_id": "00000000-0000-4000-8000-000000000001", "subject_type": "admin_ui_config_family", "evidence_refs": [{"evidence_id": "status:health:1", "evidence_type": "event"}], "aggregate_type": "admin_ui_config_family", "schema_version": "1.0.0", "idempotency_key": "admin-ui-config:00000000-0000-4000-8000-000000000001:status:health:1", "visibility_scope": {"tenant_id": "00000000-0000-4000-8000-000000000001"}}', '{"producer": "postgres.admin_ui_config_family_revisions", "schema_version": "1.0.0", "legacy_repaired_by": "000162_repair_legacy_config_changed_outbox_envelopes"}', 'admin-ui-config:00000000-0000-4000-8000-000000000001:status:health:1', 'admin-ui-config:00000000-0000-4000-8000-000000000001:status:health:1', 'pending', 0, '2026-07-16 07:30:26.070397+00', NULL, NULL, '2026-07-16 07:30:26.070397+00', '2026-07-16 07:30:26.662991+00', 0),
	('9412f76f-bc5c-4ca7-9bb0-cfb55351ac04', '00000000-0000-4000-8000-000000000001', '6f87fc81-82d4-d040-c3ef-c10610f06cdf', 'config.changed', '1.0.0', 'admin_ui_config_family', '00000000-0000-4000-8000-000000000001', 'config.changed', '{"actor": {"actor_id": null, "actor_ref": null, "actor_type": "system_rule"}, "payload": {"source": "status_definitions.insert", "metadata": {"table": "status_definitions", "operation": "INSERT", "coalesced_change_count": 2}, "revision": 1, "tenant_id": "00000000-0000-4000-8000-000000000001", "family_key": "config", "content_hash": "abf950783d9736805a7c7ce25cf3ffc9"}, "event_id": "6f87fc81-82d4-d040-c3ef-c10610f06cdf", "producer": {"module": "admin_ui_config_family_revisions", "service": "postgres", "version": null}, "trace_id": "admin-ui-config:00000000-0000-4000-8000-000000000001:config:1", "event_type": "config.changed", "schema_ref": "contracts/jsonschema/domain-event-envelope.schema.json#config.changed", "subject_id": "config:1", "occurred_at": "2026-07-16T07:30:26.070397Z", "recorded_at": "2026-07-16T07:30:26.070397Z", "aggregate_id": "00000000-0000-4000-8000-000000000001", "subject_type": "admin_ui_config_family", "evidence_refs": [{"evidence_id": "config:1", "evidence_type": "event"}], "aggregate_type": "admin_ui_config_family", "schema_version": "1.0.0", "idempotency_key": "admin-ui-config:00000000-0000-4000-8000-000000000001:config:1", "visibility_scope": {"tenant_id": "00000000-0000-4000-8000-000000000001"}}', '{"producer": "postgres.admin_ui_config_family_revisions", "schema_version": "1.0.0", "legacy_repaired_by": "000162_repair_legacy_config_changed_outbox_envelopes"}', 'admin-ui-config:00000000-0000-4000-8000-000000000001:config:1', 'admin-ui-config:00000000-0000-4000-8000-000000000001:config:1', 'pending', 0, '2026-07-16 07:30:26.070397+00', NULL, NULL, '2026-07-16 07:30:26.070397+00', '2026-07-16 07:30:26.662991+00', 0),
	('efcc11fc-4bd0-4d16-9e94-c30afa4ff42e', '00000000-0000-4000-8000-000000000001', '4324e050-9899-b998-899c-59307981c65a', 'config.changed', '1.0.0', 'admin_ui_config_family', '00000000-0000-4000-8000-000000000001', 'config.changed', '{"actor": {"actor_id": null, "actor_ref": null, "actor_type": "system_rule"}, "payload": {"source": "sop_versions.update", "metadata": {"table": "sop_versions", "operation": "UPDATE", "coalesced_change_count": 1}, "revision": 1, "tenant_id": "00000000-0000-4000-8000-000000000001", "family_key": "sops:vaccination", "content_hash": "06d4ff51e14dce2b5cec21fe6b2b2d69"}, "event_id": "4324e050-9899-b998-899c-59307981c65a", "producer": {"module": "admin_ui_config_family_revisions", "service": "postgres", "version": null}, "trace_id": "admin-ui-config:00000000-0000-4000-8000-000000000001:sops:vaccination:1", "event_type": "config.changed", "schema_ref": "contracts/jsonschema/domain-event-envelope.schema.json#config.changed", "subject_id": "sops:vaccination:1", "occurred_at": "2026-07-16T07:30:26.760024Z", "recorded_at": "2026-07-16T07:30:26.760024Z", "aggregate_id": "00000000-0000-4000-8000-000000000001", "subject_type": "admin_ui_config_family", "evidence_refs": [{"evidence_id": "sops:vaccination:1", "evidence_type": "event"}], "aggregate_type": "admin_ui_config_family", "schema_version": "1.0.0", "idempotency_key": "admin-ui-config:00000000-0000-4000-8000-000000000001:sops:vaccination:1", "visibility_scope": {"tenant_id": "00000000-0000-4000-8000-000000000001"}}', '{"producer": "postgres.admin_ui_config_family_revisions", "schema_version": "1.0.0"}', 'admin-ui-config:00000000-0000-4000-8000-000000000001:sops:vaccination:1', 'admin-ui-config:00000000-0000-4000-8000-000000000001:sops:vaccination:1', 'pending', 0, '2026-07-16 07:30:26.753457+00', NULL, NULL, '2026-07-16 07:30:26.753457+00', '2026-07-16 07:30:26.753457+00', 0),
	('79ee22d9-6d6f-4f4a-b4e0-e5790aa6e01a', '00000000-0000-4000-8000-000000000001', '42950e71-8da6-5c40-8182-6f4c0da6a6f9', 'config.changed', '1.0.0', 'admin_ui_config_family', '00000000-0000-4000-8000-000000000001', 'config.changed', '{"actor": {"actor_id": null, "actor_ref": null, "actor_type": "system_rule"}, "payload": {"source": "sop_versions.update", "metadata": {"table": "sop_versions", "operation": "UPDATE", "coalesced_change_count": 1}, "revision": 2, "tenant_id": "00000000-0000-4000-8000-000000000001", "family_key": "config", "content_hash": "d4debeb5fad3547760da88c9fd7b8783"}, "event_id": "42950e71-8da6-5c40-8182-6f4c0da6a6f9", "producer": {"module": "admin_ui_config_family_revisions", "service": "postgres", "version": null}, "trace_id": "admin-ui-config:00000000-0000-4000-8000-000000000001:config:2", "event_type": "config.changed", "schema_ref": "contracts/jsonschema/domain-event-envelope.schema.json#config.changed", "subject_id": "config:2", "occurred_at": "2026-07-16T07:30:26.761468Z", "recorded_at": "2026-07-16T07:30:26.761468Z", "aggregate_id": "00000000-0000-4000-8000-000000000001", "subject_type": "admin_ui_config_family", "evidence_refs": [{"evidence_id": "config:2", "evidence_type": "event"}], "aggregate_type": "admin_ui_config_family", "schema_version": "1.0.0", "idempotency_key": "admin-ui-config:00000000-0000-4000-8000-000000000001:config:2", "visibility_scope": {"tenant_id": "00000000-0000-4000-8000-000000000001"}}', '{"producer": "postgres.admin_ui_config_family_revisions", "schema_version": "1.0.0"}', 'admin-ui-config:00000000-0000-4000-8000-000000000001:config:2', 'admin-ui-config:00000000-0000-4000-8000-000000000001:config:2', 'pending', 0, '2026-07-16 07:30:26.753457+00', NULL, NULL, '2026-07-16 07:30:26.753457+00', '2026-07-16 07:30:26.753457+00', 0);

--
-- Data for Name: park_profiles; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: parties; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.parties VALUES
	('00000000-0000-4000-8000-000000001001', 'org', 'Mesha', 'active', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001101', 'org', 'Rajasthan Farms', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001102', 'org', 'Gokul Agronomics', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001103', 'org', 'Goat World', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('00000000-0000-4000-8000-000000001104', 'org', 'Bhopal Agro', 'review', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00');

--
-- Data for Name: position_module_duties; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: procurement_hf_vaccination_evidence; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: procurement_load_goats; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: procurement_loads; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: procurement_pc_handoffs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: procurement_source_health_checks; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: proof_artifacts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: protocol_definitions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: protocol_rule_dimensions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: protocol_rules; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: protocol_triggers; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: protocol_versions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: reminder_cadence_progress; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: seed_runs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: shed_lifecycle_status_lookup; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: shed_profiles; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: shifting_event_impacts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: shifting_events; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: sop_definitions; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.sop_definitions VALUES
	('27b220ba-6021-4c8a-a06a-d0f07b9d3cbe', '00000000-0000-4000-8000-000000000001', 'shifting', 'Shifting', 'Move goats from one location to another with proof and verification.', 'active', NULL, '2026-07-16 07:30:25.287157+00', '2026-07-16 07:30:25.287157+00', 1),
	('b0000000-0000-4000-8000-000000000003', '00000000-0000-4000-8000-000000000001', 'feed.direction', 'Feed direction', 'Shed feed direction execution + proof skeleton.', 'draft', NULL, '2026-07-16 07:30:25.635797+00', '2026-07-16 07:30:25.635797+00', 1),
	('b0000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000001', 'vaccination.drive', 'Vaccination Session', 'Shed or cohort vaccination drive execution with backend-owned proof and verification.', 'active', NULL, '2026-07-16 07:30:25.608363+00', '2026-07-16 07:30:25.667615+00', 2);

--
-- Data for Name: sop_submission_items; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: sop_submissions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: sop_task_review_fanouts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: sop_task_submission_fanouts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: sop_tasks; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: sop_versions; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.sop_versions VALUES
	('d1919c8b-a509-45f0-89e9-986ee1157c0a', '00000000-0000-4000-8000-000000000001', '27b220ba-6021-4c8a-a06a-d0f07b9d3cbe', 1, 'Shifting v1', 'published', '{"rules": [{"type": "block_submission_if", "when": {"field": "destination_location_id", "operator": "empty"}, "message": "Destination location is required."}, {"type": "proof_required_if", "when": {"field": "category", "operator": "not_empty"}, "field": "proof_video", "message": "Video proof is required before verification."}], "title": "Shifting", "fields": [{"key": "shift_type", "type": "select", "label": "Shift Type", "options": ["request", "direction"], "required": true}, {"key": "category", "type": "select", "label": "Category", "options": ["routine", "health", "sale", "procurement", "emergency"], "required": true}, {"key": "priority", "type": "select", "label": "Priority", "options": ["low", "normal", "high", "urgent"], "required": true}, {"key": "goat_ids", "type": "goat_lookup", "label": "Goats", "repeat": true, "required": true}, {"key": "source_location_id", "type": "location_picker", "label": "Source Location", "required": true, "option_source": "locations.active"}, {"key": "destination_location_id", "type": "location_picker", "label": "Destination Location", "required": true, "option_source": "locations.active"}, {"key": "destination_count", "min": 1, "type": "number", "label": "Destination Count", "required": true}, {"key": "reason", "type": "text", "label": "Reason", "required": false}, {"key": "proof_video", "type": "video_proof", "label": "Destination Proof Video", "required": true, "proof_action": "video.capture"}], "sop_code": "shifting", "workflow": {"edges": [{"to": "proof_verification", "from": "operator_submission", "condition": "proof_required"}, {"to": "accepted", "from": "proof_verification", "condition": "verifier_approves"}, {"to": "rework_requested", "from": "proof_verification", "condition": "verifier_rejects"}], "nodes": [{"key": "operator_submission", "type": "operator_execution", "label": "Operator Submission"}, {"key": "proof_verification", "type": "proof_verification", "label": "Proof Verification"}, {"key": "accepted", "type": "accepted", "label": "Accepted"}, {"key": "rework_requested", "type": "rework", "label": "Rework Requested"}]}, "schema_version": "goatos.sop-form.v1", "repeat_for_each_goat": true}', '{"scope": "batch", "types": ["video"], "required": true, "minimum_count": 1, "verify_before_apply": true}', '{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "select", "multiselect", "goat_lookup", "rfid_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}', '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded Shifting v1 walking skeleton."}]}', NULL, NULL, '2026-07-16 07:30:25.287157+00', NULL, '2026-07-16 07:30:25.287157+00', '2026-07-16 07:30:25.287157+00', 1),
	('b0000000-0000-4000-8000-000000000004', '00000000-0000-4000-8000-000000000001', 'b0000000-0000-4000-8000-000000000003', 1, 'v1-skeleton', 'draft', '{"steps": [{"key": "shed_feed_video", "type": "video", "label": "Shed feed video"}, {"key": "feed_lot", "type": "scan", "label": "Feed lot proof"}, {"key": "quantity_fed", "type": "number", "label": "Quantity fed"}, {"key": "head_count", "type": "number", "label": "Head count"}, {"key": "fed_at", "type": "datetime", "label": "Fed at"}, {"key": "est_vs_used", "type": "number", "label": "Estimated vs used quantity"}, {"key": "verifier_review", "type": "review", "label": "Verifier review"}]}', '{"required": ["shed_feed_video", "feed_lot", "quantity_fed"], "verify_capability": "proof.verify"}', '{}', '{}', NULL, NULL, NULL, NULL, '2026-07-16 07:30:25.635797+00', '2026-07-16 07:30:25.635797+00', 1),
	('b0000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 'b0000000-0000-4000-8000-000000000001', 1, 'Vaccination Session v1', 'published', '{"rules": [{"type": "block_submission_if", "when": {"field": "cold_chain_verified", "value": false, "operator": "equals"}, "message": "Cold chain must be verified before submitting the drive."}, {"type": "required_if", "when": {"field": "adverse_reaction", "value": true, "operator": "equals"}, "field": "adverse_reaction_notes", "message": "Adverse reaction notes are required when a reaction is observed."}, {"type": "proof_required_if", "when": {"field": "goat_ids", "operator": "not_empty"}, "field": "shed_video", "message": "Shed drive video proof is required."}, {"type": "proof_required_if", "when": {"field": "vaccine_lot_id", "operator": "not_empty"}, "field": "vial_lot_video", "message": "Vial and lot proof is required."}, {"type": "proof_required_if", "when": {"field": "goat_ids", "operator": "not_empty"}, "field": "administration_video", "message": "Administration proof is required."}, {"type": "required_if", "when": {"field": "extra_video_1", "operator": "not_empty"}, "field": "extra_video_1_caption", "message": "Caption is required when additional video 1 is attached."}, {"type": "required_if", "when": {"field": "extra_video_2", "operator": "not_empty"}, "field": "extra_video_2_caption", "message": "Caption is required when additional video 2 is attached."}], "title": "Vaccination Session", "fields": [{"key": "vaccine_lot_id", "type": "vaccine_batch_picker", "label": "Vaccine batch", "required": true, "description": "Select the vaccine batch/lot being used for this drive, sourced from FEFO inventory.", "option_source": "inventory.vaccine_lots.fefo"}, {"key": "cold_chain_verified", "type": "boolean", "label": "Cold chain verified", "required": true, "description": "Confirm the vaccine was kept within the cold chain temperature range up to the point of use."}, {"key": "shed_video", "type": "video_proof", "label": "Shed drive proof video", "required": true, "description": "Video of the shed/drive — which shed and group is being vaccinated.", "proof_subject": "shed"}, {"key": "vial_lot_video", "type": "video_proof", "label": "Vial and lot proof video", "required": true, "description": "Video of the vaccine vial + lot/batch number, for traceability.", "proof_subject": "vial_lot"}, {"key": "goat_ids", "type": "goat_scan", "label": "Goats vaccinated", "repeat": true, "required": true, "description": "Scan each goat''s RFID tag as it is vaccinated."}, {"key": "dose_ml_given", "type": "number", "label": "Dose given", "required": true, "description": "Dose administered, in millilitres, per the protocol."}, {"key": "route_site", "type": "select", "label": "Route / site", "required": true, "description": "Route and body site used for administration.", "option_source": "vaccination.route_sites"}, {"key": "administered_at", "type": "date_time", "label": "Administered at", "required": true, "description": "Date and time the dose was administered."}, {"key": "adverse_reaction", "type": "boolean", "label": "Adverse reaction observed", "required": true, "description": "Whether an adverse reaction was observed after administration."}, {"key": "adverse_reaction_notes", "type": "text", "label": "Adverse reaction notes", "required": false, "description": "Details of the observed adverse reaction, if any."}, {"key": "administration_video", "type": "video_proof", "label": "Administration proof video", "required": true, "description": "Video of the actual injection — proof the dose was given, not just logged.", "proof_subject": "administration"}, {"key": "extra_video_1", "type": "video_proof", "label": "Additional video 1 (optional)", "required": false, "description": "Optional extra video, e.g. a second shed or batch covered in this drive. Add a caption below if you attach this.", "proof_subject": "additional"}, {"key": "extra_video_1_caption", "type": "text", "label": "Additional video 1 caption", "required": false, "description": "Caption describing what additional video 1 shows."}, {"key": "extra_video_2", "type": "video_proof", "label": "Additional video 2 (optional)", "required": false, "description": "Optional second extra video, e.g. a third shed or batch covered in this drive. Add a caption below if you attach this.", "proof_subject": "additional"}, {"key": "extra_video_2_caption", "type": "text", "label": "Additional video 2 caption", "required": false, "description": "Caption describing what additional video 2 shows."}], "trigger": "protocol_window", "sop_code": "vaccination.drive", "workflow": {"edges": [{"to": "proof_verification", "from": "operator_submission", "condition": "proof_required"}, {"to": "accepted", "from": "proof_verification", "condition": "verifier_approves"}, {"to": "rework_requested", "from": "proof_verification", "condition": "verifier_rejects"}], "nodes": [{"key": "operator_submission", "type": "operator_execution", "label": "Operator submission"}, {"key": "proof_verification", "type": "proof_verification", "label": "Proof verification"}, {"key": "accepted", "type": "accepted", "label": "Accepted"}, {"key": "rework_requested", "type": "rework", "label": "Rework requested"}]}, "schema_version": "goatos.sop-form.v1", "repeat_for_each_goat": {"item_key": "goat_id", "source_field": "goat_ids"}}', '{"types": ["video"], "required": true, "maximum_count": 5, "minimum_count": 3, "subject_scope": "batch", "retention_policy": "operational_90d", "expected_subjects": ["shed", "vial_lot", "administration"], "verify_capability": "proof.verify", "ad_hoc_extra_videos": {"allowed": true, "max_count": 2, "field_keys": ["extra_video_1", "extra_video_2"], "requires_caption": true}, "verify_before_apply": true}', '{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "boolean", "select", "goat_scan", "rfid_scan", "vaccine_batch_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in", "not_in", "gt", "gte", "lt", "lte"]}', '{"valid": true, "errors": [], "warnings": [{"code": "structural_seed", "field": "form_dsl", "message": "Structural vaccination SOP seed; protocol schedule values are configured separately."}]}', NULL, NULL, '2026-07-16 07:30:25.667615+00', NULL, '2026-07-16 07:30:25.608363+00', '2026-07-16 07:30:26.753457+00', 3);

--
-- Data for Name: source_entry_decisions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: source_holding_stays; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: status_definitions; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.status_definitions VALUES
	('alive', 'lifecycle', 'Alive', 'Alive', 'Canonical active lifecycle state.', 10, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('dead', 'lifecycle', 'Dead', 'Dead', 'Deceased lifecycle state.', 90, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('sold', 'lifecycle', 'Sold', 'Sold', 'Exited through sale; future sales workflow owns behavior.', 100, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('merged', 'lifecycle', 'Merged', 'Merged', 'Redirected duplicate identity.', 110, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('inactive', 'lifecycle', 'Inactive', 'Inactive', 'Inactive identity state.', 120, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('K0', 'growth_cohort', 'K0 - Newborn', 'K0', 'Newborn kids with mother.', 10, true, 1, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('K1', 'growth_cohort', 'K1 - Bottle milk training', 'K1', 'Bottle milk training.', 20, true, 7, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('K2', 'growth_cohort', 'K2 - Milk drinking', 'K2', 'Milk drinking after training.', 30, true, 42, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('K3', 'growth_cohort', 'K3 - Weaning', 'K3', 'Weaning to solid feed.', 40, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('F2', 'growth_cohort', 'F2 - Fattening', 'F2', 'Post-weaning fattening stage.', 50, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('warmup', 'management', 'Warmup - Adaptation', 'Warmup', 'Source holding or park transition adaptation.', 10, true, 14, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('m0_post_delivery', 'management', 'M0 - Post-delivery mother', 'M0', 'Mother post-delivery management stage.', 20, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('pregnant', 'reproductive', 'Pregnant', 'Pregnant', NULL, 10, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('non_pregnant', 'reproductive', 'Non-pregnant', 'Non-pregnant', NULL, 20, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('mother', 'reproductive', 'Mother', 'Mother', NULL, 30, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('milking', 'reproductive', 'Milking', 'Milking', NULL, 40, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('buck', 'reproductive', 'Buck', 'Buck', 'Adult male breeding role.', 50, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('healthy', 'health', 'Healthy', 'Healthy', NULL, 10, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('icu', 'health', 'ICU', 'ICU', 'Serious illness health status; Phase 1 stores only.', 90, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('quarantine', 'health', 'Quarantine', 'Quarantine', 'Quarantine health status; Phase 1 stores only.', 95, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('under_treatment', 'health', 'Under treatment', 'Treatment', 'Treatment status; Phase 1 stores only.', 80, true, NULL, '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00'),
	('sick', 'health', 'Sick', 'Sick', 'Illness health status; vaccination generation can create visible deferred obligations when selected as a defer state.', 70, true, NULL, '2026-07-16 07:30:26.070397+00', '2026-07-16 07:30:26.070397+00'),
	('recovering', 'health', 'Recovering', 'Recovering', 'Recovery health status after illness/treatment; eligible for targeting but not a default defer state.', 75, true, NULL, '2026-07-16 07:30:26.070397+00', '2026-07-16 07:30:26.070397+00');

--
-- Data for Name: tenants; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.tenants VALUES
	('00000000-0000-4000-8000-000000000001', 'Mesha', 'active', '2026-07-16 07:30:24.352544+00', '2026-07-16 07:30:24.352544+00');

--
-- Data for Name: transit_handoffs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: user_scope_grants; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: vaccination_capacity_config; Type: TABLE DATA; Schema: public; Owner: -
--

INSERT INTO public.vaccination_capacity_config VALUES
	('00000000-0000-4000-8000-000000000001', 100, 'tenant', 7, 'split_within_safe_window_last_safe_may_exceed_cap', 2, '2026-07-16 07:30:26.590966+00', '2026-07-16 07:30:26.617672+00');

--
-- Data for Name: vaccination_completions; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: vaccination_eligibility_rollups; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: vaccination_generation_runs; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: vaccination_reminder_cadence_fires; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Data for Name: vaccination_source_facts; Type: TABLE DATA; Schema: public; Owner: -
--

--
-- Name: rollup_run_run_id_seq; Type: SEQUENCE SET; Schema: analytics; Owner: -
--

SELECT pg_catalog.setval('analytics.rollup_run_run_id_seq', 1, false);

--
-- Name: goat_display_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.goat_display_id_seq', 1, false);

--
-- Name: herd_register_summary_project_herd_register_summary_project_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.herd_register_summary_project_herd_register_summary_project_seq', 1, false);

--
-- Name: vaccination_eligibility_rollups_rollup_id_seq; Type: SEQUENCE SET; Schema: public; Owner: -
--

SELECT pg_catalog.setval('public.vaccination_eligibility_rollups_rollup_id_seq', 1, false);

--
-- PostgreSQL database dump complete
--

-- baseline: post-data indexes, constraints, triggers
--
-- PostgreSQL database dump
--

-- Dumped from database version 16.9
-- Dumped by pg_dump version 16.9

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

--
-- Name: crash_daily crash_daily_pkey; Type: CONSTRAINT; Schema: analytics; Owner: -
--

ALTER TABLE ONLY analytics.crash_daily
    ADD CONSTRAINT crash_daily_pkey PRIMARY KEY (tenant_id, event_date, app_version);

--
-- Name: engagement_daily engagement_daily_pkey; Type: CONSTRAINT; Schema: analytics; Owner: -
--

ALTER TABLE ONLY analytics.engagement_daily
    ADD CONSTRAINT engagement_daily_pkey PRIMARY KEY (tenant_id, event_date);

--
-- Name: funnel_daily funnel_daily_pkey; Type: CONSTRAINT; Schema: analytics; Owner: -
--

ALTER TABLE ONLY analytics.funnel_daily
    ADD CONSTRAINT funnel_daily_pkey PRIMARY KEY (tenant_id, event_date, funnel_key, step_key);

--
-- Name: journey_daily journey_daily_pkey; Type: CONSTRAINT; Schema: analytics; Owner: -
--

ALTER TABLE ONLY analytics.journey_daily
    ADD CONSTRAINT journey_daily_pkey PRIMARY KEY (tenant_id, event_date, journey_key);

--
-- Name: rollup_run rollup_run_pkey; Type: CONSTRAINT; Schema: analytics; Owner: -
--

ALTER TABLE ONLY analytics.rollup_run
    ADD CONSTRAINT rollup_run_pkey PRIMARY KEY (run_id);

--
-- Name: admin_ui_config_entries admin_ui_config_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_ui_config_entries
    ADD CONSTRAINT admin_ui_config_entries_pkey PRIMARY KEY (tenant_id, locale, route_id, config_key);

--
-- Name: admin_ui_config_family_change_queue admin_ui_config_family_change_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_ui_config_family_change_queue
    ADD CONSTRAINT admin_ui_config_family_change_queue_pkey PRIMARY KEY (transaction_id, tenant_id, family_key);

--
-- Name: admin_ui_config_family_revisions admin_ui_config_family_revisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_ui_config_family_revisions
    ADD CONSTRAINT admin_ui_config_family_revisions_pkey PRIMARY KEY (tenant_id, family_key);

--
-- Name: animal_stage_lookup animal_stage_lookup_code_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.animal_stage_lookup
    ADD CONSTRAINT animal_stage_lookup_code_unique UNIQUE (tenant_id, stage_code);

--
-- Name: animal_stage_lookup animal_stage_lookup_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.animal_stage_lookup
    ADD CONSTRAINT animal_stage_lookup_pkey PRIMARY KEY (animal_stage_id);

--
-- Name: animal_stage_lookup animal_stage_lookup_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.animal_stage_lookup
    ADD CONSTRAINT animal_stage_lookup_tenant_id_unique UNIQUE (tenant_id, animal_stage_id);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_item_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_item_unique UNIQUE (tenant_id, review_id, item_key);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_pkey PRIMARY KEY (review_goat_id);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_pkey PRIMARY KEY (review_id);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_tenant_id_unique UNIQUE (tenant_id, review_id);

--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (audit_id, recorded_at);

--
-- Name: auth_pending_email_grants auth_pending_email_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_pending_email_grants
    ADD CONSTRAINT auth_pending_email_grants_pkey PRIMARY KEY (pending_grant_id);

--
-- Name: breed_aliases breed_aliases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.breed_aliases
    ADD CONSTRAINT breed_aliases_pkey PRIMARY KEY (alias_id);

--
-- Name: breed_aliases breed_aliases_unique_alias; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.breed_aliases
    ADD CONSTRAINT breed_aliases_unique_alias UNIQUE (normalized_alias, source_system);

--
-- Name: breeds breeds_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.breeds
    ADD CONSTRAINT breeds_pkey PRIMARY KEY (breed_id);

--
-- Name: breeds breeds_unique_name; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.breeds
    ADD CONSTRAINT breeds_unique_name UNIQUE (species, canonical_name);

--
-- Name: bulk_status_job bulk_status_job_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bulk_status_job
    ADD CONSTRAINT bulk_status_job_pkey PRIMARY KEY (bulk_status_job_id);

--
-- Name: bulk_status_job_row bulk_status_job_row_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bulk_status_job_row
    ADD CONSTRAINT bulk_status_job_row_pkey PRIMARY KEY (bulk_status_job_row_id);

--
-- Name: bulk_status_job_row bulk_status_job_row_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bulk_status_job_row
    ADD CONSTRAINT bulk_status_job_row_unique UNIQUE (job_id, goat_id, axis);

--
-- Name: calendar_reconciler_progress calendar_reconciler_progress_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calendar_reconciler_progress
    ADD CONSTRAINT calendar_reconciler_progress_pkey PRIMARY KEY (tenant_id);

--
-- Name: calendar_snoozes calendar_snoozes_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calendar_snoozes
    ADD CONSTRAINT calendar_snoozes_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: calendar_snoozes calendar_snoozes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calendar_snoozes
    ADD CONSTRAINT calendar_snoozes_pkey PRIMARY KEY (snooze_id);

--
-- Name: count_base_anchors count_base_anchors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_pkey PRIMARY KEY (base_count_anchor_id);

--
-- Name: count_dimension_aliases count_dimension_aliases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_dimension_aliases
    ADD CONSTRAINT count_dimension_aliases_pkey PRIMARY KEY (alias_id);

--
-- Name: count_mismatch_scan_runs count_mismatch_scan_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_mismatch_scan_runs
    ADD CONSTRAINT count_mismatch_scan_runs_pkey PRIMARY KEY (count_mismatch_scan_run_id);

--
-- Name: count_projection_exception_resolutions count_projection_exception_resolutions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exception_resolutions
    ADD CONSTRAINT count_projection_exception_resolutions_pkey PRIMARY KEY (count_projection_exception_resolution_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_pkey PRIMARY KEY (count_projection_exception_id);

--
-- Name: count_projection_recompute_runs count_projection_recompute_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_recompute_runs
    ADD CONSTRAINT count_projection_recompute_runs_pkey PRIMARY KEY (count_projection_recompute_run_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_pkey PRIMARY KEY (count_projection_snapshot_row_id);

--
-- Name: count_projection_snapshots count_projection_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshots
    ADD CONSTRAINT count_projection_snapshots_pkey PRIMARY KEY (count_projection_snapshot_id);

--
-- Name: count_source_import_runs count_source_import_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_source_import_runs
    ADD CONSTRAINT count_source_import_runs_pkey PRIMARY KEY (count_source_import_run_id);

--
-- Name: counts_shifting_readiness_evidence counts_shifting_readiness_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.counts_shifting_readiness_evidence
    ADD CONSTRAINT counts_shifting_readiness_evidence_pkey PRIMARY KEY (counts_shifting_readiness_evidence_id);

--
-- Name: counts_shifting_readiness_subgates counts_shifting_readiness_subgates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.counts_shifting_readiness_subgates
    ADD CONSTRAINT counts_shifting_readiness_subgates_pkey PRIMARY KEY (tenant_id, subgate_id);

--
-- Name: departments departments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_pkey PRIMARY KEY (department_id);

--
-- Name: departments departments_tenant_department_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_tenant_department_key UNIQUE (tenant_id, department_id);

--
-- Name: domain_event_processed_events domain_event_processed_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domain_event_processed_events
    ADD CONSTRAINT domain_event_processed_events_pkey PRIMARY KEY (tenant_id, subscription_id, event_id);

--
-- Name: farm_profiles farm_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.farm_profiles
    ADD CONSTRAINT farm_profiles_pkey PRIMARY KEY (location_id);

--
-- Name: feed_direction_completions feed_direction_completions_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: feed_direction_completions feed_direction_completions_obligation_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_obligation_unique UNIQUE (tenant_id, obligation_id);

--
-- Name: feed_direction_completions feed_direction_completions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_pkey PRIMARY KEY (completion_id);

--
-- Name: goat_custody_history goat_custody_history_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_pkey PRIMARY KEY (custody_history_id);

--
-- Name: goat_identifiers goat_identifiers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identifiers
    ADD CONSTRAINT goat_identifiers_pkey PRIMARY KEY (identifier_id);

--
-- Name: goat_identifiers goat_identifiers_tenant_identifier_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identifiers
    ADD CONSTRAINT goat_identifiers_tenant_identifier_unique UNIQUE (tenant_id, identifier_id);

--
-- Name: goat_identity_events goat_identity_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_pkey PRIMARY KEY (identity_event_id, recorded_at);

--
-- Name: goat_identity_events goat_identity_events_tenant_event_recorded_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_tenant_event_recorded_unique UNIQUE (tenant_id, identity_event_id, recorded_at);

--
-- Name: goat_location_history goat_location_history_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_pkey PRIMARY KEY (location_history_id);

--
-- Name: goat_merge_links goat_merge_links_merged_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_merged_unique UNIQUE (merged_goat_id);

--
-- Name: goat_merge_links goat_merge_links_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_pkey PRIMARY KEY (merge_link_id);

--
-- Name: goat_ownership goat_ownership_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_pkey PRIMARY KEY (ownership_id);

--
-- Name: goats goats_display_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_display_id_unique UNIQUE (display_id);

--
-- Name: goats goats_health_status_check; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.goats
    ADD CONSTRAINT goats_health_status_check CHECK (((health_status IS NULL) OR (health_status = ANY (ARRAY['healthy'::text, 'sick'::text, 'under_treatment'::text, 'recovering'::text, 'quarantine'::text, 'icu'::text])))) NOT VALID;

--
-- Name: goats goats_lifecycle_status_check; Type: CHECK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.goats
    ADD CONSTRAINT goats_lifecycle_status_check CHECK ((lifecycle_status = ANY (ARRAY['alive'::text, 'sick'::text, 'under_treatment'::text, 'quarantine'::text, 'icu'::text, 'dead'::text, 'sold'::text, 'culled'::text, 'transferred'::text, 'lost'::text, 'merged'::text, 'inactive'::text]))) NOT VALID;

--
-- Name: goats goats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_pkey PRIMARY KEY (goat_id);

--
-- Name: goats goats_tenant_goat_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_tenant_goat_unique UNIQUE (tenant_id, goat_id);

--
-- Name: herd_register_goat_projection herd_register_goat_projection_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.herd_register_goat_projection
    ADD CONSTRAINT herd_register_goat_projection_pkey PRIMARY KEY (tenant_id, goat_id);

--
-- Name: herd_register_summary_projection herd_register_summary_projection_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.herd_register_summary_projection
    ADD CONSTRAINT herd_register_summary_projection_pkey PRIMARY KEY (herd_register_summary_projection_id);

--
-- Name: herd_register_summary_projection herd_register_summary_projection_scope_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.herd_register_summary_projection
    ADD CONSTRAINT herd_register_summary_projection_scope_key UNIQUE NULLS NOT DISTINCT (tenant_id, park_id, farm_id, current_location_id, breed, sex, lifecycle_status);

--
-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (idempotency_key);

--
-- Name: identifier_policies identifier_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identifier_policies
    ADD CONSTRAINT identifier_policies_pkey PRIMARY KEY (policy_version, identifier_type);

--
-- Name: identifier_policy_versions identifier_policy_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identifier_policy_versions
    ADD CONSTRAINT identifier_policy_versions_pkey PRIMARY KEY (policy_version);

--
-- Name: identity_conflict_goats identity_conflict_goats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_goats
    ADD CONSTRAINT identity_conflict_goats_pkey PRIMARY KEY (conflict_id, goat_id);

--
-- Name: identity_conflict_source_records identity_conflict_source_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_source_records
    ADD CONSTRAINT identity_conflict_source_records_pkey PRIMARY KEY (conflict_id, source_system, source_record_id);

--
-- Name: identity_conflicts identity_conflicts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflicts
    ADD CONSTRAINT identity_conflicts_pkey PRIMARY KEY (conflict_id);

--
-- Name: identity_conflicts identity_conflicts_tenant_conflict_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflicts
    ADD CONSTRAINT identity_conflicts_tenant_conflict_unique UNIQUE (tenant_id, conflict_id);

--
-- Name: identity_correction_requests identity_correction_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_pkey PRIMARY KEY (correction_request_id);

--
-- Name: identity_decision_events identity_decision_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_events
    ADD CONSTRAINT identity_decision_events_pkey PRIMARY KEY (decision_id, event_id, event_recorded_at);

--
-- Name: identity_decision_goats identity_decision_goats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_goats
    ADD CONSTRAINT identity_decision_goats_pkey PRIMARY KEY (decision_id, goat_id, role);

--
-- Name: identity_decision_identifiers identity_decision_identifiers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_identifiers
    ADD CONSTRAINT identity_decision_identifiers_pkey PRIMARY KEY (decision_identifier_id);

--
-- Name: identity_decision_media identity_decision_media_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_media
    ADD CONSTRAINT identity_decision_media_pkey PRIMARY KEY (decision_id, media_id);

--
-- Name: identity_decisions identity_decisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decisions
    ADD CONSTRAINT identity_decisions_pkey PRIMARY KEY (decision_id);

--
-- Name: identity_decisions identity_decisions_tenant_decision_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decisions
    ADD CONSTRAINT identity_decisions_tenant_decision_unique UNIQUE (tenant_id, decision_id);

--
-- Name: inventory_items inventory_items_code_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_items
    ADD CONSTRAINT inventory_items_code_unique UNIQUE (tenant_id, item_code);

--
-- Name: inventory_items inventory_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_items
    ADD CONSTRAINT inventory_items_pkey PRIMARY KEY (item_id);

--
-- Name: inventory_items inventory_items_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_items
    ADD CONSTRAINT inventory_items_tenant_id_unique UNIQUE (tenant_id, item_id);

--
-- Name: inventory_stock inventory_stock_lot_identity_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock
    ADD CONSTRAINT inventory_stock_lot_identity_unique UNIQUE (tenant_id, stock_id, item_id, location_id);

--
-- Name: inventory_stock_movements inventory_stock_movements_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: inventory_stock_movements inventory_stock_movements_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_pkey PRIMARY KEY (movement_id);

--
-- Name: inventory_stock inventory_stock_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock
    ADD CONSTRAINT inventory_stock_pkey PRIMARY KEY (stock_id);

--
-- Name: inventory_stock inventory_stock_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock
    ADD CONSTRAINT inventory_stock_tenant_id_unique UNIQUE (tenant_id, stock_id);

--
-- Name: location_aliases location_aliases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_aliases
    ADD CONSTRAINT location_aliases_pkey PRIMARY KEY (alias_id);

--
-- Name: location_capacity_records location_capacity_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_capacity_records
    ADD CONSTRAINT location_capacity_records_pkey PRIMARY KEY (capacity_record_id);

--
-- Name: location_operational_attributes location_operational_attributes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_operational_attributes
    ADD CONSTRAINT location_operational_attributes_pkey PRIMARY KEY (location_id);

--
-- Name: location_projection_invalidations location_projection_invalidations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_projection_invalidations
    ADD CONSTRAINT location_projection_invalidations_pkey PRIMARY KEY (invalidation_id);

--
-- Name: location_review_items location_review_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_review_items
    ADD CONSTRAINT location_review_items_pkey PRIMARY KEY (review_id);

--
-- Name: locations locations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.locations
    ADD CONSTRAINT locations_pkey PRIMARY KEY (location_id);

--
-- Name: locations locations_tenant_location_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.locations
    ADD CONSTRAINT locations_tenant_location_unique UNIQUE (tenant_id, location_id);

--
-- Name: locations locations_unique_code; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.locations
    ADD CONSTRAINT locations_unique_code UNIQUE (tenant_id, location_code);

--
-- Name: movement_commands movement_commands_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.movement_commands
    ADD CONSTRAINT movement_commands_pkey PRIMARY KEY (command_id);

--
-- Name: notification_delivery_attempts notification_delivery_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_delivery_attempts
    ADD CONSTRAINT notification_delivery_attempts_pkey PRIMARY KEY (attempt_id);

--
-- Name: notification_delivery_attempts notification_delivery_attempts_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_delivery_attempts
    ADD CONSTRAINT notification_delivery_attempts_unique UNIQUE (tenant_id, notification_request_id, attempt_no);

--
-- Name: notification_requests notification_requests_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_requests
    ADD CONSTRAINT notification_requests_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: notification_requests notification_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_requests
    ADD CONSTRAINT notification_requests_pkey PRIMARY KEY (notification_request_id);

--
-- Name: obligation_batches obligation_batches_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_batches
    ADD CONSTRAINT obligation_batches_pkey PRIMARY KEY (batch_id);

--
-- Name: obligation_batches obligation_batches_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_batches
    ADD CONSTRAINT obligation_batches_tenant_id_unique UNIQUE (tenant_id, batch_id);

--
-- Name: obligation_escalations obligation_escalations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_escalations
    ADD CONSTRAINT obligation_escalations_pkey PRIMARY KEY (escalation_id);

--
-- Name: obligation_goat_shift_watermarks obligation_goat_shift_watermarks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_goat_shift_watermarks
    ADD CONSTRAINT obligation_goat_shift_watermarks_pkey PRIMARY KEY (tenant_id, goat_id);

--
-- Name: obligation_instances obligation_instances_dup_guard; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_dup_guard UNIQUE NULLS NOT DISTINCT (tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at);

--
-- Name: obligation_instances obligation_instances_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: obligation_instances obligation_instances_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_pkey PRIMARY KEY (obligation_id);

--
-- Name: obligation_instances obligation_instances_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_tenant_id_unique UNIQUE (tenant_id, obligation_id);

--
-- Name: obligation_status_events obligation_status_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_status_events
    ADD CONSTRAINT obligation_status_events_pkey PRIMARY KEY (obligation_event_id, recorded_at);

--
-- Name: org_role_catalog org_role_catalog_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_role_catalog
    ADD CONSTRAINT org_role_catalog_pkey PRIMARY KEY (role_key);

--
-- Name: org_tiers org_tiers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_tiers
    ADD CONSTRAINT org_tiers_pkey PRIMARY KEY (tier_code);

--
-- Name: org_tiers org_tiers_rank_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_tiers
    ADD CONSTRAINT org_tiers_rank_unique UNIQUE (rank);

--
-- Name: org_verticals org_verticals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_verticals
    ADD CONSTRAINT org_verticals_pkey PRIMARY KEY (vertical_code);

--
-- Name: org_verticals org_verticals_sort_order_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_verticals
    ADD CONSTRAINT org_verticals_sort_order_unique UNIQUE (sort_order);

--
-- Name: orgs orgs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orgs
    ADD CONSTRAINT orgs_pkey PRIMARY KEY (party_id);

--
-- Name: outbox_dlq_actions outbox_dlq_actions_key_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_dlq_actions
    ADD CONSTRAINT outbox_dlq_actions_key_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: outbox_dlq_actions outbox_dlq_actions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_dlq_actions
    ADD CONSTRAINT outbox_dlq_actions_pkey PRIMARY KEY (action_id);

--
-- Name: outbox_messages outbox_messages_event_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_messages
    ADD CONSTRAINT outbox_messages_event_unique UNIQUE (event_id);

--
-- Name: outbox_messages outbox_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_messages
    ADD CONSTRAINT outbox_messages_pkey PRIMARY KEY (outbox_id);

--
-- Name: park_profiles park_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.park_profiles
    ADD CONSTRAINT park_profiles_pkey PRIMARY KEY (location_id);

--
-- Name: parties parties_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.parties
    ADD CONSTRAINT parties_pkey PRIMARY KEY (party_id);

--
-- Name: position_module_duties position_module_duties_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.position_module_duties
    ADD CONSTRAINT position_module_duties_pkey PRIMARY KEY (id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_pkey PRIMARY KEY (evidence_id);

--
-- Name: procurement_load_goats procurement_load_goats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_load_goats
    ADD CONSTRAINT procurement_load_goats_pkey PRIMARY KEY (load_goat_id);

--
-- Name: procurement_load_goats procurement_load_goats_unique_goat_per_load; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_load_goats
    ADD CONSTRAINT procurement_load_goats_unique_goat_per_load UNIQUE (tenant_id, load_id, goat_id);

--
-- Name: procurement_loads procurement_loads_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: procurement_loads procurement_loads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_pkey PRIMARY KEY (load_id);

--
-- Name: procurement_loads procurement_loads_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_tenant_id_unique UNIQUE (tenant_id, load_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_one_per_goat; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_one_per_goat UNIQUE (tenant_id, load_id, goat_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_pkey PRIMARY KEY (handoff_id);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_pkey PRIMARY KEY (health_check_id);

--
-- Name: proof_artifacts proof_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proof_artifacts
    ADD CONSTRAINT proof_artifacts_pkey PRIMARY KEY (proof_id);

--
-- Name: protocol_definitions protocol_definitions_code_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_definitions
    ADD CONSTRAINT protocol_definitions_code_unique UNIQUE (tenant_id, code);

--
-- Name: protocol_definitions protocol_definitions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_definitions
    ADD CONSTRAINT protocol_definitions_pkey PRIMARY KEY (protocol_id);

--
-- Name: protocol_definitions protocol_definitions_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_definitions
    ADD CONSTRAINT protocol_definitions_tenant_id_unique UNIQUE (tenant_id, protocol_id);

--
-- Name: protocol_rule_dimensions protocol_rule_dimensions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rule_dimensions
    ADD CONSTRAINT protocol_rule_dimensions_pkey PRIMARY KEY (protocol_rule_dimension_id);

--
-- Name: protocol_rule_dimensions protocol_rule_dimensions_tenant_id_protocol_version_id_rule_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rule_dimensions
    ADD CONSTRAINT protocol_rule_dimensions_tenant_id_protocol_version_id_rule_key UNIQUE (tenant_id, protocol_version_id, rule_id, selector_key);

--
-- Name: protocol_rules protocol_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rules
    ADD CONSTRAINT protocol_rules_pkey PRIMARY KEY (rule_id);

--
-- Name: protocol_rules protocol_rules_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rules
    ADD CONSTRAINT protocol_rules_tenant_id_unique UNIQUE (tenant_id, rule_id);

--
-- Name: protocol_rules protocol_rules_version_dose_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rules
    ADD CONSTRAINT protocol_rules_version_dose_unique UNIQUE (tenant_id, protocol_version_id, dose_code);

--
-- Name: protocol_triggers protocol_triggers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_triggers
    ADD CONSTRAINT protocol_triggers_pkey PRIMARY KEY (trigger_id);

--
-- Name: protocol_triggers protocol_triggers_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_triggers
    ADD CONSTRAINT protocol_triggers_tenant_id_unique UNIQUE (tenant_id, trigger_id);

--
-- Name: protocol_versions protocol_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_pkey PRIMARY KEY (protocol_version_id);

--
-- Name: protocol_versions protocol_versions_published_no_overlap; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_published_no_overlap EXCLUDE USING gist (tenant_id WITH =, protocol_id WITH =, scope_type WITH =, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid) WITH =, daterange(effective_from, effective_to, '[)'::text) WITH &&) WHERE ((status = 'published'::text));

--
-- Name: protocol_versions protocol_versions_scope_version_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_scope_version_unique UNIQUE NULLS NOT DISTINCT (tenant_id, protocol_id, scope_type, scope_id, version);

--
-- Name: protocol_versions protocol_versions_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_tenant_id_unique UNIQUE (tenant_id, protocol_version_id);

--
-- Name: reminder_cadence_progress reminder_cadence_progress_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reminder_cadence_progress
    ADD CONSTRAINT reminder_cadence_progress_pkey PRIMARY KEY (tenant_id);

--
-- Name: seed_runs seed_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.seed_runs
    ADD CONSTRAINT seed_runs_pkey PRIMARY KEY (seed_run_id);

--
-- Name: shed_lifecycle_status_lookup shed_lifecycle_status_lookup_code_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_lifecycle_status_lookup
    ADD CONSTRAINT shed_lifecycle_status_lookup_code_unique UNIQUE (tenant_id, status_code);

--
-- Name: shed_lifecycle_status_lookup shed_lifecycle_status_lookup_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_lifecycle_status_lookup
    ADD CONSTRAINT shed_lifecycle_status_lookup_pkey PRIMARY KEY (shed_lifecycle_status_id);

--
-- Name: shed_lifecycle_status_lookup shed_lifecycle_status_lookup_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_lifecycle_status_lookup
    ADD CONSTRAINT shed_lifecycle_status_lookup_tenant_id_unique UNIQUE (tenant_id, shed_lifecycle_status_id);

--
-- Name: shed_profiles shed_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_profiles
    ADD CONSTRAINT shed_profiles_pkey PRIMARY KEY (location_id);

--
-- Name: shifting_event_impacts shifting_event_impacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_event_impacts
    ADD CONSTRAINT shifting_event_impacts_pkey PRIMARY KEY (shifting_event_impact_id);

--
-- Name: shifting_events shifting_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_pkey PRIMARY KEY (shifting_event_id);

--
-- Name: sop_definitions sop_definitions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_definitions
    ADD CONSTRAINT sop_definitions_pkey PRIMARY KEY (sop_id);

--
-- Name: sop_submission_items sop_submission_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submission_items
    ADD CONSTRAINT sop_submission_items_pkey PRIMARY KEY (item_id);

--
-- Name: sop_submission_items sop_submission_items_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submission_items
    ADD CONSTRAINT sop_submission_items_tenant_id_unique UNIQUE (tenant_id, item_id);

--
-- Name: sop_submissions sop_submissions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submissions
    ADD CONSTRAINT sop_submissions_pkey PRIMARY KEY (submission_id);

--
-- Name: sop_task_scan_captures sop_task_scan_captures_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_captures
    ADD CONSTRAINT sop_task_scan_captures_pkey PRIMARY KEY (capture_id);

--
-- Name: sop_task_scan_attempts sop_task_scan_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_attempts
    ADD CONSTRAINT sop_task_scan_attempts_pkey PRIMARY KEY (attempt_id);

--
-- Name: sop_task_review_fanouts sop_task_review_fanouts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_review_fanouts
    ADD CONSTRAINT sop_task_review_fanouts_pkey PRIMARY KEY (review_fanout_id);

--
-- Name: sop_task_review_fanouts sop_task_review_fanouts_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_review_fanouts
    ADD CONSTRAINT sop_task_review_fanouts_unique UNIQUE (tenant_id, task_id, task_row_version, outcome);

--
-- Name: sop_task_submission_fanouts sop_task_submission_fanouts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_submission_fanouts
    ADD CONSTRAINT sop_task_submission_fanouts_pkey PRIMARY KEY (submission_fanout_id);

--
-- Name: sop_task_submission_fanouts sop_task_submission_fanouts_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_submission_fanouts
    ADD CONSTRAINT sop_task_submission_fanouts_unique UNIQUE (tenant_id, submission_id);

--
-- Name: sop_tasks sop_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_tasks
    ADD CONSTRAINT sop_tasks_pkey PRIMARY KEY (task_id);

--
-- Name: sop_tasks sop_tasks_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_tasks
    ADD CONSTRAINT sop_tasks_tenant_id_unique UNIQUE (tenant_id, task_id);

--
-- Name: sop_versions sop_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_versions
    ADD CONSTRAINT sop_versions_pkey PRIMARY KEY (sop_version_id);

--
-- Name: sop_versions sop_versions_tenant_id_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_versions
    ADD CONSTRAINT sop_versions_tenant_id_unique UNIQUE (tenant_id, sop_version_id);

--
-- Name: source_entry_decisions source_entry_decisions_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: source_entry_decisions source_entry_decisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_pkey PRIMARY KEY (decision_id);

--
-- Name: source_holding_stays source_holding_stays_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_holding_stays
    ADD CONSTRAINT source_holding_stays_pkey PRIMARY KEY (stay_id);

--
-- Name: source_holding_stays source_holding_stays_unique_window; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_holding_stays
    ADD CONSTRAINT source_holding_stays_unique_window UNIQUE (tenant_id, load_id, goat_id, holding_location_id, started_at);

--
-- Name: status_definitions status_definitions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.status_definitions
    ADD CONSTRAINT status_definitions_pkey PRIMARY KEY (status_code);

--
-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (tenant_id);

--
-- Name: transit_handoffs transit_handoffs_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: transit_handoffs transit_handoffs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_pkey PRIMARY KEY (handoff_id);

--
-- Name: user_scope_grants user_scope_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_scope_grants
    ADD CONSTRAINT user_scope_grants_pkey PRIMARY KEY (grant_id);

--
-- Name: vaccination_capacity_config vaccination_capacity_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_capacity_config
    ADD CONSTRAINT vaccination_capacity_config_pkey PRIMARY KEY (tenant_id);

--
-- Name: vaccination_completions vaccination_completions_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: vaccination_completions vaccination_completions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_pkey PRIMARY KEY (completion_id);

--
-- Name: vaccination_eligibility_rollups vaccination_eligibility_rollups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_eligibility_rollups
    ADD CONSTRAINT vaccination_eligibility_rollups_pkey PRIMARY KEY (rollup_id);

--
-- Name: vaccination_generation_runs vaccination_generation_runs_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_generation_runs
    ADD CONSTRAINT vaccination_generation_runs_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: vaccination_generation_runs vaccination_generation_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_generation_runs
    ADD CONSTRAINT vaccination_generation_runs_pkey PRIMARY KEY (run_id);

--
-- Name: vaccination_reminder_cadence_fires vaccination_reminder_cadence_fires_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_reminder_cadence_fires
    ADD CONSTRAINT vaccination_reminder_cadence_fires_pkey PRIMARY KEY (tenant_id, fire_key);

--
-- Name: vaccination_source_facts vaccination_source_facts_lineage_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_source_facts
    ADD CONSTRAINT vaccination_source_facts_lineage_unique UNIQUE (tenant_id, lineage_key);

--
-- Name: vaccination_source_facts vaccination_source_facts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_source_facts
    ADD CONSTRAINT vaccination_source_facts_pkey PRIMARY KEY (source_fact_id);

--
-- Name: vaccines vaccines_item_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccines
    ADD CONSTRAINT vaccines_item_unique UNIQUE (tenant_id, item_id);

--
-- Name: vaccines vaccines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccines
    ADD CONSTRAINT vaccines_pkey PRIMARY KEY (vaccine_id);

--
-- Name: verification_items verification_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.verification_items
    ADD CONSTRAINT verification_items_pkey PRIMARY KEY (item_id);

--
-- Name: verification_items verification_items_tenant_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.verification_items
    ADD CONSTRAINT verification_items_tenant_idempotency_unique UNIQUE (tenant_id, idempotency_key);

--
-- Name: workforce_absences workforce_absences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_absences
    ADD CONSTRAINT workforce_absences_pkey PRIMARY KEY (absence_id);

--
-- Name: workforce_capabilities workforce_capabilities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_capabilities
    ADD CONSTRAINT workforce_capabilities_pkey PRIMARY KEY (capability_id);

--
-- Name: workforce_external_identities workforce_external_identities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_external_identities
    ADD CONSTRAINT workforce_external_identities_pkey PRIMARY KEY (external_identity_id);

--
-- Name: workforce_member_app_sessions workforce_member_app_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_app_sessions
    ADD CONSTRAINT workforce_member_app_sessions_pkey PRIMARY KEY (session_id);

--
-- Name: workforce_member_capabilities workforce_member_capabilities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_capabilities
    ADD CONSTRAINT workforce_member_capabilities_pkey PRIMARY KEY (member_capability_id);

--
-- Name: workforce_member_devices workforce_member_devices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_devices
    ADD CONSTRAINT workforce_member_devices_pkey PRIMARY KEY (device_id);

--
-- Name: workforce_members workforce_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_members
    ADD CONSTRAINT workforce_members_pkey PRIMARY KEY (workforce_member_id);

--
-- Name: workforce_positions workforce_positions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_positions
    ADD CONSTRAINT workforce_positions_pkey PRIMARY KEY (position_id);

--
-- Name: workforce_roster_assignments workforce_roster_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_roster_assignments
    ADD CONSTRAINT workforce_roster_assignments_pkey PRIMARY KEY (roster_assignment_id);

--
-- Name: crash_daily_tenant_event_date_idx; Type: INDEX; Schema: analytics; Owner: -
--

CREATE INDEX crash_daily_tenant_event_date_idx ON analytics.crash_daily USING btree (tenant_id, event_date);

--
-- Name: engagement_daily_tenant_event_date_idx; Type: INDEX; Schema: analytics; Owner: -
--

CREATE INDEX engagement_daily_tenant_event_date_idx ON analytics.engagement_daily USING btree (tenant_id, event_date);

--
-- Name: funnel_daily_tenant_event_date_idx; Type: INDEX; Schema: analytics; Owner: -
--

CREATE INDEX funnel_daily_tenant_event_date_idx ON analytics.funnel_daily USING btree (tenant_id, event_date);

--
-- Name: journey_daily_tenant_event_date_idx; Type: INDEX; Schema: analytics; Owner: -
--

CREATE INDEX journey_daily_tenant_event_date_idx ON analytics.journey_daily USING btree (tenant_id, event_date);

--
-- Name: rollup_run_source_date_idx; Type: INDEX; Schema: analytics; Owner: -
--

CREATE INDEX rollup_run_source_date_idx ON analytics.rollup_run USING btree (source_date, started_at DESC);

--
-- Name: admin_ui_config_entries_route_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX admin_ui_config_entries_route_idx ON public.admin_ui_config_entries USING btree (tenant_id, locale, route_id, status, updated_at DESC);

--
-- Name: admin_ui_config_family_changed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX admin_ui_config_family_changed_idx ON public.admin_ui_config_family_revisions USING btree (tenant_id, changed_at DESC, family_key);

--
-- Name: arrival_intake_review_goats_load_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX arrival_intake_review_goats_load_idx ON public.arrival_intake_review_goats USING btree (tenant_id, load_id, arrival_state, goat_id);

--
-- Name: arrival_intake_review_goats_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX arrival_intake_review_goats_review_idx ON public.arrival_intake_review_goats USING btree (tenant_id, review_id, arrival_state, review_goat_id);

--
-- Name: arrival_intake_reviews_load_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX arrival_intake_reviews_load_idx ON public.arrival_intake_reviews USING btree (tenant_id, load_id, status, reviewed_at DESC);

--
-- Name: audit_log_actor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_actor_idx ON public.audit_log USING btree (actor_id, created_at DESC);

--
-- Name: audit_log_resource_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_resource_idx ON public.audit_log USING btree (resource_type, resource_id, created_at DESC);

--
-- Name: audit_log_tenant_action_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_action_idx ON public.audit_log USING btree (tenant_id, action, recorded_at DESC);

--
-- Name: audit_log_tenant_actor_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_actor_recorded_idx ON public.audit_log USING btree (tenant_id, actor_id, recorded_at DESC);

--
-- Name: audit_log_tenant_actor_type_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_actor_type_recorded_idx ON public.audit_log USING btree (tenant_id, actor_type, recorded_at DESC, audit_id DESC);

--
-- Name: audit_log_tenant_calendar_event_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_calendar_event_recorded_idx ON public.audit_log USING btree (tenant_id, ((metadata ->> 'calendar_event_id'::text)), recorded_at DESC, audit_id DESC) WHERE (metadata ? 'calendar_event_id'::text);

--
-- Name: audit_log_tenant_domain_module_category_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_domain_module_category_recorded_idx ON public.audit_log USING btree (tenant_id, ((metadata ->> 'domain'::text)), ((metadata ->> 'module'::text)), ((metadata ->> 'category'::text)), recorded_at DESC, audit_id DESC);

--
-- Name: audit_log_tenant_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_recorded_idx ON public.audit_log USING btree (tenant_id, recorded_at DESC, audit_id DESC);

--
-- Name: audit_log_tenant_resource_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_resource_recorded_idx ON public.audit_log USING btree (tenant_id, resource_type, resource_id, recorded_at DESC);

--
-- Name: audit_log_tenant_result_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_result_recorded_idx ON public.audit_log USING btree (tenant_id, ((metadata ->> 'result'::text)), recorded_at DESC, audit_id DESC);

--
-- Name: audit_log_tenant_scope_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_scope_recorded_idx ON public.audit_log USING btree (tenant_id, scope_type, scope_id, recorded_at DESC);

--
-- Name: audit_log_tenant_status_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX audit_log_tenant_status_recorded_idx ON public.audit_log USING btree (tenant_id, ((metadata ->> 'status'::text)), recorded_at DESC, audit_id DESC);

--
-- Name: auth_pending_email_grants_active_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX auth_pending_email_grants_active_unique_idx ON public.auth_pending_email_grants USING btree (tenant_id, normalized_email, role, scope_type, scope_id) WHERE ((status = 'active'::text) AND (valid_to IS NULL));

--
-- Name: auth_pending_email_grants_lookup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX auth_pending_email_grants_lookup_idx ON public.auth_pending_email_grants USING btree (normalized_email, tenant_id, status, valid_from, valid_to);

--
-- Name: bulk_status_job_row_claim_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX bulk_status_job_row_claim_idx ON public.bulk_status_job_row USING btree (tenant_id, job_id) WHERE (row_state = ANY (ARRAY['pending'::text, 'retry'::text, 'claimed'::text]));

--
-- Name: bulk_status_job_row_job_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX bulk_status_job_row_job_idx ON public.bulk_status_job_row USING btree (job_id);

--
-- Name: bulk_status_job_tenant_idem_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX bulk_status_job_tenant_idem_idx ON public.bulk_status_job USING btree (tenant_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);

--
-- Name: calendar_snoozes_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX calendar_snoozes_active_idx ON public.calendar_snoozes USING btree (tenant_id, calendar_event_id, snooze_until) WHERE (status = 'active'::text);

--
-- Name: calendar_snoozes_event_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX calendar_snoozes_event_idx ON public.calendar_snoozes USING btree (tenant_id, calendar_event_id, created_at DESC, snooze_id DESC);

--
-- Name: count_base_anchors_hot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_base_anchors_hot_idx ON public.count_base_anchors USING btree (tenant_id, park_id, shed_id, lower(breed_key), counted_at DESC, base_count_anchor_id DESC) WHERE (anchor_state = 'adopted'::text);

--
-- Name: count_base_anchors_idempotency_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_base_anchors_idempotency_unique ON public.count_base_anchors USING btree (tenant_id, idempotency_key);

--
-- Name: count_base_anchors_mismatch_scan_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_base_anchors_mismatch_scan_idx ON public.count_base_anchors USING btree (tenant_id, counted_at, base_count_anchor_id) WHERE ((anchor_state = 'adopted'::text) AND (discrepancy_state <> 'resolved'::text));

--
-- Name: count_base_anchors_mismatch_scan_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_base_anchors_mismatch_scan_scope_idx ON public.count_base_anchors USING btree (tenant_id, park_id, shed_id, counted_at, base_count_anchor_id) WHERE ((anchor_state = 'adopted'::text) AND (discrepancy_state <> 'resolved'::text));

--
-- Name: count_base_anchors_source_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_base_anchors_source_unique ON public.count_base_anchors USING btree (tenant_id, park_id, shed_id, lower(breed_key), counted_at, source_hash);

--
-- Name: count_base_anchors_tenant_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_base_anchors_tenant_id_unique ON public.count_base_anchors USING btree (tenant_id, base_count_anchor_id);

--
-- Name: count_dimension_aliases_current_approved_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_dimension_aliases_current_approved_unique ON public.count_dimension_aliases USING btree (tenant_id, dimension, source_system, source_value_norm) WHERE ((review_status = 'approved'::text) AND (effective_to IS NULL));

--
-- Name: count_dimension_aliases_lookup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_dimension_aliases_lookup_idx ON public.count_dimension_aliases USING btree (tenant_id, dimension, source_system, source_value_norm, review_status, effective_from, effective_to);

--
-- Name: count_dimension_aliases_review_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_dimension_aliases_review_queue_idx ON public.count_dimension_aliases USING btree (tenant_id, review_status, updated_at DESC, alias_id DESC);

--
-- Name: count_mismatch_scan_runs_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_mismatch_scan_runs_scope_idx ON public.count_mismatch_scan_runs USING btree (tenant_id, park_id, shed_id, counted_before DESC);

--
-- Name: count_mismatch_scan_runs_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_mismatch_scan_runs_status_idx ON public.count_mismatch_scan_runs USING btree (tenant_id, status, updated_at DESC);

--
-- Name: count_mismatch_scan_runs_tenant_started_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_mismatch_scan_runs_tenant_started_idx ON public.count_mismatch_scan_runs USING btree (tenant_id, started_at DESC, count_mismatch_scan_run_id DESC);

--
-- Name: count_projection_exception_resolutions_exception_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_exception_resolutions_exception_idx ON public.count_projection_exception_resolutions USING btree (tenant_id, count_projection_exception_id, created_at DESC, count_projection_exception_resolution_id DESC);

--
-- Name: count_projection_exception_resolutions_idempotency_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_projection_exception_resolutions_idempotency_unique ON public.count_projection_exception_resolutions USING btree (tenant_id, idempotency_key);

--
-- Name: count_projection_exceptions_closed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_exceptions_closed_idx ON public.count_projection_exceptions USING btree (tenant_id, status, resolved_at DESC, count_projection_exception_id DESC) WHERE (status = ANY (ARRAY['resolved'::text, 'dismissed'::text]));

--
-- Name: count_projection_exceptions_location_list_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_exceptions_location_list_idx ON public.count_projection_exceptions USING btree (tenant_id, status, park_id, shed_id, updated_at DESC, count_projection_exception_id DESC);

--
-- Name: count_projection_exceptions_open_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_projection_exceptions_open_unique ON public.count_projection_exceptions USING btree (tenant_id, exception_type, source_key, grain_key) WHERE (status = 'open'::text);

--
-- Name: count_projection_exceptions_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_exceptions_queue_idx ON public.count_projection_exceptions USING btree (tenant_id, status, severity, updated_at DESC, count_projection_exception_id DESC);

--
-- Name: count_projection_exceptions_status_list_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_exceptions_status_list_idx ON public.count_projection_exceptions USING btree (tenant_id, status, updated_at DESC, count_projection_exception_id DESC);

--
-- Name: count_projection_exceptions_work_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_exceptions_work_queue_idx ON public.count_projection_exceptions USING btree (tenant_id, status, work_state, severity, due_at, updated_at DESC, count_projection_exception_id DESC) WHERE (status = 'open'::text);

--
-- Name: count_projection_recompute_runs_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_recompute_runs_scope_idx ON public.count_projection_recompute_runs USING btree (tenant_id, park_id, horizon, target_date DESC, status, updated_at DESC);

--
-- Name: count_projection_recompute_runs_tenant_started_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_recompute_runs_tenant_started_idx ON public.count_projection_recompute_runs USING btree (tenant_id, started_at DESC, count_projection_recompute_run_id DESC);

--
-- Name: count_projection_snapshot_rows_anchor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_snapshot_rows_anchor_idx ON public.count_projection_snapshot_rows USING btree (tenant_id, base_count_anchor_id) WHERE (base_count_anchor_id IS NOT NULL);

--
-- Name: count_projection_snapshot_rows_blocker_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_snapshot_rows_blocker_idx ON public.count_projection_snapshot_rows USING btree (tenant_id, target_date, ration_context_resolution_state, count_projection_snapshot_row_id) WHERE (ration_context_resolution_state = ANY (ARRAY['unresolved'::text, 'blocked'::text]));

--
-- Name: count_projection_snapshot_rows_feed_hot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_snapshot_rows_feed_hot_idx ON public.count_projection_snapshot_rows USING btree (tenant_id, target_date, park_id, shed_id, lower(breed_key), count_projection_snapshot_row_id);

--
-- Name: count_projection_snapshot_rows_grain_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_projection_snapshot_rows_grain_unique ON public.count_projection_snapshot_rows USING btree (tenant_id, count_projection_snapshot_id, grain_key);

--
-- Name: count_projection_snapshot_rows_resolution_feed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_snapshot_rows_resolution_feed_idx ON public.count_projection_snapshot_rows USING btree (tenant_id, target_date, park_id, ration_context_resolution_state, shed_id, lower(breed_key), count_projection_snapshot_row_id);

--
-- Name: count_projection_snapshots_hot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_projection_snapshots_hot_idx ON public.count_projection_snapshots USING btree (tenant_id, horizon, park_id, target_date DESC, created_at DESC);

--
-- Name: count_projection_snapshots_source_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_projection_snapshots_source_unique ON public.count_projection_snapshots USING btree (tenant_id, horizon, park_id, target_date, source_hash);

--
-- Name: count_projection_snapshots_tenant_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX count_projection_snapshots_tenant_id_unique ON public.count_projection_snapshots USING btree (tenant_id, count_projection_snapshot_id);

--
-- Name: count_source_import_runs_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_source_import_runs_status_idx ON public.count_source_import_runs USING btree (tenant_id, status, updated_at DESC);

--
-- Name: count_source_import_runs_tenant_started_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX count_source_import_runs_tenant_started_idx ON public.count_source_import_runs USING btree (tenant_id, started_at DESC, count_source_import_run_id DESC);

--
-- Name: counts_shifting_readiness_evidence_prefix_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX counts_shifting_readiness_evidence_prefix_idx ON public.counts_shifting_readiness_evidence USING btree (tenant_id, subgate_id, evidence_ref text_pattern_ops, recorded_at DESC);

--
-- Name: counts_shifting_readiness_evidence_subgate_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX counts_shifting_readiness_evidence_subgate_idx ON public.counts_shifting_readiness_evidence USING btree (tenant_id, subgate_id, recorded_at DESC);

--
-- Name: counts_shifting_readiness_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX counts_shifting_readiness_status_idx ON public.counts_shifting_readiness_subgates USING btree (tenant_id, status, updated_at DESC);

--
-- Name: departments_tenant_code_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX departments_tenant_code_unique ON public.departments USING btree (tenant_id, code);

--
-- Name: domain_event_processed_events_retention_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX domain_event_processed_events_retention_idx ON public.domain_event_processed_events USING btree (processed_at, tenant_id, subscription_id, event_id) WHERE ((status = 'processed'::text) AND (processed_at IS NOT NULL));

--
-- Name: domain_event_processed_events_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX domain_event_processed_events_status_idx ON public.domain_event_processed_events USING btree (tenant_id, subscription_id, status, updated_at);

--
-- Name: feed_direction_completions_batch_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX feed_direction_completions_batch_idx ON public.feed_direction_completions USING btree (tenant_id, batch_id, status);

--
-- Name: feed_direction_completions_obligation_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX feed_direction_completions_obligation_idx ON public.feed_direction_completions USING btree (tenant_id, obligation_id);

--
-- Name: feed_direction_completions_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX feed_direction_completions_review_idx ON public.feed_direction_completions USING btree (tenant_id, fed_at) WHERE (status = 'recorded'::text);

--
-- Name: feed_direction_completions_shed_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX feed_direction_completions_shed_history_idx ON public.feed_direction_completions USING btree (tenant_id, shed_id, fed_at DESC);

--
-- Name: goat_custody_history_custodian_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_custody_history_custodian_idx ON public.goat_custody_history USING btree (custodian_party_id, valid_to);

--
-- Name: goat_custody_history_goat_current_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_custody_history_goat_current_idx ON public.goat_custody_history USING btree (goat_id, valid_to);

--
-- Name: goat_identifiers_goat_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identifiers_goat_status_idx ON public.goat_identifiers USING btree (goat_id, status);

--
-- Name: goat_identifiers_lifetime_value_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX goat_identifiers_lifetime_value_unique ON public.goat_identifiers USING btree (tenant_id, normalized_value);

--
-- Name: goat_identifiers_lookup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identifiers_lookup_idx ON public.goat_identifiers USING btree (tenant_id, identifier_type, normalized_value, scope_key, status);

--
-- Name: goat_identifiers_primary_per_goat_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX goat_identifiers_primary_per_goat_unique ON public.goat_identifiers USING btree (goat_id, identifier_type) WHERE (is_primary_for_goat AND (status = 'active'::text));

--
-- Name: goat_identifiers_source_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identifiers_source_idx ON public.goat_identifiers USING btree (source_system, source_record_id);

--
-- Name: goat_identity_events_goat_timeline_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identity_events_goat_timeline_idx ON public.goat_identity_events USING btree (goat_id, occurred_at DESC);

--
-- Name: goat_identity_events_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identity_events_idempotency_idx ON public.goat_identity_events USING btree (idempotency_key);

--
-- Name: goat_identity_events_tenant_goat_timeline_keyset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identity_events_tenant_goat_timeline_keyset_idx ON public.goat_identity_events USING btree (tenant_id, goat_id, occurred_at DESC, identity_event_id DESC);

--
-- Name: goat_identity_events_tenant_recorded_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identity_events_tenant_recorded_at_idx ON public.goat_identity_events USING btree (tenant_id, recorded_at DESC);

--
-- Name: goat_identity_events_tenant_recorded_event_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identity_events_tenant_recorded_event_idx ON public.goat_identity_events USING btree (tenant_id, recorded_at, identity_event_id);

--
-- Name: goat_identity_events_tenant_type_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_identity_events_tenant_type_recorded_idx ON public.goat_identity_events USING btree (tenant_id, event_type, recorded_at DESC);

--
-- Name: goat_location_history_goat_timeline_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_location_history_goat_timeline_idx ON public.goat_location_history USING btree (goat_id, occurred_at DESC);

--
-- Name: goat_location_history_tenant_from_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_location_history_tenant_from_location_idx ON public.goat_location_history USING btree (tenant_id, from_location_id, occurred_at DESC) WHERE (from_location_id IS NOT NULL);

--
-- Name: goat_location_history_tenant_to_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_location_history_tenant_to_location_idx ON public.goat_location_history USING btree (tenant_id, to_location_id, occurred_at DESC);

--
-- Name: goat_location_history_to_location_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_location_history_to_location_idx ON public.goat_location_history USING btree (to_location_id, occurred_at DESC);

--
-- Name: goat_merge_links_survivor_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_merge_links_survivor_idx ON public.goat_merge_links USING btree (survivor_goat_id);

--
-- Name: goat_ownership_goat_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_ownership_goat_active_idx ON public.goat_ownership USING btree (goat_id, status, valid_to);

--
-- Name: goat_ownership_owner_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goat_ownership_owner_idx ON public.goat_ownership USING btree (owner_party_id, status);

--
-- Name: goats_breed_sex_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_breed_sex_lifecycle_idx ON public.goats USING btree (breed_id, sex, lifecycle_status);

--
-- Name: goats_breed_text_sex_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_breed_text_sex_idx ON public.goats USING btree (breed, sex);

--
-- Name: goats_cohort_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_cohort_lifecycle_idx ON public.goats USING btree (cohort_id, lifecycle_status);

--
-- Name: goats_current_location_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_current_location_lifecycle_idx ON public.goats USING btree (current_location_id, lifecycle_status);

--
-- Name: goats_farm_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_farm_lifecycle_idx ON public.goats USING btree (farm_id, lifecycle_status);

--
-- Name: goats_merged_into_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_merged_into_idx ON public.goats USING btree (merged_into_goat_id);

--
-- Name: goats_park_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_park_lifecycle_idx ON public.goats USING btree (park_id, lifecycle_status);

--
-- Name: goats_shed_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_shed_lifecycle_idx ON public.goats USING btree (shed_id, lifecycle_status);

--
-- Name: goats_tenant_breed_display_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_breed_display_idx ON public.goats USING btree (tenant_id, breed, display_id) WHERE (merged_into_goat_id IS NULL);

--
-- Name: goats_tenant_breed_sex_display_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_breed_sex_display_idx ON public.goats USING btree (tenant_id, breed, sex, display_id) WHERE (merged_into_goat_id IS NULL);

--
-- Name: goats_tenant_custodian_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_custodian_lifecycle_idx ON public.goats USING btree (tenant_id, custodian_party_id, lifecycle_status);

--
-- Name: goats_tenant_display_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_display_idx ON public.goats USING btree (tenant_id, display_id);

--
-- Name: goats_tenant_growth_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_growth_idx ON public.goats USING btree (tenant_id, growth_cohort_tag);

--
-- Name: goats_tenant_health_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_health_idx ON public.goats USING btree (tenant_id, health_status);

--
-- Name: goats_tenant_lifecycle_display_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_lifecycle_display_idx ON public.goats USING btree (tenant_id, lifecycle_status, display_id) WHERE (merged_into_goat_id IS NULL);

--
-- Name: goats_tenant_lifecycle_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_lifecycle_idx ON public.goats USING btree (tenant_id, lifecycle_status);

--
-- Name: goats_tenant_lifecycle_shed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_lifecycle_shed_idx ON public.goats USING btree (tenant_id, lifecycle_status, shed_id) WHERE (merged_into_goat_id IS NULL);

--
-- Name: goats_tenant_management_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_management_idx ON public.goats USING btree (tenant_id, management_stage);

--
-- Name: goats_tenant_reproductive_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_reproductive_idx ON public.goats USING btree (tenant_id, reproductive_status);

--
-- Name: goats_tenant_sex_display_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goats_tenant_sex_display_idx ON public.goats USING btree (tenant_id, sex, display_id) WHERE (merged_into_goat_id IS NULL);

--
-- Name: herd_register_goat_projection_display_uidx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX herd_register_goat_projection_display_uidx ON public.herd_register_goat_projection USING btree (tenant_id, display_id);

--
-- Name: herd_register_goat_projection_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX herd_register_goat_projection_scope_idx ON public.herd_register_goat_projection USING btree (tenant_id, lifecycle_status, park_id, breed, sex, display_id);

--
-- Name: herd_register_summary_projection_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX herd_register_summary_projection_scope_idx ON public.herd_register_summary_projection USING btree (tenant_id, lifecycle_status, park_id, breed, sex, farm_id, current_location_id) INCLUDE (active_count, adult_count, kid_count, untagged_kid_count);

--
-- Name: idempotency_keys_expires_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_expires_idx ON public.idempotency_keys USING btree (expires_at);

--
-- Name: idempotency_keys_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idempotency_keys_scope_idx ON public.idempotency_keys USING btree (scope, first_seen_at DESC);

--
-- Name: identity_conflict_goats_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_conflict_goats_goat_idx ON public.identity_conflict_goats USING btree (goat_id);

--
-- Name: identity_conflicts_decision_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_conflicts_decision_idx ON public.identity_conflicts USING btree (decision_id);

--
-- Name: identity_conflicts_identifier_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_conflicts_identifier_idx ON public.identity_conflicts USING btree (identifier_type, identifier_value);

--
-- Name: identity_conflicts_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_conflicts_queue_idx ON public.identity_conflicts USING btree (tenant_id, state, severity, created_at DESC);

--
-- Name: identity_correction_requests_actor_keyset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_correction_requests_actor_keyset_idx ON public.identity_correction_requests USING btree (tenant_id, requested_by, created_at DESC, correction_request_id DESC);

--
-- Name: identity_correction_requests_admin_keyset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_correction_requests_admin_keyset_idx ON public.identity_correction_requests USING btree (tenant_id, created_at DESC, correction_request_id DESC);

--
-- Name: identity_correction_requests_admin_state_keyset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_correction_requests_admin_state_keyset_idx ON public.identity_correction_requests USING btree (tenant_id, state, created_at DESC, correction_request_id DESC);

--
-- Name: identity_correction_requests_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_correction_requests_goat_idx ON public.identity_correction_requests USING btree (goat_id, state);

--
-- Name: identity_correction_requests_identifier_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_correction_requests_identifier_idx ON public.identity_correction_requests USING btree (identifier_type, identifier_value);

--
-- Name: identity_correction_requests_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_correction_requests_queue_idx ON public.identity_correction_requests USING btree (tenant_id, state, created_at DESC);

--
-- Name: identity_decision_goats_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_decision_goats_goat_idx ON public.identity_decision_goats USING btree (goat_id);

--
-- Name: identity_decision_identifiers_decision_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_decision_identifiers_decision_idx ON public.identity_decision_identifiers USING btree (decision_id);

--
-- Name: identity_decision_identifiers_identifier_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_decision_identifiers_identifier_idx ON public.identity_decision_identifiers USING btree (identifier_id);

--
-- Name: identity_decisions_tenant_type_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX identity_decisions_tenant_type_state_idx ON public.identity_decisions USING btree (tenant_id, decision_type, decision_state, created_at DESC);

--
-- Name: idx_obligation_instances_calendar_exceptions; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_obligation_instances_calendar_exceptions ON public.obligation_instances USING btree (tenant_id, status) WHERE ((batch_id IS NULL) AND (status = ANY (ARRAY['missed'::text, 'in_progress'::text, 'deferred'::text])));

--
-- Name: idx_obligation_instances_calendar_overdue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_obligation_instances_calendar_overdue ON public.obligation_instances USING btree (tenant_id, status, due_at) WHERE ((batch_id IS NULL) AND (status = ANY (ARRAY['scheduled'::text, 'due'::text])));

--
-- Name: idx_obligation_instances_calendar_window; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_obligation_instances_calendar_window ON public.obligation_instances USING btree (tenant_id, due_at) WHERE ((batch_id IS NULL) AND (status <> ALL (ARRAY['waived'::text, 'canceled'::text, 'superseded'::text])));

--
-- Name: idx_obligation_instances_sop_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_obligation_instances_sop_task ON public.obligation_instances USING btree (tenant_id, sop_task_id) WHERE (sop_task_id IS NOT NULL);

--
-- Name: inventory_stock_fefo_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_fefo_idx ON public.inventory_stock USING btree (tenant_id, location_id, item_id, expiry_date) WHERE (quantity_in_stock > (0)::numeric);

--
-- Name: inventory_stock_movements_batch_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_batch_idx ON public.inventory_stock_movements USING btree (tenant_id, batch_id) WHERE (batch_id IS NOT NULL);

--
-- Name: inventory_stock_movements_lot_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_lot_idx ON public.inventory_stock_movements USING btree (tenant_id, lot_id, occurred_at);

--
-- Name: location_aliases_canonical_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_aliases_canonical_order_idx ON public.location_aliases USING btree (tenant_id, canonical_location_id, status, source_context, alias_code, alias_id);

--
-- Name: location_aliases_canonical_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_aliases_canonical_status_idx ON public.location_aliases USING btree (tenant_id, canonical_location_id, status, source_context);

--
-- Name: location_aliases_unique_active_alias; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX location_aliases_unique_active_alias ON public.location_aliases USING btree (tenant_id, source_context, lower(regexp_replace(btrim(alias_code), '\s+'::text, ' '::text, 'g'::text))) WHERE (status = 'active'::text);

--
-- Name: location_capacity_records_effective_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_capacity_records_effective_idx ON public.location_capacity_records USING btree (tenant_id, location_id, capacity_kind, effective_from DESC, effective_to);

--
-- Name: location_capacity_records_source_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_capacity_records_source_idx ON public.location_capacity_records USING btree (tenant_id, source, source_ref);

--
-- Name: location_operational_attributes_counts_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_operational_attributes_counts_idx ON public.location_operational_attributes USING btree (tenant_id, usable_for_counts, is_holding, is_quarantine, is_icu);

--
-- Name: location_projection_invalidations_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_projection_invalidations_pending_idx ON public.location_projection_invalidations USING btree (tenant_id, projection_module, status, created_at DESC);

--
-- Name: location_review_items_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_review_items_label_idx ON public.location_review_items USING btree (tenant_id, source_context, normalized_source_label);

--
-- Name: location_review_items_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX location_review_items_queue_idx ON public.location_review_items USING btree (tenant_id, status, review_type, updated_at DESC, review_id DESC);

--
-- Name: location_review_items_unique_open_evidence; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX location_review_items_unique_open_evidence ON public.location_review_items USING btree (tenant_id, review_type, COALESCE(source_context, ''::text), COALESCE(normalized_source_label, ''::text), evidence_hash) WHERE (status = 'open'::text);

--
-- Name: locations_parent_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_parent_idx ON public.locations USING btree (parent_location_id);

--
-- Name: locations_tenant_lower_name_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_tenant_lower_name_idx ON public.locations USING btree (tenant_id, lower(name), location_id);

--
-- Name: locations_tenant_parent_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_tenant_parent_order_idx ON public.locations USING btree (tenant_id, parent_location_id, location_type, display_order, name, location_id);

--
-- Name: locations_tenant_parent_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_tenant_parent_status_idx ON public.locations USING btree (tenant_id, parent_location_id, status, display_order, name, location_id);

--
-- Name: locations_tenant_status_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_tenant_status_order_idx ON public.locations USING btree (tenant_id, status, location_type, display_order, name, location_id);

--
-- Name: locations_tenant_type_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_tenant_type_idx ON public.locations USING btree (tenant_id, location_type, status);

--
-- Name: locations_tenant_type_status_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX locations_tenant_type_status_order_idx ON public.locations USING btree (tenant_id, location_type, status, display_order, name, location_id);

--
-- Name: movement_commands_submission_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX movement_commands_submission_unique_idx ON public.movement_commands USING btree (tenant_id, submission_id, command_type);

--
-- Name: movement_commands_task_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX movement_commands_task_idx ON public.movement_commands USING btree (tenant_id, task_id, created_at DESC);

--
-- Name: notification_delivery_attempts_request_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_delivery_attempts_request_idx ON public.notification_delivery_attempts USING btree (tenant_id, notification_request_id, attempt_no);

--
-- Name: notification_requests_due_order_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_requests_due_order_idx ON public.notification_requests USING btree (tenant_id, COALESCE(next_attempt_at, requested_at), notification_request_id) WHERE (status = ANY (ARRAY['queued'::text, 'failed'::text]));

--
-- Name: notification_requests_event_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_requests_event_idx ON public.notification_requests USING btree (tenant_id, calendar_event_id, requested_at DESC, notification_request_id DESC);

--
-- Name: notification_requests_oldest_due_requested_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_requests_oldest_due_requested_at_idx ON public.notification_requests USING btree (tenant_id, requested_at) INCLUDE (next_attempt_at) WHERE (status = ANY (ARRAY['queued'::text, 'failed'::text]));

--
-- Name: notification_requests_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_requests_queue_idx ON public.notification_requests USING btree (tenant_id, status, COALESCE(next_attempt_at, requested_at), notification_request_id) WHERE (status = ANY (ARRAY['queued'::text, 'failed'::text]));

--
-- Name: notification_requests_recipient_ref_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_requests_recipient_ref_pending_idx ON public.notification_requests USING btree (tenant_id, recipient_ref, status) WHERE ((recipient_ref IS NOT NULL) AND (status = ANY (ARRAY['queued'::text, 'failed'::text])));

--
-- Name: notification_requests_sending_lease_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX notification_requests_sending_lease_idx ON public.notification_requests USING btree (tenant_id, leased_at, notification_request_id) WHERE (status = 'sending'::text);

--
-- Name: obligation_batches_combo_align_keyset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_batches_combo_align_keyset_idx ON public.obligation_batches USING btree (tenant_id, scope_type, scope_id, session, batch_id) WHERE ((status = 'planned'::text) AND (sop_task_id IS NULL) AND (session ~~ 'combo:%'::text));

--
-- Name: obligation_batches_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_batches_scope_idx ON public.obligation_batches USING btree (tenant_id, scope_type, scope_id, status);

--
-- Name: obligation_batches_stock_reconcile_required_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_batches_stock_reconcile_required_idx ON public.obligation_batches USING btree (tenant_id, updated_at, batch_id) WHERE (((context #>> '{defer_repair,state}'::text[]) = 'stock_reconcile_required'::text) OR ((context #>> '{shift_repair,state}'::text[]) = 'stock_reconcile_required'::text) OR ((context #>> '{cancel_repair,state}'::text[]) = 'stock_reconcile_required'::text) OR ((context #>> '{missed_repair,state}'::text[]) = 'stock_reconcile_required'::text));

--
-- Name: obligation_batches_unfinalized_planned_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX obligation_batches_unfinalized_planned_unique_idx ON public.obligation_batches USING btree (tenant_id, protocol_version_id, scope_type, scope_id, COALESCE(session, ''::text), COALESCE(planned_date, '-infinity'::date), COALESCE(window_start, '-infinity'::timestamp with time zone), COALESCE(window_end, '-infinity'::timestamp with time zone)) WHERE ((status = 'planned'::text) AND (sop_task_id IS NULL) AND (NOT (context ? 'stock_reservation'::text)));

--
-- Name: obligation_escalations_obligation_level_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_escalations_obligation_level_idx ON public.obligation_escalations USING btree (tenant_id, obligation_id, level, status);

--
-- Name: obligation_escalations_open_level_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX obligation_escalations_open_level_unique ON public.obligation_escalations USING btree (tenant_id, obligation_id, level) WHERE (status = ANY (ARRAY['open'::text, 'acknowledged'::text]));

--
-- Name: obligation_escalations_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_escalations_status_idx ON public.obligation_escalations USING btree (tenant_id, status, level);

--
-- Name: obligation_goat_shift_watermarks_timeline_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_goat_shift_watermarks_timeline_idx ON public.obligation_goat_shift_watermarks USING btree (tenant_id, last_occurred_at DESC);

--
-- Name: obligation_instances_batch_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_instances_batch_idx ON public.obligation_instances USING btree (tenant_id, batch_id, status);

--
-- Name: obligation_instances_due_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_instances_due_window_idx ON public.obligation_instances USING btree (tenant_id, status, due_at, obligation_id);

--
-- Name: obligation_instances_missed_deadline_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_instances_missed_deadline_idx ON public.obligation_instances USING btree (tenant_id, status, COALESCE(window_end, due_at), obligation_id) WHERE (status = ANY (ARRAY['scheduled'::text, 'due'::text, 'in_progress'::text]));

--
-- Name: obligation_instances_open_logical_due_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX obligation_instances_open_logical_due_idx ON public.obligation_instances USING btree (tenant_id, protocol_version_id, rule_id, target_type, target_id, sequence, due_at) WHERE (status = ANY (ARRAY['scheduled'::text, 'due'::text, 'in_progress'::text, 'deferred'::text, 'missed'::text]));

--
-- Name: obligation_instances_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_instances_scope_idx ON public.obligation_instances USING btree (tenant_id, scope_type, scope_id, status, due_at);

--
-- Name: obligation_instances_target_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_instances_target_idx ON public.obligation_instances USING btree (tenant_id, target_type, target_id, status);

--
-- Name: obligation_instances_unbatched_due_version_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_instances_unbatched_due_version_idx ON public.obligation_instances USING btree (tenant_id, protocol_version_id, scope_type, scope_id, rule_id, due_at, obligation_id) WHERE ((batch_id IS NULL) AND (status = ANY (ARRAY['scheduled'::text, 'due'::text, 'missed'::text])));

--
-- Name: obligation_status_events_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX obligation_status_events_idempotency_idx ON public.obligation_status_events USING btree (tenant_id, idempotency_key);

--
-- Name: obligation_status_events_obligation_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_status_events_obligation_idx ON public.obligation_status_events USING btree (obligation_id, occurred_at DESC);

--
-- Name: obligation_status_events_tenant_type_recorded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX obligation_status_events_tenant_type_recorded_idx ON public.obligation_status_events USING btree (tenant_id, event_type, recorded_at DESC);

--
-- Name: org_role_catalog_tier_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX org_role_catalog_tier_idx ON public.org_role_catalog USING btree (tier_code);

--
-- Name: org_role_catalog_vertical_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX org_role_catalog_vertical_idx ON public.org_role_catalog USING btree (vertical_code);

--
-- Name: outbox_dlq_actions_tenant_created_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_dlq_actions_tenant_created_idx ON public.outbox_dlq_actions USING btree (tenant_id, created_at DESC, action_id DESC);

--
-- Name: outbox_messages_aggregate_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_messages_aggregate_idx ON public.outbox_messages USING btree (aggregate_type, aggregate_id, created_at);

--
-- Name: outbox_messages_config_changed_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_config_changed_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'config.changed'::text);

--
-- Name: outbox_messages_counts_base_anchor_recorded_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_counts_base_anchor_recorded_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'counts.base_count_anchor.recorded'::text);

--
-- Name: outbox_messages_counts_projection_exception_closed_idempotency_; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_counts_projection_exception_closed_idempotency_ ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'counts.projection_exception.closed'::text);

--
-- Name: outbox_messages_counts_projection_exception_opened_idempotency_; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_counts_projection_exception_opened_idempotency_ ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'counts.projection_exception.opened'::text);

--
-- Name: outbox_messages_counts_projection_exception_updated_idempotency; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_counts_projection_exception_updated_idempotency ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'counts.projection_exception.updated'::text);

--
-- Name: outbox_messages_counts_shifting_event_recorded_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_counts_shifting_event_recorded_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'counts.shifting_event.recorded'::text);

--
-- Name: outbox_messages_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_messages_created_at_idx ON public.outbox_messages USING btree (created_at);

--
-- Name: outbox_messages_discarded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_messages_discarded_idx ON public.outbox_messages USING btree (tenant_id, status, updated_at DESC, outbox_id DESC) WHERE (status = 'discarded'::text);

--
-- Name: outbox_messages_obligation_canceled_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_obligation_canceled_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'goat.obligations_canceled'::text);

--
-- Name: outbox_messages_obligation_missed_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_obligation_missed_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'obligation.missed'::text);

--
-- Name: outbox_messages_obligation_rescoped_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_obligation_rescoped_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'obligation.rescoped'::text);

--
-- Name: outbox_messages_protocol_published_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_protocol_published_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'protocol.version.published'::text);

--
-- Name: outbox_messages_protocol_retired_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_protocol_retired_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'protocol.version.retired'::text);

--
-- Name: outbox_messages_replay_guard_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_messages_replay_guard_idx ON public.outbox_messages USING btree (tenant_id, status, replay_count, updated_at) WHERE (status = ANY (ARRAY['dead_letter'::text, 'failed'::text]));

--
-- Name: outbox_messages_status_next_attempt_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_messages_status_next_attempt_idx ON public.outbox_messages USING btree (status, next_attempt_at, created_at);

--
-- Name: outbox_messages_tenant_status_attempt_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX outbox_messages_tenant_status_attempt_idx ON public.outbox_messages USING btree (tenant_id, status, next_attempt_at, created_at, outbox_id);

--
-- Name: outbox_messages_vaccination_completed_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_vaccination_completed_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = 'vaccination.completed'::text);

--
-- Name: outbox_messages_verification_idempotency_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX outbox_messages_verification_idempotency_idx ON public.outbox_messages USING btree (tenant_id, idempotency_key) WHERE (event_type = ANY (ARRAY['verification.item.pending'::text, 'verification.verdict.approved'::text, 'verification.verdict.rework'::text, 'verification.item.closed'::text]));

--
-- Name: planned_batch_finalization_keyset_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX planned_batch_finalization_keyset_idx ON public.obligation_batches USING btree (tenant_id, protocol_version_id, created_at, batch_id) WHERE (status = 'planned'::text);

--
-- Name: position_module_duties_by_module; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX position_module_duties_by_module ON public.position_module_duties USING btree (tenant_id, module_code, duty_type) WHERE (status = 'active'::text);

--
-- Name: position_module_duties_by_position; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX position_module_duties_by_position ON public.position_module_duties USING btree (tenant_id, position_code) WHERE (status = 'active'::text);

--
-- Name: position_module_duties_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX position_module_duties_uq ON public.position_module_duties USING btree (tenant_id, position_code, module_code, duty_type, effective_from);

--
-- Name: procurement_hf_vaccination_evidence_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_hf_vaccination_evidence_goat_idx ON public.procurement_hf_vaccination_evidence USING btree (tenant_id, goat_id, review_status, administered_at DESC, evidence_id DESC);

--
-- Name: procurement_hf_vaccination_evidence_load_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_hf_vaccination_evidence_load_idx ON public.procurement_hf_vaccination_evidence USING btree (tenant_id, load_id, review_status, administered_at DESC, evidence_id DESC);

--
-- Name: procurement_hf_vaccination_evidence_trusted_rule_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_hf_vaccination_evidence_trusted_rule_idx ON public.procurement_hf_vaccination_evidence USING btree (tenant_id, goat_id, protocol_version_id, rule_id, dose_code, administered_at DESC) WHERE (review_status = 'trusted'::text);

--
-- Name: procurement_load_goats_action_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_action_idx ON public.procurement_load_goats USING btree (tenant_id, current_state, ownership_state, source_entry_state, updated_at DESC, goat_id);

--
-- Name: procurement_load_goats_animal_identifier_1_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_animal_identifier_1_idx ON public.procurement_load_goats USING btree (tenant_id, lower(animal_identifier_1), load_id) WHERE (animal_identifier_1 IS NOT NULL);

--
-- Name: procurement_load_goats_animal_identifier_2_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_animal_identifier_2_idx ON public.procurement_load_goats USING btree (tenant_id, animal_identifier_2, load_id) WHERE (animal_identifier_2 IS NOT NULL);

--
-- Name: procurement_load_goats_goat_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_goat_state_idx ON public.procurement_load_goats USING btree (tenant_id, goat_id, current_state, updated_at DESC);

--
-- Name: procurement_load_goats_intake_eligibility_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_intake_eligibility_idx ON public.procurement_load_goats USING btree (tenant_id, load_id, current_state, health_state, source_entry_state, ownership_state, goat_id) WHERE ((loaded_at IS NOT NULL) AND (arrived_at IS NOT NULL));

--
-- Name: procurement_load_goats_load_detail_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_load_detail_idx ON public.procurement_load_goats USING btree (tenant_id, load_id, created_at, goat_id);

--
-- Name: procurement_load_goats_load_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_load_state_idx ON public.procurement_load_goats USING btree (tenant_id, load_id, current_state, goat_id);

--
-- Name: procurement_load_goats_purpose_warmup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_purpose_warmup_idx ON public.procurement_load_goats USING btree (tenant_id, purpose, warmup_days, current_state, updated_at DESC);

--
-- Name: procurement_load_goats_work_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_load_goats_work_idx ON public.procurement_load_goats USING btree (tenant_id, updated_at DESC, load_goat_id DESC);

--
-- Name: procurement_loads_board_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_loads_board_idx ON public.procurement_loads USING btree (tenant_id, status, updated_at DESC, load_id DESC);

--
-- Name: procurement_loads_page_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_loads_page_idx ON public.procurement_loads USING btree (tenant_id, updated_at DESC, load_id DESC);

--
-- Name: procurement_loads_source_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_loads_source_idx ON public.procurement_loads USING btree (tenant_id, source_party_id, purchase_date DESC, load_id DESC);

--
-- Name: procurement_pc_handoffs_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_pc_handoffs_goat_idx ON public.procurement_pc_handoffs USING btree (tenant_id, goat_id, accepted_at DESC);

--
-- Name: procurement_pc_handoffs_pending_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_pc_handoffs_pending_idx ON public.procurement_pc_handoffs USING btree (tenant_id, event_status, accepted_at, handoff_id);

--
-- Name: procurement_source_health_checks_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX procurement_source_health_checks_goat_idx ON public.procurement_source_health_checks USING btree (tenant_id, goat_id, checked_at DESC);

--
-- Name: proof_artifacts_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX proof_artifacts_scope_idx ON public.proof_artifacts USING btree (tenant_id, scope_type, scope_id, created_at DESC);

--
-- Name: proof_artifacts_subject_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX proof_artifacts_subject_idx ON public.proof_artifacts USING btree (tenant_id, subject_type, subject_id, created_at DESC) WHERE (subject_id IS NOT NULL);

--
-- Name: proof_artifacts_tenant_idempotency_key_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX proof_artifacts_tenant_idempotency_key_unique_idx ON public.proof_artifacts USING btree (tenant_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);

--
-- Name: proof_artifacts_tenant_object_key_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX proof_artifacts_tenant_object_key_unique_idx ON public.proof_artifacts USING btree (tenant_id, object_key);

--
-- Name: proof_artifacts_tenant_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX proof_artifacts_tenant_state_idx ON public.proof_artifacts USING btree (tenant_id, upload_state, created_at DESC, proof_id DESC);

--
-- Name: protocol_rule_dimensions_age_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_rule_dimensions_age_idx ON public.protocol_rule_dimensions USING btree (tenant_id, protocol_version_id, category, min_age_days, max_age_days);

--
-- Name: protocol_rule_dimensions_match_folded_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_rule_dimensions_match_folded_idx ON public.protocol_rule_dimensions USING btree (tenant_id, protocol_version_id, category, species, lower(animal_stage), sex, lower(breed));

--
-- Name: protocol_rule_dimensions_match_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_rule_dimensions_match_idx ON public.protocol_rule_dimensions USING btree (tenant_id, protocol_version_id, category, species, animal_stage, sex, breed);

--
-- Name: protocol_rule_dimensions_rule_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_rule_dimensions_rule_idx ON public.protocol_rule_dimensions USING btree (tenant_id, rule_id);

--
-- Name: protocol_rules_version_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_rules_version_idx ON public.protocol_rules USING btree (tenant_id, protocol_version_id, sort_order);

--
-- Name: protocol_triggers_version_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_triggers_version_idx ON public.protocol_triggers USING btree (tenant_id, protocol_version_id, is_active);

--
-- Name: protocol_versions_lookup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX protocol_versions_lookup_idx ON public.protocol_versions USING btree (tenant_id, protocol_id, status, effective_from);

--
-- Name: seed_runs_tenant_state_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX seed_runs_tenant_state_idx ON public.seed_runs USING btree (tenant_id, updated_at DESC);

--
-- Name: shed_profiles_animal_stage_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shed_profiles_animal_stage_idx ON public.shed_profiles USING btree (animal_stage_id);

--
-- Name: shifting_event_impacts_event_breed_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shifting_event_impacts_event_breed_idx ON public.shifting_event_impacts USING btree (tenant_id, shifting_event_id, lower(breed_key));

--
-- Name: shifting_event_impacts_grain_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX shifting_event_impacts_grain_unique ON public.shifting_event_impacts USING btree (tenant_id, shifting_event_id, grain_key);

--
-- Name: shifting_event_impacts_projection_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shifting_event_impacts_projection_idx ON public.shifting_event_impacts USING btree (tenant_id, lower(breed_key), stage_tag, ration_context_resolution_state);

--
-- Name: shifting_events_destination_park_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shifting_events_destination_park_window_idx ON public.shifting_events USING btree (tenant_id, destination_park_id, event_status, effective_at, shifting_event_id) WHERE (event_status = ANY (ARRAY['authorized'::text, 'applied'::text]));

--
-- Name: shifting_events_idempotency_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX shifting_events_idempotency_unique ON public.shifting_events USING btree (tenant_id, idempotency_key);

--
-- Name: shifting_events_logical_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX shifting_events_logical_key_unique ON public.shifting_events USING btree (tenant_id, logical_shifting_event_key);

--
-- Name: shifting_events_projection_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shifting_events_projection_window_idx ON public.shifting_events USING btree (tenant_id, event_status, effective_at, destination_park_id, destination_shed_id, shifting_event_id);

--
-- Name: shifting_events_source_park_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shifting_events_source_park_window_idx ON public.shifting_events USING btree (tenant_id, source_park_id, event_status, effective_at, shifting_event_id) WHERE ((source_shed_id IS NOT NULL) AND (event_status = ANY (ARRAY['authorized'::text, 'applied'::text])));

--
-- Name: shifting_events_source_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX shifting_events_source_window_idx ON public.shifting_events USING btree (tenant_id, event_status, effective_at, source_park_id, source_shed_id, shifting_event_id) WHERE (source_shed_id IS NOT NULL);

--
-- Name: shifting_events_tenant_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX shifting_events_tenant_id_unique ON public.shifting_events USING btree (tenant_id, shifting_event_id);

--
-- Name: sop_definitions_tenant_code_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_definitions_tenant_code_unique_idx ON public.sop_definitions USING btree (tenant_id, code);

--
-- Name: sop_definitions_tenant_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_definitions_tenant_status_idx ON public.sop_definitions USING btree (tenant_id, status, updated_at DESC, sop_id DESC);

--
-- Name: sop_submission_items_goat_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_submission_items_goat_history_idx ON public.sop_submission_items USING btree (tenant_id, goat_id, created_at DESC) WHERE (goat_id IS NOT NULL);

--
-- Name: sop_submission_items_submission_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_submission_items_submission_idx ON public.sop_submission_items USING btree (tenant_id, submission_id, item_id);

--
-- Name: sop_submissions_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_submissions_review_idx ON public.sop_submissions USING btree (tenant_id, state, submitted_at DESC, submission_id DESC);

--
-- Name: sop_submissions_task_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_submissions_task_history_idx ON public.sop_submissions USING btree (tenant_id, task_id, submitted_at DESC);

--
-- Name: sop_submissions_tenant_idempotency_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_submissions_tenant_idempotency_unique_idx ON public.sop_submissions USING btree (tenant_id, idempotency_key);

--
-- Name: sop_task_scan_captures_idempotency_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_task_scan_captures_idempotency_unique_idx ON public.sop_task_scan_captures USING btree (tenant_id, idempotency_key);

--
-- Name: sop_task_scan_captures_task_field_tag_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_task_scan_captures_task_field_tag_unique_idx ON public.sop_task_scan_captures USING btree (tenant_id, task_id, field_key, normalized_tag);

--
-- Name: sop_task_scan_captures_task_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_task_scan_captures_task_idx ON public.sop_task_scan_captures USING btree (tenant_id, task_id, captured_at, capture_id);

--
-- Name: sop_task_scan_attempts_idempotency_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_task_scan_attempts_idempotency_unique_idx ON public.sop_task_scan_attempts USING btree (tenant_id, idempotency_key);

--
-- Name: sop_task_scan_attempts_task_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_task_scan_attempts_task_goat_idx ON public.sop_task_scan_attempts USING btree (tenant_id, task_id, goat_id, captured_at, attempt_id);

--
-- Name: sop_task_scan_attempts_task_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_task_scan_attempts_task_idx ON public.sop_task_scan_attempts USING btree (tenant_id, task_id, captured_at, attempt_id);

--
-- Name: sop_task_review_fanouts_retry_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_task_review_fanouts_retry_idx ON public.sop_task_review_fanouts USING btree (tenant_id, status, updated_at, review_fanout_id) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));

--
-- Name: sop_task_submission_fanouts_retry_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_task_submission_fanouts_retry_idx ON public.sop_task_submission_fanouts USING btree (tenant_id, status, updated_at, submission_fanout_id) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));

--
-- Name: sop_tasks_assignee_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_tasks_assignee_queue_idx ON public.sop_tasks USING btree (tenant_id, assigned_to, state, due_at, task_id) WHERE (assigned_to IS NOT NULL);

--
-- Name: sop_tasks_obligation_batch_id_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_tasks_obligation_batch_id_unique_idx ON public.sop_tasks USING btree (tenant_id, ((context ->> 'obligation_batch_id'::text))) WHERE (context ? 'obligation_batch_id'::text);

--
-- Name: sop_tasks_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_tasks_queue_idx ON public.sop_tasks USING btree (tenant_id, state, due_at, task_id);

--
-- Name: sop_tasks_scope_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_tasks_scope_queue_idx ON public.sop_tasks USING btree (tenant_id, scope_type, scope_id, state, due_at, task_id);

--
-- Name: sop_tasks_sop_version_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_tasks_sop_version_idx ON public.sop_tasks USING btree (tenant_id, sop_version_id, state);

--
-- Name: sop_versions_one_published_per_sop_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_versions_one_published_per_sop_idx ON public.sop_versions USING btree (tenant_id, sop_id) WHERE (status = 'published'::text);

--
-- Name: sop_versions_tenant_sop_version_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX sop_versions_tenant_sop_version_unique_idx ON public.sop_versions USING btree (tenant_id, sop_id, version);

--
-- Name: sop_versions_tenant_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX sop_versions_tenant_status_idx ON public.sop_versions USING btree (tenant_id, status, updated_at DESC, sop_version_id DESC);

--
-- Name: source_entry_decisions_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX source_entry_decisions_goat_idx ON public.source_entry_decisions USING btree (tenant_id, goat_id, decided_at DESC);

--
-- Name: source_entry_decisions_load_stage_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX source_entry_decisions_load_stage_idx ON public.source_entry_decisions USING btree (tenant_id, load_id, decision_stage, decided_at DESC);

--
-- Name: source_holding_stays_goat_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX source_holding_stays_goat_idx ON public.source_holding_stays USING btree (tenant_id, goat_id, status, started_at DESC);

--
-- Name: source_holding_stays_load_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX source_holding_stays_load_idx ON public.source_holding_stays USING btree (tenant_id, load_id, status, started_at DESC);

--
-- Name: source_holding_stays_purpose_warmup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX source_holding_stays_purpose_warmup_idx ON public.source_holding_stays USING btree (tenant_id, purpose, warmup_days, status, started_at DESC);

--
-- Name: source_holding_stays_warmup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX source_holding_stays_warmup_idx ON public.source_holding_stays USING btree (tenant_id, warmup_state, warmup_days, status);

--
-- Name: status_definitions_axis_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX status_definitions_axis_active_idx ON public.status_definitions USING btree (axis, active, sort_order);

--
-- Name: transit_handoffs_load_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX transit_handoffs_load_idx ON public.transit_handoffs USING btree (tenant_id, load_id, status, dispatched_at DESC);

--
-- Name: transit_handoffs_load_proof_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX transit_handoffs_load_proof_idx ON public.transit_handoffs USING btree (tenant_id, load_id, status, proof_ref_id) WHERE (proof_ref_id IS NOT NULL);

--
-- Name: user_scope_grants_scope_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_scope_grants_scope_idx ON public.user_scope_grants USING btree (tenant_id, scope_type, scope_id, role, status);

--
-- Name: user_scope_grants_user_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX user_scope_grants_user_active_idx ON public.user_scope_grants USING btree (user_id, status, valid_from, valid_to);

--
-- Name: vaccination_completions_accepted_history_calendar_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_completions_accepted_history_calendar_idx ON public.vaccination_completions USING btree (tenant_id, administered_at, obligation_id) WHERE (status = 'accepted'::text);

--
-- Name: vaccination_completions_batch_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_completions_batch_idx ON public.vaccination_completions USING btree (tenant_id, batch_id, status);

--
-- Name: vaccination_completions_goat_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_completions_goat_history_idx ON public.vaccination_completions USING btree (tenant_id, goat_id, administered_at DESC);

--
-- Name: vaccination_completions_obligation_goat_active_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX vaccination_completions_obligation_goat_active_unique_idx ON public.vaccination_completions USING btree (tenant_id, obligation_id, goat_id) WHERE (status = ANY (ARRAY['recorded'::text, 'accepted'::text]));

--
-- Name: vaccination_completions_obligation_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_completions_obligation_idx ON public.vaccination_completions USING btree (tenant_id, obligation_id);

--
-- Name: vaccination_completions_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_completions_review_idx ON public.vaccination_completions USING btree (tenant_id, administered_at) WHERE (status = 'recorded'::text);

--
-- Name: vaccination_completions_submission_item_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_completions_submission_item_idx ON public.vaccination_completions USING btree (tenant_id, sop_submission_item_id) WHERE (sop_submission_item_id IS NOT NULL);

--
-- Name: vaccination_eligibility_rollups_grain_uidx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX vaccination_eligibility_rollups_grain_uidx ON public.vaccination_eligibility_rollups USING btree (tenant_id, COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid), COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid), species, management_stage, sex, breed, health_status, usable_for_vaccination);

--
-- Name: vaccination_eligibility_rollups_preview_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_eligibility_rollups_preview_idx ON public.vaccination_eligibility_rollups USING btree (tenant_id, usable_for_vaccination, park_id, management_stage, sex, breed, health_status);

--
-- Name: vaccination_generation_runs_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_generation_runs_status_idx ON public.vaccination_generation_runs USING btree (tenant_id, status, updated_at DESC);

--
-- Name: vaccination_generation_runs_version_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_generation_runs_version_idx ON public.vaccination_generation_runs USING btree (tenant_id, protocol_version_id, started_at DESC);

--
-- Name: vaccination_reminder_cadence_fires_park_day_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_reminder_cadence_fires_park_day_idx ON public.vaccination_reminder_cadence_fires USING btree (tenant_id, park_id, fire_day);

--
-- Name: vaccination_source_facts_tenant_disposition_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX vaccination_source_facts_tenant_disposition_idx ON public.vaccination_source_facts USING btree (tenant_id, disposition);

--

--

--

--
-- Name: verification_items_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX verification_items_queue_idx ON public.verification_items USING btree (tenant_id, status, category, captured_at, item_id);

--
-- Name: verification_items_source_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX verification_items_source_idx ON public.verification_items USING btree (tenant_id, source_module, source_ref_type, source_ref_id);

--
-- Name: verification_items_source_submission_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX verification_items_source_submission_idx ON public.verification_items USING btree (tenant_id, source_submission_id) WHERE (source_submission_id IS NOT NULL);

--
-- Name: verification_items_leadership_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX verification_items_leadership_queue_idx ON public.verification_items USING btree (tenant_id, park_id, source_submission_id, captured_at, item_id) WHERE ((status = 'approved'::text) AND (closed_at IS NULL));

--
-- Name: workforce_absences_member_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_absences_member_window_idx ON public.workforce_absences USING btree (tenant_id, workforce_member_id, status, starts_at, ends_at);

--
-- Name: workforce_absences_scope_window_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_absences_scope_window_idx ON public.workforce_absences USING btree (tenant_id, scope_type, scope_id, status, starts_at, ends_at);

--
-- Name: workforce_capabilities_code_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_capabilities_code_unique_idx ON public.workforce_capabilities USING btree (tenant_id, capability_code);

--
-- Name: workforce_capabilities_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_capabilities_status_idx ON public.workforce_capabilities USING btree (tenant_id, status, capability_code);

--
-- Name: workforce_external_identities_member_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_external_identities_member_status_idx ON public.workforce_external_identities USING btree (tenant_id, workforce_member_id, status) WHERE (workforce_member_id IS NOT NULL);

--
-- Name: workforce_external_identities_ref_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_external_identities_ref_unique_idx ON public.workforce_external_identities USING btree (tenant_id, source_system, external_ref_type, external_ref_hash);

--
-- Name: workforce_external_identities_status_seen_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_external_identities_status_seen_idx ON public.workforce_external_identities USING btree (tenant_id, status, last_seen_at DESC, external_identity_id DESC);

--
-- Name: workforce_member_app_sessions_device_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_member_app_sessions_device_idx ON public.workforce_member_app_sessions USING btree (tenant_id, device_id, last_seen_at DESC) WHERE (device_id IS NOT NULL);

--
-- Name: workforce_member_app_sessions_member_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_member_app_sessions_member_status_idx ON public.workforce_member_app_sessions USING btree (tenant_id, workforce_member_id, status, last_seen_at DESC);

--
-- Name: workforce_member_capabilities_active_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_member_capabilities_active_unique_idx ON public.workforce_member_capabilities USING btree (tenant_id, workforce_member_id, capability_id, scope_type, scope_id) WHERE ((status = 'active'::text) AND (valid_to IS NULL));

--
-- Name: workforce_member_capabilities_member_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_member_capabilities_member_active_idx ON public.workforce_member_capabilities USING btree (tenant_id, workforce_member_id, status, valid_from, valid_to);

--
-- Name: workforce_member_capabilities_scope_active_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_member_capabilities_scope_active_idx ON public.workforce_member_capabilities USING btree (tenant_id, capability_id, scope_type, scope_id, status, valid_from, valid_to);

--
-- Name: workforce_member_devices_install_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_member_devices_install_unique_idx ON public.workforce_member_devices USING btree (tenant_id, app_install_id);

--
-- Name: workforce_member_devices_last_seen_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_member_devices_last_seen_idx ON public.workforce_member_devices USING btree (tenant_id, last_seen_at DESC);

--
-- Name: workforce_member_devices_member_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_member_devices_member_status_idx ON public.workforce_member_devices USING btree (tenant_id, workforce_member_id, status);

--
-- Name: workforce_members_active_user_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_members_active_user_unique_idx ON public.workforce_members USING btree (tenant_id, user_id) WHERE ((user_id IS NOT NULL) AND (status = 'active'::text));

--
-- Name: workforce_members_code_unique_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_members_code_unique_idx ON public.workforce_members USING btree (tenant_id, display_code);

--
-- Name: workforce_members_department_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_members_department_idx ON public.workforce_members USING btree (tenant_id, department_id);

--
-- Name: workforce_members_location_status_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_members_location_status_idx ON public.workforce_members USING btree (tenant_id, primary_location_id, status, updated_at DESC) WHERE (primary_location_id IS NOT NULL);

--
-- Name: workforce_members_status_updated_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_members_status_updated_idx ON public.workforce_members USING btree (tenant_id, status, updated_at DESC, workforce_member_id DESC);

--
-- Name: workforce_members_user_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_members_user_idx ON public.workforce_members USING btree (tenant_id, user_id) WHERE (user_id IS NOT NULL);

--
-- Name: workforce_positions_active_seat_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX workforce_positions_active_seat_unique ON public.workforce_positions USING btree (tenant_id, scope_type, scope_id, position_code) WHERE (status = 'active'::text);

--
-- Name: workforce_positions_backup_group_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_positions_backup_group_idx ON public.workforce_positions USING btree (tenant_id, scope_type, scope_id, backup_group_code, is_backup_slot, status) WHERE (backup_group_code IS NOT NULL);

--
-- Name: workforce_positions_member_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_positions_member_idx ON public.workforce_positions USING btree (tenant_id, workforce_member_id, status);

--
-- Name: workforce_roster_assignments_member_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_roster_assignments_member_date_idx ON public.workforce_roster_assignments USING btree (tenant_id, workforce_member_id, shift_date, status);

--
-- Name: workforce_roster_assignments_scope_date_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX workforce_roster_assignments_scope_date_idx ON public.workforce_roster_assignments USING btree (tenant_id, shift_date, scope_type, scope_id, status);

--
-- Name: animal_stage_lookup admin_ui_animal_stages_config_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_animal_stages_config_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.animal_stage_lookup FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('config');

--
-- Name: animal_stage_lookup admin_ui_animal_stages_protocol_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_animal_stages_protocol_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.animal_stage_lookup FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('protocols:vaccination');

--
-- Name: animal_stage_lookup admin_ui_animal_stages_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_animal_stages_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.animal_stage_lookup FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('animal-stages');

--
-- Name: breeds admin_ui_breeds_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_breeds_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.breeds FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_global_family_trg('breeds');

--
-- Name: admin_ui_config_entries admin_ui_config_entries_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_config_entries_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.admin_ui_config_entries FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('admin-ui-config');

--
-- Name: admin_ui_config_family_change_queue admin_ui_config_family_change_queue_flush_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER admin_ui_config_family_change_queue_flush_trg AFTER INSERT OR UPDATE ON public.admin_ui_config_family_change_queue DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.admin_ui_flush_config_family_change_trg();

--
-- Name: inventory_items admin_ui_inventory_feed_items_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_inventory_feed_items_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.inventory_items FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_feed_item_family_trg();

--
-- Name: locations admin_ui_locations_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_locations_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.locations FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('locations');

--
-- Name: auth_pending_email_grants admin_ui_pending_email_grants_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_pending_email_grants_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.auth_pending_email_grants FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('permissions');

--
-- Name: protocol_definitions admin_ui_protocol_definitions_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_protocol_definitions_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.protocol_definitions FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_protocol_family_trg();

--
-- Name: protocol_rules admin_ui_protocol_rules_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_protocol_rules_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.protocol_rules FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_protocol_family_trg();

--
-- Name: protocol_triggers admin_ui_protocol_triggers_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_protocol_triggers_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.protocol_triggers FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_protocol_family_trg();

--
-- Name: protocol_versions admin_ui_protocol_versions_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_protocol_versions_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.protocol_versions FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_protocol_family_trg();

--
-- Name: sop_definitions admin_ui_sop_definitions_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_sop_definitions_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.sop_definitions FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_sop_family_trg();

--
-- Name: sop_versions admin_ui_sop_versions_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_sop_versions_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.sop_versions FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_sop_family_trg();

--
-- Name: status_definitions admin_ui_status_definitions_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_status_definitions_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.status_definitions FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_status_family_trg();

--
-- Name: user_scope_grants admin_ui_user_scope_grants_revision_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER admin_ui_user_scope_grants_revision_trg AFTER INSERT OR DELETE OR UPDATE ON public.user_scope_grants FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('permissions');

--
-- Name: counts_shifting_readiness_subgates counts_shifting_readiness_evidence_after_write; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER counts_shifting_readiness_evidence_after_write AFTER INSERT OR UPDATE ON public.counts_shifting_readiness_subgates FOR EACH ROW EXECUTE FUNCTION public.record_counts_shifting_readiness_evidence();

--
-- Name: farm_profiles farm_profiles_validate_type_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER farm_profiles_validate_type_trg BEFORE INSERT OR UPDATE OF tenant_id, location_id ON public.farm_profiles FOR EACH ROW EXECUTE FUNCTION public.validate_location_profile_type('farm');

--
-- Name: goat_custody_history goat_custody_history_block_merged_goat_child_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goat_custody_history_block_merged_goat_child_write_trg BEFORE INSERT OR UPDATE ON public.goat_custody_history FOR EACH ROW EXECUTE FUNCTION public.block_merged_goat_child_write();

--
-- Name: goat_identifiers goat_identifiers_block_merged_goat_child_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goat_identifiers_block_merged_goat_child_write_trg BEFORE INSERT OR UPDATE ON public.goat_identifiers FOR EACH ROW EXECUTE FUNCTION public.block_merged_goat_child_write();

--
-- Name: goat_identity_events goat_identity_events_block_merged_goat_child_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goat_identity_events_block_merged_goat_child_write_trg BEFORE INSERT OR UPDATE ON public.goat_identity_events FOR EACH ROW EXECUTE FUNCTION public.block_merged_goat_child_write();

--
-- Name: goat_location_history goat_location_history_block_merged_goat_child_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goat_location_history_block_merged_goat_child_write_trg BEFORE INSERT OR UPDATE ON public.goat_location_history FOR EACH ROW EXECUTE FUNCTION public.block_merged_goat_child_write();

--
-- Name: goat_merge_links goat_merge_links_validate_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goat_merge_links_validate_trg BEFORE INSERT OR UPDATE ON public.goat_merge_links FOR EACH ROW EXECUTE FUNCTION public.validate_goat_merge_link();

--
-- Name: goat_ownership goat_ownership_active_share_total_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER goat_ownership_active_share_total_trg AFTER INSERT OR DELETE OR UPDATE ON public.goat_ownership DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.check_goat_active_ownership_total();

--
-- Name: goat_ownership goat_ownership_block_merged_goat_child_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goat_ownership_block_merged_goat_child_write_trg BEFORE INSERT OR UPDATE ON public.goat_ownership FOR EACH ROW EXECUTE FUNCTION public.block_merged_goat_child_write();

--
-- Name: goats goats_prevent_hard_delete_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goats_prevent_hard_delete_trg BEFORE DELETE ON public.goats FOR EACH ROW EXECUTE FUNCTION public.prevent_goat_hard_delete();

--
-- Name: goats goats_prevent_merged_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER goats_prevent_merged_write_trg BEFORE UPDATE ON public.goats FOR EACH ROW EXECUTE FUNCTION public.prevent_merged_goat_normal_update();

--
-- Name: goats herd_register_goats_after_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER herd_register_goats_after_write_trg AFTER INSERT OR DELETE OR UPDATE OF tenant_id, goat_id, display_id, park_id, farm_id, current_location_id, breed, sex, lifecycle_status, age_band, management_stage, merged_into_goat_id ON public.goats FOR EACH ROW EXECUTE FUNCTION public.herd_register_goats_after_write_trg();

--
-- Name: goat_identifiers herd_register_identifiers_after_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER herd_register_identifiers_after_write_trg AFTER INSERT OR DELETE OR UPDATE OF tenant_id, goat_id, identifier_type, status ON public.goat_identifiers FOR EACH ROW EXECUTE FUNCTION public.herd_register_identifiers_after_write_trg();

--
-- Name: herd_register_goat_projection herd_register_projection_summary_after_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER herd_register_projection_summary_after_write_trg AFTER INSERT OR DELETE OR UPDATE OF park_id, farm_id, current_location_id, breed, sex, lifecycle_status, is_kid, is_untagged ON public.herd_register_goat_projection FOR EACH ROW EXECUTE FUNCTION public.herd_register_projection_summary_trg();

--
-- Name: identity_correction_requests identity_correction_requests_block_merged_goat_child_write_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER identity_correction_requests_block_merged_goat_child_write_trg BEFORE INSERT OR UPDATE ON public.identity_correction_requests FOR EACH ROW EXECUTE FUNCTION public.block_merged_goat_child_write();

--
-- Name: inventory_stock_movements inventory_stock_movements_batch_reserve_marker_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER inventory_stock_movements_batch_reserve_marker_trg AFTER INSERT ON public.inventory_stock_movements FOR EACH ROW EXECUTE FUNCTION public.mark_obligation_batch_stock_reservation_from_movement();

--
-- Name: location_aliases location_aliases_seeded_scope_guard_delete_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER location_aliases_seeded_scope_guard_delete_trg BEFORE DELETE ON public.location_aliases FOR EACH ROW EXECUTE FUNCTION public.location_seeded_scope_guard();

--
-- Name: location_aliases location_aliases_seeded_scope_guard_update_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER location_aliases_seeded_scope_guard_update_trg BEFORE UPDATE OF alias_code, canonical_location_id, source_context, status, retired_at ON public.location_aliases FOR EACH ROW EXECUTE FUNCTION public.location_seeded_scope_guard();

--
-- Name: location_capacity_records location_capacity_records_no_overlap_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER location_capacity_records_no_overlap_trg BEFORE INSERT OR UPDATE OF tenant_id, location_id, capacity_kind, effective_from, effective_to ON public.location_capacity_records FOR EACH ROW EXECUTE FUNCTION public.reject_overlapping_location_capacity();

--
-- Name: locations locations_seeded_scope_guard_delete_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER locations_seeded_scope_guard_delete_trg BEFORE DELETE ON public.locations FOR EACH ROW EXECUTE FUNCTION public.location_seeded_scope_guard();

--
-- Name: locations locations_seeded_scope_guard_update_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER locations_seeded_scope_guard_update_trg BEFORE UPDATE OF location_type, location_code, name, parent_location_id, status, retired_at, retired_by ON public.locations FOR EACH ROW EXECUTE FUNCTION public.location_seeded_scope_guard();

--
-- Name: obligation_batches obligation_batches_validate_scope_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER obligation_batches_validate_scope_trg BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON public.obligation_batches FOR EACH ROW EXECUTE FUNCTION public.validate_obligation_scope();

--
-- Name: obligation_instances obligation_instances_procurement_vaccination_guard_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER obligation_instances_procurement_vaccination_guard_trg BEFORE INSERT OR UPDATE OF target_type, target_id, status, protocol_version_id ON public.obligation_instances FOR EACH ROW EXECUTE FUNCTION public.block_active_vaccination_for_procurement_excluded_goat();

--
-- Name: obligation_instances obligation_instances_validate_scope_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER obligation_instances_validate_scope_trg BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON public.obligation_instances FOR EACH ROW EXECUTE FUNCTION public.validate_obligation_scope();

--
-- Name: obligation_instances obligation_instances_validate_target_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER obligation_instances_validate_target_trg BEFORE INSERT OR UPDATE OF tenant_id, target_type, target_id ON public.obligation_instances FOR EACH ROW EXECUTE FUNCTION public.validate_obligation_target();

--
-- Name: outbox_messages outbox_messages_validate_event_tenant_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER outbox_messages_validate_event_tenant_trg BEFORE INSERT OR UPDATE OF tenant_id, event_id ON public.outbox_messages FOR EACH ROW EXECUTE FUNCTION public.validate_outbox_event_tenant();

--
-- Name: park_profiles park_profiles_validate_type_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER park_profiles_validate_type_trg BEFORE INSERT OR UPDATE OF tenant_id, location_id ON public.park_profiles FOR EACH ROW EXECUTE FUNCTION public.validate_location_profile_type('park');

--
-- Name: protocol_rules protocol_rules_require_draft_version_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER protocol_rules_require_draft_version_trg BEFORE INSERT OR DELETE OR UPDATE ON public.protocol_rules FOR EACH ROW EXECUTE FUNCTION public.ensure_protocol_child_version_is_draft();

--
-- Name: protocol_triggers protocol_triggers_require_draft_version_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER protocol_triggers_require_draft_version_trg BEFORE INSERT OR DELETE OR UPDATE ON public.protocol_triggers FOR EACH ROW EXECUTE FUNCTION public.ensure_protocol_child_version_is_draft();

--
-- Name: protocol_versions protocol_versions_published_immutable_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER protocol_versions_published_immutable_trg BEFORE UPDATE ON public.protocol_versions FOR EACH ROW EXECUTE FUNCTION public.ensure_published_protocol_version_is_immutable();

--
-- Name: protocol_versions protocol_versions_published_no_delete_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER protocol_versions_published_no_delete_trg BEFORE DELETE ON public.protocol_versions FOR EACH ROW EXECUTE FUNCTION public.prevent_published_protocol_version_delete();

--
-- Name: protocol_versions protocol_versions_validate_scope_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER protocol_versions_validate_scope_trg BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON public.protocol_versions FOR EACH ROW EXECUTE FUNCTION public.validate_protocol_version_scope();

--
-- Name: shed_profiles shed_profiles_validate_type_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER shed_profiles_validate_type_trg BEFORE INSERT OR UPDATE OF tenant_id, location_id ON public.shed_profiles FOR EACH ROW EXECUTE FUNCTION public.validate_location_profile_type('shed');

--
-- Name: user_scope_grants user_scope_grants_validate_scope_trg; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER user_scope_grants_validate_scope_trg BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON public.user_scope_grants FOR EACH ROW EXECUTE FUNCTION public.validate_user_scope_grant();

--
-- Name: verification_items verification_items_touch_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER verification_items_touch_updated_at BEFORE UPDATE ON public.verification_items FOR EACH ROW EXECUTE FUNCTION public.verification_items_touch_updated_at();

--
-- Name: admin_ui_config_entries admin_ui_config_entries_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_ui_config_entries
    ADD CONSTRAINT admin_ui_config_entries_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: admin_ui_config_family_change_queue admin_ui_config_family_change_queue_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_ui_config_family_change_queue
    ADD CONSTRAINT admin_ui_config_family_change_queue_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: admin_ui_config_family_revisions admin_ui_config_family_revisions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_ui_config_family_revisions
    ADD CONSTRAINT admin_ui_config_family_revisions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: animal_stage_lookup animal_stage_lookup_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.animal_stage_lookup
    ADD CONSTRAINT animal_stage_lookup_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_proof_ref_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_proof_ref_id_fkey FOREIGN KEY (proof_ref_id) REFERENCES public.proof_artifacts(proof_id);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_review_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_review_tenant_fk FOREIGN KEY (tenant_id, review_id) REFERENCES public.arrival_intake_reviews(tenant_id, review_id);

--
-- Name: arrival_intake_review_goats arrival_intake_review_goats_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_review_goats
    ADD CONSTRAINT arrival_intake_review_goats_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_media_proof_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_media_proof_id_fkey FOREIGN KEY (media_proof_id) REFERENCES public.proof_artifacts(proof_id);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_park_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_park_tenant_fk FOREIGN KEY (tenant_id, park_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: arrival_intake_reviews arrival_intake_reviews_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.arrival_intake_reviews
    ADD CONSTRAINT arrival_intake_reviews_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: audit_log audit_log_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: audit_log audit_log_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: audit_log audit_log_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: auth_pending_email_grants auth_pending_email_grants_role_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_pending_email_grants
    ADD CONSTRAINT auth_pending_email_grants_role_fk FOREIGN KEY (role) REFERENCES public.org_role_catalog(role_key);

--
-- Name: auth_pending_email_grants auth_pending_email_grants_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auth_pending_email_grants
    ADD CONSTRAINT auth_pending_email_grants_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: breed_aliases breed_aliases_breed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.breed_aliases
    ADD CONSTRAINT breed_aliases_breed_id_fkey FOREIGN KEY (breed_id) REFERENCES public.breeds(breed_id);

--
-- Name: bulk_status_job_row bulk_status_job_row_job_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.bulk_status_job_row
    ADD CONSTRAINT bulk_status_job_row_job_id_fkey FOREIGN KEY (job_id) REFERENCES public.bulk_status_job(bulk_status_job_id) ON DELETE CASCADE;

--
-- Name: calendar_snoozes calendar_snoozes_replaced_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calendar_snoozes
    ADD CONSTRAINT calendar_snoozes_replaced_fk FOREIGN KEY (replaced_by_snooze_id) REFERENCES public.calendar_snoozes(snooze_id);

--
-- Name: calendar_snoozes calendar_snoozes_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calendar_snoozes
    ADD CONSTRAINT calendar_snoozes_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_base_anchors count_base_anchors_breed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_breed_id_fkey FOREIGN KEY (breed_id) REFERENCES public.breeds(breed_id);

--
-- Name: count_base_anchors count_base_anchors_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: count_base_anchors count_base_anchors_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_shed_id_fkey FOREIGN KEY (shed_id) REFERENCES public.locations(location_id);

--
-- Name: count_base_anchors count_base_anchors_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_base_anchors count_base_anchors_tenant_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_base_anchors count_base_anchors_tenant_shed_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_base_anchors
    ADD CONSTRAINT count_base_anchors_tenant_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_dimension_aliases count_dimension_aliases_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_dimension_aliases
    ADD CONSTRAINT count_dimension_aliases_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_mismatch_scan_runs count_mismatch_scan_runs_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_mismatch_scan_runs
    ADD CONSTRAINT count_mismatch_scan_runs_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: count_mismatch_scan_runs count_mismatch_scan_runs_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_mismatch_scan_runs
    ADD CONSTRAINT count_mismatch_scan_runs_shed_id_fkey FOREIGN KEY (shed_id) REFERENCES public.locations(location_id);

--
-- Name: count_mismatch_scan_runs count_mismatch_scan_runs_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_mismatch_scan_runs
    ADD CONSTRAINT count_mismatch_scan_runs_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_mismatch_scan_runs count_mismatch_scan_runs_tenant_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_mismatch_scan_runs
    ADD CONSTRAINT count_mismatch_scan_runs_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_mismatch_scan_runs count_mismatch_scan_runs_tenant_shed_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_mismatch_scan_runs
    ADD CONSTRAINT count_mismatch_scan_runs_tenant_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_projection_exception_resolutions count_projection_exception_re_count_projection_exception_i_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exception_resolutions
    ADD CONSTRAINT count_projection_exception_re_count_projection_exception_i_fkey FOREIGN KEY (count_projection_exception_id) REFERENCES public.count_projection_exceptions(count_projection_exception_id) ON DELETE CASCADE;

--
-- Name: count_projection_exception_resolutions count_projection_exception_resolutions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exception_resolutions
    ADD CONSTRAINT count_projection_exception_resolutions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_count_projection_snapshot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_count_projection_snapshot_id_fkey FOREIGN KEY (count_projection_snapshot_id) REFERENCES public.count_projection_snapshots(count_projection_snapshot_id) ON DELETE SET NULL;

--
-- Name: count_projection_exceptions count_projection_exceptions_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_resolution_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_resolution_id_fkey FOREIGN KEY (resolution_id) REFERENCES public.count_projection_exception_resolutions(count_projection_exception_resolution_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_shed_id_fkey FOREIGN KEY (shed_id) REFERENCES public.locations(location_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_tenant_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_tenant_shed_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_tenant_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_projection_exceptions count_projection_exceptions_tenant_snapshot_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_exceptions
    ADD CONSTRAINT count_projection_exceptions_tenant_snapshot_fk FOREIGN KEY (tenant_id, count_projection_snapshot_id) REFERENCES public.count_projection_snapshots(tenant_id, count_projection_snapshot_id) ON DELETE SET NULL;

--
-- Name: count_projection_recompute_runs count_projection_recompute_runs_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_recompute_runs
    ADD CONSTRAINT count_projection_recompute_runs_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: count_projection_recompute_runs count_projection_recompute_runs_snapshot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_recompute_runs
    ADD CONSTRAINT count_projection_recompute_runs_snapshot_id_fkey FOREIGN KEY (snapshot_id) REFERENCES public.count_projection_snapshots(count_projection_snapshot_id) ON DELETE SET NULL;

--
-- Name: count_projection_recompute_runs count_projection_recompute_runs_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_recompute_runs
    ADD CONSTRAINT count_projection_recompute_runs_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_row_count_projection_snapshot_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_row_count_projection_snapshot_id_fkey FOREIGN KEY (count_projection_snapshot_id) REFERENCES public.count_projection_snapshots(count_projection_snapshot_id) ON DELETE CASCADE;

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_breed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_breed_id_fkey FOREIGN KEY (breed_id) REFERENCES public.breeds(breed_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_shed_id_fkey FOREIGN KEY (shed_id) REFERENCES public.locations(location_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_tenant_anchor_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_tenant_anchor_fk FOREIGN KEY (tenant_id, base_count_anchor_id) REFERENCES public.count_base_anchors(tenant_id, base_count_anchor_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_tenant_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_tenant_shed_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_tenant_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_projection_snapshot_rows count_projection_snapshot_rows_tenant_snapshot_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshot_rows
    ADD CONSTRAINT count_projection_snapshot_rows_tenant_snapshot_fk FOREIGN KEY (tenant_id, count_projection_snapshot_id) REFERENCES public.count_projection_snapshots(tenant_id, count_projection_snapshot_id) ON DELETE CASCADE;

--
-- Name: count_projection_snapshots count_projection_snapshots_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshots
    ADD CONSTRAINT count_projection_snapshots_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: count_projection_snapshots count_projection_snapshots_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshots
    ADD CONSTRAINT count_projection_snapshots_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: count_projection_snapshots count_projection_snapshots_tenant_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_projection_snapshots
    ADD CONSTRAINT count_projection_snapshots_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: count_source_import_runs count_source_import_runs_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.count_source_import_runs
    ADD CONSTRAINT count_source_import_runs_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: counts_shifting_readiness_evidence counts_shifting_readiness_evidence_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.counts_shifting_readiness_evidence
    ADD CONSTRAINT counts_shifting_readiness_evidence_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: counts_shifting_readiness_subgates counts_shifting_readiness_subgates_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.counts_shifting_readiness_subgates
    ADD CONSTRAINT counts_shifting_readiness_subgates_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: departments departments_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.departments
    ADD CONSTRAINT departments_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: domain_event_processed_events domain_event_processed_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.domain_event_processed_events
    ADD CONSTRAINT domain_event_processed_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: farm_profiles farm_profiles_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.farm_profiles
    ADD CONSTRAINT farm_profiles_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.locations(location_id);

--
-- Name: farm_profiles farm_profiles_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.farm_profiles
    ADD CONSTRAINT farm_profiles_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: farm_profiles farm_profiles_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.farm_profiles
    ADD CONSTRAINT farm_profiles_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: feed_direction_completions feed_direction_completions_batch_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES public.obligation_batches(tenant_id, batch_id);

--
-- Name: feed_direction_completions feed_direction_completions_lot_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_lot_tenant_fk FOREIGN KEY (tenant_id, feed_inventory_lot_id) REFERENCES public.inventory_stock(tenant_id, stock_id);

--
-- Name: feed_direction_completions feed_direction_completions_obligation_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES public.obligation_instances(tenant_id, obligation_id);

--
-- Name: feed_direction_completions feed_direction_completions_shed_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: feed_direction_completions feed_direction_completions_submission_item_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_submission_item_tenant_fk FOREIGN KEY (tenant_id, sop_submission_item_id) REFERENCES public.sop_submission_items(tenant_id, item_id);

--
-- Name: feed_direction_completions feed_direction_completions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.feed_direction_completions
    ADD CONSTRAINT feed_direction_completions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goat_custody_history goat_custody_history_custodian_party_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_custodian_party_id_fkey FOREIGN KEY (custodian_party_id) REFERENCES public.parties(party_id);

--
-- Name: goat_custody_history goat_custody_history_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: goat_custody_history goat_custody_history_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: goat_custody_history goat_custody_history_from_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_from_location_id_fkey FOREIGN KEY (from_location_id) REFERENCES public.locations(location_id);

--
-- Name: goat_custody_history goat_custody_history_from_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_from_location_tenant_fk FOREIGN KEY (tenant_id, from_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goat_custody_history goat_custody_history_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_custody_history goat_custody_history_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_custody_history goat_custody_history_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goat_custody_history goat_custody_history_to_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_to_location_id_fkey FOREIGN KEY (to_location_id) REFERENCES public.locations(location_id);

--
-- Name: goat_custody_history goat_custody_history_to_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_custody_history
    ADD CONSTRAINT goat_custody_history_to_location_tenant_fk FOREIGN KEY (tenant_id, to_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goat_identifiers goat_identifiers_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identifiers
    ADD CONSTRAINT goat_identifiers_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_identifiers goat_identifiers_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identifiers
    ADD CONSTRAINT goat_identifiers_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_identifiers goat_identifiers_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identifiers
    ADD CONSTRAINT goat_identifiers_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goat_identity_events goat_identity_events_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: goat_identity_events goat_identity_events_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: goat_identity_events goat_identity_events_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_identity_events goat_identity_events_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_identity_events goat_identity_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_identity_events
    ADD CONSTRAINT goat_identity_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goat_location_history goat_location_history_from_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_from_location_id_fkey FOREIGN KEY (from_location_id) REFERENCES public.locations(location_id);

--
-- Name: goat_location_history goat_location_history_from_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_from_location_tenant_fk FOREIGN KEY (tenant_id, from_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goat_location_history goat_location_history_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_location_history goat_location_history_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_location_history goat_location_history_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goat_location_history goat_location_history_to_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_to_location_id_fkey FOREIGN KEY (to_location_id) REFERENCES public.locations(location_id);

--
-- Name: goat_location_history goat_location_history_to_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_location_history
    ADD CONSTRAINT goat_location_history_to_location_tenant_fk FOREIGN KEY (tenant_id, to_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goat_merge_links goat_merge_links_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: goat_merge_links goat_merge_links_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: goat_merge_links goat_merge_links_merged_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_merged_goat_id_fkey FOREIGN KEY (merged_goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_merge_links goat_merge_links_merged_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_merged_tenant_fk FOREIGN KEY (tenant_id, merged_goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_merge_links goat_merge_links_survivor_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_survivor_goat_id_fkey FOREIGN KEY (survivor_goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_merge_links goat_merge_links_survivor_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_survivor_tenant_fk FOREIGN KEY (tenant_id, survivor_goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_merge_links goat_merge_links_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_merge_links
    ADD CONSTRAINT goat_merge_links_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goat_ownership goat_ownership_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: goat_ownership goat_ownership_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: goat_ownership goat_ownership_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goat_ownership goat_ownership_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goat_ownership goat_ownership_owner_party_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_owner_party_id_fkey FOREIGN KEY (owner_party_id) REFERENCES public.parties(party_id);

--
-- Name: goat_ownership goat_ownership_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goat_ownership
    ADD CONSTRAINT goat_ownership_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: goats goats_breed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_breed_id_fkey FOREIGN KEY (breed_id) REFERENCES public.breeds(breed_id);

--
-- Name: goats goats_cohort_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_cohort_id_fkey FOREIGN KEY (cohort_id) REFERENCES public.locations(location_id);

--
-- Name: goats goats_cohort_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_cohort_tenant_fk FOREIGN KEY (tenant_id, cohort_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goats goats_current_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_current_location_id_fkey FOREIGN KEY (current_location_id) REFERENCES public.locations(location_id);

--
-- Name: goats goats_current_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_current_location_tenant_fk FOREIGN KEY (tenant_id, current_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goats goats_custodian_party_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_custodian_party_id_fkey FOREIGN KEY (custodian_party_id) REFERENCES public.parties(party_id);

--
-- Name: goats goats_farm_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_farm_id_fkey FOREIGN KEY (farm_id) REFERENCES public.locations(location_id);

--
-- Name: goats goats_farm_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_farm_tenant_fk FOREIGN KEY (tenant_id, farm_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goats goats_merged_into_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_merged_into_goat_id_fkey FOREIGN KEY (merged_into_goat_id) REFERENCES public.goats(goat_id);

--
-- Name: goats goats_merged_into_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_merged_into_tenant_fk FOREIGN KEY (tenant_id, merged_into_goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: goats goats_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: goats goats_park_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_park_tenant_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goats goats_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_shed_id_fkey FOREIGN KEY (shed_id) REFERENCES public.locations(location_id);

--
-- Name: goats goats_shed_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: goats goats_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goats
    ADD CONSTRAINT goats_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: herd_register_goat_projection herd_register_goat_projection_goat_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.herd_register_goat_projection
    ADD CONSTRAINT herd_register_goat_projection_goat_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id) ON DELETE CASCADE;

--
-- Name: herd_register_summary_projection herd_register_summary_projection_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.herd_register_summary_projection
    ADD CONSTRAINT herd_register_summary_projection_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: idempotency_keys idempotency_keys_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identifier_policies identifier_policies_version_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identifier_policies
    ADD CONSTRAINT identifier_policies_version_fk FOREIGN KEY (policy_version) REFERENCES public.identifier_policy_versions(policy_version);

--
-- Name: identity_conflict_goats identity_conflict_goats_conflict_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_goats
    ADD CONSTRAINT identity_conflict_goats_conflict_id_fkey FOREIGN KEY (conflict_id) REFERENCES public.identity_conflicts(conflict_id) ON DELETE CASCADE;

--
-- Name: identity_conflict_goats identity_conflict_goats_conflict_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_goats
    ADD CONSTRAINT identity_conflict_goats_conflict_tenant_fk FOREIGN KEY (tenant_id, conflict_id) REFERENCES public.identity_conflicts(tenant_id, conflict_id) ON DELETE CASCADE;

--
-- Name: identity_conflict_goats identity_conflict_goats_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_goats
    ADD CONSTRAINT identity_conflict_goats_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: identity_conflict_goats identity_conflict_goats_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_goats
    ADD CONSTRAINT identity_conflict_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: identity_conflict_goats identity_conflict_goats_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_goats
    ADD CONSTRAINT identity_conflict_goats_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_conflict_source_records identity_conflict_source_records_conflict_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_source_records
    ADD CONSTRAINT identity_conflict_source_records_conflict_id_fkey FOREIGN KEY (conflict_id) REFERENCES public.identity_conflicts(conflict_id) ON DELETE CASCADE;

--
-- Name: identity_conflict_source_records identity_conflict_source_records_conflict_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_source_records
    ADD CONSTRAINT identity_conflict_source_records_conflict_tenant_fk FOREIGN KEY (tenant_id, conflict_id) REFERENCES public.identity_conflicts(tenant_id, conflict_id) ON DELETE CASCADE;

--
-- Name: identity_conflict_source_records identity_conflict_source_records_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflict_source_records
    ADD CONSTRAINT identity_conflict_source_records_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_conflicts identity_conflicts_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflicts
    ADD CONSTRAINT identity_conflicts_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: identity_conflicts identity_conflicts_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflicts
    ADD CONSTRAINT identity_conflicts_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: identity_conflicts identity_conflicts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_conflicts
    ADD CONSTRAINT identity_conflicts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_correction_requests identity_correction_requests_cohort_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_cohort_id_fkey FOREIGN KEY (cohort_id) REFERENCES public.locations(location_id);

--
-- Name: identity_correction_requests identity_correction_requests_cohort_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_cohort_tenant_fk FOREIGN KEY (tenant_id, cohort_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: identity_correction_requests identity_correction_requests_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id);

--
-- Name: identity_correction_requests identity_correction_requests_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id);

--
-- Name: identity_correction_requests identity_correction_requests_farm_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_farm_id_fkey FOREIGN KEY (farm_id) REFERENCES public.locations(location_id);

--
-- Name: identity_correction_requests identity_correction_requests_farm_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_farm_tenant_fk FOREIGN KEY (tenant_id, farm_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: identity_correction_requests identity_correction_requests_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: identity_correction_requests identity_correction_requests_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: identity_correction_requests identity_correction_requests_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_park_id_fkey FOREIGN KEY (park_id) REFERENCES public.locations(location_id);

--
-- Name: identity_correction_requests identity_correction_requests_park_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_park_tenant_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: identity_correction_requests identity_correction_requests_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_shed_id_fkey FOREIGN KEY (shed_id) REFERENCES public.locations(location_id);

--
-- Name: identity_correction_requests identity_correction_requests_shed_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_shed_tenant_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: identity_correction_requests identity_correction_requests_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_correction_requests
    ADD CONSTRAINT identity_correction_requests_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_decision_events identity_decision_events_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_events
    ADD CONSTRAINT identity_decision_events_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_events identity_decision_events_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_events
    ADD CONSTRAINT identity_decision_events_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_events identity_decision_events_event_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_events
    ADD CONSTRAINT identity_decision_events_event_fk FOREIGN KEY (event_id, event_recorded_at) REFERENCES public.goat_identity_events(identity_event_id, recorded_at);

--
-- Name: identity_decision_events identity_decision_events_event_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_events
    ADD CONSTRAINT identity_decision_events_event_tenant_fk FOREIGN KEY (tenant_id, event_id, event_recorded_at) REFERENCES public.goat_identity_events(tenant_id, identity_event_id, recorded_at);

--
-- Name: identity_decision_events identity_decision_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_events
    ADD CONSTRAINT identity_decision_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_decision_goats identity_decision_goats_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_goats
    ADD CONSTRAINT identity_decision_goats_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_goats identity_decision_goats_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_goats
    ADD CONSTRAINT identity_decision_goats_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_goats identity_decision_goats_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_goats
    ADD CONSTRAINT identity_decision_goats_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: identity_decision_goats identity_decision_goats_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_goats
    ADD CONSTRAINT identity_decision_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: identity_decision_goats identity_decision_goats_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_goats
    ADD CONSTRAINT identity_decision_goats_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_decision_identifiers identity_decision_identifiers_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_identifiers
    ADD CONSTRAINT identity_decision_identifiers_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_identifiers identity_decision_identifiers_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_identifiers
    ADD CONSTRAINT identity_decision_identifiers_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_identifiers identity_decision_identifiers_identifier_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_identifiers
    ADD CONSTRAINT identity_decision_identifiers_identifier_id_fkey FOREIGN KEY (identifier_id) REFERENCES public.goat_identifiers(identifier_id);

--
-- Name: identity_decision_identifiers identity_decision_identifiers_identifier_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_identifiers
    ADD CONSTRAINT identity_decision_identifiers_identifier_tenant_fk FOREIGN KEY (tenant_id, identifier_id) REFERENCES public.goat_identifiers(tenant_id, identifier_id);

--
-- Name: identity_decision_identifiers identity_decision_identifiers_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_identifiers
    ADD CONSTRAINT identity_decision_identifiers_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_decision_media identity_decision_media_decision_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_media
    ADD CONSTRAINT identity_decision_media_decision_id_fkey FOREIGN KEY (decision_id) REFERENCES public.identity_decisions(decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_media identity_decision_media_decision_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_media
    ADD CONSTRAINT identity_decision_media_decision_tenant_fk FOREIGN KEY (tenant_id, decision_id) REFERENCES public.identity_decisions(tenant_id, decision_id) ON DELETE CASCADE;

--
-- Name: identity_decision_media identity_decision_media_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decision_media
    ADD CONSTRAINT identity_decision_media_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: identity_decisions identity_decisions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.identity_decisions
    ADD CONSTRAINT identity_decisions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: inventory_items inventory_items_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_items
    ADD CONSTRAINT inventory_items_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: inventory_stock inventory_stock_item_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock
    ADD CONSTRAINT inventory_stock_item_tenant_fk FOREIGN KEY (tenant_id, item_id) REFERENCES public.inventory_items(tenant_id, item_id);

--
-- Name: inventory_stock inventory_stock_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock
    ADD CONSTRAINT inventory_stock_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: inventory_stock_movements inventory_stock_movements_batch_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES public.obligation_batches(tenant_id, batch_id);

--
-- Name: inventory_stock_movements inventory_stock_movements_lot_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_lot_tenant_fk FOREIGN KEY (tenant_id, lot_id, item_id, location_id) REFERENCES public.inventory_stock(tenant_id, stock_id, item_id, location_id);

--
-- Name: inventory_stock_movements inventory_stock_movements_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: inventory_stock inventory_stock_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock
    ADD CONSTRAINT inventory_stock_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: location_aliases location_aliases_canonical_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_aliases
    ADD CONSTRAINT location_aliases_canonical_location_id_fkey FOREIGN KEY (canonical_location_id) REFERENCES public.locations(location_id);

--
-- Name: location_aliases location_aliases_canonical_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_aliases
    ADD CONSTRAINT location_aliases_canonical_location_tenant_fk FOREIGN KEY (tenant_id, canonical_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: location_aliases location_aliases_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_aliases
    ADD CONSTRAINT location_aliases_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: location_capacity_records location_capacity_records_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_capacity_records
    ADD CONSTRAINT location_capacity_records_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.locations(location_id);

--
-- Name: location_capacity_records location_capacity_records_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_capacity_records
    ADD CONSTRAINT location_capacity_records_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: location_capacity_records location_capacity_records_tenant_location_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_capacity_records
    ADD CONSTRAINT location_capacity_records_tenant_location_fk FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: location_operational_attributes location_operational_attributes_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_operational_attributes
    ADD CONSTRAINT location_operational_attributes_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.locations(location_id) ON DELETE CASCADE;

--
-- Name: location_operational_attributes location_operational_attributes_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_operational_attributes
    ADD CONSTRAINT location_operational_attributes_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: location_operational_attributes location_operational_attributes_tenant_location_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_operational_attributes
    ADD CONSTRAINT location_operational_attributes_tenant_location_fk FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: location_projection_invalidations location_projection_invalidations_affected_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_projection_invalidations
    ADD CONSTRAINT location_projection_invalidations_affected_location_id_fkey FOREIGN KEY (affected_location_id) REFERENCES public.locations(location_id);

--
-- Name: location_projection_invalidations location_projection_invalidations_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_projection_invalidations
    ADD CONSTRAINT location_projection_invalidations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: location_review_items location_review_items_canonical_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_review_items
    ADD CONSTRAINT location_review_items_canonical_location_id_fkey FOREIGN KEY (canonical_location_id) REFERENCES public.locations(location_id);

--
-- Name: location_review_items location_review_items_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.location_review_items
    ADD CONSTRAINT location_review_items_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: locations locations_parent_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.locations
    ADD CONSTRAINT locations_parent_location_id_fkey FOREIGN KEY (parent_location_id) REFERENCES public.locations(location_id);

--
-- Name: locations locations_parent_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.locations
    ADD CONSTRAINT locations_parent_location_tenant_fk FOREIGN KEY (tenant_id, parent_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: locations locations_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.locations
    ADD CONSTRAINT locations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: movement_commands movement_commands_submission_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.movement_commands
    ADD CONSTRAINT movement_commands_submission_id_fkey FOREIGN KEY (submission_id) REFERENCES public.sop_submissions(submission_id);

--
-- Name: movement_commands movement_commands_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.movement_commands
    ADD CONSTRAINT movement_commands_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: movement_commands movement_commands_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.movement_commands
    ADD CONSTRAINT movement_commands_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: notification_delivery_attempts notification_delivery_attempts_notification_request_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_delivery_attempts
    ADD CONSTRAINT notification_delivery_attempts_notification_request_id_fkey FOREIGN KEY (notification_request_id) REFERENCES public.notification_requests(notification_request_id);

--
-- Name: notification_delivery_attempts notification_delivery_attempts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_delivery_attempts
    ADD CONSTRAINT notification_delivery_attempts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: notification_requests notification_requests_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.notification_requests
    ADD CONSTRAINT notification_requests_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: obligation_batches obligation_batches_lot_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_batches
    ADD CONSTRAINT obligation_batches_lot_tenant_fk FOREIGN KEY (tenant_id, primary_inventory_lot_id) REFERENCES public.inventory_stock(tenant_id, stock_id);

--
-- Name: obligation_batches obligation_batches_sop_task_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_batches
    ADD CONSTRAINT obligation_batches_sop_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES public.sop_tasks(tenant_id, task_id);

--
-- Name: obligation_batches obligation_batches_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_batches
    ADD CONSTRAINT obligation_batches_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: obligation_batches obligation_batches_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_batches
    ADD CONSTRAINT obligation_batches_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES public.protocol_versions(tenant_id, protocol_version_id);

--
-- Name: obligation_escalations obligation_escalations_obligation_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_escalations
    ADD CONSTRAINT obligation_escalations_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES public.obligation_instances(tenant_id, obligation_id);

--
-- Name: obligation_escalations obligation_escalations_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_escalations
    ADD CONSTRAINT obligation_escalations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: obligation_goat_shift_watermarks obligation_goat_shift_watermarks_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_goat_shift_watermarks
    ADD CONSTRAINT obligation_goat_shift_watermarks_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: obligation_goat_shift_watermarks obligation_goat_shift_watermarks_last_scope_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_goat_shift_watermarks
    ADD CONSTRAINT obligation_goat_shift_watermarks_last_scope_id_fkey FOREIGN KEY (last_scope_id) REFERENCES public.locations(location_id);

--
-- Name: obligation_goat_shift_watermarks obligation_goat_shift_watermarks_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_goat_shift_watermarks
    ADD CONSTRAINT obligation_goat_shift_watermarks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: obligation_instances obligation_instances_batch_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES public.obligation_batches(tenant_id, batch_id);

--
-- Name: obligation_instances obligation_instances_rule_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_rule_tenant_fk FOREIGN KEY (tenant_id, rule_id) REFERENCES public.protocol_rules(tenant_id, rule_id);

--
-- Name: obligation_instances obligation_instances_sop_task_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_sop_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES public.sop_tasks(tenant_id, task_id);

--
-- Name: obligation_instances obligation_instances_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: obligation_instances obligation_instances_trigger_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_trigger_tenant_fk FOREIGN KEY (tenant_id, generated_by_trigger_id) REFERENCES public.protocol_triggers(tenant_id, trigger_id);

--
-- Name: obligation_instances obligation_instances_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_instances
    ADD CONSTRAINT obligation_instances_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES public.protocol_versions(tenant_id, protocol_version_id);

--
-- Name: obligation_status_events obligation_status_events_obligation_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_status_events
    ADD CONSTRAINT obligation_status_events_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES public.obligation_instances(tenant_id, obligation_id);

--
-- Name: obligation_status_events obligation_status_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.obligation_status_events
    ADD CONSTRAINT obligation_status_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: org_role_catalog org_role_catalog_tier_code_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_role_catalog
    ADD CONSTRAINT org_role_catalog_tier_code_fkey FOREIGN KEY (tier_code) REFERENCES public.org_tiers(tier_code);

--
-- Name: org_role_catalog org_role_catalog_vertical_code_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.org_role_catalog
    ADD CONSTRAINT org_role_catalog_vertical_code_fkey FOREIGN KEY (vertical_code) REFERENCES public.org_verticals(vertical_code);

--
-- Name: orgs orgs_party_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orgs
    ADD CONSTRAINT orgs_party_id_fkey FOREIGN KEY (party_id) REFERENCES public.parties(party_id);

--
-- Name: outbox_dlq_actions outbox_dlq_actions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_dlq_actions
    ADD CONSTRAINT outbox_dlq_actions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: outbox_messages outbox_messages_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outbox_messages
    ADD CONSTRAINT outbox_messages_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: park_profiles park_profiles_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.park_profiles
    ADD CONSTRAINT park_profiles_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.locations(location_id);

--
-- Name: park_profiles park_profiles_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.park_profiles
    ADD CONSTRAINT park_profiles_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: park_profiles park_profiles_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.park_profiles
    ADD CONSTRAINT park_profiles_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_proof_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_proof_fk FOREIGN KEY (proof_ref_id) REFERENCES public.proof_artifacts(proof_id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_protocol_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_protocol_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES public.protocol_versions(tenant_id, protocol_version_id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_rule_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_rule_tenant_fk FOREIGN KEY (tenant_id, rule_id) REFERENCES public.protocol_rules(tenant_id, rule_id);

--
-- Name: procurement_hf_vaccination_evidence procurement_hf_vaccination_evidence_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_hf_vaccination_evidence
    ADD CONSTRAINT procurement_hf_vaccination_evidence_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: procurement_load_goats procurement_load_goats_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_load_goats
    ADD CONSTRAINT procurement_load_goats_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: procurement_load_goats procurement_load_goats_holding_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_load_goats
    ADD CONSTRAINT procurement_load_goats_holding_location_tenant_fk FOREIGN KEY (tenant_id, holding_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: procurement_load_goats procurement_load_goats_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_load_goats
    ADD CONSTRAINT procurement_load_goats_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: procurement_load_goats procurement_load_goats_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_load_goats
    ADD CONSTRAINT procurement_load_goats_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: procurement_loads procurement_loads_source_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_source_location_id_fkey FOREIGN KEY (source_location_id) REFERENCES public.locations(location_id);

--
-- Name: procurement_loads procurement_loads_source_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_source_location_tenant_fk FOREIGN KEY (tenant_id, source_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: procurement_loads procurement_loads_source_party_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_source_party_id_fkey FOREIGN KEY (source_party_id) REFERENCES public.parties(party_id);

--
-- Name: procurement_loads procurement_loads_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_loads
    ADD CONSTRAINT procurement_loads_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_park_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_park_tenant_fk FOREIGN KEY (tenant_id, park_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_shed_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_shed_tenant_fk FOREIGN KEY (tenant_id, shed_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: procurement_pc_handoffs procurement_pc_handoffs_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_pc_handoffs
    ADD CONSTRAINT procurement_pc_handoffs_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_proof_ref_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_proof_ref_id_fkey FOREIGN KEY (proof_ref_id) REFERENCES public.proof_artifacts(proof_id);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_task_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES public.sop_tasks(tenant_id, task_id);

--
-- Name: procurement_source_health_checks procurement_source_health_checks_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.procurement_source_health_checks
    ADD CONSTRAINT procurement_source_health_checks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: proof_artifacts proof_artifacts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proof_artifacts
    ADD CONSTRAINT proof_artifacts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: protocol_definitions protocol_definitions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_definitions
    ADD CONSTRAINT protocol_definitions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: protocol_rule_dimensions protocol_rule_dimensions_protocol_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rule_dimensions
    ADD CONSTRAINT protocol_rule_dimensions_protocol_version_id_fkey FOREIGN KEY (protocol_version_id) REFERENCES public.protocol_versions(protocol_version_id) ON DELETE CASCADE;

--
-- Name: protocol_rule_dimensions protocol_rule_dimensions_rule_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rule_dimensions
    ADD CONSTRAINT protocol_rule_dimensions_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES public.protocol_rules(rule_id) ON DELETE CASCADE;

--
-- Name: protocol_rule_dimensions protocol_rule_dimensions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rule_dimensions
    ADD CONSTRAINT protocol_rule_dimensions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id) ON DELETE CASCADE;

--
-- Name: protocol_rules protocol_rules_sop_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rules
    ADD CONSTRAINT protocol_rules_sop_version_tenant_fk FOREIGN KEY (tenant_id, sop_version_id) REFERENCES public.sop_versions(tenant_id, sop_version_id);

--
-- Name: protocol_rules protocol_rules_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rules
    ADD CONSTRAINT protocol_rules_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: protocol_rules protocol_rules_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_rules
    ADD CONSTRAINT protocol_rules_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES public.protocol_versions(tenant_id, protocol_version_id);

--
-- Name: protocol_triggers protocol_triggers_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_triggers
    ADD CONSTRAINT protocol_triggers_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: protocol_triggers protocol_triggers_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_triggers
    ADD CONSTRAINT protocol_triggers_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES public.protocol_versions(tenant_id, protocol_version_id);

--
-- Name: protocol_versions protocol_versions_protocol_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_protocol_tenant_fk FOREIGN KEY (tenant_id, protocol_id) REFERENCES public.protocol_definitions(tenant_id, protocol_id);

--
-- Name: protocol_versions protocol_versions_sop_version_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_sop_version_tenant_fk FOREIGN KEY (tenant_id, sop_version_id) REFERENCES public.sop_versions(tenant_id, sop_version_id);

--
-- Name: protocol_versions protocol_versions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.protocol_versions
    ADD CONSTRAINT protocol_versions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: shed_lifecycle_status_lookup shed_lifecycle_status_lookup_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_lifecycle_status_lookup
    ADD CONSTRAINT shed_lifecycle_status_lookup_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: shed_profiles shed_profiles_animal_stage_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_profiles
    ADD CONSTRAINT shed_profiles_animal_stage_tenant_fk FOREIGN KEY (tenant_id, animal_stage_id) REFERENCES public.animal_stage_lookup(tenant_id, animal_stage_id);

--
-- Name: shed_profiles shed_profiles_lifecycle_status_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_profiles
    ADD CONSTRAINT shed_profiles_lifecycle_status_tenant_fk FOREIGN KEY (tenant_id, shed_lifecycle_status_id) REFERENCES public.shed_lifecycle_status_lookup(tenant_id, shed_lifecycle_status_id);

--
-- Name: shed_profiles shed_profiles_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_profiles
    ADD CONSTRAINT shed_profiles_location_id_fkey FOREIGN KEY (location_id) REFERENCES public.locations(location_id);

--
-- Name: shed_profiles shed_profiles_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_profiles
    ADD CONSTRAINT shed_profiles_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: shed_profiles shed_profiles_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shed_profiles
    ADD CONSTRAINT shed_profiles_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: shifting_event_impacts shifting_event_impacts_breed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_event_impacts
    ADD CONSTRAINT shifting_event_impacts_breed_id_fkey FOREIGN KEY (breed_id) REFERENCES public.breeds(breed_id);

--
-- Name: shifting_event_impacts shifting_event_impacts_shifting_event_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_event_impacts
    ADD CONSTRAINT shifting_event_impacts_shifting_event_id_fkey FOREIGN KEY (shifting_event_id) REFERENCES public.shifting_events(shifting_event_id) ON DELETE CASCADE;

--
-- Name: shifting_event_impacts shifting_event_impacts_tenant_event_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_event_impacts
    ADD CONSTRAINT shifting_event_impacts_tenant_event_fk FOREIGN KEY (tenant_id, shifting_event_id) REFERENCES public.shifting_events(tenant_id, shifting_event_id) ON DELETE CASCADE;

--
-- Name: shifting_event_impacts shifting_event_impacts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_event_impacts
    ADD CONSTRAINT shifting_event_impacts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: shifting_events shifting_events_destination_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_destination_park_id_fkey FOREIGN KEY (destination_park_id) REFERENCES public.locations(location_id);

--
-- Name: shifting_events shifting_events_destination_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_destination_shed_id_fkey FOREIGN KEY (destination_shed_id) REFERENCES public.locations(location_id);

--
-- Name: shifting_events shifting_events_source_park_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_source_park_id_fkey FOREIGN KEY (source_park_id) REFERENCES public.locations(location_id);

--
-- Name: shifting_events shifting_events_source_shed_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_source_shed_id_fkey FOREIGN KEY (source_shed_id) REFERENCES public.locations(location_id);

--
-- Name: shifting_events shifting_events_tenant_destination_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_tenant_destination_park_fk FOREIGN KEY (tenant_id, destination_park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: shifting_events shifting_events_tenant_destination_shed_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_tenant_destination_shed_fk FOREIGN KEY (tenant_id, destination_shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: shifting_events shifting_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: shifting_events shifting_events_tenant_source_park_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_tenant_source_park_fk FOREIGN KEY (tenant_id, source_park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: shifting_events shifting_events_tenant_source_shed_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.shifting_events
    ADD CONSTRAINT shifting_events_tenant_source_shed_fk FOREIGN KEY (tenant_id, source_shed_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: sop_definitions sop_definitions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_definitions
    ADD CONSTRAINT sop_definitions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_submission_items sop_submission_items_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submission_items
    ADD CONSTRAINT sop_submission_items_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: sop_submission_items sop_submission_items_submission_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submission_items
    ADD CONSTRAINT sop_submission_items_submission_id_fkey FOREIGN KEY (submission_id) REFERENCES public.sop_submissions(submission_id);

--
-- Name: sop_submission_items sop_submission_items_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submission_items
    ADD CONSTRAINT sop_submission_items_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: sop_submission_items sop_submission_items_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submission_items
    ADD CONSTRAINT sop_submission_items_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_submissions sop_submissions_sop_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submissions
    ADD CONSTRAINT sop_submissions_sop_version_id_fkey FOREIGN KEY (sop_version_id) REFERENCES public.sop_versions(sop_version_id);

--
-- Name: sop_submissions sop_submissions_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submissions
    ADD CONSTRAINT sop_submissions_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: sop_submissions sop_submissions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_submissions
    ADD CONSTRAINT sop_submissions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_task_scan_captures sop_task_scan_captures_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_captures
    ADD CONSTRAINT sop_task_scan_captures_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: sop_task_scan_captures sop_task_scan_captures_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_captures
    ADD CONSTRAINT sop_task_scan_captures_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: sop_task_scan_captures sop_task_scan_captures_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_captures
    ADD CONSTRAINT sop_task_scan_captures_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_task_scan_attempts sop_task_scan_attempts_goat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_attempts
    ADD CONSTRAINT sop_task_scan_attempts_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);

--
-- Name: sop_task_scan_attempts sop_task_scan_attempts_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_attempts
    ADD CONSTRAINT sop_task_scan_attempts_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: sop_task_scan_attempts sop_task_scan_attempts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_scan_attempts
    ADD CONSTRAINT sop_task_scan_attempts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_task_review_fanouts sop_task_review_fanouts_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_review_fanouts
    ADD CONSTRAINT sop_task_review_fanouts_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: sop_task_review_fanouts sop_task_review_fanouts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_review_fanouts
    ADD CONSTRAINT sop_task_review_fanouts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_task_submission_fanouts sop_task_submission_fanouts_submission_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_submission_fanouts
    ADD CONSTRAINT sop_task_submission_fanouts_submission_id_fkey FOREIGN KEY (submission_id) REFERENCES public.sop_submissions(submission_id);

--
-- Name: sop_task_submission_fanouts sop_task_submission_fanouts_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_submission_fanouts
    ADD CONSTRAINT sop_task_submission_fanouts_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);

--
-- Name: sop_task_submission_fanouts sop_task_submission_fanouts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_task_submission_fanouts
    ADD CONSTRAINT sop_task_submission_fanouts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_tasks sop_tasks_sop_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_tasks
    ADD CONSTRAINT sop_tasks_sop_id_fkey FOREIGN KEY (sop_id) REFERENCES public.sop_definitions(sop_id);

--
-- Name: sop_tasks sop_tasks_sop_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_tasks
    ADD CONSTRAINT sop_tasks_sop_version_id_fkey FOREIGN KEY (sop_version_id) REFERENCES public.sop_versions(sop_version_id);

--
-- Name: sop_tasks sop_tasks_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_tasks
    ADD CONSTRAINT sop_tasks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: sop_versions sop_versions_sop_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_versions
    ADD CONSTRAINT sop_versions_sop_id_fkey FOREIGN KEY (sop_id) REFERENCES public.sop_definitions(sop_id);

--
-- Name: sop_versions sop_versions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sop_versions
    ADD CONSTRAINT sop_versions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: source_entry_decisions source_entry_decisions_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: source_entry_decisions source_entry_decisions_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: source_entry_decisions source_entry_decisions_owner_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_owner_fk FOREIGN KEY (owner_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: source_entry_decisions source_entry_decisions_proof_ref_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_proof_ref_id_fkey FOREIGN KEY (proof_ref_id) REFERENCES public.proof_artifacts(proof_id);

--
-- Name: source_entry_decisions source_entry_decisions_task_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES public.sop_tasks(tenant_id, task_id);

--
-- Name: source_entry_decisions source_entry_decisions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_entry_decisions
    ADD CONSTRAINT source_entry_decisions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: source_holding_stays source_holding_stays_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_holding_stays
    ADD CONSTRAINT source_holding_stays_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: source_holding_stays source_holding_stays_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_holding_stays
    ADD CONSTRAINT source_holding_stays_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: source_holding_stays source_holding_stays_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_holding_stays
    ADD CONSTRAINT source_holding_stays_location_tenant_fk FOREIGN KEY (tenant_id, holding_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: source_holding_stays source_holding_stays_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.source_holding_stays
    ADD CONSTRAINT source_holding_stays_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: transit_handoffs transit_handoffs_from_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_from_location_tenant_fk FOREIGN KEY (tenant_id, from_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: transit_handoffs transit_handoffs_load_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_load_tenant_fk FOREIGN KEY (tenant_id, load_id) REFERENCES public.procurement_loads(tenant_id, load_id);

--
-- Name: transit_handoffs transit_handoffs_proof_ref_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_proof_ref_id_fkey FOREIGN KEY (proof_ref_id) REFERENCES public.proof_artifacts(proof_id);

--
-- Name: transit_handoffs transit_handoffs_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: transit_handoffs transit_handoffs_to_location_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.transit_handoffs
    ADD CONSTRAINT transit_handoffs_to_location_tenant_fk FOREIGN KEY (tenant_id, to_location_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: user_scope_grants user_scope_grants_role_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_scope_grants
    ADD CONSTRAINT user_scope_grants_role_fk FOREIGN KEY (role) REFERENCES public.org_role_catalog(role_key);

--
-- Name: user_scope_grants user_scope_grants_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_scope_grants
    ADD CONSTRAINT user_scope_grants_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: vaccination_capacity_config vaccination_capacity_config_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_capacity_config
    ADD CONSTRAINT vaccination_capacity_config_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: vaccination_completions vaccination_completions_batch_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES public.obligation_batches(tenant_id, batch_id);

--
-- Name: vaccination_completions vaccination_completions_goat_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: vaccination_completions vaccination_completions_lot_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_lot_tenant_fk FOREIGN KEY (tenant_id, vaccine_inventory_lot_id) REFERENCES public.inventory_stock(tenant_id, stock_id);

--
-- Name: vaccination_completions vaccination_completions_obligation_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES public.obligation_instances(tenant_id, obligation_id);

--
-- Name: vaccination_completions vaccination_completions_submission_item_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_submission_item_tenant_fk FOREIGN KEY (tenant_id, sop_submission_item_id) REFERENCES public.sop_submission_items(tenant_id, item_id);

--
-- Name: vaccination_completions vaccination_completions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_completions
    ADD CONSTRAINT vaccination_completions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: vaccination_eligibility_rollups vaccination_eligibility_rollups_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_eligibility_rollups
    ADD CONSTRAINT vaccination_eligibility_rollups_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: vaccination_generation_runs vaccination_generation_runs_cursor_goat_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_generation_runs
    ADD CONSTRAINT vaccination_generation_runs_cursor_goat_fk FOREIGN KEY (tenant_id, cursor_goat_id) REFERENCES public.goats(tenant_id, goat_id);

--
-- Name: vaccination_generation_runs vaccination_generation_runs_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_generation_runs
    ADD CONSTRAINT vaccination_generation_runs_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: vaccination_generation_runs vaccination_generation_runs_version_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_generation_runs
    ADD CONSTRAINT vaccination_generation_runs_version_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES public.protocol_versions(tenant_id, protocol_version_id);

--
-- Name: vaccination_reminder_cadence_fires vaccination_reminder_cadence_fires_location_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_reminder_cadence_fires
    ADD CONSTRAINT vaccination_reminder_cadence_fires_location_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id);

--
-- Name: vaccination_reminder_cadence_fires vaccination_reminder_cadence_fires_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccination_reminder_cadence_fires
    ADD CONSTRAINT vaccination_reminder_cadence_fires_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: vaccines vaccines_item_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccines
    ADD CONSTRAINT vaccines_item_tenant_fk FOREIGN KEY (tenant_id, item_id) REFERENCES public.inventory_items(tenant_id, item_id);

--
-- Name: vaccines vaccines_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vaccines
    ADD CONSTRAINT vaccines_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: verification_items verification_items_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.verification_items
    ADD CONSTRAINT verification_items_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_absences workforce_absences_replacement_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_absences
    ADD CONSTRAINT workforce_absences_replacement_member_id_fkey FOREIGN KEY (replacement_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_absences workforce_absences_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_absences
    ADD CONSTRAINT workforce_absences_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_absences workforce_absences_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_absences
    ADD CONSTRAINT workforce_absences_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_capabilities workforce_capabilities_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_capabilities
    ADD CONSTRAINT workforce_capabilities_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_external_identities workforce_external_identities_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_external_identities
    ADD CONSTRAINT workforce_external_identities_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_external_identities workforce_external_identities_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_external_identities
    ADD CONSTRAINT workforce_external_identities_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_member_app_sessions workforce_member_app_sessions_device_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_app_sessions
    ADD CONSTRAINT workforce_member_app_sessions_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.workforce_member_devices(device_id);

--
-- Name: workforce_member_app_sessions workforce_member_app_sessions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_app_sessions
    ADD CONSTRAINT workforce_member_app_sessions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_member_app_sessions workforce_member_app_sessions_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_app_sessions
    ADD CONSTRAINT workforce_member_app_sessions_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_member_capabilities workforce_member_capabilities_capability_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_capabilities
    ADD CONSTRAINT workforce_member_capabilities_capability_id_fkey FOREIGN KEY (capability_id) REFERENCES public.workforce_capabilities(capability_id);

--
-- Name: workforce_member_capabilities workforce_member_capabilities_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_capabilities
    ADD CONSTRAINT workforce_member_capabilities_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_member_capabilities workforce_member_capabilities_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_capabilities
    ADD CONSTRAINT workforce_member_capabilities_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_member_devices workforce_member_devices_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_devices
    ADD CONSTRAINT workforce_member_devices_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_member_devices workforce_member_devices_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_member_devices
    ADD CONSTRAINT workforce_member_devices_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_members workforce_members_department_tenant_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_members
    ADD CONSTRAINT workforce_members_department_tenant_fk FOREIGN KEY (tenant_id, department_id) REFERENCES public.departments(tenant_id, department_id);

--
-- Name: workforce_members workforce_members_primary_location_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_members
    ADD CONSTRAINT workforce_members_primary_location_id_fkey FOREIGN KEY (primary_location_id) REFERENCES public.locations(location_id);

--
-- Name: workforce_members workforce_members_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_members
    ADD CONSTRAINT workforce_members_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_positions workforce_positions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_positions
    ADD CONSTRAINT workforce_positions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_positions workforce_positions_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_positions
    ADD CONSTRAINT workforce_positions_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- Name: workforce_roster_assignments workforce_roster_assignments_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_roster_assignments
    ADD CONSTRAINT workforce_roster_assignments_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);

--
-- Name: workforce_roster_assignments workforce_roster_assignments_workforce_member_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workforce_roster_assignments
    ADD CONSTRAINT workforce_roster_assignments_workforce_member_id_fkey FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members(workforce_member_id);

--
-- PostgreSQL database dump complete
--

SELECT pg_catalog.set_config('search_path', 'public', false);

-- Vaccination execution proof is captured from each scanned goat row. One to five camera clips
-- may cover every vaccine administered to that goat in the same handling; finalization only
-- validates the already-synced goat proof refs.
WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    jsonb_set(
      jsonb_set(
        sv.form_dsl,
        '{fields}',
        COALESCE((
          SELECT jsonb_agg(
            CASE
              WHEN field ->> 'key' = 'goat_ids' THEN
                jsonb_set(
                  field,
                  '{description}',
                  to_jsonb('Scan each goat RFID exactly when the vaccine is given. The scan timestamp is the vaccination timestamp.'::text),
                  true
                )
              ELSE field
            END
            ORDER BY ordinal
          )
          FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
            WITH ORDINALITY AS entries(field, ordinal)
          WHERE field ->> 'key' NOT IN (
            'shed_video', 'vial_lot_video', 'administration_video',
            'extra_video_1', 'extra_video_1_caption', 'extra_video_2', 'extra_video_2_caption',
            'vaccine_lot_id', 'cold_chain_verified', 'dose_ml_given', 'route_site',
            'administered_at', 'adverse_reaction', 'adverse_reaction_notes'
          )
        ), '[]'::jsonb),
        true
      ),
      '{rules}',
      COALESCE((
        SELECT jsonb_agg(rule ORDER BY ordinal)
        FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'rules', '[]'::jsonb))
          WITH ORDINALITY AS entries(rule, ordinal)
        WHERE COALESCE(rule ->> 'field', '') NOT IN (
          'shed_video', 'vial_lot_video', 'administration_video',
          'extra_video_1_caption', 'extra_video_2_caption',
          'cold_chain_verified', 'adverse_reaction_notes'
        )
        AND COALESCE(rule -> 'when' ->> 'field', '') NOT IN (
          'cold_chain_verified', 'adverse_reaction'
        )
      ), '[]'::jsonb),
      true
    ) || jsonb_build_object(
      'goat_row_proof',
      jsonb_build_object(
        'subject_scope', 'goat',
        'capture_source', 'in_app_camera',
        'minimum_clips', 1,
        'maximum_clips', 5,
        'one_clip_covers_same_handling_vaccines', true
      )
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'subject_scope', 'goat',
      'expected_subjects', jsonb_build_array('goat'),
	  'minimum_count', 1,
      'minimum_count_per_subject', 1,
      'maximum_count_per_subject', 5,
      'capture_source', 'in_app_camera',
      'one_clip_covers_same_handling_vaccines', true,
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;

SELECT pg_catalog.set_config('search_path', 'public', false);


-- -----------------------------------------------------------------------------
-- Collapsed from 000002_vaccination_capacity_overflow_policy.sql
-- -----------------------------------------------------------------------------
-- Keep already-created databases compatible with the vaccination capacity enum rename.
-- Clean-slate DBs get this from the baseline; dev/stg DBs that already applied the baseline
-- need a forward migration instead of relying on edited historical SQL.

-- Drop old constraint first to allow the update.
ALTER TABLE vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_overflow_check;

-- Now update rows to the new enum value.
UPDATE vaccination_capacity_config
SET overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap'
WHERE overflow_policy = 'split_within_safe_window_then_mark_needs_review';

-- Add the new constraint on the updated value.
ALTER TABLE vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_overflow_check
  CHECK (overflow_policy = 'split_within_safe_window_last_safe_may_exceed_cap');


-- -----------------------------------------------------------------------------
-- Collapsed from 000003_r50_forward_compatibility.sql
-- -----------------------------------------------------------------------------
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=ravi issue=R50-015 reason=lock-safety-refactor-of-existing-CHECK-constraints-no-new-seed-data-or-read-model expiry=2026-10-31
-- R50-015 P0 fix: this migration was previously wrapped in goose's default single
-- transaction. Even though the CHECK-constraint deltas below already used the
-- DROP CONSTRAINT + ADD CONSTRAINT ... NOT VALID + VALIDATE CONSTRAINT pattern,
-- wrapping all three steps in ONE transaction defeats the purpose of NOT VALID:
-- Postgres locks are held at their strongest level for the lifetime of the
-- transaction, not the statement. DROP CONSTRAINT acquires ACCESS EXCLUSIVE on the
-- hot table; inside a single transaction that ACCESS EXCLUSIVE lock is still held
-- (it never downgrades) while VALIDATE CONSTRAINT performs its full-table scan,
-- so concurrent inserts/updates on notification_requests / obligation_status_events
-- are blocked for the entire scan duration -- exactly the outage NOT VALID is
-- supposed to avoid. `-- +goose NO TRANSACTION` makes goose apply every statement
-- below in its own autocommit transaction, so each ACCESS EXCLUSIVE catalog-only
-- lock (DROP CONSTRAINT, ADD CONSTRAINT ... NOT VALID, CREATE TABLE, ADD COLUMN)
-- is acquired and released in milliseconds, and each VALIDATE CONSTRAINT then only
-- ever needs to hold the much weaker SHARE UPDATE EXCLUSIVE lock (which permits
-- concurrent reads AND writes, only blocking other DDL) for the duration of its
-- scan. Every statement in this file is already idempotent/re-runnable (IF EXISTS /
-- IF NOT EXISTS / DO $$ existence checks), which is required for NO TRANSACTION
-- migrations: a failure partway through does not roll back earlier statements, so a
-- retry must be a safe no-op for everything that already applied.
-- Forward-compatibility catch-up for dev/stg databases that already applied the ORIGINAL
-- 000001 clean-slate baseline before it was edited in place by later commits. Every
-- statement below is idempotent/re-runnable: a database that already has a delta (via the
-- edited baseline, via a clean install, or via a prior partial run of this migration) is
-- left unchanged, and one that is missing a delta is caught up to match the current
-- baseline shape exactly. Does NOT include the vaccination_capacity_config overflow_policy
-- rename -- that delta is already covered by 000002_vaccination_capacity_overflow_policy.sql.

-- 1) notification_requests.notification_type: allow the verification lifecycle values that
--    the edited baseline added ('verification_approved', 'verification_closed').
--    Lock-safe on hot table: NOT VALID + concurrent VALIDATE for hot tables (notification_requests).
--    Bounded lock_timeout so a lock-contended DROP/ADD/VALIDATE fails fast (statement error,
--    retried on the next migration run) instead of hanging indefinitely behind a long-lived
--    reader/writer transaction.
SET lock_timeout = '5s';

ALTER TABLE notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;

ALTER TABLE notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text
  ]))) NOT VALID;

ALTER TABLE notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;

-- 2) SOP RFID scan-capture tables (goat-scan proof capture + attempt audit trail). These are
--    wholly new tables added by the edited baseline; an old-baseline database has neither.
CREATE TABLE IF NOT EXISTS public.sop_task_scan_captures (
    capture_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    field_key text NOT NULL,
    tag text NOT NULL,
    normalized_tag text NOT NULL,
    goat_id uuid,
    obligation_id uuid,
    captured_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    captured_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_scan_captures_pkey PRIMARY KEY (capture_id),
    CONSTRAINT sop_task_scan_captures_field_key_check CHECK ((btrim(field_key) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_normalized_tag_check CHECK ((btrim(normalized_tag) <> ''::text)),
    CONSTRAINT sop_task_scan_captures_tag_check CHECK ((btrim(tag) <> ''::text))
);

CREATE TABLE IF NOT EXISTS public.sop_task_scan_attempts (
    attempt_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL,
    field_key text NOT NULL,
    tag text NOT NULL,
    normalized_tag text NOT NULL,
    goat_id uuid,
    obligation_id uuid,
    outcome text NOT NULL,
    tag_role text DEFAULT 'unknown'::text NOT NULL,
    reason text,
    captured_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    captured_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT sop_task_scan_attempts_pkey PRIMARY KEY (attempt_id),
    CONSTRAINT sop_task_scan_attempts_field_key_check CHECK ((btrim(field_key) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_idempotency_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_normalized_tag_check CHECK ((btrim(normalized_tag) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_outcome_check CHECK ((outcome = ANY (ARRAY['accepted'::text, 'duplicate'::text, 'not_due'::text, 'unknown'::text]))),
    CONSTRAINT sop_task_scan_attempts_tag_check CHECK ((btrim(tag) <> ''::text)),
    CONSTRAINT sop_task_scan_attempts_tag_role_check CHECK ((tag_role = ANY (ARRAY['primary'::text, 'secondary'::text, 'unknown'::text])))
);

CREATE UNIQUE INDEX IF NOT EXISTS sop_task_scan_captures_idempotency_unique_idx ON public.sop_task_scan_captures USING btree (tenant_id, idempotency_key);
CREATE UNIQUE INDEX IF NOT EXISTS sop_task_scan_captures_task_field_tag_unique_idx ON public.sop_task_scan_captures USING btree (tenant_id, task_id, field_key, normalized_tag);
CREATE INDEX IF NOT EXISTS sop_task_scan_captures_task_idx ON public.sop_task_scan_captures USING btree (tenant_id, task_id, captured_at, capture_id);

CREATE UNIQUE INDEX IF NOT EXISTS sop_task_scan_attempts_idempotency_unique_idx ON public.sop_task_scan_attempts USING btree (tenant_id, idempotency_key);
CREATE INDEX IF NOT EXISTS sop_task_scan_attempts_task_goat_idx ON public.sop_task_scan_attempts USING btree (tenant_id, task_id, goat_id, captured_at, attempt_id);
CREATE INDEX IF NOT EXISTS sop_task_scan_attempts_task_idx ON public.sop_task_scan_attempts USING btree (tenant_id, task_id, captured_at, attempt_id);

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_goat_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_captures
      ADD CONSTRAINT sop_task_scan_captures_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_task_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_captures
      ADD CONSTRAINT sop_task_scan_captures_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_captures_tenant_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_captures
      ADD CONSTRAINT sop_task_scan_captures_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_goat_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_attempts
      ADD CONSTRAINT sop_task_scan_attempts_goat_id_fkey FOREIGN KEY (goat_id) REFERENCES public.goats(goat_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_task_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_attempts
      ADD CONSTRAINT sop_task_scan_attempts_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.sop_tasks(task_id);
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sop_task_scan_attempts_tenant_id_fkey') THEN
    ALTER TABLE ONLY public.sop_task_scan_attempts
      ADD CONSTRAINT sop_task_scan_attempts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(tenant_id);
  END IF;
END $$;

-- 3) vaccination_capacity_config.overflow_policy rename: already covered by
--    000002_vaccination_capacity_overflow_policy.sql. Intentionally skipped here.

-- 4) verification_items: subject_label / closed_by / closed_at columns plus their CHECKs,
--    added by the edited baseline for the verification-closure workflow.
ALTER TABLE verification_items ADD COLUMN IF NOT EXISTS subject_label text;
ALTER TABLE verification_items ADD COLUMN IF NOT EXISTS closed_by uuid;
ALTER TABLE verification_items ADD COLUMN IF NOT EXISTS closed_at timestamp with time zone;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_subject_label_check') THEN
    ALTER TABLE verification_items
      ADD CONSTRAINT verification_items_subject_label_check
      CHECK (((subject_label IS NULL) OR (btrim(subject_label) <> ''::text)));
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_closed_pair_check') THEN
    ALTER TABLE verification_items
      ADD CONSTRAINT verification_items_closed_pair_check
      CHECK (((closed_by IS NULL) = (closed_at IS NULL)));
  END IF;
END $$;

DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'verification_items_closed_approved_check') THEN
    ALTER TABLE verification_items
      ADD CONSTRAINT verification_items_closed_approved_check
      CHECK (((closed_at IS NULL) OR (status = 'approved'::text)));
  END IF;
END $$;

-- 5) outbox_messages verification idempotency index: add 'verification.item.closed' to the
--    partial predicate so closure events get outbox idempotency coverage.
--    Handled concurrently in 000004_r50_forward_compat_concurrent_indexes.sql for hot-table safety.

-- 6) verification_items leadership closure queue: approved-and-not-yet-closed items, scoped
--    for the leadership review queue.
--    Handled concurrently in 000004_r50_forward_compat_concurrent_indexes.sql for large-table safety.

-- 7) Vaccination goat-scan SOP: MOVED TO 000006_r50_vaccination_sop_dml.sql for lock safety.
--    This step (step 7) is heavy DML that rewrites form_dsl/proof_policy for vaccination.drive /
--    vaccination.session SOP versions. Keeping it in this transaction with fast DDL would hold
--    ACCESS EXCLUSIVE locks on hot tables for the duration of the long UPDATE. Moved to a
--    separate migration to release DDL locks quickly (R50-015 P0).

-- 8) obligation_status_events.event_type: allow 'in_progress' so MarkInProgress (PEND-1: the
--    obligation_instances/obligation_batches 'in_progress' writer that was missing entirely,
--    which left MarkMissedBefore's in_progress-batch safety guard permanently dead) can record a
--    durable per-obligation status event when SOP-submit-time capture begins, mirroring
--    MarkCompleted's existing event pattern.
--    Lock-safe on hot table: NOT VALID + concurrent VALIDATE for hot tables (obligation_status_events).
--    Bounded lock_timeout (see notification_requests comment above for rationale).
SET lock_timeout = '5s';

ALTER TABLE obligation_status_events
  DROP CONSTRAINT IF EXISTS obligation_status_events_type_check;

ALTER TABLE obligation_status_events
  ADD CONSTRAINT obligation_status_events_type_check
  CHECK ((event_type = ANY (ARRAY[
    'scheduled'::text,
    'became_due'::text,
    'dispatched'::text,
    'in_progress'::text,
    'completed'::text,
    'missed'::text,
    'waived'::text,
    'escalated'::text,
    'escalation_acknowledged'::text,
    'escalation_resolved'::text,
    'canceled'::text,
    'deferred'::text,
    'rescoped'::text
  ]))) NOT VALID;

ALTER TABLE obligation_status_events
  VALIDATE CONSTRAINT obligation_status_events_type_check;


-- -----------------------------------------------------------------------------
-- Collapsed from 000004_r50_forward_compat_concurrent_indexes.sql
-- -----------------------------------------------------------------------------
-- +goose NO TRANSACTION
-- Item 5: Rebuild the outbox verification idempotency index with 'verification.item.closed' predicate.
-- Lock-safe on hot table outbox_messages: CREATE UNIQUE INDEX CONCURRENTLY with the NEW predicate (step 1),
-- ensuring ON CONFLICT always has a valid target, then DROP the old narrower index CONCURRENTLY (step 2).
-- This eliminates the 42P10 window. (R50-015 P0)
--
-- Dedup guard (judge Finding 2): the rebuilt index adds 'verification.item.closed' to the partial
-- predicate, widening which rows it covers. A forward-compat database that already has duplicate
-- (tenant_id, idempotency_key) rows among the now-covered event types would make the CREATE UNIQUE INDEX
-- CONCURRENTLY fail on populated tables. Keep the earliest row per (tenant_id, idempotency_key) among
-- predicate-covered rows and delete the rest. This is a one-way, non-reversible cleanup scoped to:
-- (a) event types covered by the new predicate, (b) rows created in the last 7 days, (c) batches of
-- up to 10000 rows per statement. Ordinary non-covered rows and rows with NULL idempotency_key are
-- never touched.
WITH duplicate_rows AS (
  SELECT dupe.outbox_id
  FROM public.outbox_messages dupe
  WHERE dupe.idempotency_key IS NOT NULL
    AND dupe.created_at > now() - '7 days'::interval
    AND dupe.event_type = ANY (ARRAY[
      'verification.item.pending'::text,
      'verification.verdict.approved'::text,
      'verification.verdict.rework'::text,
      'verification.item.closed'::text
    ])
    -- Keep the EARLIEST (tenant_id, idempotency_key) row per group; a row is a duplicate-to-delete
    -- iff an earlier row EXISTS. (Bug fix: this was NOT EXISTS, which selected the earliest row
    -- itself for deletion and left every later duplicate — so 3 dupes left 2 and the CREATE UNIQUE
    -- INDEX CONCURRENTLY below still failed 42P10.)
    AND EXISTS (
      SELECT 1
      FROM public.outbox_messages keep
      WHERE keep.tenant_id = dupe.tenant_id
        AND keep.idempotency_key = dupe.idempotency_key
        AND keep.idempotency_key IS NOT NULL
        AND keep.event_type = ANY (ARRAY[
          'verification.item.pending'::text,
          'verification.verdict.approved'::text,
          'verification.verdict.rework'::text,
          'verification.item.closed'::text
        ])
        AND (keep.created_at, keep.outbox_id) < (dupe.created_at, dupe.outbox_id)
    )
  LIMIT 10000
)
DELETE FROM public.outbox_messages
WHERE outbox_id IN (SELECT outbox_id FROM duplicate_rows);

-- ROOT-CAUSE FIX for R50-015 P0: drop the _v2 index ONLY IF it is INVALID (a leftover from a prior
-- CONCURRENTLY that failed partway). A VALID _v2 from a fully-successful prior attempt must be KEPT:
-- unconditionally dropping it would, on a retry where the OLD index is already gone, leave NO
-- ON CONFLICT arbiter and break every write (42P10 "no unique or exclusion constraint matching the
-- ON CONFLICT specification"). Invariant: the old index is dropped ONLY after _v2 is valid, so
-- whenever _v2 is invalid the old index still exists as the arbiter — this conditional drop never
-- opens a no-arbiter window. A non-CONCURRENT drop is safe here: an invalid index serves no reads.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_class c
    JOIN pg_index i ON i.indexrelid = c.oid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relname = 'outbox_messages_verification_idempotency_idx_v2'
      AND n.nspname = 'public'
      AND NOT i.indisvalid
  ) THEN
    DROP INDEX IF EXISTS public.outbox_messages_verification_idempotency_idx_v2;
  END IF;
END
$$;
-- +goose StatementEnd

-- Create the NEW unique index CONCURRENTLY with the widened predicate. Using a distinct name (_v2)
-- to avoid naming conflict with the old index during the transition. ON CONFLICT (tenant_id, idempotency_key)
-- will find either index on these columns; once the new one is live, writers are safe.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_verification_idempotency_idx_v2
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = ANY (ARRAY[
    'verification.item.pending'::text,
    'verification.verdict.approved'::text,
    'verification.verdict.rework'::text,
    'verification.item.closed'::text
  ]));

-- Now that the new index is live, drop the old (narrower) index CONCURRENTLY. No 42P10 window.
DROP INDEX CONCURRENTLY IF EXISTS public.outbox_messages_verification_idempotency_idx;

-- Item 6: Create the leadership closure queue index.
-- Lock-safe on potentially large table verification_items: CREATE INDEX CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_leadership_queue_idx
  ON public.verification_items USING btree (tenant_id, park_id, source_submission_id, captured_at, item_id)
  WHERE ((status = 'approved'::text) AND (closed_at IS NULL));

-- Item 7 (PEND-1/R50-006 root-cause fix): obligation_status_events_idempotency_idx was only ever a
-- plain (non-unique) index, so InsertDeferredObligation's
-- `INSERT ... ON CONFLICT (tenant_id, idempotency_key) DO NOTHING` had no matching arbiter
-- constraint and errored (SQLSTATE 42P10) on every call -- every deferred obligation insert was
-- broken. Swap it for a real UNIQUE index so that self-healing ON CONFLICT actually works.
-- Lock-safe on hot table obligation_status_events: CREATE UNIQUE INDEX CONCURRENTLY with the new
-- unique constraint (step 1), then DROP the old (plain, non-unique) index CONCURRENTLY (step 2).
-- This eliminates the 42P10 window. (R50-015 P0)
--
-- A forward-compat database that ran against the OLD plain (non-unique) index may have duplicate
-- (tenant_id, idempotency_key) rows. Dedup (judge Finding 2) before CREATE UNIQUE INDEX CONCURRENTLY
-- so the index creation cannot fail/come back INVALID. Keep the earliest row per (tenant_id, idempotency_key)
-- and delete the rest. This is a one-way, non-reversible cleanup scoped to: (a) rows created in the
-- last 30 days, (b) batches of up to 10000 rows per statement. Rows with NULL idempotency_key are
-- never touched.
WITH duplicate_rows AS (
  SELECT dupe.obligation_event_id
  FROM public.obligation_status_events dupe
  WHERE dupe.idempotency_key IS NOT NULL
    AND dupe.recorded_at > now() - '30 days'::interval
    -- Keep the EARLIEST row per (tenant_id, idempotency_key); delete a row iff an earlier row EXISTS.
    -- (Same P0 fix as the outbox dedup above: NOT EXISTS deleted the earliest and kept the later
    -- duplicates, so the CREATE UNIQUE INDEX CONCURRENTLY below still failed 42P10.)
    AND EXISTS (
      SELECT 1
      FROM public.obligation_status_events keep
      WHERE keep.tenant_id = dupe.tenant_id
        AND keep.idempotency_key = dupe.idempotency_key
        AND keep.idempotency_key IS NOT NULL
        AND (keep.recorded_at, keep.obligation_event_id) < (dupe.recorded_at, dupe.obligation_event_id)
    )
  LIMIT 10000
)
-- seed-migration-guard:ignore owner=ravi issue=R50-015 reason=one-time-dedup-cleanup-before-unique-index-rebuild-no-new-seed-data expiry=2026-10-31
DELETE FROM public.obligation_status_events
WHERE obligation_event_id IN (SELECT obligation_event_id FROM duplicate_rows);

-- ROOT-CAUSE FIX for R50-015 P0: Before attempting CREATE, drop any leftover INVALID index from
-- a prior failed attempt. If a prior CREATE INDEX CONCURRENTLY failed partway, Postgres leaves an
-- INVALID index. On retry, CREATE ... IF NOT EXISTS sees the invalid index "exists" and skips
-- rebuilding, then DROP removes the working OLD index, leaving the table with ONLY an invalid
-- unique index that breaks all writes. Dropping the invalid _v2 first ensures it will be properly
-- rebuilt, and ensures a valid target exists before we drop the old index. Idempotent: noop if _v2
-- doesn't exist.
-- R50-015 P0 (same fix as the outbox _v2 above): drop _v2 ONLY IF invalid; a valid _v2 must be
-- kept so a retry (with the old index already gone) never leaves a no-arbiter 42P10 window.
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM pg_class c
    JOIN pg_index i ON i.indexrelid = c.oid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relname = 'obligation_status_events_idempotency_idx_v2'
      AND n.nspname = 'public'
      AND NOT i.indisvalid
  ) THEN
    DROP INDEX IF EXISTS public.obligation_status_events_idempotency_idx_v2;
  END IF;
END
$$;
-- +goose StatementEnd

-- Create the new UNIQUE index CONCURRENTLY using a distinct name (_v2) to avoid naming conflict
-- with the old (non-unique) index. ON CONFLICT (tenant_id, idempotency_key) will find the unique
-- index on these columns; once the new one is live, writers are safe from 42P10.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS obligation_status_events_idempotency_idx_v2
  ON public.obligation_status_events USING btree (tenant_id, idempotency_key);

-- Now that the new unique index is live, drop the old (plain, non-unique) index CONCURRENTLY.
-- No 42P10 window.
DROP INDEX CONCURRENTLY IF EXISTS public.obligation_status_events_idempotency_idx;


-- -----------------------------------------------------------------------------
-- Collapsed from 000005_drop_stage_review.sql
-- -----------------------------------------------------------------------------
-- no-mismatch-review-queue:ignore: owner=ravi issue=goatos-r50-closure scope=legacy_stage_review_feature_removal expiry=2026-08-31
DROP TABLE IF EXISTS public.vaccination_stage_review_items CASCADE;


-- -----------------------------------------------------------------------------
-- Collapsed from 000006_r50_vaccination_sop_dml.sql
-- -----------------------------------------------------------------------------
-- Vaccination SOP form_dsl/proof_policy rewrite (moved from 000003 for lock safety).
--
-- Heavy DML that rewrites form_dsl/proof_policy for vaccination.drive / vaccination.session
-- SOP versions from batch-level proof to per-goat-row proof capture. This was originally
-- in 000003_r50_forward_compatibility.sql but has been separated to release DDL locks quickly
-- on hot tables (000003 now holds locks only for fast DDL/table creates). (R50-015 P0)
--
-- Guarded by subject_scope so a clean install (already migrated inside 000001's own tail),
-- a prior run of 000003, or a run of this migration is a no-op when already applied.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND COALESCE(sv.proof_policy ->> 'subject_scope', '') <> 'goat'
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    jsonb_set(
      jsonb_set(
        sv.form_dsl,
        '{fields}',
        COALESCE((
          SELECT jsonb_agg(
            CASE
              WHEN field ->> 'key' = 'goat_ids' THEN
                jsonb_set(
                  field,
                  '{description}',
                  to_jsonb('Scan each goat RFID exactly when the vaccine is given. The scan timestamp is the vaccination timestamp.'::text),
                  true
                )
              ELSE field
            END
            ORDER BY ordinal
          )
          FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
            WITH ORDINALITY AS entries(field, ordinal)
          WHERE field ->> 'key' NOT IN (
            'shed_video', 'vial_lot_video', 'administration_video',
            'extra_video_1', 'extra_video_1_caption', 'extra_video_2', 'extra_video_2_caption'
          )
        ), '[]'::jsonb),
        true
      ),
      '{rules}',
      COALESCE((
        SELECT jsonb_agg(rule ORDER BY ordinal)
        FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'rules', '[]'::jsonb))
          WITH ORDINALITY AS entries(rule, ordinal)
        WHERE COALESCE(rule ->> 'field', '') NOT IN (
          'shed_video', 'vial_lot_video', 'administration_video',
          'extra_video_1_caption', 'extra_video_2_caption'
        )
      ), '[]'::jsonb),
      true
    ) || jsonb_build_object(
      'goat_row_proof',
      jsonb_build_object(
        'subject_scope', 'goat',
        'capture_source', 'in_app_camera',
        'minimum_clips', 1,
        'maximum_clips', 5,
        'one_clip_covers_same_handling_vaccines', true
      )
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'subject_scope', 'goat',
      'expected_subjects', jsonb_build_array('goat'),
      'minimum_count', 1,
      'minimum_count_per_subject', 1,
      'maximum_count_per_subject', 5,
      'capture_source', 'in_app_camera',
      'one_clip_covers_same_handling_vaccines', true,
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;


-- -----------------------------------------------------------------------------
-- Collapsed from 000007_r50_vaccination_shed_ack_dml.sql
-- -----------------------------------------------------------------------------
-- Vaccination shed completion = acknowledgement, not a manual medical form (R50-shed-ack).
--
-- The operator already does the real work at ANIMAL level: scan each goat's RFID + attach one
-- live camera proof clip per goat row (mig 000006). Shed completion is only a final
-- acknowledgement that every expected animal in the shed has been scanned and proofed — there is
-- nothing left to fill in manually. This migration strips the remaining manual-medical-form
-- fields (vaccine batch, cold chain, dose, route/site, administered-at, adverse reaction) and
-- their block/required rules from the vaccination.drive / vaccination.session SOP versions'
-- form_dsl. `administered_at` becomes a server-derived value (submit time), never an operator
-- answer; `dose_ml_given` / `route_site` are no longer collected; `cold_chain_verified` no
-- longer gates submission; `adverse_reaction[_notes]` is no longer a form field (adverse events
-- are handled via the existing problem-report path, tracked separately from the acknowledgement).
--
-- Idempotent: guarded on presence of 'cold_chain_verified' in the SOP version's fields, so a
-- clean install (already stripped by 000001's own tail rewrite) or a prior run of this migration
-- is a no-op.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb)) AS f(field)
      WHERE field ->> 'key' IN (
        'vaccine_lot_id', 'cold_chain_verified', 'dose_ml_given', 'route_site',
        'administered_at', 'adverse_reaction', 'adverse_reaction_notes'
      )
    )
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    jsonb_set(
      jsonb_set(
        sv.form_dsl,
        '{fields}',
        COALESCE((
          SELECT jsonb_agg(field ORDER BY ordinal)
          FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
            WITH ORDINALITY AS entries(field, ordinal)
          WHERE field ->> 'key' NOT IN (
            'vaccine_lot_id', 'cold_chain_verified', 'dose_ml_given', 'route_site',
            'administered_at', 'adverse_reaction', 'adverse_reaction_notes'
          )
        ), '[]'::jsonb),
        true
      ),
      '{rules}',
      COALESCE((
        SELECT jsonb_agg(rule ORDER BY ordinal)
        FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'rules', '[]'::jsonb))
          WITH ORDINALITY AS entries(rule, ordinal)
        WHERE COALESCE(rule ->> 'field', '') NOT IN (
          'cold_chain_verified', 'adverse_reaction_notes'
        )
        AND COALESCE(rule -> 'when' ->> 'field', '') NOT IN (
          'cold_chain_verified', 'adverse_reaction'
        )
      ), '[]'::jsonb),
      true
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;

-- Relax vaccination_completions NOT NULL constraints that assumed a filled manual answer.
-- administered_at keeps a server-side default (submit/scan time) so it is never actually NULL in
-- practice, but the column no longer needs to reject a NULL insert if a caller derives it later
-- in the same transaction. Lock-safe: SET NOT NULL is being dropped (cheap catalog-only change,
-- does not require a table scan); adverse_reaction/cold_chain_verified already default false and
-- are left as-is since they are booleans with harmless defaults, not fillable answers anymore.
ALTER TABLE public.vaccination_completions
  ALTER COLUMN administered_at DROP NOT NULL,
  ALTER COLUMN administered_at SET DEFAULT now();


-- -----------------------------------------------------------------------------
-- Collapsed from 000008_obligation_batches_sop_task_idx.sql
-- -----------------------------------------------------------------------------
-- +goose NO TRANSACTION
-- Covering index for the batch lookup in ShedCompletionSummary (and shedCompletionVaccineBreakdown):
--   SELECT ... FROM obligation_batches ob WHERE ob.tenant_id = $1 AND ob.sop_task_id = $2
-- No existing obligation_batches index leads with (tenant_id, sop_task_id) -- the closest,
-- obligation_batches_scope_idx, leads with (tenant_id, scope_type, scope_id, status) and does not
-- cover a sop_task_id equality probe -- so the read planned a sequential scan on obligation_batches.
-- Add a partial index keyed on (tenant_id, sop_task_id) so the shed-completion read is an index
-- probe. Partial on sop_task_id IS NOT NULL because planned batches carry a NULL sop_task_id until
-- a task is materialized; only task-linked rows are ever probed here, keeping the index small.
--
-- Lock-safe: CREATE INDEX CONCURRENTLY (cannot run inside a transaction -- hence NO TRANSACTION),
-- IF NOT EXISTS so re-runs / forward-compat databases are a no-op. Mirrors the 000004 concurrent
-- index pattern.
CREATE INDEX CONCURRENTLY IF NOT EXISTS obligation_batches_sop_task_idx
  ON public.obligation_batches USING btree (tenant_id, sop_task_id)
  WHERE (sop_task_id IS NOT NULL);


-- -----------------------------------------------------------------------------
-- Collapsed from 000009_counts_approval_requests.sql
-- -----------------------------------------------------------------------------
-- P1: Baseline → Forward migration. Move counts_approval_requests table definition from 000001
-- to this forward migration so environments that already ran 000001 still get the table.
-- This migration includes P1 improvements: uniqueness constraint for shifting_event_id with
-- status='approved' to prevent multiple approved requests for the same shifting event.

-- Name: counts_approval_requests; Type: TABLE; Schema: public; Owner: -

-- counts_approval_requests: the Counts module's lifecycle approval workflow.
--
-- Maintainer decision (2026-07-19), superseding the previous apply-on-submit behaviour of
-- POST /app/counts/{birth,death,shifting}-events:
--
--   * BIRTH and DEATH are PENDING UNTIL APPROVED. Submitting one must NOT mutate `goats` and must
--     NOT emit goat.created / goat.exited. A kid's vaccination obligations are therefore generated
--     only on approval, and a death's open obligations are cancelled only on approval.
--   * SHIFTING already lands pending (shifting_events.authorization_state = 'pending'), so its
--     approval request LINKS to the existing row rather than duplicating the payload.
--   * Approver authority is per request_type: park_head decides shifting, ceo_internal decides
--     birth and death, operators decide nothing (permissions.CountsApproveShifting /
--     permissions.CountsApproveLifecycle).
--
-- Why the payload is stored here rather than applied eagerly: birth and death have no pending
-- representation anywhere else. `goats` is the APPLIED state, so a pending birth cannot be a goats
-- row without becoming visible to the herd register, the census, and the vaccination generator. The
-- validated request body is therefore held verbatim as JSONB and replayed through the SAME guarded
-- identity service command on approval (identity CreateAdminGoat / CriticalDeathExit) -- never a
-- bypass, so the dead+died critical-death guardrail is still enforced at apply time.
--
-- Grain: ONE row per submitted request. Not a projection, not a read model -- this is canonical
-- source state (the request and its decision), so it is rebuilt by nothing and owned here.

CREATE TABLE IF NOT EXISTS public.counts_approval_requests (
    approval_request_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    request_type text NOT NULL,

    -- Birth/death carry their validated submit body verbatim; shifting carries a small descriptor
    -- (the movement itself lives in shifting_events, referenced below) so the approvals list can be
    -- rendered without a join.
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,

    -- Set for request_type='shifting' only: the already-written pending shifting_events row this
    -- request authorizes. Enforced by counts_approval_requests_shifting_link_check below.
    shifting_event_id uuid,

    -- Set for request_type='death' only: the animal the request would exit. Denormalised from
    -- payload so the pending list and the duplicate-open guard do not have to parse JSONB.
    subject_goat_id uuid,

    status text DEFAULT 'pending'::text NOT NULL,

    raised_by_user_id uuid NOT NULL,
    raised_at timestamp with time zone DEFAULT now() NOT NULL,

    decided_by_user_id uuid,
    decided_at timestamp with time zone,
    decision_reason text,

    -- What the approval actually produced, recorded in the SAME transaction as the status flip.
    -- birth/death -> ('goat', <goat_id>); shifting -> ('shifting_event', <shifting_event_id>).
    -- A non-null pair is the proof that an 'approved' row's effect committed; it is also what makes
    -- a second approve a no-op replay instead of a second application.
    applied_result_type text,
    applied_result_id uuid,

    -- Submit-time idempotency (the operator's write). Mirrors the shifting_events convention:
    -- the key identifies the request, the fingerprint identifies the payload, so an exact replay
    -- returns the original row and a same-key/different-payload replay is a conflict.
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,

    -- Decision-time idempotency (the approver's write). Approve/reject are themselves mutating
    -- writes and carry the full contract, so they need their own key/fingerprint pair rather than
    -- reusing the submit key.
    decision_idempotency_key text,
    decision_request_fingerprint text,

    row_version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,

    CONSTRAINT counts_approval_requests_type_check
        CHECK ((request_type = ANY (ARRAY['birth'::text, 'death'::text, 'shifting'::text]))),
    CONSTRAINT counts_approval_requests_status_check
        CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))),
    CONSTRAINT counts_approval_requests_payload_object_check
        CHECK ((jsonb_typeof(payload) = 'object'::text)),

    -- A shifting request MUST point at its shifting_events row; birth/death MUST NOT. This is what
    -- keeps "approve" able to dispatch on request_type without trusting the payload.
    CONSTRAINT counts_approval_requests_shifting_link_check
        CHECK (((request_type = 'shifting'::text) = (shifting_event_id IS NOT NULL))),
    -- Only a death names a subject animal up front (a birth CREATES its animal on approval).
    CONSTRAINT counts_approval_requests_subject_goat_check
        CHECK (((subject_goat_id IS NULL) OR (request_type = 'death'::text))),

    -- Reject requires a reason -- enforced in the DB, not only in Go (same rule as
    -- verification_items_reject_reason_check).
    CONSTRAINT counts_approval_requests_reject_reason_check
        CHECK (((status <> 'rejected'::text) OR ((decision_reason IS NOT NULL) AND (btrim(decision_reason) <> ''::text)))),
    -- A decided row must record WHO decided and WHEN; a pending row must record neither.
    CONSTRAINT counts_approval_requests_decision_shape_check
        CHECK ((((status = 'pending'::text) AND (decided_by_user_id IS NULL) AND (decided_at IS NULL))
             OR ((status <> 'pending'::text) AND (decided_by_user_id IS NOT NULL) AND (decided_at IS NOT NULL)))),
    -- An APPROVED row must carry the applied result. This is the schema-level half of the atomic
    -- transition rule: a row cannot read 'approved' without naming the effect that committed with it.
    CONSTRAINT counts_approval_requests_applied_result_check
        CHECK ((((status = 'approved'::text) AND (applied_result_type IS NOT NULL) AND (applied_result_id IS NOT NULL))
             OR ((status <> 'approved'::text) AND (applied_result_type IS NULL) AND (applied_result_id IS NULL)))),

    CONSTRAINT counts_approval_requests_idem_check CHECK ((btrim(idempotency_key) <> ''::text)),
    CONSTRAINT counts_approval_requests_fingerprint_check CHECK ((btrim(request_fingerprint) <> ''::text)),
    CONSTRAINT counts_approval_requests_row_version_check CHECK ((row_version >= 1)),

    PRIMARY KEY (approval_request_id),
    UNIQUE (tenant_id, approval_request_id)
);

-- P1: Completion fan-out uniqueness constraint. Only one approved request per shifting event.
-- Partial unique index: only applies to approved shifting requests.
CREATE UNIQUE INDEX IF NOT EXISTS counts_approval_requests_approved_shifting_unique
  ON public.counts_approval_requests (tenant_id, shifting_event_id)
  WHERE (status = 'approved'::text) AND (shifting_event_id IS NOT NULL);

-- P1: ListPending scope filtering index for park-scoped approvers.
-- Index on (tenant_id, status, request_type, raised_at DESC, approval_request_id DESC)
-- for pending-list filtering.
CREATE INDEX IF NOT EXISTS counts_approval_requests_pending_queue_idx
  ON public.counts_approval_requests (tenant_id, status, request_type, raised_at DESC, approval_request_id DESC)
  WHERE (status = 'pending'::text);

-- Status-queue index for decided history.
CREATE INDEX IF NOT EXISTS counts_approval_requests_status_queue_idx
  ON public.counts_approval_requests (tenant_id, status, request_type, raised_at DESC, approval_request_id DESC);

-- Idempotency key uniqueness index.
CREATE UNIQUE INDEX IF NOT EXISTS counts_approval_requests_idempotency_unique
  ON public.counts_approval_requests (tenant_id, idempotency_key);

-- Open-shifting uniqueness constraint: only one pending request per shifting event.
CREATE UNIQUE INDEX IF NOT EXISTS counts_approval_requests_open_shifting_unique
  ON public.counts_approval_requests (tenant_id, shifting_event_id)
  WHERE ((status = 'pending'::text) AND (shifting_event_id IS NOT NULL));

-- Foreign key: shifting_event_id references shifting_events.
ALTER TABLE ONLY public.counts_approval_requests
    ADD CONSTRAINT counts_approval_requests_shifting_event_fkey
    FOREIGN KEY (tenant_id, shifting_event_id) REFERENCES public.shifting_events(tenant_id, shifting_event_id) ON DELETE RESTRICT;

-- P0 (review): shifting completion/cancellation columns on the PRE-EXISTING shifting_events table.
-- shifting_execution.go writes event_status='applied' with applied_at/applied_by (completion) and
-- event_status='canceled' with canceled_at/canceled_by/cancel_reason (cancellation), each with its
-- own execution-time idempotency key + fingerprint. shifting_events pre-dates this PR on main and
-- never had these columns, so completion/cancellation would fail at runtime ("column does not
-- exist"). Add them here as a FORWARD migration (not a baseline edit) so fresh AND already-migrated
-- environments converge. Additive + nullable, so existing rows are unaffected.
ALTER TABLE public.shifting_events
    ADD COLUMN IF NOT EXISTS applied_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS applied_by uuid,
    ADD COLUMN IF NOT EXISTS canceled_at timestamp with time zone,
    ADD COLUMN IF NOT EXISTS canceled_by uuid,
    ADD COLUMN IF NOT EXISTS cancel_reason text,
    ADD COLUMN IF NOT EXISTS completion_idempotency_key text,
    ADD COLUMN IF NOT EXISTS completion_request_fingerprint text,
    ADD COLUMN IF NOT EXISTS cancel_idempotency_key text,
    ADD COLUMN IF NOT EXISTS cancel_request_fingerprint text;

-- Shape checks: an 'applied' event must carry applied_at (and only 'applied' may); a 'canceled'
-- event must carry canceled_at/by + a non-empty reason (and only 'canceled' may); and an event may
-- only be 'applied' once it was authorized. Guarded so a re-run of this migration is a no-op.
--
-- Added NOT VALID (no full-table scan / no ACCESS EXCLUSIVE validation lock on the hot
-- shifting_events table) and DELIBERATELY LEFT NOT VALID. A NOT VALID CHECK is still enforced on
-- every subsequent INSERT/UPDATE -- it only skips the one-time scan of PRE-EXISTING rows -- and the
-- completion/cancel columns these checks cover are brand new here (all NULL, no 'applied'/'canceled'
-- row exists), so every existing row already satisfies them and the checks are effectively fully
-- valid for all data. A separate `VALIDATE CONSTRAINT` is intentionally NOT issued: the
-- migration-safety guard (validate-postgres-migrations) forbids a direct VALIDATE on the hot
-- shifting_events table, which must go through an explicitly reviewed concurrent rollout. If a future
-- backfill ever writes rows that predate these columns, validate them in that reviewed rollout, not
-- here.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_applied_shape_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_applied_shape_check
            CHECK ((((event_status = 'applied'::text) AND (applied_at IS NOT NULL))
                 OR ((event_status <> 'applied'::text) AND (applied_at IS NULL) AND (applied_by IS NULL)))) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_canceled_shape_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_canceled_shape_check
            CHECK ((((event_status = 'canceled'::text) AND (canceled_at IS NOT NULL) AND (canceled_by IS NOT NULL)
                     AND (cancel_reason IS NOT NULL) AND (btrim(cancel_reason) <> ''::text))
                 OR ((event_status <> 'canceled'::text) AND (canceled_at IS NULL) AND (canceled_by IS NULL)
                     AND (cancel_reason IS NULL)))) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'shifting_events_applied_requires_authorization_check') THEN
        ALTER TABLE public.shifting_events
            ADD CONSTRAINT shifting_events_applied_requires_authorization_check
            CHECK (((event_status <> 'applied'::text) OR (authorization_state = 'authorized'::text))) NOT VALID;
    END IF;
END $$;
-- +goose StatementEnd


-- -----------------------------------------------------------------------------
-- Collapsed from 000010_department_module_grants.sql
-- -----------------------------------------------------------------------------
-- P0 (rebase recovery): create public.department_module_grants.
--
-- The counts/nav PR originally added this table to the 000001 baseline. When the branch was rebased
-- onto a main whose baseline had been independently rewritten, the auto-merge of 000001 dropped the
-- table -- so bootstrap_copy.go composes nav LIVE from department_module_grants
-- (ListGrantedModuleKeys), but no migration created it, and every non-leadership operator's
-- /app/bootstrap would fail or compose a blank bottom bar. This restores it as a FORWARD migration
-- (departments and workforce_members.department_id already exist in the current baseline), so fresh
-- AND already-migrated environments converge. Additive only.
--
-- department_module_grants: the module side of "a person's job = role x granted modules".
-- Canonical rule: docs/decisions/role-module-nav-composition.md. module_key is deliberately NOT an
-- enum: the moduleNavRegistry in bootstrap_copy.go is the semantic source of truth for which module
-- keys mean anything; an unrecognised key simply contributes no nav. Adding a module is a registry
-- entry plus a grant row, never a schema migration. The CHECK only enforces the shared code shape.

CREATE TABLE IF NOT EXISTS public.department_module_grants (
    department_module_grant_id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    department_id uuid NOT NULL,
    module_key text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT department_module_grants_module_key_check CHECK ((module_key ~ '^[a-z][a-z0-9_]*$'::text)),
    CONSTRAINT department_module_grants_status_check CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))
);

-- Guarded so a re-run (or a base that already had the table) is a no-op.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_module_grants_pkey') THEN
        ALTER TABLE ONLY public.department_module_grants
            ADD CONSTRAINT department_module_grants_pkey PRIMARY KEY (department_module_grant_id);
    END IF;
    -- One row per (tenant, department, module); re-granting is an idempotent upsert.
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_module_grants_tenant_department_module_key') THEN
        ALTER TABLE ONLY public.department_module_grants
            ADD CONSTRAINT department_module_grants_tenant_department_module_key
            UNIQUE (tenant_id, department_id, module_key);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'department_module_grants_department_id_fkey') THEN
        ALTER TABLE ONLY public.department_module_grants
            ADD CONSTRAINT department_module_grants_department_id_fkey
            FOREIGN KEY (tenant_id, department_id)
            REFERENCES public.departments(tenant_id, department_id) ON DELETE CASCADE;
    END IF;
END $$;
-- +goose StatementEnd

-- Serves ListGrantedModuleKeys (the two-key /app/bootstrap lookup).
CREATE INDEX IF NOT EXISTS department_module_grants_tenant_department_active_idx
    ON public.department_module_grants USING btree (tenant_id, department_id)
    INCLUDE (module_key) WHERE (status = 'active'::text);


-- -----------------------------------------------------------------------------
-- Collapsed from 000011_feed_ration_config.sql
-- -----------------------------------------------------------------------------
-- Feed ration configuration tables (the authored grid behind daily feed quantity).
--
-- WHY A FORWARD MIGRATION (not an edit to the 000001 baseline):
-- clean-slate databases get 000001 verbatim, but dev/stg databases have already applied and
-- checksum-tracked it. Editing historical SQL would leave those databases silently missing the
-- feed ration schema. Same rule that produced 000002.
--
-- ---------------------------------------------------------------------------
-- THE MODEL
-- ---------------------------------------------------------------------------
-- Feed quantity for one shed row is:
--
--   quantity = head_count x grams_per_head x shed_factor        (split across sessions)
--
-- and the ration lookup key is:
--
--   ration_group = (goats.age_band = 'kid') ? 'Kid' : breed_to_group(goats.breed)
--   tag          = goats.management_stage
--   rate         = (park, ration_group, tag, feed_item) -> grams_per_head
--
-- Two properties of that key drive this schema and are easy to get wrong:
--
--   1. FOR KIDS THE BREED IS IGNORED. The source workbook literally stores the string 'Kid' in
--      its breed column, so every kid of every breed shares one ration group. feed_ration_groups
--      therefore maps ADULT breed labels only; the 'Kid' group is selected by age band upstream
--      and never resolved through the breed map.
--
--   2. RATES ARE PARK-SCOPED. CBE and CPT agree on 75 of 77 group/tag rows and genuinely differ
--      on 2 (Osmanabadi/Non-Pregnant and Malai/Non-Pregnant). A tenant-global rate table would
--      quietly feed one of the two parks the wrong ration, so park_id is part of the natural key
--      of feed_ration_rates rather than an optional override dimension.
--
-- The breed -> ration_group map also carries a real merge: Beetal and Sirohi are two live breeds
-- that share ONE ration group, 'Beetal/Sirohi'. That is why the mapping is a table (data) and not
-- an identity function over goats.breed (code).
--
-- ---------------------------------------------------------------------------
-- CONFIGURED ZERO IS NOT MISSING CONFIGURATION  (the safety-critical rule here)
-- ---------------------------------------------------------------------------
-- A rate of 0 is a REAL, AUTHORED business value: K0 and K1 kids are on milk and are correctly
-- fed 0 g of every solid feed item. A rate that was never authored is a DIFFERENT state, and the
-- two must never collapse, because the consequences are opposite:
--
--   rate = 0        -> feed nothing. Correct. Proceed.
--   rate not found  -> we do not know what to feed this group/tag. BLOCK and surface the gap.
--
-- If "no row" were allowed to read as 0, an unconfigured shed would silently be fed nothing and
-- the operator would see a clean, complete-looking feed sheet. That is a starvation path, not a
-- display bug. So the distinction is structural, not conventional:
--
--   * grams_per_head is NOT NULL and has NO DEFAULT. A row therefore always carries an authored
--     number, and 0 is one of the legal authored numbers.
--   * ABSENCE OF A ROW is the only representation of "not configured". There is no sentinel
--     value, no nullable rate, and no default-to-zero anywhere in this schema.
--   * The read path must treat a zero-row lookup as a BLOCKING gap. It must not COALESCE, must
--     not LEFT JOIN a missing rate to 0, and must not fall back to another park's rate.
--
-- The same rule applies to feed_shed_factors.multiplier and feed_experiment_config.absolute_kg.
--
-- ---------------------------------------------------------------------------
-- NORMALIZATION
-- ---------------------------------------------------------------------------
-- Live goats.management_stage is free text off the source sheet and carries real separator and
-- case variants of the SAME tag: 'ICU- kid' (42 rows) vs 'ICU-Kid' (27), 'F2- Male' (8) vs
-- 'F2-Male' (453). Joining a stored tag to a live stage on raw equality would miss those rows --
-- and a missed rate lookup here is the blocking state described above, so 8 animals would stall
-- a shed's feed sheet for a purely cosmetic difference.
--
-- feed_config_norm() is the single definition of the join key. It is the counts module's
-- countAliasNorm / feedGrainNormSQL (backend/internal/counts/adapters/postgres) widened by one
-- character class: countAliasNorm collapses whitespace runs to '_', which unifies
-- 'Milking Warmup' but NOT 'ICU- kid' vs 'ICU-Kid', because the hyphen survives and leaves
-- 'icu-_kid' <> 'icu-kid'. Folding runs of [whitespace _ -] to a single '_' unifies both while
-- keeping every one of the 31 live tags, 7 ration groups, and 10 feed items distinct (verified
-- against the source grid: zero collisions).
--
-- It is applied on BOTH sides by construction:
--   * stored side  -- the *_key columns are GENERATED ALWAYS ... STORED over the label, so the
--                     database, not the caller, computes them. There is exactly one normalizer
--                     and a writer cannot bypass it.
--   * lookup side  -- callers wrap the raw live value: feed_config_norm(g.management_stage).
--     The function is IMMUTABLE, so that predicate is index-eligible.
--
-- feed_config_norm is idempotent (its output is already lowercase and '_'-joined), so applying it
-- to an already-normalized key is a no-op rather than a second transform.

CREATE FUNCTION feed_config_norm(value text) RETURNS text
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    RETURNS NULL ON NULL INPUT
    AS $$
      SELECT lower(regexp_replace(btrim(value), '[\s_-]+', '_', 'g'))
    $$;

COMMENT ON FUNCTION feed_config_norm(text) IS
  'Canonical feed-config join key: trim, casefold, collapse runs of whitespace/underscore/hyphen to a single underscore. Widened twin of counts.countAliasNorm; must be applied to BOTH the stored label and the live lookup value.';


-- ---------------------------------------------------------------------------
-- 1. feed_ration_groups -- breed label -> ration group
-- ---------------------------------------------------------------------------
-- Adult breeds only. Carries the Beetal + Sirohi -> 'Beetal/Sirohi' merge, which is exactly why
-- this is data rather than an identity mapping over goats.breed. Kids bypass this table
-- entirely (see the FOR KIDS THE BREED IS IGNORED note above).
CREATE TABLE feed_ration_groups (
    ration_group_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
    breed_label         text NOT NULL,
    breed_key           text GENERATED ALWAYS AS (feed_config_norm(breed_label)) STORED,
    ration_group_label  text NOT NULL,
    ration_group_key    text GENERATED ALWAYS AS (feed_config_norm(ration_group_label)) STORED,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_ration_groups_breed_label_not_blank CHECK (btrim(breed_label) <> ''),
    CONSTRAINT feed_ration_groups_group_label_not_blank CHECK (btrim(ration_group_label) <> '')
);

CREATE UNIQUE INDEX feed_ration_groups_natural_key_uidx
    ON feed_ration_groups (tenant_id, breed_key);

CREATE INDEX feed_ration_groups_group_idx
    ON feed_ration_groups (tenant_id, ration_group_key);

COMMENT ON TABLE feed_ration_groups IS
  'Maps a live goats.breed label to its ration group. Adult breeds only -- kids resolve to the fixed ''Kid'' group by age band and never consult this table. Carries the Beetal/Sirohi merge.';


-- ---------------------------------------------------------------------------
-- 2. feed_shed_tags -- tenant-scoped tag vocabulary
-- ---------------------------------------------------------------------------
-- The authored vocabulary the ration grid is indexed by, matched against live
-- goats.management_stage. applies_to records whether a tag belongs to the kid course or the
-- adult course; the two sets are disjoint in the source grid (17 kid, 14 adult, 31 total).
CREATE TABLE feed_shed_tags (
    shed_tag_id    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants (tenant_id),
    shed_tag_label text NOT NULL,
    shed_tag_key   text GENERATED ALWAYS AS (feed_config_norm(shed_tag_label)) STORED,
    applies_to     text NOT NULL,
    display_order  integer NOT NULL DEFAULT 0,
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_shed_tags_label_not_blank CHECK (btrim(shed_tag_label) <> ''),
    CONSTRAINT feed_shed_tags_applies_to_check CHECK (applies_to = ANY (ARRAY['adult'::text, 'kid'::text])),
    CONSTRAINT feed_shed_tags_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_shed_tags_natural_key_uidx
    ON feed_shed_tags (tenant_id, shed_tag_key);

COMMENT ON TABLE feed_shed_tags IS
  'Authored shed-tag vocabulary the ration grid is indexed by, matched against live goats.management_stage via feed_config_norm. applies_to splits the kid course from the adult course.';


-- ---------------------------------------------------------------------------
-- 3. feed_item_catalog -- the feed items themselves
-- ---------------------------------------------------------------------------
-- Nutritional/handling attributes are NULLABLE on purpose: unlike a ration rate, an unknown
-- energy value does not silently under-feed an animal, it just means an energy rollup cannot be
-- computed for that item. That is a reportable gap, not a feeding decision.
CREATE TABLE feed_item_catalog (
    feed_item_id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    feed_item_label    text NOT NULL,
    feed_item_key      text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    energy_kcal_per_kg numeric(10, 3),
    dry_matter_factor  numeric(6, 4),
    wastage_factor     numeric(6, 4),
    display_order      integer NOT NULL DEFAULT 0,
    status             text NOT NULL DEFAULT 'active',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_item_catalog_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_item_catalog_energy_check CHECK (energy_kcal_per_kg IS NULL OR energy_kcal_per_kg >= 0),
    CONSTRAINT feed_item_catalog_dry_matter_check CHECK (dry_matter_factor IS NULL OR (dry_matter_factor > 0 AND dry_matter_factor <= 1)),
    CONSTRAINT feed_item_catalog_wastage_check CHECK (wastage_factor IS NULL OR (wastage_factor >= 0 AND wastage_factor < 1)),
    CONSTRAINT feed_item_catalog_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_item_catalog_natural_key_uidx
    ON feed_item_catalog (tenant_id, feed_item_key);

COMMENT ON TABLE feed_item_catalog IS
  'Feed items and their nutritional/handling attributes. Attributes are nullable because a missing energy value blocks only a rollup, never a feeding decision -- unlike feed_ration_rates.grams_per_head, which is NOT NULL.';


-- ---------------------------------------------------------------------------
-- 4. feed_ration_rates -- THE EDITABLE GRID
-- ---------------------------------------------------------------------------
-- (tenant, park, ration_group, shed_tag, feed_item) -> grams per head per day.
--
-- Effective-dated: a rate change CLOSES the current row (valid_to = the day the change takes
-- effect) and OPENS a new one, so "what were we feeding Osmanabadi/Pregnant at CBE last March"
-- stays answerable. An in-place UPDATE of grams_per_head would destroy that audit trail and is
-- not the intended edit path.
--
-- grams_per_head is NOT NULL with NO DEFAULT: 0 is authored ("K0 kids are on milk"), and absence
-- of a row is the ONLY encoding of "not configured", which the read path must treat as blocking.
-- See the CONFIGURED ZERO block at the top of this migration.
CREATE TABLE feed_ration_rates (
    ration_rate_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id            uuid NOT NULL,
    ration_group_label text NOT NULL,
    ration_group_key   text GENERATED ALWAYS AS (feed_config_norm(ration_group_label)) STORED,
    shed_tag_label     text NOT NULL,
    shed_tag_key       text GENERATED ALWAYS AS (feed_config_norm(shed_tag_label)) STORED,
    feed_item_label    text NOT NULL,
    feed_item_key      text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    grams_per_head     numeric(12, 3) NOT NULL,
    valid_from         date NOT NULL DEFAULT CURRENT_DATE,
    valid_to           date,
    source_system      text NOT NULL DEFAULT 'manual',
    created_by         uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_ration_rates_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_ration_rates_group_label_not_blank CHECK (btrim(ration_group_label) <> ''),
    CONSTRAINT feed_ration_rates_tag_label_not_blank CHECK (btrim(shed_tag_label) <> ''),
    CONSTRAINT feed_ration_rates_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    -- >= 0, NOT > 0: zero is a legitimate authored rate (milk-fed kids).
    CONSTRAINT feed_ration_rates_grams_check CHECK (grams_per_head >= 0),
    CONSTRAINT feed_ration_rates_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- Natural key including valid_from: one authored rate per key per effective date.
CREATE UNIQUE INDEX feed_ration_rates_natural_key_uidx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key, feed_item_key, valid_from);

-- At most ONE open (current) rate per key. Without this, two open rows would make the lookup
-- ambiguous and a feed sheet non-deterministic.
CREATE UNIQUE INDEX feed_ration_rates_open_row_uidx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key, feed_item_key)
    WHERE valid_to IS NULL;

-- Hot read: resolve every feed item's rate for one (park, ration_group, shed_tag) in one indexed
-- scan of the currently-open rows. This is the access pattern the daily feed sheet runs per shed.
CREATE INDEX feed_ration_rates_current_lookup_idx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key)
    INCLUDE (feed_item_key, grams_per_head)
    WHERE valid_to IS NULL;

-- As-of read: the same lookup for a historical business date (audit / back-dated recompute).
CREATE INDEX feed_ration_rates_asof_lookup_idx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key, valid_from DESC);

COMMENT ON TABLE feed_ration_rates IS
  'The editable ration grid: (tenant, park, ration_group, shed_tag, feed_item) -> grams per head per day. Park-scoped because CBE and CPT genuinely differ. Effective-dated so a rate change is auditable rather than destructive.';
COMMENT ON COLUMN feed_ration_rates.grams_per_head IS
  'Authored grams per head per day. NOT NULL with no default: 0 means "authored as zero" (milk-fed K0/K1 kids) and a MISSING ROW means "not configured" -- a blocking gap the read path must surface, never COALESCE to 0.';
COMMENT ON COLUMN feed_ration_rates.valid_to IS
  'NULL = currently in force. A rate change closes this row and inserts a new one; it does not UPDATE grams_per_head in place.';


-- ---------------------------------------------------------------------------
-- 5. feed_shed_factors -- per-shed multiplier
-- ---------------------------------------------------------------------------
-- The third term of head_count x grams_per_head x shed_factor. Effective-dated for the same
-- audit reason as the rates. Default 1.0 applies to a row being INSERTED without an explicit
-- multiplier -- it is NOT a fallback for a missing row. A shed with no factor row is treated as
-- 1.0 by the read path, which is safe (it cannot zero out a ration); a factor of 0 must be
-- authored explicitly.
CREATE TABLE feed_shed_factors (
    shed_factor_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id         uuid NOT NULL,
    shed_id         uuid NOT NULL,
    feed_item_label text NOT NULL,
    feed_item_key   text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    multiplier      numeric(8, 4) NOT NULL DEFAULT 1.0,
    valid_from      date NOT NULL DEFAULT CURRENT_DATE,
    valid_to        date,
    created_by      uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_shed_factors_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_shed_factors_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_shed_factors_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_shed_factors_multiplier_check CHECK (multiplier >= 0),
    CONSTRAINT feed_shed_factors_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE UNIQUE INDEX feed_shed_factors_natural_key_uidx
    ON feed_shed_factors (tenant_id, park_id, shed_id, feed_item_key, valid_from);

CREATE UNIQUE INDEX feed_shed_factors_open_row_uidx
    ON feed_shed_factors (tenant_id, park_id, shed_id, feed_item_key)
    WHERE valid_to IS NULL;

CREATE INDEX feed_shed_factors_current_lookup_idx
    ON feed_shed_factors (tenant_id, park_id, shed_id)
    INCLUDE (feed_item_key, multiplier)
    WHERE valid_to IS NULL;

COMMENT ON TABLE feed_shed_factors IS
  'Per-shed, per-feed-item multiplier -- the shed_factor term of head_count x grams_per_head x shed_factor. Effective-dated. A missing row reads as 1.0 (safe); a 0 multiplier must be authored explicitly.';


-- ---------------------------------------------------------------------------
-- 6. feed_session_templates -- how the daily quantity is split across sessions
-- ---------------------------------------------------------------------------
-- The daily quantity computed above is divided across the park's feeding sessions by
-- split_fraction. The fractions for one park are expected to sum to 1.0; that is a cross-row
-- invariant a table CHECK cannot express, so it is enforced by the writer (the seed command
-- validates it) and must be re-validated by any future session-template editor.
CREATE TABLE feed_session_templates (
    session_template_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id             uuid NOT NULL,
    session_no          integer NOT NULL,
    session_label       text NOT NULL,
    split_fraction      numeric(6, 4) NOT NULL,
    display_order       integer NOT NULL DEFAULT 0,
    status              text NOT NULL DEFAULT 'active',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_session_templates_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_session_templates_session_no_check CHECK (session_no >= 1),
    CONSTRAINT feed_session_templates_label_not_blank CHECK (btrim(session_label) <> ''),
    CONSTRAINT feed_session_templates_split_check CHECK (split_fraction > 0 AND split_fraction <= 1),
    CONSTRAINT feed_session_templates_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_session_templates_natural_key_uidx
    ON feed_session_templates (tenant_id, park_id, session_no);

CREATE INDEX feed_session_templates_park_order_idx
    ON feed_session_templates (tenant_id, park_id, display_order, session_no);

COMMENT ON TABLE feed_session_templates IS
  'Per-park feeding-session ordering and the fraction of the daily quantity each session carries. split_fraction across a park''s active sessions is expected to sum to 1.0 -- a cross-row invariant enforced by the writer, not by a CHECK.';


-- ---------------------------------------------------------------------------
-- 7. feed_conversions -- inter-feed substitution matrix
-- ---------------------------------------------------------------------------
-- ratio = how many kg of to_item replace 1 kg of from_item. Directional: A->B and B->A are two
-- rows and are NOT required to be reciprocal, because a substitution is a nutritional judgement
-- rather than arithmetic.
CREATE TABLE feed_conversions (
    conversion_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants (tenant_id),
    from_item_label text NOT NULL,
    from_item_key   text GENERATED ALWAYS AS (feed_config_norm(from_item_label)) STORED,
    to_item_label   text NOT NULL,
    to_item_key     text GENERATED ALWAYS AS (feed_config_norm(to_item_label)) STORED,
    ratio           numeric(12, 6) NOT NULL,
    notes           text,
    status          text NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_conversions_from_label_not_blank CHECK (btrim(from_item_label) <> ''),
    CONSTRAINT feed_conversions_to_label_not_blank CHECK (btrim(to_item_label) <> ''),
    -- A substitution must actually substitute: ratio 0 would silently drop the feed.
    CONSTRAINT feed_conversions_ratio_check CHECK (ratio > 0),
    CONSTRAINT feed_conversions_not_self CHECK (feed_config_norm(from_item_label) <> feed_config_norm(to_item_label)),
    CONSTRAINT feed_conversions_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_conversions_natural_key_uidx
    ON feed_conversions (tenant_id, from_item_key, to_item_key);

CREATE INDEX feed_conversions_from_idx
    ON feed_conversions (tenant_id, from_item_key)
    WHERE status = 'active';

COMMENT ON TABLE feed_conversions IS
  'Directional inter-feed substitution matrix: kg of to_item that replace 1 kg of from_item. A->B and B->A are separate rows and need not be reciprocal.';


-- ---------------------------------------------------------------------------
-- 8. feed_experiment_config -- hand-entered experiment sheds
-- ---------------------------------------------------------------------------
-- Experiment sheds are NOT computed from the ration grid. An operator hand-enters ABSOLUTE kg
-- per feed item for the whole shed, so absolute_kg is a shed total, NOT a per-head rate, and
-- head_count is informational only -- it must never be multiplied into absolute_kg. That is the
-- single most important distinction between this table and feed_ration_rates.
--
-- Grain is (tenant, park, shed, feed_item). head_count and experiment_category describe the SHED
-- and are therefore repeated across that shed's item rows; a writer must keep them consistent
-- for a given shed (no single-column CHECK can express a cross-row invariant).
--
-- absolute_kg follows the same zero-vs-missing rule as grams_per_head: NOT NULL, no default,
-- 0 means "authored as zero" and a missing row means "not configured" (blocking).
CREATE TABLE feed_experiment_config (
    experiment_config_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id              uuid NOT NULL,
    shed_id              uuid NOT NULL,
    feed_item_label      text NOT NULL,
    feed_item_key        text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    absolute_kg          numeric(12, 3) NOT NULL,
    head_count           integer,
    experiment_category  text NOT NULL,
    notes                text,
    status               text NOT NULL DEFAULT 'active',
    created_by           uuid,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_experiment_config_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_experiment_config_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_experiment_config_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_experiment_config_category_not_blank CHECK (btrim(experiment_category) <> ''),
    CONSTRAINT feed_experiment_config_absolute_kg_check CHECK (absolute_kg >= 0),
    CONSTRAINT feed_experiment_config_head_count_check CHECK (head_count IS NULL OR head_count >= 0),
    CONSTRAINT feed_experiment_config_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_experiment_config_natural_key_uidx
    ON feed_experiment_config (tenant_id, park_id, shed_id, feed_item_key);

CREATE INDEX feed_experiment_config_shed_lookup_idx
    ON feed_experiment_config (tenant_id, park_id, shed_id)
    INCLUDE (feed_item_key, absolute_kg)
    WHERE status = 'active';

COMMENT ON TABLE feed_experiment_config IS
  'Hand-entered experiment sheds. absolute_kg is a SHED TOTAL in kg, never a per-head rate; head_count is informational and must not be multiplied into it.';
COMMENT ON COLUMN feed_experiment_config.absolute_kg IS
  'Absolute kg for the whole shed. NOT NULL with no default: 0 means "authored as zero", a missing row means "not configured" and must block rather than read as 0.';
COMMENT ON COLUMN feed_experiment_config.head_count IS
  'Informational shed head count. NOT a multiplier -- absolute_kg is already the shed total.';



-- -----------------------------------------------------------------------------
-- Collapsed from 000012_feed_schedule_config.sql
-- -----------------------------------------------------------------------------
-- Feed schedule configuration: the CLOCK that drives daily feed dispatch, plus the write ledger
-- that makes the authored feed config editable from the app under the mandatory idempotency
-- contract.
--
-- WHY A FORWARD MIGRATION (not an edit to 000003):
-- the migrator is forward-only. 000003 has already been applied and checksum-tracked on dev, so
-- editing it would leave those databases silently missing everything below. Same rule that
-- produced 000002 and 000003.
--
-- ---------------------------------------------------------------------------
-- 1. feed_schedule_config -- WHEN the day's feed direction happens
-- ---------------------------------------------------------------------------
-- 000003 answered "how much"; this answers "when". One row per (tenant, park, workflow) carries
-- three clock times:
--
--   direction_time   -- when the day's packing direction is ISSUED to the shed.
--   correction_time  -- when emergency-shifting corrections are BATCHED and reissued.
--   transport_time   -- the deadline after which a correction can no longer reach the shed.
--
-- WHY THIS IS PER-WORKFLOW AND NOT ONE GLOBAL SETTING
-- ---------------------------------------------------
-- The 'normal' and 'experiment' workflows genuinely run on different clocks: normal packing is
-- directed first thing in the morning (07:00), while experiment sheds are hand-entered and are
-- directed in the afternoon (14:00). A single tenant- or park-global direction_time would have to
-- pick one of the two, and whichever it picked would issue the other workflow's direction at the
-- wrong hour every single day. So `workflow` is part of the natural key, not a filter column.
--
-- WHY CORRECTIONS ARE BATCHED AT A FIXED TIME, NOT FIRED ON APPROVAL
-- ------------------------------------------------------------------
-- MAINTAINER DECISION: an emergency shifting approved during the day does NOT immediately reissue
-- that shed's direction. Corrections accumulate and are reissued ONCE, at correction_time (14:00).
--
-- The alternative -- fire-on-approval -- was rejected because it races: three approvals in one
-- morning produce three amended directions for the same shed, arriving in whatever order the
-- consumer happens to process them, and the shed staff cannot tell which sheet is current. One
-- amended direction per shed per day is unambiguous by construction. correction_time is therefore
-- a business decision stored as config, not a scheduling implementation detail.
--
-- transport_time is the point past which a correction is pointless: the feed has left. It is
-- NULLABLE because a park that has not yet declared its transport cutoff has no honest value for
-- it, and inventing one would make a late correction look deliverable. NULL means "no declared
-- cutoff", which the read path must treat as "unknown", never as "no deadline".
--
-- TIME SEMANTICS -- these are INDIA BUSINESS CALENDAR (Asia/Kolkata) LOCAL times
-- ------------------------------------------------------------------------------
-- Per AGENTS.md, every business meaning derived from an instant converts to Asia/Kolkata first;
-- UTC never defines a Goat OS business day. These columns are `time` (time WITHOUT time zone) and
-- carry NO offset on purpose. 07:00 means seven in the morning at the park, on whatever date the
-- reader is scheduling, forever -- it is a recurring wall-clock rule, not an instant.
--
-- Storing `timetz` or a UTC-shifted 01:30 would be actively wrong here: it would bind a recurring
-- business rule to a fixed offset, and it would make the stored value unreadable to the operator
-- who authored "7 AM". The consumer combines (business date, this local time, Asia/Kolkata) to get
-- the instant. Do not add a UTC offset column.
CREATE TABLE feed_schedule_config (
    feed_schedule_config_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id                 uuid NOT NULL,
    workflow                text NOT NULL,
    direction_time          time NOT NULL,
    correction_time         time NOT NULL,
    transport_time          time,
    valid_from              date NOT NULL DEFAULT CURRENT_DATE,
    valid_to                date,
    created_by              uuid,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_schedule_config_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_schedule_config_workflow_check CHECK (workflow = ANY (ARRAY['normal'::text, 'experiment'::text])),
    -- A correction amends a direction that was already issued, so it cannot precede it. Equality is
    -- legal and is the live experiment case (direction 14:00, correction 14:00): the first
    -- direction of the day already carries that day's approved corrections.
    CONSTRAINT feed_schedule_config_correction_order_check CHECK (correction_time >= direction_time),
    -- Transport is the deadline a correction races; a cutoff before the correction batch would make
    -- every correction dead on arrival.
    CONSTRAINT feed_schedule_config_transport_order_check CHECK (transport_time IS NULL OR transport_time >= correction_time),
    CONSTRAINT feed_schedule_config_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- Natural key including valid_from: one authored clock per (park, workflow) per effective date.
CREATE UNIQUE INDEX feed_schedule_config_natural_key_uidx
    ON feed_schedule_config (tenant_id, park_id, workflow, valid_from);

-- At most ONE open (current) clock per (park, workflow). Two open rows would make "when do we issue
-- today's direction" ambiguous, and the dispatcher would pick one arbitrarily.
CREATE UNIQUE INDEX feed_schedule_config_open_row_uidx
    ON feed_schedule_config (tenant_id, park_id, workflow)
    WHERE valid_to IS NULL;

-- Hot read: the dispatcher resolves one (park, workflow) clock per tick. Covering, so the current
-- row is answered from the index.
CREATE INDEX feed_schedule_config_current_lookup_idx
    ON feed_schedule_config (tenant_id, park_id, workflow)
    INCLUDE (direction_time, correction_time, transport_time)
    WHERE valid_to IS NULL;

-- As-of read: the same lookup for a historical business date (audit / back-dated recompute).
CREATE INDEX feed_schedule_config_asof_lookup_idx
    ON feed_schedule_config (tenant_id, park_id, workflow, valid_from DESC);

COMMENT ON TABLE feed_schedule_config IS
  'Per (tenant, park, workflow) feed dispatch clock: when the day''s direction is issued, when emergency-shifting corrections are batched and reissued, and the transport cutoff after which a correction cannot land. Per-workflow because normal (07:00) and experiment (14:00) genuinely differ. Effective-dated.';
COMMENT ON COLUMN feed_schedule_config.workflow IS
  'Which dispatch workflow this clock governs: ''normal'' (grid-computed sheds) or ''experiment'' (hand-entered sheds). Part of the natural key -- the two run on different cutoffs.';
COMMENT ON COLUMN feed_schedule_config.direction_time IS
  'LOCAL Asia/Kolkata wall-clock time the day''s packing direction is issued. No UTC offset is stored: this is a recurring business-calendar rule, not an instant. Combine with the business date in Asia/Kolkata to get the instant.';
COMMENT ON COLUMN feed_schedule_config.correction_time IS
  'LOCAL Asia/Kolkata wall-clock time at which approved emergency-shifting corrections are BATCHED and the amended direction is reissued. Maintainer decision: batched at a fixed time rather than fired on approval, so a shed receives at most one amended direction per day instead of several racing ones.';
COMMENT ON COLUMN feed_schedule_config.transport_time IS
  'LOCAL Asia/Kolkata wall-clock deadline after which a correction can no longer reach the shed. NULLABLE: NULL means the park has not declared a cutoff and must read as UNKNOWN, never as "no deadline".';
COMMENT ON COLUMN feed_schedule_config.valid_to IS
  'NULL = currently in force. A clock change closes this row and inserts a new one; it does not UPDATE the times in place.';


-- ---------------------------------------------------------------------------
-- 2. feed_config_write_log -- the idempotency + audit ledger for authored edits
-- ---------------------------------------------------------------------------
-- 000003's config tables are now EDITABLE from the app (/feed-config/* write routes), and
-- AGENTS.md makes idempotency a mandatory write-path contract: every mutating endpoint persists a
-- client idempotency key AND a request fingerprint in the SAME transaction as its side effects,
-- returns the original result for an exact replay without rerunning them, and rejects a
-- same-key/different-payload replay.
--
-- WHY A LEDGER TABLE RATHER THAN idempotency_key COLUMNS ON EACH CONFIG TABLE
-- ---------------------------------------------------------------------------
-- counts carries idempotency_key/request_fingerprint on the written row itself (see
-- count_projection_exception_resolutions), which works because one write produces exactly one new
-- row. An effective-dated config edit does not: superseding a rate CLOSES an existing row and
-- INSERTS a new one, so the write's identity spans two rows and belongs to neither. Hanging the
-- key off the new row would also lose the key entirely for the "unchanged" outcome, which writes
-- no row at all and must still replay identically.
--
-- So the ledger records the WRITE, not the row: one entry per accepted authored edit, carrying the
-- key, the fingerprint, what the edit did, and which rows it produced/closed. That makes replay a
-- single indexed lookup and gives the config surface an audit trail of who changed what and when,
-- which the config tables' own created_by cannot express for a supersede.
--
-- The ledger row is written INSIDE the same transaction as the close/insert. There is no
-- "best-effort afterwards" path: an entry that exists is proof the side effects committed.
CREATE TABLE feed_config_write_log (
    feed_config_write_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants (tenant_id),
    -- Which authored surface was edited. Part of the audit record, NOT of the idempotency key:
    -- uniqueness is (tenant, key) alone, so one client key can never mean two different writes.
    write_kind           text NOT NULL,
    idempotency_key      text NOT NULL,
    request_fingerprint  text NOT NULL,
    -- What the edit actually did, using the same vocabulary as seed-feed-ration's reconciliation:
    --   inserted   -- no open row existed; a first authored value was created.
    --   superseded -- an open row authored on an EARLIER day was closed and a new one opened.
    --   corrected  -- an open row authored TODAY was corrected in place (a same-day re-author
    --                 cannot be given a window without violating valid_to > valid_from).
    --   unchanged  -- the authored value already matched; no row was written.
    outcome              text NOT NULL,
    -- The row now in force after this write. NULL only for 'unchanged' writes that matched a row
    -- the caller did not address by id.
    result_row_id        uuid,
    -- The row this write closed, for 'superseded'. NULL otherwise -- this is what makes the
    -- effective-dated history walkable from the ledger.
    superseded_row_id    uuid,
    -- The business date (Asia/Kolkata) the edit took effect on -- the new row's valid_from and the
    -- closed row's valid_to. Stored so the ledger is readable without joining the config tables.
    effective_from       date NOT NULL,
    actor_ref            text NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_config_write_log_kind_check CHECK (write_kind = ANY (ARRAY['ration_rate'::text, 'shed_factor'::text, 'schedule_config'::text])),
    CONSTRAINT feed_config_write_log_outcome_check CHECK (outcome = ANY (ARRAY['inserted'::text, 'superseded'::text, 'corrected'::text, 'unchanged'::text])),
    CONSTRAINT feed_config_write_log_idem_check CHECK ((btrim(idempotency_key) <> ''::text) AND (btrim(request_fingerprint) <> ''::text)),
    CONSTRAINT feed_config_write_log_actor_check CHECK (btrim(actor_ref) <> ''::text),
    -- A supersede must name BOTH ends of the window it created, or the history it claims to
    -- preserve cannot be reconstructed.
    CONSTRAINT feed_config_write_log_supersede_shape_check CHECK (
        (outcome <> 'superseded'::text) OR (result_row_id IS NOT NULL AND superseded_row_id IS NOT NULL)
    ),
    CONSTRAINT feed_config_write_log_insert_shape_check CHECK (
        (outcome NOT IN ('inserted'::text, 'corrected'::text)) OR result_row_id IS NOT NULL
    )
);

-- THE idempotency index. Its violation is how a concurrent duplicate is detected: the second
-- transaction's INSERT fails on this constraint, re-reads the committed entry, and returns the
-- original result instead of applying the edit twice.
CREATE UNIQUE INDEX feed_config_write_log_idempotency_uidx
    ON feed_config_write_log (tenant_id, idempotency_key);

-- Audit read: "what was edited on this surface recently", newest first.
CREATE INDEX feed_config_write_log_kind_recent_idx
    ON feed_config_write_log (tenant_id, write_kind, created_at DESC);

COMMENT ON TABLE feed_config_write_log IS
  'Idempotency + audit ledger for authored feed-config edits. One entry per accepted write, written in the SAME transaction as the effective-dated close/insert it describes. Exists as a ledger rather than as key columns on each config table because a supersede spans two rows and an unchanged write spans none.';
COMMENT ON COLUMN feed_config_write_log.request_fingerprint IS
  'Stable hash of the canonical client request. An exact replay hashes identically and returns the original result; a same-key/different-payload replay hashes differently and is rejected as a conflict.';
COMMENT ON COLUMN feed_config_write_log.outcome IS
  'inserted | superseded | corrected | unchanged -- the same three-way reconciliation seed-feed-ration performs, so the UI and the seed cannot disagree about what an edit means.';



-- -----------------------------------------------------------------------------
-- Collapsed from 000013_feed_session_template_items.sql
-- -----------------------------------------------------------------------------
-- Two defects found by running /feed-direction/preview against live seeded data. Both are about
-- the same thing: WHICH feed items a direction row is allowed to name.
--
-- WHY A FORWARD MIGRATION (not an edit to 000003):
-- the migrator is forward-only. 000001-000004 are applied and checksum-tracked on dev/stg, so
-- editing historical SQL would leave those databases silently missing everything below. Same rule
-- that produced 000002, 000003, and 000004.
--
-- ===========================================================================
-- DEFECT 1 -- the direction generated feed nobody packs
-- ===========================================================================
-- 000003 modelled feed_session_templates as (park, session_no, session_label, split_fraction) and
-- stopped there. It recorded HOW MUCH of the day each session carries, but never WHICH feed items
-- that session actually consists of. With no such declaration, the generator had only one list of
-- items to walk -- the whole feed_item_catalog -- so every shed row asked for every feed item in
-- the tenant.
--
-- That is wrong on the live grid for a specific reason. Hybrid, COFS, Hedge Lucerne and Dry Maize
-- are not four feeds that are all served; they are inter-feed SUBSTITUTION alternatives (that is
-- what feed_conversions exists for). Exactly one roughage is fed. Walking the catalog produced,
-- on one real CBE run, 112 blocked cells (28 each for the four alternatives, which are legitimately
-- unauthored for ~12 of the 77 group/tag rows) plus 619 kg of "Hybrid" that no packer has a bag
-- for. Both symptoms are the same root cause: the catalog is the tenant's VOCABULARY of feeds, not
-- any park's RECIPE for a session.
--
-- The source workbook has always carried the recipe. Its `Template` tab declares, per farm per
-- session, exactly five numbered feed slots:
--
--   CBE / CPT, Session 1 and 2:
--     Feed 1 Concentrate | Feed 2 Dry Masoor Bhusa | Feed 3 Mesha Concentrate Goat
--     Feed 4 Mesha Concentrate Sheep | Feed 5 Baking Soda
--
-- feed_session_template_items below is that tab. The generator now emits ONLY the items declared
-- for the park+session it is generating, in slot_no order.
--
-- THIS DOES NOT WEAKEN THE BLOCKED-VS-ZERO RULE. Read 000003's CONFIGURED ZERO block again: it
-- governs what happens when a REQUESTED item has no authored rate, and that is unchanged. A slot
-- that IS declared but whose (park, group, tag, item) cell has no currently-open rate still blocks
-- loudly and still contributes no number. What changed is only which items are requested:
--
--   declared slot, authored rate      -> resolved (0 is a legal authored rate)
--   declared slot, NO authored rate   -> BLOCKED, exactly as before   <-- unchanged
--   catalog item, never declared      -> not requested, so absent entirely -- not blocked,
--                                        because nobody ever said this park feeds it
--
-- The last line is the fix, and it is a narrowing of the QUESTION, not a softening of the ANSWER.
-- "This park does not feed Hybrid" and "this park feeds Hybrid but nobody said how much" are
-- different states and must not render identically; before this migration they did, and the real
-- gaps were buried under 112 cells of noise.
--
-- WHY THE SLOTS HANG OFF THE SESSION AND NOT OFF THE PARK
-- -------------------------------------------------------
-- Both live parks currently declare the same five slots in both sessions, so a park-level list
-- would fit today's data. It is modelled per session anyway because the workbook models it per
-- session: the Template tab has a row per session precisely so a farm CAN feed concentrate in the
-- morning and roughage in the evening. Collapsing that to a park list would make the first such
-- change a migration instead of a config edit, and would silently split the wrong feed across the
-- wrong session in the meantime.
--
-- The experiment workbook's Template tab declares a DIFFERENT five (Mesha Concentrate Goat |
-- Mesha Concentrate Sheep | RGS Concentrate | Vijay Concentrate | Dry Masoor Bhusa). This table
-- represents that shape fine -- it is just another (park, session) slot set. It is deliberately
-- NOT seeded: feed_experiment_config has no source rows yet, and seeding a recipe for sheds that
-- do not exist would author config nobody can trace to a farm decision. Experiment sheds also do
-- not consult this table at all -- their hand-entered cells ARE their complete item list (see
-- domain.ExperimentPlanner), which is why the generator scopes slots per planner rather than
-- globally.
--
-- EFFECTIVE-DATED, like feed_ration_rates and feed_schedule_config. Changing what a session
-- consists of is a real farm decision with a date, and "what were we packing for CBE session 2 in
-- March" must stay answerable. A change CLOSES the current row (valid_to = the day the change
-- takes effect) and OPENS a new one; it is not an in-place UPDATE of feed_item_label.
CREATE TABLE feed_session_template_items (
    session_template_item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id                  uuid NOT NULL,
    session_no               integer NOT NULL,
    -- The workbook's "Feed 1..Feed 5" column position. It is the PACKING ORDER, not a mere display
    -- hint: the direction sheet and the packing worklist both render items in this sequence, so a
    -- packer's bags come out in the order the sheet lists them.
    slot_no                  integer NOT NULL,
    feed_item_label          text NOT NULL,
    feed_item_key            text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    status                   text NOT NULL DEFAULT 'active',
    valid_from               date NOT NULL DEFAULT CURRENT_DATE,
    valid_to                 date,
    created_by               uuid,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_session_template_items_park_fk
        FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    -- A slot for a session that does not exist is unusable: it declares an item for a share of the
    -- day that has no split_fraction, so it could never be given a quantity. Unlike the label FKs
    -- this schema deliberately omits (rates do not FK the catalog either -- an authored label is
    -- allowed to name a feed the catalog has not caught up with, and a missing rate is what blocks,
    -- not a missing catalog row), this one is a structural impossibility rather than a data gap.
    CONSTRAINT feed_session_template_items_session_fk
        FOREIGN KEY (tenant_id, park_id, session_no)
        REFERENCES feed_session_templates (tenant_id, park_id, session_no),
    CONSTRAINT feed_session_template_items_session_no_check CHECK (session_no >= 1),
    CONSTRAINT feed_session_template_items_slot_no_check CHECK (slot_no >= 1),
    CONSTRAINT feed_session_template_items_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_session_template_items_status_check
        CHECK (status = ANY (ARRAY['active'::text, 'retired'::text])),
    CONSTRAINT feed_session_template_items_window_check
        CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- Natural key including valid_from: one authored item per (park, session, slot) per effective date.
CREATE UNIQUE INDEX feed_session_template_items_natural_key_uidx
    ON feed_session_template_items (tenant_id, park_id, session_no, slot_no, valid_from);

-- At most ONE open (current) row per slot. Two open rows would make "what is Feed 3 of CBE session
-- 1" ambiguous and the packing order non-deterministic.
CREATE UNIQUE INDEX feed_session_template_items_open_slot_uidx
    ON feed_session_template_items (tenant_id, park_id, session_no, slot_no)
    WHERE valid_to IS NULL;

-- The SAME feed item may not occupy two live slots of one session. This is not tidiness: the
-- generator emits one line per declared slot, so a duplicate would print the shed's concentrate
-- twice and the packer would fill two bags of it. Retiring a slot frees the item to be re-declared
-- elsewhere, which is why status is in the predicate.
CREATE UNIQUE INDEX feed_session_template_items_open_item_uidx
    ON feed_session_template_items (tenant_id, park_id, session_no, feed_item_key)
    WHERE valid_to IS NULL AND status = 'active';

-- Hot read: the whole park's slot set for one business date, in one indexed scan, once per
-- generation request. Covering, so the recipe is answered from the index.
CREATE INDEX feed_session_template_items_current_lookup_idx
    ON feed_session_template_items (tenant_id, park_id, session_no, slot_no)
    INCLUDE (feed_item_label, feed_item_key)
    WHERE valid_to IS NULL AND status = 'active';

-- As-of read: the same lookup for a historical business date (audit / back-dated regeneration).
CREATE INDEX feed_session_template_items_asof_lookup_idx
    ON feed_session_template_items (tenant_id, park_id, session_no, valid_from DESC);

COMMENT ON TABLE feed_session_template_items IS
  'The source workbook''s Template tab: which feed items each (park, session) actually consists of, in packing-slot order. The generator emits ONLY these items -- feed_item_catalog is the tenant''s feed VOCABULARY, not any park''s session RECIPE. Narrowing which items are requested does NOT weaken 000003''s blocked-vs-zero rule: a declared slot with no authored rate still blocks.';
COMMENT ON COLUMN feed_session_template_items.slot_no IS
  'The workbook''s Feed 1..Feed 5 position. Packing order, not a display hint: the direction sheet and the packing worklist both render items in this sequence.';
COMMENT ON COLUMN feed_session_template_items.feed_item_label IS
  'The authored feed name, joined to feed_ration_rates.feed_item_label through the generated feed_item_key. A declared item whose (park, group, tag, item) cell has no currently-open rate BLOCKS the shed -- it is never fed 0.';
COMMENT ON COLUMN feed_session_template_items.valid_to IS
  'NULL = currently in force. Changing what a session consists of closes this row and inserts a new one; it does not UPDATE feed_item_label in place.';


-- ===========================================================================
-- DEFECT 2 -- feed item labels were spreadsheet column headers, not feed names
-- ===========================================================================
-- The catalog and the grid were loaded straight from the source VALIDATION sheet's column headers,
-- which read "Concentrate Per Goat", "Dry Masoor Bhusa Per Goat", "Hybrid Per Goat", and so on.
-- "Per Goat" is that sheet's UNIT descriptor -- it tells a reader the column holds a per-head
-- figure -- and it is not part of any feed's name. Nobody at the farm orders "Baking Soda Per
-- Goat"; they order baking soda.
--
-- The unit is already carried separately and correctly by the app contract ("Grams / head / day"),
-- so the suffix was pure duplication in the one place it does damage: a packing sheet, where the
-- line an operator reads must be the name on the sack.
--
-- WHY THE RENAME IS A DATA MIGRATION AND NOT A CODE-SIDE DISPLAY TRIM
-- -------------------------------------------------------------------
-- feed_item_key is GENERATED ALWAYS from feed_item_label, and it is the JOIN KEY between the
-- catalog, the ration grid, the shed factors, the substitution matrix, and (from today) the session
-- slots. Trimming the suffix at render time would leave the stored keys as
-- 'concentrate_per_goat' while the new session slots key on 'concentrate' -- every rate lookup
-- would miss, and per 000003's rule a missed lookup BLOCKS. Renaming the label is therefore the
-- only way to move the key, and the label must move in every table that carries one, in the same
-- transaction, or the grid orphans itself.
--
-- Goose runs this file in a single transaction, so the statements below either all land or none
-- do; there is no window in which the catalog is renamed and the rates are not.
--
-- SAFE BY CONSTRUCTION: the strip is applied uniformly to every table's copy of the label, so two
-- items that were distinct stay distinct. All ten live labels remain distinct after the strip
-- (Concentrate, Mesha Concentrate Goat, Mesha Concentrate Sheep, Dry Masoor Bhusa, Toor Dal Bhusa
-- Pellet, Hybrid, COFS, Hedge Lucerne, Dry Maize, Baking Soda). If some other database did hold a
-- pair that collides under the strip, the natural-key unique indexes reject the UPDATE and this
-- migration fails -- which is the correct outcome, because silently merging two feeds' rates would
-- be a feeding error, not a naming one.
--
-- The pattern is anchored to the END of the label and requires whitespace before "per", so the
-- genuine names are untouched: "Mesha Concentrate Goat" and "Mesha Concentrate Sheep" do not end in
-- "Per Goat", and "Toor Dal Bhusa Pellet" contains neither word.
--
-- SCALE BOUNDARY (row-count assumption, do not remove without re-checking the live count):
-- each regexp UPDATE below runs unconditionally over its WHOLE table (WHERE is a value predicate
-- on feed_item_label, not a bounded key range), and all five run inside ONE goose transaction, so
-- the row locks are held for the combined duration of all five statements. This is safe today
-- because every table here is a small authored catalog/config table -- feed_ration_rates is the
-- largest at ~770 rows, and the other four are lower still (three are empty in production). At
-- that size a full-table regexp UPDATE completes in milliseconds and the lock window is
-- negligible.
--
-- This stops being safe once any of these tables crosses roughly 1,000 rows: a full-table rewrite
-- at that size starts to hold row/page locks long enough to contend with concurrent writers on a
-- live tenant. If a future migration needs to re-run a bulk label rewrite on a table past that
-- size, split it out of this single-transaction DO block into its own batched migration -- chunk
-- by primary-key range or `ctid`, commit each batch, and re-check the strip's collision safety
-- per batch (two labels that were distinct pre-strip must stay distinct) -- rather than adding a
-- sixth statement to this pattern.

-- +goose StatementBegin
DO $$
DECLARE
    -- Anchored at end-of-string; requires at least one space before 'per'. Case-insensitive so a
    -- hand-authored 'per goat' is caught too.
    strip_pattern CONSTANT text := '[[:space:]]+per[[:space:]]+goat[[:space:]]*$';
BEGIN
    -- 1. The catalog -- the vocabulary itself.
    UPDATE feed_item_catalog
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    -- 2. The ration grid -- carries the same string, so the same rename, or every rate orphans.
    UPDATE feed_ration_rates
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    -- 3-5. The remaining tables that carry a feed_item_label. They hold no rows today (they are
    -- authored in-app and have no source data yet), but they are renamed in the same transaction
    -- anyway: leaving them out would make the completeness of this migration depend on the
    -- accident of those tables being empty, and the first authored shed factor under an old label
    -- would join to nothing.
    UPDATE feed_shed_factors
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    UPDATE feed_experiment_config
    SET feed_item_label = regexp_replace(feed_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE feed_item_label ~* strip_pattern
      AND btrim(regexp_replace(feed_item_label, strip_pattern, '', 'i')) <> '';

    UPDATE feed_conversions
    SET from_item_label = regexp_replace(from_item_label, strip_pattern, '', 'i'),
        to_item_label = regexp_replace(to_item_label, strip_pattern, '', 'i'),
        updated_at = now()
    WHERE (from_item_label ~* strip_pattern OR to_item_label ~* strip_pattern)
      AND btrim(regexp_replace(from_item_label, strip_pattern, '', 'i')) <> ''
      AND btrim(regexp_replace(to_item_label, strip_pattern, '', 'i')) <> '';
END
$$;
-- +goose StatementEnd

-- POST-CONDITION: no rate may be orphaned by the rename. Every feed_ration_rates.feed_item_key must
-- still resolve to a feed_item_catalog row of the same tenant. This is asserted rather than assumed
-- because an orphaned rate is invisible at write time and only surfaces later as a blocked shed --
-- the exact failure mode 000003 exists to prevent. Failing the migration is the loud alternative.
-- +goose StatementBegin
DO $$
DECLARE
    orphans bigint;
BEGIN
    SELECT count(*) INTO orphans
    FROM feed_ration_rates r
    WHERE NOT EXISTS (
        SELECT 1 FROM feed_item_catalog c
        WHERE c.tenant_id = r.tenant_id AND c.feed_item_key = r.feed_item_key
    );
    IF orphans > 0 THEN
        RAISE EXCEPTION
            'feed item rename orphaned % feed_ration_rates row(s): a rate key no longer resolves to a feed_item_catalog row', orphans;
    END IF;
END
$$;
-- +goose StatementEnd



-- -----------------------------------------------------------------------------
-- Collapsed from 000014_feed_experiment_write_kind.sql
-- -----------------------------------------------------------------------------
-- Experiment sheds become AUTHORABLE. Until now feed_experiment_config existed (migration 000003)
-- and the generator honoured it (domain.ExperimentPlanner), but nothing could write it: the module
-- had no endpoint, the seed had no source rows, and the write ledger could not even record such an
-- edit. This migration opens the last of those three.
--
-- WHY A FORWARD MIGRATION (not an edit to 000004):
-- the migrator is forward-only. 000001-000005 are applied and checksum-tracked on dev/stg, so
-- editing historical SQL would leave those databases with a ledger that rejects every experiment
-- edit while the API happily accepts one. Same rule that produced 000002-000005.
--
-- ===========================================================================
-- WHAT THIS UNBLOCKS, AND WHY IT IS A CORRECTNESS FIX RATHER THAN A FEATURE
-- ===========================================================================
-- Membership in feed_experiment_config IS what makes a shed an experiment shed -- there is no
-- separate flag (see ExperimentPlanner.Applies). With the table empty, EVERY shed fell through to
-- NormalPlanner and was fed head_count x grams_per_head x shed_factor off the ration grid.
--
-- For the 34 sheds the farm actually runs as experiments that is the wrong arithmetic, not merely
-- the wrong label: their real feed is hand-entered ABSOLUTE kg per shed, and the source workbook
-- writes them into normal Feed Direction as EMPTY placeholders precisely so nobody computes them
-- per head. Measured against the workbook's own Feed Direction tab for 2026-07-20, feeding those
-- 34 sheds from the per-head grid inflated CBE's concentrate total from 182.0 kg to 398.8 kg --
-- about 2.2x. Excluding them, per-shed parity is already good (42 of 46 CBE sheds within 0.35 kg),
-- so the ration maths was never the defect; the missing experiment rows were.
--
-- ===========================================================================
-- THE CHANGE: one more write_kind
-- ===========================================================================
-- feed_config_write_log is the idempotency ledger EVERY authored feed-config write commits inside
-- its own transaction (see 000004 and feedconfig/adapters/postgres.runWrite). Its write_kind CHECK
-- is a closed vocabulary, so an experiment edit would fail the ledger insert -- and because the
-- ledger row and the side effect share one transaction, that failure correctly rolls the whole edit
-- back. The API would return a 500 on every experiment write. Widening the vocabulary is therefore
-- a precondition of the endpoint, not a cosmetic addition.
--
-- ONE kind covers both experiment writes ('experiment_config'):
--
--   * authoring one (park, shed, feed_item) cell -- absolute kg, informational head count, arm;
--   * switching a whole shed between the experiment and the normal grid, which is implemented as a
--     status flip on that shed's rows rather than a DELETE, so the authored quantities survive a
--     withdraw-and-restore and the audit trail keeps the numbers that were in force.
--
-- They are one kind because they are one authoring surface with one identity space; the ledger's
-- result_row_id and outcome already distinguish what an individual edit did.
--
-- NO NEW INDEX. The experiment listing reads
--   WHERE tenant_id = $1 AND park_id = $2 [AND status = $3] ORDER BY shed_id, feed_item_key
-- which is served by feed_experiment_config_natural_key_uidx
-- (tenant_id, park_id, shed_id, feed_item_key) -- leading columns match the predicate and the sort
-- is a prefix-ordered walk of the same index. The read is also bounded by construction: it is one
-- park's hand-authored sheds (34 rows x 5 items across BOTH live parks today), and the service
-- rejects an offset past 5000.
--
-- NOT CHANGED, deliberately: feed_experiment_config is NOT effective-dated, unlike feed_ration_rates
-- / feed_shed_factors / feed_schedule_config. An experiment quantity is a hand-entered figure for a
-- running trial, corrected in place while the trial runs, rather than a standing rule whose past
-- values must stay reconstructable to explain an old feed sheet. Adding valid_from/valid_to here
-- would imply an audit guarantee this table does not make; the write ledger records who changed
-- what and when, which is the guarantee it does make.

-- feed_config_write_log is an append-only, unboundedly-growing idempotency ledger (every authored
-- feed-config write commits a row here). A plain DROP + ADD CONSTRAINT re-validates the widened
-- CHECK against every existing row under an ACCESS EXCLUSIVE lock -- a full-table scan that blocks
-- reads/writes on the ledger for its duration and gets worse every day the ledger grows. Split it:
-- add the new CHECK NOT VALID (enforced on every subsequent INSERT immediately, no scan, brief
-- ACCESS EXCLUSIVE just to add the constraint metadata), then VALIDATE CONSTRAINT separately, which
-- takes only SHARE UPDATE EXCLUSIVE (blocks other DDL, not reads/writes) while it scans existing
-- rows. Unlike shifting_events (see 000007), feed_config_write_log has no forbid on direct
-- VALIDATE from validate-postgres-migrations -- it is a narrower, purely-additive ledger table, so
-- validating in the same migration is safe and keeps the constraint fully enforced immediately
-- rather than leaving it silently NOT VALID indefinitely.
ALTER TABLE feed_config_write_log
    DROP CONSTRAINT feed_config_write_log_kind_check;

ALTER TABLE feed_config_write_log
    ADD CONSTRAINT feed_config_write_log_kind_check
    CHECK (write_kind = ANY (ARRAY[
        'ration_rate'::text,
        'shed_factor'::text,
        'schedule_config'::text,
        'experiment_config'::text
    ])) NOT VALID;

ALTER TABLE feed_config_write_log
    VALIDATE CONSTRAINT feed_config_write_log_kind_check;

COMMENT ON COLUMN feed_config_write_log.write_kind IS
  'Which authored feed-config surface this ledger row describes. ''experiment_config'' covers both authoring an experiment shed''s absolute-kg cell and switching a shed between the experiment workflow and the normal ration grid -- one authoring surface, one identity space.';



-- -----------------------------------------------------------------------------
-- Collapsed from 000015_feed_direction_issues.sql
-- -----------------------------------------------------------------------------
-- Feed-direction ISSUES: the FROZEN, issued feed sheet and its lifecycle.
--
-- WHY A FORWARD MIGRATION (not an edit to 000003-000006):
-- the migrator is forward-only. 000003-000006 have already been applied and checksum-tracked on
-- dev/stg, so editing any of them would leave those databases silently missing everything below.
-- Same rule that produced 000002 through 000006.
--
-- ---------------------------------------------------------------------------
-- WHY THIS EXISTS -- Feed Direction stops being a live calculator
-- ---------------------------------------------------------------------------
-- Until now /feed-direction/preview LIVE-COMPUTED a sheet for ANY target_date on every request:
-- the projection is "live herd + approved-but-unexecuted shiftings" and nothing is dated into the
-- future, so asking for tomorrow, next week, or three months out all returned the same confident
-- number. Three defects followed: (1) tomorrow's sheet was viewable now as if it had been issued;
-- (2) a PAST sheet could not be retrieved, because nothing was ever stored; (3) the 14:00
-- correction the business runs had nothing to correct, because no issued artifact existed.
--
-- Feed for day D is produced on D-1, per (park, workflow), on the clock already stored in
-- feed_schedule_config (000004): direction_time ISSUES the sheet (generate once, freeze immutably),
-- correction_time AMENDS it (recompute, diff, persist the affected sheds only), transport_time
-- LOCKS it (no further change; later changes roll to the next feed day). This migration is the
-- durable record those three transitions write. The issue IS the operational-kernel
-- expected-process record: what sheet was promised, whether it was amended, and the evidence.
--
-- ---------------------------------------------------------------------------
-- 1. feed_direction_issues -- the issued sheet HEADER, one live row per (tenant, park, day, workflow)
-- ---------------------------------------------------------------------------
-- Per-workflow because 000004's dispatch clock is per-workflow: normal packing is directed at
-- 07:00 and the hand-entered experiment sheds at 14:00, so a park-day's full sheet is composed of
-- up to TWO issues that can be issued at different instants. The read path unions them.
--
-- The row is MUTATED IN PLACE across its lifecycle (issued -> amended -> locked): issued_at,
-- amended_at and locked_at accumulate on the one row, and amendment_count counts the corrections.
-- There is never more than one live issue per (tenant, park, feed_day, workflow) -- the partial
-- unique index below is that guarantee.
--
-- IDEMPOTENCY (AGENTS.md mandatory write-path contract). The worker re-runs the same issue/amend/
-- lock safely: idempotency_key is the stable operation identity ("issue:{tenant}:{park}:{day}:
-- {workflow}") and request_fingerprint == generation_input_fingerprint is the hash of the herd +
-- config the sheet was generated from. An EXACT re-issue (same key, same fingerprint) is a no-op
-- replay that returns the original and runs no side effects. A re-issue with a DIFFERENT fingerprint
-- is a legitimate fresh issue ONLY while state='issued' and nothing downstream has consumed the
-- sheet -- the service replaces the frozen rows in place. Once state is 'amended' or 'locked' a
-- plain re-issue with changed inputs is REJECTED: a correction or the transport lock has already
-- acted on the issued document, and changing it now must go through Amend, not a silent re-Issue.
CREATE TABLE feed_direction_issues (
    feed_direction_issue_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id                     uuid NOT NULL,
    feed_day                    date NOT NULL,
    workflow                    text NOT NULL,
    state                       text NOT NULL DEFAULT 'issued',
    -- Lifecycle instants. Business meaning is Asia/Kolkata (AGENTS.md); the service stamps them
    -- from a business-calendar clock, never from SQL now(). issued_at is always present; the other
    -- two fill in as the sheet is amended and then locked.
    issued_at                   timestamptz NOT NULL,
    amended_at                  timestamptz,
    locked_at                   timestamptz,
    -- Hash of the herd + config the sheet was generated from. A re-issue with an identical
    -- fingerprint is a no-op; an amend compares this against a fresh recompute to tell whether
    -- anything actually changed before writing a single row.
    generation_input_fingerprint text NOT NULL,
    -- The idempotency envelope. request_fingerprint == generation_input_fingerprint by construction
    -- (the request that produces an issue IS its generation inputs); it is stored under the contract
    -- name too so the replay/conflict logic reads the same as every other module's write path.
    idempotency_key             text NOT NULL,
    request_fingerprint         text NOT NULL,
    -- Provenance, mirroring the counts projection snapshots' source_contract/version columns.
    source_contract             text NOT NULL,
    source_contract_version     text NOT NULL,
    amendment_count             integer NOT NULL DEFAULT 0,
    generated_by                text NOT NULL,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_direction_issues_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_direction_issues_workflow_check CHECK (workflow = ANY (ARRAY['normal'::text, 'experiment'::text])),
    CONSTRAINT feed_direction_issues_state_check CHECK (state = ANY (ARRAY['issued'::text, 'amended'::text, 'locked'::text])),
    -- The lifecycle instants must exist for the state that implies them: an 'amended' sheet has an
    -- amended_at, a 'locked' sheet has a locked_at. A sheet locked without an intervening amend has
    -- a null amended_at, which is correct.
    CONSTRAINT feed_direction_issues_amended_shape_check CHECK (state <> 'amended' OR amended_at IS NOT NULL),
    CONSTRAINT feed_direction_issues_locked_shape_check CHECK (state <> 'locked' OR locked_at IS NOT NULL),
    CONSTRAINT feed_direction_issues_amendment_count_check CHECK (amendment_count >= 0),
    CONSTRAINT feed_direction_issues_fingerprint_check CHECK (btrim(generation_input_fingerprint) <> ''),
    CONSTRAINT feed_direction_issues_idem_check CHECK ((btrim(idempotency_key) <> ''::text) AND (btrim(request_fingerprint) <> ''::text)),
    CONSTRAINT feed_direction_issues_source_check CHECK ((btrim(source_contract) <> ''::text) AND (btrim(source_contract_version) <> ''::text)),
    CONSTRAINT feed_direction_issues_generated_by_check CHECK (btrim(generated_by) <> ''::text)
);

-- ONE LIVE ISSUE per (tenant, park, feed_day, workflow). Partial on the live state set so that a
-- future archival/superseded state can be added without colliding with this guarantee; today every
-- legal state is live, so this is effectively the natural key.
CREATE UNIQUE INDEX feed_direction_issues_live_uidx
    ON feed_direction_issues (tenant_id, park_id, feed_day, workflow)
    WHERE state IN ('issued', 'amended', 'locked');

-- THE idempotency index. Its violation is how a concurrent duplicate is detected: the second
-- transaction's INSERT fails here, re-reads the committed row, and returns the original result
-- instead of issuing twice.
CREATE UNIQUE INDEX feed_direction_issues_idempotency_uidx
    ON feed_direction_issues (tenant_id, idempotency_key);

-- Serving read: resolve every workflow's issue for one park + feed day in one indexed lookup. The
-- read path unions the (at most two) rows and reports their aggregated lifecycle.
CREATE INDEX feed_direction_issues_serve_idx
    ON feed_direction_issues (tenant_id, park_id, feed_day);

COMMENT ON TABLE feed_direction_issues IS
  'The issued feed-direction sheet header, one live row per (tenant, park, feed_day, workflow), mutated in place across issued -> amended -> locked. The durable operational-kernel record of what feed sheet was promised for a day and how it changed. Per-workflow because the 000004 dispatch clock is per-workflow.';
COMMENT ON COLUMN feed_direction_issues.state IS
  'issued (frozen on generation) | amended (a correction batch changed some sheds) | locked (transport cutoff passed; no further change). Mutated in place; the *_at columns accumulate.';
COMMENT ON COLUMN feed_direction_issues.generation_input_fingerprint IS
  'Hash of the herd + config the sheet was generated from. Equal fingerprint => a re-issue is a no-op and an amend has nothing to change. Stored so change detection never re-reads the whole sheet.';


-- ---------------------------------------------------------------------------
-- 2. feed_direction_issue_rows -- the FROZEN sheet, the WHOLE generated scope
-- ---------------------------------------------------------------------------
-- The complete generated scope for the issue, NOT a page: past reads and pagination both serve
-- STORED rows, so the whole park-day must be here. One row per generated CELL -- (grain, session,
-- feed_item) -- which is the same grain the live DirectionRow.Items carries, denormalized flat so
-- the read path can reconstruct the exact DirectionRows and re-run the tested whole-scope summary.
--
-- BLOCKED-VS-ZERO IS PRESERVED STRUCTURALLY, EXACTLY AS THE GENERATOR ENCODES IT. quantity_kg is
-- NULL if and only if the cell is blocked (no authored ration); a stored 0.000 is an AUTHORED zero
-- (milk-fed kids), a real feeding instruction. The two are opposite states with opposite
-- consequences and the CHECK below makes them un-collapsible at the schema level: a blocked cell
-- MUST carry a reason code and no quantity, a resolved cell MUST carry a quantity and no reason.
--
-- THE GRAIN DISCRIMINATOR IS IN THE NATURAL KEY. A shed can hold several ration grains (two breeds
-- sharing a ration group, e.g. Beetal + Sojat), so (shed_id, session_no, feed_item) alone is NOT
-- unique within an issue. shed_tag_key and breed_key -- generated by the same feed_config_norm()
-- the generator groups grains by -- complete the key so multi-grain sheds never collide.
CREATE TABLE feed_direction_issue_rows (
    feed_direction_issue_row_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   uuid NOT NULL REFERENCES tenants (tenant_id),
    feed_direction_issue_id     uuid NOT NULL REFERENCES feed_direction_issues (feed_direction_issue_id) ON DELETE CASCADE,
    park_id                     uuid NOT NULL,
    park_label                  text NOT NULL,
    shed_id                     uuid NOT NULL,
    shed_label                  text NOT NULL,
    -- The authored tag the animals resolved onto (raw source text on a blocked row). Its norm key
    -- matches the grain group key the generator used, which is what makes the natural key exact.
    shed_tag                    text NOT NULL DEFAULT '',
    shed_tag_key                text GENERATED ALWAYS AS (feed_config_norm(shed_tag)) STORED,
    breed                       text NOT NULL DEFAULT '',
    breed_key                   text GENERATED ALWAYS AS (feed_config_norm(breed)) STORED,
    ration_group                text NOT NULL DEFAULT '',
    experiment_arm              text NOT NULL DEFAULT '',
    session_no                  integer NOT NULL,
    session_label               text NOT NULL DEFAULT '',
    head_count                  bigint NOT NULL,
    -- True for the experiment workflow, whose authored kg is already a shed total: head_count must
    -- never be multiplied into the quantity. Carried through so no reader or rollup ever does.
    head_count_informational    boolean NOT NULL,
    workflow                    text NOT NULL,
    feed_item_label             text NOT NULL,
    feed_item_key               text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    -- NULL IFF BLOCKED. A stored 0.000 is an authored zero, not a gap. See the CHECK below.
    quantity_kg                 numeric(12, 3),
    -- The authored rate / multiplier this quantity came from, echoed for the operator. NULL on a
    -- blocked cell and on every experiment cell (which authors absolute kg with no per-head rate).
    grams_per_head              numeric(12, 3),
    shed_factor                 numeric(8, 4),
    -- Set IFF blocked, mirroring domain.BlockedReason: a machine-stable code plus the operator
    -- sentence that names the exact missing coordinate.
    blocked_reason_code         text,
    blocked_reason_detail       text,
    -- The (grain, session) session total, summing RESOLVED cells only -- denormalized onto every
    -- cell of the grain so a row reconstructs without a second pass. Partial when the row is blocked.
    session_total_kg            numeric(12, 3) NOT NULL,
    overdue_pending             boolean NOT NULL,
    -- Generation order, so the frozen sheet reconstructs byte-for-byte: row_seq orders the grain
    -- rows, item_seq orders the cells within a row.
    row_seq                     integer NOT NULL,
    item_seq                    integer NOT NULL,
    -- An amendment marks ONLY the cells it changed, so the UI can show exactly what moved.
    amended                     boolean NOT NULL DEFAULT false,
    amended_at                  timestamptz,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_direction_issue_rows_workflow_check CHECK (workflow = ANY (ARRAY['normal'::text, 'experiment'::text])),
    -- BLOCKED-VS-ZERO, enforced structurally: blocked iff no quantity and a reason present; resolved
    -- iff a quantity is present and no reason. A stored 0.000 is therefore necessarily resolved.
    CONSTRAINT feed_direction_issue_rows_blocked_shape_check CHECK (
        (quantity_kg IS NULL AND blocked_reason_code IS NOT NULL)
        OR (quantity_kg IS NOT NULL AND blocked_reason_code IS NULL)
    ),
    CONSTRAINT feed_direction_issue_rows_seq_check CHECK (row_seq >= 0 AND item_seq >= 0),
    CONSTRAINT feed_direction_issue_rows_amended_shape_check CHECK (amended = false OR amended_at IS NOT NULL)
);

-- Natural key: the grain cell inside an issue. shed_tag_key + breed_key are the grain discriminator
-- that keeps a multi-grain shed's identically-named feed items from colliding.
CREATE UNIQUE INDEX feed_direction_issue_rows_natural_key_uidx
    ON feed_direction_issue_rows (tenant_id, feed_direction_issue_id, shed_id, session_no, shed_tag_key, breed_key, feed_item_key);

-- Serving read: load one issue's whole scope in generation order for reconstruction, and narrow to
-- a shed for the shed filter. Ordered so the frozen sheet comes back exactly as it was written.
CREATE INDEX feed_direction_issue_rows_serve_idx
    ON feed_direction_issue_rows (tenant_id, feed_direction_issue_id, shed_id, row_seq, item_seq);

COMMENT ON TABLE feed_direction_issue_rows IS
  'The FROZEN feed-direction sheet: the whole generated scope for an issue (not a page), one row per (grain, session, feed_item) cell. Read paths serve these stored rows; the whole-scope summary is recomputed over them so it stays invariant to page size. Blocked-vs-zero is preserved: quantity_kg NULL iff blocked.';
COMMENT ON COLUMN feed_direction_issue_rows.quantity_kg IS
  'The frozen packable quantity in kg. NULL IFF blocked (no authored ration -- surfaced as a gap); a stored 0.000 is an AUTHORED zero and a real feeding instruction. The blocked_shape CHECK makes the two un-collapsible.';
COMMENT ON COLUMN feed_direction_issue_rows.amended IS
  'True on a cell an amendment changed. An amend marks only the rows it moved, so the UI can show what a correction did without diffing the whole sheet.';



-- -----------------------------------------------------------------------------
-- Collapsed from 000016_shifting_taxonomy_governing_docs.sql
-- -----------------------------------------------------------------------------
-- Shifting-event taxonomy realigned to the GOVERNING product docs (maintainer decision 2026-07-20,
-- "Governing docs win"). The Shifting Reports / Feed-Shiftings-Count source documents define:
--
--   * Priority: High / Low            (was: normal / high / emergency)
--   * Category: Growth / Health / Breeding / Delivery
--                                      (was: routine / high_priority / pregnancy / warmup /
--                                            medical / quarantine / other)
--
-- The prior free-mixed vocabulary is retired. This forward migration remaps every existing
-- shifting_events row to the governing values and swaps the CHECK constraints. shifting_events is a
-- bounded operational table (one row per authored movement), so a validated constraint swap is
-- lock-safe here; it is not one of the large import/goat/event/obligation tables.
--
-- Value mapping (documented so the remap is auditable and the code paths agree):
--
--   PRIORITY   old 'emergency' -> 'high'   (fast lane: keeps the 1-day feed-follow lead + the
--                                            documented high-priority source-ration bridge)
--              old 'high'      -> 'high'    (queue-order signal folds into the fast lane)
--              old 'normal'    -> 'low'     (standard 2-day feed-follow lead)
--
--   CATEGORY   old 'routine'       -> 'growth'
--              old 'high_priority' -> 'growth'
--              old 'other'         -> 'growth'
--              old 'warmup'        -> 'breeding'   (reproductive-readiness prep)
--              old 'pregnancy'     -> 'breeding'
--              old 'medical'       -> 'health'
--              old 'quarantine'    -> 'health'
--              ('delivery' is a new governing category for birth/K0-driven moves; no legacy row maps
--               to it, but it is a valid selectable value going forward.)
--
-- Order matters: DROP the old CHECKs first (the new values violate the OLD allow-list), UPDATE the
-- data, then ADD the governing CHECKs.

-- Lock-safe constraint re-definition (validate-hot-index-migrations): DROP the old CHECK, remap the
-- data, ADD the governing CHECK as NOT VALID (no full-table ACCESS EXCLUSIVE scan on ADD), then
-- VALIDATE it as a separate statement (SHARE UPDATE EXCLUSIVE, does not block writes). shifting_events
-- is bounded, but the safe pattern is used regardless so the guard's hot-table contract holds.
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_priority_check;
ALTER TABLE public.shifting_events DROP CONSTRAINT IF EXISTS shifting_events_category_check;

UPDATE public.shifting_events
SET priority = CASE priority
        WHEN 'emergency' THEN 'high'
        WHEN 'high'      THEN 'high'
        WHEN 'normal'    THEN 'low'
        ELSE 'low'
    END
WHERE priority NOT IN ('high', 'low');

UPDATE public.shifting_events
SET category = CASE category
        WHEN 'routine'       THEN 'growth'
        WHEN 'high_priority' THEN 'growth'
        WHEN 'other'         THEN 'growth'
        WHEN 'warmup'        THEN 'breeding'
        WHEN 'pregnancy'     THEN 'breeding'
        WHEN 'medical'       THEN 'health'
        WHEN 'quarantine'    THEN 'health'
        ELSE 'growth'
    END
WHERE category NOT IN ('growth', 'health', 'breeding', 'delivery');

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_priority_check
    CHECK ((priority = ANY (ARRAY['high'::text, 'low'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_priority_check;

ALTER TABLE public.shifting_events
    ADD CONSTRAINT shifting_events_category_check
    CHECK ((category = ANY (ARRAY['growth'::text, 'health'::text, 'breeding'::text, 'delivery'::text]))) NOT VALID;
ALTER TABLE public.shifting_events VALIDATE CONSTRAINT shifting_events_category_check;


-- -----------------------------------------------------------------------------
-- Collapsed from 000017_calendar_exception_due_index.sql
-- -----------------------------------------------------------------------------
-- +goose NO TRANSACTION
-- Keep Calendar's bounded exception catch-up path index-selective.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_obligation_instances_calendar_exceptions_due
ON public.obligation_instances (tenant_id, status, due_at)
WHERE batch_id IS NULL
  AND status IN ('missed', 'in_progress', 'deferred');


-- -----------------------------------------------------------------------------
-- Collapsed from 000018_retire_admin_rbac_role.sql
-- -----------------------------------------------------------------------------
-- Retire the legacy RBAC role_key 'admin'.
--
-- Product/API route names such as /admin/* and the admin-web application name
-- remain unchanged. This migration only removes the grantable RBAC role. CEO/CXO
-- top access is represented exclusively by role='ceo_internal'.

-- user_scope_grants: collapse admin into ceo_internal, avoiding unique/FK drift
-- when a user already has the equivalent ceo_internal grant for the same scope.
UPDATE user_scope_grants admin_grant
SET status = 'revoked',
    valid_to = coalesce(admin_grant.valid_to, now())
WHERE admin_grant.role = 'admin'
  AND EXISTS (
    SELECT 1
    FROM user_scope_grants ceo_grant
    WHERE ceo_grant.tenant_id = admin_grant.tenant_id
      AND ceo_grant.user_id = admin_grant.user_id
      AND ceo_grant.role = 'ceo_internal'
      AND ceo_grant.scope_type = admin_grant.scope_type
      AND ceo_grant.scope_id = admin_grant.scope_id
      AND ceo_grant.status = admin_grant.status
      AND ceo_grant.valid_from = admin_grant.valid_from
      AND ceo_grant.valid_to IS NOT DISTINCT FROM admin_grant.valid_to
  );

UPDATE user_scope_grants
SET role = 'ceo_internal'
WHERE role = 'admin';

-- Pending email grants: same collapse, but include the email identity in the
-- duplicate test because active uniqueness is per email + role + scope.
UPDATE auth_pending_email_grants admin_grant
SET status = 'revoked',
    valid_to = coalesce(admin_grant.valid_to, now()),
    updated_at = now(),
    metadata = coalesce(admin_grant.metadata, '{}'::jsonb)
      || jsonb_build_object('retired_role', 'admin', 'replacement_role', 'ceo_internal')
WHERE admin_grant.role = 'admin'
  AND admin_grant.status = 'active'
  AND admin_grant.valid_to IS NULL
  AND EXISTS (
    SELECT 1
    FROM auth_pending_email_grants ceo_grant
    WHERE ceo_grant.tenant_id = admin_grant.tenant_id
      AND ceo_grant.normalized_email = admin_grant.normalized_email
      AND ceo_grant.role = 'ceo_internal'
      AND ceo_grant.scope_type = admin_grant.scope_type
      AND ceo_grant.scope_id = admin_grant.scope_id
      AND ceo_grant.status = 'active'
      AND ceo_grant.valid_to IS NULL
  );

UPDATE auth_pending_email_grants
SET role = 'ceo_internal',
    updated_at = now(),
    metadata = coalesce(metadata, '{}'::jsonb)
      || jsonb_build_object('retired_role', 'admin', 'replacement_role', 'ceo_internal')
WHERE role = 'admin';

-- Historical escalations that targeted the old role now target CEO/CXO.
UPDATE obligation_escalations
SET escalated_to_role = 'ceo_internal'
WHERE escalated_to_role = 'admin';

ALTER TABLE obligation_escalations
  DROP CONSTRAINT IF EXISTS obligation_escalations_role_check;

ALTER TABLE obligation_escalations
  ADD CONSTRAINT obligation_escalations_role_check
  CHECK (
    escalated_to_role IS NULL OR
    escalated_to_role = ANY (ARRAY[
      'park_head'::text,
      'pc_director'::text,
      'operator'::text,
      'verifier'::text,
      'ceo_internal'::text
    ])
  );

DELETE FROM org_role_catalog
WHERE role_key = 'admin';


-- -----------------------------------------------------------------------------
-- Collapsed from 000019_retire_admin_workforce_hint.sql
-- -----------------------------------------------------------------------------
-- Retire the old workforce display hint. CEO/CXO staff use primary_role_hint='cxo'.
-- This is display/bootstrap metadata only; RBAC grants remain role='ceo_internal'.

ALTER TABLE workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

UPDATE workforce_members
SET primary_role_hint = 'cxo',
    updated_at = now(),
    row_version = row_version + 1
WHERE primary_role_hint = 'admin';

ALTER TABLE workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (
    primary_role_hint = ANY (ARRAY[
      'operator'::text,
      'park_head'::text,
      'pc_director'::text,
      'verifier'::text,
      'supervisor'::text,
      'cxo'::text,
      'other'::text
    ])
  );


-- -----------------------------------------------------------------------------
-- Collapsed from 000020_vaccination_drive_assignments.sql
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.vaccination_drive_assignments (
  assignment_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  batch_id uuid NOT NULL,
  planned_date date NOT NULL,
  operator_id uuid,
  park_id uuid NOT NULL,
  shed_id uuid,
  physical_shed text NOT NULL,
  partition_label text NOT NULL DEFAULT 'whole',
  animal_count integer NOT NULL,
  capacity_status text NOT NULL DEFAULT 'within_cap',
  warnings jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_drive_assignments_pkey PRIMARY KEY (assignment_id),
  CONSTRAINT vaccination_drive_assignments_animals_check CHECK (animal_count >= 0),
  CONSTRAINT vaccination_drive_assignments_capacity_check CHECK (capacity_status = ANY (ARRAY['within_cap'::text, 'over_cap_required'::text, 'capacity_action'::text])),
  CONSTRAINT vaccination_drive_assignments_warnings_array_check CHECK (jsonb_typeof(warnings) = 'array'),
  CONSTRAINT vaccination_drive_assignments_tenant_batch_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES public.obligation_batches(tenant_id, batch_id) ON DELETE CASCADE,
  CONSTRAINT vaccination_drive_assignments_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT vaccination_drive_assignments_tenant_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT vaccination_drive_assignments_operator_fk FOREIGN KEY (operator_id) REFERENCES public.workforce_members(workforce_member_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_uq
  ON public.vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid), physical_shed, partition_label);

CREATE INDEX IF NOT EXISTS vaccination_drive_assignments_operator_day_idx
  ON public.vaccination_drive_assignments (tenant_id, operator_id, planned_date)
  WHERE operator_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS vaccination_drive_assignments_park_day_idx
  ON public.vaccination_drive_assignments (tenant_id, park_id, planned_date);


-- -----------------------------------------------------------------------------
-- Collapsed from 000021_vaccination_shed_level_video_sop.sql
-- -----------------------------------------------------------------------------
-- Vaccination proof mode is SOP-controlled. Current Mesha staging/default SOP uses one shed-level
-- video bundle (1 mandatory, up to 5 total) captured from camera or gallery on the shed submit
-- screen. Per-goat video remains a valid SOP mode; this migration changes the published
-- vaccination defaults, it does not delete the per-goat code path.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    (
      jsonb_set(
        jsonb_set(
          (sv.form_dsl - 'goat_row_proof') || jsonb_build_object(
            'shed_video',
            jsonb_build_object(
              'subject_scope', 'shed',
              'capture_source', 'in_app_camera',
              'allowed_capture_sources', jsonb_build_array('in_app_camera', 'gallery_picker'),
              'minimum_clips', 1,
              'maximum_clips', 5
            )
          ),
          '{fields}',
          COALESCE((
            SELECT jsonb_agg(field ORDER BY ordinal)
            FROM (
              SELECT field, ordinal
              FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
                WITH ORDINALITY AS entries(field, ordinal)
              WHERE field ->> 'key' <> 'goat_row_proof'
                AND field ->> 'key' <> 'shed_video'
                AND field ->> 'key' <> 'goat_ids'
              UNION ALL
              SELECT jsonb_build_object(
                'key', 'goat_ids',
                'label', 'Goats vaccinated',
                'type', 'goat_scan',
                'required', true,
                'repeat', true,
                'description', 'Scan each goat RFID exactly when the vaccine is given. The scan timestamp is the vaccination timestamp.'
              ), 9998
              UNION ALL
              SELECT jsonb_build_object(
                'key', 'shed_video',
                'label', 'Shed proof videos',
                'type', 'video_proof',
                'required', true,
                'repeat', true,
                'proof_subject', 'shed',
                'help_text', 'Add 1 required shed-level video before submit; camera or gallery allowed, up to 5 videos.'
              ), 9999
            ) fields
          ), '[]'::jsonb),
          true
        ),
        '{rules}',
        COALESCE((sv.form_dsl -> 'rules'), '[]'::jsonb),
        true
      )
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'proof_mode', 'shed_level_video',
      'subject_scope', 'shed',
      'expected_subjects', jsonb_build_array('shed'),
      'minimum_count', 1,
      'maximum_count', 5,
      'maximum_count_per_subject', 5,
      'capture_source', 'in_app_camera',
      'allowed_capture_sources', jsonb_build_array('in_app_camera', 'gallery_picker'),
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    compatibility = jsonb_set(
      COALESCE(sv.compatibility, '{}'::jsonb),
      '{supported_proof_actions}',
      jsonb_build_array('video.capture', 'video.pick'),
      true
    ),
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;


-- -----------------------------------------------------------------------------
-- Collapsed from 000022_vaccination_drive_assignment_operator_grain.sql
-- -----------------------------------------------------------------------------
DROP INDEX IF EXISTS public.vaccination_drive_assignments_batch_shed_part_uq;

ALTER TABLE vaccination_drive_assignments
  ADD COLUMN IF NOT EXISTS vaccine_rule_ids uuid[] NOT NULL DEFAULT '{}';

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_operator_lane_uq
  ON public.vaccination_drive_assignments (
    tenant_id,
    batch_id,
    planned_date,
    park_id,
    COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
    physical_shed,
    partition_label,
    COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(vaccine_rule_ids, '{}'::uuid[])
  );


-- -----------------------------------------------------------------------------
-- Collapsed from 000023_goat_shed_partitions.sql
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS goat_shed_partitions (
  tenant_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  partition_label text NOT NULL DEFAULT 'whole',
  source_shed_name text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, goat_id),
  CONSTRAINT goat_shed_partitions_goat_fk
    FOREIGN KEY (tenant_id, goat_id)
    REFERENCES goats (tenant_id, goat_id)
    ON DELETE CASCADE,
  CONSTRAINT goat_shed_partitions_shed_fk
    FOREIGN KEY (tenant_id, shed_id)
    REFERENCES locations (tenant_id, location_id)
    ON DELETE RESTRICT,
  CONSTRAINT goat_shed_partitions_partition_nonblank
    CHECK (btrim(partition_label) <> ''),
  CONSTRAINT goat_shed_partitions_source_nonblank
    CHECK (btrim(source_shed_name) <> '')
);

CREATE INDEX IF NOT EXISTS goat_shed_partitions_shed_partition_idx
  ON goat_shed_partitions (tenant_id, shed_id, partition_label);


-- -----------------------------------------------------------------------------
-- Collapsed from 000024_ceo_ai_reporting_schema.sql
-- -----------------------------------------------------------------------------
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai reporting schema — the governed, business-language read surface the
-- Mesha leadership assistant (CEO/CXO chatbot) reads from.
--
-- WHY THIS EXISTS
-- The assistant is READ-ONLY and must never see raw normalized tables or hold
-- an app-user credential. This schema is the stable contract (docs/ceo-ai/
-- mcp-toolbox-plan.md → "Reporting Schema Coverage Contract"): every
-- leadership-relevant Mesha domain is exposed as exactly one business-language
-- view here, consumed by MCP Toolbox curated tools (docs/ceo-ai/
-- mcp-toolbox-tools.yaml) and the guarded read-only SQL fallback.
--
-- CONTRACT RULES honored below
--  * Every contract column maps to a REAL source column/derivation, or is a
--    typed NULL / 0 with an explicit `-- TODO(source):` note. No silent gaps.
--  * Tenant-scoped: every view carries tenant_id and the caller filters on it.
--    The tenant predicate MUST reach the base scans. Where a view LEFT JOINs a
--    pre-aggregated measure onto a spine, PG canNOT derive the measure's
--    tenant_id by equivalence (nullable side of the outer join), so such shapes
--    would Seq-Scan all tenants. counts_movement_daily was rebuilt to a single
--    UNION ALL event stream aggregated on tenant_id (the driving relation) so the
--    predicate pushes into every branch scan — see its inline note.
--  * Aggregate / leadership grain — no per-animal row dumps except
--    animal_current_scope, which is the canonical per-animal base the
--    aggregate tools GROUP over (it never leaves the DB as raw rows; tools
--    aggregate it, per the toolbox statements).
--  * Scale posture (HONEST): these are leadership REPORTING reads served at the
--    current 5k-50k canonical-read envelope
--    (docs/decisions/operational-kernel-5k-50k-scale-envelope.md), NOT
--    compute-on-write projections. EXPLAIN at the local seed shows:
--      - counts_movement_daily / mortality_base / feed_adherence: the tenant
--        predicate pushes to the base scans (per-TENANT work), bounded by the
--        envelope; a Seq Scan of the tenant's own rows is expected here.
--      - vaccination_shed_status / action_center_current: a per-tenant Seq Scan
--        of that tenant's shed obligation_instances (they read a large fraction
--        of the tenant's open+done obligations, so the planner does not use the
--        scope index). This is an ACCEPTED reporting-read plan at 5k-50k; it is
--        NOT certified at the 1-5M bar. Before 1-5M certification these must earn
--        a compute-on-write projection or a proven partial index — do not read
--        this header as a "single grouped scan, verified" guarantee (it is not).
--  * India business calendar: every business date is derived at Asia/Kolkata,
--    never UTC (AGENTS.md time semantics).
--  * Vaccine labels are HUMAN (ET+TT, PPR · Booster, …). Raw dose_code tokens
--    never surface — they resolve through ceo_ai.vaccine_label_map.
--
-- GRANTS to mesha_ceo_readonly / mesha_cube_readonly are applied at the END of
-- this migration inside a guarded DO-block (IF the role exists). No role and no
-- password live in this migration; roles are created by
-- tools/dev/setup-ceo-ai-local-role.sh and by Secret-Manager-backed runtime.
-- ===========================================================================

CREATE SCHEMA IF NOT EXISTS ceo_ai;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Vaccine display-label map — the ONLY bridge from raw dose_code tokens to the
-- human labels the assistant is allowed to speak. Closes the discovery-judge
-- vaccine_label gap: vaccination_dose_pickup.vaccine_label derives through this
-- table, never from a raw code or protocol family token. Seeded from the known
-- Preventive Care families; an unmapped code yields NULL (surfaced as
-- "unspecified vaccine", never a raw token).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS ceo_ai.vaccine_label_map (
    family_prefix text PRIMARY KEY,   -- dose_code family prefix, e.g. 'et_tt'
    vaccine_label text NOT NULL,      -- human label, e.g. 'ET+TT'
    created_at    timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO ceo_ai.vaccine_label_map (family_prefix, vaccine_label) VALUES
    ('et_tt',      'ET+TT'),
    ('ppr',        'PPR'),
    ('blue_tongue','Blue Tongue'),
    ('goat_pox',   'Goat Pox'),
    ('sheep_pox',  'Sheep Pox'),
    ('fmd',        'FMD'),
    ('hs',         'HS')
ON CONFLICT (family_prefix) DO NOTHING;
-- +goose StatementEnd

-- Resolve a raw dose_code to a human label (with a "· Booster" suffix for the
-- revac/booster dose). Returns NULL for an unmapped family so callers can say
-- "unspecified vaccine" rather than leak a code. STABLE + no table dependency
-- beyond the map, so it is safe inside the read-only views.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ceo_ai.vaccine_label_for(p_dose_code text)
RETURNS text
LANGUAGE sql
STABLE
AS $fn$
    SELECT CASE
        WHEN m.vaccine_label IS NULL THEN NULL
        WHEN p_dose_code LIKE '%revac%' OR p_dose_code LIKE '%booster%'
            THEN m.vaccine_label || ' · Booster'
        ELSE m.vaccine_label
    END
    FROM ceo_ai.vaccine_label_map m
    WHERE p_dose_code LIKE m.family_prefix || '%'
    ORDER BY length(m.family_prefix) DESC
    LIMIT 1;
$fn$;
-- +goose StatementEnd

-- ===========================================================================
-- 1. ceo_ai.animal_current_scope — per-animal base (the aggregate tools GROUP
--    over this; it is never returned as raw rows).
-- ===========================================================================
-- projection-review: membership=canonical goats/obligation_instances/shifting rows filtered to the current tenant; group_key=(tenant_id, shed_id|scope_id|batch_id|business_day) per view — every COUNT/SUM below groups on the same key it is later joined on; join_cardinality=all COUNT/SUM CTEs (occ, obl, anim, dose_pickup, counts_movement_daily deltas) pre-aggregate the many-side to one row per group_key BEFORE the outer LEFT JOIN to sheds/locations, so no fan-out double-counts (owner/backup seats join 1:1 on shed_id); pagination=these are bounded per-shed / per-batch / per-day reporting views read tenant-scoped by Cube and the MCP toolbox with LIMIT at the call site, never whole-tenant unpaginated raw-row reads; scope=explicit park_id/shed_id/scope_id columns preserved on every row so leadership scope (park→shed→cohort) and the vaccination status matrix (due/done/missed via COUNT FILTER on status) resolve without collapsing distinct scopes. Adversarial grain proof: backend/internal/ceoai/reporting/reporting_views_test.go (Postgres-gated).
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.animal_current_scope AS
SELECT
    g.tenant_id                                   AS tenant_id,
    g.goat_id                                     AS animal_id,
    g.park_id                                     AS park_id,
    pk.name                                       AS park_label,
    g.shed_id                                     AS shed_id,
    sh.name                                       AS shed_label,
    g.species                                     AS species,
    g.management_stage                            AS management_stage,
    g.lifecycle_status                            AS lifecycle_status,
    g.sex                                         AS sex,
    COALESCE(b.canonical_name, g.breed)           AS breed,
    -- age at Asia/Kolkata business day; dob preferred, approx_dob fallback
    (((now() AT TIME ZONE 'Asia/Kolkata')::date) - COALESCE(g.dob, g.approx_dob))::int
                                                  AS age_days
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id
LEFT JOIN breeds    b  ON b.breed_id     = g.breed_id;
-- +goose StatementEnd

-- ===========================================================================
-- 2. ceo_ai.shed_capacity_current — occupancy vs capacity per shed.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.shed_capacity_current AS
WITH occ AS (
    SELECT tenant_id, shed_id, COUNT(*)::bigint AS animals
    FROM goats
    WHERE shed_id IS NOT NULL
      AND lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, shed_id
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS owner_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
)
SELECT
    s.tenant_id                                   AS tenant_id,
    pk.name                                       AS park_label,
    s.name                                        AS shed_label,
    COALESCE(occ.animals, 0)                      AS animals,
    sp.capacity                                   AS capacity,
    (sp.capacity - COALESCE(occ.animals, 0))      AS variance,
    CASE
        WHEN sp.capacity IS NULL THEN 'unknown_capacity'
        WHEN COALESCE(occ.animals, 0) > sp.capacity THEN 'over_capacity'
        WHEN COALESCE(occ.animals, 0) = sp.capacity THEN 'at_capacity'
        ELSE 'under_capacity'
    END                                           AS status,
    o.owner_label                                 AS owner_label,
    bk.backup_label                               AS backup_label
FROM locations s
LEFT JOIN locations     pk ON pk.location_id = s.parent_location_id
LEFT JOIN shed_profiles sp ON sp.location_id = s.location_id
LEFT JOIN occ         ON occ.tenant_id = s.tenant_id AND occ.shed_id = s.location_id
LEFT JOIN owner_seat  o  ON o.tenant_id  = s.tenant_id AND o.shed_id  = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';
-- +goose StatementEnd

-- ===========================================================================
-- 3. ceo_ai.vaccination_shed_status — due/done/overdue + ownership per shed.
--    Obligations are shed-scoped (scope_type='shed'). Aggregate ONCE.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.vaccination_shed_status AS
WITH obl AS (
    SELECT
        tenant_id,
        scope_id AS shed_id,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress'))                          AS due,
        COUNT(*) FILTER (WHERE status IN ('completed','accepted'))                                    AS done,
        COUNT(*) FILTER (WHERE status IN ('scheduled','due','in_progress','missed')
                          AND window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)                 AS overdue,
        MIN(due_at) FILTER (WHERE status IN ('scheduled','due','in_progress'))                        AS next_due,
        COUNT(DISTINCT batch_id) FILTER (WHERE batch_id IS NOT NULL)                                  AS planned_sessions
    FROM obligation_instances
    WHERE scope_type = 'shed'
    GROUP BY tenant_id, scope_id
),
anim AS (
    SELECT tenant_id, shed_id, SUM(animal_count) FILTER (WHERE usable_for_vaccination)::bigint AS animals
    FROM vaccination_eligibility_rollups
    GROUP BY tenant_id, shed_id
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS manager_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
)
SELECT
    s.tenant_id                                                    AS tenant_id,
    pk.name                                                        AS park_label,
    s.name                                                         AS shed_label,
    COALESCE(anim.animals, 0)                                      AS animals,
    COALESCE(obl.due, 0)                                           AS due,
    COALESCE(obl.done, 0)                                          AS done,
    COALESCE(obl.planned_sessions, 0)                              AS planned_sessions,
    (obl.next_due AT TIME ZONE 'Asia/Kolkata')::date              AS next_due_date,
    om.manager_label                                               AS manager_label,
    bk.backup_label                                                AS backup_label,
    CASE
        WHEN COALESCE(obl.overdue, 0) > 0 THEN 'overdue'
        WHEN COALESCE(obl.due, 0)     > 0 THEN 'due'
        WHEN COALESCE(obl.done, 0)    > 0 THEN 'complete'
        ELSE 'no_work_due'
    END                                                           AS status
FROM locations s
LEFT JOIN locations   pk ON pk.location_id = s.parent_location_id
LEFT JOIN obl         ON obl.tenant_id  = s.tenant_id AND obl.shed_id  = s.location_id
LEFT JOIN anim        ON anim.tenant_id = s.tenant_id AND anim.shed_id = s.location_id
LEFT JOIN owner_seat  om ON om.tenant_id = s.tenant_id AND om.shed_id = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';
-- +goose StatementEnd

-- ===========================================================================
-- 4. ceo_ai.vaccination_dose_pickup — doses to pick per business day / park /
--    shed / vaccine. Batches are park-scoped drives; obligations shed-scoped.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.vaccination_dose_pickup AS
WITH obl AS (
    SELECT
        oi.tenant_id,
        oi.batch_id,
        oi.scope_id AS shed_id,
        oi.rule_id,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress'))                       AS animals_due,
        COUNT(*) FILTER (WHERE oi.status IN ('scheduled','due','in_progress','missed')
                          AND oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date)             AS animals_overdue
    FROM obligation_instances oi
    WHERE oi.scope_type = 'shed' AND oi.batch_id IS NOT NULL
    GROUP BY oi.tenant_id, oi.batch_id, oi.scope_id, oi.rule_id
)
SELECT
    ob.tenant_id                                                   AS tenant_id,
    bt.planned_date                                                AS business_date,
    pk.name                                                        AS park_label,
    sh.name                                                        AS shed_label,
    -- HUMAN label only; NULL (never a raw dose_code) when family unmapped.
    ceo_ai.vaccine_label_for(pr.dose_code)                         AS vaccine_label,
    bt.reserved_quantity                                           AS doses_to_pick,
    ob.animals_due                                                 AS animals_due,
    ob.animals_overdue                                             AS animals_overdue,
    ownm.display_name                                              AS owner_label,
    bkm.display_name                                               AS backup_label,
    CASE
        WHEN ob.animals_overdue > 0 THEN 'catch_up_overdue'
        WHEN ob.animals_due     > 0 THEN 'pick_and_administer'
        ELSE 'no_action'
    END                                                           AS next_action
FROM obl ob
JOIN obligation_batches bt ON bt.tenant_id = ob.tenant_id AND bt.batch_id = ob.batch_id
LEFT JOIN locations      sh ON sh.location_id = ob.shed_id
LEFT JOIN locations      pk ON pk.location_id = sh.parent_location_id
LEFT JOIN protocol_rules pr ON pr.rule_id = ob.rule_id
-- drive conductor (batch.conducted_by is a user_id → workforce_members.user_id)
LEFT JOIN workforce_members ownm ON ownm.tenant_id = bt.tenant_id AND ownm.user_id = bt.conducted_by
-- backup: active backup seat at the shed
LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.tenant_id = ob.tenant_id AND wp.scope_type = 'shed' AND wp.scope_id = ob.shed_id
      AND wp.is_backup_slot = true AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
    LIMIT 1
) bkm ON true;
-- +goose StatementEnd

-- ===========================================================================
-- 5. ceo_ai.feed_direction_current — issued feed-direction cells (directive).
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.feed_direction_current AS
SELECT
    r.tenant_id                                   AS tenant_id,
    i.feed_day                                    AS feed_day,
    r.park_label                                  AS park_label,
    r.shed_label                                  AS shed_label,
    r.workflow                                    AS workflow,
    r.session_no                                  AS session_no,
    r.feed_item_label                             AS feed_item_label,
    r.quantity_kg                                 AS quantity_kg,
    -- blocked = missing config / gate, NEVER zero. NULL reason means not blocked.
    r.blocked_reason_code                         AS blocked_reason,
    COALESCE(r.amended, false)                    AS amended
FROM feed_direction_issue_rows r
JOIN feed_direction_issues i
  ON i.feed_direction_issue_id = r.feed_direction_issue_id;
-- +goose StatementEnd

-- ===========================================================================
-- 6. ceo_ai.counts_movement_daily — per business-day births/deaths/shifts.
-- ===========================================================================
-- +goose StatementBegin
-- SCALE: the tenant predicate MUST push into every base-table scan. An earlier
-- shape pre-aggregated each measure into its own CTE and LEFT JOINed them onto a
-- UNION `keys` spine; the outer `WHERE tenant_id = $1` then sat ABOVE the
-- aggregation/UNION barrier and, because the measure CTEs were on the nullable
-- side of LEFT JOINs, PG could not derive `births.tenant_id = $1` by equivalence,
-- so it Seq-Scanned goats/shifting_events for ALL tenants per request. This shape
-- fixes that: every measure is one tagged branch of a single UNION ALL event
-- stream (the DRIVING, non-nullable relation), and the outer aggregation groups
-- on tenant_id. A predicate on tenant_id therefore pushes below the GROUP BY,
-- into each UNION ALL branch, and below each branch's own GROUP BY (tenant_id is
-- a grouping key there too) — reaching the base scans as `<table>.tenant_id = $1`.
-- The locations LEFT JOINs are the small dimension side and never widen scope.
--
-- SCALE EXCEPTION (5k-50k envelope, disclosed): the tenant predicate pushes down,
-- but the requested date window does NOT. `event_date` is an expression-derived
-- grouping key (COALESCE(entry_date, created_at::date), exited_at::date,
-- applied_at::date), so an outer `WHERE event_date >= now()-Nd` sits ABOVE each
-- branch's GROUP BY and cannot push into the base goats/shifting scans. Every read
-- therefore re-aggregates the tenant's FULL births/deaths/shift history and filters
-- the window afterward — bounded per tenant, unbounded over time. This is accepted
-- ONLY under docs/decisions/operational-kernel-5k-50k-scale-envelope.md at the
-- current envelope (same class as the vaccination_shed_status / action_center
-- tenant-scoped scans). To carry this view past the envelope, add a materialized
-- per-(tenant,shed,event_date) rollup maintained on write (birth/death/shift events)
-- so the date filter becomes an indexed range scan, and add a query-plan proof at
-- the ~500k-row upper bound. Do NOT widen the window or add read-time caps to mask it.
CREATE OR REPLACE VIEW ceo_ai.counts_movement_daily AS
WITH events AS (
    SELECT tenant_id, shed_id, event_date,
           births, 0::bigint AS deaths, 0::bigint AS transfers_out,
           0::bigint AS shifts_in, 0::bigint AS shifts_out, 0::bigint AS approvals_pending
    FROM (
        SELECT tenant_id, shed_id,
               (COALESCE(entry_date, (created_at AT TIME ZONE 'Asia/Kolkata')::date)) AS event_date,
               COUNT(*)::bigint AS births
        FROM goats
        WHERE origin_type = 'birth'
        GROUP BY 1,2,3
    ) b
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, deaths, 0, 0, 0, 0
    FROM (
        SELECT tenant_id, shed_id,
               (exited_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               COUNT(*)::bigint AS deaths
        FROM goats
        WHERE exited_at IS NOT NULL AND exit_reason = 'died'
        GROUP BY 1,2,3
    ) d
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, transfers_out, 0, 0, 0
    FROM (
        -- cross-park exit is terminal (transferred/sold); attribute to source shed.
        SELECT se.tenant_id, se.source_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS transfers_out
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
          AND se.category IN ('transfer','sale','exit')
        GROUP BY 1,2,3
    ) t
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, 0, shifts_in, 0, 0
    FROM (
        SELECT se.tenant_id, se.destination_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS shifts_in
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
        GROUP BY 1,2,3
    ) si
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, 0, 0, shifts_out, 0
    FROM (
        SELECT se.tenant_id, se.source_shed_id AS shed_id,
               (se.applied_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               SUM(im.head_count)::bigint AS shifts_out
        FROM shifting_events se
        JOIN shifting_event_impacts im ON im.shifting_event_id = se.shifting_event_id
        WHERE se.applied_at IS NOT NULL AND se.event_status = 'completed'
        GROUP BY 1,2,3
    ) so
    UNION ALL
    SELECT tenant_id, shed_id, event_date,
           0, 0, 0, 0, 0, approvals_pending
    FROM (
        SELECT ca.tenant_id, se.source_shed_id AS shed_id,
               (ca.raised_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
               COUNT(*)::bigint AS approvals_pending
        FROM counts_approval_requests ca
        LEFT JOIN shifting_events se ON se.shifting_event_id = ca.shifting_event_id
        WHERE ca.status = 'pending'
        GROUP BY 1,2,3
    ) a
)
SELECT
    e.tenant_id                                   AS tenant_id,
    e.event_date                                  AS event_date,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    SUM(e.births)::bigint                         AS births,
    SUM(e.deaths)::bigint                         AS deaths,
    SUM(e.transfers_out)::bigint                  AS transfers_out,
    SUM(e.shifts_in)::bigint                      AS shifts_in,
    SUM(e.shifts_out)::bigint                     AS shifts_out,
    SUM(e.approvals_pending)::bigint              AS approvals_pending
FROM events e
LEFT JOIN locations sh ON sh.location_id = e.shed_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id
GROUP BY e.tenant_id, e.event_date, e.shed_id, sh.name, pk.name;
-- +goose StatementEnd

-- ===========================================================================
-- 7. ceo_ai.procurement_pipeline — one row per load with stage + animal rollup.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.procurement_pipeline AS
WITH lg AS (
    SELECT tenant_id, load_id,
           COUNT(*)::bigint AS animals,
           COUNT(*) FILTER (WHERE current_state = 'rejected' OR selection_state = 'rejected')::bigint AS rejected
    FROM procurement_load_goats
    GROUP BY tenant_id, load_id
),
vpend AS (
    SELECT tenant_id, load_id,
           COUNT(*) FILTER (WHERE review_status IN ('pending','unreviewed','needs_review'))::bigint AS vaccination_pending
    FROM procurement_hf_vaccination_evidence
    GROUP BY tenant_id, load_id
)
SELECT
    l.tenant_id                                   AS tenant_id,
    COALESCE(pt.display_name, loc.name)           AS source_label,
    -- No stored human load label column; derive a stable short code from load_id
    -- + purchase_date. TODO(source): add procurement_loads.load_code if the
    -- business wants a curated batch label.
    ('Load ' || left(l.load_id::text, 8) ||
        COALESCE(' · ' || to_char(l.purchase_date, 'DD Mon'), '')) AS batch_label,
    l.status                                      AS current_stage,
    COALESCE(lg.animals, 0)                        AS animals,
    COALESCE(vpend.vaccination_pending, 0)         AS vaccination_pending,
    COALESCE(lg.rejected, 0)                        AS rejected,
    l.created_at                                  AS entered_at
FROM procurement_loads l
LEFT JOIN parties   pt  ON pt.party_id     = l.source_party_id
LEFT JOIN locations loc ON loc.location_id = l.source_location_id
LEFT JOIN lg    ON lg.tenant_id    = l.tenant_id AND lg.load_id    = l.load_id
LEFT JOIN vpend ON vpend.tenant_id = l.tenant_id AND vpend.load_id = l.load_id;
-- +goose StatementEnd

-- ===========================================================================
-- 8. ceo_ai.source_entry_health_status — intake variance + health per load.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.source_entry_health_status AS
WITH hc AS (
    SELECT tenant_id, load_id,
           COUNT(*) FILTER (WHERE health_state IN ('blocked','failed','sick','quarantine'))::bigint AS health_blockers
    FROM procurement_source_health_checks
    GROUP BY tenant_id, load_id
),
ev AS (
    SELECT tenant_id, load_id,
           COUNT(*)::bigint AS evidence_total,
           COUNT(*) FILTER (WHERE review_status IN ('pending','unreviewed','needs_review'))::bigint AS evidence_pending
    FROM procurement_hf_vaccination_evidence
    GROUP BY tenant_id, load_id
)
SELECT
    l.tenant_id                                   AS tenant_id,
    ('Load ' || left(l.load_id::text, 8) ||
        COALESCE(' · ' || to_char(l.purchase_date, 'DD Mon'), '')) AS load_label,
    COALESCE(pt.display_name, loc.name)           AS source_label,
    COALESCE(air.expected_count, l.expected_count) AS animals_expected,
    air.arrived_count                             AS animals_received,
    air.matched_count                             AS animals_accepted,
    air.rejected_count                            AS animals_rejected,
    COALESCE(hc.health_blockers, 0)               AS health_blockers,
    CASE
        WHEN ev.evidence_total IS NULL OR ev.evidence_total = 0 THEN 'no_evidence'
        WHEN ev.evidence_pending > 0 THEN 'evidence_pending'
        ELSE 'evidence_complete'
    END                                           AS evidence_status
FROM procurement_loads l
LEFT JOIN parties   pt  ON pt.party_id     = l.source_party_id
LEFT JOIN locations loc ON loc.location_id = l.source_location_id
LEFT JOIN arrival_intake_reviews air ON air.tenant_id = l.tenant_id AND air.load_id = l.load_id
LEFT JOIN hc ON hc.tenant_id = l.tenant_id AND hc.load_id = l.load_id
LEFT JOIN ev ON ev.tenant_id = l.tenant_id AND ev.load_id = l.load_id;
-- +goose StatementEnd

-- ===========================================================================
-- 9. ceo_ai.ops_exception_queue — cross-module open exceptions (UNION).
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.ops_exception_queue AS
-- counts projection exceptions
SELECT
    cpe.tenant_id                                 AS tenant_id,
    'counts'                                      AS area,
    cpe.severity                                  AS severity,
    cpe.status                                    AS status,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    cpe.exception_type                            AS title,
    cpe.created_at                                AS opened_at,
    cpe.owner_ref                                 AS owner_label,
    cpe.count_projection_exception_id::text       AS source_id
FROM count_projection_exceptions cpe
LEFT JOIN locations sh ON sh.location_id = cpe.shed_id
LEFT JOIN locations pk ON pk.location_id = cpe.park_id
WHERE cpe.status NOT IN ('resolved','closed')
UNION ALL
-- vaccination/obligation escalations
SELECT
    oe.tenant_id                                  AS tenant_id,
    'vaccination'                                 AS area,
    CASE oe.level WHEN 0 THEN 'low' WHEN 1 THEN 'medium' WHEN 2 THEN 'high' ELSE 'critical' END AS severity,
    oe.status                                     AS status,
    NULL::text                                    AS park_label,   -- TODO(source): escalation carries no park; obligation scope join is heavy, deferred
    NULL::text                                    AS shed_label,   -- TODO(source): see above
    COALESCE(oe.reason, 'obligation_escalation')  AS title,
    oe.opened_at                                  AS opened_at,
    oe.escalated_to_role                          AS owner_label,
    oe.escalation_id::text                        AS source_id
FROM obligation_escalations oe
WHERE oe.status NOT IN ('resolved','closed','acknowledged')
UNION ALL
-- blocked feed cells
SELECT
    fr.tenant_id                                  AS tenant_id,
    'feed'                                        AS area,
    'medium'                                      AS severity,
    'blocked'                                     AS status,
    fr.park_label                                 AS park_label,
    fr.shed_label                                 AS shed_label,
    COALESCE(fr.blocked_reason_code, 'feed_blocked') AS title,
    fr.created_at                                 AS opened_at,
    NULL::text                                    AS owner_label,  -- TODO(source): feed rows carry no owner; owner is the shed position
    fr.feed_direction_issue_row_id::text          AS source_id
FROM feed_direction_issue_rows fr
WHERE fr.blocked_reason_code IS NOT NULL
UNION ALL
-- rejected verification items
SELECT
    vi.tenant_id                                  AS tenant_id,
    COALESCE(vi.vertical, 'verification')         AS area,
    'high'                                        AS severity,
    vi.status                                     AS status,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    COALESCE(vi.subject_label, vi.category, 'verification_rejected') AS title,
    vi.captured_at                                AS opened_at,
    NULL::text                                    AS owner_label,  -- TODO(source): operator identity is sensitive; expose role/label upstream only
    vi.item_id::text                              AS source_id
FROM verification_items vi
LEFT JOIN locations sh ON sh.location_id = vi.shed_id
LEFT JOIN locations pk ON pk.location_id = vi.park_id
WHERE vi.status = 'rejected';
-- +goose StatementEnd

-- ===========================================================================
-- 10. ceo_ai.sop_execution_status — SOP task execution status.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.sop_execution_status AS
SELECT
    t.tenant_id                                   AS tenant_id,
    COALESCE(t.task_type, 'sop')                  AS area,
    CASE WHEN t.scope_type = 'park' THEN loc.name END AS park_label,
    CASE WHEN t.scope_type = 'shed' THEN loc.name END AS shed_label,
    t.title                                       AS task_label,
    t.state                                       AS status,
    t.due_at                                      AS due_at,
    COALESCE(sub.accepted_at, t.verified_at)      AS completed_at,
    vm.display_name                               AS verifier_label
FROM sop_tasks t
LEFT JOIN locations loc ON loc.location_id = t.scope_id
LEFT JOIN LATERAL (
    SELECT accepted_at FROM sop_submissions s
    WHERE s.tenant_id = t.tenant_id AND s.task_id = t.task_id AND s.accepted_at IS NOT NULL
    ORDER BY s.accepted_at DESC LIMIT 1
) sub ON true
LEFT JOIN workforce_members vm ON vm.tenant_id = t.tenant_id AND vm.user_id = t.verified_by;
-- +goose StatementEnd

-- ===========================================================================
-- 11. ceo_ai.verification_queue_status — verification counts per scope/area.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.verification_queue_status AS
SELECT
    vi.tenant_id                                                        AS tenant_id,
    COALESCE(vi.vertical, 'verification')                               AS area,
    pk.name                                                            AS park_label,
    sh.name                                                            AS shed_label,
    COUNT(*) FILTER (WHERE vi.status = 'pending')::bigint               AS pending,
    COUNT(*) FILTER (WHERE vi.status = 'rejected')::bigint              AS rejected,
    COUNT(*) FILTER (WHERE vi.status IN ('accepted','verified'))::bigint AS accepted,
    MIN(vi.captured_at) FILTER (WHERE vi.status = 'pending')            AS oldest_pending_at,
    NULL::text                                                         AS owner_label  -- TODO(source): owner is the shed position holder; operator_id is sensitive
FROM verification_items vi
LEFT JOIN locations sh ON sh.location_id = vi.shed_id
LEFT JOIN locations pk ON pk.location_id = vi.park_id
GROUP BY vi.tenant_id, COALESCE(vi.vertical, 'verification'), pk.name, sh.name;
-- +goose StatementEnd

-- ===========================================================================
-- 12. ceo_ai.inventory_stock_position — stock on hand per item/location.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.inventory_stock_position AS
WITH stock AS (
    SELECT s.tenant_id, s.item_id, s.location_id,
           SUM(s.quantity_in_stock)::numeric        AS stock_on_hand,
           MAX(s.quantity_unit)                     AS unit,
           MAX(s.updated_at)                        AS last_updated_at
    FROM inventory_stock s
    WHERE s.status IS DISTINCT FROM 'retired'
    GROUP BY s.tenant_id, s.item_id, s.location_id
),
last_recon AS (
    SELECT tenant_id, item_id, location_id, MAX(occurred_at) AS last_reconciled_at
    FROM inventory_stock_movements
    WHERE movement_type IN ('reconcile','adjust','adjustment','count')
    GROUP BY tenant_id, item_id, location_id
)
SELECT
    st.tenant_id                                  AS tenant_id,
    it.name                                       AS item_label,
    it.category                                   AS category,
    st.stock_on_hand                              AS stock_on_hand,
    COALESCE(st.unit, it.base_unit)               AS unit,
    COALESCE(pk.name, loc.name)                   AS park_label,
    -- TODO(source): no reorder_point/min_level column exists on inventory_items
    -- or inventory_stock. reorder_flag is a typed NULL until a reorder-threshold
    -- config column is added; the assistant must say "reorder level not configured".
    NULL::boolean                                 AS reorder_flag,
    COALESCE(lr.last_reconciled_at, st.last_updated_at) AS last_reconciled_at
FROM stock st
JOIN inventory_items it ON it.item_id = st.item_id
LEFT JOIN locations loc ON loc.location_id = st.location_id
LEFT JOIN locations pk  ON pk.location_id  = loc.parent_location_id
LEFT JOIN last_recon lr ON lr.tenant_id = st.tenant_id AND lr.item_id = st.item_id AND lr.location_id = st.location_id;
-- +goose StatementEnd

-- ===========================================================================
-- 13. ceo_ai.workforce_coverage_status — coverage per park/role.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.workforce_coverage_status AS
WITH active_absence AS (
    SELECT tenant_id, workforce_member_id, replacement_member_id
    FROM workforce_absences
    WHERE status = 'approved'
      AND now() >= starts_at AND now() < COALESCE(ends_at, 'infinity'::timestamptz)
),
work_load AS (
    -- open assigned SOP tasks per member (bounded aggregate)
    SELECT tenant_id, assigned_to AS user_scope_member,
           COUNT(*) FILTER (WHERE state NOT IN ('completed','verified','canceled'))::bigint AS active_work_count,
           COUNT(*) FILTER (WHERE state NOT IN ('completed','verified','canceled')
                             AND due_at < now())::bigint AS overdue_work_count
    FROM sop_tasks
    WHERE assigned_to IS NOT NULL
    GROUP BY tenant_id, assigned_to
)
SELECT
    wm.tenant_id                                  AS tenant_id,
    loc.name                                      AS park_label,
    COALESCE(rc.label, wm.primary_role_hint)      AS role_label,
    wm.display_name                               AS owner_label,
    rep.display_name                              AS backup_label,
    CASE
        WHEN aa.workforce_member_id IS NULL THEN 'present'
        WHEN aa.replacement_member_id IS NOT NULL THEN 'covered_by_backup'
        ELSE 'uncovered_absence'
    END                                           AS coverage_status,
    COALESCE(wl.active_work_count, 0)             AS active_work_count,
    COALESCE(wl.overdue_work_count, 0)            AS overdue_work_count
FROM workforce_members wm
LEFT JOIN locations        loc ON loc.location_id = wm.primary_location_id
LEFT JOIN org_role_catalog rc  ON rc.role_key     = wm.primary_role_hint
LEFT JOIN active_absence   aa  ON aa.tenant_id = wm.tenant_id AND aa.workforce_member_id = wm.workforce_member_id
LEFT JOIN workforce_members rep ON rep.workforce_member_id = aa.replacement_member_id
LEFT JOIN work_load        wl  ON wl.tenant_id = wm.tenant_id AND wl.user_scope_member = wm.user_id
WHERE wm.status = 'active';
-- +goose StatementEnd

-- ===========================================================================
-- 14. ceo_ai.action_center_current — cross-module actionable items (UNION).
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.action_center_current AS
-- open vaccination obligations that are due/overdue (shed-scoped)
SELECT
    oi.tenant_id                                  AS tenant_id,
    'vaccination'                                 AS area,
    CASE WHEN oi.window_end < (now() AT TIME ZONE 'Asia/Kolkata')::date THEN 'high' ELSE 'medium' END AS severity,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    COALESCE(ceo_ai.vaccine_label_for(pr.dose_code), 'Vaccination') AS title,
    om.display_name                               AS owner_label,
    NULL::text                                    AS backup_label, -- TODO(source): backup seat lookup is per-shed; use vaccination_shed_status for backup
    oi.due_at                                     AS due_at,
    oi.status                                     AS status
FROM obligation_instances oi
LEFT JOIN locations      sh ON sh.location_id = oi.scope_id
LEFT JOIN locations      pk ON pk.location_id = sh.parent_location_id
LEFT JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.tenant_id = oi.tenant_id AND wp.scope_type = 'shed' AND wp.scope_id = oi.scope_id
      AND wp.is_backup_slot = false AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
    LIMIT 1
) om ON true
WHERE oi.scope_type = 'shed' AND oi.status IN ('scheduled','due','in_progress','missed')
UNION ALL
-- pending count-change approvals
SELECT
    ca.tenant_id                                  AS tenant_id,
    'counts'                                      AS area,
    'medium'                                      AS severity,
    NULL::text                                    AS park_label, -- TODO(source): approval carries no direct park; via shifting_event only
    NULL::text                                    AS shed_label,
    COALESCE(ca.request_type, 'count_approval')   AS title,
    NULL::text                                    AS owner_label, -- TODO(source): raised_by_user_id is a user; role/label mapping deferred
    NULL::text                                    AS backup_label,
    ca.raised_at                                  AS due_at,
    ca.status                                     AS status
FROM counts_approval_requests ca
WHERE ca.status = 'pending';
-- +goose StatementEnd

-- ===========================================================================
-- 15. ceo_ai.audit_activity_summary — audit activity per business day/area.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.audit_activity_summary AS
SELECT
    al.tenant_id                                                AS tenant_id,
    (al.created_at AT TIME ZONE 'Asia/Kolkata')::date           AS business_date,
    COALESCE(al.resource_type, al.scope_type, 'general')        AS area,
    -- actor identity is sensitive: surface actor_type (user/system) not personal
    -- name. TODO(source): map to role via workforce_members when a role-only
    -- label is required; keep PII out of the answer.
    COALESCE(al.actor_type, 'system')                           AS actor_label,
    al.action                                                   AS action_label,
    -- TODO(source): audit_log has no explicit result column; success/failure
    -- lives implicitly in after_state/metadata jsonb. Typed NULL until a
    -- first-class outcome field exists.
    (al.metadata->>'result')                                    AS result,
    COUNT(*)::bigint                                            AS count,
    MAX(al.created_at)                                          AS last_activity_at
FROM audit_log al
GROUP BY al.tenant_id,
         (al.created_at AT TIME ZONE 'Asia/Kolkata')::date,
         COALESCE(al.resource_type, al.scope_type, 'general'),
         COALESCE(al.actor_type, 'system'),
         al.action,
         (al.metadata->>'result');
-- +goose StatementEnd

-- ===========================================================================
-- COVERAGE-GAP VIEWS demanded by the discovery judge
-- ===========================================================================

-- 16. ceo_ai.mortality_base — deaths + active population per business day/park,
--     the base the governed Cube mortality_rate metric divides over.
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.mortality_base AS
WITH deaths AS (
    SELECT g.tenant_id, g.park_id,
           (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
           COUNT(*)::bigint AS deaths
    FROM goats g
    WHERE g.exited_at IS NOT NULL AND g.exit_reason IN ('death','dead','mortality')
    GROUP BY g.tenant_id, g.park_id, (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date
),
pop AS (
    SELECT tenant_id, park_id, COUNT(*)::bigint AS active_population
    FROM goats
    WHERE lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, park_id
)
SELECT
    d.tenant_id                                   AS tenant_id,
    d.event_date                                  AS event_date,
    pk.name                                       AS park_label,
    d.deaths                                      AS deaths,
    COALESCE(pop.active_population, 0)            AS active_population
FROM deaths d
LEFT JOIN locations pk ON pk.location_id = d.park_id
LEFT JOIN pop ON pop.tenant_id = d.tenant_id AND pop.park_id IS NOT DISTINCT FROM d.park_id;
-- +goose StatementEnd

-- 17. ceo_ai.feed_adherence — directed vs actual fed per feed day / shed.
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.feed_adherence AS
WITH directed AS (
    SELECT r.tenant_id, i.feed_day, r.shed_id,
           SUM(r.quantity_kg)::numeric AS directed_kg,
           COUNT(*) FILTER (WHERE r.blocked_reason_code IS NOT NULL)::bigint AS blocked_cells
    FROM feed_direction_issue_rows r
    JOIN feed_direction_issues i ON i.feed_direction_issue_id = r.feed_direction_issue_id
    GROUP BY r.tenant_id, i.feed_day, r.shed_id
),
fed AS (
    SELECT tenant_id, shed_id,
           (fed_at AT TIME ZONE 'Asia/Kolkata')::date AS feed_day,
           SUM(quantity_fed)::numeric AS fed_kg
    FROM feed_direction_completions
    WHERE status IN ('accepted','verified','completed')
    GROUP BY tenant_id, shed_id, (fed_at AT TIME ZONE 'Asia/Kolkata')::date
)
SELECT
    d.tenant_id                                   AS tenant_id,
    d.feed_day                                    AS feed_day,
    pk.name                                       AS park_label,
    sh.name                                       AS shed_label,
    d.directed_kg                                 AS directed_kg,
    COALESCE(f.fed_kg, 0)                          AS fed_kg,
    (COALESCE(f.fed_kg, 0) - d.directed_kg)       AS variance_kg,
    (d.blocked_cells > 0)                          AS blocked
FROM directed d
LEFT JOIN locations sh ON sh.location_id = d.shed_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id
LEFT JOIN fed f ON f.tenant_id = d.tenant_id AND f.shed_id = d.shed_id AND f.feed_day = d.feed_day;
-- +goose StatementEnd

-- 18. ceo_ai.notification_delivery_health — reminder/escalation delivery
--     reliability per business day/channel.
-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.notification_delivery_health AS
SELECT
    nr.tenant_id                                                        AS tenant_id,
    (nr.requested_at AT TIME ZONE 'Asia/Kolkata')::date                 AS business_date,
    nr.channel                                                          AS channel,
    nr.notification_type                                                AS notification_type,
    COUNT(*)::bigint                                                    AS requested,
    COUNT(*) FILTER (WHERE nr.status IN ('sent','delivered','read'))::bigint AS sent,
    COUNT(*) FILTER (WHERE nr.status = 'failed')::bigint               AS failed,
    COUNT(*) FILTER (WHERE nr.status IN ('pending','queued','retrying'))::bigint AS pending,
    MIN(nr.requested_at) FILTER (WHERE nr.status IN ('pending','queued','retrying')) AS oldest_pending_at
FROM notification_requests nr
GROUP BY nr.tenant_id,
         (nr.requested_at AT TIME ZONE 'Asia/Kolkata')::date,
         nr.channel, nr.notification_type;
-- +goose StatementEnd

-- ===========================================================================
-- Read-only SQL fallback stub — MUST NOT run dynamic SQL until the backend
-- validator + audit logging land (docs/ceo-ai/mcp-toolbox-plan.md). Fails loud.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ceo_ai.run_readonly_sql(sql text, tenant uuid)
RETURNS SETOF jsonb
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = ceo_ai, pg_temp
AS $rs$
BEGIN
    RAISE EXCEPTION 'ceo_ai.run_readonly_sql: implement through the Mesha backend SQL validator first (SELECT-only, ceo_ai.* allowlist, mandatory tenant filter, LIMIT<=100)';
END;
$rs$;
-- deny the dynamic-SQL fallback to everyone by default; the backend calls it
-- only after its validator lands and an explicit grant is added.
REVOKE ALL ON FUNCTION ceo_ai.run_readonly_sql(text, uuid) FROM PUBLIC;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — apply only if the read-only roles exist. No role creation,
-- no passwords here (that is tools/dev/setup-ceo-ai-local-role.sh + Secret
-- Manager). Idempotent: safe to re-run.
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('REVOKE ALL ON SCHEMA public FROM %I', r);
            EXECUTE format('GRANT USAGE ON SCHEMA ceo_ai TO %I', r);
            EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA ceo_ai TO %I', r);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA ceo_ai GRANT SELECT ON TABLES TO %I', r);
            -- fallback exec is denied until the backend validator lands
            EXECUTE format('REVOKE ALL ON FUNCTION ceo_ai.run_readonly_sql(text, uuid) FROM %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd


-- -----------------------------------------------------------------------------
-- Collapsed from 000025_ceo_ai_assistant_tables.sql
-- -----------------------------------------------------------------------------
-- ===========================================================================
-- ceo_ai assistant app-owned tables (public schema) — the durable state the
-- Mesha leadership assistant backend writes: audit/trace, conversation threads,
-- messages, feedback, response cache, and a persisted rate-limit window.
--
-- These are APP-OWNED (written by the Go backend service, NOT by the read-only
-- roles). mesha_ceo_readonly / mesha_cube_readonly have NO access to public
-- (REVOKE ALL ON SCHEMA public, per 000020 + setup script) — the assistant
-- reads business data only through ceo_ai.* views and writes its own state here
-- with the normal app credential.
--
-- Design honors AGENTS.md: keyset-friendly indexes (no OFFSET), tenant-scoped,
-- idempotency/soft-delete + retention where the plan requires it, jsonb for
-- step traces / tool calls / citations (INTERNAL only — never returned in the
-- leadership chat answer, per the internal-tracking rule).
-- ===========================================================================

-- ---------------------------------------------------------------------------
-- ceo_ai_assistant_audit — one tamper-evident row per assistant request.
-- Internal/admin-only. Holds the step trace + review verdict for the debug
-- surface; NEVER surfaced in the user chat answer.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_assistant_audit (
    audit_id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid,                         -- authenticated leadership user (sensitive)
    actor_role         text,                         -- e.g. ceo_internal
    conversation_id    uuid,                         -- FK added after ceo_ai_conversations exists
    request_id         text NOT NULL,                -- correlation id returned to the client
    question_hash      text NOT NULL,                -- sha256 of normalized question (no raw PII in indexes)
    question_redacted  text,                         -- redacted question text for debug
    route_tier         text,                         -- cube | mesha_api | mcp_toolbox | sql_fallback
    tool_called        text,                         -- resolved tool / metric / view name
    generated_sql_hash text,                         -- hash of validated fallback SQL (when used)
    source_views       text[] NOT NULL DEFAULT '{}', -- ceo_ai.* views touched
    row_count          integer,
    latency_ms         integer,
    status             text NOT NULL DEFAULT 'ok',   -- ok | rejected | error | over_budget | degraded
    rejection_reason   text,
    step_trace         jsonb NOT NULL DEFAULT '[]'::jsonb, -- INTERNAL plan/route/exec timeline
    review_verdict     text,                         -- optional MESHA_AI_REVIEW verdict (pass/flag)
    model_version      text,
    prompt_version     text,
    created_at         timestamptz NOT NULL DEFAULT now()
);

-- keyset browse of the internal audit log (newest-first), tenant-scoped.
CREATE INDEX ceo_ai_assistant_audit_tenant_created_idx
    ON ceo_ai_assistant_audit (tenant_id, created_at DESC, audit_id DESC);
CREATE INDEX ceo_ai_assistant_audit_request_idx
    ON ceo_ai_assistant_audit (tenant_id, request_id);
CREATE INDEX ceo_ai_assistant_audit_conversation_idx
    ON ceo_ai_assistant_audit (conversation_id, created_at DESC)
    WHERE conversation_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- ceo_ai_conversations — durable threads (create/list/resume/archive + purge).
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_conversations (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid NOT NULL,                -- owning leadership user
    title              text,                         -- derived from first question; renamable
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    archived_at        timestamptz,                  -- soft-archive (user hide)
    retention_expires_at timestamptz                 -- hard-purge boundary (retention policy)
);

-- keyset list of a user's active threads, newest-updated first.
CREATE INDEX ceo_ai_conversations_owner_idx
    ON ceo_ai_conversations (tenant_id, actor_id, updated_at DESC, id DESC)
    WHERE archived_at IS NULL;
-- purge scan by retention boundary.
CREATE INDEX ceo_ai_conversations_retention_idx
    ON ceo_ai_conversations (retention_expires_at)
    WHERE retention_expires_at IS NOT NULL;

-- backfill the audit FK now that the table exists.
ALTER TABLE ceo_ai_assistant_audit
    ADD CONSTRAINT ceo_ai_assistant_audit_conversation_fk
    FOREIGN KEY (conversation_id) REFERENCES ceo_ai_conversations (id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- ceo_ai_messages — the turn-by-turn message history for a thread.
-- tool_calls + citations are jsonb: citations MAY be shown to the user
-- (structured provenance chips), tool_calls are INTERNAL only.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_messages (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id    uuid NOT NULL REFERENCES ceo_ai_conversations (id) ON DELETE CASCADE,
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    role               text NOT NULL,                -- user | assistant | system
    content            text NOT NULL,
    tool_calls         jsonb NOT NULL DEFAULT '[]'::jsonb, -- INTERNAL routing/exec detail
    citations          jsonb NOT NULL DEFAULT '[]'::jsonb, -- user-visible provenance
    source             text,                         -- resolved source surface for assistant turns
    mode               text,                         -- governed | operational | exploratory
    request_id         text,                         -- links to ceo_ai_assistant_audit.request_id
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ceo_ai_messages_role_chk CHECK (role IN ('user','assistant','system'))
);

-- ordered keyset read of a thread's messages (required index from the spec).
CREATE INDEX ceo_ai_messages_conversation_created_idx
    ON ceo_ai_messages (conversation_id, created_at, id);

-- ---------------------------------------------------------------------------
-- ceo_ai_feedback — thumbs + reason on an assistant message, for eval mining.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_feedback (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id         uuid NOT NULL REFERENCES ceo_ai_messages (id) ON DELETE CASCADE,
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid NOT NULL,
    rating             smallint NOT NULL,            -- +1 thumbs up, -1 thumbs down
    reason             text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ceo_ai_feedback_rating_chk CHECK (rating IN (-1, 1)),
    -- one feedback per (message, actor); a re-vote updates it.
    CONSTRAINT ceo_ai_feedback_message_actor_unique UNIQUE (message_id, actor_id)
);

CREATE INDEX ceo_ai_feedback_tenant_created_idx
    ON ceo_ai_feedback (tenant_id, created_at DESC, id DESC);

-- ---------------------------------------------------------------------------
-- ceo_ai_response_cache — semantic/response cache keyed by tenant + question
-- hash + as_of bucket. Never crosses tenants.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_response_cache (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    question_hash      text NOT NULL,                -- sha256(normalized question + as_of bucket)
    answer             jsonb NOT NULL,               -- cached composed answer + metadata
    source_views       text[] NOT NULL DEFAULT '{}',
    created_at         timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL,         -- TTL / as_of-day rollover invalidation
    CONSTRAINT ceo_ai_response_cache_tenant_hash_unique UNIQUE (tenant_id, question_hash)
);

-- sweep expired cache rows.
CREATE INDEX ceo_ai_response_cache_expiry_idx
    ON ceo_ai_response_cache (expires_at);

-- ---------------------------------------------------------------------------
-- ceo_ai_rate_limit — persisted per-(tenant, actor) fixed-window request
-- counter so caps survive a process restart (the in-Go token bucket resets on
-- restart). Backend increments count within the current window_start bucket.
-- ---------------------------------------------------------------------------
CREATE TABLE ceo_ai_rate_limit (
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    actor_id           uuid NOT NULL,
    window_start       timestamptz NOT NULL,         -- truncated window bucket start
    count              integer NOT NULL DEFAULT 0,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_id, window_start)
);

-- prune old windows.
CREATE INDEX ceo_ai_rate_limit_window_idx
    ON ceo_ai_rate_limit (window_start);


-- -----------------------------------------------------------------------------
-- Collapsed from 000026_ceo_ai_conversation_idempotency.sql
-- -----------------------------------------------------------------------------
-- ===========================================================================
-- ceo_ai_conversations idempotent-create key.
--
-- 000021 shipped ceo_ai_conversations without an idempotency key. The assistant
-- persistence layer's create path is required to be replay-safe (a retried
-- POST /conversations must return the original thread, never fork a second one),
-- which needs a stable per-(tenant, actor) key plus a partial unique index.
--
-- Additive + lock-safe: ADD COLUMN of a nullable text is a metadata-only change
-- (no table rewrite), and the table is new so the unique index build is trivial.
-- NULLs stay distinct under the partial index, so keyless creates never collide;
-- only a keyed (tenant, actor, key) create is deduplicated on replay.
-- ===========================================================================

ALTER TABLE ceo_ai_conversations
    ADD COLUMN IF NOT EXISTS idempotency_key text;

CREATE UNIQUE INDEX IF NOT EXISTS ceo_ai_conversations_idem_uq
    ON ceo_ai_conversations (tenant_id, actor_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;


-- -----------------------------------------------------------------------------
-- Collapsed from 000027_ceo_ai_operator_views.sql
-- -----------------------------------------------------------------------------
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai OPERATOR-GRAIN DRIVE reporting — the governed, business-language
-- read surface for the OPERATOR-BASED DRIVE model.
--
-- WHY THIS EXISTS
-- Main moved DRIVE planning to an OPERATOR grain: drives are planned
-- by operator animal CAPACITY, work is assigned at OPERATOR grain, and proof is
-- shed-level video. The prior ceo_ai reporting views (migration 000023) answer
-- the SHED question (which sheds are due/overdue). They carry NO operator
-- dimension, so the leadership assistant could not answer "which operators are
-- behind", "who is overloaded", "operator assignments today", or "how many
-- animals is <operator> assigned". This view closes that gap over the REAL new
-- operator tables.
--
-- SOURCE (operator model, learned from main):
--   * public.drive_assignments (one row per batch, operator, park, shed grain).
--     Columns used: tenant_id, operator_id, park_id, shed_id,
--     physical_shed, partition_label, animal_count, capacity_status, batch_id,
--     planned_date. operator_id FK → workforce_members(workforce_member_id).
--   * public.obligation_batches — batch_id status tells whether the assigned
--     work is done vs still open. Each assignment row references exactly ONE batch
--     (assignment→batch is 1:1), so joining batch status never fans out.
--   * public.capacity_config — tenant-scoped max_per_day is the
--     animal CAP one operator handles on one business date (the unit
--     the OperatorPlanner consumes).
--   * public.workforce_members — operator display label.
--
-- GRAIN: one row per (tenant_id, operator_id, planned_date, park_id, shed_id).
-- This preserves BOTH the operator dimension AND the park→shed scope leadership
-- filters on. Capacity/utilization are a per-operator-per-DAY concept, so a
-- day-level window carries the operator's whole-day assigned total and the
-- utilization ratio onto every shed row (capacity is NOT summed across sheds —
-- summing a per-day cap across shed rows would multiply it; the Cube measure
-- uses MAX(daily_capacity) at the operator-day grouping to avoid that trap).
--
-- SCALE: vaccination_drive_assignments is a DRIVE-PLAN table (bounded by
-- operators × business days × sheds in the planning window), NOT a per-animal
-- table, so this is a bounded reporting read at the 5k-50k envelope
-- (docs/decisions/operational-kernel-5k-50k-scale-envelope.md). The tenant
-- predicate pushes into the base scan because tenant_id is a GROUP BY key, and
-- vaccination_drive_assignments_operator_day_idx (tenant_id, operator_id,
-- planned_date) covers the operator/day access.
--
-- IST business calendar: "overdue" compares planned_date to the Asia/Kolkata
-- business day, never a UTC instant.
-- ===========================================================================

-- projection-review: membership=canonical vaccination_drive_assignments rows filtered to the current tenant (operator_id NOT NULL); group_key=(tenant_id, operator_id, planned_date, park_id, shed_id) — every SUM/FILTER below groups on the same key; join_cardinality=assignment→obligation_batches is 1:1 on (tenant_id,batch_id) so the batch-status FILTER never double-counts animal_count, and workforce_members/locations/capacity_config are 1:1 dimension joins on their keys (max_per_day is tenant-constant, joined 1:1); pagination=drive-plan table bounded by operators×days×sheds, read tenant-scoped with LIMIT at the Cube/toolbox call site; scope=explicit park_id/shed_id + operator_id preserved on every row so leadership scope (operator→park→shed→day) and the capacity/overdue matrix resolve without collapsing operators or sheds. daily_capacity is per-operator-per-day (NOT additive across sheds); utilization is computed from a per-(operator,day) window sum, and the Cube twin aggregates capacity with MAX not SUM. Adversarial grain proof: backend/internal/ceoai/reporting/operator_views_test.go (Postgres-gated).
CREATE OR REPLACE VIEW ceo_ai.vaccination_operator_status AS
WITH assigned AS (
    SELECT
        a.tenant_id,
        a.operator_id,
        a.park_id,
        a.shed_id,
        a.planned_date,
        COALESCE(SUM(a.animal_count), 0)::bigint                                            AS assigned_animals,
        COALESCE(SUM(a.animal_count) FILTER (WHERE b.status IN ('planned','in_progress')), 0)::bigint AS due,
        COALESCE(SUM(a.animal_count) FILTER (WHERE b.status = 'completed'), 0)::bigint       AS done,
        COALESCE(SUM(a.animal_count) FILTER (
            WHERE b.status IN ('planned','in_progress')
              AND a.planned_date < (now() AT TIME ZONE 'Asia/Kolkata')::date
        ), 0)::bigint                                                                        AS overdue,
        bool_or(a.capacity_status IN ('over_cap_required','capacity_action'))               AS any_over_cap
    FROM public.vaccination_drive_assignments a
    LEFT JOIN public.obligation_batches b
      ON b.tenant_id = a.tenant_id AND b.batch_id = a.batch_id
    WHERE a.operator_id IS NOT NULL
    GROUP BY a.tenant_id, a.operator_id, a.park_id, a.shed_id, a.planned_date
),
cap AS (
    -- tenant-wide per-operator-per-day animal cap (only 'tenant' scope is honored
    -- by the planner today; see vaccinationexecution/domain/capacity.go).
    SELECT tenant_id, max_per_day
    FROM public.vaccination_capacity_config
    WHERE capacity_scope = 'tenant'
)
SELECT
    s.tenant_id                                                            AS tenant_id,
    s.operator_id                                                          AS operator_id,
    wm.display_name                                                        AS operator_label,
    s.park_id                                                             AS park_id,
    pk.name                                                                AS park_label,
    s.shed_id                                                             AS shed_id,
    sh.name                                                                AS shed_label,
    s.planned_date                                                        AS planned_date,
    s.assigned_animals                                                     AS assigned_animals,
    s.due                                                                  AS due,
    s.done                                                                 AS done,
    s.overdue                                                              AS overdue,
    COALESCE(cap.max_per_day, 200)                                         AS daily_capacity,
    -- the operator's WHOLE-day assigned total (across every shed that day), the
    -- correct numerator for a per-operator-per-day utilization ratio.
    SUM(s.assigned_animals) OVER (
        PARTITION BY s.tenant_id, s.operator_id, s.planned_date
    )                                                                     AS operator_day_assigned,
    ROUND(
        (SUM(s.assigned_animals) OVER (
            PARTITION BY s.tenant_id, s.operator_id, s.planned_date
        ))::numeric / NULLIF(COALESCE(cap.max_per_day, 200), 0), 3
    )                                                                     AS utilization,
    CASE
        WHEN s.overdue > 0 THEN 'catch_up_overdue'
        WHEN (SUM(s.assigned_animals) OVER (
                 PARTITION BY s.tenant_id, s.operator_id, s.planned_date))
             > COALESCE(cap.max_per_day, 200) THEN 'rebalance_overloaded'
        WHEN s.due > 0 THEN 'run_drive'
        WHEN s.done > 0 AND s.due = 0 THEN 'completed'
        ELSE 'no_action'
    END                                                                   AS next_action
FROM assigned s
LEFT JOIN public.workforce_members wm ON wm.workforce_member_id = s.operator_id
LEFT JOIN public.locations         pk ON pk.location_id = s.park_id
LEFT JOIN public.locations         sh ON sh.location_id = s.shed_id
LEFT JOIN cap ON cap.tenant_id = s.tenant_id;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — re-apply the ceo_ai SELECT grant for the new view if the
-- read-only roles exist (idempotent; mirrors migration 000023). ALTER DEFAULT
-- PRIVILEGES from 000023 already covers objects created afterward, but an
-- explicit grant here keeps this migration self-contained and safe to re-run.
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.vaccination_operator_status TO %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd


-- -----------------------------------------------------------------------------
-- Collapsed from 000028_vaccination_drive_date_overrides.sql
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.vaccination_drive_date_overrides (
  override_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  vaccine_code text NOT NULL,
  original_drive_date date NOT NULL,
  override_date date NOT NULL,
  reason text NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  canceled_at timestamp with time zone,
  canceled_by uuid,
  cancel_reason text,
  CONSTRAINT vaccination_drive_date_overrides_pkey PRIMARY KEY (override_id),
  CONSTRAINT vaccination_drive_date_overrides_vaccine_code_not_blank CHECK (btrim(vaccine_code) <> ''),
  CONSTRAINT vaccination_drive_date_overrides_reason_not_blank CHECK (btrim(reason) <> ''),
  CONSTRAINT vaccination_drive_date_overrides_postpone_check CHECK (override_date > original_drive_date),
  CONSTRAINT vaccination_drive_date_overrides_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_date_overrides_active_uq
  ON public.vaccination_drive_date_overrides (tenant_id, park_id, (lower(btrim(vaccine_code))), original_drive_date)
  WHERE canceled_at IS NULL;

CREATE INDEX IF NOT EXISTS vaccination_drive_date_overrides_lookup_idx
  ON public.vaccination_drive_date_overrides (tenant_id, park_id, original_drive_date, (lower(btrim(vaccine_code))))
  WHERE canceled_at IS NULL;


-- -----------------------------------------------------------------------------
-- Collapsed from 000029_vaccination_drive_assignment_dose_summary.sql
-- -----------------------------------------------------------------------------
ALTER TABLE vaccination_drive_assignments
  ADD COLUMN IF NOT EXISTS vaccine_rule_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
  ADD COLUMN IF NOT EXISTS total_doses integer NOT NULL DEFAULT 0;

ALTER TABLE vaccination_drive_assignments
  DROP CONSTRAINT IF EXISTS vaccination_drive_assignments_total_doses_check,
  ADD CONSTRAINT vaccination_drive_assignments_total_doses_check
    CHECK (total_doses >= 0);


-- -----------------------------------------------------------------------------
-- Collapsed from 000030_ceo_ai_cube_source_views.sql
-- -----------------------------------------------------------------------------
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai CUBE SOURCE views — governed, business-language read surfaces that the
-- Cube Core semantic layer reads FROM, so Cube never touches raw public tables.
--
-- WHY THIS EXISTS
-- Cube Core connects to Postgres as the `mesha_cube_readonly` role, which by
-- design has `REVOKE ALL ON SCHEMA public` and SELECT on `ceo_ai.*` ONLY
-- (tools/dev/setup-ceo-ai-local-role.sh). But five Cube models still ran inline
-- SQL `FROM obligation_instances / goats / feed_direction_completions /
-- procurement_loads / sop_tasks` — raw public tables. Inline-SQL cubes execute
-- with the connecting role's own privileges (no view-owner indirection), so
-- every such query failed with `permission denied for table obligation_instances`
-- and the leadership assistant returned `cube: could not be retrieved.`
--
-- FIX (matches migration 000027, which already repointed the operator cube):
-- expose one thin passthrough view per cube at the SAME row grain and with the
-- SAME column names the cube models already select. These views are owned by the
-- migration role, so Postgres runs them with owner rights (default view
-- behavior, security_invoker off) and the read-only Cube role can SELECT them
-- without any grant on public. The measure FORMULAS live in the Cube models and
-- are unchanged; only the FROM source moves from raw public to ceo_ai.*.
--
-- GRAIN (unchanged, one row per):
--   ceo_ai.vaccination_obligations_base  -> obligation_instances (per obligation)
--   ceo_ai.animals_base                  -> goats               (per goat)
--   ceo_ai.feed_completions_base         -> feed_direction_completions (per completion)
--   ceo_ai.procurement_loads_base        -> procurement_loads   (per load)
--   ceo_ai.workforce_tasks_base          -> sop_tasks           (per task)
--
-- IST business calendar: business-day columns convert to Asia/Kolkata first,
-- never the UTC instant, identical to the cube inline SQL they replace.
-- ===========================================================================

CREATE OR REPLACE VIEW ceo_ai.vaccination_obligations_base AS
SELECT
    o.obligation_id                                    AS obligation_id,
    o.tenant_id                                        AS tenant_id,
    o.status                                           AS status,
    o.due_at                                           AS due_at,
    o.completed_at                                     AS completed_at,
    o.scope_id                                         AS shed_id,
    sh.name                                            AS shed_label,
    sh.parent_location_id                              AS park_id,
    pk.name                                            AS park_label,
    g.species                                          AS species,
    (o.due_at AT TIME ZONE 'Asia/Kolkata')::date       AS due_business_day,
    (o.completed_at AT TIME ZONE 'Asia/Kolkata')::date AS completed_business_day
FROM obligation_instances o
LEFT JOIN locations sh ON sh.location_id = o.scope_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id
LEFT JOIN goats    g  ON g.goat_id      = o.target_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.animals_base AS
SELECT
    g.goat_id                                          AS goat_id,
    g.tenant_id                                        AS tenant_id,
    g.species                                          AS species,
    g.lifecycle_status                                 AS lifecycle_status,
    g.management_stage                                 AS management_stage,
    g.park_id                                          AS park_id,
    pk.name                                            AS park_label,
    g.shed_id                                          AS shed_id,
    sh.name                                            AS shed_label,
    g.entry_date                                       AS entry_date,
    (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date    AS exit_business_day,
    g.exit_reason                                      AS exit_reason
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.feed_completions_base AS
SELECT
    c.completion_id                                    AS completion_id,
    c.tenant_id                                        AS tenant_id,
    c.shed_id                                          AS shed_id,
    sh.name                                            AS shed_label,
    sh.parent_location_id                              AS park_id,
    pk.name                                            AS park_label,
    c.quantity_fed                                     AS quantity_fed,
    c.head_count                                       AS head_count,
    c.status                                           AS status,
    (c.fed_at AT TIME ZONE 'Asia/Kolkata')::date       AS fed_business_day
FROM feed_direction_completions c
LEFT JOIN locations sh ON sh.location_id = c.shed_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.procurement_loads_base AS
SELECT
    pl.load_id                                         AS load_id,
    pl.tenant_id                                       AS tenant_id,
    pl.status                                          AS status,
    pl.expected_count                                  AS expected_count,
    pl.source_location_id                              AS source_location_id,
    loc.name                                           AS source_label,
    (pl.purchase_date)                                 AS purchase_date,
    (pl.created_at AT TIME ZONE 'Asia/Kolkata')::date  AS entered_business_day
FROM procurement_loads pl
LEFT JOIN locations loc ON loc.location_id = pl.source_location_id;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE VIEW ceo_ai.workforce_tasks_base AS
SELECT
    t.task_id                                          AS task_id,
    t.tenant_id                                        AS tenant_id,
    t.state                                            AS state,
    t.task_type                                        AS task_type,
    t.assigned_to                                      AS operator_id,
    t.scope_id                                         AS scope_id,
    sh.name                                            AS shed_label,
    sh.parent_location_id                              AS park_id,
    pk.name                                            AS park_label,
    t.verified_at                                      AS verified_at,
    (t.due_at AT TIME ZONE 'Asia/Kolkata')::date       AS due_business_day
FROM sop_tasks t
LEFT JOIN locations sh ON sh.location_id = t.scope_id
LEFT JOIN locations pk ON pk.location_id = sh.parent_location_id;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — grant SELECT on the new cube source views to the read-only
-- roles if they exist (idempotent; mirrors migration 000027).
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
    v text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            FOREACH v IN ARRAY ARRAY[
                'ceo_ai.vaccination_obligations_base',
                'ceo_ai.animals_base',
                'ceo_ai.feed_completions_base',
                'ceo_ai.procurement_loads_base',
                'ceo_ai.workforce_tasks_base'
            ] LOOP
                EXECUTE format('GRANT SELECT ON %s TO %I', v, r);
            END LOOP;
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd


-- -----------------------------------------------------------------------------
-- Collapsed from 000031_assistant_roles_public_read.sql
-- -----------------------------------------------------------------------------
-- +goose StatementBegin
-- ===========================================================================
-- Full read (SELECT) on schema public for the leadership-assistant read-only
-- DB roles.
--
-- MAINTAINER DECISION 2026-07-23: the leadership assistant is an INTERNAL,
-- CEO/CXO-only, READ-ONLY chat. It should not hit a permission wall on current
-- public tables. The prior boundary (ceo_ai.* SELECT only, public
-- revoked) caused governed Cube metrics that read public tables to fail with
-- `permission denied for table ...`. Per the maintainer, remove that schema
-- restriction: grant the assistant roles SELECT on ALL current public tables.
-- Future tables are covered by ALTER DEFAULT PRIVILEGES FOR ROLE <owner>, applied
-- per current table owner by tools/dev/grant-assistant-public-read.sh (a brand-new
-- owner role needs a re-run). Access stays READ-ONLY — only SELECT is
-- granted and the roles carry `default_transaction_read_only = on`; no INSERT/
-- UPDATE/DELETE/DDL is granted. This supersedes the "Cube never reads raw
-- Postgres" restriction for these two read-only roles only.
--
-- ORDERING (P1): this is GUARDED with IF EXISTS. A role that does NOT yet exist
-- when this migration runs receives NO grant here. Cloud Deploy running the
-- migration ALONE does not guarantee the grant lands. Create the Cloud SQL
-- readonly roles BEFORE migrating, or re-apply afterward (idempotent) with:
--     make grant-assistant-public-read   (tools/dev/grant-assistant-public-read.sh)
-- The local path (tools/dev/setup-ceo-ai-local-role.sh) creates the roles then
-- grants in the correct order already.
--
-- DEFAULT PRIVILEGES SCOPE (P2): ALTER DEFAULT PRIVILEGES below covers only
-- tables created by the SAME role that executes this migration. This assumes all
-- schema migrations run as one owner (the migration role). Tables later created
-- by a DIFFERENT owner do NOT auto-grant SELECT and need a re-run of the grant
-- command above.
-- ===========================================================================
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT USAGE ON SCHEMA public TO %I', r);
            EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA public TO %I', r);
            EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd


-- -----------------------------------------------------------------------------
-- Collapsed from 000032_workforce_position_vaccination_cap.sql
-- -----------------------------------------------------------------------------
ALTER TABLE public.workforce_positions
  ADD COLUMN IF NOT EXISTS vaccination_daily_animal_cap integer;

ALTER TABLE public.workforce_positions
  DROP CONSTRAINT IF EXISTS workforce_positions_vaccination_daily_animal_cap_check,
  ADD CONSTRAINT workforce_positions_vaccination_daily_animal_cap_check
    CHECK (vaccination_daily_animal_cap IS NULL OR vaccination_daily_animal_cap BETWEEN 1 AND 200);

COMMENT ON COLUMN public.workforce_positions.vaccination_daily_animal_cap IS
  'Optional HRMS-authored vaccination animal capacity for this operator seat. Null means use vaccination_capacity_config.max_per_day.';


-- -----------------------------------------------------------------------------
-- Collapsed from 000033_rescope_shed_proof_artifacts_to_shed.sql
-- -----------------------------------------------------------------------------
-- Re-scope proof artifacts from the old contract (scope_type='task', subject_type='shed')
-- to the new shed scope (scope_type='shed', scope_id=subject_id) to ensure that shed-level
-- shed-level proof videos are counted correctly by the shed readiness queries.
--
-- The old contract stored proof rows with:
--   scope_type='task' (parent sop_task_id) and subject_type='shed' (the shed_id)
-- The new contract scopes them directly:
--   scope_type='shed' and scope_id=subject_id (the shed_id)
--
-- This UPDATE is idempotent and lock-safe (no DDL, no constraints added). Only rows that need
-- re-scoping are updated (filtered by the WHERE clause); re-running is harmless.
--
-- seed-migration-guard:ignore owner=ravi issue=shed-proof-scope-review-followup reason=no-seed-impact: a fresh seed writes shed proofs at scope_type='shed' already (client sends shed scope), so this back-compat re-scope matches zero fresh-seeded rows; it only repairs pre-existing task-scoped shed proofs on already-populated envs expiry=2026-10-31

UPDATE public.proof_artifacts
   SET scope_type = 'shed',
       scope_id   = subject_id
 WHERE scope_type = 'task'
   AND subject_type = 'shed'
   AND subject_id IS NOT NULL
   AND scope_id <> subject_id;


-- -----------------------------------------------------------------------------
-- Collapsed from 000034_drop_ceo_ai_feedback.sql
-- -----------------------------------------------------------------------------
DROP TABLE IF EXISTS ceo_ai_feedback;


-- -----------------------------------------------------------------------------
-- Collapsed from 000035_vaccination_operator_assignment_config.sql
-- -----------------------------------------------------------------------------
-- Persist the vaccination operator shift + N-active-operators-per-day default assignment config.
-- The admin config screen authors these rows, and the drive/obligation scheduler consumes them
-- when choosing the operator set for each business day.

CREATE TABLE IF NOT EXISTS public.vaccination_operator_assignment_config (
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  active_operators_per_day integer NOT NULL DEFAULT 1,
  default_operator_id uuid NOT NULL,
  selected_operator_ids uuid[] NOT NULL DEFAULT '{}'::uuid[],
  row_version bigint NOT NULL DEFAULT 1,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_operator_assignment_config_pkey PRIMARY KEY (tenant_id, park_id),
  CONSTRAINT vaccination_operator_assignment_config_n_check
    CHECK (active_operators_per_day >= 1 AND active_operators_per_day <= 3),
  CONSTRAINT vaccination_operator_assignment_config_row_version_check
    CHECK (row_version >= 1),
  CONSTRAINT vaccination_operator_assignment_config_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT vaccination_operator_assignment_config_default_operator_fk
    FOREIGN KEY (default_operator_id)
    REFERENCES public.workforce_members (workforce_member_id)
);

ALTER TABLE public.vaccination_operator_assignment_config
  ADD COLUMN IF NOT EXISTS selected_operator_ids uuid[] NOT NULL DEFAULT '{}'::uuid[];

CREATE INDEX IF NOT EXISTS vaccination_operator_assignment_config_tenant_park_idx
  ON public.vaccination_operator_assignment_config (tenant_id, park_id);

CREATE TABLE IF NOT EXISTS public.vaccination_operator_shift_config (
  operator_id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shift_start_minute integer NOT NULL,
  shift_end_minute integer NOT NULL,
  shift_label text NOT NULL,
  week_off_weekday text,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_operator_shift_config_pkey PRIMARY KEY (tenant_id, operator_id, park_id),
  CONSTRAINT vaccination_operator_shift_config_start_minute_check
    CHECK (shift_start_minute >= 0 AND shift_start_minute <= 1439),
  CONSTRAINT vaccination_operator_shift_config_end_minute_check
    CHECK (shift_end_minute >= 0 AND shift_end_minute <= 1439),
  CONSTRAINT vaccination_operator_shift_config_label_check
    CHECK (shift_label IN ('am', 'pm', 'rover')),
  CONSTRAINT vaccination_operator_shift_config_week_off_check
    CHECK (week_off_weekday IS NULL OR week_off_weekday IN
      ('monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday')),
  CONSTRAINT vaccination_operator_shift_config_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT vaccination_operator_shift_config_operator_fk
    FOREIGN KEY (operator_id)
    REFERENCES public.workforce_members (workforce_member_id)
);

CREATE INDEX IF NOT EXISTS vaccination_operator_shift_config_tenant_park_idx
  ON public.vaccination_operator_shift_config (tenant_id, park_id);

COMMENT ON TABLE public.vaccination_operator_assignment_config IS
  'N active operators/day + CEO-set default operator per park. Consumed by the drive scheduler when planning daily operator assignment.';
COMMENT ON TABLE public.vaccination_operator_shift_config IS
  'Per-operator shift window (minutes-of-day) + week-off weekday, per park. Consumed by the drive scheduler when resolving default/PM/shift fallback.';


-- -----------------------------------------------------------------------------
-- Collapsed from 000036_operator_config_replan_watermarks.sql
-- -----------------------------------------------------------------------------
-- Idempotency dedupe table for the operator-config auto-cascade consumer
-- (backend/internal/obligation/app/operator_config_replan.go). On
-- vaccination.capacity.changed / vaccination.roster.changed / vaccination.leave.changed, the consumer
-- claims a row here keyed by (tenant_id, event_id) BEFORE calling the existing
-- RecomputeFutureVaccinationDrives release path. A replay of the SAME event_id is a no-op (ON CONFLICT
-- DO NOTHING => 0 rows affected => consumer returns without re-invoking recompute), matching the
-- watermark-claim-in-tx pattern used by obligation_goat_shift_watermarks for goat.location.changed.
CREATE TABLE IF NOT EXISTS public.obligation_operator_config_replan_watermarks (
  tenant_id uuid NOT NULL,
  event_id text NOT NULL,
  park_id uuid NOT NULL,
  event_type text NOT NULL,
  processed_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT obligation_operator_config_replan_watermarks_pkey PRIMARY KEY (tenant_id, event_id)
);

CREATE INDEX IF NOT EXISTS obligation_operator_config_replan_watermarks_park_idx
  ON public.obligation_operator_config_replan_watermarks (tenant_id, park_id);


-- -----------------------------------------------------------------------------
-- Collapsed from 000037_operator_config_replan_watermark_status.sql
-- -----------------------------------------------------------------------------
-- Add status column to operator_config_replan_watermarks to track pending/succeeded state.
-- The two-phase watermark pattern: claim as 'pending' before recompute, mark 'succeeded' only
-- if recompute succeeds. On retry/redelivery, if status != 'succeeded', retry recompute.
-- This makes the consumer durable and idempotent: exactly-once effect, at-least-once attempt.
ALTER TABLE public.obligation_operator_config_replan_watermarks
ADD COLUMN status text DEFAULT 'pending' NOT NULL;

-- Index to support querying pending events (not yet fully processed)
CREATE INDEX IF NOT EXISTS obligation_operator_config_replan_watermarks_status_idx
  ON public.obligation_operator_config_replan_watermarks (tenant_id, status)
  WHERE status = 'pending';


-- -----------------------------------------------------------------------------
-- Collapsed from 000038_cascade_outbox_idempotency_index.sql
-- -----------------------------------------------------------------------------
-- +goose NO TRANSACTION
-- CONCURRENTLY cannot run inside a transaction; hot table => NO TRANSACTION.

-- Dedup guard: a populated database could already hold duplicate
-- (tenant_id, idempotency_key) rows among the cascade event types (rows written
-- before this index existed). A CREATE UNIQUE INDEX CONCURRENTLY would fail on
-- such rows. Keep the earliest row per (tenant_id, event_type, idempotency_key)
-- among the covered event types and delete the rest. One-way, non-reversible,
-- scoped to the covered event types only.
-- +goose StatementBegin
DO $$
BEGIN
  DELETE FROM public.outbox_messages dupe
  USING (
    SELECT outbox_id
    FROM (
      SELECT outbox_id,
             row_number() OVER (
               PARTITION BY tenant_id, event_type, idempotency_key
               ORDER BY created_at ASC, outbox_id ASC
             ) AS rn
      FROM public.outbox_messages
      WHERE idempotency_key IS NOT NULL
        AND event_type = ANY (ARRAY[
          'vaccination.capacity.changed'::text,
          'vaccination.roster.changed'::text,
          'vaccination.leave.changed'::text
        ])
    ) ranked
    WHERE ranked.rn > 1
  ) losers
  WHERE dupe.outbox_id = losers.outbox_id;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_capacity_changed_idempotency_idx
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = 'vaccination.capacity.changed');

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_roster_changed_idempotency_idx
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = 'vaccination.roster.changed');

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_messages_leave_changed_idempotency_idx
  ON public.outbox_messages USING btree (tenant_id, idempotency_key)
  WHERE (event_type = 'vaccination.leave.changed');


-- -----------------------------------------------------------------------------
-- Collapsed from 000039_outbox_validate_cascade_aggregates.sql
-- -----------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM verification_items
      WHERE tenant_id = NEW.tenant_id
        AND item_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

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

  IF NEW.aggregate_type = 'absence' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM workforce_absences
      WHERE tenant_id = NEW.tenant_id
        AND absence_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'absence outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'park' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM locations
      WHERE tenant_id = NEW.tenant_id
        AND location_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'park outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
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


-- -----------------------------------------------------------------------------
-- Collapsed from 000040_vaccination_drive_assignment_members.sql
-- -----------------------------------------------------------------------------
-- seed-fixture-guard:ignore: operational membership written only by the scheduler
-- (visit_shot_lock upsert / drive-date replan) and cascade-deleted with its parent
-- assignment or obligation. It is never authored as seed data and never rebuilt by
-- seed closeout -- a fresh seed populates it through the normal sweeper producer
-- path -- so it carries no seed-data contract and needs no fixture/manifest/runbook
-- companions.
-- Exact drive membership: which obligations (one goat x one rule) a drive
-- assignment row actually covers. Without this, a split shed/partition row
-- ("200 on Jul 24, 124 on Jul 25") only stores counts, so a death/sale/cull
-- cannot be attributed to the exact assignment row. Operational membership
-- written by the scheduler; NOT a seeded catalog and NOT a rebuildable
-- projection.
CREATE TABLE IF NOT EXISTS public.vaccination_drive_assignment_members (
  tenant_id uuid NOT NULL,
  assignment_id uuid NOT NULL,
  obligation_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_drive_assignment_members_pkey PRIMARY KEY (assignment_id, obligation_id),
  CONSTRAINT vaccination_drive_assignment_members_tenant_obligation_uq UNIQUE (tenant_id, obligation_id),
  CONSTRAINT vaccination_drive_assignment_members_assignment_fk FOREIGN KEY (assignment_id)
    REFERENCES public.vaccination_drive_assignments(assignment_id) ON DELETE CASCADE,
  CONSTRAINT vaccination_drive_assignment_members_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id)
    REFERENCES public.obligation_instances(tenant_id, obligation_id) ON DELETE CASCADE,
  CONSTRAINT vaccination_drive_assignment_members_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES public.goats(tenant_id, goat_id)
);

CREATE INDEX IF NOT EXISTS vaccination_drive_assignment_members_tenant_goat_idx
  ON public.vaccination_drive_assignment_members (tenant_id, goat_id);

CREATE INDEX IF NOT EXISTS vaccination_drive_assignment_members_tenant_assignment_idx
  ON public.vaccination_drive_assignment_members (tenant_id, assignment_id);


-- -----------------------------------------------------------------------------
-- Collapsed from 000041_vaccination_prearrival_history_entries.sql
-- -----------------------------------------------------------------------------
SET lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS public.vaccination_prearrival_history_entries (
    entry_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    goat_id uuid NOT NULL,
    source_system text NOT NULL DEFAULT 'procurement_pc_handoff',
    source_event_id text NOT NULL,
    protocol_version_id uuid,
    rule_id uuid,
    vaccine_code text NOT NULL,
    dose_code text NOT NULL,
    sequence integer NOT NULL DEFAULT 0,
    administered_at timestamptz NOT NULL,
    schedule_path text NOT NULL,
    review_status text NOT NULL,
    rejection_reason text,
    reviewed_by uuid,
    reviewed_at timestamptz NOT NULL,
    claim jsonb NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT vaccination_prearrival_history_status_check
        CHECK (review_status IN ('accepted', 'rejected')),
    CONSTRAINT vaccination_prearrival_history_path_check
        CHECK (schedule_path IN ('kid', 'adult_procurement')),
    -- A rejected entry MUST carry a reason: a durable sink with no reason is the
    -- silent-drop defect one column over.
    CONSTRAINT vaccination_prearrival_history_rejected_reason_check
        CHECK (review_status <> 'rejected' OR nullif(btrim(rejection_reason), '') IS NOT NULL),
    -- An accepted entry MUST resolve to a published protocol rule; unresolved
    -- history can never be matched to a dose and would silently suppress nothing
    -- or, worse, everything.
    CONSTRAINT vaccination_prearrival_history_accepted_rule_check
        CHECK (review_status <> 'accepted'
               OR (protocol_version_id IS NOT NULL
                   AND rule_id IS NOT NULL
                   AND nullif(btrim(vaccine_code), '') IS NOT NULL
                   AND nullif(btrim(dose_code), '') IS NOT NULL)),
    CONSTRAINT vaccination_prearrival_history_claim_object_check
        CHECK (jsonb_typeof(claim) = 'object')
);

-- Write-path idempotency: a stable per-claim key plus the semantic payload
-- fingerprint. Exact replay conflicts on this index and returns the original row;
-- a same-key/different-payload replay is rejected by the writer after comparing
-- request_fingerprint.
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_prearrival_history_idempotency_idx
    ON public.vaccination_prearrival_history_entries (tenant_id, idempotency_key);

-- Generation read path: accepted anchors for a page of goats, newest first.
CREATE INDEX IF NOT EXISTS vaccination_prearrival_history_accepted_goat_idx
    ON public.vaccination_prearrival_history_entries (tenant_id, goat_id, administered_at DESC)
    WHERE review_status = 'accepted';

-- Operator surfacing of the rejected sink (never silently dropped).
CREATE INDEX IF NOT EXISTS vaccination_prearrival_history_rejected_idx
    ON public.vaccination_prearrival_history_entries (tenant_id, reviewed_at DESC)
    WHERE review_status = 'rejected';


-- -----------------------------------------------------------------------------
-- Collapsed from 000042_idempotency_result_snapshot.sql
-- -----------------------------------------------------------------------------
-- BUG-037: an exact idempotent replay must return the ORIGINAL result, not a fresh read of the
-- record's CURRENT state. idempotency_keys only stored result_type/result_id, so every replay path
-- re-read the row by id -- which, for a MUTABLE record (workforce_positions bumps row_version on
-- every edit), hands the caller whatever the row looks like now. A replay of update A that happened
-- before unrelated updates B and C returned B/C's state under A's key.
--
-- result_snapshot stores the response body produced by the FIRST call, written inside the same
-- transaction as the side effects, so a replay can return it verbatim with no re-read and no
-- re-execution. Nullable jsonb with no default: PostgreSQL records this as a catalog-only change
-- (no table rewrite, no full-table lock held while writing), so it is safe on a hot shared table.
-- Rows written before this migration keep result_snapshot NULL and fall back to the previous
-- read-by-result_id behaviour.
SET lock_timeout = '5s';

ALTER TABLE public.idempotency_keys
  ADD COLUMN IF NOT EXISTS result_snapshot jsonb;

COMMENT ON COLUMN public.idempotency_keys.result_snapshot IS
  'Response body produced by the first call for this key, persisted in the same transaction as the side effects so an exact replay returns the original result instead of the record''s current state (BUG-037).';


-- -----------------------------------------------------------------------------
-- Collapsed from 000043_ceo_ai_prearrival_history_views.sql
-- -----------------------------------------------------------------------------
-- +goose StatementBegin
-- ===========================================================================
-- ceo_ai PRE-ARRIVAL VACCINATION HISTORY reporting — supplier-claim trust
-- quality as a leadership read surface.
--
-- WHY THIS EXISTS
-- Migration 000041 added public.vaccination_prearrival_history_entries: the
-- reviewed pre-arrival vaccination history channel for PROCURED animals. Every
-- supplier-attested claim that arrives on the procurement PC handoff is
-- machine-validated against the published protocol and persisted either as
-- review_status='accepted' (it becomes real history and suppresses a re-dose)
-- or review_status='rejected' with a mandatory rejection_reason.
--
-- That accepted/rejected split is a genuine LEADERSHIP signal, not internal
-- plumbing: it answers "how many procured animals arrived with vaccination
-- history we could trust", "what share of supplier vaccination claims did we
-- reject", and "why are we rejecting them" — i.e. supplier data quality and
-- avoided re-injection. It is therefore covered here rather than excluded.
--
-- SOURCE: public.vaccination_prearrival_history_entries only (plus no joins —
-- the table already carries every reporting dimension). Human vaccine labels
-- are NOT available on this table; vaccine_code is the raw protocol code, so it
-- is deliberately NOT exposed as a business label column here (the assistant
-- must not print raw config tokens). See the typed placeholder below.
--
-- GRAIN: one row per
--   (tenant_id, reviewed_date_ist, source_system, schedule_path,
--    review_status, rejection_reason).
-- rejection_reason is NULL for accepted rows (accepted rows carry no reason by
-- CHECK constraint), so the accepted and rejected buckets never collapse.
--
-- SCALE: bounded by procured animals × claims per animal (a procurement-volume
-- table, not a per-animal-per-day table), read tenant-scoped with a LIMIT at
-- the toolbox/SQL call site, and the tenant predicate pushes into the base scan
-- because tenant_id is the leading GROUP BY key.
--
-- IST business calendar: reviewed_date_ist buckets reviewed_at on the
-- Asia/Kolkata business day, never a UTC instant.
-- ===========================================================================

-- projection-review: membership=all vaccination_prearrival_history_entries rows for the tenant (accepted + rejected; nothing is filtered out, so the rejected sink stays visible); group_key=(tenant_id, reviewed_date_ist, source_system, schedule_path, review_status, rejection_reason) — every count below groups on exactly that key; join_cardinality=NO joins, single base table, so no fan-out is possible; pagination=procurement-volume table, bounded, read tenant-scoped with LIMIT at the Toolbox/SQL-fallback call site; scope=tenant + review status + schedule path + reason preserved on every row so "accepted vs rejected share" and "why were claims rejected" resolve without collapsing buckets. distinct_animals uses count(DISTINCT goat_id) inside the same group, so an animal with several claims in one bucket is counted once for the animal metric while claims stays per-claim.
CREATE OR REPLACE VIEW ceo_ai.vaccination_prearrival_history_review AS
SELECT
    e.tenant_id                                                   AS tenant_id,
    (e.reviewed_at AT TIME ZONE 'Asia/Kolkata')::date             AS reviewed_date_ist,
    e.source_system                                               AS source_system,
    e.schedule_path                                               AS schedule_path,
    e.review_status                                               AS review_status,
    e.rejection_reason                                            AS rejection_reason,
    count(*)::bigint                                              AS claims,
    count(DISTINCT e.goat_id)::bigint                             AS distinct_animals,
    min((e.administered_at AT TIME ZONE 'Asia/Kolkata')::date)    AS earliest_administered_date,
    max((e.administered_at AT TIME ZONE 'Asia/Kolkata')::date)    AS latest_administered_date,
    NULL::text -- TODO(no source yet): vaccine_label — this table stores the raw
               -- protocol vaccine_code only; the human label lives on the
               -- published protocol rule and is not joinable at this grain
               -- without fanning the review buckets out per vaccine. Leadership
               -- answers therefore stay at the trust/quality grain, and raw
               -- config tokens are never surfaced.
                                                                  AS vaccine_label
FROM public.vaccination_prearrival_history_entries e
GROUP BY 1, 2, 3, 4, 5, 6;
-- +goose StatementEnd

-- ===========================================================================
-- GUARDED GRANTS — idempotent SELECT grant for the read-only reporting roles
-- if they exist (mirrors migrations 000023 / 000027).
-- ===========================================================================
-- +goose StatementBegin
DO $grants$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['mesha_ceo_readonly','mesha_cube_readonly'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('GRANT SELECT ON ceo_ai.vaccination_prearrival_history_review TO %I', r);
        END IF;
    END LOOP;
END;
$grants$;
-- +goose StatementEnd


-- -----------------------------------------------------------------------------
-- Collapsed from 000044_vaccination_operator_capacity_overrides.sql
-- -----------------------------------------------------------------------------
-- Date-scoped vaccination operator capacity exceptions. The normal cap remains
-- on workforce_positions/vaccination_capacity_config; this table is for explicit
-- operational exceptions such as the one-time CPT seed catch-up day.
-- seed-fixture-guard:ignore: operational date-scoped scheduler override table;
-- rows are created only from an explicit operator-roster contract block, not
-- authored as raw HRMS/vaccination source rows in the full fixture.

CREATE TABLE IF NOT EXISTS public.vaccination_operator_capacity_overrides (
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  operator_id uuid NOT NULL,
  capacity_date date NOT NULL,
  max_animals integer NOT NULL,
  reason text NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_operator_capacity_overrides_pkey
    PRIMARY KEY (tenant_id, park_id, operator_id, capacity_date),
  CONSTRAINT vaccination_operator_capacity_overrides_max_check CHECK (max_animals >= 1),
  CONSTRAINT vaccination_operator_capacity_overrides_reason_not_blank CHECK (btrim(reason) <> ''),
  CONSTRAINT vaccination_operator_capacity_overrides_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT vaccination_operator_capacity_overrides_operator_fk
    FOREIGN KEY (operator_id) REFERENCES public.workforce_members (workforce_member_id)
);

CREATE INDEX IF NOT EXISTS vaccination_operator_capacity_overrides_lookup_idx
  ON public.vaccination_operator_capacity_overrides (tenant_id, park_id, capacity_date, operator_id);

COMMENT ON TABLE public.vaccination_operator_capacity_overrides IS
  'Explicit date-scoped vaccination operator animal-cap exceptions. Normal caps stay on HRMS positions; planner reads this table only for the matching operator/date.';


-- -----------------------------------------------------------------------------
-- Collapsed from 000045_vaccination_capacity_config_animal_shot_cap.sql
-- -----------------------------------------------------------------------------
-- Editable animal shot-cap override on the tenant's vaccination_capacity_config row. NULL means "no
-- override, use the published rule_dsl drive_policy.max_shots_per_animal_per_drive / code default"
-- (domain.DefaultMaxShotsPerAnimalPerDrive). A non-null value is a tenant-wide admin override enforced
-- by the obligation sweeper's same-day shot cap and honored ahead of the DSL value. Lock-safe: a bare
-- ADD COLUMN with a NULL default takes only a brief metadata lock, no table rewrite/scan.
--
-- No seed companion: the column defaults NULL and the planner falls back to the published rule_dsl /
-- code default, so existing vaccination-config / HRMS seed correctly leaves it unset.
-- seed-fixture-guard:ignore: nullable shot-cap override column, default NULL falls back to rule_dsl/default; HRMS/config seed fixtures carry no value for it and need no companion update
-- seed-migration-guard:ignore owner=ravi issue=caps-editable reason=nullable-shot-cap-override-defaults-null-planner-falls-back-to-rule_dsl-so-seed-leaves-it-unset expiry=2026-10-31
ALTER TABLE public.vaccination_capacity_config
  ADD COLUMN IF NOT EXISTS max_shots_per_animal_per_drive integer
  CONSTRAINT vaccination_capacity_config_max_shots_check
    CHECK (max_shots_per_animal_per_drive IS NULL OR max_shots_per_animal_per_drive >= 1);

COMMENT ON COLUMN public.vaccination_capacity_config.max_shots_per_animal_per_drive IS
  'Admin-editable override of the same-day per-animal shot cap. NULL = fall back to the published rule_dsl drive_policy value / code default (domain.DefaultMaxShotsPerAnimalPerDrive).';


-- -----------------------------------------------------------------------------
-- Collapsed from 000046_proof_artifact_retention.sql
-- -----------------------------------------------------------------------------
-- +goose NO TRANSACTION
-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: adds runtime proof_artifacts retention bookkeeping; seed SOP proof_policy already carries retention_policy and no fixture/source shape change is needed expiry=2026-10-31
ALTER TABLE proof_artifacts
  ADD COLUMN IF NOT EXISTS retention_policy text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS retention_expires_at timestamptz,
  ADD COLUMN IF NOT EXISTS upload_expires_at timestamptz;

-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: constrains the runtime retention_policy values added above; no seed fixture/source shape change is needed expiry=2026-10-31
SET lock_timeout = '5s';
ALTER TABLE proof_artifacts
  ADD CONSTRAINT proof_artifacts_retention_policy_check
  CHECK (retention_policy = ANY (ARRAY[''::text, 'operational_90d'::text, 'standard_1y'::text, 'critical_7y'::text, 'legal_hold'::text])) NOT VALID;

-- seed-migration-guard:ignore owner=ravi issue=proof-retention-policy reason=no-seed-impact: validates runtime retention_policy values only; no seed fixture/source shape change is needed expiry=2026-10-31
SET lock_timeout = '5s';
ALTER TABLE proof_artifacts
  VALIDATE CONSTRAINT proof_artifacts_retention_policy_check;

CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_retention_expiry_idx
  ON proof_artifacts (retention_expires_at, tenant_id, proof_id)
  WHERE retention_expires_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS proof_artifacts_abandoned_upload_idx
  ON proof_artifacts (upload_expires_at, tenant_id, proof_id)
  WHERE upload_state IN ('pending', 'uploading')
    AND upload_expires_at IS NOT NULL;


-- -----------------------------------------------------------------------------
-- Collapsed from 000046_validate_vaccination_batch_outbox.sql
-- -----------------------------------------------------------------------------
-- Vaccination drive ready/closed events are emitted with aggregate_type='vaccination_batch'.
-- The outbox validator must validate them against obligation_batches instead of falling through
-- to the legacy goat_identity_events fallback, which rejects the deterministic drive event_id.
-- seed-fixture-guard:ignore: trigger-only validation fix; no seed data shape changes
-- seed-migration-guard:ignore owner=ravi issue=verification-drive-events reason=trigger-only-outbox-aggregate-validation-fix-no-seed-impact expiry=2026-10-31
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.validate_outbox_event_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  config_family_key text;
BEGIN
  IF NEW.aggregate_type = 'verification_item' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM verification_items
      WHERE tenant_id = NEW.tenant_id
        AND item_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'verification item outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'vaccination_batch' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM obligation_batches
      WHERE tenant_id = NEW.tenant_id
        AND batch_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'vaccination batch outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

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

  IF NEW.aggregate_type = 'absence' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM workforce_absences
      WHERE tenant_id = NEW.tenant_id
        AND absence_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'absence outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
        USING ERRCODE = '23503';
    END IF;

    RETURN NEW;
  END IF;

  IF NEW.aggregate_type = 'park' THEN
    IF NOT EXISTS (
      SELECT 1
      FROM locations
      WHERE tenant_id = NEW.tenant_id
        AND location_id = NEW.aggregate_id
    ) THEN
      RAISE EXCEPTION 'park outbox aggregate % does not exist for tenant %', NEW.aggregate_id, NEW.tenant_id
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
-- +goose NO TRANSACTION

DROP SCHEMA IF EXISTS ceo_ai CASCADE;
DROP SCHEMA IF EXISTS analytics CASCADE;
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO postgres;
GRANT ALL ON SCHEMA public TO public;
