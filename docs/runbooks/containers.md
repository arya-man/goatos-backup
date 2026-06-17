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

`apps/admin-web/Dockerfile` builds the Next standalone production server for the
dashboard surface. The app is compiled with `basePath: /dashboard`; no
`assetPrefix` is set because the service will sit behind the same HTTPS load
balancer path.

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
