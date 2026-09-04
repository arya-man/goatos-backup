# Feed purchase entry (maintainer decision, 2026-08-24)

## What changed

Feed bought for CBE and CPT is now **recorded in the app**, on `/procurement/feed-purchases`,
with the same fields the legacy "Feed DB" sheet's Purchase row carries.

This **supersedes the read-only half** of the lock recorded in
`backend/migrations/postgres/000174_feed_purchases_ledger.sql`, which said:

> ONE-TIME BOOTSTRAP, READ-ONLY FOR NOW. Rows arrive through `cmd/import-feed-purchases` from the
> sheet's Purchase rows. There is no authoring UI; purchase/vendor entry screens belong to the
> future Procurement vertical. Nothing else writes here.

That vertical now exists (`/procurement/source-entry`, `/procurement/vendors`,
`/procurement/sales`), so the screen it was waiting for was built where 000174 said it belonged.

## What did NOT change

The other two decisions in 000174 stand, and the write path enforces both:

1. **CURRENT-CATALOG FEEDS ONLY.** An entered feed must resolve to an ACTIVE `feed_item_catalog`
   row, checked INSIDE the write transaction. An unknown feed is rejected with
   `feed_item_not_in_catalog` and a message naming Feed Config — never invented into the catalog.
   The importer expresses the same rule by skipping such a feed.
2. **STOCK DEPLETES AT SHEET LOCK.** An app-entered row sets `depletes_from = purchase_date` and
   `consumed_at_import_kg = 0`: GoatOS has directed nothing against a load bought today, so the
   whole quantity is available and the existing stock/days-left read on `/feed/analytics` needed
   **no change at all**.

The importer is unchanged and stays idempotent on `(farm, feed, batch_no)`.

## Where it lives, and why

**Procurement owns the WRITE; feeddirection keeps the READ.** Buying feed is the procurement
desk's job and its suppliers are already in that vertical's vendor register, so the service,
repository and routes are in `backend/internal/procurement`. The stock and days-left cards on
`/feed/analytics` keep reading `feed_purchases` exactly as they did.

## Permissions

`feed.purchase.read` / `feed.purchase.write` are **dedicated permissions**, not a reuse of
`procurement.read`. Seven roles including `operator` and `park_head` hold `procurement.read` for
the source-entry intake screens they work; this ledger carries supplier prices and payment state —
the same class of commercial fact that earned `procurement.vendor.read` its own permission. Gating
the leaf on `procurement.read` would put it in every operator's sidebar.

| Role | Read | Record |
|---|---|---|
| `ceo_internal` | yes | yes (founder/builder visibility invariant) |
| `procurement_director` | yes | yes |
| `procurement_manager` | yes | yes |
| `feed_director` | yes | **no** — owns what the farm feeds and is accountable for the stock cards these loads are counted from, but buying is the procurement desk's job. Same read/write split that keeps this role out of `FeedDirectionComplete`. |
| everyone else | no | no |

Per the role-scoped-UI-is-capability-gated lock, the difference between a read-only Feed Director
and the procurement desk arrives ONLY through the `record_feed_purchase` page-contract control and
the route's own permission. There is no role-string conditional in the page component.

## Rules the entry form applies

- **Purchase date may not be in the future**, judged against the **IST business day** — stock the
  farm does not have yet must not deplete a feed sheet.
- **Batch number is optional.** Left blank, the next number for that farm and feed is assigned
  inside the write transaction, so two concurrent submits cannot both claim `max+1` (the loser hits
  `feed_purchases_natural_uq` and is reported as a duplicate, never silently overwritten).
- **Landed cost:** an explicit total wins; otherwise the feed/transport/loading/unloading parts are
  summed. **Nothing entered stays NULL** — a load whose cost is not yet known is a real state, and
  a zero would report a free load. **Per-kg cost is DERIVED** from total ÷ quantity, never entered,
  so it cannot drift from its own total the way the sheet's hand-kept column does.
- **Payment status** is the sheet's two-word vocabulary (`Paid`, `Pending` — 176 and 29 rows of the
  imported history, and no third value). An unrecognised value is rejected, never defaulted.
- **The catalog's spelling is stored**, not the typed one, so one feed reads with one spelling and
  the stock cards cannot group a feed against itself.
- **`Idempotency-Key` is mandatory.** A feed load is money, and on this ledger a duplicate would
  also double the farm's available stock. An exact replay returns the original row with no new side
  effects; a same-key/different-payload replay is refused.

## Delivery: a load is stock when it REACHES, not when it is bought (maintainer decision 2026-09-03)

This section **supersedes the "STOCK DEPLETES AT SHEET LOCK" note above in one respect**: an
app-entered row no longer goes into stock on its purchase date. Everything else about depletion
(locked sheets, FIFO by `depletes_from`) stands.

**What was wrong.** Recording a purchase put the whole quantity into stock that day and raised the
aflatoxin strip test at once. The farm does not work that way: the procurement desk records the
load when the money is committed, the truck takes three or four days, and only what comes off the
truck is feed anyone can use or test.

**What a load is now.** Two states, no third:

| `delivery_status` | Meaning | Stock | Toxin test |
|---|---|---|---|
| `purchased` | bought, still on the road | counted **nowhere** | not yet raised |
| `reached` | arrived on `reached_on` | counted from that day | raised at that moment |

The desk marks a load reached on `/procurement/feed-purchases` (the **Mark reached** control,
`PUT /procurement/feed-purchases/{id}/delivery`, same `feed.purchase.write` authority as recording
it). The record form also accepts an arrival date for a load that had already come in when it was
written up, so catching up on paper does not leave loads falsely on the road.

**Which weight counts.** The stock figure is the weight actually **received** (`reached_weight_kg`)
once the desk enters it, and the **buying** weight (`quantity_kg`) until then. Entering the received
weight is **deferrable by design**: the weighbridge figure is often known days after the feed is in
use, and stock must not sit at zero waiting for it. When it is entered, stock and the per-kg rate
move to it from that moment; the buying weight is never overwritten, so the loss on the road stays
readable as the difference. The per-kg rate is landed cost over the weight the farm has to show for
it, so a load that shrank reads a higher rate than the vendor quoted -- that is the true cost of the
feed in the store.

**Where the rule lives.** `feed_purchases.stock_kg` is a GENERATED column (received weight if
entered, else buying weight, `0` while on the road). Every stock read in feeddirection analytics
(Stock cards, forecast, Mesha concentrate table, low-stock alert) reads that column and keeps only
`delivery_status = 'reached'` rows, and `depletes_from` follows `reached_on`, so depletion starts on
the day the feed was actually there. No read re-derives the CASE.

**The toxin trigger moved.** `procurement.feed_purchase.recorded` is retired.
`procurement.feed_purchase.reached` is emitted inside the transaction that flips a load to reached
-- on that transition **only**, under the purchase row lock, so a later correction of the arrival
day or received weight raises no second test and two desks marking the same load at once serialize
to one event. The toxin task does **not** wait for anything: stock and the test start together on
arrival, and the test's outcome never gates feeding (v1 rule from the toxin decision, unchanged).

**History.** Every row present before this decision -- sheet history and app rows recorded so far
-- is `reached` on its purchase date (migration `000248`). Nothing in stock or analytics moved for
history; only purchases recorded from now on start on the road. The sheet importer keeps writing
reached rows: it records loads that arrived long ago.

Pinned by `TestFeedPurchaseDeliveryPostgresPaths` (state flip, `depletes_from`, transition-only
event, deferred weight, idempotent replay), `TestStockCountsOnlyLoadsThatReached` (a load on the
road is absent from the balance until it reaches, then counts at the received weight) and the
toxin consumer test, which publishes the retired `recorded` type and expects no task.

## Provenance

Migration `000206_feed_purchases_app_entry.sql` adds `entry_source` (`sheet_import` | `app`) and
`recorded_by`. Existing rows keep the `sheet_import` default — every row present before that
migration arrived through the importer. The ledger shows the difference rather than presenting
imported history as something a person typed on this screen.
