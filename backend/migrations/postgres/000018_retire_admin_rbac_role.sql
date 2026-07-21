-- +goose Up
-- Retire the legacy RBAC role_key 'admin'.
--
-- Product/API route names such as /admin/* and the admin-web application name
-- remain unchanged. This migration only removes the grantable RBAC role. CEO/CXO
-- top access is represented exclusively by role='ceo_internal'.

-- user_scope_grants: collapse admin into ceo_internal, avoiding unique/FK drift
-- when a user already has the equivalent ceo_internal grant for the same scope.
UPDATE user_scope_grants admin_grant
SET status = 'revoked',
    valid_to = coalesce(admin_grant.valid_to, now())
WHERE admin_grant.role = 'admin'
  AND EXISTS (
    SELECT 1
    FROM user_scope_grants ceo_grant
    WHERE ceo_grant.tenant_id = admin_grant.tenant_id
      AND ceo_grant.user_id = admin_grant.user_id
      AND ceo_grant.role = 'ceo_internal'
      AND ceo_grant.scope_type = admin_grant.scope_type
      AND ceo_grant.scope_id = admin_grant.scope_id
      AND ceo_grant.status = admin_grant.status
      AND ceo_grant.valid_from = admin_grant.valid_from
      AND ceo_grant.valid_to IS NOT DISTINCT FROM admin_grant.valid_to
  );

UPDATE user_scope_grants
SET role = 'ceo_internal'
WHERE role = 'admin';

-- Pending email grants: same collapse, but include the email identity in the
-- duplicate test because active uniqueness is per email + role + scope.
UPDATE auth_pending_email_grants admin_grant
SET status = 'revoked',
    valid_to = coalesce(admin_grant.valid_to, now()),
    updated_at = now(),
    metadata = coalesce(admin_grant.metadata, '{}'::jsonb)
      || jsonb_build_object('retired_role', 'admin', 'replacement_role', 'ceo_internal')
WHERE admin_grant.role = 'admin'
  AND admin_grant.status = 'active'
  AND admin_grant.valid_to IS NULL
  AND EXISTS (
    SELECT 1
    FROM auth_pending_email_grants ceo_grant
    WHERE ceo_grant.tenant_id = admin_grant.tenant_id
      AND ceo_grant.normalized_email = admin_grant.normalized_email
      AND ceo_grant.role = 'ceo_internal'
      AND ceo_grant.scope_type = admin_grant.scope_type
      AND ceo_grant.scope_id = admin_grant.scope_id
      AND ceo_grant.status = 'active'
      AND ceo_grant.valid_to IS NULL
  );

UPDATE auth_pending_email_grants
SET role = 'ceo_internal',
    updated_at = now(),
    metadata = coalesce(metadata, '{}'::jsonb)
      || jsonb_build_object('retired_role', 'admin', 'replacement_role', 'ceo_internal')
WHERE role = 'admin';

-- Historical escalations that targeted the old role now target CEO/CXO.
UPDATE obligation_escalations
SET escalated_to_role = 'ceo_internal'
WHERE escalated_to_role = 'admin';

ALTER TABLE obligation_escalations
  DROP CONSTRAINT IF EXISTS obligation_escalations_role_check;

ALTER TABLE obligation_escalations
  ADD CONSTRAINT obligation_escalations_role_check
  CHECK (
    escalated_to_role IS NULL OR
    escalated_to_role = ANY (ARRAY[
      'park_head'::text,
      'pc_director'::text,
      'operator'::text,
      'verifier'::text,
      'ceo_internal'::text
    ])
  );

DELETE FROM org_role_catalog
WHERE role_key = 'admin';

-- +goose Down
INSERT INTO org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label)
VALUES ('admin', 'ceo_cxo', NULL, true, 'Admin (retired alias of CEO/CXO)')
ON CONFLICT (role_key) DO NOTHING;

ALTER TABLE obligation_escalations
  DROP CONSTRAINT IF EXISTS obligation_escalations_role_check;

ALTER TABLE obligation_escalations
  ADD CONSTRAINT obligation_escalations_role_check
  CHECK (
    escalated_to_role IS NULL OR
    escalated_to_role = ANY (ARRAY[
      'admin'::text,
      'park_head'::text,
      'pc_director'::text,
      'operator'::text,
      'verifier'::text,
      'ceo_internal'::text
    ])
  );
