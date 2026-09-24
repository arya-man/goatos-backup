# Mortality rate: divide by every animal that was there

Date: 2026-09-24
Status: accepted (maintainer decision)
Surface: Counts › Mortality (`/counts/mortality`, `GET /counts/mortality`)
Supersedes: the 2026-09-18 rule "the denominator is the section's live head count today"

## Rule

```text
mortality % = deaths in the window
            ÷ every animal that was in that section at any point in the window
```

"Every animal that was there" includes animals that died, were shifted out or were sold
during the window, and animals that arrived during it. A death still counts in the section
the animal was in **when it died**. Only the divisor changed.

## Why the old rule was replaced

The old divisor was today's live head count, so:

| What happened | Old rule | New rule |
|---|---|---|
| F2: 30 animals, 1 dies, then 15 shifted out | 1 ÷ 15 = 6.7% | 1 of 30 = 3.3% |
| ICU: the only animal dies | 1 ÷ 0, no rate | 1 of 1 = 100% |
| Fattening: 100 animals, 2 die, then 80 sold | 2 ÷ 18 = 11.1% | 2 of 100 = 2.0% |
| A past month, after animals move today | changes | stays fixed |

Sick, ICU and quarantined animals were also left out of the old divisor, because it counted
only `lifecycle_status = 'alive'`. They are on the farm, so they are counted now.

## How it is computed

- **Farm total, breed, sex, species, farm, load, vendor** never change for an animal. A
  section's animals are the animals on the farm during the window: they arrived on or before
  its last day and had not left before its first day. Every death in the window is included.
- **Stage and kid/adult** come from `goat.stage_changed` events in `goat_identity_events`.
  **Pen** comes from `goat_location_history`, which records both partitions. Each is read as
  spans, worked backwards from the animal's own row. The span since the last change holds the
  row's value; each earlier span holds the value the change that ended it replaced. Anchoring
  on the row means a stage written without an event can't contradict the current value.
- A kid that moved to an adult stage in the window counts as a kid **and** an adult.

## What readers must know

- Deaths add up across a series. Animals do **not** for stage, kid/adult and pen: an animal
  counts in every section it passed through. The farm total counts each animal once.
- Over a long window, an animal that spent one day in a pen counts the same as one that
  stayed for months. Read long periods month by month.

## Unchanged

The page layout, columns, API field names (`animals`, `rate_pct`) and every COUNT series (age
at death, season, cause, days since arrival or vaccination) are unchanged. Only the page's
explanatory sentences were reworded to state the new rule.

## Pinned by

`backend/internal/counts/adapters/postgres/mortality_integration_test.go`:
`TestMortalityAnimalsAreEveryAnimalThatWasThere`, `TestMortalityShiftingOutDoesNotMoveTheRate`
(including a closed month staying fixed after a later shift),
`TestMortalityTheOnlyAnimalInAPenDyingReadsOneOfOne`, `TestMortalitySellingAnimalsDoesNotMoveTheRate`
and `TestMortalityDeathsNeverExceedTheAnimalsThatWereThere`. Each fails on the old query.
