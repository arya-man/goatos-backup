# Goat OS Observability — Index & Quickstart

> Env: **goatos-stg**, region **asia-south1**. Status: infra written, not yet
> applied (see `RUNBOOK.md`). This page is the front door to the stack — start
> here, then follow the links below for depth.

## What / why

Goat OS is an event-driven operational kernel
(`event → txn → audit/outbox → trigger → obligation → sweeper → notify → proof
→ read model`). A slow or silently-broken stage anywhere in that chain is
invisible without instrumentation, so every layer — backend API, DB, kernel
workers, the admin-web frontend, and the mobile app — emits telemetry over
OpenTelemetry (OTLP), and a single self-hosted **Grafana** is the pane of
glass over GCP-native, no-TSDB-to-operate backends (Google Managed Prometheus,
Cloud Trace, Cloud Logging) plus Postgres/BigQuery for product analytics. See
`OBSERVABILITY_DESIGN.md` for the full rationale and rejected alternatives.

## Overview diagram

```
                         ┌───────────────────────── Grafana (Cloud Run, IAM) ───────────────────────────┐
                         │  datasources: Managed-Prometheus · Cloud Trace · Cloud Logging · Postgres · BigQuery │
                         └──────────────▲──────────────▲───────────────▲──────────────▲──────────────────┘
                                        │ PromQL       │ traces         │ logs         │ SQL rollups
        metrics ─────────► Google Managed Service for Prometheus (GMP)  │              │
        traces  ─────────► Cloud Trace                                  │              │
   ┌────────────────────┐                                              Cloud Logging   Cloud SQL (analytics.*)
   │  OTel Collector     │◄── OTLP HTTP ── backend api (Cloud Run)      │              ▲ scheduled rollup
   │  (Cloud Run svc)    │◄── OTLP ─────── 13 worker Jobs               │              │
   │  exporters:         │◄── OTLP ─────── Grafana Alloy (Faro rcv)     │        BigQuery (GA4 export)
   │   googlemanagedprom │◄── Faro/OTLP ── admin-web (browser RUM)      │              ▲
   │   googlecloud trace │◄── OTLP ─────── goatos-android (OkHttp)*     │        Firebase Analytics (GA4)
   │   googlecloud log   │                                        stdout│  Firebase Perf + Crashlytics
   └────────────────────┘                     Cloud Run stdout JSON ────┘        (Android)
```
\* Android OTLP export is reserved (`BuildConfig.OTLP_ENDPOINT`) but not yet
wired — see `apps/goatos-android/docs/TELEMETRY.md` §6.

Full architecture, signal-by-signal design, naming/cardinality rules, and
rollout plan: **`OBSERVABILITY_DESIGN.md`**.

## Dashboards (v1)

Provisioned as file-based JSON in `infra/grafana/dashboards/`. Each answers a
distinct operator question:

| # | Dashboard | Answers |
|---|---|---|
| 1 | **API / RED** | Is the API healthy right now? Request rate, error rate, p50/p90/p99 per route, top slow routes, in-flight requests. |
| 2 | **DB** | Is Postgres the bottleneck? Query p50/90/99 by operation/table, connection-pool saturation, Cloud SQL CPU/mem/connections, Query Insights. |
| 3 | **Kernel pipeline** | Is the event→obligation→notify chain keeping up? Per-stage latency, outbox queue depth/DLQ, consumer lag, sweeper batch throughput, notification success by channel, Cloud Tasks enqueue, end-to-end event age. |
| 4 | **Frontend RUM** | Is admin-web fast and error-free for real users? Web Vitals (LCP/CLS/INP/TTFB), JS error rate, slow routes, session count, geo. |
| 5 | **Mobile** | Is the Android app fast, stable, and completing its funnel? App-start, screen render, network p50/90/99, crash-free rate, funnel conversion (login→bootstrap→drive-open→scan→vaccination-capture→submit). |
| 6 | **SLO / burn** | Are we about to breach an SLO? Availability + latency SLOs with multi-window burn-rate alerts. |

## Signal → backend mapping

| Signal | Producer | Transport | Backend | Grafana datasource |
|---|---|---|---|---|
| Metrics (RED, kernel, DB pool) | backend api, 13 kernel Jobs | OTLP/HTTP → OTel Collector | Google Managed Prometheus (GMP) | `gmp-prometheus` (via `goatos-stg-gmp-frontend` proxy) |
| Traces (request, DB query, kernel stage) | backend api, kernel Jobs | OTLP/HTTP → OTel Collector | Cloud Trace | `cloud-trace` |
| Logs (structured `slog` JSON) | backend api, kernel Jobs | stdout → Cloud Run auto-collection | Cloud Logging | `cloud-logging` |
| Web RUM (Web Vitals, JS errors, route traces) | admin-web (`@grafana/faro-web-sdk`) | Faro → Grafana Alloy → OTLP/HTTP → OTel Collector | Cloud Trace (traces) / Cloud Logging (RUM events, logs-as-metrics) | `cloud-trace`, `cloud-logging` |
| Cloud SQL infra metrics (CPU/mem/connections) | Cloud SQL itself | native | Cloud Monitoring | `cloud-monitoring` (`stackdriver`) |
| Mobile funnels/journeys | goatos-android (Firebase Analytics/GA4) | GA4 native export | BigQuery (raw) → scheduled rollup → Postgres `analytics.*` | `postgres-analytics` (dashboards), `bigquery-analytics` (ad-hoc) |
| Mobile crash/perf | goatos-android (Crashlytics, Firebase Performance) | Firebase native | Firebase console (not yet in Grafana) | — |

## Where do I look when X is slow/broken?

| Symptom | Start here |
|---|---|
| API requests are slow or erroring | Dashboard 1 (API/RED) → drill into the slow route's trace in Cloud Trace |
| A specific request is slow end-to-end (API → DB → kernel) | Cloud Trace, find the trace by `request_id`/`traceparent`; spans should span API→DB→outbox→consumer since kernel trace context propagates through the outbox envelope |
| DB looks like the bottleneck | Dashboard 2 (DB) — check pool saturation first, then Cloud SQL Query Insights for the offending query |
| Obligations/notifications aren't firing or are late | Dashboard 3 (Kernel pipeline) — check outbox queue depth/DLQ and consumer lag first |
| admin-web feels slow or is throwing JS errors for users | Dashboard 4 (Frontend RUM) — Web Vitals + JS error rate; `ObservabilityErrorBoundary` also reports to Cloud Logging |
| Android app is slow, crashing, or funnel drop-off | Dashboard 5 (Mobile) — crash-free rate and funnel conversion; raw crash detail lives in Firebase Crashlytics console (not yet mirrored to Grafana) |
| Something might be about to breach an SLO | Dashboard 6 (SLO/burn) — multi-window burn-rate alerts page `monitoring_alert_email_addresses` |
| "Is telemetry even flowing?" after a deploy | `RUNBOOK.md` → "Verify telemetry is flowing" checklist |
| Can't reach Grafana at all | `GRAFANA_ACCESS.md` |

## Related docs

| Doc | Covers |
|---|---|
| `OBSERVABILITY_DESIGN.md` | Architecture, signal design, naming/cardinality, rollout plan — source of truth |
| `INFRA.md` | Terraform resources, apply order, Grafana datasources, known caveats/risks |
| `GRAFANA_ACCESS.md` | How humans and Claude/Codex reach Grafana (headless token flow, MCP wiring, browser fallback) |
| `RUNBOOK.md` | Deploy commands, verify-telemetry-is-flowing checklist, day-2 ops |
| `TELEMETRY_GUARDRAILS.md` | The CI rule requiring every new feature to wire analytics/crash/funnel telemetry |
| **`PROD_PROMOTION.md`** | **PROMOTING TO PROD? Read this first.** Stg→prod as a var-flip, not rebuild. Env matrix, two promotion paths (Terraform/imperative), handling empty state, canary rollout, verification checklist. |
| **`LESSONS_AND_GUARDS.md`** | **ANTI-RECURRENCE LEDGER.** Every bug fixed in stg build: symptom · root cause · fix · prevention guard · next-env check. Pre-deploy checklist for dev/stg/prod. |
| `../../apps/goatos-android/docs/TELEMETRY.md` | Android-specific telemetry wiring (Firebase Analytics/Perf/Crashlytics, funnel call sites, OTLP TODO) |
| `../decisions/observability.md` | Backend logging ADR (`slog`, `GOATOS_OBS_SINK`, panic-recovery logging) |
| `CEO_AI_OBSERVABILITY.md` | Leadership assistant (CEO AI) metrics catalog, SLO/latency budget, alerting notes, and the admin-only step-trace debug surface |

## Rollout status (see `INFRA.md`, `PROD_PROMOTION.md`, and `RUNBOOK.md` for detail)

- **Env: stg (live, partially flowing)**
  - Grafana live at https://goatos-stg-grafana-awtrpmn4za-el.a.run.app (Cloud Run, imperatively deployed)
  - All 6 dashboards + datasources provisioned via HTTP API (dashboards empty until backend telemetry flows)
  - **Backend telemetry is NOT flowing yet** — needs api + 13 kernel Jobs redeployed with
    `GOATOS_OBS_SINK=otlp` + collector sidecar (canary-gated rollout pending)
  - Terraform code written and ready (must be imported once, since stg was built imperatively)

- **Env: prod (not started)**
  - Use `PROD_PROMOTION.md` for Terraform-first approach (recommended over imperative)
  - Canonical path: copy infra/envs/stg/ → infra/envs/prod/, change env names, `terraform apply`

- **Not yet done (applies to any env)**
  - GA4→BigQuery Firebase-console link (manual Firebase console step per env)
  - Android OTLP export wiring (reserved in `BuildConfig`, not yet enabled)
  - Per-route admin-web Faro events (tracked in `TELEMETRY_GUARDRAILS.md` §3.2 as `warn` mode)
  - 4 mobile funnel feature call sites (owned by parallel mobile-feature session)
