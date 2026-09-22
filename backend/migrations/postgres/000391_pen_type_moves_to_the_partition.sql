-- +goose Up
-- seed-fixture-guard:ignore: moves an existing column one grain down and carries its values with
-- it. No vaccination/HRMS seed contract, source fixture schema or read-model table changes.
--
-- PEN TYPE IS A FACT ABOUT THE PEN THE FARM WORKS, NOT THE BUILDING (maintainer instruction
-- 2026-09-22: "assignment will be per partition only not pen").
--
-- Migration 000385 made pen type real data for the first time -- shed_profiles.shed_type, set one
-- BUILDING at a time -- and that was the right first move: it retired an in-query guess that read
-- elevated/ground out of a pen's notes and then out of a hardcoded list of building names. What it
-- could not say is that Castro 1 and Castro 2 are different pens. The farm's pen IS the partition:
-- "Castro 1", "Mandela 1 - Part 3" are what is painted on the buildings and what an operator is
-- sent to, and one building really can hold pens that were built differently.
--
-- So the column moves DOWN a grain. Nothing is re-derived and no chart moves: every partition
-- inherits the type its own building already carried, which is exactly the answer both readers
-- were getting from it before. A pen whose building was never classified stays NULL, and NULL
-- still means "nobody has said", never "not elevated".
--
-- The vocabulary is UNCHANGED (elevated | non_elevated). Renaming it in the same breath as moving
-- it would make one migration answer two questions, and the second one has no farm reason.

ALTER TABLE public.shed_partitions
  ADD COLUMN IF NOT EXISTS shed_type text;

ALTER TABLE public.shed_partitions
  DROP CONSTRAINT IF EXISTS shed_partitions_shed_type_check,
  ADD CONSTRAINT shed_partitions_shed_type_check
    CHECK (shed_type IS NULL OR shed_type = ANY (ARRAY['elevated'::text, 'non_elevated'::text]));

-- Carry every classified building down to its own pens. A partition that already carries a type
-- keeps it: this migration must be able to run after someone has started classifying by hand.
-- projection-review: membership=every active-or-retired shed_partitions row of the tenant, joined
-- to the profile of its OWN building and of its alias location; group_key=(tenant_id, shed_id,
-- normalized_label), which is shed_partitions' primary key, so the UPDATE touches each pen once;
-- join_cardinality=shed_profiles is keyed on location_id (its primary key) so each of the two
-- LEFT JOINs is 1:{0,1} and neither can multiply a partition; pagination=NONE, a one-shot
-- migration over a bounded catalog (117 pens on the live farm); scope=tenant_id carried on every
-- join and on the UPDATE predicate, so one tenant's pens can never be typed from another's.
UPDATE public.shed_partitions sp
SET shed_type = src.shed_type,
    updated_at = now()
FROM (
  -- A partition's type comes from its OWN building, or -- for the legacy alias rows, where the
  -- pen is stored as its own location -- from the alias location's profile. Both spellings of one
  -- pen must land on the same answer, which is the whole point of doing this in SQL once rather
  -- than leaving each reader to resolve it.
  SELECT p.tenant_id, p.shed_id, p.normalized_label,
         COALESCE(alias_profile.shed_type, own_profile.shed_type) AS shed_type
  FROM public.shed_partitions p
  LEFT JOIN public.shed_profiles own_profile
    ON own_profile.tenant_id = p.tenant_id AND own_profile.location_id = p.shed_id
  LEFT JOIN public.shed_profiles alias_profile
    ON alias_profile.tenant_id = p.tenant_id AND alias_profile.location_id = p.alias_location_id
) src
WHERE sp.tenant_id = src.tenant_id
  AND sp.shed_id = src.shed_id
  AND sp.normalized_label = src.normalized_label
  AND sp.shed_type IS NULL
  AND src.shed_type IS NOT NULL;

-- The old column is RETIRED, not left as a second place to look. Two columns holding one farm fact
-- is the cross-surface disagreement this repository treats as a maintainer question: whichever
-- screen read the stale one would be quietly wrong, and nobody would know which.
ALTER TABLE public.shed_profiles
  DROP CONSTRAINT IF EXISTS shed_profiles_shed_type_check;

ALTER TABLE public.shed_profiles
  DROP COLUMN IF EXISTS shed_type;

CREATE INDEX IF NOT EXISTS shed_partitions_shed_type_idx
  ON public.shed_partitions (tenant_id, shed_type)
  WHERE shed_type IS NOT NULL;

-- +goose Down
ALTER TABLE public.shed_profiles
  ADD COLUMN IF NOT EXISTS shed_type text;

ALTER TABLE public.shed_profiles
  DROP CONSTRAINT IF EXISTS shed_profiles_shed_type_check,
  ADD CONSTRAINT shed_profiles_shed_type_check
    CHECK (shed_type IS NULL OR shed_type = ANY (ARRAY['elevated'::text, 'non_elevated'::text]));

-- Going back up a grain cannot be exact: a building whose pens disagree has no single answer, so
-- it is left unclassified rather than given one of them. Recorded here because a reader of the
-- Down path deserves to know it is lossy.
-- projection-review: membership=every (tenant, shed) whose partitions carry a type;
-- group_key=(tenant_id, shed_id), exactly the GROUP BY; join_cardinality=the many partitions are
-- COLLAPSED by the aggregate and the HAVING count(DISTINCT shed_type) = 1 is what keeps it honest
-- -- a building whose pens DISAGREE yields no row at all rather than one of their answers;
-- pagination=NONE; scope=tenant_id is in the GROUP BY and in the UPDATE predicate. Ratio key set:
-- the min() and the count(DISTINCT) range over the IDENTICAL grouped rows, so a count of 1
-- provably means the single value min() returns.
UPDATE public.shed_profiles pr
SET shed_type = agreed.shed_type
FROM (
  SELECT tenant_id, shed_id, min(shed_type) AS shed_type
  FROM public.shed_partitions
  WHERE shed_type IS NOT NULL
  GROUP BY tenant_id, shed_id
  HAVING count(DISTINCT shed_type) = 1
) agreed
WHERE pr.tenant_id = agreed.tenant_id AND pr.location_id = agreed.shed_id;

DROP INDEX IF EXISTS public.shed_partitions_shed_type_idx;
ALTER TABLE public.shed_partitions DROP CONSTRAINT IF EXISTS shed_partitions_shed_type_check;
ALTER TABLE public.shed_partitions DROP COLUMN IF EXISTS shed_type;
