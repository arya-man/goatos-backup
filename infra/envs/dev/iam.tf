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

resource "google_project_iam_member" "calendar_cloudtasks_enqueuer" {
  for_each = toset([
    "calendar_reminder_sweeper",
    "calendar_escalation_sweeper",
  ])

  project = var.project_id
  role    = "roles/cloudtasks.enqueuer"
  member  = "serviceAccount:${google_service_account.runtime[each.key].email}"
}

resource "google_service_account_iam_member" "cloudscheduler_scheduler_token_creator" {
  service_account_id = google_service_account.runtime["scheduler"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.cloudscheduler.email}"
}

resource "google_service_account_iam_member" "cloudtasks_scheduler_token_creator" {
  service_account_id = google_service_account.runtime["scheduler"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.cloudtasks.email}"
}

resource "google_service_account_iam_member" "calendar_cloudtasks_oauth_act_as" {
  for_each = toset([
    "calendar_reminder_sweeper",
    "calendar_escalation_sweeper",
  ])

  service_account_id = google_service_account.runtime["scheduler"].name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.runtime[each.key].email}"
}
