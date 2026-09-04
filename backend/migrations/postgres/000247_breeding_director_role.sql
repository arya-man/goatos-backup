-- +goose Up
-- `breeding_director` is the live grant key for the Breeding Director business role
-- (maintainer decision 2026-09-04: the desk that PLANS hoof trimming and hair trimming --
-- the two PC Care categories that belong to breeding husbandry -- while deworming and
-- ticks removal stay CEO-planned). Permission set in
-- backend/internal/permissions/permissions.go (RoleBreedingDirector: pc_care.plan_trimming
-- + pc_care.monitor, deliberately NO pc_care.plan and NO pc_care.execute); per-person
-- access row `pc_trimming` in backend/internal/permissions/capability.go. The generic
-- tier x vertical catalog row (`director_breeding`) stays as dormant scaffolding; grants
-- use this name_director key to match `pc_director` / `growth_director` / `feed_director`
-- / `health_director` / `procurement_director` (000068 / 000180 pattern).
--
-- Without this row every grant INSERT fails: both user_scope_grants_role_fk and
-- auth_pending_email_grants_role_fk are FKs to org_role_catalog(role_key), so the role is
-- declared in code but not grantable.
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES
  ('breeding_director', 'director', 'breeding', true, 'Breeding Director', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    tier_code = EXCLUDED.tier_code,
    vertical_code = EXCLUDED.vertical_code,
    label = EXCLUDED.label;

-- A JOB gets a designation row (000219 pattern: procurement_director has one, the
-- per-person counts_approver deliberately does not), so picking "Breeding Director" when
-- adding a person pre-fills the desk. The per-designation module defaults are written by
-- backend/cmd/backfill-person-access from permissions.AssignmentsForRole('breeding_director'),
-- the same Go mapping every other designation resolves through -- never duplicated as SQL.
INSERT INTO public.designation_catalog (designation_code, label, grade, sort_order)
VALUES ('breeding_director', 'Breeding Director', 'director', 55)
ON CONFLICT (designation_code) DO NOTHING;

-- +goose Down
DELETE FROM public.user_scope_grants WHERE role = 'breeding_director';
DELETE FROM public.auth_pending_email_grants WHERE role = 'breeding_director';
DELETE FROM public.designation_module_defaults WHERE designation_code = 'breeding_director';
DELETE FROM public.designation_catalog WHERE designation_code = 'breeding_director';
DELETE FROM public.org_role_catalog WHERE role_key = 'breeding_director';
