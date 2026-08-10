-- +goose Up
-- Make shed partitions addressable as real operational location rows.
--
-- Domain invariant after this migration:
--   * goats.current_location_id = exact operational residence
--       - pen row for partitioned sheds
--       - shed row for undivided/lumpsum sheds
--   * goats.shed_id = physical parent shed / rollup shed
--   * shed_partitions.operational_location_id = durable parent+partition -> pen mapping
--
-- This migration intentionally updates only live goats that already have exact
-- goat_shed_partitions evidence. Terminal history/proofs/observations/events are
-- snapshots and are not rewritten here.

ALTER TABLE public.shed_partitions
  ADD COLUMN IF NOT EXISTS operational_location_id uuid;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'shed_partitions_operational_location_fk'
      AND conrelid = 'public.shed_partitions'::regclass
  ) THEN
    ALTER TABLE public.shed_partitions
      ADD CONSTRAINT shed_partitions_operational_location_fk
      FOREIGN KEY (tenant_id, operational_location_id)
      REFERENCES public.locations (tenant_id, location_id)
      ON DELETE RESTRICT;
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS shed_partitions_operational_location_unique
  ON public.shed_partitions (tenant_id, operational_location_id)
  WHERE operational_location_id IS NOT NULL;

DO $$
DECLARE
  ambiguous_count integer;
BEGIN
  SELECT count(*) INTO ambiguous_count
  FROM (
    SELECT sp.tenant_id, sp.shed_id, sp.normalized_label
    FROM public.shed_partitions sp
    JOIN public.locations pen
      ON pen.tenant_id = sp.tenant_id
     AND pen.parent_location_id = sp.shed_id
     AND pen.location_type = 'pen'
     AND (
       (sp.status = 'active' AND pen.status = 'active')
       OR (sp.status = 'retired' AND pen.status = 'inactive')
     )
    JOIN public.locations parent
      ON parent.tenant_id = pen.tenant_id
     AND parent.location_id = pen.parent_location_id
    WHERE (
      lower(pen.name) = lower(parent.name || ' - Part ' || sp.partition_label)
      OR lower(pen.name) = lower(parent.name || ' - Part ' || sp.normalized_label)
    )
    GROUP BY sp.tenant_id, sp.shed_id, sp.normalized_label
    HAVING count(*) > 1
  ) ambiguous;

  IF ambiguous_count <> 0 THEN
    RAISE EXCEPTION 'partition_operational_location_mapping_ambiguous: % partition rows match multiple candidate pens', ambiguous_count;
  END IF;
END $$;

-- The catalog was deliberately loose when first introduced. Before attaching a
-- location id, make sure every current goat-side partition is represented.
INSERT INTO public.shed_partitions (
  tenant_id,
  shed_id,
  partition_label,
  normalized_label,
  source
)
SELECT DISTINCT ON (
    gsp.tenant_id,
    gsp.shed_id,
    regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
  )
  gsp.tenant_id,
  gsp.shed_id,
  btrim(gsp.partition_label),
  regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', ''),
  'goat_attested'
FROM public.goat_shed_partitions gsp
WHERE regexp_replace(lower(btrim(COALESCE(gsp.partition_label, 'whole'))), '^part[[:space:]]+', '') <> 'whole'
ORDER BY
  gsp.tenant_id,
  gsp.shed_id,
  regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', ''),
  btrim(gsp.partition_label)
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING;

DO $$
DECLARE
  ambiguous_count integer;
BEGIN
  SELECT count(*) INTO ambiguous_count
  FROM (
    SELECT sp.tenant_id, sp.shed_id, sp.normalized_label
    FROM public.shed_partitions sp
    JOIN public.locations pen
      ON pen.tenant_id = sp.tenant_id
     AND pen.parent_location_id = sp.shed_id
     AND pen.location_type = 'pen'
     AND (
       (sp.status = 'active' AND pen.status = 'active')
       OR (sp.status = 'retired' AND pen.status = 'inactive')
     )
    JOIN public.locations parent
      ON parent.tenant_id = pen.tenant_id
     AND parent.location_id = pen.parent_location_id
    WHERE (
      lower(pen.name) = lower(parent.name || ' - Part ' || sp.partition_label)
      OR lower(pen.name) = lower(parent.name || ' - Part ' || sp.normalized_label)
    )
    GROUP BY sp.tenant_id, sp.shed_id, sp.normalized_label
    HAVING count(*) > 1
  ) ambiguous;

  IF ambiguous_count <> 0 THEN
    RAISE EXCEPTION 'partition_operational_location_mapping_ambiguous: % partition rows match multiple candidate pens after goat-attested partition import', ambiguous_count;
  END IF;
END $$;

-- Reuse existing pen children when a prior run already created them.
UPDATE public.shed_partitions sp
SET operational_location_id = pen.location_id,
    updated_at = now()
FROM public.locations pen
JOIN public.locations parent
  ON parent.tenant_id = pen.tenant_id
 AND parent.location_id = pen.parent_location_id
WHERE sp.operational_location_id IS NULL
  AND pen.tenant_id = sp.tenant_id
  AND pen.parent_location_id = sp.shed_id
  AND pen.location_type = 'pen'
  AND (
    (sp.status = 'active' AND pen.status = 'active')
    OR (sp.status = 'retired' AND pen.status = 'inactive')
  )
  AND (
    lower(pen.name) = lower(parent.name || ' - Part ' || sp.partition_label)
    OR lower(pen.name) = lower(parent.name || ' - Part ' || sp.normalized_label)
  );

-- Create one canonical pen row per catalog partition still missing a real
-- location. Prefer an existing legacy alias display name when one matches the
-- same parent shed inside the same park; otherwise use "Parent - Part N".
-- Retired partitions are still mapped, but their pen rows stay inactive so
-- they preserve history without entering active operator dropdowns.
WITH alias_names AS (
  SELECT DISTINCT ON (sp.tenant_id, sp.shed_id, sp.normalized_label)
    sp.tenant_id,
    sp.shed_id,
    sp.normalized_label,
    alias.name AS display_name,
    alias.display_order
  FROM public.shed_partitions sp
  JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
   AND parent.location_type = 'shed'
  JOIN public.locations alias
    ON alias.tenant_id = sp.tenant_id
   AND alias.parent_location_id = parent.parent_location_id
   AND alias.location_type = 'shed'
   AND alias.status = 'inactive'
   AND alias.name <> parent.name
   AND alias.name LIKE parent.name || '%'
  WHERE sp.operational_location_id IS NULL
    AND regexp_replace(
          lower(btrim(regexp_replace(substr(alias.name, length(parent.name) + 1), '^[[:space:]]*-?[[:space:]]*', ''))),
          '^part[[:space:]]+',
          ''
        ) = sp.normalized_label
  ORDER BY sp.tenant_id, sp.shed_id, sp.normalized_label, length(alias.name)
),
created AS (
  INSERT INTO public.locations (
    tenant_id,
    location_type,
    location_code,
    name,
    parent_location_id,
    country,
    timezone,
    status,
    display_order,
    operational_notes
  )
  SELECT
    sp.tenant_id,
    'pen',
    NULL,
    COALESCE(an.display_name, parent.name || ' - Part ' || sp.normalized_label),
    sp.shed_id,
    parent.country,
    parent.timezone,
    CASE WHEN sp.status = 'active' THEN 'active' ELSE 'inactive' END,
    COALESCE(an.display_order, COALESCE(sp.display_order, 0)),
    'Created by migration 000148 from shed_partitions parent+partition mapping'
  FROM public.shed_partitions sp
  JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
   AND parent.location_type = 'shed'
  LEFT JOIN alias_names an
    ON an.tenant_id = sp.tenant_id
   AND an.shed_id = sp.shed_id
   AND an.normalized_label = sp.normalized_label
  WHERE sp.operational_location_id IS NULL
  RETURNING tenant_id, location_id, parent_location_id, name
)
UPDATE public.shed_partitions sp
SET operational_location_id = created.location_id,
    updated_at = now()
FROM created
WHERE sp.tenant_id = created.tenant_id
  AND sp.shed_id = created.parent_location_id
  AND (
    lower(created.name) = lower((SELECT p.name FROM public.locations p WHERE p.tenant_id = sp.tenant_id AND p.location_id = sp.shed_id) || ' - Part ' || sp.partition_label)
    OR regexp_replace(
         lower(btrim(regexp_replace(substr(created.name, length((SELECT p.name FROM public.locations p WHERE p.tenant_id = sp.tenant_id AND p.location_id = sp.shed_id)) + 1), '^[[:space:]]*-?[[:space:]]*', ''))),
         '^part[[:space:]]+',
         ''
       ) = sp.normalized_label
  );

DO $$
DECLARE
  missing_count integer;
BEGIN
  SELECT count(*) INTO missing_count
  FROM public.shed_partitions sp
  WHERE sp.operational_location_id IS NULL;

  IF missing_count <> 0 THEN
    RAISE EXCEPTION 'partition_operational_location_mapping_incomplete: % shed_partitions lack operational_location_id', missing_count;
  END IF;
END $$;

-- Validate the bridge points to a pen under the physical parent shed. Active
-- partitions must point to active pens; retired partitions may point to
-- inactive pens.
DO $$
DECLARE
  bad_count integer;
BEGIN
  SELECT count(*) INTO bad_count
  FROM public.shed_partitions sp
  JOIN public.locations pen
    ON pen.tenant_id = sp.tenant_id
   AND pen.location_id = sp.operational_location_id
  WHERE NOT (
      pen.location_type = 'pen'
      AND pen.parent_location_id = sp.shed_id
      AND (
        (sp.status = 'active' AND pen.status = 'active')
        OR (sp.status = 'retired' AND pen.status = 'inactive')
      )
    );

  IF bad_count <> 0 THEN
    RAISE EXCEPTION 'partition_operational_location_mapping_invalid: % rows do not point to valid pen children', bad_count;
  END IF;
END $$;

-- Copy parent operational flags onto mapped pen rows. Vaccination/procurement
-- eligibility code reads location_operational_attributes from the goat's exact
-- current_location_id; after this migration that is a pen for partitioned goats.
-- Without this copy, a quarantined/ICU/holding parent shed could look usable
-- through a newly-created pen that has no attributes row.
INSERT INTO public.location_operational_attributes (
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
  notes,
  updated_at
)
SELECT
  parent_loa.tenant_id,
  sp.operational_location_id,
  parent_loa.usable_for_counts,
  parent_loa.usable_for_feed,
  parent_loa.usable_for_vaccination,
  parent_loa.usable_for_sop,
  parent_loa.is_holding,
  parent_loa.is_quarantine,
  parent_loa.is_icu,
  parent_loa.display_order,
  parent_loa.notes,
  now()
FROM public.shed_partitions sp
JOIN public.location_operational_attributes parent_loa
  ON parent_loa.tenant_id = sp.tenant_id
 AND parent_loa.location_id = sp.shed_id
WHERE NOT EXISTS (
  SELECT 1
  FROM public.location_operational_attributes pen_loa
  WHERE pen_loa.tenant_id = sp.tenant_id
    AND pen_loa.location_id = sp.operational_location_id
);

-- Move live partitioned goats to the exact pen location. The parent shed stays
-- in goats.shed_id for rollups and legacy filters.
SET lock_timeout = '2s';
UPDATE public.goats g
SET current_location_id = sp.operational_location_id,
    updated_at = now(),
    row_version = g.row_version + 1
FROM public.goat_shed_partitions gsp
JOIN public.shed_partitions sp
  ON sp.tenant_id = gsp.tenant_id
 AND sp.shed_id = gsp.shed_id
 AND sp.normalized_label = regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
 AND sp.status = 'active'
WHERE g.tenant_id = gsp.tenant_id
  AND g.goat_id = gsp.goat_id
  AND g.shed_id = gsp.shed_id
  AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
  AND g.merged_into_goat_id IS NULL
  AND g.current_location_id IS DISTINCT FROM sp.operational_location_id;
RESET lock_timeout;

-- Block cutover if a live partitioned goat cannot be mapped exactly.
DO $$
DECLARE
  unmapped_count integer;
BEGIN
  SELECT count(*) INTO unmapped_count
  FROM public.goats g
  JOIN public.goat_shed_partitions gsp
    ON gsp.tenant_id = g.tenant_id
   AND gsp.goat_id = g.goat_id
  LEFT JOIN public.shed_partitions sp
    ON sp.tenant_id = gsp.tenant_id
   AND sp.shed_id = gsp.shed_id
   AND sp.normalized_label = regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
   AND sp.status = 'active'
  WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    AND g.merged_into_goat_id IS NULL
    AND g.shed_id = gsp.shed_id
    AND sp.operational_location_id IS NULL;

  IF unmapped_count <> 0 THEN
    RAISE EXCEPTION 'live_partitioned_goats_unmapped: % live goats have no operational pen mapping', unmapped_count;
  END IF;
END $$;

-- Guard the canonical residence invariant for live goats after backfill.
DO $$
DECLARE
  bad_count integer;
BEGIN
  SELECT count(*) INTO bad_count
  FROM public.goats g
  JOIN public.locations cur
    ON cur.tenant_id = g.tenant_id
   AND cur.location_id = g.current_location_id
  JOIN public.locations shed
    ON shed.tenant_id = g.tenant_id
   AND shed.location_id = g.shed_id
  WHERE g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    AND g.merged_into_goat_id IS NULL
    AND NOT (
      (
        cur.location_type = 'pen'
        AND cur.parent_location_id = g.shed_id
        AND shed.parent_location_id = g.park_id
      )
      OR (
        cur.location_type = 'shed'
        AND cur.location_id = g.shed_id
        AND shed.parent_location_id = g.park_id
      )
    );

  IF bad_count <> 0 THEN
    RAISE EXCEPTION 'goat_current_location_rollup_invariant_failed: % live goats violate current_location/shed/park hierarchy', bad_count;
  END IF;
END $$;

-- operational_location_id deliberately remains nullable so legacy fixtures and
-- repair scripts can still create catalog rows before a later reconcile step
-- maps them. Runtime writers fail closed before placing live animals into an
-- unmapped partition.

CREATE OR REPLACE FUNCTION public.ensure_shed_partition_operational_location()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  parent_row public.locations%ROWTYPE;
  candidate_id uuid;
  candidate_count integer;
  mapped_valid boolean;
BEGIN
  IF NEW.status NOT IN ('active', 'retired') THEN
    RETURN NEW;
  END IF;

  SELECT *
  INTO parent_row
  FROM public.locations
  WHERE tenant_id = NEW.tenant_id
    AND location_id = NEW.shed_id
    AND location_type = 'shed'
    AND (NEW.status <> 'active' OR status = 'active');

  IF NOT FOUND THEN
    RAISE EXCEPTION 'shed_partition_parent_shed_missing: tenant %, shed %', NEW.tenant_id, NEW.shed_id;
  END IF;

  IF NEW.status = 'retired' THEN
    IF NEW.operational_location_id IS NULL THEN
      RETURN NEW;
    END IF;

    SELECT EXISTS (
      SELECT 1
      FROM public.locations pen
      WHERE pen.tenant_id = NEW.tenant_id
        AND pen.location_id = NEW.operational_location_id
        AND pen.parent_location_id = NEW.shed_id
        AND pen.location_type = 'pen'
        AND pen.status = 'inactive'
    )
    INTO mapped_valid;

    IF mapped_valid THEN
      RETURN NEW;
    END IF;

    RAISE EXCEPTION 'shed_partition_operational_location_invalid: tenant %, shed %, partition %',
      NEW.tenant_id, NEW.shed_id, NEW.partition_label;
  END IF;

  PERFORM pg_advisory_xact_lock(hashtextextended(
    NEW.tenant_id::text || ':' || NEW.shed_id::text || ':' || NEW.normalized_label,
    148
  ));

  IF NEW.operational_location_id IS NOT NULL THEN
    SELECT EXISTS (
      SELECT 1
      FROM public.locations pen
      WHERE pen.tenant_id = NEW.tenant_id
        AND pen.location_id = NEW.operational_location_id
        AND pen.parent_location_id = NEW.shed_id
        AND pen.location_type = 'pen'
        AND pen.status = 'active'
    )
    INTO mapped_valid;

    IF mapped_valid THEN
      RETURN NEW;
    END IF;

    IF TG_OP = 'UPDATE' AND NEW.operational_location_id IS NOT DISTINCT FROM OLD.operational_location_id THEN
      NEW.operational_location_id := NULL;
    ELSE
      RAISE EXCEPTION 'shed_partition_operational_location_invalid: tenant %, shed %, partition %',
        NEW.tenant_id, NEW.shed_id, NEW.partition_label;
    END IF;
  END IF;

  SELECT count(*), (array_agg(pen.location_id ORDER BY pen.location_id))[1]
  INTO candidate_count, candidate_id
  FROM public.locations pen
  WHERE pen.tenant_id = NEW.tenant_id
    AND pen.parent_location_id = NEW.shed_id
    AND pen.location_type = 'pen'
    AND pen.status = 'active'
    AND (
      lower(pen.name) = lower(parent_row.name || ' - Part ' || NEW.partition_label)
      OR lower(pen.name) = lower(parent_row.name || ' - Part ' || NEW.normalized_label)
    );

  IF candidate_count > 1 THEN
    RAISE EXCEPTION 'shed_partition_operational_location_ambiguous: tenant %, shed %, partition %',
      NEW.tenant_id, NEW.shed_id, NEW.partition_label;
  END IF;

  IF candidate_id IS NULL THEN
    INSERT INTO public.locations (
      tenant_id,
      location_type,
      location_code,
      name,
      parent_location_id,
      country,
      timezone,
      status,
      display_order,
      operational_notes
    ) VALUES (
      NEW.tenant_id,
      'pen',
      NULL,
      parent_row.name || ' - Part ' || NEW.normalized_label,
      NEW.shed_id,
      parent_row.country,
      parent_row.timezone,
      'active',
      COALESCE(NEW.display_order, 0),
      'Created from active shed_partitions row'
    )
    RETURNING location_id INTO candidate_id;
  END IF;

  NEW.operational_location_id := candidate_id;

  INSERT INTO public.location_operational_attributes (
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
    notes,
    updated_at
  )
  SELECT
    parent_loa.tenant_id,
    candidate_id,
    parent_loa.usable_for_counts,
    parent_loa.usable_for_feed,
    parent_loa.usable_for_vaccination,
    parent_loa.usable_for_sop,
    parent_loa.is_holding,
    parent_loa.is_quarantine,
    parent_loa.is_icu,
    parent_loa.display_order,
    parent_loa.notes,
    now()
  FROM public.location_operational_attributes parent_loa
  WHERE parent_loa.tenant_id = NEW.tenant_id
    AND parent_loa.location_id = NEW.shed_id
  ON CONFLICT (location_id) DO NOTHING;

  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS shed_partitions_operational_location_trg ON public.shed_partitions;
CREATE TRIGGER shed_partitions_operational_location_trg
BEFORE INSERT OR UPDATE OF shed_id, partition_label, normalized_label, status, operational_location_id
ON public.shed_partitions
FOR EACH ROW
EXECUTE FUNCTION public.ensure_shed_partition_operational_location();

-- +goose Down
DROP TRIGGER IF EXISTS shed_partitions_operational_location_trg ON public.shed_partitions;
DROP FUNCTION IF EXISTS public.ensure_shed_partition_operational_location();

SET lock_timeout = '2s';
UPDATE public.goats g
SET current_location_id = g.shed_id,
    updated_at = now(),
    row_version = g.row_version + 1
FROM public.shed_partitions sp
WHERE g.tenant_id = sp.tenant_id
  AND g.current_location_id = sp.operational_location_id
  AND g.shed_id = sp.shed_id;
RESET lock_timeout;

ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_operational_location_fk;

DROP INDEX IF EXISTS public.shed_partitions_operational_location_unique;

ALTER TABLE public.shed_partitions
  DROP COLUMN IF EXISTS operational_location_id;
