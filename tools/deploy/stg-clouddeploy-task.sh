#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-goatos}"
API_SERVICE="${API_SERVICE:-goatos-api-stg}"
KERNEL_WORKER_SERVICE="${KERNEL_WORKER_SERVICE:-goatos-kernel-worker-stg}"
ADMIN_WEB_SERVICE="${ADMIN_WEB_SERVICE:-goatos-admin-web-stg}"
MIGRATE_JOB="${MIGRATE_JOB:-goatos-stg-migrate}"
VACCINATION_SCHEDULE_PROJECTOR_JOB="${VACCINATION_SCHEDULE_PROJECTOR_JOB:-goatos-stg-vaccination-schedule-projector}"
STG_API_URL="${STG_API_URL:-https://goatos-api-stg-awtrpmn4za-el.a.run.app}"
STG_DASHBOARD_URL="${STG_DASHBOARD_URL:-https://stg.dashboard.mesha.sg}"

COMMIT_SHA="${CLOUD_DEPLOY_customTarget_commitSha:-}"
BACKEND_IMAGE="${CLOUD_DEPLOY_customTarget_backendImage:-}"
MIGRATION_IMAGE="${CLOUD_DEPLOY_customTarget_migrationImage:-}"
ADMIN_WEB_IMAGE="${CLOUD_DEPLOY_customTarget_adminWebImage:-}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

require_param() {
  local name="$1"
  local value="$2"
  [[ -n "$value" ]] || die "missing Cloud Deploy parameter $name"
}

require_release_inputs() {
  require_param "customTarget/commitSha" "$COMMIT_SHA"
  require_param "customTarget/backendImage" "$BACKEND_IMAGE"
  require_param "customTarget/migrationImage" "$MIGRATION_IMAGE"
  require_param "customTarget/adminWebImage" "$ADMIN_WEB_IMAGE"
}

assert_target() {
  [[ "$PROJECT_ID" == "goatos-stg" ]] || die "PROJECT_ID must be goatos-stg, got $PROJECT_ID"
  [[ "$PROJECT_NUMBER" == "514832198871" ]] || die "PROJECT_NUMBER must be 514832198871, got $PROJECT_NUMBER"
  [[ "$REGION" == "asia-south1" ]] || die "REGION must be asia-south1, got $REGION"
}

assert_image() {
  local purpose="$1"
  local image="$2"
  local expected_prefix="${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}/"

  [[ "$image" == "$expected_prefix"* ]] || die "$purpose image is outside $expected_prefix: $image"
  gcloud artifacts docker images describe "$image" --project="$PROJECT_ID" >/dev/null
}

run() {
  echo "+ $*"
  "$@"
}

write_results() {
  local status="$1"
  local manifest_file="${2:-}"
  local output_path="${CLOUD_DEPLOY_OUTPUT_GCS_PATH:-}"

  [[ -n "$output_path" ]] || return 0

  python3 - "$status" "$manifest_file" > results.json <<'PY'
import json
import sys

payload = {"resultStatus": sys.argv[1]}
if len(sys.argv) > 2 and sys.argv[2]:
    payload["manifestFile"] = sys.argv[2]
print(json.dumps(payload, sort_keys=True))
PY
  gcloud storage cp results.json "$output_path/results.json" >/dev/null
}

image_from_resource_json() {
  python3 -c '
import json
import sys

doc = json.load(sys.stdin)
spec = doc.get("spec", {})
tmpl = spec.get("template", {})

containers = None
if isinstance(tmpl, dict):
    service_spec = tmpl.get("spec", {})
    if isinstance(service_spec, dict) and "containers" in service_spec:
        containers = service_spec.get("containers", [])
    job_template = service_spec.get("template", {}) if isinstance(service_spec, dict) else {}
    job_spec = job_template.get("spec", {}) if isinstance(job_template, dict) else {}
    if containers is None and isinstance(job_spec, dict):
        containers = job_spec.get("containers", [])

for container in containers or []:
    image = container.get("image")
    if image:
        print(image)
' | sed -n '1p'
}

service_image() {
  gcloud run services describe "$1" --project="$PROJECT_ID" --region="$REGION" --format=json | image_from_resource_json
}

job_image() {
  gcloud run jobs describe "$1" --project="$PROJECT_ID" --region="$REGION" --format=json | image_from_resource_json
}

smoke_http() {
  local url="$1"
  local expected="$2"
  local code

  code="$(curl -fsS -o /dev/null -w '%{http_code}' "$url")"
  [[ "$code" == "$expected" ]] || die "$url returned $code, expected $expected"
}

render() {
  assert_target
  require_release_inputs
  assert_image "backend" "$BACKEND_IMAGE"
  assert_image "migration" "$MIGRATION_IMAGE"
  assert_image "admin-web" "$ADMIN_WEB_IMAGE"

  local output_path="${CLOUD_DEPLOY_OUTPUT_GCS_PATH:-}"
  [[ -n "$output_path" ]] || die "CLOUD_DEPLOY_OUTPUT_GCS_PATH is required for render"

  cat > goatos-stg-release.txt <<EOF
project_id=$PROJECT_ID
project_number=$PROJECT_NUMBER
region=$REGION
commit_sha=$COMMIT_SHA
backend_image=$BACKEND_IMAGE
migration_image=$MIGRATION_IMAGE
admin_web_image=$ADMIN_WEB_IMAGE
rollout_order=migrate,vaccination_schedule_projection,api,kernel_worker,manual_backend_jobs,admin_web,smoke_and_skew
EOF

  local manifest_uri="$output_path/goatos-stg-release.txt"
  run gcloud storage cp goatos-stg-release.txt "$manifest_uri"
  write_results "SUCCEEDED" "$manifest_uri"
}

deploy() {
  assert_target
  require_release_inputs
  assert_image "backend" "$BACKEND_IMAGE"
  assert_image "migration" "$MIGRATION_IMAGE"
  assert_image "admin-web" "$ADMIN_WEB_IMAGE"

  local backend_prefix="${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}/backend:"
  local updated_jobs=()

  # Fail before touching the database when Terraform has not created every
  # release target. In particular, never migrate and then discover that the
  # consolidated worker service is absent.
  gcloud run services describe "$API_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$KERNEL_WORKER_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$ADMIN_WEB_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run jobs describe "$MIGRATE_JOB" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run jobs describe "$VACCINATION_SCHEDULE_PROJECTOR_JOB" --project="$PROJECT_ID" --region="$REGION" >/dev/null

  run gcloud run jobs update "$MIGRATE_JOB" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$MIGRATION_IMAGE" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet

  run gcloud run jobs execute "$MIGRATE_JOB" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --wait \
    --quiet

  run gcloud run jobs update "$VACCINATION_SCHEDULE_PROJECTOR_JOB" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet

  run gcloud run jobs execute "$VACCINATION_SCHEDULE_PROJECTOR_JOB" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --wait \
    --quiet
  updated_jobs+=("$VACCINATION_SCHEDULE_PROJECTOR_JOB")

  run gcloud run services update "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet

  run gcloud run services update "$KERNEL_WORKER_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet

  while IFS= read -r job; do
    [[ -n "$job" ]] || continue
    [[ "$job" != "$MIGRATE_JOB" ]] || continue
    [[ "$job" != "$VACCINATION_SCHEDULE_PROJECTOR_JOB" ]] || continue

    current_image="$(job_image "$job")"
    if [[ "$current_image" == "$backend_prefix"* ]]; then
      run gcloud run jobs update "$job" \
        --project="$PROJECT_ID" \
        --region="$REGION" \
        --image="$BACKEND_IMAGE" \
        --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
        --quiet
      updated_jobs+=("$job")
    else
      echo "skip non-backend-image job $job ($current_image)"
    fi
  done < <(gcloud run jobs list --project="$PROJECT_ID" --region="$REGION" --format='value(metadata.name)' | sort)

  run gcloud run services update "$ADMIN_WEB_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$ADMIN_WEB_IMAGE" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet

  [[ "$(service_image "$API_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$API_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$KERNEL_WORKER_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$KERNEL_WORKER_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$ADMIN_WEB_SERVICE")" == "$ADMIN_WEB_IMAGE" ]] || die "$ADMIN_WEB_SERVICE image did not settle on $ADMIN_WEB_IMAGE"
  [[ "$(job_image "$MIGRATE_JOB")" == "$MIGRATION_IMAGE" ]] || die "$MIGRATE_JOB image did not settle on $MIGRATION_IMAGE"
  [[ "$(job_image "$VACCINATION_SCHEDULE_PROJECTOR_JOB")" == "$BACKEND_IMAGE" ]] || die "$VACCINATION_SCHEDULE_PROJECTOR_JOB image did not settle on $BACKEND_IMAGE"

  for job in "${updated_jobs[@]}"; do
    [[ "$(job_image "$job")" == "$BACKEND_IMAGE" ]] || die "$job image did not settle on $BACKEND_IMAGE"
  done

  smoke_http "$STG_API_URL/livez" "204"
  smoke_http "$STG_API_URL/readyz" "204"
  curl -fsSIL "$STG_DASHBOARD_URL/login" >/dev/null

  printf 'cloud-deploy-stg-ok commit=%s backend_jobs=%s api=%s worker=%s admin=%s\n' \
    "$COMMIT_SHA" "${#updated_jobs[@]}" "$BACKEND_IMAGE" "$BACKEND_IMAGE" "$ADMIN_WEB_IMAGE"
  write_results "SUCCEEDED"
}

main() {
  local command="${1:-}"
  case "$command" in
    render) render ;;
    deploy) deploy ;;
    *) die "usage: $0 render|deploy" ;;
  esac
}

if ! main "$@"; then
  rc=$?
  write_results "FAILED" || true
  exit "$rc"
fi
