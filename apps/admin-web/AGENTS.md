# Admin Web Agent Context

Read first:

- `../../context/frontend/current-admin-web-scope.md`
- `../../mock/goatos-dashboard-mock.html`

## Current Scope

Admin-web is being rebuilt around the vaccination process-integrity slice:

- **Admin / Data Ops**: generic protocol config and SOP policy at `/config`.
- **PHC / Vaccination**: vaccination operations at `/vaccination` and
  `/vaccination/adherence`.
- **Parks vaccination layer**: park/shed/stage/defer/blocker/owner context
  inside the vaccination workflow. Do not build generic Parks yet.
- **Goat Passport**: contextual drilldown at `/goats/{goat_id}` only.
- **Control Tower**: root summary shell for broken or at-risk process only.

The full Action Center page and full Control Tower product can come later, but
the status model underneath PHC/Parks must exist: due, overdue, blocked,
proof-pending, verification-pending, rejected, deferred, and owner-missing.

## UI Source Of Truth

`../../mock/goatos-dashboard-mock.html` is the only admin-web UI/UX source of
truth. Port its layout, table shapes, empty states, icon system, spacing, font
scale, and density. It is not a color theme.

Do not reuse, adapt, recolor, or recreate the old admin UI. The old
`admin-primitives` component, old chart/layout components, and old dashboard
routes have been deleted.

Required before frontend handoff:

```bash
npm run check:mock-fidelity
npm run lint
npm run typecheck
npm run build
```

When local backend/admin-web can run:

```bash
npm run smoke:visual:live
```

Open the generated screenshots under
`.codex-goatos-render/admin-web-screenshots/` before claiming visual QA.

## Active Routes

Only these routes are current product routes:

```text
/login
/
/vaccination
/vaccination/adherence
/config
/goats/{goat_id}
```

`/vaccination/config` may redirect to `/config?category=vaccination` for
compatibility, but Config itself stays generic and category/schema-driven.

## Hard Rules

- PHC is a vertical and must not use the syringe/injection icon.
- Vaccination may use the syringe/injection icon.
- Parks/Sheds are execution context for vaccination, not a generic Parks
  product build in this slice.
- Control Tower must not show raw goat census/count totals or generic dashboard
  KPIs.
- Goat Passport is reached from scoped rows, cohorts, obligations, or known URLs;
  do not add a global million-goat search as the main workflow.
- Backend/API/RBAC/session wiring may be reused; old frontend routes and old
  visual shell must not be reused.
- Do not add direct BigQuery, Sheets, GCS, Firestore, or database access from
  frontend code.

## Removed From Active Admin-Web

Do not rebuild these unless the product scope is explicitly reopened:

```text
/counts
/locations
/operators
/sops
/tasks
/import-review
/data-quality
/data-quality/review-guide
/legacy-sync
/dashboard/mortality
/herd
```

The live `../../dashboard/` repo remains untouched reference material only.
