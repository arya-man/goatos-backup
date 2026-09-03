-- +goose Up
-- seed-fixture-guard:ignore: per-person access rows for one module surface; no vaccination/HRMS
-- seed contract change.
--
-- VENDORS ON THE PHONE FOR EVERYONE WHO ALREADY HAS IT ON THE WEB (maintainer decision 2026-09-03).
--
-- A person's phone modules are their TICKS (person_module_access, 000219), and a tick narrows an
-- offer -- it never widens one. The Vendors module is newly offered on the phone to whoever holds
-- vendor read, but every person already backfilled carries a WEB tick for `vendors` and no MOBILE
-- one, so the offer would be narrowed away on every existing phone until an admin re-ticked thirty
-- people. This copies each person's web `vendors` capabilities onto a mobile row, once, exactly as
-- the role backfill would have written had the module been on both surfaces then. Idempotent: a
-- mobile row that already exists (an admin ticked it by hand) is left alone.
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, updated_by, pages)
SELECT web.tenant_id, web.workforce_member_id, 'mobile', 'vendors', web.capabilities, now(), web.updated_by, '{}'::text[]
FROM public.person_module_access web
WHERE web.surface = 'web' AND web.module_key = 'vendors' AND cardinality(web.capabilities) > 0
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.person_module_access WHERE surface = 'mobile' AND module_key = 'vendors';
