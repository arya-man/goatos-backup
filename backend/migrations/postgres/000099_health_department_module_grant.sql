-- +goose Up
-- Health department operators own the Health Adult/Kids worklists. Migration 000061
-- granted aas_health only to departments that already had Vaccination, which omitted the
-- canonical health department used by the Android field identity.
INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
SELECT d.tenant_id, d.department_id, 'aas_health', 'active'
FROM public.departments d
WHERE d.code = 'health'
  AND d.status = 'active'
ON CONFLICT (tenant_id, department_id, module_key) DO NOTHING;

-- +goose Down
-- Intentionally no-op: a module grant may have been independently governed after this
-- migration. Removing or disabling it during rollback would overwrite operational authority.
