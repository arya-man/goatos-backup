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
```

Known backend deferments:

```text
auth/RBAC adapter
legacy import runner
merge/unmerge and conflict/candidate resolve command handlers
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
