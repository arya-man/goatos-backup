# Alerts: what the tab is for, and the five things that must line up

Canonical rule: `docs/decisions/module-alerts-tab.md`.
Machine gate: `make module-alerts-tab-guard` (required by `make ci-local`).

## What an Alerts tab actually is

An alerts feed answers ONE question for the person holding the phone:

> **What does this feature need from me, that I have not done yet?**

It is not a notification log, not a news feed, and not an audit trail. It is the
durable, in-app copy of the work-state transitions that were routed to **this
person** for **this feature** — so that a push notification which was swiped away,
delivered while the phone was off, or never granted permission is not the only
place that fact exists.

That is why the rule is per FEATURE and per ROLE:

- **Per feature** — the alerts belong to the module whose work they describe. A
  weighing operator opening Weighing sees weighing alerts. The same person opening
  Vaccination sees vaccination alerts. Neither tab shows the other's.
- **Per role** — routing is by who owns the *next action*. Work assigned goes DOWN
  to the operator; a shed submitted for verification goes UP; a proof sent back for
  rework or a shed reopened goes DOWN again; work closed goes UP. A leadership
  principal and the operator standing in the same shed see different rows, from the
  same feed, because they are owed different things.

Nothing new is produced for the feed. Every row already exists as a durable
`notification_requests` row written by that module's notification consumers; the
feed is a READ of what was already routed. If a transition is worth an alert, it is
already worth an event — add the event first, then the feed shows it.

## The five things that must line up

A module's alerts tab is only real when all five hold. The guard checks them
together because each one alone looks fine in a review diff:

| # | Requirement | Symptom when missing |
|---|---|---|
| 1 | The module contributes an alerts nav item | the feature simply has no alerts |
| 2 | The href is the module's OWN feed, module-prefixed | the tab shows another feature's rows, or 403s |
| 3 | `labelKey` is the generic `nav.alerts` | "Vaccination alerts" inside the vaccination module — naming what the reader is already standing in |
| 4 | The nav key maps to `Bell` in `MeshaIcons.forNavKey` | renders the generic module glyph, reads as a broken tab |
| 5 | The route is hosted AND in `supportedRootDestinations` | tapping works, but every notification/deep link to it lands on the home screen with the "unavailable" notice |

## How it has broken (real incidents, both shipped)

**Borrowed feed → tab deleted → feature left with nothing.** Weighing's bar carried
`/alerts`, which was vaccination's feed. Its upstream needs `ObligationRead` +
`VaccinationRead`, so a weighing operator got a permanently-empty tab that 403'd.
The fix was to delete the tab — which read as a cleanup, and left weighing with no
alerts at all until its own feed was built. The root cause was the generic ROUTE
NAME: `/alerts` looked shared, so it got copied. Both feeds are now module-prefixed
(`/vaccination/alerts`, `/weighing/alerts`), and requirement 2 is enforced.

**Half-wired feed.** When weighing's feed was built, the nav key was added and the
composable registered, but the route was never added to `supportedRootDestinations`.
Backend, grants, feed and tab were all correct; the destination table was not, so
deep links landed on the home screen. Requirement 5 exists because of this.

**Not a bug: APK/API mismatch.** An APK built from a commit that does not know a nav
key renders the generic glyph for it and no-ops on tap, because neither the icon
mapping nor the route exists in that build. Before filing, check which commit each
side is running.

## Adding a feed to a new module

1. Confirm the module's transitions already write `notification_requests` rows with
   a routed recipient. If not, that is the first piece of work — the feed reads, it
   does not invent.
2. Add the read: a module-scoped endpoint gated on that module's capabilities only,
   returning what was routed to the caller.
3. Add the nav contribution: `{key: "<module>_alerts", labelKey: "nav.alerts",
   href: "/<module>/alerts", shared_key: ""}`. No `shared_key` — it must never
   dedupe against another module's alerts item.
4. Android: map the nav key to `Bell`, register the composable, and add the route to
   `supportedRootDestinations`.
5. Remove the module from `PENDING_ALERTS_FEED` in the guard.
6. Run `make module-alerts-tab-guard`.

## Tracked gaps

- **counts** — no counts notification feed exists on any branch. Waived in the
  guard, logged as ALERTS-001 in the consolidated bug ledger.
- **vaccination route alias** — `/alerts` is still hosted as a legacy tap target for
  alerts already delivered to phones; the bar points at `/vaccination/alerts`.
  Logged as ALERTS-002.
