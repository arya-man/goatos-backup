-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one module surface; no vaccination/HRMS
-- seed contract change.
--
-- SALES ON THE PHONE FOR EVERYONE WHO ALREADY HAS IT ON THE WEB (maintainer decision 2026-09-05).
--
-- Exactly the shape of 000251, for the same reason and against the same trap. Sales became its own
-- phone module -- the sales ledger moved out of Procurement and took the selling half of the vendor
-- register with it -- so `sales` gained SurfaceMobile in the capability catalog. But a person's
-- phone modules are their TICKS (person_module_access, 000219), and a tick NARROWS an offer, never
-- widens one: every backfilled person carries a WEB tick for `sales` and no MOBILE one, so the new
-- module would be narrowed away on every existing phone until an admin re-ticked thirty people.
--
-- This copies each person's web `sales` capabilities onto a mobile row, once, exactly as the role
-- backfill would have written had the module been on both surfaces then. Idempotent: a mobile row
-- that already exists is left alone.
--
-- IT COPIES THE `sales` TICK AND NOT THE `vendors` ONE, even though the module's second tab shows
-- vendor rows. The tick answers "may this person reach the Sales module"; the Vendors TAB inside it
-- is separately gated on VendorRead at the nav contribution, so a person with a sales tick and no
-- vendor read gets the module with one tab rather than a module they cannot open. Copying the
-- vendors tick instead would hand the Sales module to whoever runs the buying desk.
--
-- The rows THIS migration writes are remembered in a small ledger, so the Down path removes exactly
-- those and nothing else: a mobile tick that existed before, or that an admin adds by hand
-- afterwards, is real per-person access and must survive a rollback or a rehearsal.
CREATE TABLE IF NOT EXISTS public.person_module_access_sales_mobile_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, updated_by, pages)
  SELECT web.tenant_id, web.workforce_member_id, 'mobile', 'sales', web.capabilities, now(), web.updated_by, '{}'::text[]
  FROM public.person_module_access web
  WHERE web.surface = 'web' AND web.module_key = 'sales' AND cardinality(web.capabilities) > 0
  ON CONFLICT DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_sales_mobile_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
-- Only the rows the Up path inserted (the ledger); pre-existing or hand-added mobile ticks stay.
DELETE FROM public.person_module_access p
USING public.person_module_access_sales_mobile_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'mobile'
  AND p.module_key = 'sales';
DROP TABLE IF EXISTS public.person_module_access_sales_mobile_backfill;
