locals {
  monitoring_notification_channel_names = [
    for channel in google_monitoring_notification_channel.ops_email : channel.name
  ]
}

resource "google_monitoring_notification_channel" "ops_email" {
  for_each = var.monitoring_alert_email_addresses

  display_name = "goatos-dev ${each.value}"
  type         = "email"
  enabled      = true

  labels = {
    email_address = each.value
  }

  depends_on = [google_project_service.enabled]
}

resource "google_monitoring_alert_policy" "cloud_run_errors" {
  display_name          = "goatos-dev Cloud Run errors"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "Cloud Run service or job emitted ERROR logs"

    condition_matched_log {
      filter = trimspace(<<-EOT
        (resource.type="cloud_run_revision" OR resource.type="cloud_run_job")
        AND resource.labels.location="${var.region}"
        AND severity>=ERROR
      EOT
      )
    }
  }

  alert_strategy {
    notification_rate_limit {
      period = "300s"
    }

    auto_close = "604800s"
  }

  depends_on = [google_project_service.enabled]
}

resource "google_monitoring_alert_policy" "outbox_relay_dead_letter" {
  display_name          = "goatos-dev outbox relay dead letters"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "Outbox relay reported dead-lettered messages"

    # The outbox-relay stage now runs inside the kernel-worker SERVICE, not a
    # Cloud Run Job, so match the service revision logs instead of job logs.
    condition_matched_log {
      filter = trimspace(<<-EOT
        resource.type="cloud_run_revision"
        AND resource.labels.service_name="${google_cloud_run_v2_service.kernel_worker.name}"
        AND jsonPayload.dead_letter > 0
      EOT
      )
    }
  }

  alert_strategy {
    notification_rate_limit {
      period = "300s"
    }

    auto_close = "604800s"
  }

  depends_on = [google_project_service.enabled]
}

resource "google_monitoring_alert_policy" "pubsub_dlq_backlog" {
  display_name          = "goatos-dev Pub/Sub DLQ backlog"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "Outbox native DLQ inspection subscription has undelivered messages"

    condition_threshold {
      filter          = "resource.type=\"pubsub_subscription\" AND metric.type=\"pubsub.googleapis.com/subscription/num_undelivered_messages\" AND resource.labels.subscription_id=\"${google_pubsub_subscription.outbox_events_dlq_inspect.name}\""
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = 0

      aggregations {
        alignment_period   = "60s"
        per_series_aligner = "ALIGN_MAX"
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

resource "google_monitoring_alert_policy" "cloud_sql_cpu" {
  display_name          = "goatos-dev Cloud SQL CPU pressure"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "Cloud SQL CPU utilization above 80%"

    condition_threshold {
      filter          = "resource.type=\"cloudsql_database\" AND metric.type=\"cloudsql.googleapis.com/database/cpu/utilization\""
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = 0.8

      aggregations {
        alignment_period   = "60s"
        per_series_aligner = "ALIGN_MEAN"
      }
    }
  }

  depends_on = [google_project_service.enabled]
}

# NO notification backlog-age alert in dev (KERN-REV-07): dev has no OTel
# collector sidecar and no observability_config bucket, so the kernel worker's
# GOATOS_OBS_SINK stays stdout_json and SetupTelemetry never exports metrics to
# Cloud Monitoring. An enabled alert on a metric that never produces samples is
# false-green — it would sit "OK" forever regardless of a real backlog. The
# backlog-age SLO + a metric-absence deadman live in stg, which has the
# collector. Reintroduce a dev alert only alongside a real dev metrics exporter.
