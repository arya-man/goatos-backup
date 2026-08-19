#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
GOOGLE_PLAY_PACKAGE="${GOOGLE_PLAY_PACKAGE:-sg.mesha.goatos.stg}"
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
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0

  python3 - "$status" "$text" "$commit_sha" "$build_id" "$triggered_by" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import sys

status, text, sha, build_id, triggered_by = sys.argv[1:]
color = {"STARTED": "#439FE0", "SUCCEEDED": "#2EB67D", "FAILED": "#E01E5A"}.get(status, "#AAAAAA")
build_url = f"https://console.cloud.google.com/cloud-build/builds;region=asia-south1/{build_id}?project=goatos-stg"
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
            {"type": "button", "text": "Direct APK", "url": "https://mesha.sg/app.apk"},
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

on_exit() {
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    notify_slack "FAILED" "Mobile distribution failed. Nothing should be called complete until Firebase, Play Internal, and mesha.sg/app.apk all pass."
    post_deploy_panel
  fi
}
trap on_exit EXIT

[[ "$PROJECT_ID" == "goatos-stg" ]] || { echo "PROJECT_ID must be goatos-stg" >&2; exit 1; }
[[ -z "$(git status --porcelain)" ]] || { echo "Refusing Android distribution from a dirty worktree." >&2; exit 1; }

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
curl -fsSL \
  https://storage.googleapis.com/goatos-stg-public-downloads/operator/latest/app.apk \
  -o .local/verify-latest-app.apk

apk_sha="$(shasum -a 256 "$APK" | awk '{print $1}')"
mirror_sha="$(shasum -a 256 .local/verify-latest-app.apk | awk '{print $1}')"
[[ "$apk_sha" == "$mirror_sha" ]] || { echo "latest/app.apk SHA mismatch" >&2; exit 1; }

curl -fsSI https://storage.googleapis.com/goatos-stg-public-downloads/operator/latest/app.apk | grep -qi 'content-type: application/vnd.android.package-archive'
curl -fsSIL https://mesha.sg/app.apk | grep -qi 'content-type: application/vnd.android.package-archive'

notify_slack "SUCCEEDED" "Mobile distribution succeeded: Firebase App Distribution uploaded, Play Internal updated to versionCode ${ANDROID_VERSION_CODE}, and mesha.sg/app.apk now serves ${DOWNLOAD_NAME}."
post_deploy_panel
trap - EXIT

echo "MOBILE_DISTRIBUTED ${commit_sha} ${ANDROID_VERSION_NAME} ${ANDROID_VERSION_CODE}"
