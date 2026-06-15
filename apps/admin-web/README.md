# Mesha Admin Web

This app is the Phase 1 admin-web console. It is intentionally not the full
Goat Passport workflow UI yet, but the main demo screens and already-defined
Phase 1 actions are backed by Mesha backend app/admin/analytics APIs through
server-side adapters.

The current console provides:

- the Next app builds;
- `@goatos/api-client` imports from `../../packages/api-client`;
- the executable legacy BigQuery/Sheets API routes are removed;
- a Mesha legacy-style dark sidebar, compact module navigation, breadcrumb
  header, KPI cards, tabs, charts, and dense tables;
- herd search, goat passport with live identity timeline, identity counts,
  conflict/candidate/correction queues, and live Import Review with a recent-run
  picker plus summary/row screens;
- server-side forms for the already-built Phase 1 actions: correction request
  create/resolve, candidate reject, conflict reject/merge, field-check
  requests, identifier-dispute marking, and goat identifier add/retire.

## Framework Baseline

Admin-web is pinned to the Mesha internal frontend baseline:

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

This app intentionally tracks the current Next/React/Tailwind line. Every new
frontend dependency, shadcn/Radix-style component, copied legacy component, or
chart/table package must be checked against Next 16, React 19, TypeScript 6, and
Tailwind 4 before it lands. If the latest package does not compose cleanly, pin
the latest compatible version and document the exception in the frontend
architecture doc, BUILD-STATUS, and this README. `lucide-react` `1.17.0` is the
intentional current 1.x icon baseline.

## Run The Console

For normal local development, start the API and admin-web together from the repo
root:

```bash
cd /Users/ravi/mesha/goatos
make dev-local
```

This starts or reuses the local API on `127.0.0.1:8080`, waits for readiness,
seeds the local `ceo_internal` tenant grant idempotently, mints a fresh bearer
token, and starts admin-web on `127.0.0.1:3300`. Use this path when the browser
shows `401 invalid_bearer_token`; it replaces stale shell tokens automatically.

Open `http://127.0.0.1:3300`.

For a durable local dashboard on this Mac, install the user LaunchAgent once:

```bash
cd /Users/ravi/mesha/goatos
make dev-local-service-install
```

That keeps the local API (`127.0.0.1:8080`) and admin-web
(`127.0.0.1:3300`) running behind a macOS user service. The local Postgres DB is
still the Docker container; the durable service supervises the two host
processes and restarts them if either one dies.

Useful service commands:

```bash
make dev-local-service-status
make dev-local-service-restart
make dev-local-service-stop
make dev-local-service-logs
make dev-local-service-uninstall
```

For direct admin-web-only checks:

```bash
cd /Users/ravi/mesha/goatos/apps/admin-web
npm install
npm run lint
npm run typecheck
npm run build
npm run dev:local
```

`dev:local` and `start:local` bind explicitly to `127.0.0.1:3300`, seed the
local grant, and mint a fresh server-side token unless
`GOATOS_LOCAL_DEV_AUTO_AUTH=false` is set. If port 3300 is busy, the script
fails and prints the owning process instead of silently moving to another port.

```bash
npm run start:local
```

## Environment

Backend calls are made from Next server components/adapters only. Set these in
the server process that runs plain `npm run dev`, `npm run build`, or
`npm run start`. `make dev-local`, `dev:local`, and `start:local` set local
defaults and mint the bearer token for you:

```bash
export GOATOS_API_BASE_URL=http://127.0.0.1:8080
export GOATOS_BEARER_TOKEN=<local-dev-token>
export GOATOS_TENANT_ID=<tenant-uuid>
export GOATOS_IMPORT_RUN_ID=<local-import-run-uuid>
```

`GOATOS_TENANT_ID` is required for `GET /analytics/identity/counts` because the
OpenAPI contract requires a `tenant_id` query parameter. It must match the
bearer token tenant and the seeded DB grant tenant, otherwise the counts page
shows the backend `tenant_scope_mismatch` denial.

`GOATOS_IMPORT_RUN_ID` is optional. It pins `/import-review` to a known run for
visual smoke or proof reruns; without it, `/import-review` lists recent tenant
import runs and lets the operator open one.

After a local backend and admin-web are already running, capture the live SSR
visual proof screenshots:

```bash
npm run smoke:visual:live
```

The smoke requires the same server-only env values, fetches a real goat_id from
`GET /goats/search`, captures desktop and narrow screenshots under the ignored
`.codex-goatos-render/admin-web-screenshots/` directory, exercises the live
overview/counts/herd/passport/Import Review/Data Quality surfaces, and fails if
it renders configuration-error pages, detects layout overflow/clipping,
detects serious or critical accessibility issues, renders debug trace footers,
or leaks the bearer token into HTML.

The smoke also supports a local visual baseline set for frontend QA:

```bash
npm run smoke:visual:update-baseline
npm run smoke:visual:baseline
```

`smoke:visual:update-baseline` captures the current route screenshots into the
ignored `.codex-goatos-render/admin-web-baselines/` directory.
`smoke:visual:baseline` compares a fresh run against that baseline and writes
diffs under the current screenshot directory if the pixel delta exceeds the
configured threshold. Use this when polishing a page so visual drift is reviewed
as an artifact instead of claimed by memory.

For frontend changes, the screenshots must be opened and reviewed before push;
do not rely on the command exiting successfully. When the changed page has a
legacy dashboard analogue, open the legacy page in a separate tab and compare
the local page against it for visual quality. For counts, use
`https://dashboard--goatos-sheets.us-central1.hosted.app/counts/overall`.
Review sidebar/nav alignment, tab/title spacing, typography, colors, card
padding, chart sizing, labels, icons, empty space, overflow, clipping,
desktop/narrow responsive states, and whether the screen actually has live data
instead of a configuration error. A `missing_config` render is not valid visual
proof; start the backend/admin-web with the required server-only env first.

Do not commit tokens or secrets. Do not put tokens in browser code, localStorage,
`NEXT_PUBLIC_*` env vars, rendered HTML, query params, or static files. The build
script runs a token leak guard after `next build`; when `GOATOS_BEARER_TOKEN` is
set, the guard scans client/static build output and fails if the token appears.

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

The backend defaults to bearer auth. `make dev-local` is the preferred local
path because it uses one shared `GOATOS_AUTH_*` config for the API and token
minting, avoids stale bearer tokens, and refuses non-local database targets.

The default local identity is:

```text
tenant_id = 00000000-0000-4000-8000-000000000001
user_id   = 90000000-0000-4000-8000-000000000101
role      = ceo_internal
```

Override it only when you need to test a specific role:

```bash
export GOATOS_LOCAL_USER_ID=<user-uuid>
export GOATOS_LOCAL_ROLE=verifier
make dev-local
```

For manual local auth debugging, use matching values for the API and token
minting:

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
  -role ceo_internal
```

The command is idempotent: if the same active local grant already exists, it
prints that grant and exits successfully instead of inserting duplicates.

`ceo_internal` is the intended local role for the internal admin surface. The
product decision is that it is a full Mesha product-admin role for Phase 1 API
actions, not a read-only dashboard role. That is app authorization only; it does
not imply Google Cloud, IAM, billing, GitHub, or repository administration.

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

When admin-web calls the backend, the bearer token stays server-side. Client
components receive rendered data; they do not call the backend with raw bearer
credentials.

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
deleted legacy routes; new admin screens must call Mesha app/admin/analytics
APIs through generated clients.

Legacy BigQuery is a temporary upstream while Goat OS backend, web, and mobile
capture workflows are being completed. During this bridge period the flow is:

```text
legacy BQ export -> backend sync/reconcile -> Goat OS DB -> backend API -> admin-web
```

Keep BQ table names, column names, and export-specific parsing inside backend
adapter/config code. Do not copy those names into React pages or generated UI
labels. When operators write directly into Goat OS, the BQ adapter should be
removable without changing admin-web screens.

## Import Review

`/import-review` is read-only. Without an `import_run_id`, it lists recent
tenant import runs from the backend so operators do not need to remember UUIDs.
Provide `import_run_id` as a query parameter only to open a specific older run:

```text
/import-review?import_run_id=<import-run-uuid>
```

The row table is keyset-paginated, supports `processing_state` and `reason_code`
filters, and shows only whitelisted review fields. Nullable run metrics that are
not tracked by Phase 1 import/apply are rendered as "Not tracked." The run
summary counts are navigation controls: click rows processed, goats created,
needs review, or errors to filter the row table for that run. Import-row
review/fix actions are still deferred; defined identity/correction decisions
live on Data Quality and Goat Passport.

The page also exposes server-side CSV downloads:

```text
/import-review/export?import_run_id=<import-run-uuid>&scope=messy
/import-review/export?import_run_id=<import-run-uuid>&scope=current
```

`scope=messy` downloads every `needs_review`, `rejected`, and `error` row for
the run. `scope=current` downloads the currently selected state/reason filter.
CSV generation is owned by the backend `rows.csv` route and relayed by the Next
server so the browser never receives the backend bearer token. The CSV contains
the same whitelisted Import Review row fields visible in the UI, not raw source
workbook payloads.
