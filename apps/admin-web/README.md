# Goat OS Admin Web

This app is the Phase 1 admin-web readiness shell. It is intentionally not the
full Goat Passport UI yet.

The current shell proves:

- the Next app builds;
- `@goatos/api-client` imports from `../../packages/api-client`;
- the executable legacy BigQuery/Sheets API routes are removed;
- Phase 1 tabs are visible but disabled until the next frontend slice wires real
  screens through Goat OS APIs.

## Framework Baseline

Admin-web is pinned to the Goat OS frontend baseline:

```text
Next.js 16.2.9
React 19.2.7
React DOM 19.2.7
Tailwind CSS 4.3.0 with @tailwindcss/postcss 4.3.0
TanStack Query 5.101.0
lucide-react 1.17.0
Recharts 3.8.1
TypeScript 6.0.3
ESLint 9.39.4 with eslint-config-next 16.2.9
```

ESLint is pinned to the latest compatible 9.x release because ESLint 10 crashes
inside the current Next 16 React lint plugin stack. Upgrade that only after the
Next/React lint plugins support it cleanly.

## Run The Shell

```bash
cd /Users/ravi/mesha/goatos/apps/admin-web
npm install
npm run lint
npm run typecheck
npm run build
npm run dev -- --hostname 127.0.0.1 --port 3000
```

Open `http://127.0.0.1:3000`.

Use another port if `3000` is busy:

```bash
npm run dev -- --hostname 127.0.0.1 --port 3001
```

## Environment

The shell does not fetch data yet. The generated-client smoke reads these
optional environment variables so later screens can use the same shape:

```bash
export GOATOS_API_BASE_URL=http://127.0.0.1:8080
export GOATOS_BEARER_TOKEN=<local-dev-token>
```

Do not commit tokens or secrets.

## Generated Client

The app imports `@goatos/api-client` through a local file dependency:

```json
"@goatos/api-client": "file:../../packages/api-client"
```

Regenerate the client from OpenAPI:

```bash
cd /Users/ravi/mesha/goatos
make api-client-generate
```

Check drift:

```bash
make api-client-check
./tools/agent-hooks/check-contract-drift.sh
```

The contract drift hook now fails if OpenAPI changes without regenerated
`packages/api-client/src/generated/*` files.

## Local Dev Auth

The backend defaults to bearer auth. For local development, use one shared
`GOATOS_AUTH_*` config for the API and token minting:

```bash
export GOATOS_ENV=local
export GOATOS_AUTH_MODE=bearer
export GOATOS_AUTH_ISSUER=goatos-local
export GOATOS_AUTH_AUDIENCE=goatos-api
export GOATOS_AUTH_HS256_SECRET=<at-least-32-bytes>
export GOATOS_AUTH_MAX_TOKEN_TTL=24h
export DATABASE_URL=postgres://postgres:goatos@127.0.0.1:5432/goatos?sslmode=disable
```

Seed an active tenant grant into a local database only:

```bash
cd /Users/ravi/mesha/goatos/backend
go run ./cmd/seed-dev-grant \
  -tenant-id 00000000-0000-4000-8000-000000000001 \
  -user-id 90000000-0000-4000-8000-000000000101 \
  -role operator
```

Mint a local dev token:

```bash
go run ./cmd/mint-dev-token \
  -tenant-id 00000000-0000-4000-8000-000000000001 \
  -user-id 90000000-0000-4000-8000-000000000101 \
  -ttl 30m
```

`seed-dev-grant` requires `GOATOS_ENV` to be exactly `local`, `dev`, or `test`.
It refuses production/staging-looking targets and non-local database hosts,
including Cloud SQL-style Unix socket paths. It is not a migration and does not
silently create admin grants.

When admin-web calls the backend, the bearer token must stay server-side. Do not
put local dev tokens in browser code, localStorage, public env vars, rendered
HTML, or query params. Client components receive rendered data or call server
adapters, not the backend with raw bearer credentials.

## Auth Smoke

The local smoke starts Docker Postgres, applies migrations, starts the backend,
seeds a local grant, mints tokens, calls `GET /goats/search?limit=10`, and runs
the admin-web generated-client typecheck using the same auth config:

```bash
cd /Users/ravi/mesha/goatos
backend/tests/integration/smoke-auth-local.sh
```

The smoke is local-only and does not require cloud databases.

## BigQuery And Sheets

Executable BigQuery/Sheets routes have been removed from this app:

```text
apps/admin-web/app/api/
apps/admin-web/lib/bigquery.ts
```

The old live dashboard repos remain untouched. Git history is the reference for
deleted legacy routes; new admin screens must call Goat OS app/admin/analytics
APIs through generated clients.
