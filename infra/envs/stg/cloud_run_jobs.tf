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
