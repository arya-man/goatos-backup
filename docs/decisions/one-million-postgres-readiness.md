# One-Million PostgreSQL Readiness

**Status:** Accepted readiness contract; implementation and certification remain open

**Date:** 2026-07-13

**Applies to:** Goat OS API, scheduled workers, projectors, sweepers, repair jobs, and operational Postgres read models

## Decision

Goat OS needs database workload protection at one-million-animal scale, but it
must not copy a fixed PgBouncer split such as `80 API / 80 workers / 20
webhooks` without measurements. Animal count and daily operation count do not
directly determine connection count. The relevant inputs are concurrent
database work, query duration, burst overlap, Cloud Run instance limits,
worker concurrency, and the Cloud SQL connection ceiling.

The required design is:

1. one explicit global connection budget with protected API, worker, and
   repair/migration lanes;
2. query-shape-driven indexes plus real planner proof on the scaled dataset;
3. partitioning and retention based on table behavior, not row count alone;
4. incremental projection maintenance as the normal path, with full rebuilds
   reserved for bootstrap and repair; and
5. measured one-million-scale certification before any production-ready claim.

PgBouncer remains a conditional implementation choice. It is not a substitute
for connection budgets, bounded worker concurrency, staggered schedules,
efficient SQL, or pool-pressure telemetry.

## Current State

This is the repository state reviewed on 2026-07-13. It is a useful baseline,
not proof that the one-million gate passes.

| Area | Current evidence | Readiness judgment |
| --- | --- | --- |
| Application pools | The shared pgx configuration uses `GOATOS_PG_MAX_CONNS`, defaulting to 10. The staging API can scale to 10 instances. | A default deployment can therefore request up to 100 API connections before scheduled jobs and operational reserve are counted. There is no committed global budget or per-workload allocation. |
| Scheduled database work | Process-integrity, vaccination execution, vaccination operations, obligation sweeping, Calendar upcoming, reminders, and escalation jobs share five-minute boundaries. | Expensive jobs can overlap and compete with API traffic. Cadences must be staggered and overlap bounded. |
| Serving indexes | Obligation due scans and versioned vaccination projection tables have tenant-leading, query-aligned indexes and keyset-oriented serving paths. | Good foundation. CI index-reachability checks do not prove the planner will choose those indexes at scale. |
| Partitioning | `obligation_status_events`, `goat_identity_events`, and `audit_log` have range partitions and coverage maintenance. `obligation_instances`, `obligation_batches`, and `vaccination_completions` are currently ordinary indexed tables. | Partitioning is partial. Existing documentation previously claimed more partitioning than the migrations implement. The mismatch is corrected by this decision. |
| Projection refresh | The shed vaccination projection has a bounded dirty-scope worker. Process-integrity, vaccination execution, and vaccination operations still have scheduled full recompute paths. Calendar history explicitly uses a temporary hourly full replay. | The dirty-scope worker is the model to extend. Repeated whole-tenant rebuilds are not the normal certified steady-state design. |
| Projection limits | Vaccination execution recompute has a hard `1_000_000` build-row budget and fails when exhausted. | A defensive limit is useful, but it cannot become an accidental certification ceiling. The rebuild must be bounded by cursor/shard or replaced by incremental maintenance. |
| Monitoring | Staging alerts on Cloud SQL CPU, but the committed monitoring does not alert on database connection pressure or application pool wait. | Pool exhaustion could hurt API traffic before the existing alert explains the cause. |
| Scale proof | The disposable `goatos-dev` VM document is an implementation/rehearsal handoff and states that no run has occurred. It uses PostgreSQL inside the VM, not the committed staging Cloud SQL profile. | The VM run can provide one-million-row behavior and query evidence, but it cannot close Cloud SQL or full-chain certification. Goat OS is not yet one-million certified. |

## Required Connection Model

### Global budget

Each environment must declare a database connection budget:

```text
allocatable connections
  = Cloud SQL safe connection ceiling
  - administration and migration reserve
  - failover and incident reserve
```

The allocatable budget is then divided into workload lanes:

- **API lane:** highest priority and protected from worker bursts;
- **worker lane:** bounded scheduled consumers, sweepers, and incremental
  projectors; and
- **repair lane:** full recomputes, backfills, migrations, and operator repair
  jobs, with the smallest concurrency and an explicit admission gate.

The budget must be enforced together through `GOATOS_PG_MAX_CONNS`, Cloud Run
service instance limits, Cloud Run job task/parallelism limits, and application
worker counts. A configuration is invalid if its worst-case requested
connections exceed the allocatable budget.

API capacity must remain available when every scheduled job is runnable.
Heavy repair work must not start merely because a cron boundary was reached.
Five-minute jobs must be staggered, and same-tenant projector overlap must be
prevented with advisory locks or equivalent durable leases.

### Pool telemetry and alerts

Before certification, Goat OS must expose and alert on:

- pgx acquired, idle, and maximum connections;
- empty-pool acquisition count and acquisition wait duration;
- Cloud SQL backend connection count and utilization;
- database CPU, storage I/O, and lock wait duration; and
- per-job duration, overlap, projection lag, and backlog.

The certification floor remains connection-pool p95 at or below 80% of max,
with lock waits p99 below 250 ms. Alerts must fire early enough to preserve the
API reserve, not only after a request failure.

### PgBouncer decision

Introduce PgBouncer only when the measured workload shows that direct pgx
pools cannot safely absorb Cloud Run instance churn or connection bursts
within the global budget. If introduced:

- use workload-specific users or endpoints so the API reserve cannot be
  consumed by workers;
- prefer transaction pooling only after auditing prepared statements,
  temporary tables, advisory locks, and other session-dependent behavior;
- size lanes from measured concurrency and database headroom, not copied
  counts; and
- keep application-level concurrency limits and telemetry in place.

## Indexing Contract

Indexes are accepted only when they serve a named query shape. Operational hot
paths must normally begin with tenant/scope and then the filter, ordering, and
stable keyset cursor columns used by the SQL. Offset pagination and broad
compute-on-read aggregation over herd-scale facts are not accepted.

The two proof layers are deliberately different:

1. per-push plan guards prove that an eligible index path exists; and
2. the loaded one-million dataset, after `ANALYZE`, must use
   `EXPLAIN (ANALYZE, BUFFERS)` without disabling sequential scans to prove
   actual planner choice, latency, rows scanned, and buffer behavior.

An index that exists but is not selected under production-shaped skew does not
close the gate.

## Partitioning and Retention Contract

Do not partition every table merely because it may contain one million rows.
Partitioning adds key, uniqueness, foreign-key, migration, and query-planning
complexity. Use it when it materially improves pruning, retention, maintenance,
or removal of old projection versions.

| Table behavior | Default decision |
| --- | --- |
| Append-only time-series data such as audit, status events, receipts, and historical event streams | Range-partition by the dominant time key, keep a default partition, automate future coverage, and define retention/archive. These are the strongest partition candidates. |
| Mutable current-state rows such as active obligations and batches | Keep tenant-leading indexes first. Partition only after scale plans show a benefit and the partition key preserves hot query pruning and uniqueness semantics. Bound terminal rows with an explicit retention/archive decision. |
| Completion facts | Measure history queries, write rate, retention, and deletion cost. Partition by administration/completion time only when pruning or lifecycle management justifies it. |
| Serving projections | Do not partition solely by animal count. Prefer bounded incremental upserts. For large versioned snapshots, use a version/shard layout that permits bounded retirement, including partition drop when measurements justify it. |
| Idempotency and run ledgers | Declare the replay-safety window and use retention or partitioning so indexes cannot grow forever without an operational decision. |

For every partitioned table, the owner must document partition key, unique-key
implications, default-partition behavior, advance creation, retention/archive,
late-arriving rows, and recovery when maintenance fails.

## Projection Maintenance Contract

Projection tables are correct for the Goat OS command surfaces: APIs should
serve bounded indexed read models rather than aggregate canonical history on
every request. The remaining risk is how those projections are maintained.

The steady-state path must be incremental and idempotent:

```text
canonical transaction
  -> outbox/domain event or durable dirty scope
  -> bounded tenant/scope claim
  -> projection upsert/delete
  -> freshness and backlog telemetry
```

The vaccination dirty-shed worker is the current reference pattern. Process
integrity, vaccination execution, vaccination operations, and Calendar history
must either adopt an equivalent incremental path or prove that a bounded
sharded rebuild meets the certification thresholds without starving API work.
Full recompute remains available for bootstrap, reconciliation, and repair,
behind repair-lane admission and overlap protection.

Projection correctness is not inferred from speed. Certification must compare
canonical and projected counts exactly after refresh and verify the documented
freshness envelope.

## Closure Ledger

| Priority | Required closure | Evidence required |
| --- | --- | --- |
| P0 | Commit the per-environment global connection budget and workload allocations. | Calculated worst-case API/job/repair connections remain below the safe ceiling with reserves intact. |
| P0 | Stagger heavy schedules and prevent overlapping same-tenant rebuilds. | Terraform/job configuration plus an overlap/lock test. |
| P0 | Export pool-pressure metrics and add connection/pool-wait alerts. | Dashboard and alert-policy evidence under induced pressure. |
| P0 | Run one-million certification on the committed `goatos-stg-1m-benchmark-v1` Cloud SQL profile against an exact commit. | Published measured values for every hard threshold, Cloud SQL and application-pool pressure, real plans, reconciliation, and the complete environment/profile manifest. |
| P1 | Reconcile partition/retention decisions for obligations, batches, completions, idempotency, and run ledgers. | Migration or an explicit measured decision to retain indexed non-partitioned storage, plus lifecycle policy. |
| P1 | Replace steady-state whole-tenant projectors with incremental dirty-scope/event maintenance or bounded sharded rebuilds. | Backlog, freshness, repair, and canonical-parity proof. |
| P1 | Remove the execution projector's fixed one-million-row ceiling as a scale boundary. | Cursor/shard/incremental design and a rebuild over the certification dataset. |
| P2 | Evaluate PgBouncer after the preceding controls are measured. | A load-test comparison showing whether it improves connection churn/headroom without breaking session semantics. |

## Certification Boundary

This decision does not certify Goat OS at one million. Certification still
requires the hard thresholds in the
[High-Scale Kernel Validation Plan](../protocol-engine/high-scale-kernel-validation-plan.md),
including the skewed multi-tenant seed, scaled planner proof, API latency,
database pressure, projection freshness/rebuild, tenant fairness, replay, and
canonical/projection reconciliation. The certifying environment is the committed
`goatos-stg-1m-benchmark-v1` Cloud SQL profile in the
[Google Cloud Environments Runbook](../runbooks/google-cloud-environments.md),
executed through the staging command and evidence contract in the validation
plan. The
[Disposable GCP One-Million Scale Test Handoff](../../context/execution/gcp-disposable-1m-scale-test-handoff-2026-07-13.md)
defines a `goatos-dev` VM rehearsal and cleanup workflow only. Even a passing
one-million-row VM run is not Cloud SQL, staging, or full-chain certification.

## Non-Goals

- Prescribing pool counts before measuring concurrency and the Cloud SQL
  ceiling.
- Partitioning every million-row table.
- Treating BigQuery or a queue as the operational source of truth.
- Calling static plan guards, projection-table existence, or an unexecuted load
  harness one-million certification.

## Repository Evidence

- pgx pool configuration: `backend/internal/platform/postgres/postgres.go`
- API scaling and scheduled jobs: `infra/envs/stg/cloud_run_services.tf` and
  `infra/envs/stg/cloud_run_jobs.tf`
- obligation and vaccination storage:
  `backend/migrations/postgres/000074_obligation_engine.sql` and
  `backend/migrations/postgres/000075_vaccination_module.sql`
- partition maintenance:
  `backend/migrations/postgres/000117_partition_coverage_maintenance.sql`
- execution projection budget:
  `backend/internal/vaccinationexecution/adapters/postgres/repository.go`
- query-plan reachability guard:
  `backend/tests/integration/validate-sqlc-query-plans.sh`
- staging monitoring: `infra/envs/stg/monitoring.tf`
