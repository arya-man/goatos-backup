-- +goose Up
-- Maintainer decision 2026-07-31: Milk Preparation and Milk Feeding move out of the
-- Counts module into their own "milk" module in the drawer. The two pages keep their
-- existing /counts/... routes and their CountsWrite authority; only the drawer grouping
-- changes.
--
-- Because the bottom bar is composed from department_module_grants, that regrouping
-- would REMOVE both pages from every operator who reaches them through the counts
-- grant. This forward migration grants "milk" to exactly those departments, so the move
-- is nav-only and no operator loses a page they can already open.
--
-- Membership source is department_module_grants itself, not a literal department list:
-- the departments that must keep milk are precisely the ones holding an active counts
-- grant today (preventive_care and health at the time of writing, plus anything a later
-- seed added). Bounded by the active grant catalog x one literal module.
--
-- projection-review: producer (department_module_grants) is unique on
-- (tenant_id, department_id, module_key); this statement selects one row per matching
-- (tenant_id, department_id) where module_key = 'counts' and inserts exactly one 'milk'
-- row per such pair. The source is already 1:1 on the insert's conflict key, so no
-- fan-out and no dedupe is required.
INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
SELECT g.tenant_id, g.department_id, 'milk', 'active'
FROM public.department_module_grants g
WHERE g.module_key = 'counts'
  AND g.status = 'active'
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE
SET status = 'active', updated_at = now()
WHERE department_module_grants.status <> 'active';

-- +goose Down
-- Forward-only authority change. Revoking the grant on rollback would hide Milk
-- Preparation and Milk Feeding from operators who depend on them daily, and could
-- overwrite a later operational decision made after this migration was applied.
SELECT 1;
