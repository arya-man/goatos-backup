# Security And Ops Reference

Load this when working on auth, RBAC, dashboard access, secrets, IAM, Slack token
cleanup, prod agent access, or deployment safety.

Canonical docs:

- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `docs/features/operator-management/PRD.md`
- `docs/features/operator-management/TRD.md`
- `docs/features/operator-management/AGENT-TASK-ADMIN.md`
- `docs/features/operator-management/AGENT-TASK-ANDROID.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/phc-vaccination/TRD.md`

Rules:

- Gate dashboards with auth/RBAC.
- Gate Android operator execution with Operator Management active profile,
  database grants, capabilities, device/session state, and app bootstrap; token
  claims or Slack membership are not permission authority.
- Rotate leaked Slack/Redis tokens before removing code references.
- Store secrets in Secret Manager or env-specific secret stores, never source.
- Enforce prod read-only for agents through IAM, not markdown promises.
- Separate dev/stg/prod projects, DBs, datasets, service accounts, and secrets.
- Customer/investor-facing data is sanitized read-model data, not raw operational truth.
- `backend/cmd/mint-dev-token` and `backend/cmd/seed-dev-grant` are local/dev
  bootstrap helpers only. They must not become production issuance or grant
  management surfaces.
- Dev grant seeding is never a migration; it requires explicit role input,
  requires `GOATOS_ENV` to be exactly `local`, `dev`, or `test`, and must refuse
  production/staging-looking, non-local, or Cloud SQL-style socket DB targets.
- Local auth smoke uses one shared `GOATOS_AUTH_*` config across backend,
  token minting, grant seed, and admin-web generated-client smoke.
- For the `goatos-dev` Cloud Run bring-up, backend invocation is public at the
  Cloud Run layer and auth is enforced by Goat OS JWKS/RBAC. Admin-web SSR uses
  the Firebase ID-token cookie bridge to forward the signed-in user's
  `Authorization: Bearer <id_token>` plus `X-GoatOS-Tenant-ID`; do not make the
  backend IAM-private until a separate service-to-service auth design exists.
  Do not copy this public-invoker posture to `goatos-stg` or `goatos-prod`.
- `goatos-dev` uses Google Identity Platform / Firebase Auth as the JWKS IdP.
  Firebase is auth only; do not use Firebase Hosting or Firebase App Hosting.
- Admin-web session POST/DELETE records durable auth audit events through the
  backend `/auth/session-events` route. The backend verifies the Firebase/JWKS
  bearer token, maps the external UID to the stable internal actor UUID, and
  writes `audit_log` actions such as `auth.sign_in`,
  `auth.session_refresh`, `auth.sign_out`, and verified-token
  `auth.failed_sign_in` without storing raw Firebase or Google tokens.
  The admin-web proxy only pre-filters missing, malformed, or expired
  Firebase ID-token cookies; it does not verify signatures. Backend JWKS
  verification remains the trust boundary for auth audit and data routes.
  Pin shared environments with `GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS` and
  keep `GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE` enabled unless a controlled
  local smoke explicitly disables it with `0`.
- Shared dashboard JWKS environments must set non-empty
  `GOATOS_AUTH_ALLOWED_EMAILS`; the API fails closed at startup if it is empty.
  Treat Google provider `hd` values as picker hints only; the backend
  verified-email allowlist plus DB grants are the real access-control boundary.
  Pre-approved first-time users belong in
  `auth_pending_email_grants`; verified `auth.sign_in` claims the real
  `user_scope_grants` row automatically and records
  `auth.pending_email_grant_claimed`. Do not grant synthetic Firebase UIDs.
- Once a custom dashboard host is live, set
  `GOATOS_CANONICAL_DASHBOARD_HOST` on admin-web and disable the Cloud Run
  default URL where supported so raw `*.run.app` hosts do not become parallel
  SSO surfaces.
- Before paid dev infra apply, verify the goatos-dev-only budget on billing
  account `01FEDE-96BCB3-76D992` and keep Terraform free of secret versions,
  database users/passwords, API keys, bearer tokens, or Firebase config values.
