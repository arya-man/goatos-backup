# Sales > Farm born: the animals born on this farm

Maintainer request 2026-09-18. Status: built; page `/sales/farm-born`, read
`GET /procurement/farm-born-sales`, migration `000357`.

## What was missing

Load wise (`/sales/loads`) reconciles every PURCHASED load: bought, sold, died,
still on farm, and the money. Nothing answered the same questions for the
animals born here — the kids the farm bred itself. "How many do I
have, how many did I sell, of which breed / pen / stage / sex, and what did I
earn" had no screen.

## Decisions

1. **Its own page under Sales, beside Load wise.** Load wise reconciles every
   purchased load; this page answers the same questions for the farm's own
   animals. (Its original "the two pages partition the herd" clause is retired
   by decision 2 below.)
2. **Population = exactly `goats.origin_type = 'birth'`** (maintainer decision
   2026-09-22, SUPERSEDING the 2026-09-19 not-on-a-load rule quoted below). The
   page answers what the register actually says about where an animal came from.
   A blank origin is NOT read as born here: that is the register saying nothing,
   and guessing on its behalf is how a bought animal ends up counted as the
   farm's own.

   **The two pages no longer partition the herd, and that is accepted rather
   than overlooked.** The retired rule was chosen precisely because the origin
   field is under-filled — on the live herd 536 alive animals carry no
   `origin_type` (264 of them this year's kids) and 392 more are marked
   `procured` while sitting on no purchase load — so "on the farm now" read 248
   against the 1,176 the complement rule produced. Those animals now appear on
   NEITHER Farm born nor Load wise. Closing that gap is a REGISTER job: fill in
   the origins. A reporting read that swallowed the unknowns to make the two
   halves add up would be inventing origins nobody recorded.

   Decision 1's "an animal is on exactly one of them" therefore no longer holds
   in either direction: an animal marked `birth` that also sits on an accepted
   load reads on both pages, and an animal with no origin reads on neither.
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
