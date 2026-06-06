# Goat OS Pluggable Architecture

ARCHIVED / NOT AUTHORITATIVE.

This file is historical planning material. Do not use it as build instruction
unless a human explicitly asks for archive comparison.

## Principle

Goat OS must not be built around Firebase, BigQuery, Cloud Run, Slack, or any single vendor.

Goat OS is built around internal contracts:

```text
Auth contract
Permission contract
Storage contract
Analytics contract
Notification contract
Scheduler contract
Workflow contract
AI/context contract
```

Vendors are adapters.

```text
Firebase Auth     = auth adapter
BigQuery          = analytics adapter
Cloud Storage     = object storage adapter
Cloud Tasks       = scheduler adapter
FCM/WhatsApp/SMS  = notification adapters
```

## Architecture Style

Use ports and adapters / hexagonal architecture inside the Go modular monolith.

```text
domain module
  -> depends on interfaces

provider adapter
  -> implements interfaces

composition root
  -> wires chosen providers at startup
```

Example:

```text
vaccination/
  service.go          domain logic
  repository.go       module-owned DB interface
  events.go           typed events

platform/auth/
  principal.go        internal auth identity
  verifier.go         TokenVerifier interface
  firebase.go         Firebase adapter
  oidc.go             OIDC/Auth0/Keycloak adapter later

platform/analytics/
  sink.go             AnalyticsSink interface
  bigquery.go         BigQuery adapter
  noop.go             dev adapter
```

## Dependency Rule

```text
domain modules never import Firebase SDK
domain modules never import BigQuery SDK
domain modules never import Cloud Storage SDK
domain modules never import Slack SDK
domain modules never import vendor-specific auth SDKs
```

Only `platform/adapters/*` imports provider SDKs.

## Runtime Shape

```text
cmd/api
  wires adapters
  starts HTTP server

cmd/worker
  wires adapters
  drains outbox/scheduler/notifications

cmd/jobs
  wires adapters
  runs import/backfill/reconciliation jobs
```

The domain code is the same. Only configuration changes by environment.

## Config

Use environment-based provider config.

```text
GOATOS_ENV=dev|stg|prod

AUTH_PROVIDER=firebase|oidc|mock
ANALYTICS_PROVIDER=bigquery|noop|snowflake_later
OBJECT_STORE_PROVIDER=gcs|s3_later|local
SCHEDULER_PROVIDER=cloud_tasks|postgres_sweep
NOTIFICATION_PROVIDER=fcm|noop|whatsapp_later
```

Provider config comes from Secret Manager in deployed environments.

## Auth Boundary

Separate authentication from authorization.

```text
authentication = who are you?
authorization  = what can you do?
```

Auth provider is replaceable:

```text
Firebase Auth
Auth0
Keycloak
Cognito
Google Identity-Aware Proxy
custom OIDC
```

But Goat OS permissions are not replaceable vendor claims. They live in Goat OS.

```text
external auth provider verifies login
Goat OS maps external identity to internal user
Goat OS checks roles, parks, sheds, teams, and permissions
Goat OS audits every action
```

## Auth Interfaces

```go
type Principal struct {
    UserID          uuid.UUID
    ExternalSubject string
    Provider        string
    Email           string
    Phone           string
    DisplayName     string
    Realm           string
}

type TokenVerifier interface {
    VerifyToken(ctx context.Context, rawToken string) (ExternalIdentity, error)
}

type UserMapper interface {
    MapExternalIdentity(ctx context.Context, ext ExternalIdentity) (Principal, error)
}

type Authorizer interface {
    Can(ctx context.Context, principal Principal, permission Permission, scope Scope) error
}
```

Provider adapters:

```text
FirebaseTokenVerifier
OIDCTokenVerifier
MockTokenVerifier
```

Internal services use only:

```text
Principal
Authorizer
```

They do not know whether login came from Firebase, Google, Auth0, or Keycloak.

## Permission Model

Keep this inside Postgres.

```text
users
external_identities
roles
permissions
role_permissions
user_roles
parks
sheds
user_scope_assignments
audit_log
```

Example:

```text
task.execute.vaccination
task.verify.ground
task.verify.video
vaccination.config.manage
roster.manage
goat.read
goat.import
report.view
```

Scope examples:

```text
global
park:CBE
park:CPT
shed:Yashoda-8
team:vaccination
```

## Auth Realms

Do not mix internal Goat OS users with commerce/investor users.

```text
goat_ops realm:
  operators
  supervisors
  park heads
  central verifiers
  vets
  admins
  management

commerce realm later:
  investors
  buyers
  donors
  partners
```

Same auth provider can technically serve both, but Goat OS treats them as separate realms.

```text
customer token cannot access goat_ops permissions
worker token cannot access commerce ownership unless explicitly mapped
```

## Analytics Boundary

Analytics should be replaceable too.

Do not let domain modules call BigQuery directly.

Correct pattern:

```text
domain typed event
  -> outbox
  -> analytics exporter
  -> analytics sink adapter
  -> BigQuery/Snowflake/etc
```

## Analytics Interfaces

```go
type AnalyticsEvent struct {
    EventID      uuid.UUID
    EventType    string
    OccurredAt   time.Time
    RecordedAt   time.Time
    SourceModule string
    EntityType   string
    EntityID     uuid.UUID
    Payload      json.RawMessage
    Version      int
}

type AnalyticsSink interface {
    Publish(ctx context.Context, events []AnalyticsEvent) error
}
```

Provider adapters:

```text
BigQuerySink
SnowflakeSink later
PubSubSink later
LocalFileSink for dev
NoopSink for tests
```

Domain modules never know which analytics tool receives the event.

## Metric Definitions

Metric definitions live in the context layer, not inside a dashboard.

```text
context/analytics/metrics/active_goat.md
context/analytics/metrics/due_vaccination.md
context/analytics/metrics/verified_task.md
context/analytics/metrics/missed_task.md
```

Code, SQL, dashboards, and AI analyst refer to the same definitions.

## Storage Boundary

Proof videos must not depend directly on GCS in domain code.

```go
type ObjectStore interface {
    CreateUploadURL(ctx context.Context, object ObjectRef, opts UploadOptions) (SignedURL, error)
    CreateDownloadURL(ctx context.Context, object ObjectRef, opts DownloadOptions) (SignedURL, error)
    Delete(ctx context.Context, object ObjectRef) error
}
```

Provider adapters:

```text
GCSObjectStore
S3ObjectStore later
LocalObjectStore for dev
```

Domain stores:

```text
bucket logical name
object key
checksum
content type
size
duration
```

Not vendor-specific public URLs as truth.

## Scheduler Boundary

Do not hardcode Cloud Tasks into task engine.

```go
type Scheduler interface {
    Schedule(ctx context.Context, job ScheduledJob) error
    Cancel(ctx context.Context, jobID string) error
}
```

Provider adapters:

```text
CloudTasksScheduler
PostgresSweepScheduler
TemporalScheduler later if needed
```

V1:

```text
Cloud Tasks + Postgres due sweep
```

Later:

```text
Temporal for complex flows only if needed
```

## Notification Boundary

```go
type Notifier interface {
    Send(ctx context.Context, notification Notification) error
}
```

Provider adapters:

```text
FCMNotifier
WhatsAppNotifier later
SMSNotifier later
EmailNotifier later
SlackNotifier only for migration/transition
NoopNotifier for tests
```

Task engine says:

```text
notify assignee
notify verifier
notify park head
```

It does not know if the notification goes through FCM, WhatsApp, SMS, or email.

## AI / Context Boundary

AI tools do not directly mutate Goat OS truth.

```text
Codex
Claude
AI analyst
model workers
```

They interact through:

```text
context layer
read APIs
approved tools
proposal/claim tables
verification queue
CI/CD
```

AI output is:

```text
proposal
claim
draft
analysis
review
```

AI output is not:

```text
canonical goat event
permission bypass
direct prod DB mutation
```

## Agent Interfaces

Agents get tools by environment.

```text
dev:
  code write
  migration write
  test data create
  dev DB reset

stg:
  read
  test
  migration proposal
  import rehearsal
  approved writes

prod:
  read only
  diagnostics
  SQL draft
  incident summary
  no direct mutation
```

## Module Ownership

Each module owns its tables and interfaces.

```text
vaccination owns vaccination tables
tasks owns task tables
verification owns verification tables
identity owns goat identity tables
workforce owns user/roster/team tables
platform owns auth/audit/outbox/config rails
```

Cross-module access happens by:

```text
module service interface
typed event
read projection
```

Not by reaching into another module's tables.

## Example: Vaccination Without Vendor Lock-In

```text
operator logs in with Firebase today
operator could log in with Keycloak later
Goat OS receives Principal either way

operator uploads proof to GCS today
could upload to S3 later
Goat OS stores ObjectRef either way

vaccination event exports to BigQuery today
could export to Snowflake later
Goat OS emits AnalyticsEvent either way
```

The vaccination module does not change.

## What To Avoid

Do not make this mistake:

```text
vaccination imports Firebase SDK
tasks imports Cloud Tasks SDK everywhere
media stores raw GCS URLs as canonical truth
dashboard SQL defines business terms differently from backend
agent scripts directly update prod tables
```

Correct:

```text
domain -> internal interface
adapter -> vendor SDK
context -> definitions
outbox -> events
audit -> all mutations
```

## Recommended V1 Providers

Use these now:

```text
Auth:          Firebase Auth adapter
Permissions:   Goat OS Postgres
Database:      Cloud SQL Postgres
Storage:       GCS adapter
Analytics:     BigQuery adapter
Scheduler:     Cloud Tasks + Postgres sweep adapter
Notifications: FCM adapter
Secrets:       Secret Manager
Runtime:       Cloud Run
```

But keep these provider interfaces clean so you can swap later.

## Final Rule

```text
Goat OS owns the domain.
Providers only provide infrastructure.
```

If a provider changes, Goat OS should keep the same:

```text
domain events
state machines
permissions
audit trail
API contracts
context definitions
```
