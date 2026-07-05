# Goat OS High-Scale Kernel Validation Plan

**Status:** Active plan
**Date:** 2026-07-05
**Applies to:** Preventive Care (PC) vaccination now; every future protocol-driven domain later.

This plan locks how Goat OS proves the operational kernel before calling a slice
production-ready at 1-5 lakh animals per park and 1-5M animal operations overall.
The kernel is generic: vaccination is the first heavy user, but the same design
must support feed direction, breeding, procurement, parks, HR, inventory,
critical animal actions, and future protocol-driven workflows.

## 1. Non-Negotiable Architecture

Every domain feature must plug into this shared chain:

```text
business event
  -> canonical DB transaction
  -> idempotency + audit + outbox
  -> durable relay to Pub/Sub/event bus
  -> trigger evaluation
  -> bounded obligation generation
  -> sweeper / scheduler / batch planner
  -> SOP/proof execution
  -> verification/completion
  -> notification/escalation/acknowledgement
  -> process read models
  -> Control Tower / Action Center / Calendar / Protocol Adherence
```

Postgres canonical state is the source of truth. Pub/Sub, Cloud Tasks, workers,
and notification channels are delivery/execution layers. If a message is lost,
delayed, retried, or delivered twice, the kernel must be able to reconstruct the
right answer from Postgres and idempotency keys.

## 2. Scale Model

The design target is not "small farm demo" scale.

| Dimension | Required shape |
| --- | --- |
| Animals per park | 1-5 lakh |
| Operations to tolerate | 1-5M generated, swept, retried, completed, and projected operations; target-evaluation-only runs are labeled separately and do not certify the full chain |
| Animals per shed/tag | 500-1000 typical; skewed parks/sheds must be tested |
| Rule resolution | Cache active protocol versions per `(tenant, park, category, as_of)` within a run |
| Animal scanning | Keyset pages, normally 500-1000 animals per page |
| Worker concurrency | Bounded worker pool, not one goroutine per animal |
| Transaction size | One page or one claimed batch at a time |
| Reads | Keyset/paginated and index-backed; no broad OFFSET/full-sort hot path |
| Writes | Deterministic idempotency keys and conflict-safe inserts/updates |
| Projections | Hot dashboards use projection/counter tables or capped estimates |

Correct concurrency model:

```text
page size = 500-1000 animals
worker pool = configured cap, e.g. 4/8/16 workers depending on DB pool and CPU
each worker owns one page/batch at a time
each page/batch has a small transaction boundary
shared caches are precomputed or concurrency-safe
```

Never run one goroutine per animal. Never load a whole park or whole tenant into
memory. Never hold one giant transaction across all animals.

## 3. Current Kernel Guarantees To Preserve

These are already the intended shape in the current codebase and must not be
regressed:

- Protocol publish uses backend-owned transactions, idempotency keys, audit, and
  outbox events. The frontend is never the source of truth for activation.
- `vaccination.matrix` publish generates one scoped immutable active version per
  company or park scope, retires overlapping versions, and emits publish/retire
  outbox events.
- `outbox_messages` is the durable handoff before Pub/Sub. `outbox-relay` claims
  bounded rows, validates envelopes, publishes, retries with backoff, and sends
  exhausted retry rows to dead-letter status.
- Staging/production must use `GOATOS_OUTBOX_PUBLISHER=pubsub`; logging/eventbus
  modes are local-only and require explicit non-durable opt-in.
- Versioned publish/manual vaccination generation has durable
  `vaccination_generation_runs` rows, run idempotency keys, heartbeat,
  completed/failed state, and stale-run reclaim. The safe no-version
  effective-cohort CLI/backfill path is bounded and cached, but it is not yet
  wrapped in durable run rows.
- Existing-cohort generation resolves active vaccination versions with a
  per-run park cache instead of querying once per animal.
- Obligation generation uses deterministic keys and conflict-safe writes so
  replayed events do not duplicate obligations.
- The sweeper/batch layer is separate from rule generation: first derive due
  work, then group it into operational drives/tasks.

## 4. Required Local Docker Validation Before Claiming Kernel Ready

Every high-risk kernel change must pass a local Docker chain that exercises the
same architectural boundaries as cloud. Unit tests alone are not enough. The
result must be a committed or attached pass/fail report with command output,
seed shape, query-plan evidence, and failed-case evidence.

### 4.1 Local Stack

Run locally with:

```text
Docker Postgres
API/backend
admin-web where the workflow has a UI surface
outbox-relay
domain event consumer or in-process eventbus rehearsal
obligation sweeper
calendar/read-model refresh where relevant
notification/alert stub adapters
```

For local Pub/Sub publisher/subscriber behavior, use the GCP Pub/Sub emulator or
a test project topic/subscription. For local-only business-chain smoke tests,
eventbus mode is allowed only when the test explicitly states it is not proving
durable cloud egress.

Emulator proof is not cloud-readiness proof. Real GCP readiness must separately
prove topic/subscription wiring, IAM, ack deadline, retry policy, and DLQ
behavior in `goatos-stg` or an explicitly approved test project.

Local validation is allowed to run at reduced row count when developer machines
cannot hold full scale, but the plan must also define the staging-scale command
and thresholds. A small local run proves behavior; the staging run proves scale.

### 4.2 Must-Prove Failure Cases

| Failure | Expected behavior |
| --- | --- |
| API request fails before commit | No canonical state, no outbox event; retry creates the change once |
| API commits but HTTP response dies | Retrying same idempotency key returns/replays success |
| Same idempotency key with different payload | Backend rejects conflict |
| Outbox relay crashes after claiming rows | Stale publishing lease reclaims rows and retries |
| Pub/Sub publish fails transiently | Outbox row schedules retry with backoff |
| Pub/Sub publish fails permanently | Outbox row becomes failed/dead-letter with operator visibility |
| Pub/Sub delivers duplicate event | Consumer/generation idempotency prevents duplicate work |
| Generation crashes mid-page or mid-run | Current versioned runs retry by replaying idempotently and creating only missing work; cursor/page resume must be added before parallel high-scale certification |
| Worker times out during one drain stage | Later run continues; no giant transaction rollback |
| Stock/proof/verification failure | Process exception is visible; no silent log-only failure |
| Read model refresh fails | Canonical truth remains queryable; refresh can rerun idempotently |

### 4.3 Must-Prove Scale Cases

Seed realistic skew, not uniform fake rows:

- At least 2 tenants, including one noisy tenant, so outbox/sweeper fairness is
  tested and one tenant cannot starve another.
- At least one tenant with multiple parks: one 1-5 lakh-animal park, at least
  one smaller park, one active park override, and one tenant-default park with no
  override. Vaccination-slice certification proves scoped default/override
  behavior for `vaccination.matrix`; multi-domain kernel certification later adds
  a second operational category so resolution by `(tenant, park, category, as_of)`
  proves generic scope behavior.
- 500-1000 animals per shed/tag, plus a few high-skew sheds.
- Mixed goat/sheep animals.
- Sick, ICU, quarantine, warm-up, pregnancy month 4/5, lactating, sold/dead,
  shifted shed, and recovered animals.
- Trusted procurement-holding vaccination history and untrusted outside history.
- Overlapping due windows where batching may wait up to 7 days once, but never
  beyond the medical safety window.

Acceptance checks:

- Effective protocol version lookup count is proportional to parks, not animals.
- Scope resolution proves tenant-default and park-override behavior in the same
  run; park overrides apply only to their park/category and tenant-default rules
  still apply elsewhere. Multi-domain certification must also prove
  mixed-category behavior in the same seed.
- Goat/animal pages are keyseted and bounded.
- History/proof/compatibility reads are bulk per page, not N+1 per animal.
- Obligation writes are page-bounded and idempotent.
- Sweeper claims indexed windows and does not scan whole tenant state.
- Control Tower / Action Center / Calendar / Protocol Adherence read paths use
  keyset/cursor or projection tables for broad 1M views.
- `validate-sqlc-plans` or equivalent per-push plan checks prove index
  reachability for every hot list/sweeper query shape. Staging-scale
  certification must also run scaled `EXPLAIN (ANALYZE, BUFFERS)` without
  disabling sequential scans, after seed load and `ANALYZE`, to prove the
  planner chooses the intended index/projection path at real row counts.
- Outbox, sweeper, generation, projection, and notification workers expose
  queue depth, retry count, dead-letter count, heartbeat age, batch duration,
  and rows-per-second metrics or logs.
- Multi-tenant outbox and sweeper workers either use tenant-fair claiming or
  prove that a high-volume tenant does not starve lower-volume tenants.
- Expected-vs-actual reconciliation matches the seeded protocol matrix:
  generated + deferred + reopened + suppressed_by_trusted_history +
  skipped_no_due_date + ineligible + canceled outcomes reconcile by tenant,
  park, category, protocol version, rule, and target cohort.

### 4.4 Minimum Staging Threshold Floor

Every staging-scale report must name the exact command before it runs and list
the canonical environment shape. Certification runs must use the fixed
`goatos-stg` benchmark baseline profile from
`docs/runbooks/google-cloud-environments.md`, currently
`goatos-stg-1m-benchmark-v1`. The report must include profile id, Cloud SQL
tier, CPU/memory, disk type/size/autoscaling, replicas, app/worker replicas, DB
pool sizes, Pub/Sub topic/subscription/DLQ config, worker concurrency, repo git
SHA, migration head/checksum digest, seed generator/config identity, input
fixture checksum manifest, and loaded dataset checksum/row-count manifest. A run
on a different or larger shape, schema head, seed generator, fixture set, or
loaded dataset is a stress exploration unless a new benchmark profile is
committed first.

The canonical vaccination-slice command target to add before certifying the
current Preventive Care (PC) vaccination slice is:

```text
make load-test-kernel-stg \
  GOATOS_ENV=stg \
  KERNEL_CERTIFICATION_SCOPE=vaccination_slice \
  KERNEL_CERTIFICATION_LEVEL=full_chain \
  KERNEL_SCALE_OPERATIONS=1000000 \
  KERNEL_SCALE_TENANTS=2 \
  KERNEL_SCALE_PARKS_PER_NOISY_TENANT=3 \
  KERNEL_SCALE_NOISY_TENANT_SHARE=0.80 \
  KERNEL_SCALE_INCLUDE_PARK_OVERRIDE=1 \
  KERNEL_SCALE_CATEGORIES=vaccination \
  KERNEL_SCALE_FAIL_ON_THRESHOLD=1
```

Multi-domain kernel certification is separate and later:

```text
make load-test-kernel-stg \
  GOATOS_ENV=stg \
  KERNEL_CERTIFICATION_SCOPE=multi_domain_kernel \
  KERNEL_CERTIFICATION_LEVEL=full_chain \
  KERNEL_SCALE_OPERATIONS=1000000 \
  KERNEL_SCALE_TENANTS=2 \
  KERNEL_SCALE_PARKS_PER_NOISY_TENANT=3 \
  KERNEL_SCALE_NOISY_TENANT_SHARE=0.80 \
  KERNEL_SCALE_INCLUDE_PARK_OVERRIDE=1 \
  KERNEL_SCALE_CATEGORIES=vaccination,<second_operational_category> \
  KERNEL_SCALE_FAIL_ON_THRESHOLD=1
```

The second category must already have operational generation, obligation writes,
event delivery, sweeper/batch behavior, notification/escalation policy,
projection/read APIs, and tests. Feed Direction is not required for
vaccination-slice certification while the Feed Direction TRD still marks
generation, obligation creation, HTTP/OpenAPI, consumers/schedulers, stock, and
notification routing as not operational.

Early generator-only rehearsals may set
`KERNEL_CERTIFICATION_LEVEL=target_eval`. That label proves page/cache/query
shape for target evaluation only. It cannot be described as 1M full-chain
certification, production readiness, or end-to-end kernel scale proof.

If the command name changes, the report must show the equivalent invocation and
prove that it exercises the same chain: generation, outbox relay, domain
consumer, sweeper, notification/escalation planning, projection refresh, and
hot read APIs. Local reduced-row runs can be marked `local behavior proof` only;
they cannot satisfy this staging floor.

The 1,000,000 target is not a summed "operation attempts" bucket. The report must
list each stage counter separately so cheap target evaluations cannot hide an
untested kernel stage. A `target_eval` certification only needs the first row. A
`full_chain` certification must meet every minimum below or fail with `not
implemented yet`.

| Stage counter | What counts | 1M target-eval minimum | 1M full-chain minimum |
| --- | --- | --- | --- |
| Target evaluation | One target/rule evaluation against the active scoped protocol. | >= 1,000,000 evaluations. | >= 1,000,000 evaluations by tenant, park, category, protocol version, rule, and cohort. |
| Obligation write | One durable obligation insert, update, reopen, cancel, defer, suppress, or no-op conflict. | Report only. | >= 250,000 attempted writes and >= 100,000 changed durable rows, with duplicate business effects = 0 and idempotent replay no-ops counted separately. |
| Outbox publish | One outbox row published and marked through the configured relay path. | Report only. | >= 100,000 publish attempts through the configured relay/Pub/Sub path, including success, retry, failed/DLQ, oldest unsent age, and publish rows/sec. |
| Consumer handle | One delivered event handled by a domain consumer or approved local equivalent. | Report only. | >= 100,000 delivered event handles, including at least 1% duplicate/replay deliveries that produce zero duplicate effects. |
| Sweeper claim | One due/missed/retryable work row claimed or intentionally skipped by a bounded worker. | Report only. | >= 100,000 due/missed/retryable claims or intentional skips, with stale reclaim and tenant-fair lag reported. |
| Batch create/attach | One batch/work unit created or updated, plus the obligation rows attached to it. | Report only. | >= 1,000 batch/work units and >= 100,000 attached obligation rows, with zero double-claim effects and stock reservation/release impacts reported. |
| Completion/proof | One accepted, rejected, reworked, duplicate, or failed completion/proof action. | Report only. | >= 100,000 completion/proof attempts across accepted, rejected/rework, and replay paths, with stock consume/release reconciliation. |
| Notification intent | One durable reminder, nudge, escalation, exhausted-delivery, or suppression intent row. | Report only. | >= 50,000 notification/escalation intent rows, including >= 1,000 escalation or exhausted-delivery cases when the slice declares escalation policy. |
| Retry/DLQ injection | One deliberately retried or poison delivery/work item. | Report only. | >= 10,000 retryable attempts and >= 100 poison/DLQ items, all audited with replay/discard reason and zero duplicate business effects. |
| Projection update | One projection row upsert, rebuild row, or invalidation processed for command lenses. | Report only. | >= 250,000 projection row updates or a full projection rebuild over the 1M seed; canonical/projection count parity must pass after refresh. |
| Hot read | One API/read-model request against the loaded seed. | Report only. | >= 10,000 hot read requests across Control Tower, Action Center, Calendar, Protocol Adherence, and detail/list APIs, with p95/p99 and scaled query plans reported. |

Minimum pass/fail thresholds:

| Signal | Pass/fail floor |
| --- | --- |
| Run size, scope, and skew | Vaccination-slice certification requires at least 1,000,000 vaccination target evaluations across at least 2 tenants. One noisy tenant carries about 80% of generated load and contains at least 3 parks: one 1-5 lakh-animal park, one park with an active vaccination override, and one tenant-default park. Full-chain certification additionally requires every stage minimum above. Multi-domain kernel certification is separate and requires at least two operational categories so resolver cache keys prove `(tenant, park, category, as_of)` across domains. |
| Generation duration | 1,000,000 target evaluations complete in 60 minutes or less on the committed `goatos-stg-1m-benchmark-v1` staging profile. A later bounded-worker certification target should tighten this to 30 minutes or less. |
| Generation throughput | Sustained throughput after seed warm-up is at least 300 target evaluations/sec and at least 150 durable obligation writes/sec, with duplicate obligations = 0. |
| Correctness reconciliation | For every seeded matrix cell, `generated + deferred + reopened + suppressed_by_trusted_history + skipped_no_due_date + ineligible + canceled = expected targets` by tenant, park, category, protocol version, rule, and cohort. Missing obligations = 0, duplicate business effects = 0, and same-key replay changes 0 rows. Any residual or future bucket not named here must be 0 unless the report documents the reason and owner-approved acceptance criteria. |
| Sweeper and batch throughput | Due-window claiming, missed marking, and batch planning sustain at least 500 rows/sec combined, with bounded transactions and no growing memory profile. |
| Outbox/Pub/Sub throughput and lag | Outbox relay sustains at least 200 published+marked messages/sec against Pub/Sub; oldest unsent event p95 is less than 60s during steady load and never exceeds 300s during burst load. Pub/Sub oldest unacked p95 is less than 60s and backlog stops growing within 10 minutes after load stops. |
| Tenant fairness | Under the noisy-tenant seed, every tenant with ready work receives claim progress within 5 minutes, and the quiet tenant p95 outbox/sweeper lag is no more than 2x the noisy tenant p95 lag. |
| API and hot query latency | Mutating API paths p95 < 800ms and p99 < 1500ms excluding external provider latency. Keyset list/projection APIs p95 < 1200ms and p99 < 2500ms. Hot DB queries p95 < 250ms and p99 < 1000ms, with scaled `EXPLAIN (ANALYZE, BUFFERS)` proving index/projection-backed planner choice without `enable_seqscan=off`. |
| DB CPU, I/O, locks, and pool pressure | Cloud SQL CPU 15-minute average <= 70% and p95 <= 85%; storage I/O p95 <= 80%; connection pool p95 <= 80% of max; lock waits p99 < 250ms; no broad herd sequential scan appears in hot-path plans. |
| Retry, DLQ, and replay | With no injected provider failures, retry rate < 0.1% and DLQ count = 0. With failure injection, only injected poison reaches DLQ; replay/discard actions are 100% audited and idempotent; duplicate business effects = 0. |
| Notification/escalation freshness | Reminder/escalation intent rows are created within 60s p95 and 300s p99 after due/missed state crosses the policy threshold; exhausted delivery rows become operator-visible within 2 minutes. |
| Dashboard projection staleness and accuracy | Process projections used by Control Tower, Action Center, Calendar, and Protocol Adherence are fresh within 60s p95 and 300s p99 after canonical writes. Any stale response must expose the freshness envelope. A full projection rebuild over the 1M-operation seed completes within 30 minutes. Projection integer counts for due, overdue, missed, deferred, suppressed, canceled, completed, and exception buckets match canonical Postgres counts exactly after refresh; declared approximate/rate metrics may differ by at most 0.1% and must be labeled. |
| Idempotency and run-ledger growth | Idempotency and run/progress tables keep primary-key/index probes within the latency floor above, and the report states the retention or partition policy used for replay safety and index-bloat control. |

The report must include measured value, threshold, pass/fail, and evidence link
for every row above. `Not implemented yet` is acceptable in planning reports, but
it fails high-scale certification.

## 5. Bounded Goroutine Plan

Parallelism is allowed and wanted, but only inside explicit limits.

Recommended worker shape:

```text
producer: lists animal pages by keyset cursor
workers: fixed pool, e.g. 4-16 workers
worker input: one page/batch
worker output: counts + errors + emitted obligation writes
shared cache: precomputed per park/scope, or protected read-only map
DB pool: larger than worker count + API/system overhead
```

Rules:

- No goroutine per animal.
- No unbounded channel fed by all animals.
- No shared mutable cache without synchronization.
- Each page/batch must be independently retryable.
- A failed page must record enough cursor/context to replay before the worker
  pool is used for high-scale certification. Current single-thread generation
  can replay from the start because writes are deterministic and idempotent;
  parallel page workers require cursor/shard state.
- Worker count must be configuration-driven and load-tested against DB pool,
  CPU, and lock contention.

## 6. Circuit Breaker, Retry, DLQ, And Replay Policy

Every adapter that talks to a non-Postgres system must sit behind a port and
declare its retry behavior.

| Adapter kind | Required behavior |
| --- | --- |
| Pub/Sub publisher | retryable/permanent errors, bounded attempts, DLQ/dead-letter, metrics |
| Pub/Sub subscriber | max outstanding messages, ack only after durable handling, idempotent consumer |
| Cloud Tasks / reminder sender | reconstructable from Postgres, no canonical truth in task payload only |
| Notification sender | provider circuit breaker, dead-letter/failed delivery rows, retry budget |
| Inventory/stock mutation | canonical DB transaction or idempotent command; no duplicate consume on retry |
| Projection refresh | idempotent rebuild/merge, safe rerun after failure |

Retries must use bounded exponential backoff; jitter is required for production
adapters before high-scale certification. Circuit breakers must trip noisy
downstream adapters without corrupting canonical state. DLQ replay and discard
must require operator reason, idempotency key, and audit row.

Circuit breakers are not allowed to drop canonical work. When an adapter is
open/tripped, the kernel records pending/retryable work in Postgres and exposes
it through process integrity surfaces.

Notification, escalation, delivery, DLQ replay, and progress recording are
generic kernel mechanics. A future domain can supply policy and vocabulary, but
it must not build a private retry, replay, delivery, or escalation engine.

## 7. Generic Composition Model

Future domains must reuse the kernel by composition, not by copy-pasting a new
mini-engine.

| Component | Generic responsibility | Domain supplies |
| --- | --- | --- |
| `EventIntake` | validate command, reserve idempotency, write canonical event/outbox | command schema and authority |
| `ProtocolResolver` | active version lookup by category/scope/effective date | category scope policy |
| `TriggerEvaluator` | decide whether obligations/tasks are due | rule DSL and selectors |
| `TargetPager` | keyset page targets | domain target query |
| `EvidenceReader` | bulk fetch history/proof/suppression facts | domain evidence tables |
| `ObligationWriter` | deterministic key, conflict-safe write | target/rule/due payload |
| `RunLedger` / `ProgressRecorder` | record run id, attempt, heartbeat, cursor/page/shard progress, stale reclaim, completion, and failure state | run type, scope, cursor semantics, retry policy, completion criteria |
| `WorkClaimer` | claim bounded work rows with tenant-fair ordering, `SKIP LOCKED`/lease-safe semantics, and stale-claim recovery | work table, claim query, indexes, status transitions, scope dimensions |
| `LeaseManager` | start, heartbeat, expire, reclaim, and release leases for jobs, claims, deliveries, and replay actions | lease duration, owner identity, takeover policy |
| `BackpressurePolicy` | cap worker concurrency, batch size, polling cadence, and provider calls from queue depth, DB pressure, and adapter health | urgency, SLA, priority, and domain-specific pause/degrade rules |
| `BatchPlanner` | group compatible obligations into executable work | domain compatibility/scoring |
| `ExecutionAdapter` | create SOP task and proof requirements | SOP/form/proof policy |
| `CompletionAdapter` | verify proof, consume stock, mark complete | domain completion table |
| `NotificationPlanner` | create durable reminder and nudge intents from due, blocked, missed, failed, or verification-pending state | recipient chain, timing policy, message semantics, suppression rules |
| `EscalationPlanner` | advance escalation level, acknowledgement, ownership, resolution, and exception state | role ladder, SLA thresholds, acknowledgement/resolution policy |
| `DeliveryGateway` | deliver notification/escalation intents through replaceable channels with retry budget, circuit breaker, and delivery ledger | channel selection, template/content payload, vendor adapter config |
| `DLQReplayController` | list, replay, repair, discard, and audit poison messages or failed work without duplicate effects | operator permissions, repair payload schema, replay safety rules |
| `ProjectionUpdater` | update read models | dashboard/process state contract |

Domain logic belongs in small policy objects/functions plugged into these
interfaces. Cross-cutting mechanics stay in the kernel.

Tenant fairness is part of the shared kernel contract, not a one-off outbox or
sweeper trick. `WorkClaimer`, `LeaseManager`, and `BackpressurePolicy` are the
ports every high-volume worker should reuse so noisy-neighbor behavior can be
proved once and applied across domains.

## 8. Known Implementation Upgrades To Plan/Track

This plan does not claim every item below is already implemented. These are the
next kernel hardening items to keep explicit:

1. **Page-bounded batched missed-state writes.** Keep the existing page boundary
   but replace per-row repeated inserts/updates with multi-row statements per
   page where safe.
2. **Per-stage timeout budgets.** Long sweepers/read-model refreshes should use
   separate bounded contexts per drain stage/page so one slow stage does not
   starve later stages.
3. **Explicit Pub/Sub subscriber flow control.** Consumers should configure max
   outstanding messages/bytes and ack deadlines instead of relying on defaults.
4. **Durable effective-cohort generation runs.** Wrap the no-version
   `GenerateEffectiveForAllGoats` CLI/backfill path in durable run rows,
   heartbeat, replay status, and idempotency just like versioned publish/manual
   generation.
5. **Cursor/page resume for parallel generation.** Persist page cursor or shard
   context so a crashed parallel worker can replay only its page/range instead
   of requiring full-run replay.
6. **Notification circuit breaker implementation.** The durable
   `notification_requests` retry/exhausted queue exists; add
   open/half-open/closed provider state, cooldowns, metrics, pending-work
   visibility, and recovery tests for notification/alert adapters.
7. **Backoff jitter.** Add jitter to outbox, notification, and future adapter
   retries where currently deterministic exponential backoff is used.
8. **Tenant-fair work claiming ports.** Implement or prove reusable
   `WorkClaimer`, `LeaseManager`, and `BackpressurePolicy` behavior so outbox,
   sweeper, projection, notification, and future workers cannot let a noisy
   tenant monopolize global capacity.
9. **Projection decision for 1M dashboards.** Broad CT/PA/Calendar/Action
   Center summaries need projection tables or capped estimates, not exact
   full-scan aggregates on every page.
10. **Parallel generation worker pool.** Add bounded page-level workers only
   after the Docker chain proves single-thread correctness and plan guards cover
   the hot queries.
11. **Failure injection tests.** Add scripted cases for relay crash, duplicate
   Pub/Sub delivery, generation mid-run failure, stale heartbeat reclaim, DLQ
   replay, and idempotency conflict.
12. **Load-test report.** Keep a committed report/checklist with seed shape,
   Section 4.4 thresholds, commands, pass/fail output, query plans, and observed
   bottlenecks.
13. **Reusable notification/escalation/replay ports.** Extract or standardize
   `NotificationPlanner`, `EscalationPlanner`, `DeliveryGateway`,
   `DLQReplayController`, and `RunLedger` / `ProgressRecorder` contracts before
   future domains copy those mechanics.
14. **Idempotency retention and index-bloat decision.** Decide and document the
   replay window, partition/retention policy, and expired-replay behavior for
   `idempotency_keys` so replay safety does not silently become unbounded primary
   key/index growth at 1M+ operations.
15. **Hot-table migration lock safety.** Late indexes on already-populated hot
   tables such as `obligation_instances`, `goats`, outbox, notification, audit,
   projection, completion, dedupe, run-ledger, movement/event, proof, and status
   event tables must use non-blocking patterns: `CREATE INDEX CONCURRENTLY` /
   `DROP INDEX CONCURRENTLY`, with `-- +goose NO TRANSACTION` in the relevant
   goose `Up`/`Down` section. Do not add direct `ALTER TABLE [IF EXISTS] ...
   ADD CONSTRAINT ... UNIQUE`, `PRIMARY KEY`, or `EXCLUDE` constraints to
   populated hot tables. For uniqueness or primary keys, build
   `CREATE UNIQUE INDEX CONCURRENTLY` first and attach with `ALTER TABLE ... ADD
   CONSTRAINT ... UNIQUE/PRIMARY KEY USING INDEX`; exclusion constraints need a
   reviewed no-lock rollout path. Do not drop hot-table constraints directly
   with `ALTER TABLE ... DROP CONSTRAINT`; index-backed constraints can remove
   the backing index and all hot-table constraint drops need an explicitly
   reviewed no-lock rollout. The hot-index guard must warn on every historical
   pre-guard late hot-table index/constraint lock risk and fail future ones. A
   canonical 1M benchmark database must apply all historical warning migrations
   before loading 1M data; any already-populated 1M database still below current
   head needs an explicit no-lock rollout path before certification.
16. **Durable verify-fanout path.** Route vaccination verify accepted/rejected
   fanout through the durable outbox or document and test a replay-safe repair
   path. Direct synchronous `eventbus.Publish` fanout is not enough for
   high-scale certification.
17. **Scaled planner-choice guard.** Keep per-push `validate-sqlc-plans` as an
   index-reachability guard, but add a seeded staging/CI path that runs
   `ANALYZE` and `EXPLAIN (ANALYZE, BUFFERS)` without `enable_seqscan=off` for
   the heaviest hot queries.

## 9. Minimum E2E Checklist

A kernel slice is not closed until the report lists these cases and their result:

- publish company version, publish park override, retire old active version
- replay publish with same idempotency key
- reject conflicting idempotency payload
- outbox relay success path
- outbox relay transient failure retry path
- outbox dead-letter + replay path
- Pub/Sub/eventbus duplicate event path
- real GCP Pub/Sub topic/subscription/IAM/ack/DLQ proof before cloud readiness
- multi-tenant noisy-neighbor outbox/sweeper fairness path
- generation success, completed replay, stale running retry, failed retry
- effective-cohort CLI/backfill generation durable run path
- cursor/page resume or explicit full-replay acceptance for non-parallel paths
- bounded page processing at realistic page size
- vaccination-slice multi-park scoped generation: tenant-default version, park
  override, and override retirement/inheritance in one run
- multi-domain scoped generation, only for `multi_domain_kernel`
  certification: at least two operational protocol categories in one run
- missed/recovered/deferred animal path
- shift re-scope path: shifted animals move open same-vaccine work to the
  destination shed/park scope, detach or replan affected batches, and leave audit
  evidence for the prior scope
- death/sale/exit cancel path: exited animals cancel open scheduled/due/overdue
  work, release or reconcile stock reservations, and emit status/audit evidence
- no exited animal remains overdue in Action Center, Calendar, Protocol
  Adherence, Control Tower, or projection rows after refresh
- sweeper creates batch/drive/task
- notification circuit breaker open/half-open/closed behavior and pending visibility
- proof submission accepted, rejected/rework, and duplicate submit
- inventory reserve/consume/release idempotency
- read model refresh and UI surface reflects process state
- broad read/list query plan is index-backed or projection-backed

The E2E report must say `passed`, `failed`, or `not implemented yet` per item.
No silent blanks.

## 10. Current Feedback Triage

Latest architecture feedback is classified as:

| Feedback | Status |
| --- | --- |
| Effective vaccination versions queried once per goat | Stale; current generation has per-run park cache and test coverage. |
| `ListUnbatchedDueForVersion` lacks version index/plan guard | Fixed by the current tail migration/plan guard; preserve it. |
| Outbox retry/DLQ unclear | Real requirement; this plan makes local relay retry/DLQ validation mandatory. |
| Need goroutine per batch/page | Correct direction, but only as bounded worker pool after single-thread Docker proof. |
| "Doesn't matter how many" batching language | Retired; the rule is bounded pages, bounded workers, bounded transactions. |
| Kernel must extend beyond vaccination | Correct; composition model above is the target. |
| Durable generation exists for every path | Not yet; versioned publish/manual paths have run rows, effective-cohort CLI/backfill path needs an upgrade. |
| Cursor resume already exists | Not yet; current safe behavior is idempotent replay, with cursor/page resume required before parallel certification. |
| Pub/Sub emulator proves cloud readiness | No; emulator proves local behavior only. Real topic/subscription/IAM/DLQ proof is separate. |
| Concrete 1M pass/fail thresholds are optional | No; Section 4.4 defines the minimum staging threshold floor and every report must list measured value vs threshold. |
| Tenant fairness can live separately in each worker | No; fairness belongs in reusable `WorkClaimer`, `LeaseManager`, and `BackpressurePolicy` ports. |
| Notification/escalation/replay can be copied per domain | No; planners, delivery gateways, DLQ replay, and run/progress ledgers are shared kernel pieces with domain policy plugged in. |
| Idempotency rows can grow forever without an explicit decision | Not accepted by default; retention, partitioning, or permanent-replay tradeoffs must be documented and measured before high-scale certification. |
| One huge park proves scoped override behavior | No; the scale seed must include multiple parks in one tenant, an active park override, and a tenant-default park. Mixed categories are required for the separate multi-domain certification. |
| Performance thresholds are enough for certification | No; Section 4.4 requires expected-vs-actual reconciliation against seeded protocol matrix outcomes and canonical/projection count agreement. |
| Sold/dead/shifted seed rows are enough | No; the E2E checklist requires named shift re-scope, death/sale/exit cancel, stock reconciliation, and no-exited-animal-overdue proof. |
| Vaccination certification must include Feed Direction | No; vaccination-slice certification runs `vaccination` only. Multi-domain certification is separate and may use Feed Direction only after it is operational. |
| 1M operation attempts can mean any cheap counter | No; Section 4.4 defines separate stage counters for target evaluation, obligation write, outbox publish, consumer handle, sweeper claim, batch create, notification intent, projection update, and hot read. |
| Generation reconciliation can ignore residual buckets | No; reconciliation includes `reopened` and `skipped_no_due_date`, and any unnamed residual bucket must be zero or explicitly accepted with a reason. |
| 1M target evaluations prove the full chain | No; Section 4.4 separates `target_eval` from `full_chain` and defines minimum downstream stage volumes. |
| Mixed categories are required to close the vaccination slice | No; vaccination-slice E2E proves tenant-default and park-override behavior for vaccination. Mixed categories are required only for `multi_domain_kernel` certification. |
| Staging scale can use any larger DB/schema/data shape | No; certification is tied to the committed `goatos-stg-1m-benchmark-v1` benchmark profile. Changing infra shape, migration/schema identity, seed generator/config, fixture checksum, or loaded dataset checksum creates a new profile id. |
| Per-push plan guards prove planner choice at scale | Not by themselves; they prove index reachability. Scaled planner choice requires seeded `ANALYZE` plus `EXPLAIN (ANALYZE, BUFFERS)` without disabling sequential scans. |
| Late hot-table indexes or unique/primary-key/exclusion constraints can be plain blocking DDL | No; future late indexes/drops on hot tables require concurrent/no-transaction patterns, and future direct hot-table `UNIQUE`, `PRIMARY KEY`, `EXCLUDE`, or `DROP CONSTRAINT` lock risks are rejected unless uniqueness/primary-key constraints are attached with `USING INDEX` or the drop has an explicitly reviewed no-lock rollout. The guard reports all historical pre-guard hot-table warnings, and the 1M baseline must apply them before loading benchmark-scale data or use a no-lock rollout path. |
| Verify fanout can bypass the durable outbox | Not for certification; verify accepted/rejected fanout must be durable or have a tested replay-safe repair path. |
