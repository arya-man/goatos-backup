resource "google_service_account" "github_deployer" {
  account_id   = "goatos-github-deploy-stg"
  display_name = "Goat OS staging GitHub deployer"
  description  = "GitHub Actions deployer for vgoats/goatos stg branch."
}

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github-goatos"
  display_name              = "GitHub vgoats/goatos"
  description               = "OIDC trust for vgoats/goatos GitHub Actions."
}

resource "google_iam_workload_identity_pool_provider" "github_actions" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-goatos-provider"
  display_name                       = "GitHub Actions"
  description                        = "Allows vgoats/goatos GitHub Actions to deploy to goatos-stg."

  attribute_mapping = {
    "google.subject"         = "assertion.sub"
    "attribute.environment"  = "assertion.environment"
    "attribute.repository"   = "assertion.repository"
    "attribute.ref"          = "assertion.ref"
    "attribute.workflow"     = "assertion.workflow"
    "attribute.workflow_ref" = "assertion.workflow_ref"
  }

  attribute_condition = <<-EOT
    assertion.repository == 'vgoats/goatos' &&
    assertion.ref == 'refs/heads/stg' &&
    assertion.workflow_ref == 'vgoats/goatos/.github/workflows/stg-deploy.yml@refs/heads/stg' &&
    assertion.environment == 'staging'
  EOT

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_project_iam_member" "github_deployer_artifact_writer" {
  project = var.project_id
  role    = "roles/artifactregistry.writer"
  member  = "serviceAccount:${google_service_account.github_deployer.email}"
}

resource "google_project_iam_member" "github_deployer_run_admin" {
  project = var.project_id
  role    = "roles/run.admin"
  member  = "serviceAccount:${google_service_account.github_deployer.email}"
}

resource "google_service_account_iam_member" "github_deployer_act_as_runtime" {
  for_each = google_service_account.runtime

  service_account_id = each.value.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.github_deployer.email}"
}

resource "google_service_account_iam_member" "github_deployer_workload_identity_user" {
  service_account_id = google_service_account.github_deployer.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.ref/refs/heads/stg"
}
