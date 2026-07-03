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

## Source Q&A Decisions

The latest source doc includes the operating decisions below. These are part of
the rule source and must stay aligned with Config presets and kernel behavior.

| Question | Source answer | V1 implication |
|---|---|---|
| Mother not vaccinated / unknown mother status | Source DOCX contains an early branch. | **Ignore it in GoatOS.** Never ask this as a rule/config/import question. Always use the approved standard schedule. |
| Kid strict schedule: till 16 weeks only? | Yes, while still obeying live/killed constraints. | Kid schedule applies through the 16-week cutoff; compatibility rules still control final due dates. |
| Live-to-live gap: 4 weeks? | Yes. | Live vaccine spacing is a hard 28-day floor. |
| Vaccinated at source then warm-up: 7 days from warm-up entry or source dose? | Seven days from warm-up entry. | Warm-up hold anchors to farm-entry date. |
| After delivery: all missed doses in 2 weeks or by priority? | By priority and whatever is due; ideally mothers are fully vaccinated before delivery. | Post-delivery catch-up uses vaccine priority and should be rare because breeding/pregnancy vaccination is planned earlier. |
| Sheep adults: Blue Tongue booster timing vs pox step? | Any booster can be given after 3 weeks. | Booster rows keep a 21-day minimum gap. |
| Untrusted procurement vaccine notes: full catch-up or trust with review? | Never trust vaccine outside our supervision; trust only our parks or procurement holding parks. | Third-party/vendor claims do not suppress scheduled work. |
| Pregnancy month 1-5: what date starts the clock? | Rough known breeding date. | Pregnancy month calculation starts from breeding date when available. |
| Mother vaccinated: how is it recorded? | Mother ID is known and mother vaccines are ensured before gestation month 4. | Dam link may be stored for lineage/audit, but kid scheduling must not branch on dam vaccination status. |
| Warm-up entry: which date counts? | Date the animal enters our farm; deworming-type work may start from day 3. | Vaccination waits 7 days from farm entry; adjacent non-vaccination interventions can have shorter holds. |
| Kid vs adult: age only, stage only, or both? | Once kids are at least 16 weeks, no minimum-age constraints remain for giving a vaccine. | Age is the cutoff for the kid schedule; adult catch-up/repeat logic applies after it. |
| Missed / late doses: catch up now, skip to next cycle, or Preventive Care approval? | Depends on next drive distance: immediate if trailing the next cycle; wait if same-cycle drive is within 2 weeks; immediate if more than 2 weeks away. | Missed-dose handling is cycle-relative. |
| Mixed goat + sheep in one park/shed/tag drive plan: one drive or split by species? | Maximize the doctor visit at park level. Kids can be one shared goat+sheep drive group; adults remain species-specific execution groups inside the same park visit. | Drive planning is park-level with shed/tag breakdowns. Mixed-species grouping is kid-only; adult vaccine work is species-safe. |
| Additional constraints? | Hold bred animals for at least one month from breeding; prioritize breeding-ready animals; avoid vaccinating milking-department animals during milking. | Breeding hold, breeding-ready prioritization, and milking avoidance are V1 policy inputs. |

## Additional Rules

### Spacing, combination, and gaps

- After procurement/warm-up, even if the source vaccinated the animal, do not
  give any vaccination for one week.
- At most **2 shots per animal per drive/doctor visit**. Same-day compatibility
  does not mean "give everything due." If more than 2 vaccines are due, GoatOS
  picks the highest-priority compatible pair and schedules the remainder on the
  next safe date.
- Two live vaccines must have a 4-week gap.
- Bacterial + viral vaccines may be combined on the same day.
- Live viral + killed viral vaccines may be combined on the same day.
- Quarantine and ICU animals should not be vaccinated.
- Live followed by killed needs a 2-week gap.
- Killed followed by killed needs a 2-week gap.
- Live followed by live needs a 4-week gap.
- Any kid booster needs a 3-week gap.
- Example: a live + killed pair can run on the same day when no other blocker
  exists. If the next due vaccine is also live, it must wait at least 4 weeks
  from the prior live dose even if the park is running another drive sooner.

### Drive batching hold

- The operating goal is to maximize safe doctor output at the **park visit**
  level, while preserving exact shed/tag/species/vaccine breakdowns for the
  work list, proof, and audit.
- GoatOS may hold a due shed/tag group for up to **7 calendar days** to combine
  it with another compatible same-park drive group, but only if every animal
  remains inside its medical safe window.
- This batching hold is **one-time per obligation/dose cycle**. Once a due item
  has been held to overlap with a later compatible group, it cannot be held
  again to chase the next group. No rolling postponement.
- If the item was already held once, the medical window would expire, vaccine
  compatibility fails, stock/proof/worker requirements fail, or the animal enters
  a defer state, GoatOS runs a micro-drive now or escalates the explicit blocker.
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
  held 3–4 weeks near the buying region under our procurement SOP, with
  video/physical validation) are trusted and suppress duplicate work in the
  kernel.
- **Untrusted source = unverified third-party/vendor claims** given outside our
  SOP and supervision. These are never trusted and must not suppress scheduled
  work; the animal follows the normal schedule (catch-up/review), not evidence
  suppression.
- For kids up to 16 weeks, the normal schedule must be followed regardless of
  source claims.

### New-animal procurement schedule

- Newly procured animals receive ET+TT + PPR first.
- After 4 weeks, give Goat Pox for goats or Sheep Pox for sheep plus ET+TT
  booster (the 4-week wait honors the live→live spacing rule).

### Warm-up entry

- Warm-up hold is 7 days from the **farm-entry date** (the date the animal
  enters our farm), not from the source dose date; resume vaccination after the
  7-day cool-off.
- Related non-vaccination interventions (e.g. deworming) may start from Day 3
  rather than waiting the full week.

### Breeding, milking, and prioritization

- Do not vaccinate any bred animal for at least one month from its breeding
  date.
- Prioritize vaccinations for non-pregnant breeding-ready animals over others,
  timed so the schedule completes roughly 2 months before their proposed
  breeding start date.
- Milking-department animals should not be vaccinated during milking, as it
  reduces milk output; vaccinate them in the non-milking part of the cycle.

### Adult drives and the production cycle

- An adult goat needs three drives, completable within about 4 weeks:
  1. ET+TT; PPR
  2. Goat Pox
  3. FMD + HS
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

- clinical defer states: sick, under treatment, quarantine, ICU;
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
- kid booster minimum gap of 21 days;
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
- quarantine/ICU/sick/under-treatment visible defers;
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

| Species/rules row | Vaccine | Type | Pathogen | Course | Rules table days from DOB | V1 effective days | Revaccination days | Dose | Vial | Priority |
|---|---|---|---|---|---:|---:|---:|---:|---:|---:|
| goat | ET+TT | toxoid | bacterial | booster | 28, 49 | 28, 49 | 182 | 2 ml | 100 | 1 |
| goat | PPR | live | viral | single | 112 | 112 | 1095 | 1 ml | 100 | 2 |
| goat | Goat Pox | live | viral | single | 112 | 140 | 365 | 1 ml | 25 | 3 |
| goat | FMD | killed | viral | single | 84 | 84 | 274 | 1 ml | 30 | 5 |
| goat | HS | killed | bacterial | single | 84 | 84 | 365 | 2 ml | 100 | 5 |
| sheep source | ET+TT | toxoid | bacterial | booster | 28, 49 | 28, 49 | 182 | 2 ml | 100 | 1 |
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
