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
# How the agent authenticates to Claude:
#   auto      (default) both: Vertex env + the Anthropic API key secret. The service probes
#             Vertex at startup and every 15 min and moves to it by itself once it answers
#             (quota approved); until then it uses the API key. See provider.mjs.
#   vertex    Claude on Vertex AI via the runtime SA; no key, billed to GCP.
#   api-key   Anthropic Console API key from Secret Manager.
#   oauth     `claude setup-token` subscription token (personal plans are for the
#             subscriber's own use; not for a shared multi-user service).
CLAUDE_AUTH="${ASK_MESHA_CLAUDE_AUTH:-auto}"
# claude-sonnet-5 / claude-opus-5-5 are GA only on the Vertex global endpoint (verified 2026-09-24;
# 404 in asia-south1 and us-east5). global = capacity routing, not pinned to India.
VERTEX_REGION="${ASK_MESHA_VERTEX_REGION:-global}"
SECRET_ANTHROPIC="${ASK_MESHA_SECRET_ANTHROPIC:-goatos-stg-ask-mesha-anthropic-api-key}"
SECRET_OAUTH="${ASK_MESHA_SECRET_OAUTH:-goatos-stg-ask-mesha-claude-oauth-token}"
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
VERTEX_ENV="CLAUDE_CODE_USE_VERTEX=1,ANTHROPIC_VERTEX_PROJECT_ID=${PROJECT_ID},CLOUD_ML_REGION=${VERTEX_REGION}"
case "$CLAUDE_AUTH" in
  auto)
    CLAUDE_ENV="${VERTEX_ENV}"
    CLAUDE_SECRET="ANTHROPIC_API_KEY=${SECRET_ANTHROPIC}:latest,"
    CLAUDE_SECRETS=("$SECRET_ANTHROPIC") ;;
  vertex)
    CLAUDE_ENV="${VERTEX_ENV}"
    CLAUDE_SECRET=""
    CLAUDE_SECRETS=() ;;
  api-key)
    CLAUDE_ENV=""
    CLAUDE_SECRET="ANTHROPIC_API_KEY=${SECRET_ANTHROPIC}:latest,"
    CLAUDE_SECRETS=("$SECRET_ANTHROPIC") ;;
  oauth)
    CLAUDE_ENV=""
    CLAUDE_SECRET="CLAUDE_CODE_OAUTH_TOKEN=${SECRET_OAUTH}:latest,"
    CLAUDE_SECRETS=("$SECRET_OAUTH") ;;
  *) die "ASK_MESHA_CLAUDE_AUTH must be auto|vertex|api-key|oauth (got $CLAUDE_AUTH)" ;;
esac

for secret in "${CLAUDE_SECRETS[@]}" "$SECRET_APP_DB" "$SECRET_RO_DB"; do
  gcloud secrets describe "$secret" --project="$PROJECT_ID" --format='value(name)' >/dev/null 2>&1 \
    || die "secret $secret missing; see RUNBOOK.md"
done
# The Claude credential must also have an enabled version (a disabled key would only
# surface as a failed answer). auto: disable the key deliberately with --update-env-vars
# ASK_MESHA_CLAUDE_AUTH=vertex first; see RUNBOOK.md §3b.
for secret in "${CLAUDE_SECRETS[@]}"; do
  [[ -n "$(gcloud secrets versions list "$secret" --project="$PROJECT_ID" --filter='state=ENABLED' --limit=1 --format='value(name)' 2>/dev/null)" ]] \
    || die "secret $secret has no enabled version; see RUNBOOK.md §3b"
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
  --set-env-vars="ASK_MESHA_UPLOADS_BUCKET=${UPLOADS_BUCKET},ASK_MESHA_READONLY=1,ASK_MESHA_DB_MIGRATE=1,ASK_MESHA_MONTHLY_BUDGET_USD=${ASK_MESHA_MONTHLY_BUDGET_USD:-100},ASK_MESHA_PER_ANSWER_BUDGET_USD=${ASK_MESHA_PER_ANSWER_BUDGET_USD:-1},ASK_MESHA_DEEP_ANSWER_BUDGET_USD=${ASK_MESHA_DEEP_ANSWER_BUDGET_USD:-5},ASK_MESHA_DB_POOL=${ASK_MESHA_DB_POOL:-5},GOATOS_BASE_SHA=${COMMIT_TAG},ASK_MESHA_CLAUDE_AUTH=${CLAUDE_AUTH}${CLAUDE_ENV:+,${CLAUDE_ENV}}" \
  --set-secrets="${CLAUDE_SECRET}ASK_MESHA_DATABASE_URL=${SECRET_APP_DB}:latest,ASK_MESHA_READONLY_DB_URL=${SECRET_RO_DB}:latest" \
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
