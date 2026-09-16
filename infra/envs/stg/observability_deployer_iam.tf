# Cloud Deploy upserts only the bounded Faro log-based metrics. No LoggingAdmin,
# log entry access, metric deletion, or unrelated logging configuration rights.
resource "google_project_iam_custom_role" "observability_metric_updater" {
  project     = var.project_id
  role_id     = "goatosObservabilityMetricUpdater"
  title       = "GoatOS observability metric updater"
  description = "Create and update log-based metric definitions during certified staging releases."
  permissions = [
    "logging.logMetrics.get",
    "logging.logMetrics.create",
    "logging.logMetrics.update",
  ]
}

resource "google_project_iam_member" "github_deployer_observability_metric_updater" {
  project = var.project_id
  role    = google_project_iam_custom_role.observability_metric_updater.name
  member  = "serviceAccount:${google_service_account.github_deployer.email}"
}
