# Vaccination Source Rules Matrix

**Source file:** `/Users/ravi/mesha/wiki/Vaccination Rules.docx`
**Extracted into repo:** 2026-07-03 18:06 local source revision
**Scope:** V1 Preventive Care (PC) Vaccination config, generation, Calendar, Action Center,
Protocol Adherence, Workflows, and Animal Passport behavior.

This markdown is the tracked engineering source for the vaccination rule
matrix. The local DOCX remains private/source material; this file must travel
with the codebase because V1 config and kernel behavior depend on it.

## Source Vaccine Classes

### Goats

| Vaccine | Source class |
|---|---|
| ET+TT | bacteria killed |
| PPR | virus live |
| Goat Pox | virus live |
| FMD | virus killed |
| HS | bacteria killed |

### Sheep

| Vaccine | Source class |
|---|---|
| ET+TT | bacteria killed |
| PPR | virus live |
| Blue Tongue | virus killed |
| Sheep Pox | virus live |
| FMD | virus killed |
| HS | bacteria killed |

## Approved GoatOS Schedule Table

The source DOCX contains a mother-not-vaccinated / unknown-mother branch. GoatOS
does not implement that branch. The final product rule is: **mother vaccination
status is never a vaccination scheduling input**. Do not model it as a shed tag,
category, rule selector, JSON field, UI prompt, import question, seed fixture,
or fallback schedule. Operations keeps breeding and mother animals vaccinated;
every kid uses the approved standard schedule below.

| Vaccine | Approved GoatOS timing | Revaccination | Priority |
|---|---:|---:|---:|
| ET+TT | 4 weeks and 7 weeks | 6 months | 1 |
| PPR | 16 weeks | 3 years | 2 |
| Blue Tongue | 16 weeks and 20 weeks | 1 year | 4 |
| Goat Pox | 16 weeks | 1 year | 3 |
| Sheep Pox | 12 weeks | 1 year | 3 |
| FMD | 12 weeks | 9 months | 5 |
| HS | 12 weeks | 1 year | 5 |

Priority order (1 = highest): ET+TT → PPR → Goat Pox / Sheep Pox → Blue Tongue →
FMD / HS. The source now specifies every priority (earlier revisions left the
non-core rows blank).

## Source Dose / Vial Table

| Vaccine | Source course type | Dosage | Vial doses |
|---|---|---:|---:|
| PPR | Single | 1 ml | 100 |
| ET+TT | Booster | 2 ml | 100 |
| Blue Tongue | Booster | 2 ml | 100 |
| Goat Pox | Single | 1 ml | 25 |
| Sheep Pox | Single | 1 ml | 100 |
| FMD | Single | 1 ml | 30 |
| HS | Single | 2 ml | 100 |

## Same-Day Session, Overflow, And Gap Rules

These rules apply to every vaccine in the matrix. They are not Blue
Tongue-specific, and they are not operator-cap rules.

### Hard Session Cap

| Rule | Value |
|---|---:|
| Maximum vaccines per animal in one vaccination session / doctor visit | 2 |
| Operator cap counts | Animals handled by the operator |
| Operator cap does not count | Vaccine doses, vaccine rows, or obligation rows |

Same-day compatibility only means two vaccines may share one session. It never
means "give every due vaccine now." If three or more vaccines are due for the
same animal, GoatOS must select at most two compatible vaccines for this session
and schedule the remaining vaccines by the medical gap rules below.

### Vaccine Class Matrix

| Vaccine | Species scope | Class | Priority |
|---|---|---|---:|
| ET+TT | goat + sheep | killed bacterial / toxoid | 1 |
| PPR | goat + sheep | live viral | 2 |
| Goat Pox | goat only | live viral | 3 |
| Sheep Pox | sheep only | live viral | 3 |
| Blue Tongue | sheep only | killed viral | 4 |
| FMD | goat + sheep | killed viral | 5 |
| HS | goat + sheep | killed bacterial | 5 |

Priority 1 is highest. When the scheduler has to choose between competing due
vaccines, it keeps the highest-priority compatible pair in the current session.

### Cross-Vaccine Gap Matrix

| Previous vaccine class | Next vaccine class | Minimum gap |
|---|---|---:|
| live | live | 28 days / 4 weeks |
| live | killed | 14 days / 2 weeks |
| killed | live | 14 days / 2 weeks unless an explicitly allowed same-day pair is selected for the same session |
| killed | killed | 14 days / 2 weeks unless an explicitly allowed same-day pair is selected for the same session |

The normal scheduling buffer is then applied on top of the minimum safe date:
the safe scheduling range is **earliest safe date through earliest safe date +
7 calendar days**.

### Course / Booster Gaps

| Vaccine/course | Minimum gap |
|---|---:|
| ET+TT dose 1 -> ET+TT dose 2 / booster | 21 days / 3 weeks |
| Blue Tongue kid dose 1 -> Blue Tongue kid dose 2 / booster | 28 days / 4 weeks |
| ET+TT repeat/revaccination | Starts only after dose 2 / course completion; repeats every 182 days |

The ET+TT 21-day booster rule applies to **adults and kids**. Do not treat adult
ET+TT booster as optional, kid-only, or as the 182-day revaccination.

Adult entry_date is never a vaccination due-date anchor. Kids and young animals
keep strict DOB/birth-age scheduling. Adults with accepted same-vaccine history
use the last accepted vaccination date for booster/revac timing. Adults with no
accepted same-vaccine history join the normal adult campaign/catch-up cohort for
that vaccine automatically; no separate manual-campaign trigger or approval is
required. Repeat and initial/catch-up remain per-animal dose instructions inside
one logical vaccine drive. If history-backed repeat dates differ but their safe
windows overlap, schedule every repeat-history and blank-history member on the
latest readiness date inside that shared window. Do not create an earlier partial
drive. If accepted cohort history arrives after a stable blank-history row was
already generated, atomically move that same open row onto the shared date; do
not retain the split date or create a duplicate obligation. Pack by whole physical shed: a shed at or below the
full per-operator cap carries intact to the next operator-day when residual
capacity is insufficient, and partition splitting is allowed only when the
physical shed itself exceeds that full cap. Enforce the same boundary in park
pre-batching and operator planning, grouping initial/catch-up and repeat rule
rows for the same vaccine under one physical shed. Adults must not be split into
post_arrival singleton drives because their source entry dates differ.
Every operator-day row in a multi-day campaign carries the same logical
`drive_name` and the distinct-animal `drive_total` across all of its days; the
individual day count remains the executable workload for that date.

The operator submission timestamp is the medical `administered_at`. A verifier or
director may approve/close the work later, but `verified_at` and `closed_at` are
workflow timestamps and never replace the date used for the next dose.
Both generic submission closure and vaccination-batch closure preserve that
medical timestamp. Closure membership includes only active `recorded` and
`accepted` completion attempts; retained `rejected` or `reversed` attempts are
audit history and cannot block a successful retry.

### Overflow Rule For 3+ Due Vaccines

If more than two vaccines are due for the same animal:

1. Build the medically compatible candidate pairs for the current session.
2. Pick the highest-priority compatible pair, capped at two vaccines.
3. Schedule overflow vaccines from the current session date, not from the
   original old due date.
4. Apply live/killed gap rules to the overflow vaccine.
5. Apply the +7-day scheduling buffer to that overflow safe date.
6. If operator capacity would push an animal beyond its safe end date, raise a
   capacity breach and overtake the normal operator cap when needed; do not
   silently delay the animal past the safe window.

Concrete example:

| Current session date | Vaccines given now | Overflow vaccine | Earliest overflow date | Latest overflow date with buffer |
|---|---|---|---:|---:|
| 2026-07-22 | ET+TT + PPR | Blue Tongue | 2026-08-05 | 2026-08-12 |
| 2026-07-22 | ET+TT + PPR | FMD | 2026-08-05 | 2026-08-12 |
| 2026-07-22 | PPR | Goat Pox / Sheep Pox | 2026-08-19 | 2026-08-26 |

Explicit invalid schedule:

| Date | Vaccines |
|---|---|
| 2026-07-22 | ET+TT + PPR |
| 2026-07-23 | Blue Tongue |

This is invalid because Blue Tongue becomes the next vaccine after a completed
two-vaccine session and must respect the 14-day live-to-killed gap. The scheduler
must never interpret operator-cap overflow as permission to give tomorrow's
third vaccine.

## Source Q&A Decisions

The latest source doc includes the operating decisions below. These are part of
the rule source and must stay aligned with Config presets and kernel behavior.

> **Do not miss this ET+TT adult booster rule.** ET+TT is a two-dose course for
> adults as well as kids. Adult ET+TT dose 2 / booster is due 21 days after
> adult ET+TT dose 1, with the normal scheduling buffer applied by the planner.
> Do not read the adult sheet's `ET+TT Booster` column as kid-only, optional, or
> as the 182-day revaccination. The 182-day ET+TT repeat starts only after dose
> 2/course completion.
>
> **Post-seed invariant:** if the DB contains accepted `et_tt_adult_w1`
> completions but zero matching same-goat `et_tt_adult_w2` obligations or
> completions, the seed/generation output is invalid. Reporting only later
> generated drive rows while adult dose 2 is missing is a blocker.

| Question | Source answer | V1 implication |
|---|---|---|
| Mother not vaccinated / unknown mother status | Source DOCX contains an early branch. | **Ignore it in GoatOS.** Never ask this as a rule/config/import question. Always use the approved standard schedule. |
| Kid strict schedule: till 16 weeks only? | Yes, while still obeying live/killed constraints. | Kid schedule applies through the 16-week cutoff; compatibility rules still control final due dates. |
| Live-to-live gap: 4 weeks? | Yes. | Live vaccine spacing is a hard 28-day floor. |
| Vaccinated at source then warm-up: 7 days from warm-up entry or source dose? | Seven days from warm-up entry. | Warm-up hold anchors to farm-entry date. |
| After delivery: all missed doses in 2 weeks or by priority? | By priority and whatever is due; ideally mothers are fully vaccinated before delivery. | Post-delivery catch-up uses vaccine priority and should be rare because breeding/pregnancy vaccination is planned earlier. |
| Sheep adults: Blue Tongue booster timing vs pox step? | ET+TT booster can be given after 3 weeks for both kid and adult courses. Blue Tongue kid booster remains 4 weeks. | ET+TT course rows keep a 21-day minimum gap; Blue Tongue kid course keeps 28 days. |
| Untrusted procurement vaccine notes: full catch-up or trust with review? | Never trust vaccine outside our supervision; trust only our parks or procurement holding parks. | Procurement holding-park vaccination starts the GoatOS schedule there; third-party/vendor claims do not suppress scheduled work. |
| Pregnancy month 1-5: what date starts the clock? | Rough known breeding date. | Pregnancy month calculation starts from breeding date when available. |
| Mother vaccinated: how is it recorded? | Mother ID is known and mother vaccines are ensured before gestation month 4. | Dam link may be stored for lineage/audit, but kid scheduling must not branch on dam vaccination status. |
| Warm-up entry: which date counts? | Date the animal enters our farm; deworming-type work may start from day 3. | Vaccination waits 7 days from farm entry; adjacent non-vaccination interventions can have shorter holds. |
| Kid vs adult: age only, stage only, or both? | Once kids are at least 16 weeks, no minimum-age constraints remain for giving a vaccine. | Age is the cutoff for the kid schedule; adult catch-up/repeat logic applies after it. |
| Missed / late doses: catch up now, skip to next cycle, or Preventive Care approval? | Depends on next drive distance: immediate if trailing the next cycle; wait if same-cycle drive is within 2 weeks; immediate if more than 2 weeks away. | Missed-dose handling is cycle-relative. |
| Mixed goat + sheep in one park/shed/tag drive plan: one drive or split by species? | Maximize the doctor visit at park level. Kids can be one shared goat+sheep drive group; adults remain species-specific execution groups inside the same park visit. | Drive planning is park-level with shed/tag breakdowns. Mixed-species grouping is kid-only; adult vaccine work is species-safe. |
| Additional constraints? | Hold bred animals for at least one month from breeding; prioritize breeding-ready animals; avoid vaccinating milking-department animals during milking. | Breeding hold, breeding-ready prioritization, and milking avoidance are V1 policy inputs. |

## Dose Suppression and Protocol Lineage

When a vaccination obligation is suppressed because the animal already has an
accepted completion for that dose:

- **Dose suppression requires matching protocol lineage.** An accepted completion
  for vaccine X only suppresses/counts-as-done for the same protocol lineage and
  dose position. A completion for PPR under "PHC standard kid schedule" does not
  suppress an obligation for PPR under a different protocol rule (for example,
  "catch-up adult drive" or "procurement warm-up protocol"). A completion for
  one ET+TT course position (dose 1) does not suppress the next ET+TT course
  position (dose 2) even if they are the same vaccine.
  The rule engine must match the protocol version, rule identity, or lineage key,
  not just the vaccine name. If a source-backed rule or admin-authored override
  generates an obligation and the animal already has an accepted completion for
  that exact rule position, suppress it; otherwise, let the obligation stand.

- **Trusted source suppression is scoped to supervised lifecycle only.** An
  accepted completion recorded outside our parks and procurement holding parks
  (for example a vendor claim at the source before arrival) may be a
  `source_vaccination_evidence` ledger row, but it does NOT suppress GoatOS
  scheduled work unless it also appears in our supervised history (our parks or
  our procurement holding-park SOP records). The generation rule engine must
  distinguish `supervised_completion` (our parks, procurement holding parks under
  SOP + video validation) from `unsupervised_source_evidence` and only suppress
  work on the former.

## Date Corrections and Effective Lock

When a user corrects a field on an accepted vaccination completion (for example,
the administered date was recorded as July 5 but should be July 4):

- **Date corrections validate the ENTIRE effective locked record.** A correction
  to a single field (administered date, recorded date, witness) must
  re-validate the whole effective record (the completion row plus any downstream
  derived state such as pregnancy month exclusion, booster due calculation,
  sequencing rule adherence) under the corrected values. If the correction
  makes the record invalid (for example, correcting the date reveals that the
  dose was given during pregnancy month 4, which is prohibited), the correction
  must be rejected with a clear reason, not silently applied. The database
  must enforce this in a transaction: apply the update, re-run the validation
  check that led to acceptance/publication, and roll back if any check fails.
  This prevents silent creation of invalid state that breaks downstream
  assumptions (booster scheduling, protocol sequencing, schedule adherence).

## Business Dates (India Operational)

Every business meaning derived from timestamps — scheduling, due/missed buckets,
reminder keys, reporting groups, age/stage calculation, pregnancy month clocks —
must use India/local operational dates:

- **Business dates use India/local operational dates, never raw UTC instants.**
  When comparing an animal's birth date (e.g., 2024-07-15 in India) to a due-date
  schedule (e.g., 4 weeks old), calculate the animal's age in India-local days,
  not UTC days. A kid born at 23:30 on July 15 in India Timezone is still
  July 15 for scheduling purposes, even if that instant is July 15 22:00 UTC
  (earlier in UTC). Scheduling, due buckets, missed/overdue/future grouping,
  and UI labels must all convert timestamps to `Asia/Kolkata` first, then
  derive the business meaning.
- **Pregnancy month clocks start from breeding date in local timezone.** If
  breeding occurred on June 10 (roughly) and pregnancy is 5 months, the calendar
  for "months 4 and 5 no vaccination" runs from June → July → August →
  September → October (months 1-5), and no vaccination is allowed in September
  and October. The month boundaries are defined by calendar month in the
  operational timezone, not by absolute 30-day windows.
- **Booster due calculation is based on completion date in local timezone.** If a
  completion is accepted as of July 15 at 19:00 IST, and the booster interval is
  6 months, the booster is due on January 15 of the following year (not 180 or
  181 absolute days). Use calendar month arithmetic in the operational timezone.

## Additional Rules

### Spacing, combination, and gaps

- After procurement/warm-up, even if the source vaccinated the animal, do not
  give any vaccination for one week.
- At most **2 shots per animal per drive/doctor visit**. Same-day compatibility
  does not mean "give everything due." If more than 2 vaccines are due, GoatOS
  picks the highest-priority compatible pair and schedules the remainder from
  the current session date using the cross-vaccine gap matrix. Operator-cap
  overflow must not become a next-day third shot.
- Two live vaccines must have a 4-week gap.
- Bacterial + viral vaccines may be combined on the same day.
- Live viral + killed viral vaccines may be combined on the same day.
- Quarantine and ICU animals should not be vaccinated.
- Live followed by killed needs a 2-week gap.
- Killed followed by live needs a 2-week gap unless selected as an explicitly
  allowed same-day pair in the same session.
- Killed followed by killed needs a 2-week gap.
- Live followed by live needs a 4-week gap.
- ET+TT dose 2 needs a 3-week gap after ET+TT dose 1 for both kid and adult
  courses. ET+TT repeat/revaccination starts only after dose 2/course
  completion and repeats every 182 days.
- Blue Tongue kid dose 2 keeps its 4-week gap after Blue Tongue dose 1.
- Example: a live + killed pair can run on the same day when no other blocker
  exists. If the next due vaccine is also live, it must wait at least 4 weeks
  from the prior live dose even if the park is running another drive sooner.

### Drive batching hold

- The operating goal is to maximize safe doctor output at the **park visit**
  level, while preserving exact shed/tag/species/vaccine breakdowns for the
  work list, proof, and audit.
- Park-drive batching is **animal-count first**. Rank candidate drive dates by
  the number of distinct animals that can safely attend, not by obligation row
  count, vaccine count, or shed count. One goat with two vaccine rows must never
  beat a date that safely serves two goats.
- Shed count is not a batching constraint. A valid park drive may contain one
  shed or many sheds; shed names are display/proof detail only. Reject any
  planner/review/test that strands animals because the candidate date had "only
  one shed" while more same-park animals could safely club inside the window.
- GoatOS may hold a due shed/tag group for up to **7 calendar days** to combine
  it with another compatible same-park drive group, but only if every animal
  remains inside its medical safe window.
- The 7-day batching hold is a maximum delay from each animal/group's own due or
  ready date. It does not permit exact `due_at + 4 weeks` scheduling for every
  individual animal, and it does not permit a small drive when another
  compatible same-park drive exists between the group's due/ready date and its
  safe-until date.
- The planner may maximize output only across animals that are individually
  feasible on the picked drive date. A larger drive is invalid if even one
  attached obligation's safe window has already ended before that planned date.
- Operational per-drive animal caps are soft; medical safe-until is hard. When
  overflow animals can safely move to the next feasible day, move them. When an
  animal is on its last safe day and moving would cross its own safe-until date,
  keep it in the current/last-safe drive even if that drive exceeds the normal
  cap. The per-animal shot cap still remains hard.
- Combo-session alignment is subject to the same hard rule after batching:
  moving a built batch to a shared combo date is valid only when that date is
  inside every attached obligation's safe window.
- Snapshot/backfill sweeps must drain the bounded candidate set across repeated
  safe passes. A first pass that batches only part of a group because of shot
  caps or safe-window filtering must retry the remaining candidates within their
  own safe windows instead of leaving them as calendar micro-drives.
- A seed/reseed/import is not operator-ready after obligation generation alone.
  The closeout must run the drive-batching sweeper through the visible schedule
  horizon and fail if any in-window `scheduled`/`due` vaccination obligation is
  still missing an `obligation_batches` assignment. Calendar and Full Schedule
  proofs taken before that sweeper pass are partial-environment artifacts, not
  algorithm evidence.
- The closeout must also run the vaccination drive-clubbing DB proof. It fails
  if any 1-2 animal drive has another compatible same-park drive inside that
  tiny drive's own due/ready-to-safe-until window. A tiny drive is acceptable
  only when this proof has no reachable clubbing target.
- Live goat placement is mandatory. Seed/import must deterministically assign
  every accepted live goat to a real active shed before vaccination generation.
  Goat vaccination obligations are shed-scoped only (`scope_type='shed'`,
  `scope_id=goats.shed_id`). Park is the visit/drive grouping scope used after
  obligations exist; it is not a fallback location for an animal's obligation.
- This batching hold is **one-time per obligation/dose cycle**. Once a due item
  has been held to overlap with a later compatible group, it cannot be held
  again to chase the next group. No rolling postponement.
- If the item was already held once, the medical window would expire, vaccine
  compatibility fails, stock/proof/worker requirements fail, or the animal enters
  a defer state, GoatOS runs a micro-drive now or applies the explicit blocker.
- Dead, culled, and sold animals are permanently out of vaccination scheduling.
  Sick, ICU, quarantine, under-treatment, pregnancy month 4-5, post-breeding
  hold, and similar temporary blocks defer; when the animal becomes eligible
  again, the planner re-enters from that recovery/ready date, uses the accepted
  vaccination history already on the animal, and clubs into the nearest
  compatible same-park drive inside the allowed hold/safe window.
- Example: if a booster is due after a 3-week minimum gap and a compatible
  park drive is safely due in week 4, the planner may move it once to week 4.
  It must not keep moving it to week 5 or week 6 to chase a larger batch.

### Pregnancy and delivery

- Pregnancy is around 5 months.
- Vaccines are allowed up to pregnancy month 3.
- Vaccines must not be scheduled in pregnancy months 4 and 5.
- After delivery, missed vaccines can be given within 2 weeks, prioritized by
  vaccine priority. In practice this should be rare: mothers are fully
  vaccinated before delivery (only vaccinated animals are used for breeding, and
  pregnant-animal drives are prioritized) so the kid inherits immunity.
- The pregnancy month clock starts from the rough known **breeding date**.
- Do not derive kid vaccination timing from dam/mother vaccination status. Dam
  links may exist for lineage and audit, but the vaccination kernel always uses
  the approved standard kid schedule.

### Procurement and source trust (scope-split)

- When procuring adult goats/sheep, vaccination can be done at source; breeding
  and fattening animals can also be vaccinated at source.
- **Trusted source = our supervised lifecycle only.** Vaccinations performed by
  our team in our parks OR in our procurement holding parks (where animals are
  held 4–5 weeks near the buying region under our procurement SOP, with
  video/physical validation) are trusted. These vaccinations start the GoatOS
  vaccination course while the animal is still in the procurement holding park;
  after accepted intake into our regular sheds, the kernel continues from that
  trusted history instead of duplicating those doses.
- **Untrusted source = unverified third-party/vendor claims** given outside our
  SOP and supervision. These are never trusted and must not suppress scheduled
  work. If the source is not one of our procurement holding parks or our own
  parks, the vaccination course starts after the animal reaches/enters our shed
  and clears the warm-up/health gates.
- For kids up to 16 weeks, the normal schedule must be followed regardless of
  source claims.

### New-animal procurement schedule

- Newly procured animals receive ET+TT + PPR first after the warm-up hold.
- ET+TT dose 2 is due 3 weeks after that ET+TT dose 1 for both goats and
  sheep.
- Goat Pox for goats or Sheep Pox for sheep remains due after 4 weeks because
  the 4-week wait honors the PPR/live-to-pox live→live spacing rule.

### Warm-up entry

- Warm-up hold is 7 days from the **farm-entry date** (the date the animal
  enters our farm), not from the source dose date; resume vaccination after the
  7-day cool-off.
- Related non-vaccination interventions (e.g. deworming) may start from Day 3
  rather than waiting the full week.

### Deferred animals returning to eligibility

- Sick, under-treatment, ICU, quarantine, pregnancy month 4–5, and post-breeding
  hold are predefined postponement rules. They are safety blocks, not optional
  planner choices.
- When an animal recovers/exits the defer state and is back in its valid shed/tag,
  GoatOS reopens the missed vaccination obligation from that recovery/ready date.
- The planner may place the recovered animal into the nearest compatible
  same-park drive only if that drive is within 7 calendar days of the recovery
  ready date and all medical/vaccine rules remain safe.
- If the nearest compatible drive is more than 7 calendar days away, GoatOS must
  create a micro-drive within the 7-day buffer, even for a single animal.
- Example: if a drive is on July 10, animals recovered on July 4 and July 5 may
  both join it if compatibility and medical windows are safe. If the next drive
  is July 12, the recovered animals cannot wait for it; they are grouped into a
  smaller drive within their 7-day recovery buffer.

### Breeding, milking, and prioritization

- Do not vaccinate any bred animal for at least one month from its breeding
  date.
- Prioritize vaccinations for non-pregnant breeding-ready animals over others,
  timed so the schedule completes roughly 2 months before their proposed
  breeding start date.
- Milking-department animals should not be vaccinated during milking, as it
  reduces milk output; vaccinate them in the non-milking part of the cycle.

### Adult drives and the production cycle

- An adult goat needs drive intents with ET+TT dose 2 at 3 weeks and pox/live
  spacing at 4 weeks:
  1. ET+TT; PPR
  2. ET+TT dose 2
  3. Goat Pox
  4. FMD + HS
- Rough 8-month adult production cycle: gestation 5 months, milking 1 month,
  rest 0.5 month, prep-for-next-breeding 1.5 months. Use the rest and prep
  windows to run vaccination drives.

### Missed / late doses (cycle-relative)

- Catch-up timing depends on how far the next drive is:
  - If the next drive is cycle 2 and this animal is still on cycle 1 →
    vaccinate immediately.
  - If the next cycle-1 drive is within 2 weeks → wait for it.
  - If the next cycle-1 drive is more than 2 weeks away → vaccinate immediately.

### Mixed-species sheds

- Parks contain multiple sheds/tags. The operating goal is to give doctors the
  maximum safe count for a park visit, while still showing the exact per-shed
  and per-tag animal counts.
- GoatOS must not treat "one shed = one tiny drive" as the end goal. It should
  combine compatible due work across sheds/tags in the same park whenever every
  animal remains inside its safe medical window.
- Kid shed/tag groups can combine goat and sheep kids into one shared drive
  group when due windows, live/killed spacing, max-shots-per-visit, stock,
  health, quarantine/ICU, and warm-up rules are all safe.
- Adult work remains species-specific inside the same park visit. Doctors may
  physically handle adult goats and adult sheep on the same day, but GoatOS
  keeps separate adult goat and adult sheep execution groups because adult
  species vaccines differ: Goat Pox is goat-only; Sheep Pox and Blue Tongue are
  sheep-only; ET+TT, PPR, FMD, and HS are shared only where the matrix allows.

## V1 Implementation Contract

V1 must support this tracked matrix in the Config modal and in the kernel.

The Config modal must author these values per matrix row:

- vaccine code and name;
- immunological type: live, killed, toxoid, or reviewed combo;
- pathogen class: bacterial, viral, mixed, or reviewed unknown;
- source course type: single or booster;
- stage, sex, and breed eligibility;
- schedule text from the tracked rules table;
- dose amount and unit;
- vial dose count;
- revaccination interval in days;
- priority (the source now provides a priority for every row);
- protocol version/audit metadata. Source-review fields are not part of the
  CEO/COO Config UI; this file is engineering evidence for the preset values.

The shared V1 policy must author:

- clinical defer states: sick, under treatment, recovering, quarantine, ICU;
- reproductive exclusions, including pregnancy skip policy (months 4–5) and the
  post-breeding one-month vaccination hold;
- breeding-ready prioritization (complete ~2 months before proposed breeding);
- milking-department vaccination avoidance during milking;
- procurement warm-up hold of 7 days from farm-entry date;
- kid normal-schedule cutoff at 16 weeks;
- adult prior-vaccination allowed only for our supervised lifecycle (our parks
  and procurement holding parks under SOP with validation); unverified
  third-party source claims are not trusted;
- same-day compatibility allowed flags;
- live/killed spacing days;
- ET+TT course booster minimum gap of 21 days for both kid and adult courses;
- Blue Tongue kid booster minimum gap of 28 days;
- first and second procurement waves.

The V1 kernel must enforce:

- birth-age schedule rows from the standard mother/adult schedule;
- trusted prior vaccination evidence suppression **scoped to supervised-lifecycle
  sources only** (our parks / procurement holding parks under SOP), so
  pre-existing ET+TT/PPR/FMD records from those sources do not duplicate work;
  unverified third-party claims must not suppress work;
- generation of the next missing source-schedule row when trusted prior evidence
  exists;
- one safe older-animal catch-up/review item instead of flooding all missed old
  doses;
- cycle-relative missed-dose handling (immediate when the animal trails the next
  cycle or the next same-cycle drive is >2 weeks out; wait when it is ≤2 weeks);
- one-week post-arrival warm-up offset from farm-entry date;
- quarantine/ICU/sick/under-treatment/recovering visible defers;
- pregnancy exclusion where the animal state says pregnant/lactating; month-4
  and month-5 precision must use pregnancy month fields (clock from breeding
  date) when present in the animal data model;
- post-breeding one-month vaccination hold from the breeding date;
- park-level drive planning that maximizes safe doctor coverage while retaining
  per-shed/tag breakdowns;
- mixed-species kid drive groups (single shared group for compatible goat+sheep
  kids; adult groups stay species-specific inside the same park visit);
- max 2 shots per animal per drive/doctor visit, with overflow scheduled by
  vaccine priority and safe gap rules;
- one-time batching hold up to 7 calendar days to merge compatible same-park
  shed/tag groups when the medical window stays safe; never rolling
  postponement;
- Calendar drive aggregation after sweeper/planner batching, with animal-level
  due rows remaining in Passport, Protocol Adherence, and Vaccination detail.

## V1 Source Matrix Presets

These are the V1 authoring presets loaded by the Config modal's
**Load Vaccine Matrix Preset** action. Rows target all stages by default because
the medical timing is DOB/completion based; stage, sex, and breed can still be
narrowed by an admin after loading when the source rule needs a specific combo.

| Species/rules row | Vaccine | Type | Pathogen | Course | Kid timing | Adult timing | Revaccination days | Dose | Vial | Priority |
|---|---|---|---|---|---|---|---:|---:|---:|---:|
| goat | ET+TT | killed | bacterial | booster | 28d, 49d | dose 1, dose 2 after 21d | 182 after dose 2 | 2 ml | 100 | 1 |
| goat | PPR | live | viral | single | 112 | 112 | 1095 | 1 ml | 100 | 2 |
| goat | Goat Pox | live | viral | single | 112 | 140 | 365 | 1 ml | 25 | 3 |
| goat | FMD | killed | viral | single | 84 | 84 | 274 | 1 ml | 30 | 5 |
| goat | HS | killed | bacterial | single | 84 | 84 | 365 | 2 ml | 100 | 5 |
| sheep source | ET+TT | killed | bacterial | booster | 28d, 49d | dose 1, dose 2 after 21d | 182 after dose 2 | 2 ml | 100 | 1 |
| sheep source | PPR | live | viral | single | 112 | 112 | 1095 | 1 ml | 100 | 2 |
| sheep source | Blue Tongue | killed | viral | booster | 112, 140 | 112, 140 | 365 | 2 ml | 100 | 4 |
| sheep source | Sheep Pox | live | viral | single | 84 | 84 | 365 | 1 ml | 100 | 3 |
| sheep source | FMD | killed | viral | single | 84 | 84 | 274 | 1 ml | 30 | 5 |
| sheep source | HS | killed | bacterial | single | 84 | 84 | 365 | 2 ml | 100 | 5 |

Goat Pox has a rules-table schedule of 16 weeks, but PPR is also a live viral row at
16 weeks and priority 2. V1 therefore preserves PPR at 112 days and moves Goat
Pox to 140 days when the source vaccine matrix is loaded, honoring the source
live-live 4-week spacing rule.

Each goat source preset also authors an adult revaccination dose row using the
source revaccination interval (`after_previous_completion` with the source
interval as the offset/minimum gap and `repeat=every_n_days`). SM-7 schedules
the first adult revaccination after the prior accepted completion, then
reschedules the same adult row after each accepted adult dose so the source
6-month, 9-month, 1-year, and 3-year adult cycles continue from actual accepted
completion dates.

The runtime target type is `herd_animal`; sheep rows are retained in the V1
tracked matrix so the config evidence is complete. Runtime species-aware
targeting must not silently pretend sheep are goats; when species fields are
present in animal data, sheep rows must use the same V1 kernel rules with a
species eligibility dimension.

## Brands and products under one vaccine (Z1 / Z2 / Z3) — UNRESOLVED

Source: WhatsApp clarification from Aryaman, 2026-08-19 21:51–21:55 IST, in response to
"can u give z1 z2 z3 properties in this way" against the vaccine properties table.

Stated verbatim:

- "goat + sheep; killed; bacterial/toxoid; 4 weeks + booster at 7 weeks; every 6 months" —
  **for all** of Z1, Z2 and Z3
- "z1 and z2 are diff brands"
- "z3 is diff product"
- "All with et+tt can be given simultaneously"

So all three carry ET+TT's clinical properties: goat + sheep, killed, bacterial/toxoid,
4 weeks + booster at 7 weeks, revaccination every 6 months.

### What is not yet known

`Z1` / `Z2` / `Z3` are internal shorthand, not trade names. They do not appear anywhere in this
repo, in the wiki, or in public veterinary product listings — Indian or otherwise. The real
products in this class are named CDT / 3-way, Covexin-8, Bar-Vac CD/T, Toxipra Plus, or (Indian
market) Clostridium perfringens Type D toxoid preparations. None abbreviates to Z-anything.

**Required before this can be configured:** the actual label names and manufacturers. Without
them a lot cannot be traced to a manufacturer, a batch cannot be checked, and "Z1" written on a
vial photo is unverifiable in an audit.

### Two modelling questions this raises

**1. Brands are a layer below `vaccine`, which the plan does not model.**

The protocol plan authors *vaccines* (ET+TT, PPR, FMD). Z1/Z2/Z3 sit underneath one of them.

- If all three are interchangeable ET+TT preparations, they belong in **inventory** — as lots
  selected by the operator through the existing FEFO lot picker — and must **not** become three
  rows in the plan. Three rows with an identical schedule would triple the obligations for one
  clinical event.
- If Z3 is genuinely a separate product given *in addition to* ET+TT rather than instead of it,
  it needs its own vaccine row, its own `compatibility_group`, and its own priority.

Aryaman's wording ("z3 is diff product", but same properties, and all givable together) does not
settle which. It reads closer to the second, which is the more expensive answer.

**2. "All with ET+TT can be given simultaneously" conflicts with a published safety rule.**

The authoring UI states, read-only: *"Max 2 vaccines per animal per doctor visit."* Enforced as
`max_vaccines_per_combo_session: 2` in `rule_dsl.compatibility_policy` and validated at publish
(`protocol/app/publish.go` rejects a `procurement_policy` wave exceeding the cap).

ET+TT + Z1 + Z3 in one visit is three. Either:

- they count as one vaccine for the cap (same family / same `compatibility_group`), in which case
  the cap logic needs an explicit same-family exemption; or
- the cap is correct and the vet's "simultaneously" is describing something the system will
  refuse.

This must be resolved before any of Z1/Z2/Z3 is entered as a vaccine row, not after.

### Status

Open. No config change has been made for Z1/Z2/Z3. The vaccine matrix above is unchanged.
