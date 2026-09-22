# Pen type is configured, and health problems are read four ways

Maintainer instruction, 2026-09-22. Two halves of one request, recorded together because the
second is why the first had to exist.

## The ask

> Health Analytics needs a **Health problems** section: the total number of health problems over
> a selectable window — last 30 / 90 / 120 days, or everything since the reforms — and that same
> total shown as four separate graphs: month-wise, breed-wise, pen-wise split into **elevated**
> and **non-elevated**, and age-wise.
>
> And I need to configure whether a pen is elevated or non-elevated **on that particular pen**,
> in Items and configurations — not anywhere else.

## Part one: pen type stops being a guess

Nothing stored a pen's type. The Weighing gain-by-breed comparison INFERRED it per request: it
matched the words *elevated* / *crown* / *ground* in a pen's free-text notes and, when the notes
said nothing, fell back to a **hardcoded list of building names** compiled into Go — Gandhi and
Castro are ground, Mandela and Godel are elevated.

Three things were wrong with that, and only the third one is obvious:

1. a pen built after the list was written could not be classified at all;
2. nobody on the farm could correct a pen the list got wrong;
3. and the query's own comment claimed the class "must come from explicit shed metadata; an
   unclassified shed is not guessed from its name", directly beside the code guessing from the
   name. A reader had no way to know which half to believe.

It is now `shed_profiles.shed_type`, set per pen on **Configuration → Items and settings → Pens**
beside that pen's capacity and gender (migration `000385`). One pen at a time, with no farm-wide
or park-wide switch, because the farm mixes both kinds inside one park.

**NULL is a real and expected state** — "nobody has said yet". An unclassified pen is reported as
unclassified and is never assigned a side. A pen the farm has not typed must not silently land in
one half of a comparison and move the average there.

**The inference is not kept as a fallback**, and that is deliberate rather than tidying. A
fallback would make CLEARING a pen's type on screen do nothing: the guess would immediately
re-assert the answer the farm just removed, and the screen would look broken. Migration `000385`
wrote the inference's answers into the column ONCE, so no chart moved on the day it landed, and
the Pens screen is now where they get corrected. Pinned by
`TestWeightDemographicsReadsTheConfiguredPenTypeAndNeverGuesses` (which is the old name-list test
inverted) and `TestPenTypeBackfillCarriesTheRetiredNameList`.

### One farm concept, one name

The retired second class was keyed `ground` and labelled *Crown/Ground*. It is `non_elevated` and
**Non-elevated** everywhere now — wire key, copy, and OpenAPI enum — so a reader moving between
the Weighing comparison and the Health chart is not asked to work out whether two words mean the
same pens.

## Part two: health problems, one total cut four ways

A **health problem** is one case — one episode of one illness in one animal, the same grain as
`totals.new_cases` and the disease board. An animal treated twice is two problems, because a
relapse is a problem the farm had twice.

The window carries four presets — last 30, 90, 120 days, and **Since the reforms**, which opens
on `2026-08-01`, the first day the Health module recorded anything. A preset is only a from/to
pair written into the URL; the calendar beside the chips writes the same two parameters, so a
preset and a hand-picked range are the same state and the page cannot show one while the chips
claim the other. Whichever chip matches the **served** window reads as selected, so a window the
read clamped or defaulted can never leave a chip highlighted that does not describe the chart
below it.

### Every breakdown sums to the headline

This is the property the section rests on, and the reason each arm carries an explicit bucket for
what is not known — a breed nobody recorded, a pen nobody has typed, an animal with no date of
birth:

- dropping those rows would leave four charts each quietly answering a **smaller** question than
  the headline above them;
- and a reader comparing two bars would be comparing them inside a total that is not the total on
  screen.

Named absence is honest; a silently shorter bar is not. "Breed not recorded" even sorts with the
rest rather than being pinned last: when it is the biggest bar, that IS the finding.

The three cuts are computed in ONE query over one scoped set, stacked with `UNION ALL` and told
apart by a `dimension` column, so they agree by construction rather than by three queries being
kept in step. The fourth cut, by month, is the existing `months` series and is not recomputed —
a second copy could disagree with the first.

Two more narrowings worth carrying forward:

- **Age is taken at the START of the case**, not today. The question is how old the animal was
  when it fell ill; its age today says nothing about a case opened last winter. The bands are
  deliberately identical to the mortality board's, pinned by
  `TestHealthProblemAgeBandsMatchMortality`, because a farm reading "1–3 months" on both is
  entitled to assume they mean the same animals.
- **The breed arm is the only capped one, and the cap is taken AFTER the total.** A farm with
  more breeds than the cap still reads the true count above the chart; summing the visible bars
  to find a headline is the banned read-time rollup, one card wide.

The pen-type and age spines are FIXED, never sorted by size. Two bars that are read against each
other must not swap places between windows, and an empty side must read as a zero rather than
vanishing and making the chart look like the farm only has one kind of pen.

## What is pinned

| Property | Test |
|---|---|
| The pen type is read from configuration and never guessed | `TestWeightDemographicsReadsTheConfiguredPenTypeAndNeverGuesses` |
| The retired name list survives only in the one-time backfill | `TestPenTypeBackfillCarriesTheRetiredNameList` |
| Set, flip, clear-to-NULL, and a refused third value | `TestPenTypeIsConfiguredOnThePenAndClearsToUnclassified` |
| The Pens register offers exactly the two options | `TestPensRegisterOffersExactlyTheTwoPenTypes` |
| Every breakdown sums to the headline | `TestHealthProblemBreakdownsEachSumToTheTotal`, `TestHealthProblemsOneToManyCountsEachEpisodeOnceAcrossEveryBreakdown` |
| The breed cap never moves the headline | `TestBreedCapNeverMovesTheHeadline`, `TestHealthProblemsBreedPaginationCapNeverMovesTheTotal` |
| Park scope narrows every arm together | `TestHealthProblemsParkScopeNarrowsEveryBreakdownTogether` |
| Every case status counts, and the unknowns are named | `TestHealthProblemsStatusMatrixCountsEveryStatusAndNamesTheUnknowns` |
| The age bands match the mortality board | `TestHealthProblemAgeBandsMatchMortality` |
| Empty pen-type sides stay as zeros | `TestPenTypeSpineKeepsEmptySidesAsZeros`, `TestPenTypeOrderIsFixedAndUnclassifiedIsLast` |

Each pin was mutation-tested when written.
