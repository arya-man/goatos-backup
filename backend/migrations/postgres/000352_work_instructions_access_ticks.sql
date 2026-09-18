-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one new phone module; no
-- vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- WORK INSTRUCTIONS REACH EVERY ROLE THAT HOLDS THEM ON THE JOB (SOP studio phase 2, maintainer
-- decision 2026-09-18). Since the 2026-08-24 cutover a migrated person's person_module_access rows
-- DECIDE, so the module shipped today reaches nobody already migrated until a row exists (the
-- 000331 pen-routines shape). WHO mirrors capability_backfill.go: operators, park heads, every
-- director and ceo_internal get the PHONE module with Do. ADDITIVE ONLY, and only for people the
-- cutover already migrated (EXISTS person_access).
CREATE TABLE IF NOT EXISTS public.person_module_access_work_instructions_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'work_instructions', ARRAY['do']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('operator', 'park_head',
                  'pc_director', 'growth_director', 'feed_director',
                  'health_director', 'breeding_director', 'procurement_director',
                  'ceo_internal')
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_work_instructions_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.person_module_access a
USING public.person_module_access_work_instructions_backfill b
WHERE a.tenant_id = b.tenant_id AND a.workforce_member_id = b.workforce_member_id
  AND a.surface = 'mobile' AND a.module_key = 'work_instructions';
DROP TABLE IF EXISTS public.person_module_access_work_instructions_backfill;
