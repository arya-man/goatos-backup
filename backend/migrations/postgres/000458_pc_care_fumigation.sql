-- +goose Up
-- 000458_pc_care_fumigation.sql
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
--   1. (The category CHECKs are 000457, a NO TRANSACTION migration of their own.)
--   2. The seeded fumigation card is added IN PLACE to every pc_care.tasks version that lacks
--      it (the weighing 000315 shape). No task is pinned to fumigation before this migration,
--      so no task's behaviour moves; the version keeps its number. Without it the SOP editor
--      would refuse to save a new version ("every work category needs its capture card") and a
--      published version would lean on the Go seed fallback for the new card. The card is
--      embedded verbatim and pinned by TestMigrationEmbedsTheSeededFumigationCard.
--   3. / 4. The planners' per-person ticks and job defaults (below), additive and ledgered.
--   5. A top-level Fumigation list in Configuration › Items & categories holding Virufix (ml)
--      and the Fumigator (piece), ledgered.
--
-- seed-fixture-guard:ignore: adds an operational PC Care work category and its seeded SOP card;
-- no vaccination / HRMS / goats schema moves.



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

-- 5. A FUMIGATION LIST IN THE ITEM CATALOGUE (maintainer decision 2026-09-30). Configuration ›
--    Items & categories gets its own top-level "Fumigation" list, beside Medicines, Vaccines and
--    Consumables, holding what the work uses: Virufix (the disinfectant, ml, the dose in its
--    notes) and the Fumigator (the spray machine, counted in pieces). A list's whole subtree is
--    one kind, so the list is CONSUMABLE -- not Medicine, which would put Virufix in the Health
--    treatment medicine picker and file a machine as a medicine. The item codes are the ones the
--    screen makes from the names (ITM-VIRUFIX, ITM-FUMIGATOR), so every row edits like a
--    hand-made one. A tenant that already has a top-level Fumigation list keeps it and the items
--    go into it; an item that already exists by name or code is left where it is. Ledgered.
CREATE TABLE IF NOT EXISTS public.item_categories_fumigation_backfill (
  tenant_id   uuid NOT NULL,
  category_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, category_id)
);

WITH inserted AS (
  INSERT INTO public.item_categories (tenant_id, name, normalized_name, item_kind, sort_order)
  SELECT t.tenant_id, 'Fumigation', 'fumigation', 'consumable', 65
  FROM public.tenants t
  WHERE NOT EXISTS (
    SELECT 1 FROM public.item_categories c
    WHERE c.tenant_id = t.tenant_id AND c.parent_category_id IS NULL AND c.normalized_name = 'fumigation'
  )
  RETURNING tenant_id, category_id
)
INSERT INTO public.item_categories_fumigation_backfill (tenant_id, category_id)
SELECT tenant_id, category_id FROM inserted
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS public.inventory_items_fumigation_backfill (
  tenant_id uuid NOT NULL,
  item_id   uuid NOT NULL,
  PRIMARY KEY (tenant_id, item_id)
);

WITH inserted AS (
  INSERT INTO public.inventory_items (tenant_id, item_code, name, category, base_unit, category_id, context)
  SELECT c.tenant_id, v.code, v.name, 'consumable', v.unit, c.category_id, jsonb_build_object('notes', v.notes)
  FROM public.item_categories c
  CROSS JOIN (VALUES
    ('ITM-VIRUFIX',   'Virufix',   'ml',    'Pen disinfectant for Preventive Care fumigation: 5 ml per litre of water, sprayed across the pen.'),
    ('ITM-FUMIGATOR', 'Fumigator', 'piece', 'Spray machine for Preventive Care fumigation of a pen.')
  ) AS v(code, name, unit, notes)
  WHERE c.parent_category_id IS NULL
    AND c.normalized_name = 'fumigation'
    AND c.item_kind = 'consumable'
    AND c.status = 'active'
    AND NOT EXISTS (
      SELECT 1 FROM public.inventory_items i
      WHERE i.tenant_id = c.tenant_id AND (lower(i.name) = lower(v.name) OR i.item_code = v.code)
    )
  ON CONFLICT (tenant_id, item_code) DO NOTHING
  RETURNING tenant_id, item_id
)
INSERT INTO public.inventory_items_fumigation_backfill (tenant_id, item_id)
SELECT tenant_id, item_id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.inventory_items i
USING public.inventory_items_fumigation_backfill b
WHERE i.tenant_id = b.tenant_id AND i.item_id = b.item_id;
DROP TABLE IF EXISTS public.inventory_items_fumigation_backfill;
DELETE FROM public.item_categories c
USING public.item_categories_fumigation_backfill b
WHERE c.tenant_id = b.tenant_id AND c.category_id = b.category_id
  AND NOT EXISTS (SELECT 1 FROM public.inventory_items i WHERE i.tenant_id = c.tenant_id AND i.category_id = c.category_id)
  AND NOT EXISTS (SELECT 1 FROM public.item_categories x WHERE x.tenant_id = c.tenant_id AND x.parent_category_id = c.category_id);
DROP TABLE IF EXISTS public.item_categories_fumigation_backfill;

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
