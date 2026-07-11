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
