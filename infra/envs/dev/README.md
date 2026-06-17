# goatos-dev Terraform

P4 status: remote state bootstrap and minimal Terraform backend/provider wiring.
This does not make the app live.

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
