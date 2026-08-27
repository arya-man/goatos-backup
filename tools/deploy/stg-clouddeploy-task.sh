#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-goatos}"
API_SERVICE="${API_SERVICE:-goatos-api-stg}"
MCP_SERVICE="${MCP_SERVICE:-goatos-mcp-stg}"
KERNEL_WORKER_SERVICE="${KERNEL_WORKER_SERVICE:-goatos-kernel-worker-stg}"
HERD_SIGNALS_MQTT_BRIDGE_SERVICE="${HERD_SIGNALS_MQTT_BRIDGE_SERVICE:-goatos-herd-signals-mqtt-bridge-stg}"
HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT="${HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT:-goatos-hs-mqtt-bridge-stg@goatos-stg.iam.gserviceaccount.com}"
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

write_failed_on_exit() {
  local rc=$?
  if [[ "$rc" -ne 0 ]]; then
    write_results "FAILED" || true
  fi
}

trap write_failed_on_exit EXIT

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

service_uri() {
  gcloud run services describe "$1" --project="$PROJECT_ID" --region="$REGION" --format='value(status.url)'
}

job_image() {
  gcloud run jobs describe "$1" --project="$PROJECT_ID" --region="$REGION" --format=json | image_from_resource_json
}

job_exists() {
  gcloud run jobs describe "$1" --project="$PROJECT_ID" --region="$REGION" >/dev/null 2>&1
}

capture_serving_revisions() {
  local service="$1"

  gcloud run services describe "$service" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --format=json | python3 -c '
import json
import sys

doc = json.load(sys.stdin)
for target in doc.get("status", {}).get("traffic", []):
    if int(target.get("percent") or 0) <= 0:
        continue
    revision = target.get("revisionName")
    if revision:
        print(revision)
'
}

latest_ready_revision() {
  gcloud run services describe "$1" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --format='value(status.latestReadyRevisionName)'
}

wait_service_ready() {
  local service="$1"
  local expected_phase="${2:-ready}"
  local attempt latest_created latest_ready ready_condition

  for attempt in $(seq 1 60); do
    read -r latest_created latest_ready ready_condition < <(
      gcloud run services describe "$service" \
        --project="$PROJECT_ID" \
        --region="$REGION" \
        --format=json | python3 -c '
import json
import sys

doc = json.load(sys.stdin)
status = doc.get("status", {})
ready = ""
for condition in status.get("conditions", []):
    if condition.get("type") == "Ready":
        ready = condition.get("status") or ""
        break
print(status.get("latestCreatedRevisionName") or "", status.get("latestReadyRevisionName") or "", ready)
'
    )
    if [[ -n "$latest_created" && "$latest_created" == "$latest_ready" && "$ready_condition" == "True" ]]; then
      return 0
    fi
    echo "waiting for $service $expected_phase revision readiness: created=${latest_created:-?} ready=${latest_ready:-?} condition=${ready_condition:-?} attempt=$attempt"
    sleep 5
  done
  die "$service did not reach $expected_phase readiness before continuing"
}

drain_replaced_revisions() {
  local service="$1"
  shift
  local latest revision serving_count=0

  latest="$(latest_ready_revision "$service")"
  [[ -n "$latest" ]] || die "$service has no latest ready revision after replacement"

  while IFS= read -r revision; do
    [[ -n "$revision" ]] || continue
    serving_count=$((serving_count + 1))
    [[ "$revision" == "$latest" ]] || die "$service still routes traffic to old revision $revision"
  done < <(capture_serving_revisions "$service")
  [[ "$serving_count" -eq 1 ]] || die "$service must route 100% to exactly one latest revision before migration"

  for revision in "$@"; do
    [[ -n "$revision" ]] || continue
    [[ "$revision" != "$latest" ]] || continue
    run gcloud run revisions delete "$revision" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --no-async \
      --quiet
  done
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
rollout_order=quiesce_api_and_kernel_worker,migrate,restore_api_and_kernel_worker,manual_backend_jobs,admin_web,smoke_and_skew
external_mcp_service=$MCP_SERVICE
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
  local old_api_revisions=()
  local old_worker_revisions=()
  local revision

  # Fail before touching the database when Terraform has not created every
  # release target. In particular, never migrate and then discover that the
  # consolidated worker service is absent.
  gcloud run services describe "$API_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$MCP_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$KERNEL_WORKER_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$ADMIN_WEB_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud iam service-accounts describe "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT" --project="$PROJECT_ID" >/dev/null
  gcloud run jobs describe "$MIGRATE_JOB" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  if ! job_exists "$VACCINATION_SCHEDULE_PROJECTOR_JOB"; then
    echo "optional job $VACCINATION_SCHEDULE_PROJECTOR_JOB is absent; skipping explicit projector execution"
  fi

  # Contract migrations may remove database arbiters used by the prior binary. Quiesce public
  # writers first: admin-web is the public write entrypoint, and the API is also made internal
  # before old API/worker revisions are drained.
  run gcloud run services update "$ADMIN_WEB_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --ingress=internal \
    --min=0 \
    --max=1 \
    --min-instances=0 \
    --max-instances=1 \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy,rollout_phase=pre_migration_quiesce" \
    --quiet
  wait_service_ready "$ADMIN_WEB_SERVICE" "pre-migration quiesce"

  while IFS= read -r revision; do
    [[ -n "$revision" ]] && old_api_revisions+=("$revision")
  done < <(capture_serving_revisions "$API_SERVICE")
  [[ "${#old_api_revisions[@]}" -gt 0 ]] || die "$API_SERVICE has no serving revision to quiesce"

  run gcloud run services update "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --ingress=internal \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy,rollout_phase=pre_migration_quiesce" \
    --quiet
  run gcloud run services update-traffic "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --to-latest \
    --quiet
  wait_service_ready "$API_SERVICE" "pre-migration quiesce"

  # The worker has revision-level minimum instances, so lowering the next revision's minimum is
  # not itself a drain. Replace it with the new image with stages disabled, route to that revision,
  # then delete every previously serving revision before touching the schema.
  while IFS= read -r revision; do
    [[ -n "$revision" ]] && old_worker_revisions+=("$revision")
  done < <(capture_serving_revisions "$KERNEL_WORKER_SERVICE")
  [[ "${#old_worker_revisions[@]}" -gt 0 ]] || die "$KERNEL_WORKER_SERVICE has no serving revision to drain"

  run gcloud run services update "$KERNEL_WORKER_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --min=0 \
    --max=1 \
    --min-instances=0 \
    --max-instances=1 \
    --cpu-throttling \
    --update-env-vars="GOATOS_WORKER_STAGES_ENABLED=false" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy,rollout_phase=pre_migration_drain" \
    --quiet
  run gcloud run services update-traffic "$KERNEL_WORKER_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --to-latest \
    --quiet
  wait_service_ready "$KERNEL_WORKER_SERVICE" "pre-migration drain"

  drain_replaced_revisions "$API_SERVICE" "${old_api_revisions[@]}"
  drain_replaced_revisions "$KERNEL_WORKER_SERVICE" "${old_worker_revisions[@]}"

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

  if job_exists "$VACCINATION_SCHEDULE_PROJECTOR_JOB"; then
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
  fi

  run gcloud run services update "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --ingress=all \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet
  run gcloud run services update-traffic "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --to-latest \
    --quiet
  wait_service_ready "$API_SERVICE" "post-migration restore"

  run gcloud run services update "$KERNEL_WORKER_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --min=2 \
    --max=2 \
    --min-instances=2 \
    --max-instances=2 \
    --no-cpu-throttling \
    --update-env-vars="GOATOS_WORKER_STAGES_ENABLED=true" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet
  wait_service_ready "$KERNEL_WORKER_SERVICE" "post-migration restore"

  run gcloud run services update "$MCP_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --ingress=all \
    --update-env-vars="MESHA_MCP_PUBLIC_URL=https://mcp.mesha.sg,MESHA_MCP_TENANT_ID=00000000-0000-4000-8000-000000000001,MESHA_MCP_DEFAULT_PARK_ID=00000000-0000-4000-8000-000000003002" \
    --update-secrets="GOATOS_FIREBASE_WEB_CONFIG=goatos-stg-firebase-web-config:latest" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet
  run gcloud run services update-traffic "$MCP_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --to-latest \
    --quiet
  wait_service_ready "$MCP_SERVICE" "post-migration restore"

  run gcloud run deploy "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$BACKEND_IMAGE" \
    --command="/app/bin/herd-signals-mqtt-bridge" \
    --service-account="$HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT" \
    --ingress=internal \
    --min-instances=1 \
    --max-instances=1 \
    --cpu=1 \
    --memory=512Mi \
    --no-cpu-throttling \
    --add-cloudsql-instances="${PROJECT_ID}:${REGION}:goatos-stg-core-db" \
    --set-env-vars="GOATOS_ENV=stg,GOATOS_HEALTH_ADDR=:8080,HERD_SIGNALS_MQTT_HOST=__REDACTED_HERD_SIGNALS_MQTT_HOST__,HERD_SIGNALS_MQTT_PORT=8883,HERD_SIGNALS_MQTT_TLS=true,HERD_SIGNALS_MQTT_CLIENT_ID=herd-signals-mqtt-bridge-stg,HERD_SIGNALS_MQTT_USERNAME=__REDACTED_HERD_SIGNALS_MQTT_USERNAME__,HERD_SIGNALS_MQTT_TOPIC=GwData,HERD_SIGNALS_TENANT_ID=00000000-0000-4000-8000-000000000001,HERD_SIGNALS_DEFAULT_GATEWAY_ID=f130d402dcb4,HERD_SIGNALS_MQTT_BATCH_SIZE=50,HERD_SIGNALS_MQTT_BATCH_INTERVAL=2s,HERD_SIGNALS_MQTT_QUEUE_MAX=5000" \
    --set-secrets="DATABASE_URL=goatos-stg-database-url:latest,HERD_SIGNALS_MQTT_PASSWORD=herd-signals-mqtt-gateway-514060-password:latest,HERD_SIGNALS_MQTT_CA_CERT=herd-signals-mqtt-ca-crt:latest" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet
  wait_service_ready "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE" "post-migration restore"

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
    --ingress=all \
    --min=1 \
    --max=4 \
    --min-instances=1 \
    --max-instances=4 \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet

  [[ "$(service_image "$API_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$API_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$MCP_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$MCP_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$KERNEL_WORKER_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$KERNEL_WORKER_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$ADMIN_WEB_SERVICE")" == "$ADMIN_WEB_IMAGE" ]] || die "$ADMIN_WEB_SERVICE image did not settle on $ADMIN_WEB_IMAGE"
  [[ "$(job_image "$MIGRATE_JOB")" == "$MIGRATION_IMAGE" ]] || die "$MIGRATE_JOB image did not settle on $MIGRATION_IMAGE"
  if job_exists "$VACCINATION_SCHEDULE_PROJECTOR_JOB"; then
    [[ "$(job_image "$VACCINATION_SCHEDULE_PROJECTOR_JOB")" == "$BACKEND_IMAGE" ]] || die "$VACCINATION_SCHEDULE_PROJECTOR_JOB image did not settle on $BACKEND_IMAGE"
  fi

  for job in "${updated_jobs[@]}"; do
    [[ "$(job_image "$job")" == "$BACKEND_IMAGE" ]] || die "$job image did not settle on $BACKEND_IMAGE"
  done

  smoke_http "$STG_API_URL/livez" "204"
  smoke_http "$STG_API_URL/readyz" "204"
  mcp_url="$(service_uri "$MCP_SERVICE")"
  smoke_http "$mcp_url/livez" "200"
  smoke_http "$mcp_url/readyz" "200"
  curl -fsSIL "$STG_DASHBOARD_URL/login" >/dev/null

  printf 'cloud-deploy-stg-ok commit=%s backend_jobs=%s api=%s mcp=%s worker=%s admin=%s\n' \
    "$COMMIT_SHA" "${#updated_jobs[@]}" "$BACKEND_IMAGE" "$BACKEND_IMAGE" "$BACKEND_IMAGE" "$ADMIN_WEB_IMAGE"
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

main "$@"
