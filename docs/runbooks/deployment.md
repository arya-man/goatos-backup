# Deployment Runbook — goatos-dev / goatos-stg / goatos-prod

Status: deploy procedure + config are committed here. Actual cloud provisioning
and deploy execution are **external operator actions** and are blocked from the
build workspace (see "What is blocked" below). This runbook is the contract for
how a Goat OS backend release reaches a shared/staging/production environment in
the correct organization.

Read the org boundary first: `docs/runbooks/google-cloud-environments.md`.

## Org / project guardrail (run before ANY cloud command)

Goat OS cloud work targets the Mesha/VGoats organization only.

```text
Organization: vgoats.com  (org id 563962826703)
Folder:       goat-os      (folder id 188649904255)
Projects:     goatos-dev | goatos-stg | goatos-prod   (already created, billing linked)
Never:        Heva / Slice orgs, hevaplatform, goatos-sheets (legacy, untouched)
```

Before any create/update/delete/IAM/billing/deploy command, verify and state the
active account, org, folder, project, and target repo:

```bash
gcloud config list --format="text(core.account,core.project)"   # expect ravi@mesha.sg, project explicit
```

If the active context is not Mesha/VGoats, stop and fix context first.

## Environment posture

| Env | Purpose | Data | Auth |
| --- | --- | --- | --- |
| `goatos-dev` | real-data debug clone, non-authoritative | real-shaped clone | `jwks` (real IdP) — HS256 only for throwaway local rehearsal |
| `goatos-stg` | scale rehearsal + load tests | 1M synthetic baseline | `jwks` |
| `goatos-prod` | live truth | production | `jwks` only |

The local dev-token demo (`make dev-local`, `mint-dev-token`, `seed-dev-grant`)
is not a deployment path. `seed-dev-grant` refuses shared/staging/production
targets; goatos-dev Cloud SQL use requires the explicit dev Cloud SQL opt-in
guard, an exact `GOATOS_DEV_CLOUDSQL_CONNECTION_NAME` match, and is for dev
rehearsal only.

## Dev API and budget gate

P5 enabled only the approved `goatos-dev` APIs:

```text
artifactregistry.googleapis.com
billingbudgets.googleapis.com
cloudbuild.googleapis.com
cloudscheduler.googleapis.com
compute.googleapis.com
identitytoolkit.googleapis.com
pubsub.googleapis.com
run.googleapis.com
secretmanager.googleapis.com
sqladmin.googleapis.com
```

Compute API enablement auto-created the default VPC and default firewall rules.
They are unused for the current Cloud Run plus Cloud SQL connector/socket plan;
do not delete or modify them unless explicitly approved.

P6 created the dev-only budget:

```text
Billing account: 01FEDE-96BCB3-76D992
Budget:          Goat OS dev monthly budget
Budget id:       f90ceaf4-efea-4c4f-9059-32b57b349992
Scope:           projects/634659905829 (goatos-dev only)
Amount:          INR 4,750 monthly (billing-account currency; about USD 50)
Alerts:          50%, 80%, 100% current spend
```

## Terraform state

The `goatos-dev` Terraform state bucket was bootstrapped imperatively before
Terraform backend init:

```text
Bucket:       gs://goatos-dev-tf-state
Project:      goatos-dev
Location:     asia-south1
State prefix: terraform/dev
```

The bucket has uniform bucket-level access, public access prevention enforced,
and object versioning enabled. It stores Terraform state only: no goat data,
legacy exports, secrets, app artifacts, container images, or migration payloads.
Do not delete or modify the state bucket unless explicitly approved.

## Dev Layer 1 foundation plan

P7-preapply adds Terraform for Layer 1 foundation only and stops before apply.
The validated plan is expected to create:

```text
Artifact Registry Docker repo: asia-south1-docker.pkg.dev/goatos-dev/goatos
Cloud SQL Postgres shell:      goatos-dev-core-db / database goatos
Cloud SQL tier:                db-f1-micro
Cloud SQL activation policy:   ALWAYS (running while raw dev dashboard is live)
Cloud SQL connectivity:        public IPv4 + future /cloudsql connector socket
Pub/Sub:                       outbox topic, analytics subscription, DLQ
Secret Manager:                regional containers only, no secret versions
IAM:                           runtime SAs, Cloud SQL client, secret access,
                               topic publisher, Pub/Sub service-agent DLQ IAM
```

The plan must not include Cloud Run services/jobs, Scheduler jobs, load
balancing, DNS, VPC connectors, Cloud SQL users/passwords, authorized networks,
Secret Manager secret versions, Firebase Hosting/App Hosting, image pushes,
migrations, or legacy imports.

## Dev Cloud Run invocation decision

For the `goatos-dev` bring-up, deploy the backend Cloud Run service as publicly
invokable at the Cloud Run layer and enforce authentication/authorization inside
Goat OS with `GOATOS_AUTH_MODE=jwks` and DB-backed RBAC grants. Live admin-web
uses Firebase Auth client persistence plus an HTTP-only Firebase ID-token cookie
so server-side calls can forward the signed-in user's
`Authorization: Bearer <id_token>` to the backend. Admin-web does not currently
mint a separate Cloud Run IAM identity token. Making the backend service
IAM-private before adding a separate service-to-service auth design will fail as
a Cloud Run 403 before the request reaches Goat OS app auth.

This is a dev-only bring-up shortcut. Do not copy the public-invoker Cloud Run
posture to `goatos-stg` or `goatos-prod`; those environments are blocked until a
service-to-service/IAM or IAP design exists that preserves Goat OS app auth
rather than replacing the user's app bearer token.

## Required backend config per environment

The API binary (`backend/cmd/api`) is configured entirely through env vars. A
per-env template lives at `infra/envs/<env>/goatos-api.env.example`. Production
values come from Secret Manager, never from committed files.

```text
# HTTP
GOATOS_HTTP_ADDR=:8080

# Database (Cloud SQL connection string / socket)
DATABASE_URL=postgres://.../goatos?sslmode=...
GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance> # dev guard only

# Auth — shared/staging/prod MUST USE jwks (see docs/runbooks/auth.md)
GOATOS_AUTH_MODE=jwks
GOATOS_AUTH_ISSUER=<idp issuer URL>
GOATOS_AUTH_AUDIENCE=<goat-os api audience>
GOATOS_AUTH_JWKS_URL=<idp JWKS endpoint>
GOATOS_AUTH_CLOCK_SKEW=60s            # optional
GOATOS_AUTH_ALLOWED_ALGS=RS256,ES256  # optional
GOATOS_AUTH_JWKS_CACHE_TTL=10m        # optional
GOATOS_AUTH_MAX_TOKEN_TTL=24h         # optional ceiling

# Environment + observability
GOATOS_ENV=stg|prod                    # NOT local/dev/test for shared envs
GOATOS_OBS_SINK=gcm                    # stdout_json | otlp | gcm
```

`GOATOS_AUTH_MODE=bearer` (HS256) is rejected unless `GOATOS_ENV` is
`local`, `dev`, or `test`. Shared/staging/prod must run `jwks`.

`goatos-dev` uses Google Identity Platform / Firebase Auth as the JWKS IdP.
Firebase is auth only; do not use Firebase Hosting or Firebase App Hosting. For
dev Firebase tokens, set issuer `https://securetoken.google.com/goatos-dev`,
audience `goatos-dev`, and JWKS URL
`https://www.googleapis.com/service_accounts/v1/jwk/securetoken@system.gserviceaccount.com`.

## Release steps

1. **Build + publish images.** Build the three image families in
   `docs/runbooks/containers.md`: backend multi-binary, migration job, and
   admin-web dashboard. Tag by git SHA, push to the project's asia-south1
   Artifact Registry, and record the SHA — it is the rollback handle.
2. **Apply migrations.** Migrations live in `backend/migrations/postgres/`
   (`000001`..`000022`, forward-only, never edit an applied migration).
   `make validate-migrations` validates the goose-style SQL locally. For a fresh
   `goatos-dev` Cloud SQL database, run the migration image from
   `docs/runbooks/containers.md`; it is the sole shared-DB applier and records
   applied files in `goatos_schema_migrations`. Do not mix it with a separate
   goose/psql/local validator applier on the same Cloud SQL database. Migrations
   must run to completion before the new image serves traffic. The `goatos-dev`
   migration job must set `GOATOS_ENV=dev`,
   `GOATOS_ALLOW_DEV_CLOUDSQL_TARGET=true`,
   `GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance>`, and
   a socket-form `DATABASE_URL` whose host is exactly
   `/cloudsql/goatos-dev:asia-south1:<instance>`.
3. **Seed the admin grant (first deploy only).** Production authorization comes
   from active `user_scope_grants` rows, not from token claims. For Firebase
   dev login, map the non-UUID Firebase UID to the backend's stable internal
   actor UUID, then insert the initial `ceo_internal`/`admin` tenant-scope grant
   for that actor and tenant through a reviewed migration or an explicit,
   audited grant script — `seed-dev-grant` is not a staging/production bootstrap
   path and will refuse those targets.
4. **Deploy.** Roll the new image. Keep the previous revision available for
   rollback.
5. **Smoke.** See "Smoke checks" — must pass before announcing the release.
6. **Counters.** After any large data load, run `rebuild-identity-counters` for
   the tenant, then confirm analytics freshness is not `rebuild_required`.

## Smoke checks

```text
GET /livez     -> 204 (no auth; use this for Cloud Run liveness smoke)
GET /readyz    -> 204 (DB reachable)
A real bearer token (from the IdP) on a read route (e.g. GET /goats/search) -> 200
A token with wrong issuer/audience/alg -> 401
GET /analytics/identity/counts -> 200 with freshness fields, no rebuild_required after a clean rebuild
```

`GET /healthz` remains a local/container liveness route, but raw Cloud Run/GFE
can reserve or intercept that exact path before it reaches the container. Use
`/livez` for public Cloud Run smoke checks.

`backend/tests/integration/smoke-auth-local.sh` is the local analogue; the
production smoke uses a real IdP token instead of a minted HS256 token.

## Rollback

```text
1. Redeploy the previous image SHA (fast path; no data change).
2. If a migration caused the failure: a forward-only fix migration is preferred.
   Only use a down-migration if the change is provably reversible and no rows
   depend on it. Never hand-edit data to "undo" — write a corrective migration.
3. Counters/projections are rebuildable: re-run rebuild-identity-counters after
   any rollback that touched identity data.
```

## Monitoring / alerts (per env)

Wire these before calling an environment production-ready:

```text
API latency (p50/p95/p99) and error rate per route
DB pressure: connections, slow queries, Cloud SQL CPU/mem
Outbox lag: pending/oldest-unpublished age, dead_letter count
Counter freshness: rebuild_required true, projection staleness
Import/sync run failures and conflict volume
```

Observability uses `GOATOS_OBS_SINK=gcm` (Google Cloud Monitoring/Logging/Trace)
per `docs/decisions/observability.md`. OpenTelemetry spans/metrics exporters are
a deferred hardening item.

## What is blocked (external operator action, not Phase 1 code)

The following require cloud access in the verified `vgoats.com` context and are
**not** performed from the build workspace:

```text
- Running terraform apply for the P7 Layer 1 foundation plan or authoring later
  app-resource modules beyond Layer 1.
- Provisioning Cloud SQL, GCS, Pub/Sub topics, Secret Manager secrets, service
  accounts, and Artifact Registry per project.
- Standing up a production IdP/JWKS endpoint and loading signing keys/secrets.
- Wiring Cloud Scheduler / Cloud Run Job for the production-safe legacy sync
  executor. Initial cadence should be configurable per source; start with
  15-minute polling only where upstream freshness and BigQuery cost budgets allow.
- Wiring the Pub/Sub outbox publisher + worker deploy (see event egress
  follow-up in BUILD-STATUS).
- Running the migration apply, image deploy, and smoke against real projects.
```

These are production-launch tasks. The Phase 1 backend, migrations, auth modes
(including `jwks`), and worker entrypoints are built and locally verified; what
remains is provisioning + deploy execution under the correct org.
