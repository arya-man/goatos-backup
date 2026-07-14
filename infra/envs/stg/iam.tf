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
  description = "Allows the notification dispatcher to send Firebase Cloud Messaging messages."
  permissions = ["cloudmessaging.messages.create"]
}

resource "google_project_iam_member" "notification_dispatcher_fcm_sender" {
  project = var.project_id
  role    = google_project_iam_custom_role.notification_fcm_sender.name
  member  = "serviceAccount:${google_service_account.runtime["notification_dispatcher"].email}"
}

resource "google_service_account_iam_member" "cloudscheduler_scheduler_token_creator" {
  service_account_id = google_service_account.runtime["scheduler"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.cloudscheduler.email}"
}

resource "google_service_account_iam_member" "cloudtasks_enqueuer_token_creator" {
  service_account_id = google_service_account.runtime["cloud_tasks_enqueuer"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.cloudtasks.email}"
}

