# Goat OS Operational Kernel Golden Rule

Date: 2026-06-27

Purpose: define the permanent architecture lens for every Goat OS feature.

## Golden Rule

Maintainer non-deviation decision, 2026-08-10: Goat OS is one event-driven,
interlinked task/ticketing waterfall. This is a permanent product boundary, not
an optional pattern. A module may own domain facts and its execution state
machine, but it may not own a separate app-visible task authority, owner
fallback, due/overdue calculation, scheduler, reminder/escalation ladder,
verification queue, or follow-up screen outside the shared operational kernel.

Every feature must be designed as part of the same operational kernel:

```text
business event
  -> canonical transaction
  -> audit + outbox
  -> stable owned task node + policy-pinned clock
  -> bounded parent/child hierarchy
  -> trigger / scheduler / reminder
  -> acknowledgement-gated contact waterfall
  -> proof
  -> separately owned verification/sign-off task
  -> close/reopen rollup
  -> process read models
  -> leadership answer: is the process followed, where broken, who owns next?
```

The normal integration shape writes domain state, task coordination, audit,
idempotency, and outbox through one transaction-aware port. A recorded strict
module boundary changes direction, not participation: that module atomically
writes domain state, audit, idempotency, and a complete outbox event; a
shared-kernel consumer outside the module materializes task coordination in a
receipt-backed, idempotent, version-fenced transaction with lag visibility,
bounded replay, and source reconciliation. Weighing uses this outward-only
shape so raw scan-and-submit remains free-flow and never reads generic task,
SOP, obligation, roster, herd, or lifecycle state. Generic task state never
gates Weighing execution, but Weighing's app-visible owner, clock, hierarchy,
contact, proof, sign-off, and rollup still belong to the shared kernel.

Operational work that cannot satisfy one of those two shapes remains shadowed
or blocked. A screen projection, notification side effect, or best-effort event
consumer is not task truth. Any proposed exception requires an explicit
maintainer decision and same-change updates to this document, the execution
plan, the prevention contract, the relevant structural guard, and adversarial
tests. See `context/execution/operational-task-kernel-remediation-plan.md` and
`context/execution/defect-prevention-execution-contract.md`.

Goat OS is not a set of isolated screens. The kernel exists to answer the CEO and
operator question for every domain:

```text
What process was expected?
Was it followed?
If not, where did it break?
Who owns the next action?
What is due by when?
What evidence proves the answer?
What alert/escalation fired when the deadline was crossed?
```

This rule applies to Preventive Care (PC) vaccination now and to future feed, breeding,
procurement, parks, HR/people, farmer network, sales/commerce, and finance
modules later.

Critical guardrails are part of this kernel, not side workflows. Any high-risk
state transition must go through a generic guardrail engine: source-backed
reason, validation, proof, authority, obligations, deadline/escalation
waterfall, audit, and read-model visibility. Animal-operation policy packs for
quarantine entry/exit, ICU entry/exit, contagious-risk isolation, death
reporting, birth, procurement arrival, sale/allocation blockers, and high-risk
movement are tracked in `docs/features/critical-animal-action-guardrails.md`.
When a kernel pack replaces a legacy workflow, it must declare and test the
legacy capability parity floor first, then close known legacy gaps with stronger
typed policy, source-backed evidence, obligations, proof, escalation, and
scalable projections. Parity never means preserving weak validation as the
target behavior.

For the concrete backend, frontend, infra, DLQ, SLA waterfall, and vertical
plug-in system design, read
`context/architecture/operational-kernel-system-design.md`.

For the retry, Pub/Sub/outbox, bounded-worker, Docker-validation, and
future one-million-operation proof plan, read
`docs/protocol-engine/high-scale-kernel-validation-plan.md` before changing
generation, sweepers, projections, notification delivery, or protocol publish
fanout. That plan is one-million-scale future/regression material; the active
release target and worker topology are set by the ADR
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`.

## Current Release Scale Target

The active release target is the **5,000-to-50,000-animal envelope**, per the
accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. That
ADR is the authority for operational-kernel deployment scale and worker
topology, and it narrows the one-million-animal framing elsewhere in this
document to future scale work rather than a present release invariant.

For this envelope the kernel runs as:

- **one modular kernel worker** process next to the API and Postgres, not a
  fleet of independently scheduled Cloud Run Jobs;
- **0 Cloud Scheduler cron jobs and 0 scheduled Cloud Run Jobs** in the normal
  topology — one continuous event-consumer loop plus four logical cadence
  classes (fast delivery, operational, hourly obligation generation, daily
  housekeeping) live inside the single worker;
- **0 operational-kernel projection tables** in the active runtime — Calendar,
  process-integrity, and the vaccination screens read canonical obligation/
  batch/SOP/proof tables through bounded, indexed SQL; and
- canonical Postgres as the only source of operational truth, with idempotent,
  replay-safe writes and the transactional outbox unchanged.

The one-million-animal topology, its projection fleet, and monthly partitioning
remain valid future-scale research and regression material. They are
reintroduced per measured hotspot along the scale-out ladder in the ADR, not as
day-one requirements. Serving five screens from canonical tables at this
envelope also deletes the projection-drift bug class (stale watermarks,
dual-writer races, rebuild-availability failures); the ADR reconciles this with
the `make scale-guard` CI gate through scoped, plan-tested annotations rather
than disabling the guard.

## Kernel Responsibilities

The kernel owns these reusable capabilities. Feature modules plug into them; they
must not rebuild local versions.

| Capability | Kernel rule |
| --- | --- |
| Event intake | API, mobile, import, device, webhook, and scheduled events write canonical Postgres state, audit, idempotency, and outbox in one transaction. |
| Trigger evaluation | Rules/protocols decide what business events create obligations, reminders, reviews, or suppressions. Triggers are deterministic and replay-safe. |
| Obligations and batches | Far-future due work lives in Postgres. Group work uses batches/work units. Do not create one SOP task per goat for group drives. |
| Sweepers and schedulers | Workers scan bounded indexed windows, create due work, reminders, deadline alerts, and escalations. Frontend timers are never business schedulers. |
| Reminder and notification | Reminder/nudge/escalation requests are durable rows/events behind `NotificationGateway` style ports. Slack, FCM, email, webhook, Opsgenie/PagerDuty-style adapters are replaceable. |
| Deadline crossing | Missed SLA/deadline state creates visible process exceptions, not just logs. Control Tower, Action Center, Calendar, Protocol Adherence, and Workflow must be able to show the break. |
| Proof and verification | SOP execution, media proof, verifier decisions, rework, and completion are shared platform engines. |
| Read models / CQRS | Command writes stay canonical. Read models/projections feed dashboards, Calendar, Action Center, Protocol Adherence, Workflow, analytics, and AI context. At the 5k-to-50k envelope these operator reads come from canonical tables via indexed SQL, and a screen-specific projection is added only per measured hotspot (see Current Release Scale Target). |
| Audit and history | Every important transition has actor, scope, source, idempotency key, before/after or payload, and trace identifiers. |
| Observability | API, worker, queue, DB, projection, notification, and deadline-lag metrics exist before claiming production readiness. |

## Audit And Logging Boundary

When people say "audit" or "logging" in Goat OS, keep these layers separate.
Both are part of the kernel, but they serve different jobs.

| Layer | Product meaning | Technical meaning | Storage / surface |
| --- | --- | --- | --- |
| Business/product audit | Who changed a business fact, why, from what to what, under which authority/evidence. This answers operator, CEO, compliance, and process-review questions. | Durable domain transition record written in the same transaction as canonical state where possible. | Postgres `audit_log`, domain status/event ledgers, proof/verification history, visible through `/operations/audit` and entity histories. |
| Technical logging | What failed or behaved unusually in the software while serving a request, worker, relay, scheduler, adapter, or integration. This answers engineering diagnosis questions. | Structured logs, metrics, traces, panic recovery logs, DLQ/error counters, slow-query/queue-lag signals. | `backend/internal/platform/observability`, Cloud Logging/Monitoring/Error Reporting/Trace, local stdout JSON. |
| Analytics facts | Aggregated operational and business metrics derived from canonical events/projections. This answers trend and BI questions. | Read-side transforms and governed metric definitions. | Projection tables, BigQuery/Tinybird/Cube/Metabase behind analytics boundaries. |

Rules:

- Do not use technical logs as business audit truth.
- Do not flood business audit with every page view, polling request, or frontend
  hover/click. Business audit is for meaningful state transitions, decisions,
  proof, exceptions, assignments, deadline/snooze/nudge actions, and policy
  changes.
- Read-only views may produce technical access logs and metrics; they become
  business audit rows only when product policy explicitly requires access
  review.
- Every mutating product action must decide whether it writes a business audit
  row, a domain status/event ledger row, an outbox event, or all of them. That
  decision belongs in the feature handoff/PRD/TRD.
- Technical logs must include trace/request/tenant and useful business
  identifiers, but secrets are always redacted.
- Operations Audit is a business product surface over durable audit rows. It is
  not a raw engineering log viewer.

## Google-Centered Infrastructure Equivalents

Use managed Google primitives by default, behind ports/adapters so they can be
replaced later if needed.

| Common pattern | Goat OS / Google shape |
| --- | --- |
| Kafka / SQS event bus | Transactional Postgres outbox -> outbox relay -> Pub/Sub topic/subscription with DLQ. |
| Delayed queue / near-term timers | Cloud Tasks for near-term retries/reminders, backed by durable Postgres state. |
| Cron / scheduled sweeper | Managed-primitive menu behind ports/adapters. At the active 5k-to-50k envelope the sweeper cadences run as logical stages inside the **single kernel worker** (0 Cloud Scheduler crons, 0 scheduled Cloud Run Jobs) per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`; Cloud Scheduler -> Cloud Run Job is the future per-hotspot extraction shape, not the day-one topology. Local Docker runs the same binary manually or through dev scripts. |
| CQRS read side | Postgres canonical tables + generated API clients, with projection/read-model tables added per measured hotspot. At the active 5k-to-50k envelope the operator screens read canonical tables directly through indexed SQL and carry **0 operational-kernel projection tables**; a screen-specific projection/read-model table is added only per measured hot read along the scale-out ladder in `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, not as the day-one read-side shape. |
| Time-series analytics | Partition-ready Postgres operational history for product truth; BigQuery/Tinybird/Cube only through analytics boundaries. At the active 5k-to-50k envelope the event/audit/history/time-series-like tables run as **ordinary indexed tables** (not partitioned), with monthly Postgres partitioning reintroduced only for the first measured history/event hotspot per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. |
| Redis cache / locks | Memorystore/Redis only for cache, rate limit, short leases, or acceleration. It is never canonical truth. |
| Incident alerting | Cloud Monitoring/Error Reporting plus notification adapters; Opsgenie/PagerDuty-style webhooks behind an alert gateway. |
| Object/media storage | GCS signed upload/download through storage ports. API does not proxy video bytes. |
| Search/indexing | Postgres indexed reads first; add dedicated search only behind an adapter and never as source of truth. |

Cloud Tasks, Pub/Sub, Redis, BigQuery, and alerting tools are transport,
execution, or reporting layers. Postgres canonical tables plus audit/outbox are
the source of operational truth.

## Scale Non-Negotiables

The kernel's design must stay *safe to scale* toward more than one million goat
operations, but the active release target is the 5k-to-50k envelope in
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. At that envelope
the properties below are met with canonical indexed SQL and one worker;
one-million-scale mechanisms (a projection fleet, monthly partitioning, extra
services/queues) are added per measured hotspot along the ADR's scale-out
ladder, not switched on by default. These properties are cheap correctness
invariants and remain non-negotiable at every scale:

- Tenant, park, shed, cohort, date, status, and owner scope are first-class query
  dimensions.
- Hot API reads are paginated/keyseted and index-backed. No full-herd scans.
- Workers use bounded batch sizes, retry cursors, leases or idempotent claims,
  bounded goroutines, and DLQ/error visibility.
- High-volume event, audit, history, and time-series-like tables are
  partition-*ready*: at the 5k-to-50k envelope they run as ordinary indexed
  tables, and partitioning is reintroduced for the first measured
  history/event hotspot (see the ADR).
- Far-future due state lives in Postgres, not in Cloud Tasks.
- Near-term reminders/retries can use Cloud Tasks but must be reconstructable
  from Postgres if messages are lost.
- Every write, worker, importer, webhook, notification send, and projection
  refresh is idempotent and replay-safe.
- Query-plan validation is required for hot paths and widest allowed list
  requests.
- Load proof uses realistic skew, not uniform fantasy data.

Projections are compute-on-write when they exist, never compute-on-read — but at
the 5k-to-50k envelope the operator screens read canonical tables directly and
carry **no** operational-kernel projection tables. A screen-specific projection
is added only when a measured hot read cannot meet its latency/DB-pressure
target after query and index repair, per the scale-out ladder in the ADR.
Projections are a per-hotspot response to measurement, not a day-one invariant.
Summary aggregates (Control Tower gaps, adherence rollups, process-integrity
counts) are the first projection candidates because, unlike keyset list reads,
their cost grows with open-obligation count; query-plan validation must cover
them at the upper-bound row count, not only the 5k list case.

## Worker Topology and Durability Rules

Every sweeper, scheduler, reminder, obligation-generator, projector, and
notification dispatcher must satisfy these non-negotiable rules:

1. **Every worker must be bounded, resumable, and forward-progressing.**
   - Bounded: processes a finite window or page of work per invocation (not
     infinite until shutdown). Use `limit N` rows, `ORDER BY cursor DESC LIMIT
     N`, or a time window.
   - Resumable: tracks its progress durably (a `last_cursor` or
     `last_processed_key` in the result row or in a per-worker state table).
     The next invocation starts from `WHERE cursor > last_cursor`, never from
     `WHERE cursor >= MIN(cursor)` or from zero.
   - Forward-progressing: the cursor always increases or at minimum never goes
     backwards. A reset of the cursor or a re-generated sorted set that omits
     already-processed rows is a defect. Idempotency guards against re-processing
     the same row twice, but monotonic cursors prevent silent skips.

2. **Required recorders fail closed.** Audit writes, outbox events, domain events,
   and durable notification rows that are required for a feature to function must
   be written in the same transaction as the canonical state change, not as a
   best-effort side effect. If a mandatory recorder (e.g., the outbox or
   notification_requests table) cannot write, the operation fails rather than
   silently proceeding without that record. A `ServiceError` that includes the
   recorder error (not a swallowed log) must surface to the caller. Consequence:
   no silent loss of audit, no unrecorded state changes, no notifications that
   were meant to fire but got dropped during a DB outage.

3. **Manual-review queues must be durable, paginated, visible, and resolvable.**
   When a feature creates work that requires human review or intervention — a
   deferred obligation, a rework task, a config-approval step, or an audit
   follow-up — the queue must:
   - Live as durable rows in Postgres (never as in-memory lists or frontend state).
   - Be visible through a paginated operator screen (e.g., Action Center or a
     dedicated "Review Pending" list), keyset-paginated, bounded to ~20 rows per
     page.
   - Include explicit status (pending, in_review, approved, rejected, closed),
     owner (who is responsible for review), and due/SLA date (when it is
     overdue).
   - Be resolvable: an operator can mark an item approved/rejected with a reason,
     and that decision is persisted and visible in audit/history. A "cleared from
     the queue" state is not an escape hatch; it is a resolved state recorded with
     evidence.
   - If a queue item is associated with a user-assigned task (e.g., a Preventive
     Care director's due review), cancelling the parent trigger (e.g., cancelling
     a vaccine hold) must explicitly cancel or mark the queue item as no longer
     applicable, never silently leave it orphaned.

## Feature Design Checklist

Operational, mutating, scheduled, or process-integrity features must answer
these questions before implementation. Read-only/foundation/reference features
must still walk the checklist, but may mark a step `N/A` with an explicit reason
such as "read-only lookup, no state transition" or "foundation route, no deadline
or human action."

1. What canonical business event starts the process?
2. What transaction writes canonical rows, audit, idempotency, and outbox?
3. What trigger rule evaluates the event?
4. What obligation, task, review, batch, or process exception is created?
5. What deadlines, reminders, nudges, and escalations exist?
6. What happens when a deadline is crossed?
7. What proof/evidence is required?
8. Who verifies or accepts the proof?
9. What read models answer "process followed / broken / owner / next action"?
10. What API bounds, indexes, partitions, and query-plan tests prove scale?
11. What metrics, logs, traces, DLQ, and alert thresholds prove operations?
12. What local Docker path runs the same binaries/adapters as cloud?
13. What seed/E2E data proves all major states, actions, and negative cases?

If an operational/process feature cannot answer these, it is not ready to build.
If a read-only/foundation feature marks a step `N/A`, the reason must be written
in the PRD/TRD/handoff so agents do not invent fake obligations, proof,
notifications, or escalations just to satisfy the checklist.

## Pre-E2E Kernel Review

Before any feature is declared E2E-ready, review the actual code, schemas,
workers, contracts, tests, and UI against the kernel. Classify each item as
`present`, `implemented in this slice`, `intentionally deferred`, `N/A with
reason`, or `blocker`.

A feature is blocked before E2E when any of these are true:

- mutating product actions lack idempotency, business audit/history, permission
  checks, or outbox/domain events where downstream consumers need them.
- deadline, reminder, nudge, snooze, notification, or escalation behavior exists
  only as technical logs, frontend state, or mock fixtures.
- technical logs are being used as business audit truth, or business audit is
  flooded with routine read-only polling/hover/click noise.
- Calendar, Action Center, Protocol Adherence, Workflow, or Control Tower reads
  a different truth source for the same process state.
- hot APIs or workers are unbounded, unindexed, or scan more than the requested
  tenant/park/shed/cohort/date/status slice.
- frontend owns canonical due/reminder/escalation/proof state instead of calling
  generated backend contracts.

## Backend Rules

- Use ports/adapters and composition. Do not build inheritance-heavy feature
  trees that make new domains require broad refactors.
- Domain/app code depends on interfaces, not Google SDKs, Redis clients, Slack
  clients, FCM clients, or alerting vendors.
- Handlers stay thin. App services own behavior, transactions, state
  transitions, idempotency, and outbox/audit writes.
- Consumers, sweepers, notification senders, projection refreshers, and repair
  jobs are regular kernel clients with idempotency and observability.
- A feature-specific table can exist, but the trigger, obligation, task, proof,
  notification, audit, and read-model patterns stay shared.

## Frontend Rules

- Frontend is not the scheduler, source of truth, queue, or process engine.
- React state may own filters, selected rows, drawer open/close, hover, loading,
  optimistic affordances, and retry UI. It must not own canonical due state.
- Use generated clients and bounded list/detail APIs.
- Show process state honestly: loading, empty, disabled, unauthorized, failed,
  blocked, overdue, missing proof, missing owner, and escalation states must be
  distinguishable.
- Buttons such as nudge, snooze, verify, rework, accept, assign, or escalate
  call backend actions or render honest disabled states.
- Every command-lens UI must help answer: process followed, where broken, who
  owns next, what evidence exists.

## Local And Cloud Parity

Local development uses `compose.local-kernel.yml` to keep the same command and
contract boundaries. The parity stack is a behavior proof, not a claim that a
Google emulator certifies Google IAM, quotas, regional behavior, or managed
service operations.

The `local`/`cloud` columns below record the **pre-cutover split-worker
topology** — independently scheduled Cloud Run Jobs, Cloud Scheduler crons, and
standalone projector binaries. That topology is being retired per
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`. In the active
5k-to-50k target the same command and contract boundaries are exercised by **one
modular kernel worker** with **0 scheduled Cloud Run Jobs, 0 Cloud Scheduler
cron jobs, and 0 projector binaries / projection tables**: the continuous
event-consumer loop plus the four cadence classes (fast delivery, operational,
hourly obligation generation, daily housekeeping) run inside that worker, and
operator screens read canonical tables through indexed SQL instead of
projections. The columns are kept as the recoverable pre-cutover inventory, not
the day-one runtime.

With that framing the parity stack still proves the same command/contract
boundaries:

```text
local (pre-cutover parity; the 5k-to-50k target folds these stages into one kernel worker):
  Docker Postgres 16
  official Google Pub/Sub emulator + source topic/subscription/DLQ bootstrap
  API with local filesystem proof-storage adapter
  outbox-relay configured to Pub/Sub (never eventbus/logging in parity mode)
  domain-event-consumer using the production Google Pub/Sub client
  the same generator/sweeper/projector/notification binaries as Cloud Run Jobs (projector = pre-cutover only; retired per the ADR above, no projector in the 5k-to-50k target)
  durable Postgres notification rows + periodic dry-run dispatcher
  local parity smoke and duplicate-delivery proof

cloud (pre-cutover split-worker topology, retired per the ADR above):
  Cloud SQL
  Cloud Run API/services
  Cloud Run Jobs            (retired: replaced by one long-running kernel worker)
  Cloud Scheduler           (retired: 0 cron jobs in the 5k-to-50k target)
  Pub/Sub + DLQ
  Cloud Tasks
  GCS
  Cloud Monitoring/Error Reporting
  notification/alert adapters
```

Logging an outbox row is not delivery. The canonical local parity smoke must
exercise Postgres -> outbox relay -> official Pub/Sub emulator -> durable domain
consumer -> domain effect, then replay the event and prove idempotent duplicate
handling. It also runs the real generator/sweeper/notification stages and
checks their materialized canonical effects. The standalone projector binary in
that smoke belonged to the pre-cutover topology retired per
`docs/decisions/operational-kernel-5k-50k-scale-envelope.md`; at the 5k-to-50k
envelope those effects are canonical rows read directly, not projection rows.

Cloud Tasks has no supported local API emulator. Goat OS therefore does not
fake Cloud Tasks semantics. Local runs prove the durable Postgres notification
intent plus the same dispatcher binary on a periodic loop; staging separately
proves Cloud Tasks queue/IAM/OIDC/dispatch/retry behavior. Local proof storage
uses the filesystem adapter through the same storage port; staging separately
proves GCS signed URLs and IAM. Pub/Sub emulator proof does not prove IAM.

`GOATOS_OBS_SINK=otlp|gcm` currently falls back to structured stdout, so an OTel
collector is not part of the parity stack until the backend has a real OTLP
exporter. Redis is intentionally absent because it is optional acceleration,
not kernel truth. See `docs/runbooks/local-gcp-kernel-parity.md`.

## Current Vaccination Application

Preventive Care (PC) vaccination is the first visible proof of the kernel:

```text
goat created / accepted intake / source-backed protocol publish
  -> vaccination trigger generation
  -> obligation_instances / obligation_batches
  -> sweeper creates drive/SOP/proof work
  -> proof and verification
  -> completion / booster / rework
  -> Calendar, Action Center, Protocol Adherence, Workflow, Control Tower
```

The same shape must hold for every future module. Only the domain rule adapters
change.
