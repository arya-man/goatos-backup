-- +goose Up
-- `procurement_director` is the live grant key for the Procurement Director business role
-- (maintainer decision 2026-08-21: a director-tier procurement seat whose admin-web workspace is
-- the Procurement and Feed modules only; permission set in
-- backend/internal/permissions/permissions.go, workspace lens in
-- backend/internal/adminui/app/procurement_director_lens.go). The generic tier x vertical catalog
-- row (`director_procurement`) stays as dormant scaffolding; grants use this name_director key to
-- match `pc_director` / `growth_director` / `feed_director` / `health_director` (000068 pattern).
--
-- Without this row every grant INSERT fails: both user_scope_grants_role_fk and
-- auth_pending_email_grants_role_fk are FKs to org_role_catalog(role_key), so the role is
-- declared in code but not grantable.
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES
  ('procurement_director', 'director', 'procurement', true, 'Procurement Director', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    tier_code = EXCLUDED.tier_code,
    vertical_code = EXCLUDED.vertical_code,
    label = EXCLUDED.label;

-- +goose Down
DELETE FROM public.user_scope_grants WHERE role = 'procurement_director';
DELETE FROM public.auth_pending_email_grants WHERE role = 'procurement_director';
DELETE FROM public.org_role_catalog WHERE role_key = 'procurement_director';
