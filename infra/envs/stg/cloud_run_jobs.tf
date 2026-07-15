locals {
  backend_image                   = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/backend:${var.backend_image_tag}"
  migration_image                 = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/migrate:${var.migration_image_tag}"
  admin_web_image                 = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/admin-web:${var.admin_web_image_tag}"
  run_job_api_base                = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${var.region}/jobs"
  notification_dispatcher_run_url = "${local.run_job_api_base}/goatos-stg-notification-dispatcher:run"

  kernel_jobs = {
    # outbox_relay, domain_event_consumer, domain_event_processed_sweeper, and
    # vaccination_generator were consolidated into the long-running kernel-worker
    # SERVICE (infra/envs/stg/cloud_run_worker.tf) as its fast-lane / continuous
    # / housekeeping / generation stages. Their scheduled jobs are retired so the
    # worker and a job can never run the same stage concurrently. See
    # docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
    #
    # process_integrity_projector, vaccination_execution_projector,
    # vaccination_operations_projector, and vaccination_projection_worker were
    # removed here (KERN-001 follow-up cleanup): their binaries
    # (process-integrity-projection-recompute, vaccination-execution-projection-recompute,
    # vaccination-operations-projection-recompute, vaccination-projection-worker) and the
    # dirty-scope/shed-projection tables they depended on were already deleted on main by
    # commit cb6fd35e ("feat(kernel): drop the 4 non-calendar screen projections + projector
    # code (U7)", migration 000187) -- these four job blocks were left dangling, referencing
    # binaries that do not exist in backend/Dockerfile, so every scheduled execution failed
    # to start (container ENTRYPOINT not found). tools/agent-hooks/check-deployed-job-flags.mjs
    # (`make deployed-job-flags-guard`) now fails CI if a job like this is reintroduced without
    # a matching Dockerfile-built binary.
    # obligation_sweeper, notification_dispatcher, inventory_batch_reconciler,
    # idempotency_key_sweeper, and sop_review_fanout_retry were consolidated into
    # the kernel-worker SERVICE (cloud_run_worker.tf) — the 5-minute operational
    # and 1-minute fast lanes. The near-term Cloud Tasks fast path that the
    # obligation sweeper used to invoke the notification-dispatcher job for
    # sub-minute delivery is retired with those jobs (see cloud_tasks.tf):
    # notifications stay durable in notification_requests and drain via the
    # worker's 1-minute NotificationDispatcherStage.
    partition_maintainer = {
      name                = "goatos-stg-partition-maintainer"
      service_account_key = "partition_maintainer"
      command             = ["/app/bin/partition-maintainer"]
      args                = ["-timeout=60s", "-months-ahead=12"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "11 0 * * *"
      env                 = {}
    }
  }
}

resource "google_cloud_run_v2_job" "migrate" {
  name                = "goatos-stg-migrate"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.runtime["migrate"].email
      timeout         = "900s"
      max_retries     = 0

      containers {
        image   = local.migration_image
        command = ["/app/bin/migrate"]
        args    = ["-timeout=10m"]

        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
          }
        }

        env {
          name  = "GOATOS_ENV"
          value = "stg"
        }

        env {
          name  = "GOATOS_ALLOW_STG_CLOUDSQL_TARGET"
          value = "true"
        }

        env {
          name  = "GOATOS_STG_CLOUDSQL_CONNECTION_NAME"
          value = google_sql_database_instance.core.connection_name
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

        volume_mounts {
          name       = "cloudsql"
          mount_path = "/cloudsql"
        }
      }

      volumes {
        name = "cloudsql"
        cloud_sql_instance {
          instances = [google_sql_database_instance.core.connection_name]
        }
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_job" "outbox_dlq" {
  name                = "goatos-stg-outbox-dlq"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.runtime["outbox_dlq"].email
      timeout         = "120s"
      max_retries     = 0

      containers {
        image   = local.backend_image
        command = ["/app/bin/outbox-dlq"]
        args    = ["-mode=list", "-status=dead_letter", "-limit=100", "-timeout=60s"]

        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
          }
        }

        env {
          name  = "GOATOS_TENANT_ID"
          value = var.stg_tenant_id
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

        volume_mounts {
          name       = "cloudsql"
          mount_path = "/cloudsql"
        }
      }

      volumes {
        name = "cloudsql"
        cloud_sql_instance {
          instances = [google_sql_database_instance.core.connection_name]
        }
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_job" "kernel" {
  for_each = local.kernel_jobs

  name                = each.value.name
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.runtime[each.value.service_account_key].email
      timeout         = each.value.timeout
      max_retries     = 1

      containers {
        # Declared FIRST: Cloud Run Jobs consider a multi-container task
        # execution complete when this first-listed container exits — the
        # otel-collector sidecar below is then sent SIGTERM regardless of its
        # own state. Keeping the app container first preserves existing job
        # success/failure semantics untouched by adding the sidecar.
        name = "app"

        # Wait for the collector sidecar's startup_probe before running, so
        # the job's own OTLP exports (best-effort; SetupTelemetry never fails
        # the process) have somewhere to land from the first line of work.
        depends_on = ["otel-collector"]

        image   = local.backend_image
        command = each.value.command
        args    = each.value.args

        resources {
          limits = {
            cpu    = each.value.cpu
            memory = each.value.memory
          }
        }

        env {
          name  = "GOATOS_ENV"
          value = "stg"
        }

        env {
          name  = "GOATOS_OBS_SINK"
          value = "otlp"
        }

        # OTLP/HTTP export to the OTel Collector sidecar container in this
        # same task, over loopback — see the sidecar decision documented at
        # the top of observability.tf and in docs/observability/INFRA.md.
        # Short-lived Jobs force-flush on exit via SetupTelemetry's deferred
        # shutdown, which synchronously posts to this loopback endpoint
        # before the app container exits — see the residual-risk note in
        # INFRA.md about the sidecar's own flush-on-SIGTERM window.
        env {
          name  = "GOATOS_OTLP_ENDPOINT"
          value = "http://localhost:4318"
        }

        env {
          name  = "GOATOS_TRACE_SAMPLE_RATIO"
          value = var.trace_sample_ratio
        }

        dynamic "env" {
          for_each = each.value.env
          content {
            name  = env.key
            value = env.value
          }
        }

        dynamic "env" {
          for_each = try(each.value.secret_env, {})
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = google_secret_manager_secret.container[env.value].secret_id
                version = "latest"
              }
            }
          }
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

        volume_mounts {
          name       = "cloudsql"
          mount_path = "/cloudsql"
        }
      }

      # OTel Collector sidecar (see the header comment in observability.tf
      # and docs/observability/INFRA.md "Sidecar collector decision").
      # Declared SECOND so it never determines this task's success/failure —
      # only the "app" container's exit code does (see the comment on that
      # container above). Cloud Run terminates this sidecar automatically
      # once "app" exits.
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
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_job_iam_member" "scheduler_invoker" {
  for_each = google_cloud_run_v2_job.kernel

  project  = var.project_id
  location = var.region
  name     = each.value.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.runtime["scheduler"].email}"
}

resource "google_cloud_scheduler_job" "kernel" {
  for_each = local.kernel_jobs

  name        = "${each.value.name}-schedule"
  description = "Runs ${each.value.name} for the Goat OS staging operational kernel."
  region      = var.region
  schedule    = each.value.schedule
  time_zone   = "Asia/Kolkata"

  attempt_deadline = "320s"

  retry_config {
    retry_count          = 3
    min_backoff_duration = "10s"
    max_backoff_duration = "300s"
    max_doublings        = 3
  }

  http_target {
    http_method = "POST"
    uri         = "${local.run_job_api_base}/${each.value.name}:run"
    body        = base64encode("{}")
    headers = {
      "Content-Type" = "application/json"
    }
    oauth_token {
      service_account_email = google_service_account.runtime["scheduler"].email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }

  depends_on = [
    google_cloud_run_v2_job_iam_member.scheduler_invoker,
    google_service_account_iam_member.cloudscheduler_scheduler_token_creator,
  ]
}
