#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-goatos}"
API_SERVICE="${API_SERVICE:-goatos-api-stg}"
ANALYTICS_EVENTS_SERVICE="${ANALYTICS_EVENTS_SERVICE:-goatos-analytics-events-stg}"
ANALYTICS_EVENTS_SERVICE_ACCOUNT="${ANALYTICS_EVENTS_SERVICE_ACCOUNT:-goatos-events-stg@goatos-stg.iam.gserviceaccount.com}"
GOATOS_STG_TENANT_ID="${GOATOS_STG_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
MCP_SERVICE="${MCP_SERVICE:-goatos-mcp-stg}"
KERNEL_WORKER_SERVICE="${KERNEL_WORKER_SERVICE:-goatos-kernel-worker-stg}"
HERD_SIGNALS_MQTT_BRIDGE_SERVICE="${HERD_SIGNALS_MQTT_BRIDGE_SERVICE:-goatos-herd-signals-mqtt-bridge-stg}"
HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT="${HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT:-goatos-hs-mqtt-bridge-stg@goatos-stg.iam.gserviceaccount.com}"
ADMIN_WEB_SERVICE="${ADMIN_WEB_SERVICE:-goatos-admin-web-stg}"
MIGRATE_JOB="${MIGRATE_JOB:-goatos-stg-migrate}"
VACCINATION_SCHEDULE_PROJECTOR_JOB="${VACCINATION_SCHEDULE_PROJECTOR_JOB:-goatos-stg-vaccination-schedule-projector}"
STG_API_URL="${STG_API_URL:-https://api.goatos.mesha.sg}"
STG_DASHBOARD_URL="${STG_DASHBOARD_URL:-https://dashboard.mesha.sg}"
GOATOS_CANONICAL_DASHBOARD_HOST="${GOATOS_CANONICAL_DASHBOARD_HOST:-dashboard.mesha.sg}"
GOATOS_API_BASE_URL="${GOATOS_API_BASE_URL:-https://api.goatos.mesha.sg/}"
GOATOS_STG_ZERO_DOWNTIME_DEPLOY="${CLOUD_DEPLOY_customTarget_zeroDowntimeDeploy:-${GOATOS_STG_ZERO_DOWNTIME_DEPLOY:-true}}"
GOATOS_ANALYTICS_SOURCE_APP_ID="${GOATOS_ANALYTICS_SOURCE_APP_ID:-sg.mesha.goatos}"
GOATOS_CRASHLYTICS_BQ_TABLE="${GOATOS_CRASHLYTICS_BQ_TABLE:-${PROJECT_ID}.firebase_crashlytics.sg_mesha_goatos_ANDROID}"
GOATOS_CRASHLYTICS_SESSIONS_TABLE="${GOATOS_CRASHLYTICS_SESSIONS_TABLE:-${PROJECT_ID}.firebase_sessions.sg_mesha_goatos_ANDROID}"
GOATOS_PERFORMANCE_BQ_TABLE="${GOATOS_PERFORMANCE_BQ_TABLE:-${PROJECT_ID}.firebase_performance.sg_mesha_goatos_ANDROID}"

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
  [[ "$GOATOS_STG_ZERO_DOWNTIME_DEPLOY" == "true" || "$GOATOS_STG_ZERO_DOWNTIME_DEPLOY" == "false" ]] ||
    die "customTarget/zeroDowntimeDeploy must be true or false, got $GOATOS_STG_ZERO_DOWNTIME_DEPLOY"
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

# Post-API parallel rollout lanes (see deploy()). Global so the EXIT trap can
# join them if the foreground dies while they still run.
PARALLEL_ROLLOUT_DIR=""
PARALLEL_ROLLOUT_PIDS=()

join_parallel_rollout_on_exit() {
  [[ "${#PARALLEL_ROLLOUT_PIDS[@]}" -gt 0 ]] || return 0
  local entry pid name
  echo "deploy exiting with parallel rollout lanes still running; waiting for them before reporting" >&2
  for entry in "${PARALLEL_ROLLOUT_PIDS[@]}"; do
    pid="${entry%%:*}"
    name="${entry#*:}"
    if wait "$pid"; then
      echo "parallel rollout lane ${name}: ok" >&2
    else
      echo "parallel rollout lane ${name}: FAILED" >&2
    fi
    sed "s/^/[${name}] /" "$PARALLEL_ROLLOUT_DIR/${name}.log" >&2 || true
  done
  PARALLEL_ROLLOUT_PIDS=()
}

write_failed_on_exit() {
  local rc=$?
  join_parallel_rollout_on_exit || true
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

service_account_exists() {
  gcloud iam service-accounts describe "$1" --project="$PROJECT_ID" >/dev/null 2>&1
}

secret_accessor_exists() {
  local secret="$1"
  local service_account="$2"
  local policy

  policy="$(gcloud secrets get-iam-policy "$secret" --project="$PROJECT_ID" --format=json)"
  python3 -c '
import json
import sys

service_account = sys.argv[1]
member = f"serviceAccount:{service_account}"
policy = json.load(sys.stdin)
for binding in policy.get("bindings", []):
    if binding.get("role") == "roles/secretmanager.secretAccessor" and member in binding.get("members", []):
        sys.exit(0)
sys.exit(1)
' "$service_account" <<<"$policy"
}

run_analytics_events_routing() {
  if [[ -x tools/deploy/stg-analytics-events-routing.sh ]]; then
    run tools/deploy/stg-analytics-events-routing.sh
    return
  fi
  run /usr/local/bin/goatos-stg-analytics-events-routing
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

wait_revision_ready() {
  local revision="$1"
  local expected_phase="${2:-ready}"
  local attempt ready_condition

  for attempt in $(seq 1 60); do
    ready_condition="$(
      gcloud run revisions describe "$revision" \
        --project="$PROJECT_ID" \
        --region="$REGION" \
        --format=json | python3 -c '
import json
import sys

doc = json.load(sys.stdin)
ready = ""
for condition in doc.get("status", {}).get("conditions", []):
    if condition.get("type") == "Ready":
        ready = condition.get("status") or ""
        break
print(ready)
'
    )"
    if [[ "$ready_condition" == "True" ]]; then
      return 0
    fi
    echo "waiting for revision $revision $expected_phase readiness: condition=${ready_condition:-?} attempt=$attempt"
    sleep 5
  done
  die "revision $revision did not reach $expected_phase readiness before continuing"
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

smoke_public_events_route() {
  local trace_id span_id traceparent code seen filter

  trace_id="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(16))
PY
)"
  span_id="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(8))
PY
)"
  traceparent="00-${trace_id}-${span_id}-01"
  code="$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST "$STG_API_URL/app/analytics/events" \
    -H "content-type: application/json" \
    -H "traceparent: $traceparent" \
    --data '{"events":[]}' || true)"
  [[ "$code" =~ ^(200|202|204|400|401|403)$ ]] ||
    die "$STG_API_URL/app/analytics/events returned unexpected smoke status $code"

  filter="resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"$ANALYTICS_EVENTS_SERVICE\" AND (jsonPayload.trace_id=\"$traceparent\" OR trace=\"projects/$PROJECT_ID/traces/$trace_id\")"
  for _ in {1..10}; do
    seen="$(gcloud logging read \
      "$filter" \
      --project="$PROJECT_ID" \
      --freshness=10m \
      --limit=1 \
      --format='value(resource.labels.service_name)')"
    [[ "$seen" == "$ANALYTICS_EVENTS_SERVICE" ]] && break
    sleep 3
  done
  [[ "$seen" == "$ANALYTICS_EVENTS_SERVICE" ]] ||
    die "public /app/analytics/events smoke did not land on $ANALYTICS_EVENTS_SERVICE (status=$code traceparent=$traceparent)"
}

assert_analytics_worker_iam() {
  local account policy
  account="$(gcloud run services describe "$KERNEL_WORKER_SERVICE" --project="$PROJECT_ID" --region="$REGION" --format='value(spec.template.spec.serviceAccountName)')"
  [[ -n "$account" ]] || die "Kernel worker runtime service account missing"
  policy="$(gcloud run jobs get-iam-policy goatos-stg-analytics-rollup --project="$PROJECT_ID" --region="$REGION" --format=json)"
  printf '%s' "$policy" | python3 -c 'import json,sys
policy=json.load(sys.stdin)
member="serviceAccount:"+sys.argv[1]
required={"roles/run.jobsExecutorWithOverrides","roles/run.viewer"}
actual={b["role"] for b in policy.get("bindings",[]) if member in b.get("members",[]) and not b.get("condition")}
assert required <= actual, "Kernel worker requires job-scoped execution and reconciliation IAM before deployment"' "$account"
}

analytics_rollup_env_vars() {
  printf '%s' "GOATOS_ANALYTICS_SOURCE_APP_ID=${GOATOS_ANALYTICS_SOURCE_APP_ID},GOATOS_CRASHLYTICS_BQ_TABLE=${GOATOS_CRASHLYTICS_BQ_TABLE},GOATOS_CRASHLYTICS_SESSIONS_TABLE=${GOATOS_CRASHLYTICS_SESSIONS_TABLE},GOATOS_PERFORMANCE_BQ_TABLE=${GOATOS_PERFORMANCE_BQ_TABLE}"
}

assert_analytics_rollup_env() {
  local job_json
  job_json="$(gcloud run jobs describe goatos-stg-analytics-rollup --project="$PROJECT_ID" --region="$REGION" --format=json)"
  printf '%s' "$job_json" | python3 -c '
import json
import sys

doc = json.load(sys.stdin)
expected = {
    "GOATOS_ANALYTICS_SOURCE_APP_ID": sys.argv[1],
    "GOATOS_CRASHLYTICS_BQ_TABLE": sys.argv[2],
    "GOATOS_CRASHLYTICS_SESSIONS_TABLE": sys.argv[3],
    "GOATOS_PERFORMANCE_BQ_TABLE": sys.argv[4],
}
containers = doc.get("spec", {}).get("template", {}).get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
if not containers:
    raise SystemExit("analytics-rollup job has no container template")
env = {item.get("name"): item.get("value") for item in containers[0].get("env", [])}
missing = [f"{key}={value}" for key, value in expected.items() if env.get(key) != value]
if missing:
    raise SystemExit("analytics-rollup Firebase export env mismatch: " + ", ".join(missing))
' "$GOATOS_ANALYTICS_SOURCE_APP_ID" "$GOATOS_CRASHLYTICS_BQ_TABLE" "$GOATOS_CRASHLYTICS_SESSIONS_TABLE" "$GOATOS_PERFORMANCE_BQ_TABLE"
}

# Grafana/Alloy/observability is owned and deployed by vgoats/mesha-ops
# (docs/decisions/grafana-owned-by-mesha-ops.md). The STG deploy only asserts
# the analytics-rollup job carries the verified backend image and Firebase env;
# the kernel worker triggers its executions, not the deploy.
assert_analytics_rollup_job() {
  [[ "$(job_image goatos-stg-analytics-rollup)" == "$BACKEND_IMAGE" ]] || die "analytics-rollup must use the verified backend image"
  assert_analytics_rollup_env
}

render() {
  assert_target
  require_release_inputs
  assert_image "backend" "$BACKEND_IMAGE"
  assert_image "migration" "$MIGRATION_IMAGE"
  assert_image "admin-web" "$ADMIN_WEB_IMAGE"

  local output_path="${CLOUD_DEPLOY_OUTPUT_GCS_PATH:-}"
  [[ -n "$output_path" ]] || die "CLOUD_DEPLOY_OUTPUT_GCS_PATH is required for render"
  local rollout_order="zero_downtime_migrate,api_and_worker,manual_backend_jobs,admin_web,smoke_and_skew"
  if [[ "$GOATOS_STG_ZERO_DOWNTIME_DEPLOY" != "true" ]]; then
    rollout_order="quiesce_api_admin,migrate,api_and_worker,manual_backend_jobs,admin_web,smoke_and_skew"
  fi

  cat > goatos-stg-release.txt <<EOF
project_id=$PROJECT_ID
project_number=$PROJECT_NUMBER
region=$REGION
commit_sha=$COMMIT_SHA
backend_image=$BACKEND_IMAGE
migration_image=$MIGRATION_IMAGE
admin_web_image=$ADMIN_WEB_IMAGE
zero_downtime_deploy=$GOATOS_STG_ZERO_DOWNTIME_DEPLOY
rollout_order=$rollout_order
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

  assert_analytics_worker_iam

  local backend_prefix="${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}/backend:"
  local updated_jobs=()
  local old_api_revisions=()
  local old_worker_revisions=()
  local revision
  local api_revision
  local admin_web_revision

  # Fail before touching the database when Terraform has not created every
  # release target. In particular, never migrate and then discover that the
  # consolidated worker service is absent.
  gcloud run services describe "$API_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$MCP_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$KERNEL_WORKER_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  gcloud run services describe "$ADMIN_WEB_SERVICE" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  service_account_exists "$ANALYTICS_EVENTS_SERVICE_ACCOUNT" ||
    die "analytics events service account $ANALYTICS_EVENTS_SERVICE_ACCOUNT is absent; apply infra or create the dedicated runtime account before deploy"
  secret_accessor_exists "goatos-stg-gcs-service-account-json" "$ANALYTICS_EVENTS_SERVICE_ACCOUNT" ||
    die "analytics events service account $ANALYTICS_EVENTS_SERVICE_ACCOUNT cannot read goatos-stg-gcs-service-account-json; apply infra before deploy"
  gcloud iam service-accounts describe "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE_ACCOUNT" --project="$PROJECT_ID" >/dev/null
  gcloud run jobs describe "$MIGRATE_JOB" --project="$PROJECT_ID" --region="$REGION" >/dev/null
  if ! job_exists "$VACCINATION_SCHEDULE_PROJECTOR_JOB"; then
    echo "optional job $VACCINATION_SCHEDULE_PROJECTOR_JOB is absent; skipping explicit projector execution"
  fi

  if [[ "$GOATOS_STG_ZERO_DOWNTIME_DEPLOY" == "true" ]]; then
    echo "zero-downtime STG deploy: keeping public API/admin revisions serving during migration"
  else
    # Emergency fallback for a known destructive migration. Keep current public
    # API/admin traffic pinned; only prepare an API candidate revision without
    # traffic. Admin-web is not touched until after migrations pass, so a failed
    # migration cannot take dashboard.mesha.sg down.
    while IFS= read -r revision; do
      [[ -n "$revision" ]] && old_api_revisions+=("$revision")
    done < <(capture_serving_revisions "$API_SERVICE")
    [[ "${#old_api_revisions[@]}" -gt 0 ]] || die "$API_SERVICE has no serving revision to quiesce"

    run gcloud run services update "$API_SERVICE" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --image="$BACKEND_IMAGE" \
      --ingress=internal-and-cloud-load-balancing \
      --no-traffic \
      --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy,rollout_phase=pre_migration_quiesce" \
      --quiet
    api_revision="$(gcloud run services describe "$API_SERVICE" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --format='value(status.latestCreatedRevisionName)')"
    [[ -n "$api_revision" ]] || die "$API_SERVICE did not create a pre-migration API revision"
    wait_revision_ready "$api_revision" "pre-migration quiesce"
  fi

  # The worker is background processing, not the public web/Android request path.
  # Drain it before migrations so no old worker keeps mutating rows mid-release.
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

  if [[ "$GOATOS_STG_ZERO_DOWNTIME_DEPLOY" != "true" ]]; then
    drain_replaced_revisions "$API_SERVICE" "${old_api_revisions[@]}"
  fi
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
    --ingress=internal-and-cloud-load-balancing \
    --min=1 \
    --max=4 \
    --min-instances=1 \
    --max-instances=4 \
    --concurrency=10 \
    --no-traffic \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet
  api_revision="$(gcloud run services describe "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --format='value(status.latestCreatedRevisionName)')"
  [[ -n "$api_revision" ]] || die "$API_SERVICE did not create an API revision before traffic switch"
  wait_revision_ready "$api_revision" "api post-migration pre-traffic"
  run gcloud run services update-traffic "$API_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    "--to-revisions=${api_revision}=100" \
    --quiet
  wait_service_ready "$API_SERVICE" "post-migration restore"

  # After the API has taken traffic, the remaining backend-image services are
  # independent of each other and of admin-web: run them concurrently and fail
  # the deploy if ANY of them fails. The strict order kernel-worker drain ->
  # migration -> API above is unchanged. Each lane logs to its own file, which
  # is printed after `wait` so the Cloud Deploy log stays readable.
  local parallel_entry parallel_pid parallel_name
  local parallel_failed=()
  PARALLEL_ROLLOUT_DIR="$(mktemp -d)"
  PARALLEL_ROLLOUT_PIDS=()
  (
    set -euo pipefail
    run gcloud run deploy "$ANALYTICS_EVENTS_SERVICE" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --image="$BACKEND_IMAGE" \
      --ingress=all \
      --service-account="$ANALYTICS_EVENTS_SERVICE_ACCOUNT" \
      --allow-unauthenticated \
      --add-cloudsql-instances="${PROJECT_ID}:${REGION}:goatos-stg-core-db" \
      --min-instances=0 \
      --max-instances=1 \
      --concurrency=20 \
      --set-env-vars="GOATOS_ENV=stg,GOATOS_HTTP_ADDR=:8080,GOATOS_API_ROUTE_MODE=events,GOATOS_AUTH_MODE=jwks,GOATOS_MEDIA_STORAGE=gcs,GOATOS_GCS_BUCKET=goatos-stg-media,GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS=${GOATOS_STG_TENANT_ID},GOATOS_ANALYTICS_MAX_IN_FLIGHT=1,GOATOS_PG_MAX_CONNS=2,GOATOS_PG_QUERY_TIMEOUT=3s" \
      --set-secrets="DATABASE_URL=goatos-stg-database-url:latest,GOATOS_AUTH_ISSUER=goatos-stg-auth-issuer:latest,GOATOS_AUTH_AUDIENCE=goatos-stg-auth-audience:latest,GOATOS_AUTH_JWKS_URL=goatos-stg-auth-jwks-url:latest,GOATOS_AUTH_ALLOWED_EMAILS=goatos-stg-auth-allowed-emails:latest,GOATOS_GCS_SERVICE_ACCOUNT_JSON=goatos-stg-gcs-service-account-json:latest,GOATOS_BULK_IMPORT_PREVIEW_SIGNING_KEY=goatos-stg-bulk-import-preview-signing-key:latest" \
      --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
      --quiet
    run gcloud run services update-traffic "$ANALYTICS_EVENTS_SERVICE" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --to-latest \
      --quiet
    wait_service_ready "$ANALYTICS_EVENTS_SERVICE" "post-migration restore"
    run_analytics_events_routing
  ) >"$PARALLEL_ROLLOUT_DIR/analytics-events.log" 2>&1 &
  PARALLEL_ROLLOUT_PIDS+=("$!:analytics-events")

  (
    set -euo pipefail
    run gcloud run services update "$KERNEL_WORKER_SERVICE" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --image="$BACKEND_IMAGE" \
      --min=2 \
      --max=2 \
      --min-instances=2 \
      --max-instances=2 \
      --no-cpu-throttling \
      --update-env-vars="GOATOS_WORKER_STAGES_ENABLED=true,GOATOS_ANALYTICS_ROLLUP_JOB=projects/${PROJECT_ID}/locations/${REGION}/jobs/goatos-stg-analytics-rollup" \
      --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
      --quiet
    wait_service_ready "$KERNEL_WORKER_SERVICE" "post-migration restore"
  ) >"$PARALLEL_ROLLOUT_DIR/kernel-worker.log" 2>&1 &
  PARALLEL_ROLLOUT_PIDS+=("$!:kernel-worker")

  (
    set -euo pipefail
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
  ) >"$PARALLEL_ROLLOUT_DIR/mcp.log" 2>&1 &
  PARALLEL_ROLLOUT_PIDS+=("$!:mcp")

  (
    set -euo pipefail
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
      --set-env-vars="GOATOS_ENV=stg,GOATOS_HEALTH_ADDR=:8080,HERD_SIGNALS_MQTT_TLS=true,HERD_SIGNALS_TENANT_ID=00000000-0000-4000-8000-000000000001,HERD_SIGNALS_DEFAULT_GATEWAY_ID=f130d402dcb4,HERD_SIGNALS_MQTT_BATCH_SIZE=50,HERD_SIGNALS_MQTT_BATCH_INTERVAL=2s,HERD_SIGNALS_MQTT_QUEUE_MAX=5000" \
      --set-secrets="DATABASE_URL=goatos-stg-database-url:latest,HERD_SIGNALS_MQTT_HOST=herd-signals-mqtt-host:latest,HERD_SIGNALS_MQTT_PORT=herd-signals-mqtt-port:latest,HERD_SIGNALS_MQTT_TOPIC=herd-signals-mqtt-topic:latest,HERD_SIGNALS_MQTT_CLIENT_ID=herd-signals-mqtt-client-id:latest,HERD_SIGNALS_MQTT_USERNAME=herd-signals-mqtt-username:latest,HERD_SIGNALS_MQTT_PASSWORD=herd-signals-mqtt-gateway-514060-password:latest,HERD_SIGNALS_MQTT_CA_CERT=herd-signals-mqtt-ca-crt:latest" \
      --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
      --quiet
    wait_service_ready "$HERD_SIGNALS_MQTT_BRIDGE_SERVICE" "post-migration restore"
  ) >"$PARALLEL_ROLLOUT_DIR/mqtt-bridge.log" 2>&1 &
  PARALLEL_ROLLOUT_PIDS+=("$!:mqtt-bridge")

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
      if [[ "$job" == "goatos-stg-analytics-rollup" ]]; then
        run gcloud run jobs update "$job" --project="$PROJECT_ID" --region="$REGION" \
          --args="-timeout=25m,-source=app_events,-lookback-days=3" \
          --update-env-vars="$(analytics_rollup_env_vars)" \
          --quiet
      fi
      updated_jobs+=("$job")
    else
      echo "skip non-backend-image job $job ($current_image)"
    fi
  done < <(gcloud run jobs list --project="$PROJECT_ID" --region="$REGION" --format='value(metadata.name)' | sort)

  run gcloud run services update "$ADMIN_WEB_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --image="$ADMIN_WEB_IMAGE" \
    --command="node" \
    --args="apps/admin-web/server.js" \
    --ingress=internal-and-cloud-load-balancing \
    --min=1 \
    --max=2 \
    --min-instances=1 \
    --max-instances=2 \
    --no-traffic \
    --update-env-vars="GOATOS_CANONICAL_DASHBOARD_HOST=${GOATOS_CANONICAL_DASHBOARD_HOST},GOATOS_API_BASE_URL=${GOATOS_API_BASE_URL},NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED=1,NEXT_PUBLIC_FARO_COLLECTOR_URL=https://goatos-stg-grafana-alloy-awtrpmn4za-el.a.run.app/collect,NEXT_PUBLIC_GOATOS_ENV=stg,NEXT_PUBLIC_APP_VERSION=${COMMIT_SHA}" \
    --update-secrets="GOATOS_FIREBASE_WEB_CONFIG=goatos-stg-firebase-web-config:latest" \
    --update-labels="commit_sha=${COMMIT_SHA},deployed_by=cloud-deploy" \
    --quiet
  admin_web_revision="$(gcloud run services describe "$ADMIN_WEB_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --format='value(status.latestCreatedRevisionName)')"
  [[ -n "$admin_web_revision" ]] || die "$ADMIN_WEB_SERVICE did not create an admin-web revision before traffic switch"
  wait_revision_ready "$admin_web_revision" "admin-web pre-traffic"
  run gcloud run services update-traffic "$ADMIN_WEB_SERVICE" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    "--to-revisions=${admin_web_revision}=100" \
    --quiet

  for parallel_entry in "${PARALLEL_ROLLOUT_PIDS[@]}"; do
    parallel_pid="${parallel_entry%%:*}"
    parallel_name="${parallel_entry#*:}"
    if wait "$parallel_pid"; then
      echo "parallel rollout lane ${parallel_name}: ok"
    else
      parallel_failed+=("$parallel_name")
    fi
    sed "s/^/[${parallel_name}] /" "$PARALLEL_ROLLOUT_DIR/${parallel_name}.log" || true
  done
  PARALLEL_ROLLOUT_PIDS=()
  [[ "${#parallel_failed[@]}" -eq 0 ]] || die "parallel post-API rollout failed for: ${parallel_failed[*]}"

  [[ "$(service_image "$API_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$API_SERVICE image did not settle on $BACKEND_IMAGE"
  [[ "$(service_image "$ANALYTICS_EVENTS_SERVICE")" == "$BACKEND_IMAGE" ]] || die "$ANALYTICS_EVENTS_SERVICE image did not settle on $BACKEND_IMAGE"
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
  smoke_public_events_route
  mcp_url="$(service_uri "$MCP_SERVICE")"
  smoke_http "$mcp_url/livez" "200"
  smoke_http "$mcp_url/readyz" "200"
  curl -fsSIL "$STG_DASHBOARD_URL/login" >/dev/null

  assert_analytics_rollup_job

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
