# Backend Implementation Reference

Load this when extending the Go backend, reviewing backend code, or starting a
Phase 1 backend continuation session.

Canonical docs:

- `docs/decisions/go-backend-stack.md`
- `backend/AGENTS.md`
- `docs/phases/phase-01-goat-passport/BUILD-STATUS.md`
- `docs/phases/phase-01-goat-passport/PRD.md`
- `docs/phases/phase-01-goat-passport/TRD.md`

Current backend shape:

```text
backend/cmd/api                 process entrypoint and graceful shutdown
backend/internal/bootstrap      explicit constructor wiring
backend/internal/platform       shared platform adapters
backend/internal/identity       Phase 1 Goat Passport module
```

Identity module layout:

```text
identity/domain                 contract-shaped domain DTOs and value types
identity/app                    use cases and state-machine logic
identity/ports                  interfaces owned by the identity module
identity/adapters/http          thin net/http handlers
identity/adapters/postgres      repository adapter behind ports.Repository
identity/adapters/postgres/sqlc generated query package for static reads
```

Rules:

- Handlers stay thin: parse request, call app service, write contract-shaped
  response/error envelope.
- App services own identity behavior: merge redirect, identifier resolution
  state-machine, tenant-scope validation, and error mapping.
- Domain/app/ports must not import HTTP or pgx.
- Postgres adapters must satisfy module-owned ports and keep SQL tenant-scoped.
- Do not scan the full herd in API paths. Use indexed lookup paths and bounded
  `limit` values.
- `X-GoatOS-Tenant-ID` is a local/dev placeholder until auth/RBAC lands. It is
  not production authentication.
- Analytics `tenant_id` query scope must not conflict with the temporary tenant
  header while that header exists.
- Request middleware preserves `X-Request-ID` and `traceparent`, generates a
  request ID when missing, and logs method/path/status/duration with slog.
- Full OpenTelemetry exporters/spans/metrics are deferred, but the request
  context/logging shape must remain OTel-friendly.
- The Postgres repository uses generated sqlc for stable static reads and
  handwritten pgx for dynamic optional-filter reads. Do not spread new
  hand-written SQL into write-heavy/import/reconciliation paths without either
  generating it or documenting why the shape must remain dynamic.
- Write commands use repository-owned Postgres transactions behind the module
  port. The app service validates command semantics and idempotency identity;
  the Postgres adapter owns SQL and commits idempotency row, domain write,
  audit row, and outbox row together.
- POST `/identity/correction-requests` is the first write pattern:
  `Idempotency-Key` is required, the stored key is
  `<tenant_id>:createCorrectionRequest:<client_key>`, request_hash includes
  canonical JSON body + command + route + tenant, exact replay re-fetches the
  correction request, and same key/different body is a conflict.
- Temporary `X-GoatOS-Actor-ID` is required for write scaffolding until
  auth/RBAC lands. It must parse as uuid and is local/dev only.
- Correction request create supports goat-linked and goatless requests. It does
  not create goat_identity_events because it is a review input, not canonical
  goat identity mutation. It writes a `correction_request` outbox event envelope
  in the same transaction.
- POST `/admin/identity/correction-requests/{correction_request_id}/resolve`
  is the first admin write pattern. It requires `Idempotency-Key`,
  temporary `X-GoatOS-Actor-ID`, typed `evidence_refs`, `reason`, and
  `row_version`. The stored key is
  `<tenant_id>:resolveCorrectionRequest:<client_key>`, and request_hash includes
  tenant, command, route, correction_request_id, and canonical JSON body.
- Correction resolve is optimistic-concurrency guarded:
  `state in (open, assigned, needs_field_check)`, matching row_version, and
  `state <> target`. Approved/rejected/closed are terminal except exact
  idempotent replay.
- Correction resolve writes `identity_decisions` with
  `decision_type=resolve_correction_request` and
  `policy_version=phase1-manual-correction-review-v1`. `decision_state` is the
  lifecycle of the decision record; `decision_result` is the business outcome.
  Do not use old assignment/status framing for `decision_state`.
- Correction resolve maps targets as:
  approved -> decision_state approved/result approved; rejected -> rejected;
  needs_field_check -> needs_review/result needs_field_check; closed ->
  approved/result closed. Terminal targets set `resolved_at`; needs_field_check
  leaves `resolved_at` null.
- Correction resolve persists correction update, decision, audit, outbox, and
  idempotency completion in one transaction. It does not directly mutate goat
  identity and must not write `goat_identity_events`.
- POST `/admin/goats/{goat_id}/identifiers` and
  `/admin/goats/{goat_id}/identifiers/{identifier_id}/retire` are the first
  canonical goat identity mutation commands. They require `Idempotency-Key`,
  temporary `X-GoatOS-Actor-ID`, typed `evidence_refs`, and goat `row_version`.
  Add also requires `scope_key`; RFID values are trim + uppercase normalized,
  while other identifier values are trimmed.
- Identifier add/retire uses one transaction for idempotency row, goat
  row_version guard, identifier mutation, `identity_decisions`,
  `identity_decision_identifiers`, `goat_identity_events`,
  `identity_decision_events`, `audit_log`, `outbox_messages`, and idempotency
  completion. The outbox aggregate is `goat`; the subject is `identifier`.
  Replay re-fetches the DB state from idempotency result metadata and decision
  joins, never cached response bodies.
- Identifier add/retire must not use merge override GUCs. Merged goats, stale
  row_version, wrong-tenant/wrong-goat identifiers, already-retired
  identifiers, duplicate active RFID, duplicate same-scope old_tag, identifier
  policy `primary_allowed=false`, and active primary-per-goat conflicts all
  reject as not_found or write_conflict according to route visibility.
- `make sqlc-check` regenerates the migration-derived schema dump and generated
  sqlc code with the pinned `tools/sqlc/sqlc.version`; it fails on drift.
- `make validate-sqlc-plans` extracts every generated static read from
  `sqlc/query.sql`, runs EXPLAIN checks, and rejects sequential scans on the
  hot lookup tables. New generated queries must be registered in that plan
  validator or the check fails.

Phase 1 read behaviors already built:

```text
GET /goats/{goat_id}
  accepts goat_id or display_id
  follows merged_into_goat_id chains to the live survivor
  detects redirect cycles and max-depth failures
  never returns a merged goat as a normal live passport

GET /identifiers/{type}/{value}/resolve
  single_match
  multiple_matches
  no_match
  needs_review for retired/disputed-only matches
  merged_redirect for identifiers attached to merged goats
  same-scope multiple matches may attach a conflict_id
  cross-scope multiple matches must not attach the wrong conflict_id

GET /goats/search
  requires bounded limit
  excludes merged goats from normal list results
  tenant-scoped

GET /admin/identity/conflicts
GET /admin/identity/conflicts/{conflict_id}
GET /analytics/identity/counts
  structural read paths only; counters are populated by later projection/import work

POST /identity/correction-requests
  strict contract-shaped JSON body validation with unknown-field rejection
  goat-linked tenant ownership validation
  goatless correction requests allowed
  state=open
  same-transaction idempotency + correction row + audit_log + outbox_messages
  correction_request is the outbox aggregate; goat is subject only when goat_id exists

POST /admin/identity/correction-requests/{correction_request_id}/resolve
  strict contract-shaped JSON body validation with unknown-field rejection
  old evidence_ids payloads rejected; use typed evidence_refs
  open/assigned/needs_field_check -> approved/rejected/closed or needs_field_check
  exact idempotent replay returns original correction request plus decision
  same key/different body, same-state no-op, stale row_version, and terminal
  re-resolve all conflict
  same-transaction idempotency + identity_decisions + correction update +
  audit_log + outbox_messages
  correction_request is aggregate and subject; no goat_identity_events are
  written because this is review state, not canonical goat identity mutation

POST /admin/goats/{goat_id}/identifiers
POST /admin/goats/{goat_id}/identifiers/{identifier_id}/retire
  strict contract-shaped JSON body validation with unknown-field rejection
  old evidence_ids payloads rejected; use typed evidence_refs
  tenant/route/subject namespaced idempotency keys
  add writes decision_type=attach_identifier, result=identifier_attached,
  policy_version=phase1-identifier-v1, decision identifier action=attach,
  goat_identity_events event_type=goat.identifier.added
  retire writes decision_type=retire_identifier, result=identifier_retired,
  policy_version=phase1-identifier-v1, decision identifier action=retire,
  goat_identity_events event_type=goat.identifier.retired
  exact idempotent replay returns the original decision/event plus current goat
  identifier state
  same key/different body conflicts
  goat identity event outbox rows are DB-validated against same-tenant
  goat_identity_events

POST /admin/identity/conflicts/{conflict_id}/resolve
  implements decision_type/result pairs:
  merge_goats + same_goat_merge
  reject_match + candidate_rejected
  request_field_verification + field_verification_required
  mark_identifier_disputed + different_goats_identifier_disputed
  create_goat + new_goat_required remains typed not_implemented until the
  contract defines required goat creation fields
  strict JSON rejects old evidence_ids; use typed evidence_refs
  requires Idempotency-Key, temporary X-GoatOS-Actor-ID, affected goat IDs,
  identifier actions array, reason, and conflict row_version
  survivor goat is required only for merge_goats and rejected for non-merge
  decisions
  stored idempotency key is
  <tenant_id>:resolveIdentityConflict:<conflict_id>:<client_key>
  inserts identity_decisions before the guarded conflict update so
  identity_conflicts.decision_id satisfies its FK, then gates the mutation with
  one conditional update on tenant, conflict, target state,
  open/needs_field_check state, and row_version
  reject_match is terminal: conflict state rejected, resolved_at/resolved_by
  set, decision + audit only, no goat_identity_events/outbox
  request_field_verification is nonterminal: conflict state needs_field_check,
  resolved_at/resolved_by null, decision + audit only, no
  goat_identity_events/outbox; same-state field-check with a new idempotency key
  is a write conflict
  mark_identifier_disputed is a goat identity mutation: it requires explicit
  identifier_actions with action=dispute and identifier_id; identifiers must be
  same-tenant, active, attached to conflict member goats, and match conflict
  identifier type/value when those fields are present; selected identifiers are
  set to status=disputed and primary=false, with one goat row_version bump per
  affected goat and one goat.identifier.disputed event/outbox per identifier
  merge is allowed only for duplicate-identity conflict types:
  possible_duplicate_goat, duplicate_active_identifier, rfid_already_linked,
  old_tag_reused
  survivor and affected goats must belong to the conflict
  merge override GUCs are SET LOCAL inside the transaction only
  goats.merged_into_goat_id is the authoritative live-survivor pointer;
  goat_merge_links is immutable history
  decision records use the resolved live survivor and newly merged goat IDs,
  with requested affected IDs preserved for traceability
  loser identifiers are default-retired unless explicitly transferred; no
  active identifier remains attached to a newly merged goat
  survivor goat row_version is bumped when a loser identifier is transferred to
  the survivor
  row-version invariant: every implemented write that changes a goat identity
  surface must bump that goat row_version exactly once per transaction; future
  candidate approve/reject, correction auto-apply, unmerge, and create_goat
  slices must apply the same rule when they mutate goat identity
  writes identity_decision_goats roles survivor and merged, applied
  identity_decision_identifiers actions, goat_merge_links,
  goat_identity_events, identity_decision_events, audit_log, outbox_messages,
  and idempotency completion in one transaction
  decision-only reject_match/request_field_verification changes are not
  event-stream visible until a conflict-aggregate event/projection contract is
  defined; projections must read canonical conflict state
  exact replay rebuilds from DB state, not cached response bodies

GET /admin/identity/candidates
  actionable queue only: proposed and needs_review candidates; approved,
  rejected, and expired candidates are excluded by default
  requires bounded limit and uses keyset pagination by created_at desc,
  candidate_id desc; cursor carries both fields
  CandidateSummary includes row_version because reject needs optimistic
  concurrency and there is no candidate-detail endpoint

POST /admin/identity/candidates/{candidate_id}/reject
  strict JSON rejects old evidence_ids; use typed evidence_refs
  requires Idempotency-Key, temporary X-GoatOS-Actor-ID, reason,
  evidence_refs, and candidate row_version
  stored idempotency key is
  <tenant_id>:rejectIdentityCandidate:<candidate_id>:<client_key>
  inserts identity_decisions before the guarded candidate update, then gates the
  mutation with one conditional update on tenant, candidate,
  state in proposed/needs_review, and row_version
  maps to decision_type=reject_match, decision_result=candidate_rejected,
  decision_state=rejected, policy_version=phase1-manual-correction-review-v1
  updates identity_match_candidates to rejected, sets reviewed_by/reviewed_at
  and decision_id, increments candidate row_version, writes audit_log and
  idempotency completion in one transaction
  does not mutate goat identity, bump goat row_version, write
  goat_identity_events, or write outbox_messages
  exact replay rebuilds from DB state, not cached response bodies

POST /admin/identity/candidates/{candidate_id}/approve
  remains typed not_implemented until canonical merge/create/identifier
  mutation semantics are contract-defined; do not mark candidates approved as a
  decision-only shortcut
```

Known backend deferments:

```text
auth/RBAC adapter
legacy import runner
create_goat conflict decision fields
candidate approve canonical mutation semantics
standalone merge command handler
unmerge command handler/contract
projection workers and counter population
outbox relay/runtime workers
OpenTelemetry exporters/spans/metrics
P8 sales/allocation behavior
```

Before extending this backend:

```text
read BUILD-STATUS.md for current Phase 1 state
run make test
run make check
run make sqlc-check for DB query changes
run make validate-sqlc-plans when adding/changing indexed identity read queries
run make validate-migrations for schema-sensitive work
keep fixtures synthetic
```
