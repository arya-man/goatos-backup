-- +goose Up
-- seed-fixture-guard:ignore: per-person access ticks for an existing leadership cohort; no vaccination HRMS seed rows are changed
--
-- THE LEADERSHIP DESK OVER TASKS IS AN EXPLICIT TICK (review of PR 295, 2026-09-18).
--
-- Until now "monitor" -- the tenant-wide Team progress scope, and the authority to edit,
-- move, cancel or comment on ANY open task -- was INFERRED in the http adapter as
-- leadership_tasks.raise AND leadership_tasks.act AND NOT pen_visits.execute. Any person
-- ticked Configure + Oversee on Tasks, without pen visits, silently became a tenant-wide
-- editor. It is now its own module, `leadership_tasks_monitor` (Oversee =
-- leadership_tasks.monitor), the `verification_policy` shape, and this migration hands it to
-- the people who already hold the CEO/CXO grant -- and to nobody else. A director keeps their
-- own tasks only, exactly as the domain rule always intended.
--
-- Per-person ticks NARROW ordinary users and never narrow the founder cohort (AGENTS.md,
-- "CEO/CXO visibility is not an HRMS clean-up task"), so this is additive: it writes only the
-- row that grants the desk, records what it wrote, and the Down removes only that.

CREATE TABLE IF NOT EXISTS public.person_module_access_leadership_tasks_monitor_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  surface             text NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id, surface)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, s.surface, 'leadership_tasks_monitor', ARRAY['oversee']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role = 'ceo_internal'
  CROSS JOIN (VALUES ('mobile'), ('web')) AS s(surface)
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id, surface
)
INSERT INTO public.person_module_access_leadership_tasks_monitor_backfill (tenant_id, workforce_member_id, surface)
SELECT tenant_id, workforce_member_id, surface FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
-- Removes only the rows THIS migration wrote (the ledger), never a tick an admin made on /people.
DELETE FROM public.person_module_access p
USING public.person_module_access_leadership_tasks_monitor_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = b.surface
  AND p.module_key = 'leadership_tasks_monitor';
DROP TABLE IF EXISTS public.person_module_access_leadership_tasks_monitor_backfill;
