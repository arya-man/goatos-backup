# Vaccine Inventory Director Task: Kernel Notes

Date: 2026-08-26

## Requirement

When a vaccination is scheduled, Goat OS must automatically create a
Preventive Care director inventory task seven days before the vaccination date.
The task asks the PC director to verify vaccine stock availability with fridge
photo or video proof. The task has a 24-hour window; if it is not completed in
that window, it becomes overdue.

This must work even when vaccination schedule data is inserted or changed
directly in Postgres, and when anchor-date changes generate future vaccination
dates. It must not depend only on an admin-web or mobile API path.

The feature is gated by `inventory_vaccine` and currently targets the
`pc_director` owner path for Chandrakant. CEO/progress views and verifier views
must see the same submitted proof media.

## Existing Watcher Layer

The system already has a permanent background watcher: the consolidated
`kernel-worker`.

In Google staging, it is deployed as the always-on Cloud Run service
`goatos-kernel-worker-stg`, defined in:

- `infra/envs/stg/cloud_run_worker.tf`
- `backend/cmd/kernel-worker/main.go`

Important staging properties:

- It runs `/app/bin/kernel-worker`.
- It is a Cloud Run service, not a Cloud Run Job.
- It has `min_instance_count = 2` and `max_instance_count = 2`.
- CPU is always allocated with `cpu_idle = false`.
- Worker stages are enabled with `GOATOS_WORKER_STAGES_ENABLED=true`.
- Health endpoints are only lifecycle probes; they do not trigger work.
- Advisory locks prevent the two instances from double-running a stage.

`infra/envs/stg/README.md` records the staging target as:

```text
Scheduler:        zero jobs; the always-on kernel worker owns cadence stages
```

The OCI/source copy currently matches the Google staging worker source and
worker Terraform for this layer:

- `goatos/backend/cmd/kernel-worker/main.go`
- `goatos-origin-main-oci/backend/cmd/kernel-worker/main.go`
- `goatos/infra/envs/stg/cloud_run_worker.tf`
- `goatos-origin-main-oci/infra/envs/stg/cloud_run_worker.tf`

## What Runs When

The worker registers the following relevant lanes in
`backend/cmd/kernel-worker/main.go`:

- Continuous: Pub/Sub domain-event consumer.
- Every 1 minute: outbox relay and notification dispatcher.
- Every 5 minutes: obligation sweep and operational stages.
- Every 1 hour: vaccination generation and housekeeping/recovery stages.

For vaccination specifically:

- `backend/internal/kernelstages/generation.go`
  runs `VaccinationGenerationStage` hourly and calls
  `GenerateEffectiveForAllGoats`. This is the safety net for future vaccination
  obligations, including missed event recovery shape.
- `backend/internal/kernelstages/obligation_sweeper.go`
  runs `ObligationSweeperStage` every five minutes. It materializes due
  vaccination obligations into batches and SOP tasks, reserves inventory, marks
  missed work, and queues reminders/escalations.
- `backend/internal/kernelstages/reminder_cadence.go`
  handles the existing vaccination reminder ladder and resolves vaccination
  audiences from workforce duties/positions.

## What Not To Use

Do not use Cloud Tasks for this director inventory task. The near-term Cloud
Tasks queue was intentionally retired; `infra/envs/stg/cloud_tasks.tf` says
notifications now stay durable in `notification_requests` and are drained by
the worker's one-minute notification stage.

Do not add a separate Cloud Scheduler cron for this feature in staging. The
normal topology is zero scheduler jobs for kernel work. New recurring work must
plug into the existing kernel-worker lanes unless a separate maintainer
decision changes the operational-kernel topology.

Do not rely only on Pub/Sub/domain events. Pub/Sub is the durable event delivery
path for normal application writes, but direct DB inserts/updates may not emit a
domain event. The seven-days-before inventory task needs reconciliation from
canonical tables.

## Correct Integration Shape

Add or extend a kernel-worker reconciliation stage that scans canonical
vaccination schedule/batch/obligation state and idempotently creates the PC
director inventory task when:

```text
inventory_task_due_at = vaccination_date - 7 days
task_window_end       = inventory_task_due_at + 24 hours
```

The stage should run on an existing cadence, most likely the five-minute
operational lane, because it is deadline-oriented and must catch direct DB
changes without waiting for a UI/API event.

The task creation must be idempotent. Use a stable idempotency key at the
business grain, for example tenant + planned vaccination date + park/shed/batch
+ vaccine identity, so retries and repeated sweeps create only one director
task.

The task should become overdue after the 24-hour window if still incomplete.
Completed-today cards may remain visible as done for the current business day,
but old completed cards from previous days must not remain in operator/director
current-card views.

## Proof And Verification

The director task proof must accept image or video of the fridge stock. The same
proof media must be visible to:

- CEO/progress surfaces.
- The inventory-vaccine verification flow.
- The verifier responsible for `inventory_vaccine` review.

This should follow the shared operational-kernel rule: domain state, task state,
proof, verification item, status rollup, and leadership visibility are one
process chain, not separate screen-only records.

## E2E Scope

The end-to-end proof should cover:

- App/API-created vaccination schedule.
- Direct DB-created or DB-updated vaccination schedule.
- Anchor-date generated future vaccination dates.
- Director mobile card ordering and visibility.
- Operator/director stale-card filtering: current, carry-over overdue, and
  completed-today only.
- CEO progress visibility.
- Verifier proof visibility.
- Google `goatos-stg` kernel-worker topology.
- OCI/source parity for the same worker stage.
