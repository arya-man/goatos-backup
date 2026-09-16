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
# Execute the packaged entrypoint before publishing its digest. Syntax checks
# alone do not catch executable bits lost by an archived build context.
docker run --rm --platform linux/amd64 --entrypoint /bin/bash "$image" -ceu '
  test -x /usr/local/bin/goatos-stg-clouddeploy-task
  test -x /usr/local/bin/goatos-stg-analytics-events-routing
  test -r /usr/local/bin/goatos-stg-grafana-sso.py
  node --check /opt/goatos/tools/deploy/smoke-stg-grafana-dashboards.mjs
  test -r /opt/goatos/infra/observability/firebase-initial-export.json
  python3 -m py_compile /usr/local/bin/goatos-stg-grafana-sso.py /usr/local/bin/goatos-stg-observability.py
  bash -n /usr/local/bin/goatos-stg-clouddeploy-task
  bash -n /usr/local/bin/goatos-stg-analytics-events-routing
  set +e
  /usr/local/bin/goatos-stg-clouddeploy-task executable-smoke > /tmp/entrypoint-smoke.log 2>&1
  status=$?
  set -e
  test "$status" -eq 1
  grep -q "usage: .* render|deploy" /tmp/entrypoint-smoke.log
' >&2
docker push "$image" >&2

digest="$(gcloud artifacts docker images describe "$image" --project="$PROJECT_ID" --format='value(image_summary.digest)')"
[[ -n "$digest" ]] || die "could not resolve digest for $image"

echo "${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}/clouddeploy-stg-runner@${digest}"
