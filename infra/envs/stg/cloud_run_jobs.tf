locals {
  backend_image                   = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/backend:${var.backend_image_tag}"
  migration_image                 = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/migrate:${var.migration_image_tag}"
  admin_web_image                 = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/admin-web:${var.admin_web_image_tag}"
  run_job_api_base                = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${var.region}/jobs"
  notification_dispatcher_run_url = "${local.run_job_api_base}/goatos-stg-notification-dispatcher:run"

  kernel_jobs = {
    outbox_relay = {
      name                = "goatos-stg-outbox-relay"
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
    domain_event_consumer = {
      name                = "goatos-stg-domain-event-consumer"
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
    domain_event_processed_sweeper = {
      name                = "goatos-stg-domain-event-processed-sweeper"
      service_account_key = "domain_event_processed_sweeper"
      command             = ["/app/bin/domain-event-processed-sweeper"]
      args                = ["-timeout=45s", "-limit=1000", "-retention=336h"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "37 * * * *"
      env = {
        GOATOS_TENANT_ID = var.stg_tenant_id
      }
    }
    vaccination_generator = {
      name                = "goatos-stg-vaccination-generator"
      service_account_key = "vaccination_generator"
      command             = ["/app/bin/generate-vaccination-obligations"]
      args                = ["-timeout=120s"]
      timeout             = "180s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "0 * * * *"
      env = {
        GOATOS_TENANT_ID = var.stg_tenant_id
      }
    }
    obligation_sweeper = {
      name                = "goatos-stg-obligation-sweeper"
      service_account_key = "obligation_sweeper"
      command             = ["/app/bin/obligation-sweeper"]
      args                = ["-timeout=60s"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOOGLE_CLOUD_PROJECT                     = var.project_id
        GOATOS_TENANT_ID                         = var.stg_tenant_id
        GOATOS_CLOUD_TASKS_PROJECT_ID            = var.project_id
        GOATOS_CLOUD_TASKS_LOCATION              = var.region
        GOATOS_CLOUD_TASKS_QUEUE_ID              = google_cloud_tasks_queue.near_term_kernel.name
        GOATOS_CLOUD_TASKS_OAUTH_SERVICE_ACCOUNT = google_service_account.runtime["cloud_tasks_enqueuer"].email
        GOATOS_NOTIFICATION_DISPATCHER_RUN_URL   = local.notification_dispatcher_run_url
      }
    }
    calendar_projector = {
      name                = "goatos-stg-calendar-projector"
      service_account_key = "calendar_projector"
      command             = ["/app/bin/calendar-vaccination-projector"]
      args                = ["-timeout=60s"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOATOS_TENANT_ID = var.stg_tenant_id
      }
    }
    calendar_reminder_sweeper = {
      name                = "goatos-stg-calendar-reminder-sweeper"
      service_account_key = "calendar_reminder_sweeper"
      command             = ["/app/bin/calendar-reminder-sweeper"]
      args                = ["-timeout=45s"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOOGLE_CLOUD_PROJECT                     = var.project_id
        GOATOS_TENANT_ID                         = var.stg_tenant_id
        GOATOS_CLOUD_TASKS_PROJECT_ID            = var.project_id
        GOATOS_CLOUD_TASKS_LOCATION              = var.region
        GOATOS_CLOUD_TASKS_QUEUE_ID              = google_cloud_tasks_queue.near_term_kernel.name
        GOATOS_CLOUD_TASKS_OAUTH_SERVICE_ACCOUNT = google_service_account.runtime["cloud_tasks_enqueuer"].email
        GOATOS_NOTIFICATION_DISPATCHER_RUN_URL   = local.notification_dispatcher_run_url
      }
    }
    calendar_escalation_sweeper = {
      name                = "goatos-stg-calendar-escalation-sweeper"
      service_account_key = "calendar_escalation_sweeper"
      command             = ["/app/bin/calendar-escalation-sweeper"]
      args                = ["-timeout=45s"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/10 * * * *"
      env = {
        GOOGLE_CLOUD_PROJECT                     = var.project_id
        GOATOS_TENANT_ID                         = var.stg_tenant_id
        GOATOS_CLOUD_TASKS_PROJECT_ID            = var.project_id
        GOATOS_CLOUD_TASKS_LOCATION              = var.region
        GOATOS_CLOUD_TASKS_QUEUE_ID              = google_cloud_tasks_queue.near_term_kernel.name
        GOATOS_CLOUD_TASKS_OAUTH_SERVICE_ACCOUNT = google_service_account.runtime["cloud_tasks_enqueuer"].email
        GOATOS_NOTIFICATION_DISPATCHER_RUN_URL   = local.notification_dispatcher_run_url
      }
    }
    notification_dispatcher = {
      name                = "goatos-stg-notification-dispatcher"
      service_account_key = "notification_dispatcher"
      command             = ["/app/bin/notification-dispatcher"]
      args                = ["-timeout=45s", "-limit=100"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "* * * * *"
      env = {
        GOOGLE_CLOUD_PROJECT  = var.project_id
        GOATOS_FCM_PROJECT_ID = var.project_id
        GOATOS_TENANT_ID      = var.stg_tenant_id
      }
      secret_env = {
        GOATOS_SLACK_WEBHOOK_URL    = "notification_slack_webhook_url"
        GOATOS_INCIDENT_WEBHOOK_URL = "notification_incident_webhook_url"
      }
    }
    inventory_batch_reconciler = {
      name                = "goatos-stg-inventory-batch-reconciler"
      service_account_key = "inventory_batch_reconciler"
      command             = ["/app/bin/inventory-batch-reconciler"]
      args                = ["-timeout=60s", "-limit=100"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOATOS_TENANT_ID = var.stg_tenant_id
      }
    }
    idempotency_key_sweeper = {
      name                = "goatos-stg-idempotency-key-sweeper"
      service_account_key = "idempotency_key_sweeper"
      command             = ["/app/bin/idempotency-key-sweeper"]
      args                = ["-timeout=45s", "-limit=1000"]
      timeout             = "90s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "17 * * * *"
      env = {
        GOATOS_TENANT_ID = var.stg_tenant_id
      }
    }
    sop_review_fanout_retry = {
      name                = "goatos-stg-sop-review-fanout-retry"
      service_account_key = "sop_review_fanout_retry"
      command             = ["/app/bin/sop-review-fanout-retry"]
      args                = ["-timeout=60s", "-limit=100"]
      timeout             = "120s"
      memory              = "512Mi"
      cpu                 = "1"
      schedule            = "*/5 * * * *"
      env = {
        GOATOS_TENANT_ID = var.stg_tenant_id
      }
    }
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
          value = "gcm"
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

resource "google_cloud_run_v2_job_iam_member" "cloud_tasks_notification_dispatcher_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_job.kernel["notification_dispatcher"].name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.runtime["cloud_tasks_enqueuer"].email}"
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
