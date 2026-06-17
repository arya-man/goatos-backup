terraform {
  backend "gcs" {
    bucket = "goatos-dev-tf-state"
    prefix = "terraform/dev"
  }
}
