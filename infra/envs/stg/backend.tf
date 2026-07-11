terraform {
  backend "gcs" {
    bucket = "goatos-stg-tf-state"
    prefix = "terraform/stg"
  }
}
