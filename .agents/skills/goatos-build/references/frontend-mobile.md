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
  slice should turn it into the SSR-first internal admin surface for read-only
  demo screens backed by `@goatos/api-client`.
- Before building real Phase 1 screens, upgrade admin-web from the copied
  dashboard's Next 14 / React 18 stack to the frozen framework baseline in
  `context/frontend/final-frontend-mobile-backend-architecture.md`:
  Next 16.2.9, React 19.2.7, React DOM 19.2.7, matching
  `eslint-config-next`, TypeScript/types, Tailwind, React Query, lucide, and
  Recharts. Do not silently build new screens on the old framework stack.
- Use surface-level microfrontend discipline, not one giant dashboard bundle:
  admin/internal, investor/external, operator/device, and public/partner are the
  real surface boundaries. Phase 1 builds the admin surface now, with feature
  modules such as goat passport, import review, and analytics counts as
  standalone-capable route modules inside that surface.
- Prefer Next.js SSR/server components, route-level loading, dynamic imports for
  heavy charts/tables, backend pagination, and backend-shaped summaries. Do not
  client-render and ship every dashboard module up front.
- When a second web surface lands, prefer Next.js Multi-Zones or separate Next
  apps routed by path/domain for independent surface deploys. Use Module
  Federation only after an explicit decision that runtime module-into-host
  remotes are needed inside a surface.
- Do not allow cross-module deep imports. Feature modules consume public module
  interfaces plus shared `packages/ui`, generated clients, auth, and RBAC
  helpers. The first slice that creates real admin feature modules must add a
  `check-boundaries.sh` guard or equivalent CI check for this boundary.
- Do not reintroduce executable `apps/admin-web/app/api/*` BigQuery/Sheets
  routes or `lib/bigquery.ts`. Deleted legacy route code is available in git
  history if needed as reference.
