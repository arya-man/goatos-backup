-- +goose Up
-- Phase 0 · Protocol (ruleset) layer. Admin-authored config; NO production rules,
-- NO rule values seeded. Mirrors sop_definitions / sop_versions shape. All cross-table refs
-- use tenant-safe composite (tenant_id, *) FKs (000002 pattern), including sop_version_id
-- (nullable, engine-set) — this migration adds UNIQUE(tenant_id, sop_version_id) to sop_versions.

CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Tenant-safe composite key on the SOP module's version table (sop_version_id alone is the PK;
-- this adds the (tenant_id, sop_version_id) pair) so protocol refs can use composite FKs.
ALTER TABLE sop_versions ADD CONSTRAINT sop_versions_tenant_id_unique UNIQUE (tenant_id, sop_version_id);

CREATE TABLE protocol_definitions (
  protocol_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  code text NOT NULL,
  name text NOT NULL,
  category text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT protocol_definitions_code_format_check CHECK (code ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
  CONSTRAINT protocol_definitions_category_check CHECK (category IN (
    'vaccination', 'deworming', 'biosecurity', 'feed_water_testing', 'panel_cleaning',
    'sanitization', 'fire_safety', 'sop_video', 'stock_check', 'director_reporting', 'feed_direction')),
  CONSTRAINT protocol_definitions_status_check CHECK (status IN ('draft', 'active', 'retired')),
  CONSTRAINT protocol_definitions_row_version_check CHECK (row_version >= 1),
  CONSTRAINT protocol_definitions_code_unique UNIQUE (tenant_id, code),
  CONSTRAINT protocol_definitions_tenant_id_unique UNIQUE (tenant_id, protocol_id)
);

CREATE TABLE protocol_versions (
  protocol_version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  protocol_id uuid NOT NULL,
  scope_type text NOT NULL DEFAULT 'tenant',
  scope_id uuid NULL,
  version int NOT NULL,
  version_label text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'draft',
  effective_from date NOT NULL,
  effective_to date NULL,
  rule_dsl jsonb NOT NULL DEFAULT '{}'::jsonb,
  proof_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
  sop_version_id uuid NULL,
  drafted_by uuid NULL,
  published_by uuid NULL,
  published_at timestamptz NULL,
  retired_at timestamptz NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version int NOT NULL DEFAULT 1,
  CONSTRAINT protocol_versions_scope_type_check CHECK (scope_type IN ('tenant', 'park')),
  CONSTRAINT protocol_versions_status_check CHECK (status IN ('draft', 'published', 'retired')),
  CONSTRAINT protocol_versions_version_check CHECK (version > 0),
  CONSTRAINT protocol_versions_row_version_check CHECK (row_version >= 1),
  CONSTRAINT protocol_versions_scope_shape_check CHECK (
    (scope_type = 'tenant' AND scope_id IS NULL)
    OR (scope_type = 'park' AND scope_id IS NOT NULL)),
  CONSTRAINT protocol_versions_effective_range_check CHECK (effective_to IS NULL OR effective_to > effective_from),
  -- NULLS NOT DISTINCT so the tenant-default (scope_id IS NULL) version number is unique.
  CONSTRAINT protocol_versions_scope_version_unique
    UNIQUE NULLS NOT DISTINCT (tenant_id, protocol_id, scope_type, scope_id, version),
  CONSTRAINT protocol_versions_tenant_id_unique UNIQUE (tenant_id, protocol_version_id),
  CONSTRAINT protocol_versions_protocol_tenant_fk FOREIGN KEY (tenant_id, protocol_id) REFERENCES protocol_definitions(tenant_id, protocol_id),
  CONSTRAINT protocol_versions_sop_version_tenant_fk FOREIGN KEY (tenant_id, sop_version_id) REFERENCES sop_versions(tenant_id, sop_version_id),
  -- Published windows cannot overlap WITHIN the same scope; tenant default and a park
  -- calendar coexist (different scope), and past/present/future published versions coexist.
  CONSTRAINT protocol_versions_published_no_overlap EXCLUDE USING gist (
    tenant_id WITH =,
    protocol_id WITH =,
    scope_type WITH =,
    (COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid)) WITH =,
    daterange(effective_from, effective_to, '[)') WITH &&
  ) WHERE (status = 'published')
);

CREATE INDEX protocol_versions_lookup_idx ON protocol_versions(tenant_id, protocol_id, status, effective_from);

CREATE TABLE protocol_rules (
  rule_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  protocol_version_id uuid NOT NULL,
  dose_code text NOT NULL,
  sequence int NOT NULL DEFAULT 1,
  trigger_type text NOT NULL,
  offset_days int NOT NULL DEFAULT 0,
  due_window_days int NOT NULL DEFAULT 0,
  min_gap_days int NOT NULL DEFAULT 0,
  repeat text NOT NULL DEFAULT 'none',
  repeat_until_after_age text NULL,
  catch_up text NOT NULL DEFAULT 'phc_approval',
  eligibility_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  sop_version_id uuid NULL,
  proof_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
  withdrawal_days int NULL,
  sort_order int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT protocol_rules_trigger_type_check CHECK (trigger_type IN ('birth_age', 'post_arrival', 'calendar', 'after_previous_completion', 'manual_campaign')),
  CONSTRAINT protocol_rules_repeat_check CHECK (repeat IN ('none', 'every_n_days', 'yearly', 'until_age', 'after_age')),
  CONSTRAINT protocol_rules_catch_up_check CHECK (catch_up IN ('immediate', 'next_cycle', 'phc_approval', 'defer')),
  CONSTRAINT protocol_rules_offset_check CHECK (offset_days >= 0),
  CONSTRAINT protocol_rules_window_check CHECK (due_window_days >= 0),
  CONSTRAINT protocol_rules_gap_check CHECK (min_gap_days >= 0),
  CONSTRAINT protocol_rules_version_dose_unique UNIQUE (tenant_id, protocol_version_id, dose_code),
  CONSTRAINT protocol_rules_tenant_id_unique UNIQUE (tenant_id, rule_id),
  CONSTRAINT protocol_rules_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES protocol_versions(tenant_id, protocol_version_id),
  CONSTRAINT protocol_rules_sop_version_tenant_fk FOREIGN KEY (tenant_id, sop_version_id) REFERENCES sop_versions(tenant_id, sop_version_id)
);

CREATE INDEX protocol_rules_version_idx ON protocol_rules(tenant_id, protocol_version_id, sort_order);

CREATE TABLE protocol_triggers (
  trigger_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  protocol_version_id uuid NOT NULL,
  trigger_type text NOT NULL,
  trigger_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT protocol_triggers_type_check CHECK (trigger_type IN ('schedule', 'goat_lifecycle', 'location_event', 'manual', 'upstream_completion')),
  CONSTRAINT protocol_triggers_tenant_id_unique UNIQUE (tenant_id, trigger_id),
  CONSTRAINT protocol_triggers_version_tenant_fk FOREIGN KEY (tenant_id, protocol_version_id) REFERENCES protocol_versions(tenant_id, protocol_version_id)
);

CREATE INDEX protocol_triggers_version_idx ON protocol_triggers(tenant_id, protocol_version_id, is_active);

-- A park-scoped version must reference a same-tenant location of location_type='park'.
-- (tenant scope_id nullness is already enforced by protocol_versions_scope_shape_check.)
CREATE OR REPLACE FUNCTION validate_protocol_version_scope()
RETURNS trigger
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

CREATE TRIGGER protocol_versions_validate_scope_trg
  BEFORE INSERT OR UPDATE OF tenant_id, scope_type, scope_id ON protocol_versions
  FOR EACH ROW EXECUTE FUNCTION validate_protocol_version_scope();

-- Capability definitions (explicit, no wildcard). Member/role assignment (COO/CEO) happens
-- at runtime via workforce_member_capabilities; these rows make the capabilities grantable.
INSERT INTO workforce_capabilities (tenant_id, capability_code, description, status)
VALUES
  ('00000000-0000-4000-8000-000000000001', 'protocol.draft.vaccination', 'Draft/propose vaccination protocol rules.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'protocol.draft.feed_direction', 'Draft/propose feed direction protocol rules.', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'protocol.publish.vaccination', 'Publish vaccination protocol versions (CEO/COO).', 'active'),
  ('00000000-0000-4000-8000-000000000001', 'protocol.publish.feed_direction', 'Publish feed direction protocol versions (CEO/COO).', 'active')
ON CONFLICT (tenant_id, capability_code) DO NOTHING;

-- +goose Down
DELETE FROM workforce_capabilities
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND capability_code IN (
    'protocol.draft.vaccination', 'protocol.draft.feed_direction',
    'protocol.publish.vaccination', 'protocol.publish.feed_direction');
DROP INDEX IF EXISTS protocol_triggers_version_idx;
DROP TABLE IF EXISTS protocol_triggers;
DROP INDEX IF EXISTS protocol_rules_version_idx;
DROP TABLE IF EXISTS protocol_rules;
DROP INDEX IF EXISTS protocol_versions_lookup_idx;
DROP TRIGGER IF EXISTS protocol_versions_validate_scope_trg ON protocol_versions;
DROP TABLE IF EXISTS protocol_versions;
DROP FUNCTION IF EXISTS validate_protocol_version_scope();
DROP TABLE IF EXISTS protocol_definitions;
ALTER TABLE sop_versions DROP CONSTRAINT IF EXISTS sop_versions_tenant_id_unique;
