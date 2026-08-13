-- +goose Up
-- Maintainer decision 2026-08-05: the preventive_care department no longer holds the "milk" or
-- "aas_health" modules. A PC seat's bottom bar becomes Vaccination + Counts + Feed.
--
-- How each grant got there, which is why one statement cannot cover both:
--   - aas_health: migration 000098 copied EVERY active vaccination grant into aas_health, and
--     preventive_care holds vaccination, so it was granted as a side effect rather than by a
--     decision naming it.
--   - milk: migration 000101 copied every active counts grant into milk so the 2026-07-31 module
--     split would not silently drop Milk Prep / Milk Feeding from a bar that already showed them.
--     That was migration-safety, not a standing rule that PC must run milk.
--
-- Losing those two pages from the PC bar is the POINT of this decision, not a regression. The
-- pages are untouched: /counts/milk-preparation and /counts/milk-feeding still exist, still route
-- under /counts/..., and still require CountsWrite. Only the preventive_care nav entry is gone.
--
-- DEACTIVATE, never DELETE. Every sibling grant change in this table (health -> counts, migration
-- 000100) flips status and keeps the row, so the grant's history stays readable and re-enabling is
-- one UPDATE rather than a re-INSERT that would lose created_at.
--
-- Scoped to preventive_care BY CODE. The health department holds both modules by its own
-- 2026-07-30 decision and must not be touched; an unscoped "revoke milk everywhere counts is
-- granted" would strip Health's daily pages as collateral.
--
-- projection-review:
--   producer: department_module_grants is UNIQUE on (tenant_id, department_id, module_key).
--   consumer: this UPDATE matches on (tenant_id, department_id) via the departments join plus a
--     2-value module_key IN list, so it addresses at most 2 rows per tenant.
--   multiplicity: departments is UNIQUE on (tenant_id, code), so the join is 1:1 and cannot
--     fan out. No aggregate, no ratio, no GROUP BY -- there is no key set to compare.
UPDATE public.department_module_grants g
   SET status = 'inactive', updated_at = now()
  FROM public.departments d
 WHERE d.tenant_id = g.tenant_id
   AND d.department_id = g.department_id
   AND d.code = 'preventive_care'
   AND g.module_key IN ('milk', 'aas_health')
   AND g.status <> 'inactive';

-- +goose Down
-- Forward-only authority change, matching migrations 000099/000100/000101. Re-granting on
-- rollback would overwrite whatever module administration decided after this migration ran.
SELECT 1;
