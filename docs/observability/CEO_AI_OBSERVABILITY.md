# CEO AI (Leadership Assistant) — Observability, SLO & Admin Debug

> Additive section to the observability stack (`README.md` → source of truth
> `OBSERVABILITY_DESIGN.md`). Scope: the read-only leadership assistant
> (`backend/internal/ceoai/**`, admin-web `features/ceo-ai*`). It reuses the
> shared OTel pipeline — the Collector, Google Managed Prometheus, Cloud Trace,
> and Cloud Logging — with no new backend to operate.

## 1. Why this exists

The assistant is a multi-stage read pipeline: `plan (Vertex) → route
(Cube → Mesha API → MCP Toolbox → SQL fallback) → execute → review → compose`.
A slow or silently-degraded stage (Vertex failover, Cube down, budget/rate
rejection, SQL guard rejection, injection block) is invisible without per-stage
metrics. This doc defines the metric set, the SLO/latency budget, alerting
notes, and the **admin-only step-trace debug surface** — the sanctioned way for
the small `ceo_internal`/superadmin cohort to see each internal step WITHOUT
violating the committed Internal Tracking rule that bans traces from the
leadership chat answer.

## 2. Metrics

Emitted from `backend/internal/ceoai/adapters/observability/metrics.go` against
the OTel global Meter `github.com/vgoats/goatos/backend/ceoai`. Instruments are
created eagerly and delegate to the real MeterProvider once
`observability.SetupTelemetry` installs it; in local/dev every record is a
no-op. Labels are bounded low-cardinality enums only — **never** `tenant_id`,
actor, `request_id`, question text, or any free-form value (per-request
correlation lives in the step trace / audit row, not in metric labels).

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `assistant_requests_total` | counter | `tool`, `status` | Terminal requests by resolved tool/tier and outcome (`ok`/`rejected`/`error`/`over_budget`/`degraded`). |
| `assistant_latency_ms` | histogram (ms) | `tool` | End-to-end answer latency. |
| `assistant_tool_rows_returned` | histogram (rows) | `tool` | Rows the grounding read returned. |
| `assistant_sql_rejected_total` | counter | `reason` | SQL-fallback statements rejected by the read-only guard. |
| `assistant_vertex_failover_total` | counter | — | Falls back from the Vertex planner to the deterministic keyword planner. |
| `assistant_toolbox_errors_total` | counter | — | MCP Toolbox tier errors. |
| `assistant_rate_limit_trips_total` | counter | — | Per-tenant/user rate-limiter rejections. |
| `assistant_budget_rejections_total` | counter | — | Per-tenant/user token/cost budget-cap rejections. |
| `assistant_cache_lookups_total` | counter | `result` (`hit`/`miss`) | Response-cache lookups; the hit/miss split is the `cache_hit_ratio` SLI. |
| `assistant_review_corrections_total` | counter | — | Answers the runtime review downgraded/corrected before returning. |
| `assistant_injection_blocked_total` | counter | — | Prompt-injection / scope-override attempts blocked. |
| `assistant_breaker_state` | gauge | `dependency` | Circuit-breaker state per dependency: `0` closed, `1` half_open, `2` open. |

Derived SLIs (PromQL sketch):

```promql
# cache hit ratio
sum(rate(assistant_cache_lookups_total{result="hit"}[5m]))
  / sum(rate(assistant_cache_lookups_total[5m]))

# error+degraded rate
sum(rate(assistant_requests_total{status=~"error|degraded"}[5m]))
  / sum(rate(assistant_requests_total[5m]))

# p95 latency
histogram_quantile(0.95, sum(rate(assistant_latency_ms_bucket[5m])) by (le))
```

## 3. SLO & latency budget

The assistant is an interactive leadership surface. Because it fans out to
Vertex + a governed read, its budget is **looser than the sub-500ms operator
API policy** (`tools/perf/api-latency-policy.mjs`) but still bounded — a
multi-second blocking answer is a bug. First-token latency matters more than
full-answer latency because the UX streams (SSE).

| SLI | Target (rolling 28-day) |
| --- | --- |
| Availability — non-`error` terminal answers (a `degraded` honest fallback still counts as served) | ≥ 99.0% |
| First-token latency (streaming) p95 | ≤ 1500 ms |
| Full-answer latency p95 (cache miss, single tool) | ≤ 6000 ms |
| Full-answer latency p95 (cache hit) | ≤ 800 ms |
| SQL-fallback rejection rate (`assistant_sql_rejected_total` / requests routed to SQL) | monitor-only; a spike is an injection/regression signal, not a user-facing SLO |

Error budget = 1.0% of terminal requests over 28 days. Budget burn is spent by
`status="error"`; `over_budget` and `rejected` (scope refusals) are **correct**
outcomes and do not burn the availability budget.

## 4. Alerting notes

Wire these as Managed-Prometheus alert rules (see `INFRA.md` for the alerting
pattern; none are applied until the assistant ships to stg):

- **Vertex failover storm** — `rate(assistant_vertex_failover_total[10m]) > 0`
  sustained ⇒ the Vertex planner is unhealthy; assistant is answering via the
  degraded keyword planner. Page if it persists > 15m.
- **Breaker open** — `max(assistant_breaker_state) >= 2` for any dependency
  sustained > 5m ⇒ a downstream (Cube/Toolbox/Postgres) is shedding.
- **Error-rate SLO burn** — error+degraded ratio > 5% for 10m ⇒ investigate.
- **Injection spike** — `rate(assistant_injection_blocked_total[10m])`
  abnormally high ⇒ possible abuse; correlate by tenant via the audit table (not
  a metric label).
- **Budget/rate exhaustion** — sustained `assistant_budget_rejections_total` /
  `assistant_rate_limit_trips_total` ⇒ a tenant is hitting caps; review caps or
  investigate a runaway client.
- **Cache collapse** — `cache_hit_ratio` dropping toward 0 with rising latency
  ⇒ cache invalidation bug or key churn.

## 5. Admin-only step-trace debug surface

The internal execution trace (sub-questions, resolved tool/tier, redacted
params, row counts, per-step latency, review verdict) is **INTERNAL** and never
appears in the leadership chat answer. It is exposed only to admins/engineers so
they can debug a specific request.

- **Storage**: `ceo_ai_assistant_audit` (migration `000021`), one row per
  request keyed by `request_id`, tenant-scoped. Written via
  `observability.PostgresTraceStore.RecordTrace`, which calls `TraceRecord.Sanitize()`
  so credential-shaped tokens are scrubbed **on write**. Goat RFID/tags are NOT
  PII and are kept for debugging; human-actor identity and secrets are not
  surfaced in the trace body (actor id stays in the audit row's own column, not
  the client payload).
- **Backend endpoint**: `GET /ceo-ai/admin/trace/{request_id}`
  (`ceoai/adapters/http/admin_trace.go`). **Hard role gate**: only an active
  `ceo_internal` grant may read; every non-admin (or unauthenticated) caller
  gets `403`/`401` and can never distinguish "exists" from "forbidden". Tenant
  scope comes from the session, so an admin in tenant A can never read tenant
  B's trace (cross-tenant reads return `404`, indistinguishable from missing).
  The response is `Sanitize()`d again at the boundary (defense-in-depth).
- **admin-web**: `app/(admin)/ceo-ai-admin/page.tsx` renders
  `features/ceo-ai-admin` — a request-id lookup that shows the trace. It is an
  internal diagnostic tool, not in the backend-composed sidebar nav (same state
  as `/verification` and `/operations/dlq`), reachable by direct route and
  deep-linkable with `?request_id=…` from an audit row. The proxy
  (`app/api/ceo-ai/admin/trace/[request_id]/route.ts`) adds no gating — the
  backend role gate is the security boundary. UI copy is a documented exception
  in `context/frontend/admin-web-backend-ui-contract.md`.
- **Redaction contract**: `RedactSecrets` matches Bearer tokens, JWTs, API-key
  shapes (`sk-…`, `AIza…`), PEM private keys, and `password|secret|token|
  api_key|authorization = value` pairs. Tests
  (`trace_test.go`, `admin_trace_test.go`) assert no secret leaks and that goat
  tags survive.

## 6. Where to look when the assistant is slow/broken

| Symptom | First check |
| --- | --- |
| Answers slow | `assistant_latency_ms` p95 by `tool`; `assistant_breaker_state`; cache hit ratio |
| Wrong/degraded answers | `assistant_vertex_failover_total`; `assistant_review_corrections_total`; then the admin step-trace for the exact `request_id` |
| "Access denied" for a leader | backend `ceo_internal` grant — scope is server-side, never client regex |
| A specific request's internal steps | admin debug surface `/ceo-ai-admin?request_id=…` (admin only) |
| SQL-guard rejections rising | `assistant_sql_rejected_total{reason}`; treat as an injection/regression signal |
