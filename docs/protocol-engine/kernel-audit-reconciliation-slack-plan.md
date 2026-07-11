# Kernel Audit, Reconciliation, And Slack Reporting Plan

Status: proposed architecture and rollout plan
Date: 2026-07-11
Slack target: `#goatos-audit` (`C0BGSJDD420`) in workspace `T091RHJF43E`
Slack URL: https://app.slack.com/client/T091RHJF43E/C0BGSJDD420

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
  version, rule, and cohort.
- `docs/preventive-care-vaccination/vaccination-rules.md`: vaccination timing,
  pregnancy holds, warm-up, trusted source, same-day compatibility, defer and
  recovery behavior.
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

Default audit cadence:

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
- Write a durable `notification_requests` or audit-report delivery row before
  sending.
- Delivery retry/exhaustion must be visible and replay-safe.
- Legacy Apps Script Slack patterns are reference only; do not extend them for
  Goat OS canonical reports.

## 5. Core Data Model

Add these concepts when implementation starts. Exact table names can change,
but the responsibilities should not.

| Concept | Purpose |
| --- | --- |
| `kernel_audit_invariant_versions` | Versioned registry of invariant packs. Each pack is tied to a category/module and source doc/code owner. |
| `kernel_audit_runs` | One row per sweep, with run type, started/completed time, status, code SHA, config hash, input windows, and totals. |
| `kernel_audit_findings` | Durable finding rows with invariant ID, subject, severity, status, owner, evidence, expected, actual, and first/last seen. |
| `kernel_audit_repairs` | Repair attempts and outcomes, including idempotency key, before/after, affected rows, and rollback/replay notes. |
| `kernel_audit_suppressions` | Explicit reviewed suppressions with scope, reason, expiry, approver, and source evidence. |
| `kernel_audit_slack_deliveries` | Delivery ledger for Slack messages, retry state, payload hash, permalink if available, and exhausted-delivery reason. |

Findings should flow into command lenses:

- Control Tower: high-level exception and adherence health.
- Action Center: owner-specific next actions and blocked fixes.
- Protocol Adherence: rule/process-level failures.
- Calendar: due, missed, blocked, snoozed, and escalation time state.
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
repairable derived gaps
non-repairable business gaps
owner role and escalation chain
projection/read-model expectations
Slack summary fields
scale bounds and indexes
test fixtures and seed cases
source docs / rules / approvals
```

Future automatic pickup depends on this rule:

```text
If a PR adds or changes a domain event, protocol category, obligation strategy,
status transition, notification policy, projection, or business rule, it must
update or add the relevant audit pack in the same change.
```

CI should eventually fail when it detects:

- new `protocol_definitions.category` without an audit pack;
- new domain event schema without expected-artifact mapping;
- new obligation status or transition without an invariant update;
- new sweeper/worker without run-ledger and audit coverage;
- new Slack/reporting route without delivery ledger and retry behavior;
- future PRD/TRD missing an `Audit And Reconciliation` section.

## 7. Detection And Repair Classes

| Class | Detect | Auto-fix? | Rule |
| --- | --- | --- | --- |
| Missing derived obligation | Canonical fact and active rule imply an obligation, but no active row exists. | Yes | Insert/upsert with deterministic idempotency key and status event. |
| Duplicate active obligation | More than one active obligation exists for same tenant/rule/target/due key. | Sometimes | Supersede/cancel duplicates only if deterministic winner is provable; otherwise process exception. |
| Missing outbox event | Canonical transition committed but expected outbox row absent. | Sometimes | Create replay-safe repair event only when payload can be reconstructed exactly. |
| Stuck outbox/consumer/DLQ | Age, retry count, or dead-letter state breaches policy. | Sometimes | Reclaim/retry known transient states; poison requires operator repair/discard reason. |
| Missing projection/read-model row | Canonical truth exists, projection missing/stale. | Yes | Rebuild/upsert projection idempotently. |
| Missing notification intent | Due/missed/escalated condition exists, no durable notification intent. | Yes | Create intent with dedupe key; do not mark delivery successful. |
| Missing Slack delivery | Audit report row exists, Slack delivery absent/failed. | Yes | Retry delivery through notification adapter. |
| Missing owner/assignee | Work exists but no source-backed owner/backfill. | No | Create process exception; never invent owner. |
| Missing business source data | DOB, pregnancy date, source trust, shed/stage, proof, count, feed vector, or stock lot missing. | No | Fail closed; create source-data exception. |
| Illegal status transition | Row moved out of allowed state machine. | No direct rewrite | Preserve evidence; create repair workflow or corrective work item. |
| Terminal state mutation | Completed/missed/waived/canceled/superseded changed in place. | No | P0/P1 finding; corrective lineage, never silent rewrite. |
| Rule/config drift | Code, seed, rule DSL, migration, and docs disagree. | No | Block publish/apply until explicit owner decision. |
| Legacy parity gap | Legacy workflow signal has no Goat OS equivalent. | No direct runtime fix | Track as migration/cutover gap with owner and target module. |

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
- impact preview not generated or not reviewed before activation;
- new protocol category not listed in audit pack registry.

Auto-fix:

- rebuild derived scope-resolution rows;
- refresh protocol list/read projections;
- retry missing publish outbox only when payload can be reconstructed exactly.

Do not auto-fix:

- publish/retire rules;
- infer missing SOP binding;
- widen scope from park to tenant;
- invent approval or rule source evidence.

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

- create missing obligations from canonical fact plus active rule;
- reopen deferred obligation after recovery if deterministic;
- cancel open obligations after exit event;
- regenerate booster from accepted completion event;
- rebuild vaccination eligibility rollups and command-lens projections;
- create missing notification intent for due/missed/escalated state.

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
- UI shows action enabled when backend says disabled.

Auto-fix:

- create missing planned batch/task for due rows if deterministic;
- rebuild task/read-model projections;
- retry replay-safe proof fanout or projection update;
- create missing notification intent for proof pending, verification pending,
  rejected proof, or missed execution.

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

- reclaim stale relay leases;
- retry transient failed outbox rows;
- replay deduped event handlers when idempotency proves no duplicate effects;
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
- reminder/nudge/escalation policy threshold crossed without durable intent;
- notification request exhausted without operator visibility;
- sweeper scans unbounded windows or lacks tenant/date/status index shape;
- noisy tenant starves quiet tenants;
- Cloud Tasks contains work not reconstructable from Postgres.

Auto-fix:

- promote due rows by bounded indexed windows;
- create missing reminder/escalation intent with dedupe key;
- retry notification delivery through adapter circuit breaker;
- rebuild missed/due projections.

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

- rebuild audit read projections where source rows exist;
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

- rerun bounded projection recompute;
- rerun mismatch scan;
- refresh readiness evidence ledger;
- rebuild read projections.

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

- rerun feed readiness and generation preview;
- rebuild feed read-model buckets;
- supersede stale open Diff work when deterministic;
- retry notification intent for blocked/shortfall/rework states.

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

- replay accepted-intake handoff to create missing deterministic downstream
  obligations;
- cancel open obligations for rejected/exited animals;
- rebuild procurement/vaccination handoff projections.

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

- refresh roster coverage/read projections;
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

- retry safe mobile sync/outbox submissions;
- refresh bootstrap/admin UI contract cache;
- rebuild projections.

Do not auto-fix:

- mutate local client state as canonical truth;
- resolve same-key/different-payload conflict automatically.

Slack:

- P2 for UI contract drift.
- P1 when drift could cause unsafe/incorrect field action.

## 9. Future Feature Audit Packs

Each future feature must add its own pack before implementation is called done.
Minimum pack expectations:

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
- Slack delivery status: delivered

Critical:
1. P1 vaccination.birth_missing_obligation
   18 accepted animal-created facts had no matching vaccination obligation.
   Repair: 18 obligations created idempotently.
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
- Create initial audit packs for protocol, vaccination, execution, event spine,
  sweeper, notifications, operations audit, counts/shifting, feed readiness,
  procurement handoff, workforce, admin-web, and mobile sync.
- Add docs PRD/TRD template requirement: `Audit And Reconciliation`.

### Phase B - Read-only runner

- Implement `backend/cmd/kernel-audit` in report-only mode.
- Record `kernel_audit_runs` and `kernel_audit_findings`.
- Cover vaccination and event-spine invariants first.
- No auto-repair in this phase.

### Phase C - Deterministic repair

- Add repair allowlist for derived artifacts only:
  missing obligations, stale projections, missing notification intents, stale
  relay leases, and replay-safe outbox repair.
- Every repair writes `kernel_audit_repairs`, audit/history, and idempotency key.
- Same-key different-payload is a conflict, never a repair.

### Phase D - Slack delivery

- Add Slack delivery adapter behind notification/reporting port.
- Use Secret Manager for token/webhook.
- Route daily digest and P0/P1 alerts to `C0BGSJDD420`.
- Add delivery retry/exhausted visibility.

### Phase E - Future-feature enforcement

- Add CI checks for new event/category/status/worker/projection without audit
  pack update.
- Add codegen or manifest validation so audit runner picks up new packs.
- Require audit-pack status in phase closeout.

### Phase F - Scale certification

- Prove audit scans are tenant/date/status/cursor bounded.
- Validate query plans for widest audit checks.
- Include noisy-tenant fairness in audit workers.
- Add high-scale report rows for audit runner throughput, lag, repairs,
  Slack delivery, and projection parity.

## 13. Query And Worker Rules

The audit runner must follow the same high-scale rules as the kernel:

- no full-herd scans;
- no `OFFSET` on hot tables;
- keyset pagination by tenant/scope/date/status/cursor;
- bounded worker pool;
- one page/batch per transaction;
- deterministic idempotency keys;
- stale-run heartbeat and reclaim;
- tenant-fair claim order;
- query-plan validation for hot checks;
- clear freshness envelope for report data.

An audit finding is not allowed to be based on stale or partial data without
saying so. Reports must include input windows, projection freshness, source
watermarks, and skipped/deferred scan reasons.

## 14. Non-Goals

- Do not resurrect legacy Slack/App Script as Goat OS runtime.
- Do not use Slack as canonical truth.
- Do not let AI agents apply business fixes.
- Do not create one private audit cron per module.
- Do not write repairs directly to another module's tables outside owning
  service/port boundaries.
- Do not turn daily digest into a noisy dump of every P3 hygiene issue.

## 15. Acceptance Criteria

The plan is implemented when:

1. Four scheduled audit sweeps run daily with durable run rows.
2. Daily Slack digest reaches `#goatos-audit`.
3. P0/P1 findings alert promptly and link to Operations Audit/Action Center.
4. Vaccination birth/procurement/completion/exit/defer/recovery cases reconcile
   expected-vs-actual obligations and projections.
5. Event spine, outbox, DLQ, sweepers, notification intent, and projection
   freshness are covered.
6. Auto-repair is restricted to deterministic derived artifacts and fully
   audited.
7. Missing source truth creates process exceptions, not fabricated data.
8. Feed Direction and every future feature can register an audit pack before it
   ships.
9. CI blocks new kernel events/categories/statuses/workers without audit
   coverage.
10. High-scale validation proves audit scans are bounded and tenant-fair.
