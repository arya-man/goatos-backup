variable "organization_id" {
  description = "Mesha/VGoats Google Cloud organization ID."
  type        = string

  validation {
    condition     = var.organization_id == "563962826703"
    error_message = "goatos-stg Terraform must target vgoats.com organization 563962826703."
  }
}

variable "folder_id" {
  description = "Goat OS Google Cloud folder ID."
  type        = string

  validation {
    condition     = var.folder_id == "188649904255"
    error_message = "goatos-stg Terraform must target goat-os folder 188649904255."
  }
}

variable "project_id" {
  description = "Staging Google Cloud project ID."
  type        = string

  validation {
    condition     = var.project_id == "goatos-stg"
    error_message = "This staging composition must target project_id goatos-stg."
  }
}

variable "project_number" {
  description = "Staging Google Cloud project number."
  type        = string

  validation {
    condition     = var.project_number == "514832198871"
    error_message = "goatos-stg project_number must be 514832198871."
  }
}

variable "region" {
  description = "Primary Goat OS staging region."
  type        = string

  validation {
    condition     = var.region == "asia-south1"
    error_message = "goatos-stg region must remain asia-south1."
  }
}

variable "state_bucket_name" {
  description = "Imperatively bootstrapped GCS bucket used by the staging Terraform backend."
  type        = string

  validation {
    condition     = var.state_bucket_name == "goatos-stg-tf-state"
    error_message = "state_bucket_name must match the staging bootstrap bucket."
  }
}

variable "artifact_repository_id" {
  description = "Artifact Registry Docker repository for Goat OS staging images."
  type        = string
  default     = "goatos"

  validation {
    condition     = var.artifact_repository_id == "goatos"
    error_message = "The staging Artifact Registry repository id must remain goatos."
  }
}

variable "cloud_sql_instance_name" {
  description = "goatos-stg Cloud SQL Postgres instance name."
  type        = string
  default     = "goatos-stg-core-db"

  validation {
    condition     = var.cloud_sql_instance_name == "goatos-stg-core-db"
    error_message = "The staging Cloud SQL instance name must remain goatos-stg-core-db."
  }
}

variable "cloud_sql_database_name" {
  description = "Initial Goat OS application database shell name."
  type        = string
  default     = "goatos"

  validation {
    condition     = var.cloud_sql_database_name == "goatos"
    error_message = "The initial staging database shell must be named goatos."
  }
}

variable "cloud_sql_tier" {
  description = "Staging Cloud SQL tier. Pin the exact tier in each benchmark profile before a 1M rehearsal."
  type        = string
  default     = "db-g1-small"

  validation {
    condition = contains([
      "db-g1-small",
      "db-custom-2-7680",
      "db-custom-4-15360",
      "db-custom-8-30720",
    ], var.cloud_sql_tier)
    error_message = "Use a pinned staging benchmark tier: db-custom-2-7680, db-custom-4-15360, or db-custom-8-30720."
  }
}

variable "cloud_sql_activation_policy" {
  description = "Staging Cloud SQL activation policy."
  type        = string
  default     = "ALWAYS"

  validation {
    condition     = contains(["ALWAYS", "NEVER"], var.cloud_sql_activation_policy)
    error_message = "cloud_sql_activation_policy must be ALWAYS or NEVER."
  }
}

variable "cloud_sql_disk_size_gb" {
  description = "Staging Cloud SQL SSD size."
  type        = number
  default     = 20

  validation {
    condition     = var.cloud_sql_disk_size_gb >= 20
    error_message = "cloud_sql_disk_size_gb must be at least 20."
  }
}

variable "stg_tenant_id" {
  description = "Non-secret staging tenant id used by scheduled kernel workers."
  type        = string
  default     = "00000000-0000-4000-8000-000000000001"

  validation {
    condition     = can(regex("^[0-9a-fA-F-]{36}$", var.stg_tenant_id))
    error_message = "stg_tenant_id must be a UUID string."
  }
}

variable "stg_sweeper_actor_id" {
  description = "Existing Goat OS workforce-member UUID used as the audited obligation-sweeper task creator. Supply via private tfvars; there is deliberately no default."
  type        = string

  validation {
    condition     = can(regex("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$", var.stg_sweeper_actor_id))
    error_message = "stg_sweeper_actor_id must be an explicit RFC 4122 version-4 UUID for an existing audited workforce member."
  }
}

variable "backend_image_tag" {
  description = "Backend image tag consumed by Cloud Run Jobs in stg."
  type        = string
  default     = "stg"

  validation {
    condition     = length(trimspace(var.backend_image_tag)) > 0
    error_message = "backend_image_tag is required."
  }
}

variable "migration_image_tag" {
  description = "Migration image tag consumed by the goatos-stg Cloud Run migration job."
  type        = string
  default     = "stg"

  validation {
    condition     = length(trimspace(var.migration_image_tag)) > 0
    error_message = "migration_image_tag is required."
  }
}

variable "admin_web_image_tag" {
  description = "Admin-web image tag consumed by the goatos-stg Cloud Run service."
  type        = string
  default     = "stg"

  validation {
    condition     = length(trimspace(var.admin_web_image_tag)) > 0
    error_message = "admin_web_image_tag is required."
  }
}

variable "canonical_dashboard_host" {
  description = "Canonical public dashboard hostname for the current production-facing rollout backed by goatos-stg."
  type        = string
  default     = "dashboard.mesha.sg"

  validation {
    condition     = var.canonical_dashboard_host == "dashboard.mesha.sg"
    error_message = "canonical_dashboard_host must remain dashboard.mesha.sg for the production-facing rollout."
  }
}

variable "api_base_url" {
  description = "Public API base URL used by admin-web SSR for the current production-facing rollout backed by goatos-stg."
  type        = string
  default     = "https://api.goatos.mesha.sg/"

  validation {
    condition     = var.api_base_url == "https://api.goatos.mesha.sg/"
    error_message = "api_base_url must remain https://api.goatos.mesha.sg/ for the production-facing rollout."
  }
}

variable "google_sign_in_client_id" {
  description = "Public Google Sign-In OAuth client id for goatos-stg admin-web."
  type        = string
  default     = "514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com"

  validation {
    condition     = can(regex("^514832198871-[a-z0-9]+\\.apps\\.googleusercontent\\.com$", var.google_sign_in_client_id))
    error_message = "google_sign_in_client_id must be the goatos-stg web client id."
  }
}

variable "monitoring_alert_email_addresses" {
  description = "Approved Mesha/VGoats operator email addresses for goatos-stg alert notifications. Supply through private tfvars or -var during Goal 2."
  type        = set(string)

  validation {
    condition = length(var.monitoring_alert_email_addresses) > 0 && alltrue([
      for address in var.monitoring_alert_email_addresses :
      address == trimspace(address) && can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", address))
    ])
    error_message = "monitoring_alert_email_addresses must contain at least one approved, trimmed email address."
  }
}

# ---------------------------------------------------------------------------
# Kernel-worker cutover control (KERN-01 safety: two-phase legacy job retirement)
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Observability stack (infra/envs/stg/observability.tf, monitoring.tf,
# secrets.tf, analytics_rollup.tf, cloud_sql.tf). See
# docs/observability/OBSERVABILITY_DESIGN.md and docs/observability/INFRA.md.
# ---------------------------------------------------------------------------

variable "otel_collector_image" {
  description = "Container image for the OTel Collector sidecar mounted inside the api Cloud Run service, every kernel Cloud Run Job, and the grafana_alloy Cloud Run service (see docs/observability/INFRA.md 'Sidecar collector decision' — there is no standalone otel-collector Cloud Run service). otel/opentelemetry-collector-contrib is the reference upstream image; pin to a digest for a real deploy."
  type        = string
  default     = "otel/opentelemetry-collector-contrib:0.114.0"

  validation {
    condition     = length(trimspace(var.otel_collector_image)) > 0
    error_message = "otel_collector_image is required."
  }
}

variable "gmp_frontend_image" {
  description = "Container image for the goatos-stg-gmp-frontend Cloud Run service — the small Prometheus-compatible query proxy in front of Google Managed Service for Prometheus (see docs/observability/INFRA.md for why this exists)."
  type        = string
  default     = "gke.gcr.io/prometheus-engine/frontend:v0.15.1"

  validation {
    condition     = length(trimspace(var.gmp_frontend_image)) > 0
    error_message = "gmp_frontend_image is required."
  }
}

variable "grafana_image" {
  description = "Container image for the goatos-stg-grafana Cloud Run service."
  type        = string
  default     = "grafana/grafana:11.4.0"

  validation {
    condition     = length(trimspace(var.grafana_image)) > 0
    error_message = "grafana_image is required."
  }
}

variable "grafana_min_instance_count" {
  description = "Minimum Cloud Run instance count for Grafana. 0 is acceptable in stg (cold start is a UI-only cost, not a telemetry-loss risk)."
  type        = number
  default     = 0

  validation {
    condition     = var.grafana_min_instance_count >= 0
    error_message = "grafana_min_instance_count must be >= 0."
  }
}

variable "grafana_postgres_datasource_user" {
  description = "Read-only Postgres role Grafana's analytics-rollup datasource connects as. Must exist as a least-privilege (SELECT-only on analytics.*) role created by a backend/migration change, not by Terraform."
  type        = string
  default     = "goatos_grafana_ro"

  validation {
    condition     = length(trimspace(var.grafana_postgres_datasource_user)) > 0
    error_message = "grafana_postgres_datasource_user is required."
  }
}

variable "grafana_alloy_image" {
  description = "Container image for the goatos-stg-grafana-alloy Cloud Run service."
  type        = string
  default     = "grafana/alloy:v1.5.1"

  validation {
    condition     = length(trimspace(var.grafana_alloy_image)) > 0
    error_message = "grafana_alloy_image is required."
  }
}

variable "grafana_alloy_min_instance_count" {
  description = "Minimum Cloud Run instance count for Grafana Alloy. Keep at >= 1 in stg since it is the public-facing browser RUM ingest endpoint and cold starts would drop admin-web page-load telemetry."
  type        = number
  default     = 1

  validation {
    condition     = var.grafana_alloy_min_instance_count >= 0
    error_message = "grafana_alloy_min_instance_count must be >= 0."
  }
}

variable "observability_operator_members" {
  description = "IAM principals (e.g. \"user:name@mesha.sg\", \"group:goatos-observability@vgoats.com\") granted roles/run.invoker on the goatos-stg-grafana Cloud Run service. Deliberately no default — supply the reviewed operator list via private tfvars, same discipline as stg_sweeper_actor_id. Access flow: `gcloud run services proxy goatos-stg-grafana --region=asia-south1` for an authenticated local tunnel; see docs/observability/INFRA.md."
  type        = set(string)
  default     = []

  validation {
    condition = alltrue([
      for member in var.observability_operator_members :
      can(regex("^(user|group|serviceAccount|domain):", member))
    ])
    error_message = "Each observability_operator_members entry must be an IAM principal prefixed with user:, group:, serviceAccount:, or domain:."
  }
}

variable "cloud_sql_query_insights_enabled" {
  description = "Enables Cloud SQL Query Insights on goatos-stg-core-db (docs/observability/OBSERVABILITY_DESIGN.md section 4)."
  type        = bool
  default     = true
}

variable "cloud_sql_query_insights_query_string_length" {
  description = "Max stored query string length for Cloud SQL Query Insights (bytes)."
  type        = number
  default     = 4500

  validation {
    condition     = var.cloud_sql_query_insights_query_string_length >= 256 && var.cloud_sql_query_insights_query_string_length <= 4500
    error_message = "cloud_sql_query_insights_query_string_length must be between 256 and 4500 (Cloud SQL's own accepted range)."
  }
}

variable "cloud_sql_query_insights_query_plans_per_minute" {
  description = "Number of query execution plans per minute captured by Cloud SQL Query Insights."
  type        = number
  default     = 20

  validation {
    condition     = var.cloud_sql_query_insights_query_plans_per_minute >= 0 && var.cloud_sql_query_insights_query_plans_per_minute <= 20
    error_message = "cloud_sql_query_insights_query_plans_per_minute must be between 0 and 20 (Cloud SQL's own accepted range)."
  }
}

variable "api_availability_slo" {
  description = "API availability SLO (non-5xx ratio). docs/observability/OBSERVABILITY_DESIGN.md section 7 target: 99.5%."
  type        = number
  default     = 0.995

  validation {
    condition     = var.api_availability_slo > 0 && var.api_availability_slo < 1
    error_message = "api_availability_slo must be a ratio strictly between 0 and 1."
  }
}

variable "api_error_rate_alert_threshold_ratio" {
  description = "5xx-error-rate ratio (0-1) that pages ops for the API error-rate SLO burn alert."
  type        = number
  default     = 0.01

  validation {
    condition     = var.api_error_rate_alert_threshold_ratio > 0 && var.api_error_rate_alert_threshold_ratio < 1
    error_message = "api_error_rate_alert_threshold_ratio must be a ratio strictly between 0 and 1."
  }
}

variable "api_latency_p99_read_slo_seconds" {
  description = "API read-path p99 latency SLO in seconds. docs/observability/OBSERVABILITY_DESIGN.md section 7 target: < 800ms."
  type        = number
  default     = 0.8

  validation {
    condition     = var.api_latency_p99_read_slo_seconds > 0
    error_message = "api_latency_p99_read_slo_seconds must be > 0."
  }
}

variable "api_latency_p99_write_slo_seconds" {
  description = "API write-path p99 latency SLO in seconds. docs/observability/OBSERVABILITY_DESIGN.md section 7 target: < 1500ms."
  type        = number
  default     = 1.5

  validation {
    condition     = var.api_latency_p99_write_slo_seconds > 0
    error_message = "api_latency_p99_write_slo_seconds must be > 0."
  }
}

variable "consumer_lag_slo_seconds" {
  description = "Domain event consumer lag SLO in seconds. docs/observability/OBSERVABILITY_DESIGN.md section 7 target: < 60s."
  type        = number
  default     = 60

  validation {
    condition     = var.consumer_lag_slo_seconds > 0
    error_message = "consumer_lag_slo_seconds must be > 0."
  }
}

variable "notification_success_slo_ratio" {
  description = "Notification dispatch success-rate SLO (ratio 0-1). docs/observability/OBSERVABILITY_DESIGN.md section 7 target: > 99%."
  type        = number
  default     = 0.99

  validation {
    condition     = var.notification_success_slo_ratio > 0 && var.notification_success_slo_ratio < 1
    error_message = "notification_success_slo_ratio must be a ratio strictly between 0 and 1."
  }
}

variable "notification_failure_rate_alert_threshold_ratio" {
  description = "Notification failure-rate ratio (0-1) that pages ops (should be 1 - notification_success_slo_ratio or tighter)."
  type        = number
  default     = 0.01

  validation {
    condition     = var.notification_failure_rate_alert_threshold_ratio > 0 && var.notification_failure_rate_alert_threshold_ratio < 1
    error_message = "notification_failure_rate_alert_threshold_ratio must be a ratio strictly between 0 and 1."
  }
}

variable "ga4_export_dataset_id" {
  description = "BigQuery dataset id of the Firebase GA4 BigQuery export for goatos-stg, once linked in the Firebase console (Firebase names it automatically, typically analytics_<GA4_property_id>). Left empty (default) until that manual linking step is done — see docs/observability/INFRA.md. Must be a dataset located in asia-south1 (Mumbai); relink GA4 export location if Firebase chose a different default."
  type        = string
  default     = ""
}

variable "trace_sample_ratio" {
  description = "OpenTelemetry parent-based trace sampling ratio applied by the backend api service and kernel worker Jobs (GOATOS_TRACE_SAMPLE_RATIO). Metrics are always unsampled (full-fidelity p50/p90/p99); this only bounds trace volume. 0.1 = sample 10% of root traces in staging."
  type        = string
  default     = "0.1"

  validation {
    condition     = can(tonumber(var.trace_sample_ratio)) && tonumber(var.trace_sample_ratio) >= 0 && tonumber(var.trace_sample_ratio) <= 1
    error_message = "trace_sample_ratio must be a number between 0 and 1."
  }
}
