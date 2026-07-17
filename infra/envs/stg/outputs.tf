output "artifact_registry_repository" {
  description = "Staging Artifact Registry Docker repository."
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

output "cloud_run_explicit_jobs" {
  description = "Cloud Run Job names retained for deploy-time or manual execution; none is scheduled."
  value = {
    migrate                        = google_cloud_run_v2_job.migrate.name
    outbox_dlq                     = google_cloud_run_v2_job.outbox_dlq.name
    vaccination_schedule_projector = google_cloud_run_v2_job.vaccination_schedule_projector.name
    analytics_rollup               = google_cloud_run_v2_job.analytics_rollup.name
  }
}

output "cloud_run_services" {
  description = "Cloud Run service URLs for staging API, admin-web, and the two-instance kernel worker."
  value = {
    api           = google_cloud_run_v2_service.api.uri
    admin_web     = google_cloud_run_v2_service.admin_web.uri
    kernel_worker = google_cloud_run_v2_service.kernel_worker.uri
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

output "proof_media_bucket" {
  description = "GCS bucket for staging proof media objects."
  value       = google_storage_bucket.proof_media.name
}

output "proof_media_signer_service_account" {
  description = "Service account whose key is loaded out-of-band into the proof GCS signer secret."
  value       = google_service_account.proof_media_signer.email
}

output "pubsub_outbox_dlq_inspect_subscription" {
  description = "Pub/Sub subscription short id for inspecting native DLQ messages."
  value       = google_pubsub_subscription.outbox_events_dlq_inspect.name
}

output "monitoring_alert_policies" {
  description = "Baseline goatos-stg Cloud Monitoring alert policies."
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

output "observability_cloud_run_services" {
  description = "Cloud Run service URLs for the goatos-stg observability stack. Note: the OTel Collector and GMP query-frontend are sidecar containers inside api/kernel-jobs/grafana and grafana (respectively), not standalone Cloud Run services — see docs/observability/INFRA.md 'Sidecar collector decision' and section 13."
  value = {
    grafana       = google_cloud_run_v2_service.grafana.uri
    grafana_alloy = google_cloud_run_v2_service.grafana_alloy.uri
  }
}

output "observability_service_accounts" {
  description = "Runtime service account emails for the observability stack. There is no dedicated otel_collector or gmp_frontend SA: both run as sidecars under their host revision's SA (grafana and grafana for the respective services, or the kernel/api producer SAs for the collector) — see docs/observability/INFRA.md 'Sidecar collector decision'."
  value = {
    grafana          = google_service_account.grafana.email
    grafana_alloy    = google_service_account.grafana_alloy.email
    analytics_rollup = google_service_account.analytics_rollup.email
  }
}

output "observability_secret_container_ids" {
  description = "Observability Secret Manager container ids; values are populated out-of-band later."
  value = {
    grafana_admin_password           = google_secret_manager_secret.grafana_admin_password.secret_id
    grafana_postgres_datasource_pass = google_secret_manager_secret.grafana_postgres_datasource_password.secret_id
  }
}

output "analytics_rollup_bigquery_dataset" {
  description = "BigQuery dataset id for the analytics rollup job's own working tables (asia-south1)."
  value       = google_bigquery_dataset.analytics_rollup.dataset_id
}

output "analytics_rollup_cloud_run_job" {
  description = "Cloud Run Job name for the manual GA4->BigQuery->Postgres analytics rollup."
  value       = google_cloud_run_v2_job.analytics_rollup.name
}

output "observability_monitoring_alert_policies" {
  description = "SLO/burn-rate Cloud Monitoring alert policies added by the observability stack."
  value = {
    api_error_rate_slo_burn          = google_monitoring_alert_policy.api_error_rate_slo_burn.name
    api_latency_p99_burn             = google_monitoring_alert_policy.api_latency_p99_burn.name
    api_latency_p99_write_burn       = google_monitoring_alert_policy.api_latency_p99_write_burn.name
    kernel_consumer_lag              = google_monitoring_alert_policy.kernel_consumer_lag.name
    kernel_notification_failure_rate = google_monitoring_alert_policy.kernel_notification_failure_rate.name
  }
}
