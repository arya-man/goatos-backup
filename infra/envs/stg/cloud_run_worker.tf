# Kernel worker — the single consolidated long-running service that replaces the
# retired per-stage scheduled Cloud Run Jobs (see
# docs/decisions/operational-kernel-5k-50k-scale-envelope.md). Deployed as a
# Cloud Run SERVICE (not a Job) at min=2/max=2 with CPU always allocated so the
# supervisor's continuous domain-event consumer and every cadence stage run
# without scale-to-zero. Health/lifecycle only on $PORT — /livez + /readyz never
# trigger stage work (kernel-worker's own health listener). Advisory locks keep
# the two instances from double-running a stage, and one instance takes over on
# crash/rolling deploy (name-derived hashtext lock keys, stable across versions).
resource "google_cloud_run_v2_service" "kernel_worker" {
  name                = "goatos-kernel-worker-stg"
  location            = var.region
  deletion_protection = false
  # Internal only: the worker serves no product traffic, just health probes.
  ingress = "INGRESS_TRAFFIC_INTERNAL_ONLY"
  labels  = local.labels

  template {
    service_account = google_service_account.runtime["kernel_worker"].email

    # HA pair. min == max == 2: exactly two always-warm instances, one active
    # per stage (advisory-lock serialized) and one hot standby for failover.
    scaling {
      min_instance_count = 2
      max_instance_count = 2
    }

    containers {
      name = "kernel-worker"

      # Wait for the OTel Collector sidecar's startup_probe before starting the
      # supervisor, so stage telemetry has somewhere to land from the first tick.
      depends_on = ["otel-collector"]

      image = local.backend_image
      # Override the image's ENTRYPOINT (/app/bin/api) — run the worker binary.
      # -timeout=0s (no forced run deadline; runs for the service lifetime) is
      # explicit so it matches deploy/runtime/workers.json + make e2e-parity.
      command = ["/app/bin/kernel-worker"]
      args    = ["-timeout=0s"]

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
        # CPU always allocated: the worker does background work between (and
        # outside) requests, so it must not be throttled to zero when idle.
        cpu_idle = false
      }

      env {
        name  = "GOATOS_ENV"
        value = "stg"
      }

      # Health/lifecycle listener for Cloud Run startup + liveness probes.
      env {
        name  = "GOATOS_HEALTH_ADDR"
        value = ":8080"
      }

      env {
        name  = "GOATOS_TENANT_ID"
        value = var.stg_tenant_id
      }

      env {
        name  = "GOATOS_WORKER_STAGES_ENABLED"
        value = "true"
      }

      env {
        name  = "GOATOS_PG_QUERY_TIMEOUT"
        value = "30s"
      }

      # Preserve the retired jobs' per-stage batch budgets (KERN-REV-05): the
      # consolidated stages must not silently fall back to the one-shot default
      # of 50. Outbox relay drained 500/run and the notification dispatcher 100.
      env {
        name  = "GOATOS_OUTBOX_LIMIT"
        value = "500"
      }

      env {
        name  = "GOATOS_NOTIFICATION_LIMIT"
        value = "100"
      }

      # Obligation sweeper stage: audited actor for SOP-task creation.
      env {
        name  = "GOATOS_SWEEPER_ACTOR_ID"
        value = var.stg_sweeper_actor_id
      }

      # Outbox relay stage: publish domain events to Pub/Sub.
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

      # Domain-event consumer stage: subscribe + validate envelopes.
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

      # Notification dispatcher stage: FCM sender project.
      env {
        name  = "GOATOS_FCM_PROJECT_ID"
        value = var.project_id
      }

      env {
        name  = "GOATOS_FCM_DEFAULT_TOPIC"
        value = "goatos-stg-all"
      }

      # NOTE: the near-term Cloud Tasks path (GOATOS_CLOUD_TASKS_* +
      # GOATOS_NOTIFICATION_DISPATCHER_RUN_URL) is intentionally OMITTED. That
      # optimization invoked the retired notification-dispatcher Job for
      # sub-minute delivery; with the job gone, notifications stay durable in
      # notification_requests and are drained idempotently by this worker's
      # 1-minute fast-lane NotificationDispatcherStage (<=1-min added latency,
      # no loss). taskqueue.ConfigFromEnv() self-disables cleanly when unset.

      # Observability: OTLP export to the sidecar over loopback.
      env {
        name  = "GOATOS_OBS_SINK"
        value = "otlp"
      }

      env {
        name  = "GOATOS_OTLP_ENDPOINT"
        value = "http://localhost:4318"
      }

      env {
        name  = "GOATOS_TRACE_SAMPLE_RATIO"
        value = var.trace_sample_ratio
      }

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

      # Startup probe: the revision is not ready until /readyz reports the
      # supervisor started + DB reachable.
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

      # Liveness probe: restart a wedged instance. Uses /livez (process alive),
      # not /readyz, so a transient DB blip does not force a restart loop.
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

    # OTel Collector sidecar — mirrors the api service (see observability.tf and
    # docs/observability/INFRA.md "Sidecar collector decision"). No ports, so it
    # is reachable only via loopback from the worker container above.
    containers {
      name = "otel-collector"

      image = var.otel_collector_image
      args  = ["--config=/etc/otelcol-contrib/config.yaml"]

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        cpu_idle = false
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
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.core.connection_name]
      }
    }

    volumes {
      name = "collector-config"
      gcs {
        bucket    = google_storage_bucket.observability_config.name
        read_only = true
      }
    }
  }

  depends_on = [google_project_service.enabled]
}
