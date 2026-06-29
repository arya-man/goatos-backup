# Container Images

Status: active container build and dev deploy runbook.

Build from the repository root so Docker can see both app code and shared
packages:

```bash
docker build -f backend/Dockerfile -t goatos-backend:local .
docker build -f backend/Dockerfile.migrate -t goatos-migrate:local .
docker build -f apps/admin-web/Dockerfile -t goatos-admin-web:local .
```

## Image Families

`backend/Dockerfile` builds the Go multi-binary runtime image:

```text
/app/bin/api
/app/bin/outbox-relay
/app/bin/domain-event-consumer
/app/bin/domain-event-processed-sweeper
/app/bin/generate-vaccination-obligations
/app/bin/obligation-sweeper
/app/bin/calendar-vaccination-projector
/app/bin/calendar-reminder-sweeper
/app/bin/calendar-escalation-sweeper
/app/bin/notification-dispatcher
/app/bin/backfill-goat-created
/app/bin/idempotency-key-sweeper
/app/bin/inventory-batch-reconciler
/app/bin/sop-review-fanout-retry
/app/bin/seed-dev-grant
/app/bin/seed-dev-email-grants
/app/bin/seed-vaccination-trigger
/app/bin/mint-dev-token
```

The default entrypoint is `/app/bin/api`. Run another binary by overriding the
entrypoint. Local/dev outbox runs must choose an explicit non-durable publisher;
staging/production must use `GOATOS_OUTBOX_PUBLISHER=pubsub` and Pub/Sub topic
env instead.

```bash
docker run --rm \
  -e DATABASE_URL="${DATABASE_URL}" \
  -e GOATOS_OUTBOX_PUBLISHER=logging \
  -e GOATOS_OUTBOX_ALLOW_NONDURABLE=1 \
  --entrypoint /app/bin/outbox-relay \
  goatos-backend:local -limit 10
```

Expired shared idempotency keys are cleaned by a bounded job-style binary. Dry
run first, then execute with an operator-chosen limit:

```bash
docker run --rm \
  -e DATABASE_URL="${DATABASE_URL}" \
  --entrypoint /app/bin/idempotency-key-sweeper \
  goatos-backend:local -limit 1000 -dry-run

docker run --rm \
  -e DATABASE_URL="${DATABASE_URL}" \
  --entrypoint /app/bin/idempotency-key-sweeper \
  goatos-backend:local -limit 1000
```

Old processed domain-event dedupe rows are cleaned by a bounded job-style
binary. It deletes only `status='processed'` rows older than the retention
cutoff; `processing` and `failed` rows stay for retry/repair.

```bash
docker run --rm \
  -e DATABASE_URL="${DATABASE_URL}" \
  --entrypoint /app/bin/domain-event-processed-sweeper \
  goatos-backend:local -limit 1000 -dry-run
```

Inventory stock repair and SOP review fanout retry are also packaged as
job-style binaries in the list above.

Old import/reconciliation/reporting binaries are no longer packaged in the
runtime image. If migration audit tooling is reintroduced later, it must be a
new backend-only cutover artifact, not an admin-web product surface.


`backend/Dockerfile.migrate` builds a migration image containing
`/app/bin/migrate` plus `backend/migrations/postgres/*.sql`. It reads
`DATABASE_URL` and supports Cloud SQL Unix socket URLs through pgx. The default
migration directory is `/app/backend/migrations/postgres`.

For the `goatos-dev` Cloud SQL bring-up, `/app/bin/migrate` is dev Cloud
SQL-only and fails before connecting unless all of these are true:

```text
GOATOS_ENV=dev
GOATOS_ALLOW_DEV_CLOUDSQL_TARGET=true
GOATOS_DEV_CLOUDSQL_CONNECTION_NAME=goatos-dev:asia-south1:<instance>
DATABASE_URL host is exactly goatos-dev:asia-south1:<instance>
  or /cloudsql/goatos-dev:asia-south1:<instance>
```

The `goatos-dev` Terraform defines an unscheduled Cloud Run migration job with
the guard envs above and the exact Cloud SQL connection name. It must be run
manually by an operator after image publish and before API/admin traffic moves
to the new schema. If any guard env is missing or mismatched, the image
self-rejects before opening a database connection.

The migration image records applied files in `goatos_schema_migrations`. For a
fresh `goatos-dev` Cloud SQL database, the migration image is the sole schema
applier. Do not run a separate goose/psql/local validator applier against that
same database; those validators remain local proof that the goose-style source
SQL still applies cleanly.

`apps/admin-web/Dockerfile` builds the Next standalone production server for the
dashboard surface. The app is served from the root of the dashboard host; no
`assetPrefix` is set because the service sits behind the same HTTPS load
balancer host.

If the admin-web root path or any future Next `basePath` changes, update and
grep beyond the app directory. Local service health checks in `tools/dev`, smoke
scripts, auth/proxy cookie paths, deploy URLs, and runbooks must move together
so a cloud fix does not strand local development.

For the dev deploy, admin-web server-side API calls forward the signed-in
Firebase user's ID token in the standard `Authorization` header and the
configured tenant in `X-GoatOS-Tenant-ID`. The backend Cloud Run service must
therefore be publicly invokable at the Cloud Run layer and enforce auth at the
Goat OS app layer with JWKS/RBAC. Do not make the
backend service IAM-private until a separate service-to-service auth design
exists that does not replace or collide with the user's app bearer token. This
dev posture must not be copied to `goatos-stg` or `goatos-prod`.

## Admin-web dev deploy checklist

Git push is not a Google dev deploy. When a UI fix is meant to be visible at the
raw `goatos-admin-web-dev` Cloud Run URL, do all of these steps before telling
the operator the live dev UI is fixed.

Before any mutation, verify the normal Goat OS cloud/repo gate:

```bash
gcloud config list
gcloud auth list --filter=status:ACTIVE
gcloud organizations list
gcloud resource-manager folders describe 188649904255
gcloud projects get-ancestors goatos-dev
git remote -v
git status --short --branch
git config --get-regexp '^alias\.mesha-push$|^mesha-push\.'
```

Abort if the active account is not `ravi@mesha.sg`, the org is not
`vgoats.com`, the folder is not `goat-os / folders/188649904255`, the project is
not `goatos-dev`, or the repo is not `vgoats/goatos`.

Build and push the admin-web image for Cloud Run as `linux/amd64`. On Apple
Silicon, do not use a plain `docker build` image for Cloud Run; it can produce an
ARM image that deploys badly or is rejected.

```bash
SHA="$(git rev-parse --short=12 HEAD)"
IMAGE="asia-south1-docker.pkg.dev/goatos-dev/goatos/admin-web:${SHA}"

docker buildx build \
  --platform linux/amd64 \
  -f apps/admin-web/Dockerfile \
  -t "${IMAGE}" \
  --push \
  .
```

Deploy only the existing dev admin-web service to that image:

```bash
gcloud run deploy goatos-admin-web-dev \
  --image="${IMAGE}" \
  --region=asia-south1 \
  --project=goatos-dev \
  --quiet
```

If `gcloud run` crashes because the local Cloud SDK is using an unsupported
Python, set `CLOUDSDK_PYTHON` to a Python 3.10+ interpreter and rerun the same
command. Do not change project/account context to work around a local CLI issue.

Verify the live service before reporting success:

```bash
gcloud run services describe goatos-admin-web-dev \
  --project=goatos-dev \
  --region=asia-south1 \
  --format='value(status.latestReadyRevisionName,spec.template.spec.containers[0].image,status.url)'

curl -I --max-time 15 \
  https://goatos-admin-web-dev-farig3r27a-el.a.run.app/login
```

The image in the `services describe` output must match the current `IMAGE`, and
traffic must be on the new ready revision. If the operator is looking at the raw
Cloud Run URL, tell them to hard reload only after this verification passes.

If the dev custom hostname is in use, also verify it after the raw Cloud Run
smoke:

```bash
curl -I --max-time 15 https://dev.dashboard.mesha.sg/login
```

This custom-host smoke only proves DNS/TLS/LB/page load. Google sign-in also
requires the OAuth web client's authorized JavaScript origins to include
`https://dev.dashboard.mesha.sg`.

## Dev Defaults

Cloud Run services should set `PORT=8080`. Backend services and jobs that need
Postgres should mount Cloud SQL with:

```text
--add-cloudsql-instances=goatos-dev:asia-south1:<instance>
```

and use a socket-form `DATABASE_URL` whose host is:

```text
/cloudsql/goatos-dev:asia-south1:<instance>
```

The local/dev DB-writing helper guard requires exact
`GOATOS_DEV_CLOUDSQL_CONNECTION_NAME` matching for dev Cloud SQL rehearsals.
