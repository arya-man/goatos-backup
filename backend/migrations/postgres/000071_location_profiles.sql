-- +goose Up
-- Phase 0 · Location profiles + structural lookups (schema only).
-- farm/park/shed profiles are 1:1 extensions of locations. Tenant safety is enforced by
-- composite (tenant_id, location_id) FKs (mirrors the 000002 hardening pattern) plus a
-- per-profile location_type guard trigger.

CREATE TABLE animal_stage_lookup (
  animal_stage_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  stage_code text NOT NULL,
  name text NOT NULL,
  min_age_days int NULL,
  max_age_days int NULL,
  sort_order int NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT animal_stage_lookup_status_check CHECK (status IN ('active', 'inactive', 'retired')),
  CONSTRAINT animal_stage_lookup_age_check CHECK (min_age_days IS NULL OR max_age_days IS NULL OR max_age_days >= min_age_days),
  CONSTRAINT animal_stage_lookup_code_unique UNIQUE (tenant_id, stage_code),
  CONSTRAINT animal_stage_lookup_tenant_id_unique UNIQUE (tenant_id, animal_stage_id)
);

CREATE TABLE shed_lifecycle_status_lookup (
  shed_lifecycle_status_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  status_code text NOT NULL,
  name text NOT NULL,
  sort_order int NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shed_lifecycle_status_lookup_status_check CHECK (status IN ('active', 'inactive', 'retired')),
  CONSTRAINT shed_lifecycle_status_lookup_code_unique UNIQUE (tenant_id, status_code),
  CONSTRAINT shed_lifecycle_status_lookup_tenant_id_unique UNIQUE (tenant_id, shed_lifecycle_status_id)
);

CREATE TABLE farm_profiles (
  location_id uuid PRIMARY KEY REFERENCES locations(location_id),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  farm_kind text NULL,
  capacity int NULL,
  notes text NOT NULL DEFAULT '',
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT farm_profiles_kind_check CHECK (farm_kind IS NULL OR farm_kind IN ('core', 'holding', 'contract')),
  CONSTRAINT farm_profiles_capacity_check CHECK (capacity IS NULL OR capacity >= 0),
  CONSTRAINT farm_profiles_row_version_check CHECK (row_version >= 1),
  CONSTRAINT farm_profiles_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES locations(tenant_id, location_id)
);

CREATE TABLE park_profiles (
  location_id uuid PRIMARY KEY REFERENCES locations(location_id),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  park_code text NULL,
  capacity int NULL,
  notes text NOT NULL DEFAULT '',
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT park_profiles_capacity_check CHECK (capacity IS NULL OR capacity >= 0),
  CONSTRAINT park_profiles_row_version_check CHECK (row_version >= 1),
  CONSTRAINT park_profiles_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES locations(tenant_id, location_id)
);

CREATE TABLE shed_profiles (
  location_id uuid PRIMARY KEY REFERENCES locations(location_id),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  animal_stage_id uuid NULL,
  shed_lifecycle_status_id uuid NULL,
  sex text NULL,
  capacity int NULL,
  has_icu boolean NOT NULL DEFAULT false,
  notes text NOT NULL DEFAULT '',
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  row_version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT shed_profiles_sex_check CHECK (sex IS NULL OR sex IN ('female', 'male', 'mixed')),
  CONSTRAINT shed_profiles_capacity_check CHECK (capacity IS NULL OR capacity >= 0),
  CONSTRAINT shed_profiles_row_version_check CHECK (row_version >= 1),
  CONSTRAINT shed_profiles_location_tenant_fk FOREIGN KEY (tenant_id, location_id) REFERENCES locations(tenant_id, location_id),
  CONSTRAINT shed_profiles_animal_stage_tenant_fk FOREIGN KEY (tenant_id, animal_stage_id) REFERENCES animal_stage_lookup(tenant_id, animal_stage_id),
  CONSTRAINT shed_profiles_lifecycle_status_tenant_fk FOREIGN KEY (tenant_id, shed_lifecycle_status_id) REFERENCES shed_lifecycle_status_lookup(tenant_id, shed_lifecycle_status_id)
);

CREATE INDEX shed_profiles_animal_stage_idx ON shed_profiles(animal_stage_id);

CREATE OR REPLACE FUNCTION validate_location_profile_type()
RETURNS trigger
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

CREATE TRIGGER farm_profiles_validate_type_trg
  BEFORE INSERT OR UPDATE OF tenant_id, location_id ON farm_profiles
  FOR EACH ROW EXECUTE FUNCTION validate_location_profile_type('farm');
CREATE TRIGGER park_profiles_validate_type_trg
  BEFORE INSERT OR UPDATE OF tenant_id, location_id ON park_profiles
  FOR EACH ROW EXECUTE FUNCTION validate_location_profile_type('park');
CREATE TRIGGER shed_profiles_validate_type_trg
  BEFORE INSERT OR UPDATE OF tenant_id, location_id ON shed_profiles
  FOR EACH ROW EXECUTE FUNCTION validate_location_profile_type('shed');

-- +goose Down
DROP TRIGGER IF EXISTS shed_profiles_validate_type_trg ON shed_profiles;
DROP TRIGGER IF EXISTS park_profiles_validate_type_trg ON park_profiles;
DROP TRIGGER IF EXISTS farm_profiles_validate_type_trg ON farm_profiles;
DROP FUNCTION IF EXISTS validate_location_profile_type();
DROP INDEX IF EXISTS shed_profiles_animal_stage_idx;
DROP TABLE IF EXISTS shed_profiles;
DROP TABLE IF EXISTS park_profiles;
DROP TABLE IF EXISTS farm_profiles;
DROP TABLE IF EXISTS shed_lifecycle_status_lookup;
DROP TABLE IF EXISTS animal_stage_lookup;
