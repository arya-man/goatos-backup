resource "google_cloud_run_v2_service" "api" {
  name                = "goatos-api-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["api"].email

    scaling {
      min_instance_count = 1
      max_instance_count = 10
    }

    containers {
      name = "api"

      # Wait for the OTel Collector sidecar's startup_probe before serving
      # traffic — see the sidecar decision documented at the top of
      # observability.tf and in docs/observability/INFRA.md.
      depends_on = ["otel-collector"]

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
        name  = "GOATOS_HTTP_ADDR"
        value = ":8080"
      }

      env {
        name  = "GOATOS_AUTH_MODE"
        value = "jwks"
      }

      env {
        name  = "GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS"
        value = var.stg_tenant_id
      }

      env {
        name  = "GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE"
        value = "120"
      }

      env {
        name  = "GOATOS_OBS_SINK"
        value = "otlp"
      }

      # OTLP/HTTP export to the OTel Collector sidecar container in this same
      # revision, over loopback — no VPC connector, no auth, no TLS needed
      # (see the sidecar decision documented at the top of observability.tf
      # and in docs/observability/INFRA.md "Sidecar collector decision").
      # telemetry.go's normalizeOTLPEndpoint treats "http://" as insecure, so
      # this Just Works with SetupTelemetry unchanged. SetupTelemetry no-ops
      # safely if the sidecar is unreachable, so the api never fails to boot
      # on a collector hiccup. Base URL only — the exporter appends
      # /v1/traces and /v1/metrics.
      env {
        name  = "GOATOS_OTLP_ENDPOINT"
        value = "http://localhost:4318"
      }

      env {
        name  = "GOATOS_TRACE_SAMPLE_RATIO"
        value = var.trace_sample_ratio
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

      env {
        name  = "GOATOS_PG_QUERY_TIMEOUT"
        value = "15s"
      }

      env {
        name  = "MESHA_AI_PROVIDER"
        value = "vertex"
      }

      env {
        name  = "MESHA_VERTEX_PROJECT"
        value = var.project_id
      }

      env {
        name  = "MESHA_VERTEX_LOCATION"
        value = var.region
      }

      env {
        name  = "MESHA_VERTEX_MODEL"
        value = "gemini-3.5-flash-lite"
      }

      env {
        name  = "MESHA_MCP_TOOLSET"
        value = "mesha_ceo_toolset"
      }

      env {
        name = "MESHA_MCP_DB_DSN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["mesha_ceo_readonly_db_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "MESHA_CUBE_DB_DSN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["mesha_cube_readonly_db_url"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "MESHA_CUBE_API_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["mesha_cube_api_secret"].secret_id
            version = "latest"
          }
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }
    }

    # OTel Collector sidecar (see the header comment in observability.tf and
    # docs/observability/INFRA.md "Sidecar collector decision"). Not the
    # ingress container, so it declares no `ports` and is unreachable except
    # via loopback from the api container above.
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
      min_instance_count = 1
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
          memory = "512Mi"
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
        name  = "GOATOS_API_BASE_URL"
        value = var.api_base_url
      }

      env {
        name  = "GOATOS_TENANT_ID"
        value = var.stg_tenant_id
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_service" "mcp" {
  name                = "goatos-mcp-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = merge(local.labels, { component = "external-mcp" })

  template {
    service_account = google_service_account.runtime["mcp"].email

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      name    = "mcp"
      image   = local.backend_image
      command = ["/app/bin/mcp"]

      ports {
        container_port = 8080
      }

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
        name  = "GOATOS_AUTH_MODE"
        value = "jwks"
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
        name  = "GOATOS_HTTP_ADDR"
        value = ":8080"
      }

      env {
        name  = "MESHA_MCP_PATH"
        value = "/mcp"
      }

      env {
        name  = "MESHA_MCP_UPSTREAM_URL"
        value = var.api_base_url
      }

      env {
        name  = "MESHA_MCP_PUBLIC_URL"
        value = "https://goatos-mcp-stg-awtrpmn4za-el.a.run.app"
      }

      env {
        name = "MESHA_MCP_ALLOWED_EMAILS"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["auth_allowed_emails"].secret_id
            version = "latest"
          }
        }
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

      startup_probe {
        initial_delay_seconds = 0
        timeout_seconds       = 2
        period_seconds        = 5
        failure_threshold     = 12

        http_get {
          path = "/readyz"
          port = 8080
        }
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_cloud_run_v2_service_iam_member" "mcp_public_invoker" {
  project  = var.project_id
  location = google_cloud_run_v2_service.mcp.location
  name     = google_cloud_run_v2_service.mcp.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service_iam_member" "api_public_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service_iam_member" "admin_web_public_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.admin_web.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}
