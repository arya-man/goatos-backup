#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
FIREBASE_APP_DISTRIBUTION_SECRET="${FIREBASE_APP_DISTRIBUTION_SECRET:-goatos-stg-firebase-app-distribution-sa-json}"
umask 077

die() {
  echo "restore-stg-android-release-env: $*" >&2
  exit 1
}

[[ "$PROJECT_ID" == "goatos-stg" ]] || die "PROJECT_ID must be goatos-stg, got $PROJECT_ID"

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || printf '%s\n' "${BUILD_WORKSPACE_DIRECTORY:-/workspace}")"
cd "$repo_root"

active_project="$(gcloud config get-value project 2>/dev/null)"
[[ "$active_project" == "$PROJECT_ID" ]] || die "active gcloud project must be $PROJECT_ID, got $active_project"

mkdir -p .local/android-signing
chmod 700 .local/android-signing

gcloud secrets versions access latest \
  --project "$PROJECT_ID" \
  --secret android-stg-upload-keystore-jks \
  --out-file .local/android-signing/goatos-stg-upload.jks

gcloud secrets versions access latest \
  --project "$PROJECT_ID" \
  --secret "$FIREBASE_APP_DISTRIBUTION_SECRET" \
  --out-file .local/android-signing/firebase-app-distribution-sa.json

cat > .local/android-signing/stg-release-env.sh <<EOF
export GOATOS_ANDROID_STG_KEYSTORE="$repo_root/.local/android-signing/goatos-stg-upload.jks"
export GOATOS_ANDROID_STG_KEYSTORE_PASSWORD="\$(gcloud secrets versions access latest --project $PROJECT_ID --secret android-stg-upload-keystore-password)"
export GOATOS_ANDROID_STG_KEY_ALIAS="\$(gcloud secrets versions access latest --project $PROJECT_ID --secret android-stg-upload-key-alias)"
export GOATOS_ANDROID_STG_KEY_PASSWORD="\$(gcloud secrets versions access latest --project $PROJECT_ID --secret android-stg-upload-key-password)"
export GOOGLE_APPLICATION_CREDENTIALS="$repo_root/.local/android-signing/firebase-app-distribution-sa.json"
EOF
chmod 600 \
  .local/android-signing/goatos-stg-upload.jks \
  .local/android-signing/firebase-app-distribution-sa.json \
  .local/android-signing/stg-release-env.sh

echo "restore-stg-android-release-env: restored signing key and Firebase upload credentials"
echo "restore-stg-android-release-env: run: source .local/android-signing/stg-release-env.sh"
