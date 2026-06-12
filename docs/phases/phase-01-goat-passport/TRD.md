# Phase 1 TRD: Goat Passport And Herd Registry

Status: draft for review.

## Technical Summary

Phase 1 implements the canonical identity foundation for Goat OS.

Core technical rule:

```text
goats.goat_id is immutable truth.
goat_identifiers stores old tags, RFID, visual tags, source row IDs, purchase IDs, and temp IDs.
identity_reconciliation records proposals, conflicts, and human decisions.
merged goats are tombstoned/redirected, never deleted.
```

No downstream module should treat a tag as the goat primary key.

## Modules Touched

Backend modules:

```text
backend/internal/identity
backend/internal/locations
backend/internal/media       # only if identity proof/photo refs are needed
backend/internal/permissions
backend/internal/legacy_import
backend/internal/reporting   # identity count projections only
backend/platform/outbox
backend/platform/db
backend/platform/observability
```

Frontend/mobile:

```text
apps/admin-web
apps/operator-mobile
packages/api-client
packages/rbac
```

Contracts:

```text
contracts/openapi/app-api.yaml
contracts/openapi/admin-api.yaml
contracts/openapi/analytics-api.yaml
contracts/jsonschema/domain-event-envelope.schema.json
contracts/jsonschema/decision-record.schema.json
```

## Phase Architecture Invariants

These rules apply to this phase and every future phase. They are part of the
implementation contract, not style preferences.

### Modular Monolith

```text
backend/internal/identity      owns goat identity and identifier rules
backend/internal/locations     owns location hierarchy and movement references
backend/internal/legacy_import owns import staging and reconciliation inputs
backend/internal/permissions   owns RBAC/scope checks
backend/internal/reporting     owns identity count projections only
backend/platform/outbox        owns reliable event publishing
```

Rules:

```text
modules own their tables and repositories
one module must not write another module's tables directly
cross-module access goes through public package APIs or explicit interfaces
handlers stay thin; domain services hold business rules
domain code must not import cloud/vendor SDKs directly
```

### Ports And Adapters

Every replaceable tool is accessed through a port/interface first:

```text
auth
object/media storage
analytics publishing
Pub/Sub/outbox relay
RFID/device lookup
AI identity suggestion
notifications
dashboard projection reads
external legacy sources
```

Adapter rules:

```text
SOLID dependency inversion is mandatory: domain services depend on interfaces
vendor SDK code lives in adapters/platform packages only
domain services depend on ports, not concrete vendors
adapter selection happens in bootstrap/factory wiring, not inside domain logic
each external adapter must have a fake/test adapter for contract and replay tests
replacing Firebase/Auth0, BigQuery/Tinybird, GCS/S3, or device vendors must not
rewrite identity business logic
```

### Mutation, Audit, Reversal, And Cache Rules

Phase 1 is not a disposable import script. Every command must be designed for
large herds, operator mistakes, network retries, and later genetics/R&D use.

Mutation rules:

```text
all create/update/merge/link/void actions go through command services, not direct table writes from handlers
each command validates permissions, idempotency, current row_version, and domain invariants before mutation
canonical mutation, identity_decision when applicable, typed event, audit_log, outbox row, and idempotency record commit in one transaction
no destructive hard delete for goat identity data; use inactive/voided/retired/merged states with audit trail
```

Reversal rules:

```text
wrong action is corrected by a new decision/event, not by editing history away
merge reversal uses unmerge/correction flow and keeps the original merge decision visible
wrong temporary goat creation uses void/merge/unmerge correction depending on evidence
identifier mistakes retire/dispute/reassign identifiers with decision records
admin UI must show before/after evidence for any risky correction
```

Cache/projection rules:

```text
goats table keeps current placement/status caches for fast lookup
goat_identity_counters keeps scoped counts so dashboards do not scan full herd tables
cache/projection rows are rebuildable from canonical goats + events + decisions
import jobs rebuild projections in chunks; steady-state writes update hot projections incrementally
API responses include freshness where projections can lag
stale/rebuilding projections never cause dashboards to query raw full-herd tables
```

Scale rule:

```text
design for 100k to 1M goats from the first implementation: scoped indexes, pagination, idempotency, outbox, counters, partitioned event/audit tables, chunked imports, and load tests are mandatory
```

### API And Protocol Decision

For Phase 1 product clients:

```text
admin web       -> REST/JSON -> OpenAPI-generated TypeScript client
operator mobile -> REST/JSON -> OpenAPI-generated TypeScript client
dashboards      -> analytics/app facade APIs, not raw DB/vendor reads
```

Contract rules:

```text
OpenAPI owns request/response contracts for app/admin/analytics APIs
JSON Schema owns event envelopes, decision records, import policies, and DLQ
repair payloads
generated clients are committed or generated in CI before frontend/mobile use
once generated clients exist, contract drift checks must regenerate them and
fail on diff
```

gRPC/protobuf decision:

```text
do not expose direct gRPC to browser or React Native clients in Phase 1
use protobuf only if an internal high-volume workload needs it, such as future
device-gateway telemetry or a split service behind the app API boundary
adding direct client gRPC requires a written ADR that replaces this decision
```

### Analytics And Observability Slice

The final analytics stack is defined in
`context/analytics/final-analytics-infra.md`. Phase 1 must connect to that
architecture without making BI tools part of canonical identity writes.

Phase 1 owns these analytics primitives:

```text
goat_identity_events
outbox_messages
goat_identity_counters
GET /analytics/identity/counts
dashboard freshness fields: as_of_recorded_at, is_rebuilding
```

Tool boundaries:

```text
Postgres
  canonical identity truth and operational projections

outbox -> Pub/Sub
  event fan-out path for analytics, notifications, and workers

BigQuery
  historical identity facts/marts after events are exported

Tinybird
  not required for normal Phase 1 identity counts; reserved for hot/live
  telemetry-style paths when that volume exists

Cube
  single semantic layer for official metrics when analytics stack is wired;
  Phase 1 count definitions must be compatible with Cube metric definitions

Metabase
  internal read-only exploration over governed marts/Cube metrics; no product
  write path and no independent KPI definitions

Grafana
  optional observability dashboard later; Phase 1 instrumentation uses
  OpenTelemetry with Google Cloud Monitoring/Logging/Trace first
```

Direct usage rules:

```text
identity domain code must not import BigQuery/Tinybird/Cube/Metabase/Grafana SDKs
dashboard pages must call analytics/app facade APIs, not raw BigQuery or Cube SQL
official KPI definitions must not be duplicated in dashboard code or Metabase
```

Enforcement note:

```text
These boundaries are phase requirements now.
Current check-boundaries.sh covers basic safety checks only.
Add Go depguard and TypeScript dependency-cruiser style checks before treating
green CI as proof that module/vendor/analytics boundaries are mechanically
enforced.
```

### Transaction And Scale Rules

Every canonical identity mutation must commit these together:

```text
canonical table change
typed identity event
audit record
outbox message
idempotency key
```

Scale rules:

```text
all list/search APIs are scope-filtered and paginated
no full-herd scans on request path
background sweeps are chunked and idempotent
partitions must be pre-created with a default partition backstop
SLO/load tests must include 1M goats, 3M identifiers, dirty data skew, import
replay, duplicate submits, and dashboard projection reads
```

## Data Model

### `tenants`

Isolation boundary for row-level security and future data residency. This is
not the same thing as an organization/vendor/franchisee.

```text
tenant_id uuid primary key
name text not null
status text not null
created_at timestamptz not null
updated_at timestamptz not null
```

Reporting support tables:

```text
goat_identity_counter_projection_state:
  tenant_id primary key
  last_processed_recorded_at timestamptz null
  last_processed_event_id uuid null
  rebuild_required boolean not null default false
  rebuild_reason text null
  updated_at timestamptz not null

goat_identity_counter_processed_events:
  tenant_id uuid not null
  event_id uuid not null
  event_recorded_at timestamptz not null
  event_type text not null
  outcome text not null check applied|noop
  processed_at timestamptz not null
  primary key (tenant_id, event_id, event_recorded_at)
  foreign key (tenant_id, event_id, event_recorded_at)
    references goat_identity_events(tenant_id, identity_event_id, recorded_at)
```

Rules:

```text
seed one tenant for current Mesha operations
tenant_id is copied onto tenant-owned rows so RLS can filter without fragile joins
parties and orgs are global actors and do not carry tenant_id
future party visibility within a tenant uses membership/access tables, not tenant_id on parties
```

### `parties`

Global actor spine for anyone that can own, custody, sell, lend, operate, or be
referenced as a counterparty. This keeps ownership/custody extensible without
polymorphic foreign keys.

```text
party_id uuid primary key
party_type text not null
display_name text not null
status text not null
created_at timestamptz not null
updated_at timestamptz not null
```

Allowed initial party types:

```text
org
person
token_pool
system
```

### `orgs`

Organization subtype of `parties`. Shared primary key: the org id is the party
id. Do not create a second `org_id`.

```text
party_id uuid primary key references parties(party_id)
org_type text not null
legal_name text null
status text not null
created_at timestamptz not null
updated_at timestamptz not null
```

Initial org types:

```text
mesha
farm_operator
vendor
franchisee
lender
partner
```

Seed:

```text
one Mesha party with party_type = org and org_type = mesha
```

### `goats`

Canonical goat record.

```text
goat_id uuid primary key
tenant_id uuid not null references tenants(tenant_id)
display_id text unique not null
species text not null default 'goat'
breed text null
breed_id uuid null
sex text null
approx_dob date null
age_band text null
lifecycle_status text not null
reproductive_status text null
growth_cohort_tag text null
management_stage text null
health_status text null
identity_state text not null
custodian_party_id uuid not null references parties(party_id)
current_location_id uuid null
farm_id uuid null
park_id uuid null
shed_id uuid null
cohort_id uuid null
merged_into_goat_id uuid null references goats(goat_id)
source_confidence numeric null
row_version int not null default 1
created_at timestamptz not null
updated_at timestamptz not null
created_by uuid null
```

Rules:

```text
goat_id never changes
display_id is human-facing only; default format is G-000001 style global numeric code
display_id is server-generated, immutable, searchable, and separate from goat_id
display_id must not encode current farm/location because goats can move
display_id comes from a transactional DB sequence/allocator, never count(*) or max(display_id)+1
display_id gaps are allowed; never recycle a display_id
idempotent replay of the same source mutation returns the same goat_id/display_id
display_id allocation must be safe under parallel import workers and chunked imports
tenant_id is the isolation/RLS scope and does not change during normal movement
custodian_party_id is the current operationally responsible party, not economic ownership
farm_id/park_id/shed_id/cohort_id are current placement caches for fast scoped reads
lifecycle_status is the alive/dead/sold/merged/inactive axis only
reproductive_status is the pregnancy/mother/buck/milking axis
growth_cohort_tag is the K0/K1/K2/K3/F1/F2-style cohort axis; sex remains in `sex`
management_stage is the farm-operations stage such as warmup/intake when it is not reproductive, health, or growth
management_stage is enumerated reference data, not a junk drawer; add new
values only through status_definitions/legacy_status_mappings review
health_status is the healthy/sick/ICU/quarantine/under-treatment axis
do not mash compound legacy labels into lifecycle_status
compound labels are decomposed:
  F2-Male -> growth_cohort_tag=F2; sex remains sourced from Gender evidence
  F2-Female -> growth_cohort_tag=F2; sex remains sourced from Gender evidence
  Fattening -> growth_cohort_tag=F2
  M0 -> reproductive_status=mother and management_stage=m0_post_delivery
  ICU-Non-Pregnant -> health_status=icu and reproductive_status=non_pregnant
  ICU-Kid -> health_status=icu and growth/age axis remains kid-stage when known
identity_state: clean | needs_review | disputed | merged | inactive
merged goats set merged_into_goat_id and reject normal future writes
row_version supports optimistic concurrency for admin edits
daily task assignment stays in workforce/tasks and must not be modeled as custody
```

Concurrency and atomic-update patterns:

```text
use row_version optimistic concurrency for user/admin decisions that edit a
specific identity aggregate snapshot
goat passport, identifier add/retire/dispute, correction resolve, conflict
resolve, and candidate reject use row_version or the command aggregate's
row_version to reject stale reviewer actions
when a write changes a goat identity surface, bump that goat row_version exactly
once per transaction; this is for stale-write rejection and future
projection/cache invalidation, not for numerical counting
append-only ledgers use idempotent inserts keyed by business identity plus
outbox/event linkage
progress counters, projection counters, retry attempts, and import run counts
use atomic SQL updates such as count_value = count_value + n or are rebuilt in
bounded batches; do not protect hot counters with row_version compare-and-retry
parallel import/projection workers must not contend on goat_identity_counters
hot rows; large imports rebuild grouped counter projections instead of issuing
per-row counter increments
```

Correction request resolution uses the same optimistic-concurrency rule:
`POST /admin/identity/correction-requests/{correction_request_id}/resolve`
must include the current `row_version`. The write is conditional on
`state in (open, assigned, needs_field_check)`, matching `row_version`, and a
state change. Approved/rejected/closed requests are terminal and can only be
returned by exact idempotent replay.

### `status_definitions`

Canonical status/stage reference data used by APIs and UI. These records keep
legacy operator terms searchable while giving Goat OS clean dimensions for
analytics, genetics, and R&D.

```text
status_code text primary key
axis text not null                 -- lifecycle | reproductive | growth_cohort | management | health
display_name text not null          -- UI label, for example "K0 - Newborn"
short_label text not null           -- compact chip, for example "K0"
description text null
legacy_label text null              -- familiar old label when one exists
sort_order int not null default 0
active boolean not null default true
expected_duration_days int null     -- optional hint, for example warmup or M0 review windows
created_at timestamptz not null
updated_at timestamptz not null
```

Initial examples:

```text
axis=growth_cohort, status_code=K0, display_name="K0 - Newborn", short_label="K0"
axis=growth_cohort, status_code=K1, display_name="K1 - Bottle milk training", short_label="K1"
axis=growth_cohort, status_code=K2, display_name="K2 - Milk drinking", short_label="K2"
axis=growth_cohort, status_code=K3, display_name="K3 - Weaning", short_label="K3"
axis=growth_cohort, status_code=F2, display_name="F2 - Fattening", short_label="F2"
axis=management, status_code=warmup, display_name="Warmup - Adaptation", short_label="Warmup"
axis=management, status_code=m0_post_delivery, display_name="M0 - Post-delivery mother", short_label="M0"
axis=reproductive, status_code=pregnant, display_name="Pregnant", short_label="Pregnant"
axis=reproductive, status_code=non_pregnant, display_name="Non-pregnant", short_label="Non-pregnant"
axis=reproductive, status_code=mother, display_name="Mother", short_label="Mother"
axis=reproductive, status_code=milking, display_name="Milking", short_label="Milking"
axis=reproductive, status_code=buck, display_name="Buck", short_label="Buck"
axis=health, status_code=icu, display_name="ICU", short_label="ICU"
axis=health, status_code=quarantine, display_name="Quarantine", short_label="Quarantine"
```

UI rule:

```text
operator/admin UI shows `display_name` with `short_label` chips.
legacy labels remain searchable aliases.
do not show raw dirty legacy strings as the primary UI label unless unmapped.
unmapped values show as "Needs review: <raw_label>" until mapped.
```

Genetics/R&D rule:

```text
genetics and R&D must query structured fields: breed_id, sex, lifecycle_status,
reproductive_status, growth_cohort_tag, management_stage, health_status,
age/approx_dob, growth events, health events, breeding events, and meat-yield
feedback.
they must not infer genetics from one raw legacy label such as "F2-Male".
```

### `legacy_status_mappings`

Mapping from old labels to canonical axes. This table lets Phase 1 import
known labels immediately, preserve unknown labels safely, and improve mappings
later without rewriting goat identity.

```text
mapping_id uuid primary key
source_system text not null
raw_label text not null
normalized_raw_label text not null
lifecycle_status text null
reproductive_status text null
growth_cohort_tag text null
management_stage text null
health_status text null
sex_override text null
display_status_code text null references status_definitions(status_code)
confidence text not null             -- high | medium | low
review_required boolean not null default false
notes text null
created_at timestamptz not null
updated_at timestamptz not null
```

Rules:

```text
store the raw label from source in legacy_import_rows and source evidence
map known labels through legacy_status_mappings
unknown labels do not block import; set review_required=true and keep raw_label
if a compound label implies sex, such as F2-Male/F2-Female, and the source sex
column disagrees, preserve both values and route to conflict/review; do not let
the F2 label override the source Gender value
if source Gender is blank/unknown and the only sex clue is F2-Male/F2-Female,
leave sex needs-review; do not infer sex from the F2 suffix
legacy raw label Fattening maps to growth_cohort_tag=F2
F0 is not an active stage; preserve as raw evidence and route to mapping review if encountered
sale/allocation blocking and routine task triggers are not stored here; they
belong to later status_rule_policies in sales/SOP phases
```

Indexes:

```text
(lifecycle_status)
(tenant_id, lifecycle_status)
(tenant_id, custodian_party_id, lifecycle_status)
(tenant_id, reproductive_status)
(tenant_id, growth_cohort_tag)
(tenant_id, management_stage)
(tenant_id, health_status)
(current_location_id, lifecycle_status)
(farm_id, lifecycle_status)
(park_id, lifecycle_status)
(shed_id, lifecycle_status)
(cohort_id, lifecycle_status)
(breed_id, sex, lifecycle_status)
(breed, sex)
```

### `goat_identifiers`

All identifiers attached to a goat.

```text
identifier_id uuid primary key
goat_id uuid not null references goats(goat_id)
identifier_type text not null
identifier_value text not null
normalized_value text not null
scope_key text not null
is_primary_for_goat boolean not null default false
status text not null
valid_from timestamptz not null
valid_to timestamptz null
source_system text null
source_record_id text null
normalizer_version text not null
confidence numeric null
approved_by uuid null
created_at timestamptz not null
updated_at timestamptz not null
```

Identifier types:

```text
old_tag
rfid
visual_tag
sheet_row_id
purchase_load_id
temp_field_id
external_system_id
```

Uniqueness scope examples:

```text
global
farm
park
purchase_load
source_system
import_run
unknown
```

Statuses:

```text
active
retired
disputed
duplicate
invalid
```

Constraints:

```text
unique(normalized_value) where identifier_type = 'rfid' and status = 'active'
unique(identifier_type, normalized_value, scope_key) where identifier_type in ('old_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id') and status = 'active'
unique(goat_id, identifier_type) where is_primary_for_goat = true and status = 'active'
```

This prevents two active goats from owning the same active identifier inside
the same uniqueness scope without creating a review/conflict path.

RFID should normally use `global`. Old tags may use `farm`, `purchase_load`, or
`source_system` depending on the confirmed legacy rule.

Import must not silently default an unknown identifier scope to `global`.
Unknown scope is represented explicitly:

```text
scope_key = unknown
```

Any unknown-scope identifier that would mutate canonical identity must open a
review/conflict path instead of auto-linking.

Visual tags are not globally unique by default. If the business later confirms a
visual tag uniqueness rule, it must be added as an explicit identifier policy,
not inferred from the generic identifier table.

RFID values must pass the configured format validator before they occupy global
RFID uniqueness. Invalid or suspicious values route to review/reject according
to `invalid_value_action`; they must not silently block a real future RFID.
`rfid_v1` is the Phase 1 validator policy name, but the concrete accepted
pattern is not locked until source RFID examples are supplied. Do not invent a
regex or silently reject canonical RFID writes from guessed format rules; keep
uncertain values on the configured review/reject path.

### `identifier_policies`

Versioned rules for each identifier type. Import, admin APIs, and mobile lookup
must read these policies instead of hardcoding tag scope behavior in UI or one
import script.

```text
policy_version text not null
identifier_type text not null
default_scope_type text not null
scope_required boolean not null
active_uniqueness text not null
auto_link_allowed boolean not null
primary_allowed boolean not null
unknown_scope_action text not null
missing_or_conflicting_scope_action text not null
normalizer_version text not null
format_validator_version text null
invalid_value_action text not null
created_at timestamptz not null
approved_by uuid null
```

Constraints:

```text
primary key(policy_version, identifier_type)
unknown_scope_action in ('review', 'reject')
missing_or_conflicting_scope_action in ('review', 'reject')
active_uniqueness in ('global', 'scoped', 'non_unique')
invalid_value_action in ('review', 'reject')
```

Phase 1 defaults:

```text
rfid:
  active_uniqueness = global
  default_scope_type = global
  auto_link_allowed = true only for trusted device/admin paths
  unknown_scope_action = review
  missing_or_conflicting_scope_action = review
  format_validator_version = rfid_v1
  invalid_value_action = review

old_tag:
  active_uniqueness = scoped
  default_scope_type = farm
  scope_required = true
  unknown_scope_action = review
  missing_or_conflicting_scope_action = review
  format_validator_version = none
  invalid_value_action = review

visual_tag:
  active_uniqueness = non_unique unless business locks a stricter rule
  auto_link_allowed = false by default
  unknown_scope_action = review
  missing_or_conflicting_scope_action = review
  format_validator_version = none
  invalid_value_action = review

sheet_row_id:
  active_uniqueness = scoped
  default_scope_type = source_system
  auto_link_allowed = false unless paired with a stable source-key policy
  unknown_scope_action = review
  missing_or_conflicting_scope_action = review
  format_validator_version = none
  invalid_value_action = review
```

Policy lifecycle:

```text
once used by an import run or canonical mutation, a policy_version is immutable
changing scope, normalization, or auto-link rules creates a new policy_version
legacy decisions store the policy_version that produced them
```

Every goat must have at least one active identifier. If import or field capture
cannot provide a reliable external tag, Goat OS mints:

```text
identifier_type = temp_field_id
identifier_value = display_id or goat_id-derived temporary value
scope_key = global
status = active
```

That temporary identifier keeps the goat findable, but it does not make the
identity clean. The goat stays `identity_state = needs_review` until stronger
evidence is attached.

Temporary goat rule:

```text
temporary goats are allowed before a real tag/RFID is attached, or when a tag is missing, lost, dirty, or unreadable
field-created temporary goat requires current/unknown location, created_by, reason, and photo/proof as the visual dedupe anchor
import-created temporary goat requires source_system, source_dataset, source_record_id/source_row_key, current/unknown location, and import_run_id
do not use one blank/missing identifier as shared identity for multiple temporary goats
temporary goats are never auto-clean; they remain needs_review until linked to stronger evidence
temporary goats with no durable tag/RFID/approved visual link after the configured staleness window escalate to review
linking a temporary goat to RFID/old tag/visual tag writes identity_decision + audit_log + event + outbox
wrong temporary goat creation is corrected by void/merge/unmerge decision flows, never by hard delete
```

### `locations`

Location hierarchy.

```text
location_id uuid primary key
tenant_id uuid not null references tenants(tenant_id)
location_type text not null
name text not null
parent_location_id uuid null references locations(location_id)
country text not null default 'IN'
state_region text null
district text null
pincode text null
lat numeric null
lng numeric null
timezone text not null default 'Asia/Kolkata'
status text not null
created_at timestamptz not null
```

Minimum bootstrap locations:

```text
one explicit unknown farm/scope exists for staging only
real migration into canonical goats requires tenant_id and custodian_party_id resolution
unknown current location is allowed as a location record, not an empty/null string
```

Types:

```text
farm
park
shed
cohort
pen
unknown
```

### `goat_location_history`

```text
location_history_id uuid primary key
goat_id uuid not null
from_location_id uuid null
to_location_id uuid not null
reason text null
occurred_at timestamptz not null
recorded_at timestamptz not null
actor_id uuid null
source_record_id text null
```

Location source-of-truth:

```text
goat_location_history is the audited movement log
goats.current_location_id and farm/park/shed/cohort columns are current-state caches
movement/import/location changes update history and current-state cache in the same transaction
location movement does not automatically change tenant_id or custodian_party_id
custody changes are captured separately in goat_custody_history
```

### `goat_ownership`

Temporal economic ownership ledger. This is a foundation seam only; it does not
build token, investor, lending, franchise, or billing workflows in Phase 1.
Ownership transfer workflow is not part of Phase 1; the ledger exists so the
schema does not narrow known future ownership to organizations only.

```text
ownership_id uuid primary key
tenant_id uuid not null references tenants(tenant_id)
goat_id uuid not null references goats(goat_id)
owner_party_id uuid not null references parties(party_id)
share_bps int not null
valid_from timestamptz not null
valid_to timestamptz null
status text not null
decision_id uuid null
created_at timestamptz not null
created_by uuid null
```

Rules:

```text
seed RFID DB first-import goats with Mesha owner_party_id at share_bps = 10000
do not apply the Mesha owner default to later partner-held/HF rows unless source evidence proves Mesha ownership
share_bps is integer basis points where 10000 = 100%; never use floating shares
active ownership means status = active and valid_to is null
active ownership shares for a goat must sum to 10000 basis points
enforce active-share total with a deferrable constraint trigger at commit
service transactions also pre-check the total for friendly validation errors
reconciliation job flags any goat whose active ownership total drifts from 10000
overlapping validity windows for the same owner/goat require explicit decision
ownership is economic truth; it does not imply daily task responsibility
```

### `goat_custody_history`

Temporal operational responsibility ledger. Current custody is cached on
`goats.custodian_party_id` for fast reads.
Custody transfer workflow is not part of Phase 1; this table is the schema seam
and audit target for future lending/handover flows.

```text
custody_history_id uuid primary key
tenant_id uuid not null references tenants(tenant_id)
goat_id uuid not null references goats(goat_id)
custodian_party_id uuid not null references parties(party_id)
from_location_id uuid null references locations(location_id)
to_location_id uuid null references locations(location_id)
valid_from timestamptz not null
valid_to timestamptz null
decision_id uuid null
reason text null
created_at timestamptz not null
created_by uuid null
```

Rules:

```text
seed every imported goat with Mesha custodian_party_id unless source evidence says otherwise
custody can change without ownership changing
location can change without custody changing
operator task assignment is not custody; it stays in workforce/tasks
do not build custody handover APIs or UI in Phase 1
```

### `legacy_import_policies`

Versioned import policy used to make repeated Sheet/XLSX imports deterministic.

```text
policy_version text primary key
source_system text not null
source_dataset text not null
identifier_policy_version text not null
source_key_recipe jsonb not null
source_key_recipe_version text not null
hash_recipe jsonb not null
hash_recipe_version text not null
field_diff_policy jsonb not null
auto_link_policy jsonb not null
normalizer_version text not null
status text not null
created_at timestamptz not null
approved_at timestamptz null
approved_by uuid null
```

Rules:

```text
policy_version is required on every import run
approved policy is immutable once any run starts
source_key_recipe must not use spreadsheet row number, sorted position, or export line number
hash_recipe must exclude export-volatile fields such as exported_at, formatting, row_number, and formula timestamps
field_diff_policy routes each changed field to auto_apply, review, ignore, or reject
dry-run and committed import use the same policy_version so reconciliation results are reproducible
first Phase 1 import scope is RFID DB only; tagless/event-log temporary identities are a later import pass after more animals receive RFID tags
F2-Male/F2-Female mappings must not set sex; sex comes from source Gender evidence
blank/unknown Gender plus an F2 sex suffix creates a sex_needs_review item
F2 label and source Gender disagreement creates a sex_status_conflict review item
old tag uniqueness scope is normalized old_tag_number + normalized park_code, not old_tag_number alone
historic park aliases normalize CJB -> CBE and BLR -> CPT while preserving source evidence
RFID DB shed disagreement with latest DB event uses latest DB event as current placement, preserves RFID shed evidence, and creates a reconciliation note
HF partner-held goats can be shared/pending ownership when only advance payment is confirmed; do not seed full owner ledger without stronger evidence
HF in Origin Farm/source columns means provenance/source reference only; it does not set current custody or ownership
HF in current Farm/location columns means goat is currently held at an external holding location; set temporary custodian/location evidence where supported and route ownership to review
partner-held rows must not silently seed Mesha owner_party_id @10000 unless source evidence proves Mesha ownership
```

### `legacy_import_runs`

```text
import_run_id uuid primary key
source_name text not null
source_system text not null
source_dataset text not null
source_file_ref text null
source_file_hash text null
policy_version text not null references legacy_import_policies(policy_version)
dry_run boolean not null default false
started_at timestamptz not null
completed_at timestamptz null
status text not null
row_count int not null default 0
created_goat_count int not null default 0
updated_goat_count int not null default 0
conflict_count int not null default 0
error_count int not null default 0
started_by uuid null
```

### `legacy_import_rows`

Raw-ish normalized staging rows. Do not make these canonical truth directly.

```text
legacy_row_id uuid primary key
import_run_id uuid not null
row_number int not null
source_system text not null
source_dataset text not null
source_record_id text null
source_row_key text not null
source_key_recipe_version text not null
source_row_version_hash text not null
hash_recipe_version text not null
raw_payload jsonb not null
normalized_payload jsonb not null
processing_state text not null
matched_goat_id uuid null
error_reason text null
created_at timestamptz not null
```

Constraints:

```text
unique(tenant_id, source_system, source_dataset, source_row_key, source_row_version_hash)
index(import_run_id, processing_state, row_number)
index(source_row_key)
```

Source row identity/version rules:

```text
source_row_key identifies the source record across repeated imports
source_row_key must come from a stable source/business identifier when available
spreadsheet row_number is execution metadata only and must not be used as source_row_key
source_row_version_hash detects content changes within the same source_row_key
source_row_version_hash must never be used as source identity by itself
```

Stable source key examples:

```text
preferred: source_system + source_dataset + stable source ID column
acceptable with review: source_system + source_dataset + immutable external goat code/load code
not acceptable: visible spreadsheet row number, sorted position, or export line number
```

If the source has no stable key, import policy must synthesize one from a
documented set of stable identity columns and mark the row `needs_review` when
collision risk exists. Two tagless goats with the same breed/sex/location must
not collapse into one goat because their content hash matches.

Version hash recipe:

```text
hash only a documented stable projection of meaningful normalized fields
exclude export-volatile fields: exported_at, formatting, row_number, formulas that change every export
store hash_recipe_version with the import policy
normalizer upgrades create a deliberate reprocess plan, not accidental mass changed rows
```

Field-level diff:

```text
hash difference only says "something changed"
field-level diff decides auto-apply vs review
shed/current-location change may auto-apply if movement policy allows it
breed/sex/identifier change is identity-material and requires review
source row missing from a later import is not a goat retire/delete by itself
```

States:

```text
pending
auto_linked
created_goat
needs_review
rejected
error
```

### `identity_match_candidates`

Candidate match proposals.

```text
candidate_id uuid primary key
legacy_row_id uuid null
proposed_goat_id uuid null
candidate_goat_id uuid null
match_score numeric not null
match_reasons jsonb not null
state text not null
created_by text not null
reviewed_by uuid null
reviewed_at timestamptz null
decision_id uuid null
row_version int not null default 1
created_at timestamptz not null
```

`row_version` supports optimistic concurrency for candidate review commands.
`CandidateSummary` must expose it because there is no candidate-detail endpoint
and reject requires the current candidate row version.

AI-created candidates must remain `state in ('proposed','needs_review')` and
must not be marked approved/rejected by the AI worker. Candidate approval remains
an apply-layer decision with canonical mutation semantics; candidate rejection
is an audited human/admin review action.

States:

```text
proposed
approved
rejected
needs_review
expired
```

### `identity_conflicts`

Conflict cases requiring review.

```text
conflict_id uuid primary key
conflict_type text not null
severity text not null
state text not null
identifier_type text null
identifier_value text null
goat_ids uuid[] not null
source_record_ids text[] null
evidence jsonb not null
created_at timestamptz not null
resolved_at timestamptz null
resolved_by uuid null
decision_id uuid null
row_version int not null default 1
```

Array columns are allowed only as denormalized display caches. Referential
integrity must use join tables:

```text
identity_conflict_goats(conflict_id, goat_id)
identity_conflict_source_records(conflict_id, source_system, source_record_id)
```

Conflict types:

```text
duplicate_active_identifier
missing_required_identifier
tagless_goat_review
rfid_already_linked
old_tag_reused
possible_duplicate_goat
location_mismatch
status_mismatch
```

### `identity_decisions`

Audited human/system decisions.

```text
decision_id uuid primary key
decision_type text not null
decision_result text not null
decision_state text not null
decided_by_type text not null
decided_by uuid null
policy_version text not null
source_record_ids text[] null
source_identifier_ids uuid[] null
source_goat_ids uuid[] null
source_event_ids uuid[] null
source_media_ids uuid[] null
model_version text null
confidence numeric null
reviewer_id uuid null
evidence jsonb not null
created_at timestamptz not null
approved_at timestamptz null
decided_at timestamptz null
```

Array columns are allowed only as denormalized display caches. Referential
integrity must use join tables:

```text
identity_decision_goats(decision_id, goat_id, role)
identity_decision_identifiers(decision_id, identifier_id, action)
identity_decision_events(decision_id, event_id)
identity_decision_media(decision_id, media_id)
```

For `merge_goats`, canonical `identity_decision_goats.role` values are
`survivor` for the live survivor and `merged` for each goat tombstoned by the
decision.

For redirected merges, `identity_decision_goats` stores the resolved live
survivor and newly tombstoned goat ids. The decision evidence may preserve the
requested survivor and affected goat ids for traceability, but consumers must
not treat requested ids as the authoritative merge outcome.

Decision types:

```text
create_goat
attach_identifier
retire_identifier
mark_identifier_disputed
merge_goats
batch_merge_goats
reject_match
request_field_verification
```

Conflict resolve target mapping:

```text
merge_goats + same_goat_merge
  -> identity_conflicts.state = resolved
  -> terminal; set resolved_at/resolved_by
  -> goat identity mutation; emits goat.identity.merge_approved events/outbox

reject_match + candidate_rejected
  -> identity_conflicts.state = rejected
  -> terminal; set resolved_at/resolved_by
  -> decision + audit only; no goat_identity_events or outbox row

request_field_verification + field_verification_required
  -> identity_conflicts.state = needs_field_check
  -> nonterminal; leave resolved_at/resolved_by null
  -> decision + audit only; no goat_identity_events or outbox row
  -> resolving an already needs_field_check conflict with a new idempotency key is a write conflict

mark_identifier_disputed + different_goats_identifier_disputed
  -> identity_conflicts.state = resolved
  -> terminal; set resolved_at/resolved_by
  -> goat identity mutation; selected identifiers become disputed, affected goats bump row_version, and emit goat.identifier.disputed events/outbox

create_goat + new_goat_required
  -> not implemented until ResolveConflictRequest defines the required goat creation fields
```

Decision-only conflict state changes (`reject_match` and
`request_field_verification`) intentionally do not emit `goat_identity_events`
or `outbox_messages` until a conflict-aggregate event payload/projection
contract is defined. Projections and admin queues must read canonical
`identity_conflicts` state for those transitions.

Approval authority:

```text
central admin can approve identity merges and assign which roles are allowed to approve specific decision types
farm admin can approve merge/correction decisions only when central admin grants that role and scope
park head or operator can request/recommend corrections, but cannot mutate canonical identity directly unless granted an approval role
mass approval is allowed only as a configured admin workflow after validation preview, evidence sampling, and dry-run results
batch merge approval creates one batch decision plus individual child decision records for every affected goat pair
high-risk identity merges must never be silently auto-approved by AI/import logic
AI workers must not impersonate `system_rule` or `import_policy`; governed
automation may use those actor types only for deterministic, approved policy
paths.
```

### `goat_merge_links`

Merge/tombstone mapping. Required because merging cannot delete history.

```text
merge_link_id uuid primary key
survivor_goat_id uuid not null references goats(goat_id)
merged_goat_id uuid not null references goats(goat_id)
decision_id uuid not null references identity_decisions(decision_id)
reason text not null
created_at timestamptz not null
created_by uuid not null
```

Constraints:

```text
unique(merged_goat_id)
merged_goat_id != survivor_goat_id
survivor_goat_id must resolve to a non-merged goat at decision time
```

Behavior:

```text
merged goat identity_state = merged
merged goat merged_into_goat_id = survivor_goat_id
merged goat remains readable for audit
lookup by merged goat redirects to survivor with warning
events are not physically moved or deleted
new writes must use survivor_goat_id
goats.merged_into_goat_id is the authoritative live-survivor pointer
goat_merge_links is immutable merge history and is not rewritten when
transitive redirects are flattened
```

Lookup rule:

```text
all goat lookup APIs resolve merged_into_goat_id to the live survivor
response includes redirect warning and original_goat_id
normal writes to merged goats fail with merged_goat_write_blocked
admin correction/unmerge flow is the only exception
transitive merges are flattened by resolving the live survivor before writing goat_merge_links
merge code rejects cycles and locks affected goat rows in deterministic goat_id order
```

Forward dependency for sale/allocation:

```text
Phase 8 booking/allocation must resolve any supplied goat reference through
merged_into_goat_id before checking availability or writing an allocation.
The no-double-promise invariant must be enforced against the live survivor
goat_id, not the stale merged goat_id or a legacy tag value.

Phase 8 replacement/substitution must run the same eligibility/readiness path as
fresh allocation. A substitute goat cannot bypass sale blockers, uncleared
feed-contamination exposure, promised-weight risk, or booking-date price-audit
rules.

Phase 8/P4 must also implement a scheduled and event-triggered promise
re-evaluation sweeper over open bookings/allocations. It must re-run the same
delivery-date readiness policy after critical facts change, write a
decision_record/event, and create a remediation or replacement review task when
the answer changes. The sweeper must be idempotent and paginated by
tenant/park/date/status indexes, not a full-herd scan.

Phase 1 preserves the inputs these later checks need: stable goat_id,
source_record_ids, decision_records, audit_log, status axes, identity redirects,
and goat_location_history. It does not execute customer/festival eligibility in
Phase 1.
```

Undo/correction:

```text
unmerge is administrative only
requires new identity_decision
does not delete the original merge decision
restores write eligibility only after conflict review
```

### `identity_correction_requests`

Operator/park-head correction request. These requests do not mutate canonical
identity directly.

```text
correction_request_id uuid primary key
request_type text not null
state text not null
goat_id uuid null references goats(goat_id)
identifier_type text null
identifier_value text null
farm_id uuid null
park_id uuid null
shed_id uuid null
cohort_id uuid null
description text not null
evidence jsonb not null
requested_by uuid not null
assigned_reviewer_id uuid null
decision_id uuid null references identity_decisions(decision_id)
created_at timestamptz not null
resolved_at timestamptz null
```

States:

```text
open
assigned
needs_field_check
approved
rejected
closed
```

Request types:

```text
missing_tag
tag_reused
rfid_conflict
possible_duplicate
wrong_location
wrong_status
field_verification_result
identifier_seen_but_not_attached
```

Rules:

```text
correction requests are review inputs only; they never attach identifiers directly
operator-submitted request scope is limited to the operator's granted work/location scope
rfid_conflict and tag_reused requests must include observed identifier, location context, actor, and observed_at
field_verification_result may include media/proof refs, but proof bytes still use the platform media path
```

### `goat_identity_events`

Typed event table for identity timeline.

```text
identity_event_id uuid primary key
goat_id uuid not null
event_type text not null
event_version int not null
occurred_at timestamptz not null
recorded_at timestamptz not null
actor_id uuid null
source_system text null
source_record_id text null
payload jsonb not null
decision_id uuid null
idempotency_key text not null
```

Partitioning:

```text
partition by range(recorded_at), monthly partitions
recorded_at is the partition key because it is server-side insert time
occurred_at remains the real-world event time for timelines and analytics
archive old cold partitions to BigQuery/GCS according to retention policy
```

Partition lifecycle:

```text
backend/platform/scheduler or pg_partman pre-creates partitions at least 3 months ahead
DEFAULT partition exists as a backstop so month rollover never blocks identity writes
ops alert fires if future partitions are missing
timeline reads use goat_identity_events(goat_id, occurred_at desc); recorded_at partition pruning is not expected for per-goat timeline reads
```

Do not rely on a unique constraint on `idempotency_key` inside this partitioned
event table. In Postgres, unique constraints on partitioned tables must include
the partition key, which would weaken cross-partition idempotency. Use the
non-partitioned `idempotency_keys` table below for dedupe.

### `idempotency_keys`

Small non-partitioned dedupe table for write requests and import-derived
canonical mutations. This table protects business idempotency while high-volume
event/audit tables remain partitioned.

```text
idempotency_key text primary key
scope text not null
request_hash text not null
status text not null
result_type text null
result_id uuid null
first_seen_at timestamptz not null
completed_at timestamptz null
expires_at timestamptz null
```

Rules:

```text
same idempotency_key + same request_hash returns the first result
same idempotency_key + different request_hash is rejected
canonical mutation, typed event, audit row, outbox row, and idempotency record are committed together
import source_row_key/source_row_version_hash maps to a stable idempotency_key for canonical writes
expired keys are swept by backend/platform/scheduler
interactive write keys expire after the configured replay window
import keys expire only after migration replay/audit window is closed
```

### `outbox_messages`

Hot queue for publishing identity events to Pub/Sub and downstream analytics,
notifications, and projection workers. The outbox row is written in the same
Postgres transaction as the canonical mutation, identity event, audit row, and
idempotency record.

```text
outbox_id uuid primary key
event_id uuid not null
event_type text not null
schema_version text not null
aggregate_type text not null
aggregate_id uuid not null
topic text not null
payload jsonb not null
headers jsonb not null
idempotency_key text not null
trace_id text null
status text not null
attempt_count int not null default 0
next_attempt_at timestamptz null
last_error text null
published_at timestamptz null
created_at timestamptz not null
updated_at timestamptz not null
```

Constraints and indexes:

```text
unique(event_id)
index(status, next_attempt_at, created_at)
index(aggregate_type, aggregate_id, created_at)
index(created_at)
```

Statuses:

```text
pending
publishing
published
failed
dead_letter
```

Relay rules:

```text
API/import transactions never publish directly to Pub/Sub
local/dev relay foundation lives in backend/cmd/outbox-relay and
backend/internal/outbox
relay claims pending rows where next_attempt_at is null or due, ordered by
created_at/outbox_id, in bounded chunks with FOR UPDATE SKIP LOCKED
claim happens in a short transaction; publish happens outside the claim
transaction
before incrementing attempts, pending rows already at max attempts move to
dead_letter and are not published
stale publishing rows are reclaimed by lease timeout without resetting
attempt_count; fresh publishing leases are not stolen
publish success marks published, sets published_at, clears last_error, and
clears next_attempt_at
retryable publish failure resets to pending, schedules future next_attempt_at,
and stores sanitized last_error
invalid domain event envelope payloads fail terminally with status=failed and
are not retried
exhausted attempts move to dead_letter
relay update SQL only touches status, attempt_count, next_attempt_at,
last_error, published_at, and updated_at; it does not update tenant_id or
event_id
the local logging/no-op publisher logs safe metadata only and never logs raw
payload, RFID/tag values, headers, or media/source data
published rows are kept only for hot replay/debug window, then archived/exported or deleted by scheduler
relay uses event_id/idempotency_key so retry cannot create a second downstream business event
real Google Pub/Sub adapter, production worker deployment, event consumers,
frontend event UI, and richer DLQ UI are deferred
```

### `goat_identity_counters`

Small operational count projection for admin/dashboard identity counts. This
prevents Phase 1 dashboards from doing full-herd `count(*)` scans over 50k now
or 1M later.

```text
counter_id uuid primary key
counter_grain text not null
tenant_id uuid not null
custodian_party_id uuid null
farm_id uuid null
park_id uuid null
shed_id uuid null
cohort_id uuid null
lifecycle_status text null
reproductive_status text null
growth_cohort_tag text null
management_stage text null
health_status text null
identity_state text null
breed_id uuid null
sex text null
count_value bigint not null
as_of_recorded_at timestamptz null
source_import_run_id uuid null
is_rebuilding boolean not null default false
updated_at timestamptz not null
```

Rules:

```text
dashboard count APIs read this projection, not the full goats table
GET /analytics/identity/counts requires a bounded limit and uses keyset
pagination so counter buckets are never silently truncated
count reads order by count_value desc, counter_id asc and use an opaque
base64url cursor carrying cursor version, count_value, and counter_id
structurally valid cursors are treated as keyset positions and are not checked
against existing rows; structurally invalid cursors return 400
count cursors are scoped to the same query shape/projection version and are not
durable pagination guarantees across rebuilds
freshness fields report projection freshness/as_of state, not raw table count
projection is rebuildable from goats + identity/location events
backend/internal/reporting owns goat_identity_counters reads, rebuilds, and
local incremental counter updates
backend/internal/identity must not query goat_identity_counters
GET /analytics/identity/counts stays as the public route and is wired to
reporting-owned service/repository code
local rebuild command recomputes all 10 Phase 1 grains by default and supports
optional source_import_run_id stamping
large import runs rebuild counters at import completion in bounded grouped queries
counter rebuild uses one repeatable-read transaction per committed tenant
rebuild attempt and takes a per-tenant advisory lock before validation/rebuild
work; serialization/deadlock failures retry the whole transaction in a bounded
loop
counter rebuild deletes old tenant+grain buckets and inserts the freshly grouped
complete result set inside the same transaction, so readers see all-old or all-new
counter rebuild writes `as_of_recorded_at` as the projection watermark; the
watermark is max(goat_identity_events.recorded_at) from the rebuild snapshot,
not `now()` and not goats.updated_at
if the tenant has no goat_identity_events, as_of_recorded_at is null
successful rebuild writes goat_identity_counter_projection_state with the
checkpoint (recorded_at + event_id) under the same advisory lock
is_rebuilding remains false on final Phase 1 rebuild rows; externally visible
rebuild status metadata beyond rebuild_required is deferred to a future metadata
table
local steady-state incremental updates are event-driven, not a large-import
row-by-row path
local incremental command backend/cmd/update-identity-counters scans
goat_identity_events ordered by recorded_at asc, identity_event_id asc after the
projection checkpoint
incremental processed-event dedupe uses tenant_id + event_id + event_recorded_at
so partition-aware event identity is preserved
goat.created increments all matching Phase 1 grain buckets from the shared
goat_identity_counter_memberships view
goat.identifier.added, goat.identifier.retired, and goat.identifier.disputed are
noops for counters and only advance the checkpoint
goat.identity.merge_approved and unknown event types mark
goat_identity_counter_projection_state.rebuild_required=true and stop before
later events are processed
missing projection state with counters or events marks rebuild_required; tenants
with no counters and no events get an empty projection state
analytics freshness.warning surfaces rebuild_required and dashboards still must
not fall back to raw goats scans
processed-event retention pruning is bounded by tenant and checkpoint
projection failures must not roll back canonical goat writes; the
`count_value >= 0` check means projection underflow/drift is isolated in the
projection worker, not allowed to abort a valid goat mutation
future richer incremental deltas require before/after dimension snapshots because
a goat change can affect multiple counter grains
production incremental workers must include a drift-detection trigger, such as
scheduled rebuild plus bounded reconciliation checks, because high-side drift is
otherwise silent
parallel import workers must not contend on hot counter rows
dashboard/API responses include as_of_recorded_at and is_rebuilding
if counters are stale/rebuilding, dashboards show freshness state and still must not fall back to raw table scans
the rebuild implementation must explicitly pin grain membership semantics before
code, including how merged/inactive goats are counted or excluded
```

Materialized grains:

```text
tenant_lifecycle: tenant_id + lifecycle_status
custodian_lifecycle: tenant_id + custodian_party_id + lifecycle_status
custodian_identity: tenant_id + custodian_party_id + identity_state
park_lifecycle: tenant_id + park_id + lifecycle_status
shed_lifecycle: tenant_id + park_id + shed_id + lifecycle_status
breed_sex_lifecycle: tenant_id + breed_id + sex + lifecycle_status
health_status: tenant_id + health_status
growth_cohort: tenant_id + growth_cohort_tag
management_stage: tenant_id + management_stage
reproductive_status: tenant_id + reproductive_status
```

Phase 1 grain membership:

```text
all counter rebuild queries exclude goats with identity_state='merged' because
merged goats are tombstones/redirects and would double-count the survivor
all counter rebuild queries exclude goats with identity_state='inactive' because
inactive is not a current canonical herd member for Phase 1 operational counts
lifecycle-bearing grains (tenant_lifecycle, custodian_lifecycle,
park_lifecycle, shed_lifecycle, breed_sex_lifecycle) count all non-merged,
non-inactive goats by lifecycle_status, including dead and sold buckets
non-lifecycle operational grains (health_status, growth_cohort,
management_stage, reproductive_status) count only alive, non-merged,
non-inactive goats; dead/sold goats do not appear in these current-state
distributions
custodian_identity counts only alive, non-merged, non-inactive goats by
identity_state and therefore includes clean, needs_review, and disputed for the
currently alive herd in Phase 1
location grains use only current goats.park_id for park_lifecycle and
goats.park_id plus goats.shed_id for shed_lifecycle in Phase 1; farm_id and
cohort_id exist as cache columns but no farm_lifecycle or cohort_lifecycle grain
exists
location grains do not read historical location ledgers
custodian grains use goats.custodian_party_id as the current custodian cache
NULL dimensions are valid unknown buckets under the NULLS NOT DISTINCT unique
index
```

Do not materialize arbitrary combinations of all nullable dimensions. New grains
must be explicitly added with a named `counter_grain`, rebuild query, read API,
and load test.

Breed counter grain requires normalized breed data. Do not use raw free-text
breed strings as counter dimensions; map legacy breed text to controlled
`breed_id` before rebuilding `breed_sex_lifecycle`.

Seed breed/species reference data from the canonical glossary/source findings.
Known source labels include Malai, Beetal, Sojat, Osmanabadi, Boer, Anantapur
Sheep, Anantapur, and Kenguri. Treat these as reviewable reference rows and
aliases, not hardcoded enums. Unknown or dirty breed strings stay as raw evidence
and route to review instead of fragmenting counters.

### `audit_log`

Platform audit record written in the same transaction as canonical identity
mutations. Identity decisions explain why a domain decision was made; audit
records prove who/what changed or attempted to access the system.

```text
audit_id uuid primary key
actor_id uuid null
actor_type text not null
action text not null
resource_type text not null
resource_id uuid null
scope_type text null
scope_id uuid null
decision_id uuid null references identity_decisions(decision_id)
before_state jsonb null
after_state jsonb null
metadata jsonb not null
trace_id text null
created_at timestamptz not null
```

Partitioning:

```text
partition by range(created_at), monthly partitions
retain hot audit partitions in Postgres
archive/export cold partitions to BigQuery/GCS according to compliance policy
```

Partition lifecycle:

```text
backend/platform/scheduler or pg_partman pre-creates partitions at least 3 months ahead
DEFAULT partition exists as a backstop so audit writes do not fail at month rollover
ops alert fires if future partitions are missing
```

### `user_scope_grants`

Minimal scoped access table for Phase 1 RBAC.

```text
grant_id uuid primary key
user_id uuid not null
role text not null
scope_type text not null
scope_id uuid not null
status text not null
valid_from timestamptz not null
valid_to timestamptz null
created_by uuid null
created_at timestamptz not null
```

Phase 1 internal goat-ops roles:

```text
admin
verifier
park_head
operator
ceo_internal
```

Phase 1 permissions:

```text
goat.read
correction.create
goat.view_dirty_data
goat.review_identity
goat.write_identity
analytics.identity.read
import.run.manage
import.run.view
```

Role permissions:

```text
admin
  all Phase 1 permissions

verifier
  goat.read
  correction.create
  goat.view_dirty_data
  goat.review_identity
  goat.write_identity
  analytics.identity.read
  import.run.view

park_head
  goat.read
  correction.create
  analytics.identity.read

operator
  goat.read
  correction.create

ceo_internal
  goat.read
  correction.create
  goat.view_dirty_data
  goat.review_identity
  goat.write_identity
  analytics.identity.read
  import.run.manage
  import.run.view
```

`ceo_internal` is the Goat OS product-admin role for Phase 1 internal product
actions. It intentionally has the same Phase 1 product permissions as `admin`
inside Goat OS. This is app authorization only and does not grant Google Cloud,
IAM, billing, GitHub, or repository administration.

Multi-grant semantics:

```text
users can hold multiple active tenant-scope grants
permissions are the union of active matching tenant grant roles
authorize when any active matching tenant grant role confers the required permission
operator+verifier can access verifier-only review routes
product-admin-only routes require an active `admin` or `ceo_internal` grant
revoked, inactive, expired, future-valid, and missing grants contribute no permissions
```

`import.run.view` on `verifier` is intentional: identity reviewers need import
provenance and dirty-row context when triaging conflicts, candidates, and
corrections. It does not grant import run creation or canonical import apply.

Endpoint permission mapping:

```text
searchGoats                         -> goat.read
getGoatPassport                     -> goat.read
getGoatTimeline                     -> goat.read
resolveIdentifier                   -> goat.read
createCorrectionRequest             -> correction.create
listCorrectionRequests              -> correction.create for the app own/visible correction list; Phase 1B scoped list semantics are implemented

createImportRun                     -> import.run.manage, product-admin only
getImportRun                        -> import.run.view
listImportRunRows                   -> import.run.view

listIdentityConflicts               -> goat.view_dirty_data
getIdentityConflict                 -> goat.view_dirty_data
listIdentityCandidates              -> goat.view_dirty_data
adminListCorrectionRequests         -> goat.review_identity

resolveCorrectionRequest            -> goat.review_identity
resolveIdentityConflict             -> goat.review_identity
approveIdentityCandidate            -> goat.review_identity
rejectIdentityCandidate             -> goat.review_identity

createAdminGoat                     -> goat.write_identity
updateAdminGoat                     -> goat.write_identity
addGoatIdentifier                   -> goat.write_identity
retireGoatIdentifier                -> goat.write_identity

getIdentityCounts                   -> analytics.identity.read
```

Contract correction implemented before route permissions were enforced:

```text
app-api owns GET /identity/correction-requests as listCorrectionRequests
admin-api owns adminListCorrectionRequests at GET /admin/identity/correction-requests
one method+path cannot have two different permissions in one ServeMux
```

The API router must use an explicit route-to-permission registry. A protected
route missing from that registry must fail closed, not pass through. The only
unauthenticated API routes are:

```text
GET /healthz
GET /readyz
```

Identity/admin/app handlers and reporting/analytics handlers must consume the
same permission registry package. Do not maintain a separate analytics-only
permission table for `analytics.identity.read`.

Phase 1 auth/RBAC scope:

```text
API bootstrap defaults to bearer auth when GOATOS_AUTH_MODE is empty
signed bearer token proves user identity
Goat OS DB grant for token sub proves tenant membership and role authority
role and permissions come from user_scope_grants, never from token role claims
tenant_id from the token is the request tenant
X-GoatOS-Tenant-ID and X-GoatOS-Actor-ID are local/dev scaffolds only and are
ignored/overwritten in bearer mode
```

Analytics tenant handling:

```text
getIdentityCounts derives CountParams.TenantID from the token context
tenant_id query param is only an optional assertion and must equal token tenant
if tenant_id is omitted, use the token tenant
q.Get("tenant_id") must not reach the reporting repository as authority
```

HS256 bearer verification is only a bootstrap verifier for local/shared-dev
Phase 1 API hardening. Production authentication requires a real IdP and
asymmetric verification such as RS256/ES256/JWKS with issuer and audience
validation.

The bootstrap HS256 verifier uses Go standard-library primitives
(`crypto/hmac`, `crypto/sha256`, JSON, and base64url parsing). Do not add a JWT
dependency unless `go.mod` and `go.sum` are updated and the alg-confusion tests
remain green.

Bearer-mode startup must fail if required auth config is missing or weak:

```text
GOATOS_AUTH_HS256_SECRET must be at least 32 bytes
GOATOS_AUTH_ISSUER must be set
GOATOS_AUTH_AUDIENCE must be set
GOATOS_AUTH_MAX_TOKEN_TTL defaults to 24h and must parse as a positive Go duration when set
```

Bearer tokens whose `exp` is farther in the future than
`GOATOS_AUTH_MAX_TOKEN_TTL` are rejected. This is a bootstrap HS256 blast-radius
control so accidentally long-lived tokens do not linger indefinitely. It is not
final production revocation; production auth still requires issuer-managed token
lifetimes, key rotation, refresh-token policy, and revocation/session handling.

The dev-header escape hatch is unsafe and must be hard to enable accidentally.
It requires `GOATOS_AUTH_MODE=dev_headers` plus `GOATOS_DEV_HEADERS_ALLOW=true`,
emits a loud startup warning, and is allowed only when `GOATOS_ENV` is exactly
`local`, `dev`, or `test`. Normal bearer mode ignores/overwrites
`X-GoatOS-Tenant-ID` and `X-GoatOS-Actor-ID`.

Local bootstrap helpers:

```text
backend/cmd/mint-dev-token
  local/dev HS256 bearer token minting only
  reads GOATOS_AUTH_HS256_SECRET, GOATOS_AUTH_ISSUER,
  GOATOS_AUTH_AUDIENCE, and GOATOS_AUTH_MAX_TOKEN_TTL
  reuses backend/internal/platform/auth signing/verifier validation rules
  prints only the token by default and never prints secrets

backend/cmd/seed-dev-grant
  local/dev tenant-scope grant helper only
  inserts user_scope_grants with scope_type=tenant and scope_id=tenant_id
  requires explicit tenant-id, user-id, and role
  requires GOATOS_ENV to be exactly local, dev, or test
  refuses production/staging-looking targets and non-local DB hosts, including Cloud SQL-style Unix socket paths
```

`user_scope_grants` must not be seeded by migrations. Local grant bootstrap is
an operator/dev action so production grant state is explicit and auditable.

The shared local auth smoke must use one `GOATOS_AUTH_*` config for the backend,
`mint-dev-token`, local grant seeding, and admin-web client smoke. It must run
only against a local Docker/dev database, call `GET /goats/search?limit=10`, and
prove the granted user receives 200 while a valid token without a matching grant
receives 403.

HTTP auth/RBAC covers network API requests. Local/system CLIs such as
`rfid-import`, `rfid-apply`, `outbox-relay`, `rebuild-identity-counters`, and
`update-identity-counters` remain operator-trusted entrypoints outside HTTP
auth, but they must still require explicit tenant input and keep SQL
tenant-scoped.

Middleware construction:

```text
bootstrap.NewAPI validates required bearer auth configuration
middleware constructors take validated verifier/authorizer dependencies
middleware constructors do not read env or terminate tests
handler tests build a test verifier with an in-test secret or explicitly opt into dev_headers
```

Bearer mode actor rule:

```text
actor_id for writes is token sub from context
X-GoatOS-Actor-ID must not be read by production handlers in bearer mode
audit, decision, and correction actor fields use the token sub
```

Phase 1 RBAC is an internal goat-ops realm. Do not map investor, buyer, donor,
partner, franchise, lending, or external customer users into
`user_scope_grants`. Investor/reduced dashboards belong to a later sanitized
analytics or commerce realm. Legacy procurement roles such as procurement head,
procurement manager, and procurement assistant manager belong to procurement
and workforce phases, not to this Goat Passport identity role set.

Tenant-scope RBAC is broader than the final intended scope model. A tenant-wide
`verifier` or `admin` can act across all goats in the tenant until the
custodian/farm/park/shed/cohort filtering slice lands. This is acceptable only
as a Phase 1 deploy-gate foundation and must be tightened before broader
multi-scope rollout.

Rules:

```text
tenant-scoped reads filter by tenant_id
custody-scoped reads filter by custodian_party_id
park/shed/cohort reads filter by current placement cache
cross-custody or cross-location placement, if allowed, must grant visibility by both custody and placement policy
search and identifier resolve apply scope filters before response shaping
out-of-scope matches are not returned as goat summaries, source records, conflict details, or proof refs
exact goat_id reads outside scope return a generic not_found_or_not_allowed error envelope
admin conflict queues require both goat.view_dirty_data and scope over at least one affected goat
merge/identifier mutation commands require scope over every affected goat and identifier
all permission denials write audit_log with action, requested scope, and trace_id
```

## Matching And Reconciliation Algorithm

### Normalization

Normalize every identifier before matching:

```text
trim whitespace
uppercase where appropriate
apply type-specific normalizer
preserve leading zeros and vendor encodings unless that identifier type allows cleanup
preserve original value for display
store normalized_value for matching
store normalizer_version for future replay/debug
```

### Match Strength

Signals:

```text
RFID exact active match       strongest
old tag exact active match    strong
sheet/source row match        strong within same source
purchase/load/source          supporting
farm/park/shed/cohort         supporting
breed/sex/status axes         supporting
age/DOB range                 supporting
mother/father references      supporting
photo/FaceID                  proposal only
```

### Source Precedence

When sources disagree, Goat OS treats them by authority level:

```text
RFID scan from trusted device
  strong evidence if device and scan context are trusted

current Sheet/XLSX source
  migration evidence, not permanent truth by itself

dashboard counts
  derived/reporting evidence only, never identity truth

Slack/App Script form mention
  operational clue only; must pass through Goat OS validation

operator memory/manual note
  correction request or review evidence, never auto-link by itself
```

### Auto-Link Rules

Auto-link only when all are true:

```text
one unique active match
no active conflict for that identifier
no material breed/sex/status-axis mismatch
location mismatch is allowed only if movement history supports it
import policy allows this identifier type to auto-link
identifier uniqueness scope is known
scope_key is not unknown
```

### Conflict Rules

Create conflict when:

```text
same active RFID appears on more than one goat
same active old tag appears on more than one goat inside the same confirmed uniqueness scope
row has no usable identifier
candidate score is below auto-link threshold
breed/sex/status-axis conflict is material
source row appears already linked to different goat
identifier scope is unknown and match would otherwise mutate canonical identity
```

Uniqueness constraints are backstops, not the user experience. Import and API
code must pre-check likely duplicates and create conflicts. If the database
constraint still fires because of a race, the handler catches it, rolls back the
canonical mutation, and creates/returns the conflict path instead of crashing
the whole import chunk.

### Merge Rules

Merging is a human-approved decision, not an import side effect.

```text
one survivor_goat_id is selected
duplicate goat gets identity_state = merged
goat_merge_links stores survivor/merged mapping
identifiers are transferred, retired, or disputed according to decision and collision policy
all affected timelines show the merge decision
no events, proof, or source rows are deleted
```

If downstream events already exist on both goats, Phase 1 must not silently
rewrite them. It should either:

```text
show both timelines through survivor view
or require a later controlled migration step
```

Merge transaction minimum:

```text
lock survivor and merged goat rows
write identity_decision
write goat_merge_links
set merged_goat.merged_into_goat_id
apply identifier collision policy
write identity events for both goats
write audit row and outbox rows
```

Identifier collision policy:

```text
survivor active identifiers win by default
loser identifiers with colliding unique keys are retired, not transferred
non-colliding loser identifiers may transfer to survivor only when decision explicitly says so
loser identifiers not explicitly transferred are retired; merged goats do not retain active identifiers
transferred identifiers are demoted with is_primary_for_goat = false by default
survivor keeps its existing primary identifier for each identifier_type unless reviewer explicitly changes it
RFID collision always creates/keeps conflict unless reviewer resolves device/tag evidence
all retired/transferred/disputed actions are written to identity_decision_identifiers
```

### Reconciliation State Flow

```text
legacy_import_row
  -> clean deterministic match
       -> canonical mutation + identity_decision + event
  -> possible duplicate
       -> identity_match_candidate
       -> approve/reject
       -> identity_decision
       -> canonical mutation only if approved
  -> hard conflict
       -> identity_conflict
       -> resolve conflict
       -> identity_decision
       -> canonical mutation only if approved
  -> field uncertainty
       -> identity_correction_request
       -> field check / admin review
       -> identity_decision
```

### No AI Truth Rule

AI/image/fuzzy matching can only insert into:

```text
identity_match_candidates
```

It cannot write:

```text
goats
goat_identifiers active links
merge decisions
```

AI proposal guardrails:

```text
AI-authored identity suggestions must use created_by/decided_by_type =
ai_proposal.
AI proposals may only be proposed or needs_review. They must not be approved,
rejected, auto-applied, or terminal by themselves.
AI workers must never write as system_rule or import_policy. Those actor types
are reserved for deterministic, governed policy automation.
AI suggestions must include explainable match_reasons, confidence/model
metadata where applicable, and evidence/source links sufficient for a human or
policy worker to verify the suggestion.
Future AI-worker migrations should add a DB check mirroring
identity_decisions_ai_not_approved_check for candidates:
created_by <> 'ai_proposal' OR state IN ('proposed','needs_review').
```

## API Contracts

Common API rules:

```text
All list endpoints require limit and cursor.
All write endpoints require Idempotency-Key.
PATCH endpoints require row_version or ETag.
Admin correction resolve is a POST command but still requires row_version
because it changes a reviewed correction request state.
All endpoints enforce RBAC scopes server-side.
All read endpoints apply visibility scope before building response DTOs.
Errors use a shared envelope:
  code
  message
  field_errors
  trace_id
  retryable
```

Scope response rule:

```text
out-of-scope resources return code = not_found_or_not_allowed
identifier search filters hidden matches before returning goat summaries
operator/mobile responses never include hidden_match_count or out-of-scope goat metadata
admin responses may include hidden/affected counts only with goat.view_dirty_data and matching scope grants
```

### Admin APIs

```text
POST /admin/import-runs
GET  /admin/import-runs/{import_run_id}
GET  /admin/import-runs/{import_run_id}/rows

GET  /admin/identity/conflicts
GET  /admin/identity/conflicts/{conflict_id}
POST /admin/identity/conflicts/{conflict_id}/resolve

GET  /admin/identity/candidates
POST /admin/identity/candidates/{candidate_id}/approve
POST /admin/identity/candidates/{candidate_id}/reject

POST /admin/goats
PATCH /admin/goats/{goat_id}
POST /admin/goats/{goat_id}/identifiers
POST /admin/goats/{goat_id}/identifiers/{identifier_id}/retire

POST /identity/correction-requests
GET  /identity/correction-requests
GET  /admin/identity/correction-requests
POST /admin/identity/correction-requests/{correction_request_id}/resolve
```

Import Review read semantics:

```text
GET /admin/import-runs/{import_run_id} returns the tenant-scoped run summary.
rows_processed maps to legacy_import_runs.row_count.
goats_created maps to legacy_import_runs.created_goat_count.
error_count maps to legacy_import_runs.error_count.
conflicts_opened maps to legacy_import_runs.conflict_count.
rows_needing_review is derived from legacy_import_rows where processing_state=needs_review.
metrics not tracked by Phase 1 import/apply return null, not fake zeroes.

GET /admin/import-runs/{import_run_id}/rows is read-only, tenant/import-run
scoped, keyset-paginated by row_number and legacy_row_id, and bounded to limit
<=500. It supports processing_state and reason_code filters. reason_code
matches error_reason or normalized_payload.processing_reasons. Reason buckets
can overlap, so reason counts must never be presented as summing to total rows.
Rows return whitelisted review fields only and never return raw_payload.
```

Phase 1 local closeout status:

```text
Fresh local Docker Postgres proof with all migrations through 000014:
  normal RFID apply -> created_goat 436, needs_review 787, error 0
  guarded --allow-rfid-only-blank-suffix apply -> created_goat 711, needs_review 512, error 0
  confirmed non-goat disposition -> created_goat 711, needs_review 8, rejected 504, error 0
  remaining review reason occurrences:
    blank_gender 4
    duplicate_old_tag_same_scope 4
  rejected reason:
    confirmed_non_goat_species 504
  tenant_lifecycle alive counter = 711
  Mesha admin-web SSR routes verified against the final run:
    /
    /counts
    /herd
    /goats/{goat_id} with live timeline
    /import-review?import_run_id=<final_run_id>
    /data-quality with honest empty states when queues are empty
```

The real local proof DB had no conflicts, candidates, or correction requests;
backend repository/handler coverage proves populated list paths for those
queues separately.

This proves the local identity/import/read/admin-demo spine only. Production
auth/IdP, cloud deployment, Pub/Sub/event egress, richer messy-data search,
messy-row fix/approve workflows, correction workflows beyond the defined safe
actions, and non-Phase-1 legacy modules remain deferred.

Phase 1B-0 implements the read-only goat timeline and app/admin correction
request list APIs. Remaining typed not_implemented endpoints:

```text
POST /admin/import-runs
POST /admin/goats
PATCH /admin/goats/{goat_id}
```

Phase 1B admin-web action wiring is intentionally limited to services whose
write semantics already exist in this TRD/backend: create correction request,
reject candidate, resolve correction request, resolve conflict as reject_match
or merge_goats, add goat identifier, and retire goat identifier. The UI sends
idempotency keys, evidence refs, and row_version values to the backend and does
not invent client-side mutation authority. Confirmed non-goat import-row
terminal disposition is implemented through the local import tool, not a browser
write action. Candidate approve, conflict create_goat, and Import Review row
fix/approve actions remain separate contract/design slices.

### App APIs

```text
GET /goats/search?q=&identifier_type=&scope_key=&farm_id=&park_id=&location_id=&status=
GET /goats/{goat_id}
GET /goats/{goat_id}/timeline
GET /identifiers/{type}/{value}/resolve?scope_key=&farm_id=&park_id=&location_id=
```

Identifier resolve semantics:

```text
default resolve is active identifiers only
scope context is required for scoped identifier types such as old_tag
if active match exists, return single_match plus retired/disputed history warnings
if only retired/disputed matches exist, return needs_review with history, not single_match
if the matched goat is merged, return merged_redirect with survivor goat
if the same visible value is active across multiple scopes and request gives no scope context, return multiple_matches
if multiple active matches exist in the same scope, create/return conflict_id because it violates constraints
out-of-scope matches are treated as not visible for operator/mobile response shaping
```

### Dashboard/Analytics APIs

```text
GET /analytics/identity/counts?grain=&tenant_id=&limit=&cursor=&custodian_party_id=&farm_id=&park_id=&shed_id=&cohort_id=&lifecycle_status=&reproductive_status=&growth_cohort_tag=&management_stage=&health_status=&identity_state=&breed_id=&sex=
```

Count endpoint rules:

```text
reads goat_identity_counters or governed analytics facade, never raw goats count(*)
requires explicit grain and scope filters
requires explicit limit 1..500; missing, malformed, or above-max limits return 400
uses deterministic keyset pagination ordered count_value desc, counter_id asc
returns has_more and next_cursor; next_cursor is null when no more rows exist
valid cursors are query-shape/projection-version scoped and may return
empty/partial pages after a rebuild; they are not durable cross-rebuild cursors
structurally valid arbitrary cursor values are allowed as keyset positions
applies RBAC before reading counter rows
returns count_value plus as_of_recorded_at, source_import_run_id, and is_rebuilding
does not support arbitrary dimension combinations outside named materialized grains
goat_identity_counters_lookup_idx remains for now; review old updated_at-order
lookup index cleanup only after no read path uses it
```

### Response DTOs

Goat summary:

```text
goat_id
display_id
primary_old_tag
rfid
breed
sex
age_band
lifecycle_status
reproductive_status
growth_cohort_tag
health_status
identity_state
location_path
warnings[]
```

Conflict summary:

```text
conflict_id
conflict_type
severity
identifier
goat_count
source_record_count
state
created_at
```

Generated-client readiness:

```text
OpenAPI must include schemas for every request/response.
TypeScript clients are generated in packages/api-client before frontend/mobile
screen work starts.
apps/admin-web and future mobile code import Goat OS APIs from generated
clients/adapters, not hand-copied DTOs.
Contract fixtures cover success, validation error, permission error, conflict,
and idempotent replay.
```

### Minimum Payload Contracts To Draft Before Code

These are not optional implementation details. They must be captured in OpenAPI
before backend, admin web, or mobile code starts.

Create import run request:

```text
source_system
source_dataset
source_file_ref
policy_version
dry_run
```

Resolve identifier response:

```text
resolution_state: single_match | multiple_matches | no_match | needs_review | merged_redirect
goat_summary
candidate_goats[]
warnings[]
conflict_id
redirect_goat_id
trace_id
```

Resolve conflict request:

```text
decision_type
decision_result
survivor_goat_id
affected_goat_ids[]
identifier_actions[]
evidence_refs[]          -- typed EvidenceRef objects, not evidence_ids
reason
row_version
```

`merge_goats` is valid only for duplicate-identity conflict types:
`possible_duplicate_goat`, `duplicate_active_identifier`,
`rfid_already_linked`, and `old_tag_reused`. It must reject
`location_mismatch`, `status_mismatch`, `missing_required_identifier`, and
`tagless_goat_review`.

For `merge_goats`, the persisted decision record must use the resolved live
`survivor_goat_id` and resolved live `merged_goat_ids` as the outcome truth. If
the reviewer submitted stale ids that redirected during merge resolution, the
record may also include `requested_survivor_goat_id` and
`requested_affected_goat_ids` as audit trail fields.

For `mark_identifier_disputed`, the reviewer must select identifiers
explicitly through `identifier_actions[]` with `action=dispute` and non-null
`identifier_id`. The backend must not infer disputed identifiers loosely from
`identifier_type` and `identifier_value` alone. Each selected identifier must
belong to the same tenant, be active, belong to a goat in the conflict, and,
when the conflict stores `identifier_type` or `identifier_value`, match those
fields. The mutation sets `goat_identifiers.status=disputed`,
`is_primary_for_goat=false`, `approved_by=<actor>`, and `updated_at=<decision
time>`, bumps each affected goat `row_version` once, then writes
`identity_decision_identifiers(action=dispute)`, `goat.identifier.disputed`,
`identity_decision_events`, audit, outbox, and idempotency completion in the
same transaction. When `merge_goats` transfers a loser identifier to the
survivor, the survivor goat `row_version` is also bumped because the survivor's
identifier surface changed.

Row-version invariant: every implemented write that changes a goat identity
surface must bump that goat's `row_version` exactly once per transaction. Future
Phase 1 mutation slices must preserve this before projection/cache invalidation
depends on `row_version`: candidate approve/reject only when it mutates goat
identity, correction auto-apply when it applies attach/retire/dispute/status
changes, unmerge when redirect or identifier state changes, and create_goat
starts with its initial `row_version` unless follow-up mutations happen.

Candidate review API behavior:

`GET /admin/identity/candidates` returns the actionable queue only:
`identity_match_candidates.state in ('proposed', 'needs_review')`. It excludes
approved, rejected, and expired candidates by default, is tenant-scoped, requires
a bounded `limit`, and uses deterministic keyset pagination ordered by
`created_at DESC, candidate_id DESC`. The cursor must carry both values needed
to continue that order. `CandidateSummary` includes `state` and `row_version`.

`ReviewCandidateRequest` uses typed `evidence_refs`; old `evidence_ids` payloads
are rejected by `additionalProperties=false`. Required fields are `reason`,
`evidence_refs`, and `row_version`.

`POST /admin/identity/candidates/{candidate_id}/reject` is a decision-only
candidate state transition. It requires `Idempotency-Key`, temporary
`X-GoatOS-Tenant-ID`, authenticated actor, `reason`, typed
`evidence_refs`, and current candidate `row_version`. The guarded update must
match tenant, candidate, `state in ('proposed','needs_review')`, and
`row_version`, then set `state='rejected'`, `reviewed_by=<actor>`,
`reviewed_at=<decision time>`, `decision_id=<decision>`, and increment
`identity_match_candidates.row_version` by one.

Candidate reject writes `identity_decisions` before the guarded candidate update
so `identity_match_candidates.decision_id` can satisfy its FK. The decision maps
to `decision_type=reject_match`, `decision_result=candidate_rejected`,
`decision_state=rejected`, `decided_by_type=human`,
`policy_version=phase1-manual-correction-review-v1`, and preserves typed
`evidence_refs` in `decision.evidence.evidence_refs`. It writes audit and
idempotency completion in the same transaction, does not mutate goat identity,
does not bump goat `row_version`, and does not write `goat_identity_events` or
`outbox_messages` unless a candidate/conflict aggregate event contract is later
defined.

`POST /admin/identity/candidates/{candidate_id}/approve` remains typed
`not_implemented` until canonical mutation semantics are contract-defined. Do
not mark a candidate approved without applying a defined merge/create/identifier
mutation.

For `reject_match`, the resolver records a rejected decision, marks the conflict
`rejected`, and does not mutate goat identity. For
`request_field_verification`, the resolver records a `needs_review` decision,
marks the conflict `needs_field_check`, and leaves `resolved_at` and
`resolved_by` null.

Create correction request:

```text
request_type
goat_id
identifier_type
identifier_value
location_scope
description
evidence_refs[]
```

Resolve correction request:

```text
state: approved | rejected | needs_field_check | closed
reason
evidence_refs[]          -- typed EvidenceRef objects, not evidence_ids
row_version
```

Resolve correction request decision record:

```text
decision_type = resolve_correction_request
policy_version = phase1-manual-correction-review-v1
decided_by_type = human
decided_by = reviewer actor id
reviewer_id = reviewer actor id
evidence.evidence_refs[] preserves typed EvidenceRef objects
reason is preserved in the decision record and audit metadata
```

Decision-state mapping for correction resolve:

```text
target approved          -> decision_state approved     + decision_result approved
target rejected          -> decision_state rejected     + decision_result rejected
target needs_field_check -> decision_state needs_review + decision_result needs_field_check
target closed            -> decision_state approved     + decision_result closed
```

`decision_state` is the lifecycle of the review decision record
(proposed/approved/rejected/needs_review). It is not external review wording or
operator work status. The business outcome belongs in `decision_result`.
Resolving a correction request does not directly mutate goat identity and does
not create `goat_identity_events`; identity mutation, if approved later, must be
its own explicit write with evidence and audit.

Shared error envelope:

```text
code
message
field_errors[]
trace_id
retryable
```

## Events

Identity domain events:

```text
goat.created
goat.identity.updated
goat.identifier.added
goat.identifier.retired
goat.identifier.disputed
goat.identity.conflict_opened
goat.identity.conflict_resolved
goat.identity.merge_proposed
goat.identity.merge_approved
goat.identity.merge_rejected
goat.location.changed
legacy_import.row_processed
legacy_import.completed
```

Every event uses the shared event envelope:

```text
event_id
event_type
schema_version
aggregate_type
aggregate_id
occurred_at
recorded_at
producer
idempotency_key
actor
subject_type
subject_id
visibility_scope:
  tenant_id
  custodian_party_id null
  farm_id null
  park_id null
  shed_id null
  cohort_id null
evidence_refs[]
payload
trace_id
```

These names intentionally match the platform outbox relay contract in
`context/analytics/final-analytics-infra.md`. `aggregate_type` and
`aggregate_id` identify the stream owner used for ordering, replay, and outbox
indexing. `subject_type` and `subject_id` identify the specific thing the event
is about when it differs from the stream owner. Example:

```text
goat.identifier.added
  aggregate = goat
  subject = identifier

goat.identity.merge_approved
  aggregate = survivor goat
  subject = merged goat
```

If subject is the same thing as aggregate, subject fields may repeat the
aggregate values. They are not ad-hoc API aliases; they are canonical envelope
fields and must be generated from the same JSON Schema.

Outbox payload rule:

```text
outbox_messages.payload contains the validated event envelope
event_id in goat_identity_events and outbox_messages must match
schema_version must be validated before commit
analytics/count consumers must be idempotent by event_id
```

## Permissions

Permission names:

```text
goat.read
goat.create
goat.update_identity
goat.attach_identifier
goat.retire_identifier
goat.resolve_conflict
goat.import_legacy
goat.view_dirty_data
goat.request_correction
```

Role mapping:

```text
admin
  all Phase 1 permissions

park_head
  goat.read scoped to park
  goat.request_correction
  optional low-risk conflict resolution if approved by business

operator
  goat.read scoped to assigned work/location
  goat.request_correction

verifier
  goat.read and identity evidence view

ceo_internal
  full Goat OS product-admin access by granted tenant scope
```

Enforcement notes:

```text
handlers ask PermissionPolicy for the principal's allowed scopes before querying
repositories receive explicit scope filters; they do not fetch then filter in memory
admin merge/resolve commands check every affected goat_id before transaction commit
operator lookup never returns out-of-scope goat summaries, source evidence, or hidden-match counts
dashboard count APIs filter counter rows by granted scope
```

## Frontend Requirements

### Admin Web

Use existing dashboard shell/components where useful.

Pages:

```text
/goats
/goats/{goat_id}
/identity/imports
/identity/imports/{import_run_id}
/identity/conflicts
/identity/conflicts/{conflict_id}
```

### Operator Mobile

Screens:

```text
Goat lookup
Scan RFID/tag
Goat compact passport card
Multiple-match warning
Correction request
```

No direct DB/Firebase/GCS access. Use generated API client.

## Import Pipeline

Implementation sequence:

```text
1. accept extract or local file path in admin/import tool
2. create legacy_import_run
3. write legacy_import_rows
4. normalize rows
5. process in chunks
6. create identity_decisions + goats/identifiers/events/audit/outbox for clean rows
7. create candidates/conflicts for dirty rows
8. write audit/outbox records for conflicts and review cases
9. expose import summary
```

Chunking:

```text
process rows in bounded batches
load legacy_import_policy by policy_version before staging rows
source_row_key = source_system + source_dataset + stable source record identifier
source_row_version_hash = hash(stable normalized projection, hash_recipe_version)
import_run_id identifies one execution only
import_run_id must not be part of business idempotency
same source_row_key + same source_row_version_hash must not create duplicate goat/identifier/event/conflict
same source_row_key + different source_row_version_hash requires field-level diff before action
dry_run records import-run-scoped preview results but does not mutate canonical goats/identifiers/counters or active conflict queues
never load full future herd into memory
```

Phase 1 RFID runner foundation:

```text
backend/cmd/rfid-import is the thin CLI entrypoint
backend/internal/legacy_import owns workbook parsing, source-key/hash creation,
row-state classification, and staging orchestration
backend/internal/legacy_import/adapters/postgres owns legacy_import_runs and
legacy_import_rows persistence; cmd must not write those tables directly
the runner supports local .xlsx only in this slice; live Google Sheets export is
deferred and discovery returns a skipped status for source_type=google_sheet
required CLI flags: input workbook path, tenant_id
optional CLI flags: sheet name, dry-run, batch-size, started-by actor UUID,
source-name, policy-version defaulting to phase1-rfid-db-import-v1
source discovery runs before import and classifies Shape 2 as importable,
Shape 1 as recognized but not importable until mapping extension, and
operational/unknown sources as rejected before staging
source_system and source_dataset always come from the approved policy row
the runner validates the policy exists and status=approved before import
source_file_hash is sha256 over workbook bytes
source_file_ref is null for local CLI imports so local absolute paths are never
stored
dry-run writes a completed legacy_import_runs row and aggregate counts only; it
does not insert legacy_import_rows
real staging writes a running legacy_import_runs row, inserts rows in bounded
batches, then marks the run completed or failed
sanitized anomaly reports are local ignored CSV artifacts; the final rehearsal
report is generated after rfid-apply so it includes staging reasons and
apply-stage review reasons such as unknown_status_mapping and
species_or_breed_requires_review. Reports group actual error_reason and
processing_reasons codes, mask RFID/old-tag values by default, and hash
source_row_key references because source_row_key can contain source identifier
evidence. Grouped review summaries may read raw_payload only through the safe
source-label whitelist Tag, Breed, Gender, Farm, Shed, and Partition, falling
back to normalized fields for those same labels when raw is absent. They never
emit full raw_payload, raw row JSON, raw RFID, or raw old-tag values.
Grouped CSV cells are spreadsheet-formula safe. duplicate_old_tag_same_scope
groups use stable non-reversible old-tag/scope refs instead of raw values or
short masks, so short old-tag labels do not collapse into one bucket.
species_or_breed_requires_review groups are for human alias-vs-exclusion
decisions, not auto-aliasing. blank_old_tag_suffix groups support the explicit
`rfid-apply --allow-rfid-only-blank-suffix` policy and do not imply suffix
derivation.
created_goat_count, updated_goat_count, and conflict_count remain 0 during
staging; canonical apply updates created_goat_count for rows it creates
```

RFID runner normalization:

```text
RFID, Old ID, Old ID Suffix, and other identifier-like fields are read from
OOXML cell text/raw values and never through float conversion
RFID is trimmed and uppercased for staging; duplicate RFID values within the
same workbook hard-error all affected rows
Old ID becomes normalized_old_tag
Old ID Suffix becomes normalized_park_code and old_tag scope; historic aliases
CJB -> CBE and BLR -> CPT are normalized while preserving raw_payload evidence
Current Farm never replaces Old ID Suffix as the old_tag scope
blank Old ID Suffix routes the row to needs_review
same old_tag in the same normalized scope routes affected rows to needs_review
same old_tag in different normalized scopes is allowed
Gender is the only sex source; blank/unknown Gender routes to needs_review
F2, F2-Male, F2-Female, and Fattening are growth/status context only and must
not set sex
RFID-less rows are staged with a policy-compatible source_row_key when stable
old-tag/scope evidence exists and route to needs_review
clean staged rows use processing_state=pending
review rows use processing_state=needs_review
hard malformed/unparseable rows use processing_state=error with error_reason
do not invent processing states such as staged, anomaly, or review
```

Local full-stack rehearsal:

```text
Use docs/runbooks/local-full-stack-rehearsal.md for the repeatable local path:
local XLSX export -> discovery -> dry-run -> staging -> rfid-apply -> final
anomaly/review report -> counter rebuild -> backend API smoke -> admin-web
typecheck/build.
DB-writing local rehearsal CLIs use backend/internal/platform/localtarget and
must reject non-local/staging/prod/Cloud SQL database targets.
Frontend/admin-web must consume backend APIs only and must not import Google
Sheets, Apps Script, BigQuery, direct CSV exports, or XLSX readers for live
data.
```

Source key and hash implementation:

```text
source_row_key is built from policy source_key_recipe fields in fixed order:
source_system, source_dataset, normalized_old_tag, normalized_park_code, rfid
tenant_id is stored separately and must never be prepended to source_row_key
row_number, sorted_position, and export_line_number are forbidden in
source_row_key
source_key_recipe_version is copied from the policy row
source_row_version_hash is sha256 over a deterministic fixed-order
serialization of the policy hash_recipe include_fields after normalization
hash_recipe_version is copied from the policy row
same logical row re-imported with the same normalized projection produces the
same source_row_key and source_row_version_hash
the same source_row_key with a changed normalized projection produces a
different source_row_version_hash and routes the new staged row to needs_review
row staging uses ON CONFLICT against the 000002 uniqueness constraint:
(tenant_id, source_system, source_dataset, source_row_key,
source_row_version_hash)
```

The staging runner intentionally stops at legacy_import_runs and
legacy_import_rows. Canonical apply is a separate Phase 1 command so workbook
parsing, review classification, and canonical mutation can be retried and
audited independently.

Phase 1 RFID canonical apply:

```text
backend/cmd/rfid-apply is the thin CLI entrypoint
backend/internal/legacy_import owns apply orchestration for staged RFID rows
apply requires tenant_id and import_run_id
optional flags: dry-run preview, batch-size, actor UUID, policy-version guard
apply only reads completed non-dry-run import runs
run policy_version must exist, be approved, and match run source_system/source_dataset
apply resolves one active Mesha org party (org_type=mesha); tenant_id is not a
party_id and must not be used as owner/custodian
for the first RFID DB import only, safe created goats use Mesha party as
custodian_party_id, goat_ownership.owner_party_id, and
goat_custody_history.custodian_party_id
```

Apply row eligibility:

```text
only legacy_import_rows.processing_state='pending' rows for the requested run
are eligible
needs_review, error, rejected, created_goat, and auto_linked rows are skipped
valid RFID is required and must not already be active globally
Gender-derived sex is required; F2/F2-Male/F2-Female/Fattening never provide sex
nonblank Tag/status labels must map through legacy_status_mappings for
source_system='legacy_rfid_db'
unknown status mappings and review_required mappings route to needs_review
blank Tag/status defaults lifecycle_status='alive' only
breed/species must resolve to a clear active goat breed alias; unsafe or
non-goat labels such as Anantapur Sheep route to needs_review
same source_row_key with different source_row_version_hash routes to
needs_review before canonical creation
deterministic SQL data/integrity failures for one staged row (SQLSTATE class
22/23) must not abort the whole import run; that row's canonical write
transaction rolls back, then a separate short transaction marks the row
processing_state='error' with sanitized SQLSTATE/constraint metadata only and
refreshes legacy_import_runs.error_count
transient, concurrency, infrastructure, context, and unknown failures abort the
run instead of quarantining the row
error rows are not auto-retried by rfid-apply; an operator must inspect/fix and
promote the row back to pending before retry
```

Confirmed non-goat terminal disposition:

```text
rfid-apply --reject-confirmed-non-goats is an explicit post-apply command
dry-run counts current confirmed non-goat rows without mutation
execute updates only tenant/import_run scoped legacy_import_rows where:
  processing_state='needs_review'
  review reasons include species_or_breed_requires_review
  source breed label is a confirmed non-goat label (currently Anantapur Sheep)
it sets processing_state='rejected' and error_reason='confirmed_non_goat_species'
it writes audit_log action=legacy_import_row.rejected per row
it preserves raw_payload and normalized_payload evidence
it creates no goats, identifiers, identity_decisions, identity events, or outbox rows
unknown or unclassified goat breeds are not rejected
replay is idempotent because already rejected rows are skipped
future correction/review tooling may reverse a row by creating a new audited state-change command
```

Future apply ops hardening backlog:

```text
last-attempt visibility fields may be added later:
apply_started_at, apply_completed_at, apply_status, sanitized apply_error_reason
these fields are latest-attempt visibility only, not full history
if full attempt history matters, add a separate apply_attempts table instead of
overloading legacy_import_runs
add delete/update guards for foundational seed/reference rows such as Mesha
party/org, active goat breed seeds, identifier policies, and import policies
to prevent systemic FK failures from missing reference data
bounded per-row retry for 40001/40P01 is deferred until apply becomes parallel
or those failures appear in real operation
unknown non-SQL errors abort; deterministic non-SQL row-local faults may be
isolated only after proven reachable and safely sanitizable
```

Canonical writes for one safe row commit in one transaction:

```text
identity_decisions
goats
goat_identifiers for active primary RFID scope global
optional goat_identifiers for active old_tag scope park:<normalized_park_code>
  when same-scope uniqueness is clear
goat_ownership with share_bps=10000, status=active, owner_party_id=Mesha party
goat_custody_history with reason=first_rfid_import and custodian_party_id=Mesha party
goat_identity_events event_type=goat.created
identity_decision_goats
identity_decision_identifiers
identity_decision_events using exact goat_identity_events.recorded_at
audit_log
outbox_messages
idempotency_keys completion
legacy_import_rows.processing_state='created_goat' and matched_goat_id
legacy_import_runs.created_goat_count increment
```

Apply does not write goat_location_history, candidates, conflicts, correction
requests, counters, projection rows, or an outbox publisher. Dirty row
conflict/candidate creation and auto-link reconciliation remain later slices.

Canonical apply idempotency:

```text
business idempotency key derives from tenant_id, command name, source_system,
source_dataset, source_row_key, and source_row_version_hash
import_run_id is intentionally excluded from the business idempotency identity
exact replay returns the existing created goat and marks the staged row
created_goat without duplicate goat/identifier/ownership/custody/decision/event/outbox rows
same source_row_key with a different source_row_version_hash is review-driven,
not auto-applied
```

Apply decision/event contract:

```text
identity_decisions.decision_type=create_goat
identity_decisions.decision_result=imported_from_rfid_source
identity_decisions.decision_state=approved
identity_decisions.decided_by_type=import_policy
policy_version is the import run policy_version
decision record evidence_refs include import_run and source_record only using
existing evidence_type enum values
goat.created event envelope aggregate_type=goat, aggregate_id=goat_id,
subject_type=goat, subject_id=goat_id, actor.actor_type=import_job
goat_identity_events is inserted before outbox_messages so the outbox tenant
integrity trigger sees the canonical event first
```

## Scale And Performance Requirements

Designed for:

```text
50k goats current
1M+ goats future
large import batches
operator search under field conditions
dashboard counts without full-herd API scans
```

Rules:

```text
search requires indexed identifier lookup
dashboard counts come from API/analytics projections, not giant client fetches
Phase 1 count endpoints read goat_identity_counters or equivalent projection, not full-table scans
import jobs run in chunks
conflict queues are paginated
all list APIs require limit and scope filters
location-scoped reads use farm_id/park_id/shed_id/cohort_id columns or an approved closure strategy
```

Required indexes:

```text
goat_identifiers(identifier_type, normalized_value, scope_key) where identifier_type in ('old_tag', 'sheet_row_id', 'purchase_load_id', 'temp_field_id', 'external_system_id') and status = 'active'
goat_identifiers(normalized_value) where identifier_type = 'rfid' and status = 'active'
goat_identifiers(normalized_value) where identifier_type = 'visual_tag' and status = 'active'
goat_identifiers(goat_id, identifier_type) where is_primary_for_goat = true and status = 'active'
identifier_policies(policy_version, identifier_type)
goats(tenant_id, lifecycle_status)
goats(tenant_id, custodian_party_id, lifecycle_status)
goats(tenant_id, reproductive_status)
goats(tenant_id, growth_cohort_tag)
goats(tenant_id, health_status)
goats(farm_id, lifecycle_status)
goats(park_id, lifecycle_status)
goats(shed_id, lifecycle_status)
goats(cohort_id, lifecycle_status)
identity_conflicts(state, severity, created_at)
identity_conflicts(identifier_type, identifier_value)
legacy_import_policies(source_system, source_dataset, status)
legacy_import_runs(policy_version, status, started_at)
legacy_import_rows(import_run_id, processing_state, row_number)
legacy_import_rows(source_row_key, source_row_version_hash)
goat_identity_events(goat_id, occurred_at desc)
identity_correction_requests(state, park_id, created_at)
goat_ownership(tenant_id, goat_id, status, valid_from, valid_to)
goat_ownership(owner_party_id, status)
goat_custody_history(tenant_id, goat_id, valid_from, valid_to)
goat_custody_history(custodian_party_id, valid_from, valid_to)
goat_identity_counters(tenant_id, custodian_party_id, farm_id, park_id, shed_id, lifecycle_status)
goat_identity_counters(tenant_id, breed_id, sex, lifecycle_status)
goat_identity_counters(tenant_id, health_status)
goat_identity_counters(tenant_id, growth_cohort_tag)
goat_identity_counters(tenant_id, management_stage)
goat_identity_counters(tenant_id, reproductive_status)
goat_identity_events(tenant_id, recorded_at asc, identity_event_id asc)
goat_identity_counter_processed_events(tenant_id, processed_at, event_recorded_at)
user_scope_grants(user_id, role, scope_type, scope_id, status)
audit_log(resource_type, resource_id, created_at)
idempotency_keys(expires_at)
outbox_messages(status, next_attempt_at, created_at)
outbox_messages(aggregate_type, aggregate_id, created_at)
```

Initial API targets:

```text
identifier resolve p95 < 300 ms on indexed lookup
goat passport fetch p95 < 500 ms
conflict queue p95 < 800 ms with pagination
import throughput target defined after XLSX sample baseline
```

## Observability

Metrics:

```text
import rows processed
import rows/sec
identity conflicts opened
identity conflicts resolved
duplicate active identifier violations
identifier lookup latency
goat search latency
dirty-data queue age
merged-goat redirect count
event/outbox lag
outbox publish failures
outbox dead-letter count
counter projection rebuild duration
counter projection stale/rebuilding count
```

Logs:

```text
import run started/completed
conflict created/resolved
identifier attached/retired
merge approved/rejected
permission denial
```

Alerts required before Phase 1 release:

```text
duplicate active identifier count > 0
dirty-data queue oldest age above threshold
import error rate spike
outbox oldest-unsent age breach
identity lookup p99 breach
conflict queue growth spike
```

## Testing Plan

Unit tests:

```text
identifier normalization
identifier format validation
identifier policy lookup/enforcement
import policy immutability and source-key recipe validation
match scoring
auto-link rules
conflict creation rules
merge decision validation
merged-goat redirect behavior
permission checks
counter projection update rules
counter rebuild watermark protocol and safe replacement strategy
incremental counter checkpoint, event_id+recorded_at dedupe, noops, rebuild-required, and retention semantics
future before/after delta semantics for richer counter event types
RBAC scope filtering
audit log write rules
outbox write/envelope validation rules
idempotency_keys conflict/replay behavior
partition lifecycle/default-partition checks
idempotency retention sweep rules
```

Integration tests:

```text
import clean row -> goat + identifiers + ownership + custody + decision + event + outbox + audit
duplicate old tag in same scope -> needs_review, no duplicate goat
RFID already linked -> needs_review, no duplicate goat
invalid RFID format -> review/reject before occupying global RFID uniqueness
approve candidate -> identifier link / merge path
reject candidate -> no canonical mutation
same source row in a new import run -> no duplicate goat/event
same source row with same payload -> no duplicate goat/identifier/event/conflict
same source row with changed payload -> needs_review before canonical mutation
spreadsheet row reorder -> no new source_row_key, no duplicate goat
dry-run import/apply -> preview only, no canonical goats/identifiers/active conflicts/counters
two tagless goats with same breed/sex/location -> two review cases, not one collapsed goat
export timestamp/formatting change -> hash unchanged
identity-material diff -> review, not auto-apply
lookup merged goat -> redirects to survivor
out-of-scope lookup -> no goat/source/conflict detail leakage
operator correction request -> review queue, no direct identity mutation
duplicate insert race -> conflict response, no crashed import chunk
status-axis/location change -> current cache and counters update consistently
import completion -> counters rebuild without hot-row contention
count endpoint while rebuild is running -> returns projection freshness, no raw fallback
scoped identifier resolve without scope -> multiple_matches / needs_review, no blind pick
merge transfer -> survivor primary identifiers preserved unless explicitly changed
visual tag lookup -> indexed non-unique search
outbox relay retry -> one downstream event by event_id
expired idempotency keys -> sweeper removes only after replay/audit window
missing future event/audit partition -> alert and DEFAULT partition protects writes
breed text normalization -> breed_id before breed counter rebuild
```

Contract tests:

```text
OpenAPI validates goat search/passport/conflict responses
event payloads validate against JSON Schema
decision records validate against JSON Schema
```

Frontend tests:

```text
goat search renders
passport detail renders identity warnings
conflict review shows side-by-side evidence
operator lookup handles single match / multiple match / no match
```

Load tests:

```text
50k imported goats baseline
1M synthetic goats
3M synthetic identifiers across RFID/old_tag/visual_tag/source IDs
12 months of identity/audit partitions
high-volume identifier lookup with scoped and unscoped old tags
visual tag lookup
large import run
outbox lag during import burst
audit/event partition write baseline
partition rollover/default-partition protection
conflict queue pagination
dashboard count endpoint under load
RBAC-filtered search under load
```

## Inputs And Policy Handling For Migrations

Contracts, Postgres migrations, import seeds, identifier/import policy seeds,
and canonical import logic can start using the locked Phase 1 shape.

The items below must be represented in staging/review/policy design, but
unanswered later-business policies must not block Phase 1 import. If a value is
not confirmed, preserve the raw source value, map what is known, and route the
unclear part to review instead of inventing a default.

```text
sample XLSX/Sheet export
column dictionary
status labels and raw source values
official breed/species seed list from glossary/source findings
legacy breed text mapping to normalized breed_id
location hierarchy source
old tag uniqueness rule
RFID format examples if available
known duplicate examples
known missing-tag examples
known tag reuse examples across farm/load/source if they exist
source stable ID column or approved synthesized-key recipe
hash recipe field list and hash_recipe_version
field-diff routing policy: auto-apply vs review
approved identifier_policy_version
approved legacy_import_policy policy_version for first migration
```

Locked migration decisions:

These are no longer a reason to stop Phase 1. The implementation agent may add
review states, nullable geo fields, raw legacy value columns, and policy seams
where later business behavior is still unconfirmed.

### Pre-Migration Legacy Discovery Pass

Before asking for human confirmation, run a read-only discovery pass over the
legacy artifacts. This is the evidence stage for the locked decisions.

Inputs:

```text
private herd workbook kept outside git
<mesha-workspace>/dashboard/public/data/*.csv
<mesha-workspace>/vgoats-dashboard/public/data/*.csv
<mesha-workspace>/dashboard/app/api/**/*
<mesha-workspace>/dashboard/lib/**/*
<mesha-workspace>/vgoats-dashboard/app/api/**/*
<mesha-workspace>/vgoats-dashboard/lib/**/*
<mesha-workspace>/slack-automation-scripts/**/*.{js,ts,docx}
<mesha-workspace>/procurement_app/src/**/*.{ts,tsx}
```

Required proposal outputs:

```text
old_tag uniqueness:
  park-scoped uniqueness is locked for Phase 1: old_tag_number + normalized
  park_code. Group normalized old tags by source, park, historic park alias,
  load/source if present, and count
  distinct candidate goats/rows. Show duplicate examples as anonymized source
  refs, not raw private rows. Identify which source column should populate the
  park scope, normalize CJB -> CBE and BLR -> CPT, preserve source aliases as
  evidence, and route missing/conflicting park values to review.

status vocabulary:
  extract distinct statuses from XLSX/CSVs/dashboard display code. Propose
  mapping into separate lifecycle_status, reproductive_status, growth_cohort_tag,
  management_stage, and health_status axes. Seed confirmed labels from Drive
  source docs: K0, K1, K2, K3, F2, Warmup, M0, ICU, Quarantine, Pregnant,
  Non Pregnant, Mother, Mother Milking Waiting, Milking Warmup, Milking, Buck,
  Flushing, and Breeding. F0 is not active and should remain reviewable if found.
  Health diagnosis follow-up behavior is legacy-derived from Diagnosis Form,
  Problem, Follow Up, Adults SOP, and Kids SOP. Known K0/K1/K2/K3/M0/F2/Warmup
  meanings live in context/product/glossary.md.

tenant/party/custody/location mapping:
  identify columns/code paths for farm, park, shed, shed_tag, load, source, CBE,
  CPT, historic aliases CJB/BLR, holdings, and unknown locations. Propose tenant, owner_party,
  custodian_party, current_location, and unknown-location handling. CBE is
  Coimbatore, CPT is Channapatna, HF is Holding Farm, and Origin Farm is
  source/origin evidence. If only advance-paid HF holding is known, set
  ownership to shared/pending review instead of asserting full Mesha or partner
  ownership.

first migration source:
  list candidate sources, sheet/tab/file name, row counts, column dictionary,
  freshness/as-of date if available, and whether the source appears canonical or
  derived.

SOP/form inventory:
  summarize Slack/App Script forms/workflows found, including health, feed,
  shifting/death, procurement, video verification, attendance/manager flows,
  and any form fields/conditions visible in code/docs. This informs later SOP
  phases and prevents losing current operating knowledge.

policy-only decisions:
  record future policy decisions not required for Phase 1 import, such as
  sale/allocation blocking labels, non-health status/stage task triggers, and
  official geo details. Seed known rules from Drive source docs: ICU/serious
  illness, Quarantine/viral disease, milk-drinking kids up to K3, and future
  medication withdrawal periods restrict sale/allocation; K/F kids weigh every
  Monday; adults weigh monthly on the 15th; vaccinations run by schedule; feed
  changes are experiment-driven. First import scope is locked to RFID DB first. HF
  partner handling is locked to minimal external party records plus physical
  location records only when source evidence supports them.
```

Discovery output rules:

```text
do not commit raw XLSX rows, raw Slack payloads, PII, tokens, or media URLs
commit only derived proposals, aggregate counts, source paths, column names,
anonymized examples, and confidence notes if a proposal doc is created
state dataset coverage; never claim global uniqueness from a partial snapshot
future sale/task/geo policy may be proposed for later confirmation, but it must
not be invented inside Phase 1 migrations or import logic
```

```text
goat_id format: UUID v7 unless explicitly rejected
display_id generator: server-generated G-000001 style global code; unique, readable, searchable, not manually typed
tenant/party/custody mapping: each canonical goat row must map to a tenant,
owner_party_id, and custodian_party_id; source rows that cannot be safely
mapped remain in staging/review
geo/location mapping: locations include country, state_region, district, pincode, lat, lng, timezone where known
old_tag scope policy: locked as old_tag_number + normalized park_code for Phase 1;
historic aliases CJB -> CBE and BLR -> CPT normalize while preserving source
evidence; missing/conflicting park values route to review
identifier policies per type: uniqueness scope, auto-link allowed?, primary allowed?
identifier_policy_version: immutable once first import/mutation uses it
temporary identity minimum: field-created temp requires photo/proof; import-created temp requires source row evidence; both require current/unknown location and review state
status structure: lifecycle_status, reproductive_status, growth_cohort_tag, management_stage, and health_status are separate axes
status semantics: Phase 1 stores raw labels and known axis mappings; Drive source docs seed sale-blocking and routine-task policy inputs, but Phase 1 does not execute those policies
site-code meanings: CBE = Coimbatore, CJB = historic CBE alias, CPT = Channapatna, BLR = historic CPT alias, HF = Holding Farm, Origin Farm = source/origin evidence
location conflict rule: latest DB event is the current placement source when RFID DB shed is stale; preserve both pieces of evidence and route disagreement to reconciliation/review
source_row_key recipe: stable source ID, never spreadsheet row position
source_row_version_hash recipe: stable projection fields and hash_recipe_version
first RFID apply rule: clean pending RFID rows can create goats only through the
rfid-apply command; unsafe rows become needs_review and dirty conflict/candidate
creation remains deferred
legacy_import_policy: source-key recipe, hash recipe, field-diff policy, and auto-link policy approved before first import
first_import_scope: RFID DB only for the first import; event-log/tagless temporary identities are a later import pass
```

## Implementation Order

```text
1. OpenAPI + JSON Schema contracts for identity/passport/conflict/decision.
2. Run/read the legacy discovery proposal before schema defaults:
   docs/phases/phase-01-goat-passport/legacy-discovery-proposals.md
   context/source-findings/drive-docs-findings.md
3. Confirm pre-migration locked decisions; stop for human answers if any are unknown.
4. Postgres migrations for identity/location/import/reconciliation/policy/outbox tables.
5. Identifier and import policy seeds for first migration sample.
6. Import staging and normalization.
7. Deterministic matching/conflict engine.
8. Admin APIs.
9. App lookup APIs.
10. Admin web goat search/passport/conflict screens.
11. Operator mobile lookup shell.
12. Dashboard count adapter from canonical identity.
13. Tests/load baseline.
```

## Open Technical Questions

```text
Should location hierarchy be imported before goats or created during import?
What exact RFID DB export file/date is the first migration test fixture?
Locked: custodian_party_id may differ from current physical farm/location.
Open ops/policy question: when custody and placement differ, which roles/scopes can view or mutate the goat?
What exact geo values should be filled for CBE, CPT, and Holding Farm rows when source files omit pincode/coordinates? City/state can seed first; exact address/GPS is nullable.
```

## Phase 1 Exit Criteria

```text
schema reviewed and migrated locally
contracts reviewed and generated
sample import succeeds
dirty cases become review records
admin can resolve at least one duplicate-tag case
admin can merge duplicate goats without deleting either record
operator can lookup goat by identifier
dashboard copy can show canonical counts through generated API contract backed by imported sample data
all identity mutations emit outbox events
all identity mutations write audit + event + outbox + idempotency records in one transaction
tests cover clean import, duplicate conflict, scoped lookup, outbox retry, and idempotent replay
1M-goat synthetic load baseline validates indexes, partitions, and count projections
```
