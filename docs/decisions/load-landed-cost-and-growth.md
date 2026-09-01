# Purchase value is LANDED cost, and a load is read per animal and per kilogram

**Maintainer decisions, 2026-09-01.**

> "Purchase value must include all costs including transport. It is not just ex-farm animal value."

> "no keep table like that only on clicking show detailed data"

> "Below this graph, I need two graphs in the same order of loads — Avg purchase weight per animal
> / Avg sale weight per animal — Per kg landing price / Per kg sale price — Number of fattening
> days in Mesha (Sale date - date on which animals reached the farm). Purchase date can be earlier
> as we do warmup at the source."

## 1. The defect: the formula was right, the data was half-imported

`procurement/domain.loadPurchaseValue` has always been `animal + transport + other`. Only the
animal figure was ever recorded, so Purchase value read as the ex-farm animal price.

The cause is written in the original importer,
`tools/dev/seed-stg-loadwise-legacy-loads.sql`:

> "Cost lands in animal_cost (the sheet keeps one total; no transport split exists there)."

That reading was wrong in a specific and instructive way. The farm's Procurement DB sheet does not
split cost into COLUMNS — it records **one row per cost event per load**, with a `Record Type` of
Purchase, Transport, Booking, Labour, Transit or Transition Feed. The importer took the Purchase
row and concluded no split existed. Across the sheet that left ₹12.1L of transport, ₹5.0L of
booking, ₹1.3L of labour, ₹0.8L of transit and ₹0.35L of transition feed outside the number the
farm was shown. On the eight live loads it understated purchase value by **₹2.99L (5.8%)**:
₹51.9L shown against ₹54.9L actually spent, and profit overstated by the same ₹2.99L.

**All six cost types count** (maintainer decision). `animal` and `transport` keep their own
columns; booking, labour, transit and transition feed share `other`.

**Independent confirmation.** The sheet maintains its own hand-computed "Per kg cost" column. On
seven of the eight loads it equals landed cost ÷ live weight to the paisa (the eighth differs by
₹0.03, the sheet's own rounding). The farm had been computing landing price per live kg by hand
all along; GoatOS now derives the same number.

## 2. The itemisation: three columns, detail on click

The list keeps three cost columns. Opening a load shows what they are made of.

`procurement_load_cost_lines` is the source of truth and the three columns on `procurement_loads`
are a roll-up maintained in the same transaction. Two writable places for one number is how the
two come to disagree, so there is exactly one rule: **a hand edit through the cost drawer replaces
that load's lines with one line per bucket it names.** The breakdown a reader opens can then never
claim a split that does not add up to the figure beside it.

An **unknown cost kind rolls into `other` rather than being dropped** — a cost nobody has
classified is still money the farm spent, and silently excluding it is the same defect one layer
down.

## 3. Landing price per live kg, and the growth charts

`purchase_weight_kg` (live weight bought) joins the load, and the table gains
**Landing price / live kg** = landed cost ÷ live kg. Three charts follow the money chart, in the
same load order: average weight per animal (in vs out), price per kg (landing vs sale), and
fattening days.

**The fattening clock starts on ARRIVAL, not purchase.** The farm warms animals up at the source,
so a load is bought a day or more before it lands. `arrived_on` comes from the sheet's `Unloaded`
row; on all eight loads it is purchase + 1 day. `fattening_days` is **animal-weighted** across the
load's sales — a load that leaves in four batches over four months has no single sale date, and
weighting by how many animals left on each answers "how long was the average animal fattened"
rather than "when did the last straggler go".

## 4. The sale side: one source for weight, another for revenue — deliberately

Two legacy sources disagree on sold revenue:

| load | `salesDB_clean` | `fattening_load_sales_comparison` (on screen today) |
| --- | --- | --- |
| 100 | ₹16,24,875 | ₹12,26,428 |
| 101 | ₹3,72,355 | ₹11,18,399 |
| 113 | ₹12,73,832 | ₹12,46,533 |

Load 101's gap is explained: its largest sale (41 of 67 animals, 2026-05-08) carries blank weight
AND blank value in `salesDB_clean`.

**The maintainer chose to keep the displayed Sold value and take only weight from salesDB.** So
`sold_weighed_value` feeds price-per-kg ONLY and must never be summed into the sold-value column.

**The denominator is the load-bearing part.** `sold_weighed_animals` counts only the animals that
actually carry a sale weight. Dividing load 101's 882.58 kg by its 66 SOLD animals gives 13.4 kg
against an 18.9 kg purchase animal — the farm appears to have SHRUNK its stock over 204 days of
fattening, a conclusion drawn entirely from a missing-data artefact. Dividing by the 26 weighed
animals gives 33.9 kg, which is the real figure. Price per kg divides value and weight drawn from
that same set of rows.

A load that has sold nothing reports **absence, not zero**, on every sale-side figure: a 0 kg sale
animal would be drawn as a real bar and read as "these animals are worthless".

## 5. The age clock and the daily CXO alert

`days_since_purchase` is the load's AGE — purchase date to today's Asia/Kolkata business date. It
is a **different clock from `fattening_days`** and the two must not be confused:

| | starts | stops |
| --- | --- | --- |
| `fattening_days` | ARRIVAL on farm | the load's sales (animal-weighted) |
| `days_since_purchase` | PURCHASE | never — it runs while the load is open |

Derived, never stored: a stored age is wrong the next morning.

**A load past `LoadAgeAlertDays` (90) that still holds animals raises a daily alert to the CXO.**
Both conditions matter, and the second is what makes the alert worth reading: a load bought a year
ago that sold out is history, and alerting on it every morning forever would train the reader to
ignore the alert — which costs more than the alert gains. On the live data that is 5 of 8 loads;
load 113 is 292 days old and correctly silent because it has no animals left.

Counted as strictly greater than 90 — "exceeds 90 days" is the maintainer's wording.

**To the CXO alone.** The Procurement Director buys loads and the Feed Director feeds them, but
the decision to hold or move stock sits with that desk. Widening the audience is a maintainer
decision, as it was for the feed low-stock alert's three seats.

**Once a day with no scheduler.** `LoadAgeAlertStage` rides the shared 5-minute operational
cadence; "once per day" comes from the business date inside the idempotency key, so the first tick
of the day writes and every later tick writes nothing. That property survives a worker restart, a
mid-day redeploy, and both HA instances ticking together, and it rolls over to a fresh key
tomorrow. Same mechanism as `FeedLowStockNotifier` — the house pattern for a daily alert, and the
task-kernel lock forbids a module keeping a private scheduler.

**The alert reads the finished read model**, not raw rows, so the push and the Purchase & barn
chart can never disagree about whether a load is overdue.

## Proof

- `TestPurchaseValueIsLandedCostNotExFarm` — Load 131's real figures roll up to ₹5,28,900, and the
  test fails explicitly if the value is the ex-farm ₹5,02,000. Mutation-tested by dropping the
  non-animal/transport kinds.
- `TestEveryCostKindReachesABucket` / `TestUnknownCostKindIsCountedNotDropped` — no cost type can
  silently understate landed cost.
- `TestEmptyBucketsAreAbsentNotZero` — mutation-tested by collapsing nil to zero.
- `TestSaleWeightAverageUsesOnlyTheWeighedAnimals` / `TestSalePricePerKgDividesOneSetOfSales` —
  both mutation-tested, by using the sold count as the denominator and by using the other source's
  revenue.
- `TestUnsoldLoadHasNoSaleFiguresAtAll`, plus the four grain tests
  (`...OneToManyCostLinesDoNotMultiplyTheLoad`, `...PaginationKeepsWholeTenantTotalDistinct`,
  `...ParkScopeStaysWithItsOwnLoad`, `...StatusBucketsStayDisjoint`).

- `TestOverdueLoadAlertFiresOnlyForOpenLoadsPastTheThreshold` — mutation-tested by dropping the
  still-holding-animals gate and by making the threshold inclusive.
- `TestOverdueLoadAlertGoesToTheCXOAlone` — three other leadership seats are reachable in the
  fixture, so any widening turns it red.
- `TestOverdueLoadAlertIsOncePerDayByIdempotencyKey` — same key across ticks, different key
  tomorrow. Mutation-tested by dropping the business date from the key.
- `TestOverdueLoadAlertNamesTheLoadVendorFarmAgeAndHeadCount`,
  `TestOverdueLoadAlertReadsTheSharedReadModelOnce`, `TestNoOverdueLoadsQueuesNothing`.

Schema: migration `000234_procurement_load_cost_lines.sql`.
