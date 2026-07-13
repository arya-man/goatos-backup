#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-goatos}"
DELIVERY_PIPELINE="${DELIVERY_PIPELINE:-goatos-stg}"
TARGET_ID="${TARGET_ID:-goatos-stg}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ "$PROJECT_ID" == "goatos-stg" ]] || die "PROJECT_ID must be goatos-stg, got $PROJECT_ID"
[[ "$PROJECT_NUMBER" == "514832198871" ]] || die "PROJECT_NUMBER must be 514832198871, got $PROJECT_NUMBER"
[[ "$REGION" == "asia-south1" ]] || die "REGION must be asia-south1, got $REGION"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [[ "${GOATOS_ALLOW_DIRTY_RELEASE:-}" != "1" ]]; then
  git diff --quiet || die "working tree has unstaged changes; commit or set GOATOS_ALLOW_DIRTY_RELEASE=1"
  git diff --cached --quiet || die "working tree has staged changes; commit or set GOATOS_ALLOW_DIRTY_RELEASE=1"
fi

active_account="$(gcloud config get-value account 2>/dev/null)"
active_project="$(gcloud config get-value project 2>/dev/null)"
[[ "$active_account" == "ravi@mesha.sg" ]] || die "active gcloud account must be ravi@mesha.sg, got $active_account"
[[ "$active_project" == "$PROJECT_ID" ]] || die "active gcloud project must be $PROJECT_ID, got $active_project"

commit_sha="$(git rev-parse --short=12 HEAD)"
registry="${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}"
backend_image="${registry}/backend:${commit_sha}"
migration_image="${registry}/migrate:${commit_sha}"
admin_web_image="${registry}/admin-web:${commit_sha}"
release_id="${RELEASE_ID:-goatos-stg-${commit_sha}-$(date -u +%Y%m%d%H%M%S)}"

echo "Creating Goat OS staging Cloud Deploy release"
echo "account=$active_account"
echo "project=$active_project"
echo "commit=$commit_sha"
echo "release=$release_id"

gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

docker build --platform linux/amd64 -f backend/Dockerfile -t "$backend_image" .
docker push "$backend_image"

docker build --platform linux/amd64 -f backend/Dockerfile.migrate -t "$migration_image" .
docker push "$migration_image"

docker build --platform linux/amd64 -f apps/admin-web/Dockerfile -t "$admin_web_image" .
docker push "$admin_web_image"

gcloud deploy releases create "$release_id" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --delivery-pipeline="$DELIVERY_PIPELINE" \
  --source=. \
  --skaffold-file=deploy/clouddeploy/stg/skaffold.yaml \
  --to-target="$TARGET_ID" \
  --labels="commit_sha=${commit_sha},deployed_by=cloud-deploy" \
  --deploy-parameters="customTarget/commitSha=${commit_sha},customTarget/backendImage=${backend_image},customTarget/migrationImage=${migration_image},customTarget/adminWebImage=${admin_web_image}"

echo "Release submitted: $release_id"
