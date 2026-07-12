# Goat OS Operational Kernel Golden Rule

Date: 2026-06-27

Purpose: define the permanent architecture lens for every Goat OS feature.

## Golden Rule

Every feature must be designed as part of the same operational kernel:

```text
business event
  -> canonical transaction
  -> audit + outbox
  -> trigger evaluation
  -> obligation / work item / batch
  -> sweeper / scheduler / reminder
  -> notification / escalation
  -> proof / verification / completion
  -> process read models
  -> leadership answer: is the process followed, where broken, who owns next?
```

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

For the scale, retry, Pub/Sub/outbox, bounded-worker, Docker-validation, and
1M-operation proof plan, read
`docs/protocol-engine/high-scale-kernel-validation-plan.md` before changing
generation, sweepers, projections, notification delivery, or protocol publish
fanout.

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
| Read models / CQRS | Command writes stay canonical. Read models/projections feed dashboards, Calendar, Action Center, Protocol Adherence, Workflow, analytics, and AI context. |
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
| Cron / scheduled sweeper | Cloud Scheduler -> Cloud Run Job/worker; local Docker runs the same binary manually or through dev scripts. |
| CQRS read side | Postgres canonical tables + projection/read-model tables + generated API clients. |
| Time-series analytics | Partitioned Postgres operational history for product truth; BigQuery/Tinybird/Cube only through analytics boundaries. |
| Redis cache / locks | Memorystore/Redis only for cache, rate limit, short leases, or acceleration. It is never canonical truth. |
| Incident alerting | Cloud Monitoring/Error Reporting plus notification adapters; Opsgenie/PagerDuty-style webhooks behind an alert gateway. |
| Object/media storage | GCS signed upload/download through storage ports. API does not proxy video bytes. |
| Search/indexing | Postgres indexed reads first; add dedicated search only behind an adapter and never as source of truth. |

Cloud Tasks, Pub/Sub, Redis, BigQuery, and alerting tools are transport,
execution, or reporting layers. Postgres canonical tables plus audit/outbox are
the source of operational truth.

## Scale Non-Negotiables

The kernel must be safe for more than one million goat operations.

- Tenant, park, shed, cohort, date, status, and owner scope are first-class query
  dimensions.
- Hot API reads are paginated/keyseted and index-backed. No full-herd scans.
- Workers use bounded batch sizes, retry cursors, leases or idempotent claims,
  bounded goroutines, and DLQ/error visibility.
- High-volume event, audit, history, and time-series-like tables are
  partition-aware.
- Far-future due state lives in Postgres, not in Cloud Tasks.
- Near-term reminders/retries can use Cloud Tasks but must be reconstructable
  from Postgres if messages are lost.
- Every write, worker, importer, webhook, notification send, and projection
  refresh is idempotent and replay-safe.
- Query-plan validation is required for hot paths and widest allowed list
  requests.
- Load proof uses realistic skew, not uniform fantasy data.

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
service operations:

```text
local:
  Docker Postgres 16
  official Google Pub/Sub emulator + source topic/subscription/DLQ bootstrap
  API with local filesystem proof-storage adapter
  outbox-relay configured to Pub/Sub (never eventbus/logging in parity mode)
  domain-event-consumer using the production Google Pub/Sub client
  the same generator/sweeper/projector/notification binaries as Cloud Run Jobs
  durable Postgres notification rows + periodic dry-run dispatcher
  local parity smoke and duplicate-delivery proof

cloud:
  Cloud SQL
  Cloud Run API/services
  Cloud Run Jobs
  Cloud Scheduler
  Pub/Sub + DLQ
  Cloud Tasks
  GCS
  Cloud Monitoring/Error Reporting
  notification/alert adapters
```

Logging an outbox row is not delivery. The canonical local parity smoke must
exercise Postgres -> outbox relay -> official Pub/Sub emulator -> durable domain
consumer -> domain effect, then replay the event and prove idempotent duplicate
handling. It also runs the real sweeper/projector/notification binaries and
checks their materialized effects.

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
