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
--
-- The rows THIS migration writes are remembered in small ledgers, so the Down path removes exactly
-- those and nothing else. A work_board grant that existed before, or that an admin adds by hand
-- afterwards, is real access and must survive rollback.
CREATE TABLE IF NOT EXISTS public.person_module_access_work_board_backfill (
  tenant_id            uuid NOT NULL,
  workforce_member_id  uuid NOT NULL,
  surface              text NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id, surface)
);

CREATE TABLE IF NOT EXISTS public.department_module_grants_work_board_backfill (
  tenant_id      uuid NOT NULL,
  department_id  uuid NOT NULL,
  PRIMARY KEY (tenant_id, department_id)
);

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
,
inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
  SELECT tenant_id, workforce_member_id, surface, 'work_board', capabilities
  FROM merged
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id, surface
)
INSERT INTO public.person_module_access_work_board_backfill (tenant_id, workforce_member_id, surface)
SELECT tenant_id, workforce_member_id, surface FROM inserted
ON CONFLICT DO NOTHING;

-- FIELD PRINCIPALS are offered a phone module through their DEPARTMENT's module grant,
-- narrowed by their own mobile tick (workforce/app.candidateModuleKeysFrom). Every
-- department that already carries a phone module gets work_board too, so an operator's
-- own rows are reachable from the bar; the mobile tick written above is what keeps it.
WITH inserted AS (
  INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
  SELECT DISTINCT g.tenant_id, g.department_id, 'work_board', 'active'
  FROM public.department_module_grants g
  WHERE g.status = 'active'
    AND NOT EXISTS (
      SELECT 1 FROM public.department_module_grants x
      WHERE x.tenant_id = g.tenant_id AND x.department_id = g.department_id AND x.module_key = 'work_board'
    )
  RETURNING tenant_id, department_id
)
INSERT INTO public.department_module_grants_work_board_backfill (tenant_id, department_id)
SELECT tenant_id, department_id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
-- Only the rows the Up path inserted (the ledgers); pre-existing or hand-added grants stay.
DELETE FROM public.person_module_access p
USING public.person_module_access_work_board_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = b.surface
  AND p.module_key = 'work_board';

DELETE FROM public.department_module_grants g
USING public.department_module_grants_work_board_backfill b
WHERE g.tenant_id = b.tenant_id
  AND g.department_id = b.department_id
  AND g.module_key = 'work_board';

DROP TABLE IF EXISTS public.department_module_grants_work_board_backfill;
DROP TABLE IF EXISTS public.person_module_access_work_board_backfill;
