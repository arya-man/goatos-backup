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

case "$MODE" in
  production-smoke|post-main-certification) ;;
  *) die "unsupported GOATOS_DASHBOARD_AUTOMATION_MODE=${MODE}" ;;
esac

if [[ -f "$ENV_FILE" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
else
  die "env file not found: ${ENV_FILE}"
fi

cd "$REPO_ROOT"

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
