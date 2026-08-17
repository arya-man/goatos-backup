#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
CONSOLE_AUTHUSER="${CONSOLE_AUTHUSER:-ravi@mesha.sg}"
DEPLOY_MOBILE="${DEPLOY_MOBILE:-false}"

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
        "title": f"Goat OS STG deploy {status.lower()}",
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
                "text": "*Goat OS STG deploy*\nDeploy the current `main` branch to Google staging, or distribute only the Android STG build.",
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
                    "text": {"type": "plain_text", "text": "Deploy main to STG"},
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
  local api_tag admin_tag
  api_tag="$(live_image_tag goatos-api-stg)"
  admin_tag="$(live_image_tag goatos-admin-web-stg)"

  [[ "$api_tag" == "$commit_sha" && "$admin_tag" == "$commit_sha" ]]
}

on_exit() {
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    notify_slack "FAILED" "Cloud Build failed before STG rollout completed."
    post_deploy_panel
  fi
}

trap on_exit EXIT

if already_deployed; then
  notify_slack "SUCCEEDED" 'STG is already running the latest `main`; no new release was created.'
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
notify_slack "STARTED" "Building images and creating Cloud Deploy release for STG."

tools/deploy/stg-clouddeploy-release.sh

if [[ "$DEPLOY_MOBILE" == "true" ]]; then
  notify_slack "SUCCEEDED" "STG rollout succeeded and live images were verified. Android mobile distribution will start next."
else
  notify_slack "SUCCEEDED" "STG rollout succeeded and live images were verified." 1
fi
trap - EXIT

echo "DEPLOYED ${commit_sha} to goatos-stg"
echo "Cloud Deploy release: ${release_id}"
