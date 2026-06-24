# Current Admin-Web Frontend Scope

This is the active rule for the Mesha admin-web rebuild.

## Product Taxonomy (READ FIRST — fixed words)

These words are not interchangeable:

- **Vertical** = business operating domain/department. Examples: PHC, Parks,
  Procurement, Admin/Data Ops, Counts, Breeding, Inventory, HR/People, Farmer
  Network.
- **Module** = a concrete workflow/product inside a vertical. Examples:
  PHC -> Vaccination, PHC -> future Treatment/Deworming, Procurement -> Source
  Entry, or future Parks-owned modules. Parks is only a scope/context dimension
  for Vaccination, not the owner of Vaccination execution.
- **Operational module screen** = where module work happens. Examples:
  `/vaccination` for PHC -> Vaccination, including operations, status matrix,
  cohort detail, and park/shed execution context scoped by the top-bar park
  dropdown, and `/procurement/source-entry` for Procurement -> Source Entry. Parks
  does NOT own a vaccination product screen; do not create `/parks/vaccination`
  routes, redirects, API contracts, or feature folders.
- **Command lens** = top-level cross-module screen, not a vertical and not a
  module. Control Tower, Action Center, Protocol Adherence, and Workflows are
  command lenses. They read rows/events/status from modules and answer
  leadership/work/adherence/workflow questions.
- **Authority screen** = top-level Admin/Data Ops authoring surface. Config
  (`/config`) and SOP Library (`/sops`) are authority screens.

Correct example:

```text
Vertical: PHC
  Module: Vaccination
    Operational screen: /vaccination

Top-level command lenses:
  /                       Control Tower
  /action-center          Action Center
  /protocol-adherence     Protocol Adherence
  /workflows              Workflows
```

Wrong example:

```text
PHC
  Vaccination
    Action Center          forbidden
    Protocol Adherence     forbidden
    Workflows              forbidden
```

## Platform Model (READ FIRST — do not misread this as a vaccination app)

Goat OS is a **generic, multi-vertical operating system**. It is NOT a vaccination
app.

```text
Platform (generic engine: protocol rules, obligations, SOP tasks, proof/media,
          verification/rework, process-integrity row shape — all module-agnostic)
  └── Verticals (e.g. PHC, and more)
        └── Modules (e.g. Vaccination today; Feed Direction, deworming, and others next)
```

- **Vaccination is one module under the PHC vertical** — it is *today's* visible
  build slice, not the app's identity. Feed Direction and other modules are coming
  and use the **same generic engine**.
- Anywhere a rule says "vaccination-only," it means the **current visible content
  slice**, never that the app, engine, shell, Config, or SOP layer is
  vaccination-specific. Those layers stay generic and category/schema-driven.
- Do NOT stamp "Vaccination" as a global/app-wide scope (no app-wide vaccination
  badge, label, or assumption). The active module is shown by the sidebar nav and
  the page crumb (`PHC › Vaccination`), nothing more.
- Do NOT bake vaccination into generic layers; do NOT show unbuilt modules as live.

## Non-Negotiable IA Rule

**Vaccination-only is data scope, not UI hierarchy. Mock hierarchy wins.**

Control Tower, Action Center, Protocol Adherence, and Workflows are **top-level
command-room screens** — exactly where the mock puts them — not tabs inside PHC /
Vaccination. Config and SOP Library are top-level Admin / Data Ops authority
screens. The selected module/domain filters their content; it never relocates
them under a vertical. `/vaccination` is the PHC vaccination operations surface
only.

This same rule applies to every vertical. Modules under Procurement, PHC, or
any future vertical may feed these command-room screens via a selected
domain/filter/lens, but must not duplicate them as nested page routes,
compatibility redirects, tabs, nav items, or big in-page shortcut panels. Do not
create routes or tab panels such as `/vaccination/adherence`,
`/vaccination/workflows`, `/vaccination/config`,
`/procurement/source-entry/action-center`,
`/procurement/source-entry/control-tower`, or any `/parks/vaccination/*` product
paths. Parks is not a vaccination module.

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

## Scope Chrome Rule

Keep park/date scope in the top bar. Remove repeated park/date/scope chips from
page bodies.

The top bar is the single visible owner for the current park and as-of date
scope across Control Tower, Action Center, Protocol Adherence, Workflows, PHC
Vaccination, Vaccination execution context, Config, and SOP Library. If a route or
query selects a park such as CBE, the top bar must show that selected park; it
must not still say "All parks".

Page bodies may show page-specific controls only. For example, Action Center may
show Status board/SOP queues, domain chips, work-state chips, My tasks, Filters,
and the board columns. It must not also show "Scope", "All parks", or the same
as-of date again in the body. If a page needs a park filter, place it behind
Filters or update the top-bar scope instead of duplicating it inline.

## Current Product Slice

Build one connected vaccination process-integrity slice:

```text
Admin / Data Ops config + SOP policy
  -> PHC Vaccination operations
  -> Vaccination execution context scoped by park/shed
  -> Control Tower gap summary
```

The dashboard purpose is process integrity: config + SOP set the process, and
the product proves whether that process is being followed. The four visible
lenses read the same backend truth:

Architecture boundary:

```text
Reusable engine underneath:
  protocol rules, obligations, SOP tasks/submissions, proof/media,
  verification/rework, and process-integrity row shape.

Visible product slice now:
  PHC / Vaccination only.
```

Do not make future modules impossible by baking vaccination into the generic
engine, but also do not show future modules as live product before they are
built.

```text
Control Tower      = is vaccination process intact / not intact, where, owner, next action
Action Center      = exact vaccination work/gaps to act on now
Protocol Adherence = expected vs actual for vaccination rules + SOP proof
Workflow drilldown = config -> obligation -> SOP task -> proof -> verification -> completion
```

Control Tower and Protocol Adherence are intentionally separate:

```text
Protocol Adherence:
  detailed expected-vs-actual ledger, including gaps, deferred/explained rows,
  evidence, and adherence math.

Control Tower:
  exception-only leadership summary of broken/at-risk vaccination process,
  with severity, owner, and next action.
```

Do not remove or dismiss these screens as generic dashboard fluff. The mistake
to avoid is showing all-domain mock content. The correct implementation is
mock-shaped, vaccination-only process integrity.

This means:

- **Admin / Data Ops** owns generic protocol config at `/config` and the
  reopened generic SOP Library / form-builder surface at `/sops`.
- **PHC / Vaccination** owns obligations, drives, execution, proof upload,
  verification, missed/deferred handling, status matrix, cohort detail, and
  adherence links.
- **Vaccination execution context** renders INSIDE PHC / Vaccination at
  `/vaccination`, scoped by the top-bar park dropdown. It is
  powered by the vaccination execution read-model endpoints (`/vaccination/execution`,
  `/vaccination/execution/sheds/{shed_id}`) but is NOT a separate visible Parks
  module (no sidebar entry, no Parks routes). It shows physical context only where
  relevant to vaccination: park, shed, animal stage, defer status, blocker, owner
  chain, drive status, stock status, proof status, and verification status. Parks
  is NOT a vaccination product route.
- **Action Center** is the top-level work board at `/action-center` (mock board
  density/shape, vaccination rows only): due, overdue, blocked, proof-pending,
  verification-pending, rejected, deferred, owner-missing. It is not a PHC tab.
- **PHC / Vaccination** (`/vaccination`) is the module surface: SOP / Import
  sheet / New drive actions, Target -> Group -> Route -> Execute chain,
  vaccination status matrix, per-cohort detail, drive/shed-event execution, and
  proof/verification/rework states. It is not the Action Center.
- **Control Tower** (`/`) is the top summary shell only: process intact/not
  intact, where, severity, owner, and next action. It must not become a generic
  KPI dashboard, but it should still resemble the mock's alert-summary structure.
- **Protocol Adherence** answers whether vaccination config + SOP are being
  followed: Expected -> Actual -> Gap -> Severity -> Owner -> Next -> Evidence.
- **Workflow drilldown** shows the live chain for a vaccination drive/obligation:
  published config -> obligation -> batch/drive -> SOP task -> proof ->
  verification -> completion.

Do not build generic Parks, generic dashboards, old Locations, old Operations,
or old import/data-quality workflows in this slice. Parks is not a vaccination
product route, module, sidebar entry, or product surface.

## Scope Lock

Build exactly the active user-approved slice. Do not broaden a shared engine into
a visible product surface just because the backend or mock can support future
domains.

Current visible slice:

```text
PHC / Vaccination process integrity
Admin / Data Ops config
Admin / Data Ops SOP Library for vaccination SOP policy
Vaccination execution context scoped by park/shed
```

For backend/frontend handoff details, read:

```text
context/execution/vaccination-process-integrity-backend-handoff.md
context/frontend/vaccination-process-integrity-frontend-handoff.md
```

For `/sops`, the backend SOP engine may stay generic, but the visible admin-web
review surface must remain vaccination-only until a separate all-domain SOP
rollout is explicitly approved.

Rules for `/sops` in this slice:

- Show vaccination SOPs only, such as `vaccination.drive` or `vaccination.*`.
- Do not show non-vaccination SOP cards such as shifting, procurement, health
  diagnosis, HR, counts, breeding, feed, or inventory SOPs in the active review
  surface.
- Domain chips may remain for mock fidelity, but non-vaccination domains must
  appear inactive/zero/disabled and must not look like built product inventory.
- New SOP defaults must be vaccination-specific: vaccination drive/session,
  vaccine batch/cold-chain/proof/verification/repeat-per-goat semantics.
- Empty states must say there are no vaccination SOPs, not fall back to showing
  unrelated SOPs.
- Generic builder/helper code can exist internally for future reuse, but the UI,
  screenshots, handoff notes, and tests must communicate the vaccination slice.

## Active Routes

These are the only current admin-web product routes:

```text
/login
/                          Control Tower      (top-level command)
/action-center             Action Center      (top-level command)
/protocol-adherence        Protocol Adherence (top-level command)
/workflows                 Workflows          (top-level command)
/workflows/{row_id}        Workflow drilldown
/vaccination               PHC Vaccination module surface
/vaccination/execution/sheds/{shed_id}
/config
/sops
/goats/{goat_id}
```

Nested compatibility redirects are not allowed for command-room or authority
screens. Link directly to `/protocol-adherence`, `/workflows/{row_id}`, and
`/config?category=vaccination`.

There is no sanctioned `/parks/vaccination` exception. Vaccination execution is a
vaccination-owned physical context inside `/vaccination`; shed details use
`/vaccination/execution/sheds/{shed_id}`. Do not build generic Parks, and do not
revive `/locations` or old Parks navigation.

### Reopened: Procurement source-entry vertical (2026-06-24)

The procurement/source-entry slice was explicitly reopened as its own vertical
(NOT nested under PHC). It reads/writes its own generated backend contracts
(`/procurement/source-entry/*` in admin-api). Only procurement operational pages
belong under the procurement route tree. Command-room screens stay top-level.
These are current procurement product routes:

```text
/procurement/source-entry                       Source Entry Board (+ New load)
/procurement/source-entry/loads/{load_id}        Load Detail (+ pre-dispatch / arrival gate / accept-intake actions)
```

Forbidden procurement UI routes:

```text
/procurement/source-entry/action-center
/procurement/source-entry/protocol-adherence
/procurement/source-entry/adherence
/procurement/source-entry/control-tower
/procurement/source-entry/workflows
/procurement/source-entry/workflows/{row_id}
```

Procurement can feed the existing top-level command screens only when selected
as a domain/filter/lens:

```text
/                         Control Tower with procurement selected
/action-center?domain=procurement
/protocol-adherence?domain=procurement
/workflows?domain=procurement
/workflows/{row_id}?domain=procurement
```

Implementation rule for that future lens: reuse the existing command screens
and shared scope/date parser. Do not introduce a second procurement command
engine, second route tree, or nested shortcut band. Procurement-specific backend
read models may exist only as adapters feeding the same top-level command-lens
contract. If a UI action is enabled in those screens, it must call a real
backend action or be honestly disabled with a reason.

Boundary rules: procurement data is separate from the default vaccination
command-screen content; rejected/source-only/unresolved goats stay procurement
history and must not appear as PHC vaccination or vaccination execution work; only accepted-intake
goats flow to PHC. Until a backend built from current source is running
(procurement in the permission registry + migration `000083` + seed), these
routes render honest `route_not_registered` / empty states — never fake rows.

Navigation should be (command screens top-level, verticals below):

```text
Control Tower
Action Center
Protocol Adherence
Workflows
PHC
  Vaccination            (module surface including execution context)
Procurement              (reopened vertical)
  Source Entry
Admin / Data Ops
  Config
  SOP Library
```

Parks is NOT a vaccination module or sidebar entry. Vaccination execution context
is rendered inside /vaccination, not as a separate Parks nav item.

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
/tasks
/import-review
/data-quality
/data-quality/review-guide
/legacy-sync
/dashboard/mortality
/herd
```

`/sops` was explicitly reopened on 2026-06-24 as the new Admin / Data Ops SOP
Library / form-builder surface. The engine can be generic, but the current
visible review surface is vaccination-only. This does not reopen old `/tasks`,
old Operations, old generic SOP/task pages, old admin primitives, all-domain SOP
inventory, or old dashboard UI.

Do not rebuild removed routes unless the product scope is explicitly reopened
and the screen is rebuilt from the mock, not from old admin-web code.

## Completion Bar

Before asking for approval:

- `/login`, `/`, `/action-center`, `/protocol-adherence`, `/workflows`,
  `/vaccination`, `/config`, `/sops`, and contextual `/goats/{goat_id}` match the
  mock structure and density.
- PHC/Vaccination, Admin Config, and vaccination execution context are connected by
  real backend status/proof/verification data.
- Control Tower summarizes only broken or at-risk process.
- Config is generic and category/schema-driven.
- Old dashboard/admin routes are not visible in nav and have no active product
  implementation.
- No direct frontend access to BigQuery, Sheets, GCS, Firestore, or databases.
- No horizontal clipping or text overflow on desktop/narrow screenshots.

Do not resume full Feed Direction operational screens until PHC/Vaccination and
the generic Config foundation are reviewed and approved.
