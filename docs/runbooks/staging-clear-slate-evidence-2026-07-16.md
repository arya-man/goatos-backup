# Staging clear-slate evidence — 2026-07-16

Status: destructive cleanup complete; no seed data survives.

This record covers the July 16 cleanup of the disposable `goatos-stg`
environment before its next deployment. It is evidence of a blank target, not
a claim that the application is demo-ready. The next deployment must apply the
current migrations before any separately authorized reseed.

## Verified boundary

| Boundary | Value |
| --- | --- |
| Account | `ravi@mesha.sg` |
| Organization | `vgoats.com` / `563962826703` |
| Folder | `goat-os` / `188649904255` |
| Project | `goatos-stg` / `514832198871` |
| Region | `asia-south1` |
| Repository | `vgoats/goatos` |
| Tenant reserved for a later seed | `00000000-0000-4000-8000-000000000001` |

The legacy `goatos-sheets` project and all Heva/Slice resources were outside
scope and were not touched.

## Recovery point

The on-demand Cloud SQL backup completed successfully before the destructive
work:

| Backup ID | Type | Started (UTC) | Finished (UTC) | Status |
| --- | --- | --- | --- | --- |
| `1784159640146` | `ON_DEMAND` | `2026-07-15T23:54:00.153Z` | `2026-07-15T23:55:21.333Z` | `SUCCESSFUL` |

## Database actions and final state

The application database URL secret was repaired by adding version 2 with the
current application credential. No secret value is recorded in source.

During verification, migration execution `goatos-stg-migrate-w5vck` succeeded.
An initial reset had removed only `public`; a later migration execution,
`goatos-stg-migrate-b6x7t`, correctly failed at migration 000168 because the
separate `analytics.funnel_daily` table still existed. The reset contract was
therefore corrected to include every application-owned schema. Migration
execution `goatos-stg-migrate-cclgx` then succeeded from a genuinely clean
database.

A partial seed was started before the operator clarified that reseeding was not
authorized. Founder email grants and roster rows were temporarily written; the
vaccination import timed out while publishing the matrix. The final destructive
reset removed all of those rows and all migrated objects. Nothing from that
attempt survives.

Final database state:

- dropped `analytics` and `public` with `CASCADE`;
- recreated `public` owned by `goatos_app`;
- retained PostgreSQL system schemas only;
- application schemas present: `public` only;
- objects in `public`: **0**;
- migrations present: **0**;
- seed rows present: **0**.

## Retired runtime deletion

Zero Cloud Scheduler jobs and zero Cloud Tasks queues existed at final
verification. The following 15 obsolete Cloud Run Job definitions were deleted:

1. `goatos-stg-calendar-escalation-sweeper`
2. `goatos-stg-calendar-projector`
3. `goatos-stg-calendar-reminder-sweeper`
4. `goatos-stg-domain-event-consumer`
5. `goatos-stg-domain-event-processed-sweeper`
6. `goatos-stg-idempotency-key-sweeper`
7. `goatos-stg-inventory-batch-reconciler`
8. `goatos-stg-notification-dispatcher`
9. `goatos-stg-obligation-sweeper`
10. `goatos-stg-outbox-relay`
11. `goatos-stg-partition-maintainer`
12. `goatos-stg-process-integrity-projector`
13. `goatos-stg-sop-review-fanout-retry`
14. `goatos-stg-vaccination-generator`
15. `goatos-stg-vaccination-shed-projector`

Their 15 legacy service accounts were deleted as well:

- `goatos-calendar-escalation-stg`
- `goatos-calendar-projector-stg`
- `goatos-calendar-reminder-stg`
- `goatos-cloud-tasks-stg`
- `goatos-domain-consumer-stg`
- `goatos-domain-event-sweep-stg`
- `goatos-idempotency-sweeper-stg`
- `goatos-inventory-reconcile-stg`
- `goatos-notify-stg`
- `goatos-obligation-sweeper-stg`
- `goatos-outbox-relay-stg`
- `goatos-partition-maint-stg`
- `goatos-scheduler-stg`
- `goatos-sop-review-retry-stg`
- `goatos-vax-generator-stg`

Before account deletion, cleanup removed their project-level Cloud SQL, Cloud
Tasks, telemetry, and FCM grants; database/webhook secret access; and Pub/Sub
publisher/subscriber bindings. The nine obsolete runtime service-account
instances that remained in Terraform state were removed from state after their
live deletion. Current Terraform source does not define the retired fleet, so a
normal current-source apply cannot recreate it.

## Artifacts built but not released

Runtime artifacts were built from commit
`158c2bc005946f3d8435564d5f1c0d21670043e5` while validating the migration
path. They were pushed to the staging Artifact Registry but were not promoted
as a Cloud Deploy release after the operator requested a blank target:

| Artifact | Immutable digest |
| --- | --- |
| Backend | `sha256:6a9c55f282ccff298fba2e49acbeca7eeb87ef51b4678c05313ec228bc13949c` |
| Migration | `sha256:5fe91014c27a9938e2453348523976aa06f5f2d497429d4417da3976397f3059` |
| Admin web | `sha256:2a8e838bb2310eb90d71e104724463ee43b01d384bd2e807738da7e88834f2c0` |

The Cloud Deploy custom-target runner is pinned to
`sha256:85c1ccad8514b5a4a638e2c1a85a32ec7daf5d2b6a7475327452348262fa4a13`.
The next rollout must still build/release the exact promoted `main` commit; the
existence of these images is not deployment evidence.

## Resources intentionally retained

The cleanup preserves the foundation needed for the next deployment:

- Cloud SQL instance and the successful recovery backup;
- Secret Manager secrets, Artifact Registry, networking, Pub/Sub, and
  observability foundation;
- Cloud Deploy pipeline, custom target, and deployer identity;
- API, admin-web, Grafana, and Grafana Alloy services;
- migration job `goatos-stg-migrate`;
- current-topology identities for API, admin-web, kernel worker, migration,
  outbox-DLQ, legacy import, observability, deploy, and proof signing.

At the clear-slate boundary the kernel-worker service and the manual
`goatos-stg-outbox-dlq` and `goatos-stg-analytics-rollup` jobs are not live yet.
They must be created by the current deployment topology. Analytics is retained
as a required capability and remains an explicit manual job after reseed; it is
not a scheduler.

## Final verification and next authorized sequence

Final live counts:

| Resource | Count / state |
| --- | --- |
| Application DB objects | `0` |
| Cloud Scheduler jobs | `0` |
| Cloud Tasks queues | `0` |
| Obsolete Cloud Run Jobs | `0` |
| Legacy job service accounts | `0` |
| Legacy project IAM bindings | `0` |
| Legacy secret IAM bindings | `0` |
| Legacy Pub/Sub IAM bindings | `0` |
| Retained Cloud Run Jobs | `1` (`goatos-stg-migrate`) |
| Active/incomplete job executions | `0` |

The only valid next sequence is:

1. promote current `main` to `stg` through a same-repository PR;
2. create/converge the kernel worker and the two missing manual jobs;
3. create a Cloud Deploy release from one immutable commit;
4. run migrations;
5. stop unless the operator separately authorizes reseeding;
6. after an authorized seed, run analytics manually and complete authenticated
   API/UI/worker smoke checks.

Do not replay an old staging release or apply Terraform from an old commit: that
is the only path that could recreate the retired scheduler/job topology.
