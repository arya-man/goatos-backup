# PHC Vaccination Approved Schedule Matrix

Date reviewed: 2026-07-02

Base tag source: `wiki/Goats and Parks.docx`
Vaccine rule source: `wiki/Nuances_Rules.docx`

This is the schedule matrix to use for PHC Vaccination authoring, review, seed
data, tests, and product discussion. `Goats and Parks.docx` is the base for
shed tags, lifecycle/stage, warmup, pregnancy, ICU, and quarantine. `Nuances_Rules.docx`
overlays vaccine timing, dose, vial, repeat, procurement, pregnancy, and gap
rules.

The alternate early-kid source branch is intentionally excluded from this file.
Do not add it to config, tests, UI tables, source comparisons, catch-up logic, or
seed data unless Ravi explicitly reopens that branch.

## Operating Rules

- Tags are housing/lifecycle context; vaccine due dates are driven by DOB or
  herd-entry date plus the last accepted vaccination completion.
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

## Schedule By Tag And Age

| Base tag / bucket | Goats & Parks meaning | Timing anchor | Goat vaccine | Sheep vaccine | Dose / vial | Repeat / note |
| --- | --- | ---: | --- | --- | --- | --- |
| K0 Newborn | Newborn with mother, maximum one day. | day 0-1 | None | None | - | No vaccination. |
| K1 Milk Training | Separated from mother and trained on milk system, maximum seven days. | first week | None | None | - | No vaccination in the approved schedule. |
| K2 Milk Drinking | Milk drinking after K1, generally about 42 days / six weeks. | 4w | ET+TT dose 1 | ET+TT dose 1 | ET+TT: 2 ml / vial 100 | Course continues at 7w. |
| K2 late | Tail end of K2 timing. | 7w | ET+TT booster | ET+TT booster | ET+TT: 2 ml / vial 100 | ET+TT repeats every 6 months after the course. |
| K3 Weaning | Weaning stage; milk feeding normally ends around 60-90 days. | 12w | FMD + HS | FMD + HS; Sheep Pox also due | FMD: 1 ml / vial 30; HS: 2 ml / vial 100; Sheep Pox: 1 ml / vial 100 | FMD repeats every 9 months. HS and Sheep Pox repeat every 1 year. Sheep 3-way administration is pairwise-legal but PHC should confirm before treating it as an approved 3-way drive. |
| Fattening Male / Fattening Female | Post-weaning kid. | 16w | PPR; Goat Pox raw source also says 16w | PPR + Blue Tongue dose 1 | PPR: 1 ml / vial 100; Goat Pox: 1 ml / vial 25; Blue Tongue: 2 ml / vial 100 | PPR repeats every 3 years. Blue Tongue repeats every 1 year. Goat Pox has a live-live conflict if PPR is also administered at 16w. |
| Fattening Male / Fattening Female | Post-weaning kid. | 20w derived | Goat Pox if PPR was administered at 16w | Blue Tongue booster | Goat Pox: 1 ml / vial 25; Blue Tongue: 2 ml / vial 100 | Goat Pox is moved to 20w only to satisfy the mandatory 4-week live-live gap after PPR. Goat Pox repeats every 1 year. |
| Fattening Male Warmup / Fattening Female Warmup | Purchased kid on warmup diet before park diet. | age-based after 7-day hold | ET+TT 2 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25 when age-due | ET+TT 2 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Sheep Pox 1 ml / vial 100; PPR 1 ml / vial 100; Blue Tongue 2 ml / vial 100 when age-due | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; Sheep Pox 1 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Blue Tongue 2 ml / vial 100 | Kids up to 16w follow the normal age schedule after the 7-day hold. If PPR was given at 16w, Goat Pox moves to 20w. |
| Adult Warmup | Purchased adult on warmup diet, generally maximum 14 days. | first eligible day after 7-day hold | ET+TT + PPR | ET+TT + PPR | ET+TT: 2 ml / vial 100; PPR: 1 ml / vial 100 | Pregnant month 4-5 skip overrides this procurement step. |
| Adult procured step 2 | Adult procured animal after first eligible procurement step. | 4w after first adult step | ET+TT booster + Goat Pox | ET+TT booster + Sheep Pox | ET+TT: 2 ml / vial 100; Goat Pox: 1 ml / vial 25; Sheep Pox: 1 ml / vial 100 | Exact adult procurement rule from Nuance. ET+TT repeats every 6 months after course; pox vaccines repeat every 1 year. |
| Non Pregnant / Flushing / Breeding / Mother / Mother Milking Waiting / Milking Warmup / Milking / Buck | Adult steady-state tags. | repeat due | ET+TT 2 ml / vial 100 every 6 months; PPR 1 ml / vial 100 every 3 years; Goat Pox 1 ml / vial 25 every 1 year; FMD 1 ml / vial 30 every 9 months; HS 2 ml / vial 100 every 1 year | ET+TT 2 ml / vial 100 every 6 months; PPR 1 ml / vial 100 every 3 years; Blue Tongue 2 ml / vial 100 every 1 year; Sheep Pox 1 ml / vial 100 every 1 year; FMD 1 ml / vial 30 every 9 months; HS 2 ml / vial 100 every 1 year | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; Sheep Pox 1 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Blue Tongue 2 ml / vial 100 | Repeat from last accepted vaccination or accepted course completion. |
| Pregnant Early Gestation | Ultrasound-confirmed pregnant females until about three months gestation. | if due | Allowed if due | Allowed if due | ET+TT 2 ml / vial 100; PPR 1 ml / vial 100; Goat Pox 1 ml / vial 25; Sheep Pox 1 ml / vial 100; FMD 1 ml / vial 30; HS 2 ml / vial 100; Blue Tongue 2 ml / vial 100 | Still obey same-day and gap rules. |
| Pregnant Late Gestation | Goats & Parks tag starts around three months; Nuance skip applies during months 4 and 5. | month 4-5 | Skip | Skip | - | Missed vaccines catch up within two weeks after delivery. If exactly around month 3, PHC must evaluate gestation month rather than skipping only from tag text. |
| ICU Milk Kids / ICU Fattening Kids / ICU Adults | Serious condition requiring urgent care. | any age | No vaccination | No vaccination | - | Resume/review after cleared. |
| Quarantine Milk Kids / Quarantine Fattening Kids / Quarantine Adults | Viral/isolation state such as ORF. | any age | No vaccination | No vaccination | - | Resume/review after cleared. |

## Vaccine Dose, Vial, Timing, And Repeat

| Vaccine | Species | Class | Course type | Dose | Vial doses | Approved timing | Repeat | Priority |
| --- | --- | --- | --- | ---: | ---: | --- | --- | ---: |
| ET+TT | Goat + sheep | Bacteria killed | Booster | 2 ml | 100 | 4w, 7w | 6 months | 1 |
| PPR | Goat + sheep | Virus live | Single | 1 ml | 100 | 16w; adult procurement step 1 | 3 years | 2 |
| Goat Pox | Goat | Virus live | Single | 1 ml | 25 | 16w raw source; 20w derived if PPR was administered at 16w; adult procurement step 2 | 1 year | - |
| Sheep Pox | Sheep | Virus live | Single | 1 ml | 100 | 12w; adult procurement step 2 | 1 year | - |
| FMD | Goat + sheep | Virus killed | Single | 1 ml | 30 | 12w | 9 months | - |
| HS | Goat + sheep | Bacteria killed | Single | 2 ml | 100 | 12w | 1 year | - |
| Blue Tongue | Sheep | Virus killed | Booster | 2 ml | 100 | 16w, 20w | 1 year | - |

## Compatibility And Gap Rules

| Pair / situation | Decision |
| --- | --- |
| ET+TT + PPR | Allowed; adult procurement first step. |
| ET+TT booster + Goat Pox | Allowed; adult goat second step after 4w. |
| ET+TT booster + Sheep Pox | Allowed; adult sheep second step after 4w. |
| PPR + Blue Tongue | Allowed by live-viral + killed-viral same-day rule. |
| FMD + HS | Allowed by bacterial + viral same-day rule. |
| PPR + Goat Pox | Not same day; both are live, so a 4w gap is mandatory. |
| Sheep Pox + Blue Tongue booster | Not a source same-day bundle. Sheep Pox is 12w; Blue Tongue booster is 20w. |
| FMD + HS + Sheep Pox at 12w sheep | Pairwise legal, but 3-way administration should be PHC-confirmed before product or drive logic treats it as approved. |
| Any live -> live | 4w gap mandatory. |
| Kid booster | Minimum 3w after the prior kid dose. |
| Live -> killed | 2w gap unless a same-day class rule applies. |
| Killed -> killed | 2w gap unless explicitly allowed or confirmed. |

## Source Conflict To Preserve

Nuance raw timing lists PPR at 16w and Goat Pox at 16w. Both are live vaccines,
and Nuance also requires a 4-week gap between live vaccines. Therefore:

- Preserve raw source truth: Goat Pox appears as 16w in the source.
- Use safe effective scheduling when PPR is administered at 16w: Goat Pox moves
  to 20w.
- Do not hide this as a normal source value. Label it as derived from the
  live-live gap rule.

## Not In This Matrix

- BQ has no approved timing, dose, vial, repeat, or gap placement in
  `Nuances_Rules.docx`. Keep it as label-only unless a later reviewed source
  adds schedule-bearing values.
- Imported/procurement vaccination mentions are evidence only until reconciled
  into accepted `vaccination_completions`; they are not completion truth by row
  existence.
