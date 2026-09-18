# Sales > Farm born: the not-on-a-load half of the herd

Maintainer request 2026-09-18. Status: built; page `/sales/farm-born`, read
`GET /procurement/farm-born-sales`, migration `000346`.

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
2. **Population = every animal not on an accepted purchase load — the register's
   origin field is ignored** (maintainer instruction 2026-09-19, replacing two
   earlier readings the same day). On the live herd 536 alive animals carry no
   `origin_type` (264 of them this year's kids) and 392 are marked `procured`
   while on no load; keyed on that field, "on the farm now" read 248 against a
   herd the farm knows is larger. The load table is the one complete fact, and
   "not bought on a load" is what the farm means by its own stock. On farm today:
   1,176; with Load wise's "still on farm" (397) that is the whole live herd.
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
   facts served by the read (not narrowed by the other filters); sex / species
   are contract option groups.
7. **Read-only by contract.** No control; entry stays on Sales Config.
