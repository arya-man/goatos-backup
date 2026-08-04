# Every feature's bottom bar carries its own Alerts tab

**Status:** ratified — maintainer decision, 2026-08-03.
**Machine guard:** `make module-alerts-tab-guard`
(`tools/agent-hooks/check-module-alerts-tab.mjs`), required by local CI.

## What an Alerts feed is for

It answers one question for the person holding the phone: **what does this feature
need from me that I have not done yet?** It is not a notification log and not an
audit trail — it is the durable in-app copy of the work-state transitions routed to
THIS person for THIS feature, so a push that was swiped away, delivered to a phone
that was off, or blocked by a permission prompt is not the only place that fact
exists.

Nothing new is produced for a feed. Every row already exists as a routed
`notification_requests` row written by that module's notification consumers; the
feed READS what was already routed. If a transition deserves an alert, it first
deserves an event.

Routing follows the next-action owner, which is why the same feed shows different
rows to different people: work assigned goes DOWN to the operator, a submission for
verification goes UP, a rework or reopen goes DOWN again, a closure goes UP.

## The rule

> A person standing inside a feature can see **that feature's** alerts from **that
> feature's** bottom bar. Alerts are scoped by **feature AND role**.

Concretely, for every module the backend serves as `available` with its own bottom bar:

1. The module contributes an **Alerts** nav item.
2. The href is that module's **own** feed. A module never borrows another module's feed.
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
| href starts with the module's own prefix | a generic address that reads as a shared feed and gets copied into another feature's bar |

The `verification` module is exempt from the registry parse: its bar is composed per
reviewed feature at runtime (`verificationModuleForFeature`), and its alerts items are
covered by that path's own tests.

## Pending feeds (the visible debt)

A module with genuinely no feed yet is listed in `PENDING_ALERTS_FEED` inside the guard,
with a reason. It is a tracked gap, not an opt-out: adding a new module to that list
requires editing the guard in the same commit, so it shows up in review.

- **counts** — no counts notification feed exists on any branch. The only counts-shaped
  alerts today are the *verifier's* `shifting_move` queue, which belongs to the
  verification module's bar, not to Counts' own.

Removing an entry from that list is the goal. The guard fails if a module is waived **and**
has an alerts tab, so stale waivers cannot hide the next regression.

## Route naming

Every feed is addressed by the feature that owns it: `/vaccination/alerts`,
`/weighing/alerts`. The verification module scopes by query category instead
(`/verify/alerts?category=…`) because one verifier reviews several features from one
bar; it is exempt from the prefix check.

There is **no generic `/alerts` route**. It was deleted, not kept as an alias: a
generic address is what made the vaccination feed look shared, which is how it ended
up in weighing's bar. Keeping it "just for old links" would have preserved exactly the
thing the rule bans.

The alias was checked before removal rather than assumed: nothing ever produced
`/alerts` as a notification tap target. The bridge emits only `/vaccination`,
`/weighing`, `/counts`, `/feed`, so no delivered alert names it and nothing was
stranded. Profile's notifications action now opens `/vaccination/alerts` by name.

## Related

- `docs/decisions/role-module-nav-composition.md` — drawer vs bottom bar, and where "You" lives.
- `AGENTS.md` → user-facing copy firewall — why the tab is never titled with internal wording.
