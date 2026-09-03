# The Weights page filters by FARM BORN vs PURCHASED

Maintainer decision, 2026-09-01.

## What was asked for

> "in weights we need one more filter as farm born and purchased"

and, when the mapping was put to him:

> "in sales we have loads which are mapped to animals which animals have loads those are
> purchased ... castro 1, 2, 3 in cbe and cpt castro 1 and castro 2 and godel 2 - part 1 and 2
> are purchased only"

The farm both breeds its own kids and buys them in loads. The two grow differently enough that
reading them together answers nothing — on the landing window the purchased kids move at
**79 g/day** and the farm-born ones at **140 g/day**, a difference the unfiltered page averages
away.

## The rule

> **SUPERSEDED SAME DAY — see "The pen-level rule was wrong for a mixed pen" below.** The first
> version of this filter was pen-level, and the maintainer caught it on the first run. The section
> is kept because the reasoning that led to it is the reasoning a future reader will repeat.

**A pen carrying a purchase-load tag is PURCHASED. A pen with no load tag is FARM BORN.**

Origin is a fact about the PEN a load was put into, not about the animal. So a pen's whole-shed
weighs and its individually scanned weighs always land on the same side of the filter, and there
is no third "not recorded" bucket: every weighed pen has an answer.

Like the Sex filter beside it (2026-08-26), this governs the WHOLE page — every KPI, the shed
table, both leaderboards, the load chart, the breed/sex/stage gain cards and the Growth Director
widgets. A page whose cards disagree about which kids they counted has no true number on it.
Selecting Sex and Origin together reports the kids in BOTH.

## Why it needs NO isolation exception

This is the part worth carrying forward. Weighing is ISOLATED and may name no herd table; Sex
needed a recorded, file-scoped exception (`sex_scope.go`) precisely because sex is a fact about an
ANIMAL, so resolving it means resolving a scanned tag to a goat.

Origin needed none. Weighing already owns the shed → load mapping in **`weighing_shed_load_tags`**
(migration 000131), authored inside weighing so the load chart on this same page would need no
procurement table. `origin_scope.go` therefore reads only:

| table | why it is allowed |
|---|---|
| `weighing_campaign_sheds`, `weighing_campaigns`, `weighing_observations` | weighing-owned |
| `weighing_shed_load_tags` | weighing-owned |
| `shed_partitions` | one of the four permitted ORG tables |

and reads **no** `goats`, `goat_identifiers`, `goat_shed_partitions` or `procurement_load_goats`.
`make weighing-free-flow-guard` passes unchanged and the exempt-file list did not grow.

Two other sources were considered and rejected:

- **`goats.origin_type`** — blank on 55% of the live herd (908 of 1,649) and on 226 of the 452
  individually weighed animals, with ZERO weighed animals marked `procured`. A filter built on it
  would have shown an empty Purchased tab.
- **`procurement_load_goats`** — per-animal, so it drags the herd register onto a weighing read
  path. It agrees with the load tags (the seven named pens are 100% load-mapped in it), but it
  buys nothing the pen-level tag does not already answer, at the cost of an exception.

## Three narrowings, each load-bearing

1. **ONE PEN, TWO SPELLINGS.** Channapatna's Godel 2 - Part 1 is weighed under both a legacy alias
   location literally NAMED `Godel 2 - Part 1` (11 buckets, and where the load tag was authored)
   and the modern shape, location `Godel 2` carrying label `Part 1` (1 bucket). Matching on
   `location_id` alone puts eleven of that pen's buckets in Purchased and the twelfth in Farm born
   — the same pen on both sides of the filter, its weighs missing from whichever side the reader
   is on. Resolution goes through `shed_partitions.alias_location_id` (migration 000239), the
   canonical mapping, and never by pasting the shed name and label back together: that string
   surgery is the OL-3/OL-7 defect class and would also collide Coimbatore's `Godel 2 - Part 1`
   with Channapatna's, which is a different pen holding a different load. The catalog stores the
   scrubbed key `1` while the bucket carries `Part 1`, so both sides reduce to the same key.

2. **AT LEAST ONE TAG, NOT EXACTLY ONE.** The load CHART on the same page drops a shed carrying two
   loads (`HAVING count(*) = 1`), because one shed average cannot be split between two suppliers.
   This filter asks a strictly coarser question — was this pen bought — and both of Channapatna's
   Mandela 1 - Part 1 tags (loads 100 and 101) answer yes. Copying the chart's rule would file a
   pen the farm demonstrably bought under Farm born: a wrong answer, not a cautious one.

3. **NO AGREE-OR-NEITHER RULE.** The Sex filter must ask whether a pen's residents agree with each
   other, because the register can show a mixed pen, and claims a lump-sum bucket only when they
   do. A load tag is a fact about the pen itself, so its residents cannot disagree.

## Defaults

`origin` absent means EVERY kid — deliberately unlike `sex`, whose absence means MALE. There is no
"the number the farm cares about" side here, and defaulting to one would hide half the herd from a
reader who never chose. That also means the bar's own blank option IS this filter's All, so unlike
Sex it does not carry a second explicit `all` value.

An unknown value is REJECTED (400), never ignored: silently widening a filter shows a reader more
kids than the heading they are reading says.

## What the data looks like today

All seven whole-shed pens are purchased and every individually scanned pen except
Mandela 1 - Part 1 is farm born, so Purchased is essentially the lump-sum half of the page and
Farm born the scanned half. That is what the farm's records say, not something the filter imposes.
On the local STG mirror the two halves add up exactly: 382 + 497 = 879 animals, 55 + 11 = 66 sheds.

## Where it lives

- Rule: `backend/internal/weighing/adapters/postgres/origin_scope.go`
- Shared scope shape both cohort filters return: `.../report_scope.go` (`ReportScope`,
  `IntersectScopes`). A third cohort filter should mean one more resolver and no edit to any read.
- Pinned by `TestOriginScopeClaimsBothSpellingsOfOneLoadTaggedPen`,
  `TestOriginScopeClaimsAPenHoldingTwoLoads`,
  `TestIntersectScopesTreatsUnselectedAndEmptyDifferently` and
  `TestIntersectScopesKeepsPartitionsOfOneShedApart` — each mutation-tested when written
  (dropping the alias resolution, copying the chart's one-load rule, inferring "unset" from an
  empty list, and intersecting the two bucket arrays independently each turn one red).

---

# Road to sale counts WHOLE-SHED PENS too

Maintainer decision, 2026-09-01 (same day, separate ask): *"in this add lumpsum value also"*.

## What changed

The Growth Director's **Road to sale weight** board read `weighing_observations` alone, so it
answered "where does every kid sit on the way to 30 kg" from scanned tags only — **501 kids while
555 more sat in nine pens it could not see**. It now has two arms:

- a SCANNED kid contributes **itself, at its own weight**;
- a WHOLE-SHED pen contributes **all of its animals, at the pen's average**, which is the only
  weight that pen has.

The two are disjoint by construction: `weighing_category` is fixed when a bucket is created and the
write path fills exactly one of the two tables, so no animal is counted twice.

This is the same shape the daily-gain headline already carries from 2026-08-26, extended to the
board that shows the distribution behind it.

## Pens count in MOVEMENT too, and the trade is real

Asked directly whether a pen whose average crossed a band boundary should count its animals into
*moved up / held / slipped back*, the maintainer said **yes**. So a pen creeping from 24.9 to
25.1 kg reports every one of its kids as having moved up a band, and a pen's average also moves when
animals enter or leave it. That coarseness is accepted deliberately rather than describe the herd
from the third of it weighed one by one — the identical trade already recorded for daily gain.

**This is NOT the same call as the losing-animals list**, which stays scanned-only. There the output
NAMES individual animals, and a shed average cannot name one.

## The Over 30 kg / Over 35 kg cards take the same trade (2026-09-03)

Maintainer decision, 2026-09-03: *"in 30 kg and 35 kg above include lump-sum also"*.

The two sale-threshold cards on the Weights page (`at_or_above_30kg`, `at_or_above_35kg` on the
shed-weights summary) had stayed scanned-only with their own narrower denominator
(`threshold_basis_animals`, "190 weighed one by one"), on the reasoning that a pen average cannot
say how many of its animals cleared a line. That reasoning still holds, and the answer is the one the
band board already gave: a whole-shed pen is counted **whole or not at all** at its latest average.
A pen averaging 31 kg puts every one of its animals over 30 kg and none over 35 kg. The denominator
widens with the counts and now equals `animals_weighed`; the field stays on the wire so the counts
always travel with their own basis. Pinned by
`TestShedWeightsSaleThresholdsCountWholeShedPensAtThePenAverage`.

## Wire changes, and why the renames were part of the fix

| was | is | why |
|---|---|---|
| `WeightBand.identity_count` | `animal_count` | it counts animals now, not tags |
| `BandMovement.pair_identities` | `pair_animals` | same |
| — | `lump_sum_animals`, `lump_sum_pens` | how much of the board is the coarser measure |
| — | `total_animals` | what the bands add up to |

`total_identities` / `matched_identities` / `unmatched_identities` keep their meaning **exactly**:
they answer "did this tag resolve in the register", which a pen carrying no tag cannot be asked.
Widening them would report hundreds of kids as unmatched when nothing about them was unmatched.

Renaming was part of the change rather than tidying after it: a field named for identities that
returns animals is the same trap as `median_adg_g_per_day` returning a mean (2026-08-26), and it is
the reader of the *next* change who pays for it. `pair_identities` on FairFight, SlowGrowth and the
feed rows is untouched — those statistics stay individual-only.

## Two rules that would silently corrupt the numbers

1. **`withdrawn_at IS NULL` on the pen arm.** Live-row uniqueness on `weighing_shed_observations` is
   a PARTIAL index (000067), so a reopened bucket keeps its superseded rows — and a superseded row
   can be NEWER than the live one. A newest-wins pick without the predicate selects a weight the farm
   has already retracted and moves a whole pen into the wrong band.
2. **A filtered `sum` over no matching rows is NULL, not 0.** Without `COALESCE` the read fails
   outright on the ordinary case of a farm that weighed nothing by the pen. This was caught by the
   existing fixtures, not by review.

## The chart shows all six bands

`Road to sale` was drawn in the 150px `wbars-short` scroll box, which fits four rows. That was
survivable while the top two bands were empty; the pens put 218 kids into 30-35 and 35+, and bars a
reader has to scroll to find no longer visibly add up to the head count above them. The card uses
`tall`, where all six fit without scrolling.

Pinned by `TestGrowthDirectorRoadToSaleBandsMovementAndTrust`, whose fixture carries a third,
**newer** withdrawn pen row precisely so the `withdrawn_at` predicate is load-bearing rather than
merely present — mutation-tested by dropping the predicate (the pen moves band) and by counting a
pen as one animal instead of its head count.

---

# The pen-level rule was wrong for a MIXED pen

Maintainer correction, 2026-09-01, within an hour of the above shipping:

> *"whole mandela 1 - part 1 are not purchased only few are purchased in that right??"*

He was right, and the evidence is unambiguous:

| park | pen | live | bought | |
|---|---|---|---|---|
| CBE | Castro 1 / 2 / 3 | 63 / 74 / 63 | 63 / 74 / 63 | all bought |
| CPT | Castro 1 / 2 | 31 / 32 | 31 / 32 | all bought |
| CPT | Godel 2 - Part 1 / 2 | 38 / 39 | 38 / 39 | all bought |
| CPT | **Mandela 1 - Part 1** | **13** | **4** | **mixed** |

The pen-level rule is exactly right for the seven pens the maintainer originally named — every
resident came off a load — and wrong for the one he did not, which is the one he spotted. It filed
all 12 of Mandela's individually scanned kids as purchased when only 4 came off a load.

## The corrected rule

**Per animal where the evidence allows it; per pen only where it does not.**

- A **scanned** weigh carries a tag, so it is claimed through the animal that tag resolves to.
  `procurement_load_goats` is the only table that says which animal came off which load.
- A **whole-shed** weigh carries no tag, so it is claimed through its pen, and ONLY when every live
  resident agrees: all bought, or none. A **mixed pen is claimed by NEITHER side** — one average
  weight cannot be divided between two cohorts, and claiming it whole is the same defect one grain
  up. This is the identical agree-or-neither shape the Sex filter uses for a mixed-sex shed.
- A tag that resolves to **nothing** is claimed by neither side. It is still recorded and still
  counted unfiltered; it simply cannot answer where the animal came from. So the filtered halves
  need not add up to the unfiltered total, and that gap is honest rather than missing data.

On the maintainer's own data the correction moved Mandela's contribution from 12 purchased kids to
**4**, with the rest reading farm born.

## This is what made it a recorded exception

The pen-level version needed none — it read only weighing-owned tables. The per-animal version
reads `goat_identifiers`, `goats`, `goat_shed_partitions` and `procurement_load_goats`, so
`origin_scope.go` becomes the FOURTH named entry in `HERD_JOIN_EXEMPT_FILES`. It buys accuracy the
pen-level rule cannot reach at any price: a mixed pen has no correct pen-level answer.

**The guard's table allowlist is now keyed PER FILE**, which it was not before. A single shared
table set would have handed `procurement_load_goats` to the census and demographics files too,
neither of which has any business asking where an animal was bought. `procurement_load_goats` is
legal in `origin_scope.go` and still a finding in `sex_scope.go` — asserted by an adversarial
self-test that names a genuinely exempt sibling file, and mutation-tested by widening that
sibling's list.

## A labelling defect this surfaced

Asked why the KPI strip and Road to sale disagreed (481 vs 482 purchased; 153 vs 174 farm born),
the answer was that they count different populations: the strip counts kids **weighed twice** — the
population every gain figure beside it is computed from — while Road to sale counts every kid
weighed at all. The gap is exactly the kids weighed once.

Both were correct; the labels were not. `kpi.kids.split.total_sub` read "total in the selected
period", which is a plain head count, so the screen carried two near-identical sentences over two
different denominators with nothing to tell them apart. It now reads **"weighed twice in the
selected period"**. No number moved.

Pinned by `TestOriginScopeSplitsAMixedPenPerAnimalAndRefusesToClaimItWhole` (mutation-tested by
claiming a mixed pen whole — it goes red),
`TestOriginScopeClaimsAWholeShedPenWhenEveryResidentAgrees` (without which the refusal above would
be indistinguishable from a filter that never claims a pen) and
`TestOriginScopeCountsAnAnimalOnTwoLoadsOnce`, whose comment records honestly that today's `EXISTS`
is a semi-join and so the test passes with the `DISTINCT` removed — it pins the behaviour, not the
clause.

## The movement tiles show their own denominator

Same review pass: the maintainer added up the Road to sale tiles and found `74 + 404 + 0 = 478`
against a head count of `482`. Both were right — band movement needs a previous weigh to compare
against, so it counts a strictly smaller population than "kids weighed in this period", and the
four missing kids were weighed once — but the card never showed the number the three tiles summed
to, which reads as an error.

`pair_animals` was already on the wire, so it is now a tile: **"of them weighed twice, so able to
move a band"**. The card is a 3x2 grid whose two rows each self-check —
`bands = kids weighed`, `moved up + held + slipped back = weighed twice`.

The band chart also stopped being a fixed-height box. It has a FIXED SIX rows and no chart toggle,
so `tall` (300px) only ever left dead space under the last bar; `wbars-bands` sizes to its content
with a `max-height` for safety, and the empty state keeps a `min-height` so it cannot collapse onto
the caption.
