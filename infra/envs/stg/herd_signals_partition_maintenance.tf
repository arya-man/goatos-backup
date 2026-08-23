# Herd Signals raw-packet partition maintenance
# (docs/modules/herd-signals-system-design.md Section 3.2,
# docs/runbooks/herd-signals-partition-retention.md).
#
# Declares the Cloud Run Job the same way analytics_rollup.tf does, but unlike dev
# (infra/envs/dev/cloud_run_jobs.tf local.kernel_jobs) there is NO Cloud Scheduler wiring for
# ANY job in this stg environment yet -- that infra does not exist here today (the "scheduler"
# runtime SA declared in main.tf is provisioned for future use but has no
# google_cloud_scheduler_job resource pointed at it in this env). This job is declared and
# invokable, but running it on stg is a MANUAL step until stg gets the same
# Cloud Scheduler wiring dev has. See the runbook above for the exact manual trigger command and
# the operational contract (healthy vs unhealthy signals) this job implements.
#
# Getting this wrong has real consequences: if nothing calls
# herd_signal_packets_ensure_future_partitions, the pre-created partition runway (migration
# 000200) eventually runs out and new packets fall into the DEFAULT catch-all partition,
# silently destroying partition pruning; if nothing calls
# herd_signal_packets_prune_expired_partitions, raw packets accumulate forever (~65 GB/day at
# the release envelope). Dropping a partition is IRREVERSIBLE -- the raw packets in it are gone,
# not archived; only the derived activity windows/snapshot survive.

resource "google_service_account" "herd_signals_partition_maintenance" {
  account_id   = "goatos-stg-herd-sig-partmnt"
  display_name = "Goat OS staging herd-signals partition maintenance runtime"
  description  = "Runs backend/cmd/herd-signals-partition-maintenance (ensure-future-partitions + prune-expired-partitions) against herd_signal_packets only. See herd_signals_partition_maintenance.tf."
}

resource "google_project_iam_member" "herd_signals_partition_maintenance_cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.herd_signals_partition_maintenance.email}"
}

resource "google_secret_manager_secret_iam_member" "herd_signals_partition_maintenance_database_url_accessor" {
  secret_id = google_secret_manager_secret.container["database_url"].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.herd_signals_partition_maintenance.email}"
}

locals {
  # The backend image already contains /app/bin/herd-signals-partition-maintenance (Dockerfile
  # build list). Reusing the release image keeps this job on the same commit as the API and
  # kernel worker instead of depending on an independently stale tag.
  herd_signals_partition_maintenance_image = local.backend_image
}

resource "google_cloud_run_v2_job" "herd_signals_partition_maintenance" {
  name                = "goatos-stg-herd-signals-partition-maintenance"
  location            = var.region
  deletion_protection = false
  labels              = local.labels

  template {
    task_count  = 1
    parallelism = 1

    template {
      service_account = google_service_account.herd_signals_partition_maintenance.email
      timeout         = "120s"
      max_retries     = 1

      containers {
        image   = local.herd_signals_partition_maintenance_image
        command = ["/app/bin/herd-signals-partition-maintenance"]
        # -retention-days=14 matches the documented default
        # (docs/modules/herd-signals-system-design.md Section 3.2).
        args = ["-timeout=90s", "-days-ahead=14", "-retention-days=14"]

        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
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
  }

  depends_on = [google_project_service.enabled]
}
