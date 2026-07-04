# Frontend And Mobile Reference

Load this when working on admin-web, operator-mobile, UI reuse, RBAC
visibility, generated clients, offline sync, media capture, or app adapters.

Canonical docs:

- `context/frontend/current-admin-web-scope.md`
- `context/frontend/vaccination-process-integrity-frontend-handoff.md`
- `context/execution/vaccination-process-integrity-backend-handoff.md`
- `context/execution/calendar-vaccination-slice-parallel-handoff.md`
- `docs/decisions/calendar-ownership.md`
- `context/architecture/operational-kernel.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/execution/target-repo-structure.md`
- `docs/preventive-care-vaccination/PRD.md`
- `docs/preventive-care-vaccination/TRD.md`
- `docs/protocol-engine/IMPLEMENTATION-PLAN.md`
- `docs/protocol-engine/obligation-engine.md`
- `docs/protocol-engine/state-machines.md`

## Current Admin-Web Build

The current admin-web slice is:

```text
Admin / Data Ops config + SOP policy
Preventive Care (PC) Vaccination operations
Calendar vaccination due-work command lens
Vaccination execution context scoped by park/shed
Control Tower process-gap summary
Animal Passport contextual drilldown
```

The current admin-web dashboard is a process-integrity product, not decorative
KPIs. Keep these command lenses, all vaccination-only:

```text
Control Tower      = process intact/not intact summary
Action Center      = exact work/gaps to act on now
Calendar           = vaccination due work by time, owner pill, park, shed, date
Protocol Adherence = expected vs actual, gap, severity, owner, next action, evidence
Workflow drilldown = config -> obligation -> SOP -> proof -> verification -> completion
```

Frontend must present the operational kernel honestly. It is a command surface
over backend truth, not a scheduler or source of truth. Screens should show what
process was expected, whether it was followed, where broken, who owns next
action, what is due by when, what evidence exists, and whether reminder,
deadline, or escalation state is active.

Use `context/frontend/vaccination-process-integrity-frontend-handoff.md` for the
mock-shaped frontend split before reshaping `/`, `/action-center`,
`/protocol-adherence`, `/workflows`, `/vaccination`, or
`/sops`.

Build these routes/surfaces only unless the user explicitly reopens scope.
Current implemented admin-web product routes:

```text
/login
/
/vaccination
/vaccination/execution/sheds/{shed_id}
/action-center
/protocol-adherence
/workflows
/workflows/{row_id}
/procurement/source-entry
/procurement/source-entry/loads/{load_id}
/counts/herd
/operations/audit
/config
/sops
/animals/{animal_id}
/calendar
```

`/calendar` is reopened only for the Preventive Care (PC) Vaccination due-work slice. It must use
the generic `CalendarEvent` summary contract, vaccination-specific detail drawer,
generated backend clients, active owner pills `all`, `pc`, `inventory`, and
`admin_data_ops`, and the mock Calendar/drawer UI treatment. Keep primary nav,
mock-fidelity scan coverage, and `smoke:visual:live` coverage in sync with the
route. Do not show live all-domain Calendar content.

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
Center is the top-level `/action-center` command lens. `/vaccination` is the Preventive Care (PC)
Vaccination operations module only; it must not contain Action Center,
Protocol Adherence, Workflows, Config, or SOP Library as tabs, nested pages, or
large shortcut cards. The shared status model must power Preventive Care (PC), Parks, and Control
Tower: due, overdue, blocked, proof-pending, verification-pending, rejected,
deferred, owner-missing, and completed. These are read-model/UI statuses; do not
ask backend to mutate canonical `obligation_instances.status` just to match a
Calendar or dashboard label.

## UI Rules

- `mock/goatos-dashboard-mock.html` is the only admin-web UI/UX source of truth.
- Port the mock's layout, table shapes, empty states, icon system, spacing,
  density, and interaction model.
- Backend-driven UI contract is mandatory. Frontend must not invent product
  truth. Visible navigation, page titles, section/table labels, column labels,
  filter/sort/page-size semantics, chips/tabs, row-click params, drawer/action
  labels, disabled reasons, empty/error copy, and summary/detail field sets must
  come from backend app/OpenAPI contracts. Frontend owns only layout, CSS,
  responsive density, icon-token mapping, focus/hover behavior, and local
  open/closed or selected-row state. For admin-web, load
  `context/frontend/admin-web-backend-ui-contract.md` before changing shell,
  route bodies, tables, filters, chips, or drawers.
- Backend-driven does not permit backend-code live-data constants. Tenant,
  location/park, person, goat, shed, vendor/operator IDs, CBE/CPT-style codes,
  capacities, role scope, permissions, and governed dropdown vocabularies must
  come from Postgres/source-backed config and be compiled into the backend
  contract. Stable UI text that rarely changes (nav/page titles, table/filter
  labels, chips/tabs, empty/error copy, disabled reasons) also belongs in the
  backend contract; when runtime governance is needed, store it as tenant-scoped
  `admin_ui_config_entries` and compile it into `/admin-web/bootstrap`.
  UI config entries must not relabel live/module-owned option groups such as
  parks, sheds, breeds, SOP labels, feed items, or role/grant scopes, and must
  not override semantic option metadata such as source-system publishability.
  Frontend must not render local defaults and then replace them with async
  config. Static backend code may hold only product contract shape, compile
  mapping, and intentional default skeletons for missing optional UI config rows.
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
- Frontend timers, localStorage, mock rows, or optimistic UI state must never be
  the canonical reminder, deadline, escalation, proof, or obligation state.
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
