# Alerts: what the tab is for, and the five things that must line up

Canonical rule: `docs/decisions/module-alerts-tab.md`.
Machine gate: `make module-alerts-tab-guard` (required by `make ci-local`).

## What an Alerts tab actually is

An alerts feed answers ONE question for the person holding the phone:

> **What does this feature need from me, that I have not done yet?**

It is not a notification log, not a news feed, and not an audit trail. It is a
module-scoped navigation lens over the shared operational task/contact kernel:
durable work owned by **this person** for **this feature**, including the next
action and acknowledgement state. `notification_requests` rows are delivery
evidence only; they never determine whether work exists, who owns it, or what
must happen next.

That is why the rule is per FEATURE and per ROLE:

- **Per feature** — the alerts belong to the module whose work they describe. A
  weighing operator opening Weighing sees weighing alerts. The same person opening
  Vaccination sees vaccination alerts. Neither tab shows the other's.
- **Per role** — routing is by who owns the *next action*. Work assigned goes DOWN
  to the operator; a shed submitted for verification goes UP; a proof sent back for
  rework or a shed reopened goes DOWN again; work closed goes UP. A leadership
  principal and the operator standing in the same shed see different rows, from the
  same feed, because they are owed different things.

The domain still publishes its source event transactionally. An outward-only
materializer maps that event into shared owner/clock/hierarchy/contact truth,
and the module route reads the shared lens. Never create or preserve a private
task list, scheduler, escalation ladder, verifier queue, or notification-backed
work feed to power the tab.

During migration only, an existing module route may read its legacy
`notification_requests` compatibility rows while it is shadow-compared against
the shared lens. Suppress the direct producer before shared contacts activate;
retire the compatibility source after replay, parity, and zero-use proof.

## The five things that must line up

A module's alerts tab is only real when all five hold. The guard checks them
together because each one alone looks fine in a review diff:

| # | Requirement | Symptom when missing |
|---|---|---|
| 1 | The module contributes an alerts nav item | the feature simply has no alerts |
| 2 | The href is the module's own module-prefixed **lens route over shared task/contact truth** | the tab opens another feature's work, preserves a private feed, or 403s |
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

## Adding an Alerts lens to a module

1. Confirm the domain mutation writes its canonical source fact, audit, and
   outbox event in one transaction. Do not add an Alerts-only producer.
2. Onboard the source through one of the two governed seams: normally use the
   transaction-aware shared task port inside the owning domain transaction; for
   a recorded strict-isolation boundary such as Weighing, publish the source
   event transactionally and materialize it through an outward-only adapter with
   a receipt/source-version fence. Both shapes require a real owner, pinned
   clock, hierarchy/source identity, contact policy, replay safety, and an
   explicit materialization-failure owner. Only a recorded strict-isolation
   module is forbidden from reading shared task state or waiting on the shared
   materializer; that restriction must not be generalized into a second kernel.
3. Add the read as a module-scoped lens over shared task/contact truth, gated on
   that module's capabilities and filtered to the caller. Do not query
   `notification_requests` as canonical work state and do not build a
   module-private task/escalation/verifier store.
4. Add the nav contribution: `{key: "<module>_alerts", labelKey: "nav.alerts",
   href: "/<module>/alerts", shared_key: ""}`. No `shared_key` — it must never
   dedupe against another module's alerts item.
5. Android: map the nav key to `Bell`, register the composable, and add the route to
   `supportedRootDestinations`.
6. If a legacy direct feed exists, shadow/compare it, repair retained gaps,
   suppress its producer before shared contacts activate, and prove zero use
   before retirement. Prevent duplicate task/contact delivery across cutover.
7. Remove the module from `PENDING_ALERTS_FEED` in the guard only after the
   shared lens/nav is active.
8. Run `make module-alerts-tab-guard` and the operational-task-kernel
   non-deviation guard once F0 lands it.

## Tracked gaps

- **counts** — `GET /app/counts/alerts` exists as a legacy, per-recipient
  `notification_requests` compatibility feed. It remains waived because Counts
  has no activated module-scoped lens over shared task/contact truth; do not
  preserve the legacy source as the final implementation.
- **feed_direction** — `GET /app/feed/alerts` is the equivalent legacy
  compatibility feed. Feed has no activated shared-task lens/nav yet; suppress
  and retire the direct source during cutover.
- **none for vaccination** — `/vaccination/alerts` is the only vaccination feed route.
  The old generic `/alerts` was DELETED, not aliased: no notification ever named it
  (the bridge emits only `/vaccination`, `/weighing`, `/counts`, `/feed`), and keeping
  a generic address alive is what made the feed look shared in the first place.
  ALERTS-002 is closed.
