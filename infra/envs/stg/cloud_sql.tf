resource "google_sql_database_instance" "core" {
  name             = var.cloud_sql_instance_name
  database_version = "POSTGRES_16"
  region           = var.region

  # Live staging is currently kept warm so every stg branch deploy can run the
  # migration job and /readyz smoke without a manual database start step.
  settings {
    tier              = var.cloud_sql_tier
    edition           = "ENTERPRISE"
    availability_type = "ZONAL"
    activation_policy = var.cloud_sql_activation_policy

    disk_type       = "PD_SSD"
    disk_size       = var.cloud_sql_disk_size_gb
    disk_autoresize = true

    backup_configuration {
      enabled                        = false
      point_in_time_recovery_enabled = false
    }

    ip_configuration {
      ipv4_enabled = true
      ssl_mode     = "ENCRYPTED_ONLY"
    }

    # Cloud SQL Query Insights (docs/observability/OBSERVABILITY_DESIGN.md
    # section 4 / "DB" dashboard #2). No raw query text with bound
    # parameters is retained in Cloud SQL's own query string; goat/tenant
    # identifiers are not PII per AGENTS.md, but query args are left
    # untagged here anyway since Insights' "record_client_address" only
    # affects app_name/client host, not row-level goat data.
    insights_config {
      query_insights_enabled  = var.cloud_sql_query_insights_enabled
      query_string_length     = var.cloud_sql_query_insights_query_string_length
      record_application_tags = true
      record_client_address   = true
      query_plans_per_minute  = var.cloud_sql_query_insights_query_plans_per_minute
    }

    user_labels = local.labels
  }

  deletion_protection = true
}

resource "google_sql_database" "app" {
  name     = var.cloud_sql_database_name
  instance = google_sql_database_instance.core.name
}
