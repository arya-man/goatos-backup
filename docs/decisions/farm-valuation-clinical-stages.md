# An animal in ICU is still inventory

Maintainer decision, 2026-09-10.

The Farm Value cards on `/sales` valued the live herd through the animal's management stage and
dropped anything the stage vocabulary did not name into a **not-valued** list beside the cards. On
STG that list held sixteen animals, every one of them tagged `ICU-Kid`. They are alive, they are on
the farm, and they are worth what their cohort is worth -- the tag says where an animal is being
KEPT, not that it has no value. Sixteen kids is small money; a rule that silently drops clinically
housed animals is not, because it grows with every animal the farm puts in ICU.

Three rules, and nothing else about the rollup changes.

| Stage | Valued as | Why |
|---|---|---|
| `ICU-Kid` | the **K2** bucket | its own milk cohort wins when the register still knows it; K2 is the maintainer's default for the kid whose band was lost on the way into ICU |
| `ICU` | **Adult females** / **Adult males** by the animal's own sex | a plain ICU tag is an adult tag |
| `Mother` | **Adult females**, always | a mother is female whatever the sex column happens to hold |

## What the order of the branches is doing

The `CASE` stops at its first true arm, so the order IS the rule.

**A mother is claimed before the sex branches.** Otherwise a mother carrying a wrong or missing sex
would be read off the sex column instead of off the fact that she is a mother.

**An ICU kid is claimed after the milk-cohort branches.** Migration `000166` recovers a clinically
housed kid's real band from its stage history, and where that succeeded the animal already values
correctly as K1, K2 or K3. Claiming ICU kids first would flatten a known K3 to K2 -- inventing a
worse answer than the one the register already holds. The K2 fallback reaches only the kid whose
band nobody knows.

**`unmapped` stays the last resort.** A stage these rules do not name is still counted, still
excluded from the value, and still listed by name in the not-valued breakdown. The farm can see
what the software could not price.

## The stage is normalized, once

The imported herd genuinely carries both `ICU- kid` (42 rows) and `ICU-Kid` (27). Raw equality
would value one spelling and drop the other -- the same defect `000166` had to repair for milk
cohorts. The stage is matched through that migration's normalizer (upper-case, strip
non-alphanumerics) via a `CROSS JOIN LATERAL`, so there is ONE definition rather than four inline
copies drifting apart. The lateral reads no table: it is a scalar over the goat's own row, so it
cannot fan the herd out and inflate the counts it feeds.

The tags are matched LITERALLY. No `ILIKE` over kid-like or clinical-looking text -- that sweeps in
stages nobody named, which is the trap the `K0` branch was already guarded against.

Quarantine stages are deliberately NOT included. They remain visible in the not-valued breakdown
until the maintainer says what they are worth.

## Proof

Read-only against live STG (`goatos-stg`, tenant `…0001`), the same SQL the page runs:

| | before | after |
|---|---|---|
| K2 animals | 19 | 35 |
| valued animals | 1,560 | 1,576 |
| excluded animals | 16 | 0 |
| not-valued breakdown | `ICU-Kid × 16` | empty |

Every other bucket -- fattening 655, adult females 808, adult males 38, K0 0, K1 0, K3 40 -- is
byte-identical across the two runs, so the sixteen animals moved and nothing else did.

Rule: `backend/internal/sales/adapters/postgres/overview_repository.go` → `farmValuationSQL`.
Pinned by `TestFarmValuationClinicalStagesAreValuedThroughTheirCohort` (asserts the branch ORDER,
mutation-tested by moving `Mother` after the sex branches and `ICU-Kid` before the cohort
branches), `TestFarmValuationNormalizesTheClinicalStageOnce` (mutation-tested by inlining a second
copy of the normalizer) and
`TestFarmValuationClinicalStagesMultipleDimensionsPageBoundaryParkScopeEveryStatus` (the aggregate
cover: no fan-out, no page window, scope predicates intact, terminal statuses still excluded).
