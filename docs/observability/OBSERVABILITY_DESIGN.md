# Goat OS Observability & APM — Design (locked)

> Status: **building** · Owner: platform · First target env: **stg** (`goatos-stg`, `asia-south1`)
> Decision: **GCP-native backends + self-hosted Grafana** (no external SaaS, no self-run TSDB).

Goat OS is an event-driven operational kernel
(`event → txn → audit/outbox → trigger → obligation → sweeper → notify → proof → read model`).
Observability must measure **every stage** plus the request path, the DB round-trip, the
frontend (web RUM), and the mobile app (journeys, funnels, crashes, network latency).

---

## 1. Chosen architecture

All signals are produced with **OpenTelemetry** and shipped over **OTLP** to an
**OpenTelemetry Collector sidecar** running inside each producer's own Cloud Run
service/Job revision (not a standalone Cloud Run service — see
`docs/observability/INFRA.md` section 12 for why), which fans out to GCP-managed
backends. **Grafana** (self-hosted on Cloud Run, behind Cloud Run IAM) is the
single pane of glass.

```
                         ┌───────────────────────── Grafana (Cloud Run + IAP) ─────────────────────────┐
                         │  datasources: Managed-Prometheus · Cloud Trace · Cloud Logging · Postgres · BigQuery │
                         └──────────────▲──────────────▲───────────────▲──────────────▲──────────────────┘
                                        │ PromQL       │ traces         │ logs         │ SQL rollups
        metrics ─────────► Google Managed Service for Prometheus (GMP)  │              │
        traces  ─────────► Cloud Trace                                  │              │
   ┌────────────────────┐                                              Cloud Logging   Cloud SQL (analytics.*)
   │  OTel Collector     │◄── OTLP gRPC/HTTP ── backend api (Cloud Run) │              ▲ scheduled rollup
   │  (Cloud Run svc)    │◄── OTLP ─────────── 13 worker Jobs           │              │
   │  exporters:         │◄── OTLP ─────────── Grafana Alloy (Faro rcv) │        BigQuery (GA4 export)
   │   googlemanagedprom │◄── Faro/OTLP ────── admin-web (browser RUM)  │              ▲
   │   googlecloud trace │◄── OTLP ─────────── goatos-android (OkHttp)  │        Firebase Analytics (GA4)
   │   googlecloud log   │                                             stdout│  Firebase Perf + Crashlytics
   └────────────────────┘                     Cloud Run stdout JSON ────────►┘        (Android)
```

Why this shape:
- Cloud Run services + **Cloud Run Jobs are ephemeral / scale-to-zero** → pull-based Prometheus
  scraping does not fit. Everything is **push (OTLP)**.
- GMP is PromQL-compatible with **no TSDB to operate**; Cloud Trace + Cloud Logging are already
  enabled on `goatos-stg`. Zero external egress, all inside the `vgoats.com` org.
- One Collector = one export contract; adding dev/prod is a var flip.

### Rejected alternatives
- **Grafana Cloud (Mimir/Tempo/Loki/Faro)** — fastest, but external SaaS egress + cost + needs a
  vgoats-owned Grafana org. Kept as a future option; our SDK/OTLP wiring is portable to it.
- **Self-host VictoriaMetrics + Tempo/Loki on GKE** — max control, heaviest ops. Revisit only if
  GMP cost becomes the dominant line item.

---

## 2. Signal-by-signal design

### 2.1 Backend metrics (RED + kernel)
Emitted via OTel Meter from a shared `platform/observability` bootstrap.

Request path (RED), labels `route,method,status_class,tenant` (bounded cardinality — route
templates, **never** raw paths/ids):
- `http.server.request.duration` (histogram, seconds) → p50/p90/p99 per route
- `http.server.requests` (counter)
- `http.server.active_requests` (up-down counter)

DB round-trip (pgx tracer + pool stats):
- `db.client.operation.duration` (histogram) labels `operation,table`
- `db.client.connections.{usage,idle,max,pending}` from `pgxpool.Stat`

Kernel stages (from each worker's existing `RunResult` counts + timing):
- `kernel.outbox.publish.duration`, `kernel.outbox.queue_depth`, `kernel.outbox.dead_letters`,
  `kernel.outbox.reclaimed`, `kernel.outbox.retry_scheduled`
- `kernel.consumer.handle.duration`, `kernel.consumer.lag`, `kernel.consumer.validation_errors`
- `kernel.sweeper.batch.duration`, `kernel.sweeper.obligations_swept`, `kernel.sweeper.tasks_created`
- `kernel.notify.send.duration{channel}`, `kernel.notify.failures{channel}`, `kernel.notify.exhausted`
- `kernel.cloudtasks.enqueue.duration`, `kernel.cloudtasks.idempotent_collisions`
- Pub/Sub backlog comes free from GCP metric `pubsub.googleapis.com/subscription/num_undelivered_messages`.

### 2.2 Traces (distributed)
- Server: wrap the mux with `otelhttp`; span per request carrying `tenant_id,park_id,request_id`.
- DB: `otelpgx` query tracer → child span per query with SQL summary (no bound args).
- Kernel: manual spans in outbox relay / consumer / sweeper / dispatcher; **propagate trace
  context through the outbox message** (store `traceparent` in envelope) so a write and its async
  publish/consume/obligation land on **one trace**.
- Export: Collector → **Cloud Trace** (`googlecloud` exporter).

### 2.3 Logs
- Keep `slog` JSON → stdout → **Cloud Logging** (already auto-collected on Cloud Run).
- Add `logging.googleapis.com/trace` + `spanId` fields to every record so Cloud Logging &
  Grafana pivot log↔trace. Grafana **Cloud Logging** datasource for query.

### 2.4 Frontend web RUM (admin-web, Next.js 16)
- `@grafana/faro-web-sdk` + `@grafana/faro-web-tracing` initialised in `app/layout.tsx` (client).
- Captures Web Vitals (LCP/CLS/INP/TTFB), JS errors, route changes, session, and **traces fetch
  calls with W3C `traceparent`** → links browser span to backend span.
- Ships to **Grafana Alloy** (Cloud Run) `faro.receiver`, which forwards traces→Cloud Trace,
  logs→Cloud Logging, RUM metrics→GMP.
- API client (`packages/api-client`, fetch wrapper line ~93) injects/propagates `traceparent`.

### 2.5 Mobile (goatos-android)
- **Firebase Analytics (GA4)**: real `AnalyticsPort` impl replaces `NoopAnalytics`; wire existing
  `AnalyticsEvents` + define **funnels**: login → bootstrap → drive-open → scan → vaccination-capture
  → submit. User props: role, primary_park, flavor, tenant.
- **Firebase Performance**: app-start, screen render, and automatic + custom network traces.
- **Firebase Crashlytics**: crash + non-fatal reporting.
- **OTel-Android OkHttp interceptor**: emits network span/latency with `traceparent` to the
  Collector → Grafana, so mobile round-trips sit natively beside backend spans.
- Firebase project is **per env flavor** (`dev`/`stg`/`prod` → matching `goatos-*` Firebase app);
  each flavor gets its own `google-services.json`. **New files only** — do not touch the 4
  viewmodels currently modified by the parallel mobile session.

### 2.6 Product analytics / funnels → Grafana (BQ-cost-aware)
- GA4 (per Firebase project) → **BigQuery daily export** (native, free to export).
- A scheduled job (Cloud Run Job + Scheduler) runs BQ aggregation and **rolls funnel/journey
  metrics into Cloud SQL `analytics.*` tables**; Grafana reads **Postgres** for dashboards
  (cheap, no per-load BQ scan). Grafana **BigQuery** datasource kept only for ad-hoc deep dives.

---

## 3. Naming, cardinality, config

- Metric namespace `goatos.*` / `kernel.*` / `http.*` / `db.*`. Resource attrs:
  `service.name`, `service.version`, `deployment.environment`, `cloud.region`.
- **Cardinality guard**: label with route templates, tenant, park, status_class only. Never raw
  id/path/rfid/email. (Goat RFID/old-tag are non-PII and OK in logs, not as metric labels.)
- Backend env (extends existing `GOATOS_OBS_SINK`, `GOATOS_OTLP_ENDPOINT`):
  - `GOATOS_OBS_SINK=otlp` activates the real exporter (was stubbed → stdout).
  - `GOATOS_OTLP_ENDPOINT` = Collector URL. `OTEL_*` standard vars honoured.
  - `GOATOS_TRACE_SAMPLE_RATIO` (default 0.1 stg, 1.0 dev, tune prod).
- Secrets: `goatos-stg-grafana-admin-password`, `goatos-stg-grafana-*-datasource-*` in Secret Manager.

---

## 4. Infra (terraform `infra/envs/stg`, mirrored to dev/prod via vars)

New/changed files:
- `observability.tf` — OTel Collector Cloud Run svc, Grafana Cloud Run svc (+IAP), Grafana Alloy
  (Faro receiver) Cloud Run svc, service accounts + IAM (monitoring.metricWriter, cloudtrace.agent,
  logging.logWriter for collector; monitoring.viewer, cloudtrace.user, logging.viewer, bigquery.dataViewer,
  cloudsql.client for Grafana), Cloud SQL Insights enablement.
- `monitoring.tf` — add SLO alert policies (latency burn, error rate, DLQ, consumer lag) + keep existing.
- `secrets.tf` — Grafana admin + datasource secrets.
- Grafana provisioning (datasources + dashboards) as **files in repo**, mounted/baked:
  `infra/grafana/provisioning/datasources/*.yaml`, `infra/grafana/dashboards/*.json`.
- GA4→BQ rollup: BigQuery dataset + scheduled query / Cloud Run Job + Cloud Scheduler.

IAM principle: one least-privileged SA per component. Grafana is **read-only** to all backends.
Collector is **write-only** to metrics/trace/log. No component gets project-wide roles.

---

## 5. Dashboards (v1)
1. **API / RED** — req rate, error rate, p50/p90/p99 per route, top slow routes, in-flight.
2. **DB** — query p50/90/99 by op/table, pool saturation, Cloud SQL CPU/mem/connections/Insights.
3. **Kernel pipeline** — per-stage latency, outbox queue depth, DLQ, consumer lag, sweeper batch,
   notify success by channel, Cloud Tasks enqueue — one row per stage, end-to-end event age.
4. **Frontend RUM** — Web Vitals, JS error rate, slow routes, session count, geo.
5. **Mobile** — app-start, screen render, network p50/90/99, crash-free rate, funnel conversion.
6. **SLO / burn** — availability + latency SLOs with multi-window burn-rate alerts.

## 6. Rollout
- **stg first** (this pass), terraform parametrized so **dev/prod flip on with a var**.
- prod needs its Layer-1 terraform foundation before enabling (tracked separately).
- Deploy: build collector/grafana/alloy images → `terraform apply` in `infra/envs/stg` →
  redeploy backend api + 13 jobs with `GOATOS_OBS_SINK=otlp` + endpoint → verify metrics/traces
  flow in Grafana → wire admin-web RUM → wire Android telemetry.

## 7. SLOs (initial, tune with data)
- API availability 99.5% (non-5xx). API latency: p99 < 800ms read, < 1500ms write.
- Outbox publish lag p99 < 30s. Consumer lag < 60s. Notification success > 99%.
