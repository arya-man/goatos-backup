resource "google_cloud_run_v2_service" "api" {
  name                = "goatos-api-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["api"].email

    scaling {
      min_instance_count = 0
      max_instance_count = 10
    }

    containers {
      image = local.backend_image

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
      }

      env {
        name  = "GOATOS_ENV"
        value = "stg"
      }

      env {
        name  = "GOATOS_AUTH_MODE"
        value = "jwks"
      }

      env {
        name  = "GOATOS_OBS_SINK"
        value = "gcm"
      }

      env {
        name  = "GOATOS_MEDIA_STORAGE"
        value = "gcs"
      }

      env {
        name  = "GOATOS_GCS_BUCKET"
        value = google_storage_bucket.proof_media.name
      }

      env {
        name = "GOATOS_AUTH_ISSUER"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["auth_issuer"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_AUTH_AUDIENCE"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["auth_audience"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_AUTH_JWKS_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["auth_jwks_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_AUTH_ALLOWED_EMAILS"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["auth_allowed_emails"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["auth_session_allowed_tenant_ids"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_APPCHECK_ENFORCE"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["appcheck_enforce"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_APPCHECK_ISSUER"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["appcheck_issuer"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_APPCHECK_AUDIENCE"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["appcheck_audience"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_APPCHECK_JWKS_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["appcheck_jwks_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_BULK_IMPORT_PREVIEW_SIGNING_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["bulk_import_preview_signing_key"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_GCS_SERVICE_ACCOUNT_JSON"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["proof_gcs_service_account_json"].secret_id
            version = "latest"
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

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_service" "admin_web" {
  name                = "goatos-admin-web-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["admin_web"].email

    scaling {
      min_instance_count = 0
      max_instance_count = 5
    }

    containers {
      image = local.admin_web_image

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
      }

      env {
        name  = "GOATOS_ENV"
        value = "stg"
      }

      env {
        name  = "GOATOS_CANONICAL_DASHBOARD_HOST"
        value = var.canonical_dashboard_host
      }

      env {
        name  = "GOATOS_GOOGLE_SIGN_IN_CLIENT_ID"
        value = var.google_sign_in_client_id
      }

      env {
        name = "GOATOS_FIREBASE_WEB_CONFIG"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["firebase_web_config"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_API_BASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["admin_web_api_base_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_TENANT_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["admin_web_tenant_id"].secret_id
            version = "latest"
          }
        }
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_service_iam_member" "admin_web_public_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.admin_web.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
