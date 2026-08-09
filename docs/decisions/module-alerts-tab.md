# Every feature's bottom bar carries its own Alerts tab

**Status:** ratified navigation decision, coordination source superseded by the
2026-08-10 operational task-kernel non-deviation ADR.
**Machine guard:** `make module-alerts-tab-guard`
(`tools/agent-hooks/check-module-alerts-tab.mjs`), required by local CI.

## What an Alerts feed is for

It answers one question for the person holding the phone: **what does this
feature need from me that I have not done yet?** The module-scoped bottom-bar
lens and route remain valid navigation. Its work truth comes from the shared
task/contact kernel, not `notification_requests` and not a module-private task
feed. Delivery requests are transport evidence; they cannot define whether work
still exists, who owns it, or what action is next.

During migration, a legacy module feed may read already-routed notification rows
only as a compatibility source. Shadow/compare it against shared task/contact
state, suppress the legacy producer before shared contacts activate, and retire
the feed after replay and zero-use proof. No new module-private feed is allowed.

Routing follows the next-action owner, which is why the same feed shows different
rows to different people: work assigned goes DOWN to the operator, a submission for
verification goes UP, a rework or reopen goes DOWN again, a closure goes UP.

## The rule

> A person standing inside a feature can see **that feature's** alerts from **that
> feature's** bottom bar. Alerts are scoped by **feature AND role**.

Concretely, for every module the backend serves as `available` with its own bottom bar:

1. The module contributes an **Alerts** nav item.
2. The href is that module's own **lens route** over shared task/contact truth. A
   module never borrows another module's lens, but it also never owns a separate
   backend work authority.
3. The tab is titled just **"Alerts"** in every locale (`labelKey: "nav.alerts"`). The
   href carries the scoping, not the label. Per-feature label keys
   (`nav.alerts.vaccination`, `nav.alerts.weighing`, …) were deleted and must not return —
   the reader is already standing inside the module.
4. Role scoping is the ordinary nav mechanism: `requiredPermission` on the contribution,
   plus server-side filtering of the feed itself. A tab that is visible must not 403.
5. On Android the destination must be **hosted** (a `composable(...)` in `AppNavHost`),
   mapped to the **bell** icon in `MeshaIcons.forNavKey`, and listed in
   **`supportedRootDestinations`** — it is a bottom-bar root.

## Why this is a written rule with a guard

It broke twice, in opposite directions, and neither break was visible in a review diff.

**Break 1 — borrowed feed.** Weighing's bar carried `/alerts`, the *vaccination*
process-integrity feed. Its upstream needs `ObligationRead` + `VaccinationRead` and its
label read "Vaccination alerts" in all four languages, so a weighing operator got a
permanently-empty cross-module tab that 403'd. The fix at the time was to delete the tab,
which left weighing with **no alerts at all** — a regression that read like a cleanup.

**Break 2 — half-wired feed.** When weighing's own feed was built
(`/app/weighing/alerts`, module-scoped, gated on weighing capabilities), the nav key
`weighing_alerts` was added and the composable registered — but the route was **not** added
to `supportedRootDestinations`. A deep link or notification naming it is treated as
unhosted and lands the reader on their home screen with the "unavailable" notice. The
backend, the feed, the grants and the tab were all correct; the destination table was not.

A third failure mode is only reachable in a mixed stack, and it is worth naming because it
wastes debugging time: an APK built from a commit that does not know a nav key renders the
**generic module glyph** for it and no-ops on tap, because neither the icon mapping nor the
route exists in that build. That is not a product bug — it is an APK/API mismatch. Check
which commit each side is running before filing it.

## What the guard checks

For every `moduleStatusAvailable` module with nav contributions in
`backend/internal/workforce/app/bootstrap_copy.go`:

| Check | Failure it prevents |
|---|---|
| an alerts contribution exists | a feature ships with no alerts tab |
| `labelKey == "nav.alerts"` | per-feature labels creeping back in |
| nav key maps to `Bell` | the generic module glyph rendering as "Alerts" |
| href is hosted in `AppNavHost` | tapping the tab does nothing |
| href is in `supportedRootDestinations` | deep links landing on the home screen |
| href starts with the module's own prefix | a lens opening in the wrong feature context; the backend source remains shared task/contact truth |

The `verification` module is exempt from the registry parse: its bar is composed per
reviewed feature at runtime (`verificationModuleForFeature`), and its alerts items are
covered by that path's own tests.

## Pending shared-task lenses (the visible debt)

A module with no activated lens over shared task/contact truth is listed in
`PENDING_ALERTS_FEED` inside the guard, with a reason. A legacy
`notification_requests` compatibility feed does not close that gap. It is a
tracked gap, not an opt-out: adding a new module to that list requires editing
the guard in the same commit, so it shows up in review.

- **counts** — `GET /app/counts/alerts` exists as a legacy, per-recipient
  `notification_requests` compatibility feed. Counts has no activated
  module-scoped lens over shared task/contact truth yet, so the existing route
  must not be mistaken for canonical work state or preserved as the final
  implementation.
- **feed_direction** — `GET /app/feed/alerts` exists as the equivalent legacy
  compatibility feed. Feed has no activated module-scoped shared-task lens/nav
  yet; the direct notification-backed source is suppressed and retired during
  cutover rather than promoted to work truth.

Removing an entry from that list is the goal. The guard fails if a module is waived **and**
has an alerts tab, so stale waivers cannot hide the next regression.

## Route naming

Every lens is addressed by its feature context: `/vaccination/alerts`,
`/weighing/alerts`. The verification module scopes by query category instead
(`/verify/alerts?category=…`) because one verifier reviews several features from one
bar; it is exempt from the prefix check.

There is **no generic `/alerts` UI route**. Shared backend task/contact truth does
not require one generic navigation address; module-scoped lens routes preserve
the person's feature context.

The alias was checked before removal rather than assumed: nothing ever produced
`/alerts` as a notification tap target. The bridge emits only `/vaccination`,
`/weighing`, `/counts`, `/feed`, so no delivered alert names it and nothing was
stranded. Profile's notifications action now opens `/vaccination/alerts` by name.

## Related

- `docs/decisions/role-module-nav-composition.md` — drawer vs bottom bar, and where "You" lives.
- `docs/decisions/operational-task-kernel-non-deviation.md` — shared work/contact
  authority and legacy-feed cutover.
- `AGENTS.md` → user-facing copy firewall — why the tab is never titled with internal wording.
