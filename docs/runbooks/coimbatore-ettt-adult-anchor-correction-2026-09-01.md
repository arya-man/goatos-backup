# Coimbatore ET+TT Adult Anchor Correction - 2026-09-01

## Operator Decision

For Coimbatore adults, ET+TT remains a two-dose adult course. Dose 2 is the
course-completion anchor. Future ET+TT revaccination scheduling must chain from
the adult dose 2 anchor.

Do not treat adults who have dose 2 but no recorded dose 1 as CEO-dashboard
"exceptions" for this correction. They are part of the adult ET+TT course anchor
population.

## Scope

- Tenant: `00000000-0000-4000-8000-000000000001`
- Park: Coimbatore / `CBE` / `00000000-0000-4000-8000-000000003001`
- Cohort: adult management stages `Non-Pregnant`, `Buck`, `Mother`
- Vaccine: ET+TT
- Anchor dose: `et_tt_adult_w2`
- Corrected anchor date: `2026-08-01`

## Pre-Correction STG State

Read-only inspection on 2026-09-01 showed 485 Coimbatore adult animals in scope.

Existing accepted ET+TT adult completions:

| Dose code | Administered date | Animals |
| --- | ---: | ---: |
| `et_tt_adult_w1` | 2026-06-30 | 146 |
| `et_tt_adult_w1` | 2026-07-01 | 293 |
| `et_tt_adult_w2` | 2026-08-01 | 280 |
| `et_tt_adult_w2` | 2026-08-05 | 134 |

Derived pre-correction groups:

| Group | Animals |
| --- | ---: |
| Adult cohort total | 485 |
| Accepted dose 1 | 439 |
| Accepted dose 2 | 414 |
| Accepted both dose 1 and dose 2 | 369 |
| Accepted dose 2 but no accepted dose 1 | 45 |
| Neither accepted dose 1 nor accepted dose 2 | 1 |

The single neither-dose animal:

| Display ID | RFID/tag 1 | RFID/tag 2 | Shed | Stage | Sex | DOB |
| --- | --- | --- | --- | --- | --- | --- |
| `G-002503` | `901007000504807` | `901007000504774` | Mandela 1 | Non-Pregnant | female | 2025-06-12 |

The 45 dose2-only adults were all accepted on 2026-08-01 and had no linked
animal-level vaccination proof refs in `pc_care_task_animals` during inspection.
Shed split: Godel 2 = 42, Gandhi = 1, Godel 1 = 1, Mandela 1 = 1.

## Intended Data Correction

Set/ensure every in-scope Coimbatore adult has an accepted `et_tt_adult_w2`
course-completion anchor dated `2026-08-01`.

This means:

- Keep dose 1/dose 2 course semantics.
- Use adult dose 2 as the revaccination anchor.
- Move the 134 existing `et_tt_adult_w2` accepted completions dated
  `2026-08-05` to `2026-08-01`.
- Add or otherwise materialize the missing `et_tt_adult_w2` accepted anchor for
  `G-002503` on `2026-08-01`.
- Preserve the previous state in this document and DB audit rows before/after
  mutation.

## Applied STG Correction

Applied on 2026-09-01 against `goatos-stg`.

DB mutations:

| Change | Rows |
| --- | ---: |
| Existing `et_tt_adult_w2` completions moved from `2026-08-05` to `2026-08-01` | 134 |
| Existing matching obligation rows moved to the same completed/due timestamp | 134 |
| Missing accepted `et_tt_adult_w2` anchor inserted for `G-002503` | 1 |
| Remaining Coimbatore adults without accepted `et_tt_adult_w2` anchors inserted | 70 |
| Existing `et_tt_revac` rows reopened/rescheduled from the dose 2 anchor | 414 |
| Missing `et_tt_revac` rows inserted from the dose 2 anchor | 71 |

Final checked state:

| Check | Result |
| --- | ---: |
| Adult cohort total | 485 |
| Adults with accepted `et_tt_adult_w2` | 485 |
| Adults with accepted `et_tt_adult_w2` on `2026-08-01` | 485 |
| Adults missing accepted `et_tt_adult_w2` | 0 |
| Scheduled `et_tt_revac` rows due `2027-01-30` | 485 |
| Adults missing scheduled `et_tt_revac` due `2027-01-30` | 0 |

Audit rows were inserted with trace IDs:

- `manual:cbe-ettt-adult-dose2-anchor:2026-08-01`
- `manual:cbe-ettt-adult-dose2-anchor-backfill:2026-08-01`
- `manual:cbe-ettt-revac-from-dose2-anchor:2026-08-01`

The code change paired with this correction removes the CEO command-board
dose-sequence exception read/render path. The adult ET+TT course remains a
two-dose course; the accepted adult dose 2 date is the course-completion anchor
for future revaccination scheduling.

Current scheduler behavior as verified on 2026-09-01: accepted
`et_tt_adult_w2` creates the next `et_tt_revac` obligation 182 days later. The
published scheduler repeats that same `et_tt_revac` rule after an accepted
revac completion; it does not currently materialize a separate visible
`et_tt_adult_w2` row three weeks after each future revac completion.
