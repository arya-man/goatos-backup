resource "google_secret_manager_secret" "container" {
  for_each = local.secret_containers

  secret_id = each.value.secret_id

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  labels = local.labels
}

resource "google_secret_manager_secret_iam_member" "secret_accessor" {
  for_each = local.secret_accessor_bindings

  secret_id = google_secret_manager_secret.container[each.value.secret_key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.runtime[each.value.accessor].email}"
}

resource "google_secret_manager_secret" "slack_deploy_signing_secret" {
  secret_id = "goatos-stg-slack-deploy-signing-secret"

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  labels = local.labels
}

resource "google_secret_manager_secret" "slack_deploy_webhook_url" {
  secret_id = "goatos-stg-deploy-slack-webhook-url"

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  labels = local.labels
}

resource "google_secret_manager_secret_iam_member" "slack_deploy_signing_secret_accessor" {
  secret_id = google_secret_manager_secret.slack_deploy_signing_secret.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.slack_deploy_bot.email}"
}

resource "google_secret_manager_secret_iam_member" "slack_deploy_webhook_url_accessor" {
  secret_id = google_secret_manager_secret.slack_deploy_webhook_url.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.slack_deploy_bot.email}"
}

# ---------------------------------------------------------------------------
# Observability secrets (declared standalone, not folded into
# local.secret_containers in main.tf, to keep this lane additive). Same
# replication pattern as the containers above: user-managed, single region
# asia-south1. Terraform creates the container only; secret versions are an
# operator/Secret Manager action, never committed.
# ---------------------------------------------------------------------------

resource "google_secret_manager_secret" "grafana_admin_password" {
  secret_id = "goatos-stg-grafana-admin-password"

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  labels = local.labels
}

resource "google_secret_manager_secret_iam_member" "grafana_admin_password_accessor" {
  secret_id = google_secret_manager_secret.grafana_admin_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.grafana.email}"
}

# Password for the read-only Postgres role Grafana's "Postgres (analytics
# rollups)" datasource connects as (see docs/observability/INFRA.md for the
# expected `goatos_grafana_ro` role grant, which is a backend/migration-owned
# SQL change, not something Terraform creates).
resource "google_secret_manager_secret" "grafana_postgres_datasource_password" {
  secret_id = "goatos-stg-grafana-postgres-datasource-password"

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  labels = local.labels
}

resource "google_secret_manager_secret_iam_member" "grafana_postgres_datasource_password_accessor" {
  secret_id = google_secret_manager_secret.grafana_postgres_datasource_password.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.grafana.email}"
}
