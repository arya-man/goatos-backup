# Feed purchased vs consumed, load by load (maintainer request, 2026-09-19)

## What was asked

When the procurement desk records a feed load it may say how many **days of stock** the load is
meant to cover. Feed Analytics then shows, for every load, what the buyer said against what
actually happened to the load: how many days it has been consumed, how many are left, and whether
the sum comes out even. A load that ran out sooner than it was bought for is highlighted.

The figure is **optional** (maintainer clarification, same day). Sheet history never carried one
and stays blank.

## Where it lives

- **Entry**: `days_of_stock` on the feed-purchase record and edit forms, web
  (`/procurement/feed-purchases`) and phone (Procurement module). One nullable column on
  `feed_purchases` (migration `000381`), `CHECK (days_of_stock IS NULL OR days_of_stock > 0)`.
  Zero is **rejected**, never read as "not stated": a client that coerces a blank box into `0`
  must hear about it rather than store a belief nobody held.
- **Reading**: the **last table on the Stock tab** of `/feed/analytics` (maintainer instruction,
  2026-09-21 — it was briefly a tab of its own), served by `GET /feed-analytics/stock-loads`. The
  cards answer "how much is in the store"; this answers "what happened to each load that put it
  there", and a reader should not change tabs between the two. Being a table on that tab, it rides
  the stock permission (`feed_analytics.stock.read`) by construction: there is no separate tab
  option to grant or withhold, so whoever sees Stock — the Procurement Director included — sees it.
- **Milk is left out** of the rows and of the feed-item filter (maintainer instruction, same day).
  UHT milk is drawn by preparation batches rather than the ration sheet, so days-of-stock per load
  is not a question about it, and an option that can never produce a row would be a dead one. The
  exclusion is one named list, `domain.StockLoadExcludedFeedItemKeys`, reporting-only; milk stock,
  milk purchases and the milk consumption series are untouched everywhere else.

## The arithmetic, and why it is FIFO

Consumption is attributed to loads **oldest arrival first within one (farm, feed)** — the same rule
the Stock tab's "Consumption from" already applies to the latest load. The kg fed are DIRECTED kg
off the LOCKED sheets plus externally-tracked consumption (milk), from the family's ledger start:

```
prior_net_kg  = sum of the kg of every earlier reached load of that feed at that farm
consumed_kg   = clamp(directed_total - prior_net_kg, 0, load_kg)   -- every load but the newest
              = max(0, directed_total - prior_net_kg)               -- the NEWEST load
left_kg       = load_kg - consumed_kg                               -- NEGATIVE on an overrun
```

The newest load takes the whole remainder so that feed the farm fed but the ledger never bought
shows as **negative kg left** (`status = overrun`), never clamped away: the negative number is the
finding, and it says a load is missing from the ledger.

Days:

- `days_consumed` — the locked feed days that drew on the load: a day counts for a load when the
  running total passes the load's start and the total *before* the day is still inside the load's
  range, so a day that finishes one load and starts the next counts for both.
- `days_left` — `left_kg` over the feed's recent daily rate (the same 3-most-recent-locked-days
  average the stock cards use); `0` once the load is finished; **null** when no recent rate exists.
- `gap_days = days_said - days_consumed - days_left` — **null** whenever either side is unknown
  (no figure stated, or no rate to project from). A check nobody could make is not a check that
  passed, so absence renders as a dash, never as "matches".

Zero means the buyer's figure held.

> **OPEN — the sign of this check is currently mislabelled, raised 2026-09-21 and awaiting the
> maintainer's call.** With `gap = said − used − left`, a load that runs SHORT produces a POSITIVE
> number and one that lasts LONGER produces a NEGATIVE one — but the copy reads `+N days` for the
> first and "N days short" (red) for the second, which is the wrong way round on both. Observed
> live: a load said to cover 12 days, 13 used with 1 left (so 2 days LONGER), rendered "2 days
> short". The arithmetic matches the instruction as given; only the words and tones hung on it are
> inverted. Resolve by either relabelling (positive = short, red) or negating the formula; do not
> change one half alone.

The whole-filter count of negative loads was shown as a tile above the table and was REMOVED on
2026-09-21 (maintainer instruction) along with its `negative_gaps` field, end to end — a payload key
no consumer reads is an accept-and-discard, so it is deleted rather than left served.

An in-transit load shows its bought quantity as what is coming, nothing consumed, and no days
projection.

## The handoff: when a new load of a feed already in use starts depleting

Asked directly by the maintainer on 2026-09-21, and the answer is **not on the day it arrives**.

Arrival makes a load ELIGIBLE. The queue decides when it is reached: a load starts depleting on the
first locked feed day whose RUNNING TOTAL for that (farm, feed) passes the kilograms of every load
that arrived before it.

So a 100 kg load bought while 50 kg of the previous load is still in the store waits two more feed
days at 25 kg/day, and its `Used from` reads the day the total crossed 100 — not the arrival day.
The day the total lands EXACTLY on the previous load's last kilogram belongs to the previous load
alone (the boundary is strict), and a day that genuinely straddles two loads counts for both,
because the operator really did feed out of both sacks that day.

Four narrowings, each pinned and each mutation-tested in
`TestStockLoadsHandoffWhenANewLoadOfAFeedAlreadyInUseStartsDepleting`:

- **Arrival day orders the queue, not the purchase day and not the batch number.** A load bought
  late and delivered early leads the queue; a load bought first and delivered last waits. Catch-up
  data entry therefore cannot reorder consumption that already happened.
- **The weighbridge figure moves the queue, not the invoice.** A load bought at 100 kg and received
  at 80 hands over after 80.
- **A load still on the road holds no place in the queue.** It is stock-to-be: it neither blocks the
  load behind it nor is charged anything, and it gets no days projection.
- **A load arriving into a family that has nothing left is drawn on from its own first locked day.**

## KNOWN LIMITATION: a deficit is charged to the next load to arrive, even before it arrived

When the farm feeds past everything the ledger says it bought — the case this tab exists to
surface — the deficit is charged to the newest REACHED load, because that is what makes an overrun
show as negative kg rather than being clamped away. That rule has no date test on it, while the
day columns do, so the two halves of such a row answer from different clocks:

> `gap` family: 100 kg directed against a 50 kg load, the next load lands six days later.
> It reads **in use, 50 kg used, 50 kg left** beside **used 0 days, never started** — and the 50 kg
> left understates the 100 kg physically sitting in the store.

The family total is still right (150 bought − 100 directed = 50 left), so the tab and the stock
cards agree. What is wrong is WHICH LOAD carries the deficit: a load that had not arrived could not
have fed anything, and the honest owner is the load that was newest on the day the feed went out.
Correcting that changes what a per-load number means, so it is a maintainer decision rather than a
bug fix. Both affected rows are pinned in the test (`gap#2` and `backdate#2`) with the value they
should move to if it is ever corrected: `not_started`, with the full load still left.

The tell to watch for in the UI is exactly what the test caught — kilograms consumed beside zero
days consumed.

## What did NOT change

- Nothing on any write path reads `days_of_stock`. It is a reporting comparison only.
- The stock cards, the per-farm Mesha table, the forecast and the expenditure series are untouched
  and range over the same purchases and cells, so the per-feed totals agree by construction.
- The transitional split-concentrate fold is **not** applied here: loads are ledger rows of one
  feed each, and folding a retired member's load into its successor would attribute consumption
  across two ledger identities. If that fold is ever wanted per load it is a separate decision.

## Pinned by

- `feeddirection/adapters/postgres.TestStockLoadsFifoOneToManyStatusBucketsParkScopePageBoundary`
  — two reached loads and one in transit at one farm, a straddling day, an overrun at the other
  farm, a load with no figure, park scope, farm/feed filters and a page boundary with whole-filter
  counts.
- `feeddirection/adapters/postgres.TestStockLoadsHandoffWhenANewLoadOfAFeedAlreadyInUseStartsDepleting`
  — six families, one per edge of the handoff rule: a load arriving mid-life of the previous one,
  the strict exact-zero boundary, a load arriving after the family had already run dry, a received
  weight under the bought weight, an in-transit load that must not block the queue, and a load
  bought late but delivered early. Mutation-tested four ways (relaxing the boundary to `>=`,
  reading `quantity_kg` instead of `stock_kg`, ordering FIFO by purchase day instead of arrival
  day, and letting in-transit loads hold a place) — each turns it red.
- `feeddirection/adapters/postgres.TestStockLoadsEndToEndFromPurchaseThroughSheetLockToCorrection`
  — the whole chain through both modules' real write paths, nothing hand-inserted: a load recorded
  through procurement's `CreateFeedPurchase`, a sheet ISSUED (which must move nothing) then LOCKED
  (which is the moment stock moves), the rate proved as the mean of the three most recent locked
  days, and corrections through `UpdateFeedPurchase` — stated days, quantity, and clearing the
  figure — each reaching the very next read rather than a stale cached page. Mutation-tested by
  letting an issued sheet deplete stock and by narrowing the rate window to two days.
- `feeddirection/adapters/postgres.TestStockLoadsLeavesMilkOutOfTheTableAndOutOfTheFeedFilter`.
- `feeddirection/domain.TestStockLoadGapIsAbsentWheneverEitherSideIsUnknown`.
- `procurement/domain.TestFeedPurchaseValidateRejectsEachBadField` (zero / negative days) and
  `procurement/adapters/postgres.TestFeedPurchaseEditPostgresPaths` (recorded, edited, cleared).
- `adminui/app.TestCeoKeepsEveryFeedAnalyticsTab` /
  `TestProcurementDirectorKeepsOnlyStockFeedAnalyticsTab` (the tab rides the stock gate).
