# goatos-dev API config

Authoritative env var list: `../../../docs/runbooks/deployment.md` ("Required
backend config per environment"). Real values come from Secret Manager; never
commit real secrets (`*.env.*` is gitignored on purpose).

dev specifics:

```text
GOATOS_ENV=dev
GOATOS_AUTH_MODE=jwks            # real IdP for realistic debugging
GOATOS_AUTH_AUDIENCE=goatos-api-dev
DATABASE_URL=<Cloud SQL goatos-dev>
GOATOS_OBS_SINK=gcm
NEXT_PUBLIC_ENABLE_SOP_PLAYGROUND=true  # internal Phase 2 visualization only
```

HS256 bearer is only for throwaway local rehearsal and logs a non-production
warning when GOATOS_ENV is not local/dev/test.
