-- +goose Up
-- 000457_pc_care_fumigation.sql
--
-- FUMIGATION (maintainer instruction 2026-09-30): a sixth PC Care work category, the pen
-- disinfectant spray. The operator mixes Virufix into water (5 ml per litre) and sprays the pen.
--
-- It is PEN work, not animal work: nothing is scanned, and the pen's two live-camera videos --
-- the mixing, then the spraying -- are the whole evidence (the task_proof capture the fridge
-- check and the removal card already use, rows in pc_care_task_proofs). No feed & water removal
-- and no planning cutoff: it may be planned for today. The tenant verifier reviews it and the
-- sprayed pen owes the next-day pen visit like the other five.
--
-- Rollout-safe, every change:
--
--   1. The category CHECKs on pc_care_tasks / pc_care_rounds admit 'fumigation'. No existing
--      row changes.
--   2. The seeded fumigation card is added IN PLACE to every pc_care.tasks version that lacks
--      it (the weighing 000315 shape). No task is pinned to fumigation before this migration,
--      so no task's behaviour moves; the version keeps its number. Without it the SOP editor
--      would refuse to save a new version ("every work category needs its capture card") and a
--      published version would lean on the Go seed fallback for the new card. The card is
--      embedded verbatim and pinned by TestMigrationEmbedsTheSeededFumigationCard.
--   3. / 4. The planners' per-person ticks and job defaults (below), additive and ledgered.
--
-- seed-fixture-guard:ignore: adds an operational PC Care work category and its seeded SOP card;
-- no vaccination / HRMS / goats schema moves.

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'fumigation', 'inventory_vaccine', 'feed_water_removal')
  );

ALTER TABLE public.pc_care_rounds
  DROP CONSTRAINT IF EXISTS pc_care_rounds_category_check;

ALTER TABLE public.pc_care_rounds
  ADD CONSTRAINT pc_care_rounds_category_check CHECK (
    category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'fumigation')
  );

-- seed-migration-guard:ignore owner=claude issue=pc-care-fumigation reason=in-place card add on the pc_care.tasks library document; no task is pinned to the new category before this migration expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{pc_care,categories,fumigation}', $fumigation${
      "instruction": "Mix 5 ml of Virufix liquid into every litre of water and spray the whole pen. Record two videos for the pen: the mixing, then the spraying.",
      "proofs": [
        {"key": "mixing_video", "title": "Mixing video", "hint": "Show 5 ml of Virufix being mixed into each litre of water", "kind": "video", "required": true},
        {"key": "spraying_video", "title": "Spraying video", "hint": "Show the mixture being sprayed across this pen", "kind": "video", "required": true}
      ],
      "questions": []
    }$fumigation$::jsonb, true),
    updated_at = now()
FROM public.sop_definitions sd
WHERE sd.tenant_id = v.tenant_id
  AND sd.sop_id = v.sop_id
  AND sd.code = 'pc_care.tasks'
  AND jsonb_typeof(v.form_dsl -> 'pc_care' -> 'categories') = 'object'
  AND NOT (v.form_dsl -> 'pc_care' -> 'categories' ? 'fumigation');

UPDATE public.sop_definitions
SET description = 'Deworming, anti protozoan, ticks removal, hoof trimming, hair trimming and fumigation: what the operator captures (per animal, or per pen for fumigation), the questions answered at submit, and whether a tablet-in-feed deworming removes feed and water the evening before (required, optional or off, which categories, the evening, the removal card''s captures and questions). The capture mode, free-flow scan, whole-task submit and per-task verification stay the module''s.',
    updated_at = now()
WHERE code = 'pc_care.tasks';

-- 3. WHO PLANS IT: the ticks, for everyone the access cutover already migrated.
--
--    A person's modules are their TICKS (person_module_access). The role map in
--    capability_backfill.go is only what a NEW backfill writes, so without these rows every park
--    head, Health Director and Breeding Director already migrated would 403 on the planner routes
--    (the 000454 / 000245 defect). Keyed on the ROLE GRANT, the population the backfill writes:
--      park_head          -> phone: pc_care View + pc_fumigation View/Configure (no web bootstrap)
--      health_director    -> both surfaces: pc_care View + pc_fumigation View/Configure
--                            (maintainer 2026-09-30: the Health Director now has Preventive Care access)
--      breeding_director  -> both surfaces: pc_fumigation View/Configure (already reads pc_care)
--      ceo_internal       -> both surfaces: pc_fumigation View/Configure (the CEO floor row; the
--                            CEO already plans fumigation through pc_care.plan)
--    ADDITIVE ONLY (an existing row is left exactly as it is) and ledgered, so the Down path removes
--    exactly these rows and a tick an admin adds later survives a rollback. A person with no rows
--    at all is still on the role path and is not touched.
CREATE TABLE IF NOT EXISTS public.person_module_access_fumigation_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  surface             text NOT NULL,
  module_key          text NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id, surface, module_key)
);

WITH wanted (role, surface, module_key, capabilities) AS (
  VALUES
    ('park_head',         'mobile', 'pc_care',       ARRAY['view']::text[]),
    ('park_head',         'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('health_director',   'mobile', 'pc_care',       ARRAY['view']::text[]),
    ('health_director',   'web',    'pc_care',       ARRAY['view']::text[]),
    ('health_director',   'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('health_director',   'web',    'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('breeding_director', 'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('breeding_director', 'web',    'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('ceo_internal',      'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('ceo_internal',      'web',    'pc_fumigation', ARRAY['view', 'configure']::text[])
),
inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT ON (m.tenant_id, m.workforce_member_id, w.surface, w.module_key)
         m.tenant_id, m.workforce_member_id, w.surface, w.module_key, w.capabilities, now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
  JOIN wanted w ON w.role = g.role
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ORDER BY m.tenant_id, m.workforce_member_id, w.surface, w.module_key, cardinality(w.capabilities) DESC
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id, surface, module_key
)
INSERT INTO public.person_module_access_fumigation_backfill (tenant_id, workforce_member_id, surface, module_key)
SELECT tenant_id, workforce_member_id, surface, module_key FROM inserted
ON CONFLICT DO NOTHING;

-- 4. The same ticks as each job's DEFAULT, so a person given one of these designations on /people
--    tomorrow is pre-filled with them. Additive and ledgered like the person rows.
CREATE TABLE IF NOT EXISTS public.designation_module_defaults_fumigation_backfill (
  designation_code text NOT NULL,
  surface          text NOT NULL,
  module_key       text NOT NULL,
  PRIMARY KEY (designation_code, surface, module_key)
);

WITH wanted (designation_code, surface, module_key, capabilities) AS (
  VALUES
    ('park_head',         'mobile', 'pc_care',       ARRAY['view']::text[]),
    ('park_head',         'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('health_director',   'mobile', 'pc_care',       ARRAY['view']::text[]),
    ('health_director',   'web',    'pc_care',       ARRAY['view']::text[]),
    ('health_director',   'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('health_director',   'web',    'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('breeding_director', 'mobile', 'pc_fumigation', ARRAY['view', 'configure']::text[]),
    ('breeding_director', 'web',    'pc_fumigation', ARRAY['view', 'configure']::text[])
),
inserted AS (
  INSERT INTO public.designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
  SELECT w.designation_code, w.surface, w.module_key, w.capabilities, '{}'::text[]
  FROM wanted w
  JOIN public.designation_catalog d ON d.designation_code = w.designation_code
  ON CONFLICT (designation_code, surface, module_key) DO NOTHING
  RETURNING designation_code, surface, module_key
)
INSERT INTO public.designation_module_defaults_fumigation_backfill (designation_code, surface, module_key)
SELECT designation_code, surface, module_key FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.designation_module_defaults d
USING public.designation_module_defaults_fumigation_backfill b
WHERE d.designation_code = b.designation_code
  AND d.surface = b.surface
  AND d.module_key = b.module_key;
DROP TABLE IF EXISTS public.designation_module_defaults_fumigation_backfill;

DELETE FROM public.person_module_access p
USING public.person_module_access_fumigation_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = b.surface
  AND p.module_key = b.module_key;
DROP TABLE IF EXISTS public.person_module_access_fumigation_backfill;

UPDATE public.sop_versions v
SET form_dsl = v.form_dsl #- '{pc_care,categories,fumigation}'
FROM public.sop_definitions sd
WHERE sd.tenant_id = v.tenant_id
  AND sd.sop_id = v.sop_id
  AND sd.code = 'pc_care.tasks';

DELETE FROM public.pc_care_tasks WHERE category = 'fumigation';
DELETE FROM public.pc_care_rounds WHERE category = 'fumigation';

ALTER TABLE public.pc_care_rounds
  DROP CONSTRAINT IF EXISTS pc_care_rounds_category_check;

ALTER TABLE public.pc_care_rounds
  ADD CONSTRAINT pc_care_rounds_category_check CHECK (
    category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming')
  );

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'anti_protozoan', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine', 'feed_water_removal')
  );
