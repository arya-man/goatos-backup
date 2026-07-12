# Goat OS staging observability stack: OTel Collector, Grafana (+ its GMP
# query-frontend proxy), and Grafana Alloy (browser RUM ingest).
#
# Every resource here is pinned to asia-south1 (var.region) — no us/eu/global
# defaults. GMP and Cloud Trace are Google-managed, project-scoped services
# without a single-region pin option; see docs/observability/INFRA.md
# "Regionality" for that explicit call-out.
#
# Service accounts and secret containers for this stack are declared
# self-contained in this file (and secrets.tf) rather than folded into
# main.tf's shared `runtime_service_accounts` / `secret_containers` locals,
# to keep this observability lane additive and avoid touching a file another
# concurrent lane may be editing. They follow the exact same shape/style.
#
# ---------------------------------------------------------------------------
# OTel Collector reachability decision (see docs/observability/INFRA.md
# "Sidecar collector decision" for the full write-up):
#
# There is NO Serverless VPC Access connector / Direct VPC egress anywhere in
# this stg environment, so a Cloud Run service cannot reach another Cloud
# Run service's `INGRESS_TRAFFIC_INTERNAL_ONLY` endpoint — that traffic would
# have to leave over the public internet, where it is rejected. Separately,
# the OTLP/HTTP Go exporters (otlptracehttp/otlpmetrichttp) used by the
# backend api and every kernel worker Job cannot attach a Google-signed ID
# token, so even a public+`run.invoker`-gated collector would 401 every
# export call.
#
# The fix (Google's documented Cloud Run pattern for exactly this class of
# problem) is to run the OTel Collector as a **sidecar container** inside the
# SAME Cloud Run service/Job revision as each producer, instead of as its own
# standalone service. Producers export to `http://localhost:4318` — loopback,
# no TLS, no auth, no VPC — and the sidecar fans out to GMP/Cloud
# Trace/Cloud Logging exactly as before. This is wired onto:
#   - `google_cloud_run_v2_service.api` (cloud_run_services.tf)
#   - `google_cloud_run_v2_job.kernel` (cloud_run_jobs.tf, for_each)
#   - `google_cloud_run_v2_service.grafana_alloy` (below) — Alloy gets its own
#     sidecar too, so the standalone `otel_collector` Cloud Run service (which
#     had the identical internal-ingress problem for Alloy's public->internal
#     hop) is removed entirely rather than patched with a VPC connector or a
#     Google-auth exporter shim. See "why not option (a)" in INFRA.md.
#
# Cloud Run v2 assigns exactly ONE service account per revision template,
# shared by every container in it (sidecars included) — there is no
# per-container service account. So the collector sidecar inside the api
# service runs as `runtime["api"]`, the sidecar inside each kernel Job runs
# as that job's own runtime SA, and the sidecar inside Alloy runs as
# `grafana_alloy` (already granted the 3 export roles below). The dedicated
# `otel_collector` service account this file used to declare is gone; its 3
# IAM roles are now granted directly to every producer's own runtime SA.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Service accounts (least-privileged, one per component)
# ---------------------------------------------------------------------------

resource "google_service_account" "grafana" {
  account_id   = "goatos-stg-grafana"
  display_name = "Goat OS staging Grafana runtime"
  description  = "Read-only: single pane of glass over GMP, Cloud Trace, Cloud Logging, Cloud SQL analytics.*, and BigQuery (GA4 export). Never granted write access to any backend."
}

resource "google_service_account" "grafana_alloy" {
  account_id   = "goatos-stg-grafana-alloy"
  display_name = "Goat OS staging Grafana Alloy runtime"
  description  = "Terminates browser RUM (Faro) telemetry from admin-web and forwards it to the OTel Collector over OTLP/HTTP."
}

resource "google_service_account" "gmp_frontend" {
  account_id   = "goatos-stg-gmp-frontend"
  display_name = "Goat OS staging GMP query-frontend runtime"
  description  = "Read-only Prometheus-compatible query proxy in front of Google Managed Service for Prometheus, so Grafana's plain 'Prometheus' datasource type can query GMP without generating its own GCP OAuth token. See docs/observability/INFRA.md."
}

resource "google_service_account" "analytics_rollup" {
  account_id   = "goatos-stg-analytics-rollup"
  display_name = "Goat OS staging analytics GA4->BQ->Postgres rollup runtime"
  description  = "Reads the GA4 BigQuery export and writes rolled-up funnel/journey metrics into Cloud SQL analytics.* tables. See analytics_rollup.tf."
}

# ---------------------------------------------------------------------------
# IAM — least privilege per design section 4 ("IAM principle")
# ---------------------------------------------------------------------------

resource "google_project_iam_member" "grafana_monitoring_viewer" {
  project = var.project_id
  role    = "roles/monitoring.viewer"
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

resource "google_project_iam_member" "grafana_trace_user" {
  project = var.project_id
  role    = "roles/cloudtrace.user"
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

resource "google_project_iam_member" "grafana_logging_viewer" {
  project = var.project_id
  role    = "roles/logging.viewer"
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

resource "google_project_iam_member" "grafana_bigquery_data_viewer" {
  project = var.project_id
  role    = "roles/bigquery.dataViewer"
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

resource "google_project_iam_member" "grafana_bigquery_job_user" {
  project = var.project_id
  role    = "roles/bigquery.jobUser"
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

resource "google_project_iam_member" "grafana_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

resource "google_project_iam_member" "grafana_alloy_metric_writer" {
  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.grafana_alloy.email}"
}

resource "google_project_iam_member" "grafana_alloy_trace_agent" {
  project = var.project_id
  role    = "roles/cloudtrace.agent"
  member  = "serviceAccount:${google_service_account.grafana_alloy.email}"
}

resource "google_project_iam_member" "grafana_alloy_log_writer" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.grafana_alloy.email}"
}

# NOTE: gmp_frontend service account no longer needs monitoring.viewer IAM,
# as the sidecar now runs inside the Grafana Cloud Run service and inherits
# the grafana SA's roles/monitoring.viewer permission.

# Runtime SAs whose Cloud Run revision now carries an OTel Collector sidecar:
# the backend api plus every distinct kernel-worker-Job service account
# (14 — see main.tf local.database_clients, minus the one-shot migrate/
# legacy_sync/outbox_dlq admin jobs that are not part of the continuous
# kernel pipeline), matching the worker Jobs callout in
# docs/observability/OBSERVABILITY_DESIGN.md section 1.
#
# Each of these SAs is the template-level service account for its Cloud Run
# service/Job revision, so it is also the identity the sidecar collector
# container runs as (Cloud Run v2 has one service account per revision,
# shared by every container including sidecars — see the header comment
# above). These 3 roles replace the old dedicated `otel_collector` SA's
# grants now that there is no standalone collector service to hold them.
locals {
  otel_collector_telemetry_producers = setsubtract(
    local.database_clients,
    toset(["migrate", "legacy_sync", "outbox_dlq"])
  )
}

resource "google_project_iam_member" "sidecar_collector_metric_writer" {
  for_each = local.otel_collector_telemetry_producers

  project = var.project_id
  role    = "roles/monitoring.metricWriter"
  member  = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

resource "google_project_iam_member" "sidecar_collector_trace_agent" {
  for_each = local.otel_collector_telemetry_producers

  project = var.project_id
  role    = "roles/cloudtrace.agent"
  member  = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

resource "google_project_iam_member" "sidecar_collector_log_writer" {
  for_each = local.otel_collector_telemetry_producers

  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

# ---------------------------------------------------------------------------
# NOTE: there is no standalone "OTel Collector" Cloud Run service anymore.
# It ran into the reachability problem documented in the header comment
# above and in docs/observability/INFRA.md, so the collector is now a
# sidecar container inside api (cloud_run_services.tf), each kernel Job
# (cloud_run_jobs.tf), and grafana_alloy (below) — all reading the same
# `otel-collector-config.yaml` from the `observability_config` GCS bucket
# declared later in this file.
# ---------------------------------------------------------------------------

# NOTE: There is no standalone "GMP query-frontend" Cloud Run service anymore.
# It is now a sidecar container inside the Grafana Cloud Run service (see above)
# so that Grafana can reach it via loopback (http://localhost:9090) without
# the internal-ingress reachability problem that standalone internal-only
# services have. The gmp_frontend service account is retained for documentation
# but is not used; consider retiring it after the sidecar approach is verified.

# ---------------------------------------------------------------------------
# Grafana — Cloud Run service
#
# Access decision (design section 4 said "IAP or Cloud Run IAM, whichever is
# lighter"): this pass uses Cloud Run IAM, NOT an IAP-secured HTTPS Load
# Balancer. Standing up IAP requires a global external HTTPS LB, a backend
# service, an OAuth consent brand/client, and IAP web-app-user IAM bindings —
# real infrastructure this stg-only single-service pane-of-glass does not
# warrant yet (the existing admin-web LB in config.md was provisioned
# manually, outside Terraform, for the same reason). Cloud Run's own
# `roles/run.invoker` IAM plus Google-signed identity tokens gives equivalent
# per-user access control for a single internal service with far less
# surface area. Revisit IAP if/when Grafana needs a stable custom hostname or
# multiple backend services behind one LB. See docs/observability/INFRA.md
# for the exact `gcloud run services proxy` access flow operators use.
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_service" "grafana" {
  name                = "goatos-stg-grafana"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_INTERNAL_ONLY"
  labels              = local.labels

  template {
    service_account = google_service_account.grafana.email

    scaling {
      min_instance_count = var.grafana_min_instance_count
      max_instance_count = 2
    }

    containers {
      name  = "grafana"
      image = var.grafana_image

      # Wait for the GMP frontend sidecar to be ready before starting Grafana,
      # so datasource connections don't race an unready backend.
      depends_on = ["gmp-frontend"]

      ports {
        container_port = 3000
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
      }

      env {
        name  = "GF_PATHS_PROVISIONING"
        value = "/mnt/grafana-provisioning/provisioning"
      }

      env {
        name  = "GF_SECURITY_ADMIN_USER"
        value = "admin"
      }

      env {
        name = "GF_SECURITY_ADMIN_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.grafana_admin_password.secret_id
            version = "latest"
          }
        }
      }

      env {
        name  = "GF_INSTALL_PLUGINS"
        value = "googlecloud-trace-datasource,googlecloud-logging-datasource,grafana-bigquery-datasource"
      }

      env {
        name  = "GF_SERVER_ROOT_URL"
        value = "%(protocol)s://%(domain)s:%(http_port)s/"
      }

      env {
        name  = "GOATOS_GRAFANA_POSTGRES_SOCKET"
        value = "/cloudsql/${google_sql_database_instance.core.connection_name}"
      }

      env {
        name  = "GOATOS_GRAFANA_POSTGRES_USER"
        value = var.grafana_postgres_datasource_user
      }

      env {
        name = "GOATOS_GRAFANA_POSTGRES_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.grafana_postgres_datasource_password.secret_id
            version = "latest"
          }
        }
      }

      volume_mounts {
        name       = "grafana-provisioning"
        mount_path = "/mnt/grafana-provisioning"
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }
    }

    # GMP query-frontend sidecar (same pattern as the otel-collector sidecars
    # in api/kernel-jobs/grafana-alloy). Grafana queries via loopback:
    # http://localhost:9090 (no TLS, no auth, no VPC connector). The standalone
    # goatos-stg-gmp-frontend Cloud Run service is retired entirely (it had the
    # same internal-ingress reachability problem that sidecars solve).
    containers {
      name = "gmp-frontend"

      image = var.gmp_frontend_image
      args = [
        "--web.listen-address=:9090",
        "--query.project-id=${var.project_id}",
      ]

      resources {
        limits = {
          cpu    = "512m"
          memory = "256Mi"
        }
      }

      startup_probe {
        initial_delay_seconds = 0
        timeout_seconds       = 1
        period_seconds        = 3
        failure_threshold     = 10

        tcp_socket {
          port = 9090
        }
      }
    }

    volumes {
      name = "grafana-provisioning"
      gcs {
        bucket    = google_storage_bucket.grafana_provisioning.name
        read_only = true
      }
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.core.connection_name]
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_service_iam_member" "grafana_operator_invoker" {
  for_each = var.observability_operator_members

  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.grafana.name
  role     = "roles/run.invoker"
  member   = each.value
}

# ---------------------------------------------------------------------------
# Grafana Alloy — Cloud Run service (public: admin-web's browser RUM SDK
# posts directly to this endpoint from the user's browser, so it cannot be
# INTERNAL_ONLY like Grafana). It carries its own OTel Collector sidecar
# (same as api/kernel jobs) rather than calling the old standalone collector
# service, for the identical internal-ingress + no-VPC-connector reason
# documented at the top of this file — Alloy's own ingress is public, but
# the hop from Alloy to the collector was still public->internal Cloud Run,
# which has the same reachability problem. `grafana_alloy`'s existing
# monitoring.metricWriter/cloudtrace.agent/logging.logWriter grants above
# now cover the sidecar too, since both containers share this one SA.
#
# SECURITY NOTE: This service runs with INGRESS_TRAFFIC_ALL + allUsers run.invoker,
# making the faro.receiver RUM endpoint publicly accessible. Mitigations:
#   1. faro.receiver CORS is restricted to https://stg.dashboard.mesha.sg in
#      alloy-config.alloy to reject browsers outside that origin.
#   2. Cloud Armor rate limiting (required future work) at the Cloud Run LB level
#      to defend against flooding attacks. See docs/observability/INFRA.md.
# This is acceptable risk for staging; production should add Cloud Armor.
# ---------------------------------------------------------------------------

resource "google_cloud_run_v2_service" "grafana_alloy" {
  name                = "goatos-stg-grafana-alloy"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = local.labels

  template {
    service_account = google_service_account.grafana_alloy.email

    scaling {
      min_instance_count = var.grafana_alloy_min_instance_count
      max_instance_count = 5
    }

    containers {
      name = "grafana-alloy"

      # Cloud Run only allows one container per revision to be the ingress
      # container (the one declaring `ports`); wait for the collector
      # sidecar to pass its startup_probe before Alloy starts so its first
      # RUM export doesn't race an unready collector.
      depends_on = ["otel-collector"]

      image = var.grafana_alloy_image
      args = [
        "run",
        "--server.http.listen-addr=0.0.0.0:12347",
        "/etc/alloy/config.alloy",
      ]

      ports {
        container_port = 12347
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }

      # Loopback to the sidecar collector in this same revision — no TLS, no
      # auth, no VPC connector needed. See alloy-config.alloy for how this is
      # consumed (sys.env(...) + tls.insecure = true).
      env {
        name  = "GOATOS_OTEL_COLLECTOR_ENDPOINT"
        value = "http://localhost:4318"
      }

      volume_mounts {
        name       = "alloy-config"
        mount_path = "/etc/alloy"
      }
    }

    containers {
      name = "otel-collector"

      image = var.otel_collector_image
      args  = ["--config=/etc/otelcol-contrib/config.yaml"]

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }

      env {
        name  = "GOOGLE_CLOUD_PROJECT"
        value = var.project_id
      }

      startup_probe {
        initial_delay_seconds = 0
        timeout_seconds       = 1
        period_seconds        = 3
        failure_threshold     = 10

        tcp_socket {
          port = 4318
        }
      }

      volume_mounts {
        name       = "collector-config"
        mount_path = "/etc/otelcol-contrib"
      }
    }

    volumes {
      name = "collector-config"
      gcs {
        bucket    = google_storage_bucket.observability_config.name
        read_only = true
      }
    }

    volumes {
      name = "alloy-config"
      gcs {
        bucket    = google_storage_bucket.observability_config.name
        read_only = true
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_service_iam_member" "grafana_alloy_public_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.grafana_alloy.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# ---------------------------------------------------------------------------
# Static config delivered to Cloud Run via GCS volume mounts (asia-south1
# single-region buckets; no image rebuild required to change collector/alloy
# config — edit the file, `terraform apply`, and force a new revision).
# ---------------------------------------------------------------------------

resource "google_storage_bucket" "observability_config" {
  name                        = "goatos-stg-observability-config"
  location                    = var.region
  force_destroy               = true
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  labels                      = local.labels

  versioning {
    enabled = true
  }

  depends_on = [google_project_service.enabled]
}

resource "google_storage_bucket_object" "otel_collector_config" {
  name   = "otel-collector-config.yaml"
  bucket = google_storage_bucket.observability_config.name
  source = "${path.module}/../../observability/otel-collector-config.yaml"
}

resource "google_storage_bucket_object" "grafana_alloy_config" {
  name   = "config.alloy"
  bucket = google_storage_bucket.observability_config.name
  source = "${path.module}/../../observability/alloy-config.alloy"
}

resource "google_storage_bucket" "grafana_provisioning" {
  name                        = "goatos-stg-grafana-provisioning"
  location                    = var.region
  force_destroy               = true
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  labels                      = local.labels

  versioning {
    enabled = true
  }

  depends_on = [google_project_service.enabled]
}

resource "google_storage_bucket_object" "grafana_datasources" {
  name   = "provisioning/datasources/datasources.yaml"
  bucket = google_storage_bucket.grafana_provisioning.name
  source = "${path.module}/../../grafana/provisioning/datasources/datasources.yaml"
}

resource "google_storage_bucket_object" "grafana_dashboards_provider" {
  name   = "provisioning/dashboards/dashboards.yaml"
  bucket = google_storage_bucket.grafana_provisioning.name
  source = "${path.module}/../../grafana/provisioning/dashboards/dashboards.yaml"
}

resource "google_storage_bucket_object" "grafana_dashboard_jsons" {
  for_each = fileset("${path.module}/../../grafana/dashboards", "*.json")

  name   = "dashboards/${each.value}"
  bucket = google_storage_bucket.grafana_provisioning.name
  source = "${path.module}/../../grafana/dashboards/${each.value}"
}

# ---------------------------------------------------------------------------
# GCS bucket IAM — least privilege read access for config volumes
# ---------------------------------------------------------------------------

# observability_config is mounted read-only by:
#   - api service (runs as runtime["api"])
#   - each kernel job (runs as runtime[job_name])
#   - grafana_alloy service (runs as grafana_alloy SA)
resource "google_storage_bucket_iam_member" "observability_config_api_reader" {
  bucket = google_storage_bucket.observability_config.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.runtime["api"].email}"
}

resource "google_storage_bucket_iam_member" "observability_config_kernel_jobs_reader" {
  for_each = local.otel_collector_telemetry_producers

  bucket = google_storage_bucket.observability_config.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

resource "google_storage_bucket_iam_member" "observability_config_grafana_alloy_reader" {
  bucket = google_storage_bucket.observability_config.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.grafana_alloy.email}"
}

# grafana_provisioning is mounted read-only by:
#   - grafana service (runs as grafana SA)
resource "google_storage_bucket_iam_member" "grafana_provisioning_grafana_reader" {
  bucket = google_storage_bucket.grafana_provisioning.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.grafana.email}"
}
