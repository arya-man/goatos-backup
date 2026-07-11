resource "google_sql_database_instance" "core" {
  name             = var.cloud_sql_instance_name
  database_version = "POSTGRES_16"
  region           = var.region

  # Dev stays stopped while the shared dashboard is archived to avoid 24/7
  # CPU/RAM charges. Switch to ALWAYS only for an explicit dev rollout.
  settings {
    tier              = var.cloud_sql_tier
    edition           = "ENTERPRISE"
    availability_type = "ZONAL"
    activation_policy = "NEVER"

    disk_type       = "PD_SSD"
    disk_size       = 10
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
