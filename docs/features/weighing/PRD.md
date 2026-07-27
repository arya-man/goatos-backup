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

Weighing v1 adds a kids-only, shed/partition execution workflow on Android.
Leadership selects the kid sheds/partitions to cover. The app turns that
selection into rolling operator work cards spread across about three work days,
keeps the work open until finished, and records each animal's RFID, weight,
original expected shed, actual scanned shed context, and mandatory per-animal
proof video for `individual_animal` rows. For `per_shed_partition` rows, it
records the selected shed/partition weighing result and required shed/partition
proof video without creating individual animal weights.

## 1.1 Vaccination lessons this feature must absorb

The last Vaccination hardening cycle exposed the kinds of bugs Weighing must
avoid from the first build:

| Vaccination hurdle | Weighing requirement |
|---|---|
| Shed submit state leaked across sibling sheds when the parent task was shared. | Every weighing row, proof, progress count, and submit action must keep exact campaign + work group + expected shed/partition + animal grain. |
| Proof refs could be lost or not recoverable after mobile submit/retry. | A weighed animal is not complete until the weight and its per-animal proof ref are durably linked and recoverable on retry. |
| Idempotency keys were too broad for shed/partition submit. | Weighing idempotency must include campaign, work group, animal, operator, and proof intent. |
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
| Preventive Director | Review and monitor weighing tasks, similar to Chandrakant-style director supervision. Dinakar belongs here for v1, but he does not create tasks. |
| Operator | Execute assigned weighing work on Android. Amit is the v1 operator. |
| System | Builds shed/partition work groups, rolls unfinished work forward, records scans/proofs, updates progress/read models, and raises delayed-work visibility. |

V1 must not treat Dinakar as a field operator or add his capacity to the daily
execution calculation. Dinakar is supervisor/reviewer scope only.

## 3. Core product model

Weighing v1 is a **kids-only weekly work container for the operator UI**. Adults
are not part of this slice. The source/v1 cadence is:

| Animal group | Source weighing cadence | V1 behavior |
|---|---|---|
| Kids / K and F kid groups | Every Monday | Only active v1 campaign. All kids are covered across about three operator work days, and the task rolls until finished. |
| Adult goats | Source says monthly on the 15th, but Aryaman clarified "adults not doing" for this build. | Out of scope for v1. Do not expose an adult monthly lane, do not auto-schedule adults, and do not allow adult sheds as manual exceptions until product reopens the scope. |

CEO/CXO may create the week's active task on any day inside the week. From that
day, the selected kid sheds become active weighing work. The system should
suggest three practical daily work groups using a capacity target, but the task
stays open and keeps rolling until the selected shed/partition work is finished.

Example:

| Field | Example |
|---|---|
| Week | 2026-07-26 to 2026-08-01 |
| Scheduled/created date | 2026-07-29 |
| Selected sheds | Kid sheds S1, S2, S3, S4, S5 |
| Total expected animals | 324 |
| Planning cap | 100 animals/day |
| Expected plan | About three work days from 2026-07-29 onward, rolling if needed. |
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

- CEO/CXO sees planner and monitoring entry points.
- Preventive director/Dinakar sees monitoring and review entry points only.
- Amit/operator sees execution-only entry points for assigned open work.
- Dinakar must never appear as task creator, execution assignee,
  execution-capacity contributor, or scan/submit actor.
- Users without `weighing.plan`, `weighing.monitor`, or `weighing.execute` see
  no Weighing sidebar entry and cannot deep-link into Weighing routes.

CEO/CXO planning flow:

1. Open Weighing.
2. Select a week tab, matching the Vaccination weekly mental model.
3. Create or edit that week's weighing task.
4. Confirm the v1 lane: weekly kids/K/F work only.
5. Select one farm/park scope as needed.
6. Select multiple kid sheds/partitions.
7. Review any shed/partition containing adult or mixed membership as excluded or
   requiring source-data review; adults are not scheduled in v1.
8. Review total expected animals and suggested daily groups.
9. Confirm assignment to the operator.

The UI must say this is a weekly weighing task that starts from the selected
business date and rolls forward until done for kids. It should not imply the
work must finish inside the calendar week, and it must not show or silently
schedule adult monthly work in v1.

Week tabs must preserve the planning bucket. A campaign created inside a week
remains visible from that week tab even after open work rolls beyond week end,
while the operator queue shows the same open work on its current rolled-forward
business date.

If the same week already has an active weighing campaign for the same farm/park
and overlapping selected sheds, the UI must show the existing campaign and ask
for edit/extend/cancel rather than creating a duplicate silent task.

Each week tab must show one backend-owned campaign state: `no_task`, `draft`,
`planned`, `published`, `in_progress`, `delayed`, `completed`, or `canceled`.
CEO/CXO opening a week sees create/edit controls. CEO/CXO and preventive
director/Dinakar both see the active campaign card first, including start date,
kids-only lane, selected sheds/partitions, expected count, operator, suggested
finish, actual progress, and whether the campaign has rolled beyond the selected
week.

## 4.1 Measurement category per shed/partition

Leadership must choose the weighing category for each selected kid
shed/partition. Sheds/partitions are still the grouping and assignment unit for
Amit's work, but the evidence/completion rule depends on the selected category:

| Category | Product behavior |
|---|---|
| Individual animal weighing | Operator scans each animal RFID or supported animal identifier, records that animal's weight, and attaches mandatory per-animal proof video. |
| Per-shed/partition weighing (field label: lumpsum) | Operator records the shed/partition weighing result for the selected scope and attaches the required shed/partition proof video. This does not create individual animal weights. |

For individual animal weighing, the selected shed/partition tells the operator
where to work and which expected animals belong in that assignment. It never
substitutes for animal-wise RFID, weight, and video.

For per-shed/partition weighing, the selected shed/partition itself is the
measurement scope. The app must label it separately from individual animal
weighing so reports never pretend each RFID animal got a fresh individual
weight.

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

Android must treat Room as the local source of truth for this flow. The app may
refresh from network in the background, but the visible work card, scan roster,
captured observations, video upload state, mismatch rows, and submit/retry state
must render from principal-scoped Room rows. Process death, app restart, network
loss, and sign-in refresh must not drop a captured weight/video pair or route
the operator into the wrong campaign/work group.

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

RFID scan state must be explicit:

- `pending_local`: RFID resolved locally and weight/video capture is in progress.
- `proof_uploading`: video exists locally and upload is running/retryable.
- `ready_to_submit`: weight is valid and required video proof is durably linked.
- `sync_failed`: backend or upload rejected; row stays editable/retryable where
  policy allows.
- `accepted`: backend accepted the observation; only audited correction can
  change it.
- `conflict/review_needed`: duplicate animal, stale campaign, unknown RFID, or
  backend semantic mismatch requires operator/supervisor recovery.

The operator list must stay usable when a campaign covers thousands of animals.
The phone shows a paged, shed-grouped worklist with full-task summary counts from
the backend. It must not download every expected animal in the campaign just to
render the first screen or compute a progress badge.

## 7. Wrong-shed animal behavior

In Vaccination, an animal scanned from a different shed is highlighted and may
be separated into another table/work item. For Weighing, an animal from another
shed is an operational mismatch, not a failed clinical rule. Keep the scan in
the same execution table but make the mismatch obvious.

V1 behavior:

- Accept the scan if the RFID resolves to a current active animal.
- Show the row in the same table.
- Add columns such as `Expected/original shed` and `Actual/current shed`.
- Highlight mismatches.
- Include mismatch counts in supervisor progress.
- Keep the expected animal's original/planned shed visible even if current
  animal location changed after planning.
- Do not silently change the animal's canonical shed because it appeared during
  weighing. Movement remains a separate authorized workflow.
- If the scanned animal was not part of the selected sheds at planning time,
  record it as `not in campaign` rather than pretending it satisfied another
  expected animal.
- If an expected animal shifted to another selected or unselected shed and is
  weighed there, count that animal as weighed for the campaign only once, while
  still showing the mismatch/actual shed insight.

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

Missing animal UX must use three buckets:

| Bucket | Meaning |
|---|---|
| Pending | Still active in expected shed and not yet weighed. |
| Unavailable | Canonical state says ICU, quarantine, dead, culled, sold, transferred, or exited. |
| Review needed | Current state is unknown, contradictory, or movement/lifecycle projection is stale. |

Only `Pending` counts as remaining operator workload. `Unavailable` and
`Review needed` stay visible to leadership with reason and source timestamp.

## 9. Evidence model

Vaccination currently uses SOP-driven proof. For the active shed/partition
vaccination SOP, one shed video is mandatory and other videos are optional.
Weighing proof depends on the selected shed/partition category.

Each completed individual animal weighing row must have a proof artifact
attached to that animal and weighing session. A shed/partition recap video may be
added later, but it must not replace per-animal proof for individual rows.

Per-animal video is mandatory for individual weighing even when the operator
scans many animals in the same physical shed without moving. A bulk/shed recap
cannot satisfy missing animal videos, and submit must show exactly which
accepted/locally completed animals are blocked by missing, uploading,
failed, or detached proof.

For per-shed/partition weighing, the required proof is a shed/partition proof
video tied to that selected scope and weighing session. It must not be treated as
per-animal proof and must not update animal-level latest trusted weight.

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
- Which per-shed/partition selected scopes have completed proof-backed weighing
  results without animal latest-weight updates.
- Which expected animals are still pending versus unavailable because they
  shifted, entered ICU/quarantine, died, were culled, or exited.

Delay is an operational signal, not a task failure. If a task spills past the
week, show it as delayed/open and keep it executable.

Progress buckets must be category-aware, disjoint, and explainable.

For `individual_animal` selected sheds/partitions:

```text
individual_expected_at_planning
= individual_weighed_expected
+ individual_pending_expected
+ individual_unavailable_expected
+ individual_closed_by_leadership
```

For `per_shed_partition` selected sheds/partitions:

```text
per_shed_partition_selected_scopes
= per_shed_partition_completed_scopes
+ per_shed_partition_pending_scopes
+ per_shed_partition_blocked_or_failed_proof_scopes
+ per_shed_partition_closed_by_leadership
```

Per-shed/partition progress is based on accepted selected-scope observations,
not expected-animal rows. These rows are excluded from animal latest-weight truth
and must not mark every expected animal as individually weighed.

Wrong-shed and not-in-campaign scans are shown as separate insight counts. They
must not inflate the expected completion numerator unless the animal was one of
the campaign's expected animals.

Leadership and operator recovery screens must make incomplete work actionable:

- proof missing/uploading/failed by animal;
- sync pending/failed by animal;
- unresolved RFID or duplicate scan conflicts;
- expected animals unavailable because of movement, ICU/quarantine, death,
  culling, sale/transfer, or unknown state;
- delayed work groups rolled beyond the suggested date/week; and
- manual close/cancel decisions with audit reason when leadership chooses not to
  chase remaining animals.

Leadership progress card contract:

- Header: campaign date range, cadence lane, status, operator, and start date.
- Primary counts: individual expected at planning, individual weighed expected,
  individual pending expected, individual unavailable expected,
  per-shed/partition selected scopes completed/pending/proof-blocked, and closed
  by leadership.
- Insight counts: other-shed weighed, not-in-campaign weighed, proof
  missing/failed, and delayed days.
- Shed rows: selected shed/partition, category, planned count, individual
  weighed/pending/unavailable counts where applicable, per-shed/partition
  observation/proof status where applicable, latest activity, and status.
- Actions: edit pending sheds, close with reason, cancel, view animal rows, and
  view proof issues.

All labels, tones, disabled reasons, and drilldown routes come from the backend
contract.

Leadership assistant/reporting must answer:

- Which kids-only weighing campaigns are active this week?
- Which sheds are delayed and by how many days?
- How many individual expected animals were weighed, pending, unavailable, or
  closed?
- Which per-shed/partition selected scopes are completed, pending,
  proof-blocked, or closed?
- Which animals from other sheds were weighed during this campaign?
- Which expected animals are missing because of ICU, death, cull,
  sale/transfer, or movement?
- Which weighed animals are missing mandatory proof or have failed upload?

Assistant answers must come from Weighing read models, not raw observation rows
or frontend-calculated totals.

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
- Adult goat weighing, monthly adult scheduling, and adult manual exceptions.
- Requiring exact 100 animals/day completion.
- Letting weighing directly mutate animal lifecycle/location state.
- Reusing Vaccination's shed/partition proof policy as a substitute for per-animal
  video.
- Reusing generic SOP task state as the only source of Weighing progress.

## 12. Acceptance criteria

- CEO/CXO can create a weekly kids-only weighing task by selecting kid
  sheds/partitions.
- Preventive director/Dinakar can review and monitor weighing tasks but cannot
  create, publish, or edit the task plan in v1.
- Adult monthly weighing is not available in v1: adult sheds are excluded from
  the weekly lane and cannot be added as manual exceptions.
- Leadership can mark each selected kid shed/partition as individual animal
  weighing or per-shed/partition weighing.
- Amit receives the operator execution work; Dinakar does not receive operator
  capacity.
- Suggested work groups preserve shed/partition atomicity and use the daily cap
  only as planning guidance.
- Unfinished groups roll forward until complete, including beyond the calendar
  week.
- For individual animal sheds/partitions, the operator can scan RFID, enter
  weight, and attach mandatory per-animal video for each completed animal row.
- For per-shed/partition sheds/partitions, the operator can record the selected
  scope's weighing result with the required shed/partition proof video, without
  updating animal-level latest trusted weights.
- Wrong-shed scans remain in the table with expected/original shed context and
  visible highlighting.
- Missing expected animals are classified separately from ordinary pending
  animals when current herd truth says they shifted, entered ICU/quarantine,
  died, were culled, sold/transferred, or otherwise exited.
- Leadership can see expected, completed, pending, mismatch, and delayed state.
- Progress counts remain correct when one work group contains multiple sheds,
  when one shed rolls to the next day, and when an extra animal from another
  shed is scanned.
- Leadership close/adjust flow is explicit: closing pending expected animals
  requires a reason code, actor, timestamp, affected count, and animal list
  snapshot. Closed animals are excluded from operator workload but remain visible
  in campaign audit and reporting.
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
  preventive-director reviewer/supervisor.
- Per-animal media is mandatory for completed `individual_animal` rows.
  Shed/partition proof is mandatory for completed `per_shed_partition` rows.
  Generic SOP blobs or mobile-only transient references are insufficient for
  either category.
- Weighing lists and summaries stay correct past page one and at the 5k-to-50k
  animal envelope.
- Assignment/reminder/delay notifications are durable and retryable.
- Per-animal proof linkage is recoverable after app restart, upload retry, or
  idempotent submit replay.
