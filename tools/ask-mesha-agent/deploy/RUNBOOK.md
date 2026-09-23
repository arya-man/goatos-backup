# Ask Mesha agent on Cloud Run (STG) — one-time setup, deploy, rollback

Service `goatos-ask-mesha-stg` (asia-south1, project `goatos-stg`) runs
`tools/ask-mesha-agent` from image
`asia-south1-docker.pkg.dev/goatos-stg/goatos/ask-mesha-agent:<sha12>`, built and
deployed by the existing `cloudbuild.stg.yaml` path (steps
`build-ask-mesha-agent-image`, `deploy-ask-mesha-agent` →
`tools/ask-mesha-agent/deploy/deploy-stg.sh`). Both are no-ops unless the build
sets `_ASK_MESHA_DEPLOY=true`, so the pipeline change lands before this infra.

Nothing below has been run. Ravi reviews and runs it as `ravi@mesha.sg`.

## 0. Preconditions

```bash
gcloud config get-value account   # ravi@mesha.sg
export PROJECT=goatos-stg REGION=asia-south1
export SA=goatos-ask-mesha-stg@${PROJECT}.iam.gserviceaccount.com
export DEPLOYER=goatos-github-deploy-stg@${PROJECT}.iam.gserviceaccount.com
export ADMIN_WEB_SA=goatos-admin-web-stg@${PROJECT}.iam.gserviceaccount.com  # verified via run services describe
export INSTANCE=goatos-stg-core-db BUCKET=goatos-stg-ask-mesha
```

## 1. Runtime service account (least privilege)

```bash
gcloud iam service-accounts create goatos-ask-mesha-stg --project=$PROJECT \
  --display-name="Ask Mesha agent (STG)"
gcloud projects add-iam-policy-binding $PROJECT \
  --member=serviceAccount:$SA --role=roles/cloudsql.client --condition=None
```

## 2. Uploads bucket (private, uniform access, 180-day lifecycle)

```bash
gcloud storage buckets create gs://$BUCKET --project=$PROJECT --location=$REGION \
  --uniform-bucket-level-access --public-access-prevention
cat > /tmp/ask-mesha-lifecycle.json <<'JSON'
{"rule":[{"action":{"type":"Delete"},"condition":{"age":180}}]}
JSON
gcloud storage buckets update gs://$BUCKET --lifecycle-file=/tmp/ask-mesha-lifecycle.json
gcloud storage buckets add-iam-policy-binding gs://$BUCKET \
  --member=serviceAccount:$SA --role=roles/storage.objectAdmin
```

## 3. Writable chat-storage DB (separate from `mesha_ceo_readonly`)

```bash
gcloud sql databases create ask_mesha --instance=$INSTANCE --project=$PROJECT
ASK_PW="$(openssl rand -base64 32 | tr -d '/+=\n')"
gcloud sql users create ask_mesha --instance=$INSTANCE --project=$PROJECT --password="$ASK_PW"
```

As the instance admin, keep `ask_mesha` confined to its own database:

```sql
-- connected to database ask_mesha
REVOKE ALL ON DATABASE ask_mesha FROM PUBLIC;
GRANT CONNECT, CREATE, TEMP ON DATABASE ask_mesha TO ask_mesha;
CREATE SCHEMA IF NOT EXISTS ask_mesha AUTHORIZATION ask_mesha;
-- connected to database goatos
REVOKE CONNECT ON DATABASE goatos FROM ask_mesha;
```

## 3b. Claude access (pick ONE; deploy flag `ASK_MESHA_CLAUDE_AUTH`)

**vertex (default, recommended):** no key; Claude is billed to the GCP project.

```bash
# Enable Claude Sonnet 5 / Opus 5.5 once in Console → Vertex AI → Model Garden (accept terms), then:
gcloud services enable aiplatform.googleapis.com --project=$PROJECT
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:goatos-ask-mesha-stg@$PROJECT.iam.gserviceaccount.com" --role=roles/aiplatform.user
```

Region: `ASK_MESHA_VERTEX_REGION` (default `us-east5`; use one where the models are enabled).

**api-key:** Anthropic Console key (set a monthly spend limit there) → secret `goatos-stg-ask-mesha-anthropic-api-key` (§4).

**oauth:** `claude setup-token` token → secret `goatos-stg-ask-mesha-claude-oauth-token`. Personal
Pro/Max plans are for the subscriber's own use; a service used by several people should use
vertex or api-key instead.

## 3c. Spend cap ($100/month)

The agent enforces the cap itself: once this month's summed answer cost reaches
`ASK_MESHA_MONTHLY_BUDGET_USD` (default 100) new questions get a "budget reached" reply
without calling Claude; `ASK_MESHA_PER_ANSWER_BUDGET_USD` (default 1) aborts a single
runaway answer. GCP budgets only alert (they never stop spend), so add one as a backstop:

```bash
BILLING=$(gcloud billing projects describe $PROJECT --format='value(billingAccountName)' | sed 's#billingAccounts/##')
gcloud billing budgets create --billing-account=$BILLING \
  --display-name="Ask Mesha Claude (Vertex) $100" --budget-amount=100USD \
  --filter-projects=projects/$PROJECT --filter-services=services/aiplatform.googleapis.com \
  --threshold-rule=percent=0.5 --threshold-rule=percent=0.8 --threshold-rule=percent=1.0
```

(That filter covers all Vertex AI use in the project; Ask Mesha is the only Claude user today.)
With `api-key`, also set a $100 monthly limit in the Anthropic Console.

## 4. Secrets (values piped from stdin, never echoed)

```bash
# Anthropic API key: paste into stdin, then Ctrl-D
gcloud secrets create goatos-stg-ask-mesha-anthropic-api-key --project=$PROJECT \
  --replication-policy=automatic --data-file=-
printf 'postgresql://ask_mesha:%s@/ask_mesha?host=/cloudsql/%s:%s:%s' \
  "$ASK_PW" $PROJECT $REGION $INSTANCE | \
  gcloud secrets create goatos-stg-ask-mesha-db-url --project=$PROJECT \
  --replication-policy=automatic --data-file=-
unset ASK_PW
for s in goatos-stg-ask-mesha-anthropic-api-key goatos-stg-ask-mesha-db-url mesha-ceo-readonly-db-url; do
  gcloud secrets add-iam-policy-binding $s --project=$PROJECT \
    --member=serviceAccount:$SA --role=roles/secretmanager.secretAccessor
done
```

`mesha-ceo-readonly-db-url` already exists; `deploy/entrypoint.sh` turns it
(URL or `host=... user=...` DSN) into `$ASK_MESHA_STATE_DIR/.pgenv` at startup.

## 5. Deployer + invoker permissions

```bash
# Cloud Build deployer may deploy the service and act as its runtime SA.
gcloud iam service-accounts add-iam-policy-binding $SA --project=$PROJECT \
  --member=serviceAccount:$DEPLOYER --role=roles/iam.serviceAccountUser
# (deployer already holds roles/run.developer|admin for the existing services;
#  confirm: gcloud projects get-iam-policy $PROJECT --flatten=bindings --filter="bindings.members:$DEPLOYER")
# The preflight reads the SA/secrets/bucket metadata:
gcloud secrets add-iam-policy-binding goatos-stg-ask-mesha-anthropic-api-key --project=$PROJECT \
  --member=serviceAccount:$DEPLOYER --role=roles/secretmanager.viewer
gcloud secrets add-iam-policy-binding goatos-stg-ask-mesha-db-url --project=$PROJECT \
  --member=serviceAccount:$DEPLOYER --role=roles/secretmanager.viewer
gcloud storage buckets add-iam-policy-binding gs://$BUCKET \
  --member=serviceAccount:$DEPLOYER --role=roles/storage.legacyBucketReader
```

After the first deploy creates the service:

```bash
gcloud run services add-iam-policy-binding goatos-ask-mesha-stg --project=$PROJECT --region=$REGION \
  --member=serviceAccount:$ADMIN_WEB_SA --role=roles/run.invoker
```

## 6. Deploy

Trigger the normal `goatos-stg-deploy-main` build (launcher / Slack) with the
extra substitutions:

1. First run: `_ASK_MESHA_DEPLOY=true` (service only). Grant `run.invoker` (§5).
2. Second run: `_ASK_MESHA_DEPLOY=true,_ASK_MESHA_WIRE_ADMIN_WEB=true` — sets
   `CEO_AI_AGENT_URL` and `CEO_AI_AGENT_AUDIENCE` (both = service URL) on
   `goatos-admin-web-stg` with `--update-env-vars` and shifts traffic to that
   revision. Cloud Deploy releases also use `--update-env-vars`, so the wiring
   survives later deploys. (If Terraform `infra/envs/stg` is applied to
   `admin_web`, add the two env vars there too or it will drop them.)

Smoke: `/api/ceo-ai/starters` in admin-web STG returns 200 from the agent;
`gcloud run services logs read goatos-ask-mesha-stg --region=$REGION --limit=50`.

## Rollback

Fastest (admin-web back to the backend `/ceo-ai/*`):

```bash
gcloud run services update goatos-admin-web-stg --project=$PROJECT --region=$REGION \
  --remove-env-vars=CEO_AI_AGENT_URL,CEO_AI_AGENT_AUDIENCE --no-traffic --quiet
REV=$(gcloud run services describe goatos-admin-web-stg --project=$PROJECT --region=$REGION \
  --format='value(status.latestCreatedRevisionName)')
gcloud run services update-traffic goatos-admin-web-stg --project=$PROJECT --region=$REGION \
  --to-revisions=$REV=100 --quiet
```

Bad agent image only: `gcloud run services update-traffic goatos-ask-mesha-stg
--to-revisions=<previous-rev>=100`. Stop spend: set `--min-instances=0` or
delete the service; the bucket and `ask_mesha` DB keep chat history.
