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
- **Reading**: the **Purchased vs consumed** tab on `/feed/analytics`, served by
  `GET /feed-analytics/stock-loads`. It rides the Stock tab's permission
  (`feed_analytics.stock.read`), so whoever sees Stock — the Procurement Director included — sees
  it. The Procurement Director's stock-scope page therefore now offers two tabs.

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

Zero means the buyer's figure held. Positive means the load is lasting longer than it was bought
for (amber). **Negative is red**: the load ran, or will run, out sooner than said — the case that
leaves animals unfed if nobody re-orders in time. The tab leads with the whole-filter count of
negative loads so a reader on page one knows how many sit on later pages.

An in-transit load shows its bought quantity as what is coming, nothing consumed, and no days
projection.

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
- `feeddirection/domain.TestStockLoadGapIsAbsentWheneverEitherSideIsUnknown`.
- `procurement/domain.TestFeedPurchaseValidateRejectsEachBadField` (zero / negative days) and
  `procurement/adapters/postgres.TestFeedPurchaseEditPostgresPaths` (recorded, edited, cleared).
- `adminui/app.TestCeoKeepsEveryFeedAnalyticsTab` /
  `TestProcurementDirectorKeepsOnlyStockFeedAnalyticsTab` (the tab rides the stock gate).
