#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
GITHUB_PAT_SECRET="${GITHUB_PAT_SECRET:-goatos-github-pat}"
DEPLOY_METADATA_FILE="${GOATOS_STG_DEPLOY_METADATA_FILE:-/workspace/goatos-stg-deploy.env}"
TAG_WORKSPACE="${GOATOS_RELEASE_TAG_WORKSPACE:-/workspace}"

if [[ ! -f "$DEPLOY_METADATA_FILE" ]]; then
  echo "release-tag-bookkeeping: no deploy metadata file at $DEPLOY_METADATA_FILE; skipping"
  exit 0
fi

# shellcheck disable=SC1090
source "$DEPLOY_METADATA_FILE"

if [[ -n "${COMMIT_SHA:-}" ]]; then
  commit_sha="$(printf '%s' "$COMMIT_SHA" | cut -c1-12)"
else
  commit_sha="$(git rev-parse --short=12 HEAD 2>/dev/null || true)"
fi
if [[ -z "$commit_sha" ]]; then
  echo "release-tag-bookkeeping: COMMIT_SHA is required when running outside a Git worktree; skipping"
  notify_reason="STG deploy succeeded, but release-tag bookkeeping did not receive COMMIT_SHA. STG remains deployed; deploy status is green."
fi
release_id="${RELEASE_ID:-}"
build_id="${BUILD_ID:-local}"
triggered_by="${TRIGGERED_BY:-unknown Slack user}"

slack_webhook_url() {
  gcloud secrets versions access latest \
    --project="$PROJECT_ID" \
    --secret="$SLACK_WEBHOOK_SECRET" 2>/dev/null || true
}

github_pat() {
  gcloud secrets versions access latest \
    --project="$PROJECT_ID" \
    --secret="$GITHUB_PAT_SECRET" 2>/dev/null || true
}

notify_slack_bookkeeping_warning() {
  local text="$1"
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0

  python3 - "$text" "$commit_sha" "$release_id" "$build_id" "$triggered_by" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import sys

text, sha, release, build_id, triggered_by = sys.argv[1:]
build_url = f"https://console.cloud.google.com/cloud-build/builds;region=asia-south1/{build_id}?project=goatos-stg"
deploy_url = "https://console.cloud.google.com/deploy/delivery-pipelines/asia-south1/goatos-stg/releases?project=goatos-stg"
payload = {
    "attachments": [{
        "color": "#ECB22E",
        "title": "Goat OS STG release bookkeeping warning",
        "text": text,
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Release", "value": release or "unknown", "short": True},
            {"title": "Triggered by", "value": triggered_by, "short": False},
        ],
        "actions": [
            {"type": "button", "text": "Cloud Build logs", "url": build_url},
            {"type": "button", "text": "Cloud Deploy", "url": deploy_url},
        ],
    }]
}
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

tag_checkout="${TAG_WORKSPACE}/goatos-release-tag-${commit_sha}"
rm -rf "$tag_checkout"

pat="$(github_pat)"
if [[ -z "$pat" ]]; then
  echo "release-tag-bookkeeping: GitHub PAT secret $GITHUB_PAT_SECRET is empty or inaccessible" >&2
else
  auth="$(printf 'x-access-token:%s' "$pat" | base64 | tr -d '\n')"
fi

if [[ -z "${notify_reason:-}" && -n "$pat" ]]; then
  if repo_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
    cd "$repo_root"
    git worktree add --detach "$tag_checkout" "$commit_sha"
    trap 'git worktree remove --force "$tag_checkout" >/dev/null 2>&1 || true' EXIT
  else
    git -c "http.https://github.com/vgoats/goatos.git.extraheader=AUTHORIZATION: basic ${auth}" \
      clone --no-checkout https://github.com/vgoats/goatos.git "$tag_checkout"
    git -C "$tag_checkout" checkout --detach "$commit_sha"
    trap 'rm -rf "$tag_checkout"' EXIT
  fi

  git -C "$tag_checkout" config user.name "Goat OS Deploy Bot"
  git -C "$tag_checkout" config user.email "deploy@vgoats.com"
  git -C "$tag_checkout" remote set-url origin "https://github.com/vgoats/goatos.git"
  git -C "$tag_checkout" config http.https://github.com/vgoats/goatos.git.extraheader "AUTHORIZATION: basic ${auth}"
fi

if [[ -z "${notify_reason:-}" && -n "$pat" ]] && ENV=stg SHA="$(git -C "$tag_checkout" rev-parse HEAD)" CLOUD_DEPLOY_RELEASE="$release_id" \
  "$tag_checkout/tools/release/create-release-tag.sh"; then
  echo "release-tag-bookkeeping: release tag recorded for $commit_sha"
  exit 0
fi

echo "release-tag-bookkeeping: dirty status in deploy workspace, if any:"
git status --porcelain --untracked-files=all 2>/dev/null || true
echo "release-tag-bookkeeping: dirty status in clean tag checkout, if any:"
git -C "$tag_checkout" status --porcelain --untracked-files=all 2>/dev/null || true
echo "release-tag-bookkeeping: ${notify_reason:-release tag failed after verified STG rollout; STG remains deployed.}"
notify_slack_bookkeeping_warning "${notify_reason:-STG deploy succeeded, but release-tag bookkeeping failed. STG remains deployed; deploy status is green.}"
exit 0
