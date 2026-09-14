-- +goose Up
-- seed-fixture-guard:ignore: per-person access ticks for the market survey (000304); the
-- workforce_members join only maps a grant's user to its roster row and changes no
-- vaccination/HRMS seed source, fixture schema, or read-model.
-- MARKET SURVEY ACCESS TICKS (maintainer decision 2026-09-14), split out of 000304 so the
-- schema migration carries only DDL and this one carries only the additive person-row repairs.
-- Person rows DECIDE at request time after the 2026-08-24 cutover, so a module or page shipped
-- today reaches nobody already migrated until a row exists (the 000245 shape).

-- The Sales module gained a CONFIGURE level today (what the market is asked: cities and
-- questions), held by the CEO/CXO and the Procurement Director. Person rows DECIDE after the
-- 2026-08-24 cutover and the role map is dead data for anyone already migrated, so the two roles'
-- existing web `sales` rows get the level here -- the 000245 shape, additive only. The CEO floor
-- makes ceo_internal need no row; the Procurement Director does.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=additive-capability-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access pma
   SET capabilities = array_append(pma.capabilities, 'configure')
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('ceo_internal', 'procurement_director')
 WHERE pma.tenant_id = m.tenant_id
   AND pma.workforce_member_id = m.workforce_member_id
   AND pma.surface = 'web'
   AND pma.module_key = 'sales'
   AND m.status = 'active'
   AND NOT ('configure' = ANY (pma.capabilities));

-- THE REPORTER'S PHONE TICK. The market_reporter grant is seeded per person by name
-- (seed-stg-login-grants perPersonGrants: today the Procurement Director), but person rows DECIDE
-- at request time for anyone already migrated, so the grant alone opens nothing until a mobile
-- `market_survey` row exists. This writes that row, once, for every migrated person holding the
-- procurement_director grant today -- the one desk the maintainer named -- and for any
-- market_reporter grant already present. It is a one-time repair for people migrated before the
-- module existed; from here on /people ticks it like any other module, and a FUTURE procurement
-- director inherits nothing from this statement because it runs once.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=one-time-mobile-tick-for-the-named-reporter expiry=2026-12-31
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'market_survey', ARRAY['do']::text[], now(), '{}'::text[]
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role IN ('procurement_director', 'market_reporter')
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- Give the people who already hold the web Sales module the new Market analytics page tick, the
-- 000245 shape: person rows DECIDE after the 2026-08-24 cutover, and a person whose sales row
-- names specific pages would otherwise never see a page shipped after their rows were written.
-- Additive only; the CEO floor makes ceo_internal need no row.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=additive-page-tick-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = array_append(pages, 'sales-market-analytics')
 WHERE surface = 'web'
   AND module_key = 'sales'
   AND pages IS NOT NULL
   AND array_length(pages, 1) > 0
   AND NOT ('sales-market-analytics' = ANY (pages));

-- +goose Down
DELETE FROM public.person_module_access WHERE module_key = 'market_survey';
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=additive-page-tick-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = array_remove(pages, 'sales-market-analytics')
 WHERE surface = 'web' AND module_key = 'sales' AND 'sales-market-analytics' = ANY (pages);
-- The Sales `configure` capability is left in place: a row an admin ticked on /people is
-- indistinguishable from one written here, which is the honest cost of an additive repair.
