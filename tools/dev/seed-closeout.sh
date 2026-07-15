#!/usr/bin/env bash
# Deterministic post-seed/post-migration closeout.
#
# This script rebuilds derived read models from canonical source data. It must
# not insert fake business truth. If a new app-visible projection table is added,
# register its projector/backfill here in the same patch.

set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
dry_run=0

for arg in "$@"; do
  case "$arg" in
    --dry-run) dry_run=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

if [ -z "${tenant_id// }" ]; then
  echo "GOATOS_TENANT_ID is required for seed closeout" >&2
  exit 2
fi

run_cmd() {
  local label="$1"; shift
  echo "==> seed-closeout: ${label}"
  if [ "$dry_run" -eq 1 ]; then
    printf '    cd backend &&'
    printf ' %q' "$@"
    printf '\n'
    return
  fi
  (cd "$repo/backend" && "$@")
}

run_go_cmd() {
  local cmd="$1"; shift
  if [ ! -d "$repo/backend/cmd/$cmd" ]; then
    echo "==> seed-closeout: skip ${cmd} (command not present in this checkout)"
    return
  fi
  run_cmd "$cmd" go run "./cmd/$cmd" "$@"
}

run_required_projectors() {
  # U7 (operational-kernel-5k-50k-scale-envelope ADR): the process-integrity, vaccination
  # shed/execution/operations screens are served from canonical indexed SQL at the 5k-50k
  # envelope, so their projection tables + projector commands were dropped/removed. No
  # recompute is required for those screens (seed-green is a canonical-read 200). Only the
  # SURVIVING indexed summary — the vaccination eligibility rollup — is recomputed here.
  run_go_cmd vaccination-eligibility-rollup-recompute -tenant-id "$tenant_id"
}

run_calendar_projectors() {
  if [ "${GOATOS_SEED_CLOSEOUT_RUN_CALENDAR:-1}" = "0" ]; then
    echo "==> seed-closeout: skip calendar projectors (GOATOS_SEED_CLOSEOUT_RUN_CALENDAR=0)"
    return
  fi
}

run_counts_projectors() {
  if [ "${GOATOS_SEED_CLOSEOUT_RUN_COUNTS:-0}" = "0" ]; then
    echo "==> seed-closeout: skip counts projector (set GOATOS_SEED_CLOSEOUT_RUN_COUNTS=1 with GOATOS_COUNTS_PARK_ID and GOATOS_COUNTS_TARGET_DATE)"
    return
  fi
  if [ -z "${GOATOS_COUNTS_PARK_ID:-}" ] || [ -z "${GOATOS_COUNTS_TARGET_DATE:-}" ]; then
    echo "GOATOS_COUNTS_PARK_ID and GOATOS_COUNTS_TARGET_DATE are required when GOATOS_SEED_CLOSEOUT_RUN_COUNTS=1" >&2
    exit 2
  fi
  run_go_cmd counts-projection-recompute \
    -tenant-id "$tenant_id" \
    -park-id "$GOATOS_COUNTS_PARK_ID" \
    -target-date "$GOATOS_COUNTS_TARGET_DATE"
}

echo "seed-closeout: tenant=${tenant_id}"
# VACC-REV-02 gate: refuse to run closeout against a RESET_REQUIRED (failed) seed. A half-seeded
# database must never be dressed up as healthy by rebuilding read models on top of it.
if [ "$dry_run" -eq 0 ] && [ -d "$repo/backend/cmd/seed-state-check" ]; then
  echo "==> seed-closeout: seed-state-check (mode=closeout)"
  (cd "$repo/backend" && go run ./cmd/seed-state-check -tenant-id "$tenant_id" -mode closeout)
fi
run_required_projectors
run_calendar_projectors
run_counts_projectors
echo "seed-closeout: complete"
