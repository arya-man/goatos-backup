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
