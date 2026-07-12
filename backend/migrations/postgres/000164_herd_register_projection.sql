-- +goose Up
BEGIN;

-- The repository migration runner executes statements individually. We intentionally avoid locking
-- canonical identity tables in SHARE ROW EXCLUSIVE mode here to prevent writer stalls on large herds.
-- Instead, this migration performs a full set-based projection backfill and then enables live write
-- triggers so normal upstream updates continue without a write-time lock. Any brief in-flight window is
-- bounded by normal backfill semantics and accepted for this online migration.

-- Herd Register request paths serve one cursor page from goats and exact KPI
-- counts from this incrementally maintained read model. The summary relation is
-- at dimension-grain, so its size follows configured parks/breeds/statuses, not
-- the number of animals.
CREATE TABLE herd_register_goat_projection (
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
  projected_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, goat_id),
  CONSTRAINT herd_register_goat_projection_goat_fk
    FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id) ON DELETE CASCADE
);

COMMENT ON TABLE herd_register_goat_projection IS
  'Identity-owned per-goat read model used for bounded exact-id/identifier Herd Register summaries. Canonical goats and identifiers remain truth.';

CREATE UNIQUE INDEX herd_register_goat_projection_display_uidx
  ON herd_register_goat_projection (tenant_id, display_id);

CREATE TABLE herd_register_summary_projection (
  herd_register_summary_projection_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  park_id uuid,
  farm_id uuid,
  current_location_id uuid,
  breed text,
  sex text NOT NULL,
  lifecycle_status text NOT NULL,
  active_count bigint NOT NULL DEFAULT 0,
  adult_count bigint NOT NULL DEFAULT 0,
  kid_count bigint NOT NULL DEFAULT 0,
  untagged_kid_count bigint NOT NULL DEFAULT 0,
  projected_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT herd_register_summary_projection_scope_key
    UNIQUE NULLS NOT DISTINCT (
      tenant_id, park_id, farm_id, current_location_id, breed, sex, lifecycle_status
    ),
  CONSTRAINT herd_register_summary_projection_nonnegative_check CHECK (
    active_count >= 0 AND adult_count >= 0 AND kid_count >= 0 AND untagged_kid_count >= 0
  )
);

COMMENT ON TABLE herd_register_summary_projection IS
  'Exact Herd Register KPIs at finite filter-dimension grain. API requests sum these rows and never aggregate the goats table.';

CREATE INDEX herd_register_summary_projection_scope_idx
  ON herd_register_summary_projection (
    tenant_id, lifecycle_status, park_id, breed, sex, farm_id, current_location_id
  ) INCLUDE (active_count, adult_count, kid_count, untagged_kid_count);

CREATE FUNCTION herd_register_is_kid(p_age_band text, p_management_stage text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
  SELECT lower(COALESCE(p_age_band, '')) = 'kid'
      OR (
        lower(COALESCE(p_age_band, '')) NOT IN ('kid', 'adult')
        AND upper(COALESCE(p_management_stage, '')) ~ '^K[0-9]'
      );
$$;

-- Initial projection is set-based. Creating the maintenance triggers after the
-- backfill avoids millions of row-triggered aggregate upserts during migration.
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
WHERE g.merged_into_goat_id IS NULL;

INSERT INTO herd_register_summary_projection (
  tenant_id, park_id, farm_id, current_location_id, breed, sex, lifecycle_status,
  active_count, adult_count, kid_count, untagged_kid_count, projected_at
)
SELECT
  tenant_id,
  park_id,
  farm_id,
  current_location_id,
  breed,
  sex,
  lifecycle_status,
  count(*)::bigint,
  count(*) FILTER (WHERE NOT is_kid)::bigint,
  count(*) FILTER (WHERE is_kid)::bigint,
  count(*) FILTER (WHERE is_kid AND is_untagged)::bigint,
  now()
FROM herd_register_goat_projection
GROUP BY tenant_id, park_id, farm_id, current_location_id, breed, sex, lifecycle_status;

CREATE FUNCTION herd_register_apply_summary_delta(
  p_tenant_id uuid,
  p_park_id uuid,
  p_farm_id uuid,
  p_current_location_id uuid,
  p_breed text,
  p_sex text,
  p_lifecycle_status text,
  p_active_delta bigint,
  p_adult_delta bigint,
  p_kid_delta bigint,
  p_untagged_kid_delta bigint
)
RETURNS void
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

CREATE FUNCTION herd_register_projection_summary_trg()
RETURNS trigger
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

CREATE TRIGGER herd_register_projection_summary_after_write_trg
AFTER INSERT OR DELETE OR UPDATE OF
  park_id, farm_id, current_location_id, breed, sex, lifecycle_status, is_kid, is_untagged
ON herd_register_goat_projection
FOR EACH ROW EXECUTE FUNCTION herd_register_projection_summary_trg();

CREATE FUNCTION herd_register_refresh_goat_projection(p_tenant_id uuid, p_goat_id uuid)
RETURNS void
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

CREATE FUNCTION herd_register_goats_after_write_trg()
RETURNS trigger
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

CREATE TRIGGER herd_register_goats_after_write_trg
AFTER INSERT OR DELETE OR UPDATE OF
  tenant_id, goat_id, display_id, park_id, farm_id, current_location_id,
  breed, sex, lifecycle_status, age_band, management_stage, merged_into_goat_id
ON goats
FOR EACH ROW EXECUTE FUNCTION herd_register_goats_after_write_trg();

CREATE FUNCTION herd_register_identifiers_after_write_trg()
RETURNS trigger
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

CREATE TRIGGER herd_register_identifiers_after_write_trg
AFTER INSERT OR DELETE OR UPDATE OF
  tenant_id, goat_id, identifier_type, status
ON goat_identifiers
FOR EACH ROW EXECUTE FUNCTION herd_register_identifiers_after_write_trg();

COMMIT;

-- +goose Down
DROP TRIGGER IF EXISTS herd_register_identifiers_after_write_trg ON goat_identifiers;
DROP FUNCTION IF EXISTS herd_register_identifiers_after_write_trg();
DROP TRIGGER IF EXISTS herd_register_goats_after_write_trg ON goats;
DROP FUNCTION IF EXISTS herd_register_goats_after_write_trg();
DROP FUNCTION IF EXISTS herd_register_refresh_goat_projection(uuid, uuid);
DROP TRIGGER IF EXISTS herd_register_projection_summary_after_write_trg ON herd_register_goat_projection;
DROP FUNCTION IF EXISTS herd_register_projection_summary_trg();
DROP FUNCTION IF EXISTS herd_register_apply_summary_delta(uuid, uuid, uuid, uuid, text, text, text, bigint, bigint, bigint, bigint);
DROP FUNCTION IF EXISTS herd_register_is_kid(text, text);
DROP TABLE IF EXISTS herd_register_summary_projection;
DROP TABLE IF EXISTS herd_register_goat_projection;
