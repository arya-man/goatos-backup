# Kernel Audit, Reconciliation, And Slack Reporting Plan

Status: proposed architecture and rollout plan
Date: 2026-07-11
Slack target: `#goatos-audit` (`C0BGSJDD420`) in workspace `T091RHJF43E`
Slack URL: https://app.slack.com/client/T091RHJF43E/C0BGSJDD420

## 0. Existing Machinery And Orchestration

This is not a greenfield audit fleet. Goat OS already has command-line workers,
sweepers, reconcilers, seeders, imports, and validation tools under
`backend/cmd`. `backend/cmd/kernel-audit` should become the orchestrator,
normalizer, run ledger, finding ledger, repair requester, and Slack reporter. It
must not duplicate existing business logic or quietly replace owning workers.

| Existing command(s) | Current role | Audit decision |
| --- | --- | --- |
| `outbox-relay`, `outbox-dlq`, `domain-event-consumer`, `domain-event-processed-sweeper`, `idempotency-key-sweeper` | Event spine, durable delivery, dead-letter, processed-event, and idempotency hygiene. | Orchestrate and wrap. Kernel audit checks liveness, lag, parity, and repair outcomes, then asks these owners to retry/replay/reclaim through their existing paths. |
| `obligation-sweeper`, `sweeper`, `calendar-reminder-sweeper`, `calendar-escalation-sweeper`, `calendar-vaccination-projector`, `notification-dispatcher`, `sop-review-fanout-retry`, `bulk-status-worker` | Time spine, due/missed state, reminders, escalations, calendar projection, notifications, SOP review fanout, and status fanout. | Orchestrate and wrap. Kernel audit detects missing or stale artifacts, records findings, and invokes the owning sweeper/dispatcher/worker contract. |
| `vaccination-eligibility-rollup-recompute`, `inventory-batch-reconciler` | Derived vaccination eligibility and inventory batch reconciliation. | Orchestrate for current vaccination scope; verify aggregate parity after repair. |
| `counts-mismatch-scan`, `counts-source-parity-check`, `counts-alias-coverage-check`, `counts-projection-recompute`, `counts-query-plan-check`, `counts-source-import`, `counts-workbook-mapping-check`, `counts-workbook-source-scan`, `location-profile-coverage-check`, `location-profile-source-import` | Counts, source, workbook, alias, location, and query-plan validation/recompute tools. | Run alongside and absorb only through a pack contract later. These remain source-quality and projection owners; kernel audit consumes their results and reports cross-kernel impact. |
| `generate-vaccination-obligations`, `backfill-goat-created`, `seed-vaccination-trigger` | Replay/backfill/generation inputs for vaccination and goat-created flows. | Callable repair inputs only when wrapped by the owning vaccination/protocol service with idempotency. They are not private audit logic. |
| `partition-maintainer` | Partition lifecycle and hot-table hygiene. | Orchestrate liveness and overdue maintenance checks; keep physical maintenance owned by this command. |
| `api`, `legacy-god-sheet-sync` | Runtime API and legacy bridge. | Run alongside. Audit may use them as evidence/source comparison, but must not make legacy sync a canonical repair path. |
| `migrate`, `mint-dev-token`, `seed-calendar-vaccination-dev`, `seed-dev-email-grants`, `seed-dev-grant`, `seed-position-duties`, `seed-roster-real`, `seed-shed-positions`, `seed-vaccination-real` | Migration, local/dev auth, and one-time seed/import tools. | Not audit runners. They may define source fixtures, permissions, and initial data that audit packs reference. |

The implementation contract is:

```text
kernel-audit detects, records, requests, verifies, reports.
Owning modules repair through their ports/services/commands.
No cross-module SQL fixer.
```

Adoption note: this remains a proposed plan until linked from the canonical
architecture/context index and agent skill references. Once adopted, Phase B
implementation must conform to this orchestration, repair-boundary, liveness,
and notification contract.

### 0.1 Runtime Feature Flag

Kernel audit is opt-in runtime behavior. The entire audit system must sit behind
an explicit global flag, default `false` in every environment:

```text
GOATOS_KERNEL_AUDIT_ENABLED=false
```

Implementation can back this with environment config, app config, or a feature
flag service, but the runtime contract is the same:

- if the global flag is off, `backend/cmd/kernel-audit` exits as a no-op;
- no scheduled audit sweeps are enqueued or executed;
- no watchdog/dead-man alerts fire for missing audit runs;
- no Slack audit summaries or P0/P1 audit alerts are sent;
- no `kernel_audit_runs`, findings, repairs, repair proofs, cap claims, or
  suppressions are written by audit runtime;
- no self-healing, projection rebuild request, notification request, or owning
  repair-port call is attempted;
- sub-flags such as report-only, auto-repair, tenant allowlists, or Slack
  delivery cannot override the global off switch.

Schema migrations, static CI/`make ai-doctor` manifest checks, and docs can
exist while the flag is off. Runtime execution must remain disabled until an
operator deliberately enables the flag. If a per-tenant or per-module allowlist
is added later, both the global flag and the narrower allowlist must be enabled;
global off always wins.

## 1. Purpose

Goat OS is an event-driven operational kernel. A business fact such as birth,
accepted procurement intake, shift, death, pregnancy state, vaccination
completion, future feed direction, proof rejection, or worker absence should
create all expected downstream kernel artifacts:

```text
canonical fact
  -> audit/history
  -> outbox/domain event
  -> trigger evaluation
  -> obligation/work/batch
  -> reminder/escalation/notification intent
  -> proof/verification/completion state
  -> projection/read model
  -> Control Tower / Action Center / Calendar / Protocol Adherence answer
```

The audit system is the safety net for this chain. It must answer:

```text
Given canonical Postgres truth and published business rules, what should exist?
What is missing, stale, duplicated, or illegal?
Can Goat OS repair it deterministically?
If not, who owns the process exception?
What should leadership see in Slack and command lenses today?
```

This is not a replacement for event-driven code. Events stay the first path.
Audit and reconciliation jobs exist because at-least-once delivery, worker
crashes, rule changes, data migrations, source gaps, and future modules create
edge cases. The audit runner must catch those gaps before the business discovers
them manually.

## 2. Source Review Summary

This plan was prepared from the current Goat OS code and docs, Mesha wiki
graphs, and legacy repositories. Important source anchors:

- `context/architecture/operational-kernel.md`: product truth is Postgres plus
  durable audit/outbox/ledgers; logs are not business audit.
- `context/architecture/operational-kernel-system-design.md`: every vertical
  plugs into one kernel path; future domains supply policy packs, not private
  schedulers.
- `docs/protocol-engine/obligation-engine.md`: protocol rules, obligation
  rows, batches, proof, inventory, status events, and projections are shared.
- `docs/protocol-engine/state-machines.md`: SM-1 through SM-7 define schedule,
  shift, exit, batch, stock, feed generation, and booster behavior.
- `docs/protocol-engine/high-scale-kernel-validation-plan.md`: reconciliation
  must prove expected-vs-actual counts by tenant, park, category, protocol
  version, rule, and cohort; rule resolution is cached by
  `(tenant, park, category, as_of)` and projection accuracy must reconcile due,
  overdue, missed, deferred, suppressed, canceled, completed, and exception
  buckets.
- `docs/preventive-care-vaccination/vaccination-rules.md`: vaccination timing,
  pregnancy holds, warm-up, trusted source, same-day compatibility, defer and
  recovery behavior.
- `backend/internal/protocol/ports/ports.go`,
  `backend/internal/protocol/adapters/postgres/repository.go`, and
  `backend/migrations/postgres/000155_vaccination_capacity_config.sql`:
  vaccination capacity publish is guarded by `ErrCapacityParityMismatch`,
  transactional capacity upsert/parity check, and database checks for
  `max_per_day >= 1` and `max_buffer_days >= 0`.
- `docs/feed-direction/*`: feed direction requires safe counts/shifting input,
  pregnancy/warm-up/lactation signals, ration config, generation, packing,
  transport, consumption, wastage, proof, exception, and rework.
- `context/product/goat-os-feature-phases.md`: future modules include identity,
  SOP/proof, health, treatment, death, procurement, feed, breeding, workforce,
  inventory, sales/allocation, promise safety, analytics, and AI analyst.
- Legacy `dashboard/docs/ceo-dashboard-data-quality-audit-plan.md`: useful
  patterns for metric inventory, mismatch classification, relationship checks,
  and audit-only first rollout.
- Legacy `slack-automation-scripts/counting-db-slack-automation-handoff.md`:
  old Slack alerts used channel IDs and bot tokens in Apps Script properties;
  future Slack notifications must route through Goat OS APIs/adapters, not
  direct legacy scripts.
- Mesha wiki graphs: Slack modules, director handbooks, feed director material,
  and department roles are business context, but not runtime truth.

## 3. Architectural Decision

Use deterministic audit jobs and idempotent repair workers as the core.

AI agents may:

- summarize daily findings;
- cluster recurring failures;
- propose new invariants;
- draft human-readable runbooks;
- explain why a finding is critical.

AI agents must not:

- decide medical, feed, breeding, proof, or source-truth outcomes;
- create or edit canonical business facts;
- mark proof accepted;
- invent missing owners, DOB, pregnancy dates, feed quantities, completions, or
  source mappings;
- silently apply rule changes.

The kernel repair rule is:

```text
Auto-repair derived artifacts.
Create process exceptions for missing business truth or policy decisions.
Never fabricate source facts.
```

## 4. Schedule And Delivery

Default audit cadence when `GOATOS_KERNEL_AUDIT_ENABLED=true`:

| Run | Proposed IST time | Purpose |
| --- | --- | --- |
| Morning sweep | 06:00 | Catch overnight worker, outbox, projection, reminder, and missed-deadline gaps before field work starts. |
| Midday sweep | 12:00 | Catch first-half execution, stock, proof, and work-owner gaps. |
| Evening sweep | 18:00 | Catch day's operational gaps before leadership review. |
| Night sweep | 23:30 | Final reconcile after daily work, projection rebuilds, and notification dispatch. |

Slack delivery:

- P0/P1 findings: immediate Slack alert to `#goatos-audit`.
- Each sweep: short run summary if there are P0/P1/P2 findings or repairs.
- Daily digest: one end-of-day report after the night sweep.
- Weekly trend report: optional later, useful after enough run history exists.

Slack implementation rule:

- Store Slack bot token or webhook in Secret Manager, not in code.
- Store channel ID `C0BGSJDD420` in environment/config, not as the only routing
  source.
- P0/P1 alerts always create durable notification/report requests through the
  shared notification/reporting port before any Slack send is attempted.
- `kernel_audit_slack_deliveries` is only the Slack delivery ledger. It is not
  the source of alert state and must not become a private notification engine.
- P2/P3 summaries can share the same durable report-request path; batch digest
  delivery may use audit-report request rows, but still behind the shared
  notification/reporting boundary.
- Delivery retry/exhaustion must be visible and replay-safe.
- Legacy Apps Script Slack patterns are reference only; do not extend them for
  Goat OS canonical reports.

### 4.1 Liveness And Meta-Monitoring

Audit has to be audited, but only when the runtime flag is enabled. Every
expected sweep writes a `kernel_audit_runs` heartbeat with schedule key,
tenant/scope window, started time, heartbeat time, deadline, completed time,
status, code SHA, config hash, flag state, input windows, and report-delivery
state.

A small watchdog checks:

- an expected run did not start by its due time;
- a run is stuck in `running` beyond its deadline;
- a run failed repeatedly or stopped heartbeating;
- the daily digest was not requested or delivered;
- Slack delivery exhausted retries;
- the watchdog itself missed its own heartbeat.

Meta-alert truth must live outside Slack. Slack is last-mile delivery only. P0/P1
meta-alerts should also flow to Cloud Monitoring, email/PagerDuty-style webhook,
or an operator incident channel so a Slack outage does not hide audit failure.
When the global audit flag is off, the watchdog must not alert merely because
scheduled audit runs are absent.

## 5. Core Data Model

Add these concepts when implementation starts. Exact table names can change,
but the responsibilities should not.

Every audit table must be tenant-scoped and operationally scoped from day one.
Hot reads must be index-backed by tenant, scope, status, severity, category, and
time/cursor windows. A finding without `tenant_id` and explicit subject/scope is
not acceptable because it cannot be routed, suppressed, repaired, or reported
safely.

| Concept | Purpose and minimum contract |
| --- | --- |
| `kernel_audit_invariant_versions` | Versioned registry of invariant packs. Each pack is tied to tenant/category/module/source owner, rule source, code owner, and pack hash. |
| `kernel_audit_runs` | One row per sweep with `tenant_id`, run type, operational scope, schedule key, started/heartbeat/deadline/completed time, status, code SHA, config hash, input windows, watermarks, skipped scopes, and totals. Hot indexes: `(tenant_id, status, started_at)`, `(tenant_id, schedule_key, deadline_at)`, and `(tenant_id, scope_type, scope_id, started_at)`. |
| `kernel_audit_findings` | Durable finding rows with `tenant_id`, `run_id`, invariant ID, category, subject type/id, scope type/id, severity, status, owner, evidence, expected, actual, first/last seen, and resolution state. Hot indexes: `(tenant_id, status, severity, last_seen_at)`, `(tenant_id, category, status, last_seen_at)`, `(tenant_id, subject_type, subject_id)`, and a dedupe key. |
| `kernel_audit_repairs` | Repair requests and outcomes with `tenant_id`, `run_id`, finding ID, owning module, repair port/command, confidence level, dry-run diff hash, expected-version/precondition token, idempotency key, before/after hashes, affected rows, status, error, rollback/replay notes, verifier result, `trace_id`, `correlation_id`, and `causation_id`. Hot indexes: `(tenant_id, status, created_at)`, `(tenant_id, idempotency_key)`, `(tenant_id, finding_id)`, `(tenant_id, trace_id, created_at)`, `(tenant_id, correlation_id, created_at)`, and `(tenant_id, causation_id, created_at)`. |
| `kernel_audit_repair_proofs` | Immutable proof packet for each repair attempt: source event IDs, canonical row references, rule/protocol version, expected state, actual state, precondition result, dry-run diff, before/after snapshots or hashes, verification query, verifier result, and linked operation/audit rows. Hot indexes: `(tenant_id, repair_id)`, `(tenant_id, subject_type, subject_id, created_at, repair_id)`, `(tenant_id, invariant_id, created_at, repair_id)`, `(tenant_id, trace_id, created_at)`, `(tenant_id, correlation_id, created_at)`, `(tenant_id, causation_id, created_at)`, and cursor/time access for repair-run pages. Partition by tenant/time or time with tenant-leading local indexes before millions-scale retention. |
| `kernel_audit_repair_limits` | Per-tenant, per-invariant, per-category repair caps and circuit-breaker state. Stores dry-run-only flags, max repairs per run/day, confidence threshold, owner approval requirement, and last tripped reason. Hot indexes: `(tenant_id, invariant_id, status)` and `(tenant_id, tripped_at)`. |
| `kernel_audit_repair_cap_buckets` | One counter row per tenant/invariant/category/repair type/confidence level/period bucket. `UNIQUE(tenant_id, invariant_id, category, repair_type, confidence_level, period_start, period_end)` is the serialization anchor for quota claims. The bucket counter is the authoritative cap-enforcement state. Stores limit, reserved, consumed, refunded, circuit-breaker state, row version, and updated time. |
| `kernel_audit_repair_cap_claims` | Reservation rows under a cap bucket for mutation-capable repairs. Each row has `tenant_id`, `bucket_id`, `reservation_id`, requested/applied/refunded counts, status, expiry, and linked repair/proof IDs. Hot indexes: `(tenant_id, bucket_id, status)`, `(tenant_id, reservation_id)`, `(tenant_id, status, expires_at)`, and `(tenant_id, status, updated_at)` for `needs_reconcile` age checks. |
| `kernel_audit_scope_claims` | Single-flight leases for audit runner scope pages so overlapping orchestrator instances do not double-scan or double-repair the same tenant/scope/window. Claims use `FOR UPDATE SKIP LOCKED` or equivalent lease-safe semantics, heartbeat, expiry, reclaim status, owner identity, and a tenant-fair queue cursor so `SKIP LOCKED` cannot let a noisy tenant dominate every claim cycle. |
| `kernel_audit_suppressions` | Explicit reviewed suppressions with `tenant_id`, invariant/category, subject/scope, reason, expiry, approver, source evidence, and status. Hot indexes: `(tenant_id, status, expires_at)` and `(tenant_id, invariant_id, scope_type, scope_id)`. |
| `kernel_audit_slack_deliveries` | Delivery ledger for Slack messages, retry state, durable notification/report request ID, payload hash, channel, permalink if available, and exhausted-delivery reason. Hot indexes: `(tenant_id, status, next_attempt_at)` and `(tenant_id, request_id)`. |

Audit ledgers must have explicit retention, archive, and partition rollover
before millions-scale runs. Hot partitions keep active/open findings, recent
runs, recent repairs, live cap claims, and retryable deliveries. Closed
findings, completed repair proofs, old runs, old Slack deliveries, and expired
cap claims roll to cold/archive partitions by tenant/time after the configured
replay and investigation window. Operations Audit trace lookup must either span
hot and archive partitions transparently or publish an explicit trace-retrieval
window; acceptance cannot promise trace-from-any-subject while the archive path is
unqueryable. A `kernel-audit-retention` sweeper or the shared
`partition-maintainer` owns rollover, expiry, archive export, and index bloat
checks; no audit table may grow forever by default.

Findings should flow into command lenses:

- Control Tower: high-level exception and adherence health.
- Action Center: owner-specific next actions and blocked fixes.
- Protocol Adherence: rule/process-level failures, stale adherence
  percentages, and denominator/numerator mismatches.
- Calendar and workflow tickets: due, missed, blocked, snoozed, escalation,
  assignee, ticket count, and ticket status state.
- Operations Audit: durable state transitions, repairs, suppressions, and
  report deliveries.

## 6. Invariant Pack Contract

Every current and future feature that touches the kernel must add an audit pack.
An audit pack declares:

```text
category/module
business events it owns
canonical tables it writes
expected downstream artifacts
allowed statuses and transitions
obligation strategy and batch strategy
notification, reminder, escalation, and reporting policy
repairable derived gaps
non-repairable business gaps
owner role and escalation chain
projection/read-model expectations
metric, ticket, and command-lens expectations
Slack summary fields
scale bounds and indexes
test fixtures and seed cases
source docs / rules / approvals
```

Future automatic pickup depends on this rule:

```text
If a PR adds or changes a domain event, protocol category, obligation strategy,
status transition, notification policy, projection/read model, metric,
workflow ticket, calendar surface, or business rule, it must update or add the
relevant audit pack in the same change.
```

Initial CI/`make ai-doctor` enforcement should fail from registry/manifest
presence checks when it detects:

- new `protocol_definitions.category` without an audit pack;
- new domain event schema without an audit-pack entry and source pointer;
- new obligation strategy, batch strategy, or repair policy without invariant
  registry coverage;
- new obligation status or transition without an invariant registry update;
- new notification, reminder, escalation, or reporting policy without delivery
  and state coverage registration;
- new projection/read model, metric, command-lens, workflow ticket, or calendar
  surface without parity-expectation registration;
- new business rule or rule DSL path without detect/repair/non-repair policy
  registration;
- new sweeper/worker without run-ledger and audit coverage;
- new Slack/reporting route without delivery ledger and retry behavior;
- future PRD/TRD missing an `Audit And Reconciliation` section.

Deeper semantic checks that prove every expected artifact is fully mapped can
come after the audit-pack manifest format is stable. The first gate should be
portable and mechanical enough for `make ai-doctor` and normal CI.

## 7. Detection And Repair Classes

Repair boundary:

- `kernel-audit` owns audit runs, findings, repair requests, suppressions, and
  report delivery ledgers.
- Every business repair goes through the owning module's idempotent
  repair port, service, or command.
- The auditor records the finding, dispatches or requests the canonical repair,
  verifies the result, and reports the outcome.
- Direct SQL writes to another module's tables are forbidden unless the owning
  module exposes that exact SQL path as its repair implementation.
- No repair is considered successful until the invariant passes after the
  repair and the proof packet links the source evidence to the mutation.

| Class | Detect | Auto-fix? | Rule |
| --- | --- | --- | --- |
| Missing derived obligation | Canonical fact and active rule imply an obligation, but no active row exists. | Yes | Request the owning obligation/protocol/vaccination repair service to create the obligation with deterministic idempotency and status events; verify after. |
| Duplicate active obligation | More than one active obligation exists for same tenant/rule/target/due key. | Sometimes | Request owning obligation repair to supersede/cancel duplicates only if deterministic winner is provable; otherwise process exception. |
| Missing outbox event | Canonical transition committed but expected outbox row absent. | Sometimes | Request owning event publisher/outbox repair only when payload can be reconstructed exactly by that module. |
| Stuck outbox/consumer/DLQ | Age, retry count, or dead-letter state breaches policy. | Sometimes | Request owning relay/consumer/DLQ repair command to reclaim/retry known transient states; poison requires operator repair/discard reason. |
| Missing projection/read-model row | Canonical truth exists, projection missing/stale. | Yes | Request owning projection worker/recompute command to rebuild idempotently. |
| Stale lens, metric, ticket, or calendar aggregate | Canonical work/finding/proof/notification state disagrees with Control Tower, Action Center, Protocol Adherence percentage, workflow tickets, or Calendar. | Yes | Request owning projection/reporting worker to rebuild aggregate from canonical rows; never edit displayed totals directly. |
| Missing notification intent | Due/missed/escalated condition exists, no durable notification intent. | Yes | Create through shared notification/reporting port with dedupe key; do not mark delivery successful. |
| Missing Slack delivery | Durable notification/report request exists, Slack delivery absent/failed. | Yes | Retry delivery through notification adapter; Slack ledger is delivery evidence only. |
| Missing owner/assignee | Work exists but no source-backed owner/backfill. | No | Create process exception; never invent owner. |
| Missing business source data | DOB, pregnancy date, source trust, shed/stage, proof, count, feed vector, or stock lot missing. | No | Fail closed; create source-data exception. |
| Illegal status transition | Row moved out of allowed state machine. | No direct rewrite | Preserve evidence; create repair workflow or corrective work item. |
| Terminal state mutation | Completed/missed/waived/canceled/superseded changed in place. | No | P0/P1 finding; corrective lineage, never silent rewrite. |
| Rule/config drift | Code, seed, rule DSL, migration, and docs disagree. | No | Block publish/apply until explicit owner decision. |
| Legacy parity gap | Legacy workflow signal has no Goat OS equivalent. | No direct runtime fix | Track as migration/cutover gap with owner and target module. |

### 7.1 Repair Proof, Traceability, And Safe Self-Healing

Self-healing must behave like an audited workflow, not like a cron silently
editing rows. Every attempted repair writes a repair proof packet before and
after it calls the owning module.

Required repair lifecycle:

```text
detect mismatch
  -> write finding
  -> build repair plan
  -> dry-run expected diff
  -> re-check preconditions and capture expected-version token
  -> atomically reserve repair cap / enforce confidence threshold
  -> call owning module repair service with expected-version token
  -> emit repair/status/audit/outbox events through owner
  -> verify invariant again
  -> close, retry, revert request, or escalate
```

Required proof packet fields:

```text
repair_id
tenant_id / operational scope
invariant_id and audit pack version
severity and confidence level
source event IDs
canonical row references used as evidence
rule version / protocol version / config hash
expected state
actual state
dry-run diff
precondition query and result
expected row versions / source watermarks / invariant input hash
owning repair port/service/command
idempotency key
before snapshot or hash
after snapshot or hash
verification query and result
affected row count
repair status: proposed / skipped / applied / verified / failed / escalated
trace_id / correlation_id / causation_id
links to audit rows, status events, outbox rows, notifications, and Slack digest
cap reservation ID when a mutation-capable repair is attempted
```

The default decision rule:

```text
No repair without evidence.
No evidence without canonical source.
No mutation without owning service.
No success without verification.
No scale without caps, idempotency, and traceability.
```

Precondition re-check is mandatory. If the original mismatch changed between
detection and repair, the repair must skip, write `precondition_changed`, and
let the next audit window re-evaluate. This prevents self-healing from racing a
real worker and undoing valid state.

Owning repair ports must accept an expected-version/precondition token captured
at the final precondition check. The token should include the row versions,
source-event watermarks, and invariant input hash needed by that module. If a
live worker writes a newer state between audit precondition and repair apply,
the owning port returns `precondition_changed` and performs no mutation. Audit
idempotency alone is not enough protection against a different concurrent live
write.

Confidence levels:

| Level | Examples | Default action |
| --- | --- | --- |
| `safe_derived` | Projection rebuild, notification retry, Slack delivery retry, read-model aggregate rebuild. | Auto-repair allowed with caps. |
| `controlled_derived` | Missing obligation from canonical fact plus active rule, booster from accepted completion, open-work cancel after proven exit. | Dry-run first; auto-repair only after invariant-specific approval and low blast radius. |
| `manual_required` | Missing DOB, pregnancy date, proof decision, owner policy, stock truth, attendance approval, payroll truth. | Never auto-repair; create process exception. |

Blast-radius controls:

- per-run and per-day caps by tenant, invariant, category, repair type, and
  subject type;
- caps must be enforced through atomic period-bucketed reservations, not a
  read-then-write check that concurrent workers can race;
- dry-run-only mode for new invariants and newly changed business rules;
- automatic circuit breaker when expected affected rows exceed cap, verifier
  fails, repeated repairs hit the same subject, or same-key/different-payload
  idempotency conflicts appear;
- sample review requirement before promoting a `controlled_derived` repair from
  report-only to auto-fix;
- kill switch per invariant and per tenant.

Atomic cap reservation contract:

- the runner computes the cap bucket from tenant, invariant, category, repair
  type, confidence level, and period window such as run/day;
- each bucket has exactly one `kernel_audit_repair_cap_buckets` row protected by
  `UNIQUE(tenant_id, invariant_id, category, repair_type, confidence_level,
  period_start, period_end)`;
- the quota claim updates the bucket counter row and writes the
  `kernel_audit_repair_cap_claims`, `kernel_audit_repairs`, and initial proof
  packet in one transaction with a unique `reservation_id`;
- the transaction must lock or compare-and-update the bucket row, or run under a
  serializable retry loop, so `reserved_count + claim_count <= limit` is
  enforced by the database under concurrent workers;
- a worker may call the owning repair service only after the reservation commits
  and the repair request references the `reservation_id`;
- skipped, precondition-changed, dry-run-only, or owner-rejected repairs release
  or refund the reservation in a replay-safe transaction;
- applied repairs keep the reservation consumed even if post-repair verification
  later fails, so repeated bad repairs trip the circuit breaker instead of
  cycling through refunded capacity;
- expired in-flight reservations are not blindly refunded. The watchdog first
  reconciles against `kernel_audit_repairs`, proof rows, owning service audit
  rows, and status/outbox evidence. If the owning repair committed, the
  reservation is consumed; if it provably did not commit, it is refunded; if
  outcome is unclear, the reservation becomes `needs_reconcile` and counts
  against the cap until an operator or deterministic verifier resolves it;
- `needs_reconcile` is fail-safe, not silent. Expose count and oldest-age metrics
  by tenant, invariant, repair type, and bucket window; alert when count or age
  crosses the cap-ledger SLO so ambiguous crash outcomes cannot quietly exhaust a
  bucket and block all future repairs;
- `kernel_audit_repair_cap_buckets` is authoritative for cap enforcement.
  `kernel_audit_repair_cap_claims` is the explainable ledger underneath it. A
  periodic self-audit must check bucket/claim parity, including
  `bucket.reserved == SUM(open claim.requested)` and consumed/refunded totals for
  closed claims. Drift opens a P1 audit-system finding and blocks mutation-capable
  repairs for that bucket until reconciled.

Traceability rules:

- carry `trace_id`, `correlation_id`, and `causation_id` from source event to
  finding, repair request, owning service call, status event, outbox event,
  notification, projection update, Operations Audit row, and Slack digest;
- every goat, obligation, ticket, calendar item, notification, and projection
  touched by repair must be able to show "changed by audit repair X because
  invariant Y failed, using evidence A/B/C";
- raw logs are secondary. Debugging starts from durable repair/finding/proof
  rows and only then jumps to logs/traces.

Operator surfaces:

- Operations Audit needs a repair detail page with evidence, dry-run diff,
  before/after hashes, verifier result, owning service, idempotency key, and
  full trace chain across hot and archived partitions, or a visible retention
  window when older traces have intentionally expired;
- Control Tower needs repair health by category: repaired, skipped,
  precondition-changed, failed, escalated, repeated, and circuit-broken;
- Action Center needs owner-visible process exceptions for every non-repairable
  finding;
- Slack messages must include counts plus links to the audit run and top repair
  proof packets, not raw row dumps.

## 8. Current Feature Audit Matrix

### 8.1 Protocol Config And Rule Publishing

Detect:

- published rule version without required `protocol.publish.<category>` server
  capability check;
- missing explicit publish capability seed for a category;
- overlapping active windows for a scope where policy says single active;
- published version missing executable `sop_version_id` or object
  `proof_policy` where execution needs proof;
- draft/inactive versions generating obligations;
- rule DSL embedding animal snapshots or herd facts instead of policy;
- published `rule_dsl.capacity` differs from the derived
  `vaccination_capacity_config` row for that tenant;
- capacity values that violate storage invariants, such as `max_per_day < 1`
  or `max_buffer_days < 0`, appear in any published path;
- an `ErrCapacityParityMismatch`-class publish rollback leaves a visible
  partially published version or capacity row;
- impact preview not generated or not reviewed before activation;
- new protocol category not listed in audit pack registry.

Auto-fix:

- ask the protocol-owned repair/recompute path to rebuild derived
  scope-resolution rows;
- ask the protocol-owned projection path to refresh protocol list/read
  projections;
- retry missing publish outbox only through the protocol publisher/outbox repair
  path when payload can be reconstructed exactly;
- repair capacity read-model drift only when the published rule version is
  authoritative and the protocol parity repair path proves the capacity row is
  the only stale derived artifact.

Do not auto-fix:

- publish/retire rules;
- infer missing SOP binding;
- widen scope from park to tenant;
- invent approval or rule source evidence;
- bypass the atomic protocol publish transaction or its capacity parity guard.

Slack:

- P0 for active conflicting rules or draft rules generating work.
- P1 for publish blocked by missing SOP/proof/capability seed.

### 8.2 Preventive Care Vaccination

Detect:

- accepted birth/procurement/stage-change event did not create expected
  vaccination obligations;
- birth-age rules silently skipped animals with missing DOB instead of producing
  visible defer/block reason;
- warm-up hold uses wrong anchor date;
- mother-vaccination status appears in model, config, seed, import, or schedule;
- pregnancy month 4/5 animals have schedulable vaccination work;
- deferred sick/ICU/quarantine/pregnancy animals do not reopen or micro-drive
  after recovery;
- recovered deferred animals do not rejoin a compatible drive within 7
  location-local calendar days or get a micro-drive scheduled inside that
  recovery buffer;
- trusted procurement holding-park history suppresses work correctly, while
  third-party/vendor claims do not;
- same-day compatibility, max two shots, priority, one-time batching hold, and
  live/killed gaps are violated;
- species-locked vaccine reaches wrong species;
- booster obligation missing after accepted completion;
- booster generated before accepted verification;
- dead/sold/transferred/lost animal still appears active/overdue;
- open in-flight batch still contains exited or shifted animal without
  reconciliation marker;
- inventory reservation/consume/release not balanced for a batch;
- Calendar, Action Center, Protocol Adherence, Passport, and Control Tower
  disagree on due/missed/completed/deferred state.

Auto-fix:

- request the vaccination/obligation repair service to create missing
  obligations from canonical fact plus active rule;
- request the owning obligation repair path to reopen deferred work after
  recovery when deterministic;
- request the owning obligation repair path to cancel open work after exit
  event;
- request the vaccination booster generator to regenerate booster work from an
  accepted completion event;
- request vaccination eligibility rollup and command-lens projection recompute;
- create missing notification intent through the shared
  notification/reporting port for due/missed/escalated state.

Do not auto-fix:

- mark vaccination completed without proof/verification;
- trust third-party vaccination claims;
- infer pregnancy month without breeding-date evidence;
- change medical schedule values;
- invent stock lots or proof media.

Slack:

- P0 for unsafe scheduling: pregnancy hold violation, wrong species, vendor
  claim suppressing work, dead animal actionable.
- P1 for missing obligations, missing booster, stuck defer recovery, projection
  contradiction, stock reconciliation issue.

### 8.3 Vaccination Execution, SOP, Proof, And Verification

Detect:

- due obligations not grouped into eligible batches/SOP tasks by sweeper;
- batch exists without linked obligations or task;
- task/proof accepted but obligation/completion/projection not updated;
- proof rejected but rework obligation/action missing;
- completion did not consume/release stock as required;
- status event ledger missing for obligation transition;
- verification fanout did not publish completion/rejection event;
- in-progress work expired but not marked missed/rework;
- missed/overdue/due bucket is derived from current obligation status alone
  instead of as-of-effective state reconstructed from due window,
  `completed_at`, status events, and sweeper-persisted missed markers;
- UI shows action enabled when backend says disabled.

Auto-fix:

- request the owning execution/obligation service to create missing planned
  batch/task rows for due work when deterministic;
- request task/read-model projection rebuild;
- retry replay-safe proof fanout or projection update through the SOP/proof
  owner;
- create missing notification intent through the shared
  notification/reporting port for proof pending, verification pending, rejected
  proof, or missed execution.

Do not auto-fix:

- accept or reject proof;
- complete a vaccination from media presence alone;
- rewrite terminal statuses.

Slack:

- P1 for proof/completion chain break.
- P2 for stale projections or missing non-critical notifications.

### 8.4 Event Spine, Outbox, Consumers, And DLQ

Detect:

- canonical transition committed without audit/outbox where required;
- outbox row stuck in publishing/scheduled/retry beyond policy;
- relay oldest-unsent age or Pub/Sub backlog breaches threshold;
- processed-event dedupe missing for at-least-once consumer;
- duplicate event created duplicate business effect;
- poison messages in DLQ without owner/replay/discard plan;
- local non-durable publisher mode accidentally configured outside local/dev.

Auto-fix:

- request the owning outbox relay to reclaim stale relay leases;
- request the owning outbox/DLQ command to retry transient failed rows;
- request the owning consumer to replay deduped handlers when idempotency proves
  no duplicate effects;
- create operator-visible DLQ repair finding.

Do not auto-fix:

- discard poison message without reason and authority;
- reconstruct event payload when canonical data is incomplete;
- switch production delivery to non-durable local/eventbus mode.

Slack:

- P0 for duplicate business effect or production non-durable event mode.
- P1 for DLQ growth, old unsent age, or stuck relay.

### 8.5 Time Spine: Sweepers, Reminders, Escalations, Notifications

Detect:

- `scheduled` rows past due not promoted to `due`;
- due rows past SLA not marked missed or escalated;
- as-of due/missed/overdue bucket is computed from UTC or server-default date
  instead of the location-local business calendar;
- persisted missed state disagrees with the as-of-effective expectation for the
  audited window;
- read-time overdue projections disagree with as-of-effective due/completion
  state;
- reminder/nudge/escalation policy threshold crossed without durable intent;
- notification request exhausted without operator visibility;
- sweeper scans unbounded windows or lacks tenant/date/status index shape;
- noisy tenant starves quiet tenants;
- Cloud Tasks contains work not reconstructable from Postgres.

Auto-fix:

- request the owning sweeper to promote due rows by bounded indexed windows;
- create missing reminder/escalation intent through the shared
  notification/reporting port with dedupe key;
- retry notification delivery through the shared adapter circuit breaker;
- request missed/due projection rebuild.

Do not auto-fix:

- resolve/acknowledge escalation;
- hide exhausted delivery;
- use frontend timers as scheduler.

Slack:

- P1 for missed deadline without escalation.
- P2 for notification delivery retry/exhausted states.

### 8.6 Operations Audit Surface

Detect:

- business transition exists only in technical logs;
- product state mutation lacks audit/history/status event;
- Operations Audit route reads mock data or raw engineering logs;
- audit rows missing actor, source, idempotency, scope, or evidence;
- audit volume flooded by read-only page polling.

Auto-fix:

- request Operations Audit projection rebuild where source rows exist;
- create audit-run finding for missing audit source.

Do not auto-fix:

- invent an actor or reason;
- backfill business audit if before/after state cannot be proven.

Slack:

- P1 for missing audit on material state transitions.

### 8.7 Counts, Shifting, And Safe Feed Input

Detect:

- base count or shifting projection missing/stale for target date;
- unreported shifting mismatch between accepted count and movement evidence;
- missing or ambiguous structured cohort/stage impact;
- alias conflict for breed/stage/shed-tag/sex/age;
- projection row lacks pregnancy/lactation/warm-up or ration-context state
  required by feed;
- pregnant/lactating/warm-up shifted cohort lacks destination safety projection;
- open projection exception not visible to Feed readiness;
- Counts projection and Feed readiness disagree.

Auto-fix:

- invoke the counts/location owning commands for bounded projection recompute;
- invoke the counts mismatch scan owner;
- request readiness evidence ledger refresh through the feed/counts owner;
- request read-projection rebuild.

Do not auto-fix:

- silently normalize aliases;
- change counts from ambiguous movement;
- infer shed placement without source/owner decision.

Slack:

- P1 for feed-blocking projection gaps on tomorrow's feed window.
- P2 for stale or pending alias/source evidence.

### 8.8 Feed Direction

Detect:

- generation allowed without safe Counts/Shifting projection;
- generation skipped when all gates are ready and active feed rules exist;
- Diff did not supersede/cancel affected stale open work;
- stock reserved at generation instead of packing;
- packing proof accepted without consume/release ledger;
- shifted pregnant/lactating/warm-up cohort not recalculated before serving;
- destination shortage does not block or escalate;
- overpack, leftover, moist feed, refusal, or sickness-risk threshold has no
  exception/rework path;
- transport map missing for grouped transport work;
- command lenses lack buckets for blocked generation, packing shortfall, proof
  missing, transport rejected, consumption incomplete, wastage exception,
  bridge exception, stock-out, and rework.

Auto-fix:

- request feed readiness and generation preview through the feed owner;
- request feed read-model bucket rebuild;
- request stale open Diff work supersede/cancel through the feed obligation
  repair path when deterministic;
- create or retry notification intent through the shared
  notification/reporting port for blocked/shortfall/rework states.

Do not auto-fix:

- calculate ration from unapproved workbook formulas;
- infer pregnancy/warm-up policy;
- invent transport maps or feed vectors;
- consume inventory without accepted proof.

Slack:

- P0 for unsafe feed risk: pregnant/high-risk underfeed, unblocked shortage,
  stale feed served risk.
- P1 for missing generation/Diff/packing/rework artifacts.

### 8.9 Procurement Source Entry And Accepted Intake

Detect:

- accepted intake does not emit accepted-intake or animal-created outbox event;
- rejected/deferred/block decisions create active vaccination obligations;
- arrival count mismatch unresolved but intake accepted;
- pre-dispatch/truck/arrival proof missing for accepted animal;
- dead/sold/lost during holding/transit does not cancel/rescope work;
- holding-park trusted vaccination history not linked with evidence;
- third-party vendor vaccination claim suppresses Goat OS work.

Auto-fix:

- request procurement handoff replay to create missing deterministic downstream
  obligations;
- request obligation repair to cancel open work for rejected/exited animals;
- request procurement/vaccination handoff projection rebuild.

Do not auto-fix:

- accept/reject an animal;
- trust source claims;
- resolve arrival mismatch without proof.

Slack:

- P1 for accepted intake missing downstream obligations or unresolved arrival
  mismatch.

### 8.10 Workforce, Roster, Backfill, And HR/People

Detect:

- actionable obligation has no explicit owner/backfill/escalation chain;
- assigned worker absent but no qualified backup or park-head escalation;
- roster coverage gap for due work window;
- role/capability missing for executor, verifier, publisher, or escalator;
- provisional assignments used in runtime path;
- owner shown in UI differs from backend contract.

Auto-fix:

- request workforce/HRMS projection refresh for roster coverage and read models;
- create missing owner-gap process exception.

Do not auto-fix:

- assign work by round-robin or "first available" without reviewed policy;
- mark provisional mappings as reviewed.

Slack:

- P1 for due work with no owner or failed backfill.

### 8.11 Admin Web, Mobile, Offline Sync, And UI Contracts

Detect:

- frontend/mobile hardcodes workflow status, action labels, filters, or disabled
  reasons that backend owns;
- UI shows stale canonical state after projection refresh;
- offline outbox entry stuck, duplicated, or submitted with same key different
  payload;
- mobile proof upload missing metadata or retry state;
- route visible outside RBAC scope;
- command lens reads a different source than backend projections.

Auto-fix:

- request mobile sync owner to retry safe outbox submissions;
- request admin/bootstrap owner to refresh UI contract cache;
- request owning projection rebuild.

Do not auto-fix:

- mutate local client state as canonical truth;
- resolve same-key/different-payload conflict automatically.

Slack:

- P2 for UI contract drift.
- P1 when drift could cause unsafe/incorrect field action.

## 9. Future Feature Audit Packs

Each future feature must add its own pack before implementation is called done.
Minimum pack expectations:

All repair policies below mean "through the owning module's repair
port/service/command, then verified by kernel audit." They are shorthand, not
permission for `kernel-audit` to write directly into module tables.

| Future feature | Must detect | Repair policy |
| --- | --- | --- |
| Birth and abortion | Birth fact without animal identity, dam linkage, vaccination schedule, kid count/projection, proof/review, or exception. | Create derived obligations/projections; source gaps become exception. |
| Breeding and pregnancy | Pregnancy/breeding event without feed/vaccination re-evaluation, late-pregnancy hold, delivery prep, or owner alert. | Recompute derived obligations; never infer breeding date. |
| Treatment and deworming | Diagnosis/treatment event without medicine withdrawal, follow-up obligation, proof, stock ledger, or health status projection. | Create missing follow-up/notification; never mark recovered. |
| Quarantine and ICU | Entry/exit without blocking/resuming vaccination/feed/movement obligations, proof, owner, and escalation. | Defer/resume deterministic work; never create health truth from tags alone. |
| Death, sale, lost, cull, transfer | Exit without canceling active work, releasing reservations, closing projections, and preserving proof/audit. | Cancel/release derived open work; never delete history. |
| Inventory and stock | Reservation/consume/release imbalance, expired lot used, negative balance, missing stock-out exception. | Reconcile ledger only when movement evidence is exact; otherwise exception. |
| Movement and shifting | Movement fact without location history, obligation re-scope, count projection, feed projection, and proof. | Re-scope open work; ambiguous movement becomes exception. |
| Farmer network | Contract/source event without onboarding tasks, verification, payment/procurement linkage, and proof. | Derived tasks only; no invented partner truth. |
| Sales/allocation/promise safety | Booking/allocation without eligibility check, health/feed/withdrawal monitoring, double-book prevention, or replacement task. | Recompute promise risk; never allocate by inference. |
| Workflow tickets and calendar | Business work without ticket/calendar state, stale ticket counts, missing assignee/escalation, or mismatched Calendar/Action Center/Protocol Adherence totals. | Rebuild derived ticket/calendar/protocol-adherence projections through owning reporting ports; no direct metric edits. |
| HRMS, attendance, and payroll | Attendance, leave, role, capability, shift, payroll, or roster fact without owner/backfill recalculation, work reassignment, notification, audit, or read-model update. | Recompute workforce/HRMS projections and owner-gap exceptions through HRMS/workforce ports; never infer attendance, approval, or payroll truth. |
| Finance/payments | Payment obligation without source invoice/event, approval, reconciliation, or ledger state. | Rebuild derived read models; no payment truth from Slack text. |
| AI analyst | AI recommendation without evidence, confidence, reviewer, and non-mutating proposal state. | Never auto-apply AI output; create review proposal only. |

## 10. Slack Report Format

Daily digest example:

```text
Goat OS audit digest - 2026-07-11
Window: 00:00-23:30 IST
Code/config: <git_sha> / <audit_pack_hash>

Overall:
- P0: 0
- P1: 3
- P2: 12
- Auto-repaired: 41
- Needs owner decision: 5
- Repair verifier failures: 0
- Circuit breakers tripped: 0
- Slack delivery status: delivered

Critical:
1. P1 vaccination.birth_missing_obligation
   18 accepted animal-created facts had no matching vaccination obligation.
   Repair: 18 obligations requested through vaccination repair service and
   verified idempotently.
   Trace: repair_run=<url> proof_packets=<url>
   Owner: Preventive Care Director

2. P1 feed.safe_input_blocked
   Tomorrow's feed generation blocked for 2 sheds due to open alias conflict.
   Repair: no auto-fix; Action Center exception created.
   Owner: Feed Director

Auto-repaired:
- 18 missing vaccination obligations
- 9 stale Calendar projection rows
- 7 missing notification intents
- 7 stale Count/Feed readiness rows

Needs action:
- 2 missing owner/backfill mappings
- 2 feed alias conflicts
- 1 untrusted source vaccination claim used in intake notes

Links:
- Control Tower: <url>
- Action Center filtered to audit findings: <url>
- Operations Audit run: <url>
```

Per-sweep Slack message should be shorter:

```text
Goat OS audit sweep 12:00 IST: P0=0 P1=1 P2=4 auto_repaired=12 blocked=2.
Top issue: feed safe input blocked for tomorrow in 2 sheds.
run=<url> top_proofs=<url>
```

## 11. Severity Model

| Severity | Meaning | Slack behavior |
| --- | --- | --- |
| P0 | Unsafe business outcome likely or already happened. Example: wrong-species vaccine, pregnant month 4/5 scheduled, dead animal actionable, duplicate completion effect. | Immediate alert, repeated until acknowledged/resolved. |
| P1 | Kernel chain broken but deterministic repair or owner action can prevent harm. Example: missing obligation, stuck DLQ, due item not escalated. | Immediate or next sweep alert plus daily digest. |
| P2 | Process integrity degradation. Example: stale projection, missing non-critical notification, UI contract drift. | Sweep summary and daily digest. |
| P3 | Hygiene/trend. Example: old suppressions near expiry, low-volume retry noise. | Daily/weekly only. |

## 12. Implementation Roadmap

### Phase A - Audit inventory and registry

- Build read-only inventory of events, protocol categories, obligation rules,
  worker commands, projections, notifications, and command-lens APIs.
- Classify existing `backend/cmd/*` jobs as orchestrate/wrap, run alongside,
  callable repair input, or non-audit seed/migration/dev tooling.
- Create initial audit packs for protocol, vaccination, execution, event spine,
  sweeper, notifications, operations audit, counts/shifting, feed readiness,
  procurement handoff, workforce, admin-web, and mobile sync.
- Add docs PRD/TRD template requirement: `Audit And Reconciliation`.

### Phase B - Read-only runner

- Implement `backend/cmd/kernel-audit` in report-only mode as orchestrator,
  normalizer, run ledger, finding ledger, and reporter.
- Gate the command, scheduler, watchdog, Slack delivery, and any repair request
  behind `GOATOS_KERNEL_AUDIT_ENABLED`; default off must be a no-op.
- Record `kernel_audit_runs` and `kernel_audit_findings`.
- Add expected-run liveness heartbeats and watchdog alerts before auto-repair.
- Add repair proof packet schema before enabling any mutation-capable repair.
- Cover vaccination and event-spine invariants first.
- No auto-repair in this phase.

### Phase C - Deterministic repair

- Add repair allowlist for derived artifacts only, executed through owning
  module repair ports/services/commands:
  missing obligations, stale projections, missing notification intents, stale
  relay leases, and replay-safe outbox repair.
- Every repair writes `kernel_audit_repairs`, audit/history, and idempotency key.
- Every repair has a verifier query and explicit owner module.
- Every repair stores dry-run diff, precondition result, proof packet,
  before/after hashes, trace IDs, and post-repair verifier result.
- Every mutation-capable repair reserves cap capacity atomically in the same
  transaction as the repair request and initial proof packet, using a
  reservation ID that is consumed, refunded, expired, or circuit-broken
  explicitly.
- Add per-tenant and per-invariant repair caps, dry-run-only flags, confidence
  thresholds, and kill switches before enabling `controlled_derived` repairs.
- Same-key different-payload is a conflict, never a repair.

### Phase D - Slack delivery

- Add Slack delivery adapter behind notification/reporting port.
- Use Secret Manager for token/webhook.
- Route daily digest and P0/P1 alerts to `C0BGSJDD420`.
- Add delivery retry/exhausted visibility in `kernel_audit_slack_deliveries`.
- Keep P0/P1 alert state in durable notification/report requests, not in the
  Slack delivery ledger.

### Phase E - Future-feature enforcement

- Add manifest/registry-presence checks for new event/category/status/worker/
  projection/metric/ticket/calendar/notification/business-rule surfaces without
  audit pack registration.
- Hook the presence checks into `make ai-doctor`/CI first; keep deeper semantic
  expected-artifact validation as a later implementation gate once the audit
  pack manifest format is stable.
- Add codegen or manifest validation so audit runner picks up new packs.
- Require audit-pack status in phase closeout.

### Phase F - Scale certification

- Prove audit scans are tenant/date/status/cursor bounded.
- Run `make scale-guard` and keep new audit implementation compliant with
  `docs/decisions/scale-anti-patterns.md`. Any unavoidable compute-from-raw
  reconciliation inside `backend/internal/**` must be bounded and carry a narrow
  `// scale-guard:ignore: <bounded reason>` annotation; do not baseline new
  audit offenders.
- Validate query plans for widest audit checks with `make validate-sqlc-plans`
  plus seeded `ANALYZE` and `EXPLAIN (ANALYZE, BUFFERS)` evidence at realistic
  row counts, without relying on `enable_seqscan=off`.
- Include noisy-tenant fairness in audit workers.
- Add high-scale report rows for audit runner throughput, lag, repairs,
  Slack delivery, and projection parity.
- Load-test repair proof lookup and trace traversal by tenant, subject, repair,
  invariant, and correlation ID before enabling millions-scale auto-fix.
- Run the real kernel gates where applicable: `make high-scale-kernel-e2e-data`,
  `make high-scale-kernel-e2e-all`, `make high-scale-kernel-e2e-certification`,
  and `make scale-kernel-gate` / `make scale-kernel-gate-smoke` for the shared
  kernel paths the audit runner exercises.
- Commit generated E2E/scale reports into the repo and surface them on the
  GitHub Pages CI report site before handoff. Reports must state whether they are
  local-only proof, staging certification, or production certification.

## 13. Query And Worker Rules

The audit runner must follow the same high-scale rules as the kernel:

- no full-herd scans;
- no `OFFSET` on hot tables;
- keyset pagination by tenant/scope/date/status/cursor;
- scope/page claims use `FOR UPDATE SKIP LOCKED`, lease single-flight, or an
  equivalent claim table with heartbeat and stale-claim reclaim;
- bounded worker pool;
- one page/batch per transaction;
- deterministic idempotency keys;
- trace/correlation/causation IDs on every audit, repair, event, notification,
  projection, and report write;
- stale-run heartbeat and reclaim;
- tenant-fair claim order;
- query-plan validation for hot checks;
- `make scale-guard` compliance for implementation code;
- clear freshness envelope for report data.

Tenant fairness is an ordering contract, not a side effect of `SKIP LOCKED`.
The scheduler must maintain a tenant-fair queue, round-robin cursor, weighted
fair cursor, or equivalent per-tenant claim budget before it enters
`FOR UPDATE SKIP LOCKED` page claims. `SKIP LOCKED` prevents double-claiming
inside the selected queue slice; it must not be the only mechanism deciding which
tenant receives the next worker page. Under skewed load, quiet tenants must still
receive progress and visible skipped-scope reasons.

### Reconciliation Strategy

Routine audits use event-window deltas first: new or changed domain events,
status events, outbox rows, obligation rows, proof rows, notification requests,
projection updates, and repair outcomes since the last good watermark.

Aggregate parity checks compare bounded counts by tenant, category, scope,
status, due bucket, protocol version, and command-lens bucket. When parity
fails, the runner scans only the changed scope or partition needed to identify
subjects. Partitioned deep scans run on a slower schedule with tenant fairness,
cursor checkpoints, and explicit skipped-scope reasons. A routine sweep must not
scan the whole herd, all tickets, all calendar rows, or all HRMS/workforce rows
just because one invariant changed.

Temporal reconciliation must be as-of-effective. Missed/overdue checks should
reconstruct expected state for the audit window from due dates, SLA windows,
`completed_at`, status-event history, and sweeper-persisted missed markers.
Day boundaries must resolve through the subject location's business calendar
timezone, with the current fallback/default as `Asia/Kolkata` where no more
specific location timezone exists. The auditor must not compute medical or
calendar-day buckets with `time.Now().UTC().Date()`, server-local defaults, or
raw UTC date truncation. It also must not bucket directly from current
`oi.status`, because current status can encode the bug being audited. Persisted
`missed` and read-time `overdue` must be reconciled against the same
location-local as-of expectation before the auditor creates or closes findings.

This reconstruction is an audit-only exception to the normal compute-on-write
rule. The implementation must still follow `docs/decisions/scale-anti-patterns.md`:
use event-window deltas, aggregate parity, keyset-scoped subject scans, persisted
watermarks, and module-owned projections. Do not implement due/missed/overdue
reconciliation as a whole-tenant compute-on-read god CTE or request-path
rebuild. If a compute-from-raw check is genuinely unavoidable in
`backend/internal/**`, it must be narrowly bounded, explain why the bounded scan
is safe, and carry a `// scale-guard:ignore: <bounded reason>` annotation instead
of disabling or extending the scale-guard baseline.

An audit finding is not allowed to be based on stale or partial data without
saying so. Reports must include input windows, projection freshness, source
watermarks, and skipped/deferred scan reasons.

## 14. Non-Goals

- Do not run kernel audit runtime unless `GOATOS_KERNEL_AUDIT_ENABLED=true`.
- Do not resurrect legacy Slack/App Script as Goat OS runtime.
- Do not use Slack as canonical truth.
- Do not let AI agents apply business fixes.
- Do not create one private audit cron per module.
- Do not let `kernel-audit` re-own existing sweepers, relays, dispatchers,
  generators, or projection rebuilders.
- Do not write repairs directly to another module's tables outside owning
  service/port boundaries.
- Do not use `kernel_audit_slack_deliveries` as canonical alert state.
- Do not mark a repair successful without proof packet and post-repair
  verification.
- Do not allow unlimited auto-fix volume for any invariant, tenant, or repair
  type.
- Do not enforce repair caps with a non-atomic read/check/write sequence.
- Do not reconcile due/missed/overdue buckets from current status alone.
- Do not compute user-facing medical, recovery, due, missed, or overdue
  calendar-day buckets in UTC when the business rule is location-local.
- Do not turn daily digest into a noisy dump of every P3 hygiene issue.

## 15. Acceptance Criteria

The plan is implemented when:

1. `GOATOS_KERNEL_AUDIT_ENABLED` defaults off, and off means no runner, schedule,
   watchdog alert, Slack audit delivery, finding write, repair request, or
   self-healing action.
2. When the flag is enabled, four scheduled audit sweeps run daily with durable
   run rows that record the flag state.
3. Expected-run watchdogs alert when enabled sweeps, daily digest, Slack
   delivery, or the watchdog itself miss heartbeat/deadline.
4. Daily Slack digest reaches `#goatos-audit`.
5. P0/P1 findings alert promptly through durable notification/report requests
   and link to Operations Audit/Action Center.
6. Vaccination birth/procurement/completion/exit/defer/recovery cases reconcile
   expected-vs-actual obligations and projections.
7. Event spine, outbox, DLQ, sweepers, notification intent, and projection
   freshness are covered.
8. Auto-repair is restricted to deterministic derived artifacts, executed
   through owning module repair ports/services/commands, verified, and fully
   audited.
9. Every repair has a durable proof packet with source evidence, dry-run diff,
   preconditions, before/after hashes, verifier result, idempotency key, and
   trace/correlation/causation IDs.
10. Repair caps, confidence levels, dry-run mode, circuit breakers, and kill
    switches prevent broad accidental mutation at tenant and invariant scale;
    cap reservations are atomic and period-bucketed under concurrent workers,
    using a unique cap-bucket serialization row, cap bucket counters are the
    authoritative enforcement source, claim parity is self-audited, and
    `needs_reconcile` count/age alerts prevent silent cap starvation.
11. Missing source truth creates process exceptions, not fabricated data.
12. Protocol Adherence percentages, ticket counts, notification state, calendar
   state, and command-lens projections reconcile from canonical state using
   location-local calendar day semantics for user-facing due/missed/overdue and
   recovery windows.
13. Operations Audit can trace from any repaired subject to finding, source
    evidence, owning repair service, emitted events, notifications, projections,
    and Slack report link across hot and archive partitions, or it publishes an
    explicit trace-retrieval retention window.
14. Feed Direction, HRMS/workforce, and every future feature can register an
   audit pack before it ships.
15. CI blocks new kernel events/categories/statuses/workers/projections/metrics/
   tickets/calendar surfaces/business rules without audit-pack registry
   coverage.
16. High-scale validation proves audit scans, repair proof lookup, and trace
   traversal are bounded and tenant-fair, including lookups by invariant,
   trace ID, correlation ID, subject, repair, and time cursor. Phase F evidence
   includes `make scale-guard`, `make validate-sqlc-plans`, realistic
   `EXPLAIN (ANALYZE, BUFFERS)` plans, applicable high-scale kernel gates, and
   committed GitHub Pages reports that label local-only vs staging/production
   certification.
17. Audit tables have a documented retention, archive, and partition-rollover
    policy, and scope claims are both lease-safe against overlapping
    orchestrators and tenant-fair under skewed load.
