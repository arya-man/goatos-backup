# Goat OS Infra

Intended IaC + environment layout for the three Goat OS Google Cloud projects.
Provisioning runs under the Mesha/VGoats org only — see
`../docs/runbooks/google-cloud-environments.md` and
`../docs/runbooks/deployment.md`.

## Layout

```text
infra/
  modules/        terraform modules (scaffold; to be authored)
    cloud-sql/    Postgres instance per project
    cloud-run/    api + worker services
    gcs/          object/media storage
    pubsub/       outbox event egress topics/subscriptions
    secret-manager/ GOATOS_AUTH_* + DATABASE_URL secrets
    iam/          per-project service accounts, prod read-only agent access
    bigquery/     historical identity facts/marts
  envs/
    dev/  stg/  prod/    per-env composition + config.md (env var template)
  scripts/        deploy/migrate helper scripts (to be authored)
```

## Status

```text
Created (see google-cloud-environments.md): org folder goat-os, projects
goatos-dev/stg/prod, billing linked.

Committed here: per-env backend config templates (envs/<env>/goatos-api.env.example)
and the deployment procedure (docs/runbooks/deployment.md).

BLOCKED on external operator action in the verified vgoats.com context:
- Terraform module + env composition authoring and terraform plan/apply.
- Cloud SQL / GCS / Pub/Sub / Secret Manager / IAM / Artifact Registry provisioning.
- Production IdP/JWKS endpoint + signing-key/secret provisioning.
- Pub/Sub outbox publisher + worker deploy wiring.
```

The module/env directories are intentional placeholders. Do not commit
speculative Terraform that cannot be `terraform validate`/`plan`-checked against
the real projects; author it during provisioning with cloud access.

## Rules

- Keep dev, stg, and prod isolated: no shared DBs, buckets, topics, datasets, or
  secrets.
- Enforce prod read-only agent access through IAM, not markdown.
- Run heavy load tests only in `goatos-stg`.
