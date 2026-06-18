-- +goose Up
-- Email-based bootstrap policy for verified Firebase/JWKS sign-ins.
-- This table stores approved admin emails, not raw Google/Firebase tokens.
CREATE TABLE auth_pending_email_grants (
  pending_grant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  email text NOT NULL,
  normalized_email text NOT NULL,
  role text NOT NULL,
  scope_type text NOT NULL DEFAULT 'tenant',
  scope_id uuid NOT NULL,
  status text NOT NULL DEFAULT 'active',
  valid_from timestamptz NOT NULL DEFAULT now(),
  valid_to timestamptz NULL,
  source text NOT NULL DEFAULT 'manual',
  created_by uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_claimed_user_id uuid NULL,
  last_claimed_external_subject text NULL,
  last_claimed_at timestamptz NULL,
  claim_count bigint NOT NULL DEFAULT 0,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT auth_pending_email_grants_email_check CHECK (
    normalized_email = lower(btrim(email))
    AND normalized_email <> ''
    AND normalized_email NOT LIKE '%,%'
    AND normalized_email NOT LIKE '% %'
    AND position('@' IN normalized_email) > 1
  ),
  CONSTRAINT auth_pending_email_grants_role_check CHECK (role IN ('admin', 'park_head', 'operator', 'verifier', 'ceo_internal')),
  CONSTRAINT auth_pending_email_grants_scope_check CHECK (scope_type = 'tenant' AND scope_id = tenant_id),
  CONSTRAINT auth_pending_email_grants_status_check CHECK (status IN ('active', 'revoked')),
  CONSTRAINT auth_pending_email_grants_valid_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE UNIQUE INDEX auth_pending_email_grants_active_unique_idx
  ON auth_pending_email_grants (tenant_id, normalized_email, role, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL;

CREATE INDEX auth_pending_email_grants_lookup_idx
  ON auth_pending_email_grants (normalized_email, tenant_id, status, valid_from, valid_to);

COMMENT ON TABLE auth_pending_email_grants IS
  'Approved verified-email bootstrap grants. On auth.sign_in, a matching verified email is converted idempotently into user_scope_grants for the Firebase/JWKS subject.';
COMMENT ON COLUMN auth_pending_email_grants.normalized_email IS
  'Lowercase trimmed email used for exact verified-email matching. Never trust browser-supplied email; the backend uses verified token claims.';
COMMENT ON COLUMN auth_pending_email_grants.last_claimed_external_subject IS
  'Last external IdP subject that claimed this email policy, such as a Firebase UID. Raw tokens are never stored.';

-- +goose Down
DROP TABLE IF EXISTS auth_pending_email_grants;
