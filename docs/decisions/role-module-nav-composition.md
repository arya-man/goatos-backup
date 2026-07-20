# Role × Module nav composition (no hardcoded per-module nav)

Status: hard rule for Claude, Codex, and every developer. Machine-blocked by
`make nav-composition-guard`. Companion to `context/architecture/org-role-model.md`
(the tier × vertical × park model) and the frontend golden rule in AGENTS.md
(backend owns navigation).

## The rule
Navigation — nav bar, **bottom-bar icons/labels**, and which **screens** a person
sees — is **composed from that person's job (role × granted modules)**, backend-
driven, and **reused across modules**. It is NOT a fixed per-role or per-module
template.

Two facts drive it:
1. **A person's job = role × the modules they are granted** (`department_module_grants`,
   mig `000002_department_module_grants.sql`; see org-role-model.md). A vaccination
   operator, a feed operator, and a verifier see different nav. One person may hold
   several modules. Grants are **department-scoped**, resolved
   `user → workforce_members.department_id → department_module_grants.module_key`
   (`Repository.ListGrantedModuleKeys`), not attached per user.
2. **Nav items and screens are shared across modules.** A bottom-bar item /
   screen can be common to 2–3 modules — e.g. **Calendar** is shared by
   Vaccination + Feed Direction + Video Verification; a proof **Upload** or a
   **Verifier** screen can be common across modules under different verticals.
   Shared items appear once (deduped), not duplicated per module.

## How it is built (shipped)
- A **module registry** (`moduleNavRegistry` in
  `backend/internal/workforce/app/bootstrap_copy.go`) is the single source of truth.
  Each entry is a `moduleDefinition` carrying **two things**:
  1. the module's **drawer identity** — `{ key, labelKey, landingHref, status,
     priority }`, the row a person taps to switch modules; and
  2. its **nav contributions** — the `{ key, labelKey, href, shared_key?, priority }`
     items that make up that module's bottom bar.
  Drawer identity used to be hardcoded client-side (Android `GoatOsShell.kt` held a
  literal Vaccination row plus its own `SOON_MODULES` list). It is backend-owned now,
  so adding a module is a registry entry, not a client change.
- **The bottom bar is MODULE-SCOPED, not a flat union.** `BootstrapResponse.Modules`
  carries one `BootstrapModule` per granted+available module, each with **its own**
  `NavItems`; selecting a module in the drawer swaps the bar. `VisibleNavigation`
  carries the default (lowest-priority available) module's bar — see
  `activeModuleKey()`/`modulesFor()`.
  **Why not a union:** a literal union of just Vaccination (Drives, Calendar, Alerts)
  and Counts (Counts, Birth/Death, Shifting) is already 6 tabs, and every further
  module adds more. That is unusable on a field phone. Module-scoping keeps each bar
  at 3–5 items no matter how many modules a person holds; the drawer, not the bar,
  absorbs the growth.
- **`shared_key` dedupe still applies** wherever several modules are composed into
  ONE bar. That is not dead code: `composeNavigationFromModules()` takes a module
  list, and the leadership bar is exactly that case (Overview + the cross-module
  Calendar/Alerts). A module contributing `calendar`/`alerts` under the same
  `shared_key` appears once, at the first contributing module's priority.
- **"Soon" modules are backend-declared.** A registry entry with
  `status: "soon"` (`feed_direction`, `breeding`) renders as a disabled drawer row
  for every principal via `soonModuleKeys` — it advertises the roadmap and confers
  no access. Soon modules are skipped by `activeModuleKey()` and do not count in
  `countAvailableModules()`, so they never earn the drawer or become a default bar.
- **Nav chrome follows granted available modules, not a role flag.** `navChromeFor`
  counts registry-known, `available`, granted modules: 1 → bottom bar only, ≥2 →
  expanded drawer. It is no longer derived from a leadership boolean.
- **Backend owns it** (the `/app/bootstrap` + `/admin-web/bootstrap` contract);
  mobile/admin-web render the composed nav — they never hardcode module nav.
- Reusable **screens** are keyed by a generic capability (e.g. `calendar`,
  `verify-queue`, `proof-upload`) and parameterised by module/category, not
  copy-pasted per vertical.
- A granted `module_key` the registry does not know contributes nothing. That is
  deliberate: `department_module_grants.module_key` is intentionally not an enum, so
  "add a module" stays a registry entry plus a grant row, never a schema migration.

## Per-item permission gating (one module, different pages per job)
A module is not all-or-nothing. `moduleNavContribution` carries a
**`requiredPermission`** field; `""` means the item is ungated and visible to anyone
holding the module. This is what lets ONE registry entry expose **different pages to
different jobs without a per-role nav template** — the thing this ADR bans. The
registry declares the permission; the role→permission table decides who holds it.
Nobody adds a `case role == "operator"` branch to the nav builder.

- **Filtering** — `permittedContributions(def, grants)` drops items whose
  `requiredPermission` no grant role satisfies (`grantsHavePermission` mirrors how
  `permissions.routePermissions` is evaluated, so a nav item and its route agree on
  who may reach it).
- **A fully-gated-away module is OMITTED, not shown empty.** `modulesFor`,
  `countAvailableModules`, and `activeModuleKey` all skip a module with zero permitted
  items, so it never reaches the drawer, never counts toward the ≥2 drawer threshold,
  and never becomes a default bar. An empty module row would be a dead end that
  advertises access the principal does not have.
- **Landing-href fallback.** A module's declared `landingHref` can itself be gated
  away. `modulesFor` checks `navItemsContainHref(items, href)` and falls back to
  `items[0].Href` — the first page this principal may actually open. Landing someone
  on a route that 403s on arrival would be a self-inflicted dead end.
- **Hiding the item is NOT the access control.** The matching route in
  `permissions.routePermissions` requires the same permission, so an unlisted page is
  *unreachable*, not merely invisible. Nav composition and route authorization read
  the same table; a hidden page returns 403 if requested directly.

**Leadership is composed by permission, not by department.** `candidateModuleKeys()`
gives a leadership principal **every** registry module (then permission-filters it);
everyone else stays limited to their department's granted modules. The reason:
module grants resolve `user → workforce_members.department_id →
department_module_grants.module_key`, and leadership roles are **org-level — a CEO or
Director is not a member of a department**, so department-scoping them would resolve
to zero grants and hide every module. Their access is decided by permission alone.
This is a widening of *candidates*, not of access: a leadership principal still
receives only the modules and items whose permissions they hold.

**Worked example — the Counts matrix (maintainer decision 2026-07-18).** Counts
contributes three items under two permissions: the census page `/counts` requires
`counts.read`, and the two capture pages (`/counts/birth-death`, `/counts/shifting`)
require `counts.write`. `counts.read` is deliberately split off `goat.read` and
narrower than it — `goat.read` is held by nearly every role, so reusing it would have
made the tenant-wide census effectively public. The split encodes the authority that
matters: **field capture and tenant-wide census visibility are different
authorities.** One registry entry then yields:

| role | census `/counts` | `/counts/birth-death` | `/counts/shifting` | module in drawer |
|---|---|---|---|---|
| `operator` | — | yes | yes | yes (2-item bar) |
| `park_head` | — | yes | yes | yes (2-item bar) |
| `admin` | yes | yes | yes | yes (3-item bar) |
| `ceo_internal` | yes | yes | yes | yes (3-item bar) |
| `pc_director` | — | — | — | **NO — all items gated, module omitted** |
| `verifier` | — | — | — | **NO — all items gated, module omitted** |

Operator and Park Head land on `/counts/birth-death` via the landing-href fallback,
since the declared `/counts` landing is gated away from them. `pc_director` and
`verifier` hold neither counts permission, so the module disappears from their drawer
entirely. Pinned by `TestCountsModuleRoleMatrix`
(`backend/internal/workforce/app/service_test.go`), which asserts the item sets, the
two omissions, and that every module's landing href is among its permitted items.

## BANNED (the anti-pattern this guard blocks)
- A **fixed nav template array hardcoded per role or per module** — e.g.
  `var operatorNavigation = []navigationTemplate{ {href:"/vaccination"}, … }` — is
  banned. Nav must be composed from module grants, not a literal list that names a
  specific vertical/module.
- Hardcoding a **module/vertical route** (`/vaccination`, `/feed`, `/breeding`, …)
  into a nav item literal in the nav builder.
- **Duplicating** a shared screen/nav item per module instead of one registry
  entry reused via `shared_key`.
- The client (mobile/admin-web) deciding nav by `role ==` or a hardcoded module
  list instead of rendering the backend-composed nav.
- **Branching the nav builder on a role** to vary which pages a module exposes
  (`if role == "operator" { … }`). Declare `requiredPermission` on the item instead —
  the role→permission table is the only place roles are enumerated.
- **Hiding a nav item without gating its route.** If an item declares a
  `requiredPermission`, the route behind it must require the same permission in
  `permissions.routePermissions`. A page hidden in nav but reachable by URL is not
  access control.

## Guard
`make nav-composition-guard` (`tools/agent-hooks/check-nav-composition.mjs`) fails
on a NEW hardcoded per-role/per-module nav template. The previously-baselined
offenders (`operatorNavigation`, `leadershipNavigation` in `bootstrap_copy.go`) are
gone — replaced by the registry composition this ADR called for.
`moduleNavRegistry` itself carries `nav-composition:ignore:` markers because it IS
the registry, not a per-role template; genuinely-fixed system nav (a global
"You"/Settings) may append `nav-composition:ignore: <reason>` on the line for the
same reason. Do not use the marker to smuggle a per-role nav literal back in.

## Scope note
The registry composition is **built**, and **Counts is the 2nd module** — the case this
ADR was written to make cheap. Counts contributes three items (Counts `/counts`,
Birth/Death `/counts/birth-death`, Shifting `/counts/shifting`) and landed as a
registry entry plus a grant row, with no nav rewrite on either backend or client.
Its per-role page split (see the matrix above) then landed as two
`requiredPermission` values on those same three items — again no per-role template,
no nav rewrite.

Granted modules come from `department_module_grants`; the previous
`grantedModules = []string{"vaccination"}` hardcode and the leadership-boolean nav
chrome are both gone. Seed coupling for that table is
`backend/cmd/seed-roster-real` (department defaults for the source seed) and
`backend/cmd/seed-dev-grant -modules` (dev identities) — see
`docs/runbooks/android-dev-device.md`.
