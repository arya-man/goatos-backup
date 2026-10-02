-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one new capability module; no
-- vaccination/HRMS seed contract change.
--
-- THE BUYERS HALF OF THE VENDOR REGISTER IS ITS OWN MODULE (People / HRMS fixes, 2026-10-02).
--
-- One permission (procurement.vendor.read) opened BOTH halves of the one vendor register, so a
-- person given only the buyers (Sales > Vendors) needed the Vendors module, which also showed --
-- and served through the API -- every supplier. The buyers now ride `vendors_sales`
-- (procurement.vendor.sales.read / .write); `vendors` keeps the suppliers. Code:
-- permissions.capability.go, the vendor handler's side check.
--
-- Person rows DECIDE at request time and a module added today reaches nobody until a row exists
-- (the 000245 / 000302 shape), so this writes `vendors_sales` exactly where a person could reach
-- the buyers before, and nowhere else -- nobody gains or loses a screen they could see:
--
--   WEB    -- they held Vendors on the web AND Sales on the web with Sales > Vendors ticked (or
--             every Sales page: an empty list means every page). The sidebar has always resolved
--             from web rows alone, so this is exactly who saw the leaf.
--   PHONE  -- they held Sales on the phone and Vendors on either surface. The phone's Sales >
--             Vendors tab was gated on the whole permission set.
--
-- Capabilities are copied from the Vendors row on the same surface (else the other surface), so
-- an Oversee holder keeps the buyers' payment details exactly as before.
--
-- `sales-vendors` now belongs to the buyers module, so it is taken out of every Sales page list.
-- A web Sales row that was narrowed to that ONE page existed only to reach the buyers; it is
-- retired rather than left with an empty list, which would mean "every Sales page" and widen it.
--
-- Designation pre-fills (designation_module_defaults) get the same treatment, so picking a job
-- title on /people starts the same person the same way.
--
-- Every change is written to a ledger so the Down path reverses exactly these rows.

CREATE TABLE IF NOT EXISTS public.person_module_access_vendors_sales_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  surface             text NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id, surface)
);

CREATE TABLE IF NOT EXISTS public.person_module_access_sales_vendors_page_removed (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  capabilities        text[] NOT NULL,
  pages               text[] NOT NULL,
  updated_by          uuid,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

CREATE TABLE IF NOT EXISTS public.designation_module_defaults_vendors_sales_backfill (
  designation_code text NOT NULL,
  surface          text NOT NULL,
  PRIMARY KEY (designation_code, surface)
);

CREATE TABLE IF NOT EXISTS public.designation_module_defaults_sales_vendors_page_removed (
  designation_code text NOT NULL,
  surface          text NOT NULL,
  pages            text[] NOT NULL,
  PRIMARY KEY (designation_code, surface)
);

-- WEB: who saw Sales > Vendors.
WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, updated_by, pages)
  SELECT v.tenant_id, v.workforce_member_id, 'web', 'vendors_sales', v.capabilities, now(), v.updated_by, '{}'::text[]
  FROM public.person_module_access v
  JOIN public.person_module_access s
    ON s.tenant_id = v.tenant_id AND s.workforce_member_id = v.workforce_member_id
   AND s.surface = 'web' AND s.module_key = 'sales' AND cardinality(s.capabilities) > 0
   AND (cardinality(s.pages) = 0 OR 'sales-vendors' = ANY (s.pages))
  WHERE v.surface = 'web' AND v.module_key = 'vendors' AND cardinality(v.capabilities) > 0
  ON CONFLICT DO NOTHING
  RETURNING tenant_id, workforce_member_id, surface
)
INSERT INTO public.person_module_access_vendors_sales_backfill (tenant_id, workforce_member_id, surface)
SELECT tenant_id, workforce_member_id, surface FROM inserted
ON CONFLICT DO NOTHING;

-- PHONE: who had the Sales > Vendors tab.
WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, updated_by, pages)
  SELECT s.tenant_id, s.workforce_member_id, 'mobile', 'vendors_sales',
         COALESCE(
           (SELECT vm.capabilities FROM public.person_module_access vm
             WHERE vm.tenant_id = s.tenant_id AND vm.workforce_member_id = s.workforce_member_id
               AND vm.surface = 'mobile' AND vm.module_key = 'vendors' AND cardinality(vm.capabilities) > 0),
           (SELECT vw.capabilities FROM public.person_module_access vw
             WHERE vw.tenant_id = s.tenant_id AND vw.workforce_member_id = s.workforce_member_id
               AND vw.surface = 'web' AND vw.module_key = 'vendors' AND cardinality(vw.capabilities) > 0)),
         now(), s.updated_by, '{}'::text[]
  FROM public.person_module_access s
  WHERE s.surface = 'mobile' AND s.module_key = 'sales' AND cardinality(s.capabilities) > 0
    AND EXISTS (
      SELECT 1 FROM public.person_module_access v
       WHERE v.tenant_id = s.tenant_id AND v.workforce_member_id = s.workforce_member_id
         AND v.module_key = 'vendors' AND cardinality(v.capabilities) > 0)
  ON CONFLICT DO NOTHING
  RETURNING tenant_id, workforce_member_id, surface
)
INSERT INTO public.person_module_access_vendors_sales_backfill (tenant_id, workforce_member_id, surface)
SELECT tenant_id, workforce_member_id, surface FROM inserted
ON CONFLICT DO NOTHING;

-- `sales-vendors` leaves every Sales page list (ledgered first, then rewritten / retired).
INSERT INTO public.person_module_access_sales_vendors_page_removed (tenant_id, workforce_member_id, capabilities, pages, updated_by)
SELECT tenant_id, workforce_member_id, capabilities, pages, updated_by
FROM public.person_module_access
WHERE surface = 'web' AND module_key = 'sales' AND 'sales-vendors' = ANY (pages)
ON CONFLICT DO NOTHING;

DELETE FROM public.person_module_access
WHERE surface = 'web' AND module_key = 'sales' AND pages = ARRAY['sales-vendors']::text[];

UPDATE public.person_module_access
   SET pages = array_remove(pages, 'sales-vendors'), updated_at = now()
WHERE surface = 'web' AND module_key = 'sales' AND 'sales-vendors' = ANY (pages);

-- Designation pre-fills, same rule.
WITH inserted AS (
  INSERT INTO public.designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
  SELECT v.designation_code, v.surface, 'vendors_sales', v.capabilities, '{}'::text[]
  FROM public.designation_module_defaults v
  JOIN public.designation_module_defaults s
    ON s.designation_code = v.designation_code AND s.surface = v.surface AND s.module_key = 'sales'
   AND cardinality(s.capabilities) > 0
   AND (v.surface = 'mobile' OR cardinality(s.pages) = 0 OR 'sales-vendors' = ANY (s.pages))
  WHERE v.module_key = 'vendors' AND cardinality(v.capabilities) > 0
  ON CONFLICT DO NOTHING
  RETURNING designation_code, surface
)
INSERT INTO public.designation_module_defaults_vendors_sales_backfill (designation_code, surface)
SELECT designation_code, surface FROM inserted
ON CONFLICT DO NOTHING;

INSERT INTO public.designation_module_defaults_sales_vendors_page_removed (designation_code, surface, pages)
SELECT designation_code, surface, pages
FROM public.designation_module_defaults
WHERE module_key = 'sales' AND 'sales-vendors' = ANY (pages)
ON CONFLICT DO NOTHING;

UPDATE public.designation_module_defaults
   SET pages = array_remove(pages, 'sales-vendors')
WHERE module_key = 'sales' AND 'sales-vendors' = ANY (pages);

-- +goose Down
UPDATE public.designation_module_defaults d
   SET pages = r.pages
  FROM public.designation_module_defaults_sales_vendors_page_removed r
 WHERE d.designation_code = r.designation_code AND d.surface = r.surface AND d.module_key = 'sales';

DELETE FROM public.designation_module_defaults d
 USING public.designation_module_defaults_vendors_sales_backfill b
 WHERE d.designation_code = b.designation_code AND d.surface = b.surface AND d.module_key = 'vendors_sales';

INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, updated_by, pages)
SELECT tenant_id, workforce_member_id, 'web', 'sales', capabilities, now(), updated_by, pages
FROM public.person_module_access_sales_vendors_page_removed
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key)
DO UPDATE SET pages = EXCLUDED.pages, updated_at = now();

DELETE FROM public.person_module_access p
 USING public.person_module_access_vendors_sales_backfill b
 WHERE p.tenant_id = b.tenant_id AND p.workforce_member_id = b.workforce_member_id
   AND p.surface = b.surface AND p.module_key = 'vendors_sales';

DROP TABLE IF EXISTS public.designation_module_defaults_sales_vendors_page_removed;
DROP TABLE IF EXISTS public.designation_module_defaults_vendors_sales_backfill;
DROP TABLE IF EXISTS public.person_module_access_sales_vendors_page_removed;
DROP TABLE IF EXISTS public.person_module_access_vendors_sales_backfill;
