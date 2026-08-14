# Cloud Deploy Staging Runbook

Status: `goatos-stg` deployment authority is the Slack deploy button backed by
Google Cloud Build and Cloud Deploy. GitHub Actions is not a Goat OS staging
deploy path. A local operator may run the scripts only for documented
break-glass/repair, but Cloud Run staging services and jobs must be mutated by
the Cloud Deploy rollout task only.

## Why this exists

The old staging path mixed branch, workflow, and manual release assumptions. If
the image source was stale, staging could run a mixed commit: API from one build,
migration job from another build, and database schema from a third point in
time. This runbook makes Cloud Deploy the only staging deployment authority.

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
6. Update `goatos-kernel-worker-stg` to the backend image. It is one service
   with two always-warm, advisory-lock-coordinated instances.
7. Update every existing manual goatos-stg Cloud Run Job whose current image is
   the backend image, including the analytics rollup.
8. Update goatos-admin-web-stg to the admin-web image.
9. Verify API/admin/migration/kernel-worker/manual-job image skew is zero.
10. Smoke /livez, /readyz, and https://stg.dashboard.mesha.sg/login.
```

Do not move migration after service deploy. The rollout fails before migration
if the kernel worker service is absent. Do not hand-maintain a stale manual-job
list; the rollout discovers existing backend-image jobs and updates them.

## Create A Release

Normal staging releases originate from the `#goatos-stg-deploy` Slack button.
The button invokes Cloud Build manual trigger `goatos-stg-deploy-main`, which
reads latest approved `origin/main` and runs `cloudbuild.stg.yaml`. Do not
create or wait for a `main -> stg` pull request, GitHub Actions workflow, or
remote `stg` branch update as part of staging deployment.

Break-glass only: run from a clean repo checkout that points at the intended
commit:

```bash
git fetch origin main --prune
git checkout main
git reset --hard origin/main
git status --short --branch
git rev-parse HEAD
git rev-parse origin/main
gcloud config set project goatos-stg
tools/deploy/stg-clouddeploy-release.sh
```

## Deploy From Slack / Google Cloud Build

The Slack app posts a deploy card in `#goatos-stg-deploy`. Use the latest
bottom-most deploy panel; after each deploy reaches success or failure, the
automation posts a fresh panel again so no one has to scroll channel history to
find the next button. Pressing
`Deploy main to STG` calls the Cloud Run Slack bot
`goatos-stg-slack-deploy-bot`, which starts Cloud Build trigger
`goatos-stg-deploy-main`.

The trigger points at GitHub repo `vgoats/goatos`, branch `main`, and uses
`cloudbuild.stg.yaml`. It runs as:

```text
goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com
```

Cloud Build is an operator-controlled button, not a push-on-every-commit
deployment. The same release helper still refuses non-`origin/main` commits and
waits for Cloud Deploy rollout/image verification.

The card has two deploy buttons:

- `Deploy main to STG`: if the `Also distribute Android mobile` checkbox is
  unchecked, only the STG backend/web deploy runs. If checked, Cloud Build runs
  mobile only after the STG deploy step succeeds.
- `Distribute Android only`: skips the STG Cloud Deploy step and runs only the
  Android STG distribution flow from current `main`.

Mobile means all three channels, as one release: Firebase App Distribution,
Google Play Internal Testing package `sg.mesha.goatos.stg`, and
`https://mesha.sg/app.apk`. Any failure in those channels fails the Cloud Build
and posts a Slack failure alert. The bot allows only one active deployment at a
time: while any STG, STG+mobile, or Android-only build is queued or working, new
button clicks replace the panel with an "already running" status card and links
to Cloud Build / Cloud Deploy instead of starting another build.

To inspect progress or failure, open the Cloud Build link posted by Slack. The
STG deploy step links to Cloud Deploy releases/rollouts; the mobile step logs
Firebase upload, Play internal upload, and APK mirror verification.

Optional Slack alerts use Secret Manager secret:

```text
goatos-stg-deploy-slack-webhook-url
```

If the secret is absent, deploy continues and logs remain in Cloud Build.

The script refuses a dirty working tree unless `GOATOS_ALLOW_DIRTY_RELEASE=1`
is set. Dirty release is for emergency debugging only; do not use it for normal
staging handoff.

Cloud Deploy appends `-to-<target>-0001` to the release id when it creates a
rollout. Keep `RELEASE_ID` short enough for that generated rollout id to stay
under Google Cloud's 63-character resource-id limit. The release helper defaults
to `r-<12-char-sha>-<HHMMSS>` for this reason. Do not use long manual ids such
as `goatos-stg-<sha>-manual-<timestamp>`; they can create the release and then
fail before rollout creation.

## Forbidden Deploy Detours

Do not use these for Goat OS STG deployment:

- GitHub Actions or `.github/workflows/stg-deploy.yml`
- a `main -> stg` pull request as a deployment trigger
- a direct or forced push to remote `stg`
- direct `gcloud run services update`, `gcloud run jobs update`, or manual
  migration execution except documented break-glass followed by a Cloud Deploy
  release from the same commit

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
