# Goat OS Infra And Context Direction

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

## Decision

Use Google Cloud, but do not start with Kubernetes or VMs.

```text
Runtime:        Cloud Run
Database:       Cloud SQL PostgreSQL
Files/media:    Cloud Storage
Async/schedule: Cloud Tasks + Cloud Scheduler
Imports:        Cloud Run Jobs
Analytics:      BigQuery
Secrets:        Secret Manager
Images:         Artifact Registry
Push:           Firebase Cloud Messaging
Web hosting:    Firebase Hosting or Cloud Run
```

Start with a Go modular monolith:

```text
goatos-api
goatos-worker
goatos-jobs
```

Same codebase, same modules, different entrypoints.

## Environments

Use separate environments from day one.

Best setup:

```text
goatos-dev
goatos-stg
goatos-prod
```

If project creation is slow, use one GCP org/folder but still isolate resources clearly. Do not share prod DB with dev or staging.

## Environment Rules

```text
dev:
  used by engineers/agents
  cheap resources
  fake/test data allowed
  destructive reset allowed
  agents can write here

stg:
  production-like
  migrated/scrubbed sample data
  used for UAT, demos, load testing, migration rehearsals
  agents can propose/write with approval

prod:
  real Goat OS
  real workers
  real goats
  real proof videos
  real audit trail
  agents are read-only by default
  writes only through CI/CD, migrations, or approved tools
```

## Per-Environment Resources

Each environment gets its own:

```text
Cloud SQL instance/database
Cloud Storage buckets
Cloud Run services
Cloud Run jobs
Cloud Tasks queues
Cloud Scheduler jobs
Secret Manager secrets
Artifact Registry repo or image tags
BigQuery datasets
Firebase app/project config if needed
service accounts
IAM bindings
```

Example naming:

```text
goatos-dev-core-db
goatos-stg-core-db
goatos-prod-core-db

goatos-dev-proof-media
goatos-stg-proof-media
goatos-prod-proof-media

goatos-dev-api
goatos-stg-api
goatos-prod-api
```

## Deployment Shape

```text
Android Field App
  -> app-api endpoints on goatos-api

Admin Command Center
  -> app-api endpoints on goatos-api

goatos-api
  -> Cloud SQL Postgres
  -> Cloud Storage signed upload/download URLs
  -> Cloud Tasks for delayed work
  -> Outbox table for async events

goatos-worker
  -> drains outbox
  -> sends notifications
  -> schedules/reschedules tasks
  -> exports analytics events

goatos-jobs
  -> legacy import
  -> backfills
  -> reconciliation jobs
  -> one-off admin operations

analytics
  -> BigQuery datasets
  -> dashboards
  -> AI analyst later
```

## Modular Monolith Shape

```text
goatos/
├── cmd/
│   ├── api/
│   ├── worker/
│   └── jobs/
│
├── internal/
│   ├── platform/
│   │   auth, permissions, audit, outbox, notifications, config, db, clock
│   │
│   ├── identity/
│   ├── ledger/
│   ├── passport/
│   ├── locations/
│   ├── workforce/
│   ├── sop/
│   ├── tasks/
│   ├── media/
│   ├── verification/
│   ├── inventory/
│   ├── vaccination/
│   ├── reporting/
│   └── legacy_import/
│
├── migrations/
├── api/
│   OpenAPI specs
│
├── context/
│   AI/human context layer
│
└── deploy/
    Terraform or Pulumi later
```

## V1 Kernel

V1 is not the whole company. V1 is the vaccination kernel.

```text
config_engine
  vaccine schedules
  SOP templates
  assignment rules

task_engine
  task creation
  assignment
  execution
  missed/pending carry-forward
  closure

video_verification_engine
  proof upload
  Park Head ground verification
  central video verification
  approve/reject/reschedule

hr_engine
  park roster
  weekly vaccination team schedule
  absence fallback
```

V1 modules:

```text
identity
ledger
passport
locations
workforce
sop
tasks
media
verification
inventory
vaccination
reporting
legacy_import
platform
```

## Later Deployable Splits

Do not split too early. Start with one Go deployable plus worker/jobs.

Split only when pain is real:

```text
device-gateway:
  split when hardware/RFID/camera ingestion becomes hot or unreliable

commerce:
  split before real external money/customers scale

analytics:
  already separate as BigQuery/dashboard layer

ai-rnd:
  split as model services/jobs when models become real
```

## Context Layer

This must be first-class. It is not just docs.

`context/` is the shared brain for:

```text
humans
Codex
Claude
future internal AI analyst
testing agents
migration agents
support/debugging agents
```

It should live in the main repo and be versioned with code.

```text
context/
├── product/
│   v1 slice, CEO language, workflows, glossary
│
├── architecture/
│   module map, boundaries, wires, deploy shape
│
├── domain/
│   goat identity, vaccination, tasks, verification, HR roster
│
├── data/
│   schema, event envelope, table ownership, data dictionary
│
├── api/
│   OpenAPI, endpoint ownership, examples, error codes
│
├── events/
│   event catalog, payloads, idempotency rules
│
├── workflows/
│   state machines, SOP flows, retry/carry-forward rules
│
├── analytics/
│   metrics, BigQuery mappings, dashboard definitions
│
├── security/
│   roles, permissions, environments, audit rules
│
├── ops/
│   runbooks, deploy, rollback, incident response
│
└── agents/
    Codex/Claude instructions, skills, allowed actions, review checklists
```

## Agent Layer

Agents should exist at every layer, but with strict permissions.

```text
dev agents:
  can write code
  can write migrations
  can run tests
  can create sample data
  can reset dev DB

stg agents:
  can inspect
  can run migrations with approval
  can run import rehearsals
  can generate reports
  can test flows

prod agents:
  read-only by default
  can inspect logs/metrics
  can generate SQL suggestions
  can draft fixes
  cannot mutate goat truth directly
  cannot run destructive SQL
```

Never let an agent bypass Goat OS permissions.

```text
Agents use APIs/tools.
Agents do not write prod tables directly.
Agents do not mutate canonical state without approved workflow.
All agent actions are audited.
```

## Codex / Claude Skills

Create repo-local agent skills/checklists:

```text
context/agents/codex/
├── backend-module-skill.md
├── migration-skill.md
├── api-contract-skill.md
├── test-skill.md
├── code-review-skill.md
└── incident-debug-skill.md

context/agents/claude/
├── product-review-skill.md
├── workflow-review-skill.md
├── schema-review-skill.md
└── docs-review-skill.md
```

Each skill should say:

```text
what context to read
what files it may edit
what commands to run
what invariants not to break
what final checklist to produce
```

## AI Context Rules

The context layer is authoritative for definitions.

```text
"active goat"
"due vaccination"
"verified task"
"missed task"
"closed task"
"Park Head verified"
"central video verified"
"sellable goat"
```

Definitions live once in `context/`, then code/tests/analytics refer to them.

Do not let:

```text
dashboard define active goat one way
backend define active goat another way
AI analyst define active goat a third way
```

## Data Isolation

Do not use one database with `environment` column.

Use separate DBs:

```text
dev DB
stg DB
prod DB
```

Reasons:

```text
prevents accidental prod writes
allows destructive dev tests
allows migration rehearsal in staging
keeps prod audit clean
keeps agents contained
```

## Storage Isolation

Proof media buckets are separate:

```text
goatos-dev-proof-media
goatos-stg-proof-media
goatos-prod-proof-media
```

Use object paths like:

```text
proof/{farm_id}/{task_id}/{media_id}.mp4
imports/{import_run_id}/source.xlsx
exports/{report_id}.csv
```

Prod media should not be copied to dev unless scrubbed.

## Database Direction

Use Postgres as operational truth.

```text
Cloud SQL Postgres for v1
UUID primary keys
typed event tables
ledger timeline projection
outbox table
bitemporal timestamps
idempotency keys
audit table
```

Do not use:

```text
Google Sheets as DB
Firestore as canonical goat state
BigQuery as operational DB
```

Firestore can be used later as realtime projection only if needed.

## Async Direction

V1:

```text
Cloud Tasks
Cloud Scheduler
Postgres due-task sweep
Outbox table
goatos-worker
```

Not v1:

```text
Kafka
Redpanda
Temporal
Kubernetes
```

Add those only when metrics force it.

## CI/CD Direction

Use GitHub Actions.

```text
PR:
  go test
  go vet
  lint
  migration validation
  OpenAPI validation
  frontend typecheck

merge to main:
  deploy dev

tag/release or manual approval:
  deploy stg

manual approval:
  deploy prod
```

Migrations:

```text
dev auto
stg approval
prod approval + backup + rollback plan
```

## Parallel Team Plan

For 2-3 people:

```text
Dev 1: backend kernel
  schema
  event envelope
  identity
  vaccination
  tasks
  verification

Dev 2: Android/admin surfaces
  field app task list
  proof recording/upload
  admin vaccine config
  weekly roster UI
  verification UI

Dev 3: infra/import/analytics/context
  GCP dev/stg/prod
  legacy import
  reporting
  context layer
  CI/CD
```

If only 2 people:

```text
Dev 1: backend + infra
Dev 2: app/admin + import/reporting
```

## First Build Order

```text
1. Create goatos repo structure.
2. Create context/ skeleton.
3. Write v1 schema + migrations.
4. Build auth/permissions minimal.
5. Build legacy import for existing goats and vaccine stock.
6. Build vaccination config.
7. Generate vaccination tasks.
8. Build weekly roster assignment.
9. Build Android task list and proof upload.
10. Build Park Head verification.
11. Build central video verification.
12. Close/reschedule tasks.
13. Build v1 reporting.
14. Deploy dev.
15. Rehearse migration in stg.
16. Launch one park/shed in prod.
```

## Final Direction

```text
Cloud Run, not Kubernetes.
Cloud SQL Postgres, not Sheets/Firestore as truth.
Separate dev/stg/prod, not shared DB.
Go modular monolith, not microservices.
Context layer from day one, not afterthought.
Agents everywhere, but permissioned and audited.
V1 vaccination kernel first, not whole Goat OS at once.
```
