-- ============================================================================
-- stg-partition-catalog-repair.sql
--
-- Repairs the shed-partition catalog corruption behind the "Godel 1 1" /
-- "Godel 1 10" shifting-partition dropdown bug and the Add-birth dropdown
-- listing "Godel 1" six times, observed against the stg replica:
--   postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable
--
-- Evidence (counts as of 2026-08-07, see docs/runbooks/stg-partition-data-repair.md
-- for the full audit trail):
--   Class A (CORRUPT catalog rows, self-referential alias-as-shed):     2 rows
--   Class B (redundant alias-as-shed locations, safe to retire):      15 rows
--   Class C (redundant alias-as-shed locations, NEEDS HUMAN DECISION): 10 rows
--   Class D (park-mismatched alias rows, cosmetic, folded into A/B):    3 rows
--   Class E (Yashoda-style standalone sheds): 0 rows touched — confirmed NOT
--            partitions, mechanically distinguishable (no " - Part " token
--            in the name; each is its own top-level `locations` row parented
--            directly under a park, never under another shed).
--
-- SAFETY
--   * Every mutating statement is preceded by a SELECT that shows exactly
--     what it will touch. Run the SELECTs first; only run the guarded
--     UPDATE/DELETE blocks after a human has read the output.
--   * No goat table (goats, goat_shed_partitions, goat_location_history,
--     goat_custody_history) is touched by this script. All animal
--     placement is verified read-only, confirming 0 goats reference any of
--     the rows below (see runbook "Verify Before" section).
--   * No `locations` ROW IS DELETED. Redundant alias-as-shed rows are
--     soft-retired (status -> 'inactive', retired_at set), never dropped,
--     because `shed_partitions_shed_fk` / `goat_shed_partitions_shed_fk`
--     are ON DELETE RESTRICT and because other tables may hold historical
--     FK references we did not exhaustively enumerate (goat_location_history,
--     shifting_events, weighing_campaign_sheds, etc.) — see runbook.
--   * Idempotent: every UPDATE/DELETE is scoped with a WHERE clause that is
--     already-true-safe to re-run (status already 'inactive' -> no-op;
--     catalog row already deleted -> no-op).
--   * Wrapped in an explicit transaction. Read the COMMIT/ROLLBACK lines at
--     the bottom before running — nothing here auto-commits.
--
-- SCOPE THIS SCRIPT DELIBERATELY DOES NOT COVER
--   * Class C (Mandela 1's 10 orphan alias-as-shed rows) — no active parent
--     `Mandela 1` shed exists to attach a catalog entry to. Retiring these
--     blind would delete the only surviving record that "Mandela 1" pens
--     exist at all. A human must decide (see runbook) whether to (a) create
--     a `Mandela 1` parent shed + shed_partitions rows first, or (b) keep
--     these standalone until real placement data justifies a decision.
--   * The `partition_id` schema migration (see runbook "Cannot Be Repaired
--     By Data Alone").
-- ============================================================================

BEGIN;

-- ----------------------------------------------------------------------------
-- STEP 0: Pin the tenant so every guard below is unambiguous.
-- ----------------------------------------------------------------------------
-- All rows audited belong to tenant 00000000-0000-4000-8000-000000000001.
-- If this repair is ever run against a DB with more than one tenant, verify
-- that assumption first:
--   SELECT DISTINCT tenant_id FROM shed_partitions;
-- must return exactly one row equal to the constant below, or STOP.

-- ----------------------------------------------------------------------------
-- STEP 1 (Class A) — EVIDENCE: the 2 corrupt shed_partitions catalog rows
-- whose shed_id points at a location-alias row instead of the true parent
-- shed (partition_label = '0', normalized_label = '0', source =
-- 'location_alias'). Confirmed 0 goats reference either shed_id via
-- goats.shed_id, goats.current_location_id, or goat_shed_partitions.shed_id.
-- ----------------------------------------------------------------------------
SELECT sp.tenant_id, sp.shed_id, l.name AS shed_id_points_at, sp.partition_label,
       sp.normalized_label, sp.source, sp.status,
       (SELECT count(*) FROM goats g WHERE g.shed_id = sp.shed_id) AS goats_via_shed_id,
       (SELECT count(*) FROM goats g WHERE g.current_location_id = sp.shed_id) AS goats_via_current_loc,
       (SELECT count(*) FROM goat_shed_partitions gsp WHERE gsp.shed_id = sp.shed_id) AS goats_via_gsp
FROM shed_partitions sp
JOIN locations l ON l.location_id = sp.shed_id AND l.tenant_id = sp.tenant_id
WHERE sp.source = 'location_alias'
  AND sp.partition_label = '0'
  AND sp.normalized_label = '0';
-- EXPECTED (2026-08-07 baseline): exactly 2 rows
--   shed_id da1f37fc-a939-4357-a980-6e96914dfee4 -> "Godel 1 - Part 1"
--   shed_id 3505169a-e3a9-49ab-b53b-06a8e7bc7eff -> "Godel 2 - Part 1"
--   all three goat-reference counts = 0 for both rows.
-- If any goat-reference count is NOT 0, STOP. Do not run STEP 2. That means
-- a goat has been placed directly on the alias-as-shed row and this becomes
-- a human decision (reassign the goat to the correct parent+partition first).

-- ----------------------------------------------------------------------------
-- STEP 2 (Class A) — REPAIR: delete the 2 corrupt catalog rows.
-- Guarded to only ever match the exact corrupt signature (source =
-- 'location_alias' AND label = '0'); safe to re-run (no-op once deleted).
-- This does NOT delete the underlying `locations` rows — see Class B/C below
-- for those.
-- ----------------------------------------------------------------------------
DELETE FROM shed_partitions sp
WHERE sp.source = 'location_alias'
  AND sp.partition_label = '0'
  AND sp.normalized_label = '0'
  AND NOT EXISTS (
    SELECT 1 FROM goats g WHERE g.shed_id = sp.shed_id OR g.current_location_id = sp.shed_id
  )
  AND NOT EXISTS (
    SELECT 1 FROM goat_shed_partitions gsp WHERE gsp.shed_id = sp.shed_id
  );

-- ----------------------------------------------------------------------------
-- STEP 3 (Class B) — EVIDENCE: 15 redundant alias-as-shed `locations` rows
-- for Godel 1 / Godel 2 / Mandela 2. Each duplicates a pen that is ALREADY
-- correctly cataloged in shed_partitions against the true parent shed (e.g.
-- shed_partitions has {shed=Godel 1 (a80948b1...), label='Part 1'}, so the
-- separate top-level location "Godel 1 - Part 1" (da1f37fc...) is pure
-- duplication, feeding the "Godel 1 1" / sixfold "Godel 1" dropdown bug).
-- Confirmed 0 goats reference any of these 15 rows directly.
-- ----------------------------------------------------------------------------
SELECT l.location_id, l.name, l.parent_location_id, l.status,
       (SELECT count(*) FROM goats g WHERE g.shed_id = l.location_id) AS goats_via_shed_id,
       (SELECT count(*) FROM goats g WHERE g.current_location_id = l.location_id) AS goats_via_current_loc,
       (SELECT count(*) FROM goat_shed_partitions gsp WHERE gsp.shed_id = l.location_id) AS goats_via_gsp
FROM locations l
WHERE l.status = 'active'
  AND (
    l.name LIKE 'Godel 1 - Part %'
    OR l.name LIKE 'Godel 2 - Part %'
    OR l.name LIKE 'Mandela 2 - Part %'
  )
ORDER BY l.name;
-- EXPECTED (2026-08-07 baseline): exactly 15 rows (3 Godel 1, 2 Godel 2,
-- 10 Mandela 2), all three goat-reference counts = 0 for every row.
-- If any row shows a non-zero goat count, remove that specific location_id
-- from STEP 4's WHERE list and treat it as a human decision instead — do
-- NOT retire a location that is currently holding an animal.

-- ----------------------------------------------------------------------------
-- STEP 4 (Class B) — REPAIR: soft-retire the 15 redundant rows. Idempotent
-- (status already 'inactive' -> WHERE excludes it, no-op on re-run). Never
-- deletes the row (FK-safe, preserves history).
-- ----------------------------------------------------------------------------
UPDATE locations l
SET status = 'inactive',
    retired_at = COALESCE(l.retired_at, now()),
    row_version = l.row_version + 1,
    updated_at = now()
WHERE l.status = 'active'
  AND (
    l.name LIKE 'Godel 1 - Part %'
    OR l.name LIKE 'Godel 2 - Part %'
    OR l.name LIKE 'Mandela 2 - Part %'
  )
  AND NOT EXISTS (SELECT 1 FROM goats g WHERE g.shed_id = l.location_id OR g.current_location_id = l.location_id)
  AND NOT EXISTS (SELECT 1 FROM goat_shed_partitions gsp WHERE gsp.shed_id = l.location_id);

-- ----------------------------------------------------------------------------
-- STEP 5 (Class C) — EVIDENCE ONLY, NO REPAIR. 10 "Mandela 1 - Part N"
-- alias-as-shed rows with NO active parent "Mandela 1" shed to attach a
-- catalog entry to. This SELECT is provided so a human reviewing this
-- script sees the class without the script silently skipping it.
-- ----------------------------------------------------------------------------
SELECT l.location_id, l.name, l.parent_location_id, l.status,
       (SELECT count(*) FROM goats g WHERE g.shed_id = l.location_id) AS goats_via_shed_id,
       (SELECT count(*) FROM goats g WHERE g.current_location_id = l.location_id) AS goats_via_current_loc,
       (SELECT count(*) FROM goat_shed_partitions gsp WHERE gsp.shed_id = l.location_id) AS goats_via_gsp,
       (SELECT count(*) FROM locations p WHERE p.name = 'Mandela 1' AND p.status = 'active') AS active_parent_exists
FROM locations l
WHERE l.status = 'active'
  AND l.name LIKE 'Mandela 1 - Part %'
ORDER BY l.name;
-- HUMAN DECISION REQUIRED before any repair touches these 10 rows — see
-- docs/runbooks/stg-partition-data-repair.md "What A Human Must Decide".
-- DO NOT extend STEP 4's pattern match to include 'Mandela 1' without that
-- decision being made and recorded.

-- ----------------------------------------------------------------------------
-- STEP 6 — VERIFY: post-repair catalog should show 0 remaining corrupt rows
-- and 0 remaining active redundant Godel/Mandela-2 alias-as-shed rows.
-- ----------------------------------------------------------------------------
SELECT 'class_a_remaining' AS check_name, count(*) AS remaining
FROM shed_partitions
WHERE source = 'location_alias' AND partition_label = '0' AND normalized_label = '0'
UNION ALL
SELECT 'class_b_remaining', count(*)
FROM locations
WHERE status = 'active'
  AND (name LIKE 'Godel 1 - Part %' OR name LIKE 'Godel 2 - Part %' OR name LIKE 'Mandela 2 - Part %');
-- EXPECTED after a successful run: both rows = 0.

-- ----------------------------------------------------------------------------
-- Deliberately uncommitted. A human runs this script, reads the STEP 1/3/5
-- SELECT output, and only then chooses:
--   COMMIT;
-- or, to back out and change nothing:
--   ROLLBACK;
-- ----------------------------------------------------------------------------
-- ROLLBACK;
