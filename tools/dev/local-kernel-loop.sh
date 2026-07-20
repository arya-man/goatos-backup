#!/bin/sh
set -eu

mode="${1:-}"
interval="${GOATOS_LOCAL_KERNEL_INTERVAL_SECONDS:-15}"
maintenance_interval="${GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS:-3600}"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
sweeper_actor_id="${GOATOS_SWEEPER_ACTOR_ID:-${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000101}}"

run_worker() {
  name="$1"
  shift
  printf '%s local kernel worker start: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$name"
  if "$@"; then
    printf '%s local kernel worker complete: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$name"
  else
    status="$?"
    printf '%s local kernel worker failed: %s status=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$name" "$status" >&2
    return "$status"
  fi
}

case "$mode" in
  outbox-relay)
    while true; do
      run_worker outbox-relay /app/bin/outbox-relay -limit=500 -timeout=30s
      sleep "${GOATOS_LOCAL_OUTBOX_POLL_SECONDS:-1}"
    done
    ;;
  workers)
    while true; do
      run_worker vaccination-generator /app/bin/generate-vaccination-obligations -timeout=45s
      run_worker obligation-sweeper /app/bin/obligation-sweeper -tenant-id "$tenant_id" -actor-id "$sweeper_actor_id" -timeout=45s
      run_worker notification-dispatcher /app/bin/notification-dispatcher -timeout=30s -limit=100 -dry-run
      sleep "$interval"
    done
    ;;
  maintenance)
    while true; do
      run_worker domain-event-processed-sweeper /app/bin/domain-event-processed-sweeper -timeout=30s -limit=1000
      run_worker idempotency-key-sweeper /app/bin/idempotency-key-sweeper -timeout=30s -limit=1000
      run_worker inventory-batch-reconciler /app/bin/inventory-batch-reconciler -timeout=60s -limit=1000
      run_worker sop-review-fanout-retry /app/bin/sop-review-fanout-retry -timeout=60s -limit=100
      sleep "$maintenance_interval"
    done
    ;;
  *)
    printf 'usage: %s outbox-relay|workers|maintenance\n' "$0" >&2
    exit 2
    ;;
esac
