# goatos-dev Terraform

Status: P4 remote state bootstrap plus P7-preapply Layer 1 foundation plan. This
does not make the app live, and no Layer 1 Terraform apply has been run.

## State Backend

```text
Project:      goatos-dev
Project no:   634659905829
Org:          vgoats.com / organizations/563962826703
Folder:       goat-os / folders/188649904255
Region:       asia-south1
Bucket:       goatos-dev-tf-state
State prefix: terraform/dev
Backend:      gcs
```

The state bucket was bootstrapped imperatively before Terraform backend init. It
is not defined as a Terraform resource in this environment, because Terraform
cannot safely own the same bucket it uses as its backend.

Commands run:

```bash
gcloud storage buckets create gs://goatos-dev-tf-state \
  --project=goatos-dev \
  --location=asia-south1 \
  --uniform-bucket-level-access \
  --public-access-prevention

gcloud storage buckets update gs://goatos-dev-tf-state \
  --project=goatos-dev \
  --versioning
```

Verification command:

```bash
gcloud storage buckets describe gs://goatos-dev-tf-state \
  --project=goatos-dev \
  --format=json
```

Expected bucket posture:

```text
location: ASIA-SOUTH1
uniform_bucket_level_access: true
public_access_prevention: enforced
versioning_enabled: true
```

Terraform init command run:

```bash
env -u GOOGLE_APPLICATION_CREDENTIALS \
  -u GOOGLE_ADC_TOKEN \
  -u GOOGLE_API_KEY \
  GOOGLE_OAUTH_ACCESS_TOKEN="$(gcloud auth print-access-token --account=ravi@mesha.sg)" \
  terraform -chdir=infra/envs/dev init -input=false -reconfigure
```

The local shell had an Application Default Credential pointing outside
VGoats/Goat OS. Terraform commands for this environment must use verified
Mesha/VGoats credentials only; unset any unrelated ADC/service-account env vars
before init, plan, or apply.

This bucket is only for Terraform state metadata. Do not store goat data, legacy
exports, secrets, container images, app artifacts, or migration payloads here.

Using a state bucket inside `goatos-dev` is acceptable for dev simplicity. The
`goatos-stg` and `goatos-prod` state design can be stricter later, but do not
create or modify those environments during dev bring-up.

Do not delete, empty, rename, lock, or otherwise modify this bucket unless the
operator explicitly approves a Terraform-state teardown or migration.

## API and Budget Gate

P5 enabled only the approved `goatos-dev` APIs:

```text
artifactregistry.googleapis.com
billingbudgets.googleapis.com
cloudbuild.googleapis.com
cloudscheduler.googleapis.com
compute.googleapis.com
identitytoolkit.googleapis.com
pubsub.googleapis.com
run.googleapis.com
secretmanager.googleapis.com
sqladmin.googleapis.com
```

Compute API enablement created the default VPC and default firewall rules. They
are unused for the current Cloud Run plus Cloud SQL connector/socket plan; do
not delete or modify them unless explicitly approved.

P6 budget:

```text
Billing account: 01FEDE-96BCB3-76D992
Budget:          Goat OS dev monthly budget
Budget id:       f90ceaf4-efea-4c4f-9059-32b57b349992
Scope:           projects/634659905829 (goatos-dev only)
Amount:          INR 4,750 monthly (billing-account currency; about USD 50)
Alerts:          50%, 80%, 100% current spend
```

## Layer 1 Plan

Layer 1 Terraform is limited to foundation resources:

```text
Artifact Registry Docker repo
Cloud SQL Postgres instance shell + database shell
Secret Manager containers only, no secret versions
Pub/Sub outbox topic, analytics subscription, DLQ, message storage in asia-south1
Pub/Sub service-agent IAM for DLQ correctness
Runtime service accounts
Cloud SQL client, secret accessor, and topic publisher IAM
```

Planned names:

```text
Artifact Registry: asia-south1-docker.pkg.dev/goatos-dev/goatos
Cloud SQL:         goatos-dev-core-db
Database:          goatos
Outbox topic:      goatos-dev-outbox-events
DLQ topic:         goatos-dev-outbox-events-dlq
Subscription:      goatos-dev-analytics-export
```

Cloud SQL is planned with `activation_policy = "NEVER"` so the instance starts
stopped after apply and can stay stopped between work sessions until Layer 2
needs migrations or app connectivity. It still has storage cost after apply.

Connectivity is public IP plus future Cloud SQL connector/socket:

```text
/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db
```

Do not add authorized networks, private IP, or a Serverless VPC Access connector
for this dev batch.

Forbidden in this stack until a later approved layer:

```text
terraform apply
Cloud Run services/jobs
Scheduler jobs
Load balancer/serverless NEGs/DNS
Cloud SQL users or passwords
Secret Manager secret versions
Firebase Hosting or Firebase App Hosting
Image pushes
Migrations
Legacy imports
```

Terraform plan command run:

```bash
env -u GOOGLE_APPLICATION_CREDENTIALS \
  -u GOOGLE_CREDENTIALS \
  -u GOOGLE_OAUTH_ACCESS_TOKEN \
  -u CLOUDSDK_AUTH_CREDENTIAL_FILE_OVERRIDE \
  GOOGLE_OAUTH_ACCESS_TOKEN="$(gcloud auth print-access-token --account=ravi@mesha.sg)" \
  terraform -chdir=infra/envs/dev plan -input=false -no-color
```

Plan result: `42 to add, 0 to change, 0 to destroy`. No plan file was written;
do not commit `tfplan`, `*.tfplan`, `.terraform/`, or `*.tfstate*`.
