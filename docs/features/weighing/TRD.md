# Weighing — Technical Requirements / Design (TRD)

**Status:** Draft v1 · **Date:** 2026-07-27
**Companion:** [PRD.md](./PRD.md)
**Foundation:** shared operational kernel, proof/media engine, Android offline
capture stack, and existing Vaccination execution patterns.
**Source references:** `context/architecture/operational-kernel.md`,
`docs/decisions/vaccination-work-session-bundle.md`,
`docs/decisions/vaccination-shed-ack-not-form.md`,
`docs/decisions/mobile-data-fetch-anti-patterns.md`,
`context/source-findings/goats-and-parks-source-findings.md`,
`context/source-findings/goats-and-parks-source-extract.md`,
`context/source-findings/sheds-db-source-findings.md`.

---

## 1. Scope

This TRD defines the first Weighing implementation slice:

1. Cadence-driven weighing campaigns created by leadership from Android, with
   weekly kid/K/F work and monthly adult work represented explicitly.
2. Shed/partition selection and count snapshot.
3. Rolling daily work groups driven by a capacity target.
4. Amit-only operator execution for v1.
5. RFID scan, weight capture, and mandatory per-animal video proof.
6. Progress/read models for leadership and operator screens.

The design deliberately borrows Vaccination's work-session execution shape, but
not its clinical scheduler. Weighing is not a vaccine obligation. It is a
per-animal measurement campaign over selected sheds/partitions. The weekly UI
container must not erase the source cadence difference: kids/K/F groups weigh
weekly, adult goats weigh monthly on the 15th unless leadership creates a manual
exception.

## 1.1 Last-30-commit Vaccination hardening lens

The Weighing implementation must be reviewed against the recent Vaccination
failure patterns before code starts:

| Failure pattern fixed in Vaccination | Weighing design guard |
|---|---|
| Shared parent task hid the real shed submit grain. | No aggregate campaign/task state may be used as per-shed/per-animal completion truth. |
| Over-broad scan items wrote sibling shed completions. | Observation inserts must join through `weighing_expected_animals` and the active proof/subject grain before counting expected completion. |
| Proof refs disappeared across submit retry/recovery. | Observation, proof artifact, upload state, and idempotency replay must be recoverable from durable rows. |
| Terminal submit state accepted new side effects. | Completed/canceled work groups are immutable except explicit correction/reopen flows. |
| Android routes chose the wrong task/shed after scan. | Route identity must carry campaign, work group, expected shed/partition, and animal scan context end to end. |
| Permission/proof gates ran too late. | Backend must reject observation completion without `weighing.execute` and required proof. |
| Leadership/admin cards used fallback copy/shape. | Backend contracts own progress buckets, disabled reasons, labels, and media URLs. |
| Notification and projection fanout retries were needed after partial failure. | Observation acceptance, progress projection, media review/recovery, notification, and leadership summaries must be durable retryable consumers with visible degraded states. |
| Mobile scan/RFID fixes exposed O(n) lookup, lost identity, and permission timing hazards. | Android must use indexed Room/cache lookup, exact route/outbox identity, and backend-first capability/proof gates. |

## 2. Architecture invariants

Go backend remains a modular monolith with strict module boundaries. Domain
logic must depend on ports/interfaces, not vendor SDKs directly. Adapters wrap
auth, storage, media, devices, notifications, analytics, and external APIs.
Adapter selection belongs in bootstrap/factory code.

Web/mobile clients use REST/JSON APIs described by OpenAPI and generated
clients. Forms, events, decisions, DLQ repair payloads, and imports use JSON
Schema where payload compatibility matters. gRPC/protobuf is allowed only behind
the app API boundary for internal high-volume workloads or a future split
service.

Every mutating path must define idempotency, audit, outbox publication, RBAC
scope, observability, replay behavior, and tests. Weighing must plug into the
operational kernel chain:

```text
business event
-> canonical transaction
-> audit/outbox
-> work item
-> sweeper/reminder/deadline alert
-> notification/escalation
-> proof/verification
-> read model
```

The kernel chain is not optional because Weighing looks simpler than
Vaccination. Weighing still creates operational work, captures proof, records a
trusted animal fact, affects leadership answers, and can become delayed or
blocked by missing animals. Those are kernel concerns.

## 3. Domain grain

Canonical grains:

| Grain | Purpose |
|---|---|
| Weighing campaign | One cadence instance created by leadership for a farm/park/week-or-month/start date. |
| Selected shed/partition | Atomic planning unit. It carries expected animal membership at planning time. |
| Work group | One suggested operator chunk containing one or more whole selected sheds/partitions. |
| Animal weighing observation | One animal's RFID, measured weight, proof video, and expected/actual shed context. |
| Proof artifact | Mandatory per-animal video linked to the observation. |
| Availability exception | Current herd-state explanation for why an expected animal is not weighable from the planned shed. |
| Correction | Audited replacement/voiding of an incorrect weight or proof after sync. |

Shed/partition is atomic for planning. A work group may contain multiple
sheds/partitions. A shed/partition must not be split only to satisfy the daily
cap. If one shed/partition exceeds the daily cap, the group remains the whole
shed/partition and may span multiple business dates through execution progress.

Every query, projection, outbox event, notification, Android route, and proof
lookup must preserve this key set:

```text
tenant_id
campaign_id
campaign_shed_id / expected_location_id
work_group_id where execution context exists
animal_id where the fact is animal-grain
proof_artifact_id where the fact is proof/media-grain
```

`weighing_campaigns.status` is aggregate bookkeeping. It cannot answer whether a
shed, animal, proof, or operator submission is complete. Expected completion
comes from `weighing_expected_animals` joined to accepted observations at the
same campaign + animal grain. Extra scans come from observations with no matching
expected row and must remain outside the expected numerator.

## 4. Proposed backend module

Add a dedicated `weighing` module rather than encoding weighing as a
vaccination subtype.

Suggested package shape:

```text
backend/internal/weighing/
  domain/
  ports/
  app/
  adapters/postgres/
  adapters/http/
```

The module may reuse shared SOP/proof/media infrastructure through ports, but
it owns weighing-specific tables, planner policy, observations, and read models.
It must not write Vaccination tables or depend on Vaccination-specific protocol
rules. If generic SOP tasks are used for assignment/routing, their state is
aggregate bookkeeping only; Weighing's per-animal observation/proof tables remain
the source of completion truth.

## 5. Data model draft

Names are draft and should be reconciled against current migration conventions
before implementation.

### `weighing_campaigns`

| Column | Notes |
|---|---|
| `campaign_id uuid pk` | Idempotent campaign identity. |
| `tenant_id uuid not null` | Tenant boundary. |
| `farm_id` / `park_id` | Execution scope. Use current location model terminology at implementation time. |
| `period_type text not null` | `week`, `month`, or `manual_window`. |
| `period_start_date date not null` | Week start for weekly kid campaigns; month start for monthly adult campaigns; selected window start for manual exceptions. |
| `period_end_date date not null` | Week end, month end, or selected manual window end. |
| `cadence_type text not null` | `weekly_kids`, `monthly_adults`, or `manual_exception`. |
| `cadence_due_date date not null` | Monday for weekly kid work; 15th for monthly adult work; selected date for manual exception. |
| `display_week_start_date date` | Derived/display anchor for Android week tabs when a monthly/manual campaign appears inside a week view. |
| `display_month date` | Derived/display anchor for monthly adult overview. |
| `animal_group_filter text not null` | `kids_k_f`, `adult_goats`, or `manual_selected`. |
| `start_business_date date not null` | Day leadership created/scheduled work, e.g. 2026-07-29. |
| `status text not null` | `draft`, `planned`, `published`, `in_progress`, `delayed`, `completed`, `canceled`. |
| `planned_cap_per_day int not null` | Default 100 for v1; authored/configured later. |
| `operator_user_id uuid` | Amit for v1. |
| `published_at timestamptz` | Set when operator-visible work is created. |
| `completed_at timestamptz` | Set only when expected animals are weighed/unavailable/closed under policy. |
| `created_by`, `created_at`, `updated_at`, `row_version` | Audit/optimistic lock. |

### `weighing_campaign_sheds`

| Column | Notes |
|---|---|
| `campaign_shed_id uuid pk` | Row identity. |
| `campaign_id uuid not null` | Parent campaign. |
| `location_id uuid not null` | Physical shed or partition/cohort scope. |
| `location_type text not null` | `shed`, `cohort`, or implementation-supported partition grain. |
| `display_name text not null` | Snapshot label for audit/display. |
| `expected_animal_count int not null` | Snapshot count at planning time. |
| `status text not null` | `pending`, `in_progress`, `completed`, `canceled`. |
| `completed_at timestamptz` | Closed when expected membership is complete or leadership override closes it. |

### `weighing_work_groups`

| Column | Notes |
|---|---|
| `work_group_id uuid pk` | Suggested execution group. |
| `campaign_id uuid not null` | Parent campaign. |
| `planned_business_date date not null` | Suggested day. Moves forward if not completed. |
| `sequence_no int not null` | Stable order. |
| `status text not null` | `pending`, `in_progress`, `completed`, `delayed`, `canceled`. |
| `expected_count int not null` | Sum of selected group membership. |
| `effective_business_date date not null` | Current visible execution date after roll-forward. |
| `rolled_from_date date` | Last scheduled date when carried forward. |

Group membership should be a separate join table:

```text
weighing_work_group_sheds(work_group_id, campaign_shed_id)
```

The join table is the only source of work-group shed membership. Do not infer a
work group's sheds from planned date, display name, route params, or a generic
SOP parent task. This is the vaccination sibling-shed bug class in weighing
form.

Suggested work groups are planner output, not membership authority. The
membership authority remains `weighing_expected_animals`. A replan may change a
pending expected row's planned work group, but it must not rewrite which animals
were expected at campaign publication unless leadership explicitly edits the
selected sheds and the edit records a new membership snapshot version.

### `weighing_expected_animals`

Snapshot expected membership at campaign creation. This prevents later animal
movement from rewriting what the operator was asked to cover.

| Column | Notes |
|---|---|
| `campaign_id uuid not null` | Campaign. |
| `animal_id uuid not null` | Current canonical animal identity. |
| `expected_location_id uuid not null` | Shed/partition at planning time. |
| `expected_location_label text not null` | Snapshot display. |
| `expected_group_id uuid` | Optional planned group assignment. |
| `status text not null` | `pending`, `weighed`, `unavailable`, `missed`, `canceled`, `closed_by_override`. |
| `availability_status text` | `expected_shed`, `moved_other_shed`, `icu`, `quarantine`, `dead`, `culled`, `sold_transferred`, `exited`, `unknown_review`. |
| `current_location_id uuid` | Last projected location used for exception display, nullable. |
| `current_lifecycle_status text` | Last projected lifecycle state used for exception display, nullable. |
| `availability_checked_at timestamptz` | When the exception projection last reconciled this row. |

Unique key: `(campaign_id, animal_id)`.

If leadership can later add sheds to an active campaign, add
`membership_version int not null` and include it in the idempotency/audit story.
Implementation must choose either "campaign membership is immutable after
publish" or "membership versions are first-class"; it must not silently append
rows that make old progress/proof counts impossible to reconstruct.

### `weighing_observations`

| Column | Notes |
|---|---|
| `observation_id uuid pk` | Idempotent observation identity. |
| `campaign_id uuid not null` | Parent campaign. |
| `work_group_id uuid` | Work group being executed. |
| `animal_id uuid not null` | Resolved from RFID/Animal ID scan. |
| `scanned_identifier text not null` | Raw scanned value for audit. |
| `weight_kg numeric not null` | Positive measured weight. |
| `observed_at timestamptz not null` | Device/business timestamp. |
| `operator_user_id uuid not null` | Field operator. |
| `expected_location_id uuid` | From `weighing_expected_animals` if present. |
| `actual_location_id uuid` | Current animal location or selected execution context at scan time. |
| `location_match_status text not null` | `expected`, `other_shed`, `not_in_campaign`, `unknown`. |
| `proof_artifact_id uuid not null` | Mandatory per-animal video proof. |
| `status text not null` | `local_pending`, `submitted`, `accepted`, `rejected`, `voided`, `replaced`. |
| `correction_of_observation_id uuid` | Nullable pointer for audited replacement. |
| `sync_state/audit columns` | Follow existing mobile capture conventions. |

Uniqueness should prevent duplicate same-campaign animal observations unless an
explicit correction/replacement flow is added. V1 can use one accepted
observation per `(campaign_id, animal_id)`.

Expected-vs-extra counting rule:

- if `(campaign_id, animal_id)` exists in `weighing_expected_animals`, the
  accepted observation may satisfy that expected row;
- if it does not exist, the observation is `not_in_campaign` and contributes
  only to extra/mismatch insight counts;
- a not-in-campaign observation must never increment expected completion or
  silently create expected membership for another shed.

### `weighing_observation_corrections`

Corrections are explicit audit records, not destructive updates.

| Column | Notes |
|---|---|
| `correction_id uuid pk` | Row identity. |
| `observation_id uuid not null` | Observation being voided/replaced. |
| `replacement_observation_id uuid` | Nullable until replacement is accepted. |
| `reason text not null` | Stable reason code plus optional note. |
| `requested_by uuid not null` | Actor. |
| `created_at timestamptz not null` | Audit instant. |

### Durable media linkage

The proof/media link must be queryable in both directions:

- from observation to proof artifact for leadership/review display;
- from proof artifact to observation for upload retry/recovery;
- from idempotency key to existing accepted observation for replay.

Do not rely on transient Android form state or a generic SOP submission blob as
the only proof reference.

### Status and constraint requirements

Implementation must define persisted status values in migrations with `CHECK`
constraints and mirror them in domain value objects. At minimum, the design must
separate:

- campaign lifecycle: draft/planned/published/in_progress/delayed/completed/
  canceled;
- work-group lifecycle: pending/in_progress/delayed/completed/canceled;
- expected-animal status: pending/weighed/unavailable/missed/closed_by_override/
  canceled;
- observation status: pending_upload/submitted/accepted/rejected/voided/replaced.

Exact names should follow current migration conventions, but terminal states
must be immutable except explicit correction/reopen commands. Display buckets
such as "open today", "delayed", "moved", or "review needed" must not be
persisted as ad-hoc statuses unless the migration and state machine define them.

## 5.1 Count and projection grain

The first implementation must write the projection proof into the SQL/repository
next to the query marker `projection-review: weighing-progress`.

Required written proof:

```text
Producer unique key:
  weighing_expected_animals = tenant_id + campaign_id + animal_id
  weighing_observations accepted row = tenant_id + campaign_id + animal_id
Consumer group key:
  overview = tenant_id + campaign_id
  shed progress = tenant_id + campaign_id + campaign_shed_id
  work group progress = tenant_id + campaign_id + work_group_id
  animal row = tenant_id + campaign_id + animal_id
Joined side multiplicity:
  expected animal -> accepted observation is 0:1 in v1
  expected animal -> current animal state is 1:1 after tenant + animal filter
  observation -> proof artifact is 1:1 for accepted rows
Ratio/cap key set:
  numerator and denominator both range over campaign expected animals, never
  over visible page rows, current shed residents, or observations alone
```

Projection buckets are disjoint:

```text
expected_total
= weighed_expected
+ pending_expected
+ unavailable_expected
+ closed_by_leadership
```

Additional counters such as `wrong_shed_expected`, `not_in_campaign_scans`,
`proof_pending`, and `correction_pending` are insight counters. They may overlap
with operational states only when the API names that explicitly; they must not
be silently added into `expected_total`.

Never use `ORDER BY ... LIMIT 1` to bind an observation to a work group, shed, or
proof when multiple rows can legitimately exist for the same campaign. Use the
exact membership/proof key or reject the ambiguity.

## 6. Planning algorithm

Inputs:

- Selected shed/partition rows with expected animal counts.
- Cadence lane and animal group filter.
- `start_business_date`.
- `planned_cap_per_day`, default 100.
- Operator assignment, Amit for v1.

Algorithm:

1. Sort selected sheds/partitions by stable operational order.
2. Resolve expected animals using the campaign cadence filter. Weekly campaigns
   include kids/K/F-group animals only; monthly adult campaigns include adult
   goats due for that monthly lane; manual exceptions include explicitly
   selected membership.
3. Treat each selected shed/partition as an atomic item after filtering.
4. Build work groups greedily:
   - add the next item if it does not exceed cap;
   - if the current group is empty, add the item even when it exceeds cap;
   - otherwise close the current group and start the next.
5. Optionally fit a small later item into remaining capacity if it avoids a
   tiny group and does not reorder across parks/farms.
6. Assign suggested business dates starting at `start_business_date`.
7. Do not create hard errors for under-cap or over-cap groups when caused by
   atomic shed/partition rules.

The planner must be deterministic and idempotent for the same campaign snapshot.
Planner output is advisory until published. Once published, re-running the
planner must preserve completed observations, accepted proof, explicit
corrections, and in-progress operator work. A replan can only move pending
work-group dates or create new groups for newly added sheds.

Planner stability requirements:

- Sort by canonical farm/park order, then shed/partition operational order, then
  stable id. Do not rely on display labels alone.
- Store `planner_policy_version`, `input_snapshot_hash`, and `output_hash` on the
  plan/publish response.
- Same snapshot + same policy version must produce byte-identical work-group
  membership and sequence numbers.
- Capacity math uses operator-business-date grain. For v1 there is one operator
  (Amit), but the query/model must not collapse future multi-operator or
  multi-date capacity into a single campaign-level number.
- Replan only pending groups. Accepted observations, proof, corrections, and
  terminal groups are immutable unless an explicit correction/reopen command
  creates auditable new facts.

Vaccination comparison:

- Do not evaluate vaccine due windows, medical safe windows, compatibility,
  protocol rules, vial lots, or dose cells.
- Do reuse the concept that published/in-flight work is not silently destroyed.
  Replanning should preserve completed observations and explicit operator
  progress.

## 7. Rolling execution

At day boundary, a sweeper or read-model update marks incomplete current work as
still open and delayed/rolled forward. The work remains executable until all
expected animals are weighed or leadership cancels/closes the campaign.

Allowed execution cases:

- Operator completes only part of a suggested group.
- Operator completes one shed but not another shed in the same group.
- Operator weighs an animal from another shed during the same session.
- Task extends beyond the week end.
- Operator removes/replaces an unsynced proof locally before submission.
- Synced weight/proof needs correction through an audited replacement flow.

Forbidden behavior:

- Auto-cancel because week end passed.
- Auto-split shed/partition rows to force daily cap.
- Auto-move animal location because the animal was scanned in another shed.
- Accept a completed observation without mandatory proof video.
- Keep animals that are now dead, culled, sold/transferred, ICU, quarantine, or
  shifted elsewhere in the same "operator missed it" bucket forever.
- Change completion counts by reading only the visible/paginated rows.

## 8. Availability reconciliation

At planning time, `weighing_expected_animals` snapshots the selected shed/
partition membership. After that, other canonical workflows may change animal
availability before the operator weighs the animal:

- normal shed shift;
- ICU or quarantine movement/status;
- death;
- culling;
- sale/transfer/other lifecycle exit;
- identity/location correction.

Weighing must consume current herd/location/lifecycle truth to classify expected
animals before presenting misses. This should be implemented as a bounded
reconciliation path, not a full-tenant scan:

1. Reconcile open campaign expected rows by campaign/work group/shed.
2. Join current animal state by `tenant_id + animal_id`.
3. Update the expected row's `availability_status` and current-state snapshot.
4. Leave the original expected shed untouched for audit.
5. Remove unavailable animals from operator remaining workload counts where the
   status means the animal is not practically weighable.
6. Keep shifted-to-other-normal-shed animals visible as moved/mismatch; if
   scanned and weighed, record the observation with original expected shed plus
   actual/current shed.

This reconciliation must not write animal movement/lifecycle facts. It only
reads facts owned by Movement, Health/ICU/quarantine, Death/Culling, Sale/
Transfer, or identity correction workflows.

Availability events from other modules should invalidate/reconcile only affected
open campaigns by `tenant_id + animal_id`, not trigger a tenant-wide rebuild.
If a required lifecycle module is missing when this slice ships, record a
durable review-needed availability state rather than silently treating the
animal as pending forever.

Reconciliation query shape:

- Event-driven path: `tenant_id + animal_id` finds open
  `weighing_expected_animals` rows through an index on
  `(tenant_id, animal_id, status)`.
- Campaign catch-up path: bounded by `tenant_id + campaign_id + status` and
  paged by `(campaign_id, animal_id)`; no full-tenant scan.
- Current herd/location/lifecycle lookup must be batched by animal ids from the
  page and joined/pre-aggregated once. It must not issue one query per missing
  animal.
- Stale event protection must prevent an older movement/lifecycle event from
  overwriting a newer availability snapshot.

## 9. API draft

Leadership:

```text
GET  /api/v1/weighing/weeks?from=&to=
GET  /api/v1/weighing/months?from=&to=
POST /api/v1/weighing/campaigns
GET  /api/v1/weighing/campaigns/{campaign_id}
GET  /api/v1/weighing/campaigns/{campaign_id}/leadership-contract
PATCH /api/v1/weighing/campaigns/{campaign_id}
POST /api/v1/weighing/campaigns/{campaign_id}/plan
POST /api/v1/weighing/campaigns/{campaign_id}/publish
POST /api/v1/weighing/campaigns/{campaign_id}/close-pending
POST /api/v1/weighing/campaigns/{campaign_id}/cancel
```

Operator/mobile:

```text
GET  /api/v1/app/weighing/bootstrap
GET  /api/v1/app/weighing/weeks?from=&to=
GET  /api/v1/app/weighing/work-groups?date=&status=&cursor=&limit=
GET  /api/v1/app/weighing/work-groups/{work_group_id}
GET  /api/v1/app/weighing/work-groups/{work_group_id}/progress-contract
GET  /api/v1/app/weighing/work-groups/{work_group_id}/animals?status=&cursor=&limit=
POST /api/v1/app/weighing/observations
POST /api/v1/app/weighing/observations/{observation_id}/corrections
POST /api/v1/app/weighing/work-groups/{work_group_id}/submit-progress
```

All mutating routes require idempotency keys and semantic request fingerprints.
List endpoints must return `items`, `next_cursor`, `total`, and backend-owned
summary buckets. Android must not infer campaign totals from the current page.

API response contracts must include backend-owned:

- row IDs and row versions for campaign, work group, selected shed/partition,
  expected animal, observation, and proof artifact;
- disjoint progress buckets;
- mismatch and availability reason codes plus display labels;
- disabled reasons for publish, submit progress, replace proof, correct
  observation, close remaining, and cancel;
- media upload/recovery states and signed playback/download URLs where allowed;
- deep-link/tap targets for Android and admin-web leadership surfaces.

No client should derive these from local enum guesses or from the current page
of animals.

Backend-owned contracts must include labels, tones, disabled reasons, empty
states, route targets, proof media URLs, summary buckets, and
page-size-independent totals. Android and admin-web render these fields; they do
not hardcode bucket meanings.

API read contracts:

- `GET /api/v1/app/weighing/work-groups` returns an L1 page plus day/week
  markers and whole-filter summary buckets.
- `GET /api/v1/app/weighing/work-groups/{work_group_id}` returns only header,
  summary, selected shed memberships, and cursors for animal lists. It must not
  embed all animals for large groups.
- `GET /api/v1/app/weighing/work-groups/{work_group_id}/animals` is keyset
  paged by stable animal/work-row identity and accepts status/availability/shed
  filters.
- Leadership campaign detail returns summary buckets and paged drilldowns
  separately. A leadership card must not depend on fetching every animal row.
- Every list route must support stable `as_of` or revision semantics so counts
  and pages do not visibly fight each other during sync/reconciliation.

## 10. RBAC

Capabilities should map to existing role grants rather than inventing an admin
role:

| Capability | Actors |
|---|---|
| `weighing.plan` | CEO/CXO, preventive director |
| `weighing.monitor` | CEO/CXO, preventive director, relevant park leadership |
| `weighing.execute` | Operator, Amit in v1 |
| `weighing.verify` | Future verifier/supervisor route if proof review becomes explicit |

Dinakar has planning/monitoring capability, not execution capacity.

RBAC must be enforced in backend query predicates, not only by sidebar
visibility. Every route that accepts `campaign_id`, `work_group_id`,
`animal_id`, `location_id`, or `proof_artifact_id` must re-check tenant and
authorized park/farm/shed scope in SQL or the owning service before returning or
mutating the row. Android role gating is UX only.

## 11. Android UX contract

Sidebar:

- Add Weighing for CEO/CXO, preventive director, and operator personas.

Leadership screen:

- Week tabs.
- Campaign status for each week.
- Shed/partition selector.
- Expected count and suggested days.
- Operator assignment display.
- Progress summary: expected, weighed, pending, other-shed/mismatch, delayed.

Operator screen:

- Today's/open weighing work.
- Work group detail grouped by selected shed/partition.
- Scan-first flow.
- Weight input.
- Mandatory per-animal video capture.
- Same table for all scanned animals, with mismatch highlighting and columns for
  expected/original shed and actual/current shed.
- Pending section distinguishes true pending animals from unavailable/moved
  expected animals: shifted, ICU/quarantine, dead, culled, sold/transferred,
  exited, or review-needed.
- Offline queue and sync status consistent with current Android capture patterns.

Android data contract:

- Screen reads are network to Room to Flow to UI. Weighing must not ship a
  network-only read repository.
- Do not mutate `ScanViewModel` or `feature-scan` in place with Weighing-only
  assumptions. Extract reusable scan/proof renderer pieces behind neutral models
  if needed, keep the Vaccination adapter and tests intact, and add a separate
  Weighing adapter/route/viewmodel contract.
- Work groups, expected animals, observations, proof upload rows, and sync
  attempts are principal-scoped Room rows and are wiped on sign-out.
- L1 work groups and L2 animal rows use keyset paging with a phone-sized page.
- RFID lookup is O(1) against indexed Room/cache state, not a linear scan of a
  large in-memory list.
- UI states distinguish loading, cached/offline, empty, forbidden, backend
  error, sync pending, sync failed, and conflict.
- Route identity includes campaign id, work group id, selected shed/partition
  id, animal id, and proof id where relevant; never "pick first open group" after
  a scan or process restart.
- Per-animal proof capture must survive process death, retry, and offline
  re-entry until synced or explicitly removed by the operator before submit.
- Weight/proof capture writes Room first, queues an outbox command with a stable
  device event id, and syncs through the shared retry engine. The UI renders the
  Room row while upload/acceptance is pending.
- Uploaded-but-unsubmitted proof can be removed by the operator before final
  observation acceptance; this must call a real backend/media endpoint when the
  proof is already uploaded and then update Room.
- The sign-out wipe inventory must include weighing Room tables, proof/video
  cache files, upload work, outbox rows, and saved route/draft state.
- The scan screen must not fetch all expected animals into memory. It should
  page visible rows and resolve RFID through an indexed lookup/cache path.

## 12. Proof/media

Use the shared proof artifact/media system through a weighing-specific policy:

- `subject_scope = animal`
- `proof_mode = per_animal_video`
- exactly one mandatory video for each accepted weighing observation in v1
- capture source should prefer in-app camera where existing mobile policy
  requires it

Shed-level proof is not sufficient for Weighing v1.

Backend completion rule:

```text
accepted_weighing_observation
requires weight_kg > 0
and resolved animal_id
and proof_artifact_id with subject_scope=animal
and proof upload accepted/recoverable
and idempotent command accepted for the same semantic payload
```

An uploaded proof may be removed/replaced before final observation acceptance.
After acceptance, replacement is a correction event with preserved old proof
lineage.

Media implementation requirements:

- Video capture source, max duration/size, transcoding, upload retry, and signed
  URL generation must reuse the shared proof/media policy infrastructure.
- The backend must be able to recover from all partial states: proof uploaded
  without observation, observation submitted with proof still processing,
  idempotency replay after app reinstall/process death, and accepted observation
  whose playback URL needs regeneration.
- Proof lineage must preserve original and replacement artifacts for audits.
- Media URLs exposed to admin-web, Android, and leadership assistant surfaces
  must come from backend contract fields, not hand-built client paths.

## 13. Events and projections

Domain events:

- `weighing.campaign.planned`
- `weighing.work_group.planned`
- `weighing.observation.recorded`
- `weighing.work_group.progressed`
- `weighing.campaign.completed`
- `weighing.campaign.delayed`
- `weighing.expected_animal.availability_changed`
- `weighing.observation.corrected`
- `weighing.campaign.canceled`

External/cross-module events to consume:

- animal location changed;
- animal lifecycle/status changed;
- ICU/quarantine admission or release, if modeled separately;
- identity/RFID corrected or replaced;
- proof artifact accepted/rejected/removed, if proof uses its own event stream.

Every event added or consumed must be registered in
`context/architecture/domain-event-registry.json` with durable producer,
consumer, replay, DLQ, and E2E proof. A handler registered only in test wiring or
an unused bus is not accepted.

Read models:

- Leadership weekly weighing overview.
- Leadership monthly adult weighing overview.
- Operator open work groups.
- Campaign shed progress.
- Animal weight history / latest trusted weight projection.
- Other-shed mismatch summary.
- Missing/unavailable expected animal summary by reason.
- Proof recovery/review queue, if a proof upload exists without accepted
  observation or an observation references unavailable proof.

The implementation must register producer/consumer relationships in the domain
event registry and update leadership assistant coverage or document a deliberate
exclusion.

Projection grain requirements:

- expected progress source: `weighing_expected_animals`;
- accepted observation source: `weighing_observations`;
- media state source: proof/media tables through the proof port;
- availability source: current canonical herd/location/lifecycle projections;
- group membership source: `weighing_work_group_sheds`;
- totals are computed or projected over the full filtered set, never from one
  page of rows;
- every projection row must carry tenant, campaign, farm/park, work group and,
  where relevant, shed/partition grain.

Projection/update rules:

- Observation acceptance updates expected-animal status, progress counters,
  latest-weight candidate state, audit, and outbox in one transaction or through
  an idempotent outbox consumer with replay-safe aggregate versioning.
- Progress projections must be incrementally updated by campaign + shed + work
  group. A fallback full recompute is allowed only as a bounded repair job for a
  named campaign, never as a hot read path.
- Projection consumers must dedupe by event id and aggregate version. Replay
  must be safe after partial failure between observation acceptance, proof
  linkage, notification enqueue, and progress update.
- If no projection table is used for v1, canonical SQL reads must still satisfy
  the same grain proof, page-boundary tests, and 5k-to-50k latency envelope.

## 14. Idempotency and replay

Campaign creation:

- Key source: client idempotency key plus a semantic fingerprint containing
  tenant, farm/park scope, cadence type, cadence due date/month lane, animal
  group filter, selected shed/partition ids, expected membership snapshot or
  source revision, operator id, start business date, planned cap, and requested
  publish mode.
- Same key + same payload returns the existing campaign.
- Same key + different payload fails.

Observation submission:

- Key source: device idempotency key per animal scan/proof submission.
- Semantic fingerprint includes campaign, animal, weight, observed timestamp
  bucket or exact device event ID, and proof artifact reference.
- Same key replay returns existing observation.
- Same campaign + same animal duplicate without correction intent fails or
  returns the accepted observation depending on chosen API behavior.

Outbox consumers dedupe by event id and aggregate version.

Every idempotent write must have a matching database unique constraint or
reservation row. The TRD implementation is incomplete until the migration,
repository SQL, and tests prove:

- exact replay returns the original response and writes no new audit/outbox/media
  side effect;
- same key with different semantic fingerprint fails before mutation;
- duplicate same-campaign/same-animal observation fails unless it is an explicit
  correction/replacement;
- retry after partial proof upload recovers the same proof/observation lineage;
- terminal work-group/campaign states reject fresh side effects while allowing
  exact replay.

Submit-progress command:

- Key source: work group submit idempotency key.
- Semantic fingerprint includes campaign, work group, operator, completed
  observation ids, and expected-animal status revisions.
- Same key replay returns the same progress response.
- Same key with a different observation/proof set fails with no side effects.
- Terminal `completed`/`canceled` work groups reject new side effects except
  explicit correction/reopen commands.

## 15. Scale and query shape

Current release target follows the 5k-to-50k animal envelope. Avoid full-tenant
hot reads from Android or leadership dashboards.

Requirements:

- Campaign reads page by week/scope.
- Work group reads page by assigned operator/date/status.
- Observation lists page by campaign/work group/shed.
- Expected animal lists page by campaign/work group/status/availability.
- Animal expected membership snapshots are inserted set-wise.
- Progress projections are maintained incrementally by campaign/shed/work group.
- Summary buckets are computed over the full filtered result, not the current
  page.
- Projection membership source is `weighing_expected_animals` for expected
  progress and `weighing_observations` for extra/not-in-campaign scans; do not
  reconstruct membership from coincidentally equal shed/date fields.
- Indexed predicates keep typed columns bare; do not cast indexed UUID/text
  columns in predicates.
- Reconciliation after lifecycle/location events must be affected-animal
  bounded. Do not rebuild all open campaigns for a tenant on every shift/death/
  cull event.
- Read models serving Android and admin-web should have query-plan proof at the
  50k-animal release envelope and keep p90/p95 latency inside the API latency
  policy.

Required indexes should cover:

- `(tenant_id, period_type, period_start_date, status)`
- `(tenant_id, display_week_start_date, status)` for week-tab campaign lookup
- `(tenant_id, display_month, status)` for monthly adult overview lookup
- `(tenant_id, operator_user_id, planned_business_date, status)`
- `(campaign_id, location_id)`
- `(campaign_id, animal_id)`
- `(campaign_id, work_group_id, animal_id)`
- `(campaign_id, availability_status, status)`
- `(tenant_id, animal_id, status)` for affected-campaign reconciliation
- proof/media lookup by subject and aggregate id using existing proof patterns

Expected cardinality envelope for validation:

| Shape | Validation target |
|---|---:|
| Tenant animals | 5k, 25k, 50k |
| One large weekly campaign | 5k expected animals |
| Many open campaigns | 52 weekly campaigns with retained history |
| Observations/history | Multiple observations per animal across retained weeks |
| Android page size | Around 20 detail rows; no bulk page of 100/1000 |

Hot query contracts:

- Overview queries filter by `tenant_id + period_type + period_start_date/status`
  or by the relevant display anchor (`display_week_start_date` /
  `display_month`) and prebuilt or bounded progress buckets.
- Operator worklist filters by `tenant_id + operator_user_id + effective_business_date/status`
  and keyset cursor. It does not scan all campaign animals.
- Animal list filters by `tenant_id + campaign_id + work_group_id/status` or
  `campaign_shed_id/status` and keyset cursor.
- Observation submit performs one indexed lookup for expected membership and one
  indexed lookup for proof state. It must not load the whole work group.
- Availability reconciliation uses event-affected animals or campaign pages, not
  tenant-wide current herd scans.
- Media/proof display preloads proof metadata in one batched query for the page.
  No per-row signed URL/proof lookup loop.

Performance proof expected in implementation:

- EXPLAIN for campaign overview, operator work group list, expected animal list,
  observation insert lookup, and availability reconciliation at the 50k-animal
  envelope.
- Query-count tests proving no per-animal N+1 proof/media or current-location
  lookups.
- Page-boundary tests proving totals do not change when page size changes.
- API latency proof against the repo policy budget for operator worklist, work
  group detail, animal page, and leadership overview.

## 16. Notifications and reminders

V1 notification rules:

| Trigger | Audience | Route | Dedupe key |
|---|---|---|---|
| Campaign published | Amit | Open weighing work group | campaign + operator |
| Day-start open work | Amit | Today/open weighing work | operator + business date |
| Proof failed or missing after submit | Amit | Proof repair screen | observation/proof artifact |
| Campaign delayed after week end or expected finish | CEO/CXO + preventive director | Campaign progress | campaign + delayed date |
| Review-needed availability | Preventive director | Missing/review bucket | campaign + animal/status revision |

Avoid noisy per-animal pushes. Leadership summaries include missing/unavailable
counts without blaming the operator for animals unavailable due to canonical
lifecycle/location facts.

Notification rows must be durable. Delivery failures belong in the shared
notification retry/DLQ path.

Notification contracts must name trigger, audience source, cadence/SLA, summary
copy fields, and tap route. Audience resolution must come from active role
grants/profile truth and assigned operator rows, not hardcoded names. V1
specifics:

- Amit receives operator assignment, daily open-work, sync-failed/proof-failed
  nudges.
- CEO/CXO and preventive director receive delayed/open summary notifications
  when a campaign rolls beyond the planned week or has unresolved review-needed
  animals.
- Dinakar receives planner/supervisor notifications, not operator execution
  assignments.

Notification durability contract:

- Assignment, day-start reminder, rolled-forward reminder, delayed-beyond-week,
  proof-upload-failed, and leadership summary notifications are created as
  durable notification requests from backend/domain events.
- Recipient resolution reads active role grants/profile truth. Dinakar receives
  planning/monitoring notifications, not operator execution pushes.
- A failed FCM/send does not change campaign/work-group state. It retries through
  the shared notification dispatcher and becomes visible in DLQ/repair tooling if
  exhausted.
- Notification tap routes include enough identity to land on the exact weighing
  campaign/work group/shed/proof review context, not "first open weighing task".

## 16.1 Observability and operations

Weighing must emit structured logs, metrics, and traces with low-cardinality
labels plus request/event ids:

- Planner: campaign id, policy version, selected shed count, expected count,
  generated group count, cap, duration, and replan reason.
- Observation submit: campaign id, work group id, animal id hash/id, proof id,
  idempotency replay/conflict, location match status, and latency.
- Proof upload/recovery: proof id, observation id, upload state, retry count, and
  recovery outcome.
- Availability reconciliation: campaign id or event animal id, affected expected
  row count, status transitions by bucket, stale-event drops, and duration.
- Projection refresh: rows touched, previous/current aggregate version, retry
  count, and DLQ reason on failure.
- Notification: notification request id, event id, recipient role, delivery
  state, retry count, and tap route type.

Operational runbooks must cover: replaying a stuck observation/proof link,
reconciling availability for one campaign, regenerating progress for one
campaign, inspecting delayed work, and diagnosing why a notification did not
reach Amit or leadership.

## 17. Tests and validation

Minimum tests before implementation is considered done:

- Planner preserves shed/partition atomicity under cap.
- Weekly kid/K/F cadence and monthly adult cadence are represented separately.
- Adult sheds are not included in weekly campaign membership unless the campaign
  is a manual exception.
- Planner allows over-cap single shed/partition.
- Planner groups `80 + 20` but execution allows `80` today and rolls `20`.
- Campaign created on 2026-07-29 inside week 2026-07-26..2026-08-01 can finish
  after 2026-08-01.
- Dinakar can plan/monitor but is not counted as operator capacity.
- Amit can execute.
- Wrong-shed animal scan records in same table with mismatch status.
- Expected animal shifted to another shed is not shown as an ordinary miss; if
  scanned, observation records original and actual/current shed.
- Expected animal moved to ICU/quarantine is classified unavailable and removed
  from ordinary remaining workload.
- Expected animal marked dead/culled/sold/transferred/exited after planning is
  classified as lifecycle exit and not held open as operator pending.
- Unknown current state remains review-needed, not completed.
- Observation without proof video is rejected.
- Duplicate observation/idempotency replay is safe.
- Android offline capture sync preserves proof and weight.
- Progress read models do not multiply rows across sheds/work groups.
- Summary totals remain correct beyond page one.
- Proof upload recovery restores observation proof refs after retry/process
  death.
- Completed/canceled work group rejects fresh submit side effects, while exact
  idempotency replay is allowed.
- Backend-owned contract supplies labels/disabled reasons for leadership and
  Android; clients do not hardcode bucket semantics.
- Leadership close/adjust command requires reason code, actor, timestamp,
  affected count, and animal list snapshot; closed animals leave operator
  workload but remain in audit/reporting.
- Domain event registry covers every weighing producer and consumer.
- Leadership assistant coverage is updated or an explicit exclusion is
  documented.
- Backend migration constraints reject illegal statuses, impossible foreign
  grains, duplicate accepted observations, and orphan proof links.
- API integration tests prove tenant/scope enforcement for campaign, work group,
  animal, location, and proof ids.
- Projection tests include a work group with multiple sheds where only one shed
  submits today and the sibling shed remains pending.
- Fanout/retry tests prove observation acceptance still reaches progress,
  latest-weight projection, notification, and media/recovery queues after a
  transient consumer failure.
- Android tests cover process death/reopen, sign-out wipe, page-2 animals,
  O(1) RFID lookup, proof removal after upload but before acceptance, and route
  identity after scan.
- Admin-web tests, if leadership screens are added there, prove backend contract
  labels/media URLs are rendered without local fallback copy.
- 5k/25k/50k seed fixtures prove hot query plans remain indexed and bounded.
- Query-count tests prove proof/media and current-location lookups are batched.
- Multi-page campaign tests prove whole-task summaries are stable across page
  size and cursor boundaries.
- Planner hash tests prove deterministic output for identical inputs and safe
  pending-only replan for changed inputs.
- Notification tests prove durable request creation, exact replay, retry/DLQ
  behavior, and role-correct recipients.
- Observability tests or golden log/metric assertions cover key failure paths:
  idempotency conflict, proof missing, wrong-shed scan, stale availability event,
  projection retry, and notification failure.

Implementation guard targets to add:

- `weighing-planner-atomicity-guard`
- `weighing-observation-proof-guard`
- `weighing-progress-grain-guard`
- `weighing-mobile-contract-guard`
- `weighing-availability-reconciliation-guard`
- `weighing-query-plan-guard`
- `weighing-notification-durability-guard`
- `weighing-observability-contract-guard`

## 18. Open decisions

- Exact canonical naming: `animal_id`/`herd_animals` target versus current
  `goat_id`/`goats` implementation names during this slice.
- Whether leadership can manually close remaining pending animals as
  `closed_by_override` in v1 or only cancel the whole campaign.
- Whether a wrong-shed weighing observation should trigger a suggested movement
  review item immediately or only appear in weighing mismatch reports.
- Which lifecycle statuses should automatically remove an expected animal from
  remaining workload versus require supervisor confirmation.
- Whether weight values need verifier review before becoming the trusted latest
  weight projection.
- Whether the daily cap should be tenant-wide, farm-specific, or operator
  configuration in v1. Product default is 100.
- Whether accepted weighing immediately updates the canonical latest-weight
  projection or waits for optional supervisor verification.
- Which existing proof review/verifier roles, if any, can reject an animal-level
  weighing video in v1.
- Whether active campaign membership is immutable after publish or supports
  versioned add/remove edits.
- Whether v1 stores progress projections or serves canonical indexed SQL only
  under the 5k-to-50k envelope.
- Whether the adult monthly lane appears as month tabs, a dated 15th card inside
  the week containing the 15th, or both.
