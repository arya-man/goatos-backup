locals {
  backend_image   = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/backend:${var.backend_image_tag}"
  migration_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/migrate:${var.migration_image_tag}"
  admin_web_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/admin-web:${var.admin_web_image_tag}"
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

# ---------------------------------------------------------------------------
# Feed direction dispatch clock: ISSUE, AMEND and LOCK the daily feed sheet.
# ---------------------------------------------------------------------------
#
# WHY THIS EXISTS. Until these run, a feed sheet is only ever a LIVE PREVIEW: the API regenerates it
# from current counts on every read. The counts projection deliberately includes authorized-but-
# unexecuted shiftings with ZERO lead time, so a movement approved at 09:00 changes a shed's
# quantity on the next screen open -- after the operator has already packed the bags against the
# morning figure. ISSUING is what freezes the sheet into feed_direction_issue_rows; every read after
# that serves the stored rows and shiftings no longer move them.
#
# THE DAY. Feed for day D is produced on D-1, so each run acts on feed_day = as-of + 1:
#
#   07:00  issue   -> tomorrow's NORMAL sheet, frozen
#   14:00  issue   -> tomorrow's EXPERIMENT sheet, frozen
#   14:00  amend   -> recompute, diff, persist only the CHANGED sheds (the one correction window)
#   15:30  lock    -> final; later changes roll to the next feed day
#
# ONE issue job serves BOTH workflows because the command self-gates on the authored clock
# (app/lifecycle_schedule.go: `if asOf.Before(issueAt) { return nil, nil }`). Fired at 07:00 it
# issues normal and skips experiment, whose direction_time is 14:00; fired again at 14:00 it issues
# experiment and no-ops on the already-issued normal. The times are NOT hardcoded here -- they live
# in feed_schedule_config, and the cron expressions below only decide when the job WAKES UP to ask.
# A job that wakes late issues late; one that wakes early simply does nothing.
#
# All schedules are Asia/Kolkata: a feed day is an India business day (AGENTS.md time semantics).
#
# Each action is idempotent (an exact re-issue is a no-op, an unchanged amend still stamps
# amended_at, a second lock is a no-op), so max_retries = 0 is deliberate: a FAILED run is a signal
# an operator must see, not something an automatic retry should paper over. Cloud Scheduler's own
# retry_config covers transient invocation failure.

resource "google_cloud_run_v2_job" "feed_direction_issue" {
  name                = "goatos-stg-feed-direction-issue"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.runtime["feed_direction"].email
      timeout         = "300s"
      max_retries     = 0

      containers {
        image   = local.backend_image
        command = ["/app/bin/feed-direction-issue"]
        args    = ["-action=issue", "-workflow=both", "-timeout=120s"]

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
          name  = "GOATOS_ENV"
          value = "stg"
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

resource "google_cloud_run_v2_job" "feed_direction_amend" {
  name                = "goatos-stg-feed-direction-amend"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.runtime["feed_direction"].email
      timeout         = "300s"
      max_retries     = 0

      containers {
        image   = local.backend_image
        command = ["/app/bin/feed-direction-issue"]
        args    = ["-action=amend", "-workflow=both", "-timeout=120s"]

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
          name  = "GOATOS_ENV"
          value = "stg"
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

resource "google_cloud_run_v2_job" "feed_direction_lock" {
  name                = "goatos-stg-feed-direction-lock"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.runtime["feed_direction"].email
      timeout         = "300s"
      max_retries     = 0

      containers {
        image   = local.backend_image
        command = ["/app/bin/feed-direction-issue"]
        args    = ["-action=lock", "-workflow=both", "-timeout=120s"]

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
          name  = "GOATOS_ENV"
          value = "stg"
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

# The scheduler identity may run these three jobs and nothing else -- granted per job rather than at
# project level, so a job added later is not automatically triggerable by it.
resource "google_cloud_run_v2_job_iam_member" "feed_direction_scheduler_invoker" {
  for_each = {
    issue = google_cloud_run_v2_job.feed_direction_issue.name
    amend = google_cloud_run_v2_job.feed_direction_amend.name
    lock  = google_cloud_run_v2_job.feed_direction_lock.name
  }

  project  = var.project_id
  location = var.region
  name     = each.value
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.runtime["scheduler"].email}"
}

# One entry per WAKE-UP, not per job: `issue` is triggered TWICE because normal (07:00) and
# experiment (14:00) have different authored direction_times and the job skips whichever is not due.
resource "google_cloud_scheduler_job" "feed_direction" {
  for_each = {
    issue_normal = {
      job      = google_cloud_run_v2_job.feed_direction_issue.name
      schedule = "0 7 * * *"
      note     = "normal direction_time 07:00 -- experiment is not due yet and is skipped"
    }
    issue_experiment = {
      job      = google_cloud_run_v2_job.feed_direction_issue.name
      schedule = "0 14 * * *"
      note     = "experiment direction_time 14:00 -- normal is already issued and no-ops"
    }
    amend = {
      job      = google_cloud_run_v2_job.feed_direction_amend.name
      schedule = "0 14 * * *"
      note     = "correction_time 14:00, both workflows"
    }
    lock = {
      job      = google_cloud_run_v2_job.feed_direction_lock.name
      schedule = "30 15 * * *"
      note     = "transport_time 15:30 -- aligned with the MaterializeTransportTasks gate (migration 000104)"
    }
  }

  name        = "${each.value.job}-${each.key}"
  description = "Goat OS staging feed direction dispatch clock (${each.value.note})."
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
    uri         = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${var.region}/jobs/${each.value.job}:run"
    body        = base64encode("{}")
    headers = {
      "Content-Type" = "application/json"
    }
    oauth_token {
      service_account_email = google_service_account.runtime["scheduler"].email
      scope                 = "https://www.googleapis.com/auth/cloud-platform"
    }
  }

  depends_on = [google_cloud_run_v2_job_iam_member.feed_direction_scheduler_invoker]
}
