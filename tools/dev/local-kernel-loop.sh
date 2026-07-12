#!/bin/sh
set -eu

mode="${1:-}"
interval="${GOATOS_LOCAL_KERNEL_INTERVAL_SECONDS:-15}"

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
      run_worker obligation-sweeper /app/bin/obligation-sweeper -timeout=45s
      run_worker calendar-projector /app/bin/calendar-vaccination-projector -timeout=45s
      run_worker process-integrity-projector /app/bin/process-integrity-projection-recompute -timeout=45s
      run_worker vaccination-projection-worker /app/bin/vaccination-projection-worker -timeout=45s -limit=100
      run_worker notification-dispatcher /app/bin/notification-dispatcher -timeout=30s -limit=100 -dry-run
      sleep "$interval"
    done
    ;;
  *)
    printf 'usage: %s outbox-relay|workers\n' "$0" >&2
    exit 2
    ;;
esac
