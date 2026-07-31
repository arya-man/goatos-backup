-- +goose Up
-- `growth_director` is the live mobile/STG role key for Growth Director.
-- Keep the generic tier x vertical catalog row (`director_growth`) as dormant
-- scaffolding; grants use this name_director key to match `pc_director`.
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES ('growth_director', 'director', 'growth', true, 'Growth Director', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    label = EXCLUDED.label;

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
      'verifier'::text,
      'supervisor'::text,
      'cxo'::text,
      'other'::text
    ])
  );

DELETE FROM public.org_role_catalog
WHERE role_key = 'growth_director';
