-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one module surface; no vaccination/HRMS
-- seed contract change.
--
-- ANIMAL PURCHASES ON THE PHONE FOR EVERYONE WHO ALREADY HAS PROCUREMENT THERE (maintainer decision
-- 2026-09-13). The third tab of the Procurement phone module rides its own module key
-- (animal_purchases) so the CEO's web review Oversee level never carries the phone's Write. A
-- person's phone modules are their TICKS (person_module_access, 000219) and a tick never widens
-- itself, so every person already holding a mobile `vendors` tick would see no Animal purchases tab
-- until an admin re-ticked them. This copies each mobile `vendors` tick onto a mobile
-- `animal_purchases` tick, once -- view stays view, do/oversee become do (the module's mobile levels
-- are view and do). The 000251 shape, with the same ledger so the Down path removes exactly these.
CREATE TABLE IF NOT EXISTS public.person_module_access_animal_purchases_mobile_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

CREATE TABLE IF NOT EXISTS public.person_module_access_animal_purchases_web_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, updated_by, pages)
  SELECT v.tenant_id, v.workforce_member_id, 'mobile', 'animal_purchases',
         -- RECORDING is the buying desk's, never the CEO's (the toxin separation of duty: the
         -- person who accepts an animal must not be the one who filmed it). A CEO holds the
         -- Procurement phone module too, so a blanket copy of the vendors tick would hand the
         -- CEO `do`; the decide-side grant is what excludes them here.
         CASE WHEN v.capabilities && ARRAY['do','oversee']::text[]
               AND NOT EXISTS (
                 SELECT 1 FROM public.workforce_members m
                 JOIN public.user_scope_grants g ON g.tenant_id = m.tenant_id AND g.user_id = m.user_id
                 WHERE m.tenant_id = v.tenant_id AND m.workforce_member_id = v.workforce_member_id
                   AND g.status = 'active' AND (g.valid_to IS NULL OR g.valid_to > now())
                   AND g.role = 'ceo_internal')
              THEN ARRAY['view','do']::text[] ELSE ARRAY['view']::text[] END,
         now(), v.updated_by, '{}'::text[]
  FROM public.person_module_access v
  WHERE v.surface = 'mobile' AND v.module_key = 'vendors' AND cardinality(v.capabilities) > 0
  ON CONFLICT DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_animal_purchases_mobile_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- THE CEO'S WEB REVIEW PAGE (the 000245 shape). Person rows DECIDE at request time and the
-- role map is dead data after the 2026-08-24 cutover, so a module added today reaches nobody
-- already migrated until a row exists. Every active ceo_internal grant holder who has been
-- migrated gets the web `animal_purchases` row at view + oversee (the CEO decides, never
-- records). Additive only: a hand-ticked row is left as it is.
WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'web', 'animal_purchases', ARRAY['view','oversee']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role = 'ceo_internal'
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
INSERT INTO public.person_module_access_animal_purchases_web_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.person_module_access p
USING public.person_module_access_animal_purchases_web_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'web'
  AND p.module_key = 'animal_purchases';
DELETE FROM public.person_module_access p
USING public.person_module_access_animal_purchases_mobile_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'mobile'
  AND p.module_key = 'animal_purchases';
DROP TABLE IF EXISTS public.person_module_access_animal_purchases_web_backfill;
DROP TABLE IF EXISTS public.person_module_access_animal_purchases_mobile_backfill;
