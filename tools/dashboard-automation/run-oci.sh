#!/usr/bin/env bash
# Guarded OCI entrypoint for dashboard automation.
#
# This script is intentionally small: the Node runner owns receipts, redaction,
# deterministic layers, and agent-review metadata. This wrapper only loads a
# local env file outside Git, proves the checkout identity, and calls the runner.
#
# Tooling ref vs. certified main (docs/runbooks/dashboard-automation-oci.md,
# "Tooling ref"): the automation CODE in this checkout tracks
# origin/${GOATOS_DASHBOARD_TOOLING_REF} (default ops/dashboard-automation), so a
# Slack-card fix does not need a Goat OS landing. The APPLICATION source being
# certified (admin-web routes, backend tests, the SHA the live API must report)
# is always an origin/main SHA, checked out into a separate git worktree with
# the tooling ref's tools/dashboard-automation overlaid on top of it.
# GOATOS_DASHBOARD_TOOLING_REF=main restores the old single-checkout behaviour.
#
# Everything runs inside main() so bash has parsed the whole file before the
# tooling sync below rewrites it on disk.
set -euo pipefail

main() {
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MODE="${GOATOS_DASHBOARD_AUTOMATION_MODE:-production-smoke}"
ENV_FILE="${GOATOS_DASHBOARD_AUTOMATION_ENV_FILE:-${HOME}/.config/goatos/dashboard-automation.env}"
OUT_ROOT="${GOATOS_DASHBOARD_AUTOMATION_OUT_ROOT:-${REPO_ROOT}/.codex-goatos-render/dashboard-automation}"
STATE_DIR="${GOATOS_DASHBOARD_AUTOMATION_STATE_DIR:-${HOME}/.local/state/goatos/dashboard-automation}"

die() {
  echo "dashboard-automation-oci: $*" >&2
  exit 2
}

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

TOOLING_REF="${GOATOS_DASHBOARD_TOOLING_REF:-ops/dashboard-automation}"
[[ "$TOOLING_REF" =~ ^[A-Za-z0-9._/-]+$ ]] || die "invalid GOATOS_DASHBOARD_TOOLING_REF=${TOOLING_REF}"

cd "$REPO_ROOT"

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
  die "refusing dirty tracked checkout"
fi

git fetch --quiet origin \
  "+refs/heads/main:refs/remotes/origin/main" \
  "+refs/heads/${TOOLING_REF}:refs/remotes/origin/${TOOLING_REF}" \
  || die "cannot fetch origin/main and origin/${TOOLING_REF}"
tooling_target="$(git rev-parse --verify --quiet "origin/${TOOLING_REF}^{commit}")" \
  || die "origin/${TOOLING_REF} does not exist; create it from main (runbook: Tooling ref)"

# Move the tooling checkout to origin/<tooling ref>, then re-exec once so the
# new copy of this script is what runs.
if [[ "$(git rev-parse HEAD)" != "$tooling_target" ]]; then
  [[ "${GOATOS_DASHBOARD_TOOLING_SYNCED:-0}" != "1" ]] \
    || die "tooling checkout still not at origin/${TOOLING_REF} after sync"
  git checkout --quiet -B "$TOOLING_REF" "$tooling_target"
  git reset --quiet --hard "$tooling_target"
  GOATOS_DASHBOARD_TOOLING_SYNCED=1 exec "${REPO_ROOT}/tools/dashboard-automation/run-oci.sh" "$@"
fi

tooling_sha="$(git rev-parse HEAD)"
origin_tooling_sha="$(git rev-parse "origin/${TOOLING_REF}")"
if [[ "$tooling_sha" != "$origin_tooling_sha" ]]; then
  die "checkout HEAD ${tooling_sha} does not match origin/${TOOLING_REF} ${origin_tooling_sha}"
fi

# The main SHA under certification: pinned by run-post-main-if-new.sh, else origin/main.
main_sha="${GOATOS_DASHBOARD_CERTIFY_MAIN_SHA:-$(git rev-parse origin/main)}"
main_sha="$(git rev-parse --verify --quiet "${main_sha}^{commit}")" || die "unknown main SHA ${main_sha}"
git merge-base --is-ancestor "$main_sha" origin/main || die "main SHA ${main_sha} is not on origin/main"

if [[ "$TOOLING_REF" == "main" ]]; then
  # Legacy single-checkout mode: tooling and application are the same main SHA.
  [[ "$tooling_sha" == "$main_sha" ]] \
    || die "checkout HEAD ${tooling_sha} does not match origin/main ${main_sha}"
  app_dir="$REPO_ROOT"
else
  app_dir="${GOATOS_DASHBOARD_APP_SOURCE_DIR:-${STATE_DIR}/app-main}"
  if [[ -e "${app_dir}/.git" ]]; then
    git -C "$app_dir" checkout --quiet --detach --force "$main_sha"
  else
    git worktree prune
    mkdir -p "$(dirname "$app_dir")"
    git worktree add --quiet --detach --force "$app_dir" "$main_sha"
  fi
  # Overlay this tooling SHA's tools/dashboard-automation onto main's app source.
  rm -rf "${app_dir}/tools/dashboard-automation"
  git archive "$tooling_sha" tools/dashboard-automation | tar -x -C "$app_dir"
  # Reuse the tooling checkout's installed dependencies (playwright, admin-web).
  for nm in node_modules apps/admin-web/node_modules; do
    if [[ -d "${REPO_ROOT}/${nm}" && ! -e "${app_dir}/${nm}" ]]; then
      ln -s "${REPO_ROOT}/${nm}" "${app_dir}/${nm}"
    fi
  done
  # Identity proof: main_sha everywhere except the overlay, and the overlay is tooling_sha.
  [[ "$(git -C "$app_dir" rev-parse HEAD)" == "$main_sha" ]] \
    || die "app source worktree ${app_dir} is not at main ${main_sha}"
  git -C "$app_dir" diff --quiet "$main_sha" -- . ':(exclude)tools/dashboard-automation' \
    || die "app source worktree ${app_dir} differs from main ${main_sha} outside tools/dashboard-automation"
  git -C "$app_dir" diff --quiet "$tooling_sha" -- tools/dashboard-automation \
    || die "tooling overlay in ${app_dir} differs from ${TOOLING_REF} ${tooling_sha}"
fi

cd "$app_dir"

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
  rm -f "$runtime_env_file"
fi

if [[ -z "${CHROME_PATH:-}" && -d "${app_dir}/node_modules/playwright" ]]; then
  CHROME_PATH="$(node -e 'console.log(require("playwright").chromium.executablePath())')"
  export CHROME_PATH
fi

mkdir -p "$OUT_ROOT"
export GOATOS_DASHBOARD_TOOLING_REF="$TOOLING_REF"
export GOATOS_DASHBOARD_TOOLING_SHA="$tooling_sha"
export GOATOS_DASHBOARD_CERTIFIED_MAIN_SHA="$main_sha"
echo "dashboard-automation-oci: tooling ${TOOLING_REF}@${tooling_sha} certifying main@${main_sha} in ${app_dir}"

exec node tools/dashboard-automation/run.mjs \
  --mode "$MODE" \
  --out-dir "${OUT_ROOT}/$(date -u +%Y%m%dT%H%M%SZ)-${main_sha:0:12}-${MODE}"
}

main "$@"
exit $?
