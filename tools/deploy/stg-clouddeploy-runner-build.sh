#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
REGION="${REGION:-asia-south1}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-goatos}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ "$PROJECT_ID" == "goatos-stg" ]] || die "PROJECT_ID must be goatos-stg, got $PROJECT_ID"
[[ "$REGION" == "asia-south1" ]] || die "REGION must be asia-south1, got $REGION"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

tag="${1:-$(git rev-parse --short=12 HEAD)}"
image="${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}/clouddeploy-stg-runner:${tag}"

gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet >&2
docker build --platform linux/amd64 -f deploy/clouddeploy/stg/runner.Dockerfile -t "$image" . >&2
docker push "$image" >&2

digest="$(gcloud artifacts docker images describe "$image" --project="$PROJECT_ID" --format='value(image_summary.digest)')"
[[ -n "$digest" ]] || die "could not resolve digest for $image"

echo "${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}/clouddeploy-stg-runner@${digest}"
