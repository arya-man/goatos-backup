#!/usr/bin/env bash
# STG post-seed readiness gate.
#
# Run after DB seed closeout, Firebase password setup, and login grant
# materialization. This does not create business data. It proves the cloud-facing
# pieces needed after a seed are wired: login grants, GCS proof storage, FCM
# notification dispatch config, and CEO AI assistant config/grants.
set -euo pipefail

PROJECT="${GOATOS_STG_PROJECT:-goatos-stg}"
REGION="${GOATOS_STG_REGION:-asia-south1}"
TENANT_ID="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
API_SERVICE="${GOATOS_STG_API_SERVICE:-goatos-api-stg}"
KERNEL_SERVICE="${GOATOS_STG_KERNEL_SERVICE:-goatos-kernel-worker-stg}"
ADMIN_WEB_SERVICE="${GOATOS_STG_ADMIN_WEB_SERVICE:-goatos-admin-web-stg}"
EXPECT_ACCOUNT="${GOATOS_STG_ACCOUNT:-ravi@mesha.sg}"
EXPECT_ORG_ID="${GOATOS_STG_ORG_ID:-563962826703}"

failures=0
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

pass() { printf 'PASS  %s\n' "$*"; }
warn() { printf 'WARN  %s\n' "$*"; }
fail() { printf 'FAIL  %s\n' "$*" >&2; failures=$((failures + 1)); }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    fail "$name is required"
  else
    pass "$name is set"
  fi
}

service_json() {
  local service="$1" out="$2"
  if gcloud run services describe "$service" --project="$PROJECT" --region="$REGION" --format=json >"$out" 2>"$out.err"; then
    pass "Cloud Run service ${service} exists"
  else
    fail "Cloud Run service ${service} not readable: $(tr '\n' ' ' <"$out.err")"
    printf '{}\n' >"$out"
  fi
}

service_env() {
  local json="$1" name="$2"
  python3 - "$json" "$name" <<'PY'
import json, sys
path, name = sys.argv[1], sys.argv[2]
try:
    data = json.load(open(path))
except Exception:
    print("")
    raise SystemExit
containers = data.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
for container in containers:
    for env in container.get("env", []) or []:
        if env.get("name") == name:
            if "value" in env:
                print(env.get("value") or "")
            elif "valueFrom" in env:
                ref = env.get("valueFrom", {}).get("secretKeyRef", {})
                print(f"secret:{ref.get('name','')}:{ref.get('key','')}")
            raise SystemExit
print("")
PY
}

require_service_env() {
  local json="$1" service="$2" name="$3" expected="${4:-}"
  local value
  value="$(service_env "$json" "$name")"
  if [[ -z "${value// }" ]]; then
    fail "${service}: ${name} is missing"
    return
  fi
  if [[ -n "$expected" && "$value" != "$expected" ]]; then
    fail "${service}: ${name}=${value}, want ${expected}"
    return
  fi
  pass "${service}: ${name} is wired"
}

require_secret_enabled() {
  local name="$1"
  if gcloud secrets versions list "$name" --project="$PROJECT" --filter='state=enabled' --format='value(name)' 2>/dev/null | grep -q .; then
    pass "Secret ${name} has an enabled version"
  else
    fail "Secret ${name} has no enabled version in ${PROJECT}"
  fi
}

require_bucket() {
  local bucket="$1"
  if [[ -z "${bucket// }" || "$bucket" == secret:* ]]; then
    fail "GCS bucket name is not a literal Cloud Run env value; expose GOATOS_GCS_BUCKET as the stg bucket name"
    return
  fi
  if [[ "$bucket" != *stg* ]]; then
    fail "GOATOS_GCS_BUCKET=${bucket} does not look like a staging bucket"
    return
  fi
  if gcloud storage buckets describe "gs://${bucket}" --project="$PROJECT" >/dev/null 2>&1; then
    pass "GCS bucket gs://${bucket} exists in ${PROJECT}"
  else
    fail "GCS bucket gs://${bucket} not found/readable in ${PROJECT}"
  fi
}

sql_scalar() {
  local sql="$1"
  psql "$DATABASE_URL" -qAt -v ON_ERROR_STOP=1 -c "$sql" | tr -d '[:space:]'
}

check_context() {
  require_cmd gcloud
  require_cmd psql
  require_cmd python3
  local acct proj org
  acct="$(gcloud config get-value account 2>/dev/null || true)"
  proj="$(gcloud config get-value project 2>/dev/null || true)"
  if [[ "$acct" == "$EXPECT_ACCOUNT" ]]; then pass "gcloud account ${acct}"; else fail "gcloud account ${acct:-<unset>} != ${EXPECT_ACCOUNT}"; fi
  if [[ "$proj" == "$PROJECT" ]]; then pass "gcloud project ${proj}"; else fail "gcloud project ${proj:-<unset>} != ${PROJECT}"; fi
  org="$(gcloud projects get-ancestors "$PROJECT" --format='value(id)' 2>/dev/null | tail -1 || true)"
  if [[ "$org" == "$EXPECT_ORG_ID" ]]; then pass "project ${PROJECT} is under vgoats.com"; else fail "project ${PROJECT} ancestor ${org:-<unset>} != ${EXPECT_ORG_ID}"; fi
  require_env DATABASE_URL
}

check_login_grants() {
  echo "==> postflight: login grants"
  local uid_count leadership fields verifier_pending
  uid_count="$(sql_scalar "SELECT count(*) FROM user_scope_grants WHERE tenant_id='${TENANT_ID}'::uuid AND status='active' AND role IN ('ceo_internal','operator','pc_director');")"
  leadership="$(sql_scalar "SELECT count(*) FROM user_scope_grants WHERE tenant_id='${TENANT_ID}'::uuid AND status='active' AND role='ceo_internal';")"
  fields="$(sql_scalar "SELECT count(*) FROM user_scope_grants WHERE tenant_id='${TENANT_ID}'::uuid AND status='active' AND role IN ('operator','pc_director');")"
  verifier_pending="$(sql_scalar "SELECT count(*) FROM auth_pending_email_grants WHERE tenant_id='${TENANT_ID}'::uuid AND status='active' AND normalized_email='jyothipvg12345@gmail.com' AND role='verifier';")"
  if [[ "$uid_count" -ge 9 ]]; then pass "9 UID-backed login grants materialized"; else fail "active UID-backed grants=${uid_count}, want >=9"; fi
  if [[ "$leadership" -ge 5 ]]; then pass "5 leadership ceo_internal grants active"; else fail "leadership active grants=${leadership}, want >=5"; fi
  if [[ "$fields" -ge 4 ]]; then pass "4 field/director grants active"; else fail "field/director active grants=${fields}, want >=4"; fi
  if [[ "$verifier_pending" -ge 1 ]]; then pass "Jyothi verifier pending grant active"; else fail "Jyothi verifier pending grant missing"; fi
}

check_gcs() {
  echo "==> postflight: GCS proof storage"
  local api="$tmpdir/api.json"
  service_json "$API_SERVICE" "$api"
  require_service_env "$api" "$API_SERVICE" GOATOS_MEDIA_STORAGE gcs
  local bucket
  bucket="$(service_env "$api" GOATOS_GCS_BUCKET)"
  require_service_env "$api" "$API_SERVICE" GOATOS_GCS_BUCKET
  if [[ -n "$(service_env "$api" GOATOS_GCS_SERVICE_ACCOUNT_JSON)" || ( -n "$(service_env "$api" GOATOS_GCS_CLIENT_EMAIL)" && -n "$(service_env "$api" GOATOS_GCS_PRIVATE_KEY)" ) ]]; then
    pass "${API_SERVICE}: GCS signing identity is configured"
  else
    fail "${API_SERVICE}: no GCS signing identity env configured"
  fi
  require_bucket "$bucket"
}

check_fcm() {
  echo "==> postflight: FCM notification wiring"
  local kernel="$tmpdir/kernel.json"
  service_json "$KERNEL_SERVICE" "$kernel"
  local project topic devices
  project="$(service_env "$kernel" GOATOS_FCM_PROJECT_ID)"
  if [[ -z "$project" ]]; then
    project="$(service_env "$kernel" GOOGLE_CLOUD_PROJECT)"
  fi
  if [[ "$project" == "$PROJECT" ]]; then pass "${KERNEL_SERVICE}: FCM project points at ${PROJECT}"; else fail "${KERNEL_SERVICE}: FCM project=${project:-<missing>}, want ${PROJECT}"; fi
  topic="$(service_env "$kernel" GOATOS_FCM_DEFAULT_TOPIC)"
  devices="$(sql_scalar "SELECT count(*) FROM workforce_member_devices WHERE tenant_id='${TENANT_ID}'::uuid AND status='active' AND NULLIF(btrim(COALESCE(fcm_token,'')), '') IS NOT NULL;")"
  if [[ -n "${topic// }" || "$devices" -gt 0 ]]; then
    pass "FCM has default topic or active device token recipients"
  else
    fail "FCM has no GOATOS_FCM_DEFAULT_TOPIC and no active workforce_member_devices.fcm_token rows"
  fi
}

check_ceo_ai() {
  echo "==> postflight: CEO AI / chatbot wiring"
  local api="$tmpdir/api.json"
  [[ -f "$api" ]] || service_json "$API_SERVICE" "$api"
  require_service_env "$api" "$API_SERVICE" MESHA_AI_PROVIDER
  require_service_env "$api" "$API_SERVICE" MESHA_VERTEX_PROJECT "$PROJECT"
  require_service_env "$api" "$API_SERVICE" MESHA_VERTEX_LOCATION
  require_service_env "$api" "$API_SERVICE" MESHA_VERTEX_MODEL
  require_service_env "$api" "$API_SERVICE" MESHA_MCP_TOOLSET mesha_ceo_toolset
  require_secret_enabled mesha-cube-api-secret
  require_secret_enabled mesha-ceo-readonly-db-url
  require_secret_enabled mesha-cube-readonly-db-url
  require_secret_enabled mesha-mcp-toolset
  local readonly_roles ceo_ai_views ceo_role_can_read cube_role_can_read
  readonly_roles="$(sql_scalar "SELECT count(*) FROM pg_roles WHERE rolname IN ('mesha_ceo_readonly','mesha_cube_readonly');")"
  if [[ "$readonly_roles" -eq 2 ]]; then pass "CEO AI read-only DB roles exist"; else fail "CEO AI read-only DB roles found=${readonly_roles}, want 2"; fi
  ceo_ai_views="$(sql_scalar "SELECT count(*) FROM information_schema.views WHERE table_schema='ceo_ai';")"
  if [[ "$ceo_ai_views" -gt 0 ]]; then pass "ceo_ai reporting views exist"; else fail "ceo_ai reporting views missing"; fi
  ceo_role_can_read="$(sql_scalar "SELECT COALESCE(bool_and(has_table_privilege('mesha_ceo_readonly', format('%I.%I', table_schema, table_name), 'SELECT')), false) FROM information_schema.views WHERE table_schema='ceo_ai';")"
  cube_role_can_read="$(sql_scalar "SELECT COALESCE(bool_and(has_table_privilege('mesha_cube_readonly', format('%I.%I', table_schema, table_name), 'SELECT')), false) FROM information_schema.views WHERE table_schema='ceo_ai';")"
  if [[ "$ceo_role_can_read" == "t" ]]; then pass "mesha_ceo_readonly can SELECT ceo_ai views"; else fail "mesha_ceo_readonly lacks SELECT on at least one ceo_ai view"; fi
  if [[ "$cube_role_can_read" == "t" ]]; then pass "mesha_cube_readonly can SELECT ceo_ai views"; else fail "mesha_cube_readonly lacks SELECT on at least one ceo_ai view"; fi
  if [[ "${GOATOS_RUN_SEED_POSTFLIGHT_ASSISTANT_EVAL:-0}" == "1" ]]; then
    require_env MESHA_ASSISTANT_URL
    require_env GOATOS_EVAL_DATABASE_URL
    require_env GOATOS_EVAL_TENANT_ID
    make ceo-ai-eval
  else
    warn "CEO AI live ask/eval skipped; set GOATOS_RUN_SEED_POSTFLIGHT_ASSISTANT_EVAL=1 with MESHA_ASSISTANT_URL/GOATOS_EVAL_* to require it"
  fi
}

check_admin_web() {
  echo "==> postflight: admin-web service"
  local admin="$tmpdir/admin.json"
  service_json "$ADMIN_WEB_SERVICE" "$admin"
  pass "admin-web post-seed login/bootstrap smoke remains manual unless bearer/session env is supplied"
}

check_context
check_login_grants
check_gcs
check_fcm
check_ceo_ai
check_admin_web

if [[ "$failures" -gt 0 ]]; then
  echo "stg-seed-postflight: FAILED (${failures} issue(s))" >&2
  exit 1
fi
echo "stg-seed-postflight: PASS"
