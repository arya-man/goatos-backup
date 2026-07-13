# Cloud Deploy Staging Runbook

Status: `goatos-stg` deployment authority is Cloud Deploy. GitHub Actions,
Cloud Build, or a local operator may build images and create a release, but
Cloud Run staging services and jobs must be mutated by the Cloud Deploy rollout
task only.

## Why this exists

The old staging path let one workflow build images, run migrations, update API,
update worker jobs, update admin-web, and smoke the result. If the workflow or
image source was stale, staging could run a mixed commit: API from one build,
migration job from another build, and database schema from a third point in
time.

Cloud Deploy makes the release the unit of change. A staging release carries
exactly four values:

```text
customTarget/commitSha
customTarget/backendImage
customTarget/migrationImage
customTarget/adminWebImage
```

The rollout task applies those values in one ordered sequence.

## Authoritative Files

```text
deploy/clouddeploy/stg/clouddeploy.yaml
deploy/clouddeploy/stg/skaffold.yaml
tools/deploy/stg-clouddeploy-task.sh
tools/deploy/stg-clouddeploy-release.sh
.github/workflows/stg-deploy.yml
```

`clouddeploy.yaml` defines a custom target because Goat OS staging deploys both
Cloud Run services and Cloud Run Jobs. The custom target task is intentionally
scripted so migrations run before API/admin/worker image changes.

## Required Order

The rollout task must keep this order:

```text
1. Validate project goatos-stg, project number 514832198871, region asia-south1.
2. Validate all release images live under asia-south1-docker.pkg.dev/goatos-stg/goatos.
3. Update goatos-stg-migrate to the migration image.
4. Execute goatos-stg-migrate and wait for success.
5. Update goatos-api-stg to the backend image.
6. Update every existing goatos-stg Cloud Run Job whose current image is the backend image.
7. Update goatos-admin-web-stg to the admin-web image.
8. Verify API/admin/migration/worker image skew is zero.
9. Smoke /livez, /readyz, and https://stg.dashboard.mesha.sg/login.
```

Do not move migration after service deploy. Do not hand-maintain a stale worker
job list; the rollout discovers existing backend-image jobs and updates them.

## Create A Release Locally (Break Glass Only)

Normal staging releases must originate from GitHub merging the same-repository
`main -> stg` pull request. Do not use this helper as an alternative staging
promotion path. It exists only for an explicitly-authorized break-glass repair
after the operator verifies the exact already-approved commit and cloud target.

Run from a clean repo checkout that points at the intended commit:

```bash
gcloud config set project goatos-stg
tools/deploy/stg-clouddeploy-release.sh
```

The script refuses a dirty working tree unless `GOATOS_ALLOW_DIRTY_RELEASE=1`
is set. Dirty release is for emergency debugging only; do not use it for normal
staging handoff.

## GitHub Workflow Behavior

`.github/workflows/stg-deploy.yml` is now a release producer:

```text
merged main -> stg SHA verification -> build images -> push images ->
gcloud deploy releases create
```

The SHA verification happens before OIDC authentication. It requires the exact
current `stg` SHA to be the merge commit of a closed, merged, same-repository
`main -> stg` PR. Direct pushes and dispatches from `main` or agent branches
fail before any cloud write.

It must not call `gcloud run services update`, `gcloud run jobs update`, or
`gcloud run jobs execute` directly. Those commands belong inside the Cloud
Deploy custom target rollout task.

## Apply Or Repair The Pipeline

Verify target first:

```bash
gcloud config list --format="text(core.account,core.project)"
gcloud projects describe goatos-stg --format="value(projectId,projectNumber,parent.type,parent.id)"
```

Expected:

```text
account ravi@mesha.sg
project goatos-stg
projectNumber 514832198871
folder 188649904255
```

Enable and apply:

```bash
gcloud services enable clouddeploy.googleapis.com --project=goatos-stg
gcloud deploy apply --project=goatos-stg --region=asia-south1 --file=deploy/clouddeploy/stg/clouddeploy.yaml
```

IAM expected for the deployer:

```text
goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com
  roles/artifactregistry.writer
  roles/run.admin
  roles/clouddeploy.releaser
  roles/clouddeploy.jobRunner
  roles/iam.serviceAccountUser on staging runtime service accounts
```

Cloud Deploy's service agent must be able to act as that deployer service
account. Terraform source for this is in `infra/envs/stg/github_actions.tf`.

## Verification Commands

```bash
gcloud deploy delivery-pipelines describe goatos-stg \
  --project=goatos-stg \
  --region=asia-south1

gcloud deploy targets describe goatos-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --delivery-pipeline=goatos-stg

gcloud run services describe goatos-api-stg \
  --project=goatos-stg \
  --region=asia-south1 \
  --format="value(metadata.labels.commit_sha,metadata.labels.deployed_by,status.latestReadyRevisionName)"
```

## Break Glass

Direct `gcloud run services update` or `gcloud run jobs update` is break-glass
only. If used, immediately create a follow-up Cloud Deploy release from the same
commit and record why the bypass happened.
