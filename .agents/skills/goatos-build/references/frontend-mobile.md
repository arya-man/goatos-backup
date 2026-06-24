# Frontend And Mobile Reference

Load this when working on admin-web, operator-mobile, UI reuse, RBAC
visibility, generated clients, offline sync, media capture, or app adapters.

Canonical docs:

- `context/frontend/current-admin-web-scope.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/execution/target-repo-structure.md`
- `docs/phc-vaccination/PRD.md`
- `docs/phc-vaccination/TRD.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`

## Current Admin-Web Build

The current admin-web slice is:

```text
Admin / Data Ops config + SOP policy
PHC Vaccination operations
Parks vaccination execution context
Control Tower process-gap summary
Goat Passport contextual drilldown
```

Build these routes/surfaces only unless the user explicitly reopens scope:

```text
/login
/
/vaccination
/vaccination/adherence
/config
/goats/{goat_id}
```

Parks is in scope only for vaccination context: park, shed, animal stage,
defer/blocker state, owner chain, drive status, proof status, and verification
status. Do not rebuild old Locations or a generic Parks vertical.

The standalone Action Center page can come later, but the shared status model
must already power PHC/Parks: due, overdue, blocked, proof-pending,
verification-pending, rejected, deferred, and owner-missing.

## UI Rules

- `mock/goatos-dashboard-mock.html` is the only admin-web UI/UX source of truth.
- Port the mock's layout, table shapes, empty states, icon system, spacing,
  density, and interaction model.
- Do not reuse or recolor old admin-web UI, old `admin-primitives`, old chart
  components, old layout components, or old dashboard routes.
- Run `npm --prefix apps/admin-web run check:mock-fidelity` before frontend
  handoff.
- Run lint/typecheck/build, and run `smoke:visual:live` when local backend and
  admin-web can be started.

## Data Access Rules

- Admin-web and mobile use generated OpenAPI clients and small app adapters.
- Browser/mobile code must not read BigQuery, Sheets, GCS, Firestore, or
  operational databases directly.
- Backend RBAC remains authority. Frontend visibility is convenience, not
  security.
- Tokens stay server-side for admin-web. Do not put bearer tokens in
  `NEXT_PUBLIC_*`, localStorage, rendered HTML, query params, or static assets.

## Removed From Active Frontend Scope

Do not revive these old admin/dashboard features unless product scope is
explicitly reopened and the screen is rebuilt from the mock:

```text
counts dashboard
mortality dashboard
herd search as a global primary surface
Import Review product UI
Data Quality queues
Legacy Sync UI
old Locations page
old Operators page
old SOP builder page
old Tasks page
old admin-primitives/charts/layout components
old app/api BigQuery or Sheets routes
```

Historical docs and git history may contain those names; treat them as
archaeology, not active build instructions.
