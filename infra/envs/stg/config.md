# goatos-stg API config

Authoritative env var list: `../../../docs/runbooks/deployment.md` ("Required
backend config per environment"). Real values come from Secret Manager; never
commit real secrets (`*.env.*` is gitignored on purpose).

stg specifics:

```text
GOATOS_ENV=stg
GOATOS_AUTH_MODE=jwks            # staging uses a real IdP; HS256 is not a staging mode
GOATOS_AUTH_AUDIENCE=goatos-api-stg
DATABASE_URL=<Cloud SQL goatos-stg, 1M synthetic baseline>
GOATOS_OBS_SINK=gcm
```

Run heavy load tests only here, never against prod.
