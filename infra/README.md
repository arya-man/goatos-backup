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
goatos-dev Terraform remote-state/backend foundation wiring, and source-only
goatos-stg Terraform support. Staging Terraform has not been applied from this
workspace.

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

Staging Terraform source support exists under `envs/stg/`:
- Project guardrails pin `goatos-stg`, project number `514832198871`, folder
  `188649904255`, and region `asia-south1`.
- The composition models Artifact Registry, Cloud SQL, Secret Manager
  containers, runtime IAM, Pub/Sub/DLQ, Cloud Tasks, Cloud Run services/jobs,
  Scheduler, GCS proof media, and baseline Monitoring.
- Cloud SQL defaults to `activation_policy = "NEVER"` until an explicit staging
  rehearsal/deploy window.
- Existing manually-created staging resources must be imported or kept manual
  before any apply.

P8 raw-URL dev dashboard bring-up temporarily switched dev Cloud SQL to
`activation_policy = "ALWAYS"` so migrations and serving could use the
instance. The dev dashboard was archived on 2026-07-11, so Terraform now keeps
Cloud SQL at `activation_policy = "NEVER"` until an explicit dev rollout
reactivates it.

Dev custom dashboard URL is archived:
- `https://dev.dashboard.mesha.sg/`
- Cloudflare DNS still has the historical `A dev.dashboard -> 8.232.140.161`
  record, but the Google global static IP and load-balancer resources were
  deleted on 2026-07-11 to stop the public dashboard edge billing.
- Historical Google Cloud global HTTPS LB recreation commands live in
  `envs/dev/README.md`.
- Firebase/Auth Platform authorized domains include `dev.dashboard.mesha.sg`.
- The Google OAuth web client allows JavaScript origin
  `https://dev.dashboard.mesha.sg`, and Google Auth Platform Branding app name
  is `Mesha`.

Still blocked on later explicit operator approval in the verified vgoats.com
context:
- Terraform apply for Layer 1 foundation resources.
- goatos-stg Terraform backend bucket bootstrap, imports for existing manual
  staging resources, and any future apply.
- Remaining app-resource execution: stg/prod LB/DNS Terraform ownership,
  Secret Manager secret versions, Cloud SQL users/passwords, image pushes,
  migrations, seed/import commands, and legacy imports.
- Production IdP/JWKS endpoint + signing-key/secret provisioning.
- Pub/Sub outbox publisher + worker deploy wiring.
```

The module directories are intentional placeholders. Environment compositions
may exist before apply, but do not treat committed Terraform as evidence that a
resource exists until the matching project state/import/apply has been verified.

## Rules

- Keep dev, stg, and prod isolated: no shared DBs, buckets, topics, datasets, or
  secrets.
- Enforce prod read-only agent access through IAM, not markdown.
- Run heavy load tests only in `goatos-stg`.
