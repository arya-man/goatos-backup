#!/usr/bin/env bash
# Poll origin/main and run post-main dashboard certification once per new SHA.
#
# This is intentionally OCI-local instead of GitHub-hosted: the production auth,
# OCI parity DB, and Slack delivery env live on the runner, not in GitHub.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_FILE="${GOATOS_DASHBOARD_AUTOMATION_ENV_FILE:-${HOME}/.config/goatos/dashboard-automation.env}"
STATE_DIR="${GOATOS_DASHBOARD_AUTOMATION_STATE_DIR:-${HOME}/.local/state/goatos/dashboard-automation}"
LAST_SHA_FILE="${STATE_DIR}/last-post-main-sha"
LOCK_DIR="${STATE_DIR}/post-main.lock"

die() {
  echo "dashboard-automation-post-main: $*" >&2
  exit 2
}

[[ -f "$ENV_FILE" ]] || die "env file not found: ${ENV_FILE}"

# shellcheck disable=SC1090
source "$ENV_FILE"

mkdir -p "$STATE_DIR"

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  echo "dashboard-automation-post-main: another run is active; skipping"
  exit 0
fi
trap 'rmdir "$LOCK_DIR"' EXIT

cd "$REPO_ROOT"

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  die "refusing dirty tracked checkout"
fi

git fetch --quiet origin main
remote_sha="$(git rev-parse origin/main)"
last_sha=""
if [[ -f "$LAST_SHA_FILE" ]]; then
  last_sha="$(<"$LAST_SHA_FILE")"
fi

if [[ "$remote_sha" == "$last_sha" ]]; then
  echo "dashboard-automation-post-main: origin/main unchanged at ${remote_sha}; skipping"
  exit 0
fi

git checkout --quiet main
git reset --quiet --hard "$remote_sha"

GOATOS_DASHBOARD_AUTOMATION_MODE=post-main-certification \
  "${REPO_ROOT}/tools/dashboard-automation/run-oci.sh"

printf '%s\n' "$remote_sha" >"$LAST_SHA_FILE"
echo "dashboard-automation-post-main: certified ${remote_sha}"
