# Phase 1 Build Status

Status: active implementation.

This file records what is actually built so future sessions do not rely on
conversation memory.

## Built And Pushed

Contracts:

```text
contracts/openapi/app-api.yaml
contracts/openapi/admin-api.yaml
contracts/openapi/analytics-api.yaml
contracts/jsonschema/domain-event-envelope.schema.json
contracts/jsonschema/decision-record.schema.json
contracts/examples/
tools/contract-validation/
```

Database foundation:

```text
backend/migrations/postgres/000001_phase_1_identity_foundation.sql
backend/migrations/postgres/000002_phase_1_identity_schema_hardening.sql
backend/migrations/postgres/000003_phase_1_correction_outbox_support.sql
backend/migrations/postgres/000004_phase_1_correction_resolve_support.sql
backend/migrations/postgres/000005_phase_1_conflict_resolve_support.sql
backend/migrations/postgres/000006_phase_1_candidate_review_support.sql
backend/migrations/postgres/000007_phase_1_reporting_counter_rebuild_support.sql
backend/migrations/postgres/000008_phase_1_reporting_counts_pagination.sql
backend/migrations/postgres/000009_phase_1_reporting_incremental_counters.sql
backend/tests/integration/validate-postgres-migrations.sh
make validate-migrations
```

Go backend read foundation:

```text
backend/cmd/api
backend/cmd/outbox-relay
backend/cmd/rebuild-identity-counters
backend/cmd/update-identity-counters
backend/internal/bootstrap
backend/internal/platform/logger
backend/internal/platform/httpmiddleware
backend/internal/platform/postgres
backend/internal/identity/domain
backend/internal/identity/app
backend/internal/identity/ports
backend/internal/identity/adapters/http
backend/internal/identity/adapters/postgres
backend/internal/identity/adapters/postgres/sqlc
backend/internal/reporting
backend/internal/reporting/adapters/postgres/sqlc
backend/sqlc.yaml
tools/sqlc/
```

Go backend write foundation:

```text
POST /identity/correction-requests
transactional correction request create
POST /admin/identity/correction-requests/{correction_request_id}/resolve
transactional correction request resolve
POST /admin/goats/{goat_id}/identifiers
transactional admin identifier attach
POST /admin/goats/{goat_id}/identifiers/{identifier_id}/retire
transactional admin identifier retire
POST /admin/identity/conflicts/{conflict_id}/resolve
transactional conflict resolve for merge_goats, reject_match,
request_field_verification, and mark_identifier_disputed
GET /admin/identity/candidates
actionable identity candidate queue list
POST /admin/identity/candidates/{candidate_id}/reject
transactional candidate rejection decision
tenant/route-namespaced idempotency key handling
same-transaction audit_log + optional outbox_messages persistence
correction_request domain event envelope payloads
resolve_correction_request decision records
attach_identifier and retire_identifier decision records
goat.identifier.added and goat.identifier.retired domain event envelopes
merge_goats, reject_match, request_field_verification, and
mark_identifier_disputed decision records
goat.identity.merge_approved domain event envelopes
goat.identifier.disputed domain event envelopes
reject candidate decision records
```

Outbox relay foundation:

```text
backend/cmd/outbox-relay
backend/internal/outbox/domain
backend/internal/outbox/app
backend/internal/outbox/ports
backend/internal/outbox/adapters/postgres
backend/internal/outbox/adapters/publisher/logging

The relay is local/dev worker/CLI foundation only. It claims pending rows where
next_attempt_at is null or due, ordered by created_at/outbox_id, in bounded
FOR UPDATE SKIP LOCKED batches. Claiming happens in a short transaction, then
publishing happens outside the claim transaction through a publisher port.

Status behavior:
  pending -> publishing when claimed, with attempt_count incremented
  publishing -> published on success, with published_at set and last_error cleared
  publishing -> pending on retryable publish failure, with future next_attempt_at
  publishing -> failed when the domain event envelope is invalid
  pending/publishing -> dead_letter when max attempts are exhausted

Fresh outbox rows use status=pending and next_attempt_at=null. Stale publishing
rows are reclaimed by lease timeout without resetting attempt_count; fresh
publishing leases are not stolen. Pending rows already at max attempts are moved
to dead_letter at claim time and are not published. Publisher panics are
recovered per row and treated as retryable failures unless attempts are
exhausted. The relay validates outbox payloads against
contracts/jsonschema/domain-event-envelope.schema.json using
github.com/santhosh-tekuri/jsonschema/v6. Relay update SQL only touches status,
attempt_count, next_attempt_at, last_error, published_at, and updated_at; it
does not update tenant_id or event_id. The logging/no-op publisher logs safe
metadata only and never logs raw payloads.
```

Legacy import staging foundation:

```text
backend/cmd/rfid-import
backend/cmd/rfid-apply
backend/internal/legacy_import
backend/internal/legacy_import/adapters/postgres
backend/internal/legacy_import/adapters/postgres/sqlc
backend/internal/legacy_import/testdata/synthetic_rfid_import.xlsx

RFID workbook import runner loads approved policy phase1-rfid-db-import-v1,
parses .xlsx rows as raw text, normalizes RFID/old tag/scope/status evidence,
and stages legacy_import_runs plus legacy_import_rows only.

RFID apply runner reads completed non-dry-run staging runs and creates canonical
goats only for safe pending rows. It writes identity_decisions, goats,
goat_identifiers, ownership, custody history, goat_identity_events,
identity_decision_* join rows, audit_log, outbox_messages, idempotency
completion, and legacy row/run state in one transaction per applied row.
```

## Verified Behaviors

Schema/migration invariants:

```text
display_id generated by DB sequence
global active RFID uniqueness
tenant-aware scoped identifier uniqueness
same old tag allowed in a different tenant/scope
legacy import row replay uniqueness namespaced by tenant/source/dataset
active ownership share total must equal 10000 bps
goat_id-changing ownership updates check old and new goats
merged goat normal writes blocked
merged goat child writes blocked
goat hard delete blocked
tenant-scoped child/location/decision/outbox/user-scope mismatches rejected
monthly event/audit partitions and default backstop route rows
reporting processed counter events use a partition-aware
(tenant_id, event_id, event_recorded_at) FK to goat_identity_events
```

Go read/API behaviors:

```text
goat passport lookup by goat_id or display_id
merged goat lookup follows merged_into_goat_id to the live survivor
merge redirect chains are followed with max-hop and cycle guards
identifier resolve state-machine:
  single_match
  multiple_matches
  no_match
  needs_review
  merged_redirect
same-scope multiple_matches can attach conflict_id
cross-scope multiple_matches must not attach a wrong conflict_id
goat search/list is tenant-scoped and excludes merged goats
identity conflict list/detail read paths exist
reporting-owned identity counts read path exists; counters are populated by
reporting rebuild and the local incremental counter update worker
request middleware preserves/generates request IDs and trace context
analytics tenant_id query/header mismatch is rejected
deferred endpoints return typed not_implemented error envelopes
stable static identity reads are generated by sqlc:
  goat lookup by goat_id/display_id
  identifiers for goat
  scoped open conflict lookup
  conflict summary/goat/source-record detail joins
dynamic optional-filter reads remain handwritten:
  goat search
  identifier match lookup
  conflict list
reporting/adapters/postgres/sqlc owns identity counter projection reads,
rebuild, and local incremental update statements:
  ListIdentityCounts
  ListIdentityEventsAfterCheckpoint
  shared grouped insert per Phase 1 counter grain
  created-goat incremental counter upsert with processed-event dedupe
sqlc drift check regenerates schema/code and fails on stale generated files
sqlc query-plan validation covers every generated query.sql read and rejects
hot-path sequential scans
correction request create:
  requires Idempotency-Key
  requires temporary X-GoatOS-Actor-ID uuid
  rejects unknown JSON fields
  supports goat-linked and goatless requests
  validates goat-linked tenant ownership
  maps location_scope to DB location FK columns
  writes correction row, idempotency row, audit row, and outbox row in one tx
  exact idempotent replay returns the original correction request
  same key with different request hash conflicts
  persisted outbox payload validates against domain-event-envelope JSON Schema
correction request resolve:
  requires Idempotency-Key
  requires temporary X-GoatOS-Actor-ID uuid
  rejects unknown JSON fields and old evidence_ids payloads
  requires typed evidence_refs and current row_version
  allows open/assigned/needs_field_check -> approved/rejected/closed or needs_field_check
  treats approved/rejected/closed as terminal except exact idempotent replay
  rejects same-state no-op resolves unless exact idempotent replay
  uses conditional row_version update and increments row_version on success
  sets resolved_at only for terminal approved/rejected/closed targets
  writes identity_decisions decision_type=resolve_correction_request
  uses policy_version=phase1-manual-correction-review-v1
  maps needs_field_check to decision_state=needs_review and result=needs_field_check
  maps closed to decision_state=approved and result=closed
  preserves reason and typed evidence_refs in the decision record JSON
  writes correction row, decision row, idempotency row, audit row, and outbox row in one tx
  exact idempotent replay returns the original correction request and decision
  same key with different request hash conflicts
  persisted decision_record validates against decision-record JSON Schema
  persisted outbox payload validates against domain-event-envelope JSON Schema
  does not directly mutate goat identity and does not write goat_identity_events
admin identifier add/retire:
  requires Idempotency-Key
  requires temporary X-GoatOS-Actor-ID uuid
  rejects unknown JSON fields and old evidence_ids payloads
  requires typed evidence_refs, scope_key for add, and current goat row_version
  normalizes RFID by trim + uppercase; other identifier values are trimmed
  records policy_version=phase1-identifier-v1; `rfid_v1` is named but the
  concrete RFID format validator is not locked until source RFID examples are
  supplied
  guards goat mutation with conditional row_version update and identity_state <> merged
  disambiguates goat guard failures as not_found or write_conflict
  rejects stale row_version, merged goat, wrong-tenant goat, wrong-goat identifier,
  already-retired identifier, duplicate active RFID, duplicate same-scope old_tag,
  disallowed primary_allowed policy requests, and active primary-per-goat conflicts
  allows the same old_tag value in a different scope
  writes identity_decisions decision_type=attach_identifier or retire_identifier
  uses policy_version=phase1-identifier-v1
  writes identity_decision_identifiers and identity_decision_events join rows
  writes goat_identity_events with event_type goat.identifier.added or
  goat.identifier.retired
  writes goat row_version update, identifier mutation, decision, event, audit,
  outbox, and idempotency completion in one transaction
  exact idempotent replay rebuilds the admin goat response from the DB
  same key with different request_hash conflicts
  persisted decision_record validates against decision-record JSON Schema
  persisted outbox payload validates against domain-event-envelope JSON Schema
  goat outbox rows are DB-validated against same-tenant goat_identity_events
conflict resolve:
  implements POST /admin/identity/conflicts/{conflict_id}/resolve for
  decision_type/result pairs:
    merge_goats + same_goat_merge
    reject_match + candidate_rejected
    request_field_verification + field_verification_required
    mark_identifier_disputed + different_goats_identifier_disputed
  keeps create_goat + new_goat_required as typed not_implemented because the
  contract/TRD do not define the required goat creation fields
  rejects unknown JSON fields and old evidence_ids payloads
  validates exact decision_type/decision_result pairs
  requires typed evidence_refs, affected_goat_ids, identifier_actions, reason,
  current conflict row_version, Idempotency-Key, and temporary
  X-GoatOS-Actor-ID uuid
  requires survivor_goat_id only for merge_goats and rejects it for non-merge
  decisions
  adds identity_conflicts.row_version via migration 000005 and uses a single
  conditional conflict update on target state + row_version
  maps reject_match to state=rejected, terminal resolved_at/resolved_by,
  decision_state=rejected, audit only, and no goat_identity_events/outbox
  maps request_field_verification to state=needs_field_check,
  decision_state=needs_review, no resolved_at/resolved_by, audit only, and no
  goat_identity_events/outbox; same-state field-check with a new idempotency key
  is a write conflict
  maps mark_identifier_disputed to state=resolved, terminal
  resolved_at/resolved_by, decision_state=approved, selected identifier status
  disputed, primary flag cleared, one goat.identifier.disputed event/outbox per
  selected identifier, and one goat row_version bump per goat with disputed
  identifiers
  requires mark_identifier_disputed to supply explicit identifier_actions with
  action=dispute and identifier_id; identifiers must be same-tenant, active,
  attached to conflict member goats, and match conflict identifier type/value
  when those fields are present
  allows merge only for duplicate-identity conflict types:
  possible_duplicate_goat, duplicate_active_identifier, rfid_already_linked,
  and old_tag_reused
  rejects location/status/missing/tagless conflict types for merge_goats
  requires survivor and affected goats to belong to the conflict
  sets merge override GUCs with SET LOCAL only inside the merge transaction
  resolves survivor redirects, detects redirect cycles/max-hop, and repoints
  goats currently redirecting to merged losers to the final survivor
  treats goats.merged_into_goat_id as the authoritative live-survivor pointer
  and keeps goat_merge_links as immutable merge history
  locks affected goat rows in deterministic goat_id order
  keeps survivor live; marks merged goats identity_state=merged with
  merged_into_goat_id pointing to the final survivor
  writes one goat_merge_links row and one goat.identity.merge_approved
  goat_identity_event/outbox row per newly merged goat
  writes identity_decision_goats roles survivor and merged
  writes identity_decision_identifiers for supplied/applied identifier actions
  transfers only explicitly requested non-colliding loser identifiers and
  demotes transferred identifiers from primary by default
  bumps the survivor goat row_version when a loser identifier is transferred to
  the survivor
  default-retires loser identifiers that are not explicitly transferred
  records the resolved live survivor/merged goat IDs in the decision record
  while preserving requested affected goat IDs for traceability
  exact idempotent replay rebuilds the merge response from DB state
  same key with different request_hash conflicts
  persisted decision_record validates against decision-record JSON Schema
  persisted outbox payload validates against domain-event-envelope JSON Schema
  decision-only reject_match/request_field_verification changes are not
  event-stream visible until a conflict-aggregate event contract/projection
  worker is defined; projections must read canonical conflict state
candidate review:
  implements GET /admin/identity/candidates as the actionable queue:
  proposed and needs_review candidates only; approved/rejected/expired are
  excluded by default
  requires bounded limit and uses keyset pagination by created_at desc,
  candidate_id desc
  CandidateSummary exposes row_version because reject needs optimistic
  concurrency and there is no candidate-detail endpoint
  implements POST /admin/identity/candidates/{candidate_id}/reject
  rejects unknown JSON fields and old evidence_ids payloads; use typed
  evidence_refs
  requires Idempotency-Key, temporary X-GoatOS-Actor-ID, reason,
  evidence_refs, and candidate row_version
  stored idempotency key is
  <tenant_id>:rejectIdentityCandidate:<candidate_id>:<client_key>
  inserts identity_decisions before the guarded candidate update, then gates
  the mutation with one conditional update on tenant, candidate,
  state in proposed/needs_review, and row_version
  maps to decision_type=reject_match, decision_result=candidate_rejected,
  decision_state=rejected, policy_version=phase1-manual-correction-review-v1
  updates identity_match_candidates to rejected, sets reviewed_by/reviewed_at
  and decision_id, increments candidate row_version, writes audit_log and
  idempotency completion in one transaction
  does not mutate goat identity, bump goat row_version, write
  goat_identity_events, or write outbox_messages
  exact idempotent replay rebuilds from DB state, not cached response bodies
  POST /admin/identity/candidates/{candidate_id}/approve remains typed
  not_implemented until canonical mutation semantics are contract-defined
RFID source-of-truth staging:
  CLI requires input workbook path and tenant_id; optional dry-run, batch-size,
  started-by actor UUID, source-name, and policy-version are supported
  source_system and source_dataset come from the approved
  legacy_import_policies row, not CLI flags
  policy phase1-rfid-db-import-v1 must exist and be approved before importing
  source_file_hash stores a sha256 of workbook bytes; source_file_ref remains
  null so local absolute paths are not persisted
  dry-run writes one completed legacy_import_runs row with aggregate counts only
  and writes no legacy_import_rows
  real staging writes legacy_import_runs as running -> completed or failed and
  inserts legacy_import_rows in bounded batches
  source_row_key uses policy source_key_recipe fields in order:
  source_system, source_dataset, normalized_old_tag, normalized_park_code, RFID
  source_row_key never prepends tenant_id and never uses spreadsheet row_number
  source_row_version_hash uses deterministic fixed-order serialization of the
  policy hash_recipe include_fields and stores hash_recipe_version
  same tenant/source/dataset/source_row_key/source_row_version_hash re-import is
  ON CONFLICT DO NOTHING; the same source key with a different hash stages a
  new row as needs_review
  RFID values are read as text and never through float conversion
  duplicate RFID values within a workbook hard-error the affected rows
  duplicate old_tag within the same normalized park/scope routes affected rows
  to needs_review; the same old_tag in different scopes remains allowed
  blank Old ID Suffix, blank Gender, and RFID-less rows route to needs_review
  F2/F2-Male/F2-Female/Fattening map only to growth/status context; sex comes
  only from the Gender column
  staging does not mutate goats, goat_identifiers, goat_identity_events,
  outbox_messages, counters, candidates, conflicts, or correction requests
  synthetic .xlsx fixture rows are committed; raw private workbook rows, RFID
  values, local paths, screenshots, names, media URLs, and PII are not committed
RFID source-of-truth canonical apply:
  CLI requires tenant_id and import_run_id; optional dry-run preview,
  batch-size, actor-id, and policy-version guard are supported
  apply loads the completed non-dry-run import run, verifies its approved
  policy and source_system/source_dataset, and resolves exactly one active Mesha
  org party with org_type=mesha
  apply processes only legacy_import_rows.processing_state=pending for the
  requested import_run_id; needs_review/error/rejected/created_goat/auto_linked
  rows are skipped
  safe rows create goats with DB-generated display_id, species=goat,
  identity_state=clean, sex from Gender only, status axes from
  legacy_status_mappings, active goat breed_id when the breed alias is clear,
  and Mesha party as custodian/owner for this first RFID DB import only
  each created goat gets active primary RFID scope global and optional active
  primary old_tag scope park:<normalized_park_code> when same-scope uniqueness
  is clear
  ownership is 10000 bps active and custody history reason is
  first_rfid_import; goat_location_history and counters are not written in this
  slice
  active RFID conflicts, active same-scope old_tag conflicts, unknown or
  review_required status mappings, unsafe species/breed labels, and changed
  source_row_version_hash route the row to needs_review without creating a goat
  deterministic SQL data/integrity per-row failures roll back that row's
  canonical write transaction, mark only that row processing_state=error in a
  separate short transaction with sanitized SQLSTATE/constraint metadata,
  refresh error_count, and continue to later pending rows; transient,
  concurrency, infrastructure, context, and unknown failures abort the run; error
  rows are not auto-retried until an operator promotes them back to pending
  stable apply idempotency derives from tenant, command, source_system,
  source_dataset, source_row_key, and source_row_version_hash; import_run_id is
  intentionally not part of the business idempotency identity
  decision records use decision_type=create_goat,
  decision_result=imported_from_rfid_source,
  decision_state=approved, decided_by_type=import_policy, and evidence_refs for
  import_run plus source_record
  goat.created events are inserted before outbox rows, and
  identity_decision_events stores the exact goat_identity_events.recorded_at
```

## Temporary Scaffolds

```text
X-GoatOS-Tenant-ID is a local/dev tenant-scope placeholder.
It is not production authentication or authorization.
Replace it with the auth/RBAC adapter before deploy-like environments.

X-GoatOS-Actor-ID is the temporary local/dev actor scaffold for writes until
auth/RBAC lands. It must parse as uuid and is not production authentication.

The identity Postgres repository still has handwritten pgx for dynamic
optional-filter reads. New write-heavy/import/reconciliation SQL should use
generated sqlc unless the query shape is intentionally dynamic and documented.

AI proposal guardrail is documented but no AI worker is built yet:
AI-authored identity suggestions must use `ai_proposal`, remain
`proposed`/`needs_review`, include explainable reasons and source/evidence
links, and must never write as `system_rule` or `import_policy`. A future
AI-worker migration should add a candidate CHECK mirroring
`identity_decisions_ai_not_approved_check`.
```

## Deferred Work

```text
auth/RBAC adapter
AI suggestion worker and candidate-state DB hardening
remaining admin write handlers except identifier add/retire, correction resolve,
candidate reject, and built conflict resolve paths
dirty staged-row conflict/candidate creation and auto-link reconciliation
create_goat conflict decision until required goat creation fields are
contract-defined
candidate approve canonical mutation until required semantics are
contract-defined
standalone merge command handler
unmerge command handler/contract
real Google Pub/Sub outbox publisher and production worker deployment
frontend event/DLQ UI
production async projection worker deployment and Pub/Sub consumer wiring
externally visible counter rebuild-status metadata table
partition auto-creation worker or pg_partman
OpenTelemetry spans/metrics/exporters
generated client drift checks
fresh private RFID DB import run
P8 sales/allocation/promise behavior
```

Counter projection rebuild:

```text
backend/internal/reporting owns goat_identity_counters reads and rebuilds.
/analytics/identity/counts is still the public analytics route, but bootstrap
wires it to reporting-owned service/repository code; backend/internal/identity
has a boundary check preventing new goat_identity_counters ownership outside
generated sqlc schema/model dumps.

Count reads are paginated and never silently truncated. The endpoint requires
`limit` 1..500 and accepts optional opaque base64url keyset `cursor` values.
Rows order by `count_value DESC, counter_id ASC`; the cursor carries version,
`count_value`, and `counter_id`. Structurally invalid cursors return 400, while
structurally valid arbitrary positions are accepted and may return empty pages.
The cursor is scoped to the same query shape/projection version and is not a
durable cross-rebuild snapshot guarantee. Freshness fields remain projection
freshness (`as_of_recorded_at`, `source_import_run_id`, `is_rebuilding`), not
raw count freshness. `goat_identity_counters_lookup_idx` remains for now; old
updated_at-order lookup cleanup is deferred until usage is reviewed.

The local rebuild CLI is:

  cd backend && go run ./cmd/rebuild-identity-counters \
    -tenant-id <tenant_uuid> \
    [-source-import-run-id <import_run_uuid>] \
    [-grains tenant_lifecycle,health_status]

Rebuild semantics:
  one repeatable-read transaction per committed tenant rebuild attempt
  per-tenant advisory lock is taken before validation/rebuild work
  serialization/deadlock failures retry the whole transaction in a bounded loop
  as_of_recorded_at is max(goat_identity_events.recorded_at) for the tenant
  inside the rebuild snapshot; it is null when the tenant has no identity events
  the rebuild never uses now() or goats.updated_at as freshness watermark
  each rebuilt tenant+grain deletes stale buckets and inserts the freshly
  grouped complete result set inside the same transaction
  after successful rebuild, goat_identity_counter_projection_state stores the
  checkpoint (recorded_at + event_id) under the same advisory lock
  is_rebuilding remains false on final rows; richer externally visible rebuild
  status is deferred to a future metadata table

The local incremental update CLI is:

  cd backend && go run ./cmd/update-identity-counters \
    -tenant-id <tenant_uuid> \
    [-limit 500] \
    [-processed-events-retention 720h]

  or: make update-identity-counters TENANT_ID=<tenant_uuid>

Incremental update semantics:
  processes goat_identity_events ordered by recorded_at ASC, identity_event_id ASC
  uses goat_identity_counter_processed_events for event_id+recorded_at dedupe
  goat.created increments the same 10 Phase 1 grains as rebuild from the shared
  goat_identity_counter_memberships view
  goat.identifier.added/retired/disputed are noops that advance the checkpoint
  goat.identity.merge_approved and unknown event types mark rebuild_required and
  stop before later events are processed
  missing projection state with counters or events marks rebuild_required; a
  tenant with no counters and no events gets an empty projection state
  freshness.warning surfaces rebuild_required on analytics reads
  processed-event retention pruning is bounded by tenant and checkpoint
  projection failures remain outside canonical identity write transactions
  production async worker deployment, Pub/Sub consumer wiring, before/after
  deltas for richer event types, and drift automation remain deferred

Phase 1 counter membership is pinned:
  exclude identity_state=merged from all grains because merged goats are
  tombstones/redirects; exclude identity_state=inactive from Phase 1 operational
  counts; lifecycle-bearing grains count dead/sold lifecycle_status buckets for
  non-merged, non-inactive goats; non-lifecycle operational grains and
  custodian_identity count alive goats only; location grains use only current
  goats.park_id and goats.shed_id cache columns for Phase 1; no farm_lifecycle
  or cohort_lifecycle grain exists; custodian grains use goats.custodian_party_id.
```

## Future Ops Hardening Backlog

```text
RFID apply attempt visibility:
  add last-attempt fields later if operators need per-run visibility:
  apply_started_at, apply_completed_at, apply_status, and sanitized
  apply_error_reason. These fields describe the latest apply attempt only; they
  are not a full attempt ledger.

RFID apply attempt history:
  if full attempt history becomes necessary, add a separate apply_attempts table
  instead of overloading legacy_import_runs.

Foundational seed/reference delete guards:
  add DB delete/update guards for permanent seed/reference rows such as the
  Mesha party/org, active goat breed seeds, identifier policies, and import
  policies. The goal is to prevent systemic FK failures from missing
  foundational rows instead of teaching the row-error classifier to guess around
  preventable reference-data deletion.

Analytics count grain-dimension validator drift:
  check-contract-drift validates the analytics counts example against a
  hand-maintained grain -> allowed dimensions table. That table matches the
  reporting rebuild SQL today, but future grain dimension changes must update
  both. Add a later hardening test or derived spec that diffs the validator
  table against backend/internal/reporting/adapters/postgres/sqlc/commands.sql
  rebuild INSERT column sets, or replace both with one shared declared
  grain-dimension spec.

Apply retry policy:
  add bounded per-row retry only if apply becomes parallel or real 40001/40P01
  failures appear in operations. Until then, transient/concurrency/unknown
  failures abort the run and do not mark rows error.

Non-SQL deterministic row-local faults:
  keep unknown non-SQL errors as run-aborting failures. Only extend row-error
  isolation to deterministic non-SQL row-local faults after such faults are
  proven reachable and can be sanitized safely.
```

## Merge/Unmerge Contract Status

```text
The only contract path that performs a merge is
  POST /admin/identity/conflicts/{conflict_id}/resolve
with ResolveConflictRequest.decision_type = merge_goats and
decision_result = same_goat_merge, returning ResolveConflictResponse with an
optional MergeResult.
That merge path is now implemented for duplicate-identity conflict types.
admin-api.yaml exposes no POST /admin/identity/merges,
POST /admin/goats/{goat_id}/merge, or equivalent standalone merge route.
```

Unmerge remains contract-blocked:

```text
No unmerge route in admin-api.yaml or the TRD API list.
No unmerge request/response schema anywhere.
No unmerge_goats decision_type in decision-record.schema.json.
No unmerge / merge_reversed event_type in domain-event-envelope.schema.json.
Unmerge exists only as TRD prose and needs a future contract decision before
implementation.
```

## Next Write-Slice Guardrails

```text
Write-side tables already exist in the Phase 1 migrations. Migration 000003 is
the narrow correction-request outbox support patch: correction_request outbox
events do not require a goat_identity_events row because goatless correction
requests have no goat_id. Do not add new write-side tables for
idempotency_keys, outbox_messages, audit_log, goat_identity_events,
identity_correction_requests, goat_merge_links, or identity_decisions unless the
current schema cannot safely support the required behavior.

The first write foundation is built:
  - repository-owned Postgres transaction around command persistence
  - idempotency via INSERT ... ON CONFLICT, not SELECT-then-INSERT
  - same-transaction audit_log + outbox where the command does not mutate goat
    identity
  - POST /identity/correction-requests

Idempotency rules for write endpoints:
  - Idempotency-Key is required when the OpenAPI contract says it is required.
  - For synchronous writes, idempotency row creation, domain write, audit, event
    and outbox must happen in one transaction. The started -> completed
    transition is internal to that transaction; if the transaction fails, the
    idempotency row rolls back too.
  - INSERT ... ON CONFLICT on the idempotency_keys primary key is the
    concurrency primitive. The second concurrent request must block on the row
    conflict, then read the committed completed result.
  - The idempotency_keys primary key is key-only, so the stored key must be
    tenant/route namespaced, for example
    <tenant_id>:<command_or_route>:<client_idempotency_key>. The raw client
    header must not be stored as the bare primary key because two tenants can
    send the same key string.
  - request_hash is based on canonical JSON body + command identity + route
    identity + tenant_id.
  - Completed replay re-fetches the result by result_type/result_id; the
    idempotency table does not store full response bodies.
  - For POST /identity/correction-requests, fresh create returns 201 and exact
    completed replay returns 200.
  - Same key with different request_hash is a conflict.
  - Committed started-key reclaim via expires_at is not part of this synchronous
    write path; it belongs to a future async/two-phase command design if one is
    introduced.

Correction request create exists in app-api.yaml. Admin correction resolve and
admin identifier add/retire exist in admin-api.yaml and are implemented write
commands.
Create supports goat-linked and goatless requests; goat_id is optional in the
contract and DB. Goatless correction requests use the correction request as the
outbox aggregate and do not create a goat timeline event. evidence_refs maps
into the DB evidence jsonb shape and must never be NULL. request_type must stay
within the contract/DB enum. Create writes state='open', populates scope, and
requires the temporary actor header to parse as uuid because requested_by is uuid
NOT NULL.

Correction resolve writes an identity_decisions row with
decision_type=resolve_correction_request and
policy_version=phase1-manual-correction-review-v1. The request must include
typed evidence_refs and row_version. The state machine is
open/assigned/needs_field_check -> approved/rejected/closed or
needs_field_check, and already-terminal requests must not be re-resolved except
as exact idempotent replay. decision_state is the lifecycle of the decision
record; decision_result is the outcome. Do not reuse external review wording for
decision_state.

Identifier add/retire mutates goat identity. It must guard the goat with one
conditional update on tenant_id + goat_id + row_version + identity_state <>
merged, then perform the identifier change and write identity_decisions,
identity_decision_identifiers, goat_identity_events, identity_decision_events,
audit_log, outbox_messages, and idempotency completion in the same transaction.
The outbox aggregate is the goat and the subject is the identifier. Do not use
merge override GUCs for normal identifier mutation; merged goats must reject as
write conflicts. Replay must rebuild from the database, not cached response
bodies.

Conflict resolve mutates conflict state through
POST /admin/identity/conflicts/{conflict_id}/resolve. It must keep using
SET LOCAL goatos.allow_merged_goat_update='on' and
goatos.allow_merged_goat_child_write='on' only inside the merge transaction,
resolve identifier actions before marking losers merged, and write
identity_decisions, identity_decision_goats, identity_decision_identifiers,
goat_merge_links, goat_identity_events, identity_decision_events, audit_log,
outbox_messages, and idempotency completion in one transaction for merge_goats.
reject_match and request_field_verification are decision-only conflict state
changes: they write decision + conflict update + audit + idempotency in one
transaction and do not emit goat_identity_events/outbox rows. mark_identifier_disputed
is a goat identity mutation: it requires explicit dispute identifier actions,
updates selected identifiers to disputed, bumps each affected goat row_version
once, and writes goat_identity_events, identity_decision_events,
outbox_messages, audit_log, and idempotency completion in one transaction.
Merge transfer actions also bump the survivor goat row_version because the
survivor's identifier surface changes. create_goat remains typed
not_implemented until the goat creation fields are contract-defined.

Row-version invariant for this phase: every implemented write that changes a
goat identity surface must bump that goat's row_version exactly once per
transaction. Future identity mutation slices must apply the same rule before
projection/cache invalidation depends on row_version: candidate approve/reject
only when it mutates identity, correction auto-apply when it applies
attach/retire/dispute/status changes, unmerge when redirect or identifier state
changes, and create_goat starts with its initial row_version unless follow-up
mutations happen.

Concurrency pattern split: row_version is for stale human/admin identity
decisions and aggregate invalidation; import progress, retry counts, dashboard
projection counters, and other numerical counters use atomic SQL increments or
bounded rebuilds instead of row-version compare-and-retry loops.

Until auth/RBAC lands, write endpoints using X-GoatOS-Tenant-ID or temporary
actor headers are local/dev scaffolding only. They are a deploy gate for shared,
staging, or production-like environments.

Outbox relay is a standalone local/dev CLI foundation. It is not run as an API
server goroutine and does not include real cloud publishing. Production
deployment, Google Pub/Sub adapter, event consumers, production projection
workers, rebuild-status metadata, and richer DLQ operations remain deferred.
```

## Validation Commands

Current expected checks:

```text
make test
make check
PATH="/path/to/pinned/sqlc/bin:$PATH" make sqlc-check
make validate-sqlc-plans
make validate-migrations
./tools/agent-hooks/check-boundaries.sh
./tools/agent-hooks/check-contract-drift.sh
git diff --check
```

CI runs guardrails, Go tests, `make sqlc-check`, `make validate-sqlc-plans`,
`make validate-migrations`, and `git diff --check` with Docker Postgres pinned
to `postgres:16.9-alpine`.

For Go-only changes, `make check` is the preferred root command. Running
`go test ./...` from the repo root is not valid because the Go module lives under
`backend/`; use `make test` or `cd backend && go test ./...`.
