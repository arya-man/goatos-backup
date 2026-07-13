#!/bin/sh
set -eu

mode="${1:-}"
interval="${GOATOS_LOCAL_KERNEL_INTERVAL_SECONDS:-15}"
maintenance_interval="${GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS:-3600}"

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
      run_worker obligation-sweeper /app/bin/obligation-sweeper -timeout=45s -project-calendar=false -project-vaccination-read-models=true
      run_worker calendar-reminder-sweeper /app/bin/calendar-reminder-sweeper -timeout=30s -limit=100
      run_worker calendar-escalation-sweeper /app/bin/calendar-escalation-sweeper -timeout=30s -limit=100
      # C5-001: the completed-history projection replays up to ~400 days of accepted vaccination
      # history on every run; history changes slowly, so it is decoupled onto the far-less-frequent
      # maintenance loop below (-project-calendar-history=false here) instead of full-replaying it on
      # every workers-loop tick (default every 15s locally / every 5m in Cloud Run).
      run_worker calendar-projector /app/bin/calendar-vaccination-projector -timeout=45s -project-calendar-history=false
      run_worker process-integrity-projector /app/bin/process-integrity-projection-recompute -timeout=45s
      run_worker vaccination-projection-worker /app/bin/vaccination-projection-worker -timeout=45s -limit=200 -enqueue-due-transitions=true -due-transitions-limit=200
      run_worker notification-dispatcher /app/bin/notification-dispatcher -timeout=30s -limit=100 -dry-run
      sleep "$interval"
    done
    ;;
  maintenance)
    while true; do
      # C5-001: the ONLY place calendar-vaccination-projector's history recompute
      # (-project-calendar-history=true) runs locally, on the maintenance loop's hourly-scale cadence
      # (GOATOS_LOCAL_MAINTENANCE_INTERVAL_SECONDS, default 3600s) rather than the workers loop's much
      # tighter interval. Also re-refreshes the upcoming projection as a side effect of the shared
      # binary entrypoint -- cheap and harmless, not the waste this decouples.
      run_worker calendar-history-projector /app/bin/calendar-vaccination-projector -timeout=45s -project-calendar-history=true
      run_worker domain-event-processed-sweeper /app/bin/domain-event-processed-sweeper -timeout=30s -limit=1000
      run_worker idempotency-key-sweeper /app/bin/idempotency-key-sweeper -timeout=30s -limit=1000
      run_worker inventory-batch-reconciler /app/bin/inventory-batch-reconciler -timeout=60s -limit=1000
      run_worker sop-review-fanout-retry /app/bin/sop-review-fanout-retry -timeout=60s -limit=100
      run_worker partition-maintainer /app/bin/partition-maintainer -timeout=60s -months-ahead=12
      sleep "$maintenance_interval"
    done
    ;;
  *)
    printf 'usage: %s outbox-relay|workers|maintenance\n' "$0" >&2
    exit 2
    ;;
esac
