# Frontend And Mobile Reference

Load this when working on admin-web, operator-mobile, UI reuse, RBAC
visibility, generated clients, offline sync, media capture, or app adapters.

Canonical docs:

- `context/frontend/current-admin-web-scope.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
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
Vaccination execution context scoped by park/shed
Control Tower process-gap summary
Goat Passport contextual drilldown
```

The current admin-web dashboard is a process-integrity product, not decorative
KPIs. Keep these four lenses, all vaccination-only:

```text
Control Tower      = process intact/not intact summary
Action Center      = exact work/gaps to act on now
Protocol Adherence = expected vs actual, gap, severity, owner, next action, evidence
Workflow drilldown = config -> obligation -> SOP -> proof -> verification -> completion
```

Use `context/frontend/vaccination-process-integrity-frontend-handoff.md` for the
mock-shaped frontend split before reshaping `/`, `/action-center`,
`/protocol-adherence`, `/workflows`, `/vaccination`, or
`/sops`.

Build these routes/surfaces only unless the user explicitly reopens scope:

```text
/login
/
/vaccination
/action-center
/protocol-adherence
/workflows
/workflows/{row_id}
/config
/sops
/goats/{goat_id}
```

Scope lock: do not turn shared engines into visible product breadth. For the
current `/sops` route, the backend SOP engine can remain generic, but admin-web
must present the vaccination SOP slice only. Show vaccination SOPs such as
`vaccination.drive` / `vaccination.*`; hide or zero/disable other domains in
the visible surface; do not show shifting, procurement, HR, counts, breeding,
feed, inventory, or generic health SOPs as active review cards.

Parks is a vertical, but it does NOT own the Vaccination product route/module.
Vaccination execution
context (park, shed, animal stage, defer/blocker state, owner chain, drive status,
proof status, verification status) renders ONLY inside /vaccination,
not as a separate Parks route or sidebar entry. Do not rebuild old Locations or a
generic Parks vertical.

Do not confuse "not generic Action Center" with "no Action Center." Action
Center is the top-level `/action-center` command lens. `/vaccination` is the PHC
Vaccination operations module only; it must not contain Action Center,
Protocol Adherence, Workflows, Config, or SOP Library as tabs, nested pages, or
large shortcut cards. The shared status model must power PHC/Parks/Control
Tower: due, overdue, blocked, proof-pending, verification-pending, rejected,
deferred, owner-missing, and completed.

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
