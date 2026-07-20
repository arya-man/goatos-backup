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
GOATOS_APPCHECK_ENFORCE=off       # switch to monitor, then enforce, after Android/App Check rollout
GOATOS_APPCHECK_ISSUER=https://firebaseappcheck.googleapis.com/514832198871
GOATOS_APPCHECK_AUDIENCE=projects/514832198871
GOATOS_APPCHECK_JWKS_URL=https://firebaseappcheck.googleapis.com/v1/jwks
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
Certificate status: ACTIVE
Canonical host env: GOATOS_CANONICAL_DASHBOARD_HOST=stg.dashboard.mesha.sg
```

Staging API public host:

```text
URL:         https://stg-api.dashboard.mesha.sg/
Project:     goatos-stg
Region:      asia-south1
Cloud Run:   goatos-api-stg
Raw URL:     https://goatos-api-stg-awtrpmn4za-el.a.run.app
LB IP:       8.233.143.24
NEG:         goatos-api-stg-neg
Backend:     goatos-api-stg-backend
URL map:     goatos-stg-dashboard-map host rule stg-api.dashboard.mesha.sg -> api-host
Certificate: goatos-stg-api-cert
Android stg API_BASE_URL: https://stg-api.dashboard.mesha.sg/
```

DNS lives in Cloudflare, not Google Cloud DNS:

```text
Cloudflare account: Manju@flokx.io's Account
Cloudflare account id: 13c352a0cade56bf65b77c0d8b78bf53
Zone: mesha.sg
Record: A stg.dashboard -> 8.233.143.24
Record: A stg-api.dashboard -> 8.233.143.24
Proxy: DNS only
TTL: Auto
```

If there is no scoped Cloudflare API token available locally, use the logged-in
Cloudflare browser session to add/update the DNS record. Do not store a
personal Cloudflare token in `.zshrc`. For future automation, create a
zone-scoped token for `mesha.sg` with only `Zone:Read` and `DNS:Edit`, then
store it as a GitHub secret such as `CLOUDFLARE_API_TOKEN_MESHA_DNS`; store the
zone id separately as `CLOUDFLARE_ZONE_ID_MESHA_SG`.

Use this verification set after DNS or LB changes:

```bash
dig +short stg.dashboard.mesha.sg A
dig +short stg-api.dashboard.mesha.sg A
gcloud compute ssl-certificates describe goatos-stg-dashboard-cert \
  --project=goatos-stg \
  --global \
  --format='json(managed.status,managed.domainStatus)'
gcloud compute ssl-certificates describe goatos-stg-api-cert \
  --project=goatos-stg \
  --global \
  --format='json(managed.status,managed.domainStatus)'
curl -fsSI https://goatos-admin-web-stg-awtrpmn4za-el.a.run.app/login
curl -fsSI https://stg.dashboard.mesha.sg/login
curl -sSI https://stg-api.dashboard.mesha.sg/app/bootstrap | sed -n '1,8p'
```

Do not set `GOATOS_CANONICAL_DASHBOARD_HOST=stg.dashboard.mesha.sg` on
admin-web until the Google-managed certificate is `ACTIVE`; otherwise the raw
Cloud Run URL redirects users to a host that may still fail TLS.

Do not create prod DNS/certs until the prod API/backend/Firebase project are
live. Prod should reuse the same shape later:

```text
Android package: sg.mesha.goatos
Firebase project: goatos-prod/goatos-prd (final name to be created/confirmed)
API host: https://api.dashboard.mesha.sg/ or the final approved prod API host
Dashboard host: https://dashboard.mesha.sg/
```

Google SSO uses the classic Google Auth Platform web client below. The broken
IAM OAuth UUID-style client must not be used for Google Identity Services.

```text
OAuth client display name: goatos-stg-admin-web
OAuth client id: 514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com
Google Auth Platform app name: Goat OS Staging
Audience: External
Publishing status: In production
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

Start the Cloud SQL Auth Proxy with a Unix socket path, not a localhost TCP
port:

```bash
cloud-sql-proxy --unix-socket /tmp/cloudsql goatos-stg:asia-south1:goatos-stg-core-db
```

Then run the seed with a `DATABASE_URL` whose `host` visibly contains the exact
staging Cloud SQL connection name. The guard intentionally rejects
`localhost:5433` for staging because a local TCP port does not prove which Cloud
SQL instance the proxy is connected to.

```bash
GOATOS_ENV=stg \
GOATOS_ALLOW_STG_CLOUDSQL_TARGET=true \
GOATOS_STG_CLOUDSQL_CONNECTION_NAME=goatos-stg:asia-south1:goatos-stg-core-db \
DATABASE_URL="postgres://goatos_app:<password>@/goatos?host=/tmp/cloudsql/goatos-stg:asia-south1:goatos-stg-core-db&sslmode=disable" \
GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 \
make seed-stg-email-grants
```

Run heavy load tests only here, never against prod.

## Terraform source support

`infra/envs/stg/*.tf` now models the staging foundation and app runtime shape:
Artifact Registry, Cloud SQL, Secret Manager containers, runtime IAM, Pub/Sub,
Cloud Tasks, Cloud Run services/jobs, Scheduler, proof-media GCS, and Monitoring.
It has not been applied. Existing manually-created staging resources must be
imported or left manual before any future `terraform apply`.

The API and admin-web services are publicly invokable at the Cloud Run layer for
staging. This is intentional for the current staging path: Android, admin-web SSR,
and deploy smoke checks need a reachable app API, while protected data routes
remain gated by Firebase/JWKS plus backend RBAC. Production must make its own
ingress decision before build-out.
