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

plain_build() {
  echo "stg-image-build: plain docker build for ${image} ($1)"
  shift
  docker build --platform linux/amd64 "$@" -f "$dockerfile" -t "$image" .
  docker push "$image"
}

cached_build() {
  builder="goatos-$(printf '%s' "${repo_ref##*/}" | tr -c 'a-zA-Z0-9-' '-')-$$"
  # Creating the docker-container builder pulls moby/buildkit; a pull failure
  # or rate limit must fall back to the plain build, never fail the deploy.
  docker buildx create --name "$builder" --driver docker-container --use >/dev/null 2>&1 || return 3
  docker buildx inspect --bootstrap "$builder" >/dev/null 2>&1 || { docker buildx rm "$builder" >/dev/null 2>&1 || true; return 3; }
  rc=0
  docker buildx build \
    --platform linux/amd64 \
    --cache-from "type=registry,ref=${cache_ref}" \
    --cache-to "type=registry,ref=${cache_ref},mode=max,ignore-error=true" \
    --provenance=false \
    "$@" \
    -f "$dockerfile" \
    -t "$image" \
    --push \
    . || rc=$?
  docker buildx rm "$builder" >/dev/null 2>&1 || true
  return "$rc"
}

if [ "${GOATOS_IMAGE_BUILD_CACHE:-1}" = "1" ] && docker buildx version >/dev/null 2>&1; then
  rc=0
  cached_build "$@" || rc=$?
  if [ "$rc" -eq 3 ]; then
    plain_build "buildx builder unavailable" "$@"
  elif [ "$rc" -ne 0 ]; then
    exit "$rc"
  fi
else
  plain_build "buildx unavailable or cache disabled" "$@"
fi
echo "stg-image-build: pushed ${image}"
