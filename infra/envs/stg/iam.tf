resource "google_service_account" "runtime" {
  for_each = local.runtime_service_accounts

  account_id   = each.value.account_id
  display_name = each.value.display_name
  description  = "Layer 1 Goat OS staging runtime service account for ${each.key}."
}

resource "google_project_iam_member" "cloudsql_client" {
  for_each = local.database_clients

  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

resource "google_project_iam_custom_role" "notification_fcm_sender" {
  role_id     = "goatosNotificationFcmSenderStaging"
  title       = "Goat OS staging notification FCM sender"
  description = "Allows the kernel worker's notification dispatcher stage to send Firebase Cloud Messaging messages."
  permissions = ["cloudmessaging.messages.create"]
}

# The kernel worker runs the notification-dispatcher stage in-process, so it
# holds the FCM sender role that the retired notification-dispatcher job used to.
resource "google_project_iam_member" "kernel_worker_fcm_sender" {
  project = var.project_id
  role    = google_project_iam_custom_role.notification_fcm_sender.name
  member  = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

resource "google_project_iam_member" "api_vertex_user" {
  project = var.project_id
  role    = "roles/aiplatform.user"
  member  = "serviceAccount:${google_service_account.runtime["api"].email}"
}

resource "google_project_iam_member" "api_firebase_auth_admin" {
  project = var.project_id
  role    = "roles/firebaseauth.admin"
  member  = "serviceAccount:${google_service_account.runtime["api"].email}"
}

# The near-term Cloud Tasks enqueuer (which invoked the retired
# notification-dispatcher job for sub-minute delivery) is gone: notifications
# now drain via the kernel worker's 1-minute fast-lane stage. No Cloud Tasks
# OAuth token-creator binding is needed.
