-- +goose Up
-- Phase 0 · Obligation (due-state) layer — SOURCE OF TRUTH for due work. Schema only;
-- no obligations generated (SM-1..SM-7 handlers are Phase 1). obligation_instances holds
-- far-future due rows and is queryable; obligation_status_events is the append-only,
-- RANGE(recorded_at)-partitioned ledger (mirrors goat_identity_events). All cross-table refs
-- use tenant-safe composite (tenant_id, *) FKs (000002 pattern), including sop_task_id
-- (nullable, engine-set) — this migration adds UNIQUE(tenant_id, task_id) to sop_tasks.

-- Tenant-safe composite key on the SOP module's task table (task_id alone is the PK; this
-- adds the (tenant_id, task_id) pair) so obligation refs can use composite FKs.
ALTER TABLE sop_tasks ADD CONSTRAINT sop_tasks_tenant_id_unique UNIQUE (tenant_id, task_id);

-- Shared scope validator: tenant -> scope_id = tenant_id; custodian_party -> parties;
-- farm/park/shed/cohort -> location of the matching location_type. Used by both
-- obligation_instances and obligation_batches.
CREATE OR REPLACE FUNCTION validate_obligation_scope()
RETURNS trigger
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

CREATE TABLE obligation_batches (
  batch_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  protocol_version_id uuid NOT NULL,
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  session text NULL,
  planned_date date NULL,
  window_start timestamptz NULL,
  window_end timestamptz NULL,
  status text NOT NULL DEFAULT 'planned',
  estimated_targets int NOT NULL DEFAULT 0,
  planned_quantity numeric NULL,
  reserved_quantity numeric NOT NULL DEFAULT 0,
  used_quantity numeric NOT NULL DEFAULT 0,
  quantity_unit text NULL,
  primary_inventory_lot_id uuid NULL,
  sop_task_id uuid NULL,
  conducted_by uuid NULL,
  proof_ref text NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT obligation_batches_scope_type_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT obligation_batches_status_check CHECK (status IN ('planned', 'in_progress', 'completed', 'superseded', 'canceled')),
  CONSTRAINT obligation_batches_reserved_check CHECK (reserved_quantity >= 0),
  CONSTRAINT obligation_batches_used_check CHECK (used_quantity >= 0),
  CONSTRAINT obligation_batches_row_version_check CHECK (row_version >= 1),
  CONSTRAINT obligation_batches_tenant_id_unique UNIQUE (tenant_id, batch_id),
  CONSTRAINT obligation_batches_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES protocol_versions(tenant_id, protocol_version_id),
  CONSTRAINT obligation_batches_lot_tenant_fk FOREIGN KEY (tenant_id, primary_inventory_lot_id) REFERENCES inventory_stock(tenant_id, stock_id),
  CONSTRAINT obligation_batches_sop_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES sop_tasks(tenant_id, task_id)
);

CREATE TRIGGER obligation_batches_validate_scope_trg
  BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON obligation_batches
  FOR EACH ROW EXECUTE FUNCTION validate_obligation_scope();

CREATE INDEX obligation_batches_scope_idx ON obligation_batches(tenant_id, scope_type, scope_id, status);

CREATE TABLE obligation_instances (
  obligation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  protocol_version_id uuid NOT NULL,
  rule_id uuid NOT NULL,
  batch_id uuid NULL,
  target_type text NOT NULL,
  target_id uuid NOT NULL,
  scope_type text NOT NULL,
  scope_id uuid NOT NULL,
  due_at timestamptz NOT NULL,
  window_start timestamptz NULL,
  window_end timestamptz NULL,
  status text NOT NULL DEFAULT 'scheduled',
  sop_task_id uuid NULL,
  idempotency_key text NOT NULL,
  generated_by_trigger_id uuid NULL,
  sequence int NOT NULL DEFAULT 1,
  completed_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT obligation_instances_target_type_check CHECK (target_type IN ('goat', 'cohort', 'shed', 'park', 'tenant')),
  CONSTRAINT obligation_instances_scope_type_check CHECK (scope_type IN ('tenant', 'custodian_party', 'farm', 'park', 'shed', 'cohort')),
  CONSTRAINT obligation_instances_status_check CHECK (status IN ('scheduled', 'due', 'in_progress', 'completed', 'missed', 'waived', 'canceled', 'superseded')),
  CONSTRAINT obligation_instances_row_version_check CHECK (row_version >= 1),
  -- Deterministic idempotency: generation is a no-op on replay.
  CONSTRAINT obligation_instances_idempotency_unique UNIQUE (tenant_id, idempotency_key),
  -- Duplicate-spawn guard (includes rule_id so two vaccines can be due the same day for one goat).
  CONSTRAINT obligation_instances_dup_guard UNIQUE NULLS NOT DISTINCT (tenant_id, protocol_version_id, rule_id, target_type, target_id, due_at),
  CONSTRAINT obligation_instances_tenant_id_unique UNIQUE (tenant_id, obligation_id),
  CONSTRAINT obligation_instances_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES protocol_versions(tenant_id, protocol_version_id),
  CONSTRAINT obligation_instances_rule_tenant_fk FOREIGN KEY (tenant_id, rule_id) REFERENCES protocol_rules(tenant_id, rule_id),
  CONSTRAINT obligation_instances_batch_tenant_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES obligation_batches(tenant_id, batch_id),
  CONSTRAINT obligation_instances_trigger_tenant_fk FOREIGN KEY (tenant_id, generated_by_trigger_id) REFERENCES protocol_triggers(tenant_id, trigger_id),
  CONSTRAINT obligation_instances_sop_task_tenant_fk FOREIGN KEY (tenant_id, sop_task_id) REFERENCES sop_tasks(tenant_id, task_id)
);

CREATE TRIGGER obligation_instances_validate_scope_trg
  BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON obligation_instances
  FOR EACH ROW EXECUTE FUNCTION validate_obligation_scope();

-- Target integrity: goat -> same-tenant goats; park/shed/cohort -> same-tenant location of
-- the matching location_type; tenant -> target_id = tenant_id.
CREATE OR REPLACE FUNCTION validate_obligation_target()
RETURNS trigger
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

CREATE TRIGGER obligation_instances_validate_target_trg
  BEFORE INSERT OR UPDATE OF tenant_id, target_type, target_id ON obligation_instances
  FOR EACH ROW EXECUTE FUNCTION validate_obligation_target();

CREATE INDEX obligation_instances_due_window_idx ON obligation_instances(tenant_id, status, due_at, obligation_id);
CREATE INDEX obligation_instances_target_idx ON obligation_instances(tenant_id, target_type, target_id, status);
CREATE INDEX obligation_instances_scope_idx ON obligation_instances(tenant_id, scope_type, scope_id, status, due_at);
CREATE INDEX obligation_instances_batch_idx ON obligation_instances(tenant_id, batch_id, status);

CREATE TABLE obligation_status_events (
  obligation_event_id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  obligation_id uuid NOT NULL,
  event_type text NOT NULL,
  occurred_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_id uuid NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_key text NOT NULL,
  PRIMARY KEY (obligation_event_id, recorded_at),
  CONSTRAINT obligation_status_events_type_check CHECK (event_type IN ('scheduled', 'became_due', 'dispatched', 'completed', 'missed', 'waived', 'escalated', 'canceled', 'deferred')),
  -- Tenant-safe link to the obligation (obligation_instances exposes UNIQUE(tenant_id, obligation_id)).
  CONSTRAINT obligation_status_events_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES obligation_instances(tenant_id, obligation_id)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE obligation_status_events_2026_06 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-06-01 00:00:00+00') TO ('2026-07-01 00:00:00+00');
CREATE TABLE obligation_status_events_2026_07 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-07-01 00:00:00+00') TO ('2026-08-01 00:00:00+00');
CREATE TABLE obligation_status_events_2026_08 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-08-01 00:00:00+00') TO ('2026-09-01 00:00:00+00');
CREATE TABLE obligation_status_events_2026_09 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-09-01 00:00:00+00') TO ('2026-10-01 00:00:00+00');
CREATE TABLE obligation_status_events_2026_10 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-10-01 00:00:00+00') TO ('2026-11-01 00:00:00+00');
CREATE TABLE obligation_status_events_2026_11 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-11-01 00:00:00+00') TO ('2026-12-01 00:00:00+00');
CREATE TABLE obligation_status_events_2026_12 PARTITION OF obligation_status_events
  FOR VALUES FROM ('2026-12-01 00:00:00+00') TO ('2027-01-01 00:00:00+00');
CREATE TABLE obligation_status_events_default PARTITION OF obligation_status_events DEFAULT;

CREATE INDEX obligation_status_events_obligation_idx ON obligation_status_events(obligation_id, occurred_at DESC);
CREATE INDEX obligation_status_events_tenant_type_recorded_idx ON obligation_status_events(tenant_id, event_type, recorded_at DESC);
-- A global UNIQUE cannot exclude the partition key, so cross-partition idempotency is enforced
-- at the app/consumer layer via the shared idempotency_keys table (mirrors goat_identity_events).
-- This index supports that dedup lookup.
CREATE INDEX obligation_status_events_idempotency_idx ON obligation_status_events(tenant_id, idempotency_key);

CREATE TABLE obligation_escalations (
  escalation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  obligation_id uuid NOT NULL,
  level int NOT NULL,
  escalated_to_user_id uuid NULL,
  escalated_to_role text NULL,
  reason text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open',
  opened_at timestamptz NOT NULL DEFAULT now(),
  acknowledged_at timestamptz NULL,
  resolved_at timestamptz NULL,
  CONSTRAINT obligation_escalations_level_check CHECK (level >= 1),
  CONSTRAINT obligation_escalations_role_check CHECK (escalated_to_role IS NULL OR escalated_to_role IN ('admin', 'park_head', 'operator', 'verifier', 'ceo_internal')),
  CONSTRAINT obligation_escalations_status_check CHECK (status IN ('open', 'acknowledged', 'resolved', 'expired')),
  CONSTRAINT obligation_escalations_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id) REFERENCES obligation_instances(tenant_id, obligation_id)
);

CREATE INDEX obligation_escalations_status_idx ON obligation_escalations(tenant_id, status, level);

-- Now that obligation_batches exists, wire the inventory ledger's batch reference (tenant-safe).
ALTER TABLE inventory_stock_movements
  ADD CONSTRAINT inventory_stock_movements_batch_tenant_fk
    FOREIGN KEY (tenant_id, batch_id) REFERENCES obligation_batches(tenant_id, batch_id);

-- +goose Down
ALTER TABLE inventory_stock_movements DROP CONSTRAINT IF EXISTS inventory_stock_movements_batch_tenant_fk;
DROP INDEX IF EXISTS obligation_escalations_status_idx;
DROP TABLE IF EXISTS obligation_escalations;
DROP INDEX IF EXISTS obligation_status_events_idempotency_idx;
DROP INDEX IF EXISTS obligation_status_events_tenant_type_recorded_idx;
DROP INDEX IF EXISTS obligation_status_events_obligation_idx;
DROP TABLE IF EXISTS obligation_status_events;
DROP TRIGGER IF EXISTS obligation_instances_validate_target_trg ON obligation_instances;
DROP TRIGGER IF EXISTS obligation_instances_validate_scope_trg ON obligation_instances;
DROP INDEX IF EXISTS obligation_instances_batch_idx;
DROP INDEX IF EXISTS obligation_instances_scope_idx;
DROP INDEX IF EXISTS obligation_instances_target_idx;
DROP INDEX IF EXISTS obligation_instances_due_window_idx;
DROP TABLE IF EXISTS obligation_instances;
DROP TRIGGER IF EXISTS obligation_batches_validate_scope_trg ON obligation_batches;
DROP INDEX IF EXISTS obligation_batches_scope_idx;
DROP TABLE IF EXISTS obligation_batches;
DROP FUNCTION IF EXISTS validate_obligation_target();
DROP FUNCTION IF EXISTS validate_obligation_scope();
ALTER TABLE sop_tasks DROP CONSTRAINT IF EXISTS sop_tasks_tenant_id_unique;
