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

### 4.2 Weighing V1 empty bucket model

Weighing V1 does not use an expected animal roster. A selected shed/partition is
only an empty bucket for this weighing campaign. The operator chooses the bucket,
then puts whatever RFID/tag, weight, and video proof they capture into that
bucket. This is intentionally separate from Vaccination and Herd Register
truth.

Non-negotiables for this V1 slice:

- There is no expected animal count per shed for submit.
- There is no expected animal list per shed for submit.
- There is no wrong-shed rule.
- There is no "this RFID belongs only to this shed" rule.
- There is no rule preventing the same RFID/tag from being captured in multiple
  shed buckets.
- Weighing records stay under Weighing tables/storage and must not mutate goat
  identity, goat location, Vaccination assignment, or Herd Register truth.
- Submit means "close the scanned evidence captured in this bucket", not
  "complete every goat that might exist in this shed."

Acceptance matrix:

| Flow | Required before submit | Allowed before submit | Not allowed as a gate |
|---|---|---|---|
| Individual shed bucket | Every visible captured RFID/tag row in that bucket has one saved weight and one synced video proof. | Random RFID/tag values, duplicate RFID/tag values across different shed buckets, navigating away/back, editing weight before submit, replacing/reuploading video before submit. | Expected animal count, expected animal list, Herd Register UUID resolution, wrong-shed validation, duplicate-across-shed rejection, backend accepted-state restore lag. |
| Lump-sum shed bucket | Total weight, total animal number, and at least one synced shed-level video proof. | Additional shed-level proof videos up to the configured proof policy limit; V1 UI/verification should handle 1-5 videos. | Expected animal count, individual RFID rows, Herd Register roster completion, wrong-shed validation. |

Backend/list restore requirement: when the app returns to a shed bucket, the
backend and Room must surface every captured Weighing entry for that bucket,
including duplicate RFID/tag values that also appear in other buckets. Each
individual entry remains valid only as a pair of saved weight + synced video.

Offline/Room/outbox requirement: the local Android database, proof upload queue,
weighing observation queue, backend submit endpoint, and backend restore
endpoint must all use the same bucket-local grain:

- `campaign_id + work_group_id + campaign_shed_id + scanned_identifier` for
  individual evidence rows.
- `campaign_id + work_group_id + campaign_shed_id` for lump-sum shed evidence.
- Duplicate scanned identifiers across different shed buckets must never be
  collapsed, rejected, or overwritten by Room, offline replay, backend restore,
  or verification reads.
- Leaving the screen, returning from L0, losing network, restarting the app, or
  syncing the proof before the weighing observation must not change submit
  readiness once a captured row has saved weight + synced video.
- A reviewer must not require Weighing to resolve scanned identifiers to Herd
  Register goat UUIDs before submit. If a future version adds identity
  resolution, it must be an additive reconciliation layer, not a V1 submit gate.

For `individual_animal` weighing in V1:

- The selected shed/partition is an empty Weighing bucket, not a Herd Register
  roster. The operator can put any scanned RFID/tag into that bucket for this
  weighing campaign.
- The same RFID/tag may be captured under different shed buckets in this V1
  slice. Weighing stores that as Weighing evidence only; it does not mutate goat
  identity, goat location, Vaccination assignment, or Herd Register truth.
- The scan screen is a free-scan work session: each valid RFID/animal identifier
  scan creates a weighing entry with that identifier, the entered weight, and
  mandatory animal-level proof video.
- The app should not block execution because an animal is outside a preloaded
  visible page, because the shed has no roster snapshot, or because the scanned
  tag is duplicated in another shed bucket.
- Duplicate RFID scans in the same weighing work session must be visible and
  handled idempotently; corrections/re-records must not create silent duplicate
  accepted weights.
- Operator can end/close the individual weighing session after at least one
  proof-backed weighing entry if field work is done for now. The closure records
  what was actually scanned/weighed; it must not pretend unknown/unscanned
  animals were completed.

For `per_shed_partition` / lumpsum weighing in V1:

- One shed/partition-level proof video is enough to close the selected scope;
  additional videos may be allowed by proof policy but are not required.
- The operator must enter the shed/partition weighing value and proof before
  closing that selected scope.

All Weighing V1 entries still use the mobile Room/outbox/proof pipeline. Free
scan means "do not require a preloaded roster before accepting a weighing
entry"; it does not mean network-only writes, no proof, no idempotency, or no
verification. Weight values and proof are subject to the normal verification
flow before leadership treats them as trusted.

Reviewer contract for Weighing V1:

- Do not apply Vaccination's roster-completion rule to individual Weighing.
  Weighing is tag-first/free-flow: a scanned identifier is a valid weighing
  subject even when it does not resolve to a preloaded goat UUID.
- Do not require or infer expected animal counts per shed for individual
  Weighing V1. The shed is a bucket label for captured weighing evidence, not a
  declaration that only certain goats belong there.
- Do not flag duplicate scanned identifiers across different shed buckets as a
  product bug. Cross-bucket duplicates are allowed in V1 because Weighing is
  evidence capture, not canonical shed membership.
- Mobile must not require `matchTag()` before capturing a weight/proof. If a tag
  lookup exists locally, it is only display help; it is not a submit gate and it
  must not rewrite Weighing into a Herd Register identity workflow.
- Backend individual-scope submit closes only the submitted scanned identifiers
  that have completed proof-backed observations. It must not mark every
  unscanned expected-roster animal as completed, missing, or closed.
- Accepted/synced local state is keyed by the scanned identifier for Weighing
  completion. A review that expects restored drafts to be keyed by goat UUID is
  applying the wrong domain model.
- The admin-web `/weighing` surface is intentionally hidden until product
  approval of the web planning/monitoring UX. This does not disable mobile
  weighing or the weighing API.
- The E2E fixture may contain shed/partition business names, but contract
  `location_type` values are the API enum values (`shed`, `cohort`, `pen`).

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
- Planning estimates are not submit requirements. Operator execution records the
  bucket evidence actually captured; missing Herd Register animals are not
  treated as operator misses in V1.

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
captured observations, video upload state, and submit/retry state
must render from principal-scoped Room rows. Process death, app restart, network
loss, and sign-in refresh must not drop a captured weight/video pair or route
the operator into the wrong campaign/work group.

For each scanned identifier, the operator must capture:

- RFID/tag scan.
- Weight value.
- Mandatory per-animal video proof.
- The selected Weighing shed bucket from the current work session.

Completion is intentionally looser than Vaccination. The operator submits the
RFIDs/tags actually scanned, weighed, and proofed in the selected bucket. There
is no expected-roster remainder to compute or close.

Weighing V1 completion is bucket-local:

| Layer | Completion meaning |
|---|---|
| Scanned row | A scanned RFID/tag, valid weight, and mandatory per-animal proof video were accepted or explicitly corrected/voided. |
| Shed bucket | The submitted scanned rows in that bucket were accepted. This does not imply anything about unscanned goats in the physical shed. |

Submitting one or many scanned rows is allowed. Marking the selected Weighing
bucket submitted must not update Herd Register location, Vaccination assignment,
or any expected animal status.

The operator must be able to correct a mistaken local capture before final sync:
remove/replace a just-captured animal video, edit the weight value, or discard a
wrong scan. After sync, corrections require an auditable correction/replacement
flow rather than silent overwrite.

RFID scan state must be explicit:

- `pending_local`: RFID/tag captured locally and weight/video capture is in progress.
- `proof_uploading`: video exists locally and upload is running/retryable.
- `ready_to_submit`: weight is valid and required video proof is durably linked.
- `sync_failed`: backend or upload rejected; row stays editable/retryable where
  policy allows.
- `accepted`: backend accepted the observation; only audited correction can
  change it.
- `conflict/review_needed`: stale campaign, rejected proof, or backend semantic
  mismatch requires operator/supervisor recovery. Duplicate RFID/tag across
  different shed buckets is not a conflict in V1.

The operator list must stay usable when many RFIDs are captured. The phone shows
the captured bucket feed from Room and backend-supplied campaign/shed cards. It
must not download a Herd Register roster or expected animal list just to render
the first screen or compute a progress badge.

## 7. Wrong-shed animal behavior

Wrong-shed behavior is not part of Weighing V1. In this slice the selected shed
is only a Weighing bucket, so a scanned RFID/tag cannot be rejected or flagged
because canonical herd location says it belongs somewhere else. Reviewers must
not raise missing wrong-shed validation as a PR bug for this V1 flow.

## 8. Missing expected animals and lifecycle exceptions

Missing expected animals are not part of Weighing V1. There is no expected
animal list for a shed bucket, so the app and backend must not compute missing,
unavailable, or pending herd animals during submit. Future leadership analytics
may compare captured RFIDs to herd truth, but that would be a new reviewable
feature and must not be used to block this mobile free-flow slice.

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
- For individual buckets: captured RFID/tag rows, accepted weight+video rows,
  pending upload/sync rows, and rows needing correction.
- Which operator owns execution.
- Which day the task started.
- Whether the task has rolled beyond the week or expected finish date.
- Which scanned RFID/tag values have fresh Weighing observations.
- Which per-shed/partition selected scopes have completed proof-backed weighing
  results without animal latest-weight updates.

Delay is an operational signal, not a task failure. If a task spills past the
week, show it as delayed/open and keep it executable.

Progress buckets must be category-aware, disjoint, and explainable.

For `individual_animal` selected sheds/partitions:

```text
individual_captured_rows
= individual_weight_video_accepted
+ individual_pending_weight_or_proof_sync
+ individual_failed_or_needs_correction
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

Wrong-shed and not-in-campaign counts are not V1 submit gates. A future
analytics layer may compare Weighing scans to Herd Register truth, but that must
be clearly labeled as analysis and must not rewrite or reject bucket evidence.

Leadership and operator recovery screens must make incomplete work actionable:

- proof missing/uploading/failed by captured row;
- sync pending/failed by captured row;
- duplicate RFID/tag values across buckets shown as bucket-local evidence, not
  submit conflicts;
- delayed work groups rolled beyond the suggested date/week; and
- manual close/cancel decisions with audit reason when leadership chooses not to
  chase remaining bucket work.

Leadership progress card contract:

- Header: campaign date range, cadence lane, status, operator, and start date.
- Primary counts: individual captured rows, individual accepted rows,
  individual pending-sync/proof rows, individual correction-needed rows,
  per-shed/partition selected scopes completed/pending/proof-blocked, and closed
  by leadership.
- Insight counts: duplicate-across-bucket evidence, proof missing/failed, sync
  retrying, and delayed days.
- Shed rows: selected shed/partition, category, captured-row counts where
  applicable, per-shed/partition observation/proof status where applicable,
  latest activity, and status.
- Actions: edit pending sheds, close with reason, cancel, view animal rows, and
  view proof issues.

All labels, tones, disabled reasons, and drilldown routes come from the backend
contract.

Leadership assistant/reporting must answer:

- Which kids-only weighing campaigns are active this week?
- Which sheds are delayed and by how many days?
- How many individual captured rows are accepted, pending upload/sync, needing
  correction, or closed?
- Which per-shed/partition selected scopes are completed, pending,
  proof-blocked, or closed?
- Which RFID/tag values appear in more than one Weighing bucket?
- Which captured rows are missing mandatory proof or have failed upload?

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
- Random RFID/tag scans remain in the selected Weighing bucket as Weighing
  evidence, even if Herd Register truth would place that tag elsewhere.
- The same RFID/tag can be captured in different shed buckets without Room,
  offline sync, backend restore, submit, or verification collapsing the rows.
- Leadership can see captured, accepted, pending-sync, failed/retry, and delayed
  state for Weighing buckets.
- Progress counts remain correct when one work group contains multiple sheds,
  when one shed rolls to the next day, and when the same RFID/tag appears in
  multiple shed buckets.
- Leadership close/adjust flow is explicit and category-aware: every close
  requires a reason code, actor, timestamp, affected count, and affected-grain
  snapshot. Closing `individual_animal` pending work uses captured bucket rows,
  not a Herd Register expected-animal list. Closing `per_shed_partition` pending
  work requires the selected `campaign_shed_id`/scope snapshot. Closed rows are
  excluded from operator workload but remain visible in campaign audit and
  reporting.
- No Weighing implementation reuses Vaccination's clinical due-window or dose
  obligation logic as the source of grouping truth.

## 13. Product non-negotiables before build

- Backend contracts own all status labels, counts, disabled reasons, media
  availability states, and tap/deep-link targets.
- Leadership and operator screens must distinguish pending, delayed,
  sync-pending, sync-failed, proof-failed, and correction-needed states.
- A row scanned into a Weighing bucket can be accepted as a Weighing
  observation, regardless of Herd Register location truth, but it must not
  mutate canonical goat location or Vaccination assignment.
- Missing Herd Register animals are not a V1 execution concept. Future analytics
  may compare captured bucket evidence to canonical herd truth, but that must
  not become a mobile submit gate.
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
