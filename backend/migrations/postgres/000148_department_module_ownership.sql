-- +goose Up
-- Department-sourced module/vertical ownership. Drives the visible-module set +
-- nav chrome (sidebar only when a principal owns >=2 modules) on BOTH
-- /admin-web/bootstrap and /app/bootstrap.
-- See docs/decisions/user-module-ownership-and-nav-chrome.md (Manju/CEO 2026-07-09:
-- ownership follows DEPARTMENT, modeled in Goat OS HR — "department to HR DB, all
-- staff including myself"; sidebar only when >=2 features/modules).
--
-- Ownership chain:  workforce_members.department_id -> departments
--                   departments -> department_module_grants (which modules owned)
--                   actor's owned modules = active grants for their department
-- nav chrome = drawer/sidebar iff count(active owned visible modules) >= 2.
--
-- Distinct from existing models, do NOT conflate:
--   user_scope_grants      = WHERE (org scope: park/shed) + coarse role
--   workforce capabilities = skills (can scan / can capture video)
--   department (this)      = HR unit the person belongs to
--   department_module_grants = WHICH product modules a department owns
-- RBAC stays server-authoritative on every command: ownership only hides/shows
-- nav + sets chrome; it never widens access.
--
-- Seed-first, no UI: departments + grants + members.department_id are seeded by
-- migration (next migration) for testing; an admin/HR UI to edit comes later.
-- Only BUILT modules are ever seeded/surfaced (scope-lock): today pc.vaccination
-- + admin.* (config/sop/audit).
--
-- Tenant isolation: because these tables drive visible nav, department references
-- use COMPOSITE (tenant_id, department_id) FKs so a grant/member can never point
-- at another tenant's department (repo pattern, cf. 000002 locations FKs).
-- code/vertical/module become bootstrap keys, so they carry normalized-format
-- checks (cf. workforce_capabilities_code_check) rather than a blank-only guard.

-- HR department vocabulary (Goat OS is its own HR DB).
CREATE TABLE departments (
  department_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  code text NOT NULL,                    -- e.g. 'vaccination', 'feed', 'admin_data', 'leadership'
  label text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT departments_code_check CHECK (code ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT departments_label_check CHECK (btrim(label) <> ''),
  CONSTRAINT departments_status_check CHECK (status IN ('active', 'inactive')),
  -- composite-FK target so children stay tenant-isolated
  CONSTRAINT departments_tenant_department_key UNIQUE (tenant_id, department_id)
);
CREATE UNIQUE INDEX departments_tenant_code_unique ON departments (tenant_id, code);

-- Which product modules/verticals a department owns (auditable, time-bounded;
-- shape mirrors user_scope_grants).
CREATE TABLE department_module_grants (
  grant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  department_id uuid NOT NULL,
  vertical text NOT NULL,                -- e.g. 'preventive_care', 'procurement', 'counts', 'admin_data'
  module text NOT NULL,                  -- e.g. 'pc.vaccination', 'procurement.source_entry', 'admin.config'
  status text NOT NULL,
  valid_from timestamptz NOT NULL DEFAULT now(),
  valid_to timestamptz NULL,
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT department_module_grants_vertical_check CHECK (vertical ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT department_module_grants_module_check CHECK (module ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
  CONSTRAINT department_module_grants_status_check CHECK (status IN ('active', 'inactive', 'revoked')),
  CONSTRAINT department_module_grants_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from),
  CONSTRAINT department_module_grants_department_tenant_fk
    FOREIGN KEY (tenant_id, department_id) REFERENCES departments (tenant_id, department_id)
);
-- One active ownership row per (tenant, department, module); history rows coexist.
CREATE UNIQUE INDEX department_module_grants_active_unique
  ON department_module_grants (tenant_id, department_id, module)
  WHERE status = 'active';
-- Bootstrap compile reads a department's active modules (tenant scoped).
CREATE INDEX department_module_grants_dept_active_idx
  ON department_module_grants (tenant_id, department_id, status);

-- Attach the person to their HR department (nullable; composite-FK tenant-isolated).
ALTER TABLE workforce_members
  ADD COLUMN department_id uuid NULL;
ALTER TABLE workforce_members
  ADD CONSTRAINT workforce_members_department_tenant_fk
  FOREIGN KEY (tenant_id, department_id) REFERENCES departments (tenant_id, department_id);
CREATE INDEX workforce_members_department_idx
  ON workforce_members (tenant_id, department_id);

-- +goose Down
ALTER TABLE workforce_members DROP CONSTRAINT IF EXISTS workforce_members_department_tenant_fk;
ALTER TABLE workforce_members DROP COLUMN IF EXISTS department_id;
DROP TABLE IF EXISTS department_module_grants;
DROP TABLE IF EXISTS departments;
