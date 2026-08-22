-- +goose Up
-- seed-fixture-guard:ignore: new auth infrastructure table; no seed contract writes it (env allowlist remains the bootstrap source)
--
-- DB-BACKED AUTH EMAIL ALLOWLIST (People/HRMS rewrite, maintainer request 2026-08-22).
--
-- Today the login email allowlist is ONLY the GOATOS_AUTH_ALLOWED_EMAILS env
-- var, sourced from a Secret Manager secret, so every single hire needs a new
-- secret version PLUS a Cloud Run revision bump before they can sign in. That
-- is the step the in-app "Add Person" flow removes: creating a person inserts
-- an active row here in the SAME transaction as the workforce_members and
-- user_scope_grants rows, and the auth middleware admits the email without any
-- deploy-side change.
--
-- Read/write ownership (documented cross-module exception, same precedent as
-- the workforce module writing user_scope_grants in its create-person tx):
--   * WRITTEN by the workforce CreatePerson transaction
--     (backend/internal/workforce/adapters/postgres/people_repository.go).
--   * READ by the auth middleware through
--     backend/internal/permissions/adapters/postgres/email_allowlist.go with a
--     short in-process TTL cache. Semantics are a UNION with the env list, and
--     enforcement stays keyed on the env list being non-empty: an empty env
--     allowlist means "allowlist disabled" (local dev) exactly as before.
--
-- normalized_email mirrors authallow.NormalizeEmail (lower/trim); the CHECK
-- mirrors auth_pending_email_grants' email shape check so an unparseable email
-- can never be admitted.
CREATE TABLE IF NOT EXISTS public.auth_allowed_emails (
  allowed_email_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id        uuid NOT NULL REFERENCES tenants (tenant_id),
  email            text NOT NULL,
  normalized_email text NOT NULL,
  status           text DEFAULT 'active' NOT NULL,
  -- Provenance: what put the row here ('workforce_create_person', a future
  -- import, or a hand-run repair). Diagnostic only, never a behavior switch.
  source           text DEFAULT '' NOT NULL,
  created_by       uuid,
  created_at       timestamptz DEFAULT now() NOT NULL,
  updated_at       timestamptz DEFAULT now() NOT NULL,
  CONSTRAINT auth_allowed_emails_pkey PRIMARY KEY (allowed_email_id),
  CONSTRAINT auth_allowed_emails_status_check
    CHECK (status IN ('active', 'revoked')),
  CONSTRAINT auth_allowed_emails_email_check
    CHECK (normalized_email = lower(btrim(normalized_email))
           AND normalized_email LIKE '%_@_%'
           AND normalized_email NOT LIKE '% %')
);

-- One active row per email per tenant; a revoked row may coexist as history.
CREATE UNIQUE INDEX IF NOT EXISTS auth_allowed_emails_active_uq
  ON public.auth_allowed_emails (tenant_id, normalized_email)
  WHERE status = 'active';

-- The middleware's read is "load every active email"; the table is tiny (one
-- row per staff login), so this index serves both the load and the uniqueness
-- probe on insert.
CREATE INDEX IF NOT EXISTS auth_allowed_emails_status_idx
  ON public.auth_allowed_emails (tenant_id, status);

-- +goose Down
DROP TABLE IF EXISTS public.auth_allowed_emails;
