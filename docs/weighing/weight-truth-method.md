# Weight Truth — method, sources and every rule

Companion document to the page mock at [`mock/weight-truth.html`](../../mock/weight-truth.html).

This page answers one question: **when a weighing says an animal got lighter or heavier,
is that the animal, or is it the record?** It classifies every consecutive pair of weighings
in the legacy weighing history and attaches a reason to each.

This document exists so the method can be reviewed and disputed. It states where every
number comes from, every rule, every threshold, and — explicitly — which rules rest on
evidence and which rest on an assumption that has not been verified with the farm.

**Status: draft for review. Not wired to any backend. The page is a static mock built from a
one-off extract.**

**This document has been audited against its own code and data, and was wrong in 26 places.
Those corrections are in §12. Read that section before trusting anything else here.**

---

## 1. Where the data comes from

All of it is BigQuery in the legacy project **`goatos-sheets`**, read with `bq` as
`ravi@mesha.sg`. Nothing comes from the Goat OS database — `goatos-stg` holds no historical
weighings, so the legacy sheet is the only source for this history.

| Source | Used for |
|---|---|
| `weights.weights_db_clean_dev` | every weighing: date, park, shed, shed tag, breed, sex, animal id, average weight, animal type |
| `healthDB.diagnosis_clean_table` | health tickets — disease, symptoms, status, date, per animal |
| `goatsDB.goat_activity_timeline` | birth / purchase / sale / death / abortion, and every shifting with its free-text reason, source shed and destination shed |
| `goatsDB.goat_activity_timeline` (`mother_id`) | kiddings, resolved to the mother |

**Coverage:** 15,834 weighings · 2,099 animals · **2025-07-14 → 2026-07-29** · 56 distinct
weighing dates · parks CBE and CPT.

`animals.sql` returns 2,103 animals over 15,867 weighings; stage 4 (§11) then drops four
junk-id buckets — `No ID`, `No Id`, `No Tag`, `No id` — that the SQL's case-sensitive filter
misses, removing 4 animals and 33 weighings. Of the 56 weighing dates, 53 carry at least one
individual animal pair and 3 (2026-06-09, 2026-06-29, 2026-07-20) appear only as whole-shed
readings.

**No coverage query is committed.** These figures are derived from the extracts in §11 rather
than from a query anyone else can run, which is why they were wrong until the audit.

**Two things the extract could not reach:**

- `weights.weights_unclean_updated` is a Drive-backed external table. The active credentials
  lack the Drive scope, so the raw sheet itself was never read. The parsed native table was
  used instead — same content, already cleaned.
- The `goat_type` column marks animals `ADLUT` on rows dated **August and October 2025 only**
  (37 and 312 change-rows). No row after October 2025 carries it, so every recent finding on
  this page is about animals the source calls kids. **The page's Adult/Kid chip does not say
  this.** It resolves type per animal with `ANY_VALUE(goat_type)` over the whole history
  (`animals.sql`), so 376 animals stay chipped Adult forever and 396 of their changes run to
  2026-07-29 — 19 inside the default four-week window. Filtering to Adult selects animals that
  were *once* typed adult, not weighings of adults.

### Extract queries

| File | What it pulls |
|---|---|
| `sql/enrich.sql` | every consecutive pair where the animal came out **lighter**, joined to health, kidding, abortion and shifting records covering the interval **or falling within a rule-specific lookback before it** — see the Lookback notes in §5a |
| `sql/enrich_pos.sql` | the same for pairs where it came out **heavier** |
| `sql/animals.sql` | per-animal series: first and last weighing, span, overall rate, current park / shed / partition |
| `sql/shedlevel.sql` | whole-shed readings — per shed-day aggregate weight, 146 rows |

**Shed and partition are normalised in `animals.sql` and `shedlevel.sql` only.** There,
`GODEL 1 - PART 3`, `GODEL 1 PART 3` and `Godel 1 - Part 3` all resolve to shed `GODEL 1`,
partition `3`. `enrich.sql` and `enrich_pos.sql` do **no** normalisation — they carry the raw
shed string through as `ANY_VALUE(shed)` and `LAG(shed)`, which is what `classify.py` then
compares. See the `transport_shed_change` note in §5a for what that costs.

**The id filter is inconsistent across the queries.** `enrich.sql` and `enrich_pos.sql` drop
`('', 'No tag', '-')`. `animals.sql` also drops `'NA'`. All are case-sensitive, so `No Tag`,
`No Id`, `No ID` and `No id` survive every one and are removed later by stage 4. They should
share one predicate — `UPPER(TRIM(goat_id)) NOT IN ('', 'NO TAG', 'NO ID', 'NA', '-', 'NIL',
'?')` — which is what `sql/shedlevel.sql` already uses.

**`animals.sql` does not share the change extract's grain.** `enrich.sql` groups one row per
`(goat_id, date)`; `animals.sql` groups per `(goat_id, farm, date, shed_norm, breed, gender,
goat_type)`, so a goat whose farm/shed/breed/sex/type is spelled two ways on one day becomes
two rows, and `COUNT(*) OVER(PARTITION BY goat_id)` counts rows rather than weighing days.
Ten animals have `n_weighings = 2` with `span_days = 0`, and for those the
`FIRST_VALUE`/`LAST_VALUE` windows — ordered only by `date` — pick a winner arbitrarily. Id
1505 is reported as first 45.9 kg and last 38.3 kg, both on 2025-10-15: a 7.6 kg "change"
over zero days.

---

## 2. What a "change" is

One change = **two consecutive weighings filed under the same animal id** — chained across
every park, shed and partition, because the id is the only thing linking them
(`LAG(...) OVER(PARTITION BY goat_id ORDER BY date)`, with `farm` and `shed` carried as
payload). `animals.sql` uses the same key, so a cross-park id collapses into one animal row
under one park and its overall rate spans both. Each change has:

- `days` — calendar days between them
- `delta` — kilograms gained or lost
- `pct` — that delta as a share of the earlier weight
- `adg` — delta ÷ days, in kg/day

**13,141 changes on 1,759 animals.** The other 340 of the 2,099 animals yield no change at
all: 309 were weighed once, 21 have only flat pairs, 10 have every row on a single date. Any
per-animal figure computed against 2,099 is inflated by 19%.

**Two kinds of interval are silently excluded by the extract SQL and appear in no denominator
on this page.** `enrich.sql` ends `AND p.wt < p.pw` and `enrich_pos.sql` ends `AND p.wt >
p.pw`, both strict — so an interval where the two weights are identical exists in neither
file, even though it is a real measurement. And the `w` CTE groups by `(goat_id, date)`, so
two weighings on the same day collapse to their average and never form a pair; nine of the ten
single-date animals in fact carry *different* weights that day (id 1505 is 45.9 and 38.3 on
2025-10-15), which is the same-day-conflict phenomenon, not a flat reading.

Median interval is **7 days** — weighing runs weekly. 75% of intervals are 4–7 days; the
longest is 336 days.

**Period scoping.** The page defaults to the **last 4 weeks**. A change belongs to the period
if its *later* weighing falls inside it. A per-animal rate is measured from the earliest to the
latest weighing **inside** the window; if that span is under 4 days, the last reading before
the window is used as the anchor instead, because below four days gut fill alone outweighs the
signal. Where no usable interval exists the page shows an em dash with the reason, never a zero.

---

## 3. The four outcomes

| Outcome | Meaning |
|---|---|
| **Real change** | The animal genuinely changed — either a record explains it (775 changes), or it matched no rule at all and fell below the growth ceiling (1,776 changes, see §5e) |
| **Bad data** | The number is wrong; the arithmetic of the two readings rules biology out |
| **Not an individual weight** | The row was never about this animal (§4) |
| **Open** | Looks real, nothing on record explains it — needs a person |

Over the full history: **2,551 real · 665 bad data · 9,650 not individual · 275 open.**

---

## 4. The largest finding: most history is not per-animal

**9,650 of 13,141 changes (73%) are one shed figure written against every animal in a block.**

Detection: five or more animals in the same park, shed and date carrying an **identical weight
pair** — same earlier weight, same later weight, to the decimal. That does not happen by chance
at that scale.

Worked example — CBE Ho Chi Minh 1 carries a single weight across **93 animal ids, every week
for months**: 24.9 → 26.0 → 26.3 → 26.3 → 27.6 → 27.9 → 29.7. Another — CPT Mandela 2,
2026-05-25, ids `ID - 86` through `ID - 98`, all recording `23.9 → 11.2 kg`.

**150 of 440 shed-days have exactly one weight pair for every animal weighed.**

These rows are not errors. They are a shed weighed as one and filed per animal. The page treats
them as their own outcome: excluded from every per-animal rate, never counted as flagged.

**This has changed.** Over the last four weeks the count is **zero** — recent weighing is
genuinely per-animal. The practice already improved; the history does not say so.

---

## 5. Every rule

Thresholds are named where they exist. **The 5a–5e grouping is by what each rule rests on, not
by precedence** — do not read it as evaluation order, because the actual order is close to its
reverse. There are two programs, and neither runs these sections in sequence.

**Losses (`classify.py`), first match wins:** `decimal_shift_down` → `digit_transposition` →
`dropped_leading_digit` → `keypad_tens_slip` (5c) → `same_day_conflict` → `v_shape_outlier`
(5b) → `abortion` → `post_kidding` → `illness_treatment` → `illness_shifting` →
`weaning_stage_change` → `breeding_cycle` → `transport_shed_change` (5a) → `identity_drift` →
`within_noise` → `implausible_rate` (5d) → `no_recorded_cause` (5e).

**Gains (`classify_pos.py`), first match wins:** `correction_of_prior_error` (5b) →
`decimal_shift_up` → `digit_transposition` → `added_leading_digit` → `keypad_tens_slip` (5c) →
`same_day_conflict` → `spike_then_drop` (5b) → `compensatory_regain` → `refill_after_move`
(5a) → `within_noise` → `implausible_gain` → `identity_drift` / `above_growth_ceiling` (5d) →
`recovery_after_care` (5a) → `normal_growth` (5e).

`shed_figure_copied` is in neither list: it is an overlay applied after both programs, and it
beats every code above (§11).

Three consequences worth stating plainly:

- **The keypad signatures in 5c — the family this document flags as unverified — are tested
  before any joined record.** A doe with a recorded kidding whose two readings happen to share
  a digit multiset is filed `digit_transposition` and never reaches `post_kidding`. On the
  current data no row with a joined record is actually pre-empted, but that is a property of
  the data, not of the order.
- **`transport_shed_change` (0.6) is tested before `within_noise`.** 67 of the 202
  transport-shrink rows, and 14 of the 66 `refill_after_move` rows, are inside the noise band
  and would otherwise read as noise. That contradicts §12's own "gut fill is not directional"
  reasoning and is an open decision, not a settled one.
- **`recovery_after_care` (5a) runs after the 5d growth ceilings.** Nine gains whose shifting
  record names care are filed `above_growth_ceiling` (6) or `implausible_gain` (3) instead.

### 5a. Rules grounded in a joined record

These fire because a record exists that covers the interval, or falls inside a lookback window
before it. They are the defensible ones.

**The lookbacks are asymmetric.** Health records join `BETWEEN DATE_SUB(prev_date, INTERVAL 7
DAY) AND cur_date`; kidding and abortion join with a 14-day pre-window; shifting joins the
exact interval (`BETWEEN prev_date AND cur_date`, inclusive of the earlier weighing day, so a
move recorded that day counts into an interval it already anchors). One event can therefore
legitimately appear as context for two consecutive weekly intervals.

| Rule | Fires when | Confidence |
|---|---|---|
| `post_kidding` | a birth event names this animal as mother. **Lookback 14 days.** Cap 20% of body weight if the kidding falls inside the interval, 12% if it predates it | 0.95 / 0.7 |
| `abortion` | an abortion event, loss ≤ 20%. **Lookback 14 days, and unverifiable** — the extract exposes `abortion_n` but no abortion date, so the rule cannot tell an abortion inside the interval from one 13 days before it | 0.9 |
| `illness_treatment` | a health ticket for this animal. **Lookback 7 days, and unchecked** — the rule tests only that a ticket exists, never when | 0.9 |
| `illness_shifting` | the shifting reason names fever, health, weak, ICU, treatment | 0.8 |
| `weaning_stage_change` | a K-stage move (`K0→K1`, `K1→K2`, `K2→K3`, `→Mother`) in the window | 0.8 |
| `breeding_cycle` | the shifting reason names mating, breeding, delivery, sponge | 0.6 |
| `transport_shed_change` | the raw shed **string** changed, loss under 12%. **Not the same as the shed changing.** `classify.py` compares `e['shed'] != e['pshed']` and neither string is normalised (§1), so `GODEL 1 PART 2` → `GODEL 1 - PART 2` counts as a move. **19 of the 202 rows reported as transport shrink are pure spelling differences** — 17 hyphen/space variants inside GODEL 1, plus `YASHODA 2` → `YASHODA-2` and `YASHODA 1` → `YASHODA-1`. The comparison must be separator-insensitive across the whole shed name, not only the `PART` suffix, or the two YASHODA rows survive the fix. `refill_after_move` inherits the contamination | 0.6 |
| `compensatory_regain` | a gain following a documented illness, kidding, abortion or weaning | 0.8 |
| `recovery_after_care` | a gain in a window whose shifting record names care | 0.7 |
| `refill_after_move` | a gain within 21 days of a shed move, under the growth ceiling — gut fill, not new tissue | 0.7 |

Two of these cannot check their own window. `classify.py` gates `illness_treatment` on
`health_n > 0` or a regex over the health text, with no date test at all; and the SQL exposes
no abortion date. §12 records that this exact problem was found and fixed for `post_kidding`.
It was not fixed for the other two.

### 5b. Rules grounded in the number series itself

No external record needed; the readings around the change prove it.

| Rule | Fires when | Confidence |
|---|---|---|
| `v_shape_outlier` | fell below 90% then recovered to ≥97% of the earlier weight at the next weighing. **Neither the rule nor the phrase "an animal cannot regain that" constrains how long the rebound took.** The test is `nw >= pw*0.97 and cw < pw*0.9` and nothing more. Median gap to the rebound is 7 days, but 12 of 76 rows took over 30 days and the longest took 179 — over which a 10% regain is ordinary growth. It should fire on the *rate* of the rebound against the §5d ceilings | 0.85 |
| `spike_then_drop` | rose above 112% then fell back to ≤105%. **Same defect, same absence of a time bound**: 3 of 69 rows took over 30 days, the longest 52. The reversing leg is a loss, so the right gate is the 0.30 kg/day loss threshold | 0.85 |
| `same_day_conflict` | two different weights filed against one id on one day, more than 2 kg apart | 0.85 |
| `correction_of_prior_error` | the earlier reading was already flagged — this is the number returning, not the animal growing. Suppressed when the current reading is itself contradicted. **The 0.9 is asserted fresh, not inherited.** Of 144 such changes, 143 descend from a parent flag weaker than 0.9: `v_shape_outlier` 73 (0.85), `implausible_rate` 52 (0.7), `same_day_conflict` 9 (0.85), `identity_drift` 8 (0.75). A chain's confidence is a product, not a restart. This is also the **first** check on the gain path, so a self-evident 0.95 `decimal_shift_up` is currently demoted to a derived 0.9 | 0.9 |
| `shed_figure_copied` | see §4. **Not one branch among these — an overlay applied after both classifiers, overriding whatever they produced.** The 9,650 rows it claims were first classified `within_noise` (3,385), `normal_growth` (3,085), `no_recorded_cause` (953), `identity_drift` (558) and `correction_of_prior_error` (547). It is the only producer of the fifth verdict value `copied` | 0.9 |

### 5c. Rules that assume a keypad — **unverified**

These are standard data-entry signatures. They all assume the operator **types** a number. If
weighing is a scale reading written on paper, or an RFID scan with an auto-captured weight,
they are meaningless. **This has not been confirmed with the farm.**

| Rule | Fires when | Confidence |
|---|---|---|
| `decimal_shift_down` / `decimal_shift_up` | the reading is one tenth or ten times the previous one | 0.95 |
| `digit_transposition` | both readings use the same digits in a different order, more than 1.5 kg apart | 0.9 |
| `dropped_leading_digit` / `added_leading_digit` | the reading is the previous one with a digit removed from or added to the front, more than 3 kg apart | 0.85 |
| `keypad_tens_slip` | exactly 10.0 kg apart within 21 days | 0.7 |

Together these account for **15 of 13,141** changes as shipped. (A raw tally of the classifier
outputs gives 31; 16 of those are overridden by the `shed_figure_copied` overlay before they
reach the page.) Removing the family would cost almost nothing; it is kept because the
individual cases are convincing, and flagged here because its premise is not.

### 5d. Rules resting on a threshold I chose

| Rule | Threshold | Where the number came from |
|---|---|---|
| `implausible_rate` | loss faster than 0.30 kg/day with nothing on record | my estimate |
| `implausible_gain` | gain above the hard ceiling | see below |
| `above_growth_ceiling` | gain above the realistic ceiling but below the hard one — flagged **Open**, not condemned. **Except for pen-slot and `ID - n` ids**, which `identity_drift` claims first inside the same band and condemns as Bad data at 0.75. 13 such changes | see below |
| `within_noise` | ≤3% of body weight, or ≤6% when the two weighings are 3 days or fewer apart, where gut fill alone outweighs the signal | my estimate — assumed bands, not measured from this scale; no repeatability or calibration study exists |
| `identity_drift` (loss) | a pen-slot or written-label id **and** loss faster than 0.30 kg/day. 37 changes | magnitude gate added after review |
| `identity_drift` (gain) | a pen-slot or written-label id **and** a gain in the same band as `above_growth_ceiling`. 13 changes | inherits the growth ceilings; moves when they move |

**Growth ceilings**

| | Realistic ceiling | Impossible |
|---|---|---|
| Kid | 350 g/day | 600 g/day |
| Adult | 150 g/day | 300 g/day |

**Which ceiling an animal gets.** A string test on the `goat_type` column:
`(goat_type or '').upper().startswith('ADL')`. The only values present are `KID` (12,804
changes) and `ADLUT` — a misspelling of ADULT — (349 changes), plus one NULL that falls through
to the kid ceiling. So the kid ceiling judges 12,805 of 13,154 changes and the adult ceiling
judges 349, or 2.7%. **The argument about these numbers is almost entirely an argument about
350/600.**

**These ceilings apply to gains only.** `classify.py` never reads `goat_type`, so the loss
threshold `implausible_rate` is a single 0.30 kg/day gate for kids and adults alike.

**Observed kid gain rates, so the line can be seen rather than argued about** (n = 8,758 kid
gains): p50 0.123 · p90 0.393 · p95 0.55 · p99 1.23 kg/day. A 350 g/day ceiling cuts at roughly
p87. Lowering it to 250 g/day moves about 745 changes — 21.7% of kid gains sit above 250 g/day
against 13.2% above 350. Lowering it also pulls more gains into `identity_drift`, not only into
review.

**These are my estimates, not a veterinary source and not your records.** They need sign-off
before this ships.

**Age-banded ceilings are not derivable from this extract.** `enrich.sql` computes
`bd.birth_date` and `age_days`, but neither classifier reads either field, and a birth date
resolves for only 1,503 of 13,154 changes (11.4%) — 40 of the 349 adult changes. A two-week kid
and a six-month kid are judged by the same number.

The herd includes sheep (`ANANTAPUR SHEEP`, a large share of rows) alongside goats and crosses.
The ceilings do not distinguish them.

### 5e. The two defaults — what a change gets when nothing matched

Neither appears above, and between them they decide **1,962 of 13,141** changes. Both are the
last thing their classifier tries.

| Rule | Fires when | Confidence | Changes |
|---|---|---|---|
| `normal_growth` | a gain reached the end of `classify_pos.py` — no keypad signature, no series contradiction, no joined record, and below the realistic growth ceiling | 0.8 | 1,776 (70% of the Real-change bucket) |
| `no_recorded_cause` | a loss reached the end of `classify.py` — nothing matched and the rate is inside the 0.30 kg/day gate | 0.4 | 186 (68% of the Open bucket) |

`normal_growth` is a default, not a finding. No record was consulted and none was found; the
only thing between a gain and this label is the kid growth ceiling in §5d, which is why that
number is the biggest lever on the page. `no_recorded_cause` carries 0.4 for the same reason
stated honestly: nothing was established. **A 0.8 confidence on a verdict assigned without any
corroborating evidence is itself open to challenge** — see §10.

---

## 6. Identity

Animal ids come in five shapes, and they are not equally trustworthy:

| Shape | Rows | Trust |
|---|---|---|
| RFID ear tag (14+ digits) | 5,733 | scanned; but see the cross-park note in §9 |
| Numbered ear tag | 4,255 | trustworthy unless re-issued |
| Pen slot label (`Godel 2 - Part 8 - ID 3`, `CBE-126-ID 14`, `Yashoda 4 - ID 2`) | 4,057 | **names a place, not an animal** |
| Written label (`ID - 104`) | 1,733 | **re-used between rounds** |
| Free text | 56 | no fixed format |

These sum to 15,834, the coverage figure in §1.

**The pen-slot count above is not the count the classifier uses.** Both scripts test for a pen
slot with `re.search(r'(?i)part.*id *\d+', goat_id)`, which requires the literal word PART.
That misses 1,539 rows of the form `CBE-126-ID 14` and `Yashoda 4 - ID 2` — pen slots by every
other criterion — and files them as free text. Because `identity_drift` only fires for
`positional_pen` and `ID-n`, it has never been evaluated against those 1,539 rows. The table
above describes the data as extracted; §3 describes it as currently classified, and the two
will not agree until the test is widened in both classifiers. Widening needs care:
`positional_pen` is tested before `ID-n`, so a broadened pattern will swallow the bare
`ID - 104` shape and empty that bucket unless the `ID-n` full-match is tested first.

A pen-slot label is a position in the shed, written fresh each round. Two weighings under one
such label are very likely two different animals, so the difference between them is not a
change at all. That is what `identity_drift` catches — but only above a magnitude gate, because
below it an ordinary loss on a badly-labelled animal is still an ordinary loss. There are two
different gates, one per direction; see §5d.

---

## 7. Counting weight that never moved

"Phantom kilos" is the weight the herd appears to have gained or lost because of bad readings.

One bad reading spoils **two** intervals — the one into it and the one out of it. Counting both
double-counts the same typo. Each bad reading is therefore attributed once, to the
higher-confidence interval; the mirror interval keeps its flag (it is still untrustworthy) but
contributes zero kilos. **71 mirror intervals** are suppressed this way — 61 from
`v_shape_outlier`, 9 from `same_day_conflict`, 1 from `correction_of_prior_error`. All 71 are
Bad-data intervals; no Real, Not-individual or Open interval is ever suppressed. The count is
checkable from the page: it is exactly the number of embedded changes with `ph == 0`.

---

## 8. What this method cannot tell you

- **Who entered a reading, or on what device.** The legacy weighing sheet has no operator
  column and no capture-mode column. Every statement about *how* a wrong number got there is
  inference from the number itself.
- **Whether an animal that is losing weight is sick.** It can only say whether a health ticket
  exists. Absence of a ticket is not absence of illness — that is what "Open" means.
- **Whether an animal is a kid or an adult, consistently.** `goat_type` is unstable per animal:
  62 goats carry both values across their own weighings (goat 158 is `ADLUT` on 2025-10-15 and
  `KID` on every 2026 row). The page chip reads the per-goat `ANY_VALUE`; `classify_pos.py`
  reads the per-row value to pick the growth ceiling. They disagree on 108 gain rows — 51
  chipped Adult but judged against the kid ceiling, 57 chipped Kid but judged against the adult
  one — and 50 sit in the 0.15–0.60 kg/day band where the two ceiling sets give different
  verdicts. Losses are unaffected: `classify.py` never reads the column.
- **Whether two weighings under one id are in fact one animal, across parks.** 24 ids appear
  under both CBE and CPT, including 15-digit RFIDs — the shape §6 grades as trustworthy. Each
  such pair is chained into a single series and emitted as a change with a day gap and a rate.
  Several share identical endpoint weights (41.28 kg on 2025-10-15, 24.0 kg on 2026-06-05),
  which is the `shed_figure_copied` signature. These 24 should be flagged as cross-park pairs
  and excluded from every gain figure, not classified — whether they are id collisions or
  genuine transfers is exactly what is unknown.
- **Anything about adults after October 2025.** The source stopped typing them.
- **Anything about whole-shed sheds.** A whole-shed reading has weight but can never have a gain
  per animal, and is never counted into a gain figure.

---

## 9. Open questions for review

1. **Which resolution of `goat_type` is authoritative,** and does the farm's own record support
   any animal being re-typed from ADLUT to KID? Until that is settled, type assignment is a
   larger lever on "real growth" than the ceiling values below. The fix is to resolve type once
   per `(goat_id, date)` with forward-fill, and feed the same value to both the page chip and
   the ceiling test.
2. **Are the growth ceilings right?** 350/600 g/day for kids, 150/300 for adults. My numbers.
   Concretely: observed kid gains run p50 0.123 / p90 0.393 / p95 0.55 kg/day, so 350 g/day cuts
   at about p87 and moves ~745 changes if lowered to 250. The adult pair governs only 349
   changes, so it is close to moot. A published per-species ADG reference range would settle
   this faster than my estimate will.
3. **Do operators type weights, or read a scale?** Decides whether §5c should exist at all.
4. **Should sheep have their own ceilings?** They are currently judged as goats.
5. **Is the 5-animal threshold right** for calling a block a copied shed figure? Lower catches
   more, risks coincidence; higher misses small pens. `build_dataset.py` asserts the current
   figures, so the experiment has a baseline.
6. **Should a pre-window abortion or health ticket carry the same 0.9 as one inside the
   interval?** §12 already answered no for kidding, with a cap and a confidence drop. There is
   a real defence for illness — a ticket dated shortly before the interval can describe an
   episode still running through it — which is why this is a question, not an obvious fix.
7. **What should happen to a flagged reading?** Today the page only recommends a re-weigh.
   There is no correction workflow behind it.
8. **Is "Open" actionable as it stands?** 275 changes (186 `no_recorded_cause`, 89
   `above_growth_ceiling`), no owner assigned. The number is not stable: widening the pen-slot
   test (§6) moves rows into `identity_drift`, and the cross-park pairs in §8 are currently
   handed to a shed manager as "ask what happened between these two dates" for a transition
   this method cannot even display.

---

## 10. How the page was actually built

Five stages, in this order:

1. `sql/enrich.sql`, `sql/enrich_pos.sql`, `sql/animals.sql`, `sql/shedlevel.sql` against
   BigQuery → `neg_events.json` (4,047 rows), `pos_events.json` (9,107), `animals.json`
   (2,103 animals), `shedlevel.json` (121).
2. `classify.py` over the losses → `classified.json`.
3. `classify_pos.py` over the gains, reading stage 2's output → `classified_pos.json`.
4. `build_dataset.py` — the overlay. Detects copied shed figures and rewrites those rows to the
   verdict `copied`; attributes phantom kilos once per bad reading; drops the case-variant junk
   ids the SQL misses (13 rows, 13,154 → 13,141); emits the page's row schema.
5. The result is embedded in `mock/weight-truth.html`.

```bash
cd docs/weighing
mkdir -p work

# Stage 1. --max_rows is NOT optional: bq defaults to 100 rows and will
# silently hand you a truncated extract that still classifies cleanly.
bq --project_id=goatos-sheets query --nouse_legacy_sql --format=prettyjson \
   --max_rows=20000 < sql/enrich.sql     > work/neg_events.json
bq --project_id=goatos-sheets query --nouse_legacy_sql --format=prettyjson \
   --max_rows=20000 < sql/enrich_pos.sql > work/pos_events.json
bq --project_id=goatos-sheets query --nouse_legacy_sql --format=prettyjson \
   --max_rows=20000 < sql/animals.sql    > work/animals.json
bq --project_id=goatos-sheets query --nouse_legacy_sql --format=prettyjson \
   --max_rows=20000 < sql/shedlevel.sql  > work/shedlevel.json

# Stages 2-4.
export WEIGHING_DATA_DIR=./work
python3 classify.py
python3 classify_pos.py
python3 build_dataset.py --in ./work --out ./work/dataset.json --check
```

`--check` asserts every figure this document quotes: 2,099 animals, 13,141 changes, 9,650
copied, 71 mirrors, 13 dropped, 146 whole-shed rows. If a rule or an extract changes, it fails
— which is the point.

**The stage 1 outputs are not in the repository**, so a clean clone can run stages 2–4 only
against a fresh BigQuery extract. Everything else is committed.

---

## 11. Files

| File | What |
|---|---|
| `mock/weight-truth.html` | the page. Self-contained, data embedded, no network |
| `docs/weighing/weight-truth-method.md` | this document |
| `docs/weighing/sql/enrich.sql` | stage 1 — losses, with joined records |
| `docs/weighing/sql/enrich_pos.sql` | stage 1 — gains, with joined records |
| `docs/weighing/sql/animals.sql` | stage 1 — per-animal series |
| `docs/weighing/sql/shedlevel.sql` | stage 1 — whole-shed readings |
| `docs/weighing/classify.py` | stage 2 — loss classifier |
| `docs/weighing/classify_pos.py` | stage 3 — gain classifier |
| `docs/weighing/build_dataset.py` | stage 4 — the overlay, with `--check` |

---

## 12. Corrections already made to this method

Recorded because the method was wrong in ways worth remembering. The first block is method
defects found while building; the second is documentation defects found by an adversarial audit
of this file against its own code and data.

### Method

| Was | Now | Why |
|---|---|---|
| **A pounds-vs-kilograms rule** flagged any pair differing by ≈2.2046 | **deleted** | Invented from a generic data-quality pattern. This farm has never used pounds. It matched coincidental ratios, and over long intervals flagged ordinary growth — a kid going 3.6 → 7.7 kg over 35 days is 117 g/day, not a unit error |
| Rate measured "first to last weighing in the period" | measured inside the window, with a pre-window anchor only when the in-window span is under 4 days | the baseline could predate the window by up to 336 days, so a "4 week" rate was measured over months |
| No minimum interval | 4-day floor | a 2-day gap makes gut fill look like a rate; the two worst "losing" animals were artefacts |
| Weekly trend drew 0 g/day for weeks with no clean reading | draws a gap | a missing measurement is not zero growth |
| `identity_drift` fired on id shape alone | needs a magnitude gate too | ordinary losses on badly-labelled animals were called identity collisions |
| `post_kidding` accepted any kidding in a 14-day pre-window, uncapped | capped, and confidence drops when the kidding predates the interval | it was passing an 84% loss as valid at 0.95 confidence |
| `refill_after_move` returned before the ceiling check | ceiling applies first | physically impossible gains were exempt from every check |
| One ceiling for all animals | split kid / adult | a mature animal does not gain like a kid |
| Noise band 3% for losses, 6%-within-3-days for gains | one shared rule | gut fill is not directional |
| Page legend read "Under 3% of body weight" for `within_noise` | states both branches | the legend never mentioned the 6%-within-3-days exception, so 24 changes were labelled "under 3%" while ranging up to 5.88% |

### Documentation

| Was | Now |
|---|---|
| Stage 4 was not committed, so the largest finding (73% of rows) could not be reproduced from this repository at all | `build_dataset.py` committed, with `--check` |
| The classifiers hard-coded absolute paths and could not run from a clean clone | read `WEIGHING_DATA_DIR` |
| The whole-shed extract had no committed query | `sql/shedlevel.sql` committed |
| "Every rule" omitted both terminal defaults, deciding 1,962 changes between them | §5e added |
| "Rules are evaluated in order" implied §5's order was the execution order; it is close to its reverse | both orders written out in §5 |
| Coverage stated 18,466 weighings / 2,104 animals / 51 dates | 15,834 / 2,099 / 56 |
| "13,141 changes across 2,099 animals" | 1,759 animals actually have a change |
| Id-format table summed to 18,466 and overstated free text 2.5× | recomputed; pen slots are 4,057, not 2,532 |
| "200 mirror intervals" | 71 — the figure predated the `copied` outcome |
| "Adults were only weighed July–October 2025" | it is a column that stops, and the page's Adult chip means something else |
| Normalisation claimed "at extract time" | only two of four queries do it; the other two feed the classifier that compares shed strings |
| Id-drop list matched none of the three queries | each one's actual filter quoted, plus the predicate they should share |
| `transport_shed_change` described as "the shed genuinely changed" | it compares raw strings; 19 of 202 rows are spelling differences |
| Join lookbacks undisclosed | 7 days health, 14 days kidding/abortion, exact interval shifting — and two rules cannot check their own window |
| `correction_of_prior_error` published at 0.9 | still 0.9, but disclosed as asserted rather than inherited from parents averaging 0.79 |
| "An animal cannot regain that" / "Growth does not reverse" | both rules have no time bound; 12 and 3 rows respectively took over 30 days |
| Growth ceilings stated without saying which animals get which | the `goat_type` test, the 12,805/349 split, and the observed percentile distribution |
| Series key not described | it is the bare animal id, chained across parks |

### Documentation — second audit

Found by a review that re-ran the pipeline against BigQuery rather than reading the files.

| Was | Now |
|---|---|
| The mock's `within_noise` legend still read "Under 3% of body weight" — the exact defect the first audit's row above says was fixed. It was fixed in this document and never in the page | the page states both branches |
| `sql/shedlevel.sql` returns 146 rows today; the document said 121 and the mock embedded 121. `--check` passed because it never asserted the shed-level count. The committed SQL uses a case-insensitive junk-id filter that my original ad-hoc query did not, so it correctly picks up 25 more rows | 146, embedded and asserted |
| The mock's STATUS filter labelled the `valid` bucket "Real loss", though it holds gains too | "Real change" |
| The stage 1 commands were not written down, so a reproducer hit `bq`'s silent 100-row default | commands given in full, with `--max_rows` and why it matters |
| The deleted pounds/kilograms rule still had two label definitions in the mock's config. No row used them, but dead config undermines a correction that claims deletion | removed |
