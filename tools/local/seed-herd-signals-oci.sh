#!/usr/bin/env bash
# Herd Signals OCI dev seed -- two stages, one flow.
#
#   Stage 1 (SQL, tools/local/seed-herd-signals-oci.sql)
#     loads EXTERNAL FACTS only: the gateway, the captured BLE packets, and the
#     tag-to-animal goat_identifiers rows.
#
#   Stage 2 (Go, backend/cmd/seed-herd-signals-oci)
#     replays the same capture through the real herd-signals ingest service so
#     herd_signal_tag_latest and herd_signal_activity_windows are computed by
#     shipped code. Nothing hand-derives read-model state in SQL; a seeded
#     stand-in for derived state drifts from the classifier it imitates.
#
# SAFETY: refuses to run against anything except the OCI dev database reached
# through the SSH tunnel on 127.0.0.1:15432, database "goatos". In particular it
# refuses the local 5433 stack and any non-loopback host.
#
# Usage:
#   bash tools/local/seed-herd-signals-oci.sh [--csv PATH] [--facts-only] [--replay-only]
#
# Prerequisites:
#   1. $HOME/mesha/tools/local/oci-goatos-a1-dev.sh tunnel
#   2. DATABASE_URL exported, or the local OCI env file present (auto-sourced).
#
# Full runbook: docs/runbooks/herd-signals-oci-seed.md

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_FILE="${GOATOS_OCI_ENV_FILE:-${GOATOS_OCI_DB_ENV:-$HOME/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env}}"
SEED_SQL="${SCRIPT_DIR}/seed-herd-signals-oci.sql"

TENANT_ID="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
GATEWAY_ID="${GOATOS_HERD_SIGNALS_GATEWAY_ID:-honeycomm-gateway-001}"
CSV_PATH="${GOATOS_HERD_SIGNALS_CAPTURE_CSV:-$HOME/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv}"

# Only this host:port is an acceptable target. The OCI Postgres is bound to the
# VM's loopback and is only reachable through the SSH tunnel, so a correct run
# always talks to 127.0.0.1:15432.
ALLOWED_HOST="127.0.0.1"
ALLOWED_PORT="15432"
ALLOWED_DB="goatos"

run_facts=1
run_replay=1

while [ $# -gt 0 ]; do
  case "$1" in
    --csv) CSV_PATH="$2"; shift 2 ;;
    --csv=*) CSV_PATH="${1#*=}"; shift ;;
    --facts-only) run_replay=0; shift ;;
    --replay-only) run_facts=0; shift ;;
    *) echo "seed-herd-signals-oci: unknown argument: $1" >&2; exit 2 ;;
  esac
done

log()  { echo "[seed-herd-signals-oci] $*"; }
die()  { echo "[seed-herd-signals-oci] ERROR: $*" >&2; exit 1; }

# ============================================================================
# Resolve DATABASE_URL
#
# An explicitly exported DATABASE_URL is HONOURED, not overwritten. The previous
# version of this script sourced the OCI env file unconditionally, which meant
# pointing it at prod or at the local 5433 stack was silently rewritten to the
# OCI URL instead of refused -- a guard that can never fire is not a guard.
# ============================================================================
if [ -z "${DATABASE_URL:-}" ]; then
  [ -f "$ENV_FILE" ] || die "DATABASE_URL is unset and the OCI env file is missing: $ENV_FILE"
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  log "sourced DATABASE_URL from $ENV_FILE"
else
  log "using DATABASE_URL from the environment"
fi
[ -n "${DATABASE_URL:-}" ] || die "DATABASE_URL is empty"

# ============================================================================
# REFUSE-TO-RUN GUARD
# ============================================================================
url_no_scheme="${DATABASE_URL#*://}"
url_authority="${url_no_scheme%%\?*}"          # strip query string
url_hostport_db="${url_authority#*@}"          # strip user:password@ if present
url_hostport="${url_hostport_db%%/*}"
url_db="${url_hostport_db#*/}"
url_db="${url_db%%\?*}"
url_host="${url_hostport%%:*}"
url_port="${url_hostport##*:}"
[ "$url_port" = "$url_host" ] && url_port="5432"

log "target host=${url_host} port=${url_port} db=${url_db}"

if [ "$url_host" != "$ALLOWED_HOST" ] && [ "$url_host" != "localhost" ]; then
  die "REFUSING TO RUN: host '${url_host}' is not the OCI SSH tunnel (${ALLOWED_HOST}). tools/local seeds never target remote/production databases."
fi
if [ "$url_port" != "$ALLOWED_PORT" ]; then
  die "REFUSING TO RUN: port '${url_port}' is not the OCI tunnel port (${ALLOWED_PORT}). Port 5433 is the shared local stack and 5432 is a direct/production socket; this seed targets the OCI dev database only."
fi
if [ "$url_db" != "$ALLOWED_DB" ]; then
  die "REFUSING TO RUN: database '${url_db}' is not '${ALLOWED_DB}'."
fi

# ============================================================================
# Prerequisites
# ============================================================================
[ -f "$SEED_SQL" ] || die "seed SQL not found: $SEED_SQL"
[ -f "$CSV_PATH" ] || die "capture CSV not found: $CSV_PATH"

nc -z "$ALLOWED_HOST" "$ALLOWED_PORT" 2>/dev/null \
  || die "SSH tunnel is not listening on ${ALLOWED_HOST}:${ALLOWED_PORT}. Start it with: \$HOME/mesha/tools/local/oci-goatos-a1-dev.sh tunnel"

psql "$DATABASE_URL" -qAt -c "SELECT 1" >/dev/null 2>&1 || die "cannot connect to the database"
log "tunnel and connection verified"

# ============================================================================
# Stage 1: facts
# ============================================================================
if [ "$run_facts" -eq 1 ]; then
  log "stage 1/2: loading external facts (gateway, packets, identifiers) from ${CSV_PATH}"
  # The capture path travels as an env var read by a client-side \copy FROM
  # PROGRAM: psql does not expand :variables inside \copy arguments, and making
  # the path injectable is what lets a frozen copy of the capture be used.
  GOATOS_HERD_SIGNALS_CAPTURE_CSV="$CSV_PATH" \
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$SEED_SQL"
else
  log "stage 1/2: skipped (--replay-only)"
fi

# ============================================================================
# Stage 2: derived read models, computed by shipped code
# ============================================================================
if [ "$run_replay" -eq 1 ]; then
  log "stage 2/2: replaying the capture through the ingest service (computes tag_latest + activity_windows)"
  (cd "$REPO_ROOT/backend" && DATABASE_URL="$DATABASE_URL" go run ./cmd/seed-herd-signals-oci \
    -csv "$CSV_PATH" \
    -tenant-id "$TENANT_ID" \
    -gateway-id "$GATEWAY_ID")
else
  log "stage 2/2: skipped (--facts-only)"
fi

log "done"
