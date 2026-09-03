-- +goose Up
-- seed-fixture-guard:ignore: repairs per-person access rows for an existing module; no
-- source fixture schema, HRMS seed contract, or read-model change.
-- Give the people who already hold Sales the sale_allocation tick they were never written.
--
-- WHY THIS EXISTS
--
-- `sale_allocation` is its own capability module (permissions.ModuleCapabilities), held
-- apart from `sales` on purpose: recording a ledger row must not imply exiting live
-- animals from the herd. 92f11850e added it to the ROLE map in capability_backfill.go
-- alongside the two roles that get it, ceo_internal and procurement_director.
--
-- That map is DEAD DATA after the 2026-08-24 cutover. It is read by the one-time
-- backfill and by the parity test, never at request time -- so adding a module to it
-- changes NOTHING for anyone already migrated, and every migrated person kept a stored
-- row set with no `sale_allocation` in it. Since person rows DECIDE (auth logs the denial
-- as decided_by=person), `sales.allocate_animals` was held by nobody, and all five
-- sale-allocation routes 403'd for the CEO herself.
--
-- The visible symptom was an EMPTY PARK LIST in the "Tag animals to sale" drawer:
-- GET /admin/goats/sale-locations returned 403, the page read it as an empty catalog,
-- and the picker offered "Choose a park" and nothing to choose.
--
-- WHO GETS IT, and why the key is the role grant rather than the stored `sales` row.
-- The two flat roles above are the only ones the mapping gives it to. A senior person in
-- the Sales VERTICAL (verticalModule) also carries `sales` web {view,do} and deliberately
-- does NOT get sale_allocation, so keying on the stored sales row would widen the grant
-- past what the mapping says. user_scope_grants is still the record of who held which
-- role, so it is the exact same population the backfill would have written.
--
-- ADDITIVE ONLY. A person who already has a `sale_allocation` row -- because they were
-- created after 92f11850e, or because an admin ticked it on /people -- is left exactly as
-- they are. This repairs an absent row; it never rewrites a decision someone made on the
-- access screen.
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'web', 'sale_allocation', ARRAY['do']::text[]
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role IN ('ceo_internal', 'procurement_director')
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  -- Only people the cutover already migrated. Someone with no rows at all is still on the
  -- role fallback path (decideAuthorization logs it), and writing a single row here would
  -- take them OFF that path and leave them holding sale_allocation and nothing else.
  AND EXISTS (
    SELECT 1 FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- +goose Down
-- Removes only the rows this migration could have written. A row an admin ticked on
-- /people is indistinguishable from one written here, which is the honest cost of an
-- additive repair; Down is the rollback of the release, not an access edit.
DELETE FROM public.person_module_access
WHERE surface = 'web' AND module_key = 'sale_allocation';
