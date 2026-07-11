resource "google_cloud_tasks_queue" "near_term_kernel" {
  name     = "goatos-stg-near-term-kernel"
  location = var.region

  rate_limits {
    max_dispatches_per_second = 5
    max_concurrent_dispatches = 20
  }

  retry_config {
    max_attempts       = 5
    min_backoff        = "30s"
    max_backoff        = "600s"
    max_doublings      = 5
    max_retry_duration = "1800s"
  }

  stackdriver_logging_config {
    sampling_ratio = 1.0
  }

  depends_on = [google_project_service.enabled]
}
