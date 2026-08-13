-- +goose Up
-- `counts_approver` is a PER-PERSON authority grant for the Counts approval queue
-- (birth / death / shifting), NOT a job role.
--
-- Maintainer decision 2026-08-05: the approval queue returns to the phone as its own
-- Approvals module (superseding the 2026-07-21 "approvals live on admin-web only"
-- decision), and the approvers are the CEO/CXO cohort plus two NAMED directors --
-- explicitly "rbac per person, not per group".
--
-- Permissions in this system resolve from the roles on a caller's active
-- user_scope_grants rows, so the only way to express "this person, not this job" is a
-- narrow role granted per person. Adding counts.approve_* to pc_director /
-- growth_director instead would have granted it to every FUTURE holder of those jobs
-- and reversed the one-module-one-director segregation lock for everyone.
--
-- Without this row every grant INSERT fails: user_scope_grants_role_fk and
-- auth_pending_email_grants_role_fk are both FKs to org_role_catalog(role_key), so the
-- role is declared in Go (permissions.RoleCountsApprover) but not grantable.
--
-- tier_code is NOT NULL and FKs to org_tiers, so this carries 'director' -- the tier of
-- the people who hold it. vertical_code stays NULL because there is no 'counts' vertical
-- in org_verticals (the 9 departments do not include one); is_legacy = true is what makes
-- a NULL vertical legal under org_role_catalog_vertical_or_legacy_check, and is the same
-- shape growth_director (000057) and feed_director/health_director (000068) use.
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES ('counts_approver', 'director', NULL, true, 'Counts Approver', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    label = EXCLUDED.label;

-- Deliberately NO workforce_members_role_hint_check change, unlike 000057 and 000068.
-- primary_role_hint is the person's JOB; counts_approver is never anyone's job, only an
-- extra authority held alongside one. A holder keeps their real hint (pc_director,
-- growth_director), which is exactly what makes revoking the grant leave the job intact.

-- +goose Down
-- Revoke any outstanding grants first: user_scope_grants_role_fk and
-- auth_pending_email_grants_role_fk both reference this row, so the DELETE below fails
-- while a grant survives. Rolling this migration back is meant to remove the authority,
-- so the grants go with it.
DELETE FROM public.user_scope_grants WHERE role = 'counts_approver';
DELETE FROM public.auth_pending_email_grants WHERE role = 'counts_approver';

DELETE FROM public.org_role_catalog WHERE role_key = 'counts_approver';
