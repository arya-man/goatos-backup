data "google_project" "current" {
  project_id = var.project_id
}

locals {
  cost_alert_bridge_audience = "https://${google_cloud_run_v2_service.cost_alert_bridge.name}-${data.google_project.current.number}.${var.region}.run.app"
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
