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
packages/api-client
make api-client-generate
make api-client-check
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
backend/migrations/postgres/000010_phase_1_rfid_plain_status_mappings.sql
backend/migrations/postgres/000011_phase_1_rfid_breed_cross_mappings.sql
backend/migrations/postgres/000012_phase_1_rfid_blank_suffix_apply_candidates.sql
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
backend/internal/permissions
backend/internal/permissions/adapters/postgres
backend/internal/platform/auth
backend/internal/platform/logger
backend/internal/platform/httpmiddleware
backend/internal/platform/localtarget
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

HTTP auth/RBAC foundation:

```text
backend/internal/platform/auth
backend/internal/platform/httpmiddleware/auth.go
backend/internal/permissions
backend/internal/permissions/adapters/postgres

API bootstrap defaults to bearer auth when GOATOS_AUTH_MODE is empty.
Bearer mode requires GOATOS_AUTH_ISSUER, GOATOS_AUTH_AUDIENCE, and
GOATOS_AUTH_HS256_SECRET with at least 32 bytes.

The bootstrap verifier is HS256 only and uses standard-library HMAC-SHA256,
base64url, and JSON parsing. The server pins expected alg=HS256 and rejects
alg=none, wrong alg, wrong secret, malformed tokens, missing sub/tenant,
wrong issuer/audience, expired tokens, and future nbf tokens. Extra token role
claims are ignored; they are not authorization authority.
Tokens whose exp is more than the configured max TTL in the future are rejected;
the default max TTL is 24h. This caps bootstrap HS256 blast radius only and is
not a substitute for production revocation/rotation infrastructure.

Bearer middleware attaches tenant_id from token tenant_id and actor/user_id
from token sub. Normal bearer mode overwrites/ignores X-GoatOS-Tenant-ID and
X-GoatOS-Actor-ID. Only GET /healthz and GET /readyz bypass auth.

Authorization uses active tenant-scope rows in user_scope_grants:
user_id=sub, tenant_id=token tenant, scope_type=tenant, scope_id=tenant_id,
status=active, valid_from<=now, and valid_to null or future. Revoked, inactive,
expired, future, and missing grants deny live. Route permissions are enforced by
the shared backend/internal/permissions registry and fail closed for protected
routes not in the registry.
Multiple active tenant grants are unioned: if any active matching tenant grant
role confers the required permission, the request is authorized. For example,
operator+verifier grants authorize verifier-only review permissions, while
admin-only routes still require an active admin role.

dev_headers mode is retained only as an explicit local-development escape hatch:
GOATOS_AUTH_MODE=dev_headers plus GOATOS_DEV_HEADERS_ALLOW=true, allowed only
when GOATOS_ENV is exactly local, dev, or test, with a loud warning. It still
uses the same DB-backed grant checks after reading local headers.
backend/cmd/mint-dev-token mints bootstrap HS256 local/dev tokens using the
same backend/internal/platform/auth signing and validation rules as the API
verifier. backend/cmd/seed-dev-grant inserts explicit active tenant-scope
user_scope_grants only when GOATOS_ENV is exactly local, dev, or test and the
database target is local. It rejects production/staging-looking targets, remote
hosts, and Cloud SQL-style Unix socket paths; it is not a migration and does not
silently grant admin. backend/tests/integration/smoke-auth-local.sh uses one
shared GOATOS_AUTH_* config for the API, token minting, local grant seed, and
admin-web generated-client smoke.
```

Frontend readiness foundation:

```text
packages/api-client generates OpenAPI TypeScript types from app-api,
admin-api, and analytics-api. apps/admin-web imports @goatos/api-client through
its local file dependency and package-lock.json.

check-contract-drift now validates OpenAPI/JSON Schema/examples and then runs
make api-client-check, so generated-client drift is no longer deferred.

apps/admin-web is a buildable Phase 1 readiness shell only. It shows disabled
placeholder tabs for Herd Search, Goat Passport, Import Runs, Dirty Data Review,
Corrections, and Analytics Counts. It does not fetch herd data and does not wire
501/incomplete endpoints.

Executable legacy BigQuery/Sheets routes were removed from apps/admin-web:
app/api/*, lib/bigquery.ts, CSV data-loader, and the legacy route pages/hooks
that fetched those app/api routes are no longer in the Next build.
```

Local Docker storage safety:

```text
docs/runbooks/local-docker-storage.md
tools/dev/docker-storage-report.sh
tools/dev/docker-cleanup-goatos.sh
tools/dev/test-docker-storage-scripts.sh
make docker-storage-report
make docker-cleanup-goatos-dry-run
make docker-cleanup-goatos-execute
make docker-storage-scripts-test

Local Docker is the default daily dev path for Docker Postgres, tests, and small
synthetic data. GCP setup is not required for normal coding. goatos-dev Cloud
SQL comes later for explicit cloud rehearsal. goatos-stg later holds the
persistent 1M benchmark dataset. Local 1M tests are temporary only:
create explicit temp volume -> run test -> export summary/report -> delete temp
volume.

Permanent local dev Postgres volume name:
goatos_dev_pg_data

Only these temp volume prefixes are cleanup-eligible:
goatos_tmp_
goatos_test_
goatos_bench_tmp_

The cleanup script is dry-run by default, never runs docker volume prune, never
deletes global build cache/images, skips goatos_dev_pg_data, and skips
unclassified volumes. Docker Desktop for Mac may keep host-side Docker.raw/VM
disk space allocated after in-Docker deletes; use Docker Desktop disk
usage/reclaim/reset workflow if macOS still reports the disk as full.

Future cloud defaults are asia-south1 Mumbai, never US by default.
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
goats only for safe rows. By default it uses the original pending-only apply
path. With explicit `--allow-rfid-only-blank-suffix`, it also scans candidate
rows whose staged review evidence contains `blank_old_tag_suffix`; Go re-locks
each row and only applies rows whose complete reason set is exactly
`blank_old_tag_suffix`. Successful rows create the goat plus primary RFID,
create no old_tag identifier, and preserve unresolved old-tag source evidence
on the import row and decision/audit trail. It writes identity_decisions,
goats, goat_identifiers, ownership, custody history, goat_identity_events,
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
  requires authenticated actor uuid
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
  requires authenticated actor uuid
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
  requires authenticated actor uuid
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
  current conflict row_version, Idempotency-Key, and authenticated actor uuid
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
  requires Idempotency-Key, authenticated actor, reason,
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
  CLI supports source discovery, sheet-by-name selection, dry-run staging
  preview, real staging, and sanitized anomaly report generation for local
  rehearsal
  CLI requires input workbook path and tenant_id for import; optional --sheet,
  dry-run, batch-size, started-by actor UUID, source-name, and policy-version
  are supported
  local DB-writing import/apply/counter commands share the
  backend/internal/platform/localtarget guard: GOATOS_ENV must be local/dev and
  DATABASE_URL must point to loopback or approved local socket paths, never
  production/staging-looking hosts or Cloud SQL sockets
  source_system and source_dataset come from the approved
  legacy_import_policies row, not CLI flags
  policy phase1-rfid-db-import-v1 must exist and be approved before importing
  source_file_hash stores a sha256 of workbook bytes; source_file_ref remains
  null so local absolute paths are not persisted
  dry-run writes one completed legacy_import_runs row with aggregate counts only
  and writes no legacy_import_rows
  source discovery classifies Shape 2 RFID headers as importable, recognizes
  Shape 1 RFID headers as blocked until mapping extension, and rejects
  operational/unknown source shapes before staging
  Google Sheet live export/import is deliberately skipped in Phase 1 local
  rehearsal; operators must export to a local XLSX and run the backend import
  pipeline
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
  during staging; only the explicit RFID apply flag can later process rows
  whose complete reason set is exactly blank_old_tag_suffix
  F2/F2-Male/F2-Female/Fattening map only to growth/status context; sex comes
  only from the Gender column
  staging does not mutate goats, goat_identifiers, goat_identity_events,
  outbox_messages, counters, candidates, conflicts, or correction requests
  anomaly reports are local ignored CSV artifacts under
  .codex-goatos-render/import-reports by default; the final rehearsal report is
  generated after rfid-apply so it includes staging reasons and apply-stage
  review reasons such as unknown_status_mapping and
  species_or_breed_requires_review
  reports group actual emitted reason codes from error_reason and
  processing_reasons, mask RFID/old-tag values by default, and hash
  source_row_key references because source keys can contain source identifiers
  rfid-import --anomaly-report also writes a reviewer-focused CSV pack under a
  per-run reviewer-<import_run_id>/ subdirectory:
  review-summary.csv, needs-review-rows.csv,
  non-goat-exclusion-candidates.csv, blank-old-tag-suffix.csv,
  blank-gender.csv, duplicate-old-tag-same-scope.csv, and README.txt
  non-goat-exclusion-candidates.csv carries confirmed non-goat breed/species
  labels such as Anantapur Sheep so they are visible for exclusion review; the
  tool does not duplicate those same sheep rows into a breed/species issue file
  future non-confirmed breed/species labels emit
  breed/species-needs-classification.csv instead
  reviewer_action and reviewer_notes columns are scratch-only; Goat OS does not
  ingest edited reviewer CSVs yet. Corrections re-enter through the source
  workbook or a future approved correction overlay, then normal import/apply is
  rerun
  grouped review summaries read raw_payload only through safe source-label
  fields Tag, Breed, Gender, Farm, Shed, and Partition, falling back to
  normalized fields for those same labels; they never emit full raw_payload,
  raw row JSON, raw RFID, or raw old-tag values
  grouped CSV cells are spreadsheet-formula safe; duplicate old-tag groups use
  stable non-reversible old-tag/scope refs instead of raw values or short masks
  species_or_breed_requires_review groups are for human alias-vs-exclusion
  decisions, not auto-aliasing; blank_old_tag_suffix groups support the
  opt-in RFID-only creation policy and do not imply suffix derivation
  docs/phases/phase-01-goat-passport/rfid-data-mapping-review.md captures the
  local post-apply mapping review. Migration 000010 implements only the
  approved plain F2/K2 status mappings. The follow-up local rerun cleared
  unknown_status_mapping from 437 to 0, raised created_goat from 145 to 383,
  left error at 0, and redistributed 199 rows to the downstream
  species_or_breed_requires_review bucket. Migration 000011 implements Sirohi
  and the approved goat-cross labels as active goat breed/alias mappings using
  the Phase 1 crossbreed-as-breed-row simplification. The follow-up rerun raised
  created_goat from 383 to 436, left error at 0, and reduced
  species_or_breed_requires_review from 397 to 344. At that point the remaining
  breed/species bucket was Anantapur Sheep only and had to stay out of goat
  creation through breed/species semantics. After the `000012` blank-suffix
  opt-in run, the confirmed non-goat disposition scope is 504 breed/species
  review rows because 160 blank-suffix rows also hit the same gate. Confirmed
  non-goat rows should not stay forever in an actionable review queue; a later
  review-ops/apply-semantics slice should decide terminal rejected disposition
  versus earlier source classification.
  blank_old_tag_suffix policy review recommended guarded RFID-only creation
  without old_tag identifier creation. Migration 000012 adds the supporting
  apply-candidate index and `rfid-apply --allow-rfid-only-blank-suffix`
  implements it behind an explicit flag. Maximum possible additional goats is
  435. The implementation dry-run and real local rehearsal both produced 711
  created goats total, 275 above the post-000011 baseline of 436, with
  needs_review 512 and error 0. Remaining open-review reason occurrences are
  species_or_breed_requires_review 504, blank_old_tag_suffix 160,
  blank_gender 4, and duplicate_old_tag_same_scope 4; reason counts are
  occurrences because the 160 remaining blank-suffix rows also fail the
  breed/species gate.
  C-lite suffix derivation from Farm/Shed/Partition is rejected because
  Partition and Shed are not one-to-one with suffix context and a wrong derived
  old_tag scope is worse than unresolved evidence. blank_gender plus duplicate
  same-scope old_tag rows remain blocked pending source correction or explicit
  reviewed policy.
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

## Auth And RBAC Scope

```text
X-GoatOS-Tenant-ID and X-GoatOS-Actor-ID are no longer production API
authority. Bearer mode derives tenant and actor from the signed token and
overwrites these headers before handlers run.

The headers remain only for explicit local dev_headers mode and direct handler
unit tests below the auth middleware.

Phase 1 RBAC role boundary is pinned:

```text
internal goat-ops roles:
  admin
  verifier
  park_head
  operator
  ceo_internal

Phase 1 permissions:
  goat.read
  correction.create
  goat.view_dirty_data
  goat.review_identity
  goat.write_identity
  analytics.identity.read
  import.run.manage
  import.run.view
```

Role and permissions must come from the active `user_scope_grants` row for the
token `sub`; token role claims are not authority. Analytics count reads must use
the token tenant as the DB tenant filter, with query `tenant_id` only as an
optional equality assertion.
When a user has multiple active tenant grants, permissions are the union of
those active grant roles. A user with operator and verifier grants can use
verifier-only review routes; admin-only routes still require admin.

The auth/RBAC implementation must use an explicit route-to-permission registry
with a fail-closed default. Only `/healthz` and `/readyz` are unauthenticated.
Identity/admin/app handlers and reporting/analytics handlers must share that
registry; `analytics.identity.read` must not be enforced from a divergent
analytics-only permission table.
`adminListCorrectionRequests` must move to
`GET /admin/identity/correction-requests` before route permissions are enforced;
`GET /identity/correction-requests` remains the app own/visible correction list.

`import.run.view` on `verifier` is deliberate so identity reviewers can inspect
import provenance while triaging dirty identity data. It does not grant import
run creation or canonical import apply.

HTTP auth/RBAC does not cover local/system CLIs (`rfid-import`, `rfid-apply`,
`outbox-relay`, `rebuild-identity-counters`, `update-identity-counters`). Those
entrypoints remain operator-trusted, but must still take explicit tenant input
and keep database writes tenant-scoped.

Actor attribution comes from the token `sub` in bearer mode. Write audit,
decision, correction, and idempotency actor fields use the authenticated actor
from request context; old actor headers cannot override a bearer token.

Tenant-scope RBAC is broader than the final intended scope model. Tenant-wide
admin/verifier grants can act across the tenant until custodian/farm/park/shed
and cohort filters are implemented. Treat that as a known Phase 1 limitation,
not the final RBAC shape.

Investor/reduced dashboards are not Phase 1 goat-ops roles. Investor, buyer,
donor, partner, franchise, lending, external customer, procurement, health,
workforce, and device-specific roles belong to later realms/phases unless a
future migration explicitly extends the contracts and DB role checks.

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
production IdP/JWKS/asymmetric auth, token issuance, rotation, revocation,
refresh tokens, Secret Manager wiring, rate limiting, clock skew policy, TLS
termination, and leaked-secret runbook
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
approved RFID mapping/policy build from the local data mapping review:
source cleanup or reviewed policy for blank_gender plus duplicate same-scope
old_tag rows; optional earlier source classification for Anantapur Sheep while
keeping it out of goat creation;
terminal rejected disposition for the 504 current confirmed non-goat
breed/species review rows so actionable review queues do not carry permanent
non-goat noise
P8 sales/allocation/promise behavior
```

## Near-Term Phase 1 Closure Order

This order is intentional and should not be inferred from conversation memory:

```text
1. Build the local read-only admin demo UI before mutating non-goat review state.
   The UI should show clean goats, passport detail, review buckets, reviewer CSV
   export locations, identity counts, and a separate confirmed non-goat bucket.
   Admin-web is now on the frozen Next 16 / React 19 / Tailwind 4 framework
   baseline from the frontend architecture doc, so the next UI slice should wire
   dev auth and @goatos/api-client before adding real data views, use SSR-first
   route modules, lazy-load heavy tables/charts, and keep feature modules
   standalone-capable inside the admin surface. The same slice should add a
   boundary guard so feature modules cannot deep-import one another's internals.

2. Add a local demo runner/runbook that reproduces the real Shape-2 rehearsal:
   import, apply with the explicit RFID-only blank-suffix flag, rebuild counters,
   generate reviewer CSVs, start backend/admin-web, and print final counts/URLs.

3. Do Phase 1 closeout docs after the demo path is reproducible.

4. Treat terminal non-goat disposition as a separate canonical-state slice unless
   it becomes required before closeout. The demo UI may label the current 504
   confirmed non-goat breed/species review rows without changing their state.
```

The terminal non-goat slice is not mechanical. It must choose and document the
mechanism first: terminal `rejected` at apply/review time versus earlier
source-discovery or staging exclusion. It must remain auditable, reversible by a
future correction path, and must not silently drop source rows.

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

Bearer auth/RBAC is now the API default. The remaining deploy gate is
production-grade authentication infrastructure: IdP/JWKS, secret management,
token lifecycle, rate limiting, TLS, and operations runbooks.

Admin-web now has the Phase 1 readiness foundation: generated client plumbing,
local dev token/grant helpers, local auth smoke, and a buildable disabled shell.
Full data-bound frontend screens remain deferred.

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
make api-client-check
PATH="/path/to/pinned/sqlc/bin:$PATH" make sqlc-check
make validate-sqlc-plans
make validate-migrations
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run build
backend/tests/integration/smoke-auth-local.sh
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
