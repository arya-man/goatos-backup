# Backend Review — Go Modular Monolith (Hexagonal)

Backend lives in `backend/` — a Go modular monolith with hexagonal ports &
adapters. Stack rules: `docs/decisions/go-backend-stack.md` (net/http, pgx, plain
SQL / sqlc; no GORM, no DI container). Repo rules: `backend/AGENTS.md`. For kernel
and scale concerns, pair this with `references/kernel-and-scale.md`.

> **Review principle — verify, don't memorize.** This file encodes *patterns to
> check* and *where to confirm the current answer*, not frozen values. Status
> lists, allowed drive/vaccine combos, gap/threshold numbers, exact cron names,
> and table/column names all drift. When a check needs a concrete value, read it
> from the committed source named in the check (a migration `CHECK` constraint,
> seeded config, the rules doc, the Makefile, `package.json`) at review time.
> Any value written inline below is illustrative and may already be stale.

## Layout

```
backend/
  cmd/                 # api (HTTP server) + workers/sweepers/relays/checkers
  internal/
    <module>/          # domain modules (obligation, vaccination, protocol,
      domain/          #   calendar, notification, proof, sop, inventory,
      ports/           #   identity, locations, permissions, operationsaudit,
      adapters/        #   counts, feed, outbox, …)
        postgres/
        http/
        publisher/
      app/
    platform/          # shared infra: observability, postgres, outbox, taskqueue,
                       #   auth, httpmiddleware, httpresponse, pgtest, …
  migrations/postgres/ # SQL migrations (goose)
  sqlc.yaml
```

`cmd/` holds the HTTP server plus many worker/sweeper/relay/checker binaries
(api, outbox-relay, obligation-sweeper, notification-dispatcher, domain-event
consumers, counts/import checkers, etc.). Do not assume a specific binary exists
or assert an exact count — list `backend/cmd/` at review time. In particular, do
not invent binaries: if a review claims a worker (e.g. an in-progress-timeout or
drive-membership sweeper) enforces a rule, confirm the directory actually exists
before trusting it.

Locate code with CRG (`semantic_search_nodes_tool`, `query_graph_tool`) before
grepping. Use `get_review_context_tool` / `get_minimal_context_tool` to pull the
changed source. CRG namespace is harness-dependent (Claude
`mcp__code-review-graph__*`, Codex `mcp__code_review_graph__*`). ToolSearch the
live tool list before relying on optional tools such as `get_affected_flows_tool`;
if absent, use `detect_changes_tool`, `get_impact_radius_tool`, and targeted
`query_graph_tool`.

## Layer boundaries (enforced — violations are CRITICAL/HIGH)

```
HTTP handler (adapters/http)  -> parse request, call app service, write response
      │                          NO business logic, NO direct DB access
Application service (app/)    -> business logic, transaction boundaries,
      │                          idempotency, outbox writes; depends on PORTS
Domain (domain/)              -> entities, value objects, pure rules; imports NOTHING
Ports (ports/)                -> interfaces (Repository, Publisher, Gateway); imports domain only
Adapters (adapters/*)         -> pgx/SQL, HTTP, publishers; vendor SDKs CONFINED here
```

Check for:
- [ ] No business logic in HTTP handlers; no DB access outside adapters
- [ ] No `*pgx.Pool` / request-context types / vendor SDKs leaking into `app/` or `domain/`
- [ ] `domain/` imports no `net/http`, `pgx`, `database/sql`, or Google SDKs
- [ ] Ports are the contract; app depends on the interface, adapters implement it
- [ ] Each module owns its tables; other modules READ for projections but call the
      owning module's service/port for WRITES (no cross-module table writes)
- [ ] No new global state, `init()` side effects, or hidden package singletons
- [ ] Vendor SDK calls are not spread through product code — confined to an adapter behind a port

## Errors & correctness

- [ ] Errors never swallowed — handled or wrapped with context (`fmt.Errorf("Service.Method: %w", err)`)
- [ ] Error wrapping preserves the cause chain — no `%v` on an error that a caller
      or a `mapRepoError`-style translator needs to `errors.Is`/`errors.As` against
- [ ] No double-logging (log OR return, not both)
- [ ] No `panic` for expected failures; panics recovered-and-logged at goroutine edges
- [ ] `context.Context` propagated through all layers; no `context.Background()` in request paths
- [ ] Input validated at the boundary (size/type limits) before use

## State machines & status values (verify against migration + docs)

Obligation/calendar/outbox rows carry a status column governed by a DB `CHECK`
constraint AND a documented transition table. Do NOT trust a status list from
memory — the allowed set changes by migration (states get added over time).

- [ ] Any status literal a handler/query/sweeper writes is in the CURRENT allowed
      set — verify against the latest migration that alters the relevant
      `..._status_check` constraint (search `migrations/postgres/` for the newest
      `ALTER … status_check` on that table), not an earlier one
      *(e.g. obligation_instances today allows a scheduled/due/in_progress/
      deferred/completed/missed/waived/canceled/superseded family with default
      `scheduled`; treat this as illustrative — confirm in the migration)*
- [ ] Invented statuses are flagged: a computed view/work-state label (e.g. a
      `*_pending` display string) is NOT a persisted DB status — don't let a query
      filter on a status the constraint never allows (returns zero rows silently)
- [ ] Transitions follow the documented state machine
      (`docs/protocol-engine/state-machines.md`,
      `docs/protocol-engine/obligation-engine.md`): terminal states are immutable
      and only exit via explicit correction/rework, never automatic forward
      progression; a code path that mutates a terminal row is CRITICAL
- [ ] Read-time-derived states (e.g. an "overdue" bucket computed from due-window)
      are not conflated with persisted states, and `as_of` reconstructions bucket
      off completion/status-event history, not just the current row status

## Database, pgx & sqlc

- [ ] Parameterized queries only — no string-concatenated SQL
- [ ] Every scoped query filters `tenant_id` (multi-tenant isolation)
- [ ] **Cross-tenant IDOR:** a handler taking an object id from the URL path
      (`{goat_id}`, `{location_id}`, `{proof_id}`, `{task_id}`, `{obligation_id}`,
      …) re-derives / enforces the caller's tenant (and park/scope where relevant)
      IN the query. An RBAC pass authorizes the *action type*; it does NOT confirm
      the row belongs to the caller's tenant. A `WHERE id = $1` with no
      `AND tenant_id = $caller` is CRITICAL even behind a permission check.
- [ ] Atomic work uses a single transaction (state + audit + outbox together)
- [ ] Idempotency is a write-path contract: a stable key + semantic fingerprint is
      persisted in the SAME transaction as side effects; exact replay returns the
      original result with no new side effects/outbox; same-key different-payload
      is rejected or returns the original. `ON CONFLICT DO UPDATE` that only
      re-sets `idempotency_key = EXCLUDED.idempotency_key` is NOT sufficient when
      later code still mutates state — prefer `ON CONFLICT … DO NOTHING` on the
      `(tenant_id, idempotency_key)` unique constraint plus an explicit reserve.
      Confirm the unique constraint/reserve exists on the touched table rather
      than assuming a column name.
- [ ] Concurrent writes lock rows (`SELECT … FOR UPDATE`) rather than racing;
      multi-worker claim queries use `SKIP LOCKED`, leases, or another
      double-claim-safe pattern
- [ ] Keyset pagination + explicit `LIMIT` on list queries; no unbounded scans
- [ ] Hot-path queries have an indexed access path; `make validate-sqlc-plans`
      updated when the query touches import/animal/event/counter rows at scale

### Hot-table migration lock safety (populated tables → CRITICAL if unsafe)

A DDL that takes a strong lock on a large, live table stalls writes for the whole
tenant. On any migration that touches a populated hot table (identity/animal,
events, obligations, outbox, counters):

- [ ] New indexes on hot tables use `CREATE INDEX CONCURRENTLY` (which requires
      the migration run OUTSIDE a transaction — `-- +goose NO TRANSACTION`)
- [ ] New FK / CHECK constraints are added `NOT VALID` first, then a separate later
      migration runs `VALIDATE CONSTRAINT` (validation takes a weaker lock)
- [ ] New PRIMARY KEY / UNIQUE on a hot table is built as an index
      `CONCURRENTLY` then attached with `ADD CONSTRAINT … USING INDEX`, not a bare
      inline `ADD PRIMARY KEY` / `ADD UNIQUE` that rebuilds under an exclusive lock
- [ ] No direct inline `ADD COLUMN … NOT NULL DEFAULT <volatile>` or table rewrite
      on a large table without the safe multi-step rollout
- [ ] Migration lock-safety and structure are checked with
      `make validate-hot-index-migrations` and `make validate-migrations`; sqlc
      access plans with `make validate-sqlc-plans`
- [ ] `-- +goose Up`/`Down` are both present and reversible where feasible; a
      down that drops data is called out explicitly

## Auth, tenant, and abuse resistance

- [ ] Every new/changed mutation route is registered in the central route table
      `backend/internal/permissions/routes.go` (`protectedRoutes`). Fail-closed
      middleware rejects an *unregistered* route
      (`backend/internal/platform/httpmiddleware/auth.go`), so a MISSING entry is
      caught — but a registered-but-too-WEAK permission is NOT. The reviewer must
      confirm the mapped permission matches the write's real sensitivity.
- [ ] Mutation routes use the least-privilege permission for the write's actual
      sensitivity, not a nearby read/general permission (a destructive/override/
      config write behind a plain read or generic-action permission is HIGH)
- [ ] `dev_headers` auth bypass stays environment-gated: it may only apply when
      the config flag is set AND the environment is in the allowlist
      (local/dev/test — see `DevHeadersEnvironmentAllowed` in
      `platform/httpmiddleware/auth.go`). It must be impossible to enable in a
      production-like environment.
- [ ] Expensive, auth-adjacent, import, proof/media, replay, or repair endpoints
      have an abuse / rate-limit / backpressure story (per-tenant quota, bounded
      work, or job offload) rather than an unbounded synchronous path
- [ ] Proof/media paths enforce tenant scope, signed-URL expiry, content hash,
      size/type limits, and no API byte proxying for large media (see below)

## Media / proof integrity

Proof storage issues signed URLs and hashes content; there is a fixed signed-URL
TTL (illustratively ~15 min — verify `defaultSignedURLTTL` in
`backend/internal/proof/app/service.go`) and a content SHA-256 over stored bytes
(`proof/adapters/storage/local` and `.../gcs`). Check:

- [ ] Signed URL has a bounded TTL AND the serve path re-verifies expiry before
      returning the object (don't hand out a URL that outlives its grant)
- [ ] Content hash is computed on upload and re-verified on read/verify so a
      swapped/corrupted object is detected
- [ ] Upload enforces a max size and rejects oversized / disallowed content types
      BEFORE persisting (do not rely on the client). If no explicit size cap
      exists on the write path, that is a finding, not an acceptable default.
- [ ] Verify handles object-missing-at-verify time (deleted/expired blob) with a
      clear error, not a nil-deref or a silent pass
- [ ] EXIF / PII stripping is considered for user-captured media, and orphaned
      media (row deleted, blob left / blob deleted, row left) has a
      reconciliation/cleanup path
- [ ] Large media is streamed from storage with a signed URL, never proxied as
      bytes through the API process

## Observability & resilience

- [ ] Loggers built via `backend/internal/platform/observability` — never hand-rolled
      `slog.New`; sink from `GOATOS_OBS_SINK`. Package-level `slog.Error`,
      `slog.Info`, `slog.Warn`, and `slog.Debug` calls in product code are also
      forbidden outside `platform/observability` (tests exempt) because they
      bypass service/version/env fields and sink selection. See
      `docs/decisions/observability.md`.
- [ ] Log once at boundaries with trace / request / tenant / import_run_id context
- [ ] **Trace-context crosses async boundaries.** The event envelope written to
      the outbox must carry the trace/correlation id (e.g. `traceparent`) so
      outbox → Pub/Sub → consumer stays correlated. HTTP-only trace propagation
      loses the chain the moment work goes async — a consumer that starts a fresh
      root span with no link to the producing request is a finding.
- [ ] New APIs/workers add metrics: latency, errors, DB pressure, queue lag, DLQ, media failures
- [ ] Queue metrics include actionable lag/backlog SLOs such as oldest-unsent or
      oldest-unacked age; reconciler mismatch counters have alerts/owners
- [ ] Metric labels low-cardinality (no per-animal IDs, no free text, no path params)
- [ ] External calls (HTTP, Pub/Sub, GCS, Cloud Tasks) have explicit timeouts + ctx cancellation
- [ ] Retries only on idempotent ops with bounded backoff; DLQ + max-attempts, no unbounded retry
- [ ] Outbox/deployment reviews verify publisher mode safety: staging/prod jobs use
      `GOATOS_OUTBOX_PUBLISHER=pubsub` with project/topic/subscription/DLQ env,
      while `eventbus`/`logging` are local/dev-only and require explicit
      `GOATOS_OUTBOX_ALLOW_NONDURABLE=1`. A local non-durable relay run proves
      behavior only; it is not cloud readiness or durable Pub/Sub egress proof.
- [ ] Provider integrations that can fail under load have explicit circuit-breaker
      states (closed/open/half-open) and operator-visible pending/replay/discard
      paths; DLQ replay/discard is an operator action that is itself audited
      (who replayed/discarded what, when) rather than a silent fire-and-forget
- [ ] Logging redaction rule: secrets only (credentials, tokens, service-account JSON).
      Goat identifiers (RFID, old tag, breed, farm, shed) are livestock data, NOT
      PII — log them so a failure traces to the exact animal/row.
- [ ] **No secret committed to source or config.** A diff must not add a hardcoded
      credential, API key, bearer/OAuth token, DB password/DSN, private key, or
      service-account JSON to Go source, YAML/JSON config, migrations, or test
      fixtures. Secrets come from Secret Manager or the environment (`GOATOS_*` /
      injected secret refs) and are validated present at startup — never inlined.
      This is separate from the log-redaction rule above (that governs runtime
      output; this governs the committed tree), and there is no CI secret-scanner
      backstop, so the reviewer is the gate. Flag any real-looking key/token/`-----BEGIN`
      block added to the tree; a rotated/exposed secret must be rotated, not just
      deleted from the diff.

## Time & scheduling correctness

- [ ] Date/window math for sweepers and due-date comparisons resolves the
      business calendar day in the location timezone (currently `Asia/Kolkata` by
      default — verify `locations.timezone` in migrations/config), not the server
      timezone and not raw UTC. A local-vs-UTC mismatch shifts "due today",
      "missed", recovery, and drive-planned dates across a day boundary.
- [ ] Business dates use `platform/biztime` or an explicit animal/location
      timezone. Raw UTC may appear only for non-calendar instants such as
      persisted audit/event storage, retention cutoffs, and event-key
      normalization. A sweeper or scheduler using `.UTC().Date()` or
      `time.Now().UTC()` to decide a medical/business day is a finding. Convert
      the instant to the animal/location timezone first, then take the calendar
      day.
- [ ] No naive `time.Now()` in local time where the animal/location timezone or a
      stored instant is required; no assumption that the server timezone equals
      the tenant/location timezone.

## Concurrency

- [ ] No unbounded goroutines — bounded worker pools / fixed batch sizes
- [ ] Goroutines have cancellation + leak prevention + panic recovery
- [ ] No bare `go func()` in handlers — use the background-task runner / jobs / Pub/Sub

## Contracts & drift

- [ ] Web/mobile APIs stay OpenAPI-first. When a handler's request/response shape
      changes, verify the OpenAPI spec AND the generated client are regenerated
      and aligned with the handler — do NOT merely defer to CI. A handler that
      returns a field the spec/client doesn't know about (or vice versa) is a
      contract-drift finding even if the build passes.
- [ ] Backend-owned UI truth (nav, labels, filters, disabled reasons, summary vs
      detail field sets) flows through the contract, not frontend hardcoding —
      cross-check `context/frontend/` scope rules for the touched surface.
- [ ] Analytics / BI / dashboard-query changes honor cost and performance
      guardrails from `context/analytics/final-analytics-infra.md`: large facts
      require partition filters or marts/materialized views; reviewed BigQuery
      queries set max-bytes/quota/dry-run controls where applicable, avoid
      `SELECT *`, export jobs metadata, and alert on scan spikes.
- [ ] Dashboard/read-model APIs backed by projections expose the standard
      freshness envelope from `docs/decisions/high-scale-dashboard-projections.md`
      (`as_of`/`last_success_at`, `freshness_status`, `serving_state`, source
      watermark, unavailable sources, rebuild/stale flags, projection version).
      Stale, rebuilding, source-unavailable, or approximate responses must be
      visible to clients; do not serve projection data as if it is fresh truth.
- [ ] **Rate/percentage projections store `numerator` and `denominator`, not only
      the final percentage** (`docs/decisions/high-scale-dashboard-projections.md`).
      A bare stored `%` cannot be re-aggregated, re-based, or blended across
      sources, and a mixed-source rate then reads as fresh canonical truth. During
      a legacy→canonical cutover the projection also records source composition/
      version so a blended rate is never shown as pure canonical.

## Testing

- [ ] New services: table-driven unit tests against mock port interfaces (not real DB/Pub/Sub)
- [ ] New handlers: success AND each error path covered
- [ ] State machines / transactions / migrations: integration test via `platform/pgtest`
- [ ] Idempotency tests: first call, exact replay, same-key different-payload, downstream dup prevention
- [ ] **Migration test class (required for schema changes on hot tables):** the
      migration is exercised up AND down, applied against existing/backfill-shaped
      data (not just an empty schema), and any backfill is asserted for
      correctness and for lock-safety of the rollout
- [ ] **Query-plan / load test class (required for scale-sensitive paths):**
      changes to import/animal/event/counter queries carry query-plan evidence
      (`make validate-sqlc-plans`) and/or a load test, not only unit tests
- [ ] Rate-limit / backpressure behavior on expensive or auth-adjacent endpoints
      is covered by a test where one exists
- [ ] Tests run with `-race`; use CRG `query_graph_tool tests_for` to confirm the change is covered

## Style

- [ ] `gofmt` + `go vet` clean; run the narrowest relevant `go build` / `go test` for the change
- [ ] Files focused (<800 lines), functions small (<50 lines), nesting shallow (<4 levels)
- [ ] New code is enterprise-grade even where adjacent legacy is not — don't copy legacy patterns
