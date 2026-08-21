-- +goose Up
-- seed-fixture-guard:ignore: module-grant catalog row for the new PC Care module; the
-- Vaccination HRMS seed contract is unchanged
--
-- PC Care (module_key pc_care) belongs to the Preventive Care department, beside
-- Vaccination (maintainer decision 2026-08-21). Grant it to every active
-- preventive-care department so operators in that department are OFFERED the module;
-- individual access still composes from pc_care.* permissions and per-task assignees.
INSERT INTO public.department_module_grants (tenant_id, department_id, module_key, status)
SELECT d.tenant_id, d.department_id, 'pc_care', 'active'
FROM public.departments d
WHERE d.code = 'preventive_care'
  AND d.status = 'active'
ON CONFLICT (tenant_id, department_id, module_key) DO NOTHING;

-- +goose Down
-- Intentionally no-op: a module grant may have been independently governed after this
-- migration. Removing or disabling it during rollback would overwrite operational authority.
