locals {
  enabled_services = toset([
    "artifactregistry.googleapis.com",
    "aiplatform.googleapis.com",
    "bigquery.googleapis.com",
    "clouddeploy.googleapis.com",
    "cloudtasks.googleapis.com",
    "cloudtrace.googleapis.com",
    "fcm.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "identitytoolkit.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "pubsub.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "sqladmin.googleapis.com",
    "storage.googleapis.com",
  ])
}

resource "google_project_service" "enabled" {
  for_each = local.enabled_services

  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_project_service_identity" "cloudtasks" {
  provider = google-beta

  project = var.project_id
  service = "cloudtasks.googleapis.com"

  depends_on = [google_project_service.enabled]
}

resource "google_project_service_identity" "clouddeploy" {
  provider = google-beta

  project = var.project_id
  service = "clouddeploy.googleapis.com"

  depends_on = [google_project_service.enabled]
}
