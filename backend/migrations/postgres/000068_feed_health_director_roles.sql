-- +goose Up
-- `feed_director` and `health_director` are the live grant keys for Feed Director
-- and Health Director (maintainer decision 2026-08-01: Feed -> feed_director,
-- Counts -> health_director). The generic tier x vertical catalog rows
-- (`director_feed`, `director_health`) stay as dormant scaffolding; grants use
-- these name_director keys to match `pc_director` / `growth_director`.
--
-- Without these rows every grant INSERT fails: both user_scope_grants_role_fk and
-- auth_pending_email_grants_role_fk are FKs to org_role_catalog(role_key), so the
-- roles are declared in code but not grantable.
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES
  ('feed_director', 'director', 'feed', true, 'Feed Director', now()),
  ('health_director', 'director', 'health', true, 'Health Director', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    label = EXCLUDED.label;

-- validRoleHint (backend/internal/workforce/app/service.go) already accepts both
-- keys; without extending this CHECK the DB rejects the row at commit.
ALTER TABLE public.workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

ALTER TABLE public.workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (
    primary_role_hint = ANY (ARRAY[
      'operator'::text,
      'park_head'::text,
      'pc_director'::text,
      'growth_director'::text,
      'feed_director'::text,
      'health_director'::text,
      'verifier'::text,
      'supervisor'::text,
      'cxo'::text,
      'other'::text
    ])
  );

-- +goose Down
ALTER TABLE public.workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

ALTER TABLE public.workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (
    primary_role_hint = ANY (ARRAY[
      'operator'::text,
      'park_head'::text,
      'pc_director'::text,
      'growth_director'::text,
      'verifier'::text,
      'supervisor'::text,
      'cxo'::text,
      'other'::text
    ])
  );

DELETE FROM public.org_role_catalog
WHERE role_key IN ('feed_director', 'health_director');
