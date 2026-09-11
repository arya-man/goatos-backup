-- +goose Up
-- seed-fixture-guard:ignore: narrows per-person Tasks ticks for an existing module; no
-- source fixture schema, HRMS seed contract, or read-model change.
-- Tasks assignees are leadership only: CXOs, directors and park heads (maintainer decision
-- 2026-09-11, SUPERSEDING the "or employee" half of the 2026-09-08 two-way seed in 000285).
--
-- WHY THIS EXISTS
--
-- The assignee picker behind "+" on the phone's Tasks module is not a role list. It lists
-- every person whose own mobile row on /people carries the Tasks module at OVERSEE, and the
-- raise-time check reads the same tick inside the write (decision 2026-09-04). 000285 then
-- seeded EVERY active employee -- operators included -- at view+oversee so that a task could
-- be assigned "to a director, park head, or employee". On 2026-09-11 the maintainer, raising
-- a task from the phone, found the picker full of operators and ruled that a Task is
-- leadership work: it is assigned to a CXO, a director or a park head, never to an operator.
--
-- WHAT IT DOES, AND WHAT IT LEAVES ALONE
--
-- It removes the OVERSEE capability from the mobile Tasks row of a person who holds no
-- active leadership grant (ceo_internal, park_head, or any of the six director roles), and
-- ONLY where 000285 is what put that tick there: either 000285 inserted the row wholesale
-- (`..._two_way_rows`) or it widened an existing row with `oversee`
-- (`..._two_way_caps`). A tick an admin set by hand on /people is not the seed going stale;
-- it is a decision someone made on the access screen, and it stays. View is kept: the module
-- itself is not withdrawn here, only the ability to be assigned a task in it.
--
-- Leadership is keyed on the ROLE GRANT at migration time, the same population 000285 read.
-- A park head is leadership for this purpose because the maintainer named park heads in the
-- same breath as CXOs and directors.
--
-- Rollback safety follows the 000285 pattern: every (tenant, member, surface) this
-- migration narrows is recorded so Down restores exactly those ticks and no other.
CREATE TABLE IF NOT EXISTS public.person_module_access_leadership_tasks_assignee_narrowing (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  surface             text NOT NULL DEFAULT 'mobile',
  PRIMARY KEY (tenant_id, workforce_member_id, surface)
);

WITH leadership AS (
  SELECT DISTINCT m.tenant_id, m.workforce_member_id
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN (
     'ceo_internal',
     'park_head',
     'pc_director',
     'growth_director',
     'feed_director',
     'health_director',
     'procurement_director',
     'breeding_director'
   )
  WHERE m.user_id IS NOT NULL
),
seeded_by_two_way AS (
  SELECT tenant_id, workforce_member_id, surface
  FROM public.person_module_access_leadership_tasks_two_way_rows
  UNION
  SELECT tenant_id, workforce_member_id, surface
  FROM public.person_module_access_leadership_tasks_two_way_caps
  WHERE capability = 'oversee'
),
narrowed AS (
  UPDATE public.person_module_access a
  SET capabilities = array_remove(a.capabilities, 'oversee')
  FROM seeded_by_two_way s
  WHERE a.tenant_id = s.tenant_id
    AND a.workforce_member_id = s.workforce_member_id
    AND a.surface = s.surface
    AND a.surface = 'mobile'
    AND a.module_key = 'leadership_tasks'
    AND 'oversee' = ANY(a.capabilities)
    AND NOT EXISTS (
      SELECT 1 FROM leadership l
      WHERE l.tenant_id = a.tenant_id
        AND l.workforce_member_id = a.workforce_member_id
    )
  RETURNING a.tenant_id, a.workforce_member_id, a.surface
)
INSERT INTO public.person_module_access_leadership_tasks_assignee_narrowing (tenant_id, workforce_member_id, surface)
SELECT tenant_id, workforce_member_id, surface
FROM narrowed
ON CONFLICT DO NOTHING;

-- +goose Down
-- Puts oversee back on exactly the rows this migration narrowed. A row deleted since is
-- simply not there to widen; a row an admin re-ticked by hand is not appended twice, which
-- is also what keeps the distinct-array CHECK satisfied.
UPDATE public.person_module_access a
SET capabilities = ARRAY(
  SELECT cap FROM unnest(a.capabilities || ARRAY['oversee']::text[]) AS cap
  GROUP BY cap ORDER BY cap
)
FROM public.person_module_access_leadership_tasks_assignee_narrowing n
WHERE a.tenant_id = n.tenant_id
  AND a.workforce_member_id = n.workforce_member_id
  AND a.surface = n.surface
  AND a.module_key = 'leadership_tasks'
  AND NOT 'oversee' = ANY(a.capabilities);

DROP TABLE IF EXISTS public.person_module_access_leadership_tasks_assignee_narrowing;
