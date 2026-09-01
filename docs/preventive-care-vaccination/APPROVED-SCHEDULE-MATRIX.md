# Preventive Care (PC) Vaccination Approved Schedule Matrix

Date reviewed: 2026-07-02

Base tag source: `context/source-findings/goats-and-parks-source-findings.md`
Vaccine rule source: `docs/preventive-care-vaccination/vaccination-rules.md`

This is the schedule matrix to use for Preventive Care (PC) Vaccination
authoring, review, seed data, tests, and product discussion. The Goats and Parks
source finding is the base for shed tags, lifecycle/stage, warmup, pregnancy,
ICU, and quarantine. The Vaccination Rules source finding overlays vaccine timing,
dose, vial, repeat, procurement, pregnancy, and gap rules. Raw DOCX provenance
is recorded inside the in-repo vaccination rules source files; this matrix must point
developers to committed Markdown sources they can open after cloning
`vgoats/goatos`.

The alternate early-kid source branch is intentionally excluded from this file.
Do not add it to config, tests, UI tables, source comparisons, catch-up logic, or
seed data unless Ravi explicitly reopens that branch.

## Binding Rules (govern ALL vaccination work)

These rules are non-negotiable and MUST be honored by every piece of
vaccination authoring, review, seed data, config, obligation/rule generation,
tests, drive planning, and admin-web/operator UI. They are stated in full in the
sections below; this block makes them binding law, not commentary.

1. **Kid-course rendering rule (4w through approved due points).** Schedule boards, drive
   previews, and generated obligations show ages where a vaccine is actually
   **due** for the selected species/path — not every week of life. The goat/sheep
   kid path starts/eligibility timing is 16w for 16-week vaccines; its due points
   are 4w, 7w, 12w, 16w, and 19w for Blue Tongue sheep boosters. K0 / K1 / early K2 have **no** approved-schedule
   vaccine before 4w. After the applicable kid-course continuation point, the
   animal moves to steady-state repeat scheduling driven by accepted completion
   dates, not fixed week slots.
   Adult procurement is a separate path: first eligible day after the 7-day
   warmup hold, ET+TT dose 2 after 21 days, and pox after the 28-day
   live-to-live spacing window. See "Kid Course Rendering Rule".

2. **Current pox timing rule.** Goat Pox and Sheep Pox are 16-week vaccines.
   Do not seed, publish, or regenerate Goat Pox at 20w or Sheep Pox at 12w. If a
   same-day live-vaccine conflict exists, the drive planner must use compatibility
   and max-vaccine limits to place the work safely without changing the vaccine's
   configured source timing.

3. **No mother-vaccination-status category.** Do not create a tag, matrix row,
   selector, JSON dimension, API field, import prompt, seed, test fixture, UI
   option, or fallback schedule based on missing/unknown/not-vaccinated mother
   evidence. GoatOS ignores that source branch and always uses the approved
   standard kid schedule. Mothers use the standard adult/mother schedule because
   the operating policy is to keep them vaccinated.

4. **Per-vaccine anchor precedence.** For each vaccine independently, the latest
   accepted administration of that same vaccine is the authoritative anchor for
   its next dose or repeat. DOB and herd-entry date are fallback anchors only
   when that vaccine has no accepted administration history. Adding or correcting
   DOB/entry later must not cancel, replace, duplicate, or move an obligation
   already anchored to accepted same-vaccine history. One vaccine's history must
   never anchor another vaccine. See "Per-Vaccine Anchor Precedence".

## Operating Rules

- Tags are housing/lifecycle context. A same-vaccine accepted administration
  takes precedence over DOB and herd-entry date; DOB/entry are fallback anchors
  only when that vaccine has no accepted history.
- If a tag and age disagree, review the animal; do not blindly schedule only
  from shed tag text.
- Repeat cycles start from the actual accepted vaccination date, or from the
  accepted completion of a booster course where the vaccine has a course.
- ICU and quarantine block vaccination until the state clears.
- Warmup/procured animals have a 7-day no-vaccination hold before any eligible
  procurement or age-based vaccination.
- Pregnancy is evaluated by gestation month. Vaccines are allowed up to about
  three months of pregnancy; during months 4 and 5, skip and catch up within two
  weeks after delivery.

## Per-Vaccine Anchor Precedence

Anchor selection is evaluated separately for every vaccine family. It is never
a single goat-wide date and it must never infer a DOB from field vaccination
history.

| Evidence available for the vaccine being scheduled | Required behavior |
| --- | --- |
| Manual anchor campaign for the same vaccine | Use the manual campaign date as the course base for the selected animals, whether it is dose 1, booster, or revaccination. Follow-on doses schedule from that anchor or its accepted completion; DOB/entry/calendar rows before the anchor must not reappear. |
| Accepted administration history for the same vaccine | Use the latest accepted administration (or accepted course completion where the matrix defines a course) plus that vaccine's configured next-dose/repeat interval. Ignore DOB/entry as due-date anchors for that vaccine. |
| No same-vaccine history, but trusted DOB is available | Use DOB only for an eligible new age-based course. Do not fabricate historical administrations. |
| No same-vaccine history or DOB, but trusted herd-entry date is available | Use entry date only for the applicable procurement/adult-primary path and its warmup constraints. |
| No same-vaccine history, DOB, or entry date | Create the approved adult catch-up/primary action for the next compatible drive. Missing identity dates alone must not defer vaccination. |

Examples:

- A goat has an accepted FMD administration on `2026-03-20`. Its next FMD due
  date is calculated from `2026-03-20` using the FMD repeat interval. Entering a
  DOB tomorrow must not change that FMD due date.
- The same goat has never received PPR. Its FMD date cannot anchor PPR. PPR uses
  a trusted DOB/entry fallback when applicable; if neither exists, PPR enters
  the approved adult catch-up path.
- A DOB/entry correction may supersede an obligation only when that obligation
  was actually anchored to the old DOB/entry value (or was a no-date catch-up
  placeholder) and there is still no accepted same-vaccine history.

Forbidden behavior:

- deriving or back-calculating DOB from vaccination dates;
- using an administration of one vaccine to anchor another vaccine;
- regenerating a DOB/entry-based primary after same-vaccine history exists;
- moving, canceling, or duplicating a completion-anchored obligation when DOB or
  entry date is later added or corrected;
- deferring vaccination solely because DOB and entry date are missing.

Required regression proof:

1. Seed a goat with no DOB/entry and an accepted same-vaccine administration.
2. Generate the next obligation from that administration date.
3. Correct DOB/entry through the canonical identity command and deliver the
   durable recheck event.
4. Assert the due date and active obligation identity remain unchanged and no
   DOB/entry-based primary or duplicate is created.
5. Separately prove that a vaccine with no history may use DOB/entry, and that a
   vaccine with none of the three anchors enters adult catch-up without a
   missing-date defer.

## Kid Course Rendering Rule

Schedule boards and drive previews must show ages where a vaccine is actually
due for the selected species/path, not every age in the animal's life. For the
approved goat/sheep kid path, the fixed due points are:

| Age | Goat due item | Sheep due item |
| ---: | --- | --- |
| 4w | ET+TT dose 1 | ET+TT dose 1 |
| 7w | ET+TT booster | ET+TT booster |
| 12w | FMD + HS | FMD + HS |
| 16w | PPR + Goat Pox | PPR + Sheep Pox + Blue Tongue dose 1 |
| 19w | - | Blue Tongue booster |

K0, K1, and early K2 have no approved-schedule vaccine due before 4w. After
19w, the fixed kid course is complete; steady-state adult/fattening vaccination
uses repeat intervals from accepted completion dates, not fixed week-number
slots. Adult procurement is a separate path: first eligible day after the
warmup hold, ET+TT dose 2 after 21 days, and pox after the 28-day live-to-live
spacing window.

## Schedule By Tag And Age

| Base tag / bucket | Goats & Parks source age | Goats & Parks meaning | Species policy | Timing anchor | Goat vaccine | Sheep vaccine | Dose / vial | Repeat / note |
| --- | --- | --- | --- | ---: | --- | --- | --- | --- |
| K0 Newborn | 1-2 days | Newborn with mother, maximum one day. | goat + sheep kids where park data allows | day 0-1 | None | None | - | No vaccination. |
| K1 Milk Training | 3-9 days | Separated from mother and trained on milk system, maximum seven days. | goat + sheep kids where park data allows | first week | None | None | - | No vaccination in the approved schedule. |
| K2 Milk Drinking | 10-77 days | Milk drinking after K1, generally about 42 days / six weeks. | goat + sheep kids where park data allows | 4w | ET+TT dose 1 | ET+TT dose 1 | ET+TT: 2 ml / vial 100 | Course continues at 7w. |
| K2 late | 10-77 days | Tail end of K2 timing. | goat + sheep kids where park data allows | 7w | ET+TT booster | ET+TT booster | ET+TT: 2 ml / vial 100 | ET+TT repeats every 6 months after the course. |
| K3 Weaning | 78-84 days | Weaning stage; milk feeding normally ends around 60-90 days. | goat + sheep kids where park data allows | 12w | FMD + HS | FMD + HS | FMD: 1 ml / vial 30; HS: 2 ml / vial 100 | FMD repeats every 9 months. HS repeats every 1 year. |
| Fattening Male / Fattening Female | 120-240 days | Post-weaning kid. | goat + sheep when tag policy allows | 16w | PPR + Goat Pox | PPR + Sheep Pox + Blue Tongue dose 1 | PPR: 1 ml / vial 100; Goat Pox: 1 ml / vial 25; Sheep Pox: 1 ml / vial 100; Blue Tongue: 2 ml / vial 100 | PPR repeats every 3 years. Blue Tongue and pox repeat every 1 year. Planner still enforces live-vaccine compatibility and max-vaccine limits. |
| Fattening Male / Fattening Female | 120-240 days | Post-weaning kid. | goat + sheep when tag policy allows | 19w | - | Blue Tongue booster | Blue Tongue: 2 ml / vial 100 | Blue Tongue booster is 21 days after the 16w first dose. |
| Fattening Male Warmup / Fattening Female Warmup | 120-240 days | Purchased kid on warmup diet before park diet. | goat + sheep when tag policy allows | age-based after 7-day hold | ET+TT 2 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25 when age-due | ET+TT 2 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Sheep Pox 1 ml / vial 100; PPR 1 ml / vial 100; Blue Tongue 2 ml / vial 100 when age-due | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; Sheep Pox 1 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Blue Tongue 2 ml / vial 100 | Kids up to 16w follow the normal age schedule after the 7-day hold. Goat Pox and Sheep Pox remain 16-week source-timing vaccines; planner compatibility decides same-day grouping. |
| Adult Warmup | 300+ days | Purchased adult on warmup diet, generally maximum 14 days. | species-specific adult tag policy | first eligible day after 7-day hold | ET+TT + PPR | ET+TT + PPR | ET+TT: 2 ml / vial 100; PPR: 1 ml / vial 100 | Pregnant month 4-5 skip overrides this procurement step. |
| Adult procured ET+TT dose 2 | 300+ days | Adult procured animal after first eligible procurement ET+TT dose. | species-specific adult tag policy | 21d after ET+TT dose 1 | ET+TT booster | ET+TT booster | ET+TT: 2 ml / vial 100 | ET+TT repeats every 6 months only after dose 2/course completion. |
| Adult procured pox step | 300+ days | Adult procured animal after first eligible procurement PPR/live dose. | species-specific adult tag policy | 4w after PPR/live dose | Goat Pox | Sheep Pox | Goat Pox: 1 ml / vial 25; Sheep Pox: 1 ml / vial 100 | The 4-week wait is the live→live spacing rule after PPR. Pox vaccines repeat every 1 year. |
| Non Pregnant / Flushing / Breeding / Buck | 300+ days | Adult steady-state tags; Buck is treated as the shared adult-male breeder tag for vaccination eligibility. | goat + sheep for these steady-state adult tags unless a future species-specific tag replaces them | repeat due | ET+TT 2 ml / vial 100 every 6 months; PPR 1 ml / vial 100 every 3 years; Goat Pox 1 ml / vial 25 every 1 year; FMD 1 ml / vial 30 every 9 months; HS 2 ml / vial 100 every 1 year | ET+TT 2 ml / vial 100 every 6 months; PPR 1 ml / vial 100 every 3 years; Blue Tongue 2 ml / vial 100 every 1 year; Sheep Pox 1 ml / vial 100 every 1 year; FMD 1 ml / vial 30 every 9 months; HS 2 ml / vial 100 every 1 year | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; Sheep Pox 1 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Blue Tongue 2 ml / vial 100 | Repeat from last accepted vaccination or accepted course completion. |
| Mother / lactating adult | 300+ days | Biological mother/lactation state after birth; sheep may produce enough milk for the kid around K0, not a commercial milking workflow. | goat + sheep biological state | repeat due | Standard adult goat repeat schedule | Standard adult sheep repeat schedule | Species-specific adult vaccines | Do not split mother rows by vaccination status; missing/unknown/not-vaccinated mother evidence is ignored for scheduling. |
| Mother Milking Waiting / Milking Warmup / Milking | 300+ days | Commercial goat-milk workflow tags. | goat/doe only unless future approved sheep dairy policy adds equivalents | repeat due | Standard adult goat repeat schedule | Not applicable | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; FMD 1 ml / vial 30; HS 2 ml / vial 100 | These tags are not sheep mother tags. |
| Pregnant Early Gestation | 300+ days | Ultrasound-confirmed pregnant females until about three months gestation. | species-specific female pregnancy tags | if due | Allowed if due | Allowed if due | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; Sheep Pox 1 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Blue Tongue 2 ml / vial 100 | Still obey same-day and gap rules. |
| Pregnant Late Gestation | 300+ days | Goats & Parks tag starts around three months; Vaccination Rules skip applies during months 4 and 5. | species-specific female pregnancy tags | month 4-5 | Skip | Skip | - | Missed vaccines catch up within two weeks after delivery. If exactly around month 3, Preventive Care must evaluate gestation month rather than skipping only from tag text. |
| ICU Milk Kids / ICU Fattening Kids / ICU Adults | 3-77 / 120-240 / 300+ days | Serious condition requiring urgent care. | species-specific health/isolation tag policy | any age | No vaccination | No vaccination | - | Resume/review after cleared. |
| Quarantine Milk Kids / Quarantine Fattening Kids / Quarantine Adults | 3-77 / 120-240 / 300+ days | Viral/isolation state such as ORF. | species-specific health/isolation tag policy | any age | No vaccination | No vaccination | - | Resume/review after cleared. |

## Vaccine Dose, Vial, Timing, And Repeat

| Vaccine | Species | Class | Course type | Dose | Vial doses | Approved timing | Repeat | Priority |
| --- | --- | --- | --- | ---: | ---: | --- | --- | ---: |
| ET+TT | Goat + sheep | Bacteria killed | Booster | 2 ml | 100 | kid 4w then 7w; adult dose 1 then dose 2 after 21d | 6 months after dose 2/course completion | 1 |
| PPR | Goat + sheep | Virus live | Single | 1 ml | 100 | 16w; adult procurement step 1 | 3 years | 2 |
| Goat Pox | Goat | Virus live | Single | 1 ml | 25 | 16w; adult procurement step 2 | 1 year | - |
| Sheep Pox | Sheep | Virus live | Single | 1 ml | 100 | 16w; adult procurement step 2 | 1 year | - |
| FMD | Goat + sheep | Virus killed | Single | 1 ml | 30 | 12w | 9 months | - |
| HS | Goat + sheep | Bacteria killed | Single | 2 ml | 100 | 12w | 1 year | - |
| Blue Tongue | Sheep | Virus killed | Booster | 2 ml | 100 | 16w, 19w | 1 year | - |

## Compatibility And Gap Rules

| Pair / situation | Decision |
| --- | --- |
| ET+TT + PPR | Allowed; adult procurement first step. |
| ET+TT booster + Goat Pox | Allowed only when both are due; ET+TT dose 2 is due after 21d, Goat Pox after the 28d live→live spacing window. |
| ET+TT booster + Sheep Pox | Allowed only when both are due; ET+TT dose 2 is due after 21d, Sheep Pox after the 28d live→live spacing window. |
| PPR + Blue Tongue | Allowed by live-viral + killed-viral same-day rule. |
| FMD + HS | Allowed by bacterial + viral same-day rule. |
| PPR + Goat Pox | Same source timing at 16w, but both are live; planner must separate them if the live-live safety rule is active. |
| Sheep Pox + Blue Tongue booster | Not a source same-day bundle. Sheep Pox is 16w; Blue Tongue booster is 19w. |
| FMD + HS at 12w | Allowed by bacterial + viral same-day rule. Sheep Pox is not a 12w vaccine. |
| Any live -> live | 4w gap mandatory. |
| Kid booster | Minimum 3w after the prior kid dose. |
| Live -> killed | 2w gap unless a same-day class rule applies. |
| Killed -> killed | 2w gap unless explicitly allowed or confirmed. |

## Source Conflict To Preserve

The Vaccination Rules source finding records PPR at 16w and Goat Pox at 16w. Both are
live vaccines, and the same source requires a 4-week gap between live vaccines.
Therefore:

- Preserve raw source truth: Goat Pox appears as 16w in the source.
- Use safe effective scheduling when PPR is administered at 16w: Goat Pox moves
  only when required by live-live spacing; the raw rule remains 16w.
- Do not hide this as a normal source value. Label it as derived from the
  live-live gap rule.

## Not In This Matrix

- BQ has no approved timing, dose, vial, repeat, or gap placement in the Vaccination Rules source finding. Keep it as label-only unless a later reviewed source adds
  schedule-bearing values.
- Imported/procurement vaccination mentions are evidence only until reconciled
  into accepted `vaccination_completions`; they are not completion truth by row
  existence.
