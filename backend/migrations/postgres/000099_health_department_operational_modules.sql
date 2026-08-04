-- +goose Up
-- Maintainer decision 2026-07-30: Health-department operators perform Health,
-- Counts, Feed, and Vaccination work. Migrations 000061/000062 already provide
-- aas_health and counts is part of the baseline roster seed; this forward migration
-- adds the two missing module grants to every active Health department.
--
-- The source is bounded by the active department catalog x two literal modules. The
-- conflict update makes the confirmed policy effective even when a stale seed left a
-- matching grant inactive; already-active grants are untouched by the WHERE clause.
INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
SELECT d.tenant_id, d.department_id, modules.module_key, 'active'
FROM public.departments d
CROSS JOIN (VALUES ('feed_direction'), ('vaccination')) AS modules(module_key)
WHERE d.code = 'health'
  AND d.status = 'active'
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE
SET status = 'active', updated_at = now()
WHERE department_module_grants.status <> 'active';

-- +goose Down
-- Forward-only authority change. Removing either grant on rollback could overwrite a
-- later operational decision made after this migration was applied.
SELECT 1;
