# Farm valuation assumptions are data, edited on Sales Config (maintainer instruction 2026-09-19)

Status: accepted. Owner: sales + procurement + weighing (one parameter) + adminui + admin-web.
Migration `000367_sales_valuation_assumptions.sql`.

## What was asked

"Form value — how you are calculating the price — and the Over 35 kg chart: if I want to change
it to 40 kg it should be configurable in web. Make this also configurable and change them. Remove
the line 'Animals not yet sold are valued at ₹11,794 each' and make this also configurable."

## What was hard-coded

- The Farm value formula was a `VALUES` table inside the overview SQL: fattening at the measured
  weight × ₹450; adult females 40 kg × ₹600; bucks 60 kg × ₹500; K0/K1 3 kg, K2 8 kg, K3 15 kg
  × ₹500.
- "Over 35 kg" was `weighing/domain.SaleThresholdUpperKg` plus a literal 35 on the page and in the
  margin control.
- Load wise valued every unsold animal at the overall average sold price and said so under the
  table.

## Decision

One row per tenant, `sales_valuation_assumptions`, seeded with exactly the figures above so no
farm's valuation moved on deploy, re-read per request:

| figure | key |
|---|---|
| per bucket: label, weight used (blank = measured), ₹ per kg | `buckets` (jsonb; keyed `<stage>_<species>_<gender>` since 2026-10-02, four rows per stage — see `docs/decisions/loadwise-stock-valuation.md`) |
| ~~price every unsold animal is carried at on Load wise~~ | `unsold_stock_price_rupees` — RETIRED 2026-10-02: Load wise now values each unsold animal at its latest weight × its bucket's ₹/kg; the input is removed and a save clears the column |

- **Edited on Sales Config**, a "Farm valuation" section beside the market survey: one form, one
  save, landing in place. `GET/PUT /sales/valuation-assumptions`; the PUT is a whole-set replace
  under a `row_version` fence (409), figures outside their band are **refused 400 naming the
  field**, never clamped; one audit row per write.
- **Access is its own permission** `sales.valuation.write`, on the Sales module's Configure level
  (held by CEO/CXO and the Procurement Director), so `/people` grants it per person; the page
  shows the section with the save disabled and the backend's reason for everyone else. Reading
  rides `sales.read`.
- **Consumers**: the overview's valuation CTE reads the row (falling back to the seeded defaults
  for a tenant with no row); Load wise reads the bucket `price_per_kg` (and the authored stages) and
  values each unsold animal at its latest weight × that price (docs/decisions/sales-loadwise.md,
  2026-10-02); the sale-ready line itself is NOT here: it is the growth assumption `sale_ready_threshold_kg`, edited
  from the Weighing SOP page's Assumptions drawer (docs/decisions/weighing-assumptions.md), and the Farm
  value page hands that figure to the weighing shed-weights read as before;
  as the `sale_threshold_kg` **parameter** — weighing stays isolated, the figure arrives on the
  request, never from a table — and fills `{kg}` in the card copy from it.
- The "Animals not yet sold are valued at ₹N each" line under Load wise is removed; the figure is
  set and read on Sales Config.

## Relation to PR #320 (Weighing assumptions)

#320 makes the Weighing area's own decided figures (sale price ₹/kg for FCR, weight bands, the
30/35 kg lines on the Weights pages) editable from the Assumptions drawer on `/weighing/sops`, and
also hands Farm value a sale-ready line. When both land, the sale-ready line has two homes and
must be reconciled to ONE (the Sales Config row is the sales page's; the drawer is the Weights
pages'). Everything else here — the valuation buckets and the unsold-stock price — is sales money
#320 does not touch.

## Pinned by

`sales/domain.TestValidateValuationAssumptions`, `procurement/domain.
TestFinalizeLoadwiseAssumedUnsoldPriceReplacesEveryBasis`, and the Chrome run on the throwaway
stack (2026-09-19): save 45 kg × ₹700 / 40 kg / ₹15,000 → Farm value ADULT FEMALES ₹2,53,57,500
(805 × 45 × 700), OVER 40 KG, margin "40+"; Load wise "incl. stock ₹52L" (347 × 15,000), the note
gone; a price of 0 refused "buckets[5].price_per_kg: must be between 1 and 10000 rupees per kg";
an operator's PUT 403.

## Rebase note 2026-09-20

Landed after the Weighing Assumptions drawer (`000363`-`000365`), which had already made the
sale-ready line data (`growth_assumptions.sale_ready_threshold_kg`, read by Farm value and the
Weights pages). The `sale_ready_kg` column this decision first carried was DROPPED before landing
so the line has exactly one home; migration `000367` never created it on any shared environment.
