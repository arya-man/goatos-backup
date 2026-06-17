# goatos-dev API config

Authoritative env var list: `../../../docs/runbooks/deployment.md` ("Required
backend config per environment"). Real values come from Secret Manager; never
commit real secrets (`*.env.*` is gitignored on purpose).

dev specifics:

```text
GOATOS_ENV=dev
GOATOS_AUTH_MODE=jwks            # real IdP for realistic debugging
GOATOS_AUTH_AUDIENCE=goatos-api-dev
GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance>
DATABASE_URL=<Cloud SQL socket /cloudsql/goatos-dev:asia-south1:<instance>>
GOATOS_OBS_SINK=gcm
```

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
`GOATOS_BEARER_TOKEN` as the app bearer token; do not make the backend
IAM-private until a separate service-to-service auth design is implemented.

HS256 bearer is only for throwaway local rehearsal. The API rejects HS256 unless
GOATOS_ENV is local/dev/test, and shared deploys should use jwks even in
goatos-dev when real IdP testing is available.
