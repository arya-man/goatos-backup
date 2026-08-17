#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
GOOGLE_PLAY_PACKAGE="${GOOGLE_PLAY_PACKAGE:-sg.mesha.goatos.stg}"
FIREBASE_APP_ID="${FIREBASE_APP_ID:-1:514832198871:android:0cb898377ba4f7f7f19492}"
CONSOLE_AUTHUSER="${CONSOLE_AUTHUSER:-ravi@mesha.sg}"
ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-/workspace/android-sdk}"
ANDROID_HOME="$ANDROID_SDK_ROOT"
export ANDROID_SDK_ROOT ANDROID_HOME

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

commit_sha="$(git rev-parse --short=12 HEAD)"
build_id="${BUILD_ID:-local}"
triggered_by="${TRIGGERED_BY:-unknown Slack user}"

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

  python3 - "$status" "$text" "$commit_sha" "$build_id" "$triggered_by" "$include_panel" "$PROJECT_NUMBER" "$REGION" "$CONSOLE_AUTHUSER" "$FIREBASE_APP_ID" "$GOOGLE_PLAY_PACKAGE" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import sys
import urllib.parse

status, text, sha, build_id, triggered_by, include_panel, project_number, region, authuser, firebase_app_id, package_name = sys.argv[1:]
color = {"STARTED": "#439FE0", "SUCCEEDED": "#2EB67D", "FAILED": "#E01E5A"}.get(status, "#AAAAAA")
build_query = urllib.parse.urlencode({"project": project_number, "authuser": authuser})
build_url = f"https://console.cloud.google.com/cloud-build/builds;region={region}/{build_id}?{build_query}"
firebase_url = f"https://console.firebase.google.com/u/0/project/goatos-stg/appdistribution/app/android:{firebase_app_id}/releases"
play_url = f"https://play.google.com/apps/testing/{package_name}"
apk_url = "https://mesha.sg/app.apk"
payload = {
    "attachments": [{
        "color": color,
        "title": f"Goat OS Android STG distribution {status.lower()}",
        "text": text,
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Channels", "value": "Firebase App Distribution, Play Internal, mesha.sg/app.apk", "short": False},
            {"title": "Triggered by", "value": triggered_by, "short": False},
        ],
        "actions": [
            {"type": "button", "text": "Cloud Build logs", "url": build_url},
            {"type": "button", "text": "Firebase releases", "url": firebase_url},
            {"type": "button", "text": "Play Internal", "url": play_url},
            {"type": "button", "text": "Direct APK", "url": apk_url},
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

on_exit() {
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    if [[ "${DEPLOY_STG:-false}" == "true" ]]; then
      notify_slack "FAILED" "STG rollout succeeded. Android mobile distribution failed. Firebase App Distribution, Play Internal Testing, and mesha.sg/app.apk did NOT all complete."
    else
      notify_slack "FAILED" "Android mobile distribution failed. Firebase App Distribution, Play Internal Testing, and mesha.sg/app.apk did NOT all complete."
    fi
    post_deploy_panel
  fi
}
trap on_exit EXIT

[[ "$PROJECT_ID" == "goatos-stg" ]] || { echo "PROJECT_ID must be goatos-stg" >&2; exit 1; }
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || { echo "Refusing Android distribution because tracked source files changed." >&2; exit 1; }

install_android_sdk() {
  if [[ -x "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" ]]; then
    return 0
  fi
  mkdir -p "$ANDROID_HOME/cmdline-tools"
  curl -fsSL "https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip" -o /workspace/android-commandlinetools.zip
  unzip -q /workspace/android-commandlinetools.zip -d "$ANDROID_HOME/cmdline-tools"
  mv "$ANDROID_HOME/cmdline-tools/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest"
}

notify_slack "STARTED" "STG backend/web deploy finished; building signed Android STG release."

install_android_sdk
yes | "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --licenses >/dev/null || true
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "platforms;android-36" "build-tools;36.0.0"

make restore-stg-android-release-env
source .local/android-signing/stg-release-env.sh

cd apps/goatos-android
./gradlew \
  :app:assembleStgRelease \
  :app:bundleStgRelease \
  :app:appDistributionUploadStgRelease \
  --no-configuration-cache \
  -PfadReleaseNotes="Goat OS (Mesha) STG release from main ${commit_sha}"

cd "$repo_root"
APK="apps/goatos-android/app/build/outputs/apk/stg/release/app-stg-release.apk"
AAB="apps/goatos-android/app/build/outputs/bundle/stgRelease/app-stg-release.aab"
test -f "$APK"
test -f "$AAB"

ANDROID_VERSION_NAME="$("$ANDROID_HOME/cmdline-tools/latest/bin/apkanalyzer" manifest version-name "$APK")"
ANDROID_VERSION_CODE="$("$ANDROID_HOME/cmdline-tools/latest/bin/apkanalyzer" manifest version-code "$APK")"
DOWNLOAD_NAME="Mesha-${ANDROID_VERSION_NAME}.apk"

play_access_token="$(gcloud auth print-access-token --scopes=https://www.googleapis.com/auth/androidpublisher)"
play_base="https://androidpublisher.googleapis.com/androidpublisher/v3/applications/${GOOGLE_PLAY_PACKAGE}"
edit_id="$(curl -fsS -X POST -H "Authorization: Bearer ${play_access_token}" "${play_base}/edits" | jq -r '.id')"
[[ -n "$edit_id" && "$edit_id" != "null" ]] || { echo "Could not create Google Play edit." >&2; exit 1; }

play_version_code="$(
  curl -fsS -X POST \
    -H "Authorization: Bearer ${play_access_token}" \
    -H "Content-Type: application/octet-stream" \
    --data-binary @"$AAB" \
    "https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications/${GOOGLE_PLAY_PACKAGE}/edits/${edit_id}/bundles?uploadType=media" \
    | jq -r '.versionCode'
)"
[[ "$play_version_code" == "$ANDROID_VERSION_CODE" ]] || {
  echo "Play uploaded versionCode $play_version_code, APK has $ANDROID_VERSION_CODE" >&2
  exit 1
}

jq -n --arg vc "$ANDROID_VERSION_CODE" '{
  releases: [{
    name: ("Goat OS STG " + $vc),
    status: "completed",
    versionCodes: [$vc]
  }]
}' > .local/android-signing/play-internal-track.json

curl -fsS -X PUT \
  -H "Authorization: Bearer ${play_access_token}" \
  -H "Content-Type: application/json" \
  --data-binary @.local/android-signing/play-internal-track.json \
  "${play_base}/edits/${edit_id}/tracks/internal" >/dev/null

curl -fsS -X POST \
  -H "Authorization: Bearer ${play_access_token}" \
  "${play_base}/edits/${edit_id}:commit" >/dev/null

gcloud storage cp "$APK" \
  "gs://goatos-stg-public-downloads/operator/releases/${DOWNLOAD_NAME}" \
  --project="$PROJECT_ID" \
  --content-type='application/vnd.android.package-archive' \
  --cache-control='public, max-age=31536000, immutable' \
  --content-disposition="attachment; filename=\"${DOWNLOAD_NAME}\""

gcloud storage cp "$APK" \
  gs://goatos-stg-public-downloads/operator/latest/app.apk \
  --project="$PROJECT_ID" \
  --content-type='application/vnd.android.package-archive' \
  --cache-control='no-cache, max-age=0' \
  --content-disposition="attachment; filename=\"${DOWNLOAD_NAME}\""

mkdir -p .local
gcloud storage cp \
  gs://goatos-stg-public-downloads/operator/latest/app.apk \
  .local/verify-latest-app.apk \
  --project="$PROJECT_ID"

apk_sha="$(shasum -a 256 "$APK" | awk '{print $1}')"
mirror_sha="$(shasum -a 256 .local/verify-latest-app.apk | awk '{print $1}')"
[[ "$apk_sha" == "$mirror_sha" ]] || { echo "latest/app.apk SHA mismatch" >&2; exit 1; }

curl -fsSI https://storage.googleapis.com/goatos-stg-public-downloads/operator/latest/app.apk | grep -qi 'content-type: application/vnd.android.package-archive'
curl -fsSIL https://mesha.sg/app.apk | grep -qi 'content-type: application/vnd.android.package-archive'

notify_slack "SUCCEEDED" "Mobile distribution succeeded: Firebase App Distribution uploaded, Play Internal updated to versionCode ${ANDROID_VERSION_CODE}, and mesha.sg/app.apk now serves ${DOWNLOAD_NAME}."
post_deploy_panel
trap - EXIT

echo "MOBILE_DISTRIBUTED ${commit_sha} ${ANDROID_VERSION_NAME} ${ANDROID_VERSION_CODE}"
