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
   mig `000148`; see org-role-model.md). A vaccination operator, a feed operator,
   and a verifier see different nav. One person may hold several modules.
2. **Nav items and screens are shared across modules.** A bottom-bar item /
   screen can be common to 2–3 modules — e.g. **Calendar** is shared by
   Vaccination + Feed Direction + Video Verification; a proof **Upload** or a
   **Verifier** screen can be common across modules under different verticals.
   Shared items appear once (deduped), not duplicated per module.

## How to build it (target)
- A **module → nav contribution registry**: each module declares its nav item(s)
  `{ key, icon, labelKey, route, shared_key? }` and which screens it owns/reuses.
- The bootstrap **composes the visible nav** from the person's granted modules:
  union the modules' nav contributions, **dedupe by `shared_key`** (Calendar,
  Alerts, You, Verifier-queue…), order by a stable priority. One module → bottom-
  bar only; ≥2 → drawer (existing nav-chrome rule).
- **Backend owns it** (the `/app/bootstrap` + `/admin-web/bootstrap` contract);
  mobile/admin-web render the composed nav — they never hardcode module nav.
- Reusable **screens** are keyed by a generic capability (e.g. `calendar`,
  `verify-queue`, `proof-upload`) and parameterised by module/category, not
  copy-pasted per vertical.

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

## Guard
`make nav-composition-guard` (`tools/agent-hooks/check-nav-composition.mjs`) fails
on a NEW hardcoded per-role/per-module nav template. Existing offenders
(`operatorNavigation`, `leadershipNavigation` in `bootstrap_copy.go`) are baselined
under the nav-generalization item with an expiry — they are tracked debt to be
replaced by the registry composition, and the guard blocks any NEW hardcoded nav.
Genuinely-fixed system nav (a global "You"/Settings) may append
`nav-composition:ignore: <reason>` on the line.

## Scope note
Vaccination ships today on the existing (baselined) hardcoded nav because it is the
only module. The registry composition is the foundation to build BEFORE the 2nd
module so a new vertical/module is a registry entry, not a nav rewrite.
