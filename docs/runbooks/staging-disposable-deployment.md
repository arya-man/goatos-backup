# Disposable staging deployment

Status: authoritative for `goatos-stg` while the product is under development.

Latest destructive-reset record:
`staging-clear-slate-evidence-2026-07-16.md`.

This runbook implements the accepted 5k-to-50k operational-kernel envelope.
Staging contains disposable test data: reset the application schema and reseed
it instead of preserving an obsolete topology or attempting mixed-version
database compatibility.

## Final staging topology

- `goatos-api-stg` and `goatos-admin-web-stg` Cloud Run services;
- one `goatos-kernel-worker-stg` Cloud Run service with exactly two always-warm
  instances (`min=2`, `max=2`), with Postgres advisory locks electing one owner
  for each stage and the other instance acting as hot standby;
- zero Cloud Scheduler jobs;
- zero scheduled Cloud Run Jobs;
- `goatos-stg-migrate`, `goatos-stg-outbox-dlq`, and
  `goatos-stg-analytics-rollup` retained as explicit Cloud Run Jobs;
- GA4/BigQuery/Postgres analytics retained. The analytics rollup uses the same
  backend release image and is run manually after reseed and before a demo;
- `goat_identity_events`, `audit_log`, and `obligation_status_events` are
  ordinary indexed Postgres tables. There is no partition-maintenance runtime.

The seven former per-stage jobs, the partition-maintainer job, their service
accounts, and Scheduler invoker IAM are retired from staging Terraform. The
kernel worker owns the continuous, fast, operational, generation, and
housekeeping stages.

## Source and infrastructure convergence

Terraform is the resource-shape authority, but a broad apply is allowed only
after the existing staging resources are imported into the remote state. Until
that import is complete, produce a reviewed plan for only the resources needed
to converge this topology. Never approve a plan containing unrelated destroys.

Before every cloud write, verify:

```text
account:        ravi@mesha.sg
organization:   vgoats.com / 563962826703
folder:         goat-os / 188649904255
project:        goatos-stg / 514832198871
region:         asia-south1
repository:     vgoats/goatos
```

The convergence plan must create or update the two-instance kernel worker,
retain the three explicit jobs above, and delete every legacy Scheduler/job/SA
resource represented in state. It must not recreate a partition maintainer or
an analytics schedule.

## Clean deployment order

1. Confirm all writers and executions are stopped. Take and record a successful
   on-demand Cloud SQL backup.
2. Drop every application-owned schema in `goatos-stg` (`public` and
   `analytics` today), then recreate an empty `public` schema owned by
   `goatos_app`. Do not touch PostgreSQL system schemas. This must remove the
   separate `analytics` schema as well as `public`; otherwise migration 000168
   collides with stale analytics rollup tables on the next clean deployment.
3. Promote the exact reviewed `main` commit through the same-repository
   `main -> stg` PR. Direct pushes to `stg` are forbidden.
4. Cloud Deploy verifies the API, kernel worker, admin-web, and migration job
   exist before the first database mutation.
5. Cloud Deploy updates and executes `goatos-stg-migrate`, then updates the API,
   kernel worker, manual backend-image jobs, and admin-web from the same commit.
6. Seed founder grants, workforce/ownership, goats, reviewed vaccination
   source history, canonical obligations, and the retained small summaries in
   the order specified by `staging-vaccination-clean-slate.md`.
7. Execute the analytics rollup explicitly:

   ```bash
   gcloud run jobs execute goatos-stg-analytics-rollup \
     --project=goatos-stg --region=asia-south1 --wait
   ```

8. Verify API health, authenticated dashboard access, canonical vaccination and
   Calendar reads, worker stage logs on both instances, zero Scheduler jobs,
   zero failed job executions, and analytics rows for the available GA4 export
   day.

Do not start the demo from a partially seeded database. A failed migration,
seed invariant, worker stage, smoke check, or analytics rollup is a failed
deployment and requires correcting the source or repeating the clean reset.
