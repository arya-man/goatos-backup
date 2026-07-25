#!/usr/bin/env bash
# Run the Google genai MCP Toolbox locally for the Mesha leadership assistant.
#
# What it does (idempotent):
#   1. loads local config from .env.ceo-ai.local (gitignored; names + retrieval
#      documented in docs/runbooks/mcp-toolbox-local.md),
#   2. ensures the pinned toolbox binary exists (downloads if missing),
#   3. starts the Toolbox server on 127.0.0.1:5001 serving
#      docs/ceo-ai/mcp-toolbox-tools.yaml with the native /api endpoint enabled,
#   4. health-checks it and exits non-zero if it does not come up.
#
# Re-running while a healthy server is already listening on the port is a no-op
# (it reports the running instance and exits 0), unless --restart is passed.
#
# The read-only SQL fallback is NOT served here; the Go backend sqlguard owns
# tier-4 fallback. This process only serves the curated ceo_ai.* tools.
#
# Usage:
#   tools/dev/run-mcp-toolbox-local.sh              # start (or no-op if healthy)
#   tools/dev/run-mcp-toolbox-local.sh --restart    # stop any running one, start fresh
#   tools/dev/run-mcp-toolbox-local.sh --foreground  # run attached (Ctrl-C to stop)
#   tools/dev/run-mcp-toolbox-local.sh --stop        # stop the running server
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo"

TOOLBOX_VERSION="${TOOLBOX_VERSION:-0.32.0}"
TOOLBOX_BIN="${TOOLBOX_BIN:-$repo/.toolbox-bin/toolbox}"
CONFIG_FILE="$repo/docs/ceo-ai/mcp-toolbox-tools.yaml"
ENV_FILE="${MESHA_CEO_AI_ENV_FILE:-$repo/.env.ceo-ai.local}"
ADDRESS="${MESHA_MCP_TOOLBOX_ADDRESS:-127.0.0.1}"
PORT="${MESHA_MCP_TOOLBOX_PORT:-5001}"
PID_FILE="$repo/.toolbox-bin/toolbox-local.pid"
LOG_FILE="$repo/.toolbox-bin/toolbox-local.log"

mode="start"
foreground=0
for arg in "${@:-}"; do
  case "$arg" in
    --restart) mode="restart" ;;
    --stop) mode="stop" ;;
    --foreground|-f) foreground=1 ;;
    "") ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '[run-mcp-toolbox-local] %s\n' "$*" >&2; }

health_url="http://${ADDRESS}:${PORT}/"

is_healthy() {
  curl -fsS -o /dev/null --max-time 2 "$health_url" 2>/dev/null
}

stop_server() {
  if [ -f "$PID_FILE" ]; then
    local pid; pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
      log "stopping toolbox (pid $pid)"
      kill "$pid" 2>/dev/null || true
      for _ in 1 2 3 4 5; do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.3
      done
      kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$PID_FILE"
  fi
}

# ---- --stop short-circuit ---------------------------------------------------
if [ "$mode" = "stop" ]; then
  stop_server
  log "stopped"
  exit 0
fi

# ---- env --------------------------------------------------------------------
if [ -f "$ENV_FILE" ]; then
  log "loading env from $ENV_FILE"
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
else
  log "WARNING: $ENV_FILE not found."
  log "Create it from Secret Manager (see docs/runbooks/mcp-toolbox-local.md)."
  log "Falling back to any MESHA_MCP_DB_* already exported in the environment."
fi

# Sensible local defaults so a laptop dev can run against goatos-local-current.
export MESHA_MCP_DB_HOST="${MESHA_MCP_DB_HOST:-127.0.0.1}"
export MESHA_MCP_DB_PORT="${MESHA_MCP_DB_PORT:-5433}"
export MESHA_MCP_DB_NAME="${MESHA_MCP_DB_NAME:-goatos}"
export MESHA_MCP_DB_USER="${MESHA_MCP_DB_USER:-mesha_ceo_readonly}"

if [ -z "${MESHA_MCP_DB_PASSWORD:-}" ]; then
  log "ERROR: MESHA_MCP_DB_PASSWORD is not set (needed for the read-only role)."
  log "Add it to $ENV_FILE or export it. See docs/runbooks/mcp-toolbox-local.md."
  exit 3
fi

# ---- binary (download-if-missing, pinned + gitignored) ----------------------
ensure_binary() {
  if [ -x "$TOOLBOX_BIN" ]; then
    local have; have="$("$TOOLBOX_BIN" --version 2>/dev/null | awk '{print $3}' | cut -d'+' -f1 || true)"
    if [ "$have" = "$TOOLBOX_VERSION" ]; then
      return 0
    fi
    log "toolbox version mismatch (have '${have:-none}', want '$TOOLBOX_VERSION'); re-downloading"
  fi
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *) log "unsupported OS $(uname -s)"; exit 4 ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch="arm64" ;;
    x86_64|amd64)  arch="amd64" ;;
    *) log "unsupported arch $(uname -m)"; exit 4 ;;
  esac
  local url="https://storage.googleapis.com/genai-toolbox/v${TOOLBOX_VERSION}/${os}/${arch}/toolbox"
  mkdir -p "$(dirname "$TOOLBOX_BIN")"
  log "downloading toolbox v${TOOLBOX_VERSION} (${os}/${arch})"
  curl -fsSL "$url" -o "$TOOLBOX_BIN"
  chmod +x "$TOOLBOX_BIN"
  "$TOOLBOX_BIN" --version >/dev/null 2>&1 || { log "downloaded binary failed --version"; exit 4; }
}

ensure_binary

# ---- idempotent start -------------------------------------------------------
if [ "$mode" = "restart" ]; then
  stop_server
fi

if is_healthy; then
  if [ "$mode" = "restart" ]; then
    : # already stopped above; continue to start
  else
    log "toolbox already healthy on ${ADDRESS}:${PORT} (no-op). Use --restart to replace."
    exit 0
  fi
fi

# A stale process on the port but not from us -> fail clearly, don't fight it.
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1 && ! is_healthy; then
  log "ERROR: port $PORT is occupied by a non-healthy process. Free it or set MESHA_MCP_TOOLBOX_PORT."
  exit 5
fi

toolbox_args=(
  --config "$CONFIG_FILE"
  --address "$ADDRESS"
  --port "$PORT"
  --enable-api
  --allowed-hosts "127.0.0.1,localhost"
  --allowed-origins "http://127.0.0.1:3300,http://localhost:3300"
  --log-level info
)

log "toolset: mesha_ceo_toolset  db: ${MESHA_MCP_DB_USER}@${MESHA_MCP_DB_HOST}:${MESHA_MCP_DB_PORT}/${MESHA_MCP_DB_NAME}"

if [ "$foreground" -eq 1 ]; then
  log "starting toolbox in foreground on ${ADDRESS}:${PORT} (Ctrl-C to stop)"
  exec "$TOOLBOX_BIN" "${toolbox_args[@]}"
fi

mkdir -p "$(dirname "$LOG_FILE")"
log "starting toolbox on ${ADDRESS}:${PORT} (logs: $LOG_FILE)"
nohup "$TOOLBOX_BIN" "${toolbox_args[@]}" >"$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"

# health wait
for _ in $(seq 1 20); do
  if is_healthy; then
    log "healthy on ${ADDRESS}:${PORT} (pid $(cat "$PID_FILE"))"
    log "toolset manifest: curl -s http://${ADDRESS}:${PORT}/api/toolset/mesha_ceo_toolset"
    exit 0
  fi
  sleep 0.5
done

log "ERROR: toolbox did not become healthy; last log lines:"
tail -20 "$LOG_FILE" >&2 || true
stop_server
exit 6
