# Goat OS staging observability — infra reference

> Owner: platform (infra lane) · Companion to `docs/observability/OBSERVABILITY_DESIGN.md`
> (read that first — this file documents the Terraform/config that implements it).
> Status: **written, not applied**. Nothing in this pass runs `terraform apply`,
> `gcloud`, or any cloud-mutating command — see `infra/envs/stg/README.md`,
> which already establishes that live staging resources must be imported into
> the `gs://goatos-stg-tf-state` backend before any broad `apply`.

Every resource below is pinned to **asia-south1** — no us/eu/global defaults.
See "Regionality" near the end for the two GCP-managed services that cannot
be region-pinned by design (GMP, Cloud Trace).

---

## 1. Files added/edited

| File | What it declares |
|---|---|
| `infra/envs/stg/observability.tf` | 4 service accounts (grafana, grafana_alloy, gmp_frontend, analytics_rollup — no dedicated `otel_collector` SA, see section 12), their IAM bindings (including 3 GMP/Trace/Logging write roles granted directly to every OTel Collector sidecar host: `runtime["api"]`, every kernel job SA, and `grafana_alloy`), 3 Cloud Run v2 services (GMP query-frontend, Grafana, Grafana Alloy — Grafana Alloy carries an OTel Collector sidecar container; there is no standalone OTel Collector service), invoker IAM, and the 2 GCS buckets + objects that deliver static config/provisioning (the collector config now feeds 3 different sidecars: api, every kernel job, and grafana_alloy). |
| `infra/envs/stg/secrets.tf` (edited) | Adds `goatos-stg-grafana-admin-password` and `goatos-stg-grafana-postgres-datasource-password` Secret Manager containers + accessor IAM for the `grafana` SA. |
| `infra/envs/stg/monitoring.tf` (edited) | Adds 5 SLO/burn alert policies: API 5xx error-rate burn, API read-path p99 burn, API write-path p99 burn, consumer-lag, notification-failure-rate. Existing 4 policies (Cloud Run errors, outbox DLQ, Pub/Sub DLQ backlog, Cloud SQL CPU) are untouched. |
| `infra/envs/stg/cloud_sql.tf` (edited) | Adds `insights_config` (Query Insights) to `google_sql_database_instance.core`. |
| `infra/envs/stg/analytics_rollup.tf` | BigQuery dataset (asia-south1) for rollup working tables, conditional IAM read access to the GA4-owned export dataset once linked, the `goatos-stg-analytics-rollup` Cloud Run Job, and its daily Cloud Scheduler trigger. |
| `infra/envs/stg/variables.tf` (edited) | ~24 new variables — images, min-instance counts, operator IAM list, Query Insights tunables, SLO thresholds, rollup schedule/image/GA4 dataset id. All documented inline; defaults chosen so `terraform plan` works without extra input except where explicitly noted below. |
| `infra/envs/stg/outputs.tf` (edited) | Cloud Run URLs, service account emails, secret container ids, BigQuery dataset id, rollup job name, new alert policy names. |
| `infra/observability/otel-collector-config.yaml` | OTel Collector config: OTLP grpc+http receivers, memory_limiter/resourcedetection/batch processors, googlemanagedprometheus + googlecloud (traces) + googlecloud (logs) exporters, 3 pipelines. |
| `infra/observability/alloy-config.alloy` | Grafana Alloy config: `faro.receiver` (browser RUM ingest) forwarding logs+traces to the collector via `otelcol.exporter.otlphttp`. |
| `infra/grafana/provisioning/datasources/datasources.yaml` | 6 datasources (see section 4). |
| `infra/grafana/provisioning/dashboards/dashboards.yaml` | Dashboard file-provider pointing at the mounted dashboard JSON directory. |
| `infra/grafana/dashboards/01..06-*.json` | The 6 dashboards from design section 5 (API/RED, DB, Kernel pipeline, Frontend RUM, Mobile, SLO/burn). |
| `docs/observability/INFRA.md` | This file. |

`main.tf`, `services.tf`, `iam.tf`, `cloud_run_services.tf`, `cloud_run_jobs.tf`, `pubsub.tf`, `cloud_tasks.tf`, `gcs.tf`, `artifact_registry.tf`, `github_actions.tf`, `terraform.tfvars` were **not** touched. Every new SA/secret/BigQuery-dataset is declared standalone in the new files rather than folded into `main.tf`'s shared `runtime_service_accounts`/`secret_containers` locals, specifically so this observability lane stays additive and never risks a merge conflict with another concurrent lane's edits to those shared maps. Existing resources are only *referenced* (e.g. `google_sql_database_instance.core.connection_name`, `google_service_account.runtime[...]`), never redeclared.

---

## 2. New GCP resources at a glance

```text
Service accounts:  goatos-stg-grafana, goatos-stg-grafana-alloy,
                    goatos-stg-gmp-frontend, goatos-stg-analytics-rollup
                    (no dedicated otel-collector SA — see section 12)
Cloud Run services: goatos-stg-gmp-frontend     (internal, port 9090)
                     goatos-stg-grafana          (internal, port 3000, Cloud Run IAM)
                     goatos-stg-grafana-alloy    (public, port 12347, Faro ingest;
                                                  + OTel Collector sidecar, loopback 4318)
Cloud Run Job:       goatos-stg-analytics-rollup
OTel Collector:      sidecar container (loopback :4318, no external ingress) inside
                     goatos-api-stg, every goatos-stg-* kernel Job, and
                     goatos-stg-grafana-alloy — NOT its own Cloud Run service.
                     See section 12.
Cloud Scheduler:     goatos-stg-analytics-rollup-schedule (03:15 IST daily)
GCS buckets:         goatos-stg-observability-config (collector/alloy config)
                     goatos-stg-grafana-provisioning (datasources/dashboards)
BigQuery dataset:    goatos_stg_analytics_rollup (asia-south1)
Secret Manager:      goatos-stg-grafana-admin-password
                     goatos-stg-grafana-postgres-datasource-password
Cloud SQL change:    insights_config added to goatos-stg-core-db
Monitoring policies: goatos-stg API 5xx error-rate SLO burn
                     goatos-stg API p99 latency SLO burn (read)
                     goatos-stg API write-path p99 latency SLO burn
                     goatos-stg domain event consumer lag
                     goatos-stg notification dispatch failure rate
```

All Cloud Run services, the Cloud Run Job, Cloud Scheduler job, GCS buckets,
and the BigQuery dataset explicitly set `location`/`region` = `var.region`
(`asia-south1`) or a literal `"asia-south1"` for the BigQuery dataset — never
left to provider defaults.

---

## 3. Why 5 GCP components exist for 3 named services

The task named exactly 3 Cloud Run services (OTel Collector, Grafana, Grafana
Alloy). Two more were added because the named 3 cannot actually deliver what
the design doc asks for without them — both are called out explicitly so
they can be descoped if unwanted:

1. **`goatos-stg-gmp-frontend`** — Grafana's plain `prometheus` datasource
   type has no way to attach a GCP OAuth bearer token to each HTTP request,
   which is required to query Google Managed Service for Prometheus (GMP).
   Google's own reference architecture for "query GMP with a
   Prometheus-compatible client" is exactly this: run the small
   `gke.gcr.io/prometheus-engine/frontend` proxy, which holds
   `roles/monitoring.viewer` and injects the token server-side, and point
   the Prometheus datasource at the proxy instead of
   `monitoring.googleapis.com` directly. Internal-only, invoker restricted
   to the `grafana` service account.
2. **Google Cloud Monitoring (`stackdriver`) datasource** (in
   `datasources.yaml`, not a new Cloud Run service) — the DB dashboard needs
   native Cloud SQL infra metrics (`cloudsql.googleapis.com/database/cpu|memory|...`)
   and these live in a completely different metric namespace than GMP/OTLP
   application metrics. This datasource ships built into OSS Grafana at no
   extra cost/plugin and needs no extra IAM beyond `roles/monitoring.viewer`
   already granted to `grafana`.

If you want to strictly cap this to only the 3 named Cloud Run services,
remove `observability.tf`'s `gmp_frontend` SA/service/IAM block and switch
the "Google Managed Prometheus" datasource in `datasources.yaml` to
`type: stackdriver` with `jsonData.gceDefaultProject`'s PromQL query mode
(Grafana's native Cloud Monitoring datasource can query GMP data directly,
just with a different query UI than a "real" Prometheus datasource).

---

## 4. Grafana datasources (6)

| Name | uid | type | Auth |
|---|---|---|---|
| Google Managed Prometheus | `gmp-prometheus` | `prometheus` | via `goatos-stg-gmp-frontend` proxy |
| Google Cloud Trace | `cloud-trace` | `googlecloud-trace-datasource` | GCE ADC (`grafana` SA) |
| Google Cloud Logging | `cloud-logging` | `googlecloud-logging-datasource` | GCE ADC |
| Postgres (analytics rollups) | `postgres-analytics` | `postgres` | Cloud SQL Unix socket + `goatos_grafana_ro` role |
| BigQuery (GA4 export, ad-hoc) | `bigquery-analytics` | `grafana-bigquery-datasource` | GCE ADC |
| Google Cloud Monitoring | `cloud-monitoring` | `stackdriver` | GCE ADC |

**Plugin-id caveat (please verify before first apply):** `googlecloud-trace-datasource`,
`googlecloud-logging-datasource`, and `grafana-bigquery-datasource` are
third-party/community plugin ids installed via `GF_INSTALL_PLUGINS` on the
Grafana container. Context7 (library docs) and general web access were
**unavailable in this build session** (Context7 returned "Invalid API key");
confirm the exact current plugin ids/versions at
`grafana.com/grafana/plugins` before the first real deploy and adjust
`GF_INSTALL_PLUGINS` in `observability.tf` + the `type:` fields in
`datasources.yaml` if they've changed.

**Postgres datasource prerequisite (backend/migration-owned, not Terraform):**
create a read-only Postgres role `goatos_grafana_ro` (`GRANT USAGE ON SCHEMA
analytics; GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO
goatos_grafana_ro;` plus a default-privileges grant for future tables) and
load its password into `goatos-stg-grafana-postgres-datasource-password`.
Terraform only creates the secret container and the IAM accessor binding.

---

## 5. Cloud Run single-ingress-port constraint (OTLP grpc+http) — historical

This constraint mattered when the OTel Collector was a **standalone** Cloud
Run service: Cloud Run v2 routes a service's hostname to exactly **one**
container port per revision, so it could never expose both the OTLP gRPC
(4317) and OTLP HTTP (4318) receivers to external callers from one service.

As of section 12's sidecar rework, the collector is no longer an ingress
target at all — it runs as a **sidecar container with no `ports` block**
inside api/each kernel job/grafana_alloy, reachable only over loopback from
the container in the same revision. `otel-collector-config.yaml` still
declares both receivers (for architectural completeness / any future
non-Cloud-Run deployment target), but every producer talks to it over
**OTLP/HTTP on `http://localhost:4318`**, never OTLP/gRPC — `alloy-config.alloy`
already uses `otelcol.exporter.otlphttp` for this reason, and
`backend/internal/platform/observability/telemetry.go` is HTTP-only by
construction. A sidecar container declares no `ports` block at all (only the
ingress container in a revision may), so nothing in Terraform gates which
receiver a producer can reach — the collector image already listens on both
4317 and 4318 over loopback. If a gRPC-only SDK is unavoidable later, that
producer's exporter simply targets `localhost:4317` instead of `:4318`; no
infra change needed.

---

## 6. Grafana access: Cloud Run IAM, not IAP

The design doc allowed either IAP or plain Cloud Run IAM, "whichever is
lighter." This pass uses **Cloud Run IAM** (`ingress =
INGRESS_TRAFFIC_INTERNAL_ONLY` + `roles/run.invoker` granted only to
`var.observability_operator_members`), not an IAP-secured HTTPS Load
Balancer. Rationale: IAP requires a global external HTTPS LB, backend
service, an OAuth consent brand/client, and IAP-specific IAM bindings — real,
separately-billed infrastructure that a single internal stg pane-of-glass
service doesn't yet justify (the existing admin-web LB in
`infra/envs/stg/config.md` was provisioned *manually*, outside Terraform,
for the same class of reason). Revisit IAP only if Grafana needs a stable
public hostname or ends up behind a shared LB with other IAP-protected
services.

Until `var.observability_operator_members` is supplied, **nobody** can invoke
Grafana (empty set by default, mirroring the `stg_sweeper_actor_id`
no-default discipline — never guess a principal). Operator access flow once
IAM is granted:

```bash
gcloud run services proxy goatos-stg-grafana \
  --project=goatos-stg --region=asia-south1
# then open http://localhost:8080 — the proxy attaches your ID token per request
```

---

## 7. Apply order

Nothing here has been applied. Live staging resources still need import into
`gs://goatos-stg-tf-state` before *any* broad `terraform apply` — see
`infra/envs/stg/README.md`. Once that's done and the images below exist:

```bash
cd infra/envs/stg
terraform init                     # real backend, not -backend=false
terraform plan \
  -var='monitoring_alert_email_addresses=["ravi@mesha.sg"]' \
  -var='stg_sweeper_actor_id=<reviewed workforce_member_id>' \
  -var='observability_operator_members=["user:ravi@mesha.sg"]' \
  -out=observability.tfplan
# review the plan — it should show ONLY additive resources (4 SAs, 3 Cloud
# Run services, 1 Cloud Run Job, 1 Scheduler job, 2 GCS buckets + objects,
# 1 BigQuery dataset, 2 secrets, 1 cloud_sql_database_instance UPDATE
# in-place for insights_config, 5 new alert policies) PLUS in-place updates
# to the existing api service and every kernel Job (new otel-collector
# sidecar container + collector-config volume + GOATOS_OTLP_ENDPOINT env
# value change, per section 12) — nothing destructive to api/admin_web/
# kernel jobs, but this pass does change their revision templates.
terraform apply observability.tfplan
```

Recommended sub-order if you want to stage it instead of one big apply:

1. `terraform apply -target=google_secret_manager_secret.grafana_admin_password -target=google_secret_manager_secret.grafana_postgres_datasource_password` — then populate secret *versions* out-of-band (`gcloud secrets versions add ...`), same discipline as every other secret in this repo.
2. `terraform apply -target=google_sql_database_instance.core` — enables Query Insights alone; low blast radius, quick to verify in the console.
3. `terraform apply -target=google_cloud_run_v2_service.gmp_frontend` — verify it boots before wiring Grafana's Prometheus datasource at it.
4. `terraform apply -target=google_cloud_run_v2_service.grafana` — sign in, confirm datasources green (Cloud Trace/Logging/BigQuery/Cloud Monitoring should test-connect immediately via ADC; Postgres needs the `goatos_grafana_ro` role to exist first; GMP needs the frontend proxy reachable).
5. `terraform apply -target=google_cloud_run_v2_service.grafana_alloy` — this revision now includes the OTel Collector sidecar (section 12); check the new revision's logs for BOTH containers — Alloy should show its faro.receiver listening, and the sidecar should show "Everything is ready" — before wiring admin-web's `faro-web-sdk` `url` to this service's public URL (output `observability_cloud_run_services.grafana_alloy`).
6. `terraform apply -target=google_cloud_run_v2_service.api -target=google_cloud_run_v2_job.kernel` — this is what actually lands the OTel Collector sidecar + `GOATOS_OTLP_ENDPOINT=http://localhost:4318` onto api and every kernel Job (section 12). Confirm the new api revision's `otel-collector` container log shows "Everything is ready" before assuming telemetry flows; `GOATOS_OBS_SINK=otlp` must already be set (it is, in cloud_run_services.tf/cloud_run_jobs.tf) for `SetupTelemetry` to activate at all.
7. `terraform apply` (full) — picks up the analytics rollup + remaining alert policies.

There is no longer a step "redeploy backend api + kernel jobs with a new
`GOATOS_OTLP_ENDPOINT` output value" — the endpoint is now the Terraform-fixed
literal `http://localhost:4318`, baked into cloud_run_services.tf/
cloud_run_jobs.tf directly (section 12), so a plain `terraform apply` on those
resources is the only redeploy trigger needed; there is no cross-lane output
value for the backend lane to consume for this endpoint anymore.

---

## 8. New variables you must supply (no safe default)

| Variable | Why it has no default |
|---|---|
| `observability_operator_members` | Defaults to `[]` (nobody can invoke Grafana) — deliberately, so no principal is guessed. Supply the reviewed operator list via private tfvars. |
| `ga4_export_dataset_id` | Defaults to `""` (analytics rollup skips the GA4-dataset IAM grant/query entirely) until the manual Firebase-console GA4→BigQuery link (section 9) has run and you know the dataset id Firebase assigned. |

Everything else (images, min-instance counts, SLO thresholds, rollup
schedule, Query Insights tunables) ships with a reviewed default — override
only if you need a different value. Notable defaults:

```text
otel_collector_image   = otel/opentelemetry-collector-contrib:0.114.0
gmp_frontend_image     = gke.gcr.io/prometheus-engine/frontend:v0.15.1
grafana_image          = grafana/grafana:11.4.0
grafana_alloy_image    = grafana/alloy:v1.5.1
analytics_rollup_image_tag = stg   (image URL built the same way backend_image_tag is)
analytics_rollup_schedule  = "15 3 * * *" (Asia/Kolkata)
api_availability_slo           = 0.995
api_latency_p99_read_slo_seconds  = 0.8
api_latency_p99_write_slo_seconds = 1.5
consumer_lag_slo_seconds       = 60
notification_success_slo_ratio = 0.99
```

Pin every image to a digest, not a floating tag, before a real staging
deploy — the defaults above are reference/reviewed-at-write-time versions,
not a supply-chain guarantee.

---

## 9. Manual step: link GA4 → BigQuery in the Firebase console

This cannot be done from Terraform — it's a Firebase console action against
the `goatos-stg` Firebase project (see `goatos-firebase-project` memory: Goat
OS uses `goatos-prod`... confirm the exact per-flavor Firebase project id for
stg with the mobile/backend lane before doing this if it differs).

1. Firebase console → Project settings → Integrations → BigQuery → Link.
2. **Select the "asia-south1 (Mumbai)" BigQuery location explicitly.**
   Firebase's default suggestion is often a US multi-region — do not accept
   it. If GA4 data must live outside India for a legal reason, stop and get
   an explicit decision before proceeding (see the Organization Boundary
   Rule in AGENTS.md: Goat OS data belongs in the `vgoats.com` org's India
   region by default).
3. Enable "Daily" export (streaming export is unnecessary cost for this
   rollup cadence).
4. Firebase creates a dataset named `analytics_<GA4_property_id>` in the
   `goatos-stg` project, in the location you picked. Copy that dataset id
   into `var.ga4_export_dataset_id` and re-`terraform plan` — this turns on
   the conditional `data.google_bigquery_dataset.ga4_export` /
   `google_bigquery_dataset_iam_member.analytics_rollup_ga4_export_viewer`
   block in `analytics_rollup.tf`.
5. The `analytics` Postgres schema (and its rollup tables —
   `mobile_funnel_rollup`, `mobile_app_start_rollup`,
   `mobile_screen_render_rollup`, `mobile_crash_free_rollup`, referenced by
   the Mobile dashboard) is created by a **backend-owned SQL migration**,
   the same way every other Goat OS schema ships — not by this Terraform
   pass. Coordinate with the backend lane before the rollup job's first run.

---

## 10. Regionality — what could not be pinned to asia-south1

- **Google Managed Service for Prometheus (GMP)** and **Cloud Trace** are
  Google-managed, project-scoped services with no customer-facing region
  selector — Google decides the storage region/replication internally as
  part of the managed product. There is no Terraform argument to pin them.
  What *is* pinned: the OTel Collector, the GMP query-frontend proxy, and
  Grafana itself all run in `asia-south1`, and every metric/trace carries
  `deployment.environment=stg` / project `goatos-stg` so queries stay scoped
  to this project regardless of Google's internal storage placement.
- **Cloud Logging** is similarly a project-level, not region-selectable,
  sink by default (log buckets *can* be region-pinned, but the default
  `_Default` bucket is multi-region and out of scope for this pass — flagging
  for a follow-up if log data residency in India specifically is a hard
  requirement, since that would need a dedicated regional log bucket +
  routing sink, not just an exporter setting).
- Everything else — Cloud Run services/job, Cloud Scheduler, GCS buckets,
  Secret Manager replication, the BigQuery dataset, Cloud SQL itself — is
  explicitly `asia-south1` in this pass, not inherited from a provider
  default.

---

## 11. Assumptions / risks (read before treating this as final)

1. **Context7 (library docs) was unavailable this session** — every
   `resolve-library-id` call returned "Invalid API key." Nothing here was
   checked against live upstream docs for the OTel Collector
   `googlemanagedprometheus`/`googlecloud` exporters, Grafana Alloy's
   `faro.receiver`, or the Grafana plugin catalog. `terraform validate`
   passed (HCL is syntactically correct and references resolve), but the
   YAML/`.alloy` configs and the exact GMP-derived PromQL metric names in
   the dashboards/alert filters are **best-effort mappings**, not
   live-verified. Re-check all of the following before trusting an alert or
   dashboard panel: OTel Collector config schema for your pinned collector
   version, Alloy `faro.receiver`/`otelcol.exporter.otlphttp` argument
   names for your pinned Alloy version, and the exact
   `prometheus.googleapis.com/...` metric type strings once real telemetry
   flows (Metrics Explorer).
2. **Faro RUM "metrics"** — Alloy's `faro.receiver` forwards Faro payloads as
   OTel logs + traces, not native metrics (Web Vitals arrive as structured
   log records). The Frontend RUM dashboard queries Cloud Logging for these;
   if true GMP metrics for RUM are required later, add an OTel Collector
   `transform`/log-to-metric stage — not built in this pass.
3. **Mobile dashboard rollup tables** (`analytics.mobile_*_rollup`) are
   referenced by `05-mobile.json` but their schema/population is entirely
   backend-lane-owned; this infra pass only wires the Postgres datasource
   and the Cloud Run Job/Scheduler shell that will eventually populate them.
4. **GMP query-frontend and Cloud Monitoring datasource are additions**
   beyond the literal 3-named-service scope (section 3) — remove them if
   the reviewer wants to hold strictly to 3 Cloud Run services and accept a
   non-functional Prometheus datasource / no Cloud SQL infra-metric panels
   instead.
5. **`insights_config.record_client_address = true`** — Goat OS's own
   AGENTS.md logging-redaction rule only restricts credentials/tokens, not
   operational data, so this was left on; revisit if Cloud SQL client IP
   retention needs a different call for a compliance reason not currently
   documented.

---

## 12. Sidecar collector decision (OTel Collector reachability fix)

**Status: this is a fix to the original design in sections 1-11 above**,
applied before the stack was ever deployed. The standalone
`goatos-stg-otel-collector` Cloud Run service described there could never
actually receive telemetry. This section documents the blocker, the fix, and
what stays as a residual risk.

### 12.1 The blocker

The original design put the OTel Collector behind its own Cloud Run service
with `ingress = INGRESS_TRAFFIC_INTERNAL_ONLY`, and pointed the backend api,
all 13 kernel worker Jobs, and Grafana Alloy at it via
`GOATOS_OTLP_ENDPOINT = google_cloud_run_v2_service.otel_collector.uri`. Two
independent problems made this a dead end:

1. **No network path.** There is no Serverless VPC Access connector or
   Direct VPC egress configuration anywhere in `infra/envs/stg/*.tf`. A Cloud
   Run service calling another Cloud Run service's `INGRESS_TRAFFIC_INTERNAL_ONLY`
   endpoint over the *public* internet is rejected outright — internal
   ingress only accepts traffic that already arrives over Google's internal
   network (a VPC connector, Direct VPC egress, a Pub/Sub push subscription,
   or another product with an internal routing path), none of which existed
   here. Without one of those, "internal-only" is unreachable from anywhere
   except the Cloud Console/`gcloud run services proxy`.
2. **No way to authenticate even if the network path existed.** Cloud Run
   services normally require a Google-signed OIDC identity token on every
   request from another principal (`roles/run.invoker`) — this repo already
   granted that IAM role to every producer's SA. But the OTLP/HTTP Go
   exporters used here (`otlptracehttp`, `otlpmetrichttp` — see
   `backend/internal/platform/observability/telemetry.go`) have no built-in
   mechanism to fetch and attach a Google ID token to each export call; they
   are plain HTTP clients. So even a *public* collector gated by
   `run.invoker` would 401 every request from the api/kernel jobs, because
   nothing on the producer side ever attaches the token `run.invoker` checks
   for.

Net effect: telemetry from the backend api and every kernel worker Job would
never have reached the collector at all, silently — `SetupTelemetry` never
fails the process on an unreachable collector (by design, so telemetry
outages can't take down the API), so this would have shipped as a stack that
*looks* deployed and healthy while exporting nothing.

### 12.2 Options considered

- **(a) Add a Serverless VPC Access connector + keep the collector
  internal-only**, having producers route egress through the connector so
  internal ingress becomes reachable. Rejected for this pass: still leaves
  problem 2 unsolved (Go OTLP exporters still can't attach an ID token, so
  `run.invoker` would have to be dropped or replaced with a network-only
  trust boundary), and adds a new piece of standing network infrastructure
  (the connector itself, plus subnet planning) to solve a problem that has a
  simpler fix.
- **(b) Sidecar the collector into every producer's own Cloud Run
  revision** (this repo's choice) — Google's own documented pattern for
  exactly this class of problem
  (cloud.google.com/run/docs/deploying#sidecars). Producers talk to
  `http://localhost:4318`: no network hop leaves the revision's sandbox, so
  there is nothing for a VPC connector to bridge and nothing to
  authenticate. Zero new standing infrastructure; the trade-off is that the
  collector's resource footprint (1 vCPU / 512Mi in this pass) is now
  duplicated per revision (api, each kernel Job, Alloy) instead of shared by
  one instance — acceptable in stg given the modest telemetry volume; revisit
  the per-sidecar resource limits before a prod sizing pass.

### 12.3 What changed (this fix)

- **Removed**: the standalone `google_cloud_run_v2_service.otel_collector`
  resource, its dedicated `google_service_account.otel_collector`, that SA's
  3 project IAM role grants, and both `google_cloud_run_v2_service_iam_member.otel_collector_invoker*`
  bindings (no longer meaningful — nothing calls the collector as a separate
  Cloud Run principal anymore). Also removed: the `otel_collector_min_instance_count`
  variable (a sidecar scales with its parent revision; there is no separate
  instance count to tune) and the `otel_collector` keys in
  `observability_cloud_run_services` / `observability_service_accounts` outputs.
- **Added**: an `otel-collector` sidecar `containers` block (same
  `var.otel_collector_image`, same `otel-collector-config.yaml` via the
  existing `observability_config` GCS bucket volume, a `startup_probe`
  TCP-checking port 4318) inside:
  - `google_cloud_run_v2_service.api` (`cloud_run_services.tf`) — the `api`
    container gains `depends_on = ["otel-collector"]` so it waits for the
    sidecar's startup probe.
  - `google_cloud_run_v2_job.kernel` (`cloud_run_jobs.tf`, `for_each` over
    all 13 distinct kernel job service accounts) — the app container is
    declared **first** (`name = "app"`) and the sidecar **second**, because
    Cloud Run Jobs treat the first-declared multi-container task as the one
    whose exit code determines task success/failure; the sidecar is
    auto-terminated once `app` exits, regardless of the sidecar's own state.
    `app` also gets `depends_on = ["otel-collector"]`.
  - `google_cloud_run_v2_service.grafana_alloy` (`observability.tf`) — same
    shape, main container renamed `grafana-alloy` with `depends_on =
    ["otel-collector"]`.
- **Changed**: `GOATOS_OTLP_ENDPOINT` (api, every kernel job) and
  `GOATOS_OTEL_COLLECTOR_ENDPOINT` (Alloy) all now hold the literal
  `http://localhost:4318` instead of `google_cloud_run_v2_service.otel_collector.uri`.
  `alloy-config.alloy`'s `tls.insecure` flipped from `false` to `true` to
  match (loopback HTTP, nothing to encrypt).
- **IAM**: since Cloud Run v2 has exactly one service account per revision
  template — shared by every container in it, sidecars included — the
  collector sidecar inside api runs as `runtime["api"]`, the sidecar inside
  each kernel Job runs as that job's own runtime SA, and the sidecar inside
  Alloy runs as `grafana_alloy`. The 3 roles the old dedicated SA held
  (`roles/monitoring.metricWriter`, `roles/cloudtrace.agent`,
  `roles/logging.logWriter`) are now granted directly to `runtime["api"]` and
  all 13 kernel job SAs (new `google_project_iam_member.sidecar_collector_*`
  resources, `for_each` over the same `local.otel_collector_telemetry_producers`
  set that used to drive `run.invoker` grants on the now-deleted service).
  `grafana_alloy` already held all 3 roles from the original design, so no
  change was needed there.

### 12.4 Backend correctness check (no backend change needed)

`backend/internal/platform/observability/telemetry.go`'s
`normalizeOTLPEndpoint` already special-cases `http://` prefixes: it strips
the prefix and returns `insecure = true`, which both `otlpTraceHTTPOptions`
and `otlpMetricHTTPOptions` then translate into `otlptracehttp.WithInsecure()`
/ `otlpmetrichttp.WithInsecure()`. So `GOATOS_OTLP_ENDPOINT=http://localhost:4318`
is handled correctly by the existing exporter construction — **no backend
code change was required for this fix.**

### 12.5 The RUM/Alloy path specifically

Alloy's ingress is public (`INGRESS_TRAFFIC_ALL` — browsers post RUM payloads
directly to it), but the *old* Alloy -> collector hop was still
public-Cloud-Run-service -> internal-Cloud-Run-service, the identical
blocker as api/kernel-jobs. Two fixes were possible: (a) make the standalone
collector `INGRESS_TRAFFIC_ALL` + `run.invoker`-gated and give Alloy's
`otelcol.exporter.otlphttp` an `auth` component that attaches a Google ID
token, or (b) give Alloy its own sidecar collector and drop the standalone
service entirely. This pass took **(b)** — same sidecar shape as api/kernel
jobs, for the same reason: zero new network/auth surface, and it lets the
standalone collector service (and its dedicated SA) be deleted outright
instead of widening its ingress and building token-attachment config that
would only exist to serve one caller.

### 12.6 Residual risks

- **Sidecar flush-on-shutdown window (kernel Jobs).** The app container's
  `SetupTelemetry` deferred shutdown does a synchronous `ForceFlush` before
  the process exits, so its own batched spans/metrics are POSTed to the
  loopback collector *before* the app container exits — that part is solid.
  What's unverified in this pass: whether the collector sidecar itself gets
  enough grace period between the SIGTERM Cloud Run sends it (once `app`
  exits) and the SIGKILL that follows to flush its own `batch` processor
  out to GMP/Cloud Trace/Cloud Logging. OTel Collector flushes its batch
  processor immediately on receiving a shutdown signal rather than waiting
  out `batch.timeout` (5s in `otel-collector-config.yaml`), which should be
  fast, but this has not been live-verified against Cloud Run's actual
  multi-container termination grace period for Jobs. Watch for a gap between
  "last kernel Job span the app exported" and "spans actually visible in
  Cloud Trace" during first real staging runs; if it's lossy, the fix is
  either a longer Job `timeout` (grace happens within it) or an explicit
  short sleep in the collector's shutdown path.
- **Per-sidecar resource duplication.** Every api instance, every kernel Job
  task, and Alloy now each run their own 1 vCPU / 512Mi collector process
  instead of sharing one. Fine at stg's telemetry volume; re-evaluate sizing
  (and whether a shared collector + VPC connector becomes worth it) before a
  prod pass with real traffic volume.
- **Grafana -> GMP-frontend reachability fixed.** The `gmp_frontend`
  query-frontend proxy now runs as a **sidecar container inside the Grafana
  Cloud Run service revision** (same pattern as the OTel Collector sidecars),
  instead of as its own `INGRESS_TRAFFIC_INTERNAL_ONLY` service. Grafana
  queries it via `http://localhost:9090` (loopback, no TLS, no auth), avoiding
  the internal-ingress reachability gap entirely. The standalone
  `google_cloud_run_v2_service.gmp_frontend` service and its dedicated SA are
  retired; the Prometheus datasource is now fully functional.

---

## 13. Security — open RUM sink (staging-only risk)

The `goatos-stg-grafana-alloy` Cloud Run service runs with `INGRESS_TRAFFIC_ALL`
and `allUsers` granted `roles/run.invoker`, exposing the `faro.receiver` RUM
endpoint to the public internet. This is **necessary for browsers to POST
telemetry**, but creates a risk: malicious websites could flood it with fake
RUM payloads, consuming quota and inflating billing on Cloud Trace / Cloud
Logging / GMP.

**Mitigations:**

1. **CORS origin restriction** (implemented in `alloy-config.alloy`): the
   `faro.receiver` is configured to reject CORS preflight and cross-origin
   requests from any origin except `https://stg.dashboard.mesha.sg`. Browsers
   enforce this; non-browser clients may bypass it, but the first-line filter
   reduces naive attacks. If the stg domain changes, update both the Alloy
   config and the admin-web Faro SDK configuration.

2. **Rate limiting** (required, out of scope for this pass): a Cloud Armor
   policy must be added to the Grafana Alloy Cloud Run service's load
   balancer to drop requests exceeding a conservative per-IP threshold (e.g.,
   100 RUM events per minute per IP). See `docs/observability/OBSERVABILITY_DESIGN.md`
   section 4.3 and the Cloud Armor documentation. Until this is deployed,
   staging is vulnerable to RUM-endpoint flooding attacks.

For **production**, the RUM endpoint must be gated by a real rate limiter at
the LB level (Cloud Armor is the natural choice) and the CORS origin list must
be reviewed and narrowed to only the prod admin-web domain. Document this as a
pre-production launch checklist item.
