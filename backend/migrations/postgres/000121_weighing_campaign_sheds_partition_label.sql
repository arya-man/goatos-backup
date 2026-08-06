-- seed-fixture-guard:ignore: weighing_campaign_sheds is weighing-owned and free-flow; this adds a partition_label column + backfill from the locations/shed_partitions catalog. It touches no goat, vaccination, protocol or SOP table and changes no seed-data contract, fixture shape or source column.
-- +goose Up
-- weighing_campaign_sheds.partition_label: snapshot the partition label when the planner
-- assigns a shed to a campaign.
--
-- WEIGHING ISOLATION: This column is populated from the authoritative shed_partitions catalog
-- (migration 000112), an ORG-scoped table like locations/workforce_members/user_scope_grants.
-- Never joined to goat_shed_partitions (which is per-goat and would reveal animal location),
-- vaccination_drive_assignments, or any other module's schema.
--
-- Maintainer decision (2026-08-06): shed_partitions is added to the weighing read allowlist
-- because it is an exact catalog of partitions that physically exist (including empty ones like
-- CBE Yashoda 5), keyed by location ID with no per-animal data. This replaces the name-parsing
-- inference path with authoritative resolution and avoids the risk of NULL labels when names
-- don't match the convention. It is strictly ORG-scoped (locations → shed_partitions), not
-- animal-scoped (goat_shed_partitions remains banned).
--
-- Backfill: Resolve partition_label from shed_partitions where available (exact catalog lookup
-- on location_id). For rows the catalog cannot resolve (e.g., bulk-created sheds without catalog
-- entries), fall back to name-parsing inference. Unresolvable -> NULL, never invent.

ALTER TABLE weighing_campaign_sheds ADD COLUMN IF NOT EXISTS partition_label text;

-- Backfill: PRIMARY PATH uses the shed_partitions catalog (migration 000112).
-- For each campaign shed, look up the location_id in shed_partitions and take partition_label
-- directly. This gives exact resolution and includes empty partitions.
UPDATE weighing_campaign_sheds wcs
SET partition_label = sp.partition_label
FROM shed_partitions sp
WHERE wcs.location_id = sp.shed_id
  AND wcs.tenant_id = sp.tenant_id
  AND wcs.partition_label IS NULL;  -- only backfill empty rows

-- Backfill: FALLBACK PATH for rows not found in the shed_partitions catalog.
-- This handles legacy data or bulk-created sheds where the catalog entry may not exist yet.
-- Extract partition_label from the location_id's name when it refers to a
-- partition-bearing location row (status='inactive', name like 'Shed Name Label').
-- Regular active sheds (location_type='shed', status='active') are non-partitioned: leave NULL.
--
-- DETERMINISM: Use LATERAL subquery with DISTINCT ON + ORDER BY length(shed.name) DESC to ensure
-- the longest-matching parent is selected. This prevents ambiguity (e.g., "Godel 1 - Part 3"
-- matches both "Godel" and "Godel 1"; we want "Godel 1").
-- SEPARATOR BOUNDARY: Require explicit separator (space or " - ") after parent name to prevent
-- prefix-only false matches (e.g., "Castro1" does not match parent "Castro").
UPDATE weighing_campaign_sheds wcs
SET partition_label = CASE
    WHEN loc.status = 'active' AND loc.location_type = 'shed' THEN NULL
    WHEN loc.status = 'inactive' AND loc.location_type = 'shed' AND shed_match.parent_name IS NOT NULL THEN
        -- This is a partition alias. Extract the label from the location name by:
        -- 1. Find the parent shed (active, same park, name is a prefix of this location's name + separator)
        -- 2. Remove the parent shed name and separator from the partition-bearing location name
        -- 3. Trim and normalize the remainder as the partition_label
        btrim(regexp_replace(
            substr(loc.name, length(shed_match.parent_name) + 1),
            '^[[:space:]]*-?[[:space:]]*', ''
        ))
    ELSE NULL
END
FROM locations loc
LEFT JOIN LATERAL (
    SELECT DISTINCT ON (loc.location_id) shed.location_id AS shed_id, shed.name AS parent_name
    FROM locations shed
    WHERE shed.tenant_id = loc.tenant_id
      AND shed.parent_location_id = loc.parent_location_id  -- same park
      AND shed.location_type = 'shed'
      AND shed.status = 'active'
      AND shed.retired_at IS NULL
      AND shed.name <> loc.name
      AND ((loc.name LIKE shed.name || ' %') OR (loc.name LIKE shed.name || ' - %'))
    ORDER BY loc.location_id, length(shed.name) DESC
) shed_match ON true
WHERE wcs.location_id = loc.location_id
  AND wcs.partition_label IS NULL  -- only backfill empty rows (catalog path missed these)
  AND loc.location_type = 'shed';   -- only sheds (active or inactive partition aliases)

-- Verification query: inspect backfilled rows to ensure determinism and correctness.
-- This query surfaces any rows where the backfill produced suspect values.
-- Asserts: no label contains " - Part " twice; no label equals the whole alias name; no empty label after trim.
-- Active sheds with partition-like names (e.g., "Gandhi 1" active shed) correctly receive NULL.
DO $$
  DECLARE
    bad_rows INT;
    bad_row_ids TEXT;
    catalog_coverage INT;
    fallback_coverage INT;
  BEGIN
    SELECT COUNT(*), STRING_AGG(wcs.campaign_shed_id::TEXT, ', ')
    INTO bad_rows, bad_row_ids
    FROM weighing_campaign_sheds wcs
    JOIN locations loc ON wcs.location_id = loc.location_id
    WHERE loc.location_type = 'shed'
      AND wcs.partition_label IS NOT NULL
      AND (
        wcs.partition_label LIKE '%' || ' - Part ' || '% - Part %'  -- label contains " - Part " twice
        OR (loc.status = 'inactive' AND wcs.partition_label = loc.name)  -- label equals whole alias name
        OR wcs.partition_label ~ '^\s*$'  -- empty after trim
      );
    IF bad_rows > 0 THEN
      RAISE WARNING 'Backfill verification FAILED: % malformed rows (campaign_shed_ids: %)', bad_rows, bad_row_ids;
    ELSE
      RAISE NOTICE 'Backfill verification PASSED: no malformed partition_labels detected.';
    END IF;

    -- Coverage report: count how many rows were resolved via catalog vs fallback path
    SELECT COUNT(*)
    INTO catalog_coverage
    FROM weighing_campaign_sheds wcs
    JOIN shed_partitions sp ON wcs.location_id = sp.shed_id AND wcs.tenant_id = sp.tenant_id
    WHERE wcs.partition_label IS NOT NULL;

    SELECT COUNT(*)
    INTO fallback_coverage
    FROM weighing_campaign_sheds wcs
    JOIN locations loc ON wcs.location_id = loc.location_id
    WHERE wcs.partition_label IS NOT NULL
      AND NOT EXISTS (
        SELECT 1 FROM shed_partitions sp
        WHERE wcs.location_id = sp.shed_id AND wcs.tenant_id = sp.tenant_id
      );

    RAISE NOTICE 'Backfill coverage: % from catalog, % from fallback path', catalog_coverage, fallback_coverage;
  END
$$;

-- Add index for reads by campaign_shed_id (common in reads) and by partition_label
-- when filtering campaign sheds by partition within a shed.
CREATE INDEX IF NOT EXISTS weighing_campaign_sheds_campaign_partition_idx
  ON weighing_campaign_sheds (tenant_id, campaign_id, location_id, COALESCE(partition_label, ''));

-- +goose Down
DROP INDEX IF EXISTS weighing_campaign_sheds_campaign_partition_idx;
ALTER TABLE weighing_campaign_sheds DROP COLUMN IF EXISTS partition_label;
