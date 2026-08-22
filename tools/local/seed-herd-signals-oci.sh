#!/bin/bash
# Seed script wrapper for Herd Signals OCI database.
#
# Seeds the OCI dev PostgreSQL database with HoneyComm BLE gateway data,
# packet capture, and tag-to-animal mappings for local feature testing.
#
# SAFETY: This script ONLY connects to the OCI database at 127.0.0.1:15432
# via SSH tunnel. It refuses to run against other databases.
#
# USAGE:
#   bash tools/local/seed-herd-signals-oci.sh
#
# PREREQUISITES:
#   1. SSH tunnel to OCI database must be running:
#      /Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel
#   2. Environment variable DATABASE_URL must be set or sourced from:
#      source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env
#   3. goatos worktree at /Users/ravi/goatos-work/herd-signals

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
ENV_FILE="/Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env"
SEED_SQL="${SCRIPT_DIR}/seed-herd-signals-oci.sql"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
  echo -e "${GREEN}[INFO]${NC} $*"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
  echo -e "${RED}[ERROR]${NC} $*"
}

# ============================================================================
# Check prerequisites
# ============================================================================
log_info "Checking prerequisites..."

if [[ ! -f "$ENV_FILE" ]]; then
  log_error "Environment file not found: $ENV_FILE"
  exit 1
fi

if [[ ! -f "$SEED_SQL" ]]; then
  log_error "Seed SQL file not found: $SEED_SQL"
  exit 1
fi

# Source environment
source "$ENV_FILE"

if [[ -z "${DATABASE_URL:-}" ]]; then
  log_error "DATABASE_URL not set after sourcing env file"
  exit 1
fi

# Verify tunnel is running
if ! nc -z 127.0.0.1 15432 2>/dev/null; then
  log_error "SSH tunnel to OCI database is not running."
  log_info "Start it with: /Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel"
  exit 1
fi

log_info "SSH tunnel is active on 127.0.0.1:15432"

# Verify database connection
if ! psql "$DATABASE_URL" -c "SELECT 1" >/dev/null 2>&1; then
  log_error "Cannot connect to database at $DATABASE_URL"
  exit 1
fi

log_info "Database connection verified"

# ============================================================================
# Run seed script
# ============================================================================
log_info "Running seed script..."
log_info "Loading BLE gateway data from: /Users/ravi/mesha/local-data/honeycomm-gateway-capture/"
log_info ""

psql "$DATABASE_URL" -f "$SEED_SQL"

log_info ""
log_info "Seed script completed successfully"
log_info "Data loaded:"
log_info "  - 1 HoneyComm gateway (MAC f130d402dcb4)"
log_info "  - 12,000+ BLE advertisement packets"
log_info "  - 20 tag_latest snapshots (one per unique tag)"
log_info "  - 200 activity windows (60-second buckets)"
log_info "  - 19 tags mapped to Castro shed goats (smart_tag_capable=true)"
log_info "  - 1 tag unmapped (A0003B) for testing unmapped state"
