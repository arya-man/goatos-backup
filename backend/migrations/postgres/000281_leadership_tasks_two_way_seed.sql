-- +goose Up
-- Leadership Tasks extension (Manju ask, 2026-09-08): keep the module capability-driven,
-- but seed existing OCI/staging people so CEO/CXO can assign work downward and park heads
-- can receive/act on tasks from their phone. No leadership_tasks rows are hand-created here;
-- operational task data must still be produced by the app write path.

WITH role_people AS (
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, g.role
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('ceo_internal', 'park_head')
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1
      FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
),
desired AS (
  SELECT tenant_id, workforce_member_id, 'mobile'::text AS surface, 'leadership_tasks'::text AS module_key,
         CASE
           WHEN role = 'ceo_internal' THEN ARRAY['view','oversee','configure']::text[]
           ELSE ARRAY['view','oversee']::text[]
         END AS capabilities
  FROM role_people
)
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT tenant_id, workforce_member_id, surface, module_key, capabilities
FROM desired
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO UPDATE
SET capabilities = (
  SELECT array_agg(DISTINCT cap ORDER BY cap)
  FROM unnest(public.person_module_access.capabilities || EXCLUDED.capabilities) AS cap
);

INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view','oversee']::text[]
FROM public.workforce_members m
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  AND lower(btrim(m.email)) IN ('ravi@mesha.sg', 'manju@mesha.sg', 'aryaman@mesha.sg')
  AND EXISTS (
    SELECT 1
    FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO UPDATE
SET capabilities = (
  SELECT array_agg(DISTINCT cap ORDER BY cap)
  FROM unnest(public.person_module_access.capabilities || EXCLUDED.capabilities) AS cap
);

-- +goose Down
SELECT 1;
