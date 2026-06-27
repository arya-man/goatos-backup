resource "google_project_service_identity" "pubsub" {
  provider = google-beta

  project = var.project_id
  service = "pubsub.googleapis.com"
}

resource "google_pubsub_topic" "outbox_events" {
  name = "goatos-dev-outbox-events"

  message_storage_policy {
    allowed_persistence_regions = [var.region]
  }

  labels = local.labels
}

resource "google_pubsub_topic" "outbox_events_dlq" {
  name = "goatos-dev-outbox-events-dlq"

  message_storage_policy {
    allowed_persistence_regions = [var.region]
  }

  labels = local.labels
}

resource "google_pubsub_subscription" "analytics_export" {
  name  = "goatos-dev-analytics-export"
  topic = google_pubsub_topic.outbox_events.id

  ack_deadline_seconds       = 30
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.outbox_events_dlq.id
    max_delivery_attempts = 5
  }

  labels = local.labels
}

resource "google_pubsub_subscription" "domain_events" {
  name  = "goatos-dev-domain-events"
  topic = google_pubsub_topic.outbox_events.id

  ack_deadline_seconds       = 30
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.outbox_events_dlq.id
    max_delivery_attempts = 5
  }

  labels = local.labels
}

resource "google_pubsub_topic_iam_member" "outbox_relay_publisher" {
  topic  = google_pubsub_topic.outbox_events.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${google_service_account.runtime["outbox_relay"].email}"
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

resource "google_pubsub_subscription_iam_member" "domain_consumer_subscriber" {
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_service_account.runtime["domain_consumer"].email}"
}

resource "google_pubsub_subscription_iam_member" "pubsub_service_agent_domain_source_subscriber" {
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_project_service_identity.pubsub.email}"
}
