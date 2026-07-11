resource "google_artifact_registry_repository" "goatos" {
  location      = var.region
  repository_id = var.artifact_repository_id
  description   = "Goat OS staging Docker images."
  format        = "DOCKER"

  labels = local.labels
}
