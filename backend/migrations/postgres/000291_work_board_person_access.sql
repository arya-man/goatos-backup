-- +goose Up
-- seed-fixture-guard:ignore: repairs per-person access rows for a new module; no source
-- fixture schema, HRMS seed contract, or read-model change.
-- Give the people who already hold the Work Board's roles the `work_board` tick the
-- 2026-08-24 cutover could never have written (maintainer decision 2026-09-10, the Work
-- Board Build Plan).
--
-- WHY THIS EXISTS. Person rows DECIDE after the cutover (auth logs the denial as
-- decided_by=person), and the role map in capability_backfill.go is dead data read only
-- by the one-time backfill. A module added to that map reaches nobody already migrated:
-- every CEO, director, park head and operator kept a stored row set with no `work_board`
-- in it, and both board routes 403'd for the CEO herself on the first live call. This is
-- the 000245 (sale_allocation) shape, repeated for the same reason.
--
-- WHO GETS WHAT mirrors flatRoleAssignments exactly:
--   ceo_internal and the six director roles  web + mobile  {view, oversee}
--   park_head                                mobile        {view, oversee}
--   operator                                 mobile        {view}
-- Keyed on the ACTIVE ROLE GRANT, not on any stored module row, so a senior person who
-- holds some other module's rows through a vertical does not gain the board by accident.
--
-- ADDITIVE ONLY: a person who already has a work_board row (ticked on /people, or created
-- after this landed) is left exactly as they are. Only people the cutover migrated are
-- touched; someone with no rows at all is still on the role fallback path, and one row
-- here would take them off it holding work_board and nothing else.
WITH grants AS (
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, g.role
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
),
rows AS (
  SELECT tenant_id, workforce_member_id, 'web'::text AS surface, ARRAY['view','oversee']::text[] AS capabilities
  FROM grants WHERE role IN ('ceo_internal','pc_director','growth_director','feed_director','health_director','procurement_director','breeding_director')
  UNION
  SELECT tenant_id, workforce_member_id, 'mobile', ARRAY['view','oversee']::text[]
  FROM grants WHERE role IN ('ceo_internal','pc_director','growth_director','feed_director','health_director','procurement_director','breeding_director','park_head')
  UNION
  SELECT tenant_id, workforce_member_id, 'mobile', ARRAY['view']::text[]
  FROM grants WHERE role = 'operator'
),
-- A person holding two of the above (a director who also carries an operator grant) gets
-- the WIDER capability set on that surface, never two rows.
merged AS (
  SELECT tenant_id, workforce_member_id, surface,
         CASE WHEN bool_or('oversee' = ANY(capabilities)) THEN ARRAY['view','oversee']::text[] ELSE ARRAY['view']::text[] END AS capabilities
  FROM rows
  GROUP BY tenant_id, workforce_member_id, surface
)
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT tenant_id, workforce_member_id, surface, 'work_board', capabilities
FROM merged
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- +goose Down
-- Removes only rows this migration could have written; a tick an admin made on /people
-- is indistinguishable from one written here, the honest cost of an additive repair.
DELETE FROM public.person_module_access WHERE module_key = 'work_board';
