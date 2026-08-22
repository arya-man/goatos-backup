-- +goose Up
-- seed-fixture-guard:ignore: additive nullable identity columns; existing seed rows stay valid without touching any seed command
--
-- WORKFORCE PEOPLE IDENTITY (People/HRMS rewrite, maintainer request 2026-08-22).
--
-- The People directory and the in-app "Add Person" onboarding flow need the
-- person's structured name and login email ON the workforce_members row itself.
-- Today the email exists only inside auth_pending_email_grants and (sometimes)
-- workforce_members.metadata->>'email', and there is no first/last name split at
-- all -- only display_name/display_code. These columns are:
--
--   * nullable: every existing roster/seed row stays valid; legacy rows keep
--     rendering from display_name alone.
--   * email is a UNIQUENESS KEY into the identity chain (Firebase user ->
--     StableSubjectID -> user_scope_grants), not an annotation, which is why it
--     is a real column and not metadata JSON.
--
-- The partial unique index tolerates historical duplicates only by excluding
-- NULLs; two ACTIVE-or-not rows may never share a login email inside a tenant,
-- because the create-person flow resolves the Firebase account by email.
ALTER TABLE public.workforce_members
  ADD COLUMN IF NOT EXISTS first_name text,
  ADD COLUMN IF NOT EXISTS last_name  text,
  ADD COLUMN IF NOT EXISTS email      text;

CREATE UNIQUE INDEX IF NOT EXISTS workforce_members_tenant_email_uq
  ON public.workforce_members (tenant_id, lower(email))
  WHERE email IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS workforce_members_tenant_email_uq;
ALTER TABLE public.workforce_members
  DROP COLUMN IF EXISTS first_name,
  DROP COLUMN IF EXISTS last_name,
  DROP COLUMN IF EXISTS email;
