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

The migration image records applied files in `goatos_schema_migrations`. For a
fresh `goatos-dev` Cloud SQL database, the migration image is the sole schema
applier. Do not run a separate goose/psql/local validator applier against that
same database; those validators remain local proof that the goose-style source
SQL still applies cleanly.

`apps/admin-web/Dockerfile` builds the Next standalone production server for the
dashboard surface. The app is compiled with `basePath: /dashboard`; no
`assetPrefix` is set because the service will sit behind the same HTTPS load
balancer path.

For the dev deploy, admin-web server-side API calls send the Goat OS app bearer
token in the standard `Authorization` header. The backend Cloud Run service must
therefore be publicly invokable at the Cloud Run layer and enforce auth at the
Goat OS app layer with JWKS/RBAC. Do not make the backend service IAM-private
until a separate service-to-service auth design exists that does not replace or
collide with the app bearer token.

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
