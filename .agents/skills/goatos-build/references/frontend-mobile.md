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
- `apps/admin-web` is currently a Phase 1 readiness shell. The next Phase 1 UI
  slice should turn it into the composition shell for read-only demo screens
  backed by `@goatos/api-client`.
- Use a module-first frontend architecture: admin-web is the shell, shared UI
  components live in packages, and major product areas such as goat passport,
  import review, and analytics counts should be standalone-capable feature
  modules that can run inside the shell and be exercised independently during
  development/demo.
- Phase 1 should not add runtime module federation or separately deployed
  microfrontends unless explicitly approved. The immediate goal is clean module
  boundaries and standalone-capable development, not deployment complexity.
- Do not reintroduce executable `apps/admin-web/app/api/*` BigQuery/Sheets
  routes or `lib/bigquery.ts`. Deleted legacy route code is available in git
  history if needed as reference.
