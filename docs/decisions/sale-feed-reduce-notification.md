# Sale → Feed Director notice and feed-day reminder

**Maintainer decision, 2026-09-07.** When animals are tagged to a sale, the Feed Director is
told which pens now hold fewer mouths and from which feed day their feed should be smaller, and
is reminded on that feed day to confirm it did reduce. Feed Director alone; no values in the
reminder.

## What the farm asked for

> A notification to go to the feed director when sales are recorded, stating sales recorded,
> feed should reduce in this pen like that. And the next day also a notification should go,
> like feed reduced or not for these pens.

Three choices were put to the maintainer and answered the same day:

| Question | Answer |
| --- | --- |
| When does the first notice fire? | **When the animals are tagged** (the sale-allocation confirm). The commercial sale row on `/sales/config` knows no pens; only the confirm does, and it is also the moment the animals leave the register. |
| What does the next-day message compare? | **Nothing** — "no need to show values, just a reminder is enough." It asks the director to confirm the pens' feed reduced; it does not read the sheet. |
| Who receives both? | **The Feed Director only.** Park heads and the CEO were offered and declined. |

## Why the feed sheet already knows, and why a push is still needed

The confirm exits each animal as `sold` through the canonical per-goat transition, so the herd
register drops them at once and the feed sheet — which projects head counts from the live
register plus pending shiftings — counts the pen smaller on the next issue or correction. Nothing
in the feed chain is changed by this decision.

What was missing was the *person*: the sheet quietly shrank a head count and the packer found a
smaller bag, with nobody accountable for feed having been told why. The notice closes that gap;
the reminder closes the other half, that a reduction the director was told about did in fact
land.

## The feed day is the clock's, not a constant

A sheet for feed day F is issued at `direction_time` on F−1 and may be amended once at
`correction_time` on F−1 (the afternoon correction). So a sale confirmed on business date D:

- **before D's correction cutoff** → feed day **D+1** (its sheet is corrected at D 14:00 with the new count);
- **at or after that cutoff** → feed day **D+2** (D+1's sheet is frozen and loaded; the next morning's issue is the first to see the register).

The cutoff is read from the park's own `feed_schedule_config` row for the **normal** workflow
(`feeddirection/domain.SaleFeedReductionClock`), never hardcoded. With no clock on record the
rule falls back to D+1 — the earlier candidate, so the director checks a day early rather than a
day late. Rule and both branches: `feeddirection/domain.SaleFeedReductionDay`, pinned by
`TestSaleFeedReductionDayFollowsTheParkCorrectionCutoff`.

## Shape

```text
RecordSaleAllocations (identity, one tx)
  ├─ goat_sale_allocations rows           (snapshot of pen per animal)
  ├─ goat.exited × N                      (per animal: obligation/tasks/health consume)
  └─ goat.sale_allocated × 1  ── NEW ──   (per confirm: pens + counts + allocated_at)
                                 │
                                 ▼
   notificationbridge.SaleFeedReduceNotifier.HandleEvent
       → feed_sale_reduce push to feed_director         "Sale confirmed: reduce feed for 2 pens at Coimbatore"

   kernelstages.SaleFeedReduceReminderStage (shared 5-min cadence)
       → reads confirms of the last 4 days (goat_sale_allocations_recent_tagged_idx)
       → on each batch's feed day, from 07:00 IST
       → feed_sale_reduce_reminder push to feed_director "Did feed reduce for 2 pens at Coimbatore?"
```

**Why a second event beside `goat.exited`.** The Feed Director's grain is the pen. Reassembling
"8 left Castro 1, 4 left Mandela 1 - Part 2" from up to a hundred per-goat events would mean a
consumer that re-reads the allocation table on every one and relies on the notification queue
to swallow ninety-nine duplicates. The confirm transaction already knows the whole batch; it
says so once. The aggregate is the sale (`sales_deal`), an opaque reference as migration 000177
settled.

**Once each, no scheduler.** Both pushes are keyed on the deal and the confirm instant
(`feed.sale_reduce:<deal>:<µs>` / `feed.sale_reduce.reminder:<deal>:<µs>`); a redelivered event, a
retried tick or the HA pair collapse onto one row per device. The reminder is a stage on the
shared operational cadence per the task-kernel lock — no private cron — and "on the feed day at a
waking hour" is a gate inside the notifier, so a worker down at 07:00 still reminds when it
returns.

**A sale confirmed in two halves** (more than 100 animals, or a later addition) is two batches,
two notices and two reminders — each is a separate moment the register changed.

## Copy

Both messages name the park, every pen with how many animals left it (capped at six named pens,
the rest counted), the sale date and the feed day, in farm date format — the notification
specificity rule. No kilograms and no sheet head counts anywhere, by the maintainer's answer.

## Recorded boundaries

- `notification_requests.notification_type` gains `feed_sale_reduce` and
  `feed_sale_reduce_reminder` (migration `000274`; the outbox validator branch is `000275`); the enum stays closed.
- `goat_sale_allocations_recent_tagged_idx` `(tenant_id, allocated_at DESC) WHERE status='tagged'`
  serves the reminder's trailing read.
- Weighing is untouched. The feed chain's write paths are untouched. Android renders the push
  through the existing `href`/`screen` extras (`/feed/analytics`); no new screen.
- Pinned by `sale_feed_reduce_notify_test.go` (notice copy and audience, cutoff branch, reminder
  gate and key stability, pen-list cap, no-recipient path), the identity integration test on the
  real confirm path (one event per confirm, pens in the payload, batch reader round trip), and the
  kernel-worker wiring test.
