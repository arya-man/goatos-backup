# Operational Task Kernel Remediation Plan

Status: implementation plan; policy decisions listed below remain explicit
gates.

Source review snapshot: `97e2b462cc4cc39e78b3c99209a09f739c3d2e31`

Fresh-main counter-review snapshot:
`4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f`

Final counter-review correction snapshot:
`07640ad5388ffb6ed87701ea7f4f522c92fb3e82`

Companion bug queue:
[current whole-project remediation ledger](../repo-audits/current-whole-project-remediation-ledger.md)

## Outcome

Build one durable coordination layer that can answer, before an app opens:

- what work exists;
- who owns it now;
- what clock applies;
- how it rolls up through a bounded hierarchy;
- what separate verification or sign-off work is outstanding;
- whom to contact, through which channel, and when to stop contacting them.

Do this without replacing module-owned business state machines, inventing
owners, adding another scheduler binary, or creating screen-specific projection
tables at the current 5k-to-50k scale envelope.

The build is not “unify all enums.” The audited primary lower-bound inventory
contains 28 human-work, container, and proof tables with 31 independently
constrained state columns; secondary procurement, escalation, and technical
status families make the repository-wide total larger.
Several modules have useful local hierarchy, but no generic parent pointer,
generic owner/clock contract, or generic upward close/reopen mechanism. Keep
those domain rows authoritative for domain facts; add canonical shared rows only
for cross-module coordination facts.

## Fix first, design in parallel

Kernel design can run while independent application bugs are being repaired,
but activation has hard dependencies:

```text
notification compatibility + release/migration safety
  -> authoritative owner/duty/clock data
  -> corrected module source behavior
  -> task-node vertical slice in shadow
  -> Today cutover + escalation activation
  -> hierarchy/sign-off rollout
  -> additional modules and channels
```

Do not wait for every P2 cleanup item. Do wait for the companion ledger's N0,
F0, S0, and the selected module's D0 roots. New shared schema must not encode
today's missing receipts, stale leases, silent recipient loss, cross-park scope
bugs, or invalid notification values.

## What exists today

Keep and extend these proven parts:

- canonical PostgreSQL transactions with audit, idempotency, and outbox;
- the consolidated kernel worker and its current one-minute, five-minute, and
  hourly lanes;
- obligation generation, batches, SOP tasks, proof, verification, notification
  requests, dispatcher retries/DLQ, and canonical indexed reads;
- module-local hierarchy such as Health case -> session -> step and Weighing
  campaign -> shed -> work item;
- actor-scoped `/app/tasks`, which is already keyset-paginated and capped at 20.

Do not describe the operational cadence as 15 minutes. Runtime registration on
the reviewed source runs the shared operational lane every **five minutes** and
performs an immediate startup catch-up. The older 15-minute prose is stale and
must be corrected when the affected architecture docs are next edited.

The missing pieces are:

- a canonical generic task node and bounded parent hierarchy;
- mandatory authoritative ownership plus assignment history;
- one clock/timing policy contract across modules;
- generic close/reopen propagation and separate sign-off tasks;
- a real effective-duty resolver that handles shifts, absence, replacement,
  week-off, and backup;
- a run/step/contact escalation model gated by acknowledgement and delivery;
- a Room-backed, business-day personal task board;
- explicit materialization failures and reconciliation.

### Audited lower-bound state inventory

The reproducible 28-table primary inventory is:

- Obligation/SOP/verification: `obligation_batches`, `obligation_instances`,
  `sop_tasks`, `sop_submissions`, `sop_submission_items`, `verification_items`;
- Vaccination/Feed: `vaccination_completions`,
  `feed_direction_completions`, `feed_direction_session_completions`,
  `feed_distribution_completions`, `feed_packing_completions`,
  `feed_transport_tasks`, `feed_transport_attempts`;
- Workflow/Weighing: `workflow_instances`, `workflow_actions`,
  `weighing_campaigns`, `weighing_campaign_sheds`, `weighing_work_items`,
  `weighing_observations`, `weighing_shed_observations`;
- Shifting/Milk/Health/Counts: `shifting_events`,
  `milk_preparation_completions`, `milk_feeding_tasks`, `health_cases`,
  `health_treatment_sessions`, `health_session_steps`,
  `counts_approval_requests`, `count_projection_exceptions`.

Each contributes at least one independent CHECK-constrained state column;
`shifting_events` contributes three and `count_projection_exceptions` two, for
31 columns. This is an audited coordination-focused lower bound, not a claim
that secondary procurement, escalation, import, or technical statuses do not
exist. The implementation must regenerate this inventory/query at cutover and
state every inclusion and exclusion; it must not use the count to pull Weighing
inside the generic boundary.

## Architecture boundaries

### Domain facts stay with their module

Module tables remain authoritative for vaccination completion, feed submission,
health treatment, shifting, procurement, counts, milk, and other domain facts.
The generic state is a coordination state; it does not replace local enums.

A human sub-task becomes `done` when that person's work is complete. If another
person must verify it, operator and verifier are separate owned leaves beneath a
shared work-unit container. The owning module command still performs the domain
transition. A task command must not update another module's tables directly.

### `task_nodes` is canonical coordination state, not a read projection

At the active scale envelope, screens read canonical indexed SQL. Do not build
an eventually consistent task projection per module. The owning app transaction
must write the domain change, task coordination change, audit/history,
idempotency, and outbox atomically through a shared transaction-aware port.

If a source cannot meet that contract, keep it off the generic board until an
explicit ADR defines lag, failure visibility, reconciliation, and cutover. An
outbox-only asynchronous adapter may feed analytics or an external oversight
view; it must not silently become the source of today's operational task truth.

### No fake owner

An owner is a canonical workforce member, not a device, role slug, grant, or
arbitrary user UUID. An app-visible owner also needs an active, unique tenant
user mapping. Store tenant-safe workforce identity and assignment history. A
real owner without a delivery token remains the owner; delivery reports
`no_route` and escalation continues according to policy. A missing or ambiguous
user mapping is a distinct materialization failure. Authorization grants may
filter what a person can do, but they do not prove that person is scheduled,
present, or the work owner.

If ownership cannot be resolved, do not choose a plausible default. Fail a
user-created command or write a durable, visible materialization exception for
generated work. The exception must alert an accountable resolver and remain
replayable; an omitted task is not success.

Some current sources behave like claim queues rather than assignments. Do not
silently make canonical ownership nullable. Inventory each pool-like source and
record a product decision: the default is a named effective-duty owner or a
materialization exception. If a workflow is explicitly approved as claim-based,
model a canonical claim pool with an accountable owner/SLA, eligibility rules,
an atomic compare-and-set claim, assignment history, nullable-owner constraint,
and separate eligible-pool Today predicate. Keep claim pools out of the
Vaccination K1/K2 slice and the generic board until that complete contract is
approved; an unresolved assigned task must never fall back into a pool.

### Weighing remains isolated

Weighing must not read task, SOP, obligation, roster, animal-lifecycle, cadence,
overdue, or missed state, and generic tasks must never gate capture or close.
Repair `WEIGH-001..006` inside Weighing. If leadership later needs a generic
oversight view, consume Weighing outbox events outward only after an explicit
scope ruling and guard amendment. Weighing is not an initial task-node adapter.

## Canonical data contracts

Names below are design-level; the schema review may refine names without
weakening the invariants.

### Task node

Required fields and constraints:

- `(tenant_id, task_node_id)` primary identity;
- immutable `parent_task_node_id` with a tenant-scoped composite self-FK;
- `tier` and `kind`, with database-checked compatible parent/tier rules;
- `(source_module, source_type, source_id, source_part)` stable, non-null unique
  idempotency key within the tenant; use `source_part=''` when there is exactly
  one task per source. Otherwise `source_part` names the independently owned
  coordination unit or state axis (for example operator, verifier, sign-off, or
  a declared multi-axis source part). Every adapter enumerates its parts; do not
  blindly turn every status column into a task;
- mutable `source_row_version` as a concurrency fence, never as part of task
  identity;
- canonical `owner_workforce_member_id`, assignment source, assigned-at, and a
  separate append-only assignment history;
- timing class, window start/deadline, business timezone/date, hard-miss rule,
  and pinned clock-policy version;
- coordination state, terminal outcome/reason, defer-until/reason, completion
  actor/time, and `row_version`;
- immediate-child counters needed for bounded rollup;
- created/updated audit fields.

Parent rules:

1. The parent must exist before the child and is immutable after insert. Enforce
   immutability in PostgreSQL, not only service code. That, plus a no-self-parent
   check, prevents cycles without an unbounded recursive write-time scan.
2. Choose and document a maximum supported depth before migration. Enforce it
   with a normalized allowed-edge table plus tenant/tier-aware composite parent
   keys or a deferred constraint trigger; an ordinary CHECK cannot inspect the
   parent row. Keep the service validation as an earlier error, not the sole
   enforcement.
3. A child insert must lock and reject a parent that is closing or closed unless
   the same command is an authorized reopen.
4. Source identity is immutable. Rescope creates a declared move/supersession
   operation with old/new ancestry recomputation; it is not an arbitrary parent
   edit. Lock old and new parents in deterministic ID order.
5. Each adapter defines tombstone, delete, source-rebuild, and source-generation
   behavior. After cutover, a source hard delete must not orphan or silently
   recreate work; preserve a tombstone/supersession fact or fail into explicit
   repair with the stable source identity.

### Parent counters and state propagation

Do not scan all siblings on every child write, and do not promise transactional
counters without locking any parent. Use this bounded algorithm:

1. lock the immediate parent in one consistent order;
2. apply an idempotent child-state delta to its counters;
3. when closing, verify the terminal condition with an indexed canonical
   `NOT EXISTS` check;
4. transition/reopen the parent under the same transaction;
5. propagate to the next ancestor only when the parent's aggregate state class
   changed, so an epic is not locked for every animal write;
6. run a bounded drift reconciler and alert on any mismatch.

Set-based sources such as batch materialization and missed sweeps must emit one
deduplicated transition set, aggregate counter deltas per parent, lock affected
parents in deterministic order, and apply one delta per parent. Persist a unique
transition receipt (for example child plus from/to version or event identity) or
derive old/new state classes under the child lock so replay cannot double-apply.

Define before implementation whether deferred counts as open, which terminal
outcomes close a parent, when reopen is legal, how clocks behave in each state,
and how close versus new-child races are fenced.

### Sign-off

Verification or leadership approval is a separate task node with its own owner,
clock, proof reference, and state. Operator and verifier leaves are siblings
under a work-unit container, so the operator leaf can become Done immediately
while the container waits for sign-off. Sign-off is not a Boolean on the
operator leaf and not an implicit verification status hidden from My Tasks.
Bind existing `verification_items`/SOP verification facts to that one canonical
verifier leaf with a stable dedup key; declare which row is visible on Today so
the same review never appears twice.

### Effective duty and assignment

Make `workforce_roster_assignments` production-owned. Its current dormant shape
is not sufficient: add a tenant-safe position FK, normalize the assignment scope
vocabulary, replace the user-domain escalation owner with canonical workforce
identity, define the status machine, add a deterministic materializer key and
overlap rules, and index scope/time/status lookup. Supply authoring or seed plus
a recurring-pattern materializer and a production reader/writer.

Make dated `workforce_roster_assignments` the sole effective-duty truth. Choose
one recurrence authoring source that materializes those dated assignments;
`workforce_positions.week_off_weekday` and
`vaccination_operator_shift_config` currently overlap and cannot remain parallel
effective truths. Repoint the drive planner and effective-duty SQL to dated
assignments, reconcile transitional recurrence sources, prove parity, and retire
them only through the compatibility gate.

The resolver returns structured state, not only a recipient array:

```text
DutyResolution {
  status: resolved | uncovered | ambiguous
  members: [...]              # owners, independent of devices
  coverage_source: scheduled | absence_replacement | week_off_backup | ...
  reason: ...
}
```

Specify precedence and tests for multiple active seats, cross-midnight shifts,
overlapping absence and week-off, replacement absence, backup validity, inactive
holders, missing seats, and conflicting assignments. Resolve ownership first;
resolve delivery devices/channels separately.

Inventory every current single/batch member, duty, position, and Vaccination
effective-owner resolver as replace, repoint, or intentionally retain. The
canonical resolver is read-only: a GET must not mint temporary grants. Grant
materialization belongs to roster/coverage changes. Split member resolution from
device resolution before Today/escalation activation; a real owner with no
reachable device records durable `no_route`, this is not an ownership failure,
and the work clock continues.

Canonical task ownership is `workforce_member_id`. App authorization maps that
member to exactly one active tenant `user_id`; shadow comparison is on member
identity, not user/device. Missing, ambiguous, inactive, or cross-tenant mappings
produce a deduplicated materialization exception and reconciliation outcome.
Before cutover, report mapping coverage, expected exception volume, abort
threshold, and resolver capacity; main roster seeds do not prove every member
has a login mapping.

Materialization exceptions need their own contract: stable source identity,
class/fingerprint, source version and payload, status/attempts/next retry,
first/last seen, resolver owner/SLA, alert route, unique dedupe, and replay
receipt. Route them to a configuration/operations owner that does not depend on
the failed duty resolution. Grants may define an explicit leadership delivery
audience or approved leadership accountability; they do not prove scheduled
operational ownership.

Duty, position, absence, replacement, and roster changes enqueue bounded,
idempotent re-resolution of unstarted affected tasks with a source watermark.
Preserve in-progress/completed work according to versioned policy, record every
assignment change, and run a drift reconciler. Generalize the existing
Vaccination leave-change behavior rather than adding an unrelated path.

### Escalation run, step, and contact attempt

Do not activate a new waterfall on the one-row-per-obligation-level shape. It
cannot represent a reopened run, parallel recipients, multiple channels, or a
delivery-failure jump. Introduce the general shape before the FCM slice goes
live:

- `escalation_runs`: task, breach/run identity, timing class, policy snapshot,
  work deadline, status, acknowledged/resolved actor/time, row version;
- `escalation_steps`: ordered human level, `next_contact_at`, recipient rule,
  channel plan, lease token/time, step state;
- `contact_attempts`: exact person/device/channel identity, delivery state,
  attempt idempotency key, provider reference, retry/failure reason.

Policies have versions and effective dates; every run pins an immutable
snapshot, including timezone, quiet/contact windows, an explicit no-quiet-hours
value when applicable, and emergency or delivery-failure override behavior.
Acknowledgement stops future contacts by clearing `next_contact_at`
and atomically canceling/fencing every future or open step. It does not alter
the work deadline or erase the violation. A contact worker must recheck run and
step state immediately before the external send so an already leased attempt
cannot contact after acknowledgement. Completion or cancellation resolves the
run. Before every advance, re-read canonical task state and use
compare-and-set/row version to settle acknowledgement, completion, and worker
races.

Use the existing notification dispatcher as the sole external-send engine.
Each contact attempt creates or binds one `notification_request` carrying its
run/step identity. Claiming and immediately before gateway send must fail-closed
recheck run/step state and fence acknowledgement, resolution, suppression, and
version changes; delivery evidence updates notification and contact state
consistently. Do not add a second contact worker with independent leases,
suppression, retry, or evidence state.

Delivery failure may advance exactly one human level immediately when the
pinned policy says so. Retryable channel failure may use a short bounded retry,
but it cannot consume the human acknowledgement window indefinitely. Flexible
work receives badge/digest behavior; it does not page leadership merely because
it is old.

## Delivery milestones

These are dependency milestones, not promises of calendar weeks. Independent
items inside a milestone may run in parallel. Re-estimate after schema/API
review and the first real-PostgreSQL proof.

### K0 — prerequisite correctness

- Complete companion-ledger N0 and the applicable F0/S0 gates.
- Require the companion ledger's D0 closure for `KERN-002..010`; D0 is the
  owning batch for those reusable reliability fixes.
- Select Vaccination as the first shadow module. Correct `VAX-002..009` and the
  shared `KERN-002..010` roots before creating its adapter; Health remains the
  hierarchy reference, not the first cutover.
- Record the policy decisions below in canonical docs.

Exit: notification types and emitted escalation roles write successfully;
release/migration proofs are trustworthy; no selected source command silently
loses required state/outbox; authorization is capability-bound; every selected
policy decision is recorded in its canonical source.

### K1 — owner, duty, clock, task envelope, and stable ancestry

- Make roster assignments authoritative and implement the effective-duty
  resolver.
- Add the canonical task-node schema including parent/tier/depth and immutable
  stable ancestry, assignment history, clock policy, materialization exceptions,
  audit/outbox, and reconciliation command. Hierarchical rollup can remain
  feature-disabled until K3, but ancestry cannot be bolted onto live flat rows
  later. A controlled ancestry backfill exception is allowed only before any
  module cutover, through a one-time audited command; after that gate, parents
  are immutable.
- Supply owner and due/window at work creation. Vaccination bulk SOP creation
  currently supplies neither and must be repaired.
- Before K2 cutover, repair the current person-display fallbacks in
  `apps/admin-web/features/people/positions-panel.tsx` and
  `apps/admin-web/features/people/timetable-panel.tsx`: missing
  `person_display_name` must render an explicit unassigned/data-integrity state,
  and an unresolved seat must not count toward named-person availability or
  capacity. Never substitute `position_title` or `position_code` as the
  owner/person name. Inventory sibling owner/person fallbacks and add
  fail-honest regressions.
- Persist Vaccination's stable campaign/epic identity and shadow-backfill its
  epic/batch/work-unit/leaf ancestry before any K2 read cutover. K3 may activate
  rollup later, but it must not add parents after ancestry becomes immutable.
- Add the Vaccination source adapter behind a durable tenant+module flag,
  initially shadow-only. Define config ownership, default-off and stale-config
  behavior, audit, rollback, and per-tenant/module read/write/cutover grain.
- Seed roster/clock/task prerequisites and add them to seed closeout. Run a
  resumable selected-module owner/clock/task backfill with a run ledger before
  K2; do not leave existing ownerless/dueless SOP rows invisible.
- Before final DDL, model active/write/history/contact volume, retention, purge
  or archive, index growth, and partition thresholds. Add every introduced task,
  history, assignment, exception, and escalation table to the hot-migration
  validator inventory in the introducing migration.

Exit: every shadow task has a real owner and policy-pinned clock or a loud
materialization exception; replay creates no duplicate; source and task changes
commit atomically. A K1 kill switch stops new shadow writes without deleting
compatible schema or evidence, and the backfill/reconciler can resume safely.

### K2 — personal Today and generic FCM escalation

- Extend existing `/app/tasks`. Add the India business-day/active-state horizon
  and exact matching indexes on both legacy and task-node sources, using
  lock-safe `CONCURRENTLY` rollout, restart proof, and query plans. Retain the
  required `total` contract until an additive OpenAPI/client deprecation proves
  it can be removed; otherwise supply a bounded, correct total. The Today
  predicate is owner mapped to the actor, non-done/non-terminal/non-canceled,
  due before the next Asia/Kolkata midnight, and not deferred beyond `now`.
  Define per-adapter defer mapping: NULL means active/not deferred, source-only
  deferred status needs an explicit policy/timestamp mapping, and comparisons
  use the pinned India instant/day grain.
- During shadow, keep the current SOP result authoritative and expose comparison
  only to operator/admin diagnostics. Cut over one module allowlist at a time:
  return `task_nodes` for that module and legacy SOP rows for uncut modules,
  suppress duplicates by stable source identity, and paginate through one
  globally ordered SQL `UNION ALL` (or an equivalent cursor with per-source
  positions) before LIMIT. Include cutover/config generation, source
  discriminator, null-due class, due, stable source identity, and row ID in both
  the order and cursor; reject/restart a stale-generation cursor. Compute
  `total` over that same normalized, deduplicated visibility set. Never page each
  source independently and merge afterward.
- Preserve legacy `task_id` for SOP-backed rows and never pass a task-node UUID
  to legacy detail/submission/scan endpoints. Evolve the response additively
  with a discriminator, stable reference/capabilities/action links, regenerate
  OpenAPI clients in the same change, use `(kind,id)` for Room identity, and
  define cache migration/invalidation. Schema-additive fields alone do not make
  old Android behavior compatible: before returning task-node rows, either give
  every existing task-scoped endpoint a compatibility facade or enforce a
  minimum client/API capability with fail-closed cached/offline update-gate and
  rollback proof.
- Add an Android Room/Paging consumer only after `MOB-001` and M0's
  `MOB-002/008/010` overlay contract are fixed; reuse that durable queued,
  success, and terminal-failure state rather than adding a second overlay.
- Implement the full escalation run/step/contact schema with named-person FCM
  as the first channel.
- Add acknowledgement, completion/cancel resolution, delivery failure, leasing,
  idempotency, and reconciliation.
- Cut over from the live `obligation_escalations` lane explicitly: name the
  owner of each lane during shadow, suppress one lane per tenant/module, fence
  acknowledgement/resolution across both, choose open-row migration versus
  drain, seed already-fired levels so activation cannot resend, reconcile, and
  retire the legacy table/sweep only after zero-use proof.

Exit: owner-correct Today works before app open after the normal materialization
cadence; shadow/cutover pagination contains no duplicate or skipped task; ack
fences every future contact but not the work clock; a reopened run has a new
identity; no role slug is sent to FCM. A K2 rollback disables task-node reads and
new escalation generation while preserving legacy-compatible reads, open-run
evidence, and a safe drain path. Switching back is atomic per tenant/module: it
projects fired, acknowledged, and resolved fences into the legacy lane before
legacy generation resumes. If that projection cannot be proven, fail closed and
page the accountable operator rather than leaving both lanes disabled.

### K3 — hierarchy and sign-off

- Activate the K1 parent hierarchy and tier rules.
- Implement immediate-parent counters, close/reopen propagation, fencing, and
  drift reconciliation.
- Model verifier/CEO sign-off as separate owned task nodes.
- Activate the already-shadowed Vaccination epic/batch/work-unit/leaf ancestry
  without creating an SOP task per goat.

Exit: race tests prove close versus child creation, reopen after close,
idempotent replay, rescope, terminal/defer behavior, and sign-off ownership.
No sibling scan appears in a child hot path. K3 activation can be disabled
without mutating established ancestry; schema and transition receipts remain
backward-compatible through the acceptance window.

### K4 — rollout and additional modules

For each module:

1. close its source-ledger roots;
2. define source identity, owner, clock, state mapping, proof/sign-off policy,
   and terminal/reopen rules;
3. shadow-write and reconcile;
4. backfill with a resumable keyset command;
5. compare source/task counts and states;
6. enable canonical reads for an allowlisted tenant/module;
7. retain rollback and reconciliation until the acceptance window passes.

Every adapter checklist includes an owner/clock creation audit and existing-row
backfill, with any pool-like workflow explicitly classified by the ownership
decision above. Record per-tenant backfill runs, seed/closeout changes, counts,
watermarks, exceptions, retries, and rollback.

Use Health as the structural reference for three levels. Vaccination is the
first business vertical only after its source defects and stable epic identity
are fixed. Onboard Feed/Milk/Procurement/Shifting only after their companion
ledger batches. Keep Weighing outside this adapter list.

### K5 — channels and scale-out

- Add WhatsApp and voice behind the existing gateway only after vendor,
  consent, regional, retry, delivery-receipt, and acknowledgement contracts are
  approved.
- Add sideways/parallel contact plans through contact attempts, not duplicated
  escalation rows.
- Keep one consolidated worker at the current envelope. Split workers or add a
  screen projection only after measured saturation and the scale ADR's ladder.

## Proof matrix

Every milestone must supply:

- real-PostgreSQL production writer/reader tests;
- duplicate, replay, stale-version, concurrent-close, and worker-lease tests;
- cross-tenant and cross-park authorization tests;
- owner absence/replacement/week-off/cross-midnight tests;
- India business-day and restart/late-catch-up tests;
- ack/completion/delivery race tests and exact contact idempotency;
- migration lock contention, interruption, rerun, and old/new binary overlap;
- bounded query plans at the active upper envelope (up to roughly 500k retained
  obligation/task rows), with keyset reads and no unbounded sibling scans;
- Android process death, offline replay, logout generation, and Room migration;
- backlog age, materialization failure, uncovered duty, reconciliation drift,
  escalation deadline, delivery failure, DLQ, and no-output observability.

If a required interruption, race, process-death, or real-PostgreSQL harness does
not yet exist for that exact production path, building the harness is milestone
work, not a reason to substitute a mock or prose proof.

Do not claim completion from unit tests, a mock board, a prose review, or a
successful enqueue alone.

## Policy and documentation gates

The target model settles several product directions, but the corresponding
recorded decisions and guards must be superseded in the same implementation
change:

- operator sub-task Done at own-work completion; verifier is a separate task;
- animal-level `task_node` children are coordination rows, not one `sop_task`
  per goat;
- authorized descendant reopen propagates upward after close;
- acknowledgement stops contacts, not the task clock;
- a hard-window terminal state is the durable violation record;
- approved leadership sign-off is represented as a real owned task.

These remain explicit maintainer choices before activation:

1. timing classes and ladder intervals for workflows that do not already have a
   locked workflow-specific policy; inherit recorded Vaccination cadence unless
   it is explicitly superseded;
2. strict-window ownership outside normal shift hours;
3. representation of genuine physical work completed before a death/sale event
   arrived, while immutable evidence remains preserved;
4. whether any generic oversight of Weighing is allowed beyond outward events,
   because the specific free-flow guard is more restrictive than the generic
   target rule.
5. which, if any, current queue-like workflows are true claim pools instead of
   duty-owned work, and the accountable pool owner/SLA and atomic claim policy
   for each approved pool.

“Ambiguity is not approval” still applies. Record the choice and update the
affected canonical decision/guard in the same change; do not guess in code.

## Retirement gate

Retirements are last, never a prerequisite for the task kernel. Before deleting
any table, event, endpoint, enum value, or fallback, prove:

- live row count and trigger/function dependencies;
- all current and retained/replay producers and consumers;
- minimum supported Android/client version and drained legacy outbox work;
- outbox and Pub/Sub retention windows;
- registry, contract, generated-client, test, dashboard, and runbook cleanup;
- a forward data migration or an explicit no-data proof;
- post-cutover telemetry at zero use.

Known unsafe proposals that must **not** be executed from the earlier draft:

- `counts_shifting_readiness_evidence` is actively written and read;
- `identity_conflicts` is actively queried;
- `identity_correction_requests` needs live data and trigger audit;
- the legacy Feed completion contract needs client/outbox drain proof;
- `goat.shifted` remains a replay-compatibility alias;
- only L1 escalation generation may stop using `local-stub`; the enum/gateway is
  still used by Calendar, reminders, tests, and fallback delivery.

## Adjudication of the external v3 draft

The draft had the right high-level direction: fix the notification failure
first, establish roster ownership, avoid enum unification, build a thin generic
task tier, separate timing classes, and put retirements last. This plan keeps
that common ground.

The current-source review requires these corrections:

- add `verification_withdrawn` beside `obligation_missed`; a constant-only guard
  would miss the raw producer literal;
- `/app/tasks` already has actor scope and keyset pagination at 20; repair its
  horizon/index/count and add Room Paging instead of rebuilding it;
- use a canonical transaction-aware task table, not asynchronous per-module
  operational projections at the current scale envelope;
- introduce escalation run/step/contact identity before activation, not after a
  temporary obligation-only FCM model;
- separate work deadline from next contact time and pin a versioned policy;
- define tenant parent integrity, immutable ancestry, depth/tier rules,
  assignment history, counter locks, race fencing, and reconciliation;
- keep Weighing outside generic task dependencies;
- remove unsafe retirement claims and require live compatibility proof.

Those are implementation corrections, not a rejection of the target task
model. The shared conclusion is: fix the hard correctness gates first, then
build the task kernel in staged, shadowed, reversible vertical slices.

## Fresh-main adjudication of the 68 proposed improvements

The later 68-item review was counter-checked on fresh `origin/main` at
`4ec92c99e75fe83d27b50b4fcabad73be6cc1d6f`, with the final disputed claims
rechecked at `07640ad5388ffb6ed87701ea7f4f522c92fb3e82`: **48 accepted, 15
partially accepted/narrowed, and 5 rejected**. Accepted duplicates were merged
into one owning instruction rather than counted as separate implementation
work.

The important accepted common ground now encoded in this plan is:

- apply one transactional current-state/source-version actionability fence to
  recovery and new missed events, persist every branch outcome, fail closed
  again immediately before external delivery, and repair the new `KERN-012`
  role constraint;
- explicitly cut over and retire the live legacy escalation lane, and reuse the
  existing dispatcher with fail-closed pre-send run/step fencing;
- make roster authority, resolver purity, member/device separation, identity
  mapping, ownership re-resolution, and exception capacity concrete;
- define source parts, database tier enforcement, set-based/idempotent counter
  deltas, verifier dedup, source tombstones, and hot-table coverage;
- preserve `/app/tasks` compatibility, total semantics, global mixed-source
  pagination, legacy IDs, defer mapping, Room dependencies, seeds, backfills,
  flags, rollback, and retention budgets;
- widen logout fencing to every sync engine, finish the existing scope-decision
  migration, close the SOP cross-park hole, and guard the investor quarantine.

The main rejected or narrowed points were also material:

- `make sqlc-check` already regenerates and diffs all six configured mirrors;
- claim pools are a product decision, not permission to introduce ownerless
  canonical tasks; named duty ownership remains the default;
- the existing verifier-cache instruction already allows the two valid fixes;
- verifier enqueue and the task-node transaction port are related transaction
  capabilities, not the same authoritative seam;
- Health verification categories are currently reserved/non-producing, so they
  get a future-producer gate rather than a current defect ID;
- the People roster/timetable position-title fallback is a narrowed
  display-integrity repair before owner cutover, not a new stable defect ID;
- K0 already serializes shared kernel primitives and already gates `MOB-001`;
  those claims were not duplicated.

Two genuinely new stable findings were added to the companion ledger:
`KERN-012` P1 for the emitted-role/constraint mismatch and `SHIFT-004` P2 for
the unwired duplicate Shifting verification adapter. This adjudication updates
instructions only; it does not claim that any product defect is fixed.
