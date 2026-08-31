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

1. **Counts reconcile per load, with an explicit Unaccounted column.** Purchased = animals
   accepted at herd intake for the load. Outcomes are DISJOINT buckets over exactly those animals:
   sold / mortality (died) / other exits (culled, transferred, lost — real outcomes, not
   discrepancies) / remaining (alive + clinical states) / unaccounted (merged, inactive, data
   gaps). Unaccounted renders red when non-zero.
2. **Purchase value is a RECORDED landed cost, entered in the app.** `procurement_loads` gains
   `animal_cost`, `transport_cost`, `other_cost` (migration 000229). Absent cost renders "Cost
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
5. **App loads only.** Legacy sheet loads (the old dashboard's ~100–131) are not imported by this
   feature. Where the legacy sheet carries details for loads that ALREADY exist in goatos (STG),
   those details may be backfilled onto those existing loads only (follow-up, same 2026-08-31
   thread).
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
