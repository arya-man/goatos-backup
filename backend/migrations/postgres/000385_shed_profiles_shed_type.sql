-- +goose Up
-- seed-fixture-guard:ignore: one optional column on the pen profile, configured on screen; no
-- vaccination/HRMS seed contract and no read-model table changes shape.
--
-- PEN TYPE IS CONFIGURED, NOT GUESSED (maintainer instruction 2026-09-22).
--
-- The farm builds two kinds of pen -- ELEVATED (slatted floor, animals off the ground) and
-- NON-ELEVATED (the animals stand on the ground) -- and it wants to read its numbers split by
-- that, because the two do not keep animals equally well. Weighing already reports daily gain
-- that way and Health Analytics now reports health problems that way.
--
-- Until today NOTHING STORED IT. `weight_demographics.go` INFERRED the class per request: it
-- matched the words "elevated" / "crown" / "ground" in a pen's free-text notes, and when the
-- notes said nothing it fell back to a HARDCODED LIST OF PEN NAMES compiled in Go -- Gandhi and
-- Castro are ground, Mandela and Godel are elevated. That is a guess about a building, baked
-- into a query, that no one on the farm can correct and that silently misfiles every pen built
-- after the list was written. A new owner adding a pen had no way to say which kind it is.
--
-- So it becomes a COLUMN, set per pen on Configuration -> Items and settings -> Pens, beside
-- that pen's capacity and gender. One pen at a time: there is no farm-wide or park-wide switch,
-- because the farm mixes both kinds inside one park.
--
-- NULL IS A REAL AND EXPECTED STATE: "nobody has said yet". An unclassified pen is reported as
-- unclassified and is never assigned a side -- a pen the farm has not typed must not silently
-- land in one half of a comparison and move the average there.
--
-- VOCABULARY IS 'elevated' / 'non_elevated', and the farm's word for the second one is
-- "Non-elevated" everywhere a person reads it. The weighing read used to key the second class
-- as 'ground' and label it Crown/Ground; that key and that label are retired in this same
-- change, so one farm concept has one name on every screen.
ALTER TABLE public.shed_profiles
  ADD COLUMN IF NOT EXISTS shed_type text;

ALTER TABLE public.shed_profiles
  DROP CONSTRAINT IF EXISTS shed_profiles_shed_type_check,
  ADD CONSTRAINT shed_profiles_shed_type_check
    CHECK (shed_type IS NULL OR shed_type = ANY (ARRAY['elevated'::text, 'non_elevated'::text]));

COMMENT ON COLUMN public.shed_profiles.shed_type IS
  'Whether this pen is elevated (slatted, animals off the ground) or non_elevated. Configured per pen on Configuration -> Items and settings -> Pens. NULL means nobody has classified it yet and it is reported as unclassified, never folded into either side.';

-- BACKFILL FROM WHAT THE SCREENS ALREADY SHOWED, so no chart changes its answer on the day this
-- lands. Two sources, in the order the retired inference used them:
--
--   1. the pen's own words -- "elevated" in its notes or context, else "crown"/"ground";
--   2. the hardcoded name list that inference fell back to.
--
-- Source 2 is a GUESS and is written here only because it is the guess the farm has been reading
-- for months; the point of the column is that the Pens screen is now where it gets corrected.
-- A pen matching neither is left unclassified rather than assigned a side.
--
-- IT DRIVES OFF locations, NOT shed_profiles, AND INSERTS THE MISSING PROFILE ROW. This is the
-- whole reason the first version of this backfill was wrong, and it was only visible against the
-- farm's own data: 49 of 122 active pens have no shed_profiles row at all -- including Mandela 1,
-- which holds 118 live animals -- because a profile row is written lazily, when someone first
-- sets a capacity or a gender. An UPDATE could never reach them, so the pens that matter most
-- landed unclassified and dropped off both bars. The retired inference LEFT JOINed the profile
-- and never noticed.
INSERT INTO public.shed_profiles (location_id, tenant_id, shed_type)
SELECT pen.location_id,
       pen.tenant_id,
       CASE
         WHEN pen.words ~ '\melevate' THEN 'elevated'
         WHEN pen.words ~ '\m(crown|crowned|ground)\M' THEN 'non_elevated'
         WHEN pen.names ~ '\m(gandhi|castro|ho chi minh|old yashoda|yashoda old)\M' THEN 'non_elevated'
         WHEN pen.names ~ '\m(mandela|godel|sumathi|new yashoda|yashoda new|yashoda)\M' THEN 'elevated'
       END
FROM (
  SELECT l.location_id,
         l.tenant_id,
         lower(
           coalesce(l.operational_notes, '') || ' ' ||
           coalesce(parent.operational_notes, '') || ' ' ||
           coalesce(sp.notes, '') || ' ' || coalesce(sp.context::text, '') || ' ' ||
           coalesce(parent_sp.notes, '') || ' ' || coalesce(parent_sp.context::text, '')
         ) AS words,
         lower(coalesce(l.name, '') || ' ' || coalesce(parent.name, '')) AS names,
         sp.shed_type AS existing_type
  FROM public.locations l
  LEFT JOIN public.locations parent
    ON parent.location_id = l.parent_location_id AND parent.tenant_id = l.tenant_id
  LEFT JOIN public.shed_profiles sp
    ON sp.location_id = l.location_id AND sp.tenant_id = l.tenant_id
  LEFT JOIN public.shed_profiles parent_sp
    ON parent_sp.location_id = l.parent_location_id AND parent_sp.tenant_id = l.tenant_id
  WHERE l.location_type = 'shed'
) pen
WHERE pen.existing_type IS NULL
  AND (
    pen.words ~ '\melevate'
    OR pen.words ~ '\m(crown|crowned|ground)\M'
    OR pen.names ~ '\m(gandhi|castro|ho chi minh|old yashoda|yashoda old|mandela|godel|sumathi|new yashoda|yashoda new|yashoda)\M'
  )
ON CONFLICT (location_id) DO UPDATE
  SET shed_type = EXCLUDED.shed_type,
      updated_at = now(),
      row_version = public.shed_profiles.row_version + 1
  WHERE public.shed_profiles.shed_type IS NULL;

-- +goose Down
ALTER TABLE public.shed_profiles
  DROP CONSTRAINT IF EXISTS shed_profiles_shed_type_check;

ALTER TABLE public.shed_profiles
  DROP COLUMN IF EXISTS shed_type;
