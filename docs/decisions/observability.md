# Observability and Logging

Status: accepted
Scope: backend (Go). Cross-cutting infra, not a single phase deliverable.

## Context

Backend logging is structured `slog`, with request_id + W3C traceparent +
request duration at the HTTP layer. Gaps that lost failures:

- Each `cmd/*/main.go` hand-rolled its own slog handler. No single seam to point
  at a different sink, so pointing at Grafana/GCM meant editing every binary.
- HTTP 5xx responses returned a trace id to the client but logged nothing
  server-side: the trace id correlated to no log line.
- No HTTP panic-recovery middleware: a handler panic was silent.
- The outbox publisher recovered a panic without logging it, so retries were
  blind.

## Decision

`backend/internal/platform/observability` is the single seam.

- `observability.New(Config{...})` builds the `*slog.Logger` and stamps standard
  fields (service, version, env) on every record.
- Sink selected by `GOATOS_OBS_SINK`:
  - `stdout_json` (default, local) — structured JSON to stdout.
  - `otlp` — OTLP over HTTP (not gRPC, per the go-backend-stack ADR).
  - `gcm` — alias for `otlp`, intended for GCP OTLP ingestion.
  - Phase 1: `otlp`/`gcm` are stubs that still emit `stdout_json`; a real
    OTLP-HTTP exporter wires into `observability.New` without changing callers.
- Level via `GOATOS_LOG_LEVEL` (debug/info/warn/error), env via `GOATOS_ENV`.

Logging discipline (enforced by `tools/agent-hooks/check-boundaries.sh`):

- Construct loggers only via `observability.New`. No hand-rolled `slog.New` in
  `cmd/`, `bootstrap/`, or `internal/` outside the package (test files exempt).
- Log once at boundaries (HTTP 5xx, CLI top, worker loops) with err +
  request_id + trace_id + tenant_id + import_run_id. Do not log-and-return at
  every `if err != nil`; wrap with `%w` and let it surface once.
- recover + log at every goroutine edge (HTTP middleware, worker loops): a
  `recover()` must log the panic before suppressing or converting it. The guard
  fails any `recover()` block whose file has no log call.

## Data classification (important)

Goat identifiers (RFID, old tag, breed, farm, shed, partition) are operational
livestock business data, NOT PII. Log them in diagnostics so a failure is
traceable to the exact goat/row. The only logging redaction rule is secrets:
never log credentials, tokens, or service-account JSON. (Not committing raw
private source files to git is separate repo hygiene, not a logging rule.)

## Rollout

1. Observability package + env sink; replaced hand-rolled slog in cmds and
   bootstrap. (done)
2. HTTP 5xx logs server-side before writing the envelope. (done)
3. Outermost HTTP panic-recovery middleware: recover -> log err + stack +
   trace_id -> 500 envelope. (done)
4. Outbox/worker recover sites log the panic before retry. (done)
5. check-boundaries guards: `slog.New` outside package, `recover()` without a
   log. (done)
6. Later (separate ADR): real OTLP-HTTP exporter; traces/metrics spans across
   DB/import/worker; cloud sink wiring for goatos-dev.

## Consequences

- One env var swaps local stdout vs Grafana/GCM. Plug-and-play.
- A 5xx, panic, or worker failure now leaves a structured, trace-correlated log
  carrying the goat/row identifiers needed to diagnose it.
- OTLP-HTTP keeps the no-direct-gRPC rule intact; a gRPC exporter needs its own
  ADR.
