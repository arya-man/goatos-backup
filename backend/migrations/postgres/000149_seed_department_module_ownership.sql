-- +goose Up
-- Seed the department-sourced module ownership from 000148 for tenant Mesha.
-- See docs/decisions/user-module-ownership-and-nav-chrome.md (ADR step 2).
--
-- Ownership chain seeded here:
--   departments (HR units)  ->  department_module_grants (owned modules)
--   workforce_members.department_id  ->  departments  (the person's HR unit)
--
-- Scope-lock (ADR:66-68, 000148:24-25): ONLY built modules are ever seeded/
-- surfaced. Today that is pc.vaccination + admin.* (config/sop/audit). Do NOT
-- seed procurement/counts here even though their static nav groups exist; they
-- are not in the built set.
--
-- nav chrome (compiled later from these rows): drawer/sidebar iff the actor's
-- department owns >=2 active visible modules, else bottom-bar/no-sidebar.
--   vaccination dept -> 1 module (pc.vaccination)      -> no sidebar
--   admin_data  dept -> 3 modules (admin.*)            -> sidebar
--   leadership  dept -> 4 modules (pc.vaccination+admin.*) -> sidebar (full command room)
--
-- Department UUIDs are gen_random_uuid() (non-deterministic per DB); every child
-- row resolves the department by (tenant_id, code) JOIN, never a hardcoded uuid,
-- so this seed is fresh-DB portable and idempotent.
--
-- MEMBER ATTACH SCOPE (important): leadership/admin actors do NOT get a
-- workforce_member on sign-in (the /app + /admin-web actor->member join is by
-- user_id, unknown at migration time), so a migration cannot attach a real
-- leadership member to the leadership department. This seed therefore attaches
-- ONLY the deterministic vaccination CLI fixtures (seed-vaccination-trigger) to
-- the vaccination department, no-op-safe when that CLI has not run. Provisioning
-- a leadership/admin workforce_member with department_id belongs to the sign-in/
-- claim path (permissions ensureUserGrant) or CreateOperator, tracked in the
-- bootstrap-wiring step (ADR steps 3/6), NOT this migration.

-- 1) Departments (HR vocabulary). Idempotent on (tenant_id, code).
INSERT INTO departments (tenant_id, code, label, status) VALUES
  ('00000000-0000-4000-8000-000000000001', 'vaccination', 'Vaccination',       'active'),
  ('00000000-0000-4000-8000-000000000001', 'admin_data',  'Admin / Data Ops',  'active'),
  ('00000000-0000-4000-8000-000000000001', 'leadership',  'Leadership',        'active')
ON CONFLICT (tenant_id, code) DO NOTHING;

-- 2) department -> owned module grants (built modules only). Idempotent on the
--    partial unique index (tenant_id, department_id, module) WHERE status='active'.
INSERT INTO department_module_grants (tenant_id, department_id, vertical, module, status)
SELECT d.tenant_id, d.department_id, g.vertical, g.module, 'active'
FROM departments d
JOIN (VALUES
  ('vaccination', 'preventive_care', 'pc.vaccination'),
  ('admin_data',  'admin_data',      'admin.config'),
  ('admin_data',  'admin_data',      'admin.sop'),
  ('admin_data',  'admin_data',      'admin.audit'),
  ('leadership',  'preventive_care', 'pc.vaccination'),
  ('leadership',  'admin_data',      'admin.config'),
  ('leadership',  'admin_data',      'admin.sop'),
  ('leadership',  'admin_data',      'admin.audit')
) AS g(dept_code, vertical, module) ON g.dept_code = d.code
WHERE d.tenant_id = '00000000-0000-4000-8000-000000000001'
ON CONFLICT (tenant_id, department_id, module) WHERE status = 'active' DO NOTHING;

-- 3) Attach the vaccination CLI fixtures to the vaccination department.
--    No-op-safe: touches 0 rows on a fresh DB where seed-vaccination-trigger has
--    not run. Only sets rows that are not already in the vaccination department
--    (so a re-run bumps nothing).
UPDATE workforce_members wm
SET department_id = d.department_id,
    updated_at = now(),
    row_version = wm.row_version + 1
FROM departments d
WHERE d.tenant_id = wm.tenant_id
  AND d.code = 'vaccination'
  AND wm.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND wm.display_code IN ('CBE-VACC-OP-01', 'CBE-PARK-HEAD-01', 'CBE-VACC-VERIFY-01')
  AND wm.department_id IS DISTINCT FROM d.department_id;

-- +goose Down
-- Detach members first (drops the FK ref), then grants (FK -> departments), then
-- the departments themselves.
UPDATE workforce_members wm
SET department_id = NULL,
    updated_at = now(),
    row_version = wm.row_version + 1
FROM departments d
WHERE d.tenant_id = wm.tenant_id
  AND d.code = 'vaccination'
  AND wm.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND wm.display_code IN ('CBE-VACC-OP-01', 'CBE-PARK-HEAD-01', 'CBE-VACC-VERIFY-01')
  AND wm.department_id = d.department_id;

DELETE FROM department_module_grants g
USING departments d
WHERE g.tenant_id = d.tenant_id
  AND g.department_id = d.department_id
  AND d.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND d.code IN ('vaccination', 'admin_data', 'leadership')
  AND g.module IN ('pc.vaccination', 'admin.config', 'admin.sop', 'admin.audit');

DELETE FROM departments
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND code IN ('vaccination', 'admin_data', 'leadership');
