resource "google_cloud_run_v2_service" "api" {
  name                = "goatos-api-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["api"].email
    max_instance_request_concurrency = 10

    scaling {
      min_instance_count = 1
      max_instance_count = 4
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
        name  = "GOATOS_ANALYTICS_MAX_IN_FLIGHT"
        value = "1"
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
        value = "global"
      }

      env {
        name  = "MESHA_VERTEX_MODEL"
        value = "gemini-3.8-flash"
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
    # Ask Mesha answers (and live tag watches, max 30 min) stream through /api/ceo-ai/ask;
    # the default 300s would cut them off. Must stay >= the agent's 30-min watch cap + answer time.
    timeout = "2100s"

    scaling {
      min_instance_count = 1
      max_instance_count = 2
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

      # Server-Action redirect() makes Next self-fetch the redirect target to stream it back in the
      # action response. Without this, the self-fetch goes to http://<public host>, the load
      # balancer 301s http->https, and Node's fetch drops the Cookie header on that cross-origin
      # redirect — the request arrives sessionless, the auth middleware bounces it to /login, and
      # every verifier approve / approvals decision flashed a logout/login (incident 2026-08-18).
      # Pointing the self-fetch at the container itself keeps the session cookie intact; the
      # matching loopback-host exemption lives in apps/admin-web/proxy.ts.
      env {
        name  = "__NEXT_PRIVATE_ORIGIN"
        value = "http://127.0.0.1:8080"
      }

      env {
        name  = "GOATOS_GOOGLE_SIGN_IN_CLIENT_ID"
        value = var.google_sign_in_client_id
      }

      env {
        name  = "NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED"
        value = "1"
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
        name  = "GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY"
        value = var.firebase_web_push_vapid_key
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

resource "google_cloud_run_v2_service" "analytics_events" {
  name                = "goatos-analytics-events-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = merge(local.labels, { lane = "analytics-events" })

  template {
    service_account = google_service_account.runtime["analytics_events"].email
    max_instance_request_concurrency = 20

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      name  = "analytics-events"
      image = local.backend_image

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
        name  = "GOATOS_HTTP_ADDR"
        value = ":8080"
      }

      env {
        name  = "GOATOS_API_ROUTE_MODE"
        value = "events"
      }

      env {
        name  = "GOATOS_AUTH_MODE"
        value = "jwks"
      }

      # api bootstrap builds proof storage before the event-only route branch,
      # so the isolated event lane must carry the same required boot env.
      env {
        name  = "GOATOS_MEDIA_STORAGE"
        value = "gcs"
      }

      env {
        name  = "GOATOS_GCS_BUCKET"
        value = google_storage_bucket.proof_media.name
      }

      env {
        name  = "GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS"
        value = var.stg_tenant_id
      }

      env {
        name  = "GOATOS_ANALYTICS_MAX_IN_FLIGHT"
        value = "1"
      }

      env {
        name  = "GOATOS_PG_MAX_CONNS"
        value = "2"
      }

      env {
        name  = "GOATOS_PG_QUERY_TIMEOUT"
        value = "3s"
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

resource "google_cloud_run_v2_service_iam_member" "analytics_events_public_invoker" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.analytics_events.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service" "mcp" {
  name                = "goatos-mcp-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = merge(local.labels, { component = "external-mcp" })

  template {
    service_account = google_service_account.runtime["mcp"].email
    # ask_goatos may wait up to 240s for the Ask Mesha agent (MESHA_MCP_AGENT_URL).
    timeout = "300s"

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
        value = "https://mcp.mesha.sg"
      }

      env {
        name  = "MESHA_MCP_TENANT_ID"
        value = "00000000-0000-4000-8000-000000000001"
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

resource "google_cloud_run_v2_service" "cost_alert_bridge" {
  name                = local.cost_alert_bridge_name
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_ALL"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["cost_alert_bridge"].email

    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    containers {
      image   = local.backend_image
      command = ["/app/bin/cost-alert-bridge"]

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
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
        name = "GOATOS_COST_ALERT_SHARED_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["cost_alert_bridge_shared_token"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GOATOS_COST_ALERT_SLACK_BOT_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["cost_alert_slack_bot_token"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name  = "GOATOS_COST_ALERT_SLACK_CHANNEL_ID"
        value = "C0C1HLFEYAU"
      }

      env {
        name  = "GOATOS_COST_ALERT_STATE_BUCKET"
        value = google_storage_bucket.cost_alert_state.name
      }

      env {
        name  = "GOATOS_BILLING_QUERY_PROJECT_ID"
        value = var.project_id
      }

      env {
        name  = "GOATOS_BILLING_EXPORT_TABLE"
        value = "${var.project_id}.${google_bigquery_dataset.billing_export.dataset_id}.gcp_billing_export_v1_01FEDE_96BCB3_76D992"
      }

      env {
        name  = "GOATOS_BILLING_MONITORED_PROJECTS"
        # "GoatOS" is the display name of project_id goatos-stg.
        value = "goatos-stg,goatos-sheets,goatos-dev"
      }

      env {
        name  = "GOATOS_COST_ALERT_OIDC_AUDIENCE"
        value = local.cost_alert_bridge_audience
      }

      env {
        name  = "GOATOS_COST_ALERT_OIDC_EMAILS"
        value = "${google_service_account.runtime["cost_alert_bridge"].email},${google_service_account.runtime["scheduler"].email}"
      }
    }
  }

  depends_on = [
    google_project_service.enabled,
    google_bigquery_dataset.billing_export,
    google_storage_bucket_iam_member.cost_alert_state_writer,
  ]

  # The image is rolled by its own pipeline (cloudbuild.cost-alert-bridge.yaml),
  # not the Goat OS release. Terraform owns config/secrets/IAM only, so an apply
  # never reverts the bridge to an older backend image.
  lifecycle {
    ignore_changes = [template[0].containers[0].image]
  }
}

resource "google_cloud_run_v2_service_iam_member" "cost_alert_bridge_public_invoker" {
  project  = var.project_id
  location = google_cloud_run_v2_service.cost_alert_bridge.location
  name     = google_cloud_run_v2_service.cost_alert_bridge.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service_iam_member" "cost_alert_bridge_scheduler_invoker" {
  project  = var.project_id
  location = google_cloud_run_v2_service.cost_alert_bridge.location
  name     = google_cloud_run_v2_service.cost_alert_bridge.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.runtime["scheduler"].email}"
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

resource "google_secret_manager_secret_iam_member" "herd_signals_mqtt_bridge_gateway_password_accessor" {
  secret_id = "projects/${var.project_id}/secrets/herd-signals-mqtt-gateway-514060-password"
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.runtime["herd_signals_mqtt_bridge"].email}"
}

resource "google_secret_manager_secret_iam_member" "herd_signals_mqtt_bridge_ca_accessor" {
  secret_id = "projects/${var.project_id}/secrets/herd-signals-mqtt-ca-crt"
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.runtime["herd_signals_mqtt_bridge"].email}"
}

resource "google_cloud_run_v2_service" "herd_signals_mqtt_bridge" {
  name                = "goatos-herd-signals-mqtt-bridge-stg"
  location            = var.region
  deletion_protection = false
  ingress             = "INGRESS_TRAFFIC_INTERNAL_ONLY"
  labels              = local.labels

  template {
    service_account = google_service_account.runtime["herd_signals_mqtt_bridge"].email

    scaling {
      min_instance_count = 1
      max_instance_count = 1
    }

    containers {
      name    = "herd-signals-mqtt-bridge"
      image   = local.backend_image
      command = ["/app/bin/herd-signals-mqtt-bridge"]

      ports {
        container_port = 8080
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        # This is a long-running MQTT subscriber, not request-driven HTTP traffic.
        cpu_idle = false
      }

      env {
        name  = "GOATOS_ENV"
        value = "stg"
      }

      env {
        name  = "GOATOS_HEALTH_ADDR"
        value = ":8080"
      }

      env {
        name  = "HERD_SIGNALS_TENANT_ID"
        value = var.stg_tenant_id
      }

      env {
        name = "HERD_SIGNALS_MQTT_HOST"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["herd_signals_mqtt_host"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "HERD_SIGNALS_MQTT_PORT"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["herd_signals_mqtt_port"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name  = "HERD_SIGNALS_MQTT_TLS"
        value = "true"
      }

      env {
        name = "HERD_SIGNALS_MQTT_TOPIC"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["herd_signals_mqtt_topic"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "HERD_SIGNALS_MQTT_CLIENT_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["herd_signals_mqtt_client_id"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "HERD_SIGNALS_MQTT_USERNAME"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.container["herd_signals_mqtt_username"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "HERD_SIGNALS_MQTT_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = "herd-signals-mqtt-gateway-514060-password"
            version = "latest"
          }
        }
      }

      env {
        name = "HERD_SIGNALS_MQTT_CA_CERT"
        value_source {
          secret_key_ref {
            secret  = "herd-signals-mqtt-ca-crt"
            version = "latest"
          }
        }
      }

      env {
        name  = "HERD_SIGNALS_DEFAULT_GATEWAY_ID"
        value = "f130d402dcb4"
      }

      env {
        name  = "HERD_SIGNALS_MQTT_BATCH_SIZE"
        value = "50"
      }

      env {
        name  = "HERD_SIGNALS_MQTT_BATCH_INTERVAL"
        value = "2s"
      }

      env {
        name  = "HERD_SIGNALS_MQTT_QUEUE_MAX"
        value = "5000"
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

      startup_probe {
        initial_delay_seconds = 5
        timeout_seconds       = 5
        period_seconds        = 10
        failure_threshold     = 6

        http_get {
          path = "/readyz"
          port = 8080
        }
      }

      liveness_probe {
        initial_delay_seconds = 30
        timeout_seconds       = 5
        period_seconds        = 30
        failure_threshold     = 3

        http_get {
          path = "/livez"
          port = 8080
        }
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
