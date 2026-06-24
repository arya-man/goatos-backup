# Current Admin-Web Frontend Scope

This is the active rule for the Mesha admin-web rebuild.

## UI Source Of Truth

The only admin-web UI/UX source of truth is:

```text
/Users/ravi/mesha/goatos/mock/goatos-dashboard-mock.html
```

Port the mock's layout, screen structure, tables, empty states, icon system,
spacing, font scale, and density. It is not a color theme.

Do not reuse, adapt, or recolor the old admin UI. The old admin primitives,
chart components, layout shell, and dashboard routes have been removed.

Required before frontend push/handoff:

```bash
npm --prefix apps/admin-web run check:mock-fidelity
npm --prefix apps/admin-web run lint
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run build
```

When local backend/admin-web can run, capture and inspect screenshots:

```bash
npm --prefix apps/admin-web run smoke:visual:live
```

## Current Product Slice

Build one connected vaccination process-integrity slice:

```text
Admin / Data Ops config + SOP policy
  -> PHC Vaccination operations
  -> Parks vaccination execution context
  -> Control Tower gap summary
```

This means:

- **Admin / Data Ops** owns generic protocol config and SOP policy at
  `/config`.
- **PHC / Vaccination** owns obligations, drives, execution, proof upload,
  verification, missed/deferred handling, and adherence views.
- **Parks vaccination layer** shows physical context only where relevant to
  vaccination: park, shed, animal stage, defer status, blocker, owner chain,
  drive status, proof status, and verification status.
- **Action Center status logic** must exist underneath PHC/Parks as due,
  overdue, blocked, proof-pending, verification-pending, rejected, deferred, and
  owner-missing. The full standalone Action Center page can be added later.
- **Control Tower** is the top summary shell only: process intact/not intact,
  where, severity, owner, and next action. It must not become a generic KPI
  dashboard.

Do not build generic Parks, generic dashboards, old Locations, old Operations,
or old import/data-quality workflows in this slice.

## Active Routes

These are the only current admin-web product routes:

```text
/login
/
/vaccination
/vaccination/adherence
/config
/goats/{goat_id}
```

`/vaccination/config` may exist only as a compatibility redirect to
`/config?category=vaccination`.

Parks vaccination execution may be shown inside the vaccination workflow until a
specific Parks route is approved. If a Parks route is added later, it must be
vaccination-scoped and must not revive `/locations`.

Navigation should be:

```text
Control Tower
PHC
  Vaccination
Parks
  Vaccination execution
Admin / Data Ops
  Config
```

If Parks is not implemented yet, keep the nav leaf hidden rather than linking
to a placeholder product page.

## Role And IA Rules

- Real RBAC is server-enforced. The frontend only hides/shows permitted
  surfaces; it never grants authority.
- CEO/COO/superadmin may get a role-preview lens. Staff roles do not get a role
  switcher when they log in.
- Config publish/raw edit remains backend-gated by protocol capabilities.
- PHC is a vertical and must not use the syringe/injection icon.
- Vaccination may use the syringe/injection icon.
- Counts is a separate future vertical. Control Tower must not show raw goat
  census/count totals.
- Goat Passport is contextual drilldown only. Do not add global million-goat
  search as the main workflow.
- The hamburger beside the Mesha logo must actually collapse/expand desktop nav
  and open/close mobile nav.

## Config Rules

Config is one generic protocol authority screen:

```text
Admin / Data Ops -> Config
```

It is category/schema-driven. Vaccination, feed direction, deworming, and future
modules all use the same generic engine, but category changes the form fields
and `rule_dsl`.

Vaccination config may include animal/shed stage, age or post-arrival trigger,
sex where needed, schedule dose rows, booster/catch-up/missed-dose policy,
defer states such as ICU/quarantine/sick, SOP/proof policy, and stock/vaccine
lot requirements.

Feed Direction config uses different fields: animal stage, breed/class if
needed, ration/feed item, quantity/unit, session timing, packing/execution
proof, and inventory reserve/consume/release policy.

## Removed From Current Admin-Web

These routes/features were old dashboard or old Phase 1 review surfaces and are
removed from active admin-web:

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

Do not rebuild them unless the product scope is explicitly reopened and the
screen is rebuilt from the mock, not from old admin-web code.

## Completion Bar

Before asking for approval:

- `/login`, `/`, `/vaccination`, `/vaccination/adherence`, `/config`, and
  contextual `/goats/{goat_id}` match the mock structure and density.
- PHC/Vaccination, Admin Config, and Parks vaccination context are connected by
  real backend status/proof/verification data.
- Control Tower summarizes only broken or at-risk process.
- Config is generic and category/schema-driven.
- Old dashboard/admin routes are not visible in nav and have no active product
  implementation.
- No direct frontend access to BigQuery, Sheets, GCS, Firestore, or databases.
- No horizontal clipping or text overflow on desktop/narrow screenshots.

Do not resume full Feed Direction operational screens until PHC/Vaccination and
the generic Config foundation are reviewed and approved.
