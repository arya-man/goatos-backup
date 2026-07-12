# Environment, Load Testing, And Doc Hygiene Plan

This document closes the execution gaps around environments, load testing, git layout, context placement, and which markdown files remain authoritative.

## Final Review

The architecture decisions are settled. The open work is operational:

- root version control without accidentally committing nested repos or real goat data
- dev/stg/prod isolation
- load and stress testing before production cutover
- security cleanup for live secrets and public dashboards
- doc hygiene so agents do not follow stale planning notes
- `context/` placement once the real `goatos/` repo exists

## Context Placement Decision

Final target:

```text
goatos/
  context/        canonical architecture and agent context
  backend/
  apps/
  packages/
```

Current temporary state:

```text
context/
  canonical until goatos/ exists
```

When `goatos/` is created, move `context/` into `goatos/context/` and update root `AGENTS.md` to point there. The `mesha/` root can remain a meta workspace for existing repos and archived source material, but the main build repo should carry its context beside code.

Do not keep two live context folders. One canonical context only.

Historical planning/archive notes must not live under `context/` now that
`goatos/` exists. Agents read `context/` by default, so stale notes inside that
tree can revive dead decisions. The old archive was removed from the active
repo; use git history only when a human explicitly asks for archaeology.

Active-doc grep:

```bash
rg "term" context
```

Historical comparison, only when explicitly requested, should use git history
or source material rather than active build docs.

## Git Strategy

Current state:

- `<mesha-workspace>` is not a git repo.
- Existing app folders already have their own `.git` directories.

Correct move:

```text
<mesha-workspace>    meta-repo
  tracks: context/, root AGENTS.md, root CLAUDE.md, root planning index
  ignores: nested app repos, data files, secrets, build artifacts
```

Do not run a blind `git add .`.

Root `.gitignore` should ignore:

```text
dashboard/
vgoats-dashboard/
procurement_app/
website/
slack-automation-scripts/

*.xlsx
*.xls
*.csv
*.env
*.env.*
service-account-key.json
google-services.json
GoogleService-Info.plist
**/google-services.json
**/GoogleService-Info.plist
**/service-account-key.json

node_modules/
.next/
dist/
build/
.turbo/
.codex-goatos-render/
.code-review-graph/
```

When `goatos/` is created, it should become its own real repo. Do not mix nested old repos into it unless doing a deliberate monorepo migration.

## Dev / Stg / Prod Setup

Use isolated environments. Do not share databases, buckets, queues, or analytics datasets across envs.

```text
dev
  local Docker + dev GCP resources
  fake/synthetic data by default
  agent write access allowed only here
  cheap, resettable, fast iteration

stg
  production-like topology
  migration rehearsal
  load/stress testing
  masked/synthetic large data
  right-sized resources that can stop when idle
  no writes to prod systems

prod
  real users, real goat data, real media
  least-privilege service accounts
  agents read-only
  deploy through CI/CD service account only
  break-glass human access audited
```

Recommended GCP layout:

```text
goatos-dev
goatos-stg
goatos-prod
```

Each environment owns:

```text
Cloud Run services
Cloud SQL / Postgres
GCS buckets
Pub/Sub topics and subscriptions
BigQuery datasets
Tinybird workspace or datasource namespace
Cube deployment/config
Secret Manager secrets
Firebase Auth project/config if Firebase Auth is used
service accounts and IAM bindings
```

Naming example:

```text
goatos-api-dev
goatos-api-stg
goatos-api-prod

goatos-media-dev
goatos-media-stg
goatos-media-prod

goatos_events_dev
goatos_events_stg
goatos_events_prod
```

## IAM Rules

Markdown says intent; IAM enforces reality.

```text
developer human
  dev editor
  stg limited deploy/debug
  prod read-only by default

agent service account
  dev write
  stg read + controlled test-write if explicitly needed
  prod read-only, no mutation grants

ci deploy service account
  deploy-only
  no broad owner/editor

runtime service account
  only the resources each service needs
```

Prod write access should require explicit break-glass flow and audit trail.

## Load And Stress Testing

Use `k6` as the standard HTTP/API load tool.

Use synthetic data generators for Goat OS domain volume:

```text
50k goats
1M goats
5k sales/day equivalent
daily vaccination campaigns
large task assignment waves
proof media upload bursts
duplicate/offline replay storms
device telemetry firehose
```

Synthetic data must preserve real-world skew. Do not generate uniform fake data.

The generator should model:

```text
goats per farm/park/shed distribution
goats per cohort distribution
events per goat distribution
high-activity goats vs quiet goats
missing/dirty/duplicate tag patterns from the current XLSX
operator/team assignment skew
vaccination campaign bursts
media size distribution
offline replay bursts by park/network quality
device telemetry bursts by gateway/shed
```

Uniform data hides hot partitions, overloaded sheds, bad indexes, and skewed queues.

Core test scenarios:

```text
operator sync
  login -> fetch weekly tasks -> fetch pinned form_version -> submit forms offline/online

vaccination campaign
  generate due tasks -> assign by roster -> execute -> media upload -> verification queue

media proof
  signed URL creation -> parallel video upload -> finalize -> verifier read

idempotency storm
  same submission retried many times -> exactly one typed event

outbox relay
  many domain events -> Pub/Sub -> BigQuery/Tinybird/GCS -> lag stays bounded

dashboard traffic
  CEO/internal dashboard pages -> analytics API -> Cube/Tinybird/BQ

device telemetry
  RFID/scale/camera/collar events -> gateway -> raw telemetry -> hot analytics

sweeper
  due-date task generation across large herd -> chunked, idempotent, bounded memory
```

Measured signals:

```text
API p50/p95/p99 latency
error rate
Postgres CPU, locks, slow queries
Cloud SQL connections
outbox pending count and age
Pub/Sub publish/subscription lag
BigQuery load failures and bytes scanned
Tinybird ingest lag
media upload failure/retry rate
DLQ count and oldest age
verification queue depth
task generation duration
mobile sync success rate
```

Every load test must have pass/fail thresholds before it starts. Initial targets should be written per scenario and tightened after baseline runs.

Example threshold shape:

```text
operator sync
  p90 <= 300 ms, p95 <= 500 ms, p99 <= 1000 ms for task/form fetch
  error rate < 0.1%

form submit
  p90 <= 300 ms, p95 <= 500 ms, p99 <= 1000 ms without media
  p99 <= 1000 ms under retry/idempotency storm
  duplicate typed events = 0

media proof
  signed URL creation p95 < 300 ms
  finalize p95 <= 500 ms, p99 <= 1000 ms
  upload failure rate < 1% excluding client network aborts

outbox relay
  oldest unsent event age < 60 s during normal load
  oldest unsent event age < 300 s during burst load
  DLQ rate < 0.1% and every DLQ item has repair metadata

dashboard analytics
  p90 <= 300 ms, p95 <= 500 ms, p99 <= 1000 ms for cached/governed metrics
  no raw BigQuery scan path for official KPIs
  BigQuery bytes scanned stays under configured query budget

device telemetry
  sustained ingest target defined per active gateway count
  hot analytics lag < 30 s for live operational views

sweeper
  chunked memory bounded
  duplicate generated tasks = 0
  full due-task generation completes within the configured maintenance window
```

Rules:

- Run heavy tests only in `stg`.
- Never load test prod with write traffic.
- Use masked/synthetic data.
- Every test run must produce a report: scenario, data size, concurrency, bottleneck, fix, rerun result.
- Include cost observation: BigQuery bytes, GCS egress, Tinybird ingest, Cloud Run/SQL scaling.

Frontend/mobile validation:

```text
Playwright
  dashboard route smoke tests
  auth/role visibility checks
  chart render checks

React Native
  form runner contract tests
  offline queue/idempotency tests
  camera/media adapter mocks
  Android emulator smoke tests
```

## Security Cleanup

Immediate live risks:

- hardcoded Slack `xoxb-*` tokens exist in `slack-automation-scripts`
- public dashboard deployments have exposed internal data paths

Correct order:

1. Rotate/revoke Slack tokens in Slack admin.
2. Replace hardcoded tokens with Script Properties/env.
3. Purge or archive old secret-bearing history if those repos remain shared.
4. Gate dashboard routes and API endpoints with auth/RBAC.
5. Move prod credentials behind Secret Manager and least-privilege service accounts.

Do not rely on deleting tokens from the latest code. They are already exposed in history.

## Contract Source Of Truth

Use one source per contract family:

```text
contracts/openapi/
  source for REST app APIs
  generates TypeScript clients and Go handler/server types

contracts/jsonschema/
  source for form DSL, form submissions, event payloads, DLQ repair payloads
  validates runtime payloads and fixtures

contracts/proto/
  source only for high-volume telemetry/internal service contracts when needed
```

Do not hand-maintain the same shape across Go, TypeScript, JSON Schema, and proto.

Do not create one mega universal `Goat` schema. API DTOs, domain models, event payloads, and analytics facts are related but not identical.

## Markdown Maintenance

Authoritative docs to maintain:

```text
context/README.md
context/architecture/final-architecture.md
context/frontend/final-frontend-mobile-backend-architecture.md
context/forms/final-forms-sop-engine.md
context/analytics/final-analytics-infra.md
context/agents/ai-agent-context-and-protocols.md
context/execution/next-contracts.md
context/execution/env-load-test-and-doc-hygiene.md
```

Agent entry files to maintain:

```text
<mesha-workspace>/AGENTS.md
<mesha-workspace>/CLAUDE.md
*/AGENTS.md
*/CLAUDE.md
```

Historical planning/archive docs were removed from the active tree after their
useful build guidance was folded into `context/`, phase docs, and skill
references. Do not recreate them as active build instructions. If a human asks
for archaeology, use git history or source material.

Historical source material mattered until:

- root meta-repo is initialized
- current authoritative docs are committed
- anything still useful has been folded into `/context`
- secret-bearing repos are handled separately

Delete only generated junk, build artifacts, or duplicate scratch files after git is initialized and ignored paths are verified.

## Legacy Repo Agent Status

Legacy/reference repos must say so clearly:

```text
slack-automation-scripts
  REFERENCE ONLY / FROZEN
  do not extend canonical workflows here

vgoats-dashboard
  REFERENCE / CONVERGE
  role-filtered dashboard variant, not separate long-term product

procurement_app
  REFERENCE BASE
  reuse camera/upload/team ideas, not canonical Goat OS app
```

## Next Execution Order

```text
1. Initialize root meta-repo with safe .gitignore.
2. Rotate Slack tokens and scrub scripts to env/Script Properties.
3. Gate dashboard routes and API endpoints.
4. Create goatos/ repo and move context/ into goatos/context/.
5. Create contracts/openapi and contracts/jsonschema.
6. Draft form DSL grammar, AnalyticsEvent/event envelope, and operational schema.
7. Scaffold Go modular monolith and generated TS clients.
8. Build one complete vaccination/task/form/proof/verification loop.
9. Add k6 load scenarios and synthetic data generator.
```

Stop expanding architecture before these are started.
