-- +goose Up
-- The diagnosis TYPE and the animals it serves become authored data.
--
-- WHAT THIS REPLACES
-- ---------------------------------------------------------------------------
-- 000388 made the register's CONTENT authorable -- the questions, the mapping and the
-- rules, published as one version per animal class. What stayed in Go was the other
-- half of the same idea: WHICH classes exist, and WHICH animals reach each one.
--
--   backend/internal/health/diagnosis/embed.go         -> the four class ids
--   backend/internal/health/domain/animal_resolution.go -> kidStageClasses + the
--                                                          age-band branch for adults
--
-- So a farm could rewrite every rule in the adult register from the screen, and still
-- could not say "Warmup kids are their own cohort now" without a deploy. On 2026-09-23
-- that cost 58 live kids: `Warmup` was simply absent from the Go map, every animal on it
-- was refused, and the repair was a commit. Maintainer instruction the same day: adding a
-- type, and pointing a stage at it, is dashboard work.
--
-- WHAT IS DELIBERATELY NOT CHANGED
-- ---------------------------------------------------------------------------
-- THE FAIL-CLOSED PROPERTY. The 2026-08-17 decision is that an animal whose stage cannot
-- choose a register is REFUSED, never defaulted, because the registers disagree about the
-- things most likely to kill an animal -- a fattening kid diagnosed off the milk register
-- is never checked for acidosis. Moving the map into a table does not soften that: a stage
-- with no route still refuses, with the same sentence naming the stage.
--
-- TODAY'S ROUTING, BYTE FOR BYTE. The seed below reproduces the Go map exactly, so the
-- deploy changes no animal's register. That is what the ADULT WILDCARD row is for, and it
-- is the subtle part of this migration: adults are NOT routed by stage today. The Go code
-- reads `age_band == 'adult'` and never looks at the stage at all, so a doe on `Mother`,
-- a buck, and an adult sitting in `ICU` all reach the adult register. A stage-keyed table
-- alone would refuse every adult whose stage nobody thought to seed -- including the
-- clinical placements, which carry no age band of their own.
--
-- So a route may name a STAGE or stand for a whole AGE BAND, and the more specific one
-- wins. `(adult, *) -> adult` reproduces the age-band branch; adding `(adult, Mother) ->
-- mothers` later overrides it for those does alone and leaves every other adult where she
-- is. Kids get NO wildcard, which is precisely why an unmapped kid still refuses.

BEGIN;

-- The types a farm can diagnose against. One published register per ACTIVE type, which is
-- the identity 000388 already keys on (`animal_class`), so this table names the vocabulary
-- that column draws from rather than introducing a second one.
CREATE TABLE IF NOT EXISTS public.health_diagnosis_types (
  health_diagnosis_type_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES public.tenants (tenant_id),

  -- The machine key, and the value written to `health_diagnosis_register_versions.animal_class`
  -- and to `health_diagnosis_runs`. Immutable once created: a stored run names the type it was
  -- judged under, and renaming the key would make old proposals unreadable. The LABEL is what
  -- a screen shows and may be edited freely.
  type_key   text NOT NULL,
  label      text NOT NULL,

  -- `active` or `retired`. Retiring hides a type from the authoring screen and from routing;
  -- it never deletes it, because runs judged under it must stay interpretable.
  status     text NOT NULL DEFAULT 'active',
  sort_order integer NOT NULL DEFAULT 0,

  -- The four seeded types are the ones the engine shipped with. They may be relabelled and
  -- re-routed, but not retired: their registers are the committed rulebook every other type
  -- is authored beside, and a farm with none of them has nothing to diagnose against.
  is_builtin boolean NOT NULL DEFAULT false,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT health_diagnosis_types_key_uq UNIQUE (tenant_id, type_key),
  CONSTRAINT health_diagnosis_types_status_check CHECK (status IN ('active', 'retired')),
  CONSTRAINT health_diagnosis_types_key_shape_check
    CHECK (type_key ~ '^[a-z][a-z0-9_]{0,48}$'),
  CONSTRAINT health_diagnosis_types_label_check CHECK (btrim(label) <> '')
);

-- Which animals reach which type.
--
-- A row is keyed on the age band plus EITHER a concrete management stage or the wildcard
-- '*'. Resolution takes the exact stage first and falls back to the band's wildcard, so the
-- specific always beats the general and a farm can peel one stage off a broad rule without
-- touching the rest.
CREATE TABLE IF NOT EXISTS public.health_diagnosis_stage_routes (
  health_diagnosis_stage_route_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants (tenant_id),

  -- `adult` or `kid`, matching the goat's own age band as the engine reads it.
  age_band  text NOT NULL,

  -- The management stage code from `animal_stage_lookup`, normalised to lower case because
  -- that catalog holds the same codes in more than one casing across import runs -- the
  -- reason kidStageClasses compares case-insensitively today. '*' is the band wildcard.
  stage_code text NOT NULL,

  type_key   text NOT NULL,

  -- The sub-stage the register reads INSIDE the type: kid_milk runs a different ladder for a
  -- week-old K1 than for a K2 on the free-choice bar, and `offer_ors` / `session_bottle` /
  -- `force_milk` all branch on it. Blank where the type has one cohort, which is every type
  -- except kid_milk today. Dropping it would diagnose off the wrong half of the right register.
  sub_stage  text NOT NULL DEFAULT '',

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT health_diagnosis_stage_routes_uq UNIQUE (tenant_id, age_band, stage_code),
  CONSTRAINT health_diagnosis_stage_routes_band_check CHECK (age_band IN ('adult', 'kid')),
  CONSTRAINT health_diagnosis_stage_routes_stage_check CHECK (btrim(stage_code) <> ''),
  CONSTRAINT health_diagnosis_stage_routes_lower_check CHECK (stage_code = lower(stage_code)),
  -- A route must point at a type of the SAME tenant. Without this a route could name a key
  -- that does not exist and the animal would refuse for a reason nobody could see on screen.
  CONSTRAINT health_diagnosis_stage_routes_type_fk
    FOREIGN KEY (tenant_id, type_key)
    REFERENCES public.health_diagnosis_types (tenant_id, type_key)
    ON UPDATE CASCADE
);

-- The resolution read: one animal, one row. Covers the exact-stage probe and the wildcard
-- fallback in the same index because both are (tenant, band, stage) equality.
CREATE INDEX IF NOT EXISTS health_diagnosis_stage_routes_lookup_idx
  ON public.health_diagnosis_stage_routes (tenant_id, age_band, stage_code);

-- ---------------------------------------------------------------------------
-- The seed: today's Go map, for every tenant that has a register.
--
-- Scoped to tenants that already hold `health_diagnosis_register_versions` rows OR any goat,
-- so this does not fabricate health config for a tenant that has none.
-- ---------------------------------------------------------------------------

INSERT INTO public.health_diagnosis_types (tenant_id, type_key, label, sort_order, is_builtin)
SELECT t.tenant_id, v.type_key, v.label, v.sort_order, true
FROM public.tenants t
CROSS JOIN (VALUES
  ('adult',          'Adults',          10),
  ('kid_milk',       'Kids on milk',    20),
  ('kid_weaning',    'Kids weaning',    30),
  ('kid_fattening',  'Kids fattening',  40)
) AS v(type_key, label, sort_order)
WHERE EXISTS (SELECT 1 FROM public.goats g WHERE g.tenant_id = t.tenant_id)
   OR EXISTS (SELECT 1 FROM public.health_diagnosis_register_versions r WHERE r.tenant_id = t.tenant_id)
ON CONFLICT (tenant_id, type_key) DO NOTHING;

INSERT INTO public.health_diagnosis_stage_routes (tenant_id, age_band, stage_code, type_key, sub_stage)
SELECT t.tenant_id, v.age_band, v.stage_code, v.type_key, v.sub_stage
FROM public.tenants t
CROSS JOIN (VALUES
  -- kidStageClasses, exactly as animal_resolution.go holds it today.
  ('kid', 'k0',        'kid_milk',      'K0'),
  ('kid', 'k1',        'kid_milk',      'K1'),
  ('kid', 'k2',        'kid_milk',      'K2'),
  ('kid', 'k3',        'kid_weaning',   'K3'),
  ('kid', 'f2',        'kid_fattening', ''),
  ('kid', 'f2-male',   'kid_fattening', ''),
  ('kid', 'f2-female', 'kid_fattening', ''),
  -- Added 2026-09-23 by maintainer decision, after 58 live kids on this stage could not be
  -- observed at all. It is an ordinary row here, which is the whole point of the table.
  ('kid', 'warmup',    'kid_fattening', '')
) AS v(age_band, stage_code, type_key, sub_stage)
WHERE EXISTS (SELECT 1 FROM public.health_diagnosis_types ty WHERE ty.tenant_id = t.tenant_id)
ON CONFLICT (tenant_id, age_band, stage_code) DO NOTHING;

-- THE ADULT SIDE IS SEEDED FROM THE FARM'S OWN STAGES, one explicit row each.
--
-- The Go code never reads an adult's stage -- it branches on `age_band = 'adult'` -- so the
-- faithful translation of that is a WILDCARD row, and this migration seeded one. It was correct
-- and it was unreadable: the screen showed adults covered by "Every other stage", which is the
-- implementation's phrasing rather than the farm's, and it told a director nothing about which
-- cohorts were actually in there.
--
-- Naming each stage gives the identical routing today -- every adult stage the catalog holds
-- points at `adult`, which is where every adult goes now -- and makes the two things a farm
-- actually does possible on screen: see what a category covers, and move one cohort out of it
-- without touching the rest.
--
-- It also makes the fail-closed property real for adults rather than nominal. A stage added to
-- the catalog later reaches no type until someone says which, and the routing screen reports it
-- as a gap with its live animal count. That is the 2026-09-23 maintainer decision applied to both
-- age bands instead of only to kids.
INSERT INTO public.health_diagnosis_stage_routes (tenant_id, age_band, stage_code, type_key, sub_stage)
SELECT s.tenant_id, 'adult', lower(btrim(s.stage_code)), 'adult', ''
FROM public.animal_stage_lookup s
WHERE s.status = 'active'
  AND lower(coalesce(s.age_band, '')) = 'adult'
  AND EXISTS (SELECT 1 FROM public.health_diagnosis_types ty WHERE ty.tenant_id = s.tenant_id)
ON CONFLICT (tenant_id, age_band, stage_code) DO NOTHING;

-- The wildcard survives ONLY for a tenant whose stage catalog holds no adult stage at all.
--
-- Without it such a tenant would have every adult refused on deploy day, which is a behaviour
-- change smuggled in by a migration rather than chosen. The row is a safety net for an unseeded
-- catalog, not the intended shape: a farm with real stages gets the explicit rows above and never
-- sees it.
INSERT INTO public.health_diagnosis_stage_routes (tenant_id, age_band, stage_code, type_key, sub_stage)
SELECT t.tenant_id, 'adult', '*', 'adult', ''
FROM public.tenants t
WHERE EXISTS (SELECT 1 FROM public.health_diagnosis_types ty WHERE ty.tenant_id = t.tenant_id)
  AND NOT EXISTS (
        SELECT 1 FROM public.animal_stage_lookup s
         WHERE s.tenant_id = t.tenant_id AND s.status = 'active'
           AND lower(coalesce(s.age_band, '')) = 'adult')
ON CONFLICT (tenant_id, age_band, stage_code) DO NOTHING;

COMMIT;

-- +goose Down
BEGIN;
DROP INDEX IF EXISTS public.health_diagnosis_stage_routes_lookup_idx;
DROP TABLE IF EXISTS public.health_diagnosis_stage_routes;
DROP TABLE IF EXISTS public.health_diagnosis_types;
COMMIT;
