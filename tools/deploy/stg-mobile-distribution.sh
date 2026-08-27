#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
PLAY_QUOTA_PROJECT="${PLAY_QUOTA_PROJECT:-$PROJECT_ID}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
GOOGLE_PLAY_PACKAGE="${GOOGLE_PLAY_PACKAGE:-sg.mesha.goatos}"
FIREBASE_APP_ID="${FIREBASE_APP_ID:-}"
CONSOLE_AUTHUSER="${CONSOLE_AUTHUSER:-ravi@mesha.sg}"
ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-/workspace/android-sdk}"
ANDROID_HOME="$ANDROID_SDK_ROOT"
export ANDROID_SDK_ROOT ANDROID_HOME

if repo_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  cd "$repo_root"
  commit_sha="$(git rev-parse --short=12 HEAD)"
  git_dirty_check=true
else
  repo_root="${BUILD_WORKSPACE_DIRECTORY:-/workspace}"
  cd "$repo_root"
  commit_sha="$(printf '%s' "${COMMIT_SHA:?COMMIT_SHA is required when Git metadata is unavailable}" | cut -c1-12)"
  git_dirty_check=false
fi
build_id="${BUILD_ID:-local}"
triggered_by="${TRIGGERED_BY:-unknown Slack user}"
firebase_uploaded=false
play_uploaded=false
apk_mirrored=false

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
firebase_tester_url = f"https://appdistribution.firebase.google.com/testerapps/{firebase_app_id}"
apk_url = "https://mesha.sg/app.apk"
payload = {
    "attachments": [{
        "color": color,
        "title": f"GoatOS Android distribution {status.lower()}",
        "text": text,
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Channels", "value": "Firebase App Distribution, Play Internal Testing, mesha.sg/app.apk", "short": False},
            {"title": "Triggered by", "value": triggered_by, "short": False},
        ],
        "actions": [
            {"type": "button", "text": "Cloud Build logs", "url": build_url},
            {"type": "button", "text": "Firebase tester", "url": firebase_tester_url},
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
                "text": "*GoatOS deploy*\nDeploy the current `main` branch to the existing production-facing services, or distribute only the Android build.",
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

play_access_token() {
  local credentials="${GOOGLE_APPLICATION_CREDENTIALS:-}"
  [[ -n "$credentials" && -f "$credentials" ]] || {
    echo "GOOGLE_APPLICATION_CREDENTIALS must point at the Android Publisher service account JSON" >&2
    return 1
  }

  local key_file=".local/android-signing/play-private-key.pem"
  local assertion_file=".local/android-signing/play-jwt.txt"
  jq -r '.private_key' "$credentials" > "$key_file"
  chmod 600 "$key_file"

  python3 - "$credentials" > "$assertion_file" <<'PY'
import base64
import json
import subprocess
import sys
import time

credentials_path = sys.argv[1]
with open(credentials_path, encoding="utf-8") as fh:
    credentials = json.load(fh)

def b64url(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")

now = int(time.time())
header = b64url(json.dumps({"alg": "RS256", "typ": "JWT"}, separators=(",", ":")).encode())
payload = b64url(json.dumps({
    "iss": credentials["client_email"],
    "scope": "https://www.googleapis.com/auth/androidpublisher",
    "aud": "https://oauth2.googleapis.com/token",
    "iat": now,
    "exp": now + 3600,
}, separators=(",", ":")).encode())
signing_input = f"{header}.{payload}"
signature = subprocess.check_output(
    ["openssl", "dgst", "-sha256", "-sign", ".local/android-signing/play-private-key.pem", "-binary"],
    input=signing_input.encode(),
)
print(f"{signing_input}.{b64url(signature)}")
PY

  local token_response
  token_response="$(
    curl -fsS -X POST \
      -d grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer \
      --data-urlencode "assertion@${assertion_file}" \
      https://oauth2.googleapis.com/token
  )" || return 1
  jq -er '.access_token' <<<"$token_response"
}

on_exit() {
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    local prefix="Android mobile distribution failed."
    [[ "${DEPLOY_STG:-false}" == "true" ]] && prefix="Backend/web rollout succeeded. Android mobile distribution failed."

    if [[ "$firebase_uploaded" == "true" && "$apk_mirrored" != "true" ]]; then
      notify_slack "FAILED" "${prefix} Firebase App Distribution uploaded, but mesha.sg/app.apk did not update."
    else
      notify_slack "FAILED" "${prefix} Firebase App Distribution, Play Internal Testing, and mesha.sg/app.apk did NOT all complete."
    fi
    post_deploy_panel
  fi
}
trap on_exit EXIT

[[ "$PROJECT_ID" == "goatos-stg" ]] || { echo "PROJECT_ID must be goatos-stg" >&2; exit 1; }
if [[ "$git_dirty_check" == "true" ]]; then
  [[ -z "$(git status --porcelain --untracked-files=no)" ]] || { echo "Refusing Android distribution because tracked source files changed." >&2; exit 1; }
fi

install_android_sdk() {
  if [[ -x "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" ]]; then
    return 0
  fi
  mkdir -p "$ANDROID_HOME/cmdline-tools"
  local commandline_tools_zip
  commandline_tools_zip="$ANDROID_HOME/cmdline-tools/android-commandlinetools.zip"
  curl -fsSL "https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip" -o "$commandline_tools_zip"
  unzip -q "$commandline_tools_zip" -d "$ANDROID_HOME/cmdline-tools"
  rm -f "$commandline_tools_zip"
  mv "$ANDROID_HOME/cmdline-tools/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest"
}

[[ -n "$FIREBASE_APP_ID" ]] || { echo "FIREBASE_APP_ID must be set to the Firebase Android app id for package sg.mesha.goatos." >&2; exit 1; }
[[ "$GOOGLE_PLAY_PACKAGE" == "sg.mesha.goatos" ]] || { echo "GOOGLE_PLAY_PACKAGE must be sg.mesha.goatos for production-facing distribution." >&2; exit 1; }
test -f apps/goatos-android/app/src/prod/google-services.json || {
  echo "Missing apps/goatos-android/app/src/prod/google-services.json. Add Firebase Android app sg.mesha.goatos to project goatos-stg and download the config first." >&2
  exit 1
}
jq -er '.client[] | select(.client_info.android_client_info.package_name == "sg.mesha.goatos") | .client_info.mobilesdk_app_id' \
  apps/goatos-android/app/src/prod/google-services.json >/tmp/goatos-prod-firebase-app-id.txt || {
  echo "apps/goatos-android/app/src/prod/google-services.json must contain Android package sg.mesha.goatos." >&2
  exit 1
}
json_firebase_app_id="$(sed -n '1p' /tmp/goatos-prod-firebase-app-id.txt)"
[[ "$json_firebase_app_id" == "$FIREBASE_APP_ID" ]] || {
  echo "FIREBASE_APP_ID does not match app/src/prod/google-services.json: got $FIREBASE_APP_ID, want $json_firebase_app_id" >&2
  exit 1
}

notify_slack "STARTED" "Backend/web deploy finished; building signed GoatOS Android release."

install_android_sdk
yes | "$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" --licenses >/dev/null || true
"$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager" "platforms;android-36" "build-tools;36.0.0"

make restore-stg-android-release-env
source .local/android-signing/stg-release-env.sh

DEPLOY_VERSION_CODE="${GOATOS_ANDROID_VERSION_CODE:-}"
DEPLOY_VERSION_NAME="${GOATOS_ANDROID_VERSION_NAME:-}"

cd apps/goatos-android
common_gradle_args=(
  -x lintVitalProdRelease \
  -x lintVitalAnalyzeRelease \
  -x lintVitalAnalyzeProdRelease \
  --no-configuration-cache \
  -PallowDirtyFirebaseDistribution=true
)
apk_gradle_args=(
  :app:assembleProdRelease \
  :app:appDistributionUploadProdRelease \
  "${common_gradle_args[@]}" \
  -PfadReleaseNotes="GoatOS (Mesha) release from main ${commit_sha}"
)
bundle_gradle_args=(
  :app:bundleProdRelease \
  "${common_gradle_args[@]}"
)
if [[ -n "$DEPLOY_VERSION_CODE" ]]; then
  apk_gradle_args+=("-PgoatosVersionCode=$DEPLOY_VERSION_CODE")
  bundle_gradle_args+=("-PgoatosVersionCode=$DEPLOY_VERSION_CODE")
fi
if [[ -n "$DEPLOY_VERSION_NAME" ]]; then
  apk_gradle_args+=("-PgoatosVersionName=$DEPLOY_VERSION_NAME")
  bundle_gradle_args+=("-PgoatosVersionName=$DEPLOY_VERSION_NAME")
fi
./gradlew "${apk_gradle_args[@]}"
firebase_uploaded=true
./gradlew "${bundle_gradle_args[@]}"

cd "$repo_root"
APK="apps/goatos-android/app/build/outputs/apk/prod/release/app-prod-release.apk"
AAB="apps/goatos-android/app/build/outputs/bundle/prodRelease/app-prod-release.aab"
test -f "$APK"
test -f "$AAB"

ANDROID_VERSION_NAME="$("$ANDROID_HOME/cmdline-tools/latest/bin/apkanalyzer" manifest version-name "$APK")"
ANDROID_VERSION_CODE="$("$ANDROID_HOME/cmdline-tools/latest/bin/apkanalyzer" manifest version-code "$APK")"
DOWNLOAD_NAME="Mesha-${ANDROID_VERSION_NAME}.apk"

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
apk_mirrored=true

play_base="https://androidpublisher.googleapis.com/androidpublisher/v3/applications/${GOOGLE_PLAY_PACKAGE}"
if play_access_token="$(play_access_token)" &&
  edit_response="$(curl -sS -X POST -H "Authorization: Bearer ${play_access_token}" -H "x-goog-user-project: ${PLAY_QUOTA_PROJECT}" "${play_base}/edits")"; then
  edit_id="$(jq -r '.id // empty' <<<"$edit_response")"
  if [[ -n "$edit_id" ]]; then
    upload_response_file=".local/android-signing/play-upload-response.json"
    upload_status="$(
      curl -sS -o "$upload_response_file" -w '%{http_code}' -X POST \
        -H "Authorization: Bearer ${play_access_token}" \
        -H "x-goog-user-project: ${PLAY_QUOTA_PROJECT}" \
        -H "Content-Type: application/octet-stream" \
        --data-binary @"$AAB" \
        "https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications/${GOOGLE_PLAY_PACKAGE}/edits/${edit_id}/bundles?uploadType=media"
    )"
    if [[ "$upload_status" =~ ^2 ]]; then
      play_version_code="$(jq -r '.versionCode // empty' "$upload_response_file")"
      if [[ "$play_version_code" == "$ANDROID_VERSION_CODE" ]]; then
        jq -n --arg vc "$ANDROID_VERSION_CODE" '{
          releases: [{
            name: ("GoatOS " + $vc),
            status: "completed",
            versionCodes: [$vc]
          }]
        }' > .local/android-signing/play-internal-track.json

        if curl -fsS -X PUT \
          -H "Authorization: Bearer ${play_access_token}" \
          -H "x-goog-user-project: ${PLAY_QUOTA_PROJECT}" \
          -H "Content-Type: application/json" \
          --data-binary @.local/android-signing/play-internal-track.json \
          "${play_base}/edits/${edit_id}/tracks/internal" >/dev/null &&
          curl -fsS -X POST \
            -H "Authorization: Bearer ${play_access_token}" \
            -H "x-goog-user-project: ${PLAY_QUOTA_PROJECT}" \
            "${play_base}/edits/${edit_id}:commit" >/dev/null; then
          play_uploaded=true
        fi
      else
        echo "Play uploaded versionCode ${play_version_code:-empty}, APK has $ANDROID_VERSION_CODE; leaving Play Internal unchanged." >&2
      fi
    else
      echo "Play Internal upload skipped after HTTP $upload_status; Firebase and direct APK are published." >&2
      sed 's/^/play-upload-response: /' "$upload_response_file" >&2 || true
    fi
  else
    echo "Play edit was not created; Firebase and direct APK are published." >&2
    printf '%s\n' "$edit_response" | sed 's/^/play-edit-response: /' >&2
  fi
else
  echo "Could not start Play Internal upload; Firebase and direct APK are published." >&2
fi

if [[ "$play_uploaded" != "true" ]]; then
  echo "Play Internal did not update; failing mobile distribution instead of leaving a partial publish." >&2
  exit 1
fi

notify_slack "SUCCEEDED" "Mobile distribution succeeded: Firebase App Distribution uploaded, Play Internal updated to versionCode ${ANDROID_VERSION_CODE}, and mesha.sg/app.apk now serves ${DOWNLOAD_NAME}."
post_deploy_panel
trap - EXIT

echo "MOBILE_DISTRIBUTED ${commit_sha} ${ANDROID_VERSION_NAME} ${ANDROID_VERSION_CODE}"
