locals {
  monitoring_notification_channel_names = [
    for channel in google_monitoring_notification_channel.ops_email : channel.name
  ]
}

resource "google_monitoring_notification_channel" "ops_email" {
  for_each = var.monitoring_alert_email_addresses

  display_name = "goatos-stg ${each.value}"
  type         = "email"
  enabled      = true

  labels = {
    email_address = each.value
  }

  depends_on = [google_project_service.enabled]
}

resource "google_monitoring_alert_policy" "cloud_run_errors" {
  display_name          = "goatos-stg Cloud Run errors"
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
  display_name          = "goatos-stg outbox relay dead letters"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "Outbox relay reported dead-lettered messages"

    condition_matched_log {
      filter = trimspace(<<-EOT
        resource.type="cloud_run_job"
        AND resource.labels.job_name="${local.kernel_jobs.outbox_relay.name}"
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
  display_name          = "goatos-stg Pub/Sub DLQ backlog"
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
  display_name          = "goatos-stg Cloud SQL CPU pressure"
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

# ---------------------------------------------------------------------------
# SLO / burn-rate alert policies (docs/observability/OBSERVABILITY_DESIGN.md
# section 7). Metrics arrive via the OTel Collector -> Google Managed
# Prometheus (GMP); GMP metrics are visible to Cloud Monitoring alert
# policies as ordinary `prometheus.googleapis.com/...` metric types, so no
# separate Grafana-side alerting is required. Metric-name suffixes follow
# GMP's standard OTLP naming convention:
#   - Counters get `_total` suffix (e.g., http_server_requests_total)
#   - Second-unit metrics get `_seconds` suffix (e.g., http_server_request_duration_seconds)
# MUST verify the exact metric type strings in Cloud Monitoring Metrics Explorer
# once real telemetry is flowing and adjust the `filter` and alert policies below
# if they differ from what the OTel Collector actually exports to GMP.
# ---------------------------------------------------------------------------

resource "google_monitoring_alert_policy" "api_error_rate_slo_burn" {
  display_name          = "goatos-stg API 5xx error-rate SLO burn"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "5xx error rate over 5m exceeds ${var.api_error_rate_alert_threshold_ratio * 100}% (SLO ${(1 - var.api_availability_slo) * 100}% error budget)"

    condition_threshold {
      # Numerator: rate of 5xx errors
      filter = join(" ", [
        "metric.type=\"prometheus.googleapis.com/http_server_requests_total/counter\"",
        "AND resource.type=\"generic_task\"",
        "AND metric.labels.status_class=\"5xx\"",
      ])
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = var.api_error_rate_alert_threshold_ratio

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
      }

      # Denominator: rate of ALL requests (compute true ratio: 5xx/total)
      denominator_filter = join(" ", [
        "metric.type=\"prometheus.googleapis.com/http_server_requests_total/counter\"",
        "AND resource.type=\"generic_task\"",
      ])

      denominator_aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
      }
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

resource "google_monitoring_alert_policy" "api_latency_p99_burn" {
  display_name          = "goatos-stg API p99 latency SLO burn"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "p99 request duration above the read SLO (${var.api_latency_p99_read_slo_seconds}s)"

    condition_threshold {
      filter = join(" ", [
        "metric.type=\"prometheus.googleapis.com/http_server_request_duration_seconds/histogram\"",
        "AND resource.type=\"generic_task\"",
      ])
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = var.api_latency_p99_read_slo_seconds

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_PERCENTILE_99"
        cross_series_reducer = "REDUCE_MAX"
      }
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

resource "google_monitoring_alert_policy" "api_latency_p99_write_burn" {
  display_name          = "goatos-stg API write-path p99 latency SLO burn"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "p99 write-path request duration above the write SLO (${var.api_latency_p99_write_slo_seconds}s)"

    condition_threshold {
      filter = join(" ", [
        "metric.type=\"prometheus.googleapis.com/http_server_request_duration_seconds/histogram\"",
        "AND resource.type=\"generic_task\"",
        "AND metric.labels.method=one_of(\"POST\",\"PUT\",\"PATCH\",\"DELETE\")",
      ])
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = var.api_latency_p99_write_slo_seconds

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_PERCENTILE_99"
        cross_series_reducer = "REDUCE_MAX"
      }
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

resource "google_monitoring_alert_policy" "kernel_consumer_lag" {
  display_name          = "goatos-stg domain event consumer lag"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "kernel.consumer.lag above the SLO (${var.consumer_lag_slo_seconds}s)"

    condition_threshold {
      filter = join(" ", [
        "metric.type=\"prometheus.googleapis.com/kernel_consumer_lag_seconds/gauge\"",
        "AND resource.type=\"generic_task\"",
      ])
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = var.consumer_lag_slo_seconds

      aggregations {
        alignment_period   = "60s"
        per_series_aligner = "ALIGN_MAX"
      }
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

resource "google_monitoring_alert_policy" "kernel_notification_failure_rate" {
  display_name          = "goatos-stg notification dispatch failure rate"
  combiner              = "OR"
  enabled               = true
  notification_channels = local.monitoring_notification_channel_names
  user_labels           = local.labels

  conditions {
    display_name = "kernel.notify.failures rate exceeds ${var.notification_failure_rate_alert_threshold_ratio * 100}% (SLO success > ${var.notification_success_slo_ratio * 100}%)"

    condition_threshold {
      filter = join(" ", [
        "metric.type=\"prometheus.googleapis.com/kernel_notify_failures_total/counter\"",
        "AND resource.type=\"generic_task\"",
      ])
      comparison      = "COMPARISON_GT"
      duration        = "300s"
      threshold_value = var.notification_failure_rate_alert_threshold_ratio

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
      }
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
