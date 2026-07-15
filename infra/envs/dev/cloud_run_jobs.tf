locals {
  backend_image                   = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/backend:${var.backend_image_tag}"
  migration_image                 = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/migrate:${var.migration_image_tag}"
  run_job_api_base                = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${var.region}/jobs"
  notification_dispatcher_run_url = "${local.run_job_api_base}/goatos-dev-notification-dispatcher:run"

  kernel_jobs = {
    # outbox_relay, domain_event_consumer, domain_event_processed_sweeper, and
    # vaccination_generator were consolidated into the kernel-worker SERVICE
    # (infra/envs/dev/cloud_run_worker.tf). Their scheduled jobs are retired so
    # the worker and a job can never run the same stage concurrently. See
    # docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
    #
    # process_integrity_projector, vaccination_execution_projector,
    # vaccination_operations_projector, and vaccination_projection_worker were removed
    # here (KERN-001 follow-up cleanup) -- see the matching comment in
    # infra/envs/stg/cloud_run_jobs.tf for why: their binaries and backing tables were
    # already deleted on main by commit cb6fd35e (migration 000187), leaving these four
    # job blocks dangling on a nonexistent Dockerfile binary.
    # obligation_sweeper, notification_dispatcher, inventory_batch_reconciler,
    # idempotency_key_sweeper, and sop_review_fanout_retry were consolidated into
    # the kernel-worker SERVICE (cloud_run_worker.tf). The near-term Cloud Tasks
    # fast path is retired with them (see cloud_tasks.tf): notifications stay
    # durable in notification_requests and drain via the worker's 1-minute
    # NotificationDispatcherStage.
    partition_maintainer = {
      name                = "goatos-dev-partition-maintainer"
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
  name                = "goatos-dev-migrate"
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
          value = "dev"
        }

        env {
          name  = "GOATOS_ALLOW_DEV_CLOUDSQL_TARGET"
          value = "true"
        }

        env {
          name  = "GOATOS_DEV_CLOUDSQL_CONNECTION_NAME"
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
  name                = "goatos-dev-outbox-dlq"
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
          value = var.dev_tenant_id
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
        image   = local.backend_image
        command = each.value.command
        args    = each.value.args

        resources {
          limits = {
            cpu    = each.value.cpu
            memory = each.value.memory
          }
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
  description = "Runs ${each.value.name} for the Goat OS dev operational kernel."
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
