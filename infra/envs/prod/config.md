# goatos-prod API config

Authoritative env var list: `../../../docs/runbooks/deployment.md` ("Required
backend config per environment"). Real values come from Secret Manager only;
never commit real prod secrets and never share prod secrets with dev/stg
(`*.env.*` is gitignored on purpose).

prod specifics:

```text
GOATOS_ENV=prod
GOATOS_AUTH_MODE=jwks            # production MUST use asymmetric JWKS verification
GOATOS_AUTH_AUDIENCE=goatos-api-prod
DATABASE_URL=<Cloud SQL goatos-prod, live data>
GOATOS_OBS_SINK=gcm
```

`GOATOS_AUTH_MODE=bearer` (HS256) is rejected as a production posture.
