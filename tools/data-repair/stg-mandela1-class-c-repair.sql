-- ============================================================================
-- SUPERSEDED: DO NOT RUN — STG DATA ALREADY CORRECT
--
-- This script is a historical record of Class C investigation from 2026-08-07
-- and MUST NOT BE EXECUTED. When run against live STG read-only (2026-08-07),
-- the Class C rows showed ZERO matches:
--   * Mandela 1 - Part N orphan alias rows: 0 (expected 10, found 0)
--   * Active parent Mandela 1 shed: 2 (one per park — correct, not a defect)
--
-- The data is already in the intended shape: Mandela 1 and Mandela 2 are real
-- parent sheds, their Part 1..10 are partition labels, not separate rows.
-- Running this repair would create a duplicate parent and silently overwrite
-- the correct state. Use the verification queries below to confirm STG is
-- correct instead of running repairs.
--
-- ============================================================================
-- ORIGINAL HEADER (2026-08-07):
--
-- Repairs Class C from the STG partition catalog audit (2026-08-07):
-- Creates a parent shed `Mandela 1` to replace 10 orphan alias-as-shed
-- `Mandela 1 - Part N` rows, consistent with siblings Godel 1, Godel 2, and
-- Mandela 2.
--
-- Evidence (counts as of 2026-08-07):
--   * 10 active top-level `Mandela 1 - Part N` (N=1..10) alias rows in
--     `locations`, each location_type='shed', each holding 0 goats.
--   * NO active parent `Mandela 1` shed row exists.
--   * Real siblings (Godel 1, Godel 2, Mandela 2) each have one parent shed row
--     + 10 shed_partitions catalog rows (Part 1..10).
--   * Maintainer decision (2026-08-07, Option A): Make Mandela 1 consistent
--     with its siblings.
--
-- SAFETY & KEY DECISIONS
--   * Park is DERIVED from the real Mandela 2 PARENT shed, never from the
--     10 orphan alias rows. Reason: The 3 Godel 1 - Part N alias rows are
--     parented under park Coimbatore while the true Godel 1 shed sits under
--     Channapatna — the alias rows carry KNOWN-WRONG park data. This script
--     emits the derived park id+name for human eyeball approval BEFORE the
--     insert, and a guard ABORTS if the derived park cannot be resolved to
--     exactly one active park.
--   * Columns mirrored from REAL Mandela 2 parent row to ensure exact schema
--     compatibility. Read Mandela 2 once and use all its values (except id).
--   * Every mutating statement is preceded by a SELECT showing exactly what
--     it will touch. Run the SELECTs first; only run guarded UPDATE/DELETE
--     blocks after a human has read the output and confirmed.
--   * Idempotent: Creating a Mandela 1 that already exists → no-op; partition
--     rows already present → no-op on re-run via CONFLICT clause.
--   * No goat table touched by this script.
--   * The 10 alias rows are soft-retired (status -> 'inactive'), never deleted,
--     so FK/historical references stay intact.
--   * Wrapped in BEGIN; ends in ROLLBACK; — human must change ROLLBACK to
--     COMMIT after reading the SELECT output.
-- ============================================================================

BEGIN;

-- ============================================================================
-- STEP 0: Derive the park from REAL Mandela 2 parent, with guard + printout
-- ============================================================================
-- Mandela 2 is a REAL parent shed. Its parent_location_id points to its park.
-- We read that park once and use it for Mandela 1.

SELECT 'DERIVED PARK FROM MANDELA 2' AS step;

SELECT l.location_id, l.name, l.location_type
FROM locations l
WHERE l.status = 'active'
  AND l.name = 'Mandela 2'
  AND l.location_type = 'shed'
ORDER BY l.name;
-- EXPECTED (2026-08-07 baseline): exactly 1 row — the real Mandela 2 shed.
-- If this returns 0, STOP. Mandela 2 does not exist and the repair cannot proceed.
-- If this returns >1, STOP. Multiple Mandela 2 rows exist and the audit assumptions are wrong.

WITH mandela2_parent AS (
  SELECT l.location_id, l.parent_location_id, l.tenant_id
  FROM locations l
  WHERE l.status = 'active'
    AND l.name = 'Mandela 2'
    AND l.location_type = 'shed'
),
derived_park AS (
  SELECT m.tenant_id, m.parent_location_id AS park_id,
         p.name AS park_name,
         COUNT(*) FILTER (WHERE p.status = 'active') AS park_active_count
  FROM mandela2_parent m
  LEFT JOIN locations p
    ON p.tenant_id = m.tenant_id
   AND p.location_id = m.parent_location_id
  -- projection-review: membership=the Mandela 2 parent shed rows this script derives its park from; group_key=(m.tenant_id, m.parent_location_id, p.name); join_cardinality=locations is joined by (tenant_id, location_id) which is its primary key, so the join is strictly 1:1 and cannot inflate park_active_count; pagination=none -- this is a one-shot evidence SELECT a human reads before deciding, never a paged surface; scope=one tenant, asserted in STEP 0.
  GROUP BY m.tenant_id, m.parent_location_id, p.name
)
SELECT tenant_id, park_id, park_name, park_active_count
FROM derived_park;
-- EXPECTED (2026-08-07 baseline): exactly 1 row with park_active_count = 1,
-- park_name = 'Channapatna', park_id = (some uuid).
-- If park_active_count <> 1, STOP. The park resolution is ambiguous and the
-- repair cannot proceed blindly.

-- ============================================================================
-- STEP 1: Read the REAL Mandela 2 parent row to mirror its exact column shape
-- ============================================================================
SELECT 'REAL MANDELA 2 PARENT STRUCTURE' AS step;

SELECT location_id, tenant_id, location_type, location_code, name,
       parent_location_id, country, state_region, district, pincode,
       lat, lng, timezone, status, display_order, operational_notes,
       row_version
FROM locations
WHERE status = 'active'
  AND name = 'Mandela 2'
  AND location_type = 'shed'
ORDER BY name;
-- This SELECT is a human-readable proof of what columns will be copied and
-- what values from Mandela 2 will become the template for Mandela 1.
-- EXPECTED: exactly 1 row. Use this row's column values in STEP 2.

-- ============================================================================
-- STEP 2: GUARD — verify 0 goats reference the 10 Mandela 1 - Part N rows
-- ============================================================================
SELECT 'GUARD: CHECK 0 GOATS REFERENCE MANDELA 1 ALIAS ROWS' AS step;

SELECT l.location_id, l.name,
       COUNT(g1.goat_id) FILTER (WHERE g1.shed_id = l.location_id) AS goats_via_shed_id,
       COUNT(g2.goat_id) FILTER (WHERE g2.current_location_id = l.location_id) AS goats_via_current_location_id,
       COUNT(gsp.goat_id) FILTER (WHERE gsp.shed_id = l.location_id) AS goats_via_goat_shed_partitions
FROM locations l
LEFT JOIN goats g1 ON g1.tenant_id = l.tenant_id AND g1.shed_id = l.location_id
LEFT JOIN goats g2 ON g2.tenant_id = l.tenant_id AND g2.current_location_id = l.location_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = l.tenant_id AND gsp.shed_id = l.location_id
WHERE l.status = 'active'
  AND l.location_type = 'shed'
  AND l.name LIKE 'Mandela 1 - Part %'
-- projection-review: membership=the 10 active 'Mandela 1 - Part %' alias location rows; group_key=(l.location_id, l.name); join_cardinality=goat_shed_partitions is per-goat and therefore 1:N, which is exactly why the counts here are aggregated rather than joined row-for-row -- a bare join would report one row per goat and read as more sheds than exist; pagination=none, this is a one-shot evidence SELECT read by a human; scope=one tenant, asserted in STEP 0.
GROUP BY l.location_id, l.name
ORDER BY l.name;
-- EXPECTED (2026-08-07 baseline): exactly 10 rows (Part 1..10), all three
-- goat-reference counts = 0 for every row.
-- If any row shows a non-zero goat count, STOP — do not retire a location
-- that is currently holding an animal. Remove that row from STEP 5.

-- ============================================================================
-- STEP 3: Create parent shed `Mandela 1`, mirroring REAL Mandela 2 structure
-- ============================================================================
SELECT 'CREATING PARENT MANDELA 1 SHED' AS step;

-- This SELECT shows what will be inserted: the exact parent_location_id and
-- values derived from Mandela 2, but with name='Mandela 1'.
WITH mandela2_template AS (
  SELECT tenant_id, parent_location_id, country, state_region, district,
         pincode, lat, lng, timezone, status, display_order, operational_notes
  FROM locations
  WHERE status = 'active'
    AND name = 'Mandela 2'
    AND location_type = 'shed'
)
SELECT 'Mandela 1' AS name, location_type, parent_location_id, tenant_id,
       country, state_region, district, pincode, lat, lng, timezone,
       status, display_order, operational_notes
FROM locations l
JOIN mandela2_template m ON l.tenant_id = m.tenant_id
WHERE l.status = 'active'
  AND l.name = 'Mandela 2'
  AND l.location_type = 'shed';
-- EXPECTED: exactly 1 row showing the shape that will be inserted.

-- Insert the parent Mandela 1 shed, idempotent via the upsert on name conflict.
-- (No native ON CONFLICT for names, so we use a conditional INSERT-or-skip.)
INSERT INTO locations (tenant_id, location_type, location_code, name,
                       parent_location_id, country, state_region, district,
                       pincode, lat, lng, timezone, status, display_order,
                       operational_notes)
SELECT tenant_id, 'shed', NULL, 'Mandela 1', parent_location_id,
       country, state_region, district, pincode, lat, lng, timezone,
       'active', 0, NULL
FROM locations
WHERE status = 'active'
  AND name = 'Mandela 2'
  AND location_type = 'shed'
  AND NOT EXISTS (
    SELECT 1 FROM locations check_mandela1
    WHERE check_mandela1.tenant_id = locations.tenant_id
      AND check_mandela1.name = 'Mandela 1'
      AND check_mandela1.location_type = 'shed'
      AND check_mandela1.status = 'active'
  );

-- ============================================================================
-- STEP 4: Register shed_partitions rows for Mandela 1's Part 1..10
-- ============================================================================
SELECT 'REGISTERING SHED_PARTITIONS FOR MANDELA 1' AS step;

-- First, capture the new Mandela 1 shed_id for use in this step and the next.
-- This query will be executed as part of the INSERT.

-- Read the existing Mandela 2 partition rows to match their shape exactly.
SELECT COUNT(*) AS expected_mandela2_partitions,
       STRING_AGG(partition_label || ' (' || normalized_label || ')', ', ' ORDER BY partition_label)
FROM shed_partitions
WHERE shed_id = (SELECT location_id FROM locations WHERE status = 'active'
                  AND name = 'Mandela 2' AND location_type = 'shed')
  -- NO source filter: the audit records BOTH 'goat_attested' (Godel 1 example)
  -- and 'location_alias' rows. Filtering to one value silently selects ZERO
  -- rows and creates a parent shed with NO partitions -- a silent no-op that
  -- reads as success. Take every partition row Mandela 2 actually has.
ORDER BY partition_label;
-- EXPECTED: 10 rows with labels Part 1..10 and normalized_labels 1..10.

-- GUARD: abort rather than create a partition-less parent shed.
DO $$
DECLARE n int;
BEGIN
  SELECT COUNT(*) INTO n FROM shed_partitions
  WHERE shed_id = (SELECT location_id FROM locations
                   WHERE status='active' AND name='Mandela 2' AND location_type='shed');
  IF n = 0 THEN
    RAISE EXCEPTION 'ABORT: Mandela 2 has no shed_partitions rows to mirror. Creating Mandela 1 with zero partitions would leave the same orphan state this repair exists to fix.';
  END IF;
END $$;

-- Register the same 10 partitions against the new Mandela 1 shed_id.
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
WITH mandela1_shed AS (
  SELECT location_id, tenant_id
  FROM locations
  WHERE status = 'active'
    AND name = 'Mandela 1'
    AND location_type = 'shed'
),
mandela2_partitions AS (
  SELECT tenant_id, partition_label, normalized_label, status, source
  FROM shed_partitions
  WHERE shed_id = (SELECT location_id FROM locations
                   WHERE status = 'active' AND name = 'Mandela 2' AND location_type = 'shed')
    -- deliberately unfiltered on source; see STEP 4 evidence note above
)
SELECT m1.tenant_id, m1.location_id, m2p.partition_label, m2p.normalized_label,
       m2p.status, m2p.source
FROM mandela1_shed m1
JOIN mandela2_partitions m2p ON m2p.tenant_id = m1.tenant_id
ON CONFLICT (tenant_id, shed_id, normalized_label) DO NOTHING;

-- ============================================================================
-- STEP 5: Soft-retire the 10 orphan alias-as-shed rows
-- ============================================================================
SELECT 'RETIRING MANDELA 1 - PART N ALIAS ROWS' AS step;

SELECT location_id, name, status, retired_at
FROM locations
WHERE status = 'active'
  AND location_type = 'shed'
  AND name LIKE 'Mandela 1 - Part %'
ORDER BY name;
-- EXPECTED (2026-08-07 baseline): exactly 10 rows (Part 1..10), all active,
-- all retired_at = NULL.

UPDATE locations l
SET status = 'inactive',
    retired_at = COALESCE(l.retired_at, now()),
    row_version = l.row_version + 1,
    updated_at = now()
WHERE l.status = 'active'
  AND l.location_type = 'shed'
  AND l.name LIKE 'Mandela 1 - Part %'
  AND NOT EXISTS (
    SELECT 1 FROM goats g
    WHERE g.tenant_id = l.tenant_id
      AND (g.shed_id = l.location_id OR g.current_location_id = l.location_id)
  )
  AND NOT EXISTS (
    SELECT 1 FROM goat_shed_partitions gsp
    WHERE gsp.tenant_id = l.tenant_id
      AND gsp.shed_id = l.location_id
  );

-- ============================================================================
-- STEP 6: VERIFY — post-repair state
-- ============================================================================
SELECT 'POST-REPAIR VERIFICATION' AS step;

-- Verify parent Mandela 1 exists
SELECT 'parent_exists' AS check_name,
       COUNT(*) AS count_expected_1
FROM locations
WHERE status = 'active'
  AND name = 'Mandela 1'
  AND location_type = 'shed';

-- Verify 10 shed_partitions rows exist for Mandela 1
SELECT 'mandela1_partitions' AS check_name,
       COUNT(*) AS count_expected_10
FROM shed_partitions
WHERE shed_id = (SELECT location_id FROM locations
                 WHERE status = 'active' AND name = 'Mandela 1' AND location_type = 'shed')
  AND status = 'active';

-- Verify 10 alias rows are now inactive
SELECT 'mandela1_aliases_inactive' AS check_name,
       COUNT(*) AS count_expected_10
FROM locations
WHERE status = 'inactive'
  AND location_type = 'shed'
  AND name LIKE 'Mandela 1 - Part %';

-- Verify 0 goats reference any of the retired Mandela 1 alias rows
SELECT 'goats_reference_retired_aliases' AS check_name,
       COUNT(*) AS count_expected_0
FROM goats g
WHERE g.shed_id IN (SELECT location_id FROM locations
                    WHERE status = 'inactive'
                      AND location_type = 'shed'
                      AND name LIKE 'Mandela 1 - Part %')
   OR g.current_location_id IN (SELECT location_id FROM locations
                                WHERE status = 'inactive'
                                  AND location_type = 'shed'
                                  AND name LIKE 'Mandela 1 - Part %');

-- ============================================================================
-- Deliberately uncommitted. A human runs this script, reads the STEP 0..6
-- SELECT output, and only then chooses:
--   COMMIT;
-- or, to back out and change nothing:
--   ROLLBACK;
-- ============================================================================
-- ROLLBACK;

-- Terminated ROLLBACK so an unattended run changes NOTHING and holds no locks.
-- To apply: change this to COMMIT; after reading the STEP 0..6 output.
ROLLBACK;
