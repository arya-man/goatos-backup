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

    user_labels = local.labels
  }

  deletion_protection = true
}

resource "google_sql_database" "app" {
  name     = var.cloud_sql_database_name
  instance = google_sql_database_instance.core.name
}
