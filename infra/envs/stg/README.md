# goatos-stg Terraform And Deployment Handoff

Status: staging infrastructure source is now aligned with the live
`goatos-stg` environment and includes the GitHub Actions deploy identity. Live
resources still need to be imported into the `gs://goatos-stg-tf-state`
Terraform backend before a broad `terraform apply`; do not apply an empty state
against staging.

## Verified Target

```text
Organization: vgoats.com / organizations/563962826703
Folder:       goat-os / folders/188649904255
Project:      goatos-stg
Project no:   514832198871
Region:       asia-south1
Backend:      gs://goatos-stg-tf-state/terraform/stg
Repo:         https://github.com/vgoats/goatos
```

The backend bucket exists and is for Terraform state only. Do not store goat
data, images, secrets, app artifacts, or migration payloads in it.

## Terraform Coverage

The staging composition models these live resource groups:

```text
Artifact Registry: asia-south1-docker.pkg.dev/goatos-stg/goatos
Cloud SQL:         goatos-stg-core-db / database goatos
Cloud SQL policy:  activation_policy = ALWAYS, db-g1-small, 20GB SSD
Secret Manager:   containers only, no secret versions
IAM:              runtime SAs, GitHub OIDC deployer, least-scope bindings
Pub/Sub:          outbox topic, analytics/domain subscriptions, DLQ
Cloud Tasks:      near-term kernel queue
Cloud Run:        goatos-api-stg and goatos-admin-web-stg services
Cloud Run Jobs:   migration and scheduled kernel workers
Scheduler:        job invocations in Asia/Kolkata time
GCS:              goatos-stg-media bucket and goatos-proof-signer-stg
Monitoring:       Cloud Run errors, outbox dead letters, DLQ backlog, Cloud SQL CPU
```

Current staging keeps both `goatos-api-stg` and `goatos-admin-web-stg` publicly
invokable at the Cloud Run layer. Goat OS JWKS/RBAC remains the authorization
boundary for protected app routes. `/livez` and `/readyz` are used by deployment
smoke checks.

## GitHub Deployment Identity

The staging deploy workflow authenticates with GitHub OIDC, not a downloaded
service-account key.

```text
Workload identity pool:     github-goatos
Provider:                   github-goatos-provider
Deploy service account:     goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com
Repo variables:
  GOATOS_STG_WIF_PROVIDER
  GOATOS_STG_DEPLOYER_SERVICE_ACCOUNT
```

The deployer can write images to Artifact Registry, update/execute Cloud Run,
and act as the existing staging runtime service accounts. It should not receive
Secret Manager admin access or database credentials.

## Branch Automation

```text
main -> stg pull request:
  .github/workflows/stg-pr-gate.yml
  Runs backend DB/API/migration gates, admin-web build gates, and stg release APK build.

push/merge to stg:
  .github/workflows/stg-deploy.yml
  Builds backend/migrate/admin-web images, runs migrations, updates services/jobs,
  and smokes /livez, /readyz, and https://stg.dashboard.mesha.sg/login.
```

The deploy workflow skips only when the pushed commit message contains
`[skip stg deploy]`, which is reserved for branch bootstrapping or workflow-only
seeding.

## Secret Values

Terraform creates or adopts secret containers only. Secret versions remain an
operator/Secret Manager action and must never be committed. Live staging secrets
include:

```text
goatos-stg-database-url
goatos-stg-db-app-credential
goatos-stg-auth-issuer
goatos-stg-auth-audience
goatos-stg-auth-jwks-url
goatos-stg-auth-allowed-emails
goatos-stg-bulk-import-preview-signing-key
goatos-stg-gcs-service-account-json
goatos-stg-firebase-web-config
goatos-stg-google-oauth-web-credential
goatos-stg-notification-slack-webhook-url
goatos-stg-notification-incident-webhook-url
```

If the app keeps using V4 signed URLs from a private key, create the key only in
the verified `goatos-stg` context and load the JSON into
`goatos-stg-gcs-service-account-json`. A safer follow-up is to replace
private-key signing with IAMCredentials `signBlob`.

## Safe Local Checks

Read-only/source checks:

```bash
terraform -chdir=infra/envs/stg fmt -check
terraform -chdir=infra/envs/stg init -backend=false
terraform -chdir=infra/envs/stg validate
```

Before any real `terraform apply`, import the live resources into the staging
state first. Applying without imports can fail on duplicate names or overwrite
manual configuration.
