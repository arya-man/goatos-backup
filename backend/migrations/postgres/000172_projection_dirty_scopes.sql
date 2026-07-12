-- +goose Up
-- Module-neutral transactional projection invalidation ledger. Each family coalesces its source
-- writes by tenant + logical scope + IST business date and shares one lease/retry/DLQ/checkpoint contract.
CREATE TABLE projection_dirty_scopes (
  dirty_scope_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  family text NOT NULL CHECK (family ~ '^[a-z][a-z0-9_]*$'),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  scope_type text NOT NULL CHECK (scope_type IN ('tenant','park','shed')),
  scope_id uuid NOT NULL,
  park_id uuid,
  shed_id uuid,
  business_date date NOT NULL,
  dirty_from timestamptz NOT NULL,
  dirty_through timestamptz NOT NULL,
  reason_codes text[] NOT NULL DEFAULT '{}'::text[],
  status text NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','processing','retry','completed','dead_letter')),
  attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
  max_attempts integer NOT NULL DEFAULT 8 CHECK (max_attempts > 0),
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_owner text,
  lease_token uuid,
  lease_until timestamptz,
  checkpoint jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (family, tenant_id, scope_type, scope_id, business_date),
  CHECK ((status = 'processing') = (lease_token IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX projection_dirty_scopes_claim_idx
  ON projection_dirty_scopes (family, status, available_at, dirty_scope_id)
  WHERE status IN ('pending','retry');
CREATE INDEX projection_dirty_scopes_expired_lease_idx
  ON projection_dirty_scopes (family, lease_until, dirty_scope_id)
  WHERE status = 'processing';
CREATE INDEX projection_dirty_scopes_scope_idx
  ON projection_dirty_scopes (family, tenant_id, scope_type, scope_id, business_date, dirty_scope_id);

CREATE OR REPLACE FUNCTION projection_enqueue_dirty_scope(
  p_family text, p_tenant_id uuid, p_scope_type text, p_scope_id uuid,
  p_park_id uuid, p_shed_id uuid, p_reason text,
  p_changed_at timestamptz DEFAULT clock_timestamp()
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE v_business_date date;
BEGIN
  IF p_family IS NULL OR p_family !~ '^[a-z][a-z0-9_]*$'
     OR p_tenant_id IS NULL OR p_scope_type NOT IN ('tenant','park','shed') OR p_scope_id IS NULL THEN
    RAISE EXCEPTION 'invalid projection dirty scope family=% tenant=% type=% id=%',
      p_family,p_tenant_id,p_scope_type,p_scope_id USING ERRCODE='22023';
  END IF;
  v_business_date := (COALESCE(p_changed_at,clock_timestamp()) AT TIME ZONE 'Asia/Kolkata')::date;
  INSERT INTO projection_dirty_scopes (
    family,tenant_id,scope_type,scope_id,park_id,shed_id,business_date,
    dirty_from,dirty_through,reason_codes
  ) VALUES (
    p_family,p_tenant_id,p_scope_type,p_scope_id,p_park_id,p_shed_id,v_business_date,
    COALESCE(p_changed_at,clock_timestamp()),COALESCE(p_changed_at,clock_timestamp()),
    ARRAY[COALESCE(NULLIF(btrim(p_reason),''),'source_change')]
  )
  ON CONFLICT (family,tenant_id,scope_type,scope_id,business_date) DO UPDATE SET
    park_id=COALESCE(EXCLUDED.park_id,projection_dirty_scopes.park_id),
    shed_id=COALESCE(EXCLUDED.shed_id,projection_dirty_scopes.shed_id),
    dirty_from=LEAST(projection_dirty_scopes.dirty_from,EXCLUDED.dirty_from),
    dirty_through=GREATEST(projection_dirty_scopes.dirty_through,EXCLUDED.dirty_through),
    reason_codes=ARRAY(
      SELECT DISTINCT reason FROM unnest(projection_dirty_scopes.reason_codes||EXCLUDED.reason_codes) reason ORDER BY reason
    ),
    status='pending',attempt_count=0,available_at=now(),lease_owner=NULL,lease_token=NULL,
    lease_until=NULL,last_error=NULL,updated_at=now();
END;
$$;

CREATE OR REPLACE FUNCTION vaccination_projection_enqueue_shed(
  p_tenant_id uuid,
  p_shed_id uuid,
  p_reason text,
  p_changed_at timestamptz DEFAULT clock_timestamp()
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  v_park_id uuid;
BEGIN
  IF p_tenant_id IS NULL OR p_shed_id IS NULL THEN
    RETURN;
  END IF;
  SELECT parent_location_id INTO v_park_id
  FROM locations
  WHERE tenant_id = p_tenant_id AND location_id = p_shed_id AND location_type = 'shed';
  PERFORM projection_enqueue_dirty_scope('vaccination',p_tenant_id,'shed',p_shed_id,
    v_park_id,p_shed_id,p_reason,p_changed_at);
END;
$$;

CREATE OR REPLACE FUNCTION vaccination_projection_enqueue_scope(
  p_tenant_id uuid,
  p_target_type text,
  p_target_id uuid,
  p_scope_type text,
  p_scope_id uuid,
  p_reason text,
  p_changed_at timestamptz DEFAULT clock_timestamp()
) RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
  v_shed_id uuid;
  v_park_id uuid;
BEGIN
  IF p_tenant_id IS NULL THEN RETURN; END IF;
  IF p_target_type = 'goat' AND p_target_id IS NOT NULL THEN
    SELECT shed_id INTO v_shed_id FROM goats
    WHERE tenant_id = p_tenant_id AND goat_id = p_target_id;
    PERFORM vaccination_projection_enqueue_shed(p_tenant_id, v_shed_id, p_reason, p_changed_at);
    RETURN;
  END IF;
  IF p_target_type = 'shed' AND p_target_id IS NOT NULL THEN
    PERFORM vaccination_projection_enqueue_shed(p_tenant_id, p_target_id, p_reason, p_changed_at);
    RETURN;
  END IF;
  IF p_scope_type = 'shed' AND p_scope_id IS NOT NULL THEN
    PERFORM vaccination_projection_enqueue_shed(p_tenant_id, p_scope_id, p_reason, p_changed_at);
    RETURN;
  END IF;
  v_park_id := CASE
    WHEN p_target_type = 'park' THEN p_target_id
    WHEN p_scope_type = 'park' THEN p_scope_id
    ELSE NULL
  END;
  IF v_park_id IS NOT NULL THEN
    PERFORM vaccination_projection_enqueue_shed(p_tenant_id, location_id, p_reason, p_changed_at)
    FROM locations
    WHERE tenant_id = p_tenant_id AND parent_location_id = v_park_id AND location_type = 'shed';
    RETURN;
  END IF;
  PERFORM vaccination_projection_enqueue_shed(p_tenant_id, location_id, p_reason, p_changed_at)
  FROM locations
  WHERE tenant_id = p_tenant_id AND location_type = 'shed';
END;
$$;

-- Existing tenants need one initial bounded queue window. The worker recognizes missing serving
-- pointers and runs the one-time full bootstrap before switching permanently to shard replacement.
INSERT INTO projection_dirty_scopes (
  family,tenant_id,scope_type,scope_id,park_id,shed_id,business_date,dirty_from,dirty_through,reason_codes
)
SELECT 'vaccination',tenant_id,'shed',location_id,parent_location_id,location_id,
       (clock_timestamp() AT TIME ZONE 'Asia/Kolkata')::date,
       clock_timestamp(), clock_timestamp(), ARRAY['projection_bootstrap']
FROM locations
WHERE location_type='shed' AND status='active'
ON CONFLICT (family,tenant_id,scope_type,scope_id,business_date) DO NOTHING;

CREATE OR REPLACE FUNCTION vaccination_projection_goat_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    PERFORM vaccination_projection_enqueue_shed(OLD.tenant_id, OLD.shed_id, 'goat', OLD.updated_at);
  END IF;
  IF TG_OP <> 'DELETE' THEN
    PERFORM vaccination_projection_enqueue_shed(NEW.tenant_id, NEW.shed_id, 'goat', NEW.updated_at);
  END IF;
  RETURN COALESCE(NEW, OLD);
END;
$$;
CREATE TRIGGER vaccination_projection_goat_dirty
AFTER INSERT OR UPDATE OF tenant_id, shed_id, lifecycle_status, merged_into_goat_id, management_stage,
  age_band, approx_dob, health_status OR DELETE ON goats
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_goat_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_obligation_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP <> 'INSERT' THEN
    PERFORM vaccination_projection_enqueue_scope(OLD.tenant_id, OLD.target_type, OLD.target_id,
      OLD.scope_type, OLD.scope_id, 'obligation', OLD.updated_at);
  END IF;
  IF TG_OP <> 'DELETE' THEN
    PERFORM vaccination_projection_enqueue_scope(NEW.tenant_id, NEW.target_type, NEW.target_id,
      NEW.scope_type, NEW.scope_id, 'obligation', NEW.updated_at);
  END IF;
  RETURN COALESCE(NEW, OLD);
END;
$$;
CREATE TRIGGER vaccination_projection_obligation_dirty
AFTER INSERT OR UPDATE OR DELETE ON obligation_instances
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_obligation_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_completion_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE r vaccination_completions%ROWTYPE;
BEGIN
  r := COALESCE(NEW, OLD);
  PERFORM vaccination_projection_enqueue_scope(r.tenant_id, 'goat', r.goat_id, NULL, NULL,
    'vaccination_completion', r.updated_at);
  RETURN r;
END;
$$;
CREATE TRIGGER vaccination_projection_completion_dirty
AFTER INSERT OR UPDATE OR DELETE ON vaccination_completions
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_completion_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_status_event_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE r obligation_status_events%ROWTYPE; oi obligation_instances%ROWTYPE;
BEGIN
  r := COALESCE(NEW, OLD);
  SELECT * INTO oi FROM obligation_instances
  WHERE tenant_id = r.tenant_id AND obligation_id = r.obligation_id;
  PERFORM vaccination_projection_enqueue_scope(oi.tenant_id, oi.target_type, oi.target_id,
    oi.scope_type, oi.scope_id, 'obligation_status_event', r.recorded_at);
  RETURN r;
END;
$$;
CREATE TRIGGER vaccination_projection_status_event_dirty
AFTER INSERT OR UPDATE OR DELETE ON obligation_status_events
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_status_event_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_batch_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE r obligation_batches%ROWTYPE;
BEGIN
  r := COALESCE(NEW, OLD);
  PERFORM vaccination_projection_enqueue_scope(r.tenant_id, NULL, NULL, r.scope_type, r.scope_id,
    'obligation_batch', r.updated_at);
  RETURN r;
END;
$$;
CREATE TRIGGER vaccination_projection_batch_dirty
AFTER INSERT OR UPDATE OR DELETE ON obligation_batches
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_batch_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_task_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE r sop_tasks%ROWTYPE;
BEGIN
  r := COALESCE(NEW, OLD);
  PERFORM vaccination_projection_enqueue_scope(r.tenant_id, NULL, NULL, r.scope_type, r.scope_id,
    'sop_task', r.updated_at);
  RETURN r;
END;
$$;
CREATE TRIGGER vaccination_projection_task_dirty
AFTER INSERT OR UPDATE OR DELETE ON sop_tasks
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_task_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_location_scope_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_tenant uuid; v_location uuid; v_type text; v_changed timestamptz;
BEGIN
  v_tenant := COALESCE(NEW.tenant_id, OLD.tenant_id);
  v_location := COALESCE(NEW.location_id, OLD.location_id);
  v_type := COALESCE(NEW.location_type, OLD.location_type);
  v_changed := clock_timestamp();
  PERFORM vaccination_projection_enqueue_scope(v_tenant, v_type, v_location, v_type, v_location,
    'location', v_changed);
  RETURN COALESCE(NEW, OLD);
END;
$$;
CREATE TRIGGER vaccination_projection_location_dirty
AFTER INSERT OR UPDATE OF name, parent_location_id, status OR DELETE ON locations
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_location_scope_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_location_attributes_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE r location_operational_attributes%ROWTYPE; v_type text;
BEGIN
  r := COALESCE(NEW, OLD);
  SELECT location_type INTO v_type FROM locations
  WHERE tenant_id = r.tenant_id AND location_id = r.location_id;
  PERFORM vaccination_projection_enqueue_scope(r.tenant_id, v_type, r.location_id, v_type, r.location_id,
    'location_operational_attributes', r.updated_at);
  RETURN r;
END;
$$;
CREATE TRIGGER vaccination_projection_location_attributes_dirty
AFTER INSERT OR UPDATE OR DELETE ON location_operational_attributes
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_location_attributes_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_workforce_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE r workforce_members%ROWTYPE; v_type text;
BEGIN
  r := COALESCE(NEW, OLD);
  IF r.primary_location_id IS NULL THEN
    PERFORM vaccination_projection_enqueue_scope(r.tenant_id, NULL, NULL, 'tenant', r.tenant_id,
      'workforce', r.updated_at);
  ELSE
    SELECT location_type INTO v_type FROM locations
    WHERE tenant_id = r.tenant_id AND location_id = r.primary_location_id;
    PERFORM vaccination_projection_enqueue_scope(r.tenant_id, v_type, r.primary_location_id,
      v_type, r.primary_location_id, 'workforce', r.updated_at);
  END IF;
  RETURN r;
END;
$$;
CREATE TRIGGER vaccination_projection_workforce_dirty
AFTER INSERT OR UPDATE OR DELETE ON workforce_members
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_workforce_dirty_trg();

CREATE OR REPLACE FUNCTION vaccination_projection_tenant_dirty_trg() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_tenant uuid;
BEGIN
  v_tenant := COALESCE(NEW.tenant_id, OLD.tenant_id);
  PERFORM vaccination_projection_enqueue_scope(v_tenant, NULL, NULL, 'tenant', v_tenant,
    TG_TABLE_NAME, clock_timestamp());
  RETURN COALESCE(NEW, OLD);
END;
$$;
CREATE TRIGGER vaccination_projection_capacity_dirty
AFTER INSERT OR UPDATE OR DELETE ON vaccination_capacity_config
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_tenant_dirty_trg();
CREATE TRIGGER vaccination_projection_protocol_definition_dirty
AFTER INSERT OR UPDATE OR DELETE ON protocol_definitions
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_tenant_dirty_trg();
CREATE TRIGGER vaccination_projection_protocol_version_dirty
AFTER INSERT OR UPDATE OR DELETE ON protocol_versions
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_tenant_dirty_trg();
CREATE TRIGGER vaccination_projection_protocol_rule_dirty
AFTER INSERT OR UPDATE OR DELETE ON protocol_rules
FOR EACH ROW EXECUTE FUNCTION vaccination_projection_tenant_dirty_trg();

-- +goose Down
DROP TRIGGER IF EXISTS vaccination_projection_protocol_rule_dirty ON protocol_rules;
DROP TRIGGER IF EXISTS vaccination_projection_protocol_version_dirty ON protocol_versions;
DROP TRIGGER IF EXISTS vaccination_projection_protocol_definition_dirty ON protocol_definitions;
DROP TRIGGER IF EXISTS vaccination_projection_capacity_dirty ON vaccination_capacity_config;
DROP TRIGGER IF EXISTS vaccination_projection_workforce_dirty ON workforce_members;
DROP TRIGGER IF EXISTS vaccination_projection_location_attributes_dirty ON location_operational_attributes;
DROP TRIGGER IF EXISTS vaccination_projection_location_dirty ON locations;
DROP TRIGGER IF EXISTS vaccination_projection_task_dirty ON sop_tasks;
DROP TRIGGER IF EXISTS vaccination_projection_batch_dirty ON obligation_batches;
DROP TRIGGER IF EXISTS vaccination_projection_status_event_dirty ON obligation_status_events;
DROP TRIGGER IF EXISTS vaccination_projection_completion_dirty ON vaccination_completions;
DROP TRIGGER IF EXISTS vaccination_projection_obligation_dirty ON obligation_instances;
DROP TRIGGER IF EXISTS vaccination_projection_goat_dirty ON goats;
DROP FUNCTION IF EXISTS vaccination_projection_tenant_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_workforce_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_location_attributes_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_location_scope_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_task_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_batch_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_status_event_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_completion_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_obligation_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_goat_dirty_trg();
DROP FUNCTION IF EXISTS vaccination_projection_enqueue_scope(uuid,text,uuid,text,uuid,text,timestamptz);
DROP FUNCTION IF EXISTS vaccination_projection_enqueue_shed(uuid,uuid,text,timestamptz);
DROP FUNCTION IF EXISTS projection_enqueue_dirty_scope(text,uuid,text,uuid,uuid,uuid,text,timestamptz);
DROP TABLE IF EXISTS projection_dirty_scopes;
