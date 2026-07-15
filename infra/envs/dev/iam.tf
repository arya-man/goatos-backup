resource "google_service_account" "runtime" {
  for_each = local.runtime_service_accounts

  account_id   = each.value.account_id
  display_name = each.value.display_name
  description  = "Layer 1 Goat OS dev runtime service account for ${each.key}."
}

resource "google_project_iam_member" "cloudsql_client" {
  for_each = local.database_clients

  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

resource "google_project_iam_custom_role" "notification_fcm_sender" {
  role_id     = "goatosNotificationFcmSenderDev"
  title       = "Goat OS dev notification FCM sender"
  description = "Allows the kernel worker's notification dispatcher stage to send Firebase Cloud Messaging messages."
  permissions = ["cloudmessaging.messages.create"]
}

# The kernel worker runs the notification-dispatcher stage in-process.
resource "google_project_iam_member" "kernel_worker_fcm_sender" {
  project = var.project_id
  role    = google_project_iam_custom_role.notification_fcm_sender.name
  member  = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

resource "google_service_account_iam_member" "cloudscheduler_scheduler_token_creator" {
  service_account_id = google_service_account.runtime["scheduler"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.cloudscheduler.email}"
}

# Near-term Cloud Tasks enqueuer retired with the notification-dispatcher job;
# notifications drain via the worker's 1-minute fast-lane stage.

