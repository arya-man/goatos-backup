# Kernel worker for dev — the single consolidated long-running service that
# replaces the retired per-stage scheduled Cloud Run Jobs (see
# docs/decisions/operational-kernel-5k-50k-scale-envelope.md). Unlike stg, dev
# has no OTel collector sidecar and no api service: telemetry is OPTIONAL here
# (default stdout_json sink — SetupTelemetry no-ops without an OTLP endpoint), so
# the absence of an OTel collector never blocks the worker from booting. Deployed
# as a Cloud Run SERVICE at min=2/max=2 with CPU always allocated, health-only on
# $PORT. Deploy is intentionally gated: var.dev_sweeper_actor_id has no default,
# so plan/apply fails closed until a real goatos-dev operator UUID is supplied.
resource "google_cloud_run_v2_service" "kernel_worker" {
  name                = "goatos-kernel-worker-dev"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_INTERNAL_ONLY"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["kernel_worker"].email

    scaling {
      min_instance_count = 2
      max_instance_count = 2
    }

    containers {
      name = "kernel-worker"

      image   = local.backend_image
      command = ["/app/bin/kernel-worker"]

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
        cpu_idle = false
      }

      env {
        name  = "GOATOS_ENV"
        value = "dev"
      }

      env {
        name  = "GOATOS_HEALTH_ADDR"
        value = ":8080"
      }

      env {
        name  = "GOATOS_TENANT_ID"
        value = var.dev_tenant_id
      }

      env {
        name  = "GOATOS_PG_QUERY_TIMEOUT"
        value = "30s"
      }

      env {
        name  = "GOATOS_SWEEPER_ACTOR_ID"
        value = var.dev_sweeper_actor_id
      }

      env {
        name  = "GOOGLE_CLOUD_PROJECT"
        value = var.project_id
      }

      env {
        name  = "GOATOS_OUTBOX_PUBLISHER"
        value = "pubsub"
      }

      env {
        name  = "GOATOS_OUTBOX_PUBSUB_TOPIC_ID"
        value = google_pubsub_topic.outbox_events.name
      }

      env {
        name  = "GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID"
        value = google_pubsub_subscription.domain_events.name
      }

      env {
        name  = "GOATOS_DOMAIN_EVENT_SCHEMA_PATH"
        value = "/app/contracts/jsonschema/domain-event-envelope.schema.json"
      }

      env {
        name  = "GOATOS_PUBSUB_PROJECT_ID"
        value = var.project_id
      }

      env {
        name  = "GOATOS_FCM_PROJECT_ID"
        value = var.project_id
      }

      # Telemetry optional in dev: no GOATOS_OBS_SINK/OTLP endpoint -> default
      # stdout_json, no sidecar. The near-term Cloud Tasks path is likewise
      # omitted (see cloud_tasks.tf) — notifications drain via the worker's
      # 1-minute fast-lane stage.

      env {
        name = "DATABASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["database_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_SLACK_WEBHOOK_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["notification_slack_webhook_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_INCIDENT_WEBHOOK_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["notification_incident_webhook_url"].secret_id
            version = "latest"
          }
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      startup_probe {
        initial_delay_seconds = 10
        timeout_seconds       = 5
        period_seconds        = 10
        failure_threshold     = 6

        http_get {
          path = "/readyz"
          port = 8080
        }
      }

      liveness_probe {
        initial_delay_seconds = 30
        timeout_seconds       = 5
        period_seconds        = 30
        failure_threshold     = 3

        http_get {
          path = "/livez"
          port = 8080
        }
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
