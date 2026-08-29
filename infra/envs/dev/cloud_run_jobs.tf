locals {
  backend_image                   = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/backend:${var.backend_image_tag}"
  migration_image                 = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/migrate:${var.migration_image_tag}"
  run_job_api_base                = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${var.region}/jobs"
  notification_dispatcher_run_url = "${local.run_job_api_base}/goatos-dev-notification-dispatcher:run"

  # KERN-01 SAFETY: Two-phase kernel-worker cutover.
  # Phase 1 (retire_legacy_stage_jobs = false): kernel-worker service + legacy jobs
  #   Both run for parity verification; no stage runs concurrently in job and worker.
  # Phase 2 (retire_legacy_stage_jobs = true): legacy jobs removed after service proven healthy.
  # This gate prevents a single terraform apply from destroying all 9 retired jobs before the
  # replacement service is ready, which would violate the required parity-first cutover.
  # See docs/decisions/operational-kernel-5k-50k-scale-envelope.md.

  # Legacy jobs consolidated into the kernel-worker SERVICE (cloud_run_worker.tf).
  # Gated by var.retire_legacy_stage_jobs; phase 2 apply sets flag=true to enable removal.
  legacy_stage_jobs = var.retire_legacy_stage_jobs ? {} : {
    # Retired to kernel-worker fast-lane stage (continuous domain-event consumer):
    domain_event_consumer = {
      name                = "goatos-dev-domain-event-consumer"
      service_account_key = "domain_consumer"
      command             = ["/app/bin/domain-event-consumer"]
      args                = ["-timeout=9m"]
      timeout             = "600s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/10 * * * *"
      env = {
        GOOGLE_CLOUD_PROJECT                 = var.project_id
        GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID = google_pubsub_subscription.domain_events.name
        GOATOS_DOMAIN_EVENT_SCHEMA_PATH      = "/app/contracts/jsonschema/domain-event-envelope.schema.json"
        GOATOS_PUBSUB_PROJECT_ID             = var.project_id
      }
    }
    # Retired to kernel-worker continuous housekeeping stage:
    outbox_relay = {
      name                = "goatos-dev-outbox-relay"
      service_account_key = "outbox_relay"
      command             = ["/app/bin/outbox-relay"]
      args                = ["-limit=500", "-timeout=240s"]
      timeout             = "300s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "* * * * *"
      env = {
        GOOGLE_CLOUD_PROJECT          = var.project_id
        GOATOS_OUTBOX_PUBLISHER       = "pubsub"
        GOATOS_OUTBOX_PUBSUB_TOPIC_ID = google_pubsub_topic.outbox_events.name
      }
    }
    # Retired to kernel-worker housekeeping stage:
    domain_event_processed_sweeper = {
      name                = "goatos-dev-domain-event-processed-sweeper"
      service_account_key = "domain_event_processed_sweeper"
      command             = ["/app/bin/domain-event-processed-sweeper"]
      args                = ["-timeout=45s", "-limit=1000", "-retention=336h"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "37 * * * *"
      env = {
        GOATOS_TENANT_ID = var.dev_tenant_id
      }
    }
    # Retired to kernel-worker generation stage:
    vaccination_generator = {
      name                = "goatos-dev-vaccination-generator"
      service_account_key = "vaccination_generator"
      command             = ["/app/bin/generate-vaccination-obligations"]
      args                = ["-timeout=120s"]
      timeout             = "180s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "0 * * * *"
      env = {
        GOATOS_TENANT_ID = var.dev_tenant_id
      }
    }
    # Note: obligation_sweeper and notification_dispatcher are NOT restored here because
    # their supporting infrastructure (google_cloud_tasks_queue.near_term_kernel) was
    # retired. These jobs cannot be rolled back to without rebuilding the queue. They
    # remain consolidated in the kernel-worker SERVICE only.
    # Retired to kernel-worker 5-minute operational lane:
    inventory_batch_reconciler = {
      name                = "goatos-dev-inventory-batch-reconciler"
      service_account_key = "inventory_batch_reconciler"
      command             = ["/app/bin/inventory-batch-reconciler"]
      args                = ["-timeout=60s", "-limit=100"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOATOS_TENANT_ID = var.dev_tenant_id
      }
    }
    # Retired to kernel-worker 5-minute operational lane:
    idempotency_key_sweeper = {
      name                = "goatos-dev-idempotency-key-sweeper"
      service_account_key = "idempotency_key_sweeper"
      command             = ["/app/bin/idempotency-key-sweeper"]
      args                = ["-timeout=45s", "-limit=1000"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "17 * * * *"
      env = {
        GOATOS_TENANT_ID = var.dev_tenant_id
      }
    }
    # Retired to kernel-worker 5-minute operational lane:
    sop_review_fanout_retry = {
      name                = "goatos-dev-sop-review-fanout-retry"
      service_account_key = "sop_review_fanout_retry"
      command             = ["/app/bin/sop-review-fanout-retry"]
      args                = ["-timeout=60s", "-limit=100"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOATOS_TENANT_ID = var.dev_tenant_id
      }
    }
  }

  kernel_jobs = merge(
    local.legacy_stage_jobs,
    {
      # Retained until the de-partition migration removes it (see KERN-001 cleanup note below).
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
      # Daily herd_signal_packets partition ensure + prune (migration 000201,
      # backend/cmd/herd-signals-partition-maintenance). -retention-days=14 matches the
      # documented default (docs/modules/herd-signals-system-design.md Section 3.2). Runs at
      # 00:21 IST, offset from partition_maintainer (00:11) and the other 00:xx-hour jobs so
      # they don't all hit Cloud SQL in the same minute. Dropping a partition here is
      # IRREVERSIBLE -- see docs/runbooks/herd-signals-partition-retention.md for the
      # operational contract and what healthy/unhealthy looks like.
      herd_signals_partition_maintenance = {
        name                = "goatos-dev-herd-signals-partition-maintenance"
        service_account_key = "herd_signals_partition_maintenance"
        command             = ["/app/bin/herd-signals-partition-maintenance"]
        args                = ["-timeout=90s", "-days-ahead=14", "-retention-days=14"]
        timeout             = "120s"
        memory              = "512Mi"
        cpu                 = "1"
        schedule            = "21 0 * * *"
        env                 = {}
      }
    }
  )
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
