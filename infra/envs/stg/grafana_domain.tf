# Additive definitions for the Grafana custom hostname. The shared HTTPS proxy
# and URL map predate Terraform; do not replace or import them implicitly.
# Attach this backend as grafana-host and append this certificate using the
# reviewed preservation steps in docs/observability/GRAFANA_ACCESS.md.
# Live resources created imperatively must be imported before any full apply.
resource "google_compute_region_network_endpoint_group" "grafana" {
  project               = var.project_id
  region                = var.region
  name                  = "goatos-stg-grafana-neg"
  network_endpoint_type = "SERVERLESS"

  cloud_run {
    service = google_cloud_run_v2_service.grafana.name
  }
}

resource "google_compute_backend_service" "grafana" {
  project               = var.project_id
  name                  = "goatos-stg-grafana-backend"
  protocol              = "HTTP"
  load_balancing_scheme = "EXTERNAL_MANAGED"
  enable_cdn            = false

  backend {
    group = google_compute_region_network_endpoint_group.grafana.id
  }
}

resource "google_compute_managed_ssl_certificate" "grafana" {
  project = var.project_id
  name    = "goatos-grafana-cert"

  managed {
    domains = ["grafana.mesha.sg"]
  }
}
