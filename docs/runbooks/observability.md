# Observability

How to see what failed.

## Sinks

Set `GOATOS_OBS_SINK`:

- `stdout_json` (default) — structured JSON to stdout (local).
- `otlp` — OTLP over HTTP (Grafana Loki/Tempo, or GCM via OTLP). Phase 1 stub:
  still emits `stdout_json` until a real exporter is wired.
- `gcm` — alias for `otlp`.

If `GOATOS_OBS_SINK` is mistyped, startup logs
`observability_sink_unknown` and falls back to `stdout_json`. If `otlp`/`gcm`
is selected, startup logs `observability_sink_not_implemented` and records only
whether `GOATOS_OTLP_ENDPOINT` is configured plus the sanitized scheme/host. It
does not log the full endpoint URL because it may contain a token or password.

Other env: `GOATOS_LOG_LEVEL` (debug/info/warn/error), `GOATOS_ENV`.

## Local: persist logs (don't lose them on container teardown)

Pipe the backend/CLI stdout JSON to a file under the ignored local dir, e.g.
`.codex-goatos-render/logs/`. The rehearsal Docker container is ephemeral; the
file survives.

## Find a failed request

1. Client got a 5xx with a `trace_id`.
2. Grep the logs for that `trace_id` -> the server-side error line (error
   chain, request_id, tenant_id, route).
3. For import/apply failures, grep `import_run_id`.

## Panics

The outermost HTTP recovery middleware logs error + stack + trace_id and
returns a 500 envelope. Worker/outbox panics are logged with stack before
retry. A `recover()` that neither logs nor re-panics to an outer logger is
rejected by `tools/agent-hooks/check-boundaries.sh`.

## Rules

- Goat identifiers (RFID, old tag, breed, farm) are business data, not PII — they
  ARE logged so you can identify the exact goat/row that failed.
- The only redaction rule is secrets: never log credentials, tokens, or
  service-account JSON.
- Construct loggers via `backend/internal/platform/observability`, never
  hand-rolled `slog.New` or package-level `slog.Error`/`Info`/`Warn`/`Debug`.
