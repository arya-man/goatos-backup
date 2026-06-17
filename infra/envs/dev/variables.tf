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
