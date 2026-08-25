# Sales vertical and the Economics page

Maintainer decisions, 2026-08-25.

## 1. Sales is its own vertical

Sales moved OUT of the Procurement nav group into its own **Sales** group:
selling is not buying. This is a **nav regrouping, not a route change** — the
Sales page keeps its `/procurement/sales` href, exactly as the Milk split kept
`/counts/milk-preparation` (see the nav comment in
`backend/internal/adminui/app/service.go`). Deep links, the page contract's
route id, and every existing test keep working; only the sidebar group and the
breadcrumb (`crumb: "Sales"`) changed.

The Sales group holds two leaves: **Sales** (the deals board, gated `SalesRead`
as before) and **Economics** (`/sales/economics`, new).

## 2. The Economics page is the core-of-the-business read

`/sales/economics` lays the three sides of the business against each other, per
animal: what a day of feed costs (the priced feed sheet), what a day of growth
returns (weighing pairs priced at the realized ₹/kg), and what a kg actually
sells for (the sales ledger). One backend read serves it:
`GET /economics/overview`.

### Leadership-only

The page carries feed spend, sale values and margins side by side, so it is
gated on the **dedicated `sales.economics.read`**, granted to `ceo_internal`
ALONE. It is deliberately NOT a reuse of `SalesRead` (held by the sales and
procurement directors) and NOT granted to `growth_director` — each would see
the other's money numbers. Widening it to a director later is a one-line grant
change. Pinned by `TestSalesEconomicsNavLeafIsLeadershipOnly` and
`TestEconomicsGateIsTheDedicatedPermission`.

### Its own read-only module, in the Growth Director's shape

`backend/internal/economics` is a READ-ONLY reporting module outside weighing,
sales, feeddirection and identity, mirroring `backend/internal/growthdirector`
— the recorded precedent for consuming weighing tables without touching the
weighing isolation boundary (`docs/weighing/growth-director-weights-widgets.md`
→ "Architecture decision — why a new module"). It gates nothing, writes
nothing, and needs no new guard exemptions:

- **Weighing isolation**: untouched — zero diff under
  `backend/internal/weighing/**`; tags resolve through
  `goat_identifiers.normalized_value = upper(tag_key)` with the one-hop merge
  redirect, the same join growthdirector uses. A tag that resolves to nothing
  simply stays out of this report (the pulse discloses the denominators).
- **Sales lock (000173)**: untouched — sales still reads no herd table. This
  module reaches the deal row through the identity-owned
  `goat_sale_allocations` mapping by its **opaque** `sales_deal_id`, the exact
  read path migration `000177` describes.

### Estimate semantics (`estimate: true`, always)

Every rupee figure is a disclosed estimate:

- **Feed cost** is what the sheet DIRECTED, priced at the latest same-park
  purchase load on or before the feed day (the same LATERAL the Feed Analytics
  expenditure read uses) — never a measured consumption. Blocked cells (NULL)
  are never coalesced to zero; an item with no purchase on record prices
  NOTHING and is counted in `pulse.unpriced_feed_items` instead of invented.
  Experiment pens ARE priced per head by dividing the pen's authored total by
  its recorded cohort size — dividing a real total by a real count is honest;
  what stays banned is MULTIPLYING grams by an informational head count.
- **Per-animal feed cost** is the animal's own (shed, pen, stage tag, breed)
  grain cell. A mixed pen's sheet row carries `'_+_'`-joined composite keys, so
  the match is SET MEMBERSHIP, collapsed with avg() to keep one row per animal
  (`TestEconomicsOneToManyFeedCellsCollapsePerAnimal`).
- **Per-animal sale revenue** is the deal's `sales_value` split EVENLY across
  its live tagged allocations — no per-animal price is recorded anywhere in the
  system, and the page's copy says so.
- **Deal figures are tenant-wide** (realized price, sold revenue): the sales
  ledger records a farm label, not a park id, so the park filter narrows animal
  and feed figures only (`TestEconomicsParkScopeNarrowsAnimalsNotDeals`).
- **Realized price basis** is disclosed: the window's own closed weighed deals,
  falling back to the trailing 365 days, falling back to null — never a made-up
  price.

### Grain and caps

The per-animal table and sold panel are capped at 200 rows (worst daily net
first / newest sale first); every pulse and band figure is a whole-filter
aggregate computed independently, so the caps never bend a headline number
(`TestEconomicsPaginationCapNeverBendsSummaries`). Status boundaries
(rework weighs, non-closed deals, released allocations, blocked cells) are
pinned by `TestEconomicsStatusMatrixReworkDealsAllocationsAndBlockedFeed`.
