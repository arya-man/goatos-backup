# Feature Entry Points Are Never In The Top-Right App Bar

Status: accepted.

## Decision

**A feature entry point belongs in the bottom bar** — or in the module drawer,
per the existing "sidebar/drawer only for principals with 2+ modules" rule. A
top-right app-bar icon is **not** an acceptable place to introduce a feature
surface, in any module, on mobile or on web.

The point is consistency: a user must find the same thing in the same place in
every module. Where a thing lives is not a per-screen styling choice.

This rule governs **where an entry point may live**. It sits alongside, and does
not modify:

- [android-navigation-stack.md](./android-navigation-stack.md) — which
  destinations own global chrome (L0 only) and how drills are hosted.
- [role-module-nav-composition.md](./role-module-nav-composition.md) — that nav
  is **backend-driven**: the registry in
  `backend/internal/workforce/app/bootstrap_copy.go` composes visible navigation
  and clients render it. Clients must not hardcode visible nav.

## The distinction

**Legitimate in the app bar — an action ON the current screen.** It changes,
reloads, or narrows what the user is already looking at, and leaves them there.

- refresh / sync
- filter, sort, date scope
- search-within-this-list
- an overflow menu of screen-scoped actions
- a create/record drill for the very list on screen (Counts "+" → add a birth to
  this list)

**Not legitimate — a doorway to a different feature surface.** It takes the user
somewhere else, and that somewhere else is a *destination*.

- alerts / notifications
- inbox, videos, proof review
- profile / "You"
- settings-as-a-feature
- a module switch

The structural test: **is the target a navigation destination?** The backend nav
registry is the single source of truth for that answer. Every L0 `href` and every
`nav.*` label is declared in `bootstrap_copy.go`. If a control leads to one of
those, it is nav chrome, and it renders where nav chrome renders.

## The worked example (the incident)

On `bf4f875be`, the Android weighing operator screen ("My work",
`apps/goatos-android/feature/feature-weighing/.../WeighingScreen.kt`) rendered a
**Bell in the top-right of the app bar**, next to the sync icon, with a no-op
`onClick`, while its bottom bar had a single item.

The vaccination module ships the *same concept* correctly: a bottom bar of
`[ Drives | Alerts ]`, with the bell on the **Alerts tab** and nothing top-right.
Vaccination has `{key: "alerts", href: "/alerts"}` in the registry; weighing did
not, so the entry point got hand-placed into the nearest available slot.

Same concept, two different places, in one app. That inconsistency is the defect
— not the bell itself.

The fix is not "move the icon". The fix is to give the module its **nav item**,
so the entry point is composed by the backend and rendered as a tab like every
other destination.

## Enforcement

`make nav-entry-point-placement-guard`
(`tools/agent-hooks/check-nav-entry-point-placement.mjs`), diff-scoped against
`origin/main`, self-tested with adversarial fixtures, registered in
`tools/ci/guardrail-manifest.json` and wired into `make guardrails` and
`tools/ci/run-local-ci.sh`.

Inside every Compose `actions = { ... }` slot it rejects:

| rule | what it catches |
| --- | --- |
| `nav-route-in-appbar` | the action routes — `navController.navigate(`, a `Routes.X` whose value is a registry L0 href, or a literal registry href |
| `nav-named-appbar-entry` | the control is *named* after a nav destination (its `contentDescription` resolves to a registry `nav.*` label) |
| `inert-appbar-entry` | the control has a no-op `onClick` — it does nothing to this screen, so it is a parked entry point |

The forbidden set is **derived from the registry**, not from a token list, so a
new nav destination ("Reports") is covered the day it is added without editing
the guard.

Blind spots are enumerated in the guard's own header comment — read them before
trusting a green run. In short: textual Kotlin scan, `actions = {}` slots only,
no type resolution, English labels only, Android only, diff-scoped.

Pre-existing violations are pinned in
`tools/agent-hooks/nav-entry-point-baseline.txt` so the rule is enforced going
forward without a mass refactor. **Shrink that file, never grow it.** A new entry
needs the maintainer's sign-off.

Escape hatch for a genuine screen-scoped case the heuristics misread: append
`nav-placement:ignore: <reason>` on the line.

## Known outstanding placement debt

- `apps/goatos-android/.../weighing/WeighingScreen.kt` — the top-right bell above
  (baselined; a separate change moves weighing alerts onto nav chrome).
- `apps/admin-web/components/mesha-shell.tsx` — the global shell top bar renders
  a **disabled** notifications bell with a `disabled_reason`. It is inert
  placeholder chrome, not a live entry point, and the web guard does not exist
  yet. When web alerts ship, they ship as a nav destination, not as that bell.
