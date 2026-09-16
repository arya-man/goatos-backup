-- +goose Up
-- seed-fixture-guard:ignore: repairs per-person access rows for a new module; no source
-- fixture schema, HRMS seed contract, or read-model change.
-- Give the people who already hold the director/CEO roles the `alerts` tick the role map says
-- they hold (maintainer decision 2026-09-16).
--
-- WHY THIS EXISTS -- the 000245 pattern. Since the 2026-08-24 cutover a person's stored
-- person_module_access rows DECIDE their permissions (auth logs the denial as
-- decided_by=person); the role map in capability_backfill.go is read by the one-time backfill
-- and the parity test, never at request time. So a module added to that map reaches nobody who
-- was already migrated: on the day the Alerts page shipped, /alerts/rows 403'd for the CEO
-- herself and the sidebar item stayed disabled for every director.
--
-- WHO GETS WHAT, mirroring the role map exactly (the same population the backfill would have
-- written on cutover day):
--   ceo_internal                          -> alerts web {view, configure}
--   pc/growth/feed/health/procurement/    -> alerts web {view}
--   breeding director
-- Configure stays with the CEO row and whoever HRMS ticks on /people afterwards: that per-person
-- tick, not a job title, is what puts the Configure button on the page (alerts.configure).
--
-- Only people the cutover already migrated (they have person_access rows). Someone with no
-- rows at all is still on the role fallback path, and writing a single row would take them
-- off it holding `alerts` and nothing else.
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT m.tenant_id, m.workforce_member_id, 'web', 'alerts',
       CASE WHEN bool_or(g.role = 'ceo_internal') THEN ARRAY['view', 'configure']::text[] ELSE ARRAY['view']::text[] END
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role IN ('ceo_internal', 'pc_director', 'growth_director', 'feed_director', 'health_director', 'procurement_director', 'breeding_director')
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
-- Removes only what Up could have written: the alerts web rows. A row HRMS ticked by hand after
-- deploy is indistinguishable and goes with it; that is the accepted cost of a repair Down.
DELETE FROM public.person_module_access
WHERE surface = 'web' AND module_key = 'alerts';
