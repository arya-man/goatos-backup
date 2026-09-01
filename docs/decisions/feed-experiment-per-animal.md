# Experiment feed is authored PER ANIMAL

**Maintainer decision, 2026-09-01.**

## What changed

An experiment pen's feed is still hand-entered cell by cell, on its own screen, with no ration
grid involved. Only the QUESTION each cell answers changed:

```text
before   absolute_kg     kg for the WHOLE pen        fed as-is, head count never multiplied
after    grams_per_head  grams for ONE animal        fed as grams x the pen's LIVE head count
```

Everything else about the experiment workflow is untouched. Membership in `feed_experiment_config`
is still what puts a pen on the workflow; the arm is still what prints in the shed-tag column; the
authored cells are still the complete list of what the pen is fed; a missing row still means "not
part of this experiment" rather than a gap.

## The four narrowings the maintainer chose

1. **The multiplier is the LIVE head count** — the same one the ration grid uses, so an experiment
   pen and a normal pen answer "how many animals am I feeding" from one source. The stored
   `feed_experiment_config.head_count` is not read, written or shown anywhere any more (maintainer
   instruction, same day: *"use live only, forget recorded"*); it survives only as provenance for
   what the conversion divided by. The Feed Config screen shows the pen's live population, resolved
   per request from the herd register.
2. **No shed factor.** An experiment quantity is grams x head count and nothing else. The pen's row
   in `feed_shed_factors` is deliberately not consulted, so `ShedFactor` is nil on an experiment
   item rather than an un-applied `1.0`.
3. **The existing values are SEEDED across, not left behind** (maintainer decision the same day,
   REPLACING the original "no previously authored data changes" instruction). Migration `000238`
   converts every legacy cell in place: `grams = kg x 1000 / the row's own head_count`. See
   "Seeding the rates from what the farm already feeds" below, which is where the consequences are.
   The two-column shape still stands, because a row the conversion cannot derive (no head count)
   stays on the legacy basis, and the basis is stored per CELL so a pen can hold both.
4. **The 14:00 correction still does NOT reopen a packed experiment pen.** This one is a trade, and
   it is recorded as one. The exemption's original reason was arithmetic — an absolute pen total does
   not move when the head count does — and that reason is now gone. The maintainer was shown the
   consequence and kept the exemption: an experiment pen whose count moves between packing and the
   correction keeps a video proving the pre-correction quantity. Normal pens are unaffected and still
   reopen. See `feeddirection/app.reopenPackingForCorrection`.

## Seeding the rates from what the farm already feeds

Migration `000238` converts every cell still on the legacy basis:

```text
grams_per_head = trunc(absolute_kg * 1000 / the pen's LIVE resident count, 3)
```

**The denominator is the LIVE herd**, not the stored `head_count`. The two disagree for 15 of the 34
live pens, by as much as 7 recorded against 16 actual, because nothing has maintained the stored
figure since somebody typed it.

The consequence is the good one: **every pen keeps being fed exactly what it is fed today**, because
the rate back-multiplies by the number it was divided by. From tomorrow the total follows the
animals, which is the point of the per-animal basis, but the conversion itself moves nothing.
Dividing by the recorded count would instead have re-based those 15 pens on the spot — `Godel 1 -
Part 7` up 129%, `Godel 2 - Part 3` down 78% — on the strength of a number nobody keeps up to date.

**A pen with no live animals is left on the legacy basis**: no denominator, no rate, and the stored
count is not used as a stand-in. An empty pen is fed nothing either way, and the cell is still there
to convert once animals arrive and someone re-authors it.

**The rate is TRUNCATED, never rounded to nearest**, and that is a correctness rule rather than a
formatting choice. The generator rounds a pen's session quantity UP to a packable 0.1 kg, so a rate a
hair ABOVE exact lifts an UNCHANGED pen's sheet by a whole notch: 8 kg / 31 animals rounds to
258.065 g, back-multiplies to 8000.015 g, and turns a 4.000 kg session into 4.100 kg. Truncating
keeps the derived rate at or a hair below exact (at most 0.001 g per animal), so a pen whose
population has not moved is fed exactly what it is fed today.

`feed_experiment_basis_conversions` records every conversion's inputs (pen total, denominator,
derived rate), which makes the arithmetic auditable off one `SELECT` and the `Down` path exact
rather than a re-derivation that lands a few grams away.

The conversion touches only `quantity_basis = 'absolute_kg'`, so it is idempotent and can never
overwrite a cell already authored in the app.

## Nobody types a head count any more

The screen's count column is the pen's live population (`live_head_count`, resolved by the backend in
the list read), and the head-count input is gone from the cell editor, the add-item form and the pen
enroller. The write path no longer accepts one, and it does not touch the stored column: blanking it
on an ordinary quantity edit would destroy the conversion's provenance for nothing.

## The workbook seeder converts too, and no longer overwrites hand authoring

`seed-feed-ration` reads the experiment workbook, which still records kg per pen, and divides by the
pen's LIVE population on the way in — the same truncated arithmetic `000238` applies — so a migrated
database and a freshly seeded one land on identical rates.

The workbook's own head count is used ONLY where the herd register cannot answer: a pen with no live
animals, which on a fresh database is every pen until the goats are loaded. Without that fallback the
ORDER of two seed commands would decide whether the feed config lands at all. The seed prints how
many pens fell back, because a rate derived from a workbook count is worth re-seeding once the herd
exists.

`feed_experiment_config.source` (`workbook` | `app`) is what keeps the seeder off a hand authoring.
It re-asserts its own rows on every run, as it always has; a cell someone corrected on `/feed/config`
is stamped `app` by the write path and is skipped. Note the flip side, which predates this change: a
re-seed still re-asserts the workbook over any WORKBOOK-sourced row that has since been edited
elsewhere, so `seed-feed-ration` is not a safe thing to run casually against STG.

## Why two columns and not one

`quantity_basis` is a stored fact, never inferred from whichever column happens to be non-null, and
the table's pairing CHECK guarantees exactly one figure is present. The two readings of the same
number differ by the pen's ENTIRE POPULATION — 350 for a pen of 63 is either 350 kg or 22.05 kg — so
there is no safe default: the generator BLOCKS a cell whose basis it cannot read rather than picking
one. Every write authors the per-animal basis; there is no route that writes a pen total any more,
and editing a legacy cell moves it over in the same statement that clears the old figure.

## The one honest gap

The Feed Config quantity filter compares GRAMS PER ANIMAL. A legacy pen-total cell is claimed by
neither side of it and drops out while that filter is on, because its number is kg for a whole pen
and cannot answer a per-animal question. It is still listed, and still counted, in the unfiltered
view. This is the same shape as the Weights sex filter's unresolvable tags: a gap that is stated
rather than papered over with a unit conversion nobody authored.

## Where it lives

| Layer | File |
| --- | --- |
| Schema | `backend/migrations/postgres/000237_feed_experiment_grams_per_head.sql` |
| Seeded conversion | `backend/migrations/postgres/000238_feed_experiment_seed_grams_per_head.sql` |
| Generator | `feeddirection/domain.ExperimentPlanner` (`experimentItem`, the basis switch) |
| Snapshot read | `feeddirection/adapters/postgres.loadExperiments` |
| Write path | `feedconfig` service + repository (`UpsertExperimentConfig`, `...Batch`) |
| Workbook seed | `backend/cmd/seed-feed-ration` — writes the legacy basis, and SKIPS a cell the app has re-authored |
| Screen | `/feed/config` experiment section; copy in `adminui/app/service.go` |

Pinned by `TestFeedExperimentBasisSeedDerivesRatesFromTheLiveHerd` (its fixture makes the stored
count disagree with the herd on purpose; mutation-tested twice — swapping `trunc` for `round`, and
reading `head_count` instead of the live count, each turns it red),
`TestExperimentHeadCountIsTheLivePenPopulation`,
`TestWorkbookGramsPerHeadTruncatesToTheStoredScale`,
`TestExperimentStrategyMultipliesGramsPerHeadByTheLiveHeadCount`,
`TestExperimentPenMixingBothBasesReadsEachCellOnItsOwnBasis`,
`TestExperimentCellWithUnknownBasisBlocksRatherThanGuessing`, and the retained legacy test
`TestExperimentStrategyUsesAbsoluteKgAndIgnoresHeadCount`.
