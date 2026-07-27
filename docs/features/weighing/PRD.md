# Weighing — Product Requirements (PRD)

**Status:** Draft v1 · **Date:** 2026-07-27  
**Companion:** [TRD.md](./TRD.md)  
**Source hierarchy:** maintainer clarification in the 2026-07-27 working session; existing Vaccination execution behavior for rolling task/window UX; `context/source-findings/goats-and-parks-source-findings.md`, `context/source-findings/goats-and-parks-source-extract.md`, and `context/source-findings/sheds-db-source-findings.md` for herd, RFID, shed, and weight source semantics.  
**Explicitly not a clone:** Preventive Care Vaccination has clinical due windows, vaccine rules, and strict obligation completion. Weighing reuses the shed/partition work-session pattern, but it has different evidence and completion rules.

---

## 1. Why this exists

Weight is a core farm signal. It tells leadership whether fattening animals are
gaining properly, whether breeding adults are holding condition, and whether a
specific shed/cohort needs feed, health, or movement attention. The legacy
source material already treats weighing as a non-negotiable per-animal data
point, but the current operational workflow is manual: leadership tells the
field team which sheds to cover, and the operator records weights outside a
first-class Goat OS task.

Weighing adds a cadence-driven, shed-level execution workflow on Android.
Leadership selects the sheds/partitions to cover. The app turns that selection
into rolling operator work cards, keeps the work open until finished, and
records each animal's RFID, weight, original expected shed, actual scanned shed
context, and mandatory per-animal proof video.

## 1.1 Vaccination lessons this feature must absorb

The last Vaccination hardening cycle exposed the kinds of bugs Weighing must
avoid from the first build:

| Vaccination hurdle | Weighing requirement |
|---|---|
| Shed submit state leaked across sibling sheds when the parent task was shared. | Every weighing row, proof, progress count, and submit action must keep exact campaign + work group + expected shed/partition + animal grain. |
| Proof refs could be lost or not recoverable after mobile submit/retry. | A weighed animal is not complete until the weight and its per-animal proof ref are durably linked and recoverable on retry. |
| Idempotency keys were too broad for shed-level submit. | Weighing idempotency must include campaign, work group, animal, operator, and proof intent. |
| Android scan screens lost shed/partition identity in titles/routes. | Weighing screens must always display the active work group and expected shed/partition context. |
| Permission checks allowed scan/submit paths before the correct proof/capability gate. | Weighing submit must be blocked until the user has execution capability and every completed animal has mandatory proof. |
| Admin/leadership cards drifted from backend contract fields. | Leadership progress, mismatch, unavailable, and delayed labels must be backend-owned contract fields, not hardcoded UI guesses. |
| Retry/fanout failures made proof/progress appear stuck until a later repair. | Weighing must have durable retry, recovery, and supervisor-visible degraded states for media upload, observation acceptance, projection, and notification fanout. |
| Android offline and route identity bugs let the app reopen the wrong shed/task after process death or scan. | Weighing routes, Room rows, and outbox commands must carry campaign, group, shed/partition, animal, and proof identity end to end. |

These lessons are acceptance constraints, not implementation polish. A Weighing
screen that "looks right" but derives totals from the current phone page,
reconstructs shed membership from current location, loses proof linkage on
retry, or lets a shared campaign row stand in for animal-level completion is not
accepted.

## 2. Actors and authority

| Actor | Product authority |
|---|---|
| CEO/CXO | Create and monitor weekly weighing tasks across farms/sheds. |
| Preventive Director | Create and monitor weighing tasks, similar to Chandrakant-style director supervision. Dinakar belongs here for v1. |
| Operator | Execute assigned weighing work on Android. Amit is the v1 operator. |
| System | Builds shed/partition work groups, rolls unfinished work forward, records scans/proofs, updates progress/read models, and raises delayed-work visibility. |

V1 must not treat Dinakar as a field operator or add his capacity to the daily
execution calculation. Dinakar is supervisor/planner scope.

## 3. Core product model

Weighing v1 is a **weekly work container for the operator UI**, not a promise
that every animal type is weighed weekly. The source cadence is:

| Animal group | Source weighing cadence | V1 behavior |
|---|---|---|
| Kids / K and F kid groups | Every Monday | Primary v1 weekly campaign. A leader may schedule inside that week, and the task rolls until finished. |
| Adult goats | 15th of every month | Excluded from automatic weekly creation unless leadership explicitly selects adult sheds as a manual campaign. Monthly adult scheduling must be represented as its own due campaign/date, not generated every week. |

A leader may create the week's active task on any day inside the week. From that
day, the selected sheds become active weighing work. The system should suggest
daily work groups using a capacity target, but the task stays open and keeps
rolling until the selected shed/partition work is finished.

Example:

| Field | Example |
|---|---|
| Week | 2026-07-26 to 2026-08-01 |
| Scheduled/created date | 2026-07-29 |
| Selected sheds | S1, S2, S3, S4, S5 |
| Total expected animals | 324 |
| Planning cap | 100 animals/day |
| Expected plan | 2026-07-29, 2026-07-30, 2026-07-31, 2026-08-01 |
| Allowed reality | Work may finish later, for example 2026-08-02 or 2026-08-03. |

The cap is a planning guide. It must not force operators to hit exactly 100
animals per day. If the suggested day group is `S1=80 + S2=20`, and Amit weighs
only `S1=80` on that day, the app must allow completion for that day's actual
work and roll `S2=20` forward.

Weighing work has three separate clocks:

| Clock | Meaning |
|---|---|
| Campaign week | The leadership planning bucket shown in week tabs. |
| Start business date | The date from which operator work becomes visible. |
| Execution/roll-forward date | The date on which unfinished work is currently shown to the operator. |

The UI must never collapse these into one "due date." A task created on
2026-07-29 for the 2026-07-26 to 2026-08-01 week remains the same weekly
campaign even if unfinished work rolls to 2026-08-02 or later.

## 4. Scheduling UX

Leadership sees a Weighing sidebar entry in Android. The first audience is CEO,
director/preventive director, and the operator.

Sidebar visibility is backend-contract and capability driven:

- CEO/CXO and preventive director see planner and monitoring entry points.
- Amit/operator sees execution-only entry points for assigned open work.
- Dinakar sees planner/monitoring surfaces only; he must never appear as an
  execution assignee, execution-capacity contributor, or scan/submit actor.
- Users without `weighing.plan`, `weighing.monitor`, or `weighing.execute` see
  no Weighing sidebar entry and cannot deep-link into Weighing routes.

Leadership flow:

1. Open Weighing.
2. Select a week tab, matching the Vaccination weekly mental model.
3. Create or edit that week's weighing task.
4. Confirm the cadence lane: weekly kids/K/F work, monthly adult work, or a
   manual exception campaign.
5. Select one farm/park scope as needed.
6. Select multiple sheds/partitions.
7. Review whether selected sheds contain adult monthly animals, weekly animals,
   or mixed membership.
8. Review total expected animals and suggested daily groups.
9. Confirm assignment to the operator.

The UI must say this is a weekly weighing task that starts from the selected
business date and rolls forward until done for the selected cadence lane. It
should not imply the work must finish inside the calendar week, and it must not
silently schedule adult monthly sheds every week.

If the same week already has an active weighing campaign for the same farm/park
and overlapping selected sheds, the UI must show the existing campaign and ask
for edit/extend/cancel rather than creating a duplicate silent task.

## 5. Grouping rules

Weighing uses vaccination-style **shed/partition work grouping**, but not
vaccination's clinical planner.

Rules:

- Preserve shed/partition atomicity. Do not split a shed or partition just to
  perfectly hit the daily cap.
- Club smaller sheds/partitions together when the combined count fits reasonably
  under the daily cap.
- If one shed/partition itself exceeds the daily cap, keep it as one work group
  and allow the group to span multiple days.
- If the operator finishes less than the suggested cap, roll the remaining
  shed/partition work forward.
- If the task extends one or two days beyond the selected week, keep it open and
  visibly delayed instead of blocking execution.
- Missing expected animals must be explained against current herd truth before
  they are treated as operator misses. Animals can legitimately leave the
  selected shed after planning because of shifting, ICU, quarantine, death,
  culling, sale/transfer, or other lifecycle workflows.

Example:

| Shed | Expected animals |
|---|---:|
| S1 | 80 |
| S2 | 30 |
| S3 | 50 |
| S4 | 70 |
| S5 | 20 |

Valid suggested groups:

| Suggested day | Group | Count |
|---|---|---:|
| Day 1 | S1 + S5 | 100 |
| Day 2 | S2 + S3 | 80 |
| Day 3 | S4 | 70 |

Another valid execution reality:

| Business date | Actual completed | Remaining behavior |
|---|---|---|
| Day 1 | S1 only, 80 animals | S5 rolls forward. |
| Day 2 | S5 + S2, 50 animals | Allowed even below cap. |

Capacity is not a promise that every business date contains exactly 100 animals.
It is a planning target used to create stable chunks. Leadership should see
`planned_count`, `completed_count`, `remaining_count`, and `rolled_forward_count`
as backend-supplied values so a below-cap day reads as normal field reality, not
as UI arithmetic failure.

## 6. Operator execution

The operator opens the assigned weighing card and sees the current rolling work
group. The screen should be optimized for repeated RFID scan, weight entry, and
proof capture.

For each animal, the operator must capture:

- RFID/Animal ID scan.
- Weight value.
- Mandatory per-animal video proof.
- Actual scanned context from the current work session.
- Expected/original shed from canonical herd location at task planning time.

Completion is less strict than vaccination. The operator can submit the animals
actually scanned and weighed, while the task continues to show pending expected
animals until all selected sheds/partitions are done or leadership explicitly
closes/adjusts the task.

Weighing completion is also two-layered:

| Layer | Completion meaning |
|---|---|
| Animal row | A resolved RFID/animal, valid weight, and mandatory per-animal proof video were accepted or explicitly corrected/voided. |
| Campaign/shed | Every expected animal is either weighed, unavailable under current herd truth, or explicitly closed by leadership with an audited reason. |

Submitting a partial day is allowed. Marking the selected shed/partition itself
complete is not allowed while active expected animals remain merely pending.

The operator must be able to correct a mistaken local capture before final sync:
remove/replace a just-captured animal video, edit the weight value, or discard a
wrong scan. After sync, corrections require an auditable correction/replacement
flow rather than silent overwrite.

The operator list must stay usable when a campaign covers thousands of animals.
The phone shows a paged, shed-grouped worklist with full-task summary counts from
the backend. It must not download every expected animal in the campaign just to
render the first screen or compute a progress badge.

## 7. Wrong-shed animal behavior

In Vaccination, an animal scanned from a different shed is highlighted as a
problem and may be separated into another table/work item. For Weighing, keep
the scan in the same execution table but make the mismatch obvious.

V1 behavior:

- Accept the scan if the RFID resolves to a current active animal.
- Show the row in the same table.
- Add columns such as `Expected/original shed` and `Actual/current shed`.
- Highlight mismatches.
- Include mismatch counts in supervisor progress.
- Do not silently change the animal's canonical shed because it appeared during
  weighing. Movement remains a separate authorized workflow.
- If the scanned animal was not part of the selected sheds at planning time,
  record it as `not in campaign` rather than pretending it satisfied another
  expected animal.

## 8. Missing expected animals and lifecycle exceptions

An expected animal can be missing from the weighing session for two very
different reasons:

1. The operator did not weigh it yet.
2. The animal is no longer practically available in that shed because another
   canonical workflow changed its status or location.

Weighing must not flatten these into one "missed" bucket. At execution and
supervisor review time, the app should classify pending expected animals using
current canonical herd state:

| Current truth after planning | Weighing display behavior |
|---|---|
| Shifted to another normal shed/partition | Show as moved/other shed; allow weighing if the operator scans it, but keep original expected shed visible. |
| Shifted to ICU or quarantine | Show as unavailable due to ICU/quarantine; do not count as ordinary operator miss. |
| Dead or culled | Show as lifecycle exit; remove from remaining operator workload after audit/projection catches up. |
| Sold/transferred/exited | Show as exited; remove from remaining operator workload after audit/projection catches up. |
| Still active in expected shed | Keep as pending/missed until weighed or leadership closes it. |
| Unknown or unresolved identity/location | Show as review-needed, not silently completed. |

This classification is read/display and worklist behavior. Weighing must not
itself perform movement, death, cull, sale, or health-state changes. Those stay
owned by their canonical workflows.

## 9. Evidence model

Vaccination currently uses SOP-driven proof. For the active shed-level
vaccination SOP, one shed video is mandatory and other videos are optional.
Weighing must instead require **per-animal proof video**.

Each completed weighing row must have a proof artifact attached to that animal
and weighing session. A shed-level recap video may be added later, but it must
not replace per-animal proof.

The proof experience must be designed for poor connectivity. Operators should be
able to capture weight + video offline, see that the row is pending upload/sync,
retry safely, and remove or replace an unsynced video before final submit. After
backend acceptance, replacement must be an audited correction rather than silent
overwrite.

## 10. Progress, delay, and leadership visibility

Leadership needs to know:

- Which weekly weighing tasks exist.
- Which sheds/partitions were selected.
- Expected animals, weighed animals, pending animals, and mismatch animals.
- Which operator owns execution.
- Which day the task started.
- Whether the task has rolled beyond the week or expected finish date.
- Which animals have fresh trusted weight observations.
- Which expected animals are still pending versus unavailable because they
  shifted, entered ICU/quarantine, died, were culled, or exited.

Delay is an operational signal, not a task failure. If a task spills past the
week, show it as delayed/open and keep it executable.

Progress buckets must be disjoint and explainable:

```text
expected_at_planning
= weighed_expected
+ pending_expected
+ unavailable_expected
+ closed_by_leadership
```

Wrong-shed and not-in-campaign scans are shown as separate insight counts. They
must not inflate the expected completion numerator unless the animal was one of
the campaign's expected animals.

All progress summaries must be whole-campaign or whole-filter summaries. They
must remain correct if the UI page size changes from 20 to 10, if a selected
shed rolls forward, or if an extra animal from a different shed is weighed during
the same session.

## 10.1 Scale and reliability expectations

V1 must be designed for the current Goat OS 5k-to-50k animal envelope. Product
acceptance includes these visible outcomes:

- Weighing overview, operator worklist, and animal rows open quickly from
  backend-owned summaries and paged detail rows.
- Leadership can inspect a campaign with many sheds without waiting for a
  tenant-wide animal scan.
- Operator scan feedback stays fast because RFID matching uses indexed local
  state, not a linear search through the entire campaign.
- Missing animals are explained from current canonical state without rebuilding
  every open campaign on every read.
- Assignment, reminder, delay, and proof-recovery notifications are durable. A
  failed push can retry or land in a visible repair path; it is not just a log.
- Support/debug views can answer: which campaign, work group, shed/partition,
  animal, proof, idempotency key, and notification request produced a row.

## 11. Out of scope for v1

- Automatic weight-anomaly clinical diagnosis.
- Feed-ration recalculation from weighing results.
- Auto movement/shifting based on weight.
- Multiple operator capacity splitting beyond the current Amit-only execution.
- Live weighing-machine Bluetooth integration.
- Bulk historical Weights DB import, except as source evidence for data model
  alignment.
- Requiring exact 100 animals/day completion.
- Letting weighing directly mutate animal lifecycle/location state.
- Reusing Vaccination's shed-level proof policy as a substitute for per-animal
  video.
- Reusing generic SOP task state as the only source of Weighing progress.

## 12. Acceptance criteria

- CEO/CXO and preventive director can create a weekly weighing task by selecting
  sheds/partitions.
- Adult monthly weighing is not auto-scheduled every week. Adult sheds are either
  excluded from the weekly lane, explicitly selected as a manual exception, or
  scheduled through the monthly adult lane.
- Amit receives the operator execution work; Dinakar does not receive operator
  capacity.
- Suggested work groups preserve shed/partition atomicity and use the daily cap
  only as planning guidance.
- Unfinished groups roll forward until complete, including beyond the calendar
  week.
- Operator can scan RFID, enter weight, and attach mandatory per-animal video.
- Wrong-shed scans remain in the table with expected/original shed context and
  visible highlighting.
- Missing expected animals are classified separately from ordinary pending
  animals when current herd truth says they shifted, entered ICU/quarantine,
  died, were culled, sold/transferred, or otherwise exited.
- Leadership can see expected, completed, pending, mismatch, and delayed state.
- Progress counts remain correct when one work group contains multiple sheds,
  when one shed rolls to the next day, and when an extra animal from another
  shed is scanned.
- No Weighing implementation reuses Vaccination's clinical due-window or dose
  obligation logic as the source of grouping truth.

## 13. Product non-negotiables before build

- Backend contracts own all status labels, counts, disabled reasons, mismatch
  reasons, media availability states, and tap/deep-link targets.
- Leadership and operator screens must distinguish pending, delayed,
  unavailable, mismatch, not-in-campaign, sync-pending, sync-failed, and
  correction-needed states.
- A row scanned from another shed can be accepted as a weighing observation, but
  it must not silently complete a different expected animal or mutate canonical
  location.
- Animals missing because of shifting, ICU/quarantine, death, culling, sale, or
  transfer must be explained from current canonical herd truth before being
  blamed on the operator.
- The v1 operator capacity calculation counts Amit only. Dinakar is a
  preventive-director planner/supervisor.
- Per-animal media is mandatory for completed weighing rows. Shed-level proof,
  generic SOP blobs, or mobile-only transient references are insufficient.
- Weighing lists and summaries stay correct past page one and at the 5k-to-50k
  animal envelope.
- Assignment/reminder/delay notifications are durable and retryable.
- Per-animal proof linkage is recoverable after app restart, upload retry, or
  idempotent submit replay.
