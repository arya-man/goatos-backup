-- +goose Up
-- Retire the old workforce display hint. CEO/CXO staff use primary_role_hint='cxo'.
-- This is display/bootstrap metadata only; RBAC grants remain role='ceo_internal'.

ALTER TABLE workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

UPDATE workforce_members
SET primary_role_hint = 'cxo',
    updated_at = now(),
    row_version = row_version + 1
WHERE primary_role_hint = 'admin';

ALTER TABLE workforce_members
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

-- +goose Down
ALTER TABLE workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;

ALTER TABLE workforce_members
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
