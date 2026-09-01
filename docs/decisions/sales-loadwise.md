# Sales: load-wise reconciliation (Purchased vs From the barn)

Maintainer decisions 2026-08-31 (Manohar). Status: accepted, implemented.

## What was asked

Under Sales, two tabs: **Purchased** (animals that came in through procurement loads — the
fattening pipeline) and **From the barn** (farm-born / breeding side). Under Purchased, load-wise
data: per procurement load, how many were purchased, sold, and died — and if the numbers do not
add up, the difference is displayed, never absorbed. Money on the same grain: total purchase value
of the load, sold value of the load, and the remaining stock's value estimated at the average sold
price. Two charts: numbers, and cost — the value chart carrying three bars (purchase value, sold
value, remaining estimated value) with the counts readable beside them.

## Decisions locked

1. **Counts reconcile per load, with an explicit Unaccounted column.** Outcomes are DISJOINT
   buckets over the load's accepted animals: sold / mortality (died) / other exits (culled,
   transferred, lost — real outcomes, not discrepancies) / remaining (alive + clinical states).
   **The DENOMINATOR is the load's own declared size** (`procurement_loads.expected_count`)
   whenever it states one; only a load that declares nothing falls back to the animals attributed
   to it. Unaccounted is the gap against that denominator — positive when the load declares
   animals nothing accounts for, negative when more are attributed than declared — and renders
   red either way, never clamped and never absorbed.

   *Why the denominator matters (correction made the same day):* deriving Purchased from its own
   parts made Unaccounted zero by construction, so the column could never fire — the exact
   difference the maintainer asked to see was structurally invisible. Pinned by
   `TestFinalizeLoadwiseUsesTheDeclaredCountAsTheDenominator`.

1b. **Pre-GoatOS history is folded in, with its dates.** A legacy load was partly sold and partly
   dead before its remaining animals were tracked here. `procurement_load_prior_outcomes`
   (migration 000232) holds one aggregate row per (load, outcome): count, sold revenue where the
   records carry it, and the date range the events span, each with its source. `FinalizeLoadwise`
   folds those into sold / mortality / sold value before deriving Unaccounted; the raw blocks stay
   on the row so the table tooltips and the cost drawer show the history with its dates. The load
   NUMBER (`context->>'load_ref'`, e.g. 131) leads every row, chart axis and drawer header — the
   same identity the Weights "Daily gain by load" card uses.
2. **Purchase value is a RECORDED landed cost, entered in the app.** `procurement_loads` gains
   `animal_cost`, `transport_cost`, `other_cost` (migration 000232). Absent cost renders "Cost
   not recorded" — never a fabricated zero, and never a vendor-price estimate. Entry is the
   load-wise table's row drawer, gated on the DEDICATED buying-desk permission
   `procurement.load_cost.write` (`LoadCostWrite`) — the FeedPurchaseWrite precedent: operators
   hold ProcurementWrite for source-entry, and this is supplier money. Granted exactly where
   FeedPurchaseWrite is granted (ceo_internal, procurement_director, procurement_manager;
   capability module `load_costs`).
3. **Sold value is attributed per animal through tagged sale allocations.** A deal's
   `sales_value` divides evenly across its `goat_sale_allocations` rows with `status='tagged'`
   (pre-aggregated per deal before joining), summed by the load each animal came from. A sold
   animal with no tagged deal contributes nothing and is counted as unpriced (`sold_priced` <
   `sold` shows an asterisk + hint). Imported sheet-history deals have no allocations, so their
   revenue stays unattributed rather than guessed onto loads.
4. **Remaining stock value = remaining × average sold price**, basis in order: the load's OWN
   priced sales; else the tenant-wide average across every tagged, positive-value sale (farm-born
   included — a realized price is a price); else NO estimate (`price_basis` = load / overall /
   none). A zero-value share never forms a basis.
5. **Only the loads STG already tracks.** The eight loads on the Weights "Daily gain by load"
   card (`weighing_shed_load_tags`: 100, 101, 113, 126, 128, 129, 130, 131) are seeded by
   `tools/dev/seed-stg-loadwise-legacy-loads.sql` from the load sheet plus the legacy BigQuery
   outcome history; every other sheet load waits for procurement source entry. Membership is each
   load's still-alive residents of its tagged pen. Loads 100 and 101 share one tagged pen, so
   neither claims it — their survivors surface as a red Unaccounted count (1 and 3), which is
   exactly the `current_count` the legacy records carry for those loads.
6. **From the barn is a shell** until the farm-born sales view is designed; its tab renders
   backend-owned copy saying so.

## Boundary note (recorded cross-module read)

Migration 000173's sales lock stands: the sales module reads nothing from herd/procurement. This
feature's read lives in PROCUREMENT (`backend/internal/procurement/adapters/postgres/
loadwise_repository.go`) and joins OUT to `goats`, `goat_sale_allocations` and `sales_deals` —
read-only, reporting grain only; nothing gates a sale, an exit, or a pipeline step on it. The
dependency direction is procurement → sales facts, the same shape as `sales_deals.buyer_vendor_id`
pointing the other way (opaque, no FK).

## Contract

- `GET /procurement/loadwise-sales` (permission `sales.read`): newest 60 loads + whole-tenant
  `total_loads` + `overall_avg_sold_price` + a summary over exactly the served rows. Backend owns
  every number; clients render verbatim (grain proof in the repository's `projection-review`
  marker; pinned by `TestLoadwiseSalesPostgresRead`).
- `PUT /procurement/loads/{load_id}/cost` (permission `procurement.load_cost.write`): full-state
  cost write, naturally idempotent, row-locked, audited (`procurement.load_cost.set`); negative
  values and detail-without-animal-cost rejected (schema + domain).
- Sales page contract: `sales-loadwise` table, `sales_views` tab option group (purchased /
  from_barn), load-wise copy keys, `record_load_cost` control (pinned by
  `TestSalesPageContractAndNavigation` and `TestRecordLoadCostControlIsCapabilityGated`).

## Rendered proof (2026-08-31)

Verified in Chrome against a clone of STG (schema + herd/sales/location data), migration 000232
applied and the seed run: eight loads render with their numbers, both charts, and a table where
load 113 reconciles exactly (100 = 91 sold + 9 died + 0 remaining + 0 unaccounted). The cost
drawer opens on a load, shows "Before these records" with the dated history, and recording a
transport cost returns "Load cost recorded." with the audit row written.

Two defects found in that rendered review and fixed in the same batch: the table shredded its own
values (the global `.celllink { overflow-wrap: anywhere }` split "91" into "9"/"1" and stacked
"CPT" a letter per line — fixed with the scoped three-property rule herd-register and people
already carry), and the Farm column read "Not recorded" for a sold-out load, which now falls back
to the farm the load itself records.
