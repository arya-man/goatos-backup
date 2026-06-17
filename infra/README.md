# Goat OS Infra

Intended IaC + environment layout for the three Goat OS Google Cloud projects.
Provisioning runs under the Mesha/VGoats org only — see
`../docs/runbooks/google-cloud-environments.md` and
`../docs/runbooks/deployment.md`.
Container image build/run notes live in `../docs/runbooks/containers.md`.

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

Committed here: per-env backend config templates, the deployment procedure,
P4 goatos-dev Terraform remote-state backend wiring, and the P7-preapply dev
Layer 1 foundation plan.

P4 completed for goatos-dev only:
- Terraform state bucket `gs://goatos-dev-tf-state` was bootstrapped
  imperatively in project `goatos-dev`, region `asia-south1`.
- Bucket posture: uniform bucket-level access enabled, public access prevention
  enforced, object versioning enabled, no retention lock.
- Dev Terraform backend path: `gs://goatos-dev-tf-state/terraform/dev`.

P5/P6 completed for goatos-dev only:
- Required dev APIs are enabled.
- Dev-only budget `Goat OS dev monthly budget` is scoped to
  `projects/634659905829`, amount INR 4,750 monthly, with 50/80/100 percent
  current-spend alerts.

P7-preapply completed for goatos-dev only:
- Terraform validates and plans Layer 1 foundation resources: Artifact Registry,
  small Cloud SQL Postgres shell, Secret Manager containers only, Pub/Sub
  outbox/DLQ wiring, runtime service accounts, and pre-Cloud-Run IAM.

P8 raw-URL dev dashboard bring-up switches dev Cloud SQL to
`activation_policy = "ALWAYS"` so migrations and serving can use the instance.
This is a running-cost posture while the raw dev dashboard is live.

Still blocked on later explicit operator approval in the verified vgoats.com
context:
- Terraform apply for Layer 1 foundation resources.
- Layer 2 app deploy resources: Cloud Run services/jobs, Scheduler jobs, LB/DNS,
  Secret Manager secret versions, Cloud SQL users/passwords, image pushes,
  migrations, and legacy imports.
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
