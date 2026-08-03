---
name: nav-composition
description: >-
  Use when adding OR reviewing any navigation, bottom-bar item, or screen on
  mobile (apps/goatos-android) or admin-web — or the backend bootstrap nav
  builder. Enforces job-driven, module-grant-composed, reusable-across-modules
  navigation and blocks hardcoded per-role/per-module nav templates. Invoke
  before touching nav/bootstrap and before pushing. Also owns WHERE a feature
  entry point may live: bottom bar/drawer, never the top-right app bar. Machine
  gates: `make nav-composition-guard`, `make nav-entry-point-placement-guard`.
---

# Role × module nav composition

Canonical rule: `docs/decisions/role-module-nav-composition.md`. Backend owns nav
(golden frontend rule); mobile/admin-web render the composed nav.

## The rule
Nav bar, **bottom-bar icons/labels**, and which **screens** a person sees are
**composed from their job (role × granted modules)** and **reused across modules**.
- A person's job = role × `department_module_grants` (mig 000148). Vaccination
  operator ≠ feed operator ≠ verifier; one person may hold several modules.
- Nav items / screens are **shared** across modules: Calendar is shared by
  Vaccination + Feed + Video-Verification; a proof-Upload or Verifier screen can be
  common to 2–3 modules. Shared items appear once (dedupe by `shared_key`).

## Alerts tabs
Every available feature's bottom bar carries its OWN alerts tab, scoped by feature
AND role, module-prefixed href, titled just "Alerts", bell icon, hosted route,
bottom-bar root. What the feed is FOR, the five wiring requirements, both shipped
incidents, and how to add one to a new module:
`references/alerts-tab.md`. Machine gate: `make module-alerts-tab-guard`.

## Build it as
- A **module → nav-contribution registry**: each module declares `{key, icon,
  labelKey, route, shared_key?}` + owned/reused screens.
- Bootstrap **composes** the visible nav = union of granted modules' contributions,
  deduped by `shared_key`, ordered by priority. 1 module → bottom-bar; ≥2 → drawer.
- Reusable screens keyed by capability (`calendar`, `verify-queue`, `proof-upload`)
  and parameterised by module/category — never copy-pasted per vertical.
- If Android must choose between two renderers for the same route, use a
  backend-owned bootstrap feature flag derived from permission constants. Example:
  `weighing_execute` chooses field execution vs monitor display for `/weighing`.
  Do not derive this from role labels in the app.
- Feature ownership is split by director role: `pc_director` owns Vaccination;
  `growth_director` owns Weighing. Do not use `director_growth` or give
  `pc_director` Weighing affordances.

## BANNED (guard fails on these)
- A fixed nav template array hardcoded per role/module
  (`var xNavigation = []navigationTemplate{ {href:"/vaccination"}, … }`).
- Hardcoding a module/vertical route into a nav-item literal in the nav builder.
- Duplicating a shared screen/nav item per module instead of one registry entry.
- Client deciding nav by `role ==` or a hardcoded module list.
- Client deciding execute-vs-display routing by role label.

## Guard
`make nav-composition-guard` (`tools/agent-hooks/check-nav-composition.mjs`) — fails
on a NEW hardcoded nav template; the 2 existing (`operatorNavigation`,
`leadershipNavigation`) are baselined as nav-generalization debt. Genuinely-fixed
system nav → `nav-composition:ignore: <reason>`.

## Placement: a feature entry point is NEVER top-right
Canonical rule: `docs/decisions/nav-entry-point-placement.md`.

The app bar carries actions **ON the current screen** — refresh/sync, filter,
search-within-this-list, a screen-scoped overflow, a create drill for the very
list shown. It must never carry a **doorway to another feature surface** (alerts,
inbox, videos, profile, settings-as-a-feature, a module switch). Those are nav
destinations: they are composed by the backend registry and render as a
bottom-bar item or a drawer row, in the same place in every module.

Worked example: weighing "My work" shipped a top-right bell while vaccination
shipped the same concept as a `[ Drives | Alerts ]` bottom bar. The fix is not to
move the icon — it is to give the module its **nav registry item**.

Guard: `make nav-entry-point-placement-guard`
(`tools/agent-hooks/check-nav-entry-point-placement.mjs`), diff-scoped. It derives
the forbidden set from `bootstrap_copy.go` (L0 hrefs + `nav.*` labels) and rejects,
inside any `actions = { ... }` slot: navigation to a nav destination, a control
named after a nav destination, or an inert `onClick = {}` placeholder. Read the
guard header for its blind spots. Pre-existing violations:
`tools/agent-hooks/nav-entry-point-baseline.txt` — shrink it, never grow it.
Escape hatch: `nav-placement:ignore: <reason>`.
