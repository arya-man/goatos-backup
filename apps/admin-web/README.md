# Mesha Admin Web

This app is the current Mesha internal admin surface for the vaccination
process-integrity build. It is not the old dashboard, not the old Import Review
console, and not a BigQuery/Sheets runtime UI.

## Current Product Slice

Build and verify these surfaces only:

- `/login`
- `/`
- `/vaccination`
- `/vaccination/adherence`
- `/config`
- `/goats/{goat_id}`

The working product model is:

- **Admin / Data Ops** owns generic protocol config and SOP policy.
- **PHC / Vaccination** owns vaccination operations, execution, proof, and
  verification.
- **Parks vaccination layer** provides physical execution context: park, shed,
  stage, defer/blocker state, owner chain, and linked vaccination drive status.
- **Action Center status logic** exists underneath the slice as due, overdue,
  blocked, proof-pending, verification-pending, rejected, deferred, and
  owner-missing state. A standalone Action Center page can come later.
- **Control Tower** is allowed as the root summary shell, but it must summarize
  only broken or at-risk process. Do not build generic KPIs there.

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

Open the generated screenshots under
`.codex-goatos-render/admin-web-screenshots/` before claiming visual QA.

## Local Development

From the repo root:

```bash
cd /Users/ravi/mesha/goatos
make dev-local
```

Open `http://127.0.0.1:3300/`.

For admin-web-only checks:

```bash
cd /Users/ravi/mesha/goatos/apps/admin-web
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
  Tasks, SOP builder, or Herd routes as product screens.
- No old cyan/slate/admin-primitives visual system.
- No global Goat Passport search as the primary workflow.
