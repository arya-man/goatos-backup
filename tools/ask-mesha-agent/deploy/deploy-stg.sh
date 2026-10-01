#!/usr/bin/env bash
# Deploys goatos-ask-mesha-stg from cloudbuild.stg.yaml (step deploy-ask-mesha-agent).
# Only runs when the build sets _ASK_MESHA_DEPLOY=true. Preflights the one-time infra
# from RUNBOOK.md and fails loudly (never creates infra) if any piece is missing.
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
REGION="${REGION:-asia-south1}"
SERVICE="${ASK_MESHA_SERVICE:-goatos-ask-mesha-stg}"
RUNTIME_SA="${ASK_MESHA_RUNTIME_SA:-goatos-ask-mesha-stg@${PROJECT_ID}.iam.gserviceaccount.com}"
CLOUDSQL_INSTANCE="${ASK_MESHA_CLOUDSQL_INSTANCE:-goatos-stg:asia-south1:goatos-stg-core-db}"
UPLOADS_BUCKET="${ASK_MESHA_UPLOADS_BUCKET:-goatos-stg-ask-mesha}"
# Model: Gemini on Vertex AI, called with the runtime SA (ADC from the metadata server).
# No model API key or secret exists; the SA needs roles/aiplatform.user on the project.
# Newest Gemini models are served on the Vertex global endpoint (verified 2026-10-01).
GEMINI_PROJECT="${ASK_MESHA_GEMINI_PROJECT:-$PROJECT_ID}"
GEMINI_LOCATION="${ASK_MESHA_GEMINI_LOCATION:-global}"
# Optional model overrides; empty = the defaults in gemini.mjs (newest Pro / newest Flash).
GEMINI_MODEL_ENV=""
[[ -n "${ASK_MESHA_MODEL:-}" ]] && GEMINI_MODEL_ENV+=",ASK_MESHA_MODEL=${ASK_MESHA_MODEL}"
[[ -n "${ASK_MESHA_FAST_MODEL:-}" ]] && GEMINI_MODEL_ENV+=",ASK_MESHA_FAST_MODEL=${ASK_MESHA_FAST_MODEL}"
SECRET_APP_DB="${ASK_MESHA_SECRET_APP_DB:-goatos-stg-ask-mesha-db-url}"
SECRET_RO_DB="${ASK_MESHA_SECRET_RO_DB:-mesha-ceo-readonly-db-url}"
ADMIN_WEB_SERVICE="${ADMIN_WEB_SERVICE:-goatos-admin-web-stg}"
MCP_SERVICE="${MCP_SERVICE:-goatos-mcp-stg}"
MCP_RUNTIME_SA="${MCP_RUNTIME_SA:-goatos-mcp-stg@${PROJECT_ID}.iam.gserviceaccount.com}"
# ask_goatos waits up to this long for the agent; the MCP service's request timeout is set above it.
MCP_AGENT_TIMEOUT="${MESHA_MCP_AGENT_TIMEOUT:-240s}"
: "${ASK_MESHA_IMAGE:?ASK_MESHA_IMAGE is required}"
: "${COMMIT_TAG:?COMMIT_TAG is required}"

die() { echo "ask-mesha deploy: $*" >&2; exit 1; }

echo "ask-mesha deploy: preflight (project=${PROJECT_ID}, service=${SERVICE})"
gcloud iam service-accounts describe "$RUNTIME_SA" --project="$PROJECT_ID" --format='value(email)' >/dev/null 2>&1 \
  || die "runtime service account $RUNTIME_SA missing; see tools/ask-mesha-agent/deploy/RUNBOOK.md"
# The runtime SA must be able to call Vertex AI (Gemini). Checked, never granted here.
gcloud projects get-iam-policy "$GEMINI_PROJECT" --flatten='bindings[].members' \
  --filter="bindings.role=roles/aiplatform.user AND bindings.members=serviceAccount:${RUNTIME_SA}" \
  --format='value(bindings.role)' 2>/dev/null | grep -q aiplatform.user \
  || die "runtime SA $RUNTIME_SA lacks roles/aiplatform.user on $GEMINI_PROJECT; see RUNBOOK.md §3b"
for secret in "$SECRET_APP_DB" "$SECRET_RO_DB"; do
  gcloud secrets describe "$secret" --project="$PROJECT_ID" --format='value(name)' >/dev/null 2>&1 \
    || die "secret $secret missing; see RUNBOOK.md"
done
gcloud storage buckets describe "gs://${UPLOADS_BUCKET}" --format='value(name)' >/dev/null 2>&1 \
  || die "bucket gs://${UPLOADS_BUCKET} missing; see RUNBOOK.md"

gcloud run deploy "$SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$ASK_MESHA_IMAGE" \
  --service-account="$RUNTIME_SA" \
  --no-allow-unauthenticated \
  --ingress=all \
  --port=8080 \
  --cpu=2 \
  --memory=4Gi \
  --min-instances=1 \
  --max-instances=1 \
  --concurrency=12 \
  --timeout=3600 \
  --no-cpu-throttling \
  --execution-environment=gen2 \
  --add-cloudsql-instances="$CLOUDSQL_INSTANCE" \
  --set-env-vars="ASK_MESHA_UPLOADS_BUCKET=${UPLOADS_BUCKET},ASK_MESHA_READONLY=1,ASK_MESHA_DB_MIGRATE=1,ASK_MESHA_MONTHLY_BUDGET_USD=${ASK_MESHA_MONTHLY_BUDGET_USD:-100},ASK_MESHA_PER_ANSWER_BUDGET_USD=${ASK_MESHA_PER_ANSWER_BUDGET_USD:-1},ASK_MESHA_DEEP_ANSWER_BUDGET_USD=${ASK_MESHA_DEEP_ANSWER_BUDGET_USD:-5},ASK_MESHA_DB_POOL=${ASK_MESHA_DB_POOL:-5},GOATOS_BASE_SHA=${COMMIT_TAG},ASK_MESHA_GEMINI_PROJECT=${GEMINI_PROJECT},ASK_MESHA_GEMINI_LOCATION=${GEMINI_LOCATION}${GEMINI_MODEL_ENV}" \
  --set-secrets="ASK_MESHA_DATABASE_URL=${SECRET_APP_DB}:latest,ASK_MESHA_READONLY_DB_URL=${SECRET_RO_DB}:latest" \
  --update-labels="commit_sha=${COMMIT_TAG},deployed_by=cloud-build" \
  --quiet

url="$(gcloud run services describe "$SERVICE" --project="$PROJECT_ID" --region="$REGION" --format='value(status.url)')"
[[ -n "$url" ]] || die "could not read $SERVICE URL"
echo "ask-mesha deploy: $SERVICE ready at $url (image $ASK_MESHA_IMAGE)"

if [[ "${ASK_MESHA_WIRE_ADMIN_WEB:-false}" == "true" ]]; then
  # --update-env-vars keeps every other admin-web env var; later Cloud Deploy
  # releases also use --update-env-vars, so these survive normal deploys.
  gcloud run services update "$ADMIN_WEB_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --no-traffic \
    --timeout=2100 \
    --update-env-vars="CEO_AI_AGENT_URL=${url},CEO_AI_AGENT_AUDIENCE=${url}" \
    --quiet
  rev="$(gcloud run services describe "$ADMIN_WEB_SERVICE" --project="$PROJECT_ID" --region="$REGION" \
    --format='value(status.latestCreatedRevisionName)')"
  [[ -n "$rev" ]] || die "admin-web revision not created"
  gcloud run services update-traffic "$ADMIN_WEB_SERVICE" --project="$PROJECT_ID" --region="$REGION" \
    "--to-revisions=${rev}=100" --quiet
  echo "ask-mesha deploy: $ADMIN_WEB_SERVICE -> $rev now forwards /api/ceo-ai/* to $url"
fi

if [[ "${ASK_MESHA_WIRE_MCP:-false}" == "true" ]]; then
  # Hosted MCP (https://mcp.mesha.sg/mcp): ask_goatos answers via this agent instead of the
  # legacy API /ceo-ai/ask. The MCP runtime SA must be allowed to invoke the IAM-protected agent;
  # it sends a metadata-server ID token (audience = agent URL) in X-Serverless-Authorization.
  gcloud iam service-accounts describe "$MCP_RUNTIME_SA" --project="$PROJECT_ID" --format='value(email)' >/dev/null 2>&1 \
    || die "MCP runtime service account $MCP_RUNTIME_SA missing"
  gcloud run services add-iam-policy-binding "$SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --member="serviceAccount:${MCP_RUNTIME_SA}" \
    --role=roles/run.invoker \
    --quiet >/dev/null
  # --update-env-vars keeps the Terraform-set env; --timeout=300 lets a 240s answer finish.
  # A later `terraform apply` on goatos-mcp-stg drops these two vars (ask_goatos then falls back to
  # the legacy path, which is safe); re-run this wiring after such an apply. See RUNBOOK.md §6b.
  gcloud run services update "$MCP_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --timeout=300 \
    --update-env-vars="MESHA_MCP_AGENT_URL=${url},MESHA_MCP_AGENT_AUDIENCE=${url},MESHA_MCP_AGENT_TIMEOUT=${MCP_AGENT_TIMEOUT}" \
    --quiet
  echo "ask-mesha deploy: $MCP_SERVICE ask_goatos now answers via $url (invoker granted to $MCP_RUNTIME_SA)"
fi
