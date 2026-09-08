# Who hears a leadership push is config, per designation

**Maintainer decision, 2026-09-08.** Status: accepted, implemented.

## The ask

"Make notifications configurable. What type of notifications we have to know, like
yesterday's reading. It should be configurable, not for a single person, but for the
designation."

## What changed

Every upward-routing push in Goat OS used to name its audience as a literal position
code inside the notifier that sent it: the daily low-stock alert went to the CEO, the
Feed Director and the Procurement Director because three constants said so; the
overdue-load alert to the CEO alone; every proof pending / approved / rework copy to
the park head, the module's director and the CEO; every weighing lifecycle notice to
the Growth Director and the CEO; the 20:30 vaccination checkpoint to the PC Director
and the CEO. Correct on the day each was written, and unchangeable afterwards without
a release.

Now:

1. **A catalog names every configurable alert** and its default audience
   (`backend/internal/notificationaudience/domain/catalog.go`). The default of every
   alert is byte-for-byte the audience the notifier resolved by hand before, listed in
   the order it resolved it, so deploying this changed nobody's phone
   (`TestDefaultsReproduceThePreCatalogAudiences`).
2. **One resolver answers "who hears this alert"** (`notificationaudience/app.Resolver`,
   exposed to the bridge as `notificationbridge.AudienceResolver`): the tenant's stored
   override when an admin has customised the alert, the catalog default otherwise. Each
   designation is handed to the workforce resolver the notifiers already used
   (`ResolvePositionRecipients`: active seats, active role grants, reachable devices),
   at tenant scope for a director desk and at the alert's park for a park desk, and
   the result is deduped by device.
3. **The audience is stored per designation** in `notification_alert_audiences`
   (migration 000281): one row per (tenant, alert) holding the designation codes.
   Absence is the default. An empty array is a decision ("nobody") and is kept distinct
   from "not customised".
4. **An admin edits it on People / HRMS -> Notifications**: a matrix of alerts (rows,
   grouped by module) by designations (columns, from `designation_catalog`), with
   per-row Save and Use default. Reading is `OperatorsRead`, the same as the directory
   it sits beside; writing is `OperatorsManageCapability`, the same authority as the
   per-person access editor, because deciding what every holder of a job title is told
   is deciding what they may do. Both halves are gated: the `edit_notifications` page
   control and the PUT route.

## Why designation, not person

An alert is addressed to a desk. The Feed Director hears that feed is low because the
Feed Director runs the chain, not because of who currently holds the title; when the
title changes hands the alert must follow it without anyone editing a list. Per-person
lists are exactly the thing that rots: the 2026-08-24 access rewrite found four people
wearing stacked titles and a department named after an individual, all workarounds
for lists that could not say "this desk". The matrix is keyed on
`designation_catalog`, the same job titles the access editor pre-fills from, and the
device resolution stays where it was.

## Addressed alerts (maintainer ask, same day: "CXO should have this task notification,
yes or no"; "verifier, which video")

Some pushes name their recipient: a leadership task is raised FOR one CXO, a proof video
waits for the verifier ON DUTY for that park. These are rows too, with a different rule
(`Resolver.Addressed`): the addressed person is KEPT while their own job title is ticked, and
every other ticked title receives a copy. Unticking CEO / CXO on "Task raised for a CXO"
silences it for every CXO; ticking Feed Director on "Feed video waiting for the verifier"
sends the Feed Director a copy of what the verifier is asked to review. The verifier is a
TENANT desk (one verifier across every park), so its designation resolves at tenant scope.

## What is deliberately NOT configurable

- **Operator work pushes.** The operator whose proof bounced, the packer whose bag was
  reopened, the operator whose plan was published, the park head whose pen visit is due:
  these name the individual who has to act and stay on `ResolveMemberRecipients`. The
  catalog blurbs say so ("the operator is always told; this is the leadership copy").
- **The Slack channel posts** (feed proof times). A channel is not a desk.

## The alert catalog

| Module | Alert key | Default audience |
| --- | --- | --- |
| Vaccination | `vaccination.due_today_leadership` | pc_director, ceo_internal |
| Vaccination | `vaccination.work_missed` | park_head, pc_director |
| Vaccination | `vaccination.drive_ready` | park_head, pc_director, ceo_internal |
| Vaccination | `vaccination.drive_closed` | ceo_internal |
| Vaccination / Weighing / Feed / Preventive Care / Health / Herd Operations | `<module>.proof_pending`, `<module>.proof_approved`, `<module>.proof_rework` | park_head, the module's director, ceo_internal |
| Weighing | `weighing.plan_published`, `weighing.submitted`, `weighing.reopened`, `weighing.verdict_approved`, `weighing.pen_closed`, `weighing.task_closed`, `weighing.work_cadence` | growth_director, ceo_internal |
| Weighing | `weighing.verdict_rework` | growth_director |
| Feed | `feed.low_stock` | ceo_internal, feed_director, procurement_director |
| Feed | `feed.sale_reduce` | feed_director |
| every verification module | `<module>.proof_review` (ADDRESSED: the verifier on duty) | verifier |
| Leadership Tasks | `leadership.task_raised` (ADDRESSED: the CXO the task names) | ceo_internal |
| Leadership Tasks | `leadership.task_done` (ADDRESSED: the director who raised it) | every director designation |
| Procurement | `procurement.load_overdue` | ceo_internal |

Park desks are `park_head`, `verifier`, `operator`, `procurement_manager`. A park desk
ticked onto an alert that carries no park (the daily low-stock run, the weighing
lifecycle notices today) resolves to nobody rather than guessing a park; the matrix
blurb does not hide this, and the resolver logs `notification_audience_no_recipients`
naming the alert.

## Two wiring rules, both pinned

1. **Every constructor builds a defaults-only resolver** from the `RecipientResolver`
   it already takes. That is what keeps every roster-fake unit test in
   `notificationbridge` valid, and it means a consumer never resolves to nobody for
   want of wiring.
2. **Every production construction site chains `.WithAudience(NewStoredAudience(...))`**:
   the API's in-process bus (`bootstrap/api.go`), the durable buses
   (`kernelstages/bus.go`, `cmd/domain-event-consumer`, `cmd/outbox-relay`), and the
   kernel stages (`feed_low_stock.go`, `load_age_alert.go`, `reminder_cadence.go`). A
   site that forgets serves defaults forever and the matrix silently does nothing for
   it. `TestEveryUpwardNotifierIsWiredToTheStoredAudience` walks those files and reads
   each constructor's whole builder chain; `TestUpwardPushesAskTheResolverNotAPositionCode`
   fails any file in the bridge that resolves a director or CEO position code by hand
   again.

`TestEveryVerificationModuleHasAnAudienceCatalogRow` pins that every module in
`pendingModuleProfiles` has its three proof rows and that the row's default names the
same director the profile routes to, so the matrix never shows one desk while the
default push goes to another.

## The write path

`PUT /admin/notifications/designations/{alert_key}` replaces one alert's whole
audience in one transaction: the codes are validated against the ACTIVE designation
catalog inside it (an unknown or retired code is refused, never dropped), the row is
version-fenced (`row_version`; a first customisation must insert, a later one must
match the version the screen loaded, either mismatch is 409 `audience_changed`), and
the audit row lands with it (`notification_audience.replaced` / `.reset`).
`use_defaults: true` deletes the row. The Postgres round trip
`TestAudienceRoundTripThroughTheStoredOverride` proves save, override, stale refusal,
unknown-code refusal, empty-means-nobody, reset and audit on a migrated database.

## Leadership tasks push on every status change

Maintainer ask, same day: "I need tasks when assigned or status changes notification". The
2026-09-04 done-only rule is superseded: `leadership_task.status_changed` now pushes for every
status (doing, done, reopened, cancelled) to the OTHER party -- the raiser when the CXO moved
it (row `leadership.task_done`), the CXO when the raiser did (row `leadership.task_raised`) --
and never to whoever made the change. The consumer is also registered on the API's in-process
bus so a local stack shows the push.

## Proven end to end (2026-09-08, throwaway STG-mirror clone)

Real tasks raised and moved through `/app/leadership-tasks` as Chandrakant (PC Director) and
Aryaman (CXO); real `verification.item.pending` / `verdict.approved` / `verdict.rework`,
`obligation.missed` and `weighing.shed_submission.completed` events replayed through the
kernel-worker's production bus; the low-stock, overdue-load and reminder-cadence stages run
once. Defaults queued exactly the pre-catalog audiences. After the matrix was flipped: low
stock to Park Head only queued nobody (no park on the alert), overdue load reached CXOs plus
the Growth Director, the vaccination proof-pending leadership copy fell silent while the Feed
Director got the verifier's copy, missed work reached the Growth Director alone, a raised task
reached no CXO, and status changes went to the CXOs instead of the raiser.

Two data facts worth knowing from that run: Ravi's and Hemant's only tokened devices carry
`notifications_enabled = false` on STG, so they receive nothing regardless of the matrix; and
Dinakar holds five roles, so any row he is on lists him under every title.

## A gap this closed on the way

`ResolvePositionRecipients` resolved tenant role GRANTS only for five roles
(`ceo_internal`, `pc_director`, `growth_director`, `feed_director`,
`health_director`); `procurement_director` and `breeding_director` were reached only
through a `workforce_positions` seat. With designations now selectable on the matrix
those two, and `verifier`, are in the grant list too, in both the single and the batch read.

A second, larger gap: a **park head is a park-scoped role grant** (`scope_type='park'`) under
the per-person access model, and the STG mirror has four such grants and ZERO `park_head`
seats in `workforce_positions`. Resolving a `center` scope from seats alone reached nobody, so
every park-head copy (proof pending/approved/rework, missed work) was silently going to no one.
Both resolvers now union a `park_grant_recipients` CTE for the park desks; pinned by
`TestResolvePositionRecipientsReachesParkScopedGrants`.

## Adding an alert later

Add a row to the catalog with its default audience, ask
`audience.Recipients(ctx, tenantID, parkID, key)` from the notifier, and nothing
else: the matrix lists it on the next load, and the catalog test refuses a row whose
default names a code that is not a designation.
