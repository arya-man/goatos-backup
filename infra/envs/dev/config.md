# goatos-dev API config

Authoritative env var list: `../../../docs/runbooks/deployment.md` ("Required
backend config per environment"). Real values come from Secret Manager; never
commit real secrets (`*.env.*` is gitignored on purpose).

dev specifics:

```text
GOATOS_ENV=dev
GOATOS_AUTH_MODE=jwks            # Google Identity Platform / Firebase Auth
GOATOS_AUTH_ISSUER=https://securetoken.google.com/goatos-dev
GOATOS_AUTH_AUDIENCE=goatos-dev
GOATOS_AUTH_JWKS_URL=https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com
GOATOS_AUTH_ALLOWED_EMAILS=<approved Mesha Admin emails>
GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance>
DATABASE_URL=<Cloud SQL socket /cloudsql/goatos-dev:asia-south1:<instance>>
GOATOS_OBS_SINK=gcm
GOATOS_CANONICAL_DASHBOARD_HOST=dev.dashboard.mesha.sg   # admin-web
```

`goatos-dev` uses Google Identity Platform / Firebase Auth for auth only. Do not
use Firebase Hosting or Firebase App Hosting. `GOATOS_AUTH_ALLOWED_EMAILS`
narrows the Firebase/Google sign-in surface to approved admin emails; matching
DB grants are still required before dashboard APIs authorize. Pre-approved dev
dashboard admins are stored in `auth_pending_email_grants`; first verified
sign-in claims the real `user_scope_grants` row automatically.

Current dev dashboard admin email policy:

```text
abhishek@mesha.sg
aryaman@mesha.sg
manju@mesha.sg
ravi@mesha.sg
```

Admin-web sets `GOATOS_CANONICAL_DASHBOARD_HOST=dev.dashboard.mesha.sg` so raw
Cloud Run dashboard URLs redirect to the custom host before login. The shared
dev Cloud Run default URL should also stay disabled once the load balancer
hostname is healthy.

Migration jobs and guarded dev DB-writing helpers also set
`GOATOS_ALLOW_DEV_CLOUDSQL_TARGET=true`; do not put that opt-in on unrelated
runtime surfaces.

P9 migration job required env:

```text
GOATOS_ENV=dev
GOATOS_ALLOW_DEV_CLOUDSQL_TARGET=true
GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance>
DATABASE_URL=<Cloud SQL socket /cloudsql/goatos-dev:asia-south1:<instance>>
```

For this dev bring-up, the backend Cloud Run service is publicly invokable at
the Cloud Run layer and relies on Goat OS JWKS/RBAC for app auth. Admin-web uses
Firebase Auth client persistence plus an HTTP-only Firebase ID-token cookie so
SSR calls can forward the signed-in user's `Authorization: Bearer <id_token>` to
the backend, along with `X-GoatOS-Tenant-ID` from the configured
`GOATOS_TENANT_ID`. Do not make the backend IAM-private until a separate
service-to-service auth design is implemented.

HS256 bearer is only for throwaway local rehearsal. The API rejects HS256 unless
GOATOS_ENV is local/dev/test, and shared deploys should use jwks even in
goatos-dev when real IdP testing is available.
