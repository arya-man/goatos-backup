# Backend Review — Go Modular Monolith (Hexagonal)

Backend lives in `backend/` — a Go modular monolith with hexagonal ports &
adapters. Stack rules: `docs/decisions/go-backend-stack.md` (net/http, pgx, plain
SQL / sqlc; no GORM, no DI container). Repo rules: `backend/AGENTS.md`. For kernel
and scale concerns, pair this with `references/kernel-and-scale.md`.

## Layout

```
backend/
  cmd/                 # ~37 binaries: api (HTTP server) + workers/sweepers/relays
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

Locate code with CRG (`semantic_search_nodes_tool`, `query_graph_tool`) before
grepping. Use `get_review_context_tool` to pull the changed source.

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
- [ ] No double-logging (log OR return, not both)
- [ ] No `panic` for expected failures; panics recovered-and-logged at goroutine edges
- [ ] `context.Context` propagated through all layers; no `context.Background()` in request paths
- [ ] Input validated at the boundary (size/type limits) before use

## Database, pgx & sqlc

- [ ] Parameterized queries only — no string-concatenated SQL
- [ ] Every scoped query filters `tenant_id` (multi-tenant isolation)
- [ ] Atomic work uses a single transaction (state + audit + outbox together)
- [ ] Concurrent writes lock rows (`SELECT … FOR UPDATE`) rather than racing
- [ ] Keyset pagination + explicit `LIMIT` on list queries; no unbounded scans
- [ ] Hot-path queries have an indexed access path; `make validate-sqlc-plans`
      updated when the query touches import/animal/event/counter rows at scale
- [ ] Migrations: `CREATE INDEX CONCURRENTLY`; no `NOT NULL` without `DEFAULT` on
      large existing tables; partition high-volume tables; migrations are ordered
      and idempotent-safe

## Observability & resilience

- [ ] Loggers built via `backend/internal/platform/observability` — never hand-rolled
      `slog.New`; sink from `GOATOS_OBS_SINK`. See `docs/decisions/observability.md`.
- [ ] Log once at boundaries with trace / request / tenant / import_run_id context
- [ ] New APIs/workers add metrics: latency, errors, DB pressure, queue lag, DLQ, media failures
- [ ] Metric labels low-cardinality (no per-animal IDs, no free text, no path params)
- [ ] External calls (HTTP, Pub/Sub, GCS, Cloud Tasks) have explicit timeouts + ctx cancellation
- [ ] Retries only on idempotent ops with bounded backoff; DLQ + max-attempts, no unbounded retry
- [ ] Logging redaction rule: secrets only (credentials, tokens, service-account JSON).
      Goat identifiers (RFID, old tag, breed, farm, shed) are livestock data, NOT
      PII — log them so a failure traces to the exact animal/row.

## Concurrency

- [ ] No unbounded goroutines — bounded worker pools / fixed batch sizes
- [ ] Goroutines have cancellation + leak prevention + panic recovery
- [ ] No bare `go func()` in handlers — use the background-task runner / jobs / Pub/Sub

## Testing

- [ ] New services: table-driven unit tests against mock port interfaces (not real DB/Pub/Sub)
- [ ] New handlers: success AND each error path covered
- [ ] State machines / transactions / migrations: integration test via `platform/pgtest`
- [ ] Idempotency tests: first call, exact replay, same-key different-payload, downstream dup prevention
- [ ] Tests run with `-race`; use CRG `query_graph_tool tests_for` to confirm the change is covered

## Style

- [ ] `gofmt` + `go vet` clean; run the narrowest relevant `go build` / `go test` for the change
- [ ] Files focused (<800 lines), functions small (<50 lines), nesting shallow (<4 levels)
- [ ] New code is enterprise-grade even where adjacent legacy is not — don't copy legacy patterns
