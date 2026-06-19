-- +goose Up
ALTER TABLE locations
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS row_version integer NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS display_order integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS operational_notes text NULL,
  ADD COLUMN IF NOT EXISTS retired_at timestamptz NULL,
  ADD COLUMN IF NOT EXISTS retired_by uuid NULL;

CREATE INDEX IF NOT EXISTS locations_tenant_parent_status_idx
  ON locations (tenant_id, parent_location_id, status, display_order, name, location_id);

CREATE INDEX IF NOT EXISTS locations_tenant_lower_name_idx
  ON locations (tenant_id, lower(name), location_id);

ALTER TABLE location_aliases
  ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'active',
  ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS row_version integer NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS retired_at timestamptz NULL;

ALTER TABLE location_aliases
  DROP CONSTRAINT IF EXISTS location_aliases_unique_alias;

ALTER TABLE location_aliases
  DROP CONSTRAINT IF EXISTS location_aliases_status_check;

ALTER TABLE location_aliases
  ADD CONSTRAINT location_aliases_status_check CHECK (status IN ('active', 'retired', 'review'));

CREATE UNIQUE INDEX IF NOT EXISTS location_aliases_unique_active_alias
  ON location_aliases (tenant_id, source_context, lower(regexp_replace(btrim(alias_code), '\s+', ' ', 'g')))
  WHERE status = 'active';

CREATE INDEX IF NOT EXISTS location_aliases_canonical_status_idx
  ON location_aliases (tenant_id, canonical_location_id, status, source_context);

CREATE TABLE location_operational_attributes (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  location_id uuid PRIMARY KEY REFERENCES locations(location_id) ON DELETE CASCADE,
  usable_for_counts boolean NOT NULL DEFAULT true,
  usable_for_feed boolean NOT NULL DEFAULT true,
  usable_for_vaccination boolean NOT NULL DEFAULT true,
  usable_for_sop boolean NOT NULL DEFAULT true,
  is_holding boolean NOT NULL DEFAULT false,
  is_quarantine boolean NOT NULL DEFAULT false,
  is_icu boolean NOT NULL DEFAULT false,
  display_order integer NOT NULL DEFAULT 0,
  notes text NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT location_operational_attributes_tenant_location_fk
    FOREIGN KEY (tenant_id, location_id) REFERENCES locations(tenant_id, location_id)
);

CREATE INDEX location_operational_attributes_counts_idx
  ON location_operational_attributes (tenant_id, usable_for_counts, is_holding, is_quarantine, is_icu);

CREATE TABLE location_capacity_records (
  capacity_record_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  location_id uuid NOT NULL REFERENCES locations(location_id),
  capacity_kind text NOT NULL,
  capacity_value integer NOT NULL,
  effective_from date NOT NULL,
  effective_to date NULL,
  source text NOT NULL,
  source_ref text NULL,
  notes text NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT location_capacity_records_tenant_location_fk
    FOREIGN KEY (tenant_id, location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT location_capacity_records_kind_check CHECK (capacity_kind IN ('goat_occupancy', 'quarantine', 'feed_trial', 'other')),
  CONSTRAINT location_capacity_records_source_check CHECK (source IN ('manual', 'legacy_bq', 'android_sop', 'import')),
  CONSTRAINT location_capacity_records_positive_check CHECK (capacity_value > 0),
  CONSTRAINT location_capacity_records_window_check CHECK (effective_to IS NULL OR effective_to > effective_from)
);

CREATE INDEX location_capacity_records_effective_idx
  ON location_capacity_records (tenant_id, location_id, capacity_kind, effective_from DESC, effective_to);

CREATE INDEX location_capacity_records_source_idx
  ON location_capacity_records (tenant_id, source, source_ref);

CREATE OR REPLACE FUNCTION reject_overlapping_location_capacity()
RETURNS trigger
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

CREATE TRIGGER location_capacity_records_no_overlap_trg
BEFORE INSERT OR UPDATE OF tenant_id, location_id, capacity_kind, effective_from, effective_to
ON location_capacity_records
FOR EACH ROW
EXECUTE FUNCTION reject_overlapping_location_capacity();

CREATE TABLE location_review_items (
  review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  review_type text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  source_context text NULL,
  source_label text NULL,
  normalized_source_label text NULL,
  canonical_location_id uuid NULL REFERENCES locations(location_id),
  candidate_location_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  evidence_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence_hash text NOT NULL,
  sync_run_id uuid NULL REFERENCES legacy_sync_runs(sync_run_id) ON DELETE SET NULL,
  created_by uuid NULL,
  resolved_by uuid NULL,
  resolution_notes text NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz NULL,
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT location_review_items_type_check CHECK (review_type IN ('unknown_alias', 'alias_conflict', 'parent_type_conflict', 'capacity_conflict', 'retire_blocked', 'usage_conflict')),
  CONSTRAINT location_review_items_status_check CHECK (status IN ('open', 'resolved', 'dismissed')),
  CONSTRAINT location_review_items_candidate_array_check CHECK (jsonb_typeof(candidate_location_ids) = 'array'),
  CONSTRAINT location_review_items_evidence_object_check CHECK (jsonb_typeof(evidence_json) = 'object')
);

CREATE INDEX location_review_items_queue_idx
  ON location_review_items (tenant_id, status, review_type, updated_at DESC, review_id DESC);

CREATE INDEX location_review_items_label_idx
  ON location_review_items (tenant_id, source_context, normalized_source_label);

CREATE UNIQUE INDEX location_review_items_unique_open_evidence
  ON location_review_items (
    tenant_id,
    review_type,
    COALESCE(source_context, ''),
    COALESCE(normalized_source_label, ''),
    evidence_hash
  )
  WHERE status = 'open';

CREATE TABLE location_projection_invalidations (
  invalidation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  source_module text NOT NULL,
  projection_module text NOT NULL,
  reason text NOT NULL,
  affected_location_id uuid NULL REFERENCES locations(location_id),
  source_ref text NULL,
  status text NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now(),
  acknowledged_at timestamptz NULL,
  CONSTRAINT location_projection_invalidations_status_check CHECK (status IN ('pending', 'acknowledged', 'superseded')),
  CONSTRAINT location_projection_invalidations_projection_check CHECK (projection_module IN ('counts', 'infra', 'mortality', 'feed', 'vaccination', 'other'))
);

CREATE INDEX location_projection_invalidations_pending_idx
  ON location_projection_invalidations (tenant_id, projection_module, status, created_at DESC);

CREATE OR REPLACE FUNCTION location_seeded_scope_guard()
RETURNS trigger
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
    ELSIF TG_TABLE_NAME = 'location_aliases' THEN
      IF OLD.tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
        AND OLD.source_context IN ('legacy_location_code', 'legacy_bq_dashboard_shed') THEN
        RAISE EXCEPTION 'seeded location alias scope requires approved migration plan';
      END IF;
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER locations_seeded_scope_guard_update_trg
BEFORE UPDATE OF location_type, location_code, name, parent_location_id, status, retired_at, retired_by
ON locations
FOR EACH ROW
EXECUTE FUNCTION location_seeded_scope_guard();

CREATE TRIGGER locations_seeded_scope_guard_delete_trg
BEFORE DELETE
ON locations
FOR EACH ROW
EXECUTE FUNCTION location_seeded_scope_guard();

CREATE TRIGGER location_aliases_seeded_scope_guard_update_trg
BEFORE UPDATE OF alias_code, canonical_location_id, source_context, status, retired_at
ON location_aliases
FOR EACH ROW
EXECUTE FUNCTION location_seeded_scope_guard();

CREATE TRIGGER location_aliases_seeded_scope_guard_delete_trg
BEFORE DELETE
ON location_aliases
FOR EACH ROW
EXECUTE FUNCTION location_seeded_scope_guard();

INSERT INTO location_operational_attributes (
  tenant_id,
  location_id,
  usable_for_counts,
  usable_for_feed,
  usable_for_vaccination,
  usable_for_sop,
  is_holding,
  is_quarantine,
  is_icu,
  display_order,
  notes
)
SELECT
  l.tenant_id,
  l.location_id,
  true,
  true,
  true,
  true,
  l.location_code IN ('HF_UNKNOWN'),
  false,
  false,
  l.display_order,
  CASE WHEN l.location_code IN ('CBE', 'CPT', 'HF_UNKNOWN') THEN 'Seeded Phase 1 scope anchor; migration-plan protected.' ELSE NULL END
FROM locations l
ON CONFLICT (location_id) DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS location_aliases_seeded_scope_guard_delete_trg ON location_aliases;
DROP TRIGGER IF EXISTS location_aliases_seeded_scope_guard_update_trg ON location_aliases;
DROP TRIGGER IF EXISTS locations_seeded_scope_guard_delete_trg ON locations;
DROP TRIGGER IF EXISTS locations_seeded_scope_guard_update_trg ON locations;
DROP FUNCTION IF EXISTS location_seeded_scope_guard();
DROP TABLE IF EXISTS location_projection_invalidations;
DROP INDEX IF EXISTS location_review_items_unique_open_evidence;
DROP TABLE IF EXISTS location_review_items;
DROP TRIGGER IF EXISTS location_capacity_records_no_overlap_trg ON location_capacity_records;
DROP FUNCTION IF EXISTS reject_overlapping_location_capacity();
DROP TABLE IF EXISTS location_capacity_records;
DROP TABLE IF EXISTS location_operational_attributes;
DROP INDEX IF EXISTS location_aliases_unique_active_alias;
ALTER TABLE location_aliases DROP CONSTRAINT IF EXISTS location_aliases_status_check;
ALTER TABLE location_aliases
  DROP COLUMN IF EXISTS retired_at,
  DROP COLUMN IF EXISTS row_version,
  DROP COLUMN IF EXISTS updated_at,
  DROP COLUMN IF EXISTS status;
ALTER TABLE location_aliases
  ADD CONSTRAINT location_aliases_unique_alias UNIQUE (tenant_id, alias_code, source_context);
ALTER TABLE locations
  DROP COLUMN IF EXISTS retired_by,
  DROP COLUMN IF EXISTS retired_at,
  DROP COLUMN IF EXISTS operational_notes,
  DROP COLUMN IF EXISTS display_order,
  DROP COLUMN IF EXISTS row_version,
  DROP COLUMN IF EXISTS updated_at;
