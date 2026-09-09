resource "google_project_service_identity" "pubsub" {
  provider = google-beta

  project = var.project_id
  service = "pubsub.googleapis.com"
}

resource "google_pubsub_topic" "outbox_events" {
  name = "goatos-stg-outbox-events"

  message_storage_policy {
    allowed_persistence_regions = [var.region]
  }

  labels = local.labels
}

resource "google_pubsub_topic" "outbox_events_dlq" {
  name = "goatos-stg-outbox-events-dlq"

  message_storage_policy {
    allowed_persistence_regions = [var.region]
  }

  labels = local.labels
}

resource "google_pubsub_topic" "cost_alert_budget_notifications" {
  name = "goatos-stg-cost-alert-budget-notifications"

  message_storage_policy {
    allowed_persistence_regions = [var.region]
  }

  labels = local.labels
}

resource "google_pubsub_subscription" "analytics_export" {
  name  = "goatos-stg-analytics-export"
  topic = google_pubsub_topic.outbox_events.id

  ack_deadline_seconds       = 300
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.outbox_events_dlq.id
    max_delivery_attempts = 5
  }

  labels = local.labels
}

resource "google_pubsub_subscription" "domain_events" {
  name  = "goatos-stg-domain-events"
  topic = google_pubsub_topic.outbox_events.id

  ack_deadline_seconds       = 300
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.outbox_events_dlq.id
    max_delivery_attempts = 5
  }

  labels = local.labels
}

resource "google_pubsub_subscription" "outbox_events_dlq_inspect" {
  name  = "goatos-stg-outbox-events-dlq-inspect"
  topic = google_pubsub_topic.outbox_events_dlq.id

  ack_deadline_seconds       = 60
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  labels = local.labels
}

resource "google_pubsub_subscription" "cost_alert_budget_push" {
  name  = "goatos-stg-cost-alert-budget-push"
  topic = google_pubsub_topic.cost_alert_budget_notifications.id

  ack_deadline_seconds       = 60
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  push_config {
    push_endpoint = "${google_cloud_run_v2_service.cost_alert_bridge.uri}/budget-pubsub?token=${var.cost_alert_bridge_shared_token}"

    oidc_token {
      service_account_email = google_service_account.runtime["cost_alert_bridge"].email
    }
  }

  labels = local.labels
}

resource "google_pubsub_topic_iam_member" "billing_budget_cost_alert_publisher" {
  topic  = google_pubsub_topic.cost_alert_budget_notifications.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:billing-budget-alert@system.gserviceaccount.com"
}

resource "google_service_account_iam_member" "pubsub_cost_alert_bridge_token_creator" {
  service_account_id = google_service_account.runtime["cost_alert_bridge"].name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_project_service_identity.pubsub.email}"
}

# The kernel worker's outbox-relay stage publishes domain events.
resource "google_pubsub_topic_iam_member" "kernel_worker_publisher" {
  topic  = google_pubsub_topic.outbox_events.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

resource "google_pubsub_topic_iam_member" "pubsub_service_agent_dlq_publisher" {
  topic  = google_pubsub_topic.outbox_events_dlq.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${google_project_service_identity.pubsub.email}"
}

resource "google_pubsub_subscription_iam_member" "pubsub_service_agent_source_subscriber" {
  subscription = google_pubsub_subscription.analytics_export.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_project_service_identity.pubsub.email}"
}

# The kernel worker's continuous domain-event consumer stage subscribes.
resource "google_pubsub_subscription_iam_member" "kernel_worker_subscriber" {
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

resource "google_pubsub_subscription_iam_member" "pubsub_service_agent_domain_source_subscriber" {
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_project_service_identity.pubsub.email}"
}
