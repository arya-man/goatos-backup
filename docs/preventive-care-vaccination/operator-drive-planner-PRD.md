# Preventive Care Vaccination Operator Drive Planner PRD

**Status:** Draft v1  
**Date:** 2026-07-22  
**Owner:** Preventive Care / Goat OS  
**Scope:** Vaccination drive planning logic for all parks, animal stages, and
species. The current implementation proof uses the provided CPT adult seed files
as the first data slice, but the product logic must remain generic across parks,
kids/adults, goats/sheep, procurement cohorts, pregnancy/lactation states, and
future seed sources. This PRD defines how Goat OS turns already-generated
vaccination obligations into date-wise, operator-wise, shed-wise assignments.

## 1. Problem

Vaccination work is currently easy to mis-plan because eligible vaccines,
eligible animals, sheds, partitions, operator capacity, weekly availability,
leave, and clinical defer states all interact.

The vaccination rules engine remains the medical source of truth. Adult and kid
rules, booster gaps, repeat cycles, plus-one-week buffers, vaccine compatibility,
and animal-state deferrals stay exactly in the rule layer. The planner must not
change whether an animal is due; it only decides who should handle the animal,
where, and on which date.

The planner must not think in vaccine doses alone. One animal may need two
vaccines in the same drive, but the operator's practical workload cap is how
many unique animals they physically handle that day.

The planner must also respect farm reality: sheds are physical buildings, and
some shed names represent partitions inside a building, for example `Gandhi 1`,
`Gandhi 2`, `Gandhi 3` are partitions under physical shed `Gandhi`. Operators
should usually own whole shed/partition blocks so the field team can execute the
MOP cleanly without duplicate scans, missed animals, or confused proof capture.

## 2. Goals

- Generate a drive plan by date, operator, physical shed, partition, animal
  count, and vaccine bundle.
- Preserve the existing vaccination obligation kernel for all kid/adult,
  species, dose, booster, repeat, buffer, and medical defer rules.
- Count operator capacity by unique animals handled per day, not vaccine dose
  count or obligation row count.
- Automatically use the operators available on each drive date based on the
  weekly timetable, leave, and role eligibility.
- Keep every physical shed at or below one operator's full configured cap intact,
  even when it does not fit the current day's residual slots; carry it to the
  next operator-day.
- Apply that boundary before batch creation and across rule rows: initial/catch-up
  and history-backed repeat instructions for the same vaccine cannot divide one
  physical shed into different operator-days.
- Split by partition only when the physical shed itself exceeds one operator's
  full configured cap, then split inside a partition only when that partition
  also exceeds the cap.
- Spill work to later dates when total eligible animals exceed that day's
  available operator capacity.
- Recompute operator availability and capacity independently on each spillover
  date.
- Exclude or defer animals that should not be vaccinated because of clinical,
  reproductive, quarantine, ICU, recovery, or data-quality states.
- Make every exception visible to the Preventive Care Director / Park Head
  instead of silently dropping work.

## 3. Non-Goals

- This PRD does not redefine vaccine medical rules, dose gaps, vaccine
  compatibility, or revaccination cycles. Those remain in
  `vaccination-rules.md` and the published protocol matrix.
- This PRD does not create a generic workforce scheduling system.
- This PRD does not assign hourly time slots. Planning grain is date-level
  unless a later MOP explicitly adds time windows.
- This PRD does not treat the Preventive Care Director as a default field
  operator. Directors supervise, approve, and monitor across parks unless
  manually assigned as an operator.

## 4. Core Definitions

| Term | Meaning |
|---|---|
| Eligible animal | A unique animal with at least one vaccination obligation that can be performed on the drive date. |
| Vaccine bundle | The one or more vaccines that can safely be administered to the animal in the same drive visit. |
| Operator cap | Maximum unique animals an operator can handle on one date. |
| Daily capacity | Sum of caps for operators available on that date. |
| Physical shed | A real building or major shed grouping. |
| Partition | A sub-area inside a physical shed, usually encoded in source names like `Gandhi 1` or `Godel 1 - Part 4`. |
| Work block | A schedulable unit: usually physical shed + partition + vaccine bundle + species group. |
| Review hold | Animals that cannot be auto-scheduled because required facts are missing or contradictory. |

## 5. Source Inputs

### 5.1 Animal and Vaccination Inputs

The provided CPT adult JSON files are the seed/proof data for this slice only. They must not narrow the long-term planner to CPT-only or adult-only. Future CBE, kid, procurement, pregnancy, lactation, mixed-stage, and other park/cohort inputs must flow through the same assignment rules once their eligibility obligations are generated.

The planner consumes already-computed vaccination eligibility from the protocol
engine. It must not reimplement vaccine gap logic in the assignment layer.

Required animal facts:

- `animal_id`
- species
- current park
- current shed/location
- partition or partition-derived shed name
- operational stage / shed tag
- lifecycle status
- health status
- reproductive status
- pregnancy / lactation / breeding hold facts where available
- accepted vaccination obligations and allowed same-drive bundle

### 5.2 CPT Adult Seed / Mock Run

This seed proves the planner using the provided local source files:

- `CPT-Adult-goats.json`
- `CPT-Adult-vaccination.json`
- `CPT_Nuanced Timetable.xlsx`

This task must not load CBE animal seed data. CBE remains a future park input for
the same generic planner once CBE source data is intentionally selected.

Seed counts:

| Measure | Count |
|---|---:|
| Animal rows in goats JSON | 324 |
| Vaccination rows after headers | 324 |
| Source animals in the CPT proof packet | 324 |
| Review holds / incomplete rows | 0 |

Physical shed normalization for the mock run:

| Physical shed | Source partitions | Total animals | Clean schedulable |
|---|---|---:|---:|
| Gandhi | `Gandhi 1`, `Gandhi 2`, `Gandhi 3` | 114 | 114 |
| Godel 1 | `Godel 1 - Part 1`, `Godel 1 - Part 3`, `Godel 1 - Part 4` | 120 | 120 |
| Godel 2 | `Godel 2 - Part 4` | 32 | 32 |
| Mandela 2 | `Mandela 2 - Part 1`, `Part 2`, `Part 3`, `Part 7`, `Part 8` | 47 | 47 |
| Old Yashoda | `Old Yashoda 1`, `Old Yashoda 5` | 11 | 11 |

Current proof operators:

| Person | Role | Execution capacity | Week-off from timetable |
|---|---|---:|---|
| Amit Kumar | Vaccination Operator | Yes | Friday |
| Darshan Talwar | Vaccination Operator | Yes | Sunday |
| Sagar Mahoor | Vaccination Operator | Yes | Saturday |
| Chandrakant / Chandrakanth | Preventive Care Director | No by default | No operator timetable |

Example July 23 drive plan with cap `200` animals/operator/day:

| Date | Operator | Physical shed(s) | Partition(s) | Animals |
|---|---|---|---|---:|
| 2026-07-23 | Amit Kumar | Gandhi | 1, 2, 3 | 114 |
| 2026-07-23 | Darshan Talwar | Godel 1 | Part 1, Part 3, Part 4 | 120 |
| 2026-07-23 | Sagar Mahoor | Godel 2, Mandela 2, Old Yashoda | mixed | 90 |

This plan is preferred over `200 / 124 / 0` because all three available
operators receive meaningful work while physical shed/partition integrity is
preserved.

### 5.3 Operator Inputs

Required operator facts:

- operator identity
- role/capability: can execute vaccination or not
- park scope grants
- weekly timetable presence by weekday
- date-specific leave / absence override
- date-specific manual inclusion / exclusion override
- per-day animal cap

The weekly timetable is a weekday pattern unless a dated roster or leave source
overrides it.

### 5.4 Shed/Partition Name Normalization

The planner must normalize common partition patterns:

| Raw name | Physical shed | Partition |
|---|---|---|
| `Gandhi 1` | `Gandhi` | `1` |
| `Gandhi 2` | `Gandhi` | `2` |
| `Godel 1 - Part 4` | `Godel 1` | `Part 4` |
| `Mandela 2 - Part 8` | `Mandela 2` | `Part 8` |
| `Old Yashoda 5` | `Old Yashoda` | `5` |

Normalization must preserve the raw source label for audit and proof matching.

## 6. Assignment Rules

### 6.1 Capacity Grain

Operator cap is consumed by unique animals.

Example: if a sheep receives `ET+TT Booster + PPR` in one visit, that consumes
one operator animal slot, not two vaccine slots.

The default proof-slice cap is `200` unique animals per operator per business
date unless a reviewed published config changes it. That number must be visible
and editable in Config as
`Animals per operator per day`, then published into the drive planner config.

### 6.2 Operator Availability

For each planned date:

1. Convert the date to the local park business weekday.
2. Read operators present in the weekly timetable for that weekday.
3. Remove operators on approved leave or manual exclusion.
4. Remove people without vaccination execution role/scope for the park.
5. Apply manual inclusion only if the person has the required execution role.
6. Compute daily capacity from the remaining operators.

If no operator is available, the date is skipped and the planner tries the next
date within the drive planning horizon.

### 6.3 Fairness

The planner should not fill two operators to cap while leaving another available
operator at zero when there is enough meaningful work.

The goal is reasonable fairness, not exact mathematical equality. A plan like
`114 / 118 / 89` is acceptable when it keeps sheds intact. A plan like
`200 / 121 / 0` is not acceptable when the third operator is available and can
execute meaningful work.

Fairness rules:

- Use all available operators when remaining work is large enough to create
  meaningful assignments.
- Prefer shed/partition integrity over perfect equality.
- Avoid tiny artificial splits just to make totals identical.
- A configurable `minimum_meaningful_assignment_animals` should prevent silly
  splits for very small spillover work.
- The last day of a spillover may use fewer operators if remaining work is tiny.

### 6.4 Shed and Partition Integrity

Assignment priority:

1. Keep a physical shed with one operator whenever the complete shed fits the
   full configured per-operator cap, normally 200 animals. Residual capacity
   never justifies a split. Count the shed across every compatible catch-up/
   repeat rule row in the drive.
2. If a physical shed exceeds one operator's cap, split by partition.
3. If a partition exceeds one operator's cap, split the partition by animal list.
4. If all work fits without splitting partitions, do not split partitions.
5. Never split an individual animal or its vaccine bundle.

Common parent partitions should move together whenever the parent physical shed
fits the cap. For example, do not peel `Godel 1 - Part 4` away from the rest of
`Godel 1` just to fill residual capacity.

### 6.5 Multi-Day Spillover

If total eligible animals for a date exceed total available operator capacity,
the planner schedules as much as possible that date and carries remaining work
forward.

For each spillover date, the planner must recompute:

- weekday
- available operators
- leave
- daily capacity
- deferred/recovered animal eligibility
- open obligations still eligible

The second day is not a continuation with the same people by default. It is a
fresh date-level assignment using that day's actual availability.

### 6.6 Safe-Window Priority and Breach

Medical windows remain owned by the rule engine. The drive planner consumes each
animal's due/eligible date and latest safe date, including the plus-one-week
buffer where applicable.

When capacity is constrained, assignment priority is:

1. Animals already overdue or closest to crossing `latest_safe_date`.
2. Animals whose latest safe date is earlier than other work in the same shed.
3. Shed/partition integrity and operator balancing among animals with similar
   urgency.

If available operator capacity cannot schedule a medically eligible animal
before its latest safe date, the planner must not silently push it later and
must not treat escalation as a substitute for vaccination. It must emit a hard
capacity breach with the animal count, shed/partition, missed latest-safe date,
and required operational override.

Allowed resolutions:

- add more available operators for that date;
- approve overtime/over-cap for specific operators;
- add an emergency drive date before the latest safe date;
- manually defer only when a medical rule justifies defer.

If none of the above is done, eligible animals crossing the latest safe date must
be scheduled over cap and finished. The operator cap is the normal planning
constraint; the latest-safe medical window is the hard deadline for eligible
animals.

Over-cap completion is never allowed to bypass medical hard blocks. Animals that
are sick, ICU, quarantined, under treatment, in blocked pregnancy windows, dead,
sold, culled, lost, or otherwise terminal/unsafe remain deferred, closed, or held
per the vaccination rule engine.

## 7. Scheduling Algorithm

For each park and drive window:

1. Build eligible animal list from open vaccination obligations and medical
   compatibility rules.
2. Bundle same-visit vaccines per animal.
3. Remove animals blocked by clinical/defer/data-quality rules into review or
   deferred buckets.
4. Normalize shed and partition names.
5. Group clean eligible animals first by `park + physical_shed`, then retain
   partition, species, vaccine bundle, and dose-instruction breakdowns inside
   that indivisible shed group.
6. For the target date, build the available operator pool.
7. Sort whole-shed groups by the park's canonical field route, keeping partitions
   and rule rows attached. CPT route order is `Gandhi`, `Godel 1`, `Godel 2`,
   `Mandela 2`, `Old Yashoda`.
8. Assign canonical-route physical shed groups to available operators.
9. Assign remaining smaller sheds to the currently lowest-loaded operator.
10. Check caps. If an assignment exceeds cap, split at partition boundary first,
    then within partition only if required.
11. Check fairness. If an available operator has zero meaningful work while
    others have large assignments, move the smallest coherent shed/partition
    block to that operator.
12. If daily capacity is exhausted, carry unscheduled blocks to the next date
    and repeat from operator availability.
13. Emit plan, review holds, defers, and capacity warnings.

With CPT's 324-adult clone and one 200-animal operator, this deterministic route
produces Day 1 = Gandhi 114 + Godel 2 32 + Mandela 2 47 = 193 and Day 2 =
Godel 1 120 + Old Yashoda 11 = 131. Both rows carry the same logical drive name
and the backend-owned `drive_total` is 324. Calendar must display both
`drive_name` and `drive_total`; it must not present the two operator-days as
unrelated drives or derive the total from only the currently visible row.

## 8. Animal State and Defer Rules

The planner must never schedule animals that are not medically or operationally
ready. These animals remain visible with a reason and next action.

| State / Condition | Planner Behavior |
|---|---|
| Sick | Defer. Recheck when recovered/cleared. |
| Under treatment | Defer. Do not vaccinate until treatment/recovery state allows. |
| ICU | Defer. No vaccination scheduling. |
| Quarantine | Defer. No vaccination scheduling until quarantine exits. |
| Recovering | Defer or schedule only after recovery-ready date, based on protocol rules. |
| Pregnancy month 4-5 | Skip/defer per vaccination rules. |
| Pregnancy month 1-3 | Eligible only if vaccination rules allow and other gates pass. |
| Recent breeding hold | Defer at least one month from breeding date when rule applies. |
| Milking department | Avoid during milking period when rule applies. |
| Recently procured / warm-up | Defer until warm-up hold completes. |
| Dead / sold / transferred terminal state | Do not schedule; close/cancel open work as appropriate. |
| Missing species | Review hold; cannot choose species-specific vaccine safely. |
| Missing current shed/location | Review hold; cannot assign to shed/operator route. |
| Ambiguous partition/shed mapping | Review hold or assign to raw shed with warning, based on severity. |
| Missing RFID / scan identifier | Review hold unless field execution has an approved alternate identity MOP. |
| Vaccine history conflict | Review hold; do not silently suppress or duplicate work. |

Deferred animals must re-enter planning automatically when their blocking state
clears and their vaccination window remains valid or becomes catch-up eligible.

## 9. Operator Leave and Day-Off Edge Cases

| Case | Expected Behavior |
|---|---|
| Operator is not listed on weekday timetable | Treat as unavailable unless manually included. |
| Operator is on approved leave | Remove from that date even if timetable lists them. |
| Operator leaves after plan is published | Re-plan remaining unstarted work for that date or next date. |
| Operator completes partial shed then leaves | Completed animals stay completed; remaining animals are reallocated. |
| Only one operator available | Use that operator up to cap, spill remainder. |
| No operators available | Skip date, alert planner/director, try next date. |
| Director available but not operator | Do not count in capacity unless explicitly assigned execution role. |
| Cross-park director | Can supervise/view multiple parks; not default execution capacity. |

## 10. Shed/Partition Edge Cases

| Case | Expected Behavior |
|---|---|
| Physical shed total is below cap | Prefer assigning whole shed to one operator. |
| Physical shed total exceeds cap | Split by partitions. |
| One partition exceeds cap | Split within partition by animal list. |
| Many tiny partitions | Combine them for one operator if physical movement is sensible. |
| Mixed vaccine bundles in same partition | Keep same operator if cap allows; show bundle breakdown. |
| Mixed species in same physical area | Schedule only species-safe bundles; adult execution groups remain species-specific inside the same park visit. |
| Animals physically mixed across partitions | Assign by current recorded partition but require field MOP scan verification and exception capture. |
| Shed name parse fails | Preserve raw label; send warning to data-quality queue. |

## 11. Output Requirements

The planner must output:

- date
- park
- operator
- physical shed
- partition
- animal count
- species breakdown
- vaccine bundle breakdown
- cap consumed
- cap remaining
- review holds
- deferred animal count by reason
- spillover date and remaining count when applicable
- warnings for forced splits or data-quality issues

Example output shape:

| Date | Operator | Shed | Partition | Animals | Bundle |
|---|---|---|---|---:|---|
| 2026-07-23 | Amit | Gandhi | 1, 2, 3 | 114 | species/bundle breakdown |
| 2026-07-23 | Darshan | Godel 1 | Part 1, 3, 4 | 120 | species/bundle breakdown |
| 2026-07-23 | Sagar | Godel 2, Mandela 2, Old Yashoda | mixed | 90 | species/bundle breakdown |

## 12. UX Requirements

Planner UI must show:

- who is available on each date and why
- who is unavailable and why, such as weekly off or leave
- operator cap and current load
- shed/partition blocks before and after assignment
- animals held for review with reason
- deferred animals with reason and expected re-entry trigger
- spillover days with recomputed operator availability
- manual rebalance controls that preserve validation and audit

The same published drive-plan state must feed:

| Surface | Must show |
|---|---|
| Control Tower (CT) | Drive health, safe-window breach counts, operator capacity risk, director/park visibility. |
| Action Center (AC) | Actionable exceptions: no available operator, over-cap request, latest-safe-date breach, review holds, unresolved defer. |
| Protocol Adherence (PA) | Rule adherence state: due, scheduled in safe window, breached safe window, medically deferred, completed, skipped with reason. |

AC, PA, and CT must not recalculate their own capacity from dose rows or
vaccination cells. They read the same operator-date assignment/read model so
counts match across the product.

Manual edits must be validated before publish:

- no operator above cap unless explicitly approved by an authorized override
- no animal assigned twice
- no deferred/review-hold animal scheduled without resolution
- no vaccine bundle incompatible with the date
- no operator assigned outside role/scope/availability without override

## 13. Audit and MOP Requirements

Every published drive plan must record:

- plan version
- generated by / generated at
- published by / published at
- source eligibility snapshot
- operator availability snapshot
- leave/override inputs
- cap configuration
- shed normalization version
- manual edits and reasons

The field MOP must require:

- scan animal identity
- confirm animal belongs to assigned shed/partition or log exception
- administer assigned bundle only
- capture proof according to SOP
- record operator, timestamp, vaccine lot, and completion evidence
- mark skipped animal with reason, never silently ignore

## 14. Acceptance Criteria

- Given 324 source animals, 3 available operators, and cap 200, planner
  assigns all 3 operators meaningful work without splitting partitions
  unnecessarily.
- Given an operator is weekly-off on the spillover date, planner excludes them
  and recomputes capacity for that date.
- Given total eligible animals exceed daily capacity, planner spills remaining
  animals to the next available date.
- Given a physical shed fits the full operator cap but not the remaining slots
  on the current day, planner carries the entire shed to the next date without
  assigning any of its partitions early.
- Given one shed exceeds one operator cap, planner splits by partition first.
- Given one partition exceeds one operator cap, planner splits within that
  partition and records a forced-split warning.
- Given an animal is sick, ICU, in quarantine, under treatment, or blocked by a
  reproductive/warm-up rule, planner does not schedule it and shows the reason.
- Given an animal recovers, exits quarantine, or clears warm-up, planner can
  re-enter it into the next valid drive window.
- Given a row lacks species or current shed, planner puts it in review hold.
- Given a manual planner edit creates over-cap or duplicate assignment, publish
  fails with a clear validation message.

## 15. Open Questions

- What is the default `minimum_meaningful_assignment_animals` below which the
  planner may use fewer available operators on the last spillover day?
- Should an operator's cap vary by shed distance, vaccine bundle complexity, or
  species handling difficulty, or remain a single daily animal cap?
- Which source is canonical for date-specific leave: HRMS, timetable override,
  manual drive planner override, or all three with precedence?
- Should field execution allow a same-day operator swap after partial proof
  capture, and what approval is required?
- Should the Preventive Care Director be able to publish an emergency over-cap
  plan, or only CEO/CXO/Park Head?
