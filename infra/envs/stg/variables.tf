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
  description = "Canonical staging dashboard hostname."
  type        = string
  default     = "stg.dashboard.mesha.sg"

  validation {
    condition     = var.canonical_dashboard_host == "stg.dashboard.mesha.sg"
    error_message = "canonical_dashboard_host must remain stg.dashboard.mesha.sg."
  }
}

variable "api_base_url" {
  description = "Current public goatos-stg API base URL used by admin-web SSR."
  type        = string
  default     = "https://goatos-api-stg-awtrpmn4za-el.a.run.app"

  validation {
    condition     = var.api_base_url == "https://goatos-api-stg-awtrpmn4za-el.a.run.app"
    error_message = "api_base_url must remain the current goatos-stg Cloud Run API URL until the custom API host is introduced."
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
