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

Project: goatos-stg
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

The three Goat OS projects exist and are active under the `goat-os` folder.

Billing is linked for all three Goat OS projects.

```text
Billing account: 01FEDE-96BCB3-76D992
Billing display name: My Billing Account

goatos-dev   billingEnabled: true
goatos-stg   billingEnabled: true
goatos-prod  billingEnabled: true
```

`goatos-prod` was linked after Google granted additional billing project quota
on 2026-06-10.

## Billing Commands

Use these only when a new Goat OS project needs billing linked to the same
account.

```bash
USER_EMAIL=ravi@mesha.sg
BILLING_ACCOUNT_ID=01FEDE-96BCB3-76D992

gcloud billing accounts add-iam-policy-binding "$BILLING_ACCOUNT_ID" \
  --member="user:$USER_EMAIL" \
  --role="roles/billing.user"

gcloud billing accounts add-iam-policy-binding "$BILLING_ACCOUNT_ID" \
  --member="user:$USER_EMAIL" \
  --role="roles/billing.viewer"
```

Link a project:

```bash
BILLING_ACCOUNT_ID=01FEDE-96BCB3-76D992

gcloud billing projects link PROJECT_ID \
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
gcloud projects describe goatos-stg \
  --format="yaml(projectId,name,lifecycleState,parent)"

gcloud projects describe goatos-stg \
  --format="yaml(projectId,name,lifecycleState,parent)"

gcloud projects describe goatos-prod \
  --format="yaml(projectId,name,lifecycleState,parent)"
```

Verify billing:

```bash
for project in goatos-dev goatos-stg goatos-prod; do
  gcloud billing projects describe "$project" \
    --format="yaml(projectId,billingAccountName,billingEnabled)"
done
```

## goatos-stg Read-Only Cloud SQL Access

> goatos-dev is ARCHIVED (maintainer, 2026-08-05). Every read-only data pull
> goes to goatos-stg. This section used to name dev throughout, which sent
> readers at a dead project.

Use this path when a task asks for real Google-backed data, a dashboard issue
CSV, Cloud SQL data, or "use gcloud/browser login". The dashboard UI can provide
visual context, but it is not the extraction source.

Before connecting, verify that the local shell is in the Mesha/VGoats context:

```bash
gcloud auth list --format="table(account,status)"
gcloud config list --format="text(core.account,core.project)"
git -C /path/to/goatos remote get-url origin \
  | sed -E 's#(https://)[^/@]+@#\1***@#'
gcloud organizations list --format="table(displayName,name,directoryCustomerId)"
gcloud projects describe goatos-stg --format="json(projectId,name,parent)"
```

Expected context:

```text
Account: ravi@mesha.sg
Project: goatos-stg
Organization: vgoats.com / organizations/563962826703
Folder: goat-os / folders/188649904255
Repo: https://github.com/vgoats/goatos.git
```

If the account or project is wrong, correct it before doing anything else:

```bash
gcloud config set account ravi@mesha.sg
gcloud config set project goatos-stg
```

If the gcloud user token is expired, use browser-code auth:

```bash
gcloud auth login --no-launch-browser --brief
```

Open the printed URL, complete Google login as `ravi@mesha.sg`, then paste the
verification code back into the CLI. Do not enter a Google password directly
into the terminal.

For data pulls, discover the current runtime resources instead of guessing:

```bash
gcloud run services describe goatos-api-dev \
  --project=goatos-stg \
  --region=asia-south1 \
  --format=json

gcloud sql instances describe goatos-stg-core-db \
  --project=goatos-stg \
  --format="json(name,connectionName,ipAddresses,settings.ipConfiguration)"

gcloud secrets list --project=goatos-stg
```

Current dev database facts:

```text
Cloud SQL instance: goatos-stg-core-db
Connection name:    goatos-stg:asia-south1:goatos-stg-core-db
Database:           goatos
Runtime DB user:    goatos_app
DB URL secret:      goatos-stg-database-url
Admin tenant secret: goatos-stg-admin-web-tenant-id
```

The proxy listens on 5455, NEVER 5433: 5433 is the maintainer's own local
seeded Postgres (`goatos-local-current`). Binding the proxy there silently
puts stg data behind the port every local tool already treats as the dev DB.

Prefer an explicit OAuth access token for the Cloud SQL Auth Proxy. Local ADC
can be stale or point at another business account, which commonly fails with
`invalid_rapt`. Do not write the bearer token to a temp file or shell profile.

```bash
CSQL_PROXY_TOKEN="$(gcloud auth print-access-token --account=ravi@mesha.sg)" \
cloud-sql-proxy \
  --port 5455 \
  goatos-stg:asia-south1:goatos-stg-core-db
```

In another shell, read the secret-backed DSN without printing it, override the
host/port to the local proxy, and run read-only SQL. `psql` may not be installed
on every Codex host; Python with `psycopg2` is an acceptable local client.

Fast `psql` path on Ravi's Mac:

```bash
DB_URL="$(gcloud secrets versions access latest \
  --secret=goatos-stg-database-url \
  --project=goatos-stg)"

DB_PASS="$(DB_URL="$DB_URL" python3 - <<'PY'
import os
from urllib.parse import urlparse, unquote

print(unquote(urlparse(os.environ["DB_URL"]).password or ""))
PY
)"

PGPASSWORD="$DB_PASS" /opt/homebrew/opt/libpq/bin/psql \
  -h 127.0.0.1 \
  -p 5455 \
  -U goatos_app \
  -d goatos
```

Do not rebuild the secret URL by hand. The secret uses a Cloud SQL Unix-socket
host (`host=/cloudsql/...`), and its password is percent-encoded; naive URL
rewrites commonly fail with `password authentication failed`. If port `5455` is
already occupied by a stale proxy, start your own proxy on a clearly named
alternate port such as `5456` and change only the `-p` value above.

```bash
DB_URL="$(gcloud secrets versions access latest \
  --secret=goatos-stg-database-url \
  --project=goatos-stg)"

TENANT_ID="$(gcloud secrets versions access latest \
  --secret=goatos-stg-admin-web-tenant-id \
  --project=goatos-stg)"

DB_URL="$DB_URL" TENANT_ID="$TENANT_ID" python3 - <<'PY'
import os
from urllib.parse import urlparse
import psycopg2

url = urlparse(os.environ["DB_URL"])
conn = psycopg2.connect(
    dbname=url.path.lstrip("/"),
    user=url.username,
    password=url.password,
    host="127.0.0.1",
    port=5455,
    sslmode="disable",
)
conn.set_session(readonly=True, autocommit=True)
with conn.cursor() as cur:
    cur.execute("select current_database(), current_user")
    print(cur.fetchone())
conn.close()
PY
```

For dashboard-matching exports, filter by the admin-web tenant secret when the
table is tenant-scoped. Export CSVs from Postgres query results, not from Chrome
or dashboard DOM scraping.

When done, stop `cloud-sql-proxy`.

No write SQL, migrations, IAM changes, deploys, billing changes, or Cloud
resource changes are allowed in this workflow unless the task explicitly asks
for them and the active account/project/org have been re-verified first.

## Resource Plan After Billing

After billing is linked, provision each environment separately:

```text
goatos-dev  (ARCHIVED 2026-08-05 -- do not target; kept here only so an old
  reference in a ticket or script is recognisable)
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

Canonical benchmark profile:

```text
profile_id: goatos-stg-1m-benchmark-v1
project: goatos-stg
dataset: fixed 1M-goat synthetic/legacy-shaped baseline
database: Cloud SQL for PostgreSQL 16 in goatos-stg
schema identity: pinned in the profile report by repo git SHA, highest migration
  version/file, and ordered migration checksum digest before loading data;
  historical hot-table index warnings are not safe to run after data is already
  at benchmark scale
seed/data identity: pinned in the profile report by seed generator command,
  seed generator git SHA, seed config version, input fixture checksum manifest,
  and loaded dataset checksum/row-count manifest
write-path reads/writes: primary instance only; read replicas are measured
  separately and cannot hide primary write-path bottlenecks
infra shape: Cloud SQL tier, storage type/size/autoscaling, replica count,
  app/worker CPU+memory, replica counts, DB pool sizes, Pub/Sub retry/DLQ
  policy, and worker concurrency are pinned in each certification report
profile changes: changing any infra shape field, repo git SHA, migration head,
  migration checksum digest, seed generator/config, input fixture checksum, or
  loaded dataset checksum creates a new profile id before certification; do not
  compare pass/fail results across profiles
```

Until a concrete profile report exists for `goatos-stg-1m-benchmark-v1`, any
large run is a rehearsal only. It must not be described as the canonical 1M
certification baseline.

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
