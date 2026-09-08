-- +goose Up
-- Leadership Tasks extension (Manju ask, 2026-09-08): keep the module capability-driven,
-- but seed existing OCI/staging people so CEO/CXO can assign work downward, directors can
-- still raise the cross-leadership asks they already hold by role, and park heads can
-- receive/act on tasks from their phone. Active employees are seeded as receivers, so the
-- assignee picker covers Manju's "director, park head, or employee" path.
-- No leadership_tasks rows are hand-created here;
-- operational task data must still be produced by the app write path.
--
-- Rollback safety follows the 000251/000272 access-backfill pattern, with one extra
-- wrinkle: this migration both inserts new rows and widens existing rows. Inserted rows
-- are tracked row-wise; widened rows are tracked capability-wise so Down removes only
-- the exact capability elements this Up added.
CREATE TABLE IF NOT EXISTS public.person_module_access_leadership_tasks_two_way_rows (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  surface             text NOT NULL DEFAULT 'mobile',
  PRIMARY KEY (tenant_id, workforce_member_id, surface)
);

CREATE TABLE IF NOT EXISTS public.person_module_access_leadership_tasks_two_way_caps (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  surface             text NOT NULL DEFAULT 'mobile',
  capability          text NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id, surface, capability)
);

WITH role_people AS (
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, g.role
  FROM public.workforce_members m
  LEFT JOIN public.user_scope_grants g
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
     'breeding_director',
     'operator'
   )
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
	               array_cat(
	                 ARRAY['view','oversee']::text[],
	                 CASE
	                   WHEN COALESCE(bool_or(role IN (
	                     'pc_director',
	                     'growth_director',
	                     'feed_director',
	                     'health_director',
	                     'procurement_director',
	                     'breeding_director'
	                   )), false) THEN ARRAY['do']::text[]
	                   ELSE ARRAY[]::text[]
	                 END
	               ),
	               CASE
	                  WHEN COALESCE(bool_or(role IN (
	                    'ceo_internal',
	                    'pc_director',
	                    'growth_director',
	                    'feed_director',
	                    'health_director',
	                    'procurement_director',
	                    'breeding_director'
	                  )), false) THEN ARRAY['configure']::text[]
	                  ELSE ARRAY[]::text[]
	                END
	             )
	           ) AS cap
           ORDER BY cap
         ) AS capabilities
  FROM role_people
  GROUP BY tenant_id, workforce_member_id
  UNION ALL
  SELECT tenant_id, workforce_member_id, 'web'::text AS surface, 'leadership_tasks'::text AS module_key,
         CASE
           WHEN COALESCE(bool_or(role = 'ceo_internal'), false) THEN ARRAY['view','oversee','configure']::text[]
           ELSE ARRAY['view','configure']::text[]
         END AS capabilities
  FROM role_people
  GROUP BY tenant_id, workforce_member_id
  HAVING COALESCE(bool_or(role IN (
    'ceo_internal',
    'pc_director',
    'growth_director',
    'feed_director',
    'health_director',
    'procurement_director',
    'breeding_director'
  )), false)
),
missing_caps AS (
  SELECT d.tenant_id, d.workforce_member_id, d.surface, cap.capability
  FROM desired d
  JOIN public.person_module_access a
    ON a.tenant_id = d.tenant_id
   AND a.workforce_member_id = d.workforce_member_id
   AND a.surface = d.surface
   AND a.module_key = d.module_key
  CROSS JOIN LATERAL unnest(d.capabilities) AS cap(capability)
  WHERE NOT cap.capability = ANY(a.capabilities)
),
inserted AS (
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT tenant_id, workforce_member_id, surface, module_key, capabilities
FROM desired
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO UPDATE
SET capabilities = (
  SELECT array_agg(DISTINCT cap ORDER BY cap)
  FROM unnest(public.person_module_access.capabilities || EXCLUDED.capabilities) AS cap
)
RETURNING tenant_id, workforce_member_id, surface, (xmax = 0) AS inserted
),
recorded_rows AS (
  INSERT INTO public.person_module_access_leadership_tasks_two_way_rows (tenant_id, workforce_member_id, surface)
  SELECT tenant_id, workforce_member_id, surface
  FROM inserted
  WHERE inserted
  ON CONFLICT DO NOTHING
)
INSERT INTO public.person_module_access_leadership_tasks_two_way_caps (tenant_id, workforce_member_id, surface, capability)
SELECT tenant_id, workforce_member_id, surface, capability
FROM missing_caps
ON CONFLICT DO NOTHING;

WITH desired AS (
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile'::text AS surface,
         'leadership_tasks'::text AS module_key, ARRAY['view','oversee']::text[] AS capabilities
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
),
missing_caps AS (
  SELECT d.tenant_id, d.workforce_member_id, d.surface, cap.capability
  FROM desired d
  JOIN public.person_module_access a
    ON a.tenant_id = d.tenant_id
   AND a.workforce_member_id = d.workforce_member_id
   AND a.surface = d.surface
   AND a.module_key = d.module_key
  CROSS JOIN LATERAL unnest(d.capabilities) AS cap(capability)
  WHERE NOT cap.capability = ANY(a.capabilities)
),
inserted AS (
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT tenant_id, workforce_member_id, surface, module_key, capabilities
FROM desired
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO UPDATE
SET capabilities = (
  SELECT array_agg(DISTINCT cap ORDER BY cap)
  FROM unnest(public.person_module_access.capabilities || EXCLUDED.capabilities) AS cap
)
RETURNING tenant_id, workforce_member_id, surface, (xmax = 0) AS inserted
),
recorded_rows AS (
  INSERT INTO public.person_module_access_leadership_tasks_two_way_rows (tenant_id, workforce_member_id, surface)
  SELECT tenant_id, workforce_member_id, surface
  FROM inserted
  WHERE inserted
  ON CONFLICT DO NOTHING
)
INSERT INTO public.person_module_access_leadership_tasks_two_way_caps (tenant_id, workforce_member_id, surface, capability)
SELECT tenant_id, workforce_member_id, surface, capability
FROM missing_caps
ON CONFLICT DO NOTHING;

-- +goose Down
UPDATE public.person_module_access a
SET capabilities = ARRAY(
  SELECT cap
  FROM unnest(a.capabilities) AS cap
  WHERE NOT EXISTS (
    SELECT 1
    FROM public.person_module_access_leadership_tasks_two_way_caps c
    WHERE c.tenant_id = a.tenant_id
      AND c.workforce_member_id = a.workforce_member_id
      AND c.surface = a.surface
      AND c.capability = cap
  )
  ORDER BY cap
)
WHERE a.module_key = 'leadership_tasks'
  AND EXISTS (
    SELECT 1
    FROM public.person_module_access_leadership_tasks_two_way_caps c
    WHERE c.tenant_id = a.tenant_id
      AND c.workforce_member_id = a.workforce_member_id
      AND c.surface = a.surface
      AND c.capability = ANY(a.capabilities)
  );

DELETE FROM public.person_module_access a
USING public.person_module_access_leadership_tasks_two_way_rows r
WHERE a.tenant_id = r.tenant_id
  AND a.workforce_member_id = r.workforce_member_id
  AND a.surface = r.surface
  AND a.module_key = 'leadership_tasks'
  AND NOT EXISTS (
    SELECT 1
    FROM public.person_module_access_leadership_tasks_two_way_caps c
    WHERE c.tenant_id = a.tenant_id
      AND c.workforce_member_id = a.workforce_member_id
      AND c.surface = a.surface
  );

DROP TABLE IF EXISTS public.person_module_access_leadership_tasks_two_way_caps;
DROP TABLE IF EXISTS public.person_module_access_leadership_tasks_two_way_rows;
