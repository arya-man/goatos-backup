-- +goose Up
-- HR department membership. Departments are an HR grouping on a workforce
-- member; they do not own product modules and do not drive navigation or access.
CREATE TABLE departments (
  department_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  code text NOT NULL,
  label text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT departments_code_check CHECK (code ~ '^[a-z][a-z0-9_]*$'),
  CONSTRAINT departments_label_check CHECK (btrim(label) <> ''),
  CONSTRAINT departments_status_check CHECK (status IN ('active', 'inactive')),
  CONSTRAINT departments_tenant_department_key UNIQUE (tenant_id, department_id)
);
CREATE UNIQUE INDEX departments_tenant_code_unique
  ON departments (tenant_id, code);

ALTER TABLE workforce_members
  ADD COLUMN department_id uuid NULL;
ALTER TABLE workforce_members
  ADD CONSTRAINT workforce_members_department_tenant_fk
  FOREIGN KEY (tenant_id, department_id) REFERENCES departments (tenant_id, department_id);
CREATE INDEX workforce_members_department_idx
  ON workforce_members (tenant_id, department_id);

-- +goose Down
DROP INDEX IF EXISTS workforce_members_department_idx;
ALTER TABLE workforce_members DROP CONSTRAINT IF EXISTS workforce_members_department_tenant_fk;
ALTER TABLE workforce_members DROP COLUMN IF EXISTS department_id;
DROP INDEX IF EXISTS departments_tenant_code_unique;
DROP TABLE IF EXISTS departments;
