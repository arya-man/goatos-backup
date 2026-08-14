#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
REGION="${REGION:-asia-south1}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

commit_sha="$(git rev-parse --short=12 HEAD)"
release_id="${RELEASE_ID:-r-${commit_sha}-$(date -u +%H%M%S)}"
build_id="${BUILD_ID:-local}"

slack_webhook_url() {
  gcloud secrets versions access latest \
    --project="$PROJECT_ID" \
    --secret="$SLACK_WEBHOOK_SECRET" 2>/dev/null || true
}

notify_slack() {
  local status="$1"
  local text="$2"
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0

  python3 - "$status" "$text" "$commit_sha" "$release_id" "$build_id" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import os
import sys

status, text, sha, release, build_id = sys.argv[1:]
color = {"STARTED": "#439FE0", "SUCCEEDED": "#2EB67D", "FAILED": "#E01E5A"}.get(status, "#AAAAAA")
build_url = f"https://console.cloud.google.com/cloud-build/builds/{build_id}?project=goatos-stg"
deploy_url = "https://console.cloud.google.com/deploy/delivery-pipelines/asia-south1/goatos-stg/releases?project=goatos-stg"
payload = {
    "attachments": [{
        "color": color,
        "title": f"Goat OS STG deploy {status.lower()}",
        "text": text,
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Release", "value": release, "short": True},
        ],
        "actions": [
            {"type": "button", "text": "Cloud Build logs", "url": build_url},
            {"type": "button", "text": "Cloud Deploy", "url": deploy_url},
        ],
    }]
}
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
  post_deploy_panel
  trap - EXIT
  echo "ALREADY_DEPLOYED ${commit_sha} on goatos-stg"
  exit 0
fi

export RELEASE_ID="$release_id"
notify_slack "STARTED" "Building images and creating Cloud Deploy release for STG."

tools/deploy/stg-clouddeploy-release.sh

notify_slack "SUCCEEDED" "STG rollout succeeded and live images were verified."
post_deploy_panel
trap - EXIT

echo "DEPLOYED ${commit_sha} to goatos-stg"
echo "Cloud Deploy release: ${release_id}"
