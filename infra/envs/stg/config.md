# goatos-stg API config

Authoritative env var list: `../../../docs/runbooks/deployment.md` ("Required
backend config per environment"). Real values come from Secret Manager; never
commit real secrets (`*.env.*` is gitignored on purpose).

stg specifics:

```text
GOATOS_ENV=stg
GOATOS_AUTH_MODE=jwks            # staging uses a real IdP; HS256 is not a staging mode
GOATOS_AUTH_ISSUER=https://securetoken.google.com/goatos-stg
GOATOS_AUTH_AUDIENCE=goatos-stg
GOATOS_AUTH_JWKS_URL=https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com
DATABASE_URL=<Cloud SQL goatos-stg, 1M synthetic baseline>
GOATOS_OBS_SINK=gcm
```

Dashboard auth supports Google SSO plus Firebase email/password and forgot
password. The Firebase email/password provider must be enabled in `goatos-stg`;
approved users still need `GOATOS_AUTH_ALLOWED_EMAILS` plus database grants.
Initial password access is seeded by creating/verifying the approved Firebase
users and sending Firebase password-reset emails; there is no shared bootstrap
password. See `docs/runbooks/auth.md`.

## Dashboard endpoint, DNS, and OAuth

Staging dashboard public host:

```text
URL:         https://stg.dashboard.mesha.sg/
Project:     goatos-stg
Region:      asia-south1
Cloud Run:   goatos-admin-web-stg
Raw URL:     https://goatos-admin-web-stg-awtrpmn4za-el.a.run.app
LB IP:       8.233.143.24
LB IP name:  goatos-stg-dashboard-ip
Certificate: goatos-stg-dashboard-cert
```

DNS lives in Cloudflare, not Google Cloud DNS:

```text
Cloudflare account: Manju@flokx.io's Account
Cloudflare account id: 13c352a0cade56bf65b77c0d8b78bf53
Zone: mesha.sg
Record: A stg.dashboard -> 8.233.143.24
Proxy: DNS only
TTL: Auto
```

Use this verification set after DNS or LB changes:

```bash
dig +short stg.dashboard.mesha.sg A
gcloud compute ssl-certificates describe goatos-stg-dashboard-cert \
  --project=goatos-stg \
  --global \
  --format='json(managed.status,managed.domainStatus)'
curl -fsSI https://goatos-admin-web-stg-awtrpmn4za-el.a.run.app/login
```

Do not set `GOATOS_CANONICAL_DASHBOARD_HOST=stg.dashboard.mesha.sg` on
admin-web until the Google-managed certificate is `ACTIVE`; otherwise the raw
Cloud Run URL redirects users to a host that may still fail TLS.

Google SSO uses the classic Google Auth Platform web client below. The broken
IAM OAuth UUID-style client must not be used for Google Identity Services.

```text
OAuth client display name: goatos-stg-admin-web
OAuth client id: 514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com
Google Auth Platform app name: Goat OS Staging
Audience: Internal
Support/contact email: ravi@mesha.sg
Secret Manager: goatos-stg-google-oauth-web-credential
```

Authorized JavaScript origins:

```text
https://stg.dashboard.mesha.sg
https://goatos-admin-web-stg-514832198871.asia-south1.run.app
https://goatos-admin-web-stg-awtrpmn4za-el.a.run.app
http://localhost:3000
http://localhost:3300
http://localhost:3311
```

Authorized redirect URIs:

```text
https://goatos-stg.firebaseapp.com/__/auth/handler
https://goatos-stg.web.app/__/auth/handler
```

The `goatos-stg` Identity Platform Google provider is enabled and must point to
the same OAuth client ID. Firebase/Auth Platform authorized domains must include
`localhost`, `goatos-stg.firebaseapp.com`, `goatos-stg.web.app`, and
`stg.dashboard.mesha.sg`.

Seed the matching dashboard DB grants with the staging-guarded helper:

```bash
GOATOS_ENV=stg \
GOATOS_ALLOW_STG_CLOUDSQL_TARGET=true \
GOATOS_STG_CLOUDSQL_CONNECTION_NAME=goatos-stg:asia-south1:goatos-stg-core-db \
DATABASE_URL="postgres://goatos_app:<password>@localhost:5433/goatos?sslmode=disable" \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
make seed-stg-email-grants
```

Run heavy load tests only here, never against prod.
