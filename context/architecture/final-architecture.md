# Goat OS Final Architecture

Status: authoritative.

This document supersedes older root-level planning docs that used phase labels. Forms, analytics, streaming, verification, telemetry, and cost controls are production-grade from the start. The system grows by adding configuration, modules, farms, devices, and load tuning; not by changing quality tiers.

## Core Principle

```text
Goat OS owns truth, contracts, rules, and verification.
Managed infra carries critical path.
Open-source read-side tools carry BI/semantic exploration.
No raw table or external tool becomes the source of truth.
```

## Runtime Shape

```text
apps
  android field app
  admin web/mobile command center
  CEO/internal dashboard
  investor sanitized dashboard
  public website is outside Goat OS core; it may only call approved public APIs if ever needed

goat-ops-core
  Go modular monolith
  Postgres source of truth
  domain modules with owned tables
  typed domain events
  outbox

device-gateway
  structural validation for RFID, scale, camera, ultrasound, collar, sensor events
  publishes raw/normalized telemetry
  does not mutate operational truth directly

analytics
  BigQuery historical warehouse
  Tinybird hot telemetry/live APIs
  GCS raw archive/media
  dbt Core transforms/tests
  Cube Core semantic layer
  Metabase internal BI
  Next.js product dashboards

platform
  auth adapter
  permissions/RBAC
  audit
  notifications
  scheduler/sweeper
  media storage
  context layer for engineers/AI agents
```

## Critical Path

```text
operator/admin action
  -> app-api
  -> permissions
  -> domain validation
  -> one Postgres transaction
       canonical row(s)
       typed event
       outbox record
       audit record
  -> outbox relay
  -> Pub/Sub
  -> analytics/notifications/workers
```

No app reads or writes databases directly. Apps call APIs. APIs can compose read-only views, but writes always go to the owning context.

## Million-Goat Engineering Hard Rules

Goat OS must be designed for 50k goats now and 1M+ goats later. Scale is a
design constraint from the first table, worker, API, and dashboard.

Backend rules:

```text
Go modular monolith with strict module boundaries.
Handlers stay thin; domain logic lives in module services.
Modules own their tables and do not write each other's tables directly.
Ports/adapters wrap replaceable tools: auth, storage, pubsub, analytics, devices, notifications.
Every external call has context cancellation, timeout, retry policy, and bounded payload size.
Goroutines must use bounded worker pools, backpressure, cancellation, and error reporting.
No unbounded goroutine per goat/event/media upload.
```

Database rules:

```text
Postgres is operational truth.
Design tables by ownership and access pattern, not by spreadsheet shape.
No API path may scan the full herd.
All large reads filter by farm/park/shed/cohort/date/status where applicable.
High-volume tables are indexed for real queries before production use.
Partition high-volume event/outbox/media/telemetry-derived tables by date and/or scope where needed.
Canonical writes use transactions.
Offline/mobile submits require idempotency keys.
Outbox writes happen in the same transaction as canonical state changes.
```

Task and worker rules:

```text
Task generation is chunked by park/date/shed/cohort.
Sweepers are resumable and safe to rerun.
Generation keys/upserts prevent duplicate tasks.
Workers page through records; they never load 1M goats into memory.
Pending/missed work carries reason and history, not silent overwrite.
```

Workforce operations rules:

```text
This is goat-care workforce ops, not payroll HRMS.
Every task has owner, scope, due time, backup path, escalation path, and audit history.
Rosters are scoped by park/team/date/shift/task_type, never global free-form assignment.
Absence/backfill creates a reassignment record; it does not overwrite original ownership.
Backfill selection must match task skill, park/shed/cohort scope, current load, and task risk.
Backfill must notify the new assignee and the park head.
If no qualified backup exists, the task escalates to the park head; it is never silently dropped.
Overdue escalation fires on due_at breach even when absence was never marked.
Park heads see work by park/shed/team/operator/status/risk.
Operators see only their assigned/current work and entry history.
Task queues are indexed by park_id, shed_id/cohort_id where relevant, team_id, assignee_id, status, due_at, task_type.
Feeding, vaccination, health follow-up, weighing, movement, breeding checks, device checks, and verification all use the same task/workforce engine.
```

Media rules:

```text
Android uploads media directly to GCS using signed URLs.
API never proxies video/photo bytes.
Postgres stores metadata, proof reference, hash, status, and audit link.
Raw videos expire by lifecycle/retention policy after verification/legal window.
```

Proof and verification rules:

```text
Proof capture is a platform capability, not a per-module uploader.
Each module declares proof policy: media type, subject, verifier, retention, and rework rules.
Proof may attach to a goat, task, batch, load, attendance record, crop action, or exit event.
Verification records preserve requested_by, verifier, decision, reason, media refs, and audit link.
Rejected proof creates rework/correction; it does not silently overwrite completed work.
```

Feed supply and crop/fodder boundary:

```text
Feed availability and feed cost affect goat growth, health, and economics.
If crop/fodder farming is inside Goat OS, model farmers, crop seasons, sowing,
daily crop tasks, harvest, expenditure, and inventory handoff.
If crop/fodder farming stays outside Goat OS, integrate through a clean feed
inventory and cost intake boundary.
Dashboards must not depend on hidden Slack/App Script crop calculations.
```

Analytics rules:

```text
Operational APIs do not run heavy analytics.
Dashboards use analytics APIs/Cube/governed marts, not raw table scans.
BigQuery tables are partitioned/clustered and guarded by max-bytes/quota rules.
Tinybird serves hot telemetry/live views only; long-term history goes to BigQuery/GCS.
Official metrics have one definition in Cube.
Unit economics are governed metrics: landing cost, feed cost, medicine/treatment
cost, mortality loss, realized margin, and cost per goat/kg/load/source.
```

Observability rules:

```text
Instrument APIs/workers with OpenTelemetry from day one.
Track API p50/p95/p99 latency, error rate, DB slow queries, locks, connection pressure,
outbox age, Pub/Sub lag, DLQ count, media failures, BigQuery bytes scanned, Tinybird lag.
Prod alerts must exist for API p99 breach, outbox oldest-unsent age, DLQ > threshold,
Pub/Sub lag, BigQuery scan spike, and media failure spike.
Use Google Cloud Monitoring/Logging/Trace/Error Reporting first; Grafana is optional later.
```

## AI Authority Boundary

AI is never the source of truth in Goat OS.

```text
AI may:
  propose identity matches
  pre-check videos/photos
  flag health or behavior anomalies
  suggest eligibility/pricing risks
  summarize records
  triage verification queues

AI may not:
  directly merge goat identities
  directly mark goats eligible/sold/booked
  directly create canonical treatment, death, vaccination, breeding, or sale events
  bypass withdrawal-period, delivery-date, or policy rules
  bypass database constraints, transactions, or human review gates
```

Canonical truth is created only by deterministic domain validation, database constraints, audited transactions, and human approval where risk demands it.

High-risk decisions must store an evidence-backed decision record:

```text
decision_type
decision_result
decision_state: proposed | approved | rejected | needs_review
decided_by: system_rule | human | ai_proposal
policy_version
source_event_ids
source_media_ids
source_record_ids
model_version
confidence
reviewer_id
decided_at
```

Examples:

```text
identity merge:
  AI can create identity_match_proposal.
  Risky merges require human approval before canonical identity changes.

sale or festival eligibility:
  Eligibility is calculated for the delivery/festival date from lifecycle status, health ledger, treatment withdrawal periods, pregnancy/breeding state, ownership locks, and booking state.
  AI can flag risks, but policy code decides.

booking:
  Double-booking protection is enforced by Postgres transaction/constraint/lock, not by AI or client checks.

weight and pricing:
  Pricing uses trusted-weight policy: accepted readings, device/source confidence, recency window, outlier rejection, and source evidence.
  AI can explain or flag anomalies, not invent the price.

ambiguous cases:
  unresolved identity, eligibility, proof, treatment, or pricing cases become needs_review.
```

## Infra Decisions

```text
runtime:
  Cloud Run for API and workers.
  Prod API keeps min instances >= 1 for warm field UX.

database:
  Cloud SQL Postgres for canonical state.
  Local Docker Postgres for developer machines.
  Staging is production-like and stopped when idle; it is not a toy DB.

streaming:
  Pub/Sub is the streaming backbone.
  No Kafka or Redpanda unless a specific Pub/Sub wall is proven.

scheduling:
  Postgres due-date tables are calendar truth.
  Sweepers generate due work idempotently.
  Cloud Tasks handles near-term retries/reminders.
  No Temporal for SOP/vaccination calendars.

storage:
  GCS for videos/photos/raw archives.
  Signed URLs for direct upload/download.
  API never proxies video bytes.
```

## Rejected For The Core

```text
Snowflake:
  redundant warehouse beside BigQuery.

Kafka/Redpanda:
  redundant bus beside Pub/Sub for this GCP-centered system.

Temporal:
  wrong default for calendar/state-machine SOP work.
  Only reconsider for future compensating sagas with real rollback complexity.

Google Forms/Typeform:
  migration/public lead tools only.

Form.io/SurveyJS as runtime:
  not canonical, not mobile/offline/domain-authoritative.
```

## Execution Risks To Design Explicitly

```text
repeat_for_each_goat:
  one form can fan out to many goat events, proof records, and verification records.
  Must support partial completion, offline resume, per-goat idempotency, and batch submit.

server-authoritative gates:
  block_submission_if and requires_supervisor_if are UX hints offline but final decisions server-side.
  Server rechecks live state and permissions at submit.

sweeper:
  chunked by park/date.
  idempotent generation keys.
  no full-herd in-memory scans.

media:
  compression, lifecycle, proof hashes, CDN/cache for frequently viewed proof.

verification:
  AI triage, confidence routing, trust scores, and sampling are required to control human labor.
```
