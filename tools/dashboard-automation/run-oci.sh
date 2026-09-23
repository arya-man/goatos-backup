#!/usr/bin/env bash
# Guarded OCI entrypoint for dashboard automation.
#
# This script is intentionally small: the Node runner owns receipts, redaction,
# deterministic layers, and agent-review metadata. This wrapper only loads a
# local env file outside Git, proves the checkout identity, and calls the runner.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MODE="${GOATOS_DASHBOARD_AUTOMATION_MODE:-production-smoke}"
ENV_FILE="${GOATOS_DASHBOARD_AUTOMATION_ENV_FILE:-${HOME}/.config/goatos/dashboard-automation.env}"
OUT_ROOT="${GOATOS_DASHBOARD_AUTOMATION_OUT_ROOT:-${REPO_ROOT}/.codex-goatos-render/dashboard-automation}"

die() {
  echo "dashboard-automation-oci: $*" >&2
  exit 2
}

# ---------------------------------------------------------------------------
# ONE SWEEP AT A TIME, EVER.
#
# 2026-09-23: agents were run in parallel against production and saturated the
# API. Its ceiling is 2 instances x 10 concurrent requests = 20 slots; 18 headless
# browsers plus a latency sweep plus SQL on the primary filled every one of them,
# the dashboard's own requests queued behind them, and users got backend_down.
#
# This lock makes that impossible rather than discouraged. A second sweep does not
# queue and does not "run a bit slower" -- it refuses and exits. Nothing that talks
# to production may start while this is held.
# ---------------------------------------------------------------------------
LOCK_FILE="${GOATOS_DASHBOARD_LOCK_FILE:-${HOME}/.cache/goatos-dashboard-automation.lock}"
mkdir -p "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  holder="$(cat "$LOCK_FILE" 2>/dev/null || true)"
  die "another sweep is already running${holder:+ (started by ${holder})}; refusing to run a second one against production"
fi
echo "pid $$ on $(hostname) at $(date -u +%FT%TZ)" >&9 2>/dev/null || true

# Refuse to start if anything is already driving a browser at production: a stale
# sweep, a hand-run script, or an agent. Stopping the parent is not enough -- the
# browsers outlive it, which is exactly how this was missed the first time.
stray_browsers="$(pgrep -c -f "headless_shell|chrome-linux/chrome" 2>/dev/null || echo 0)"
if [ "${stray_browsers:-0}" -gt 0 ]; then
  die "${stray_browsers} browser process(es) are already running; refusing to add production load. Stop them, then rerun."
fi

# Cap how much this sweep may ask of production at once, well under the API's
# 20-slot ceiling so the dashboard always has room to answer a real person.
# The flicker sweep refuses to open a browser unless it can see this, so it can
# never run outside the flock taken above. It is exported HERE, after the lock
# and after the stray-browser refusal, so the two can never come apart: anything
# that skips this file skips the permission with it.
export GOATOS_DASHBOARD_LOCK_HELD=1
export GOATOS_SMOKE_MAX_CONCURRENCY="${GOATOS_SMOKE_MAX_CONCURRENCY:-4}"
export GOATOS_SMOKE_REQUEST_DELAY_MS="${GOATOS_SMOKE_REQUEST_DELAY_MS:-150}"
# Any SQL this sweep runs is against the PRIMARY, not a standby: pg_is_in_recovery()
# is false on GOATOS_STG_READONLY_DATABASE_URL. Cap it so a check can never become a
# long scan on the database serving the product.
export PGOPTIONS="${PGOPTIONS:--c statement_timeout=15000 -c idle_in_transaction_session_timeout=15000}"

case "$MODE" in
  production-smoke|post-main-certification) ;;
  *) die "unsupported GOATOS_DASHBOARD_AUTOMATION_MODE=${MODE}" ;;
esac

if [[ -f "$ENV_FILE" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
else
  die "env file not found: ${ENV_FILE}"
fi

cd "$REPO_ROOT"

runtime_env_file=""
cleanup_runtime_env() {
  if [[ -n "$runtime_env_file" && -f "$runtime_env_file" ]]; then
    rm -f "$runtime_env_file"
  fi
}
trap cleanup_runtime_env EXIT

if [[ -n "${GOATOS_FIREBASE_REFRESH_TOKEN:-}" ]]; then
  runtime_env_file="$(mktemp)"
  node tools/dashboard-automation/refresh-firebase-token.mjs \
    --out-env "$runtime_env_file" \
    --env-file "$ENV_FILE"
  # shellcheck disable=SC1090
  source "$runtime_env_file"
fi

if [[ -z "${CHROME_PATH:-}" && -d "${REPO_ROOT}/node_modules/playwright" ]]; then
  CHROME_PATH="$(node -e 'console.log(require("playwright").chromium.executablePath())')"
  export CHROME_PATH
fi

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  die "refusing dirty tracked checkout"
fi

current_sha="$(git rev-parse HEAD)"
origin_sha="$(git rev-parse origin/main)"
if [[ "$current_sha" != "$origin_sha" ]]; then
  die "checkout HEAD ${current_sha} does not match origin/main ${origin_sha}"
fi

mkdir -p "$OUT_ROOT"

exec node tools/dashboard-automation/run.mjs \
  --mode "$MODE" \
  --out-dir "${OUT_ROOT}/$(date -u +%Y%m%dT%H%M%SZ)-${current_sha:0:12}-${MODE}"
