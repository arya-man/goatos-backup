-- +goose Up
-- seed-fixture-guard:ignore: moves an existing column one grain down and rebuilds the one reporting
-- view that read it. No vaccination/HRMS seed contract, source fixture schema or read-model table
-- changes.
--
-- CAPACITY IS A FACT ABOUT THE PEN THE FARM WORKS, NOT THE BUILDING (maintainer instruction
-- 2026-09-30: "now we have capacity per shed; it should be per partition capacity, it won't be per
-- pen" -- on Items & settings the building register carried Capacity and the Partitions register
-- did not). This is the same move 000391 made for pen type, and for the same reason: the farm's pen
-- IS the partition -- Castro 1, Mandela 1 - Part 3, Yashoda 2 -- and how many animals a pen holds is
-- a property of that pen. One number on the building cannot say that Castro 1 holds 60 and Castro 2
-- holds 40.
--
-- UNLIKE PEN TYPE, CAPACITY IS NOT COPIED DOWN TO EVERY PARTITION, and that is the load-bearing
-- difference. A type is the same answer for each pen of a building; a capacity is a HEAD COUNT, so
-- copying a building's 200 onto its three pens would report 600 places for 200 animals. A value is
-- carried only where it provably belongs to one pen:
--   * the legacy alias location's own profile (that location IS the pen), and
--   * a building that holds exactly one partition (the building and the pen are the same place).
-- A building split into several pens has no honest per-pen answer and is left blank for the farm to
-- set per pen. On goatos-stg (checked read-only 2026-09-30) no building carries a capacity at all,
-- so this carries nothing there; the rules above are for any other database.

ALTER TABLE public.shed_partitions
  ADD COLUMN IF NOT EXISTS capacity integer;

ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_capacity_check,
  ADD CONSTRAINT shed_partitions_capacity_check CHECK (capacity IS NULL OR capacity >= 0);

-- projection-review: membership=every shed_partitions row of the tenant that has no capacity yet;
-- group_key=(tenant_id, shed_id, normalized_label), shed_partitions' primary key, so the UPDATE
-- touches each pen at most once; join_cardinality=shed_profiles is keyed on location_id (its primary
-- key) so both LEFT JOINs are 1:{0,1}, and the sibling count is a correlated scalar over the
-- partition's own building, so nothing can multiply a pen; pagination=NONE, a one-shot migration
-- over a bounded catalog (117 active pens on the live farm); scope=tenant_id on every join, on the
-- sibling count and on the UPDATE predicate.
UPDATE public.shed_partitions sp
SET capacity = src.capacity,
    updated_at = now()
FROM (
  SELECT p.tenant_id, p.shed_id, p.normalized_label,
         COALESCE(
           alias_profile.capacity,
           CASE WHEN (SELECT count(*) FROM public.shed_partitions sib
                      WHERE sib.tenant_id = p.tenant_id AND sib.shed_id = p.shed_id) = 1
                THEN own_profile.capacity END
         ) AS capacity
  FROM public.shed_partitions p
  LEFT JOIN public.shed_profiles own_profile
    ON own_profile.tenant_id = p.tenant_id AND own_profile.location_id = p.shed_id
  LEFT JOIN public.shed_profiles alias_profile
    ON alias_profile.tenant_id = p.tenant_id AND alias_profile.location_id = p.alias_location_id
) src
WHERE sp.tenant_id = src.tenant_id
  AND sp.shed_id = src.shed_id
  AND sp.normalized_label = src.normalized_label
  AND sp.capacity IS NULL
  AND src.capacity IS NOT NULL;

-- THE FARM'S OWN FIGURES (maintainer instruction 2026-09-30: "capacity seed data from here only
-- capacity" -- the Sheds DB Google Sheet, tab DB, Capacity columns CBE / CPT, read 2026-09-30).
-- Only the Capacity columns were taken; tags, area and potential tags in that sheet are ignored.
-- A pen is matched by park code + pen building name + normalized partition label, never by id, so
-- the same migration lands on any clone of the farm. Only a pen with NO capacity yet is written, so
-- a value the farm has already set in Items & settings is never overwritten. Left out on purpose,
-- and reported to the maintainer rather than guessed:
--   * sheet rows with no such pen: CBE Gandhi 1 - Part 1 / Part 2, CBE Q1-Q3, CPT Ho Chi Minh 1/2;
--   * sheet rows whose pen is retired: CBE Ho Chi Minh 2, CBE Godel 1 - Part 9 / Part 10;
--   * active pens the sheet leaves blank stay blank.
-- projection-review: membership=the 105 (park code, building, partition) rows below that name a
-- catalogued pen; group_key=(tenant_id, shed_id, normalized_label), shed_partitions' primary key, so
-- each pen is written at most once; join_cardinality=locations joined 1:1 on its primary key for the
-- building and its park, park_profiles 1:{0,1} on location_id, and the VALUES list holds each
-- (park, building, label) once; pagination=NONE, a one-shot seed over a bounded list;
-- scope=tenant_id carried from the partition onto its building and park joins and the UPDATE.
UPDATE public.shed_partitions sp
SET capacity = v.capacity,
    updated_at = now()
FROM (VALUES
    ('CBE', 'Castro', '1', 50),
    ('CBE', 'Castro', '2', 50),
    ('CBE', 'Castro', '3', 50),
    ('CBE', 'Gandhi', '2', 50),
    ('CBE', 'Gandhi', '3', 50),
    ('CBE', 'Godel 1', '1', 10),
    ('CBE', 'Godel 1', '2', 10),
    ('CBE', 'Godel 1', '3', 10),
    ('CBE', 'Godel 1', '4', 10),
    ('CBE', 'Godel 1', '5', 10),
    ('CBE', 'Godel 1', '6', 10),
    ('CBE', 'Godel 1', '7', 10),
    ('CBE', 'Godel 1', '8', 10),
    ('CBE', 'Godel 2', '1', 10),
    ('CBE', 'Godel 2', '2', 10),
    ('CBE', 'Godel 2', '3', 10),
    ('CBE', 'Godel 2', '4', 10),
    ('CBE', 'Godel 2', '5', 10),
    ('CBE', 'Godel 2', '6', 10),
    ('CBE', 'Godel 2', '7', 10),
    ('CBE', 'Godel 2', '8', 10),
    ('CBE', 'Ho Chi Minh', '1', 30),
    ('CBE', 'Mandela 1', '1', 13),
    ('CBE', 'Mandela 1', '2', 13),
    ('CBE', 'Mandela 1', '3', 13),
    ('CBE', 'Mandela 1', '4', 13),
    ('CBE', 'Mandela 1', '5', 13),
    ('CBE', 'Mandela 1', '6', 13),
    ('CBE', 'Mandela 1', '7', 26),
    ('CBE', 'Mandela 2', '1', 13),
    ('CBE', 'Mandela 2', '2', 13),
    ('CBE', 'Mandela 2', '3', 13),
    ('CBE', 'Mandela 2', '4', 13),
    ('CBE', 'Mandela 2', '5', 13),
    ('CBE', 'Mandela 2', '6', 13),
    ('CBE', 'Mandela 2', '7', 13),
    ('CBE', 'Mandela 2', '8', 13),
    ('CBE', 'Sumathi 1', '1', 13),
    ('CBE', 'Sumathi 1', '2', 13),
    ('CBE', 'Sumathi 1', '3', 13),
    ('CBE', 'Sumathi 1', '4', 13),
    ('CBE', 'Sumathi 1', '5', 13),
    ('CBE', 'Sumathi 1', '6', 13),
    ('CBE', 'Sumathi 1', '7', 13),
    ('CBE', 'Sumathi 1', '8', 13),
    ('CBE', 'Sumathi 2', '1', 13),
    ('CBE', 'Sumathi 2', '2', 13),
    ('CBE', 'Sumathi 2', '3', 13),
    ('CBE', 'Sumathi 2', '4', 13),
    ('CBE', 'Sumathi 2', '5', 13),
    ('CBE', 'Sumathi 2', '6', 13),
    ('CBE', 'Sumathi 2', '7', 13),
    ('CBE', 'Sumathi 2', '8', 13),
    ('CBE', 'Yashoda', '1', 15),
    ('CBE', 'Yashoda', '10', 15),
    ('CBE', 'Yashoda', '2', 15),
    ('CBE', 'Yashoda', '3', 15),
    ('CBE', 'Yashoda', '4', 15),
    ('CBE', 'Yashoda', '5', 15),
    ('CBE', 'Yashoda', '6', 15),
    ('CBE', 'Yashoda', '7', 15),
    ('CBE', 'Yashoda', '8', 15),
    ('CBE', 'Yashoda', '9', 30),
    ('CPT', 'Castro', '1', 30),
    ('CPT', 'Castro', '2', 30),
    ('CPT', 'Gandhi', '1', 50),
    ('CPT', 'Gandhi', '2', 50),
    ('CPT', 'Gandhi', '3', 50),
    ('CPT', 'Godel 1', '1', 20),
    ('CPT', 'Godel 1', '2', 20),
    ('CPT', 'Godel 1', '3', 20),
    ('CPT', 'Godel 1', '4', 20),
    ('CPT', 'Godel 2', '1', 20),
    ('CPT', 'Godel 2', '2', 20),
    ('CPT', 'Godel 2', '3', 20),
    ('CPT', 'Godel 2', '4', 20),
    ('CPT', 'Mandela 1', '1', 10),
    ('CPT', 'Mandela 1', '10', 10),
    ('CPT', 'Mandela 1', '2', 10),
    ('CPT', 'Mandela 1', '3', 10),
    ('CPT', 'Mandela 1', '4', 10),
    ('CPT', 'Mandela 1', '5', 10),
    ('CPT', 'Mandela 1', '6', 10),
    ('CPT', 'Mandela 1', '7', 10),
    ('CPT', 'Mandela 1', '8', 10),
    ('CPT', 'Mandela 1', '9', 10),
    ('CPT', 'Mandela 2', '1', 10),
    ('CPT', 'Mandela 2', '10', 10),
    ('CPT', 'Mandela 2', '2', 10),
    ('CPT', 'Mandela 2', '3', 10),
    ('CPT', 'Mandela 2', '4', 10),
    ('CPT', 'Mandela 2', '5', 10),
    ('CPT', 'Mandela 2', '6', 10),
    ('CPT', 'Mandela 2', '7', 10),
    ('CPT', 'Mandela 2', '8', 10),
    ('CPT', 'Mandela 2', '9', 10),
    ('CPT', 'Old Yashoda', '1', 5),
    ('CPT', 'Old Yashoda', '2', 5),
    ('CPT', 'Old Yashoda', '3', 5),
    ('CPT', 'Old Yashoda', '4', 5),
    ('CPT', 'Old Yashoda', '5', 15),
    ('CPT', 'Yashoda', '1', 10),
    ('CPT', 'Yashoda', '2', 10),
    ('CPT', 'Yashoda', '3', 20),
    ('CPT', 'Yashoda', '4', 40)
) AS v(park_code, shed_name, normalized_label, capacity),
     public.locations s,
     public.locations p
LEFT JOIN public.park_profiles pp ON pp.tenant_id = p.tenant_id AND pp.location_id = p.location_id
WHERE s.tenant_id = sp.tenant_id AND s.location_id = sp.shed_id
  AND p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id AND p.location_type = 'park'
  AND upper(COALESCE(NULLIF(pp.park_code, ''), p.location_code)) = v.park_code
  AND s.name = v.shed_name
  AND sp.normalized_label = v.normalized_label
  AND sp.status = 'active'
  AND sp.capacity IS NULL;

-- The building column is RETIRED: nothing writes it after this change and the one reader (below)
-- moves to the partition. It is kept rather than dropped because a dozen test fixtures still insert
-- it as part of a building profile -- the same choice 2026-09-22 made for the pen's stage and sex.
COMMENT ON COLUMN public.shed_profiles.capacity IS
  'RETIRED 000464: capacity is per partition (shed_partitions.capacity). Never read or write this.';

-- ceo_ai.shed_capacity_current: row MEMBERSHIP is unchanged (one bare row per shed location plus one
-- row per partition attested by animals, exactly as 000111 built it); only where CAPACITY comes from
-- changes. 000111 had to show every partition row its BUILDING's capacity because no per-partition
-- figure existed; now a partition row shows its own, and a building row shows the SUM of its active
-- pens -- but only when EVERY one of them has a capacity. A building with one unset pen reports
-- unknown_capacity rather than a total that silently leaves that pen out.
-- projection-review: membership=unchanged from 000111 (shed locations + animal-attested partition
-- labels); group_key=(tenant_id, shed_id) for occ and building capacity, (tenant_id, shed_id,
-- partition label) for part_occ, and the partition capacity join is on shed_partitions' primary key
-- (tenant_id, shed_id, normalized_label) with the label normalised by the same expression the
-- catalog uses, so it is 1:{0,1}; alias_cap groups by (tenant_id, alias_location_id) and yields a
-- value only when exactly one pen names that alias; join_cardinality=every capacity CTE is
-- pre-aggregated to its join key, so no join multiplies a grain row; pagination=none, whole-tenant
-- rollup read by reporting; scope=tenant_id on every CTE and join. Ratio key set: variance and
-- status compare animals and capacity drawn from the SAME grain row (building or partition).
CREATE OR REPLACE VIEW ceo_ai.shed_capacity_current AS
WITH occ AS (
    SELECT tenant_id, shed_id, COUNT(*)::bigint AS animals
    FROM goats
    WHERE shed_id IS NOT NULL
      AND lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, shed_id
),
part_occ AS (
    SELECT g.tenant_id, g.shed_id, NULLIF(gsp.partition_label, 'whole') AS partition_label,
           COUNT(*)::bigint AS animals
    FROM goats g
    JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
    WHERE g.shed_id IS NOT NULL
      AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
      AND NULLIF(gsp.partition_label, 'whole') IS NOT NULL
    GROUP BY g.tenant_id, g.shed_id, NULLIF(gsp.partition_label, 'whole')
),
partitions AS (
    SELECT DISTINCT tenant_id, shed_id, NULLIF(partition_label, 'whole') AS partition_label
    FROM goat_shed_partitions
    WHERE NULLIF(partition_label, 'whole') IS NOT NULL
),
-- A building's capacity is the sum of its active pens, and only when none of them is unset.
building_cap AS (
    SELECT tenant_id, shed_id,
           CASE WHEN count(*) = count(capacity) THEN sum(capacity)::integer END AS capacity
    FROM shed_partitions
    WHERE status = 'active'
    GROUP BY tenant_id, shed_id
),
-- A legacy alias location ("Castro 1" stored as its own location) is one pen.
alias_cap AS (
    SELECT tenant_id, alias_location_id AS shed_id,
           CASE WHEN count(*) = 1 THEN max(capacity) END AS capacity
    FROM shed_partitions
    WHERE status = 'active' AND alias_location_id IS NOT NULL
    GROUP BY tenant_id, alias_location_id
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS owner_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
shed_grains AS (
    SELECT s.tenant_id, s.location_id AS shed_id, NULL::text AS partition_label
    FROM locations s
    WHERE s.location_type = 'shed'
    UNION ALL
    SELECT p.tenant_id, p.shed_id, p.partition_label
    FROM partitions p
),
grain_values AS (
    SELECT sg.tenant_id, sg.shed_id, sg.partition_label,
           CASE WHEN sg.partition_label IS NULL
                THEN COALESCE(occ.animals, 0)
                ELSE COALESCE(po.animals, 0)
           END AS animals,
           CASE WHEN sg.partition_label IS NULL
                THEN COALESCE(bc.capacity, ac.capacity)
                ELSE pc.capacity
           END AS capacity
    FROM shed_grains sg
    LEFT JOIN occ         ON occ.tenant_id = sg.tenant_id AND occ.shed_id = sg.shed_id
    LEFT JOIN part_occ po ON po.tenant_id = sg.tenant_id AND po.shed_id = sg.shed_id AND po.partition_label = sg.partition_label
    LEFT JOIN building_cap bc ON sg.partition_label IS NULL AND bc.tenant_id = sg.tenant_id AND bc.shed_id = sg.shed_id
    LEFT JOIN alias_cap    ac ON sg.partition_label IS NULL AND ac.tenant_id = sg.tenant_id AND ac.shed_id = sg.shed_id
    LEFT JOIN shed_partitions pc
      ON sg.partition_label IS NOT NULL
     AND pc.tenant_id = sg.tenant_id AND pc.shed_id = sg.shed_id
     AND pc.normalized_label = regexp_replace(lower(btrim(sg.partition_label)), '^part[[:space:]]+', '')
)
SELECT
    s.tenant_id                                   AS tenant_id,
    pk.name                                       AS park_label,
    s.name                                        AS shed_label,
    gv.animals                                    AS animals,
    gv.capacity                                   AS capacity,
    (gv.capacity - gv.animals)                    AS variance,
    CASE
        WHEN gv.capacity IS NULL THEN 'unknown_capacity'
        WHEN gv.animals > gv.capacity THEN 'over_capacity'
        WHEN gv.animals = gv.capacity THEN 'at_capacity'
        ELSE 'under_capacity'
    END                                           AS status,
    o.owner_label                                 AS owner_label,
    bk.backup_label                               AS backup_label,
    gv.partition_label                            AS partition_label
FROM grain_values gv
JOIN locations s ON s.location_id = gv.shed_id AND s.tenant_id = gv.tenant_id
LEFT JOIN locations     pk ON pk.location_id = s.parent_location_id
LEFT JOIN owner_seat  o  ON o.tenant_id  = s.tenant_id AND o.shed_id  = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

-- +goose Down
-- Back to 000111's view (the building's capacity on every row of it).
CREATE OR REPLACE VIEW ceo_ai.shed_capacity_current AS
WITH occ AS (
    SELECT tenant_id, shed_id, COUNT(*)::bigint AS animals
    FROM goats
    WHERE shed_id IS NOT NULL
      AND lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, shed_id
),
part_occ AS (
    SELECT g.tenant_id, g.shed_id, NULLIF(gsp.partition_label, 'whole') AS partition_label,
           COUNT(*)::bigint AS animals
    FROM goats g
    JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id
    WHERE g.shed_id IS NOT NULL
      AND g.lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
      AND NULLIF(gsp.partition_label, 'whole') IS NOT NULL
    GROUP BY g.tenant_id, g.shed_id, NULLIF(gsp.partition_label, 'whole')
),
partitions AS (
    SELECT DISTINCT tenant_id, shed_id, NULLIF(partition_label, 'whole') AS partition_label
    FROM goat_shed_partitions
    WHERE NULLIF(partition_label, 'whole') IS NOT NULL
),
owner_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS owner_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = false
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
backup_seat AS (
    SELECT wp.tenant_id, wp.scope_id AS shed_id, wm.display_name AS backup_label
    FROM workforce_positions wp
    JOIN workforce_members wm ON wm.workforce_member_id = wp.workforce_member_id
    WHERE wp.scope_type = 'shed' AND wp.is_backup_slot = true
      AND wp.status = 'active'
      AND now() >= wp.valid_from AND now() < COALESCE(wp.valid_to, 'infinity'::timestamptz)
),
shed_grains AS (
    SELECT s.tenant_id, s.location_id AS shed_id, NULL::text AS partition_label
    FROM locations s
    WHERE s.location_type = 'shed'
    UNION ALL
    SELECT p.tenant_id, p.shed_id, p.partition_label
    FROM partitions p
)
SELECT
    s.tenant_id                                   AS tenant_id,
    pk.name                                       AS park_label,
    s.name                                        AS shed_label,
    CASE WHEN sg.partition_label IS NULL
         THEN COALESCE(occ.animals, 0)
         ELSE COALESCE(po.animals, 0)
    END                                           AS animals,
    sp.capacity                                   AS capacity,
    (sp.capacity - CASE WHEN sg.partition_label IS NULL
                        THEN COALESCE(occ.animals, 0)
                        ELSE COALESCE(po.animals, 0)
                   END)                           AS variance,
    CASE
        WHEN sp.capacity IS NULL THEN 'unknown_capacity'
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(occ.animals, 0) ELSE COALESCE(po.animals, 0) END) > sp.capacity THEN 'over_capacity'
        WHEN (CASE WHEN sg.partition_label IS NULL THEN COALESCE(occ.animals, 0) ELSE COALESCE(po.animals, 0) END) = sp.capacity THEN 'at_capacity'
        ELSE 'under_capacity'
    END                                           AS status,
    o.owner_label                                 AS owner_label,
    bk.backup_label                                AS backup_label,
    sg.partition_label                            AS partition_label
FROM shed_grains sg
JOIN locations s ON s.location_id = sg.shed_id AND s.tenant_id = sg.tenant_id
LEFT JOIN locations     pk ON pk.location_id = s.parent_location_id
LEFT JOIN shed_profiles sp ON sp.location_id = s.location_id
LEFT JOIN occ         ON occ.tenant_id = sg.tenant_id AND occ.shed_id = sg.shed_id
LEFT JOIN part_occ  po ON po.tenant_id = sg.tenant_id AND po.shed_id = sg.shed_id AND po.partition_label = sg.partition_label
LEFT JOIN owner_seat  o  ON o.tenant_id  = s.tenant_id AND o.shed_id  = s.location_id
LEFT JOIN backup_seat bk ON bk.tenant_id = s.tenant_id AND bk.shed_id = s.location_id
WHERE s.location_type = 'shed';

-- Going back up a grain: a building gets the sum of its pens only when every pen has one, and never
-- overwrites a value the building still carries. Lossy by nature and recorded as such.
-- projection-review: membership=every (tenant, shed) whose active pens all carry a capacity;
-- group_key=(tenant_id, shed_id), exactly the GROUP BY; join_cardinality=the pens are collapsed by
-- the aggregate before the 1:1 join to shed_profiles on its primary key; pagination=NONE;
-- scope=tenant_id in the GROUP BY and the UPDATE predicate. Ratio key set: count(*) and
-- count(capacity) range over the identical grouped rows.
UPDATE public.shed_profiles pr
SET capacity = totals.capacity
FROM (
  SELECT tenant_id, shed_id, sum(capacity)::integer AS capacity
  FROM public.shed_partitions
  WHERE status = 'active'
  GROUP BY tenant_id, shed_id
  HAVING count(*) = count(capacity)
) totals
WHERE pr.tenant_id = totals.tenant_id AND pr.location_id = totals.shed_id AND pr.capacity IS NULL;

COMMENT ON COLUMN public.shed_profiles.capacity IS NULL;
ALTER TABLE public.shed_partitions DROP CONSTRAINT IF EXISTS shed_partitions_capacity_check;
ALTER TABLE public.shed_partitions DROP COLUMN IF EXISTS capacity;
