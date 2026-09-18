# Sales > Farm born: the not-on-a-load half of the herd

Maintainer request 2026-09-18. Status: built; page `/sales/farm-born`, read
`GET /procurement/farm-born-sales`, migration `000357`.

## What was missing

Load wise (`/sales/loads`) reconciles every PURCHASED load: bought, sold, died,
still on farm, and the money. Nothing answered the same questions for the
animals the farm did not buy on a load — the kids born here. "How many do I
have, how many did I sell, of which breed / pen / stage / sex, and what did I
earn" had no screen.

## Decisions

1. **Its own page under Sales, beside Load wise.** The two pages partition the
   herd: an animal is on exactly one of them. Membership is the exact complement
   of the load-wise membership (accepted rows on `procurement_load_goats`).
2. **The page is farm born only: `origin_type = 'birth'`, no origin control.**
   On the live herd 536 alive animals carry no `origin_type` and 392 are marked
   `procured` while sitting on no load (founding stock bought before loads were
   recorded). The read still accepts `origin=bought_no_load|not_recorded` for
   those readings, but the maintainer removed the Origin filter from the page
   the same day it was offered (2026-09-18): the screen answers one question,
   and those animals are not "my farm's kids". They remain reachable through
   the API only.
3. **The period binds the SOLD side only.** "How many do I have" is answered
   live, today, whatever the period. "How many did I sell, of what, for how
   much" is answered for the sales whose date falls in the period. Default
   period: the last calendar month ending today (IST).
4. **Sale date and value.** A sold animal's date is its deal's `sale_date`, else
   the IST day of `goats.exited_at` when it was exited as sold without a deal.
   Its value is its share of its deal (`sales_value / animals tagged`), the same
   attribution Load wise uses; an animal sold with no deal counts as sold and
   carries no value — the headline says how many, and the average divides only
   over priced sales.
5. **Pen at sale.** A sale allocation snapshots park / shed / partition at
   tagging and wins for a sold animal; otherwise the animal's own
   `goats.shed_id` + `goat_shed_partitions` row, which outlives the exit. The
   display is `oploc.Display()`; the wire key is `<shed_id>|<partition>`.
6. **Filter bar governs the whole page** (period, park, pen, species, breed,
   sex, stage) — every KPI, every breakdown and the ledger range over the
   same animals, so each breakdown's On farm / Sold columns sum to the headline
   by construction. Vocabularies for park / pen / breed / stage are LIVE herd
   facts served by the read for the origin reading (not narrowed by the other
   filters); sex / species are contract option groups.
7. **Read-only by contract.** No control; entry stays on Sales Config.
8. **No "on the farm now" figure (maintainer instruction 2026-09-19).** The
   register's `origin_type` is under-filled: 264 of the 2026 kids and 272 older
   adults carry no origin at all, so a live farm-born count read 248 against a
   herd the farm knows is larger. Rather than show a number the register cannot
   back, the tile and the breakdowns' On farm column were removed; the read still
   returns `on_farm` (unused by the page) until the origin field is repaired.

## Where it lives

- Domain / port / repo / service / handler: `backend/internal/procurement/**/farm_born_sales*`
  — a RECORDED cross-module reporting read in the load-wise shape
  (`docs/decisions/sales-loadwise.md`); procurement joins OUT to goats,
  goat_identifiers, goat_shed_partitions, locations, goat_sale_allocations and
  sales_deals. Nothing gates a write; the sales module's own lock (migration
  000173) is untouched.
- Page contract and copy: `adminui/app/service.go` (`sales-farm-born`); catalog
  row in `permissions/capability_pages.go`; route on `SalesRead`.
- Admin-web: `apps/admin-web/features/procurement/sales-farm-born.tsx` +
  `farm-born-sold-table.tsx`; the page owns its park control, so it is listed in
  `PAGES_OWNING_PARK_SCOPE`.

Pinned by `TestSalesFarmBornPageContract`, `TestBuildFarmBornSalesBreakdownsSumToTheHeadline`,
`TestFarmBornServiceRefusesBadFilters`, `TestFarmBornHandlerForwardsEveryFilterAndSerialisesThePage`.
