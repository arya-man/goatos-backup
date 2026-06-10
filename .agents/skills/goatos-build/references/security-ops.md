# Security And Ops Reference

Load this when working on auth, RBAC, dashboard access, secrets, IAM, Slack token
cleanup, prod agent access, or deployment safety.

Canonical docs:

- `context/execution/env-load-test-and-doc-hygiene.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`

Rules:

- Gate dashboards with auth/RBAC.
- Rotate leaked Slack/Redis tokens before removing code references.
- Store secrets in Secret Manager or env-specific secret stores, never source.
- Enforce prod read-only for agents through IAM, not markdown promises.
- Separate dev/stg/prod projects, DBs, datasets, service accounts, and secrets.
- Customer/investor-facing data is sanitized read-model data, not raw operational truth.
- `backend/cmd/mint-dev-token` and `backend/cmd/seed-dev-grant` are local/dev
  bootstrap helpers only. They must not become production issuance or grant
  management surfaces.
- Dev grant seeding is never a migration; it requires explicit role input and
  must refuse production/staging-looking or non-local DB targets.
- Local auth smoke uses one shared `GOATOS_AUTH_*` config across backend,
  token minting, grant seed, and admin-web generated-client smoke.
