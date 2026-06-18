# Container Images

Status: P3 containerization prerequisite.

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
/app/bin/rebuild-identity-counters
/app/bin/update-identity-counters
/app/bin/rfid-import
/app/bin/rfid-apply
/app/bin/bq-reconcile
/app/bin/seed-dev-grant
/app/bin/seed-dev-email-grants
/app/bin/mint-dev-token
```

The default entrypoint is `/app/bin/api`. Run another binary by overriding the
entrypoint. Example:

```bash
docker run --rm --entrypoint /app/bin/outbox-relay goatos-backend:local -limit 10
```

`bq-reconcile` is the current legacy sync executor primitive packaged for dev
bring-up. The dedicated scheduled legacy-sync executor remains the P13 build
item.

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

When P9 wires the Cloud Run migration job, all three guard envs above must be
present on the job spec along with the exact socket-form `DATABASE_URL`;
otherwise the image should self-reject before opening a database connection.

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
