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
-- seed-migration-guard:ignore owner=maintainer issue=people-hrms-rewrite reason=purely-additive-nullable-columns;-existing-seed-commands-insert-without-them-and-stay-valid expiry=2026-11-30
ALTER TABLE public.workforce_members
  ADD COLUMN IF NOT EXISTS first_name text,
  ADD COLUMN IF NOT EXISTS last_name  text,
  ADD COLUMN IF NOT EXISTS email      text;

-- Backfill: pre-existing rows carry their login email only inside metadata —
-- 'email' (STG seeds + manual onboarding rows) or 'normalized_email' (the
-- runtime pending-grant claim path). Copy it into the new column ONCE, exactly
-- one row per (tenant, email) — active first, then oldest — so the unique
-- index below cannot collide when two rows (e.g. an orphan auth:<uid>
-- placeholder plus the real roster row) carry the same metadata email; the
-- losing row keeps its metadata-only email and renders without one.
-- seed-migration-guard:ignore owner=maintainer issue=people-hrms-rewrite reason=one-time-metadata-email-backfill;-no-seed-command-contract-change expiry=2026-11-30
WITH candidates AS (
  SELECT workforce_member_id,
         lower(btrim(COALESCE(nullif(metadata->>'email', ''), metadata->>'normalized_email'))) AS meta_email,
         ROW_NUMBER() OVER (
           PARTITION BY tenant_id, lower(btrim(COALESCE(nullif(metadata->>'email', ''), metadata->>'normalized_email')))
           ORDER BY (status = 'active') DESC, created_at ASC, workforce_member_id ASC
         ) AS rn
  FROM public.workforce_members
  WHERE email IS NULL
    AND COALESCE(btrim(COALESCE(nullif(metadata->>'email', ''), metadata->>'normalized_email')), '') <> ''
    AND btrim(COALESCE(nullif(metadata->>'email', ''), metadata->>'normalized_email')) LIKE '%_@_%'
    AND btrim(COALESCE(nullif(metadata->>'email', ''), metadata->>'normalized_email')) NOT LIKE '% %'
)
UPDATE public.workforce_members wm
SET email = c.meta_email
FROM candidates c
WHERE c.workforce_member_id = wm.workforce_member_id
  AND c.rn = 1
  AND NOT EXISTS (
    SELECT 1 FROM public.workforce_members other
    WHERE other.tenant_id = wm.tenant_id AND lower(other.email) = c.meta_email
  );

-- The leadership auth-profile rows were seeded with the literal placeholder
-- display_name 'CEO/CXO', which renders as N identical unidentifiable rows in
-- the People directory. Give each its person name derived from the email
-- local-part (ravi@… -> 'Ravi'); the ROLE stays visible through
-- hr_designation_grade = 'cxo'. The seeders that wrote the placeholder
-- (seed-stg-login-grants ensureAuthProfileMember, the pending-grant claim
-- path) are fixed in the same change to write the person's name directly.
-- seed-migration-guard:ignore owner=maintainer issue=people-hrms-rewrite reason=one-time-placeholder-display-name-repair;-seeders-fixed-in-same-change expiry=2026-11-30
UPDATE public.workforce_members
SET display_name = initcap(replace(replace(split_part(email, '@', 1), '.', ' '), '_', ' ')),
    first_name   = COALESCE(first_name, initcap(replace(replace(split_part(email, '@', 1), '.', ' '), '_', ' ')))
WHERE display_name = 'CEO/CXO'
  AND email IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS workforce_members_tenant_email_uq
  ON public.workforce_members (tenant_id, lower(email))
  WHERE email IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS workforce_members_tenant_email_uq;
-- seed-migration-guard:ignore owner=maintainer issue=people-hrms-rewrite reason=down-drops-the-same-additive-columns expiry=2026-11-30
ALTER TABLE public.workforce_members
  DROP COLUMN IF EXISTS first_name,
  DROP COLUMN IF EXISTS last_name,
  DROP COLUMN IF EXISTS email;
