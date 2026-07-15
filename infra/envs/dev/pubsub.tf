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
  name  = "goatos-dev-domain-events"
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
  name  = "goatos-dev-outbox-events-dlq-inspect"
  topic = google_pubsub_topic.outbox_events_dlq.id

  ack_deadline_seconds       = 60
  message_retention_duration = "604800s"
  retain_acked_messages      = false

  labels = local.labels
}

# The kernel worker's outbox-relay stage publishes domain events.
resource "google_pubsub_topic_iam_member" "kernel_worker_publisher" {
  topic  = google_pubsub_topic.outbox_events.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

# KERN-01 phase 1 (retire_legacy_stage_jobs = false): the restored legacy
# outbox-relay JOB publishes to the outbox topic with its own SA, so it needs the
# publisher grant back. Phase 2 (flag = true): the job + its SA are gone and only
# kernel_worker_publisher above remains.
resource "google_pubsub_topic_iam_member" "outbox_relay_publisher" {
  count  = var.retire_legacy_stage_jobs ? 0 : 1
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

# The kernel worker's continuous domain-event consumer stage subscribes.
resource "google_pubsub_subscription_iam_member" "kernel_worker_subscriber" {
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_service_account.runtime["kernel_worker"].email}"
}

# KERN-01 phase 1 (retire_legacy_stage_jobs = false): the restored legacy
# domain-event-consumer JOB subscribes to the domain-events subscription with its
# own SA, so it needs the subscriber grant back. Phase 2 (flag = true): the job +
# its SA are gone and only kernel_worker_subscriber above remains.
resource "google_pubsub_subscription_iam_member" "domain_consumer_subscriber" {
  count        = var.retire_legacy_stage_jobs ? 0 : 1
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_service_account.runtime["domain_consumer"].email}"
}

resource "google_pubsub_subscription_iam_member" "pubsub_service_agent_domain_source_subscriber" {
  subscription = google_pubsub_subscription.domain_events.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${google_project_service_identity.pubsub.email}"
}
