# First-party analytics.app_events -> Cloud SQL daily summaries. The scheduled
# job reuses the backend release image. Optional legacy GA4 dataset/IAM remains
# available for explicitly configured export runs; app_events needs no GA4 link.
# BigQuery datasets remain in asia-south1 for the optional export path.

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

data "google_bigquery_dataset" "firebase_exports" {
  for_each = toset(["firebase_crashlytics", "firebase_sessions", "firebase_performance"])

  dataset_id = each.key
}

resource "google_bigquery_dataset_iam_member" "analytics_rollup_firebase_export_viewer" {
  for_each = data.google_bigquery_dataset.firebase_exports

  dataset_id = each.value.dataset_id
  role       = "roles/bigquery.dataViewer"
  member     = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

locals {
  # The backend image already contains /app/bin/analytics-rollup. Reusing the
  # release image makes Cloud Deploy keep this scheduled job on the same commit as
  # the API and kernel worker instead of depending on an independently stale tag.
  analytics_rollup_image = local.backend_image
  # Firebase export tables are app-wide diagnostics. Firebase names Android
  # app tables from the package id with periods converted to underscores and
  # the platform suffix appended; keep these explicit so the rollup never
  # silently falls back to "export unavailable" after the transfer exists.
  analytics_rollup_source_app_id       = "sg.mesha.goatos"
  analytics_rollup_crashlytics_table   = "${var.project_id}.firebase_crashlytics.sg_mesha_goatos_ANDROID"
  analytics_rollup_sessions_table      = "${var.project_id}.firebase_sessions.sg_mesha_goatos_ANDROID"
  analytics_rollup_performance_table   = "${var.project_id}.firebase_performance.sg_mesha_goatos_ANDROID"
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
        args    = ["-timeout=25m", "-source=app_events", "-lookback-days=3"]
        # Connection budget: cap this job's pool (default 10); see the api service.
        env {
          name  = "GOATOS_PG_MAX_CONNS"
          value = "2"
        }

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

        # app_events keeps 15 days hot; older received days are exported to the
        # archive bucket, verified (row count + stored size/MD5), then deleted in
        # bounded batches. Empty bucket = no archive AND no deletion.
        env {
          name  = "GOATOS_ANALYTICS_ARCHIVE_BUCKET"
          value = google_storage_bucket.analytics_archive.name
        }

        env {
          name  = "GOATOS_ANALYTICS_APP_EVENTS_RETENTION_DAYS"
          value = "15"
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
          name  = "GOATOS_ANALYTICS_SOURCE_APP_ID"
          value = local.analytics_rollup_source_app_id
        }

        env {
          name  = "GOATOS_CRASHLYTICS_BQ_TABLE"
          value = local.analytics_rollup_crashlytics_table
        }

        env {
          name  = "GOATOS_CRASHLYTICS_SESSIONS_TABLE"
          value = local.analytics_rollup_sessions_table
        }

        env {
          name  = "GOATOS_PERFORMANCE_BQ_TABLE"
          value = local.analytics_rollup_performance_table
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

# analytics.funnel_daily, journey_daily, engagement_daily and rollup_run are
# backend-migration-owned Postgres tables, not Terraform-managed schema objects.

# Shared kernel-worker operational cadence owns daily dispatch. No Cloud
# Scheduler: disposable staging topology retains the job for manual repair.
resource "google_cloud_run_v2_job_iam_member" "analytics_rollup_worker_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_job.analytics_rollup.name
  role     = "roles/run.jobsExecutorWithOverrides"
  member   = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}
resource "google_cloud_run_v2_job_iam_member" "analytics_rollup_worker_viewer" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_job.analytics_rollup.name
  role     = "roles/run.viewer"
  member   = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

# ---------------------------------------------------------------------------
# Cold archive for analytics.app_events older than the 15-day hot window.
# One gzip JSONL object per UTC received day:
#   gs://goatos-stg-analytics-archive/app_events/dt=YYYY-MM-DD/part-<sha16>.jsonl.gz
# The job only creates (ifGenerationMatch=0) and reads object metadata to
# verify; it can neither overwrite nor delete archived objects.
# ---------------------------------------------------------------------------
resource "google_storage_bucket" "analytics_archive" {
  name                        = "goatos-stg-analytics-archive"
  location                    = var.region # asia-south1, co-located with the BigQuery dataset below.
  force_destroy               = false
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  labels                      = local.labels

  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type          = "SetStorageClass"
      storage_class = "COLDLINE"
    }
  }

  lifecycle_rule {
    condition {
      age = 90
    }
    action {
      type          = "SetStorageClass"
      storage_class = "ARCHIVE"
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_storage_bucket_iam_member" "analytics_rollup_archive_creator" {
  bucket = google_storage_bucket.analytics_archive.name
  role   = "roles/storage.objectCreator"
  member = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

# Verification reads the stored object's size/MD5 (storage.objects.get) before
# any Postgres row is deleted; objectCreator alone cannot read metadata.
resource "google_storage_bucket_iam_member" "analytics_rollup_archive_viewer" {
  bucket = google_storage_bucket.analytics_archive.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.analytics_rollup.email}"
}

# Query archived events without restoring them:
#   SELECT event_name, count(*) FROM `goatos-stg.goatos_stg_analytics_rollup.app_events_archive`
#   WHERE dt BETWEEN '2026-09-01' AND '2026-09-07' GROUP BY 1
# Always filter on dt (hive partition) so BigQuery reads only those day objects.
# Coldline/Archive objects incur per-GB retrieval charges when scanned.
resource "google_bigquery_table" "app_events_archive" {
  dataset_id          = google_bigquery_dataset.analytics_rollup.dataset_id
  table_id            = "app_events_archive"
  deletion_protection = false
  labels              = local.labels

  external_data_configuration {
    source_format = "NEWLINE_DELIMITED_JSON"
    compression   = "GZIP"
    autodetect    = false
    source_uris   = ["gs://${google_storage_bucket.analytics_archive.name}/app_events/*"]

    hive_partitioning_options {
      mode                     = "CUSTOM"
      source_uri_prefix        = "gs://${google_storage_bucket.analytics_archive.name}/app_events/{dt:DATE}"
      require_partition_filter = true
    }

    json_options {
      encoding = "UTF-8"
    }

    ignore_unknown_values = true

    schema = jsonencode([
      { name = "event_id", type = "STRING", mode = "REQUIRED" },
      { name = "tenant_id", type = "STRING", mode = "NULLABLE" },
      { name = "actor_id", type = "STRING", mode = "NULLABLE" },
      { name = "device_id", type = "STRING", mode = "NULLABLE" },
      { name = "event_name", type = "STRING", mode = "REQUIRED" },
      { name = "properties", type = "JSON", mode = "NULLABLE" },
      { name = "client_event_id", type = "STRING", mode = "NULLABLE" },
      { name = "client_event_time", type = "TIMESTAMP", mode = "NULLABLE" },
      { name = "received_at", type = "TIMESTAMP", mode = "REQUIRED" },
      { name = "flavor", type = "STRING", mode = "NULLABLE" },
      { name = "app_version_name", type = "STRING", mode = "NULLABLE" },
      { name = "app_version_code", type = "INT64", mode = "NULLABLE" },
      { name = "request_id", type = "STRING", mode = "NULLABLE" },
      { name = "trace_id", type = "STRING", mode = "NULLABLE" },
      { name = "client_info", type = "JSON", mode = "NULLABLE" },
    ])
  }
}
