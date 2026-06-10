# Google Cloud Environments

## Organization Boundary

Goat OS Google Cloud resources belong under the Mesha/VGoats organization only.

```text
Organization: vgoats.com
Organization ID: 563962826703
```

Do not create Goat OS resources under Heva or Slice organizations/projects.
Do not use `hevaplatform` for Goat OS work.

The legacy `goatos-sheets` project remains untouched. It is not replaced by the
new Goat OS projects.

## Created Resources

Created on 2026-06-10:

```text
Folder: goat-os
Folder ID: 188649904255
Parent: organizations/563962826703

Project: goatos-dev
Parent: folders/188649904255
Purpose: real-data debug clone, non-authoritative, no production side effects

Project: goatos-stg
Parent: folders/188649904255
Purpose: scale rehearsal with legacy-shaped and synthetic data

Project: goatos-prod
Parent: folders/188649904255
Purpose: live Goat OS truth after release
```

Existing legacy project:

```text
Project: goatos-sheets
Parent: organizations/563962826703
Status: legacy, leave untouched
```

Existing non-Goat-OS folders:

```text
system-gsuite
apps-script
```

Do not place Goat OS projects under those folders.

## Current State

The three Goat OS projects exist and are active.

Billing is not linked yet. The current CLI account can create projects/folders,
but does not currently see a billing account:

```bash
gcloud billing accounts list
```

returns no visible billing accounts.

## Billing Next Step

An owner/admin of the billing account must grant this account access:

```bash
USER_EMAIL=ravi@mesha.sg
BILLING_ACCOUNT_ID=PASTE_BILLING_ACCOUNT_ID_HERE

gcloud billing accounts add-iam-policy-binding "$BILLING_ACCOUNT_ID" \
  --member="user:$USER_EMAIL" \
  --role="roles/billing.user"

gcloud billing accounts add-iam-policy-binding "$BILLING_ACCOUNT_ID" \
  --member="user:$USER_EMAIL" \
  --role="roles/billing.viewer"
```

After billing is visible, link all three projects:

```bash
BILLING_ACCOUNT_ID=PASTE_BILLING_ACCOUNT_ID_HERE

gcloud billing projects link goatos-dev \
  --billing-account="$BILLING_ACCOUNT_ID"

gcloud billing projects link goatos-stg \
  --billing-account="$BILLING_ACCOUNT_ID"

gcloud billing projects link goatos-prod \
  --billing-account="$BILLING_ACCOUNT_ID"
```

## Verification Commands

Check active account and make sure no default project is accidentally set:

```bash
gcloud config list --format="text(core.account,core.project)"
```

Expected account:

```text
ravi@mesha.sg
```

Project may be unset. Do not default to `hevaplatform`.

Verify folder:

```bash
gcloud resource-manager folders list \
  --organization=563962826703 \
  --format="table(displayName,name,parent)"
```

Verify projects directly:

```bash
gcloud projects describe goatos-dev \
  --format="yaml(projectId,name,lifecycleState,parent)"

gcloud projects describe goatos-stg \
  --format="yaml(projectId,name,lifecycleState,parent)"

gcloud projects describe goatos-prod \
  --format="yaml(projectId,name,lifecycleState,parent)"
```

## Resource Plan After Billing

After billing is linked, provision each environment separately:

```text
goatos-dev
  Cloud SQL, GCS, Pub/Sub, Secret Manager, logs/metrics
  real-data clone for debugging, no production outbound side effects

goatos-stg
  Cloud SQL, GCS, Pub/Sub, Secret Manager, logs/metrics
  scale data and load tests

goatos-prod
  Cloud SQL, GCS, Pub/Sub, Secret Manager, logs/metrics
  live production data and operations
```

Keep service accounts, buckets, databases, Pub/Sub topics, and secrets separate
per project.

## Guardrails

- Verify account, organization, folder, project, and repo before every cloud
  create/update/delete/IAM/billing/deploy command.
- Do not touch `goatos-sheets` unless a task explicitly says to work on legacy
  Sheets automation.
- Do not connect dev/stg services to production outbound integrations.
- Do not share prod secrets with dev or stg.
- Use `goatos-stg` for scale tests, not prod.
