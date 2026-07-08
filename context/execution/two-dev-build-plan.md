# Goat OS Two-Developer Build Plan

This is the practical execution plan for two developers. These are delivery phases, not weak product versions. The foundation choices remain production-grade from the start: real contracts, real auth boundary, real Postgres schema, real outbox, real form DSL, real analytics path.

For the product feature rollout in plain Goat OS language, read:

```text
context/product/goat-os-feature-phases.md
```

## Current State

### Present And Usable

```text
context/
  final architecture docs
  forms/SOP decision
  analytics/infra decision
  frontend/mobile/backend adapter decision
  agent context docs
  env/load/doc hygiene plan

dashboard/
  reusable CEO dashboard UI
  Next.js 14, TypeScript, Tailwind, Recharts, TanStack Query
  pages for counts, mortality, births, feed, MIS, infra, health, vaccination, sales, etc.
  currently queries BigQuery directly
  keep live repo untouched; create a new goatos/apps/admin-web snapshot for changes

vgoats-dashboard/
  reusable investor/reduced dashboard UI
  keep live repo untouched; create a new goatos/apps/investor-web-shadow snapshot for validation

procurement_app/
  reusable React Native camera/video/upload/team/SOP overlay ideas
  currently procurement-specific and Firebase/Firestore/GCS-coupled

slack-automation-scripts/
  legacy source for current SOP/form/workflow discovery
  reference only, not canonical future workflow

root .gitignore
  safe ignore rules for nested repos, real data, secrets, generated junk
```

### Missing

```text
root git meta-repo
goatos/ main build repo
contracts/openapi
contracts/jsonschema
Go backend modular monolith
Postgres schema/migrations
app APIs
server-side auth/RBAC
form DSL compiler/validator/runtime contract
mobile task-first app shell
mobile offline SQLite submission queue
signed media upload/finalize APIs
outbox relay to Pub/Sub
Cube semantic model
Tinybird/BQ production analytics wiring
k6 load tests
dev/stg/prod infra
security cleanup for Slack tokens and dashboards
```

## What To Use

```text
Backend
  Go modular monolith
  Postgres / Cloud SQL
  sqlc or equivalent typed SQL
  Goose/Atlas/Flyway-style migrations
  OpenAPI for REST app APIs
  JSON Schema for form DSL/events/submissions
  Pub/Sub for event fan-out
  GCS signed URLs for media

Frontend dashboard
  Next.js
  TypeScript
  Tailwind
  Recharts
  TanStack Query
  generated API clients

Mobile
  React Native CLI, not Expo
  TypeScript
  React Navigation
  Vision Camera
  SQLite for task/form/submission queue
  MMKV for small session/config cache
  generated API clients

Analytics
  BigQuery for warehouse/history
  Tinybird for hot telemetry/live APIs
  Cube as governed semantic layer
  dbt Core for transforms/tests only
  Metabase for internal exploration

Testing
  Go tests
  contract tests from OpenAPI/JSON Schema fixtures
  Playwright for dashboards
  React Native unit/integration tests where practical
  k6 for load/stress
```

## Team Split

### Developer A: Core Backend / Data / Contracts

Owns:

```text
goatos/ repo skeleton
OpenAPI contracts
JSON Schema contracts
Go backend modules
Postgres migrations
auth/RBAC
tasks/SOP/forms/vaccination/verification/media APIs
outbox relay
load-test fixtures and synthetic data generator
dev/stg/prod infra skeleton
CI/CD pipeline
IAM/service-account/secret boundaries
```

### Developer B: Frontend / Mobile / Analytics UI

Owns:

```text
dashboard contract extraction
dashboard auth/RBAC gating
analytics-client package
role-aware dashboard shell
mobile app shell from procurement_app
form runner UI
camera/media adapter reuse
offline queue UI states
Playwright smoke tests
```

Shared:

```text
contract reviews
form DSL examples
vaccination workflow acceptance test
security cleanup verification
load-test acceptance
```

Rule: Developer B does not wait for full backend completion. Use generated clients plus mock server/fixtures from contracts. Developer A does not wait for final UI. Use curl/k6/contract tests against APIs.

## Phase 0: Stabilize The Workspace

Goal: stop losing work and close live security holes.

Developer A:

```text
git init root meta-repo
commit context/ + root AGENTS.md + CLAUDE.md + .gitignore
create goatos/ repo
move context/ into goatos/context/
create initial contracts/ directories
create CI skeleton: lint/typecheck/test/contract-validate
create infra skeleton: dev/stg/prod config layout, service-account plan, Secret Manager plan
```

Developer B:

```text
audit dashboard routes and response shapes
list exact pages/components to salvage
create fresh dashboard snapshots/clones inside goatos/apps/
audit mobile screens/services to salvage
mark old repos reference-only/live-untouched in agent docs
```

Both:

```text
rotate Slack tokens
remove hardcoded token usage from scripts after rotation
gate dashboard deployments or disable sharing until auth exists
```

Exit criteria:

```text
context is versioned
goatos/ exists
Slack tokens rotated
dashboard exposure decision made
work split is clear
CI runs on pull request
infra/env ownership is assigned
```

## Phase 1: Contracts And Skeleton

Goal: make web, mobile, and backend build against the same contracts.

Developer A:

```text
create contracts/openapi/app-api.yaml
create contracts/openapi/analytics-api.yaml
create contracts/jsonschema/form-dsl.schema.json
create contracts/jsonschema/form-submission.schema.json
create contracts/jsonschema/domain-event-envelope.schema.json
create contracts/jsonschema/decision-record.schema.json
scaffold Go modules: identity, tasks, forms, media, verification, vaccination, permissions, outbox
add Postgres migrations for core tables
add auth provider port and Firebase/OIDC-compatible adapter shape
run migration spike with a real XLSX sample against draft identity schema
create dirty-data quarantine/reconciliation shape before schema hardens
validate CI: Go tests, generated clients, schema validation, migration apply
```

Developer B:

```text
create generated TS client workflow
create dashboard analytics-client wrapper
create mobile api-client wrapper
create contract-backed local test API fixtures for dashboard/mobile
extract reusable chart/UI components
create mobile task-first navigation skeleton
```

Exit criteria:

```text
contracts generate TS clients
Go server compiles
dashboard can call local contract-backed analytics API fixture
mobile can call local contract-backed app API fixture
first DB migrations apply locally
real XLSX sample exposes identity/tag edge cases
dirty rows have a quarantine path
CI blocks broken contracts/migrations
AI proposal and decision records are modeled before AI features are trusted
```

## Phase 2: Vaccination Task/Form/Proof Loop

Goal: one real Goat OS workflow from assigned task to verified canonical event.

Developer A:

```text
implement task generation and assignment APIs
implement minimal workforce roster for vaccination campaign assignment
implement absence/backfill rule for assigned vaccination tasks
backfill by vaccinator skill, park/shed scope, current load, and task risk
notify new assignee and park head on reassignment
escalate to park head when no eligible backup exists
implement form definition publish/read APIs
implement vaccination submission API
implement server-side form_version validation
implement idempotency
implement media signed upload/finalize APIs
implement verification record creation
include verification routing fields: method, confidence, sample rate, reviewer source
implement outbox event write in same transaction
wire FCM notification path for assigned/overdue/rejected tasks
```

Developer B:

```text
build mobile task list
show assigned operator/team/park-head context on tasks
build mobile form runner for vaccination fields
build goat lookup/manual tag input
reuse/adapt camera proof capture
implement signed media upload client
build submission queue states: pending, uploading, submitted, failed, retry
build operator entry log screen
build simple park-head view of assigned/overdue vaccination tasks
build field-usability basics: Telugu/Hindi-ready labels, icon-first controls, large touch targets, sunlight-safe contrast
support low-literacy UX patterns: minimal text, visual confirmation, optional voice/audio prompts where useful
```

Shared acceptance test:

```text
admin publishes vaccination SOP
task assigned to operator from roster
absent operator backfills to qualified backup without losing history
new assignee and park head are notified
no eligible backup escalates to park head
unmarked absence still becomes visible through overdue escalation
operator opens task on Android
operator fills form and captures video/photo proof
offline retry does not duplicate event
server creates vaccination event + verification record + outbox
park head/video verifier can approve/reject
goat passport/timeline shows result
```

Exit criteria:

```text
one vaccination workflow works end to end
duplicate submit creates exactly one typed event
proof media uses signed URL path
all writes are server-authorized
operator flow is usable on Android in field conditions
verification record can later accept AI confidence/routing without schema rewrite
tests cover submit idempotency, form validation, media finalize, and verification queue creation
```

## Phase 3: Dashboard Rewire And RBAC

Goal: keep existing dashboard UI but stop direct data coupling.

Developer A:

```text
implement analytics-api facade
implement AnalyticsGateway
wrap current BigQuery queries behind adapter where needed
define first Cube metrics for official KPIs
enforce auth principal + role/farm scope on analytics queries
keep CI contract tests for analytics DTOs and role-scoped responses
```

Developer B:

```text
freeze current dashboard response DTOs
replace direct page fetches with analytics-client calls in goatos copies only
keep old live dashboard repos untouched during validation
compare admin-web and investor-web-shadow against current live dashboards
hide/show pages based on server grants
add Playwright smoke tests for role visibility
add visual/regression smoke for critical dashboard pages
```

Exit criteria:

```text
dashboard requires login
CEO sees internal pages
investor sees sanitized subset
no official KPI bypasses governed metrics
current UI still renders
```

## Phase 3.5: Workforce Operations Build-Out

Goal: turn the minimal vaccination roster into the full Goat OS workforce
operations engine. This is not payroll HRMS. It controls goat-care ownership,
shifts, absence, backfill, handover, escalation, and accountability.

Developer A:

```text
implement workforce tables: worker, role, team, park/shed scope, supervisor/park-head links
implement shift templates and daily shift instances
implement attendance/absence records with optional photo/video proof policy
implement fallback/backfill rules by park/team/task_type/shed/cohort
select backup by skill match, scope match, current load, and task risk
notify assignee and park head on backfill
escalate when no qualified backup is available
run due_at escalation independent of whether absence was marked
implement task ownership and handover history
implement escalation policies by SLA/risk/task_type
implement audit trail for assignment, reassignment, missed work, and override
index queues by park_id, team_id, assignee_id, status, due_at, task_type
```

Developer B:

```text
build roster management screens
build shift calendar/list views
build attendance/check-in, absence marking, and backfill UI
build operator workload view
build park-head control view by team/shed/task type
build overdue/escalation views
build operator entry log and handover history
```

Exit criteria:

```text
every task has owner, scope, due time, backup path, and escalation path
absence creates backfill without deleting original assignment history
backfill uses skill/scope/load/risk selection, not random reassignment
no qualified backup escalates instead of dropping work
overdue escalation catches unmarked absence
park head can see who owns pending feeding/vaccination/health work today
operator sees only assigned/current work and entry history
all assignment/reassignment/handover actions are audited
```

## Phase 4: Data Migration And Legacy Bridge

Goal: move away from Sheets/Slack without losing current data.

Developer A:

```text
build legacy_import pipeline for XLSX/Sheets extracts
map old IDs/tags to identity records
import existing goat population and vaccination/health history where possible
build reconciliation tables for dirty/missing data
build Slack inbound bridge only through Goat OS APIs if needed
```

Developer B:

```text
build admin reconciliation UI
build import review screens
build error/DLQ repair screens
build dashboard views comparing old vs new counts
```

Exit criteria:

```text
existing herd loads into Postgres
bad rows are quarantined, not silently dropped
admin can resolve identity/tag conflicts
Slack is notification/bridge only
```

## Phase 5: Analytics, Telemetry, And AI Readiness

Goal: make analytics useful and controlled, not a BigQuery bill generator.

Developer A:

```text
outbox relay to Pub/Sub
BigQuery sink/load path
Tinybird hot telemetry path
GCS raw archive path
dbt transforms/tests
Cube metrics
unit-economics metrics: landing cost, feed cost, health/treatment cost,
mortality loss, realized margin, cost per goat/kg/load/source
observability on outbox/PubSub/BQ/Tinybird lag
```

Developer B:

```text
dashboard pages cut over page by page
Metabase internal exploration connected to governed marts
AI analyst access limited to Cube/clean facts
analytics UI cost/error states
```

Exit criteria:

```text
events flow from Postgres outbox to analytics
Cube owns official metrics
dashboard no longer owns SQL definitions
BigQuery guardrails are configured
Tinybird used for hot telemetry/live APIs
```

## Phase 6: Load, Security, And Release Hardening

Goal: prove the system before real operational cutover.

Developer A:

```text
create synthetic data generator with real skew
create k6 load scenarios
set pass/fail SLO thresholds
run stg load tests
fix DB indexes, queues, relay lag, slow endpoints
validate IAM least privilege
```

Developer B:

```text
run dashboard Playwright smoke/regression
run mobile offline/retry tests
test role visibility
test proof upload failure states
test operator field UX on Android devices
```

Exit criteria:

```text
load tests pass thresholds
no prod write access for agents
no known public dashboard exposure
no hardcoded secrets
restore/backup plan tested
mobile handles offline/retry
```

## Phase 7: Cutover

Goal: move operators from Slack/Sheets to Goat OS safely.

Developer A:

```text
final migration run
read-only freeze or sync bridge for old Sheets
monitor API/DB/outbox/PubSub/media/analytics
prepare rollback/replay path
```

Developer B:

```text
mobile app rollout
admin dashboard rollout
training/support screens
watch error logs and user drop-offs
collect missing SOP/form cases
```

Exit criteria:

```text
operators use Android app for assigned tasks
admins use Goat OS dashboard
Slack is notification/support only
canonical writes land in Goat OS
old Sheets are no longer operational truth
```

## What To Delete Or Archive

Old root planning/archive docs were removed from the active repo after current
guidance moved into `context/`, phase docs, and skill references. Use git
history only when explicitly asked. Former archive topics included:

```text
goatos-categories.md
goatos-infra-and-context-direction.md
goatos-pluggable-architecture.md
goatos-dashboard-visibility.md
goatos-frontend-gap-analysis.md
goatos-current-system-assessment.md
existing-repos-inventory.md
existing-repos-deep-audit.md
```

When `goatos/` is created, this archive stays outside `context/` so agents do
not grep stale decisions during normal work. Keep it as history until the
authoritative context docs are committed and reviewed.

Keep:

```text
context/**
AGENTS.md
CLAUDE.md
.gitignore
```

Reference only:

```text
dashboard/
vgoats-dashboard/
procurement_app/
slack-automation-scripts/
```

These can remain as separate repos until their useful code is copied or migrated deliberately.

Out of scope for Goat OS core:

```text
website/
  public MESHA website
  URL/content was used only to understand the business context
  not part of backend/mobile/dashboard/operator execution plan
```

## Start Here

Developer A starts with:

```text
git init meta-repo
create goatos/
move context/
contracts/openapi
contracts/jsonschema
Go backend skeleton
Postgres migrations
```

Developer B starts with:

```text
dashboard response contract inventory
analytics-client wrapper
mobile task-first shell
form runner UI skeleton
camera/upload adapter extraction
```

First shared finish line:

```text
one vaccination task assigned -> executed on Android -> proof uploaded -> server validated -> verification queued -> event emitted -> dashboard visible
```
