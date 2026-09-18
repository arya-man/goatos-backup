-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one new module; no vaccination/HRMS
-- seed contract, source fixture schema, or read-model change.
--
-- CONFIGURATION REACHES THE CEO/CXO WHO ALREADY HOLD IT ON THE ROLE (maintainer instruction
-- 2026-09-18). Since the 2026-08-24 cutover a migrated person's person_module_access rows
-- DECIDE (auth logs the denial as decided_by=person) and the role map in capability_backfill.go
-- is read only by the one-time backfill and its parity test. So the module shipped today reaches
-- NOBODY already migrated until a row exists: /admin/configuration/registers 403'd for the CEO
-- cohort on the first run against a real database. The 000321 (alerts) shape.
--
-- WHO GETS WHAT mirrors capability_backfill.go: ceo_internal -> configuration web
-- {view, configure}. Nobody else on a role; anyone else is ticked by HRMS on /people afterwards,
-- and that per-person tick -- not a job title -- is what puts the writes on the page.
--
-- ADDITIVE ONLY, and only for people the cutover already migrated (EXISTS person_access):
-- someone with no rows at all is still on the role fallback path, and writing a single row
-- would take them off it holding `configuration` and nothing else.
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT m.tenant_id, m.workforce_member_id, 'web', 'configuration', ARRAY['view', 'configure']::text[]
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
GROUP BY m.tenant_id, m.workforce_member_id
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- +goose Down
-- Removes only what Up could have written: the configuration web rows. A row HRMS ticked by hand
-- after deploy is indistinguishable and goes with it; that is the accepted cost of a repair Down.
DELETE FROM public.person_module_access
WHERE surface = 'web' AND module_key = 'configuration';
