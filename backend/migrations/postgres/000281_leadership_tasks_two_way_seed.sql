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
         ARRAY(
           SELECT DISTINCT cap
           FROM unnest(
             array_cat(
               ARRAY['view','oversee']::text[],
               CASE
                 WHEN bool_or(role = 'ceo_internal') THEN ARRAY['configure']::text[]
                 ELSE ARRAY[]::text[]
               END
             )
           ) AS cap
           ORDER BY cap
         ) AS capabilities
  FROM role_people
  GROUP BY tenant_id, workforce_member_id
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
UPDATE public.person_module_access a
SET capabilities = ARRAY(
  SELECT cap
  FROM unnest(a.capabilities) AS cap
  WHERE cap <> 'configure'
  ORDER BY cap
)
USING public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role = 'ceo_internal'
WHERE m.tenant_id = a.tenant_id
  AND m.workforce_member_id = a.workforce_member_id
  AND m.status = 'active'
  AND a.surface = 'mobile'
  AND a.module_key = 'leadership_tasks'
  AND 'configure' = ANY (a.capabilities);

DELETE FROM public.person_module_access a
USING public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role = 'park_head'
WHERE m.tenant_id = a.tenant_id
  AND m.workforce_member_id = a.workforce_member_id
  AND m.status = 'active'
  AND a.surface = 'mobile'
  AND a.module_key = 'leadership_tasks'
  AND a.capabilities <@ ARRAY['view','oversee']::text[];

DELETE FROM public.person_module_access a
USING public.workforce_members m
WHERE m.tenant_id = a.tenant_id
  AND m.workforce_member_id = a.workforce_member_id
  AND m.status = 'active'
  AND lower(btrim(m.email)) IN ('ravi@mesha.sg', 'manju@mesha.sg', 'aryaman@mesha.sg')
  AND a.surface = 'mobile'
  AND a.module_key = 'leadership_tasks'
  AND a.capabilities <@ ARRAY['view','oversee']::text[];
