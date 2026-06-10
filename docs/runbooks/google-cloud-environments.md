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

## Staging Benchmark Model

`goatos-stg` is the scale rehearsal environment. Its benchmark dataset is a
fixed 1M-goat synthetic/legacy-shaped baseline, not an ever-growing import log.

Operating model:

```text
1. Load the 1M benchmark baseline once.
2. Snapshot or otherwise preserve the clean baseline.
3. Stop Cloud SQL when not testing.
4. Start Cloud SQL only for rehearsals.
5. For read/query/counter tests, run directly against the clean baseline.
6. For destructive write/load tests, use a clone or delete only that run's
   delta after the test.
7. Do not append another 1M rows on top of the baseline for each rehearsal.
```

Cost model:

```text
Fixed 1M text-only benchmark
  -> stable storage cost while stopped
  -> no running CPU/RAM charge while Cloud SQL is stopped

Running about 40 hours/month
  -> storage/backups all month
  -> compute only for those 40 running hours
```

Expected rough monthly `goatos-stg` range with no fake media and controlled
logs:

```text
Stopped storage/backups floor:  low tens to low hundreds USD/month
40 hours running compute:       tens to a few hundred USD/month
Expected normal range:          roughly low hundreds USD/month
```

This estimate is a planning range, not a committed budget. Confirm actual spend
with billing reports after the first benchmark load.

Rules that keep the benchmark cost stable:

- The benchmark remains exactly 1M goats unless a deliberate new scale target is
  approved.
- Do not keep appending event/audit/outbox/import rows from repeated write
  rehearsals onto the baseline.
- Avoid fake media in the 1M benchmark. Use tiny placeholders or metadata-only
  rows unless media throughput is the test target.
- Keep PITR and backup retention low/off for disposable mock benchmark data.
- Add a budget alert for `goatos-stg` before running large rehearsals.
- If a write test needs to mutate heavily, prefer clone -> test -> delete clone
  so the baseline stays clean.

## Guardrails

- Verify account, organization, folder, project, and repo before every cloud
  create/update/delete/IAM/billing/deploy command.
- Do not touch `goatos-sheets` unless a task explicitly says to work on legacy
  Sheets automation.
- Do not connect dev/stg services to production outbound integrations.
- Do not share prod secrets with dev or stg.
- Use `goatos-stg` for scale tests, not prod.
