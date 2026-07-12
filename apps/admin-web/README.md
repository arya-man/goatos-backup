# Mesha Admin Web

This app is the current Mesha internal admin surface for the vaccination
process-integrity build. It is not the old dashboard, not the old Import Review
console, and not a BigQuery/Sheets runtime UI.

## Current Product Slice

Build and verify these implemented surfaces only:

- `/login`
- `/` Control Tower
- `/calendar`
- `/action-center`
- `/protocol-adherence`
- `/workflows`
- `/workflows/{row_id}`
- `/vaccination`
- `/vaccination/execution/sheds/[shedId]`
- `/procurement/source-entry`
- `/procurement/source-entry/loads/{load_id}`
- `/counts/herd`
- `/operations/audit`
- `/operations/dlq`
- `/config`
- `/sops`
- `/goats/{goat_id}`

The working product model is:

- **Admin / Data Ops** owns generic protocol config and SOP policy.
- **Preventive Care (PC) / Vaccination** owns vaccination operations, proof, and verification.
  Vaccination execution context (park, shed, stage, defer/blocker, owner, SOP/proof
  status) renders INSIDE /vaccination#execution, not as a separate Parks route.
- **Command lenses** are top-level screens over the same backend truth: Control
  Tower, Action Center, Calendar, Protocol Adherence, and Workflows. Calendar
  is reopened only for vaccination-related due work, not all-domain mock content.
- **Control Tower** summarizes only broken or at-risk process. Do not build
  generic KPIs there.

## UI Source Of Truth

The only admin-web UI/UX source of truth is:

```text
../../mock/goatos-dashboard-mock.html
```

Port its layout, table shapes, empty states, icon system, spacing, density, and
interaction model. Do not reuse or recolor the old admin UI. The old dashboard
routes and old primitives have been removed from this app.

Required frontend gates:

```bash
npm run check:mock-fidelity
npm run lint
npm run typecheck
npm run build
```

When a local backend and admin-web are running, also run:

```bash
npm run smoke:visual:live
```

`smoke:visual:live` covers implemented routes only, including Calendar and the
Operations DLQ repair lane.

Open the generated screenshots under
`.codex-goatos-render/admin-web-screenshots/` before claiming visual QA.

## Local Development

From the repo root:

```bash
cd /path/to/goatos
make dev-local
```

Open `http://127.0.0.1:3300/`.

For admin-web-only checks:

```bash
cd /path/to/goatos/apps/admin-web
npm install
npm run dev:local
```

`dev:local` and `start:local` bind to `127.0.0.1:3300`, seed the local grant,
and mint a fresh server-side token unless `GOATOS_LOCAL_DEV_AUTO_AUTH=false` is
set.

## Environment

Backend calls are made from Next server components/adapters only:

```bash
export GOATOS_API_BASE_URL=http://127.0.0.1:8080
export GOATOS_BEARER_TOKEN=<local-dev-token>
export GOATOS_TENANT_ID=<tenant-uuid>
```

Do not put tokens in browser code, `NEXT_PUBLIC_*` variables, localStorage,
rendered HTML, query params, or static assets.

### Web RUM (Grafana Faro)

Frontend Real User Monitoring (`components/observability/faro-provider.tsx`,
OBSERVABILITY_DESIGN.md §2.4) is public browser config, so — unlike the backend
vars above — it is intentionally `NEXT_PUBLIC_*`:

```bash
# Grafana Alloy faro.receiver endpoint (infra lane; see
# docs/observability/OBSERVABILITY_DESIGN.md §1/§4). Leave unset to disable RUM
# entirely — local dev without a collector is a no-op, not an error.
export NEXT_PUBLIC_FARO_COLLECTOR_URL=https://alloy.example/collect

# Environment label attached to every RUM signal (e.g. local, stg, prod).
export NEXT_PUBLIC_GOATOS_ENV=local

# App version label attached to every RUM signal (release tag / build SHA).
export NEXT_PUBLIC_APP_VERSION=dev
```

Optional, only needed if the browser ever fetches the backend origin directly
(it does not today — all backend calls are proxied through Next server
components/route handlers per this doc's "Backend calls" note above). Faro's
fetch instrumentation always attaches `traceparent` to same-origin requests
without this:

```bash
export NEXT_PUBLIC_GOATOS_API_BASE_ORIGIN=https://api.example.com
```

When `NEXT_PUBLIC_FARO_COLLECTOR_URL` is unset, `FaroProvider` no-ops (no SDK
initialization, no network calls) — safe for local dev and any environment
without a deployed collector.

## Generated Client

The app imports `@goatos/api-client` from the repo package:

```json
"@goatos/api-client": "file:../../packages/api-client"
```

Regenerate and check drift from the repo root:

```bash
make api-client-generate
make api-client-check
./tools/agent-hooks/check-contract-drift.sh
```

## Hard No

- No direct BigQuery, Sheets, GCS, Firestore, or database reads from frontend.
- No old Import Review, Data Quality, Legacy Sync, Counts, Mortality, Operators,
  Tasks, old generic SOP builder, or old Herd routes as product screens.
- No nested command-lens routes such as `/vaccination/adherence`,
  `/vaccination/calendar`, or `/vaccination/workflows`.
- No old cyan/slate/admin-primitives visual system.
- No global Goat Passport search as the primary workflow.
