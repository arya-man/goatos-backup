-- +goose Up
-- A birth submission creates every canonical child immediately. Web approval is a separate,
-- litter-grain transition that controls only whether those children participate in herd counts.
-- Existing birth rows pre-date that split and were already counted, so they backfill as approved.
ALTER TABLE public.goat_births
  ADD COLUMN birth_event_id uuid,
  ADD COLUMN child_ordinal smallint,
  ADD COLUMN count_status text NOT NULL DEFAULT 'approved',
  ADD COLUMN count_approved_at timestamp with time zone,
  ADD COLUMN count_approved_by uuid;

UPDATE public.goat_births
SET birth_event_id = child_goat_id,
    child_ordinal = 1,
    count_approved_at = COALESCE(count_approved_at, created_at)
WHERE birth_event_id IS NULL OR child_ordinal IS NULL;

ALTER TABLE public.goat_births
  ALTER COLUMN birth_event_id SET NOT NULL,
  ALTER COLUMN child_ordinal SET NOT NULL,
  ADD CONSTRAINT goat_births_child_ordinal_check CHECK (child_ordinal BETWEEN 1 AND litter_size),
  ADD CONSTRAINT goat_births_count_status_check CHECK (count_status IN ('pending', 'approved', 'rejected')),
  ADD CONSTRAINT goat_births_count_decision_shape_check CHECK (
    (count_status = 'approved' AND count_approved_at IS NOT NULL)
    OR (count_status <> 'approved' AND count_approved_at IS NULL AND count_approved_by IS NULL)
  ),
  ADD CONSTRAINT goat_births_event_child_unique UNIQUE (tenant_id, birth_event_id, child_ordinal);

CREATE INDEX goat_births_count_pending_idx
  ON public.goat_births (tenant_id, birth_event_id, child_goat_id)
  WHERE count_status = 'pending';

-- Exact PK lookup: one child has at most one goat_births row. Legacy/non-birth goats have no row
-- and remain eligible; only a birth row explicitly marked approved joins the herd projection.
CREATE OR REPLACE FUNCTION public.herd_register_goat_count_eligible(p_tenant_id uuid, p_goat_id uuid)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
  SELECT NOT EXISTS (
    SELECT 1
    FROM public.goat_births gb
    WHERE gb.tenant_id = p_tenant_id
      AND gb.child_goat_id = p_goat_id
      AND gb.count_status <> 'approved'
  );
$$;

CREATE OR REPLACE FUNCTION public.herd_register_refresh_goat_projection(p_tenant_id uuid, p_goat_id uuid)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
  INSERT INTO herd_register_goat_projection (
    tenant_id, goat_id, display_id, park_id, farm_id, current_location_id,
    breed, sex, lifecycle_status, is_kid, is_untagged, projected_at
  )
  SELECT
    g.tenant_id, g.goat_id, g.display_id, g.park_id, g.farm_id, g.current_location_id,
    g.breed, g.sex, g.lifecycle_status,
    herd_register_is_kid(g.age_band, g.management_stage),
    NOT EXISTS (
      SELECT 1 FROM goat_identifiers gi
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
    AND herd_register_goat_count_eligible(g.tenant_id, g.goat_id)
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
    EXCLUDED.display_id, EXCLUDED.park_id, EXCLUDED.farm_id,
    EXCLUDED.current_location_id, EXCLUDED.breed, EXCLUDED.sex,
    EXCLUDED.lifecycle_status, EXCLUDED.is_kid, EXCLUDED.is_untagged
  );

  IF NOT FOUND THEN
    DELETE FROM herd_register_goat_projection p
    WHERE p.tenant_id = p_tenant_id
      AND p.goat_id = p_goat_id
      AND NOT EXISTS (
        SELECT 1 FROM goats g
        WHERE g.tenant_id = p_tenant_id
          AND g.goat_id = p_goat_id
          AND g.merged_into_goat_id IS NULL
          AND herd_register_goat_count_eligible(g.tenant_id, g.goat_id)
      );
  END IF;
END;
$$;

CREATE OR REPLACE FUNCTION public.herd_register_birth_after_write_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  PERFORM herd_register_refresh_goat_projection(NEW.tenant_id, NEW.child_goat_id);
  RETURN NEW;
END;
$$;

CREATE TRIGGER goat_births_herd_projection_trg
AFTER INSERT OR UPDATE OF count_status ON public.goat_births
FOR EACH ROW EXECUTE FUNCTION public.herd_register_birth_after_write_trg();

-- +goose Down
DROP TRIGGER IF EXISTS goat_births_herd_projection_trg ON public.goat_births;
DROP FUNCTION IF EXISTS public.herd_register_birth_after_write_trg();

-- Keep the projection function callable while restoring pre-gate behavior.
CREATE OR REPLACE FUNCTION public.herd_register_goat_count_eligible(uuid, uuid)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$ SELECT true; $$;

DROP INDEX IF EXISTS public.goat_births_count_pending_idx;
ALTER TABLE public.goat_births
  DROP CONSTRAINT IF EXISTS goat_births_event_child_unique,
  DROP CONSTRAINT IF EXISTS goat_births_count_decision_shape_check,
  DROP CONSTRAINT IF EXISTS goat_births_count_status_check,
  DROP CONSTRAINT IF EXISTS goat_births_child_ordinal_check,
  DROP COLUMN IF EXISTS count_approved_by,
  DROP COLUMN IF EXISTS count_approved_at,
  DROP COLUMN IF EXISTS count_status,
  DROP COLUMN IF EXISTS child_ordinal,
  DROP COLUMN IF EXISTS birth_event_id;
