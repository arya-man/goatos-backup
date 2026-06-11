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
- `apps/admin-web` is currently the Phase 1 Mesha-style SSR-first internal admin
  surface for read-only demo screens backed by `@goatos/api-client`.
  Server-side adapters keep bearer tokens out of browser code; live screens
  cover the overview, herd search, goat passport, identity counts, Import
  Review, and data quality queues. The visible shell uses Mesha branding; Goat
  OS and VGoat labels are internal/legacy labels and must not appear in rendered
  admin-web UI copy.
- Fresh local closeout proof on June 12, 2026 rendered the Mesha admin-web
  against the real 711/512 RFID import run: overview, counts, herd search, a
  real goat passport, live Import Review rows, and an honest Data Quality empty
  state because that local DB had no conflicts or candidates.
- Admin-web is upgraded from the copied dashboard's Next 14 / React 18 stack to
  the frozen framework baseline in
  `context/frontend/final-frontend-mobile-backend-architecture.md`: Next
  16.2.9, React 19.2.7, React DOM 19.2.7, Tailwind 4.3.0, React Query 5.101.0,
  lucide 1.17.0, Recharts 3.8.1, and matching TypeScript/types/ESLint tooling.
  Do not silently build new screens on the old framework stack.
- Treat the frontend baseline as intentionally current. Every new frontend
  dependency, shadcn/Radix-style component, copied legacy component, or chart
  package must be checked against Next 16, React 19, TypeScript 6, and Tailwind
  4 before landing. If the latest package is not compatible, document the
  latest compatible pin and reason in the canonical frontend doc, BUILD-STATUS,
  and admin-web README.
- Use surface-level microfrontend discipline, not one giant dashboard bundle:
  admin/internal, investor/external, operator/device, and public/partner are the
  real surface boundaries. Phase 1 builds the admin surface now, with feature
  modules such as goat passport, import review, and analytics counts as
  standalone-capable route modules inside that surface.
- Prefer Next.js SSR/server components, route-level loading, dynamic imports for
  heavy charts/tables, backend pagination, and backend-shaped summaries. Do not
  client-render and ship every dashboard module up front.
- Stack rules: use TanStack Query only for client-interactive API views; use
  Zustand only for local UI state, never canonical goat/backend data or tokens;
  treat shadcn-style components as local source components; dynamically import
  Recharts when heavy; defer Auth.js until production web sessions are designed.
  Phase 1 bearer/dev tokens stay server-side and backend RBAC remains authority.
- When a second web surface lands, prefer Next.js Multi-Zones or separate Next
  apps routed by path/domain for independent surface deploys. Use Module
  Federation only after an explicit decision that runtime module-into-host
  remotes are needed inside a surface.
- No-rewrite rule: Phase 1 `admin-web` is the future `/admin` zone. Adding
  `apps/investor-web` or another surface later must be additive, not a rewrite,
  because shared UI, generated clients, auth/RBAC helpers, and server-side data
  adapters live behind package/public module boundaries from the start.
- Do not allow cross-module deep imports. Feature modules consume public module
  interfaces plus shared `packages/ui`, generated clients, auth, and RBAC
  helpers. The first slice that creates real admin feature modules must add a
  `check-boundaries.sh` guard or equivalent CI check for this boundary.
- Do not reintroduce executable `apps/admin-web/app/api/*` BigQuery/Sheets
  routes or `lib/bigquery.ts`. Deleted legacy route code is available in git
  history if needed as reference.
- The boundary guard checks rendered/admin-web-facing TypeScript and TSX for
  visible `Goat OS` or `VGoat` labels. Internal code identifiers such as
  `GOATOS_*`, `@goatos/api-client`, and `GoatOSApiError` remain allowed.
