#!/usr/bin/env bash
# Durable local bring-up for the Mesha Leadership Assistant stack (backend + admin-web).
# Cube (:4000) + MCP toolbox (:5001) are expected already running (docker/agent).
# Run this in a real terminal so the processes survive your session.
set -euo pipefail
export PATH=/usr/bin:/bin:/usr/local/bin:/opt/homebrew/bin:$PATH
REPO=/Users/ravi/mesha/goatos
cd "$REPO"

# --- compose backend env (shared known auth secret so admin-web token is accepted) ---
set -a
[ -f /tmp/goatos-api-full.sh ] && source /tmp/goatos-api-full.sh || true
[ -f "$REPO/.env.ceo-ai.local" ] && source "$REPO/.env.ceo-ai.local" || true
GOATOS_ENV=local
GOATOS_AUTH_MODE=bearer
GOATOS_AUTH_HS256_SECRET=goatos-local-dev-secret-32-bytes-min
GOATOS_AUTH_ISSUER=goatos-local
GOATOS_AUTH_AUDIENCE=goatos-api
GOATOS_HTTP_ADDR=127.0.0.1:8080
MESHA_AI_PROVIDER=vertex
MESHA_VERTEX_PROJECT=goatos-stg
MESHA_VERTEX_LOCATION=asia-south1
MESHA_VERTEX_MODEL=gemini-2.5-flash
MESHA_AI_REVIEW=1
MESHA_CUBE_URL=http://127.0.0.1:4000
MESHA_MCP_TOOLBOX_ADDRESS=127.0.0.1:5001
set +a

# --- backend ---
pkill -9 -f 'bin/api' 2>/dev/null || true; sleep 1
nohup "$REPO/backend/bin/api" > /tmp/goatos-api.log 2>&1 < /dev/null &
echo "backend pid $!"
for i in $(seq 1 15); do curl -sf -o /dev/null http://127.0.0.1:8080/readyz && { echo "backend UP"; break; }; sleep 2; done

# --- admin-web (skip migrate; allow dirty tree; default secret matches backend) ---
cd "$REPO/apps/admin-web"
pkill -9 -f 'run-local-next' 2>/dev/null || true; sleep 1
export GOATOS_LOCAL_DB_PREPARED=1 GOATOS_ALLOW_STALE_LOCAL_STACK=1 GOATOS_ENV=local GOATOS_AUTH_MODE=bearer
nohup npm run dev:local > /tmp/admin-web-3300.log 2>&1 < /dev/null &
echo "admin-web pid $!"
for i in $(seq 1 25); do code=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3300/ --max-time 4); [ "$code" != "000" ] && { echo "admin-web UP ($code)"; break; }; sleep 3; done
echo "OPEN: http://127.0.0.1:3300"
