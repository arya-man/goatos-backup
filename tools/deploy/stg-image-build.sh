#!/bin/sh
# Build and push ONE STG image at the exact deploy commit tag, with a BuildKit
# registry layer cache. Used by cloudbuild.stg.yaml image steps, which now run
# in parallel (see docs/progress/ci-deploy-speedup.md).
#
# Provenance is unchanged: every image is rebuilt from the checked-out commit
# and pushed at <repo>/<name>:<12-char sha> with the same --build-arg values the
# step passes. The cache ref (<repo>/<name>:buildcache) only supplies reusable
# layers (go mod download, npm ci, base images); it is never deployed.
#
# Cache failures never fail the build: cache-from on a missing ref is a no-op
# and cache-to uses ignore-error=true. If buildx is unavailable the script falls
# back to the previous plain `docker build` + `docker push`.
#
# usage: stg-image-build.sh <image-ref:tag> <dockerfile> [docker build args...]
set -eu

image="${1:?image ref required}"
dockerfile="${2:?dockerfile required}"
shift 2

repo_ref="${image%:*}"
cache_ref="${repo_ref}:buildcache"

if [ "${GOATOS_IMAGE_BUILD_CACHE:-1}" = "1" ] && docker buildx version >/dev/null 2>&1; then
  builder="goatos-$(printf '%s' "${repo_ref##*/}" | tr -c 'a-zA-Z0-9-' '-')-$$"
  docker buildx create --name "$builder" --driver docker-container --use >/dev/null
  trap 'docker buildx rm "$builder" >/dev/null 2>&1 || true' EXIT
  docker buildx build \
    --platform linux/amd64 \
    --cache-from "type=registry,ref=${cache_ref}" \
    --cache-to "type=registry,ref=${cache_ref},mode=max,ignore-error=true" \
    --provenance=false \
    "$@" \
    -f "$dockerfile" \
    -t "$image" \
    --push \
    .
else
  echo "stg-image-build: buildx unavailable or cache disabled; plain docker build for ${image}"
  docker build --platform linux/amd64 "$@" -f "$dockerfile" -t "$image" .
  docker push "$image"
fi
echo "stg-image-build: pushed ${image}"
