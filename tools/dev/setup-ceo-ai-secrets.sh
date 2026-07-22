#!/usr/bin/env bash
# ===========================================================================
# setup-ceo-ai-secrets.sh
#
# Idempotently CREATE / UPDATE the Google Secret Manager entries the Mesha
# leadership assistant ("CEO AI") runtime + CI depend on, in the canonical
# staging project, and grant least-privilege accessor to the service accounts
# that consume them.
#
#   SECRET SOURCE OF TRUTH: all credentials live in Google Secret Manager
#   (project goatos-stg, org vgoats.com). This script is the ONLY sanctioned way
#   to seed/rotate them. It NEVER prints a secret value and NEVER commits one.
#
# What it manages (creds = real secret; config = low-sensitivity but versioned
# here so one place is authoritative):
#
#   mesha-ceo-readonly-db-url   (CRED) postgres DSN for mesha_ceo_readonly
#   mesha-cube-readonly-db-url  (CRED) postgres DSN for mesha_cube_readonly
#   mesha-cube-api-secret       (CRED) Cube Core JWT signing secret
#   mesha-mcp-toolset           (CONFIG) toolset name served by MCP Toolbox
#
# Non-secret runtime config (Vertex project/location/model, MESHA_CUBE_URL,
# MESHA_MCP_TOOLBOX_URL) is documented as plain env in
# docs/runbooks/leadership-assistant-secrets.md and .env.ceo-ai.local.example —
# it is intentionally NOT stored as a secret.
#
# The real staging Cloud SQL read-only roles (mesha_ceo_readonly /
# mesha_cube_readonly) and their live DSNs are a PENDING DEPLOY STEP. Until that
# lands this script seeds the two DB-url secrets with a clearly-marked
# placeholder so the wiring, IAM, and CI are provable now, without fabricating a
# working staging credential. Pass --seed-placeholders to (re)seed placeholders,
# or supply real values via the env vars below to write a real version.
#
# Usage:
#   tools/dev/setup-ceo-ai-secrets.sh                 # create-if-missing, grant IAM
#   tools/dev/setup-ceo-ai-secrets.sh --seed-placeholders
#   tools/dev/setup-ceo-ai-secrets.sh --rotate-cube-api-secret
#
# Provide real credential values (writes a new version instead of a placeholder):
#   MESHA_CEO_READONLY_DB_URL=postgres://... \
#   MESHA_CUBE_READONLY_DB_URL=postgres://... \
#     tools/dev/setup-ceo-ai-secrets.sh
#
# Guardrails: refuses to run unless the active gcloud account + project + org
# match Mesha/VGoats (never Heva/Slice).
# ===========================================================================
set -euo pipefail

PROJECT="${MESHA_SECRETS_PROJECT:-goatos-stg}"
EXPECT_ACCOUNT="${MESHA_SECRETS_ACCOUNT:-ravi@mesha.sg}"
EXPECT_ORG_ID="563962826703" # vgoats.com

SEED_PLACEHOLDERS=0
ROTATE_CUBE_API=0
for arg in "$@"; do
  case "$arg" in
    --seed-placeholders) SEED_PLACEHOLDERS=1 ;;
    --rotate-cube-api-secret) ROTATE_CUBE_API=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

# --- accessor service accounts (least privilege) ---------------------------
# Backend API server calls Vertex/Cube/Toolbox/Postgres server-side.
BACKEND_SA="goatos-api-stg@${PROJECT}.iam.gserviceaccount.com"
# Cube + MCP Toolbox Cloud Run services are a pending deploy; grant them when
# they exist (script skips missing SAs with a notice).
CUBE_SA="mesha-cube-stg@${PROJECT}.iam.gserviceaccount.com"
TOOLBOX_SA="mesha-mcp-toolbox-stg@${PROJECT}.iam.gserviceaccount.com"

# --- org / account guard ---------------------------------------------------
guard_context() {
  local acct proj org
  acct="$(gcloud config get-value account 2>/dev/null || true)"
  proj="$(gcloud config get-value project 2>/dev/null || true)"
  if [[ "$acct" != "$EXPECT_ACCOUNT" ]]; then
    echo "REFUSING: active gcloud account '$acct' != expected '$EXPECT_ACCOUNT'." >&2
    echo "  Run: gcloud auth login ${EXPECT_ACCOUNT}" >&2
    exit 1
  fi
  if [[ "$proj" != "$PROJECT" ]]; then
    echo "REFUSING: active project '$proj' != expected '$PROJECT'." >&2
    echo "  Run: gcloud config set project ${PROJECT}" >&2
    exit 1
  fi
  org="$(gcloud projects describe "$PROJECT" --format='value(parent.id)' 2>/dev/null || true)"
  # parent may be a folder; walk up to the org.
  if [[ "$(gcloud projects get-ancestors "$PROJECT" --format='value(id)' 2>/dev/null | tail -1)" != "$EXPECT_ORG_ID" ]]; then
    echo "REFUSING: ${PROJECT} is not under the vgoats.com org (${EXPECT_ORG_ID}). Wrong org — stop." >&2
    exit 1
  fi
  echo "==> context OK: ${acct} / ${PROJECT} / org vgoats.com (${EXPECT_ORG_ID})"
}

# --- secret helpers --------------------------------------------------------
secret_exists() { gcloud secrets describe "$1" --project="$PROJECT" >/dev/null 2>&1; }

ensure_secret() {
  local name="$1" kind="$2" # kind: credential | config
  if secret_exists "$name"; then
    echo "  secret ${name} exists"
  else
    gcloud secrets create "$name" \
      --project="$PROJECT" \
      --replication-policy=automatic \
      --labels="app=ceo-ai,kind=${kind},managed-by=setup-ceo-ai-secrets" >/dev/null
    echo "  created secret ${name} (kind=${kind})"
  fi
}

# add_version reads the value from stdin so it never appears in argv / logs.
add_version() {
  local name="$1"
  gcloud secrets versions add "$name" --project="$PROJECT" --data-file=- >/dev/null
}

has_any_version() {
  gcloud secrets versions list "$1" --project="$PROJECT" \
    --filter='state=enabled' --format='value(name)' 2>/dev/null | grep -q .
}

grant_accessor() {
  local name="$1" sa="$2"
  if ! gcloud iam service-accounts describe "$sa" --project="$PROJECT" >/dev/null 2>&1; then
    echo "    (skip IAM) service account ${sa} not found yet — pending deploy"
    return 0
  fi
  gcloud secrets add-iam-policy-binding "$name" \
    --project="$PROJECT" \
    --member="serviceAccount:${sa}" \
    --role="roles/secretmanager.secretAccessor" \
    --condition=None >/dev/null 2>&1 || true
  echo "    granted secretAccessor on ${name} to ${sa}"
}

gen_secret() { openssl rand -hex 32; }

guard_context

echo "==> ensuring Secret Manager entries in ${PROJECT}"

# ---- credential secrets ----------------------------------------------------
ensure_secret mesha-ceo-readonly-db-url credential
ensure_secret mesha-cube-readonly-db-url credential
ensure_secret mesha-cube-api-secret credential

# ---- config secret ---------------------------------------------------------
ensure_secret mesha-mcp-toolset config

# ---- seed values -----------------------------------------------------------
PLACEHOLDER='PLACEHOLDER-pending-stg-cloudsql-readonly-role-see-docs/runbooks/leadership-assistant-secrets.md'

seed_db_url() {
  local name="$1" env_val="$2"
  if [[ -n "$env_val" ]]; then
    printf '%s' "$env_val" | add_version "$name"
    echo "  wrote REAL version for ${name} (from env)"
  elif [[ "$SEED_PLACEHOLDERS" == "1" ]] || ! has_any_version "$name"; then
    printf '%s' "$PLACEHOLDER" | add_version "$name"
    echo "  seeded PLACEHOLDER version for ${name} (real stg role pending)"
  else
    echo "  ${name} already has a version (left unchanged)"
  fi
}

seed_db_url mesha-ceo-readonly-db-url "${MESHA_CEO_READONLY_DB_URL:-}"
seed_db_url mesha-cube-readonly-db-url "${MESHA_CUBE_READONLY_DB_URL:-}"

# Cube API secret: a signing secret we own (not a Cloud SQL cred), so we can
# generate a real value now. Only (re)write when missing or on explicit rotate.
if [[ "$ROTATE_CUBE_API" == "1" ]] || ! has_any_version mesha-cube-api-secret; then
  if [[ -n "${MESHA_CUBE_API_SECRET:-}" ]]; then
    printf '%s' "$MESHA_CUBE_API_SECRET" | add_version mesha-cube-api-secret
    echo "  wrote mesha-cube-api-secret (from env)"
  else
    gen_secret | add_version mesha-cube-api-secret
    echo "  generated mesha-cube-api-secret (random 256-bit)"
  fi
else
  echo "  mesha-cube-api-secret already has a version (left unchanged)"
fi

# Toolset name is config, not a secret; seed the canonical value if empty.
if ! has_any_version mesha-mcp-toolset; then
  printf '%s' "${MESHA_MCP_TOOLSET:-mesha_ceo_toolset}" | add_version mesha-mcp-toolset
  echo "  seeded mesha-mcp-toolset=${MESHA_MCP_TOOLSET:-mesha_ceo_toolset}"
fi

# ---- IAM: least-privilege accessor grants ----------------------------------
echo "==> granting secretAccessor (least privilege)"
for name in mesha-ceo-readonly-db-url mesha-cube-readonly-db-url mesha-cube-api-secret mesha-mcp-toolset; do
  grant_accessor "$name" "$BACKEND_SA"
done
# Cube service only needs its own DB url + API secret.
grant_accessor mesha-cube-readonly-db-url "$CUBE_SA"
grant_accessor mesha-cube-api-secret "$CUBE_SA"
# Toolbox service only needs the ceo readonly DB url + toolset.
grant_accessor mesha-ceo-readonly-db-url "$TOOLBOX_SA"
grant_accessor mesha-mcp-toolset "$TOOLBOX_SA"

echo "==> done. Secret NAMES (values never printed):"
gcloud secrets list --project="$PROJECT" \
  --filter='labels.app=ceo-ai' --format='table(name,labels.kind)'
