# Operational Task Kernel Remediation Plan

Status: implementation plan; policy decisions listed below remain explicit
gates.

Source review snapshot: `97e2b462cc4cc39e78b3c99209a09f739c3d2e31`

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

The build is not “unify all enums.” Goat OS currently has 28 human-work,
container, and proof tables with 31 independently constrained state columns.
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
  one task per source;
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
   in the write service and database-compatible tier rules.
3. A child insert must lock and reject a parent that is closing or closed unless
   the same command is an authorized reopen.
4. Source identity is immutable. Rescope creates a declared move/supersession
   operation with old/new ancestry recomputation; it is not an arbitrary parent
   edit. Lock old and new parents in deterministic ID order.

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

Define before implementation whether deferred counts as open, which terminal
outcomes close a parent, when reopen is legal, how clocks behave in each state,
and how close versus new-child races are fenced.

### Sign-off

Verification or leadership approval is a separate task node with its own owner,
clock, proof reference, and state. Operator and verifier leaves are siblings
under a work-unit container, so the operator leaf can become Done immediately
while the container waits for sign-off. Sign-off is not a Boolean on the
operator leaf and not an implicit verification status hidden from My Tasks.

### Effective duty and assignment

Make `workforce_roster_assignments` production-owned: authoring or seed plus a
recurring-pattern materializer, a tenant-safe link to the canonical position,
and indexes for scope/time lookup.

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
snapshot. Acknowledgement stops future contacts by clearing `next_contact_at`
and atomically canceling/fencing every future or open step. It does not alter
the work deadline or erase the violation. A contact worker must recheck run and
step state immediately before the external send so an already leased attempt
cannot contact after acknowledgement. Completion or cancellation resolves the
run. Before every advance, re-read canonical task state and use
compare-and-set/row version to settle acknowledgement, completion, and worker
races.

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
- Correct selected module source defects before creating its adapter.
- Record the policy decisions below in canonical docs.

Exit: notification types write successfully; release/migration proofs are
trustworthy; no selected source command silently loses required state/outbox;
authorization is capability-bound.

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
- Add one source adapter behind a feature flag, initially shadow-only.

Exit: every shadow task has a real owner and policy-pinned clock or a loud
materialization exception; replay creates no duplicate; source and task changes
commit atomically.

### K2 — personal Today and generic FCM escalation

- Extend existing `/app/tasks`; do not rebuild its pagination. Add the India
  business-day/active-state horizon and matching index; remove the full
  `count(*)` from the keyset response. The Today predicate is owner mapped to
  the actor, non-done/non-terminal/non-canceled, `due_at` before the next
  Asia/Kolkata midnight, and not deferred beyond `now`.
- During shadow, keep the current SOP result authoritative and expose comparison
  only to operator/admin diagnostics. Cut over one module allowlist at a time:
  return `task_nodes` for that module and legacy SOP rows for uncut modules,
  suppress duplicates by stable source identity, and normalize both to one API
  ID and `(due_at, task_id)` cursor.
- Add an Android Room/Paging consumer and offline/outbox overlay.
- Implement the full escalation run/step/contact schema with named-person FCM
  as the first channel.
- Add acknowledgement, completion/cancel resolution, delivery failure, leasing,
  idempotency, and reconciliation.

Exit: owner-correct Today works before app open after the normal materialization
cadence; shadow/cutover pagination contains no duplicate or skipped task; ack
fences every future contact but not the work clock; a reopened run has a new
identity; no role slug is sent to FCM.

### K3 — hierarchy and sign-off

- Activate the K1 parent hierarchy and tier rules.
- Implement immediate-parent counters, close/reopen propagation, fencing, and
  drift reconciliation.
- Model verifier/CEO sign-off as separate owned task nodes.
- Persist the first stable Vaccination drive/campaign epic identity and map its
  batch/story and individual operational children without creating an SOP task
  per goat.

Exit: race tests prove close versus child creation, reopen after close,
idempotent replay, rescope, terminal/defer behavior, and sign-off ownership.
No sibling scan appears in a child hot path.

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

1. timing classes and ladder intervals per workflow;
2. strict-window ownership outside normal shift hours;
3. representation of genuine physical work completed before a death/sale event
   arrived, while immutable evidence remains preserved;
4. whether any generic oversight of Weighing is allowed beyond outward events,
   because the specific free-flow guard is more restrictive than the generic
   target rule.

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
