#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
DEPLOY_METADATA_FILE="${GOATOS_STG_DEPLOY_METADATA_FILE:-/workspace/goatos-stg-deploy.env}"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [[ ! -f "$DEPLOY_METADATA_FILE" ]]; then
  echo "release-tag-bookkeeping: no deploy metadata file at $DEPLOY_METADATA_FILE; skipping"
  exit 0
fi

# shellcheck disable=SC1090
source "$DEPLOY_METADATA_FILE"

commit_sha="${COMMIT_SHA:-$(git rev-parse --short=12 HEAD)}"
release_id="${RELEASE_ID:-}"
build_id="${BUILD_ID:-local}"

slack_webhook_url() {
  gcloud secrets versions access latest \
    --project="$PROJECT_ID" \
    --secret="$SLACK_WEBHOOK_SECRET" 2>/dev/null || true
}

notify_slack_bookkeeping_warning() {
  local text="$1"
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0

  python3 - "$text" "$commit_sha" "$release_id" "$build_id" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import sys

text, sha, release, build_id = sys.argv[1:]
build_url = f"https://console.cloud.google.com/cloud-build/builds/{build_id}?project=goatos-stg"
deploy_url = "https://console.cloud.google.com/deploy/delivery-pipelines/asia-south1/goatos-stg/releases?project=goatos-stg"
payload = {
    "attachments": [{
        "color": "#ECB22E",
        "title": "Goat OS STG release bookkeeping warning",
        "text": text,
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Release", "value": release or "unknown", "short": True},
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

if ENV=stg SHA="$(git rev-parse HEAD)" CLOUD_DEPLOY_RELEASE="$release_id" \
  tools/release/create-release-tag.sh; then
  echo "release-tag-bookkeeping: release tag recorded for $commit_sha"
  exit 0
fi

echo "release-tag-bookkeeping: release tag failed after verified STG rollout; STG remains deployed."
notify_slack_bookkeeping_warning "STG deploy succeeded, but release-tag bookkeeping failed. STG remains deployed; deploy status is green."
exit 0
