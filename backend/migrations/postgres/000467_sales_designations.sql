-- +goose Up
-- Sales Director and Sales Manager as DESIGNATIONS (People / HRMS fixes, 2026-10-02).
--
-- The Add Person form's role list is now the designation catalog itself (adminui people_roles,
-- compiled from active designation_catalog rows that are known RBAC roles; the write path checks
-- the same catalog). Both rows were added on STG by hand through the Config Roles register so a
-- sales person could be hired; a fresh database has neither, so the form would not offer them.
--
-- The grant keys are the composite org-role keys already in org_role_catalog since the
-- clean-slate baseline (000001: director_sales, manager_sales), so no role row is needed here.
-- ON CONFLICT DO NOTHING keeps STG's existing rows (and their labels/order) untouched.
INSERT INTO public.designation_catalog (designation_code, label, grade, sort_order)
VALUES
  ('director_sales', 'Sales Director', 'director', 57),
  ('manager_sales',  'Sales Manager',  'manager',  114)
ON CONFLICT (designation_code) DO NOTHING;

-- +goose Down
-- Deliberately a no-op: STG already carried both rows before this migration, and deleting a
-- designation a person's access header points at would fail the person_access FK.
SELECT 1;
