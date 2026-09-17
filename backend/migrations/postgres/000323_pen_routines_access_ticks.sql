-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one new module; no vaccination/HRMS
-- seed contract, source fixture schema, or read-model change.
--
-- ROUTINES REACHES THE PEOPLE WHO ALREADY HOLD IT ON THE ROLE (maintainer instruction
-- 2026-09-16, docs/decisions/pen-routines.md). Since the 2026-08-24 cutover a migrated person's
-- person_module_access rows DECIDE (auth logs the denial as decided_by=person) and the role map
-- in capability_backfill.go is dead data read only by the one-time backfill and its parity test.
-- So a module shipped today reaches NOBODY already migrated until a row exists: the park head
-- would open the phone and find no Routines module, and every /app/pen-routines* route would
-- 403 for the exact people the module was built for. The 000245 (sale_allocation) and 000302
-- (animal_purchases) shape, with the 000302 ledger so the Down path removes exactly these rows.
--
-- WHO GETS WHAT (2026-09-17 revision: routines are assigned BY ROLE, and every role in the
-- vocabulary -- the CXO included -- may be handed one, so every holder must be able to open it):
--
--   park_head                         mobile {view, do}
--   the six director roles            mobile {view, do}     web {view}   (the /routines page)
--   ceo_internal                      mobile {view, do}     web {view, configure}
--
-- A web `do` would carry pen_routines.execute onto a surface with no execute screen, so a
-- director's web row is view only. The CXO's web row carries configure: the desk writes the
-- rules. The CEO floor (permissions/ceo_floor.go) would serve the web half from the role anyway;
-- the row is written so the stored ticks on /people say what the person can do, and the PHONE
-- half is not floor-served at all.
--
-- The eight roles are penroutines/domain.AssignableRoles: park_head, pc_director,
-- breeding_director, growth_director, feed_director, health_director, procurement_director,
-- ceo_internal. Every write is keyed on the ACTIVE ROLE GRANT (user_scope_grants), the same
-- population the backfill would have written, never on a stored row of another module. A
-- person holding several of the roles gets ONE row per surface (GROUP BY the member), the CXO
-- capabilities winning on web.
--
-- ADDITIVE ONLY. A person who already holds a pen_routines row -- ticked on /people since the
-- module shipped -- is left exactly as they are (ON CONFLICT DO NOTHING). Only people the
-- cutover already migrated (EXISTS person_access) are touched: someone with no rows at all is
-- still on the role fallback path, and writing a single row would take them OFF that path and
-- leave them holding Routines and nothing else.
CREATE TABLE IF NOT EXISTS public.person_module_access_pen_routines_mobile_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

CREATE TABLE IF NOT EXISTS public.person_module_access_pen_routines_web_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

-- (a) The PHONE module: every role a routine can be assigned to works it there.
WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'pen_routines', ARRAY['view','do']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('park_head',
                  'pc_director', 'growth_director', 'feed_director',
                  'health_director', 'breeding_director', 'procurement_director',
                  'ceo_internal')
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_pen_routines_mobile_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- (b) The WEB /routines page: directors read what the park owes; the CXO desk also authors it.
WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT m.tenant_id, m.workforce_member_id, 'web', 'pen_routines',
         CASE WHEN bool_or(g.role = 'ceo_internal') THEN ARRAY['view','configure']::text[]
              ELSE ARRAY['view']::text[] END,
         now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('pc_director', 'growth_director', 'feed_director',
                  'health_director', 'breeding_director', 'procurement_director',
                  'ceo_internal')
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  GROUP BY m.tenant_id, m.workforce_member_id
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_pen_routines_web_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
-- Removes only the rows THIS migration wrote (the ledger), never a tick an admin made on /people.
DELETE FROM public.person_module_access p
USING public.person_module_access_pen_routines_web_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'web'
  AND p.module_key = 'pen_routines';
DELETE FROM public.person_module_access p
USING public.person_module_access_pen_routines_mobile_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'mobile'
  AND p.module_key = 'pen_routines';
DROP TABLE IF EXISTS public.person_module_access_pen_routines_web_backfill;
DROP TABLE IF EXISTS public.person_module_access_pen_routines_mobile_backfill;
