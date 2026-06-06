# Admin Web Agent Context

Read first:

- `../../context/frontend/final-frontend-mobile-backend-architecture.md`
- `../../context/analytics/final-analytics-infra.md`

Purpose:

- Snapshot of the current CEO/admin dashboard UI for safe Goat OS rewiring.
- The live `../../dashboard/` repo is not touched.

Do:

- Preserve useful layout, charts, route inventory, and UX patterns.
- Move data access behind generated analytics/app clients.
- Gate pages by server-side auth/RBAC.

Do not:

- Do not add direct BigQuery/Sheets/GCS/DB access as the final data path.
- Do not expose unauthenticated real goat data.
- Do not treat this copy as proof that live dashboards have changed.
