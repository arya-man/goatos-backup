-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows and one job default for one module surface;
-- no vaccination/HRMS seed contract change.
--
-- PARK HEADS TAG ANIMALS TO A SALE, AND NOTHING ELSE OF SALES (maintainer decision 2026-09-11).
--
-- Two parts, one decision. The weight typed per animal at tagging already has its column
-- (weight_kg, 000282); the park head records no price (maintainer decision 2026-09-26).
--
-- 1. THE TAG-ONLY PHONE MODULE FOR EVERY PARK HEAD ALREADY BACKFILLED.
--
--    A person's phone modules are their TICKS (person_module_access, 000219), and the role map
--    in capability_backfill.go is dead data after the 2026-08-24 cutover: adding
--    `sale_allocation` to the park_head role there changes nothing for a park head already
--    migrated, whose stored rows carry no such module (the exact defect 000245 repaired for the
--    CEO). Route auth decides from the person rows, so without this every park head would 403 on
--    the tagging routes and see no module. Keyed on the ROLE GRANT, the same population the
--    backfill would have written; only people the cutover already migrated (a person with no
--    rows at all is still on the role path and must stay there).
--
--    ADDITIVE ONLY, and remembered in a ledger so the Down path removes exactly these rows: a
--    tick an admin adds by hand afterwards is real per-person access and survives a rollback.
CREATE TABLE IF NOT EXISTS public.person_module_access_sale_tagging_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'sale_allocation', ARRAY['do']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role = 'park_head'
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
INSERT INTO public.person_module_access_sale_tagging_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- 2. THE SAME TICK AS THE PARK HEAD JOB'S DEFAULT. Picking "Park head" for a NEW person on /people
--    pre-fills from designation_module_defaults, so without this row a park head hired tomorrow
--    would reach no tagging screen until somebody remembered to tick it by hand. Additive only
--    and ledgered like the person rows: an admin who later edits the default keeps the edit.
CREATE TABLE IF NOT EXISTS public.designation_module_defaults_sale_tagging_backfill (
  designation_code text PRIMARY KEY
);

WITH inserted AS (
  INSERT INTO public.designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
  SELECT d.designation_code, 'mobile', 'sale_allocation', ARRAY['do']::text[], '{}'::text[]
  FROM public.designation_catalog d
  WHERE d.designation_code = 'park_head'
  ON CONFLICT (designation_code, surface, module_key) DO NOTHING
  RETURNING designation_code
)
INSERT INTO public.designation_module_defaults_sale_tagging_backfill (designation_code)
SELECT designation_code FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.person_module_access p
USING public.person_module_access_sale_tagging_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'mobile'
  AND p.module_key = 'sale_allocation';
DROP TABLE IF EXISTS public.person_module_access_sale_tagging_backfill;
DELETE FROM public.designation_module_defaults d
USING public.designation_module_defaults_sale_tagging_backfill b
WHERE d.designation_code = b.designation_code
  AND d.surface = 'mobile'
  AND d.module_key = 'sale_allocation';
DROP TABLE IF EXISTS public.designation_module_defaults_sale_tagging_backfill;
