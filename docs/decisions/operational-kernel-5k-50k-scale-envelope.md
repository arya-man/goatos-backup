# ADR: Operational kernel for the 5k-to-50k scale envelope

Status: accepted product direction; documentation complete, implementation pending.

Date: 2026-07-14

Decision owner: Goat OS product owner

Baseline implementation: commit `0819fa3327c880bf9b92710cc0839acd504ef592`
on `fix/ledger-closure-20260712`. The exact split-worker topology is also
captured below so it remains reconstructable after the infrastructure is
removed.

## Decision

Goat OS will optimize its first operational-kernel deployment for:

- approximately 5,000 animals today;
- up to 50,000 animals within the next year; and
- the obligations, events, batches, SOP tasks, notifications, history, and read
  paths required by that population.

The current fleet of independently scheduled Cloud Run Jobs is not the target
runtime for this envelope. It will be replaced by one modular kernel worker
process next to the API and Postgres.

This decision changes the active deployment scale target. One-million-animal
deployment topology is future work, not a present release requirement. Existing
one-million-scale documents remain useful research and regression material, but
they do not justify separate services, queues, schedules, partitions, or
projectors until measured workload requires them.

All data in the current environment is test data. No existing projection row,
generated obligation, notification, or benchmark row needs to survive this
restructure. The environment may be dropped and reseeded from reviewed source
fixtures after the new kernel is ready.

The current operational-kernel projection tables are also outside the target
5k-to-50k architecture. APIs will read canonical tables through bounded,
indexed SQL. The projection schemas and projector implementations are preserved
by the pre-cutover Git tag and this inventory, not kept alive as unused runtime
infrastructure.

This decision does **not** remove inexpensive correctness properties:

- Postgres remains canonical truth;
- tenant isolation remains mandatory;
- writes and worker stages remain idempotent and replay-safe;
- the transactional outbox remains;
- queries remain indexed, bounded, and paginated where applicable;
- newly generated obligation, SOP, proof, notification, and audit history remain
  durable after the clean reseed; and
- module boundaries remain inside the Go modular monolith.

The system is being simplified at the deployment, orchestration, and derived
read-model layers. It is not reduced to memory-only business state.

### What this additionally eliminates (not just cost)

Removing the derived read models is a correctness win, not only a cost cut.
Serving screens from canonical tables deletes the entire projection-drift bug
class: no stale `as_of`/`last_success_at`, no false-freshness watermark, no
dual-writer race between a projector and a sweeper rebuilding the same rows, no
read-through-vs-503 rebuild-availability failure. These are exactly the
rebuild-trigger anti-patterns catalogued in
`docs/decisions/scale-anti-patterns.md` (Calendar/CT/AC/PA rebuild fix,
2026-07-13). A canonical read cannot be stale relative to the canonical write,
so no reconciler is needed to keep a read model in sync. The obligation
sweeper's 15-minute operational stage remains the backstop for time-derived
state (due/missed), which is genuinely a clock effect and not a projection.

## What is being retired

The current Terraform defines 17 independently scheduled jobs. Their configured
cadences create 5,833 scheduler triggers per day, 174,990 in a 30-day month, and
2,129,045 in a 365-day year even when there is no business work.

| Cadence | Jobs | Triggers per day | Triggers per 30 days |
| --- | ---: | ---: | ---: |
| Every minute | 2 | 2,880 | 86,400 |
| Every 5 minutes | 9 | 2,592 | 77,760 |
| Every 10 minutes | 2 | 288 | 8,640 |
| Hourly | 3 | 72 | 2,160 |
| Daily | 1 | 1 | 30 |
| **Total** | **17** | **5,833** | **174,990** |

These are execution triggers, not obligations. An empty sweep still counts as a
trigger but creates zero obligations.

### Cost of the retired topology

The 17 jobs are Cloud Run Jobs, which always bill on the instance-based model:
each execution is charged for the full container lifetime with a **one-minute
minimum**, regardless of how quickly the code exits. A job scheduled every
minute is therefore billed like a full-time 1-vCPU/512-MiB worker even when it
finishes in seconds, and the every-5-minute cohort pays the same one-minute
floor 288 times a day each. At 1 vCPU / 512 MiB across the full fleet this is
roughly **US$250-300 per month before the free tier**, and it is a fixed
compute floor: it does not fall with a smaller herd, because it is buying idle
minutes, not business work. The target topology replaces the fleet with one
long-running worker (billed as a single warm instance) plus Postgres, for a
rough **US$25-65 per month** — an order-of-magnitude reduction that is
independent of animal count.

### Existing job inventory

The SQL column names the principal tables. It is intentionally not an exhaustive
list of every joined lookup or audit row.

| # | Existing job | Cadence | Runs/day | Principal business/SQL effect | Target disposition |
| ---: | --- | --- | ---: | --- | --- |
| 1 | `outbox-relay` | 1 minute | 1,440 | Claims `outbox_messages`, publishes events, marks published/failed/dead-letter state. | Fast lane in the kernel worker. |
| 2 | `domain-event-consumer` | 10 minutes, running for up to 9 minutes | 144 | Pulls Pub/Sub, deduplicates in `domain_event_processed_events`, invokes module handlers that update their owned canonical/projection tables. | Continuous event loop in the kernel worker. |
| 3 | `domain-event-processed-sweeper` | hourly at `:37` | 24 | Deletes old terminal rows from `domain_event_processed_events`, up to 1,000 per run. | Daily housekeeping. |
| 4 | `vaccination-generator` | hourly at `:00` | 24 | Reads goats and effective protocol/rule state; records `vaccination_generation_runs`; idempotently creates or updates `obligation_instances` and `obligation_status_events`. | Hourly generation stage. |
| 5 | `process-integrity-projector` | 5 minutes | 288 | Recomputes `process_integrity_projection_rows` and projection state from canonical work/proof data. | Remove schedule, command, projection rows, and state tables. Serve the screen from canonical indexed SQL. |
| 6 | `vaccination-shed-projector` | 5 minutes | 288 | Recomputes `vaccination_shed_projection_rows` and state. | Remove schedule, command, projection rows, and state tables. Serve the screen from canonical indexed SQL. |
| 7 | `vaccination-execution-projector` | 5 minutes | 288 | Recomputes `vaccination_execution_projection_rows` and state. | Remove schedule, command, projection rows, and state tables. Serve the screen from canonical indexed SQL. |
| 8 | `vaccination-operations-projector` | 5 minutes | 288 | Recomputes `vaccination_operations_projection_rows` and state. | Remove schedule, command, projection rows, and state tables. Serve the screen from canonical indexed SQL. |
| 9 | `obligation-sweeper` | 5 minutes | 288 | Materializes due work into `obligation_batches`, links `obligation_instances`, creates `sop_tasks`, can reserve inventory, marks missed work, refreshes Calendar and three vaccination projections, and queues reminders/escalations. | Single 15-minute operational stage with every projection call removed. It retains canonical obligations/batches/SOP/inventory/reminder/escalation work. |
| 10 | `calendar-projector` | 5 minutes | 288 | Reads obligations/batches; upserts and prunes `calendar_event_projections`. | Remove schedule, command, projection table, and projection-specific state. Calendar reads canonical obligations/batches/SOP/proof directly. |
| 11 | `calendar-reminder-sweeper` | 5 minutes | 288 | Reads Calendar/snooze state, inserts idempotent `notification_requests`, updates reminder state. | Fold into the operational stage and select due reminders directly from canonical obligations/SOP state. |
| 12 | `calendar-escalation-sweeper` | 10 minutes | 144 | Reads overdue obligations/Calendar state, inserts idempotent escalation `notification_requests`, updates escalation state. | Fold into the operational stage and select overdue work directly from canonical obligations/SOP state. |
| 13 | `notification-dispatcher` | 1 minute | 1,440 | Claims `notification_requests` with leases, sends configured channels, and records sent/failed/exhausted state. | Fast lane in the kernel worker. |
| 14 | `inventory-batch-reconciler` | 5 minutes | 288 | Reconciles batch reservation remainders using `obligation_batches`, `inventory_stock`, and `inventory_stock_movements`. | Operational stage. |
| 15 | `idempotency-key-sweeper` | hourly at `:17` | 24 | Deletes expired `idempotency_keys`, up to 1,000 per run. | Daily housekeeping. |
| 16 | `sop-review-fanout-retry` | 5 minutes | 288 | Retries pending/failed `sop_task_review_fanouts` and applies the owning completion/rework handlers. | Operational stage. |
| 17 | `partition-maintainer` | daily at `00:11` India time | 1 | Calls `goatos_ensure_partition_coverage` for partitioned event/history tables. | Remove schedule, command, and partition-maintenance functions. Recreate the three current parents as ordinary indexed tables. |

### Confirmed duplicate work in the existing topology

`obligation-sweeper` defaults currently enable all of the following:

- Calendar vaccination projection refresh;
- vaccination shed projection refresh;
- vaccination execution projection refresh;
- vaccination operations projection refresh;
- reminder sweep; and
- escalation sweep.

All six also have independent schedules. Therefore the existing fleet does more
SQL work than the 5,833 scheduler-trigger count suggests. None of these six
projection paths survives in the 5k-to-50k runtime. Reminder and escalation
behavior survives, but it reads canonical state directly and has one owner.

### Projection tables removed in the clean restructure

The implementation will drop the following derived tables and their associated
state/version tables where present:

- `calendar_event_projections`;
- `process_integrity_projection_rows`;
- `vaccination_shed_projection_rows`;
- `vaccination_execution_projection_rows`; and
- `vaccination_operations_projection_rows`.

Because the current rows are test data, there is no projection backfill or data
migration. Any foreign keys from notification/snooze tables to
`calendar_event_projections` must be replaced with references to canonical
source work such as `obligation_instances`, `obligation_batches`, or `sop_tasks`
before the table is dropped.

### Partitioning removed in the clean restructure

The current partition maintainer covers `goat_identity_events`, `audit_log`, and
`obligation_status_events`. For the 5k-to-50k envelope these tables will be
recreated as ordinary indexed tables with the same logical columns and
constraints. Their current rows are test data and are not migrated. Monthly
partitioning and its maintenance command remain recoverable from the baseline
tag and may be reintroduced for the first measured history/event-table hotspot.

## Target topology for 5k-to-50k

```text
Goat OS API
    -> canonical Postgres transaction + audit + outbox

One kernel worker process
    -> continuous domain-event consumer
    -> one-minute fast-delivery cadence
    -> fifteen-minute operational cadence
    -> hourly obligation-generation cadence
    -> daily housekeeping cadence

Postgres
    -> canonical business tables
    -> durable obligation/SOP/proof/notification state
    -> keyset-paginated list reads + indexed summary aggregates for Calendar,
       process integrity, and vaccination screens
```

### Process and schedule count

- Cloud Scheduler cron jobs in the normal topology: **0**.
- Scheduled Cloud Run Jobs in the normal topology: **0**.
- Long-running kernel worker processes: **1**.
- Continuous subscriber loops: **1**.
- Logical cadence classes inside the worker: **4**.
- Non-projection, non-partition one-shot commands retained for manual repair/backfill: **11**.
- Projection commands retained in the active runtime: **0**.
- Projection tables retained in the active runtime: **0**.
- Partition-maintenance commands retained in the active runtime: **0**.

The worker uses one supervisor with isolated stage runners. A panic or timeout in
one stage is recovered and reported without terminating unrelated stages. Each
stage has its own timeout and Postgres advisory lock so a rolling deployment
cannot run the same stage twice concurrently.

### Availability model (single worker is not a single point of failure)

Consolidating 17 jobs into one worker moves the crash blast radius from one
function to the whole kernel, so the worker is run for high availability, not as
a lone instance. Because every stage claims a Postgres advisory lock before it
runs, **two or more worker instances are safe to run concurrently**: the lock
serializes each stage to exactly one runner while the second instance stands by
and takes over on crash or rolling deploy. The target is therefore
**min-instances >= 2**, advisory-lock-serialized, with health checks and the
immediate startup catch-up run closing any correctness gap after a failover. A
single instance is acceptable only in local/dev, where a cold-start gap on crash
is tolerable.

| Cadence class | Initial cadence | Wake-ups/day | Stages |
| --- | --- | ---: | --- |
| Event consumer | Continuous | not a cron | Receive Pub/Sub events, deduplicate, dispatch module handlers. |
| Fast delivery | Every minute, plus immediate startup run | 1,440 | Drain outbox; dispatch due notifications. |
| Operational | Every 15 minutes, plus immediate startup catch-up | 96 | Sweep due/missed obligations, create batches/SOP work, reconcile inventory, queue reminders/escalations directly from canonical state, and retry SOP fanout. |
| Obligation generation | Hourly, plus event-triggered generation for relevant goat/protocol changes | 24 | Generate/recheck effective vaccination obligations idempotently. |
| Housekeeping | Daily | 1 | Processed-event retention and expired idempotency keys. |

The listed intervals are initial engineering defaults for the 5k-to-50k
envelope. They are configuration, not separate deployments. Product freshness
requirements may tighten a cadence without splitting the worker.

### Why Pub/Sub is retained at this scale

The outbox relay and the domain-event consumer now run inside the same worker
process, so at 5k-to-50k the transport could technically collapse to an
in-process channel and drop Pub/Sub entirely. Pub/Sub is deliberately retained
anyway for three reasons: it keeps the event contract and at-least-once/DLQ
delivery semantics stable across the future multi-worker extraction, it lets a
stage be pulled into its own worker later without rewriting producers, and it
decouples relay throughput from handler latency. The cost is small at this
volume. This is a conscious trade, not an oversight: if per-message cost or the
relay->publish->pull latency hop ever shows up as a measured problem before
extraction, folding the consumer onto an in-process channel is the first
simplification to make.

## Obligation volume: what can and cannot be predicted

Animal count is not obligation count. The number of obligations is determined
by active protocol rules and due occurrences:

```text
new obligations in a period
  = eligible animal/rule/dose occurrences
  - occurrences already present under the idempotency/duplicate guards
  - occurrences suppressed by trusted history or ineligible lifecycle state
  +/- deferred, reopened, canceled, superseded, or recovery-repair transitions
```

The generator is replay-safe because `obligation_instances` has tenant-scoped
idempotency and duplicate guards. Running it hourly does not create a fresh copy
of the same obligation every hour.

Capacity scenarios below are arithmetic examples, not claims about the
vaccination protocol:

| Animals | Average retained obligations per animal | Obligation rows |
| ---: | ---: | ---: |
| 5,000 | 1 | 5,000 |
| 5,000 | 10 | 50,000 |
| 50,000 | 1 | 50,000 |
| 50,000 | 3 | 150,000 |
| 50,000 | 10 | 500,000 |

These row counts are within the selected Postgres-first envelope when queries
use the existing tenant/status/due/scope indexes and workers keep bounded batch
sizes. Actual counts must come from SQL, not from animal-count multiplication.

## SQL used to measure the real workload

All examples require an explicitly verified tenant UUID. They are read-only.

### Animals and obligations by status

```sql
SELECT count(*) AS animals
FROM goats
WHERE tenant_id = :'tenant_id';

SELECT status, count(*) AS obligations
FROM obligation_instances
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;
```

### Open and due obligation pressure

```sql
SELECT
  count(*) AS open_obligations,
  count(DISTINCT target_id) FILTER (WHERE target_type = 'goat') AS affected_goats,
  min(due_at) AS oldest_due_at,
  max(due_at) AS latest_due_at
FROM obligation_instances
WHERE tenant_id = :'tenant_id'
  AND status IN ('scheduled', 'due', 'in_progress', 'deferred', 'missed');

SELECT
  (due_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date,
  status,
  count(*) AS obligations
FROM obligation_instances
WHERE tenant_id = :'tenant_id'
  AND due_at >= now() - interval '7 days'
  AND due_at < now() + interval '30 days'
GROUP BY business_date, status
ORDER BY business_date, status;
```

### Batches, SOP work, and notification backlog

```sql
SELECT status, count(*) AS batches, sum(estimated_targets) AS estimated_targets
FROM obligation_batches
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;

SELECT status, count(*) AS sop_tasks
FROM sop_tasks
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;

SELECT status, count(*) AS notifications, min(requested_at) AS oldest_request
FROM notification_requests
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;
```

### Event and retry backlog

```sql
SELECT status, count(*) AS outbox_rows, min(created_at) AS oldest_created_at
FROM outbox_messages
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;

SELECT status, count(*) AS consumed_events, min(updated_at) AS oldest_updated_at
FROM domain_event_processed_events
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;

SELECT status, count(*) AS review_fanouts, min(updated_at) AS oldest_updated_at
FROM sop_task_review_fanouts
WHERE tenant_id = :'tenant_id'
GROUP BY status
ORDER BY status;
```

### Disposable projection row volume before the clean reset

```sql
SELECT 'calendar' AS projection, count(*) AS rows
FROM calendar_event_projections WHERE tenant_id = :'tenant_id'
UNION ALL
SELECT 'process_integrity', count(*)
FROM process_integrity_projection_rows WHERE tenant_id = :'tenant_id'
UNION ALL
SELECT 'vaccination_shed', count(*)
FROM vaccination_shed_projection_rows WHERE tenant_id = :'tenant_id'
UNION ALL
SELECT 'vaccination_execution', count(*)
FROM vaccination_execution_projection_rows WHERE tenant_id = :'tenant_id'
UNION ALL
SELECT 'vaccination_operations', count(*)
FROM vaccination_operations_projection_rows WHERE tenant_id = :'tenant_id';
```

These queries become the baseline report before implementation and are rerun
only before the clean reset. The projection counts are recorded for comparison,
then discarded. Animal, canonical obligation, backlog, and notification counts
are rerun after the clean reseed. The architecture document must never
substitute guessed business counts.

## Implementation boundary

The consolidation preserves canonical module services and repositories. The new
worker orchestrates them; it does not merge all SQL into a new god package.
Projection repositories and projector commands may be removed from the active
tree after the baseline tag is created.

Required implementation sequence:

1. Capture the pre-cutover commit and create a `kernel-split-workers-v1` tag.
2. Add `backend/cmd/kernel-worker` as a thin supervisor over reusable stage
   runner interfaces.
3. Extract non-projection one-shot command wiring into reusable constructors
   where necessary and keep those commands buildable for manual repair.
4. Replace Calendar, process-integrity, shed, execution, and operations API reads
   with indexed SQL over canonical obligation/batch/SOP/proof/inventory tables.
   Distinguish the two read shapes explicitly and do not conflate them under the
   word "bounded":
   - **List reads** (day list, shed list, per-shed capture) are keyset-paginated
     at ~20 rows and are genuinely bounded regardless of herd size.
   - **Summary aggregates** (Control Tower gaps, adherence rollups,
     process-integrity counts) cannot be keyset-paginated; they are indexed scans
     whose cost grows with open-obligation count. They are acceptable at this
     envelope but are the first extraction candidate.
   Add query-plan tests for BOTH shapes, and run the aggregate path against the
   upper-bound row count in the obligation-volume table (up to ~500k obligation
   rows at 50k animals), not just the 5k list case. A green plan at 5k must not
   be read as proof for the 50k aggregate.
5. Change reminder, escalation, snooze, and notification references so they do
   not depend on `calendar_event_projections`. Notification and snooze tables
   retain their `calendar_event_id` columns as plain application-validated text
   (never FK-constrained). Referential integrity is enforced in application code:
   every writer re-derives the canonical work reference before writing. A
   periodic reconciler function (goatos_reconcile_calendar_event_references) is
   available to surface any orphaned references for review.
6. Run the baseline SQL and record animal, obligation, backlog, notification,
   and disposable projection counts.
7. Drop the projection row/state tables, projector commands, projection-specific
   seeds, partition-maintenance functions/command, and all current test data.
   Recreate the three event/history parents as ordinary indexed tables, then
   recreate/reseed canonical source fixtures.
8. Make one owner explicit for every remaining stage and remove the six
   duplicate projection paths currently embedded plus separately scheduled.
9. Deploy the worker with the existing scheduled jobs still paused.
10. Prove accepted intake -> outbox -> event consumer -> obligation -> batch/SOP
    -> notification/proof -> direct canonical API read flow.
11. Remove the 17 active Terraform schedules only after parity is proven.

No canonical schema is dropped. Derived projection schemas and all current test
rows are intentionally dropped.

### Scale gate: calendar canonical read is index-bound (migration 000190)

Step 4's aggregate-path plan test (`TestCalendarCanonicalReadPlanAtScale`,
gated behind `GOATOS_SCALE_CERT=1`, run by `make scale-cert` at 500k) surfaced a
real regression and its fix:

- **Finding.** The production-shaped bounded calendar read (1-week window, single
  park scope, `LIMIT 21`) sequentially scanned `obligation_instances` at scale.
  Two causes: (a) the drive-membership predicate was a non-sargable 3-way `OR`
  (window / exception-catch-up / overdue-by-due_at) that no single index can
  satisfy; (b) `obligation_drive_summary` LEFT JOINed membership rows to its
  per-`(park_id, due_date)` sub-aggregates at **row** grain with `IS NOT DISTINCT
  FROM` keys, so the planner re-executed each sub-aggregate once per membership
  row (O(n²): ~19s at 500k once the scan was fixed).
- **Fix.** Migration **000190** adds three partial indexes on
  `obligation_instances` — `..._calendar_window (tenant_id, due_at)`,
  `..._calendar_exceptions (tenant_id, status)`,
  `..._calendar_overdue (tenant_id, status, due_at)` — plus
  `..._sop_task (tenant_id, sop_task_id)`. `calendarCanonicalEventsCTE` is
  rewritten so each `OR` branch is a separate index-scannable `UNION` selecting
  full rows (deduped, so a row matching two branches is counted exactly once — the
  drive-summary count invariants are preserved), and `obligation_drive_summary`
  aggregates membership to the `(park_id, due_date)` group grain *before* joining
  the 1:1 sub-metrics. The out-of-window visibility of missed/in-progress/
  deferred/overdue drives is unchanged (regression-tested in
  `repository_integration_test.go` + the e2e story).
- **Result.** At 500k obligations the bounded read is Seq-Scan-free (all
  `obligation_instances` access is index scan) and executes well under the 1s SLO.
  `obligation_batches` stays a cheap scan by design (low cardinality; its
  `COALESCE(window_start, planned_date::timestamptz, window_end)` drive-time is not
  IMMUTABLE and cannot back an index). The seed must anchor to `now()` over a
  realistic multi-month spread — a fixed past date is a time-bomb that turns every
  scheduled row overdue, making the window non-selective and a seq scan genuinely
  optimal.

### Scale-guard reconciliation (required — this is a CI gate, not just a doc)

Step 4 intentionally serves five screens from canonical tables per request. That
read shape is the `compute-on-read` / god-CTE pattern that
`docs/decisions/scale-anti-patterns.md` bans and that `make scale-guard`
(the CI `guardrails` job) blocks mechanically. Narrowing the ADR text does NOT
disable the guard, so the implementation will hit red CI on the first canonical
Calendar/process-integrity read unless the guard is reconciled in the same
change:

- The five named read paths (Calendar, process-integrity, shed, execution,
  operations) are exempted under this envelope using the sanctioned
  `// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
  annotation on each read, with a matching entry in
  `tools/scale-guard/baseline.txt` if required. The guard is NOT globally
  disabled and stays fully active for every other path in `backend/internal/**`.
- The exemption is scoped to reads that are query-plan-tested per step 4 above
  (both list and aggregate shapes). An exempted read that is not plan-tested is a
  defect.
- When a screen later earns its own projection (see the scale-out ladder), the
  annotation is removed and that read returns under guard enforcement.

This keeps the machine gate honest: the anti-pattern rule is suspended only for
the specific, measured, plan-tested envelope reads, never blanket-disabled.

## Recovery and future extraction

The old structure remains recoverable without keeping it live:

1. Git tag: `kernel-split-workers-v1` records the complete pre-cutover code and
   Terraform.
2. This ADR records every old schedule, cadence, responsibility, and principal
   SQL table.
3. Shared stage interfaces: the consolidated worker and any future extracted
   worker invoke the same application services and Postgres repositories.

To restore the whole split-worker topology:

1. stop the consolidated worker so no stage has two owners;
2. restore the projection and partition migrations, projector/partition
   commands, repositories, and `infra/envs/stg/cloud_run_jobs.tf` from the
   baseline tag;
3. recreate projection schemas, convert the three event/history tables back to
   partitioned parents, rebuild derived rows from canonical data, and re-point
   the notification/snooze foreign keys back to `calendar_event_projections`
   that step 5 of the cutover had rewired onto canonical
   `obligation_instances`/`obligation_batches`/`sop_tasks` (reversal is
   incomplete until those FKs are restored); test data itself is not restored;
4. deploy the jobs in paused state;
5. run one manual execution per job and verify idempotent parity;
6. enable schedules only after confirming that embedded duplicate stages are
   disabled; and
7. monitor backlog age and errors through one complete cadence window.

To add one projection later, introduce only the projection table and projector
for the measured hot read path. Keep other screens on canonical SQL. To extract
only one worker stage, disable that stage in the consolidated worker and deploy
it independently. Do not restore all 17 jobs because one read path or stage
became hot.

## When the architecture may scale out

The architecture increases step by step; it does not jump from the 50k design
back to the entire old fleet. Apply these steps in order:

1. repair query plans and indexes;
2. reduce unnecessary scans and duplicate work;
3. tune batch sizes and bounded concurrency;
4. tighten or loosen cadence according to product freshness;
5. add one narrowly owned projection table when a specific direct canonical
   read cannot meet its latency/DB-pressure target after query repair;
6. add that projection's stage to the consolidated worker first;
7. extract only the stage whose measured backlog or failure boundary requires
   independent scaling; and
8. add partitioning, another queue, or another service only for the measured
   hotspot.

A stage becomes an extraction candidate when one or more of these persist after
query/index repair:

- its p95 duration consumes more than half of its cadence interval;
- its oldest eligible backlog remains beyond the product freshness window;
- its failures repeatedly delay unrelated stages;
- it needs materially different IAM/secrets or deployment ownership; or
- database pressure remains sustained during that stage and is attributable to
  its workload.

The extraction decision must include measurements, the single stage being
split, the before/after topology, and the rollback path.

The scale ladder is therefore:

```text
5k to 50k
  canonical indexed SQL + one kernel worker + no projection tables

first proven hot read
  one screen-specific projection table + one stage in the same worker

projection refresh becomes the bottleneck
  extract only that projector into its own worker

multiple independently proven hotspots
  add workers/queues/partitions one measured boundary at a time
```

## Documents this decision supersedes or narrows

Where the following documents require a one-million-animal topology as a
present release invariant, this ADR narrows them to correctness guidance and
future scale work for the initial 5k-to-50k release envelope:

- `context/architecture/operational-kernel.md`;
- `context/architecture/operational-kernel-system-design.md`;
- `context/architecture/final-architecture.md`;
- `docs/decisions/high-scale-dashboard-projections.md`;
- `docs/decisions/scale-anti-patterns.md`;
- `docs/protocol-engine/high-scale-kernel-validation-plan.md`; and
- `.agents/skills/goatos-build/references/architecture.md`.

Those documents must be reconciled during implementation so future agents do
not receive contradictory scale instructions. Until then, this ADR is the
authority for operational-kernel deployment scale and worker topology.
