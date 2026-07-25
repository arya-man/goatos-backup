#!/usr/bin/env bash
# run-cube-local.sh — start Mesha Cube Core locally for the leadership assistant.
#
# Cube is the governed metric layer. Locally it runs as a single Docker
# container (cubejs/cube) that reads the canonical Goat OS Postgres through a
# read-only-intent DB user and serves the metric API on MESHA_CUBE_URL
# (default 127.0.0.1:4000).
#
# Idempotent: re-running reuses/replaces the named container. Config + model are
# bind-mounted from analytics/cube so edits are picked up on restart.
#
# Secrets/config come from the environment or a gitignored .env.ceo-ai.local at
# the repo root. NO secret value is committed. Required/consumed vars:
#
#   MESHA_CUBE_URL          (default http://127.0.0.1:4000)
#   MESHA_CUBE_API_SECRET   (required — JWT signing secret; backend signs with it)
#   MESHA_CUBE_DB_HOST      (default host.docker.internal)
#   MESHA_CUBE_DB_PORT      (default 5433)
#   MESHA_CUBE_DB_NAME      (default goatos)
#   MESHA_CUBE_DB_USER      (default postgres  — use mesha_cube_readonly in stg)
#   MESHA_CUBE_DB_PASSWORD  (required)
#
# Usage:
#   tools/dev/run-cube-local.sh            # start (or restart) Cube
#   tools/dev/run-cube-local.sh stop       # stop + remove the container
#   tools/dev/run-cube-local.sh logs       # follow logs
#   tools/dev/run-cube-local.sh status     # health probe

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CUBE_DIR="${REPO_ROOT}/analytics/cube"
CONTAINER="mesha-cube-local"
IMAGE="${MESHA_CUBE_IMAGE:-cubejs/cube:v1.3.63}"

# Load local env file if present (never committed).
ENV_FILE="${REPO_ROOT}/.env.ceo-ai.local"
if [[ -f "${ENV_FILE}" ]]; then
  # shellcheck disable=SC1090
  set -a; source "${ENV_FILE}"; set +a
fi

MESHA_CUBE_URL="${MESHA_CUBE_URL:-http://127.0.0.1:4000}"
DB_HOST="${MESHA_CUBE_DB_HOST:-host.docker.internal}"
DB_PORT="${MESHA_CUBE_DB_PORT:-5433}"
DB_NAME="${MESHA_CUBE_DB_NAME:-goatos}"
DB_USER="${MESHA_CUBE_DB_USER:-postgres}"
DB_PASSWORD="${MESHA_CUBE_DB_PASSWORD:-}"
API_SECRET="${MESHA_CUBE_API_SECRET:-}"

# Port that MESHA_CUBE_URL maps to on the host.
HOST_PORT="$(printf '%s' "${MESHA_CUBE_URL}" | sed -E 's#^https?://[^:/]+:?([0-9]+)?.*#\1#')"
HOST_PORT="${HOST_PORT:-4000}"

health() { curl -fsS "${MESHA_CUBE_URL%/}/readyz" >/dev/null 2>&1; }

cmd="${1:-start}"

case "${cmd}" in
  stop)
    docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
    echo "stopped ${CONTAINER}"
    exit 0
    ;;
  logs)
    exec docker logs -f "${CONTAINER}"
    ;;
  status)
    if health; then echo "cube healthy at ${MESHA_CUBE_URL}"; exit 0; else echo "cube NOT healthy at ${MESHA_CUBE_URL}"; exit 1; fi
    ;;
  start) ;;
  *) echo "unknown command: ${cmd}" >&2; exit 2 ;;
esac

if [[ -z "${API_SECRET}" ]]; then
  echo "ERROR: MESHA_CUBE_API_SECRET is required (set it in ${ENV_FILE} or the environment)." >&2
  exit 1
fi
if [[ -z "${DB_PASSWORD}" ]]; then
  echo "ERROR: MESHA_CUBE_DB_PASSWORD is required (set it in ${ENV_FILE} or the environment)." >&2
  exit 1
fi

# Idempotent: drop any prior container.
docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true

echo "starting ${CONTAINER} (${IMAGE}) -> ${MESHA_CUBE_URL}, db ${DB_USER}@${DB_HOST}:${DB_PORT}/${DB_NAME}"
docker run -d --name "${CONTAINER}" \
  -p "127.0.0.1:${HOST_PORT}:4000" \
  --add-host host.docker.internal:host-gateway \
  -e CUBEJS_DEV_MODE=true \
  -e CUBEJS_DB_TYPE=postgres \
  -e CUBEJS_DB_HOST="${DB_HOST}" \
  -e CUBEJS_DB_PORT="${DB_PORT}" \
  -e CUBEJS_DB_NAME="${DB_NAME}" \
  -e CUBEJS_DB_USER="${DB_USER}" \
  -e CUBEJS_DB_PASS="${DB_PASSWORD}" \
  -e CUBEJS_API_SECRET="${API_SECRET}" \
  -e CUBEJS_CUBESTORE_NO_UPGRADE=true \
  -v "${CUBE_DIR}/cube.js:/cube/conf/cube.js:ro" \
  -v "${CUBE_DIR}/model:/cube/conf/model:ro" \
  "${IMAGE}" >/dev/null

echo -n "waiting for cube health"
for _ in $(seq 1 60); do
  if health; then echo " ... ready"; echo "cube up at ${MESHA_CUBE_URL}"; exit 0; fi
  echo -n "."; sleep 1
done
echo ""
echo "ERROR: cube did not become healthy in time. Recent logs:" >&2
docker logs --tail 40 "${CONTAINER}" >&2 || true
exit 1
