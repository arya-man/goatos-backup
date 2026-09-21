-- +goose Up
-- seed-fixture-guard:ignore: removes per-person access rows for one retired phone module; no
-- vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- WORK INSTRUCTIONS IS RETIRED FROM THE PHONE (maintainer decision 2026-09-21, superseding the
-- mobile half of the 2026-09-18 SOP studio phase 2 decision). The module reached every operator,
-- park head, director and CXO through 000362's backfill, so retiring the registry entry alone
-- would leave a stored tick pointing at a module the bootstrap no longer composes. This deletes
-- those mobile rows.
--
-- SCOPE: the phone feature. The AUTHORING half is untouched -- Configuration -> Work instructions
-- still writes general SOPs (sop.read / sop.write) and the seeded Gate visitor check still exists
-- -- but nothing starts a run any more: the same change retires the module, the
-- work_instructions.execute permission, its /people capability row, the Android screen, and the
-- two endpoints only it called (GET /app/sops/general, POST /app/workflows/start).
DELETE FROM public.person_module_access
WHERE surface = 'mobile' AND module_key = 'work_instructions';

DROP TABLE IF EXISTS public.person_module_access_work_instructions_backfill;

-- +goose Down
-- Restores the stored ticks for the same audience 000362 named. The Down of the code half (the
-- registry entry, the permission and the capability row) is a revert, not a migration.
-- Restores the module for the same audience 000362 named: active operators, park heads, every
-- director and ceo_internal, and only for people the 2026-08-24 cutover already migrated. The
-- exact per-person set 000362 wrote is not recoverable once its receipt table is dropped, so the
-- Down re-derives it from the same rule rather than claiming to reproduce it row for row.
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
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;
