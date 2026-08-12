-- +goose Up
-- Make shed partitions addressable as real operational location rows.
--
-- Domain invariant after this migration:
--   * goats.current_location_id = exact operational residence
--       - partition shed row for partitioned sheds
--       - shed row for undivided/lumpsum sheds
--   * goats.shed_id = exact real shed/partition residence
--       - same partition shed row as current_location_id for partitioned sheds
--       - same shed row as current_location_id for undivided/lumpsum sheds
--   * goats.shed_group_id = legacy parent/group shed when a partition exists
--   * shed_partitions.operational_location_id = durable group+partition -> real shed mapping
--
-- This migration intentionally updates only live goats that already have exact
-- goat_shed_partitions evidence. Terminal history/proofs/observations/events are
-- snapshots and are not rewritten here.

ALTER TABLE public.shed_partitions
  ADD COLUMN IF NOT EXISTS operational_location_id uuid;

ALTER TABLE public.goats
  ADD COLUMN IF NOT EXISTS shed_group_id uuid;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'goats_shed_group_tenant_fk'
      AND conrelid = 'public.goats'::regclass
  ) THEN
    ALTER TABLE public.goats
      ADD CONSTRAINT goats_shed_group_tenant_fk
      FOREIGN KEY (tenant_id, shed_group_id)
      REFERENCES public.locations (tenant_id, location_id)
      ON DELETE RESTRICT
      NOT VALID;
  END IF;
END $$;

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
      ON DELETE RESTRICT
      NOT VALID;
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS shed_partitions_operational_location_unique
  ON public.shed_partitions (tenant_id, operational_location_id)
  WHERE operational_location_id IS NOT NULL;

CREATE OR REPLACE FUNCTION public.operational_location_display(
  p_shed_name text,
  p_partition_label text
) RETURNS text
LANGUAGE sql
AS $$
  SELECT NULLIF(BTRIM(
    CASE
      WHEN NULLIF(BTRIM(COALESCE(p_partition_label, '')), '') IS NULL
        OR lower(BTRIM(p_partition_label)) = 'whole'
        THEN BTRIM(COALESCE(p_shed_name, ''))
      WHEN BTRIM(p_partition_label) ~* '^part[[:space:]]+'
        THEN format('%s - %s', BTRIM(COALESCE(p_shed_name, '')), BTRIM(p_partition_label))
      WHEN BTRIM(p_partition_label) ~ '^[0-9]+$'
        THEN format('%s - Part %s', BTRIM(COALESCE(p_shed_name, '')), BTRIM(p_partition_label))
      ELSE format('%s - %s', BTRIM(COALESCE(p_shed_name, '')), BTRIM(p_partition_label))
    END
  ), '');
$$;

CREATE OR REPLACE FUNCTION public.shed_partition_lock_key(
  p_tenant_id uuid,
  p_shed_id uuid,
  p_partition_label text
) RETURNS text
LANGUAGE sql
AS $$
  SELECT p_tenant_id::text || ':' || p_shed_id::text || ':' ||
	         regexp_replace(lower(btrim(COALESCE(p_partition_label, 'whole'))), '^part[[:space:]]+', '');
$$;

CREATE OR REPLACE FUNCTION public.copy_shed_partition_profile(
  p_tenant_id uuid,
  p_group_shed_id uuid,
  p_exact_shed_id uuid
) RETURNS void
LANGUAGE sql
AS $$
  INSERT INTO public.shed_profiles (
    location_id,
    tenant_id,
    animal_stage_id,
    shed_lifecycle_status_id,
    sex,
    capacity,
    has_icu,
    notes,
    context,
    row_version,
    created_at,
    updated_at
  )
  SELECT
    p_exact_shed_id,
    parent_profile.tenant_id,
    parent_profile.animal_stage_id,
    parent_profile.shed_lifecycle_status_id,
    parent_profile.sex,
    parent_profile.capacity,
    parent_profile.has_icu,
    parent_profile.notes,
    parent_profile.context,
    1,
    now(),
    now()
  FROM public.shed_profiles parent_profile
  WHERE parent_profile.tenant_id = p_tenant_id
    AND parent_profile.location_id = p_group_shed_id
  ON CONFLICT (location_id) DO NOTHING;
$$;

CREATE OR REPLACE FUNCTION public.reject_active_location_under_inactive_parent()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  bad_parent boolean;
BEGIN
  IF NEW.status = 'active' AND NEW.parent_location_id IS NOT NULL THEN
    SELECT EXISTS (
      SELECT 1
      FROM public.locations parent
      WHERE parent.tenant_id = NEW.tenant_id
        AND parent.location_id = NEW.parent_location_id
        AND parent.status <> 'active'
    )
    INTO bad_parent;

    IF bad_parent THEN
      RAISE EXCEPTION 'active_location_parent_inactive: tenant %, location %, parent %',
        NEW.tenant_id, NEW.location_id, NEW.parent_location_id;
    END IF;
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS locations_active_parent_trg ON public.locations;
CREATE TRIGGER locations_active_parent_trg
BEFORE INSERT OR UPDATE OF status, parent_location_id
ON public.locations
FOR EACH ROW
EXECUTE FUNCTION public.reject_active_location_under_inactive_parent();

DO $$
DECLARE
  ambiguous_count integer;
BEGIN
  SELECT count(*) INTO ambiguous_count
  FROM (
    SELECT sp.tenant_id, sp.shed_id, sp.normalized_label
    FROM public.shed_partitions sp
    LEFT JOIN (
      SELECT
        gsp.tenant_id,
        gsp.shed_id,
        regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '') AS normalized_label,
        CASE WHEN count(DISTINCT NULLIF(btrim(gsp.source_shed_name), '')) = 1
          THEN max(NULLIF(btrim(gsp.source_shed_name), ''))
          ELSE NULL
        END AS source_shed_name
      FROM public.goat_shed_partitions gsp
      GROUP BY gsp.tenant_id, gsp.shed_id, regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
    ) gsp_source
      ON gsp_source.tenant_id = sp.tenant_id
     AND gsp_source.shed_id = sp.shed_id
     AND gsp_source.normalized_label = sp.normalized_label
    JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
   AND (
     (sp.status = 'active' AND parent.status = 'active')
     OR sp.status = 'retired'
   )
    JOIN public.locations pen
      ON pen.tenant_id = sp.tenant_id
     AND pen.parent_location_id = parent.parent_location_id
     AND pen.location_type = 'shed'
     AND pen.location_id <> sp.shed_id
     AND (
       (sp.status = 'active' AND pen.status = 'active')
       OR (sp.status = 'retired' AND pen.status = 'inactive')
     )
    WHERE (
      lower(pen.name) = lower(operational_location_display(parent.name, sp.partition_label))
      OR lower(pen.name) = lower(operational_location_display(parent.name, sp.normalized_label))
      OR (gsp_source.source_shed_name IS NOT NULL AND lower(pen.name) = lower(gsp_source.source_shed_name))
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
    LEFT JOIN (
      SELECT
        gsp.tenant_id,
        gsp.shed_id,
        regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '') AS normalized_label,
        CASE WHEN count(DISTINCT NULLIF(btrim(gsp.source_shed_name), '')) = 1
          THEN max(NULLIF(btrim(gsp.source_shed_name), ''))
          ELSE NULL
        END AS source_shed_name
      FROM public.goat_shed_partitions gsp
      GROUP BY gsp.tenant_id, gsp.shed_id, regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
    ) gsp_source
      ON gsp_source.tenant_id = sp.tenant_id
     AND gsp_source.shed_id = sp.shed_id
     AND gsp_source.normalized_label = sp.normalized_label
    JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
   AND (
     (sp.status = 'active' AND parent.status = 'active')
     OR sp.status = 'retired'
   )
    JOIN public.locations pen
      ON pen.tenant_id = sp.tenant_id
     AND pen.parent_location_id = parent.parent_location_id
     AND pen.location_type = 'shed'
     AND pen.location_id <> sp.shed_id
     AND (
       (sp.status = 'active' AND pen.status = 'active')
       OR (sp.status = 'retired' AND pen.status = 'inactive')
     )
    WHERE (
      lower(pen.name) = lower(operational_location_display(parent.name, sp.partition_label))
      OR lower(pen.name) = lower(operational_location_display(parent.name, sp.normalized_label))
      OR (gsp_source.source_shed_name IS NOT NULL AND lower(pen.name) = lower(gsp_source.source_shed_name))
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
FROM public.locations parent
JOIN public.locations pen
  ON pen.tenant_id = parent.tenant_id
 AND pen.parent_location_id = parent.parent_location_id
WHERE sp.operational_location_id IS NULL
  AND parent.tenant_id = sp.tenant_id
  AND parent.location_id = sp.shed_id
  AND pen.location_type = 'shed'
  AND pen.location_id <> sp.shed_id
  AND (
    (sp.status = 'active' AND parent.status = 'active')
    OR sp.status = 'retired'
  )
  AND (
    (sp.status = 'active' AND pen.status = 'active')
    OR (sp.status = 'retired' AND pen.status = 'inactive')
  )
  AND (
    lower(pen.name) = lower(operational_location_display(parent.name, sp.partition_label))
    OR lower(pen.name) = lower(operational_location_display(parent.name, sp.normalized_label))
    OR lower(pen.name) = lower((
      SELECT CASE WHEN count(DISTINCT NULLIF(btrim(gsp.source_shed_name), '')) = 1
        THEN max(NULLIF(btrim(gsp.source_shed_name), ''))
        ELSE NULL
      END
      FROM public.goat_shed_partitions gsp
      WHERE gsp.tenant_id = sp.tenant_id
        AND gsp.shed_id = sp.shed_id
        AND regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '') = sp.normalized_label
    ))
  );

-- Create one canonical shed row per catalog partition still missing a real
-- location. Prefer an existing legacy alias display name when one matches the
-- same parent shed inside the same park; otherwise use "Parent - Part N".
-- Retired partitions are still mapped, but their shed rows stay inactive so
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
   AND (
     (sp.status = 'active' AND parent.status = 'active')
     OR sp.status = 'retired'
   )
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
source_names AS (
  SELECT
    gsp.tenant_id,
    gsp.shed_id,
    regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '') AS normalized_label,
    CASE WHEN count(DISTINCT NULLIF(btrim(gsp.source_shed_name), '')) = 1
      THEN max(NULLIF(btrim(gsp.source_shed_name), ''))
      ELSE NULL
    END AS source_shed_name
  FROM public.goat_shed_partitions gsp
  GROUP BY gsp.tenant_id, gsp.shed_id, regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '')
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
    'shed',
    NULL,
    COALESCE(sn.source_shed_name, an.display_name, operational_location_display(parent.name, COALESCE(NULLIF(BTRIM(sp.partition_label), ''), sp.normalized_label))),
    parent.parent_location_id,
    parent.country,
    parent.timezone,
    CASE WHEN sp.status = 'active' THEN 'active' ELSE 'inactive' END,
    COALESCE(an.display_order, COALESCE(sp.display_order, 0)),
    'Created by migration 000152 from shed_partitions parent+partition mapping'
  FROM public.shed_partitions sp
  JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
   AND parent.location_type = 'shed'
  LEFT JOIN alias_names an
    ON an.tenant_id = sp.tenant_id
   AND an.shed_id = sp.shed_id
   AND an.normalized_label = sp.normalized_label
  LEFT JOIN source_names sn
    ON sn.tenant_id = sp.tenant_id
   AND sn.shed_id = sp.shed_id
   AND sn.normalized_label = sp.normalized_label
  WHERE sp.operational_location_id IS NULL
  RETURNING tenant_id, location_id, parent_location_id, name
)
UPDATE public.shed_partitions sp
SET operational_location_id = created.location_id,
    updated_at = now()
FROM created
WHERE sp.tenant_id = created.tenant_id
	  AND created.parent_location_id = (SELECT p.parent_location_id FROM public.locations p WHERE p.tenant_id = sp.tenant_id AND p.location_id = sp.shed_id)
	  AND (
	    lower(created.name) = lower(
	      operational_location_display((SELECT p.name FROM public.locations p WHERE p.tenant_id = sp.tenant_id AND p.location_id = sp.shed_id),
	                                  sp.partition_label)
	    )
	    OR lower(created.name) = lower(
	      operational_location_display((SELECT p.name FROM public.locations p WHERE p.tenant_id = sp.tenant_id AND p.location_id = sp.shed_id),
	                                  sp.normalized_label)
	    )
      OR lower(created.name) = lower((
        SELECT sn.source_shed_name
        FROM source_names sn
        WHERE sn.tenant_id = sp.tenant_id
          AND sn.shed_id = sp.shed_id
          AND sn.normalized_label = sp.normalized_label
      ))
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

-- Validate the bridge points to a real partition shed under the same park as
-- the grouping shed. Active partitions must point to active sheds; retired
-- partitions may point to inactive sheds.
DO $$
DECLARE
  bad_count integer;
BEGIN
  SELECT count(*) INTO bad_count
  FROM public.shed_partitions sp
  JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
  JOIN public.locations pen
    ON pen.tenant_id = sp.tenant_id
   AND pen.location_id = sp.operational_location_id
  WHERE NOT (
      pen.location_type = 'shed'
      AND pen.parent_location_id = parent.parent_location_id
      AND pen.location_id <> sp.shed_id
      AND (
        (sp.status = 'active' AND pen.status = 'active')
        OR (sp.status = 'retired' AND pen.status = 'inactive')
      )
    );

  IF bad_count <> 0 THEN
    RAISE EXCEPTION 'partition_operational_location_mapping_invalid: % rows do not point to valid pen children', bad_count;
  END IF;
END $$;

ALTER TABLE public.shed_partitions
  VALIDATE CONSTRAINT shed_partitions_operational_location_fk;

-- Copy parent operational flags onto mapped partition shed rows. Vaccination/procurement
-- eligibility code reads location_operational_attributes from the goat's exact
-- current_location_id; after this migration that is a partition shed for
-- partitioned goats. Without this copy, a quarantined/ICU/holding parent group
-- could look usable through a newly-created shed that has no attributes row.
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
ON CONFLICT (location_id) DO NOTHING;

-- Partition sheds inherit the grouping shed profile at cutover. After this,
-- vaccination/feed/identity code can read shed_profiles from the exact shed id.
SELECT public.copy_shed_partition_profile(sp.tenant_id, sp.shed_id, sp.operational_location_id)
FROM public.shed_partitions sp
WHERE sp.operational_location_id IS NOT NULL;

-- Backfill partition-specific operational rows so the database itself says the
-- partition location is the shed. The old parent shed remains only in
-- goat_shed_partitions/shed_partitions as the grouping bridge.
WITH exact_partition AS (
  SELECT sp.tenant_id,
         sp.shed_id AS group_shed_id,
         sp.operational_location_id AS exact_shed_id,
         sp.partition_label,
         sp.normalized_label,
         exact.name AS exact_shed_name
  FROM public.shed_partitions sp
  JOIN public.locations exact
    ON exact.tenant_id = sp.tenant_id
   AND exact.location_id = sp.operational_location_id
  WHERE sp.status = 'active'
    AND sp.operational_location_id IS NOT NULL
)
UPDATE public.vaccination_drive_assignments vda
SET shed_id = ep.exact_shed_id,
    physical_shed = COALESCE(NULLIF(ep.exact_shed_name, ''), vda.physical_shed),
    partition_label = 'whole',
    updated_at = now()
FROM exact_partition ep
WHERE vda.tenant_id = ep.tenant_id
  AND vda.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(vda.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.vaccination_eligibility_rollups ver
SET shed_id = ep.exact_shed_id,
    partition_label = NULL,
    updated_at = now()
FROM exact_partition ep
WHERE ver.tenant_id = ep.tenant_id
  AND ver.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(ver.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.verification_items vi
SET shed_id = ep.exact_shed_id,
    partition_label = NULL,
    updated_at = now(),
    row_version = vi.row_version + 1
FROM exact_partition ep
WHERE vi.tenant_id = ep.tenant_id
  AND vi.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(vi.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.health_cases hc
SET shed_id = ep.exact_shed_id,
    partition_label = NULL,
    updated_at = now(),
    row_version = hc.row_version + 1
FROM exact_partition ep
WHERE hc.tenant_id = ep.tenant_id
  AND hc.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(hc.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.feed_transport_tasks ftt
SET shed_id = ep.exact_shed_id,
    partition_label = '',
    updated_at = now(),
    row_version = ftt.row_version + 1
FROM exact_partition ep
WHERE ftt.tenant_id = ep.tenant_id
  AND ftt.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(ftt.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

ALTER TABLE public.feed_distribution_completions
  DROP CONSTRAINT IF EXISTS feed_distribution_completions_weight_proof_check;

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.feed_distribution_completions fdc
SET shed_id = ep.exact_shed_id,
    partition_label = '',
    updated_at = now(),
    row_version = fdc.row_version + 1
FROM exact_partition ep
WHERE fdc.tenant_id = ep.tenant_id
  AND fdc.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fdc.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

ALTER TABLE public.feed_distribution_completions
  ADD CONSTRAINT feed_distribution_completions_weight_proof_check
  CHECK (
    status <> 'pending_verification'
    OR (feed_weight_proof_ref IS NOT NULL AND btrim(feed_weight_proof_ref) <> '')
  ) NOT VALID;

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.feed_packing_completions fpc
SET shed_id = ep.exact_shed_id,
    partition_label = '',
    updated_at = now(),
    row_version = fpc.row_version + 1
FROM exact_partition ep
WHERE fpc.tenant_id = ep.tenant_id
  AND fpc.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fpc.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.feed_direction_issue_rows fdir
SET shed_id = ep.exact_shed_id,
    partition_label = '',
    updated_at = now()
FROM exact_partition ep
WHERE fdir.tenant_id = ep.tenant_id
  AND fdir.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fdir.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.feed_experiment_config fec
SET shed_id = ep.exact_shed_id,
    partition_label = '',
    updated_at = now()
FROM exact_partition ep
WHERE fec.tenant_id = ep.tenant_id
  AND fec.shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fec.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.shifting_events se
SET source_shed_id = ep.exact_shed_id,
    source_partition_label = NULL,
    updated_at = now(),
    row_version = se.row_version + 1
FROM exact_partition ep
WHERE se.tenant_id = ep.tenant_id
  AND se.source_shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(se.source_partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.shifting_events se
SET destination_shed_id = ep.exact_shed_id,
    destination_partition_label = NULL,
    updated_at = now(),
    row_version = se.row_version + 1
FROM exact_partition ep
WHERE se.tenant_id = ep.tenant_id
  AND se.destination_shed_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(se.destination_partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
INSERT INTO public.feed_shed_factors (
  tenant_id, park_id, shed_id, feed_item_label, multiplier, valid_from, valid_to, created_by, created_at, updated_at
)
SELECT fsf.tenant_id, fsf.park_id, ep.exact_shed_id, fsf.feed_item_label, fsf.multiplier,
       fsf.valid_from, fsf.valid_to, fsf.created_by, fsf.created_at, now()
FROM public.feed_shed_factors fsf
JOIN exact_partition ep
  ON ep.tenant_id = fsf.tenant_id
 AND ep.group_shed_id = fsf.shed_id
ON CONFLICT (tenant_id, park_id, shed_id, feed_item_key, valid_from) DO UPDATE
SET multiplier = EXCLUDED.multiplier,
    valid_to = EXCLUDED.valid_to,
    updated_at = now();

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label
  FROM public.shed_partitions
  WHERE status = 'active'
    AND operational_location_id IS NOT NULL
)
UPDATE public.weighing_campaign_sheds wcs
SET location_id = ep.exact_shed_id,
    location_type = 'shed',
    display_name = exact.name,
    partition_label = NULL,
    updated_at = now()
FROM exact_partition ep
JOIN public.locations exact
  ON exact.tenant_id = ep.tenant_id
 AND exact.location_id = ep.exact_shed_id
WHERE wcs.tenant_id = ep.tenant_id
  AND wcs.location_id = ep.group_shed_id
  AND ep.normalized_label = regexp_replace(lower(btrim(COALESCE(wcs.partition_label, 'whole'))), '^part[[:space:]]+', '')
  AND ep.normalized_label <> 'whole';

UPDATE public.weighing_work_items wwi
SET shed_location_id = wcs.location_id,
    updated_at = now()
FROM public.weighing_campaign_sheds wcs
WHERE wwi.tenant_id = wcs.tenant_id
  AND wwi.campaign_shed_id = wcs.campaign_shed_id
  AND wwi.shed_location_id IS DISTINCT FROM wcs.location_id;

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
    AND sp.operational_location_id IS NULL;

  IF unmapped_count <> 0 THEN
    RAISE EXCEPTION 'live_partitioned_goats_unmapped: % live goats have no operational shed mapping', unmapped_count;
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
  display_partition_label text;
  source_shed_name text;
BEGIN
  IF NEW.status NOT IN ('active', 'retired') THEN
    RETURN NEW;
  END IF;

  display_partition_label := NULLIF(NEW.partition_label, '');
  IF display_partition_label IS NULL THEN
    display_partition_label := NEW.normalized_label;
  END IF;

  SELECT CASE WHEN count(DISTINCT NULLIF(btrim(gsp.source_shed_name), '')) = 1
    THEN max(NULLIF(btrim(gsp.source_shed_name), ''))
    ELSE NULL
  END
  INTO source_shed_name
  FROM public.goat_shed_partitions gsp
  WHERE gsp.tenant_id = NEW.tenant_id
    AND gsp.shed_id = NEW.shed_id
    AND regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '') = NEW.normalized_label;

  SELECT *
  INTO parent_row
  FROM public.locations
  WHERE tenant_id = NEW.tenant_id
    AND location_id = NEW.shed_id
    AND location_type = 'shed'
    AND (NEW.status <> 'active' OR status = 'active')
  FOR SHARE;

  IF NOT FOUND THEN
    RAISE EXCEPTION 'shed_partition_parent_shed_missing: tenant %, shed %', NEW.tenant_id, NEW.shed_id;
  END IF;

  IF NEW.status = 'retired' THEN
    IF NEW.operational_location_id IS NULL THEN
      RETURN NEW;
    END IF;

    PERFORM pg_advisory_xact_lock(hashtextextended(shed_partition_lock_key(NEW.tenant_id, NEW.shed_id, 'whole'), 150));
    PERFORM pg_advisory_xact_lock(hashtextextended(shed_partition_lock_key(NEW.tenant_id, NEW.shed_id, NEW.normalized_label), 150));

    IF TG_OP = 'UPDATE'
      AND OLD.status = 'active'
      AND NEW.operational_location_id IS NOT DISTINCT FROM OLD.operational_location_id THEN
      IF EXISTS (
        SELECT 1
        FROM public.goats g
        WHERE g.tenant_id = NEW.tenant_id
          AND g.current_location_id = NEW.operational_location_id
          AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
          AND g.merged_into_goat_id IS NULL
      ) THEN
        RAISE EXCEPTION 'shed_partition_operational_location_in_use: tenant %, shed %, partition %',
          NEW.tenant_id, NEW.shed_id, NEW.partition_label;
      END IF;

      UPDATE public.locations pen
      SET status = 'inactive',
          updated_at = now()
      WHERE pen.tenant_id = NEW.tenant_id
        AND pen.location_id = NEW.operational_location_id
        AND pen.parent_location_id = parent_row.parent_location_id
        AND pen.location_type = 'shed'
        AND pen.location_id <> NEW.shed_id
        AND pen.status = 'active';
    END IF;

    SELECT EXISTS (
      SELECT 1
      FROM public.locations pen
      WHERE pen.tenant_id = NEW.tenant_id
        AND pen.location_id = NEW.operational_location_id
        AND pen.parent_location_id = parent_row.parent_location_id
        AND pen.location_type = 'shed'
        AND pen.location_id <> NEW.shed_id
        AND pen.status = 'inactive'
    )
    INTO mapped_valid;

    IF mapped_valid THEN
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
        NEW.operational_location_id,
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

      PERFORM public.copy_shed_partition_profile(NEW.tenant_id, NEW.shed_id, NEW.operational_location_id);

      RETURN NEW;
    END IF;

    RAISE EXCEPTION 'shed_partition_operational_location_invalid: tenant %, shed %, partition %',
      NEW.tenant_id, NEW.shed_id, NEW.partition_label;
  END IF;

  PERFORM pg_advisory_xact_lock(hashtextextended(shed_partition_lock_key(NEW.tenant_id, NEW.shed_id, 'whole'), 150));
  PERFORM pg_advisory_xact_lock(hashtextextended(shed_partition_lock_key(NEW.tenant_id, NEW.shed_id, NEW.normalized_label), 150));

  IF TG_OP = 'INSERT' OR OLD.status <> 'active' THEN
    IF EXISTS (
      SELECT 1
      FROM public.goats g
      WHERE g.tenant_id = NEW.tenant_id
        AND g.current_location_id = NEW.shed_id
        AND COALESCE(g.shed_group_id, g.shed_id) = NEW.shed_id
        AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
        AND g.merged_into_goat_id IS NULL
    ) THEN
      RAISE EXCEPTION 'shed_partition_activation_bare_residents: tenant %, shed %, partition %',
        NEW.tenant_id, NEW.shed_id, NEW.partition_label;
    END IF;
  END IF;

  IF NEW.operational_location_id IS NOT NULL THEN
    IF TG_OP = 'UPDATE'
      AND NEW.operational_location_id IS NOT DISTINCT FROM OLD.operational_location_id
      AND (
        NEW.shed_id IS DISTINCT FROM OLD.shed_id
        OR NEW.normalized_label IS DISTINCT FROM OLD.normalized_label
        OR NEW.partition_label IS DISTINCT FROM OLD.partition_label
      )
      AND EXISTS (
        SELECT 1
        FROM public.goats g
        WHERE g.tenant_id = NEW.tenant_id
          AND g.current_location_id = OLD.operational_location_id
          AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
          AND g.merged_into_goat_id IS NULL
      ) THEN
      RAISE EXCEPTION 'shed_partition_operational_location_in_use: tenant %, shed %, partition %',
        NEW.tenant_id, OLD.shed_id, OLD.partition_label;
    END IF;

    IF TG_OP = 'UPDATE'
      AND NEW.operational_location_id IS NOT DISTINCT FROM OLD.operational_location_id
      AND (
        NEW.shed_id IS DISTINCT FROM OLD.shed_id
        OR NEW.normalized_label IS DISTINCT FROM OLD.normalized_label
        OR NEW.partition_label IS DISTINCT FROM OLD.partition_label
      ) THEN
      NEW.operational_location_id := NULL;
    END IF;
  END IF;

  IF NEW.operational_location_id IS NOT NULL THEN
    SELECT EXISTS (
      SELECT 1
      FROM public.locations pen
      WHERE pen.tenant_id = NEW.tenant_id
        AND pen.location_id = NEW.operational_location_id
        AND pen.parent_location_id = parent_row.parent_location_id
        AND pen.location_type = 'shed'
        AND pen.location_id <> NEW.shed_id
        AND pen.status = 'active'
    )
    INTO mapped_valid;

    IF mapped_valid THEN
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
        NEW.operational_location_id,
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

      PERFORM public.copy_shed_partition_profile(NEW.tenant_id, NEW.shed_id, NEW.operational_location_id);

      RETURN NEW;
    END IF;

    RAISE EXCEPTION 'shed_partition_operational_location_invalid: tenant %, shed %, partition %',
      NEW.tenant_id, NEW.shed_id, NEW.partition_label;
  END IF;

  SELECT count(*), (array_agg(pen.location_id ORDER BY pen.location_id))[1]
  INTO candidate_count, candidate_id
  FROM public.locations pen
  WHERE pen.tenant_id = NEW.tenant_id
    AND pen.parent_location_id = parent_row.parent_location_id
    AND pen.location_type = 'shed'
    AND pen.location_id <> NEW.shed_id
    AND pen.status = 'active'
    AND (
      lower(pen.name) = lower(operational_location_display(parent_row.name, NEW.partition_label))
      OR lower(pen.name) = lower(operational_location_display(parent_row.name, display_partition_label))
      OR (source_shed_name IS NOT NULL AND lower(pen.name) = lower(source_shed_name))
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
      'shed',
      NULL,
      COALESCE(source_shed_name, operational_location_display(parent_row.name, display_partition_label)),
      parent_row.parent_location_id,
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

  PERFORM public.copy_shed_partition_profile(NEW.tenant_id, NEW.shed_id, NEW.operational_location_id);

  IF TG_OP = 'UPDATE'
    AND OLD.operational_location_id IS NOT NULL
    AND OLD.operational_location_id IS DISTINCT FROM NEW.operational_location_id THEN
    IF EXISTS (
      SELECT 1
      FROM public.goats g
      JOIN public.goat_shed_partitions gsp
        ON gsp.tenant_id = g.tenant_id
       AND gsp.goat_id = g.goat_id
      WHERE g.tenant_id = NEW.tenant_id
        AND g.current_location_id = OLD.operational_location_id
        AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
        AND g.merged_into_goat_id IS NULL
        AND gsp.shed_id = NEW.shed_id
        AND regexp_replace(lower(btrim(gsp.partition_label)), '^part[[:space:]]+', '') = NEW.normalized_label
    ) THEN
      RAISE EXCEPTION 'shed_partition_operational_location_in_use: tenant %, shed %, partition %',
        NEW.tenant_id, NEW.shed_id, NEW.partition_label;
    END IF;
  END IF;

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
DROP TRIGGER IF EXISTS locations_active_parent_trg ON public.locations;
DROP TRIGGER IF EXISTS shed_partitions_operational_location_trg ON public.shed_partitions;
DROP FUNCTION IF EXISTS public.operational_location_display(text, text);
DROP FUNCTION IF EXISTS public.shed_partition_lock_key(uuid, uuid, text);
DROP FUNCTION IF EXISTS public.copy_shed_partition_profile(uuid, uuid, uuid);
DROP FUNCTION IF EXISTS public.reject_active_location_under_inactive_parent();
DROP FUNCTION IF EXISTS public.ensure_shed_partition_operational_location();

WITH exact_partition AS (
  SELECT sp.tenant_id,
         sp.shed_id AS group_shed_id,
         sp.operational_location_id AS exact_shed_id,
         sp.normalized_label,
         sp.partition_label,
         parent.name AS group_shed_name
  FROM public.shed_partitions sp
  JOIN public.locations parent
    ON parent.tenant_id = sp.tenant_id
   AND parent.location_id = sp.shed_id
  WHERE sp.operational_location_id IS NOT NULL
)
UPDATE public.vaccination_drive_assignments vda
SET shed_id = ep.group_shed_id,
    physical_shed = COALESCE(NULLIF(ep.group_shed_name, ''), vda.physical_shed),
    partition_label = ep.partition_label,
    updated_at = now()
FROM exact_partition ep
WHERE vda.tenant_id = ep.tenant_id
  AND vda.shed_id = ep.exact_shed_id
  AND (
    COALESCE(NULLIF(btrim(vda.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(vda.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label, partition_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.vaccination_eligibility_rollups ver
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now()
FROM exact_partition ep
WHERE ver.tenant_id = ep.tenant_id
  AND ver.shed_id = ep.exact_shed_id
  AND (
    ver.partition_label IS NULL
    OR COALESCE(NULLIF(btrim(ver.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(ver.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label, partition_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.verification_items vi
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now(),
    row_version = vi.row_version + 1
FROM exact_partition ep
WHERE vi.tenant_id = ep.tenant_id
  AND vi.shed_id = ep.exact_shed_id
  AND (
    vi.partition_label IS NULL
    OR COALESCE(NULLIF(btrim(vi.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(vi.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label, partition_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.health_cases hc
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now(),
    row_version = hc.row_version + 1
FROM exact_partition ep
WHERE hc.tenant_id = ep.tenant_id
  AND hc.shed_id = ep.exact_shed_id
  AND (
    hc.partition_label IS NULL
    OR COALESCE(NULLIF(btrim(hc.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(hc.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.feed_transport_tasks ftt
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now(),
    row_version = ftt.row_version + 1
FROM exact_partition ep
WHERE ftt.tenant_id = ep.tenant_id
  AND ftt.shed_id = ep.exact_shed_id
  AND (
    COALESCE(NULLIF(btrim(ftt.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(ftt.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

ALTER TABLE public.feed_distribution_completions
  DROP CONSTRAINT IF EXISTS feed_distribution_completions_weight_proof_check;

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.feed_distribution_completions fdc
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now(),
    row_version = fdc.row_version + 1
FROM exact_partition ep
WHERE fdc.tenant_id = ep.tenant_id
  AND fdc.shed_id = ep.exact_shed_id
  AND (
    COALESCE(NULLIF(btrim(fdc.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fdc.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

ALTER TABLE public.feed_distribution_completions
  ADD CONSTRAINT feed_distribution_completions_weight_proof_check
  CHECK (
    status <> 'pending_verification'
    OR (feed_weight_proof_ref IS NOT NULL AND btrim(feed_weight_proof_ref) <> '')
  ) NOT VALID;

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.feed_packing_completions fpc
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now(),
    row_version = fpc.row_version + 1
FROM exact_partition ep
WHERE fpc.tenant_id = ep.tenant_id
  AND fpc.shed_id = ep.exact_shed_id
  AND (
    COALESCE(NULLIF(btrim(fpc.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fpc.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.shifting_events se
SET source_shed_id = ep.group_shed_id,
    source_partition_label = ep.partition_label,
    updated_at = now(),
    row_version = se.row_version + 1
FROM exact_partition ep
WHERE se.tenant_id = ep.tenant_id
  AND se.source_shed_id = ep.exact_shed_id
  AND (
    se.source_partition_label IS NULL
    OR COALESCE(NULLIF(btrim(se.source_partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(se.source_partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.shifting_events se
SET destination_shed_id = ep.group_shed_id,
    destination_partition_label = ep.partition_label,
    updated_at = now(),
    row_version = se.row_version + 1
FROM exact_partition ep
WHERE se.tenant_id = ep.tenant_id
  AND se.destination_shed_id = ep.exact_shed_id
  AND (
    se.destination_partition_label IS NULL
    OR COALESCE(NULLIF(btrim(se.destination_partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(se.destination_partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.feed_direction_issue_rows fdir
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now()
FROM exact_partition ep
WHERE fdir.tenant_id = ep.tenant_id
  AND fdir.shed_id = ep.exact_shed_id
  AND (
    COALESCE(NULLIF(btrim(fdir.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fdir.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, partition_label, normalized_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.feed_experiment_config fec
SET shed_id = ep.group_shed_id,
    partition_label = ep.partition_label,
    updated_at = now()
FROM exact_partition ep
WHERE fec.tenant_id = ep.tenant_id
  AND fec.shed_id = ep.exact_shed_id
  AND (
    COALESCE(NULLIF(btrim(fec.partition_label), ''), 'whole') = 'whole'
    OR ep.normalized_label = regexp_replace(lower(btrim(COALESCE(fec.partition_label, 'whole'))), '^part[[:space:]]+', '')
  )
  AND ep.normalized_label <> 'whole';

WITH exact_partition AS (
  SELECT tenant_id, shed_id AS group_shed_id, operational_location_id AS exact_shed_id, normalized_label, partition_label
  FROM public.shed_partitions
  WHERE operational_location_id IS NOT NULL
)
UPDATE public.weighing_campaign_sheds wcs
SET location_id = ep.group_shed_id,
    location_type = 'shed',
    partition_label = ep.partition_label,
    updated_at = now()
FROM exact_partition ep
WHERE wcs.tenant_id = ep.tenant_id
  AND wcs.location_id = ep.exact_shed_id
  AND ep.normalized_label <> 'whole';

ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_operational_location_fk;

DELETE FROM public.location_operational_attributes loa
USING public.locations pen
WHERE loa.tenant_id = pen.tenant_id
  AND loa.location_id = pen.location_id
  AND pen.location_type = 'shed'
  AND pen.operational_notes IN (
    'Created by migration 000152 from shed_partitions parent+partition mapping',
    'Created from active shed_partitions row'
  );

DELETE FROM public.shed_profiles sp
USING public.locations loc
WHERE sp.tenant_id = loc.tenant_id
  AND sp.location_id = loc.location_id
  AND loc.location_type = 'shed'
  AND loc.operational_notes IN (
    'Created by migration 000152 from shed_partitions parent+partition mapping',
    'Created from active shed_partitions row'
  );

DELETE FROM public.locations pen
WHERE pen.location_type = 'shed'
  AND pen.operational_notes IN (
    'Created by migration 000152 from shed_partitions parent+partition mapping',
    'Created from active shed_partitions row'
  );

DROP INDEX IF EXISTS public.shed_partitions_operational_location_unique;

ALTER TABLE public.shed_partitions
  DROP COLUMN IF EXISTS operational_location_id;
