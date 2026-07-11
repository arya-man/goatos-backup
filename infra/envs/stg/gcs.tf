resource "google_storage_bucket" "proof_media" {
  name                        = "goatos-stg-media"
  location                    = var.region
  force_destroy               = false
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  labels = local.labels

  depends_on = [google_project_service.enabled]
}

resource "google_service_account" "proof_media_signer" {
  account_id   = "goatos-proof-signer-stg"
  display_name = "Goat OS staging proof media signer"
  description  = "Signs direct GCS proof media upload/download URLs for staging."
}

resource "google_storage_bucket_iam_member" "proof_media_signer_object_admin" {
  bucket = google_storage_bucket.proof_media.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.proof_media_signer.email}"
}
