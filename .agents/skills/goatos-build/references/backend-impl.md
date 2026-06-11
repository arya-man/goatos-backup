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
backend/cmd/outbox-relay        Phase 1 local/dev outbox relay one-shot CLI
backend/cmd/rebuild-identity-counters Phase 1 reporting counter rebuild CLI
backend/cmd/update-identity-counters  Phase 1 local incremental counter update CLI
backend/cmd/rfid-import         Phase 1 RFID workbook staging CLI
backend/cmd/rfid-apply          Phase 1 RFID staged-row canonical apply CLI
backend/internal/bootstrap      explicit constructor wiring
backend/internal/platform       shared platform adapters
backend/internal/platform/localtarget local/dev database target guard for DB-writing rehearsal CLIs
backend/internal/identity       Phase 1 Goat Passport module
backend/internal/legacy_import  Phase 1 import staging and reconciliation inputs
backend/internal/outbox         Phase 1 local/dev outbox relay foundation
backend/internal/reporting      Phase 1 analytics count reads/rebuild/incremental updates
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

Legacy import module layout:

```text
legacy_import                   parser, normalization, key/hash, runner orchestration
legacy_import/adapters/postgres repository for staging and RFID apply SQL
legacy_import/adapters/postgres/sqlc generated import policy/staging/apply SQL
```

Local import rehearsal routing:

```text
docs/runbooks/local-full-stack-rehearsal.md  local Phase 1 import/API/admin-web rehearsal flow once added
docs/runbooks/local-docker-storage.md        local Docker storage safety and cleanup rules
backend/cmd/rfid-import                      RFID workbook staging CLI
backend/cmd/rfid-apply                       staged RFID canonical apply CLI
backend/cmd/rebuild-identity-counters        local/reporting counter rebuild after apply
backend/internal/legacy_import/xlsx.go       workbook parser and sheet-selection behavior
backend/internal/legacy_import/normalize.go  emitted staging/review/error reason codes
```

Outbox module layout:

```text
outbox/domain                  relay DTOs and outbox status constants
outbox/app                     claim/publish/retry/dead-letter workflow
outbox/ports                   repository and publisher interfaces
outbox/adapters/postgres       outbox_messages claim/update adapter
outbox/adapters/publisher/logging local no-op publisher with safe metadata logs
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
- Apply the scale lens before approving any backend change: large-table queries
  must be tenant/run scoped, index-backed, chunked or paginated, bounded in
  memory/goroutines, idempotent for retries, and covered by plan validation when
  the query can touch import, goat, event, counter, or outbox rows at scale.
- API bootstrap defaults to bearer auth. `X-GoatOS-Tenant-ID` and
  `X-GoatOS-Actor-ID` are local/dev placeholders only and are overwritten by
  bearer auth before handlers run.
- Phase 1 RBAC is the internal goat-ops realm only. The DB-enforced role set is
  `admin`, `verifier`, `park_head`, `operator`, and `ceo_internal`. Investor,
  buyer, donor, partner, franchise, lending, procurement, health, workforce, and
  device-specific roles are later realm/phase work unless the schema and
  contracts are explicitly extended.
- Phase 1 permissions are `goat.read`, `correction.create`,
  `goat.view_dirty_data`, `goat.review_identity`, `goat.write_identity`, and
  `analytics.identity.read`, plus `import.run.manage` and `import.run.view`.
  These are enforced from signed bearer identity plus active tenant-scope
  `user_scope_grants`. `createImportRun` is product-admin-only via
  `import.run.manage`; import run reads use `import.run.view`. HS256 is a
  bootstrap verifier only; production IdP/JWKS/asymmetric verification remains
  deferred.
- GET `/admin/import-runs/{import_run_id}` and
  `/admin/import-runs/{import_run_id}/rows` are live read-only Phase 1 Import
  Review APIs. They are tenant/run scoped, keyset-paginated for rows, enforce a
  max row limit of 500, support `processing_state` and `reason_code` filters,
  expose only whitelisted normalized review fields, and return nullable
  "not tracked" summary metrics instead of fake zeroes. `POST /admin/import-runs`
  and all review/fix/import write actions remain deferred.
- Import Review query-plan validation now checks both the real generated
  `ListImportRunRows` shape with a non-null `reason_code` and a separate JSONB
  GIN usability probe. Phase 1 accepts the current ordered keyset plan with a
  residual reason predicate for local review queues; before staging/1M sparse
  reason-filter use, add a reason-keyset/materialized reason strategy or prove
  the residual scan stays bounded. The current no-Sort plan guard is part of
  the local ordered-keyset proof and should be revised if the later sparse-reason
  plan legitimately uses a GIN bitmap scan plus sort or another reason-keyset
  shape.
- `ceo_internal` is the full Goat OS product-admin role for Phase 1 internal
  product actions, equivalent to `admin` for product/API permissions. This does
  not grant Google Cloud, IAM, billing, GitHub, or repository administration.
- Verifier access to `import.run.view` is deliberate: identity reviewers need
  import provenance while triaging dirty data. It does not grant import run
  creation or canonical import apply.
- The bootstrap HS256 verifier uses Go standard-library primitives
  (`crypto/hmac`, `crypto/sha256`, JSON, and base64url parsing). Bearer-mode
  startup fails if issuer/audience are missing or the HS256 secret is
  missing/weak. Tokens whose `exp` is farther than the configured max TTL are
  rejected; the default max TTL is 24h. This is a bootstrap blast-radius cap,
  not production revocation. The dev-header escape hatch requires explicit
  local-only opt-in and refuses staging/production-looking configuration.
- Role and permissions must come from the active `user_scope_grants` row for
  the token `sub`; token role claims are not authority. Bearer-mode write actor
  attribution must use token `sub`, not `X-GoatOS-Actor-ID`.
- Multiple active tenant grants are unioned for authorization. If any active
  matching tenant grant role confers the required permission, the request is
  authorized; product-admin-only routes require an active `admin` or
  `ceo_internal` role.
- The auth middleware is backed by an explicit route-to-permission registry with
  fail-closed default. Only `/healthz` and `/readyz` are unauthenticated. The
  correction-list contract paths are split: app `listCorrectionRequests` stays
  `GET /identity/correction-requests`, while admin `adminListCorrectionRequests`
  is `GET /admin/identity/correction-requests`.
- Identity/admin/app handlers and reporting/analytics handlers must consume the
  same permission registry package. Do not maintain a separate analytics-only
  table for `analytics.identity.read`.
- Analytics count reads use token tenant as the DB tenant filter. Query
  `tenant_id` is only an optional equality assertion and must not become
  repository authority.
- HTTP auth/RBAC covers network API requests. Local/system CLIs (`rfid-import`,
  `rfid-apply`, `outbox-relay`, `rebuild-identity-counters`, and
  `update-identity-counters`) remain operator-trusted entrypoints outside HTTP
  auth, but DB-writing local rehearsal commands must use
  `backend/internal/platform/localtarget`, require explicit tenant input, and
  keep SQL tenant-scoped. The local target guard allows only `GOATOS_ENV=local`
  or `dev` for import/apply/counter rehearsal commands and rejects
  production/staging-looking database URLs, remote DB hosts, and Cloud SQL Unix
  sockets.
- Local Docker is the default daily development path for Docker Postgres, tests,
  and small synthetic data; GCP is not required for normal coding. `goatos-dev`
  Cloud SQL later serves explicit cloud rehearsal, while `goatos-stg` later
  holds the persistent 1M benchmark dataset. Local 1M tests must be temporary
  volume runs only. Permanent local DB volume name is `goatos_dev_pg_data`; temp
  test/benchmark volumes use `goatos_tmp_`, `goatos_test_`, or
  `goatos_bench_tmp_`. Cleanup tooling must skip unclassified volumes, never
  run blind `docker volume prune`, and must not silently prune machine-wide
  Docker build cache/images. On Docker Desktop for Mac, deleting Docker
  resources may not immediately shrink the host-side Docker.raw/VM disk image.
  Future Goat OS cloud defaults are `asia-south1` Mumbai, never US by default.
- Tenant-scope RBAC is broader than final intended scope. Tenant-wide
  admin/verifier grants can act across all goats in the tenant until
  custodian/farm/park/shed/cohort filtering lands.
- Analytics `tenant_id` query scope must not conflict with the authenticated
  token tenant; the query value is only an optional equality assertion.
- Request middleware preserves `X-Request-ID` and `traceparent`, generates a
  request ID when missing, and logs method/path/status/duration with slog.
- Full OpenTelemetry exporters/spans/metrics are deferred, but the request
  context/logging shape must remain OTel-friendly.
- The Postgres repository uses generated sqlc for stable static reads and
  handwritten pgx for dynamic optional-filter reads. Do not spread new
  hand-written SQL into write-heavy/import/reconciliation paths without either
  generating it or documenting why the shape must remain dynamic.
- Use both concurrency patterns deliberately. User/admin commands that edit a
  specific identity aggregate use `row_version` optimistic guards or the command
  aggregate's `row_version`. Writes that change a goat identity surface bump
  that goat `row_version` exactly once per transaction for stale-write rejection
  and future projection/cache invalidation. Counters, import progress, retry
  attempts, and projections use atomic SQL increments or rebuilds, not
  row-version compare-and-retry loops.
- Write commands use repository-owned Postgres transactions behind the module
  port. The app service validates command semantics and idempotency identity;
  the Postgres adapter owns SQL and commits idempotency row, domain write,
  audit row, and outbox row together.
- POST `/identity/correction-requests` is the first write pattern:
  `Idempotency-Key` is required, the stored key is
  `<tenant_id>:createCorrectionRequest:<client_key>`, request_hash includes
  canonical JSON body + command + route + tenant, exact replay re-fetches the
  correction request, and same key/different body is a conflict.
- Bearer mode supplies the write actor from token `sub` in request context.
  `X-GoatOS-Actor-ID` remains a local/dev handler-test scaffold only and cannot
  override bearer context.
- Correction request create supports goat-linked and goatless requests. It does
  not create goat_identity_events because it is a review input, not canonical
  goat identity mutation. It writes a `correction_request` outbox event envelope
  in the same transaction.
- POST `/admin/identity/correction-requests/{correction_request_id}/resolve`
  is the first admin write pattern. It requires `Idempotency-Key`,
  authenticated actor, typed `evidence_refs`, `reason`, and
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
  authenticated actor, typed `evidence_refs`, and goat `row_version`.
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
  validator or the check fails. If a generated query widens a hot path, add the
  supporting migration/index in the same slice and prove the new branch uses it.
- `backend/internal/legacy_import` owns import staging and reconciliation
  inputs. Do not route staging writes through identity adapters, and do not let
  `cmd` write `legacy_import_*` tables directly.
- The RFID import CLI supports source discovery, sheet-by-name selection,
  dry-run, real staging, and sanitized anomaly reports. It loads approved policy
  `phase1-rfid-db-import-v1`; source system/dataset, source key recipe/version,
  hash recipe/version, identifier policy version, and normalizer version come
  from that DB policy row.
- RFID workbook imports parse `.xlsx` cells as text/raw values. Never coerce
  RFID or old-tag-like fields through float conversion.
- `ParseXLSXSheet` resolves explicit sheet names through `xl/workbook.xml` and
  `xl/_rels/workbook.xml.rels`; explicit missing sheets fail clearly. Do not
  assume visible tab order equals `sheetN.xml` file order.
- Dry-run imports write only `legacy_import_runs` aggregate counts. Real staging
  writes `legacy_import_rows` in bounded batches and does not mutate canonical
  goats, identifiers, events, outbox, counters, candidates, conflicts, or
  corrections.
- Source row keys follow the policy recipe and never include tenant prefixes or
  spreadsheet row numbers. Row version hashes use deterministic fixed-order
  serialization from the policy hash recipe.
- `backend/cmd/rfid-apply` applies only completed non-dry-run RFID staging
  runs. By default it processes bounded batches of `legacy_import_rows` where
  `processing_state='pending'`, uses the approved import policy and Mesha org
  party from DB, and leaves needs_review/error/rejected/created_goat/auto_linked
  rows untouched. The explicit flag `--allow-rfid-only-blank-suffix` widens the
  scan to rows containing `blank_old_tag_suffix`, then the Go transaction
  re-locks the row and only applies it if the complete reason set is exactly
  `blank_old_tag_suffix`.
- RFID apply creates canonical goats only for safe clean rows. One row
  transaction writes the import-policy `create_goat` decision, DB-generated
  goat, active primary RFID, optional active primary old_tag in
  `park:<normalized_park_code>`, 10000 bps Mesha ownership, initial Mesha
  custody history with `reason=first_rfid_import`, `goat.created` event,
  decision join rows, audit row, outbox row, idempotency completion, and
  legacy row/run state.
- When `--allow-rfid-only-blank-suffix` is enabled, rows whose only staged
  review reason is `blank_old_tag_suffix` may create a goat plus primary RFID
  with no `old_tag` identifier. The importer must not derive old-tag scope from
  Farm, Shed, or Partition. Unresolved old-tag evidence remains in raw/normalized
  import payloads and the decision/audit evidence trail; created rows no longer
  appear in open anomaly/reviewer reports.
- RFID apply routes unsafe rows to `needs_review` without goat creation:
  duplicate active RFID, duplicate active old_tag in the same scope, unknown or
  review-required status mapping, unsafe/non-goat breed or species labels, and
  changed source row hashes. It does not create dirty conflicts/candidates,
  goat_location_history, counters, projections, or an outbox relay.
- Deterministic per-row SQL data/integrity failures (SQLSTATE class 22/23)
  roll back that row's canonical write transaction, then a separate short
  transaction marks only that staged row `processing_state='error'` with
  sanitized SQLSTATE/constraint metadata and refreshes the run `error_count`.
  Later pending rows continue. Transient, concurrency, infrastructure, context,
  or unknown failures abort the run so good rows are not quarantined. Error rows
  are not auto-retried; an operator must promote them back to `pending` after
  fixing the root cause.
- RFID apply idempotency derives from tenant, command, source_system,
  source_dataset, source_row_key, and source_row_version_hash. It intentionally
  excludes import_run_id, and exact replay must not duplicate goat, identifier,
  ownership, custody, decision, event, audit, or outbox rows.
- Future RFID apply ops hardening is tracked but not built: optional latest
  apply-attempt visibility fields (`apply_started_at`, `apply_completed_at`,
  `apply_status`, sanitized `apply_error_reason`), a separate `apply_attempts`
  table only if full history is needed, DB delete/update guards for permanent
  seed/reference rows such as Mesha party/org, active goat breed seeds,
  identifier policies, and import policies, bounded retry only if real
  40001/40P01 appears or apply becomes parallel, and deterministic non-SQL
  row-local isolation only after such faults are proven reachable and safely
  sanitizable.
- Import fixtures committed to git must stay synthetic `.xlsx` files only. Do
  not commit raw private workbook rows, RFID values, local paths, screenshots,
  names, media URLs, or PII.
- Phase 1 local full-stack rehearsal routes source data through backend import
  tooling into local Postgres first. Frontend must read Goat OS backend APIs
  only; it must never read Sheets, Apps Script, XLSX files, CSV exports, or
  Google SDKs directly.
- Current RFID workbook parser/normalizer accepts the Shape-2 source headers:
  `Farm`, `Old ID`, `Old ID Suffix`, `RFID`, `Age`, `Gender`, `Breed`, `Tag`,
  `Shed`, and `Partition`. Shape-1 RFID headers such as `Origin Farm`,
  `Old Tag ID`, and `Shed Tag` are recognized source evidence but require a
  separate mapping extension before import.
- Source discovery classifies Shape 2 as importable, recognizes Shape 1 as
  blocked until mapping extension, and rejects operational/unknown source
  shapes before staging. Operational counting/feed/health/death/shifting/
  dashboard Sheets must not be fed into the Phase 1 RFID identity importer.
- Google Sheet live export/import is not implemented in Phase 1. Operators must
  export to local XLSX and use `rfid-import --source-type=local_xlsx`; the
  `google_sheet` discovery path returns a skipped status.
- Anomaly/review reports must be generated after `rfid-apply` for the final
  local rehearsal handoff so they include both staging reasons and apply-stage
  review reasons such as `unknown_status_mapping` and
  `species_or_breed_requires_review`. They must use actual emitted reason codes
  from `legacy_import` data, such as `malformed_rfid`,
  `duplicate_rfid_in_workbook`, `missing_rfid`,
  `duplicate_old_tag_same_scope`, `blank_old_tag_suffix`,
  `tagless_identity_evidence`, `blank_gender`, `unknown_gender`, and
  `source_row_changed`. Do not invent report buckets or imply validations that
  are not implemented. Reports belong under ignored local output paths, must
  mask sensitive identifiers by default, and must not emit raw source_row_key
  values because source keys can include RFID/old-tag evidence.
- `rfid-import --anomaly-report` keeps the existing detail, reason summary, and
  grouped summary CSVs at the report root, and also emits reviewer-friendly CSVs
  under a per-run `reviewer-<import_run_id>/` subdirectory:
  `review-summary.csv`, `needs-review-rows.csv`,
  `non-goat-exclusion-candidates.csv`, `blank-old-tag-suffix.csv`,
  `blank-gender.csv`, `duplicate-old-tag-same-scope.csv`, and `README.txt`.
  These files are export-only; `reviewer_action` and `reviewer_notes` are
  scratch columns and are not ingested by Goat OS. Corrections must go back
  through the source workbook, or a future approved correction overlay, followed
  by the normal Shape-2 import/apply flow.
- Anomaly grouped summaries may read `legacy_import_rows.raw_payload` only
  through the safe source-label whitelist `Tag`, `Breed`, `Gender`, `Farm`,
  `Shed`, and `Partition`, falling back to normalized fields for those same
  labels when raw data is absent. They must never serialize full raw_payload,
  raw row JSON, raw RFID, or raw old-tag values. Grouped CSV cells must be
  spreadsheet-formula safe because humans open these files in spreadsheet
  tools. Duplicate old-tag groups use stable non-reversible old-tag/scope refs
  rather than raw values or short masks. Breed review groups are a human gate
  for alias-vs-exclusion decisions, not auto-aliasing. Blank old-tag suffix
  groups support the explicit RFID-only apply flag and do not imply suffix
  derivation.
- While `species_or_breed_requires_review` is confirmed to be Anantapur Sheep
  only, those rows belong in `non-goat-exclusion-candidates.csv`; do not emit a
  duplicate breed/species issue file with the same sheep rows. If future imports
  contain non-confirmed breed/species labels again, emit
  `breed/species-needs-classification.csv` for that classification work.
- The first local post-apply RFID mapping review is captured in
  `docs/phases/phase-01-goat-passport/rfid-data-mapping-review.md`. Migration
  `000010_phase_1_rfid_plain_status_mappings.sql` implements only the approved
  plain F2/K2 `legacy_status_mappings`. The follow-up local rerun cleared
  `unknown_status_mapping` from 437 to 0, raised `created_goat` from 145 to
  383, kept `error` at 0, and redistributed 199 rows to downstream
  `species_or_breed_requires_review`. Migration
  `000011_phase_1_rfid_breed_cross_mappings.sql` implements Sirohi and the
  approved goat-cross labels as active goat breed/alias mappings. It uses the
  Phase 1 crossbreed-as-breed-row simplification because goats currently store
  one `breed_id`; richer parentage/compound-breed modeling is deferred. The
  follow-up local rerun raised `created_goat` from 383 to 436, kept `error` at
  0, and reduced `species_or_breed_requires_review` from 397 to 344. At that
  point the remaining breed/species bucket was Anantapur Sheep only. After the
  `000012` blank-suffix opt-in run, the confirmed non-goat disposition scope is
  504 breed/species review rows because 160 blank-suffix rows also hit the same
  gate. Later work may classify confirmed non-goats earlier in
  discovery/staging, but must keep them out of goat creation. Do not let
  confirmed non-goat rows stay indefinitely in an actionable review queue; a
  later review-ops/apply-semantics slice should choose terminal rejected
  disposition versus earlier source classification.
  The blank old-tag suffix policy decision recommended guarded RFID-only goat
  creation without creating an old_tag identifier: maximum possible additional
  goats is 435. Migration
  `000012_phase_1_rfid_blank_suffix_apply_candidates.sql` and
  `rfid-apply --allow-rfid-only-blank-suffix` implement the policy behind an
  explicit opt-in. The implementation dry-run and real local rehearsal both
  produced 711 created goats total, 275 above the post-000011 baseline of 436,
  with needs_review 512 and error 0. Remaining open-review reason occurrences
  are species_or_breed_requires_review 504, blank_old_tag_suffix 160,
  blank_gender 4, and duplicate_old_tag_same_scope 4; the 160 blank-suffix rows
  also fail breed/species review. The local SSR proof rendered real herd rows, a
  real goat passport, the 711 tenant_lifecycle alive counter, and live Import
  Review summary/rows against the same 711/512 run through admin-web. Fresh
  local closeout proof on June 12, 2026 reproduced this from a clean Docker
  Postgres database with all migrations through 000013. The real local DB had 0
  conflicts and 0 candidates, so Data Quality rendered an honest empty state
  while backend tests cover populated conflict/candidate read paths. C-lite
  suffix derivation from
  Farm/Shed/Partition remains rejected because Partition and Shed are not
  one-to-one with suffix context. Further data work includes source correction
  or reviewed policy for blank_gender plus duplicate same-scope old_tag rows.
- The import loop is repeatable, not one-time. Existing source-row identity is
  `source_row_key`; existing content-change detection is
  `source_row_version_hash`. Do not build a parallel dedupe state machine.
  Re-imports with the same key/hash are idempotent; same key/different hash is
  review-driven; new keys stage as new source rows.
- Future AI suggestion workers are proposer-only. They must write
  `ai_proposal` records that remain `proposed` or `needs_review`, include
  reasons plus evidence/source links, and must never write as `system_rule` or
  `import_policy`. Governed deterministic automation may use those actor types
  only under approved policy. Add candidate DB hardening before building the AI
  worker.
- `backend/cmd/outbox-relay` is the Phase 1 local/dev relay foundation. It is a
  one-shot CLI, not an API-server goroutine. It claims due `pending`
  `outbox_messages` rows in bounded `FOR UPDATE SKIP LOCKED` chunks, moves them
  to `publishing` with atomic attempt_count increment, validates payloads
  against `contracts/jsonschema/domain-event-envelope.schema.json`, publishes
  outside the claim transaction through a publisher port, and then marks
  `published`, `pending` retry with future `next_attempt_at`, `failed`, or
  `dead_letter`. Relay SQL only updates `status`, `attempt_count`,
  `next_attempt_at`, `last_error`, `published_at`, and `updated_at`; it must not
  update `tenant_id` or `event_id`.
- Fresh outbox rows have `status='pending'` and `next_attempt_at IS NULL`.
  Stale `publishing` rows are reclaimed by lease timeout without resetting
  `attempt_count`; fresh publishing leases must not be stolen. Rows already at
  max attempts dead-letter at claim time without publisher calls. Publisher
  panic is recovered per row and treated as retryable unless attempts are
  exhausted. Logging/no-op publisher logs safe metadata only and never raw
  payloads, RFID/tag values, headers, source rows, media URLs, or PII.
- Real Google Pub/Sub publishing, production worker deployment, event
  consumers, frontend event UI, and richer DLQ operations remain deferred.

Observability and logging:

- Construct every backend `*slog.Logger` via
  `backend/internal/platform/observability` (`observability.New`). Do not
  hand-roll `slog.New`/`slog.NewJSONHandler` in `cmd/`, `bootstrap/`, or
  `internal/` outside that package. `tools/agent-hooks/check-boundaries.sh`
  enforces this, rejects package-level `slog.Error`/`Info`/`Warn`/`Debug` calls
  in product code, and checks that `recover()` blocks either log or re-panic to
  an outer logger.
- Sink is env-selected via `GOATOS_OBS_SINK`: `stdout_json` (default, local),
  `otlp` (OTLP over HTTP, not gRPC per the go-backend-stack ADR), or `gcm`.
  OTLP/gcm are Phase 1 stubs that still emit `stdout_json`; startup logs warn
  about the fallback and record only whether `GOATOS_OTLP_ENDPOINT` is
  configured plus the sanitized scheme/host, never the full URL. Unknown sink
  values warn and fall back to `stdout_json` so typos are visible.
- Log once at boundaries, not at every `if err != nil`: HTTP 5xx logs
  server-side with trace_id/request_id/tenant through
  `backend/internal/platform/httpresponse.WriteError` before writing the
  envelope; the outermost recovery middleware logs panic + stack and returns a
  500 envelope; worker/CLI loops log errors with `import_run_id` context. Wrap
  propagating errors with `%w` so the boundary log carries the full chain.
- Goat identifiers (RFID, old tag, breed, farm) are business data, not PII —
  log them so failures are traceable to the exact goat/row. The only redaction
  rule is secrets: never log credentials, tokens, or service-account JSON.
- See `docs/decisions/observability.md` and `docs/runbooks/observability.md`.

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
  requires Idempotency-Key, authenticated actor, affected goat IDs,
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
  requires Idempotency-Key, authenticated actor, reason,
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
GET /goats/{goat_id}/timeline
GET /identity/correction-requests
GET /admin/identity/correction-requests
POST /admin/import-runs
POST /admin/goats
PATCH /admin/goats/{goat_id}
production IdP/JWKS/asymmetric auth and token lifecycle
AI suggestion worker and candidate-state DB hardening
dirty staged-row conflict/candidate creation and auto-link reconciliation
create_goat conflict decision fields
candidate approve canonical mutation semantics
standalone merge command handler
unmerge command handler/contract
incremental projection workers
externally visible counter rebuild-status metadata table
real Google Pub/Sub outbox publisher and production worker deployment
frontend event/DLQ UI
OpenTelemetry exporters/spans/metrics
P8 sales/allocation behavior
```

Counter projection rebuild:

```text
backend/internal/reporting owns goat_identity_counters reads and rebuilds.
/analytics/identity/counts remains the route, but bootstrap wires it to
reporting-owned service/repository code. backend/internal/identity must not
query goat_identity_counters; check-boundaries enforces this outside generated
sqlc schema/model dumps.

The analytics count read is explicitly paginated. Clients must provide `limit`
1..500 and may provide an opaque base64url JSON cursor. The repository orders
rows by `count_value DESC, counter_id ASC`, reads `limit+1`, and returns at most
`limit` rows with `has_more` plus nullable `next_cursor`. Structurally invalid
cursors fail closed as `invalid_cursor`; structurally valid arbitrary cursor
values are treated as keyset positions and are not checked against existing
rows. Cursors are scoped to the same query shape/projection version and are not
durable across counter rebuilds. Freshness fields describe projection freshness,
not raw count freshness. The old updated_at-order
`goat_identity_counters_lookup_idx` remains until a later cleanup verifies it is
unused.

check-contract-drift also validates the analytics counts example against a
hand-maintained grain -> allowed dimensions table. That table matches the
reporting rebuild SQL today. If a future slice changes a counter grain's
dimension columns, update both the reporting rebuild INSERT columns and the
validator table, or add a derived/diff test that compares the validator table
against backend/internal/reporting/adapters/postgres/sqlc/commands.sql.

The local rebuild command is backend/cmd/rebuild-identity-counters. It supports
tenant_id, optional source_import_run_id stamping, and an optional grain subset.
The rebuild uses grouped SQL per Phase 1 grain, delete+insert replacement per
tenant+grain, one repeatable-read transaction per committed rebuild attempt,
and a per-tenant advisory lock. Serialization/deadlock failures retry the whole
transaction in a bounded loop. The freshness watermark is
max(goat_identity_events.recorded_at) from the rebuild snapshot, never now() or
goats.updated_at. If no tenant events exist, the watermark is null. Successful
rebuild writes goat_identity_counter_projection_state with the checkpoint
(recorded_at + event_id) under the same advisory lock. Final rows keep
is_rebuilding=false; richer externally visible rebuild status metadata is
deferred.

Phase 1 counter membership excludes identity_state=merged and
identity_state=inactive from all grains. Lifecycle-bearing grains count
dead/sold lifecycle buckets for non-merged, non-inactive goats. Non-lifecycle
operational grains and custodian_identity count alive goats only. Location
grains use only goats.park_id for park_lifecycle and goats.park_id+shed_id for
shed_lifecycle in Phase 1; no farm_lifecycle or cohort_lifecycle grain exists.
Custodian grains use goats.custodian_party_id.

The local incremental command is backend/cmd/update-identity-counters. It scans
goat_identity_events ordered by recorded_at ASC, identity_event_id ASC after the
projection checkpoint and uses goat_identity_counter_processed_events for
event_id+recorded_at dedupe. Processed counter events must FK to
goat_identity_events on the full partition-aware identity
(tenant_id, identity_event_id, recorded_at), not event_id alone. goat.created
increments the same Phase 1 grain memberships as rebuild through the shared
goat_identity_counter_memberships view. goat.identifier.added, goat.identifier.retired, and
goat.identifier.disputed are noops that advance the checkpoint. merge_approved
or unknown event types set projection_state.rebuild_required=true and stop
before later events are processed; analytics freshness then returns
warning=rebuild_required. Missing projection state with counters or events also
requires rebuild; tenants with no counters and no events get an empty state.
Processed-event retention pruning is tenant/checkpoint bounded.

Production async worker deployment, Pub/Sub consumer wiring, before/after
multi-grain deltas for richer event types, and drift automation remain
deferred. Projection failures must never roll back canonical identity writes;
the counter table's count_value >= 0 check is a projection concern, not a reason
to abort a valid goat mutation. Large imports rebuild counters instead of doing
per-row counter increments.
```

Before extending this backend:

```text
read BUILD-STATUS.md for current Phase 1 state
run make test
run make check
run make sqlc-check for DB query changes
run make validate-sqlc-plans when adding/changing large-table identity/import/reporting reads
run make validate-migrations for schema-sensitive work
keep fixtures synthetic
```
