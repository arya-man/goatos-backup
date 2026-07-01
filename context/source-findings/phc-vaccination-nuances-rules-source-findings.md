# PHC Vaccination Nuance Rules Source Findings

Date reviewed: 2026-07-02

Source reviewed: `wiki/Nuances_Rules.docx`

Status: sanitized in-repo source finding for PHC Vaccination schedule, dosage,
vial, repeat, procurement, pregnancy, and compatibility/gap rules. Do not commit
the raw DOCX, screenshots, local paths, or private media. If a later source owner
changes these values, create a reviewed source-backed version rather than
overriding them in frontend code or seed data.

This finding is the resolvable in-repo source for vaccine rules. Use it with
`context/source-findings/goats-and-parks-source-findings.md`, which remains the
base source for shed tag, lifecycle, warmup, pregnancy, ICU, and quarantine
semantics.

## Vaccine Classes By Species

| Species | Vaccine | Class |
| --- | --- | --- |
| Goat | ET+TT | Bacteria killed |
| Goat | PPR | Virus live |
| Goat | Goat Pox | Virus live |
| Goat | FMD | Virus killed |
| Goat | HS | Bacteria killed |
| Sheep | ET+TT | Bacteria killed |
| Sheep | PPR | Virus live |
| Sheep | Blue Tongue | Virus killed |
| Sheep | Sheep Pox | Virus live |
| Sheep | FMD | Virus killed |
| Sheep | HS | Bacteria killed |

## Approved Timing, Repeat, And Priority

| Vaccine | Species | Approved timing | Repeat | Priority |
| --- | --- | --- | --- | ---: |
| ET+TT | Goat + sheep | 4w, 7w | 6 months | 1 |
| PPR | Goat + sheep | 16w | 3 years | 2 |
| Blue Tongue | Sheep | 16w, 20w | 1 year | - |
| Goat Pox | Goat | 16w raw; 20w derived when PPR is administered at 16w | 1 year | - |
| Sheep Pox | Sheep | 12w | 1 year | - |
| FMD | Goat + sheep | 12w | 9 months | - |
| HS | Goat + sheep | 12w | 1 year | - |

## Dose And Vial

| Vaccine | Course type | Dose | Vial doses |
| --- | --- | ---: | ---: |
| PPR | Single | 1 ml | 100 |
| ET+TT | Booster | 2 ml | 100 |
| Blue Tongue | Booster | 2 ml | 100 |
| Goat Pox | Single | 1 ml | 25 |
| Sheep Pox | Single | 1 ml | 100 |
| FMD | Single | 1 ml | 30 |
| HS | Single | 2 ml | 100 |

## Additional Rules

- Procured or warmup animals have a 7-day no-vaccination hold after entry.
- Spacing between two live vaccines must be 4 weeks.
- A bacterial and viral vaccine can be combined on the same day.
- A live viral and killed viral vaccine can be combined on the same day.
- Quarantine and ICU animals do not receive vaccination until cleared.

## Gap Rules

| Situation | Rule |
| --- | --- |
| Live vaccine followed by killed vaccine | Keep a 2-week gap unless a same-day class rule applies. |
| Killed vaccine followed by killed vaccine | Keep a 2-week gap unless explicitly allowed or confirmed. |
| Live vaccine followed by live vaccine | Keep a 4-week gap. |
| Kid booster | Keep at least a 3-week gap after the prior kid dose. |

## Pregnancy Rule

- Goat/sheep pregnancy is around 5 months.
- Vaccines can be given up to 3 months of pregnancy.
- During months 4 and 5 of pregnancy, vaccination can be skipped.
- After delivery, missed vaccines can be given within 2 weeks.

## Procurement Rule

- Adult procured goats/sheep can be vaccinated at procurement.
- Breeding and fattening goats/sheep can be vaccinated at source.
- Kids up to 16 weeks must follow the normal vaccination schedule.
- For newly procured adult goats/sheep, give ET+TT + PPR first.
- Wait 4 weeks, then give Goat Pox for goats or Sheep Pox for sheep with the
  ET+TT booster.

## Source Conflict To Preserve

The source timing puts PPR at 16w and Goat Pox at 16w. Both are live vaccines,
and the same source requires a 4-week live-live gap. Therefore Goat Pox remains
recorded as 16w raw source timing, but the effective schedule moves Goat Pox to
20w when PPR is administered at 16w.

## Kid Course Interpretation For GoatOS

The kid schedule is a fixed primary course with due points at 4w, 7w, 12w, 16w,
and, where applicable, 20w. It is not an instruction to render every week of
life as a drive day. There is no approved-schedule vaccine before 4w, and after
the 20w due point the animal moves to steady-state repeat scheduling driven by
accepted completion dates. Adult procurement remains a separate source rule:
ET+TT + PPR first, then 4w later pox + ET+TT booster.

## GoatOS Implication

- `docs/phc-vaccination/APPROVED-SCHEDULE-MATRIX.md` is the assembled schedule
  view that combines this vaccine-rule finding with the Goats and Parks
  stage/tag finding.
- Due generation must use DOB or herd-entry date plus accepted vaccination
  completion history, not only current shed tag text.
- Repeat cycles must start from the actual accepted vaccination date, or from
  accepted course completion for booster courses.
- Imported or procurement vaccination mentions are evidence only until
  reconciled into accepted `vaccination_completions`.
