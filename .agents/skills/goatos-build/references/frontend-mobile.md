# Frontend And Mobile Reference

Load this when working on admin-web, dashboards, operator-mobile, UI reuse,
RBAC visibility, generated clients, offline sync, media capture, or app adapters.

Canonical docs:

- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/execution/target-repo-structure.md`
- `context/execution/two-dev-build-plan.md`

Rules:

- Existing live dashboard repos are reference/source material and must stay untouched during Goat OS rewiring.
- Take fresh snapshots/clones into `goatos/apps/admin-web` and `goatos/apps/investor-web-shadow`; change only those copies.
- Keep useful UI/components, replace data path with Goat OS APIs/analytics APIs in the goatos copies.
- Current live dashboard URLs keep running until the new Goat OS dashboards validate against them.
- Operators use Android task/form app, not BI dashboards.
- App code depends on generated clients and adapters, not direct vendor SDK calls.
- `packages/api-client` is the generated OpenAPI TypeScript client package for
  app/admin/analytics APIs. Admin-web and future mobile screens must use it
  through small app adapters instead of hand-copying DTOs.
- `apps/admin-web` is currently a Phase 1 readiness shell: disabled tabs only,
  generated-client import smoke, and no data fetches until real screen slices
  land.
- Do not reintroduce executable `apps/admin-web/app/api/*` BigQuery/Sheets
  routes or `lib/bigquery.ts`. Deleted legacy route code is available in git
  history if needed as reference.
- No microfrontends until separate teams/release cadences justify them.
