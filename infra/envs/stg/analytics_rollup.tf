# GA4 -> BigQuery -> Cloud SQL analytics.* rollup infra
# (docs/observability/OBSERVABILITY_DESIGN.md section 2.6).
#
# Firebase Analytics (GA4) exports raw events to BigQuery natively once
# linked in the Firebase console (a manual, per-Firebase-project step — see
# docs/observability/INFRA.md, this cannot be done from Terraform). This file
# only declares the infra shape: a BigQuery dataset for the rollup's own
# working tables/views, IAM access to the GA4-owned export dataset once its
# id is known, and the Cloud Run Job + Cloud Scheduler that runs the rollup
# daily. The rollup job's CODE (the actual BQ aggregation -> Postgres upsert
# logic) is owned by the backend lane, not this infra lane — this file
# declares infra + vars only, per the task scope.
#
# BigQuery location is pinned to asia-south1 explicitly. BigQuery datasets
# default to the "US" multi-region if `location` is left unset — that default
# would put GA4/rollup data outside India and must never happen here.

resource "google_bigquery_dataset" "analytics_rollup" {
  dataset_id  = "goatos_stg_analytics_rollup"
  location    = var.region # asia-south1 — explicit, BigQuery's own default is "US".
  description = "Working dataset for the Goat OS staging GA4->Postgres analytics rollup job (views/staging tables only; GA4's own export dataset is separate and Firebase-owned)."

  labels = local.labels

  depends_on = [google_project_service.enabled]
}

# NOTE: analytics_rollup SA does NOT need bigquery.dataEditor on the rollup
# dataset. The job reads the GA4 export dataset (dataViewer granted below) and
# writes Postgres analytics.* tables directly (Cloud SQL client role covers that).
# No intermediate BQ staging happens, so no editor role is needed.

resource "google_project_iam_member" "analytics_rollup_job_user" {
  project = var.project_id
  role    = "roles/bigquery.jobUser"
  member  = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

resource "google_project_iam_member" "analytics_rollup_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

resource "google_secret_manager_secret_iam_member" "analytics_rollup_database_url_accessor" {
  secret_id = google_secret_manager_secret.container["database_url"].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

# GA4's BigQuery export dataset is created and named by Firebase itself
# (typically `analytics_<GA4_property_id>`) only after the manual console
# linking step below is completed. Until `var.ga4_export_dataset_id` is
# supplied, this whole block is skipped (count = 0) rather than failing
# `terraform plan` against a dataset that does not exist yet.
data "google_bigquery_dataset" "ga4_export" {
  count      = var.ga4_export_dataset_id != "" ? 1 : 0
  dataset_id = var.ga4_export_dataset_id
}

resource "google_bigquery_dataset_iam_member" "analytics_rollup_ga4_export_viewer" {
  count      = var.ga4_export_dataset_id != "" ? 1 : 0
  dataset_id = data.google_bigquery_dataset.ga4_export[0].dataset_id
  role       = "roles/bigquery.dataViewer"
  member     = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

locals {
  analytics_rollup_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/analytics-rollup:${var.analytics_rollup_image_tag}"
}

resource "google_cloud_run_v2_job" "analytics_rollup" {
  name                = "goatos-stg-analytics-rollup"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.analytics_rollup.email
      timeout         = "1800s"
      max_retries     = 1

      containers {
        image   = local.analytics_rollup_image
        command = ["/app/bin/analytics-rollup"]
        args    = ["-timeout=25m"]

        resources {
          limits = {
            cpu    = "1"
            memory = "1Gi"
          }
        }

        env {
          name  = "GOOGLE_CLOUD_PROJECT"
          value = var.project_id
        }

        env {
          name  = "GOATOS_TENANT_ID"
          value = var.stg_tenant_id
        }

        env {
          name  = "GOATOS_BQ_LOCATION"
          value = var.region
        }

        env {
          name  = "GOATOS_GA4_EXPORT_DATASET"
          value = var.ga4_export_dataset_id
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

resource "google_cloud_run_v2_job_iam_member" "analytics_rollup_scheduler_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_job.analytics_rollup.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.runtime["scheduler"].email}"
}

resource "google_cloud_scheduler_job" "analytics_rollup" {
  name        = "goatos-stg-analytics-rollup-schedule"
  description = "Runs the GA4->BigQuery->Postgres analytics rollup for Goat OS staging."
  region      = var.region
  schedule    = var.analytics_rollup_schedule
  time_zone   = "Asia/Kolkata"

  attempt_deadline = "1800s"

  retry_config {
    retry_count          = 2
    min_backoff_duration = "60s"
    max_backoff_duration = "600s"
    max_doublings        = 2
  }

  http_target {
    http_method = "POST"
    uri         = "https://run.googleapis.com/v2/projects/${var.project_id}/locations/${var.region}/jobs/${google_cloud_run_v2_job.analytics_rollup.name}:run"
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
    google_cloud_run_v2_job_iam_member.analytics_rollup_scheduler_invoker,
    google_service_account_iam_member.cloudscheduler_scheduler_token_creator,
  ]
}

# NOTE: Cloud SQL has no Terraform-managed "schema" resource. The
# `analytics` Postgres schema (and its rollup tables — mobile_funnel_rollup,
# mobile_app_start_rollup, mobile_screen_render_rollup,
# mobile_crash_free_rollup, referenced by the Mobile dashboard) is created by
# a backend-owned SQL migration, the same way every other Goat OS schema
# ships. This file only grants the infra-level IAM/secret access the rollup
# job needs to reach Cloud SQL; it does not create the schema itself. See
# docs/observability/INFRA.md.
