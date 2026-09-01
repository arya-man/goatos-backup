-- +goose Up
-- seed-fixture-guard:ignore: records an existing legacy location/pen relationship on the
-- partition catalog; no source fixture schema, HRMS seed contract, or read-model change.
-- Name the pen a legacy alias LOCATION stands for, as data, and stop inferring it.
--
-- WHY THIS EXISTS
--
-- A pen alias is a location row that duplicates a pen of another shed: 'Castro 1'
-- IS 'Castro' pen 1, 'Godel 2 - Part 1' IS 'Godel 2' pen 'Part 1'. 000112 seed 2
-- recorded that relationship only as source='location_alias' on the pen's catalog
-- row, and only for aliases with status='inactive'. Two things broke that.
--
-- First, on 2026-08-10 a bulk update flipped every alias location back to 'active'
-- (two batches, 00:38:08 and 01:21:55; producer never identified), so seed 2 no
-- longer matched them. Second, and why re-running seed 2 cannot fix it: the pen
-- already carries a catalog row from seed 1 with source='goat_attested', and the
-- unique key is (tenant_id, shed_id, normalized_label). The alias insert therefore
-- conflicts and is skipped. The catalog was never missing the PEN -- it was missing
-- the statement that a particular LOCATION is that pen, and a single `source`
-- column cannot hold both facts at once.
--
-- That surfaced when 3c771ffc7 made the lump-sum head count a herd-register census.
-- Weighing buckets point at these alias locations, which hold no goats directly
-- (the animals sit on the canonical shed plus a pen label), so the census returned
-- 0 and refused every lump-sum submit with ErrShedCountUnavailable: 25 HTTP 422s on
-- 2026-08-31 and 19 on 2026-09-01, zero successes, while the operator's video
-- uploaded fine each time and the team hand-inserted the rows daily.
--
-- WHY A COLUMN AND NOT AN INFERENCE
--
-- Two runtime inferences were tried and rejected. Matching the bucket's NAME
-- against a parent shed's name plus pen suffix cannot tell a pen alias from a
-- genuinely standalone shed whose name merely ends the same way ('Yashoda 2',
-- 'Q1 2') -- the two are identical in shape. Reading
-- goat_shed_partitions.source_shed_name is worse: both goat_relocate.go and
-- admin_goat_create.go write it as oploc.Display(destination), so a goat living in
-- canonical 'Q1' pen '4' carries the string 'Q1 4', exactly what a standalone shed
-- named 'Q1 4' would carry. Inferring from it let an EMPTY standalone shed report
-- its parent pen's animals as weighed (reproduced in review).
-- goat_location_history cannot stand in either: 38 rows on STG, recording none of
-- the historical moves off the alias locations.
--
-- So the relationship is recorded ONCE, here, as an explicit location id. After
-- this the census resolves a bucket by exact id and parses no names at all, and a
-- future standalone shed is never auto-aliased by shape -- only a row that says so.
-- A wrong row is visible and correctable; a wrong inference re-derives itself.
--
-- WHAT THE BACKFILL CLAIMS, AND THE EVIDENCE FOR IT
--
-- Verified read-only against STG before writing this. The separation is total:
-- 104 active locations match the alias shape and hold ZERO live animals between
-- them, while the 21 active sheds that do NOT match hold ALL 1633 live animals.
-- Every one of the 104 duplicates a pen its parent already has, and every one
-- carries one of the two 2026-08-10 bulk-flip timestamps -- they are exactly the
-- rows seed 2 would have registered before the flip.
--
-- The guard seed 2 never had is kept here: a location holding its own live animals
-- is a real shed and is never claimed as another shed's pen.
-- ON DELETE SET NULL, not the default RESTRICT: locations.DeleteLocation really does
-- DELETE a row (status 'staging'/'review'), and a plain reference would turn this
-- marker into a lock that fails that delete. If the location is gone the mapping is
-- meaningless, so it should clear itself rather than block an unrelated write.
ALTER TABLE shed_partitions
    ADD COLUMN IF NOT EXISTS alias_location_id uuid REFERENCES locations (location_id) ON DELETE SET NULL;

COMMENT ON COLUMN shed_partitions.alias_location_id IS
    'Legacy location row that IS this pen (''Castro 1'' = ''Castro'' pen 1). Set only by an explicit backfill or an operator decision, never inferred at read time: a shed name that merely ends in a pen-like suffix is not evidence.';

-- One location stands for at most one pen.
CREATE UNIQUE INDEX IF NOT EXISTS shed_partitions_alias_location_uidx
    ON shed_partitions (tenant_id, alias_location_id)
    WHERE alias_location_id IS NOT NULL;

WITH candidate AS (
    SELECT DISTINCT ON (alias.tenant_id, alias.location_id)
           alias.tenant_id,
           alias.location_id AS alias_location_id,
           shed.location_id  AS shed_id,
           regexp_replace(
               lower(btrim(regexp_replace(substr(alias.name, length(shed.name) + 1), '^[[:space:]]*-?[[:space:]]*', ''))),
               '^part[[:space:]]+', ''
           ) AS normalized_label,
           length(shed.name) AS shed_name_len
    FROM locations alias
    JOIN locations shed
      ON shed.tenant_id = alias.tenant_id
     AND shed.parent_location_id = alias.parent_location_id  -- same park; shed names repeat ACROSS parks
     AND shed.location_type = 'shed'
     AND shed.status = 'active'
     AND shed.retired_at IS NULL
     AND alias.name <> shed.name
     -- The separator is load-bearing, as migration 000129 already found: a bare
     -- prefix match binds 'Yashoda 10' to 'Yashoda 1' and derives the pen '0'.
     -- With ORDER BY shed_name_len DESC below, that WRONG longer parent wins, so
     -- the bare form both risks a false mapping and denies 'Yashoda 10' the
     -- correct one ('Yashoda' pen '10'). Three such names are live on STG today.
     AND (alias.name LIKE shed.name || ' %' OR alias.name LIKE shed.name || ' - %')
    WHERE alias.location_type = 'shed'
      AND alias.retired_at IS NULL
      -- Settled sheds only. A 'staging'/'review' row is not yet a place on the farm
      -- and is the one thing DeleteLocation may hard-delete, so it is never declared
      -- to be another shed's pen. Both statuses exist on STG today.
      AND alias.status IN ('active', 'inactive')
      -- A location holding its own live animals is a real shed, never a pen alias.
      AND NOT EXISTS (
          SELECT 1 FROM goats g
          WHERE g.tenant_id = alias.tenant_id
            AND g.shed_id = alias.location_id
            AND g.lifecycle_status = 'alive'
            AND g.exited_at IS NULL
      )
    -- Longest parent name wins, so 'Godel 1 - Part 3' binds to 'Godel 1' and never
    -- to a shorter shed sharing the prefix.
    ORDER BY alias.tenant_id, alias.location_id, shed_name_len DESC, shed.name
)
UPDATE shed_partitions sp
   SET alias_location_id = c.alias_location_id
  FROM candidate c
 WHERE sp.tenant_id = c.tenant_id
   AND sp.shed_id = c.shed_id
   AND sp.normalized_label = c.normalized_label
   AND sp.status = 'active'
   AND sp.alias_location_id IS NULL
   AND c.normalized_label <> ''
   AND c.normalized_label <> 'whole';

-- +goose Down
DROP INDEX IF EXISTS shed_partitions_alias_location_uidx;
ALTER TABLE shed_partitions DROP COLUMN IF EXISTS alias_location_id;
