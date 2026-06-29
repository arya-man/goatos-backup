variable "organization_id" {
  description = "Mesha/VGoats Google Cloud organization ID."
  type        = string

  validation {
    condition     = var.organization_id == "563962826703"
    error_message = "goatos-dev Terraform must target vgoats.com organization 563962826703."
  }
}

variable "folder_id" {
  description = "Goat OS Google Cloud folder ID."
  type        = string

  validation {
    condition     = var.folder_id == "188649904255"
    error_message = "goatos-dev Terraform must target goat-os folder 188649904255."
  }
}

variable "project_id" {
  description = "Dev Google Cloud project ID."
  type        = string

  validation {
    condition     = var.project_id == "goatos-dev"
    error_message = "P4 is dev-only; project_id must be goatos-dev."
  }
}

variable "project_number" {
  description = "Dev Google Cloud project number."
  type        = string

  validation {
    condition     = var.project_number == "634659905829"
    error_message = "goatos-dev project_number must be 634659905829."
  }
}

variable "region" {
  description = "Primary Goat OS dev region."
  type        = string

  validation {
    condition     = var.region == "asia-south1"
    error_message = "goatos-dev region must remain asia-south1."
  }
}

variable "state_bucket_name" {
  description = "Imperatively bootstrapped GCS bucket used by the dev Terraform backend."
  type        = string

  validation {
    condition     = var.state_bucket_name == "goatos-dev-tf-state"
    error_message = "state_bucket_name must match the P4 bootstrap bucket."
  }
}

variable "artifact_repository_id" {
  description = "Artifact Registry Docker repository for Goat OS dev images."
  type        = string
  default     = "goatos"

  validation {
    condition     = var.artifact_repository_id == "goatos"
    error_message = "The dev Artifact Registry repository id must remain goatos."
  }
}

variable "cloud_sql_instance_name" {
  description = "goatos-dev Cloud SQL Postgres instance name."
  type        = string
  default     = "goatos-dev-core-db"

  validation {
    condition     = var.cloud_sql_instance_name == "goatos-dev-core-db"
    error_message = "The dev Cloud SQL instance name must remain goatos-dev-core-db."
  }
}

variable "cloud_sql_database_name" {
  description = "Initial Goat OS application database shell name."
  type        = string
  default     = "goatos"

  validation {
    condition     = var.cloud_sql_database_name == "goatos"
    error_message = "The initial dev database shell must be named goatos."
  }
}

variable "cloud_sql_tier" {
  description = "Smallest reasonable dev Cloud SQL tier to plan before paid apply."
  type        = string
  default     = "db-f1-micro"

  validation {
    condition     = contains(["db-f1-micro", "db-g1-small"], var.cloud_sql_tier)
    error_message = "Use a small shared-core dev tier: db-f1-micro or db-g1-small."
  }
}

variable "dev_tenant_id" {
  description = "Non-secret dev tenant id used by scheduled kernel workers."
  type        = string
  default     = "00000000-0000-4000-8000-000000000001"

  validation {
    condition     = can(regex("^[0-9a-fA-F-]{36}$", var.dev_tenant_id))
    error_message = "dev_tenant_id must be a UUID string."
  }
}

variable "backend_image_tag" {
  description = "Backend image tag consumed by Cloud Run Jobs in dev."
  type        = string
  default     = "dev"

  validation {
    condition     = length(trimspace(var.backend_image_tag)) > 0
    error_message = "backend_image_tag is required."
  }
}

variable "migration_image_tag" {
  description = "Migration image tag consumed by the goatos-dev Cloud Run migration job."
  type        = string
  default     = "dev"

  validation {
    condition     = length(trimspace(var.migration_image_tag)) > 0
    error_message = "migration_image_tag is required."
  }
}
