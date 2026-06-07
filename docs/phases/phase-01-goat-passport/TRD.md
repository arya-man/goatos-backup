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
growth_cohort_tag is the K0/K1/K2/K3/F1/F2/M0-style cohort axis
health_status is the healthy/sick/ICU/quarantine/under-treatment axis
do not mash compound legacy labels into lifecycle_status
identity_state: clean | needs_review | disputed | merged | inactive
merged goats set merged_into_goat_id and reject normal future writes
row_version supports optimistic concurrency for admin edits
daily task assignment stays in workforce/tasks and must not be modeled as custody
```

Indexes:

```text
(lifecycle_status)
(tenant_id, lifecycle_status)
(tenant_id, custodian_party_id, lifecycle_status)
(tenant_id, reproductive_status)
(tenant_id, growth_cohort_tag)
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
seed every imported goat with Mesha owner_party_id at share_bps = 10000
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
unique(source_row_key, source_row_version_hash)
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
created_at timestamptz not null
```

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

Approval authority:

```text
central admin can approve identity merges and assign which roles are allowed to approve specific decision types
farm admin can approve merge/correction decisions only when central admin grants that role and scope
park head or operator can request/recommend corrections, but cannot mutate canonical identity directly unless granted an approval role
mass approval is allowed only as a configured admin workflow after validation preview, evidence sampling, and dry-run results
batch merge approval creates one batch decision plus individual child decision records for every affected goat pair
high-risk identity merges must never be silently auto-approved by AI/import logic
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
relay claims pending rows in bounded chunks with SKIP LOCKED or equivalent
stale publishing rows are reclaimed by lease timeout
publish success records provider metadata and marks published
publish failure increments attempt_count and schedules bounded backoff
poison rows move to failed/dead-letter state with alertable error metadata
published rows are kept only for hot replay/debug window, then archived/exported or deleted by scheduler
relay uses event_id/idempotency_key so retry cannot create a second downstream business event
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
projection is rebuildable from goats + identity/location events
large import runs rebuild counters at import completion in bounded grouped queries
steady-state single-goat changes update counters incrementally
same-transaction counter updates are allowed only for low-volume steady-state writes
parallel import workers must not contend on hot counter rows
dashboard/API responses include as_of_recorded_at and is_rebuilding
if counters are stale/rebuilding, dashboards show freshness state and still must not fall back to raw table scans
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
reproductive_status: tenant_id + reproductive_status
```

Do not materialize arbitrary combinations of all nullable dimensions. New grains
must be explicitly added with a named `counter_grain`, rebuild query, read API,
and load test.

Breed counter grain requires normalized breed data. Do not use raw free-text
breed strings as counter dimensions; map legacy breed text to controlled
`breed_id` before rebuilding `breed_sex_lifecycle`.

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

## API Contracts

Common API rules:

```text
All list endpoints require limit and cursor.
All write endpoints require Idempotency-Key.
PATCH endpoints require row_version or ETag.
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
POST /admin/identity/correction-requests/{correction_request_id}/resolve
```

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
GET /analytics/identity/counts?grain=&tenant_id=&custodian_party_id=&farm_id=&park_id=&shed_id=&cohort_id=&lifecycle_status=&reproductive_status=&growth_cohort_tag=&health_status=&identity_state=&breed_id=&sex=
```

Count endpoint rules:

```text
reads goat_identity_counters or governed analytics facade, never raw goats count(*)
requires explicit grain and scope filters
applies RBAC before reading counter rows
returns count_value plus as_of_recorded_at, source_import_run_id, and is_rebuilding
does not support arbitrary dimension combinations outside named materialized grains
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
TypeScript clients are generated before frontend/mobile work starts.
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
evidence_ids[]
reason
row_version
```

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
`context/analytics/final-analytics-infra.md`. OpenAPI/JSON Schema may expose
aliases such as subject_id only if generated from the same canonical envelope.

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
  goat.read and dashboard access by grant
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
goat_identity_counters(tenant_id, reproductive_status)
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
RBAC scope filtering
audit log write rules
outbox write/envelope validation rules
idempotency_keys conflict/replay behavior
partition lifecycle/default-partition checks
idempotency retention sweep rules
```

Integration tests:

```text
import clean row -> goat + identifiers + event
duplicate old tag -> conflict
RFID already linked -> conflict
invalid RFID format -> review/reject before occupying global RFID uniqueness
approve candidate -> identifier link / merge path
reject candidate -> no canonical mutation
same source row in a new import run -> no duplicate goat/event
same source row with same payload -> no duplicate goat/identifier/event/conflict
same source row with changed payload -> new row version processed with audit trail
spreadsheet row reorder -> no new source_row_key, no duplicate goat
dry-run import -> preview only, no canonical goats/identifiers/active conflicts/counters
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

## Inputs Needed Before Migrations And Canonical Import Logic

Contracts can start now using the locked Phase 1 shape. The items below block
Postgres migrations, import seeds, identifier/import policy seeds, and canonical
import logic only.

```text
sample XLSX/Sheet export
column dictionary
official status list
official breed list if available
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

Pre-migration locked decisions:

These are human/business answers, not agent defaults. The implementation agent
may list options and consequences, but must stop and ask if any value is
unknown. Do not write migrations, seed policies, or canonical import logic past
an unanswered pre-migration decision.

### Pre-Migration Legacy Discovery Pass

Before asking for human confirmation, run a read-only discovery pass over the
legacy artifacts. This is the evidence stage for the locked decisions.

Inputs:

```text
<mesha-workspace>/source-material/private-data/private herd workbook
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
  farm-scoped uniqueness is locked for Phase 1. Group normalized old tags by
  source, farm, load/source if present, and count
  distinct candidate goats/rows. Show duplicate examples as anonymized source
  refs, not raw private rows. Identify which source column should populate the
  farm scope, and route missing/conflicting farm values to review.

status vocabulary:
  extract distinct statuses from XLSX/CSVs/dashboard display code. Propose
  mapping into separate lifecycle_status, reproductive_status, growth_cohort_tag,
  and health_status axes. Flag official labels, sale-blocking labels, and
  SOP-trigger labels for ops confirmation.

tenant/party/custody/location mapping:
  identify columns/code paths for farm, park, shed, shed_tag, load, source, CBE,
  CPT, holdings, and unknown locations. Propose tenant, owner_party,
  custodian_party, current_location, and unknown-location handling.

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
  list decisions not derivable from data, such as merge approval authority and
  minimum evidence for tagless temporary goats, with recommended options and
  consequences.
```

Discovery output rules:

```text
do not commit raw XLSX rows, raw Slack payloads, PII, tokens, or media URLs
commit only derived proposals, aggregate counts, source paths, column names,
anonymized examples, and confidence notes if a proposal doc is created
state dataset coverage; never claim global uniqueness from a partial snapshot
agent may propose, a human owner must confirm before migrations/seeds/import logic
```

```text
goat_id format: UUID v7 unless explicitly rejected
display_id generator: server-generated G-000001 style global code; unique, readable, searchable, not manually typed
tenant/party/custody mapping: each canonical goat row must map to a tenant,
owner_party_id, and custodian_party_id; source rows that cannot be safely
mapped remain in staging/review
geo/location mapping: locations include country, state_region, district, pincode, lat, lng, timezone where known
old_tag scope policy: locked farm-scoped for Phase 1; ops input is only which
source column populates farm scope and how missing/conflicting farm values route
to review
identifier policies per type: uniqueness scope, auto-link allowed?, primary allowed?
identifier_policy_version: immutable once first import/mutation uses it
temporary identity minimum: field-created temp requires photo/proof; import-created temp requires source row evidence; both require current/unknown location and review state
status structure: lifecycle_status, reproductive_status, growth_cohort_tag, and health_status are separate axes
status semantics: official labels, sale-blocking labels, and SOP-trigger labels require ops confirmation
source_row_key recipe: stable source ID, never spreadsheet row position
source_row_version_hash recipe: stable projection fields and hash_recipe_version
legacy_import_policy: source-key recipe, hash recipe, field-diff policy, and auto-link policy approved before first import
first_import_scope: RFID-linked registry only, or RFID registry plus event-log/tagless temporary identities
```

## Implementation Order

```text
1. OpenAPI + JSON Schema contracts for identity/passport/conflict/decision.
2. Run/read the legacy discovery proposal before schema defaults:
   docs/phases/phase-01-goat-passport/legacy-discovery-proposals.md
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
What is the first approved source file for migration testing?
Locked: custodian_party_id may differ from current physical farm/location.
Open ops/policy question: when custody and placement differ, which roles/scopes can view or mutate the goat?
What default geo values should be used for CBE, CPT, and Holding Farm rows when source files omit pincode/coordinates?
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
