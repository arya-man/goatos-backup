#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
CONSOLE_AUTHUSER="${CONSOLE_AUTHUSER:-ravi@mesha.sg}"
DEPLOY_MOBILE="${DEPLOY_MOBILE:-false}"
PUBLIC_DASHBOARD_HOST="${PUBLIC_DASHBOARD_HOST:-dashboard.mesha.sg}"
PUBLIC_API_HOST="${PUBLIC_API_HOST:-api.goatos.mesha.sg}"
EXPECTED_LB_IP="${EXPECTED_LB_IP:-8.233.143.24}"
URL_MAP_NAME="${URL_MAP_NAME:-goatos-stg-dashboard-map}"

if repo_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  :
else
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
cd "$repo_root"

if [[ -n "${COMMIT_SHA:-}" ]]; then
  commit_sha="$(printf '%s' "$COMMIT_SHA" | cut -c1-12)"
else
  commit_sha="$(git rev-parse --short=12 HEAD)"
fi
release_id="${RELEASE_ID:-r-${commit_sha}-$(date -u +%H%M%S)}"
build_id="${BUILD_ID:-local}"
triggered_by="${TRIGGERED_BY:-unknown Slack user}"
deploy_metadata_file="${GOATOS_STG_DEPLOY_METADATA_FILE:-/workspace/goatos-stg-deploy.env}"

slack_webhook_url() {
  gcloud secrets versions access latest \
    --project="$PROJECT_ID" \
    --secret="$SLACK_WEBHOOK_SECRET" 2>/dev/null || true
}

notify_slack() {
  local status="$1"
  local text="$2"
  local include_panel="${3:-0}"
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0

  python3 - "$status" "$text" "$commit_sha" "$release_id" "$build_id" "$triggered_by" "$include_panel" "$PROJECT_NUMBER" "$REGION" "$CONSOLE_AUTHUSER" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import sys
import urllib.parse

status, text, sha, release, build_id, triggered_by, include_panel, project_number, region, authuser = sys.argv[1:]
color = {"STARTED": "#439FE0", "SUCCEEDED": "#2EB67D", "FAILED": "#E01E5A"}.get(status, "#AAAAAA")
query = urllib.parse.urlencode({"project": project_number, "authuser": authuser})
build_url = f"https://console.cloud.google.com/cloud-build/builds;region={region}/{build_id}?{query}"
deploy_url = f"https://console.cloud.google.com/deploy/delivery-pipelines/{region}/goatos-stg/releases/{release}?{query}"
payload = {
    "attachments": [{
        "color": color,
        "title": f"GoatOS deploy {status.lower()}",
        "text": text,
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Release", "value": release, "short": True},
            {"title": "Triggered by", "value": triggered_by, "short": False},
        ],
        "actions": [
            {"type": "button", "text": "Cloud Build logs", "url": build_url},
            {"type": "button", "text": "Cloud Deploy", "url": deploy_url},
        ],
    }]
}
if include_panel == "1":
    payload["blocks"] = [
        {"type": "divider"},
        {
            "type": "section",
            "text": {
                "type": "mrkdwn",
                "text": "*GoatOS deploy*\nDeploy the current `main` branch to the existing production-facing services, or distribute only the Android release.",
            },
        },
        {
            "type": "actions",
            "block_id": "deploy_options",
            "elements": [{
                "type": "checkboxes",
                "action_id": "deploy_options",
                "options": [{
                    "text": {"type": "plain_text", "text": "Also distribute Android mobile"},
                    "description": {
                        "type": "plain_text",
                        "text": "Firebase App Distribution, Play Internal Testing, and mesha.sg/app.apk",
                    },
                    "value": "mobile_distribution",
                }],
            }],
        },
        {
            "type": "actions",
            "elements": [
                {
                    "type": "button",
                    "text": {"type": "plain_text", "text": "Deploy backend/web"},
                    "style": "primary",
                    "action_id": "deploy_goatos_stg_main",
                    "value": "main",
                },
                {
                    "type": "button",
                    "text": {"type": "plain_text", "text": "Distribute Android only"},
                    "action_id": "deploy_goatos_mobile_only",
                    "value": "mobile",
                },
            ],
        },
    ]
print(json.dumps(payload))
PY
}

post_deploy_panel() {
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0
  [[ -f tools/deploy/slack-stg-deploy-bot/deploy-card.json ]] || return 0

  curl -fsS -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @tools/deploy/slack-stg-deploy-bot/deploy-card.json \
    "$webhook" >/dev/null || true
}

live_image_tag() {
  local service="$1"
  gcloud run services describe "$service" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --format='value(spec.template.spec.containers[0].image)' \
    2>/dev/null | awk -F: '{print $NF}'
}

already_deployed() {
  local api_tag admin_tag bridge_tag
  api_tag="$(live_image_tag goatos-api-stg)"
  admin_tag="$(live_image_tag goatos-admin-web-stg)"
  bridge_tag="$(live_image_tag goatos-herd-signals-mqtt-bridge-stg)"

  [[ "$api_tag" == "$commit_sha" && "$admin_tag" == "$commit_sha" && "$bridge_tag" == "$commit_sha" ]]
}

require_public_host_ready() {
  local host="$1"
  local expected_ip="$2"
  local resolved
  resolved="$(dig +short "$host" A | sed -n '1p')"
  [[ "$resolved" == "$expected_ip" ]] || {
    echo "ERROR: $host must resolve to $expected_ip before deploy; got ${resolved:-no A record}" >&2
    return 1
  }
}

require_managed_cert_ready() {
  local host="$1"
  curl -sSIL --max-time 15 "https://${host}/" >/dev/null || {
    echo "ERROR: https://${host}/ must complete a verified TLS handshake before deploy" >&2
    return 1
  }
}

require_url_map_host_rule() {
  local host="$1"
  local backend_suffix="$2"
  gcloud compute url-maps describe "$URL_MAP_NAME" \
    --project="$PROJECT_ID" \
    --global \
    --format=json |
    python3 - "$host" "$backend_suffix" <<'PY'
import json
import sys

host, backend_suffix = sys.argv[1:]
doc = json.load(sys.stdin)
matchers = {item.get("name"): item for item in doc.get("pathMatchers", [])}
for rule in doc.get("hostRules", []):
    if host not in rule.get("hosts", []):
        continue
    matcher = matchers.get(rule.get("pathMatcher"), {})
    service = matcher.get("defaultService", "")
    if service.endswith(backend_suffix):
        sys.exit(0)
    print(f"ERROR: {host} routes to {service or 'no default service'}, want suffix {backend_suffix}", file=sys.stderr)
    sys.exit(1)
print(f"ERROR: {host} has no host rule in URL map", file=sys.stderr)
sys.exit(1)
PY
}

require_public_ingress_ready() {
  require_public_host_ready "$PUBLIC_DASHBOARD_HOST" "$EXPECTED_LB_IP"
  require_public_host_ready "$PUBLIC_API_HOST" "$EXPECTED_LB_IP"
  require_managed_cert_ready "$PUBLIC_DASHBOARD_HOST"
  require_managed_cert_ready "$PUBLIC_API_HOST"
  require_url_map_host_rule "$PUBLIC_API_HOST" "/goatos-api-stg-backend"
}

deploy_herd_signals_mqtt_bridge() {
  local backend_image="asia-south1-docker.pkg.dev/${PROJECT_ID}/goatos/backend:${commit_sha}"
  local service="goatos-herd-signals-mqtt-bridge-stg"

  gcloud run deploy "$service" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$backend_image" \
    --command="/app/bin/herd-signals-mqtt-bridge" \
    --service-account="goatos-hs-mqtt-bridge-stg@goatos-stg.iam.gserviceaccount.com" \
    --ingress=internal \
    --min-instances=1 \
    --max-instances=1 \
    --cpu=1 \
    --memory=512Mi \
    --no-cpu-throttling \
    --add-cloudsql-instances="${PROJECT_ID}:${REGION}:goatos-stg-core-db" \
    --set-env-vars="GOATOS_ENV=stg,GOATOS_HEALTH_ADDR=:8080,HERD_SIGNALS_MQTT_HOST=8.234.104.45,HERD_SIGNALS_MQTT_PORT=8883,HERD_SIGNALS_MQTT_TLS=true,HERD_SIGNALS_MQTT_CLIENT_ID=herd-signals-mqtt-bridge-stg,HERD_SIGNALS_MQTT_USERNAME=gw-514060,HERD_SIGNALS_MQTT_TOPIC=GwData,HERD_SIGNALS_TENANT_ID=00000000-0000-4000-8000-000000000001,HERD_SIGNALS_DEFAULT_GATEWAY_ID=f130d402dcb4,HERD_SIGNALS_MQTT_BATCH_SIZE=50,HERD_SIGNALS_MQTT_BATCH_INTERVAL=2s,HERD_SIGNALS_MQTT_QUEUE_MAX=5000" \
    --set-secrets="DATABASE_URL=goatos-stg-database-url:latest,HERD_SIGNALS_MQTT_PASSWORD=herd-signals-mqtt-gateway-514060-password:latest,HERD_SIGNALS_MQTT_CA_CERT=herd-signals-mqtt-ca-crt:latest" \
    --update-labels="commit_sha=${commit_sha},deployed_by=cloud-build-release" \
    --quiet

  local actual
  actual="$(gcloud run services describe "$service" --project="$PROJECT_ID" --region="$REGION" --format='value(spec.template.spec.containers[0].image)')"
  [[ "$actual" == "$backend_image" ]] || {
    echo "ERROR: $service image stale: got $actual want $backend_image" >&2
    return 1
  }
}

on_exit() {
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    notify_slack "FAILED" "Cloud Build failed before backend/web rollout completed."
    post_deploy_panel
  fi
}

trap on_exit EXIT

require_public_ingress_ready

if already_deployed; then
  notify_slack "SUCCEEDED" 'Backend/web is already running the latest `main`; no new release was created.'
  if [[ "$DEPLOY_MOBILE" != "true" ]]; then
    post_deploy_panel
  fi
  trap - EXIT
  echo "ALREADY_DEPLOYED ${commit_sha} on goatos-stg"
  exit 0
fi

export RELEASE_ID="$release_id"
{
  printf 'COMMIT_SHA=%q\n' "$commit_sha"
  printf 'RELEASE_ID=%q\n' "$release_id"
  printf 'BUILD_ID=%q\n' "$build_id"
  printf 'TRIGGERED_BY=%q\n' "$triggered_by"
} >"$deploy_metadata_file"
notify_slack "STARTED" "Building images and creating Cloud Deploy release for backend/web."

tools/deploy/stg-clouddeploy-release.sh
deploy_herd_signals_mqtt_bridge

if [[ "$DEPLOY_MOBILE" == "true" ]]; then
  notify_slack "SUCCEEDED" "Backend/web rollout succeeded and live images were verified. Android mobile distribution will start next."
else
  notify_slack "SUCCEEDED" "Backend/web rollout succeeded and live images were verified." 1
fi
trap - EXIT

echo "DEPLOYED ${commit_sha} to goatos-stg"
echo "Cloud Deploy release: ${release_id}"
