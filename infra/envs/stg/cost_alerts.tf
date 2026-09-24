data "google_project" "current" {
  project_id = var.project_id
}

locals {
  cost_alert_bridge_name     = "goatos-stg-cost-alert-bridge"
  cost_alert_bridge_audience = "https://${local.cost_alert_bridge_name}-${data.google_project.current.number}.${var.region}.run.app"
  # The bridge's code, image and deploy live in vgoats/mesha-ops (cost-alert-bridge/cloudbuild.yaml),
  # which rolls this service image independently of Goat OS releases. Terraform owns env, secrets,
  # IAM and scaling only; the image below is used on first create and then ignored.
  cost_alert_bridge_image = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repository_id}/cost-alert-bridge:manual-a9546c7f-81c9-46bc-8d06-81e0f8751d2d"
}

resource "google_bigquery_dataset" "billing_export" {
  dataset_id    = "goatos_billing_export"
  friendly_name = "GoatOS Billing Export"
  description   = "Standard Cloud Billing export for GoatOS cost alerts."
  location      = "US"
  labels        = local.labels

  depends_on = [google_project_service.enabled]
}

resource "google_project_iam_member" "cost_alert_bridge_bigquery_job_user" {
  project = var.project_id
  role    = "roles/bigquery.jobUser"
  member  = "serviceAccount:${google_service_account.runtime["cost_alert_bridge"].email}"
}

resource "google_project_iam_member" "cost_alert_bridge_bigquery_data_viewer" {
  project = var.project_id
  role    = "roles/bigquery.dataViewer"
  member  = "serviceAccount:${google_service_account.runtime["cost_alert_bridge"].email}"
}

resource "google_service_account_iam_member" "cloudscheduler_cost_alert_token_creator" {
  service_account_id = google_service_account.runtime["scheduler"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.cloudscheduler.email}"
}

resource "google_billing_budget" "goatos_monthly_forecast_slack_alerts" {
  billing_account = "01FEDE-96BCB3-76D992"
  display_name    = "GoatOS monthly forecast Slack alerts"

  budget_filter {
    calendar_period        = "MONTH"
    credit_types_treatment = "INCLUDE_ALL_CREDITS"
  }

  amount {
    specified_amount {
      currency_code = "INR"
      units         = "45000"
    }
  }

  threshold_rules {
    threshold_percent = 0.5555555556
    spend_basis       = "FORECASTED_SPEND"
  }

  threshold_rules {
    threshold_percent = 0.7777777778
    spend_basis       = "FORECASTED_SPEND"
  }

  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "FORECASTED_SPEND"
  }

  all_updates_rule {
    pubsub_topic   = google_pubsub_topic.cost_alert_budget_notifications.id
    schema_version = "1.0"
  }
}

# Dedicated notification state; no access to proof/media objects is granted.
resource "google_storage_bucket" "cost_alert_state" {
  name                        = "${var.project_id}-cost-alert-state"
  location                    = var.region
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = false
  labels                      = local.labels

  lifecycle_rule {
    condition { age = 400 }
    action { type = "Delete" }
  }
  depends_on = [google_project_service.enabled]
}

resource "google_storage_bucket_iam_member" "cost_alert_state_writer" {
  bucket = google_storage_bucket.cost_alert_state.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.runtime["cost_alert_bridge"].email}"
}
