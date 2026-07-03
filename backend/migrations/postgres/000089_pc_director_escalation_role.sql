-- +goose Up
-- +goose StatementBegin
ALTER TABLE user_scope_grants
  DROP CONSTRAINT IF EXISTS user_scope_grants_role_check;
ALTER TABLE user_scope_grants
  ADD CONSTRAINT user_scope_grants_role_check
  CHECK (role IN ('admin', 'park_head', 'pc_director', 'operator', 'verifier', 'ceo_internal'));

ALTER TABLE auth_pending_email_grants
  DROP CONSTRAINT IF EXISTS auth_pending_email_grants_role_check;
ALTER TABLE auth_pending_email_grants
  ADD CONSTRAINT auth_pending_email_grants_role_check
  CHECK (role IN ('admin', 'park_head', 'pc_director', 'operator', 'verifier', 'ceo_internal'));

ALTER TABLE workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;
ALTER TABLE workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (primary_role_hint IN ('operator', 'park_head', 'pc_director', 'verifier', 'supervisor', 'admin', 'other'));

ALTER TABLE obligation_escalations
  DROP CONSTRAINT IF EXISTS obligation_escalations_role_check;
ALTER TABLE obligation_escalations
  ADD CONSTRAINT obligation_escalations_role_check
  CHECK (escalated_to_role IS NULL OR escalated_to_role IN ('admin', 'park_head', 'pc_director', 'operator', 'verifier', 'ceo_internal'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE user_scope_grants
SET role = 'admin'
WHERE role = 'pc_director';

UPDATE auth_pending_email_grants
SET role = 'admin',
    status = 'revoked'
WHERE role = 'pc_director';

UPDATE workforce_members
SET primary_role_hint = 'admin'
WHERE primary_role_hint = 'pc_director';

UPDATE obligation_escalations
SET escalated_to_role = 'admin'
WHERE escalated_to_role = 'pc_director';

ALTER TABLE user_scope_grants
  DROP CONSTRAINT IF EXISTS user_scope_grants_role_check;
ALTER TABLE user_scope_grants
  ADD CONSTRAINT user_scope_grants_role_check
  CHECK (role IN ('admin', 'park_head', 'operator', 'verifier', 'ceo_internal'));

ALTER TABLE auth_pending_email_grants
  DROP CONSTRAINT IF EXISTS auth_pending_email_grants_role_check;
ALTER TABLE auth_pending_email_grants
  ADD CONSTRAINT auth_pending_email_grants_role_check
  CHECK (role IN ('admin', 'park_head', 'operator', 'verifier', 'ceo_internal'));

ALTER TABLE workforce_members
  DROP CONSTRAINT IF EXISTS workforce_members_role_hint_check;
ALTER TABLE workforce_members
  ADD CONSTRAINT workforce_members_role_hint_check
  CHECK (primary_role_hint IN ('operator', 'park_head', 'verifier', 'supervisor', 'admin', 'other'));

ALTER TABLE obligation_escalations
  DROP CONSTRAINT IF EXISTS obligation_escalations_role_check;
ALTER TABLE obligation_escalations
  ADD CONSTRAINT obligation_escalations_role_check
  CHECK (escalated_to_role IS NULL OR escalated_to_role IN ('admin', 'park_head', 'operator', 'verifier', 'ceo_internal'));
-- +goose StatementEnd
