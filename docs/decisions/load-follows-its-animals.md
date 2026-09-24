# A load is its animals

**Maintainer decision, 2026-09-24.** Supersedes the pen SOURCE of the weighing load charts in
[load-charts-name-their-pens.md](load-charts-name-their-pens.md) and the fixed load-to-pen mapping
of migration `000131_weighing_shed_load_tags.sql`. The bracket of pens beside a load name stays.

In the maintainer's words: *"Initially those loads may be in Castro 1, after some days we move them
to Castro 2, then Castro 3. The load maps to those animals, and the load's ADG should follow how
those animals' weighings went. It should not stick to one particular pen. If I add a load and
weigh, it should come directly — there is no need to add anything separately."*

## What changed

| | Before | Now |
|---|---|---|
| Which animals are a load | the animals in pens typed once into `weighing_shed_load_tags` from the 08/08 load sheet | the animals bought on it (`procurement_load_goats`, written at purchase) |
| A load moves pens | the chart kept reading the old pens, whoever was in them | the chart follows the animals |
| A new load | missing until someone tagged its pens (Load 136, 58 animals, bought 16/09, was missing) | appears as soon as its animals are weighed |
| Pens beside the load name | the tagged pens | where the load's live animals are today (herd register) |
| A pen holding two loads | counted for neither | each load's own animals get that pen's average |

## How a weigh counts for a load animal

- **Kid weighed on its own:** its tag names the animal, so its weighs are its own, in whatever pen.
- **Pen weighed as one total:** there is no tag. The pen's average change between two weighs counts
  for every load animal that was **in that pen at both weighs**. An animal that moved in or out in
  between was not there for the whole stretch and does not get it.
- **Where an animal was on a day:** its last recorded move (`goat_location_history`) on or before
  that day; before its first move, the pen that move took it from; with no move, where the register
  has it now. Move history starts 2026-08-13.
- **An animal's gain** is its total grams over its total days (the headline's statistic); **a load's
  gain** is the mean over its animals; its **latest weight** is each animal's latest weight in the
  period (its own scan, or the pen average of a pen it was in that day).
- **Which loads are shown:** a load stays on every load chart while at least one of its animals is
  on the farm, and leaves once all are sold (maintainer, same day: "show until all animals are
  sold"). Old loads with a few animals left (Load 100: 1, Load 101: 3) stay.
- **Filters:** Sex is the animal's own. Every load animal was bought, so *Farm born* shows no load.

## Surfaces

The Weights page load chart, ADG Analytics › Comparison (latest weight and value), ADG Analytics ›
Time-wise per-load table, and Sales › Loads all read `load_animals.go`. Counts › Breakdown already
read the register and is unchanged. The Comparison tab now reads the **selected period** and hides
the Sex, Origin and Weighing filters it does not apply (maintainer request, same day).

## Isolation

`backend/internal/weighing/adapters/postgres/load_animals.go` is a recorded, file-scoped exemption
in `tools/agent-hooks/check-weighing-free-flow-guard.mjs`, allowed `goats`, `goat_identifiers`,
`goat_shed_partitions`, `goat_location_history`, `procurement_load_goats`, `procurement_loads` and
`parties`. It is read-only and reporting-only: no capture, submit, close or verdict path calls it,
and no scan is gated on a load, a pen or an identity.

Pinned by `TestLoadFollowsItsAnimalsThroughPenMovesOneToManyParkScopeStatusMatrix` and
`TestLoadWeeksFollowTheAnimalsAndThePenScopePageBoundary`.
