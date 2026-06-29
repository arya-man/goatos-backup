output "artifact_registry_repository" {
  description = "Dev Artifact Registry Docker repository."
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.goatos.repository_id}"
}

output "cloud_sql_connection_name" {
  description = "Cloud SQL connection name for future /cloudsql socket mounts."
  value       = google_sql_database_instance.core.connection_name
}

output "cloud_sql_activation_policy" {
  description = "Layer 1 planned Cloud SQL activation policy."
  value       = google_sql_database_instance.core.settings[0].activation_policy
}

output "runtime_service_accounts" {
  description = "Runtime service account emails for later Cloud Run services/jobs."
  value = {
    for key, account in google_service_account.runtime : key => account.email
  }
}

output "pubsub_outbox_topic" {
  description = "Outbox event topic short id for GOATOS_OUTBOX_PUBSUB_TOPIC_ID."
  value       = google_pubsub_topic.outbox_events.name
}

output "pubsub_domain_events_subscription" {
  description = "Domain event consumer subscription short id for GOATOS_DOMAIN_EVENTS_SUBSCRIPTION_ID."
  value       = google_pubsub_subscription.domain_events.name
}

output "cloud_tasks_near_term_queue" {
  description = "Near-term kernel Cloud Tasks queue id."
  value       = google_cloud_tasks_queue.near_term_kernel.name
}

output "cloud_run_kernel_jobs" {
  description = "Cloud Run Job names for scheduled kernel workers."
  value = {
    for key, job in google_cloud_run_v2_job.kernel : key => job.name
  }
}

output "cloud_run_migration_job" {
  description = "Cloud Run Job name for manual guarded schema migrations."
  value       = google_cloud_run_v2_job.migrate.name
}

output "cloud_run_outbox_dlq_job" {
  description = "Cloud Run Job name for manual outbox DLQ list/replay operations."
  value       = google_cloud_run_v2_job.outbox_dlq.name
}

output "pubsub_outbox_dlq_inspect_subscription" {
  description = "Pub/Sub subscription short id for inspecting native DLQ messages."
  value       = google_pubsub_subscription.outbox_events_dlq_inspect.name
}

output "monitoring_alert_policies" {
  description = "Baseline goatos-dev Cloud Monitoring alert policies."
  value = {
    cloud_run_errors         = google_monitoring_alert_policy.cloud_run_errors.name
    cloud_sql_cpu            = google_monitoring_alert_policy.cloud_sql_cpu.name
    outbox_relay_dead_letter = google_monitoring_alert_policy.outbox_relay_dead_letter.name
    pubsub_dlq_backlog       = google_monitoring_alert_policy.pubsub_dlq_backlog.name
  }
}

output "monitoring_notification_channels" {
  description = "Operator notification channel resource names."
  value       = local.monitoring_notification_channel_names
}

output "secret_container_ids" {
  description = "Secret Manager container ids; values are populated out-of-band later."
  value = {
    for key, secret in google_secret_manager_secret.container : key => secret.secret_id
  }
}
